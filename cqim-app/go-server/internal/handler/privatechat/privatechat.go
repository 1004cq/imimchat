// Package privatechat 移植自 server/private-chat.ts：私聊会话与消息 API。
//
// 挂载前缀 /api/chat，全部路由需要 userAuth。
// WS 实时投递：本机连接由主进程通过 LocalDeliver 注册；跨节点走 Redis
// 频道 cqim:im:push（与 Node 端 publish-im.ts 频道名一致）；离线推送由
// 推送模块通过 PushNotifier 注册。
package privatechat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// LocalDeliver 由主进程注册的本机 WS 投递函数（对应 Node 端 req.app.locals.trySendTo）。
// 返回 true 表示已投递到本机持有的连接；未注册（nil）时视为未投递，
// 走跨节点 Redis 推送，由持有对端连接的节点/Gateway 完成投递。
var LocalDeliver func(userID string, payload map[string]any) bool

// PushNotifier 由推送模块注册的离线推送函数（对应 Node 端 notifyPrivateMessagePush）。
// 仅在对端既不在本机、跨节点推送也未确认送达、且 Redis 在线标记缺失时调用。
// 未注册时不做推送。
var PushNotifier func(toUserID, senderID, chatID, messageID, previewText string)

// Handler 私聊模块 handler，共享 Deps。
type Handler struct{ d *handler.Deps }

// RegisterRoutes 注册 /api/chat 下全部路由（全部包 userAuth）。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := &Handler{d: d}
	auth := d.Auth.UserAuth
	mux.Handle("POST /api/chat/create", auth(http.HandlerFunc(h.createChat)))
	mux.Handle("POST /api/chat/send", auth(http.HandlerFunc(h.sendUnified)))
	mux.Handle("GET /api/chat/list", auth(http.HandlerFunc(h.listChats)))
	mux.Handle("DELETE /api/chat/{chatId}", auth(http.HandlerFunc(h.hideChat)))
	mux.Handle("GET /api/chat/{chatId}", auth(http.HandlerFunc(h.getChatDetail)))
	mux.Handle("POST /api/chat/{chatId}/messages", auth(http.HandlerFunc(h.sendMessage)))
	mux.Handle("GET /api/chat/{chatId}/messages", auth(http.HandlerFunc(h.listMessages)))
	mux.Handle("POST /api/chat/{chatId}/read", auth(http.HandlerFunc(h.markRead)))
	mux.Handle("POST /api/chat/{chatId}/recall/{messageId}", auth(http.HandlerFunc(h.recallMessage)))
}

// ============ 行结构（与查询列一一对应） ============

type chatRow struct {
	ID            string     `db:"id"`
	ParticipantA  string     `db:"participantA"`
	ParticipantB  string     `db:"participantB"`
	LastMessage   *string    `db:"lastMessage"`
	LastMessageAt *time.Time `db:"lastMessageAt"`
	CreatedAt     time.Time  `db:"createdAt"`
}

type userBrief struct {
	ID       string  `db:"id"`
	Username string  `db:"username"`
	Nickname *string `db:"nickname"`
	Avatar   *string `db:"avatar"`
	Bio      *string `db:"bio"`
}

type privateMessageRow struct {
	ID            string     `db:"id"`
	ChatID        string     `db:"chatId"`
	SenderID      string     `db:"senderId"`
	MsgType       string     `db:"msgType"`
	Content       string     `db:"content"`
	ReplyToID     *string    `db:"replyToId"`
	IsRevoked     bool       `db:"isRevoked"`
	Status        string     `db:"status"`
	Extra         *string    `db:"extra"`
	BurnAfterRead *int32     `db:"burnAfterRead"`
	BurnReadAt    *time.Time `db:"burnReadAt"`
	Hmac          *string    `db:"hmac"`
	CreatedAt     time.Time  `db:"createdAt"`
}

// ============ 响应 JSON（字段名照抄 TS，camelCase） ============

type peerJSON struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
}

type chatJSON struct {
	ID            string    `json:"id"`
	ParticipantA  string    `json:"participantA"`
	ParticipantB  string    `json:"participantB"`
	LastMessage   *string   `json:"lastMessage"`
	LastMessageAt *int64    `json:"lastMessageAt"`
	CreatedAt     int64     `json:"createdAt"`
	UnreadCount   *int      `json:"unreadCount,omitempty"`
	Peer          *peerJSON `json:"peer"`
}

