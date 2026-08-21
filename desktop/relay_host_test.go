package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	internalfeishu "github.com/attson/atterm/internal/feishu"
)

// newTestRelayHost spins up a relayHost on a temp HOME / XDG_CONFIG_HOME so
// the userstore SQLite file lives under a temp dir cleaned up with the test.
// Shared by tests that want a real mini-relay without booting the full *App
// wiring.
//
// Every caller of this forks a REAL shell, so `-count=N` for large N is not a
// meaningful stress mode for the TestRelayHost family and its failures there
// are not a bug. Measured on a machine with kern.tty.ptmx_max = 511: the
// family at -count=60 fails 136 times, while TWO separate -count=30 runs fail
// zero times each. That split is the signature of a per-process resource pool
// running out — ptys are reclaimed when the test binary exits — and not of
// shared state between tests. The failure reads `open pty: device not
// configured`.
//
// Recorded because it was misdiagnosed once as "some other shared state we
// have not found yet" and cost an investigation. If you are chasing a
// -count=N failure in this family, check the pty limit first.
//
// The root is os.MkdirTemp, NOT t.TempDir(), and that is load-bearing.
// t.TempDir registers its own removal as a separate cleanup, and t.Cleanup is
// LIFO, so the removal ran BEFORE h.Stop for every caller that created
// anything afterwards. Worse, HOME points here and these tests start a real
// /bin/sh: a shell writes .bash_history when it EXITS, and an AI-restore
// session's child writes .claude.json and .claude/backups/. Killing those
// processes during cleanup makes them write into a directory RemoveAll has
// already scanned, and the removal fails with "directory not empty" — which
// surfaced as an unattributable flake in whichever test happened to be
// running (seen across several branches, including trees predating any of
// this work).
//
// Owning the directory here lets one cleanup do the two things in the right
// order — stop the host first, then remove — and retry the removal for the
// moment it takes a dying shell to finish its last write.
func newTestRelayHost(t *testing.T) *relayHost {
	t.Helper()
	root, err := os.MkdirTemp("", "atterm-relayhost-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("LocalAppData", filepath.Join(root, "local"))

	cfgStore := &configStore{}
	h, err := startRelayHost(cfgStore)
	if err != nil {
		os.RemoveAll(root)
		t.Fatalf("startRelayHost: %v", err)
	}
	t.Cleanup(func() {
		h.Stop()
		removeAllEventually(t, root)
	})
	return h
}

// removeAllEventually deletes dir, retrying while a process that is on its way
// out is still creating files inside it. A shell's .bash_history lands after
// the shell has been told to die, so a single RemoveAll loses that race often
// enough to redden CI.
func removeAllEventually(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := os.RemoveAll(dir); err == nil {
			return
		}
		if time.Now().After(deadline) {
			// Leaving a temp dir behind is not worth failing a green test
			// over; the OS reaps it. Say so rather than silently swallowing.
			t.Logf("could not remove %s within 2s; leaving it for the OS", dir)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
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
