# Relay health check page — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a relay-internal diagnostics page so operators can confirm version, HTTPS, origins, bootstrap-admin, rate limits, and active uplinks at a glance — plus a copyable plaintext blob for issues — without depending on the Vue frontend build.

**Architecture:** Three HTTP endpoints inside `internal/relay/`: `/healthz` (public minimal JSON), `/admin/api/health` (admin JSON), `/admin/health` (admin HTML, server-rendered via `html/template`). A single `collectHealth(s *Server) HealthPayload` function feeds both the JSON and the template. Active uplink count is an atomic `int64` on the `Server` struct, incremented inside `handleUplinkHTTP` right after `websocket.Accept` succeeds.

**Tech Stack:** Go 1.22+ stdlib (`html/template`, `embed`, `sync/atomic`); existing `IdentityResolver` for admin auth; reuse `publicBaseURL`'s HTTPS-detection idiom.

**Reference spec:** `docs/superpowers/specs/2026-05-31-relay-health-page-design.md`

---

## File map

- **Create:** `internal/relay/health_http.go` — `HealthPayload`, `collectHealth`, `isMobileOriginCompatible`, the three handlers, embedded template `*template.Template`.
- **Create:** `internal/relay/templates/health.gohtml` — the HTML page (inline CSS + JS).
- **Create:** `internal/relay/health_http_test.go` — endpoint contract tests + uplink-count test + helper unit tests.
- **Modify:** `internal/relay/server.go` — add `startTime time.Time` and `uplinkCount int64` (atomic) to `Server`, add `(s *Server) UplinkCount() int64`, increment/decrement in `handleUplinkHTTP`, register the three new mux routes.

No new dependencies. No migration. No frontend changes.

---

## Task 1: Add `startTime` and atomic `uplinkCount` to `Server`

**Files:**
- Modify: `internal/relay/server.go`

- [ ] **Step 1: Add the two fields to the `Server` struct**

In `internal/relay/server.go`, replace the existing `Server` struct (around line 83):

```go
// Server bundles the registry and HTTP handlers.
type Server struct {
	cfg         Config
	registry    *session.Registry
	mux         *http.ServeMux
	rate        *fixedWindowLimiter
	conns       *connectionLimiter
	startTime   time.Time
	uplinkCount int64 // atomic; read via UplinkCount()
}
```

- [ ] **Step 2: Initialise `startTime` in `NewServer`**

In `NewServer` (around line 130), find the `s := &Server{...}` literal and add `startTime: time.Now(),` alongside `cfg:`, `registry:`, `mux:`, `rate:`, `conns:`. The struct literal becomes:

```go
s := &Server{
    cfg:       cfg,
    registry:  session.NewRegistry(),
    mux:       http.NewServeMux(),
    rate:      newFixedWindowLimiter(rateLimit, time.Minute),
    conns:     newConnectionLimiter(connLimit),
    startTime: time.Now(),
}
```

- [ ] **Step 3: Add accessors**

Append to `internal/relay/server.go` (anywhere after `NewServer` — place it just before `func (s *Server) handleAgentHTTP` for readability, around line 312):

```go
// UplinkCount returns the current number of in-progress uplink WebSocket
// connections. Read via atomic load — safe to call from any goroutine.
func (s *Server) UplinkCount() int64 {
	return atomic.LoadInt64(&s.uplinkCount)
}

// StartTime returns the moment NewServer was called. Used by /healthz to
// expose uptime without exposing a mutable clock to consumers.
func (s *Server) StartTime() time.Time {
	return s.startTime
}
```

If `sync/atomic` isn't already imported, add it to the import block at the top of the file (the file already imports `"time"`).

- [ ] **Step 4: Verify the change compiles**

Run: `cd /Users/attson/code/github.com.attson/atterm && go build ./internal/relay/...`
Expected: clean (no output).

