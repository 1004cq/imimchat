package message

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"github.com/neomsg/neomsg/backend/internal/protocol"
	"github.com/neomsg/neomsg/backend/internal/push"
	"github.com/neomsg/neomsg/backend/internal/storage"
	"github.com/neomsg/neomsg/backend/internal/store/postgres"
	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

// DeviceInfo 在线设备信息
type DeviceInfo struct {
	UserID    int64
	DeviceID  string
	SessionID int64
	Platform  string
}

// DeliveryPacket NATS 设备投递包
type DeliveryPacket struct {
	UserID    int64  `json:"user_id"`
	DeviceID  string `json:"device_id"`
	SessionID int64  `json:"session_id"`
	Payload   []byte `json:"payload"`
}

// Engine 消息处理核心引擎
type Engine struct {
	pg    *postgres.Store
	redis *redisstore.Store
	nats  *nats.Conn
	store *storage.MessageStore
	push  *push.Dispatcher
}

func NewEngine(pg *postgres.Store, redis *redisstore.Store, nc *nats.Conn, pushDisp *push.Dispatcher) *Engine {
	return &Engine{
		pg:    pg,
		redis: redis,
		nats:  nc,
		store: storage.NewMessageStore(pg),
		push:  pushDisp,
	}
}

// ProcessMessage 处理接收到的消息（核心入口）
func (e *Engine) ProcessMessage(ctx context.Context, header *protocol.MTHeader, msg *pb.Message) (*pb.MessageAck, error) {
	if err := e.validateSession(header); err != nil {
		return nil, err
	}
	if msg.FromId == 0 {
		msg.FromId = header.UserID
	}
	if msg.ChatId == 0 {
		return nil, fmt.Errorf("chat_id required")
	}
	if msg.Id == 0 {
		msg.Id = NewWireMessageID()
	}

	allowed, err := e.redis.CheckRateLimit(ctx,
		fmt.Sprintf("rate:msg:%d", msg.FromId), 60, time.Minute)
	if err != nil || !allowed {
		return nil, fmt.Errorf("rate limited")
	}

	saved, err := e.store.SaveMessage(ctx, msg)
	if err != nil {
		return nil, err
	}
	msg.SeqId = saved.Seq
	msg.Timestamp = saved.CreatedAt.UnixMilli()

	e.updateChatSeq(ctx, msg.ChatId, msg.SeqId)
	e.fanoutToOnlineDevices(msg)
	go e.pushToOfflineUsers(context.Background(), msg)

	ack := &pb.MessageAck{
		MsgId:   msg.Id,
		SeqId:   msg.SeqId,
		Success: true,
	}
	log.Printf("[Engine] message processed: id=%d chat=%d seq=%d", msg.Id, msg.ChatId, msg.SeqId)
	return ack, nil
}

func (e *Engine) fanoutToOnlineDevices(msg *pb.Message) {
	devices := e.getOnlineDevices(msg.ChatId)
	if len(devices) == 0 {
		return
	}

	for _, device := range devices {
		if device.UserID == msg.FromId {
			continue
		}
		payload, err := protocol.EncodeMessageForDevice(msg, device.SessionID)
		if err != nil {
			log.Printf("[Engine] encode deliver: %v", err)
			continue
		}
		if e.nats == nil {
			continue
		}
		packet := DeliveryPacket{
			UserID:    device.UserID,
			DeviceID:  device.DeviceID,
			SessionID: device.SessionID,
			Payload:   payload,
		}
		body, err := json.Marshal(packet)
		if err != nil {
			continue
		}
		subject := "msg.deliver." + device.DeviceID
		if err := e.nats.Publish(subject, body); err != nil {
			log.Printf("[Engine] nats publish %s: %v", subject, err)
		}
	}
}

func (e *Engine) pushToOfflineUsers(ctx context.Context, msg *pb.Message) {
	for _, userID := range e.getOfflineUsers(ctx, msg.ChatId, msg.FromId) {
		if e.push == nil {
			log.Printf("[Engine] push to user %d: %s", userID, msg.Content)
			continue
		}
		_ = e.push.Dispatch(ctx, &push.PushPayload{
			UserID:   userID,
			Title:    "NeoMsg",
			Body:     truncate(msg.Content, 120),
			DialogID: msg.ChatId,
			Badge:    1,
		})
	}
}

func (e *Engine) updateChatSeq(ctx context.Context, chatID, seq int64) {
	if err := e.redis.SetChatSeq(ctx, chatID, seq); err != nil {
		log.Printf("[Engine] set chat seq: %v", err)
	}
}

func (e *Engine) getOnlineDevices(chatID int64) []DeviceInfo {
	ctx := context.Background()
	members, err := e.pg.ListDialogMemberIDs(ctx, chatID)
	if err != nil {
		return nil
	}

	var devices []DeviceInfo
	for _, userID := range members {
		ds, err := e.redis.ListUserDevices(ctx, userID)
		if err != nil {
			continue
		}
		for _, d := range ds {
			devices = append(devices, DeviceInfo{
				UserID:    userID,
				DeviceID:  d.DeviceID,
				SessionID: d.SessionID,
				Platform:  d.Platform,
			})
		}
	}
	return devices
}

func (e *Engine) getOfflineUsers(ctx context.Context, chatID, senderID int64) []int64 {
	members, err := e.pg.ListDialogMemberIDs(ctx, chatID)
	if err != nil {
		return nil
	}
	var offline []int64
	for _, userID := range members {
		if userID == senderID {
			continue
		}
		online, err := e.redis.IsOnline(ctx, userID)
		if err != nil || online {
			continue
		}
		offline = append(offline, userID)
	}
	return offline
}

func (e *Engine) validateSession(header *protocol.MTHeader) error {
	if err := header.Validate(); err != nil {
		return err
	}
	// MTProto AuthKey / Session 校验可在此扩展
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
