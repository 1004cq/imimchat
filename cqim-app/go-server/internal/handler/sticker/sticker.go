// Package sticker 移植自 cqim-app/server/sticker.ts：贴纸包 API 与贴纸静态文件服务。
//
// 路由（全部挂载在 /api/stickers 下，API 路由均经 userAuth）：
//
//	GET  /api/stickers/discover      发现页推荐目录（q/limit）
//	POST /api/stickers/install       安装贴纸包（目录匹配或 Telegram 导入）
//	GET  /api/stickers/packs         已安装贴纸包列表（includeStickers）
//	GET  /api/stickers/packs/{packId} 贴纸包详情
//	GET  /api/stickers/search        贴纸搜索（q/limit）
//	GET  /api/stickers/status        存储状态
//	POST /api/stickers/reload        重新加载 manifest（触发 CDN 预热）
//	GET  /api/stickers/files/        静态文件服务（无鉴权，与 Node 版一致；
//	                                 Cache-Control: public, max-age=604800, immutable，
//	                                 .tgs -> application/x-tgsticker，CORS *）
package sticker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

const (
	// stickerStaticPrefix 与 sticker.ts 的 STICKER_STATIC_PREFIX 一致。
	stickerStaticPrefix = "/api/stickers/files"
	maxDiscoverResults  = 24
)

// StickerItem 单个贴纸条目。
type StickerItem struct {
	ID       string   `json:"id"`
	Emoji    string   `json:"emoji"`
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	File     *string  `json:"file,omitempty"`
	Keywords []string `json:"keywords"`
	Width    *int     `json:"width,omitempty"`
	Height   *int     `json:"height,omitempty"`
	Format   string   `json:"format"`
	ThumbURL *string  `json:"thumbUrl,omitempty"`
}

// StickerPack 贴纸包。
type StickerPack struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Icon        string        `json:"icon"`
	Description string        `json:"description"`
	SourceType  string        `json:"sourceType"`
	ShortName   string        `json:"shortName"`
	ShareURL    string        `json:"shareUrl"`
	Keywords    []string      `json:"keywords"`
	Stickers    []StickerItem `json:"stickers"`
}

// StickerManifest 磁盘 manifest.json 的结构。
type StickerManifest struct {
	Version   int           `json:"version"`
	UpdatedAt string        `json:"updatedAt"`
	Source    string        `json:"source"`
	Packs     []StickerPack `json:"packs"`
}

// manifestView 归一化后的 manifest 视图（含统计）。
type manifestView struct {
	Version       int
	UpdatedAt     string
	Source        string
	Packs         []StickerPack
	PackCount     int
	StickerCount  int
	HasLocalFiles bool
}

// stickerPackSummary 贴纸包摘要（列表用）。
type stickerPackSummary struct {
	ID           string `json:"id"`
	ShortName    string `json:"shortName"`
	ShareURL     string `json:"shareUrl"`
	Name         string `json:"name"`
	Icon         string `json:"icon"`
	Description  string `json:"description"`
	SourceType   string `json:"sourceType"`
	StickerCount int    `json:"stickerCount"`
	Cover        string `json:"cover"`
	Installed    bool   `json:"installed"`
}

// stickerSearchResult 搜索结果条目。
type stickerSearchResult struct {
	StickerItem
	PackID   string `json:"packId"`
	PackName string `json:"packName"`
	PackIcon string `json:"packIcon"`
}

// Handler 贴纸模块 handler。
type Handler struct {
	deps         *handler.Deps
	mu           sync.Mutex // manifest 读写锁（Node 为单线程，Go 需防并发读写）
	stickerDir   string
	filesDir     string
	manifestPath string
}

func newHandler(d *handler.Deps) *Handler {
	dir := resolveStickerDir()
	return &Handler{
		deps:         d,
		stickerDir:   dir,
		filesDir:     filepath.Join(dir, "files"),
		manifestPath: filepath.Join(dir, "manifest.json"),
	}
}

