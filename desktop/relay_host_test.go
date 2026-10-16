package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	desktopfeishu "github.com/attson/atterm/desktop/feishu"
	internalfeishu "github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
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

func TestOnRemoteTerminalToggle_TrueBackfillsEligibleLiveSessions(t *testing.T) {
	tests := []struct {
		name       string
		autoAttach string
		wantAI     bool
		wantShell  bool
	}{
		{name: "ai policy", autoAttach: "ai", wantAI: true},
		{name: "all policy", autoAttach: "all", wantAI: true, wantShell: true},
		{name: "none policy", autoAttach: "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestRelayHost(t)
			aiID := uuid.New()
			shellID := uuid.New()
			aiSess := session.New(aiID, proto.SessionInfo{Type: session.SessionTypeAI})
			shellSess := session.New(shellID, proto.SessionInfo{Type: session.SessionTypeShell})
			if _, err := h.server.Registry().Add(aiSess); err != nil {
				t.Fatalf("add AI session: %v", err)
			}
			if _, err := h.server.Registry().Add(shellSess); err != nil {
				t.Fatalf("add shell session: %v", err)
			}

			// Keep attach goroutines parked after lazyAttachIfMissing claims their
			// slots, making the synchronous toggle-on selection observable without
			// issuing real Feishu requests.
			disp := desktopfeishu.NewDispatcher(desktopfeishu.DispatcherConfig{})
			h.feishuDispatcher.Store(disp)
			release := make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			var stateCalls atomic.Int32
			h.feishuRemoteTermState = func(context.Context) (bool, string, string, bool) {
				if stateCalls.Add(1) > 1 {
					<-release
				}
				return true, "ou_bound", tt.autoAttach, true
			}

			h.OnRemoteTerminalToggle(true)

			h.feishuSubsMu.Lock()
			aiState := h.feishuSessions[aiID.String()]
			shellState := h.feishuSessions[shellID.String()]
			h.feishuSubsMu.Unlock()
			if got := aiState != nil && aiState.lazyAttachInFlight; got != tt.wantAI {
				t.Errorf("AI session queued = %v, want %v", got, tt.wantAI)
			}
			if got := shellState != nil && shellState.lazyAttachInFlight; got != tt.wantShell {
				t.Errorf("shell session queued = %v, want %v", got, tt.wantShell)
			}

			releaseOnce.Do(func() { close(release) })
		})
	}
}

func TestShouldEagerAttachFeishuSession(t *testing.T) {
	if !shouldEagerAttachFeishuSession(NewSessionReq{}) {
		t.Fatal("new session should eagerly attach")
	}
	if shouldEagerAttachFeishuSession(NewSessionReq{Recovered: true}) {
		t.Fatal("recovered session should wait for lazy activity backfill")
	}
}

