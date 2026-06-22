# Desktop Logging Formatting + PTY Input Debug — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give desktop logs a consistent leveled/tagged plain-text format, add a config-toggled PTY-input debug log to diagnose stray-ESC mid-turn cancellations, and render the existing settings log viewers colorized with a level filter.

**Architecture:** All `log.Printf` already funnels through `loggingManager` (an `io.Writer` installed via `log.SetOutput`). We make that manager own line formatting and add a leveled `Emit` API plus `logDebug/Info/Warn/Error` helpers; legacy lines auto-normalize to `INFO [app]`. The PTY input debug log lives at the desktop layer (`desktopPtyHost.Write`) gated by a new config bool, writing into the same `desktop.log`. The frontend parses the plain-text format per line and colorizes/filters by level in the two existing viewers.

**Tech Stack:** Go (desktop Wails backend, `package main` under `desktop/`), Vue 3 + TypeScript (`desktop/frontend`), Vitest, Go testing.

## Global Constraints

- Branch: `feat/desktop-logging-formatting` (already created; spec committed).
- Log line format (file, plain text, no ANSI): `2006/01/02 15:04:05.000 LEVEL [tag] message`, LEVEL left-padded to width 5, one of `DEBUG`/`INFO`/`WARN`/`ERROR`.
- PTY input debug config default: **false**. Toggle takes effect live (config read per-access via `cfgStore.Get()`).
- Reuse the existing `desktop.log`; do NOT open a second file. Do NOT log PTY output.
- Go config pattern: optional `*bool` field + `XxxOrDefault()` accessor (see `NotificationsEnabledOrDefault`). Persist via `a.cfgStore.Set`.
- Frontend api.ts is a hand-written shim over `window.go.main.App`; add an `AppBindings` interface entry + an exported wrapper for each new Go method.
- i18n: every user-facing string added to BOTH `desktop/frontend/src/i18n/messages/en.ts` and `zh-CN.ts`.
- Commit after every task.

---

### Task 1: Go logging core — structured format, Emit, leveled helpers, legacy normalization

**Files:**
- Modify: `desktop/logging.go`
- Create: `desktop/logging_format_test.go`

**Interfaces:**
- Produces:
  - `func formatLogLine(t time.Time, level, tag, msg string) string` — returns `"2006/01/02 15:04:05.000 LEVEL [tag] msg\n"`.
  - `func (m *loggingManager) Emit(level, tag, msg string)`
  - `func logDebug(tag, format string, args ...any)` (and `logInfo`, `logWarn`, `logError`)
  - package var `activeLogManager *loggingManager`

- [ ] **Step 1: Write the failing test**

Create `desktop/logging_format_test.go`:

```go
package main

import (
	"strings"
	"testing"
	"time"
)

func TestFormatLogLine(t *testing.T) {
	ts := time.Date(2026, 6, 22, 15, 4, 5, 123_000_000, time.UTC)
	cases := []struct {
		level, tag, msg, want string
	}{
		{"DEBUG", "pty-input", "write n=1 hex=1b LONE-ESC",
			"2026/06/22 15:04:05.123 DEBUG [pty-input] write n=1 hex=1b LONE-ESC\n"},
		{"INFO", "app", "hello",
			"2026/06/22 15:04:05.123 INFO  [app] hello\n"},
		{"WARN", "relay", "dropping",
			"2026/06/22 15:04:05.123 WARN  [relay] dropping\n"},
		{"ERROR", "app", "boom",
			"2026/06/22 15:04:05.123 ERROR [app] boom\n"},
	}
	for _, c := range cases {
		got := formatLogLine(ts, c.level, c.tag, c.msg)
		if got != c.want {
			t.Errorf("formatLogLine(%s) = %q, want %q", c.level, got, c.want)
		}
	}
}

func TestEmitWritesFormattedLine(t *testing.T) {
	var buf strings.Builder
	m := &loggingManager{currentWriter: &buf}
	m.Emit("DEBUG", "pty-input", "hi")
	got := buf.String()
	if !strings.Contains(got, " DEBUG [pty-input] hi\n") {
		t.Errorf("Emit wrote %q, missing formatted suffix", got)
	}
}

func TestLegacyWriteNormalized(t *testing.T) {
	var buf strings.Builder
	m := &loggingManager{currentWriter: &buf}
	_, _ = m.Write([]byte("client: dropping frame\n"))
	got := buf.String()
	if !strings.Contains(got, " INFO  [app] client: dropping frame\n") {
		t.Errorf("legacy Write produced %q, want normalized INFO [app] line", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop && go test ./... -run 'TestFormatLogLine|TestEmit|TestLegacyWrite' -v`
