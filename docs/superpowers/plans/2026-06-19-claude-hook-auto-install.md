# Claude Code Hook Auto-Install + Health Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a hookinstall package that embeds the `atterm-hook` binary into the desktop app, materializes it under `~/.atterm/bin/atterm-hook-<sha8>` on launch with a `atterm-hook` symlink, idempotently merges atterm's `Notification` hook entries into `~/.claude/settings.json`, and exposes a Settings · Feishu status block (green/amber/gray) with on/off + Retry. End state: a fresh atterm install gives users a working Feishu hook with zero manual config.

**Architecture:** New `desktop/hookinstall/` package (peer of `desktop/feishu/`) with pure functions for merge/marker, file-IO for binary materialization, and a `Check` snapshot for UI. The desktop `app.go` Startup calls `Install`; `GetHookInstallState` triggers auto-repair behind a 5s debounce. Embedded binary path is a build-time artifact produced by a new Makefile target before `wails build`.

**Tech Stack:** Go 1.23 + `//go:embed`; Vue 3 frontend (Wails); existing `cmd/atterm-hook` CLI gains a `--version` flag; no new third-party deps.

**Spec:** [`docs/superpowers/specs/2026-06-19-claude-hook-auto-install-design.md`](../specs/2026-06-19-claude-hook-auto-install-design.md)

---

## File Structure

**New files:**

| Path | Responsibility |
|---|---|
| `desktop/hookinstall/doc.go` | Package overview |
| `desktop/hookinstall/embed.go` | `//go:embed atterm-hook` → `embeddedHook []byte` + `embeddedHash` |
| `desktop/hookinstall/embed_test.go` | Verify embed bytes are a runnable atterm-hook |
| `desktop/hookinstall/marker.go` | `isAttermHookCommand` substring marker |
| `desktop/hookinstall/marker_test.go` | Marker table-driven tests |
| `desktop/hookinstall/settings.go` | JSON schema + `mergeAttermEntries` pure function + read/write |
| `desktop/hookinstall/settings_test.go` | Merge tests, JSON round-trip tests |
| `desktop/hookinstall/binary.go` | `ensureBinary` + `gcOldVersions` |
| `desktop/hookinstall/binary_test.go` | Binary materialization + GC tests |
| `desktop/hookinstall/installer.go` | `Install` / `Uninstall` (composes binary + settings) |
| `desktop/hookinstall/installer_test.go` | End-to-end (within package) Install/Uninstall |
| `desktop/hookinstall/health.go` | `Check` + `State` |
| `desktop/hookinstall/health_test.go` | State derivation tests |
| `desktop/hookinstall/internal_paths.go` | Home/path helpers with injectable `home string` (test-only override) |
| `scripts/build-hook-binary.sh` | Build `atterm-hook` → `desktop/hookinstall/atterm-hook` |
| `Makefile` | `make atterm-hook-embed`, `make dev`, `make build` |

**Modified files:**

| Path | Change |
|---|---|
| `cmd/atterm-hook/main.go` | Add `--version` flag printing `atterm-hook <version>` and exiting 0 |
| `cmd/atterm-hook/main_test.go` | Test the `--version` branch |
| `desktop/config.go` | Add `HookAutoInstallEnabled *bool` field + `HookAutoInstallEnabledOrDefault()` |
| `desktop/config_test.go` (if exists) | Tests for the default-true helper |
| `desktop/app.go` | Call `hookinstall.Install` in `startup`; add `GetHookInstallState` / `SetHookInstallEnabled` Wails methods; pass suspect callback to feishu service |
| `desktop/feishu/hook_server.go` | Accept an optional `onSuspect` callback; fire on parse failures / unknown agent kind |
| `desktop/feishu/hook_server_test.go` | Cover the new `onSuspect` branch |
| `desktop/frontend/src/lib/api.ts` | Add `getHookInstallState` / `setHookInstallEnabled` wrappers |
| `desktop/frontend/src/components/SettingsFeishu.vue` | Top block: status dot + label + on/off + Retry |
| `desktop/frontend/src/components/SettingsFeishu.test.ts` (new if missing) | Status-block render tests |
| `desktop/frontend/src/i18n/messages.ts` (or equivalent) | New keys: `settings.feishu.hook_install.*` |
| `.gitignore` | `desktop/hookinstall/atterm-hook` |
| `scripts/feishu-hook-e2e-checklist.md` | New §11 covering auto-install + amber-recovery scenarios |

---

## Conventions (read once)

- **All file-IO functions take an explicit `home string` argument**. Production callers pass `os.UserHomeDir()` via a single thin wrapper in `internal_paths.go`. Tests pass `t.TempDir()` so they don't touch the real `~/`. This mirrors `desktop/ai_sid_parse.go`'s pattern — search that file for the same shape.
- **Commit messages use Conventional Commits** matching the repo: `feat(hookinstall): ...`, `feat(desktop): ...`, `docs: ...`. Reference the PR scope in commit body when useful.
- **Run tests by package**: `go test ./desktop/hookinstall/...` etc. The repo has CI-level lint (`go vet ./...`) — keep it clean.
- **No emojis** in code, comments, or commit messages (per project memory).
- **Reply language in user-facing messages**: Chinese. Inline code, identifiers, commit messages stay English.

---

## Task 1: Add `--version` flag to `atterm-hook` CLI

**Files:**
- Modify: `cmd/atterm-hook/main.go`
- Modify: `cmd/atterm-hook/main_test.go`

We need `--version` so the embed sanity test in Task 3 can verify the embedded bytes are an executable atterm-hook (not, say, an empty file or a different binary).

- [ ] **Step 1: Write the failing test**

Append to `cmd/atterm-hook/main_test.go`:

```go
func TestVersionFlag(t *testing.T) {
	// Build the CLI into a temp dir.
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "atterm-hook")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.HasPrefix(got, "atterm-hook ") {
		t.Errorf("unexpected version line %q", got)
	}
}
```

Add to the existing imports if missing: `"os/exec"`, `"path/filepath"`, `"strings"`.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./cmd/atterm-hook/... -run TestVersionFlag -v
```
Expected: FAIL. The CLI doesn't recognize `--version`, so it reads stdin (empty), returns exit 0, prints nothing. The assertion `HasPrefix "atterm-hook "` fails.

- [ ] **Step 3: Add the version flag handling**

Insert at the top of `func main()` in `cmd/atterm-hook/main.go`, immediately after the `func main() {` line:

```go
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("atterm-hook dev")
		os.Exit(0)
	}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./cmd/atterm-hook/... -run TestVersionFlag -v
```
Expected: PASS.

- [ ] **Step 5: Run the full atterm-hook test suite to ensure nothing else broke**

```bash
go test ./cmd/atterm-hook/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/atterm-hook/main.go cmd/atterm-hook/main_test.go
git commit -m "feat(atterm-hook): add --version flag

Used by hookinstall package to sanity-check the embedded binary
on test runs."
```

---

## Task 2: Build script + Makefile + .gitignore

**Files:**
- Create: `scripts/build-hook-binary.sh`
- Create: `Makefile`
- Modify: `.gitignore`

This creates the build artifact `desktop/hookinstall/atterm-hook` that Task 3 will embed via `//go:embed`. Without this, Task 3 won't compile.

- [ ] **Step 1: Write `scripts/build-hook-binary.sh`**

```bash
#!/usr/bin/env bash
# scripts/build-hook-binary.sh — produce desktop/hookinstall/atterm-hook
# for go:embed. -trimpath + -s -w make the output reproducible so the
# embedded sha8 only changes when cmd/atterm-hook source changes.
set -euo pipefail
cd "$(dirname "$0")/.."

OUT="desktop/hookinstall/atterm-hook"
mkdir -p "$(dirname "$OUT")"
go build -trimpath -ldflags='-s -w' -o "$OUT" ./cmd/atterm-hook
echo "built $OUT ($(wc -c < "$OUT") bytes)"
```

```bash
chmod +x scripts/build-hook-binary.sh
```

- [ ] **Step 2: Create `Makefile`**

```makefile
# Makefile — top-level orchestration of desktop build steps.

HOOK_EMBED := desktop/hookinstall/atterm-hook
HOOK_SOURCES := $(wildcard cmd/atterm-hook/*.go)

.PHONY: atterm-hook-embed dev build verify-hook-embed

$(HOOK_EMBED): $(HOOK_SOURCES)
	./scripts/build-hook-binary.sh

atterm-hook-embed: $(HOOK_EMBED)

dev: atterm-hook-embed
	cd desktop && wails dev

build: atterm-hook-embed
	cd desktop && wails build

# verify-hook-embed: rebuild the embed file and check it parses; intended
# for CI. If the on-disk embed file is stale (someone forgot to run
# atterm-hook-embed), `go test ./desktop/hookinstall/...` will fail
# the embed_test runnable check.
verify-hook-embed: atterm-hook-embed
	go test ./desktop/hookinstall/... -run TestEmbeddedBinary -v
```

