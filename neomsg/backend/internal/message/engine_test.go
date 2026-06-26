package message_test

import (
	"context"
	"testing"

	"github.com/neomsg/neomsg/backend/internal/message"
	"github.com/neomsg/neomsg/backend/internal/protocol"
	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
)

func TestMTHeaderValidate(t *testing.T) {
	err := (&protocol.MTHeader{}).Validate()
	if err == nil {
		t.Fatal("expected error for empty user id")
	}
	err = (&protocol.MTHeader{UserID: 1}).Validate()
	if err != nil {
		t.Fatal(err)
	}
}

func TestProcessMessageRequiresChatID(t *testing.T) {
	engine := message.NewEngine(nil, nil, nil, nil)
	_, err := engine.ProcessMessage(context.Background(), &protocol.MTHeader{UserID: 1}, &pb.Message{
		FromId: 1,
	})
	if err == nil || err.Error() != "chat_id required" {
		t.Fatalf("expected chat_id required, got %v", err)
	}
}