type messageJSON struct {
	ID            string         `json:"id"`
	ChatID        string         `json:"chatId"`
	SenderID      string         `json:"senderId"`
	MsgType       string         `json:"msgType"`
	Content       string         `json:"content"`
	ReplyToID     *string        `json:"replyToId"`
	IsRevoked     bool           `json:"isRevoked"`
	Status        string         `json:"status"`
	Extra         map[string]any `json:"extra,omitempty"`
	CreatedAt     int64          `json:"createdAt"`
	BurnAfterRead *int32         `json:"burnAfterRead,omitempty"`
	BurnReadAt    *int64         `json:"burnReadAt,omitempty"`
	Hmac          *string        `json:"hmac,omitempty"`
}

// ============ 纯函数 helper（照抄 TS 语义） ============

// normalizeParticipants 规范化两个参与者的顺序（字典序较小的为 A），
// 确保同一对用户始终映射到同一个 Chat 记录。
func normalizeParticipants(userA, userB string) (string, string) {
	if userA < userB {
		return userA, userB
	}
	return userB, userA
}

// parseExtra 解析 extra JSON 字段，失败/为空返回 nil。
func parseExtra(extra *string) map[string]any {
	if extra == nil || *extra == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(*extra), &out); err != nil {
		return nil
	}
	return out
}

func messagePreview(msgType, content string) string {
	switch msgType {
	case "image":
		return "[图片]"
	case "voice":
		return "[语音消息]"
	case "video":
		return "[视频]"
	case "file":
		return "[文件]"
	case "sticker":
		return "[贴纸]"
	case "location":
		return "[位置]"
	case "location_share":
		return "[位置共享]"
	case "call":
		return "[通话]"
	default:
		r := []rune(content)
		if len(r) > 100 {
			r = r[:100]
		}
		return string(r)
	}
}

func isParticipant(chat *chatRow, userID string) bool {
	return chat.ParticipantA == userID || chat.ParticipantB == userID
}

func peerIDFor(chat *chatRow, userID string) string {
	if chat.ParticipantA == userID {
		return chat.ParticipantB
	}
	return chat.ParticipantA
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func msPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.UnixMilli()
	return &v
}

// ============ avatarToProxy（移植自 server/cos-signer.ts 纯函数部分） ============

var zhToASCIISeg = map[string]string{
	"头像":  "avatars",
	"群头像": "group-avatars",
	"朋友圈": "moments",
	"照片":  "photos",
	"视频":  "videos",
}

func isCosURL(raw string) bool {
	if raw == "" || !strings.HasPrefix(raw, "https://") {
		return false
	}
	return strings.Contains(raw, ".cos.") || strings.Contains(raw, ".myqcloud.com") || strings.Contains(raw, "imim.chat")
}

func cosKeyToAlias(cosKey string) string {
	if cosKey == "" {
		return cosKey
	}
	segs := strings.Split(cosKey, "/")
	for i, s := range segs {
		if alias, ok := zhToASCIISeg[s]; ok {
			segs[i] = alias
		}
	}
	return strings.Join(segs, "/")
}

// avatarToProxy：COS 直链转代理 URL；非 COS URL 原样返回。
func avatarToProxy(raw string) string {
	if raw == "" {
		return ""
	}
	if !isCosURL(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	cosKey, err := url.PathUnescape(strings.TrimPrefix(u.Path, "/"))
	if err != nil {
		return raw
	}
	aliasKey := cosKeyToAlias(cosKey)
	segs := strings.Split(aliasKey, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/api/cos/proxy/" + strings.Join(segs, "/") + "?imageMogr2/thumbnail/200x200/format/webp/quality/80"
}

func peerJSONFrom(u *userBrief) *peerJSON {
	if u == nil {
		return nil
	}
	nickname := u.Username
	if u.Nickname != nil && *u.Nickname != "" {
		nickname = *u.Nickname
	}
	return &peerJSON{
		ID:       u.ID,
		Username: u.Username,
		Nickname: nickname,
		Avatar:   avatarToProxy(util.StrVal(u.Avatar)),
		Bio:      util.StrVal(u.Bio),
	}
}

func chatJSONFrom(c *chatRow, peer *peerJSON) *chatJSON {
	return &chatJSON{
		ID:            c.ID,
		ParticipantA:  c.ParticipantA,
		ParticipantB:  c.ParticipantB,
		LastMessage:   c.LastMessage,
		LastMessageAt: msPtr(c.LastMessageAt),
		CreatedAt:     ms(c.CreatedAt),
		Peer:          peer,
	}
}

// ============ DB 小查询 ============

const chatSelectCols = `"id","participantA","participantB","lastMessage","lastMessageAt","createdAt"`

func (h *Handler) getChat(ctx context.Context, chatID string) (*chatRow, error) {
	return db.QueryRowToStruct[chatRow](ctx, h.d.DB,
		`SELECT `+chatSelectCols+` FROM "Chat" WHERE "id"=$1`, chatID)
}

func (h *Handler) getUserBrief(ctx context.Context, userID string) (*userBrief, error) {
	return db.QueryRowToStruct[userBrief](ctx, h.d.DB,
		`SELECT "id","username","nickname","avatar","bio" FROM "User" WHERE "id"=$1`, userID)
}

// decodeBody 解析 JSON 请求体；空 body 视为 {}（与 Express 行为一致），
// 非法 JSON 返回 400。
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		util.WriteError(w, http.StatusBadRequest, "请求参数格式错误")
		return false
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return true
	}
	if err := json.Unmarshal(data, v); err != nil {
		util.WriteError(w, http.StatusBadRequest, "请求参数格式错误")
		return false
	}
	return true
}

