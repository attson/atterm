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
