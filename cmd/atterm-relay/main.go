package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/attson/atterm/internal/relay"
	"github.com/attson/atterm/internal/userstore"
	"github.com/attson/atterm/internal/webpush"
	"github.com/attson/atterm/internal/webhook"
)

// Version is set at build time via -ldflags -X main.Version=<tag>.
var Version = "dev"

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	webDir := flag.String("web", "", "static web client directory; empty uses the embedded FS (production default)")
	origins := flag.String("origins", os.Getenv("ATTERM_ORIGINS"), "comma-separated allowed Origin hosts or URLs (or ATTERM_ORIGINS; empty = allow any only with --dev-insecure)")
	configPath := flag.String("config", os.Getenv("ATTERM_RELAY_CONFIG"), "persistent relay admin config path (or ATTERM_RELAY_CONFIG)")
	configDir := flag.String("config-dir", envOr("ATTERM_RELAY_CONFIG_DIR", ""), "persistent relay state directory for web-push.json etc. (or ATTERM_RELAY_CONFIG_DIR)")
	vapidSubject := flag.String("vapid-subject", envOr("ATTERM_VAPID_SUBJECT", "mailto:noreply@atterm.local"), "VAPID subject (mailto: or https: URL; advertised to push services)")
	debugDefault := envEnabled("ATTERM_RELAY_DEBUG")
	debugPayloadDefault := envEnabled("ATTERM_RELAY_DEBUG_PAYLOAD") || envEnabled("ATTERM_RELAY_DEBUG_PAYLOADS")
	rateLimit := flag.Int("rate-limit-per-minute", envInt("ATTERM_RATE_LIMIT_PER_MINUTE", 0), "request/upgrade limit per remote IP/token per minute; 0=default, negative=disable")
	maxConns := flag.Int("max-connections-per-key", envInt("ATTERM_MAX_CONNECTIONS_PER_KEY", 0), "active websocket limit per remote IP/token; 0=default, negative=disable")
	debug := flag.Bool("debug", debugDefault, "enable verbose relay interaction logs (or ATTERM_RELAY_DEBUG=1)")
	debugPayload := flag.Bool("debug-payload", debugPayloadDefault, "include IN/OUT byte contents in debug logs (or ATTERM_RELAY_DEBUG_PAYLOAD=1)")
	devInsecure := flag.Bool("dev-insecure", false, "allow insecure public relay settings (unbootstrapped admin); development/private networks only")
	flag.Parse()

	publicListen := isPublicListenAddr(*addr)
	bootstrapEmail := strings.TrimSpace(os.Getenv("ATTERM_BOOTSTRAP_ADMIN_EMAIL"))

	if publicListen && !*devInsecure && bootstrapEmail == "" {
		log.Fatal("ATTERM_BOOTSTRAP_ADMIN_EMAIL must be set for a public relay; pass --dev-insecure to skip (development only)")
	}

	allowedOrigins := allowedOriginHosts(*origins)
	if publicListen && len(allowedOrigins) == 0 && !*devInsecure {
		log.Fatal("refusing public relay without --origins; set --origins https://relay.example.com or pass --dev-insecure for development")
	}

	// Resolve persistence directory.
	persistDir := *configDir
	if persistDir == "" && *configPath != "" {
		persistDir = filepath.Dir(*configPath)
	}
	if persistDir == "" {
		persistDir = "./data/atterm-relay"
	}

	// Open user store (SQLite). Create the persistence directory if it
	// doesn't exist yet — first-run bootstrap should "just work" without
	// requiring the operator to pre-create the directory. Without this
	// SQLite fails with a misleading "out of memory (CANTOPEN)" error.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(persistDir, 0o700); err != nil {
		log.Fatalf("create persist dir %s: %v", persistDir, err)
	}

	dbPath := filepath.Join(persistDir, "users.db")
	store, err := userstore.Open(ctx, dbPath)
	if err != nil {
		log.Fatalf("open userstore: %v", err)
	}

	// OPAQUE server singleton — loads persisted OPRF seed + AKE keypair, or
	// generates them on first boot. Must run after userstore.Open (depends on
	// the opaque_server_state table created by the 0003 migration) and before
	// NewServer (Config.OpaqueServer is consumed there).
	opaqueSrv, err := relay.LoadOrInitOpaqueServer(ctx, store)
	if err != nil {
		log.Fatalf("opaque server init: %v", err)
	}

	if err := bootstrapAdmin(ctx, store, bootstrapEmail); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}

	adminCfg := relay.AdminConfig{}
	var adminStore *relay.AdminConfigStore
	if *configPath != "" {
		var err error
		adminCfg, err = relay.LoadAdminConfig(*configPath)
		if err != nil {
			log.Fatalf("load relay config: %v", err)
		}
		adminStore = relay.NewAdminConfigStore(*configPath, adminCfg)
	}
	rateVal := *rateLimit
	if rateVal == 0 {
		rateVal = adminCfg.RateLimitPerMinute
	}
	maxVal := *maxConns
	if maxVal == 0 {
		maxVal = adminCfg.MaxConnectionsPerKey
	}

	resolver := relay.NewIdentityResolver(store)

	var webFS fs.FS
	if *webDir == "" {
		webFS = relay.EmbeddedWebFS()
	} else {
		webFS = os.DirFS(*webDir)
	}

	cfg := relay.Config{
		WebFS:                webFS,
		Version:              Version,
		AllowedOrigins:       allowedOrigins,
		Debug:                *debug || *debugPayload,
		DebugPayload:         *debugPayload,
		RateLimitPerMinute:   rateVal,
		MaxConnectionsPerKey: maxVal,
		AdminConfigStore:     adminStore,
		Resolver:             resolver,
		Store:                store,
		OpaqueServer:         opaqueSrv,
	}

	wpSvc, wpErr := webpush.Open(persistDir, *vapidSubject)
	if wpErr != nil {
		log.Printf("WARN: web-push disabled: %v", wpErr)
		wpSvc = nil
	}
	cfg.WebPush = wpSvc
	cfg.Webhook = webhook.New(webhookStoreAdapter{store})

	if *devInsecure {
		log.Printf("WARNING: INSECURE relay mode enabled; tokens, terminal input, and output may be exposed")
	}

	srv := relay.NewServer(cfg)

	if wpSvc != nil {
		// Schedule daily cleanup of legacy web-push subscription files.
		go func() {
			t := time.NewTicker(24 * time.Hour)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					if err := webpush.CleanupLegacy(ctx, persistDir); err != nil {
						log.Printf("webpush: CleanupLegacy: %v", err)
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

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

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	out, err := strconv.Atoi(value)
	if err != nil {
		log.Fatalf("%s must be an integer: %v", name, err)
	}
	return out
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
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

var desktopWebviewOriginHosts = []string{
	"wails",
	"wails.localhost",
	"wails.localhost:*",
}

func allowedOriginHosts(raw string) []string {
	origins := splitCSV(raw)
	if len(origins) == 0 {
		return nil
	}
	out := make([]string, 0, len(origins)+len(desktopWebviewOriginHosts))
	for _, origin := range origins {
		out = appendUniqueString(out, normalizeOriginHostPattern(origin))
	}
	// nhooyr matches OriginPatterns against the Origin host only. A packaged
	// Wails desktop client uses these local asset hosts, while the remote relay
	// still requires the bearer token before accepting any WebSocket.
	for _, origin := range desktopWebviewOriginHosts {
		out = appendUniqueString(out, origin)
	}
	return out
}

func normalizeOriginHostPattern(origin string) string {
	if u, err := url.Parse(origin); err == nil && u.Host != "" {
		return u.Host
	}
	return origin
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
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
