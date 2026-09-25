package group

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
	"github.com/jackc/pgx/v5"
)

// ============ 类型定义 ============

// SendParams 发送群消息的参数（对应 TS GroupMessagePayload）。
type SendParams struct {
	GroupID      string
	SenderID     string
	SenderName   string
	SenderAvatar string
	MsgType      string
	Content      string
	ReplyToID    string
	Extra        any
}

// GroupMessagePayload 是 SendParams 的别名（兼容旧命名）。
type GroupMessagePayload = SendParams

// PullParams 拉取群历史消息的参数。
type PullParams struct {
	GroupID   string
	UserID    string
	AfterSeq  *int64
	BeforeSeq *int64
	Limit     int
}

// AckParams 确认已读的参数。
type AckParams struct {
	GroupID    string
	UserID     string
	LastAckSeq int64
}

// normalizeExtra 将 Extra(any) 规范化为 JSON bytes；nil/空返回 nil。
func normalizeExtra(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		if len(raw) == 0 || string(raw) == "null" {
			return nil
		}
		return raw
	}
	if b, err := json.Marshal(v); err == nil && len(b) > 0 && string(b) != "null" {
		return json.RawMessage(b)
	}
	return nil
}

// PushMessage 推送给客户端的群消息（对应 TS PushMessage）。
type PushMessage struct {
	Type         string          `json:"type"`
	GroupID      string          `json:"groupId"`
	Seq          int64           `json:"seq"`
	SenderID     string          `json:"senderId"`
	SenderName   string          `json:"senderName"`
	SenderAvatar string          `json:"senderAvatar,omitempty"`
	MsgType      string          `json:"msgType"`
	Content      string          `json:"content"`
	ReplyToID    *string         `json:"replyToId,omitempty"`
	Extra        json.RawMessage `json:"extra,omitempty"`
	Timestamp    int64           `json:"timestamp"`
}

// PulledMessage 拉取历史消息的单条结构（对应 TS pullGroupMessages 返回项）。
type PulledMessage struct {
	ID           string          `json:"id"`
	Seq          int64           `json:"seq"`
	SenderID     string          `json:"senderId"`
	SenderName   string          `json:"senderName"`
	SenderAvatar string          `json:"senderAvatar"`
	MsgType      string          `json:"msgType"`
	Content      string          `json:"content"`
	ReplyToID    *string         `json:"replyToId,omitempty"`
	Extra        json.RawMessage `json:"extra,omitempty"`
	CreatedAt    string          `json:"createdAt"`
	IsRevoked    bool            `json:"isRevoked"`
}

// PullResult 拉取结果。
type PullResult struct {
	Messages  []PulledMessage `json:"messages"`
	HasMore   bool            `json:"hasMore"`
	LatestSeq int64           `json:"latestSeq"`
}

var (
	// ErrNotGroupMember 非群成员（拉取时抛给 HTTP 层映射为 403）。
	ErrNotGroupMember = errors.New("非群成员，无权拉取消息")
	// ErrQueueFull 队列已满。
	ErrQueueFull = errors.New("消息队列已满，请稍后重试")
	// ErrPlaintextRejected 群聊强制 MLS 加密，拒绝明文业务消息。
	ErrPlaintextRejected = errors.New("群聊强制要求 MLS 加密，拒绝写入明文业务消息")
)

// checkMLS 强制 MLS 校验：只允许 mls_encrypted / system（对应 TS flushGroup 内的校验，保留）。
func checkMLS(msgType string) (string, error) {
	mType := msgType
	if mType == "" {
		mType = "mls_encrypted"
	}
	if mType != "mls_encrypted" && mType != "system" {
		return "", ErrPlaintextRejected
	}
	return mType, nil
}

// ============ 批量落库消息队列 ============

type queueItem struct {
	payload SendParams
	seq     int64
	ts      time.Time
	done    chan queueResult
}

type queueResult struct {
	msg PushMessage
	err error
}

// batchQueue 按 groupId 分桶的批量落库队列：满批次立即刷盘，未满则定时刷盘。
type batchQueue struct {
	e       *Engine
	mu      sync.Mutex
	buckets map[string][]*queueItem
	timers  map[string]*time.Timer
	flushMu map[string]*sync.Mutex // 群级刷盘互斥
	total   int
	closed  bool
}

