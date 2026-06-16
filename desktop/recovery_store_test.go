package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempRecoveryStore(t *testing.T) *RecoveryStore {
	t.Helper()
	dir := t.TempDir()
	return &RecoveryStore{
		path:    filepath.Join(dir, "recovery.json"),
		hostID:  "host-A",
		nowUnix: func() int64 { return 1750000000 },
	}
}

func TestRecoveryStore_SaveLoad_RoundTrip(t *testing.T) {
	rs := tempRecoveryStore(t)
	snap := RecoverySnapshot{
		Version:       recoverySnapshotVersion,
		HostID:        "host-A",
		CleanShutdown: true,
		SavedAtUnix:   1750000000,
		ActiveTabID:   "t-1",
		Tabs: []TabSnapshot{
			{
				ID: "t-1", Layout: "single", ColRatio: 0.5, RowRatio: 0.5,
				Panes: []PaneSnapshot{
					{Slot: 0, Shell: "/bin/zsh", LastCwd: "/Users/x"},
				},
			},
		},
	}
	if err := rs.Save(snap); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := rs.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.HostID != "host-A" || len(got.Tabs) != 1 || got.Tabs[0].Panes[0].LastCwd != "/Users/x" {
		t.Fatalf("Load round-trip mismatch: %+v", got)
	}
}

func TestRecoveryStore_Load_RejectsWrongHostID(t *testing.T) {
	rs := tempRecoveryStore(t)
	bad := RecoverySnapshot{Version: 1, HostID: "host-B", SavedAtUnix: 1750000000, Tabs: []TabSnapshot{{ID: "t"}}}
	blob, _ := json.Marshal(bad)
	if err := os.WriteFile(rs.path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := rs.Load()
	if err != nil || len(got.Tabs) != 0 {
		t.Fatalf("expected empty snapshot on host mismatch, got %+v err=%v", got, err)
	}
	if _, err := os.Stat(rs.path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, err=%v", err)
	}
}

func TestRecoveryStore_Load_RejectsExpired(t *testing.T) {
	rs := tempRecoveryStore(t)
	rs.nowUnix = func() int64 { return 1750000000 }
	old := RecoverySnapshot{Version: 1, HostID: "host-A", SavedAtUnix: 1750000000 - int64(15*24*time.Hour/time.Second), Tabs: []TabSnapshot{{ID: "t"}}}
	blob, _ := json.Marshal(old)
	_ = os.WriteFile(rs.path, blob, 0o600)
	got, _ := rs.Load()
	if len(got.Tabs) != 0 {
		t.Fatalf("expected empty snapshot on TTL miss, got %+v", got)
	}
}

func TestRecoveryStore_Load_RejectsBadJSON(t *testing.T) {
	rs := tempRecoveryStore(t)
	_ = os.WriteFile(rs.path, []byte("not json"), 0o600)
	got, _ := rs.Load()
	if len(got.Tabs) != 0 {
		t.Fatalf("expected empty snapshot on bad JSON, got %+v", got)
	}
}

func TestRecoveryStore_Load_WritesCleanShutdownFalse(t *testing.T) {
	rs := tempRecoveryStore(t)
	snap := RecoverySnapshot{Version: 1, HostID: "host-A", CleanShutdown: true, SavedAtUnix: 1750000000, Tabs: []TabSnapshot{{ID: "t"}}}
	_ = rs.Save(snap)
	got, _ := rs.Load()
	if !got.CleanShutdown {
		t.Fatalf("Load should return original clean_shutdown=true to caller, got false")
	}
	// File on disk must now have clean_shutdown=false
	blob, _ := os.ReadFile(rs.path)
	var ondisk RecoverySnapshot
	_ = json.Unmarshal(blob, &ondisk)
	if ondisk.CleanShutdown {
		t.Fatalf("on-disk clean_shutdown must be false after Load")
	}
}

func TestRecoveryStore_Discard_RemovesFile(t *testing.T) {
	rs := tempRecoveryStore(t)
	_ = rs.Save(RecoverySnapshot{Version: 1, HostID: "host-A", SavedAtUnix: 1750000000, Tabs: []TabSnapshot{{ID: "t"}}})
	if err := rs.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if _, err := os.Stat(rs.path); !os.IsNotExist(err) {
		t.Fatalf("expected file gone, err=%v", err)
	}
}

func TestRecoveryStore_Save_TooLargeRejected(t *testing.T) {
	rs := tempRecoveryStore(t)
	huge := make([]PaneSnapshot, 0, 10000)
	for i := 0; i < 10000; i++ {
		huge = append(huge, PaneSnapshot{Slot: i, Shell: "/bin/zsh", LastCwd: "/path/abcdefghijklmnopqrstuvwxyz/" + filepath.Join(filepath.Base(t.TempDir()), "x")})
	}
	snap := RecoverySnapshot{Version: 1, HostID: "host-A", SavedAtUnix: 1750000000, Tabs: []TabSnapshot{{ID: "t", Panes: huge}}}
	if err := rs.Save(snap); err == nil {
		t.Fatalf("expected size guard to reject")
	}
}
