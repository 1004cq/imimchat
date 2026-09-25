package media

// publicurl.go 移植自 server/public-url.ts。

import (
	"os"
	"regexp"
	"strings"
)

// defaultPublicBaseURL 默认公网域名（对应 TS DEFAULT_PUBLIC_BASE_URL）。
const defaultPublicBaseURL = "https://cq.je"

var absoluteURLRe = regexp.MustCompile(`(?i)^https?://`)

// GetPublicBaseURL 规范公网 origin（对应 TS getPublicBaseUrl）。
func GetPublicBaseURL() string {
	raw := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL"))
	if raw != "" {
		return strings.TrimSuffix(raw, "/")
	}
	return defaultPublicBaseURL
}

// PublicURL 拼接公网完整 URL（对应 TS publicUrl）。
func PublicURL(path string) string {
	base := GetPublicBaseURL()
	if path == "" {
		return base
	}
	if absoluteURLRe.MatchString(path) {
		return path
	}
	if strings.HasPrefix(path, "/") {
		return base + path
	}
	return base + "/" + path
}
