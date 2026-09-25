// Package admin 移植自 server/admin.ts —— 管理后台 API（挂载在 /api/admin）。
//
// 覆盖 74 个路由：管理员登录/登出/session、仪表盘、用户管理、动态/媒体/举报/
// 敏感词/IP黑名单/非法请求/登录日志/公告/操作日志/管理员账号/站点与各类
// 第三方配置（COS/pyq同步/OneBot/SMTP/阿里云/腾讯地图）。
//
// 安全行为与 Node 版一致：全部路由（除登录外）包 AdminAuth；按角色
// （superadmin/admin/moderator）用 RequireAdminRole 区分；全部路由包
// CsrfGenerate + CsrfVerify；登录走 AdminLoginRateLimit，成功后
// ResetAdminLoginLimits。
package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// Handler 管理后台 handler。
type Handler struct {
	deps *handler.Deps
}

func (h *Handler) db() *db.DB                          { return h.deps.DB }
func (h *Handler) ctx(r *http.Request) context.Context { return r.Context() }

// ============================================================
// 路由注册
// ============================================================

// adminAuth 包装：AdminAuth + 可选角色校验。
func (h *Handler) adminAuth(roles ...string) func(http.Handler) http.Handler {
	if len(roles) == 0 {
		return h.deps.Auth.AdminAuth
	}
	rr := middleware.RequireAdminRole(roles...)
	return func(next http.Handler) http.Handler {
		return h.deps.Auth.AdminAuth(rr(next))
	}
}

// handle 注册一条路由：CSRF 生成+校验 → 业务中间件 → handler。
// （与 index.ts 中 csrfTokenGenerate/csrfTokenVerify 挂载在 /api/admin 的顺序一致）
func (h *Handler) handle(mux *http.ServeMux, pattern string, mw func(http.Handler) http.Handler, fn func(http.ResponseWriter, *http.Request)) {
	var hd http.Handler = http.HandlerFunc(fn)
	hd = mw(hd)
	hd = middleware.CsrfVerify(middleware.CsrfGenerate(hd))
	mux.Handle(pattern, hd)
}