Expected: FAIL — `formatLogLine` / `Emit` undefined.

- [ ] **Step 3: Implement formatting + Emit + helpers + legacy normalization**

In `desktop/logging.go`, add imports `fmt` and `time` (keep existing). Add near the top-level funcs:

```go
// activeLogManager is the process-wide logging manager, set in
// newLoggingManager. The leveled helpers (logDebug/...) route through it.
var activeLogManager *loggingManager

const logTimeLayout = "2006/01/02 15:04:05.000"

// formatLogLine renders one structured, plain-text log record:
//   2006/01/02 15:04:05.000 LEVEL [tag] message\n
// LEVEL is left-padded to width 5 for column alignment.
func formatLogLine(t time.Time, level, tag, msg string) string {
	return t.Format(logTimeLayout) + " " + padLevel(level) + " [" + tag + "] " + msg + "\n"
}

func padLevel(level string) string {
	for len(level) < 5 {
		level += " "
	}
	return level
}

// Emit writes a level/tag-tagged record through the current sink.
func (m *loggingManager) Emit(level, tag, msg string) {
	line := formatLogLine(time.Now(), level, tag, msg)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.currentWriter == nil {
		return
	}
	_, _ = io.WriteString(m.currentWriter, line)
}

func logEmit(level, tag, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if activeLogManager != nil {
		activeLogManager.Emit(level, tag, msg)
		return
	}
	log.Printf("[%s] %s", tag, msg)
}

func logDebug(tag, format string, args ...any) { logEmit("DEBUG", tag, format, args...) }
func logInfo(tag, format string, args ...any)  { logEmit("INFO", tag, format, args...) }
func logWarn(tag, format string, args ...any)  { logEmit("WARN", tag, format, args...) }
func logError(tag, format string, args ...any) { logEmit("ERROR", tag, format, args...) }
```

Replace the existing `Write` method so legacy `log.Printf` output is normalized:

```go
func (m *loggingManager) Write(p []byte) (int, error) {
	line := formatLogLine(time.Now(), "INFO", "app", strings.TrimRight(string(p), "\n"))
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := io.WriteString(m.currentWriter, line); err != nil {
		return 0, err
	}
	return len(p), nil
}
```

In `newLoggingManager`, stop the stdlib from prepending its own timestamp and record the active manager. Change:

```go
	m.logger = log.New(m, "", log.LstdFlags)
	log.SetOutput(m)
	return m, nil
```
to:
```go
	m.logger = log.New(m, "", 0)
	log.SetFlags(0)
	log.SetOutput(m)
	activeLogManager = m
	return m, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop && go test ./... -run 'TestFormatLogLine|TestEmit|TestLegacyWrite' -v`
Expected: PASS.

- [ ] **Step 5: Build the whole desktop package**

Run: `cd desktop && go build ./...`
Expected: builds clean.

- [ ] **Step 6: Commit**

