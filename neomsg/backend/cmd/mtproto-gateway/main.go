package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/neomsg/neomsg/backend/internal/config"
	mtdelivery "github.com/neomsg/neomsg/backend/internal/mtproto/delivery"
	"github.com/neomsg/neomsg/backend/internal/mtproto"
	"github.com/neomsg/neomsg/backend/internal/mtproto/bridge"
	"github.com/neomsg/neomsg/backend/internal/mtproto/connmgr"
	"github.com/neomsg/neomsg/backend/internal/message"
	"github.com/neomsg/neomsg/backend/internal/push"
	"github.com/neomsg/neomsg/backend/internal/store/postgres"
	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	var nc *nats.Conn
	if cfg.NATSUrl != "" {
		nc, err = nats.Connect(cfg.NATSUrl)
		if err != nil {
			log.Printf("[MTProto Gateway] nats connect failed: %v", err)
		} else {
			defer nc.Close()
		}
	}

	pushDisp := push.NewDispatcher(rdb)
	engine := message.NewEngine(pg, rdb, nc, pushDisp)
	msgSvc := message.NewService(pg, rdb, engine)
	bridgeHandler := bridge.NewHandler(msgSvc)
	connManager := connmgr.NewManager()

	if nc != nil {
		sub := mtdelivery.NewSubscriber(connManager)
		if err := sub.Start(nc); err != nil {
			log.Fatalf("mtproto nats subscriber: %v", err)
		}
	}

	server, err := mtproto.NewServer(mtproto.ServerConfig{
		Addr:       cfg.MTProtoAddr,
		RSAKeyPath: cfg.MTProtoRSAKey,
		Bridge:     bridgeHandler,
		ConnMgr:    connManager,
		Redis:      rdb,
	})
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
