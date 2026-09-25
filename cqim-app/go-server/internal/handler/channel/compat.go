package channel

// 与 Node 端工具函数的兼容实现：
//   - generateChannelDialogID：utils/peerId.ts 的 generateDialogId('channel')
//   - avatarToProxy：cos-signer.ts 的 avatarToProxy
//   - publicURL：public-url.ts 的 publicUrl

import (
	"crypto/rand"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============ TG 风格 Dialog ID（utils/peerId.ts） ============

var (
	dialogMu      sync.Mutex
	dialogLastTs  int64
	dialogSeq8    int64
	dialogEpochMs = int64(1704067200000) // 自定义纪元：2024-01-01 00:00:00 UTC
)

// generateChannelDialogID 生成频道 Dialog ID：-1000000000000 - internalId，
// internalId = timestamp(41) | random(4) | sequence(8)，与 Node 端逐位一致。
func generateChannelDialogID() string {
	dialogMu.Lock()
	defer dialogMu.Unlock()
	ts := time.Now().UnixMilli() - dialogEpochMs
	if ts == dialogLastTs {
		dialogSeq8 = (dialogSeq8 + 1) & 0xFF
		if dialogSeq8 == 0 {
			for time.Now().UnixMilli()-dialogEpochMs <= dialogLastTs {
				// 自旋等待下一毫秒（与 Node 端一致，实际极少发生）
			}
			ts = time.Now().UnixMilli() - dialogEpochMs
		}
	} else {
		dialogSeq8 = 0
	}
	dialogLastTs = ts
	var rb [1]byte
	_, _ = rand.Read(rb[:])
	random := int64(rb[0] & 0x0F)
	internal := (ts << 12) | (random << 8) | dialogSeq8
	return strconv.FormatInt(-1000000000000-internal, 10)
}

// ============ avatarToProxy（cos-signer.ts） ============

var zhToAsciiSeg = map[string]string{
	"头像":  "avatars",
	"群头像": "group-avatars",
	"朋友圈": "moments",
	"照片":  "photos",
	"视频":  "videos",
}

func isCosURL(raw string) bool {
	if raw == "" || !strings.HasPrefix(raw, "https://") {
		return false
	}
	return strings.Contains(raw, ".cos.") ||
		strings.Contains(raw, ".myqcloud.com") ||
		strings.Contains(raw, "imim.chat")
}

// avatarToProxy 将头像 COS 直链转为代理 URL（200x200 webp），非 COS URL 原样返回。
func avatarToProxy(raw string) string {
	if raw == "" {
		return ""
	}
	if !isCosURL(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	key, err := url.PathUnescape(strings.TrimPrefix(u.Path, "/"))
	if err != nil {
		key = strings.TrimPrefix(u.Path, "/")
	}
	segs := strings.Split(key, "/")
	enc := make([]string, 0, len(segs))
	for _, s := range segs {
		if a, ok := zhToAsciiSeg[s]; ok {
			s = a
		}
		// 与 TS encodeURIComponent 对齐：逐段编码
		enc = append(enc, url.PathEscape(s))
	}
	return "/api/cos/proxy/" + strings.Join(enc, "/") +
		"?imageMogr2/thumbnail/200x200/format/webp/quality/80"
}

// ============ publicUrl（public-url.ts） ============

func publicBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return "https://cq.je"
}

func publicURL(path string) string {
	base := publicBaseURL()
	if path == "" {
		return base
	}
	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}
