package auth

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// RegisterRoutes 注册 /api/auth 下全部路由（与 auth.ts 路由表一致）。
//
//	POST /api/auth/send-code      (CodeRateLimit)
//	POST /api/auth/register
//	POST /api/auth/login         (LoginRateLimit)
//	GET  /api/auth/me            (userAuth)
//	POST /api/auth/logout        (userAuth)
//	POST /api/auth/change-password (userAuth)
//	POST /api/auth/reset-password
//	POST /api/auth/bind-phone    (userAuth)
//	POST /api/auth/bind-email    (userAuth)
//	PUT  /api/auth/profile       (userAuth)
//	GET  /api/auth/sessions      (userAuth)
//	DELETE /api/auth/sessions/{id} (userAuth)
//	POST /api/auth/check-account
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := &Handler{d: d}

	mux.Handle("POST /api/auth/send-code", middleware.CodeRateLimit(d.Cfg, d.DB, http.HandlerFunc(h.sendCode)))
	mux.Handle("POST /api/auth/register", http.HandlerFunc(h.register))
	mux.Handle("POST /api/auth/login", middleware.LoginRateLimit(d.Cfg, d.DB, http.HandlerFunc(h.login)))

	mux.Handle("GET /api/auth/me", d.Auth.UserAuth(http.HandlerFunc(h.me)))
	mux.Handle("POST /api/auth/logout", d.Auth.UserAuth(http.HandlerFunc(h.logout)))
	mux.Handle("POST /api/auth/change-password", d.Auth.UserAuth(http.HandlerFunc(h.changePassword)))
	mux.Handle("POST /api/auth/reset-password", http.HandlerFunc(h.resetPassword))
	mux.Handle("POST /api/auth/bind-phone", d.Auth.UserAuth(http.HandlerFunc(h.bindPhone)))
	mux.Handle("POST /api/auth/bind-email", d.Auth.UserAuth(http.HandlerFunc(h.bindEmail)))
	mux.Handle("PUT /api/auth/profile", d.Auth.UserAuth(http.HandlerFunc(h.updateProfile)))
	mux.Handle("GET /api/auth/sessions", d.Auth.UserAuth(http.HandlerFunc(h.listSessions)))
	mux.Handle("DELETE /api/auth/sessions/{id}", d.Auth.UserAuth(http.HandlerFunc(h.deleteSession)))
	mux.Handle("POST /api/auth/check-account", http.HandlerFunc(h.checkAccount))
}

// ============ 对外 user 对象（绝不含 password） ============

func dialogIDJSON(u *db.User) any {
	if u.DialogId == nil {
		return nil
	}
	return *u.DialogId
}

// registerUserJSON 注册响应中的 user 对象（字段照抄 auth.ts）。
func registerUserJSON(u *db.User) map[string]any {
	return map[string]any{
		"id":            u.Id,
		"dialogId":      dialogIDJSON(u),
		"username":      u.Username,
		"nickname":      util.StrVal(u.Nickname),
		"phone":         util.StrVal(u.Phone),
		"email":         util.StrVal(u.Email),
		"avatar":        avatarToProxy(util.StrVal(u.Avatar)),
		"isBot":         u.IsBot,
		"phoneVerified": u.PhoneVerified,
		"emailVerified": u.EmailVerified,
	}
}

// loginUserJSON 登录响应中的 user 对象（比注册多 bio 字段，照抄 auth.ts）。
func loginUserJSON(u *db.User) map[string]any {
	m := registerUserJSON(u)
	m["bio"] = util.StrVal(u.Bio)
	return m
}

// ============ 注册 ============

/**
 * POST /api/auth/register
 * body: { username, password, phone?, email?, phoneCode?, emailCode?, nickname? }
 */
