package id

import (
	"sync"
	"time"
)

// Snowflake 简易雪花 ID（41 位时间 + 10 位节点 + 12 位序列）
type Snowflake struct {
	mu       sync.Mutex
	epoch    int64
	nodeID   int64
	sequence int64
	lastMs   int64
}

func NewSnowflake(nodeID int64) *Snowflake {
	return &Snowflake{
		epoch:  1704067200000, // 2024-01-01 UTC
		nodeID: nodeID & 0x3FF,
	}
}

func (s *Snowflake) Next() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	if now == s.lastMs {
		s.sequence = (s.sequence + 1) & 0xFFF
		if s.sequence == 0 {
			for now <= s.lastMs {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		s.sequence = 0
	}
	s.lastMs = now

	return ((now - s.epoch) << 22) | (s.nodeID << 12) | s.sequence
}
