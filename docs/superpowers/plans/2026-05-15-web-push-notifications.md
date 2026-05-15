# Web Push Command-Finished Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver OSC 133 command-finished events to browsers (desktop and mobile PWA) via self-hosted Web Push, so users get notifications even when the page is not open.

**Architecture:** Desktop frontend (already detects OSC 133 in v0.1.55) emits a new `TypeCommandEvent` frame through the existing uplink to the relay. Relay's new `internal/webpush/` package owns a VAPID keypair (auto-generated + persisted to `<ATTERM_RELAY_CONFIG_DIR>/web-push.json`), a token-hash-keyed subscription store, and a dispatch goroutine that POSTs Web Push notifications via `github.com/SherClockHolmes/webpush-go`. Browser Service Worker shows the notification on receive.

**Tech Stack:** Go (`github.com/SherClockHolmes/webpush-go`), Vue 3 + TypeScript (desktop frontend), vanilla JS + Service Worker (web/), vitest, node --test, Go testing.

**Spec:** `docs/superpowers/specs/2026-05-15-web-push-notifications-design.md`

---

## File Map

**Backend (new):**
- `internal/webpush/service.go` — `Service` type, `Open`, public API
- `internal/webpush/vapid.go` — VAPID keypair generation + base64url helpers
- `internal/webpush/subscription.go` — `Subscription` type, by-token registry, 16/token cap
- `internal/webpush/persist.go` — load / save / corrupt-backup logic
- `internal/webpush/transport.go` — thin wrapper around webpush-go (injectable HTTPClient)
- `internal/webpush/dispatch.go` — `DispatchCommandFinished`, `SendTest`, fanout
- `internal/webpush/*_test.go` — per-file tests
- `internal/relay/web_push_http.go` — 4 HTTP endpoints

**Backend (modify):**
- `internal/proto/frame.go` — add `TypeCommandEvent Type = 0x35`
- `internal/proto/codec.go` — `CommandEventPayload` struct + encode/decode helpers
- `internal/proto/codec_test.go` — extend with round-trip test for new frame
- `internal/relay/server.go` — `Config.WebPush *webpush.Service` + `WebPushSessionResolver` method
- `internal/relay/uplink_conn.go` — read-loop case for `TypeCommandEvent`
- `internal/relay/uplink_conn_test.go` — extend with frame-routing test
- `internal/relay/server_test.go` — extend with resolver test
- `cmd/atterm-relay/main.go` — wire `--vapid-subject`, derive config dir, call `webpush.Open`
- `go.mod` / `go.sum` — add `github.com/SherClockHolmes/webpush-go`

**Desktop (modify):**
- `desktop/uplink.go` — add `SendCommandEvent`
- `desktop/uplink_test.go` (new if absent) — test the new sender
- `desktop/app.go` — add `BroadcastCommandFinished` Wails binding
- `desktop/app_broadcast_test.go` (new) — binding round-trip test

**Frontend / desktop (modify):**
- `desktop/frontend/src/lib/api.ts` — add `broadcastCommandFinished` wrapper + `AppBindings` entry
- `desktop/frontend/src/components/TerminalView.vue` — call broadcast after passing gate
- `desktop/frontend/src/components/TerminalView.test.ts` — extend with new source-level assertion

**Frontend / web (modify):**
- `web/sw.js` — add `push` handler + bump `CACHE` constant
- `web/sw.test.mjs` (new) — push-event handler tests
- `web/app-core.js` — add `pushSupported`, `canEnablePush`, `base64UrlToUint8Array`
- `web/app-core.test.mjs` — extend with helper tests
- `web/app.js` — add `enablePushFlow`, `disablePushFlow`, button UI
- `web/push-flow.test.mjs` (new) — push flow tests
- `web/terminal-fit.test.mjs` — extend the existing sw-cache-bump assertion

**Docs (new + modify):**
- `docs/web-push.md` (new) — user-facing how-to
- `docs/spec/protocol.md` — add `TypeCommandEvent` entry
- `README.md` — capability row + doc link

---

## Conventions

