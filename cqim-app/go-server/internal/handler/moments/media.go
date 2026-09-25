package moments

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
)

// ============ COS 缩略图 URL（纯字符串逻辑，照抄 moments.ts） ============

var (
	videoExtRe    = regexp.MustCompile(`(?i)\.(mp4|mov|avi|webm|mkv|m4v|3gp)$`)
	gifRe         = regexp.MustCompile(`(?i)\.gif(\?|$)`)
	videoPosterRe = regexp.MustCompile(`(?i)\.(mp4|mov|m4v|webm|mkv)$`)
)

// isCosImageURL 判断是否为可做图片处理的 COS 图片 URL（排除视频）。
func isCosImageURL(u string) bool {
	if u == "" || !strings.HasPrefix(u, "https://") {
		return false
	}
	if !strings.Contains(u, ".cos.") && !strings.Contains(u, ".myqcloud.com") {
		return false
	}
	if videoExtRe.MatchString(u) {
		return false
	}
	return true
}

// getCosThumbURL 为 COS 图片追加数据万象 imageMogr2 缩略图参数。
func getCosThumbURL(u, size string) string {
	if !isCosImageURL(u) {
		return u
	}
	if strings.Contains(u, "imageMogr2") || strings.Contains(u, "imageView2") {
		return u
	}
	if gifRe.MatchString(u) && (size == "small" || size == "tiny") {
		return u + "?imageMogr2/cgif/15/thumbnail/300x300/format/webp/quality/70"
	}
	params := map[string]string{
		"tiny":   "?imageMogr2/thumbnail/200x200/format/webp/quality/60",
		"small":  "?imageMogr2/thumbnail/300x300/format/webp/quality/75",
		"medium": "?imageMogr2/thumbnail/800x800/format/webp/quality/85",
		"large":  "?imageMogr2/thumbnail/1200x1200/format/webp/quality/85",
	}
	p, ok := params[size]
	if !ok {
		p = params["medium"]
	}
	return u + p
}

// getCosVideoPoster 为 COS 视频生成数据万象截图封面。
func getCosVideoPoster(u string) *string {
	if u == "" || !strings.HasPrefix(u, "https://") {
		return nil
	}
	if !strings.Contains(u, ".cos.") && !strings.Contains(u, ".myqcloud.com") {
		return nil
	}
	if !videoPosterRe.MatchString(u) {
		return nil
	}
	s := u + "?ci-process=snapshot&time=0.5&format=jpg&width=480"
	return &s
}

// ============ COS Key 中文路径段 ↔ ASCII 别名（照抄 cos-signer.ts） ============

var zhToASCIISeg = map[string]string{
	"头像":  "avatars",
	"群头像": "group-avatars",
	"朋友圈": "moments",
	"照片":  "photos",
	"视频":  "videos",
}

var asciiToZHSeg = func() map[string]string {
	m := make(map[string]string, len(zhToASCIISeg))
	for zh, en := range zhToASCIISeg {
		m[en] = zh
	}
	return m
}()

func mapKeySegs(key string, m map[string]string) string {
	if key == "" {
		return key
	}
	segs := strings.Split(key, "/")
	for i, s := range segs {
		if v, ok := m[s]; ok {
			segs[i] = v
		}
	}
	return strings.Join(segs, "/")
}

// cosKeyToAlias 中文段 → ASCII 别名（对外 URL 用）。
func cosKeyToAlias(key string) string { return mapKeySegs(key, zhToASCIISeg) }

// aliasToCosKey ASCII 别名 → 真实 COS Key。
func aliasToCosKey(alias string) string { return mapKeySegs(alias, asciiToZHSeg) }

// resolveCosKeyCandidates 新旧路径候选（中文原路径 / 别名还原 / 别名化）。
func resolveCosKeyCandidates(key string) []string {
	if key == "" {
		return nil
	}
	out := []string{}
	for _, k := range []string{key, aliasToCosKey(key), cosKeyToAlias(key)} {
		if k == "" {
			continue
		}
		dup := false
		for _, e := range out {
			if e == k {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, k)
		}
	}
	return out
}

// ============ COS URL 判定 / 代理 URL（照抄 cos-signer.ts） ============

