package delivery

import (
	"encoding/json"
	"log"

	"github.com/nats-io/nats.go"
	"github.com/neomsg/neomsg/backend/internal/gateway/session"
	"github.com/neomsg/neomsg/backend/internal/message"
)

// Subscriber 订阅 NATS 投递主题并写入本地连接
type Subscriber struct {
	sessions *session.Manager
}

func NewSubscriber(sessions *session.Manager) *Subscriber {
	return &Subscriber{sessions: sessions}
}

func (s *Subscriber) Start(nc *nats.Conn) error {
	_, err := nc.Subscribe("msg.deliver.*", func(m *nats.Msg) {
		var pkt message.DeliveryPacket
		if err := json.Unmarshal(m.Data, &pkt); err != nil {
			log.Printf("[Delivery] decode: %v", err)
			return
		}
		for _, conn := range s.sessions.GetConns(pkt.UserID) {
			if pkt.DeviceID != "" && conn.DeviceID() != pkt.DeviceID {
				continue
			}
			if err := conn.Send(pkt.Payload); err != nil {
				log.Printf("[Delivery] send user=%d device=%s: %v", pkt.UserID, conn.DeviceID(), err)
			}
		}
	})
	return err
}
