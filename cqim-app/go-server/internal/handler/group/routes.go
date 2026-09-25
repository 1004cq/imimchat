package group

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/media"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// Handler 群模块 HTTP handler（挂载在 /api/group，与 TS groupRouter 路由一致）。
type Handler struct {
	deps   *handler.Deps
	engine *Engine
}

// RegisterRoutes 注册群模块路由。TS 中 groupRouter 挂载时未包 userAuth，
// 这里保持一致：路由从 body/query 取 userId 等身份参数。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps, e *Engine) {
	h := &Handler{deps: d, engine: e}
	mux.Handle("POST /api/group/send", http.HandlerFunc(h.send))
	mux.Handle("GET /api/group/messages", http.HandlerFunc(h.messages))
	mux.Handle("POST /api/group/ack", http.HandlerFunc(h.ack))
	mux.Handle("GET /api/group/unread", http.HandlerFunc(h.unread))
	mux.Handle("POST /api/group/create", http.HandlerFunc(h.create))
	mux.Handle("GET /api/group/list", http.HandlerFunc(h.list))
	mux.Handle("GET /api/group/info", http.HandlerFunc(h.info))
	mux.Handle("GET /api/group/members", http.HandlerFunc(h.members))
	mux.Handle("POST /api/group/join", http.HandlerFunc(h.join))
	mux.Handle("GET /api/group/search", http.HandlerFunc(h.search))
	mux.Handle("POST /api/group/invite/create", http.HandlerFunc(h.inviteCreate))
	mux.Handle("GET /api/group/invite/list", http.HandlerFunc(h.inviteList))
	mux.Handle("POST /api/group/invite/revoke", http.HandlerFunc(h.inviteRevoke))
	mux.Handle("POST /api/group/invite/join", http.HandlerFunc(h.inviteJoin))
	mux.Handle("GET /api/group/peer/resolve", http.HandlerFunc(h.peerResolve))
	mux.Handle("POST /api/group/username/set", http.HandlerFunc(h.usernameSet))
	mux.Handle("PUT /api/group/update/name", http.HandlerFunc(h.updateName))
	mux.Handle("PUT /api/group/update/avatar", http.HandlerFunc(h.updateAvatar))
	mux.Handle("PUT /api/group/update/username", http.HandlerFunc(h.updateUsername))
	mux.Handle("PUT /api/group/update", http.HandlerFunc(h.updateBatch))
	mux.Handle("POST /api/group/leave", http.HandlerFunc(h.leave))
	mux.Handle("POST /api/group/kick", http.HandlerFunc(h.kick))
	mux.Handle("POST /api/group/transfer", http.HandlerFunc(h.transfer))
	mux.Handle("POST /api/group/admin/set", http.HandlerFunc(h.adminSet))
	mux.Handle("PUT /api/group/announcement", http.HandlerFunc(h.announcement))
	mux.Handle("PUT /api/group/member/nickname", http.HandlerFunc(h.memberNickname))
	mux.Handle("POST /api/group/mute", http.HandlerFunc(h.mute))
	mux.Handle("POST /api/group/dissolve", http.HandlerFunc(h.dissolve))
	mux.Handle("POST /api/group/qrcode", http.HandlerFunc(h.qrcode))
	mux.Handle("POST /api/group/invite-members", http.HandlerFunc(h.inviteMembers))
	mux.Handle("GET /api/group/invites", http.HandlerFunc(h.invites))
	mux.Handle("POST /api/group/invite-accept/{inviteId}", http.HandlerFunc(h.inviteAccept))
	mux.Handle("POST /api/group/invite-reject/{inviteId}", http.HandlerFunc(h.inviteReject))
}

func (h *Handler) ctx(r *http.Request) context.Context { return r.Context() }

// ============ 辅助函数 ============

var usernameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{4,31}$`)

// avatarToProxy 把 COS 直链转换为站内代理 URL（与 server/cos-signer.ts avatarToProxy 一致）。
func avatarToProxy(raw string) string {
	if raw == "" {
		return ""
	}
	if !isCosURL(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	segs := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	for i, s := range segs {
		if a, ok := zhToASCIISeg[s]; ok {
			segs[i] = a
		}
		segs[i] = url.PathEscape(segs[i])
	}
	safePath := strings.Join(segs, "/")
	return "/api/cos/proxy/" + safePath + "?imageMogr2/thumbnail/200x200/format/webp/quality/80"
}

func isCosURL(raw string) bool {
	if !strings.HasPrefix(raw, "https://") {
		return false
	}
	return strings.Contains(raw, ".cos.") || strings.Contains(raw, ".myqcloud.com") || strings.Contains(raw, "imim.chat")
}

var zhToASCIISeg = map[string]string{
	"头像":  "avatars",
	"群头像": "group-avatars",
	"朋友圈": "moments",
	"照片":  "photos",
	"视频":  "videos",
}

// publicUrl 拼接站外完整 URL（与 server/public-url.ts 一致）。
func publicUrl(path string) string {
	base := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL"))
	if base == "" {
		base = "https://cq.je"
	}
	base = strings.TrimSuffix(base, "/")
	if path == "" {
		return base
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// generateInviteHash 生成邀请链接 hash（22 位 URL 安全 base64，与 TS 一致）。
func generateInviteHash() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return util.NewID()
	}
	return base64.RawURLEncoding.EncodeToString(b[:])[:22]
}

// generateGroupDialogID 生成 TG 风格群 Dialog ID（移植自 server/utils/peerId.ts）。
func generateGroupDialogID(isSupergroup bool) string {
	id := generateInternalID()
	if isSupergroup {
		return strconv.FormatInt(-1000000000000-id, 10)
	}
	return strconv.FormatInt(-id, 10)
}

var (
	peerMu     = make(chan struct{}, 1)
	peerSeq    int64
	peerLastTs int64
)

func init() { peerMu <- struct{}{} }

// generateInternalID 类雪花算法内部 ID（53 位安全整数内）。
func generateInternalID() int64 {
	const epoch = int64(1704067200000) // 2024-01-01 UTC
	<-peerMu
	defer func() { peerMu <- struct{}{} }()
	now := time.Now().UnixMilli()
	ts := now - epoch
	if ts == peerLastTs {
		peerSeq = (peerSeq + 1) & 0xFF
		if peerSeq == 0 {
			for time.Now().UnixMilli() <= peerLastTs+epoch {
			}
		}
	} else {
		peerSeq = 0
	}
	peerLastTs = ts
	var rb [1]byte
	_, _ = rand.Read(rb[:])
	random := int64(rb[0] & 0x0F)
	return (ts << 12) | (random << 8) | peerSeq
}

// getPeerTypeByDialogID 根据 Dialog ID 判断 peer 类型（移植自 peerId.ts getPeerType）。
func getPeerTypeByDialogID(dialogID string) string {
	id, err := strconv.ParseInt(strings.TrimSpace(dialogID), 10, 64)
	if err != nil {
		return "group"
	}
	if id > 0 {
		return "user"
	}
	if id > -1000000000000 {
		return "group"
	}
	return "supergroup"
}

// persistGroupAvatar 群头像持久化（MinIO）。Go 版 media 模块尚未移植，暂返回未实现错误。
func persistGroupAvatar(groupID string, data []byte, ext, mimeType string) (string, error) {
	return "", errors.New("persistGroupAvatar 已废弃，请使用 media.SaveImageForGroup")
}

// sysMessage 异步发送系统消息（不阻塞请求，与 TS .catch 语义一致）。
func (h *Handler) sysMessage(groupID, content string) {
	go func() {
		if _, _, err := h.engine.SendGroupMessage(context.Background(), SendParams{
			GroupID: groupID, SenderID: "system", SenderName: "系统",
			MsgType: "system", Content: content,
		}); err != nil {
			log.Printf("[GroupMsg] 发送系统消息失败 groupId=%s: %v", groupID, err)
		}
	}()
}

// memberRole 查询成员角色；非成员返回 ("", false, nil)。
func (h *Handler) memberRole(ctx context.Context, groupID, userID string) (string, bool, error) {
	var role string
	err := h.deps.DB.Pool.QueryRow(ctx,
		`SELECT "role" FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`, groupID, userID).Scan(&role)
	if err != nil {
		if db.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return role, true, nil
}

// usernameTaken 检查用户名是否被用户或其它群占用（excludeGroupID 可为空）。
func (h *Handler) usernameTaken(ctx context.Context, username, excludeGroupID string) (bool, error) {
	var n int
	if err := h.deps.DB.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM "User" WHERE "username"=$1`, username).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	q := `SELECT COUNT(*) FROM "Group" WHERE "username"=$1`
	args := []any{username}
	if excludeGroupID != "" {
		q += ` AND "id"<>$2`
		args = append(args, excludeGroupID)
	}
	if err := h.deps.DB.Pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// joinGroup 加入群组（供 /join、/invite/join 复用，与 TS joinGroup 一致）。
