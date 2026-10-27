package gateway

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
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
	recentPushTTL         = 30 * time.Second
)

// OnlineRecord is stored at user:online:{userId}.
// Legacy plain-string values (just the node id) are still accepted when reading.
type OnlineRecord struct {
	NodeID string `json:"nodeId"`
	ConnID string `json:"connId"`
	TS     int64  `json:"ts"`
}

func userOnlineKey(userID string) string {
	return "user:online:" + userID
}

func gwUsersKey(gatewayID string) string {
	return "gw:users:" + gatewayID
}

func gwAliveKey(gatewayID string) string {
	return "gw:alive:" + gatewayID
}

func nodePushChannel(nodeID string) string {
	return imPushChannel + ":" + nodeID
}

func recentPushKey(userID, msgID string) string {
	sum := sha1.Sum([]byte(userID + ":" + msgID))
	return "cqim:im:push:seen:" + hex.EncodeToString(sum[:])
}

func (gw *Gateway) encodeOnline(userID, connID string) string {
	rec := OnlineRecord{
		NodeID: gw.id,
		ConnID: connID,
		TS:     time.Now().UnixMilli(),
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return gw.id
	}
	return string(b)
}

func parseOnlineRecord(raw string) (OnlineRecord, bool) {
	raw = trimSpace(raw)
	if raw == "" {
		return OnlineRecord{}, false
	}
	if raw[0] == '{' {
		var rec OnlineRecord
		if err := json.Unmarshal([]byte(raw), &rec); err != nil || rec.NodeID == "" {
			return OnlineRecord{}, false
		}
		return rec, true
	}
	// Legacy: value was just the gateway / node id.
	return OnlineRecord{NodeID: raw}, true
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// LookupOnline returns the Redis routing record for a user, if any.
func (gw *Gateway) LookupOnline(userID string) (OnlineRecord, bool) {
	raw, err := gw.rdb.Get(gw.ctx, userOnlineKey(userID)).Result()
	if err == redis.Nil || err != nil {
		return OnlineRecord{}, false
	}
	return parseOnlineRecord(raw)
}

// setOnline marks the user online on this Gateway (JSON route record + TTL).
func (gw *Gateway) setOnline(userID, connID string) {
	value := gw.encodeOnline(userID, connID)
	pipe := gw.rdb.Pipeline()
	pipe.Set(gw.ctx, userOnlineKey(userID), value, onlineTTL)
	pipe.SAdd(gw.ctx, gwUsersKey(gw.id), userID)
	pipe.Set(gw.ctx, gwAliveKey(gw.id), "1", gwAliveTTL)
	if _, err := pipe.Exec(gw.ctx); err != nil {
		log.Printf("[Gateway] setOnline 失败 user=%s: %v", userID, err)
	}
}

// renewOnline refreshes TTL; reclaims the key if it drifted to another node.
func (gw *Gateway) renewOnline(userID, connID string) {
	raw, err := gw.rdb.Get(gw.ctx, userOnlineKey(userID)).Result()
	if err == redis.Nil {
		gw.setOnline(userID, connID)
		return
	}
	if err != nil {
		log.Printf("[Gateway] renewOnline 读取失败 user=%s: %v", userID, err)
		return
	}
	rec, ok := parseOnlineRecord(raw)
	if !ok || rec.NodeID != gw.id {
		gw.setOnline(userID, connID)
		return
	}
	// Refresh JSON timestamp while keeping connId.
	if connID == "" {
		connID = rec.ConnID
	}
	value := gw.encodeOnline(userID, connID)
	pipe := gw.rdb.Pipeline()
	pipe.Set(gw.ctx, userOnlineKey(userID), value, onlineTTL)
	pipe.SAdd(gw.ctx, gwUsersKey(gw.id), userID)
	if _, err := pipe.Exec(gw.ctx); err != nil {
		log.Printf("[Gateway] renewOnline 失败 user=%s: %v", userID, err)
	}
}

// clearOnline deletes the route only when it still points at this node+conn.
func (gw *Gateway) clearOnline(userID, connID string) {
	raw, err := gw.rdb.Get(gw.ctx, userOnlineKey(userID)).Result()
	if err != nil {
		return
	}
	rec, ok := parseOnlineRecord(raw)
	if !ok || rec.NodeID != gw.id {
		return
	}
	// If another connection on this node already rewrote the record, keep it.
	if connID != "" && rec.ConnID != "" && rec.ConnID != connID {
		return
	}
	pipe := gw.rdb.Pipeline()
	pipe.Del(gw.ctx, userOnlineKey(userID))
	pipe.SRem(gw.ctx, gwUsersKey(gw.id), userID)
	if _, err := pipe.Exec(gw.ctx); err != nil {
		log.Printf("[Gateway] clearOnline 失败 user=%s: %v", userID, err)
	}
}

// StartPresenceLoop periodically renews local online users and gw:alive.
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
	type pair struct {
		userID string
		connID string
	}
	gw.mu.RLock()
	items := make([]pair, 0, len(gw.clients))
	for uid, c := range gw.clients {
		items = append(items, pair{userID: uid, connID: c.connID})
	}
	gw.mu.RUnlock()

	for _, item := range items {
		gw.renewOnline(item.userID, item.connID)
	}

	if err := gw.rdb.Set(gw.ctx, gwAliveKey(gw.id), "1", gwAliveTTL).Err(); err != nil {
		log.Printf("[Gateway] gw:alive 续期失败: %v", err)
	}
}

