package configsync

import (
	"encoding/json"
	"errors"
	"testing"
)

func newTestRelayCompatibility(t *testing.T) *RelayCompatibility {
	t.Helper()
	compat, err := NewRelayCompatibility("realm-1", RelayCompatibilityState{})
	if err != nil {
		t.Fatal(err)
	}
	return compat
}

func TestRelayPullImportsOnceAndIgnoresEquivalentJSON(t *testing.T) {
	compat := newTestRelayCompatibility(t)
	imports := 0
	apply := func(value RelayValue) error {
		imports++
		if string(value.Value) != `{"a":"first","z":"last"}` {
			t.Fatalf("import was not normalized: %s", value.Value)
		}
		return nil
	}
	first := RelayValue{Key: "shortcut_bindings", Value: json.RawMessage(`{"z":"last","a":"first"}`), UpdatedAt: 100}
	if errs := compat.Import([]RelayValue{first}, apply); len(errs) != 0 {
		t.Fatalf("first import errors=%v", errs)
	}
	equivalent := RelayValue{Key: "shortcut_bindings", Value: json.RawMessage(` { "a":"first", "z":"last" } `), UpdatedAt: 200}
	if errs := compat.Import([]RelayValue{equivalent}, apply); len(errs) != 0 {
		t.Fatalf("equivalent import errors=%v", errs)
	}
	if imports != 1 {
		t.Fatalf("imports=%d want=1", imports)
	}
	state := compat.State().Keys["shortcut_bindings"]
	if state.RelayUpdatedAt != 200 {
		t.Fatalf("updated_at=%d want=200", state.RelayUpdatedAt)
	}
}

func TestRelayEchoDoesNotCreateCanonicalMutation(t *testing.T) {
	compat := newTestRelayCompatibility(t)
	materialized := RelayValue{Key: "terminal_theme", Value: json.RawMessage(`"nord"`), UpdatedAt: 100}
	exports, errs := compat.PlanExports([]RelayValue{materialized})
	if len(errs) != 0 || len(exports) != 1 {
		t.Fatalf("exports=%v errs=%v", exports, errs)
	}
	imports := 0
	echo := RelayValue{Key: "terminal_theme", Value: json.RawMessage(`"nord"`), UpdatedAt: 500}
	if errs := compat.Import([]RelayValue{echo}, func(RelayValue) error { imports++; return nil }); len(errs) != 0 {
		t.Fatalf("echo errors=%v", errs)
	}
	if imports != 0 {
		t.Fatalf("echo generated %d imports", imports)
	}
	state := compat.State().Keys["terminal_theme"]
	if state.PendingExportHash != "" || state.LastExportedValueHash == "" || state.RelayValueHash != state.LastExportedValueHash {
		t.Fatalf("state=%+v", state)
	}
	// Once the Relay winner is acknowledged, the same Peer winner is not
	// exported again.
	exports, errs = compat.PlanExports([]RelayValue{{Key: "terminal_theme", Value: json.RawMessage(` "nord" `), UpdatedAt: 600}})
	if len(errs) != 0 || len(exports) != 0 {
		t.Fatalf("repeat exports=%v errs=%v", exports, errs)
	}
}

func TestFailedRelayExportRemainsRetryable(t *testing.T) {
	compat := newTestRelayCompatibility(t)
	value := RelayValue{Key: "terminal_theme", Value: json.RawMessage(`"nord"`), UpdatedAt: 100}
	first, errs := compat.PlanExports([]RelayValue{value})
	if len(errs) != 0 || len(first) != 1 {
		t.Fatalf("first=%v errs=%v", first, errs)
	}
	second, errs := compat.PlanExports([]RelayValue{value})
	if len(errs) != 0 || len(second) != 1 {
		t.Fatalf("pending export was not retried: second=%v errs=%v", second, errs)
	}
}