func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		Phone     string `json:"phone"`
		Email     string `json:"email"`
		PhoneCode string `json:"phoneCode"`
		EmailCode string `json:"emailCode"`
		Nickname  string `json:"nickname"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()

	// 基本验证
	if req.Username == "" || req.Password == "" {
		util.WriteError(w, 400, "请填写用户ID和密码")
		return
	}
	if !isValidUsername(req.Username) {
		util.WriteError(w, 400, "用户ID格式不正确（1-20位字母数字下划线）")
		return
	}
	// ★ 强密码策略检查
	if pw := checkPasswordStrength(req.Password); !pw.valid {
		util.WriteError(w, 400, pw.err)
		return
	}
	// ★ 输入安全检查
	if containsDangerousInput(req.Username) || containsDangerousInput(req.Nickname) {
		util.WriteError(w, 400, "输入包含不允许的字符")
		return
	}

	// 检查用户名是否已存在
	existing, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "username"=$1`, req.Username)
	if err != nil && !db.IsNotFound(err) {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	if existing != nil {
		util.WriteError(w, 409, "该用户ID已被使用")
		return
	}

	// 手机号验证
	phoneVerified := false
	if req.Phone != "" {
		if !isValidPhone(req.Phone) {
			util.WriteError(w, 400, "手机号格式不正确")
			return
		}
		existingPhone, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "phone"=$1`, req.Phone)
		if err != nil && !db.IsNotFound(err) {
			util.WriteError(w, 500, "服务器内部错误")
			return
		}
		if existingPhone != nil {
			util.WriteError(w, 409, "该手机号已注册")
			return
		}
		if req.PhoneCode != "" {
			result := h.verifyCode(ctx, req.Phone, req.PhoneCode, "register", "sms")
			if !result.valid {
				util.WriteError(w, 400, result.err)
				return
			}
			phoneVerified = true
		}
	}

	// 邮箱验证
	emailVerified := false
	if req.Email != "" {
		if !isValidEmail(req.Email) {
			util.WriteError(w, 400, "邮箱格式不正确")
			return
		}
		existingEmail, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "email"=$1`, req.Email)
		if err != nil && !db.IsNotFound(err) {
			util.WriteError(w, 500, "服务器内部错误")
			return
		}
		if existingEmail != nil {
			util.WriteError(w, 409, "该邮箱已注册")
			return
		}
		if req.EmailCode != "" {
			result := h.verifyCode(ctx, req.Email, req.EmailCode, "register", "email")
			if !result.valid {
				util.WriteError(w, 400, result.err)
				return
			}
			emailVerified = true
		}
	}

	// 创建用户（生成 TG 风格 Dialog ID）
	pwHash, err := util.HashPassword(req.Password)
	if err != nil {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	nickname := req.Nickname
	if nickname == "" {
		nickname = "用户" + req.Username
	}
	userID := util.NewID()
	dialogID := generateUserDialogId()
	_, err = h.d.DB.Exec(ctx,
		`INSERT INTO "User" ("id","username","password","phone","email","nickname","phoneVerified","emailVerified","dialogId","isBot","role","createdAt","updatedAt") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,false,'user',NOW(),NOW())`,
		userID, req.Username, pwHash, nullIfEmpty(req.Phone), nullIfEmpty(req.Email),
		nickname, phoneVerified, emailVerified, dialogID)
	if err != nil {
		if isUniqueViolation(err) {
			util.WriteError(w, 409, "该用户ID已被使用")
			return
		}
		util.WriteError(w, 500, "服务器内部错误")
		return
	}

	// 创建会话
	token, err := h.createSession(ctx, r, userID)
	if err != nil {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}

	// 记录登录日志
	h.logLogin(ctx, loginLogParams{
		userID: userID, username: req.Username, phone: req.Phone, email: req.Email,
		ip: h.clientIP(r), userAgent: r.UserAgent(), success: true,
	})

	user, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "id"=$1`, userID)
	if err != nil || user == nil {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}

	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"token":   token,
		"user":    registerUserJSON(user),
	})
}

// ============ 登录 ============

/**
 * POST /api/auth/login
 * body: { account, password?, code?, loginType, spToken? }
 * loginType: password | sms | email | phone_auth
 * account: 用户ID / 手机号 / 邮箱
 */
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	// 对应 TS 的 try/catch：任何未预期错误都返回 500
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[Auth] 登录错误: %v", rec)
			util.WriteError(w, 500, "服务器内部错误")
		}
	}()

	var req struct {
		Account   string `json:"account"`
		Password  string `json:"password"`
		Code      string `json:"code"`
		LoginType string `json:"loginType"`
		SpToken   string `json:"spToken"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	loginType := req.LoginType
	if loginType == "" {
		loginType = "password"
	}
	ctx := r.Context()

	if req.Account == "" {
		util.WriteError(w, 400, "请输入账号")
		return
	}

	ip := h.clientIP(r)
	ua := r.UserAgent()
	account := req.Account

	var user *db.User

	switch loginType {
	case "sms":
		// 短信验证码登录
		if !isValidPhone(account) {
			util.WriteError(w, 400, "手机号格式不正确")
			return
		}
		if req.Code == "" {
			util.WriteError(w, 400, "请输入验证码")
			return
		}
		verifyResult := h.verifyCode(ctx, account, req.Code, "login", "sms")
		if !verifyResult.valid {
			h.logLogin(ctx, loginLogParams{phone: account, ip: ip, userAgent: ua, success: false, failReason: verifyResult.err})
			util.WriteError(w, 400, verifyResult.err)
			return
		}
		u, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "phone"=$1`, account)
		if err != nil && !db.IsNotFound(err) {
			panic(err)
		}
		if u == nil {
			util.WriteError(w, 404, "该手机号未注册")
			return
		}
		user = u

	case "email":
		// 邮箱验证码登录
		if !isValidEmail(account) {
			util.WriteError(w, 400, "邮箱格式不正确")
			return
		}
		if req.Code == "" {
			util.WriteError(w, 400, "请输入验证码")
			return
		}
		verifyResult := h.verifyCode(ctx, account, req.Code, "login", "email")
		if !verifyResult.valid {
			h.logLogin(ctx, loginLogParams{email: account, ip: ip, userAgent: ua, success: false, failReason: verifyResult.err})
			util.WriteError(w, 400, verifyResult.err)
			return
		}
		u, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "email"=$1`, account)
		if err != nil && !db.IsNotFound(err) {
			panic(err)
		}
		if u == nil {
			util.WriteError(w, 404, "该邮箱未注册")
			return
		}
		user = u

	case "phone_auth":
		// 阿里云号码认证（一键登录）
		if req.SpToken == "" {
			util.WriteError(w, 400, "缺少认证 token")
			return
		}
		authResult := h.getPhoneByToken(ctx, req.SpToken)
		if !authResult.success || authResult.phone == "" {
			msg := authResult.message
			if msg == "" {
				msg = "号码认证失败"
			}
			util.WriteError(w, 400, msg)
			return
		}
		u, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "phone"=$1`, authResult.phone)
		if err != nil && !db.IsNotFound(err) {
			panic(err)
		}
		if u == nil {
			// 号码认证自动注册（生成 TG 风格 Dialog ID）
			autoUsername := "user_" + strconv.FormatInt(time.Now().UnixMilli(), 36)
			pwHash, herr := util.HashPassword(util.GenerateToken(16))
			if herr != nil {
				panic(herr)
			}
			newID := util.NewID()
			_, err = h.d.DB.Exec(ctx,
				`INSERT INTO "User" ("id","username","password","phone","nickname","phoneVerified","dialogId","isBot","role","createdAt","updatedAt") VALUES ($1,$2,$3,$4,$5,true,$6,false,'user',NOW(),NOW())`,
				newID, autoUsername, pwHash, authResult.phone, "用户"+autoUsername, generateUserDialogId())
			if err != nil {
				panic(err)
			}
			u, err = db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "id"=$1`, newID)
			if err != nil || u == nil {
				panic("auto register failed")
			}
		}
		user = u

	default:
		// 密码登录（支持用户ID/手机号/邮箱/系统ID/DialogID）
		if req.Password == "" {
			util.WriteError(w, 400, "请输入密码")
			return
		}
		u := h.findUserForLogin(ctx, account)
		if u == nil {
			h.logLogin(ctx, loginLogParams{username: account, ip: ip, userAgent: ua, success: false, failReason: "账号不存在"})
			util.WriteError(w, 401, "账号或密码错误")
			return
		}
		if !verifyPasswordCompat(req.Password, u.Password) {
			h.logLogin(ctx, loginLogParams{userID: u.Id, username: u.Username, ip: ip, userAgent: ua, success: false, failReason: "密码错误"})
			util.WriteError(w, 401, "账号或密码错误")
			return
		}
		// ★ 密码哈希自动升级：SHA-256 → bcrypt
		if isLegacyHash(u.Password) {
			if newHash, herr := util.HashPassword(req.Password); herr == nil {
				_, _ = h.d.DB.Exec(ctx, `UPDATE "User" SET "password"=$2 WHERE "id"=$1`, u.Id, newHash)
				log.Printf("[Auth] 用户 %s 密码哈希已升级为 bcrypt", u.Username)
			}
		}
		user = u
	}

	if user == nil {
		util.WriteError(w, 401, "登录失败")
		return
	}

	if user.IsBanned {
		util.WriteJSON(w, 403, map[string]any{"error": "账号已被封禁", "reason": user.BanReason})
		return
	}

	// ★ 登录成功，重置限流计数
	middleware.ResetLoginLimits(ip, account)

	// 创建会话
	token, err := h.createSession(ctx, r, user.Id)
	if err != nil {
		panic(err)
	}

	// 记录登录日志
	h.logLogin(ctx, loginLogParams{
		userID: user.Id, username: user.Username,
		phone: util.StrVal(user.Phone), email: util.StrVal(user.Email),
		ip: ip, userAgent: ua, success: true,
	})

	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"token":   token,
		"user":    loginUserJSON(user),
	})
}

