package channel

// 频道服务 — Telegram 风格单向广播频道（channel.ts 移植）
// 挂载在 /api/channel，共 16 个路由。
//
// S4 授权修复（照搬 TS）：
//   - POST /post：非成员 403，未订阅/非管理员不可发布
//   - GET /messages：userAuth + pullGroupMessages 内部成员校验（非成员 403）
//   - GET /subscribers：仅订阅者可查看
// 其余鉴权与 TS 一致：需要登录的包 d.Auth.UserAuth，/info 用 OptionalAuth，
// /resolve、/search 公开。

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/group"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

type Handler struct {
	d      *handler.Deps
	engine *group.Engine
}

// RegisterRoutes 注册频道路由。频道即特殊群组，消息收发复用群引擎（与 TS channel.ts
// 直接 import group-message.ts 一致），避免两套 seq/扇出逻辑分叉。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps, engine *group.Engine) {
	h := &Handler{d: d, engine: engine}
	auth := d.Auth.UserAuth
	mux.Handle("POST /api/channel/create", auth(http.HandlerFunc(h.create)))
	mux.Handle("GET /api/channel/info", d.Auth.OptionalAuth(http.HandlerFunc(h.info)))
	mux.Handle("GET /api/channel/resolve", http.HandlerFunc(h.resolve))
	mux.Handle("POST /api/channel/subscribe", auth(http.HandlerFunc(h.subscribe)))
	mux.Handle("POST /api/channel/unsubscribe", auth(http.HandlerFunc(h.unsubscribe)))
	mux.Handle("POST /api/channel/post", auth(http.HandlerFunc(h.post)))
	mux.Handle("GET /api/channel/messages", auth(http.HandlerFunc(h.messages)))
	mux.Handle("GET /api/channel/my", auth(http.HandlerFunc(h.my)))
	mux.Handle("GET /api/channel/search", http.HandlerFunc(h.search))
	mux.Handle("GET /api/channel/subscribers", auth(http.HandlerFunc(h.subscribers)))
	mux.Handle("POST /api/channel/admin/add", auth(http.HandlerFunc(h.adminAdd)))
	mux.Handle("POST /api/channel/admin/remove", auth(http.HandlerFunc(h.adminRemove)))
	mux.Handle("PUT /api/channel/update", auth(http.HandlerFunc(h.update)))
	mux.Handle("DELETE /api/channel/delete", auth(http.HandlerFunc(h.deleteChannel)))
	mux.Handle("POST /api/channel/ack", auth(http.HandlerFunc(h.ack)))
	mux.Handle("GET /api/channel/unread", auth(http.HandlerFunc(h.unread)))
}

