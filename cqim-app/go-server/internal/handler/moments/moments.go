// Package moments 移植自 server/moments.ts —— 朋友圈 API（发布 / Feed / 列表 /
// 点赞 / 评论 / 置顶 / 排序 / 话题）。挂载在 /api/moments。
//
// 认证统一使用共享 middleware（d.Auth.UserAuth / d.Auth.OptionalAuth），
// 不重复实现 moments.ts 内自带的 userAuth / optionalAuth。
// 路由、JSON 字段、状态码、中文错误文案与 TS 版保持一致。
package moments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// RegisterRoutes 注册朋友圈模块全部路由。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := &Handler{deps: d}
	l1JanitorOnce.Do(func() { go l1Janitor() })

	auth := d.Auth.UserAuth
	optAuth := d.Auth.OptionalAuth

	mux.Handle("GET /api/moments/feed", auth(http.HandlerFunc(h.feed)))
	mux.Handle("GET /api/moments", optAuth(http.HandlerFunc(h.list)))
	mux.Handle("GET /api/moments/", optAuth(http.HandlerFunc(h.list)))
	mux.Handle("GET /api/moments/topics/hot", http.HandlerFunc(h.hotTopics))
	mux.Handle("GET /api/moments/my", auth(http.HandlerFunc(h.myMoments)))
	mux.Handle("POST /api/moments", auth(http.HandlerFunc(h.publish)))
	mux.Handle("GET /api/moments/{id}", optAuth(http.HandlerFunc(h.detail)))
	mux.Handle("DELETE /api/moments/{id}", auth(http.HandlerFunc(h.deleteMoment)))
	mux.Handle("POST /api/moments/{id}/like", auth(http.HandlerFunc(h.like)))
	mux.Handle("POST /api/moments/{id}/comments", auth(http.HandlerFunc(h.postComment)))
	mux.Handle("DELETE /api/moments/{momentId}/comments/{commentId}", auth(http.HandlerFunc(h.deleteComment)))
	mux.Handle("POST /api/moments/{id}/pin", auth(http.HandlerFunc(h.pin)))
	mux.Handle("PUT /api/moments/reorder", auth(http.HandlerFunc(h.reorder)))
	mux.Handle("PUT /api/moments/{id}", auth(http.HandlerFunc(h.updateMoment)))
}

// Handler 朋友圈模块 handler。
type Handler struct {
	deps *handler.Deps
}

// Moment 表全列（SELECT 拼接用），顺序与 db.Moment 一致。
const momentColumns = `"id","userId","content","visibility","location","topics","isPinned","pinnedAt","viewCount","sortOrder","createdAt","updatedAt"`

// ============ L1 内存缓存（对应 moments.ts 的 cache Map） ============

type l1Entry struct {
	data     any
	expireAt time.Time
}

var l1Cache = struct {
	sync.Mutex
	m map[string]l1Entry
}{m: make(map[string]l1Entry)}

var l1JanitorOnce sync.Once

// l1Janitor 每 5 分钟清理过期条目（对应 TS 的 setInterval 清理）。
func l1Janitor() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		l1Cache.Lock()
		for k, e := range l1Cache.m {
			if now.After(e.expireAt) {
				delete(l1Cache.m, k)
			}
		}
		l1Cache.Unlock()
	}
}

func l1Get[T any](key string) (T, bool) {
	var zero T
	l1Cache.Lock()
	defer l1Cache.Unlock()
	e, ok := l1Cache.m[key]
	if !ok || time.Now().After(e.expireAt) {
		if ok {
			delete(l1Cache.m, key)
		}
		return zero, false
	}
	v, ok := e.data.(T)
	if !ok {
		return zero, false
	}
	return v, true
}

func l1Set(key string, data any, ttl time.Duration) {
	l1Cache.Lock()
	defer l1Cache.Unlock()
	l1Cache.m[key] = l1Entry{data: data, expireAt: time.Now().Add(ttl)}
}

func l1Invalidate(prefix string) {
	l1Cache.Lock()
	defer l1Cache.Unlock()
	for k := range l1Cache.m {
		if strings.HasPrefix(k, prefix) {
			delete(l1Cache.m, k)
		}
	}
}

// ============ Redis 缓存 helpers（对应 redisCacheGet/Set/Invalidate） ============

func (h *Handler) redisGetJSON(ctx context.Context, key string, v any) bool {
	s, ok, err := h.deps.Redis.GetString(ctx, key)
	if err != nil || !ok {
		return false
	}
	if err := json.Unmarshal([]byte(s), v); err != nil {
		return false
	}
	return true
}

func (h *Handler) redisSetJSON(ctx context.Context, key string, v any, ttl time.Duration) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = h.deps.Redis.SetEX(ctx, key, string(b), ttl)
}

// redisDelPattern 按通配符删除（对应 TS 的 redis.keys(pattern)+del）。
func (h *Handler) redisDelPattern(ctx context.Context, pattern string) {
	keys, err := h.deps.Redis.RDB.Keys(ctx, pattern).Result()
	if err != nil || len(keys) == 0 {
		return
	}
	_ = h.deps.Redis.Del(ctx, keys...)
}

// ============ 好友列表缓存（L1 30s + Redis 60s，对应 getFriendIds） ============