// RegisterRoutes 注册贴纸 API 路由与静态文件服务。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := newHandler(d)
	h.ensureStore() // 与 Node 版 index.ts 启动时 ensureStickerStore() 一致
	auth := d.Auth.UserAuth
	mux.Handle("GET /api/stickers/files/", http.HandlerFunc(h.serveStickerFile))
	mux.Handle("GET /api/stickers/discover", auth(http.HandlerFunc(h.discover)))
	mux.Handle("POST /api/stickers/install", auth(http.HandlerFunc(h.install)))
	mux.Handle("GET /api/stickers/packs", auth(http.HandlerFunc(h.packs)))
	mux.Handle("GET /api/stickers/packs/{packId}", auth(http.HandlerFunc(h.packDetail)))
	mux.Handle("GET /api/stickers/search", auth(http.HandlerFunc(h.search)))
	mux.Handle("GET /api/stickers/status", auth(http.HandlerFunc(h.status)))
	mux.Handle("POST /api/stickers/reload", auth(http.HandlerFunc(h.reload)))
}

// resolveStickerDir 解析贴纸数据目录。
// Node 版为 server 目录的 ../data/stickers；Go 版优先 STICKER_DATA_DIR 环境变量，
// 否则从可执行文件向上查找 go.mod 定位模块根后取 <模块根>/../data/stickers，
// 找不到则回退到工作目录的 ../data/stickers（均与 Node 版同布局）。
func resolveStickerDir() string {
	if v := strings.TrimSpace(os.Getenv("STICKER_DATA_DIR")); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return abs
		}
		return v
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return filepath.Join(dir, "..", "data", "stickers")
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if abs, err := filepath.Abs(filepath.Join("..", "data", "stickers")); err == nil {
		return abs
	}
	return filepath.Join("..", "data", "stickers")
}

// ============================================================
// manifest 存储
// ============================================================

// ensureStore 确保贴纸目录与默认 manifest 存在（对应 ensureStickerStore）。
func (h *Handler) ensureStore() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ensureStoreLocked()
}

func (h *Handler) ensureStoreLocked() {
	_ = os.MkdirAll(h.stickerDir, 0o755)
	_ = os.MkdirAll(h.filesDir, 0o755)
	if _, err := os.Stat(h.manifestPath); os.IsNotExist(err) {
		data, _ := json.MarshalIndent(defaultManifest(), "", "  ")
		_ = os.WriteFile(h.manifestPath, data, 0o644)
	}
}

// manifestFile 用于容错解析磁盘 manifest（字段缺失时回退默认值）。
type manifestFile struct {
	Version   *int          `json:"version"`
	UpdatedAt *string       `json:"updatedAt"`
	Source    *string       `json:"source"`
	Packs     []StickerPack `json:"packs"`
}

// readManifest 安全读取 manifest（对应 safeReadManifest）。
func (h *Handler) readManifest() StickerManifest {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ensureStoreLocked()
	raw, err := os.ReadFile(h.manifestPath)
	if err != nil {
		return defaultManifest()
	}
	var parsed manifestFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return defaultManifest()
	}
	m := StickerManifest{
		Version:   1,
		UpdatedAt: nowISO(),
		Source:    "manifest",
		Packs:     defaultManifestPacks(),
	}
	if parsed.Version != nil {
		m.Version = *parsed.Version
	}
	if parsed.UpdatedAt != nil {
		m.UpdatedAt = *parsed.UpdatedAt
	}
	if parsed.Source != nil {
		m.Source = *parsed.Source
	}
	if parsed.Packs != nil {
		m.Packs = parsed.Packs
	}
	return m
}

// writeManifest 安全写入 manifest（对应 safeWriteManifest）。
func (h *Handler) writeManifest(m StickerManifest) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ensureStoreLocked()
	data, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(h.manifestPath, data, 0o644)
}

// getManifest 返回归一化 + 去重 + 过滤空包后的 manifest 视图（含统计）。
func (h *Handler) getManifest() manifestView {
	m := h.readManifest()
	packs := make([]StickerPack, 0, len(m.Packs))
	for _, p := range dedupePacks(m.Packs) {
		if len(p.Stickers) > 0 {
			packs = append(packs, p)
		}
	}
	v := manifestView{
		Version:   m.Version,
		UpdatedAt: m.UpdatedAt,
		Source:    m.Source,
		Packs:     packs,
		PackCount: len(packs),
	}
	for _, p := range packs {
		v.StickerCount += len(p.Stickers)
		if p.SourceType == "local" {
			v.HasLocalFiles = true
		}
	}
	return v
}

