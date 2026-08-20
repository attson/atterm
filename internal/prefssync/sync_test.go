package prefssync

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"
)

// fakeAdapter implements Adapter for tests. It records calls.
type fakeAdapter struct {
	values map[string]json.RawMessage
	meta   map[string]Meta

	// writeValueErr, keyed by key, lets a test force WriteValue to fail for
	// one key without disturbing the others -- used to pin the log-and-continue
	// behaviour in Pull.
	writeValueErr map[string]error
}

func newFake() *fakeAdapter {
	return &fakeAdapter{values: map[string]json.RawMessage{}, meta: map[string]Meta{}}
}
func (f *fakeAdapter) ReadValue(key string) (json.RawMessage, bool) {
	v, ok := f.values[key]
	return v, ok
}
func (f *fakeAdapter) WriteValue(key string, v json.RawMessage) error {
	if err := f.writeValueErr[key]; err != nil {
		return err
	}
	f.values[key] = v
	return nil
}
func (f *fakeAdapter) ReadMeta(key string) Meta { return f.meta[key] }
func (f *fakeAdapter) WriteMeta(key string, m Meta) error {
	f.meta[key] = m
	return nil
}
func (f *fakeAdapter) Keys() []string {
	out := make([]string, 0, len(f.values))
	for k := range f.values {
		out = append(out, k)
	}
	return out
}

func TestSyncedKeys_MatchesWhitelist(t *testing.T) {
	got := SyncedKeys()
	want := []string{
		"locale_preference",
		"quick_templates",
		"notifications_enabled",
		"ai_notifications_only",
		"command_notify_threshold_seconds",
		"shell_integration_enabled",
		"pinned_session_ids",
		"ssh_hosts_encrypted",
		"terminal_theme",
		"terminal_font_head",
		"terminal_font_size",
		"terminal_line_height",
		"terminal_cursor_style",
		"terminal_cursor_blink",
		"terminal_scrollback",
		"default_shell",
		"shortcut_bindings",
		"profiles_encrypted",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d: %v", len(got), len(want), got)
	}
	set := map[string]bool{}
	for _, k := range got {
		set[k] = true
	}
	for _, k := range want {
		if !set[k] {
			t.Fatalf("missing key: %s", k)
		}
	}
}

func TestAdapterContract_RoundTrip(t *testing.T) {
	ctx := context.Background()
	_ = ctx
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	v, ok := a.ReadValue("locale_preference")
	if !ok || string(v) != `"en"` {
		t.Fatalf("round-trip: %v %s", ok, v)
	}
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: true})
	m := a.ReadMeta("locale_preference")
	if m.UpdatedAtLocal != 100 || !m.Dirty {
		t.Fatalf("meta: %+v", m)
	}
}

type fakeRelay struct {
	getReturn []ServerItem
	getErr    error
	putItems  []ClientItem
	putReturn []ServerItem
	// onPut, when set, runs after the request has been captured but before
	// the response is handed back — i.e. while the PUT is "in flight". Tests
	// use it to simulate the user changing a preference during the round trip.
	onPut func()
}

func (f *fakeRelay) Get(ctx context.Context) ([]ServerItem, error) {
	return f.getReturn, f.getErr
}
func (f *fakeRelay) Put(ctx context.Context, items []ClientItem) ([]ServerItem, error) {
	f.putItems = append([]ClientItem(nil), items...)
	if f.onPut != nil {
		f.onPut()
	}
	if f.putReturn != nil {
		return f.putReturn, nil
	}
	return nil, nil
}

func TestPull_ServerNewerOverwritesLocal(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: false})

	r := &fakeRelay{getReturn: []ServerItem{
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 500},
	}}
	e := NewEngine(a, r)
	result, err := e.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"zh-CN"` {
		t.Fatalf("expected overwrite, got %s", v)
	}
	m := a.ReadMeta("locale_preference")
	if m.UpdatedAtLocal != 500 || m.Dirty {
		t.Fatalf("meta: %+v", m)
	}
	if len(result.Adopted) != 1 || result.Adopted[0] != "locale_preference" {
		t.Fatalf("Adopted = %v; want [locale_preference]", result.Adopted)
	}
	if len(result.Conflict) != 0 {
		t.Fatalf("Conflict = %v; want empty", result.Conflict)
	}
}

// Named for the branch it actually exercises. Its fixture has local newer
// than the server (800 > 500), so the outer "server is newer" condition never
// opens and `Dirty` is never even read — this is the no-op branch, not the
// dirty-conflict branch its old name (TestPull_LocalDirtyNewerIsPreserved)
// promised. The real conflict branch is pinned by
// TestPull_ServerNewerButLocalDirty_RecordsConflict below; a name that points
// at the wrong branch is a trap for whoever next skims this file deciding
// what is already covered.
func TestPull_ServerNotNewer_LocalUnchanged(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 800, Dirty: true})

	r := &fakeRelay{getReturn: []ServerItem{
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 500},
	}}
	e := NewEngine(a, r)
	result, err := e.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"en"` {
		t.Fatalf("expected local preserved, got %s", v)
	}
	m := a.ReadMeta("locale_preference")
	if !m.Dirty {
		t.Fatalf("expected dirty kept")
	}
	// local.UpdatedAtLocal (800) > server.UpdatedAt (500): this is the no-op
	// branch ("server.updated_at <= local.updated_at_local"), not the
	// conflict branch -- the server simply isn't newer, so neither slice is
	// touched. See TestPull_ServerNewerButLocalDirty_RecordsConflict below
	// for the branch this test's name suggests but does not actually reach.
	if len(result.Conflict) != 0 {
		t.Fatalf("Conflict = %v; want empty (server was not newer)", result.Conflict)
	}
	if len(result.Adopted) != 0 {
		t.Fatalf("Adopted = %v; want empty", result.Adopted)
	}
}

