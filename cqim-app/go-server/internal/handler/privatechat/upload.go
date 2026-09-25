// POST /api/chat/send 统一发送入口（JSON 与 multipart），以及媒体落盘。
// 移植自 private-chat.ts 的 /send 路由与 media-storage.ts 的 saveMedia。
//
// 与 TS 版的差异（任务要求补齐）：
//   - busboy 缺少 error 处理 → Go 版对 ParseMultipartForm / 文件读取 / 落盘 / 入库
//     的每一步错误都有明确处理；
//   - saveMedia 原写 MinIO（需 minio-go 新依赖），Go 版按同一目录约定
//     media/{kind}/{YYYY}/{id} 落到本地 MEDIA_DIR，MediaFile 行的 publicPath
//     仍为 /api/media/{id}，diskPath 仍为对象 key，保持契约一致。
package privatechat

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// maxUploadBytes 对应 TS busboy limits.fileSize = 200MB。
const maxUploadBytes = 200 << 20

type mediaKind string

const (
	mediaImage   mediaKind = "image"
	mediaVoice   mediaKind = "voice"
	mediaVideo   mediaKind = "video"
	mediaSticker mediaKind = "sticker"
	mediaFile    mediaKind = "file"
)

// mediaMaxBytes 各类型大小上限（与 media-storage.ts MAX_BYTES 一致）。
var mediaMaxBytes = map[mediaKind]int64{
	mediaImage:   15 << 20,
	mediaVoice:   20 << 20,
	mediaVideo:   100 << 20, // 可被 MEDIA_MAX_VIDEO_BYTES 覆盖
	mediaSticker: 8 << 20,
	mediaFile:    30 << 20,
}

func mediaKindFor(requestedType string) mediaKind {
	switch requestedType {
	case "image":
		return mediaImage
	case "video":
		return mediaVideo
	case "voice":
		return mediaVoice
	default:
		return mediaFile
	}
}

func allowedUploadMime(mimeType string) bool {
	for _, p := range []string{"image/", "video/", "audio/"} {
		if strings.HasPrefix(mimeType, p) {
			return true
		}
	}
	switch mimeType {
	case "application/pdf", "text/plain", "application/zip", "application/octet-stream":
		return true
	}
	return false
}

