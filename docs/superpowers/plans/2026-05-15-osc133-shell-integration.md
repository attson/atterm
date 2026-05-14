# OSC 133 Shell Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Auto-inject OSC 133 shell hooks at PTY spawn time (zsh/bash/fish/pwsh), parse OSC 133 events in the desktop frontend's xterm.js, and fire a system notification when a command finishes while the window is unfocused and the command ran ≥ a configurable threshold.

**Architecture:** New Go package `desktop/shellintegration/` produces a `Plan` (extra env / args / cleanup) based on detected shell; `desktop/relay_host.go` merges it into `ptyhost.Open`. Frontend `lib/commandFinish.ts` consumes `OSC 133` payloads via `term.parser.registerOscHandler(133, ...)`, gates on focus + threshold + local-session, and routes to the existing `ShowNotification` binding. Two new `appConfig` fields (`ShellIntegrationEnabled *bool`, `CommandNotifyThresholdSeconds *int`) drive the toggle and threshold.

**Tech Stack:** Go (Wails v2), Vue 3, TypeScript, xterm.js, vitest, Go testing

**Spec:** `docs/superpowers/specs/2026-05-14-osc133-shell-integration-design.md`

---

## File Map

**Backend (new):**
- `desktop/shellintegration/detect.go` — basename → shell enum
- `desktop/shellintegration/detect_test.go`
- `desktop/shellintegration/prepare.go` — `Plan` struct + public `Prepare()` entrypoint
- `desktop/shellintegration/prepare_test.go`
- `desktop/shellintegration/zsh.go` — write wrapper `.zshrc` + snippet under `ZDOTDIR`
- `desktop/shellintegration/zsh_test.go`
- `desktop/shellintegration/bash.go` — write rcfile; return `--rcfile <path> -i`
- `desktop/shellintegration/bash_test.go`
- `desktop/shellintegration/fish.go` — write to `$XDG_CONFIG_HOME/fish/conf.d/atterm-integration.fish`
- `desktop/shellintegration/fish_test.go`
- `desktop/shellintegration/pwsh.go` — write `<sid>.ps1`; return `-NoExit -Command "& '<path>'"`
- `desktop/shellintegration/pwsh_test.go`
- `desktop/shellintegration/snippets.go` — `//go:embed snippets/*` directives
- `desktop/shellintegration/snippets_test.go`
- `desktop/shellintegration/snippets/atterm.zsh`
- `desktop/shellintegration/snippets/atterm.bash`
- `desktop/shellintegration/snippets/atterm.fish`
- `desktop/shellintegration/snippets/atterm.ps1`

**Backend (modify):**
- `desktop/config.go` — add `ShellIntegrationEnabled *bool` + `CommandNotifyThresholdSeconds *int` + `OrDefault` methods
- `desktop/config_shell_integration_test.go` — new test file for defaults + clamp
- `desktop/app.go` — add 4 Wails bindings
- `desktop/app_shell_integration_test.go` — new test file for binding round-trip
- `desktop/relay_host.go` — wire `shellintegration.Prepare` into `NewSession`
- `desktop/relay_host_shell_integration_test.go` — new test file for wiring

**Frontend (new):**
- `desktop/frontend/src/lib/commandFinish.ts`
- `desktop/frontend/src/lib/commandFinish.test.ts`

**Frontend (modify):**
- `desktop/frontend/src/lib/api.ts` — 4 new wrappers + `AppBindings` entries
- `desktop/frontend/src/components/TerminalView.vue` — OSC handler, threshold prop, local-session guard
- `desktop/frontend/src/components/TerminalView.test.ts` — extend
- `desktop/frontend/src/components/PaneGrid.vue` — forward props
- `desktop/frontend/src/App.vue` — load + plumb threshold
- `desktop/frontend/src/components/SettingsGeneral.vue` — UI
- `desktop/frontend/src/components/SettingsGeneral.test.ts` — extend

**Docs (new):**
- `docs/shell-integration.md`

**Docs (modify):**
- `README.md` — link to `docs/shell-integration.md`

---

## Conventions

All commands assume the project root `/Users/attson/code/github.com.attson/atterm`. Shell prologue (use for every Go test command):

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
```

Frontend tests use vitest via `npm run test -- <file>` from `desktop/frontend/`.

Commit messages follow the existing terse `<type>: <subject>` style (`feat: …`, `fix: …`, `test: …`, `docs: …`).

---

### Task 1: `detect.go` — Shell Name Detection

Resolves the shell binary path to one of four supported enums or `""`.

**Files:**
- Create: `desktop/shellintegration/detect.go`
- Create: `desktop/shellintegration/detect_test.go`
- Test: `desktop/shellintegration/detect_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/shellintegration/detect_test.go`:

```go
package shellintegration

import "testing"

func TestDetectShell(t *testing.T) {
	tests := []struct {
		name string
		path string
		want Shell
	}{
		{"empty", "", ShellUnknown},
		{"zsh absolute", "/bin/zsh", ShellZsh},
		{"zsh homebrew", "/opt/homebrew/bin/zsh", ShellZsh},
		{"bash absolute", "/bin/bash", ShellBash},
		{"bash usr", "/usr/bin/bash", ShellBash},
		{"fish", "/opt/homebrew/bin/fish", ShellFish},
		{"pwsh posix", "/usr/local/bin/pwsh", ShellPwsh},
		{"pwsh windows exe", `C:\Program Files\PowerShell\7\pwsh.exe`, ShellPwsh},
		{"powershell exe", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, ShellPwsh},
		{"cmd not supported", `C:\Windows\System32\cmd.exe`, ShellUnknown},
		{"nu not supported", "/opt/homebrew/bin/nu", ShellUnknown},
		{"basename only", "zsh", ShellZsh},
		{"basename only pwsh.exe", "pwsh.exe", ShellPwsh},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectShell(tt.path)
			if got != tt.want {
				t.Fatalf("DetectShell(%q) = %v; want %v", tt.path, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test ./desktop/shellintegration/...
```

Expected: build failure (`Shell`, `ShellUnknown`, `ShellZsh`, `ShellBash`, `ShellFish`, `ShellPwsh`, `DetectShell` undefined).

- [ ] **Step 3: Write the implementation**

Create `desktop/shellintegration/detect.go`:

```go
// Package shellintegration prepares per-shell injection plans so that
// atterm-spawned PTYs emit OSC 133 command boundary markers.
package shellintegration

import (
	"path/filepath"
	"strings"
)

// Shell enumerates the shells we know how to inject into. ShellUnknown is the
// fallback for empty paths, unsupported shells (cmd.exe, nu, xonsh, elvish),
// or detection failures.
type Shell int

const (
	ShellUnknown Shell = iota
	ShellZsh
	ShellBash
	ShellFish
	ShellPwsh
)

func (s Shell) String() string {
	switch s {
	case ShellZsh:
		return "zsh"
	case ShellBash:
		return "bash"
	case ShellFish:
		return "fish"
	case ShellPwsh:
		return "pwsh"
	default:
		return ""
	}
}

// DetectShell maps a shell binary path to a Shell. Comparison is on the
// lowercased basename without the .exe suffix; absolute paths and bare
// names both work.
func DetectShell(path string) Shell {
	if path == "" {
		return ShellUnknown
	}
	base := strings.ToLower(filepath.Base(path))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "zsh":
		return ShellZsh
	case "bash":
		return ShellBash
	case "fish":
		return ShellFish
	case "pwsh", "powershell":
		return ShellPwsh
	default:
		return ShellUnknown
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: `ok  github.com/attson/atterm/desktop/shellintegration`.

- [ ] **Step 5: Commit**

```bash
git add desktop/shellintegration/detect.go desktop/shellintegration/detect_test.go
git commit -m "feat: detect shell from binary path for OSC 133 integration"
```

---

### Task 2: `Plan` Struct + `Prepare()` Skeleton

Public surface lives here. Implementation for each shell ships in later tasks; `Prepare` initially returns a zero `Plan` for every input, plus per-shell dispatch wired in subsequent tasks.

**Files:**
- Create: `desktop/shellintegration/prepare.go`
- Create: `desktop/shellintegration/prepare_test.go`
- Test: `desktop/shellintegration/prepare_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/shellintegration/prepare_test.go`:

```go
package shellintegration

import "testing"

func TestPrepareReturnsZeroPlanWhenDisabled(t *testing.T) {
	got := Prepare("/bin/zsh", false, "sid")
	if got.Shell != "" || len(got.ExtraEnv) != 0 || len(got.ExtraArgs) != 0 || got.Cleanup != nil {
		t.Fatalf("Prepare disabled returned non-zero plan: %+v", got)
	}
}

func TestPrepareReturnsZeroPlanForUnknownShell(t *testing.T) {
	got := Prepare("/bin/cmd.exe", true, "sid")
	if got.Shell != "" || len(got.ExtraEnv) != 0 || len(got.ExtraArgs) != 0 || got.Cleanup != nil {
		t.Fatalf("Prepare unknown shell returned non-zero plan: %+v", got)
	}
}

func TestPrepareReturnsZeroPlanForEmptyPath(t *testing.T) {
	got := Prepare("", true, "sid")
	if got.Shell != "" || len(got.ExtraEnv) != 0 || len(got.ExtraArgs) != 0 || got.Cleanup != nil {
		t.Fatalf("Prepare empty path returned non-zero plan: %+v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: build failure (`Plan`, `Prepare` undefined).

- [ ] **Step 3: Write the implementation**

Create `desktop/shellintegration/prepare.go`:

```go
package shellintegration

// Plan describes how to spawn a shell with OSC 133 hooks injected.
// ExtraEnv is appended to the child's environment; ExtraArgs is appended
// to argv after the shell binary. Cleanup is non-nil when temporary files
// need removal at session close; callers MUST nil-check before invoking.
// Shell is purely informational (used in logs).
type Plan struct {
	ExtraEnv  []string
	ExtraArgs []string
	Cleanup   func()
	Shell     string
}

// Prepare returns a Plan for the given shell. If enabled is false, the path
// is empty, or the shell is unsupported, Prepare returns a zero Plan. The
// sessionID is used to scope temporary files so concurrent sessions do not
// collide. Prepare never returns an error: internal failures (mkdir, write)
// yield a zero Plan plus a one-time log line.
func Prepare(shellPath string, enabled bool, sessionID string) Plan {
	if !enabled {
		return Plan{}
	}
	switch DetectShell(shellPath) {
	case ShellZsh:
		return prepareZsh(sessionID)
	case ShellBash:
		return prepareBash(sessionID)
	case ShellFish:
		return prepareFish()
	case ShellPwsh:
		return preparePwsh(sessionID)
	default:
		return Plan{}
	}
}
```

Create stubs in four files so the package builds; the per-shell tasks below replace them. Add to `desktop/shellintegration/zsh.go`:

```go
package shellintegration

func prepareZsh(sessionID string) Plan { return Plan{} }
```

Add to `desktop/shellintegration/bash.go`:

```go
package shellintegration

func prepareBash(sessionID string) Plan { return Plan{} }
```

Add to `desktop/shellintegration/fish.go`:

```go
package shellintegration

func prepareFish() Plan { return Plan{} }
```

Add to `desktop/shellintegration/pwsh.go`:

```go
package shellintegration

func preparePwsh(sessionID string) Plan { return Plan{} }
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/shellintegration/prepare.go desktop/shellintegration/prepare_test.go \
        desktop/shellintegration/zsh.go desktop/shellintegration/bash.go \
        desktop/shellintegration/fish.go desktop/shellintegration/pwsh.go
git commit -m "feat: add Plan struct and Prepare skeleton for shell integration"
```

---

### Task 3: Snippet Embeds + Per-Snippet Guard Test

All four snippets ship as embedded files. The guards (`ATTERM_SHELL_INTEGRATION` etc.) live in the snippet itself so per-shell tasks can assert presence.

**Files:**
- Create: `desktop/shellintegration/snippets.go`
- Create: `desktop/shellintegration/snippets_test.go`
- Create: `desktop/shellintegration/snippets/atterm.zsh`
- Create: `desktop/shellintegration/snippets/atterm.bash`
- Create: `desktop/shellintegration/snippets/atterm.fish`
- Create: `desktop/shellintegration/snippets/atterm.ps1`
- Test: `desktop/shellintegration/snippets_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/shellintegration/snippets_test.go`:

```go
package shellintegration

import (
	"strings"
	"testing"
)

func TestEmbeddedSnippetsArePresent(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"zsh", zshSnippet},
		{"bash", bashSnippet},
		{"fish", fishSnippet},
		{"pwsh", pwshSnippet},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.content) == 0 {
				t.Fatalf("%s snippet is empty", c.name)
			}
		})
	}
}