func (h *Handler) getFriendIDs(ctx context.Context, userID string) ([]string, error) {
	if ids, ok := l1Get[[]string]("friends:" + userID); ok {
		return ids, nil
	}
	if s, ok, err := h.deps.Redis.GetString(ctx, "moments:friends:"+userID); err == nil && ok {
		var ids []string
		if json.Unmarshal([]byte(s), &ids) == nil {
			l1Set("friends:"+userID, ids, 30*time.Second)
			return ids, nil
		}
	}
	type row struct {
		UserA string `db:"userA"`
		UserB string `db:"userB"`
	}
	rows, err := db.QueryToStructs[row](ctx, h.deps.DB,
		`SELECT "userA","userB" FROM "Friendship" WHERE "userA"=$1 OR "userB"=$1`, userID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.UserA == userID {
			ids = append(ids, r.UserB)
		} else {
			ids = append(ids, r.UserA)
		}
	}
	l1Set("friends:"+userID, ids, 30*time.Second)
	if b, err := json.Marshal(ids); err == nil {
		_ = h.deps.Redis.SetEX(ctx, "moments:friends:"+userID, string(b), 60*time.Second)
	}
	return ids, nil
}

// ============ 实时事件推送（对应 publishMessage('moment_events', ...)） ============

// publishMomentEvent 向 msg:moment_events 频道发布点赞/评论通知（失败静默忽略）。
func (h *Handler) publishMomentEvent(ctx context.Context, typ, targetUserID string, payload any) {
	b, err := json.Marshal(map[string]any{
		"type":         typ,
		"targetUserId": targetUserID,
		"payload":      payload,
	})
	if err != nil {
		return
	}
	_ = h.deps.Redis.Publish(ctx, "msg:moment_events", string(b))
}

// ============ 通用小 helpers ============

// displayName 对应 TS 的 nickname || username。
func displayName(nickname *string, username string) string {
	if nickname != nil && *nickname != "" {
		return *nickname
	}
	return username
}

