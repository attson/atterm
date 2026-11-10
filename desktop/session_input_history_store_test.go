package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSessionInputHistoryStorePersistsBoundedSnapshot(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	dir := filepath.Join(t.TempDir(), "history")
	store, err := newSessionInputHistoryStore(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("history directory mode = %o, want 700", got)
	}
	id := uuid.New()
	history := newSessionInputHistory(func() time.Time { return now })
	history.setOnChange(func() { store.Queue(id, history) })
	history.Append("first")
	now = now.Add(time.Second)
	history.Append("second\nline")
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, id.String()+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("history file mode = %o, want 600", got)
	}

	reopened, err := newSessionInputHistoryStore(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, found, err := reopened.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found || len(got.Items) != 2 {
		t.Fatalf("persisted history = found:%v result:%+v", found, got)
	}
	if got.Items[0].Text != "second\nline" || got.Items[1].Text != "first" {
		t.Fatalf("persisted items = %+v", got.Items)
	}
}

func TestSessionInputHistoryStoreRejectsExpiredDocument(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	dir := filepath.Join(t.TempDir(), "history")
	store, err := newSessionInputHistoryStore(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := uuid.New()
	blob, err := json.Marshal(persistedSessionInputHistory{
		Version:     sessionInputHistoryStoreVersion,
		SavedAtUnix: now.Add(-recoveryTTLDuration - time.Second).Unix(),
		Items:       []SessionInputHistoryItem{{Text: "expired", SubmittedAtUnixMS: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path(id), blob, 0o600); err != nil {
		t.Fatal(err)
	}

	got, found, err := store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if found || len(got.Items) != 0 {
		t.Fatalf("expired history loaded: found:%v result:%+v", found, got)
	}
	if _, err := os.Stat(store.path(id)); !os.IsNotExist(err) {
		t.Fatalf("expired history file was not removed: %v", err)
	}
}

func TestRestoreSessionInputHistoryMergesPreviousAndLiveGeneration(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store, err := newSessionInputHistoryStore(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	previousID := uuid.New()
	previous := newSessionInputHistory(func() time.Time { return now })
	previous.Append("older")
	now = now.Add(time.Second)
	previous.Append("previous newest")
	store.Queue(previousID, previous)
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}

	currentID := uuid.New()
	host := &relayHost{
		sessions:          make(map[uuid.UUID]*activeSession),
		inputHistoryStore: store,
	}
	current := host.newSessionInputHistory(currentID)
	current.Append("automatic resume")
	host.sessions[currentID] = &activeSession{inputHistory: current}
	app := &App{host: host}
	if err := app.RestoreSessionInputHistory(previousID.String(), currentID.String()); err != nil {
		t.Fatalf("RestoreSessionInputHistory: %v", err)
	}

	got := current.Snapshot()
	if len(got.Items) != 3 {
		t.Fatalf("merged items = %+v", got.Items)
	}
	if got.Items[0].Text != "automatic resume" ||
		got.Items[1].Text != "previous newest" ||
		got.Items[2].Text != "older" {
		t.Fatalf("merged order = %+v", got.Items)
	}
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	persisted, found, err := store.Load(currentID)
	if err != nil || !found || len(persisted.Items) != 3 {
		t.Fatalf("restored target was not persisted: found:%v result:%+v err:%v", found, persisted, err)
	}
}
