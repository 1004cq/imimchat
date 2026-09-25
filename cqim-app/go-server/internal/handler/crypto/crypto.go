// Package crypto 移植 server/crypto.ts：E2EE 加密基础设施（ECDH 公钥注册分发、
// AES-256-GCM 服务端存储加密、HMAC-SHA256 完整性校验、Signal PreKey Bundle）。
//
// 路由、JSON 字段、状态码、中文错误文案与 TS 版一致；全部路由受 userAuth 保护。
package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

type h struct{ d *handler.Deps }

// RegisterRoutes 注册 /api/crypto 与 /api/mls 全部路由（受 userAuth 保护）。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := &h{d: d}
	auth := func(pat string, fn http.HandlerFunc) {
		mux.Handle(pat, d.Auth.UserAuth(fn))
	}

	// ---- /api/crypto ----
	auth("POST /api/crypto/register-key", h.registerKey)
	auth("GET /api/crypto/get-key", h.getKey)
	auth("POST /api/crypto/verify-message", h.verifyMessage)
	auth("POST /api/crypto/register-bundle", h.registerBundle)
	auth("GET /api/crypto/get-bundle", h.getBundle)
	auth("GET /api/crypto/prekey-count", h.prekeyCount)
	auth("POST /api/crypto/replenish-prekeys", h.replenishPreKeys)

	// ---- /api/mls ----
	auth("POST /api/mls/upload-key-package", h.uploadKeyPackage)
	auth("GET /api/mls/get-key-package", h.getKeyPackage)
	auth("GET /api/mls/key-package-count", h.keyPackageCount)
	auth("POST /api/mls/enable-group", h.enableGroup)
	auth("GET /api/mls/group-state", h.groupState)
	auth("POST /api/mls/update-group-state", h.updateGroupState)
	auth("GET /api/mls/is-enabled", h.isEnabled)
	auth("POST /api/mls/send-welcome", h.sendWelcome)
	auth("GET /api/mls/pending-welcome", h.pendingWelcome)
	auth("POST /api/mls/ack-welcome", h.ackWelcome)
	auth("POST /api/mls/broadcast-commit", h.broadcastCommit)
	auth("GET /api/mls/pending-commits", h.pendingCommits)
	auth("POST /api/mls/register-identity", h.registerIdentity)
	auth("GET /api/mls/get-identity", h.getIdentity)
	auth("POST /api/mls/batch-get-identity", h.batchGetIdentity)
	auth("GET /api/mls/stats", h.stats)
}

// ============================================================
// SystemConfig 读写（对应 prisma.systemConfig）
// ============================================================

