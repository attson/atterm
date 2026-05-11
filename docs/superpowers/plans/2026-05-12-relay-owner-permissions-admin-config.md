# Relay Owner Permissions And Admin Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add desktop-owner remote permissions with relay/host enforcement and a persistent relay admin config surface that never stores the main write token.

**Architecture:** Add a small permission model shared through `proto.SessionInfo.remote_permission`. Desktop persists and announces the owner permission; relay computes effective permissions as token scope intersected with session permission and enforces before forwarding inbound frames. Relay admin config lives in focused `internal/relay`/`cmd/atterm-relay` helpers, persists operational settings and hashed read-only tokens to JSON, and exposes an admin-token-protected API/page.

**Tech Stack:** Go `net/http`, `encoding/json`, atomic file writes, existing nhooyr WebSocket protocol, Wails bindings, Vue 3 Settings UI, Node `node:test`, Go `testing`.

---

## File Map

- `internal/proto/frame.go`: add optional `SessionInfo.RemotePermission` JSON field and constants.
- `internal/relay/permissions.go`: new permission parsing and allowed-frame helpers.
- `internal/relay/client_conn.go`: enforce effective permission for `IN`/`RESIZE`/`PASTE_IMAGE`.
- `internal/relay/uplink_conn.go`: preserve mirror permission from `ANNOUNCE` metadata.
- `desktop/config.go`, `desktop/app.go`, `desktop/uplink.go`: persist owner default permission and stamp it into ANNOUNCE snapshots.
- `desktop/frontend/src/components/SettingsDialog.vue`, `desktop/frontend/src/lib/api.ts`: expose desktop setting.
- `cmd/atterm-relay/main.go`: wire config path/admin token/persistent read-only token config into `relay.Config`.
- `internal/relay/admin_config.go`: runtime config store, JSON persistence, hashed token support.
- `internal/relay/admin_http.go`: admin API/page routes and auth.
- Tests: `internal/relay/version_test.go`, `desktop/uplink_announce_test.go`, `desktop/uplink_e2e_test.go`, `cmd/atterm-relay/main_test.go`, web tests if admin page assets are added.
- Docs: `AGENTS.md`, `README.md`, `docs/spec/protocol.md`, `docs/spec/architecture.md`, `docs/spec/conventions.md`.

---

### Task 1: Owner Permission Model In Protocol And Relay

**Files:**
- Modify: `internal/proto/frame.go`
- Create: `internal/relay/permissions.go`
- Modify: `internal/relay/client_conn.go`
- Modify: `internal/relay/uplink_conn.go`
- Test: `internal/relay/version_test.go`

- [ ] **Step 1: Write failing relay permission tests**

Add tests that create mirror sessions with `RemotePermission` and assert inbound forwarding behavior:

```go
func TestSessionPermissionViewDropsInputForWriteToken(t *testing.T) {
    srv := NewServer(Config{Token: "rw"})
    id := uuid.MustParse("44444444-4444-4444-8444-444444444444")
    sess := session.New(id, proto.SessionInfo{Command: "bash", RemotePermission: proto.RemotePermissionView})
    srv.registry.Add(sess)

    httpSrv := httptest.NewServer(srv)
    defer httpSrv.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    conn, _, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):] + "/client?token=rw", nil)
    if err != nil { t.Fatal(err) }
    defer conn.Close(websocket.StatusNormalClosure, "")

    attachPayload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String()})
    if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeAttach, SessionID: id, Payload: attachPayload})); err != nil { t.Fatal(err) }
    if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeIn, SessionID: id, Payload: []byte("whoami\n")})); err != nil { t.Fatal(err) }

    select {
    case f := <-sess.Inbound():
        t.Fatalf("view permission delivered inbound frame: %+v", f)
    case <-time.After(100 * time.Millisecond):
    }
}

func TestSessionPermissionControlAllowsInputButDropsPasteImage(t *testing.T) {
    srv := NewServer(Config{Token: "rw"})
    id := uuid.MustParse("55555555-5555-4555-8555-555555555555")
    sess := session.New(id, proto.SessionInfo{Command: "bash", RemotePermission: proto.RemotePermissionControl})
    srv.registry.Add(sess)

    httpSrv := httptest.NewServer(srv)
    defer httpSrv.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    conn, _, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):] + "/client?token=rw", nil)
    if err != nil { t.Fatal(err) }
    defer conn.Close(websocket.StatusNormalClosure, "")

    attachPayload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String()})
    if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeAttach, SessionID: id, Payload: attachPayload})); err != nil { t.Fatal(err) }
    if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeIn, SessionID: id, Payload: []byte("whoami\n")})); err != nil { t.Fatal(err) }
    select {
    case f := <-sess.Inbound():
        if f.Type != proto.TypeIn { t.Fatalf("inbound type=%v; want IN", f.Type) }
    case <-time.After(time.Second):
        t.Fatal("timed out waiting for allowed IN")
    }
    pastePayload, _ := json.Marshal(proto.PasteImagePayload{ContentType: "image/png", Data: []byte("png")})
    if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypePasteImage, SessionID: id, Payload: pastePayload})); err != nil { t.Fatal(err) }
    select {
    case f := <-sess.Inbound():
        t.Fatalf("control permission delivered paste frame: %+v", f)
    case <-time.After(100 * time.Millisecond):
    }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH && go test -tags webkit2_41 ./internal/relay -run 'TestSessionPermission'`