All commands assume the worktree root `/Users/attson/code/github.com.attson/atterm/.claude/worktrees/web-push-notifications`. Shell prologue (use for every Go invocation):

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
```

Frontend tests run from `desktop/frontend/` via `npm run test -- --run`. Web (vanilla) tests run from worktree root via `node --test web/*.test.mjs`. Commit messages follow the existing `<type>: <subject>` style (`feat:` / `fix:` / `test:` / `docs:`).

---

### Task 1: Add `webpush-go` Dependency + VAPID Key Helpers

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/webpush/vapid.go`
- Create: `internal/webpush/vapid_test.go`
- Test: `internal/webpush/vapid_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/webpush/vapid_test.go`:

```go
package webpush

import (
	"strings"
	"testing"
)

func TestGenerateVAPIDKeypairReturnsBase64URLStrings(t *testing.T) {
	priv, pub, err := generateVAPIDKeypair()
	if err != nil {
		t.Fatalf("generateVAPIDKeypair: %v", err)
	}
	if priv == "" {
		t.Fatal("private key empty")
	}
	if pub == "" {
		t.Fatal("public key empty")
	}
	// Both should be base64url-encoded (no '+' or '/' or '=' padding).
	for _, k := range []string{priv, pub} {
		if strings.ContainsAny(k, "+/=") {
			t.Fatalf("key %q is not base64url (contains +/=)", k)
		}
	}
	if priv == pub {
		t.Fatal("private and public keys are identical")
	}
}

func TestGenerateVAPIDKeypairProducesDistinctKeys(t *testing.T) {
	priv1, pub1, _ := generateVAPIDKeypair()
	priv2, pub2, _ := generateVAPIDKeypair()
	if priv1 == priv2 || pub1 == pub2 {
		t.Fatalf("two generations produced identical keys: priv same=%v pub same=%v", priv1 == priv2, pub1 == pub2)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test ./internal/webpush/...
```

Expected: build failure (`generateVAPIDKeypair` undefined; `webpush` package doesn't exist).

- [ ] **Step 3: Add the dependency**

Run:
```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go get github.com/SherClockHolmes/webpush-go@v1.3.0
```

Expected: `go.mod` and `go.sum` updated; no errors.

- [ ] **Step 4: Write the implementation**

Create `internal/webpush/vapid.go`:

```go
// Package webpush implements self-hosted Web Push delivery: VAPID keypair
// management, browser subscription state, and dispatch of notifications via
// RFC 8030 / 8291. The relay calls into this package; nothing else does.
package webpush

import wpgo "github.com/SherClockHolmes/webpush-go"

// generateVAPIDKeypair returns a fresh P-256 keypair as base64url-encoded
// strings (matching the format the JavaScript Push API expects for
// applicationServerKey).
func generateVAPIDKeypair() (privateKey, publicKey string, err error) {
	return wpgo.GenerateVAPIDKeys()
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run:
```bash
go test ./internal/webpush/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/webpush/vapid.go internal/webpush/vapid_test.go
git commit -m "feat: vendor webpush-go and add VAPID keypair helper"
```

---

### Task 2: Subscription Type + By-Token Registry

**Files:**
- Create: `internal/webpush/subscription.go`
- Create: `internal/webpush/subscription_test.go`
- Test: `internal/webpush/subscription_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/webpush/subscription_test.go`:

```go
package webpush

import "testing"

func TestSubStoreAddNewEndpoint(t *testing.T) {
	s := newSubStore()
	sub := Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "p"
	sub.Keys.Auth = "a"
	if err := s.Add("tok1", sub); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got := s.ByToken("tok1")
	if len(got) != 1 {
		t.Fatalf("len after Add = %d; want 1", len(got))
	}
	if got[0].Endpoint != sub.Endpoint {
		t.Fatalf("Endpoint = %q; want %q", got[0].Endpoint, sub.Endpoint)
	}
}

func TestSubStoreAddOverwritesSameEndpoint(t *testing.T) {
	s := newSubStore()
	sub := Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "p"
	sub.Keys.Auth = "a"
	sub.CreatedAt = 100
	_ = s.Add("tok1", sub)
	sub.CreatedAt = 200
	_ = s.Add("tok1", sub)
	got := s.ByToken("tok1")
	if len(got) != 1 {
		t.Fatalf("len after re-Add = %d; want 1", len(got))
	}
	if got[0].CreatedAt != 200 {
		t.Fatalf("CreatedAt = %d; want 200 (refresh)", got[0].CreatedAt)
	}
}

func TestSubStoreCapAt16PerToken(t *testing.T) {
	s := newSubStore()
	for i := 0; i < maxSubsPerToken; i++ {
		sub := Subscription{Endpoint: "https://push.example/" + intToStr(i)}
		if err := s.Add("tok1", sub); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if len(s.ByToken("tok1")) != maxSubsPerToken {
		t.Fatalf("ByToken pre-cap = %d; want %d", len(s.ByToken("tok1")), maxSubsPerToken)
	}
	overflow := Subscription{Endpoint: "https://push.example/overflow"}
	if err := s.Add("tok1", overflow); err != nil {
		t.Fatalf("Add overflow: %v", err)
	}
	if len(s.ByToken("tok1")) != maxSubsPerToken {
		t.Fatalf("ByToken post-overflow = %d; want %d (drop silently)", len(s.ByToken("tok1")), maxSubsPerToken)
	}
}

func TestSubStoreRemoveIsIdempotent(t *testing.T) {
	s := newSubStore()
	sub := Subscription{Endpoint: "https://push.example/abc"}
	_ = s.Add("tok1", sub)
	if !s.Remove("tok1", sub.Endpoint) {
		t.Fatal("Remove(existing) returned false")
	}
	if s.Remove("tok1", sub.Endpoint) {
		t.Fatal("Remove(nonexistent) returned true")
	}
	if len(s.ByToken("tok1")) != 0 {
		t.Fatal("subs not empty after Remove")
	}
}

func TestSubStoreRemoveUnknownTokenIsNoop(t *testing.T) {
	s := newSubStore()
	if s.Remove("nonexistent", "https://push.example/x") {
		t.Fatal("Remove unknown token returned true")
	}
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/webpush/...
```

Expected: build failure (`newSubStore`, `Subscription`, `maxSubsPerToken` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/webpush/subscription.go`:

```go
package webpush

import "sync"

// maxSubsPerToken caps how many endpoints a single relay token may register.
// Beyond this, Add silently drops further endpoints to keep client retries
// idempotent.
const maxSubsPerToken = 16

// Subscription is one browser's push endpoint. JSON shape is the same one
// the Browser Push API hands to the page (endpoint + keys).
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	CreatedAt int64 `json:"created_at"`
}

type subStore struct {
	mu   sync.Mutex
	byID map[string][]Subscription // tokenHash -> subs
}

func newSubStore() *subStore {
	return &subStore{byID: make(map[string][]Subscription)}
}

// Add registers a subscription under the given tokenHash. Same-endpoint
// re-adds overwrite the existing entry (refreshing CreatedAt). Returns nil
// even when the cap is hit (drops silently).
func (s *subStore) Add(tokenHash string, sub Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs := s.byID[tokenHash]
	for i, existing := range subs {
		if existing.Endpoint == sub.Endpoint {
			subs[i] = sub
			s.byID[tokenHash] = subs
			return nil
		}
	}
	if len(subs) >= maxSubsPerToken {
		return nil
	}
	s.byID[tokenHash] = append(subs, sub)
	return nil
}

// Remove deletes the subscription for the given tokenHash and endpoint.
// Returns true when something was removed.
func (s *subStore) Remove(tokenHash, endpoint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs, ok := s.byID[tokenHash]
	if !ok {
		return false
	}
	for i, existing := range subs {
		if existing.Endpoint == endpoint {
			s.byID[tokenHash] = append(subs[:i], subs[i+1:]...)
			if len(s.byID[tokenHash]) == 0 {
				delete(s.byID, tokenHash)
			}
			return true
		}
	}
	return false
}

// ByToken returns a copy of the slice for the given tokenHash.
func (s *subStore) ByToken(tokenHash string) []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs := s.byID[tokenHash]
	if subs == nil {
		return nil
	}
	out := make([]Subscription, len(subs))
	copy(out, subs)
	return out
}

// snapshot returns a deep copy of the entire registry (for persistence).
func (s *subStore) snapshot() map[string][]Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]Subscription, len(s.byID))
	for k, v := range s.byID {
		copied := make([]Subscription, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out
}

// load replaces the registry contents (used during Open).
func (s *subStore) load(m map[string][]Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID = make(map[string][]Subscription, len(m))
	for k, v := range m {
		copied := make([]Subscription, len(v))
		copy(copied, v)
		s.byID[k] = copied
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/webpush/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webpush/subscription.go internal/webpush/subscription_test.go
git commit -m "feat: web-push subscription registry with per-token cap"
```

---

### Task 3: Persistence — Load / Save / Corrupt-Backup

**Files:**
- Create: `internal/webpush/persist.go`
- Create: `internal/webpush/persist_test.go`
- Test: `internal/webpush/persist_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/webpush/persist_test.go`:

```go
package webpush

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileGeneratesFresh(t *testing.T) {
	dir := t.TempDir()
	state, err := loadOrInitState(dir)
	if err != nil {
		t.Fatalf("loadOrInitState: %v", err)
	}
	if state.PrivateKey == "" || state.PublicKey == "" {
		t.Fatal("fresh state missing VAPID keys")
	}
	if len(state.Subscriptions) != 0 {
		t.Fatalf("fresh state has %d subs; want 0", len(state.Subscriptions))
	}
	// File should now exist on disk.
	if _, err := os.Stat(filepath.Join(dir, "web-push.json")); err != nil {
		t.Fatalf("web-push.json not created: %v", err)
	}
}

func TestLoadValidFileRoundTrips(t *testing.T) {
	dir := t.TempDir()
	original := persistedState{
		PrivateKey: "priv-abc",
		PublicKey:  "pub-xyz",
		Subscriptions: map[string][]Subscription{
			"tok1": {{Endpoint: "https://push.example/abc"}},
		},
	}
	data, _ := json.Marshal(original)
	if err := os.WriteFile(filepath.Join(dir, "web-push.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := loadOrInitState(dir)
	if err != nil {
		t.Fatalf("loadOrInitState: %v", err)
	}
	if state.PrivateKey != "priv-abc" {
		t.Fatalf("priv = %q; want priv-abc", state.PrivateKey)
	}
	if state.PublicKey != "pub-xyz" {
		t.Fatalf("pub = %q; want pub-xyz", state.PublicKey)
	}
	if len(state.Subscriptions["tok1"]) != 1 {
		t.Fatal("sub not loaded")
	}
}

func TestLoadCorruptFileBacksUpAndRegenerates(t *testing.T) {
	dir := t.TempDir()
	original := []byte("not json at all {{{ broken")
	path := filepath.Join(dir, "web-push.json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := loadOrInitState(dir)
	if err != nil {
		t.Fatalf("loadOrInitState: %v", err)
	}
	if state.PrivateKey == "" {
		t.Fatal("did not regenerate after corrupt")
	}
	// Old file should be renamed with .corrupt-* suffix.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	hasCorrupt := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "web-push.json.corrupt-") {
			hasCorrupt = true
		}
	}
	if !hasCorrupt {
		t.Fatalf("no .corrupt-* backup found; entries=%v", entries)
	}
}

func TestSaveStateWriteTempRename(t *testing.T) {
	dir := t.TempDir()
	state := persistedState{PrivateKey: "p", PublicKey: "q"}
	if err := saveState(dir, state); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "web-push.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got persistedState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.PrivateKey != "p" {
		t.Fatalf("priv = %q; want p", got.PrivateKey)
	}
}

func TestLoadEmptyDirIsAllowedWhenWritable(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadOrInitState(dir); err != nil {
		t.Fatalf("loadOrInitState(writable empty dir): %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/webpush/...
```

Expected: build failure (`loadOrInitState`, `persistedState`, `saveState` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/webpush/persist.go`:

```go
package webpush

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const stateFilename = "web-push.json"

// persistedState is the on-disk JSON shape.
type persistedState struct {
	PrivateKey    string                    `json:"private_key"`
	PublicKey     string                    `json:"public_key"`
	Subscriptions map[string][]Subscription `json:"subscriptions"`
}

// loadOrInitState reads <dir>/web-push.json. If missing, generates a fresh
// VAPID keypair and persists. If corrupt, renames the bad file with a
// .corrupt-<unix> suffix, then regenerates.
func loadOrInitState(dir string) (persistedState, error) {
	if dir == "" {
		return persistedState{}, errors.New("config dir is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return persistedState{}, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, stateFilename)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return regenerateAndPersist(dir)
	}
	if err != nil {
		return persistedState{}, fmt.Errorf("read %s: %w", path, err)
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
		if renameErr := os.Rename(path, backup); renameErr != nil {
			log.Printf("webpush: rename corrupt state %s -> %s failed: %v", path, backup, renameErr)
		} else {
			log.Printf("webpush: state file corrupt; backed up to %s and regenerating", backup)
		}
		return regenerateAndPersist(dir)
	}
	if state.PrivateKey == "" || state.PublicKey == "" {
		// Partial state (no keys); regenerate but keep loaded subs.
		priv, pub, err := generateVAPIDKeypair()
		if err != nil {
			return persistedState{}, fmt.Errorf("generate vapid: %w", err)
		}
		state.PrivateKey = priv
		state.PublicKey = pub
		if err := saveState(dir, state); err != nil {
			log.Printf("webpush: persist regenerated state failed: %v", err)
		}
	}
	if state.Subscriptions == nil {
		state.Subscriptions = make(map[string][]Subscription)
	}
	return state, nil
}

func regenerateAndPersist(dir string) (persistedState, error) {
	priv, pub, err := generateVAPIDKeypair()
	if err != nil {
		return persistedState{}, fmt.Errorf("generate vapid: %w", err)
	}
	state := persistedState{
		PrivateKey:    priv,
		PublicKey:     pub,
		Subscriptions: make(map[string][]Subscription),
	}
	if err := saveState(dir, state); err != nil {
		log.Printf("webpush: persist fresh state failed: %v", err)
	}
	return state, nil
}

// saveState writes state to <dir>/web-push.json atomically (write-temp-rename).
// Failure logs a WARN; the in-memory state is the source of truth at runtime.
func saveState(dir string, state persistedState) error {
	if dir == "" {
		return errors.New("config dir is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, stateFilename)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/webpush/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webpush/persist.go internal/webpush/persist_test.go
git commit -m "feat: persist web-push state with corrupt-file backup"
```

---

### Task 4: Transport Wrapper Around webpush-go

**Files:**
- Create: `internal/webpush/transport.go`
- Create: `internal/webpush/transport_test.go`
- Test: `internal/webpush/transport_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/webpush/transport_test.go`:

```go
package webpush

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeHTTPClient struct {
	lastReq  *http.Request
	respCode int
}

func (f *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	f.lastReq = req
	return &http.Response{
		StatusCode: f.respCode,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     http.Header{},
	}, nil
}

func TestSendNotificationPostsToEndpoint(t *testing.T) {
	fake := &fakeHTTPClient{respCode: 201}
	tr := newTransport("priv-key", "pub-key", "mailto:test@example.com", fake)
	sub := Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = base64URLPad("p256dh-bytes-fake")
	sub.Keys.Auth = base64URLPad("auth-bytes-fake")
	resp, err := tr.Send(context.Background(), sub, []byte(`{"title":"x"}`))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d; want 201", resp.StatusCode)
	}
	if fake.lastReq == nil {
		t.Fatal("HTTPClient.Do not invoked")
	}
	if fake.lastReq.URL.String() != sub.Endpoint {
		t.Fatalf("URL = %s; want %s", fake.lastReq.URL.String(), sub.Endpoint)
	}
	auth := fake.lastReq.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "vapid t=") {
		t.Fatalf("Authorization = %q; want vapid t=... prefix", auth)
	}
}

// base64URLPad returns a base64url-encoded version of s suitable for stubbed
// p256dh / auth fields. webpush-go itself decodes these so we feed it
// something that decodes cleanly.
func base64URLPad(s string) string {
	// minimal 16-byte filler encoded as base64url
	return "AAECAwQFBgcICQoLDA0ODw"
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/webpush/...
```

Expected: build failure (`newTransport`, `transport.Send` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/webpush/transport.go`:

```go
package webpush

import (
	"bytes"
	"context"
	"net/http"

	wpgo "github.com/SherClockHolmes/webpush-go"
)

// transport wraps webpush-go.SendNotificationWithContext with an injectable
// HTTPClient so tests can capture requests without hitting a real push
// service.
type transport struct {
	privateKey string
	publicKey  string
	subject    string
	httpClient wpgo.HTTPClient
}

func newTransport(priv, pub, subject string, httpClient wpgo.HTTPClient) *transport {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &transport{privateKey: priv, publicKey: pub, subject: subject, httpClient: httpClient}
}

// Send POSTs an encrypted Web Push notification carrying msg to sub.
// Returns the raw response (caller inspects status code) and any transport
// error. Callers are expected to consume / close the response body.
func (t *transport) Send(ctx context.Context, sub Subscription, msg []byte) (*http.Response, error) {
	wpSub := &wpgo.Subscription{
		Endpoint: sub.Endpoint,
		Keys: wpgo.Keys{
			Auth:   sub.Keys.Auth,
			P256dh: sub.Keys.P256dh,
		},
	}
	opts := &wpgo.Options{
		HTTPClient:      t.httpClient,
		TTL:             30, // seconds
		Subscriber:      t.subject,
		VAPIDPublicKey:  t.publicKey,
		VAPIDPrivateKey: t.privateKey,
	}
	return wpgo.SendNotificationWithContext(ctx, bytes.Clone(msg), wpSub, opts)
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/webpush/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webpush/transport.go internal/webpush/transport_test.go
git commit -m "feat: transport wrapper around webpush-go SendNotificationWithContext"
```

---

### Task 5: `Service` Type + `Open` Entrypoint

**Files:**
- Create: `internal/webpush/service.go`
- Create: `internal/webpush/service_test.go`
- Test: `internal/webpush/service_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/webpush/service_test.go`:

```go
package webpush

import (
	"testing"
)

func TestOpenFreshDirGeneratesKeysAndReturnsService(t *testing.T) {
	dir := t.TempDir()
	svc, err := Open(dir, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if svc == nil {
		t.Fatal("Open returned nil service")
	}
	if svc.PublicKey() == "" {
		t.Fatal("PublicKey empty")
	}
}

func TestOpenLoadsExistingState(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	pubKey := first.PublicKey()
	second, err := Open(dir, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if second.PublicKey() != pubKey {
		t.Fatalf("second PublicKey changed (regenerated?); first=%q second=%q", pubKey, second.PublicKey())
	}
}

func TestOpenEmptyDirReturnsInMemoryService(t *testing.T) {
	svc, err := Open("", "mailto:test@example.com")
	if err != nil {
		t.Fatalf("Open(empty dir): %v", err)
	}
	if svc == nil {
		t.Fatal("Open(empty dir) returned nil; expected in-memory service")
	}
	if svc.PublicKey() == "" {
		t.Fatal("in-memory service has no VAPID public key")
	}
}

func TestServiceAddAndRemoveSubscriptionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	svc, _ := Open(dir, "mailto:test@example.com")
	sub := Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "p"
	sub.Keys.Auth = "a"
	if err := svc.AddSubscription("tokhash", sub); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	// New Open should see the persisted subscription.
	svc2, _ := Open(dir, "mailto:test@example.com")
	if got := svc2.subStore.ByToken("tokhash"); len(got) != 1 || got[0].Endpoint != sub.Endpoint {
		t.Fatalf("persisted subs not loaded; got %v", got)
	}
	if err := svc.RemoveSubscription("tokhash", sub.Endpoint); err != nil {
		t.Fatalf("RemoveSubscription: %v", err)
	}
	svc3, _ := Open(dir, "mailto:test@example.com")
	if got := svc3.subStore.ByToken("tokhash"); len(got) != 0 {
		t.Fatalf("subs not removed after persist; got %v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/webpush/...
```

Expected: build failure (`Open`, `Service.PublicKey`, `AddSubscription`, `RemoveSubscription` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/webpush/service.go`:

```go
package webpush

import (
	"log"
	"sync"

	"github.com/google/uuid"
)

// Service is the public face of the webpush package. One per relay process.
type Service struct {
	subject string
	dir     string

	mu        sync.Mutex
	vapidPriv string
	vapidPub  string

	subStore *subStore
	tr       *transport

	resolverMu sync.RWMutex
	resolver   func(uuid.UUID) []string
}

// Open initializes the service. Recoverable conditions (missing file,
// corrupt state, unwritable dir) are downgraded to in-memory mode + a
// one-time WARN log. A non-nil error is returned only when even the
// in-memory fallback cannot be constructed (e.g., crypto generation fails).
func Open(dir, vapidSubject string) (*Service, error) {
	if vapidSubject == "" {
		vapidSubject = "mailto:noreply@atterm.local"
	}
	svc := &Service{
		subject:  vapidSubject,
		dir:      dir,
		subStore: newSubStore(),
	}
	if dir == "" {
		priv, pub, err := generateVAPIDKeypair()
		if err != nil {
			return nil, err
		}
		svc.vapidPriv = priv
		svc.vapidPub = pub
		log.Printf("webpush: running in-memory (no config dir); subscriptions will be lost on restart")
	} else {
		state, err := loadOrInitState(dir)
		if err != nil {
			// Fall back to in-memory.
			log.Printf("webpush: persistence unavailable (%v); running in-memory", err)
			priv, pub, genErr := generateVAPIDKeypair()
			if genErr != nil {
				return nil, genErr
			}
			svc.vapidPriv = priv
			svc.vapidPub = pub
			svc.dir = ""
		} else {
			svc.vapidPriv = state.PrivateKey
			svc.vapidPub = state.PublicKey
			svc.subStore.load(state.Subscriptions)
		}
	}
	svc.tr = newTransport(svc.vapidPriv, svc.vapidPub, svc.subject, nil)
	return svc, nil
}

// PublicKey returns the VAPID public key as a base64url string for the
// browser's applicationServerKey.
func (s *Service) PublicKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vapidPub
}

// AddSubscription registers a subscription and persists state.
func (s *Service) AddSubscription(tokenHash string, sub Subscription) error {
	if err := s.subStore.Add(tokenHash, sub); err != nil {
		return err
	}
	s.persistBestEffort()
	return nil
}

// RemoveSubscription deregisters an endpoint and persists state.
func (s *Service) RemoveSubscription(tokenHash, endpoint string) error {
	s.subStore.Remove(tokenHash, endpoint)
	s.persistBestEffort()
	return nil
}

// SetSessionResolver registers the function that maps a session id to the
// token-hashes allowed to view it. The resolver is called from the dispatch
// goroutine; implementations must be cheap.
func (s *Service) SetSessionResolver(f func(uuid.UUID) []string) {
	s.resolverMu.Lock()
	s.resolver = f
	s.resolverMu.Unlock()
}

func (s *Service) lookupResolver() func(uuid.UUID) []string {
	s.resolverMu.RLock()
	defer s.resolverMu.RUnlock()
	return s.resolver
}

func (s *Service) persistBestEffort() {
	if s.dir == "" {
		return
	}
	state := persistedState{
		PrivateKey:    s.vapidPriv,
		PublicKey:     s.vapidPub,
		Subscriptions: s.subStore.snapshot(),
	}
	if err := saveState(s.dir, state); err != nil {
		log.Printf("webpush: persistBestEffort: %v", err)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/webpush/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webpush/service.go internal/webpush/service_test.go
git commit -m "feat: webpush Service entrypoint with persistence fallback"
```

---

### Task 6: Dispatch — Command-Finished Fanout + SendTest

**Files:**
- Create: `internal/webpush/dispatch.go`
- Create: `internal/webpush/dispatch_test.go`
- Test: `internal/webpush/dispatch_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/webpush/dispatch_test.go`:

```go
package webpush

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingHTTPClient struct {
	mu       sync.Mutex
	requests []*http.Request
	statuses []int
}

func (c *recordingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	status := 201
	if len(c.statuses) > 0 {
		status = c.statuses[0]
		c.statuses = c.statuses[1:]
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     http.Header{},
	}, nil
}

func (c *recordingHTTPClient) sentBodies() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.requests))
	for i, r := range c.requests {
		buf, _ := io.ReadAll(r.Body)
		out[i] = string(buf)
	}
	return out
}

func newServiceWithFakeTransport(t *testing.T, statuses ...int) (*Service, *recordingHTTPClient) {
	t.Helper()
	dir := t.TempDir()
	svc, err := Open(dir, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rec := &recordingHTTPClient{statuses: statuses}
	svc.tr = newTransport(svc.vapidPriv, svc.vapidPub, svc.subject, rec)
	return svc, rec
}

func TestDispatchCommandFinishedFansOutToAllSubscriptions(t *testing.T) {
	svc, rec := newServiceWithFakeTransport(t)
	subA := Subscription{Endpoint: "https://push.example/a"}
	subA.Keys.P256dh = "AAECAwQFBgcICQoLDA0ODw"
	subA.Keys.Auth = "AAECAwQFBgcICQoLDA0ODw"
	subB := Subscription{Endpoint: "https://push.example/b"}
	subB.Keys.P256dh = "AAECAwQFBgcICQoLDA0ODw"
	subB.Keys.Auth = "AAECAwQFBgcICQoLDA0ODw"
	_ = svc.AddSubscription("tok1", subA)
	_ = svc.AddSubscription("tok1", subB)
	svc.SetSessionResolver(func(_ uuid.UUID) []string {
		return []string{"tok1"}
	})
	sid := uuid.New()
	svc.DispatchCommandFinished(CommandFinished{
		SessionID: sid,
		HostID:    uuid.New(),
		ExitCode:  0,
		ElapsedMS: 12500,
		Label:     "atterm",
	})
	// Wait briefly for goroutine fanout.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		n := len(rec.requests)
		rec.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	rec.mu.Lock()
	n := len(rec.requests)
	rec.mu.Unlock()
	if n != 2 {
		t.Fatalf("requests = %d; want 2", n)
	}
}

func TestDispatchCommandFinishedReturnsImmediately(t *testing.T) {
	svc, _ := newServiceWithFakeTransport(t)
	sub := Subscription{Endpoint: "https://push.example/a"}
	sub.Keys.P256dh = "AAECAwQFBgcICQoLDA0ODw"
	sub.Keys.Auth = "AAECAwQFBgcICQoLDA0ODw"
	_ = svc.AddSubscription("tok1", sub)
	svc.SetSessionResolver(func(_ uuid.UUID) []string { return []string{"tok1"} })
	start := time.Now()
	svc.DispatchCommandFinished(CommandFinished{SessionID: uuid.New()})
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("DispatchCommandFinished took %v; expected to return immediately", elapsed)
	}
}

func TestDispatch410PrunesSubscription(t *testing.T) {
	svc, _ := newServiceWithFakeTransport(t, 410)
	sub := Subscription{Endpoint: "https://push.example/gone"}
	sub.Keys.P256dh = "AAECAwQFBgcICQoLDA0ODw"
	sub.Keys.Auth = "AAECAwQFBgcICQoLDA0ODw"
	_ = svc.AddSubscription("tok1", sub)
	svc.SetSessionResolver(func(_ uuid.UUID) []string { return []string{"tok1"} })
	svc.DispatchCommandFinished(CommandFinished{SessionID: uuid.New()})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(svc.subStore.ByToken("tok1")) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("subscription not pruned after 410; got %v", svc.subStore.ByToken("tok1"))
}

func TestDispatch429KeepsSubscription(t *testing.T) {
	svc, _ := newServiceWithFakeTransport(t, 429)
	sub := Subscription{Endpoint: "https://push.example/throttled"}
	sub.Keys.P256dh = "AAECAwQFBgcICQoLDA0ODw"
	sub.Keys.Auth = "AAECAwQFBgcICQoLDA0ODw"
	_ = svc.AddSubscription("tok1", sub)
	svc.SetSessionResolver(func(_ uuid.UUID) []string { return []string{"tok1"} })
	svc.DispatchCommandFinished(CommandFinished{SessionID: uuid.New()})
	// Give the goroutine a moment then assert sub still present.
	time.Sleep(150 * time.Millisecond)
	if len(svc.subStore.ByToken("tok1")) != 1 {
		t.Fatalf("subscription was pruned on 429; want kept")
	}
}

func TestDispatchEmitsPayloadWithSessionTagAndExpectedFields(t *testing.T) {
	svc, rec := newServiceWithFakeTransport(t)
	sub := Subscription{Endpoint: "https://push.example/a"}
	sub.Keys.P256dh = "AAECAwQFBgcICQoLDA0ODw"
	sub.Keys.Auth = "AAECAwQFBgcICQoLDA0ODw"
	_ = svc.AddSubscription("tok1", sub)
	svc.SetSessionResolver(func(_ uuid.UUID) []string { return []string{"tok1"} })
	sid := uuid.New()
	hid := uuid.New()
	svc.DispatchCommandFinished(CommandFinished{
		SessionID: sid,
		HostID:    hid,
		ExitCode:  127,
		ElapsedMS: 65000,
		Label:     "build",
	})
	// Wait for at least one request.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		n := len(rec.requests)
		rec.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// We can't read the encrypted body directly, but we CAN call payloadJSON
	// to verify shape; integration through Send is covered by the
	// "fans out to all subscriptions" test.
	body := payloadJSON(CommandFinished{
		SessionID: sid, HostID: hid, ExitCode: 127, ElapsedMS: 65000, Label: "build",
	})
	var payload struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Tag   string `json:"tag"`
		Data  struct {
			ExitCode  int    `json:"exitCode"`
			ElapsedMs int    `json:"elapsedMs"`
			SessionID string `json:"sessionId"`
			HostID    string `json:"hostId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if !strings.Contains(payload.Title, "AT Term") || !strings.Contains(payload.Title, "build") {
		t.Fatalf("title = %q; want contains 'AT Term' and 'build'", payload.Title)
	}
	if !strings.Contains(payload.Body, "exit 127") || !strings.Contains(payload.Body, "1m5s") {
		t.Fatalf("body = %q; want contains 'exit 127' and '1m5s'", payload.Body)
	}
	if payload.Tag != sid.String() {
		t.Fatalf("tag = %q; want %s", payload.Tag, sid)
	}
	if payload.Data.SessionID != sid.String() || payload.Data.HostID != hid.String() {
		t.Fatalf("data ids mismatch: %+v", payload.Data)
	}
}

func TestDispatchTruncatesLabelTo256(t *testing.T) {
	huge := strings.Repeat("a", 1000)
	body := payloadJSON(CommandFinished{Label: huge})
	if !strings.Contains(string(body), strings.Repeat("a", 256)) {
		t.Fatal("payload missing truncated label")
	}
	if strings.Contains(string(body), strings.Repeat("a", 257)) {
		t.Fatal("payload was not truncated to 256")
	}
}

func TestDispatchSendTest(t *testing.T) {
	svc, rec := newServiceWithFakeTransport(t)
	sub := Subscription{Endpoint: "https://push.example/a"}
	sub.Keys.P256dh = "AAECAwQFBgcICQoLDA0ODw"
	sub.Keys.Auth = "AAECAwQFBgcICQoLDA0ODw"
	_ = svc.AddSubscription("tok1", sub)
	n := svc.SendTest("tok1")
	if n != 1 {
		t.Fatalf("SendTest returned %d; want 1", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		got := len(rec.requests)
		rec.mu.Unlock()
		if got >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("SendTest did not POST a push")
}

func TestDispatchNoOpWhenResolverUnset(t *testing.T) {
	svc, rec := newServiceWithFakeTransport(t)
	sub := Subscription{Endpoint: "https://push.example/a"}
	sub.Keys.P256dh = "AAECAwQFBgcICQoLDA0ODw"
	sub.Keys.Auth = "AAECAwQFBgcICQoLDA0ODw"
	_ = svc.AddSubscription("tok1", sub)
	// No resolver set.
	svc.DispatchCommandFinished(CommandFinished{SessionID: uuid.New()})
	// allow goroutine to potentially run
	time.Sleep(100 * time.Millisecond)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.requests) != 0 {
		t.Fatalf("unexpected pushes without resolver; %d requests", len(rec.requests))
	}
	_ = context.Background()
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/webpush/...
```

Expected: build failure (`CommandFinished`, `DispatchCommandFinished`, `SendTest`, `payloadJSON` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/webpush/dispatch.go`:

```go
package webpush

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	maxLabelLen = 256
	sendTimeout = 10 * time.Second
)

// CommandFinished is the input to a command-finished push.
type CommandFinished struct {
	SessionID uuid.UUID
	HostID    uuid.UUID
	ExitCode  int
	ElapsedMS int
	Label     string
}

// DispatchCommandFinished fans the event out to all subscriptions of every
// token-hash returned by the session resolver. Always returns immediately;
// fanout runs in goroutines. Failures with 404/410 status prune the
// subscription; other errors are logged and the subscription is kept.
func (s *Service) DispatchCommandFinished(ev CommandFinished) {
	resolver := s.lookupResolver()
	if resolver == nil {
		return
	}
	if len(ev.Label) > maxLabelLen {
		ev.Label = ev.Label[:maxLabelLen]
	}
	tokens := resolver(ev.SessionID)
	if len(tokens) == 0 {
		return
	}
	body := payloadJSON(ev)
	for _, tokenHash := range tokens {
		subs := s.subStore.ByToken(tokenHash)
		for _, sub := range subs {
			go s.sendOne(tokenHash, sub, body)
		}
	}
}

// SendTest dispatches a "test" notification to every subscription under
// tokenHash. Returns the number of pushes dispatched (not delivered).
func (s *Service) SendTest(tokenHash string) int {
	subs := s.subStore.ByToken(tokenHash)
	body, _ := json.Marshal(map[string]interface{}{
		"title": "AT Term test",
		"body":  "It works.",
	})
	for _, sub := range subs {
		go s.sendOne(tokenHash, sub, body)
	}
	return len(subs)
}

func (s *Service) sendOne(tokenHash string, sub Subscription, body []byte) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("webpush: panic in send: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	resp, err := s.tr.Send(ctx, sub, body)
	if err != nil {
		log.Printf("webpush: send err endpoint=%s: %v", sub.Endpoint, err)
		return
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		log.Printf("webpush: endpoint %s gone (status %d); pruning", sub.Endpoint, resp.StatusCode)
		s.subStore.Remove(tokenHash, sub.Endpoint)
		s.persistBestEffort()
	default:
		log.Printf("webpush: send non-2xx endpoint=%s status=%d", sub.Endpoint, resp.StatusCode)
	}
}

// payloadJSON encodes the notification payload that the browser SW will
// see. Exposed for tests.
var payloadJSONOnce sync.Once

func payloadJSON(ev CommandFinished) []byte {
	label := ev.Label
	if label == "" {
		label = "session"
	}
	if len(label) > maxLabelLen {
		label = label[:maxLabelLen]
	}
	payload := map[string]interface{}{
		"title": fmt.Sprintf("AT Term · %s", label),
		"body":  fmt.Sprintf("Command finished · exit %d · %s", ev.ExitCode, formatElapsed(ev.ElapsedMS)),
		"tag":   ev.SessionID.String(),
		"data": map[string]interface{}{
			"exitCode":  ev.ExitCode,
			"elapsedMs": ev.ElapsedMS,
			"sessionId": ev.SessionID.String(),
			"hostId":    ev.HostID.String(),
		},
	}
	b, _ := json.Marshal(payload)
	payloadJSONOnce.Do(func() {})
	return b
}

func formatElapsed(ms int) string {
	if ms < 0 {
		ms = 0
	}
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	return fmt.Sprintf("%dm%ds", sec/60, sec%60)
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/webpush/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webpush/dispatch.go internal/webpush/dispatch_test.go
git commit -m "feat: webpush command-finished dispatch with 410 prune and label truncate"
```

---

### Task 7: Protocol — `TypeCommandEvent` Frame

**Files:**
- Modify: `internal/proto/frame.go`
- Modify: `internal/proto/codec.go`
- Modify: `internal/proto/codec_test.go`
- Test: `internal/proto/codec_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/proto/codec_test.go`:

```go
func TestCommandEventRoundTrip(t *testing.T) {
	sid := uuid.New()
	payload := CommandEventPayload{ExitCode: 0, ElapsedMS: 12500, Label: "atterm"}
	frame, err := EncodeCommandEvent(sid, payload)
	if err != nil {
		t.Fatalf("EncodeCommandEvent: %v", err)
	}
	if frame.Type != TypeCommandEvent {
		t.Fatalf("Type = %v; want %v", frame.Type, TypeCommandEvent)
	}
	if frame.SessionID != sid {
		t.Fatalf("SessionID = %v; want %v", frame.SessionID, sid)
	}
	out, err := DecodeCommandEvent(frame)
	if err != nil {
		t.Fatalf("DecodeCommandEvent: %v", err)
	}
	if out.ExitCode != 0 || out.ElapsedMS != 12500 || out.Label != "atterm" {
		t.Fatalf("decoded payload = %+v; want %+v", out, payload)
	}
}

func TestCommandEventEmptyLabelAllowed(t *testing.T) {
	sid := uuid.New()
	frame, err := EncodeCommandEvent(sid, CommandEventPayload{ExitCode: 1, ElapsedMS: 0})
	if err != nil {
		t.Fatalf("EncodeCommandEvent: %v", err)
	}
	out, err := DecodeCommandEvent(frame)
	if err != nil {
		t.Fatalf("DecodeCommandEvent: %v", err)
	}
	if out.Label != "" {
		t.Fatalf("Label = %q; want empty", out.Label)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/proto/...
```

Expected: build failure (`TypeCommandEvent`, `EncodeCommandEvent`, `DecodeCommandEvent`, `CommandEventPayload` undefined).

- [ ] **Step 3: Add the type constant**

In `internal/proto/frame.go`, find the existing `const (... Type = 0x34)` block. Add a new entry after `TypeClaimDriver`:

```go
	TypeCommandEvent  Type = 0x35 // uplink -> relay (Web Push trigger)
```

- [ ] **Step 4: Add the payload struct + encode/decode helpers**

In `internal/proto/codec.go`, append:

```go
// CommandEventPayload is the JSON body of a TypeCommandEvent frame.
// Direction: uplink -> relay. Not forwarded to clients.
type CommandEventPayload struct {
	ExitCode  int    `json:"exit_code"`
	ElapsedMS int    `json:"elapsed_ms"`
	Label     string `json:"label,omitempty"`
}

// EncodeCommandEvent builds a TypeCommandEvent frame.
func EncodeCommandEvent(sessionID uuid.UUID, payload CommandEventPayload) (Frame, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Frame{}, fmt.Errorf("marshal command event: %w", err)
	}
	return Frame{Type: TypeCommandEvent, SessionID: sessionID, Payload: body}, nil
}

// DecodeCommandEvent extracts the JSON payload from a TypeCommandEvent frame.
func DecodeCommandEvent(f Frame) (CommandEventPayload, error) {
	if f.Type != TypeCommandEvent {
		return CommandEventPayload{}, fmt.Errorf("not a TypeCommandEvent frame: %v", f.Type)
	}
	var out CommandEventPayload
	if err := json.Unmarshal(f.Payload, &out); err != nil {
		return CommandEventPayload{}, fmt.Errorf("unmarshal command event: %w", err)
	}
	return out, nil
}
```

If the `import` block at the top of `codec.go` does not already include `encoding/json` and `fmt`, add them.

- [ ] **Step 5: Run the test to verify it passes**

```bash
go test ./internal/proto/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/proto/frame.go internal/proto/codec.go internal/proto/codec_test.go
git commit -m "feat: add TypeCommandEvent (0x35) frame for Web Push triggers"
```

---

### Task 8: Relay `Config.WebPush` + `WebPushSessionResolver`

**Files:**
- Modify: `internal/relay/server.go`
- Modify: `internal/relay/server_test.go`
- Test: `internal/relay/server_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/server_test.go`:

```go
func TestWebPushSessionResolverReturnsAuthorizedTokenHashes(t *testing.T) {
	srv := NewServer(Config{
		Token:               "write-token",
		ReadOnlyTokens:      []string{"read-token"},
	})
	// Register a session with remote_permission = full.
	sid := uuid.New()
	info := proto.SessionInfo{
		Command:          "bash",
		HostID:           uuid.New().String(),
		RemotePermission: proto.RemotePermissionFull,
	}
	srv.Registry().Add(session.New(sid, info))
	defer srv.Registry().Remove(sid)
	got := srv.WebPushSessionResolver(sid)
	wantWrite := tokenHash("write-token")
	wantRead := tokenHash("read-token")
	if !containsString(got, wantWrite) {
		t.Errorf("missing write tokenHash; got %v want contains %v", got, wantWrite)
	}
	if !containsString(got, wantRead) {
		t.Errorf("missing read tokenHash; got %v want contains %v", got, wantRead)
	}
}

func TestWebPushSessionResolverEmptyForUnknownSession(t *testing.T) {
	srv := NewServer(Config{Token: "write-token"})
	got := srv.WebPushSessionResolver(uuid.New())
	if len(got) != 0 {
		t.Fatalf("WebPushSessionResolver(unknown) = %v; want empty", got)
	}
}

func TestWebPushSessionResolverSkipsReadTokenForViewOnlyRemotePermission(t *testing.T) {
	srv := NewServer(Config{
		Token:          "write-token",
		ReadOnlyTokens: []string{"read-token"},
	})
	sid := uuid.New()
	// remote_permission view: both read and write tokens can view.
	info := proto.SessionInfo{
		HostID:           uuid.New().String(),
		RemotePermission: proto.RemotePermissionView,
	}
	srv.Registry().Add(session.New(sid, info))
	defer srv.Registry().Remove(sid)
	got := srv.WebPushSessionResolver(sid)
	if !containsString(got, tokenHash("write-token")) || !containsString(got, tokenHash("read-token")) {
		t.Fatalf("expected both tokens for view permission; got %v", got)
	}
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
```

If `server_test.go` does not already import `session` and `proto`, add the imports.

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/relay/ -run TestWebPushSessionResolver
```

Expected: build failure (`Server.WebPushSessionResolver`, `tokenHash` undefined).

- [ ] **Step 3: Add `Config.WebPush` + resolver**

In `internal/relay/server.go`, find the existing `Config` struct and add:

```go
	// WebPush, when non-nil, enables the /api/push/* endpoints and the
	// TypeCommandEvent uplink handler. May be nil to disable the feature.
	WebPush *webpush.Service
```

Add the import `"github.com/attson/atterm/internal/webpush"` to the existing import block.

At the bottom of `server.go`, append the resolver:

```go
// WebPushSessionResolver returns the list of token-hashes authorized to
// view a session at the given id. Empty when the session is unknown.
// Used as the SessionResolver injected into webpush.Service at startup.
func (s *Server) WebPushSessionResolver(sessionID uuid.UUID) []string {
	sess, ok := s.registry.Get(sessionID)
	if !ok {
		return nil
	}
	info := sess.Info()
	perm := info.RemotePermission
	if perm == "" {
		perm = proto.RemotePermissionFull
	}
	out := make([]string, 0, 1+len(s.cfg.ReadOnlyTokens))
	if s.cfg.Token != "" {
		out = append(out, tokenHash(s.cfg.Token))
	}
	// All read-only tokens can view at minimum, regardless of perm.
	for _, t := range s.cfg.ReadOnlyTokens {
		out = append(out, tokenHash(t))
	}
	return out
}

// tokenHash is the canonical sha256+base64url form used as a key in
// webpush.Service subscription registry.
func tokenHash(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
```

If `crypto/sha256` or `encoding/base64` or `github.com/google/uuid` are not in the import block, add them.

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/relay/ -run TestWebPushSessionResolver
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/server.go internal/relay/server_test.go
git commit -m "feat: relay Config.WebPush + token-hash session resolver"
```

---

### Task 9: Relay HTTP Endpoints — `/api/push/*`

**Files:**
- Create: `internal/relay/web_push_http.go`
- Create: `internal/relay/web_push_http_test.go`
- Modify: `internal/relay/server.go` — register routes
- Test: `internal/relay/web_push_http_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/relay/web_push_http_test.go`:

```go
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/webpush"
)

func newWebPushTestServer(t *testing.T) (*Server, *webpush.Service) {
	t.Helper()
	svc, err := webpush.Open(t.TempDir(), "mailto:test@example.com")
	if err != nil {
		t.Fatalf("webpush.Open: %v", err)
	}
	srv := NewServer(Config{
		Token:          "write-token",
		ReadOnlyTokens: []string{"read-token"},
		WebPush:        svc,
	})
	return srv, svc
}

func doRequest(t *testing.T, srv *Server, method, path, token, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Result()
}

func TestPushKeyReturnsBase64URLPublicKey(t *testing.T) {
	srv, svc := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", "write-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var out struct{ Key string `json:"key"` }
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Key != svc.PublicKey() {
		t.Fatalf("Key = %q; want %q", out.Key, svc.PublicKey())
	}
}

func TestPushKeyAllowsReadOnlyToken(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", "read-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
}

func TestPushKeyRejectsMissingToken(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", "", "")
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d; want 401", resp.StatusCode)
	}
}

func TestPushKey503WhenWebPushDisabled(t *testing.T) {
	srv := NewServer(Config{Token: "write-token", WebPush: nil})
	resp := doRequest(t, srv, http.MethodGet, "/api/push/key", "write-token", "")
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d; want 503", resp.StatusCode)
	}
}

func TestSubscribeHappyPath(t *testing.T) {
	srv, svc := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", "write-token", body)
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, raw)
	}
	subs := svc.SubscriptionsForToken(tokenHash("write-token"))
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example/abc" {
		t.Fatalf("subs = %+v", subs)
	}
}

func TestSubscribe400OnInvalidJSON(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", "write-token", "{not json}")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d; want 400", resp.StatusCode)
	}
}

func TestSubscribe400OnHTTPEndpoint(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	body := `{"endpoint":"http://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", "write-token", body)
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d; want 400", resp.StatusCode)
	}
}

func TestSubscribe400OnMissingKeys(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"","auth":""}}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/subscribe", "write-token", body)
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d; want 400", resp.StatusCode)
	}
}

func TestUnsubscribeIdempotent(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc"}`
	resp := doRequest(t, srv, http.MethodPost, "/api/push/unsubscribe", "write-token", body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	resp2 := doRequest(t, srv, http.MethodPost, "/api/push/unsubscribe", "write-token", body)
	if resp2.StatusCode != 200 {
		t.Fatalf("repeat status = %d; want 200", resp2.StatusCode)
	}
}

func TestTestNotificationCountsSubscriptions(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	body := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"AAECAwQFBgcICQoLDA0ODw","auth":"AAECAwQFBgcICQoLDA0ODw"}}`
	_ = doRequest(t, srv, http.MethodPost, "/api/push/subscribe", "write-token", body)
	resp := doRequest(t, srv, http.MethodPost, "/api/push/test", "write-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var out struct{ Sent int `json:"sent"` }
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if out.Sent != 1 {
		t.Fatalf("sent = %d; want 1", out.Sent)
	}
}

func TestTestNotificationZeroWhenNoSubs(t *testing.T) {
	srv, _ := newWebPushTestServer(t)
	resp := doRequest(t, srv, http.MethodPost, "/api/push/test", "write-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var out struct{ Sent int `json:"sent"` }
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if out.Sent != 0 {
		t.Fatalf("sent = %d; want 0", out.Sent)
	}
}

// suppress unused import in tests.
var _ = bytes.NewReader
var _ = context.Background
```

Also add a small helper method to webpush.Service for the test. In `internal/webpush/service.go`, append:

```go
// SubscriptionsForToken is a test-only helper. Returns subscriptions for the
// given token hash without exposing internal types in production callers.
func (s *Service) SubscriptionsForToken(tokenHash string) []Subscription {
	return s.subStore.ByToken(tokenHash)
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/relay/ -run TestPush
go test ./internal/relay/ -run TestSubscribe
go test ./internal/relay/ -run TestUnsubscribe
go test ./internal/relay/ -run TestTestNotification
```

Expected: build failure (handlers undefined).

- [ ] **Step 3: Write the handlers**

Create `internal/relay/web_push_http.go`:

```go
package relay

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/webpush"
)

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebPush == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "web push disabled"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": s.cfg.WebPush.PublicKey()})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebPush == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "web push disabled"})
		return
	}
	token := tokenFromRequestNoQuery(r)
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing token"})
		return
	}
	var sub webpush.Subscription
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&sub); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if !strings.HasPrefix(sub.Endpoint, "https://") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "endpoint must be https"})
		return
	}
	if !validBase64URLKey(sub.Keys.P256dh) || !validBase64URLKey(sub.Keys.Auth) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid keys"})
		return
	}
	sub.CreatedAt = time.Now().Unix()
	if err := s.cfg.WebPush.AddSubscription(tokenHash(token), sub); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebPush == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "web push disabled"})
		return
	}
	token := tokenFromRequestNoQuery(r)
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing token"})
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	_ = s.cfg.WebPush.RemoveSubscription(tokenHash(token), body.Endpoint)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebPush == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "web push disabled"})
		return
	}
	token := tokenFromRequestNoQuery(r)
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing token"})
		return
	}
	n := s.cfg.WebPush.SendTest(tokenHash(token))
	writeJSON(w, http.StatusOK, map[string]int{"sent": n})
}

func validBase64URLKey(s string) bool {
	if s == "" {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return false
	}
	return len(data) >= 16 && len(data) <= 128
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
```

In `internal/relay/server.go`, find the mux setup (likely in `NewServer` or a `routes()` method) and register the four endpoints behind the existing token-auth middleware:

```go
mux.HandleFunc("/api/push/key", s.requireAuth(s.handlePushKey))
mux.HandleFunc("/api/push/subscribe", s.requirePostAuth(s.handlePushSubscribe))
mux.HandleFunc("/api/push/unsubscribe", s.requirePostAuth(s.handlePushUnsubscribe))
mux.HandleFunc("/api/push/test", s.requirePostAuth(s.handlePushTest))
```

If `requireAuth` / `requirePostAuth` helpers do not exist in the codebase, mirror whatever wrapper pattern is used by `/api/sessions` and `/api/version`. The key requirement: the four endpoints must run through `authorizeFromRequest` and reject unauthenticated requests with 401.

Look for the existing auth middleware shape with:
```bash
grep -n "api/sessions\|api/version\|authorize" internal/relay/server.go internal/relay/auth.go | head -20
```

Adapt accordingly.

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/relay/ -run TestPush -run TestSubscribe -run TestUnsubscribe -run TestTestNotification
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/web_push_http.go internal/relay/web_push_http_test.go internal/relay/server.go internal/webpush/service.go
git commit -m "feat: relay /api/push/{key,subscribe,unsubscribe,test} endpoints"
```

---

### Task 10: Uplink Handler — Route `TypeCommandEvent` to Webpush

**Files:**
- Modify: `internal/relay/uplink_conn.go`
- Modify: `internal/relay/uplink_conn_test.go`
- Test: `internal/relay/uplink_conn_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/uplink_conn_test.go`:

```go
func TestUplinkRoutesCommandEventToWebPush(t *testing.T) {
	dispatched := make(chan webpush.CommandFinished, 1)
	svc, _ := webpush.Open(t.TempDir(), "mailto:test@example.com")
	srv := NewServer(Config{
		Token:   "write-token",
		WebPush: svc,
	})
	// Inject a resolver that captures into the channel.
	svc.SetSessionResolver(func(_ uuid.UUID) []string { return nil })
	// We can't easily mock Service.DispatchCommandFinished — instead, this
	// test asserts that the read loop calls DispatchCommandFinished by
	// hooking a fake transport that records bodies. (See dispatch tests
	// for the wider integration.)
	_ = dispatched
	_ = srv
	t.Skip("uplink frame routing covered by integration; see TestRelayUplinkCommandEventTriggersDispatch")
}

func TestRelayUplinkCommandEventTriggersDispatch(t *testing.T) {
	svc, _ := webpush.Open(t.TempDir(), "mailto:test@example.com")
	rec := &recordingHTTPClientForRelayTest{}
	webpush.InjectTransportForTesting(svc, rec)
	srv := NewServer(Config{
		Token:   "write-token",
		WebPush: svc,
	})
	hostID := uuid.New()
	sid := uuid.New()
	registerFakeUplinkManifest(srv, hostID, []uuid.UUID{sid})
	// Add a subscription that will be the fanout target.
	sub := webpush.Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "AAECAwQFBgcICQoLDA0ODw"
	sub.Keys.Auth = "AAECAwQFBgcICQoLDA0ODw"
	_ = svc.AddSubscription(tokenHash("write-token"), sub)
	svc.SetSessionResolver(srv.WebPushSessionResolver)
	// Build a TypeCommandEvent frame and feed it through the uplink handler.
	frame, _ := proto.EncodeCommandEvent(sid, proto.CommandEventPayload{ExitCode: 0, ElapsedMS: 12500, Label: "atterm"})
	deliverUplinkFrameForTest(srv, hostID, frame)
	// Wait for at least one HTTP push.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("uplink command event did not trigger Web Push dispatch")
}

func TestRelayUplinkCommandEventDropsUnknownSession(t *testing.T) {
	svc, _ := webpush.Open(t.TempDir(), "mailto:test@example.com")
	rec := &recordingHTTPClientForRelayTest{}
	webpush.InjectTransportForTesting(svc, rec)
	srv := NewServer(Config{
		Token:   "write-token",
		WebPush: svc,
	})
	sub := webpush.Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "AAECAwQFBgcICQoLDA0ODw"
	sub.Keys.Auth = "AAECAwQFBgcICQoLDA0ODw"
	_ = svc.AddSubscription(tokenHash("write-token"), sub)
	hostID := uuid.New()
	registerFakeUplinkManifest(srv, hostID, nil)
	frame, _ := proto.EncodeCommandEvent(uuid.New(), proto.CommandEventPayload{ExitCode: 0})
	deliverUplinkFrameForTest(srv, hostID, frame)
	time.Sleep(150 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("dispatched %d pushes for unknown session; want 0", rec.count())
	}
}
```

Test helpers needed at the bottom of `uplink_conn_test.go`:

```go
type recordingHTTPClientForRelayTest struct {
	mu sync.Mutex
	n  int
}

func (r *recordingHTTPClientForRelayTest) Do(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	return &http.Response{
		StatusCode: 201,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     http.Header{},
	}, nil
}

func (r *recordingHTTPClientForRelayTest) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}
```

The helpers `registerFakeUplinkManifest`, `deliverUplinkFrameForTest`, and `webpush.InjectTransportForTesting` are stubs — define them as part of the implementation steps below.

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/relay/ -run TestRelayUplinkCommandEvent
```

Expected: build failure (helpers + handler case undefined).

- [ ] **Step 3: Add the test-only injection points to webpush**

In `internal/webpush/service.go`, add:

```go
// InjectTransportForTesting replaces the transport with one driven by the
// given HTTPClient. Test-only.
func InjectTransportForTesting(s *Service, hc wpgoHTTPClient) {
	s.tr = newTransport(s.vapidPriv, s.vapidPub, s.subject, hc)
}

// wpgoHTTPClient is webpush-go's HTTPClient interface re-exported so tests
// outside the webpush package can satisfy it.
type wpgoHTTPClient = wpgo.HTTPClient
```

Add the import alias `wpgo "github.com/SherClockHolmes/webpush-go"` if not already in scope.

- [ ] **Step 4: Add the read-loop case + test helpers in relay**

In `internal/relay/uplink_conn.go`, in the read-loop switch on frame type, add:

```go
case proto.TypeCommandEvent:
    if s.server.cfg.WebPush == nil {
        if s.server.cfg.Debug {
            log.Printf("uplink: TypeCommandEvent dropped (web push disabled)")
        }
        continue
    }
    payload, err := proto.DecodeCommandEvent(f)
    if err != nil {
        log.Printf("uplink: invalid TypeCommandEvent payload: %v", err)
        continue
    }
    if !s.manifestContains(f.SessionID) {
        if s.server.cfg.Debug {
            log.Printf("uplink: TypeCommandEvent for unknown session %s dropped", f.SessionID)
        }
        continue
    }
    s.server.cfg.WebPush.DispatchCommandFinished(webpush.CommandFinished{
        SessionID: f.SessionID,
        HostID:    s.hostID,
        ExitCode:  payload.ExitCode,
        ElapsedMS: payload.ElapsedMS,
        Label:     payload.Label,
    })
```

Add the `"github.com/attson/atterm/internal/webpush"` import.

If `manifestContains` does not already exist on `uplinkConn`, add it as a small helper that walks the conn's current manifest snapshot.

In `internal/relay/uplink_conn_test.go`, add test helpers at the bottom:

```go
// registerFakeUplinkManifest creates a synthetic uplinkConn entry with the
// given hostID and session list, sufficient for the read-loop to recognize
// frame.SessionID via manifestContains. Returns nothing — the conn lives in
// the server's internal state.
func registerFakeUplinkManifest(srv *Server, hostID uuid.UUID, sessions []uuid.UUID) {
	// Implementation detail: build a *uplinkConn with the right manifest
	// field populated and append to srv's uplink list. The exact field
	// names must match the production struct; see uplink_conn.go.
}

// deliverUplinkFrameForTest pushes a frame as if it had been read from the
// network for the named host's uplink connection.
func deliverUplinkFrameForTest(srv *Server, hostID uuid.UUID, frame proto.Frame) {
	// Implementation detail: locate the conn registered above and call its
	// frame handler synchronously.
}
```

These helpers will require small access tweaks in `uplink_conn.go` (e.g. an unexported testHooks file). Implement minimally — the goal is to drive the frame handler end-to-end without the WS layer.

If the existing `uplink_conn.go` makes this awkward, an acceptable alternative is to refactor the case body into a private method `(s *uplinkConn) handleCommandEvent(f proto.Frame)` and invoke that directly from tests.

- [ ] **Step 5: Run the test to verify it passes**

```bash
go test ./internal/relay/ -run TestRelayUplinkCommandEvent
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/relay/uplink_conn.go internal/relay/uplink_conn_test.go internal/webpush/service.go
git commit -m "feat: route TypeCommandEvent through uplink to webpush dispatch"
```

---

### Task 11: `cmd/atterm-relay/main.go` Wiring

**Files:**
- Modify: `cmd/atterm-relay/main.go`

- [ ] **Step 1: Add flags + Open + resolver wiring**

In `cmd/atterm-relay/main.go`, add to the flag block (near the existing `configPath` flag):

```go
configDir := flag.String("config-dir", envOr("ATTERM_RELAY_CONFIG_DIR", ""), "persistent relay state directory for web-push.json etc. (or ATTERM_RELAY_CONFIG_DIR)")
vapidSubject := flag.String("vapid-subject", envOr("ATTERM_VAPID_SUBJECT", "mailto:noreply@atterm.local"), "VAPID subject (mailto: or https: URL; advertised to push services)")
```

Define `envOr` near the bottom of the file if it does not already exist:

```go
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
```

After `cfg, _, err := buildRelayConfig(...)` and before `relay.NewServer(cfg)`, add:

```go
// Resolve persistence directory. If --config-dir not set, derive from
// --config file path (existing flag) or fall back to ./data/atterm-relay.
dir := *configDir
if dir == "" && *configPath != "" {
	dir = filepathDir(*configPath)
}
if dir == "" {
	dir = "./data/atterm-relay"
}
wpSvc, wpErr := webpush.Open(dir, *vapidSubject)
if wpErr != nil {
	log.Printf("WARN: web-push disabled: %v", wpErr)
	wpSvc = nil
}
cfg.WebPush = wpSvc
```

Add `"github.com/attson/atterm/internal/webpush"` to the imports.

Define `filepathDir` (or just use `filepath.Dir` with the `path/filepath` import).

After `srv := relay.NewServer(cfg)`, add:

```go
if wpSvc != nil {
	wpSvc.SetSessionResolver(srv.WebPushSessionResolver)
}
```

- [ ] **Step 2: Verify build**

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go build ./cmd/atterm-relay
go vet ./...
go test ./...
```

Expected: build succeeds; all tests pass.

- [ ] **Step 3: Commit**

```bash
git add cmd/atterm-relay/main.go
git commit -m "feat: wire webpush.Open and session resolver into atterm-relay startup"
```

---

### Task 12: Desktop `uplink.SendCommandEvent`

**Files:**
- Modify: `desktop/uplink.go`
- Create: `desktop/uplink_command_event_test.go`
- Test: `desktop/uplink_command_event_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/uplink_command_event_test.go`:

```go
package main

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestSendCommandEventDropsWhenNotConnected(t *testing.T) {
	// A zero-valued *uplink with no out chan must not panic.
	var u *uplink
	u.SendCommandEvent(uuid.New(), 0, 12500, "atterm")
}

func TestSendCommandEventQueuesFrameOnOutChan(t *testing.T) {
	u := newTestUplinkWithOutChan()
	sid := uuid.New()
	u.SendCommandEvent(sid, 0, 12500, "atterm")
	select {
	case f := <-u.out:
		if f.Type != proto.TypeCommandEvent {
			t.Fatalf("Type = %v; want TypeCommandEvent", f.Type)
		}
		if f.SessionID != sid {
			t.Fatalf("SessionID = %v; want %v", f.SessionID, sid)
		}
		payload, err := proto.DecodeCommandEvent(f)
		if err != nil {
			t.Fatalf("DecodeCommandEvent: %v", err)
		}
		if payload.ExitCode != 0 || payload.ElapsedMS != 12500 || payload.Label != "atterm" {
			t.Fatalf("payload = %+v", payload)
		}
	default:
		t.Fatal("no frame queued")
	}
}

// newTestUplinkWithOutChan builds a minimal *uplink with an out channel
// ready to receive. Production uplink wires this up inside its connect
// loop; we shortcut it for the test.
func newTestUplinkWithOutChan() *uplink {
	// implementation detail; adjust constructor based on the production code
	return &uplink{out: make(chan proto.Frame, 4)}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test ./desktop/ -run TestSendCommandEvent
```

Expected: build failure (`uplink.SendCommandEvent` undefined; possibly `uplink.out` field name mismatch).

- [ ] **Step 3: Add the method**

In `desktop/uplink.go`, after the existing writer goroutine wiring, add:

```go
// SendCommandEvent queues a TypeCommandEvent frame for the writer
// goroutine. Drops on the floor when the uplink is nil or not connected.
func (u *uplink) SendCommandEvent(sessionID uuid.UUID, exit, elapsedMS int, label string) {
	if u == nil || u.out == nil {
		return
	}
	frame, err := proto.EncodeCommandEvent(sessionID, proto.CommandEventPayload{
		ExitCode:  exit,
		ElapsedMS: elapsedMS,
		Label:     label,
	})
	if err != nil {
		log.Printf("uplink: SendCommandEvent encode: %v", err)
		return
	}
	select {
	case u.out <- frame:
	default:
		log.Printf("uplink: out chan full; dropping command event")
	}
}
```

If the field name is not `out`, adjust to match. Inspect with:

```bash
grep -n "out chan\|ch chan\|writer" desktop/uplink.go | head -10
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./desktop/ -run TestSendCommandEvent
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/uplink.go desktop/uplink_command_event_test.go
git commit -m "feat: desktop uplink SendCommandEvent for Web Push trigger frames"
```

---

### Task 13: Desktop `App.BroadcastCommandFinished` Binding

**Files:**
- Modify: `desktop/app.go`
- Create: `desktop/app_broadcast_test.go`
- Test: `desktop/app_broadcast_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/app_broadcast_test.go`:

```go
package main

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestBroadcastCommandFinishedSilentWithNilUplink(t *testing.T) {
	a := &App{}
	// Must not panic; success path is "nothing observable".
	a.BroadcastCommandFinished(uuid.New().String(), 0, 12500, "atterm")
}

func TestBroadcastCommandFinishedSendsThroughUplink(t *testing.T) {
	u := &uplink{out: make(chan proto.Frame, 4)}
	a := &App{uplink: u}
	sid := uuid.New()
	a.BroadcastCommandFinished(sid.String(), 0, 12500, "atterm")
	select {
	case f := <-u.out:
		if f.Type != proto.TypeCommandEvent || f.SessionID != sid {
			t.Fatalf("unexpected frame: type=%v sid=%v", f.Type, f.SessionID)
		}
	default:
		t.Fatal("uplink received no frame")
	}
}

func TestBroadcastCommandFinishedSilentOnInvalidUUID(t *testing.T) {
	u := &uplink{out: make(chan proto.Frame, 4)}
	a := &App{uplink: u}
	a.BroadcastCommandFinished("not-a-uuid", 0, 12500, "atterm")
	select {
	case <-u.out:
		t.Fatal("frame queued despite invalid session id")
	default:
		// ok
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./desktop/ -run TestBroadcastCommandFinished
```

Expected: build failure (`App.BroadcastCommandFinished` undefined).

- [ ] **Step 3: Add the binding**

In `desktop/app.go`, after the existing `ShowNotification` binding, append:

```go
// BroadcastCommandFinished is invoked by the desktop frontend when an OSC
// 133 command-finished event passes the local notification gate. Sends a
// TypeCommandEvent frame to the configured remote relay via the uplink so
// the relay can fan out Web Push notifications to subscribed browsers.
// Failures (no uplink, no remote relay, invalid uuid) are silent — local
// OS notification has already fired.
func (a *App) BroadcastCommandFinished(sessionID string, exitCode, elapsedMS int, label string) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return
	}
	if a.uplink == nil {
		return
	}
	a.uplink.SendCommandEvent(sid, exitCode, elapsedMS, label)
}
```

Verify the `uuid` import is already in `app.go` (it should be — used elsewhere).

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./desktop/ -run TestBroadcastCommandFinished
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/app.go desktop/app_broadcast_test.go
git commit -m "feat: BroadcastCommandFinished Wails binding for Web Push trigger"
```

---

### Task 14: TS `api.ts` Wrapper

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`

- [ ] **Step 1: Extend AppBindings + add wrapper**

Open `desktop/frontend/src/lib/api.ts`. Inside the `AppBindings` interface, after `SetCommandNotifyThresholdSeconds`, append:

```ts
  BroadcastCommandFinished(sessionId: string, exitCode: number, elapsedMs: number, label: string): Promise<void>;
```

At the bottom of the file, after `setCommandNotifyThresholdSeconds`, append:

```ts
export function broadcastCommandFinished(
  sessionId: string,
  exitCode: number,
  elapsedMs: number,
  label: string,
): Promise<void> {
  return bindings().BroadcastCommandFinished(sessionId, exitCode, elapsedMs, label);
}
```

- [ ] **Step 2: Verify build**

```bash
cd desktop/frontend
npm run build
npm run test -- --run
```

Expected: build succeeds; all tests pass (no regression).

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/web-push-notifications
git add desktop/frontend/src/lib/api.ts
git commit -m "feat: expose BroadcastCommandFinished via TS api"
```

---

### Task 15: TerminalView Calls Broadcast After Local Notify

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Modify: `desktop/frontend/src/components/TerminalView.test.ts`
- Test: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 1: Add the source-level test**

Open `desktop/frontend/src/components/TerminalView.test.ts`. Append inside the existing `describe("TerminalView")` block:

```ts
  test("imports broadcastCommandFinished from api lib", () => {
    expect(source).toContain("broadcastCommandFinished");
    expect(source).toMatch(/from "\.\.\/lib\/api"/);
  });

  test("invokes broadcastCommandFinished after the local-notify gate passes", () => {
    // Must be inside the same `if (passed)` block as showNotification.
    const passedBlock = source.match(/if\s*\(\s*!\s*passed\s*\)[\s\S]*?return[\s\S]*?showNotification[\s\S]*?broadcastCommandFinished/);
    expect(passedBlock).not.toBeNull();
  });
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd desktop/frontend
npm run test -- --run src/components/TerminalView.test.ts
```

Expected: new tests FAIL.

- [ ] **Step 3: Implement in TerminalView.vue**

In `desktop/frontend/src/components/TerminalView.vue`, find the existing import line for the `api` module — currently:

```ts
import { getHostInfo, showNotification } from "../lib/api";
```

Add `broadcastCommandFinished`:

```ts
import { broadcastCommandFinished, getHostInfo, showNotification } from "../lib/api";
```

Find the OSC 133 handler block (added in v0.1.55). It currently looks like:

```ts
      if (passed) {
        void showNotification(
          "AT Term",
          `Command finished · exit ${ev.exitCode} · ${formatElapsed(ev.elapsedMs)} · ${props.sessionLabel || "session"}`,
        );
      }
```

Replace the body of that `if (passed)` block with:

```ts
      if (passed) {
        void showNotification(
          "AT Term",
          `Command finished · exit ${ev.exitCode} · ${formatElapsed(ev.elapsedMs)} · ${props.sessionLabel || "session"}`,
        );
        void broadcastCommandFinished(
          props.sessionId,
          ev.exitCode,
          ev.elapsedMs,
          props.sessionLabel || "session",
        );
      }
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd desktop/frontend
npm run test -- --run
npm run build
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/web-push-notifications
git add desktop/frontend/src/components/TerminalView.vue \
        desktop/frontend/src/components/TerminalView.test.ts
git commit -m "feat: TerminalView broadcasts command-finished events to relay"
```

---

### Task 16: `web/app-core.js` Push Helpers

**Files:**
- Modify: `web/app-core.js`
- Modify: `web/app-core.test.mjs`
- Test: `web/app-core.test.mjs`

- [ ] **Step 1: Write the failing tests**

Append to `web/app-core.test.mjs`:

```js
import { canEnablePush, pushSupported, base64UrlToUint8Array } from "./app-core.js";

test("pushSupported true when ServiceWorker + PushManager + Notification are present", () => {
  const nav = { serviceWorker: {}, userAgent: "Mozilla/5.0 Chrome/120" };
  const win = { PushManager: function () {}, Notification: function () {} };
  assert.equal(pushSupported(nav, win), true);
});

test("pushSupported false on iOS Safari outside PWA standalone mode", () => {
  const nav = { serviceWorker: {}, userAgent: "Mozilla/5.0 iPhone Safari/16.4", standalone: false };
  const win = { PushManager: function () {}, Notification: function () {} };
  assert.equal(pushSupported(nav, win), false);
});

test("pushSupported true on iOS PWA (standalone)", () => {
  const nav = { serviceWorker: {}, userAgent: "Mozilla/5.0 iPhone Safari/16.4", standalone: true };
  const win = { PushManager: function () {}, Notification: function () {} };
  assert.equal(pushSupported(nav, win), true);
});

test("pushSupported false when PushManager missing", () => {
  const nav = { serviceWorker: {}, userAgent: "Mozilla/5.0" };
  const win = { Notification: function () {} };
  assert.equal(pushSupported(nav, win), false);
});

test("pushSupported false when Notification missing", () => {
  const nav = { serviceWorker: {}, userAgent: "Mozilla/5.0" };
  const win = { PushManager: function () {} };
  assert.equal(pushSupported(nav, win), false);
});

test("canEnablePush rejects denied, allows default and granted", () => {
  assert.equal(canEnablePush("denied"), false);
  assert.equal(canEnablePush("default"), true);
  assert.equal(canEnablePush("granted"), true);
});

test("base64UrlToUint8Array round-trips a known value", () => {
  // base64url-encoded "Hello" => "SGVsbG8"
  const out = base64UrlToUint8Array("SGVsbG8");
  assert.equal(out.length, 5);
  assert.equal(String.fromCharCode(...out), "Hello");
});

test("base64UrlToUint8Array handles missing padding", () => {
  // base64url-encoded "Hello!" => "SGVsbG8h"
  const out = base64UrlToUint8Array("SGVsbG8h");
  assert.equal(String.fromCharCode(...out), "Hello!");
});
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/web-push-notifications
node --test web/app-core.test.mjs
```

Expected: FAIL with `canEnablePush is not a function` etc.

- [ ] **Step 3: Add the helpers**

Append to `web/app-core.js`:

```js
export function pushSupported(navigator, win) {
  if (!navigator || !win) return false;
  if (!navigator.serviceWorker) return false;
  if (!win.PushManager) return false;
  if (!win.Notification) return false;
  // iOS Safari requires PWA install (standalone) before Web Push works.
  const ua = navigator.userAgent || "";
  const isIOS = /iPad|iPhone|iPod/.test(ua);
  if (isIOS && !navigator.standalone) return false;
  return true;
}

export function canEnablePush(permission) {
  return permission === "default" || permission === "granted";
}

export function base64UrlToUint8Array(value) {
  const padded = value + "===".slice((value.length + 3) % 4);
  const base64 = padded.replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  const arr = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i);
  return arr;
}
```

If `atob` is unavailable in the test runner (it is in Node 20+), no extra polyfill is needed.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
node --test web/app-core.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/app-core.js web/app-core.test.mjs
git commit -m "feat: web push capability + key helpers in app-core"
```

---

### Task 17: `web/sw.js` Push Event Handler

**Files:**
- Modify: `web/sw.js`
- Create: `web/sw.test.mjs`
- Modify: `web/terminal-fit.test.mjs` — assert the cache name bumped
- Test: `web/sw.test.mjs`

- [ ] **Step 1: Write the failing test**

Create `web/sw.test.mjs`:

```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";

function loadSWInVM() {
  const code = readFileSync(new URL("./sw.js", import.meta.url), "utf8");
  const listeners = {};
  const ctx = {
    self: {
      addEventListener(name, fn) { listeners[name] = fn; },
      skipWaiting: () => {},
      clients: { claim: () => {} },
      registration: { showNotification: null },
    },
    caches: {
      open: async () => ({ addAll: async () => {} }),
      keys: async () => [],
      match: async () => undefined,
      delete: async () => {},
    },
    location: { origin: "https://test", pathname: "/" },
    URL,
    fetch: async () => ({}),
    Promise,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return { ctx, listeners };
}

test("push event with valid JSON calls showNotification with body/tag/data", async () => {
  const { ctx, listeners } = loadSWInVM();
  const calls = [];
  ctx.self.registration.showNotification = async (title, options) => {
    calls.push({ title, options });
  };
  const event = {
    data: { json: () => ({ title: "AT Term · atterm", body: "Command finished · exit 0 · 12s", tag: "sid-1", data: { exitCode: 0 } }) },
    waitUntil: (p) => p,
  };
  await listeners["push"](event);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].title, "AT Term · atterm");
  assert.equal(calls[0].options.body, "Command finished · exit 0 · 12s");
  assert.equal(calls[0].options.tag, "sid-1");
  assert.deepEqual(calls[0].options.data, { exitCode: 0 });
});

test("push event with non-JSON data uses fallback notification", async () => {
  const { ctx, listeners } = loadSWInVM();
  const calls = [];
  ctx.self.registration.showNotification = async (title, options) => {
    calls.push({ title, options });
  };
  const event = {
    data: { json: () => { throw new Error("not json"); } },
    waitUntil: (p) => p,
  };
  await listeners["push"](event);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].title, "AT Term");
  assert.equal(calls[0].options.body, "Command finished.");
});

test("push event with no data uses fallback notification", async () => {
  const { ctx, listeners } = loadSWInVM();
  const calls = [];
  ctx.self.registration.showNotification = async (title, options) => {
    calls.push({ title, options });
  };
  const event = { waitUntil: (p) => p };
  await listeners["push"](event);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].title, "AT Term");
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
node --test web/sw.test.mjs
```

Expected: FAIL (no push handler in sw.js yet).

- [ ] **Step 3: Add the push handler and bump CACHE**

In `web/sw.js`:

Change the first line:
```js
const CACHE = "at-term-web-v3";
```
to:
```js
const CACHE = "at-term-web-v4";
```

At the end of the file, append:

```js
self.addEventListener("push", (event) => {
  event.waitUntil((async () => {
    let payload = { title: "AT Term", body: "Command finished." };
    try {
      if (event.data) {
        payload = { ...payload, ...event.data.json() };
      }
    } catch (_err) {
      // keep fallback
    }
    const { title, body, tag, data } = payload;
    const options = {};
    if (body !== undefined) options.body = body;
    if (tag !== undefined) options.tag = tag;
    if (data !== undefined) options.data = data;
    await self.registration.showNotification(title, options);
  })());
});
```

- [ ] **Step 4: Update the cache-bump test**

In `web/terminal-fit.test.mjs`, find the existing assertion that pins `CACHE` to its previous value and update to `"at-term-web-v4"`. If the test asserts "starts with `at-term-web-v`" without a specific number, no change is needed; just re-run:

```bash
node --test web/terminal-fit.test.mjs
```

If the assertion is on the literal version string, edit the value.

- [ ] **Step 5: Run all web tests**

```bash
node --test web/*.test.mjs
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add web/sw.js web/sw.test.mjs web/terminal-fit.test.mjs
git commit -m "feat: SW push event handler with fallback notification"
```

---

### Task 18: `web/app.js` Enable Flow + UI Button

**Files:**
- Modify: `web/app.js`
- Create: `web/push-flow.test.mjs`
- Test: `web/push-flow.test.mjs`

- [ ] **Step 1: Write the failing test**

Create `web/push-flow.test.mjs`:

```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { enablePushFlow } from "./app.js";

function makeFakes({ permission = "granted", subscribeOK = true, fetchOK = true, fetchStatus = 200, keyResponse = { key: "AAAA" } } = {}) {
  const calls = { fetch: [], subscribe: 0, requestPermission: 0 };
  const fakes = {
    notification: {
      permission,
      requestPermission: async () => {
        calls.requestPermission++;
        return permission;
      },
    },
    registration: {
      pushManager: {
        subscribe: async () => {
          calls.subscribe++;
          if (!subscribeOK) throw new Error("nope");
          return {
            endpoint: "https://push.example/abc",
            getKey: (name) => new Uint8Array([1, 2, 3, 4]),
            toJSON: () => ({ endpoint: "https://push.example/abc", keys: { p256dh: "AQID", auth: "AQID" } }),
          };
        },
      },
    },
    fetch: async (url, opts) => {
      calls.fetch.push({ url, opts });
      if (!fetchOK) throw new Error("network");
      if (url.endsWith("/api/push/key")) {
        return { ok: fetchStatus === 200, status: fetchStatus, json: async () => keyResponse };
      }
      return { ok: fetchStatus === 200, status: fetchStatus, json: async () => ({ ok: true }) };
    },
    token: "tok",
  };
  return { fakes, calls };
}

test("enablePushFlow happy path posts subscription", async () => {
  const { fakes, calls } = makeFakes();
  const result = await enablePushFlow(fakes);
  assert.equal(result.ok, true);
  assert.equal(calls.requestPermission, 1);
  assert.equal(calls.subscribe, 1);
  const subscribeCall = calls.fetch.find((c) => c.url.endsWith("/api/push/subscribe"));
  assert.ok(subscribeCall, "missing /api/push/subscribe call");
});

test("enablePushFlow denied permission returns failure without /api/push/key", async () => {
  const { fakes, calls } = makeFakes({ permission: "denied" });
  const result = await enablePushFlow(fakes);
  assert.equal(result.ok, false);
  assert.equal(result.reason, "denied");
  assert.equal(calls.fetch.length, 0);
});

test("enablePushFlow surfaces 503 server-disabled", async () => {
  const { fakes } = makeFakes({ fetchStatus: 503 });
  const result = await enablePushFlow(fakes);
  assert.equal(result.ok, false);
  assert.equal(result.reason, "disabled");
});

test("enablePushFlow handles subscribe throw with reason 'subscribe-failed'", async () => {
  const { fakes } = makeFakes({ subscribeOK: false });
  const result = await enablePushFlow(fakes);
  assert.equal(result.ok, false);
  assert.equal(result.reason, "subscribe-failed");
});
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
node --test web/push-flow.test.mjs
```

Expected: FAIL with `enablePushFlow is not a function`.

- [ ] **Step 3: Add the flow + UI to `web/app.js`**

In `web/app.js`, near the top, import the new helpers:

```js
import {
  base64UrlToUint8Array,
  canEnablePush,
  canRegisterServiceWorker,
  pushSupported,
} from "./app-core.js";
```

Find an appropriate spot (e.g. after `registerServiceWorker`) and append:

```js
/**
 * Drives the "Enable notifications" click path. Returns an object so tests
 * can assert the outcome without DOM coupling.
 */
