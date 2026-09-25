// Package group 移植自 server/group-message.ts —— 万人群消息引擎 + 群管理 HTTP API。
//
// 引擎部分供 /signal WS 层调用：连接注册、群在线成员、Redis 群 seq（S11 INCR）、
// 批量落库队列、扇出 worker、跨节点 Redis pub/sub（S12，频道 cqim:im:push）。
// S14-S18 的修复已保留：sendTo 背压 64KB 超限转 Redis 投递、fanout recover 防 panic、
// 落库 UPDATE 用 GREATEST(lastMsgSeq,?) 防并发回写、SendGroupMessage 等落库确认后才返回 ACK。
package group

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// IM_PUSH_CHANNEL 跨节点 IM 推送频道（与 server/publish-im.ts 的 IM_PUSH_CHANNEL 一致）。
const IM_PUSH_CHANNEL = "cqim:im:push"

// ============ 可调参数（环境变量覆盖，与 TS 同名） ============

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Engine 群消息引擎：本机连接注册、群在线成员、seq 生成、批量落库、扇出。
//
// 跨节点投递：OnSend 负责本机 WS 投递，返回 true 表示已在本机投递；
// 返回 false（本机无连接或被背压）时 Engine 内部走 Redis publish 到 cqim:im:push，
// 由持有对端连接的节点订阅投递（见 SubscribeRemotePush）。
type Engine struct {
	deps *handler.Deps

	// OnSend 本机 WS 投递回调（由 signal 服务设置）。
	// 返回 true = 已接收本地投递；false = 本机无连接或背压超限，Engine 将转 Redis 跨节点投递。
	OnSend func(userID string, msg []byte) bool
	// OnOfflinePush 群消息落库并扇出后触发（离线推送钩子，由 push 模块设置；nil 则跳过）。
	OnOfflinePush func(groupID, senderID, senderName string)

	largeGroupConcurrency int
	smallGroupConcurrency int
	largeGroupThreshold   int
	mergeWindow           time.Duration
	mergeMaxBatch         int
	queueMaxSize          int
	dbBatchSize           int
	dbFlushInterval       time.Duration
	fanoutShardSize       int

	mu          sync.Mutex
	onlineUsers map[string]struct{}            // 本机在线用户（RegisterConnection 维护）
	groupOnline map[string]map[string]struct{} // groupId -> 本机在线成员

	seqMu    sync.Mutex
	seqInit  map[string]bool        // Redis seq 是否已初始化（SET NX）
	seqLocal map[string]int64       // 高水位（Redis 成功/回退共用）
	seqFLock map[string]*sync.Mutex // 回退路径的群级互斥

	queue   *batchQueue
	mergeMu sync.Mutex
	merges  map[string]*mergeBuffer // key: groupId:userId

	workersMu sync.Mutex
	workers   map[string]*fanoutWorker

	closed atomic.Bool
	wg     sync.WaitGroup // 后台任务（flush/扇出）

	shutdownOnce sync.Once
}

// NewEngine 创建群消息引擎。
func NewEngine(d *handler.Deps) *Engine {
	e := &Engine{
		deps:                  d,
		largeGroupConcurrency: envInt("LARGE_GROUP_CONCURRENCY", 50),
		smallGroupConcurrency: envInt("SMALL_GROUP_CONCURRENCY", 200),
		largeGroupThreshold:   envInt("LARGE_GROUP_THRESHOLD", 500),
		mergeWindow:           time.Duration(envInt("MERGE_WINDOW_MS", 80)) * time.Millisecond,
		mergeMaxBatch:         envInt("MERGE_MAX_BATCH", 20),
		queueMaxSize:          envInt("QUEUE_MAX_SIZE", 100000),
		dbBatchSize:           envInt("DB_BATCH_SIZE", 100),
		dbFlushInterval:       time.Duration(envInt("DB_FLUSH_INTERVAL_MS", 50)) * time.Millisecond,
		fanoutShardSize:       envInt("FANOUT_SHARD_SIZE", 200),
		onlineUsers:           make(map[string]struct{}),
		groupOnline:           make(map[string]map[string]struct{}),
		seqInit:               make(map[string]bool),
		seqLocal:              make(map[string]int64),
		seqFLock:              make(map[string]*sync.Mutex),
		merges:                make(map[string]*mergeBuffer),
		workers:               make(map[string]*fanoutWorker),
	}
	e.queue = newBatchQueue(e)
	return e
}