func TestZshSnippetHasGuardAndHookRegistration(t *testing.T) {
	if !strings.Contains(zshSnippet, `ATTERM_SHELL_INTEGRATION`) {
		t.Fatalf("zsh snippet missing ATTERM_SHELL_INTEGRATION guard")
	}
	if !strings.Contains(zshSnippet, "preexec_functions") {
		t.Fatalf("zsh snippet does not append to preexec_functions")
	}
	if !strings.Contains(zshSnippet, "precmd_functions") {
		t.Fatalf("zsh snippet does not append to precmd_functions")
	}
	if !strings.Contains(zshSnippet, `\033]133`) && !strings.Contains(zshSnippet, `\x1b]133`) {
		t.Fatalf("zsh snippet does not emit OSC 133 sequences")
	}
}

func TestBashSnippetHasGuardAndHookRegistration(t *testing.T) {
	if !strings.Contains(bashSnippet, `ATTERM_SHELL_INTEGRATION`) {
		t.Fatalf("bash snippet missing ATTERM_SHELL_INTEGRATION guard")
	}
	if !strings.Contains(bashSnippet, "PROMPT_COMMAND") {
		t.Fatalf("bash snippet does not chain into PROMPT_COMMAND")
	}
	if !strings.Contains(bashSnippet, "DEBUG") {
		t.Fatalf("bash snippet does not trap DEBUG for preexec")
	}
}

func TestFishSnippetHasGuardAndEventHooks(t *testing.T) {
	if !strings.Contains(fishSnippet, "__atterm_loaded") {
		t.Fatalf("fish snippet missing __atterm_loaded guard")
	}
	if !strings.Contains(fishSnippet, "fish_preexec") {
		t.Fatalf("fish snippet missing fish_preexec hook")
	}
	if !strings.Contains(fishSnippet, "fish_postexec") {
		t.Fatalf("fish snippet missing fish_postexec hook")
	}
}

func TestPwshSnippetHasGuardAndPromptWrapper(t *testing.T) {
	if !strings.Contains(pwshSnippet, "ATTERM_SHELL_INTEGRATION") {
		t.Fatalf("pwsh snippet missing ATTERM_SHELL_INTEGRATION guard")
	}
	if !strings.Contains(pwshSnippet, "function prompt") && !strings.Contains(pwshSnippet, "function global:prompt") {
		t.Fatalf("pwsh snippet does not wrap the prompt function")
	}
	if !strings.Contains(pwshSnippet, "PSReadLine") && !strings.Contains(pwshSnippet, "AddToHistoryHandler") && !strings.Contains(pwshSnippet, "133;C") {
		t.Fatalf("pwsh snippet does not emit OSC 133;C on preexec (either via PSReadLine handler or inline)")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: build failure (`zshSnippet`, `bashSnippet`, `fishSnippet`, `pwshSnippet` undefined).

- [ ] **Step 3: Create the snippet files**

Create `desktop/shellintegration/snippets/atterm.zsh`:

```sh
# atterm shell integration — OSC 133 command boundary markers
# Loaded by atterm-spawned zsh sessions via a wrapper $ZDOTDIR/.zshrc.
# Safe to source manually outside atterm; the guard prevents double-load.

if [[ -n "${ATTERM_SHELL_INTEGRATION_LOADED:-}" ]]; then
  return 0
fi
ATTERM_SHELL_INTEGRATION_LOADED=1

__atterm_prompt_start() { printf '\033]133;A\007'; }
__atterm_prompt_end()   { printf '\033]133;B\007'; }
__atterm_preexec()      { printf '\033]133;C\007'; }
__atterm_precmd()       { printf '\033]133;D;%s\007' "$?"; }

# Use additive hook arrays so frameworks (oh-my-zsh, powerlevel10k, starship)
# keep their own precmd/preexec entries intact.
typeset -ag precmd_functions
typeset -ag preexec_functions
precmd_functions+=(__atterm_precmd)
preexec_functions+=(__atterm_preexec)

# Wrap PS1 with prompt-start / prompt-end markers without disturbing the rest
# of the prompt. %{ ... %} prevents zsh from counting these bytes against the
# visible prompt width.
PS1='%{$(__atterm_prompt_start)%}'"${PS1}"'%{$(__atterm_prompt_end)%}'
```

Create `desktop/shellintegration/snippets/atterm.bash`:

```sh
# atterm shell integration — OSC 133 command boundary markers
# Loaded by atterm-spawned bash sessions via --rcfile.

if [[ -n "${ATTERM_SHELL_INTEGRATION_LOADED:-}" ]]; then
  return 0
fi
ATTERM_SHELL_INTEGRATION_LOADED=1

__atterm_prompt_start='\[\033]133;A\007\]'
__atterm_prompt_end='\[\033]133;B\007\]'

__atterm_preexec() {
  # Skip programmable-completion and PROMPT_COMMAND re-entries; only fire on
  # interactive command starts. BASH_COMMAND holds the about-to-run command.
  [[ -n "$COMP_LINE" ]] && return
  [[ "$BASH_COMMAND" == "$PROMPT_COMMAND" ]] && return
  printf '\033]133;C\007'
}

__atterm_precmd() {
  local exit=$?
  printf '\033]133;D;%s\007' "$exit"
}

# Chain into existing PROMPT_COMMAND rather than overwriting it.
PROMPT_COMMAND="__atterm_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"

# Bash has no native preexec; trap DEBUG approximates it.
trap '__atterm_preexec' DEBUG

PS1="${__atterm_prompt_start}${PS1}${__atterm_prompt_end}"
```

Create `desktop/shellintegration/snippets/atterm.fish`:

```fish
# atterm shell integration — OSC 133 command boundary markers
# Auto-loaded by fish from $XDG_CONFIG_HOME/fish/conf.d/.

if set -q __atterm_loaded
    exit 0
end
set -g __atterm_loaded 1

function __atterm_preexec --on-event fish_preexec
    printf '\033]133;C\007'
end

function __atterm_postexec --on-event fish_postexec
    printf '\033]133;D;%s\007' $status
end

# Wrap fish_prompt without clobbering the user's definition. If they have one,
# rename it so we can call it from our wrapper; if not, our wrapper provides a
# minimal default. Either way the markers always bracket the real prompt.
if functions -q fish_prompt
    functions --copy fish_prompt __atterm_user_prompt
else
    function __atterm_user_prompt
        printf '%s@%s %s> ' $USER (prompt_hostname) (prompt_pwd)
    end
end

function fish_prompt
    printf '\033]133;A\007'
    __atterm_user_prompt
    printf '\033]133;B\007'
end
```

Create `desktop/shellintegration/snippets/atterm.ps1`:

```powershell
# atterm shell integration — OSC 133 command boundary markers
# Sourced by atterm-spawned PowerShell sessions via -NoExit -Command.

if ($env:ATTERM_SHELL_INTEGRATION_LOADED) { return }
$env:ATTERM_SHELL_INTEGRATION_LOADED = "1"

$global:__atterm_last_exit = 0

# Preserve the user's original prompt function (if any) so frameworks like
# oh-my-posh still render normally; we only wrap markers around it.
if (Get-Command __atterm_user_prompt -ErrorAction SilentlyContinue) { } else {
    if (Test-Path Function:\prompt) {
        Copy-Item Function:\prompt Function:\__atterm_user_prompt
    } else {
        function global:__atterm_user_prompt { "PS $($executionContext.SessionState.Path.CurrentLocation)$('>' * ($nestedPromptLevel + 1)) " }
    }
}

function global:prompt {
    $exit = if ($?) { 0 } else { 1 }
    if ($LASTEXITCODE -ne $null) { $exit = $LASTEXITCODE }
    [Console]::Write("`e]133;D;$exit`a")
    [Console]::Write("`e]133;A`a")
    $p = __atterm_user_prompt
    [Console]::Write("`e]133;B`a")
    return $p
}

# Emit OSC 133;C just before a command starts running. PSReadLine fires
# OnExecute right after the user hits Enter, which is the closest hook to
# preexec; if PSReadLine is not loaded we fall back to a no-op (the user
# can still get D/A/B markers).
if (Get-Module -ListAvailable PSReadLine) {
    Import-Module PSReadLine
    Set-PSReadLineKeyHandler -Key Enter -ScriptBlock {
        [Console]::Write("`e]133;C`a")
        [Microsoft.PowerShell.PSConsoleReadLine]::AcceptLine()
    }
}
```

Create `desktop/shellintegration/snippets.go`:

```go
package shellintegration

import _ "embed"

//go:embed snippets/atterm.zsh
var zshSnippet string

//go:embed snippets/atterm.bash
var bashSnippet string

//go:embed snippets/atterm.fish
var fishSnippet string

//go:embed snippets/atterm.ps1
var pwshSnippet string
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: PASS for all `TestEmbeddedSnippetsArePresent` / `Test{Zsh,Bash,Fish,Pwsh}Snippet...` subtests.

- [ ] **Step 5: Commit**

```bash
git add desktop/shellintegration/snippets.go desktop/shellintegration/snippets_test.go \
        desktop/shellintegration/snippets/
