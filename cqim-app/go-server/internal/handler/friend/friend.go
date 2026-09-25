// Package friend 移植自 server/friend.ts —— 好友关系 API（发送/接受/拒绝好友申请、
// 好友列表、删除好友、好友关系检查）。挂载在 /api/friend，全部路由需要登录。
package friend

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// RegisterRoutes 注册好友模块路由（全部包 d.Auth.UserAuth）。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := &Handler{deps: d}
	auth := d.Auth.UserAuth
	mux.Handle("POST /api/friend/request", auth(http.HandlerFunc(h.sendRequest)))
	mux.Handle("GET /api/friend/requests", auth(http.HandlerFunc(h.listRequests)))
	mux.Handle("POST /api/friend/accept/{requestId}", auth(http.HandlerFunc(h.acceptRequest)))
	mux.Handle("POST /api/friend/reject/{requestId}", auth(http.HandlerFunc(h.rejectRequest)))
	mux.Handle("GET /api/friend/list", auth(http.HandlerFunc(h.listFriends)))
	mux.Handle("DELETE /api/friend/{friendId}", auth(http.HandlerFunc(h.deleteFriend)))
	mux.Handle("GET /api/friend/check/{userId}", auth(http.HandlerFunc(h.checkFriend)))
}

// Handler 好友模块 handler。
type Handler struct {
	deps *handler.Deps
}

func (h *Handler) db() *db.DB { return h.deps.DB }

// ============ 辅助函数 ============

// normalizeUsers 规范化两个用户的顺序（字典序较小的为 A），
// 确保同一对用户始终映射到同一个 Friendship 记录。
func normalizeUsers(userA, userB string) (string, string) {
	if userA < userB {
		return userA, userB
	}
	return userB, userA
}

// areFriends 检查两个用户是否已经是好友。
func (h *Handler) areFriends(ctx context.Context, userID1, userID2 string) (bool, error) {
	a, b := normalizeUsers(userID1, userID2)
	_, err := db.QueryRowToStruct[db.Friendship](ctx, h.db(),
		`SELECT "id","userA","userB","createdAt" FROM "Friendship" WHERE "userA"=$1 AND "userB"=$2`,
		a, b)
	if err != nil {
		if db.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// createFriendship 创建好友关系（已存在则忽略）。
func (h *Handler) createFriendship(ctx context.Context, userID1, userID2 string) error {
	a, b := normalizeUsers(userID1, userID2)
	_, err := h.db().Exec(ctx,
		`INSERT INTO "Friendship"("id","userA","userB","createdAt") VALUES($1,$2,$3,NOW())
		 ON CONFLICT("userA","userB") DO NOTHING`,
		util.NewID(), a, b)
	return err
}

// ============ Redis 在线状态（对应 server/redis.ts） ============

const (
	onlinePrefix   = "online:"
	devicePrefix   = "devices:"
	lastSeenPrefix = "last_seen:"
)

// deviceInfo 对应 redis.ts 的 RedisDeviceInfo。
type deviceInfo struct {
	DeviceType string `json:"deviceType,omitempty"`
	Browser    string `json:"browser,omitempty"`
	OS         string `json:"os,omitempty"`
	IP         string `json:"ip,omitempty"`
}

// isUserOnline 检查用户是否在线（Redis 异常降级为 false）。
func (h *Handler) isUserOnline(ctx context.Context, userID string) bool {
	if h.deps.Redis == nil {
		return false
	}
	n, err := h.deps.Redis.RDB.Exists(ctx, onlinePrefix+userID).Result()
	if err != nil {
		return false
	}
	return n == 1
}

// getUserDevices 获取用户在线设备列表（异常降级为空）。
func (h *Handler) getUserDevices(ctx context.Context, userID string) []deviceInfo {
	out := []deviceInfo{}
	if h.deps.Redis == nil {
		return out
	}
	vals, err := h.deps.Redis.RDB.HGetAll(ctx, devicePrefix+userID).Result()
	if err != nil {
		return out
	}
	for _, raw := range vals {
		var d deviceInfo
		if json.Unmarshal([]byte(raw), &d) == nil {
			out = append(out, d)
		}
	}
	return out
}

// getUserLastSeen 获取用户最后在线时间戳（毫秒），无记录返回 nil。
func (h *Handler) getUserLastSeen(ctx context.Context, userID string) *int64 {
	if h.deps.Redis == nil {
		return nil
	}
	raw, found, err := h.deps.Redis.GetString(ctx, lastSeenPrefix+userID)
	if err != nil || !found || raw == "" {
		return nil
	}
	return parseMillis(raw)
}

func parseMillis(s string) *int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil
		}
		n = n*10 + int64(c-'0')
	}
	return &n
}

