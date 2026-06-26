package message

import (
	"context"
	"fmt"

	"github.com/neomsg/neomsg/backend/internal/id"
	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"github.com/neomsg/neomsg/backend/internal/protocol"
	"github.com/neomsg/neomsg/backend/internal/store/postgres"
)

// HandleWirePacket 处理 WirePacket（Message / SyncRequest）
func (s *Service) HandleWirePacket(ctx context.Context, userID int64, deviceID string, pkt *pb.WirePacket) ([][]byte, error) {
	switch body := pkt.Payload.(type) {
	case *pb.WirePacket_Message:
		return s.handleWireMessage(ctx, userID, deviceID, body.Message)
	case *pb.WirePacket_SyncRequest:
		return s.handleSyncRequest(ctx, userID, body.SyncRequest)
	default:
		return nil, fmt.Errorf("unsupported wire packet")
	}
}

func (s *Service) handleWireMessage(ctx context.Context, userID int64, deviceID string, msg *pb.Message) ([][]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("empty message")
	}
	if msg.ChatId == 0 {
		return nil, fmt.Errorf("chat_id required")
	}

	header := &protocol.MTHeader{
		UserID:   userID,
		DeviceID: deviceID,
	}
	ack, err := s.engine.ProcessMessage(ctx, header, msg)
	if err != nil {
		failAck := &pb.MessageAck{
			MsgId:   msg.Id,
			SeqId:   msg.SeqId,
			Success: false,
		}
		frame, encErr := protocol.EncodeAck(failAck)
		if encErr != nil {
			return nil, err
		}
		return [][]byte{frame}, nil
	}

	ackFrame, err := protocol.EncodeAck(ack)
	if err != nil {
		return nil, err
	}
	return [][]byte{ackFrame}, nil
}

func (s *Service) handleSyncRequest(ctx context.Context, userID int64, req *pb.SyncRequest) ([][]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("empty sync request")
	}
	if req.UserId != 0 && req.UserId != userID {
		return nil, fmt.Errorf("user_id mismatch")
	}

	rows, err := s.pg.ListMessagesForSync(ctx, userID, req.ChatId, req.LastSeq, 100)
	if err != nil {
		return nil, err
	}

	msgs := make([]*pb.Message, 0, len(rows))
	var lastSeq int64 = req.LastSeq
	for i := range rows {
		wm, err := s.toWireMessage(ctx, &rows[i], userID)
		if err != nil {
			continue
		}
		msgs = append(msgs, wm)
		if wm.SeqId > lastSeq {
			lastSeq = wm.SeqId
		}
	}

	frame, err := protocol.NewFrameCodec().EncodeWirePacket(&pb.WirePacket{
		Payload: &pb.WirePacket_SyncResponse{
			SyncResponse: &pb.SyncResponse{
				Messages: msgs,
				LastSeq:  lastSeq,
				HasMore:  len(msgs) >= 100,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return [][]byte{frame}, nil
}

func (s *Service) toWireMessage(ctx context.Context, m *postgres.Message, viewerID int64) (*pb.Message, error) {
	toID, err := s.pg.GetPeerUserID(ctx, m.DialogID, m.SenderID)
	if err != nil {
		toID = 0
	}
	if m.SenderID != viewerID {
		toID = viewerID
	}

	content := m.ContentText
	if content == "" && len(m.Content) > 0 {
		content = string(m.Content)
	}

	return &pb.Message{
		Id:        chooseWireID(m),
		ChatId:    m.DialogID,
		FromId:    m.SenderID,
		ToId:      toID,
		Content:   content,
		MsgType:   int32(m.MsgType),
		SeqId:     m.Seq,
		Timestamp: m.CreatedAt.UnixMilli(),
		IsSecret:  m.IsSecret,
	}, nil
}

var snowflakeGen = id.NewSnowflake(1)

func NewWireMessageID() int64 {
	return snowflakeGen.Next()
}

func chooseWireID(m *postgres.Message) int64 {
	if m.ClientMsgID != "" {
		var id int64
		if _, err := fmt.Sscanf(m.ClientMsgID, "%d", &id); err == nil && id > 0 {
			return id
		}
	}
	if m.ID > 0 {
		return m.ID
	}
	return NewWireMessageID()
}
