package admin

import (
	"context"
	"crypto/sha1"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// serverStartTime 进程启动时间（用于 /onebot/status 的 uptime，与 process.uptime() 对应）。
var serverStartTime = time.Now()

// ============================================================
// 站点设置
// ============================================================

// ============ GET /api/admin/site-config ============

func (h *Handler) getSiteConfig(w http.ResponseWriter, r *http.Request) {
	util.WriteJSON(w, 200, map[string]any{"config": h.getConfigMap(h.ctx(r), "site")})
}

// ============ PUT /api/admin/site-config ============

func (h *Handler) updateSiteConfig(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	merged := h.getConfigMap(ctx, "site")
	for k, v := range body {
		merged[k] = v
	}
	if err := h.setAdminConfigKV(ctx, "site", merged); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// COS 配置
// ============================================================

// ============ GET /api/admin/cos-config（脱敏） ============

func (h *Handler) getCosConfig(w http.ResponseWriter, r *http.Request) {
	config := h.getConfigMap(h.ctx(r), "cos")
	// 脱敏：只返回前6位 + 掩码
	if sk, ok := config["secretKey"].(string); ok && sk != "" {
		if len(sk) > 6 {
			config["secretKeyMasked"] = sk[:6] + "••••••••"
		} else {
			config["secretKeyMasked"] = "••••••••"
		}
		config["secretKey"] = "••••••••"
	}
	if si, ok := config["secretId"].(string); ok && si != "" {
		if len(si) > 10 {
			config["secretIdDisplay"] = si[:10] + "••••" + si[len(si)-4:]
		} else {
			config["secretIdDisplay"] = si
		}
	}
	util.WriteJSON(w, 200, map[string]any{"config": config})
}

// ============ PUT /api/admin/cos-config ============

func (h *Handler) updateCosConfig(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	admin := middleware.AdminFrom(r)
	ip := h.clientIP(r)
	// 如果前端传回掩码值，不覆盖真实密钥
	if body["secretKey"] == "••••••••" {
		delete(body, "secretKey")
	}
	merged := h.getConfigMap(ctx, "cos")
	for k, v := range body {
		merged[k] = v
	}
	if err := h.setAdminConfigKV(ctx, "cos", merged); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	// 记录操作日志
	var changed []string
	for k := range body {
		changed = append(changed, k)
	}
	h.addLog(ctx, admin.Id, admin.Username, "更新COS配置", "cos",
		"修改字段: "+strings.Join(changed, ", "), ip)
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ---------- 最小 COS v5 客户端（标准库实现，替代 cos-nodejs-sdk-v5） ----------

type cosError struct {
	code       string
	statusCode int
	message    string
}

func (e *cosError) Error() string { return e.message }

type cosClient struct {
	secretID  string
	secretKey string
	bucket    string
	region    string
	http      *http.Client
}

func newCosClient(secretID, secretKey, bucket, region string) *cosClient {
	return &cosClient{secretID: secretID, secretKey: secretKey, bucket: bucket, region: region,
		http: &http.Client{Timeout: 20 * time.Second}}
}

func (c *cosClient) host() string { return c.bucket + ".cos." + c.region + ".myqcloud.com" }

// cosEncode COS 签名用 URL 编码（RFC3986）。
func cosEncode(s string) string {
	e := url.QueryEscape(s)
	return strings.ReplaceAll(e, "+", "%20")
}

// auth 构造 COS v5 Authorization 头（q-sign-algorithm=sha1）。
func (c *cosClient) auth(method, path string, params map[string]string) string {
	start := time.Now().Unix() - 60
	signTime := fmt.Sprintf("%d;%d", start, start+600)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var cps []string
	for _, k := range keys {
		cps = append(cps, cosEncode(k)+"="+cosEncode(params[k]))
	}
	httpString := strings.ToLower(method) + "\n" + path + "\n" + "host\n" + strings.Join(cps, "&") + "\n"
	sha := fmt.Sprintf("%x", sha1Sum(httpString))
	stringToSign := "sha1\n" + signTime + "\n" + sha + "\n"
	signKey := hmacSHA1([]byte(c.secretKey), signTime)
	signature := fmt.Sprintf("%x", hmacSHA1(signKey, stringToSign))
	return fmt.Sprintf("q-sign-algorithm=sha1&q-ak=%s&q-sign-time=%s&q-key-time=%s&q-header-list=host&q-url-param-list=%s&q-signature=%s",
		c.secretID, signTime, signTime, strings.Join(keys, ";"), signature)
}

func sha1Sum(s string) [20]byte {
	h := sha1.New()
	h.Write([]byte(s))
	var out [20]byte
	copy(out[:], h.Sum(nil))
	return out
}

type cosAPIError struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func (c *cosClient) do(ctx context.Context, method, path string, params map[string]string) (*http.Response, error) {
	u := "https://" + c.host() + path
	if len(params) > 0 {
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var q []string
		for _, k := range keys {
			q = append(q, cosEncode(k)+"="+cosEncode(params[k]))
		}
		u += "?" + strings.Join(q, "&")
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.auth(method, path, params))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		var apiErr cosAPIError
		_ = xml.Unmarshal(body, &apiErr)
		msg := apiErr.Message
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, &cosError{code: apiErr.Code, statusCode: resp.StatusCode, message: msg}
	}
	return resp, nil
}

// headBucket 验证存储桶可访问。
func (c *cosClient) headBucket(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodHead, "/", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// putObject 上传空对象（用于创建目录占位）。
func (c *cosClient) putObject(ctx context.Context, key string) error {
	resp, err := c.do(ctx, http.MethodPut, "/"+key, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

type cosListResult struct {
	IsTruncated string `xml:"IsTruncated"`
	Contents    []struct {
		Key  string `xml:"Key"`
		Size int64  `xml:"Size"`
	} `xml:"Contents"`
}

// listBucket 列出对象（最多 1000 个）统计用量。
func (c *cosClient) listBucket(ctx context.Context) (totalObjects int, totalSize int64, isTruncated bool, err error) {
	resp, err := c.do(ctx, http.MethodGet, "/", map[string]string{
		"prefix": "", "max-keys": "1000", "delimiter": "/", "encoding-type": "url",
	})
	if err != nil {
		return 0, 0, false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, 0, false, err
	}
	var result cosListResult
	if err := xml.Unmarshal(body, &result); err != nil {
		return 0, 0, false, err
	}
	for _, o := range result.Contents {
		totalObjects++
		totalSize += o.Size
	}
	return totalObjects, totalSize, result.IsTruncated == "true", nil
}

// cosHint 把 COS 错误映射为中文提示（与 TS 一致）。
func cosHint(err error) string {
	var ce *cosError
	if errors.As(err, &ce) {
		switch {
		case ce.code == "NoSuchBucket":
			return "存储桶不存在，请检查名称和地域是否正确"
		case ce.statusCode == 403 || ce.code == "AccessDenied" || ce.code == "Forbidden":
			return "权限不足 (403 Forbidden)。请检查：\n1. SecretId/SecretKey 是否正确\n2. 该密钥是否有访问此存储桶的权限\n3. 如使用子账号密钥，请确保已授权 cos:HeadBucket、cos:PutObject 等操作\n4. 存储桶名称和地域是否与控制台一致"
		case ce.code == "SignatureDoesNotMatch":
			return "签名不匹配，SecretKey 可能填写错误，请重新复制粘贴"
		case ce.code == "InvalidAccessKeyId":
			return "SecretId 无效，请检查是否填写正确"
		default:
			code := ce.code
			if code == "" {
				code = strconv.Itoa(ce.statusCode)
			}
			return "错误码: " + code + "，" + ce.message
		}
	}
	return err.Error()
}

func cosString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

var cosInitDirs = []string{
	"imimchat/moments/.init",
	"imimchat/avatars/.init",
	"imimchat/group-avatars/.init",
}

// ============ POST /api/admin/cos-config/test COS 连接测试 ============

func (h *Handler) testCosConfig(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	config := h.getConfigMap(ctx, "cos")
	secretID, secretKey, bucket, region :=
		cosString(config, "secretId"), cosString(config, "secretKey"),
		cosString(config, "bucket"), cosString(config, "region")
	if secretID == "" || secretKey == "" || bucket == "" || region == "" {
		util.WriteJSON(w, 200, map[string]any{
			"success": false,
			"message": "请先填写完整的 COS 配置（SecretId、SecretKey、存储桶、地域均为必填）",
		})
		return
	}
	if !strings.Contains(bucket, "-") {
		util.WriteJSON(w, 200, map[string]any{
			"success": false,
			"message": "存储桶名称格式不正确，应为 bucket-APPID 格式，例如 myapp-1234567890",
		})
		return
	}
	cos := newCosClient(secretID, secretKey, bucket, region)
	// 步骤1：验证存储桶是否可访问
	if err := cos.headBucket(ctx); err != nil {
		util.WriteJSON(w, 200, map[string]any{"success": false, "message": "❌ 连接失败：" + cosHint(err)})
		return
	}
	// 步骤2：自动创建目录结构
	dirSuccess, dirFailed := 0, 0
	for _, key := range cosInitDirs {
		if err := cos.putObject(ctx, key); err != nil {
			dirFailed++
			log.Printf("[Admin] COS 创建目录 %s 失败: %v", key, err)
		} else {
			dirSuccess++
		}
	}
	var dirMsg string
	switch {
	case dirSuccess == len(cosInitDirs):
		dirMsg = "\n✅ 已自动创建目录结构：imimchat/moments/、imimchat/avatars/、imimchat/group-avatars/"
	case dirSuccess > 0:
		dirMsg = fmt.Sprintf("\n⚠️ 部分目录创建成功（%d/%d）", dirSuccess, len(cosInitDirs))
	default:
		dirMsg = "\n⚠️ 目录创建失败，请检查密钥是否有写入权限"
	}
	_ = dirFailed
	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"message": "✅ COS 连接成功！存储桶 " + bucket + "（" + region + "）可正常访问" + dirMsg,
	})
}

// ============ POST /api/admin/cos-config/init-dirs 手动初始化目录结构 ============

func (h *Handler) initCosDirs(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	config := h.getConfigMap(ctx, "cos")
	secretID, secretKey, bucket, region :=
		cosString(config, "secretId"), cosString(config, "secretKey"),
		cosString(config, "bucket"), cosString(config, "region")
	if secretID == "" || secretKey == "" || bucket == "" || region == "" {
		util.WriteJSON(w, 200, map[string]any{"success": false, "message": "请先填写完整的 COS 配置"})
		return
	}
	cos := newCosClient(secretID, secretKey, bucket, region)
	for _, key := range cosInitDirs {
		if err := cos.putObject(ctx, key); err != nil {
			util.WriteJSON(w, 200, map[string]any{"success": false, "message": "目录创建失败：" + err.Error()})
			return
		}
	}
	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"message": "✅ 目录结构创建成功：imimchat/moments/、imimchat/avatars/、imimchat/group-avatars/",
	})
}

// ============ GET /api/admin/cos-config/usage 存储用量查询 ============

func (h *Handler) cosUsage(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	config := h.getConfigMap(ctx, "cos")
	secretID, secretKey, bucket, region :=
		cosString(config, "secretId"), cosString(config, "secretKey"),
		cosString(config, "bucket"), cosString(config, "region")
	if secretID == "" || secretKey == "" || bucket == "" || region == "" {
		util.WriteJSON(w, 200, map[string]any{"success": false, "totalObjects": 0, "totalSize": 0})
		return
	}
	cos := newCosClient(secretID, secretKey, bucket, region)
	totalObjects, totalSize, isTruncated, err := cos.listBucket(ctx)
	if err != nil {
		util.WriteJSON(w, 200, map[string]any{
			"success": false, "totalObjects": 0, "totalSize": 0, "error": err.Error(),
		})
		return
	}
	message := ""
	if isTruncated {
		message = "对象数量超过 1000，仅统计前 1000 个"
	}
	util.WriteJSON(w, 200, map[string]any{
		"success": true, "totalObjects": totalObjects, "totalSize": totalSize,
		"isTruncated": isTruncated, "message": message,
	})
}

// ============================================================
// pyq 同步配置
// ============================================================

// ============ GET /api/admin/sync-config ============

func (h *Handler) getSyncConfig(w http.ResponseWriter, r *http.Request) {
	util.WriteJSON(w, 200, map[string]any{"config": h.getConfigMap(h.ctx(r), "pyqSync")})
}

// ============ PUT /api/admin/sync-config ============

func (h *Handler) updateSyncConfig(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	merged := h.getConfigMap(ctx, "pyqSync")
	for k, v := range body {
		merged[k] = v
	}
	if err := h.setAdminConfigKV(ctx, "pyqSync", merged); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============ POST /api/admin/sync-config/test ============

func (h *Handler) testSyncConfig(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	config := h.getConfigMap(ctx, "pyqSync")
	base := cosString(config, "pyqBaseUrl")
	if base == "" {
		util.WriteJSON(w, 200, map[string]any{"success": false, "message": "请先填写 pyq 服务地址"})
		return
	}
	tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(tctx, http.MethodGet, base+"/api/health", nil)
	if err != nil {
		util.WriteJSON(w, 200, map[string]any{"success": false, "message": "连接失败：" + err.Error()})
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		util.WriteJSON(w, 200, map[string]any{"success": false, "message": "连接失败：" + err.Error()})
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		util.WriteJSON(w, 200, map[string]any{"success": true, "message": "✅ pyq 服务连接成功"})
	} else {
		util.WriteJSON(w, 200, map[string]any{"success": false, "message": fmt.Sprintf("pyq 服务响应异常：HTTP %d", resp.StatusCode)})
	}
}

// ============================================================
// OneBot 配置
// ============================================================

// ============ GET /api/admin/onebot/config ============

func (h *Handler) getOnebotConfig(w http.ResponseWriter, r *http.Request) {
	util.WriteJSON(w, 200, map[string]any{"config": h.getConfigMap(h.ctx(r), "onebot")})
}

// ============ PUT /api/admin/onebot/config ============

func (h *Handler) updateOnebotConfig(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	merged := h.getConfigMap(ctx, "onebot")
	for k, v := range body {
		merged[k] = v
	}
	if err := h.setAdminConfigKV(ctx, "onebot", merged); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============ GET /api/admin/onebot/status ============

func (h *Handler) onebotStatus(w http.ResponseWriter, r *http.Request) {
	util.WriteJSON(w, 200, map[string]any{
		"status": map[string]any{
			"connectedClients": 0,
			"totalMessages":    0,
			"uptime":           time.Since(serverStartTime).Seconds(),
		},
	})
}

// autoReplyRules 读自动回复规则列表。
func (h *Handler) autoReplyRules(ctx context.Context) []map[string]any {
	v := h.getAdminConfigKV(ctx, "onebotAutoReplies")
	rules := []map[string]any{}
	if arr, ok := v.([]any); ok {
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				rules = append(rules, m)
			}
		}
	}
	return rules
}

// ============ GET /api/admin/onebot/auto-replies ============

func (h *Handler) listAutoReplies(w http.ResponseWriter, r *http.Request) {
	util.WriteJSON(w, 200, map[string]any{"rules": h.autoReplyRules(h.ctx(r))})
}

// ============ POST /api/admin/onebot/auto-replies ============

func (h *Handler) createAutoReply(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		Keyword   string `json:"keyword"`
		MatchType string `json:"matchType"`
		Scope     string `json:"scope"`
		Reply     string `json:"reply"`
		Enabled   *bool  `json:"enabled"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Keyword == "" || req.Reply == "" {
		util.WriteError(w, 400, "请填写关键词和回复")
		return
	}
	if req.MatchType == "" {
		req.MatchType = "exact"
	}
	if req.Scope == "" {
		req.Scope = "all"
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rules := h.autoReplyRules(ctx)
	rule := map[string]any{
		"id": reqKeywordID(), "keyword": req.Keyword, "matchType": req.MatchType,
		"scope": req.Scope, "reply": req.Reply, "enabled": enabled,
		"hitCount": 0, "createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	rules = append(rules, rule)
	if err := h.setAdminConfigKV(ctx, "onebotAutoReplies", rules); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "rule": rule})
}

func reqKeywordID() string { return strconv.FormatInt(time.Now().UnixMilli(), 10) }

// ============ PUT /api/admin/onebot/auto-replies/:id ============

func (h *Handler) updateAutoReply(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	rules := h.autoReplyRules(ctx)
	idx := -1
	for i, rule := range rules {
		if rule["id"] == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		util.WriteError(w, 404, "规则不存在")
		return
	}
	for k, v := range body {
		rules[idx][k] = v
	}
	if err := h.setAdminConfigKV(ctx, "onebotAutoReplies", rules); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "rule": rules[idx]})
}

// ============ DELETE /api/admin/onebot/auto-replies/:id ============

func (h *Handler) deleteAutoReply(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	rules := h.autoReplyRules(ctx)
	kept := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		if rule["id"] != id {
			kept = append(kept, rule)
		}
	}
	if err := h.setAdminConfigKV(ctx, "onebotAutoReplies", kept); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============ GET /api/admin/onebot/ai-config ============

func (h *Handler) getAIConfig(w http.ResponseWriter, r *http.Request) {
	util.WriteJSON(w, 200, map[string]any{"config": h.getConfigMap(h.ctx(r), "ai")})
}

// ============ PUT /api/admin/onebot/ai-config ============

func (h *Handler) updateAIConfig(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	merged := h.getConfigMap(ctx, "ai")
	for k, v := range body {
		merged[k] = v
	}
	if err := h.setAdminConfigKV(ctx, "ai", merged); err != nil {
		util.WriteError(w, 500, "保存失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}