// isCosURL 检查是否是 COS URL（含自定义域名 imim.chat）。
func isCosURL(u string) bool {
	if u == "" || !strings.HasPrefix(u, "https://") {
		return false
	}
	return strings.Contains(u, ".cos.") || strings.Contains(u, ".myqcloud.com") || strings.Contains(u, "imim.chat")
}

func decodeCosKey(u *url.URL) string {
	trimmed := strings.TrimPrefix(u.Path, "/")
	if key, err := url.PathUnescape(trimmed); err == nil {
		return key
	}
	return trimmed
}

func aliasSafePath(cosKey string) string {
	aliasKey := cosKeyToAlias(cosKey)
	segs := strings.Split(aliasKey, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// cosUrlToProxy COS 直链 → 站内代理 URL（/api/cos/proxy/…，绕过防盗链）。
func cosUrlToProxy(rawURL string) string {
	if !isCosURL(rawURL) {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := ""
	if u.RawQuery != "" {
		query = "?" + u.RawQuery
	}
	return "/api/cos/proxy/" + aliasSafePath(decodeCosKey(u)) + query
}

// avatarToProxy 头像 COS 直链 → 代理 URL（统一 200x200 webp）。
func avatarToProxy(rawURL *string) string {
	if rawURL == nil || *rawURL == "" {
		return ""
	}
	u := *rawURL
	if !isCosURL(u) {
		return u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	return "/api/cos/proxy/" + aliasSafePath(decodeCosKey(parsed)) + "?imageMogr2/thumbnail/200x200/format/webp/quality/80"
}

// ============ COS 预签名（纯 Go 实现，对应 cos-signer.ts getSignedUrl） ============

type cosCreds struct {
	SecretID  string
	SecretKey string
	Bucket    string
	Region    string
}

var cosConfigCache = struct {
	sync.Mutex
	v        *cosCreds
	expireAt time.Time
}{}

type signedEntry struct {
	url      string
	expireAt time.Time
}

var signedURLCache = struct {
	sync.Mutex
	m map[string]signedEntry
}{m: make(map[string]signedEntry)}

const maxSignedCacheEntries = 5000

func signedCacheGet(key string) (signedEntry, bool) {
	signedURLCache.Lock()
	defer signedURLCache.Unlock()
	e, ok := signedURLCache.m[key]
	if !ok || time.Now().After(e.expireAt) {
		if ok {
			delete(signedURLCache.m, key)
		}
		return signedEntry{}, false
	}
	return e, true
}

func signedCacheSet(key string, e signedEntry) {
	signedURLCache.Lock()
	defer signedURLCache.Unlock()
	if len(signedURLCache.m) >= maxSignedCacheEntries {
		now := time.Now()
		for k, v := range signedURLCache.m {
			if now.After(v.expireAt) {
				delete(signedURLCache.m, k)
			}
		}
	}
	signedURLCache.m[key] = e
}

// getCosCreds 读取 COS 配置：SystemConfig 表 'cos'（5 分钟缓存），缺失字段回退环境变量配置。
func (h *Handler) getCosCreds(ctx context.Context) *cosCreds {
	cosConfigCache.Lock()
	v, exp := cosConfigCache.v, cosConfigCache.expireAt
	cosConfigCache.Unlock()
	if v != nil && time.Now().Before(exp) {
		return v
	}
	if v == nil && !exp.IsZero() && time.Now().Before(exp) {
		return nil
	}

	var secretID, secretKey, bucket, region string
	if row, err := db.QueryRowToStruct[db.SystemConfig](ctx, h.deps.DB,
		`SELECT "key","value","updatedAt" FROM "SystemConfig" WHERE "key"='cos'`); err == nil && row.Value != "" {
		var m map[string]string
		if json.Unmarshal([]byte(row.Value), &m) == nil {
			secretID, secretKey, bucket, region = m["secretId"], m["secretKey"], m["bucket"], m["region"]
		}
	}
	if secretID == "" {
		secretID = h.deps.Cfg.CosSecretID
	}
	if secretKey == "" {
		secretKey = h.deps.Cfg.CosSecretKey
	}
	if bucket == "" {
		bucket = h.deps.Cfg.CosBucket
	}
	if region == "" {
		region = h.deps.Cfg.CosRegion
	}
	if region == "" {
		region = "ap-guangzhou"
	}
	var out *cosCreds
	if secretID != "" && secretKey != "" && bucket != "" {
		out = &cosCreds{SecretID: secretID, SecretKey: secretKey, Bucket: bucket, Region: region}
	}
	cosConfigCache.Lock()
	cosConfigCache.v, cosConfigCache.expireAt = out, time.Now().Add(5*time.Minute)
	cosConfigCache.Unlock()
	return out
}

func hmacSHA1Hex(key, data string) string {
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func sha1Hex(data string) string {
	sum := sha1.Sum([]byte(data))
	return hex.EncodeToString(sum[:])
}

// encodeCosPath 按段 percent-encode（对应 SDK 的 camSafeUrlEncode）。
func encodeCosPath(key string) string {
	segs := strings.Split(key, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// cosPresignURL 生成腾讯云 COS 预签名 URL（HMAC-SHA1，算法与 cos-nodejs-sdk-v5 一致）。
// processQuery 为图片处理参数（imageMogr2 / ci-process），不参与签名、直接追加。
func cosPresignURL(c *cosCreds, key string, expires int64, processQuery string) string {
	now := time.Now().Unix()
	signTime := fmt.Sprintf("%d;%d", now, now+expires)
	signKey := hmacSHA1Hex(c.SecretKey, signTime)
	encodedPath := "/" + encodeCosPath(key)
	httpString := "get\n" + encodedPath + "\n\n\n"
	stringToSign := "sha1\n" + signTime + "\n" + sha1Hex(httpString) + "\n"
	signature := hmacSHA1Hex(signKey, stringToSign)
	q := "q-sign-algorithm=sha1" +
		"&q-ak=" + url.QueryEscape(c.SecretID) +
		"&q-sign-time=" + url.QueryEscape(signTime) +
		"&q-key-time=" + url.QueryEscape(signTime) +
		"&q-header-list=&q-url-param-list=" +
		"&q-signature=" + signature
	if processQuery != "" {
		q += "&" + processQuery
	}
	return "https://" + c.Bucket + ".cos." + c.Region + ".myqcloud.com" + encodedPath + "?" + q
}

// cosObjectExists 用预签名 URL 做带 Range 的 GET，判断对象是否存在（对应 headObject 候选解析）。
func cosObjectExists(ctx context.Context, c *cosCreds, key string) bool {
	presigned := cosPresignURL(c, key, 300, "")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, presigned, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Range", "bytes=0-0")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}

// signCosURL 任意 COS 直链 → 预签名 URL（保留处理参数）；非 COS / 无配置时原样返回。
// longLived=true 时签发 6 天（视频/分享外链场景）。
func (h *Handler) signCosURL(ctx context.Context, rawURL string, longLived bool) string {
	if !isCosURL(rawURL) {
		return rawURL
	}
	creds := h.getCosCreds(ctx)
	if creds == nil {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	cosKey := decodeCosKey(u)
	processQuery := u.RawQuery

	var expires int64 = 3600
	var ttl = 50 * time.Minute
	prefix := ""
	if longLived {
		expires = 6 * 24 * 3600
		ttl = 5 * 24 * time.Hour
		prefix = "L:"
	}

	reqKey := prefix + "req:" + creds.Bucket + "/" + creds.Region + "/" + cosKey + "?" + processQuery
	if e, ok := signedCacheGet(reqKey); ok {
		return e.url
	}
	resolved := cosKey
	for _, cand := range resolveCosKeyCandidates(cosKey) {
		if cosObjectExists(ctx, creds, cand) {
			resolved = cand
			break
		}
	}
	cacheKey := prefix + creds.Bucket + "/" + creds.Region + "/" + resolved + "?" + processQuery
	if e, ok := signedCacheGet(cacheKey); ok {
		signedCacheSet(reqKey, e)
		return e.url
	}
	signed := cosPresignURL(creds, resolved, expires, processQuery)
	entry := signedEntry{url: signed, expireAt: time.Now().Add(ttl)}
	signedCacheSet(cacheKey, entry)
	signedCacheSet(reqKey, entry)
	return signed
}

// avatarToSigned 头像走签名 URL（统一压缩 200x200 webp）。
func (h *Handler) avatarToSigned(ctx context.Context, avatar *string) string {
	if avatar == nil || *avatar == "" {
		return ""
	}
	u := *avatar
	if !strings.HasPrefix(u, "https://") {
		return u
	}
	if !isCosURL(u) {
		return u
	}
	if !strings.Contains(u, "?") {
		u += "?imageMogr2/thumbnail/200x200/format/webp/quality/80"
	}
	return h.signCosURL(ctx, u, false)
}

// signMediaThumbs 返回签名后的原图 URL 与各档缩略图/封面（对应 addThumbUrlsSigned 的单条逻辑）。
func (h *Handler) signMediaThumbs(ctx context.Context, typ, rawURL string) (signedURL string, thumb, medium, low, poster *string) {
	signedURL = rawURL
	if typ == "image" && rawURL != "" {
		u := h.signCosURL(ctx, rawURL, false)
		t := h.signCosURL(ctx, getCosThumbURL(rawURL, "small"), false)
		m := h.signCosURL(ctx, getCosThumbURL(rawURL, "medium"), false)
		l := h.signCosURL(ctx, getCosThumbURL(rawURL, "tiny"), false)
		return u, &t, &m, &l, nil
	}
	if typ == "video" && rawURL != "" {
		// 视频走长效签名直链（6 天），避免经服务器代理中转的带宽瓶颈
		u := h.signCosURL(ctx, rawURL, true)
		if p := getCosVideoPoster(rawURL); p != nil {
			ps := h.signCosURL(ctx, *p, true)
			return u, &ps, nil, nil, &ps
		}
		return u, nil, nil, nil, nil
	}
	return signedURL, nil, nil, nil, nil
}

// signShareMedia 分享格式媒体签名（并行，保持顺序）。
func (h *Handler) signShareMedia(ctx context.Context, items []shareMediaIn) []shareMediaJSON {
	out := make([]shareMediaJSON, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(i int, item shareMediaIn) {
			defer wg.Done()
			u, t, m, l, p := h.signMediaThumbs(ctx, item.Type, item.URL)
			out[i] = shareMediaJSON{
				Type: item.Type, URL: u, Width: item.Width, Height: item.Height,
				Duration: item.Duration, Cover: nil,
				ThumbURL: t, MediumURL: m, LowQualityURL: l, PosterURL: p,
			}
		}(i, item)
	}
	wg.Wait()
	return out
}

// signMediaFull 完整媒体行签名（列表/详情/发布响应）。
func (h *Handler) signMediaFull(ctx context.Context, media []db.MomentMedia) []mediaFullJSON {
	out := make([]mediaFullJSON, len(media))
	var wg sync.WaitGroup
	for i, md := range media {
		wg.Add(1)
		go func(i int, md db.MomentMedia) {
			defer wg.Done()
			u, t, m, l, p := h.signMediaThumbs(ctx, md.Type, md.Url)
			out[i] = mediaFullJSON{
				ID: md.Id, MomentID: md.MomentId, Type: md.Type, URL: u,
				Width: md.Width, Height: md.Height, Duration: md.Duration,
				SortOrder: md.SortOrder, CreatedAt: md.CreatedAt,
				ThumbURL: t, MediumURL: m, LowQualityURL: l, PosterURL: p,
			}
		}(i, md)
	}
	wg.Wait()
	return out
}

// signMyMedia 我的动态列表媒体签名（仅 type/url 输入）。
func (h *Handler) signMyMedia(ctx context.Context, items []shareMediaIn) []myMediaJSON {
	out := make([]myMediaJSON, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(i int, item shareMediaIn) {
			defer wg.Done()
			u, t, m, l, p := h.signMediaThumbs(ctx, item.Type, item.URL)
			out[i] = myMediaJSON{
				Type: item.Type, URL: u,
				ThumbURL: t, MediumURL: m, LowQualityURL: l, PosterURL: p,
			}
		}(i, item)
	}
	wg.Wait()
	return out
}
