package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

// newRecoveryTestApp wires an App with a real loopback relayHost + a
// RecoveryStore pinned to t.TempDir(). eventsEmitter is a no-op so the
// Wails strict-ctx check doesn't fire. Returns app; cleanup is registered
// via t.Cleanup(host.Stop).
func newRecoveryTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("LocalAppData", filepath.Join(root, "local"))

	cfgStore := &configStore{}
	host, err := startRelayHost(cfgStore)
	if err != nil {
		t.Fatalf("startRelayHost: %v", err)
	}
	t.Cleanup(host.Stop)

	rs, err := NewRecoveryStore(host.hostID)
	if err != nil {
		t.Fatalf("NewRecoveryStore: %v", err)
	}
	rs.nowUnix = func() int64 { return 1750000000 }

	a := &App{
		cfgStore:      cfgStore,
		ctx:           context.Background(),
		host:          host,
		recoveryStore: rs,
		eventsEmitter: func(_ context.Context, _ string, _ ...interface{}) {},
	}
	return a
}

func TestApp_SaveLoadRecoverySnapshot_RoundTrip(t *testing.T) {
	app := newRecoveryTestApp(t)
	snap := RecoverySnapshot{
		Version:       recoverySnapshotVersion,
		HostID:        app.host.hostID,
		CleanShutdown: true,
		SavedAtUnix:   app.recoveryStore.nowUnix(),
		ActiveTabID:   "t-1",
		Tabs: []TabSnapshot{
			{ID: "t-1", Layout: "single", Panes: []PaneSnapshot{
				{Slot: 0, Shell: "/bin/zsh", LastCwd: "/tmp"},
			}},
		},
	}
	blob, _ := json.Marshal(snap)
	if err := app.SaveRecoverySnapshot(string(blob)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := app.LoadRecoverySnapshot()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Tabs) != 1 || got.Tabs[0].Panes[0].LastCwd != "/tmp" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !got.CleanShutdown {
		t.Fatalf("Load should surface original clean_shutdown=true to caller")
	}
}

