// Package media 移植自 server/media-router.ts、server/media-storage.ts、
// server/cos-signer.ts、server/cdn-preheat.ts、server/public-url.ts，
// 以及 server/index.ts 中的 /api/voice/upload 与 /api/cos/sts。
//
// 路由一览（API 契约与 Node 版一致）：
//   - POST /api/media/upload       JSON base64 上传（需登录）
//   - POST /api/media/upload-form  multipart 表单上传（需登录）
//   - GET  /api/media/{id}        媒体直读（无鉴权：不透明 id 即能力凭证，与 TS 注释一致）
//   - GET  /api/media/files/      本地静态文件服务（目录见 MEDIA_FILES_DIR）
//   - POST /api/voice/upload      语音 base64 上传（需登录）
//   - GET  /api/cos/sts           已停用：固定 503（需登录）
package media

import (
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// upload 表单/总大小上限：TS busboy limits.fileSize = 200MB（单文件）；
// Go 侧再加一点余量作为请求体总上限，超限映射 413（见 uploadForm）。
const (
	maxUploadFormBody = 208 * 1024 * 1024
	maxUploadFormFile = 200 * 1024 * 1024
	maxVoiceBytes     = 10 * 1024 * 1024
)

// Handler 媒体模块 handler。
type Handler struct {
	deps *handler.Deps
	s3   *s3Client
}

func (h *Handler) db() *db.DB { return h.deps.DB }

// RegisterRoutes 注册媒体模块全部路由。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := &Handler{deps: d, s3: newS3Client()}
	auth := d.Auth.UserAuth

	mux.Handle("POST /api/media/upload", auth(http.HandlerFunc(h.upload)))
	mux.Handle("POST /api/media/upload-form", auth(http.HandlerFunc(h.uploadForm)))
	// 注意：GET /api/media/{id} 无鉴权——不透明 MediaFile id 即浏览器
	// <img>/<audio>/<video> 的能力凭证（照抄 TS 注释）。
	mux.Handle("GET /api/media/{id}", http.HandlerFunc(h.serveMedia))
	mux.Handle("GET /api/media/files/", http.HandlerFunc(h.serveFiles))

	mux.Handle("POST /api/voice/upload", auth(http.HandlerFunc(h.uploadVoice)))
	mux.Handle("GET /api/cos/sts", auth(http.HandlerFunc(h.cosSTS)))
}

// normalizeKind kind 别名归一化：audio → voice（与 TS 一致）。
func normalizeKind(raw string) string {
	if raw == "audio" {
		return "voice"
	}
	return raw
}

// decodeBase64Lenient 宽松解码 base64（对标 Node Buffer.from 的容错行为）。
func decodeBase64Lenient(s string) []byte {
	// TS: b64.includes(',') ? b64.slice(indexOf(',')+1) : b64（无条件去掉 data: 前缀）
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		}
		return r
	}, s)
	if d, err := base64.StdEncoding.DecodeString(s); err == nil {
		return d
	}
	// 兜底：只保留合法字符并补齐 padding
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' {
			b.WriteByte(c)
		}
	}
	s2 := b.String()
	if m := len(s2) % 4; m != 0 {
		s2 += strings.Repeat("=", 4-m)
	}
	if d, err := base64.StdEncoding.DecodeString(s2); err == nil {
		return d
	}
	return []byte{}
}

// ============ POST /api/media/upload（JSON base64 上传） ============