- [ ] **Step 5: Run the existing relay suite as a regression gate**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/relay/server.go
git -c commit.gpgsign=false commit -m "relay: Server gains startTime + atomic uplinkCount fields"
```

---

## Task 2: Increment / decrement `uplinkCount` in `handleUplinkHTTP`

**Files:**
- Modify: `internal/relay/server.go`
- Test: deferred to Task 5 (uplink count is exposed through the `/healthz`-style probe; the test there drives a fake uplink connection)

- [ ] **Step 1: Find `handleUplinkHTTP`**

Open `internal/relay/server.go` and locate `handleUplinkHTTP` (around line 315). The relevant block is after `websocket.Accept` succeeds (around line 341-348):

```go
c, err := websocket.Accept(w, r, s.acceptOptions())
if err != nil {
    s.debugf("ws reject path=/uplink remote=%s origin=%q error=%q", r.RemoteAddr, r.Header.Get("Origin"), err)
    return
}
s.debugf("ws accept path=/uplink remote=%s origin=%q", r.RemoteAddr, r.Header.Get("Origin"))
defer c.Close(websocket.StatusInternalError, "")
s.handleUplink(r.Context(), c, ownerUserID)
```

- [ ] **Step 2: Insert the atomic counter dance**

Add two lines immediately after the `s.debugf("ws accept ...")` log:

```go
c, err := websocket.Accept(w, r, s.acceptOptions())
if err != nil {
    s.debugf("ws reject path=/uplink remote=%s origin=%q error=%q", r.RemoteAddr, r.Header.Get("Origin"), err)
    return
}
s.debugf("ws accept path=/uplink remote=%s origin=%q", r.RemoteAddr, r.Header.Get("Origin"))
atomic.AddInt64(&s.uplinkCount, 1)
defer atomic.AddInt64(&s.uplinkCount, -1)
defer c.Close(websocket.StatusInternalError, "")
s.handleUplink(r.Context(), c, ownerUserID)
```

Order matters: the `atomic.AddInt64(-1)` defer is registered BEFORE the `c.Close` defer, so it runs AFTER `c.Close` returns. The count therefore reflects "WebSocket alive" exactly.

- [ ] **Step 3: Verify the change compiles**

Run: `cd /Users/attson/code/github.com.attson/atterm && go build ./internal/relay/...`
Expected: clean.

- [ ] **Step 4: Run the relay suite again**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/relay/server.go
git -c commit.gpgsign=false commit -m "relay/uplink: count active connections via atomic int64"
```

---

## Task 3: Add `isMobileOriginCompatible` helper (test-first)

**Files:**
- Create: `internal/relay/health_http.go`
- Create: `internal/relay/health_http_test.go`

- [ ] **Step 1: Create the test file with the failing test**

Create `internal/relay/health_http_test.go`:

```go
package relay

import "testing"

func TestIsMobileOriginCompatible(t *testing.T) {
	cases := []struct {
		name    string
		origins []string
		want    bool
	}{
		{"empty (wildcard)", nil, true},
		{"capacitor", []string{"capacitor://localhost"}, true},
		{"capacitor wildcard", []string{"capacitor://*"}, true},
		{"ionic", []string{"ionic://localhost"}, true},
		{"https-localhost", []string{"https://localhost"}, true},
		{"https-localhost-port", []string{"https://localhost:1234"}, true},
		{"null", []string{"null"}, true},
		{"wails only", []string{"wails.localhost"}, false},
		{"mixed has capacitor", []string{"wails.localhost", "capacitor://localhost"}, true},
		{"mixed no capacitor", []string{"wails.localhost", "https://example.com"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isMobileOriginCompatible(tc.origins)
			if got != tc.want {
				t.Fatalf("got %v want %v for origins=%v", got, tc.want, tc.origins)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestIsMobileOriginCompatible -v`
Expected: FAIL with "undefined: isMobileOriginCompatible".

- [ ] **Step 3: Implement the helper**

Create `internal/relay/health_http.go`:

