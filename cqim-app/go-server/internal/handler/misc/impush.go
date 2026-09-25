// impush.go — 跨节点 IM 推送与用户在线状态，移植自 server/publish-im.ts 与 server/redis.ts。
//
// 频道：cqim:im:push
// 消息格式：{ userId: string, payload: object | string }
//
// 在线状态：Redis 键 online:{userId}，TTL 90 秒（ONLINE_TTL）。
package misc

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// IMPushChannel 跨节点 IM 推送频道（与 publish-im.ts IM_PUSH_CHANNEL 一致）。
const IMPushChannel = "cqim:im:push"

// ImPushEnvelope Redis 信封格式 {userId, payload}。
type ImPushEnvelope struct {
	UserID  string `json:"userId"`
	Payload any    `json:"payload"`
}

// PublishImPush 发布跨节点 IM 推送（Redis PUBLISH cqim:im:push）。
// userID 为空时直接返回 nil；Redis 不可用或发布失败返回 error。
func PublishImPush(d *handler.Deps, userID string, payload any) error {
	if userID == "" {
		return nil
	}
	if d.Redis == nil {
		return errRedisUnavailable
	}
	envelope, err := json.Marshal(ImPushEnvelope{UserID: userID, Payload: payload})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return d.Redis.Publish(ctx, IMPushChannel, string(envelope))
}

const (
	onlinePrefix = "online:"        // redis.ts ONLINE_PREFIX
	onlineTTL    = 90 * time.Second // redis.ts ONLINE_TTL
)

// SetUserOnline 标记用户在线（SETEX 90 秒）；Redis 异常静默忽略（与 TS 一致）。
func SetUserOnline(d *handler.Deps, userID string) {
	if d.Redis == nil || userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = d.Redis.SetEX(ctx, onlinePrefix+userID, strconv.FormatInt(time.Now().UnixMilli(), 10), onlineTTL)
}

// RefreshUserOnline 心跳续期（EXPIRE 90 秒）；Redis 异常静默忽略。
func RefreshUserOnline(d *handler.Deps, userID string) {
	if d.Redis == nil || userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = d.Redis.Expire(ctx, onlinePrefix+userID, onlineTTL)
}

// SetUserOffline 标记用户离线（DEL）；Redis 异常静默忽略。
func SetUserOffline(d *handler.Deps, userID string) {
	if d.Redis == nil || userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = d.Redis.Del(ctx, onlinePrefix+userID)
}

// IsUserOnline 检查用户是否在线（EXISTS online:{userId}）；Redis 异常降级为 false。
func IsUserOnline(d *handler.Deps, userID string) bool {
	if d.Redis == nil || userID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n, err := d.Redis.RDB.Exists(ctx, onlinePrefix+userID).Result()
	return err == nil && n == 1
}
