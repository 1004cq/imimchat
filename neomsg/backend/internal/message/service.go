package message

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"github.com/neomsg/neomsg/backend/internal/protocol"
	"github.com/neomsg/neomsg/backend/internal/store/postgres"
	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

// Event 消息扇出事件
type Event struct {
	DialogID    int64
	SenderID    int64
	Payload     []byte // 已序列化的 Envelope
	ExcludeUser int64
}

type Handler func(ctx context.Context, evt *Event) error

// Service 消息路由、存储、扇出
type Service struct {
	pg       *postgres.Store
	redis    *redisstore.Store
	engine   *Engine
	handlers []Handler
	mu       sync.RWMutex
}

func NewService(pg *postgres.Store, redis *redisstore.Store, engine *Engine) *Service {
	return &Service{pg: pg, redis: redis, engine: engine}
}

func (s *Service) Engine() *Engine { return s.engine }

func (s *Service) OnMessage(h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, h)
}

// Send 处理客户端发送的消息
func (s *Service) Send(ctx context.Context, req *SendRequest) (*SendResponse, error) {
	// 限流：每用户每分钟 60 条
	allowed, err := s.redis.CheckRateLimit(ctx,
		fmt.Sprintf("rate:msg:%d", req.SenderID), 60, time.Minute)
	if err != nil || !allowed {
		return nil, fmt.Errorf("rate limited")
	}

	seq, err := s.pg.NextSeq(ctx, req.DialogID)
	if err != nil {
		return nil, fmt.Errorf("next seq: %w", err)
	}

	msg := &postgres.Message{
		DialogID:    req.DialogID,
		SenderID:    req.SenderID,
		MsgType:     req.MsgType,
		Content:     req.Content,
		ContentText: req.ContentText,
		ClientMsgID: req.ClientMsgID,
		Seq:         seq,
		TTLSeconds:  req.TTLSeconds,
	}
	if err := s.pg.InsertMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("insert: %w", err)
	}

	// 构建扇出 payload（实际应 protobuf 序列化）
	payload := req.BuildEnvelope(seq)

	evt := &Event{
		DialogID:    req.DialogID,
		SenderID:    req.SenderID,
		Payload:     payload,
		ExcludeUser: 0,
	}

	s.mu.RLock()
	handlers := s.handlers
	s.mu.RUnlock()
	for _, h := range handlers {
		if err := h(ctx, evt); err != nil {
			// 扇出失败不阻塞发送方
			continue
		}
	}

	return &SendResponse{MessageID: seq, Seq: seq}, nil
}

// AckRead 处理已读回执
func (s *Service) AckRead(ctx context.Context, dialogID, userID, lastReadID int64) error {
	return s.pg.UpdateReadCursor(ctx, dialogID, userID, lastReadID)
}

type SendRequest struct {
	DialogID    int64
	SenderID    int64
	MsgType     int16
	Content     []byte
	ContentText string
	ClientMsgID string
	TTLSeconds  int32
}

func (r *SendRequest) BuildEnvelope(seq int64) []byte {
	msg := &pb.Message{
		ChatId:    r.DialogID,
		FromId:    r.SenderID,
		Content:   r.ContentText,
		MsgType:   int32(r.MsgType),
		SeqId:     seq,
		Timestamp: time.Now().UnixMilli(),
	}
	pkt := &pb.WirePacket{Payload: &pb.WirePacket_Message{Message: msg}}
	frame, err := protocol.NewFrameCodec().EncodeWirePacket(pkt)
	if err != nil {
		return []byte(fmt.Sprintf(`{"type":"new_message","dialog_id":%d,"seq":%d}`, r.DialogID, seq))
	}
	return frame
}

type SendResponse struct {
	MessageID int64
	Seq       int64
}