func (h *Handler) joinGroup(ctx context.Context, groupID, userID, nickname string) error {
	var lastMsgSeq int64
	var gtype string
	if err := h.deps.DB.Pool.QueryRow(ctx,
		`SELECT "lastMsgSeq","type" FROM "Group" WHERE "id"=$1`, groupID).Scan(&lastMsgSeq, &gtype); err != nil {
		return errString("群组不存在")
	}
	tx, err := h.deps.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var nn any
	if nickname != "" {
		nn = nickname
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO "GroupMember"("id","groupId","userId","nickname","role","lastAckSeq","joinTime","createdAt","updatedAt")
		 VALUES($1,$2,$3,$4,'member',$5,NOW(),NOW(),NOW())`,
		util.NewID(), groupID, userID, nn, lastMsgSeq); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE "Group" SET "memberCount"="memberCount"+1, "updatedAt"=NOW() WHERE "id"=$1`, groupID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// 频道不发送"加入了群聊"消息，保持频道消息流干净
	if gtype != "channel" {
		name := nickname
		if name == "" {
			name = userID
		}
		h.sysMessage(groupID, name+" 加入了群聊")
	}
	return nil
}

// userBrief 批量查用户昵称/头像。
func (h *Handler) userBriefMap(ctx context.Context, userIDs []string) map[string]db.User {
	m := make(map[string]db.User)
	if len(userIDs) == 0 {
		return m
	}
	users, err := db.QueryToStructs[db.User](ctx, h.deps.DB,
		`SELECT "id","nickname","username","avatar" FROM "User" WHERE "id"=ANY($1)`, userIDs)
	if err != nil {
		return m
	}
	for _, u := range users {
		m[u.Id] = u
	}
	return m
}

func displayName(u db.User, fallback string) string {
	if s := util.StrVal(u.Nickname); s != "" {
		return s
	}
	if u.Username != "" {
		return u.Username
	}
	return fallback
}

func writeErr(w http.ResponseWriter, err error) {
	util.WriteError(w, 500, err.Error())
}

func parseIntQuery(q url.Values, key string, def int) int {
	if s := q.Get(key); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}

func parseInt64Query(q url.Values, key string) *int64 {
	if s := q.Get(key); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return &n
		}
	}
	return nil
}

// ============ 路由实现 ============

// POST /api/group/send 发送群消息
func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID    string          `json:"groupId"`
		SenderID   string          `json:"senderId"`
		SenderName string          `json:"senderName"`
		MsgType    string          `json:"msgType"`
		Content    string          `json:"content"`
		ReplyToID  string          `json:"replyToId"`
		Extra      json.RawMessage `json:"extra"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.SenderID == "" || req.Content == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	seq, ts, err := h.engine.SendGroupMessage(h.ctx(r), SendParams{
		GroupID: req.GroupID, SenderID: req.SenderID, SenderName: req.SenderName,
		MsgType: req.MsgType, Content: req.Content, ReplyToID: req.ReplyToID, Extra: req.Extra,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "seq": seq, "timestamp": ts})
}

// GET /api/group/messages 拉取群历史消息
func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	groupID, userID := q.Get("groupId"), q.Get("userId")
	if groupID == "" || userID == "" {
		util.WriteError(w, 400, "缺少 groupId 或 userId")
		return
	}
	limit := parseIntQuery(q, "limit", 50)
	res, err := h.engine.PullGroupMessages(h.ctx(r), PullParams{
		GroupID: groupID, UserID: userID,
		AfterSeq: parseInt64Query(q, "afterSeq"), BeforeSeq: parseInt64Query(q, "beforeSeq"), Limit: limit,
	})
	if err != nil {
		if strings.Contains(err.Error(), "非群成员") {
			util.WriteError(w, 403, err.Error())
		} else {
			writeErr(w, err)
		}
		return
	}
	util.WriteJSON(w, 200, res)
}

// POST /api/group/ack 确认已读
func (h *Handler) ack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID    string `json:"groupId"`
		UserID     string `json:"userId"`
		LastAckSeq *int64 `json:"lastAckSeq"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" || req.LastAckSeq == nil {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	unread, err := h.engine.ack(h.ctx(r), req.GroupID, req.UserID, *req.LastAckSeq)
	if err != nil {
		writeErr(w, err)
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "unread": unread})
}

// GET /api/group/unread 获取未读数
func (h *Handler) unread(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	res, err := h.engine.GetGroupUnreadCounts(h.ctx(r), userID)
	if err != nil {
		writeErr(w, err)
		return
	}
	util.WriteJSON(w, 200, res)
}

// POST /api/group/create 创建群组
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string   `json:"name"`
		OwnerID    string   `json:"ownerId"`
		MemberIDs  []string `json:"memberIds"`
		Type       string   `json:"type"`
		MaxMembers *int32   `json:"maxMembers"`
		Username   string   `json:"username"`
		IsPublic   bool     `json:"isPublic"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.OwnerID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	if req.Username != "" {
		if !usernameRegex.MatchString(req.Username) {
			util.WriteError(w, 400, "群组用户名必须以字母开头，5-32位字母数字下划线")
			return
		}
		taken, err := h.usernameTaken(h.ctx(r), req.Username, "")
		if err != nil {
			writeErr(w, err)
			return
		}
		if taken {
			util.WriteError(w, 409, "该用户名已被占用")
			return
		}
	}
	gtype := req.Type
	if gtype == "" {
		gtype = "normal"
	}
	maxMembers := int32(500)
	if req.MaxMembers != nil {
		maxMembers = *req.MaxMembers
	}
	isSupergroup := gtype == "super" || gtype == "channel" || maxMembers > 200
	dialogID := generateGroupDialogID(isSupergroup)
	// Channel 保留 channel 类型；supergroup 转为 super
	resolvedType := gtype
	if gtype != "channel" && isSupergroup {
		resolvedType = "super"
	}
	resolvedMaxMembers := maxMembers
	if gtype == "channel" {
		resolvedMaxMembers = 0
	}

	ctx := h.ctx(r)
	groupID := util.NewID()
	tx, err := h.deps.DB.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var un any
	if req.Username != "" {
		un = req.Username
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO "Group"("id","dialogId","name","username","ownerId","type","isPublic","maxMembers","memberCount","createdAt","updatedAt")
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW())`,
		groupID, dialogID, req.Name, un, req.OwnerID, resolvedType, req.IsPublic, resolvedMaxMembers, len(req.MemberIDs)+1); err != nil {
		writeErr(w, err)
		return
	}
	members := []struct {
		userID string
		role   string
	}{{req.OwnerID, "owner"}}
	for _, id := range req.MemberIDs {
		if id != req.OwnerID {
			members = append(members, struct {
				userID string
				role   string
			}{id, "member"})
		}
	}
	for _, m := range members {
		if _, err := tx.Exec(ctx,
			`INSERT INTO "GroupMember"("id","groupId","userId","role","lastAckSeq","joinTime","createdAt","updatedAt")
			 VALUES($1,$2,$3,$4,0,NOW(),NOW(),NOW())`,
			util.NewID(), groupID, m.userID, m.role); err != nil {
			writeErr(w, err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, err)
		return
	}
	h.engine.InitGroupOnline(groupID)
	log.Printf("[GroupMsg] 群组已创建: id=%s dialogId=%s name=%s members=%d", groupID, dialogID, req.Name, len(members))
	util.WriteJSON(w, 200, map[string]any{"ok": true, "groupId": groupID, "dialogId": dialogID})
}