type mediaUploadBody struct {
	Kind          string   `json:"kind"`
	Type          string   `json:"type"`
	MediaType     string   `json:"mediaType"`
	Data          string   `json:"data"`
	DataBase64    string   `json:"dataBase64"`
	File          string   `json:"file"`
	Content       string   `json:"content"`
	Mime          string   `json:"mime"`
	MimeType      string   `json:"mimeType"`
	Filename      string   `json:"filename"`
	Name          string   `json:"name"`
	Width         *float64 `json:"width"`
	Height        *float64 `json:"height"`
	DurationMs    *float64 `json:"durationMs"`
	PosterMediaID string   `json:"posterMediaId"`
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func numOrNil(f *float64) *int {
	if f == nil || *f != *f { // NaN
		return nil
	}
	n := int(*f)
	return &n
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := middleware.UserFrom(r)

	var body mediaUploadBody
	// TS: const body = req.body || {} —— 空请求体视为 {}
	if r.ContentLength != 0 {
		if !util.DecodeJSON(w, r, &body) {
			return
		}
	}

	kind := normalizeKind(firstNonEmpty(body.Kind, body.Type, body.MediaType, "file"))
	if !IsMediaKind(kind) {
		util.WriteJSON(w, 400, map[string]any{
			"error": "invalid_kind",
			"allow": []string{"image", "voice", "video", "sticker", "file"},
		})
		return
	}
	b64 := firstNonEmpty(body.Data, body.DataBase64, body.File, body.Content)
	if len(b64) < 8 {
		util.WriteError(w, 400, "missing_data")
		return
	}
	buffer := decodeBase64Lenient(b64)

	row, err := h.saveMedia(ctx, saveMediaOptions{
		ownerID:       &u.Id,
		kind:          kind,
		buffer:        buffer,
		mime:          firstNonEmpty(body.Mime, body.MimeType, "application/octet-stream"),
		filename:      strOrNil(firstNonEmpty(body.Filename, body.Name)),
		width:         numOrNil(body.Width),
		height:        numOrNil(body.Height),
		durationMs:    numOrNil(body.DurationMs),
		posterMediaID: strOrNil(body.PosterMediaID),
	})
	if err != nil {
		var tooLarge *fileTooLargeError
		if errors.As(err, &tooLarge) {
			// TS: msg 以 file_too_large 开头 → 413，文案原样返回
			util.WriteJSON(w, 413, map[string]any{"error": tooLarge.msg})
			return
		}
		log.Printf("[media] MinIO upload: %v", err)
		util.WriteError(w, 500, "upload_failed")
		return
	}
	kindOut := row.Kind
	if kindOut == "" {
		kindOut = row.Type
	}
	urlOut := row.PublicPath
	if urlOut == nil || *urlOut == "" {
		urlOut = &row.Url
	}
	var size int64
	if row.Size != nil {
		size = int64(*row.Size)
	}
	util.WriteJSON(w, 200, map[string]any{
		"id":      row.Id,
		"kind":    kindOut,
		"url":     util.StrVal(urlOut),
		"size":    size,
		"mime":    util.StrVal(row.Mime),
		"storage": "minio",
	})
}

// ============ POST /api/media/upload-form（multipart 表单上传） ============

func (h *Handler) uploadForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := middleware.UserFrom(r)

	// TS 的 busboy 缺少 error 处理是已知审计项；Go 版用 ParseMultipartForm 并补齐错误处理。
	// 先用 MaxBytesReader 封顶请求体，避免超大 body 耗尽内存/磁盘。
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadFormBody)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			util.WriteJSON(w, 413, map[string]any{"error": "file_too_large"})
			return
		}
		util.WriteJSON(w, 400, map[string]any{"error": "invalid_multipart"})
		return
	}
	defer r.MultipartForm.RemoveAll()

	kind := normalizeKind(firstNonEmpty(r.FormValue("kind"), r.FormValue("mediaType"), "file"))

	// TS busboy 'file' 事件对每个文件 part 都会触发并覆盖 file 变量（最后一个生效）；
	// 这里同样取最后一个文件 part。用 haveFile 标记（空文件也是合法上传，与 TS 一致）。
	var (
		data     []byte
		haveFile bool
		filename = "upload"
		mime     = "application/octet-stream"
	)
	if r.MultipartForm != nil {
		for _, fhs := range r.MultipartForm.File {
			for _, fh := range fhs {
				f, err := fh.Open()
				if err != nil {
					continue
				}
				buf, err := io.ReadAll(io.LimitReader(f, maxUploadFormFile+1))
				f.Close()
				if err != nil {
					continue
				}
				if int64(len(buf)) > maxUploadFormFile {
					// 对应 TS busboy limit 事件 → truncated → 413
					util.WriteJSON(w, 413, map[string]any{"error": "file_too_large"})
					return
				}
				data = buf
				haveFile = true
				if fh.Filename != "" {
					filename = fh.Filename
				}
				if ct := fh.Header.Get("Content-Type"); ct != "" {
					mime = ct
				}
			}
		}
	}
	if !haveFile || !IsMediaKind(kind) {
		util.WriteJSON(w, 400, map[string]any{"error": "invalid_upload"})
		return
	}

	row, err := h.saveMedia(ctx, saveMediaOptions{
		ownerID:  &u.Id,
		kind:     kind,
		buffer:   data,
		mime:     mime,
		filename: &filename,
	})
	if err != nil {
		var tooLarge *fileTooLargeError
		if errors.As(err, &tooLarge) {
			util.WriteJSON(w, 413, map[string]any{"error": tooLarge.msg})
			return
		}
		log.Printf("[media] MinIO form upload: %v", err)
		util.WriteJSON(w, 500, map[string]any{"error": "upload_failed"})
		return
	}
	urlOut := util.StrVal(row.PublicPath)
	if urlOut == "" {
		urlOut = row.Url
	}
	var size int64
	if row.Size != nil {
		size = int64(*row.Size)
	}
	util.WriteJSON(w, 200, map[string]any{
		"ok":       true,
		"id":       row.Id,
		"kind":     row.Kind,
		"url":      urlOut,
		"fileName": util.StrVal(row.Filename),
		"storage":  "minio",
		"size":     size,
		"mime":     util.StrVal(row.Mime),
	})
}

