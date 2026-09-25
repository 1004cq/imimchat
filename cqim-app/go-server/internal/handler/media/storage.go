package media

// storage.go 移植自 server/media-storage.ts。
//
// MinIO 对象存储访问层。Node 版使用 minio SDK；Go 版不允许引入新依赖，
// 因此用标准库（crypto/hmac、crypto/sha256、net/http）实现最小化的
// S3 兼容客户端（SigV4 签名），覆盖用到的操作：
// bucketExists / makeBucket / putObject / getObject / statObject / removeObject。
//
// 落盘规则与 TS 版一致：
//   - bucket:  MINIO_BUCKET（默认 cqim-media）
//   - object:  media/{kind}/{UTC 年份}/{id}
//   - id:      12 随机字节 hex（24 字符），与 TS randomBytes(12).toString('hex') 一致
//   - 对外 URL: /api/media/{id}（url 与 publicPath 双字段同值）
//   - 单类大小上限与 TS 的 MAX_BYTES 一致

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
)

// mediaKinds 与 TS 的 MEDIA_KINDS 一致。
var mediaKinds = []string{"image", "voice", "video", "sticker", "file"}

// IsMediaKind 判断是否为合法媒体类型（对应 TS isMediaKind）。
func IsMediaKind(value string) bool {
	for _, k := range mediaKinds {
		if value == k {
			return true
		}
	}
	return false
}

func getenvStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// maxBytesForKind 各类型大小上限（对应 TS MAX_BYTES；video 可用环境变量覆盖）。
func maxBytesForKind(kind string) int64 {
	switch kind {
	case "image":
		return 15 * 1024 * 1024
	case "voice":
		return 20 * 1024 * 1024
	case "video":
		if v := os.Getenv("MEDIA_MAX_VIDEO_BYTES"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
				return n
			}
		}
		return 100 * 1024 * 1024
	case "sticker":
		return 8 * 1024 * 1024
	default: // file
		return 30 * 1024 * 1024
	}
}

// MediaBucket 对应 TS mediaBucket()。
func MediaBucket() string {
	return getenvStr("MINIO_BUCKET", "cqim-media")
}

// newMediaID 生成 24 字符 hex id（对应 TS randomBytes(12).toString('hex')）。
func newMediaID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// objectKey 落盘 key 规则：media/{kind}/{UTC 年份}/{id}（对应 TS objectKey）。
func objectKey(kind, id string) string {
	return "media/" + kind + "/" + strconv.Itoa(time.Now().UTC().Year()) + "/" + id
}

// sha256Hex 计算数据的 sha256 hex（对应 TS createHash('sha256')）。
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ============ 最小化 S3 客户端（SigV4，标准库实现） ============

type s3Client struct {
	scheme     string // http / https
	host       string // 含端口，如 minio:9000
	region     string
	accessKey  string
	secretKey  string
	bucket     string
	httpClient *http.Client
}