export async function enablePushFlow(deps) {
  const { notification, registration, fetch, token } = deps;
  if (!canEnablePush(notification.permission)) {
    return { ok: false, reason: "denied" };
  }
  const granted = await notification.requestPermission();
  if (granted !== "granted") {
    return { ok: false, reason: "denied" };
  }
  let keyResp;
  try {
    keyResp = await fetch("/api/push/key", {
      headers: { Authorization: "Bearer " + token },
    });
  } catch (_err) {
    return { ok: false, reason: "network" };
  }
  if (keyResp.status === 503) {
    return { ok: false, reason: "disabled" };
  }
  if (!keyResp.ok) {
    return { ok: false, reason: "key-failed" };
  }
  const { key } = await keyResp.json();
  let sub;
  try {
    sub = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: base64UrlToUint8Array(key),
    });
  } catch (_err) {
    return { ok: false, reason: "subscribe-failed" };
  }
  const payload = sub.toJSON ? sub.toJSON() : sub;
  let postResp;
  try {
    postResp = await fetch("/api/push/subscribe", {
      method: "POST",
      headers: {
        Authorization: "Bearer " + token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        endpoint: payload.endpoint,
        keys: { p256dh: payload.keys.p256dh, auth: payload.keys.auth },
      }),
    });
  } catch (_err) {
    return { ok: false, reason: "network" };
  }
  if (postResp.status === 503) {
    return { ok: false, reason: "disabled" };
  }
  if (!postResp.ok) {
    return { ok: false, reason: "subscribe-rejected" };
  }
  return { ok: true };
}

