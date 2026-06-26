package ws

import (
	"context"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/neomsg/neomsg/backend/internal/gateway/session"
	"github.com/neomsg/neomsg/backend/internal/message"
	"github.com/neomsg/neomsg/backend/internal/protocol"
	"github.com/neomsg/neomsg/backend/internal/push"
	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type Handler struct {
	sessions *session.Manager
	msgSvc   *message.Service
	push     *push.Dispatcher
	redis    *redisstore.Store
}

func NewHandler(sessions *session.Manager, msgSvc *message.Service, push *push.Dispatcher, redis *redisstore.Store) *Handler {
	return &Handler{sessions: sessions, msgSvc: msgSvc, push: push, redis: redis}
}

func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] upgrade failed: %v", err)
		return
	}

	// 首帧必须是 auth_request（简化：从 query 取 token）
	token := r.URL.Query().Get("token")
	deviceID := r.URL.Query().Get("device_id")
	if token == "" || deviceID == "" {
		ws.Close()
		return
	}

	userID, err := h.authenticate(token)
	if err != nil {
		ws.Close()
		return
	}

	conn := session.NewWSConn(ws, userID, deviceID)
	h.sessions.Register(userID, deviceID, conn)
	defer func() {
		h.sessions.Unregister(userID, deviceID)
		_ = h.redis.UnregisterDeviceSession(context.Background(), userID, deviceID)
	}()

	_ = h.redis.RegisterDeviceSession(context.Background(), userID, redisstore.DeviceSession{
		DeviceID: deviceID,
		Platform: r.URL.Query().Get("platform"),
	})
	_ = h.redis.SetOnline(context.Background(), userID, deviceID)

	log.Printf("[WS] connected user=%d device=%s", userID, deviceID)

	codec := protocol.NewFrameCodec()

	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		h.handleFrame(context.Background(), conn, codec, userID, data)
	}
}

func (h *Handler) authenticate(token string) (int64, error) {
	// TODO: JWT 验证
	return 1, nil
}

func (h *Handler) handleFrame(ctx context.Context, conn session.Conn, codec *protocol.FrameCodec, userID int64, data []byte) {
	pkt, err := codec.DecodeWirePacket(data)
	if err != nil {
		log.Printf("[WS] decode wire packet: %v", err)
		return
	}

	frames, err := h.msgSvc.HandleWirePacket(ctx, userID, conn.DeviceID(), pkt)
	if err != nil {
		log.Printf("[WS] handle wire packet: %v", err)
		return
	}
	for _, frame := range frames {
		if err := conn.Send(frame); err != nil {
			log.Printf("[WS] send response: %v", err)
			return
		}
	}
}
