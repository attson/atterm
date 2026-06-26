package main

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// newTestRelayHost spins up a relayHost on a temp HOME / XDG_CONFIG_HOME so
// the userstore SQLite file lives under t.TempDir() and is cleaned up with
// the test. Shared by tests that want a real mini-relay without booting
// the full *App wiring.
func newTestRelayHost(t *testing.T) *relayHost {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("LocalAppData", filepath.Join(root, "local"))

	cfgStore := &configStore{}
	h, err := startRelayHost(cfgStore)
	if err != nil {
		t.Fatalf("startRelayHost: %v", err)
	}
	t.Cleanup(h.Stop)
	return h
}

// TestStartRelayHost_CreatesLocalAdminAndSessionToken verifies that the
// loopback mini-relay now requires a real session token: startRelayHost
// must bootstrap a local admin user and surface a session token that
// /api/me accepts.
func TestStartRelayHost_CreatesLocalAdminAndSessionToken(t *testing.T) {
	h := newTestRelayHost(t)

	if h.sessionToken == "" {
		t.Fatal("relayHost.sessionToken is empty")
	}
	if h.addr == "" {
		t.Fatal("relayHost.addr is empty")
	}

	// The token must resolve via /api/me on the local relay.
	httpURL := "http://" + h.addr
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpURL+"/api/me", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.sessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("/api/me: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me: status=%d body=%s", resp.StatusCode, body)
	}
	// Sanity-check the response body mentions the local admin email so we
	// know we resolved as the right principal — not some other accidental
	// user.
	if !strings.Contains(string(body), localAdminEmail) {
		t.Fatalf("/api/me body did not mention %q: %s", localAdminEmail, body)
	}
}

// TestStartRelayHost_PersistsAdminPasswordAcrossRestarts verifies that the
// LocalAdminPassword written on first launch is reused on subsequent
// launches, so the same on-disk users.db is reachable each time.
func TestStartRelayHost_PersistsAdminPasswordAcrossRestarts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("LocalAppData", filepath.Join(root, "local"))

	cfgStore := &configStore{}

	h1, err := startRelayHost(cfgStore)
	if err != nil {
		t.Fatalf("first startRelayHost: %v", err)
	}
	pw1 := cfgStore.Get().LocalAdminPassword
	if pw1 == "" {
		t.Fatal("LocalAdminPassword not persisted on first run")
	}
	tok1 := h1.sessionToken
	h1.Stop()

	// Second launch: must reuse the password and successfully VerifyPassword
	// against the existing on-disk users.db (no UNIQUE-email error).
	h2, err := startRelayHost(cfgStore)
	if err != nil {
		t.Fatalf("second startRelayHost: %v", err)
	}
	defer h2.Stop()

	if cfgStore.Get().LocalAdminPassword != pw1 {
		t.Fatal("LocalAdminPassword changed between launches")
	}
	if h2.sessionToken == "" {
		t.Fatal("second launch produced empty session token")
	}
	if h2.sessionToken == tok1 {
		t.Fatal("expected a fresh session token on relaunch, got the same one")
	}
}

// TestOnRemoteTerminalToggle_FalseEmptiesMap verifies that
// OnRemoteTerminalToggle(false) drains h.feishuSubs. The subscribers in the
// map are nil (no real session), so sub.Detach() is skipped by
// detachFeishuSubscriber's nil-guard — but the map-drain and archive paths
// are exercised without requiring a full PTY stack.
func TestOnRemoteTerminalToggle_FalseEmptiesMap(t *testing.T) {
	h := newTestRelayHost(t)

	// Inject two sentinel nil entries — realistic keys, no real subscriber needed.
	sid1 := "11111111-1111-1111-1111-111111111111"
	sid2 := "22222222-2222-2222-2222-222222222222"
	h.feishuSubsMu.Lock()
	h.feishuSubs[sid1] = nil
	h.feishuSubs[sid2] = nil
	h.feishuSubsMu.Unlock()

	h.OnRemoteTerminalToggle(false)

	h.feishuSubsMu.Lock()
	remaining := len(h.feishuSubs)
	h.feishuSubsMu.Unlock()
	if remaining != 0 {
		t.Errorf("feishuSubs has %d entries after toggle-off, want 0", remaining)
	}
}

// TestOnRemoteTerminalToggle_TrueIsNoop verifies that
// OnRemoteTerminalToggle(true) does not touch the subscriber map.
func TestOnRemoteTerminalToggle_TrueIsNoop(t *testing.T) {
	h := newTestRelayHost(t)

	sid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	h.feishuSubsMu.Lock()
	h.feishuSubs[sid] = nil
	h.feishuSubsMu.Unlock()

	h.OnRemoteTerminalToggle(true)

	h.feishuSubsMu.Lock()
	remaining := len(h.feishuSubs)
	h.feishuSubsMu.Unlock()
	if remaining != 1 {
		t.Errorf("feishuSubs has %d entries after toggle-on, want 1", remaining)
	}
}

func TestShouldNotifySession(t *testing.T) {
	cases := []struct {
		name        string
		sessionType string
		aiOnly      bool
		want        bool
	}{
		{"ai session, ai-only on", "ai", true, true},
		{"shell session, ai-only on", "shell", true, false},
		{"ai session, ai-only off", "ai", false, true},
		{"shell session, ai-only off", "shell", false, true},
		{"empty type, ai-only on", "", true, false},
	}
	for _, c := range cases {
		if got := shouldNotifySession(c.sessionType, c.aiOnly); got != c.want {
			t.Errorf("%s: shouldNotifySession(%q, %v) = %v, want %v",
				c.name, c.sessionType, c.aiOnly, got, c.want)
		}
	}
}
