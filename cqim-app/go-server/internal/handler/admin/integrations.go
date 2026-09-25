package admin

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"log"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============================================================
// SMTP 配置（移植自 admin.ts normalizeSmtpConfig）
// ============================================================

type smtpConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Secure      bool   `json:"secure"`
	AuthUser    string `json:"authUser"`
	AuthPass    string `json:"authPass"`
	User        string `json:"user"`
	Pass        string `json:"pass"`
	FromName    string `json:"fromName"`
	FromAddress string `json:"fromAddress"`
	FromEmail   string `json:"fromEmail"`
	ReplyTo     string `json:"replyTo"`
	Enabled     bool   `json:"enabled"`
}

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func intOf(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return def
}

func boolOf(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// firstStr 取第一个非 null 的字符串字段（对应 TS ?? 语义：仅 null/undefined 时回退）。
func firstStr(raw map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := raw[k]; ok && v != nil {
			if s, ok := v.(string); ok {
				return s
			}
			return ""
		}
	}
	return ""
}

// normalizeSmtpConfig 规范化 SMTP 配置（与 TS 一致）。
func normalizeSmtpConfig(raw map[string]any) smtpConfig {
	user := firstStr(raw, "authUser", "user")
	pass := firstStr(raw, "authPass", "pass")
	fromAddress := firstStr(raw, "fromAddress", "fromEmail")
	port := intOf(raw["port"], 465)
	if port == 0 {
		port = 465 // TS: Number(raw.port || 465)
	}
	secure := true
	if _, ok := raw["secure"]; ok {
		secure = boolOf(raw["secure"], true)
	}
	fromName := strOf(raw["fromName"])
	if fromName == "" {
		fromName = "imim"
	}
	return smtpConfig{
		Host: strOf(raw["host"]), Port: port, Secure: secure,
		AuthUser: user, AuthPass: pass, User: user, Pass: pass,
		FromName: fromName, FromAddress: fromAddress, FromEmail: fromAddress,
		ReplyTo: strOf(raw["replyTo"]), Enabled: boolOf(raw["enabled"], false),
	}
}

func smtpConfigToMap(c smtpConfig) map[string]any {
	return map[string]any{
		"host": c.Host, "port": c.Port, "secure": c.Secure,
		"authUser": c.AuthUser, "authPass": c.AuthPass, "user": c.User, "pass": c.Pass,
		"fromName": c.FromName, "fromAddress": c.FromAddress, "fromEmail": c.FromEmail,
		"replyTo": c.ReplyTo, "enabled": c.Enabled,
	}
}

// ============ GET /api/admin/smtp/config ============

func (h *Handler) getSmtpConfig(w http.ResponseWriter, r *http.Request) {
	cfg := normalizeSmtpConfig(h.getConfigMap(h.ctx(r), "smtp"))
	if cfg.Pass != "" {
		cfg.Pass = "••••••••"
	}
	if cfg.AuthPass != "" {
		cfg.AuthPass = "••••••••"
	}
	util.WriteJSON(w, 200, map[string]any{"config": smtpConfigToMap(cfg)})
}

// ============ PUT /api/admin/smtp/config ============

func (h *Handler) updateSmtpConfig(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	// 掩码值不覆盖真实密码
	if body["pass"] == "••••••••" {
		delete(body, "pass")
	}
	if body["authPass"] == "••••••••" {
		delete(body, "authPass")
	}
	current := smtpConfigToMap(normalizeSmtpConfig(h.getConfigMap(ctx, "smtp")))
	for k, v := range body {
		current[k] = v
	}
	merged := normalizeSmtpConfig(current)
	if err := h.setAdminConfigKV(ctx, "smtp", smtpConfigToMap(merged)); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	resp := smtpConfigToMap(merged)
	if merged.Pass != "" {
		resp["pass"] = "••••••••"
	} else {
		resp["pass"] = ""
	}
	if merged.AuthPass != "" {
		resp["authPass"] = "••••••••"
	} else {
		resp["authPass"] = ""
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "config": resp})
}

// ============ GET /api/admin/smtp/templates ============

func (h *Handler) getSmtpTemplates(w http.ResponseWriter, r *http.Request) {
	util.WriteJSON(w, 200, map[string]any{"templates": h.getConfigMap(h.ctx(r), "emailTemplates")})
}

// ============ PUT /api/admin/smtp/templates ============