Expected: compile failure for missing `RemotePermission` constants/field or behavioral failure because current relay permits frames.

- [ ] **Step 3: Implement permission helpers and enforcement**

Add protocol constants and optional field:

```go
const (
    RemotePermissionView    = "view"
    RemotePermissionControl = "control"
    RemotePermissionFull    = "full"
)

type SessionInfo struct {
    // existing fields...
    RemotePermission string `json:"remote_permission,omitempty"`
}
```

Create `internal/relay/permissions.go`:

```go
package relay

import (
    "github.com/attson/atterm/internal/proto"
    "github.com/attson/atterm/internal/session"
)

type remotePermission uint8

const (
    permView remotePermission = iota
    permControl
    permFull
)

func parseRemotePermission(value string) remotePermission {
    switch value {
    case proto.RemotePermissionView:
        return permView
    case proto.RemotePermissionControl:
        return permControl
    case proto.RemotePermissionFull, "":
        return permFull
    default:
        return permView
    }
}

func sessionRemotePermission(sess *session.Session) remotePermission {
    if sess == nil {
        return permView
    }
    return parseRemotePermission(sess.Info().RemotePermission)
}

func frameAllowedByPermission(scope authScope, perm remotePermission, typ proto.Type) bool {
    if scope == authRead {
        switch typ {
        case proto.TypeIn, proto.TypeResize, proto.TypePasteImage:
            return false
        default:
            return true
        }
    }
    switch typ {
    case proto.TypeIn, proto.TypeResize:
        return perm >= permControl
    case proto.TypePasteImage:
        return perm >= permFull
    default:
        return true
    }
}
```

Update `handleClient` to accept `scope authScope` instead of bool and call `frameAllowedByPermission(scope, sessionRemotePermission(sess), f.Type)` before resize/update/send.

- [ ] **Step 4: Run relay permission tests and full relay tests**

Run: `go test -tags webkit2_41 ./internal/relay`

Expected: PASS.

- [ ] **Step 5: Commit**

Run: `git add internal/proto/frame.go internal/relay/permissions.go internal/relay/client_conn.go internal/relay/uplink_conn.go internal/relay/version_test.go && git commit -m "enforce owner session permissions"`

---

### Task 2: Desktop Owner Permission Setting And Uplink Host Enforcement

**Files:**
- Modify: `desktop/config.go`
- Modify: `desktop/app.go`
- Modify: `desktop/uplink.go`
- Modify: `desktop/frontend/src/lib/api.ts`
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Test: `desktop/uplink_announce_test.go`
- Test: `desktop/uplink_e2e_test.go`

- [ ] **Step 1: Write failing desktop announce test**

Add a test asserting `buildAnnouncePayload` stamps permission:

```go
func TestBuildAnnouncePayloadStampsRemotePermission(t *testing.T) {
    payload, err := buildAnnouncePayload("host-id", "host", "user", []proto.SessionInfo{{ID: "11111111-1111-4111-8111-111111111111", Command: "bash"}}, proto.RemotePermissionView)
    if err != nil { t.Fatal(err) }
    var ann proto.AnnouncePayload
    if err := json.Unmarshal(payload, &ann); err != nil { t.Fatal(err) }
    if got := ann.Sessions[0].RemotePermission; got != proto.RemotePermissionView {
        t.Fatalf("RemotePermission=%q; want view", got)
    }
}
```