```go
// Package relay — health endpoints. /healthz is public minimal; the
// /admin/health* endpoints are admin-only and emit a redaction-safe
// snapshot of operator-relevant runtime state.
package relay

import "strings"

// isMobileOriginCompatible reports whether the configured origin allow-list
// admits a mobile webview (Capacitor / Ionic / iOS WKWebView) origin. An
// empty list means "any origin is allowed" — technically compatible but
// also a security warning the caller surfaces separately.
func isMobileOriginCompatible(origins []string) bool {
	if len(origins) == 0 {
		return true
	}
	for _, o := range origins {
		switch {
		case strings.HasPrefix(o, "capacitor://"):
			return true
		case strings.HasPrefix(o, "ionic://"):
			return true
		case strings.HasPrefix(o, "https://localhost"):
			return true
		case o == "null":
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestIsMobileOriginCompatible -v`
Expected: PASS (10 sub-cases).

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/relay/health_http.go internal/relay/health_http_test.go
git -c commit.gpgsign=false commit -m "relay/health: isMobileOriginCompatible helper"
```

---

## Task 4: Add `HealthPayload` type + `collectHealth` (test-first)

**Files:**
- Modify: `internal/relay/health_http.go`
- Modify: `internal/relay/health_http_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/health_http_test.go`:

```go
import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCollectHealth_PopulatesAllFields(t *testing.T) {
	store := newTestStoreForRelay(t)
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Version:              "v0.3.99",
		AllowedOrigins:       []string{"wails.localhost", "capacitor://localhost"},
		RateLimitPerMinute:   600,
		MaxConnectionsPerKey: 64,
		Resolver:             resolver,
		Store:                store,
	})

	// Drive a fake request to give collectHealth an *http.Request for HTTPS detection.
	r := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	r.Header.Set("X-Forwarded-Proto", "https")

	payload := collectHealth(context.Background(), srv, r)

	if payload.Version != "v0.3.99" {
		t.Errorf("Version: got %q", payload.Version)
	}
	if payload.UptimeSeconds < 0 {
		t.Errorf("UptimeSeconds negative: %d", payload.UptimeSeconds)
	}
	if !payload.HTTPS {
		t.Errorf("HTTPS: got false, want true (X-Forwarded-Proto)")
	}
	if len(payload.ConfiguredOrigins) != 2 {
		t.Errorf("ConfiguredOrigins: got %v", payload.ConfiguredOrigins)
	}
	if payload.OriginsOpen {
		t.Errorf("OriginsOpen: got true, want false")
	}
	if payload.RateLimitPerMinute != 600 {
		t.Errorf("RateLimitPerMinute: got %d", payload.RateLimitPerMinute)
	}
	if payload.MaxConnectionsPerKey != 64 {
		t.Errorf("MaxConnectionsPerKey: got %d", payload.MaxConnectionsPerKey)
	}
	if payload.ActiveUplinks != 0 {
		t.Errorf("ActiveUplinks: got %d", payload.ActiveUplinks)
	}
	if !payload.MobileOriginCompatible {
		t.Errorf("MobileOriginCompatible: got false")
	}
	if !strings.HasSuffix(payload.GeneratedAt, "Z") {
		t.Errorf("GeneratedAt: got %q, want RFC3339 UTC ending in Z", payload.GeneratedAt)
	}
	// BootstrapAdminConfigured depends on Store contents; on an empty store
	// it must be false.
	if payload.BootstrapAdminConfigured {
		t.Errorf("BootstrapAdminConfigured: got true on empty store")
	}
}

