package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
)

// ============ 通用小工具（对应 index.ts 顶部 helpers） ============

// safeAvatarUrl 与 TS 版一致：只允许 /api/media/、站内相对路径、非 COS 的 https 外链，
// 其余返回空字符串让客户端使用头像占位。
func safeAvatarUrl(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/api/media/") || strings.HasPrefix(raw, "/") {
		return raw
	}
	if strings.HasPrefix(raw, "https://") && !strings.Contains(raw, ".cos.") && !strings.Contains(raw, ".myqcloud.com") {
		return raw
	}
	return ""
}

// getSystemConfig 读 SystemConfig 表的 JSON 值（对应 admin.ts getConfig）。
// 解析失败返回 nil。
func (h *Handler) getSystemConfig(ctx context.Context, key string) map[string]any {
	var raw string
	err := h.d.DB.Pool.QueryRow(ctx, `SELECT "value" FROM "SystemConfig" WHERE "key"=$1`, key).Scan(&raw)
	if err != nil || raw == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}

func cfgStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// publicBaseURL 对应 public-url.ts：PUBLIC_BASE_URL，默认 https://cq.je。
func publicBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://cq.je"
}

func publicURL(p string) string {
	base := publicBaseURL()
	if p == "" {
		return base
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

// escHTML 转义 HTML（对应 index.ts 的 escHtml）。
var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&#39;",
)

func escHTML(s string) string { return htmlEscaper.Replace(s) }

// realIP 按中间件约定取客户端 IP。
func (h *Handler) realIP(r *http.Request) string {
	return middleware.GetClientIP(r, h.d.Cfg.TrustProxy)
}

// ============ 用户昵称注册表（对应 index.ts userRegistry） ============
// profile 读写链中用作 dbNickname 兜底。

var (
	userRegistry   = map[string]string{}
	userRegistryMu sync.RWMutex
)

func init() {
	userRegistry["me"] = "清风"
	userRegistry["u1"] = "林小溪"
	userRegistry["u2"] = "陈墨白"
	userRegistry["u3"] = "苏清和"
	userRegistry["u4"] = "王竹韵"
	userRegistry["u5"] = "周清漪"
	userRegistry["u6"] = "李晨曦"
	userRegistry["BOT"] = "imim AI"
	userRegistry["official"] = "imim 官方"
}

func registryNickname(userID string) string {
	userRegistryMu.RLock()
	defer userRegistryMu.RUnlock()
	return userRegistry[userID]
}

func setRegistryNickname(userID, nickname string) {
	if nickname == "" {
		return
	}
	userRegistryMu.Lock()
	defer userRegistryMu.Unlock()
	userRegistry[userID] = nickname
}

// ============ presence Redis 键（对应 presence.ts / presence-rules.ts） ============

const (
	presenceTTL       = 90 * time.Second
	presenceKeyPrefix = "user:presence:"
	activeChatPrefix  = "user:activeChat:"
)

// shouldSkipApnsFromState 纯规则：仅 foreground 跳过 APNs（对应 presence-rules.ts）。
func shouldSkipApnsFromState(state string) bool { return state == "foreground" }

func (h *Handler) redisGetPresence(ctx context.Context, userID string) string {
	if h.d.Redis == nil {
		return "offline"
	}
	raw, found, err := h.d.Redis.GetString(ctx, presenceKeyPrefix+userID)
	if err != nil || !found {
		// TS 版 Redis 异常时保守按 offline 处理
		return "offline"
	}
	if raw == "foreground" || raw == "background" {
		return raw
	}
	return "offline"
}

func (h *Handler) redisGetActiveChatID(ctx context.Context, userID string) *string {
	if h.d.Redis == nil {
		return nil
	}
	raw, found, err := h.d.Redis.GetString(ctx, activeChatPrefix+userID)
	if err != nil || !found || raw == "" {
		return nil
	}
	return &raw
}

func (h *Handler) checkRedisHealth(ctx context.Context) bool {
	if h.d.Redis == nil {
		return false
	}
	return h.d.Redis.Ping(ctx) == nil
}

// ============ 在线状态（对应 redis.ts isUserOnline/getUserDevices/getUserLastSeen） ============

type deviceInfo struct {
	DeviceType string `json:"deviceType,omitempty"`
	Browser    string `json:"browser,omitempty"`
	OS         string `json:"os,omitempty"`
	IP         string `json:"ip,omitempty"`
}

func (h *Handler) isUserOnline(ctx context.Context, userID string) bool {
	if h.d.Redis == nil {
		return false
	}
	n, err := h.d.Redis.RDB.Exists(ctx, "online:"+userID).Result()
	return err == nil && n == 1
}

func (h *Handler) getUserDevices(ctx context.Context, userID string) []deviceInfo {
	out := []deviceInfo{}
	if h.d.Redis == nil {
		return out
	}
	vals, err := h.d.Redis.RDB.HGetAll(ctx, "devices:"+userID).Result()
	if err != nil {
		return out
	}
	for _, raw := range vals {
		var d deviceInfo
		if json.Unmarshal([]byte(raw), &d) == nil {
			out = append(out, d)
		}
	}
	return out
}

// getUserLastSeen 返回最后在线时间戳（毫秒），无记录返回 nil。
func (h *Handler) getUserLastSeen(ctx context.Context, userID string) *int64 {
	if h.d.Redis == nil {
		return nil
	}
	raw, found, err := h.d.Redis.GetString(ctx, "last_seen:"+userID)
	if err != nil || !found || raw == "" {
		return nil
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return nil
	}
	return &ts
}
