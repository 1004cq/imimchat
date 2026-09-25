package auth

// 验证码原子消费的并发安全测试（审计项：验证码并发复用）。
//
// 需要本地 Redis：测试默认连 127.0.0.1:6399（CI 可通过 CQIM_TEST_REDIS 覆盖）。
// 无 Redis 时自动跳过。

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/redisx"
)

func luaResultStr(v any, err error) string {
	if err != nil {
		return "ERR:" + err.Error()
	}
	res, _ := luaResult(v)
	return res
}

func testRedis(t *testing.T) *redisx.Client {
	t.Helper()
	addr := os.Getenv("CQIM_TEST_REDIS")
	if addr == "" {
		addr = "redis://127.0.0.1:6399"
	}
	c := redisx.New(addr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.RDB.Ping(ctx).Err(); err != nil {
		t.Skipf("无测试 Redis，跳过: %v", err)
	}
	return c
}

func seedCodeRecord(t *testing.T, c *redisx.Client, key, code string) {
	t.Helper()
	ctx := context.Background()
	rec := verifyCodeRecord{Code: code, Used: false, Attempts: 0}
	raw, _ := json.Marshal(rec)
	if err := c.RDB.Set(ctx, key, raw, time.Minute).Err(); err != nil {
		t.Fatalf("seed 失败: %v", err)
	}
}

// TestVerifyCodeConsumeConcurrent 50 个 goroutine 同时用正确验证码消费，
// 必须恰好 1 个成功（OK），其余全部 USED。
func TestVerifyCodeConsumeConcurrent(t *testing.T) {
	c := testRedis(t)
	ctx := context.Background()
	key := "test:verify:concurrent"
	seedCodeRecord(t, c, key, "123456")
	defer c.RDB.Del(ctx, key)

	const n = 50
	var okCount, usedCount int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := luaResultStr(c.Eval(ctx, verifyCodeConsumeLua, []string{key}, "123456", "5"))
			switch res {
			case "OK":
				atomic.AddInt64(&okCount, 1)
			case "USED":
				atomic.AddInt64(&usedCount, 1)
			default:
				t.Errorf("意外结果: %q", res)
			}
		}()
	}
	wg.Wait()
	if okCount != 1 {
		t.Fatalf("正确验证码应恰好消费成功 1 次，实际 %d 次", okCount)
	}
	if usedCount != n-1 {
		t.Fatalf("其余 %d 次应返回 USED，实际 %d 次", n-1, usedCount)
	}
}

// TestVerifyCodeWrongAttemptsConcurrent 10 个 goroutine 同时输错验证码，
// attempts 必须精确 +10（无丢失更新），且第 5 次后锁定。
func TestVerifyCodeWrongAttemptsConcurrent(t *testing.T) {
	c := testRedis(t)
	ctx := context.Background()
	key := "test:verify:wrong"
	seedCodeRecord(t, c, key, "123456")
	defer c.RDB.Del(ctx, key)

	const n = 10
	var lockedCount int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := luaResultStr(c.Eval(ctx, verifyCodeConsumeLua, []string{key}, "000000", "5"))
			if res == "LOCKED" {
				atomic.AddInt64(&lockedCount, 1)
			} else if res != "WRONG" {
				t.Errorf("意外结果: %q", res)
			}
		}()
	}
	wg.Wait()

	// 前 5 次 WRONG（attempts 1..5，第 5 次同时锁定），后 5 次 LOCKED
	if lockedCount != 5 {
		t.Fatalf("5 次错误后应锁定，后续 5 次应返回 LOCKED，实际 LOCKED %d 次", lockedCount)
	}
	// 再用正确验证码也必须失败（已锁定/已使用）
	res := luaResultStr(c.Eval(ctx, verifyCodeConsumeLua, []string{key}, "123456", "5"))
	if res == "OK" {
		t.Fatal("锁定后正确验证码不应消费成功")
	}
}

// TestVerifyCodeConfirmConcurrent 阿里云短信确认消费的 CAS 并发测试。
func TestVerifyCodeConfirmConcurrent(t *testing.T) {
	c := testRedis(t)
	ctx := context.Background()
	key := "test:verify:confirm"
	seedCodeRecord(t, c, key, "__aliyun__")
	defer c.RDB.Del(ctx, key)

	const n = 30
	var okCount int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := luaResultStr(c.Eval(ctx, verifyCodeConfirmLua, []string{key}))
			if res == "OK" {
				atomic.AddInt64(&okCount, 1)
			} else if res != "USED" {
				t.Errorf("意外结果: %q", res)
			}
		}()
	}
	wg.Wait()
	if okCount != 1 {
		t.Fatalf("确认消费应恰好成功 1 次，实际 %d 次", okCount)
	}
}