// splitTopics 对应 TS 的 topics.split(',').filter(Boolean)。
func splitTopics(topics *string) []string {
	out := []string{}
	if topics == nil || *topics == "" {
		return out
	}
	for _, p := range strings.Split(*topics, ",") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runeLen(s string) int { return len([]rune(s)) }

func runeSlice(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// parseLimit 对应 Math.min(20, parseInt(limit) || 10)。
func parseLimit(r *http.Request) int {
	limit := 10
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 20 {
		limit = 20
	}
	return limit
}

var errInvalidCursor = errors.New("invalid cursor")

// parseCursorTime 解析 ?cursor=（TS 的 new Date(cursor)，toISOString 格式）。
func parseCursorTime(raw string) (time.Time, bool, error) {
	if raw == "" {
		return time.Time{}, false, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true, nil
		}
	}
	return time.Time{}, false, errInvalidCursor
}

// isoMillis 格式化为 JS Date.toISOString() 形式（UTC，毫秒，Z 后缀）。
func isoMillis(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// writeETagMatch 写入 ETag；If-None-Match 命中则回 304 并返回 true。
func writeETagMatch(w http.ResponseWriter, r *http.Request, etag string) bool {
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// decodeJSONAllowEmpty 解析请求体；空 body 视为 {}（TS 里 req.body 默认为 {}）。
func decodeJSONAllowEmpty(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
	if err != nil {
		util.WriteError(w, http.StatusBadRequest, "请求参数格式错误")
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	if err := json.Unmarshal(body, v); err != nil {
		util.WriteError(w, http.StatusBadRequest, "请求参数格式错误")
		return false
	}
	return true
}

// condBuilder 动态 WHERE 条件拼接器（占位符 $n 自动编号）。
type condBuilder struct {
	conds []string
	args  []any
}

func (b *condBuilder) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

func (b *condBuilder) where(c string) { b.conds = append(b.conds, c) }

func (b *condBuilder) clause() string {
	if len(b.conds) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(b.conds, " AND ")
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func serverError(w http.ResponseWriter, err error) {
	util.WriteError(w, http.StatusInternalServerError, "服务器错误")
}

// findMoment 按 id 查询动态；不存在返回 (nil, true)。
func (h *Handler) findMoment(ctx context.Context, id string) (*db.Moment, bool, error) {
	m, err := db.QueryRowToStruct[db.Moment](ctx, h.deps.DB,
		`SELECT `+momentColumns+` FROM "Moment" WHERE "id"=$1`, id)
	if err != nil {
		if db.IsNotFound(err) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return m, false, nil
}

// ============ 路由 handlers ============

// feed 好友朋友圈 Feed（包含自己 + 好友的动态），Redis 缓存 + ETag。
func (h *Handler) feed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	if me == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	rawCursor := r.URL.Query().Get("cursor")
	cursor, hasCursor, err := parseCursorTime(rawCursor)
	if err != nil {
		util.WriteError(w, 400, "请求参数格式错误")
		return
	}
	limit := parseLimit(r)
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"

	cursorKey := rawCursor
	if cursorKey == "" {
		cursorKey = "first"
	}
	feedCacheKey := "moments:feed:" + me.Id + ":" + cursorKey + ":" + strconv.Itoa(limit)

	if !hasCursor && !force {
		var cached feedResult
		if h.redisGetJSON(ctx, feedCacheKey, &cached) {
			etag := fmt.Sprintf(`"feed-%s-%d"`, me.Id, cached.Ts)
			w.Header().Set("Cache-Control", "private, max-age=10")
			if writeETagMatch(w, r, etag) {
				return
			}
			util.WriteJSON(w, 200, cached)
			return
		}
	} else if force {
		// force 刷新时清掉该 key，避免后续请求读到旧数据
		_ = h.deps.Redis.Del(ctx, feedCacheKey)
	}

	friendIDs, err := h.getFriendIDs(ctx, me.Id)
	if err != nil {
		serverError(w, err)
		return
	}

	// 置顶动态（仅首页，仅自己的置顶）
	var pinned []db.Moment
	if !hasCursor {
		pinned, err = db.QueryToStructs[db.Moment](ctx, h.deps.DB,
			`SELECT `+momentColumns+` FROM "Moment" WHERE "userId"=$1 AND "isPinned"=true ORDER BY "pinnedAt" DESC`, me.Id)
		if err != nil {
			serverError(w, err)
			return
		}
	}

	// 普通动态：自己全部 + 好友的 public/friends
	args := []any{me.Id, friendIDs, []string{"public", "friends"}}
	cond := `("userId"=$1 OR ("userId" = ANY($2) AND "visibility" = ANY($3)))`
	if hasCursor {
		args = append(args, cursor)
		cond += ` AND "createdAt" < $4`
	}
	args = append(args, limit+1)
	normal, err := db.QueryToStructs[db.Moment](ctx, h.deps.DB,
		`SELECT `+momentColumns+` FROM "Moment" WHERE `+cond+` ORDER BY "createdAt" DESC LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		serverError(w, err)
		return
	}

	// 排除置顶动态的重复
	pinnedSet := make(map[string]bool, len(pinned))
	for _, m := range pinned {
		pinnedSet[m.Id] = true
	}
	filtered := make([]db.Moment, 0, len(normal))
	for _, m := range normal {
		if !pinnedSet[m.Id] {
			filtered = append(filtered, m)
		}
	}
	hasMore := len(filtered) > limit
	normalList := filtered
	if hasMore {
		normalList = filtered[:limit]
	}
	var nextCursor *string
	if hasMore {
		s := isoMillis(normalList[len(normalList)-1].CreatedAt)
		nextCursor = &s
	}

	// 点赞状态
	liked := make(map[string]bool)
	allIDs := make([]string, 0, len(pinned)+len(normalList))
	for _, m := range pinned {
		allIDs = append(allIDs, m.Id)
	}
	for _, m := range normalList {
		allIDs = append(allIDs, m.Id)
	}
	if len(allIDs) > 0 {
		type likeRow struct {
			MomentID string `db:"momentId"`
		}
		rows, err := db.QueryToStructs[likeRow](ctx, h.deps.DB,
			`SELECT "momentId" FROM "MomentLike" WHERE "userId"=$1 AND "momentId"=ANY($2)`, me.Id, allIDs)
		if err != nil {
			serverError(w, err)
			return
		}
		for _, l := range rows {
			liked[l.MomentID] = true
		}
	}

	ordered := normalList
	if !hasCursor {
		ordered = make([]db.Moment, 0, len(pinned)+len(normalList))
		ordered = append(ordered, pinned...)
		ordered = append(ordered, normalList...)
	}
	moments, err := h.loadShareMoments(ctx, ordered, liked, 10, 10, 5)
	if err != nil {
		serverError(w, err)
		return
	}

	result := feedResult{
		Moments:    moments,
		HasMore:    hasMore,
		NextCursor: nextCursor,
		Ts:         time.Now().UnixMilli(),
	}
	if !hasCursor {
		h.redisSetJSON(ctx, feedCacheKey, result, 30*time.Second)
	}
	etag := fmt.Sprintf(`"feed-%s-%d"`, me.Id, result.Ts)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=10")
	util.WriteJSON(w, 200, result)
}

// list 动态列表（游标分页，置顶优先）。
// 有 userId 参数时为分享/个人主页模式（走 formatMomentForShare），否则为公开广场模式。
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	rawCursor := r.URL.Query().Get("cursor")
	cursor, hasCursor, err := parseCursorTime(rawCursor)
	if err != nil {
		util.WriteError(w, 400, "请求参数格式错误")
		return
	}
	limit := parseLimit(r)
	userID := r.URL.Query().Get("userId")
	isShareMode := userID != ""

	// 分享外链页首屏（未登录）走公共 Redis 缓存
	var shareKey string
	if isShareMode && !hasCursor && me == nil {
		shareKey = "moments:share:" + userID + ":" + strconv.Itoa(limit)
		var cached shareListResult
		if h.redisGetJSON(ctx, shareKey, &cached) {
			etag := fmt.Sprintf(`"share-%s-%d"`, userID, cached.Ts)
			w.Header().Set("Cache-Control", "public, max-age=15")
			if writeETagMatch(w, r, etag) {
				return
			}
			util.WriteJSON(w, 200, cached)
			return
		}
	}

	b := &condBuilder{}
	if userID != "" {
		b.where(`"userId"=` + b.arg(userID))
		if me != nil && me.Id != userID {
			// 已登录好友可见 friends 动态，非好友仅 public
			friendIDs, err := h.getFriendIDs(ctx, me.Id)
			if err != nil {
				serverError(w, err)
				return
			}
			if containsStr(friendIDs, userID) {
				b.where(`"visibility" = ANY(` + b.arg([]string{"public", "friends"}) + `)`)
			} else {
				b.where(`"visibility"=` + b.arg("public"))
			}
		} else if me == nil {
			b.where(`"visibility"=` + b.arg("public"))
		}
		// 查看自己的朋友圈：不过滤 visibility
	} else {
		b.where(`"visibility" = ANY(` + b.arg([]string{"public"}) + `)`)
	}

	var pinned []db.Moment
	if !hasCursor {
		pinned, err = db.QueryToStructs[db.Moment](ctx, h.deps.DB,
			`SELECT `+momentColumns+` FROM "Moment" `+b.clause()+` AND "isPinned"=true ORDER BY "pinnedAt" DESC`, b.args...)
		if err != nil {
			serverError(w, err)
			return
		}
	}
	normalQ := `SELECT ` + momentColumns + ` FROM "Moment" ` + b.clause() + ` AND "isPinned"=false`
	if hasCursor {
		normalQ += ` AND "createdAt" < ` + b.arg(cursor)
	}
	normalQ += ` ORDER BY "createdAt" DESC LIMIT ` + b.arg(limit+1)
	normal, err := db.QueryToStructs[db.Moment](ctx, h.deps.DB, normalQ, b.args...)
	if err != nil {
		serverError(w, err)
		return
	}

	hasMore := len(normal) > limit
	normalList := normal
	if hasMore {
		normalList = normal[:limit]
	}
	var nextCursor *string
	if hasMore {
		s := isoMillis(normalList[len(normalList)-1].CreatedAt)
		nextCursor = &s
	}

	// 登录用户查询点赞状态
	liked := make(map[string]bool)
	if me != nil {
		allIDs := make([]string, 0, len(pinned)+len(normalList))
		for _, m := range pinned {
			allIDs = append(allIDs, m.Id)
		}
		for _, m := range normalList {
			allIDs = append(allIDs, m.Id)
		}
		if len(allIDs) > 0 {
			type likeRow struct {
				MomentID string `db:"momentId"`
			}
			rows, err := db.QueryToStructs[likeRow](ctx, h.deps.DB,
				`SELECT "momentId" FROM "MomentLike" WHERE "userId"=$1 AND "momentId"=ANY($2)`, me.Id, allIDs)
			if err != nil {
				serverError(w, err)
				return
			}
			for _, l := range rows {
				liked[l.MomentID] = true
			}
		}
	}

	ordered := normalList
	if !hasCursor {
		ordered = make([]db.Moment, 0, len(pinned)+len(normalList))
		ordered = append(ordered, pinned...)
		ordered = append(ordered, normalList...)
	}

	if isShareMode {
		moments, err := h.loadShareMoments(ctx, ordered, liked, 20, 50, 0)
		if err != nil {
			serverError(w, err)
			return
		}
		result := shareListResult{
			Moments:    moments,
			HasMore:    hasMore,
			NextCursor: nextCursor,
			Ts:         time.Now().UnixMilli(),
		}
		if !hasCursor {
			var total int64
			if err := h.deps.DB.Pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM "Moment" WHERE "userId"=$1 AND "visibility"='public'`, userID).Scan(&total); err == nil {
				result.Total = &total
			}
			if u := h.getUserLite(ctx, userID); u != nil {
				result.User = &shareUserJSON{
					ID:            u.Id,
					Username:      u.Username,
					Nickname:      u.Nickname,
					Avatar:        u.Avatar,
					BackgroundURL: u.BackgroundUrl,
					Bio:           u.Bio,
				}
				// 双重指针：shareUser 存在时 bio/backgroundUrl 必须出现（可为 null）
				result.Bio = &u.Bio
				result.BackgroundURL = &u.BackgroundUrl
			}
		}
		if shareKey != "" {
			h.redisSetJSON(ctx, shareKey, result, 30*time.Second)
			etag := fmt.Sprintf(`"share-%s-%d"`, userID, result.Ts)
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", "public, max-age=15")
		}
		util.WriteJSON(w, 200, result)
		return
	}

	cards := h.buildMomentCards(ctx, ordered, liked)
	util.WriteJSON(w, 200, listResult{
		Moments:    cards,
		HasMore:    hasMore,
		NextCursor: nextCursor,
		Ts:         time.Now().UnixMilli(),
	})
}

// publish 发布动态（限流 + 内容/媒体校验）。
func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	if me == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	if !publishLimiter.check("publish:" + me.Id) {
		util.WriteError(w, 429, "发布过于频繁，请稍后再试")
		return
	}

	var req struct {
		Content    *string   `json:"content"`
		Visibility *string   `json:"visibility"`
		Location   *string   `json:"location"`
		Topics     any       `json:"topics"`
		Media      []mediaIn `json:"media"`
	}
	if !decodeJSONAllowEmpty(w, r, &req) {
		return
	}

	normalizedContent := ""
	if req.Content != nil {
		normalizedContent = strings.TrimSpace(*req.Content)
	}
	if runeLen(normalizedContent) > 5000 {
		util.WriteError(w, 400, "内容超出长度限制（5000 字）")
		return
	}
	media := req.Media
	if media == nil {
		media = []mediaIn{}
	}
	if len(media) > 9 {
		util.WriteError(w, 400, "最多上传 9 张图片/视频")
		return
	}
	videoCount := 0
	for _, m := range media {
		if m.Type != nil && *m.Type == "video" {
			videoCount++
		}
	}
	if videoCount > 1 {
		util.WriteError(w, 400, "最多上传 1 个视频")
		return
	}
	if normalizedContent == "" && len(media) == 0 {
		util.WriteError(w, 400, "请输入内容或上传图片/视频")
		return
	}
	allowedMediaTypes := map[string]bool{"image": true, "video": true}
	for _, m := range media {
		if m.Type != nil && *m.Type != "" && !allowedMediaTypes[*m.Type] {
			util.WriteError(w, 400, "不支持的媒体类型: "+*m.Type)
			return
		}
		if m.URL != nil && *m.URL != "" {
			u := *m.URL
			if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "/") {
				util.WriteError(w, 400, "媒体 URL 格式无效")
				return
			}
			lower := strings.ToLower(u)
			if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "vbscript:") {
				util.WriteError(w, 400, "媒体 URL 包含不允许的协议")
				return
			}
		}
	}

	visibility := "public"
	if req.Visibility != nil && *req.Visibility != "" {
		visibility = *req.Visibility
	}
	var location *string
	if req.Location != nil {
		s := runeSlice(*req.Location, 200)
		location = &s
	}
	var topics *string
	switch t := req.Topics.(type) {
	case []any:
		names := make([]string, 0, len(t))
		for i, v := range t {
			if i >= 10 {
				break
			}
			if s, ok := v.(string); ok {
				names = append(names, s)
			}
		}
		s := strings.Join(names, ",")
		topics = &s
	case string:
		s := runeSlice(t, 200)
		topics = &s
	}

	momentID := util.NewID()
	_, err := h.deps.DB.Exec(ctx,
		`INSERT INTO "Moment"("id","userId","content","visibility","location","topics","isPinned","pinnedAt","viewCount","sortOrder","createdAt","updatedAt")
		 VALUES($1,$2,$3,$4,$5,$6,false,NULL,0,0,NOW(),NOW())`,
		momentID, me.Id, runeSlice(normalizedContent, 5000), visibility, location, topics)
	if err != nil {
		serverError(w, err)
		return
	}
	mediaRows := make([]db.MomentMedia, 0, len(media))
	for i, m := range media {
		typ := "image"
		if m.Type != nil && allowedMediaTypes[*m.Type] {
			typ = *m.Type
		}
		u := ""
		if m.URL != nil {
			u = *m.URL
		}
		mm := db.MomentMedia{
			Id:        util.NewID(),
			MomentId:  momentID,
			Type:      typ,
			Url:       u,
			Width:     m.Width,
			Height:    m.Height,
			Duration:  m.Duration,
			SortOrder: int32(i),
		}
		if _, err := h.deps.DB.Exec(ctx,
			`INSERT INTO "MomentMedia"("id","momentId","type","url","width","height","duration","sortOrder","createdAt")
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8,NOW())`,
			mm.Id, mm.MomentId, mm.Type, mm.Url, mm.Width, mm.Height, mm.Duration, mm.SortOrder); err != nil {
			serverError(w, err)
			return
		}
		mediaRows = append(mediaRows, mm)
	}

	// 清除相关缓存（L1 + Redis，避免好友看到旧 feed）
	l1Invalidate("friends:")
	l1Invalidate("feed:")
	h.redisDelPattern(ctx, "moments:feed:*")
	h.redisDelPattern(ctx, "moments:share:"+me.Id+":*")
	h.redisDelPattern(ctx, "moments:detail:*")

	m, _, err := h.findMoment(ctx, momentID)
	if err != nil || m == nil {
		serverError(w, err)
		return
	}
	signedMedia := h.signMediaFull(ctx, mediaRows)
	result := publishMomentJSON{
		ID: m.Id, UserID: m.UserId, Content: m.Content, Visibility: m.Visibility,
		Location: m.Location, Topics: splitTopics(m.Topics),
		IsPinned: m.IsPinned, PinnedAt: m.PinnedAt, ViewCount: m.ViewCount,
		SortOrder: m.SortOrder, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		User: &userLite{
			ID: me.Id, Username: me.Username, Nickname: me.Nickname, Avatar: me.Avatar,
		},
		Media:        signedMedia,
		Count:        countJSON{Comments: 0, Likes: 0},
		LikeCount:    0,
		CommentCount: 0,
		IsLiked:      false,
	}
	w.Header().Set("Cache-Control", "no-store")
	util.WriteJSON(w, 200, map[string]any{"success": true, "moment": result})
}

