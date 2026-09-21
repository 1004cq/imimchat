package api

// E2EE routes mirror cqim-app/server/crypto.ts and prekey-bundle.ts.
// Public keys stay in SystemConfig; private keys never reach this service.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
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
	seen := map[string]int{}
	out := make([]e2eePreKey, 0, len(existing)+len(incoming))
	add := func(item e2eePreKey) {
		id, _ := json.Marshal(item.KeyID)
		key := string(id)
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	bundle := map[string]any{
		"registrationId":   in.RegistrationID,
		"identityKey":      in.IdentityKey,
		"signingPublicKey": resolveSigningPublicKey(in.SigningPublicKey, in.IdentityKey, existing),
		"signedPreKey":     in.SignedPreKey,
		"updatedAt":        now,
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
	if len(in.PreKeys) > 0 {
		if _, err := s.mergeStoredPreKeys(r.Context(), in.UserID, in.PreKeys); err != nil {
			log.Printf("[Crypto] 注册 Bundle 失败: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "注册 Bundle 失败"})
			return
		}
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
	var oneTime any
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
		if len(preKeys) > 0 {
			oneTime = preKeys[0]
			remaining, _ := json.Marshal(preKeys[1:])
			if err := s.upsertSystemConfig(r.Context(), "e2ee:prekeys:"+userID, string(remaining)); err != nil {
				log.Printf("[Crypto] 获取 Bundle 失败: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "获取 Bundle 失败"})
				return
			}
			log.Printf("[Crypto] 消费用户 %s 的 PreKey #%v, 剩余: %d", userID, preKeys[0].KeyID, len(preKeys)-1)
		}
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
		var preKeys []any
		if json.Unmarshal([]byte(raw), &preKeys) == nil {
			count = len(preKeys)
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
	merged, err := s.mergeStoredPreKeys(r.Context(), in.UserID, in.PreKeys)
	if err != nil {
		log.Printf("[Crypto] 补充 PreKeys 失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "补充失败"})
		return
	}
	log.Printf("[Crypto] 用户 %s 补充 %d 个 PreKeys, 总计: %d", in.UserID, len(in.PreKeys), len(merged))
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "totalCount": len(merged)})
}

func (s *Server) mergeStoredPreKeys(ctx context.Context, userID string, incoming []e2eePreKey) ([]e2eePreKey, error) {
	raw, _, err := s.systemConfigValue(ctx, "e2ee:prekeys:"+userID)
	if err != nil {
		return nil, err
	}
	merged := mergePreKeys(parsePreKeys(raw), incoming)
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	if err := s.upsertSystemConfig(ctx, "e2ee:prekeys:"+userID, string(out)); err != nil {
		return nil, err
	}
	return merged, nil
}
