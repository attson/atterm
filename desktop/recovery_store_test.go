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

func TestRecoveryStore_SaveLoad_RoundTripRemotePane(t *testing.T) {
	// Regression for "remote session recovery becomes a local default session":
	// PaneSnapshot must round-trip Remote/HostID/SessionID so executeRestore
	// can branch on them instead of forking a fresh local shell.
	rs := tempRecoveryStore(t)
	snap := RecoverySnapshot{
		Version:       recoverySnapshotVersion,
		HostID:        "host-A",
		CleanShutdown: true,
		SavedAtUnix:   1750000000,
		Tabs: []TabSnapshot{
			{
				ID: "t-1", Layout: "single", ColRatio: 0.5, RowRatio: 0.5,
				Panes: []PaneSnapshot{
					{
						Slot:      0,
						Remote:    true,
						HostID:    "host-B365",
						SessionID: "remote-sid-42",
						Shell:     "zsh",
						LastCwd:   "/home/u/proj",
						Title:     "proj — vim",
					},
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
	if len(got.Tabs) != 1 || len(got.Tabs[0].Panes) != 1 {
		t.Fatalf("shape mismatch: %+v", got)
	}
	p := got.Tabs[0].Panes[0]
	if !p.Remote || p.HostID != "host-B365" || p.SessionID != "remote-sid-42" {
		t.Fatalf("remote pane round-trip mismatch: %+v", p)
	}
}

func TestRecoveryStore_Load_OldSnapshotWithoutRemoteFields(t *testing.T) {
	// Forward-compat: a snapshot written by a build that predates the
	// Remote/HostID/SessionID fields must still load (Remote defaults to
	// false so the pane goes through the local-spawn path, matching the
	// pre-fix behavior).
	rs := tempRecoveryStore(t)
	legacy := []byte(`{
		"version": 1,
		"host_id": "host-A",
		"clean_shutdown": false,
		"saved_at_unix": 1750000000,
		"tabs": [{
			"id": "t-1", "layout": "single", "active_pane_idx": 0,
			"col_ratio": 0.5, "row_ratio": 0.5,
			"panes": [{"slot": 0, "shell": "/bin/zsh", "last_cwd": "/Users/x"}]
		}]
	}`)
	if err := os.WriteFile(rs.path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := rs.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Tabs) != 1 || len(got.Tabs[0].Panes) != 1 {
		t.Fatalf("shape mismatch: %+v", got)
	}
	p := got.Tabs[0].Panes[0]
	if p.Remote || p.SessionID != "" || p.HostID != "" {
		t.Fatalf("legacy pane should default to local, got: %+v", p)
	}
	if p.Shell != "/bin/zsh" {
		t.Fatalf("legacy shell lost: %+v", p)
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
