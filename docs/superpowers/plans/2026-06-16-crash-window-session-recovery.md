# Crash Window Session Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the desktop app restore the previous window's tab/pane layout (with cwd) on relaunch, and auto-resume claude/codex AI sessions via externally observed AI-side session IDs.

**Architecture:** Atomic JSON snapshot under `~/.config/atterm/recovery.json` written debounced on tab/pane changes. On launch a confirmation dialog asks the user before respawning each pane (shell, with cwd) and injecting the AI resume command after the first prompt-ready signal. AI session IDs are captured by fs-watching claude/codex data dirs (snapshot before fork, poll after). Two-phase `clean_shutdown` flag distinguishes crash vs clean exit in the dialog copy.

**Tech Stack:** Go (Wails v2), Vue 3 + TS + xterm.js, no new external dependencies. Reuses `uuid`, `hostid`, `internal/session` ClassifyCommand semantics, existing OSC 133;A → task_state path.

**Spec:** [`docs/superpowers/specs/2026-06-16-crash-window-session-recovery-design.md`](../specs/2026-06-16-crash-window-session-recovery-design.md)

---

## File Structure

**New files:**

| Path | Responsibility |
|---|---|
| `desktop/recovery_types.go` | Pure data types: `RecoverySnapshot`, `TabSnapshot`, `PaneSnapshot`, `AIInfo` |
| `desktop/recovery_store.go` | `RecoveryStore` — atomic Save / Load / Discard / MarkCleanShutdown |
| `desktop/recovery_store_test.go` | RecoveryStore unit tests |
| `desktop/ai_sid_parse.go` | `claudeParseSid`, `codexParseSid`, `claudeWatchDir`, `codexWatchDir` (pure functions) |
| `desktop/ai_sid_parse_test.go` | Pure-function tests for the above |
| `desktop/ai_sid_sniff.go` | `aiSniffers` map + `sniffAISessionID` polling loop |
| `desktop/ai_sid_sniff_test.go` | Sniff loop tests with fake dirs |
| `desktop/frontend/src/lib/aiKind.ts` | Frontend mirror of Go ClassifyCommand for {claude,codex,aider} |
| `desktop/frontend/src/lib/__tests__/aiKind.test.ts` | aiKind tests |
| `desktop/frontend/src/composables/useRecoverySnapshot.ts` | Debounced snapshot builder + event listener |
| `desktop/frontend/src/composables/__tests__/useRecoverySnapshot.test.ts` | Composable tests |
| `desktop/frontend/src/components/RecoveryDialog.vue` | Recovery dialog UI |
| `desktop/frontend/src/components/__tests__/RecoveryDialog.test.ts` | Dialog tests |
| `desktop/frontend/src/lib/recoveryRestore.ts` | `executeRestore`, `scheduleResumeInject`, `awaitFirstPromptReady` |
| `desktop/frontend/src/lib/__tests__/recoveryRestore.test.ts` | Restore-flow tests |

**Modified files:**

| Path | Change |
|---|---|
| `desktop/app.go` | Add 4 bindings (LoadRecoverySnapshot/SaveRecoverySnapshot/DiscardRecoverySnapshot/MarkCleanShutdown), extend `NewSessionReq`, add `aiSidCaptured` event emitter |
| `desktop/relay_host.go` | `NewSession` accepts AIKind and kicks off sniff goroutine via callback |
| `desktop/main.go` | OnBeforeClose calls MarkCleanShutdown synchronously before allowing close |
| `desktop/config.go` | Add `RecoveryDialogEnabled *bool` field + accessor |
| `desktop/frontend/src/lib/api.ts` | Add Wails bindings + extend `NewSessionReq` interface |
| `desktop/frontend/src/App.vue` | `loadRecoverySnapshot` bootStage; replace unconditional `startNewTab` with dialog branch; integrate useRecoverySnapshot |
| `desktop/frontend/src/components/SettingsGeneral.vue` | Add recovery_dialog_enabled toggle |
| `desktop/frontend/src/i18n/messages/en.ts` | Add `recovery.*` + `settings.general.recoveryEnabled*` keys |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | Same keys, Chinese copy |

**Dependencies (build order):**

```
T1 types  ──┬─▶ T2 store ──▶ T3 store tests pass ──┐
            │                                       │
T4 parsers ─┴─▶ T5 sniff ──▶ T6 sniff tests pass ──┤
                                                    │
                                                    ▼
                              T7 app bindings + NewSession wiring
                              T8 main.go OnBeforeClose
                              T9 config RecoveryDialogEnabled
                                                    │
                                                    ▼
                              T10 frontend api.ts + aiKind.ts
                              T11 useRecoverySnapshot composable
                              T12 RecoveryDialog.vue
                              T13 recoveryRestore.ts
                              T14 SettingsGeneral toggle + i18n
                              T15 App.vue integration
                              T16 manual e2e + verify build
```

---

### Task 1: Recovery data types

**Files:**
- Create: `desktop/recovery_types.go`

- [ ] **Step 1: Write the types file**

```go
package main

// recoverySnapshotVersion is bumped whenever the on-disk JSON shape
// changes incompatibly. RecoveryStore.Load treats unknown versions
// as "no snapshot" (and deletes the file) — see §11 of the spec.
const recoverySnapshotVersion = 1

// RecoverySnapshot is the entire ~/.config/atterm/recovery.json document.
// Field tags use snake_case to match the spec; Wails/JSON wire shape
// is the canonical form so the frontend can JSON.parse it directly.
type RecoverySnapshot struct {
	Version       int            `json:"version"`
	HostID        string         `json:"host_id"`
	CleanShutdown bool           `json:"clean_shutdown"`
	SavedAtUnix   int64          `json:"saved_at_unix"`
	ActiveTabID   string         `json:"active_tab_id,omitempty"`
	Tabs          []TabSnapshot  `json:"tabs"`
}

// TabSnapshot mirrors the frontend `Tab` type. Layout / col_ratio / row_ratio
// are restored verbatim; `id` is only used to map ActiveTabID → restored tab,
// the restored tab gets a fresh frontend id.
type TabSnapshot struct {
	ID            string         `json:"id"`
	Layout        string         `json:"layout"`         // single | vertical | horizontal | grid2x2
	ActivePaneIdx int            `json:"active_pane_idx"`
	ColRatio      float64        `json:"col_ratio"`
	RowRatio      float64        `json:"row_ratio"`
	Panes         []PaneSnapshot `json:"panes"`
}

// PaneSnapshot describes a single pane. `shell` is the binary forked at
// NewSession time (i.e. the PTY child). `last_command_line` is the most
// recent OSC 133;C payload — used by aider for resume and by the dialog
// for display.
type PaneSnapshot struct {
	Slot            int     `json:"slot"`
	Shell           string  `json:"shell"`
	ShellArgs       []string `json:"shell_args,omitempty"`
	LastCwd         string  `json:"last_cwd,omitempty"`
	SessionType     string  `json:"session_type,omitempty"` // shell | ai | test | build | deploy
	LastCommandLine string  `json:"last_command_line,omitempty"`
	Title           string  `json:"title,omitempty"`
	AI              *AIInfo `json:"ai,omitempty"`
}

// AIInfo carries the externally observed AI-side session ID (claude/codex
// only). SessionID may be empty when sniffing timed out; aider always has
// an empty SessionID because aider resumes by cwd, not by ID.
type AIInfo struct {
	Kind            string `json:"kind"`              // claude | codex | aider
	SessionID       string `json:"session_id,omitempty"`
	CapturedAtUnix  int64  `json:"captured_at_unix,omitempty"`
}
```

- [ ] **Step 2: Verify compile**

Run: `go build -tags webkit2_41 ./desktop/`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add desktop/recovery_types.go
git commit -m "feat(recovery): data types for recovery.json snapshot"
```

---

### Task 2: RecoveryStore — Save / Load / Discard

**Files:**
- Create: `desktop/recovery_store.go`
- Create: `desktop/recovery_store_test.go`

- [ ] **Step 1: Write failing tests**

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags webkit2_41 -run 'TestRecoveryStore' ./desktop/ -v`
Expected: FAIL — `RecoveryStore` undefined.

- [ ] **Step 3: Implement RecoveryStore**

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const (
	recoveryTTLDuration = 14 * 24 * time.Hour
	recoveryMaxBytes    = 256 * 1024
)

var errSnapshotTooLarge = errors.New("recovery snapshot exceeds size cap")

// RecoveryStore owns the on-disk recovery.json document. All methods are
// safe to call from any goroutine; internal serialization is via the file
// system rename. Caller controls path + host id to keep tests fully
// isolated from real ~/.config.
type RecoveryStore struct {
	path    string
	hostID  string
	nowUnix func() int64
}

// NewRecoveryStore wires a store to ~/.config/atterm/recovery.json. Used by
// production code; tests build their own with tempRecoveryStore.
func NewRecoveryStore(hostID string) (*RecoveryStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "atterm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &RecoveryStore{
		path:    filepath.Join(dir, "recovery.json"),
		hostID:  hostID,
		nowUnix: func() int64 { return time.Now().Unix() },
	}, nil
}

// Save atomically writes snap. Caller is expected to set Version, HostID,
// SavedAtUnix; we don't override them so MarkCleanShutdown can re-Save the
// exact loaded snapshot with just CleanShutdown flipped.
func (rs *RecoveryStore) Save(snap RecoverySnapshot) error {
	blob, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal recovery snapshot: %w", err)
	}
	if len(blob) > recoveryMaxBytes {
		return errSnapshotTooLarge
	}
	tmp := rs.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("write recovery tmp: %w", err)
	}
	if f, err := os.Open(tmp); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
	if err := os.Rename(tmp, rs.path); err != nil {
		return fmt.Errorf("rename recovery: %w", err)
	}
	return nil
}

