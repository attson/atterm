# Desktop Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add default-on desktop log persistence with user-configurable file path, size-based rotation, runtime enable/disable control, and an in-app log viewer.

**Architecture:** A desktop-local logging manager owns `log.SetOutput`, path selection, sink switching, and file rotation. Go exposes separate logging settings and log-preview Wails bindings; the Vue Settings dialog adds a logging section plus a lightweight read-only log viewer modal.

**Tech Stack:** Go standard `log` package, Wails v2 runtime dialogs, Vue 3 + TypeScript desktop frontend, Go unit tests, Vitest source-level component tests.

---

## File Structure

- Modify `desktop/config.go`: add persisted logging settings and default helpers.
- Create `desktop/config_logging_test.go`: cover logging config defaults and custom-path preservation.
- Create `desktop/logging.go`: implement the logging manager, rotating file writer, preview reader, and platform-default path logic.
- Create `desktop/logging_test.go`: cover sink selection, rotation, and preview behavior.
- Modify `desktop/main.go`: initialize desktop logging before `wails.Run(...)`.
- Modify `desktop/app.go`: expose logging Wails bindings, wire runtime reconfiguration, and use native file picker support.
- Create `desktop/app_logging_test.go`: cover persistence, invalid-path handling, and log-preview API behavior.
- Modify `desktop/frontend/src/lib/api.ts`: add logging config, picker, and preview wrappers.
- Create `desktop/frontend/src/components/LogViewerDialog.vue`: render a read-only recent-log modal with refresh/copy actions.
- Create `desktop/frontend/src/components/LogViewerDialog.test.ts`: source-level tests for the viewer UI and controls.
- Modify `desktop/frontend/src/components/SettingsDialog.vue`: add a logging section and open the log viewer.
- Modify `desktop/frontend/src/components/SettingsDialog.test.ts`: source-level tests for logging controls and viewer wiring.

---

### Task 1: Persist Logging Settings And Defaults

**Files:**
- Create: `desktop/config_logging_test.go`
- Modify: `desktop/config.go`

- [ ] **Step 1: Write the failing config tests**

Create `desktop/config_logging_test.go`:

```go
package main

import "testing"

func TestLogToFileEnabledOrDefault(t *testing.T) {
	if got := (appConfig{}).LogToFileEnabledOrDefault(); !got {
		t.Fatalf("LogToFileEnabledOrDefault() = false; want true")
	}

	enabled := true
	if got := (appConfig{LogToFileEnabled: &enabled}).LogToFileEnabledOrDefault(); !got {
		t.Fatalf("LogToFileEnabledOrDefault(true) = false; want true")
	}

	disabled := false
	if got := (appConfig{LogToFileEnabled: &disabled}).LogToFileEnabledOrDefault(); got {
		t.Fatalf("LogToFileEnabledOrDefault(false) = true; want false")
	}
}

func TestLogFilePathOrDefaultPreservesCustomPath(t *testing.T) {
	cfg := appConfig{LogFilePath: "/tmp/custom-atterm.log"}
	if got := cfg.LogFilePathOrDefault(); got != "/tmp/custom-atterm.log" {
		t.Fatalf("LogFilePathOrDefault() = %q; want %q", got, "/tmp/custom-atterm.log")
	}
}

func TestDefaultLogFilePathIsNotEmpty(t *testing.T) {
	if got := defaultLogFilePath(); got == "" {
		t.Fatal("defaultLogFilePath() = empty; want platform default")
	}
}
```

- [ ] **Step 2: Run the config tests and verify they fail**

Run:

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test -tags webkit2_41 ./desktop/ -run 'TestLog(ToFileEnabledOrDefault|FilePathOrDefaultPreservesCustomPath)|TestDefaultLogFilePathIsNotEmpty' -count=1
```

Expected: FAIL because the logging config fields and helpers do not exist yet.

- [ ] **Step 3: Add logging fields and default helpers**

Patch `desktop/config.go`:

```go
	// LogToFileEnabled controls whether desktop logs are persisted to a file.
	// Nil means "never set" and defaults to true for existing installs.
	LogToFileEnabled *bool `json:"log_to_file_enabled,omitempty"`
	// LogFilePath overrides the platform default desktop log file path.
	// Empty means "use the platform default".
	LogFilePath string `json:"log_file_path,omitempty"`
```

Add the helpers below `TerminalThemeOrDefault()`:

```go
func (c appConfig) LogToFileEnabledOrDefault() bool {
	if c.LogToFileEnabled == nil {
		return true
	}
	return *c.LogToFileEnabled
}

