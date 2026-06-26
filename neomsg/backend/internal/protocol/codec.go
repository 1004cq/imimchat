package protocol

import (
	"fmt"

	"google.golang.org/protobuf/proto"
)

// Codec Protobuf 编解码器
type Codec struct{}

func NewCodec() *Codec { return &Codec{} }

// Encode 将消息序列化为 [4字节长度][payload] 帧
func (c *Codec) Encode(msg proto.Message) ([]byte, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	frame := make([]byte, 4+len(data))
	frame[0] = byte(len(data) >> 24)
	frame[1] = byte(len(data) >> 16)
	frame[2] = byte(len(data) >> 8)
	frame[3] = byte(len(data))
	copy(frame[4:], data)
	return frame, nil
}

// Decode 从帧数据中解析 protobuf 消息
func (c *Codec) Decode(data []byte, msg proto.Message) error {
	if len(data) < 4 {
		return fmt.Errorf("frame too short")
	}
	length := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	if len(data) < 4+length {
		return fmt.Errorf("incomplete frame")
	}
	return proto.Unmarshal(data[4:4+length], msg)
}