// ============ GET /api/media/{id}（媒体直读，无鉴权） ============

func (h *Handler) serveMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	row, size, stream, err := h.resolveMedia(ctx, id)
	if err != nil {
		// TS catch → 404 {error:'missing_object'}
		util.WriteJSON(w, 404, map[string]any{"error": "missing_object"})
		return
	}
	if row == nil {
		util.WriteJSON(w, 404, map[string]any{"error": "not_found"})
		return
	}
	defer stream.Close()

	mime := util.StrVal(row.Mime)
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	// TS: found.object.on('error', ...) → 流错误时若未发送头则 404；
	// Go 侧头已发送，只能中断流，语义等价。
	_, _ = io.Copy(w, stream)
}

// ============ GET /api/media/files/（本地静态文件服务） ============

// serveFiles 对应 index.ts 的 express.static 挂载（/api/media/files/）。
// 目录由 MEDIA_FILES_DIR 指定，默认 ./media-files；禁用目录列表。
func (h *Handler) serveFiles(w http.ResponseWriter, r *http.Request) {
	dir := getenvStr("MEDIA_FILES_DIR", "./media-files")
	rel := strings.TrimPrefix(r.URL.Path, "/api/media/files/")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || strings.Contains(rel, "..") {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}

// ============ POST /api/voice/upload（语音 base64 上传） ============

type voiceUploadBody struct {
	AudioBase64 string `json:"audioBase64"`
	MimeType    string `json:"mimeType"`
}

func stripDataURLPrefix(s string) string {
	if strings.HasPrefix(s, "data:") {
		if i := strings.IndexByte(s, ','); i >= 0 {
			return s[i+1:]
		}
		return ""
	}
	return s
}

func (h *Handler) uploadVoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := middleware.UserFrom(r)

	var body voiceUploadBody
	if r.ContentLength != 0 {
		if !util.DecodeJSON(w, r, &body) {
			return
		}
	}
	if body.AudioBase64 == "" {
		util.WriteJSON(w, 400, map[string]any{"ok": false, "error": "缺少 audioBase64 参数"})
		return
	}
	// TS: String(audioBase64).replace(/^data:[^,]+,/, '')
	buffer := decodeBase64Lenient(stripDataURLPrefix(body.AudioBase64))
	if len(buffer) == 0 || len(buffer) > maxVoiceBytes {
		util.WriteJSON(w, 400, map[string]any{"ok": false, "error": "语音文件无效或超过 10MB"})
		return
	}
	mime := body.MimeType
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	if mime == "" {
		mime = "audio/ogg"
	}
	filename := "voice_" + strconv.FormatInt(time.Now().UnixMilli(), 10) + ".ogg"
	row, err := h.saveMedia(ctx, saveMediaOptions{
		ownerID:  &u.Id,
		kind:     "voice",
		buffer:   buffer,
		mime:     mime,
		filename: &filename,
	})
	if err != nil {
		log.Printf("[Voice] MinIO upload failed: %v", err)
		util.WriteJSON(w, 500, map[string]any{"ok": false, "error": "上传失败"})
		return
	}
	voiceURL := util.StrVal(row.PublicPath)
	if voiceURL == "" {
		voiceURL = row.Url
	}
	util.WriteJSON(w, 200, map[string]any{
		"ok":       true,
		"voiceUrl": voiceURL,
		"fileName": row.Id,
		"storage":  "minio",
	})
}

// ============ GET /api/cos/sts（已停用） ============

func (h *Handler) cosSTS(w http.ResponseWriter, r *http.Request) {
	// 与 TS 一致：COS 已停用，客户端请使用 MinIO 媒体接口。
	util.WriteJSON(w, 503, map[string]any{"error": "COS 已停用，请使用 MinIO 媒体接口"})
}
