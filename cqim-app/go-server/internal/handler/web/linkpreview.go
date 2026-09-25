package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============ 链接预览（对应 index.ts GET /api/link-preview） ============
// 抓取目标页面的 OG/meta 元数据，返回链接预览卡片数据。

const linkPreviewUA = "Mozilla/5.0 (compatible; imim-LinkPreview/1.0; +https://imim.app)"

// GET /api/link-preview?url=<encoded_url>
func (h *Handler) linkPreview(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		util.WriteError(w, 400, "缺少 url 参数")
		return
	}
	target, err := url.Parse(rawURL)
	if err != nil || target.Host == "" {
		util.WriteError(w, 400, "无效的 URL 格式")
		return
	}
	scheme := strings.ToLower(target.Scheme)
	if scheme != "http" && scheme != "https" {
		util.WriteError(w, 400, "仅支持 http/https 链接")
		return
	}
	targetURL := target.String()
	favicon := fmt.Sprintf("https://www.google.com/s2/favicons?domain=%s&sz=64", target.Hostname())
	basicInfo := func() map[string]any {
		return map[string]any{
			"url": targetURL, "title": target.Hostname(), "description": "",
			"image": "", "siteName": target.Hostname(), "favicon": favicon,
		}
	}

	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURL, nil)
	if err != nil {
		util.WriteJSON(w, 200, basicInfo())
		return
	}
	req.Header.Set("User-Agent", linkPreviewUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		// 超时降级为基本信息（对应 TS 的 AbortError → 504；此处统一降级）
		if errors.Is(err, http.ErrHandlerTimeout) {
			util.WriteError(w, 504, "请求超时")
			return
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Timeout() {
			util.WriteError(w, 504, "请求超时")
			return
		}
		util.WriteJSON(w, 200, basicInfo())
		return
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml") {
		// 非 HTML 资源（如图片、PDF），直接返回基本信息
		util.WriteJSON(w, 200, basicInfo())
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		util.WriteJSON(w, 200, basicInfo())
		return
	}
	html := string(body)

	title := firstNonEmpty(getMeta(html, "og:title"), getMeta(html, "twitter:title"), htmlTitle(html), target.Hostname())
	description := firstNonEmpty(getMeta(html, "og:description"), getMeta(html, "twitter:description"), getMeta(html, "description"))
	image := firstNonEmpty(getMeta(html, "og:image"), getMeta(html, "twitter:image"), getMeta(html, "og:image:url"))
	siteName := firstNonEmpty(getMeta(html, "og:site_name"), target.Hostname())

	// 处理相对路径图片 URL
	absoluteImage := image
	if image != "" && !strings.HasPrefix(strings.ToLower(image), "http") {
		if u, err := url.Parse(image); err == nil {
			absoluteImage = target.ResolveReference(u).String()
		} else {
			absoluteImage = ""
		}
	}

	util.WriteJSON(w, 200, map[string]any{
		"url":         targetURL,
		"title":       truncateRunes(decodeEntities(title), 120),
		"description": truncateRunes(decodeEntities(description), 300),
		"image":       absoluteImage,
		"siteName":    truncateRunes(decodeEntities(siteName), 60),
		"favicon":     favicon,
	})
}

// getMeta 按优先级匹配 meta 标签的 content（对应 TS 的 4 种正则）。
func getMeta(html, property string) string {
	prop := regexp.QuoteMeta(property)
	patterns := []string{
		`<meta[^>]+property=["']` + prop + `["'][^>]+content=["']([^"']*)["']`,
		`<meta[^>]+content=["']([^"']*)["'][^>]+property=["']` + prop + `["']`,
		`<meta[^>]+name=["']` + prop + `["'][^>]+content=["']([^"']*)["']`,
		`<meta[^>]+content=["']([^"']*)["'][^>]+name=["']` + prop + `["']`,
	}
	for _, p := range patterns {
		if m := regexp.MustCompile(`(?i)` + p).FindStringSubmatch(html); m != nil {
			if v := strings.TrimSpace(m[1]); v != "" {
				return v
			}
		}
	}
	return ""
}

var titleRe = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)

func htmlTitle(html string) string {
	if m := titleRe.FindStringSubmatch(html); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// decodeEntities HTML 实体解码（简单处理常见实体，对应 TS）。
var entityReplacer = strings.NewReplacer(
	"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&nbsp;", " ",
)

func decodeEntities(s string) string { return entityReplacer.Replace(s) }

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