// GET /api/group/list 获取我的群聊列表
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	type row struct {
		GroupID     string    `db:"groupId"`
		LastAckSeq  int64     `db:"lastAckSeq"`
		ID          string    `db:"id"`
		DialogID    *string   `db:"dialogId"`
		Name        string    `db:"name"`
		Username    *string   `db:"username"`
		Avatar      *string   `db:"avatar"`
		OwnerID     string    `db:"ownerId"`
		Type        string    `db:"type"`
		IsPublic    bool      `db:"isPublic"`
		MemberCount int32     `db:"memberCount"`
		LastMsgSeq  int64     `db:"lastMsgSeq"`
		CreatedAt   time.Time `db:"createdAt"`
		UpdatedAt   time.Time `db:"updatedAt"`
	}
	rows, err := db.QueryToStructs[row](h.ctx(r), h.deps.DB,
		`SELECT m."groupId", m."lastAckSeq", g."id", g."dialogId", g."name", g."username", g."avatar",
		 g."ownerId", g."type", g."isPublic", g."memberCount", g."lastMsgSeq", g."createdAt", g."updatedAt"
		 FROM "GroupMember" m JOIN "Group" g ON g."id"=m."groupId"
		 WHERE m."userId"=$1 ORDER BY m."updatedAt" DESC`, userID)
	if err != nil {
		writeErr(w, err)
		return
	}
	groups := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		unread := m.LastMsgSeq - m.LastAckSeq
		if unread < 0 {
			unread = 0
		}
		groups = append(groups, map[string]any{
			"id": m.ID, "groupId": m.ID,
			"dialogId":    strOrNil(m.DialogID),
			"name":        m.Name,
			"username":    strOrNil(m.Username),
			"avatar":      avatarToProxy(util.StrVal(m.Avatar)),
			"ownerId":     m.OwnerID,
			"type":        m.Type,
			"isPublic":    m.IsPublic,
			"memberCount": m.MemberCount,
			"lastMessage": "🔒 [加密消息]",
			"unreadCount": unread,
			"createdAt":   m.CreatedAt.UnixMilli(),
			"updatedAt":   m.UpdatedAt.UnixMilli(),
		})
	}
	util.WriteJSON(w, 200, map[string]any{"groups": groups})
}

func strOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// GET /api/group/info 获取群信息
func (h *Handler) info(w http.ResponseWriter, r *http.Request) {
	groupID := r.URL.Query().Get("groupId")
	if groupID == "" {
		util.WriteError(w, 400, "缺少 groupId")
		return
	}
	g, err := db.QueryRowToStruct[db.Group](h.ctx(r), h.deps.DB,
		`SELECT * FROM "Group" WHERE "id"=$1`, groupID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "群组不存在")
		} else {
			writeErr(w, err)
		}
		return
	}
	var memberCount int64
	_ = h.deps.DB.Pool.QueryRow(h.ctx(r),
		`SELECT COUNT(*) FROM "GroupMember" WHERE "groupId"=$1`, groupID).Scan(&memberCount)
	util.WriteJSON(w, 200, map[string]any{
		"id": g.Id, "dialogId": strOrNil(g.DialogId), "name": g.Name,
		"username": strOrNil(g.Username), "avatar": avatarToProxy(util.StrVal(g.Avatar)),
		"ownerId": g.OwnerId, "type": g.Type, "isPublic": g.IsPublic,
		"maxMembers": g.MaxMembers, "memberCount": memberCount,
		"lastMsgSeq":   strconv.FormatInt(g.LastMsgSeq, 10),
		"lastMsgTime":  timeOrISO(g.LastMsgTime),
		"announcement": strOrNil(g.Announcement),
		"createdAt":    isoMillis(g.CreatedAt), "updatedAt": isoMillis(g.UpdatedAt),
	})
}

// GET /api/group/members 获取群成员列表（分页）
func (h *Handler) members(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	groupID := q.Get("groupId")
	if groupID == "" {
		util.WriteError(w, 400, "缺少 groupId")
		return
	}
	page := parseIntQuery(q, "page", 1)
	pageSize := parseIntQuery(q, "pageSize", 100)
	if page < 1 {
		page = 1
	}
	ctx := h.ctx(r)
	rawMembers, err := db.QueryToStructs[db.GroupMember](ctx, h.deps.DB,
		`SELECT * FROM "GroupMember" WHERE "groupId"=$1 ORDER BY "role" ASC, "joinTime" ASC LIMIT $2 OFFSET $3`,
		groupID, pageSize, (page-1)*pageSize)
	if err != nil {
		writeErr(w, err)
		return
	}
	var total int64
	if err := h.deps.DB.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM "GroupMember" WHERE "groupId"=$1`, groupID).Scan(&total); err != nil {
		writeErr(w, err)
		return
	}
	userIDs := make([]string, 0, len(rawMembers))
	for _, m := range rawMembers {
		userIDs = append(userIDs, m.UserId)
	}
	userMap := h.userBriefMap(ctx, userIDs)
	members := make([]map[string]any, 0, len(rawMembers))
	for _, m := range rawMembers {
		u := userMap[m.UserId]
		name := util.StrVal(m.Nickname)
		if name == "" {
			name = displayName(u, m.UserId)
		}
		members = append(members, map[string]any{
			"id": m.Id, "groupId": m.GroupId, "userId": m.UserId, "role": m.Role,
			"nickname":   strOrNil(m.Nickname),
			"lastAckSeq": strconv.FormatInt(m.LastAckSeq, 10),
			"joinTime":   isoMillis(m.JoinTime),
			"muteUntil":  timeOrISO(m.MuteUntil),
			"createdAt":  isoMillis(m.CreatedAt), "updatedAt": isoMillis(m.UpdatedAt),
			"name":   name,
			"avatar": avatarToProxy(util.StrVal(u.Avatar)),
		})
	}
	totalPages := 0
	if pageSize > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	util.WriteJSON(w, 200, map[string]any{
		"members": members, "total": total, "page": page, "pageSize": pageSize, "totalPages": totalPages,
	})
}

// POST /api/group/join 加入群组
func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID  string `json:"groupId"`
		UserID   string `json:"userId"`
		Nickname string `json:"nickname"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	if err := h.joinGroup(h.ctx(r), req.GroupID, req.UserID, req.Nickname); err != nil {
		writeErr(w, err)
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// GET /api/group/search 搜索群组
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	if keyword == "" {
		util.WriteError(w, 400, "请输入至少1个字符")
		return
	}
	groups, err := db.QueryToStructs[db.Group](h.ctx(r), h.deps.DB,
		`SELECT "id","dialogId","username","name","avatar","memberCount","type","isPublic" FROM "Group"
		 WHERE "id"=$1 OR "dialogId"=$1 OR "username"=$1 OR "name" LIKE $2 LIMIT 10`,
		keyword, "%"+keyword+"%")
	if err != nil {
		util.WriteError(w, 500, "搜索失败")
		return
	}
	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, map[string]any{
			"id": g.Id, "dialogId": strOrNil(g.DialogId), "username": strOrNil(g.Username),
			"name": g.Name, "avatar": avatarToProxy(util.StrVal(g.Avatar)),
			"memberCount": g.MemberCount, "type": g.Type, "isPublic": g.IsPublic,
		})
	}
	util.WriteJSON(w, 200, map[string]any{"groups": out})
}