```bash
git add desktop/logging.go desktop/logging_format_test.go
git commit -m "feat(logging): structured leveled log format + Emit/helpers

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: PTY input debug — revert temp code, config, Wails, desktopPtyHost.Write

**Files:**
- Modify: `internal/ptyhost/ptyhost.go` (revert the `ATTERM_PTY_DEBUG` instrumentation)
- Modify: `desktop/config.go` (add field + accessor)
- Modify: `desktop/app.go` (Get/Set methods)
- Modify: `desktop/paste_image.go` (add `cfg` field + `logPtyInput` + `Write` override)
- Modify: `desktop/relay_host.go` (pass `cfg` at the `desktopPtyHost` construction)
- Create: `desktop/pty_input_debug_test.go`

**Interfaces:**
- Consumes: `logDebug` (Task 1), `configStore.Get()`, `appConfig`.
- Produces:
  - `func (c appConfig) PtyInputDebugEnabledOrDefault() bool`
  - `func ptyInputDebugTag(p []byte) string` — `" LONE-ESC"` / `" ESC-LEAD"` / `""`
  - `func logPtyInput(cfg *configStore, p []byte)`
  - `App.GetPtyInputDebugEnabled() bool`, `App.SetPtyInputDebugEnabled(bool) error`

- [ ] **Step 1: Revert the temporary instrumentation in `internal/ptyhost/ptyhost.go`**

Restore the import block to exactly:

```go
import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)
```

Delete the `ptyDebug*` block (the `ptyDebugOnce`/`ptyDebugLog` vars, `ptyDebugLogger`, `ptyDebugWrite`). Restore `Write` to:

```go
// Write pushes bytes into the PTY master (i.e. as if the user typed them).
func (h *Host) Write(p []byte) (int, error) { return h.ptmx.Write(p) }
```

Verify nothing else references the removed symbols: `grep -rn "ptyDebug\|ATTERM_PTY_DEBUG" internal/ desktop/` → no hits.

- [ ] **Step 2: Write the failing test**

Create `desktop/pty_input_debug_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestPtyInputDebugEnabledOrDefault(t *testing.T) {
	var c appConfig
	if c.PtyInputDebugEnabledOrDefault() {
		t.Fatal("default should be false")
	}
	v := true
	c.PtyInputDebugEnabled = &v
	if !c.PtyInputDebugEnabledOrDefault() {
		t.Fatal("should be true when set")
	}
}

func TestPtyInputDebugTag(t *testing.T) {
	cases := map[string]struct {
		in   []byte
		want string
	}{
		"lone esc":  {[]byte{0x1b}, " LONE-ESC"},
		"esc lead":  {[]byte{0x1b, '[', 'O'}, " ESC-LEAD"},
		"plain":     {[]byte("a"), ""},
		"empty":     {[]byte{}, ""},
	}
	for name, c := range cases {
		if got := ptyInputDebugTag(c.in); got != c.want {
			t.Errorf("%s: ptyInputDebugTag = %q, want %q", name, got, c.want)
		}
	}
}

