package protocol

import (
	"fmt"

	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"google.golang.org/protobuf/proto"
)

type FrameCodec struct{}

func NewFrameCodec() *FrameCodec { return &FrameCodec{} }

// EncodeWirePacket 序列化为 [4字节大端长度][WirePacket]
func (c *FrameCodec) EncodeWirePacket(pkt *pb.WirePacket) ([]byte, error) {
	data, err := proto.Marshal(pkt)
	if err != nil {
		return nil, fmt.Errorf("marshal wire packet: %w", err)
	}
	frame := make([]byte, 4+len(data))
	frame[0] = byte(len(data) >> 24)
	frame[1] = byte(len(data) >> 16)
	frame[2] = byte(len(data) >> 8)
	frame[3] = byte(len(data))
	copy(frame[4:], data)
	return frame, nil
}

func (c *FrameCodec) DecodeWirePacket(frame []byte) (*pb.WirePacket, error) {
	if len(frame) < 4 {
		return nil, fmt.Errorf("frame too short")
	}
	length := int(frame[0])<<24 | int(frame[1])<<16 | int(frame[2])<<8 | int(frame[3])
	if len(frame) < 4+length {
		return nil, fmt.Errorf("incomplete frame")
	}
	var pkt pb.WirePacket
	if err := proto.Unmarshal(frame[4:4+length], &pkt); err != nil {
		return nil, fmt.Errorf("unmarshal wire packet: %w", err)
	}
	return &pkt, nil
}

func (c *FrameCodec) EncodeEnvelope(env *pb.Envelope) ([]byte, error) {
	data, err := proto.Marshal(env)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 4+len(data))
	frame[0] = byte(len(data) >> 24)
	frame[1] = byte(len(data) >> 16)
	frame[2] = byte(len(data) >> 8)
	frame[3] = byte(len(data))
	copy(frame[4:], data)
	return frame, nil
}

func (c *FrameCodec) DecodeEnvelope(frame []byte) (*pb.Envelope, error) {
	if len(frame) < 4 {
		return nil, fmt.Errorf("frame too short")
	}
	length := int(frame[0])<<24 | int(frame[1])<<16 | int(frame[2])<<8 | int(frame[3])
	if len(frame) < 4+length {
		return nil, fmt.Errorf("incomplete frame")
	}
	var env pb.Envelope
	if err := proto.Unmarshal(frame[4:4+length], &env); err != nil {
		return nil, err
	}
	return &env, nil
}