- [ ] **Step 2: Run test and verify RED**

Run: `go test -tags webkit2_41 ./desktop -run TestBuildAnnouncePayloadStampsRemotePermission`

Expected: compile failure because `buildAnnouncePayload` has no permission parameter.

- [ ] **Step 3: Implement desktop config and announce stamping**

Add `RemotePermission string` to `appConfig` and `RelayConfig` JSON as `remote_permission`. Default helper returns `proto.RemotePermissionFull` when empty/invalid. Pass the value to `newUplink` and `buildAnnouncePayload`; stamp every session snapshot with it before marshaling.

- [ ] **Step 4: Implement host-side inbound guard**

In `desktop/uplink.go`, before `u.host.SendLocalInbound`, check the configured owner permission:

```go
if !localFrameAllowedByPermission(u.remotePermission, f.Type) {
    log.Printf("uplink: drop inbound frame %s for permission %s", frameTypeNameLocal(f.Type), u.remotePermission)
    continue
}
```

Use a small local helper or a shared proto-level helper to avoid importing `internal/relay` into desktop.

- [ ] **Step 5: Update Settings UI and Wails API wrapper**

Update `desktop/frontend/src/lib/api.ts` RelayConfig with `remote_permission: string`. Add a select to `SettingsDialog.vue` with `view`, `control`, `full`, persist via `setRelayConfig`, and include explanatory hint text.

- [ ] **Step 6: Run desktop tests and frontend build/test**

Run:

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test -tags webkit2_41 ./desktop
cd desktop/frontend && npm run build && npm run test
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run: `git add desktop/config.go desktop/app.go desktop/uplink.go desktop/uplink_announce_test.go desktop/frontend/src/lib/api.ts desktop/frontend/src/components/SettingsDialog.vue && git commit -m "announce desktop remote permissions"`

---

### Task 3: Persistent Relay Admin Config Store

**Files:**
- Create: `internal/relay/admin_config.go`
- Modify: `internal/relay/auth.go`
- Modify: `internal/relay/server.go`
- Modify: `cmd/atterm-relay/main.go`
- Test: `cmd/atterm-relay/main_test.go`
- Test: `internal/relay/version_test.go`

- [ ] **Step 1: Write failing persistent config tests**

Add tests for loading/saving admin config, absence of main write token, and hashed token authentication after reload.

```go
func TestAdminConfigPersistsWithoutMainToken(t *testing.T) {
    path := filepath.Join(t.TempDir(), "relay.json")
    store := relay.NewAdminConfigStore(path, relay.AdminConfig{
        RateLimitPerMinute: 12,
        MaxConnectionsPerKey: 3,
        ReadOnlyTokens: []relay.StoredToken{{ID: "viewer", Hash: relay.HashBearerToken("secret"), CreatedAt: 123}},
    })
    if err := store.Save(); err != nil { t.Fatal(err) }
    data, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    if strings.Contains(string(data), "main-write-token") || strings.Contains(string(data), "secret\"") {
        t.Fatalf("config leaked secret: %s", data)
    }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test -tags webkit2_41 ./cmd/atterm-relay ./internal/relay -run 'TestAdminConfig|TestRelaySecurity'`

Expected: compile failure for missing admin config store/types.

- [ ] **Step 3: Implement admin config store**

Create exported types in `internal/relay/admin_config.go`:

```go
type StoredToken struct {
    ID string `json:"id"`
    Hash string `json:"hash"`
    CreatedAt int64 `json:"created_at"`
}

type AdminConfig struct {
    RateLimitPerMinute int `json:"rate_limit_per_minute"`
    MaxConnectionsPerKey int `json:"max_connections_per_key"`
    ReadOnlyTokens []StoredToken `json:"read_only_tokens,omitempty"`
}
```

Implement `HashBearerToken`, `NewAdminConfigStore`, `Load`, `Save` with atomic write `0600`, validation for unique IDs, and read-only token matching by hash.

- [ ] **Step 4: Wire hashed tokens into auth**

