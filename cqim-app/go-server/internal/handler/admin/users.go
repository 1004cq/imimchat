package admin

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============================================================
// 用户管理
//
// 注意：pgx 的 RowToStructByName 为严格模式（struct 字段必须都有对应列），
// 因此每个查询都使用字段与 SELECT 严格对应的专用行 struct，而非 db.User。
// ============================================================

// userAdminRow 用户管理通用行：与 userSelectCols 严格对应。
type userAdminRow struct {
	Id          string     `db:"id"`
	Username    string     `db:"username"`
	Nickname    *string    `db:"nickname"`
	Email       *string    `db:"email"`
	Phone       *string    `db:"phone"`
	Avatar      *string    `db:"avatar"`
	Bio         *string    `db:"bio"`
	IsBanned    bool       `db:"isBanned"`
	BanReason   *string    `db:"banReason"`
	Role        string     `db:"role"`
	CreatedAt   time.Time  `db:"createdAt"`
	UpdatedAt   time.Time  `db:"updatedAt"`
	LastLoginAt *time.Time `db:"lastLoginAt"`
	LastLoginIp *string    `db:"lastLoginIp"`
}

// userSelectCols 用户管理通用查询字段。
const userSelectCols = `"id","username","nickname","email","phone","avatar","bio","isBanned","banReason","role","createdAt","updatedAt","lastLoginAt","lastLoginIp"`

// userListRow 列表/详情行：通用字段 + postsCount（单查询内联统计，避免 N+1）。
type userListRow struct {
	userAdminRow
	PostsCount int64 `db:"postsCount"`
}

// userListSelect 列表/详情 SELECT 片段（u. 前缀 + postsCount 内联统计）。
var userListSelect = strings.ReplaceAll(userSelectCols, ",", `,u."`) +
	`,(SELECT COUNT(*) FROM "Moment" m WHERE m."userId"=u."id") AS "postsCount"`

func userListToMap(u *userListRow) map[string]any {
	return map[string]any{
		"id": u.Id, "username": u.Username, "nickname": u.Nickname, "email": u.Email,
		"phone": u.Phone, "avatar": u.Avatar, "bio": u.Bio, "isBanned": u.IsBanned,
		"banReason": u.BanReason, "role": u.Role, "createdAt": u.CreatedAt,
		"updatedAt": u.UpdatedAt, "lastLoginAt": u.LastLoginAt, "lastLoginIp": u.LastLoginIp,
		"postsCount": u.PostsCount,
	}
}

// userCreateRow 创建用户响应行（与 TS select 严格一致：无 banReason/updatedAt/lastLogin*）。
type userCreateRow struct {
	Id        string    `db:"id"`
	Username  string    `db:"username"`
	Nickname  *string   `db:"nickname"`
	Email     *string   `db:"email"`
	Phone     *string   `db:"phone"`
	Avatar    *string   `db:"avatar"`
	Bio       *string   `db:"bio"`
	IsBanned  bool      `db:"isBanned"`
	Role      string    `db:"role"`
	CreatedAt time.Time `db:"createdAt"`
}

const userCreateCols = `"id","username","nickname","email","phone","avatar","bio","isBanned","role","createdAt"`

// userUpdateRow 编辑用户响应行（与 TS select 严格一致：无 lastLoginIp/postsCount）。
type userUpdateRow struct {
	Id          string     `db:"id"`
	Username    string     `db:"username"`
	Nickname    *string    `db:"nickname"`
	Email       *string    `db:"email"`
	Phone       *string    `db:"phone"`
	Avatar      *string    `db:"avatar"`
	Bio         *string    `db:"bio"`
	IsBanned    bool       `db:"isBanned"`
	BanReason   *string    `db:"banReason"`
	Role        string     `db:"role"`
	CreatedAt   time.Time  `db:"createdAt"`
	UpdatedAt   time.Time  `db:"updatedAt"`
	LastLoginAt *time.Time `db:"lastLoginAt"`
}