git commit -m "feat: embed OSC 133 shell integration snippets for zsh/bash/fish/pwsh"
```

---

### Task 4: `zsh.go` — Write Wrapper `.zshrc` + Snippet, Return ZDOTDIR

**Files:**
- Modify: `desktop/shellintegration/zsh.go`
- Create: `desktop/shellintegration/zsh_test.go`
- Test: `desktop/shellintegration/zsh_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/shellintegration/zsh_test.go`:

```go
package shellintegration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareZshWritesWrapperAndReturnsPlan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// UserCacheDir is HOME-derived on linux/darwin once HOME is set; on macOS
	// it is $HOME/Library/Caches. The test only asserts that the dir lives
	// somewhere under dir; let UserCacheDir resolve naturally.
	t.Setenv("ZDOTDIR", "")

	p := prepareZsh("sess-1234")
	if p.Shell != "zsh" {
		t.Fatalf("Plan.Shell = %q; want zsh", p.Shell)
	}
	if p.Cleanup == nil {
		t.Fatalf("Plan.Cleanup is nil; zsh prepare must register cleanup")
	}
	defer p.Cleanup()

	zdir := ""
	for _, env := range p.ExtraEnv {
		if strings.HasPrefix(env, "ZDOTDIR=") {
			zdir = strings.TrimPrefix(env, "ZDOTDIR=")
		}
	}
	if zdir == "" {
		t.Fatalf("Plan.ExtraEnv missing ZDOTDIR; got %v", p.ExtraEnv)
	}

	// The wrapper .zshrc must exist and source both the user's rc and the
	// embedded atterm.zsh.
	wrapperRC := filepath.Join(zdir, ".zshrc")
	body, err := os.ReadFile(wrapperRC)
	if err != nil {
		t.Fatalf("read wrapper rc: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "ATTERM_ORIG_ZDOTDIR") {
		t.Fatalf("wrapper rc does not propagate ATTERM_ORIG_ZDOTDIR; got %q", got)
	}
	if !strings.Contains(got, "atterm.zsh") {
		t.Fatalf("wrapper rc does not source atterm.zsh; got %q", got)
	}

	// The snippet itself must be copied alongside so the wrapper can source it
	// without depending on file paths inside the binary.
	if _, err := os.Stat(filepath.Join(zdir, "atterm.zsh")); err != nil {
		t.Fatalf("snippet not written next to wrapper: %v", err)
	}

	// ATTERM_ORIG_ZDOTDIR must be exported so the wrapper can source the
	// user's original rc. Empty original is OK ($HOME fallback).
	foundOrig := false
	for _, env := range p.ExtraEnv {
		if strings.HasPrefix(env, "ATTERM_ORIG_ZDOTDIR=") {
			foundOrig = true
		}
	}
	if !foundOrig {
		t.Fatalf("Plan.ExtraEnv missing ATTERM_ORIG_ZDOTDIR; got %v", p.ExtraEnv)
	}

	// Cleanup should remove the temp dir.
	p.Cleanup()
	if _, err := os.Stat(zdir); !os.IsNotExist(err) {
		t.Fatalf("Cleanup did not remove dir %s: err=%v", zdir, err)
	}
}

func TestPrepareZshReturnsZeroPlanWhenCacheDirFails(t *testing.T) {
	// Force os.UserCacheDir to fail by clearing both HOME and XDG_CACHE_HOME
	// and (on darwin) HOME-derived candidates.
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	p := prepareZsh("sess-empty")
	if p.Shell != "" || len(p.ExtraEnv) != 0 || p.Cleanup != nil {
		t.Fatalf("expected zero Plan on cache-dir failure; got %+v", p)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: the new tests FAIL because the stub `prepareZsh` returns `Plan{}`.

- [ ] **Step 3: Write the implementation**

Replace `desktop/shellintegration/zsh.go`:

```go
package shellintegration

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func prepareZsh(sessionID string) Plan {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		log.Printf("shellintegration: zsh cache dir unavailable: %v", err)
		return Plan{}
	}
	dir := filepath.Join(cacheDir, "atterm", "shell-integration", "zsh-"+sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("shellintegration: zsh mkdir %s: %v", dir, err)
		return Plan{}
	}

	snippetPath := filepath.Join(dir, "atterm.zsh")
	if err := os.WriteFile(snippetPath, []byte(zshSnippet), 0o600); err != nil {
		log.Printf("shellintegration: zsh write snippet: %v", err)
		_ = os.RemoveAll(dir)
		return Plan{}
	}

	wrapper := fmt.Sprintf(`# atterm zsh wrapper — sources user rc then injects OSC 133 hooks.
_atterm_orig="${ATTERM_ORIG_ZDOTDIR}"
if [[ -z "$_atterm_orig" ]]; then
  _atterm_orig="$HOME"
fi
if [[ -f "$_atterm_orig/.zshrc" ]]; then
  source "$_atterm_orig/.zshrc" || true
fi
unset _atterm_orig
source %q || true
`, snippetPath)

	wrapperPath := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o600); err != nil {
		log.Printf("shellintegration: zsh write wrapper: %v", err)
		_ = os.RemoveAll(dir)
		return Plan{}
	}

	origZDOTDIR := os.Getenv("ZDOTDIR")

	return Plan{
		Shell: "zsh",
		ExtraEnv: []string{
			"ZDOTDIR=" + dir,
			"ATTERM_ORIG_ZDOTDIR=" + origZDOTDIR,
			"ATTERM_SHELL_INTEGRATION=1",
		},
		Cleanup: func() {
			if err := os.RemoveAll(dir); err != nil {
				log.Printf("shellintegration: zsh cleanup %s: %v", dir, err)
			}
		},
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/shellintegration/zsh.go desktop/shellintegration/zsh_test.go
git commit -m "feat: prepare zsh wrapper ZDOTDIR with OSC 133 snippet"
```

---

### Task 5: `bash.go` — Write rcfile, Return `--rcfile <path> -i`

**Files:**
- Modify: `desktop/shellintegration/bash.go`
- Create: `desktop/shellintegration/bash_test.go`
- Test: `desktop/shellintegration/bash_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/shellintegration/bash_test.go`:

```go
package shellintegration

import (
	"os"
	"strings"
	"testing"
)

func TestPrepareBashWritesRcfileAndReturnsArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	p := prepareBash("sess-bash-1")
	if p.Shell != "bash" {
		t.Fatalf("Plan.Shell = %q; want bash", p.Shell)
	}
	if p.Cleanup == nil {
		t.Fatalf("Plan.Cleanup is nil; bash prepare must register cleanup")
	}
	defer p.Cleanup()

	if len(p.ExtraArgs) < 3 || p.ExtraArgs[0] != "--rcfile" || p.ExtraArgs[2] != "-i" {
		t.Fatalf("Plan.ExtraArgs = %v; want [--rcfile <path> -i]", p.ExtraArgs)
	}

	rcPath := p.ExtraArgs[1]
	body, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("read rcfile %s: %v", rcPath, err)
	}
	got := string(body)
	if !strings.Contains(got, "~/.bashrc") {
		t.Fatalf("rcfile does not source ~/.bashrc; got %q", got)
	}
	if !strings.Contains(got, "atterm.bash") {
		t.Fatalf("rcfile does not source atterm.bash; got %q", got)
	}

	foundEnv := false
	for _, env := range p.ExtraEnv {
		if env == "ATTERM_SHELL_INTEGRATION=1" {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Fatalf("Plan.ExtraEnv missing ATTERM_SHELL_INTEGRATION=1; got %v", p.ExtraEnv)
	}

	p.Cleanup()
	if _, err := os.Stat(rcPath); !os.IsNotExist(err) {
		t.Fatalf("Cleanup did not remove rcfile %s: err=%v", rcPath, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: FAIL because stub returns `Plan{}`.

- [ ] **Step 3: Write the implementation**

Replace `desktop/shellintegration/bash.go`:

```go
package shellintegration

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func prepareBash(sessionID string) Plan {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		log.Printf("shellintegration: bash cache dir unavailable: %v", err)
		return Plan{}
	}
	dir := filepath.Join(cacheDir, "atterm", "shell-integration")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("shellintegration: bash mkdir %s: %v", dir, err)
		return Plan{}
	}

	snippetPath := filepath.Join(dir, "atterm-"+sessionID+".bash")
	if err := os.WriteFile(snippetPath, []byte(bashSnippet), 0o600); err != nil {
		log.Printf("shellintegration: bash write snippet: %v", err)
		return Plan{}
	}

	rcfile := fmt.Sprintf(`# atterm bash rcfile — sources user rc then injects OSC 133 hooks.
[[ -f ~/.bashrc ]] && source ~/.bashrc
source %q || true
`, snippetPath)

	rcPath := filepath.Join(dir, "atterm-"+sessionID+".rc")
	if err := os.WriteFile(rcPath, []byte(rcfile), 0o600); err != nil {
		log.Printf("shellintegration: bash write rcfile: %v", err)
		_ = os.Remove(snippetPath)
		return Plan{}
	}

	return Plan{
		Shell:     "bash",
		ExtraArgs: []string{"--rcfile", rcPath, "-i"},
		ExtraEnv:  []string{"ATTERM_SHELL_INTEGRATION=1"},
		Cleanup: func() {
			if err := os.Remove(rcPath); err != nil && !os.IsNotExist(err) {
				log.Printf("shellintegration: bash cleanup %s: %v", rcPath, err)
			}
			if err := os.Remove(snippetPath); err != nil && !os.IsNotExist(err) {
				log.Printf("shellintegration: bash cleanup %s: %v", snippetPath, err)
			}
		},
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/shellintegration/bash.go desktop/shellintegration/bash_test.go
git commit -m "feat: prepare bash --rcfile wrapper with OSC 133 snippet"
```

---

### Task 6: `fish.go` — Write `conf.d/atterm-integration.fish`, No Cleanup

**Files:**
- Modify: `desktop/shellintegration/fish.go`
- Create: `desktop/shellintegration/fish_test.go`
- Test: `desktop/shellintegration/fish_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/shellintegration/fish_test.go`:

```go
package shellintegration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareFishWritesToConfdAndReturnsNilCleanup(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	p := prepareFish()
	if p.Shell != "fish" {
		t.Fatalf("Plan.Shell = %q; want fish", p.Shell)
	}
	if p.Cleanup != nil {
		t.Fatalf("fish Plan.Cleanup must be nil; got non-nil")
	}
	if len(p.ExtraArgs) != 0 {
		t.Fatalf("fish Plan.ExtraArgs must be empty; got %v", p.ExtraArgs)
	}

	target := filepath.Join(xdg, "fish", "conf.d", "atterm-integration.fish")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("conf.d snippet not written to %s: %v", target, err)
	}
	if !strings.Contains(string(body), "fish_preexec") {
		t.Fatalf("conf.d snippet missing fish_preexec hook; got %q", string(body))
	}

	foundEnv := false
	for _, env := range p.ExtraEnv {
		if env == "ATTERM_SHELL_INTEGRATION=1" {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Fatalf("Plan.ExtraEnv missing ATTERM_SHELL_INTEGRATION=1; got %v", p.ExtraEnv)
	}
}

func TestPrepareFishFallsBackToHomeConfigWhenXdgUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	p := prepareFish()
	if p.Shell != "fish" {
		t.Fatalf("Plan.Shell = %q; want fish", p.Shell)
	}

	target := filepath.Join(home, ".config", "fish", "conf.d", "atterm-integration.fish")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected fallback path %s to exist: %v", target, err)
	}
}

func TestPrepareFishOverwriteIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))

	p1 := prepareFish()
	if p1.Shell != "fish" {
		t.Fatalf("first prepare returned non-fish plan: %+v", p1)
	}

	p2 := prepareFish()
	if p2.Shell != "fish" {
		t.Fatalf("second prepare returned non-fish plan: %+v", p2)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: FAIL.

- [ ] **Step 3: Write the implementation**

Replace `desktop/shellintegration/fish.go`:

```go
package shellintegration

import (
	"log"
	"os"
	"path/filepath"
)

func prepareFish() Plan {
	confDir, err := fishConfDir()
	if err != nil {
		log.Printf("shellintegration: fish conf dir unavailable: %v", err)
		return Plan{}
	}
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		log.Printf("shellintegration: fish mkdir %s: %v", confDir, err)
		return Plan{}
	}
	target := filepath.Join(confDir, "atterm-integration.fish")
	if err := os.WriteFile(target, []byte(fishSnippet), 0o644); err != nil {
		log.Printf("shellintegration: fish write %s: %v", target, err)
		return Plan{}
	}
	return Plan{
		Shell:    "fish",
		ExtraEnv: []string{"ATTERM_SHELL_INTEGRATION=1"},
	}
}

func fishConfDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "fish", "conf.d"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "fish", "conf.d"), nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/shellintegration/fish.go desktop/shellintegration/fish_test.go
git commit -m "feat: write fish OSC 133 snippet to conf.d (idempotent, no cleanup)"
```

---

### Task 7: `pwsh.go` — Write Per-Session `.ps1`, Return `-NoExit -Command`

**Files:**
- Modify: `desktop/shellintegration/pwsh.go`
- Create: `desktop/shellintegration/pwsh_test.go`
- Test: `desktop/shellintegration/pwsh_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/shellintegration/pwsh_test.go`:

```go
package shellintegration

import (
	"os"
	"strings"
	"testing"
)

func TestPreparePwshWritesScriptAndReturnsArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	p := preparePwsh("sess-pwsh-1")
	if p.Shell != "pwsh" {
		t.Fatalf("Plan.Shell = %q; want pwsh", p.Shell)
	}
	if p.Cleanup == nil {
		t.Fatalf("Plan.Cleanup is nil; pwsh prepare must register cleanup")
	}
	defer p.Cleanup()

	// Expect args: -NoExit -Command "& '<path>'"
	if len(p.ExtraArgs) != 4 {
		t.Fatalf("Plan.ExtraArgs has %d entries; want 4 (-NoExit -Command \"& '<path>'\"); got %v", len(p.ExtraArgs), p.ExtraArgs)
	}
	if p.ExtraArgs[0] != "-NoExit" || p.ExtraArgs[1] != "-Command" {
		t.Fatalf("Plan.ExtraArgs[0:2] = %v; want [-NoExit -Command]", p.ExtraArgs[0:2])
	}
	if !strings.HasPrefix(p.ExtraArgs[2], "& '") || !strings.HasSuffix(p.ExtraArgs[2], "'") {
		t.Fatalf("Plan.ExtraArgs[2] = %q; want \"& '<path>'\" form", p.ExtraArgs[2])
	}
	if p.ExtraArgs[3] != "-" {
		// We append a trailing '-' so PowerShell drops to the interactive
		// prompt after the script. (Without it, pwsh exits when -Command
		// completes despite -NoExit on some shells.)
		t.Fatalf("Plan.ExtraArgs[3] = %q; want '-'", p.ExtraArgs[3])
	}

	scriptPath := strings.TrimSuffix(strings.TrimPrefix(p.ExtraArgs[2], "& '"), "'")
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script %s: %v", scriptPath, err)
	}
	if !strings.Contains(string(body), "ATTERM_SHELL_INTEGRATION") {
		t.Fatalf("script body missing snippet markers; got %q", string(body))
	}

	p.Cleanup()
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Fatalf("Cleanup did not remove script %s: err=%v", scriptPath, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: FAIL.

- [ ] **Step 3: Write the implementation**

Replace `desktop/shellintegration/pwsh.go`:

```go
package shellintegration

import (
	"log"
	"os"
	"path/filepath"
)

func preparePwsh(sessionID string) Plan {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		log.Printf("shellintegration: pwsh cache dir unavailable: %v", err)
		return Plan{}
	}
	dir := filepath.Join(cacheDir, "atterm", "shell-integration")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("shellintegration: pwsh mkdir %s: %v", dir, err)
		return Plan{}
	}
	scriptPath := filepath.Join(dir, "atterm-"+sessionID+".ps1")
	if err := os.WriteFile(scriptPath, []byte(pwshSnippet), 0o600); err != nil {
		log.Printf("shellintegration: pwsh write %s: %v", scriptPath, err)
		return Plan{}
	}
	return Plan{
		Shell: "pwsh",
		ExtraArgs: []string{
			"-NoExit",
			"-Command",
			"& '" + scriptPath + "'",
			"-",
		},
		ExtraEnv: []string{"ATTERM_SHELL_INTEGRATION=1"},
		Cleanup: func() {
			if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
				log.Printf("shellintegration: pwsh cleanup %s: %v", scriptPath, err)
			}
		},
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./desktop/shellintegration/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/shellintegration/pwsh.go desktop/shellintegration/pwsh_test.go
git commit -m "feat: prepare pwsh OSC 133 init script and -NoExit -Command launcher"
```

---

### Task 8: Config Fields + `OrDefault` Methods

**Files:**
- Modify: `desktop/config.go`
- Create: `desktop/config_shell_integration_test.go`
- Test: `desktop/config_shell_integration_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/config_shell_integration_test.go`:

```go
package main

import "testing"

func TestShellIntegrationEnabledOrDefaultDefaultsTrue(t *testing.T) {
	cfg := appConfig{}
	if got := cfg.ShellIntegrationEnabledOrDefault(); got != true {
		t.Fatalf("ShellIntegrationEnabledOrDefault default = %v; want true", got)
	}
}

func TestShellIntegrationEnabledOrDefaultRoundTripsFalse(t *testing.T) {
	v := false
	cfg := appConfig{ShellIntegrationEnabled: &v}
	if got := cfg.ShellIntegrationEnabledOrDefault(); got != false {
		t.Fatalf("ShellIntegrationEnabledOrDefault(false) = %v; want false", got)
	}
}

func TestCommandNotifyThresholdSecondsOrDefaultDefaultsTo10(t *testing.T) {
	cfg := appConfig{}
	if got := cfg.CommandNotifyThresholdSecondsOrDefault(); got != 10 {
		t.Fatalf("CommandNotifyThresholdSecondsOrDefault default = %d; want 10", got)
	}
}

