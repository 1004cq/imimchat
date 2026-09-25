// 缓存与推送 helper，移植自 server/redis.ts 与 server/publish-im.ts 中私聊用到的部分。
package privatechat

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// IM_PUSH_CHANNEL 跨节点 IM 推送频道（与 Node 端 publish-im.ts 一致）。
const imPushChannel = "cqim:im:push"

const (
	onlinePrefix    = "online:"      // redis.ts ONLINE_PREFIX
	legacyOnlineKey = "user:online:" // redis.ts 中 isUserOnline 回退检查的旧 key
	unreadPrefix    = "unread:"      // redis.ts UNREAD_PREFIX（Hash unread:{userId}）
	convListPrefix  = "conv:list:"   // redis.ts CONV_LIST_PREFIX
	convListTTL     = 60 * time.Second
	unreadHashTTL   = 7 * 24 * time.Hour
)

// isUserOnline 检查用户是否在线（EXISTS online:{userId}）。
func isUserOnline(ctx context.Context, d *handler.Deps, userID string) bool {
	n, err := d.Redis.RDB.Exists(ctx, onlinePrefix+userID).Result()
	return err == nil && n == 1
}

// hasLegacyOnlineFlag 兼容旧 key user:online:{userId}（redis.ts 中 createPrivateMessageAndNotify 的回退检查）。
func hasLegacyOnlineFlag(ctx context.Context, d *handler.Deps, userID string) bool {
	_, ok, err := d.Redis.GetString(ctx, legacyOnlineKey+userID)
	return err == nil && ok
}

// getUnreadCount 获取某会话未读数；Redis 异常时降级返回 0（由数据库兜底）。
func getUnreadCount(ctx context.Context, d *handler.Deps, userID, chatID string) int {
	v, err := d.Redis.RDB.HGet(ctx, unreadPrefix+userID, chatID).Result()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}

// incrUnreadCount 原子增加未读数（同时更新分会话与总未读），失败静默忽略。
func incrUnreadCount(ctx context.Context, d *handler.Deps, userID, chatID string, delta int) {
	pipe := d.Redis.RDB.Pipeline()
	pipe.HIncrBy(ctx, unreadPrefix+userID, chatID, int64(delta))
	pipe.HIncrBy(ctx, unreadPrefix+userID, "total", int64(delta))
	pipe.Expire(ctx, unreadPrefix+userID, unreadHashTTL)
	_, _ = pipe.Exec(ctx)
}

// clearUnreadCount 进入会话清空未读数（原子归零）。
func clearUnreadCount(ctx context.Context, d *handler.Deps, userID, chatID string) {
	cur := getUnreadCount(ctx, d, userID, chatID)
	if cur > 0 {
		pipe := d.Redis.RDB.Pipeline()
		pipe.HSet(ctx, unreadPrefix+userID, chatID, "0")
		pipe.HIncrBy(ctx, unreadPrefix+userID, "total", int64(-cur))
		_, _ = pipe.Exec(ctx)
	}
}

// getCachedConversationList 取会话列表缓存，命中返回原始 JSON。
func getCachedConversationList(ctx context.Context, d *handler.Deps, userID string) ([]byte, bool) {
	v, ok, err := d.Redis.GetString(ctx, convListPrefix+userID)
	if err != nil || !ok || v == "" {
		return nil, false
	}
	return []byte(v), true
}

// setCachedConversationList 缓存会话列表 60s，失败静默忽略。
func setCachedConversationList(ctx context.Context, d *handler.Deps, userID string, list any) {
	b, err := json.Marshal(list)
	if err != nil {
		return
	}
	_ = d.Redis.SetEX(ctx, convListPrefix+userID, string(b), convListTTL)
}

// invalidateConversationList 删除会话列表缓存，失败静默忽略。
func invalidateConversationList(ctx context.Context, d *handler.Deps, userID string) {
	_ = d.Redis.Del(ctx, convListPrefix+userID)
}

// publishImPush 发布跨节点 IM 推送；失败仅打日志，不抛错（与 Node 端一致）。
func publishImPush(ctx context.Context, d *handler.Deps, userID string, payload map[string]any) {
	if userID == "" {
		return
	}
	envelope, err := json.Marshal(map[string]any{"userId": userID, "payload": payload})
	if err != nil {
		log.Printf("[IM Push] 信封序列化失败 userId=%s: %v", userID, err)
		return
	}
	if err := d.Redis.Publish(ctx, imPushChannel, string(envelope)); err != nil {
		log.Printf("[IM Push] PUBLISH 失败 userId=%s: %v", userID, err)
	}
}