const userUpdateCols = `"id","username","nickname","email","phone","avatar","bio","isBanned","banReason","role","createdAt","updatedAt","lastLoginAt"`

func userRowToMap(u *userUpdateRow) map[string]any {
	return map[string]any{
		"id": u.Id, "username": u.Username, "nickname": u.Nickname, "email": u.Email,
		"phone": u.Phone, "avatar": u.Avatar, "bio": u.Bio, "isBanned": u.IsBanned,
		"banReason": u.BanReason, "role": u.Role, "createdAt": u.CreatedAt,
		"updatedAt": u.UpdatedAt, "lastLoginAt": u.LastLoginAt,
	}
}

// exists 检查行是否存在。
func exists(ctx context.Context, h *Handler, sql string, args ...any) bool {
	var one int
	if err := h.db().Pool.QueryRow(ctx, sql, args...).Scan(&one); err != nil {
		return false
	}
	return one == 1
}

// ============ GET /api/admin/users 用户列表 ============

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	q := r.URL.Query()
	search := q.Get("search")
	page := atoiMax(q.Get("page"), 1, 1)
	pageSize := atoiMax(q.Get("pageSize"), 20, 1)
	if pageSize > 100 {
		pageSize = 100
	}
	status := q.Get("status")
	if status == "" {
		status = "all"
	}

	var conds []string
	var args []any
	if search != "" {
		args = append(args, search)
		conds = append(conds, fmt.Sprintf(`(u."username" LIKE '%%'||$%d||'%%' OR u."email" LIKE '%%'||$%d||'%%' OR u."nickname" LIKE '%%'||$%d||'%%' OR u."phone" LIKE '%%'||$%d||'%%')`, len(args), len(args), len(args), len(args)))
	}
	switch status {
	case "banned":
		conds = append(conds, `u."isBanned"=true`)
	case "active":
		conds = append(conds, `u."isBanned"=false`)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int64
	if err := h.db().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM "User" u `+where, args...).Scan(&total); err != nil {
		log.Printf("[Admin] 用户列表查询失败: %v", err)
		util.WriteError(w, 500, "查询失败")
		return
	}
	users, err := db.QueryToStructs[userListRow](ctx, h.db(),
		`SELECT u."`+userListSelect+` FROM "User" u `+where+` ORDER BY u."createdAt" DESC LIMIT $`+strconv.Itoa(len(args)+1)+` OFFSET $`+strconv.Itoa(len(args)+2),
		append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		log.Printf("[Admin] 用户列表查询失败: %v", err)
		util.WriteError(w, 500, "查询失败")
		return
	}
	items := make([]map[string]any, 0, len(users))
	for i := range users {
		items = append(items, userListToMap(&users[i]))
	}
	util.WriteJSON(w, 200, map[string]any{"users": items, "total": total, "page": page, "pageSize": pageSize})
}

