package group

// 群 seq：Redis key 运行中被驱逐后的重建测试（S11）。
//
// 需要本地 Redis：默认连 127.0.0.1:6399（CI 可通过 CQIM_TEST_REDIS 覆盖）。
// 无 Redis 时自动跳过。DB 传 nil：ensureSeqInit 此时仅用本地高水位做基准。

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/redisx"
)

func pingOK(c *redisx.Client) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return c.RDB.Ping(ctx).Err() == nil
}

func testRedis(t *testing.T) *redisx.Client {
	t.Helper()
	addr := os.Getenv("CQIM_TEST_REDIS")
	if addr == "" {
		addr = "redis://127.0.0.1:6399"
	}
	c := redisx.New(addr)
	if pingOK(c) {
		return c
	}
	// 无真实 Redis：起一个进程内最小 RESP 服务器（只实现测试用到的命令），
	// 让驱逐重建逻辑得到真实验证而不是跳过。
	if os.Getenv("CQIM_TEST_REDIS") == "" {
		if ln, err := net.Listen("tcp", "127.0.0.1:6399"); err == nil {
			m := &miniRedis{kv: make(map[string]string)}
			go func() {
				for {
					conn, err := ln.Accept()
					if err != nil {
						return
					}
					go m.serveConn(conn)
				}
			}()
			t.Cleanup(func() { ln.Close() })
			if pingOK(c) {
				t.Log("使用进程内 fake Redis 进行验证")
				return c
			}
			ln.Close()
		}
	}
	t.Skipf("无测试 Redis，跳过")
	return nil
}

// miniRedis 最小 RESP2 服务器：仅支持 PING/INCR/SET(NX)/DEL/CLIENT，
// 刚好覆盖群 seq 逻辑用到的命令。不是通用实现，仅供单测。
type miniRedis struct {
	mu sync.Mutex
	kv map[string]string
}

func (m *miniRedis) serveConn(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		args, err := readRespArray(r)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		m.mu.Lock()
		var resp string
		switch strings.ToUpper(args[0]) {
		case "PING":
			resp = "+PONG\r\n"
		case "HELLO":
			// 不支持 RESP3：返回错误让 go-redis 回退到 RESP2（实测回退可用）
			resp = "-ERR unknown command\r\n"
		case "CLIENT":
			resp = "+OK\r\n"
		case "INCR":
			v, _ := strconv.ParseInt(m.kv[args[1]], 10, 64)
			v++
			m.kv[args[1]] = strconv.FormatInt(v, 10)
			resp = ":" + strconv.FormatInt(v, 10) + "\r\n"
		case "SET":
			// SET key val [EX s] [NX]：NX 可出现在 args[3] 或 args[5]
			nx := false
			for _, a := range args[3:] {
				if strings.ToUpper(a) == "NX" {
					nx = true
					break
				}
			}
			if _, ok := m.kv[args[1]]; ok && nx {
				resp = "$-1\r\n" // nil：NX 未写入
			} else {
				m.kv[args[1]] = args[2]
				resp = "+OK\r\n"
			}
		case "SETNX":
			// go-redis 无过期时间的 SetNX 发原生 SETNX：:1=写入，:0=已存在
			if _, ok := m.kv[args[1]]; ok {
				resp = ":0\r\n"
			} else {
				m.kv[args[1]] = args[2]
				resp = ":1\r\n"
			}
		case "DEL":
			n := 0
			for _, k := range args[1:] {
				if _, ok := m.kv[k]; ok {
					delete(m.kv, k)
					n++
				}
			}
			resp = ":" + strconv.Itoa(n) + "\r\n"
		default:
			resp = "-ERR unknown command\r\n"
		}
		m.mu.Unlock()
		if _, err := conn.Write([]byte(resp)); err != nil {
			return
		}
	}
}

