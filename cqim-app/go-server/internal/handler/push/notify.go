// Package push — push-notify.ts 移植：离线推送主路径。
//
// 自建 APNs（P8）与 Web Push（VAPID）。推送 payload 只携带路由 ID，不携带消息明文。
// 导出函数：NotifyPrivateMessagePush / NotifyGroupMessagePush，供消息模块调用。
package push

import (
	"context"
	"log"
	"net/http"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// RegisterRoutes 注册全部推送路由（/api/apns 与 /api/web-push，与 Node 版挂载点一致）。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := &Handler{deps: d}
	mux.Handle("POST /api/apns/token", d.Auth.UserAuth(http.HandlerFunc(h.registerToken)))
	mux.Handle("POST /api/apns/voip-token", d.Auth.UserAuth(http.HandlerFunc(h.registerVoipToken)))
	mux.Handle("DELETE /api/apns/token", d.Auth.UserAuth(http.HandlerFunc(h.deleteToken)))

	mux.Handle("GET /api/web-push/public-key", http.HandlerFunc(h.webPushPublicKey)) // 公开：浏览器创建订阅前需要
	mux.Handle("POST /api/web-push/subscription", d.Auth.UserAuth(http.HandlerFunc(h.saveSubscription)))
	mux.Handle("DELETE /api/web-push/subscription", d.Auth.UserAuth(http.HandlerFunc(h.deleteSubscription)))
}

// Handler 推送模块 handler（apns.go / webpush.go 共用）。
type Handler struct {
	deps *handler.Deps
}

// ============================================================
// 参数类型（对应 TS PrivatePushParams / GroupPushParams）
// ============================================================

// PrivatePushParams 私聊推送参数。
type PrivatePushParams struct {
	ToUserID    string
	SenderID    string
	ChatID      string
	MessageID   string
	PreviewText string
}

// GroupPushParams 群聊推送参数。
type GroupPushParams struct {
	ToUserID    string
	GroupID     string
	SenderID    string
	SenderName  string
	PreviewText string
}

// ============================================================
// shouldSkipApns（与 server/presence.ts 同语义）
// ============================================================

// shouldSkipApns 与 presence.ts 的 shouldSkipApns 语义一致：
// 用户前台在线且正停留在该会话时跳过推送。
// 注意：presence 模块移植后应由其统一提供，此处为推送路径的本地实现。
func shouldSkipApns(ctx context.Context, d *handler.Deps, userID, chatID string) bool {
	state, ok, err := d.Redis.GetString(ctx, "user:presence:"+userID)
	if err != nil || !ok || (state != "foreground" && state != "background") {
		return false // offline 或读取失败：保守按 offline 处理，不跳过
	}
	if state != "foreground" {
		return false
	}
	if chatID == "" {
		return true
	}
	active, ok, err := d.Redis.GetString(ctx, "user:activeChat:"+userID)
	if err != nil || !ok || active == "" {
		return true
	}
	return active == chatID
}

type senderNameRow struct {
	Nickname *string `db:"nickname"`
	Username string  `db:"username"`
}

// loadConversationName 取发送者昵称/用户名，兜底"有人"（与 TS 版一致）。
func loadConversationName(ctx context.Context, d *handler.Deps, senderID string) string {
	u, err := db.QueryRowToStruct[senderNameRow](ctx, d.DB,
		`SELECT "nickname","username" FROM "User" WHERE "id"=$1`, senderID)
	if err != nil {
		return "有人"
	}
	if u.Nickname != nil && *u.Nickname != "" {
		return *u.Nickname
	}
	if u.Username != "" {
		return u.Username
	}
	return "有人"
}

// ============================================================
// 导出函数
// ============================================================

// NotifyPrivateMessagePush 私聊离线推送主路径：自建 APNs + Web Push。
// 推送异常只记日志，不向调用方抛错（与 TS 版 .catch 一致）。
func NotifyPrivateMessagePush(d *handler.Deps, p PrivatePushParams) {
	ctx := context.Background()
	if shouldSkipApns(ctx, d, p.ToUserID, p.ChatID) {
		return
	}

	name := loadConversationName(ctx, d, p.SenderID)
	if _, err := SendAPNsPush(d, APNsPushPayload{
		ToUserID:   p.ToUserID,
		Title:      "新消息",
		Body:       name,
		CustomData: map[string]any{"chatId": p.ChatID, "senderId": p.SenderID},
	}); err != nil {
		log.Printf("[APNs] 私聊推送异常: %v", err)
	}

	if _, err := SendWebPush(d, WebPushMessageParams{
		ToUserID:  p.ToUserID,
		ChatID:    p.ChatID,
		MessageID: p.MessageID,
		SenderID:  p.SenderID,
	}); err != nil {
		log.Printf("[WebPush] 私聊推送异常: %v", err)
	}
}

// NotifyGroupMessagePush 群聊离线推送主路径：自建 APNs + Web Push。
func NotifyGroupMessagePush(d *handler.Deps, p GroupPushParams) {
	ctx := context.Background()
	if shouldSkipApns(ctx, d, p.ToUserID, "") {
		return
	}

	body := p.SenderName
	if body == "" {
		body = "群聊消息"
	}
	if _, err := SendAPNsPush(d, APNsPushPayload{
		ToUserID:   p.ToUserID,
		Title:      "新消息",
		Body:       body,
		CustomData: map[string]any{"groupId": p.GroupID, "senderId": p.SenderID, "type": "group_message"},
	}); err != nil {
		log.Printf("[APNs] 群聊推送异常: %v", err)
	}

	if _, err := SendWebPush(d, WebPushMessageParams{
		ToUserID: p.ToUserID,
		ChatID:   p.GroupID,
		SenderID: p.SenderID,
	}); err != nil {
		log.Printf("[WebPush] 群聊推送异常: %v", err)
	}
}