func newBatchQueue(e *Engine) *batchQueue {
	return &batchQueue{
		e:       e,
		buckets: make(map[string][]*queueItem),
		timers:  make(map[string]*time.Timer),
		flushMu: make(map[string]*sync.Mutex),
	}
}

func (q *batchQueue) flushLock(groupID string) *sync.Mutex {
	q.mu.Lock()
	defer q.mu.Unlock()
	mu, ok := q.flushMu[groupID]
	if !ok {
		mu = &sync.Mutex{}
		q.flushMu[groupID] = mu
	}
	return mu
}

func (q *batchQueue) enqueue(item *queueItem) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return errors.New("engine 已关闭")
	}
	if q.total >= q.e.queueMaxSize {
		q.mu.Unlock()
		return ErrQueueFull
	}
	groupID := item.payload.GroupID
	q.buckets[groupID] = append(q.buckets[groupID], item)
	q.total++
	full := len(q.buckets[groupID]) >= q.e.dbBatchSize
	hasTimer := q.timers[groupID] != nil
	if !full && !hasTimer {
		gid := groupID
		q.timers[groupID] = time.AfterFunc(q.e.dbFlushInterval, func() {
			q.flushGroup(gid)
		})
	}
	q.mu.Unlock()
	if full {
		q.e.wg.Add(1)
		go func() {
			defer q.e.wg.Done()
			q.flushGroup(groupID)
		}()
	}
	return nil
}

// flushGroup 取出整桶批量落库。失败时降级逐条重试（与 TS 一致）。
func (q *batchQueue) flushGroup(groupID string) {
	mu := q.flushLock(groupID)
	mu.Lock()
	defer mu.Unlock()

	q.mu.Lock()
	if t, ok := q.timers[groupID]; ok {
		t.Stop()
		delete(q.timers, groupID)
	}
	batch := q.buckets[groupID]
	if len(batch) == 0 {
		q.mu.Unlock()
		return
	}
	q.buckets[groupID] = nil
	q.total -= len(batch)
	q.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := q.flushBatch(ctx, groupID, batch); err != nil {
		log.Printf("[GroupMsg] 批量落库失败(%d条), groupId=%s, 降级逐条重试: %v", len(batch), groupID, err)
		for _, item := range batch {
			if err := q.flushSingle(ctx, item); err != nil {
				item.done <- queueResult{err: err}
			}
		}
	}
}

// flushBatch 批量 INSERT + 单次 UPDATE（lastMsgSeq/lastMsgTime 用 GREATEST 防并发回写，S16）。
func (q *batchQueue) flushBatch(ctx context.Context, groupID string, batch []*queueItem) error {
	maxSeq := batch[len(batch)-1].seq
	maxTs := batch[len(batch)-1].ts

	tx, err := q.e.deps.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var sb strings.Builder
	sb.WriteString(`INSERT INTO "GroupMessage"("id","groupId","seq","senderId","senderName","msgType","content","replyToId","extra","createdAt") VALUES `)
	args := make([]any, 0, len(batch)*10)
	for i, item := range batch {
		p := item.payload
		mType, err := checkMLS(p.MsgType)
		if err != nil {
			return err
		}
		senderName := p.SenderName
		if senderName == "" {
			senderName = p.SenderID
		}
		var replyTo any
		if p.ReplyToID != "" {
			replyTo = p.ReplyToID
		}
		var extra any
		if ex := normalizeExtra(p.Extra); len(ex) > 0 {
			extra = string(ex)
		}
		base := i*10 + 1
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base, base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9)
		args = append(args, util.NewID(), groupID, item.seq, p.SenderID, senderName,
			mType, p.Content, replyTo, extra, item.ts)
	}
	if _, err := tx.Exec(ctx, sb.String(), args...); err != nil {
		return err
	}
	// S16：并发 batch 回写用 GREATEST，防止旧值覆盖新值
	if _, err := tx.Exec(ctx,
		`UPDATE "Group" SET "lastMsgSeq"=GREATEST("lastMsgSeq",$1),
		 "lastMsgTime"=CASE WHEN "lastMsgTime" IS NULL OR "lastMsgTime" < $2 THEN $2 ELSE "lastMsgTime" END,
		 "updatedAt"=NOW() WHERE "id"=$3`,
		maxSeq, maxTs, groupID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	for _, item := range batch {
		item.done <- queueResult{msg: buildPushMessage(item)}
	}
	return nil
}