// TestPull_ServerNewerButLocalDirty_RecordsConflict pins the branch that
// used to be a bare `continue`: server.updated_at > local.updated_at_local
// AND local is Dirty. Local wins for now (a later Push reconciles via LWW),
// but the user must be told two devices disagreed and a timestamp picked
// the winner -- that is the entire point of this task.
func TestPull_ServerNewerButLocalDirty_RecordsConflict(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: true})

	r := &fakeRelay{getReturn: []ServerItem{
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 500},
	}}
	e := NewEngine(a, r)
	result, err := e.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"en"` {
		t.Fatalf("expected local preserved, got %s", v)
	}
	m := a.ReadMeta("locale_preference")
	if !m.Dirty || m.UpdatedAtLocal != 100 {
		t.Fatalf("expected local meta untouched, got %+v", m)
	}
	if len(result.Conflict) != 1 || result.Conflict[0] != "locale_preference" {
		t.Fatalf("Conflict = %v; want [locale_preference] -- the user was never told two devices disagreed", result.Conflict)
	}
	if len(result.Adopted) != 0 {
		t.Fatalf("Adopted = %v; want empty", result.Adopted)
	}
}

func TestPull_ServerMissingKeyDoesNotTouchLocal(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	r := &fakeRelay{getReturn: nil}
	e := NewEngine(a, r)
	result, err := e.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"en"` {
		t.Fatalf("local wiped: %s", v)
	}
	if len(result.Adopted) != 0 || len(result.Conflict) != 0 {
		t.Fatalf("expected both slices empty, got Adopted=%v Conflict=%v", result.Adopted, result.Conflict)
	}
}

