// Package auth 移植自 cqim-app/server/auth.ts：用户认证路由（/api/auth）。
//
// userAuth/optionalAuth 已在 internal/middleware 实现，此处只移植路由 handlers。
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// Handler 认证路由处理器，共享 handler.Deps。
type Handler struct {
	d *handler.Deps
}

// clientIP 取客户端 IP（按配置决定是否信任转发头），照抄 auth.ts getClientIP 意图，
// 以 internal/middleware.GetClientIP 的安全实现为准。
func (h *Handler) clientIP(r *http.Request) string {
	return middleware.GetClientIP(r, h.d.Cfg.TrustProxy)
}

// ============ 格式校验（照抄 auth.ts） ============

var (
	phoneRe    = regexp.MustCompile(`^1[3-9]\d{9}$`)
	emailRe    = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,20}$`)
)

func isValidPhone(s string) bool    { return phoneRe.MatchString(s) }
func isValidEmail(s string) bool    { return emailRe.MatchString(s) }
func isValidUsername(s string) bool { return usernameRe.MatchString(s) }

// ============ 强密码策略（照抄 checkPasswordStrength） ============

type pwCheckResult struct {
	valid bool
	err   string
}

var (
	lowerRe   = regexp.MustCompile(`[a-z]`)
	upperRe   = regexp.MustCompile(`[A-Z]`)
	digitRe   = regexp.MustCompile(`[0-9]`)
	specialRe = regexp.MustCompile("[!@#$%^&*()_+\\-=\\[\\]{};':\"\\\\|,.<>/?~`]")
	legacyRe  = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

var weakPasswords = []string{"password", "12345678", "qwerty123", "admin123", "abc12345"}

func checkPasswordStrength(password string) pwCheckResult {
	if len(password) < 8 {
		return pwCheckResult{false, "密码长度不能少于 8 位"}
	}
	if len(password) > 128 {
		return pwCheckResult{false, "密码长度不能超过 128 位"}
	}
	if !lowerRe.MatchString(password) {
		return pwCheckResult{false, "密码必须包含小写字母"}
	}
	if !upperRe.MatchString(password) {
		return pwCheckResult{false, "密码必须包含大写字母"}
	}
	if !digitRe.MatchString(password) {
		return pwCheckResult{false, "密码必须包含数字"}
	}
	if !specialRe.MatchString(password) {
		return pwCheckResult{false, "密码必须包含特殊字符（如 !@#$%^&*）"}
	}
	lower := strings.ToLower(password)
	for _, w := range weakPasswords {
		if strings.Contains(lower, w) {
			return pwCheckResult{false, "密码过于简单，请使用更复杂的密码"}
		}
	}
	return pwCheckResult{true, ""}
}

// ============ 输入安全检查（照抄 security.ts containsDangerousInput） ============

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

func containsDangerousInput(s string) bool {
	if s == "" {
		return false
	}
	for _, p := range dangerousPatterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}

// ============ 密码兼容（bcrypt + 旧版 SHA-256） ============

// verifyPasswordCompat 与 db.ts verifyPassword 等价：兼容 bcrypt 与旧版
// SHA-256(password + 'cqim_salt_2024') 哈希。util.VerifyPassword 只认 bcrypt，
// 旧版哈希在此处处理，调用方随后按 isLegacyHash 做 bcrypt 升级。
func verifyPasswordCompat(password, hash string) bool {
	if util.VerifyPassword(password, hash) {
		return true
	}
	if legacyRe.MatchString(hash) {
		sum := sha256.Sum256([]byte(password + "cqim_salt_2024"))
		return hex.EncodeToString(sum[:]) == hash
	}
	return false
}

func isLegacyHash(hash string) bool { return legacyRe.MatchString(hash) }

// ============ 创建用户会话（照抄 createSession） ============

var browserRe = regexp.MustCompile(`(Chrome|Firefox|Safari|Edge|MicroMessenger)/([\d.]+)`)

