package middleware

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/config"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/redisx"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// 上下文键
type ctxKey string

const (
	CtxUser         ctxKey = "user"
	CtxAdmin        ctxKey = "admin"
	CtxAdminToken   ctxKey = "adminToken"
	CtxSessionToken ctxKey = "sessionToken"
)

// UserFrom 从请求上下文取当前用户。
func UserFrom(r *http.Request) *db.User {
	if u, ok := r.Context().Value(CtxUser).(*db.User); ok {
		return u
	}
	return nil
}

// AdminFrom 从请求上下文取当前管理员。
func AdminFrom(r *http.Request) *db.AdminAccount {
	if a, ok := r.Context().Value(CtxAdmin).(*db.AdminAccount); ok {
		return a
	}
	return nil
}

// SessionTokenFrom 取当前会话 token。
func SessionTokenFrom(r *http.Request) string {
	if s, ok := r.Context().Value(CtxSessionToken).(string); ok {
		return s
	}
	return ""
}

// ============================================================
// 客户端 IP（修复审计项：仅在 TRUST_PROXY 时信任转发头）
// ============================================================

// GetClientIP 获取客户端真实 IP。trustProxy 为 false 时只用连接远端地址，
// 防止攻击者伪造 X-Forwarded-For 绕过按 IP 限流。
func GetClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if v := r.Header.Get("Cf-Connecting-Ip"); v != "" {
			return cleanIP(v)
		}
		if v := r.Header.Get("X-Real-Ip"); v != "" {
			return cleanIP(v)
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			return cleanIP(strings.Split(v, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return cleanIP(r.RemoteAddr)
	}
	return cleanIP(host)
}

func cleanIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if strings.Contains(ip, "::ffff:") {
		ip = strings.Split(ip, "::ffff:")[1]
	}
	if ip == "" {
		return "unknown"
	}
	return ip
}

// ============================================================
// 安全响应头（与 security.ts 一致）
// ============================================================