func newS3Client() *s3Client {
	endpoint := getenvStr("MINIO_ENDPOINT", "minio")
	port := getenvStr("MINIO_PORT", "9000")
	host := endpoint
	if !strings.Contains(host, ":") && port != "" {
		host = host + ":" + port
	}
	scheme := "http"
	if v := os.Getenv("MINIO_USE_SSL"); v == "true" || v == "1" {
		scheme = "https"
	}
	return &s3Client{
		scheme:     scheme,
		host:       host,
		region:     getenvStr("MINIO_REGION", "us-east-1"),
		accessKey:  getenvStr("MINIO_ROOT_USER", "cqimminio"),
		secretKey:  getenvStr("MINIO_ROOT_PASSWORD", "cqimio-secret-change-me"),
		bucket:     MediaBucket(),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// encodeS3Segment 按 S3 规范编码单个路径段（unreserved 字符不编码）。
func encodeS3Segment(s string) string {
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

// encodeS3Key 编码完整对象 key（段分隔符 / 保留）。
func encodeS3Key(key string) string {
	segs := strings.Split(key, "/")
	for i, s := range segs {
		segs[i] = encodeS3Segment(s)
	}
	return strings.Join(segs, "/")
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// signRequest 对请求做 AWS SigV4 签名（path-style，S3 service）。
func (c *s3Client) signRequest(req *http.Request, payloadHash string) {
	c.signRequestAt(req, payloadHash, time.Now().UTC())
}

// signRequestAt 同 signRequest，时间可注入（便于测试）。
func (c *s3Client) signRequestAt(req *http.Request, payloadHash string, t time.Time) {
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		"", // canonical query string（本客户端不使用查询参数）
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := dateStamp + "/" + c.region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))

	kDate := hmacSHA256([]byte("AWS4"+c.secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, c.region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential="+c.accessKey+"/"+credentialScope+
			", SignedHeaders="+signedHeaders+", Signature="+signature)
}

// doRaw 执行已编码路径的请求。
func (c *s3Client) doRaw(ctx context.Context, method, rawPath string, body io.Reader, contentLength int64, contentType, filename string, payloadHash string) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		rdr = body
	}
	req, err := http.NewRequestWithContext(ctx, method, c.scheme+"://"+c.host+rawPath, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = contentLength
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if filename != "" {
		// 对应 TS putObject 的 X-Amz-Meta-Original-Filename 元数据
		req.Header.Set("x-amz-meta-original-filename", filename)
	}
	c.signRequest(req, payloadHash)
	return c.httpClient.Do(req)
}

// ensureBucket bucket 不存在则创建（对应 TS ensureMediaBucket）。
func (c *s3Client) ensureBucket(ctx context.Context) error {
	emptyHash := sha256Hex(nil)
	resp, err := c.doRaw(ctx, http.MethodHead, "/"+c.bucket, nil, 0, "", "", emptyHash)
	if err != nil {
		return err
	}
	drainAndClose(resp)
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
		return fmt.Errorf("s3 headBucket status %d", resp.StatusCode)
	}
	resp2, err := c.doRaw(ctx, http.MethodPut, "/"+c.bucket, nil, 0, "", "", emptyHash)
	if err != nil {
		return err
	}
	drainAndClose(resp2)
	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusCreated && resp2.StatusCode != http.StatusNoContent {
		return fmt.Errorf("s3 makeBucket status %d", resp2.StatusCode)
	}
	return nil
}

// putObject 上传对象（对应 TS minio.putObject）。
func (c *s3Client) putObject(ctx context.Context, key string, data []byte, contentType, filename string) error {
	rawPath := "/" + c.bucket + "/" + encodeS3Key(key)
	resp, err := c.doRaw(ctx, http.MethodPut, rawPath, bytes.NewReader(data), int64(len(data)), contentType, filename, sha256Hex(data))
	if err != nil {
		return err
	}
	drainAndClose(resp)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("s3 putObject status %d", resp.StatusCode)
	}
	return nil
}

// getObject 下载对象流（对应 TS minio.getObject）。调用方负责关闭。
func (c *s3Client) getObject(ctx context.Context, key string) (io.ReadCloser, error) {
	rawPath := "/" + c.bucket + "/" + encodeS3Key(key)
	resp, err := c.doRaw(ctx, http.MethodGet, rawPath, nil, 0, "", "", sha256Hex(nil))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		drainAndClose(resp)
		return nil, fmt.Errorf("s3 getObject status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// statObject 取对象大小（对应 TS minio.statObject）。
func (c *s3Client) statObject(ctx context.Context, key string) (int64, error) {
	rawPath := "/" + c.bucket + "/" + encodeS3Key(key)
	resp, err := c.doRaw(ctx, http.MethodHead, rawPath, nil, 0, "", "", sha256Hex(nil))
	if err != nil {
		return 0, err
	}
	size := resp.ContentLength
	drainAndClose(resp)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("s3 statObject status %d", resp.StatusCode)
	}
	return size, nil
}

// removeObject 删除对象（对应 TS minio.removeObject；不存在也视为成功）。
func (c *s3Client) removeObject(ctx context.Context, key string) error {
	rawPath := "/" + c.bucket + "/" + encodeS3Key(key)
	resp, err := c.doRaw(ctx, http.MethodDelete, rawPath, nil, 0, "", "", sha256Hex(nil))
	if err != nil {
		return err
	}
	drainAndClose(resp)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound, http.StatusAccepted:
		return nil
	default:
		return fmt.Errorf("s3 removeObject status %d", resp.StatusCode)
	}
}