func (h *Handler) updateSmtpTemplates(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	current := h.getConfigMap(ctx, "emailTemplates")
	for _, section := range []string{"verifyCode", "welcome", "resetPassword", "loginAlert"} {
		if v, ok := body[section].(map[string]any); ok && v != nil {
			base := map[string]any{}
			if cur, ok := current[section].(map[string]any); ok {
				base = cur
			}
			for k, val := range v {
				base[k] = val
			}
			current[section] = base
		}
	}
	if err := h.setAdminConfigKV(ctx, "emailTemplates", current); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// sendSmtpEmail 发送邮件（标准库实现，替代 email.js 的 sendEmail）。
func sendSmtpEmail(cfg smtpConfig, to, subject, html string) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dial := func() (net.Conn, error) {
		if cfg.Secure {
			return tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.Host})
		}
		return net.DialTimeout("tcp", addr, 10*time.Second)
	}
	conn, err := dial()
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	if cfg.AuthUser != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.AuthUser, cfg.AuthPass, cfg.Host)); err != nil {
			return err
		}
	}
	from := cfg.FromAddress
	if from == "" {
		from = cfg.AuthUser
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	fromHeader := from
	if cfg.FromName != "" {
		fromHeader = mime.QEncoding.Encode("utf-8", cfg.FromName) + " <" + from + ">"
	}
	var sb strings.Builder
	sb.WriteString("From: " + fromHeader + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(html)
	if _, err := wc.Write([]byte(sb.String())); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// ============ POST /api/admin/smtp/test 发送测试邮件 ============

func (h *Handler) testSmtp(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		To string `json:"to"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.To == "" {
		util.WriteError(w, 400, "请填写收件人")
		return
	}
	config := normalizeSmtpConfig(h.getConfigMap(ctx, "smtp"))
	if config.Host == "" || config.AuthUser == "" {
		util.WriteError(w, 400, "请先配置 SMTP 服务器地址和账号信息")
		return
	}
	var history []any
	if hv := h.getAdminConfigKV(ctx, "smtpTestHistory"); hv != nil {
		if arr, ok := hv.([]any); ok {
			history = arr
		}
	}
	siteConfig := h.getConfigMap(ctx, "site")
	siteURL := strOf(siteConfig["url"])
	if siteURL == "" {
		siteURL = "https://im.cqcq.chat"
	}
	logoURL := siteURL + "/imim-email-logo.jpg"
	testHTML := `<div style="padding: 20px; background-color: #f5f5f5; font-family: sans-serif;">
<div style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); overflow: hidden;">
<div style="background: linear-gradient(135deg, #1a237e 0%, #283593 100%); padding: 24px; text-align: center;">
<img src="` + logoURL + `" alt="imim" style="width: 64px; height: 64px; border-radius: 50%; border: 3px solid rgba(255,255,255,0.3); object-fit: cover;" />
<h1 style="color: #ffffff; margin: 12px 0 0; font-size: 20px; font-weight: 600;">imim</h1>
</div>
<div style="padding: 30px;">
<h2 style="color: #333; margin-top: 0; font-size: 18px;">SMTP 测试邮件</h2>
<p style="color: #666; font-size: 15px; line-height: 1.6;">恭喜！如果您收到此邮件，说明 SMTP 邮件服务配置正确，邮件发送功能运行正常。</p>
<div style="background: #e8f5e9; padding: 16px; border-radius: 8px; margin: 20px 0;">
<p style="color: #2e7d32; font-size: 14px; margin: 0;">✅ SMTP 连接正常</p>
<p style="color: #2e7d32; font-size: 14px; margin: 8px 0 0;">✅ 邮件发送成功</p>
</div>
</div>
<div style="background: #f8f9fa; padding: 16px; text-align: center; border-top: 1px solid #eee;">
<p style="color: #aaa; font-size: 12px; margin: 0;">此邮件由 imim 系统自动发送，请勿直接回复</p>
</div>
</div>
</div>`
	sendErr := sendSmtpEmail(config, req.To, "测试邮件", testHTML)
	record := map[string]any{
		"id": reqKeywordID(), "to": req.To, "createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	if sendErr != nil {
		record["status"] = "error"
		record["message"] = sendErr.Error()
	} else {
		record["status"] = "success"
		record["message"] = "测试邮件发送成功"
	}
	history = append([]any{record}, history...)
	if len(history) > 20 {
		history = history[:20]
	}
	_ = h.setAdminConfigKV(ctx, "smtpTestHistory", history)
	// 与 TS 一致：无论发送成败都返回 success（结果记在历史里）
	util.WriteJSON(w, 200, map[string]any{"success": true, "message": "测试邮件已发送"})
}

// ============ GET /api/admin/smtp/test-history ============

func (h *Handler) smtpTestHistory(w http.ResponseWriter, r *http.Request) {
	history := []any{}
	if v := h.getAdminConfigKV(h.ctx(r), "smtpTestHistory"); v != nil {
		if arr, ok := v.([]any); ok {
			history = arr
		}
	}
	util.WriteJSON(w, 200, map[string]any{"history": history})
}

// ============================================================
// 阿里云配置（短信/号码认证）
// ============================================================

// ============ GET /api/admin/aliyun-config（脱敏） ============

func (h *Handler) getAliyunConfig(w http.ResponseWriter, r *http.Request) {
	config := h.getConfigMap(h.ctx(r), "aliyun")
	// 脱敏处理
	if _, ok := config["accessKeySecret"].(string); ok {
		config["accessKeySecret"] = "••••••••"
	}
	util.WriteJSON(w, 200, map[string]any{"config": config})
}

// ============ PUT /api/admin/aliyun-config ============

func (h *Handler) updateAliyunConfig(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	// 如果前端传回的是脱敏值，不覆盖原值
	if body["accessKeySecret"] == "••••••••" {
		delete(body, "accessKeySecret")
	}
	merged := h.getConfigMap(ctx, "aliyun")
	for k, v := range body {
		merged[k] = v
	}
	if err := h.setAdminConfigKV(ctx, "aliyun", merged); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	admin := middleware.AdminFrom(r)
	_ = admin
	h.addLog(ctx, middleware.AdminFrom(r).Id, middleware.AdminFrom(r).Username,
		"修改阿里云配置", "system", "更新阿里云 SMS/号码认证配置", h.clientIP(r))
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// aliyunEncode 阿里云签名用 URL 编码。
func aliyunEncode(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	e = strings.ReplaceAll(e, "*", "%2A")
	e = strings.ReplaceAll(e, "%7E", "~")
	return e
}

// sendAliyunSms 发送阿里云短信（HMAC-SHA1 签名，标准库实现）。
func sendAliyunSms(ctx context.Context, accessKeyID, accessKeySecret, phone, signName, templateCode string) (code, message string, err error) {
	params := map[string]string{
		"AccessKeyId":      accessKeyID,
		"Action":           "SendSms",
		"Format":           "JSON",
		"PhoneNumbers":     phone,
		"SignName":         signName,
		"TemplateCode":     templateCode,
		"TemplateParam":    `{"code":"888888","min":"5"}`,
		"RegionId":         "cn-hangzhou",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   util.GenerateToken(16),
		"SignatureVersion": "1.0",
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          "2017-05-25",
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var canonical []string
	for _, k := range keys {
		canonical = append(canonical, aliyunEncode(k)+"="+aliyunEncode(params[k]))
	}
	stringToSign := "GET&" + aliyunEncode("/") + "&" + aliyunEncode(strings.Join(canonical, "&"))
	signature := base64.StdEncoding.EncodeToString(hmacSHA1([]byte(accessKeySecret+"&"), stringToSign))
	params["Signature"] = signature

	var q []string
	for _, k := range append(keys, "Signature") {
		q = append(q, aliyunEncode(k)+"="+aliyunEncode(params[k]))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://dysmsapi.aliyuncs.com/?"+strings.Join(q, "&"), nil)
	if err != nil {
		return "", "", err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var result struct {
		Code      string `json:"Code"`
		Message   string `json:"Message"`
		RequestID string `json:"RequestId"`
		BizID     string `json:"BizId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}
	return result.Code, result.Message, nil
}

// ============ POST /api/admin/aliyun-sms-test 测试发送短信 ============

func (h *Handler) testAliyunSms(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		Phone        string `json:"phone"`
		SignName     string `json:"signName"`
		TemplateCode string `json:"templateCode"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Phone == "" {
		util.WriteError(w, 400, "请填写测试手机号")
		return
	}
	config := h.getConfigMap(ctx, "aliyun")
	accessKeyID, accessKeySecret := cosString(config, "accessKeyId"), cosString(config, "accessKeySecret")
	if accessKeyID == "" || accessKeySecret == "" {
		util.WriteError(w, 400, "请先配置阿里云 AccessKey")
		return
	}
	// 使用指定的签名和模板，或回退到默认配置
	useSignName := req.SignName
	if useSignName == "" {
		useSignName = cosString(config, "smsSignName")
	}
	useTemplateCode := req.TemplateCode
	if useTemplateCode == "" {
		if tpl, ok := config["smsTemplates"].(map[string]any); ok {
			if login, ok := tpl["login"].(map[string]any); ok {
				useTemplateCode = strOf(login["code"])
			}
		}
	}
	if useSignName == "" {
		util.WriteError(w, 400, "请先配置短信签名")
		return
	}
	if useTemplateCode == "" {
		util.WriteError(w, 400, "请先配置短信模板")
		return
	}

	code, message, err := sendAliyunSms(ctx, accessKeyID, accessKeySecret, req.Phone, useSignName, useTemplateCode)
	if err != nil {
		util.WriteError(w, 500, err.Error())
		return
	}

	// 记录测试历史
	var history []any
	if v := h.getAdminConfigKV(ctx, "aliyunSmsTestHistory"); v != nil {
		if arr, ok := v.([]any); ok {
			history = arr
		}
	}
	status := "failed"
	if code == "OK" {
		status = "success"
	}
	msg := message
	if msg == "" {
		msg = code
	}
	if msg == "" {
		msg = "未知"
	}
	history = append([]any{map[string]any{
		"id": reqKeywordID(), "phone": req.Phone, "signName": useSignName,
		"templateCode": useTemplateCode, "status": status, "message": msg,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}}, history...)
	if len(history) > 50 {
		history = history[:50]
	}
	_ = h.setAdminConfigKV(ctx, "aliyunSmsTestHistory", history)

	if code == "OK" {
		util.WriteJSON(w, 200, map[string]any{"success": true, "message": "测试短信发送成功"})
	} else {
		util.WriteJSON(w, 400, map[string]any{"error": msg, "code": code})
	}
}

// ============ GET /api/admin/aliyun-sms-test-history ============

func (h *Handler) aliyunSmsTestHistory(w http.ResponseWriter, r *http.Request) {
	history := []any{}
	if v := h.getAdminConfigKV(h.ctx(r), "aliyunSmsTestHistory"); v != nil {
		if arr, ok := v.([]any); ok {
			history = arr
		}
	}
	util.WriteJSON(w, 200, map[string]any{"history": history})
}

// ============================================================
// 腾讯位置服务 API 配置
// ============================================================

// ============ GET /api/admin/txmap-config ============

func (h *Handler) getTxmapConfig(w http.ResponseWriter, r *http.Request) {
	util.WriteJSON(w, 200, map[string]any{"config": h.getConfigMap(h.ctx(r), "txmap")})
}

// ============ PUT /api/admin/txmap-config ============

func (h *Handler) updateTxmapConfig(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	merged := h.getConfigMap(ctx, "txmap")
	for k, v := range body {
		merged[k] = v
	}
	if err := h.setAdminConfigKV(ctx, "txmap", merged); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// 用户资料变更广播（移植自 user-profile-sync.ts publishUserProfileUpdatedById）
// ============================================================

// publishUserProfileUpdatedById 管理员修改用户资料后广播同步事件（best-effort，失败静默）。
func (h *Handler) publishUserProfileUpdatedById(ctx context.Context, userID string) {
	defer func() { _ = recover() }()
	if h.deps.Redis == nil {
		return
	}
	// pgx 严格模式：专用行 struct。
	type profileSyncUser struct {
		Id            string    `db:"id"`
		Username      string    `db:"username"`
		Nickname      *string   `db:"nickname"`
		Avatar        *string   `db:"avatar"`
		Bio           *string   `db:"bio"`
		BackgroundUrl *string   `db:"backgroundUrl"`
		UpdatedAt     time.Time `db:"updatedAt"`
	}
	u, err := db.QueryRowToStruct[profileSyncUser](ctx, h.db(),
		`SELECT "id","username","nickname","avatar","bio","backgroundUrl","updatedAt" FROM "User" WHERE "id"=$1`, userID)
	if err != nil {
		return
	}
	type friendRow struct {
		UserA string `db:"userA"`
		UserB string `db:"userB"`
	}
	friendIDs := []string{}
	if friendships, err := db.QueryToStructs[friendRow](ctx, h.db(),
		`SELECT "userA","userB" FROM "Friendship" WHERE "userA"=$1 OR "userB"=$1`, userID); err == nil {
		for _, f := range friendships {
			if f.UserA == userID {
				friendIDs = append(friendIDs, f.UserB)
			} else {
				friendIDs = append(friendIDs, f.UserA)
			}
		}
	}
	type gmRow struct {
		GroupID string `db:"groupId"`
	}
	groupIDs := []string{}
	if memberships, err := db.QueryToStructs[gmRow](ctx, h.db(),
		`SELECT "groupId" FROM "GroupMember" WHERE "userId"=$1`, userID); err == nil {
		for _, m := range memberships {
			groupIDs = append(groupIDs, m.GroupID)
		}
	}
	payload, err := json.Marshal(map[string]any{
		"userId": u.Id, "nickname": u.Nickname, "avatar": u.Avatar, "username": u.Username,
		"bio": u.Bio, "backgroundUrl": u.BackgroundUrl, "updatedAt": u.UpdatedAt.UnixMilli(),
		"targetFriendIds": friendIDs, "targetGroupIds": groupIDs,
	})
	if err != nil {
		return
	}
	if err := h.deps.Redis.Publish(ctx, "msg:user_profile_updated", string(payload)); err != nil {
		log.Printf("[Admin] 发布用户资料更新事件失败: %v", err)
	}
}