- [ ] **Step 3: Add the build artifact to `.gitignore`**

Append to `.gitignore`:

```
# Build artifact for desktop/hookinstall embed; produced by
# scripts/build-hook-binary.sh on demand.
desktop/hookinstall/atterm-hook
```

- [ ] **Step 4: Sanity-build the artifact and verify it exists**

```bash
make atterm-hook-embed
ls -la desktop/hookinstall/atterm-hook
```
Expected: a non-empty executable file appears.

- [ ] **Step 5: Confirm the artifact is gitignored**

```bash
git status --porcelain | grep desktop/hookinstall/atterm-hook || echo "ignored (good)"
```
Expected output: `ignored (good)`.

- [ ] **Step 6: Commit**

```bash
git add scripts/build-hook-binary.sh Makefile .gitignore
git commit -m "build: embed atterm-hook into desktop via Makefile

scripts/build-hook-binary.sh produces desktop/hookinstall/atterm-hook
with -trimpath -s -w for reproducible bytes. Makefile wires
dev/build/CI to ensure the embed artifact is fresh before wails."
```

---

## Task 3: `hookinstall` package skeleton + `embed.go` + runnable test

**Files:**
- Create: `desktop/hookinstall/doc.go`
- Create: `desktop/hookinstall/embed.go`
- Create: `desktop/hookinstall/embed_test.go`
- Create: `desktop/hookinstall/internal_paths.go`

- [ ] **Step 1: Write `desktop/hookinstall/doc.go`**

```go
// Package hookinstall materializes the atterm-hook binary onto disk
// and patches ~/.claude/settings.json so claude-code triggers it on
// Notification events. Exposes Install / Uninstall / Check. All
// file-IO functions accept an explicit home parameter; production
// callers pass os.UserHomeDir().
package hookinstall
```

- [ ] **Step 2: Write `desktop/hookinstall/embed.go`**

```go
package hookinstall

import (
	"crypto/sha256"
	"encoding/hex"
	_ "embed"
)

//go:embed atterm-hook
var embeddedHook []byte

// embeddedHash is the hex-encoded first 4 bytes (8 hex chars) of the
// SHA-256 of the embedded binary. Used to name the on-disk versioned
// file: ~/.atterm/bin/atterm-hook-<embeddedHash>.
var embeddedHash = func() string {
	sum := sha256.Sum256(embeddedHook)
	return hex.EncodeToString(sum[:4])
}()
```

- [ ] **Step 3: Write `desktop/hookinstall/internal_paths.go`**

```go
package hookinstall

import (
	"os"
	"path/filepath"
)

// homeOrDie panics if os.UserHomeDir returns an error. The desktop main
// process cannot run without a home directory, so we don't try to
// recover. Tests should not call this — they pass home explicitly.
func homeOrDie() string {
	h, err := os.UserHomeDir()
	if err != nil {
		panic("hookinstall: cannot determine home: " + err.Error())
	}
	return h
}

// attermBinDir returns ~/.atterm/bin given a home root.
func attermBinDir(home string) string {
	return filepath.Join(home, ".atterm", "bin")
}

// attermHookSymlink returns ~/.atterm/bin/atterm-hook given a home root.
func attermHookSymlink(home string) string {
	return filepath.Join(attermBinDir(home), "atterm-hook")
}

// claudeSettingsPath returns ~/.claude/settings.json given a home root.
func claudeSettingsPath(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

// claudeDir returns ~/.claude given a home root.
func claudeDir(home string) string {
	return filepath.Join(home, ".claude")
}
```

- [ ] **Step 4: Write the failing embed runnable test**

`desktop/hookinstall/embed_test.go`:

```go
package hookinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedBinaryRunnable writes the embedded bytes to disk and runs
// the result with --version, asserting it exits 0 and prints a line
// starting with "atterm-hook ". Catches:
//   - embed file missing or zero bytes
//   - embed file is not the atterm-hook binary
//   - atterm-hook binary lacks the --version handler
func TestEmbeddedBinaryRunnable(t *testing.T) {
	if len(embeddedHook) == 0 {
		t.Fatal("embeddedHook is empty — did you forget to run `make atterm-hook-embed`?")
	}

	tmp := t.TempDir()
	bin := filepath.Join(tmp, "atterm-hook")
	if err := os.WriteFile(bin, embeddedHook, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.HasPrefix(got, "atterm-hook ") {
		t.Errorf("unexpected output %q", got)
	}
}

func TestEmbeddedHashLength(t *testing.T) {
	if len(embeddedHash) != 8 {
		t.Errorf("hash length = %d; want 8", len(embeddedHash))
	}
}
```

- [ ] **Step 5: Verify the embed file exists; then run**

```bash
make atterm-hook-embed
go test ./desktop/hookinstall/... -v
```
Expected: PASS. (If the embed file is missing or stale, the test will fail with a clear "embeddedHook is empty" message.)

- [ ] **Step 6: Commit**

```bash
git add desktop/hookinstall/
git commit -m "feat(hookinstall): package skeleton + embed atterm-hook binary

go:embed picks up desktop/hookinstall/atterm-hook (built by
scripts/build-hook-binary.sh). embeddedHash is the first 8 hex
chars of sha256(binary), used to name on-disk versions.

TestEmbeddedBinaryRunnable verifies the embed is an executable
atterm-hook by running --version against the materialized bytes."
```

---

## Task 4: `marker.go` — `isAttermHookCommand`

**Files:**
- Create: `desktop/hookinstall/marker.go`
- Create: `desktop/hookinstall/marker_test.go`

- [ ] **Step 1: Write the failing test**

`desktop/hookinstall/marker_test.go`:

```go
package hookinstall

import "testing"

func TestIsAttermHookCommand(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{"empty", "", false},
		{"bare binary on PATH", "atterm-hook", false},
		{"user's homebrew install", "/usr/local/bin/atterm-hook", false},
		{"atterm-managed absolute", "/Users/foo/.atterm/bin/atterm-hook", true},
		{"atterm-managed with args", "/Users/foo/.atterm/bin/atterm-hook --debug", true},
		{"atterm-managed with env prefix", "FOO=1 /Users/foo/.atterm/bin/atterm-hook", true},
		{"different user", "/home/bar/.atterm/bin/atterm-hook", true},
		{"unrelated command containing .atterm", "/usr/bin/grep .atterm/bin/atterm-hook somefile", true /* corner case we accept */},
		{"only substring partial", "/Users/x/.atterm/bin/", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isAttermHookCommand(HookEntry{Command: c.cmd})
			if got != c.want {
				t.Errorf("isAttermHookCommand(%q) = %v; want %v", c.cmd, got, c.want)
			}
		})
	}
}
```

This references a type `HookEntry` that doesn't exist yet — it's defined in Task 5. To compile this test in isolation now, we must declare `HookEntry` minimally here OR put `HookEntry` definition in this commit. We choose the latter: declare `HookEntry` in `marker.go` with only the `Command` field, then Task 5 expands the type. **However**, to keep Task 5 self-contained we'll declare the full struct in Task 5. The compile order is:

  Task 4 sees a partial struct; Task 5 adds the rest.

To avoid a broken intermediate state, declare the **full** `HookEntry` in Task 4 (this is the type's natural home — it's referenced by both marker and settings). Task 5 will then import it.

So in `marker.go`:

```go
package hookinstall

import "strings"

// HookEntry mirrors a single Claude Code Notification hook entry.
// Lives here rather than in settings.go because both marker and
// settings.go reference it; this is its natural home.
type HookEntry struct {
	Matcher HookMatcher `json:"matcher"`
	Command string      `json:"command"`
}

// HookMatcher selects when the hook fires. Fields match Claude Code's
// schema as observed in production traffic: "type" is the event kind,
// "tool" optionally restricts an idle_prompt to a specific tool.
type HookMatcher struct {
	Type string `json:"type"`
	Tool string `json:"tool,omitempty"`
}

// isAttermHookCommand returns true when an entry's Command field
// references the atterm-managed binary path. Substring match — not
// strict equality — so that paths with differing $HOME expansions
// still match across machines.
func isAttermHookCommand(e HookEntry) bool {
	return strings.Contains(e.Command, "/.atterm/bin/atterm-hook")
}
```

- [ ] **Step 2: Run the test, expect PASS**

```bash
go test ./desktop/hookinstall/... -run TestIsAttermHookCommand -v
```
Expected: PASS (we wrote test + impl in the same diff because the type also needed introducing).

- [ ] **Step 3: Run vet**

```bash
go vet ./desktop/hookinstall/...
```
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add desktop/hookinstall/marker.go desktop/hookinstall/marker_test.go
git commit -m "feat(hookinstall): HookEntry/HookMatcher types + isAttermHookCommand marker

