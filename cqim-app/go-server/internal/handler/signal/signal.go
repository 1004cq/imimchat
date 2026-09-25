package signal

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/group"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/misc"
)

// WS 背压上限：与 TS 版 WS_BACKPRESSURE_LIMIT=65536 一致。
const wsBackpressureLimit = 65536

// Redis 频道
const (
	imPushChannel     = "cqim:im:push"
	momentEventsCh    = "moment_events"
	profileUpdatedCh  = "user_profile_updated"
	onlineKeyPrefix   = "online:"
	groupOnlinePrefix = "group:online:"
	unreadHashPrefix  = "unread:"
	convListPrefix    = "convlist:"
)

// Client 单个 /signal 连接。
type Client struct {
	userID      string
	conn        *websocket.Conn
	send        chan []byte
	pendingByte atomic.Int64 // 真实待发送字节数（原子操作）
	lastSeen    atomic.Int64 // unix milli（原子操作，消除读写 race）
	roomID      string
	deviceID    string
	connID      string // 连接唯一标识，用于多连接在线状态跟踪
	done        chan struct{}
	once        sync.Once
}

func (c *Client) touch() {
	c.lastSeen.Store(time.Now().UnixMilli())
}

// Server /signal WebSocket 服务。
type Server struct {
	deps     *handler.Deps
	group    *group.Engine
	upgrader websocket.Upgrader

	mu      sync.RWMutex
	clients map[string]*Client

	roomMu sync.RWMutex
	rooms  map[string]map[string]bool // roomId -> set(userId)

	shareMu sync.RWMutex
	shares  map[string]*locationShare
}

type locationShare struct {
	chatID      string
	initiatorID string
	expiresAt   int64
	duration    int
	positions   map[string]any
	timer       *time.Timer
}