// ============ 连接与在线成员管理 ============

// RegisterConnection 注册本机用户连接（signal 服务在 WS 建连时调用）。
func (e *Engine) RegisterConnection(userID string) {
	e.mu.Lock()
	e.onlineUsers[userID] = struct{}{}
	e.mu.Unlock()
}

// UnregisterConnection 注销本机用户连接，并将其从所有群在线集合中移除。
func (e *Engine) UnregisterConnection(userID string) {
	e.mu.Lock()
	delete(e.onlineUsers, userID)
	var groups []string
	for gid, members := range e.groupOnline {
		if _, ok := members[userID]; ok {
			delete(members, userID)
			groups = append(groups, gid)
		}
	}
	e.mu.Unlock()
	if e.deps.Redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for _, gid := range groups {
			if err := e.deps.Redis.SRem(ctx, "group:online:"+gid, userID); err != nil {
				log.Printf("[GroupMsg] SREM group:online:%s 失败: %v", gid, err)
			}
		}
	}
}

// IsOnline 本机是否有该用户的连接。
func (e *Engine) IsOnline(userID string) bool {
	e.mu.Lock()
	_, ok := e.onlineUsers[userID]
	e.mu.Unlock()
	return ok
}

// JoinGroupOnline 加入群在线集合（本机缓存 + Redis 集合）。
func (e *Engine) JoinGroupOnline(groupID, userID string) {
	e.mu.Lock()
	m, ok := e.groupOnline[groupID]
	if !ok {
		m = make(map[string]struct{})
		e.groupOnline[groupID] = m
	}
	m[userID] = struct{}{}
	e.mu.Unlock()
	if e.deps.Redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := e.deps.Redis.SAdd(ctx, "group:online:"+groupID, userID); err != nil {
			log.Printf("[GroupMsg] SADD group:online:%s 失败: %v", groupID, err)
		}
	}
}

// LeaveGroupOnline 离开群在线集合（本机缓存 + Redis 集合）。
func (e *Engine) LeaveGroupOnline(groupID, userID string) {
	e.mu.Lock()
	if m, ok := e.groupOnline[groupID]; ok {
		delete(m, userID)
	}
	e.mu.Unlock()
	if e.deps.Redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := e.deps.Redis.SRem(ctx, "group:online:"+groupID, userID); err != nil {
			log.Printf("[GroupMsg] SREM group:online:%s 失败: %v", groupID, err)
		}
	}
}

// InitGroupOnline 初始化群的本机在线集合（建群时调用）。
func (e *Engine) InitGroupOnline(groupID string) {
	e.mu.Lock()
	if _, ok := e.groupOnline[groupID]; !ok {
		e.groupOnline[groupID] = make(map[string]struct{})
	}
	e.mu.Unlock()
}

// localGroupMembers 本机群在线成员快照。
func (e *Engine) localGroupMembers(groupID string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.groupOnline[groupID]
	out := make([]string, 0, len(m))
	for uid := range m {
		out = append(out, uid)
	}
	return out
}

// GetGroupOnlineMembers 获取群在线成员（S18：本机集合与 Redis group:online:{groupId} 的并集）。
// Redis 失败时降级为仅返回本机集合。
func (e *Engine) GetGroupOnlineMembers(groupID string) []string {
	set := make(map[string]struct{})
	for _, uid := range e.localGroupMembers(groupID) {
		set[uid] = struct{}{}
	}
	if e.deps.Redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if members, err := e.deps.Redis.SMembers(ctx, "group:online:"+groupID); err == nil {
			for _, uid := range members {
				set[uid] = struct{}{}
			}
		} else {
			log.Printf("[GroupMsg] SMEMBERS group:online:%s 失败: %v", groupID, err)
		}
	}
	out := make([]string, 0, len(set))
	for uid := range set {
		out = append(out, uid)
	}
	return out
}

// ============ 跨节点投递（S12：照抄 publish-im.ts 逻辑） ============

