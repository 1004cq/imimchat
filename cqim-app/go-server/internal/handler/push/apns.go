// Package push 移植自 server/apns.ts（自建 APNs 推送服务）。
//
// 路由（全部 userAuth）：
//   - POST   /api/apns/token       注册/更新用户的 APNs Device Token
//   - POST   /api/apns/voip-token  注册/更新用户的 VoIP Token
//   - DELETE /api/apns/token       清除用户的 APNs Token
//
// 导出函数：SendAPNsPush / SendVoIPPush / SendApnsToUser。
// 与 Node 版关键差异（顺手优化审计项）：使用常驻 http.Client 复用 HTTP/2 连接，
// 而不是 Node 版每条推送新建一条 HTTP/2 连接。
package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// apnsHTTPClient 常驻复用连接：Go Transport 对 HTTPS 自动协商 HTTP/2（ALPN），
// 一条连接可承载多条推送，修复 Node 版"每条推送新建 HTTP/2 连接"的问题。
var apnsHTTPClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

// ============================================================
// 配置
// ============================================================

func defaultAPNsEnvironment(d *handler.Deps) string {
	if d.Cfg.ApnsProduction {
		return "production"
	}
	return "sandbox"
}

// normalizeAPNsEnvironment 与 TS 版同语义：仅接受 production|sandbox，其他回默认。
func normalizeAPNsEnvironment(d *handler.Deps, value any) string {
	if s, ok := value.(string); ok && (s == "production" || s == "sandbox") {
		return s
	}
	return defaultAPNsEnvironment(d)
}

func apnsHost(environment string) string {
	if environment == "production" {
		return "api.push.apple.com"
	}
	return "api.sandbox.push.apple.com"
}

func apnsBundleID(d *handler.Deps) string {
	if d.Cfg.ApnsBundleID != "" {
		return d.Cfg.ApnsBundleID
	}
	return "com.imim.chat" // 与 TS 版默认值一致
}

type p8Key struct {
	keyID   string
	keyData string
}

// p8Keys 读取 p8 证书。私钥只从环境变量读取（与 TS 版一致，禁止写入数据库或镜像）。
func p8Keys(d *handler.Deps) []p8Key {
	keyID := d.Cfg.ApnsKeyID
	keyData := os.Getenv("APNS_P8_KEY")
	if keyData == "" {
		keyData = os.Getenv("APNS_PRIVATE_KEY")
	}
	if keyID == "" || keyData == "" {
		return nil
	}
	return []p8Key{{keyID: keyID, keyData: strings.ReplaceAll(keyData, `\n`, "\n")}}
}

// ============================================================
// JWT（ES256，标准库 crypto/ecdsa 手工组装，不引入新依赖）
// ============================================================

type jwtEntry struct {
	token  string
	expiry time.Time
}

var jwtCache = struct {
	sync.Mutex
	m map[string]jwtEntry
}{m: make(map[string]jwtEntry)}

// clearJWTCache 清除指定 keyId 的 JWT 缓存（provider token 失效后尝试下一个证书时调用）。
func clearJWTCache(keyID string) {
	jwtCache.Lock()
	delete(jwtCache.m, keyID)
	jwtCache.Unlock()
}

func parseP8Key(keyData string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(keyData))
	if block == nil {
		return nil, fmt.Errorf("apns: 无效的 PEM 私钥")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: 解析 p8 私钥失败: %w", err)
	}
	priv, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("apns: p8 私钥不是 ECDSA 密钥")
	}
	return priv, nil
}

// signES256 对 signingInput 做 ES256 签名，返回 base64url(raw r||s)。
func signES256(signingInput string, priv *ecdsa.PrivateKey) (string, error) {
	h := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, priv, h[:])
	if err != nil {
		return "", err
	}
	raw := make([]byte, 64)
	rb, sb := r.Bytes(), s.Bytes()
	copy(raw[32-len(rb):32], rb)
	copy(raw[64-len(sb):], sb)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// generateJWT 生成 APNs provider JWT，每个 keyId 缓存 50 分钟（Apple 上限 60 分钟）。
