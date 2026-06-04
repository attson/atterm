# Session type classification — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tag each relay session with `type ∈ {shell, ai, test, build, deploy}` derived server-side from the OSC 133 command line; propagate via the existing `SessionInfo` channel to all three frontends; render as a small colored chip / icon on tab bars and session lists.

**Architecture:** A pure `internal/session/classify.go` holds the rules. `applyOSC133Locked` calls `ClassifyCommand` on every `C` event and updates `s.meta.Type` only when the result is non-shell (sticky). The new `Type` field rides on `proto.SessionInfo` so the existing META broadcast + `/api/sessions` JSON carry it without protocol changes. A shared `lib/sessionType.ts` maps `type` → `{label, color, iconPath}` and is consumed by TabBar (desktop), MobileSessionList (mobile), SessionList (web).

**Tech Stack:** Go stdlib (regex, strings) — no new deps. TypeScript + Vue 3 — no new deps. vitest for frontend, `go test` for backend.

**Reference spec:** `docs/superpowers/specs/2026-06-04-session-type-classification-design.md`

---

## File map

### Backend (Go)
- **Create:** `internal/session/classify.go` — `SessionType*` constants + `ClassifyCommand(cmd) string`.
- **Create:** `internal/session/classify_test.go` — table-driven, ~22 cases.
- **Modify:** `internal/proto/frame.go` — add `Type string \`json:"type,omitempty"\`` to `SessionInfo`.
- **Modify:** `internal/session/session.go` — initial `meta.Type = "shell"` in `New`; call `ClassifyCommand` in `applyOSC133Locked`'s `C` branch, sticky update.
- **Modify:** `internal/session/session_test.go` — three new tests for sticky behavior.

### Frontend types (read by all three renderers)
- **Modify:** `desktop/frontend/src/lib/connection.ts` — `SessionInfo.type?: string`.
- **Modify:** `desktop/frontend/src/platform/types.ts` — `RemoteSession.type?: string`.
- **Modify:** `web/src/shared/api/types.ts` — `SessionInfo.type?: string` (web has its own copy).

### Frontend helper (shared between desktop + mobile)
- **Create:** `desktop/frontend/src/lib/sessionType.ts` — `displayForType(t)` → `{ key, color, iconPath } | null`.
- **Create:** `desktop/frontend/src/lib/__tests__/sessionType.test.ts`.

### Frontend helper (web — parallel copy because web doesn't import from desktop/frontend)
- **Create:** `web/src/shared/sessionType.ts` — identical shape.
- **Create:** `web/src/shared/__tests__/sessionType.test.ts` (if web has vitest; otherwise skip and lean on desktop tests).

### Renderers
- **Modify:** `desktop/frontend/src/components/TabBar.vue` — type icon next to `.title`.
- **Modify:** `desktop/frontend/src/components/TabBar.test.ts` — new case asserts the icon.
- **Modify:** `desktop/frontend/src/mobile/MobileSessionList.vue` — type chip in the card title row.
- **Modify:** `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts` — chip-text assertion.
- **Modify:** `web/src/main/components/SessionList.vue` — type chip mirroring mobile.

### i18n
- **Modify:** `desktop/frontend/src/i18n/messages/en.ts` — 4 keys under `mobile.taskTypes`.
- **Modify:** `desktop/frontend/src/i18n/messages/zh-CN.ts` — same 4 keys.

---

## Task 1: `ClassifyCommand` helper (test-first)

**Files:**
- Create: `internal/session/classify.go`
- Create: `internal/session/classify_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/session/classify_test.go`:

```go
package session

import "testing"

func TestClassifyCommand(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty", "", "shell"},
		{"whitespace", "   ", "shell"},
		{"plain bash", "bash", "shell"},
		{"plain zsh", "zsh", "shell"},
		{"claude alone", "claude", "ai"},
		{"claude with path", "/usr/local/bin/claude", "ai"},
		{"claude with flags", "claude --help", "ai"},
		{"codex alone", "codex", "ai"},
		{"gemini chat", "gemini chat", "ai"},
		{"aider alone", "aider", "ai"},
		{"sudo claude", "sudo claude", "ai"},
		{"time go test", "time go test ./...", "test"},
		{"env npm test", "DEBUG=1 npm test", "test"},
		{"yarn test", "yarn test", "test"},
		{"pnpm test", "pnpm test --watch", "test"},
		{"cargo test", "cargo test --release", "test"},
		{"docker build", "docker build .", "build"},
		{"docker compose up", "docker compose up", "build"},
		{"docker-compose hyphen", "docker-compose up -d", "build"},
		{"docker ps not build", "docker ps", "shell"},
		{"kubectl", "kubectl get pods", "deploy"},
		{"terraform", "terraform plan", "deploy"},
		{"npx claude limitation", "npx claude", "shell"},
		{"go run not test", "go run ./cmd/foo", "shell"},
		{"nested env wrappers", "env DEBUG=1 sudo claude", "ai"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyCommand(tc.in); got != tc.want {
				t.Fatalf("ClassifyCommand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run TestClassifyCommand -v`
Expected: FAIL with "undefined: ClassifyCommand".

- [ ] **Step 3: Implement `ClassifyCommand`**

Create `internal/session/classify.go`:

```go
// Package session — session type classification.
//
// ClassifyCommand reads a command line and returns one of five labels.
// Used by applyOSC133Locked when a C event reports a new command, with
// sticky-non-shell semantics: a returned "shell" never overwrites a
// previously-set non-shell Type on the Session.
package session

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Type labels exported to clients via proto.SessionInfo.Type.
const (
	SessionTypeShell  = "shell"
	SessionTypeAI     = "ai"
	SessionTypeTest   = "test"
	SessionTypeBuild  = "build"
	SessionTypeDeploy = "deploy"
)

var envAssignRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=`)

// wrapperCommands take another command as an argument. We strip them while
// finding the "real" first token of a command line.
var wrapperCommands = map[string]struct{}{
	"sudo": {}, "time": {}, "nice": {}, "env": {},
}