// newTestStoreForRelay opens an in-memory userstore for relay tests.
// (helper colocated here because health tests are the first relay tests
// that need an in-process store outside the AuthServer test harness.)
func newTestStoreForRelay(t *testing.T) *userstore.SQLiteStore {
	t.Helper()
	store, err := userstore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("userstore.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
```

Then add the matching imports to the existing import block (replace the lone `import "testing"` with the full block above). And add the `userstore` import path at the top:

```go
import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)
```

If `newTestAuthServer` in `auth_http_test.go` already exposes a usable in-memory store via the helper there, prefer reusing it over `newTestStoreForRelay` and drop the helper. Verify by reading `auth_http_test.go:18` — if the function returns `(*AuthServer, *userstore.SQLiteStore)` and a follow-up call can extract the store, use it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestCollectHealth -v`
Expected: FAIL with "undefined: collectHealth" and/or "undefined: HealthPayload".

- [ ] **Step 3: Implement `HealthPayload` + `collectHealth`**

Replace the contents of `internal/relay/health_http.go` with:

```go
// Package relay — health endpoints. /healthz is public minimal; the
// /admin/health* endpoints are admin-only and emit a redaction-safe
// snapshot of operator-relevant runtime state.
package relay

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// HealthPayload is the contract returned by /admin/api/health and rendered
// into /admin/health. Every field is either an operator-configured value
// or an aggregate count — no PII, no secrets, no file paths.
type HealthPayload struct {
	Version                  string   `json:"version"`
	UptimeSeconds            int64    `json:"uptime_seconds"`
	HTTPS                    bool     `json:"https"`
	ConfiguredOrigins        []string `json:"configured_origins"`
	OriginsOpen              bool     `json:"origins_open"`
	BootstrapAdminConfigured bool     `json:"bootstrap_admin_configured"`
	RateLimitPerMinute       int      `json:"rate_limit_per_minute"`
	MaxConnectionsPerKey     int      `json:"max_connections_per_key"`
	ActiveUplinks            int64    `json:"active_uplinks"`
	MobileOriginCompatible   bool     `json:"mobile_origin_compatible"`
	GeneratedAt              string   `json:"generated_at"`
	Warnings                 []string `json:"health_check_warnings,omitempty"`
}

// isMobileOriginCompatible reports whether the configured origin allow-list
// admits a mobile webview (Capacitor / Ionic / iOS WKWebView) origin. An
// empty list means "any origin is allowed" — technically compatible but
// also a security warning the caller surfaces separately.
func isMobileOriginCompatible(origins []string) bool {
	if len(origins) == 0 {
		return true
	}
	for _, o := range origins {
		switch {
		case strings.HasPrefix(o, "capacitor://"):
			return true
		case strings.HasPrefix(o, "ionic://"):
			return true
		case strings.HasPrefix(o, "https://localhost"):
			return true
		case o == "null":
			return true
		}
	}
	return false
}

// httpsFromRequest mirrors publicBaseURL's TLS detection: true if the
// request arrived over TLS directly OR a forwarding proxy declared HTTPS
// via X-Forwarded-Proto.
func httpsFromRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// collectHealth gathers the runtime state of s into a HealthPayload. r
// is used only for HTTPS detection (per-request, since the same binary
// can be reached over both schemes via different listeners). On Store
// failures, BootstrapAdminConfigured falls back to false and a warning
// is appended to Warnings.
func collectHealth(ctx context.Context, s *Server, r *http.Request) HealthPayload {
	cfg := s.cfg

	rateLimit := cfg.RateLimitPerMinute
	if rateLimit == 0 {
		rateLimit = defaultRateLimitPerMinute
	}
	connLimit := cfg.MaxConnectionsPerKey
	if connLimit == 0 {
		connLimit = defaultMaxConnections
	}

	originsCopy := append([]string(nil), cfg.AllowedOrigins...)

	payload := HealthPayload{
		Version:                cfg.Version,
		UptimeSeconds:          int64(time.Since(s.startTime).Seconds()),
		HTTPS:                  httpsFromRequest(r),
		ConfiguredOrigins:      originsCopy,
		OriginsOpen:            len(originsCopy) == 0,
		RateLimitPerMinute:     rateLimit,
		MaxConnectionsPerKey:   connLimit,
		ActiveUplinks:          s.UplinkCount(),
		MobileOriginCompatible: isMobileOriginCompatible(originsCopy),
		GeneratedAt:            time.Now().UTC().Format(time.RFC3339),
	}

	if cfg.Store != nil {
		users, err := cfg.Store.ListUsers(ctx)
		if err != nil {
			payload.Warnings = append(payload.Warnings, "bootstrap_admin_lookup_failed")
		} else {
			for _, u := range users {
				if u.IsAdmin {
					payload.BootstrapAdminConfigured = true
					break
				}
			}
		}
	}

	return payload
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestCollectHealth -v`
Expected: PASS.

Also run the whole relay suite as a regression check:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/relay/health_http.go internal/relay/health_http_test.go
git -c commit.gpgsign=false commit -m "relay/health: HealthPayload + collectHealth gatherer"
```

---

## Task 5: Add `GET /healthz` public endpoint (test-first)

**Files:**
- Modify: `internal/relay/health_http.go` (add handler)
- Modify: `internal/relay/server.go` (register route)
- Modify: `internal/relay/health_http_test.go` (add tests)

- [ ] **Step 1: Write failing tests**

Append to `internal/relay/health_http_test.go`:

```go
import "encoding/json"  // add to import block if not already present

func TestHealthz_Public_ReturnsOKAndVersion(t *testing.T) {
	store := newTestStoreForRelay(t)
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Version:  "v0.3.99",
		Resolver: resolver,
		Store:    store,
	})

	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control: got %q want %q", got, "no-store")
	}

	var body struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !body.OK || body.Version != "v0.3.99" {
		t.Fatalf("body: %+v", body)
	}
}

func TestHealthz_Public_NoAuthRequired(t *testing.T) {
	// Even when Resolver is configured, /healthz must not require auth.
	store := newTestStoreForRelay(t)
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{Resolver: resolver, Store: store})

	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	// no Authorization header, no cookies
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestHealthz -v`
Expected: FAIL — `/healthz` returns 404 because the route isn't registered.

- [ ] **Step 3: Add the handler to `health_http.go`**

Append to `internal/relay/health_http.go`:

```go
import (
	"encoding/json"
	"net/http"
)

// handleHealthz returns a minimal liveness response for load balancers
// and probes. Public, no auth, no cache.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"version": s.cfg.Version,
	})
}
```

(If `health_http.go` already imports `encoding/json` or `net/http` from earlier tasks, fold the new imports into the existing block rather than duplicating it. Go won't compile two import blocks at the top of a file.)

- [ ] **Step 4: Register the route in `server.go`**

In `internal/relay/server.go`, after the existing `s.mux.HandleFunc("/api/version", s.handleVersionHTTP)` line (around line 142), add:

```go
	s.mux.HandleFunc("/healthz", s.handleHealthz)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestHealthz -v`
Expected: PASS — both tests.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/relay/health_http.go internal/relay/health_http_test.go internal/relay/server.go
git -c commit.gpgsign=false commit -m "relay: GET /healthz public liveness endpoint"
```

---

## Task 6: Add `GET /admin/api/health` JSON endpoint (test-first)

**Files:**
- Modify: `internal/relay/health_http.go`
- Modify: `internal/relay/server.go`
- Modify: `internal/relay/health_http_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/relay/health_http_test.go`:

```go
func TestAdminHealthAPI_Unauthenticated_401(t *testing.T) {
	store := newTestStoreForRelay(t)
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{Resolver: resolver, Store: store})

	r := httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAdminHealthAPI_RegularUser_401(t *testing.T) {
	store := newTestStoreForRelay(t)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	secret, _, err := store.CreateAPIToken(ctx, u.ID, "test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{Resolver: resolver, Store: store})

	r := httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	r.Header.Set("Authorization", "Bearer "+secret.Expose())
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (not admin), got %d", w.Code)
	}
}

func TestAdminHealthAPI_Admin_200_AllFieldsPresent(t *testing.T) {
	store := newTestStoreForRelay(t)
	ctx := context.Background()
	if _, err := store.EnsureAdminUser(ctx, "admin@example.com", "correcthorsebatterystaple"); err != nil {
		t.Fatalf("EnsureAdminUser: %v", err)
	}
	u, err := store.GetUserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	secret, _, err := store.CreateAPIToken(ctx, u.ID, "test-admin")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Version:        "v0.3.99",
		AllowedOrigins: []string{"capacitor://localhost"},
		Resolver:       resolver,
		Store:          store,
	})

	r := httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	r.Header.Set("Authorization", "Bearer "+secret.Expose())
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got HealthPayload
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Version != "v0.3.99" {
		t.Errorf("Version: got %q", got.Version)
	}
	if !got.BootstrapAdminConfigured {
		t.Errorf("BootstrapAdminConfigured: got false")
	}
	if !got.MobileOriginCompatible {
		t.Errorf("MobileOriginCompatible: got false")
	}
	if got.GeneratedAt == "" {
		t.Errorf("GeneratedAt empty")
	}
}
```