// memberRole 查询用户在频道中的成员角色；不存在返回 ("", false, nil)。
func (h *Handler) memberRole(r *http.Request, channelID, userID string) (string, bool, error) {
	m, err := db.QueryRowToStruct[db.GroupMember](r.Context(), h.d.DB,
		`SELECT "role" FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`, channelID, userID)
	if err != nil {
		if db.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return m.Role, true, nil
}

// channelTypeOf 查询频道类型；不存在返回 ("", false, nil)。
func (h *Handler) channelTypeOf(r *http.Request, channelID string) (string, bool, error) {
	g, err := db.QueryRowToStruct[db.GroupTypeBrief](r.Context(), h.d.DB,
		`SELECT "type" FROM "Group" WHERE "id"=$1`, channelID)
	if err != nil {
		if db.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return g.Type, true, nil
}

// ============ 频道创建 ============

var usernameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{4,31}$`)

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		Name        string  `json:"name"`
		Username    *string `json:"username"`
		Description *string `json:"description"`
		IsPublic    *bool   `json:"isPublic"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		util.WriteError(w, 400, "缺少必要参数：name")
		return
	}
	username := strVal(req.Username)
	if username != "" {
		if !usernameRe.MatchString(username) {
			util.WriteError(w, 400, "频道用户名必须以字母开头，5-32位字母数字下划线")
			return
		}
		var exists bool
		_ = h.d.DB.Pool.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM "User" WHERE "username"=$1) OR EXISTS(SELECT 1 FROM "Group" WHERE "username"=$1)`,
			username).Scan(&exists)
		if exists {
			util.WriteError(w, 409, "该用户名已被占用")
			return
		}
	}

	dialogID := generateChannelDialogID()
	isPublic := true
	if req.IsPublic != nil {
		isPublic = *req.IsPublic
	}
	var usernameVal, announcementVal *string
	if username != "" {
		usernameVal = &username
	}
	if d := strVal(req.Description); d != "" {
		announcementVal = &d
	}
	channelID := util.NewID()
	now := time.Now()
	if _, err := h.d.DB.Exec(r.Context(),
		`INSERT INTO "Group"("id","dialogId","name","username","ownerId","type","isPublic","maxMembers","memberCount","announcement","createdAt","updatedAt")
		 VALUES($1,$2,$3,$4,$5,'channel',$6,0,1,$7,$8,$8)`,
		channelID, dialogID, req.Name, usernameVal, u.Id, isPublic, announcementVal, now); err != nil {
		util.WriteError(w, 500, "创建频道失败")
		return
	}
	if _, err := h.d.DB.Exec(r.Context(),
		`INSERT INTO "GroupMember"("id","groupId","userId","role","lastAckSeq","joinTime","createdAt","updatedAt")
		 VALUES($1,$2,$3,'owner',0,$4,$4,$4)`,
		util.NewID(), channelID, u.Id, now); err != nil {
		util.WriteError(w, 500, "创建频道失败")
		return
	}
	// Redis 在线集合占位（与 TS 初始化 groupOnlineMembers 一致，跨节点可见）
	_ = h.d.Redis.SAdd(r.Context(), "group:online:"+channelID)

	var pubURL *string
	if username != "" {
		s := publicURL("/im/" + username)
		pubURL = &s
	}
	util.WriteJSON(w, 200, map[string]any{
		"ok": true,
		"channel": map[string]any{
			"id": channelID, "dialogId": dialogID, "name": req.Name,
			"username": strVal(usernameVal), "type": "channel", "isPublic": isPublic,
			"memberCount": int32(1), "publicUrl": pubURL, "createdAt": now,
		},
	})
}

// ============ 频道信息 ============

func (h *Handler) info(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channelId")
	if channelID == "" {
		util.WriteError(w, 400, "缺少 channelId")
		return
	}
	info, err := getGroupInfo(r.Context(), h.d, channelID)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if info == nil {
		util.WriteError(w, 404, "频道不存在")
		return
	}
	// 判断请求者是否是订阅者：优先用登录身份，匿名访问时才看 userId 参数
	userID := ""
	if u := middleware.UserFrom(r); u != nil {
		userID = u.Id
	} else {
		userID = r.URL.Query().Get("userId")
	}
	var (
		isSubscribed bool
		memberRole   any
	)
	if userID != "" {
		if role, ok, err := h.memberRole(r, channelID, userID); err == nil && ok {
			isSubscribed = true
			memberRole = role
		}
	}
	info["isSubscribed"] = isSubscribed
	info["memberRole"] = memberRole
	info["canPost"] = memberRole == "owner" || memberRole == "admin"
	info["avatar"] = avatarToProxy(info["avatar"].(string))
	util.WriteJSON(w, 200, info)
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	if username == "" {
		util.WriteError(w, 400, "缺少 username")
		return
	}
	g, err := db.QueryRowToStruct[db.GroupChannelBrief](r.Context(), h.d.DB,
		`SELECT "id","dialogId","name","username","avatar","type","isPublic","memberCount","announcement","createdAt"
		 FROM "Group" WHERE "username"=$1`, username)
	if err != nil || g.Type != "channel" {
		if err != nil && !db.IsNotFound(err) {
			util.WriteError(w, 500, err.Error())
			return
		}
		util.WriteError(w, 404, "频道不存在")
		return
	}
	util.WriteJSON(w, 200, map[string]any{
		"id": g.Id, "dialogId": strVal(g.DialogId), "name": g.Name,
		"username": strVal(g.Username), "avatar": avatarToProxy(strVal(g.Avatar)),
		"type": g.Type, "isPublic": g.IsPublic, "memberCount": g.MemberCount,
		"announcement": strVal(g.Announcement), "createdAt": g.CreatedAt,
		"publicUrl": publicURL("/im/" + strVal(g.Username)),
	})
}

// ============ 频道订阅 ============

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		ChannelID string `json:"channelId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.ChannelID == "" {
		util.WriteError(w, 400, "缺少必要参数：channelId")
		return
	}
	g, err := db.QueryRowToStruct[db.GroupJoinBrief](r.Context(), h.d.DB,
		`SELECT "id","type","name","memberCount","maxMembers" FROM "Group" WHERE "id"=$1`, req.ChannelID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "频道不存在")
			return
		}
		util.WriteError(w, 500, err.Error())
		return
	}
	if g.Type != "channel" {
		util.WriteError(w, 400, "该群组不是频道")
		return
	}
	if _, ok, err := h.memberRole(r, req.ChannelID, u.Id); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	} else if ok {
		util.WriteJSON(w, 200, map[string]any{
			"ok": true, "alreadySubscribed": true,
			"channelId": req.ChannelID, "channelName": g.Name,
		})
		return
	}
	if err := joinGroup(r.Context(), h.d, req.ChannelID, u.Id); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	util.WriteJSON(w, 200, map[string]any{
		"ok": true, "channelId": req.ChannelID, "channelName": g.Name,
		"memberCount": g.MemberCount + 1,
	})
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		ChannelID string `json:"channelId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.ChannelID == "" {
		util.WriteError(w, 400, "缺少必要参数：channelId")
		return
	}
	role, ok, err := h.memberRole(r, req.ChannelID, u.Id)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if !ok {
		util.WriteError(w, 404, "未订阅此频道")
		return
	}
	if role == "owner" {
		util.WriteError(w, 400, "频道所有者不能取消订阅，请先转让或删除频道")
		return
	}
	tx, err := h.d.DB.Pool.Begin(r.Context())
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(),
		`DELETE FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`, req.ChannelID, u.Id); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE "Group" SET "memberCount"="memberCount"-1 WHERE "id"=$1`, req.ChannelID); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	// 从在线集合移除
	_ = h.d.Redis.SRem(r.Context(), "group:online:"+req.ChannelID, u.Id)
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// ============ 频道消息 ============