// flushSingle 逐条落库（批量失败后的降级路径）。
func (q *batchQueue) flushSingle(ctx context.Context, item *queueItem) error {
	p := item.payload
	mType, err := checkMLS(p.MsgType)
	if err != nil {
		return err
	}
	senderName := p.SenderName
	if senderName == "" {
		senderName = p.SenderID
	}
	var replyTo any
	if p.ReplyToID != "" {
		replyTo = p.ReplyToID
	}
	var extra any
	if ex := normalizeExtra(p.Extra); len(ex) > 0 {
		extra = string(ex)
	}
	tx, err := q.e.deps.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO "GroupMessage"("id","groupId","seq","senderId","senderName","msgType","content","replyToId","extra","createdAt")
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		util.NewID(), p.GroupID, item.seq, p.SenderID, senderName, mType, p.Content, replyTo, extra, item.ts); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE "Group" SET "lastMsgSeq"=GREATEST("lastMsgSeq",$1),
		 "lastMsgTime"=CASE WHEN "lastMsgTime" IS NULL OR "lastMsgTime" < $2 THEN $2 ELSE "lastMsgTime" END,
		 "updatedAt"=NOW() WHERE "id"=$3`,
		item.seq, item.ts, p.GroupID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	item.done <- queueResult{msg: buildPushMessage(item)}
	return nil
}

func buildPushMessage(item *queueItem) PushMessage {
	p := item.payload
	senderName := p.SenderName
	if senderName == "" {
		senderName = p.SenderID
	}
	msgType := p.MsgType
	if msgType == "" {
		msgType = "text"
	}
	m := PushMessage{
		Type:       "group_message",
		GroupID:    p.GroupID,
		Seq:        item.seq,
		SenderID:   p.SenderID,
		SenderName: senderName,
		MsgType:    msgType,
		Content:    p.Content,
		Timestamp:  item.ts.UnixMilli(),
	}
	if p.SenderAvatar != "" {
		m.SenderAvatar = p.SenderAvatar
	}
	if p.ReplyToID != "" {
		m.ReplyToID = util.StrPtr(p.ReplyToID)
	}
	if ex := normalizeExtra(p.Extra); len(ex) > 0 {
		m.Extra = ex
	}
	return m
}

// shutdown 停止接收新消息并同步刷出所有剩余桶（调用方已先停止接入）。
func (q *batchQueue) shutdown() {
	q.mu.Lock()
	q.closed = true
	groups := make([]string, 0, len(q.buckets))
	for gid, items := range q.buckets {
		if len(items) > 0 {
			groups = append(groups, gid)
		}
		if t, ok := q.timers[gid]; ok {
			t.Stop()
			delete(q.timers, gid)
		}
	}
	q.mu.Unlock()
	for _, gid := range groups {
		q.flushGroup(gid)
	}
}

// ============ 并发控制扇出器（S14：recover 防 panic） ============

type fanoutWorker struct {
	sem chan struct{}
}

func newFanoutWorker(concurrency int) *fanoutWorker {
	if concurrency < 1 {
		concurrency = 1
	}
	return &fanoutWorker{sem: make(chan struct{}, concurrency)}
}

func (w *fanoutWorker) push(task func()) {
	w.sem <- struct{}{}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[GroupMsg] fanout worker panic: %v", r)
			}
			<-w.sem
		}()
		task()
	}()
}

func (e *Engine) fanoutWorkerFor(groupID string, memberCount int) *fanoutWorker {
	e.workersMu.Lock()
	defer e.workersMu.Unlock()
	w, ok := e.workers[groupID]
	if !ok {
		concurrency := e.smallGroupConcurrency
		if memberCount > e.largeGroupThreshold {
			concurrency = e.largeGroupConcurrency
		}
		w = newFanoutWorker(concurrency)
		e.workers[groupID] = w
	}
	return w
}

// ============ 高频消息合并推送（带背压控制） ============

type mergeBuffer struct {
	groupID    string
	userID     string
	msgs       []PushMessage
	serialized [][]byte
	timer      *time.Timer
}

// pushToUser 推送消息给单个用户（合并窗口 + 背压由 SendTo/OnSend 处理）。
func (e *Engine) pushToUser(userID string, msg PushMessage, serialized []byte) {
	key := msg.GroupID + ":" + userID
	e.mergeMu.Lock()
	b := e.merges[key]
	if b == nil {
		b = &mergeBuffer{groupID: msg.GroupID, userID: userID}
		e.merges[key] = b
	}
	b.msgs = append(b.msgs, msg)
	b.serialized = append(b.serialized, serialized)
	var toFlush *mergeBuffer
	if len(b.msgs) >= e.mergeMaxBatch {
		toFlush = b
		delete(e.merges, key)
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
	} else if b.timer == nil {
		b2 := b
		b.timer = time.AfterFunc(e.mergeWindow, func() { e.flushMerge(key, b2) })
	}
	e.mergeMu.Unlock()
	if toFlush != nil {
		e.deliverMerge(toFlush)
	}
}

func (e *Engine) flushMerge(key string, b *mergeBuffer) {
	e.mergeMu.Lock()
	cur, ok := e.merges[key]
	if !ok || cur != b {
		e.mergeMu.Unlock()
		return
	}
	delete(e.merges, key)
	e.mergeMu.Unlock()
	e.deliverMerge(b)
}

func (e *Engine) deliverMerge(b *mergeBuffer) {
	if len(b.msgs) == 0 {
		return
	}
	if len(b.msgs) == 1 {
		e.SendTo(b.userID, b.serialized[0])
		return
	}
	raw, err := json.Marshal(map[string]any{
		"type":     "group_message_batch",
		"groupId":  b.groupID,
		"messages": b.msgs,
		"count":    len(b.msgs),
	})
	if err != nil {
		log.Printf("[GroupMsg] batch 序列化失败: %v", err)
		return
	}
	e.SendTo(b.userID, raw)
}

// ============ 核心 API ============

// SendGroupMessage 发送群消息：生成 seq → 入队 → 等待落库确认（S17）→ 返回 ACK，
// 扇出推送完全异步。返回 (seq, timestampMillis, error)。
func (e *Engine) SendGroupMessage(ctx context.Context, p SendParams) (int64, int64, error) {
	if e.closed.Load() {
		return 0, 0, errors.New("engine 已关闭")
	}
	if _, err := checkMLS(p.MsgType); err != nil {
		return 0, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	seq, err := e.nextSeq(ctx, p.GroupID)
	if err != nil {
		return 0, 0, err
	}
	ts := time.Now()
	item := &queueItem{payload: p, seq: seq, ts: ts, done: make(chan queueResult, 1)}
	if err := e.queue.enqueue(item); err != nil {
		return 0, 0, err
	}
	// S17：等待落库确认后再返回 ACK（最坏多一次 flush 间隔）
	res := <-item.done
	if res.err != nil {
		log.Printf("[GroupMsg] 异步落库/推送失败: groupId=%s seq=%d %v", p.GroupID, seq, res.err)
		return 0, 0, res.err
	}
	// 异步扇出推送（不阻塞发送方）
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[GroupMsg] fanout panic: groupId=%s %v", p.GroupID, r)
			}
		}()
		e.fanoutToGroup(p.GroupID, res.msg, p.SenderID)
		if e.OnOfflinePush != nil {
			e.OnOfflinePush(p.GroupID, p.SenderID, res.msg.SenderName)
		}
	}()
	return seq, ts.UnixMilli(), nil
}

// fanoutToGroup 分片扇出：预序列化一次，按 FANOUT_SHARD_SIZE 分片并行推送本机成员；
// 仅在远端在线的成员走 Redis publish（publishImPush）。
func (e *Engine) fanoutToGroup(groupID string, msg PushMessage, excludeUserID string) {
	localMembers := e.localGroupMembers(groupID)
	targets := make([]string, 0, len(localMembers))
	for _, uid := range localMembers {
		if uid != excludeUserID {
			targets = append(targets, uid)
		}
	}

	serialized, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[GroupMsg] 消息序列化失败: %v", err)
		return
	}

	if len(targets) > 0 {
		worker := e.fanoutWorkerFor(groupID, len(localMembers))
		var wg sync.WaitGroup
		for i := 0; i < len(targets); i += e.fanoutShardSize {
			end := i + e.fanoutShardSize
			if end > len(targets) {
				end = len(targets)
			}
			shard := targets[i:end]
			wg.Add(1)
			worker.push(func() {
				defer wg.Done()
				for _, uid := range shard {
					e.pushToUser(uid, msg, serialized)
				}
			})
		}
		wg.Wait()
		if len(localMembers) > 100 {
			log.Printf("[GroupMsg] 扇出完成: groupId=%s online=%d pushed=%d", groupID, len(localMembers), len(targets))
		}
	}

	// 跨节点：Redis 在线集合中有、但本机无连接的成员
	if e.deps.Redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		remoteMembers, err := e.deps.Redis.SMembers(ctx, "group:online:"+groupID)
		if err != nil {
			log.Printf("[GroupMsg] 跨节点群消息扇出失败: %v", err)
			return
		}
		localSet := make(map[string]struct{}, len(localMembers))
		for _, uid := range localMembers {
			localSet[uid] = struct{}{}
		}
		for _, uid := range remoteMembers {
			if uid == excludeUserID {
				continue
			}
			if _, ok := localSet[uid]; ok {
				continue
			}
			if e.IsOnline(uid) {
				continue
			}
			e.publishImPush(uid, serialized)
		}
	}
}

// FanoutGroupSignal 向群在线成员广播信令（撤回、系统事件等）。
// 本机成员走 SendTo（含 Redis 兜底），仅远端在线的成员直接 publishImPush。
func (e *Engine) FanoutGroupSignal(groupID string, signal any, excludeUserID string) {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[GroupMsg] FanoutGroupSignal panic: %v", r)
			}
		}()
		payload, err := json.Marshal(signal)
		if err != nil {
			log.Printf("[GroupMsg] 信令序列化失败: %v", err)
			return
		}
		localMembers := e.localGroupMembers(groupID)
		localSet := make(map[string]struct{}, len(localMembers))
		for _, uid := range localMembers {
			if uid == excludeUserID {
				continue
			}
			localSet[uid] = struct{}{}
			e.SendTo(uid, payload)
		}
		if e.deps.Redis == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		remoteMembers, err := e.deps.Redis.SMembers(ctx, "group:online:"+groupID)
		if err != nil {
			log.Printf("[GroupMsg] 跨节点信令扇出失败: %v", err)
			return
		}
		for _, uid := range remoteMembers {
			if uid == excludeUserID {
				continue
			}
			if _, ok := localSet[uid]; ok {
				continue
			}
			if e.IsOnline(uid) {
				continue
			}
			e.publishImPush(uid, payload)
		}
	}()
}

// PullGroupMessages 拉取群历史消息（读扩散核心）。
// mode 由 afterSeq/beforeSeq 决定；新成员看不到入群前的历史。
// 返回 map[string]any{messages, hasMore, latestSeq}，与 TS 返回形状一致。
func (e *Engine) PullGroupMessages(ctx context.Context, p PullParams) (map[string]any, error) {
	groupID, userID := p.GroupID, p.UserID
	afterSeq, beforeSeq := p.AfterSeq, p.BeforeSeq
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	mode := "latest"
	if beforeSeq != nil {
		mode = "before"
	} else if afterSeq != nil {
		mode = "after"
	}

	tx, err := e.deps.DB.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var joinTime time.Time
	if err := tx.QueryRow(ctx,
		`SELECT "joinTime" FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`,
		groupID, userID).Scan(&joinTime); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotGroupMember
		}
		return nil, err
	}

	var latestSeq int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE("lastMsgSeq",0) FROM "Group" WHERE "id"=$1`, groupID).Scan(&latestSeq); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	var rows pgx.Rows
	switch mode {
	case "before":
		rows, err = tx.Query(ctx,
			`SELECT "id","groupId","seq","senderId","senderName","msgType","content","replyToId","isRevoked","extra","createdAt"
			 FROM "GroupMessage" WHERE "groupId"=$1 AND "seq" < $2 ORDER BY "seq" DESC LIMIT $3`,
			groupID, *beforeSeq, limit+1)
	case "after":
		var a int64
		if afterSeq != nil {
			a = *afterSeq
		}
		rows, err = tx.Query(ctx,
			`SELECT "id","groupId","seq","senderId","senderName","msgType","content","replyToId","isRevoked","extra","createdAt"
			 FROM "GroupMessage" WHERE "groupId"=$1 AND "seq" > $2 ORDER BY "seq" ASC LIMIT $3`,
			groupID, a, limit+1)
	default:
		rows, err = tx.Query(ctx,
			`SELECT "id","groupId","seq","senderId","senderName","msgType","content","replyToId","isRevoked","extra","createdAt"
			 FROM "GroupMessage" WHERE "groupId"=$1 ORDER BY "seq" DESC LIMIT $2`,
			groupID, limit+1)
	}
	if err != nil {
		return nil, err
	}
	messages, err := pgx.CollectRows(rows, pgx.RowToStructByName[db.GroupMessage])
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// 应用层过滤入群前的历史
	filtered := messages[:0]
	for _, m := range messages {
		if !m.CreatedAt.Before(joinTime) {
			filtered = append(filtered, m)
		}
	}
	hasMore := len(filtered) > limit
	if hasMore {
		filtered = filtered[:limit]
	}
	if mode == "before" || mode == "latest" {
		for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
			filtered[i], filtered[j] = filtered[j], filtered[i]
		}
	}

	// 批量查询发送者信息
	senderIDs := make([]string, 0, len(filtered))
	seen := make(map[string]struct{})
	for _, m := range filtered {
		if _, ok := seen[m.SenderId]; !ok {
			seen[m.SenderId] = struct{}{}
			senderIDs = append(senderIDs, m.SenderId)
		}
	}
	senderMap := make(map[string]db.User)
	if len(senderIDs) > 0 {
		users, err := db.QueryToStructs[db.UserBrief](ctx, e.deps.DB,
			`SELECT "id","username","nickname","avatar" FROM "User" WHERE "id"=ANY($1)`, senderIDs)
		if err == nil {
			for _, b := range users {
				u := b.ToUser()
				senderMap[u.Id] = u
			}
		}
	}

	out := make([]PulledMessage, 0, len(filtered))
	for _, m := range filtered {
		senderName := util.StrVal(m.SenderName)
		sender := senderMap[m.SenderId]
		if senderName == "" {
			senderName = util.StrVal(sender.Nickname)
		}
		if senderName == "" {
			senderName = sender.Username
		}
		if senderName == "" {
			senderName = m.SenderId
		}
		pm := PulledMessage{
			ID:           m.Id,
			Seq:          m.Seq,
			SenderID:     m.SenderId,
			SenderName:   senderName,
			SenderAvatar: avatarToProxy(util.StrVal(sender.Avatar)),
			MsgType:      m.MsgType,
			Content:      m.Content,
			CreatedAt:    m.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
			IsRevoked:    m.IsRevoked,
		}
		if util.StrVal(m.ReplyToId) != "" {
			pm.ReplyToID = m.ReplyToId
		}
		if util.StrVal(m.Extra) != "" {
			pm.Extra = json.RawMessage(util.StrVal(m.Extra))
		}
		out = append(out, pm)
	}
	return map[string]any{"messages": out, "hasMore": hasMore, "latestSeq": latestSeq}, nil
}

