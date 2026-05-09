package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/attson/atterm/internal/relay"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	webDir := flag.String("web", "web", "static web client directory (empty to disable)")
	origins := flag.String("origins", "", "comma-separated allowed Origin host patterns (empty = allow any, dev only)")
	flag.Parse()

	token := os.Getenv("ATTERM_TOKEN")
	if token == "" {
		log.Println("WARNING: ATTERM_TOKEN not set — running without auth (dev mode)")
	}

	cfg := relay.Config{
		Token:  token,
		WebDir: *webDir,
	}
	if *origins != "" {
		cfg.AllowedOrigins = splitCSV(*origins)
	}

	srv := relay.NewServer(cfg)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("atterm-relay listening on %s", *addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