// myMoments 获取我的动态列表（管理页面用）。
func (h *Handler) myMoments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	if me == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	moments, err := db.QueryToStructs[db.Moment](ctx, h.deps.DB,
		`SELECT `+momentColumns+` FROM "Moment" WHERE "userId"=$1 ORDER BY "sortOrder" ASC, "createdAt" DESC`, me.Id)
	if err != nil {
		serverError(w, err)
		return
	}
	ids := make([]string, 0, len(moments))
	for _, m := range moments {
		ids = append(ids, m.Id)
	}
	likeCounts := h.momentCounts(ctx, "MomentLike", ids)
	commentCounts := h.momentCounts(ctx, "MomentComment", ids)
	mediaMap := h.batchMedia(ctx, ids)

	items := make([]myMomentJSON, 0, len(moments))
	for _, m := range moments {
		var in []shareMediaIn
		for _, md := range mediaMap[m.Id] {
			in = append(in, shareMediaIn{Type: md.Type, URL: md.Url})
		}
		items = append(items, myMomentJSON{
			ID:           m.Id,
			Content:      m.Content,
			Visibility:   m.Visibility,
			Location:     m.Location,
			IsPinned:     m.IsPinned,
			SortOrder:    m.SortOrder,
			CreatedAt:    m.CreatedAt.UnixMilli(),
			Media:        h.signMyMedia(ctx, in),
			LikeCount:    int(likeCounts[m.Id]),
			CommentCount: int(commentCounts[m.Id]),
		})
	}
	util.WriteJSON(w, 200, map[string]any{"moments": items})
}

