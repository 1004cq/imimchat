package web

import (
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ============ 静态文件服务与 SPA fallback（对应 index.ts 约 3531-3720 行） ============
// TS 用 client/dist（按 NODE_ENV 切换 public/dist/public）；
// Go 版用环境变量 STATIC_DIR，默认 ./static。

const oneYearSeconds = 31536000

// pwaNoCacheFiles PWA 图标与 manifest 不强缓存，以便更新后立即生效。
var pwaNoCacheFiles = map[string]bool{
	"manifest.json": true, "apple-touch-icon.png": true, "favicon.ico": true,
	"favicon-16.png": true, "favicon-32.png": true,
	"icon-128.png": true, "icon-192.png": true, "icon-256.png": true,
	"icon-384.png": true, "icon-512.png": true,
	"logo.png": true, "logo.jpg": true, "icon-master.png": true,
	"apple-touch-icon-precomposed.png":         true,
	"apple-touch-icon-180x180.png":             true,
	"apple-touch-icon-180x180-precomposed.png": true,
	"apple-touch-icon-152x152.png":             true,
	"apple-touch-icon-152x152-precomposed.png": true,
	"apple-touch-icon-120x120.png":             true,
	"apple-touch-icon-120x120-precomposed.png": true,
	"apple-touch-icon-76x76.png":               true,
	"apple-touch-icon-76x76-precomposed.png":   true,
	"apple-touch-icon-60x60.png":               true,
	"apple-touch-icon-60x60-precomposed.png":   true,
}

func init() {
	// TGS 贴纸的正确 MIME 类型（对应 TS express.static.mime.define）
	_ = mime.AddExtensionType(".tgs", "application/x-tgsticker")
	_ = mime.AddExtensionType(".webp", "image/webp")
}

// staticDir 静态文件目录：STATIC_DIR，默认 ./static。
func staticDir() string {
	if v := os.Getenv("STATIC_DIR"); v != "" {
		return v
	}
	return "./static"
}

// setNoCacheHeaders HTML 与 PWA 文件不缓存（对应 TS setNoCacheHtmlHeaders）。
func setNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// serveStatic 静态文件服务 + SPA fallback（兜底路由 GET /）。
func (h *Handler) serveStatic(w http.ResponseWriter, r *http.Request) {
	dir := staticDir()
	p := path.Clean(r.URL.Path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}

	serveIndex := func() {
		data, err := os.ReadFile(filepath.Join(dir, "index.html"))
		if err != nil {
			writePlain(w, 404, "Not Found")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		setNoCacheHeaders(w)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}

	if p == "/" {
		serveIndex()
		return
	}

	// 防止路径穿越
	fullPath := filepath.Join(dir, filepath.FromSlash(p))
	rel, err := filepath.Rel(dir, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		writePlain(w, 404, "Not Found")
		return
	}
	if st, err := os.Stat(fullPath); err == nil && !st.IsDir() {
		serveStaticFile(w, r, fullPath, p, st.ModTime())
		return
	}
	// 有扩展名 → 404；否则 SPA fallback 到 index.html（对应 TS app.get("*")）
	if path.Ext(p) != "" {
		writePlain(w, 404, "Not Found")
		return
	}
	serveIndex()
}

// serveStaticFile 发送静态文件并设置缓存头（对应 TS express.static 配置）。
func serveStaticFile(w http.ResponseWriter, r *http.Request, fullPath, urlPath string, modTime time.Time) {
	f, err := os.Open(fullPath)
	if err != nil {
		writePlain(w, 404, "Not Found")
		return
	}
	defer f.Close()
	fileName := path.Base(urlPath)
	if strings.HasSuffix(urlPath, ".html") || pwaNoCacheFiles[fileName] {
		setNoCacheHeaders(w)
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	// Content-Type：优先按扩展名，否则嗅探
	if ct := mime.TypeByExtension(path.Ext(urlPath)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, fileName, modTime, f)
}
