package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
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
)

// Version is set at build time via -ldflags -X main.Version=<tag>.
var Version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		os.Exit(migrateCmd(os.Args[2:]))
	}
	addr := flag.String("addr", ":8080", "plain HTTP listen address — for a reverse proxy / internal use (empty disables)")
	httpsAddr := flag.String("https-addr", envOr("ATTERM_HTTPS_ADDR", ""), "HTTPS listen address — opt-in; requires ATTERM_TLS_CERT+ATTERM_TLS_KEY (or ATTERM_HTTPS_ADDR, e.g. :8443). Empty (default) serves plain --addr only, for a TLS-terminating proxy. Without a cert there is no self-signed fallback, so this defaults off to avoid a fatal boot.")
	webDir := flag.String("web", "", "static web client directory; empty uses the embedded FS (production default)")
	origins := flag.String("origins", os.Getenv("ATTERM_ORIGINS"), "comma-separated allowed Origin hosts or URLs (or ATTERM_ORIGINS; empty = allow any only with --dev-insecure)")
	configDir := flag.String("config-dir", envOr("ATTERM_RELAY_CONFIG_DIR", ""), "persistent relay state directory for the SQLite DB (users.db) etc. (or ATTERM_RELAY_CONFIG_DIR)")
	vapidSubject := flag.String("vapid-subject", envOr("ATTERM_VAPID_SUBJECT", "mailto:noreply@atterm.local"), "VAPID subject (mailto: or https: URL; advertised to push services)")
	debugDefault := envEnabled("ATTERM_RELAY_DEBUG")
	debugPayloadDefault := envEnabled("ATTERM_RELAY_DEBUG_PAYLOAD") || envEnabled("ATTERM_RELAY_DEBUG_PAYLOADS")
	rateLimit := flag.Int("rate-limit-per-minute", envInt("ATTERM_RATE_LIMIT_PER_MINUTE", 0), "request/upgrade limit per remote IP/token per minute; 0=default, negative=disable")
	maxConns := flag.Int("max-connections-per-key", envInt("ATTERM_MAX_CONNECTIONS_PER_KEY", 0), "active websocket limit per remote IP/token; 0=default, negative=disable")
	debug := flag.Bool("debug", debugDefault, "enable verbose relay interaction logs (or ATTERM_RELAY_DEBUG=1)")
	debugPayload := flag.Bool("debug-payload", debugPayloadDefault, "include IN/OUT byte contents in debug logs (or ATTERM_RELAY_DEBUG_PAYLOAD=1)")
	devInsecure := flag.Bool("dev-insecure", false, "allow insecure public relay settings (unbootstrapped admin); development/private networks only")
	flag.Parse()

	publicListen := (*addr != "" && isPublicListenAddr(*addr)) ||
		(*httpsAddr != "" && isPublicListenAddr(*httpsAddr))
	bootstrapEmail := strings.TrimSpace(os.Getenv("ATTERM_BOOTSTRAP_ADMIN_EMAIL"))

	if publicListen && !*devInsecure && bootstrapEmail == "" {
		log.Fatal("ATTERM_BOOTSTRAP_ADMIN_EMAIL must be set for a public relay; pass --dev-insecure to skip (development only)")
	}

	// Resolve persistence directory (SQLite stores users.db here).
	persistDir := *configDir
	if persistDir == "" {
		persistDir = "./data/atterm-relay"
	}

	// Create the persistence directory if it doesn't exist yet — first-run
	// bootstrap should "just work" without requiring the operator to
	// pre-create it. Without this SQLite fails with a misleading
	// "out of memory (CANTOPEN)" error.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(persistDir, 0o700); err != nil {
		log.Fatalf("create persist dir %s: %v", persistDir, err)
	}

	// Admin config is DB-backed. relay.json and web-push.json are no longer
	// written or read; all config + web push state live in the DB.

	dbPath := filepath.Join(persistDir, "users.db")
	var (
		store *userstore.DBStore
		err   error
	)
	switch driver := strings.ToLower(strings.TrimSpace(os.Getenv("ATTERM_RELAY_DB_DRIVER"))); driver {
	case "", "sqlite":
		store, err = userstore.Open(ctx, dbPath)
	case "postgres":
		dsn := strings.TrimSpace(os.Getenv("ATTERM_RELAY_DB_DSN"))
		if dsn == "" {
			log.Fatal("ATTERM_RELAY_DB_DRIVER=postgres requires ATTERM_RELAY_DB_DSN")
		}
		store, err = userstore.OpenPostgres(ctx, dsn)
	default:
		log.Fatalf("unknown ATTERM_RELAY_DB_DRIVER %q (want sqlite|postgres)", driver)
	}
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
	realmID, err := relay.LoadOrInitRealm(ctx, store, os.Getenv("ATTERM_RELAY_REALM_ID"))
	if err != nil {
		log.Fatalf("init realm: %v", err)
	}
	instancePublicURL := strings.TrimSpace(os.Getenv("ATTERM_RELAY_INSTANCE_PUBLIC_URL"))

	if err := bootstrapAdmin(ctx, store, bootstrapEmail); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}

	// Admin config — DB-backed. Load from DB (Version==0 means unconfigured).
	adminStore := relay.NewAdminConfigStore(store, relay.AdminConfig{})
	adminCfg, err := adminStore.LoadFromDB(ctx)
	if err != nil {
		log.Fatalf("load relay config: %v", err)
	}

	// One-time env → DB seeding. Keeps existing env-configured deployments
	// working while the admin UI becomes the source of truth. Env/flags win
	// at boot: read current DB snapshot, overlay any provided env values,
	// then persist via Set (which bumps the version).
	seeded := false
	if v := os.Getenv("ATTERM_FEISHU_ENCRYPT_KEY"); v != "" && adminCfg.FeishuEncryptKey == "" {
		adminCfg.FeishuEncryptKey = v
		adminCfg.FeishuEnabled = true
		seeded = true
	}
	if v := os.Getenv("ATTERM_FEISHU_BASE_URL"); v != "" && adminCfg.FeishuBaseURL == "" {
		adminCfg.FeishuBaseURL = v
		seeded = true
	}
	if envOrigins := splitCSV(*origins); len(envOrigins) > 0 && len(adminCfg.AllowedOrigins) == 0 {
		adminCfg.AllowedOrigins = envOrigins
		seeded = true
	}
	if adminCfg.VAPIDSubject == "" {
		adminCfg.VAPIDSubject = *vapidSubject
		seeded = true
	}
	// Debug flags/env seed the config only when turned on (a bool can't tell
	// "unset" from "false"); the admin UI is then the source of truth.
	if *debug && !adminCfg.Debug {
		adminCfg.Debug = true
		seeded = true
	}
	if *debugPayload && !adminCfg.DebugPayload {
		adminCfg.DebugPayload = true
		seeded = true
	}
	if seeded {
		if err := adminStore.Set(ctx, adminCfg); err != nil {
			log.Printf("WARN: persist seeded relay config: %v", err)
		}
		adminCfg = adminStore.Snapshot()
	}

	// Effective origins come from config (seeded from --origins/env above).
	// A public relay still requires at least one user-provided origin unless
	// --dev-insecure; desktop webview hosts are appended for runtime matching.
	if publicListen && len(adminCfg.AllowedOrigins) == 0 && !*devInsecure {
		log.Fatal("refusing public relay without origins; set --origins/ATTERM_ORIGINS or configure them in the admin UI, or pass --dev-insecure for development")
	}
	allowedOrigins := allowedOriginHosts(strings.Join(adminCfg.AllowedOrigins, ","))

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
		Debug:                adminCfg.Debug || adminCfg.DebugPayload,
		DebugPayload:         adminCfg.DebugPayload,
		RateLimitPerMinute:   rateVal,
		MaxConnectionsPerKey: maxVal,
		AdminConfigStore:     adminStore,
		Resolver:             resolver,
		Store:                store,
		OpaqueServer:         opaqueSrv,
		BootstrapAdminEmail:  bootstrapEmail,
		RealmID:              realmID,
		InstancePublicURL:    instancePublicURL,
	}

	// VAPID subject is consumed once here; changing it later needs a restart.
	effectiveVapid := adminCfg.VAPIDSubject
	if effectiveVapid == "" {
		effectiveVapid = *vapidSubject
	}
	wpSvc, wpErr := webpush.Open(store, effectiveVapid)
	if wpErr != nil {
		log.Printf("WARN: web-push disabled: %v", wpErr)
		wpSvc = nil
	}
	cfg.WebPush = wpSvc

	if *devInsecure {
		log.Printf("WARNING: INSECURE relay mode enabled; tokens, terminal input, and output may be exposed")
	}

	srv := relay.NewServer(cfg)

	// Start the cross-instance config refresher. Polls the DB every 10 s and
	// hot-applies any changes made by another relay instance.
	srv.StartConfigRefresher(ctx, 10*time.Second)

	// Attach Feishu at boot if enabled with a usable key. Never fatal — a
	// missing or bad key just leaves Feishu off; an admin can fix it in the
	// UI without restarting.
	if adminCfg.FeishuEnabled {
		if key, err := adminCfg.DecodeFeishuKey(); err != nil {
			log.Printf("WARN: feishu disabled: %v", err)
		} else if err := srv.ApplyFeishuConfig(true, key, adminCfg.FeishuBaseURL); err != nil {
			log.Printf("WARN: feishu enable failed: %v", err)
		} else {
			base := adminCfg.FeishuBaseURL
			if base == "" {
				base = "https://open.feishu.cn"
			}
			log.Printf("feishu: app-mode integration enabled (base=%s)", base)
		}
	}

	// Schedule daily sweep of expired feishu pending bind tokens.
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if n, err := store.SweepExpiredFeishuPendingBinds(ctx); err != nil {
					log.Printf("feishu: sweep expired pending binds: %v", err)
				} else if n > 0 {
					log.Printf("feishu: swept %d expired pending bind(s)", n)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if instancePublicURL != "" {
		// Immediate first heartbeat so the node is selectable without a 30s wait.
		if err := store.UpsertInstanceHeartbeat(ctx, instancePublicURL, instancePublicURL, time.Now().Unix()); err != nil {
			log.Printf("relay: initial instance heartbeat: %v", err)
		}
		go func() {
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					if err := store.UpsertInstanceHeartbeat(ctx, instancePublicURL, instancePublicURL, time.Now().Unix()); err != nil {
						log.Printf("relay: instance heartbeat: %v", err)
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Build the TLS config for the HTTPS listener. OPAQUE needs a secure
	// browser context (WebCrypto), so browser-facing traffic must be HTTPS —
	// either via a TLS-terminating proxy (Cloudflare/Caddy/nginx) in front of
	// the plain --addr listener, or by giving this listener a real cert. There
	// is no self-signed fallback: ATTERM_TLS_CERT + ATTERM_TLS_KEY are required
	// whenever --https-addr is set.
	var tlsConfig *tls.Config
	if *httpsAddr != "" {
		tlsConfig, err = buildTLSConfig(strings.TrimSpace(os.Getenv("ATTERM_TLS_CERT")), strings.TrimSpace(os.Getenv("ATTERM_TLS_KEY")))
		if err != nil {
			log.Fatal(err)
		}
	}

	if *addr == "" && *httpsAddr == "" {
		log.Fatal("no listener: set --addr and/or --https-addr")
	}

	var servers []*http.Server
	if *addr != "" {
		httpSrv := &http.Server{Addr: *addr, Handler: srv, ReadHeaderTimeout: 10 * time.Second}
		servers = append(servers, httpSrv)
		go func() {
			log.Printf("atterm-relay listening (http) on %s", *addr)
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("listen http: %v", err)
			}
		}()
	}
	if *httpsAddr != "" {
		httpsSrv := &http.Server{Addr: *httpsAddr, Handler: srv, TLSConfig: tlsConfig, ReadHeaderTimeout: 10 * time.Second}
		servers = append(servers, httpsSrv)
		go func() {
			log.Printf("atterm-relay listening (https) on %s", *httpsAddr)
			if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Fatalf("listen https: %v", err)
			}
		}()
	}

	<-ctx.Done()
	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdownCtx)
	}
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

// buildTLSConfig loads the operator-supplied certificate for the HTTPS
// listener. There is no self-signed fallback: front the relay with a
// TLS-terminating proxy (Cloudflare/Caddy/nginx) on the plain --addr port, or
// pass ATTERM_TLS_CERT + ATTERM_TLS_KEY to serve HTTPS directly.
func buildTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, errors.New("HTTPS listener requires ATTERM_TLS_CERT and ATTERM_TLS_KEY " +
			"(front with a TLS-terminating proxy, or disable with --https-addr \"\")")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS cert/key: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
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