// NewServer 创建 signal 服务。
func NewServer(d *handler.Deps, g *group.Engine) *Server {
	s := &Server{
		deps:    d,
		group:   g,
		clients: make(map[string]*Client),
		rooms:   make(map[string]map[string]bool),
		shares:  make(map[string]*locationShare),
		upgrader: websocket.Upgrader{
			// 跨域由上游 nginx / 客户端 Token 鉴权保证，与 ws 库默认行为一致
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	if g != nil {
		g.OnSend = s.trySendTo
	}
	return s
}

// RegisterRoutes 注册 /signal 路由。
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/signal", http.HandlerFunc(s.handleUpgrade))
}

// Start 启动心跳与 Redis 订阅（阻塞式跑在 goroutine）。
func (s *Server) Start(ctx context.Context) {
	go s.heartbeatLoop(ctx)
	go s.subscribeImPush(ctx)
	go s.subscribeMomentEvents(ctx)
	go s.subscribeProfileUpdates(ctx)
}

// ============================================================
// Upgrade 与鉴权（S1：强制 token，fail-closed）
// ============================================================

func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		log.Printf("[Signal] WebSocket 鉴权失败: 缺少 token")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	// fail-closed：查询异常直接拒绝
	var userID string
	var isBanned bool
	var expiresAt time.Time
	err := s.deps.DB.Pool.QueryRow(r.Context(),
		`SELECT s."userId", s."expiresAt", u."isBanned" FROM "UserSession" s JOIN "User" u ON u."id"=s."userId" WHERE s."token"=$1`,
		token).Scan(&userID, &expiresAt, &isBanned)
	if err != nil {
		if db.IsNotFound(err) {
			log.Printf("[Signal] WebSocket 鉴权失败: token 无效")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		} else {
			log.Printf("[Signal] WebSocket 鉴权异常: %v", err)
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	if expiresAt.Before(time.Now()) || isBanned {
		log.Printf("[Signal] WebSocket 鉴权失败: token 过期或用户被封禁")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	claimed := r.URL.Query().Get("userId")
	if claimed != "" && claimed != userID {
		log.Printf("[Signal] userId 参数(%s)与 token 身份(%s)不一致，已忽略该参数", claimed, userID)
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[Signal] upgrade 失败: %v", err)
		return
	}
	s.addClient(userID, conn, r)
}

func (s *Server) addClient(userID string, conn *websocket.Conn, r *http.Request) {
	c := &Client{
		userID: userID,
		conn:   conn,
		send:   make(chan []byte, 256),
		done:   make(chan struct{}),
		connID: misc.NewConnID(),
	}
	c.touch()
	c.deviceID = userID + "_" + itoa(time.Now().UnixMilli())

	s.mu.Lock()
	if prev, ok := s.clients[userID]; ok {
		// 同用户旧连接先关掉，避免双连接互踢
		prev.close(4000, "replaced")
	}
	s.clients[userID] = c
	s.mu.Unlock()

	ctx := context.Background()
	misc.SetUserOnlineConn(s.deps, userID, c.connID)
	// 设备信息（简化：UA + IP）
	ua := r.UserAgent()
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	_ = s.deps.Redis.HSet(ctx, "user:devices:"+userID, c.deviceID,
		`{"ua":`+jsonQuote(ua)+`,"ip":`+jsonQuote(ip)+`,"onlineAt":`+itoa(time.Now().UnixMilli())+`}`)

	log.Printf("[Signal] 用户连接: %s", userID)

	// 推送当前在线用户列表
	s.mu.RLock()
	onlineIDs := make([]string, 0, len(s.clients))
	for id := range s.clients {
		if id != userID {
			onlineIDs = append(onlineIDs, id)
		}
	}
	s.mu.RUnlock()
	if len(onlineIDs) > 0 {
		c.enqueue(mustJSON(map[string]any{"type": "online_users_list", "payload": map[string]any{"userIds": onlineIDs}}))
	}
	// 广播上线
	s.broadcastExcept(userID, mustJSON(map[string]any{"type": "friend_online", "from": userID, "payload": map[string]any{"userId": userID}}))

	go c.writePump()
	go s.readPump(c)
}

func (c *Client) close(code int, reason string) {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
		_ = c.conn.Close()
	})
}

// enqueue 非阻塞入队，队列满则丢弃并返回 false。
func (c *Client) enqueue(b []byte) bool {
	select {
	case c.send <- b:
		c.pendingByte.Add(int64(len(b)))
		return true
	default:
		return false
	}
}

// dequeuePending 出队时扣减 pending 字节（仅 writePump 调用）。
func (c *Client) dequeuePending(b []byte) {
	c.pendingByte.Add(-int64(len(b)))
}

// pendingBytes 返回真实待发送字节数。
func (c *Client) pendingBytes() int64 {
	return c.pendingByte.Load()
}

func (c *Client) writePump() {
	defer c.conn.Close()
	for {
		select {
		case <-c.done:
			return
		case b := <-c.send:
			c.dequeuePending(b)
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		}
	}
}

func (s *Server) readPump(c *Client) {
	defer s.removeClient(c)
	c.conn.SetReadLimit(1 << 20) // 1MB
	c.conn.SetPongHandler(func(string) error {
		c.touch()
		return nil
	})
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		c.touch()
		s.handleMessage(c, data)
	}
}

func (s *Server) removeClient(c *Client) {
	userID := c.userID
	// 离开房间
	s.roomMu.Lock()
	if c.roomID != "" {
		if room, ok := s.rooms[c.roomID]; ok {
			delete(room, userID)
			if len(room) == 0 {
				delete(s.rooms, c.roomID)
			} else {
				members := roomMembers(room)
				go s.broadcastToRoom(c.roomID, mustJSON(map[string]any{
					"type": "room_info", "from": userID, "roomId": c.roomID,
					"payload": map[string]any{"event": "peer_left", "peerId": userID, "members": members},
				}), "")
			}
		}
		c.roomID = ""
	}
	s.roomMu.Unlock()

	s.mu.Lock()
	if cur, ok := s.clients[userID]; ok && cur == c {
		delete(s.clients, userID)
	} else {
		s.mu.Unlock()
		log.Printf("[Signal] 忽略旧连接断开: %s", userID)
		return
	}
	s.mu.Unlock()

	ctx := context.Background()
	misc.SetUserOfflineConn(s.deps, userID, c.connID)
	_ = s.deps.Redis.HDel(ctx, "user:devices:"+userID, c.deviceID)
	log.Printf("[Signal] 用户断开: %s", userID)
	s.broadcastExcept(userID, mustJSON(map[string]any{"type": "friend_offline", "from": userID, "payload": map[string]any{"userId": userID}}))
}