func (h *h) cfgGet(ctx context.Context, key string) (string, bool, error) {
	v, err := db.QueryRowToStruct[db.SystemConfig](
		ctx, h.d.DB,
		`SELECT "key","value","updatedAt" FROM "SystemConfig" WHERE "key"=$1`, key)
	if err != nil {
		if db.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return v.Value, true, nil
}

func (h *h) cfgUpsert(ctx context.Context, key, value string) error {
	_, err := h.d.DB.Exec(ctx,
		`INSERT INTO "SystemConfig" ("key","value","updatedAt") VALUES ($1,$2,NOW())
		 ON CONFLICT ("key") DO UPDATE SET "value"=EXCLUDED."value","updatedAt"=NOW()`,
		key, value)
	return err
}

func (h *h) cfgDelete(ctx context.Context, key string) error {
	_, err := h.d.DB.Exec(ctx, `DELETE FROM "SystemConfig" WHERE "key"=$1`, key)
	return err
}

func (h *h) cfgByPrefix(ctx context.Context, prefix string) ([]db.SystemConfig, error) {
	return db.QueryToStructs[db.SystemConfig](ctx, h.d.DB,
		`SELECT "key","value","updatedAt" FROM "SystemConfig" WHERE "key" LIKE $1`, prefix+"%")
}

func cfgJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// isoNow 与 new Date().toISOString() 同格式（毫秒精度 UTC）。
func isoNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// ============================================================
// 1. AES-256-GCM 加解密工具（服务端存储加密）
// ============================================================

const (
	ivLength  = 12 // GCM 推荐 12 字节 IV
	tagLength = 16 // 认证标签长度
)

var (
	encKeyMu sync.Mutex
	encKey   []byte
)

// getServerEncryptionKey 获取服务端加密密钥。
// 优先环境变量 ENCRYPTION_KEY（hex），其次数据库 encryptionKey（JSON 存的 hex 串），
// 无则生成 32 字节随机密钥并持久化；数据库不可用时回退到固定派生密钥（仅开发环境）。
func getServerEncryptionKey(ctx context.Context, d *handler.Deps) []byte {
	encKeyMu.Lock()
	defer encKeyMu.Unlock()
	if encKey != nil {
		return encKey
	}

	if hexKey := os.Getenv("ENCRYPTION_KEY"); hexKey != "" {
		if b, err := hex.DecodeString(hexKey); err == nil && len(b) == 32 {
			encKey = b
			return encKey
		}
	}

	helper := &h{d: d}
	if v, ok, err := helper.cfgGet(ctx, "encryptionKey"); err == nil && ok {
		var hexStr string
		if json.Unmarshal([]byte(v), &hexStr) == nil {
			if b, err := hex.DecodeString(hexStr); err == nil && len(b) == 32 {
				encKey = b
				return encKey
			}
		}
	}

	nb := make([]byte, 32)
	if _, err := rand.Read(nb); err != nil {
		// 回退：使用固定派生密钥（仅开发环境）
		if k, derr := scrypt.Key([]byte("cqim-dev-key"), []byte("cqim-salt"), 32768, 8, 1, 32); derr == nil {
			encKey = k
			return encKey
		}
	}
	if b, err := json.Marshal(hex.EncodeToString(nb)); err == nil {
		_ = helper.cfgUpsert(ctx, "encryptionKey", string(b))
	}
	encKey = nb
	return encKey
}

// EncryptData AES-256-GCM 加密，返回 base64(IV + ciphertext + authTag)。
func EncryptData(ctx context.Context, d *handler.Deps, plaintext string) (string, error) {
	key := getServerEncryptionKey(ctx, d)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := make([]byte, ivLength)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	// gcm.Seal 把 authTag 追加在 ciphertext 之后，与 Node 布局一致
	out := append(iv, gcm.Seal(nil, iv, []byte(plaintext), nil)...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// DecryptData AES-256-GCM 解密，输入 base64(IV + ciphertext + authTag)。
func DecryptData(ctx context.Context, d *handler.Deps, encryptedBase64 string) (string, error) {
	key := getServerEncryptionKey(ctx, d)
	data, err := base64.StdEncoding.DecodeString(encryptedBase64)
	if err != nil {
		return "", err
	}
	if len(data) < ivLength+tagLength {
		return "", errShortCipher
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := data[:ivLength]
	ciphertext := data[ivLength:]
	plain, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

var errShortCipher = errCipher()

type cipherErr struct{}

func (cipherErr) Error() string { return "密文过短" }

func errCipher() error { return cipherErr{} }

// ============================================================
// 2. HMAC-SHA256 消息完整性校验
// ============================================================

// GenerateHMAC 生成 HMAC-SHA256 签名（hex）。
func GenerateHMAC(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyHMAC 验证 HMAC-SHA256 签名（防时序攻击）。
func VerifyHMAC(message, signature, secret string) bool {
	expected := GenerateHMAC(message, secret)
	a, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	b, _ := hex.DecodeString(expected)
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

// ============================================================
// 3. ECDH 密钥交换 API
// ============================================================

// POST /api/crypto/register-key
func (h *h) registerKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID    string `json:"userId"`
		PublicKey string `json:"publicKey"`
		DeviceID  string `json:"deviceId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" || req.PublicKey == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	// 验证公钥格式（Base64 编码的 ECDH 公钥）
	if !pubKeyRe.MatchString(req.PublicKey) || len(req.PublicKey) < 40 {
		util.WriteError(w, 400, "公钥格式无效")
		return
	}
	deviceID := req.DeviceID
	if deviceID == "" {
		deviceID = "default"
	}
	key := "e2ee:pubkey:" + req.UserID + ":" + deviceID
	now := isoNow()
	// TS 语义：update 分支只写 {publicKey, updatedAt}；create 分支写 {publicKey, createdAt, updatedAt}
	_, exists, err := h.cfgGet(r.Context(), key)
	if err != nil {
		util.WriteError(w, 500, "注册公钥失败")
		return
	}
	payload := map[string]string{"publicKey": req.PublicKey, "updatedAt": now}
	if !exists {
		payload["createdAt"] = now
	}
	if err := h.cfgUpsert(r.Context(), key, cfgJSON(payload)); err != nil {
		util.WriteError(w, 500, "注册公钥失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

var pubKeyRe = regexp.MustCompile(`^[A-Za-z0-9+/=]+$`)

// GET /api/crypto/get-key?userId=xxx&deviceId=xxx
func (h *h) getKey(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	deviceID := r.URL.Query().Get("deviceId")
	if deviceID == "" {
		deviceID = "default"
	}
	if userID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	v, ok, err := h.cfgGet(r.Context(), "e2ee:pubkey:"+userID+":"+deviceID)
	if err != nil {
		util.WriteError(w, 500, "获取公钥失败")
		return
	}
	if !ok {
		util.WriteError(w, 404, "未找到公钥")
		return
	}
	var data struct {
		PublicKey string `json:"publicKey"`
		UpdatedAt string `json:"updatedAt"`
	}
	_ = json.Unmarshal([]byte(v), &data)
	util.WriteJSON(w, 200, map[string]any{
		"publicKey": data.PublicKey,
		"updatedAt": data.UpdatedAt,
	})
}

// POST /api/crypto/verify-message
func (h *h) verifyMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message   string `json:"message"`
		Signature string `json:"signature"`
		SenderID  string `json:"senderId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Message == "" || req.Signature == "" || req.SenderID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	// 使用发送者的公钥作为 HMAC 密钥的一部分
	v, ok, err := h.cfgGet(r.Context(), "e2ee:pubkey:"+req.SenderID+":default")
	if err != nil {
		util.WriteError(w, 500, "验证失败")
		return
	}
	if !ok {
		util.WriteError(w, 404, "发送者公钥未注册")
		return
	}
	var data struct {
		PublicKey string `json:"publicKey"`
	}
	_ = json.Unmarshal([]byte(v), &data)
	sum := sha256.Sum256([]byte(data.PublicKey))
	hmacKey := hex.EncodeToString(sum[:])
	util.WriteJSON(w, 200, map[string]any{
		"valid": VerifyHMAC(req.Message, req.Signature, hmacKey),
	})
}

// ============================================================
// 4. Signal Protocol PreKey Bundle API
// ============================================================

type preKey struct {
	KeyID     int    `json:"keyId"`
	PublicKey string `json:"publicKey"`
}

// resolveSigningPublicKey 合并 PreKey Bundle 中的 ECDSA 签名公钥，
// 避免旧客户端把已发布字段覆盖成空。返回 nil 表示 JSON null。
func resolveSigningPublicKey(incoming any, identityKey, existingValue string) *string {
	if s, ok := incoming.(string); ok && len(s) > 0 {
		return &s
	}
	if existingValue == "" {
		return nil
	}
	var prev struct {
		IdentityKey      string `json:"identityKey"`
		SigningPublicKey string `json:"signingPublicKey"`
	}
	if err := json.Unmarshal([]byte(existingValue), &prev); err != nil {
		return nil
	}
	if prev.IdentityKey == identityKey && prev.SigningPublicKey != "" {
		return &prev.SigningPublicKey
	}
	return nil
}

// POST /api/crypto/register-bundle
func (h *h) registerBundle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID           string   `json:"userId"`
		RegistrationID   any      `json:"registrationId"`
		IdentityKey      string   `json:"identityKey"`
		SigningPublicKey any      `json:"signingPublicKey"`
		SignedPreKey     any      `json:"signedPreKey"`
		PreKeys          []preKey `json:"preKeys"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" || req.IdentityKey == "" || req.SignedPreKey == nil {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}

	existingBundle, _, _ := h.cfgGet(r.Context(), "e2ee:bundle:"+req.UserID)
	storedSigningPublicKey := resolveSigningPublicKey(req.SigningPublicKey, req.IdentityKey, existingBundle)

	now := isoNow()
	bundle := map[string]any{
		"registrationId":   req.RegistrationID,
		"identityKey":      req.IdentityKey,
		"signingPublicKey": storedSigningPublicKey,
		"signedPreKey":     req.SignedPreKey,
		"updatedAt":        now,
	}
	if existingBundle == "" {
		bundle["createdAt"] = now
	}
	if err := h.cfgUpsert(r.Context(), "e2ee:bundle:"+req.UserID, cfgJSON(bundle)); err != nil {
		util.WriteError(w, 500, "注册 Bundle 失败")
		return
	}

	// 存储 One-Time PreKeys（追加模式，不覆盖已有的）
	if len(req.PreKeys) > 0 {
		if err := h.mergePreKeys(r.Context(), req.UserID, req.PreKeys); err != nil {
			util.WriteError(w, 500, "注册 Bundle 失败")
			return
		}
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// mergePreKeys 合并新旧 PreKeys（按 keyId 去重）。
func (h *h) mergePreKeys(ctx context.Context, userID string, incoming []preKey) error {
	key := "e2ee:prekeys:" + userID
	var existing []preKey
	if v, ok, err := h.cfgGet(ctx, key); err == nil && ok {
		_ = json.Unmarshal([]byte(v), &existing)
	}
	keyMap := make(map[int]string)
	for _, k := range existing {
		keyMap[k.KeyID] = k.PublicKey
	}
	for _, k := range incoming {
		keyMap[k.KeyID] = k.PublicKey
	}
	merged := make([]preKey, 0, len(keyMap))
	for keyID, publicKey := range keyMap {
		merged = append(merged, preKey{KeyID: keyID, PublicKey: publicKey})
	}
	return h.cfgUpsert(ctx, key, cfgJSON(merged))
}

// GET /api/crypto/get-bundle?userId=xxx（返回后自动消费一个 One-Time PreKey）
func (h *h) getBundle(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	bundleVal, ok, err := h.cfgGet(r.Context(), "e2ee:bundle:"+userID)
	if err != nil {
		util.WriteError(w, 500, "获取 Bundle 失败")
		return
	}
	if !ok {
		util.WriteError(w, 404, "用户未注册 E2EE Bundle")
		return
	}
	var bundle map[string]any
	_ = json.Unmarshal([]byte(bundleVal), &bundle)

	// 获取并消费一个 One-Time PreKey
	var oneTimePreKey any
	preKeysKey := "e2ee:prekeys:" + userID
	if v, ok, err := h.cfgGet(r.Context(), preKeysKey); err == nil && ok {
		var preKeys []map[string]any
		if json.Unmarshal([]byte(v), &preKeys) == nil && len(preKeys) > 0 {
			oneTimePreKey = preKeys[0]
			preKeys = preKeys[1:]
			_ = h.cfgUpsert(r.Context(), preKeysKey, cfgJSON(preKeys))
		}
	}

	resp := map[string]any{
		"registrationId": bundle["registrationId"],
		"identityKey":    bundle["identityKey"],
		"signedPreKey":   bundle["signedPreKey"],
		"preKey":         oneTimePreKey,
	}
	if spk, ok := bundle["signingPublicKey"]; ok && spk != nil {
		resp["signingPublicKey"] = spk
	} else {
		resp["signingPublicKey"] = nil
	}
	util.WriteJSON(w, 200, resp)
}

// GET /api/crypto/prekey-count?userId=xxx
func (h *h) prekeyCount(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	count := 0
	if v, ok, err := h.cfgGet(r.Context(), "e2ee:prekeys:"+userID); err == nil && ok {
		var preKeys []any
		if json.Unmarshal([]byte(v), &preKeys) == nil {
			count = len(preKeys)
		}
	} else if err != nil {
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"count": count})
}

// POST /api/crypto/replenish-prekeys
func (h *h) replenishPreKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID  string   `json:"userId"`
		PreKeys []preKey `json:"preKeys"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" || len(req.PreKeys) == 0 {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	key := "e2ee:prekeys:" + req.UserID
	var existing []preKey
	if v, ok, err := h.cfgGet(r.Context(), key); err != nil {
		util.WriteError(w, 500, "补充失败")
		return
	} else if ok {
		_ = json.Unmarshal([]byte(v), &existing)
	}
	keyMap := make(map[int]string)
	for _, k := range existing {
		keyMap[k.KeyID] = k.PublicKey
	}
	for _, k := range req.PreKeys {
		keyMap[k.KeyID] = k.PublicKey
	}
	merged := make([]preKey, 0, len(keyMap))
	for keyID, publicKey := range keyMap {
		merged = append(merged, preKey{KeyID: keyID, PublicKey: publicKey})
	}
	if err := h.cfgUpsert(r.Context(), key, cfgJSON(merged)); err != nil {
		util.WriteError(w, 500, "补充失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "totalCount": len(merged)})
}

// ============================================================
// 5. 工具函数导出
// ============================================================

// GenerateSecureRandom 生成安全随机字节（Base64 编码），默认 32 字节。
func GenerateSecureRandom(n ...int) string {
	size := 32
	if len(n) > 0 {
		size = n[0]
	}
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

// SecureCompare 安全比较两个字符串（防时序攻击）。
func SecureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