// ClassifyCommand returns one of the SessionType* constants for cmd.
// Pure / total / O(len(cmd)).
func ClassifyCommand(cmd string) string {
	tokens := strings.Fields(cmd)
	// Strip wrappers and POSIX env-var prefixes from the front.
	for len(tokens) > 0 {
		t := tokens[0]
		if _, ok := wrapperCommands[t]; ok {
			tokens = tokens[1:]
			continue
		}
		if envAssignRE.MatchString(t) {
			tokens = tokens[1:]
			continue
		}
		break
	}
	if len(tokens) == 0 {
		return SessionTypeShell
	}
	first := filepath.Base(tokens[0])
	second := ""
	if len(tokens) > 1 {
		second = tokens[1]
	}

	switch first {
	case "codex", "claude", "gemini", "aider":
		return SessionTypeAI
	case "kubectl", "terraform":
		return SessionTypeDeploy
	case "docker-compose":
		return SessionTypeBuild
	case "docker":
		if second == "build" || second == "compose" {
			return SessionTypeBuild
		}
	case "go", "npm", "pnpm", "yarn", "cargo":
		if second == "test" {
			return SessionTypeTest
		}
	}
	return SessionTypeShell
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run TestClassifyCommand -v`
Expected: PASS — all 25 sub-cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/session/classify.go internal/session/classify_test.go
git -c commit.gpgsign=false commit -m "session/classify: ClassifyCommand maps cmd to shell|ai|test|build|deploy"
```

---

## Task 2: Add `Type` to `proto.SessionInfo`

**Files:**
- Modify: `internal/proto/frame.go`

- [ ] **Step 1: Add the field**

In `internal/proto/frame.go`, find the `SessionInfo` struct (lines 182-205). Add `Type` immediately after `LastOutputAt`:

```go
type SessionInfo struct {
	// ...existing fields up to LastOutputAt...
	LastOutputAt int64 `json:"last_output_at,omitempty"`
	// Type is the workload classification ("shell" | "ai" | "test" |
	// "build" | "deploy"), derived by session.ClassifyCommand from the
	// current command. Older publishers omit it; clients treat empty as
	// equivalent to "shell". See spec §3.
	Type string `json:"type,omitempty"`
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/attson/code/github.com.attson/atterm && go build ./...`
Expected: clean (no output).

- [ ] **Step 3: Run existing proto tests for regression**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/proto/`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/proto/frame.go
git -c commit.gpgsign=false commit -m "proto: SessionInfo gains Type field (omitempty)"
```

---

## Task 3: Wire classification into the session lifecycle (test-first)

**Files:**
- Modify: `internal/session/session.go`
- Modify: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/session/session_test.go`:

```go
func TestPushOut_AssignsTypeOnNonShellCommand(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	if got := s.MetaSnapshot().Type; got != SessionTypeShell {
		t.Fatalf("initial Type: got %q want %q", got, SessionTypeShell)
	}
	// OSC 133 C; claude --help
	osc := []byte("\x1b]133;C;claude --help\x1b\\")
	s.PushOut(osc)
	if got := s.MetaSnapshot().Type; got != SessionTypeAI {
		t.Fatalf("after claude: Type got %q want %q", got, SessionTypeAI)
	}
}

func TestPushOut_TypeStickyAfterShellCommand(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	s.PushOut([]byte("\x1b]133;C;claude\x1b\\"))
	if got := s.MetaSnapshot().Type; got != SessionTypeAI {
		t.Fatalf("post-claude: %q", got)
	}
	// "D;0" closes the running command, then a new C runs "ls".
	s.PushOut([]byte("\x1b]133;D;0\x1b\\"))
	s.PushOut([]byte("\x1b]133;C;ls -la\x1b\\"))
	if got := s.MetaSnapshot().Type; got != SessionTypeAI {
		t.Fatalf("after ls: Type got %q want %q (sticky non-shell)", got, SessionTypeAI)
	}
}

func TestPushOut_TypeChangesBetweenTwoNonShells(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	s.PushOut([]byte("\x1b]133;C;claude\x1b\\"))
	s.PushOut([]byte("\x1b]133;D;0\x1b\\"))
	s.PushOut([]byte("\x1b]133;C;npm test\x1b\\"))
	if got := s.MetaSnapshot().Type; got != SessionTypeTest {
		t.Fatalf("after npm test: Type got %q want %q", got, SessionTypeTest)
	}
}
```

If `MetaSnapshot()` does not exist as an exported accessor, check the existing test file — the existing `TestSubscribeIncludesTaskStateInMeta` may read meta a different way. Adapt the assertions to whatever accessor is already in use (e.g. `s.meta` directly if the tests are in the same package, which they are — these tests are in `package session`).

If reading `s.meta.Type` directly (same package), replace `s.MetaSnapshot().Type` with `s.meta.Type` in the three tests.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run 'TestPushOut_AssignsType|TestPushOut_TypeSticky|TestPushOut_TypeChanges' -v`
Expected: FAIL — initial Type is `""` (not yet defaulted), and applyOSC133Locked doesn't yet set `meta.Type`.

- [ ] **Step 3: Initialise `meta.Type` in `New`**

In `internal/session/session.go`, find `func New(id uuid.UUID, meta proto.SessionInfo) *Session` (line 98). Update:

```go
func New(id uuid.UUID, meta proto.SessionInfo) *Session {
	if meta.TaskState == "" {
		meta.TaskState = proto.TaskStateIdle
	}
	if meta.Type == "" {
		meta.Type = SessionTypeShell
	}
	return &Session{
		ID:        id,
		StartedAt: time.Now(),
		meta:      meta,
		subs:      make(map[*Subscriber]struct{}),
		scroll:    ringbuf.New(scrollbackBytes),
		inbound:   make(chan proto.Frame, inboundQueueDepth),
	}
}
```

- [ ] **Step 4: Update `applyOSC133Locked` to classify on each `C`**

In `internal/session/session.go`, locate the `'C'` case in `applyOSC133Locked` (starts at line ~592). After the block that sets `s.meta.CurrentCommand = command` and before the `case 'D':`, add the sticky-update block:

```go
		case 'C':
			command := strings.TrimSpace(strings.TrimPrefix(payload, "C;"))
			exitNil := (*int)(nil)
			if s.meta.TaskState != proto.TaskStateRunning {
				s.meta.TaskState = proto.TaskStateRunning
				changed = true
			}
			if s.meta.CurrentCommand != command {
				s.meta.CurrentCommand = command
				changed = true
			}
			// Sticky non-shell classification: shell never overwrites an
			// already-set non-shell tag (so opening `claude`, exiting back
			// to the shell prompt, and running `ls` keeps the session
			// flagged as ai). See spec §4.
			if newType := ClassifyCommand(command); newType != SessionTypeShell && s.meta.Type != newType {
				s.meta.Type = newType
				changed = true
			}
			started := now.Unix()
			s.cmdStarted = now
			// ...rest of the existing block (CommandStartedAt etc.) unchanged...
```

The insertion is the seven-line `if newType := ...` block right after the `CurrentCommand` assignment. The rest of the `'C'` branch stays as-is.

- [ ] **Step 5: Run the new tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run 'TestPushOut_AssignsType|TestPushOut_TypeSticky|TestPushOut_TypeChanges' -v`
Expected: PASS — three tests.

Run the full session suite as a regression gate:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/session/session.go internal/session/session_test.go
git -c commit.gpgsign=false commit -m "session: classify Type on every OSC 133 C, sticky non-shell"
```

---

## Task 4: TS type updates across the three frontend type files

**Files:**
- Modify: `desktop/frontend/src/lib/connection.ts`
- Modify: `desktop/frontend/src/platform/types.ts`
- Modify: `web/src/shared/api/types.ts`

- [ ] **Step 1: Add `type?: string` to desktop's `SessionInfo`**

In `desktop/frontend/src/lib/connection.ts`, find the `SessionInfo` interface (around line 411). Add `type?: string` after `last_output_at?: number`:

```ts
export interface SessionInfo {
  // ...existing fields...
  last_output_at?: number;
  type?: string; // "shell" | "ai" | "test" | "build" | "deploy" — absent on older publishers
}
```

- [ ] **Step 2: Add `type?: string` to mobile's `RemoteSession`**

In `desktop/frontend/src/platform/types.ts`, find the `RemoteSession` interface. Add at the end:

```ts
export interface RemoteSession {
  // ...existing fields...
  last_output_at?: number;
  type?: string;
}
```

(Check the exact existing tail — the field after which you should place `type?: string` is `last_output_at?: number`.)

Also propagate the field in the Capacitor platform's `listRemoteSessions` mapping. Open `desktop/frontend/src/platform/capacitor.ts` and find the `raw.map((s) => { ... })` block inside `listRemoteSessions`. Add a corresponding line:

```ts
if (s.type !== undefined) out.type = s.type
```

Right after the `if (s.last_output_at !== undefined) out.last_output_at = s.last_output_at` line.

Inspect the `raw` element type declaration in the same map block and add `type?: string` to the inline element type so TypeScript doesn't error.

- [ ] **Step 3: Add `type?: string` to web's `SessionInfo`**

In `web/src/shared/api/types.ts`, find `SessionInfo`. Add at the end:

```ts
export interface SessionInfo {
  // ...existing fields...
  last_output_at?: number;
  type?: string;
}
```

- [ ] **Step 4: Verify type-check**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean.

If the web project has its own tsc check, run that too:
Run: `cd /Users/attson/code/github.com.attson/atterm/web && npx vue-tsc --noEmit 2>&1 | tail -3`
Expected: clean (or skip if web doesn't run vue-tsc in CI).

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/connection.ts desktop/frontend/src/platform/types.ts desktop/frontend/src/platform/capacitor.ts web/src/shared/api/types.ts
git -c commit.gpgsign=false commit -m "frontend/types: SessionInfo.type? + RemoteSession.type? plumbed through"
```

---

## Task 5: `sessionType.ts` display helper (test-first)

**Files:**
- Create: `desktop/frontend/src/lib/sessionType.ts`
- Create: `desktop/frontend/src/lib/__tests__/sessionType.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/lib/__tests__/sessionType.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { displayForType } from '../sessionType'

describe('displayForType', () => {
  it('returns null for shell / undefined / empty / unknown', () => {
    expect(displayForType(undefined)).toBeNull()
    expect(displayForType('')).toBeNull()
    expect(displayForType('shell')).toBeNull()
    expect(displayForType('something-weird')).toBeNull()
  })

  it.each(['ai', 'test', 'build', 'deploy'] as const)('returns key+color+iconPath for %s', (t) => {
    const d = displayForType(t)
    expect(d).not.toBeNull()
    expect(d!.key).toBe(t)
    expect(d!.color).toMatch(/^#[0-9a-f]{6}$/i)
    expect(d!.iconPath.length).toBeGreaterThan(0)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/lib/__tests__/sessionType.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the helper**

Create `desktop/frontend/src/lib/sessionType.ts`:

```ts
// Shared display metadata for session.type ("shell" | "ai" | "test" | "build"
// | "deploy"). Renderers (desktop TabBar, mobile MobileSessionList, web
// SessionList) all call displayForType() and decide their own layout.
//
// SVG path strings are 16×16 viewBox, single-path, stroke="currentColor"
// stroke-width="1.6" linecap="round" linejoin="round" — match the lucide
// look used elsewhere in the app. iconPath is the `d` attribute only;
// renderers wrap it in <svg viewBox="0 0 16 16"><path d=... /></svg>.

export type DisplayKey = 'ai' | 'test' | 'build' | 'deploy'

export interface TypeDisplay {
  key: DisplayKey
  color: string
  iconPath: string
}

const TABLE: Record<DisplayKey, TypeDisplay> = {
  ai: {
    key: 'ai',
    color: '#a78bfa',
    // 4-pointed sparkle.
    iconPath: 'M8 2v3M8 11v3M2 8h3M11 8h3M3.5 3.5l2 2M10.5 10.5l2 2M3.5 12.5l2-2M10.5 5.5l2-2',
  },
  test: {
    key: 'test',
    color: '#34d399',
    // Conical flask outline.
    iconPath: 'M6 2h4M7 2v4l-4 8h10l-4-8V2',
  },
  build: {
    key: 'build',
    color: '#fbbf24',
    // Stacked box (package).
    iconPath: 'M2 5l6-3 6 3v6l-6 3-6-3V5zM2 5l6 3 6-3M8 8v6',
  },
  deploy: {
    key: 'deploy',
    color: '#f87171',
    // Up arrow into a cloud-like cap.
    iconPath: 'M8 13V4M3 8l5-5 5 5M3 13h10',
  },
}

export function displayForType(t: string | undefined | null): TypeDisplay | null {
  if (!t || t === 'shell') return null
  return (TABLE as Record<string, TypeDisplay>)[t] ?? null
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/lib/__tests__/sessionType.test.ts`
Expected: PASS — 5 cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/sessionType.ts desktop/frontend/src/lib/__tests__/sessionType.test.ts
git -c commit.gpgsign=false commit -m "frontend/sessionType: displayForType helper (color + 16x16 svg path)"
```

---

## Task 6: i18n keys

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

- [ ] **Step 1: Add the keys to `en.ts`**

In `desktop/frontend/src/i18n/messages/en.ts`, find the `mobile:` object (the same one that holds `pasteClipboard`, `pasteImage`, etc.). Add a sibling block:

```ts
    taskTypes: {
      ai: 'AI',
      test: 'Test',
      build: 'Build',
      deploy: 'Deploy',
    },
```

Place it near other `mobile.*` groups for readability.

- [ ] **Step 2: Mirror in `zh-CN.ts`**

In `desktop/frontend/src/i18n/messages/zh-CN.ts`, mirror the same shape:

```ts
    taskTypes: {
      ai: 'AI',
      test: '测试',
      build: '构建',
      deploy: '部署',
    },
```

- [ ] **Step 3: Verify parity test**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/i18n/i18n.test.ts`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git -c commit.gpgsign=false commit -m "i18n: add mobile.taskTypes.{ai,test,build,deploy} (en + zh-CN)"
```

---

## Task 7: Desktop `TabBar.vue` — render the type icon (test-first)

**Files:**
- Modify: `desktop/frontend/src/components/TabBar.vue`
- Modify: `desktop/frontend/src/components/TabBar.test.ts`

- [ ] **Step 1: Add a failing test**

Append to `desktop/frontend/src/components/TabBar.test.ts`:

```ts
it("renders a type icon for non-shell sessions and nothing for shell", async () => {
  await initI18n({
    loadPreference: async () => "en",
    getLanguages: () => ["en"],
    listenLanguageChange: () => () => undefined,
  });

  const wrapper = mount(TabBar, {
    props: {
      tabs: [
        {
          id: "tab-ai",
          layout: "single" as const,
          activeSession: { id: "s1", command: "claude", cwd: "/", title: "", cols: 80, rows: 24, started_at: 0, type: "ai" },
          activeRemote: false,
          paneCount: 1,
        },
        {
          id: "tab-shell",
          layout: "single" as const,
          activeSession: { id: "s2", command: "bash", cwd: "/", title: "", cols: 80, rows: 24, started_at: 0, type: "shell" },
          activeRemote: false,
          paneCount: 1,
        },
      ],
      currentId: "tab-ai",
      starting: false,
    },
  });

  const aiTab = wrapper.get('[data-tab-id="tab-ai"]');
  expect(aiTab.find('.type-icon').exists()).toBe(true);

  const shellTab = wrapper.get('[data-tab-id="tab-shell"]');
  expect(shellTab.find('.type-icon').exists()).toBe(false);
});
```

This test relies on `data-tab-id` attributes that don't yet exist on the tab divs — the next step adds them.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/TabBar.test.ts -t "type icon"`
Expected: FAIL — `data-tab-id` not found.

- [ ] **Step 3: Add the icon to `TabBar.vue`**

Open `desktop/frontend/src/components/TabBar.vue`. Add an import at the top of `<script setup>`:

```ts
import { displayForType } from '../lib/sessionType'
```

Add a helper inside the script:

```ts
function typeForTab(t: { activeSession: { type?: string } | null }) {
  return displayForType(t.activeSession?.type)
}
```

In the template, find the tab `<div>` (around line 67). Add `:data-tab-id="t.id"` to it for test reachability. Then between the existing `.dot` / `.layout-icon` blocks and the `.title` span, insert:

```vue
<span v-if="typeForTab(t)" class="type-icon" :title="typeForTab(t)!.key" :style="{ color: typeForTab(t)!.color }">
  <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path :d="typeForTab(t)!.iconPath" />
  </svg>
</span>
```

Add CSS in the `<style scoped>` block, next to the other `.tab .X { ... }` rules:

```css
.tab .type-icon { display: inline-flex; align-items: center; margin: 0 4px 0 2px; }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/TabBar.test.ts`
Expected: PASS — the new case plus the two existing ones.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/TabBar.vue desktop/frontend/src/components/TabBar.test.ts
git -c commit.gpgsign=false commit -m "frontend/TabBar: render session type icon for non-shell tabs"
```

---

## Task 8: Mobile `MobileSessionList.vue` — render the chip (test-first)

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileSessionList.vue`
- Modify: `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts`

- [ ] **Step 1: Add a failing test**

Open `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts` and append:

```ts
it('shows the localised type chip for non-shell sessions', async () => {
  // Mock listRemoteSessions to return one ai-typed and one shell-typed session.
  const fakePlatform = createFakePlatform()
  fakePlatform.sessions.listRemoteSessions = vi.fn().mockResolvedValue([
    { session_id: 'a', host_id: 'h', host: 'box', user: 'me', title: 'claude', cwd: '/', cols: 80, rows: 24, type: 'ai' },
    { session_id: 'b', host_id: 'h', host: 'box', user: 'me', title: 'bash', cwd: '/', cols: 80, rows: 24, type: 'shell' },
  ])
  __setPlatformForTests(fakePlatform)

  const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
  await flushPromises()

  const aiCard = w.get('[data-testid="task-card-a"]')
  expect(aiCard.find('.type-chip').exists()).toBe(true)
  expect(aiCard.find('.type-chip').text()).toBe('AI')

  const shellCard = w.get('[data-testid="task-card-b"]')
  expect(shellCard.find('.type-chip').exists()).toBe(false)
})
```

The imports `createFakePlatform`, `__setPlatformForTests`, `MobileSessionList`, `mount`, `flushPromises`, `vi` must match what the existing test file uses. If `createFakePlatform` is imported from a helper path, copy that import. The existing test file at the top of `MobileSessionList.test.ts` should show the pattern.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/MobileSessionList.test.ts -t "type chip"`
Expected: FAIL — `.type-chip` selector not found.

- [ ] **Step 3: Update `MobileSessionList.vue`**

Open `desktop/frontend/src/mobile/MobileSessionList.vue`. Add to the `<script setup>` imports:

```ts
import { displayForType } from '../lib/sessionType'
```

Add a helper:

```ts
function typeForSession(s: RemoteSession) {
  return displayForType(s.type)
}
```

In the template, find the card row that contains `<span :data-testid="\`task-card-${s.session_id}\`" class="col2">`. Wrap the existing title row, OR insert a sibling immediately before the title `<span class="ttl">`:

```vue
<span class="col2" :data-testid="`task-card-${s.session_id}`">
  <span class="title-row">
    <span v-if="typeForSession(s)" class="type-chip" :style="{ '--chip': typeForSession(s)!.color }">
      {{ t(`mobile.taskTypes.${typeForSession(s)!.key}`) }}
    </span>
    <span class="ttl">{{ taskTitle(s) }}</span>
  </span>
  <span v-if="s.cwd" :data-testid="`session-cwd-${s.session_id}`" class="cwd">{{ s.cwd }}</span>
  <span class="meta">{{ taskMeta(s) }}</span>
</span>
```

Add styling in the `<style scoped>` block:

```css
.title-row { display: flex; align-items: center; gap: 6px; min-width: 0; }
.type-chip {
  font-size: 0.66rem; line-height: 1;
  padding: 2px 6px; border-radius: 4px;
  border: 1px solid color-mix(in srgb, var(--chip) 60%, transparent);
  color: var(--chip);
  background: color-mix(in srgb, var(--chip) 12%, transparent);
  text-transform: uppercase; letter-spacing: 0.04em;
  flex: 0 0 auto;
}
```

- [ ] **Step 4: Run the tests**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/MobileSessionList.test.ts`
Expected: PASS — the new case + all existing ones (they shouldn't break; the chip is inside `.col2`, the `task-card-${id}` testid stays present).

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/MobileSessionList.vue desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts
git -c commit.gpgsign=false commit -m "frontend/mobile: render session type chip on task cards"
```

---

## Task 9: Web `SessionList.vue` — mirror the chip

**Files:**
- Create: `web/src/shared/sessionType.ts`
- Modify: `web/src/main/components/SessionList.vue`

The web project doesn't import from `desktop/frontend/`. Easiest path is a parallel copy.

- [ ] **Step 1: Copy the helper into the web project**

Create `web/src/shared/sessionType.ts` with identical content to `desktop/frontend/src/lib/sessionType.ts` (copy-paste the file contents from Task 5 step 3).

- [ ] **Step 2: Add the chip to `SessionList.vue`**

Open `web/src/main/components/SessionList.vue`. Add to the imports:

```ts
import { displayForType } from '@/shared/sessionType'
```

(Use whatever path alias the web project uses; `@/` is the typical Vite default. If the project uses a different alias for `web/src/shared`, swap it in.)

Add a helper:

```ts
function typeForSession(s: SessionInfo) {
  return displayForType(s.type)
}
```

Locate the existing per-session card markup in the template (around line 113 per the explorer report, though verify). Insert a chip span before the session title node:

```vue
<span v-if="typeForSession(s)" class="type-chip" :style="{ '--chip': typeForSession(s)!.color }">
  {{ t(`mobile.taskTypes.${typeForSession(s)!.key}`) }}
</span>
```

(If web has its own i18n keys instead of reusing `mobile.taskTypes.*`, adjust to the web-local key path. The simplest is to add the same keys under a web-local namespace and reference those. Look at the existing `t(...)` calls in `SessionList.vue` to see which namespace the file uses.)

Add styles mirroring mobile's:

```css
.type-chip {
  font-size: 0.66rem; line-height: 1;
  padding: 2px 6px; border-radius: 4px;
  border: 1px solid color-mix(in srgb, var(--chip) 60%, transparent);
  color: var(--chip);
  background: color-mix(in srgb, var(--chip) 12%, transparent);
  text-transform: uppercase; letter-spacing: 0.04em;
  margin-right: 6px;
}
```

- [ ] **Step 3: Verify the web build still compiles**

Run: `cd /Users/attson/code/github.com.attson/atterm/web && npm run build 2>&1 | tail -5`
Expected: build succeeds. If the project lacks a `build` script, check `web/package.json` and run the equivalent (`vite build`, etc.).

If the web project has tests, add a simple one analogous to the mobile chip test. If it doesn't have a test infrastructure, lean on the desktop + mobile tests for the helper's correctness.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/shared/sessionType.ts web/src/main/components/SessionList.vue
git -c commit.gpgsign=false commit -m "web/SessionList: render session type chip mirroring mobile"
```

---

## Task 10: Final smoke

**Files:**
- (none — verification only)

- [ ] **Step 1: Full Go suite**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./...`
Expected: PASS.

- [ ] **Step 2: `go vet`**

Run: `cd /Users/attson/code/github.com.attson/atterm && go vet ./...`
Expected: clean.

- [ ] **Step 3: Desktop frontend tests + type-check**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test`
Expected: PASS — every existing test plus the new ones in Tasks 5, 7, 8.

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean.

- [ ] **Step 4: Frontend builds**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:wails`
Expected: succeeds.

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:capacitor`
Expected: succeeds.

- [ ] **Step 5: Web build**

Run: `cd /Users/attson/code/github.com.attson/atterm/web && npm run build 2>&1 | tail -3`
Expected: succeeds.

- [ ] **Step 6: Manual smoke (documented, not gating)**

For local verification:
1. Start the relay locally: `go run ./cmd/atterm-relay --dev-insecure --addr :8080`.
2. Start the desktop app with a connected uplink.
3. In a desktop terminal tab, run `claude --help`. After the OSC 133 C event fires, the tab gets the violet AI icon.
4. Exit claude, run `npm test` in the same tab — icon switches to the emerald test icon.
5. Open the web admin and the mobile app pointed at the same relay — the task cards show matching chips.

No commit needed.

---

## Self-review notes

- **Spec coverage:**
  - §3 classification rules → Task 1 (table-driven with edge cases enumerated)
  - §4 sticky non-shell application timing → Task 3
  - §5 `displayForType` helper → Task 5
  - §6.1 TabBar icon → Task 7
  - §6.2 mobile chip → Task 8
  - §6.3 web chip → Task 9
  - §7 errors — classification is total / no I/O; no extra observability ride-along required for v0.5 (spec explicitly defers metrics)
  - §8.1 / §8.2 / §8.3 tests → Tasks 1, 3, 5, 7, 8
  - §9 rollout — single Go ALTER-free, JSON omitempty handles old publishers/consumers; this falls out of the design and isn't a separate task

- **Placeholder scan:** no TBDs. SVG `iconPath` strings are concrete (4 distinct shapes). Color hex values are pinned. The only "verify-the-call" instruction is Task 4 step 2 (`raw` element type in capacitor.ts) and Task 8 step 1 (the test file's existing import shape) — both are runtime navigation hints, not implementation gaps.

- **Type consistency:** `SessionType*` constants in Go (Task 1) match the string literals checked by tests in Task 3, by `displayForType` in Task 5, and by the renderers in Tasks 7-9. The frontend interface field is consistently `type?: string` in three TypeScript files (Task 4) and consumed as `s.type` in two helpers and the renderer templates.

---

Plan complete and saved to `docs/superpowers/plans/2026-06-04-session-type-classification.md`. Execution choice:

**1. Subagent-Driven (recommended)** — fresh subagent per task + two-stage review.

**2. Inline Execution** — batch tasks with checkpoints.