// ============================================================
// 发送原语（S12：本机无连接时走 Redis 跨节点投递）
// ============================================================

// SendTo 发送给用户：本机有连接直接投递，否则经 Redis 发布到持有连接的节点。
func (s *Server) SendTo(userID string, msg any) {
	s.sendTo(userID, msg)
}

// sendTo 发送给用户：本机有连接直接投递，否则经 Redis 发布到持有连接的节点。
func (s *Server) sendTo(userID string, msg any) {
	s.trySendTo(userID, mustJSON(msg))
}

// trySendTo 返回是否经本机 WS 投递成功。
func (s *Server) trySendTo(userID string, raw []byte) bool {
	s.mu.RLock()
	c, ok := s.clients[userID]
	s.mu.RUnlock()
	if ok {
		// 背压保护：待发送字节超限则转 Redis 投递，避免单连接拖垮
		if int64(len(c.send)) < int64(cap(c.send)) && c.pendingBytes() < wsBackpressureLimit {
			c.enqueue(raw)
			return true
		}
	}
	// 本机无连接或背压：跨节点投递（带源节点标记，打破回投循环）
	_ = s.publishImPush(userID, raw)
	return false
}

// sendRaw 发送已序列化的载荷（profile 广播等用）。
func (s *Server) sendRaw(userID string, raw []byte) {
	s.trySendTo(userID, raw)
}

func (s *Server) broadcastExcept(exclude string, raw []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, c := range s.clients {
		if id != exclude {
			c.enqueue(raw)
		}
	}
}

func (s *Server) broadcastToRoom(roomID string, raw []byte, exclude string) {
	s.roomMu.RLock()
	room := s.rooms[roomID]
	members := make([]string, 0, len(room))
	for uid := range room {
		if uid != exclude {
			members = append(members, uid)
		}
	}
	s.roomMu.RUnlock()
	for _, uid := range members {
		s.sendRaw(uid, raw)
	}
}

func roomMembers(room map[string]bool) []string {
	m := make([]string, 0, len(room))
	for uid := range room {
		m = append(m, uid)
	}
	return m
}

// publishImPush 跨节点投递（失败仅日志）。
func (s *Server) publishImPush(userID string, payload any) error {
	if userID == "" {
		return nil
	}
	var raw json.RawMessage
	switch p := payload.(type) {
	case []byte:
		raw = p
	case json.RawMessage:
		raw = p
	default:
		b, err := json.Marshal(p)
		if err != nil {
			return err
		}
		raw = b
	}
	env, _ := json.Marshal(map[string]any{"userId": userID, "payload": raw, "srcNode": misc.NodeID()})
	if err := s.deps.Redis.Publish(context.Background(), imPushChannel, string(env)); err != nil {
		log.Printf("[IM Push] PUBLISH 失败 userId=%s: %v", userID, err)
	}
	return nil
}

// ============================================================
// 心跳（30s 轮询，90s 无活动断开）
// ============================================================

func (s *Server) heartbeatLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now().UnixMilli()
			s.mu.RLock()
			clients := make([]*Client, 0, len(s.clients))
			for _, c := range s.clients {
				clients = append(clients, c)
			}
			s.mu.RUnlock()
			for _, c := range clients {
				lastSeen := c.lastSeen.Load()
				if lastSeen > 0 && now-lastSeen > 90000 {
					log.Printf("[Signal] 空闲超时断开: %s idle=%dms", c.userID, now-lastSeen)
					s.removeClient(c)
					c.close(4001, "idle timeout")
					continue
				}
				_ = c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
				misc.RefreshUserOnlineConn(s.deps, c.userID, c.connID)
			}
		}
	}
}

// ============================================================
// Redis 订阅
// ============================================================