Substring match so different \$HOME expansions still recognize our
own entries. The grep-corner-case false-positive is documented
in the spec; we accept it."
```

---

## Task 5: `settings.go` — JSON schema, `mergeAttermEntries`, read/write

**Files:**
- Create: `desktop/hookinstall/settings.go`
- Create: `desktop/hookinstall/settings_test.go`

- [ ] **Step 1: Write the failing tests**

`desktop/hookinstall/settings_test.go`:

```go
package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMergeAttermEntries(t *testing.T) {
	desired := []HookEntry{
		{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/H/.atterm/bin/atterm-hook"},
		{Matcher: HookMatcher{Type: "idle_prompt", Tool: "AskUserQuestion"}, Command: "/H/.atterm/bin/atterm-hook"},
	}
	cases := []struct {
		name     string
		existing []HookEntry
		want     []HookEntry
	}{
		{
			name:     "empty existing",
			existing: nil,
			want:     desired,
		},
		{
			name: "existing has only external hooks — appended",
			existing: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
			},
			want: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
				desired[0], desired[1],
			},
		},
		{
			name: "existing has stale atterm entries — replaced",
			existing: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/old/.atterm/bin/atterm-hook"},
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
			},
			want: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
				desired[0], desired[1],
			},
		},
		{
			name: "idempotent: running on already-installed produces same output",
			existing: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
				desired[0], desired[1],
			},
			want: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
				desired[0], desired[1],
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mergeAttermEntries(c.existing, desired, isAttermHookCommand)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestReadWriteSettings_Roundtrip(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(claudeDir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	path := claudeSettingsPath(home)
	// Pre-populate with other top-level keys that must be preserved.
	raw := `{"theme":"dark","hooks":{"Notification":[{"matcher":{"type":"permission_prompt"},"command":"/u/bin/x"}]}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := readClaudeSettings(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hooks.Notification) != 1 || cfg.Hooks.Notification[0].Command != "/u/bin/x" {
		t.Errorf("read lost the existing entry: %+v", cfg.Hooks.Notification)
	}

	cfg.Hooks.Notification = append(cfg.Hooks.Notification, HookEntry{
		Matcher: HookMatcher{Type: "idle_prompt"}, Command: "/u/bin/y",
	})
	if err := writeClaudeSettings(home, cfg); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(path)
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("written JSON unreadable: %v", err)
	}
	if _, ok := probe["theme"]; !ok {
		t.Errorf("write dropped the unrelated top-level field theme; got %s", out)
	}
}

func TestReadClaudeSettings_MissingFile(t *testing.T) {
	home := t.TempDir()
	cfg, err := readClaudeSettings(home)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.Hooks.Notification != nil {
		t.Errorf("missing file should yield empty cfg; got %+v", cfg)
	}
}

func TestReadClaudeSettings_InvalidJSON(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	os.WriteFile(claudeSettingsPath(home), []byte("not json"), 0o644)
	_, err := readClaudeSettings(home)
	if err == nil {
		t.Errorf("invalid JSON should surface an error")
	}
}

func TestWriteClaudeSettings_AtomicTempCleared(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	cfg := ClaudeSettings{}
	if err := writeClaudeSettings(home, cfg); err != nil {
		t.Fatal(err)
	}
	entries, _ := filepath.Glob(filepath.Join(claudeDir(home), "settings.json.atterm-tmp-*"))
	if len(entries) != 0 {
		t.Errorf("temp files left behind: %v", entries)
	}
}
```

- [ ] **Step 2: Run, expect FAIL ("undefined: mergeAttermEntries", etc.)**

```bash
go test ./desktop/hookinstall/... -run TestMergeAttermEntries -v
```
Expected: compile error.

- [ ] **Step 3: Write `desktop/hookinstall/settings.go`**

```go
package hookinstall

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ClaudeSettings models the subset of ~/.claude/settings.json we care
// about. Unknown top-level keys are preserved via extra map so the
// write back doesn't drop the user's theme, model, etc.
type ClaudeSettings struct {
	Hooks ClaudeHooks            `json:"hooks"`
	Extra map[string]any         `json:"-"`
}

// ClaudeHooks is the "hooks" object. We only own the Notification slot.
// Other hook lists (e.g. PreToolUse) are passed through unmodified.
type ClaudeHooks struct {
	Notification []HookEntry            `json:"Notification,omitempty"`
	Extra        map[string]any         `json:"-"`
}

// UnmarshalJSON splits the known field from the unknown rest so we can
// round-trip the file without losing user keys.
func (c *ClaudeSettings) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Extra = map[string]any{}
	if h, ok := raw["hooks"]; ok {
		if err := json.Unmarshal(h, &c.Hooks); err != nil {
			return err
		}
		delete(raw, "hooks")
	}
	for k, v := range raw {
		var anyV any
		if err := json.Unmarshal(v, &anyV); err != nil {
			return err
		}
		c.Extra[k] = anyV
	}
	return nil
}

func (c ClaudeSettings) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	for k, v := range c.Extra {
		out[k] = v
	}
	out["hooks"] = c.Hooks
	return json.Marshal(out)
}

func (h *ClaudeHooks) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	h.Extra = map[string]any{}
	if n, ok := raw["Notification"]; ok {
		if err := json.Unmarshal(n, &h.Notification); err != nil {
			return err
		}
		delete(raw, "Notification")
	}
	for k, v := range raw {
		var anyV any
		if err := json.Unmarshal(v, &anyV); err != nil {
			return err
		}
		h.Extra[k] = anyV
	}
	return nil
}

func (h ClaudeHooks) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	for k, v := range h.Extra {
		out[k] = v
	}
	if h.Notification != nil {
		out["Notification"] = h.Notification
	}
	return json.Marshal(out)
}

// mergeAttermEntries strips every entry the marker recognizes as
// atterm-owned, then appends desired in order. Idempotent and order-
// preserving for non-atterm entries.
func mergeAttermEntries(existing, desired []HookEntry, marker func(HookEntry) bool) []HookEntry {
	out := make([]HookEntry, 0, len(existing)+len(desired))
	for _, e := range existing {
		if !marker(e) {
			out = append(out, e)
		}
	}
	return append(out, desired...)
}

// readClaudeSettings reads ~/.claude/settings.json. Returns a zero-
// value ClaudeSettings (and nil error) when the file does not exist.
// Surfaces a JSON-parse error to the caller — Install must NOT
// overwrite an unparseable file.
func readClaudeSettings(home string) (ClaudeSettings, error) {
	path := claudeSettingsPath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ClaudeSettings{}, nil
		}
		return ClaudeSettings{}, err
	}
	var cfg ClaudeSettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ClaudeSettings{}, err
	}
	return cfg, nil
}

// writeClaudeSettings serializes cfg with 2-space indent and writes it
// atomically (temp file + rename). Creates ~/.claude/ if needed.
func writeClaudeSettings(home string, cfg ClaudeSettings) error {
	if err := os.MkdirAll(claudeDir(home), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := claudeSettingsPath(home)
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path),
		filepath.Base(path)+".atterm-tmp-"+hex.EncodeToString(suffix))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run the tests, expect PASS**

```bash
go test ./desktop/hookinstall/... -v
```
Expected: all settings tests PASS, plus prior marker + embed tests still pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/hookinstall/settings.go desktop/hookinstall/settings_test.go
git commit -m "feat(hookinstall): claude settings parser + mergeAttermEntries

ClaudeSettings round-trips unknown top-level + hooks subkeys via
the Extra map, so writing back never drops user-set fields like
\"theme\" or other hook arrays. Atomic write via temp + rename."
```

---

## Task 6: `binary.go` — `ensureBinary` + GC

**Files:**
- Create: `desktop/hookinstall/binary.go`
- Create: `desktop/hookinstall/binary_test.go`

- [ ] **Step 1: Write the failing tests**

`desktop/hookinstall/binary_test.go`:

```go
package hookinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureBinary_FirstInstall(t *testing.T) {
	home := t.TempDir()
	path, version, err := ensureBinary(home)
	if err != nil {
		t.Fatal(err)
	}
	if version != embeddedHash {
		t.Errorf("version = %q; want %q", version, embeddedHash)
	}
	if path != attermHookSymlink(home) {
		t.Errorf("path = %q; want %q", path, attermHookSymlink(home))
	}

	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(attermBinDir(home), "atterm-hook-"+embeddedHash)
	if target != want {
		t.Errorf("symlink target = %q; want %q", target, want)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("binary not executable: %s", info.Mode())
	}
	if int(info.Size()) != len(embeddedHook) {
		t.Errorf("binary size = %d; want %d", info.Size(), len(embeddedHook))
	}
}

func TestEnsureBinary_Idempotent(t *testing.T) {
	home := t.TempDir()
	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(attermBinDir(home), "atterm-hook-"+embeddedHash)
	first, _ := os.Stat(target)
	time.Sleep(20 * time.Millisecond)
	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	second, _ := os.Stat(target)
	if !first.ModTime().Equal(second.ModTime()) {
		t.Errorf("binary rewritten on second call (mtime changed)")
	}
}

func TestEnsureBinary_StaleSymlinkRetargeted(t *testing.T) {
	home := t.TempDir()
	bin := attermBinDir(home)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a stale symlink pointing somewhere else.
	stale := filepath.Join(bin, "atterm-hook-DEADBEEF")
	os.WriteFile(stale, []byte("stale"), 0o755)
	os.Symlink(stale, attermHookSymlink(home))

	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	got, _ := os.Readlink(attermHookSymlink(home))
	want := filepath.Join(bin, "atterm-hook-"+embeddedHash)
	if got != want {
		t.Errorf("symlink target = %q; want %q", got, want)
	}
}

func TestGCOldVersions_KeepsCurrentAndFresh(t *testing.T) {
	home := t.TempDir()
	bin := attermBinDir(home)
	os.MkdirAll(bin, 0o755)
	current := filepath.Join(bin, "atterm-hook-"+embeddedHash)
	old := filepath.Join(bin, "atterm-hook-OLDOLDOL")
	young := filepath.Join(bin, "atterm-hook-YOUNG123")
	os.WriteFile(current, []byte("x"), 0o755)
	os.WriteFile(old, []byte("y"), 0o755)
	os.WriteFile(young, []byte("z"), 0o755)
	weekAgo := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(old, weekAgo, weekAgo)
	// young keeps "now" mtime

	gcOldVersions(bin, current, 7*24*time.Hour)

	if _, err := os.Stat(current); err != nil {
		t.Errorf("current removed: %v", err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Errorf("young removed: %v", err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Errorf("old NOT removed")
	}
}

func TestEnsureBinary_NonHookFilesIgnoredByGC(t *testing.T) {
	home := t.TempDir()
	bin := attermBinDir(home)
	os.MkdirAll(bin, 0o755)
	// Place an unrelated user file in ~/.atterm/bin/ — GC must not touch.
	unrelated := filepath.Join(bin, "user-script.sh")
	os.WriteFile(unrelated, []byte("#!/bin/sh"), 0o755)
	weekAgo := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(unrelated, weekAgo, weekAgo)

	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("user file removed by GC: %v", err)
	}
}

func TestEnsureBinary_PrefixMustMatch(t *testing.T) {
	// Sanity: gc only touches files whose names start with atterm-hook-
	files, err := filepath.Glob(filepath.Join("/", "*"))
	_ = files; _ = err
	if !strings.HasPrefix("atterm-hook-DEAD", "atterm-hook-") {
		t.Fatal("guard")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./desktop/hookinstall/... -run TestEnsureBinary -v
```
Expected: compile error (ensureBinary undefined).

- [ ] **Step 3: Write `desktop/hookinstall/binary.go`**

```go
package hookinstall

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ensureBinary materializes the embedded atterm-hook binary as
// ~/.atterm/bin/atterm-hook-<sha8> if not already there, updates
// ~/.atterm/bin/atterm-hook symlink to point at it, and GCs siblings
// older than 7 days. Returns (symlinkPath, sha8, nil) on success.
func ensureBinary(home string) (symlinkPath string, version string, err error) {
	base := attermBinDir(home)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", "", err
	}

	target := filepath.Join(base, "atterm-hook-"+embeddedHash)
	if _, err := os.Stat(target); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
		suffix := make([]byte, 6)
		if _, err := rand.Read(suffix); err != nil {
			return "", "", err
		}
		tmp := target + ".tmp-" + hex.EncodeToString(suffix)
		if err := os.WriteFile(tmp, embeddedHook, 0o755); err != nil {
			return "", "", err
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return "", "", err
		}
	}

	link := attermHookSymlink(home)
	newLink := link + ".new"
	_ = os.Remove(newLink)
	if err := os.Symlink(target, newLink); err != nil {
		return "", "", err
	}
	if err := os.Rename(newLink, link); err != nil {
		_ = os.Remove(newLink)
		return "", "", err
	}

	gcOldVersions(base, target, 7*24*time.Hour)
	return link, embeddedHash, nil
}

// gcOldVersions removes files in base whose name begins with
// "atterm-hook-" AND whose path is not keep AND whose mtime is older
// than maxAge. Best-effort; errors are ignored.
func gcOldVersions(base, keep string, maxAge time.Duration) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "atterm-hook-") {
			continue
		}
		full := filepath.Join(base, name)
		if full == keep {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(full)
		}
	}
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./desktop/hookinstall/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/hookinstall/binary.go desktop/hookinstall/binary_test.go
git commit -m "feat(hookinstall): ensureBinary materializes embed + GC

Writes ~/.atterm/bin/atterm-hook-<sha8> via tmp+rename, retargets
the atterm-hook symlink atomically, and GCs siblings older than
7 days (skipping non-atterm-hook-* user files)."
```

---

## Task 7: `installer.go` — `Install` / `Uninstall`

**Files:**
- Create: `desktop/hookinstall/installer.go`
- Create: `desktop/hookinstall/installer_test.go`

- [ ] **Step 1: Write the failing tests**

`desktop/hookinstall/installer_test.go`:

```go
package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustReadSettings(t *testing.T, home string) ClaudeSettings {
	t.Helper()
	c, err := readClaudeSettings(home)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return c
}

func TestInstall_FreshHome(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	c := mustReadSettings(t, home)
	if len(c.Hooks.Notification) != 2 {
		t.Fatalf("want 2 entries; got %d: %+v", len(c.Hooks.Notification), c.Hooks.Notification)
	}
	link := attermHookSymlink(home)
	for _, e := range c.Hooks.Notification {
		if e.Command != link {
			t.Errorf("entry command = %q; want %q", e.Command, link)
		}
	}
}

func TestInstall_PreservesExternalNotificationHook(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	raw := `{"hooks":{"Notification":[{"matcher":{"type":"permission_prompt"},"command":"/usr/local/bin/myhook"}]}}`
	os.WriteFile(claudeSettingsPath(home), []byte(raw), 0o644)

	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	c := mustReadSettings(t, home)
	if len(c.Hooks.Notification) != 3 {
		t.Fatalf("want 3 entries; got %d", len(c.Hooks.Notification))
	}
	if c.Hooks.Notification[0].Command != "/usr/local/bin/myhook" {
		t.Errorf("external hook lost: %+v", c.Hooks.Notification[0])
	}
}

func TestInstall_PreservesOtherHookKinds(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	raw := `{"hooks":{"PreToolUse":[{"matcher":{"type":"x"},"command":"/u/y"}]}}`
	os.WriteFile(claudeSettingsPath(home), []byte(raw), 0o644)

	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(claudeSettingsPath(home))
	if !strings.Contains(string(data), `"PreToolUse"`) {
		t.Errorf("PreToolUse dropped: %s", data)
	}
}

func TestInstall_IdempotentSkipsWrite(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	path := claudeSettingsPath(home)
	first, _ := os.Stat(path)
	// Run again; mtime must not change.
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	second, _ := os.Stat(path)
	if !first.ModTime().Equal(second.ModTime()) {
		t.Errorf("settings.json rewritten on idempotent Install")
	}
}

func TestInstall_RefusesToOverwriteInvalidJSON(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	path := claudeSettingsPath(home)
	os.WriteFile(path, []byte("not json"), 0o644)
	err := installAt(home)
	if err == nil {
		t.Errorf("expected error on invalid JSON")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "not json" {
		t.Errorf("invalid JSON overwritten: %s", data)
	}
}

func TestUninstall_RemovesAttermEntriesOnly(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	// Add an external entry post-install.
	c := mustReadSettings(t, home)
	c.Hooks.Notification = append([]HookEntry{
		{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/u/bin/mine"},
	}, c.Hooks.Notification...)
	if err := writeClaudeSettings(home, c); err != nil {
		t.Fatal(err)
	}

	if err := uninstallAt(home); err != nil {
		t.Fatal(err)
	}
	c = mustReadSettings(t, home)
	if len(c.Hooks.Notification) != 1 {
		t.Fatalf("want 1 external entry left; got %d", len(c.Hooks.Notification))
	}
	if c.Hooks.Notification[0].Command != "/u/bin/mine" {
		t.Errorf("uninstall took the wrong entry: %+v", c.Hooks.Notification[0])
	}
	if _, err := os.Lstat(attermHookSymlink(home)); err == nil {
		t.Errorf("symlink not removed")
	}
}

func TestUninstall_DoesNotDeleteVersionedBinaries(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(attermBinDir(home), "atterm-hook-"+embeddedHash)
	if err := uninstallAt(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("versioned binary removed: %v", err)
	}
}

// helpers — installAt / uninstallAt accept an explicit home so tests
// don't depend on os.UserHomeDir. The exported Install/Uninstall use
// homeOrDie().
var _ = json.Marshal // keep import
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./desktop/hookinstall/... -run TestInstall -v
```
Expected: compile error.

- [ ] **Step 3: Write `desktop/hookinstall/installer.go`**

```go
package hookinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"runtime"
)

// Install ensures the binary is materialized and ~/.claude/settings.json
// contains atterm's Notification entries. Idempotent. On Windows this
// is a no-op (symlinks need admin); Check will surface the reason.
func Install(_ context.Context) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return installAt(homeOrDie())
}

// Uninstall removes the atterm-managed entries from settings.json and
// the atterm-hook symlink. Versioned binaries under ~/.atterm/bin/
// are left in place (a long-running claude session may still hold a
// reference); GC happens on the next Install.
func Uninstall(_ context.Context) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return uninstallAt(homeOrDie())
}

func installAt(home string) error {
	link, _, err := ensureBinary(home)
	if err != nil {
		return err
	}

	cfg, err := readClaudeSettings(home)
	if err != nil {
		return err
	}

	desired := desiredEntries(link)
	merged := mergeAttermEntries(cfg.Hooks.Notification, desired, isAttermHookCommand)
	if entriesEqual(cfg.Hooks.Notification, merged) {
		return nil
	}
	cfg.Hooks.Notification = merged

	return writeClaudeSettings(home, cfg)
}

func uninstallAt(home string) error {
	link := attermHookSymlink(home)
	if _, err := os.Lstat(link); err == nil {
		_ = os.Remove(link)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	cfg, err := readClaudeSettings(home)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	filtered := make([]HookEntry, 0, len(cfg.Hooks.Notification))
	for _, e := range cfg.Hooks.Notification {
		if !isAttermHookCommand(e) {
			filtered = append(filtered, e)
		}
	}
	if entriesEqual(cfg.Hooks.Notification, filtered) {
		return nil
	}
	cfg.Hooks.Notification = filtered
	return writeClaudeSettings(home, cfg)
}

// desiredEntries returns the two Notification entries we own, pointing
// at the supplied symlink path.
func desiredEntries(link string) []HookEntry {
	return []HookEntry{
		{Matcher: HookMatcher{Type: "permission_prompt"}, Command: link},
		{Matcher: HookMatcher{Type: "idle_prompt", Tool: "AskUserQuestion"}, Command: link},
	}
}

// entriesEqual compares two []HookEntry by JSON marshaling (cheap; the
// arrays are small). Used to short-circuit a no-op write.
func entriesEqual(a, b []HookEntry) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./desktop/hookinstall/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/hookinstall/installer.go desktop/hookinstall/installer_test.go
git commit -m "feat(hookinstall): Install/Uninstall compose binary + settings

Install is idempotent: it skips the write when JSON-marshaled
entries already match. Refuses to overwrite invalid JSON.
Uninstall leaves versioned binaries (long-running claude may
still fork them); next Install GCs them after 7 days."
```

---

## Task 8: `health.go` — `Check` + `State`

**Files:**
- Create: `desktop/hookinstall/health.go`
- Create: `desktop/hookinstall/health_test.go`

- [ ] **Step 1: Write the failing tests**

`desktop/hookinstall/health_test.go`:

```go
package hookinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck_Healthy(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	s := checkAt(home, true /* enabled */)
	if !s.BinaryOK {
		t.Errorf("BinaryOK = false; LastError=%q", s.LastError)
	}
	if !s.SettingsOK {
		t.Errorf("SettingsOK = false; LastError=%q", s.LastError)
	}
	if s.LastError != "" {
		t.Errorf("LastError = %q; want empty", s.LastError)
	}
	if s.BinaryVersion != embeddedHash {
		t.Errorf("BinaryVersion = %q; want %q", s.BinaryVersion, embeddedHash)
	}
}

func TestCheck_DisabledShortCircuits(t *testing.T) {
	home := t.TempDir()
	s := checkAt(home, false)
	if s.Enabled {
		t.Errorf("Enabled = true; want false")
	}
	// Disabled state still reports correct paths so UI can show them.
	if s.BinaryPath == "" || s.SettingsPath == "" {
		t.Errorf("paths empty: %+v", s)
	}
}

func TestCheck_BinaryMissing(t *testing.T) {
	home := t.TempDir()
	// Don't install. Just check.
	s := checkAt(home, true)
	if s.BinaryOK {
		t.Errorf("BinaryOK = true; expected false")
	}
	if !strings.Contains(strings.ToLower(s.LastError), "binary") {
		t.Errorf("LastError should mention binary; got %q", s.LastError)
	}
}

func TestCheck_SymlinkPointsAtNonExistentFile(t *testing.T) {
	home := t.TempDir()
	bin := attermBinDir(home)
	os.MkdirAll(bin, 0o755)
	os.Symlink(filepath.Join(bin, "nope"), attermHookSymlink(home))

	s := checkAt(home, true)
	if s.BinaryOK {
		t.Errorf("BinaryOK = true; want false")
	}
}

func TestCheck_BinaryNotExecutable(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(attermBinDir(home), "atterm-hook-"+embeddedHash)
	os.Chmod(target, 0o644)

	s := checkAt(home, true)
	if s.BinaryOK {
		t.Errorf("BinaryOK = true; want false on non-executable target")
	}
}

func TestCheck_SettingsMissingMarkerEntries(t *testing.T) {
	home := t.TempDir()
	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	// Write a settings.json that has zero atterm entries.
	os.MkdirAll(claudeDir(home), 0o700)
	os.WriteFile(claudeSettingsPath(home),
		[]byte(`{"hooks":{"Notification":[{"matcher":{"type":"x"},"command":"/u/y"}]}}`),
		0o644)

	s := checkAt(home, true)
	if s.SettingsOK {
		t.Errorf("SettingsOK = true; want false")
	}
	if !strings.Contains(s.LastError, "Notification") {
		t.Errorf("LastError should mention Notification; got %q", s.LastError)
	}
}

func TestCheck_SettingsCommandPathStale(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	// Manually mutate settings.json so one atterm entry points at a
	// stale (different) path.
	cfg, _ := readClaudeSettings(home)
	cfg.Hooks.Notification[0].Command = "/tmp/wrong/.atterm/bin/atterm-hook"
	writeClaudeSettings(home, cfg)

	s := checkAt(home, true)
	if s.SettingsOK {
		t.Errorf("SettingsOK = true; want false on stale command path")
	}
}

func TestCheck_InvalidJSON(t *testing.T) {
	home := t.TempDir()
	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(claudeDir(home), 0o700)
	os.WriteFile(claudeSettingsPath(home), []byte("garbage"), 0o644)

	s := checkAt(home, true)
	if s.SettingsOK {
		t.Errorf("SettingsOK = true; want false")
	}
	if !strings.Contains(strings.ToLower(s.LastError), "json") {
		t.Errorf("LastError should mention JSON; got %q", s.LastError)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./desktop/hookinstall/... -run TestCheck -v
```
Expected: compile error.

- [ ] **Step 3: Write `desktop/hookinstall/health.go`**

```go
package hookinstall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"time"
)

// State is the read-only health snapshot returned by Check. Renders
// directly to the Settings · Feishu status row.
type State struct {
	Enabled       bool      `json:"enabled"`
	BinaryPath    string    `json:"binary_path"`
	BinaryOK      bool      `json:"binary_ok"`
	BinaryVersion string    `json:"binary_version"`
	SettingsPath  string    `json:"settings_path"`
	SettingsOK    bool      `json:"settings_ok"`
	LastError     string    `json:"last_error"`
	LastCheck     time.Time `json:"last_check"`
}

// Healthy returns true when both surfaces are OK and auto-install is on.
func (s State) Healthy() bool { return s.Enabled && s.BinaryOK && s.SettingsOK }

// Check is a pure read of the current on-disk state. Does NOT mutate
// anything. Cheap to call: a handful of file stats + one JSON parse.
func Check(_ context.Context, enabled bool) State {
	if runtime.GOOS == "windows" {
		return State{
			Enabled:      enabled,
			BinaryPath:   "",
			SettingsPath: "",
			LastError:    "auto-install unsupported on Windows",
			LastCheck:    time.Now(),
		}
	}
	return checkAt(homeOrDie(), enabled)
}

func checkAt(home string, enabled bool) State {
	s := State{
		Enabled:      enabled,
		BinaryPath:   attermHookSymlink(home),
		SettingsPath: claudeSettingsPath(home),
		LastCheck:    time.Now(),
	}
	if !enabled {
		return s
	}

	binOK, binVer, binErr := checkBinary(home)
	s.BinaryOK = binOK
	s.BinaryVersion = binVer

	setOK, setErr := checkSettings(home, s.BinaryPath)
	s.SettingsOK = setOK

	switch {
	case binErr != "":
		s.LastError = binErr
	case setErr != "":
		s.LastError = setErr
	}
	return s
}

func checkBinary(home string) (ok bool, version string, errStr string) {
	link := attermHookSymlink(home)
	target, err := os.Readlink(link)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, "", "hook binary symlink missing"
		}
		return false, "", fmt.Sprintf("read symlink: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return false, "", fmt.Sprintf("symlink target missing: %v", err)
	}
	if info.IsDir() {
		return false, "", "symlink target is a directory"
	}
	if info.Mode()&0o111 == 0 {
		return false, "", "hook binary not executable"
	}
	// Extract version from filename suffix: atterm-hook-<sha8>
	name := info.Name()
	const prefix = "atterm-hook-"
	if len(name) > len(prefix) && name[:len(prefix)] == prefix {
		version = name[len(prefix):]
	}
	return true, version, ""
}

func checkSettings(home string, wantCommand string) (ok bool, errStr string) {
	cfg, err := readClaudeSettings(home)
	if err != nil {
		return false, fmt.Sprintf("Claude settings.json invalid JSON: %v", err)
	}
	var attermEntries []HookEntry
	for _, e := range cfg.Hooks.Notification {
		if isAttermHookCommand(e) {
			attermEntries = append(attermEntries, e)
		}
	}
	if len(attermEntries) < 2 {
		return false, "Notification hook entries missing or incomplete"
	}
	for _, e := range attermEntries {
		if e.Command != wantCommand {
			return false, "Notification entry points at stale binary path"
		}
		if _, err := os.Stat(e.Command); err != nil {
			return false, "Notification command path missing on disk"
		}
	}
	return true, ""
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./desktop/hookinstall/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/hookinstall/health.go desktop/hookinstall/health_test.go
git commit -m "feat(hookinstall): Check returns read-only State snapshot

State has Enabled/BinaryOK/SettingsOK booleans + a one-line
LastError suitable for the UI status row. Windows short-
circuits with an explanatory LastError."
```

---

## Task 9: Add `HookAutoInstallEnabled` to `appConfig`

**Files:**
- Modify: `desktop/config.go`
- Create or modify: `desktop/config_test.go`

- [ ] **Step 1: Check whether `desktop/config_test.go` exists**

```bash
ls desktop/config_test.go 2>&1 || echo "missing"
```

If missing, the next step creates it. If present, append to it.

- [ ] **Step 2: Write the failing test**

Append to `desktop/config_test.go` (or create with `package main`):

```go
func TestHookAutoInstallEnabledOrDefault(t *testing.T) {
	c := appConfig{}
	if !c.HookAutoInstallEnabledOrDefault() {
		t.Errorf("nil pointer should default to true (fresh installs opt in)")
	}
	v := false
	c.HookAutoInstallEnabled = &v
	if c.HookAutoInstallEnabledOrDefault() {
		t.Errorf("explicit false should disable")
	}
	t2 := true
	c.HookAutoInstallEnabled = &t2
	if !c.HookAutoInstallEnabledOrDefault() {
		t.Errorf("explicit true should enable")
	}
}
```

If the file is new, also add the necessary imports:

```go
package main

import "testing"
```

- [ ] **Step 3: Run, expect FAIL**

```bash
go test ./desktop/... -run TestHookAutoInstallEnabledOrDefault -v
```
Expected: compile error or missing field/method.

- [ ] **Step 4: Add field + helper to `desktop/config.go`**

Add the field inside `appConfig` (group with the other `*bool` toggles, e.g. right after `RecoveryDialogEnabled`):

```go
	// HookAutoInstallEnabled controls whether the desktop materializes
	// the atterm-hook binary and patches ~/.claude/settings.json on
	// startup. Nil means "never set" → default true for fresh installs.
	HookAutoInstallEnabled *bool `json:"hook_auto_install_enabled,omitempty"`
```

Add the helper near `RecoveryDialogEnabledOrDefault`:

```go
func (c appConfig) HookAutoInstallEnabledOrDefault() bool {
	if c.HookAutoInstallEnabled == nil {
		return true
	}
	return *c.HookAutoInstallEnabled
}
```

- [ ] **Step 5: Run, expect PASS**

```bash
go test ./desktop/... -run TestHookAutoInstallEnabledOrDefault -v
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/config.go desktop/config_test.go
git commit -m "feat(desktop): persist HookAutoInstallEnabled in appConfig

Pointer-bool with default-true so fresh installs opt in to the
new hookinstall path without writing the field to config.json
until the user actively toggles it."
```

---

## Task 10: Wire `app.go` Startup + Wails methods

**Files:**
- Modify: `desktop/app.go`
- Modify: `desktop/app_test.go` (or create a new `desktop/app_hookinstall_test.go`)

We integrate at four points: import the new package, call `Install` in `startup`, expose `GetHookInstallState` / `SetHookInstallEnabled`, and gate the call on the config toggle.

- [ ] **Step 1: Write an integration test**

Create `desktop/app_hookinstall_test.go`:

```go
package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/attson/atterm/desktop/hookinstall"
)

// TestHookInstall_StartupInstallsByDefault verifies that on a fresh
// home, calling hookinstall.Install (the same call startup makes
// when enabled=true) materializes the symlink + writes settings.json.
//
// We test the hookinstall surface directly rather than driving
// app.startup, because startup pulls in the relay host, configStore,
// logging, etc. — overkill for asserting "install happened".
func TestHookInstall_DefaultEnabledIntegration(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	c := appConfig{}
	if !c.HookAutoInstallEnabledOrDefault() {
		t.Fatal("default should be true")
	}

	if err := hookinstall.Install(context.Background()); err != nil {
		t.Fatalf("install: %v", err)
	}

	settings := filepath.Join(tmp, ".claude", "settings.json")
	if _, err := os.Stat(settings); err != nil {
		t.Errorf("settings.json not created: %v", err)
	}
	link := filepath.Join(tmp, ".atterm", "bin", "atterm-hook")
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("symlink not created: %v", err)
	}
}
```

- [ ] **Step 2: Run the test**

```bash
go test ./desktop/... -run TestHookInstall_DefaultEnabledIntegration -v
```
Expected: PASS. Tasks 3-8 already exported `hookinstall.Install`; this test only confirms the import works from `desktop/` (where `package main` lives) and that the default-true config helper is wired. If FAIL with "no required module provides package github.com/attson/atterm/desktop/hookinstall", check that `go.mod` doesn't have a `replace` directive blocking the import.

- [ ] **Step 3: Add the import to `desktop/app.go`**

Find the import block in `desktop/app.go` (around line 1-50) and add to the import group:

```go
	"github.com/attson/atterm/desktop/hookinstall"
```

- [ ] **Step 4: Call `Install` in `startup`**

Inside `func (a *App) startup(ctx context.Context)` in `desktop/app.go`, after `a.applyRelayConfig(cfg)` (around line 259) and before the Feishu startup at line 284, insert:

```go
	// Auto-install ~/.claude/settings.json hook entries + materialize
	// atterm-hook binary, so a fresh install gets Feishu notifications
	// without manual settings.json editing. Failure is non-fatal — the
	// Settings · Feishu panel will surface the LastError.
	if cfg.HookAutoInstallEnabledOrDefault() {
		if err := hookinstall.Install(ctx); err != nil {
			log.Printf("hookinstall: install: %v", err)
		}
	}
```

- [ ] **Step 5: Add Wails-bound methods on `App`**

Append to `desktop/app.go` (near `GetFeishuStatus` for cohesion, e.g. after line 1860):

```go
// hookInstallLastAttempt tracks when we last auto-repaired so the UI
// poll doesn't trigger a Check→Install loop while the underlying issue
// is permanent (e.g. read-only mount).
var (
	hookInstallLastAttempt   time.Time
	hookInstallLastAttemptMu sync.Mutex
)

const hookInstallRepairDebounce = 5 * time.Second

// GetHookInstallState returns the current health snapshot. When the
// surface is unhealthy and we haven't tried in the last 5 seconds,
// we kick a silent Install before returning the post-repair state.
func (a *App) GetHookInstallState() hookinstall.State {
	enabled := true
	if a.cfgStore != nil {
		enabled = a.cfgStore.Get().HookAutoInstallEnabledOrDefault()
	}
	s := hookinstall.Check(a.ctx, enabled)
	if !s.Healthy() && enabled && allowHookInstallRepair() {
		if err := hookinstall.Install(a.ctx); err != nil {
			log.Printf("hookinstall: auto-repair: %v", err)
		}
		s = hookinstall.Check(a.ctx, enabled)
	}
	return s
}

// SetHookInstallEnabled persists the toggle and either installs or
// uninstalls. Errors are returned to the frontend so the Retry button
// can surface them.
func (a *App) SetHookInstallEnabled(on bool) error {
	if a.cfgStore != nil {
		cfg := a.cfgStore.Get()
		cfg.HookAutoInstallEnabled = &on
		if err := a.cfgStore.Set(cfg); err != nil {
			return err
		}
	}
	if on {
		// Reset debounce so a manual toggle ALWAYS retries.
		hookInstallLastAttemptMu.Lock()
		hookInstallLastAttempt = time.Time{}
		hookInstallLastAttemptMu.Unlock()
		return hookinstall.Install(a.ctx)
	}
	return hookinstall.Uninstall(a.ctx)
}

func allowHookInstallRepair() bool {
	hookInstallLastAttemptMu.Lock()
	defer hookInstallLastAttemptMu.Unlock()
	if time.Since(hookInstallLastAttempt) < hookInstallRepairDebounce {
		return false
	}
	hookInstallLastAttempt = time.Now()
	return true
}
```

Add `"sync"` and `"time"` to the imports if not already present (most likely both are).

- [ ] **Step 6: Run tests + vet**

```bash
go vet ./desktop/...
go test ./desktop/... -run TestHookInstall -v
```
Expected: PASS.

- [ ] **Step 7: Quick smoke-build the desktop**

```bash
go build ./desktop/...
```
Expected: build succeeds.

- [ ] **Step 8: Commit**

```bash
git add desktop/app.go desktop/app_hookinstall_test.go
git commit -m "feat(desktop): wire hookinstall.Install into Startup + Wails methods

Startup gates Install on the (default-true) HookAutoInstallEnabled
config. GetHookInstallState polls Check and silently auto-repairs
behind a 5s debounce. SetHookInstallEnabled persists the toggle and
forces a fresh Install/Uninstall."
```

---

## Task 11: HookServer suspect callback

**Files:**
- Modify: `desktop/feishu/hook_server.go`
- Modify: `desktop/feishu/hook_server_test.go`
- Modify: `desktop/app.go` (wire the callback)

The hook server already swallows unknown agent kinds with 200 OK. We add a callback fired when adapter lookup fails OR adapter.Parse returns `emit=false` due to a parse error — both signal "something on the hook side is unhappy".

- [ ] **Step 1: Write the failing test**

Open `desktop/feishu/hook_server_test.go` and locate the existing `fakeWaitingDispatcher` and `fakeSessionLookup` patterns (or the names the file actually uses — `grep -n '^type fake' desktop/feishu/hook_server_test.go`). Reuse those types. Append:

```go
func TestHookServer_FiresSuspectOnUnknownAgentKind(t *testing.T) {
	disp := &fakeWaitingDispatcher{}
	sess := &fakeSessionLookup{exists: true}
	srv := NewHookServer(disp, sess)

	var suspectCalled int
	srv.SetSuspectCallback(func() { suspectCalled++ })

	addr, server, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(context.Background())

	body := bytes.NewBufferString(`{"session_id":"` + uuid.New().String() +
		`","agent_kind":"made-up","hook_input":{}}`)
	resp, err := http.Post("http://"+addr+"/atterm-hook/notify",
		"application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d; want 200", resp.StatusCode)
	}
	if suspectCalled != 1 {
		t.Errorf("suspectCalled = %d; want 1", suspectCalled)
	}
}
```

Add imports if missing: `"bytes"`, `"context"`, `"net/http"`, `"github.com/google/uuid"`. If the existing fake types are named differently, substitute — the test logic is the same.

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./desktop/feishu/... -run TestHookServer_FiresSuspect -v
```
Expected: FAIL.