func TestLogPtyInputGating(t *testing.T) {
	var buf strings.Builder
	prev := activeLogManager
	activeLogManager = &loggingManager{currentWriter: &buf}
	defer func() { activeLogManager = prev }()

	off := &configStore{}
	logPtyInput(off, []byte{0x1b})
	if buf.Len() != 0 {
		t.Fatalf("disabled: expected no output, got %q", buf.String())
	}

	on := &configStore{}
	v := true
	on.cfg.PtyInputDebugEnabled = &v
	logPtyInput(on, []byte{0x1b})
	got := buf.String()
	if !strings.Contains(got, "DEBUG [pty-input] write n=1 hex=1b LONE-ESC") {
		t.Fatalf("enabled: missing expected debug line, got %q", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd desktop && go test ./... -run 'TestPtyInputDebug|TestLogPtyInput' -v`
Expected: FAIL — `PtyInputDebugEnabledOrDefault` / `ptyInputDebugTag` / `logPtyInput` undefined.

- [ ] **Step 4: Add config field + accessor**

In `desktop/config.go`, inside the `appConfig` struct, next to the other optional bool fields (e.g. just after `NotificationsEnabled *bool`), add:

```go
	// PtyInputDebugEnabled logs every byte slice written into a session PTY
	// (hex, tagged [pty-input] at DEBUG) for diagnosing stuck/dropped input.
	PtyInputDebugEnabled *bool `json:"ptyInputDebugEnabled,omitempty"`
```

Add the accessor next to `NotificationsEnabledOrDefault`:

```go
func (c appConfig) PtyInputDebugEnabledOrDefault() bool {
	if c.PtyInputDebugEnabled == nil {
		return false
	}
	return *c.PtyInputDebugEnabled
}
```

- [ ] **Step 5: Add the tag helper + gated logger + Write override in `desktop/paste_image.go`**

Add `"encoding/hex"` to the import block. Change the struct:

```go
type desktopPtyHost struct {
	*ptyhost.Host
	cfg *configStore
}
```

Add at the end of the file:

```go
// ptyInputDebugTag flags an input write whose lone/leading ESC is the
// suspected cause of mid-turn cancellations in child TUIs.
func ptyInputDebugTag(p []byte) string {
	switch {
	case len(p) == 1 && p[0] == 0x1b:
		return " LONE-ESC"
	case len(p) > 0 && p[0] == 0x1b:
		return " ESC-LEAD"
	default:
		return ""
	}
}

// logPtyInput emits a DEBUG [pty-input] line for one PTY write when the
// config toggle is on. Never blocks or affects the actual write.
func logPtyInput(cfg *configStore, p []byte) {
	if cfg == nil || !cfg.Get().PtyInputDebugEnabledOrDefault() {
		return
	}
	logDebug("pty-input", "write n=%d hex=%s%s", len(p), hex.EncodeToString(p), ptyInputDebugTag(p))
}

// Write intercepts all session input for optional debug logging, then
// forwards to the underlying PTY master.
func (h *desktopPtyHost) Write(p []byte) (int, error) {
	logPtyInput(h.cfg, p)
	return h.Host.Write(p)
}
```

- [ ] **Step 6: Pass the config store at construction in `desktop/relay_host.go`**

Change line ~453 from:
```go
	cleanup := h.server.AdoptSession(ctx, id, info, &desktopPtyHost{Host: pty}, h.adminUserID)
```
to:
```go
	cleanup := h.server.AdoptSession(ctx, id, info, &desktopPtyHost{Host: pty, cfg: h.cfg}, h.adminUserID)
```

- [ ] **Step 7: Add Wails Get/Set in `desktop/app.go`**

After `SetNotificationsEnabled` (around line 1482), add:

```go
// GetPtyInputDebugEnabled reports whether PTY input debug logging is on.
func (a *App) GetPtyInputDebugEnabled() bool {
	if a.cfgStore == nil {
		return false
	}
	return a.cfgStore.Get().PtyInputDebugEnabledOrDefault()
}

// SetPtyInputDebugEnabled persists the PTY input debug logging toggle.
func (a *App) SetPtyInputDebugEnabled(enabled bool) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	cfg := a.cfgStore.Get()
	cfg.PtyInputDebugEnabled = &enabled
	return a.cfgStore.Set(cfg)
}
```

- [ ] **Step 8: Run tests + build**

Run: `cd desktop && go test ./... -run 'TestPtyInputDebug|TestLogPtyInput' -v && go build ./... && go build ../internal/ptyhost/`
Expected: PASS, both builds clean.

- [ ] **Step 9: Commit**

```bash
git add internal/ptyhost/ptyhost.go desktop/config.go desktop/app.go desktop/paste_image.go desktop/relay_host.go desktop/pty_input_debug_test.go
git commit -m "feat(desktop): config-toggled PTY input debug logging

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Frontend `parseLogLine` util + level ordering

**Files:**
- Create: `desktop/frontend/src/lib/parseLogLine.ts`
- Create: `desktop/frontend/src/lib/parseLogLine.test.ts`

**Interfaces:**
- Produces:
  - `type LogLevel = "DEBUG" | "INFO" | "WARN" | "ERROR"`
  - `type ParsedLogLine = { kind: "structured"; ts: string; level: LogLevel; tag: string; msg: string } | { kind: "raw"; text: string }`
  - `function parseLogLine(line: string): ParsedLogLine`
  - `const LEVEL_ORDER: Record<LogLevel, number>`
  - `function levelAtLeast(level: LogLevel, min: LogLevel): boolean`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/lib/parseLogLine.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { parseLogLine, levelAtLeast } from "./parseLogLine";

describe("parseLogLine", () => {
  it("parses a structured line", () => {
    const r = parseLogLine("2026/06/22 15:04:05.123 DEBUG [pty-input] write n=1 hex=1b LONE-ESC");
    expect(r).toEqual({
      kind: "structured",
      ts: "2026/06/22 15:04:05.123",
      level: "DEBUG",
      tag: "pty-input",
      msg: "write n=1 hex=1b LONE-ESC",
    });
  });

  it("parses padded INFO with extra spaces", () => {
    const r = parseLogLine("2026/06/22 15:04:05.123 INFO  [app] hello");
    expect(r.kind).toBe("structured");
    if (r.kind === "structured") {
      expect(r.level).toBe("INFO");
      expect(r.tag).toBe("app");
      expect(r.msg).toBe("hello");
    }
  });

  it("falls back to raw for non-matching lines", () => {
    const r = parseLogLine("    at someStackFrame (file.go:10)");
    expect(r).toEqual({ kind: "raw", text: "    at someStackFrame (file.go:10)" });
  });
});

describe("levelAtLeast", () => {
  it("compares severity", () => {
    expect(levelAtLeast("WARN", "INFO")).toBe(true);
    expect(levelAtLeast("DEBUG", "INFO")).toBe(false);
    expect(levelAtLeast("ERROR", "ERROR")).toBe(true);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/parseLogLine.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the util**

Create `desktop/frontend/src/lib/parseLogLine.ts`:

```ts
export type LogLevel = "DEBUG" | "INFO" | "WARN" | "ERROR";

export type ParsedLogLine =
  | { kind: "structured"; ts: string; level: LogLevel; tag: string; msg: string }
  | { kind: "raw"; text: string };

export const LEVEL_ORDER: Record<LogLevel, number> = {
  DEBUG: 0,
  INFO: 1,
  WARN: 2,
  ERROR: 3,
};

const LINE_RE =
  /^(\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2}\.\d{3}) (DEBUG|INFO|WARN|ERROR)\s+\[([^\]]+)\] (.*)$/;

export function parseLogLine(line: string): ParsedLogLine {
  const m = LINE_RE.exec(line);
  if (!m) return { kind: "raw", text: line };
  return {
    kind: "structured",
    ts: m[1],
    level: m[2] as LogLevel,
    tag: m[3],
    msg: m[4],
  };
}

export function levelAtLeast(level: LogLevel, min: LogLevel): boolean {
  return LEVEL_ORDER[level] >= LEVEL_ORDER[min];
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/lib/parseLogLine.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/parseLogLine.ts desktop/frontend/src/lib/parseLogLine.test.ts
git commit -m "feat(logging-ui): parseLogLine util + level ordering

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `LogLines.vue` shared colorized/filtered renderer

**Files:**
- Create: `desktop/frontend/src/components/LogLines.vue`
- Create: `desktop/frontend/src/components/LogLines.test.ts`

**Interfaces:**
- Consumes: `parseLogLine`, `levelAtLeast`, `LogLevel` (Task 3).
- Produces: a component with props `{ content: string; minLevel?: LogLevel }`. Structured lines below `minLevel` are hidden; raw (unparseable) lines are always shown.

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/LogLines.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import LogLines from "./LogLines.vue";

const content = [
  "2026/06/22 15:04:05.123 DEBUG [pty-input] write n=1 hex=61",
  "2026/06/22 15:04:05.200 WARN  [relay] dropping frame",
  "    raw continuation line",
].join("\n");

describe("LogLines", () => {
  it("renders every line at minLevel DEBUG", () => {
    const w = mount(LogLines, { props: { content, minLevel: "DEBUG" } });
    expect(w.findAll(".log-line").length).toBe(3);
    expect(w.find(".lvl-DEBUG").exists()).toBe(true);
    expect(w.find(".lvl-WARN").exists()).toBe(true);
  });

  it("hides below-threshold structured lines but keeps raw lines", () => {
    const w = mount(LogLines, { props: { content, minLevel: "WARN" } });
    // WARN line + raw line remain; DEBUG line filtered out
    expect(w.find(".lvl-DEBUG").exists()).toBe(false);
    expect(w.find(".lvl-WARN").exists()).toBe(true);
    expect(w.text()).toContain("raw continuation line");
  });

  it("shows the tag for structured lines", () => {
    const w = mount(LogLines, { props: { content, minLevel: "DEBUG" } });
    expect(w.text()).toContain("[pty-input]");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/components/LogLines.test.ts`
Expected: FAIL — component not found.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/components/LogLines.vue`:

```vue
<script lang="ts" setup>
import { computed } from "vue";
import { parseLogLine, levelAtLeast, type LogLevel } from "../lib/parseLogLine";

const props = withDefaults(
  defineProps<{ content: string; minLevel?: LogLevel }>(),
  { minLevel: "DEBUG" },
);

const lines = computed(() => {
  const out = props.content.split("\n").map(parseLogLine);
  return out.filter(
    (l) => l.kind === "raw" || levelAtLeast(l.level, props.minLevel),
  );
});
</script>

<template>
  <div class="log-lines">
    <div
      v-for="(l, i) in lines"
      :key="i"
      class="log-line"
      :class="l.kind === 'structured' ? 'lvl-' + l.level : 'lvl-raw'"
    >
      <template v-if="l.kind === 'structured'">
        <span class="ts">{{ l.ts }}</span>
        <span class="lvl">{{ l.level }}</span>
        <span class="tag">[{{ l.tag }}]</span>
        <span class="msg">{{ l.msg }}</span>
      </template>
      <template v-else>
        <span class="raw">{{ l.text }}</span>
      </template>
    </div>
  </div>
</template>

<style scoped>
.log-lines {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
}
.log-line { display: block; }
.ts { color: var(--fg-dim); margin-right: 6px; }
.lvl { margin-right: 6px; font-weight: 700; }
.tag { color: var(--accent); margin-right: 6px; }
.msg { color: var(--fg); }
.raw { color: var(--fg-dim); }
.lvl-DEBUG .lvl { color: var(--fg-dim); }
.lvl-DEBUG .msg { color: var(--fg-dim); }
.lvl-INFO .lvl { color: var(--fg); }
.lvl-WARN .lvl,
.lvl-WARN .msg { color: var(--warn, #d2a86a); }
.lvl-ERROR .lvl,
.lvl-ERROR .msg { color: var(--bad); }
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/components/LogLines.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/LogLines.vue desktop/frontend/src/components/LogLines.test.ts
git commit -m "feat(logging-ui): LogLines colorized + level-filtered renderer

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Wire `LogLines` + level filter into both viewers + i18n

**Files:**
- Modify: `desktop/frontend/src/components/SettingsLogging.vue`
- Modify: `desktop/frontend/src/components/LogViewerDialog.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

**Interfaces:**
- Consumes: `LogLines.vue`, `LogLevel` (Tasks 3-4).

- [ ] **Step 1: Add i18n keys (en)**

In `desktop/frontend/src/i18n/messages/en.ts`, inside the `logging: { ... }` block (around line 222), add:

```ts
      levelFilter: "Level",
```

- [ ] **Step 2: Add i18n keys (zh-CN)**

In `desktop/frontend/src/i18n/messages/zh-CN.ts`, inside the matching `logging: { ... }` block, add:

```ts
      levelFilter: "级别",
```

- [ ] **Step 3: Integrate into `LogViewerDialog.vue`**

Add to the `<script setup>` imports:
```ts
import LogLines from "./LogLines.vue";
import type { LogLevel } from "../lib/parseLogLine";
```
Add reactive state (after `const { t } = useI18n();`):
```ts
const minLevel = ref<LogLevel>("DEBUG");
```
(ensure `ref` is imported from `vue` — it already imports `ref`.)

In the template, replace:
```html
        <pre ref="contentEl" class="content">{{ props.preview.content }}</pre>
```
with:
```html
        <LogLines ref="contentEl" class="content" :content="props.preview.content" :minLevel="minLevel" />
```
Add a level `<select>` into the `.row` toolbar (before the refresh button):
```html
        <label class="lvl-filter">{{ t("settings.logging.levelFilter") }}
          <select v-model="minLevel">
            <option value="DEBUG">DEBUG+</option>
            <option value="INFO">INFO+</option>
            <option value="WARN">WARN+</option>
            <option value="ERROR">ERROR</option>
          </select>
        </label>
```
Note: `contentEl` is used for auto-scroll via `el.scrollTop`. Since it's now a component ref, change `scrollToBottom` to scroll the root element:
```ts
async function scrollToBottom() {
  await nextTick();
  const el = (contentEl.value as any)?.$el as HTMLElement | undefined;
  if (el) el.scrollTop = el.scrollHeight;
}
```
Add `.lvl-filter` style (small, inline) to the `<style scoped>`:
```css
.lvl-filter { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--fg-dim); margin-right: auto; }
.lvl-filter select { height: 26px; background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 6px; }
```

- [ ] **Step 4: Integrate into `SettingsLogging.vue`**

Add to imports:
```ts
import LogLines from "./LogLines.vue";
import type { LogLevel } from "../lib/parseLogLine";
```
Add state near the other tail refs:
```ts
const tailMinLevel = ref<LogLevel>("DEBUG");
```
In the tail header (`.tail-header`), add the filter before the refresh button:
```html
        <select v-model="tailMinLevel" class="tail-level">
          <option value="DEBUG">DEBUG+</option>
          <option value="INFO">INFO+</option>
          <option value="WARN">WARN+</option>
          <option value="ERROR">ERROR</option>
        </select>
```
Replace the tail `<pre>`:
```html
        <pre v-else ref="tailEl" class="tail-content">{{ tail.content }}</pre>
```
with:
```html
        <LogLines v-else ref="tailEl" class="tail-content" :content="tail.content" :minLevel="tailMinLevel" />
```
Update the auto-scroll in `refreshTail` to handle the component ref:
```ts
  await nextTick();
  const el = (tailEl.value as any)?.$el as HTMLElement | undefined;
  if (el) el.scrollTop = el.scrollHeight;
```
(Change the `tailEl` type to `ref<any>(null)` so `.$el` is reachable, or keep the cast above.)
Add a `.tail-level` style:
```css
.tail-level { height: 24px; background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 6px; font-size: 12px; }
```

- [ ] **Step 5: Run the frontend test suite + typecheck**

Run: `cd desktop/frontend && npx vitest run src/components/LogViewerDialog.test.ts src/components/SettingsDialog.test.ts && npx vue-tsc --noEmit`
Expected: PASS / no type errors. (If `LogViewerDialog.test.ts` asserts the old `props.preview.content` `<pre>` string, update that assertion to expect the `LogLines` usage.)

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/SettingsLogging.vue desktop/frontend/src/components/LogViewerDialog.vue desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts desktop/frontend/src/components/LogViewerDialog.test.ts
git commit -m "feat(logging-ui): colorized + level-filtered log viewers

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: PTY input debug toggle UI (api.ts + SettingsLogging toggle + i18n)

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`
- Modify: `desktop/frontend/src/components/SettingsLogging.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

**Interfaces:**
- Consumes: Go `GetPtyInputDebugEnabled` / `SetPtyInputDebugEnabled` (Task 2).
- Produces: `getPtyInputDebugEnabled()` / `setPtyInputDebugEnabled(enabled)` shims.

- [ ] **Step 1: Add api.ts bindings**

In `desktop/frontend/src/lib/api.ts`, add to the `AppBindings` interface (next to `GetNotificationsEnabled` ~line 271):
```ts
  GetPtyInputDebugEnabled(): Promise<boolean>;
  SetPtyInputDebugEnabled(enabled: boolean): Promise<void>;
```
Add exported wrappers (next to `getNotificationsEnabled` ~line 571):
```ts
export function getPtyInputDebugEnabled(): Promise<boolean> {
  return bindings().GetPtyInputDebugEnabled();
}

export function setPtyInputDebugEnabled(enabled: boolean): Promise<void> {
  return bindings().SetPtyInputDebugEnabled(enabled);
}
```

- [ ] **Step 2: Add i18n keys**

`en.ts` `logging` block:
```ts
      ptyInputDebug: "Log terminal input bytes (diagnose stuck/dropped input)",
```
`zh-CN.ts` `logging` block:
```ts
      ptyInputDebug: "记录终端输入字节（排查输入丢失/卡死）",
```

- [ ] **Step 3: Add the toggle to `SettingsLogging.vue`**

Add to imports:
```ts
import { getPtyInputDebugEnabled, setPtyInputDebugEnabled } from "../lib/api";
```
(merge into the existing `../lib/api` import.)
Add state:
```ts
const ptyInputDebug = ref(false);
```
In `onMounted`, after loading logging config, load the toggle:
```ts
  try {
    ptyInputDebug.value = await getPtyInputDebugEnabled();
  } catch {
    /* leave default false */
  }
```
Add a handler:
```ts
async function onTogglePtyInputDebug(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = ptyInputDebug.value;
  ptyInputDebug.value = target.checked;
  try {
    await setPtyInputDebugEnabled(target.checked);
  } catch (err: any) {
    ptyInputDebug.value = previous;
    error.value = err?.message ?? String(err);
  }
}
```
In the template, add a checkbox under the existing "write logs" checkbox:
```html
      <label class="checkbox">
        <input type="checkbox" :checked="ptyInputDebug" @change="onTogglePtyInputDebug" />
        {{ t("settings.logging.ptyInputDebug") }}
      </label>
```

- [ ] **Step 4: Typecheck + tests**

Run: `cd desktop/frontend && npx vue-tsc --noEmit && npx vitest run src/components/SettingsDialog.test.ts`
Expected: no type errors; tests pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/api.ts desktop/frontend/src/components/SettingsLogging.vue desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "feat(desktop): PTY input debug toggle in logging settings

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Full verification pass

**Files:** none (verification only)

- [ ] **Step 1: Go tests + build**

Run: `cd desktop && go test ./... && go build ./... && cd .. && go build ./...`
Expected: all pass / clean.

- [ ] **Step 2: Frontend tests + typecheck**

Run: `cd desktop/frontend && npx vitest run && npx vue-tsc --noEmit`
Expected: all pass / no type errors.

- [ ] **Step 3: Manual smoke (documented, run by the user on a build)**

In a dev/build run: open Settings → Logging, toggle "Log terminal input bytes" on, type in a session, confirm `DEBUG [pty-input] write ...` lines appear colorized in the live tail; switch the level filter to `WARN+` and confirm DEBUG lines hide. Confirm the log file on disk contains the same lines as plain text (no ANSI).

- [ ] **Step 4: Final confirmation**

No commit (no code change). Report results.

---

## Self-Review

**Spec coverage:**
- Log line format → Task 1 (`formatLogLine`). ✓
- loggingManager owns format + Emit + helpers + legacy normalization + `SetFlags(0)` → Task 1. ✓
- Revert `ATTERM_PTY_DEBUG` temp code → Task 2 Step 1. ✓
- Config `PtyInputDebugEnabled` + default false + accessor → Task 2. ✓
- Wails Get/Set → Task 2. ✓
- `desktopPtyHost.Write` gated logging + pass cfg → Task 2. ✓
- `parseLogLine` + level order → Task 3. ✓
- `LogLines.vue` colorize + filter (raw lines always shown) → Task 4. ✓
- Integrate into `SettingsLogging` tail + `LogViewerDialog` + level filter in BOTH → Task 5. ✓
- PTY toggle UI + api.ts shims + i18n (en + zh) → Task 6. ✓
- File plain-text / no ANSI / same file / input-only → enforced by design (Emit writes plain; logPtyInput input-only; reuses desktop.log). ✓

**Type consistency:** `LogLevel`, `ParsedLogLine`, `parseLogLine`, `levelAtLeast`, `LEVEL_ORDER` consistent across Tasks 3-6. Go `ptyInputDebugTag`, `logPtyInput`, `PtyInputDebugEnabledOrDefault`, `Emit`, `formatLogLine` consistent across Tasks 1-2. ✓

**Placeholder scan:** no TBD/TODO; every code step shows concrete code. ✓
