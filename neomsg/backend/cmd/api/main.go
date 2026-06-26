package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neomsg/neomsg/backend/internal/auth"
	"github.com/neomsg/neomsg/backend/internal/config"
	"github.com/neomsg/neomsg/backend/internal/store/postgres"
	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

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

	authSvc := auth.NewService(pg, rdb, cfg.JWTSecret)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/auth/register", authSvc.HandleRegister)
	mux.HandleFunc("POST /v1/auth/login", authSvc.HandleLogin)
	mux.HandleFunc("POST /v1/auth/refresh", authSvc.HandleRefresh)
	mux.HandleFunc("GET /v1/auth/devices", authSvc.HandleListDevices)
	mux.HandleFunc("DELETE /v1/auth/devices/{deviceId}", authSvc.HandleRevokeDevice)
	mux.HandleFunc("POST /v1/auth/2fa/enable", authSvc.HandleEnable2FA)
	mux.HandleFunc("POST /v1/auth/2fa/verify", authSvc.HandleVerify2FA)

	srv := &http.Server{Addr: cfg.APIAddr, Handler: mux}

	go func() {
		log.Printf("[API] listening on %s", cfg.APIAddr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("api server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
}