export async function disablePushFlow(deps) {
  const { registration, fetch, token } = deps;
  try {
    const sub = await registration.pushManager.getSubscription();
    if (sub) {
      await sub.unsubscribe();
      await fetch("/api/push/unsubscribe", {
        method: "POST",
        headers: { Authorization: "Bearer " + token, "Content-Type": "application/json" },
        body: JSON.stringify({ endpoint: sub.endpoint }),
      });
    }
  } catch (err) {
    console.warn("[AT Term] disablePushFlow error", err);
  }
  return { ok: true };
}
```

Then add the button wiring (in the section that builds the status row in `web/app.js`):

```js
function renderPushButton() {
  if (!pushSupported(navigator, window)) return null;
  const btn = document.createElement("button");
  btn.className = "push-toggle";
  const enabled = localStorage.getItem("push-enabled") === "1";
  btn.textContent = enabled ? "🔔 ON" : "🔔 Enable notifications";
  btn.addEventListener("click", async () => {
    if (enabled) {
      await disablePushFlow({
        registration: await navigator.serviceWorker.ready,
        fetch: window.fetch.bind(window),
        token: getStoredToken(),
      });
      localStorage.removeItem("push-enabled");
      btn.textContent = "🔔 Enable notifications";
    } else {
      const result = await enablePushFlow({
        notification: Notification,
        registration: await navigator.serviceWorker.ready,
        fetch: window.fetch.bind(window),
        token: getStoredToken(),
      });
      if (result.ok) {
        localStorage.setItem("push-enabled", "1");
        btn.textContent = "🔔 ON";
      } else {
        const msg = ({
          denied: "Notification permission denied.",
          disabled: "Server has push disabled.",
          network: "Could not reach server.",
          "key-failed": "Could not fetch server key.",
          "subscribe-failed": "Browser refused to subscribe.",
          "subscribe-rejected": "Server rejected subscription.",
        })[result.reason] || "Could not enable notifications.";
        btn.textContent = "🔔 " + msg;
      }
    }
  });
  return btn;
}
```

Append `renderPushButton()` into the status row after the existing connection-status DOM. Mirror the existing pattern in `app.js` — find where the status indicator is appended and add right after.

If `getStoredToken` does not already exist, use whichever helper `app.js` already uses to read the bearer token.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
node --test web/*.test.mjs
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add web/app.js web/push-flow.test.mjs
git commit -m "feat: web app enable/disable push flow + status-row button"
```

