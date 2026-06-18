# Claude Code hook auto-install + health check — design

Date: 2026-06-19
Status: Drafted — awaiting user review before plan.

## 0. Summary

Today, getting Feishu notifications from `claude-code` requires the user
to manually edit `~/.claude/settings.json` and add a `Notification` hook
entry pointing at the `atterm-hook` CLI (see `scripts/feishu-hook-e2e-checklist.md`).
That's friction during onboarding and silent breakage when the entry is
later removed, the binary disappears from `$PATH`, or a future Claude
update changes the schema.

This change ships an embedded `atterm-hook` binary inside the desktop
app, writes it to `~/.atterm/bin/atterm-hook` on startup, and patches
`~/.claude/settings.json` to point at it — all without user action.
A periodic-on-demand health check detects drift and silently re-installs.
A small Settings · Feishu UI block shows the current state (healthy /
needs attention / disabled) plus an on/off toggle.

The change is scoped to `claude-code`. Codex and other agents are out
of scope for this spec; the package leaves an extension seam for a
follow-up.

## 1. Goals

- First-time desktop launch leaves the user with a working
  `~/.claude/settings.json` Notification hook — zero manual config.
- The bundled `atterm-hook` binary tracks the desktop release cycle:
  Sparkle upgrade replaces the desktop binary, next launch writes a
  new `atterm-hook-<sha8>` and re-points the symlink.
- A health check detects three drift modes and silently auto-repairs:
  symlink missing/broken, binary file missing/non-executable,
  `~/.claude/settings.json` entries missing or pointing at a stale path.
- User-managed `Notification` hooks that aren't atterm's are preserved
  verbatim across install/uninstall.
- The user can disable auto-install in Settings · Feishu; disabling
  cleanly removes the atterm-managed entries from `settings.json`
  (other entries untouched) and stops re-writing on each launch.

## 2. Non-goals

- Codex / aider / other agent hook installation. `claude-code` only.
- Windows. Symlinks need administrator permissions there; auto-install
  is disabled on Windows and the UI surfaces a one-line explanation.
- Validating that Claude Code is itself installed. We write the file
  unconditionally; if Claude isn't installed today and gets installed
  tomorrow, the hook just works.
- Cryptographic signing or quarantine handling of the embedded binary.
  Single-user project; not the right place to spend complexity.
- Reading the conversation transcript (`*.jsonl`) for token usage,
  permission events, or anything else. Explicitly out of scope after
  brainstorming determined `permission_prompt` is not present in the
  transcript and standalone usage display is not user-valuable.

## 3. Architecture

A new package `desktop/hookinstall/` sits next to `desktop/feishu/`:

```
desktop/hookinstall/
  doc.go
  installer.go         // Install / Uninstall
  installer_test.go
  health.go            // Check returns State (read-only)
  health_test.go
  binary.go            // ensureBinary + GC
  binary_test.go
  settings.go          // pure: read/parse/mutate/serialize ~/.claude/settings.json
  settings_test.go
  marker.go            // isAttermHookCommand: identifies entries we own
  marker_test.go
  embed.go             // //go:embed atterm-hook
  embed_test.go        // TestEmbeddedBinaryRunnable
  atterm-hook          // gitignored, produced by `make atterm-hook-embed`
```

Integration points (each touch in an existing file is ≤ 20 lines):

- `desktop/app.go` — Startup calls `hookinstall.Install` when enabled
  (after `HookServer.Start`, before Feishu service start). Exposes two
  Wails-bound methods: `GetHookInstallState`, `SetHookInstallEnabled`.
- `desktop/config.go` (or current cfg store) — adds
  `HookAutoInstallEnabled *bool`; nil/missing means "true" so fresh
  installs opt in.
- `desktop/frontend/src/components/SettingsFeishu.vue` — a top block:
  status dot + label + on/off toggle + "Retry" button when amber.
- `scripts/build-desktop.sh` (or new `scripts/build-hook-binary.sh`) —
  `go build -trimpath -ldflags='-s -w' -o desktop/hookinstall/atterm-hook ./cmd/atterm-hook`
  invoked before `wails build`.
- `Makefile` — top-level target so `make dev`/`make build` produce the
  embed file before invoking Wails.
- `.gitignore` — adds `desktop/hookinstall/atterm-hook`.
- `cmd/atterm-hook/main.go` — adds a `--version` flag that prints the
  build's sha8 (used by the embed-runnable test).

Why a separate package: this code is purely desktop-side (uses
`os.UserHomeDir`, atomic file writes, Wails startup ordering). Co-locating
with `desktop/feishu/` would tangle Feishu's runtime concerns (endpoint
file, dispatcher) with install-time concerns (write settings.json on
startup). `desktop/feishu/endpoint_file.go`'s `~/.config/atterm/` is a
runtime artifact owned by the desktop process; `~/.atterm/bin/atterm-hook`
is an install artifact owned by the Claude Code config — different
lifecycles, separate packages.