func readRespArray(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 || line[0] != '*' {
		return nil, fmt.Errorf("bad array header: %q", line)
	}
	n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(hdr) < 2 || hdr[0] != '$' {
			return nil, fmt.Errorf("bad bulk header: %q", hdr)
		}
		l, err := strconv.Atoi(strings.TrimSpace(hdr[1:]))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, l+2)
		if _, err := readFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:l]))
	}
	return args, nil
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func TestNextSeqKeyEvictionRebuild(t *testing.T) {
	c := testRedis(t)
	ctx := context.Background()
	groupID := fmt.Sprintf("test-evict-%d", time.Now().UnixNano())
	e := NewEngine(&handler.Deps{Redis: c})
	defer func() {
		_ = c.RDB.Del(ctx, seqRedisKeyPrefix+groupID).Err()
	}()

	// 正常递增 1..3
	for want := int64(1); want <= 3; want++ {
		n, err := e.nextSeq(ctx, groupID)
		if err != nil {
			t.Fatalf("nextSeq 失败: %v", err)
		}
		if n != want {
			t.Fatalf("nextSeq = %d, want %d", n, want)
		}
	}

	// 模拟运行中 key 被逐出（无 TTL + maxmemory 策略）
	if err := c.RDB.Del(ctx, seqRedisKeyPrefix+groupID).Err(); err != nil {
		t.Fatalf("删除 seq key 失败: %v", err)
	}

	// 下一次 nextSeq：INCR 从 0 开始得 1，1 <= 高水位 3 → 必须重建，返回 4
	n, err := e.nextSeq(ctx, groupID)
	if err != nil {
		t.Fatalf("驱逐后 nextSeq 失败: %v", err)
	}
	if n != 4 {
		t.Fatalf("驱逐重建后 seq = %d, want 4（回退会导致 (groupId,seq) 唯一冲突）", n)
	}

	// 高水位单调：继续递增，不回退
	n, err = e.nextSeq(ctx, groupID)
	if err != nil {
		t.Fatalf("nextSeq 失败: %v", err)
	}
	if n != 5 {
		t.Fatalf("重建后继续递增 seq = %d, want 5", n)
	}
}

func TestNextSeqEvictionBeforeFirstSend(t *testing.T) {
	c := testRedis(t)
	ctx := context.Background()
	groupID := fmt.Sprintf("test-evict0-%d", time.Now().UnixNano())
	e := NewEngine(&handler.Deps{Redis: c})
	defer func() {
		_ = c.RDB.Del(ctx, seqRedisKeyPrefix+groupID).Err()
	}()

	// 首次 nextSeq 完成初始化（DB 为 nil 时基准为 0）
	if n, err := e.nextSeq(ctx, groupID); err != nil || n != 1 {
		t.Fatalf("首次 nextSeq = %d, err = %v, want 1", n, err)
	}
	// 白盒模拟：初始化时 DB lastMsgSeq=100（生产环境 DB 必存在），
	// 但 key 在首次发送前就被驱逐。此时高水位为 1，INCR 重得 1，
	// 仅靠“n <= 高水位”会漏检，必须用初始化基准兜底。
	e.seqMu.Lock()
	e.seqBase[groupID] = 100
	e.seqLocal[groupID] = 1
	e.seqMu.Unlock()
	if err := c.RDB.Del(ctx, seqRedisKeyPrefix+groupID).Err(); err != nil {
		t.Fatalf("删除 seq key 失败: %v", err)
	}

	n, err := e.nextSeq(ctx, groupID)
	if err != nil {
		t.Fatalf("驱逐后 nextSeq 失败: %v", err)
	}
	// 检测必须触发：不能返回驱逐后 INCR 的 stale 值 1。
	// 注：本测试 DB 为 nil，重建基准取本地高水位；生产环境 DB 存在时基准为 max(DB, 高水位)。
	if n == 1 {
		t.Fatalf("驱逐检测漏检：返回了 stale seq 1，会导致 seq 回退重复")
	}
}