func (s *Server) subscribeImPush(ctx context.Context) {
	pubsub := s.deps.Redis.RDB.Subscribe(ctx, imPushChannel)
	defer pubsub.Close()
	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-ch:
			if !ok {
				return
			}
			var env struct {
				UserID  string          `json:"userId"`
				Payload json.RawMessage `json:"payload"`
				SrcNode string          `json:"srcNode"`
			}
			if err := json.Unmarshal([]byte(m.Payload), &env); err != nil || env.UserID == "" {
				continue
			}
			// 只投递本机持有的连接
			s.mu.RLock()
			c, ok := s.clients[env.UserID]
			s.mu.RUnlock()
			if ok && len(env.Payload) > 0 {
				// 回投循环保护：消息源自本节点且本地仍背压时直接丢弃（避免无限回投）
				if env.SrcNode != "" && env.SrcNode == misc.NodeID() &&
					(int64(len(c.send)) >= int64(cap(c.send)) || c.pendingBytes() >= wsBackpressureLimit) {
					log.Printf("[Signal] 回投丢弃(本地背压): %s", env.UserID)
					continue
				}
				if !c.enqueue(env.Payload) {
					// 慢消费者：队列满则断开，由客户端重连后从 DB 拉取
					log.Printf("[Signal] 慢消费者断开: %s", env.UserID)
					s.removeClient(c)
					c.close(4002, "slow consumer")
				}
			}
		}
	}
}

func (s *Server) subscribeMomentEvents(ctx context.Context) {
	pubsub := s.deps.Redis.RDB.Subscribe(ctx, momentEventsCh)
	defer pubsub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-pubsub.Channel():
			if !ok {
				return
			}
			var e struct {
				Type         string          `json:"type"`
				TargetUserID string          `json:"targetUserId"`
				Payload      json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(m.Payload), &e); err != nil || e.Type == "" || e.TargetUserID == "" {
				continue
			}
			s.sendRaw(e.TargetUserID, mustJSON(map[string]any{"type": e.Type, "payload": e.Payload}))
		}
	}
}

func (s *Server) subscribeProfileUpdates(ctx context.Context) {
	pubsub := s.deps.Redis.RDB.Subscribe(ctx, profileUpdatedCh)
	defer pubsub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-pubsub.Channel():
			if !ok {
				return
			}
			var e struct {
				UserID          string   `json:"userId"`
				Nickname        *string  `json:"nickname"`
				Avatar          *string  `json:"avatar"`
				Username        *string  `json:"username"`
				Bio             *string  `json:"bio"`
				BackgroundURL   *string  `json:"backgroundUrl"`
				UpdatedAt       int64    `json:"updatedAt"`
				TargetFriendIDs []string `json:"targetFriendIds"`
				TargetGroupIDs  []string `json:"targetGroupIds"`
			}
			if err := json.Unmarshal([]byte(m.Payload), &e); err != nil || e.UserID == "" {
				continue
			}
			profile := map[string]any{
				"userId": e.UserID, "nickname": e.Nickname, "avatar": e.Avatar,
				"username": e.Username, "bio": e.Bio, "backgroundUrl": e.BackgroundURL,
				"updatedAt": e.UpdatedAt,
			}
			for _, fid := range e.TargetFriendIDs {
				if fid != e.UserID {
					s.sendRaw(fid, mustJSON(map[string]any{"type": "user_profile_updated", "from": e.UserID, "payload": profile}))
				}
			}
			for _, gid := range e.TargetGroupIDs {
				s.roomMu.RLock()
				room := s.rooms[gid]
				var members []string
				for uid := range room {
					if uid != e.UserID {
						members = append(members, uid)
					}
				}
				s.roomMu.RUnlock()
				raw := mustJSON(map[string]any{"type": "user_profile_updated", "from": e.UserID, "payload": profile})
				for _, uid := range members {
					s.sendRaw(uid, raw)
				}
			}
		}
	}
}

// ============================================================
// 小工具
// ============================================================

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// 在线状态 helpers（收敛到 misc，与 redis.ts 同键 online:<userId> / TTL 90s）
func (s *Server) setUserOnline(userID string)     { misc.SetUserOnline(s.deps, userID) }
func (s *Server) refreshUserOnline(userID string) { misc.RefreshUserOnline(s.deps, userID) }
func (s *Server) setUserOffline(userID string)    { misc.SetUserOffline(s.deps, userID) }
func (s *Server) isUserOnline(userID string) bool { return misc.IsUserOnline(s.deps, userID) }
