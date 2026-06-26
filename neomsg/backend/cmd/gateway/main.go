package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neomsg/neomsg/backend/internal/config"
	"github.com/neomsg/neomsg/backend/internal/gateway/session"
	tcpgw "github.com/neomsg/neomsg/backend/internal/gateway/tcp"
	wsgw "github.com/neomsg/neomsg/backend/internal/gateway/ws"
	"github.com/neomsg/neomsg/backend/internal/message"
	"github.com/neomsg/neomsg/backend/internal/push"
	"github.com/neomsg/neomsg/backend/internal/store/postgres"
	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 数据层
	pg, err := postgres.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pg.Close()

	rdb, err := redisstore.New(cfg.RedisAddr)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	// 业务层
	msgSvc := message.NewService(pg, rdb)
	pushDisp := push.NewDispatcher(rdb)
	sessions := session.NewManager()

	// 注册消息处理
	msgSvc.OnMessage(func(ctx context.Context, evt *message.Event) error {
		return sessions.BroadcastToDialog(ctx, evt.DialogID, evt.Payload, evt.ExcludeUser)
	})

	// WebSocket 网关
	wsHandler := wsgw.NewHandler(sessions, msgSvc, pushDisp)
	http.HandleFunc("/ws", wsHandler.ServeWS)

	// TCP 网关
	tcpServer := tcpgw.NewServer(cfg.TCPAddr, sessions, msgSvc)

	// HTTP 健康检查
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// 启动
	go func() {
		log.Printf("[Gateway] WebSocket listening on %s", cfg.WSAddr)
		if err := http.ListenAndServe(cfg.WSAddr, nil); err != nil {
			log.Fatalf("ws server: %v", err)
		}
	}()

	go func() {
		log.Printf("[Gateway] TCP listening on %s", cfg.TCPAddr)
		if err := tcpServer.ListenAndServe(ctx); err != nil {
			log.Fatalf("tcp server: %v", err)
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[Gateway] shutting down...")
	cancel()
	time.Sleep(time.Second)
}