// RegisterRoutes 注册管理后台全部路由（挂载在 /api/admin）。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := &Handler{deps: d}
	auth := h.adminAuth()
	sa := h.adminAuth("superadmin", "admin")               // 超级管理员 / 管理员
	mod := h.adminAuth("superadmin", "admin", "moderator") // 含审核员
	super := h.adminAuth("superadmin")                     // 仅超级管理员
	loginMW := func(next http.Handler) http.Handler {      // 登录限流（无需登录态）
		return middleware.AdminLoginRateLimit(d.Cfg, d.DB, next)
	}

	// ===== 认证 =====
	h.handle(mux, "POST /api/admin/login", loginMW, h.login)
	h.handle(mux, "POST /api/admin/logout", auth, h.logout)
	h.handle(mux, "GET /api/admin/me", auth, h.me)

	// ===== 仪表盘 =====
	h.handle(mux, "GET /api/admin/dashboard", auth, h.dashboard)

	// ===== 用户管理 =====
	h.handle(mux, "GET /api/admin/users", auth, h.listUsers)
	h.handle(mux, "POST /api/admin/users/create", sa, h.createUser)
	h.handle(mux, "GET /api/admin/users/{id}", auth, h.getUser)
	h.handle(mux, "PUT /api/admin/users/{id}", sa, h.updateUser)
	h.handle(mux, "POST /api/admin/users/{id}/ban", sa, h.banUser)
	h.handle(mux, "DELETE /api/admin/users/{id}", sa, h.deleteUser)

	// ===== 动态管理 =====
	h.handle(mux, "GET /api/admin/moments", auth, h.listMoments)
	h.handle(mux, "POST /api/admin/moments/{id}/pin", sa, h.pinMoment)
	h.handle(mux, "DELETE /api/admin/moments", sa, h.deleteMomentsBatch)
	h.handle(mux, "DELETE /api/admin/moments/{id}", sa, h.deleteMoment)

	// ===== 媒体管理 =====
	h.handle(mux, "GET /api/admin/media", auth, h.listMedia)
	h.handle(mux, "DELETE /api/admin/media/{id}", sa, h.deleteMedia)

	// ===== 举报管理 =====
	h.handle(mux, "GET /api/admin/reports", auth, h.listReports)
	h.handle(mux, "POST /api/admin/reports/{id}/resolve", mod, h.resolveReport)
	h.handle(mux, "POST /api/admin/reports/{id}/dismiss", mod, h.dismissReport)

	// ===== 敏感词 =====
	h.handle(mux, "GET /api/admin/sensitive-words", auth, h.listSensitiveWords)
	h.handle(mux, "POST /api/admin/sensitive-words", sa, h.createSensitiveWord)
	h.handle(mux, "PUT /api/admin/sensitive-words/{id}", sa, h.updateSensitiveWord)
	h.handle(mux, "DELETE /api/admin/sensitive-words/{id}", sa, h.deleteSensitiveWord)

	// ===== IP 黑名单 =====
	h.handle(mux, "GET /api/admin/ip-blacklist", auth, h.listIPBlacklist)
	h.handle(mux, "POST /api/admin/ip-blacklist", sa, h.createIPBlacklist)
	h.handle(mux, "DELETE /api/admin/ip-blacklist/{id}", sa, h.deleteIPBlacklist)

	// ===== 非法请求日志 =====
	h.handle(mux, "GET /api/admin/illegal-requests", auth, h.listIllegalRequests)
	h.handle(mux, "DELETE /api/admin/illegal-requests", sa, h.clearIllegalRequests)

	// ===== 登录日志 =====
	h.handle(mux, "GET /api/admin/login-logs", auth, h.listLoginLogs)
	h.handle(mux, "DELETE /api/admin/login-logs", sa, h.clearLoginLogs)

	// ===== 公告 =====
	h.handle(mux, "GET /api/admin/announcements", auth, h.listAnnouncements)
	h.handle(mux, "POST /api/admin/announcements", sa, h.createAnnouncement)
	h.handle(mux, "PUT /api/admin/announcements/{id}", sa, h.updateAnnouncement)
	h.handle(mux, "DELETE /api/admin/announcements/{id}", sa, h.deleteAnnouncement)

	// ===== 操作日志 =====
	h.handle(mux, "GET /api/admin/logs", auth, h.listLogs)

	// ===== 管理员账号（仅 superadmin） =====
	h.handle(mux, "GET /api/admin/admins", super, h.listAdmins)
	h.handle(mux, "POST /api/admin/admins", super, h.createAdmin)
	h.handle(mux, "PUT /api/admin/admins/{id}/password", super, h.updateAdminPassword)
	h.handle(mux, "DELETE /api/admin/admins/{id}", super, h.deleteAdmin)

	// ===== 站点设置 =====
	h.handle(mux, "GET /api/admin/site-config", auth, h.getSiteConfig)
	h.handle(mux, "PUT /api/admin/site-config", sa, h.updateSiteConfig)

	// ===== COS 配置 =====
	h.handle(mux, "GET /api/admin/cos-config", auth, h.getCosConfig)
	h.handle(mux, "PUT /api/admin/cos-config", sa, h.updateCosConfig)
	h.handle(mux, "POST /api/admin/cos-config/test", sa, h.testCosConfig)
	h.handle(mux, "POST /api/admin/cos-config/init-dirs", sa, h.initCosDirs)
	h.handle(mux, "GET /api/admin/cos-config/usage", auth, h.cosUsage)

	// ===== pyq 同步配置 =====
	h.handle(mux, "GET /api/admin/sync-config", auth, h.getSyncConfig)
	h.handle(mux, "PUT /api/admin/sync-config", sa, h.updateSyncConfig)
	h.handle(mux, "POST /api/admin/sync-config/test", sa, h.testSyncConfig)

	// ===== OneBot 配置 =====
	h.handle(mux, "GET /api/admin/onebot/config", auth, h.getOnebotConfig)
	h.handle(mux, "PUT /api/admin/onebot/config", sa, h.updateOnebotConfig)
	h.handle(mux, "GET /api/admin/onebot/status", auth, h.onebotStatus)
	h.handle(mux, "GET /api/admin/onebot/auto-replies", auth, h.listAutoReplies)
	h.handle(mux, "POST /api/admin/onebot/auto-replies", sa, h.createAutoReply)
	h.handle(mux, "PUT /api/admin/onebot/auto-replies/{id}", sa, h.updateAutoReply)
	h.handle(mux, "DELETE /api/admin/onebot/auto-replies/{id}", sa, h.deleteAutoReply)
	h.handle(mux, "GET /api/admin/onebot/ai-config", auth, h.getAIConfig)
	h.handle(mux, "PUT /api/admin/onebot/ai-config", sa, h.updateAIConfig)

	// ===== SMTP 配置 =====
	h.handle(mux, "GET /api/admin/smtp/config", auth, h.getSmtpConfig)
	h.handle(mux, "PUT /api/admin/smtp/config", sa, h.updateSmtpConfig)
	h.handle(mux, "GET /api/admin/smtp/templates", auth, h.getSmtpTemplates)
	h.handle(mux, "PUT /api/admin/smtp/templates", sa, h.updateSmtpTemplates)
	h.handle(mux, "POST /api/admin/smtp/test", sa, h.testSmtp)
	h.handle(mux, "GET /api/admin/smtp/test-history", auth, h.smtpTestHistory)

	// ===== 自定义外链页面 =====
	h.handle(mux, "GET /api/admin/custom-pages", auth, h.listCustomPages)
	h.handle(mux, "POST /api/admin/custom-pages", sa, h.createCustomPage)
	h.handle(mux, "PUT /api/admin/custom-pages/{id}", sa, h.updateCustomPage)
	h.handle(mux, "DELETE /api/admin/custom-pages/{id}", sa, h.deleteCustomPage)

	// ===== 阿里云配置（短信/号码认证） =====
	h.handle(mux, "GET /api/admin/aliyun-config", auth, h.getAliyunConfig)
	h.handle(mux, "PUT /api/admin/aliyun-config", sa, h.updateAliyunConfig)
	h.handle(mux, "POST /api/admin/aliyun-sms-test", sa, h.testAliyunSms)
	h.handle(mux, "GET /api/admin/aliyun-sms-test-history", auth, h.aliyunSmsTestHistory)

	// ===== 腾讯位置服务 =====
	h.handle(mux, "GET /api/admin/txmap-config", auth, h.getTxmapConfig)
	h.handle(mux, "PUT /api/admin/txmap-config", sa, h.updateTxmapConfig)
}

