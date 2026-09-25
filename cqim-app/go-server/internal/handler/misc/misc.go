// Package misc 移植自 server 层的杂项小模块：
//
//   - qr.ts → POST /api/qr/verify 二维码校验
//   - home.ts → GET /api/home/sync 首页聚合
//   - presence.ts / presence-rules.ts + index.ts → GET/POST /api/presence 在线状态
//   - burn-message.ts → POST /api/group/burn-message 群阅后即焚（含过期清理 Cron）
//   - publish-im.ts → PublishImPush 跨节点 IM 推送；在线状态 helpers（online: 键）
//   - user-profile-sync.ts → PublishUserProfileUpdated / PublishUserProfileUpdatedById
//   - email.ts → SendEmail（nodemailer → 标准库 net/smtp，无新依赖）
//
// 备注：qr.ts 只做 payload 校验，不生成二维码图片（TS 端同样没有用到第三方二维码库），
// 因此 Go 版无需任何替代方案。
package misc

import (
	"net/http"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// Handler 杂项模块 handler。
type Handler struct {
	deps *handler.Deps
}

// RegisterRoutes 注册杂项模块全部路由。
//
//	POST /api/qr/verify          二维码校验（qr.ts）
//	GET  /api/home/sync          首页聚合（home.ts）
//	POST /api/presence           设置前后台状态（index.ts + presence.ts）
//	GET  /api/presence           读取前后台状态
//	POST /api/group/burn-message 群阅后即焚销毁（burn-message.ts，S5 鉴权保留）
//
// 全部路由需要登录（d.Auth.UserAuth）。注册时同时启动阅后即焚过期清理
// Cron（对应 Node 端 index.ts 的 startBurnCleanupCron(60000)）。
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	h := &Handler{deps: d}
	auth := d.Auth.UserAuth

	mux.Handle("POST /api/qr/verify", auth(http.HandlerFunc(h.qrVerify)))
	mux.Handle("GET /api/home/sync", auth(http.HandlerFunc(h.homeSync)))
	mux.Handle("POST /api/presence", auth(http.HandlerFunc(h.setPresenceHTTP)))
	mux.Handle("GET /api/presence", auth(http.HandlerFunc(h.getPresenceHTTP)))
	mux.Handle("POST /api/group/burn-message", auth(http.HandlerFunc(h.burnMessage)))

	StartBurnCleanupCron(d, time.Minute)
}