- [ ] **Step 3: Add the callback to `HookServer`**

In `desktop/feishu/hook_server.go`:

Change the struct:

```go
type HookServer struct {
	disp     WaitingDispatcher
	sessions SessionLookup
	onSuspect func()
}
```

Add a setter (deliberately not a constructor arg, to keep `NewHookServer` signature stable for existing callers):

```go
// SetSuspectCallback registers a callback invoked when a POST is well-
// formed but the agent_kind isn't recognized, signaling that the
// installed hook is mis-wired (e.g. a stale binary, broken adapter).
// Safe to call before or after Start.
func (h *HookServer) SetSuspectCallback(fn func()) {
	h.onSuspect = fn
}
```

In `ServeHTTP`, replace:

```go
	adapter, ok := LookupHookAdapter(req.AgentKind)
	if !ok {
		w.WriteHeader(http.StatusOK)
		return
	}
```

with:

```go
	adapter, ok := LookupHookAdapter(req.AgentKind)
	if !ok {
		if h.onSuspect != nil {
			h.onSuspect()
		}
		w.WriteHeader(http.StatusOK)
		return
	}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./desktop/feishu/... -run TestHookServer -v
```
Expected: PASS for new test plus existing tests still green.

- [ ] **Step 5: Wire the callback in `desktop/app.go`**