// ============================================================
// 通用辅助
// ============================================================

func (h *Handler) clientIP(r *http.Request) string {
	return middleware.GetClientIP(r, h.deps.Cfg.TrustProxy)
}

func strOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// addLog 写管理员操作日志（失败不阻断主流程）。
func (h *Handler) addLog(ctx context.Context, adminID, adminName, action, target, detail, ip string) {
	_, err := h.db().Exec(ctx,
		`INSERT INTO "AdminLog"("id","adminId","adminName","action","target","detail","ip","createdAt")
		 VALUES($1,$2,$3,$4,$5,$6,$7,NOW())`,
		util.NewID(), strOrNil(adminID), strOrNil(adminName), action, strOrNil(target), strOrNil(detail), strOrNil(ip))
	if err != nil {
		log.Printf("[Admin] 操作日志写入失败: %v", err)
	}
}

// addFailedLoginLog 写登录失败日志（失败静默，与 TS 一致）。
func (h *Handler) addFailedLoginLog(ctx context.Context, adminName, detail, ip string) {
	_, err := h.db().Exec(ctx,
		`INSERT INTO "AdminLog"("id","adminId","adminName","action","target","detail","ip","createdAt")
		 VALUES($1,NULL,$2,'登录失败','system',$3,$4,NOW())`,
		util.NewID(), adminName, detail, ip)
	if err != nil {
		log.Printf("[Admin] 登录失败日志写入 PG 失败: %v", err)
	}
}

