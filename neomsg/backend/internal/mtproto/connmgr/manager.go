package connmgr

import (
	"fmt"
	"sync"
)

// Sender 可向客户端发送加密 MTProto 载荷
type Sender interface {
	SendEncrypted(body []byte) error
	UserID() int64
	DeviceID() string
}

type Manager struct {
	mu    sync.RWMutex
	conns map[string]Sender // key: userID:deviceID
}

func NewManager() *Manager {
	return &Manager{conns: make(map[string]Sender)}
}

func key(userID int64, deviceID string) string {
	return fmt.Sprintf("%d:%s", userID, deviceID)
}

func (m *Manager) Register(userID int64, deviceID string, s Sender) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conns[key(userID, deviceID)] = s
}

func (m *Manager) Unregister(userID int64, deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.conns, key(userID, deviceID))
}

func (m *Manager) Get(userID int64, deviceID string) Sender {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conns[key(userID, deviceID)]
}

func (m *Manager) Send(userID int64, deviceID string, body []byte) error {
	s := m.Get(userID, deviceID)
	if s == nil {
		return fmt.Errorf("connection not found")
	}
	return s.SendEncrypted(body)
}
