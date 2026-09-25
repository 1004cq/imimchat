// presence.go — 在线状态，移植自 server/presence.ts + server/presence-rules.ts，
// 路由来自 server/index.ts 的 /api/presence。
//
// GET  /api/presence（需要登录）→ {state, activeChatId, skipApns, redisAvailable}
// POST /api/presence（需要登录）Body: {state: "foreground"|"background"|"offline", activeChatId?: string|null}
//
//	→ {success: true, redisAvailable: true}
//
// Redis 键：user:presence:{userId}、user:activeChat:{userId}，TTL 90 秒。
package misc

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

const (
	presenceKeyPrefix   = "user:presence:"
	activeChatKeyPrefix = "user:activeChat:"
	presenceTTL         = 90 * time.Second // PRESENCE_TTL_SECONDS
)

// PresenceState 在线状态取值。
type PresenceState = string

// 在线状态取值（presence-rules.ts PRESENCE_STATES）。
const (
	PresenceForeground PresenceState = "foreground"
	PresenceBackground PresenceState = "background"
	PresenceOffline    PresenceState = "offline"
)

func isPresenceState(s string) bool {
	return s == PresenceForeground || s == PresenceBackground || s == PresenceOffline
}

// SetPresence 写在线状态（对应 presence.ts setPresence）。
// activeChatID: nil=字段未传（保持原值）；指向空串=清空；指向非空=设置。
func SetPresence(ctx context.Context, d *handler.Deps, userID, state string, activeChatID *string) error {
	if d.Redis == nil {
		return errRedisUnavailable
	}
	if state == PresenceOffline {
		return d.Redis.Del(ctx, presenceKeyPrefix+userID, activeChatKeyPrefix+userID)
	}
	if err := d.Redis.SetEX(ctx, presenceKeyPrefix+userID, state, presenceTTL); err != nil {
		return err
	}
	if activeChatID != nil {
		if *activeChatID != "" {
			return d.Redis.SetEX(ctx, activeChatKeyPrefix+userID, *activeChatID, presenceTTL)
		}
		return d.Redis.Del(ctx, activeChatKeyPrefix+userID)
	}
	return nil
}

// GetPresence 读在线状态；Redis 异常或值非法时保守按 offline 处理。
func GetPresence(ctx context.Context, d *handler.Deps, userID string) PresenceState {
	if d.Redis == nil {
		return PresenceOffline
	}
	raw, ok, err := d.Redis.GetString(ctx, presenceKeyPrefix+userID)
	if err != nil {
		log.Printf("[presence] Redis 读取状态失败 userId=%s，保守按 offline 处理: %v", userID, err)
		return PresenceOffline
	}
	if !ok || !isPresenceState(raw) {
		return PresenceOffline
	}
	return raw
}

// GetActiveChatID 读当前前台会话；无记录返回 ""。
func GetActiveChatID(ctx context.Context, d *handler.Deps, userID string) string {
	if d.Redis == nil {
		return ""
	}
	raw, ok, err := d.Redis.GetString(ctx, activeChatKeyPrefix+userID)
	if err != nil {
		log.Printf("[presence] Redis 读取 activeChatId 失败 userId=%s: %v", userID, err)
		return ""
	}
	if !ok {
		return ""
	}
	return raw
}

// ShouldSkipApnsFromState 纯规则：仅 foreground 跳过 APNs（presence-rules.ts）。
func ShouldSkipApnsFromState(state, chatID, activeChatID string) bool {
	if state != PresenceForeground {
		return false
	}
	if chatID == "" {
		return true
	}
	if activeChatID == "" {
		return true
	}
	return activeChatID == chatID
}

// ShouldSkipApns 是否跳过 APNs。仅前台才跳过；有 WS 在线不算免推。
func ShouldSkipApns(ctx context.Context, d *handler.Deps, userID, chatID string) bool {
	if GetPresence(ctx, d, userID) != PresenceForeground {
		return false
	}
	if chatID == "" {
		return true
	}
	return ShouldSkipApnsFromState(PresenceForeground, chatID, GetActiveChatID(ctx, d, userID))
}

// checkRedisAvailable 对应 index.ts 的 checkRedisHealth。
func checkRedisAvailable(ctx context.Context, d *handler.Deps) bool {
	if d.Redis == nil {
		return false
	}
	return d.Redis.Ping(ctx) == nil
}

// presenceSetBody 用 RawMessage 区分 activeChatId 的 undefined / null / "" 语义。
type presenceSetBody struct {
	State        string          `json:"state"`
	ActiveChatID json.RawMessage `json:"activeChatId"`
}

func (h *Handler) setPresenceHTTP(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFrom(r)

	var body presenceSetBody
	if !util.DecodeJSON(w, r, &body) {
		return
	}
	if !isPresenceState(body.State) {
		util.WriteError(w, 400, "无效的 state")
		return
	}
	// TS 语义：非空字符串→设置；null 或 ''→清空；未传→保持原值
	var activeChatID *string
	if len(body.ActiveChatID) > 0 {
		var s *string
		if err := json.Unmarshal(body.ActiveChatID, &s); err == nil {
			if s == nil || *s == "" {
				empty := ""
				activeChatID = &empty
			} else {
				activeChatID = s
			}
		}
	}
	if err := SetPresence(r.Context(), h.deps, user.Id, body.State, activeChatID); err != nil {
		log.Printf("[presence] 设置失败: %v", err)
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "redisAvailable": true})
}

func (h *Handler) getPresenceHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.UserFrom(r)

	state := GetPresence(ctx, h.deps, user.Id)
	activeChatID := GetActiveChatID(ctx, h.deps, user.Id)
	var active any
	if activeChatID != "" {
		active = activeChatID
	}
	util.WriteJSON(w, 200, map[string]any{
		"state":          state,
		"activeChatId":   active,
		"skipApns":       ShouldSkipApnsFromState(state, "", activeChatID),
		"redisAvailable": checkRedisAvailable(ctx, h.deps),
	})
}