// manifestMeta 构造各接口通用的 meta 段。
func manifestMeta(m manifestView) map[string]any {
	return map[string]any{
		"version":               m.Version,
		"updatedAt":             m.UpdatedAt,
		"source":                m.Source,
		"telegramBotConfigured": telegramToken() != "",
		"packCount":             m.PackCount,
		"stickerCount":          m.StickerCount,
		"hasLocalFiles":         m.HasLocalFiles,
	}
}

// ============================================================
// 归一化（与 sticker.ts normalize* 一一对应）
// ============================================================

func buildShareURL(shortName, packID string) string {
	slug := shortName
	if slug == "" {
		slug = packID
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "https://t.me/addstickers/" + slug
}

// encodeURIComponentSeg 按 JS encodeURIComponent 规则编码单个路径段。
func encodeURIComponentSeg(s string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(unreserved, c) >= 0 {
			sb.WriteByte(c)
		} else {
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}

// normalizeStickerURL 本地 file 优先映射为静态服务 URL（对应 normalizeStickerUrl）。
func normalizeStickerURL(item StickerItem) string {
	if item.File != nil && strings.TrimSpace(*item.File) != "" {
		segs := strings.FieldsFunc(*item.File, func(r rune) bool { return r == '/' || r == '\\' })
		enc := make([]string, 0, len(segs))
		for _, s := range segs {
			enc = append(enc, encodeURIComponentSeg(s))
		}
		return stickerStaticPrefix + "/" + strings.Join(enc, "/")
	}
	if strings.TrimSpace(item.URL) != "" {
		return item.URL
	}
	return ""
}

// inferFormat 按 url/file 后缀推断格式（对应 inferFormat）。
func inferFormat(item StickerItem) string {
	target := item.URL
	if target == "" && item.File != nil {
		target = *item.File
	}
	target = strings.ToLower(target)
	switch {
	case strings.HasSuffix(target, ".tgs"):
		return "tgs"
	case strings.HasSuffix(target, ".json"):
		return "json"
	case strings.HasSuffix(target, ".webp"):
		return "webp"
	case strings.HasSuffix(target, ".png"):
		return "png"
	case strings.HasSuffix(target, ".jpg"):
		return "jpg"
	case strings.HasSuffix(target, ".jpeg"):
		return "jpeg"
	case strings.HasSuffix(target, ".gif"):
		return "gif"
	default:
		return "webp"
	}
}

// stickerHasFile 对应 JS 的 truthy 判断（"" 视为无文件）。
func stickerHasFile(item StickerItem) bool {
	return item.File != nil && *item.File != ""
}

// normalizeStickerItem 归一化单个贴纸（对应 normalizeStickerItem）。
func normalizeStickerItem(in StickerItem) StickerItem {
	format := in.Format
	if format == "" {
		format = inferFormat(in)
	}
	emoji := in.Emoji
	if emoji == "" {
		emoji = "🙂"
	}
	name := in.Name
	if name == "" {
		name = in.ID
	}
	keywords := in.Keywords
	if keywords == nil {
		keywords = []string{}
	}
	return StickerItem{
		ID:       in.ID,
		Emoji:    emoji,
		Name:     name,
		URL:      normalizeStickerURL(in),
		File:     in.File,
		Keywords: keywords,
		Width:    in.Width,
		Height:   in.Height,
		Format:   format,
		ThumbURL: in.ThumbURL,
	}
}

// normalizePack 归一化贴纸包（对应 normalizePack）。
func normalizePack(in StickerPack) StickerPack {
	stickers := make([]StickerItem, 0, len(in.Stickers))
	for _, s := range in.Stickers {
		n := normalizeStickerItem(s)
		if n.URL != "" {
			stickers = append(stickers, n)
		}
	}
	icon := in.Icon
	if icon == "" {
		if len(stickers) > 0 {
			icon = stickers[0].Emoji
		} else {
			icon = "🙂"
		}
	}
	sourceType := in.SourceType
	if sourceType == "" {
		sourceType = "remote"
		for _, s := range stickers {
			if stickerHasFile(s) {
				sourceType = "local"
				break
			}
		}
	}
	shortName := strings.TrimSpace(in.ShortName)
	if shortName == "" {
		shortName = in.ID
	}
	shareURL := strings.TrimSpace(in.ShareURL)
	if shareURL == "" {
		shareURL = buildShareURL(in.ShortName, in.ID)
	}
	keywords := in.Keywords
	if keywords == nil {
		keywords = []string{}
	}
	name := in.Name
	if name == "" {
		name = in.ID
	}
	return StickerPack{
		ID:          in.ID,
		Name:        name,
		Icon:        icon,
		Description: in.Description,
		SourceType:  sourceType,
		ShortName:   shortName,
		ShareURL:    shareURL,
		Keywords:    keywords,
		Stickers:    stickers,
	}
}

// dedupePacks 按 id 去重（首个胜出），附带归一化（对应 dedupePacks）。
func dedupePacks(packs []StickerPack) []StickerPack {
	seen := make(map[string]bool, len(packs))
	out := make([]StickerPack, 0, len(packs))
	for _, p := range packs {
		n := normalizePack(p)
		if n.ID == "" || seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		out = append(out, n)
	}
	return out
}

// summarizePack 贴纸包摘要（对应 summarizePack）。
func summarizePack(pack StickerPack, installed map[string]bool) stickerPackSummary {
	shortName := pack.ShortName
	if shortName == "" {
		shortName = pack.ID
	}
	shareURL := pack.ShareURL
	if shareURL == "" {
		shareURL = buildShareURL(pack.ShortName, pack.ID)
	}
	icon := pack.Icon
	if icon == "" {
		icon = "🙂"
	}
	sourceType := pack.SourceType
	if sourceType == "" {
		sourceType = "remote"
	}
	cover := ""
	if len(pack.Stickers) > 0 {
		if pack.Stickers[0].ThumbURL != nil && *pack.Stickers[0].ThumbURL != "" {
			cover = *pack.Stickers[0].ThumbURL
		} else {
			cover = pack.Stickers[0].URL
		}
	}
	return stickerPackSummary{
		ID:           pack.ID,
		ShortName:    shortName,
		ShareURL:     shareURL,
		Name:         pack.Name,
		Icon:         icon,
		Description:  pack.Description,
		SourceType:   sourceType,
		StickerCount: len(pack.Stickers),
		Cover:        cover,
		Installed:    installed[pack.ID],
	}
}

var addStickersRe = regexp.MustCompile(`(?i)addstickers/([^/?#]+)`)

// parseStickerPackInput 解析用户输入的短名/链接（对应 parseStickerPackInput）。
func parseStickerPackInput(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	if m := addStickersRe.FindStringSubmatch(trimmed); m != nil {
		slug := m[1]
		if u, err := url.PathUnescape(slug); err == nil {
			slug = u
		}
		return strings.ToLower(strings.TrimSpace(slug))
	}
	s := strings.TrimPrefix(trimmed, "@")
	return strings.ToLower(strings.TrimSpace(s))
}

// packMatchesQuery 贴纸包是否命中搜索词（对应 packMatchesQuery，query 已小写）。
func packMatchesQuery(pack StickerPack, query string) bool {
	if query == "" {
		return true
	}
	contains := func(s string) bool { return strings.Contains(strings.ToLower(s), query) }
	if contains(pack.ID) || contains(pack.Name) || contains(pack.ShortName) ||
		contains(pack.Description) || contains(pack.ShareURL) {
		return true
	}
	for _, kw := range pack.Keywords {
		if contains(kw) {
			return true
		}
	}
	for _, s := range pack.Stickers {
		if contains(s.Name) {
			return true
		}
		for _, kw := range s.Keywords {
			if contains(kw) {
				return true
			}
		}
	}
	return false
}

// getDiscoveryCatalog 发现页目录 = DISCOVERY_CATALOG + 默认包（对应 getDiscoveryCatalog）。
func getDiscoveryCatalog() []StickerPack {
	all := append(discoveryCatalogPacks(), defaultManifestPacks()...)
	return dedupePacks(all)
}

// findCatalogPack 在发现目录中按短名/id/名称查找（对应 findCatalogPack）。
func findCatalogPack(input string) *StickerPack {
	slug := parseStickerPackInput(input)
	if slug == "" {
		return nil
	}
	for _, p := range getDiscoveryCatalog() {
		if strings.ToLower(p.ID) == slug ||
			strings.ToLower(p.ShortName) == slug ||
			strings.ToLower(p.Name) == slug {
			cp := p
			return &cp
		}
	}
	return nil
}

// ============================================================
// 小工具
// ============================================================

// nowISO 与 new Date().toISOString() 同格式（含毫秒）。
func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// parseJSInt 模拟 JS parseInt(x, 10) || def 的行为：
// 前导数字解析、无数字或结果为 0 时回退 def（JS 中 0/NaN 均为 falsy）。
func parseJSInt(s string, def int) int {
	s = strings.TrimSpace(s)
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	n := 0
	digits := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		digits++
	}
	if digits == 0 {
		return def
	}
	if neg {
		n = -n
	}
	if n == 0 {
		return def
	}
	return n
}

// jsString 模拟 JS String(value || ”)：falsy 值转为空字符串。
func jsString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == 0 || t != t {
			return ""
		}
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(t, 'f', -1, 64))
	case bool:
		if !t {
			return ""
		}
		return "true"
	default:
		return ""
	}
}