func drainAndClose(resp *http.Response) {
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// ============ saveMedia / resolveMedia（对应 media-storage.ts） ============

// saveMediaOptions 对应 TS saveMedia 的 opts。
type saveMediaOptions struct {
	ownerID       *string
	kind          string
	buffer        []byte
	mime          string
	filename      *string
	width         *int
	height        *int
	durationMs    *int
	posterMediaID *string
}

// fileTooLargeError 携带 "file_too_large:{kind}:{max}" 文案，便于 handler 映射 413。
type fileTooLargeError struct{ msg string }

func (e *fileTooLargeError) Error() string { return e.msg }

// saveMedia 保存媒体：MinIO 落盘 + MediaFile 入库；入库失败回删对象（与 TS 一致）。
func (h *Handler) saveMedia(ctx context.Context, opts saveMediaOptions) (*db.MediaFile, error) {
	max := maxBytesForKind(opts.kind)
	if int64(len(opts.buffer)) > max {
		return nil, &fileTooLargeError{msg: "file_too_large:" + opts.kind + ":" + strconv.FormatInt(max, 10)}
	}
	if err := h.s3.ensureBucket(ctx); err != nil {
		return nil, err
	}
	id := newMediaID()
	key := objectKey(opts.kind, id)
	mime := opts.mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	sha := sha256Hex(opts.buffer)
	publicURL := "/api/media/" + id
	var filename *string
	if opts.filename != nil && *opts.filename != "" {
		filename = opts.filename
	}
	size := int32(len(opts.buffer))
	var width, height, durationMs *int32
	if opts.width != nil {
		w := int32(*opts.width)
		width = &w
	}
	if opts.height != nil {
		he := int32(*opts.height)
		height = &he
	}
	if opts.durationMs != nil {
		d := int32(*opts.durationMs)
		durationMs = &d
	}
	var metaFilename string
	if filename != nil {
		metaFilename = *filename
	}
	if err := h.s3.putObject(ctx, key, opts.buffer, mime, metaFilename); err != nil {
		return nil, err
	}
	row, err := db.QueryRowToStruct[db.MediaFile](ctx, h.db(),
		`INSERT INTO "MediaFile"("id","userId","type","kind","url","publicPath","diskPath","filename","mime","size","width","height","durationMs","posterMediaId","sha256","createdAt")
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NOW())
		 RETURNING "id","userId","type","kind","url","filename","mime","size","width","height","durationMs","posterMediaId","sha256","diskPath","publicPath","cosKey","createdAt"`,
		id, opts.ownerID, opts.kind, opts.kind, publicURL, publicURL, key, filename, mime, size, width, height, durationMs, opts.posterMediaID, sha)
	if err != nil {
		// 与 TS 一致：入库失败时回删已上传对象
		_ = h.s3.removeObject(ctx, key)
		return nil, err
	}
	return row, nil
}

// resolveMedia 按 id 查 MediaFile 并打开对象流（对应 TS resolveMedia）。
// 返回 (row, size, stream)；row 为 nil 表示无记录（handler 映射 404 not_found）。
func (h *Handler) resolveMedia(ctx context.Context, id string) (*db.MediaFile, int64, io.ReadCloser, error) {
	row, err := db.QueryRowToStruct[db.MediaFile](ctx, h.db(),
		`SELECT "id","userId","type","kind","url","filename","mime","size","width","height","durationMs","posterMediaId","sha256","diskPath","publicPath","cosKey","createdAt"
		 FROM "MediaFile" WHERE "id"=$1`, id)
	if err != nil {
		if db.IsNotFound(err) {
			return nil, 0, nil, nil
		}
		return nil, 0, nil, err
	}
	if row.DiskPath == nil || *row.DiskPath == "" {
		return nil, 0, nil, nil
	}
	size, err := h.s3.statObject(ctx, *row.DiskPath)
	if err != nil {
		return nil, 0, nil, err
	}
	stream, err := h.s3.getObject(ctx, *row.DiskPath)
	if err != nil {
		return nil, 0, nil, err
	}
	return row, size, stream, nil
}
