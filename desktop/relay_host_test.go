package main

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	internalfeishu "github.com/attson/atterm/internal/feishu"
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
// OnRemoteTerminalToggle(false) drains h.feishuSessions. The records here
// carry no real subscriber, so sub.Detach() is skipped by
// detachFeishuSubscriber's nil-guard — but the map-drain and archive paths
// are exercised without requiring a full PTY stack.
func TestOnRemoteTerminalToggle_FalseEmptiesMap(t *testing.T) {
	h := newTestRelayHost(t)

	// Inject two sentinel records — realistic keys, no real subscriber needed.
	sid1 := "11111111-1111-1111-1111-111111111111"
	sid2 := "22222222-2222-2222-2222-222222222222"
	h.feishuSubsMu.Lock()
	h.feishuSessions[sid1] = &feishuSession{}
	h.feishuSessions[sid2] = &feishuSession{}
	h.feishuSubsMu.Unlock()

	h.OnRemoteTerminalToggle(false)

	h.feishuSubsMu.Lock()
	remaining := len(h.feishuSessions)
	h.feishuSubsMu.Unlock()
	if remaining != 0 {
		t.Errorf("feishuSessions has %d entries after toggle-off, want 0", remaining)
	}
}

// TestOnRemoteTerminalToggle_TrueIsNoop verifies that
// OnRemoteTerminalToggle(true) does not touch the session map.
func TestOnRemoteTerminalToggle_TrueIsNoop(t *testing.T) {
	h := newTestRelayHost(t)

	sid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	h.feishuSubsMu.Lock()
	h.feishuSessions[sid] = &feishuSession{}
	h.feishuSubsMu.Unlock()

	h.OnRemoteTerminalToggle(true)

	h.feishuSubsMu.Lock()
	remaining := len(h.feishuSessions)
	h.feishuSubsMu.Unlock()
	if remaining != 1 {
		t.Errorf("feishuSessions has %d entries after toggle-on, want 1", remaining)
	}
}

// Lazy anchor backfill: when the user toggles Feishu remote-terminal on
// mid-session, the next AI activity (task-state change or hook TurnEvent)
// must trigger attachFeishuSubscriberForAutoAttach exactly once per
// session even under concurrent bursts. The gate lives in
// tryStartLazyAttach / clearLazyAttachInFlight — this suite covers it.

func TestTryStartLazyAttach_SkipsWhenSubscriberExists(t *testing.T) {
	h := newTestRelayHost(t)
	sid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	// Inject a record with a non-nil sub sentinel — matches production
	// shape (attachFeishuSubscriberForAutoAttach only ever writes non-nil).
	h.feishuSubsMu.Lock()
	h.feishuSessions[sid] = &feishuSession{sub: &internalfeishu.FeishuSubscriber{}}
	h.feishuSubsMu.Unlock()

	if got := h.tryStartLazyAttach(sid); got {
		t.Fatalf("tryStartLazyAttach = true when subscriber already exists")
	}
	h.feishuSubsMu.Lock()
	inflight := false
	if fs := h.feishuSessions[sid]; fs != nil {
		inflight = fs.lazyAttachInFlight
	}
	h.feishuSubsMu.Unlock()
	if inflight {
		t.Fatalf("in-flight slot claimed despite gate returning false")
	}
}

func TestTryStartLazyAttach_TriggersWhenMissing(t *testing.T) {
	h := newTestRelayHost(t)
	sid := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	if got := h.tryStartLazyAttach(sid); !got {
		t.Fatalf("tryStartLazyAttach = false when no subscriber and no in-flight")
	}
	h.feishuSubsMu.Lock()
	in := h.feishuSessions[sid] != nil && h.feishuSessions[sid].lazyAttachInFlight
	h.feishuSubsMu.Unlock()
	if !in {
		t.Fatalf("in-flight slot not claimed after gate returned true")
	}
}

func TestTryStartLazyAttach_SkipsWhenAlreadyInFlight(t *testing.T) {
	h := newTestRelayHost(t)
	sid := "cccccccc-cccc-cccc-cccc-cccccccccccc"

	if !h.tryStartLazyAttach(sid) {
		t.Fatalf("first call: gate rejected")
	}
	if h.tryStartLazyAttach(sid) {
		t.Fatalf("second call: gate must reject while in-flight")
	}
}

func TestClearLazyAttachInFlight_ReleasesSlot(t *testing.T) {
	h := newTestRelayHost(t)
	sid := "dddddddd-dddd-dddd-dddd-dddddddddddd"

	if !h.tryStartLazyAttach(sid) {
		t.Fatalf("first call: gate rejected")
	}
	h.clearLazyAttachInFlight(sid)
	if !h.tryStartLazyAttach(sid) {
		t.Fatalf("after clear: gate must allow re-entry")
	}
}

func TestTryStartLazyAttach_ConcurrentBurstCollapsesToOne(t *testing.T) {
	h := newTestRelayHost(t)
	sid := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"

	const N = 32
	var wg sync.WaitGroup
	var winners atomic.Int32
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if h.tryStartLazyAttach(sid) {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if winners.Load() != 1 {
		t.Fatalf("winners = %d, want exactly 1 across %d concurrent callers", winners.Load(), N)
	}
	h.feishuSubsMu.Lock()
	in := h.feishuSessions[sid] != nil && h.feishuSessions[sid].lazyAttachInFlight
	h.feishuSubsMu.Unlock()
	if !in {
		t.Fatalf("in-flight slot lost after single winner claimed it")
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