// AckGroupMessages 确认已读（更新游标）。返回 nil 表示成功（未读数由调用方按需查询）。
func (e *Engine) AckGroupMessages(ctx context.Context, p AckParams) error {
	_, err := e.ack(ctx, p.GroupID, p.UserID, p.LastAckSeq)
	return err
}

// ack 确认已读并返回未读数（内部实现，供 HTTP 路由使用）。
func (e *Engine) ack(ctx context.Context, groupID, userID string, lastAckSeq int64) (int64, error) {
	if _, err := e.deps.DB.Exec(ctx,
		`UPDATE "GroupMember" SET "lastAckSeq"=$3, "updatedAt"=NOW() WHERE "groupId"=$1 AND "userId"=$2`,
		groupID, userID, lastAckSeq); err != nil {
		return 0, err
	}
	var lastMsgSeq int64
	if err := e.deps.DB.Pool.QueryRow(ctx,
		`SELECT COALESCE("lastMsgSeq",0) FROM "Group" WHERE "id"=$1`, groupID).Scan(&lastMsgSeq); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	unread := lastMsgSeq - lastAckSeq
	if unread < 0 {
		unread = 0
	}
	return unread, nil
}

// GetGroupUnreadCounts 获取用户所有群的未读数（批量）。
func (e *Engine) GetGroupUnreadCounts(ctx context.Context, userID string) (map[string]int64, error) {
	rows, err := e.deps.DB.Pool.Query(ctx,
		`SELECT m."groupId", m."lastAckSeq", COALESCE(g."lastMsgSeq",0)
		 FROM "GroupMember" m JOIN "Group" g ON g."id"=m."groupId" WHERE m."userId"=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int64)
	for rows.Next() {
		var groupID string
		var lastAckSeq, lastMsgSeq int64
		if err := rows.Scan(&groupID, &lastAckSeq, &lastMsgSeq); err != nil {
			return nil, err
		}
		if unread := lastMsgSeq - lastAckSeq; unread > 0 {
			result[groupID] = unread
		}
	}
	return result, rows.Err()
}