func installedSet(packs []StickerPack) map[string]bool {
	set := make(map[string]bool, len(packs))
	for _, p := range packs {
		set[p.ID] = true
	}
	return set
}

// ============================================================
// 路由 handlers
// ============================================================

// discover GET /api/stickers/discover
func (h *Handler) discover(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := parseJSInt(r.URL.Query().Get("limit"), 12)
	if limit < 1 {
		limit = 1
	}
	if limit > maxDiscoverResults {
		limit = maxDiscoverResults
	}
	manifest := h.getManifest()
	installed := installedSet(manifest.Packs)
	var matched []StickerPack
	for _, p := range getDiscoveryCatalog() {
		if packMatchesQuery(p, q) {
			matched = append(matched, p)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		ai, aj := 0, 0
		if installed[matched[i].ID] {
			ai = 1
		}
		if installed[matched[j].ID] {
			aj = 1
		}
		if ai != aj {
			return ai < aj
		}
		// Node 用 localeCompare(b.name, 'zh-CN')；Go 标准库无中文拼音排序，
		// 用码点序近似（固定内置数据，顺序稳定）。
		return matched[i].Name < matched[j].Name
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	packs := make([]stickerPackSummary, 0, len(matched))
	for _, p := range matched {
		packs = append(packs, summarizePack(p, installed))
	}
	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"query":   q,
		"packs":   packs,
		"meta": map[string]any{
			"total":          len(packs),
			"installedCount": manifest.PackCount,
			"source":         "builtin-catalog",
		},
	})
}

