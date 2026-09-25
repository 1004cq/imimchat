// Package push — web-push.ts 移植：浏览器 Web Push 注册与加密消息唤醒。
//
// 服务器在此绝不接触消息明文：推送载荷只携带路由元数据，客户端唤醒后自行拉取密文。
//
// 路由：
//   - GET    /api/web-push/public-key   （公开，浏览器创建 PushSubscription 前需要）
//   - POST   /api/web-push/subscription （userAuth）
//   - DELETE /api/web-push/subscription （userAuth）
//
// 导出函数：SendWebPush。
// 加密实现 RFC 8291（aes128gcm）+ RFC 8292（VAPID）：仅用标准库 crypto/ecdh、
// crypto/aes 与 golang.org/x/crypto/hkdf，不引入新依赖。
package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============================================================
// VAPID 配置（环境变量，与 TS 版一致；config 不含这些键，故直接读 env）
// ============================================================

type webPushConfig struct {
	publicKey  string // base64url，65 字节未压缩 P-256 公钥
	privateKey string // base64url，32 字节私钥标量
	subject    string
	enabled    bool
}

func loadWebPushConfig() webPushConfig {
	pub := os.Getenv("WEB_PUSH_VAPID_PUBLIC_KEY")
	priv := os.Getenv("WEB_PUSH_VAPID_PRIVATE_KEY")
	subj := os.Getenv("WEB_PUSH_VAPID_SUBJECT")
	if subj == "" {
		subj = "mailto:security@imim.chat"
	}
	return webPushConfig{
		publicKey:  pub,
		privateKey: priv,
		subject:    subj,
		enabled:    pub != "" && priv != "",
	}
}

// ============================================================
// 订阅校验与存储
// ============================================================

type webPushKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type webPushSubscription struct {
	Endpoint       string      `json:"endpoint"`
	ExpirationTime *int64      `json:"expirationTime"`
	Keys           webPushKeys `json:"keys"`
}

// normalizeSubscription 与 TS getSubscription 同语义校验并规范化。
func normalizeSubscription(raw json.RawMessage) (*webPushSubscription, bool) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	ep, _ := m["endpoint"].(string)
	if ep == "" || !strings.HasPrefix(ep, "https://") {
		return nil, false
	}
	keys, _ := m["keys"].(map[string]any)
	if keys == nil {
		return nil, false
	}
	p256dh, _ := keys["p256dh"].(string)
	auth, _ := keys["auth"].(string)
	if p256dh == "" || auth == "" {
		return nil, false
	}
	sub := &webPushSubscription{Endpoint: ep, Keys: webPushKeys{P256dh: p256dh, Auth: auth}}
	if f, ok := m["expirationTime"].(float64); ok {
		n := int64(f)
		sub.ExpirationTime = &n
	}
	return sub, true
}

// GET /api/web-push/public-key（公开路由，浏览器创建订阅前需要）
func (h *Handler) webPushPublicKey(w http.ResponseWriter, r *http.Request) {
	cfg := loadWebPushConfig()
	if !cfg.enabled {
		util.WriteJSON(w, 503, map[string]any{"enabled": false})
		return
	}
	util.WriteJSON(w, 200, map[string]any{"enabled": true, "publicKey": cfg.publicKey})
}