// ============ POST /api/admin/users/create 管理员注册用户 ============

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
		Phone    string `json:"phone"`
		Email    string `json:"email"`
		Bio      string `json:"bio"`
		Avatar   string `json:"avatar"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	admin := middleware.AdminFrom(r)
	ip := h.clientIP(r)

	// 参数校验
	if req.Username == "" || req.Password == "" {
		util.WriteError(w, 400, "用户ID和密码为必填项")
		return
	}
	// 用户ID格式校验
	if !usernameRe.MatchString(req.Username) {
		util.WriteError(w, 400, "用户ID只能包含字母、数字和下划线，长度1-20位")
		return
	}
	// 输入安全检查
	if containsDangerousInput(req.Username) {
		util.WriteError(w, 400, "用户ID包含不允许的字符")
		return
	}
	// 密码强度校验
	if len(req.Password) < 8 {
		util.WriteError(w, 400, "密码至少8位")
		return
	}
	if !lowerRe.MatchString(req.Password) || !upperRe.MatchString(req.Password) || !digitRe.MatchString(req.Password) {
		util.WriteError(w, 400, "密码必须包含大小写字母和数字")
		return
	}
	// 唯一性检查
	if exists(ctx, h, `SELECT 1 FROM "User" WHERE "username"=$1`, req.Username) {
		util.WriteError(w, 409, "用户ID已存在")
		return
	}
	if req.Phone != "" && exists(ctx, h, `SELECT 1 FROM "User" WHERE "phone"=$1`, req.Phone) {
		util.WriteError(w, 409, "手机号已被注册")
		return
	}
	if req.Email != "" && exists(ctx, h, `SELECT 1 FROM "User" WHERE "email"=$1`, req.Email) {
		util.WriteError(w, 409, "邮箱已被注册")
		return
	}

	hash, err := util.HashPassword(req.Password)
	if err != nil {
		util.WriteError(w, 500, "创建失败，请重试")
		return
	}
	nickname := req.Nickname
	if nickname == "" {
		nickname = req.Username
	}
	created, err := db.QueryRowToStruct[userCreateRow](ctx, h.db(),
		`INSERT INTO "User"("id","username","password","nickname","phone","email","bio","avatar","createdAt","updatedAt")
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,NOW(),NOW())
		 RETURNING `+userCreateCols,
		util.NewID(), req.Username, hash, nickname,
		strOrNil(req.Phone), strOrNil(req.Email), strOrNil(req.Bio), strOrNil(req.Avatar))
	if err != nil {
		log.Printf("[Admin] 创建用户失败: %v", err)
		util.WriteError(w, 500, "创建失败，请重试")
		return
	}

	h.addLog(ctx, admin.Id, admin.Username, "创建用户", "user:"+created.Id,
		"管理员创建用户 "+created.Username+" (昵称: "+util.StrVal(created.Nickname)+")", ip)
	util.WriteJSON(w, 200, map[string]any{"success": true, "user": map[string]any{
		"id": created.Id, "username": created.Username, "nickname": created.Nickname,
		"email": created.Email, "phone": created.Phone, "avatar": created.Avatar,
		"bio": created.Bio, "isBanned": created.IsBanned, "role": created.Role,
		"createdAt": created.CreatedAt,
	}})
}

// ============ GET /api/admin/users/:id 获取单个用户详情 ============

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	u, err := db.QueryRowToStruct[userListRow](ctx, h.db(),
		`SELECT u."`+userListSelect+` FROM "User" u WHERE u."id"=$1`, id)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "用户不存在")
			return
		}
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"user": userListToMap(u)})
}