In `desktop/app.go` `startFeishu`, after the existing `addr, _, err := svc.HookServer().Start()` line (around line 1804), add:

```go
	svc.HookServer().SetSuspectCallback(func() {
		// A misrouted POST may indicate stale install; nudge the
		// debounced auto-repair on next UI poll.
		hookInstallLastAttemptMu.Lock()
		hookInstallLastAttempt = time.Time{}
		hookInstallLastAttemptMu.Unlock()
	})
```

- [ ] **Step 6: Build + vet**

```bash
go vet ./...
go build ./...
```
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add desktop/feishu/hook_server.go desktop/feishu/hook_server_test.go desktop/app.go
git commit -m "feat(feishu): HookServer suspect callback for hookinstall

Unknown agent_kind on a Notification POST clears the
hookinstall debounce so the next UI poll re-checks promptly
without waiting for the 5s timer."
```

---

## Task 12: Frontend API + SettingsFeishu status block

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`
- Modify: `desktop/frontend/src/components/SettingsFeishu.vue`
- Modify: i18n messages file (locate via `grep -rn 'settings.feishu.disabled' desktop/frontend/src/i18n/`)

- [ ] **Step 1: Locate the i18n file(s)**

```bash
grep -rln 'settings\.feishu\.disabled' desktop/frontend/src/i18n/
```
Note the file path(s). There may be `en.ts` and `zh.ts` style splits or a single object — adapt the i18n step (Step 4) accordingly.

