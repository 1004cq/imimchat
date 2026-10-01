package api

// E2EE routes mirror cqim-app/server/crypto.ts and prekey-bundle.ts.
// Public keys stay in SystemConfig; private keys never reach this service.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	e2eePublicKeyRE        = regexp.MustCompile(`^[A-Za-z0-9+/=]+$`)
	errDatabaseUnavailable = errors.New("DATABASE_URL is not configured")
)

type e2eePreKey struct {
	KeyID     any    `json:"keyId"`
	PublicKey string `json:"publicKey"`
}

const maxConsumedPreKeyIDs = 2048

func numericPreKeyID(value any) (float64, bool) {
	id, ok := value.(float64)
	return id, ok && !math.IsNaN(id) && !math.IsInf(id, 0)
}

func preKeyIDToken(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func normalizeConsumedPreKeyIDs(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	seen := make(map[string]bool, len(items))
	out := make([]any, 0, len(items))
	for _, item := range items {
		if _, valid := numericPreKeyID(item); !valid {
			continue
		}
		token := preKeyIDToken(item)
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, item)
	}
	if len(out) > maxConsumedPreKeyIDs {
		out = out[len(out)-maxConsumedPreKeyIDs:]
	}
	return out
}

func rememberConsumedPreKey(value any, keyID any) []any {
	items := append(normalizeConsumedPreKeyIDs(value), keyID)
	return normalizeConsumedPreKeyIDs(items)
}

func consumedPreKeySet(value any) map[string]bool {
	consumed := normalizeConsumedPreKeyIDs(value)
	out := make(map[string]bool, len(consumed))
	for _, item := range consumed {
		out[preKeyIDToken(item)] = true
	}
	return out
}

func isValidP256SPKIPublicKey(value string) bool {
	der, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(der) == 0 {
		return false
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return false
	}
	pub, ok := parsed.(*ecdsa.PublicKey)
	return ok && pub.Curve != nil && pub.Curve.Params().Name == elliptic.P256().Params().Name
}

func filterValidP256PreKeys(keys []e2eePreKey) []e2eePreKey {
	out := make([]e2eePreKey, 0, len(keys))
	for _, item := range keys {
		if _, ok := numericPreKeyID(item.KeyID); ok && isValidP256SPKIPublicKey(item.PublicKey) {
			out = append(out, item)
		}
	}
	return out
}

func takeOneValidP256PreKey(keys []e2eePreKey, consumedIDs any) (*e2eePreKey, []e2eePreKey) {
	valid := filterValidP256PreKeys(keys)
	consumed := consumedPreKeySet(consumedIDs)
	available := make([]e2eePreKey, 0, len(valid))
	for _, item := range valid {
		if !consumed[preKeyIDToken(item.KeyID)] {
			available = append(available, item)
		}
	}
	if len(available) == 0 {
		return nil, nil
	}
	taken := available[0]
	return &taken, available[1:]
}

func signedPreKeyPublicKey(value any) string {
	item, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	publicKey, _ := item["publicKey"].(string)
	return publicKey
}

func (s *Server) registerCryptoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/crypto/register-key", s.cryptoRegisterKey)
	mux.HandleFunc("GET /api/crypto/get-key", s.cryptoGetKey)
	mux.HandleFunc("POST /api/crypto/verify-message", s.cryptoVerifyMessage)
	mux.HandleFunc("POST /api/crypto/register-bundle", s.cryptoRegisterBundle)
	mux.HandleFunc("GET /api/crypto/get-bundle", s.cryptoGetBundle)
	mux.HandleFunc("GET /api/crypto/prekey-count", s.cryptoPreKeyCount)
	mux.HandleFunc("POST /api/crypto/replenish-prekeys", s.cryptoReplenishPreKeys)
	// Catch-all keeps unknown crypto paths as JSON, never net/http "404 page not found".
	mux.HandleFunc("/api/crypto/", s.cryptoNotFound)
}