## 4. API

The package exports exactly three functions:

```go
package hookinstall

// Install ensures the embedded binary is materialized under
// ~/.atterm/bin/, the symlink atterm-hook → atterm-hook-<sha8> is
// current, and ~/.claude/settings.json contains the two atterm-managed
// Notification entries. Idempotent — running it on an already-clean
// system is a no-op (does not rewrite settings.json byte-for-byte).
// Returns the first error encountered; partial success (binary written
// but settings.json read-only) is reflected in the next Check().
func Install(ctx context.Context) error

// Uninstall removes the atterm-managed entries from settings.json
// (other entries untouched) and removes the symlink. Versioned binaries
// under ~/.atterm/bin/atterm-hook-<sha8> are left in place — a
// long-running Claude session may still hold a fork reference; GC of
// stale versions happens on the next Install.
func Uninstall(ctx context.Context) error

// State is the read-only health snapshot for the UI.
type State struct {
    Enabled       bool      // from cfg
    BinaryPath    string    // ~/.atterm/bin/atterm-hook (symlink)
    BinaryOK      bool      // symlink resolves AND target is +x
    BinaryVersion string    // sha8 of currently-linked binary
    SettingsPath  string    // ~/.claude/settings.json
    SettingsOK    bool      // both atterm entries present, paths current
    LastError     string    // human-readable, one line; "" when healthy
    LastCheck     time.Time
}

// Check is a pure read of current on-disk state. Does NOT mutate.
// Cheap to call (~5 file stats); UI may call freely.
func Check(ctx context.Context) State
```

`desktop/app.go` wires them up:

```go
func (a *App) Startup(ctx context.Context) {
    ...
    if a.cfg.HookAutoInstallEnabled.OrDefault(true) {
        if err := hookinstall.Install(ctx); err != nil {
            log.Printf("hookinstall: install: %v", err)
        }
    }
    // existing HookServer.Start, Feishu service, ...
}

// Wails-bound:
func (a *App) GetHookInstallState() hookinstall.State {
    s := hookinstall.Check(a.ctx)
    if s.Enabled && !s.healthy() && !a.recentlyAttempted() {
        if err := hookinstall.Install(a.ctx); err == nil {
            s = hookinstall.Check(a.ctx)
        }
        a.markAttempted()
    }
    return s
}

func (a *App) SetHookInstallEnabled(on bool) error {
    a.cfg.Mutate(func(c *Config) { v := on; c.HookAutoInstallEnabled = &v })
    if on {
        return hookinstall.Install(a.ctx)
    }
    return hookinstall.Uninstall(a.ctx)
}
```

The 5-second `recentlyAttempted` guard prevents a Check→Install→Check
loop when Install legitimately can't succeed (e.g. settings.json on
a read-only mount).

## 5. Settings merge algorithm

The package owns two `Notification` hook entries in
`~/.claude/settings.json`:

```json
{
  "hooks": {
    "Notification": [
      { "matcher": {"type": "permission_prompt"},
        "command": "/Users/<user>/.atterm/bin/atterm-hook" },
      { "matcher": {"type": "idle_prompt", "tool": "AskUserQuestion"},
        "command": "/Users/<user>/.atterm/bin/atterm-hook" }
    ]
  }
}
```

`mergeAttermEntries` is a pure function in `settings.go`:

```go
// mergeAttermEntries strips every existing entry the marker recognizes
// as atterm-owned, then appends desired entries. Order of non-atterm
// entries is preserved. Idempotent: identical input → identical output.
func mergeAttermEntries(existing, desired []HookEntry, marker func(HookEntry) bool) []HookEntry {
    out := make([]HookEntry, 0, len(existing)+len(desired))
    for _, e := range existing {
        if !marker(e) {
            out = append(out, e)
        }
    }
    return append(out, desired...)
}
```

"Strip then append" is chosen over "in-place update" because:

- `Notification` hooks fire as an unordered OR — no consumer cares about
  position.
- Strip+append is byte-stable: running the algorithm N times produces
  the same output as running it once, with no special-case branches.
- "Update the matching entry's command field in place" would have to
  handle entries where the user changed the matcher but kept the path,
  vs. changed both, vs. duplicated — needless complexity.

Marker (`marker.go`):

```go
// isAttermHookCommand returns true for any command string containing
// "/.atterm/bin/atterm-hook". Uses substring match (not strict equality)
// so that paths with differing $HOME expansion forms still match.
func isAttermHookCommand(e HookEntry) bool {
    return strings.Contains(e.Command, "/.atterm/bin/atterm-hook")
}
```