// imPushEnvelope 与 TS ImPushEnvelope 一致：{ userId, payload }。
type imPushEnvelope struct {
	UserID  string          `json:"userId"`
	Payload json.RawMessage `json:"payload"`
}

// publishImPush 发布跨节点 IM 推送；失败仅打日志，不抛错（与 TS publishImPush 一致）。
func (e *Engine) publishImPush(userID string, payload []byte) {
	if userID == "" || e.deps.Redis == nil {
		return
	}
	env, err := json.Marshal(imPushEnvelope{UserID: userID, Payload: json.RawMessage(payload)})
	if err != nil {
		log.Printf("[IM Push] envelope 序列化失败 userId=%s: %v", userID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := e.deps.Redis.Publish(ctx, IM_PUSH_CHANNEL, string(env)); err != nil {
		log.Printf("[IM Push] PUBLISH 失败 userId=%s: %v", userID, err)
	}
}

// SubscribeRemotePush 订阅跨节点推送（signal 服务在启动时调用一次）。
// 收到的 envelope 交给 handler(userID, payload) 做本机 WS 投递。返回取消函数。
func (e *Engine) SubscribeRemotePush(handler func(userID string, payload []byte)) (unsubscribe func()) {
	if e.deps.Redis == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	sub := e.deps.Redis.RDB.Subscribe(ctx, IM_PUSH_CHANNEL)
	// 确认订阅建立
	if _, err := sub.Receive(ctx); err != nil {
		log.Printf("[IM Push] 订阅 %s 失败: %v", IM_PUSH_CHANNEL, err)
		cancel()
		return func() {}
	}
	log.Printf("[IM Push] 已订阅 %s", IM_PUSH_CHANNEL)
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer sub.Close()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var env imPushEnvelope
				if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
					log.Printf("[IM Push] 消息解析失败: %v", err)
					continue
				}
				if env.UserID == "" {
					continue
				}
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[IM Push] handler panic: %v", r)
						}
					}()
					handler(env.UserID, []byte(env.Payload))
				}()
			}
		}
	}()
	return func() { cancel() }
}

// ============ sendTo：本机投递 + 背压 + Redis 兜底（S15） ============

// SendTo 向单个用户投递消息。
// OnSend 返回 true 表示本机已投递；返回 false（本机无连接或写队列背压超过 64KB）
// 时转 Redis publish，由持有该用户连接的节点投递。回调 panic 会被 recover 并视为未投递。
func (e *Engine) SendTo(userID string, msg []byte) {
	if userID == "" {
		return
	}
	if e.OnSend != nil {
		delivered := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[GroupMsg] OnSend panic userId=%s: %v", userID, r)
					delivered = false
				}
			}()
			delivered = e.OnSend(userID, msg)
		}()
		if delivered {
			return
		}
	}
	// 本机无连接或被背压：走 Redis 跨节点投递（S15：不再直接丢弃）
	e.publishImPush(userID, msg)
}

// ============ 群 seq 生成（S11：多节点安全） ============

const seqRedisKeyPrefix = "group:seq:"

// ensureSeqInit 进程首次为某群生成 seq 时，用 SET NX 从 DB lastMsgSeq 恢复基准；
// 多节点并发初始化时只有一个节点的基准写入成功，后续 INCR 都基于它。
func (e *Engine) ensureSeqInit(ctx context.Context, groupID string) error {
	e.seqMu.Lock()
	if e.seqInit[groupID] {
		e.seqMu.Unlock()
		return nil
	}
	// 本地高水位：key 被驱逐后重建时，不能只信 DB（批量落库可能还没刷 lastMsgSeq），
	// 取 max(DB 基准, 本地高水位)，防 seq 回退重复。
	localHigh := e.seqLocal[groupID]
	e.seqMu.Unlock()

	var base int64
	if g, err := db.QueryRowToStruct[db.GroupSeqOnly](ctx, e.deps.DB,
		`SELECT "lastMsgSeq" FROM "Group" WHERE "id"=$1`, groupID); err == nil {
		base = g.LastMsgSeq
	} else if !db.IsNotFound(err) {
		return err
	}
	if localHigh > base {
		base = localHigh
	}
	if e.deps.Redis == nil {
		return errRedisUnavailable
	}
	// redisx 未封装 SetNX，直接使用导出的 RDB（不修改 redisx 包）
	if err := e.deps.Redis.RDB.SetNX(ctx, seqRedisKeyPrefix+groupID, base, 0).Err(); err != nil {
		return err
	}
	e.seqMu.Lock()
	e.seqInit[groupID] = true
	e.seqMu.Unlock()
	return nil
}