// detail 获取单条动态（未登录结果走 Redis 缓存 2 分钟）。
func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	id := r.PathValue("id")
	cacheKey := "moments:detail:" + id

	if me == nil {
		if s, ok, err := h.deps.Redis.GetString(ctx, cacheKey); err == nil && ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(s))
			return
		}
	}

	m, notFound, err := h.findMoment(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	if notFound {
		util.WriteError(w, 404, "动态不存在")
		return
	}

	var author *userLiteBio
	if u := h.getUserLite(ctx, m.UserId); u != nil {
		author = &userLiteBio{
			ID: u.Id, Username: u.Username, Nickname: u.Nickname, Avatar: u.Avatar,
			BackgroundURL: u.BackgroundUrl, Bio: u.Bio,
		}
	}
	mediaMap := h.batchMedia(ctx, []string{m.Id})
	signedMedia := h.signMediaFull(ctx, mediaMap[m.Id])

	// 评论（顶层 + 嵌套回复）与点赞（含用户）
	topComments := h.detailComments(ctx, m.Id)
	likes := h.detailLikes(ctx, m.Id)

	commentCount := 0
	for _, c := range topComments {
		commentCount += 1 + len(c.Replies)
	}
	isLiked := false
	if me != nil {
		for _, l := range likes {
			if l.UserID == me.Id {
				isLiked = true
				break
			}
		}
	}

	var loc *string
	if m.Location != nil && *m.Location != "" {
		loc = m.Location
	}
	result := momentDetailJSON{
		ID: m.Id, UserID: m.UserId, Content: m.Content, Visibility: m.Visibility,
		Location: loc, Topics: splitTopics(m.Topics),
		IsPinned: m.IsPinned, PinnedAt: m.PinnedAt, ViewCount: m.ViewCount,
		SortOrder: m.SortOrder, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		User: author, Media: signedMedia,
		Comments: topComments, Likes: likes,
		Count:        countJSON{Comments: commentCount, Likes: len(likes)},
		LikeCount:    len(likes),
		CommentCount: commentCount,
		IsLiked:      isLiked,
	}
	if me == nil {
		h.redisSetJSON(ctx, cacheKey, result, 120*time.Second)
	}
	util.WriteJSON(w, 200, result)
}

