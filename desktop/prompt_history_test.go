package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func tempPromptHistoryStore(t *testing.T) (*PromptHistoryStore, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	store := newPromptHistoryStore(filepath.Join(t.TempDir(), "prompt-history.json"), func() time.Time {
		return now
	})
	return store, &now
}

func TestPromptHistoryStore_QueryUsesUnicodePrefixAndExcludesCaseFoldedExactMatch(t *testing.T) {
	store, _ := tempPromptHistoryStore(t)
	for _, prompt := range []string{"Push", "推送并创建 MR", "推送并运行测试", "   "} {
		if err := store.Record(prompt); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.Query("push", 3); len(got) != 0 {
		t.Fatalf("ASCII case-only exact match should be hidden, got %q", got)
	}
	if got := store.Query("推送", 3); len(got) != 2 {
		t.Fatalf("Unicode prefix returned %q, want two matches", got)
	}
	if status := store.Status(); status.Count != 3 {
		t.Fatalf("whitespace-only prompt should be ignored, status=%+v", status)
	}
}

func TestPromptHistoryStore_ConcurrentRecordKeepsAllCounts(t *testing.T) {
	store, _ := tempPromptHistoryStore(t)
	const workers = 24
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(workers)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			if err := store.Record("review concurrent changes"); err != nil {
				t.Errorf("Record: %v", err)
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()

	blob, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	var doc promptHistoryDocument
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) != 1 || doc.Entries[0].Count != workers {
		t.Fatalf("persisted concurrent count = %+v, want %d", doc.Entries, workers)
	}
}

func TestPromptHistoryStore_QueryAndStatusHideEntriesThatExpireWhileRunning(t *testing.T) {
	store, now := tempPromptHistoryStore(t)
	if err := store.Record("review an old prompt"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(promptHistoryTTL + time.Second)
	if got := store.Query("re", 3); len(got) != 0 {
		t.Fatalf("expired Query = %q", got)
	}
	if status := store.Status(); status.Count != 0 {
		t.Fatalf("expired Status = %+v", status)
	}
}

func TestPromptHistoryStore_WriteFailureKeepsMemoryUsable(t *testing.T) {
	dir := t.TempDir()
	blockedParent := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newPromptHistoryStore(filepath.Join(blockedParent, "prompt-history.json"), time.Now)
	if err := store.Record("memory still works"); err == nil {
		t.Fatal("Record should surface persistence failure")
	}
	if got := store.Query("me", 3); !reflect.DeepEqual(got, []string{"memory still works"}) {
		t.Fatalf("Query after write failure = %q", got)
	}
}

func TestPromptHistoryStore_SettingWriteFailuresRollBackMemory(t *testing.T) {
	dir := t.TempDir()
	blockedParent := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newPromptHistoryStore(filepath.Join(blockedParent, "prompt-history.json"), time.Now)
	if err := store.Record("review retained prompt"); err == nil {
		t.Fatal("Record should surface persistence failure")
	}
	if err := store.SetEnabled(false); err == nil {
		t.Fatal("SetEnabled should surface persistence failure")
	}
	if status := store.Status(); !status.Enabled || status.Count != 1 {
		t.Fatalf("status after failed SetEnabled = %+v", status)
	}
	if err := store.Clear(); err == nil {
		t.Fatal("Clear should surface persistence failure")
	}
	if got := store.Query("re", 3); !reflect.DeepEqual(got, []string{"review retained prompt"}) {
		t.Fatalf("Query after failed Clear = %q", got)
	}
}

func TestPromptHistoryStore_RecordAndQueryRanksByFrequencyThenRecency(t *testing.T) {
	store, now := tempPromptHistoryStore(t)
	for _, prompt := range []string{
		"push and create mr",
		"push and run tests",
		"push and create mr",
		"push and inspect logs",
	} {
		if err := store.Record(prompt); err != nil {
			t.Fatalf("Record(%q): %v", prompt, err)
		}
		*now = now.Add(time.Minute)
	}

	got := store.Query("Pu", 3)
	want := []string{
		"push and create mr",
		"push and inspect logs",
		"push and run tests",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Query ranking mismatch:\n got=%q\nwant=%q", got, want)
	}
	if exact := store.Query("push and create mr", 3); len(exact) != 0 {
		t.Fatalf("exact prompt must not be suggested, got %q", exact)
	}
}

func TestPromptHistoryStore_EnforcesEnabledLimitsTTLAndPermissions(t *testing.T) {
	store, now := tempPromptHistoryStore(t)
	if status := store.Status(); !status.Enabled || status.Count != 0 {
		t.Fatalf("fresh status = %+v, want enabled empty", status)
	}
	if err := store.SetEnabled(false); err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}
	if err := store.Record("disabled prompt"); err != nil {
		t.Fatalf("disabled Record: %v", err)
	}
	if got := store.Query("di", 3); len(got) != 0 {
		t.Fatalf("disabled store returned %q", got)
	}
	if err := store.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	if err := store.Record(strings.Repeat("x", promptHistoryMaxPromptBytes+1)); err != nil {
		t.Fatalf("oversize Record should be ignored, got %v", err)
	}
	if err := store.Record("old prompt"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(promptHistoryTTL + time.Second)
	if err := store.Record("new prompt"); err != nil {
		t.Fatal(err)
	}
	if status := store.Status(); status.Count != 1 {
		t.Fatalf("status after TTL prune = %+v, want one entry", status)
	}
	for i := 0; i < promptHistoryMaxEntries+5; i++ {
		*now = now.Add(time.Second)
		if err := store.Record("entry " + strings.Repeat("0", i%5) + time.Unix(int64(i), 0).Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	if status := store.Status(); status.Count != promptHistoryMaxEntries {
		t.Fatalf("capacity count = %d, want %d", status.Count, promptHistoryMaxEntries)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("history permissions = %o, want 600", got)
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if status := store.Status(); !status.Enabled || status.Count != 0 {
		t.Fatalf("status after clear = %+v", status)
	}
}

func TestPromptHistoryStore_LoadsPersistedDataAndToleratesCorruption(t *testing.T) {
	store, now := tempPromptHistoryStore(t)
	if err := store.Record("review this diff"); err != nil {
		t.Fatal(err)
	}
	reloaded := newPromptHistoryStore(store.path, func() time.Time { return *now })
	if got := reloaded.Query("re", 3); !reflect.DeepEqual(got, []string{"review this diff"}) {
		t.Fatalf("reloaded Query = %q", got)
	}

	if err := os.WriteFile(store.path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt := newPromptHistoryStore(store.path, func() time.Time { return *now })
	if status := corrupt.Status(); !status.Enabled || status.Count != 0 {
		t.Fatalf("corrupt file status = %+v, want enabled empty", status)
	}
}