func SecurityHeaders(cfg *config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(self), microphone=(self), geolocation=(self), payment=()")
		scriptSrc := "script-src 'self' 'unsafe-inline'"
		if !cfg.IsProduction() {
			scriptSrc += " 'unsafe-eval'"
		}
		w.Header().Set("Content-Security-Policy", strings.Join([]string{
			"default-src 'self'", scriptSrc,
			"style-src 'self' 'unsafe-inline' https://cdn.bootcdn.net",
			"font-src 'self' https://cdn.bootcdn.net data:",
			"img-src 'self' data: blob: https:",
			"connect-src 'self' ws: wss: https:",
			"media-src 'self' blob: https:",
			"object-src 'none'", "frame-ancestors 'none'",
			"base-uri 'self'", "form-action 'self'",
		}, "; "))
		p := r.URL.Path
		isStatic := strings.HasPrefix(p, "/api/stickers/files/") || strings.HasPrefix(p, "/api/media/files/")
		if strings.HasPrefix(p, "/api/") && !isStatic {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			w.Header().Set("Pragma", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// ============================================================
// 速率限制（与 security.ts 同参数）
// ============================================================

type limitEntry struct {
	count   int
	resetAt time.Time
}

// RateLimiter 固定窗口计数器。
type RateLimiter struct {
	mu     sync.Mutex
	store  map[string]*limitEntry
	window time.Duration
	max    int
	stopCh chan struct{}
}

func NewRateLimiter(window time.Duration, max int) *RateLimiter {
	rl := &RateLimiter{store: make(map[string]*limitEntry), window: window, max: max, stopCh: make(chan struct{})}
	go func() {
		t := time.NewTicker(window)
		if window > time.Minute {
			t = time.NewTicker(time.Minute)
		}
		defer t.Stop()
		for {
			select {
			case <-t.C:
				now := time.Now()
				rl.mu.Lock()
				for k, e := range rl.store {
					if now.After(e.resetAt) {
						delete(rl.store, k)
					}
				}
				rl.mu.Unlock()
			case <-rl.stopCh:
				return
			}
		}
	}()
	return rl
}

type limitResult struct {
	allowed   bool
	remaining int
	resetAt   time.Time
}

func (rl *RateLimiter) check(key string) limitResult {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	e, ok := rl.store[key]
	if !ok || now.After(e.resetAt) {
		e = &limitEntry{resetAt: now.Add(rl.window)}
		rl.store[key] = e
	}
	e.count++
	allowed := e.count <= rl.max
	remaining := rl.max - e.count
	if remaining < 0 {
		remaining = 0
	}
	return limitResult{allowed: allowed, remaining: remaining, resetAt: e.resetAt}
}

func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	delete(rl.store, key)
	rl.mu.Unlock()
}

var (
	globalLimiter  = NewRateLimiter(time.Minute, 120)
	loginLimiter   = NewRateLimiter(15*time.Minute, 10)
	accountLimiter = NewRateLimiter(15*time.Minute, 5)
	codeLimiter    = NewRateLimiter(time.Hour, 10)
	adminLimiter   = NewRateLimiter(15*time.Minute, 5)
)

// recordIllegal 记异常请求（IllegalRequest 表），失败静默。
func recordIllegal(d *db.DB, ip, path, reason, ua string) {
	if d == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = d.Exec(ctx,
		`INSERT INTO "IllegalRequest"("id","ip","path","reason","userAgent","createdAt") VALUES($1,$2,$3,$4,$5,NOW())`,
		util.NewID(), ip, path, reason, ua)
}

// GlobalRateLimit 全局限流：每 IP 每分钟 120 次。
func GlobalRateLimit(cfg *config.Config, d *db.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := GetClientIP(r, cfg.TrustProxy)
		res := globalLimiter.check("global:" + ip)
		w.Header().Set("X-RateLimit-Limit", "120")
		w.Header().Set("X-RateLimit-Remaining", itoa(res.remaining))
		w.Header().Set("X-RateLimit-Reset", itoa(int(res.resetAt.Unix())))
		if !res.allowed {
			recordIllegal(d, ip, r.URL.Path, "rate_limit_exceeded", r.UserAgent())
			util.WriteJSON(w, 429, map[string]any{
				"error":      "请求过于频繁，请稍后再试",
				"retryAfter": int(time.Until(res.resetAt).Seconds()) + 1,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LoginRateLimit 登录限流：IP 15分钟10次 + 账号 15分钟5次。
func LoginRateLimit(cfg *config.Config, d *db.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := GetClientIP(r, cfg.TrustProxy)
		res := loginLimiter.check("login:ip:" + ip)
		if !res.allowed {
			recordIllegal(d, ip, r.URL.Path, "login_brute_force", r.UserAgent())
			util.WriteJSON(w, 429, map[string]any{
				"error":      "登录尝试过于频繁，请 15 分钟后再试",
				"retryAfter": int(time.Until(res.resetAt).Seconds()) + 1,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ResetLoginLimits 登录成功后重置计数。
func ResetLoginLimits(ip, account string) {
	loginLimiter.Reset("login:ip:" + ip)
	accountLimiter.Reset("login:account:" + account)
}

// CheckAccountLoginLimit 按账号限流（登录 handler 内调用）。
func CheckAccountLoginLimit(account string) (bool, int) {
	res := accountLimiter.check("login:account:" + account)
	return res.allowed, int(time.Until(res.resetAt).Seconds()) + 1
}

// CodeRateLimit 验证码发送限流：每 IP 每小时 10 次。
func CodeRateLimit(cfg *config.Config, d *db.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := GetClientIP(r, cfg.TrustProxy)
		res := codeLimiter.check("code:" + ip)
		if !res.allowed {
			recordIllegal(d, ip, r.URL.Path, "code_spam", r.UserAgent())
			util.WriteJSON(w, 429, map[string]any{
				"error":      "验证码发送过于频繁，请稍后再试",
				"retryAfter": int(time.Until(res.resetAt).Seconds()) + 1,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AdminLoginRateLimit 管理后台登录限流。
func AdminLoginRateLimit(cfg *config.Config, d *db.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := GetClientIP(r, cfg.TrustProxy)
		res := adminLimiter.check("admin:login:" + ip)
		if !res.allowed {
			recordIllegal(d, ip, r.URL.Path, "admin_brute_force", r.UserAgent())
			util.WriteJSON(w, 429, map[string]any{
				"error":      "管理后台登录尝试过于频繁，请 15 分钟后再试",
				"retryAfter": int(time.Until(res.resetAt).Seconds()) + 1,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ResetAdminLoginLimits(ip string) { adminLimiter.Reset("admin:login:" + ip) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ============================================================
// 用户认证（3 级 session 缓存，与 auth.ts 一致）
// ============================================================

const sessionCacheTTL = 60 * time.Second
const sessionCacheMax = 10000

type cachedSession struct {
	user      *db.User
	expiresAt time.Time
	cachedAt  time.Time
}

var sessionMemCache sync.Map // map[string]*cachedSession

type sessionJSON struct {
	User      *db.User  `json:"user"`
	ExpiresAt time.Time `json:"expiresAt"`
	CachedAt  int64     `json:"cachedAt"`
}

// AuthContext 认证依赖。
type AuthContext struct {
	DB    *db.DB
	Redis *redisx.Client
}

func (a *AuthContext) getCachedSession(ctx context.Context, token string) *cachedSession {
	now := time.Now()
	if v, ok := sessionMemCache.Load(token); ok {
		if cs, ok := v.(*cachedSession); ok && now.Sub(cs.cachedAt) < sessionCacheTTL {
			return cs
		}
	}
	// Redis 二级
	if a.Redis != nil {
		if raw, found, _ := a.Redis.GetString(ctx, "session:"+token); found {
			var sj sessionJSON
			if err := json.Unmarshal([]byte(raw), &sj); err == nil && sj.ExpiresAt.After(now) {
				cs := &cachedSession{user: sj.User, expiresAt: sj.ExpiresAt, cachedAt: now}
				sessionMemCache.Store(token, cs)
				return cs
			}
		}
	}
	// DB 三级
	u, err := db.QueryRowToStruct[db.User](ctx, a.DB,
		`SELECT u.* FROM "UserSession" s JOIN "User" u ON u."id"=s."userId" WHERE s."token"=$1`, token)
	if err != nil {
		sessionMemCache.Delete(token)
		if a.Redis != nil {
			_ = a.Redis.Del(ctx, "session:"+token)
		}
		return nil
	}
	var expiresAt time.Time
	_ = a.DB.Pool.QueryRow(ctx, `SELECT "expiresAt" FROM "UserSession" WHERE "token"=$1`, token).Scan(&expiresAt)
	cs := &cachedSession{user: u, expiresAt: expiresAt, cachedAt: now}
	sessionMemCache.Store(token, cs)
	if a.Redis != nil {
		if raw, err := json.Marshal(sessionJSON{User: u, ExpiresAt: expiresAt, CachedAt: now.UnixMilli()}); err == nil {
			_ = a.Redis.SetEX(ctx, "session:"+token, string(raw), sessionCacheTTL)
		}
	}
	return cs
}

// InvalidateSession 使 session 缓存失效（登出/封禁/改密时调用）。
func (a *AuthContext) InvalidateSession(ctx context.Context, token string) {
	sessionMemCache.Delete(token)
	if a.Redis != nil {
		_ = a.Redis.Del(ctx, "session:"+token)
	}
}

// InvalidateUserSessions 使用户所有 session 缓存失效。
func (a *AuthContext) InvalidateUserSessions(ctx context.Context, userID string) {
	// 内存缓存按 token 存，无法按 user 批量删：删 DB 行 + Redis user 键，内存条目靠 TTL 自然过期
	// （与 Node 版 deleteUserSessionCache 语义一致：删 Redis 侧 user 键）
	if a.Redis != nil {
		_ = a.Redis.Del(ctx, "user:session:"+userID)
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// UserAuth 用户认证中间件（401 未登录 / 403 被封禁）。
func (a *AuthContext) UserAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			util.WriteError(w, 401, "未登录")
			return
		}
		cs := a.getCachedSession(r.Context(), token)
		if cs == nil || cs.expiresAt.Before(time.Now()) {
			if cs != nil {
				a.InvalidateSession(r.Context(), token)
				_, _ = a.DB.Exec(r.Context(), `DELETE FROM "UserSession" WHERE "token"=$1`, token)
			}
			util.WriteError(w, 401, "登录已过期")
			return
		}
		if cs.user.IsBanned {
			util.WriteJSON(w, 403, map[string]any{"error": "账号已被封禁", "reason": cs.user.BanReason})
			return
		}
		ctx := context.WithValue(r.Context(), CtxUser, cs.user)
		ctx = context.WithValue(ctx, CtxSessionToken, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth 可选认证：有合法 token 则注入用户，否则直接放行。
func (a *AuthContext) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := bearerToken(r); token != "" {
			if cs := a.getCachedSession(r.Context(), token); cs != nil &&
				cs.expiresAt.After(time.Now()) && !cs.user.IsBanned {
				ctx := context.WithValue(r.Context(), CtxUser, cs.user)
				r = r.WithContext(ctx)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ============================================================
// 管理员认证
// ============================================================

// AdminAuth 管理员认证：Bearer token 或 admin_token cookie。
func (a *AuthContext) AdminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			if c, err := r.Cookie("admin_token"); err == nil {
				token = c.Value
			}
		}
		if token == "" {
			util.WriteError(w, 401, "未登录")
			return
		}
		admin, err := db.QueryRowToStruct[db.AdminAccount](r.Context(), a.DB,
			`SELECT a.* FROM "AdminSession" s JOIN "AdminAccount" a ON a."id"=s."adminId" WHERE s."token"=$1 AND s."expiresAt" > NOW()`, token)
		if err != nil {
			_, _ = a.DB.Exec(r.Context(), `DELETE FROM "AdminSession" WHERE "token"=$1`, token)
			util.WriteError(w, 401, "登录已过期")
			return
		}
		ctx := context.WithValue(r.Context(), CtxAdmin, admin)
		ctx = context.WithValue(ctx, CtxAdminToken, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdminRole 要求管理员角色（super/admin）。
func RequireAdminRole(roles ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a := AdminFrom(r)
			if a == nil || !allowed[a.Role] {
				util.WriteError(w, 403, "权限不足")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ============================================================
// CSRF（双重提交 Cookie，/api/admin 生效，与 security.ts 一致）
// ============================================================

const csrfCookieName = "csrf_token"
const csrfHeaderName = "X-CSRF-Token"

// CsrfGenerate 下发 CSRF token（GET 请求）。
func CsrfGenerate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			token := util.GenerateToken(32)
			http.SetCookie(w, &http.Cookie{
				Name:     csrfCookieName,
				Value:    token,
				Path:     "/api/admin",
				HttpOnly: false, // JS 需读取后放到 header
				SameSite: http.SameSiteStrictMode,
				Secure:   r.TLS != nil,
				MaxAge:   7200,
			})
			w.Header().Set(csrfHeaderName, token)
		}
		next.ServeHTTP(w, r)
	})
}

// CsrfVerify 校验 CSRF token（POST/PUT/DELETE/PATCH）。
func CsrfVerify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
			cookie, err := r.Cookie(csrfCookieName)
			header := r.Header.Get(csrfHeaderName)
			if err != nil || cookie.Value == "" || header == "" || cookie.Value != header {
				util.WriteError(w, 403, "CSRF 校验失败")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ============================================================
// 管理后台 IP 白名单
// ============================================================

// AdminIPWhitelist 检查 ADMIN_IP_WHITELIST（逗号分隔），未配置则放行。
func AdminIPWhitelist(cfg *config.Config, next http.Handler) http.Handler {
	raw := strings.TrimSpace(getenvStr("ADMIN_IP_WHITELIST"))
	var whitelist []string
	if raw != "" {
		for _, p := range strings.Split(raw, ",") {
			if s := strings.TrimSpace(p); s != "" {
				whitelist = append(whitelist, s)
			}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(whitelist) > 0 {
			ip := GetClientIP(r, cfg.TrustProxy)
			ok := false
			for _, a := range whitelist {
				if a == ip {
					ok = true
					break
				}
			}
			if !ok {
				util.WriteError(w, 403, "IP 不在白名单")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func getenvStr(key string) string {
	return os.Getenv(key)
}