func TestLatestAnchorPatchQueue_CoalescesPendingBodies(t *testing.T) {
	started := make(chan string, 4)
	releaseFirst := make(chan struct{})
	q := newLatestAnchorPatchQueue(func(body string) {
		started <- body
		if body == "first" {
			<-releaseFirst
		}
	})

	q.Enqueue("first")
	if got := <-started; got != "first" {
		t.Fatalf("first apply = %q", got)
	}
	q.Enqueue("stale-1")
	q.Enqueue("stale-2")
	q.Enqueue("latest")
	close(releaseFirst)

	select {
	case got := <-started:
		if got != "latest" {
			t.Fatalf("coalesced apply = %q, want latest", got)
		}
	case <-time.After(time.Second):
		t.Fatal("latest pending body was not applied")
	}
	select {
	case got := <-started:
		t.Fatalf("unexpected extra apply %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestLatestAnchorPatchQueue_StopDropsPendingBody(t *testing.T) {
	started := make(chan string, 2)
	releaseFirst := make(chan struct{})
	q := newLatestAnchorPatchQueue(func(body string) {
		started <- body
		if body == "first" {
			<-releaseFirst
		}
	})

	q.Enqueue("first")
	<-started
	q.Enqueue("must-not-run")
	q.Stop()
	close(releaseFirst)
	select {
	case got := <-started:
		t.Fatalf("apply after Stop = %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

type relayHostStubTokenSource struct{}

func (*relayHostStubTokenSource) Get(context.Context) (string, string, string, error) {
	return "tenant-token", "ou_owner", "app-hash", nil
}
func (*relayHostStubTokenSource) Invalidate() {}

type relayHostCapturingCardKit struct {
	patched     chan map[string]any
	sendEntered chan struct{}
	sendRelease chan struct{}
	sendOnce    sync.Once
}

func (c *relayHostCapturingCardKit) SendAnchorCard(context.Context, string, string, []byte) (string, string, error) {
	if c.sendEntered != nil {
		c.sendOnce.Do(func() { close(c.sendEntered) })
	}
	if c.sendRelease != nil {
		<-c.sendRelease
	}
	return "om_test", "card_test", nil
}
func (*relayHostCapturingCardKit) PatchCard(context.Context, string, string, string, string, int64) error {
	return nil
}
func (c *relayHostCapturingCardKit) PatchCardElement(_ context.Context, _, _, _ string, partial map[string]any, _ int64) error {
	c.patched <- partial
	return nil
}
func (*relayHostCapturingCardKit) UpdateCardElement(context.Context, string, string, string, map[string]any, int64) error {
	return nil
}
func (*relayHostCapturingCardKit) DeleteCardElement(context.Context, string, string, string, int64) error {
	return nil
}
func (*relayHostCapturingCardKit) CreateCardElement(context.Context, string, string, string, string, []map[string]any, int64) error {
	return nil
}

func TestUpgradeFeishuSessionToAI_SwitchesBodySourceAndControlsOnce(t *testing.T) {
	cardKit := &relayHostCapturingCardKit{patched: make(chan map[string]any, 2)}
	disp := desktopfeishu.NewDispatcher(desktopfeishu.DispatcherConfig{
		Token:   &relayHostStubTokenSource{},
		CardKit: cardKit,
	})
	h := &relayHost{
		feishuCards:    internalfeishu.NewCardIndex(),
		feishuSessions: map[string]*feishuSession{},
	}
	h.feishuDispatcher.Store(disp)

	sid := uuid.New()
	sess := session.New(sid, proto.SessionInfo{Type: session.SessionTypeShell})
	flushed := make(chan string, 4)
	flush := func(body string) { flushed <- body }
	reset := make(chan string, 1)
	rt := &anchorRuntime{flush: flush}
	rt.lastInner.Store("第 6/6 页\nold shell body")
	rt.lastOutputAt.Store(time.Now())
	rt.render = func() {
		body, _ := rt.lastInner.Load().(string)
		reset <- body
	}
	sub := internalfeishu.AttachFeishuSubscriber(sess, "ou_owner", true, flush)
	defer sub.Detach()
	anchor := &internalfeishu.CardAnchor{
		SessionID:      sid.String(),
		CardToken:      "card_test",
		Pagination:     true,
		ButtonsMounted: true,
	}
	h.feishuCards.Put(anchor)
	h.feishuSessions[sid.String()] = &feishuSession{
		sub:    sub,
		anchor: rt,
	}

	h.upgradeFeishuSessionToAI(sid.String())
	h.feishuSubsMu.Lock()
	firstChunker := h.feishuSessions[sid.String()].aiChunker
	h.feishuSubsMu.Unlock()
	if firstChunker == nil {
		t.Fatal("AI chunker was not attached")
	}
	select {
	case body := <-reset:
		if body != "" {
			t.Fatalf("AI upgrade retained shell body %q", body)
		}
	case <-time.After(time.Second):
		t.Fatal("AI upgrade did not reset the shell body")
	}
	h.upgradeFeishuSessionToAI(sid.String())
	h.feishuSubsMu.Lock()
	secondChunker := h.feishuSessions[sid.String()].aiChunker
	h.feishuSubsMu.Unlock()
	if secondChunker != firstChunker {
		t.Fatal("repeated classification replaced the AI chunker")
	}

	select {
	case partial := <-cardKit.patched:
		body, err := json.Marshal(partial)
		if err != nil {
			t.Fatalf("marshal patched controls: %v", err)
		}
		got := string(body)
		if !strings.Contains(got, `"Esc"`) || !strings.Contains(got, `"Enter"`) {
			t.Fatalf("AI controls missing from PATCH: %s", got)
		}
		if strings.Contains(got, "上页") || strings.Contains(got, "下页") {
			t.Fatalf("shell pagination remained in AI controls: %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for AI controls PATCH")
	}
	if anchor.Pagination {
		t.Fatal("anchor remained in pagination mode after AI upgrade")
	}
	select {
	case extra := <-cardKit.patched:
		t.Fatalf("repeated classification issued another controls PATCH: %v", extra)
	case <-time.After(50 * time.Millisecond):
	}

	disp.DispatchTurn(sid.String(), desktopfeishu.TurnEvent{
		Kind: desktopfeishu.TurnAssistantFinal,
		Text: "structured answer",
	})
	select {
	case body := <-flushed:
		if !strings.Contains(body, "structured answer") {
			t.Fatalf("AI flush = %q", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for structured AI body")
	}
}

// CardKit may rebuild sibling controls after a body PATCH. A periodic status
// refresh can therefore erase text that the user is still composing in the
// anchor input even though no terminal output changed.
func TestAttachFeishuSubscriber_DoesNotScheduleIdleCardRefresh(t *testing.T) {
	source, err := os.ReadFile("relay_host_feishu.go")
	if err != nil {
		t.Fatalf("read relay_host_feishu.go: %v", err)
	}
	start := strings.Index(string(source), "func (h *relayHost) attachFeishuSubscriberForAutoAttach(")
	end := strings.Index(string(source), "func (h *relayHost) upgradeFeishuSessionToAI(")
	if start < 0 || end <= start {
		t.Fatal("could not isolate attachFeishuSubscriberForAutoAttach; update this regression test")
	}
	if strings.Contains(string(source[start:end]), "time.NewTicker(") {
		t.Fatal("anchor attach schedules a periodic refresh; idle body PATCHes can clear an unsent Feishu input draft")
	}
}

func TestAttachFeishuSubscriber_UpgradesWhenAIClassificationRacesCardSend(t *testing.T) {
	cardKit := &relayHostCapturingCardKit{
		patched:     make(chan map[string]any, 2),
		sendEntered: make(chan struct{}),
		sendRelease: make(chan struct{}),
	}
	disp := desktopfeishu.NewDispatcher(desktopfeishu.DispatcherConfig{
		Token:   &relayHostStubTokenSource{},
		CardKit: cardKit,
	})
	h := &relayHost{
		feishuCards:    internalfeishu.NewCardIndex(),
		feishuSessions: map[string]*feishuSession{},
	}
	h.feishuDispatcher.Store(disp)
	h.feishuRemoteTermState = func(context.Context) (bool, string, string, bool) {
		return true, "ou_owner", "all", true
	}

	sid := uuid.New()
	sess := session.New(sid, proto.SessionInfo{Type: session.SessionTypeShell})
	done := make(chan struct{})
	go func() {
		h.attachFeishuSubscriberForAutoAttach(context.Background(), sess, sid)
		close(done)
	}()

	select {
	case <-cardKit.sendEntered:
	case <-time.After(time.Second):
		t.Fatal("anchor send did not start")
	}
	// Classification happens while the anchor network request is blocked, so
	// the callback-side upgrade cannot see a subscriber yet.
	sess.PushOut(1, []byte("\x1b]133;C;codex\x07"))
	if got := sess.Info().Type; got != session.SessionTypeAI {
		t.Fatalf("session type = %q after classification, want ai", got)
	}
	close(cardKit.sendRelease)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("attach did not finish after releasing card send")
	}

	h.feishuSubsMu.Lock()
	fs := h.feishuSessions[sid.String()]
	h.feishuSubsMu.Unlock()
	if fs == nil || fs.sub == nil || fs.aiChunker == nil {
		t.Fatalf("attach race left session in shell mode: %+v", fs)
	}
	t.Cleanup(func() {
		fs.sub.Detach()
		if fs.anchor != nil && fs.anchor.patches != nil {
			fs.anchor.patches.Stop()
		}
	})

	select {
	case partial := <-cardKit.patched:
		body, _ := json.Marshal(partial)
		if strings.Contains(string(body), "上页") || !strings.Contains(string(body), `"Esc"`) {
			t.Fatalf("raced attach kept shell controls: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("raced attach did not PATCH AI controls")
	}
}

func TestRecoveredSession_FirstInputStartsLazyFeishuAttach(t *testing.T) {
	h := newTestRelayHost(t)
	disp := desktopfeishu.NewDispatcher(desktopfeishu.DispatcherConfig{})
	h.SetFeishuDispatcher(disp)

	stateEntered := make(chan struct{})
	releaseState := make(chan struct{})
	var enterOnce sync.Once
	h.feishuRemoteTermState = func(context.Context) (bool, string, string, bool) {
		enterOnce.Do(func() { close(stateEntered) })
		<-releaseState
		return true, "ou_bound", "all", true
	}
	t.Cleanup(func() {
		select {
		case <-releaseState:
		default:
			close(releaseState)
		}
	})

	id, err := h.NewSession(context.Background(), NewSessionReq{
		Command:   "/bin/cat",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Recovered: true,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		t.Fatalf("session %s not found", id)
	}

	// Restoring an AI pane injects its resume command directly into the PTY.
	// The resulting OSC 133 command classification and task-state transition
	// are startup mechanics, not user activity, and must not create a card.
	sess.PushOut(1, []byte("\x1b]133;C;codex resume 11111111-1111-1111-1111-111111111111\x07"))
	select {
	case <-stateEntered:
		t.Fatal("automatic recovery classification unexpectedly started lazy Feishu attach")
	case <-time.After(50 * time.Millisecond):
	}
	disp.DispatchTurn(id.String(), desktopfeishu.TurnEvent{
		Kind: desktopfeishu.TurnAssistantFinal,
		Text: "restored output",
	})
	select {
	case <-stateEntered:
		t.Fatal("automatic recovery turn unexpectedly started lazy Feishu attach")
	case <-time.After(50 * time.Millisecond):
	}

	if !sess.SendInbound(proto.Frame{Type: proto.TypeResize, SessionID: id}) {
		t.Fatal("resize frame was not accepted")
	}
	select {
	case <-stateEntered:
		t.Fatal("resize unexpectedly started lazy Feishu attach")
	case <-time.After(50 * time.Millisecond):
	}

	if !sess.SendInbound(proto.Frame{Type: proto.TypeIn, SessionID: id, Payload: []byte("x")}) {
		t.Fatal("input frame was not accepted")
	}
	select {
	case <-stateEntered:
	case <-time.After(time.Second):
		t.Fatal("first input did not start lazy Feishu attach")
	}

	h.feishuSubsMu.Lock()
	state := h.feishuSessions[id.String()]
	inFlight := state != nil && state.lazyAttachInFlight
	h.feishuSubsMu.Unlock()
	if !inFlight {
		t.Fatal("lazy Feishu attach was not marked in flight")
	}
	close(releaseState)
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