func (s *Server) cryptoNotFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "未找到该加密接口"})
}

// resolveSigningPublicKey matches server/prekey-bundle.ts: keep a stored ECDSA
// signing key when an older client omits the field, but drop it after identity rotation.
func resolveSigningPublicKey(incoming any, identityKey, existingValue string) *string {
	if s, ok := incoming.(string); ok && s != "" {
		return &s
	}
	if existingValue == "" {
		return nil
	}
	var prev struct {
		IdentityKey      string `json:"identityKey"`
		SigningPublicKey string `json:"signingPublicKey"`
	}
	if json.Unmarshal([]byte(existingValue), &prev) != nil {
		return nil
	}
	if prev.IdentityKey == identityKey && prev.SigningPublicKey != "" {
		v := prev.SigningPublicKey
		return &v
	}
	return nil
}

func generateHMAC(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyHMAC(message, signature, secret string) bool {
	expected := generateHMAC(message, secret)
	sig, err1 := hex.DecodeString(signature)
	exp, err2 := hex.DecodeString(expected)
	if err1 != nil || err2 != nil || len(sig) != len(exp) {
		return false
	}
	return hmac.Equal(sig, exp)
}

func (s *Server) systemConfigValue(ctx context.Context, key string) (string, bool, error) {
	if s.db == nil {
		return "", false, errDatabaseUnavailable
	}
	var value string
	err := s.db.QueryRow(ctx, `SELECT "value" FROM "SystemConfig" WHERE "key"=$1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *Server) upsertSystemConfig(ctx context.Context, key, value string) error {
	if s.db == nil {
		return errDatabaseUnavailable
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO "SystemConfig" ("key","value","updatedAt") VALUES ($1,$2,NOW())
		ON CONFLICT ("key") DO UPDATE SET "value"=EXCLUDED."value","updatedAt"=NOW()`, key, value)
	return err
}

func mergePreKeys(existing, incoming []e2eePreKey) []e2eePreKey {
	return mergePreKeysExcludingConsumed(existing, incoming, nil)
}

func mergePreKeysExcludingConsumed(existing, incoming []e2eePreKey, consumedIDs any) []e2eePreKey {
	seen := map[string]int{}
	consumed := consumedPreKeySet(consumedIDs)
	out := make([]e2eePreKey, 0, len(existing)+len(incoming))
	add := func(item e2eePreKey) {
		if _, ok := numericPreKeyID(item.KeyID); !ok || !isValidP256SPKIPublicKey(item.PublicKey) {
			return
		}
		key := preKeyIDToken(item.KeyID)
		if consumed[key] {
			return
		}
		if i, ok := seen[key]; ok {
			out[i] = item
			return
		}
		seen[key] = len(out)
		out = append(out, item)
	}
	for _, item := range existing {
		add(item)
	}
	for _, item := range incoming {
		add(item)
	}
	return out
}

func parsePreKeys(raw string) []e2eePreKey {
	if raw == "" {
		return nil
	}
	var keys []e2eePreKey
	if json.Unmarshal([]byte(raw), &keys) != nil {
		return nil
	}
	return keys
}

func (s *Server) cryptoRegisterKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID    string `json:"userId"`
		PublicKey string `json:"publicKey"`
		DeviceID  string `json:"deviceId"`
	}
	if decode(r, &in) != nil || in.UserID == "" || in.PublicKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少必要参数"})
		return
	}
	if !e2eePublicKeyRE.MatchString(in.PublicKey) || len(in.PublicKey) < 40 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "公钥格式无效"})
		return
	}
	if in.DeviceID == "" {
		in.DeviceID = "default"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	existing, found, err := s.systemConfigValue(r.Context(), "e2ee:pubkey:"+in.UserID+":"+in.DeviceID)
	if err != nil {
		log.Printf("[Crypto] 注册公钥失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "注册公钥失败"})
		return
	}
	payload := map[string]string{"publicKey": in.PublicKey, "updatedAt": now, "createdAt": now}
	if found {
		var prev map[string]string
		if json.Unmarshal([]byte(existing), &prev) == nil && prev["createdAt"] != "" {
			payload["createdAt"] = prev["createdAt"]
		}
	}
	raw, _ := json.Marshal(payload)
	if err := s.upsertSystemConfig(r.Context(), "e2ee:pubkey:"+in.UserID+":"+in.DeviceID, string(raw)); err != nil {
		log.Printf("[Crypto] 注册公钥失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "注册公钥失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) cryptoGetKey(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	deviceID := r.URL.Query().Get("deviceId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 userId"})
		return
	}
	if deviceID == "" {
		deviceID = "default"
	}
	raw, found, err := s.systemConfigValue(r.Context(), "e2ee:pubkey:"+userID+":"+deviceID)
	if err != nil {
		log.Printf("[Crypto] 获取公钥失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "获取公钥失败"})
		return
	}
	if !found || raw == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未找到公钥"})
		return
	}
	var data struct {
		PublicKey string `json:"publicKey"`
		UpdatedAt string `json:"updatedAt"`
	}
	if json.Unmarshal([]byte(raw), &data) != nil || data.PublicKey == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "获取公钥失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"publicKey": data.PublicKey, "updatedAt": data.UpdatedAt})
}

func (s *Server) cryptoVerifyMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Message   string `json:"message"`
		Signature string `json:"signature"`
		SenderID  string `json:"senderId"`
	}
	if decode(r, &in) != nil || in.Message == "" || in.Signature == "" || in.SenderID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少必要参数"})
		return
	}
	raw, found, err := s.systemConfigValue(r.Context(), "e2ee:pubkey:"+in.SenderID+":default")
	if err != nil {
		log.Printf("[Crypto] 验证签名失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "验证失败"})
		return
	}
	if !found || raw == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "发送者公钥未注册"})
		return
	}
	var data struct {
		PublicKey string `json:"publicKey"`
	}
	if json.Unmarshal([]byte(raw), &data) != nil || data.PublicKey == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "验证失败"})
		return
	}
	sum := sha256.Sum256([]byte(data.PublicKey))
	writeJSON(w, http.StatusOK, map[string]bool{"valid": verifyHMAC(in.Message, in.Signature, hex.EncodeToString(sum[:]))})
}