// POST /api/web-push/subscription 保存订阅
func (h *Handler) saveSubscription(w http.ResponseWriter, r *http.Request) {
	if !loadWebPushConfig().enabled {
		util.WriteError(w, 503, "Web Push 未配置")
		return
	}
	var req struct {
		Subscription json.RawMessage `json:"subscription"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	sub, ok := normalizeSubscription(req.Subscription)
	if !ok {
		util.WriteError(w, 400, "无效的 PushSubscription")
		return
	}

	data, err := json.Marshal(sub)
	if err != nil {
		util.WriteError(w, 500, "订阅保存失败")
		return
	}
	u := middleware.UserFrom(r)
	if _, err := h.deps.DB.Exec(r.Context(),
		`UPDATE "User" SET "webPushSubscription"=$1,"updatedAt"=NOW() WHERE "id"=$2`,
		string(data), u.Id); err != nil {
		log.Printf("[WebPush] 订阅保存失败: %v", err)
		util.WriteError(w, 500, "订阅保存失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// DELETE /api/web-push/subscription 清理订阅
func (h *Handler) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	if _, err := h.deps.DB.Exec(r.Context(),
		`UPDATE "User" SET "webPushSubscription"=NULL,"updatedAt"=NOW() WHERE "id"=$1`, u.Id); err != nil {
		log.Printf("[WebPush] 订阅清理失败: %v", err)
		util.WriteError(w, 500, "订阅清理失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// RFC 8291 消息加密（aes128gcm）
// ============================================================

func hkdfExpand(prk, info []byte, n int) ([]byte, error) {
	out := make([]byte, n)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prk, info), out); err != nil {
		return nil, err
	}
	return out, nil
}

// encryptWebPushPayload 按 RFC 8291 §3.4（aes128gcm 内容编码）加密载荷。
func encryptWebPushPayload(sub *webPushSubscription, plaintext []byte) ([]byte, error) {
	peerPubBytes, err := base64.RawURLEncoding.DecodeString(sub.Keys.P256dh)
	if err != nil {
		return nil, err
	}
	if _, err := base64.RawURLEncoding.DecodeString(sub.Keys.Auth); err != nil {
		return nil, err
	}
	peerPub, err := ecdh.P256().NewPublicKey(peerPubBytes)
	if err != nil {
		return nil, err
	}

	ephPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	ephPubBytes := ephPriv.PublicKey().Bytes() // 65 字节未压缩点

	shared, err := ephPriv.ECDH(peerPub) // IKM：32 字节共享密钥 x 坐标
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	keyInfo := append([]byte("WebPush: info\x00"), peerPubBytes...)
	keyInfo = append(keyInfo, ephPubBytes...)
	prk := hkdf.Extract(sha256.New, shared, salt)
	cek, err := hkdfExpand(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdfExpand(prk, []byte("Content-Encoding: nonce\x00"), 12)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	padded := append(append([]byte{}, plaintext...), 0x02) // 0x02 分隔符
	sealed := gcm.Seal(nil, nonce, padded, nil)

	out := make([]byte, 0, 16+4+1+len(ephPubBytes)+len(sealed))
	out = append(out, salt...)
	var rs [4]byte
	binary.BigEndian.PutUint32(rs[:], 4096)
	out = append(out, rs[:]...)
	out = append(out, byte(len(ephPubBytes)))
	out = append(out, ephPubBytes...)
	out = append(out, sealed...)
	return out, nil
}

// vapidPrivateKey 由 base64url 私钥标量构造 P-256 私钥。
func vapidPrivateKey(cfg webPushConfig) (*ecdsa.PrivateKey, error) {
	dBytes, err := base64.RawURLEncoding.DecodeString(cfg.privateKey)
	if err != nil {
		return nil, err
	}
	curve := elliptic.P256()
	x, y := curve.ScalarBaseMult(dBytes)
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y},
		D:         new(big.Int).SetBytes(dBytes),
	}, nil
}

// vapidJWT 生成 VAPID 认证 JWT（RFC 8292），有效期 12 小时。
func vapidJWT(cfg webPushConfig, audience string) (string, error) {
	priv, err := vapidPrivateKey(cfg)
	if err != nil {
		return "", err
	}
	hb, _ := json.Marshal(map[string]string{"typ": "JWT", "alg": "ES256"})
	cb, _ := json.Marshal(map[string]any{
		"aud": audience,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": cfg.subject,
	})
	signingInput := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)
	sig, err := signES256(signingInput, priv)
	if err != nil {
		return "", err
	}
	return signingInput + "." + sig, nil
}

// postWebPush 发送加密推送，返回推送服务的状态码。
func postWebPush(cfg webPushConfig, endpoint string, body []byte) (int, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return 0, err
	}
	jwt, err := vapidJWT(cfg, u.Scheme+"://"+u.Host)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("TTL", "90")
	req.Header.Set("Urgency", "high")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+cfg.publicKey)

	resp, err := apnsHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, nil
}

// ============================================================
// 导出函数
// ============================================================

// WebPushMessageParams 对应 TS sendWebPush 参数。
// 注意：只允许路由元数据（chatId/messageId/senderId），不接受任何正文，
// 从 API 层面杜绝明文消息被推送出去的可能。
type WebPushMessageParams struct {
	ToUserID  string
	ChatID    string
	MessageID string
	SenderID  string
}

type webPushSubRow struct {
	WebPushSubscription *string `db:"webPushSubscription"`
}

// SendWebPush 发送加密消息唤醒推送。无订阅/未配置时返回 (false, nil)；
func SendWebPush(d *handler.Deps, p WebPushMessageParams) (bool, error) {
	cfg := loadWebPushConfig()
	if !cfg.enabled {
		return false, nil
	}

	ctx := context.Background()
	row, err := db.QueryRowToStruct[webPushSubRow](ctx, d.DB,
		`SELECT "webPushSubscription" FROM "User" WHERE "id"=$1`, p.ToUserID)
	if err != nil {
		if db.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if row.WebPushSubscription == nil || *row.WebPushSubscription == "" {
		return false, nil
	}

	sub, ok := normalizeSubscription(json.RawMessage(*row.WebPushSubscription))
	if !ok {
		return false, nil
	}

	payload, _ := json.Marshal(map[string]any{
		"type":      "encrypted_message",
		"chatId":    p.ChatID,
		"messageId": p.MessageID,
		"senderId":  p.SenderID,
		"encrypted": true,
	})
	enc, err := encryptWebPushPayload(sub, payload)
	if err != nil {
		log.Printf("[WebPush] 加密失败: %v", err)
		return false, err
	}

	status, err := postWebPush(cfg, sub.Endpoint, enc)
	if err != nil {
		log.Printf("[WebPush] 推送失败: %v", err)
		return false, nil
	}
	if status == http.StatusNotFound || status == http.StatusGone {
		// 订阅已失效：清理，避免之后每次都尝试
		_, _ = d.DB.Exec(ctx, `UPDATE "User" SET "webPushSubscription"=NULL,"updatedAt"=NOW() WHERE "id"=$1`, p.ToUserID)
		return false, nil
	}
	if status < 200 || status >= 300 {
		log.Printf("[WebPush] 推送失败: status=%d", status)
		return false, nil
	}
	return true, nil
}
