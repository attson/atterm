package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/attson/atterm/internal/relay"
)

// Version is set at build time via -ldflags -X main.Version=<tag>.
var Version = "dev"

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	webDir := flag.String("web", "web", "static web client directory (empty to disable)")
	origins := flag.String("origins", "", "comma-separated allowed Origin host patterns (empty = allow any, dev only)")
	debugDefault := envEnabled("ATTERM_RELAY_DEBUG")
	debugPayloadDefault := envEnabled("ATTERM_RELAY_DEBUG_PAYLOAD") || envEnabled("ATTERM_RELAY_DEBUG_PAYLOADS")
	debug := flag.Bool("debug", debugDefault, "enable verbose relay interaction logs (or ATTERM_RELAY_DEBUG=1)")
	debugPayload := flag.Bool("debug-payload", debugPayloadDefault, "include IN/OUT byte contents in debug logs (or ATTERM_RELAY_DEBUG_PAYLOAD=1)")
	devInsecure := flag.Bool("dev-insecure", false, "allow insecure public relay settings (weak token); development/private networks only")
	flag.Parse()

	cfg, _, err := buildRelayConfig(relayOptions{
		addr:         *addr,
		webDir:       *webDir,
		version:      Version,
		token:        os.Getenv("ATTERM_TOKEN"),
		origins:      *origins,
		debug:        *debug || *debugPayload,
		debugPayload: *debugPayload,
		devInsecure:  *devInsecure,
		log:          os.Stderr,
	})
	if err != nil {
		log.Fatal(err)
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

func envEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
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

type relayOptions struct {
	addr         string
	webDir       string
	version      string
	token        string
	origins      string
	debug        bool
	debugPayload bool
	devInsecure  bool
	log          io.Writer
}

func buildRelayConfig(opts relayOptions) (relay.Config, string, error) {
	token := strings.TrimSpace(opts.token)
	generated := false
	if token == "" {
		var err error
		token, err = generateRelayToken()
		if err != nil {
			return relay.Config{}, "", err
		}
		generated = true
	}
	publicListen := isPublicListenAddr(opts.addr)
	weakToken := token == "dev" || len(token) < 16
	if publicListen && weakToken && !opts.devInsecure {
		return relay.Config{}, "", fmt.Errorf("refusing public relay with weak token; set a strong ATTERM_TOKEN or pass --dev-insecure for development")
	}
	allowedOrigins := splitCSV(opts.origins)

	cfg := relay.Config{
		Token:          token,
		WebDir:         opts.webDir,
		Version:        opts.version,
		AllowedOrigins: allowedOrigins,
		Debug:          opts.debug || opts.debugPayload,
		DebugPayload:   opts.debugPayload,
	}
	logStartupSecurity(opts, token, generated, publicListen)
	return cfg, token, nil
}

func logStartupSecurity(opts relayOptions, token string, generated, publicListen bool) {
	w := opts.log
	if w == nil {
		w = os.Stderr
	}
	logger := log.New(w, "", log.LstdFlags)
	if generated {
		logger.Printf("generated relay token: %s", token)
		logger.Printf("open %s/?token=%s", relayHTTPURL(opts.addr), token)
	}
	if opts.devInsecure {
		logger.Printf("WARNING: INSECURE relay mode enabled; tokens, terminal input, and output may be exposed")
	}
	if publicListen && !opts.devInsecure {
		logger.Printf("relay security: public listen requires a strong token")
		if strings.TrimSpace(opts.origins) == "" {
			logger.Printf("WARNING: --origins not set; browser WebSocket upgrades from any Origin are allowed")
		}
	}
}

func generateRelayToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func isPublicListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

func relayHTTPURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + strings.TrimPrefix(addr, "http://")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	if strings.Contains(host, ":") {
		host = "[" + strings.Trim(host, "[]") + "]"
	}
	return "http://" + net.JoinHostPort(host, port)
}