Extend `relay.Config` with stored token hashes or an auth helper so `authorizeWithScope` can accept both startup cleartext read-only tokens and persisted `sha256:<base64url>` hashes.

- [ ] **Step 5: Wire config path into relay startup**

Add `--config` / `ATTERM_RELAY_CONFIG` and load it in `buildRelayConfig`. Keep `Token` from env/flag/generated only. Persisted settings initialize rate limits, connection limits, and read-only token hashes.

- [ ] **Step 6: Run command/relay tests**

Run: `go test -tags webkit2_41 ./cmd/atterm-relay ./internal/relay`

Expected: PASS.

- [ ] **Step 7: Commit**

Run: `git add internal/relay/admin_config.go internal/relay/auth.go internal/relay/server.go cmd/atterm-relay/main.go cmd/atterm-relay/main_test.go internal/relay/version_test.go && git commit -m "persist relay admin config"`

---

### Task 4: Admin API And Minimal Page

**Files:**
- Create: `internal/relay/admin_http.go`
- Modify: `internal/relay/server.go`
- Test: `internal/relay/version_test.go`

- [ ] **Step 1: Write failing admin API tests**

Add tests:

```go
func TestAdminRoutesHiddenWithoutAdminToken(t *testing.T) {
    srv := NewServer(Config{})
    req := httptest.NewRequest(http.MethodGet, "/admin/api/config", nil)
    rec := httptest.NewRecorder()
    srv.ServeHTTP(rec, req)
    if rec.Code != http.StatusNotFound { t.Fatalf("status=%d; want 404", rec.Code) }
}

func TestAdminConfigRequiresAdminToken(t *testing.T) {
    srv := NewServer(Config{AdminToken: "admin"})
    req := httptest.NewRequest(http.MethodGet, "/admin/api/config", nil)
    rec := httptest.NewRecorder()
    srv.ServeHTTP(rec, req)
    if rec.Code != http.StatusUnauthorized { t.Fatalf("status=%d; want 401", rec.Code) }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test -tags webkit2_41 ./internal/relay -run TestAdmin`

Expected: compile failure for `AdminToken` or route behavior failure.

- [ ] **Step 3: Implement admin routes**

Add `AdminToken string` and config store pointer to `relay.Config`. Register `/admin/` and `/admin/api/*` only if token is set. Use only `Authorization: Bearer` admin auth. Return non-secret config JSON. Add create/delete hashed read-only token endpoints.

- [ ] **Step 4: Add minimal same-origin admin page**

Serve a compact HTML/JS page from Go string or `web/admin` static files. It prompts for admin token in a password input, keeps it in JS memory, and calls admin APIs with Authorization header. Do not use query tokens.

- [ ] **Step 5: Run admin tests**

Run: `go test -tags webkit2_41 ./internal/relay -run TestAdmin`

Expected: PASS.

- [ ] **Step 6: Commit**

Run: `git add internal/relay/admin_http.go internal/relay/server.go internal/relay/version_test.go && git commit -m "add relay admin api"`

---

### Task 5: Documentation And Full Verification

**Files:**
- Modify: `AGENTS.md`
- Modify: `README.md`
- Modify: `docs/spec/protocol.md`
- Modify: `docs/spec/architecture.md`
- Modify: `docs/spec/conventions.md`

- [ ] **Step 1: Update docs**

Document:

- Desktop owner permissions and intersection with token scope.
- `--config` / `ATTERM_RELAY_CONFIG`.
- `--admin-token` / `ATTERM_ADMIN_TOKEN`.
- Persistent config does not store main write token.
- Admin-created read-only tokens are shown once and stored hashed.

- [ ] **Step 2: Run full verification**

Run:

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test -tags webkit2_41 ./...
go vet -tags webkit2_41 ./...
node --test web/*.test.mjs
cd desktop/frontend && npm run build && npm run test
```

Expected: all commands exit 0; web tests show all pass; frontend vitest passes.

- [ ] **Step 3: Commit docs**

Run: `git add AGENTS.md README.md docs/spec/protocol.md docs/spec/architecture.md docs/spec/conventions.md && git commit -m "document relay permission admin config"`

- [ ] **Step 4: Final status**

Run: `git status -sb` and report commits plus verification evidence. Do not tag or release unless explicitly requested.