// ============ avatarToProxy（对应 server/cos-signer.ts） ============

func isCosURL(u string) bool {
	if u == "" || !strings.HasPrefix(u, "https://") {
		return false
	}
	return strings.Contains(u, ".cos.") || strings.Contains(u, ".myqcloud.com") || strings.Contains(u, "imim.chat")
}

var zhToASCIISeg = map[string]string{
	"头像":  "avatars",
	"群头像": "group-avatars",
	"朋友圈": "moments",
	"照片":  "photos",
	"视频":  "videos",
}

// cosKeyToAlias cosKey 中文段 → ASCII 别名（仅用于生成对外可见的 URL）。
func cosKeyToAlias(cosKey string) string {
	if cosKey == "" {
		return cosKey
	}
	segs := strings.Split(cosKey, "/")
	for i, s := range segs {
		if a, ok := zhToASCIISeg[s]; ok {
			segs[i] = a
		}
	}
	return strings.Join(segs, "/")
}

// avatarToProxy 将头像 COS 直链转换为代理 URL（附带缩略图处理参数），非 COS URL 原样返回。
func avatarToProxy(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	if !isCosURL(rawURL) {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	cosKey := strings.TrimPrefix(u.Path, "/")
	if decoded, err := url.PathUnescape(cosKey); err == nil {
		cosKey = decoded
	}
	aliasKey := cosKeyToAlias(cosKey)
	segs := strings.Split(aliasKey, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/api/cos/proxy/" + strings.Join(segs, "/") + "?imageMogr2/thumbnail/200x200/format/webp/quality/80"
}

// ============ POST /api/friend/request 发送好友申请 ============

type sendRequestBody struct {
	ToID         string  `json:"toId"`
	Message      string  `json:"message"`
	SearchMethod *string `json:"searchMethod"`
}

func (h *Handler) sendRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := middleware.UserFrom(r)

	var req sendRequestBody
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	toID := req.ToID
	message := req.Message
	searchMethod := "id"
	if req.SearchMethod != nil {
		searchMethod = *req.SearchMethod
	}

	if toID == "" {
		util.WriteError(w, 400, "缺少目标用户 ID")
		return
	}
	if toID == currentUser.Id {
		util.WriteError(w, 400, "不能向自己发送好友申请")
		return
	}

	// 验证目标用户存在
	targetUser, err := db.QueryRowToStruct[db.User](ctx, h.db(),
		`SELECT "id","username","nickname","avatar" FROM "User" WHERE "id"=$1`, toID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "用户不存在")
			return
		}
		log.Printf("[Friend] 发送好友申请失败: %v", err)
		util.WriteError(w, 500, "发送失败，请重试")
		return
	}
	_ = targetUser

	// 检查是否已经是好友
	alreadyFriends, err := h.areFriends(ctx, currentUser.Id, toID)
	if err != nil {
		log.Printf("[Friend] 发送好友申请失败: %v", err)
		util.WriteError(w, 500, "发送失败，请重试")
		return
	}
	if alreadyFriends {
		util.WriteError(w, 400, "已经是好友了")
		return
	}

	// 检查是否已有待处理的申请（双向检查）
	existing, err := db.QueryRowToStruct[db.FriendRequest](ctx, h.db(),
		`SELECT "id","fromId","toId","message","status","searchMethod","createdAt","updatedAt"
		 FROM "FriendRequest"
		 WHERE ((("fromId"=$1 AND "toId"=$2) OR ("fromId"=$2 AND "toId"=$1)) AND "status"='pending')
		 LIMIT 1`,
		currentUser.Id, toID)
	if err != nil && !db.IsNotFound(err) {
		log.Printf("[Friend] 发送好友申请失败: %v", err)
		util.WriteError(w, 500, "发送失败，请重试")
		return
	}

	if existing != nil {
		if existing.FromId == currentUser.Id {
			util.WriteError(w, 400, "已发送过好友申请，等待对方处理")
			return
		}
		// 对方已向我发送申请，直接接受
		tx, err := h.db().Pool.Begin(ctx)
		if err != nil {
			log.Printf("[Friend] 发送好友申请失败: %v", err)
			util.WriteError(w, 500, "发送失败，请重试")
			return
		}
		defer tx.Rollback(ctx)
		userA, userB := normalizeUsers(currentUser.Id, toID)
		if _, err := tx.Exec(ctx, `UPDATE "FriendRequest" SET "status"='accepted',"updatedAt"=NOW() WHERE "id"=$1`, existing.Id); err != nil {
			log.Printf("[Friend] 发送好友申请失败: %v", err)
			util.WriteError(w, 500, "发送失败，请重试")
			return
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO "Friendship"("id","userA","userB","createdAt") VALUES($1,$2,$3,NOW())
			 ON CONFLICT("userA","userB") DO NOTHING`,
			util.NewID(), userA, userB); err != nil {
			log.Printf("[Friend] 发送好友申请失败: %v", err)
			util.WriteError(w, 500, "发送失败，请重试")
			return
		}
		if err := tx.Commit(ctx); err != nil {
			log.Printf("[Friend] 发送好友申请失败: %v", err)
			util.WriteError(w, 500, "发送失败，请重试")
			return
		}
		util.WriteJSON(w, 200, map[string]any{
			"success":      true,
			"message":      "对方已向你发送申请，已自动成为好友",
			"autoAccepted": true,
		})
		return
	}

	// 创建新申请（使用 upsert 避免重复）
	var fr db.FriendRequest
	err = h.db().Pool.QueryRow(ctx,
		`INSERT INTO "FriendRequest"("id","fromId","toId","message","searchMethod","status","createdAt","updatedAt")
		 VALUES($1,$2,$3,$4,$5,'pending',NOW(),NOW())
		 ON CONFLICT("fromId","toId") DO UPDATE
		 SET "message"=EXCLUDED."message","searchMethod"=EXCLUDED."searchMethod","status"='pending',"updatedAt"=NOW()
		 RETURNING "id","fromId","toId","message","status","searchMethod","createdAt","updatedAt"`,
		util.NewID(), currentUser.Id, toID, message, searchMethod,
	).Scan(&fr.Id, &fr.FromId, &fr.ToId, &fr.Message, &fr.Status, &fr.SearchMethod, &fr.CreatedAt, &fr.UpdatedAt)
	if err != nil {
		log.Printf("[Friend] 发送好友申请失败: %v", err)
		util.WriteError(w, 500, "发送失败，请重试")
		return
	}

	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"request": map[string]any{
			"id":           fr.Id,
			"fromId":       fr.FromId,
			"toId":         fr.ToId,
			"message":      util.StrVal(fr.Message),
			"status":       fr.Status,
			"searchMethod": fr.SearchMethod,
			"createdAt":    fr.CreatedAt.UnixMilli(),
		},
	})
}

// ============ GET /api/friend/requests 获取好友申请列表 ============

// briefUser 申请列表附带的用户简要信息。
type briefUser struct {
	ID       string  `db:"id"`
	Username string  `db:"username"`
	Nickname *string `db:"nickname"`
	Avatar   *string `db:"avatar"`
}

func (h *Handler) listRequests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := middleware.UserFrom(r)
	typ := r.URL.Query().Get("type")
	if typ == "" {
		typ = "all"
	}

	var where string
	var args []any
	switch typ {
	case "received":
		where = `"toId"=$1`
		args = []any{currentUser.Id}
	case "sent":
		where = `"fromId"=$1`
		args = []any{currentUser.Id}
	default:
		where = `("toId"=$1 OR "fromId"=$1)`
		args = []any{currentUser.Id}
	}

	requests, err := db.QueryToStructs[db.FriendRequest](ctx, h.db(),
		`SELECT "id","fromId","toId","message","status","searchMethod","createdAt","updatedAt"
		 FROM "FriendRequest" WHERE `+where+` ORDER BY "createdAt" DESC LIMIT 100`,
		args...)
	if err != nil {
		log.Printf("[Friend] 获取好友申请失败: %v", err)
		util.WriteError(w, 500, "获取失败")
		return
	}

	// 批量获取相关用户信息
	userIDSet := map[string]struct{}{}
	for _, rq := range requests {
		userIDSet[rq.FromId] = struct{}{}
		userIDSet[rq.ToId] = struct{}{}
	}
	userIDs := make([]string, 0, len(userIDSet))
	for id := range userIDSet {
		userIDs = append(userIDs, id)
	}
	userMap := map[string]briefUser{}
	if len(userIDs) > 0 {
		users, err := db.QueryToStructs[briefUser](ctx, h.db(),
			`SELECT "id","username","nickname","avatar" FROM "User" WHERE "id" = ANY($1)`, userIDs)
		if err != nil {
			log.Printf("[Friend] 获取好友申请失败: %v", err)
			util.WriteError(w, 500, "获取失败")
			return
		}
		for _, u := range users {
			userMap[u.ID] = u
		}
	}

	items := make([]map[string]any, 0, len(requests))
	for _, rq := range requests {
		fromUser := userMap[rq.FromId]
		toUser := userMap[rq.ToId]
		fromName := util.StrVal(fromUser.Nickname)
		if fromName == "" {
			fromName = fromUser.Username
		}
		if fromName == "" {
			fromName = rq.FromId
		}
		fromUniqueID := fromUser.Username
		if fromUniqueID == "" {
			fromUniqueID = rq.FromId
		}
		toName := util.StrVal(toUser.Nickname)
		if toName == "" {
			toName = toUser.Username
		}
		if toName == "" {
			toName = rq.ToId
		}
		items = append(items, map[string]any{
			"id":           rq.Id,
			"fromId":       rq.FromId,
			"fromName":     fromName,
			"fromUniqueId": fromUniqueID,
			"fromAvatar":   avatarToProxy(util.StrVal(fromUser.Avatar)),
			"toId":         rq.ToId,
			"toName":       toName,
			"toAvatar":     avatarToProxy(util.StrVal(toUser.Avatar)),
			"message":      util.StrVal(rq.Message),
			"status":       rq.Status,
			"searchMethod": rq.SearchMethod,
			"timestamp":    rq.CreatedAt.UnixMilli(),
			"isIncoming":   rq.ToId == currentUser.Id,
		})
	}

	util.WriteJSON(w, 200, map[string]any{"requests": items})
}

// ============ POST /api/friend/accept/:requestId 接受好友申请 ============

func (h *Handler) acceptRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := middleware.UserFrom(r)
	requestID := r.PathValue("requestId")

	fr, err := db.QueryRowToStruct[db.FriendRequest](ctx, h.db(),
		`SELECT "id","fromId","toId","message","status","searchMethod","createdAt","updatedAt"
		 FROM "FriendRequest" WHERE "id"=$1`, requestID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "申请不存在")
			return
		}
		log.Printf("[Friend] 接受好友申请失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}

	if fr.ToId != currentUser.Id {
		util.WriteError(w, 403, "无权操作此申请")
		return
	}
	if fr.Status != "pending" {
		if fr.Status == "accepted" {
			util.WriteError(w, 400, "申请已接受")
		} else {
			util.WriteError(w, 400, "申请已拒绝")
		}
		return
	}

	tx, err := h.db().Pool.Begin(ctx)
	if err != nil {
		log.Printf("[Friend] 接受好友申请失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE "FriendRequest" SET "status"='accepted',"updatedAt"=NOW() WHERE "id"=$1`, requestID); err != nil {
		log.Printf("[Friend] 接受好友申请失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	userA, userB := normalizeUsers(fr.FromId, fr.ToId)
	if _, err := tx.Exec(ctx,
		`INSERT INTO "Friendship"("id","userA","userB","createdAt") VALUES($1,$2,$3,NOW())
		 ON CONFLICT("userA","userB") DO NOTHING`,
		util.NewID(), userA, userB); err != nil {
		log.Printf("[Friend] 接受好友申请失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		log.Printf("[Friend] 接受好友申请失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}

	// 自动创建私聊会话（如果不存在）
	participantA, participantB := normalizeUsers(fr.FromId, fr.ToId)
	var chatID string
	err = h.db().Pool.QueryRow(ctx,
		`SELECT "id" FROM "Chat" WHERE "participantA"=$1 AND "participantB"=$2`,
		participantA, participantB).Scan(&chatID)
	if err != nil {
		chatID = util.NewID()
		if _, err := h.db().Exec(ctx,
			`INSERT INTO "Chat"("id","participantA","participantB","createdAt","updatedAt")
			 VALUES($1,$2,$3,NOW(),NOW()) ON CONFLICT("participantA","participantB") DO NOTHING`,
			chatID, participantA, participantB); err != nil {
			log.Printf("[Friend] 接受好友申请失败: %v", err)
			util.WriteError(w, 500, "操作失败")
			return
		}
		// 并发时可能对方已创建，重新读取确保拿到 id
		_ = h.db().Pool.QueryRow(ctx,
			`SELECT "id" FROM "Chat" WHERE "participantA"=$1 AND "participantB"=$2`,
			participantA, participantB).Scan(&chatID)
	}

	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"message": "已接受好友申请",
		"chatId":  chatID,
	})
}

// ============ POST /api/friend/reject/:requestId 拒绝好友申请 ============

func (h *Handler) rejectRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := middleware.UserFrom(r)
	requestID := r.PathValue("requestId")

	fr, err := db.QueryRowToStruct[db.FriendRequest](ctx, h.db(),
		`SELECT "id","fromId","toId","message","status","searchMethod","createdAt","updatedAt"
		 FROM "FriendRequest" WHERE "id"=$1`, requestID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "申请不存在")
			return
		}
		log.Printf("[Friend] 拒绝好友申请失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}

	if fr.ToId != currentUser.Id {
		util.WriteError(w, 403, "无权操作此申请")
		return
	}
	if fr.Status != "pending" {
		if fr.Status == "accepted" {
			util.WriteError(w, 400, "申请已接受")
		} else {
			util.WriteError(w, 400, "申请已拒绝")
		}
		return
	}

	if _, err := h.db().Exec(ctx,
		`UPDATE "FriendRequest" SET "status"='rejected',"updatedAt"=NOW() WHERE "id"=$1`, requestID); err != nil {
		log.Printf("[Friend] 拒绝好友申请失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}

	util.WriteJSON(w, 200, map[string]any{"success": true, "message": "已拒绝好友申请"})
}

// ============ GET /api/friend/list 获取好友列表 ============

// friendUser 好友列表用的用户字段。
type friendUser struct {
	ID       string  `db:"id"`
	Username string  `db:"username"`
	Nickname *string `db:"nickname"`
	Avatar   *string `db:"avatar"`
	Bio      *string `db:"bio"`
	Phone    *string `db:"phone"`
	Email    *string `db:"email"`
}

type friendItem struct {
	ID          string       `json:"id"`
	UniqueID    string       `json:"uniqueId"`
	Name        string       `json:"name"`
	Avatar      string       `json:"avatar"`
	Bio         string       `json:"bio"`
	Status      string       `json:"status"`
	Letter      string       `json:"letter"`
	Online      bool         `json:"online"`
	Devices     []deviceInfo `json:"devices"`
	DeviceLabel *string      `json:"deviceLabel"`
	LastSeen    *int64       `json:"lastSeen"`
	Phone       *string      `json:"phone,omitempty"`
	Email       *string      `json:"email,omitempty"`
}

var (
	phoneMaskRe = regexp.MustCompile(`(\d{3})\d{4}(\d{4})`)
	emailMaskRe = regexp.MustCompile(`(.{2}).*(@.*)`)
)

// maskPhone 手机号脱敏（与 TS 相同：仅匹配 3-4-4 格式时替换）。
func maskPhone(phone string) string {
	return phoneMaskRe.ReplaceAllString(phone, "$1****$2")
}

// maskEmail 邮箱脱敏。
func maskEmail(email string) string {
	return emailMaskRe.ReplaceAllString(email, "$1***$2")
}

// firstLetter 取名字首字母（仅 ASCII 字母，否则为 '#'）。
func firstLetter(name string) string {
	if name == "" {
		return "#"
	}
	up := unicode.ToUpper([]rune(name)[0])
	if up >= 'A' && up <= 'Z' {
		return string(up)
	}
	return "#"
}

func (h *Handler) listFriends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := middleware.UserFrom(r)

	friendships, err := db.QueryToStructs[db.Friendship](ctx, h.db(),
		`SELECT "id","userA","userB","createdAt" FROM "Friendship"
		 WHERE "userA"=$1 OR "userB"=$1 ORDER BY "createdAt" DESC`, currentUser.Id)
	if err != nil {
		log.Printf("[Friend] 获取好友列表失败: %v", err)
		util.WriteError(w, 500, "获取失败")
		return
	}

	friendIDs := make([]string, 0, len(friendships))
	for _, f := range friendships {
		if f.UserA == currentUser.Id {
			friendIDs = append(friendIDs, f.UserB)
		} else {
			friendIDs = append(friendIDs, f.UserA)
		}
	}
	if len(friendIDs) == 0 {
		util.WriteJSON(w, 200, map[string]any{"friends": []any{}})
		return
	}

	users, err := db.QueryToStructs[friendUser](ctx, h.db(),
		`SELECT "id","username","nickname","avatar","bio","phone","email" FROM "User"
		 WHERE "id" = ANY($1) AND "isBanned"=false`, friendIDs)
	if err != nil {
		log.Printf("[Friend] 获取好友列表失败: %v", err)
		util.WriteError(w, 500, "获取失败")
		return
	}

	// 在线状态（Redis 异常降级为离线）
	onlineMap := map[string]bool{}
	for _, u := range users {
		onlineMap[u.ID] = h.isUserOnline(ctx, u.ID)
	}

	// 在线用户设备、离线用户最后在线时间
	devicesMap := map[string][]deviceInfo{}
	lastSeenMap := map[string]*int64{}
	for _, u := range users {
		if onlineMap[u.ID] {
			devicesMap[u.ID] = h.getUserDevices(ctx, u.ID)
		} else {
			lastSeenMap[u.ID] = h.getUserLastSeen(ctx, u.ID)
		}
	}

	friends := make([]friendItem, 0, len(users))
	for _, u := range users {
		name := util.StrVal(u.Nickname)
		if name == "" {
			name = u.Username
		}
		online := onlineMap[u.ID]
		devices := devicesMap[u.ID]
		if devices == nil {
			devices = []deviceInfo{}
		}
		var deviceLabel *string
		if online && len(devices) > 0 {
			label := devices[0].OS + " · " + devices[0].Browser
			deviceLabel = &label
		}
		var lastSeen *int64
		if !online {
			lastSeen = lastSeenMap[u.ID]
		}
		item := friendItem{
			ID:          u.ID,
			UniqueID:    u.Username,
			Name:        name,
			Avatar:      avatarToProxy(util.StrVal(u.Avatar)),
			Bio:         util.StrVal(u.Bio),
			Letter:      firstLetter(name),
			Online:      online,
			Devices:     devices,
			DeviceLabel: deviceLabel,
			LastSeen:    lastSeen,
		}
		if online {
			item.Status = "online"
		} else {
			item.Status = "offline"
		}
		if u.Phone != nil && *u.Phone != "" {
			masked := maskPhone(*u.Phone)
			item.Phone = &masked
		}
		if u.Email != nil && *u.Email != "" {
			masked := maskEmail(*u.Email)
			item.Email = &masked
		}
		friends = append(friends, item)
	}

	// 在线优先；'#' 字母排最后；再按字母、名字排序
	sort.Slice(friends, func(i, j int) bool {
		a, b := friends[i], friends[j]
		if a.Online != b.Online {
			return a.Online
		}
		aHash := a.Letter == "#"
		bHash := b.Letter == "#"
		if aHash != bHash {
			return !aHash
		}
		if a.Letter != b.Letter {
			return a.Letter < b.Letter
		}
		return a.Name < b.Name
	})

	util.WriteJSON(w, 200, map[string]any{"friends": friends})
}

// ============ DELETE /api/friend/:friendId 删除好友 ============

func (h *Handler) deleteFriend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := middleware.UserFrom(r)
	friendID := r.PathValue("friendId")

	userA, userB := normalizeUsers(currentUser.Id, friendID)
	_, err := db.QueryRowToStruct[db.Friendship](ctx, h.db(),
		`SELECT "id","userA","userB","createdAt" FROM "Friendship" WHERE "userA"=$1 AND "userB"=$2`,
		userA, userB)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "好友关系不存在")
			return
		}
		log.Printf("[Friend] 删除好友失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}

	if _, err := h.db().Exec(ctx,
		`DELETE FROM "Friendship" WHERE "userA"=$1 AND "userB"=$2`, userA, userB); err != nil {
		log.Printf("[Friend] 删除好友失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}

	util.WriteJSON(w, 200, map[string]any{"success": true, "message": "已删除好友"})
}

// ============ GET /api/friend/check/:userId 检查好友关系 ============

func (h *Handler) checkFriend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := middleware.UserFrom(r)
	userID := r.PathValue("userId")

	isFriend, err := h.areFriends(ctx, currentUser.Id, userID)
	if err != nil {
		log.Printf("[Friend] 检查好友关系失败: %v", err)
		util.WriteError(w, 500, "查询失败")
		return
	}

	pending, err := db.QueryRowToStruct[db.FriendRequest](ctx, h.db(),
		`SELECT "id","fromId","toId","message","status","searchMethod","createdAt","updatedAt"
		 FROM "FriendRequest"
		 WHERE ((("fromId"=$1 AND "toId"=$2) OR ("fromId"=$2 AND "toId"=$1)) AND "status"='pending')
		 LIMIT 1`,
		currentUser.Id, userID)
	if err != nil && !db.IsNotFound(err) {
		log.Printf("[Friend] 检查好友关系失败: %v", err)
		util.WriteError(w, 500, "查询失败")
		return
	}

	var direction *string
	if pending != nil {
		d := "received"
		if pending.FromId == currentUser.Id {
			d = "sent"
		}
		direction = &d
	}

	util.WriteJSON(w, 200, map[string]any{
		"isFriend":          isFriend,
		"hasPendingRequest": pending != nil,
		"requestDirection":  direction,
	})
}
