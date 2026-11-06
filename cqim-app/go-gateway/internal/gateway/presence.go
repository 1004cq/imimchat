package gateway

import (
	"context"
	"encoding/json"
	"log"
	"time"
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

// Presence changes are atomic and conditional on ownership. Caller serializes
// local connection replacement/cleanup with Gateway.mu.
func (gw *Gateway) setOnline(userID string) {
	ctx, cancel := context.WithTimeout(gw.ctx, 2*time.Second)
	defer cancel()
	err := gw.rdb.Eval(ctx, `redis.call('SET',KEYS[1],ARGV[1],'EX',ARGV[3]);redis.call('SADD',KEYS[2],ARGV[2]);return 1`, []string{userOnlineKey(userID), gwUsersKey(gw.id)}, gw.id, userID, int(onlineTTL.Seconds())).Err()
	if err != nil {
		log.Printf("[Gateway] setOnline failed: %v", err)
	}
}
func (gw *Gateway) renewOnline(userID string) {
	ctx, cancel := context.WithTimeout(gw.ctx, 2*time.Second)
	defer cancel()
	// A heartbeat from an older node cannot steal ownership from a newer node.
	err := gw.rdb.Eval(ctx, `local owner=redis.call('GET',KEYS[1]);if not owner or owner==ARGV[1] then redis.call('SET',KEYS[1],ARGV[1],'EX',ARGV[3]);redis.call('SADD',KEYS[2],ARGV[2]);return 1 end;return 0`, []string{userOnlineKey(userID), gwUsersKey(gw.id)}, gw.id, userID, int(onlineTTL.Seconds())).Err()
	if err != nil {
		log.Printf("[Gateway] renewOnline failed: %v", err)
	}
}
func (gw *Gateway) clearOnline(userID string) { gw.clearOnlineGroups(userID, nil) }
func (gw *Gateway) clearOnlineGroups(userID string, groups []string) {
	ctx, cancel := context.WithTimeout(gw.ctx, 2*time.Second)
	defer cancel()
	gw.clearOwnedPresence(ctx, userID, groups)
}
func (gw *Gateway) clearOwnedPresence(ctx context.Context, userID string, groups []string) {
	keys := []string{userOnlineKey(userID), gwUsersKey(gw.id)}
	for _, group := range groups {
		keys = append(keys, "group:online:"+group)
	}
	err := gw.rdb.Eval(ctx, `local owner=redis.call('GET',KEYS[1]);if not owner or owner==ARGV[1] then redis.call('DEL',KEYS[1]);for i=3,#KEYS do redis.call('SREM',KEYS[i],ARGV[2]) end end;redis.call('SREM',KEYS[2],ARGV[2]);return 1`, keys, gw.id, userID).Err()
	if err != nil {
		log.Printf("[Gateway] clearOnline failed: %v", err)
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
	clients := make([]*Client, 0, len(gw.clients))
	for _, c := range gw.clients {
		clients = append(clients, c)
	}
	gw.mu.RUnlock()

	for _, c := range clients {
		gw.refreshClient(c)
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

	for _, uid := range members {
		gw.clearOwnedPresence(ctx, uid, nil)
	}
	if err := gw.rdb.Del(ctx, gwAliveKey(gw.id)).Err(); err != nil {
		log.Printf("[Gateway] shutdown presence failed: %v", err)
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
func (gw *Gateway) StartPushSubscriber(ctx context.Context) error {
	pubsub := gw.rdb.Subscribe(ctx, imPushChannel)
	readyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	_, err := pubsub.Receive(readyCtx)
	cancel()
	if err != nil {
		pubsub.Close()
		return err
	}
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
				var event struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(payload, &event) == nil && event.Type == "session_revoked" {
					gw.revalidateUser(envelope.UserID)
					continue
				}
				gw.PushToUser(envelope.UserID, payload)
			}
		}
	}()
	return nil
}