// POST /api/chat/send — iOS/移动端统一发送入口。
// JSON: { chatId, msgType, content, replyTo }
// multipart: chatId, type, msgType(必须 encrypted), content?, file?, duration?, replyTo?/replyToId?, waveform?
func (h *Handler) sendUnified(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		var body struct {
			ChatID  string  `json:"chatId"`
			MsgType string  `json:"msgType"`
			Content string  `json:"content"`
			ReplyTo *string `json:"replyTo"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		if body.ChatID == "" {
			util.WriteError(w, http.StatusBadRequest, "缺少 chatId")
			return
		}
		if body.MsgType != "encrypted" {
			util.WriteError(w, http.StatusBadRequest, "私聊强制要求端到端加密，请发送加密消息 (msgType=encrypted)")
			return
		}
		if strings.TrimSpace(body.Content) == "" {
			util.WriteError(w, http.StatusBadRequest, "加密信封内容不能为空")
			return
		}
		h.createAndNotifyMessage(w, r, body.ChatID, "encrypted", body.Content, body.ReplyTo, nil)
		return
	}
	h.sendMultipart(w, r)
}

func (h *Handler) sendMultipart(w http.ResponseWriter, r *http.Request) {
	me := middleware.UserFrom(r).Id

	// 先限流再解析，避免超大 body 耗尽内存/磁盘。
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			util.WriteError(w, http.StatusBadRequest, "文件过大，最大 200MB")
			return
		}
		log.Printf("[PrivateChat] 解析上传失败: %v", err)
		util.WriteError(w, http.StatusInternalServerError, "发送消息失败")
		return
	}

	chatID := r.FormValue("chatId")
	if chatID == "" {
		util.WriteError(w, http.StatusBadRequest, "缺少 chatId")
		return
	}
	if r.FormValue("msgType") != "encrypted" {
		util.WriteError(w, http.StatusBadRequest, "私聊强制要求端到端加密，请发送加密消息 (msgType=encrypted)")
		return
	}

	requestedType := r.FormValue("type")
	if requestedType == "" {
		requestedType = "text"
	}
	replyTo := r.FormValue("replyTo")
	if replyTo == "" {
		replyTo = r.FormValue("replyToId")
	}
	var replyToID *string
	if replyTo != "" {
		replyToID = &replyTo
	}

	content := r.FormValue("content")
	if content == "" {
		content = messagePreview(requestedType, "")
	}
	extra := map[string]any{}

	var fh *multipart.FileHeader
	if r.MultipartForm != nil {
		if fhs := r.MultipartForm.File["file"]; len(fhs) > 0 {
			fh = fhs[0]
		}
	}

	if fh != nil {
		originalName := fh.Filename
		if originalName == "" {
			originalName = "upload"
		}
		mimeType := fh.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		if !allowedUploadMime(mimeType) {
			util.WriteError(w, http.StatusBadRequest, "不支持的文件类型: "+mimeType)
			return
		}

		buf, err := readUploadFile(fh)
		if err != nil {
			if err == errUploadTooLarge {
				util.WriteError(w, http.StatusBadRequest, "文件过大，最大 200MB")
				return
			}
			log.Printf("[PrivateChat] 读取上传文件失败: %v", err)
			util.WriteError(w, http.StatusInternalServerError, "发送消息失败")
			return
		}

		media, err := h.saveMedia(r, me, mediaKindFor(requestedType), buf, mimeType, originalName, r.FormValue("duration"))
		if err != nil {
			if err == errFileTooLarge {
				// 与 TS 版保持一致：类型上限超限走 500 '发送消息失败'
				//（saveMedia 抛错被外层 catch 捕获）。
				log.Printf("[PrivateChat] 媒体超限 kind=%s size=%d", mediaKindFor(requestedType), len(buf))
			} else {
				log.Printf("[PrivateChat] 保存媒体失败: %v", err)
			}
			util.WriteError(w, http.StatusInternalServerError, "发送消息失败")
			return
		}

		mediaURL := media.PublicPath
		extra["mediaUrl"] = mediaURL
		extra["fileName"] = originalName
		extra["fileSize"] = len(buf)
		extra["mimeType"] = mimeType

		switch requestedType {
		case "voice":
			extra["voiceUrl"] = mediaURL
			extra["duration"] = atoiOrZero(r.FormValue("duration"))
			if wf := r.FormValue("waveform"); wf != "" {
				var wv any
				if err := json.Unmarshal([]byte(wf), &wv); err == nil {
					extra["waveform"] = wv
				}
			}
			extra["audioMimeType"] = mimeType
			content = "[语音消息]"
		case "image":
			content = "[图片]"
		case "video":
			content = "[视频]"
		case "file":
			content = originalName
			if content == "" {
				content = "[文件]"
			}
		}
	} else if requestedType != "text" {
		util.WriteError(w, http.StatusBadRequest, "缺少文件")
		return
	}

	var extraArg map[string]any
	if len(extra) > 0 {
		extraArg = extra
	}
	h.createAndNotifyMessage(w, r, chatID, "encrypted", content, replyToID, extraArg)
}

var errUploadTooLarge = fmt.Errorf("upload too large")

// readUploadFile 读取上传文件，带 error 处理与大小上限。
func readUploadFile(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			log.Printf("[PrivateChat] 关闭上传文件失败: %v", cerr)
		}
	}()
	buf, err := io.ReadAll(io.LimitReader(f, maxUploadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxUploadBytes {
		return nil, errUploadTooLarge
	}
	return buf, nil
}

var errFileTooLarge = fmt.Errorf("file_too_large")

type savedMedia struct {
	ID         string
	URL        string
	PublicPath string
}

// newMediaID 生成 24 位 hex ID（对应 TS saveMedia 的 randomBytes(12).toString('hex')）。
func newMediaID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 极端降级：时间戳 hex
		return fmt.Sprintf("%024x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// mediaRoot 本地媒体根目录（默认 ./data/media，可用 MEDIA_DIR 覆盖）。
func mediaRoot() string {
	if v := os.Getenv("MEDIA_DIR"); v != "" {
		return v
	}
	return "./data/media"
}

// saveMedia 落盘并入库 MediaFile。
// 对象 key 约定与 media-storage.ts 一致：media/{kind}/{UTC年份}/{id}；
// publicPath 仍为 /api/media/{id}。
func (h *Handler) saveMedia(r *http.Request, ownerID string, kind mediaKind, buf []byte, mime, filename, duration string) (*savedMedia, error) {
	ctx := r.Context()

	max := mediaMaxBytes[kind]
	if kind == mediaVideo {
		if v := os.Getenv("MEDIA_MAX_VIDEO_BYTES"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
				max = n
			}
		}
	}
	if int64(len(buf)) > max {
		return nil, errFileTooLarge
	}

	id := newMediaID()
	key := fmt.Sprintf("media/%s/%d/%s", kind, time.Now().UTC().Year(), id)
	fullPath := filepath.Join(mediaRoot(), filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, buf, 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	publicPath := "/api/media/" + id
	sum := sha256.Sum256(buf)
	sha := hex.EncodeToString(sum[:])
	size := int32(len(buf))
	var durationMs *int32
	if duration != "" {
		if n, err := strconv.Atoi(duration); err == nil {
			v := int32(n)
			durationMs = &v
		}
	}

	if _, err := h.d.DB.Exec(ctx,
		`INSERT INTO "MediaFile"
		 ("id","userId","type","kind","url","publicPath","diskPath","filename","mime","size","durationMs","sha256","createdAt")
		 VALUES ($1,$2,$3,$3,$4,$4,$5,$6,$7,$8,$9,$10,NOW())`,
		id, ownerID, string(kind), publicPath, key, filename, mime, size, durationMs, sha); err != nil {
		_ = os.Remove(fullPath) // 入库失败回滚已落盘文件（对应 TS 版 removeObject）
		return nil, fmt.Errorf("insert media: %w", err)
	}

	return &savedMedia{ID: id, URL: publicPath, PublicPath: publicPath}, nil
}

func atoiOrZero(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