(If `EnsureAdminUser` / `GetUserByEmail` have different signatures in this repo, run `grep -n "func.*Store.*EnsureAdmin\|func.*GetUserByEmail" internal/userstore/*.go` to find the actual names and adjust. The userstore-level pattern is consistent across all relay tests in `auth_http_test.go`.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestAdminHealthAPI -v`
Expected: FAIL — all three tests get 404 because the route isn't registered.

- [ ] **Step 3: Add the handler**

Append to `internal/relay/health_http.go`:

```go
// requireAdminAccess is a thin wrapper that gates inner on PrincipalAdmin
// via the same Resolver-based auth as AdminServer. Returns 401 on failure.
func (s *Server) requireAdminAccess(inner http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Resolver == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		p := s.cfg.Resolver.Resolve(r)
		if p.Kind != PrincipalAdmin {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		inner(w, r)
	}
}

// handleAdminHealthAPI returns the JSON HealthPayload. Admin-gated.
func (s *Server) handleAdminHealthAPI(w http.ResponseWriter, r *http.Request) {
	payload := collectHealth(r.Context(), s, r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(payload)
}
```

- [ ] **Step 4: Register the route**

In `internal/relay/server.go`, immediately after the `s.mux.HandleFunc("/healthz", ...)` line you added in Task 5, add:

```go
	s.mux.HandleFunc("/admin/api/health", s.requireAdminAccess(s.handleAdminHealthAPI))
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestAdminHealthAPI -v`
Expected: PASS — three tests.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/relay/health_http.go internal/relay/health_http_test.go internal/relay/server.go
git -c commit.gpgsign=false commit -m "relay: GET /admin/api/health JSON endpoint"
```

---

## Task 7: Add `GET /admin/health` HTML endpoint (test-first)

**Files:**
- Create: `internal/relay/templates/health.gohtml`
- Modify: `internal/relay/health_http.go`
- Modify: `internal/relay/server.go`
- Modify: `internal/relay/health_http_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/relay/health_http_test.go`:

```go
func TestAdminHealth_HTML_Admin_200_AllLabelsPresent(t *testing.T) {
	store := newTestStoreForRelay(t)
	ctx := context.Background()
	if _, err := store.EnsureAdminUser(ctx, "admin@example.com", "correcthorsebatterystaple"); err != nil {
		t.Fatalf("EnsureAdminUser: %v", err)
	}
	u, _ := store.GetUserByEmail(ctx, "admin@example.com")
	secret, _, _ := store.CreateAPIToken(ctx, u.ID, "test-admin")

	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Version:              "v0.3.99",
		AllowedOrigins:       []string{"capacitor://localhost"},
		RateLimitPerMinute:   600,
		MaxConnectionsPerKey: 64,
		Resolver:             resolver,
		Store:                store,
	})

	r := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	r.Header.Set("Authorization", "Bearer "+secret.Expose())
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type: got %q", got)
	}
	body := w.Body.String()
	for _, label := range []string{
		"Version",
		"Uptime",
		"HTTPS",
		"Configured origins",
		"Bootstrap admin",
		"Rate limit",
		"Max conn",
		"Active uplinks",
		"Mobile compat",
		"Copy diagnostics",
	} {
		if !strings.Contains(body, label) {
			t.Errorf("HTML missing label %q", label)
		}
	}
	if !strings.Contains(body, "v0.3.99") {
		t.Errorf("HTML missing version value")
	}
	// __HEALTH__ JSON should be injected so the copy button has data.
	if !strings.Contains(body, "window.__HEALTH__") {
		t.Errorf("HTML missing window.__HEALTH__ injection")
	}
}

func TestAdminHealth_HTML_Unauthenticated_401(t *testing.T) {
	store := newTestStoreForRelay(t)
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{Resolver: resolver, Store: store})

	r := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestAdminHealth_HTML -v`
Expected: FAIL — `/admin/health` 404 (no route).

- [ ] **Step 3: Create the template**

Create `internal/relay/templates/health.gohtml`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>atterm-relay — health</title>
  <style>
    body { font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; max-width: 720px; margin: 2rem auto; padding: 0 1rem; color: #1c1f24; }
    h1 { font-size: 1.2rem; margin: 0 0 1rem; }
    dl.row { display: grid; grid-template-columns: 11rem 1fr; gap: 4px 12px; margin: 0 0 6px; padding: 8px 12px; border-radius: 8px; background: #f5f6f8; }
    dl.row[data-state="warn"] { background: #fff7e0; border-left: 3px solid #d59300; }
    dt { color: #5c6470; font-weight: 500; }
    dd { margin: 0; font-family: ui-monospace, Menlo, monospace; word-break: break-all; }
    .actions { display: flex; align-items: center; gap: 12px; margin-top: 1rem; }
    button { padding: 8px 14px; border: 1px solid #2563eb; background: #2563eb; color: #fff; border-radius: 6px; cursor: pointer; font: inherit; }
    button.copied { background: #15803d; border-color: #15803d; }
    .gen { color: #5c6470; font-size: 12px; }
    textarea.fallback { position: absolute; left: -9999px; }
  </style>
</head>
<body>
  <h1>atterm-relay — health</h1>

  <dl class="row"><dt>Version</dt><dd>{{ .Version }}</dd></dl>
  <dl class="row"><dt>Uptime</dt><dd>{{ .UptimeSeconds }}s</dd></dl>
  <dl class="row"><dt>HTTPS</dt><dd>{{ if .HTTPS }}yes{{ else }}no{{ end }}</dd></dl>
  <dl class="row{{ if .OriginsOpen }} warn{{ end }}"{{ if .OriginsOpen }} data-state="warn"{{ end }}>
    <dt>Configured origins</dt>
    <dd>{{ if .OriginsOpen }}(open — any origin allowed){{ else }}{{ range $i, $o := .ConfiguredOrigins }}{{ if $i }}, {{ end }}{{ $o }}{{ end }}{{ end }}</dd>
  </dl>
  <dl class="row"><dt>Bootstrap admin</dt><dd>{{ if .BootstrapAdminConfigured }}configured{{ else }}not configured{{ end }}</dd></dl>
  <dl class="row"><dt>Rate limit / min</dt><dd>{{ .RateLimitPerMinute }}</dd></dl>
  <dl class="row"><dt>Max conn / key</dt><dd>{{ .MaxConnectionsPerKey }}</dd></dl>
  <dl class="row"><dt>Active uplinks</dt><dd>{{ .ActiveUplinks }}</dd></dl>
  <dl class="row{{ if not .MobileOriginCompatible }} warn{{ end }}"{{ if not .MobileOriginCompatible }} data-state="warn"{{ end }}>
    <dt>Mobile compat</dt><dd>{{ if .MobileOriginCompatible }}yes{{ else }}no (add a capacitor:// origin){{ end }}</dd>
  </dl>

  <div class="actions">
    <button id="copy">Copy diagnostics</button>
    <span class="gen">generated {{ .GeneratedAt }}</span>
  </div>
  <textarea class="fallback" id="fallback"></textarea>

  <script>
    window.__HEALTH__ = {{ .JSON }};
    (function () {
      var btn = document.getElementById('copy');
      var fallback = document.getElementById('fallback');
      function format(h) {
        var pad = function (k) { while (k.length < 23) k += ' '; return k; };
        var lines = [
          'atterm-relay diagnostics — ' + h.generated_at,
          '-----------------------------------------------',
          pad('Version:') + h.version,
          pad('Uptime:') + h.uptime_seconds + 's',
          pad('HTTPS:') + (h.https ? 'yes' : 'no'),
          pad('Origins:') + (h.origins_open ? '(open)' : (h.configured_origins || []).join(', ')),
          pad('Origins open:') + (h.origins_open ? 'yes' : 'no'),
          pad('Bootstrap admin:') + (h.bootstrap_admin_configured ? 'configured' : 'not configured'),
          pad('Rate limit / min:') + h.rate_limit_per_minute,
          pad('Max conn / key:') + h.max_connections_per_key,
          pad('Active uplinks:') + h.active_uplinks,
          pad('Mobile compat:') + (h.mobile_origin_compatible ? 'yes' : 'no')
        ];
        return lines.join('\n');
      }
      btn.addEventListener('click', function () {
        var text = format(window.__HEALTH__);
        function done() {
          btn.textContent = 'Copied';
          btn.classList.add('copied');
          setTimeout(function () { btn.textContent = 'Copy diagnostics'; btn.classList.remove('copied'); }, 1500);
        }
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(text).then(done, function () {
            fallback.value = text; fallback.select(); document.execCommand('copy'); done();
          });
        } else {
          fallback.value = text; fallback.select(); document.execCommand('copy'); done();
        }
      });
    })();
  </script>
</body>
</html>
```

- [ ] **Step 4: Add template embed + handler to `health_http.go`**

Append to `internal/relay/health_http.go`:

```go
import (
	"bytes"
	_ "embed"
	"html/template"
)

//go:embed templates/health.gohtml
var healthTemplateSource string

var healthTemplate = template.Must(template.New("health").Parse(healthTemplateSource))

// handleAdminHealth renders the operator HTML diagnostics page.
// Admin-gated. Injects HealthPayload both as template data (for static
// rendering) and as JSON in window.__HEALTH__ (for the Copy button).
func (s *Server) handleAdminHealth(w http.ResponseWriter, r *http.Request) {
	payload := collectHealth(r.Context(), s, r)

	// Marshal the same payload as JSON for the embedded copy-diagnostics script.
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := struct {
		HealthPayload
		JSON template.JS
	}{
		HealthPayload: payload,
		JSON:          template.JS(jsonBytes),
	}

	var buf bytes.Buffer
	if err := healthTemplate.Execute(&buf, data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}
```

Fold the new imports (`bytes`, `_ "embed"`, `html/template`) into the file's existing single import block — do not add a second block.

- [ ] **Step 5: Register the route**

In `internal/relay/server.go`, immediately after the `/admin/api/health` line you added in Task 6, add:

```go
	s.mux.HandleFunc("/admin/health", s.requireAdminAccess(s.handleAdminHealth))
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestAdminHealth_HTML -v`
Expected: PASS — both tests.

Then run the full relay suite:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/relay/templates/health.gohtml internal/relay/health_http.go internal/relay/health_http_test.go internal/relay/server.go
git -c commit.gpgsign=false commit -m "relay: GET /admin/health HTML page with copy-diagnostics button"
```

---

## Task 8: Contract test for `HealthPayload` JSON field stability

**Files:**
- Modify: `internal/relay/health_http_test.go`

- [ ] **Step 1: Write the contract test**

Append to `internal/relay/health_http_test.go`:

```go
func TestHealthPayload_JSONFieldsStable(t *testing.T) {
	// Pin the JSON shape so a future rename breaks this test loudly.
	// The list below is the SOURCE OF TRUTH for consumers of /healthz
	// and /admin/api/health (the Copy Diagnostics button formats from it).
	want := []string{
		"version",
		"uptime_seconds",
		"https",
		"configured_origins",
		"origins_open",
		"bootstrap_admin_configured",
		"rate_limit_per_minute",
		"max_connections_per_key",
		"active_uplinks",
		"mobile_origin_compatible",
		"generated_at",
	}

	// Marshal a zero value and pull the keys out as a set.
	b, err := json.Marshal(HealthPayload{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing JSON key %q in HealthPayload", k)
		}
	}
}
```

Note: `Warnings` (`health_check_warnings`) has `omitempty`, so it's correctly absent on a zero value — the test doesn't check it, which means accidentally removing `omitempty` from `Warnings` won't trip this test, but renaming any of the load-bearing keys will.

- [ ] **Step 2: Run the test**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestHealthPayload_JSONFieldsStable -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/relay/health_http_test.go
git -c commit.gpgsign=false commit -m "relay/health: contract test pins JSON field names"
```

---

## Task 9: Uplink count integration test (test-first)

**Files:**
- Modify: `internal/relay/health_http_test.go`

- [ ] **Step 1: Write the failing test**

This drives a real uplink WebSocket handshake against the Server and asserts the atomic counter moves. Append to `internal/relay/health_http_test.go`:

```go
import (
	"context"
	"net/http/httptest"
	// existing imports...

	"nhooyr.io/websocket"
)

func TestUplinkCount_IncrementsOnConnectDecrementsOnClose(t *testing.T) {
	store := newTestStoreForRelay(t)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	secret, _, err := store.CreateAPIToken(ctx, u.ID, "test-uplink")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Resolver: resolver,
		Store:    store,
		// AllowedOrigins empty → any origin accepted (test path).
	})

	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	// Sanity: starting count is 0.
	if got := srv.UplinkCount(); got != 0 {
		t.Fatalf("initial UplinkCount: got %d want 0", got)
	}

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/uplink"
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: map[string][]string{
			"Authorization": {"Bearer " + secret.Expose()},
		},
	})
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}

	// Give the handler a moment to atomic.AddInt64(+1) on the server side.
	// Poll up to 1s for UplinkCount == 1, sampling every 10ms.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if srv.UplinkCount() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := srv.UplinkCount(); got != 1 {
		t.Fatalf("after dial: UplinkCount got %d want 1", got)
	}

	// Close the client side; the server defer should drop the count.
	if err := c.Close(websocket.StatusNormalClosure, "test done"); err != nil {
		t.Fatalf("c.Close: %v", err)
	}

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if srv.UplinkCount() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := srv.UplinkCount(); got != 0 {
		t.Fatalf("after close: UplinkCount got %d want 0", got)
	}
}
```

(Fold the new `nhooyr.io/websocket` import into the existing block. The `time` package is already imported by other tests in the file.)

- [ ] **Step 2: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestUplinkCount -v`
Expected: PASS — the atomic-counter wiring from Task 2 should already work end-to-end.

(If this test fails because the handshake errors out for a reason unrelated to counting — e.g., `s.allowAuthenticatedRequest` rejects the connection — investigate `internal/relay/server.go:332` first; this test reuses the same auth path as production uplinks.)

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/relay/health_http_test.go
git -c commit.gpgsign=false commit -m "relay: integration test for uplink active-count atomic counter"
```

---

## Task 10: Final smoke

**Files:**
- (none — verification only)

- [ ] **Step 1: Full backend suite**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./...`
Expected: PASS — every existing package plus the new `internal/relay` tests.

- [ ] **Step 2: Vet**

Run: `cd /Users/attson/code/github.com.attson/atterm && go vet ./...`
Expected: clean.

- [ ] **Step 3: Build the relay binary**

Run: `cd /Users/attson/code/github.com.attson/atterm && go build -o /tmp/atterm-relay-p17 ./cmd/atterm-relay`
Expected: succeeds.

- [ ] **Step 4: Manual smoke (documented, not gating)**

For local verification, after merging:

1. `go run ./cmd/atterm-relay --addr :8080 --dev-insecure`
2. `curl http://localhost:8080/healthz` → `{"ok":true,"version":"dev"}`
3. Visit `http://localhost:8080/admin/health` (after creating an admin user, logging in, and getting a token) → see the HTML page with all fields populated. Click "Copy diagnostics", paste, confirm the format matches §7 of the spec.

No commit needed — verification gate.

---

## Self-review notes

- **Spec coverage:**
  - §3 HealthPayload (full shape) → Task 4
  - §4.1 /healthz → Task 5
  - §4.2 /admin/api/health → Task 6
  - §4.3 /admin/health (HTML) → Task 7
  - §5 isMobileOriginCompatible → Task 3
  - §6 uplink counting → Tasks 1 + 2, integration test in Task 9
  - §7 HTML page layout + copy button → Task 7 (template)
  - §8 Warnings on bootstrap admin lookup → Task 4 (collectHealth handles it; not explicitly tested — collectHealth happy-path test covers the success branch; a Store-error case is left to a follow-up if needed)
  - §9.1 endpoint tests → Tasks 5, 6, 7
  - §9.1 isMobileOriginCompatible table test → Task 3
  - §9.1 uplink counter test → Task 9
  - §9.2 contract test → Task 8

- **Placeholder scan:** no TBDs; each code-emitting step is concrete and complete. The "if EnsureAdminUser has a different signature" hedge in Task 6 is a runtime fallback instruction, not a placeholder.

- **Type consistency:** `HealthPayload` field names and JSON tags appear identically in Task 4 (definition), Task 8 (contract list), Task 7 (template), and Task 6 (test assertions). `collectHealth(ctx, s, r)` has the same signature in Task 4 (definition) and Tasks 6, 7 (callers). `s.UplinkCount()` is defined in Task 1 and consumed by Task 4's `collectHealth` and Task 9's test.

---

Plan complete and saved to `docs/superpowers/plans/2026-05-31-relay-health-page.md`. Execution choice:

**1. Subagent-Driven (recommended)** — fresh subagent per task + two-stage review.

**2. Inline Execution** — batch tasks with checkpoints.
