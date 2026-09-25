// Command server: CQIM Go 服务端（Node 版完整替代）。
//
// API 契约与 Node 版保持一致：同路由、同 JSON、同 session token、同 bcrypt。
// 直连现有 PostgreSQL（Prisma schema）与 Redis。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	ossignal "os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/config"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/admin"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/auth"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/channel"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/crypto"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/friend"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/group"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/media"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/misc"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/moments"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/onebot"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/privatechat"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/push"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/signal"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/sticker"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/web"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/redisx"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	// ---- 数据库 ----
	database, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[DB] 连接失败: %v", err)
	}
	defer database.Close()
	log.Println("[DB] PostgreSQL 已连接")

	// ---- Redis ----
	rdb := redisx.New(cfg.RedisURL)
	defer rdb.Close()
	if err := rdb.Ping(ctx); err != nil {
		log.Printf("[Redis] 连接失败（降级运行）: %v", err)
	} else {
		log.Println("[Redis] 已连接")
	}

	// ---- S13：确保默认管理员 ----
	ensureAdmin(ctx, database, cfg)

	// ---- 系统配置种子 ----
	seedSystemConfigs(ctx, database)

	deps := &handler.Deps{
		Cfg:   cfg,
		DB:    database,
		Redis: rdb,
		Auth:  &middleware.AuthContext{DB: database, Redis: rdb},
	}

	// ---- 路由 ----
	mux := http.NewServeMux()

	// 群引擎（signal 与 HTTP 共享）
	engine := group.NewEngine(deps)

	auth.RegisterRoutes(mux, deps)
	friend.RegisterRoutes(mux, deps)
	privatechat.RegisterRoutes(mux, deps)
	channel.RegisterRoutes(mux, deps, engine)
	moments.RegisterRoutes(mux, deps)
	crypto.RegisterRoutes(mux, deps)
	media.RegisterRoutes(mux, deps)
	sticker.RegisterRoutes(mux, deps)
	push.RegisterRoutes(mux, deps)
	misc.RegisterRoutes(mux, deps)
	group.RegisterRoutes(mux, deps, engine)
	web.RegisterRoutes(mux, deps)
	onebot.RegisterRoutes(mux, deps)

	// 管理后台：独立子 mux + IP 白名单 + CSRF
	adminMux := http.NewServeMux()
	admin.RegisterRoutes(adminMux, deps)
	mux.Handle("/api/admin/", middleware.AdminIPWhitelist(cfg,
		middleware.CsrfGenerate(middleware.CsrfVerify(adminMux))))

	// /signal WebSocket
	sig := signal.NewServer(deps, engine)
	sig.RegisterRoutes(mux)

	// ---- 中间件链 ----
	var h http.Handler = mux
	// 全局限流仅作用于 /api/
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			middleware.GlobalRateLimit(cfg, database, mux).ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
	h = middleware.SecurityHeaders(cfg, h)

	// ---- 后台任务 ----
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()
	sig.Start(bgCtx)
	go onebot.StartReverseClient(bgCtx, deps)
	go burnCleanupLoop(bgCtx, deps, sig)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("Server running on http://localhost:%s/", cfg.Port)
		log.Printf("Signal WebSocket: ws://localhost:%s/signal", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[HTTP] 启动失败: %v", err)
		}
	}()

	// 优雅退出
	quit := make(chan os.Signal, 1)
	ossignal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[HTTP] 正在关闭...")
	bgCancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if engine != nil {
		engine.Shutdown()
	}
	_ = srv.Shutdown(shutdownCtx)
	log.Println("[HTTP] 已关闭")
}

