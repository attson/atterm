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
