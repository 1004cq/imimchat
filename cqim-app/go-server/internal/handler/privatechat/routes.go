// 会话与消息 CRUD 路由，逐一对应 private-chat.ts 的 router 定义。
package privatechat

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// POST /api/chat/create — 创建或获取与目标用户的私聊会话。
func (h *Handler) createChat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r).Id

	var body struct {
		TargetUserID string `json:"targetUserId"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.TargetUserID == "" {
		util.WriteError(w, http.StatusBadRequest, "缺少 targetUserId")
		return
	}
	if body.TargetUserID == me {
		util.WriteError(w, http.StatusBadRequest, "不能和自己创建会话")
		return
	}

	target, err := h.getUserBrief(ctx, body.TargetUserID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, http.StatusNotFound, "用户不存在")
			return
		}
		log.Printf("[PrivateChat] 查询目标用户失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}

	participantA, participantB := normalizeParticipants(me, body.TargetUserID)

	chat, err := db.QueryRowToStruct[chatRow](ctx, h.d.DB,
		`SELECT `+chatSelectCols+` FROM "Chat" WHERE "participantA"=$1 AND "participantB"=$2`,
		participantA, participantB)
	if err != nil {
		if !db.IsNotFound(err) {
			log.Printf("[PrivateChat] 查询会话失败: %v", err)
			util.WriteError(w, http.StatusInternalServerError, "创建会话失败")
			return
		}
		chat, err = db.QueryRowToStruct[chatRow](ctx, h.d.DB,
			`INSERT INTO "Chat" ("id","participantA","participantB","createdAt","updatedAt")
			 VALUES ($1,$2,$3,NOW(),NOW()) RETURNING `+chatSelectCols,
			util.NewID(), participantA, participantB)
		if err != nil {
			log.Printf("[PrivateChat] 创建会话失败: %v", err)
			util.WriteError(w, http.StatusInternalServerError, "创建会话失败")
			return
		}
	}

	// 清除当前用户对该会话的隐藏标记
	if _, err := h.d.DB.Exec(ctx,
		`INSERT INTO "ChatHidden" ("id","chatId","userId","hiddenAt") VALUES ($1,$2,$3,NOW())
		 ON CONFLICT ("chatId","userId") DO UPDATE SET "hiddenAt"=NOW()`,
		util.NewID(), chat.ID, me); err != nil {
		log.Printf("[PrivateChat] 清除隐藏标记失败: %v", err)
	}

	util.WriteJSON(w, http.StatusOK, map[string]any{
		"chat": chatJSONFrom(chat, peerJSONFrom(target)),
	})
}

// GET /api/chat/list — 获取当前用户的所有私聊会话列表。
func (h *Handler) listChats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r).Id

	chats, err := db.QueryToStructs[chatRow](ctx, h.d.DB,
		`SELECT `+chatSelectCols+` FROM "Chat"
		 WHERE "participantA"=$1 OR "participantB"=$1
		 ORDER BY "lastMessageAt" DESC, "createdAt" DESC`, me)
	if err != nil {
		log.Printf("[PrivateChat] 获取会话列表失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "获取会话列表失败")
		return
	}

	hiddenAt := map[string]time.Time{}
	if len(chats) > 0 {
		ids := make([]string, len(chats))
		for i, c := range chats {
			ids[i] = c.ID
		}
		type hiddenRow struct {
			ChatID   string    `db:"chatId"`
			HiddenAt time.Time `db:"hiddenAt"`
		}
		rows, err := db.QueryToStructs[hiddenRow](ctx, h.d.DB,
			`SELECT "chatId","hiddenAt" FROM "ChatHidden" WHERE "userId"=$1 AND "chatId" IN (`+inPlaceholders(len(ids), 2)+`)`,
			append([]any{me}, stringsToAny(ids)...)...)
		if err != nil {
			log.Printf("[PrivateChat] 查询隐藏会话失败: %v", err)
			util.WriteError(w, http.StatusInternalServerError, "获取会话列表失败")
			return
		}
		for _, row := range rows {
			hiddenAt[row.ChatID] = row.HiddenAt
		}
	}

	var visible []chatRow
	for _, c := range chats {
		ha, ok := hiddenAt[c.ID]
		if !ok {
			visible = append(visible, c)
			continue
		}
		latest := c.CreatedAt
		if c.LastMessageAt != nil {
			latest = *c.LastMessageAt
		}
		if ha.Before(latest) {
			visible = append(visible, c)
		}
	}

	// Cache-Aside：先查 Redis 缓存
	if cached, ok := getCachedConversationList(ctx, h.d, me); ok {
		util.WriteJSON(w, http.StatusOK, map[string]any{"chats": jsonRaw(cached)})
		return
	}

	peerIDs := dedupPeerIDs(visible, me)
	peerMap := map[string]*userBrief{}
	if len(peerIDs) > 0 {
		peers, err := db.QueryToStructs[userBrief](ctx, h.d.DB,
			`SELECT "id","username","nickname","avatar","bio" FROM "User" WHERE "id" IN (`+inPlaceholders(len(peerIDs), 1)+`)`,
			stringsToAny(peerIDs)...)
		if err != nil {
			log.Printf("[PrivateChat] 查询对方用户失败: %v", err)
			util.WriteError(w, http.StatusInternalServerError, "获取会话列表失败")
			return
		}
		for i := range peers {
			peerMap[peers[i].ID] = &peers[i]
		}
	}

	result := make([]*chatJSON, 0, len(visible))
	for _, c := range visible {
		c := c
		peerID := peerIDFor(&c, me)
		unread := getUnreadCount(ctx, h.d, me, c.ID)
		if unread == 0 {
			// Redis miss 时查 DB 并作为兜底
			n, err := db.QueryRowToStruct[countRow](ctx, h.d.DB,
				`SELECT COUNT(*) AS "count" FROM "PrivateMessage"
				 WHERE "chatId"=$1 AND "senderId"!=$2 AND "status"!='read' AND "isRevoked"=false`,
				c.ID, me)
			if err == nil {
				unread = int(n.Count)
			}
		}
		peer, ok := peerMap[peerID]
		var pj *peerJSON
		if ok {
			pj = peerJSONFrom(peer)
		} else {
			pj = &peerJSON{ID: peerID, Username: peerID, Nickname: peerID, Avatar: "", Bio: ""}
		}
		cj := chatJSONFrom(&c, pj)
		u := unread
		cj.UnreadCount = &u
		result = append(result, cj)
	}

	setCachedConversationList(ctx, h.d, me, result)
	util.WriteJSON(w, http.StatusOK, map[string]any{"chats": result})
}

// DELETE /api/chat/:chatId — 对当前用户隐藏一个私聊会话；后续有新消息会重新出现。
func (h *Handler) hideChat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r).Id
	chatID := r.PathValue("chatId")

	chat, err := h.getChat(ctx, chatID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, http.StatusNotFound, "会话不存在")
			return
		}
		log.Printf("[PrivateChat] 查询会话失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "隐藏会话失败")
		return
	}
	if !isParticipant(chat, me) {
		util.WriteError(w, http.StatusForbidden, "无权删除此会话")
		return
	}

	if _, err := h.d.DB.Exec(ctx,
		`INSERT INTO "ChatHidden" ("id","chatId","userId","hiddenAt") VALUES ($1,$2,$3,NOW())
		 ON CONFLICT ("chatId","userId") DO UPDATE SET "hiddenAt"=NOW()`,
		util.NewID(), chatID, me); err != nil {
		log.Printf("[PrivateChat] 隐藏会话失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "隐藏会话失败")
		return
	}
	util.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// GET /api/chat/:chatId — 获取单个会话详情。
func (h *Handler) getChatDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r).Id
	chatID := r.PathValue("chatId")

	chat, err := h.getChat(ctx, chatID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, http.StatusNotFound, "会话不存在")
			return
		}
		log.Printf("[PrivateChat] 查询会话失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "获取会话详情失败")
		return
	}
	if !isParticipant(chat, me) {
		util.WriteError(w, http.StatusForbidden, "无权访问此会话")
		return
	}

	peer, err := h.getUserBrief(ctx, peerIDFor(chat, me))
	if err != nil && !db.IsNotFound(err) {
		log.Printf("[PrivateChat] 查询对方用户失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "获取会话详情失败")
		return
	}

	unread := 0
	if n, err := db.QueryRowToStruct[countRow](ctx, h.d.DB,
		`SELECT COUNT(*) AS "count" FROM "PrivateMessage"
		 WHERE "chatId"=$1 AND "senderId"!=$2 AND "status"!='read' AND "isRevoked"=false`,
		chat.ID, me); err == nil {
		unread = int(n.Count)
	}

	cj := chatJSONFrom(chat, peerJSONFrom(peer))
	cj.UnreadCount = &unread
	util.WriteJSON(w, http.StatusOK, map[string]any{"chat": cj})
}

// POST /api/chat/:chatId/messages — 发送私聊消息（强制端到端加密）。
func (h *Handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r).Id
	chatID := r.PathValue("chatId")

	var body struct {
		Content       string  `json:"content"`
		MsgType       string  `json:"msgType"`
		ReplyToID     *string `json:"replyToId"`
		Extra         any     `json:"extra"`
		BurnAfterRead *int    `json:"burnAfterRead"`
		Hmac          *string `json:"hmac"`
	}
	if !decodeBody(w, r, &body) {
		return
	}

	// 强制 P0：私聊必须加密，禁止明文发送
	if body.MsgType != "encrypted" {
		util.WriteError(w, http.StatusBadRequest, "私聊强制要求端到端加密，请发送加密消息")
		return
	}

	// 安全处理 extra：如果客户端传入了字符串，尝试解析为对象
	var extra map[string]any
	switch v := body.Extra.(type) {
	case string:
		extra = parseExtra(&v)
	case map[string]any:
		extra = v
	}

	if body.Content == "" {
		util.WriteError(w, http.StatusBadRequest, "加密信封不能为空")
		return
	}

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

	// 解析阅后即焚参数
	var burnSeconds *int32
	if body.BurnAfterRead != nil {
		for _, t := range validBurnTimers {
			if *body.BurnAfterRead == t {
				v := int32(t)
				burnSeconds = &v
				break
			}
		}
	}

	// 消息防篡改 HMAC 签名
	var hmac *string
	if body.Hmac != nil && hmacRe.MatchString(*body.Hmac) {
		hmac = body.Hmac
	}

	msgID := util.NewID()
	var extraStr *string
	if len(extra) > 0 {
		if b, err := marshalExtra(extra); err == nil {
			extraStr = &b
		}
	}
	var createdAt time.Time
	err = h.d.DB.Pool.QueryRow(ctx,
		`INSERT INTO "PrivateMessage"
		 ("id","chatId","senderId","msgType","content","replyToId","extra","status","burnAfterRead","hmac","createdAt")
		 VALUES ($1,$2,$3,'encrypted',$4,$5,$6,'sent',$7,$8,NOW()) RETURNING "createdAt"`,
		msgID, chatID, me, body.Content, body.ReplyToID, extraStr, burnSeconds, hmac).Scan(&createdAt)
	if err != nil {
		log.Printf("[PrivateChat] 发送消息失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "发送消息失败")
		return
	}

	if _, err := h.d.DB.Exec(ctx,
		`UPDATE "Chat" SET "lastMessage"='🔒 [加密消息]',"lastMessageAt"=$2,"updatedAt"=NOW() WHERE "id"=$1`,
		chatID, createdAt); err != nil {
		log.Printf("[PrivateChat] 更新会话预览失败: %v", err)
	}

	result := &messageJSON{
		ID:        msgID,
		ChatID:    chatID,
		SenderID:  me,
		MsgType:   "encrypted",
		Content:   body.Content,
		ReplyToID: body.ReplyToID,
		IsRevoked: false,
		Status:    "sent",
		Extra:     extra,
		CreatedAt: ms(createdAt),
	}
	if burnSeconds != nil {
		result.BurnAfterRead = burnSeconds
	}
	if hmac != nil {
		result.Hmac = hmac
	}

	h.notifyPeer(ctx, me, chat, result, "🔒 [加密消息]")
	util.WriteJSON(w, http.StatusOK, map[string]any{"message": result})
}

// GET /api/chat/:chatId/messages — 拉取私聊消息（分页，支持游标）。
func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r).Id
	chatID := r.PathValue("chatId")

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := parsePositiveInt(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}

	chat, err := h.getChat(ctx, chatID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, http.StatusNotFound, "会话不存在")
			return
		}
		log.Printf("[PrivateChat] 查询会话失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "拉取消息失败")
		return
	}
	if !isParticipant(chat, me) {
		util.WriteError(w, http.StatusForbidden, "无权访问此会话")
		return
	}

	where := `"chatId"=$1`
	args := []any{chatID}
	if before := r.URL.Query().Get("before"); before != "" {
		// 稳定游标：(createdAt, id) 元组比较，避免同毫秒消息重复/遗漏。
		cursor, err := db.QueryRowToStruct[cursorRow](ctx, h.d.DB,
			`SELECT "createdAt" FROM "PrivateMessage" WHERE "id"=$1`, before)
		if err == nil {
			where += ` AND ("createdAt","id") < ($2,$3)`
			args = append(args, cursor.CreatedAt, before)
		}
	}

	rows, err := db.QueryToStructs[privateMessageRow](ctx, h.d.DB,
		`SELECT "id","chatId","senderId","msgType","content","replyToId","isRevoked","status",
		        "extra","burnAfterRead","burnReadAt","hmac","createdAt"
		 FROM "PrivateMessage" WHERE `+where+`
		 ORDER BY "createdAt" DESC, "id" DESC LIMIT `+fmt.Sprint(limit+1), args...)
	if err != nil {
		log.Printf("[PrivateChat] 拉取消息失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "拉取消息失败")
		return
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	// 反转为时间正序
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}

	messages := make([]*messageJSON, 0, len(rows))
	for _, m := range rows {
		m := m
		content := m.Content
		var extra map[string]any
		if m.IsRevoked {
			content = "消息已撤回"
		} else {
			extra = parseExtra(m.Extra)
		}
		mj := &messageJSON{
			ID:        m.ID,
			ChatID:    m.ChatID,
			SenderID:  m.SenderID,
			MsgType:   m.MsgType,
			Content:   content,
			ReplyToID: m.ReplyToID,
			IsRevoked: m.IsRevoked,
			Status:    m.Status,
			Extra:     extra,
			CreatedAt: ms(m.CreatedAt),
		}
		if m.BurnAfterRead != nil {
			mj.BurnAfterRead = m.BurnAfterRead
			mj.BurnReadAt = msPtr(m.BurnReadAt)
		}
		if m.Hmac != nil {
			mj.Hmac = m.Hmac
		}
		messages = append(messages, mj)
	}

	util.WriteJSON(w, http.StatusOK, map[string]any{"messages": messages, "hasMore": hasMore})
}

// POST /api/chat/:chatId/read — 标记会话中对方发送的消息为已读。
func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r).Id
	chatID := r.PathValue("chatId")

	var body struct {
		MessageIDs []string `json:"messageIds"`
	}
	if !decodeBody(w, r, &body) {
		return
	}

	// 严格一致性策略：进入会话置 0 未读并删除会话列表缓存
	clearUnreadCount(ctx, h.d, me, chatID)
	invalidateConversationList(ctx, h.d, me)

	chat, err := h.getChat(ctx, chatID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, http.StatusNotFound, "会话不存在")
			return
		}
		log.Printf("[PrivateChat] 查询会话失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "标记已读失败")
		return
	}
	if !isParticipant(chat, me) {
		util.WriteError(w, http.StatusForbidden, "无权操作")
		return
	}

	where := `"chatId"=$1 AND "senderId"!=$2 AND "status"!='read'`
	args := []any{chatID, me}
	if len(body.MessageIDs) > 0 {
		where += ` AND "id" IN (` + inPlaceholders(len(body.MessageIDs), 3) + `)`
		args = append(args, stringsToAny(body.MessageIDs)...)
	}
	updated, err := h.d.DB.Exec(ctx,
		`UPDATE "PrivateMessage" SET "status"='read' WHERE `+where, args...)
	if err != nil {
		log.Printf("[PrivateChat] 标记已读失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "标记已读失败")
		return
	}
	util.WriteJSON(w, http.StatusOK, map[string]any{"updated": updated})
}

// POST /api/chat/:chatId/recall/:messageId — 撤回消息（仅发送者可撤回，2分钟内）。
func (h *Handler) recallMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r).Id
	chatID := r.PathValue("chatId")
	messageID := r.PathValue("messageId")

	msg, err := db.QueryRowToStruct[privateMessageRow](ctx, h.d.DB,
		`SELECT "id","chatId","senderId","msgType","content","replyToId","isRevoked","status",
		        "extra","burnAfterRead","burnReadAt","hmac","createdAt"
		 FROM "PrivateMessage" WHERE "id"=$1`, messageID)
	if err != nil || msg.ChatID != chatID {
		util.WriteError(w, http.StatusNotFound, "消息不存在")
		if err != nil && !db.IsNotFound(err) {
			log.Printf("[PrivateChat] 查询消息失败: %v", err)
		}
		return
	}
	if msg.SenderID != me {
		util.WriteError(w, http.StatusForbidden, "只能撤回自己的消息")
		return
	}

	// 2分钟内可撤回
	if time.Since(msg.CreatedAt) > 2*time.Minute {
		util.WriteError(w, http.StatusBadRequest, "超过2分钟无法撤回")
		return
	}

	if _, err := h.d.DB.Exec(ctx,
		`UPDATE "PrivateMessage" SET "isRevoked"=true WHERE "id"=$1`, messageID); err != nil {
		log.Printf("[PrivateChat] 撤回消息失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "撤回消息失败")
		return
	}
	util.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ============ 小工具 ============

var hmacRe = regexp.MustCompile(`(?i)^[a-f0-9]{64}$`)

var validBurnTimers = []int{5, 10, 30, 60, 300, 3600, 86400, 604800}

// jsonRaw 原样嵌入已缓存的 JSON（Cache-Aside 命中时直接回吐）。
type jsonRaw []byte

func (j jsonRaw) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

type countRow struct {
	Count int64 `db:"count"`
}

type cursorRow struct {
	CreatedAt time.Time `db:"createdAt"`
}

// inPlaceholders 生成 $start,$start+1,... 占位符串。
func inPlaceholders(n, start int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "$" + itoa(start+i)
	}
	return strings.Join(parts, ",")
}

func itoa(n int) string { return fmt.Sprint(n) }

func stringsToAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func dedupPeerIDs(chats []chatRow, me string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, c := range chats {
		pid := peerIDFor(&c, me)
		if _, ok := seen[pid]; !ok {
			seen[pid] = struct{}{}
			out = append(out, pid)
		}
	}
	return out
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid int")
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

func marshalExtra(extra map[string]any) (string, error) {
	b, err := json.Marshal(extra)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