func parseDevice(ua string) string {
	device := "未知设备"
	switch {
	case strings.Contains(ua, "iPhone"):
		device = "iPhone"
	case strings.Contains(ua, "iPad"):
		device = "iPad"
	case strings.Contains(ua, "Android"):
		device = "Android"
	case strings.Contains(ua, "Windows"):
		device = "Windows"
	case strings.Contains(ua, "Mac"):
		device = "Mac"
	case strings.Contains(ua, "Linux"):
		device = "Linux"
	}
	if m := browserRe.FindStringSubmatch(ua); m != nil {
		major := strings.Split(m[2], ".")[0]
		device += " · " + m[1] + " " + major
	}
	return device
}

// createSession 生成 48 字符 base64url token，写入 UserSession 表（30 天过期），
// 并更新用户最后登录信息。逻辑照抄 auth.ts createSession。
func (h *Handler) createSession(ctx context.Context, r *http.Request, userID string) (string, error) {
	token := util.GenerateToken(48)
	ua := r.UserAgent()
	device := parseDevice(ua)
	ip := h.clientIP(r)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if _, err := h.d.DB.Exec(ctx,
		`INSERT INTO "UserSession" ("id","userId","token","device","ip","expiresAt","createdAt") VALUES ($1,$2,$3,$4,$5,$6,NOW())`,
		util.NewID(), userID, token, device, ip, expiresAt); err != nil {
		return "", err
	}
	// 更新用户最后登录信息
	_, _ = h.d.DB.Exec(ctx, `UPDATE "User" SET "lastLoginAt"=NOW(), "lastLoginIp"=$2 WHERE "id"=$1`, userID, ip)
	return token, nil
}

// ============ 6 位验证码（密码学安全随机，照抄 generateVerifyCode） ============

func generateVerifyCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "000000"
	}
	return strconv.FormatInt(n.Int64()+100000, 10)
}

// ============ TG 风格 Dialog ID（照抄 utils/peerId.ts） ============

var (
	dialogMu       sync.Mutex
	dialogLastTS   int64
	dialogSequence int64
)

// generateUserDialogId 为注册用户生成 TG 风格正数 Dialog ID。
// 组合：timestamp(41) | random(4) | sequence(8)，自定义纪元 2024-01-01 UTC。
func generateUserDialogId() string {
	const epochMs = int64(1704067200000)
	dialogMu.Lock()
	defer dialogMu.Unlock()
	now := time.Now().UnixMilli()
	ts := now - epochMs
	if ts == dialogLastTS {
		dialogSequence = (dialogSequence + 1) & 0xFF
		if dialogSequence == 0 {
			// 同一毫秒内序列号溢出，等待下一毫秒（生产中极少发生）
			waitUntil := dialogLastTS + epochMs + 1
			for time.Now().UnixMilli() < waitUntil {
				time.Sleep(time.Millisecond)
			}
			ts = time.Now().UnixMilli() - epochMs
		}
	} else {
		dialogSequence = 0
	}
	dialogLastTS = ts
	rb := make([]byte, 1)
	_, _ = rand.Read(rb)
	random := int64(rb[0] & 0x0F)
	id := (ts << 12) | (random << 8) | dialogSequence
	return strconv.FormatInt(id, 10)
}

// ============ 头像 COS 代理（照抄 cos-signer.ts avatarToProxy） ============

var zhToAsciiSeg = map[string]string{
	"头像":  "avatars",
	"群头像": "group-avatars",
	"朋友圈": "moments",
	"照片":  "photos",
	"视频":  "videos",
}

func isCosURL(raw string) bool {
	if raw == "" || !strings.HasPrefix(raw, "https://") {
		return false
	}
	return strings.Contains(raw, ".cos.") || strings.Contains(raw, ".myqcloud.com") || strings.Contains(raw, "imim.chat")
}

func cosKeyToAlias(cosKey string) string {
	if cosKey == "" {
		return cosKey
	}
	segs := strings.Split(cosKey, "/")
	for i, s := range segs {
		if a, ok := zhToAsciiSeg[s]; ok {
			segs[i] = a
		}
	}
	return strings.Join(segs, "/")
}

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
	cosKey, err := url.PathUnescape(strings.TrimPrefix(u.Path, "/"))
	if err != nil {
		cosKey = strings.TrimPrefix(u.Path, "/")
	}
	aliasKey := cosKeyToAlias(cosKey)
	segs := strings.Split(aliasKey, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/api/cos/proxy/" + strings.Join(segs, "/") + "?imageMogr2/thumbnail/200x200/format/webp/quality/80"
}

