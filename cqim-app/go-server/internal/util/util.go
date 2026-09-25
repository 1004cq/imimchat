package util

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// BcryptRounds 与 Node 端 db.ts 的 BCRYPT_ROUNDS=12 保持一致。
const BcryptRounds = 12

// HashPassword bcrypt 哈希（cost 12，与 Node 版兼容可互相验证）。
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), BcryptRounds)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// VerifyPassword 验证密码。兼容 bcrypt 与旧版哈希（旧版直接返回 false，由调用方处理迁移）。
func VerifyPassword(password, hash string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateToken 生成与 Node 版 generateToken 等价的 base64url 随机串。
func GenerateToken(length int) string {
	if length <= 0 {
		length = 32
	}
	var sb strings.Builder
	for sb.Len() < length {
		n := (length - sb.Len() + 2) / 3 * 3 // 向上取整到 3 的倍数再转 base64
		if n < 3 {
			n = 3
		}
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			// 极端降级：时间戳 + 原子计数
			sb.WriteString(time.Now().Format("150405.000000000"))
			sb.WriteString(itoa(atomic.AddUint64(&fallbackCounter, 1)))
			continue
		}
		sb.WriteString(base64.RawURLEncoding.EncodeToString(b))
	}
	s := sb.String()
	if len(s) > length {
		s = s[:length]
	}
	return s
}

var fallbackCounter uint64

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// NewID 生成 cuid 兼容风格的唯一 ID（25 字符，以 c 开头）。
// 不要求与 Node 的 cuid 库逐字符相同，只要求唯一且格式稳定。
func NewID() string {
	// cuid: c + 时间戳(base36) + 计数器(base36,4位) + 指纹(4位) + 随机(8位)
	ts := strings.ToLower(base36(uint64(time.Now().UnixMilli())))
	ctr := strings.ToLower(base36(atomic.AddUint64(&idCounter, 1) % 1679616))
	for len(ctr) < 4 {
		ctr = "0" + ctr
	}
	var rb [4]byte
	rand.Read(rb[:])
	rnd := strings.ToLower(base36(uint64(rb[0])<<24 | uint64(rb[1])<<16 | uint64(rb[2])<<8 | uint64(rb[3])))
	for len(rnd) < 4 {
		rnd = "0" + rnd
	}
	fp := "go01" // 进程指纹占位
	id := "c" + ts + ctr + fp + rnd
	if len(id) > 25 {
		id = id[:25]
	}
	return id
}

var idCounter uint64

func base36(n uint64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	var b [13]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = digits[n%36]
		n /= 36
	}
	return string(b[i:])
}

// WriteJSON 写 JSON 响应。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError 写 {error: msg} 错误响应。
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// DecodeJSON 解析请求体 JSON，失败返回 false（调用方已写 400）。
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20) // 50MB 上限（voice/media 场景）
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		WriteError(w, http.StatusBadRequest, "请求参数格式错误")
		return false
	}
	return true
}

// StrPtr / StrVal 可空字符串 helpers。
func StrPtr(s string) *string { return &s }
func StrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
