package configsync

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDurableRelayCompatibilitySurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.json")
	store, err := OpenDurableRelayCompatibility(path, "realm-1")
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Update(func(compatibility *RelayCompatibility) error {
		compatibility.SetMigrationMarkers(true, true)
		planned, errs := compatibility.PlanExports([]RelayValue{{Key: "terminal_theme", Value: []byte(`"nord"`), UpdatedAt: 100}})
		if len(errs) != 0 || len(planned) != 1 {
			t.Fatalf("planned=%+v errors=%v", planned, errs)
		}
		errs = compatibility.Import([]RelayValue{{
			Key: "quick_templates", Value: []byte(`[{"id":"template-1","label":"Build","text":"go test"}]`), UpdatedAt: 101,
		}}, func(RelayValue, []RecordRef) ([]RecordRef, error) {
			return []RecordRef{{Collection: CollectionQuickTemplate, RecordID: "template-1", KeyClass: KeyClassSync}}, nil
		})
		if len(errs) != 0 {
			t.Fatalf("import errors=%v", errs)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Keys["terminal_theme"].PendingExportHash == "" {
		t.Fatal("pending export hash was not persisted")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("compatibility file info=%v err=%v", info, err)
	}

	reopened, err := OpenDurableRelayCompatibility(path, "realm-1")
	if err != nil {
		t.Fatal(err)
	}
	compatibility, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	restored := compatibility.State()
	if !restored.LocalSeeded || !restored.MigrationComplete || restored.Keys["terminal_theme"].PendingExportHash != state.Keys["terminal_theme"].PendingExportHash {
		t.Fatalf("restored state=%+v", restored)
	}
	templates := restored.Keys["quick_templates"]
	if templates.RelayUpdatedAt != 101 || templates.RelayValueHash == "" || len(templates.RelayRecords) != 1 || templates.RelayRecords[0].RecordID != "template-1" {
		t.Fatalf("restored Relay import state=%+v", templates)
	}
	if _, err := OpenDurableRelayCompatibility(path, "another-realm"); !errors.Is(err, ErrInvalidRelayState) {
		t.Fatalf("realm mismatch error=%v", err)
	}
}

func TestDurableRelayCompatibilitySerializesProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.json")
	stores := make([]*DurableRelayCompatibility, 2)
	for index := range stores {
		var err error
		stores[index], err = OpenDurableRelayCompatibility(path, "realm-1")
		if err != nil {
			t.Fatal(err)
		}
	}
	keys := []string{"terminal_theme", "locale_preference"}
	values := [][]byte{[]byte(`"nord"`), []byte(`"zh-CN"`)}
	errs := make([]error, len(stores))
	var wait sync.WaitGroup
	for index := range stores {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, errs[index] = stores[index].Update(func(compatibility *RelayCompatibility) error {
				_, planErrors := compatibility.PlanExports([]RelayValue{{Key: keys[index], Value: values[index], UpdatedAt: int64(100 + index)}})
				if len(planErrors) != 0 {
					return planErrors[0]
				}
				return nil
			})
		}(index)
	}
	wait.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	compatibility, err := stores[0].Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if compatibility.State().Keys[key].PendingExportHash == "" {
			t.Fatalf("concurrent update lost key %s: %+v", key, compatibility.State())
		}
	}
}

func TestDurableRelayCompatibilityRejectsNonCanonicalState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.json")
	if err := os.WriteFile(path, []byte(" {\"version\":1,\"realm_id\":\"realm-1\",\"keys\":{}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDurableRelayCompatibility(path, "realm-1"); !errors.Is(err, ErrInvalidRelayState) {
		t.Fatalf("non-canonical state error=%v", err)
	}
}
