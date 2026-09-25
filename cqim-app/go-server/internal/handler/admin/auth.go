package admin

import (
	"log"
	"net/http"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============ POST /api/admin/login 管理员登录 ============
//
// 安全措施（与 TS 一致）：
// 1. 速率限制（AdminLoginRateLimit：每 IP 每 15 分钟 5 次）
// 2. 会话有效期 2 小时
// 3. 密码哈希自动升级 SHA-256 → bcrypt
// 4. 统一错误信息，防止用户名枚举
// 5. 记录登录 IP 和 UA
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	ip := h.clientIP(r)
	ua := r.UserAgent()

	if req.Username == "" || req.Password == "" {
		util.WriteError(w, 400, "请填写用户名和密码")
		return
	}

	// 输入安全检查
	if containsDangerousInput(req.Username) {
		util.WriteError(w, 400, "输入包含不允许的字符")
		return
	}

	admin, err := db.QueryRowToStruct[db.AdminAccount](ctx, h.db(),
		`SELECT "id","username","password","role","createdAt","updatedAt" FROM "AdminAccount" WHERE "username"=$1`,
		req.Username)

	// ★ 统一错误信息，防止用户名枚举攻击
	if err != nil || !verifyAdminPassword(req.Password, admin.Password) {
		name := req.Username
		if name == "" {
			name = "unknown"
		}
		h.addFailedLoginLog(ctx, name, "IP: "+ip+", UA: "+truncateUA(ua), ip)
		util.WriteError(w, 401, "用户名或密码错误")
		return
	}

	// ★ 密码哈希自动升级：SHA-256 → bcrypt
	if isLegacyHash(admin.Password) {
		if hash, herr := util.HashPassword(req.Password); herr == nil {
			if _, uerr := h.db().Exec(ctx,
				`UPDATE "AdminAccount" SET "password"=$1,"updatedAt"=NOW() WHERE "id"=$2`,
				hash, admin.Id); uerr == nil {
				log.Printf("[Admin] 管理员 %s 密码哈希已升级为 bcrypt", admin.Username)
			}
		}
	}

	// ★ 登录成功，重置限流
	middleware.ResetAdminLoginLimits(ip)

	token := util.GenerateToken(48)
	// ★ 会话有效期 2 小时
	if _, err := h.db().Exec(ctx,
		`INSERT INTO "AdminSession"("id","adminId","token","expiresAt","createdAt")
		 VALUES($1,$2,$3,NOW() + INTERVAL '2 hours',NOW())`,
		util.NewID(), admin.Id, token); err != nil {
		log.Printf("[Admin] 创建管理会话失败: %v", err)
		util.WriteError(w, 500, "登录失败，请重试")
		return
	}

	// 清理该管理员的过期会话
	_, _ = h.db().Exec(ctx,
		`DELETE FROM "AdminSession" WHERE "adminId"=$1 AND "expiresAt" < NOW()`, admin.Id)

	h.addLog(ctx, admin.Id, admin.Username, "登录", "system", "IP: "+ip+", UA: "+truncateUA(ua), ip)

	// ★ 通过 HttpOnly Cookie 下发 token（更安全），同时保留 JSON 响应兼容现有前端
	cookie := &http.Cookie{
		Name:     "admin_token",
		Value:    token,
		Path:     "/api/admin",
		HttpOnly: true,
		Secure:   h.deps.Cfg.IsProduction(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   2 * 3600,
	}
	if domain := adminDomain(); domain != "" {
		cookie.Domain = domain
	}
	http.SetCookie(w, cookie)

	util.WriteJSON(w, 200, map[string]any{
		"token": token,
		"admin": map[string]any{"id": admin.Id, "username": admin.Username, "role": admin.Role},
	})
}

// ============ POST /api/admin/logout 登出 ============

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var token string
	if v, ok := r.Context().Value(middleware.CtxAdminToken).(string); ok {
		token = v
	}
	if token != "" {
		_, _ = h.db().Exec(ctx, `DELETE FROM "AdminSession" WHERE "token"=$1`, token)
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============ GET /api/admin/me 当前管理员 ============

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	a := middleware.AdminFrom(r)
	util.WriteJSON(w, 200, map[string]any{"id": a.Id, "username": a.Username, "role": a.Role})
}