func TestCommandNotifyThresholdSecondsClampsToRange(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{-5, 1},
		{0, 1},
		{1, 1},
		{30, 30},
		{600, 600},
		{1200, 600},
	}
	for _, tc := range cases {
		v := tc.in
		cfg := appConfig{CommandNotifyThresholdSeconds: &v}
		if got := cfg.CommandNotifyThresholdSecondsOrDefault(); got != tc.want {
			t.Fatalf("CommandNotifyThresholdSecondsOrDefault(%d) = %d; want %d", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./desktop/ -run TestShellIntegration -count=1
go test ./desktop/ -run TestCommandNotifyThreshold -count=1
```

Expected: FAIL (`ShellIntegrationEnabled`, `ShellIntegrationEnabledOrDefault`, `CommandNotifyThresholdSeconds`, `CommandNotifyThresholdSecondsOrDefault` undefined).

- [ ] **Step 3: Write the implementation**

In `desktop/config.go`, find the `appConfig` struct (lines 28-59). Append these fields inside the struct (just before the closing `}`):

```go
	// ShellIntegrationEnabled controls whether atterm-spawned shells receive
	// OSC 133 hook injection at spawn time. Nil means "never set" and
	// defaults to true for existing installs. Only affects new sessions;
	// already-running PTYs keep their current behavior.
	ShellIntegrationEnabled *bool `json:"shell_integration_enabled,omitempty"`
	// CommandNotifyThresholdSeconds gates the command-finished notification:
	// commands shorter than this duration (start-to-finish) do not produce
	// a notification. Nil → default 10. Clamped to [1, 600] at read time.
	CommandNotifyThresholdSeconds *int `json:"command_notify_threshold_seconds,omitempty"`
```

Append these methods near the existing `OrDefault` helpers (after `LogFilePathOrDefault`):

```go
func (c appConfig) ShellIntegrationEnabledOrDefault() bool {
	if c.ShellIntegrationEnabled == nil {
		return true
	}
	return *c.ShellIntegrationEnabled
}

func (c appConfig) CommandNotifyThresholdSecondsOrDefault() int {
	const (
		minSec     = 1
		maxSec     = 600
		defaultSec = 10
	)
	if c.CommandNotifyThresholdSeconds == nil {
		return defaultSec
	}
	v := *c.CommandNotifyThresholdSeconds
	if v < minSec {
		return minSec
	}
	if v > maxSec {
		return maxSec
	}
	return v
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./desktop/ -run TestShellIntegration -count=1
go test ./desktop/ -run TestCommandNotifyThreshold -count=1
```

Expected: PASS for both.

- [ ] **Step 5: Commit**

```bash
git add desktop/config.go desktop/config_shell_integration_test.go
git commit -m "feat: add ShellIntegrationEnabled and CommandNotifyThresholdSeconds config fields"
```

---

### Task 9: Four New Wails Bindings on `App`

**Files:**
- Modify: `desktop/app.go`
- Create: `desktop/app_shell_integration_test.go`
- Test: `desktop/app_shell_integration_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/app_shell_integration_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func newAppWithTempCfg(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	// Ensure configPath() resolves under our temp dir.
	if err := os.MkdirAll(filepath.Join(dir, "atterm"), 0o755); err != nil {
		t.Fatalf("mkdir cfg: %v", err)
	}
	return &App{cfgStore: loadConfig()}
}

func TestGetShellIntegrationEnabledDefaultsTrue(t *testing.T) {
	a := newAppWithTempCfg(t)
	if got := a.GetShellIntegrationEnabled(); !got {
		t.Fatalf("GetShellIntegrationEnabled() = false; want true (default)")
	}
}

func TestSetShellIntegrationEnabledPersists(t *testing.T) {
	a := newAppWithTempCfg(t)
	if err := a.SetShellIntegrationEnabled(false); err != nil {
		t.Fatalf("SetShellIntegrationEnabled(false): %v", err)
	}
	if got := a.GetShellIntegrationEnabled(); got {
		t.Fatalf("after Set(false), Get() = true")
	}
}

func TestGetCommandNotifyThresholdSecondsDefaultsTo10(t *testing.T) {
	a := newAppWithTempCfg(t)
	if got := a.GetCommandNotifyThresholdSeconds(); got != 10 {
		t.Fatalf("GetCommandNotifyThresholdSeconds() = %d; want 10", got)
	}
}

func TestSetCommandNotifyThresholdSecondsPersistsAndClamps(t *testing.T) {
	a := newAppWithTempCfg(t)
	if err := a.SetCommandNotifyThresholdSeconds(45); err != nil {
		t.Fatalf("SetCommandNotifyThresholdSeconds(45): %v", err)
	}
	if got := a.GetCommandNotifyThresholdSeconds(); got != 45 {
		t.Fatalf("after Set(45), Get() = %d; want 45", got)
	}
	if err := a.SetCommandNotifyThresholdSeconds(9999); err != nil {
		t.Fatalf("SetCommandNotifyThresholdSeconds(9999): %v", err)
	}
	if got := a.GetCommandNotifyThresholdSeconds(); got != 600 {
		t.Fatalf("after Set(9999), Get() = %d; want 600 (clamped)", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./desktop/ -run TestGetShellIntegrationEnabled -count=1
go test ./desktop/ -run TestSetCommandNotifyThresholdSeconds -count=1
```

Expected: FAIL (methods undefined).

- [ ] **Step 3: Write the implementation**

In `desktop/app.go`, append four methods after `ShowNotification` (around line 568):

```go
// GetShellIntegrationEnabled returns the current persisted preference for
// OSC 133 shell-hook injection. Defaults to true for fresh installs.
func (a *App) GetShellIntegrationEnabled() bool {
	if a.cfgStore == nil {
		return true
	}
	return a.cfgStore.Get().ShellIntegrationEnabledOrDefault()
}

// SetShellIntegrationEnabled persists the user's toggle. Already-running
// sessions are unaffected; only newly spawned shells use the new value.
func (a *App) SetShellIntegrationEnabled(enabled bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.ShellIntegrationEnabled = &enabled
	return a.cfgStore.Set(cfg)
}

// GetCommandNotifyThresholdSeconds returns the current persisted command-
// finished notification threshold. Clamped to [1, 600] at read time;
// defaults to 10 for fresh installs.
func (a *App) GetCommandNotifyThresholdSeconds() int {
	if a.cfgStore == nil {
		return 10
	}
	return a.cfgStore.Get().CommandNotifyThresholdSecondsOrDefault()
}

// SetCommandNotifyThresholdSeconds persists the user's threshold. The
// stored value is clamped on read, so out-of-range writes (e.g. from a
// stale UI) are tolerated.
func (a *App) SetCommandNotifyThresholdSeconds(seconds int) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.CommandNotifyThresholdSeconds = &seconds
	return a.cfgStore.Set(cfg)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./desktop/ -run TestGetShellIntegrationEnabled -count=1
go test ./desktop/ -run TestSetCommandNotifyThresholdSeconds -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/app.go desktop/app_shell_integration_test.go
git commit -m "feat: add Wails bindings for shell integration and notify threshold"
```

---

### Task 10: Wire `shellintegration.Prepare` Into `NewSession`

**Files:**
- Modify: `desktop/relay_host.go`
- Create: `desktop/relay_host_shell_integration_test.go`
- Test: `desktop/relay_host_shell_integration_test.go`

This task is wiring-only: `NewSession` reads the config, calls `Prepare`, merges the Plan into the spawn args/env, and registers `Plan.Cleanup` on session close. The cfgStore reference is plumbed through `relayHost`.

- [ ] **Step 1: Add a small helper test for the merge logic**

Because `NewSession` does real I/O (PTY spawn), we test the merge as a small pure helper. Create `desktop/relay_host_shell_integration_test.go`:

```go
package main

import (
	"reflect"
	"sort"
	"testing"

	"github.com/attson/atterm/desktop/shellintegration"
)

func TestMergeShellIntegrationPlanAppliesEnvAndArgs(t *testing.T) {
	baseEnv := []string{"PATH=/bin", "TERM=xterm-256color"}
	baseArgv := []string{"/bin/zsh"}
	plan := shellintegration.Plan{
		ExtraEnv:  []string{"ZDOTDIR=/tmp/x", "ATTERM_SHELL_INTEGRATION=1"},
		ExtraArgs: []string{"--rcfile", "/tmp/r"},
	}
	gotArgv, gotEnv := mergeShellIntegrationPlan(baseArgv, baseEnv, plan)
	wantArgv := []string{"/bin/zsh", "--rcfile", "/tmp/r"}
	if !reflect.DeepEqual(gotArgv, wantArgv) {
		t.Fatalf("argv = %v; want %v", gotArgv, wantArgv)
	}
	sort.Strings(gotEnv)
	wantEnv := append([]string{}, baseEnv...)
	wantEnv = append(wantEnv, plan.ExtraEnv...)
	sort.Strings(wantEnv)
	if !reflect.DeepEqual(gotEnv, wantEnv) {
		t.Fatalf("env = %v; want %v", gotEnv, wantEnv)
	}
}

func TestMergeShellIntegrationPlanZeroPlanIsIdentity(t *testing.T) {
	baseEnv := []string{"PATH=/bin"}
	baseArgv := []string{"/bin/zsh"}
	gotArgv, gotEnv := mergeShellIntegrationPlan(baseArgv, baseEnv, shellintegration.Plan{})
	if !reflect.DeepEqual(gotArgv, baseArgv) {
		t.Fatalf("argv changed for zero plan: %v != %v", gotArgv, baseArgv)
	}
	if !reflect.DeepEqual(gotEnv, baseEnv) {
		t.Fatalf("env changed for zero plan: %v != %v", gotEnv, baseEnv)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./desktop/ -run TestMergeShellIntegrationPlan -count=1
```

Expected: FAIL (`mergeShellIntegrationPlan` undefined).

- [ ] **Step 3: Add the merge helper + thread cfgStore + wire into NewSession**

Open `desktop/relay_host.go`.

**(a)** Add this import to the existing import block:

```go
	"github.com/attson/atterm/desktop/shellintegration"
```

**(b)** Add a `cfg` field to `relayHost` (currently at lines 28-46):

```go
	cfg *configStore
```

Make sure the struct literal in `startRelayHost` no longer initializes it (caller will set it after creation).

**(c)** Add a setter method below `startRelayHost`:

```go
// setConfigStore wires the shared appConfig store after construction; the
// uplink and shell-integration logic both consult it. Safe to call exactly
// once at startup.
func (h *relayHost) setConfigStore(cfg *configStore) {
	h.cfg = cfg
}
```

**(d)** Add the merge helper as a free function at the bottom of `relay_host.go`:

```go
// mergeShellIntegrationPlan returns (argv', env') with the plan's args
// appended after argv[0] and its env appended after base. Zero plans are
// the identity transform.
func mergeShellIntegrationPlan(argv, env []string, p shellintegration.Plan) ([]string, []string) {
	if len(p.ExtraArgs) == 0 && len(p.ExtraEnv) == 0 {
		return argv, env
	}
	outArgv := append([]string{}, argv...)
	if len(p.ExtraArgs) > 0 {
		outArgv = append(outArgv, p.ExtraArgs...)
	}
	outEnv := append([]string{}, env...)
	if len(p.ExtraEnv) > 0 {
		outEnv = append(outEnv, p.ExtraEnv...)
	}
	return outArgv, outEnv
}
```

**(e)** In `NewSession` (currently around lines 278-349), replace the block:

```go
	argv := append([]string{req.Command}, req.Args...)
	pty, err := ptyhost.Open(ctx, ptyhost.Config{
		Argv: argv,
		Env:  terminalEnvForXterm(os.Environ()),
		Cwd:  cwd,
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("open pty: %w", err)
	}
```

with:

```go
	argv := append([]string{req.Command}, req.Args...)
	env := terminalEnvForXterm(os.Environ())

	enabled := true
	if h.cfg != nil {
		enabled = h.cfg.Get().ShellIntegrationEnabledOrDefault()
	}
	sid := uuid.New() // generated up here so the plan can scope temp files by id
	plan := shellintegration.Prepare(req.Command, enabled, sid.String())
	argv, env = mergeShellIntegrationPlan(argv, env, plan)
	if plan.Shell != "" {
		log.Printf("desktop-shell-integration: enabled session=%s shell=%s", sid, plan.Shell)
	}

	pty, err := ptyhost.Open(ctx, ptyhost.Config{
		Argv: argv,
		Env:  env,
		Cwd:  cwd,
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		if plan.Cleanup != nil {
			plan.Cleanup()
		}
		return uuid.Nil, fmt.Errorf("open pty: %w", err)
	}
```

Then change the lines that previously generated `id := uuid.New()` and call `cleanup` to reuse `sid` and to invoke `plan.Cleanup` after the existing per-session cleanup. Find:

```go
	id := uuid.New()
	info := proto.SessionInfo{
		Command:   strings.Join(argv, " "),
		Cwd:       cwd,
		Title:     req.Command,
		Cols:      cols,
		Rows:      rows,
		HostID:    h.hostID,
		Host:      h.host,
		User:      h.user,
		StartedAt: time.Now().Unix(),
	}

	cleanup := h.server.AdoptSession(ctx, id, info, &desktopPtyHost{Host: pty})

	h.mu.Lock()
	if h.sessions == nil {
		h.mu.Unlock()
		cleanup()
		_ = pty.Close()
		return uuid.Nil, fmt.Errorf("relay host stopped")
	}
	h.sessions[id] = &activeSession{host: pty, cleanup: cleanup}
	h.mu.Unlock()
	h.notifyChange()

	done := make(chan struct{})
	go h.watchCwd(id, pty, cwd, done)

	go func() {
		_ = pty.Wait()
		close(done)
		cleanup()
		_ = pty.Close()
		h.mu.Lock()
		delete(h.sessions, id)
		h.mu.Unlock()
		h.notifyChange()
	}()

	return id, nil
```

Replace with (no second `uuid.New()`; reuse `sid`; chain `plan.Cleanup` after relay cleanup):

```go
	id := sid
	info := proto.SessionInfo{
		Command:   strings.Join(argv, " "),
		Cwd:       cwd,
		Title:     req.Command,
		Cols:      cols,
		Rows:      rows,
		HostID:    h.hostID,
		Host:      h.host,
		User:      h.user,
		StartedAt: time.Now().Unix(),
	}

	cleanup := h.server.AdoptSession(ctx, id, info, &desktopPtyHost{Host: pty})

	combinedCleanup := func() {
		cleanup()
		if plan.Cleanup != nil {
			plan.Cleanup()
		}
	}

	h.mu.Lock()
	if h.sessions == nil {
		h.mu.Unlock()
		combinedCleanup()
		_ = pty.Close()
		return uuid.Nil, fmt.Errorf("relay host stopped")
	}
	h.sessions[id] = &activeSession{host: pty, cleanup: combinedCleanup}
	h.mu.Unlock()
	h.notifyChange()

	done := make(chan struct{})
	go h.watchCwd(id, pty, cwd, done)

	go func() {
		_ = pty.Wait()
		close(done)
		combinedCleanup()
		_ = pty.Close()
		h.mu.Lock()
		delete(h.sessions, id)
		h.mu.Unlock()
		h.notifyChange()
	}()

	return id, nil
```

**(f)** Wire `cfgStore` into `relayHost` at app startup. Locate the spot in `desktop/app.go` where `relayHost` is created (search for `startRelayHost()`). Immediately after the successful return, call `a.relayHost.setConfigStore(a.cfgStore)`. If the wiring site is `app.go:startup`, ensure the cfgStore is loaded first (it already is).

Run:
```bash
grep -n "startRelayHost\|relayHost\|cfgStore" /Users/attson/code/github.com.attson/atterm/desktop/app.go | head -20
```

Expected: shows where to insert `setConfigStore`. Insert one line after the assignment.

- [ ] **Step 4: Run all desktop Go tests**

Run:
```bash
go test ./desktop/... -count=1
```

Expected: PASS including the new `TestMergeShellIntegrationPlan*` tests. Other tests are unaffected.

- [ ] **Step 5: Commit**

```bash
git add desktop/relay_host.go desktop/relay_host_shell_integration_test.go desktop/app.go
git commit -m "feat: wire shell integration into desktop PTY spawn path"
```

---

### Task 11: `commandFinish.ts` — Tracker + Gate + Format

**Files:**
- Create: `desktop/frontend/src/lib/commandFinish.ts`
- Create: `desktop/frontend/src/lib/commandFinish.test.ts`
- Test: `desktop/frontend/src/lib/commandFinish.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/lib/commandFinish.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import {
  CommandTracker,
  formatElapsed,
  shouldNotifyCommand,
} from "./commandFinish";

describe("CommandTracker", () => {
  test("A and B do not emit events", () => {
    const t = new CommandTracker();
    expect(t.onOsc133("A", 1000)).toBeNull();
    expect(t.onOsc133("B", 1010)).toBeNull();
  });

  test("D without preceding C is orphan and ignored", () => {
    const t = new CommandTracker();
    expect(t.onOsc133("D;0", 1000)).toBeNull();
  });

  test("C then D emits finished event with elapsedMs and exitCode", () => {
    const t = new CommandTracker();
    t.onOsc133("C", 1000);
    const ev = t.onOsc133("D;0", 12500);
    expect(ev).not.toBeNull();
    expect(ev!.kind).toBe("finished");
    expect(ev!.exitCode).toBe(0);
    expect(ev!.elapsedMs).toBe(11500);
  });

  test("bare D treats exit as 0", () => {
    const t = new CommandTracker();
    t.onOsc133("C", 0);
    const ev = t.onOsc133("D", 5000);
    expect(ev!.exitCode).toBe(0);
  });

  test("non-numeric exit code becomes -1", () => {
    const t = new CommandTracker();
    t.onOsc133("C", 0);
    const ev = t.onOsc133("D;abc", 5000);
    expect(ev!.exitCode).toBe(-1);
  });

  test("two consecutive C overwrite", () => {
    const t = new CommandTracker();
    t.onOsc133("C", 1000);
    t.onOsc133("C", 5000);
    const ev = t.onOsc133("D;1", 7500);
    expect(ev!.elapsedMs).toBe(2500);
  });

  test("after emitting, D cannot fire again without new C", () => {
    const t = new CommandTracker();
    t.onOsc133("C", 1000);
    expect(t.onOsc133("D;0", 2000)).not.toBeNull();
    expect(t.onOsc133("D;0", 3000)).toBeNull();
  });
});

describe("shouldNotifyCommand", () => {
  const ev = { kind: "finished" as const, exitCode: 0, elapsedMs: 15000 };

  test("focused window suppresses notification", () => {
    expect(
      shouldNotifyCommand(ev, { focused: true, thresholdSec: 10, isLocal: true }),
    ).toBe(false);
  });

  test("non-local session suppresses notification", () => {
    expect(
      shouldNotifyCommand(ev, { focused: false, thresholdSec: 10, isLocal: false }),
    ).toBe(false);
  });

  test("below threshold suppresses notification", () => {
    expect(
      shouldNotifyCommand(
        { ...ev, elapsedMs: 5000 },
        { focused: false, thresholdSec: 10, isLocal: true },
      ),
    ).toBe(false);
  });

  test("unfocused, local, >=threshold passes", () => {
    expect(
      shouldNotifyCommand(ev, { focused: false, thresholdSec: 10, isLocal: true }),
    ).toBe(true);
  });

  test("threshold of 0 is clamped to 1 (still passes 15s)", () => {
    expect(
      shouldNotifyCommand(ev, { focused: false, thresholdSec: 0, isLocal: true }),
    ).toBe(true);
  });
});

describe("formatElapsed", () => {
  test.each([
    [0, "0s"],
    [999, "0s"],
    [1000, "1s"],
    [12500, "12s"],
    [59999, "59s"],
    [60000, "1m0s"],
    [125000, "2m5s"],
    [3599000, "59m59s"],
  ])("formats %dms as %s", (ms, want) => {
    expect(formatElapsed(ms)).toBe(want);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
cd desktop/frontend
npm run test -- src/lib/commandFinish.test.ts
```

Expected: FAIL — file does not exist.

- [ ] **Step 3: Write the implementation**

Create `desktop/frontend/src/lib/commandFinish.ts`:

```ts
/**
 * commandFinish — pure helpers for OSC 133 command boundary tracking and
 * command-finished notification gating. Consumed by TerminalView via
 * xterm's parser.registerOscHandler(133, …).
 *
 * The xterm parser strips the leading "OSC 133;" and the terminator (BEL or
 * ST), so handlers see payloads like "A", "B", "C", "D", or "D;<exit>".
 */

export interface CommandEvent {
  kind: "finished";
  exitCode: number;
  elapsedMs: number;
}

type State =
  | { phase: "idle" }
  | { phase: "running"; startedAt: number };

export class CommandTracker {
  private state: State = { phase: "idle" };

  /**
   * Update tracker state from a single OSC 133 payload. Returns a
   * CommandEvent when the payload signals "command finished" and a prior C
   * was seen; otherwise returns null.
   */
  onOsc133(payload: string, nowMs: number): CommandEvent | null {
    const marker = payload.charAt(0);
    switch (marker) {
      case "A":
      case "B":
        return null;
      case "C":
        this.state = { phase: "running", startedAt: nowMs };
        return null;
      case "D": {
        if (this.state.phase !== "running") return null;
        const elapsedMs = Math.max(0, nowMs - this.state.startedAt);
        const exitCode = parseExitCode(payload);
        this.state = { phase: "idle" };
        return { kind: "finished", exitCode, elapsedMs };
      }
      default:
        return null;
    }
  }
}

function parseExitCode(payload: string): number {
  // payload examples: "D", "D;0", "D;127", "D;abc"
  const idx = payload.indexOf(";");
  if (idx === -1) return 0;
  const raw = payload.slice(idx + 1).trim();
  if (raw === "") return 0;
  const n = Number(raw);
  return Number.isFinite(n) && Number.isInteger(n) ? n : -1;
}

export interface NotifyGate {
  focused: boolean;
  thresholdSec: number;
  isLocal: boolean;
}

/**
 * Returns true when an unfocused, local-session command finish meets the
 * threshold. Threshold below 1s is clamped to 1s.
 */
export function shouldNotifyCommand(ev: CommandEvent, opts: NotifyGate): boolean {
  if (!opts.isLocal) return false;
  if (opts.focused) return false;
  const thresholdMs = Math.max(1, opts.thresholdSec) * 1000;
  return ev.elapsedMs >= thresholdMs;
}

/**
 * "12s" for sub-minute durations, "MmSs" otherwise. Always integer seconds.
 */
export function formatElapsed(ms: number): string {
  const totalSec = Math.floor(ms / 1000);
  if (totalSec < 60) return `${totalSec}s`;
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return `${m}m${s}s`;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
cd desktop/frontend
npm run test -- src/lib/commandFinish.test.ts
```

Expected: all suites PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/commandFinish.ts desktop/frontend/src/lib/commandFinish.test.ts
git commit -m "feat: add CommandTracker and command-finish notification gate"
```

---

### Task 12: `api.ts` — Four New TS Wrappers

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`

This change is purely shape — the source-level checks come for free with TerminalView / SettingsGeneral tests later. We still verify the build compiles.

- [ ] **Step 1: Extend `AppBindings` and add wrappers**

Open `desktop/frontend/src/lib/api.ts`. Inside the `AppBindings` interface (currently lines 78-103), append after `ShowNotification`:

```ts
  GetShellIntegrationEnabled(): Promise<boolean>;
  SetShellIntegrationEnabled(enabled: boolean): Promise<void>;
  GetCommandNotifyThresholdSeconds(): Promise<number>;
  SetCommandNotifyThresholdSeconds(seconds: number): Promise<void>;
```

At the bottom of the file, after `showNotification`, append:

```ts
export function getShellIntegrationEnabled(): Promise<boolean> {
  return bindings().GetShellIntegrationEnabled();
}

export function setShellIntegrationEnabled(enabled: boolean): Promise<void> {
  return bindings().SetShellIntegrationEnabled(enabled);
}

export function getCommandNotifyThresholdSeconds(): Promise<number> {
  return bindings().GetCommandNotifyThresholdSeconds();
}

export function setCommandNotifyThresholdSeconds(seconds: number): Promise<void> {
  return bindings().SetCommandNotifyThresholdSeconds(seconds);
}
```

- [ ] **Step 2: Run the build to verify there are no type errors**

Run:
```bash
cd desktop/frontend
npm run build
```

Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/lib/api.ts
git commit -m "feat: expose shell integration and notify threshold via TS api"
```

---

### Task 13: `TerminalView.vue` — OSC 133 Handler + Threshold Prop + Local Guard

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Modify: `desktop/frontend/src/components/TerminalView.test.ts`
- Test: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 1: Add the source-level test assertions**

Open `desktop/frontend/src/components/TerminalView.test.ts` (existing file).

Append these tests inside the existing `describe("TerminalView")` block:

```ts
  test("imports CommandTracker and shouldNotifyCommand from commandFinish", () => {
    expect(source).toContain("CommandTracker");
    expect(source).toContain("shouldNotifyCommand");
    expect(source).toContain('from "../lib/commandFinish"');
  });

  test("declares commandNotifyThresholdSec and isLocalSession props", () => {
    expect(source).toContain("commandNotifyThresholdSec");
    expect(source).toContain("isLocalSession");
  });

  test("registers OSC 133 handler on the xterm parser", () => {
    expect(source).toMatch(/term\.parser\.registerOscHandler\(\s*133\s*,/);
  });

  test("invokes showNotification with a Command-finished body when gate passes", () => {
    expect(source).toContain("Command finished");
    // Notification is fired via the existing showNotification wrapper.
    expect(source).toMatch(/showNotification\(/);
  });

  test("notification is gated by shouldNotifyCommand with focused, isLocal, threshold", () => {
    expect(source).toMatch(/shouldNotifyCommand\([\s\S]*focused[\s\S]*thresholdSec[\s\S]*isLocal/);
  });
```

If `TerminalView.test.ts` does not already import `source`, search for how existing test files do source-level checks. The existing pattern in this repo (see e.g. `SettingsGeneral.test.ts`) is:

```ts
import source from "./TerminalView.vue?raw";
```

Ensure it's present.

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
cd desktop/frontend
npm run test -- src/components/TerminalView.test.ts
```

Expected: new tests FAIL.

- [ ] **Step 3: Implement in TerminalView.vue**

Open `desktop/frontend/src/components/TerminalView.vue`.

**(a)** Update the imports section (currently lines 1-14). Add to the existing `terminalBell` import line, and add a new line for `commandFinish`:

```ts
import { shouldNotify } from "../lib/terminalBell";
import {
  CommandTracker,
  shouldNotifyCommand,
  formatElapsed,
} from "../lib/commandFinish";
```

**(b)** Extend the `defineProps` block (currently lines 16-36). Add two new props inside the `defineProps<{ … }>()` generic:

```ts
    commandNotifyThresholdSec?: number;
    isLocalSession?: boolean;
```

And in the `withDefaults` defaults object at the end of `defineProps`:

```ts
{ active: true, focused: false, avoidTopRightBadge: false, commandNotifyThresholdSec: 10, isLocalSession: true }
```

**(c)** In `ensureTerm()` (currently around line 231) — after the existing `term.onBell` block (around lines 267-273) and before the `resizeObserver` line — append:

```ts
  const cmdTracker = new CommandTracker();
  try {
    term.parser.registerOscHandler(133, (payload) => {
      const ev = cmdTracker.onOsc133(payload, Date.now());
      if (!ev) return false;
      const focused = typeof document !== "undefined" && document.hasFocus();
      const passed = shouldNotifyCommand(ev, {
        focused,
        thresholdSec: props.commandNotifyThresholdSec ?? 10,
        isLocal: props.isLocalSession ?? true,
      });
      if (!passed) return false;
      void showNotification(
        "AT Term",
        `Command finished · exit ${ev.exitCode} · ${formatElapsed(ev.elapsedMs)} · ${props.sessionLabel || "session"}`,
      );
      return false;
    });
  } catch (err) {
    console.warn("[AT Term] OSC 133 handler registration failed", err);
  }
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
cd desktop/frontend
npm run test -- src/components/TerminalView.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue \
        desktop/frontend/src/components/TerminalView.test.ts
git commit -m "feat: wire OSC 133 command-finished notification in TerminalView"
```

---

### Task 14: `PaneGrid.vue` — Forward `isLocalSession` And `commandNotifyThresholdSec`

**Files:**
- Modify: `desktop/frontend/src/components/PaneGrid.vue`

- [ ] **Step 1: Add a new prop and forward to TerminalView**

Open `desktop/frontend/src/components/PaneGrid.vue`.

**(a)** Add to `defineProps` (currently lines 10-16):

```ts
  commandNotifyThresholdSec: number;
```

**(b)** Add the props on `<TerminalView>` (currently around lines 55-67):

```vue
        :is-local-session="!pane.remote"
        :command-notify-threshold-sec="commandNotifyThresholdSec"
```

So the `<TerminalView ... />` block becomes:

```vue
      <TerminalView
        v-if="pane.sessionId && endpointFor(pane)"
        :endpoint="endpointFor(pane)!"
        :session-id="pane.sessionId"
        :active="active"
        :focused="active && idx === tab.activePaneIdx"
        :expected-cols="sessionInfoFor(pane)?.cols"
        :expected-rows="sessionInfoFor(pane)?.rows"
        :remote-permission="sessionInfoFor(pane)?.remote_permission"
        :session-label="extractSessionLabel(sessionInfoFor(pane))"
        :avoid-top-right-badge="pane.remote"
        :theme="terminalTheme"
        :is-local-session="!pane.remote"
        :command-notify-threshold-sec="commandNotifyThresholdSec"
        @toast="emit('toast', $event)"
      />
```

- [ ] **Step 2: Verify build**

Run:
```bash
cd desktop/frontend
npm run build
```

Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/components/PaneGrid.vue
git commit -m "feat: forward isLocalSession and commandNotifyThresholdSec to TerminalView"
```

---

### Task 15: `App.vue` — Load Threshold On Mount, Plumb Down

**Files:**
- Modify: `desktop/frontend/src/App.vue`

- [ ] **Step 1: Find the relevant section**

Run:
```bash
grep -n "PaneGrid\|terminalTheme\|onMounted\|getTerminalThemePreference" /Users/attson/code/github.com.attson/atterm/desktop/frontend/src/App.vue | head -30
```

Expected: shows where theme is loaded and where `<PaneGrid>` is rendered.

- [ ] **Step 2: Add load + prop**

In `App.vue`:

**(a)** Add to imports from `./lib/api`:

```ts
import { getCommandNotifyThresholdSeconds } from "./lib/api";
```

**(b)** Add a reactive ref alongside the existing `terminalThemeId`:

```ts
const commandNotifyThresholdSec = ref(10);
```

**(c)** In the existing `onMounted` block (or wherever `getTerminalThemePreference()` is awaited), add a sibling call:

```ts
try {
  commandNotifyThresholdSec.value = await getCommandNotifyThresholdSeconds();
} catch (e) {
  console.warn("[AT Term] failed to load command-notify threshold", e);
}
```

**(d)** Pass to `<PaneGrid>`:

```vue
<PaneGrid
  ...
  :command-notify-threshold-sec="commandNotifyThresholdSec"
  ...
/>
```

- [ ] **Step 3: Verify build**

Run:
```bash
cd desktop/frontend
npm run build
```

Expected: build succeeds.

- [ ] **Step 4: Update SettingsGeneral handler to refresh App.vue's ref**

In `App.vue`, find the handler that receives setting changes from `SettingsDialog`. Add a sibling event `command-notify-threshold-changed: (seconds: number) => void` and handle it by writing to `commandNotifyThresholdSec.value = seconds`. The same listener should also fire when SettingsGeneral emits its new event (see Task 16).

If `SettingsDialog` does not currently forward arbitrary events from its children, follow the pattern already in place for `terminal-theme-changed`:

```bash
grep -n "terminal-theme-changed" /Users/attson/code/github.com.attson/atterm/desktop/frontend/src/components/SettingsDialog.vue /Users/attson/code/github.com.attson/atterm/desktop/frontend/src/App.vue
```

Mirror that pattern: define an emit on `SettingsGeneral`, re-emit through `SettingsDialog`, listen in `App.vue`, mutate the ref.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/components/SettingsDialog.vue
git commit -m "feat: plumb command-notify threshold from settings to all panes"
```

---

### Task 16: `SettingsGeneral.vue` — UI Controls

**Files:**
- Modify: `desktop/frontend/src/components/SettingsGeneral.vue`
- Modify: `desktop/frontend/src/components/SettingsGeneral.test.ts`
- Test: `desktop/frontend/src/components/SettingsGeneral.test.ts`

- [ ] **Step 1: Add source-level test assertions**

Open `desktop/frontend/src/components/SettingsGeneral.test.ts`.

Append to the existing describe block (use the same `import source from "./SettingsGeneral.vue?raw"` pattern already in the file):

```ts
  test("renders shell integration toggle wired to setShellIntegrationEnabled", () => {
    expect(source).toContain("Enable shell integration");
    expect(source).toMatch(/setShellIntegrationEnabled\(/);
  });

  test("renders command-notify threshold number input wired to setCommandNotifyThresholdSeconds", () => {
    expect(source).toContain("Command-finished notification threshold");
    expect(source).toMatch(/setCommandNotifyThresholdSeconds\(/);
    expect(source).toContain('min="1"');
    expect(source).toContain('max="600"');
  });

  test("loads shell integration and threshold on mount", () => {
    expect(source).toMatch(/getShellIntegrationEnabled\(\)/);
    expect(source).toMatch(/getCommandNotifyThresholdSeconds\(\)/);
  });

  test("emits command-notify-threshold-changed when threshold saves", () => {
    expect(source).toContain('"command-notify-threshold-changed"');
  });
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
cd desktop/frontend
npm run test -- src/components/SettingsGeneral.test.ts
```

Expected: new tests FAIL.

- [ ] **Step 3: Implement in SettingsGeneral.vue**

Open `desktop/frontend/src/components/SettingsGeneral.vue`.

**(a)** Update imports:

```ts
import {
  getCommandNotifyThresholdSeconds,
  getNotificationsEnabled,
  getShellIntegrationEnabled,
  setCommandNotifyThresholdSeconds,
  setNotificationsEnabled,
  setShellIntegrationEnabled,
  setTerminalThemePreference,
} from "../lib/api";
```

**(b)** Extend the emit signature:

```ts
const emit = defineEmits<{
  (e: "terminal-theme-changed", themeID: string): void;
  (e: "command-notify-threshold-changed", seconds: number): void;
}>();
```

**(c)** Add reactive state:

```ts
const shellIntegrationEnabled = ref(true);
const shellIntegrationLoading = ref(true);
const commandNotifyThresholdSec = ref(10);
const commandNotifyThresholdLoading = ref(true);
```

**(d)** Extend the `onMounted` block:

```ts
onMounted(async () => {
  try {
    notificationsEnabled.value = await getNotificationsEnabled();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    notificationsLoading.value = false;
  }
  try {
    shellIntegrationEnabled.value = await getShellIntegrationEnabled();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    shellIntegrationLoading.value = false;
  }
  try {
    commandNotifyThresholdSec.value = await getCommandNotifyThresholdSeconds();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    commandNotifyThresholdLoading.value = false;
  }
});
```

**(e)** Add handlers:

```ts
async function onShellIntegrationToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = shellIntegrationEnabled.value;
  shellIntegrationEnabled.value = target.checked;
  error.value = "";
  try {
    await setShellIntegrationEnabled(target.checked);
  } catch (e: any) {
    shellIntegrationEnabled.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onCommandNotifyThresholdChange(e: Event) {
  const target = e.target as HTMLInputElement;
  const raw = Number(target.value);
  const next = Number.isFinite(raw) ? Math.max(1, Math.min(600, Math.round(raw))) : 10;
  const previous = commandNotifyThresholdSec.value;
  commandNotifyThresholdSec.value = next;
  target.value = String(next);
  error.value = "";
  try {
    await setCommandNotifyThresholdSeconds(next);
    emit("command-notify-threshold-changed", next);
  } catch (e: any) {
    commandNotifyThresholdSec.value = previous;
    target.value = String(previous);
    error.value = e?.message ?? String(e);
  }
}
```

**(f)** Extend the template — add inside the `<div class="tab-pane">` block, after the existing notifications checkbox:

```vue
    <label class="checkbox" v-if="!shellIntegrationLoading">
      <input
        type="checkbox"
        :checked="shellIntegrationEnabled"
        @change="onShellIntegrationToggle"
      />
      Enable shell integration
    </label>
    <p class="hint" v-if="!shellIntegrationLoading">
      Injects OSC 133 hooks into zsh / bash / fish / pwsh at session start so we can
      detect when a command finishes. Only affects new sessions.
    </p>

    <label class="field-label" v-if="!commandNotifyThresholdLoading">
      Command-finished notification threshold (seconds)
    </label>
    <input
      v-if="!commandNotifyThresholdLoading"
      class="number-input"
      type="number"
      min="1"
      max="600"
      :value="commandNotifyThresholdSec"
      @change="onCommandNotifyThresholdChange"
    />
    <p class="hint" v-if="!commandNotifyThresholdLoading">
      Commands shorter than this duration do not produce a notification. Requires
      shell integration to be enabled.
    </p>
```

**(g)** Append minimal CSS for `.number-input` (under the existing `<style scoped>` block):

```css
.number-input {
  width: 80px;
  padding: 4px 8px;
  background: var(--input-bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--fg);
  font: inherit;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
cd desktop/frontend
npm run test -- src/components/SettingsGeneral.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SettingsGeneral.vue \
        desktop/frontend/src/components/SettingsGeneral.test.ts
git commit -m "feat: add shell integration toggle and notify threshold to settings"
```

---

### Task 17: `docs/shell-integration.md` — User Documentation

**Files:**
- Create: `docs/shell-integration.md`

- [ ] **Step 1: Write the doc**

Create `docs/shell-integration.md`:

```markdown
# Shell Integration

AT Term auto-injects [OSC 133](https://gitlab.freedesktop.org/Per_Bothner/specifications/-/blob/master/proposals/semantic-prompts.md) command-boundary hooks into shells it spawns, so commands that take a while produce a system notification when they finish (only if the AT Term window is unfocused).

## What gets injected

Each time AT Term spawns a shell, we add a small wrapper that loads our snippet **after** your normal rc:

| Shell | Mechanism | User config touched |
|-------|-----------|---------------------|
| zsh | `$ZDOTDIR` set to a temp dir whose `.zshrc` sources your original rc, then ours | none |
| bash | `--rcfile <tmp>` passed at launch; tmp sources `~/.bashrc`, then ours | none |
| fish | `$XDG_CONFIG_HOME/fish/conf.d/atterm-integration.fish` (or `~/.config/...`) | shared file in `conf.d/` |
| PowerShell | `-NoExit -Command "& '<tmp>'"` | none (your `$PROFILE` is untouched) |

For zsh / bash / pwsh, the temp files are deleted when the session closes. The fish file persists across sessions (and across AT Term uninstalls) because fish auto-loads everything in `conf.d/`. Delete it manually if you no longer want it: `rm $XDG_CONFIG_HOME/fish/conf.d/atterm-integration.fish`.

## How to disable

Settings → General → "Enable shell integration". Disabling affects newly spawned sessions; existing sessions keep their current hooks until they exit.

## Configuration

| Setting | Default | Notes |
|---------|---------|-------|
| Enable shell integration | on | Master switch |
| Command-finished notification threshold (seconds) | 10 | Only commands lasting at least this long trigger a notification. Min 1, max 600. |

Notifications also require the AT Term window to be unfocused and the session to be local (a session you started, not one you cast-attached to from another machine).

## Manual install (unsupported shells)

cmd.exe, nu, xonsh, elvish and friends are not auto-injected. If you want OSC 133 markers in those shells, source the relevant snippet manually. The snippet contents are available in the AT Term source tree under `desktop/shellintegration/snippets/`.

## Frameworks (oh-my-zsh, powerlevel10k, starship, oh-my-posh)

Our snippets never touch your `PS1` directly; they use the shell's additive hook arrays (`precmd_functions`, `preexec_functions`, `PROMPT_COMMAND`, fish event hooks, PowerShell `prompt` function wrapping). Frameworks should keep working unchanged. If your prompt looks broken after enabling integration, please file a bug with the framework name and AT Term version.

## Privacy

OSC 133 markers carry only command-boundary metadata (start, end, exit code). The command text itself stays in your shell history and the PTY output stream — AT Term does not log it separately.
```

- [ ] **Step 2: Commit**

```bash
git add docs/shell-integration.md
git commit -m "docs: add user guide for OSC 133 shell integration"
```

---

### Task 18: `README.md` — Link to Shell Integration Doc

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Locate the right section**

Run:
```bash
grep -n "shell\|Shell\|集成\|文档" /Users/attson/code/github.com.attson/atterm/README.md | head -20
```

Expected: shows existing capability table and Documentation section.

- [ ] **Step 2: Edit README.md**

**(a)** In the "现在能做什么" capability table (around lines 15-24), add a row:

```markdown
| Shell 集成（OSC 133） | macOS / Linux 自动注入 zsh / bash / fish hook；Windows 自动注入 PowerShell；命令完成 ≥10s 且窗口未聚焦时发系统通知 |
```

**(b)** In the "文档" section, add a bullet:

```markdown
- [`docs/shell-integration.md`](docs/shell-integration.md)：OSC 133 shell 集成机制、各 shell 的注入方式、如何手动卸载。
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: link shell integration page from README"
```

---

### Task 19: Full Test Sweep + Manual Smoke Verification

This task gates merge — no new code beyond what previous tasks produced. It exists so the executor explicitly runs the whole battery and the manual smoke list before declaring the feature done.

**Files:**
- No code changes.

- [ ] **Step 1: Run full Go test suite**

Run:
```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./...
```

Expected: all PASS, no `go vet` issues.

- [ ] **Step 2: Run full frontend test + build**

Run:
```bash
cd desktop/frontend
npm ci
npm run test -- --run
npm run build
```

Expected: all vitest suites PASS, build succeeds.

- [ ] **Step 3: Run web vanilla tests (if any new dependency surfaced)**

Run:
```bash
node --test web/*.test.mjs
```

Expected: PASS or no test files matched (unchanged from baseline).

- [ ] **Step 4: Manual smoke on the running app**

Build and run the desktop app once:

```bash
cd desktop
wails dev   # macOS / Windows
# or: wails dev -tags webkit2_41  # Linux
```

Run each item below in sequence. Each one must pass before moving on.

  - [ ] **4.1 macOS / zsh** — Open a new tab. Run `sleep 12; ls`. Cmd-Tab to another app. Within ~13s, a notification "AT Term — Command finished · exit 0 · 12s · <label>" arrives.
  - [ ] **4.2 macOS / bash** — Quit, set `SHELL=/bin/bash` in your environment, relaunch app. Repeat 4.1.
  - [ ] **4.3 Linux / fish** — On a Linux box (or VM), with `SHELL=/usr/bin/fish`, repeat 4.1.
  - [ ] **4.4 Windows / pwsh** — On Windows, set the spawned shell to `pwsh.exe`, repeat 4.1.
  - [ ] **4.5 Disabled toggle** — Settings → General → uncheck "Enable shell integration". Open a new session, run `sleep 12; ls`, blur the window. No notification arrives.
  - [ ] **4.6 Threshold 60** — Settings → General → threshold = 60. New session: `sleep 30; ls` blur → no notification. `sleep 70; ls` blur → notification.
  - [ ] **4.7 Focused** — With shell integration on and threshold 10, run `sleep 12; ls` keeping the AT Term window focused. No notification fires.
  - [ ] **4.8 Framework compat** — On a machine with oh-my-zsh + powerlevel10k installed for the user, open a new session. Prompt renders normally. Run a 15s command, blur, get a notification.
  - [ ] **4.9 Remote attach** — Connect a second desktop to the same relay and cast-attach this session. The cast-attached pane on the other desktop must NOT fire notifications (its `isLocalSession` is false).

- [ ] **Step 5: Final commit (only if any cleanup needed)**

Most likely no commit is needed at this step. If smoke surfaced any inline fix (typo, log line removal), commit it now with a `fix:` prefix.

```bash
git status
# If clean, you're done.
```

---

## Plan Self-Review

**Spec coverage check.** Every section of the spec has at least one task:

| Spec section | Tasks |
|--------------|-------|
| `Plan` struct + `Prepare()` | 2 |
| Detect shell | 1 |
| zsh strategy | 4 |
| bash strategy | 5 |
| fish strategy | 6 |
| pwsh strategy | 7 |
| Embedded snippets + guard variables | 3 |
| `appConfig` two fields | 8 |
| Four Wails bindings | 9 |
| `relay_host.go` wiring + cleanup chain | 10 |
| `commandFinish.ts` Tracker + helpers | 11 |
| `api.ts` wrappers | 12 |
| `TerminalView.vue` handler + props | 13 |
| `PaneGrid.vue` prop forwarding | 14 |
| `App.vue` load + plumb | 15 |
| `SettingsGeneral.vue` UI | 16 |
| `docs/shell-integration.md` | 17 |
| README link | 18 |
| Full test + manual smoke (incl. 8 items in spec's Verification list) | 19 |

**Placeholder scan.** No "TBD" / "TODO" / "implement later" / "similar to" / "handle errors" without specifics.

**Type consistency.**
- `Plan` struct shape (Shell / ExtraEnv / ExtraArgs / Cleanup) consistent across tasks 2, 4-7, 10.
- `Shell` enum names (ShellZsh / ShellBash / ShellFish / ShellPwsh / ShellUnknown) consistent across tasks 1, 2.
- `Prepare(shellPath string, enabled bool, sessionID string) Plan` signature consistent in tasks 2, 6 (fish is the only one without `sessionID` because `prepareFish()` writes to a shared `conf.d/`).
- `CommandEvent.kind` literal `"finished"` consistent in tasks 11, 13.
- `shouldNotifyCommand` parameter shape `{ focused, thresholdSec, isLocal }` consistent in tasks 11, 13.
- Config field names (`ShellIntegrationEnabled`, `CommandNotifyThresholdSeconds`) and JSON tags consistent in tasks 8, 9.
- TS wrapper names (`getShellIntegrationEnabled` / `setShellIntegrationEnabled` / `getCommandNotifyThresholdSeconds` / `setCommandNotifyThresholdSeconds`) consistent in tasks 12, 15, 16.

No gaps found.
