// Package web 移植 server/index.ts 上直接挂载的路由（不含 /signal WS，
// 也不含已拆分到 auth/media/moments/group/crypto/mls/burn/private-chat/
// home/friend/qr/apns/web-push/sticker/channel/misc/onebot 等子模块的接口）。
//
// API 契约与 Node 版保持一致：同路由、同 JSON 字段、同状态码、同中文错误文案。
package web

import (
	"net/http"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// Handler web 路由处理器。
type Handler struct {
	d *handler.Deps
}

// RegisterRoutes 注册 index.ts 上的直接挂载路由。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := &Handler{d: d}
	auth := d.Auth.UserAuth
	optAuth := d.Auth.OptionalAuth

	// 注意：以下路由已由并行开发的子模块实现，本模块不再重复注册
	// （Go ServeMux 对重复 pattern 会 panic）：
	//   - POST/GET /api/presence      → internal/handler/misc
	//   - OneBot v11 HTTP 兼容接口（/send_group_msg、/send_private_msg、/send_msg、
	//     /delete_msg、/get_login_info、/get_group_list、/get_group_info、
	//     /get_group_member_list、/get_status、/get_version_info）、
	//     POST /api/push-to-onebot、GET /api/onebot-status → internal/handler/onebot

	// ---- TRTC ----
	mux.Handle("GET /api/trtc/usersig", optAuth(http.HandlerFunc(h.trtcUserSig)))

	// ---- 登录信息 / 站点公开配置 / 健康检查 ----
	mux.HandleFunc("GET /api/login-info", h.loginInfo)
	mux.HandleFunc("GET /api/site-config-public", h.siteConfigPublic)
	mux.HandleFunc("GET /api/health", h.health)

	// ---- AI 聊天（S6：需要 userAuth） ----
	mux.Handle("POST /api/ai-chat", auth(http.HandlerFunc(h.aiChat)))

	// 注意：POST /api/voice/upload、GET /api/cos/sts 已由 internal/handler/media 实现，
	// 本模块不再重复注册（Go ServeMux 对重复 pattern 会 panic）。

	// ---- 腾讯地图 ----
	mux.HandleFunc("GET /api/txmap-config", h.txmapConfig)
	mux.HandleFunc("GET /api/txmap/geocoder/reverse", h.txmapReverse)
	mux.HandleFunc("GET /api/txmap/staticmap", h.txmapStaticmap)

	// ---- 个人资料 ----
	mux.Handle("GET /api/profile", optAuth(http.HandlerFunc(h.getProfile)))
	mux.Handle("PUT /api/profile", optAuth(http.HandlerFunc(h.putProfile)))

	// ---- 用户 ----
	mux.HandleFunc("GET /api/users/search", h.userSearch)
	mux.HandleFunc("GET /api/users/{userId}/presence", h.userPresence)
	mux.HandleFunc("GET /api/users/{userId}", h.getUser)

	// ---- 链接预览 ----
	mux.HandleFunc("GET /api/link-preview", h.linkPreview)

	// ---- 外链资料 / 投诉 / 外链解析 ----
	mux.HandleFunc("GET /api/q/profile/{userId}", h.qProfile)
	mux.HandleFunc("POST /api/report", h.report)
	mux.HandleFunc("GET /api/im/resolve/{slug}", h.imResolve)

	// ---- 外链落地页（必须在 SPA 通配之前） ----
	mux.HandleFunc("GET /im/{slug}", h.imLanding)
	mux.HandleFunc("GET /q/{userId}", h.qRedirect)
	mux.HandleFunc("GET /pyq/{userId}", h.pyqLanding)

	// ---- 静态文件 + SPA fallback（最后注册，兜底；不用 "GET /" 以免与 /api/admin/ 前缀冲突） ----
	mux.HandleFunc("/", h.serveStatic)
}
