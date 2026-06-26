package bridge

import (
	"context"
	"fmt"

	"github.com/neomsg/neomsg/backend/internal/message"
	"github.com/neomsg/neomsg/backend/internal/mtproto/tl"
	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"google.golang.org/protobuf/proto"
)

// NeoMsg 自定义 TL Constructor（MTProto 加密层桥接 Wire 协议）
const (
	CRCNeoMsgBindSession int32 = 0x6e656f01
	CRCNeoMsgBindOk      int32 = 0x6e656f11
	CRCNeoMsgInvokeWire  int32 = 0x6e656f02
	CRCNeoMsgWireResult  int32 = 0x6e656f12
	CRCNeoMsgPushWire    int32 = 0x6e656f13
)

const (
	crcBoolTrue  int32 = 1
	crcBoolFalse int32 = 0
)

// Handler 将 MTProto TL 桥接到 Message Engine
type Handler struct {
	msgSvc *message.Service
}

func NewHandler(msgSvc *message.Service) *Handler {
	return &Handler{msgSvc: msgSvc}
}

func (h *Handler) HandleInvokeWire(ctx context.Context, userID int64, deviceID string, payload []byte) ([][]byte, error) {
	var pkt pb.WirePacket
	if err := proto.Unmarshal(payload, &pkt); err != nil {
		return nil, fmt.Errorf("unmarshal wire packet: %w", err)
	}
	return h.msgSvc.HandleWirePacket(ctx, userID, deviceID, &pkt)
}

func EncodeWireResult(frames [][]byte) []byte {
	w := tl.NewWriter()
	w.WriteInt(CRCNeoMsgWireResult)
	w.WriteInt(int32(len(frames)))
	for _, f := range frames {
		w.WriteBytes(f)
	}
	return w.Bytes()
}

func EncodePushWire(frame []byte) []byte {
	w := tl.NewWriter()
	w.WriteInt(CRCNeoMsgPushWire)
	w.WriteBytes(frame)
	return w.Bytes()
}

func EncodeBindOk(success bool) []byte {
	w := tl.NewWriter()
	w.WriteInt(CRCNeoMsgBindOk)
	if success {
		w.WriteInt(crcBoolTrue)
	} else {
		w.WriteInt(crcBoolFalse)
	}
	return w.Bytes()
}

func ParseBindSession(body []byte) (userID int64, deviceID, token string, err error) {
	r := tl.NewReader(body)
	cid, err := r.ReadInt()
	if err != nil {
		return 0, "", "", err
	}
	if cid != CRCNeoMsgBindSession {
		return 0, "", "", fmt.Errorf("expected bindSession, got %#x", cid)
	}
	userID, err = r.ReadLong()
	if err != nil {
		return 0, "", "", err
	}
	deviceID, err = r.ReadString()
	if err != nil {
		return 0, "", "", err
	}
	token, err = r.ReadString()
	if err != nil {
		return 0, "", "", err
	}
	return userID, deviceID, token, nil
}

func ParseInvokeWire(body []byte) ([]byte, error) {
	r := tl.NewReader(body)
	cid, err := r.ReadInt()
	if err != nil {
		return nil, err
	}
	if cid != CRCNeoMsgInvokeWire {
		return nil, fmt.Errorf("expected invokeWire, got %#x", cid)
	}
	return r.ReadBytes()
}