// ============ 登录日志（照抄 admin.ts logLogin） ============

type loginLogParams struct {
	userID     string
	username   string
	email      string
	phone      string
	ip         string
	userAgent  string
	success    bool
	failReason string
	loginType  string
}

func (h *Handler) logLogin(ctx context.Context, p loginLogParams) {
	_, _ = h.d.DB.Exec(ctx,
		`INSERT INTO "LoginLog" ("id","userId","username","email","phone","ip","userAgent","success","failReason","loginType","createdAt") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW())`,
		util.NewID(), nullIfEmpty(p.userID), nullIfEmpty(p.username), nullIfEmpty(p.email),
		nullIfEmpty(p.phone), nullIfEmpty(p.ip), nullIfEmpty(p.userAgent),
		p.success, nullIfEmpty(p.failReason), nullIfEmpty(p.loginType))
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ============ 系统配置（照抄 admin.ts getAdminConfig/getConfig） ============

func (h *Handler) getSystemConfig(ctx context.Context, key string) map[string]any {
	row, err := db.QueryRowToStruct[db.SystemConfig](ctx, h.d.DB, `SELECT "key","value","updatedAt" FROM "SystemConfig" WHERE "key"=$1`, key)
	if err != nil || row == nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(row.Value), &m); err != nil {
		return nil
	}
	return m
}

// ============ 用户资料变更实时同步（照抄 user-profile-sync.ts） ============

// publishUserProfileUpdated 广播用户资料更新事件（Redis Pub/Sub），失败不影响主流程。
// 注意：prisma schema 实际使用 Friendship(userA/userB) 表，TS 中的 prisma.friend
// 在当前 schema 下不存在，此处按真实 schema 取好友 ID。
func (h *Handler) publishUserProfileUpdated(ctx context.Context, u *db.User) {
	defer func() { _ = recover() }()
	type idRow struct {
		FriendID string `db:"friendId"`
	}
	friends, _ := db.QueryToStructs[idRow](ctx, h.d.DB,
		`SELECT CASE WHEN "userA"=$1 THEN "userB" ELSE "userA" END AS "friendId" FROM "Friendship" WHERE "userA"=$1 OR "userB"=$1`, u.Id)
	type groupRow struct {
		GroupID string `db:"groupId"`
	}
	groups, _ := db.QueryToStructs[groupRow](ctx, h.d.DB,
		`SELECT "groupId" FROM "GroupMember" WHERE "userId"=$1`, u.Id)
	friendIDs := make([]string, 0, len(friends))
	for _, f := range friends {
		friendIDs = append(friendIDs, f.FriendID)
	}
	groupIDs := make([]string, 0, len(groups))
	for _, g := range groups {
		groupIDs = append(groupIDs, g.GroupID)
	}
	msg := map[string]any{
		"userId":          u.Id,
		"nickname":        util.StrVal(u.Nickname),
		"avatar":          avatarToProxy(util.StrVal(u.Avatar)),
		"username":        u.Username,
		"bio":             util.StrVal(u.Bio),
		"backgroundUrl":   util.StrVal(u.BackgroundUrl),
		"updatedAt":       u.UpdatedAt.UnixMilli(),
		"targetFriendIds": friendIDs,
		"targetGroupIds":  groupIDs,
	}
	payload, _ := json.Marshal(msg)
	_ = h.d.Redis.Publish(ctx, "msg:user_profile_updated", string(payload))
}

// ============ 阿里云短信 / 号码认证 ============

// getAliyunConfig 照抄 auth.ts getAliyunConfig（读 SystemConfig 'aliyun'）。
func (h *Handler) getAliyunConfig(ctx context.Context) map[string]any {
	if cfg := h.getSystemConfig(ctx, "aliyun"); cfg != nil {
		return cfg
	}
	return map[string]any{}
}

func aliyunStr(cfg map[string]any, key string) string {
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return ""
}

func aliyunBool(cfg map[string]any, key string) bool {
	if v, ok := cfg[key].(bool); ok {
		return v
	}
	return false
}

type smsResult struct {
	success bool
	message string
}

