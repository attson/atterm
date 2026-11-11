package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSessionInputHistoryKeepsPerSessionSubmissionsNewestFirst(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	history := newSessionInputHistory(func() time.Time { return now })

	history.Append("same prompt")
	now = now.Add(time.Second)
	history.Append("same prompt")
	now = now.Add(time.Second)
	history.Append("line one\nline two")

	got := history.Snapshot()
	if got.Truncated {
		t.Fatal("fresh history unexpectedly truncated")
	}
	if len(got.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(got.Items))
	}
	if got.Items[0].Text != "line one\nline two" || got.Items[1].Text != "same prompt" || got.Items[2].Text != "same prompt" {
		t.Fatalf("items are not newest-first with duplicates preserved: %+v", got.Items)
	}
	if got.Items[0].SubmittedAtUnixMS != 1_700_000_002_000 {
		t.Fatalf("newest timestamp = %d", got.Items[0].SubmittedAtUnixMS)
	}
}

func TestSessionInputHistorySupportsConcurrentAppendAndSnapshot(t *testing.T) {
	history := newSessionInputHistory(time.Now)
	var writers sync.WaitGroup
	for writer := 0; writer < 8; writer++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for i := 0; i < 100; i++ {
				history.Append("concurrent prompt")
				_ = history.Snapshot()
			}
		}()
	}
	writers.Wait()

	got := history.Snapshot()
	if len(got.Items) != sessionInputHistoryMaxItems || !got.Truncated {
		t.Fatalf("concurrent snapshot = items:%d truncated:%v", len(got.Items), got.Truncated)
	}
}

func TestSessionInputHistoryEvictsOldestByCountAndBytes(t *testing.T) {
	history := newSessionInputHistory(time.Now)
	for i := 0; i < sessionInputHistoryMaxItems+1; i++ {
		history.Append("x")
	}
	got := history.Snapshot()
	if len(got.Items) != sessionInputHistoryMaxItems || !got.Truncated {
		t.Fatalf("count-capped snapshot = items:%d truncated:%v", len(got.Items), got.Truncated)
	}

	large := newSessionInputHistory(time.Now)
	large.Append(strings.Repeat("你", sessionInputHistoryMaxBytes))
	got = large.Snapshot()
	if len(got.Items) != 1 || !got.Truncated {
		t.Fatalf("oversized snapshot = items:%d truncated:%v", len(got.Items), got.Truncated)
	}
	if len([]byte(got.Items[0].Text)) > sessionInputHistoryMaxBytes {
		t.Fatalf("oversized item kept %d bytes", len([]byte(got.Items[0].Text)))
	}
	if !strings.HasSuffix(got.Items[0].Text, "你") {
		t.Fatalf("UTF-8 truncation split a rune: %q", got.Items[0].Text[len(got.Items[0].Text)-8:])
	}
}

func TestSessionInputHistoryRestoreKeepsNewGenerationAfterOlderRecords(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	history := newSessionInputHistory(func() time.Time { return now })
	history.Append("new generation")
	history.Restore(SessionInputHistoryResult{
		Items: []SessionInputHistoryItem{
			{Text: "previous newest", SubmittedAtUnixMS: 1_699_999_999_000},
			{Text: "previous oldest", SubmittedAtUnixMS: 1_699_999_998_000},
		},
	})

	got := history.Snapshot()
	if len(got.Items) != 3 ||
		got.Items[0].Text != "new generation" ||
		got.Items[1].Text != "previous newest" ||
		got.Items[2].Text != "previous oldest" {
		t.Fatalf("restored order = %+v", got.Items)
	}
}

func TestAppGetSessionInputHistoryIsSessionScoped(t *testing.T) {
	firstID := uuid.New()
	secondID := uuid.New()
	host := &relayHost{sessions: map[uuid.UUID]*activeSession{
		firstID:  {inputHistory: newSessionInputHistory(time.Now)},
		secondID: {inputHistory: newSessionInputHistory(time.Now)},
	}}
	host.sessions[firstID].inputHistory.Append("first only")
	host.sessions[secondID].inputHistory.Append("second only")
	app := &App{host: host}

	got, err := app.GetSessionInputHistory(firstID.String())
	if err != nil {
		t.Fatalf("GetSessionInputHistory: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Text != "first only" {
		t.Fatalf("session-scoped result = %+v", got)
	}
	if _, err := app.GetSessionInputHistory(uuid.NewString()); err == nil {
		t.Fatal("unknown session should fail")
	}
}

func TestRelayHostRecordsExecutedShellCommandsInSessionHistory(t *testing.T) {
	host := newTestRelayHost(t)
	id, err := host.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/cat",
		Cwd:     t.TempDir(),
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess, ok := host.server.Registry().Get(id)
	if !ok {
		t.Fatalf("session %s not found", id)
	}

	sess.PushOut(1, []byte("unsubmitted draft\x1b]133;A\x07"))
	sess.PushOut(2, []byte("\x1b]133;C;ls -la\x07"))
	sess.PushOut(3, []byte("\x1b]133;D;0\x07"))

	got, err := host.sessionInputHistory(id)
	if err != nil {
		t.Fatalf("sessionInputHistory: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Text != "ls -la" {
		t.Fatalf("shell command history = %+v", got.Items)
	}
}

func TestRelayHostRestoresPersistedInputHistoryBeforeNewSessionReturns(t *testing.T) {
	host := newTestRelayHost(t)
	testHome := os.Getenv("HOME")
	store, err := newSessionInputHistoryStore(filepath.Join(testHome, "input-history"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	host.SetSessionInputHistoryStore(store)

	previousID := uuid.New()
	previous := newSessionInputHistory(time.Now)
	previous.Append("before restart")
	store.Queue(previousID, previous)
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}

	currentID, err := host.NewSession(context.Background(), NewSessionReq{
		Command:                   "/bin/cat",
		Cwd:                       testHome,
		Cols:                      80,
		Rows:                      24,
		Recovered:                 true,
		InputHistoryFromSessionID: previousID.String(),
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { _ = host.CloseSession(currentID) })

	got, err := host.sessionInputHistory(currentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Text != "before restart" {
		t.Fatalf("restored history at NewSession return = %+v", got.Items)
	}
}