// ensureAdmin S13：无管理员时创建，密码优先读 ADMIN_INITIAL_PASSWORD，否则随机。
func ensureAdmin(ctx context.Context, database *db.DB, cfg *config.Config) {
	var count int
	if err := database.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM "AdminAccount"`).Scan(&count); err != nil {
		log.Printf("[DB] 管理员检查失败: %v", err)
		return
	}
	if count > 0 {
		return
	}
	envPassword := strings.TrimSpace(cfg.AdminInitialPassword)
	initialPassword := envPassword
	if initialPassword == "" {
		initialPassword = util.GenerateToken(24)
	}
	hash, err := util.HashPassword(initialPassword)
	if err != nil {
		log.Printf("[DB] 密码哈希失败: %v", err)
		return
	}
	_, err = database.Exec(ctx,
		`INSERT INTO "AdminAccount"("id","username","password","role","createdAt","updatedAt") VALUES($1,'admin',$2,'superadmin',NOW(),NOW())`,
		util.NewID(), hash)
	if err != nil {
		log.Printf("[DB] 创建默认管理员失败: %v", err)
		return
	}
	if envPassword != "" {
		log.Println("[DB] 已创建默认管理员账号: admin（密码来自 ADMIN_INITIAL_PASSWORD）")
	} else {
		log.Println("[DB] 已创建默认管理员账号: admin")
		log.Printf("[DB] ★★★ 初始管理员密码（仅显示一次，请立即保存并登录后修改）: %s", initialPassword)
	}
}

// seedSystemConfigs 系统配置键种子（与 db.ts 一致，upsert 不覆盖已有）。
func seedSystemConfigs(ctx context.Context, database *db.DB) {
	seeds := map[string]string{
		"smtp":           `{"host":"","port":465,"secure":true,"user":"","pass":"","fromName":"CQIM","fromEmail":"","enabled":false}`,
		"cos":            `{"secretId":"","secretKey":"","bucket":"","region":"ap-guangzhou","domain":"","pathPrefix":"imimchat","enabled":false}`,
		"site":           `{"name":"CQIM","description":"即时通讯系统","url":"","logo":"","icp":"","policeIcp":"","copyright":"","allowRegister":true,"requireApproval":false,"autoPlayVideo":false}`,
		"onebot":         `{"botId":"imim_bot","botName":"imim AI","accessToken":"","heartbeatInterval":30,"wsEnabled":true,"httpEnabled":true,"logEnabled":false}`,
		"ai":             `{"model":"gpt-4o-mini","triggerPrefix":"@AI","temperature":0.7,"maxTokens":1000,"systemPrompt":"你是一个友好的AI助手。","enabled":false}`,
		"emailTemplates": `{"verifyCode":{"subject":"您的验证码","body":"<p>您的验证码是：<strong>{{code}}</strong>，{{expire}}分钟内有效。</p>","expireMinutes":5},"welcome":{"subject":"欢迎加入 CQIM","body":"<p>您好 {{username}}，欢迎注册 CQIM！</p>","enabled":true},"resetPassword":{"subject":"重置密码","body":"<p>点击以下链接重置密码：<a href=\"{{link}}\">重置密码</a>，链接{{expire}}分钟内有效。</p>","expireMinutes":30},"loginAlert":{"subject":"异地登录通知","body":"<p>检测到您的账号在新设备登录，请确认是否为本人操作。</p>","enabled":true}}`,
	}
	for key, value := range seeds {
		_, _ = database.Exec(ctx,
			`INSERT INTO "SystemConfig"("id","key","value","createdAt","updatedAt") VALUES($1,$2,$3,NOW(),NOW())
ON CONFLICT("key") DO NOTHING`, util.NewID(), key, value)
	}
}

// burnCleanupLoop 阅后即焚定时清理（每 30 秒，与 index.ts 一致）。
func burnCleanupLoop(ctx context.Context, deps *handler.Deps, sig *signal.Server) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cleanExpiredBurnMessages(ctx, deps, sig)
		}
	}
}

func cleanExpiredBurnMessages(ctx context.Context, deps *handler.Deps, sig *signal.Server) {
	rows, err := deps.DB.Pool.Query(ctx,
		`SELECT "id","chatId" FROM "PrivateMessage" WHERE "burnExpireAt" IS NOT NULL AND "burnExpireAt" <= NOW() LIMIT 100`)
	if err != nil {
		return
	}
	type item struct{ id, chatID string }
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.chatID); err == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	if len(items) == 0 {
		return
	}
	for _, it := range items {
		var pa, pb string
		if err := deps.DB.Pool.QueryRow(ctx,
			`SELECT "participantA","participantB" FROM "Chat" WHERE "id"=$1`, it.chatID).Scan(&pa, &pb); err == nil {
			for _, uid := range []string{pa, pb} {
				sig.SendTo(uid, map[string]any{
					"type":    "burn_delete",
					"payload": map[string]any{"chatId": it.chatID, "messageId": it.id},
				})
			}
		}
	}
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.id
	}
	_, err = deps.DB.Pool.Exec(ctx, `DELETE FROM "PrivateMessage" WHERE "id" = ANY($1)`, ids)
	if err == nil {
		log.Printf("[BurnAfterRead] 定时清理: 删除 %d 条过期消息", len(ids))
	}
}
