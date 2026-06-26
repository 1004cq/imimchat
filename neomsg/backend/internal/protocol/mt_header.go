package protocol

import (
	"fmt"

	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
)

// MTHeader 消息处理上下文（MTProto / Wire 通用）
type MTHeader struct {
	AuthKeyID int64
	SessionID int64
	UserID    int64
	DeviceID  string
	Platform  string
}

func (h *MTHeader) Validate() error {
	if h == nil {
		return fmt.Errorf("missing header")
	}
	if h.UserID == 0 {
		return fmt.Errorf("user_id required")
	}
	return nil
}

// EncodeMessageForDevice 将消息编码为设备投递帧
func EncodeMessageForDevice(msg *pb.Message, sessionID int64) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("empty message")
	}
	_ = sessionID
	pkt := &pb.WirePacket{
		Payload: &pb.WirePacket_Message{Message: msg},
	}
	return NewFrameCodec().EncodeWirePacket(pkt)
}

// EncodeAck 编码 MessageAck 帧
func EncodeAck(ack *pb.MessageAck) ([]byte, error) {
	pkt := &pb.WirePacket{
		Payload: &pb.WirePacket_MessageAck{MessageAck: ack},
	}
	return NewFrameCodec().EncodeWirePacket(pkt)
}