// ShutdownPresence cleans route keys still owned by this Gateway.
func (gw *Gateway) ShutdownPresence(ctx context.Context) {
	members, err := gw.rdb.SMembers(ctx, gwUsersKey(gw.id)).Result()
	if err != nil {
		log.Printf("[Gateway] ShutdownPresence 读取用户集合失败: %v", err)
		return
	}

	pipe := gw.rdb.Pipeline()
	for _, uid := range members {
		raw, getErr := gw.rdb.Get(ctx, userOnlineKey(uid)).Result()
		if getErr == nil {
			if rec, ok := parseOnlineRecord(raw); ok && rec.NodeID == gw.id {
				pipe.Del(ctx, userOnlineKey(uid))
			}
		}
		pipe.SRem(ctx, gwUsersKey(gw.id), uid)
	}
	pipe.Del(ctx, gwAliveKey(gw.id))
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[Gateway] ShutdownPresence 清理失败: %v", err)
	}
}

// imPushEnvelope is the Redis Pub/Sub payload for cross-node delivery.
type imPushEnvelope struct {
	UserID       string          `json:"userId"`
	Payload      json.RawMessage `json:"payload"`
	MsgID        string          `json:"msgId,omitempty"`
	FromNodeID   string          `json:"fromNodeId,omitempty"`
	TargetNodeID string          `json:"targetNodeId,omitempty"`
}

// PublishImPush publishes a cross-node IM push.
// Prefer the target node channel when known; always also publish to the shared
// channel so Node API subscribers keep working.
func (gw *Gateway) PublishImPush(userID string, payload []byte) error {
	return gw.publishImPush(userID, payload, "", "")
}

func (gw *Gateway) publishImPush(userID string, payload []byte, msgID, targetNodeID string) error {
	envelope, err := json.Marshal(imPushEnvelope{
		UserID:       userID,
		Payload:      json.RawMessage(payload),
		MsgID:        msgID,
		FromNodeID:   gw.id,
		TargetNodeID: targetNodeID,
	})
	if err != nil {
		return err
	}

	channels := []string{imPushChannel}
	if targetNodeID != "" && targetNodeID != gw.id {
		channels = append(channels, nodePushChannel(targetNodeID))
	}

	var lastErr error
	published := false
	for _, ch := range channels {
		if err := gw.rdb.Publish(gw.ctx, ch, envelope).Err(); err != nil {
			log.Printf("[Gateway] Pub/Sub 发布失败 channel=%s user=%s: %v", ch, userID, err)
			lastErr = err
			continue
		}
		published = true
	}
	if !published {
		return lastErr
	}
	return nil
}