// install POST /api/stickers/install
func (h *Handler) install(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if r.ContentLength != 0 {
		if !util.DecodeJSON(w, r, &body) {
			return
		}
	}
	rawInput := ""
	for _, k := range []string{"input", "shareUrl", "shortName", "packId"} {
		if s := jsString(body[k]); s != "" {
			rawInput = s
			break
		}
	}
	if rawInput == "" {
		util.WriteJSON(w, 400, map[string]any{"success": false, "error": "请提供贴纸包短名、链接或 packId"})
		return
	}

	candidate := findCatalogPack(rawInput)
	importedFromTelegram := false
	if candidate == nil && telegramToken() != "" {
		pack, err := importTelegramPack(r.Context(), h.filesDir, rawInput)
		if err != nil {
			util.WriteJSON(w, 502, map[string]any{
				"success": false,
				"error":   err.Error(),
				"meta":    map[string]any{"telegramBotConfigured": true},
			})
			return
		}
		candidate = pack
		importedFromTelegram = true
	}
	if candidate == nil {
		errMsg := "暂未收录该贴纸包，请先输入推荐列表中的短链或名称"
		if telegramToken() != "" {
			errMsg = "暂未找到该 Telegram 贴纸包"
		}
		util.WriteJSON(w, 404, map[string]any{
			"success": false,
			"error":   errMsg,
			"meta":    map[string]any{"telegramBotConfigured": telegramToken() != ""},
		})
		return
	}

	current := h.readManifest()
	alreadyInstalled := false
	candShort := strings.ToLower(candidate.ShortName)
	for _, p := range current.Packs {
		if p.ID == candidate.ID || strings.ToLower(p.ShortName) == candShort {
			alreadyInstalled = true
			break
		}
	}
	if !alreadyInstalled {
		current.Packs = dedupePacks(append([]StickerPack{*candidate}, current.Packs...))
		current.UpdatedAt = nowISO()
		if importedFromTelegram {
			current.Source = "telegram-bot-install"
		} else {
			current.Source = "catalog-install"
		}
		h.writeManifest(current)
	}

	manifest := h.getManifest()
	installed := installedSet(manifest.Packs)
	var pack StickerPack
	found := false
	for _, p := range manifest.Packs {
		if p.ID == candidate.ID {
			pack = p
			found = true
			break
		}
	}
	if !found {
		pack = *candidate
	}

	// 异步触发 CDN 预热（不阻塞响应），与 Node 版语义一致。
	if !alreadyInstalled && pack.SourceType != "local" && isPreheatEnabled() {
		p := pack
		go func() {
			defer func() { _ = recover() }()
			preheatStickerPack(p)
		}()
	}

	util.WriteJSON(w, 200, map[string]any{
		"success":          true,
		"alreadyInstalled": alreadyInstalled,
		"pack":             summarizePack(pack, installed),
		"meta":             manifestMeta(manifest),
	})
}