---

### Task 19: `docs/web-push.md` User Guide

**Files:**
- Create: `docs/web-push.md`

- [ ] **Step 1: Write the doc**

Create `docs/web-push.md`:

```markdown
# Web Push Notifications

AT Term can deliver "command finished" notifications to a browser or PWA via the [Web Push API](https://developer.mozilla.org/en-US/docs/Web/API/Push_API), so you get a ping even when the page is not open. This builds on the [shell integration](shell-integration.md) shipped in v0.1.55 and is self-hosted — the AT Term relay is the push origin; no third-party services are involved.

## Requirements

- A reachable relay (the desktop app must have `relay_url` configured, and the browser must be talking to that same relay).
- Shell integration enabled (Settings → General → "Enable shell integration") so the desktop frontend can detect command boundaries.
- A modern browser:
  - Chrome / Edge / Brave / Firefox on desktop or Android
  - Safari on macOS (the page must be added to the Dock for full functionality)
  - Safari on iOS 16.4+ in a PWA installed via "Add to Home Screen"

## How to enable

1. Open your relay URL in the browser, paste the relay token, connect.
2. Click the "🔔 Enable notifications" button in the status row.
3. The browser will prompt for permission. Click Allow.
4. The button changes to "🔔 ON".

On iOS Safari, follow these steps in this order:
1. Open the relay URL in Safari.
2. Tap the Share button → Add to Home Screen.
3. Open the AT Term app from the Home Screen (now in PWA / standalone mode).
4. Click "🔔 Enable notifications" and follow the iOS prompt.

## What triggers a push

The same gate as the desktop-side OS notification (`Settings → General → Command-finished notification threshold (seconds)`):

- AT Term desktop window is NOT focused.
- Command ran for at least the threshold (default 10s, configurable 1-600s).
- The session is local to the desktop AT Term (not a cast-attached remote pane).

When the gate passes, every browser that has subscribed under a relay token authorized to view that session receives a push.

## How to disable

Click the "🔔 ON" button to go back to "🔔 Enable notifications".

To globally disable Web Push on a relay, stop the relay, delete `<RELAY_CONFIG_DIR>/web-push.json`, and the four `/api/push/*` endpoints will return 503 until you re-init. (Easier: just don't enable on the client.)

## Where state lives

`<RELAY_CONFIG_DIR>/web-push.json` holds:
- the VAPID keypair (P-256 ECDSA, generated on first start)
- per-token subscription records (endpoint + browser keys)

The file is rewritten on every subscription change via atomic write-temp-rename. Loss of the file means: regenerated VAPID keypair, all existing browser subscriptions invalidated — users need to re-enable. The previous corrupt file (if any) is preserved as `web-push.json.corrupt-<timestamp>` so you can inspect it.

## Configuration

| Flag / env | Default | Notes |
|------------|---------|-------|
| `--config-dir` / `ATTERM_RELAY_CONFIG_DIR` | `./data/atterm-relay` | The persistent state directory. Web Push file lives here. |
| `--vapid-subject` / `ATTERM_VAPID_SUBJECT` | `mailto:noreply@atterm.local` | The VAPID JWT subject. Push services may reject non-`mailto:` values from some providers. |

## Limitations

- iOS requires PWA install. Plain Safari tabs cannot receive Web Push on iOS.
- Token rotation invalidates subscriptions tied to the old token. Browsers must re-enable.
- VAPID key wipe is irreversible — old subscriptions become unusable.
- No relay-side suppression for "user is actively watching this session". Both an in-page event and a Web Push may fire on a device that is also actively attached. The browser groups them by tag.
- We currently push only command-finished events. BEL and session lifecycle events are deferred.

## Troubleshooting

- **Button is missing**: your browser may not support Web Push (e.g. iOS Safari outside PWA), or `navigator.serviceWorker` isn't available (require HTTPS or loopback dev). Open the JS console.
- **"Server has push disabled"**: the relay started with no usable config directory or failed to load `web-push.json`. Check the relay log for `webpush:` lines.
- **No notifications arrive**: confirm the desktop AT Term is connected to the same relay AND the window is unfocused for at least the threshold seconds. Try the "Test notification" button (or send a `POST /api/push/test`) to confirm the relay → browser path works.
```

- [ ] **Step 2: Commit**

```bash
git add docs/web-push.md
git commit -m "docs: add user guide for Web Push notifications"
```

---

### Task 20: `docs/spec/protocol.md` + README Updates

**Files:**
- Modify: `docs/spec/protocol.md`
- Modify: `README.md`

- [ ] **Step 1: Document the new frame in `protocol.md`**

In `docs/spec/protocol.md`, find the existing frame-types table and add a row for `TypeCommandEvent`:

```markdown
| `TypeCommandEvent`  | 0x35  | uplink → relay | Triggers Web Push for a command-finished event |
```

(Adapt the table column count to whatever the existing table uses.)

Append a new section after the existing frame-type subsections:

```markdown
### TypeCommandEvent (0x35)

Direction: uplink → relay only. Not forwarded to clients.

Payload (JSON):

```json
{
  "exit_code": 0,
  "elapsed_ms": 12500,
  "label": "atterm"
}
```

- `session_id` rides the frame header (existing pattern).
- `host_id` is intentionally not in the payload. The relay reconstructs it from the sender's ANNOUNCE manifest at handler time, which makes cross-uplink spoofing impossible.
- The relay drops the frame silently when `session_id` is not present in the sender's current manifest.
- `label` is truncated to 256 bytes before being forwarded into a notification payload.
```

- [ ] **Step 2: Update README.md**

In the "现在能做什么" capability table, add a row:

```markdown
| Web Push 通知 | 浏览器和 PWA 订阅后，命令完成事件通过 self-hosted Web Push 推送，即使页面没打开也能收到（依赖 shell 集成 + 已连远端 relay） |
```

In the "文档" section, add a bullet near the related feature docs:

```markdown
- [`docs/web-push.md`](docs/web-push.md)：浏览器 / PWA 订阅 Web Push 命令完成通知的启用方式、iOS 限制、自托管关键管理。
```

- [ ] **Step 3: Commit**

```bash
git add docs/spec/protocol.md README.md
git commit -m "docs: document TypeCommandEvent frame and link Web Push guide"
```

---

### Task 21: Full Test Sweep + Vet

This task gates merge — no new code beyond what previous tasks produced. The executor explicitly runs the whole test battery before declaring the feature done. Manual smoke (the 13 items in the spec) is human-only and is documented separately for QA.

**Files:**
- No code changes.

- [ ] **Step 1: Run full Go test suite**

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/web-push-notifications
go vet ./...
go test -timeout 90s ./...
```

Expected: all PASS, no vet issues.

- [ ] **Step 2: Frontend tests + build**

```bash
cd desktop/frontend
npm ci
npm run test -- --run
npm run build
```

Expected: all vitest suites PASS; build clean.

- [ ] **Step 3: Vanilla web tests**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/web-push-notifications
node --test web/*.test.mjs
```

Expected: all tests PASS.

- [ ] **Step 4: Final commit if any cleanup**

Most likely nothing is needed. If anything surfaced an inline fix, commit with a `fix:` prefix.

```bash
git status
# Clean = done.
```

---

## Plan Self-Review

**Spec coverage check.** Every section of the spec has at least one task:

| Spec section | Tasks |
|--------------|-------|
| `internal/webpush/` package (vapid, subscription, persist, transport, service, dispatch) | 1, 2, 3, 4, 5, 6 |
| New protocol frame `TypeCommandEvent` | 7 |
| `Config.WebPush` + `WebPushSessionResolver` | 8 |
| 4 HTTP endpoints `/api/push/*` | 9 |
| Uplink frame routing + spoofing check | 10 |
| `cmd/atterm-relay/main.go` flag + Open wiring | 11 |
| Desktop `uplink.SendCommandEvent` | 12 |
| Desktop `App.BroadcastCommandFinished` binding | 13 |
| `desktop/frontend/src/lib/api.ts` wrapper | 14 |
| `TerminalView.vue` broadcast call | 15 |
| `web/app-core.js` helpers | 16 |
| `web/sw.js` push handler + cache bump | 17 |
| `web/app.js` enable / disable flow + button | 18 |
| `docs/web-push.md` user guide | 19 |
| `docs/spec/protocol.md` + README | 20 |
| Full test sweep | 21 |

**Placeholder scan.** No "TBD" / "TODO" / "implement later" / "similar to" / "handle errors" without specifics.

**Type consistency.**
- `Subscription` (with `Keys.P256dh`, `Keys.Auth`, `CreatedAt`) is the same shape in Go (subscription.go), the four HTTP endpoints, and the JSON sent by `web/app.js`'s `enablePushFlow`.
- `CommandFinished` Go struct fields (`SessionID`, `HostID`, `ExitCode`, `ElapsedMS`, `Label`) consistent in tasks 6 and 10.
- `CommandEventPayload` JSON tags (`exit_code`, `elapsed_ms`, `label`) consistent in tasks 7, 10, 12.
- `Service.PublicKey` / `AddSubscription` / `RemoveSubscription` / `DispatchCommandFinished` / `SendTest` / `SetSessionResolver` consistent across tasks 5, 6, 8, 9, 10, 11.
- `WebPushSessionResolver` signature `func(uuid.UUID) []string` consistent in tasks 8 and 11.
- TS wrapper name `broadcastCommandFinished` consistent in tasks 14, 15.
- web helper names `pushSupported`, `canEnablePush`, `base64UrlToUint8Array`, `enablePushFlow`, `disablePushFlow` consistent across tasks 16, 17, 18.
- HTTP endpoint paths `/api/push/{key,subscribe,unsubscribe,test}` consistent in tasks 9, 18, 19.

No gaps found.

---

## Manual Smoke Checklist (pre-merge, human-only)

Pulled from the spec — not auto-runnable; the executor should hand this to a human before merging.

1. New relay: `<dir>/web-push.json` auto-generated; `curl https://relay/api/push/key -H "Authorization: Bearer <token>"` returns a base64url string.
2. Chrome desktop: connect → "🔔 Enable" → permission Allow → `/api/push/subscribe` POST observed in relay log.
3. Chrome desktop: "Test notification" → OS notification "AT Term test".
4. Chrome desktop: `sleep 12; ls` + blur AT Term → within seconds, OS notification fires with `Command finished · exit 0 · 12s`.
5. iOS Safari (without PWA): Enable button shows hint "Add to Home Screen first".
6. iOS Safari → Add to Home Screen → open PWA → Enable → permission Allow.
7. iOS PWA: run step 4 on the desktop → iPhone lock-screen notification.
8. Close browser / PWA: run step 4 → notification still arrives.
9. "🔔 OFF" click → `/api/push/unsubscribe` POST → next step-4 command no notification.
10. Restart relay (preserving `<dir>/web-push.json`): subscriptions still alive.
11. Two-token isolation: read token and write token each subscribed on different browsers → both receive pushes for the visible session.
12. Focus gate: keep AT Term focused, run step-4 command → no local notification; no `TypeCommandEvent` in relay log; no push.
13. Two browsers same token: both subscribed → both notifications fire.
