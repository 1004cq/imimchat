package storage

import (
	"context"
	"fmt"

	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"github.com/neomsg/neomsg/backend/internal/store/postgres"
)

// MessageStore 消息持久化（封装 PostgreSQL）
type MessageStore struct {
	pg *postgres.Store
}

func NewMessageStore(pg *postgres.Store) *MessageStore {
	return &MessageStore{pg: pg}
}

func (s *MessageStore) SaveMessage(ctx context.Context, msg *pb.Message) (*postgres.Message, error) {
	if msg == nil {
		return nil, fmt.Errorf("empty message")
	}
	if msg.ChatId == 0 {
		return nil, fmt.Errorf("chat_id required")
	}
	if msg.FromId == 0 {
		return nil, fmt.Errorf("from_id required")
	}

	seq := msg.SeqId
	if seq == 0 {
		next, err := s.pg.NextSeq(ctx, msg.ChatId)
		if err != nil {
			return nil, err
		}
		seq = next
		msg.SeqId = seq
	}

	contentText := msg.Content
	record := &postgres.Message{
		DialogID:    msg.ChatId,
		SenderID:    msg.FromId,
		MsgType:     int16(msg.MsgType),
		Content:     []byte(msg.Content),
		ContentText: contentText,
		ClientMsgID: fmt.Sprintf("%d", msg.Id),
		Seq:         seq,
	}
	if len(msg.MediaKey) > 0 {
		record.Content = append(msg.MediaKey, record.Content...)
	}

	if err := s.pg.InsertMessage(ctx, record); err != nil {
		return nil, err
	}
	msg.Timestamp = record.CreatedAt.UnixMilli()
	return record, nil
}