// packs GET /api/stickers/packs
func (h *Handler) packs(w http.ResponseWriter, r *http.Request) {
	includeStickers := r.URL.Query().Get("includeStickers") == "true"
	manifest := h.getManifest()
	installed := installedSet(manifest.Packs)
	var packs any
	if includeStickers {
		if manifest.Packs == nil {
			packs = []StickerPack{}
		} else {
			packs = manifest.Packs
		}
	} else {
		summaries := make([]stickerPackSummary, 0, len(manifest.Packs))
		for _, p := range manifest.Packs {
			summaries = append(summaries, summarizePack(p, installed))
		}
		packs = summaries
	}
	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"packs":   packs,
		"meta":    manifestMeta(manifest),
	})
}

// packDetail GET /api/stickers/packs/{packId}
func (h *Handler) packDetail(w http.ResponseWriter, r *http.Request) {
	packID := r.PathValue("packId")
	manifest := h.getManifest()
	for _, p := range manifest.Packs {
		if p.ID == packID {
			util.WriteJSON(w, 200, map[string]any{
				"success": true,
				"pack":    p,
				"meta": map[string]any{
					"version":   manifest.Version,
					"updatedAt": manifest.UpdatedAt,
					"source":    manifest.Source,
				},
			})
			return
		}
	}
	util.WriteJSON(w, 404, map[string]any{"success": false, "error": "贴纸包不存在"})
}