func generateJWT(d *handler.Deps, keyID, keyData string) (string, error) {
	jwtCache.Lock()
	if e, ok := jwtCache.m[keyID]; ok && time.Now().Before(e.expiry) {
		t := e.token
		jwtCache.Unlock()
		return t, nil
	}
	jwtCache.Unlock()

	hb, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": keyID})
	cb, _ := json.Marshal(map[string]any{"iss": d.Cfg.ApnsTeamID, "iat": time.Now().Unix()})
	signingInput := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)

	priv, err := parseP8Key(keyData)
	if err != nil {
		return "", err
	}
	sig, err := signES256(signingInput, priv)
	if err != nil {
		return "", err
	}
	jwt := signingInput + "." + sig

	jwtCache.Lock()
	jwtCache.m[keyID] = jwtEntry{token: jwt, expiry: time.Now().Add(50 * time.Minute)}
	jwtCache.Unlock()
	return jwt, nil
}

// ============================================================
// APNs 发送
// ============================================================

type apnsResult struct {
	success    bool
	statusCode int
	reason     string
	keyID      string
}

// sendToAPNs 依次尝试每个 p8 证书发送；403 InvalidProviderToken 时清缓存并试下一个。
func sendToAPNs(d *handler.Deps, deviceToken string, payload map[string]any, topic, pushType string, priority int, environment string) apnsResult {
	keys := p8Keys(d)
	if len(keys) == 0 {
		log.Printf("[APNs] 没有可用的 p8 证书")
		return apnsResult{success: false, reason: "no_p8_keys"}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return apnsResult{success: false, reason: "payload_error"}
	}

	for _, key := range keys {
		res, err := sendWithKey(d, key, deviceToken, body, topic, pushType, priority, environment)
		if err != nil {
			log.Printf("[APNs] KeyID=%s 发送异常: %v", key.keyID, err)
			continue
		}
		if res.success {
			return res
		}
		if res.statusCode == 403 && res.reason == "InvalidProviderToken" {
			log.Printf("[APNs] KeyID=%s 无效，尝试下一个...", key.keyID)
			clearJWTCache(key.keyID)
			continue
		}
		return res
	}
	return apnsResult{success: false, reason: "all_keys_failed"}
}

