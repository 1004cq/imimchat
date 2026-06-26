package protocol

import (
	"encoding/binary"
	"fmt"

	"google.golang.org/protobuf/proto"
)

const (
	MTMagic       uint32 = 0x4E454F4D // "NEOM"
	MTVersion     uint8  = 1
	MTPayloadWire uint8  = 1
	MTPayloadEnv  uint8  = 2
	MTHeaderSize         = 32
)

// MTFrameHeader 二进制帧头（MTProto-like，与 iOS WireFrameCodec 对齐）
type MTFrameHeader struct {
	AuthKeyID   int64
	SessionID   int64
	UserID      int64
	PayloadType uint8
	Flags       uint16
	Seq         uint32
}

func (h *MTFrameHeader) Encode() []byte {
	buf := make([]byte, MTHeaderSize)
	binary.BigEndian.PutUint32(buf[0:4], MTMagic)
	buf[4] = MTVersion
	buf[5] = h.PayloadType
	binary.BigEndian.PutUint16(buf[6:8], h.Flags)
	binary.BigEndian.PutUint64(buf[8:16], uint64(h.AuthKeyID))
	binary.BigEndian.PutUint64(buf[16:24], uint64(h.SessionID))
	binary.BigEndian.PutUint64(buf[24:32], uint64(h.UserID))
	// seq 复用 flags 后空间时可扩展；当前 seq 在 Envelope 内
	return buf
}

func DecodeMTFrameHeader(b []byte) (*MTFrameHeader, error) {
	if len(b) < MTHeaderSize {
		return nil, fmt.Errorf("header too short")
	}
	if binary.BigEndian.Uint32(b[0:4]) != MTMagic {
		return nil, fmt.Errorf("bad magic")
	}
	return &MTFrameHeader{
		PayloadType: b[5],
		Flags:       binary.BigEndian.Uint16(b[6:8]),
		AuthKeyID:   int64(binary.BigEndian.Uint64(b[8:16])),
		SessionID:   int64(binary.BigEndian.Uint64(b[16:24])),
		UserID:      int64(binary.BigEndian.Uint64(b[24:32])),
	}, nil
}

// EncodeMTFrame [4B len][32B header][protobuf]
func EncodeMTFrame(header *MTFrameHeader, msg proto.Message) ([]byte, error) {
	payload, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	body := append(header.Encode(), payload...)
	frame := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(frame[0:4], uint32(len(body)))
	copy(frame[4:], body)
	return frame, nil
}

func DecodeMTFrame(frame []byte, msg proto.Message) (*MTFrameHeader, error) {
	if len(frame) < 4+MTHeaderSize {
		return nil, fmt.Errorf("frame too short")
	}
	length := binary.BigEndian.Uint32(frame[0:4])
	if len(frame) < int(4+length) {
		return nil, fmt.Errorf("incomplete frame")
	}
	header, err := DecodeMTFrameHeader(frame[4 : 4+MTHeaderSize])
	if err != nil {
		return nil, err
	}
	if err := proto.Unmarshal(frame[4+MTHeaderSize:4+length], msg); err != nil {
		return nil, err
	}
	return header, nil
}