func (h *Handler) post(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		ChannelID  string  `json:"channelId"`
		SenderName *string `json:"senderName"`
		MsgType    *string `json:"msgType"`
		Content    string  `json:"content"`
		ReplyToID  *string `json:"replyToId"`
		Extra      any     `json:"extra"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.ChannelID == "" || req.Content == "" {
		util.WriteError(w, 400, "缺少必要参数：channelId, content")
		return
	}
	// ★ S4：权限验证，仅 owner/admin 可发布
	role, ok, err := h.memberRole(r, req.ChannelID, u.Id)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if !ok {
		util.WriteError(w, 403, "未订阅此频道")
		return
	}
	if role != "owner" && role != "admin" {
		util.WriteError(w, 403, "仅频道管理员可发布消息")
		return
	}
	if t, found, err := h.channelTypeOf(r, req.ChannelID); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	} else if !found || t != "channel" {
		util.WriteError(w, 400, "该群组不是频道")
		return
	}
	msgType := strVal(req.MsgType)
	if msgType == "" {
		msgType = "text"
	}
	replyToID := ""
	if req.ReplyToID != nil {
		replyToID = *req.ReplyToID
	}
	seq, ts, err := h.engine.SendGroupMessage(r.Context(), group.SendParams{
		GroupID: req.ChannelID, SenderID: u.Id, SenderName: strVal(req.SenderName),
		MsgType: msgType, Content: req.Content, ReplyToID: replyToID, Extra: req.Extra,
	})
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "seq": seq, "timestamp": ts})
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	q := r.URL.Query()
	channelID := q.Get("channelId")
	if channelID == "" {
		util.WriteError(w, 400, "缺少 channelId")
		return
	}
	if t, found, err := h.channelTypeOf(r, channelID); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	} else if !found || t != "channel" {
		util.WriteError(w, 400, "该群组不是频道")
		return
	}
	var afterSeq, beforeSeq *int64
	if s := q.Get("afterSeq"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			afterSeq = &v
		}
	}
	if s := q.Get("beforeSeq"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			beforeSeq = &v
		}
	}
	limit := 50
	if s := q.Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			limit = v
		}
	}
	// ★ S4：pullGroupMessages 内部校验成员身份，非成员返回 403
	result, err := h.engine.PullGroupMessages(r.Context(), group.PullParams{
		GroupID: channelID, UserID: u.Id, AfterSeq: afterSeq, BeforeSeq: beforeSeq, Limit: limit,
	})
	if err != nil {
		if err == group.ErrNotGroupMember {
			util.WriteError(w, 403, err.Error())
			return
		}
		util.WriteError(w, 500, err.Error())
		return
	}
	util.WriteJSON(w, 200, result)
}

// ============ 我的频道列表 ============

func (h *Handler) my(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	memberships, err := db.QueryToStructs[db.GroupMember](r.Context(), h.d.DB,
		`SELECT m.* FROM "GroupMember" m JOIN "Group" g ON g."id"=m."groupId"
		 WHERE m."userId"=$1 AND g."type"='channel' ORDER BY m."joinTime" DESC`, u.Id)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	channels := make([]map[string]any, 0, len(memberships))
	for _, m := range memberships {
		g, err := db.QueryRowToStruct[db.Group](r.Context(), h.d.DB,
			`SELECT "id","dialogId","name","username","avatar","type","isPublic","memberCount","announcement","lastMsgSeq","lastMsgTime"
			 FROM "Group" WHERE "id"=$1`, m.GroupId)
		if err != nil {
			continue
		}
		channels = append(channels, map[string]any{
			"id": g.Id, "dialogId": strVal(g.DialogId), "name": g.Name,
			"username": strVal(g.Username), "avatar": avatarToProxy(strVal(g.Avatar)),
			"type": g.Type, "isPublic": g.IsPublic, "memberCount": g.MemberCount,
			"announcement": strVal(g.Announcement), "myRole": m.Role,
			"lastMsgSeq":  strconv.FormatInt(g.LastMsgSeq, 10),
			"lastMsgTime": g.LastMsgTime, "joinedAt": m.JoinTime,
		})
	}
	util.WriteJSON(w, 200, map[string]any{"channels": channels})
}

// ============ 搜索公开频道 ============

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	keyword := r.URL.Query().Get("q")
	if keyword == "" {
		util.WriteError(w, 400, "请输入搜索关键词")
		return
	}
	groups, err := db.QueryToStructs[db.Group](r.Context(), h.d.DB,
		`SELECT "id","dialogId","name","username","avatar","type","isPublic","memberCount","announcement"
		 FROM "Group"
		 WHERE "type"='channel' AND "isPublic"=true
		   AND ("name" ILIKE '%'||$1||'%' OR "username" ILIKE '%'||$1||'%' OR "announcement" ILIKE '%'||$1||'%')
		 ORDER BY "memberCount" DESC LIMIT 20`, keyword)
	if err != nil {
		util.WriteError(w, 500, "搜索失败")
		return
	}
	channels := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		channels = append(channels, map[string]any{
			"id": g.Id, "dialogId": strVal(g.DialogId), "name": g.Name,
			"username": strVal(g.Username), "avatar": avatarToProxy(strVal(g.Avatar)),
			"type": g.Type, "isPublic": g.IsPublic, "memberCount": g.MemberCount,
			"announcement": strVal(g.Announcement),
		})
	}
	util.WriteJSON(w, 200, map[string]any{"channels": channels})
}

// ============ 频道订阅者列表 ============

func (h *Handler) subscribers(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	q := r.URL.Query()
	channelID := q.Get("channelId")
	if channelID == "" {
		util.WriteError(w, 400, "缺少 channelId")
		return
	}
	// ★ S4：仅订阅者可查看订阅者列表
	if _, ok, err := h.memberRole(r, channelID, u.Id); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	} else if !ok {
		util.WriteError(w, 403, "未订阅此频道")
		return
	}
	page, pageSize := 1, 100
	if s := q.Get("page"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			page = v
		}
	}
	if s := q.Get("pageSize"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			pageSize = v
		}
	}
	result, err := getGroupMembers(r.Context(), h.d, channelID, page, pageSize)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	util.WriteJSON(w, 200, result)
}

// ============ 频道管理 ============

func (h *Handler) adminAdd(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		ChannelID    string `json:"channelId"`
		TargetUserID string `json:"targetUserId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.ChannelID == "" || req.TargetUserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	role, ok, err := h.memberRole(r, req.ChannelID, u.Id)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if !ok || role != "owner" {
		util.WriteError(w, 403, "仅频道所有者可添加管理员")
		return
	}
	targetRole, ok, err := h.memberRole(r, req.ChannelID, req.TargetUserID)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if !ok {
		util.WriteError(w, 404, "该用户未订阅此频道")
		return
	}
	if targetRole == "owner" {
		util.WriteError(w, 400, "频道所有者已是最高权限")
		return
	}
	if targetRole == "admin" {
		util.WriteJSON(w, 200, map[string]any{"ok": true, "message": "该用户已是管理员"})
		return
	}
	if _, err := h.d.DB.Exec(r.Context(),
		`UPDATE "GroupMember" SET "role"='admin' WHERE "groupId"=$1 AND "userId"=$2`,
		req.ChannelID, req.TargetUserID); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	// 发送系统消息（异步，与 TS 一致）
	go func() {
		_, _, _ = h.engine.SendGroupMessage(r.Context(), group.SendParams{
			GroupID: req.ChannelID, SenderID: "system", SenderName: "系统",
			MsgType: "system", Content: req.TargetUserID + " 已被提升为频道管理员",
		})
	}()
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