func sendWithKey(d *handler.Deps, key p8Key, deviceToken string, body []byte, topic, pushType string, priority int, environment string) (apnsResult, error) {
	jwt, err := generateJWT(d, key.keyID, key.keyData)
	if err != nil {
		return apnsResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://"+apnsHost(environment)+"/3/device/"+deviceToken, bytes.NewReader(body))
	if err != nil {
		return apnsResult{}, err
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", topic)
	req.Header.Set("apns-push-type", pushType)
	req.Header.Set("apns-priority", fmt.Sprint(priority))
	req.Header.Set("apns-expiration", "0")

	resp, err := apnsHTTPClient.Do(req)
	if err != nil {
		return apnsResult{keyID: key.keyID, reason: "connection_error"}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusOK {
		log.Printf("[APNs] 推送成功 (KeyID=%s)", key.keyID)
		return apnsResult{success: true, statusCode: resp.StatusCode, keyID: key.keyID}, nil
	}
	reason := "unknown"
	var parsed struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(respBody, &parsed) == nil && parsed.Reason != "" {
		reason = parsed.Reason
	}
	log.Printf("[APNs] 推送失败 (KeyID=%s): status=%d reason=%s", key.keyID, resp.StatusCode, reason)
	return apnsResult{success: false, statusCode: resp.StatusCode, reason: reason, keyID: key.keyID}, nil
}

func isInvalidDeviceToken(res apnsResult) bool {
	return res.statusCode == http.StatusGone || res.reason == "BadDeviceToken" || res.reason == "Unregistered"
}

func deleteInvalidToken(ctx context.Context, d *handler.Deps, id, ctxName string) {
	if _, err := d.DB.Exec(ctx, `DELETE FROM "PushDeviceToken" WHERE "id"=$1`, id); err != nil {
		log.Printf("[APNs] 删除失效 Token 失败 (%s, id=%s): %v", ctxName, id, err)
	}
}

// ============================================================
// API 路由
// ============================================================

// decodeBody 解析 JSON 请求体；空 body 视为 {}（与 express.json() 行为一致，
// 保证 DELETE 等无 body 请求不会 400，而是走到参数校验分支）。
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		util.WriteError(w, http.StatusBadRequest, "请求参数格式错误")
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true // v 保持零值，等价于 {}
	}
	if err := json.Unmarshal(body, v); err != nil {
		util.WriteError(w, http.StatusBadRequest, "请求参数格式错误")
		return false
	}
	return true
}

// POST /api/apns/token 注册或更新用户的 APNs Device Token
func (h *Handler) registerToken(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		Token       string `json:"token"`
		Platform    string `json:"platform"`
		Environment any    `json:"environment"`
		Kind        string `json:"kind"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Token == "" {
		util.WriteError(w, 400, "缺少 token")
		return
	}

	// 与 TS 版一致：platform === 'ios' ? 'ios' : String(platform || 'ios')
	platform := req.Platform
	if platform == "" {
		platform = "ios"
	}
	environment := normalizeAPNsEnvironment(h.deps, req.Environment)
	kind := "alert"
	if req.Kind == "voip" {
		kind = "voip"
	}

	ctx := r.Context()
	_, err := h.deps.DB.Exec(ctx, `INSERT INTO "PushDeviceToken" ("id","userId","token","platform","environment","kind","createdAt","updatedAt")
		VALUES ($1,$2,$3,$4,$5,$6,NOW(),NOW())
		ON CONFLICT ("token","kind") DO UPDATE SET "userId"=EXCLUDED."userId","platform"=EXCLUDED."platform","environment"=EXCLUDED."environment","updatedAt"=NOW()`,
		util.NewID(), u.Id, req.Token, platform, environment, kind)
	if err != nil {
		log.Printf("[APNs] Token 注册失败: %v", err)
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	log.Printf("[APNs] 用户 %s 注册 Device Token", u.Id)
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// POST /api/apns/voip-token 注册或更新用户的 VoIP Token
func (h *Handler) registerVoipToken(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		VoipToken   string `json:"voipToken"`
		Environment any    `json:"environment"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.VoipToken == "" {
		util.WriteError(w, 400, "缺少 voipToken")
		return
	}

	environment := normalizeAPNsEnvironment(h.deps, req.Environment)
	ctx := r.Context()
	_, err := h.deps.DB.Exec(ctx, `INSERT INTO "PushDeviceToken" ("id","userId","token","platform","environment","kind","createdAt","updatedAt")
		VALUES ($1,$2,$3,'ios',$4,'voip',NOW(),NOW())
		ON CONFLICT ("token","kind") DO UPDATE SET "userId"=EXCLUDED."userId","platform"=EXCLUDED."platform","environment"=EXCLUDED."environment","updatedAt"=NOW()`,
		util.NewID(), u.Id, req.VoipToken, environment)
	if err != nil {
		log.Printf("[APNs] VoIP Token 注册失败: %v", err)
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	log.Printf("[APNs] 用户 %s 注册 VoIP Token", u.Id)
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

type deviceIDRow struct {
	Id string `db:"id"`
}

// DELETE /api/apns/token 用户登出时清除 Token
func (h *Handler) deleteToken(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	var req struct {
		Token       string `json:"token"`
		Kind        string `json:"kind"`
		Environment any    `json:"environment"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	kind := "alert"
	if req.Kind == "voip" {
		kind = "voip"
	}
	environment := normalizeAPNsEnvironment(h.deps, req.Environment)

	ctx := r.Context()
	var where string
	var args []any
	if req.Token != "" {
		where = `"userId"=$1 AND "token"=$2 AND "platform"='ios'`
		args = []any{u.Id, req.Token}
	} else {
		where = `"userId"=$1 AND "platform"='ios' AND "kind"=$2 AND "environment"=$3`
		args = []any{u.Id, kind, environment}
	}

	deleted := false
	dev, err := db.QueryRowToStruct[deviceIDRow](ctx, h.deps.DB,
		`SELECT "id" FROM "PushDeviceToken" WHERE `+where+` ORDER BY "updatedAt" DESC LIMIT 1`, args...)
	if err != nil {
		if !db.IsNotFound(err) {
			log.Printf("[APNs] Token 查询失败: %v", err)
			util.WriteError(w, 500, "服务器内部错误")
			return
		}
	} else {
		if _, err := h.deps.DB.Exec(ctx, `DELETE FROM "PushDeviceToken" WHERE "id"=$1`, dev.Id); err != nil {
			log.Printf("[APNs] Token 清除失败: %v", err)
			util.WriteError(w, 500, "服务器内部错误")
			return
		}
		deleted = true
	}

	log.Printf("[APNs] 用户 %s 清除当前设备 Token token=%s", u.Id, map[bool]string{true: "provided", false: "inferred"}[req.Token != ""])
	util.WriteJSON(w, 200, map[string]any{"success": true, "deleted": deleted})
}

// ============================================================
// 推送发送工具函数（导出供其他模块调用）
// ============================================================

// APNsPushPayload 对应 TS sendAPNsPush 参数。
type APNsPushPayload struct {
	ToUserID   string
	Title      string
	Body       string
	CustomData map[string]any
}

// VoIPPushPayload 对应 TS sendVoIPPush 参数。
type VoIPPushPayload struct {
	ToUserID     string
	CallerName   string
	CallID       string
	CallerID     string
	CallerAvatar string
	CallType     string // "audio" | "video"，默认 audio
	RoomID       string
}

type pushDevice struct {
	Id          string `db:"id"`
	Token       string `db:"token"`
	Environment string `db:"environment"`
}

// SendAPNsPush 向用户全部 iOS alert 设备发送普通离线消息推送。
// 返回是否至少一台设备送达；无设备时返回 (false, nil)。
func SendAPNsPush(d *handler.Deps, p APNsPushPayload) (bool, error) {
	ctx := context.Background()
	devices, err := db.QueryToStructs[pushDevice](ctx, d.DB,
		`SELECT "id","token","environment" FROM "PushDeviceToken" WHERE "userId"=$1 AND "platform"='ios' AND "kind"='alert'`, p.ToUserID)
	if err != nil {
		log.Printf("[APNs] 推送请求失败: %v", err)
		return false, err
	}
	if len(devices) == 0 {
		return false, nil
	}

	delivered := false
	for _, dev := range devices {
		payload := map[string]any{
			"aps": map[string]any{
				"alert":           map[string]any{"title": p.Title, "body": p.Body},
				"mutable-content": 1,
				"sound":           "default",
				"badge":           1,
			},
		}
		for k, v := range p.CustomData {
			payload[k] = v
		}
		res := sendToAPNs(d, dev.Token, payload, apnsBundleID(d), "alert", 10, normalizeAPNsEnvironment(d, dev.Environment))
		if res.success {
			delivered = true
		} else if isInvalidDeviceToken(res) {
			deleteInvalidToken(ctx, d, dev.Id, "alert")
		}
	}
	return delivered, nil
}

// SendVoIPPush 向指定用户发送 VoIP 来电推送；用户未注册 VoIP Token 时静默跳过。
func SendVoIPPush(d *handler.Deps, p VoIPPushPayload) (bool, error) {
	ctx := context.Background()
	devices, err := db.QueryToStructs[pushDevice](ctx, d.DB,
		`SELECT "id","token","environment" FROM "PushDeviceToken" WHERE "userId"=$1 AND "platform"='ios' AND "kind"='voip'`, p.ToUserID)
	if err != nil {
		log.Printf("[APNs] VoIP 推送请求失败: %v", err)
		return false, err
	}
	if len(devices) == 0 {
		return false, nil
	}

	callType := p.CallType
	if callType == "" {
		callType = "audio"
	}
	payload := map[string]any{
		"type":          "call_invite",
		"call_id":       p.CallID,
		"caller_id":     p.CallerID,
		"caller_name":   p.CallerName,
		"caller_avatar": p.CallerAvatar,
		"call_type":     callType,
		"room_id":       p.RoomID,
	}

	// VoIP 推送 topic 必须添加 .voip 后缀
	delivered := false
	for _, dev := range devices {
		res := sendToAPNs(d, dev.Token, payload, apnsBundleID(d)+".voip", "voip", 10, normalizeAPNsEnvironment(d, dev.Environment))
		if res.success {
			delivered = true
			log.Printf("[APNs] VoIP 推送成功: userId=%s keyId=%s", p.ToUserID, res.keyID)
		} else {
			log.Printf("[APNs] VoIP 推送失败: userId=%s tokenId=%s reason=%s", p.ToUserID, dev.Id, res.reason)
			if isInvalidDeviceToken(res) {
				deleteInvalidToken(ctx, d, dev.Id, "voip")
			}
		}
	}
	return delivered, nil
}

// SendApnsToUser 便捷封装：向用户发送一条 APNs alert 推送，data 透传为顶层自定义字段。
func SendApnsToUser(d *handler.Deps, userID, title, body string, data map[string]any) error {
	_, err := SendAPNsPush(d, APNsPushPayload{ToUserID: userID, Title: title, Body: body, CustomData: data})
	return err
}
