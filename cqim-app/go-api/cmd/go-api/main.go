package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/1004cq/imimchat/cqim-app/go-api/internal/api"
)

func main() {
	server, closeResources, err := api.NewServerFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	defer closeResources()

	httpServer := &http.Server{
		Addr:              server.ListenAddr(),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("go-api listening on %s", httpServer.Addr)
		errCh <- httpServer.ListenAndServe()
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-signalCh:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
	}
}