// sendAliyunSms 发送短信验证码。
// Go 服务端未集成阿里云 dypnsapi SDK（不引入新依赖），始终走"发送失败"分支，
// 调用方按 auth.ts 原有降级逻辑本地生成验证码，保证 API 契约不变。
func (h *Handler) sendAliyunSms(ctx context.Context, phone, scene string) smsResult {
	cfg := h.getAliyunConfig(ctx)
	if !aliyunBool(cfg, "smsEnabled") || aliyunStr(cfg, "accessKeyId") == "" || aliyunStr(cfg, "accessKeySecret") == "" {
		return smsResult{false, "短信服务未配置"}
	}
	return smsResult{false, "短信服务暂不可用（Go 服务端未集成阿里云 SDK）"}
}

type smsCheckResult struct {
	valid bool
	err   string
}

// checkAliyunSmsCode 校验阿里云管理的验证码（__aliyun__ 占位记录）。
func (h *Handler) checkAliyunSmsCode(ctx context.Context, phone, code string) smsCheckResult {
	cfg := h.getAliyunConfig(ctx)
	if !aliyunBool(cfg, "smsEnabled") || aliyunStr(cfg, "accessKeyId") == "" || aliyunStr(cfg, "accessKeySecret") == "" {
		return smsCheckResult{false, "短信服务未配置"}
	}
	return smsCheckResult{false, "短信服务暂不可用（Go 服务端未集成阿里云 SDK）"}
}

type phoneAuthResult struct {
	success bool
	phone   string
	message string
}

// getPhoneByToken 阿里云号码认证（一键登录）。Go 版未集成 SDK，返回未配置。
func (h *Handler) getPhoneByToken(ctx context.Context, spToken string) phoneAuthResult {
	cfg := h.getAliyunConfig(ctx)
	if !aliyunBool(cfg, "phoneAuthEnabled") || aliyunStr(cfg, "accessKeyId") == "" || aliyunStr(cfg, "accessKeySecret") == "" {
		return phoneAuthResult{false, "", "号码认证服务未配置"}
	}
	return phoneAuthResult{false, "", "号码认证服务暂不可用（Go 服务端未集成阿里云 SDK）"}
}

// ============ 邮件发送（照抄 email.ts sendEmail，net/smtp 标准库实现） ============

func (h *Handler) sendEmail(ctx context.Context, to, subject, html string) {
	cfg := h.getSystemConfig(ctx, "smtp")
	if cfg == nil {
		cfg = map[string]any{}
	}
	str := func(key string, alt ...string) string {
		if v, ok := cfg[key].(string); ok && v != "" {
			return v
		}
		for _, k := range alt {
			if v, ok := cfg[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	host := str("host")
	port := 465
	if v, ok := cfg["port"].(float64); ok && v > 0 {
		port = int(v)
	}
	secure := true
	if v, ok := cfg["secure"].(bool); ok {
		secure = v
	}
	authUser := str("authUser", "user")
	authPass := str("authPass", "pass")
	fromAddress := str("fromAddress", "fromEmail")
	fromName := str("fromName")
	if fromName == "" {
		fromName = "imim"
	}
	if host == "" || authUser == "" || authPass == "" {
		log.Printf("[Email] SMTP 配置不完整，无法发送邮件 -> %s", to)
		return
	}
	from := fromAddress
	if from == "" {
		from = authUser
	}
	header := "From: \"" + fromName + "\" <" + from + ">\r\n" +
		"To: <" + to + ">\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n\r\n"
	msg := []byte(header + html)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	auth := smtp.PlainAuth("", authUser, authPass, host)
	var err error
	if secure {
		err = sendMailTLS(addr, host, auth, from, []string{to}, msg)
	} else {
		err = sendMailStartTLS(addr, host, auth, from, []string{to}, msg)
	}
	if err != nil {
		log.Printf("[Email] 邮件发送失败: %v -> %s", err, to)
		return
	}
	log.Printf("[Email] 邮件发送成功 -> %s", to)
}

// sendMailStartTLS 明文连接 + STARTTLS（587 端口）发送。
func sendMailStartTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, t := range to {
		if err := c.Rcpt(t); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
func sendMailTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, t := range to {
		if err := c.Rcpt(t); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