A user whose own hook happens to contain that substring will see their
entry replaced. This is a corner case we accept; the substring is
specific enough that accidental collision is effectively zero.

`Install` writes `settings.json` only when the new bytes differ from
the existing bytes. Identical content → skip the write entirely.
Write path: write to `<settings.json>.atterm-tmp-<rand>`, then
`os.Rename` to the final path. Atomic on POSIX; settings.json is
never observed half-written by Claude.

When `settings.json` parses as invalid JSON, **we do not overwrite it**.
Install returns an error; UI shows "Claude settings.json has invalid
JSON, manual fix needed".

## 6. Binary distribution

Source flow:

```
cmd/atterm-hook/*.go
        │ (Makefile / build-hook-binary.sh)
        │ go build -trimpath -ldflags='-s -w' -o <embed-path>
        ▼
desktop/hookinstall/atterm-hook        ← gitignored
        │ //go:embed atterm-hook
        ▼
desktop/hookinstall/embed.go           ← embeddedHook []byte
        │
        │ sha256(embeddedHook)[:8] → embeddedHash
        ▼
desktop/hookinstall/binary.go          ← writes embeddedHook to
                                          ~/.atterm/bin/atterm-hook-<embeddedHash>
                                          updates symlink, GCs old
```

Reproducible builds: `-trimpath -s -w` strips file paths and DWARF;
identical source produces identical bytes, so `embeddedHash` only
changes when `cmd/atterm-hook/*.go` actually changes. Without this,
the per-launch "binary changed, rewrite" would fire on every desktop
upgrade even when atterm-hook didn't change.

`ensureBinary` flow:

```go
func ensureBinary() (binaryPath, version string, err error) {
    base := filepath.Join(home(), ".atterm", "bin")
    os.MkdirAll(base, 0o755)

    target := filepath.Join(base, "atterm-hook-"+embeddedHash)
    if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
        tmp := target + ".tmp-" + randSuffix()
        if err := os.WriteFile(tmp, embeddedHook, 0o755); err != nil {
            return "", "", err
        }
        if err := os.Rename(tmp, target); err != nil {
            os.Remove(tmp)
            return "", "", err
        }
    }

    symlink := filepath.Join(base, "atterm-hook")
    newLink := symlink + ".new"
    os.Remove(newLink)
    if err := os.Symlink(target, newLink); err != nil {
        return "", "", err
    }
    if err := os.Rename(newLink, symlink); err != nil {
        os.Remove(newLink)
        return "", "", err
    }

    // gcOldVersions: remove atterm-hook-* files whose path != target
    // AND whose mtime is older than 7 days.
    gcOldVersions(base, target, 7*24*time.Hour)
    return symlink, embeddedHash, nil
}
```

GC: delete `atterm-hook-*` files whose name isn't the current target
**and** whose mtime is older than 7 days. The 7-day window covers
long-running Claude sessions that might still `exec` a stale path
during a Notification hook.

Windows: `os.Symlink` requires admin or Developer Mode. The package
detects `runtime.GOOS == "windows"` early in `Install`, returns nil,
and `Check` reports `LastError: "auto-install unsupported on Windows"`.
The UI surfaces this in the same status row.

Build-side enforcement: a Makefile dependency makes `desktop/hookinstall/atterm-hook`
rebuild whenever `cmd/atterm-hook/*.go` changes. `wails dev` / `wails build`
both invoke through `make`. A `make verify-hook-embed` target in CI runs
the build, hashes the result, and confirms a freshly-built embed file
matches what's expected — catches accidental binary drift in PRs.

## 7. Health check + UI state

Three external triggers, single internal function:

1. `desktop/app.go` Startup — after Install, log the resulting State.
2. Settings · Feishu panel mount — frontend calls
   `App.GetHookInstallState()`, which Check+auto-repair as shown in §4.
3. `desktop/feishu/hook_server.go` — on a POST whose `AgentKind` is
   unknown or whose adapter rejects parsing (current
   `LookupHookAdapter` failure path), kick a single Check via a
   channel. Implemented as a no-arg callback the hook server holds.

Check semantics (read-only):

```
BinaryOK ⇔ symlink readable
        ∧ symlink target is a regular file
        ∧ target mode & 0o111 ≠ 0
SettingsOK ⇔ settings.json readable
        ∧ valid JSON
        ∧ Notification array contains ≥2 entries with marker
        ∧ each marker entry's command equals the current symlink path
        ∧ no marker entry points at a non-existent file
```

UI state mapping in `SettingsFeishu.vue`:

| Condition | Dot | Label |
|---|---|---|
| `Enabled && BinaryOK && SettingsOK` | green | "Hook installed and healthy" |
| `Enabled && !(BinaryOK && SettingsOK)` | amber | "Hook needs attention: <LastError>" |
| `!Enabled` | gray | "Hook auto-install disabled" |

Amber state shows a "Retry" button calling `SetHookInstallEnabled(true)`
(same as on, but forces an Install pass).

Logging:
- Install success → `log.Printf("hookinstall: installed v%s into %s", sha8, settingsPath)`.
- Install error → `log.Printf("hookinstall: %s", err)`.
- Check is silent (frontend polls it on panel open).

## 8. Error boundaries

| Failure | Behavior | LastError |
|---|---|---|
| `~/.atterm/bin/` not writable | Install returns; state amber; atterm starts normally | `cannot write hook binary: %s` |
| `~/.claude/settings.json` on a read-only path | Install returns (rename fails); state amber | `cannot update Claude settings: %s` |
| `settings.json` is invalid JSON | **Do not overwrite**; return error | `Claude settings.json has invalid JSON, manual fix needed` |
| Marker entry present but `command` path stale | SettingsOK=false; Install rewrites | (cleared on success) |
| Symlink creation fails (Windows w/o admin) | Whole auto-install disabled on Windows | `auto-install unsupported on Windows` |
| HookServer addr not yet bound | Install path doesn't depend on it; endpoint file is HookServer's | n/a |
| `~/.claude/settings.json` doesn't exist | Create it with `{"hooks":{"Notification":[...]}}` and `0o644` | (success) |
| `~/.claude/` directory doesn't exist | Create it `0o700`, then settings.json | (success) |

Explicitly **not** handled (YAGNI):
- Concurrent writers racing on `settings.json`. Atomic rename gives
  last-writer-wins, not consistency. Users don't edit settings.json
  concurrently with atterm Startup.
- macOS Gatekeeper / quarantine on the embedded binary. The quarantine
  xattr is only set on files downloaded via a known download API;
  `os.WriteFile` does not set it.

## 9. Testing

Unit tests, all table-driven, all using injected `home string`
parameters (no real `os.UserHomeDir` in tests):

| File | Coverage |
|---|---|
| `settings_test.go` | `mergeAttermEntries`: empty existing; all-atterm; all-external; mixed; marker false-positive on user command containing the substring; preserve non-atterm order; preserve other top-level fields under `hooks` |
| `marker_test.go` | `isAttermHookCommand`: exact path, absolute path, symlink path, command with args, command with env prefix |
| `binary_test.go` | `ensureBinary`: first install; idempotent re-run; stale symlink retargeted; GC keeps fresh files; GC removes 8-day-old files; write under read-only base errors cleanly |
| `installer_test.go` | `Install`: missing settings.json → created with both entries; existing with external hooks → preserved + atterm appended; already-installed → no rewrite (byte-equal skip); invalid JSON → no overwrite, returns error |
| `health_test.go` | `Check`: every BinaryOK/SettingsOK failure mode produces a one-line `LastError` |
| `embed_test.go` | `TestEmbeddedBinaryRunnable`: writes embed bytes to temp, `exec.Command(path, "--version")` exits 0 |

Integration:
- `desktop/app_hookinstall_test.go`: fake `$HOME`, run `app.Startup`,
  assert `settings.json` contains both entries; toggle off, assert
  removed but external entries intact; toggle on again, full circle.

Manual E2E (added to `scripts/feishu-hook-e2e-checklist.md`):
- Fresh launch on a machine with no `~/.claude/settings.json` →
  file created with both atterm entries.
- Fresh launch with pre-existing user `Notification` hook → both atterm
  entries appended, user entry preserved verbatim.
- Disable in Settings → atterm entries removed, user entry intact.
- `chmod 000` on the symlink target → relaunch atterm → state goes
  green within one second (auto-repair).

## 10. Out of scope (future work)

- Codex (`~/.codex/...`) — different hook file layout; deferred to a
  follow-up spec. The marker/merge algorithm and binary distribution
  can be reused.
- Generic JSON webhook hook installer for non-AI agents — same
  shape, different settings location; speculative until a user asks.
- Transcript-derived UI surfaces (token usage, conversation timeline).
  Brainstorming determined this isn't user-valuable today.

## 11. Open questions

None. All decisions made during brainstorming:

- Auto-install on by default; Settings toggle.
- Binary lives at `~/.atterm/bin/atterm-hook-<sha8>` with `atterm-hook`
  symlink; embedded via `go:embed`.
- Silent auto-repair on UI panel open; status dot in
  Settings · Feishu.
- `claude-code` only; Codex deferred.
- User-level `~/.claude/settings.json` (not `settings.local.json`).
- Strip+append merge keyed by command-substring marker.