// Load reads the snapshot, validating version/host/TTL. On any rejection
// it deletes the file and returns the zero RecoverySnapshot. The caller
// gets the original CleanShutdown bit (so the dialog can branch on it),
// but the on-disk file is immediately overwritten with CleanShutdown=false
// to handle a second crash mid-launch.
func (rs *RecoveryStore) Load() (RecoverySnapshot, error) {
	blob, err := os.ReadFile(rs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return RecoverySnapshot{}, nil
		}
		return RecoverySnapshot{}, err
	}
	var snap RecoverySnapshot
	if err := json.Unmarshal(blob, &snap); err != nil {
		log.Printf("recovery: discard malformed snapshot: %v", err)
		_ = os.Remove(rs.path)
		return RecoverySnapshot{}, nil
	}
	now := rs.nowUnix()
	if snap.Version != recoverySnapshotVersion ||
		snap.HostID != rs.hostID ||
		(snap.SavedAtUnix != 0 && now-snap.SavedAtUnix > int64(recoveryTTLDuration/time.Second)) {
		_ = os.Remove(rs.path)
		return RecoverySnapshot{}, nil
	}
	// Two-phase clean flag: caller sees the loaded value, on-disk file goes false.
	if snap.CleanShutdown {
		dirty := snap
		dirty.CleanShutdown = false
		if err := rs.Save(dirty); err != nil {
			log.Printf("recovery: rewrite clean_shutdown=false: %v", err)
		}
	}
	return snap, nil
}