// TestPull_WriteValueErrorSkipsKeyAndContinues pins the log-and-continue
// behaviour added when the key count went 8 -> 17 (see sync.go's comment on
// this branch): a WriteValue failure for one key must not abort the loop or
// report that key as adopted/conflicted, and every later key must still be
// processed.
func TestPull_WriteValueErrorSkipsKeyAndContinues(t *testing.T) {
	a := newFake()
	a.writeValueErr = map[string]error{"shortcut_bindings": errors.New("malformed payload")}
	a.WriteMeta("shortcut_bindings", Meta{UpdatedAtLocal: 100, Dirty: false})
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: false})

	r := &fakeRelay{getReturn: []ServerItem{
		{Key: "shortcut_bindings", Value: json.RawMessage(`{bad json`), UpdatedAt: 500},
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 500},
	}}
	e := NewEngine(a, r)
	result, err := e.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	// The failing key is in neither slice.
	for _, k := range result.Adopted {
		if k == "shortcut_bindings" {
			t.Fatalf("shortcut_bindings should not be Adopted after a WriteValue error: %v", result.Adopted)
		}
	}
	for _, k := range result.Conflict {
		if k == "shortcut_bindings" {
			t.Fatalf("shortcut_bindings should not be Conflict after a WriteValue error: %v", result.Conflict)
		}
	}
	// The later key still gets processed -- the loop did not abort.
	if len(result.Adopted) != 1 || result.Adopted[0] != "locale_preference" {
		t.Fatalf("Adopted = %v; want [locale_preference] -- one bad key must not take the rest down", result.Adopted)
	}
	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"zh-CN"` {
		t.Fatalf("locale_preference not written: %s", v)
	}
}

// TestPull_ResultSlicesAreSorted pins the deterministic-order requirement:
// callers (and eventually the UI) must not need set gymnastics to compare
// what Pull did.
func TestPull_ResultSlicesAreSorted(t *testing.T) {
	a := newFake()
	a.WriteMeta("terminal_theme", Meta{UpdatedAtLocal: 100, Dirty: false})
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: false})
	a.WriteMeta("default_shell", Meta{UpdatedAtLocal: 100, Dirty: true})
	a.WriteMeta("ai_notifications_only", Meta{UpdatedAtLocal: 100, Dirty: true})

	r := &fakeRelay{getReturn: []ServerItem{
		{Key: "terminal_theme", Value: json.RawMessage(`"dark"`), UpdatedAt: 500},
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 500},
		{Key: "default_shell", Value: json.RawMessage(`"zsh"`), UpdatedAt: 500},
		{Key: "ai_notifications_only", Value: json.RawMessage(`true`), UpdatedAt: 500},
	}}
	e := NewEngine(a, r)
	result, err := e.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	wantAdopted := []string{"locale_preference", "terminal_theme"}
	wantConflict := []string{"ai_notifications_only", "default_shell"}
	if !sort.StringsAreSorted(result.Adopted) || len(result.Adopted) != len(wantAdopted) {
		t.Fatalf("Adopted = %v; want sorted %v", result.Adopted, wantAdopted)
	}
	for i, k := range wantAdopted {
		if result.Adopted[i] != k {
			t.Fatalf("Adopted = %v; want %v", result.Adopted, wantAdopted)
		}
	}
	if !sort.StringsAreSorted(result.Conflict) || len(result.Conflict) != len(wantConflict) {
		t.Fatalf("Conflict = %v; want sorted %v", result.Conflict, wantConflict)
	}
	for i, k := range wantConflict {
		if result.Conflict[i] != k {
			t.Fatalf("Conflict = %v; want %v", result.Conflict, wantConflict)
		}
	}
}

func TestPush_SendsDirtyAndClearsFlag(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"zh-CN"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 800, Dirty: true})
	a.WriteValue("notifications_enabled", json.RawMessage(`true`))
	a.WriteMeta("notifications_enabled", Meta{UpdatedAtLocal: 200, Dirty: false})

	r := &fakeRelay{putReturn: []ServerItem{
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 850},
	}}
	e := NewEngine(a, r)
	if err := e.Push(context.Background()); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if len(r.putItems) != 1 || r.putItems[0].Key != "locale_preference" {
		t.Fatalf("expected only dirty key sent, got %+v", r.putItems)
	}
	m := a.ReadMeta("locale_preference")
	if m.Dirty || m.UpdatedAtLocal != 850 {
		t.Fatalf("meta after push: %+v", m)
	}
}

func TestPush_ServerRejectionOverwritesLocal(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: true})

	// Server returns a newer value (e.g., set by another device).
	r := &fakeRelay{putReturn: []ServerItem{
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 999},
	}}
	e := NewEngine(a, r)
	if err := e.Push(context.Background()); err != nil {
		t.Fatalf("Push: %v", err)
	}

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"zh-CN"` {
		t.Fatalf("expected server value, got %s", v)
	}
	m := a.ReadMeta("locale_preference")
	if m.Dirty || m.UpdatedAtLocal != 999 {
		t.Fatalf("meta: %+v", m)
	}
}

func TestPush_NoDirtyKeysSkipsRequest(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: false})

	r := &fakeRelay{}
	e := NewEngine(a, r)
	if err := e.Push(context.Background()); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if r.putItems != nil {
		t.Fatalf("unexpected PUT: %+v", r.putItems)
	}
}

func TestMarkDirty_StampsMeta(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	e := NewEngine(a, &fakeRelay{})
	e.MarkDirty("locale_preference", 12345)
	m := a.ReadMeta("locale_preference")
	if !m.Dirty || m.UpdatedAtLocal != 12345 {
		t.Fatalf("meta: %+v", m)
	}
}

