package ws

import (
	"context"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/neomsg/neomsg/backend/internal/gateway/session"
	"github.com/neomsg/neomsg/backend/internal/message"
	"github.com/neomsg/neomsg/backend/internal/push"
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
}

func NewHandler(sessions *session.Manager, msgSvc *message.Service, push *push.Dispatcher) *Handler {
	return &Handler{sessions: sessions, msgSvc: msgSvc, push: push}
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
	defer h.sessions.Unregister(userID, deviceID)

	log.Printf("[WS] connected user=%d device=%s", userID, deviceID)

	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		h.handleFrame(context.Background(), userID, deviceID, data)
	}
}

func (h *Handler) authenticate(token string) (int64, error) {
	// TODO: JWT 验证
	return 1, nil
}

func (h *Handler) handleFrame(ctx context.Context, userID int64, deviceID string, data []byte) {
	// TODO: protobuf 解码 Envelope，按 payload 类型分发
	_ = ctx
	_ = userID
	_ = deviceID
	_ = data
}
