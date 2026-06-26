package delivery

import (
	"encoding/json"
	"log"

	"github.com/nats-io/nats.go"
	"github.com/neomsg/neomsg/backend/internal/message"
	"github.com/neomsg/neomsg/backend/internal/mtproto/bridge"
	"github.com/neomsg/neomsg/backend/internal/mtproto/connmgr"
)

// Subscriber NATS → MTProto 加密推送
type Subscriber struct {
	conns *connmgr.Manager
}

func NewSubscriber(conns *connmgr.Manager) *Subscriber {
	return &Subscriber{conns: conns}
}

func (s *Subscriber) Start(nc *nats.Conn) error {
	_, err := nc.Subscribe("msg.deliver.*", func(m *nats.Msg) {
		var pkt message.DeliveryPacket
		if err := json.Unmarshal(m.Data, &pkt); err != nil {
			log.Printf("[MTProto Delivery] decode: %v", err)
			return
		}
		body := bridge.EncodePushWire(pkt.Payload)
		if err := s.conns.Send(pkt.UserID, pkt.DeviceID, body); err != nil {
			log.Printf("[MTProto Delivery] push user=%d device=%s: %v", pkt.UserID, pkt.DeviceID, err)
		}
	})
	return err
}