// ============================================================
// 输入安全检查（移植自 security.ts containsDangerousInput）
// ============================================================

var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)<script\b`),
	regexp.MustCompile(`(?i)javascript:`),
	regexp.MustCompile(`(?i)on\w+\s*=`),
	regexp.MustCompile(`(?i)union\s+select`),
	regexp.MustCompile(`(?i);\s*drop\s+`),
	regexp.MustCompile(`(?i);\s*delete\s+`),
	regexp.MustCompile(`(?i)'\s*or\s+'1`),
	regexp.MustCompile(`--\s*$`),
}

// containsDangerousInput 检查输入是否包含危险字符（SQL 注入、XSS 载荷）。
func containsDangerousInput(s string) bool {
	for _, p := range dangerousPatterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}

// ============================================================
// 密码（bcrypt + 旧版 SHA-256 兼容，与 db.ts 一致）
// ============================================================

var legacyHashRe = regexp.MustCompile(`^[a-f0-9]{64}$`)

// legacyHashPassword 旧版 SHA-256 哈希：sha256(password + 'cqim_salt_2024')。
func legacyHashPassword(password string) string {
	sum := sha256.Sum256([]byte(password + "cqim_salt_2024"))
	return hex.EncodeToString(sum[:])
}

// isLegacyHash 检查是否为旧版 64 位十六进制哈希。
func isLegacyHash(hash string) bool { return legacyHashRe.MatchString(hash) }

// verifyAdminPassword 验证密码：兼容 bcrypt 与旧版 SHA-256。
func verifyAdminPassword(password, hash string) bool {
	if hash == "" {
		return false
	}
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") {
		return util.VerifyPassword(password, hash)
	}
	if legacyHashRe.MatchString(hash) {
		return legacyHashPassword(password) == hash
	}
	return false
}

var (
	lowerRe    = regexp.MustCompile(`[a-z]`)
	upperRe    = regexp.MustCompile(`[A-Z]`)
	digitRe    = regexp.MustCompile(`[0-9]`)
	specialRe  = regexp.MustCompile(`[!@#$%^&*]`)
	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,20}$`)
)

// truncateUA 截断 UA 到 100 个字符（与 TS ua.slice(0, 100) 一致）。
func truncateUA(ua string) string {
	r := []rune(ua)
	if len(r) > 100 {
		return string(r[:100])
	}
	return ua
}

// ============================================================
// 系统配置 KV（SystemConfig 表，移植自 admin.ts getConfig/setConfig）
// ============================================================

// getAdminConfigKV 读配置键，不存在返回 nil。
func (h *Handler) getAdminConfigKV(ctx context.Context, key string) any {
	var raw string
	err := h.db().Pool.QueryRow(ctx, `SELECT "value" FROM "SystemConfig" WHERE "key"=$1`, key).Scan(&raw)
	if err != nil {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil
	}
	return v
}

// setAdminConfigKV 写配置键（upsert）。
func (h *Handler) setAdminConfigKV(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = h.db().Exec(ctx,
		`INSERT INTO "SystemConfig"("key","value","updatedAt") VALUES($1,$2,NOW())
		 ON CONFLICT("key") DO UPDATE SET "value"=EXCLUDED."value","updatedAt"=NOW()`,
		key, string(raw))
	return err
}

// getConfigMap 读配置为 map（不存在返回空 map）。
func (h *Handler) getConfigMap(ctx context.Context, key string) map[string]any {
	if v, ok := h.getAdminConfigKV(ctx, key).(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

// GetAdminConfig 导出供其他模块使用（对应 TS export { getConfig as getAdminConfig }）。
func GetAdminConfig(ctx context.Context, d *handler.Deps, key string) any {
	h := &Handler{deps: d}
	return h.getAdminConfigKV(ctx, key)
}

// ============================================================
// 跨模块日志工具（对应 TS export logLogin / logIllegalRequest）
// ============================================================

// LogLogin 写登录日志。
func LogLogin(ctx context.Context, d *handler.Deps, data map[string]any) {
	str := func(k string) any {
		if v, ok := data[k].(string); ok && v != "" {
			return v
		}
		return nil
	}
	_, _ = d.DB.Exec(ctx,
		`INSERT INTO "LoginLog"("id","userId","username","email","phone","ip","userAgent","success","failReason","loginType","createdAt")
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW())`,
		util.NewID(), str("userId"), str("username"), str("email"), str("phone"),
		str("ip"), str("userAgent"), data["success"] == true, str("failReason"), str("loginType"))
}

// LogIllegalRequest 写非法请求日志。
func LogIllegalRequest(ctx context.Context, d *handler.Deps, ip, path, method, userAgent, reason string, statusCode int) {
	var sc any
	if statusCode != 0 {
		sc = statusCode
	}
	_, _ = d.DB.Exec(ctx,
		`INSERT INTO "IllegalRequest"("id","ip","path","method","userAgent","reason","statusCode","createdAt")
		 VALUES($1,$2,$3,$4,$5,$6,$7,NOW())`,
		util.NewID(), strOrNil(ip), strOrNil(path), strOrNil(method), strOrNil(userAgent), strOrNil(reason), sc)
}

// ============================================================
// S13：首次启动创建默认管理员（移植自 db.ts initDatabase）
// ============================================================

// EnsureDefaultAdmin 管理员表为空时创建默认 admin 账号：
// 优先读取 ADMIN_INITIAL_PASSWORD，未设置则生成 24 位随机口令并在日志一次性打印。
// 返回 (是否创建, 初始密码)。
func EnsureDefaultAdmin(ctx context.Context, d *handler.Deps) (bool, string) {
	var count int64
	if err := d.DB.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM "AdminAccount"`).Scan(&count); err != nil {
		log.Printf("[DB] 检查管理员账号失败: %v", err)
		return false, ""
	}
	if count > 0 {
		return false, ""
	}
	envPassword := strings.TrimSpace(d.Cfg.AdminInitialPassword)
	initialPassword := envPassword
	if initialPassword == "" {
		initialPassword = util.GenerateToken(24)
	}
	hash, err := util.HashPassword(initialPassword)
	if err != nil {
		log.Printf("[DB] 创建默认管理员账号失败: %v", err)
		return false, ""
	}
	if _, err := d.DB.Exec(ctx,
		`INSERT INTO "AdminAccount"("id","username","password","role","createdAt","updatedAt")
		 VALUES($1,'admin',$2,'superadmin',NOW(),NOW())`,
		util.NewID(), hash); err != nil {
		log.Printf("[DB] 创建默认管理员账号失败: %v", err)
		return false, ""
	}
	if envPassword != "" {
		log.Println("[DB] 已创建默认管理员账号: admin（密码来自 ADMIN_INITIAL_PASSWORD）")
	} else {
		log.Println("[DB] 已创建默认管理员账号: admin")
		log.Printf("[DB] ★★★ 初始管理员密码（仅显示一次，请立即保存并登录后修改）: %s", initialPassword)
	}
	return true, initialPassword
}

// hmacSHA1 供 COS/Aliyun 签名使用。
func hmacSHA1(key []byte, data string) []byte {
	m := hmac.New(sha1.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// adminDomain 读取 ADMIN_DOMAIN（登录 cookie domain）。
func adminDomain() string { return os.Getenv("ADMIN_DOMAIN") }