func (h *Handler) adminRemove(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		ChannelID    string `json:"channelId"`
		TargetUserID string `json:"targetUserId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.ChannelID == "" || req.TargetUserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	role, ok, err := h.memberRole(r, req.ChannelID, u.Id)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if !ok || role != "owner" {
		util.WriteError(w, 403, "仅频道所有者可移除管理员")
		return
	}
	targetRole, ok, err := h.memberRole(r, req.ChannelID, req.TargetUserID)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if !ok {
		util.WriteError(w, 404, "该用户未订阅此频道")
		return
	}
	if targetRole != "admin" {
		util.WriteJSON(w, 200, map[string]any{"ok": true, "message": "该用户不是管理员"})
		return
	}
	if _, err := h.d.DB.Exec(r.Context(),
		`UPDATE "GroupMember" SET "role"='member' WHERE "groupId"=$1 AND "userId"=$2`,
		req.ChannelID, req.TargetUserID); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		ChannelID    string  `json:"channelId"`
		Name         *string `json:"name"`
		Announcement *string `json:"announcement"`
		Avatar       *string `json:"avatar"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.ChannelID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	role, ok, err := h.memberRole(r, req.ChannelID, u.Id)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if !ok || (role != "owner" && role != "admin") {
		util.WriteError(w, 403, "仅频道管理员可修改频道信息")
		return
	}
	setClauses := []string{}
	args := []any{}
	changes := []string{}
	addSet := func(col string, v *string) {
		args = append(args, v)
		setClauses = append(setClauses, `"`+col+`"=$`+strconv.Itoa(len(args)))
	}
	if req.Name != nil {
		t := strings.TrimSpace(*req.Name)
		if t == "" {
			util.WriteError(w, 400, "频道名称不能为空")
			return
		}
		if utf8.RuneCountInString(t) > 30 {
			util.WriteError(w, 400, "频道名称不能超过30个字符")
			return
		}
		v := t
		addSet("name", &v)
		changes = append(changes, "频道名称已更新")
	}
	if req.Announcement != nil {
		var v *string
		if s := *req.Announcement; s != "" {
			v = &s
		}
		addSet("announcement", v)
		changes = append(changes, "频道简介已更新")
	}
	if req.Avatar != nil {
		var v *string
		if s := *req.Avatar; s != "" {
			v = &s
		}
		addSet("avatar", v)
		changes = append(changes, "频道头像已更新")
	}
	if len(setClauses) == 0 {
		util.WriteError(w, 400, "没有需要更新的字段")
		return
	}
	args = append(args, req.ChannelID)
	setSQL := ""
	for i, c := range setClauses {
		if i > 0 {
			setSQL += ", "
		}
		setSQL += c
	}
	if _, err := h.d.DB.Exec(r.Context(),
		`UPDATE "Group" SET `+setSQL+` WHERE "id"=$`+strconv.Itoa(len(args)), args...); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if len(changes) > 0 {
		content := ""
		for i, c := range changes {
			if i > 0 {
				content += "，"
			}
			content += c
		}
		go func() {
			_, _, _ = h.engine.SendGroupMessage(r.Context(), group.SendParams{
				GroupID: req.ChannelID, SenderID: "system", SenderName: "系统",
				MsgType: "system", Content: content,
			})
		}()
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "changes": changes})
}

func (h *Handler) deleteChannel(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		ChannelID string `json:"channelId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.ChannelID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	role, ok, err := h.memberRole(r, req.ChannelID, u.Id)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	if !ok || role != "owner" {
		util.WriteError(w, 403, "仅频道所有者可删除频道")
		return
	}
	if _, err := h.d.DB.Exec(r.Context(), `DELETE FROM "Group" WHERE "id"=$1`, req.ChannelID); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// ============ 已读回执 ============

func (h *Handler) ack(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		ChannelID  string `json:"channelId"`
		LastAckSeq *int64 `json:"lastAckSeq"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.ChannelID == "" || req.LastAckSeq == nil {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	if err := h.engine.AckGroupMessages(r.Context(), group.AckParams{
		GroupID: req.ChannelID, UserID: u.Id, LastAckSeq: *req.LastAckSeq,
	}); err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// ============ 频道未读数 ============

func (h *Handler) unread(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	allUnread, err := h.engine.GetGroupUnreadCounts(r.Context(), u.Id)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}
	// 与 TS 一致：简化返回所有未读数，前端过滤频道类型
	util.WriteJSON(w, 200, allUnread)
}