// Discard removes the file. Missing file is not an error.
func (rs *RecoveryStore) Discard() error {
	if err := os.Remove(rs.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// MarkCleanShutdown rewrites the file with CleanShutdown=true so the next
// launch can render "last clean exit" copy in the dialog. Idempotent; no-op
// if there's no file to mark.
func (rs *RecoveryStore) MarkCleanShutdown(snap RecoverySnapshot) error {
	if len(snap.Tabs) == 0 {
		return nil
	}
	snap.CleanShutdown = true
	snap.SavedAtUnix = rs.nowUnix()
	return rs.Save(snap)
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -tags webkit2_41 -run 'TestRecoveryStore' ./desktop/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/recovery_store.go desktop/recovery_store_test.go
git commit -m "feat(recovery): atomic recovery.json store with TTL + clean_shutdown two-phase"
```

---

### Task 3: AI sniff parsers + watch-dir helpers

**Files:**
- Create: `desktop/ai_sid_parse.go`
- Create: `desktop/ai_sid_parse_test.go`

- [ ] **Step 1: Write failing tests**

```go
package main

import (
	"testing"
	"time"
)

func TestClaudeParseSid(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"valid", "0d03a640-2884-41bb-84b1-be79969a114a.jsonl", "0d03a640-2884-41bb-84b1-be79969a114a", true},
		{"wrong ext", "0d03a640-2884-41bb-84b1-be79969a114a.json", "", false},
		{"not uuid", "hello.jsonl", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sid, ok := claudeParseSid(tc.in)
			if ok != tc.ok || sid != tc.want {
				t.Fatalf("got (%q,%v) want (%q,%v)", sid, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCodexParseSid(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"valid", "rollout-2026-06-13T15-28-31-019ebfe1-df9e-79b1-881e-431db7cb6af6.jsonl", "019ebfe1-df9e-79b1-881e-431db7cb6af6", true},
		{"missing prefix", "2026-06-13T15-28-31-019ebfe1-df9e-79b1-881e-431db7cb6af6.jsonl", "", false},
		{"wrong ext", "rollout-2026-06-13T15-28-31-019ebfe1-df9e-79b1-881e-431db7cb6af6.json", "", false},
		{"short", "rollout-x.jsonl", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sid, ok := codexParseSid(tc.in)
			if ok != tc.ok || sid != tc.want {
				t.Fatalf("got (%q,%v) want (%q,%v)", sid, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestClaudeWatchDir(t *testing.T) {
	got := claudeWatchDir("/Users/me/code/foo", time.Now(), "/HOME")
	want := "/HOME/.claude/projects/-Users-me-code-foo"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCodexWatchDir(t *testing.T) {
	tm := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	got := codexWatchDir("/x", tm, "/HOME")
	want := "/HOME/.codex/sessions/2026/06/13"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
```

- [ ] **Step 2: Run tests, expect FAIL**

Run: `go test -tags webkit2_41 -run 'TestClaudeParseSid|TestCodexParseSid|TestClaudeWatchDir|TestCodexWatchDir' ./desktop/ -v`
Expected: undefined function errors.

- [ ] **Step 3: Implement**

```go
package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// claudeWatchDir returns ~/.claude/projects/<cwd-encoded>/ where Claude
// Code writes one <UUID>.jsonl per conversation. cwd-encoded replaces
// every '/' with '-'. home is injected for tests; production uses
// os.UserHomeDir().
func claudeWatchDir(cwd string, _ time.Time, home string) string {
	return filepath.Join(home, ".claude", "projects", strings.ReplaceAll(cwd, "/", "-"))
}

// codexWatchDir returns ~/.codex/sessions/YYYY/MM/DD/ keyed by the wall
// clock at fork time. Codex writes rollout-<ts>-<UUID>.jsonl files into
// this directory. now is injected for tests.
func codexWatchDir(_ string, now time.Time, home string) string {
	y, m, d := now.Date()
	return filepath.Join(home, ".codex", "sessions",
		fmt.Sprintf("%04d", y), fmt.Sprintf("%02d", int(m)), fmt.Sprintf("%02d", d))
}

// claudeParseSid extracts the UUID stem from a Claude jsonl filename.
// "<UUID>.jsonl" → UUID; rejects anything that doesn't pass RFC4122 parse.
func claudeParseSid(name string) (string, bool) {
	if !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	stem := strings.TrimSuffix(name, ".jsonl")
	if _, err := uuid.Parse(stem); err != nil {
		return "", false
	}
	return stem, true
}

// codexParseSid extracts the UUID tail from a Codex rollout filename of
// the form "rollout-<ISO-with-dashes>-<UUID>.jsonl". The last 36
// characters of the body (stripped of prefix/suffix) are validated as
// a UUID.
func codexParseSid(name string) (string, bool) {
	if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(name, "rollout-"), ".jsonl")
	if len(body) < 36 {
		return "", false
	}
	sid := body[len(body)-36:]
	if _, err := uuid.Parse(sid); err != nil {
		return "", false
	}
	return sid, true
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test -tags webkit2_41 -run 'TestClaudeParseSid|TestCodexParseSid|TestClaudeWatchDir|TestCodexWatchDir' ./desktop/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/ai_sid_parse.go desktop/ai_sid_parse_test.go
git commit -m "feat(recovery): AI session id parsers + watch-dir helpers"
```

---

### Task 4: AI sniff loop

**Files:**
- Create: `desktop/ai_sid_sniff.go`
- Create: `desktop/ai_sid_sniff_test.go`

- [ ] **Step 1: Write failing tests**

```go
package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAISniffer_Claude_CapturesNewFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o700)
	// Pre-existing file (must NOT be reported).
	_ = os.WriteFile(filepath.Join(dir, "deadbeef-aaaa-aaaa-aaaa-aaaaaaaaaaaa.jsonl"), nil, 0o600)

	var got atomic.Value // string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sniffAISessionIDForTest(ctx, dir, aiSniffers["claude"], 100*time.Millisecond, 3*time.Second, func(sid string) {
		got.Store(sid)
	})
	time.Sleep(150 * time.Millisecond) // let loop snapshot `before`
	_ = os.WriteFile(filepath.Join(dir, "1234abcd-1234-1234-1234-12345678abcd.jsonl"), nil, 0o600)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v := got.Load(); v != nil && v.(string) != "" {
			if v.(string) != "1234abcd-1234-1234-1234-12345678abcd" {
				t.Fatalf("got sid %q", v)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("sniff did not fire")
}

func TestAISniffer_TimeoutNoEmit(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o700)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fired := false
	done := make(chan struct{})
	go func() {
		sniffAISessionIDForTest(ctx, dir, aiSniffers["claude"], 50*time.Millisecond, 300*time.Millisecond, func(sid string) {
			fired = true
		})
		close(done)
	}()
	<-done
	if fired {
		t.Fatal("expected no emit on timeout")
	}
}

func TestAISniffer_AmbiguousNoEmit(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o700)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fired := false
	done := make(chan struct{})
	go func() {
		sniffAISessionIDForTest(ctx, dir, aiSniffers["claude"], 80*time.Millisecond, 1500*time.Millisecond, func(sid string) {
			fired = true
		})
		close(done)
	}()
	time.Sleep(150 * time.Millisecond)
	// Two new files within one tick — must abort.
	_ = os.WriteFile(filepath.Join(dir, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.jsonl"), nil, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb.jsonl"), nil, 0o600)
	<-done
	if fired {
		t.Fatal("expected no emit on ambiguous diff")
	}
}

func TestAISniffer_Cancel(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o700)
	ctx, cancel := context.WithCancel(context.Background())
	fired := false
	done := make(chan struct{})
	go func() {
		sniffAISessionIDForTest(ctx, dir, aiSniffers["claude"], 50*time.Millisecond, 5*time.Second, func(sid string) {
			fired = true
		})
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sniff did not exit on cancel")
	}
	if fired {
		t.Fatal("unexpected emit")
	}
}
```

- [ ] **Step 2: Run tests, expect FAIL**

Run: `go test -tags webkit2_41 -run 'TestAISniffer' ./desktop/ -v`
Expected: undefined `aiSniffers`, `sniffAISessionIDForTest`.

- [ ] **Step 3: Implement**

```go
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// aiSniffSpec captures everything we need to watch one CLI's data dir.
// WatchDir == nil means "do not sniff" (currently aider — it resumes by
// cwd, no UUID involved).
type aiSniffSpec struct {
	Kind       string
	WatchDir   func(cwd string, now time.Time, home string) string
	NewFile    func(name string) (sid string, ok bool)
	ResumeArgs func(sid string) []string
}

var aiSniffers = map[string]aiSniffSpec{
	"claude": {
		Kind:       "claude",
		WatchDir:   claudeWatchDir,
		NewFile:    claudeParseSid,
		ResumeArgs: func(sid string) []string { return []string{"--resume", sid} },
	},
	"codex": {
		Kind:       "codex",
		WatchDir:   codexWatchDir,
		NewFile:    codexParseSid,
		ResumeArgs: func(sid string) []string { return []string{"resume", sid} },
	},
	"aider": {
		Kind: "aider",
		// WatchDir/NewFile nil → sniffer never starts; resume falls back
		// to re-injecting last_command_line.
		ResumeArgs: func(_ string) []string { return nil },
	},
}

// startAISniff is the production entry point: home from os.UserHomeDir,
// poll cadence 100ms→3.2s exponential, total 30s budget.
func startAISniff(ctx context.Context, cwd string, kind string, onCapture func(sid string)) {
	spec, ok := aiSniffers[kind]
	if !ok || spec.WatchDir == nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("recovery: no home for AI sniff: %v", err)
		return
	}
	dir := spec.WatchDir(cwd, time.Now(), home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("recovery: mkdir %s: %v — skip sniff", dir, err)
		return
	}
	go sniffAISessionIDForTest(ctx, dir, spec, 100*time.Millisecond, 30*time.Second, onCapture)
}

// sniffAISessionIDForTest is the testable core loop. Exported only via
// test access (function name is non-Test- so it's package-visible but the
// _ForTest suffix flags it as an internal seam — production callers use
// startAISniff).
func sniffAISessionIDForTest(
	ctx context.Context,
	dir string,
	spec aiSniffSpec,
	initialInterval time.Duration,
	totalBudget time.Duration,
	onCapture func(sid string),
) {
	before := snapshotJsonlNames(dir)
	deadline := time.Now().Add(totalBudget)
	interval := initialInterval
	const maxInterval = 3200 * time.Millisecond

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
		now := snapshotJsonlNames(dir)
		diff := setDiff(now, before)
		if len(diff) >= 2 {
			log.Printf("recovery: ai sniff ambiguous (%d new files in %s) — abort", len(diff), dir)
			return
		}
		if len(diff) == 1 {
			if sid, ok := spec.NewFile(diff[0]); ok {
				onCapture(sid)
				return
			}
		}
		interval *= 2
		if interval > maxInterval {
			interval = maxInterval
		}
	}
	log.Printf("recovery: ai sniff timeout in %s", dir)
}

// snapshotJsonlNames returns the set of *.jsonl basenames in dir (or
// the empty set if dir is unreadable).
func snapshotJsonlNames(dir string) map[string]struct{} {
	out := map[string]struct{}{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out[e.Name()] = struct{}{}
	}
	return out
}

func setDiff(now, before map[string]struct{}) []string {
	out := []string{}
	for k := range now {
		if _, in := before[k]; !in {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// computeResumeArgs picks the right resume invocation for an AI pane.
// Returns nil when no resume should be injected (no kind / unknown / no
// captured sid for kinds that require one).
func computeResumeArgs(kind, sid, lastCommandLine string) []string {
	spec, ok := aiSniffers[kind]
	if !ok {
		return nil
	}
	if kind == "aider" {
		if lastCommandLine == "" {
			return nil
		}
		return []string{lastCommandLine}
	}
	if sid == "" {
		return nil
	}
	bin := kind // claude → "claude" ; codex → "codex"
	args := spec.ResumeArgs(sid)
	if args == nil {
		return nil
	}
	out := append([]string{bin}, args...)
	_ = filepath.Separator // silence unused-import in some toolchains
	return out
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test -tags webkit2_41 -run 'TestAISniffer' ./desktop/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/ai_sid_sniff.go desktop/ai_sid_sniff_test.go
git commit -m "feat(recovery): AI sid sniff loop + resume args composer"
```

---

### Task 5: Extend NewSessionReq + wire sniff into NewSession

**Files:**
- Modify: `desktop/app.go` (NewSessionReq struct)
- Modify: `desktop/relay_host.go` (NewSession spawn sniff)
- Create: `desktop/relay_host_recovery_test.go`

- [ ] **Step 1: Write failing test**

```go
// desktop/relay_host_recovery_test.go
package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRelayHost_NewSession_AIKindKicksSniff(t *testing.T) {
	h, cleanup := newTestRelayHost(t)  // existing test helper (see relay_host_test.go)
	defer cleanup()

	var mu sync.Mutex
	var sniffStarted bool
	h.startSniffFn = func(ctx context.Context, cwd, kind string, onCapture func(string)) {
		mu.Lock()
		defer mu.Unlock()
		sniffStarted = true
		_ = kind
	}

	_, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh",  // fork a benign shell
		Cwd:     t.TempDir(),
		Cols:    80, Rows: 24,
		AIKind:  "claude",
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if !sniffStarted {
		t.Fatal("expected sniff to start when AIKind is claude")
	}
}

func TestRelayHost_NewSession_NoAIKind_NoSniff(t *testing.T) {
	h, cleanup := newTestRelayHost(t)
	defer cleanup()
	var called bool
	h.startSniffFn = func(_ context.Context, _, _ string, _ func(string)) { called = true }
	_, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh", Cwd: t.TempDir(), Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if called {
		t.Fatal("sniff should not start when AIKind is empty")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test -tags webkit2_41 -run 'TestRelayHost_NewSession_AIKind' ./desktop/ -v`
Expected: FAIL — `AIKind` field undefined or `startSniffFn` undefined.

- [ ] **Step 3: Extend NewSessionReq in app.go**

Add to the existing `NewSessionReq` struct in `desktop/app.go`:

```go
type NewSessionReq struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
	Cols    uint16   `json:"cols,omitempty"`
	Rows    uint16   `json:"rows,omitempty"`

	// AIKind is set by the frontend after calling its own classifyAIKind()
	// on the user-typed command. Allowed values match the keys of
	// aiSniffers ("claude" | "codex" | "aider"). Empty disables AI behavior
	// (sniffer doesn't start, no resume metadata). Names here are kept in
	// sync with internal/session/ClassifyCommand.
	AIKind string `json:"ai_kind,omitempty"`

	// InitialAISessionID is the AI-side session id we captured before a
	// previous crash. When non-empty, the frontend is responsible for
	// PTY-writing the resume command after first prompt-ready; the Go side
	// just round-trips this value through PaneSnapshot bookkeeping. We do
	// NOT pass it as an arg to the spawned process.
	InitialAISessionID string `json:"initial_ai_session_id,omitempty"`
}
```

- [ ] **Step 4: Add startSniffFn hook in relay_host.go**

In `desktop/relay_host.go`, add field on `relayHost`:

```go
type relayHost struct {
	// ... existing fields ...
	startSniffFn func(ctx context.Context, cwd, kind string, onCapture func(sid string))
}
```

Initialize in `startRelayHost`:

```go
return &relayHost{
	// ... existing fields ...
	startSniffFn: startAISniff,
}, nil
```

In `NewSession`, after `combinedCleanup := ...` and before the `h.mu.Lock()` block, add:

```go
// AI session id sniff: snapshot the CLI's data dir before the PTY can
// write anything, then poll for a new file. The captured sid is round-
// tripped to the frontend over Wails events (see app.aiSidCaptured).
if req.AIKind != "" && h.startSniffFn != nil {
	sidCopy := id
	go h.startSniffFn(ctx, cwd, req.AIKind, func(sid string) {
		h.onAISidCaptured(sidCopy, req.AIKind, sid)
	})
}
```

Add a stub method on relayHost:

```go
// onAISidCaptured is overridden by app.go to fan the capture event out to
// the frontend. Tests can leave it nil; the sniff fires-and-forgets the
// callback when nil.
func (h *relayHost) onAISidCaptured(localSessionID uuid.UUID, kind, aiSid string) {
	if h.aiSidCallback != nil {
		h.aiSidCallback(localSessionID, kind, aiSid)
	}
}
```

Add the callback field:

```go
type relayHost struct {
	// ... existing fields ...
	aiSidCallback func(localSessionID uuid.UUID, kind, aiSid string)
}
```

- [ ] **Step 5: Run tests, expect PASS**

Run: `go test -tags webkit2_41 -run 'TestRelayHost_NewSession' ./desktop/ -v`
Expected: PASS.

- [ ] **Step 6: Run full desktop test suite to make sure nothing else broke**

Run: `go vet -tags webkit2_41 ./... && go test -tags webkit2_41 ./desktop/ -timeout 90s`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add desktop/app.go desktop/relay_host.go desktop/relay_host_recovery_test.go
git commit -m "feat(recovery): NewSession accepts AIKind and spawns sid sniff"
```

---

### Task 6: Wails bindings — Load / Save / Discard / MarkCleanShutdown

**Files:**
- Modify: `desktop/app.go`

- [ ] **Step 1: Write failing test**

Add to `desktop/app_test.go` (or new `desktop/app_recovery_test.go`):

```go
package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestApp_SaveLoadRecoverySnapshot_RoundTrip(t *testing.T) {
	app, cleanup := newTestApp(t)  // existing test helper
	defer cleanup()

	snap := RecoverySnapshot{
		Version:       recoverySnapshotVersion,
		HostID:        app.host.hostID,
		CleanShutdown: true,
		SavedAtUnix:   app.recoveryStore.nowUnix(),
		ActiveTabID:   "t-1",
		Tabs: []TabSnapshot{
			{ID: "t-1", Layout: "single", Panes: []PaneSnapshot{
				{Slot: 0, Shell: "/bin/zsh", LastCwd: "/tmp"},
			}},
		},
	}
	blob, _ := json.Marshal(snap)
	if err := app.SaveRecoverySnapshot(string(blob)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := app.LoadRecoverySnapshot()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Tabs) != 1 || got.Tabs[0].Panes[0].LastCwd != "/tmp" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestApp_DiscardRecoverySnapshot(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	_ = app.SaveRecoverySnapshot(`{"version":1,"host_id":"` + app.host.hostID + `","saved_at_unix":1750000000,"tabs":[{"id":"t","panes":[{"slot":0,"shell":"/bin/zsh"}]}]}`)
	if err := app.DiscardRecoverySnapshot(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	got, _ := app.LoadRecoverySnapshot()
	if len(got.Tabs) != 0 {
		t.Fatalf("expected empty after discard, got %+v", got)
	}
	if _, err := app.recoveryStore.Load(); err != nil {
		t.Fatalf("post-discard Load: %v", err)
	}
}

func TestApp_MarkCleanShutdown(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	_ = app.SaveRecoverySnapshot(`{"version":1,"host_id":"` + app.host.hostID + `","saved_at_unix":1750000000,"tabs":[{"id":"t","panes":[{"slot":0,"shell":"/bin/zsh"}]}]}`)
	// First Load flips clean to false:
	if _, err := app.LoadRecoverySnapshot(); err != nil {
		t.Fatal(err)
	}
	if err := app.MarkCleanShutdown(); err != nil {
		t.Fatalf("MarkCleanShutdown: %v", err)
	}
	_ = filepath.Separator
	// On disk should now have clean_shutdown=true
	got2, _ := app.LoadRecoverySnapshot()
	if !got2.CleanShutdown {
		t.Fatalf("expected on-disk clean_shutdown=true after MarkCleanShutdown, got %+v", got2)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test -tags webkit2_41 -run 'TestApp_(Save|Load|Discard|Mark)RecoverySnapshot|TestApp_MarkCleanShutdown' ./desktop/ -v`
Expected: undefined methods.

- [ ] **Step 3: Wire RecoveryStore into App + add bindings**

In `desktop/app.go`, add field to `App`:

```go
type App struct {
	// ... existing fields ...
	recoveryStore *RecoveryStore
	// lastSnapshot keeps the latest in-memory snapshot the frontend sent
	// us; MarkCleanShutdown re-saves it with CleanShutdown=true. Guarded
	// by mu.
	lastSnapshot RecoverySnapshot
}
```

In `App.startup` (after `a.host` is set):

```go
if rs, err := NewRecoveryStore(a.host.hostID); err == nil {
	a.recoveryStore = rs
} else {
	log.Printf("recovery store unavailable: %v", err)
}
// Wire the relay host's sniff callback so we can emit events to the frontend.
a.host.aiSidCallback = func(localSessionID uuid.UUID, kind, aiSid string) {
	wailsruntime.EventsEmit(a.ctx, "recovery:ai-sid", map[string]string{
		"session_id":      localSessionID.String(),
		"kind":            kind,
		"ai_session_id":   aiSid,
	})
}
```

Add bindings:

```go
// LoadRecoverySnapshot returns the most recent snapshot, or a zero value
// when there's nothing to recover (no file, version mismatch, expired,
// host mismatch). Side effect: rewrites the on-disk file with
// CleanShutdown=false so a crash during the recovery dialog is caught
// next launch.
func (a *App) LoadRecoverySnapshot() (RecoverySnapshot, error) {
	if a.recoveryStore == nil {
		return RecoverySnapshot{}, nil
	}
	snap, err := a.recoveryStore.Load()
	if err != nil {
		return RecoverySnapshot{}, err
	}
	a.mu.Lock()
	a.lastSnapshot = snap
	a.mu.Unlock()
	return snap, nil
}

// SaveRecoverySnapshot accepts a JSON-encoded RecoverySnapshot from the
// frontend (debounce-driven). Validates by unmarshalling into the typed
// struct so malformed payloads fail loudly.
func (a *App) SaveRecoverySnapshot(payload string) error {
	if a.recoveryStore == nil {
		return nil
	}
	var snap RecoverySnapshot
	if err := json.Unmarshal([]byte(payload), &snap); err != nil {
		return fmt.Errorf("decode recovery snapshot: %w", err)
	}
	// Server-side enforced fields — the frontend can pass anything but we
	// pin them to the real values before writing.
	snap.Version = recoverySnapshotVersion
	snap.HostID = a.host.hostID
	snap.SavedAtUnix = a.recoveryStore.nowUnix()
	a.mu.Lock()
	a.lastSnapshot = snap
	a.mu.Unlock()
	return a.recoveryStore.Save(snap)
}

// DiscardRecoverySnapshot removes recovery.json. Used by the dialog's
// "discard" / close-✕ paths.
func (a *App) DiscardRecoverySnapshot() error {
	if a.recoveryStore == nil {
		return nil
	}
	a.mu.Lock()
	a.lastSnapshot = RecoverySnapshot{}
	a.mu.Unlock()
	return a.recoveryStore.Discard()
}

// MarkCleanShutdown is called from OnBeforeClose right before the wails
// runtime tears the window down. It rewrites the latest snapshot with
// CleanShutdown=true so the next launch's dialog can render "last clean
// exit" copy. No-op when nothing has been saved this session.
func (a *App) MarkCleanShutdown() error {
	if a.recoveryStore == nil {
		return nil
	}
	a.mu.Lock()
	snap := a.lastSnapshot
	a.mu.Unlock()
	return a.recoveryStore.MarkCleanShutdown(snap)
}
```

Required imports at top of `app.go`: ensure `"encoding/json"` is present.

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test -tags webkit2_41 -run 'TestApp_(Save|Load|Discard|Mark)' ./desktop/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/app.go desktop/app_recovery_test.go
git commit -m "feat(recovery): Wails bindings Save/Load/Discard/MarkCleanShutdown + event emit"
```

---

### Task 7: Wire MarkCleanShutdown into OnBeforeClose

**Files:**
- Modify: `desktop/main.go`
- Modify: `desktop/app.go` (`beforeClose` helper)

- [ ] **Step 1: Write failing test**

Add to `desktop/app_recovery_test.go`:

```go
func TestApp_BeforeClose_CallsMarkClean(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	_ = app.SaveRecoverySnapshot(`{"version":1,"host_id":"` + app.host.hostID + `","saved_at_unix":1750000000,"tabs":[{"id":"t","panes":[{"slot":0,"shell":"/bin/zsh"}]}]}`)
	_, _ = app.LoadRecoverySnapshot()  // flip on-disk clean=false
	// Approve quit so beforeClose proceeds to MarkCleanShutdown.
	app.ConfirmQuit()
	// Drive the OnBeforeClose path:
	_ = app.beforeClose(app.ctx, func() {})
	got, _ := app.LoadRecoverySnapshot()
	if !got.CleanShutdown {
		t.Fatal("expected clean_shutdown=true after beforeClose+confirmed quit")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test -tags webkit2_41 -run 'TestApp_BeforeClose_CallsMarkClean' ./desktop/ -v`
Expected: FAIL.

- [ ] **Step 3: Patch beforeClose**

In `desktop/app.go::beforeClose`, find the branch that runs when `quitApproved` is true (i.e. the second close after the user clicked Quit). Right before the function returns `false` to let wails close the window, call:

```go
if err := a.MarkCleanShutdown(); err != nil {
	log.Printf("recovery: MarkCleanShutdown failed: %v", err)
}
```

The expected location: at the very end of the `quitApproved` branch, after any existing cleanup but before returning `false`.

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test -tags webkit2_41 -run 'TestApp_BeforeClose' ./desktop/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/app.go desktop/app_recovery_test.go
git commit -m "feat(recovery): mark clean shutdown before wails closes the window"
```

---

### Task 8: appConfig.RecoveryDialogEnabled + accessor

**Files:**
- Modify: `desktop/config.go`
- Modify: `desktop/config_test.go` (or add `desktop/config_recovery_test.go`)

- [ ] **Step 1: Write failing test**

Add to `desktop/config_recovery_test.go`:

```go
package main

import "testing"

func TestAppConfig_RecoveryDialogEnabledDefaultsTrue(t *testing.T) {
	c := appConfig{}
	if !c.RecoveryDialogEnabledOrDefault() {
		t.Fatal("default should be true")
	}
}

func TestAppConfig_RecoveryDialogEnabledRespectsExplicitFalse(t *testing.T) {
	v := false
	c := appConfig{RecoveryDialogEnabled: &v}
	if c.RecoveryDialogEnabledOrDefault() {
		t.Fatal("explicit false must stick")
	}
}

func TestAppConfig_RecoveryDialogEnabledRespectsExplicitTrue(t *testing.T) {
	v := true
	c := appConfig{RecoveryDialogEnabled: &v}
	if !c.RecoveryDialogEnabledOrDefault() {
		t.Fatal("explicit true must stick")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test -tags webkit2_41 -run 'TestAppConfig_RecoveryDialog' ./desktop/ -v`
Expected: undefined field.

- [ ] **Step 3: Add field + accessor**

In `desktop/config.go`, add to `appConfig`:

```go
// RecoveryDialogEnabled gates the startup recovery dialog. Nil means
// "never set" → default true. Stored as pointer so we can distinguish
// "user opted out" from "fresh install".
RecoveryDialogEnabled *bool `json:"recovery_dialog_enabled,omitempty"`
```

Add accessor method:

```go
func (c appConfig) RecoveryDialogEnabledOrDefault() bool {
	if c.RecoveryDialogEnabled == nil {
		return true
	}
	return *c.RecoveryDialogEnabled
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test -tags webkit2_41 -run 'TestAppConfig_RecoveryDialog' ./desktop/ -v`
Expected: PASS.

- [ ] **Step 5: Add Wails bindings for the toggle**

In `desktop/app.go`:

```go
// GetRecoveryDialogEnabled mirrors appConfig.RecoveryDialogEnabledOrDefault.
func (a *App) GetRecoveryDialogEnabled() bool {
	return a.cfgStore.Get().RecoveryDialogEnabledOrDefault()
}

// SetRecoveryDialogEnabled persists the user's choice.
func (a *App) SetRecoveryDialogEnabled(enabled bool) error {
	cfg := a.cfgStore.Get()
	cfg.RecoveryDialogEnabled = &enabled
	return a.cfgStore.Set(cfg)
}
```

- [ ] **Step 6: Verify build**

Run: `go build -tags webkit2_41 ./desktop/`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add desktop/config.go desktop/config_recovery_test.go desktop/app.go
git commit -m "feat(recovery): config flag + bindings for recovery dialog toggle"
```

---

### Task 9: Frontend Wails API + types

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`

- [ ] **Step 1: Extend NewSessionReq + add new types/functions**

In `desktop/frontend/src/lib/api.ts`, extend the existing `NewSessionReq` interface:

```ts
export interface NewSessionReq {
  command: string;
  args?: string[];
  cwd?: string;
  cols?: number;
  rows?: number;
  // Filled by classifyAIKind() in lib/aiKind.ts when the user-typed command
  // matches a known AI CLI. Empty value disables sniff + resume.
  ai_kind?: "claude" | "codex" | "aider" | "";
  // Round-tripped from the previous run's snapshot during executeRestore.
  // Not used by Go to spawn the child — only the frontend injects the
  // resume command after prompt-ready.
  initial_ai_session_id?: string;
}
```

Add types and functions:

```ts
export interface RecoveryAIInfo {
  kind: "claude" | "codex" | "aider";
  session_id?: string;
  captured_at_unix?: number;
}

export interface RecoveryPaneSnapshot {
  slot: number;
  shell: string;
  shell_args?: string[];
  last_cwd?: string;
  session_type?: string;
  last_command_line?: string;
  title?: string;
  ai?: RecoveryAIInfo;
}

export interface RecoveryTabSnapshot {
  id: string;
  layout: "single" | "vertical" | "horizontal" | "grid2x2";
  active_pane_idx: number;
  col_ratio: number;
  row_ratio: number;
  panes: RecoveryPaneSnapshot[];
}

export interface RecoverySnapshot {
  version: number;
  host_id: string;
  clean_shutdown: boolean;
  saved_at_unix: number;
  active_tab_id?: string;
  tabs: RecoveryTabSnapshot[];
}

declare global {
  interface Window {
    go?: { main?: { App?: any } };
  }
}

const App = () => (window as any).go?.main?.App as any;

export async function loadRecoverySnapshot(): Promise<RecoverySnapshot> {
  const a = App();
  if (!a?.LoadRecoverySnapshot) return emptyRecoverySnapshot();
  const got = await a.LoadRecoverySnapshot();
  return got ?? emptyRecoverySnapshot();
}

export async function saveRecoverySnapshot(snap: RecoverySnapshot): Promise<void> {
  const a = App();
  if (!a?.SaveRecoverySnapshot) return;
  await a.SaveRecoverySnapshot(JSON.stringify(snap));
}

export async function discardRecoverySnapshot(): Promise<void> {
  const a = App();
  if (!a?.DiscardRecoverySnapshot) return;
  await a.DiscardRecoverySnapshot();
}

export async function getRecoveryDialogEnabled(): Promise<boolean> {
  const a = App();
  if (!a?.GetRecoveryDialogEnabled) return true;
  return (await a.GetRecoveryDialogEnabled()) ?? true;
}

export async function setRecoveryDialogEnabled(enabled: boolean): Promise<void> {
  const a = App();
  if (!a?.SetRecoveryDialogEnabled) return;
  await a.SetRecoveryDialogEnabled(enabled);
}

function emptyRecoverySnapshot(): RecoverySnapshot {
  return { version: 1, host_id: "", clean_shutdown: false, saved_at_unix: 0, tabs: [] };
}
```

- [ ] **Step 2: Type-check**

Run: `cd desktop/frontend && npm run build`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/lib/api.ts
git commit -m "feat(recovery): frontend Wails bindings for recovery snapshot"
```

---

### Task 10: aiKind classifier (frontend mirror)

**Files:**
- Create: `desktop/frontend/src/lib/aiKind.ts`
- Create: `desktop/frontend/src/lib/__tests__/aiKind.test.ts`

- [ ] **Step 1: Write failing tests**

```ts
// desktop/frontend/src/lib/__tests__/aiKind.test.ts
import { describe, it, expect } from "vitest";
import { classifyAIKind } from "../aiKind";

describe("classifyAIKind", () => {
  it("identifies claude by bare name", () => {
    expect(classifyAIKind("claude")).toBe("claude");
  });
  it("identifies codex", () => {
    expect(classifyAIKind("codex resume some-id")).toBe("codex");
  });
  it("identifies aider", () => {
    expect(classifyAIKind("aider --model gpt-4")).toBe("aider");
  });
  it("strips absolute path", () => {
    expect(classifyAIKind("/opt/homebrew/bin/claude --foo")).toBe("claude");
  });
  it("strips env assigns + wrappers", () => {
    expect(classifyAIKind("ANTHROPIC_API_KEY=x sudo claude")).toBe("claude");
    expect(classifyAIKind("time codex")).toBe("codex");
  });
  it("returns empty for non-AI", () => {
    expect(classifyAIKind("/bin/zsh")).toBe("");
    expect(classifyAIKind("ls -la")).toBe("");
    expect(classifyAIKind("")).toBe("");
  });
  it("gemini is treated as non-AI (out of scope v1)", () => {
    expect(classifyAIKind("gemini")).toBe("");
  });
});
```

- [ ] **Step 2: Run, expect FAIL**

Run: `cd desktop/frontend && npm test -- aiKind`
Expected: import error.

- [ ] **Step 3: Implement**

```ts
// desktop/frontend/src/lib/aiKind.ts

// Mirrors internal/session/ClassifyCommand for the {claude, codex, aider}
// subset. v1 deliberately omits gemini — its session-id story isn't stable
// enough to sniff. See spec §2 and §10 (graceful degrade for alias miss).
const WRAPPERS = new Set(["sudo", "time", "nice", "env"]);
const ENV_ASSIGN = /^[A-Z_][A-Z0-9_]*=/;

export type AIKind = "claude" | "codex" | "aider" | "";

export function classifyAIKind(command: string): AIKind {
  let tokens = command.trim().split(/\s+/).filter(Boolean);
  while (tokens.length > 0) {
    const t = tokens[0];
    if (WRAPPERS.has(t) || ENV_ASSIGN.test(t)) {
      tokens = tokens.slice(1);
      continue;
    }
    break;
  }
  if (tokens.length === 0) return "";
  const first = tokens[0].split("/").pop() ?? "";
  switch (first) {
    case "claude":
      return "claude";
    case "codex":
      return "codex";
    case "aider":
      return "aider";
    default:
      return "";
  }
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `cd desktop/frontend && npm test -- aiKind`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/aiKind.ts desktop/frontend/src/lib/__tests__/aiKind.test.ts
git commit -m "feat(recovery): frontend aiKind classifier mirroring Go ClassifyCommand"
```

---

### Task 11: useRecoverySnapshot composable

**Files:**
- Create: `desktop/frontend/src/composables/useRecoverySnapshot.ts`
- Create: `desktop/frontend/src/composables/__tests__/useRecoverySnapshot.test.ts`

- [ ] **Step 1: Write failing tests**

```ts
// __tests__/useRecoverySnapshot.test.ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { ref, nextTick } from "vue";
import { useRecoverySnapshot } from "../useRecoverySnapshot";
import * as api from "../../lib/api";
import type { Tab } from "../../lib/types";

describe("useRecoverySnapshot", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.spyOn(api, "saveRecoverySnapshot").mockResolvedValue();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("debounces structural changes ~500ms", async () => {
    const tabs = ref<Tab[]>([]);
    const currentTabId = ref<string | null>(null);
    const sessionInfoFor = vi.fn().mockReturnValue(undefined);
    useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor });

    tabs.value.push({ id: "t1", layout: "single", panes: [{ sessionId: "s1", remote: false }], activePaneIdx: 0, colRatio: 0.5, rowRatio: 0.5 });
    await nextTick();
    vi.advanceTimersByTime(490);
    expect(api.saveRecoverySnapshot).not.toHaveBeenCalled();
    vi.advanceTimersByTime(20);
    await Promise.resolve();
    expect(api.saveRecoverySnapshot).toHaveBeenCalledTimes(1);
  });

  it("integrates recovery:ai-sid event into pane.ai", async () => {
    const tabs = ref<Tab[]>([
      { id: "t1", layout: "single", panes: [{ sessionId: "s1", remote: false }], activePaneIdx: 0, colRatio: 0.5, rowRatio: 0.5 },
    ]);
    const currentTabId = ref<string | null>("t1");
    const sessionInfoFor = vi.fn().mockImplementation((sid: string) => ({
      id: sid, command: "claude", cwd: "/x", title: "t", cols: 80, rows: 24, started_at: 0, host_id: "h",
    }));
    const handlers = new Map<string, (payload: any) => void>();
    const onEvent = (name: string, cb: (payload: any) => void) => { handlers.set(name, cb); return () => handlers.delete(name); };
    useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, onEvent });
    // Snapshot should write at least once after the initial tab assignment.
    vi.advanceTimersByTime(600);
    await Promise.resolve();
    const before = (api.saveRecoverySnapshot as any).mock.calls.length;
    const handler = handlers.get("recovery:ai-sid")!;
    handler({ session_id: "s1", kind: "claude", ai_session_id: "abc-uuid-xyz" });
    vi.advanceTimersByTime(600);
    await Promise.resolve();
    const after = (api.saveRecoverySnapshot as any).mock.calls.length;
    expect(after).toBeGreaterThan(before);
    const lastCall = (api.saveRecoverySnapshot as any).mock.calls[after - 1][0];
    expect(lastCall.tabs[0].panes[0].ai?.session_id).toBe("abc-uuid-xyz");
  });
});
```

- [ ] **Step 2: Run, expect FAIL**

Run: `cd desktop/frontend && npm test -- useRecoverySnapshot`
Expected: import error.

- [ ] **Step 3: Implement**

```ts
// desktop/frontend/src/composables/useRecoverySnapshot.ts
import { watch, onUnmounted } from "vue";
import type { Ref } from "vue";
import type { Tab } from "../lib/types";
import type { SessionInfo } from "../lib/connection";
import {
  saveRecoverySnapshot,
  type RecoverySnapshot,
  type RecoveryTabSnapshot,
  type RecoveryPaneSnapshot,
  type RecoveryAIInfo,
} from "../lib/api";
import { EventsOn } from "../../wailsjs/runtime/runtime";

// AI session id captures keyed by atterm session id. Lives outside the
// reactive store so non-structural changes don't trigger watcher re-runs.
type AIState = { kind: "claude" | "codex" | "aider"; session_id: string; captured_at_unix: number };

// Default debounce intervals. Structural changes (tabs/panes/AI capture)
// flush at 500ms; cwd/title heartbeat at 5s.
const STRUCTURAL_DEBOUNCE_MS = 500;
const HEARTBEAT_DEBOUNCE_MS = 5000;

export interface UseRecoverySnapshotArgs {
  tabs: Ref<Tab[]>;
  currentTabId: Ref<string | null>;
  sessionInfoFor: (sid: string) => SessionInfo | undefined;
  // Test seam — production code calls Wails EventsOn. Returns an off()
  // function the composable calls on unmount.
  onEvent?: (name: string, cb: (payload: any) => void) => () => void;
}

export function useRecoverySnapshot(args: UseRecoverySnapshotArgs) {
  const aiBySid = new Map<string, AIState>();
  let structuralTimer: ReturnType<typeof setTimeout> | null = null;
  let heartbeatTimer: ReturnType<typeof setTimeout> | null = null;

  function buildSnapshot(): RecoverySnapshot {
    const tabs: RecoveryTabSnapshot[] = args.tabs.value.map((t) => ({
      id: t.id,
      layout: t.layout as RecoveryTabSnapshot["layout"],
      active_pane_idx: t.activePaneIdx,
      col_ratio: t.colRatio,
      row_ratio: t.rowRatio,
      panes: t.panes.map((p, idx): RecoveryPaneSnapshot => {
        const info = p.sessionId ? args.sessionInfoFor(p.sessionId) : undefined;
        const ai = p.sessionId ? aiBySid.get(p.sessionId) : undefined;
        return {
          slot: idx,
          shell: info?.command?.split(" ")[0] ?? "",
          shell_args: [],
          last_cwd: info?.cwd ?? "",
          session_type: info?.type ?? "",
          last_command_line: info?.current_command ?? "",
          title: info?.title ?? "",
          ai: ai ? { ...ai } as RecoveryAIInfo : undefined,
        };
      }),
    })).filter((t) => t.panes.length > 0);

    return {
      version: 1,
      host_id: "",       // overridden server-side
      clean_shutdown: false,
      saved_at_unix: 0,  // overridden server-side
      active_tab_id: args.currentTabId.value ?? "",
      tabs,
    };
  }

  function flushNow() {
    if (structuralTimer) { clearTimeout(structuralTimer); structuralTimer = null; }
    if (heartbeatTimer) { clearTimeout(heartbeatTimer); heartbeatTimer = null; }
    void saveRecoverySnapshot(buildSnapshot());
  }

  function scheduleStructural() {
    if (structuralTimer) clearTimeout(structuralTimer);
    structuralTimer = setTimeout(() => { structuralTimer = null; flushNow(); }, STRUCTURAL_DEBOUNCE_MS);
  }

  function scheduleHeartbeat() {
    if (heartbeatTimer) return; // first-write-wins
    heartbeatTimer = setTimeout(() => { heartbeatTimer = null; flushNow(); }, HEARTBEAT_DEBOUNCE_MS);
  }

  // Structural watcher: deep watch on tabs collection identity + slot count.
  watch(
    () => args.tabs.value.map((t) => ({
      id: t.id, layout: t.layout, active: t.activePaneIdx, col: t.colRatio, row: t.rowRatio,
      paneIds: t.panes.map((p) => p.sessionId ?? "").join("|"),
    })),
    () => { scheduleStructural(); },
    { deep: true },
  );

  // Heartbeat watcher: snapshot identity (cwd/title/cmd would otherwise
  // be inside sessionInfoFor; re-evaluate via a manual nudge below).
  watch(args.currentTabId, () => { scheduleHeartbeat(); });

  // AI sid capture: subscribe to the Wails event and merge into aiBySid.
  const evtOn = args.onEvent ?? ((name, cb) => EventsOn(name, cb));
  const off = evtOn("recovery:ai-sid", (payload: any) => {
    const sid: string = payload?.session_id ?? "";
    const kind: "claude" | "codex" | "aider" = payload?.kind ?? "";
    const aiSid: string = payload?.ai_session_id ?? "";
    if (!sid || !kind || !aiSid) return;
    aiBySid.set(sid, { kind, session_id: aiSid, captured_at_unix: Math.floor(Date.now() / 1000) });
    scheduleStructural();
  });

  onUnmounted(() => {
    off?.();
    if (structuralTimer) clearTimeout(structuralTimer);
    if (heartbeatTimer) clearTimeout(heartbeatTimer);
  });

  return {
    buildSnapshot,
    flushNow,
    // Public hook used by App.vue when META arrives with new cwd/title/current_command.
    onMetaTouch() { scheduleHeartbeat(); },
  };
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `cd desktop/frontend && npm test -- useRecoverySnapshot`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/composables/useRecoverySnapshot.ts desktop/frontend/src/composables/__tests__/useRecoverySnapshot.test.ts
git commit -m "feat(recovery): useRecoverySnapshot composable with debounced save"
```

---

### Task 12: RecoveryDialog component

**Files:**
- Create: `desktop/frontend/src/components/RecoveryDialog.vue`
- Create: `desktop/frontend/src/components/__tests__/RecoveryDialog.test.ts`

- [ ] **Step 1: Write failing tests**

```ts
// __tests__/RecoveryDialog.test.ts
import { mount } from "@vue/test-utils";
import { describe, it, expect } from "vitest";
import RecoveryDialog from "../RecoveryDialog.vue";
import type { RecoverySnapshot } from "../../lib/api";

function makeSnap(opts: Partial<RecoverySnapshot> = {}): RecoverySnapshot {
  return {
    version: 1, host_id: "h", clean_shutdown: false, saved_at_unix: Math.floor(Date.now()/1000) - 300,
    tabs: [
      { id: "t1", layout: "single", active_pane_idx: 0, col_ratio: 0.5, row_ratio: 0.5,
        panes: [{ slot: 0, shell: "/bin/zsh", last_cwd: "/Users/x/code/foo", session_type: "shell", title: "foo" }] },
      { id: "t2", layout: "vertical", active_pane_idx: 0, col_ratio: 0.5, row_ratio: 0.5,
        panes: [
          { slot: 0, shell: "/bin/zsh", last_cwd: "/Users/x/code/bar", session_type: "ai", title: "Refactor",
            ai: { kind: "claude", session_id: "abc-123", captured_at_unix: 1750000000 } },
          { slot: 1, shell: "/bin/zsh", last_cwd: "/Users/x/code/bar", session_type: "shell", title: "bar" },
        ] },
    ],
    ...opts,
  };
}

describe("RecoveryDialog", () => {
  it("shows all tabs and counts in main button when fully selected", () => {
    const w = mount(RecoveryDialog, { props: { snapshot: makeSnap(), open: true } });
    expect(w.text()).toContain("Tab 1");
    expect(w.text()).toContain("Tab 2");
    expect(w.find('[data-testid="btn-restore"]').text()).toMatch(/2/);
  });

  it("toggling a tab off updates the count + disables when zero", async () => {
    const w = mount(RecoveryDialog, { props: { snapshot: makeSnap(), open: true } });
    const checkboxes = w.findAll('input[type=checkbox]');
    await checkboxes[0].setValue(false);
    expect(w.find('[data-testid="btn-restore"]').text()).toMatch(/1/);
    await checkboxes[1].setValue(false);
    const btn = w.find('[data-testid="btn-restore"]');
    expect(btn.attributes("disabled")).toBeDefined();
  });

  it("emits restore with picked tabs", async () => {
    const w = mount(RecoveryDialog, { props: { snapshot: makeSnap(), open: true } });
    await w.find('[data-testid="btn-restore"]').trigger("click");
    const ev = w.emitted("restore");
    expect(ev?.[0]?.[0]).toHaveLength(2);
  });

  it("emits discard from secondary button", async () => {
    const w = mount(RecoveryDialog, { props: { snapshot: makeSnap(), open: true } });
    await w.find('[data-testid="btn-discard"]').trigger("click");
    expect(w.emitted("discard")?.length).toBe(1);
  });
});
```

- [ ] **Step 2: Run, expect FAIL**

Run: `cd desktop/frontend && npm test -- RecoveryDialog`
Expected: import error.

- [ ] **Step 3: Implement**

```vue
<!-- desktop/frontend/src/components/RecoveryDialog.vue -->
<script lang="ts" setup>
import { computed, ref, watchEffect } from "vue";
import type { RecoverySnapshot, RecoveryTabSnapshot } from "../lib/api";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{ snapshot: RecoverySnapshot; open: boolean }>();
const emit = defineEmits<{
  (e: "restore", picks: RecoveryTabSnapshot[]): void;
  (e: "discard"): void;
}>();

const { t } = useI18n();

const picked = ref<Record<string, boolean>>({});
const expanded = ref<Record<string, boolean>>({});

watchEffect(() => {
  if (!props.snapshot) return;
  for (const tab of props.snapshot.tabs) {
    if (picked.value[tab.id] === undefined) picked.value[tab.id] = true;
  }
});

const minutesAgo = computed(() => {
  const now = Math.floor(Date.now() / 1000);
  const dt = Math.max(0, now - props.snapshot.saved_at_unix);
  return Math.floor(dt / 60);
});

const subtitle = computed(() =>
  props.snapshot.clean_shutdown
    ? t("recovery.dialog.subtitleClean", { minutes: minutesAgo.value })
    : t("recovery.dialog.subtitleUnclean", { minutes: minutesAgo.value })
);

const pickedCount = computed(() => props.snapshot.tabs.filter((t) => picked.value[t.id]).length);
const totalCount = computed(() => props.snapshot.tabs.length);

const restoreLabel = computed(() =>
  pickedCount.value === totalCount.value
    ? t("recovery.dialog.btnRestoreAll", { count: totalCount.value })
    : t("recovery.dialog.btnRestoreSelected", { count: pickedCount.value })
);

function paneBadge(p: RecoveryTabSnapshot["panes"][number]): string {
  if (p.session_type !== "ai" && p.last_command_line) {
    // Unclassified AI: shell pane that ran an unrecognized command (alias miss).
    const tok = p.last_command_line.split(/\s+/)[0];
    if (tok && !["claude", "codex", "aider"].includes(tok)) {
      return t("recovery.dialog.badgeUnclassified");
    }
  }
  if (p.session_type === "ai") {
    if (p.ai?.kind === "aider") return t("recovery.dialog.badgeResumable");
    if (p.ai?.session_id) return t("recovery.dialog.badgeResumable");
    return t("recovery.dialog.badgeFresh");
  }
  return t("recovery.dialog.badgeShell");
}

function tabTitle(i: number, tab: RecoveryTabSnapshot): string {
  const head = tab.panes[0];
  const cwd = head?.last_cwd?.split("/").filter(Boolean).pop() ?? "";
  return `Tab ${i + 1}` + (cwd ? ` · ${cwd}` : "");
}

function emitRestore() {
  const out = props.snapshot.tabs.filter((t) => picked.value[t.id]);
  emit("restore", out);
}
</script>

<template>
  <div v-if="open" class="recovery-dialog-backdrop" role="dialog" aria-modal="true">
    <div class="recovery-dialog">
      <header>
        <h2>{{ t("recovery.dialog.title") }}</h2>
        <p class="subtitle">{{ subtitle }}</p>
      </header>
      <ul class="tab-list">
        <li v-for="(tab, i) in snapshot.tabs" :key="tab.id">
          <label class="tab-row">
            <input type="checkbox" v-model="picked[tab.id]" />
            <button class="caret" type="button" @click="expanded[tab.id] = !expanded[tab.id]">{{ expanded[tab.id] ? "▾" : "▸" }}</button>
            <span class="tab-title">{{ tabTitle(i, tab) }}</span>
            <span class="tab-meta">{{ tab.panes.length }} {{ tab.panes.length > 1 ? "panes" : "pane" }}</span>
          </label>
          <ul v-if="expanded[tab.id]" class="pane-list">
            <li v-for="p in tab.panes" :key="p.slot">
              <span class="pane-shell">{{ p.shell.split('/').pop() || p.shell }}</span>
              <span class="pane-cwd">{{ p.last_cwd }}</span>
              <span class="pane-badge">{{ paneBadge(p) }}</span>
            </li>
          </ul>
        </li>
      </ul>
      <footer>
        <button data-testid="btn-discard" class="btn-secondary" @click="emit('discard')">{{ t("recovery.dialog.btnDiscard") }}</button>
        <button data-testid="btn-restore" class="btn-primary" :disabled="pickedCount === 0" @click="emitRestore">{{ restoreLabel }}</button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.recovery-dialog-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.45); display: flex; align-items: center; justify-content: center; z-index: 200; }
.recovery-dialog { background: var(--bg, #0d1117); color: var(--fg, #d1d5db); border-radius: 8px; min-width: 480px; max-width: 720px; max-height: 80vh; overflow: hidden; display: flex; flex-direction: column; }
.recovery-dialog header { padding: 16px 20px 0; }
.recovery-dialog h2 { font-size: 1.1rem; margin: 0; }
.subtitle { font-size: 0.85rem; opacity: 0.7; margin: 4px 0 12px; }
.tab-list { list-style: none; padding: 0 20px; margin: 0; overflow: auto; }
.tab-row { display: flex; align-items: center; gap: 8px; padding: 6px 0; cursor: default; }
.caret { background: transparent; border: 0; color: inherit; cursor: pointer; padding: 0 4px; }
.tab-title { flex: 1; }
.tab-meta { opacity: 0.6; font-size: 0.8rem; }
.pane-list { list-style: none; padding-left: 36px; margin: 4px 0 8px; font-size: 0.85rem; }
.pane-list li { display: flex; gap: 8px; padding: 2px 0; }
.pane-shell { font-family: monospace; }
.pane-cwd { opacity: 0.7; flex: 1; }
.pane-badge { opacity: 0.85; font-size: 0.75rem; }
footer { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 20px 16px; }
.btn-primary { background: #2563eb; color: white; border: 0; padding: 6px 12px; border-radius: 4px; cursor: pointer; }
.btn-primary:disabled { opacity: 0.4; cursor: not-allowed; }
.btn-secondary { background: transparent; color: inherit; border: 1px solid #444; padding: 6px 12px; border-radius: 4px; cursor: pointer; }
</style>
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `cd desktop/frontend && npm test -- RecoveryDialog`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/RecoveryDialog.vue desktop/frontend/src/components/__tests__/RecoveryDialog.test.ts
git commit -m "feat(recovery): RecoveryDialog component"
```

---

### Task 13: recoveryRestore module (executeRestore + resume inject)

**Files:**
- Create: `desktop/frontend/src/lib/recoveryRestore.ts`
- Create: `desktop/frontend/src/lib/__tests__/recoveryRestore.test.ts`

- [ ] **Step 1: Write failing tests**

```ts
// __tests__/recoveryRestore.test.ts
import { describe, it, expect } from "vitest";
import { computeResumeLine } from "../recoveryRestore";

describe("computeResumeLine", () => {
  it("claude with sid", () => {
    expect(computeResumeLine({ kind: "claude", session_id: "abc" }, "")).toBe("claude --resume abc\n");
  });
  it("codex with sid", () => {
    expect(computeResumeLine({ kind: "codex", session_id: "xyz" }, "")).toBe("codex resume xyz\n");
  });
  it("aider sends last_command_line", () => {
    expect(computeResumeLine({ kind: "aider" } as any, "aider --model gpt-4")).toBe("aider --model gpt-4\n");
  });
  it("returns null when sid missing for claude", () => {
    expect(computeResumeLine({ kind: "claude" } as any, "")).toBeNull();
  });
  it("returns null when aider has no last command", () => {
    expect(computeResumeLine({ kind: "aider" } as any, "")).toBeNull();
  });
});
```

- [ ] **Step 2: Run, expect FAIL**

Run: `cd desktop/frontend && npm test -- recoveryRestore`
Expected: import error.

- [ ] **Step 3: Implement**

```ts
// desktop/frontend/src/lib/recoveryRestore.ts
import type { RecoveryAIInfo, RecoveryTabSnapshot, NewSessionReq } from "./api";

// computeResumeLine produces the exact text (including trailing newline)
// to PTY.Write into a freshly forked shell so the AI session continues.
// Returns null when no resume should be injected.
export function computeResumeLine(ai: RecoveryAIInfo | undefined, lastCommandLine: string): string | null {
  if (!ai) return null;
  if (ai.kind === "claude" && ai.session_id) return `claude --resume ${ai.session_id}\n`;
  if (ai.kind === "codex" && ai.session_id) return `codex resume ${ai.session_id}\n`;
  if (ai.kind === "aider" && lastCommandLine) return `${lastCommandLine}\n`;
  return null;
}

// buildRestoreSessionReq turns a snapshot pane into the NewSessionReq we'll
// send to the Go side. Cols/rows are predicted by the caller (App.vue
// has the measure probe).
export function buildRestoreSessionReq(
  pane: RecoveryTabSnapshot["panes"][number],
  cols: number,
  rows: number,
): NewSessionReq {
  return {
    command: pane.shell || "/bin/sh",
    args: pane.shell_args ?? [],
    cwd: pane.last_cwd ?? "",
    cols, rows,
    ai_kind: (pane.ai?.kind ?? "") as NewSessionReq["ai_kind"],
    initial_ai_session_id: pane.ai?.session_id ?? "",
  };
}

// awaitFirstPromptReady waits for SessionInfo.task_state to become
// `waiting_input` (the post-OSC-133;A state). Returns "ready" on first
// transition, "timeout" after timeoutMs. Caller polls SessionInfo via
// onMeta (provided as get()).
export function awaitFirstPromptReady(
  get: () => string | undefined,
  timeoutMs: number = 5000,
  intervalMs: number = 80,
): Promise<"ready" | "timeout"> {
  return new Promise((resolve) => {
    const start = Date.now();
    const tick = () => {
      const s = get();
      if (s === "waiting_input") return resolve("ready");
      if (Date.now() - start >= timeoutMs) return resolve("timeout");
      setTimeout(tick, intervalMs);
    };
    tick();
  });
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `cd desktop/frontend && npm test -- recoveryRestore`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/recoveryRestore.ts desktop/frontend/src/lib/__tests__/recoveryRestore.test.ts
git commit -m "feat(recovery): recoveryRestore helpers (resume line composer + prompt-ready wait)"
```

---

### Task 14: Settings toggle + i18n keys

**Files:**
- Modify: `desktop/frontend/src/components/SettingsGeneral.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

- [ ] **Step 1: Add i18n keys (English)**

Find the right namespace block in `en.ts` (existing `settings`, `app`, etc.) and add:

```ts
// Inside the `recovery:` block (create it if it doesn't exist):
recovery: {
  dialog: {
    title: "Restore your previous session?",
    subtitleClean: "Last cleanly exited · {minutes} min ago",
    subtitleUnclean: "Last session ended unexpectedly · {minutes} min ago",
    btnRestoreAll: "Restore all {count}",
    btnRestoreSelected: "Restore {count} selected",
    btnDiscard: "Discard all",
    badgeResumable: "resumable",
    badgeFresh: "fresh (sid not captured)",
    badgeShell: "(no AI)",
    badgeUnclassified: "not classified as AI",
  },
  pane: {
    failed: "(failed)",
    resumeTimeout: "{kind} resume timed out — press ↑ to recall the last command",
  },
},
```

Inside `settings.general`:

```ts
recoveryEnabled: "Show recovery prompt at launch",
recoveryEnabledDesc: "Ask before restoring last session's tabs and panes.",
```

- [ ] **Step 2: Add same keys (Chinese) in zh-CN.ts**

```ts
recovery: {
  dialog: {
    title: "恢复上次的会话？",
    subtitleClean: "上次正常退出 · {minutes} 分钟前",
    subtitleUnclean: "上次异常结束 · {minutes} 分钟前",
    btnRestoreAll: "恢复全部 {count} 个",
    btnRestoreSelected: "恢复选中的 {count} 个",
    btnDiscard: "全部丢弃",
    badgeResumable: "可续接",
    badgeFresh: "无 sid（不自动续接）",
    badgeShell: "（无 AI）",
    badgeUnclassified: "未识别为 AI",
  },
  pane: {
    failed: "（已失败）",
    resumeTimeout: "{kind} 自动续接超时，请按 ↑ 调出上次命令",
  },
},
```

Inside `settings.general`:

```ts
recoveryEnabled: "启动时检测并提示恢复上次会话",
recoveryEnabledDesc: "在启动时询问是否恢复上次的标签页和分屏。",
```

- [ ] **Step 3: Add Settings UI**

In `desktop/frontend/src/components/SettingsGeneral.vue`, find the existing toggle list (notifications / shell integration) and add an analogous toggle. Skeleton:

```vue
<script lang="ts" setup>
// ... existing imports ...
import { getRecoveryDialogEnabled, setRecoveryDialogEnabled } from "../lib/api";

const recoveryEnabled = ref(true);

onMounted(async () => {
  // ... existing ...
  recoveryEnabled.value = await getRecoveryDialogEnabled();
});

async function onRecoveryToggle(v: boolean) {
  recoveryEnabled.value = v;
  await setRecoveryDialogEnabled(v);
}
</script>

<template>
  <!-- ... existing rows ... -->
  <div class="settings-row">
    <label class="settings-row-label">
      <span>{{ t('settings.general.recoveryEnabled') }}</span>
      <span class="settings-row-desc">{{ t('settings.general.recoveryEnabledDesc') }}</span>
    </label>
    <input type="checkbox" :checked="recoveryEnabled" @change="onRecoveryToggle(($event.target as HTMLInputElement).checked)" />
  </div>
</template>
```

(Match the existing component's exact class names and toggle widget — don't introduce a new style; reuse SelectDropdown/HotkeyCaptureCell patterns only if SettingsGeneral already uses them. Read the file before editing to keep visual consistency.)

- [ ] **Step 4: Type-check + run existing tests**

Run: `cd desktop/frontend && npm run build && npm test -- SettingsGeneral`
Expected: PASS (existing SettingsGeneral test may need a stub for `getRecoveryDialogEnabled` — add a default mock return of `true`).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts desktop/frontend/src/components/SettingsGeneral.vue desktop/frontend/src/components/SettingsGeneral.test.ts
git commit -m "feat(recovery): Settings → General toggle + i18n keys"
```

---

### Task 15: App.vue integration

**Files:**
- Modify: `desktop/frontend/src/App.vue`

This is the largest single edit; do it in three sub-steps.

- [ ] **Step 1: Add bootStage for recovery load**

In `App.vue::onMounted`, after the `getHostInfo` step and before `connectLocalSessionList`, add:

```ts
bootStage = "loadRecoverySnapshot";
const recoverySnap = await loadRecoverySnapshot();
const recoveryEnabled = await getRecoveryDialogEnabled();
```

Import the bindings at the top: `loadRecoverySnapshot`, `getRecoveryDialogEnabled`, `discardRecoverySnapshot` from `./lib/api`, plus the `RecoverySnapshot`, `RecoveryTabSnapshot` types.

Move the `if (!autoStarted) { ... startNewTab() ... }` block so it branches:

```ts
if (!autoStarted) {
  autoStarted = true;
  if (recoveryEnabled && recoverySnap.tabs.length > 0) {
    recoveryDialogState.value = { open: true, snapshot: recoverySnap };
    // Wait for user pick — startNewTab fires inside onDiscard / when no picks remain.
  } else {
    await startNewTab();
  }
}
```

Add the state:

```ts
const recoveryDialogState = ref<{ open: boolean; snapshot: RecoverySnapshot | null }>({ open: false, snapshot: null });
```

- [ ] **Step 2: Wire RecoveryDialog into the template + handlers**

In the template, add (anywhere in the top-level layout, next to `SettingsDialog`):

```vue
<RecoveryDialog
  v-if="recoveryDialogState.snapshot"
  :open="recoveryDialogState.open"
  :snapshot="recoveryDialogState.snapshot"
  @restore="onRecoveryRestore"
  @discard="onRecoveryDiscard"
/>
```

Add handlers:

```ts
async function onRecoveryRestore(picks: RecoveryTabSnapshot[]) {
  recoveryDialogState.value = { open: false, snapshot: null };
  if (picks.length === 0) {
    await startNewTab();
    return;
  }
  await executeRestore(picks);
}

async function onRecoveryDiscard() {
  recoveryDialogState.value = { open: false, snapshot: null };
  await discardRecoverySnapshot();
  await startNewTab();
}

async function executeRestore(picks: RecoveryTabSnapshot[]) {
  const newIds: string[] = [];
  for (const tab of picks) {
    const t: Tab = {
      id: newId(),
      layout: tab.layout,
      activePaneIdx: tab.active_pane_idx,
      colRatio: tab.col_ratio,
      rowRatio: tab.row_ratio,
      panes: [],
    };
    const want = PANE_COUNT[tab.layout];
    for (let i = 0; i < want; i++) {
      const snap = tab.panes.find((p) => p.slot === i);
      if (!snap) { t.panes[i] = { sessionId: null, remote: false }; continue; }
      try {
        const dims = predictCellDims(tab.layout);
        const resp = await newSession(buildRestoreSessionReq(snap, dims.cols, dims.rows));
        t.panes[i] = { sessionId: resp.session_id, remote: false };
        scheduleResumeInject(resp.session_id, snap);
      } catch (e) {
        console.warn("[recovery] pane spawn failed", e);
        t.panes[i] = { sessionId: null, remote: false };
      }
    }
    tabs.value.push(t);
    newIds.push(t.id);
  }
  // Restore active tab: map snapshot.active_tab_id → new id by position.
  const activeIdx = picks.findIndex((p) => p.id === recoveryDialogState.value.snapshot?.active_tab_id);
  if (activeIdx >= 0) gotoTab(newIds[activeIdx]);
  else if (newIds.length > 0) gotoTab(newIds[0]);
}

function scheduleResumeInject(sessionId: string, snap: RecoveryTabSnapshot["panes"][number]) {
  if (snap.session_type !== "ai") return;
  const line = computeResumeLine(snap.ai, snap.last_command_line ?? "");
  if (!line) return;
  // Read task_state via findSessionInfo; tighter integration would use
  // SessionConnection.onMeta directly, but findSessionInfo updates as
  // META frames arrive.
  awaitFirstPromptReady(() => findSessionInfo(sessionId, false)?.task_state).then((result) => {
    if (result === "timeout") {
      showToast(i18nT("recovery.pane.resumeTimeout", { kind: snap.ai?.kind ?? "" }));
      return;
    }
    sendInputToSession(localEndpoint.value, sessionId, line);
  });
}
```

Imports needed at top:
```ts
import RecoveryDialog from "./components/RecoveryDialog.vue";
import { computeResumeLine, buildRestoreSessionReq, awaitFirstPromptReady } from "./lib/recoveryRestore";
import { loadRecoverySnapshot, getRecoveryDialogEnabled, discardRecoverySnapshot, type RecoverySnapshot, type RecoveryTabSnapshot } from "./lib/api";
```

- [ ] **Step 3: Wire useRecoverySnapshot**

Add to `App.vue` setup, after `tabs` / `currentTabId` are defined:

```ts
const { onMetaTouch } = useRecoverySnapshot({
  tabs,
  currentTabId,
  sessionInfoFor: (sid: string) => findSessionInfo(sid, false),
});
```

Import: `import { useRecoverySnapshot } from "./composables/useRecoverySnapshot";`

Locate the existing onMeta handler in App.vue and call `onMetaTouch()` at the end. This drives the 5s heartbeat for cwd/title/current_command changes.

- [ ] **Step 4: Run build + tests**

Run: `cd desktop/frontend && npm run build && npm test -- App.test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/App.vue
git commit -m "feat(recovery): wire snapshot save + RecoveryDialog + executeRestore into App.vue"
```

---

### Task 16: Manual end-to-end smoke test + final build verification

**Files:** none modified (verification only)

- [ ] **Step 1: Full backend test**

Run: `go vet -tags webkit2_41 ./... && go test -tags webkit2_41 ./desktop/ -timeout 120s`
Expected: PASS.

- [ ] **Step 2: Full frontend build + test**

Run: `cd desktop/frontend && npm run build && npm test`
Expected: PASS.

- [ ] **Step 3: Launch dev app**

Run: `cd desktop && wails dev` (Linux: `wails dev -tags webkit2_41`)
Expected: app launches. No `recovery.json` exists yet → first launch creates the default single-tab session.

- [ ] **Step 4: Reproduce crash → recovery**

1. Open 3 tabs: tab 1 plain zsh, tab 2 split vertical (left zsh in `~/code/foo`, right `claude` in `~/code/bar`), tab 3 `codex` in `~/code/baz`.
2. Wait ~3s for the snapshot to debounce-write.
3. From another shell: `pkill -9 -f 'AT Term'` (or use `kill -9 <pid>`).
4. Relaunch the app.
5. **Expected:** RecoveryDialog opens with 3 tabs listed.
   - Tab 1 row: 1 pane `/bin/zsh · code · (no AI)`.
   - Tab 2 row, expanded: claude pane shows `resumable` (1s ago); zsh pane shows `(no AI)`.
   - Tab 3 row: codex pane shows `resumable`.
   - Subtitle: "Last session ended unexpectedly · 0 min ago".
6. Click "Restore all 3". Each tab spawns; for the claude/codex panes, after the shell prompt shows up, `claude --resume <UUID>` / `codex resume <UUID>` is typed and runs.

- [ ] **Step 5: Reproduce clean exit → dialog still shows but says "clean"**

1. Open 2 tabs.
2. Click the close button → confirm Quit in the dialog.
3. Relaunch.
4. **Expected:** Subtitle says "Last cleanly exited · 0 min ago"; same restore flow.

- [ ] **Step 6: Disable in Settings → no dialog**

1. Open Settings → General → toggle "Show recovery prompt at launch" off.
2. Open 1 tab, kill -9.
3. Relaunch → fresh single-tab session, no dialog.
4. Toggle back on → next launch dialog appears (snapshot was still being written).

- [ ] **Step 7: Alias miss graceful degrade**

1. In a fish shell tab: `alias c='claude'; c`.
2. After claude starts, kill -9.
3. Relaunch: pane shows `not classified as AI` badge in dialog.
4. Restore: shell opens; user presses `↑` to recall `c`; works.

- [ ] **Step 8: Commit nothing (verification-only task)**

If any of the above failed, fix in a new commit and re-run. If all passed, this task is done.

---

## Self-Review Notes

- All spec sections §1–§13 are covered by tasks T1–T16. AI sid sniff timeout / ambiguity / cancel are tested in T4. RecoveryStore TTL / host_id / version / clean_shutdown two-phase tested in T2. Frontend debounce timings tested in T11. Resume args mapping tested in T13. Manual e2e in T16 covers the user-facing flows.
- No placeholders, TBDs, or "implement similarly" steps — every code-changing step contains the actual code.
- Type/name consistency check: `RecoverySnapshot`, `TabSnapshot`, `PaneSnapshot`, `AIInfo` (Go) ↔ `RecoverySnapshot`, `RecoveryTabSnapshot`, `RecoveryPaneSnapshot`, `RecoveryAIInfo` (TS); slot/shell/shell_args/last_cwd/session_type/last_command_line/title/ai are identical fields across both sides. Method names: `SaveRecoverySnapshot`, `LoadRecoverySnapshot`, `DiscardRecoverySnapshot`, `MarkCleanShutdown`, `GetRecoveryDialogEnabled`, `SetRecoveryDialogEnabled` used consistently from T6/T8 forward.