func (s *Server) cryptoRegisterBundle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID           string       `json:"userId"`
		RegistrationID   any          `json:"registrationId"`
		IdentityKey      string       `json:"identityKey"`
		SigningPublicKey any          `json:"signingPublicKey"`
		SignedPreKey     any          `json:"signedPreKey"`
		PreKeys          []e2eePreKey `json:"preKeys"`
	}
	if decode(r, &in) != nil || in.UserID == "" || in.IdentityKey == "" || in.SignedPreKey == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少必要参数"})
		return
	}
	existing, _, err := s.systemConfigValue(r.Context(), "e2ee:bundle:"+in.UserID)
	if err != nil {
		log.Printf("[Crypto] 注册 Bundle 失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "注册 Bundle 失败"})
		return
	}
	storedSigningPublicKey := resolveSigningPublicKey(in.SigningPublicKey, in.IdentityKey, existing)
	if !isValidP256SPKIPublicKey(in.IdentityKey) || !isValidP256SPKIPublicKey(signedPreKeyPublicKey(in.SignedPreKey)) || storedSigningPublicKey == nil || !isValidP256SPKIPublicKey(*storedSigningPublicKey) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "E2EE Bundle 公钥格式无效"})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	previousBundle := map[string]any{}
	if existing != "" {
		_ = json.Unmarshal([]byte(existing), &previousBundle)
	}
	sameIdentity := previousBundle["identityKey"] == in.IdentityKey
	consumedPreKeyIDs := []any{}
	if sameIdentity {
		consumedPreKeyIDs = normalizeConsumedPreKeyIDs(previousBundle["consumedPreKeyIds"])
	}
	bundle := map[string]any{
		"registrationId":    in.RegistrationID,
		"identityKey":       in.IdentityKey,
		"signingPublicKey":  storedSigningPublicKey,
		"signedPreKey":      in.SignedPreKey,
		"consumedPreKeyIds": consumedPreKeyIDs,
		"updatedAt":         now,
	}
	if existing == "" {
		bundle["createdAt"] = now
	} else {
		var prev map[string]any
		if json.Unmarshal([]byte(existing), &prev) == nil {
			if created, ok := prev["createdAt"]; ok {
				bundle["createdAt"] = created
			}
		}
	}
	raw, _ := json.Marshal(bundle)
	if err := s.upsertSystemConfig(r.Context(), "e2ee:bundle:"+in.UserID, string(raw)); err != nil {
		log.Printf("[Crypto] 注册 Bundle 失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "注册 Bundle 失败"})
		return
	}
	if _, err := s.mergeStoredPreKeys(r.Context(), in.UserID, in.PreKeys, sameIdentity, consumedPreKeyIDs); err != nil {
		log.Printf("[Crypto] 注册 Bundle 失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "注册 Bundle 失败"})
		return
	}
	log.Printf("[Crypto] 用户 %s 注册 PreKey Bundle 成功, preKeys: %d", in.UserID, len(in.PreKeys))
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) cryptoGetBundle(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 userId"})
		return
	}
	raw, found, err := s.systemConfigValue(r.Context(), "e2ee:bundle:"+userID)
	if err != nil {
		log.Printf("[Crypto] 获取 Bundle 失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "获取 Bundle 失败"})
		return
	}
	if !found || raw == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "用户未注册 E2EE Bundle"})
		return
	}
	var bundle map[string]any
	if json.Unmarshal([]byte(raw), &bundle) != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "获取 Bundle 失败"})
		return
	}
	identityKey, _ := bundle["identityKey"].(string)
	signingPublicKey, _ := bundle["signingPublicKey"].(string)
	if !isValidP256SPKIPublicKey(identityKey) || !isValidP256SPKIPublicKey(signingPublicKey) || !isValidP256SPKIPublicKey(signedPreKeyPublicKey(bundle["signedPreKey"])) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "E2EE Bundle 格式无效，请重新注册"})
		return
	}
	var oneTime *e2eePreKey
	preRaw, preFound, err := s.systemConfigValue(r.Context(), "e2ee:prekeys:"+userID)
	if err != nil {
		log.Printf("[Crypto] 获取 Bundle 失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "获取 Bundle 失败"})
		return
	}
	if preFound && preRaw != "" {
		var preKeys []e2eePreKey
		if err := json.Unmarshal([]byte(preRaw), &preKeys); err != nil {
			log.Printf("[Crypto] 获取 Bundle 失败: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "获取 Bundle 失败"})
			return
		}
		var remainingPreKeys []e2eePreKey
		oneTime, remainingPreKeys = takeOneValidP256PreKey(preKeys, bundle["consumedPreKeyIds"])
		remaining, _ := json.Marshal(remainingPreKeys)
		if oneTime != nil {
			bundle["consumedPreKeyIds"] = rememberConsumedPreKey(bundle["consumedPreKeyIds"], oneTime.KeyID)
			bundle["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			bundleRaw, _ := json.Marshal(bundle)
			if err := s.persistPreKeyConsumption(r.Context(), userID, string(remaining), string(bundleRaw)); err != nil {
				log.Printf("[Crypto] 获取 Bundle 失败: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "获取 Bundle 失败"})
				return
			}
		} else if err := s.upsertSystemConfig(r.Context(), "e2ee:prekeys:"+userID, string(remaining)); err != nil {
			log.Printf("[Crypto] 获取 Bundle 失败: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "获取 Bundle 失败"})
			return
		}
		if oneTime != nil {
			log.Printf("[Crypto] 消费用户 %s 的合法 PreKey, 剩余: %d", userID, len(remainingPreKeys))
		}
	}
	if oneTime == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "用户暂无可用的一次性 E2EE 密钥，请稍后重试"})
		return
	}
	signing := bundle["signingPublicKey"]
	if signing == nil || signing == "" {
		signing = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"registrationId":   bundle["registrationId"],
		"identityKey":      bundle["identityKey"],
		"signingPublicKey": signing,
		"signedPreKey":     bundle["signedPreKey"],
		"preKey":           oneTime,
	})
}

