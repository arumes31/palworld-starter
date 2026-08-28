package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/arumes31/palworld-starter/internal/broker"
)

func main() {
	token := os.Getenv("BROKER_TOKEN")
	allowed := strings.Split(os.Getenv("BROKER_ALLOWED_CONTAINERS"), ",")
	backend, err := broker.NewDockerBackend()
	if err != nil {
		log.Fatal(err)
	}
	defer backend.Close()
	controlServer, err := broker.NewServer(backend, token, allowed)
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              ":8081",
		Handler:           controlServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      40 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("broker shutdown: %v", err)
		}
	}()
	log.Printf("Docker broker listening on %s for %d allowlisted container(s)", server.Addr, len(allowed)) // #nosec G706 -- Addr is a constant above.
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