var errRedisUnavailable = errString("redis unavailable")

type errString string

func (s errString) Error() string { return string(s) }

// nextSeq Redis INCR 原子递增。
//
// S11 安全降级：Redis 不可用时默认 fail-closed（返回错误，拒绝发送），因为多节点下
// 进程内计数器无法保证 seq 唯一，重复 seq 会永久破坏消息顺序。单节点部署可通过环境变量
// CQIM_SEQ_LOCAL_FALLBACK=1 显式开启进程内回退（以内存高水位/DB 为起点）。
func (e *Engine) nextSeq(ctx context.Context, groupID string) (int64, error) {
	if err := e.ensureSeqInit(ctx, groupID); err == nil {
		if n, err := e.deps.Redis.Incr(ctx, seqRedisKeyPrefix+groupID); err == nil {
			e.seqMu.Lock()
			e.seqLocal[groupID] = n // 记录高水位，供 key 驱逐重建使用
			e.seqMu.Unlock()
			return n, nil
		} else {
			log.Printf("[GroupMsg] Redis seq 失败: groupId=%s %v", groupID, err)
		}
		e.seqMu.Lock()
		delete(e.seqInit, groupID)
		e.seqMu.Unlock()
	} else {
		log.Printf("[GroupMsg] Redis seq 初始化失败: groupId=%s %v", groupID, err)
	}
	if os.Getenv("CQIM_SEQ_LOCAL_FALLBACK") == "1" {
		log.Printf("[GroupMsg] 已开启本地 seq 回退（单节点模式）: groupId=%s", groupID)
		return e.nextSeqLocal(ctx, groupID)
	}
	return 0, errors.New("消息服务暂时不可用，请稍后重试")
}

func (e *Engine) seqFallbackLock(groupID string) *sync.Mutex {
	e.seqMu.Lock()
	defer e.seqMu.Unlock()
	mu, ok := e.seqFLock[groupID]
	if !ok {
		mu = &sync.Mutex{}
		e.seqFLock[groupID] = mu
	}
	return mu
}

// nextSeqLocal Redis 故障时的回退实现：群级互斥（单节点语义）。
func (e *Engine) nextSeqLocal(ctx context.Context, groupID string) (int64, error) {
	mu := e.seqFallbackLock(groupID)
	mu.Lock()
	defer mu.Unlock()

	e.seqMu.Lock()
	cur, ok := e.seqLocal[groupID]
	e.seqMu.Unlock()
	if !ok {
		if g, err := db.QueryRowToStruct[db.Group](ctx, e.deps.DB,
			`SELECT "lastMsgSeq" FROM "Group" WHERE "id"=$1`, groupID); err == nil {
			cur = g.LastMsgSeq
		} else if !db.IsNotFound(err) {
			return 0, err
		}
	}
	cur++
	e.seqMu.Lock()
	e.seqLocal[groupID] = cur
	e.seqMu.Unlock()
	return cur, nil
}

// ============ 优雅关闭（S17：先停接入，再排空落库） ============

// Shutdown 优雅关闭：停止接收新消息，刷出合并缓冲，排空批量落库队列。
// 调用方应先停止 WS/HTTP 接入（不再调用 SendGroupMessage），再调用 Shutdown。
func (e *Engine) Shutdown() {
	e.shutdownOnce.Do(func() {
		e.closed.Store(true)
		e.queue.shutdown()
		// 刷出所有合并缓冲（走 SendTo，保证投递）
		e.mergeMu.Lock()
		bufs := make([]*mergeBuffer, 0, len(e.merges))
		for key, b := range e.merges {
			if b.timer != nil {
				b.timer.Stop()
				b.timer = nil
			}
			bufs = append(bufs, b)
			delete(e.merges, key)
		}
		e.mergeMu.Unlock()
		for _, b := range bufs {
			e.deliverMerge(b)
		}
		e.wg.Wait()
	})
}