func (s *Server) cryptoPreKeyCount(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 userId"})
		return
	}
	raw, found, err := s.systemConfigValue(r.Context(), "e2ee:prekeys:"+userID)
	if err != nil {
		log.Printf("[Crypto] 查询 PreKey 数量失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询失败"})
		return
	}
	count := 0
	if found && raw != "" {
		var preKeys []e2eePreKey
		if json.Unmarshal([]byte(raw), &preKeys) == nil {
			bundleRaw, _, _ := s.systemConfigValue(r.Context(), "e2ee:bundle:"+userID)
			var bundle map[string]any
			_ = json.Unmarshal([]byte(bundleRaw), &bundle)
			count = len(mergePreKeysExcludingConsumed(nil, preKeys, bundle["consumedPreKeyIds"]))
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

func (s *Server) cryptoReplenishPreKeys(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID  string       `json:"userId"`
		PreKeys []e2eePreKey `json:"preKeys"`
	}
	if decode(r, &in) != nil || in.UserID == "" || len(in.PreKeys) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少必要参数"})
		return
	}
	validIncoming := filterValidP256PreKeys(in.PreKeys)
	if len(validIncoming) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有有效的 P-256 PreKey"})
		return
	}
	bundleRaw, _, err := s.systemConfigValue(r.Context(), "e2ee:bundle:"+in.UserID)
	if err != nil {
		log.Printf("[Crypto] 补充 PreKeys 失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "补充失败"})
		return
	}
	var bundle map[string]any
	_ = json.Unmarshal([]byte(bundleRaw), &bundle)
	merged, err := s.mergeStoredPreKeys(r.Context(), in.UserID, validIncoming, true, bundle["consumedPreKeyIds"])
	if err != nil {
		log.Printf("[Crypto] 补充 PreKeys 失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "补充失败"})
		return
	}
	log.Printf("[Crypto] 用户 %s 补充 %d 个 PreKeys, 总计: %d", in.UserID, len(validIncoming), len(merged))
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "totalCount": len(merged)})
}

