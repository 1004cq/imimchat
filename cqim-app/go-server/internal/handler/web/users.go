package web

import (
	"net/http"
	"strings"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============ 用户搜索 / 公开信息（对应 index.ts /api/users/*） ============

// GET /api/users/search?q= — 通过用户ID、手机号、邮箱精确搜索用户（添加好友用）。
func (h *Handler) userSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	keyword := strings.TrimSpace(q)
	if keyword == "" {
		util.WriteError(w, 400, "请输入至少1个字符")
		return
	}
	type row struct {
		Id            string  `db:"id"`
		Username      string  `db:"username"`
		Nickname      *string `db:"nickname"`
		Avatar        *string `db:"avatar"`
		Bio           *string `db:"bio"`
		BackgroundUrl *string `db:"backgroundUrl"`
	}
	users, err := db.QueryToStructs[row](r.Context(), h.d.DB,
		`SELECT "id","username","nickname","avatar","bio","backgroundUrl" FROM "User"
		 WHERE "isBanned"=false AND ("id"=$1 OR "username"=$1 OR "phone"=$1 OR "email"=$1)
		 LIMIT 10`, keyword)
	if err != nil {
		util.WriteError(w, 500, "搜索失败")
		return
	}
	out := []map[string]any{}
	for _, u := range users {
		nickname := u.Username
		if u.Nickname != nil && *u.Nickname != "" {
			nickname = *u.Nickname
		}
		bio := ""
		if u.Bio != nil {
			bio = *u.Bio
		}
		avatar := ""
		if u.Avatar != nil {
			avatar = *u.Avatar
		}
		out = append(out, map[string]any{
			"id": u.Id, "username": u.Username, "nickname": nickname,
			"avatar": safeAvatarUrl(avatar), "bio": bio,
		})
	}
	util.WriteJSON(w, 200, map[string]any{"users": out})
}

// GET /api/users/{userId} — 通过 ID 获取用户公开信息。
func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	ctx := r.Context()
	if userID == "official" {
		util.WriteJSON(w, 200, map[string]any{
			"id": "official", "username": "admin", "nickname": "imim 官方",
			"avatar": "/imim-official-avatar.jpg", "bio": "imim 官方账号",
			"online": true, "lastSeen": nil,
			"devices": []map[string]any{{"deviceType": "server", "browser": "System", "os": "imim Cloud"}},
		})
		return
	}
	if userID == "BOT" {
		util.WriteJSON(w, 200, map[string]any{
			"id": "BOT", "username": "BOT", "nickname": "imim AI",
			"avatar": "/imim-ai-avatar.jpg", "bio": "imim AI 助手",
			"online": true, "lastSeen": nil,
			"devices": []map[string]any{{"deviceType": "server", "browser": "AI Runtime", "os": "imim Cloud"}},
		})
		return
	}
	type row struct {
		Id            string  `db:"id"`
		Username      string  `db:"username"`
		Nickname      *string `db:"nickname"`
		Avatar        *string `db:"avatar"`
		Bio           *string `db:"bio"`
		BackgroundUrl *string `db:"backgroundUrl"`
	}
	u, err := db.QueryRowToStruct[row](ctx, h.d.DB,
		`SELECT "id","username","nickname","avatar","bio","backgroundUrl" FROM "User" WHERE "id"=$1`, userID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "用户不存在")
		} else {
			util.WriteError(w, 500, "服务器错误")
		}
		return
	}
	// 附带在线状态和最后在线时间
	online := h.isUserOnline(ctx, userID)
	var lastSeen *int64
	var devices []deviceInfo
	if online {
		devices = h.getUserDevices(ctx, userID)
	} else {
		lastSeen = h.getUserLastSeen(ctx, userID)
		devices = []deviceInfo{}
	}
	nickname := u.Username
	if u.Nickname != nil && *u.Nickname != "" {
		nickname = *u.Nickname
	}
	util.WriteJSON(w, 200, map[string]any{
		"id": u.Id, "username": u.Username, "nickname": nickname,
		"avatar":        safeAvatarUrl(strVal(u.Avatar)),
		"bio":           strVal(u.Bio),
		"backgroundUrl": strVal(u.BackgroundUrl),
		"online":        online,
		"lastSeen":      lastSeen,
		"devices":       devices,
	})
}

// GET /api/users/{userId}/presence — 获取用户在线状态、设备信息、最后在线时间。
func (h *Handler) userPresence(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	ctx := r.Context()
	if userID == "official" {
		util.WriteJSON(w, 200, map[string]any{
			"userId": userID, "online": true, "lastSeen": nil,
			"devices": []map[string]any{{"deviceType": "server", "browser": "System", "os": "imim Cloud"}},
		})
		return
	}
	if userID == "BOT" {
		util.WriteJSON(w, 200, map[string]any{
			"userId": userID, "online": true, "lastSeen": nil,
			"devices": []map[string]any{{"deviceType": "server", "browser": "AI Runtime", "os": "imim Cloud"}},
		})
		return
	}
	online := h.isUserOnline(ctx, userID)
	var lastSeen *int64
	var devices []deviceInfo
	if online {
		devices = h.getUserDevices(ctx, userID)
	} else {
		lastSeen = h.getUserLastSeen(ctx, userID)
		devices = []deviceInfo{}
	}
	util.WriteJSON(w, 200, map[string]any{
		"userId": userID, "online": online, "lastSeen": lastSeen, "devices": devices,
	})
}
