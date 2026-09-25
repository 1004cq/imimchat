package push

import (
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
	"math/big"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/hkdf"
)

// 验证 ES256 JWT 能被对应公钥验签。
func TestES256JWTVerify(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	hb, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": "K1"})
	cb, _ := json.Marshal(map[string]any{"iss": "TEAM", "iat": time.Now().Unix()})
	input := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)
	sigB64, err := signES256(input, priv)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(sigB64)
	if len(raw) != 64 {
		t.Fatalf("raw sig len=%d", len(raw))
	}
	r := new(big.Int).SetBytes(raw[:32])
	s := new(big.Int).SetBytes(raw[32:])
	h := sha256.Sum256([]byte(input))
	if !ecdsa.Verify(&priv.PublicKey, h[:], r, s) {
		t.Fatal("signature verify failed")
	}
}

// 验证 RFC 8291 加密输出结构，并用对端私钥解密验证。
func TestWebPushEncryptDecrypt(t *testing.T) {
	peerPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	peerPub := peerPriv.PublicKey().Bytes()
	auth := make([]byte, 16)
	rand.Read(auth)
	sub := &webPushSubscription{
		Endpoint: "https://example.com/push",
		Keys: webPushKeys{
			P256dh: base64.RawURLEncoding.EncodeToString(peerPub),
			Auth:   base64.RawURLEncoding.EncodeToString(auth),
		},
	}
	plaintext := []byte(`{"type":"encrypted_message"}`)
	enc, err := encryptWebPushPayload(sub, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	// 解析 header: salt(16) | rs(4) | idlen(1) | pubkey(65)
	if len(enc) < 86 {
		t.Fatalf("too short: %d", len(enc))
	}
	salt := enc[:16]
	if got := binary.BigEndian.Uint32(enc[16:20]); got != 4096 {
		t.Fatalf("rs=%d", got)
	}
	if idlen := int(enc[20]); idlen != 65 {
		t.Fatalf("idlen=%d", idlen)
	}
	ephPubBytes := enc[21 : 21+65]
	sealed := enc[21+65:]

	// 解密：重建 PRK
	ephPub, _ := ecdh.P256().NewPublicKey(ephPubBytes)
	shared, _ := peerPriv.ECDH(ephPub)
	keyInfo := append([]byte("WebPush: info\x00"), peerPub...)
	keyInfo = append(keyInfo, ephPubBytes...)
	prk := hkdf.Extract(sha256.New, shared, salt)
	cek, _ := hkdfExpand(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce, _ := hkdfExpand(prk, []byte("Content-Encoding: nonce\x00"), 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	pt, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(pt), "\x02") || string(pt[:len(pt)-1]) != string(plaintext) {
		t.Fatalf("decrypt mismatch: %q", pt)
	}
}
