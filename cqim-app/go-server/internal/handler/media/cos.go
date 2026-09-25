package media

// cos.go 移植自 server/cos-signer.ts。
//
// 腾讯云 COS 签名 URL 工具。Node 版使用 cos-nodejs-sdk-v5 的 getObjectUrl；
// Go 版不允许引入新依赖，因此用标准库实现相同的 q-sign 签名算法
// （HMAC-SHA1，见腾讯云文档「请求签名」与 SDK 的 getAuth 逻辑）：
//
//	KeyTime      = start;end（unix 秒）
//	SignKey      = HMAC-SHA1(SecretKey, KeyTime)
//	HttpString   = method\n + pathname\n + headers\n + params\n
//	StringToSign = "sha1\n" + KeyTime + "\n" + SHA1(HttpString) + "\n"
//	Signature    = HMAC-SHA1(SignKey, StringToSign)
//
// 最终 URL 形如：
// https://{bucket}.cos.{region}.myqcloud.com/{key}?q-sign-algorithm=sha1&q-ak=...&...

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// ============ 系统配置缓存（5 分钟） ============

const cosConfigTTL = 5 * time.Minute

var cosConfigCache = struct {
	sync.Mutex
	value    map[string]string
	expireAt time.Time
	set      bool
}{}

// cosCredsKey 对应 TS 的 cosClientCreds（凭据变化时重建签名上下文）。
var cosCredsKey = struct {
	sync.Mutex
	key string
}{}

var cosHTTPClient = &http.Client{Timeout: 15 * time.Second}

// GetCosConfig 读取 COS 配置（SystemConfig 表 key='cos' 的 JSON，环境变量兜底）。
// 对应 TS getCosConfig（含 5 分钟内存缓存；无配置时也缓存空值）。
func GetCosConfig(ctx context.Context, d *handler.Deps) map[string]string {
	cosConfigCache.Lock()
	if cosConfigCache.set && time.Now().Before(cosConfigCache.expireAt) {
		v := cosConfigCache.value
		cosConfigCache.Unlock()
		return v
	}
	cosConfigCache.Unlock()

	var value map[string]string
	row, err := db.QueryRowToStruct[db.SystemConfig](ctx, d.DB,
		`SELECT "key","value","updatedAt" FROM "SystemConfig" WHERE "key"='cos'`)
	if err == nil && row != nil && row.Value != "" {
		var raw map[string]any
		if jerr := json.Unmarshal([]byte(row.Value), &raw); jerr == nil {
			value = map[string]string{}
			for k, v := range raw {
				if s, ok := v.(string); ok {
					value[k] = s
				}
			}
		}
	}

	cosConfigCache.Lock()
	cosConfigCache.value = value
	cosConfigCache.expireAt = time.Now().Add(cosConfigTTL)
	cosConfigCache.set = true
	cosConfigCache.Unlock()
	return value
}