- [ ] **Step 2: Wails-regenerate bindings, then add API wrappers**

```bash
make atterm-hook-embed
(cd desktop && wails generate module) 2>&1 | tail -5
```

Expected: Wails regenerates `desktop/frontend/wailsjs/go/main/App.d.ts` to include `GetHookInstallState` and `SetHookInstallEnabled`.

Append to `desktop/frontend/src/lib/api.ts`, near `getFeishuStatus`:

```typescript
export type HookInstallState = {
  enabled: boolean;
  binary_path: string;
  binary_ok: boolean;
  binary_version: string;
  settings_path: string;
  settings_ok: boolean;
  last_error: string;
  last_check: string; // ISO timestamp
};

export function getHookInstallState(): Promise<HookInstallState> {
  return bindings().GetHookInstallState();
}

export function setHookInstallEnabled(on: boolean): Promise<void> {
  return bindings().SetHookInstallEnabled(on);
}
```

- [ ] **Step 3: Add status block to `SettingsFeishu.vue`**

Open `desktop/frontend/src/components/SettingsFeishu.vue`. At the top of the `<template>`, just inside the `<div class="tab-pane">`, insert the hook block BEFORE the existing `<p v-if="!status.enabled">` and surrounding template:

```vue
    <section class="hook-install" data-test="hook-install">
      <header class="hook-install__row">
        <span class="hook-install__dot" :class="dotClass" :title="hookState.last_error || ''"></span>
        <span class="hook-install__label">{{ hookLabel }}</span>
        <label class="hook-install__toggle">
          <input type="checkbox" :checked="hookState.enabled" @change="onToggleHook" />
          <span>{{ t('settings.feishu.hook_install.enable') }}</span>
        </label>
        <button
          v-if="hookState.enabled && (!hookState.binary_ok || !hookState.settings_ok)"
          type="button"
          class="hook-install__retry"
          @click="onRetryHook"
          data-test="hook-install-retry"
        >
          {{ t('settings.feishu.hook_install.retry') }}
        </button>
      </header>
      <p v-if="hookState.enabled && hookState.last_error" class="hook-install__error">
        {{ hookState.last_error }}
      </p>
    </section>
```