// findUserForLogin 按优先级查找用户：phone > email > username > dialogId > id。
func (h *Handler) findUserForLogin(ctx context.Context, account string) *db.User {
	queries := [][2]string{}
	if isValidPhone(account) {
		queries = append(queries, [2]string{"phone", account})
	} else if isValidEmail(account) {
		queries = append(queries, [2]string{"email", account})
	} else {
		queries = append(queries, [2]string{"username", account}, [2]string{"dialogId", account}, [2]string{"id", account})
	}
	for _, q := range queries {
		u, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "`+q[0]+`"=$1`, q[1])
		if err == nil && u != nil {
			return u
		}
	}
	return nil
}

// ============ 获取当前用户信息 ============

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	if u == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	util.WriteJSON(w, 200, map[string]any{
		"user": map[string]any{
			"id":            u.Id,
			"dialogId":      dialogIDJSON(u),
			"username":      u.Username,
			"nickname":      util.StrVal(u.Nickname),
			"phone":         util.StrVal(u.Phone),
			"email":         util.StrVal(u.Email),
			"avatar":        avatarToProxy(util.StrVal(u.Avatar)),
			"bio":           util.StrVal(u.Bio),
			"gender":        util.StrVal(u.Gender),
			"region":        util.StrVal(u.Region),
			"birthday":      util.StrVal(u.Birthday),
			"isBot":         u.IsBot,
			"phoneVerified": u.PhoneVerified,
			"emailVerified": u.EmailVerified,
			"createdAt":     u.CreatedAt,
			"updatedAt":     u.UpdatedAt.UnixMilli(),
		},
	})
}

// ============ 退出登录 ============

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	token := middleware.SessionTokenFrom(r)
	_, _ = h.d.DB.Exec(r.Context(), `DELETE FROM "UserSession" WHERE "token"=$1`, token)
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============ 修改密码 ============

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	u := middleware.UserFrom(r)
	if u == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	ctx := r.Context()

	if req.OldPassword == "" || req.NewPassword == "" {
		util.WriteError(w, 400, "请填写原密码和新密码")
		return
	}
	// ★ 强密码策略检查
	if pw := checkPasswordStrength(req.NewPassword); !pw.valid {
		util.WriteError(w, 400, pw.err)
		return
	}
	if !verifyPasswordCompat(req.OldPassword, u.Password) {
		util.WriteError(w, 400, "原密码错误")
		return
	}

	newHash, err := util.HashPassword(req.NewPassword)
	if err != nil {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	_, err = h.d.DB.Exec(ctx, `UPDATE "User" SET "password"=$2 WHERE "id"=$1`, u.Id, newHash)
	if err != nil {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}

	// 清除其他会话
	currentToken := middleware.SessionTokenFrom(r)
	_, _ = h.d.DB.Exec(ctx, `DELETE FROM "UserSession" WHERE "userId"=$1 AND "token"<>$2`, u.Id, currentToken)

	util.WriteJSON(w, 200, map[string]any{"success": true, "message": "密码修改成功"})
}

// ============ 重置密码 ============

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account     string `json:"account"`
		Code        string `json:"code"`
		NewPassword string `json:"newPassword"`
		Channel     string `json:"channel"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Channel == "" {
		req.Channel = "sms"
	}
	ctx := r.Context()

	if req.Account == "" || req.Code == "" || req.NewPassword == "" {
		util.WriteError(w, 400, "请填写完整信息")
		return
	}
	// ★ 强密码策略检查
	if pw := checkPasswordStrength(req.NewPassword); !pw.valid {
		util.WriteError(w, 400, pw.err)
		return
	}

	// 验证验证码
	verifyResult := h.verifyCode(ctx, req.Account, req.Code, "reset", req.Channel)
	if !verifyResult.valid {
		util.WriteError(w, 400, verifyResult.err)
		return
	}

	// 查找用户
	var user *db.User
	var err error
	if req.Channel == "sms" {
		user, err = db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "phone"=$1`, req.Account)
	} else {
		user, err = db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "email"=$1`, req.Account)
	}
	if err != nil && !db.IsNotFound(err) {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	if user == nil {
		util.WriteError(w, 404, "账号不存在")
		return
	}

	newHash, err := util.HashPassword(req.NewPassword)
	if err != nil {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	_, _ = h.d.DB.Exec(ctx, `UPDATE "User" SET "password"=$2 WHERE "id"=$1`, user.Id, newHash)

	// 清除所有会话
	_, _ = h.d.DB.Exec(ctx, `DELETE FROM "UserSession" WHERE "userId"=$1`, user.Id)

	util.WriteJSON(w, 200, map[string]any{"success": true, "message": "密码重置成功，请重新登录"})
}

// ============ 绑定手机号 ============

func (h *Handler) bindPhone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	u := middleware.UserFrom(r)
	if u == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	ctx := r.Context()

	if req.Phone == "" || req.Code == "" {
		util.WriteError(w, 400, "请填写手机号和验证码")
		return
	}
	if !isValidPhone(req.Phone) {
		util.WriteError(w, 400, "手机号格式不正确")
		return
	}

	// 检查手机号是否已被使用
	existing, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "phone"=$1`, req.Phone)
	if err != nil && !db.IsNotFound(err) {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	if existing != nil && existing.Id != u.Id {
		util.WriteError(w, 409, "该手机号已被其他账号绑定")
		return
	}

	// 验证验证码
	verifyResult := h.verifyCode(ctx, req.Phone, req.Code, "bind", "sms")
	if !verifyResult.valid {
		util.WriteError(w, 400, verifyResult.err)
		return
	}

	_, _ = h.d.DB.Exec(ctx, `UPDATE "User" SET "phone"=$2, "phoneVerified"=true WHERE "id"=$1`, u.Id, req.Phone)

	util.WriteJSON(w, 200, map[string]any{"success": true, "message": "手机号绑定成功"})
}

// ============ 绑定邮箱 ============

func (h *Handler) bindEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	u := middleware.UserFrom(r)
	if u == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	ctx := r.Context()

	if req.Email == "" || req.Code == "" {
		util.WriteError(w, 400, "请填写邮箱和验证码")
		return
	}
	if !isValidEmail(req.Email) {
		util.WriteError(w, 400, "邮箱格式不正确")
		return
	}

	existing, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "email"=$1`, req.Email)
	if err != nil && !db.IsNotFound(err) {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	if existing != nil && existing.Id != u.Id {
		util.WriteError(w, 409, "该邮箱已被其他账号绑定")
		return
	}

	verifyResult := h.verifyCode(ctx, req.Email, req.Code, "bind", "email")
	if !verifyResult.valid {
		util.WriteError(w, 400, verifyResult.err)
		return
	}

	_, _ = h.d.DB.Exec(ctx, `UPDATE "User" SET "email"=$2, "emailVerified"=true WHERE "id"=$1`, u.Id, req.Email)

	util.WriteJSON(w, 200, map[string]any{"success": true, "message": "邮箱绑定成功"})
}

// ============ 更新个人资料 ============

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nickname      *string `json:"nickname"`
		Avatar        *string `json:"avatar"`
		BackgroundURL *string `json:"backgroundUrl"`
		Bio           *string `json:"bio"`
		Username      *string `json:"username"`
		Gender        *string `json:"gender"`
		Region        *string `json:"region"`
		Birthday      *string `json:"birthday"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	u := middleware.UserFrom(r)
	if u == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	ctx := r.Context()
	token := middleware.SessionTokenFrom(r)

	sets := []string{}
	args := []any{}
	add := func(col string, v any) {
		sets = append(sets, `"`+col+`"=$`+strconv.Itoa(len(args)+1))
		args = append(args, v)
	}
	if req.Nickname != nil {
		add("nickname", *req.Nickname)
	}
	if req.Avatar != nil {
		add("avatar", *req.Avatar)
	}
	if req.BackgroundURL != nil {
		add("backgroundUrl", nullIfEmpty(*req.BackgroundURL))
	}
	if req.Bio != nil {
		add("bio", *req.Bio)
	}
	if req.Gender != nil {
		add("gender", nullIfEmpty(*req.Gender))
	}
	if req.Region != nil {
		add("region", nullIfEmpty(*req.Region))
	}
	if req.Birthday != nil {
		add("birthday", nullIfEmpty(*req.Birthday))
	}

	// 支持修改 username（账号ID），需校验唯一性
	if req.Username != nil && *req.Username != u.Username {
		if !usernameRe.MatchString(*req.Username) {
			util.WriteError(w, 400, "账号ID只能包含字母、数字和下划线，长度1-20位")
			return
		}
		dup, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "username"=$1`, *req.Username)
		if err != nil && !db.IsNotFound(err) {
			util.WriteError(w, 500, "资料更新失败")
			return
		}
		if dup != nil && dup.Id != u.Id {
			util.WriteError(w, 409, "该账号ID已被使用")
			return
		}
		add("username", *req.Username)
	}

	if len(sets) > 0 {
		args = append(args, u.Id)
		_, err := h.d.DB.Exec(ctx,
			`UPDATE "User" SET `+joinStrings(sets, ", ")+` WHERE "id"=$`+strconv.Itoa(len(args)), args...)
		if err != nil {
			if isUniqueViolation(err) {
				util.WriteError(w, 409, "该账号ID已被使用")
			} else {
				util.WriteError(w, 500, "资料更新失败")
			}
			return
		}
	}

	updated, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "id"=$1`, u.Id)
	if err != nil || updated == nil {
		util.WriteError(w, 500, "资料更新失败")
		return
	}

	// 资料更新后清除 session 缓存，确保 /me 等接口返回最新字段
	if token != "" {
		h.d.Auth.InvalidateSession(ctx, token)
	}

	// ★ 实时同步：使用数据库 updatedAt 作为版本时间戳
	h.publishUserProfileUpdated(ctx, updated)

	nickname := util.StrVal(updated.Nickname)
	if nickname == "" {
		nickname = updated.Username
	}
	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"user": map[string]any{
			"id":            updated.Id,
			"username":      updated.Username,
			"nickname":      util.StrVal(updated.Nickname),
			"phone":         util.StrVal(updated.Phone),
			"email":         util.StrVal(updated.Email),
			"avatar":        avatarToProxy(util.StrVal(updated.Avatar)),
			"backgroundUrl": util.StrVal(updated.BackgroundUrl),
			"bio":           util.StrVal(updated.Bio),
			"gender":        util.StrVal(updated.Gender),
			"region":        util.StrVal(updated.Region),
			"birthday":      util.StrVal(updated.Birthday),
			"phoneVerified": updated.PhoneVerified,
			"emailVerified": updated.EmailVerified,
			"updatedAt":     updated.UpdatedAt.UnixMilli(),
		},
		// 兼容 ProfileSettingsPage 的 profile 格式
		"profile": map[string]any{
			"id":            updated.Id,
			"username":      updated.Username,
			"wechatId":      updated.Username,
			"name":          nickname,
			"nickname":      nickname,
			"avatar":        avatarToProxy(util.StrVal(updated.Avatar)),
			"backgroundUrl": util.StrVal(updated.BackgroundUrl),
			"bio":           util.StrVal(updated.Bio),
			"phone":         util.StrVal(updated.Phone),
			"email":         util.StrVal(updated.Email),
			"gender":        util.StrVal(updated.Gender),
			"region":        util.StrVal(updated.Region),
			"birthday":      util.StrVal(updated.Birthday),
			"updatedAt":     updated.UpdatedAt.UnixMilli(),
		},
	})
}

// ============ 会话管理 ============

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	if u == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	ctx := r.Context()
	currentToken := middleware.SessionTokenFrom(r)

	sessions, err := db.QueryToStructs[db.UserSession](ctx, h.d.DB,
		`SELECT * FROM "UserSession" WHERE "userId"=$1 ORDER BY "createdAt" DESC`, u.Id)
	if err != nil {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	list := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		list = append(list, map[string]any{
			"id":        s.Id,
			"device":    util.StrVal(s.Device),
			"ip":        util.StrVal(s.Ip),
			"location":  util.StrVal(s.Location),
			"createdAt": s.CreatedAt,
			"isCurrent": s.Token == currentToken,
		})
	}
	util.WriteJSON(w, 200, map[string]any{"sessions": list})
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	if u == nil {
		util.WriteError(w, 401, "未登录")
		return
	}
	id := r.PathValue("id")
	_, _ = h.d.DB.Exec(r.Context(), `DELETE FROM "UserSession" WHERE "id"=$1 AND "userId"=$2`, id, u.Id)
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============ 检查账号是否存在 ============

func (h *Handler) checkAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account string `json:"account"`
		Type    string `json:"type"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()

	if req.Account == "" {
		util.WriteError(w, 400, "请输入账号")
		return
	}

	var col string
	switch req.Type {
	case "phone":
		col = "phone"
	case "email":
		col = "email"
	default:
		col = "username"
	}
	exists := false
	u, err := db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "`+col+`"=$1`, req.Account)
	if err == nil && u != nil {
		exists = true
	}
	util.WriteJSON(w, 200, map[string]any{"exists": exists})
}

// ============ 小工具 ============

// isUniqueViolation 判断是否为 PG 唯一约束冲突（对应 Prisma P2002）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func joinStrings(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
