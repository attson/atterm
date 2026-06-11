package prefssync

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeAdapter implements Adapter for tests. It records calls.
type fakeAdapter struct {
	values map[string]json.RawMessage
	meta   map[string]Meta
}

func newFake() *fakeAdapter {
	return &fakeAdapter{values: map[string]json.RawMessage{}, meta: map[string]Meta{}}
}
func (f *fakeAdapter) ReadValue(key string) (json.RawMessage, bool) {
	v, ok := f.values[key]; return v, ok
}
func (f *fakeAdapter) WriteValue(key string, v json.RawMessage) error {
	f.values[key] = v; return nil
}
func (f *fakeAdapter) ReadMeta(key string) Meta { return f.meta[key] }
func (f *fakeAdapter) WriteMeta(key string, m Meta) error {
	f.meta[key] = m; return nil
}
func (f *fakeAdapter) Keys() []string {
	out := make([]string, 0, len(f.values))
	for k := range f.values { out = append(out, k) }
	return out
}

func TestSyncedKeys_MatchesWhitelist(t *testing.T) {
	got := SyncedKeys()
	want := []string{
		"locale_preference",
		"quick_templates",
		"notifications_enabled",
		"command_notify_threshold_seconds",
		"shell_integration_enabled",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d: %v", len(got), len(want), got)
	}
	set := map[string]bool{}
	for _, k := range got { set[k] = true }
	for _, k := range want {
		if !set[k] { t.Fatalf("missing key: %s", k) }
	}
}

func TestAdapterContract_RoundTrip(t *testing.T) {
	ctx := context.Background()
	_ = ctx
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	v, ok := a.ReadValue("locale_preference")
	if !ok || string(v) != `"en"` { t.Fatalf("round-trip: %v %s", ok, v) }
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: true})
	m := a.ReadMeta("locale_preference")
	if m.UpdatedAtLocal != 100 || !m.Dirty { t.Fatalf("meta: %+v", m) }
}

type fakeRelay struct {
	getReturn []ServerItem
	getErr    error
	putItems  []ClientItem
	putReturn []ServerItem
}

func (f *fakeRelay) Get(ctx context.Context) ([]ServerItem, error) {
	return f.getReturn, f.getErr
}
func (f *fakeRelay) Put(ctx context.Context, items []ClientItem) ([]ServerItem, error) {
	f.putItems = append([]ClientItem(nil), items...)
	if f.putReturn != nil { return f.putReturn, nil }
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
	if err := e.Pull(context.Background()); err != nil { t.Fatalf("Pull: %v", err) }

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"zh-CN"` { t.Fatalf("expected overwrite, got %s", v) }
	m := a.ReadMeta("locale_preference")
	if m.UpdatedAtLocal != 500 || m.Dirty { t.Fatalf("meta: %+v", m) }
}

func TestPull_LocalDirtyNewerIsPreserved(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 800, Dirty: true})

	r := &fakeRelay{getReturn: []ServerItem{
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 500},
	}}
	e := NewEngine(a, r)
	if err := e.Pull(context.Background()); err != nil { t.Fatalf("Pull: %v", err) }

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"en"` { t.Fatalf("expected local preserved, got %s", v) }
	m := a.ReadMeta("locale_preference")
	if !m.Dirty { t.Fatalf("expected dirty kept") }
}

func TestPull_ServerMissingKeyDoesNotTouchLocal(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	r := &fakeRelay{getReturn: nil}
	e := NewEngine(a, r)
	if err := e.Pull(context.Background()); err != nil { t.Fatalf("Pull: %v", err) }
	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"en"` { t.Fatalf("local wiped: %s", v) }
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
	if err := e.Push(context.Background()); err != nil { t.Fatalf("Push: %v", err) }

	if len(r.putItems) != 1 || r.putItems[0].Key != "locale_preference" {
		t.Fatalf("expected only dirty key sent, got %+v", r.putItems)
	}
	m := a.ReadMeta("locale_preference")
	if m.Dirty || m.UpdatedAtLocal != 850 { t.Fatalf("meta after push: %+v", m) }
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
	if err := e.Push(context.Background()); err != nil { t.Fatalf("Push: %v", err) }

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"zh-CN"` { t.Fatalf("expected server value, got %s", v) }
	m := a.ReadMeta("locale_preference")
	if m.Dirty || m.UpdatedAtLocal != 999 { t.Fatalf("meta: %+v", m) }
}

func TestPush_NoDirtyKeysSkipsRequest(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: false})

	r := &fakeRelay{}
	e := NewEngine(a, r)
	if err := e.Push(context.Background()); err != nil { t.Fatalf("Push: %v", err) }
	if r.putItems != nil { t.Fatalf("unexpected PUT: %+v", r.putItems) }
}

func TestMarkDirty_StampsMeta(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	e := NewEngine(a, &fakeRelay{})
	e.MarkDirty("locale_preference", 12345)
	m := a.ReadMeta("locale_preference")
	if !m.Dirty || m.UpdatedAtLocal != 12345 { t.Fatalf("meta: %+v", m) }
}
