package middleware

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/redisx"
)

func testRedis(t *testing.T) *redisx.Client {
	t.Helper()
	url := os.Getenv("CQIM_TEST_REDIS")
	if url == "" {
		url = "redis://127.0.0.1:6399"
	}
	c := redisx.New(url)
	if err := c.Ping(context.Background()); err != nil {
		t.Skipf("Redis 不可用，跳过: %v", err)
	}
	return c
}

// TestDistributedRateLimit 多个限流器实例（模拟多节点）共享 Redis 计数，
// 总放行数不应超过 max。
func TestDistributedRateLimit(t *testing.T) {
	c := testRedis(t)
	defer c.Close()
	SetRedisClient(c)
	defer SetRedisClient(nil)

	// 清理旧 key
	ctx := context.Background()
	for _, k := range []string{"ratelimit:test-dist:u1"} {
		_ = c.Del(ctx, k)
	}

	const max = 10
	const nodes = 5
	const perNode = 10 // 每个节点尝试 10 次，总 50 次

	var allowed int64
	var wg sync.WaitGroup
	for n := 0; n < nodes; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 每个 goroutine 用独立的 RateLimiter 实例（模拟不同节点）
			rl := NewRateLimiter("test-dist", time.Minute, max)
			for i := 0; i < perNode; i++ {
				if rl.Allow("u1") {
					atomic.AddInt64(&allowed, 1)
				}
			}
		}()
	}
	wg.Wait()

	if allowed != max {
		t.Fatalf("分布式限流失效：期望恰好放行 %d 次，实际 %d 次", max, allowed)
	}
	_ = c.Del(ctx, "ratelimit:test-dist:u1")
}

// TestRateLimitRedisFailover Redis 不可用时降级为进程内计数，不阻塞请求。
func TestRateLimitRedisFailover(t *testing.T) {
	// 指向一个不存在的 Redis
	c := redisx.New("redis://127.0.0.1:19999")
	SetRedisClient(c)
	defer SetRedisClient(nil)
	defer c.Close()

	rl := NewRateLimiter("test-failover", time.Minute, 3)
	// 前 3 次应放行（降级到本地计数）
	for i := 0; i < 3; i++ {
		if !rl.Allow("u2") {
			t.Fatalf("降级模式下第 %d 次应放行", i+1)
		}
	}
	// 第 4 次应被限
	if rl.Allow("u2") {
		t.Fatal("降级模式下第 4 次应被限流")
	}
	fmt.Println("failover OK")
}
