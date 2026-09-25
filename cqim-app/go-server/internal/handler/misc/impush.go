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
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
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
	onlinePrefix = "online:"      // redis.ts ONLINE_PREFIX（保留做兼容读取）
	onlineTTL    = 90 * time.Second // redis.ts ONLINE_TTL

	// onlineConnsPrefix 按连接跟踪在线状态：ZSET online:conns:{userId}，score=最后心跳毫秒时间戳
	// 解决多连接/多节点互相踩：某连接断开时只移除自己的条目，集合为空才算离线。
	// 用 ZSET 而不用 SET：每个连接有独立时间戳，心跳时清理过期成员，避免异常断开的
	// 陈旧连接在其他连接持续续期时永久残留。
	onlineConnsPrefix = "online:conns:"
)

// nodeID 本节点标识（进程启动时生成，用于区分多节点连接）
var nodeID = func() string {
	h, _ := os.Hostname()
	return fmt.Sprintf("%s-%d", h, os.Getpid())
}()

// NodeID 返回本节点标识。
func NodeID() string { return nodeID }

// NewConnID 生成连接唯一标识（节点ID + 随机ID）
func NewConnID() string {
	return nodeID + ":" + util.NewID()
}

// SetUserOnline 标记用户在线（SETEX 90 秒）；Redis 异常静默忽略（与 TS 一致）。
//
// connID 为空时沿用旧语义（直接写 online:{userId}）；非空时写入连接集合。
func SetUserOnline(d *handler.Deps, userID string) {
	setUserOnlineConn(d, userID, "")
}

// SetUserOnlineConn 标记指定连接在线。
func SetUserOnlineConn(d *handler.Deps, userID, connID string) {
	setUserOnlineConn(d, userID, connID)
}

func setUserOnlineConn(d *handler.Deps, userID, connID string) {
	if d.Redis == nil || userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if connID == "" {
		_ = d.Redis.SetEX(ctx, onlinePrefix+userID, strconv.FormatInt(time.Now().UnixMilli(), 10), onlineTTL)
		return
	}
	now := time.Now().UnixMilli()
	_ = d.Redis.ZAdd(ctx, onlineConnsPrefix+userID, redis.Z{Score: float64(now), Member: connID})
	_ = d.Redis.Expire(ctx, onlineConnsPrefix+userID, onlineTTL)
	// 同步写旧 key，保持 IsUserOnline 的兼容读取
	_ = d.Redis.SetEX(ctx, onlinePrefix+userID, "1", onlineTTL)
}

// RefreshUserOnline 心跳续期（EXPIRE 90 秒）；Redis 异常静默忽略。
func RefreshUserOnline(d *handler.Deps, userID string) {
	refreshUserOnlineConn(d, userID, "")
}

// RefreshUserOnlineConn 为指定连接续期。
func RefreshUserOnlineConn(d *handler.Deps, userID, connID string) {
	refreshUserOnlineConn(d, userID, connID)
}

func refreshUserOnlineConn(d *handler.Deps, userID, connID string) {
	if d.Redis == nil || userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if connID == "" {
		_ = d.Redis.Expire(ctx, onlinePrefix+userID, onlineTTL)
		return
	}
	now := time.Now().UnixMilli()
	// 更新本连接时间戳（ZADD 同成员会更新 score）+ 清理过期成员 + 续期
	_ = d.Redis.ZAdd(ctx, onlineConnsPrefix+userID, redis.Z{Score: float64(now), Member: connID})
	_ = d.Redis.ZRemRangeByScore(ctx, onlineConnsPrefix+userID, "0", strconv.FormatInt(now-int64(onlineTTL/time.Millisecond), 10))
	_ = d.Redis.Expire(ctx, onlineConnsPrefix+userID, onlineTTL)
	_ = d.Redis.Expire(ctx, onlinePrefix+userID, onlineTTL)
}

// SetUserOffline 标记用户离线（DEL）；Redis 异常静默忽略。
func SetUserOffline(d *handler.Deps, userID string) {
	setUserOfflineConn(d, userID, "")
}

// SetUserOfflineConn 移除指定连接；集合为空时才删 online:{userId}。
func SetUserOfflineConn(d *handler.Deps, userID, connID string) {
	setUserOfflineConn(d, userID, connID)
}

func setUserOfflineConn(d *handler.Deps, userID, connID string) {
	if d.Redis == nil || userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if connID == "" {
		_ = d.Redis.Del(ctx, onlinePrefix+userID)
		return
	}
	_ = d.Redis.ZRem(ctx, onlineConnsPrefix+userID, connID)
	// 集合为空 → 用户真正离线，删旧 key
	n, _ := d.Redis.ZCard(ctx, onlineConnsPrefix+userID)
	if n == 0 {
		_ = d.Redis.Del(ctx, onlineConnsPrefix+userID, onlinePrefix+userID)
	}
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