// deleteMoment 删除动态（作者本人或管理员）。
func (h *Handler) deleteMoment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	if me == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	id := r.PathValue("id")
	m, notFound, err := h.findMoment(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	if notFound {
		util.WriteError(w, 404, "动态不存在")
		return
	}
	if m.UserId != me.Id && me.Role != "admin" {
		util.WriteError(w, 403, "无权删除")
		return
	}
	if _, err := h.deps.DB.Exec(ctx, `DELETE FROM "Moment" WHERE "id"=$1`, id); err != nil {
		serverError(w, err)
		return
	}
	h.redisDelPattern(ctx, "moments:feed:*")
	_ = h.deps.Redis.Del(ctx, "moments:detail:"+id)
	h.redisDelPattern(ctx, "moments:share:"+m.UserId+":*")
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// like 点赞/取消点赞（限流 30/分钟），点赞时实时通知作者。
func (h *Handler) like(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	if me == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	if !likeLimiter.check("like:" + me.Id) {
		util.WriteError(w, 429, "操作过于频繁，请稍后再试")
		return
	}
	momentID := r.PathValue("id")

	type idRow struct {
		ID string `db:"id"`
	}
	_, err := db.QueryRowToStruct[idRow](ctx, h.deps.DB,
		`SELECT "id" FROM "MomentLike" WHERE "momentId"=$1 AND "userId"=$2`, momentID, me.Id)
	liked := true
	if err == nil {
		if _, err := h.deps.DB.Exec(ctx,
			`DELETE FROM "MomentLike" WHERE "momentId"=$1 AND "userId"=$2`, momentID, me.Id); err != nil {
			serverError(w, err)
			return
		}
		liked = false
	} else if db.IsNotFound(err) {
		if _, err := h.deps.DB.Exec(ctx,
			`INSERT INTO "MomentLike"("id","momentId","userId","createdAt") VALUES($1,$2,$3,NOW())`,
			util.NewID(), momentID, me.Id); err != nil {
			serverError(w, err)
			return
		}
	} else {
		serverError(w, err)
		return
	}

	var count int64
	if err := h.deps.DB.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM "MomentLike" WHERE "momentId"=$1`, momentID).Scan(&count); err != nil {
		serverError(w, err)
		return
	}
	h.redisDelPattern(ctx, "moments:feed:"+me.Id+":*")

	if liked {
		var authorID string
		if err := h.deps.DB.Pool.QueryRow(ctx,
			`SELECT "userId" FROM "Moment" WHERE "id"=$1`, momentID).Scan(&authorID); err == nil && authorID != "" && authorID != me.Id {
			h.publishMomentEvent(ctx, "moment_like_notify", authorID, map[string]any{
				"momentId":   momentID,
				"userId":     me.Id,
				"userName":   displayName(me.Nickname, me.Username),
				"userAvatar": avatarToProxy(me.Avatar),
				"liked":      true,
				"likeCount":  count,
			})
		}
	}
	util.WriteJSON(w, 200, map[string]any{"liked": liked, "likeCount": count})
}

// postComment 发表评论（限流 20/分钟），通知动态作者与被回复者。
func (h *Handler) postComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	if me == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	var req struct {
		Content   *string `json:"content"`
		ReplyToID *string `json:"replyToId"`
	}
	if !decodeJSONAllowEmpty(w, r, &req) {
		return
	}
	content := ""
	if req.Content != nil {
		content = *req.Content
	}
	if strings.TrimSpace(content) == "" {
		util.WriteError(w, 400, "评论内容不能为空")
		return
	}
	if runeLen(content) > 1000 {
		util.WriteError(w, 400, "评论内容超出长度限制（1000 字）")
		return
	}
	if !commentLimiter.check("comment:" + me.Id) {
		util.WriteError(w, 429, "评论过于频繁，请稍后再试")
		return
	}

	momentID := r.PathValue("id")
	replyToID := req.ReplyToID
	if replyToID != nil && *replyToID == "" {
		replyToID = nil
	}
	trimmed := runeSlice(strings.TrimSpace(content), 1000)
	commentID := util.NewID()
	now := time.Now()
	if _, err := h.deps.DB.Exec(ctx,
		`INSERT INTO "MomentComment"("id","momentId","userId","content","replyToId","createdAt","updatedAt")
		 VALUES($1,$2,$3,$4,$5,NOW(),NOW())`,
		commentID, momentID, me.Id, trimmed, replyToID); err != nil {
		serverError(w, err)
		return
	}
	h.redisDelPattern(ctx, "moments:feed:*")

	comment := createdCommentJSON{
		ID: commentID, MomentID: momentID, UserID: me.Id, Content: trimmed,
		ReplyToID: replyToID, CreatedAt: now, UpdatedAt: now,
		User: &userLite{ID: me.Id, Username: me.Username, Nickname: me.Nickname, Avatar: me.Avatar},
	}

	// 通知动态作者
	var momentAuthorID string
	_ = h.deps.DB.Pool.QueryRow(ctx, `SELECT "userId" FROM "Moment" WHERE "id"=$1`, momentID).Scan(&momentAuthorID)
	if momentAuthorID != "" && momentAuthorID != me.Id {
		h.publishMomentEvent(ctx, "moment_comment_notify", momentAuthorID, map[string]any{
			"momentId":   momentID,
			"commentId":  commentID,
			"userId":     me.Id,
			"userName":   displayName(me.Nickname, me.Username),
			"userAvatar": avatarToProxy(me.Avatar),
			"content":    runeSlice(trimmed, 100),
			"replyToId":  replyToID,
		})
	}
	// 回复评论时也通知被回复者（排除自己与动态作者，避免重复）
	if replyToID != nil {
		var parentAuthorID string
		if err := h.deps.DB.Pool.QueryRow(ctx,
			`SELECT "userId" FROM "MomentComment" WHERE "id"=$1`, *replyToID).Scan(&parentAuthorID); err == nil &&
			parentAuthorID != "" && parentAuthorID != me.Id && parentAuthorID != momentAuthorID {
			h.publishMomentEvent(ctx, "moment_comment_notify", parentAuthorID, map[string]any{
				"momentId":   momentID,
				"commentId":  commentID,
				"userId":     me.Id,
				"userName":   displayName(me.Nickname, me.Username),
				"userAvatar": avatarToProxy(me.Avatar),
				"content":    runeSlice(trimmed, 100),
				"replyToId":  replyToID,
				"isReply":    true,
			})
		}
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "comment": comment})
}

// deleteComment 删除评论（评论作者或动态作者可删）。
func (h *Handler) deleteComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	if me == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	commentID := r.PathValue("commentId")
	type ccRow struct {
		ID       string `db:"id"`
		UserID   string `db:"userId"`
		AuthorID string `db:"authorId"`
	}
	row, err := db.QueryRowToStruct[ccRow](ctx, h.deps.DB,
		`SELECT c."id", c."userId", m."userId" AS "authorId"
		 FROM "MomentComment" c JOIN "Moment" m ON m."id" = c."momentId"
		 WHERE c."id"=$1`, commentID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "评论不存在")
			return
		}
		serverError(w, err)
		return
	}
	if row.UserID != me.Id && row.AuthorID != me.Id {
		util.WriteError(w, 403, "无权删除")
		return
	}
	if _, err := h.deps.DB.Exec(ctx, `DELETE FROM "MomentComment" WHERE "id"=$1`, commentID); err != nil {
		serverError(w, err)
		return
	}
	h.redisDelPattern(ctx, "moments:feed:*")
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// pin 置顶/取消置顶（仅作者；置顶时先取消该作者其他置顶）。
func (h *Handler) pin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	if me == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	var req struct {
		Pin *bool `json:"pin"`
	}
	if !decodeJSONAllowEmpty(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	m, notFound, err := h.findMoment(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	if notFound {
		util.WriteError(w, 404, "动态不存在")
		return
	}
	if m.UserId != me.Id {
		util.WriteError(w, 403, "只有作者可以置顶")
		return
	}
	pin := req.Pin != nil && *req.Pin
	if pin {
		if _, err := h.deps.DB.Exec(ctx,
			`UPDATE "Moment" SET "isPinned"=false, "pinnedAt"=NULL WHERE "userId"=$1 AND "isPinned"=true`, me.Id); err != nil {
			serverError(w, err)
			return
		}
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "Moment" SET "isPinned"=$1, "pinnedAt"=(CASE WHEN $1 THEN NOW() ELSE NULL END), "updatedAt"=NOW() WHERE "id"=$2`,
		pin, id); err != nil {
		serverError(w, err)
		return
	}
	updated, _, err := h.findMoment(ctx, id)
	if err != nil || updated == nil {
		serverError(w, err)
		return
	}
	h.redisDelPattern(ctx, "moments:feed:"+me.Id+":*")
	h.redisDelPattern(ctx, "moments:share:"+me.Id+":*")
	util.WriteJSON(w, 200, map[string]any{"success": true, "moment": updated})
}