// DeliverToUser routes a serialized message:
//  1. local WebSocket if present
//  2. Redis Pub/Sub when the user is online on another node
//  3. returns false when offline (caller may trigger APNs / store-only paths)
//
// Never re-publishes messages received from Pub/Sub (use PushToUser there).
func (gw *Gateway) DeliverToUser(userID string, serializedMsg []byte, msgID string) bool {
	if gw.PushToUser(userID, serializedMsg) {
		return true
	}

	rec, ok := gw.LookupOnline(userID)
	if !ok || rec.NodeID == "" {
		return false
	}
	if rec.NodeID == gw.id {
		// Race: Redis says local but socket map missed — do not Pub/Sub loop.
		log.Printf("[Gateway] 用户 %s 路由指向本节点但无本地连接，跳过 Pub/Sub", userID)
		return false
	}

	if msgID != "" && !gw.markPushOnce(userID, msgID) {
		log.Printf("[Gateway] 跳过重复跨节点推送 user=%s msgId=%s", userID, msgID)
		return true
	}

	if err := gw.publishImPush(userID, serializedMsg, msgID, rec.NodeID); err != nil {
		log.Printf("[Gateway] 跨节点推送失败 user=%s node=%s: %v", userID, rec.NodeID, err)
		return false
	}
	return true
}

func (gw *Gateway) markPushOnce(userID, msgID string) bool {
	ok, err := gw.rdb.SetNX(gw.ctx, recentPushKey(userID, msgID), gw.id, recentPushTTL).Result()
	if err != nil {
		// Fail open: still attempt delivery; Redis flaps should not drop chat.
		log.Printf("[Gateway] 推送去重写入失败 user=%s msgId=%s: %v", userID, msgID, err)
		return true
	}
	return ok
}

func unwrapPayload(payload json.RawMessage) []byte {
	if len(payload) > 0 && payload[0] == '"' {
		var s string
		if err := json.Unmarshal(payload, &s); err == nil {
			return []byte(s)
		}
	}
	return payload
}

func extractMsgID(payload []byte, fallback string) string {
	if fallback != "" {
		return fallback
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
		ID      string          `json:"id"`
		MsgID   string          `json:"msgId"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return ""
	}
	if envelope.MsgID != "" {
		return envelope.MsgID
	}
	if envelope.ID != "" {
		return envelope.ID
	}
	if len(envelope.Payload) == 0 {
		return ""
	}
	var inner struct {
		ID           string `json:"id"`
		MsgID        string `json:"msgId"`
		ClientMsgID  string `json:"clientMsgId"`
		ClientMsgId  string `json:"client_msg_id"`
	}
	if err := json.Unmarshal(envelope.Payload, &inner); err != nil {
		return ""
	}
	switch {
	case inner.MsgID != "":
		return inner.MsgID
	case inner.ID != "":
		return inner.ID
	case inner.ClientMsgID != "":
		return inner.ClientMsgID
	case inner.ClientMsgId != "":
		return inner.ClientMsgId
	default:
		return ""
	}
}

func (gw *Gateway) handlePushEnvelope(raw string) {
	var envelope imPushEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		log.Printf("[Gateway] Pub/Sub 消息解析失败: %v", err)
		return
	}
	if envelope.UserID == "" || len(envelope.Payload) == 0 {
		return
	}
	// Loop guard: never re-handle our own publish on the shared channel.
	if envelope.FromNodeID != "" && envelope.FromNodeID == gw.id {
		return
	}
	if envelope.TargetNodeID != "" && envelope.TargetNodeID != gw.id {
		return
	}

	payload := unwrapPayload(envelope.Payload)
	msgID := extractMsgID(payload, envelope.MsgID)
	if msgID != "" && !gw.markPushOnce(envelope.UserID, "rx:"+msgID) {
		log.Printf("[Gateway] 跳过重复本机投递 user=%s msgId=%s", envelope.UserID, msgID)
		return
	}

	if !gw.PushToUser(envelope.UserID, payload) {
		log.Printf("[Gateway] Pub/Sub 到达但本机无连接 user=%s msgId=%s", envelope.UserID, msgID)
	}
}

// StartPushSubscriber listens on the shared + node-specific push channels.
// Received messages are delivered locally only (never re-published).
func (gw *Gateway) StartPushSubscriber(ctx context.Context) {
	channels := []string{imPushChannel, nodePushChannel(gw.id)}
	pubsub := gw.rdb.Subscribe(ctx, channels...)
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
				gw.handlePushEnvelope(msg.Payload)
			}
		}
	}()
	log.Printf("[Gateway] 已订阅 Pub/Sub: %v (nodeId=%s)", channels, gw.id)
}
