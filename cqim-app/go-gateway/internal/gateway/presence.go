package gateway

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	onlineTTL             = 90 * time.Second
	presenceRenewInterval = 30 * time.Second
	gwAliveTTL            = 30 * time.Second
	imPushChannel         = "cqim:im:push"
)

func userOnlineKey(userID string) string {
	return "user:online:" + userID
}

func gwUsersKey(gatewayID string) string {
	return "gw:users:" + gatewayID
}

func gwAliveKey(gatewayID string) string {
	return "gw:alive:" + gatewayID
}

// setOnline 标记用户在本 Gateway 在线
func (gw *Gateway) setOnline(userID string) {
	pipe := gw.rdb.Pipeline()
	pipe.Set(gw.ctx, userOnlineKey(userID), gw.id, onlineTTL)
	pipe.SAdd(gw.ctx, gwUsersKey(gw.id), userID)
	if _, err := pipe.Exec(gw.ctx); err != nil {
		log.Printf("[Gateway] setOnline 失败 user=%s: %v", userID, err)
	}
}

// renewOnline 续期在线状态；若 key 不属于本 Gateway 则重新抢占
func (gw *Gateway) renewOnline(userID string) {
	val, err := gw.rdb.Get(gw.ctx, userOnlineKey(userID)).Result()
	if err == redis.Nil || val != gw.id {
		gw.setOnline(userID)
		return
	}
	if err := gw.rdb.Expire(gw.ctx, userOnlineKey(userID), onlineTTL).Err(); err != nil {
		log.Printf("[Gateway] renewOnline 失败 user=%s: %v", userID, err)
	}
}

// clearOnline 仅当 user:online 值等于本 Gateway ID 时删除
func (gw *Gateway) clearOnline(userID string) {
	val, err := gw.rdb.Get(gw.ctx, userOnlineKey(userID)).Result()
	if err != nil || val != gw.id {
		return
	}
	pipe := gw.rdb.Pipeline()
	pipe.Del(gw.ctx, userOnlineKey(userID))
	pipe.SRem(gw.ctx, gwUsersKey(gw.id), userID)
	if _, err := pipe.Exec(gw.ctx); err != nil {
		log.Printf("[Gateway] clearOnline 失败 user=%s: %v", userID, err)
	}
}

// StartPresenceLoop 定期续期本机在线用户与 gw:alive 心跳
func (gw *Gateway) StartPresenceLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(presenceRenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				gw.renewAllPresence()
			}
		}
	}()
}

func (gw *Gateway) renewAllPresence() {
	gw.mu.RLock()
	userIDs := make([]string, 0, len(gw.clients))
	for uid := range gw.clients {
		userIDs = append(userIDs, uid)
	}
	gw.mu.RUnlock()

	for _, uid := range userIDs {
		gw.renewOnline(uid)
	}

	if err := gw.rdb.Set(gw.ctx, gwAliveKey(gw.id), "1", gwAliveTTL).Err(); err != nil {
		log.Printf("[Gateway] gw:alive 续期失败: %v", err)
	}
}

// ShutdownPresence SIGTERM 时清理本机集合内仍归属本 Gateway 的在线 key
func (gw *Gateway) ShutdownPresence(ctx context.Context) {
	members, err := gw.rdb.SMembers(ctx, gwUsersKey(gw.id)).Result()
	if err != nil {
		log.Printf("[Gateway] ShutdownPresence 读取用户集合失败: %v", err)
		return
	}

	pipe := gw.rdb.Pipeline()
	for _, uid := range members {
		val, getErr := gw.rdb.Get(ctx, userOnlineKey(uid)).Result()
		if getErr == nil && val == gw.id {
			pipe.Del(ctx, userOnlineKey(uid))
		}
		pipe.SRem(ctx, gwUsersKey(gw.id), uid)
	}
	pipe.Del(ctx, gwAliveKey(gw.id))
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[Gateway] ShutdownPresence 清理失败: %v", err)
	}
}

// imPushEnvelope Redis Pub/Sub 消息格式
type imPushEnvelope struct {
	UserID  string          `json:"userId"`
	Payload json.RawMessage `json:"payload"`
}

// PublishImPush 向 Redis 发布跨节点 IM 推送
func (gw *Gateway) PublishImPush(userID string, payload []byte) error {
	envelope, err := json.Marshal(imPushEnvelope{
		UserID:  userID,
		Payload: json.RawMessage(payload),
	})
	if err != nil {
		return err
	}
	return gw.rdb.Publish(gw.ctx, imPushChannel, envelope).Err()
}

// StartPushSubscriber 订阅 cqim:im:push，投递到本机 WebSocket 连接
func (gw *Gateway) StartPushSubscriber(ctx context.Context) {
	pubsub := gw.rdb.Subscribe(ctx, imPushChannel)
	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				_ = pubsub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var envelope imPushEnvelope
				if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
					log.Printf("[Gateway] Pub/Sub 消息解析失败: %v", err)
					continue
				}
				if envelope.UserID == "" || len(envelope.Payload) == 0 {
					continue
				}
				payload := envelope.Payload
				// payload 可能是 JSON 字符串，解包为原始对象字节
				if len(payload) > 0 && payload[0] == '"' {
					var s string
					if err := json.Unmarshal(payload, &s); err == nil {
						payload = []byte(s)
					}
				}
				gw.PushToUser(envelope.UserID, payload)
			}
		}
	}()
}