func TestConcurrentRelayAndPeerChangesConvergeWithoutOscillation(t *testing.T) {
	compat := newTestRelayCompatibility(t)
	var imported []string
	apply := func(value RelayValue) error {
		imported = append(imported, string(value.Value))
		return nil
	}
	if errs := compat.Import([]RelayValue{{Key: "terminal_theme", Value: json.RawMessage(`"classic"`), UpdatedAt: 100}}, apply); len(errs) != 0 {
		t.Fatal(errs)
	}
	peerWinner := RelayValue{Key: "terminal_theme", Value: json.RawMessage(`"nord"`), UpdatedAt: 200}
	if exports, errs := compat.PlanExports([]RelayValue{peerWinner}); len(errs) != 0 || len(exports) != 1 {
		t.Fatalf("exports=%v errs=%v", exports, errs)
	}
	// A different device wins Relay's LWW race while our PUT is in flight.
	if errs := compat.Import([]RelayValue{{Key: "terminal_theme", Value: json.RawMessage(`"daylight"`), UpdatedAt: 300}}, apply); len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(imported) != 2 || imported[0] != `"classic"` || imported[1] != `"daylight"` {
		t.Fatalf("imports=%v", imported)
	}
	// If configsync materializes the imported Relay winner, it is already the
	// observed baseline and must not bounce back through PUT.
	exports, errs := compat.PlanExports([]RelayValue{{Key: "terminal_theme", Value: json.RawMessage(`"daylight"`), UpdatedAt: 300}})
	if len(errs) != 0 || len(exports) != 0 {
		t.Fatalf("oscillating exports=%v errs=%v", exports, errs)
	}
}

func TestMalformedRelayValueDoesNotBlockSiblingImport(t *testing.T) {
	compat := newTestRelayCompatibility(t)
	var imported []string
	errs := compat.Import([]RelayValue{
		{Key: "terminal_font_size", Value: json.RawMessage(`"large"`), UpdatedAt: 100},
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 100},
	}, func(value RelayValue) error {
		imported = append(imported, value.Key)
		return nil
	})
	if len(errs) != 1 || !errors.Is(errs[0], ErrInvalidSchemaValue) {
		t.Fatalf("errors=%v", errs)
	}
	if len(imported) != 1 || imported[0] != "locale_preference" {
		t.Fatalf("imported=%v", imported)
	}
}

func TestFailedRelayImportDoesNotAdvanceState(t *testing.T) {
	compat := newTestRelayCompatibility(t)
	wantErr := errors.New("durable append failed")
	item := RelayValue{Key: "terminal_theme", Value: json.RawMessage(`"nord"`), UpdatedAt: 100}
	errs := compat.Import([]RelayValue{item}, func(RelayValue) error { return wantErr })
	if len(errs) != 1 || !errors.Is(errs[0], wantErr) {
		t.Fatalf("errors=%v", errs)
	}
	if _, exists := compat.State().Keys[item.Key]; exists {
		t.Fatalf("failed import advanced state: %+v", compat.State())
	}
	called := 0
	if errs := compat.Import([]RelayValue{item}, func(RelayValue) error { called++; return nil }); len(errs) != 0 || called != 1 {
		t.Fatalf("retry called=%d errors=%v", called, errs)
	}
}

func TestRelayTimestampForkIsRejected(t *testing.T) {
	compat := newTestRelayCompatibility(t)
	if errs := compat.Import([]RelayValue{{Key: "terminal_theme", Value: json.RawMessage(`"nord"`), UpdatedAt: 100}}, func(RelayValue) error { return nil }); len(errs) != 0 {
		t.Fatal(errs)
	}
	errs := compat.Import([]RelayValue{{Key: "terminal_theme", Value: json.RawMessage(`"classic"`), UpdatedAt: 100}}, func(RelayValue) error { return nil })
	if len(errs) != 1 || !errors.Is(errs[0], ErrRelayTimestampFork) {
		t.Fatalf("errors=%v", errs)
	}
}

func TestRelayCompatibilityStateStrictRoundTrip(t *testing.T) {
	compat := newTestRelayCompatibility(t)
	compat.SetMigrationMarkers(true, true)
	_, _ = compat.PlanExports([]RelayValue{{Key: "terminal_theme", Value: json.RawMessage(`"nord"`), UpdatedAt: 100}})
	raw, err := json.Marshal(compat.State())
	if err != nil {
		t.Fatal(err)
	}
	state, err := ParseRelayCompatibilityState(raw)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewRelayCompatibility("realm-1", state)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.State().LocalSeeded || !reopened.State().MigrationComplete {
		t.Fatalf("markers lost: %+v", reopened.State())
	}
	if _, err := ParseRelayCompatibilityState(append(raw, []byte(` {}`)...)); !errors.Is(err, ErrInvalidRelayState) {
		t.Fatalf("trailing JSON error=%v", err)
	}
}