// search GET /api/stickers/search
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if q == "" {
		util.WriteJSON(w, 200, map[string]any{
			"success": true,
			"query":   "",
			"results": []stickerSearchResult{},
		})
		return
	}
	limit := parseJSInt(r.URL.Query().Get("limit"), 60)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	manifest := h.getManifest()
	results := make([]stickerSearchResult, 0)
	for _, pack := range manifest.Packs {
		packNameHit := strings.Contains(strings.ToLower(pack.Name), q)
		icon := pack.Icon
		if icon == "" {
			icon = "🙂"
		}
		for _, s := range pack.Stickers {
			hit := strings.Contains(strings.ToLower(s.Name), q) ||
				strings.Contains(s.Emoji, q) ||
				packNameHit
			if !hit {
				for _, kw := range s.Keywords {
					if strings.Contains(strings.ToLower(kw), q) {
						hit = true
						break
					}
				}
			}
			if hit {
				results = append(results, stickerSearchResult{
					StickerItem: s,
					PackID:      pack.ID,
					PackName:    pack.Name,
					PackIcon:    icon,
				})
			}
		}
	}
	if len(results) > limit {
		results = results[:limit]
	}
	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"query":   q,
		"results": results,
	})
}

// status GET /api/stickers/status
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	manifest := h.getManifest()
	localFileCount := 0
	for _, p := range manifest.Packs {
		for _, s := range p.Stickers {
			if stickerHasFile(s) {
				localFileCount++
			}
		}
	}
	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"status": map[string]any{
			"version":         manifest.Version,
			"updatedAt":       manifest.UpdatedAt,
			"source":          manifest.Source,
			"stickerDir":      h.stickerDir,
			"manifestPath":    h.manifestPath,
			"staticPrefix":    stickerStaticPrefix,
			"packCount":       manifest.PackCount,
			"stickerCount":    manifest.StickerCount,
			"localFileCount":  localFileCount,
			"remoteFileCount": manifest.StickerCount - localFileCount,
			"hasLocalFiles":   manifest.HasLocalFiles,
		},
	})
}

// reload POST /api/stickers/reload
func (h *Handler) reload(w http.ResponseWriter, r *http.Request) {
	manifest := h.getManifest()

	// 异步触发全量 CDN 预热（不阻塞响应），与 Node 版语义一致。
	if isPreheatEnabled() {
		var urls []string
		for _, p := range manifest.Packs {
			if p.SourceType == "local" {
				continue
			}
			for _, s := range p.Stickers {
				if s.URL != "" {
					urls = append(urls, s.URL)
				}
				if s.ThumbURL != nil && *s.ThumbURL != "" {
					urls = append(urls, *s.ThumbURL)
				}
			}
		}
		if len(urls) > 0 {
			go func() {
				defer func() { _ = recover() }()
				preheatCdnResources(urls)
			}()
		}
	}

	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"message": "贴纸 manifest 已重新加载（CDN 预热已触发）",
		"meta":    manifestMeta(manifest),
	})
}

// serveStickerFile GET /api/stickers/files/ 静态文件服务。
// 与 Node 版 express.static 行为对齐：无鉴权；Cache-Control: public, max-age=604800, immutable；
// .tgs 返回 application/x-tgsticker；Access-Control-Allow-Origin: *。
// 目录穿越防护：按段解码校验（拒绝 ".." 与内含 / \ 的段）+ 最终路径前缀双重校验。
func (h *Handler) serveStickerFile(w http.ResponseWriter, r *http.Request) {
	const prefix = stickerStaticPrefix + "/"
	rel := strings.TrimPrefix(r.URL.EscapedPath(), prefix)
	var segs []string
	for _, s := range strings.Split(rel, "/") {
		if s == "" || s == "." {
			continue
		}
		u, err := url.PathUnescape(s)
		if err != nil || u == "" || u == "." || u == ".." ||
			strings.ContainsRune(u, '/') || strings.ContainsRune(u, '\\') {
			http.NotFound(w, r)
			return
		}
		segs = append(segs, u)
	}
	if len(segs) == 0 {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(append([]string{h.filesDir}, segs...)...)
	// 双重保险：最终路径必须位于 filesDir 之内。
	if relPath, err := filepath.Rel(h.filesDir, full); err != nil ||
		relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		// Node 版 express.static 遇到目录会 next() 到路由最终 404；此处直接 404。
		http.NotFound(w, r)
		return
	}
	// 与 Node 版 setHeaders 一致的缓存头与 CORS 头。
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if strings.HasSuffix(strings.ToLower(st.Name()), ".tgs") {
		w.Header().Set("Content-Type", "application/x-tgsticker")
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}