// POST /api/group/invite/create 创建邀请链接
func (h *Handler) inviteCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID     string   `json:"groupId"`
		CreatorID   string   `json:"creatorId"`
		Name        string   `json:"name"`
		ExpireHours *float64 `json:"expireHours"`
		ExpireAt    string   `json:"expireAt"`
		MaxUses     *int     `json:"maxUses"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.CreatorID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.CreatorID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		util.WriteError(w, 403, "非群成员")
		return
	}
	if role == "member" {
		util.WriteError(w, 403, "仅管理员和群主可创建邀请链接")
		return
	}
	maxUses := 0
	if req.MaxUses != nil {
		maxUses = *req.MaxUses
	}
	if maxUses < 0 {
		util.WriteError(w, 400, "使用次数限制无效")
		return
	}
	var expireAt *time.Time
	if req.ExpireAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpireAt)
		if err != nil {
			util.WriteError(w, 400, "失效时间无效")
			return
		}
		expireAt = &t
	} else if req.ExpireHours != nil {
		hours := *req.ExpireHours
		if hours <= 0 {
			util.WriteError(w, 400, "有效时长无效")
			return
		}
		t := time.Now().Add(time.Duration(hours * float64(time.Hour)))
		expireAt = &t
	}
	if expireAt != nil && !expireAt.After(time.Now()) {
		util.WriteError(w, 400, "失效时间必须晚于当前时间")
		return
	}
	hash := generateInviteHash()
	var name any
	if req.Name != "" {
		name = req.Name
	}
	var expAny any
	if expireAt != nil {
		expAny = *expireAt
	}
	linkID := util.NewID()
	if _, err := h.deps.DB.Exec(ctx,
		`INSERT INTO "InviteLink"("id","hash","groupId","creatorId","name","expireAt","maxUses","usedCount","isRevoked","createdAt")
		 VALUES($1,$2,$3,$4,$5,$6,$7,0,false,NOW())`,
		linkID, hash, req.GroupID, req.CreatorID, name, expAny, maxUses); err != nil {
		writeErr(w, err)
		return
	}
	util.WriteJSON(w, 200, map[string]any{
		"ok": true,
		"inviteLink": map[string]any{
			"id": linkID, "hash": hash,
			"url":      "/im/+" + hash,
			"fullUrl":  publicUrl("/im/+" + hash),
			"name":     strOrNil((*string)(nilIfEmpty(req.Name))),
			"expireAt": timeOrISO(expireAt),
			"maxUses":  maxUses, "usedCount": 0,
		},
	})
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isoMillis 格式化为 TS Date.toISOString() 风格（带毫秒的 UTC）。
func isoMillis(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func timeOrISO(t *time.Time) any {
	if t == nil {
		return nil
	}
	return isoMillis(*t)
}

// GET /api/group/invite/list 获取群的邀请链接列表
func (h *Handler) inviteList(w http.ResponseWriter, r *http.Request) {
	groupID := r.URL.Query().Get("groupId")
	if groupID == "" {
		util.WriteError(w, 400, "缺少 groupId")
		return
	}
	links, err := db.QueryToStructs[db.InviteLink](h.ctx(r), h.deps.DB,
		`SELECT * FROM "InviteLink" WHERE "groupId"=$1 AND "isRevoked"=false ORDER BY "createdAt" DESC`, groupID)
	if err != nil {
		writeErr(w, err)
		return
	}
	now := time.Now()
	out := make([]map[string]any, 0, len(links))
	for _, l := range links {
		expiredByTime := l.ExpireAt != nil && now.After(*l.ExpireAt)
		expiredByUses := l.MaxUses > 0 && l.UsedCount >= l.MaxUses
		var remaining any
		if l.MaxUses > 0 {
			rem := l.MaxUses - l.UsedCount
			if rem < 0 {
				rem = 0
			}
			remaining = rem
		}
		out = append(out, map[string]any{
			"id": l.Id, "hash": l.Hash,
			"url":           "/im/+" + l.Hash,
			"fullUrl":       publicUrl("/im/+" + l.Hash),
			"name":          strOrNil(l.Name),
			"creatorId":     l.CreatorId,
			"expireAt":      timeOrISO(l.ExpireAt),
			"maxUses":       l.MaxUses,
			"usedCount":     l.UsedCount,
			"remainingUses": remaining,
			"isExpired":     expiredByTime || expiredByUses,
			"createdAt":     isoMillis(l.CreatedAt),
		})
	}
	util.WriteJSON(w, 200, map[string]any{"links": out})
}

// POST /api/group/invite/revoke 撤销邀请链接
func (h *Handler) inviteRevoke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hash      string `json:"hash"`
		CreatorID string `json:"creatorId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Hash == "" || req.CreatorID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	link, err := db.QueryRowToStruct[db.InviteLink](ctx, h.deps.DB,
		`SELECT * FROM "InviteLink" WHERE "hash"=$1`, req.Hash)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "邀请链接不存在")
		} else {
			writeErr(w, err)
		}
		return
	}
	if link.CreatorId != req.CreatorID {
		role, ok, err := h.memberRole(ctx, link.GroupId, req.CreatorID)
		if err != nil {
			writeErr(w, err)
			return
		}
		if !ok || role != "owner" {
			util.WriteError(w, 403, "无权撤销此链接")
			return
		}
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "InviteLink" SET "isRevoked"=true WHERE "hash"=$1`, req.Hash); err != nil {
		writeErr(w, err)
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/group/invite/join 通过邀请链接加入群组
func (h *Handler) inviteJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hash     string `json:"hash"`
		UserID   string `json:"userId"`
		Nickname string `json:"nickname"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Hash == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	link, err := db.QueryRowToStruct[db.InviteLink](ctx, h.deps.DB,
		`SELECT * FROM "InviteLink" WHERE "hash"=$1`, req.Hash)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "邀请链接不存在")
		} else {
			writeErr(w, err)
		}
		return
	}
	if link.IsRevoked {
		util.WriteError(w, 410, "邀请链接已被撤销")
		return
	}
	if link.ExpireAt != nil && time.Now().After(*link.ExpireAt) {
		util.WriteError(w, 410, "邀请链接已过期")
		return
	}
	if link.MaxUses > 0 && link.UsedCount >= link.MaxUses {
		util.WriteError(w, 410, "邀请链接已达到最大使用次数")
		return
	}
	if _, ok, err := h.memberRole(ctx, link.GroupId, req.UserID); err != nil {
		writeErr(w, err)
		return
	} else if ok {
		util.WriteJSON(w, 200, map[string]any{"ok": true, "alreadyMember": true, "groupId": link.GroupId})
		return
	}
	var memberCount, maxMembers int32
	var groupName string
	if err := h.deps.DB.Pool.QueryRow(ctx,
		`SELECT "memberCount","maxMembers","name" FROM "Group" WHERE "id"=$1`, link.GroupId).
		Scan(&memberCount, &maxMembers, &groupName); err != nil {
		writeErr(w, err)
		return
	}
	if memberCount >= maxMembers {
		util.WriteError(w, 403, "群组已满员")
		return
	}
	if err := h.joinGroup(ctx, link.GroupId, req.UserID, req.Nickname); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "InviteLink" SET "usedCount"="usedCount"+1 WHERE "hash"=$1`, req.Hash); err != nil {
		writeErr(w, err)
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "groupId": link.GroupId, "groupName": groupName})
}

// GET /api/group/peer/resolve 通过 dialogId 查询 Peer 信息
func (h *Handler) peerResolve(w http.ResponseWriter, r *http.Request) {
	dialogID := r.URL.Query().Get("dialogId")
	if dialogID == "" {
		util.WriteError(w, 400, "缺少 dialogId")
		return
	}
	ctx := h.ctx(r)
	peerType := getPeerTypeByDialogID(dialogID)
	if peerType == "user" {
		u, err := db.QueryRowToStruct[db.User](ctx, h.deps.DB,
			`SELECT "id","dialogId","username","nickname","avatar","bio","isBot" FROM "User" WHERE "dialogId"=$1`, dialogID)
		if err != nil {
			if db.IsNotFound(err) {
				util.WriteError(w, 404, "用户不存在")
			} else {
				writeErr(w, err)
			}
			return
		}
		pt := "user"
		if u.IsBot {
			pt = "bot"
		}
		util.WriteJSON(w, 200, map[string]any{
			"peerType": pt,
			"data": map[string]any{
				"id": u.Id, "dialogId": strOrNil(u.DialogId), "username": strOrNil(nilIfEmpty(u.Username)),
				"nickname": strOrNil(u.Nickname), "avatar": avatarToProxy(util.StrVal(u.Avatar)),
				"bio": strOrNil(u.Bio), "isBot": u.IsBot,
			},
		})
		return
	}
	g, err := db.QueryRowToStruct[db.Group](ctx, h.deps.DB,
		`SELECT "id","dialogId","username","name","avatar","type","isPublic","memberCount" FROM "Group" WHERE "dialogId"=$1`, dialogID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "群组不存在")
		} else {
			writeErr(w, err)
		}
		return
	}
	pt := "group"
	if g.Type == "channel" {
		pt = "channel"
	} else if peerType == "supergroup" {
		pt = "supergroup"
	}
	util.WriteJSON(w, 200, map[string]any{
		"peerType": pt,
		"data": map[string]any{
			"id": g.Id, "dialogId": strOrNil(g.DialogId), "username": strOrNil(g.Username),
			"name": g.Name, "avatar": avatarToProxy(util.StrVal(g.Avatar)),
			"type": g.Type, "isPublic": g.IsPublic, "memberCount": g.MemberCount,
		},
	})
}

// POST /api/group/username/set 设置群组公开用户名
func (h *Handler) usernameSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID  string `json:"groupId"`
		UserID   string `json:"userId"`
		Username string `json:"username"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || (role != "owner" && role != "admin") {
		util.WriteError(w, 403, "仅群主和管理员可设置用户名")
		return
	}
	if req.Username != "" {
		if !usernameRegex.MatchString(req.Username) {
			util.WriteError(w, 400, "用户名必须以字母开头，5-32位字母数字下划线")
			return
		}
		taken, err := h.usernameTaken(ctx, req.Username, req.GroupID)
		if err != nil {
			writeErr(w, err)
			return
		}
		if taken {
			util.WriteError(w, 409, "该用户名已被占用")
			return
		}
	}
	var un any
	if req.Username != "" {
		un = req.Username
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "Group" SET "username"=$2, "isPublic"=$3, "updatedAt"=NOW() WHERE "id"=$1`,
		req.GroupID, un, req.Username != ""); err != nil {
		writeErr(w, err)
		return
	}
	var groupPublicURL any
	if req.Username != "" {
		groupPublicURL = publicUrl("/im/" + req.Username)
	}
	util.WriteJSON(w, 200, map[string]any{
		"ok": true, "username": strOrNil(nilIfEmpty(req.Username)), "publicUrl": groupPublicURL,
	})
}

// PUT /api/group/update/name 修改群名称
func (h *Handler) updateName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID string `json:"groupId"`
		UserID  string `json:"userId"`
		Name    string `json:"name"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" || strings.TrimSpace(req.Name) == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || (role != "owner" && role != "admin") {
		util.WriteError(w, 403, "仅群主和管理员可修改群名称")
		return
	}
	trimmed := strings.TrimSpace(req.Name)
	if len([]rune(trimmed)) > 30 {
		util.WriteError(w, 400, "群名称不能超过30个字符")
		return
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "Group" SET "name"=$2, "updatedAt"=NOW() WHERE "id"=$1`, req.GroupID, trimmed); err != nil {
		writeErr(w, err)
		return
	}
	h.sysMessage(req.GroupID, "群名称已修改为「"+trimmed+"」")
	util.WriteJSON(w, 200, map[string]any{"ok": true, "name": trimmed})
}

// PUT /api/group/update/avatar 修改群头像
func (h *Handler) updateAvatar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID    string `json:"groupId"`
		UserID     string `json:"userId"`
		DataBase64 string `json:"dataBase64"`
		MimeType   string `json:"mimeType"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || (role != "owner" && role != "admin") {
		util.WriteError(w, 403, "仅群主和管理员可修改群头像")
		return
	}
	if req.DataBase64 == "" {
		util.WriteError(w, 400, "缺少头像数据")
		return
	}
	extMap := map[string]string{
		"image/jpeg": ".jpg", "image/jpg": ".jpg", "image/png": ".png",
		"image/gif": ".gif", "image/webp": ".webp",
	}
	mime := req.MimeType
	if mime == "" {
		mime = "image/jpeg"
	}
	ext := extMap[mime]
	if ext == "" {
		ext = ".jpg"
	}
	buf, err := base64.StdEncoding.DecodeString(req.DataBase64)
	if err != nil {
		util.WriteError(w, 400, "头像数据格式错误")
		return
	}
	if len(buf) > 5*1024*1024 {
		util.WriteError(w, 400, "头像文件过大，最大 5MB")
		return
	}
	avatarURL, err := media.SaveImageForGroup(ctx, h.deps, req.GroupID, buf, mime,
		"group_"+req.GroupID+"_avatar"+ext)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "Group" SET "avatar"=$2, "updatedAt"=NOW() WHERE "id"=$1`, req.GroupID, avatarURL); err != nil {
		writeErr(w, err)
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "avatar": avatarURL, "storage": "minio"})
}

// PUT /api/group/update/username 修改群 ID（公开用户名，仅群主）
func (h *Handler) updateUsername(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID  string `json:"groupId"`
		UserID   string `json:"userId"`
		Username string `json:"username"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || role != "owner" {
		util.WriteError(w, 403, "仅群主可修改群 ID")
		return
	}
	if req.Username != "" {
		if !usernameRegex.MatchString(req.Username) {
			util.WriteError(w, 400, "群 ID 必须以字母开头，5-32位字母数字下划线")
			return
		}
		taken, err := h.usernameTaken(ctx, req.Username, req.GroupID)
		if err != nil {
			writeErr(w, err)
			return
		}
		if taken {
			util.WriteError(w, 409, "该 ID 已被占用")
			return
		}
	}
	var un any
	if req.Username != "" {
		un = req.Username
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "Group" SET "username"=$2, "isPublic"=$3, "updatedAt"=NOW() WHERE "id"=$1`,
		req.GroupID, un, req.Username != ""); err != nil {
		writeErr(w, err)
		return
	}
	var groupPublicURL any
	if req.Username != "" {
		groupPublicURL = publicUrl("/im/" + req.Username)
	}
	util.WriteJSON(w, 200, map[string]any{
		"ok": true, "username": strOrNil(nilIfEmpty(req.Username)), "publicUrl": groupPublicURL,
	})
}

// PUT /api/group/update 批量修改群信息
func (h *Handler) updateBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID        string  `json:"groupId"`
		UserID         string  `json:"userId"`
		Name           *string `json:"name"`
		Username       *string `json:"username"`
		AvatarBase64   string  `json:"avatarBase64"`
		AvatarMimeType string  `json:"avatarMimeType"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || (role != "owner" && role != "admin") {
		util.WriteError(w, 403, "仅群主和管理员可修改群信息")
		return
	}
	setClauses := []string{}
	args := []any{req.GroupID}
	changes := []string{}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			util.WriteError(w, 400, "群名称不能为空")
			return
		}
		if len([]rune(trimmed)) > 30 {
			util.WriteError(w, 400, "群名称不能超过30个字符")
			return
		}
		args = append(args, trimmed)
		setClauses = append(setClauses, `"name"=$`+strconv.Itoa(len(args)))
		changes = append(changes, "群名称修改为「"+trimmed+"」")
	}
	if req.Username != nil {
		if role != "owner" {
			util.WriteError(w, 403, "仅群主可修改群 ID")
			return
		}
		un := *req.Username
		if un != "" {
			if !usernameRegex.MatchString(un) {
				util.WriteError(w, 400, "群 ID 必须以字母开头，5-32位字母数字下划线")
				return
			}
			taken, err := h.usernameTaken(ctx, un, req.GroupID)
			if err != nil {
				writeErr(w, err)
				return
			}
			if taken {
				util.WriteError(w, 409, "该 ID 已被占用")
				return
			}
		}
		var unAny any
		if un != "" {
			unAny = un
		}
		args = append(args, unAny)
		setClauses = append(setClauses, `"username"=$`+strconv.Itoa(len(args)))
		args = append(args, un != "")
		setClauses = append(setClauses, `"isPublic"=$`+strconv.Itoa(len(args)))
		if un != "" {
			changes = append(changes, "群 ID 设置为 @"+un)
		} else {
			changes = append(changes, "群 ID 已清除")
		}
	}
	if req.AvatarBase64 != "" {
		extMap := map[string]string{
			"image/jpeg": ".jpg", "image/jpg": ".jpg", "image/png": ".png",
			"image/gif": ".gif", "image/webp": ".webp",
		}
		mime := req.AvatarMimeType
		if mime == "" {
			mime = "image/jpeg"
		}
		ext := extMap[mime]
		if ext == "" {
			ext = ".jpg"
		}
		buf, err := base64.StdEncoding.DecodeString(req.AvatarBase64)
		if err != nil {
			util.WriteError(w, 400, "头像数据格式错误")
			return
		}
		if len(buf) > 5*1024*1024 {
			util.WriteError(w, 400, "头像文件过大，最大 5MB")
			return
		}
		avatarURL, err := persistGroupAvatar(req.GroupID, buf, ext, mime)
		if err != nil {
			writeErr(w, err)
			return
		}
		args = append(args, avatarURL)
		setClauses = append(setClauses, `"avatar"=$`+strconv.Itoa(len(args)))
		changes = append(changes, "群头像已更新")
	}
	if len(setClauses) == 0 {
		util.WriteError(w, 400, "没有需要修改的内容")
		return
	}
	setClauses = append(setClauses, `"updatedAt"=NOW()`)
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "Group" SET `+strings.Join(setClauses, ", ")+` WHERE "id"=$1 RETURNING "id","name","avatar","username","isPublic"`,
		args...); err != nil {
		writeErr(w, err)
		return
	}
	g, err := db.QueryRowToStruct[db.Group](ctx, h.deps.DB,
		`SELECT "id","name","avatar","username","isPublic" FROM "Group" WHERE "id"=$1`, req.GroupID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(changes) > 0 {
		h.sysMessage(req.GroupID, strings.Join(changes, "，"))
	}
	util.WriteJSON(w, 200, map[string]any{
		"ok": true,
		"group": map[string]any{
			"id": g.Id, "name": g.Name, "avatar": avatarToProxy(util.StrVal(g.Avatar)),
			"username": strOrNil(g.Username), "isPublic": g.IsPublic,
		},
	})
}

// POST /api/group/leave 退出群聊
func (h *Handler) leave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID string `json:"groupId"`
		UserID  string `json:"userId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		util.WriteError(w, 404, "你不是该群成员")
		return
	}
	if role == "owner" {
		util.WriteError(w, 403, "群主不能直接退出群聊，请先转让群主")
		return
	}
	tx, err := h.deps.DB.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`DELETE FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`, req.GroupID, req.UserID); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE "Group" SET "memberCount"=GREATEST("memberCount"-1,0), "updatedAt"=NOW() WHERE "id"=$1`, req.GroupID); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, err)
		return
	}
	h.sysMessage(req.GroupID, req.UserID+" 已退出群聊")
	h.engine.LeaveGroupOnline(req.GroupID, req.UserID)
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/group/kick 踢出成员
func (h *Handler) kick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID      string `json:"groupId"`
		OperatorID   string `json:"operatorId"`
		TargetUserID string `json:"targetUserId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.OperatorID == "" || req.TargetUserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	opRole, ok, err := h.memberRole(ctx, req.GroupID, req.OperatorID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || (opRole != "owner" && opRole != "admin") {
		util.WriteError(w, 403, "仅群主和管理员可以踢出成员")
		return
	}
	targetRole, ok, err := h.memberRole(ctx, req.GroupID, req.TargetUserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		util.WriteError(w, 404, "目标用户不是群成员")
		return
	}
	if targetRole == "owner" {
		util.WriteError(w, 403, "不能踢出群主")
		return
	}
	if targetRole == "admin" && opRole != "owner" {
		util.WriteError(w, 403, "仅群主可以踢出管理员")
		return
	}
	tx, err := h.deps.DB.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`DELETE FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`, req.GroupID, req.TargetUserID); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE "Group" SET "memberCount"=GREATEST("memberCount"-1,0), "updatedAt"=NOW() WHERE "id"=$1`, req.GroupID); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, err)
		return
	}
	h.sysMessage(req.GroupID, req.TargetUserID+" 已被移出群聊")
	h.engine.LeaveGroupOnline(req.GroupID, req.TargetUserID)
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/group/transfer 转让群主
func (h *Handler) transfer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID    string `json:"groupId"`
		OwnerID    string `json:"ownerId"`
		NewOwnerID string `json:"newOwnerId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.OwnerID == "" || req.NewOwnerID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.OwnerID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || role != "owner" {
		util.WriteError(w, 403, "仅群主可以转让")
		return
	}
	if _, ok, err := h.memberRole(ctx, req.GroupID, req.NewOwnerID); err != nil {
		writeErr(w, err)
		return
	} else if !ok {
		util.WriteError(w, 404, "目标用户不是群成员")
		return
	}
	tx, err := h.deps.DB.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE "GroupMember" SET "role"='admin', "updatedAt"=NOW() WHERE "groupId"=$1 AND "userId"=$2`,
		req.GroupID, req.OwnerID); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE "GroupMember" SET "role"='owner', "updatedAt"=NOW() WHERE "groupId"=$1 AND "userId"=$2`,
		req.GroupID, req.NewOwnerID); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE "Group" SET "ownerId"=$2, "updatedAt"=NOW() WHERE "id"=$1`, req.GroupID, req.NewOwnerID); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, err)
		return
	}
	h.sysMessage(req.GroupID, "群主已转让给 "+req.NewOwnerID)
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/group/admin/set 设置/取消管理员
func (h *Handler) adminSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID      string `json:"groupId"`
		OwnerID      string `json:"ownerId"`
		TargetUserID string `json:"targetUserId"`
		IsAdmin      bool   `json:"isAdmin"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.OwnerID == "" || req.TargetUserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.OwnerID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || role != "owner" {
		util.WriteError(w, 403, "仅群主可以设置管理员")
		return
	}
	targetRole, ok, err := h.memberRole(ctx, req.GroupID, req.TargetUserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		util.WriteError(w, 404, "目标用户不是群成员")
		return
	}
	if targetRole == "owner" {
		util.WriteError(w, 403, "不能修改群主角色")
		return
	}
	newRole := "member"
	if req.IsAdmin {
		newRole = "admin"
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "GroupMember" SET "role"=$3, "updatedAt"=NOW() WHERE "groupId"=$1 AND "userId"=$2`,
		req.GroupID, req.TargetUserID, newRole); err != nil {
		writeErr(w, err)
		return
	}
	if req.IsAdmin {
		h.sysMessage(req.GroupID, req.TargetUserID+" 已被设为管理员")
	} else {
		h.sysMessage(req.GroupID, req.TargetUserID+" 已被取消管理员")
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "role": newRole})
}

// PUT /api/group/announcement 设置群公告
func (h *Handler) announcement(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID      string `json:"groupId"`
		UserID       string `json:"userId"`
		Announcement string `json:"announcement"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || (role != "owner" && role != "admin") {
		util.WriteError(w, 403, "仅群主和管理员可以设置群公告")
		return
	}
	var ann any
	if req.Announcement != "" {
		ann = req.Announcement
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "Group" SET "announcement"=$2, "updatedAt"=NOW() WHERE "id"=$1`, req.GroupID, ann); err != nil {
		writeErr(w, err)
		return
	}
	if req.Announcement != "" {
		h.sysMessage(req.GroupID, "群公告已更新："+req.Announcement)
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "announcement": strOrNil(nilIfEmpty(req.Announcement))})
}

// PUT /api/group/member/nickname 更新我在本群的昵称
func (h *Handler) memberNickname(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID  string `json:"groupId"`
		UserID   string `json:"userId"`
		Nickname string `json:"nickname"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	if _, ok, err := h.memberRole(ctx, req.GroupID, req.UserID); err != nil {
		writeErr(w, err)
		return
	} else if !ok {
		util.WriteError(w, 404, "你不在该群中")
		return
	}
	trimmed := strings.TrimSpace(req.Nickname)
	if len([]rune(trimmed)) > 20 {
		util.WriteError(w, 400, "昵称不能超过20个字符")
		return
	}
	var nn any
	if trimmed != "" {
		nn = trimmed
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "GroupMember" SET "nickname"=$3, "updatedAt"=NOW() WHERE "groupId"=$1 AND "userId"=$2`,
		req.GroupID, req.UserID, nn); err != nil {
		writeErr(w, err)
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "nickname": strOrNil(nilIfEmpty(trimmed))})
}

// POST /api/group/mute 禁言/解除禁言
func (h *Handler) mute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID      string `json:"groupId"`
		OperatorID   string `json:"operatorId"`
		TargetUserID string `json:"targetUserId"`
		Duration     *int64 `json:"duration"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.OperatorID == "" || req.TargetUserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	opRole, ok, err := h.memberRole(ctx, req.GroupID, req.OperatorID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || (opRole != "owner" && opRole != "admin") {
		util.WriteError(w, 403, "仅群主和管理员可以禁言")
		return
	}
	targetRole, ok, err := h.memberRole(ctx, req.GroupID, req.TargetUserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		util.WriteError(w, 404, "目标用户不是群成员")
		return
	}
	if targetRole == "owner" {
		util.WriteError(w, 403, "不能禁言群主")
		return
	}
	if targetRole == "admin" && opRole != "owner" {
		util.WriteError(w, 403, "仅群主可以禁言管理员")
		return
	}
	var duration int64
	if req.Duration != nil {
		duration = *req.Duration
	}
	var muteUntil *time.Time
	if duration > 0 {
		t := time.Now().Add(time.Duration(duration) * time.Second)
		muteUntil = &t
	}
	var muAny any
	if muteUntil != nil {
		muAny = *muteUntil
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "GroupMember" SET "muteUntil"=$3, "updatedAt"=NOW() WHERE "groupId"=$1 AND "userId"=$2`,
		req.GroupID, req.TargetUserID, muAny); err != nil {
		writeErr(w, err)
		return
	}
	var durationText string
	switch {
	case muteUntil == nil:
		durationText = "已解除禁言"
	case duration >= 86400:
		durationText = "已被禁言 " + strconv.FormatInt(duration/86400, 10) + " 天"
	case duration >= 3600:
		durationText = "已被禁言 " + strconv.FormatInt(duration/3600, 10) + " 小时"
	default:
		durationText = "已被禁言 " + strconv.FormatInt(duration/60, 10) + " 分钟"
	}
	h.sysMessage(req.GroupID, req.TargetUserID+" "+durationText)
	util.WriteJSON(w, 200, map[string]any{"ok": true, "muteUntil": timeOrISO(muteUntil)})
}

// POST /api/group/dissolve 解散群聊（仅群主）
func (h *Handler) dissolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID string `json:"groupId"`
		OwnerID string `json:"ownerId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.OwnerID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	role, ok, err := h.memberRole(ctx, req.GroupID, req.OwnerID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok || role != "owner" {
		util.WriteError(w, 403, "仅群主可以解散群聊")
		return
	}
	// 先发送解散通知（同步等待落库，与 TS await 语义一致）
	if _, _, err := h.engine.SendGroupMessage(h.ctx(r), SendParams{
		GroupID: req.GroupID, SenderID: "system", SenderName: "系统",
		MsgType: "system", Content: "该群已被群主解散",
	}); err != nil {
		log.Printf("[GroupMsg] 发送解散通知失败: %v", err)
	}
	// 删除群组（级联删除成员和消息）
	if _, err := h.deps.DB.Exec(ctx, `DELETE FROM "Group" WHERE "id"=$1`, req.GroupID); err != nil {
		writeErr(w, err)
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/group/qrcode 群二维码（长期有效的默认邀请链接）
func (h *Handler) qrcode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID string `json:"groupId"`
		UserID  string `json:"userId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	ctx := h.ctx(r)
	if _, ok, err := h.memberRole(ctx, req.GroupID, req.UserID); err != nil {
		writeErr(w, err)
		return
	} else if !ok {
		util.WriteError(w, 403, "非群成员")
		return
	}
	existing, err := db.QueryRowToStruct[db.InviteLink](ctx, h.deps.DB,
		`SELECT * FROM "InviteLink" WHERE "groupId"=$1 AND "name"='group_qrcode' AND "isRevoked"=false ORDER BY "createdAt" DESC LIMIT 1`,
		req.GroupID)
	if err != nil && !db.IsNotFound(err) {
		writeErr(w, err)
		return
	}
	var hash string
	if err == nil {
		// 旧版 7 天二维码自动迁移为长期有效
		if existing.ExpireAt != nil || existing.MaxUses != 0 {
			if _, err := h.deps.DB.Exec(ctx,
				`UPDATE "InviteLink" SET "expireAt"=NULL, "maxUses"=0 WHERE "id"=$1`, existing.Id); err != nil {
				writeErr(w, err)
				return
			}
			existing.ExpireAt = nil
			existing.MaxUses = 0
		}
		hash = existing.Hash
	} else {
		hash = generateInviteHash()
		if _, err := h.deps.DB.Exec(ctx,
			`INSERT INTO "InviteLink"("id","hash","groupId","creatorId","name","expireAt","maxUses","usedCount","isRevoked","createdAt")
			 VALUES($1,$2,$3,$4,'group_qrcode',NULL,0,0,false,NOW())`,
			util.NewID(), hash, req.GroupID, req.UserID); err != nil {
			writeErr(w, err)
			return
		}
	}
	util.WriteJSON(w, 200, map[string]any{
		"ok": true,
		"inviteLink": map[string]any{
			"hash":     hash,
			"url":      "/im/+" + hash,
			"fullUrl":  publicUrl("/im/+" + hash),
			"expireAt": nil,
		},
	})
}

// POST /api/group/invite-members 批量邀请好友入群（发送邀请请求）
func (h *Handler) inviteMembers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID   string   `json:"groupId"`
		InviterID string   `json:"inviterId"`
		MemberIDs []string `json:"memberIds"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.InviterID == "" || len(req.MemberIDs) == 0 {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	if len(req.MemberIDs) > 50 {
		util.WriteError(w, 400, "单次最多邀请50人")
		return
	}
	ctx := h.ctx(r)
	if _, ok, err := h.memberRole(ctx, req.GroupID, req.InviterID); err != nil {
		writeErr(w, err)
		return
	} else if !ok {
		util.WriteError(w, 403, "非群成员，无权邀请")
		return
	}
	var groupName string
	if err := h.deps.DB.Pool.QueryRow(ctx,
		`SELECT "name" FROM "Group" WHERE "id"=$1`, req.GroupID).Scan(&groupName); err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "群组不存在")
		} else {
			writeErr(w, err)
		}
		return
	}
	// 过滤已是群成员的用户
	existingMembers, err := db.QueryToStructs[db.GroupMember](ctx, h.deps.DB,
		`SELECT "userId" FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=ANY($2)`, req.GroupID, req.MemberIDs)
	if err != nil {
		writeErr(w, err)
		return
	}
	existingSet := make(map[string]struct{})
	for _, m := range existingMembers {
		existingSet[m.UserId] = struct{}{}
	}
	var newMemberIDs []string
	for _, id := range req.MemberIDs {
		if _, ok := existingSet[id]; !ok {
			newMemberIDs = append(newMemberIDs, id)
		}
	}
	if len(newMemberIDs) == 0 {
		util.WriteJSON(w, 200, map[string]any{
			"ok": true, "invited": 0, "alreadyMembers": len(req.MemberIDs), "message": "所选好友已全部在群中",
		})
		return
	}
	// 验证用户存在
	validUsers, err := db.QueryToStructs[db.User](ctx, h.deps.DB,
		`SELECT "id" FROM "User" WHERE "id"=ANY($1)`, newMemberIDs)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(validUsers) == 0 {
		util.WriteError(w, 400, "没有有效的用户可邀请")
		return
	}
	validIDs := make([]string, 0, len(validUsers))
	for _, u := range validUsers {
		validIDs = append(validIDs, u.Id)
	}
	// 过滤已有待处理邀请的用户
	pendingInvites, err := db.QueryToStructs[db.GroupInvite](ctx, h.deps.DB,
		`SELECT "inviteeId" FROM "GroupInvite" WHERE "groupId"=$1 AND "inviteeId"=ANY($2) AND "status"='pending'`,
		req.GroupID, validIDs)
	if err != nil {
		writeErr(w, err)
		return
	}
	pendingSet := make(map[string]struct{})
	for _, iv := range pendingInvites {
		pendingSet[iv.InviteeId] = struct{}{}
	}
	var toInvite []string
	for _, id := range validIDs {
		if _, ok := pendingSet[id]; !ok {
			toInvite = append(toInvite, id)
		}
	}
	if len(toInvite) > 0 {
		var sb strings.Builder
		sb.WriteString(`INSERT INTO "GroupInvite"("id","groupId","inviterId","inviteeId","message","status","createdAt","updatedAt") VALUES `)
		args := make([]any, 0, len(toInvite)*8)
		for i, uid := range toInvite {
			base := i*8 + 1
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("( $" + itoa(base) + ",$" + itoa(base+1) + ",$" + itoa(base+2) + ",$" + itoa(base+3) +
				",$" + itoa(base+4) + ",$" + itoa(base+5) + ",NOW(),NOW())")
			args = append(args, util.NewID(), req.GroupID, req.InviterID, uid,
				"邀请你加入群聊「"+groupName+"」", "pending")
		}
		// skipDuplicates：唯一键 (groupId, inviteeId, status)
		sb.WriteString(` ON CONFLICT("groupId","inviteeId","status") DO NOTHING`)
		if _, err := h.deps.DB.Exec(ctx, sb.String(), args...); err != nil {
			writeErr(w, err)
			return
		}
	}
	log.Printf("[GroupMsg] 批量发送入群邀请: groupId=%s inviter=%s invited=%d alreadyPending=%d",
		req.GroupID, req.InviterID, len(toInvite), len(pendingSet))
	util.WriteJSON(w, 200, map[string]any{
		"ok": true, "invited": len(toInvite), "alreadyPending": len(pendingSet),
		"alreadyMembers": len(existingSet), "groupName": groupName,
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

// GET /api/group/invites 获取当前用户收到的群邀请列表
func (h *Handler) invites(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	userID := q.Get("userId")
	if userID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	typ := q.Get("type")
	var where string
	var args []any
	switch typ {
	case "received":
		where = `"inviteeId"=$1`
		args = []any{userID}
	case "sent":
		where = `"inviterId"=$1`
		args = []any{userID}
	default:
		where = `("inviteeId"=$1 OR "inviterId"=$1)`
		args = []any{userID}
	}
	ctx := h.ctx(r)
	invites, err := db.QueryToStructs[db.GroupInvite](ctx, h.deps.DB,
		`SELECT * FROM "GroupInvite" WHERE `+where+` ORDER BY "createdAt" DESC LIMIT 100`, args...)
	if err != nil {
		log.Printf("[GroupMsg] 获取群邀请列表失败: %v", err)
		util.WriteError(w, 500, "获取失败")
		return
	}
	// 关联群信息
	groupIDs := make([]string, 0, len(invites))
	for _, iv := range invites {
		groupIDs = append(groupIDs, iv.GroupId)
	}
	groupMap := make(map[string]db.Group)
	if len(groupIDs) > 0 {
		groups, err := db.QueryToStructs[db.Group](ctx, h.deps.DB,
			`SELECT "id","name","avatar","memberCount" FROM "Group" WHERE "id"=ANY($1)`, groupIDs)
		if err == nil {
			for _, g := range groups {
				groupMap[g.Id] = g
			}
		}
	}
	userIDs := make([]string, 0, len(invites)*2)
	for _, iv := range invites {
		userIDs = append(userIDs, iv.InviterId, iv.InviteeId)
	}
	userMap := h.userBriefMap(ctx, userIDs)
	out := make([]map[string]any, 0, len(invites))
	for _, iv := range invites {
		g := groupMap[iv.GroupId]
		inviter := userMap[iv.InviterId]
		invitee := userMap[iv.InviteeId]
		out = append(out, map[string]any{
			"id": iv.Id, "groupId": iv.GroupId,
			"groupName":        g.Name,
			"groupAvatar":      avatarToProxy(util.StrVal(g.Avatar)),
			"groupMemberCount": g.MemberCount,
			"inviterId":        iv.InviterId,
			"inviterName":      displayName(inviter, iv.InviterId),
			"inviterAvatar":    avatarToProxy(util.StrVal(inviter.Avatar)),
			"inviteeId":        iv.InviteeId,
			"inviteeName":      displayName(invitee, iv.InviteeId),
			"inviteeAvatar":    avatarToProxy(util.StrVal(invitee.Avatar)),
			"message":          util.StrVal(iv.Message),
			"status":           iv.Status,
			"timestamp":        iv.CreatedAt.UnixMilli(),
			"isIncoming":       iv.InviteeId == userID,
		})
	}
	util.WriteJSON(w, 200, map[string]any{"invites": out})
}

// POST /api/group/invite-accept/{inviteId} 同意群邀请
func (h *Handler) inviteAccept(w http.ResponseWriter, r *http.Request) {
	inviteID := r.PathValue("inviteId")
	var req struct {
		UserID string `json:"userId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	ctx := h.ctx(r)
	iv, err := db.QueryRowToStruct[db.GroupInvite](ctx, h.deps.DB,
		`SELECT * FROM "GroupInvite" WHERE "id"=$1`, inviteID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "邀请不存在")
		} else {
			writeErr(w, err)
		}
		return
	}
	if iv.InviteeId != req.UserID {
		util.WriteError(w, 403, "无权操作此邀请")
		return
	}
	if iv.Status != "pending" {
		msg := "邀请已拒绝"
		if iv.Status == "accepted" {
			msg = "邀请已接受"
		}
		util.WriteError(w, 400, msg)
		return
	}
	if _, ok, err := h.memberRole(ctx, iv.GroupId, req.UserID); err != nil {
		writeErr(w, err)
		return
	} else if ok {
		var groupName string
		_ = h.deps.DB.Pool.QueryRow(ctx, `SELECT "name" FROM "Group" WHERE "id"=$1`, iv.GroupId).Scan(&groupName)
		if _, err := h.deps.DB.Exec(ctx,
			`UPDATE "GroupInvite" SET "status"='accepted', "updatedAt"=NOW() WHERE "id"=$1`, inviteID); err != nil {
			writeErr(w, err)
			return
		}
		util.WriteJSON(w, 200, map[string]any{
			"ok": true, "message": "你已经是群成员了", "groupId": iv.GroupId, "groupName": groupName,
		})
		return
	}
	var memberCount, maxMembers int32
	var lastMsgSeq int64
	var groupName string
	var groupAvatar *string
	if err := h.deps.DB.Pool.QueryRow(ctx,
		`SELECT "memberCount","maxMembers","lastMsgSeq","name","avatar" FROM "Group" WHERE "id"=$1`, iv.GroupId).
		Scan(&memberCount, &maxMembers, &lastMsgSeq, &groupName, &groupAvatar); err != nil {
		writeErr(w, err)
		return
	}
	if memberCount >= maxMembers {
		util.WriteError(w, 403, "群组已满员")
		return
	}
	tx, err := h.deps.DB.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE "GroupInvite" SET "status"='accepted', "updatedAt"=NOW() WHERE "id"=$1`, inviteID); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO "GroupMember"("id","groupId","userId","role","lastAckSeq","joinTime","createdAt","updatedAt")
		 VALUES($1,$2,$3,'member',$4,NOW(),NOW(),NOW())`,
		util.NewID(), iv.GroupId, req.UserID, lastMsgSeq); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE "Group" SET "memberCount"="memberCount"+1, "updatedAt"=NOW() WHERE "id"=$1`, iv.GroupId); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, err)
		return
	}
	u, _ := db.QueryRowToStruct[db.User](ctx, h.deps.DB,
		`SELECT "nickname","username" FROM "User" WHERE "id"=$1`, req.UserID)
	userName := req.UserID
	if u != nil {
		userName = displayName(*u, req.UserID)
	}
	h.sysMessage(iv.GroupId, userName+" 通过邀请加入了群聊")
	log.Printf("[GroupMsg] 用户接受群邀请: inviteId=%s userId=%s groupId=%s", inviteID, req.UserID, iv.GroupId)
	util.WriteJSON(w, 200, map[string]any{
		"ok": true, "message": "已加入群聊", "groupId": iv.GroupId,
		"groupName": groupName, "groupAvatar": avatarToProxy(util.StrVal(groupAvatar)),
	})
}

// POST /api/group/invite-reject/{inviteId} 拒绝群邀请
func (h *Handler) inviteReject(w http.ResponseWriter, r *http.Request) {
	inviteID := r.PathValue("inviteId")
	var req struct {
		UserID string `json:"userId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	ctx := h.ctx(r)
	iv, err := db.QueryRowToStruct[db.GroupInvite](ctx, h.deps.DB,
		`SELECT "id","inviteeId","status" FROM "GroupInvite" WHERE "id"=$1`, inviteID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "邀请不存在")
		} else {
			writeErr(w, err)
		}
		return
	}
	if iv.InviteeId != req.UserID {
		util.WriteError(w, 403, "无权操作此邀请")
		return
	}
	if iv.Status != "pending" {
		msg := "邀请已拒绝"
		if iv.Status == "accepted" {
			msg = "邀请已接受"
		}
		util.WriteError(w, 400, msg)
		return
	}
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "GroupInvite" SET "status"='rejected', "updatedAt"=NOW() WHERE "id"=$1`, inviteID); err != nil {
		writeErr(w, err)
		return
	}
	log.Printf("[GroupMsg] 用户拒绝群邀请: inviteId=%s userId=%s", inviteID, req.UserID)
	util.WriteJSON(w, 200, map[string]any{"ok": true, "message": "已拒绝群邀请"})
}