// ============ 创建私聊消息 + 通知（对应 TS createPrivateMessageAndNotify） ============

func (h *Handler) createAndNotifyMessage(w http.ResponseWriter, r *http.Request, chatID, msgType, content string, replyToID *string, extra map[string]any) {
	ctx := r.Context()
	me := middleware.UserFrom(r).Id

	chat, err := h.getChat(ctx, chatID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, http.StatusNotFound, "会话不存在")
			return
		}
		log.Printf("[PrivateChat] 查询会话失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "发送消息失败")
		return
	}
	if !isParticipant(chat, me) {
		util.WriteError(w, http.StatusForbidden, "无权发送消息")
		return
	}

	msgID := util.NewID()
	var extraStr *string
	if len(extra) > 0 {
		if b, err := json.Marshal(extra); err == nil {
			s := string(b)
			extraStr = &s
		}
	}
	var createdAt time.Time
	err = h.d.DB.Pool.QueryRow(ctx,
		`INSERT INTO "PrivateMessage" ("id","chatId","senderId","msgType","content","replyToId","extra","status","createdAt")
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'sent',NOW()) RETURNING "createdAt"`,
		msgID, chatID, me, msgType, content, replyToID, extraStr).Scan(&createdAt)
	if err != nil {
		log.Printf("[PrivateChat] 创建消息失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "发送消息失败")
		return
	}

	preview := messagePreview(msgType, content)
	if _, err := h.d.DB.Exec(ctx,
		`UPDATE "Chat" SET "lastMessage"=$2,"lastMessageAt"=$3,"updatedAt"=NOW() WHERE "id"=$1`,
		chatID, preview, createdAt); err != nil {
		log.Printf("[PrivateChat] 更新会话预览失败: %v", err)
	}

	result := &messageJSON{
		ID:        msgID,
		ChatID:    chatID,
		SenderID:  me,
		MsgType:   msgType,
		Content:   content,
		ReplyToID: replyToID,
		IsRevoked: false,
		Status:    "sent",
		Extra:     extra,
		CreatedAt: ms(createdAt),
	}

	h.notifyPeer(ctx, me, chat, result, preview)
	util.WriteJSON(w, http.StatusOK, map[string]any{"message": result})
}

// notifyPeer 未读数/缓存失效/WS 投递/跨节点推送/离线推送（对应 TS 逻辑）。
func (h *Handler) notifyPeer(ctx context.Context, senderID string, chat *chatRow, result *messageJSON, previewText string) {
	peerID := peerIDFor(chat, senderID)

	incrUnreadCount(ctx, h.d, peerID, chat.ID, 1)
	invalidateConversationList(ctx, h.d, senderID)
	invalidateConversationList(ctx, h.d, peerID)

	payload := map[string]any{"type": "private_message", "payload": result}
	delivered := false
	if LocalDeliver != nil {
		delivered = LocalDeliver(peerID, payload)
	}

	if !delivered {
		publishImPush(ctx, h.d, peerID, payload)
	}

	if !delivered {
		peerOnlineElsewhere := isUserOnline(ctx, h.d, peerID) || hasLegacyOnlineFlag(ctx, h.d, peerID)
		if !peerOnlineElsewhere && PushNotifier != nil {
			PushNotifier(peerID, senderID, chat.ID, result.ID, previewText)
		}
	}
}