func cosCfgStr(cfg map[string]string, key, envKey, def string) string {
	if cfg != nil {
		if v := cfg[key]; v != "" {
			return v
		}
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return def
}

// InvalidateCosConfigCache 管理后台修改 COS 配置后调用，立即清缓存（对应 TS）。
func InvalidateCosConfigCache() {
	cosConfigCache.Lock()
	cosConfigCache.value = nil
	cosConfigCache.expireAt = time.Time{}
	cosConfigCache.set = false
	cosConfigCache.Unlock()

	signedURLCache.Lock()
	signedURLCache.entries = map[string]signedURLEntry{}
	signedURLCache.Unlock()

	cosCredsKey.Lock()
	cosCredsKey.key = ""
	cosCredsKey.Unlock()
}

// ============ q-sign 签名（HMAC-SHA1） ============

func hmacSHA1(key []byte, data string) []byte {
	m := hmac.New(sha1.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func sha1Hex(data string) string {
	sum := sha1.Sum([]byte(data))
	return hex.EncodeToString(sum[:])
}

// camSafeURLEncode 对应 SDK 的 camSafeUrlEncode：只保留 A-Za-z0-9-_.~，其余百分号编码。
func camSafeURLEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// encodeCosKey 编码对象 key（%2F 还原为 /，与 SDK 的 getObjectUrl 一致）。
func encodeCosKey(key string) string {
	return strings.ReplaceAll(camSafeURLEncode(key), "%2F", "/")
}

// cosObjToStr 对应 SDK 的 obj2str(obj, true)：key 按小写排序，
// key 经 camSafeUrlEncode 后转小写，value 经 camSafeUrlEncode。
func cosObjToStr(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return strings.ToLower(keys[i]) < strings.ToLower(keys[j])
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, strings.ToLower(camSafeURLEncode(k))+"="+camSafeURLEncode(m[k]))
	}
	return strings.Join(parts, "&")
}

// cosSignature 计算 q-signature，逐行对标 SDK 的 getAuth：
//   - FormatString = method\n + pathname(原始未编码路径)\n + params\n + headers\n
//   - SignKey = HMAC-SHA1(SecretKey, KeyTime) 的 hex 字符串（再以其 ASCII 字节为 key）
//   - StringToSign = "sha1\n" + KeyTime + "\n" + SHA1(FormatString) + "\n"
//   - Signature = HMAC-SHA1(SignKeyHex, StringToSign)
func cosSignature(secretKey, method, pathname string, headers, query map[string]string, keyTime string) string {
	lh := make(map[string]string, len(headers))
	for k, v := range headers {
		lh[strings.ToLower(k)] = v
	}
	lq := make(map[string]string, len(query))
	for k, v := range query {
		lq[strings.ToLower(k)] = v
	}
	formatString := strings.Join([]string{
		strings.ToLower(method),
		pathname,
		cosObjToStr(lq),
		cosObjToStr(lh),
		"",
	}, "\n")
	stringToSign := "sha1\n" + keyTime + "\n" + sha1Hex(formatString) + "\n"
	signKeyHex := hex.EncodeToString(hmacSHA1([]byte(secretKey), keyTime))
	return hex.EncodeToString(hmacSHA1([]byte(signKeyHex), stringToSign))
}

func cosHost(bucket, region string) string {
	return bucket + ".cos." + region + ".myqcloud.com"
}

// buildCosSignedURL 生成带签名的 COS URL（对应 SDK getObjectUrl Sign:true）。
// 签名覆盖 host 头（SDK 默认行为，避免跨桶访问）；pathname 参与签名的是原始路径，
// URL 上展示的是编码后路径。
func buildCosSignedURL(secretID, secretKey, bucket, region, key string, expires int64) string {
	host := cosHost(bucket, region)
	now := time.Now().Unix()
	keyTime := fmt.Sprintf("%d;%d", now-60, now+expires)
	sig := cosSignature(secretKey, "get", "/"+key, map[string]string{"host": host}, nil, keyTime)
	return "https://" + host + "/" + encodeCosKey(key) +
		"?q-sign-algorithm=sha1&q-ak=" + secretID +
		"&q-sign-time=" + keyTime + "&q-key-time=" + keyTime +
		"&q-header-list=host&q-url-param-list=&q-signature=" + sig
}

// cosObjectExists 签名 HEAD 请求判断对象是否存在（对应 TS cosObjectExists）。
func cosObjectExists(secretID, secretKey, bucket, region, key string) bool {
	host := cosHost(bucket, region)
	pathname := "/" + key // 签名用原始路径
	now := time.Now().Unix()
	keyTime := fmt.Sprintf("%d;%d", now-60, now+600)
	sig := cosSignature(secretKey, "head", pathname, map[string]string{"host": host}, nil, keyTime)
	auth := "q-sign-algorithm=sha1&q-ak=" + secretID +
		"&q-sign-time=" + keyTime + "&q-key-time=" + keyTime +
		"&q-header-list=host&q-url-param-list=&q-signature=" + sig
	req, err := http.NewRequest(http.MethodHead, "https://"+host+"/"+encodeCosKey(key), nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", auth)
	resp, err := cosHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ============ 签名 URL 缓存 ============

const (
	signedURLValidSeconds     = 3600
	signedURLCacheTTL         = 50 * time.Minute
	signedURLLongValidSeconds = 6 * 24 * 3600
	signedURLLongCacheTTL     = 5 * 24 * time.Hour
	maxSignedURLCacheEntries  = 5000
)

type signedURLEntry struct {
	url      string
	expireAt time.Time
}

var signedURLCache = struct {
	sync.Mutex
	entries map[string]signedURLEntry
}{entries: map[string]signedURLEntry{}}

func pruneSignedURLCache() {
	signedURLCache.Lock()
	defer signedURLCache.Unlock()
	if len(signedURLCache.entries) < maxSignedURLCacheEntries {
		return
	}
	now := time.Now()
	removed := 0
	for k, v := range signedURLCache.entries {
		if now.After(v.expireAt) {
			delete(signedURLCache.entries, k)
			removed++
		}
	}
	if len(signedURLCache.entries) >= maxSignedURLCacheEntries {
		target := maxSignedURLCacheEntries / 2
		toRemove := len(signedURLCache.entries) - target
		for k := range signedURLCache.entries {
			if toRemove <= 0 {
				break
			}
			delete(signedURLCache.entries, k)
			toRemove--
		}
	}
	if removed > 0 {
		log.Printf("[cos-signer] 已清理 %d 条过期签名缓存", removed)
	}
}

func getSignedURLCache(key string) (string, bool) {
	signedURLCache.Lock()
	defer signedURLCache.Unlock()
	e, ok := signedURLCache.entries[key]
	if !ok || time.Now().After(e.expireAt) {
		return "", false
	}
	return e.url, true
}

func setSignedURLCache(key, url string, ttl time.Duration) {
	signedURLCache.Lock()
	signedURLCache.entries[key] = signedURLEntry{url: url, expireAt: time.Now().Add(ttl)}
	signedURLCache.Unlock()
}

// ============ 对外 API ============

// GetSignedURL 为 COS Key 生成签名 URL（带缓存），可附加图片处理参数。
// cosKey 为已 decode 的对象 Key，接受 ASCII 别名路径（自动还原为真实 Key）。
// 返回 "" 表示不可用（对应 TS 的 null）。
func GetSignedURL(ctx context.Context, d *handler.Deps, cosKey, processQuery string, longLived bool) string {
	_ = ctx
	if cosKey == "" {
		return ""
	}
	cfg := GetCosConfig(ctx, d)
	secretID := cosCfgStr(cfg, "secretId", "COS_SECRET_ID", "")
	secretKey := cosCfgStr(cfg, "secretKey", "COS_SECRET_KEY", "")
	bucket := cosCfgStr(cfg, "bucket", "COS_BUCKET", "")
	region := cosCfgStr(cfg, "region", "COS_REGION", "ap-guangzhou")
	if secretID == "" || secretKey == "" || bucket == "" || region == "" {
		return ""
	}

	prefix := ""
	expires := int64(signedURLValidSeconds)
	ttl := signedURLCacheTTL
	if longLived {
		prefix = "L:"
		expires = signedURLLongValidSeconds
		ttl = signedURLLongCacheTTL
	}

	requestCacheKey := prefix + "req:" + bucket + "/" + region + "/" + cosKey + "?" + processQuery
	if hit, ok := getSignedURLCache(requestCacheKey); ok {
		return hit
	}

	// 凭据变化时更新标记（对标 TS cosClient 单例按凭据重建）
	credKey := secretID + ":" + secretKey
	cosCredsKey.Lock()
	if cosCredsKey.key != credKey {
		cosCredsKey.key = credKey
	}
	cosCredsKey.Unlock()

	candidates := ResolveCosKeyCandidates(cosKey)
	resolvedKey := candidates[0]
	for _, candidate := range candidates {
		if cosObjectExists(secretID, secretKey, bucket, region, candidate) {
			resolvedKey = candidate
			break
		}
	}

	cacheKey := prefix + bucket + "/" + region + "/" + resolvedKey + "?" + processQuery
	if hit, ok := getSignedURLCache(cacheKey); ok {
		setSignedURLCache(requestCacheKey, hit, ttl)
		return hit
	}

	finalURL := buildCosSignedURL(secretID, secretKey, bucket, region, resolvedKey, expires)
	if processQuery != "" {
		if strings.Contains(finalURL, "?") {
			finalURL += "&" + processQuery
		} else {
			finalURL += "?" + processQuery
		}
	}
	setSignedURLCache(cacheKey, finalURL, ttl)
	setSignedURLCache(requestCacheKey, finalURL, ttl)
	pruneSignedURLCache()
	return finalURL
}

// GetSignedURLLong 视频/分享外链等长效签名（6 天有效期）。
func GetSignedURLLong(ctx context.Context, d *handler.Deps, cosKey, processQuery string) string {
	return GetSignedURL(ctx, d, cosKey, processQuery, true)
}

// CosURLToSigned 把任意 COS 直链转换为签名 URL（保留处理参数）；非 COS URL 原样返回。
func CosURLToSigned(ctx context.Context, d *handler.Deps, rawURL string, longLived bool) string {
	if rawURL == "" || !strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if !(strings.Contains(rawURL, ".cos.") || strings.Contains(rawURL, ".myqcloud.com")) {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	cosKey := strings.TrimPrefix(u.Path, "/")
	if decoded, err := url.PathUnescape(cosKey); err == nil {
		cosKey = decoded
	}
	processQuery := u.RawQuery
	signed := GetSignedURL(ctx, d, cosKey, processQuery, longLived)
	if signed == "" {
		return rawURL
	}
	return signed
}

// CosURLToSignedLong 长效签名版本（视频/分享外链）。
func CosURLToSignedLong(ctx context.Context, d *handler.Deps, rawURL string) string {
	return CosURLToSigned(ctx, d, rawURL, true)
}

// CosURLsToSigned 批量转换（内部命中缓存时几乎瞬时）。
func CosURLsToSigned(ctx context.Context, d *handler.Deps, urls []string) []string {
	out := make([]string, len(urls))
	for i, u := range urls {
		out[i] = CosURLToSigned(ctx, d, u, false)
	}
	return out
}

// ============ 纯函数 helpers ============

// IsCosURL 检查是否是 COS URL（对应 TS isCosUrl，含 imim.chat 自定义域名）。
func IsCosURL(rawURL string) bool {
	if rawURL == "" || !strings.HasPrefix(rawURL, "https://") {
		return false
	}
	return strings.Contains(rawURL, ".cos.") || strings.Contains(rawURL, ".myqcloud.com") || strings.Contains(rawURL, "imim.chat")
}

var zhToASCIISeg = map[string]string{
	"头像":  "avatars",
	"群头像": "group-avatars",
	"朋友圈": "moments",
	"照片":  "photos",
	"视频":  "videos",
}

var asciiToZhSeg = func() map[string]string {
	m := make(map[string]string, len(zhToASCIISeg))
	for zh, en := range zhToASCIISeg {
		m[en] = zh
	}
	return m
}()

// CosKeyToAlias cosKey 中文段 → ASCII 别名（仅用于生成对外可见的 URL）。
func CosKeyToAlias(cosKey string) string {
	if cosKey == "" {
		return cosKey
	}
	segs := strings.Split(cosKey, "/")
	for i, s := range segs {
		if a, ok := zhToASCIISeg[s]; ok {
			segs[i] = a
		}
	}
	return strings.Join(segs, "/")
}

// AliasToCosKey ASCII 别名 → 真实 COS Key（用于上行签名/拉取）。
func AliasToCosKey(alias string) string {
	if alias == "" {
		return alias
	}
	segs := strings.Split(alias, "/")
	for i, s := range segs {
		if z, ok := asciiToZhSeg[s]; ok {
			segs[i] = z
		}
	}
	return strings.Join(segs, "/")
}

// ResolveCosKeyCandidates 新上传用 ASCII 路径，历史对象在中文路径；两者都尝试。
func ResolveCosKeyCandidates(cosKey string) []string {
	if cosKey == "" {
		return []string{}
	}
	out := []string{}
	for _, key := range []string{cosKey, AliasToCosKey(cosKey), CosKeyToAlias(cosKey)} {
		if key == "" {
			continue
		}
		dup := false
		for _, e := range out {
			if e == key {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, key)
		}
	}
	return out
}

// CosURLToProxy 把 COS 直链转换为站内代理 URL（无过期、可被 Nginx/CDN 缓存）。
func CosURLToProxy(rawURL string) string {
	if !IsCosURL(rawURL) {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	cosKey := strings.TrimPrefix(u.Path, "/")
	if decoded, err := url.PathUnescape(cosKey); err == nil {
		cosKey = decoded
	}
	aliasKey := CosKeyToAlias(cosKey)
	segs := strings.Split(aliasKey, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	query := ""
	if u.RawQuery != "" {
		query = "?" + u.RawQuery
	}
	return "/api/cos/proxy/" + strings.Join(segs, "/") + query
}

// AvatarToProxy 将头像 COS 直链转换为代理 URL（附带 200x200 webp 缩略图参数）。
func AvatarToProxy(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	if !IsCosURL(rawURL) {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	cosKey := strings.TrimPrefix(u.Path, "/")
	if decoded, err := url.PathUnescape(cosKey); err == nil {
		cosKey = decoded
	}
	aliasKey := CosKeyToAlias(cosKey)
	segs := strings.Split(aliasKey, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/api/cos/proxy/" + strings.Join(segs, "/") + "?imageMogr2/thumbnail/200x200/format/webp/quality/80"
}

// ParseCosURL 从 COS 直链提取已 decode 的 cosKey 与处理参数。
func ParseCosURL(rawURL string) (cosKey, processQuery string, ok bool) {
	if !IsCosURL(rawURL) {
		return "", "", false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", false
	}
	key := strings.TrimPrefix(u.Path, "/")
	if decoded, err := url.PathUnescape(key); err == nil {
		key = decoded
	}
	return key, u.RawQuery, true
}