func TestSeedFromLocal_MarksMissingNonDefaultDirty(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"zh-CN"`))
	// Server returned nothing for any key (empty PULL response). We had no
	// chance to write meta during PULL. SeedFromLocal should flag the local
	// non-default value as dirty.
	r := &fakeRelay{}
	e := NewEngine(a, r)
	e.SeedFromLocal(func(key string) bool {
		// Only locale_preference's "zh-CN" is non-default in this test.
		return key == "locale_preference"
	}, 5555)

	m := a.ReadMeta("locale_preference")
	if !m.Dirty || m.UpdatedAtLocal != 5555 {
		t.Fatalf("expected dirty seed, got %+v", m)
	}
	mn := a.ReadMeta("notifications_enabled")
	if mn.Dirty {
		t.Fatalf("non-customized key should not be dirty")
	}
}

func TestWebglRendererIsNotSynced(t *testing.T) {
	// Its correct value depends on the local GPU driver — syncing it would
	// spread the #48 input-lag bug rather than a preference.
	for _, k := range SyncedKeys() {
		if k == "webgl_renderer_enabled" {
			t.Fatal("webgl_renderer_enabled must never be synced")
		}
	}
}

func TestSyncedKeys_IncludesPinnedSessionIds(t *testing.T) {
	keys := SyncedKeys()
	found := false
	for _, k := range keys {
		if k == "pinned_session_ids" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("SyncedKeys() = %v; want pinned_session_ids", keys)
	}
}

// Regression: Push used to apply the server echo unconditionally, including
// to keys the user had changed *after* the request was serialized. On a slow
// relay a second pin landing mid-PUT was therefore overwritten by the echo of
// the first pin, and its Dirty flag was cleared too — so the new pin was lost
// both locally and on the server, and the sidebar appeared to "undo" the
// click. Reconciliation must skip any key whose local meta moved while the
// request was in flight and leave it dirty for the next Push.
func TestPush_KeepsLocalEditMadeDuringPut(t *testing.T) {
	a := newFake()
	a.WriteValue("pinned_session_ids", json.RawMessage(`["a"]`))
	a.WriteMeta("pinned_session_ids", Meta{UpdatedAtLocal: 100, Dirty: true})

	r := &fakeRelay{
		// Server accepted the ["a"] we sent and stamped it.
		putReturn: []ServerItem{
			{Key: "pinned_session_ids", Value: json.RawMessage(`["a"]`), UpdatedAt: 150},
		},
		onPut: func() {
			// User pins a second session while the PUT is still in flight.
			a.WriteValue("pinned_session_ids", json.RawMessage(`["a","b"]`))
			a.WriteMeta("pinned_session_ids", Meta{UpdatedAtLocal: 200, Dirty: true})
		},
	}

	e := NewEngine(a, r)
	if err := e.Push(context.Background()); err != nil {
		t.Fatalf("Push: %v", err)
	}

	v, _ := a.ReadValue("pinned_session_ids")
	if string(v) != `["a","b"]` {
		t.Fatalf("in-flight local edit was clobbered by the server echo: got %s, want [\"a\",\"b\"]", v)
	}
	m := a.ReadMeta("pinned_session_ids")
	if !m.Dirty {
		t.Fatal("Dirty was cleared for a key edited during the PUT; the new value would never be pushed")
	}
	if m.UpdatedAtLocal != 200 {
		t.Fatalf("UpdatedAtLocal = %d; want the local edit's 200", m.UpdatedAtLocal)
	}
}

// Keys untouched during the round trip must still reconcile normally, so the
// guard above cannot turn into "never accept the server's answer".
func TestPush_UntouchedKeyStillAdoptsServerEcho(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: true})
	a.WriteValue("pinned_session_ids", json.RawMessage(`["a"]`))
	a.WriteMeta("pinned_session_ids", Meta{UpdatedAtLocal: 100, Dirty: true})

	r := &fakeRelay{
		putReturn: []ServerItem{
			{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 900},
			{Key: "pinned_session_ids", Value: json.RawMessage(`["a"]`), UpdatedAt: 900},
		},
		onPut: func() {
			// Only the pins change mid-flight; locale must still reconcile.
			a.WriteValue("pinned_session_ids", json.RawMessage(`["a","b"]`))
			a.WriteMeta("pinned_session_ids", Meta{UpdatedAtLocal: 200, Dirty: true})
		},
	}

	e := NewEngine(a, r)
	if err := e.Push(context.Background()); err != nil {
		t.Fatalf("Push: %v", err)
	}

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"zh-CN"` {
		t.Fatalf("untouched key did not adopt the server value: %s", v)
	}
	if m := a.ReadMeta("locale_preference"); m.Dirty || m.UpdatedAtLocal != 900 {
		t.Fatalf("untouched key meta after push: %+v", m)
	}
}

// MarkDirty must never hand out a timestamp that is <= the one already
// recorded for that key. Two pins inside the same millisecond would otherwise
// share an UpdatedAtLocal, which is exactly the signal Push uses to detect an
// in-flight edit — the second pin would look "untouched" and be clobbered.
func TestMarkDirty_TimestampIsMonotonicPerKey(t *testing.T) {
	a := newFake()
	e := NewEngine(a, &fakeRelay{})

	e.MarkDirty("pinned_session_ids", 1000)
	e.MarkDirty("pinned_session_ids", 1000) // same millisecond

	if m := a.ReadMeta("pinned_session_ids"); m.UpdatedAtLocal <= 1000 {
		t.Fatalf("UpdatedAtLocal = %d; want > 1000 so the second edit is distinguishable", m.UpdatedAtLocal)
	}
	// A genuinely later clock reading is still used verbatim.
	e.MarkDirty("pinned_session_ids", 5000)
	if m := a.ReadMeta("pinned_session_ids"); m.UpdatedAtLocal != 5000 {
		t.Fatalf("UpdatedAtLocal = %d; want 5000", m.UpdatedAtLocal)
	}
}
