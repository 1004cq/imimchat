package admin

import (
	"log"
	"net/http"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// adminBrief 管理员列表/创建响应的公开字段（不含 password）。
func adminBrief(a *db.AdminAccount) map[string]any {
	return map[string]any{"id": a.Id, "username": a.Username, "role": a.Role, "createdAt": a.CreatedAt}
}

// checkAdminPasswordStrength 管理员密码强度：至少 10 位，包含大小写+数字+特殊字符。
func checkAdminPasswordStrength(password string) string {
	if len(password) < 10 {
		return "管理员密码至少 10 位"
	}
	if !lowerRe.MatchString(password) || !upperRe.MatchString(password) ||
		!digitRe.MatchString(password) || !specialRe.MatchString(password) {
		return "密码必须包含大小写字母、数字和特殊字符"
	}
	return ""
}

// ============ GET /api/admin/admins 管理员列表（仅 superadmin） ============

func (h *Handler) listAdmins(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	admins, err := db.QueryToStructs[db.AdminAccount](ctx, h.db(),
		`SELECT "id","username","password","role","createdAt","updatedAt" FROM "AdminAccount" ORDER BY "createdAt" ASC`)
	if err != nil {
		util.WriteError(w, 500, "查询失败")
		return
	}
	items := make([]map[string]any, 0, len(admins))
	for i := range admins {
		items = append(items, adminBrief(&admins[i]))
	}
	util.WriteJSON(w, 200, map[string]any{"admins": items})
}

// ============ POST /api/admin/admins 创建管理员（仅 superadmin） ============

func (h *Handler) createAdmin(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Role == "" {
		req.Role = "admin"
	}
	if req.Username == "" || req.Password == "" {
		util.WriteError(w, 400, "请填写用户名和密码")
		return
	}

	// ★ 输入安全检查
	if containsDangerousInput(req.Username) {
		util.WriteError(w, 400, "用户名包含不允许的字符")
		return
	}

	// ★ 管理员密码强度要求
	if msg := checkAdminPasswordStrength(req.Password); msg != "" {
		util.WriteError(w, 400, msg)
		return
	}

	if exists(ctx, h, `SELECT 1 FROM "AdminAccount" WHERE "username"=$1`, req.Username) {
		util.WriteError(w, 409, "用户名已存在")
		return
	}

	hash, err := util.HashPassword(req.Password)
	if err != nil {
		util.WriteError(w, 500, "创建失败，请重试")
		return
	}
	admin := middleware.AdminFrom(r)
	newAdmin, err := db.QueryRowToStruct[db.AdminAccount](ctx, h.db(),
		`INSERT INTO "AdminAccount"("id","username","password","role","createdAt","updatedAt")
		 VALUES($1,$2,$3,$4,NOW(),NOW())
		 RETURNING "id","username","password","role","createdAt","updatedAt"`,
		util.NewID(), req.Username, hash, req.Role)
	if err != nil {
		log.Printf("[Admin] 创建管理员失败: %v", err)
		util.WriteError(w, 500, "创建失败，请重试")
		return
	}
	h.addLog(ctx, admin.Id, admin.Username, "创建管理员", "admin:"+newAdmin.Id,
		"创建 "+newAdmin.Username+" (角色: "+req.Role+")", h.clientIP(r))
	util.WriteJSON(w, 200, map[string]any{"success": true, "admin": adminBrief(newAdmin)})
}

// ============ PUT /api/admin/admins/:id/password 修改管理员密码（仅 superadmin） ============

func (h *Handler) updateAdminPassword(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	var req struct {
		Password string `json:"password"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Password == "" {
		util.WriteError(w, 400, "请填写新密码")
		return
	}

	// ★ 管理员密码强度要求
	if msg := checkAdminPasswordStrength(req.Password); msg != "" {
		util.WriteError(w, 400, msg)
		return
	}

	hash, err := util.HashPassword(req.Password)
	if err != nil {
		util.WriteError(w, 500, "操作失败，请重试")
		return
	}
	if _, err := h.db().Exec(ctx,
		`UPDATE "AdminAccount" SET "password"=$1,"updatedAt"=NOW() WHERE "id"=$2`, hash, id); err != nil {
		log.Printf("[Admin] 修改管理员密码失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}

	// ★ 修改密码后清除该管理员所有会话（强制重新登录）
	_, _ = h.db().Exec(ctx, `DELETE FROM "AdminSession" WHERE "adminId"=$1`, id)

	admin := middleware.AdminFrom(r)
	h.addLog(ctx, admin.Id, admin.Username, "修改管理员密码", "admin:"+id, "", h.clientIP(r))
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============ DELETE /api/admin/admins/:id 删除管理员（仅 superadmin） ============

func (h *Handler) deleteAdmin(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	admin := middleware.AdminFrom(r)
	if admin.Id == id {
		util.WriteError(w, 400, "不能删除自己")
		return
	}
	var username string
	_ = h.db().Pool.QueryRow(ctx, `SELECT "username" FROM "AdminAccount" WHERE "id"=$1`, id).Scan(&username)
	if _, err := h.db().Exec(ctx, `DELETE FROM "AdminAccount" WHERE "id"=$1`, id); err != nil {
		log.Printf("[Admin] 删除管理员失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	target := username
	if target == "" {
		target = "unknown"
	}
	h.addLog(ctx, admin.Id, admin.Username, "删除管理员", "admin:"+id, "删除 "+target, h.clientIP(r))
	util.WriteJSON(w, 200, map[string]any{"success": true})
}
