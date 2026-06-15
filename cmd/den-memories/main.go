package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"den-memories/internal/app"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "HTTP listen address")
	dbPath := flag.String("db", envDefault("DEN_MEMORIES_DB", "./runtime/den-memories.sqlite"), "SQLite database path")
	root := flag.String("root", envDefault("DEN_MEMORIES_ROOT", "."), "repository root for contract artifacts")
	flag.Parse()

	srv, err := app.New(app.Config{
		DatabasePath: *dbPath,
		RootPath:     *root,
	})
	if err != nil {
		log.Fatalf("initialize service: %v", err)
	}
	defer srv.Close()

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("den-memories listening on http://%s", *addr)
		errCh <- httpServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
