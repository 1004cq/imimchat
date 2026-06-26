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
	"github.com/neomsg/neomsg/backend/internal/mtproto"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := mtproto.NewServer(cfg.MTProtoAddr, cfg.MTProtoRSAKey)
	if err != nil {
		log.Fatalf("mtproto server: %v", err)
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	go func() {
		log.Printf("[MTProto Gateway] health on %s", cfg.MTProtoHealthAddr)
		if err := http.ListenAndServe(cfg.MTProtoHealthAddr, nil); err != nil {
			log.Fatalf("health server: %v", err)
		}
	}()

	go func() {
		if err := server.ListenAndServe(ctx); err != nil {
			log.Fatalf("mtproto: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[MTProto Gateway] shutting down...")
	cancel()
	time.Sleep(time.Second)
}
