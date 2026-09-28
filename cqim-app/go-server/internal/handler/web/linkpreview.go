package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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

// linkPreviewIPBlocked 拦截回环/私网/链路本地/组播等不可出公网的目标。
func linkPreviewIPBlocked(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// linkPreviewCheckURL 校验 URL 的 scheme 与目标 IP。
// DNS 解析与连接建立之间存在 TOCTOU（DNS rebinding），因此实际连接时
// linkPreviewDialContext 会再次校验；此处用于前置拦截与重定向检查。
func linkPreviewCheckURL(raw string) error {
	target, err := url.Parse(raw)
	if err != nil || target.Host == "" {
		return errors.New("无效的 URL 格式")
	}
	if target.User != nil {
		return errors.New("URL 不得携带认证信息")
	}
	scheme := strings.ToLower(target.Scheme)
	if scheme != "http" && scheme != "https" {
		return errors.New("仅支持 http/https 链接")
	}
	host := target.Hostname()
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, err := net.DefaultResolver.LookupIP(context.Background(), "ip", host)
		if err != nil || len(addrs) == 0 {
			return errors.New("域名解析失败")
		}
		ips = addrs
	}
	for _, ip := range ips {
		if linkPreviewIPBlocked(ip) {
			return errors.New("不允许访问内网/回环地址")
		}
	}
	return nil
}

// linkPreviewDialContext 在建立连接时校验目标 IP，防 DNS rebinding 绕过前置检查：
// 解析后只连接校验通过的 IP，不再二次解析。
func linkPreviewDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil || len(addrs) == 0 {
			return nil, errors.New("域名解析失败")
		}
		ips = addrs
	}
	for _, ip := range ips {
		if linkPreviewIPBlocked(ip) {
			return nil, errors.New("不允许访问内网/回环地址")
		}
	}
	d := &net.Dialer{Timeout: 5 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

var linkPreviewClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		DialContext:     linkPreviewDialContext,
		MaxIdleConns:    20,
		IdleConnTimeout: 30 * time.Second,
	},
	// 重定向同样要过 SSRF 校验，且限制跳转次数。
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("重定向次数过多")
		}
		if err := linkPreviewCheckURL(req.URL.String()); err != nil {
			return err
		}
		req.Header.Set("User-Agent", linkPreviewUA)
		return nil
	},
}

// GET /api/link-preview?url=<encoded_url>
func (h *Handler) linkPreview(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		util.WriteError(w, 400, "缺少 url 参数")
		return
	}
	if err := linkPreviewCheckURL(rawURL); err != nil {
		util.WriteError(w, 400, err.Error())
		return
	}
	target, _ := url.Parse(rawURL)
	targetURL := target.String()
	favicon := fmt.Sprintf("https://www.google.com/s2/favicons?domain=%s&sz=64", target.Hostname())
	basicInfo := func() map[string]any {
		return map[string]any{
			"url": targetURL, "title": target.Hostname(), "description": "",
			"image": "", "siteName": target.Hostname(), "favicon": favicon,
		}
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURL, nil)
	if err != nil {
		util.WriteJSON(w, 200, basicInfo())
		return
	}
	req.Header.Set("User-Agent", linkPreviewUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := linkPreviewClient.Do(req)
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

// linkMetaProps 链接预览需要提取的 meta 属性；正则在包初始化时预编译，
// 避免每次请求重复编译约 36 个正则。
var linkMetaProps = []string{
	"og:title", "twitter:title",
	"og:description", "twitter:description", "description",
	"og:image", "twitter:image", "og:image:url",
	"og:site_name",
}

var linkMetaRes map[string][]*regexp.Regexp

func init() {
	linkMetaRes = make(map[string][]*regexp.Regexp, len(linkMetaProps))
	for _, property := range linkMetaProps {
		prop := regexp.QuoteMeta(property)
		patterns := []string{
			`<meta[^>]+property=["']` + prop + `["'][^>]+content=["']([^"']*)["']`,
			`<meta[^>]+content=["']([^"']*)["'][^>]+property=["']` + prop + `["']`,
			`<meta[^>]+name=["']` + prop + `["'][^>]+content=["']([^"']*)["']`,
			`<meta[^>]+content=["']([^"']*)["'][^>]+name=["']` + prop + `["']`,
		}
		res := make([]*regexp.Regexp, 0, len(patterns))
		for _, p := range patterns {
			res = append(res, regexp.MustCompile(`(?i)`+p))
		}
		linkMetaRes[property] = res
	}
}

// getMeta 按优先级匹配 meta 标签的 content（对应 TS 的 4 种正则）。
func getMeta(html, property string) string {
	for _, re := range linkMetaRes[property] {
		if m := re.FindStringSubmatch(html); m != nil {
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
