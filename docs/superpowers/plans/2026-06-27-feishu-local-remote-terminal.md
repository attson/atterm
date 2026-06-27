# Feishu Local-Mode Remote Terminal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the "enable Feishu remote terminal" toggle persist and anchor cards fire in local mode, by routing remote-terminal settings through the local keychain store and feeding the anchor-card guard state by effective mode.

**Architecture:** Local-mode remote-terminal settings (enabled + autoAttach) are stored in the keychain binding blob alongside credentials/OpenID. `app.go` dispatches `Set/GetFeishuRemoteTerminalSettings` by effective mode (local → keychain, relay → sqlite, unchanged). The anchor-card guard in `relayHost` stops reading `sqliteStore` directly and instead calls a callback injected by `app.go` that reads the correct store per mode.

**Tech Stack:** Go (desktop package), `safekeyring` keychain store, `go test` via `~/sdk/go1.24.13/bin/go` (the system `go` is 1.19 and cannot parse `go.mod`'s `go 1.23.0`).

**Build/test command note:** All Go build/test commands in this plan must run with:
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go <args>
```
from the `desktop/` directory (or repo root where noted).

---

## File structure

- `desktop/feishu/binding_store.go` — add two fields to `BindingView`.
- `desktop/feishu/binding_store_local.go` — add `remote_terminal_enabled` / `session_auto_attach` to `localBindingBlob`, surface them in `Get`, add `SetRemoteTerminalSettings` method.
- `desktop/feishu/binding_store_local_test.go` — tests for the new local store method.
- `desktop/app.go` — dispatch `Set/GetFeishuRemoteTerminalSettings` by mode; install the anchor-guard state callback in `startFeishu` and `reconcileFeishuMode`.
- `desktop/relay_host.go` — add `feishuRemoteTermState` callback field + setter; replace the direct `sqliteStore.GetFeishuBinding` read in `attachFeishuSubscriberForAutoAttach` with the callback.
- `desktop/app_feishu_remote_terminal_test.go` — new test file for app-level local-mode round-trip.

---

## Task 1: Add remote-terminal fields to the local keychain blob + BindingView

**Files:**
- Modify: `desktop/feishu/binding_store.go:22-30` (BindingView struct)
- Modify: `desktop/feishu/binding_store_local.go:31-61` (blob struct + Get)
- Test: `desktop/feishu/binding_store_local_test.go`

- [ ] **Step 1: Write the failing test**

Append to `desktop/feishu/binding_store_local_test.go`:

```go
func TestLocalStore_RemoteTerminalRoundTrip(t *testing.T) {
	safekeyring.SetFileDirForTest(t.TempDir())
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	s := NewLocalKeychainBindingStore()
	ctx := context.Background()

	// Settings persist even with no prior credentials blob.
	if err := s.SetRemoteTerminalSettings(ctx, true, "all"); err != nil {
		t.Fatalf("SetRemoteTerminalSettings (fresh): %v", err)
	}
	v, err := s.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !v.RemoteTerminalEnabled || v.SessionAutoAttach != "all" {
		t.Fatalf("want enabled+all, got %+v", v)
	}
}
```

Check the test file's imports include `context` and `safekeyring`
(`github.com/attson/atterm/internal/safekeyring`); add them if missing.

- [ ] **Step 2: Run test to verify it fails**

Run (from `desktop/`):
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go test ./feishu/ -run TestLocalStore_RemoteTerminalRoundTrip
```
Expected: FAIL — compile error `v.RemoteTerminalEnabled undefined` and `s.SetRemoteTerminalSettings undefined`.

- [ ] **Step 3: Add the two fields to BindingView**

In `desktop/feishu/binding_store.go`, inside `type BindingView struct`, add after the `DisabledAt int64` line:

```go
	// Remote-terminal settings (local mode only; relay mode manages these in
	// the embedded sqlite store, not through this view).
	RemoteTerminalEnabled bool
	SessionAutoAttach     string
```

- [ ] **Step 4: Add the two fields to the blob and surface them in Get**

In `desktop/feishu/binding_store_local.go`, inside `type localBindingBlob struct`, add after the `DisabledAt int64` line:

```go
	RemoteTerminalEnabled bool   `json:"remote_terminal_enabled,omitempty"`
	SessionAutoAttach     string `json:"session_auto_attach,omitempty"`
```

In the `Get` method's returned `&BindingView{...}`, add the two fields:

```go
		RemoteTerminalEnabled: b.RemoteTerminalEnabled,
		SessionAutoAttach:     b.SessionAutoAttach,
```

- [ ] **Step 5: Add SetRemoteTerminalSettings to the local store**

In `desktop/feishu/binding_store_local.go`, add this method (place it after `SetCredentials`):

```go
// SetRemoteTerminalSettings persists the remote-terminal toggle and autoAttach
// mode into the keychain blob. Independent of credentials: a blob is created
// with only these fields if none exists yet, so the user can enable remote
// terminal before filling in AppID/AppSecret.
func (s *LocalKeychainBindingStore) SetRemoteTerminalSettings(ctx context.Context, enabled bool, autoAttach string) error {
	switch autoAttach {
	case "ai", "all", "none":
	default:
		return fmt.Errorf("desktop/feishu: invalid session_auto_attach %q (want ai|all|none)", autoAttach)
	}
	cur, err := s.Get(ctx)
	var blob localBindingBlob
	if err == nil {
		blob = localBindingBlob{
			AppID: cur.AppID, AppSecret: cur.AppSecret,
			EncryptKey: cur.EncryptKey, VerifyToken: cur.VerifyToken,
			OpenID: cur.OpenID, BoundAt: cur.BoundAt, DisabledAt: cur.DisabledAt,
		}
	}
	blob.RemoteTerminalEnabled = enabled
	blob.SessionAutoAttach = autoAttach
	return s.write(blob)
}
```

`fmt` is already imported in this file.

- [ ] **Step 6: Run test to verify it passes**

Run (from `desktop/`):
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go test ./feishu/ -run TestLocalStore_RemoteTerminalRoundTrip
```
Expected: PASS.

- [ ] **Step 7: Add an invalid-autoAttach rejection test**

Append to `desktop/feishu/binding_store_local_test.go`:

```go
func TestLocalStore_RemoteTerminalRejectsInvalidAutoAttach(t *testing.T) {
	safekeyring.SetFileDirForTest(t.TempDir())
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	s := NewLocalKeychainBindingStore()
	if err := s.SetRemoteTerminalSettings(context.Background(), true, "bogus"); err == nil {
		t.Fatal("want error for invalid autoAttach, got nil")
	}
}
```

- [ ] **Step 8: Run both local-store tests**

Run (from `desktop/`):
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go test ./feishu/ -run TestLocalStore_RemoteTerminal
```
Expected: PASS (2 tests).

- [ ] **Step 9: Commit**

```bash
git add desktop/feishu/binding_store.go desktop/feishu/binding_store_local.go desktop/feishu/binding_store_local_test.go
git commit -m "feat(feishu): persist remote-terminal settings in local keychain blob

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Dispatch app.go Set/GetFeishuRemoteTerminalSettings by mode

**Files:**
- Modify: `desktop/app.go:1702-1746` (Get + Set methods)
- Test: `desktop/app_feishu_remote_terminal_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `desktop/app_feishu_remote_terminal_test.go`:

```go
package main

import (
	"context"
	"testing"

	"github.com/attson/atterm/desktop/feishu"
	"github.com/attson/atterm/internal/safekeyring"
)

// In local mode the remote-terminal settings round-trip through the keychain
// blob, independent of the relay sqlite store (which has no row for the user).
func TestRemoteTerminalSettings_LocalRoundTrip(t *testing.T) {
	safekeyring.SetFileDirForTest(t.TempDir())
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	svc, err := feishu.NewService(feishu.ServiceConfig{Mode: feishu.ModeLocal})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	a := &App{feishuService: svc, feishuMode: "local", ctx: context.Background()}

	if err := a.SetFeishuRemoteTerminalSettings(true, "all"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := a.GetFeishuRemoteTerminalSettings()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enabled || got.AutoAttach != "all" {
		t.Fatalf("want enabled+all, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `desktop/`):
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go test . -run TestRemoteTerminalSettings_LocalRoundTrip
```
Expected: FAIL — local mode currently routes through `a.host.sqliteStore`, which is nil here → returns `"relay host unavailable"` from Set, so the assertion fails (or Set errors).

- [ ] **Step 3: Add a local-mode helper that returns the local store**

In `desktop/app.go`, add this helper near `currentFeishu` (around line 2413):

```go
// localBindingStore returns the keychain-backed store when Feishu is running in
// local mode, or nil otherwise. Used to route remote-terminal settings to the
// keychain (relay mode keeps them in the embedded sqlite store).
func (a *App) localBindingStore() *feishu.LocalKeychainBindingStore {
	svc, mode := a.currentFeishu()
	if svc == nil || mode != "local" {
		return nil
	}
	ls, _ := svc.Store().(*feishu.LocalKeychainBindingStore)
	return ls
}
```

- [ ] **Step 4: Dispatch Get by mode**

In `desktop/app.go`, replace the body of `GetFeishuRemoteTerminalSettings` (currently starts at line 1702). Replace from `defaults := ...` down to the final `return FeishuRemoteTerminalSettings{...}, nil` with:

```go
	defaults := FeishuRemoteTerminalSettings{Enabled: false, AutoAttach: "ai"}
	if a.ctx == nil {
		return defaults, nil
	}
	// Local mode: read the keychain blob.
	if ls := a.localBindingStore(); ls != nil {
		v, err := ls.Get(a.ctx)
		if err != nil {
			return defaults, nil // no blob yet → defaults
		}
		autoAttach := v.SessionAutoAttach
		if autoAttach == "" {
			autoAttach = "ai"
		}
		return FeishuRemoteTerminalSettings{
			Enabled:    v.RemoteTerminalEnabled,
			AutoAttach: autoAttach,
		}, nil
	}
	// Relay mode: read the embedded sqlite binding (unchanged).
	if a.host == nil || a.host.sqliteStore == nil {
		return defaults, nil
	}
	b, err := a.host.sqliteStore.GetFeishuBinding(a.ctx, a.host.adminUserID)
	if err != nil {
		return defaults, nil
	}
	autoAttach := b.SessionAutoAttach
	if autoAttach == "" {
		autoAttach = "ai"
	}
	return FeishuRemoteTerminalSettings{
		Enabled:    b.RemoteTerminalEnabled,
		AutoAttach: autoAttach,
	}, nil
```

- [ ] **Step 5: Dispatch Set by mode**

In `desktop/app.go`, replace the body of `SetFeishuRemoteTerminalSettings` (currently starts at line 1731). Replace from the first `if a.host == nil ...` down to the final `return nil` with:

```go
	if a.ctx == nil {
		return fmt.Errorf("app not ready")
	}
	// Local mode: write the keychain blob; the toggle side effect still runs
	// against the in-memory subscriber map below.
	if ls := a.localBindingStore(); ls != nil {
		prevEnabled := false
		if v, err := ls.Get(a.ctx); err == nil {
			prevEnabled = v.RemoteTerminalEnabled
		}
		if err := ls.SetRemoteTerminalSettings(a.ctx, enabled, autoAttach); err != nil {
			return err
		}
		if a.host != nil && prevEnabled != enabled {
			a.host.OnRemoteTerminalToggle(enabled)
		}
		return nil
	}
	// Relay mode: write the embedded sqlite binding (unchanged).
	if a.host == nil || a.host.sqliteStore == nil {
		return fmt.Errorf("relay host unavailable")
	}
	prev, _ := a.host.sqliteStore.GetFeishuBinding(a.ctx, a.host.adminUserID)
	if err := a.host.sqliteStore.SetRemoteTerminalSettings(a.ctx, a.host.adminUserID, enabled, autoAttach); err != nil {
		return err
	}
	if prev != nil && prev.RemoteTerminalEnabled != enabled {
		a.host.OnRemoteTerminalToggle(enabled)
	}
	return nil
```

- [ ] **Step 6: Run the new test to verify it passes**

Run (from `desktop/`):
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go test . -run TestRemoteTerminalSettings_LocalRoundTrip
```
Expected: PASS.

- [ ] **Step 7: Run existing relay-mode remote-terminal tests for regression**

Run (from `desktop/`):
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go test . -run 'RemoteTerminal|FeishuRemoteTerminal'
```
Expected: PASS (new local test + any existing relay tests). If an existing relay test relied on `a.host` being set, it still uses the relay branch because `localBindingStore()` returns nil when `feishuMode != "local"`.

- [ ] **Step 8: Commit**

```bash
git add desktop/app.go desktop/app_feishu_remote_terminal_test.go
git commit -m "feat(feishu): route remote-terminal settings by mode (local→keychain)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Inject anchor-guard state callback; decouple relayHost from sqliteStore

**Files:**
- Modify: `desktop/relay_host.go` (struct field + setter + guard read at `attachFeishuSubscriberForAutoAttach`, ~line 787-805)
- Modify: `desktop/app.go` (install callback in `startFeishu` ~line 2248 and `reconcileFeishuMode` ~line 2307)
- Test: `desktop/relay_host_test.go` (or nearest existing relay_host test file)

- [ ] **Step 1: Add the callback field + setter to relayHost**

In `desktop/relay_host.go`, inside the `relayHost` struct (near the `feishuDispatcher atomic.Pointer` field around line 75), add:

```go
	// feishuRemoteTermState reports the remote-terminal gate state for the live
	// Feishu mode. Injected by app.go so the guard does not bind to a concrete
	// store. ok=false means "no binding / not ready" → skip auto-attach.
	feishuRemoteTermState func(ctx context.Context) (enabled bool, openID, autoAttach string, ok bool)
```

After `SetFeishuDispatcher` (around line 92), add:

```go
// SetFeishuRemoteTermState installs the callback the anchor-card guard uses to
// read remote-terminal gate state for the current mode.
func (h *relayHost) SetFeishuRemoteTermState(fn func(ctx context.Context) (bool, string, string, bool)) {
	h.feishuRemoteTermState = fn
}
```

- [ ] **Step 2: Replace the guard's sqliteStore read with the callback**

In `desktop/relay_host.go`, in `attachFeishuSubscriberForAutoAttach`, replace this block (currently lines ~792-805):

```go
	if h.sqliteStore == nil {
		return
	}
	b, err := h.sqliteStore.GetFeishuBinding(ctx, h.adminUserID)
	if err != nil {
		// Binding not found or store error — silently skip.
		return
	}
	if !b.RemoteTerminalEnabled {
		return
	}
	if b.OpenID == "" {
		return
	}
```

with:

```go
	if h.feishuRemoteTermState == nil {
		return
	}
	enabled, openID, autoAttach, ok := h.feishuRemoteTermState(ctx)
	if !ok {
		// No binding / not ready — silently skip.
		return
	}
	if !enabled {
		return
	}
	if openID == "" {
		return
	}
```

- [ ] **Step 3: Update the autoAttach switch to use the local variable**

Immediately below, the switch currently reads `b.SessionAutoAttach`. Replace `switch b.SessionAutoAttach {` with `switch autoAttach {`. Leave the `case "none" / "ai" / "all" / default` bodies unchanged.

- [ ] **Step 4: Build to confirm relayHost compiles (b is now unused)**

Run (from `desktop/`):
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go build ./...
```
Expected: success. If the compiler reports `b declared and not used` elsewhere in the function, confirm no later code references `b`; the replaced block removed the only `b` usage besides the switch, which Step 3 also migrated.

- [ ] **Step 5: Install the callback in app.go startFeishu**

In `desktop/app.go` `startFeishu`, inside the `if a.host != nil {` block right after `a.host.SetFeishuDispatcher(svc.Dispatcher())` (around line 2248), add:

```go
		a.host.SetFeishuRemoteTermState(a.feishuRemoteTermState)
```

- [ ] **Step 6: Install the callback in app.go reconcileFeishuMode**

In `desktop/app.go` `reconcileFeishuMode`, inside the `if a.host != nil {` block right after `a.host.SetFeishuDispatcher(newSvc.Dispatcher())` (around line 2307), add:

```go
		a.host.SetFeishuRemoteTermState(a.feishuRemoteTermState)
```

- [ ] **Step 7: Implement a.feishuRemoteTermState**

In `desktop/app.go`, add this method near `localBindingStore` (added in Task 2):

```go
// feishuRemoteTermState reads the remote-terminal gate state for the live mode:
// the keychain blob in local mode, the embedded sqlite binding in relay mode.
// Returns ok=false when no binding exists yet or the store is unavailable.
func (a *App) feishuRemoteTermState(ctx context.Context) (enabled bool, openID, autoAttach string, ok bool) {
	if ls := a.localBindingStore(); ls != nil {
		v, err := ls.Get(ctx)
		if err != nil {
			return false, "", "", false
		}
		aa := v.SessionAutoAttach
		if aa == "" {
			aa = "ai"
		}
		return v.RemoteTerminalEnabled, v.OpenID, aa, true
	}
	if a.host == nil || a.host.sqliteStore == nil {
		return false, "", "", false
	}
	b, err := a.host.sqliteStore.GetFeishuBinding(ctx, a.host.adminUserID)
	if err != nil {
		return false, "", "", false
	}
	aa := b.SessionAutoAttach
	if aa == "" {
		aa = "ai"
	}
	return b.RemoteTerminalEnabled, b.OpenID, aa, true
}
```

- [ ] **Step 8: Write a guard test for the callback wiring**

Append to `desktop/app_feishu_remote_terminal_test.go`:

```go
// The injected callback returns the local-mode gate state (enabled+openID+
// autoAttach) read from the keychain blob.
func TestFeishuRemoteTermState_LocalReadsKeychain(t *testing.T) {
	safekeyring.SetFileDirForTest(t.TempDir())
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	svc, err := feishu.NewService(feishu.ServiceConfig{Mode: feishu.ModeLocal})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx := context.Background()
	// Seed credentials + bind + enable remote terminal.
	if err := svc.Store().SetCredentials(ctx, feishu.Credentials{AppID: "a", AppSecret: "s"}); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if err := svc.Store().SetBound(ctx, "ou_user"); err != nil {
		t.Fatalf("SetBound: %v", err)
	}
	ls, _ := svc.Store().(*feishu.LocalKeychainBindingStore)
	if err := ls.SetRemoteTerminalSettings(ctx, true, "ai"); err != nil {
		t.Fatalf("SetRemoteTerminalSettings: %v", err)
	}

	a := &App{feishuService: svc, feishuMode: "local", ctx: ctx}
	enabled, openID, autoAttach, ok := a.feishuRemoteTermState(ctx)
	if !ok || !enabled || openID != "ou_user" || autoAttach != "ai" {
		t.Fatalf("want ok+enabled+ou_user+ai, got ok=%v enabled=%v openID=%q autoAttach=%q",
			ok, enabled, openID, autoAttach)
	}
}

// No keychain blob → ok=false so the guard skips auto-attach.
func TestFeishuRemoteTermState_LocalNoBindingSkips(t *testing.T) {
	safekeyring.SetFileDirForTest(t.TempDir())
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	svc, err := feishu.NewService(feishu.ServiceConfig{Mode: feishu.ModeLocal})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	a := &App{feishuService: svc, feishuMode: "local", ctx: context.Background()}
	if _, _, _, ok := a.feishuRemoteTermState(a.ctx); ok {
		t.Fatal("want ok=false when no blob exists")
	}
}
```

- [ ] **Step 9: Run the new tests**

Run (from `desktop/`):
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go test . -run 'TestFeishuRemoteTermState|TestRemoteTerminalSettings_Local'
```
Expected: PASS (3 tests).

- [ ] **Step 10: Commit**

```bash
git add desktop/relay_host.go desktop/app.go desktop/app_feishu_remote_terminal_test.go
git commit -m "feat(feishu): feed anchor-card guard state by mode via injected callback

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Full build + package test sweep

**Files:** none (verification only)

- [ ] **Step 1: Build everything**

Run (from repo root):
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go build ./...
```
Expected: success, no output.

- [ ] **Step 2: Run the desktop + feishu package tests**

Run (from repo root):
```
GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go test ./desktop/... 
```
Expected: `ok` for `desktop`, `desktop/feishu`, and any sub-packages with tests.

- [ ] **Step 3: Run the frontend type check (no frontend changes, sanity only)**

Run (from `desktop/frontend/`):
```
npx vue-tsc --noEmit -p tsconfig.json
```
Expected: exit 0. (No frontend files changed in this plan; this only confirms the existing toggle wiring still type-checks.)

- [ ] **Step 4: Manual verification note**

After building the desktop app (`wails dev` or the project's build command), in **local mode** with a bound Feishu account:
1. Toggle "enable Feishu remote terminal" on — it should stay on after closing/reopening settings.
2. Set autoAttach to "all", start a new shell session — an anchor card should appear in Feishu.

This step is a human check; record the outcome in the PR description.

---

## Notes for the implementer

- The system `go` is 1.19 and will error with `invalid go version '1.23.0'`. Always use `GOROOT=~/sdk/go1.24.13 ~/sdk/go1.24.13/bin/go`.
- `safekeyring.UseFileStore()` + `SetFileDirForTest(t.TempDir())` is the established pattern for keychain-backed tests (see `TestGetFeishuStatus_LocalConfigured` in `desktop/app_feishu_status_test.go`).
- Relay mode is intentionally untouched: `localBindingStore()` returns nil unless `feishuMode == "local"`, so every relay path falls through to the existing `sqliteStore` code.
- `OnRemoteTerminalToggle` is reused unchanged — it only mutates the in-memory `feishuSubs` map.
