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
	"net/url"
	"os"
	"os/signal"
	"strconv"
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
	origins := flag.String("origins", os.Getenv("ATTERM_ORIGINS"), "comma-separated allowed Origin hosts or URLs (or ATTERM_ORIGINS; empty = allow any only with --dev-insecure)")
	configPath := flag.String("config", os.Getenv("ATTERM_RELAY_CONFIG"), "persistent relay admin config path (or ATTERM_RELAY_CONFIG)")
	adminToken := flag.String("admin-token", os.Getenv("ATTERM_ADMIN_TOKEN"), "admin bearer token for /admin routes (or ATTERM_ADMIN_TOKEN; empty disables admin)")
	debugDefault := envEnabled("ATTERM_RELAY_DEBUG")
	debugPayloadDefault := envEnabled("ATTERM_RELAY_DEBUG_PAYLOAD") || envEnabled("ATTERM_RELAY_DEBUG_PAYLOADS")
	readOnlyTokens := flag.String("read-only-tokens", os.Getenv("ATTERM_READ_ONLY_TOKENS"), "comma-separated read-only bearer tokens (or ATTERM_READ_ONLY_TOKENS)")
	rateLimit := flag.Int("rate-limit-per-minute", envInt("ATTERM_RATE_LIMIT_PER_MINUTE", 0), "request/upgrade limit per remote IP/token per minute; 0=default, negative=disable")
	maxConns := flag.Int("max-connections-per-key", envInt("ATTERM_MAX_CONNECTIONS_PER_KEY", 0), "active websocket limit per remote IP/token; 0=default, negative=disable")
	debug := flag.Bool("debug", debugDefault, "enable verbose relay interaction logs (or ATTERM_RELAY_DEBUG=1)")
	debugPayload := flag.Bool("debug-payload", debugPayloadDefault, "include IN/OUT byte contents in debug logs (or ATTERM_RELAY_DEBUG_PAYLOAD=1)")
	devInsecure := flag.Bool("dev-insecure", false, "allow insecure public relay settings (weak token); development/private networks only")
	flag.Parse()

	cfg, _, err := buildRelayConfig(relayOptions{
		addr:         *addr,
		webDir:       *webDir,
		version:      Version,
		token:        os.Getenv("ATTERM_TOKEN"),
		readOnly:     *readOnlyTokens,
		origins:      *origins,
		configPath:   *configPath,
		adminToken:   *adminToken,
		rateLimit:    *rateLimit,
		maxConns:     *maxConns,
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

type relayOptions struct {
	addr         string
	webDir       string
	version      string
	token        string
	readOnly     string
	origins      string
	configPath   string
	adminToken   string
	rateLimit    int
	maxConns     int
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
	allowedOrigins := allowedOriginHosts(opts.origins)
	if publicListen && len(allowedOrigins) == 0 && !opts.devInsecure {
		return relay.Config{}, "", fmt.Errorf("refusing public relay without --origins; set --origins https://relay.example.com or pass --dev-insecure for development")
	}
	adminToken := strings.TrimSpace(opts.adminToken)
	if publicListen && adminToken != "" && isWeakAdminToken(adminToken) && !opts.devInsecure {
		return relay.Config{}, "", fmt.Errorf("refusing public relay with weak admin token; set a strong ATTERM_ADMIN_TOKEN or pass --dev-insecure for development")
	}
	adminCfg := relay.AdminConfig{}
	var adminStore *relay.AdminConfigStore
	if opts.configPath != "" {
		var err error
		adminCfg, err = relay.LoadAdminConfig(opts.configPath)
		if err != nil {
			return relay.Config{}, "", fmt.Errorf("load relay config: %w", err)
		}
		adminStore = relay.NewAdminConfigStore(opts.configPath, adminCfg)
	}
	rateLimit := opts.rateLimit
	if rateLimit == 0 {
		rateLimit = adminCfg.RateLimitPerMinute
	}
	maxConns := opts.maxConns
	if maxConns == 0 {
		maxConns = adminCfg.MaxConnectionsPerKey
	}
	readOnlyHashes := make([]string, 0, len(adminCfg.ReadOnlyTokens))
	for _, tok := range adminCfg.ReadOnlyTokens {
		readOnlyHashes = append(readOnlyHashes, tok.Hash)
	}

	cfg := relay.Config{
		Token:                token,
		ReadOnlyTokens:       splitCSV(opts.readOnly),
		ReadOnlyTokenHashes:  readOnlyHashes,
		WebDir:               opts.webDir,
		Version:              opts.version,
		AllowedOrigins:       allowedOrigins,
		Debug:                opts.debug || opts.debugPayload,
		DebugPayload:         opts.debugPayload,
		RateLimitPerMinute:   rateLimit,
		MaxConnectionsPerKey: maxConns,
		AdminToken:           adminToken,
		AdminConfigStore:     adminStore,
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
		logger.Printf("open %s/#token=%s", relayHTTPURL(opts.addr), token)
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

func isWeakAdminToken(token string) bool {
	token = strings.TrimSpace(token)
	return token == "admin" || token == "dev" || len(token) < 16
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