func TestApp_LoadRecoverySnapshot_MarksOnlyExactLiveLocalSession(t *testing.T) {
	app := newRecoveryTestApp(t)
	liveID := uuid.New()
	model := session.New(liveID, proto.SessionInfo{ID: liveID.String()})
	if _, err := app.host.server.Registry().Add(model); err != nil {
		t.Fatalf("add live session: %v", err)
	}
	t.Cleanup(func() { app.host.server.Registry().Remove(liveID) })

	deadID := uuid.New().String()
	snap := RecoverySnapshot{
		Tabs: []TabSnapshot{{
			ID: "t-1",
			Panes: []PaneSnapshot{
				{Slot: 0, SessionID: liveID.String(), Shell: "/bin/zsh"},
				{Slot: 1, SessionID: deadID, Shell: "/bin/zsh"},
				{Slot: 2, Remote: true, SessionID: liveID.String(), Shell: "/bin/zsh"},
			},
		}},
	}
	blob, _ := json.Marshal(snap)
	if err := app.SaveRecoverySnapshot(string(blob)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := app.LoadRecoverySnapshot()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	panes := got.Tabs[0].Panes
	if !panes[0].Reattach {
		t.Fatal("exact live local session should be marked reattach")
	}
	if panes[1].Reattach {
		t.Fatal("missing local session must not be marked reattach")
	}
	if panes[2].Reattach {
		t.Fatal("remote pane must not be marked reattach")
	}
	if app.lastSnapshot.Tabs[0].Panes[0].Reattach {
		t.Fatal("runtime reattach hint must not enter lastSnapshot")
	}
}

func TestApp_SaveRecoverySnapshot_StripsRuntimeReattachHint(t *testing.T) {
	app := newRecoveryTestApp(t)
	payload := `{"tabs":[{"id":"t","panes":[{"slot":0,"session_id":"sid","shell":"/bin/zsh","reattach":true}]}]}`
	if err := app.SaveRecoverySnapshot(payload); err != nil {
		t.Fatalf("Save: %v", err)
	}

	persisted, err := app.recoveryStore.Load()
	if err != nil {
		t.Fatalf("load persisted snapshot: %v", err)
	}
	if persisted.Tabs[0].Panes[0].Reattach {
		t.Fatal("reattach hint was persisted to recovery.json")
	}
}

func TestApp_DiscardRecoverySnapshot(t *testing.T) {
	app := newRecoveryTestApp(t)
	payload := `{"version":1,"host_id":"` + app.host.hostID + `","saved_at_unix":1750000000,"tabs":[{"id":"t","panes":[{"slot":0,"shell":"/bin/zsh"}]}]}`
	if err := app.SaveRecoverySnapshot(payload); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := app.DiscardRecoverySnapshot(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	got, _ := app.LoadRecoverySnapshot()
	if len(got.Tabs) != 0 {
		t.Fatalf("expected empty after discard, got %+v", got)
	}
}

// TestApp_LoadRecoverySnapshot_TabsNeverNil pins the wire shape that the
// frontend depends on: Go's nil slice marshals to JSON `null`, and the
// onMounted boot block accesses `recoverySnap.tabs.length` outside of a
// try/catch — a nil slice there used to lock the app on the "starting first
// session" message with no surfaced error (no recovery.json on disk = the
// first-ever launch).
func TestApp_LoadRecoverySnapshot_TabsNeverNil(t *testing.T) {
	t.Run("no recoveryStore", func(t *testing.T) {
		app := &App{ctx: context.Background()}
		got, err := app.LoadRecoverySnapshot()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.Tabs == nil {
			t.Fatalf("Tabs must be non-nil (would marshal to JSON null)")
		}
	})
	t.Run("file does not exist", func(t *testing.T) {
		app := newRecoveryTestApp(t)
		got, err := app.LoadRecoverySnapshot()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.Tabs == nil {
			t.Fatalf("Tabs must be non-nil even when recovery.json does not exist")
		}
	})
	t.Run("after discard", func(t *testing.T) {
		app := newRecoveryTestApp(t)
		payload := `{"version":1,"host_id":"` + app.host.hostID + `","saved_at_unix":1750000000,"tabs":[{"id":"t","panes":[{"slot":0,"shell":"/bin/zsh"}]}]}`
		if err := app.SaveRecoverySnapshot(payload); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if err := app.DiscardRecoverySnapshot(); err != nil {
			t.Fatalf("Discard: %v", err)
		}
		got, err := app.LoadRecoverySnapshot()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.Tabs == nil {
			t.Fatalf("Tabs must be non-nil after discard")
		}
	})
}

func TestApp_MarkCleanShutdown(t *testing.T) {
	app := newRecoveryTestApp(t)
	payload := `{"version":1,"host_id":"` + app.host.hostID + `","saved_at_unix":1750000000,"tabs":[{"id":"t","panes":[{"slot":0,"shell":"/bin/zsh"}]}]}`
	if err := app.SaveRecoverySnapshot(payload); err != nil {
		t.Fatal(err)
	}
	// First Load surfaces current clean=false and rewrites with same value.
	if _, err := app.LoadRecoverySnapshot(); err != nil {
		t.Fatal(err)
	}
	if err := app.MarkCleanShutdown(); err != nil {
		t.Fatalf("MarkCleanShutdown: %v", err)
	}
	got2, _ := app.LoadRecoverySnapshot()
	if !got2.CleanShutdown {
		t.Fatalf("expected on-disk clean_shutdown=true after MarkCleanShutdown, got %+v", got2)
	}
}

func TestApp_AiSidCallback_EmitsEvent(t *testing.T) {
	app := newRecoveryTestApp(t)
	got := make(chan map[string]string, 1)
	app.eventsEmitter = func(_ context.Context, name string, data ...interface{}) {
		if name != "recovery:ai-sid" || len(data) == 0 {
			return
		}
		if m, ok := data[0].(map[string]string); ok {
			got <- m
		}
	}
	// Re-wire the host callback to use our emitter (mirrors what startup does).
	app.host.aiSidCallback = func(sid uuid.UUID, kind, aiSid string) {
		app.eventsEmitter(app.ctx, "recovery:ai-sid", map[string]string{
			"session_id":    sid.String(),
			"kind":          kind,
			"ai_session_id": aiSid,
		})
	}
	id := uuid.New()
	app.host.aiSidCallback(id, "claude", "ai-sid-xyz")
	select {
	case m := <-got:
		if m["session_id"] != id.String() || m["kind"] != "claude" || m["ai_session_id"] != "ai-sid-xyz" {
			t.Fatalf("payload = %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("event not received")
	}
}

func TestApp_BeforeClose_MarksCleanShutdown(t *testing.T) {
	app := newRecoveryTestApp(t)
	payload := `{"version":1,"host_id":"` + app.host.hostID + `","saved_at_unix":1750000000,"tabs":[{"id":"t","panes":[{"slot":0,"shell":"/bin/zsh"}]}]}`
	if err := app.SaveRecoverySnapshot(payload); err != nil {
		t.Fatal(err)
	}
	// Drain on-disk clean flag back to false (simulates a launch reading
	// the snapshot before the user quits).
	if _, err := app.LoadRecoverySnapshot(); err != nil {
		t.Fatal(err)
	}
	// User approves quit.
	app.quitApproved.Store(true)
	// Wails calls OnBeforeClose; we expect false (proceed) and clean=true on disk.
	if proceed := app.beforeClose(app.ctx, func() {}); proceed {
		t.Fatalf("beforeClose returned true (abort), want false (proceed) after quitApproved")
	}
	got, _ := app.LoadRecoverySnapshot()
	if !got.CleanShutdown {
		t.Fatalf("expected clean_shutdown=true after beforeClose with quitApproved, got %+v", got)
	}
}

func TestApp_BeforeClose_DoesNotMarkWhenNotApproved(t *testing.T) {
	app := newRecoveryTestApp(t)
	payload := `{"version":1,"host_id":"` + app.host.hostID + `","saved_at_unix":1750000000,"tabs":[{"id":"t","panes":[{"slot":0,"shell":"/bin/zsh"}]}]}`
	if err := app.SaveRecoverySnapshot(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := app.LoadRecoverySnapshot(); err != nil {
		t.Fatal(err)
	}
	// quitApproved stays false. beforeClose should NOT mark clean.
	if proceed := app.beforeClose(app.ctx, func() {}); !proceed {
		t.Fatalf("beforeClose returned false (proceed) without quitApproved, want true (abort)")
	}
	got, _ := app.LoadRecoverySnapshot()
	if got.CleanShutdown {
		t.Fatalf("clean_shutdown must remain false when quit was not approved")
	}
}