func (s *Server) mergeStoredPreKeys(ctx context.Context, userID string, incoming []e2eePreKey, keepExisting bool, consumedIDs any) ([]e2eePreKey, error) {
	raw, _, err := s.systemConfigValue(ctx, "e2ee:prekeys:"+userID)
	if err != nil {
		return nil, err
	}
	var existing []e2eePreKey
	if keepExisting {
		existing = parsePreKeys(raw)
	}
	merged := mergePreKeysExcludingConsumed(existing, incoming, consumedIDs)
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	if err := s.upsertSystemConfig(ctx, "e2ee:prekeys:"+userID, string(out)); err != nil {
		return nil, err
	}
	return merged, nil
}

func (s *Server) persistPreKeyConsumption(ctx context.Context, userID, preKeysValue, bundleValue string) error {
	if s.db == nil {
		return errDatabaseUnavailable
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for key, value := range map[string]string{
		"e2ee:prekeys:" + userID: preKeysValue,
		"e2ee:bundle:" + userID:  bundleValue,
	} {
		if _, err := tx.Exec(ctx, `
			INSERT INTO "SystemConfig" ("key","value","updatedAt") VALUES ($1,$2,NOW())
			ON CONFLICT ("key") DO UPDATE SET "value"=EXCLUDED."value","updatedAt"=NOW()`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