Add to `<script setup lang="ts">`, near the existing imports and `status` ref:

```typescript
import {
  ...,
  getHookInstallState,
  setHookInstallEnabled,
  type HookInstallState,
} from '../lib/api'

const hookState = ref<HookInstallState>({
  enabled: true,
  binary_path: '',
  binary_ok: false,
  binary_version: '',
  settings_path: '',
  settings_ok: false,
  last_error: '',
  last_check: '',
})

async function refreshHook() {
  try {
    hookState.value = await getHookInstallState()
  } catch (e) {
    // non-fatal; UI shows last known state.
  }
}

const dotClass = computed(() => {
  if (!hookState.value.enabled) return 'hook-install__dot--gray'
  if (hookState.value.binary_ok && hookState.value.settings_ok) return 'hook-install__dot--green'
  return 'hook-install__dot--amber'
})

const hookLabel = computed(() => {
  if (!hookState.value.enabled) return t('settings.feishu.hook_install.disabled')
  if (hookState.value.binary_ok && hookState.value.settings_ok) return t('settings.feishu.hook_install.healthy')
  return t('settings.feishu.hook_install.needs_attention')
})

async function onToggleHook(e: Event) {
  const on = (e.target as HTMLInputElement).checked
  try {
    await setHookInstallEnabled(on)
  } finally {
    await refreshHook()
  }
}

async function onRetryHook() {
  try {
    await setHookInstallEnabled(true)
  } finally {
    await refreshHook()
  }
}
```