// reorder 批量排序动态（仅作者，sortOrder = 序号+1）。
func (h *Handler) reorder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	if me == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSONAllowEmpty(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		util.WriteError(w, 400, "请提供排序列表")
		return
	}
	type idRow struct {
		ID string `db:"id"`
	}
	rows, err := db.QueryToStructs[idRow](ctx, h.deps.DB,
		`SELECT "id" FROM "Moment" WHERE "id"=ANY($1) AND "userId"=$2`, req.IDs, me.Id)
	if err != nil {
		serverError(w, err)
		return
	}
	valid := make(map[string]bool, len(rows))
	for _, r := range rows {
		valid[r.ID] = true
	}
	n := 0
	for _, id := range req.IDs {
		if !valid[id] {
			continue
		}
		n++
		if _, err := h.deps.DB.Exec(ctx, `UPDATE "Moment" SET "sortOrder"=$1 WHERE "id"=$2`, n, id); err != nil {
			serverError(w, err)
			return
		}
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// updateMoment 编辑动态（仅作者；visibility 仅接受 public/friends/private）。
func (h *Handler) updateMoment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := middleware.UserFrom(r)
	if me == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	var req struct {
		Content    *string `json:"content"`
		Visibility *string `json:"visibility"`
		Location   *string `json:"location"`
	}
	if !decodeJSONAllowEmpty(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	m, notFound, err := h.findMoment(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	if notFound {
		util.WriteError(w, 404, "动态不存在")
		return
	}
	if m.UserId != me.Id {
		util.WriteError(w, 403, "只有作者可以编辑")
		return
	}

	sets := []string{}
	args := []any{}
	ph := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if req.Content != nil {
		sets = append(sets, `"content"=`+ph(runeSlice(strings.TrimSpace(*req.Content), 5000)))
	}
	if req.Visibility != nil && (*req.Visibility == "public" || *req.Visibility == "friends" || *req.Visibility == "private") {
		sets = append(sets, `"visibility"=`+ph(*req.Visibility))
	}
	if req.Location != nil {
		sets = append(sets, `"location"=`+ph(runeSlice(*req.Location, 200)))
	}
	if len(sets) > 0 {
		sets = append(sets, `"updatedAt"=NOW()`)
		if _, err := h.deps.DB.Exec(ctx,
			`UPDATE "Moment" SET `+strings.Join(sets, ",")+` WHERE "id"=`+ph(id), args...); err != nil {
			serverError(w, err)
			return
		}
	}
	updated, _, err := h.findMoment(ctx, id)
	if err != nil || updated == nil {
		serverError(w, err)
		return
	}
	h.redisDelPattern(ctx, "moments:feed:*")
	_ = h.deps.Redis.Del(ctx, "moments:detail:"+id)
	h.redisDelPattern(ctx, "moments:share:"+m.UserId+":*")
	util.WriteJSON(w, 200, map[string]any{"success": true, "moment": updated})
}

// hotTopics 热门话题（L1 2 分钟 + Redis 5 分钟缓存）。
func (h *Handler) hotTopics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if v, ok := l1Get[hotTopicsResult]("topics:hot"); ok {
		util.WriteJSON(w, 200, v)
		return
	}
	var cached hotTopicsResult
	if h.redisGetJSON(ctx, "moments:topics:hot", &cached) {
		l1Set("topics:hot", cached, 120*time.Second)
		util.WriteJSON(w, 200, cached)
		return
	}
	type tRow struct {
		Topics *string `db:"topics"`
	}
	rows, err := db.QueryToStructs[tRow](ctx, h.deps.DB,
		`SELECT "topics" FROM "Moment" WHERE "topics" IS NOT NULL LIMIT 500`)
	if err != nil {
		serverError(w, err)
		return
	}
	counts := make(map[string]int)
	for _, r := range rows {
		for _, t := range splitTopics(r.Topics) {
			counts[t]++
		}
	}
	items := make([]topicItem, 0, len(counts))
	for name, c := range counts {
		items = append(items, topicItem{Name: name, Count: c})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Count > items[j].Count })
	if len(items) > 20 {
		items = items[:20]
	}
	result := hotTopicsResult{Topics: items}
	l1Set("topics:hot", result, 120*time.Second)
	h.redisSetJSON(ctx, "moments:topics:hot", result, 300*time.Second)
	util.WriteJSON(w, 200, result)
}