func (c appConfig) LogFilePathOrDefault() string {
	if c.LogFilePath != "" {
		return c.LogFilePath
	}
	return defaultLogFilePath()
}
```

Declare `defaultLogFilePath()` in `desktop/logging.go` during Task 2; for now reference it from the config helper.

- [ ] **Step 4: Run the config tests and verify they pass**

Run:

```bash
go test -tags webkit2_41 ./desktop/ -run 'TestLog(ToFileEnabledOrDefault|FilePathOrDefaultPreservesCustomPath)|TestDefaultLogFilePathIsNotEmpty' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/config.go desktop/config_logging_test.go
git commit -m "add desktop logging config defaults"
```

---

### Task 2: Build The Logging Manager And Rotation

**Files:**
- Create: `desktop/logging.go`
- Create: `desktop/logging_test.go`

- [ ] **Step 1: Write the failing logging-manager tests**

Create `desktop/logging_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggingManagerDevModeMirrorsToTerminalAndFile(t *testing.T) {
	dir := t.TempDir()
	var terminal bytes.Buffer
	m, err := newLoggingManager(loggingOptions{
		devMode:       true,
		terminal:      &terminal,
		maxBytes:      1024,
		maxBackups:    2,
		defaultPathFn: func() string { return filepath.Join(dir, "desktop.log") },
	})
	if err != nil {
		t.Fatalf("newLoggingManager() error = %v", err)
	}
	defer m.Close()

	if err := m.Apply(loggingConfigState{enabled: true, path: ""}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	m.Logger().Print("hello dev logging")

	data, err := os.ReadFile(filepath.Join(dir, "desktop.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "hello dev logging") {
		t.Fatalf("file log missing line: %s", data)
	}
	if !strings.Contains(terminal.String(), "hello dev logging") {
		t.Fatalf("terminal log missing line: %s", terminal.String())
	}
}

func TestLoggingManagerRotatesAtSizeLimit(t *testing.T) {
	dir := t.TempDir()
	m, err := newLoggingManager(loggingOptions{
		devMode:       false,
		terminal:      &bytes.Buffer{},
		maxBytes:      64,
		maxBackups:    2,
		defaultPathFn: func() string { return filepath.Join(dir, "desktop.log") },
	})
	if err != nil {
		t.Fatalf("newLoggingManager() error = %v", err)
	}
	defer m.Close()

	if err := m.Apply(loggingConfigState{enabled: true}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	m.Logger().Print(strings.Repeat("a", 80))
	m.Logger().Print(strings.Repeat("b", 80))
	m.Logger().Print(strings.Repeat("c", 80))

	if _, err := os.Stat(filepath.Join(dir, "desktop.log.1")); err != nil {
		t.Fatalf("expected rotated file .1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "desktop.log.2")); err != nil {
		t.Fatalf("expected rotated file .2: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "desktop.log.3")); !os.IsNotExist(err) {
		t.Fatalf("expected no file .3; got err=%v", err)
	}
}

func TestLogPreviewReturnsTailAndTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desktop.log")
	content := strings.Repeat("0123456789", 40)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	preview, err := readLogPreview(path, 32)
	if err != nil {
		t.Fatalf("readLogPreview() error = %v", err)
	}
	if !preview.Exists || !preview.Truncated {
		t.Fatalf("preview = %#v; want exists and truncated", preview)
	}
	if len(preview.Content) == 0 || len(preview.Content) > 32 {
		t.Fatalf("preview content length = %d; want 1..32", len(preview.Content))
	}
}
```

- [ ] **Step 2: Run the logging-manager tests and verify they fail**

Run:

```bash
go test -tags webkit2_41 ./desktop/ -run 'TestLoggingManager|TestLogPreviewReturnsTailAndTruncation' -count=1
```

Expected: FAIL because `newLoggingManager`, `loggingOptions`, `loggingConfigState`, and `readLogPreview` do not exist.

- [ ] **Step 3: Implement the logging manager**

Create `desktop/logging.go` with:

```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const (
	defaultLogPreviewBytes = 256 * 1024
	defaultLogMaxBytes     = 10 * 1024 * 1024
	defaultLogMaxBackups   = 5
)

type loggingConfigState struct {
	enabled bool
	path    string
}

type loggingOptions struct {
	devMode       bool
	terminal      io.Writer
	maxBytes      int64
	maxBackups    int
	defaultPathFn func() string
}

type loggingManager struct {
	mu            sync.Mutex
	logger        *log.Logger
	devMode       bool
	terminal      io.Writer
	maxBytes      int64
	maxBackups    int
	defaultPathFn func() string
	currentPath   string
	currentFile   *os.File
	currentWriter io.Writer
}

func newLoggingManager(opts loggingOptions) (*loggingManager, error) { /* ... */ }
func (m *loggingManager) Logger() *log.Logger { return m.logger }
func (m *loggingManager) Apply(cfg loggingConfigState) error { /* ... */ }
func (m *loggingManager) EffectivePath(path string) string { /* ... */ }
func (m *loggingManager) Close() error { /* ... */ }
func defaultLogFilePath() string { /* ... */ }
func readLogPreview(path string, limit int64) (logPreview, error) { /* ... */ }
```

Implement the actual behavior in this task:

- `defaultLogFilePath()` returns the platform-specific default path from the approved design.
- `Apply(...)` sets logger sinks for:
  - dev + enabled: terminal + rotating file
  - dev + disabled: terminal only
  - release + enabled: rotating file only
  - release + disabled: `io.Discard`
- The rotating file writer renames `.N` backups up to the configured limit before recreating the active file.
- `readLogPreview(...)` reads the last `limit` bytes and uses `bytes.ToValidUTF8` before returning text.

- [ ] **Step 4: Run the logging-manager tests and verify they pass**

Run:

```bash
go test -tags webkit2_41 ./desktop/ -run 'TestLoggingManager|TestLogPreviewReturnsTailAndTruncation' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/logging.go desktop/logging_test.go
git commit -m "add desktop log manager"
```

---

### Task 3: Wire Startup And Expose Logging Backend APIs

**Files:**
- Modify: `desktop/main.go`
- Modify: `desktop/app.go`
- Create: `desktop/app_logging_test.go`

- [ ] **Step 1: Write the failing backend API tests**

Create `desktop/app_logging_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newLoggingTestApp(t *testing.T, cfg appConfig) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	return &App{
		cfgStore: loadConfigStoreFrom(cfg),
		logger: newTestLoggingManager(t, filepath.Join(root, "logs", "desktop.log")),
	}
}

func TestGetLoggingConfigReturnsEffectiveDefaults(t *testing.T) {
	a := newLoggingTestApp(t, appConfig{})
	cfg := a.GetLoggingConfig()
	if !cfg.Enabled {
		t.Fatalf("Enabled = false; want true")
	}
	if cfg.EffectivePath == "" {
		t.Fatal("EffectivePath = empty; want platform default")
	}
}

func TestSetLoggingConfigPersistsAndReconfigures(t *testing.T) {
	a := newLoggingTestApp(t, appConfig{})
	customPath := filepath.Join(t.TempDir(), "custom.log")

	if err := a.SetLoggingConfig(LoggingConfig{
		Enabled: true,
		Path:    customPath,
	}); err != nil {
		t.Fatalf("SetLoggingConfig() error = %v", err)
	}

	cfg := a.cfgStore.Get()
	if cfg.LogFilePath != customPath {
		t.Fatalf("LogFilePath = %q; want %q", cfg.LogFilePath, customPath)
	}
}

func TestGetLogPreviewReturnsRecentContent(t *testing.T) {
	a := newLoggingTestApp(t, appConfig{})
	a.logger.Logger().Print("preview me")

	preview, err := a.GetLogPreview()
	if err != nil {
		t.Fatalf("GetLogPreview() error = %v", err)
	}
	if !strings.Contains(preview.Content, "preview me") {
		t.Fatalf("preview content missing log line: %q", preview.Content)
	}
}
```

- [ ] **Step 2: Run the backend API tests and verify they fail**

Run:

```bash
go test -tags webkit2_41 ./desktop/ -run 'Test(GetLoggingConfigReturnsEffectiveDefaults|SetLoggingConfigPersistsAndReconfigures|GetLogPreviewReturnsRecentContent)' -count=1
```

Expected: FAIL because `LoggingConfig`, `GetLoggingConfig`, `SetLoggingConfig`, `GetLogPreview`, and the logger wiring do not exist.

- [ ] **Step 3: Implement startup wiring and Wails logging methods**

Patch `desktop/main.go` so logging starts before `wails.Run(...)`:

```go
func main() {
	cfgStore := loadConfig()
	logger, err := newDesktopLoggingManager(cfgStore.Get(), Version)
	if err != nil {
		println("Error:", err.Error())
		return
	}
	defer logger.Close()

	app := NewApp(cfgStore, logger)
	// existing Wails setup...
}
```

Patch `desktop/app.go`:

```go
type LoggingConfig struct {
	Enabled       bool   `json:"enabled"`
	Path          string `json:"path"`
	EffectivePath string `json:"effective_path"`
	DevDualOutput bool   `json:"dev_dual_output"`
}

type LogPreview struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
}
```

Extend `App`:

```go
	logger *loggingManager
```

Add methods:

- `GetLoggingConfig() LoggingConfig`
- `SetLoggingConfig(req LoggingConfig) error`
- `PickLogFilePath() (string, error)` using `wailsruntime.SaveFileDialog(...)`
- `GetLogPreview() (LogPreview, error)`

Keep `RelayConfig` separate. `SetLoggingConfig` must:

- validate that `Path` is either empty or absolute
- persist logging settings without touching relay/theme/update fields
- call `a.logger.Apply(...)` only after persistence data is validated

Also add small test helpers in `desktop/app_logging_test.go`:

```go
func loadConfigStoreFrom(cfg appConfig) *configStore { return &configStore{cfg: cfg} }
func newTestLoggingManager(t *testing.T, path string) *loggingManager { /* create manager + Apply(enabled) */ }
```

- [ ] **Step 4: Run the backend API tests and verify they pass**

Run:

```bash
go test -tags webkit2_41 ./desktop/ -run 'Test(GetLoggingConfigReturnsEffectiveDefaults|SetLoggingConfigPersistsAndReconfigures|GetLogPreviewReturnsRecentContent)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/main.go desktop/app.go desktop/app_logging_test.go
git commit -m "wire desktop logging settings"
```

---

### Task 4: Add Frontend Logging API And Settings Controls

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Modify: `desktop/frontend/src/components/SettingsDialog.test.ts`

- [ ] **Step 1: Write the failing source-level Settings tests**

Extend `desktop/frontend/src/components/SettingsDialog.test.ts`:

```ts
describe("SettingsDialog logging controls", () => {
  test("loads logging settings and picker helpers", () => {
    expect(source).toContain("getLoggingConfig");
    expect(source).toContain("setLoggingConfig");
    expect(source).toContain("pickLogFilePath");
    expect(source).toContain("getLogPreview");
  });

  test("renders logging controls and viewer button", () => {
    expect(source).toContain("write logs to file");
    expect(source).toContain("change location");
    expect(source).toContain("reset default");
    expect(source).toContain("view logs");
  });
});
```

- [ ] **Step 2: Run the Settings tests and verify they fail**

Run:

```bash
cd desktop/frontend && npm run test -- SettingsDialog.test.ts
```

Expected: FAIL because the logging bindings and logging UI are not present.

- [ ] **Step 3: Add logging bindings and Settings state**

Patch `desktop/frontend/src/lib/api.ts`:

```ts
export interface LoggingConfig {
  enabled: boolean;
  path: string;
  effective_path: string;
  dev_dual_output: boolean;
}

export interface LogPreview {
  path: string;
  exists: boolean;
  truncated: boolean;
  content: string;
}
```

Add wrappers:

```ts
export function getLoggingConfig(): Promise<LoggingConfig> { /* ... */ }
export function setLoggingConfig(cfg: { enabled: boolean; path?: string }): Promise<void> { /* ... */ }
export function pickLogFilePath(): Promise<string> { /* ... */ }
export function getLogPreview(): Promise<LogPreview> { /* ... */ }
```

Patch `desktop/frontend/src/components/SettingsDialog.vue`:

- load logging config on mount alongside relay/update/theme settings
- add refs for `logToFileEnabled`, `logFilePath`, `effectiveLogFilePath`, `showLogViewer`
- add handlers:
  - `onLoggingToggle`
  - `onPickLogFilePath`
  - `onResetLogFilePath`
  - `openLogViewer`

Keep logging actions immediate-save, like terminal theme, not tied to `save & connect`.

- [ ] **Step 4: Run the Settings tests and verify they pass**

Run:

```bash
cd desktop/frontend && npm run test -- SettingsDialog.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/api.ts desktop/frontend/src/components/SettingsDialog.vue desktop/frontend/src/components/SettingsDialog.test.ts
git commit -m "add logging controls to settings"
```

---

### Task 5: Add The In-App Log Viewer

**Files:**
- Create: `desktop/frontend/src/components/LogViewerDialog.vue`
- Create: `desktop/frontend/src/components/LogViewerDialog.test.ts`
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`

- [ ] **Step 1: Write the failing log-viewer tests**

Create `desktop/frontend/src/components/LogViewerDialog.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import source from "./LogViewerDialog.vue?raw";

describe("LogViewerDialog", () => {
  test("renders refresh and copy actions", () => {
    expect(source).toContain("refresh");
    expect(source).toContain("copy");
  });

  test("shows readonly preview content", () => {
    expect(source).toContain("props.preview.content");
    expect(source).toContain("white-space: pre-wrap");
  });
});
```

- [ ] **Step 2: Run the log-viewer tests and verify they fail**

Run:

```bash
cd desktop/frontend && npm run test -- LogViewerDialog.test.ts
```

Expected: FAIL because the dialog component does not exist.

- [ ] **Step 3: Implement the log viewer dialog**

Create `desktop/frontend/src/components/LogViewerDialog.vue` with:

```vue
<script lang="ts" setup>
const props = defineProps<{
  path: string;
  exists: boolean;
  truncated: boolean;
  content: string;
  loading?: boolean;
  error?: string;
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "refresh"): void;
}>();

async function copyContent() {
  await navigator.clipboard.writeText(props.content);
}
</script>
```

Render:

- a title showing the effective path
- an error block when `error` is set
- a friendly empty-state message when `!exists`
- a truncated-note banner when `truncated`
- a read-only `<pre>` for `content`
- `refresh`, `copy`, and `close` buttons

Patch `SettingsDialog.vue` to:

- import `LogViewerDialog`
- store the latest `LogPreview`
- fetch preview content on `openLogViewer`
- refresh preview when the dialog emits `refresh`

- [ ] **Step 4: Run the log-viewer tests and verify they pass**

Run:

```bash
cd desktop/frontend && npm run test -- LogViewerDialog.test.ts SettingsDialog.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/LogViewerDialog.vue desktop/frontend/src/components/LogViewerDialog.test.ts desktop/frontend/src/components/SettingsDialog.vue
git commit -m "add in-app log viewer"
```

---

### Task 6: Full Verification And Generated Binding Check

**Files:**
- Modify: any touched files from Tasks 1-5

- [ ] **Step 1: Format Go files**

Run:

```bash
gofmt -w desktop/config.go desktop/logging.go desktop/main.go desktop/app.go desktop/config_logging_test.go desktop/logging_test.go desktop/app_logging_test.go
```

Expected: no output; files rewritten in place if needed.

- [ ] **Step 2: Run backend verification**

Run:

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test -tags webkit2_41 -timeout 60s ./desktop/
go vet -tags webkit2_41 ./...
```

Expected: PASS for both commands.

- [ ] **Step 3: Run frontend verification**

Run:

```bash
cd desktop/frontend
npm run test
npm run build
```

Expected: PASS; Wails frontend bindings remain compatible without manual edits to generated files.

- [ ] **Step 4: Run web regression verification**

Run:

```bash
cd /Users/attson/code/github.com.attson/atterm
node --test web/*.test.mjs
```

Expected: PASS; desktop logging changes must not regress the bundled web client.

- [ ] **Step 5: Manual smoke checks**

Run through:

```text
1. Start `wails dev`, open Settings, confirm file logging is enabled and the path is populated.
2. Trigger a terminal session and confirm the dev terminal plus log file both receive new lines.
3. Change the log path through the picker and confirm subsequent writes move to the new file.
4. Disable file logging and confirm new lines stop appearing in the file while dev-terminal logs still appear.
5. Re-enable logging, generate enough output to rotate, and confirm `.1` through `.5` behavior.
6. Open "view logs" and confirm recent content renders without opening Finder.
```

- [ ] **Step 6: Commit**

```bash
git add desktop docs/superpowers/plans/2026-05-13-desktop-logging.md
git commit -m "finish desktop logging controls"
```

---

## Self-Review

- Spec coverage: config persistence, platform defaults, default-on behavior, runtime enable/disable, native path picker, size-based rotation, dev-vs-release sink behavior, and in-app recent-log viewer all map to tasks above.
- Placeholder scan: no `TODO`/`TBD` markers remain; each task names exact files, commands, and expected outcomes.
- Type consistency: `LoggingConfig`, `LogPreview`, `log_to_file_enabled`, and `log_file_path` are named consistently across backend and frontend tasks.