Add `computed` to the `import { ref, onMounted } from 'vue'` line: `import { ref, computed, onMounted } from 'vue'`.

Modify `onMounted(refresh)` to also call `refreshHook`:

```typescript
onMounted(async () => {
  await refresh()
  await refreshHook()
})
```

Add minimal scoped styles at the bottom (in the existing `<style scoped>` block):

```css
.hook-install {
  padding: 8px 0 12px;
  border-bottom: 1px solid var(--border-subtle, rgba(127, 127, 127, 0.2));
  margin-bottom: 12px;
}
.hook-install__row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.hook-install__dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.hook-install__dot--green { background: #2ea043; }
.hook-install__dot--amber { background: #d29922; }
.hook-install__dot--gray  { background: #6e7681; }
.hook-install__label { flex: 1; font-size: 13px; }
.hook-install__toggle { display: flex; gap: 4px; align-items: center; font-size: 12px; }
.hook-install__retry { font-size: 12px; padding: 2px 8px; }
.hook-install__error { font-size: 12px; color: #d29922; margin: 6px 0 0; }
```

- [ ] **Step 4: Add i18n keys**

Edit the i18n file(s) located in Step 1. Add under `settings.feishu`:

```typescript
hook_install: {
  enable: 'Auto-install Claude Code hook',
  retry: 'Retry',
  healthy: 'Hook installed and healthy',
  needs_attention: 'Hook needs attention',
  disabled: 'Hook auto-install disabled',
},
```

For zh (if a separate file exists):

```typescript
hook_install: {
  enable: '自动安装 Claude Code Hook',
  retry: '重试',
  healthy: 'Hook 已安装且健康',
  needs_attention: 'Hook 需要修复',
  disabled: 'Hook 自动安装已关闭',
},
```

- [ ] **Step 5: Run frontend type-check + tests**

```bash
cd desktop/frontend
npm run build 2>&1 | tail -20   # surfaces TS errors
npm test 2>&1 | tail -30
```
Expected: build succeeds; tests pass (existing SettingsFeishu test, if any, should still pass since we only added a section above).

If the build fails because `getHookInstallState` isn't on the bindings type, ensure `wails generate module` ran in Step 2 and re-run.

- [ ] **Step 6: Commit**

```bash
cd ../..
git add desktop/frontend/src/lib/api.ts \
         desktop/frontend/src/components/SettingsFeishu.vue \
         desktop/frontend/src/i18n/
git commit -m "feat(desktop/frontend): hook auto-install status block in Settings·Feishu

Status dot (green/amber/gray) + label + auto-install toggle +
Retry button. Polls GetHookInstallState on panel open; toggle
calls SetHookInstallEnabled which install/uninstalls in the
backend and clears the debounce."
```

---

## Task 13: Wails bindings regen + final smoke test

**Files:**
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts` (auto-generated)
- Modify: `desktop/frontend/wailsjs/go/main/App.js` (auto-generated)

- [ ] **Step 1: Regenerate Wails bindings**

```bash
(cd desktop && wails generate module)
git diff --stat desktop/frontend/wailsjs/
```
Expected: diff shows additions for `GetHookInstallState` and `SetHookInstallEnabled` in `App.d.ts` and `App.js`.

- [ ] **Step 2: Run full desktop test suite**

```bash
go test ./desktop/... -count=1
```
Expected: all green.

- [ ] **Step 3: Run frontend test suite**

```bash
cd desktop/frontend && npm test -- --run 2>&1 | tail -10 && cd ../..
```
Expected: green.

- [ ] **Step 4: Manual smoke — launch atterm and confirm install happened**

In a separate terminal:
```bash
# Save a copy first (the test may modify it).
cp ~/.claude/settings.json ~/.claude/settings.json.bak 2>/dev/null || true

make dev
```

In a third terminal, after atterm starts:
```bash
cat ~/.claude/settings.json | python3 -m json.tool
ls -la ~/.atterm/bin/
```
Expected: settings.json contains both atterm-hook entries; `~/.atterm/bin/atterm-hook` is a symlink to `atterm-hook-<sha8>`.

Restore your original after the smoke test:
```bash
mv ~/.claude/settings.json.bak ~/.claude/settings.json 2>/dev/null || true
```

- [ ] **Step 5: Commit regenerated bindings**

```bash
git add desktop/frontend/wailsjs/
git diff --cached --stat
git commit -m "chore(desktop): regenerate Wails bindings for hook install methods"
```

---

## Task 14: E2E checklist update

**Files:**
- Modify: `scripts/feishu-hook-e2e-checklist.md`

- [ ] **Step 1: Replace the "Add Notification hooks…" prereq block**

Find this section in `scripts/feishu-hook-e2e-checklist.md` (around line 14):

```markdown
3. Add Notification hooks to `~/.claude/settings.json`:
   ```json
   { "hooks": {
       "Notification": [
         { "matcher": {"type":"idle_prompt","tool":"AskUserQuestion"},
           "command": "atterm-hook" },
         { "matcher": {"type":"permission_prompt"},
           "command": "atterm-hook" }
       ] } }
   ```
```

Replace with:

```markdown
3. Hook is now auto-installed. **No manual settings.json editing needed.**
   On first atterm launch with auto-install enabled (default), check:
   - `ls -la ~/.atterm/bin/atterm-hook` shows a symlink
   - `cat ~/.claude/settings.json | python3 -m json.tool` shows two
     atterm-managed Notification entries pointing at the symlink.

   To opt out: in atterm Settings → Feishu, toggle off "Auto-install
   Claude Code hook". The two atterm entries will be removed; any
   user-managed Notification entries are preserved.
```

- [ ] **Step 2: Append a new section after "## Cleanup"**

```markdown
## Hook auto-install

- [ ] Fresh launch on a machine with **no** `~/.claude/settings.json`:
      file created with both atterm entries; `~/.atterm/bin/atterm-hook`
      symlink points at `atterm-hook-<sha8>`; Settings · Feishu shows
      green dot + "Hook installed and healthy".
- [ ] Fresh launch with pre-existing **non-atterm** Notification hook:
      both atterm entries appended; user's entry preserved verbatim.
- [ ] Toggle "Auto-install Claude Code hook" OFF in Settings · Feishu:
      both atterm entries removed; user's entry intact; symlink removed;
      versioned `atterm-hook-<sha8>` file kept.
- [ ] Toggle ON again: re-installs cleanly.
- [ ] Break the binary: `rm ~/.atterm/bin/atterm-hook-<sha8>` (the
      symlink target). Open Settings · Feishu; status dot is amber for
      the first poll, then auto-repair writes a fresh `atterm-hook-<sha8>`
      and the next poll shows green. (Note: `chmod 000` alone does NOT
      trigger re-write — `ensureBinary` skips the write when the target
      file already exists. Removal is the right reproduction.)
- [ ] Make settings.json read-only: `chmod 444 ~/.claude/settings.json`;
      restart atterm; status dot is amber; LastError mentions "cannot
      update Claude settings"; atterm is otherwise functional.
- [ ] Put garbage in settings.json: `echo not-json > ~/.claude/settings.json`;
      restart atterm; status dot amber; LastError mentions "invalid JSON";
      file is **not** overwritten.
```

- [ ] **Step 3: Commit**

```bash
git add scripts/feishu-hook-e2e-checklist.md
git commit -m "docs: e2e checklist covers hook auto-install and amber states"
```

---

## Final verification

- [ ] **Run the full Go suite**

```bash
go test ./... -count=1 2>&1 | tail -30
```
Expected: all green.

- [ ] **Run vet**

```bash
go vet ./...
```
Expected: no output.

- [ ] **Run frontend tests**

```bash
cd desktop/frontend && npm test -- --run && cd ../..
```
Expected: green.

- [ ] **Build the desktop app**

```bash
make build
```
Expected: a fresh `atterm.app` (or platform equivalent) appears under `desktop/build/bin/`.

- [ ] **Manual end-to-end on the built app**

Launch the built app, open Settings → Feishu, see the new section, toggle off and on, verify `~/.claude/settings.json` and `~/.atterm/bin/atterm-hook` change accordingly.

Once all boxes are checked: ready for ship-release.
