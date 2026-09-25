package moments

import (
	"sync"
	"time"
)

// ============ 朋友圈专用限流器（对应 moments.ts 的 MomentsRateLimiter） ============

type rlEntry struct {
	count   int
	resetAt time.Time
}

type rateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	store  map[string]*rlEntry
}

func newRateLimiter(window time.Duration, max int) *rateLimiter {
	l := &rateLimiter{window: window, max: max, store: make(map[string]*rlEntry)}
	go func() {
		t := time.NewTicker(min(window, time.Minute))
		defer t.Stop()
		for range t.C {
			now := time.Now()
			l.mu.Lock()
			for k, e := range l.store {
				if now.After(e.resetAt) {
					delete(l.store, k)
				}
			}
			l.mu.Unlock()
		}
	}()
	return l
}

// check 计数+1，返回是否在限额内（与 TS 的 allowed = count <= maxRequests 语义一致）。
func (l *rateLimiter) check(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.store[key]
	if !ok || now.After(e.resetAt) {
		e = &rlEntry{resetAt: now.Add(l.window)}
		l.store[key] = e
	}
	e.count++
	return e.count <= l.max
}

var (
	// 发布限流：每用户每分钟最多 5 条动态
	publishLimiter = newRateLimiter(time.Minute, 5)
	// 点赞限流：每用户每分钟最多 30 次
	likeLimiter = newRateLimiter(time.Minute, 30)
	// 评论限流：每用户每分钟最多 20 条
	commentLimiter = newRateLimiter(time.Minute, 20)
)