// ============ PUT /api/admin/users/:id 管理员编辑用户信息 ============

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	var req struct {
		Username *string `json:"username"`
		Nickname *string `json:"nickname"`
		Phone    *string `json:"phone"`
		Email    *string `json:"email"`
		Bio      *string `json:"bio"`
		Avatar   *string `json:"avatar"`
		Password *string `json:"password"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	admin := middleware.AdminFrom(r)
	ip := h.clientIP(r)

	existing, err := db.QueryRowToStruct[userAdminRow](ctx, h.db(),
		`SELECT `+userSelectCols+` FROM "User" WHERE "id"=$1`, id)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "用户不存在")
			return
		}
		util.WriteError(w, 500, "查询失败")
		return
	}

	var sets []string
	var args []any
	var changes []string
	profileChanged := false
	n := 1
	add := func(col string, val any) {
		sets = append(sets, fmt.Sprintf(`"%s"=$%d`, col, n))
		args = append(args, val)
		n++
	}

	// 修改用户ID（username）
	if req.Username != nil && *req.Username != existing.Username {
		if !usernameRe.MatchString(*req.Username) {
			util.WriteError(w, 400, "用户ID只能包含字母、数字和下划线，长度1-20位")
			return
		}
		if containsDangerousInput(*req.Username) {
			util.WriteError(w, 400, "用户ID包含不允许的字符")
			return
		}
		if exists(ctx, h, `SELECT 1 FROM "User" WHERE "username"=$1`, *req.Username) {
			util.WriteError(w, 409, "用户ID已被占用")
			return
		}
		add("username", *req.Username)
		changes = append(changes, "用户ID: "+existing.Username+" → "+*req.Username)
		profileChanged = true
	}
	// 修改昵称
	if req.Nickname != nil && util.StrVal(req.Nickname) != util.StrVal(existing.Nickname) {
		add("nickname", strOrNil(*req.Nickname))
		changes = append(changes, "昵称: "+dash(util.StrVal(existing.Nickname))+" → "+*req.Nickname)
		profileChanged = true
	}
	// 修改手机号
	if req.Phone != nil && util.StrVal(req.Phone) != util.StrVal(existing.Phone) {
		if *req.Phone != "" {
			var dupID string
			_ = h.db().Pool.QueryRow(ctx, `SELECT "id" FROM "User" WHERE "phone"=$1`, *req.Phone).Scan(&dupID)
			if dupID != "" && dupID != id {
				util.WriteError(w, 409, "手机号已被其他用户使用")
				return
			}
		}
		add("phone", strOrNil(*req.Phone))
		changes = append(changes, "手机号: "+dash(util.StrVal(existing.Phone))+" → "+dash(*req.Phone))
		profileChanged = true
	}
	// 修改邮箱
	if req.Email != nil && util.StrVal(req.Email) != util.StrVal(existing.Email) {
		if *req.Email != "" {
			var dupID string
			_ = h.db().Pool.QueryRow(ctx, `SELECT "id" FROM "User" WHERE "email"=$1`, *req.Email).Scan(&dupID)
			if dupID != "" && dupID != id {
				util.WriteError(w, 409, "邮箱已被其他用户使用")
				return
			}
		}
		add("email", strOrNil(*req.Email))
		changes = append(changes, "邮箱: "+dash(util.StrVal(existing.Email))+" → "+dash(*req.Email))
		profileChanged = true
	}
	// 修改简介
	if req.Bio != nil && util.StrVal(req.Bio) != util.StrVal(existing.Bio) {
		add("bio", strOrNil(*req.Bio))
		changes = append(changes, "简介已更新")
		profileChanged = true
	}
	// 修改头像
	if req.Avatar != nil && util.StrVal(req.Avatar) != util.StrVal(existing.Avatar) {
		add("avatar", strOrNil(*req.Avatar))
		changes = append(changes, "头像已更新")
		profileChanged = true
	}
	// 修改密码
	if req.Password != nil && *req.Password != "" {
		if len(*req.Password) < 8 {
			util.WriteError(w, 400, "密码至少8位")
			return
		}
		hash, herr := util.HashPassword(*req.Password)
		if herr != nil {
			util.WriteError(w, 500, "操作失败，请重试")
			return
		}
		add("password", hash)
		changes = append(changes, "密码已重置")
		// 清除该用户所有会话，强制重新登录（先清缓存再删 DB 行）
		h.deps.Auth.InvalidateUserSessions(ctx, id)
		_, _ = h.db().Exec(ctx, `DELETE FROM "UserSession" WHERE "userId"=$1`, id)
	}

	if len(sets) == 0 {
		util.WriteError(w, 400, "没有需要修改的内容")
		return
	}
	sets = append(sets, `"updatedAt"=NOW()`)
	args = append(args, id)
	updated, err := db.QueryRowToStruct[userUpdateRow](ctx, h.db(),
		`UPDATE "User" SET `+strings.Join(sets, ",")+` WHERE "id"=$`+strconv.Itoa(n)+` RETURNING `+userUpdateCols,
		args...)
	if err != nil {
		log.Printf("[Admin] 编辑用户失败: %v", err)
		util.WriteError(w, 500, "操作失败，请重试")
		return
	}

	h.addLog(ctx, admin.Id, admin.Username, "编辑用户", "user:"+updated.Id, strings.Join(changes, "; "), ip)

	// 资料字段变更时广播同步（密码重置等不触发）
	if profileChanged {
		h.publishUserProfileUpdatedById(ctx, updated.Id)
	}

	util.WriteJSON(w, 200, map[string]any{"success": true, "user": userRowToMap(updated)})
}

// ============ POST /api/admin/users/:id/ban 封禁/解封用户 ============

func (h *Handler) banUser(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	var req struct {
		Ban    bool   `json:"ban"`
		Reason string `json:"reason"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	admin := middleware.AdminFrom(r)
	ip := h.clientIP(r)

	if !exists(ctx, h, `SELECT 1 FROM "User" WHERE "id"=$1`, id) {
		util.WriteError(w, 404, "用户不存在")
		return
	}

	banReason := "管理员操作"
	if req.Reason != "" {
		banReason = req.Reason
	}
	// TS 返回完整的更新后用户对象（prisma update 无 select），此处 SELECT 全部用户列保持一致。
	u, err := db.QueryRowToStruct[db.User](ctx, h.db(),
		`UPDATE "User" SET "isBanned"=$1,"banReason"=$2,"updatedAt"=NOW() WHERE "id"=$3 RETURNING `+
			`"id","dialogId","username","email","phone","password","nickname","avatar","backgroundUrl","bio","gender","region","birthday","isBot","isBanned","banReason","role","phoneVerified","emailVerified","lastLoginAt","lastLoginIp","webPushSubscription","createdAt","updatedAt"`,
		req.Ban, strOrNil(map[bool]string{true: banReason, false: ""}[req.Ban]), id)
	if err != nil {
		log.Printf("[Admin] 封禁用户失败: %v", err)
		util.WriteError(w, 500, "操作失败，请重试")
		return
	}

	// 封禁时清除该用户所有会话，强制下线（先清缓存再删 DB 行）
	if req.Ban {
		h.deps.Auth.InvalidateUserSessions(ctx, id)
		_, _ = h.db().Exec(ctx, `DELETE FROM "UserSession" WHERE "userId"=$1`, id)
	}

	detail := ""
	if req.Ban {
		detail = "封禁 " + u.Username
		if req.Reason != "" {
			detail += " (原因: " + req.Reason + ")"
		}
	} else {
		detail = "解封 " + u.Username
	}
	action := "解封用户"
	if req.Ban {
		action = "封禁用户"
	}
	h.addLog(ctx, admin.Id, admin.Username, action, "user:"+u.Id, detail, ip)
	// PORTING.md：绝不返回 password 哈希（TS 此处返回完整对象，Go 版按规范清掉）。
	u.Password = ""
	util.WriteJSON(w, 200, map[string]any{"success": true, "user": u})
}

// ============ DELETE /api/admin/users/:id 删除用户 ============

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	admin := middleware.AdminFrom(r)

	u, err := db.QueryRowToStruct[userAdminRow](ctx, h.db(),
		`SELECT `+userSelectCols+` FROM "User" WHERE "id"=$1`, id)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "用户不存在")
			return
		}
		util.WriteError(w, 500, "查询失败")
		return
	}
	if _, err := h.db().Exec(ctx, `DELETE FROM "User" WHERE "id"=$1`, id); err != nil {
		log.Printf("[Admin] 删除用户失败: %v", err)
		util.WriteError(w, 500, "操作失败，请重试")
		return
	}
	h.addLog(ctx, admin.Id, admin.Username, "删除用户", "user:"+u.Id, "删除 "+u.Username, h.clientIP(r))
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// 小工具
// ============================================================

// atoiMax 解析整数，失败或小于 min 时返回 def。
func atoiMax(s string, def, min int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < min {
		return def
	}
	return n
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
