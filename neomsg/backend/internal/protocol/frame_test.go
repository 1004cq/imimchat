package protocol_test

import (
	"testing"

	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"github.com/neomsg/neomsg/backend/internal/protocol"
)

func TestWirePacketRoundTrip(t *testing.T) {
	codec := protocol.NewFrameCodec()
	pkt := &pb.WirePacket{
		Payload: &pb.WirePacket_Message{
			Message: &pb.Message{
				Id:       123,
				ChatId:   1,
				FromId:   2,
				ToId:     3,
				Content:  "hello",
				MsgType:  0,
				SeqId:    10,
				Timestamp: 1710000000000,
			},
		},
	}
	frame, err := codec.EncodeWirePacket(pkt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.DecodeWirePacket(frame)
	if err != nil {
		t.Fatal(err)
	}
	msg := got.GetMessage()
	if msg == nil || msg.Content != "hello" || msg.SeqId != 10 {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

func TestSyncRequestRoundTrip(t *testing.T) {
	codec := protocol.NewFrameCodec()
	pkt := &pb.WirePacket{
		Payload: &pb.WirePacket_SyncRequest{
			SyncRequest: &pb.SyncRequest{
				UserId:  1,
				LastSeq: 100,
				ChatId:  0,
			},
		},
	}
	frame, err := codec.EncodeWirePacket(pkt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.DecodeWirePacket(frame)
	if err != nil {
		t.Fatal(err)
	}
	req := got.GetSyncRequest()
	if req == nil || req.LastSeq != 100 {
		t.Fatalf("unexpected sync request: %+v", req)
	}
}
