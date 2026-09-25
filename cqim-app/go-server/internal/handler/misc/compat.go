// compat.go — 跨模块小工具（对应 server/cos-signer.ts、redis.ts 部分逻辑）。
// 与 friend/channel/privatechat 包内的同名实现保持行为一致。
package misc

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// errRedisUnavailable Redis 不可用（仅 PublishImPush 等返回 error 的函数使用）。
var errRedisUnavailable = errors.New("redis unavailable")

const (
	unreadPrefix   = "unread:"    // redis.ts UNREAD_PREFIX（Hash unread:{userId}）
	convListPrefix = "conv:list:" // redis.ts CONV_LIST_PREFIX
	convListTTL    = 60 * time.Second
)

// getUnreadTotal 总未读数（redis.ts getUnreadCount(userId)）；Redis 异常降级为 0。
func getUnreadTotal(ctx context.Context, d *handler.Deps, userID string) int {
	if d.Redis == nil {
		return 0
	}
	v, err := d.Redis.RDB.HGet(ctx, unreadPrefix+userID, "total").Result()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}

// getUnreadCountOfChat 某会话未读数（redis.ts getUnreadCount(userId, chatId)）。
func getUnreadCountOfChat(ctx context.Context, d *handler.Deps, userID, chatID string) int {
	if d.Redis == nil {
		return 0
	}
	v, err := d.Redis.RDB.HGet(ctx, unreadPrefix+userID, chatID).Result()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}

// getCachedConversationList 取会话列表缓存（JSON 原文）；未命中/损坏返回 ok=false。
func getCachedConversationList(ctx context.Context, d *handler.Deps, userID string) (json.RawMessage, bool) {
	if d.Redis == nil {
		return nil, false
	}
	v, ok, err := d.Redis.GetString(ctx, convListPrefix+userID)
	if err != nil || !ok || v == "" || !json.Valid([]byte(v)) {
		return nil, false
	}
	return json.RawMessage(v), true
}

// setCachedConversationList 缓存会话列表 60s，失败静默忽略。
func setCachedConversationList(ctx context.Context, d *handler.Deps, userID string, list any) {
	if d.Redis == nil {
		return
	}
	b, err := json.Marshal(list)
	if err != nil {
		return
	}
	_ = d.Redis.SetEX(ctx, convListPrefix+userID, string(b), convListTTL)
}

// ============ avatarToProxy（对应 server/cos-signer.ts 纯函数部分） ============

func isCosURL(u string) bool {
	if u == "" || !strings.HasPrefix(u, "https://") {
		return false
	}
	return strings.Contains(u, ".cos.") || strings.Contains(u, ".myqcloud.com") || strings.Contains(u, "imim.chat")
}

var zhToASCIISeg = map[string]string{
	"头像":  "avatars",
	"群头像": "group-avatars",
	"朋友圈": "moments",
	"照片":  "photos",
	"视频":  "videos",
}

// cosKeyToAlias cosKey 中文段 → ASCII 别名（仅用于生成对外可见的 URL）。
func cosKeyToAlias(cosKey string) string {
	if cosKey == "" {
		return cosKey
	}
	segs := strings.Split(cosKey, "/")
	for i, s := range segs {
		if a, ok := zhToASCIISeg[s]; ok {
			segs[i] = a
		}
	}
	return strings.Join(segs, "/")
}

// avatarToProxy 将头像 COS 直链转换为代理 URL（附带缩略图处理参数），非 COS URL 原样返回。
func avatarToProxy(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	if !isCosURL(rawURL) {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	cosKey := strings.TrimPrefix(u.Path, "/")
	if decoded, err := url.PathUnescape(cosKey); err == nil {
		cosKey = decoded
	}
	aliasKey := cosKeyToAlias(cosKey)
	segs := strings.Split(aliasKey, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/api/cos/proxy/" + strings.Join(segs, "/") + "?imageMogr2/thumbnail/200x200/format/webp/quality/80"
}

// strOrNil 空字符串转 nil（用于 dialogId/username 等 "xxx || null" 语义）。
func strOrNil(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}
