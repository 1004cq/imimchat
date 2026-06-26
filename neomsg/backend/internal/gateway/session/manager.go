package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

// Conn 抽象连接（WebSocket 或 TCP）
type Conn interface {
	Send(data []byte) error
	Close() error
	UserID() int64
	DeviceID() string
}

type wsConn struct {
	ws       *websocket.Conn
	userID   int64
	deviceID string
	mu       sync.Mutex
}

func NewWSConn(ws *websocket.Conn, userID int64, deviceID string) Conn {
	return &wsConn{ws: ws, userID: userID, deviceID: deviceID}
}

func (c *wsConn) Send(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws.WriteMessage(websocket.BinaryMessage, data)
}

func (c *wsConn) Close() error { return c.ws.Close() }
func (c *wsConn) UserID() int64 { return c.userID }
func (c *wsConn) DeviceID() string { return c.deviceID }

// Manager 管理 userId -> []Conn 映射
type Manager struct {
	// userID -> deviceID -> Conn
	conns map[int64]map[string]Conn
	// dialogID -> []userID（群成员在线缓存）
	dialogMembers map[int64]map[int64]struct{}
	mu            sync.RWMutex
}

func NewManager() *Manager {
	return &Manager{
		conns:         make(map[int64]map[string]Conn),
		dialogMembers: make(map[int64]map[int64]struct{}),
	}
}

func (m *Manager) Register(userID int64, deviceID string, conn Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conns[userID] == nil {
		m.conns[userID] = make(map[string]Conn)
	}
	m.conns[userID][deviceID] = conn
}

func (m *Manager) Unregister(userID int64, deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if devices, ok := m.conns[userID]; ok {
		delete(devices, deviceID)
		if len(devices) == 0 {
			delete(m.conns, userID)
		}
	}
}

func (m *Manager) GetConns(userID int64) []Conn {
	m.mu.RLock()
	defer m.mu.RUnlock()
	devices := m.conns[userID]
	result := make([]Conn, 0, len(devices))
	for _, c := range devices {
		result = append(result, c)
	}
	return result
}

// BroadcastToDialog 向会话内所有在线成员扇出（排除指定用户）
func (m *Manager) BroadcastToDialog(ctx context.Context, dialogID int64, payload []byte, excludeUser int64) error {
	m.mu.RLock()
	members := m.dialogMembers[dialogID]
	m.mu.RUnlock()

	for userID := range members {
		if userID == excludeUser {
			continue
		}
		for _, conn := range m.GetConns(userID) {
			if err := conn.Send(payload); err != nil {
				// 背压：慢消费者丢弃
				continue
			}
		}
	}
	return nil
}

// BroadcastToUser 向用户所有设备推送
func (m *Manager) BroadcastToUser(userID int64, payload []byte) error {
	for _, conn := range m.GetConns(userID) {
		if err := conn.Send(payload); err != nil {
			fmt.Printf("[Session] send failed user=%d device=%s: %v\n", userID, conn.DeviceID(), err)
		}
	}
	return nil
}

func (m *Manager) OnlineCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0
	for _, devices := range m.conns {
		total += len(devices)
	}
	return total
}
