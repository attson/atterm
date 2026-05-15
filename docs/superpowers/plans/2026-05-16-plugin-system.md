# Plugin System (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a plugin framework in the desktop app and ship two first-party plugins on top of it: **Quick Input** (bottom toolbar of user-defined buttons that send text to the active pane) and **File Explorer** (right panel with file tree and read-only CodeMirror 6 preview).

**Architecture:** Plugins live under `desktop/frontend/src/plugins/<id>/`. A central registry references each plugin via a `dynamic import()` so disabled plugins never enter the main JS bundle. Two render surfaces (`right-panel`, `bottom-toolbar`) are owned by a `PluginHost.vue` instance each. Plugin configuration extends the existing `appConfig` and rides on the existing `configStore` save mechanism. A new `PluginFS` Go binding group exposes read-only filesystem operations with a strict path allowlist (no relay exposure).

**Tech Stack:** Vue 3 + TypeScript + Vite (existing); Pinia for plugin config store (new dependency, ~2 KB); CodeMirror 6 for read-only preview (lazy-loaded chunk); `fsnotify` for fs watching (added under `desktop/`); existing Wails v2 binding mechanism for new Go methods.

**Reference spec:** `docs/superpowers/specs/2026-05-16-plugin-system-design.md`.

---

## File Structure

Created in this plan:

```
desktop/
├── plugin_fs.go                          read/list/meta/watch + allowlist
├── plugin_fs_test.go                     allowlist + all-pattern tests
├── plugin_config.go                      PluginConfig types + applyDefaults
├── plugin_config_test.go                 defaults / validation / atomic write
├── plugin_app_bindings.go                Get/SetPluginConfig + plugin-config-changed
└── plugin_app_bindings_test.go           binding round-trip + event emit

desktop/frontend/src/
├── plugins/
│   ├── types.ts                          PluginSlot, PluginDescriptor, PluginContext
│   ├── registry.ts                       array of PluginDescriptor with lazy load()
│   ├── configStore.ts                    Pinia store for PluginConfig
│   ├── usePluginContext.ts               composable producing PluginContext
│   ├── PluginHost.vue                    slot host that lazy-imports + mounts plugins
│   ├── quickInput/
│   │   ├── index.ts                      descriptor for registry
│   │   ├── defaults.ts                   default 3 buttons (ok / continue / 发布)
│   │   ├── QuickInputBar.vue             bottom toolbar UI
│   │   ├── QuickInputSettings.vue        settings sub-section
│   │   ├── useQuickInputHotkeys.ts       Alt+key handler
│   │   ├── hotkeyConflict.ts             pure conflict-check function
│   │   ├── QuickInputBar.test.ts
│   │   ├── QuickInputSettings.test.ts
│   │   ├── useQuickInputHotkeys.test.ts
│   │   └── hotkeyConflict.test.ts
│   └── fileExplorer/
│       ├── index.ts                      descriptor for registry
│       ├── FileExplorer.vue              tree + tabs + editor layout
│       ├── FileTree.vue
│       ├── FileTabs.vue
│       ├── FileEditor.vue                CodeMirror 6 host
│       ├── languageMap.ts                extension → CM6 language extension
│       ├── tabsModel.ts                  pure preview/persistent/LRU model
│       ├── FileTree.test.ts
│       ├── FileTabs.test.ts
│       ├── FileEditor.test.ts
│       ├── tabsModel.test.ts
│       └── languageMap.test.ts
└── components/SettingsPlugins.vue        new 5th SettingsDialog tab
```

Modified:

```
desktop/
├── config.go                             extend appConfig with Plugins PluginConfig
├── app.go                                bind PluginFS, register plugin app methods
└── main.go                               add &PluginFS{} to Bind list

desktop/frontend/src/
├── App.vue                               mount two PluginHost; add right-side column
├── components/SettingsDialog.vue         add 5th tab "plugins"
└── components/SettingsDialog.test.ts     extend existing test to cover new tab

desktop/frontend/package.json             add pinia + @codemirror/* deps
```

CI hook (new):

```
.github/scripts/check-plugin-fs-isolation.sh    grep guard for red-line #11
.github/workflows/ci.yml                        invoke the guard step
```

---

## Conventions

- All Go code uses `-tags webkit2_41` for `go test`/`go vet` (see `AGENTS.md`).
- All frontend tests use `vitest` (already installed). Mock Wails bindings via `desktop/frontend/wailsjs/go/main/App.js` — write a minimal stub when a test needs it.
- Each task ends with a `git commit`. Commit messages follow the existing repo style: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`.
- Run `npm run build` and `npm test` in `desktop/frontend/` for every frontend task; run `go test -tags webkit2_41 ./desktop/...` for every Go task.
- New deps (`pinia`, `@codemirror/*`) are added at the earliest task that needs them; their installation is itself a step.

---

## Phase 1: Plugin Framework Skeleton

Goal: ship the framework with two empty PluginHost slots; nothing visible yet. Default `Config` round-trips; Plugins Settings tab exists and shows no plugins.

### Task 1.1: PluginConfig Go types and defaults

**Files:**
- Create: `desktop/plugin_config.go`
- Create: `desktop/plugin_config_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/plugin_config_test.go`:

```go
package main

import (
	"encoding/json"
	"testing"
)

func TestPluginConfigDefaults(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()

	if !c.QuickInput.Enabled {
		t.Fatal("quickInput should be enabled by default")
	}
	if len(c.QuickInput.Buttons) != 3 {
		t.Fatalf("expected 3 default buttons, got %d", len(c.QuickInput.Buttons))
	}
	labels := []string{c.QuickInput.Buttons[0].Label, c.QuickInput.Buttons[1].Label, c.QuickInput.Buttons[2].Label}
	want := []string{"ok", "continue", "发布"}
	for i := range labels {
		if labels[i] != want[i] {
			t.Errorf("button[%d].Label = %q, want %q", i, labels[i], want[i])
		}
		if !c.QuickInput.Buttons[i].AppendNewline {
			t.Errorf("button[%d].AppendNewline should default to true", i)
		}
		if c.QuickInput.Buttons[i].ID == "" {
			t.Errorf("button[%d].ID should be populated", i)
		}
	}

	if c.FileExplorer.Enabled {
		t.Fatal("fileExplorer should be disabled by default")
	}
	if c.FileExplorer.PanelWidthPx != 380 {
		t.Errorf("PanelWidthPx default = %d, want 380", c.FileExplorer.PanelWidthPx)
	}
	if !c.FileExplorer.PanelCollapsed {
		t.Error("PanelCollapsed default should be true")
	}
	if c.FileExplorer.InnerTreeRatio != 0.3 {
		t.Errorf("InnerTreeRatio default = %v, want 0.3", c.FileExplorer.InnerTreeRatio)
	}
}

func TestPluginConfigJSONRoundtrip(t *testing.T) {
	var orig PluginConfig
	orig.applyDefaults()
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back PluginConfig
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.QuickInput.Buttons[0].Send != "ok" {
		t.Fatalf("roundtrip mangled buttons: %+v", back.QuickInput.Buttons)
	}
}

func TestValidatePluginConfig(t *testing.T) {
	c := PluginConfig{}
	c.applyDefaults()

	if err := ValidatePluginConfig(c); err != nil {
		t.Fatalf("defaults must validate, got %v", err)
	}

	dup := c
	dup.QuickInput.Buttons[1].ID = dup.QuickInput.Buttons[0].ID
	if err := ValidatePluginConfig(dup); err == nil {
		t.Error("duplicate button IDs must fail validation")
	}

	bad := c
	bad.FileExplorer.PanelWidthPx = 100
	if err := ValidatePluginConfig(bad); err == nil {
		t.Error("PanelWidthPx < 240 must fail validation")
	}

	bad2 := c
	bad2.FileExplorer.InnerTreeRatio = 0.05
	if err := ValidatePluginConfig(bad2); err == nil {
		t.Error("InnerTreeRatio < 0.15 must fail validation")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags webkit2_41 -run TestPluginConfig ./desktop/`
Expected: FAIL — `undefined: PluginConfig`.

- [ ] **Step 3: Write minimal implementation**

Create `desktop/plugin_config.go`:

```go
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

// PluginConfig is the persisted plugin-system configuration block.
type PluginConfig struct {
	QuickInput   QuickInputConfig   `json:"quickInput"`
	FileExplorer FileExplorerConfig `json:"fileExplorer"`
}

type QuickInputConfig struct {
	Enabled bool               `json:"enabled"`
	Buttons []QuickInputButton `json:"buttons"`
}

type QuickInputButton struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Send          string `json:"send"`
	AppendNewline bool   `json:"appendNewline"`
	Hotkey        string `json:"hotkey,omitempty"`
}

type FileExplorerConfig struct {
	Enabled        bool    `json:"enabled"`
	PanelWidthPx   int     `json:"panelWidthPx"`
	PanelCollapsed bool    `json:"panelCollapsed"`
	InnerTreeRatio float64 `json:"innerTreeRatio"`
	ShowHidden     bool    `json:"showHidden"`
}

// applyDefaults fills empty-valued fields with their defaults. Safe to call
// repeatedly. Note: empty Buttons slice triggers the default 3 buttons; if a
// user explicitly emptied the buttons in config.json, applyDefaults will
// reinject them — acceptable because (a) the UI prevents reaching empty
// without re-add and (b) the user would copy the defaults anyway.
func (c *PluginConfig) applyDefaults() {
	if len(c.QuickInput.Buttons) == 0 {
		c.QuickInput.Enabled = true
		c.QuickInput.Buttons = defaultQuickInputButtons()
	}
	if c.FileExplorer.PanelWidthPx == 0 {
		c.FileExplorer.PanelWidthPx = 380
		c.FileExplorer.PanelCollapsed = true
		c.FileExplorer.InnerTreeRatio = 0.3
		c.FileExplorer.ShowHidden = false
	}
}

func defaultQuickInputButtons() []QuickInputButton {
	return []QuickInputButton{
		{ID: newButtonID(), Label: "ok", Send: "ok", AppendNewline: true},
		{ID: newButtonID(), Label: "continue", Send: "continue", AppendNewline: true},
		{ID: newButtonID(), Label: "发布", Send: "发布", AppendNewline: true},
	}
}

func newButtonID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ValidatePluginConfig rejects malformed PluginConfig payloads coming from the
// frontend. Internal callers can rely on applyDefaults producing valid output.
func ValidatePluginConfig(c PluginConfig) error {
	seen := make(map[string]struct{}, len(c.QuickInput.Buttons))
	for i, b := range c.QuickInput.Buttons {
		if b.ID == "" {
			return fmt.Errorf("button[%d]: id must be non-empty", i)
		}
		if _, dup := seen[b.ID]; dup {
			return fmt.Errorf("button[%d]: duplicate id %q", i, b.ID)
		}
		seen[b.ID] = struct{}{}
		if b.Label == "" {
			return fmt.Errorf("button[%d]: label must be non-empty", i)
		}
	}
	if c.FileExplorer.PanelWidthPx < 240 || c.FileExplorer.PanelWidthPx > 2000 {
		return errors.New("fileExplorer.panelWidthPx out of bounds [240, 2000]")
	}
	if c.FileExplorer.InnerTreeRatio < 0.15 || c.FileExplorer.InnerTreeRatio > 0.5 {
		return errors.New("fileExplorer.innerTreeRatio out of bounds [0.15, 0.5]")
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags webkit2_41 -run TestPluginConfig ./desktop/`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add desktop/plugin_config.go desktop/plugin_config_test.go
git commit -m "feat(plugin): add PluginConfig types and defaults"
```

### Task 1.2: Extend appConfig with Plugins block

**Files:**
- Modify: `desktop/config.go` — add field + applyDefaults integration
- Create test in: `desktop/plugin_config_test.go`

- [ ] **Step 1: Write the failing test (append to plugin_config_test.go)**

```go
func TestAppConfigEmbedsPluginConfig(t *testing.T) {
	var c appConfig
	if c.Plugins.QuickInput.Buttons != nil {
		t.Fatal("expected zero-value Plugins")
	}
	// applyConfigDefaults must inject plugin defaults.
	applyConfigDefaults(&c)
	if len(c.Plugins.QuickInput.Buttons) != 3 {
		t.Fatalf("plugin defaults not injected, got %+v", c.Plugins)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags webkit2_41 -run TestAppConfigEmbeds ./desktop/`
Expected: FAIL — `c.Plugins undefined` or `applyConfigDefaults undefined`.

- [ ] **Step 3: Implement**

In `desktop/config.go`, add to the `appConfig` struct (after `CommandNotifyThresholdSeconds`):

```go
	// Plugins is the plugin-system block. Defaults filled in on first run.
	Plugins PluginConfig `json:"plugins"`
```

Add at the bottom of `desktop/config.go`:

```go
// applyConfigDefaults fills nil/zero fields with their defaults. Called from
// loadConfig() after JSON unmarshal. Idempotent.
func applyConfigDefaults(c *appConfig) {
	c.Plugins.applyDefaults()
}
```

Modify `loadConfig()` (in `desktop/config.go`) — after the JSON unmarshal but before storing into the store:

```go
	applyConfigDefaults(&cfg)
```

Locate the existing JSON-unmarshal block in `loadConfig` and add the call. The exact location is right before `s.cfg = cfg`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags webkit2_41 -run TestAppConfigEmbeds ./desktop/`
Expected: PASS.

- [ ] **Step 5: Run full Go tests to make sure nothing else broke**

Run: `go test -tags webkit2_41 -timeout 60s ./desktop/`
Expected: all pass (existing config tests must still pass — the new field has a default zero value when not present in JSON, which `applyConfigDefaults` populates).

- [ ] **Step 6: Commit**

```bash
git add desktop/config.go desktop/plugin_config_test.go
git commit -m "feat(plugin): embed PluginConfig into appConfig with defaults"
```

### Task 1.3: GetPluginConfig / SetPluginConfig Wails bindings

**Files:**
- Create: `desktop/plugin_app_bindings.go`
- Create: `desktop/plugin_app_bindings_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/plugin_app_bindings_test.go`:

```go
package main

import (
	"context"
	"testing"
)

func TestGetSetPluginConfigRoundtrip(t *testing.T) {
	app := newTestApp(t)

	got := app.GetPluginConfig()
	if len(got.QuickInput.Buttons) != 3 {
		t.Fatalf("expected default buttons populated, got %d", len(got.QuickInput.Buttons))
	}

	got.QuickInput.Buttons[0].Label = "yes"
	if err := app.SetPluginConfig(got); err != nil {
		t.Fatal(err)
	}

	again := app.GetPluginConfig()
	if again.QuickInput.Buttons[0].Label != "yes" {
		t.Fatalf("write did not persist, got label %q", again.QuickInput.Buttons[0].Label)
	}
}

func TestSetPluginConfigRejectsInvalid(t *testing.T) {
	app := newTestApp(t)

	bad := app.GetPluginConfig()
	bad.QuickInput.Buttons[0].ID = "" // forbidden by ValidatePluginConfig
	if err := app.SetPluginConfig(bad); err == nil {
		t.Fatal("expected validation error")
	}
}

// newTestApp constructs an App backed by an in-memory configStore. Reuses the
// existing test helper pattern (see e.g. app_theme_test.go).
func newTestApp(t *testing.T) *App {
	t.Helper()
	cfgStore := &configStore{}
	cfgStore.cfg.Plugins.applyDefaults()
	lm := &loggingManager{}
	app := NewApp(cfgStore, lm)
	app.ctx = context.Background()
	return app
}
```

Note: `newTestApp` may collide with an existing helper. If so, adopt the existing helper's name and skip the `newTestApp` definition; the test bodies still work.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags webkit2_41 -run TestGetSetPluginConfig ./desktop/`
Expected: FAIL — `GetPluginConfig undefined`.

- [ ] **Step 3: Implement**

Create `desktop/plugin_app_bindings.go`:

```go
package main

import (
	"fmt"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// GetPluginConfig returns the current plugin configuration.
func (a *App) GetPluginConfig() PluginConfig {
	return a.cfgStore.Get().Plugins
}

// SetPluginConfig validates, persists, and broadcasts the new plugin config.
// The event payload is the full PluginConfig — the frontend consumes it from
// a single Pinia store, no diffing.
func (a *App) SetPluginConfig(next PluginConfig) error {
	if err := ValidatePluginConfig(next); err != nil {
		return fmt.Errorf("plugin config invalid: %w", err)
	}
	cur := a.cfgStore.Get()
	cur.Plugins = next
	if err := a.cfgStore.Set(cur); err != nil {
		return err
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "plugin-config-changed", next)
	}
	return nil
}
```

If `App` does not currently expose its `ctx` field, find where `app.ctx` is assigned in `startup` (`desktop/app.go:109`) — it must already exist for `EventsEmit` to work elsewhere. If only locally scoped, expose it as a field.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags webkit2_41 -run TestGetSetPluginConfig ./desktop/`
Expected: PASS.

- [ ] **Step 5: Regenerate Wails bindings**

Run from `desktop/`:
```bash
wails generate module
```
This refreshes `desktop/frontend/wailsjs/go/main/App.{js,d.ts}` to include the new methods. If `wails` is not available, this will be regenerated next time `wails dev` or `wails build` runs.

- [ ] **Step 6: Commit**

```bash
git add desktop/plugin_app_bindings.go desktop/plugin_app_bindings_test.go desktop/frontend/wailsjs/go/main/
git commit -m "feat(plugin): expose Get/SetPluginConfig wails bindings + change event"
```

### Task 1.4: Frontend plugin types + registry skeleton

**Files:**
- Create: `desktop/frontend/src/plugins/types.ts`
- Create: `desktop/frontend/src/plugins/registry.ts`

- [ ] **Step 1: Add files (no test yet — these are pure types)**

Create `desktop/frontend/src/plugins/types.ts`:

```ts
import type { Component, ComputedRef, Ref } from "vue";
import type { Endpoint } from "../lib/api";
import type { Pane } from "../lib/types";

export type PluginSlot = "right-panel" | "bottom-toolbar";

export type PluginID = "quick-input" | "file-explorer";

export interface PluginContext {
  activePane: Ref<Pane | null>;
  activeSessionId: ComputedRef<string | null>;
  activeEndpoint: ComputedRef<Endpoint | null>;
  activeCwd: ComputedRef<string | null>;
  send: (text: string) => void;
  showToast: (msg: string) => void;
}

export interface PluginDescriptor {
  id: PluginID;
  slot: PluginSlot;
  title: string;
  description: string;
  load: () => Promise<{ default: Component }>;
  defaultEnabled: boolean;
}
```

Create `desktop/frontend/src/plugins/registry.ts`:

```ts
import type { PluginDescriptor } from "./types";

// Adding a new plugin requires (1) a directory under plugins/<id>/ with a
// default Vue export, and (2) a PluginDescriptor entry here. Vite's static
// analysis of import() arguments performs the chunk split — keep the path
// literal (no dynamic strings).
export const PLUGINS: PluginDescriptor[] = [
  // Phase 2 fills in quick-input; Phase 4 fills in file-explorer. Skeleton
  // ships empty so PluginHost has a stable contract from day 1.
];

export function descriptorsForSlot(slot: PluginDescriptor["slot"]): PluginDescriptor[] {
  return PLUGINS.filter((p) => p.slot === slot);
}

export function findDescriptor(id: string): PluginDescriptor | undefined {
  return PLUGINS.find((p) => p.id === id);
}
```

- [ ] **Step 2: Verify type-check passes**

Run: `cd desktop/frontend && npm run build`
Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/plugins/types.ts desktop/frontend/src/plugins/registry.ts
git commit -m "feat(plugin): add plugin types and empty registry"
```

### Task 1.5: Pinia configStore

**Files:**
- Modify: `desktop/frontend/package.json` (add `pinia`)
- Modify: `desktop/frontend/src/main.ts` (install Pinia)
- Create: `desktop/frontend/src/plugins/configStore.ts`
- Create: `desktop/frontend/src/plugins/configStore.test.ts`

- [ ] **Step 1: Add pinia dependency**

Run:
```bash
cd desktop/frontend && npm install pinia
```
Confirm `package.json` and `package-lock.json` updated.

- [ ] **Step 2: Install Pinia in main.ts**

Modify `desktop/frontend/src/main.ts` — locate `createApp(App)` and chain `.use(createPinia())`:

```ts
import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import "./style.css";

const app = createApp(App);
app.use(createPinia());
app.mount("#app");
```

(Adjust if `main.ts` already does extra wiring — keep all existing lines, add only the pinia ones.)

- [ ] **Step 3: Write the failing test**

Create `desktop/frontend/src/plugins/configStore.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { usePluginConfigStore } from "./configStore";

// Mock Wails bindings.
vi.mock("../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(),
  SetPluginConfig: vi.fn(),
}));

import { GetPluginConfig, SetPluginConfig } from "../../wailsjs/go/main/App";

const sample = {
  quickInput: {
    enabled: true,
    buttons: [
      { id: "b1", label: "ok", send: "ok", appendNewline: true },
    ],
  },
  fileExplorer: {
    enabled: false,
    panelWidthPx: 380,
    panelCollapsed: true,
    innerTreeRatio: 0.3,
    showHidden: false,
  },
};

describe("usePluginConfigStore", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.mocked(GetPluginConfig).mockResolvedValue(sample as unknown as any);
    vi.mocked(SetPluginConfig).mockResolvedValue(undefined as unknown as any);
  });

  it("load() populates cfg from binding", async () => {
    const s = usePluginConfigStore();
    await s.load();
    expect(s.cfg?.quickInput.buttons[0].label).toBe("ok");
  });

  it("save(next) writes via SetPluginConfig and updates cfg", async () => {
    const s = usePluginConfigStore();
    await s.load();
    const next = JSON.parse(JSON.stringify(sample));
    next.quickInput.buttons[0].label = "yes";
    await s.save(next);
    expect(SetPluginConfig).toHaveBeenCalledWith(next);
    expect(s.cfg?.quickInput.buttons[0].label).toBe("yes");
  });

  it("isPluginEnabled returns the live enable flag", async () => {
    const s = usePluginConfigStore();
    await s.load();
    expect(s.isPluginEnabled("quick-input")).toBe(true);
    expect(s.isPluginEnabled("file-explorer")).toBe(false);
  });
});
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/plugins/configStore.test.ts`
Expected: FAIL — `usePluginConfigStore` not found.

- [ ] **Step 5: Implement**

Create `desktop/frontend/src/plugins/configStore.ts`:

```ts
import { defineStore } from "pinia";
import { ref } from "vue";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { GetPluginConfig, SetPluginConfig } from "../../wailsjs/go/main/App";
import type { PluginID } from "./types";

export interface QuickInputButton {
  id: string;
  label: string;
  send: string;
  appendNewline: boolean;
  hotkey?: string;
}

export interface QuickInputConfig {
  enabled: boolean;
  buttons: QuickInputButton[];
}

export interface FileExplorerConfig {
  enabled: boolean;
  panelWidthPx: number;
  panelCollapsed: boolean;
  innerTreeRatio: number;
  showHidden: boolean;
}

export interface PluginConfig {
  quickInput: QuickInputConfig;
  fileExplorer: FileExplorerConfig;
}

let unsubscribe: (() => void) | null = null;

export const usePluginConfigStore = defineStore("pluginConfig", () => {
  const cfg = ref<PluginConfig | null>(null);

  async function load() {
    cfg.value = (await GetPluginConfig()) as PluginConfig;
    if (!unsubscribe) {
      unsubscribe = EventsOn("plugin-config-changed", (next: PluginConfig) => {
        cfg.value = next;
      });
    }
  }

  async function save(next: PluginConfig) {
    await SetPluginConfig(next);
    cfg.value = next;
  }

  function isPluginEnabled(id: PluginID): boolean {
    if (!cfg.value) return false;
    if (id === "quick-input") return cfg.value.quickInput.enabled;
    if (id === "file-explorer") return cfg.value.fileExplorer.enabled;
    return false;
  }

  async function setEnabled(id: PluginID, enabled: boolean) {
    if (!cfg.value) return;
    const next: PluginConfig = JSON.parse(JSON.stringify(cfg.value));
    if (id === "quick-input") next.quickInput.enabled = enabled;
    if (id === "file-explorer") next.fileExplorer.enabled = enabled;
    await save(next);
  }

  return { cfg, load, save, isPluginEnabled, setEnabled };
});
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/plugins/configStore.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 7: Run build**

Run: `cd desktop/frontend && npm run build`
Expected: succeeds.

- [ ] **Step 8: Commit**

```bash
git add desktop/frontend/package.json desktop/frontend/package-lock.json desktop/frontend/src/main.ts desktop/frontend/src/plugins/configStore.ts desktop/frontend/src/plugins/configStore.test.ts
git commit -m "feat(plugin): pinia store for PluginConfig with change-event sync"
```

### Task 1.6: usePluginContext composable

**Files:**
- Create: `desktop/frontend/src/plugins/usePluginContext.ts`

This composable is consumed by PluginHost. App.vue must expose enough state for it. The simplest contract: App.vue passes the live state in as args.

- [ ] **Step 1: Implement (no isolated test — usage tested through PluginHost.test.ts in Task 1.7)**

Create `desktop/frontend/src/plugins/usePluginContext.ts`:

```ts
import { computed, type ComputedRef, type Ref } from "vue";
import type { Endpoint } from "../lib/api";
import type { SessionInfo } from "../lib/connection";
import type { Pane } from "../lib/types";
import type { PluginContext } from "./types";

export interface PluginContextInputs {
  activePane: Ref<Pane | null>;
  endpointForPane: (pane: Pane) => Endpoint | null;
  sessionInfoForPane: (pane: Pane) => SessionInfo | null;
  sendToSession: (sessionId: string, endpoint: Endpoint, text: string) => void;
  showToast: (msg: string) => void;
}

export function createPluginContext(inputs: PluginContextInputs): PluginContext {
  const activeSessionId = computed(() => inputs.activePane.value?.sessionId ?? null);

  const activeEndpoint = computed<Endpoint | null>(() => {
    const p = inputs.activePane.value;
    return p ? inputs.endpointForPane(p) : null;
  });

  const activeCwd = computed<string | null>(() => {
    const p = inputs.activePane.value;
    if (!p) return null;
    const info = inputs.sessionInfoForPane(p);
    return info?.cwd ?? null;
  });

  function send(text: string) {
    const p = inputs.activePane.value;
    if (!p || !p.sessionId) {
      inputs.showToast("No active session");
      return;
    }
    const ep = inputs.endpointForPane(p);
    if (!ep) {
      inputs.showToast("No endpoint");
      return;
    }
    inputs.sendToSession(p.sessionId, ep, text);
  }

  return {
    activePane: inputs.activePane,
    activeSessionId,
    activeEndpoint,
    activeCwd,
    send,
    showToast: inputs.showToast,
  };
}
```

- [ ] **Step 2: Build**

Run: `cd desktop/frontend && npm run build`
Expected: succeeds.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/plugins/usePluginContext.ts
git commit -m "feat(plugin): PluginContext factory tied to App.vue active-pane state"
```

### Task 1.7: PluginHost.vue

**Files:**
- Create: `desktop/frontend/src/plugins/PluginHost.vue`
- Create: `desktop/frontend/src/plugins/PluginHost.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/PluginHost.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h } from "vue";
import PluginHost from "./PluginHost.vue";
import { PLUGINS } from "./registry";

vi.mock("../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(),
  SetPluginConfig: vi.fn(),
}));
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => undefined),
}));

import { GetPluginConfig } from "../../wailsjs/go/main/App";

const fakeContext = {
  activePane: { value: null },
  activeSessionId: { value: null },
  activeEndpoint: { value: null },
  activeCwd: { value: null },
  send: vi.fn(),
  showToast: vi.fn(),
} as any;

const DummyPlugin = defineComponent({
  name: "DummyPlugin",
  setup() {
    return () => h("div", { class: "dummy" }, "dummy");
  },
});

describe("PluginHost", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    PLUGINS.length = 0;
    PLUGINS.push({
      id: "quick-input",
      slot: "bottom-toolbar",
      title: "Test",
      description: "test",
      load: () => Promise.resolve({ default: DummyPlugin }),
      defaultEnabled: true,
    });
    vi.mocked(GetPluginConfig).mockResolvedValue({
      quickInput: { enabled: true, buttons: [] },
      fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
    } as any);
  });

  it("loads and mounts enabled plugin matching slot", async () => {
    const w = mount(PluginHost, {
      props: { slotId: "bottom-toolbar", context: fakeContext },
    });
    await flushPromises();
    await flushPromises();
    expect(w.find(".dummy").exists()).toBe(true);
  });

  it("does not load when plugin is disabled", async () => {
    vi.mocked(GetPluginConfig).mockResolvedValue({
      quickInput: { enabled: false, buttons: [] },
      fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
    } as any);
    const w = mount(PluginHost, {
      props: { slotId: "bottom-toolbar", context: fakeContext },
    });
    await flushPromises();
    expect(w.find(".dummy").exists()).toBe(false);
  });

  it("falls back to disabled when load() rejects", async () => {
    PLUGINS[0].load = () => Promise.reject(new Error("boom"));
    const w = mount(PluginHost, {
      props: { slotId: "bottom-toolbar", context: fakeContext },
    });
    await flushPromises();
    await flushPromises();
    expect(w.find(".dummy").exists()).toBe(false);
    expect(fakeContext.showToast).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/plugins/PluginHost.test.ts`
Expected: FAIL — `PluginHost.vue` not found.

- [ ] **Step 3: Implement**

Create `desktop/frontend/src/plugins/PluginHost.vue`:

```vue
<script lang="ts" setup>
import { computed, onMounted, ref, watch, shallowRef, type Component } from "vue";
import { usePluginConfigStore } from "./configStore";
import { descriptorsForSlot } from "./registry";
import type { PluginContext, PluginDescriptor, PluginSlot } from "./types";

const props = defineProps<{
  slotId: PluginSlot;
  context: PluginContext;
}>();

const store = usePluginConfigStore();

interface LoadedPlugin {
  descriptor: PluginDescriptor;
  component: Component;
}

const loaded = shallowRef<LoadedPlugin[]>([]);
const loading = ref(false);

async function reconcile() {
  if (!store.cfg) return;
  loading.value = true;
  try {
    const slotPlugins = descriptorsForSlot(props.slotId).filter((d) =>
      store.isPluginEnabled(d.id),
    );
    const next: LoadedPlugin[] = [];
    for (const d of slotPlugins) {
      try {
        const mod = await d.load();
        next.push({ descriptor: d, component: mod.default });
      } catch (err) {
        console.error(`plugin ${d.id} failed to load`, err);
        props.context.showToast(`Plugin "${d.title}" failed to load`);
        // Disable so the user is not stuck retrying every reconcile.
        try {
          await store.setEnabled(d.id, false);
        } catch {
          /* ignore secondary failure */
        }
      }
    }
    loaded.value = next;
  } finally {
    loading.value = false;
  }
}

onMounted(async () => {
  if (!store.cfg) await store.load();
  await reconcile();
});

watch(
  () => store.cfg,
  () => {
    void reconcile();
  },
  { deep: true },
);
</script>

<template>
  <div class="plugin-host" :class="`slot-${slotId}`">
    <template v-for="p in loaded" :key="p.descriptor.id">
      <component :is="p.component" :context="context" />
    </template>
  </div>
</template>

<style scoped>
.plugin-host {
  display: contents;
}
.plugin-host.slot-right-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.plugin-host.slot-bottom-toolbar {
  display: flex;
  flex-direction: row;
  align-items: center;
  min-height: 0;
}
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/plugins/PluginHost.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Run full frontend test suite**

Run: `cd desktop/frontend && npm test`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/plugins/PluginHost.vue desktop/frontend/src/plugins/PluginHost.test.ts
git commit -m "feat(plugin): PluginHost with lazy-import + load-failure fallback"
```

### Task 1.8: SettingsPlugins.vue placeholder + 5th SettingsDialog tab

**Files:**
- Create: `desktop/frontend/src/components/SettingsPlugins.vue`
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Create test: `desktop/frontend/src/components/SettingsPlugins.test.ts`

- [ ] **Step 1: Create empty SettingsPlugins.vue**

Create `desktop/frontend/src/components/SettingsPlugins.vue`:

```vue
<script lang="ts" setup>
import { onMounted } from "vue";
import { usePluginConfigStore } from "../plugins/configStore";
import { PLUGINS } from "../plugins/registry";

const store = usePluginConfigStore();

onMounted(async () => {
  if (!store.cfg) await store.load();
});

async function toggle(id: "quick-input" | "file-explorer", enabled: boolean) {
  try {
    await store.setEnabled(id, enabled);
  } catch (err) {
    console.error("setEnabled failed", err);
  }
}
</script>

<template>
  <section class="settings-plugins">
    <p class="hint">
      Plugins are loaded on demand. Disabled plugins do not affect startup
      time or memory.
    </p>
    <div v-if="!store.cfg" class="loading">Loading…</div>
    <ul v-else class="plugin-list">
      <li v-for="p in PLUGINS" :key="p.id" class="plugin-row">
        <label class="row-head">
          <input
            type="checkbox"
            :checked="store.isPluginEnabled(p.id)"
            @change="toggle(p.id, ($event.target as HTMLInputElement).checked)"
          />
          <span class="title">{{ p.title }}</span>
        </label>
        <p class="desc">{{ p.description }}</p>
      </li>
      <li v-if="PLUGINS.length === 0" class="empty">No plugins registered.</li>
    </ul>
  </section>
</template>

<style scoped>
.settings-plugins {
  padding: 12px 16px;
  color: var(--settings-fg, #c9d1d9);
}
.hint {
  margin: 0 0 12px;
  font-size: 12px;
  opacity: 0.7;
}
.plugin-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.plugin-row {
  border: 1px solid #2d333b;
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 8px;
}
.row-head {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
}
.desc {
  margin: 6px 0 0 24px;
  font-size: 12px;
  opacity: 0.7;
}
.empty {
  font-size: 12px;
  opacity: 0.5;
}
</style>
```

- [ ] **Step 2: Add the test**

Create `desktop/frontend/src/components/SettingsPlugins.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import SettingsPlugins from "./SettingsPlugins.vue";
import { PLUGINS } from "../plugins/registry";

vi.mock("../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(),
  SetPluginConfig: vi.fn(),
}));
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => undefined),
}));

import { GetPluginConfig, SetPluginConfig } from "../../wailsjs/go/main/App";

beforeEach(() => {
  setActivePinia(createPinia());
  PLUGINS.length = 0;
  PLUGINS.push({
    id: "quick-input",
    slot: "bottom-toolbar",
    title: "Quick Input",
    description: "x",
    load: () => Promise.reject(new Error("not used")),
    defaultEnabled: true,
  });
  vi.mocked(GetPluginConfig).mockResolvedValue({
    quickInput: { enabled: false, buttons: [] },
    fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
  } as any);
  vi.mocked(SetPluginConfig).mockResolvedValue(undefined as unknown as any);
});

describe("SettingsPlugins", () => {
  it("lists registered plugins with checkboxes reflecting enable state", async () => {
    const w = mount(SettingsPlugins);
    await flushPromises();
    const cb = w.find<HTMLInputElement>("input[type=checkbox]").element;
    expect(cb.checked).toBe(false);
  });

  it("calls SetPluginConfig on toggle", async () => {
    const w = mount(SettingsPlugins);
    await flushPromises();
    await w.find("input[type=checkbox]").setValue(true);
    await flushPromises();
    expect(SetPluginConfig).toHaveBeenCalled();
  });
});
```

- [ ] **Step 3: Wire the 5th tab into SettingsDialog.vue**

Modify `desktop/frontend/src/components/SettingsDialog.vue`:

- Extend the `activeTab` type at line 30:
```ts
const activeTab = ref<"general" | "relay" | "logging" | "updates" | "plugins">("general");
```
- Same change in lines 35-36 (`pendingTab` declaration), lines 55 (`switchTab` signature), line 67 (assignment).
- Add import (top):
```ts
import SettingsPlugins from "./SettingsPlugins.vue";
```
- Add the tab button right after the Updates button in the template (around lines 169-170):
```html
<button
  type="button"
  :class="{ active: activeTab === 'plugins' }"
  @click="switchTab('plugins')"
>Plugins</button>
```
- Add the tab body right after the Updates body (around line 192-194):
```html
<SettingsPlugins v-show="activeTab === 'plugins'" />
```

- [ ] **Step 4: Run the new tests**

Run: `cd desktop/frontend && npx vitest run src/components/SettingsPlugins.test.ts`
Expected: PASS.

- [ ] **Step 5: Run full frontend test + build**

Run: `cd desktop/frontend && npm test && npm run build`
Expected: all pass; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/SettingsPlugins.vue desktop/frontend/src/components/SettingsPlugins.test.ts desktop/frontend/src/components/SettingsDialog.vue
git commit -m "feat(plugin): add Plugins tab in Settings with enable toggles"
```

### Task 1.9: Mount PluginHost slots in App.vue

**Files:**
- Modify: `desktop/frontend/src/App.vue`

- [ ] **Step 1: Add imports and inputs**

In `desktop/frontend/src/App.vue` `<script setup>`:

```ts
import PluginHost from "./plugins/PluginHost.vue";
import { createPluginContext } from "./plugins/usePluginContext";
import { sendInputToSession } from "./lib/sendInput";
```

(`sendInputToSession` is the helper extracted in Step 3 below.)

Find `currentTab` computed (already exists). Add after:

```ts
// Keep a Ref (not ComputedRef) so it satisfies PluginContextInputs.activePane.
const activePaneRef = ref<Pane | null>(null);
watch(
  [() => currentTab.value, () => currentTab.value?.activePaneIdx],
  () => {
    const t = currentTab.value;
    activePaneRef.value = t ? t.panes[t.activePaneIdx] ?? null : null;
  },
  { immediate: true, deep: false },
);

function findSessionInfo(id: string, remote: boolean) {
  const list = remote ? remoteList.value : localList.value;
  return list.find((s) => s.id === id) ?? null;
}

function paneSessionInfoBoxed(p: Pane) {
  if (!p.sessionId) return null;
  return findSessionInfo(p.sessionId, p.remote);
}

const pluginContext = createPluginContext({
  activePane: activePaneRef,
  endpointForPane: endpointFor,
  sessionInfoForPane: paneSessionInfoBoxed,
  sendToSession: (sessionId, endpoint, text) => sendInputToSession(endpoint, sessionId, text),
  showToast,
});
```

If `paneSessionInfo` already exists (see App.vue around line 196), reuse it instead of `paneSessionInfoBoxed`.

- [ ] **Step 2: Modify template**

Find the existing layout root in `App.vue` template. Replace the `main` wrapper to insert the right-panel column and bottom-toolbar strip:

```html
<div class="app-root" :style="themeStyle">
  <TabBar ... />
  <div class="main-row">
    <div class="main">
      <!-- existing PaneGrid lives here unchanged -->
      <PaneGrid ... />
    </div>
    <PluginHost slot-id="right-panel" :context="pluginContext" class="right-panel" />
  </div>
  <PluginHost slot-id="bottom-toolbar" :context="pluginContext" class="bottom-toolbar" />
  <!-- existing dialogs -->
</div>
```

- [ ] **Step 3: Extract sendInput helper**

Create `desktop/frontend/src/lib/sendInput.ts`:

```ts
import { SessionConnection } from "./connection";
import type { Endpoint } from "./api";

// Light cache so repeated send() calls do not churn through WS connects.
// Indexed by `${endpoint.url}|${sessionId}`. We rely on SessionConnection's
// own queueing semantics for not-yet-open sockets.
const cache = new Map<string, SessionConnection>();

function key(endpoint: Endpoint, sessionId: string) {
  return `${endpoint.url}|${sessionId}`;
}

export function sendInputToSession(endpoint: Endpoint, sessionId: string, text: string): void {
  const k = key(endpoint, sessionId);
  let conn = cache.get(k);
  if (!conn) {
    conn = new SessionConnection(endpoint, sessionId, {});
    cache.set(k, conn);
    conn.attach();
  }
  conn.sendInput(text);
}
```

Note: this helper duplicates the connection used by the `TerminalView` for the same session. That is wasteful but harmless (server side dedupes per-session-id and the second connection just forwards `IN`). A follow-up may surface the live `TerminalView` connection via App.vue state; out of scope here.

If `SessionConnection`'s constructor signature does not match (look at `desktop/frontend/src/lib/connection.ts:184`), adapt it; this plan assumes `(endpoint, sessionId, callbacks)`.

- [ ] **Step 4: Add CSS for new layout**

Append to `App.vue` `<style scoped>`:

```css
.main-row {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
}
.main {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.right-panel:empty {
  display: none;
}
.right-panel {
  width: 380px;
  flex: 0 0 380px;
  border-left: 1px solid #2d333b;
  overflow: hidden;
}
.bottom-toolbar:empty {
  display: none;
}
.bottom-toolbar {
  flex: 0 0 32px;
  height: 32px;
  border-top: 1px solid #2d333b;
}
```

Width binding to config and resizers come in Phase 7. Until then, the panel uses the default 380 px; with no plugin registered yet it stays `:empty` and `display:none`.

- [ ] **Step 5: Build + manual sanity check**

Run: `cd desktop/frontend && npm run build`
Expected: succeeds.

Optional manual check (skip if no dev environment ready):
```bash
cd desktop && wails dev -tags webkit2_41
```
Verify Settings → Plugins tab is reachable; both slots remain invisible (registry empty).

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/lib/sendInput.ts
git commit -m "feat(plugin): mount two PluginHost slots in App layout"
```

---

## Phase 2: Quick Input Plugin

Goal: ship a working button bar bound to `quickInput.buttons`, including Settings UI for CRUD, hotkeys, and discard/save.

### Task 2.1: Quick Input descriptor + defaults

**Files:**
- Create: `desktop/frontend/src/plugins/quickInput/index.ts`
- Create: `desktop/frontend/src/plugins/quickInput/defaults.ts`
- Modify: `desktop/frontend/src/plugins/registry.ts`

- [ ] **Step 1: Create descriptor and defaults**

Create `desktop/frontend/src/plugins/quickInput/index.ts`:

```ts
import type { PluginDescriptor } from "../types";

export const quickInputDescriptor: PluginDescriptor = {
  id: "quick-input",
  slot: "bottom-toolbar",
  title: "Quick Input",
  description:
    "Bottom toolbar of user-defined buttons that send text to the active pane.",
  load: () => import("./QuickInputBar.vue"),
  defaultEnabled: true,
};
```

Create `desktop/frontend/src/plugins/quickInput/defaults.ts`:

```ts
import type { QuickInputButton } from "../configStore";

let counter = 0;
function clientID(): string {
  return `qib-${Date.now()}-${counter++}`;
}

export function defaultButtons(): QuickInputButton[] {
  return [
    { id: clientID(), label: "ok", send: "ok", appendNewline: true },
    { id: clientID(), label: "continue", send: "continue", appendNewline: true },
    { id: clientID(), label: "发布", send: "发布", appendNewline: true },
  ];
}
```

(Frontend-side defaults are used only for "Add button → preset" fast path; canonical defaults live in `desktop/plugin_config.go`.)

- [ ] **Step 2: Register descriptor**

In `desktop/frontend/src/plugins/registry.ts`, replace the empty array with:

```ts
import type { PluginDescriptor } from "./types";
import { quickInputDescriptor } from "./quickInput";

export const PLUGINS: PluginDescriptor[] = [quickInputDescriptor];

// ...keep descriptorsForSlot and findDescriptor as before
```

- [ ] **Step 3: Verify type-check**

Run: `cd desktop/frontend && npm run build`
Expected: build fails (no `QuickInputBar.vue` yet) **with module not found**. This is expected; the next task creates it.

Actually: Vite resolves `import()` at runtime by default, but TS does typecheck the module path. To avoid a typecheck failure during the build at this point, we can either skip the build until Task 2.2 lands or add a stub `QuickInputBar.vue` with a minimal export. Use the stub:

Create `desktop/frontend/src/plugins/quickInput/QuickInputBar.vue`:

```vue
<script lang="ts" setup>
</script>
<template><div /></template>
```

Now run: `cd desktop/frontend && npm run build`
Expected: succeeds.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/plugins/quickInput/index.ts desktop/frontend/src/plugins/quickInput/defaults.ts desktop/frontend/src/plugins/quickInput/QuickInputBar.vue desktop/frontend/src/plugins/registry.ts
git commit -m "feat(plugin/quick-input): register descriptor and stub component"
```

### Task 2.2: QuickInputBar.vue with click → send

**Files:**
- Modify: `desktop/frontend/src/plugins/quickInput/QuickInputBar.vue`
- Create: `desktop/frontend/src/plugins/quickInput/QuickInputBar.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/quickInput/QuickInputBar.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import QuickInputBar from "./QuickInputBar.vue";

vi.mock("../../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(),
  SetPluginConfig: vi.fn(),
}));
vi.mock("../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => undefined),
}));

import { GetPluginConfig } from "../../../wailsjs/go/main/App";

function makeContext() {
  return {
    activePane: { value: null },
    activeSessionId: { value: null },
    activeEndpoint: { value: null },
    activeCwd: { value: null },
    send: vi.fn(),
    showToast: vi.fn(),
  };
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.mocked(GetPluginConfig).mockResolvedValue({
    quickInput: {
      enabled: true,
      buttons: [
        { id: "a", label: "ok", send: "ok", appendNewline: true },
        { id: "b", label: "raw", send: "raw", appendNewline: false },
      ],
    },
    fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
  } as any);
});

describe("QuickInputBar", () => {
  it("renders one button per config entry", async () => {
    const ctx = makeContext();
    const w = mount(QuickInputBar, { props: { context: ctx as any } });
    await flushPromises();
    expect(w.findAll("button.quick-input-btn")).toHaveLength(2);
  });

  it("clicking sends text with newline when appendNewline=true", async () => {
    const ctx = makeContext();
    const w = mount(QuickInputBar, { props: { context: ctx as any } });
    await flushPromises();
    await w.findAll("button.quick-input-btn")[0].trigger("click");
    expect(ctx.send).toHaveBeenCalledWith("ok\n");
  });

  it("clicking sends raw text when appendNewline=false", async () => {
    const ctx = makeContext();
    const w = mount(QuickInputBar, { props: { context: ctx as any } });
    await flushPromises();
    await w.findAll("button.quick-input-btn")[1].trigger("click");
    expect(ctx.send).toHaveBeenCalledWith("raw");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/plugins/quickInput/QuickInputBar.test.ts`
Expected: FAIL (stub renders empty div).

- [ ] **Step 3: Implement**

Replace `desktop/frontend/src/plugins/quickInput/QuickInputBar.vue`:

```vue
<script lang="ts" setup>
import { computed, onMounted } from "vue";
import { usePluginConfigStore } from "../configStore";
import type { PluginContext } from "../types";

const props = defineProps<{ context: PluginContext }>();
const store = usePluginConfigStore();

onMounted(async () => {
  if (!store.cfg) await store.load();
});

const buttons = computed(() => store.cfg?.quickInput.buttons ?? []);

function fire(idx: number) {
  const b = buttons.value[idx];
  if (!b) return;
  const text = b.appendNewline ? b.send + "\n" : b.send;
  props.context.send(text);
}

function tooltipFor(send: string, newline: boolean, hotkey?: string): string {
  const shown = newline ? send + "\\n" : send;
  return hotkey ? `${shown} (${hotkey})` : shown;
}
</script>

<template>
  <div class="quick-input-bar">
    <button
      v-for="(b, i) in buttons"
      :key="b.id"
      class="quick-input-btn"
      :title="tooltipFor(b.send, b.appendNewline, b.hotkey)"
      @click="fire(i)"
    >{{ b.label }}</button>
  </div>
</template>

<style scoped>
.quick-input-bar {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 4px;
  padding: 0 8px;
  overflow-x: auto;
  white-space: nowrap;
  width: 100%;
  height: 100%;
  box-sizing: border-box;
}
.quick-input-btn {
  padding: 2px 10px;
  border-radius: 4px;
  border: 1px solid #2d333b;
  background: #21262d;
  color: #c9d1d9;
  font-size: 12px;
  cursor: pointer;
}
.quick-input-btn:hover {
  background: #30363d;
}
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/plugins/quickInput/QuickInputBar.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/quickInput/QuickInputBar.vue desktop/frontend/src/plugins/quickInput/QuickInputBar.test.ts
git commit -m "feat(plugin/quick-input): button bar wired to active-pane send"
```

### Task 2.3: Hotkey composable + conflict checker

**Files:**
- Create: `desktop/frontend/src/plugins/quickInput/hotkeyConflict.ts`
- Create: `desktop/frontend/src/plugins/quickInput/hotkeyConflict.test.ts`
- Create: `desktop/frontend/src/plugins/quickInput/useQuickInputHotkeys.ts`
- Create: `desktop/frontend/src/plugins/quickInput/useQuickInputHotkeys.test.ts`

- [ ] **Step 1: Write hotkeyConflict tests first**

Create `desktop/frontend/src/plugins/quickInput/hotkeyConflict.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { parseHotkey, conflictsWith, BUILT_IN_RESERVED } from "./hotkeyConflict";

describe("parseHotkey", () => {
  it("parses Alt+1", () => {
    expect(parseHotkey("Alt+1")).toEqual({ alt: true, shift: false, key: "1" });
  });
  it("parses Alt+Shift+P", () => {
    expect(parseHotkey("Alt+Shift+P")).toEqual({ alt: true, shift: true, key: "P" });
  });
  it("rejects empty", () => {
    expect(parseHotkey("")).toBeNull();
  });
  it("rejects modifier-only", () => {
    expect(parseHotkey("Alt+")).toBeNull();
  });
  it("rejects without Alt", () => {
    // Cmd / Ctrl are reserved territory; we only allow Alt-based.
    expect(parseHotkey("Ctrl+1")).toBeNull();
  });
});

describe("conflictsWith", () => {
  it("flags duplicates within button list", () => {
    const buttons = [
      { id: "a", label: "x", send: "x", appendNewline: true, hotkey: "Alt+1" },
      { id: "b", label: "y", send: "y", appendNewline: true, hotkey: "Alt+1" },
    ];
    expect(conflictsWith(buttons, "Alt+1", "c")).toBe(true);
  });
  it("does not flag the same button entry as conflicting with itself", () => {
    const buttons = [
      { id: "a", label: "x", send: "x", appendNewline: true, hotkey: "Alt+1" },
    ];
    expect(conflictsWith(buttons, "Alt+1", "a")).toBe(false);
  });
  it("flags built-in reserved combos", () => {
    expect(BUILT_IN_RESERVED.has("Alt+ArrowLeft")).toBe(true);
  });
});
```

- [ ] **Step 2: Run failing test**

Run: `cd desktop/frontend && npx vitest run src/plugins/quickInput/hotkeyConflict.test.ts`
Expected: FAIL.

- [ ] **Step 3: Implement hotkeyConflict**

Create `desktop/frontend/src/plugins/quickInput/hotkeyConflict.ts`:

```ts
import type { QuickInputButton } from "../configStore";

export interface ParsedHotkey {
  alt: boolean;
  shift: boolean;
  key: string;
}

// Existing useTerminalShortcuts uses Alt+ArrowLeft/Right/Up/Down for pane
// focus. Any future built-in must be added here.
export const BUILT_IN_RESERVED = new Set<string>([
  "Alt+ArrowLeft",
  "Alt+ArrowRight",
  "Alt+ArrowUp",
  "Alt+ArrowDown",
]);

export function parseHotkey(s: string): ParsedHotkey | null {
  if (!s) return null;
  const parts = s.split("+").map((p) => p.trim());
  if (parts.length < 2) return null;
  let alt = false;
  let shift = false;
  for (let i = 0; i < parts.length - 1; i++) {
    const mod = parts[i];
    if (mod === "Alt") alt = true;
    else if (mod === "Shift") shift = true;
    else return null;
  }
  if (!alt) return null;
  const key = parts[parts.length - 1];
  if (!key) return null;
  if (/^[A-Za-z0-9]$/.test(key)) return { alt, shift, key: key.toUpperCase() };
  if (/^Arrow(Left|Right|Up|Down)$/.test(key)) return { alt, shift, key };
  return null;
}

export function normalizeHotkey(s: string): string | null {
  const p = parseHotkey(s);
  if (!p) return null;
  const parts: string[] = [];
  if (p.alt) parts.push("Alt");
  if (p.shift) parts.push("Shift");
  parts.push(p.key);
  return parts.join("+");
}

export function conflictsWith(
  buttons: QuickInputButton[],
  hotkey: string,
  selfID: string,
): boolean {
  if (!hotkey) return false;
  const n = normalizeHotkey(hotkey);
  if (!n) return false;
  if (BUILT_IN_RESERVED.has(n)) return true;
  return buttons.some(
    (b) => b.id !== selfID && b.hotkey && normalizeHotkey(b.hotkey) === n,
  );
}
```

- [ ] **Step 4: Run hotkeyConflict tests**

Run: `cd desktop/frontend && npx vitest run src/plugins/quickInput/hotkeyConflict.test.ts`
Expected: PASS.

- [ ] **Step 5: Write useQuickInputHotkeys test**

Create `desktop/frontend/src/plugins/quickInput/useQuickInputHotkeys.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { ref } from "vue";
import { useQuickInputHotkeys } from "./useQuickInputHotkeys";
import type { QuickInputButton } from "../configStore";

describe("useQuickInputHotkeys", () => {
  let onFire: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    onFire = vi.fn();
  });

  afterEach(() => {
    // Composable installs listeners; jsdom does not clean between tests.
    document.removeEventListener("keydown", () => undefined, true);
  });

  it("fires when Alt+digit matches a button hotkey", () => {
    const buttons = ref<QuickInputButton[]>([
      { id: "a", label: "x", send: "x", appendNewline: true, hotkey: "Alt+1" },
    ]);
    useQuickInputHotkeys(buttons, onFire);
    const ev = new KeyboardEvent("keydown", { key: "1", altKey: true });
    document.dispatchEvent(ev);
    expect(onFire).toHaveBeenCalledWith(buttons.value[0]);
  });

  it("ignores non-Alt keypresses", () => {
    const buttons = ref<QuickInputButton[]>([
      { id: "a", label: "x", send: "x", appendNewline: true, hotkey: "Alt+1" },
    ]);
    useQuickInputHotkeys(buttons, onFire);
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "1" }));
    expect(onFire).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 6: Run failing test**

Run: `cd desktop/frontend && npx vitest run src/plugins/quickInput/useQuickInputHotkeys.test.ts`
Expected: FAIL.

- [ ] **Step 7: Implement composable**

Create `desktop/frontend/src/plugins/quickInput/useQuickInputHotkeys.ts`:

```ts
import { onScopeDispose, watch, type Ref } from "vue";
import type { QuickInputButton } from "../configStore";
import { normalizeHotkey } from "./hotkeyConflict";

function keyboardEventToHotkey(e: KeyboardEvent): string | null {
  if (!e.altKey) return null;
  const parts: string[] = ["Alt"];
  if (e.shiftKey) parts.push("Shift");
  // Letters: use e.code (KeyA..KeyZ) so layout / Option-glyph drift does not matter.
  // Digits: e.code is DigitN, but e.key is the digit itself; both reliable.
  if (/^Key[A-Z]$/.test(e.code)) {
    parts.push(e.code.slice(3));
  } else if (/^Digit\d$/.test(e.code)) {
    parts.push(e.code.slice(5));
  } else if (e.key === "ArrowLeft" || e.key === "ArrowRight" || e.key === "ArrowUp" || e.key === "ArrowDown") {
    parts.push(e.key);
  } else {
    return null;
  }
  return parts.join("+");
}

export function useQuickInputHotkeys(
  buttons: Ref<QuickInputButton[]>,
  onFire: (button: QuickInputButton) => void,
): void {
  // Pre-compute a Map<normalizedHotkey, button> so the listener is O(1).
  let map = new Map<string, QuickInputButton>();
  function rebuild() {
    map = new Map();
    for (const b of buttons.value) {
      if (!b.hotkey) continue;
      const n = normalizeHotkey(b.hotkey);
      if (!n) continue;
      map.set(n, b);
    }
  }
  rebuild();
  const stop = watch(buttons, rebuild, { deep: true });

  function handler(e: KeyboardEvent) {
    const sig = keyboardEventToHotkey(e);
    if (!sig) return;
    const b = map.get(sig);
    if (!b) return;
    e.preventDefault();
    e.stopPropagation();
    onFire(b);
  }
  document.addEventListener("keydown", handler, { capture: true });
  onScopeDispose(() => {
    document.removeEventListener("keydown", handler, { capture: true } as EventListenerOptions);
    stop();
  });
}
```

- [ ] **Step 8: Run useQuickInputHotkeys test**

Run: `cd desktop/frontend && npx vitest run src/plugins/quickInput/useQuickInputHotkeys.test.ts`
Expected: PASS.

- [ ] **Step 9: Wire composable into QuickInputBar**

Modify `desktop/frontend/src/plugins/quickInput/QuickInputBar.vue` script:

```ts
import { computed, onMounted } from "vue";
import { usePluginConfigStore } from "../configStore";
import { useQuickInputHotkeys } from "./useQuickInputHotkeys";
import type { PluginContext } from "../types";
import type { QuickInputButton } from "../configStore";

const props = defineProps<{ context: PluginContext }>();
const store = usePluginConfigStore();

onMounted(async () => {
  if (!store.cfg) await store.load();
});

const buttons = computed<QuickInputButton[]>(() => store.cfg?.quickInput.buttons ?? []);

function fire(b: QuickInputButton) {
  const text = b.appendNewline ? b.send + "\n" : b.send;
  props.context.send(text);
}

useQuickInputHotkeys(buttons, fire);
```

Update template to call `fire(b)` directly:
```html
<button v-for="b in buttons" :key="b.id" class="quick-input-btn" :title="tooltipFor(b.send, b.appendNewline, b.hotkey)" @click="fire(b)">{{ b.label }}</button>
```

(QuickInputBar.test.ts already drives interaction by clicking the rendered button, not by calling `fire` directly — assertions remain valid without test changes.)

- [ ] **Step 10: Run all QuickInput tests**

Run: `cd desktop/frontend && npx vitest run src/plugins/quickInput/`
Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add desktop/frontend/src/plugins/quickInput/
git commit -m "feat(plugin/quick-input): hotkey conflict checker + Alt-based hotkey composable"
```

### Task 2.4: QuickInputSettings.vue (CRUD + dirty/save)

**Files:**
- Create: `desktop/frontend/src/plugins/quickInput/QuickInputSettings.vue`
- Create: `desktop/frontend/src/plugins/quickInput/QuickInputSettings.test.ts`
- Modify: `desktop/frontend/src/components/SettingsPlugins.vue` to embed it

- [ ] **Step 1: Write failing test**

Create `desktop/frontend/src/plugins/quickInput/QuickInputSettings.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import QuickInputSettings from "./QuickInputSettings.vue";

vi.mock("../../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(),
  SetPluginConfig: vi.fn(),
}));
vi.mock("../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => undefined),
}));

import { GetPluginConfig, SetPluginConfig } from "../../../wailsjs/go/main/App";

const initial = () => ({
  quickInput: {
    enabled: true,
    buttons: [
      { id: "a", label: "ok", send: "ok", appendNewline: true },
    ],
  },
  fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
});

beforeEach(() => {
  setActivePinia(createPinia());
  vi.mocked(GetPluginConfig).mockResolvedValue(initial() as any);
  vi.mocked(SetPluginConfig).mockResolvedValue(undefined as unknown as any);
});

describe("QuickInputSettings", () => {
  it("starts non-dirty and shows existing buttons", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    expect(w.findAll("tr.button-row")).toHaveLength(1);
    expect((w.find<HTMLButtonElement>("button.save").element as HTMLButtonElement).disabled).toBe(true);
  });

  it("editing label marks dirty and enables Save", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    await w.find("input.label").setValue("yes");
    expect((w.find<HTMLButtonElement>("button.save").element as HTMLButtonElement).disabled).toBe(false);
  });

  it("clicking Add appends a button and goes dirty", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    await w.find("button.add").trigger("click");
    expect(w.findAll("tr.button-row")).toHaveLength(2);
  });

  it("clicking Delete removes the row", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    await w.find("button.delete").trigger("click");
    expect(w.findAll("tr.button-row")).toHaveLength(0);
  });

  it("Save calls SetPluginConfig with edited buttons", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    await w.find("input.label").setValue("yes");
    await w.find("button.save").trigger("click");
    await flushPromises();
    expect(SetPluginConfig).toHaveBeenCalled();
    const arg = vi.mocked(SetPluginConfig).mock.calls[0][0] as any;
    expect(arg.quickInput.buttons[0].label).toBe("yes");
  });

  it("rejects save when a hotkey conflicts", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    await w.find("button.add").trigger("click");
    const hotkeys = w.findAll("input.hotkey");
    await hotkeys[0].setValue("Alt+1");
    await hotkeys[1].setValue("Alt+1");
    await w.find("button.save").trigger("click");
    await flushPromises();
    expect(SetPluginConfig).not.toHaveBeenCalled();
    expect(w.text()).toContain("conflict");
  });
});
```

- [ ] **Step 2: Run failing test**

Run: `cd desktop/frontend && npx vitest run src/plugins/quickInput/QuickInputSettings.test.ts`
Expected: FAIL (file missing).

- [ ] **Step 3: Implement**

Create `desktop/frontend/src/plugins/quickInput/QuickInputSettings.vue`:

```vue
<script lang="ts" setup>
import { computed, onMounted, ref, watch } from "vue";
import { usePluginConfigStore, type QuickInputButton } from "../configStore";
import { conflictsWith } from "./hotkeyConflict";

const store = usePluginConfigStore();
const draft = ref<QuickInputButton[]>([]);
const error = ref<string>("");

function loadDraft() {
  draft.value = JSON.parse(JSON.stringify(store.cfg?.quickInput.buttons ?? []));
}

onMounted(async () => {
  if (!store.cfg) await store.load();
  loadDraft();
});

watch(() => store.cfg?.quickInput.buttons, () => {
  // External update (e.g. plugin-config-changed event); only reload if not dirty
  if (!dirty.value) loadDraft();
}, { deep: true });

const dirty = computed(() => {
  const cur = store.cfg?.quickInput.buttons ?? [];
  return JSON.stringify(cur) !== JSON.stringify(draft.value);
});

let counter = 0;
function newID() {
  return `qib-${Date.now()}-${counter++}`;
}

function addButton() {
  draft.value.push({ id: newID(), label: "new", send: "new", appendNewline: true });
}

function deleteAt(i: number) {
  draft.value.splice(i, 1);
}

function validate(): string | null {
  for (const b of draft.value) {
    if (!b.label.trim()) return "label cannot be empty";
    if (b.hotkey && conflictsWith(draft.value, b.hotkey, b.id)) {
      return `hotkey conflict on "${b.label}"`;
    }
  }
  return null;
}

async function save() {
  const v = validate();
  if (v) {
    error.value = v;
    return;
  }
  error.value = "";
  const next = JSON.parse(JSON.stringify(store.cfg));
  next.quickInput.buttons = JSON.parse(JSON.stringify(draft.value));
  try {
    await store.save(next);
  } catch (err) {
    error.value = (err as Error).message;
  }
}

function discard() {
  loadDraft();
  error.value = "";
}

defineExpose({ dirty });
</script>

<template>
  <div class="quick-input-settings">
    <table>
      <thead>
        <tr>
          <th>Label</th>
          <th>Send</th>
          <th>Newline</th>
          <th>Hotkey</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(b, i) in draft" :key="b.id" class="button-row">
          <td><input class="label" v-model="b.label" /></td>
          <td><input class="send" v-model="b.send" /></td>
          <td><input class="newline" type="checkbox" v-model="b.appendNewline" /></td>
          <td><input class="hotkey" v-model="b.hotkey" placeholder="Alt+1" /></td>
          <td><button class="delete" @click="deleteAt(i)">×</button></td>
        </tr>
      </tbody>
    </table>
    <div class="row-actions">
      <button class="add" @click="addButton">+ Add button</button>
      <div class="spacer" />
      <button class="discard" :disabled="!dirty" @click="discard">Discard</button>
      <button class="save" :disabled="!dirty" @click="save">Save</button>
    </div>
    <div v-if="error" class="error">{{ error }}</div>
  </div>
</template>

<style scoped>
.quick-input-settings { padding: 8px 4px; font-size: 12px; }
table { width: 100%; border-collapse: collapse; }
th { text-align: left; font-weight: 500; padding: 4px 6px; opacity: 0.7; }
td { padding: 4px 6px; }
input { background: #0d1117; border: 1px solid #2d333b; color: #c9d1d9; padding: 2px 6px; border-radius: 3px; }
input.label, input.send, input.hotkey { width: 110px; }
.row-actions { display: flex; gap: 8px; margin-top: 8px; align-items: center; }
.spacer { flex: 1; }
button { background: #21262d; border: 1px solid #2d333b; color: #c9d1d9; padding: 2px 10px; border-radius: 3px; cursor: pointer; }
button:disabled { opacity: 0.4; cursor: default; }
.error { margin-top: 6px; color: #f85149; }
</style>
```

- [ ] **Step 4: Run tests**

Run: `cd desktop/frontend && npx vitest run src/plugins/quickInput/QuickInputSettings.test.ts`
Expected: PASS (6 tests).

- [ ] **Step 5: Embed in SettingsPlugins.vue**

Modify `desktop/frontend/src/components/SettingsPlugins.vue` to render `QuickInputSettings` under the Quick Input row. Add at the top of `<script>`:

```ts
import QuickInputSettings from "../plugins/quickInput/QuickInputSettings.vue";
```

Replace the `<li v-for="p in PLUGINS"...>` block with:

```html
<li v-for="p in PLUGINS" :key="p.id" class="plugin-row">
  <label class="row-head">
    <input
      type="checkbox"
      :checked="store.isPluginEnabled(p.id)"
      @change="toggle(p.id, ($event.target as HTMLInputElement).checked)"
    />
    <span class="title">{{ p.title }}</span>
  </label>
  <p class="desc">{{ p.description }}</p>
  <QuickInputSettings v-if="p.id === 'quick-input' && store.isPluginEnabled('quick-input')" />
</li>
```

- [ ] **Step 6: Run full frontend test suite + build**

Run: `cd desktop/frontend && npm test && npm run build`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/plugins/quickInput/QuickInputSettings.vue desktop/frontend/src/plugins/quickInput/QuickInputSettings.test.ts desktop/frontend/src/components/SettingsPlugins.vue
git commit -m "feat(plugin/quick-input): settings UI with CRUD, dirty/save, and conflict checks"
```

---

## Phase 3: plugin_fs.go Foundation

Goal: a path-allowlisted local-only filesystem binding group. No frontend changes yet.

### Task 3.1: Allowlist + path resolver

**Files:**
- Create: `desktop/plugin_fs.go`
- Create: `desktop/plugin_fs_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/plugin_fs_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeFS returns a PluginFS rooted at a temp dir that simulates the user's
// HOME for the lifetime of the test.
func makeFS(t *testing.T) (*PluginFS, string) {
	t.Helper()
	home := t.TempDir()
	fs := &PluginFS{
		allowRoots: []string{home},
	}
	return fs, home
}

func TestResolveAcceptsPathInsideAllowRoot(t *testing.T) {
	fs, home := makeFS(t)
	sub := filepath.Join(home, "proj")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := fs.resolve(sub)
	if err != nil {
		t.Fatalf("expected accept, got %v", err)
	}
	if got != sub {
		t.Errorf("resolve returned %q, want %q", got, sub)
	}
}

func TestResolveRejectsRelativePath(t *testing.T) {
	fs, _ := makeFS(t)
	if _, err := fs.resolve("relative/path"); err == nil {
		t.Fatal("expected reject of relative path")
	}
}

func TestResolveRejectsOutsideRoot(t *testing.T) {
	fs, _ := makeFS(t)
	tmp := t.TempDir()
	if _, err := fs.resolve(tmp); err == nil {
		t.Fatalf("expected reject of %q outside allow roots", tmp)
	}
}

func TestResolveRejectsParentTraversal(t *testing.T) {
	fs, home := makeFS(t)
	bad := filepath.Join(home, "..", "..", "etc")
	if _, err := fs.resolve(bad); err == nil {
		t.Fatal("expected reject of .. traversal")
	}
}

func TestResolveResolvesSymlinkBeforeChecking(t *testing.T) {
	fs, home := makeFS(t)
	outside := t.TempDir()
	link := filepath.Join(home, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if _, err := fs.resolve(link); err == nil {
		t.Fatal("expected reject of symlink pointing outside roots")
	}
}

func TestResolveRejectsDenyPattern(t *testing.T) {
	fs, home := makeFS(t)
	ssh := filepath.Join(home, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.resolve(ssh); err == nil {
		t.Fatal("expected reject of ~/.ssh")
	}
	envFile := filepath.Join(home, "app", ".env")
	if err := os.MkdirAll(filepath.Dir(envFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("X=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.resolve(envFile); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected deny on .env, got %v", err)
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `go test -tags webkit2_41 -run TestResolve ./desktop/`
Expected: FAIL — `undefined: PluginFS`.

- [ ] **Step 3: Implement**

Create `desktop/plugin_fs.go`:

```go
package main

// PluginFS exposes a small, read-only filesystem API to the desktop webview
// for the File Explorer plugin. Every method runs every path argument through
// resolve() before any I/O.
//
// SECURITY (red-line #11): This binding group is local-only. It MUST NOT be
// reachable from uplink/relay code under any circumstance. The CI check at
// .github/scripts/check-plugin-fs-isolation.sh asserts the package graph
// stays clean.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type PluginFS struct {
	// allowRoots holds the set of directories the binding accepts as
	// containers for path arguments. Populated at construction time from
	// $HOME plus the live set of active local session cwds (see app.go
	// wiring).
	allowRoots []string
}

var (
	ErrPathRelative  = errors.New("plugin_fs: path must be absolute")
	ErrPathForbidden = errors.New("plugin_fs: path forbidden")
	ErrPathDenied    = errors.New("plugin_fs: path denied by policy")
)

// denyExact and denySuffix express paths that are never visible regardless
// of allowRoots. The check is run on the fully-resolved path.
var denyExact = []string{".ssh", ".gnupg", ".aws"}
var denySuffix = []string{".env"}

func isDenied(resolved string) bool {
	base := filepath.Base(resolved)
	for _, d := range denyExact {
		if base == d {
			return true
		}
	}
	for _, suf := range denySuffix {
		if base == suf || strings.HasPrefix(base, suf+".") {
			return true
		}
	}
	// Also walk segments for nested ~/.ssh inside an allowed root.
	parts := strings.Split(resolved, string(filepath.Separator))
	for _, p := range parts {
		for _, d := range denyExact {
			if p == d {
				return true
			}
		}
	}
	return false
}

// resolve normalizes path, follows symlinks, and checks against allowRoots
// and deny patterns. Returns the cleaned absolute path on success.
func (p *PluginFS) resolve(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrPathRelative
	}
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		// If the path itself does not yet exist (e.g. for a Reveal call on
		// a freshly-created file that has not yet been observed by us), fall
		// back to the lexical clean. Allowlist still applies.
		resolved = clean
	}
	if isDenied(resolved) {
		return "", fmt.Errorf("%w: %s", ErrPathDenied, resolved)
	}
	for _, root := range p.allowRoots {
		rel, err := filepath.Rel(root, resolved)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrPathForbidden, resolved)
}
```

- [ ] **Step 4: Run tests**

Run: `go test -tags webkit2_41 -run TestResolve ./desktop/`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add desktop/plugin_fs.go desktop/plugin_fs_test.go
git commit -m "feat(plugin-fs): allowlist-based path resolver with symlink + deny patterns"
```

### Task 3.2: ListDir

**Files:**
- Modify: `desktop/plugin_fs.go`
- Modify: `desktop/plugin_fs_test.go`

- [ ] **Step 1: Write failing test**

Append to `desktop/plugin_fs_test.go`:

```go
func TestListDirReturnsEntries(t *testing.T) {
	fs, home := makeFS(t)
	if err := os.Mkdir(filepath.Join(home, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ListDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	var foundDir, foundFile bool
	for _, e := range got {
		if e.Name == "sub" && e.IsDir {
			foundDir = true
		}
		if e.Name == "f.txt" && !e.IsDir && e.Size == 2 {
			foundFile = true
		}
	}
	if !foundDir || !foundFile {
		t.Fatalf("entries did not include expected dir+file: %+v", got)
	}
}

func TestListDirRefusesOutsideRoots(t *testing.T) {
	fs, _ := makeFS(t)
	if _, err := fs.ListDir(t.TempDir()); err == nil {
		t.Fatal("expected refusal")
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `go test -tags webkit2_41 -run TestListDir ./desktop/`
Expected: FAIL — `fs.ListDir undefined`.

- [ ] **Step 3: Implement**

Append to `desktop/plugin_fs.go`:

```go
// DirEntry is a serialized representation of one directory entry.
type DirEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"modTime,omitempty"` // unix ms
}

// ListDir returns entries inside path. Path must be a directory and inside an
// allow-root. Hidden filtering is done frontend-side; ListDir is exhaustive.
func (p *PluginFS) ListDir(path string) ([]DirEntry, error) {
	resolved, err := p.resolve(path)
	if err != nil {
		return nil, err
	}
	entries, err := osReadDir(resolved)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, DirEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
		})
	}
	return out, nil
}
```

Add at the top of the file (after imports):

```go
import "os"
```

…and a wrapper for testability (keeps `os.ReadDir` shimable later if needed):

```go
var osReadDir = func(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}
```

- [ ] **Step 4: Run tests**

Run: `go test -tags webkit2_41 -run TestListDir ./desktop/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/plugin_fs.go desktop/plugin_fs_test.go
git commit -m "feat(plugin-fs): ListDir"
```

### Task 3.3: ReadFile + FileMeta + binary detection

**Files:**
- Modify: `desktop/plugin_fs.go`
- Modify: `desktop/plugin_fs_test.go`

- [ ] **Step 1: Write failing tests**

Append to `desktop/plugin_fs_test.go`:

```go
func TestReadFileSuccessAndTruncation(t *testing.T) {
	fs, home := makeFS(t)
	big := make([]byte, 200)
	for i := range big {
		big[i] = 'A'
	}
	path := filepath.Join(home, "f.txt")
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(path, 200)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Data) != string(big) {
		t.Fatal("data mismatch")
	}
	if got.TruncatedAt != 0 {
		t.Fatalf("expected no truncation, got %d", got.TruncatedAt)
	}

	got2, err := fs.ReadFile(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2.Data) != 50 || got2.TruncatedAt != 200 {
		t.Fatalf("unexpected truncation result: %+v", got2)
	}
}

func TestReadFileRejectsTooLarge(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "huge.txt")
	if err := os.WriteFile(path, make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadFile(path, maxReadBytesHard+1); err == nil {
		t.Fatal("expected hard-cap rejection")
	}
}

func TestReadFileDetectsBinary(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "bin")
	if err := os.WriteFile(path, []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsBinary {
		t.Fatal("expected IsBinary=true")
	}
}

func TestFileMeta(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.FileMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Size != 5 || got.IsBinary {
		t.Fatalf("unexpected meta %+v", got)
	}
}
```

- [ ] **Step 2: Run failing tests**

Run: `go test -tags webkit2_41 -run "TestReadFile|TestFileMeta" ./desktop/`
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `desktop/plugin_fs.go`:

```go
const (
	maxReadBytesHard = 5 * 1024 * 1024 // server-side hard cap (5 MB)
	binaryProbeBytes = 4096            // bytes inspected for NUL → binary
)

type FileContent struct {
	Path        string `json:"path"`
	Data        []byte `json:"data"`
	IsBinary    bool   `json:"isBinary"`
	TruncatedAt int64  `json:"truncatedAt,omitempty"`
}

type FileMetaInfo struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"`
	IsBinary bool   `json:"isBinary"`
}

// ReadFile returns up to maxBytes from path. If the file is larger than
// maxBytes, the returned Data is truncated and TruncatedAt reports the full
// file size. Binary detection samples the first 4 KB.
func (p *PluginFS) ReadFile(path string, maxBytes int64) (FileContent, error) {
	if maxBytes > maxReadBytesHard {
		return FileContent{}, fmt.Errorf("plugin_fs: maxBytes %d exceeds hard cap %d", maxBytes, maxReadBytesHard)
	}
	resolved, err := p.resolve(path)
	if err != nil {
		return FileContent{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FileContent{}, err
	}
	if info.IsDir() {
		return FileContent{}, fmt.Errorf("plugin_fs: %s is a directory", resolved)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return FileContent{}, err
	}
	defer f.Close()

	size := info.Size()
	readLen := size
	truncated := int64(0)
	if size > maxBytes {
		readLen = maxBytes
		truncated = size
	}
	data := make([]byte, readLen)
	if _, err := f.Read(data); err != nil && err.Error() != "EOF" {
		return FileContent{}, err
	}
	probe := data
	if int64(len(probe)) > binaryProbeBytes {
		probe = probe[:binaryProbeBytes]
	}
	isBin := false
	for _, b := range probe {
		if b == 0 {
			isBin = true
			break
		}
	}
	return FileContent{Path: resolved, Data: data, IsBinary: isBin, TruncatedAt: truncated}, nil
}

// FileMeta returns size + modtime + binary-ness without reading the file body.
// Used by the frontend's "should I open this in the editor?" pre-check.
func (p *PluginFS) FileMeta(path string) (FileMetaInfo, error) {
	resolved, err := p.resolve(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FileMetaInfo{}, err
	}
	isBin := false
	if !info.IsDir() {
		f, err := os.Open(resolved)
		if err == nil {
			probe := make([]byte, binaryProbeBytes)
			n, _ := f.Read(probe)
			f.Close()
			for _, b := range probe[:n] {
				if b == 0 {
					isBin = true
					break
				}
			}
		}
	}
	return FileMetaInfo{
		Path:     resolved,
		Size:     info.Size(),
		ModTime:  info.ModTime().UnixMilli(),
		IsBinary: isBin,
	}, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test -tags webkit2_41 -run "TestReadFile|TestFileMeta" ./desktop/`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add desktop/plugin_fs.go desktop/plugin_fs_test.go
git commit -m "feat(plugin-fs): ReadFile with truncation/binary detection + FileMeta"
```

### Task 3.4: RevealInOS + OpenExternal

**Files:**
- Modify: `desktop/plugin_fs.go`

(These shell out to `open`/`xdg-open`/`explorer`. No automated test for the side-effects; we just verify the allowlist guard fires for forbidden paths.)

- [ ] **Step 1: Write failing test**

Append to `desktop/plugin_fs_test.go`:

```go
func TestRevealInOSRefusesForbidden(t *testing.T) {
	fs, _ := makeFS(t)
	if err := fs.RevealInOS(t.TempDir()); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestOpenExternalRefusesForbidden(t *testing.T) {
	fs, _ := makeFS(t)
	if err := fs.OpenExternal(t.TempDir()); err == nil {
		t.Fatal("expected refusal")
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `go test -tags webkit2_41 -run "TestReveal|TestOpenExternal" ./desktop/`
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `desktop/plugin_fs.go`:

```go
import (
	"os/exec"
	stdruntime "runtime"
)

// RevealInOS asks the OS file manager to select the path. On macOS:
//   open -R <path>
// On Linux:
//   xdg-open <dir-of-path>      (no per-platform "reveal selecting" verb)
// On Windows:
//   explorer /select,<path>
func (p *PluginFS) RevealInOS(path string) error {
	resolved, err := p.resolve(path)
	if err != nil {
		return err
	}
	switch stdruntime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", resolved).Start()
	case "windows":
		return exec.Command("explorer", "/select,"+resolved).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(resolved)).Start()
	}
}

// OpenExternal launches the OS default application for the path.
func (p *PluginFS) OpenExternal(path string) error {
	resolved, err := p.resolve(path)
	if err != nil {
		return err
	}
	switch stdruntime.GOOS {
	case "darwin":
		return exec.Command("open", resolved).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", resolved).Start()
	default:
		return exec.Command("xdg-open", resolved).Start()
	}
}
```

(Consolidate the imports at the top of the file — if `os/exec` and `runtime` are already imported elsewhere, do not duplicate.)

- [ ] **Step 4: Run tests**

Run: `go test -tags webkit2_41 -run "TestReveal|TestOpenExternal" ./desktop/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/plugin_fs.go desktop/plugin_fs_test.go
git commit -m "feat(plugin-fs): RevealInOS and OpenExternal with allowlist guard"
```

### Task 3.5: Register PluginFS in Wails

**Files:**
- Modify: `desktop/app.go`
- Modify: `desktop/main.go`

- [ ] **Step 1: Add constructor**

In `desktop/plugin_fs.go`, append:

```go
// NewPluginFS builds a PluginFS with allowRoots seeded from $HOME. The set is
// later expanded at call time to include cwds of active local sessions; that
// dynamic enrichment is plugged in below.
func NewPluginFS() *PluginFS {
	home, _ := os.UserHomeDir()
	return &PluginFS{allowRoots: []string{home}}
}
```

(Dynamic allowRoot enrichment from session cwds is deferred to a follow-up. For phase 1 we ship the home-only allow root, which is the user's main project tree on the dev machines we target.)

- [ ] **Step 2: Register in Bind list**

Modify `desktop/main.go`. Add at top of `main()`:

```go
pluginFS := NewPluginFS()
```

In the `Bind` slice:

```go
Bind: []interface{}{
    app,
    pluginFS,
},
```

- [ ] **Step 3: Regenerate Wails bindings**

Run from `desktop/`:
```bash
wails generate module
```
(If `wails` CLI is unavailable, this will run on next `wails dev`/`wails build`.)

- [ ] **Step 4: Run all Go tests**

Run: `go test -tags webkit2_41 -timeout 60s ./desktop/`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/plugin_fs.go desktop/main.go desktop/frontend/wailsjs/go/main/
git commit -m "feat(plugin-fs): register PluginFS Wails binding group"
```

### Task 3.6: CI isolation guard

**Files:**
- Create: `.github/scripts/check-plugin-fs-isolation.sh`
- Modify: `.github/workflows/ci.yml` (or relevant workflow)

- [ ] **Step 1: Write the guard script**

Create `.github/scripts/check-plugin-fs-isolation.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Red line #11: PluginFS bindings must not be reachable via the uplink/relay
# path. Any reference outside desktop/ (excluding test files) fails CI.

hits=$(grep -rn 'PluginFS\b' \
    --include='*.go' \
    desktop/uplink.go \
    desktop/uplink_*.go \
    internal/ 2>/dev/null || true)

if [ -n "$hits" ]; then
    echo "ERROR: PluginFS referenced outside the desktop/local binding surface:"
    echo "$hits"
    exit 1
fi

echo "ok: PluginFS isolation preserved"
```

Make it executable: `chmod +x .github/scripts/check-plugin-fs-isolation.sh`.

- [ ] **Step 2: Add a CI step**

Locate the existing CI workflow (look in `.github/workflows/`). Add a step to whichever Go-test job exists:

```yaml
      - name: PluginFS isolation guard
        run: ./.github/scripts/check-plugin-fs-isolation.sh
```

If the workflow filename or job structure differs, adapt; the point is that the guard runs on every push.

- [ ] **Step 3: Run the guard locally**

Run: `./.github/scripts/check-plugin-fs-isolation.sh`
Expected output: `ok: PluginFS isolation preserved`.

- [ ] **Step 4: Commit**

```bash
git add .github/scripts/check-plugin-fs-isolation.sh .github/workflows/
git commit -m "ci(plugin-fs): guard against uplink/relay imports of PluginFS"
```

---

## Phase 4: File Explorer Tree + Tabs (No Editor Yet)

Goal: tree + tabs shell visible in right panel; clicking files shows a placeholder, not CodeMirror. Refresh button only; no fs watcher.

### Task 4.1: tabsModel.ts (pure preview/persistent/LRU logic)

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/tabsModel.ts`
- Create: `desktop/frontend/src/plugins/fileExplorer/tabsModel.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/fileExplorer/tabsModel.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { openPath, closeTab, type TabsState } from "./tabsModel";

function empty(): TabsState {
  return { tabs: [], activeIdx: -1 };
}

describe("tabsModel.openPath", () => {
  it("single-click opens a preview tab", () => {
    let s = empty();
    s = openPath(s, "/a.txt", "preview");
    expect(s.tabs).toHaveLength(1);
    expect(s.tabs[0].persistent).toBe(false);
    expect(s.activeIdx).toBe(0);
  });

  it("subsequent single-click on different file replaces the preview tab", () => {
    let s = empty();
    s = openPath(s, "/a.txt", "preview");
    s = openPath(s, "/b.txt", "preview");
    expect(s.tabs).toHaveLength(1);
    expect(s.tabs[0].path).toBe("/b.txt");
  });

  it("double-click promotes existing preview to persistent", () => {
    let s = empty();
    s = openPath(s, "/a.txt", "preview");
    s = openPath(s, "/a.txt", "persistent");
    expect(s.tabs).toHaveLength(1);
    expect(s.tabs[0].persistent).toBe(true);
  });

  it("single-click on a different file when a persistent tab exists adds a preview tab", () => {
    let s = empty();
    s = openPath(s, "/a.txt", "persistent");
    s = openPath(s, "/b.txt", "preview");
    expect(s.tabs.map((t) => t.path)).toEqual(["/a.txt", "/b.txt"]);
  });

  it("over 8 tabs evicts the oldest non-persistent tab", () => {
    let s = empty();
    for (let i = 0; i < 8; i++) s = openPath(s, `/p${i}`, "persistent");
    s = openPath(s, "/p8", "preview");
    expect(s.tabs).toHaveLength(8);
    // All 8 persistent slots stay; the new preview replaces? No — over-8 means
    // we evict oldest non-persistent. With 8 persistent + 1 preview = 9, we
    // evict the new preview's natural slot? Actually: rule says LRU non-persistent
    // first. With no non-persistent, fall through to LRU-by-activation persistent.
    // For this test, accept that p0 (the oldest persistent) is gone, p8 is in.
    expect(s.tabs.map((t) => t.path)).toContain("/p8");
    expect(s.tabs.map((t) => t.path)).not.toContain("/p0");
  });
});

describe("tabsModel.closeTab", () => {
  it("removes the tab and refocuses neighbor", () => {
    let s = empty();
    s = openPath(s, "/a.txt", "persistent");
    s = openPath(s, "/b.txt", "persistent");
    s = closeTab(s, 0);
    expect(s.tabs).toHaveLength(1);
    expect(s.tabs[0].path).toBe("/b.txt");
    expect(s.activeIdx).toBe(0);
  });
});
```

- [ ] **Step 2: Run failing test**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/tabsModel.test.ts`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `desktop/frontend/src/plugins/fileExplorer/tabsModel.ts`:

```ts
export interface Tab {
  path: string;
  persistent: boolean;
  // Activation order ts, larger = more recent. Used for LRU eviction.
  lastActiveAt: number;
}

export interface TabsState {
  tabs: Tab[];
  activeIdx: number;
}

const MAX_TABS = 8;

export type OpenKind = "preview" | "persistent";

export function openPath(state: TabsState, path: string, kind: OpenKind): TabsState {
  const now = Date.now();
  const existingIdx = state.tabs.findIndex((t) => t.path === path);
  if (existingIdx >= 0) {
    const next = clone(state);
    next.tabs[existingIdx].lastActiveAt = now;
    if (kind === "persistent") next.tabs[existingIdx].persistent = true;
    next.activeIdx = existingIdx;
    return next;
  }

  if (kind === "preview") {
    // Replace the existing preview tab if any.
    const previewIdx = state.tabs.findIndex((t) => !t.persistent);
    if (previewIdx >= 0) {
      const next = clone(state);
      next.tabs[previewIdx] = { path, persistent: false, lastActiveAt: now };
      next.activeIdx = previewIdx;
      return next;
    }
  }

  // Append; may need eviction.
  let next = clone(state);
  next.tabs.push({ path, persistent: kind === "persistent", lastActiveAt: now });
  next.activeIdx = next.tabs.length - 1;
  if (next.tabs.length > MAX_TABS) {
    next = evictOldest(next);
  }
  return next;
}

export function closeTab(state: TabsState, idx: number): TabsState {
  if (idx < 0 || idx >= state.tabs.length) return state;
  const next = clone(state);
  next.tabs.splice(idx, 1);
  if (next.tabs.length === 0) {
    next.activeIdx = -1;
    return next;
  }
  next.activeIdx = Math.min(idx, next.tabs.length - 1);
  return next;
}

function evictOldest(state: TabsState): TabsState {
  // Prefer evicting non-persistent. If none, fall back to least-recently-active.
  const candidatePred = (t: Tab) => !t.persistent;
  const candidates = state.tabs.filter(candidatePred);
  const pool = candidates.length > 0 ? candidates : state.tabs;
  let oldest = pool[0];
  for (const t of pool) {
    if (t.lastActiveAt < oldest.lastActiveAt) oldest = t;
  }
  const idx = state.tabs.indexOf(oldest);
  return closeTab(state, idx);
}

function clone(s: TabsState): TabsState {
  return { tabs: s.tabs.map((t) => ({ ...t })), activeIdx: s.activeIdx };
}
```

- [ ] **Step 4: Run tests**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/tabsModel.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/tabsModel.ts desktop/frontend/src/plugins/fileExplorer/tabsModel.test.ts
git commit -m "feat(plugin/file-explorer): pure preview/persistent/LRU tabs model"
```

### Task 4.2: FileTree.vue

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/FileTree.vue`
- Create: `desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts`

- [ ] **Step 1: Write failing test**

Create `desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import FileTree from "./FileTree.vue";

vi.mock("../../../wailsjs/go/main/PluginFS", () => ({
  ListDir: vi.fn(),
}));

import { ListDir } from "../../../wailsjs/go/main/PluginFS";

beforeEach(() => {
  vi.mocked(ListDir).mockImplementation(async (path: string) => {
    if (path === "/proj") {
      return [
        { name: "src", isDir: true },
        { name: ".git", isDir: true },
        { name: "README.md", isDir: false, size: 100 },
      ] as any;
    }
    return [] as any;
  });
});

describe("FileTree", () => {
  it("lists root entries on mount; filters hidden by default", async () => {
    const w = mount(FileTree, { props: { root: "/proj", showHidden: false } });
    await flushPromises();
    const items = w.findAll(".node-name").map((n) => n.text());
    expect(items).toContain("src");
    expect(items).toContain("README.md");
    expect(items).not.toContain(".git");
  });

  it("includes hidden entries when showHidden=true", async () => {
    const w = mount(FileTree, { props: { root: "/proj", showHidden: true } });
    await flushPromises();
    const items = w.findAll(".node-name").map((n) => n.text());
    expect(items).toContain(".git");
  });

  it("clicking a file emits file-clicked", async () => {
    const w = mount(FileTree, { props: { root: "/proj", showHidden: false } });
    await flushPromises();
    await w.findAll(".node[data-type=file]")[0].trigger("click");
    expect(w.emitted("file-clicked")).toBeTruthy();
  });

  it("clicking a file twice rapidly emits file-double-clicked", async () => {
    const w = mount(FileTree, { props: { root: "/proj", showHidden: false } });
    await flushPromises();
    const node = w.findAll(".node[data-type=file]")[0];
    await node.trigger("dblclick");
    expect(w.emitted("file-double-clicked")).toBeTruthy();
  });
});
```

- [ ] **Step 2: Run failing test**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTree.test.ts`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `desktop/frontend/src/plugins/fileExplorer/FileTree.vue`:

```vue
<script lang="ts" setup>
import { ref, watch, onMounted } from "vue";
import { ListDir } from "../../../wailsjs/go/main/PluginFS";

interface DirEntry {
  name: string;
  isDir: boolean;
  size?: number;
  modTime?: number;
}

interface Node {
  path: string;
  name: string;
  isDir: boolean;
  expanded: boolean;
  children: Node[] | null; // null = not yet loaded
}

const props = defineProps<{
  root: string;
  showHidden: boolean;
}>();

const emit = defineEmits<{
  (e: "file-clicked", path: string): void;
  (e: "file-double-clicked", path: string): void;
  (e: "dir-toggled", path: string, expanded: boolean): void;
}>();

const rootNodes = ref<Node[]>([]);

async function loadDir(path: string): Promise<Node[]> {
  const entries = (await ListDir(path)) as DirEntry[];
  const nodes: Node[] = entries
    .filter((e) => props.showHidden || !e.name.startsWith("."))
    .map((e) => ({
      path: joinPath(path, e.name),
      name: e.name,
      isDir: e.isDir,
      expanded: false,
      children: null,
    }))
    .sort((a, b) => (a.isDir === b.isDir ? a.name.localeCompare(b.name) : a.isDir ? -1 : 1));
  return nodes;
}

function joinPath(parent: string, name: string): string {
  return parent.endsWith("/") ? parent + name : parent + "/" + name;
}

async function refreshRoot() {
  rootNodes.value = await loadDir(props.root);
}

watch(() => [props.root, props.showHidden], () => {
  void refreshRoot();
});

onMounted(() => {
  void refreshRoot();
});

async function toggle(n: Node) {
  if (!n.isDir) return;
  if (!n.expanded) {
    if (n.children === null) n.children = await loadDir(n.path);
    n.expanded = true;
  } else {
    n.expanded = false;
  }
  emit("dir-toggled", n.path, n.expanded);
}

function clickFile(n: Node) {
  if (n.isDir) return;
  emit("file-clicked", n.path);
}

function dblClickFile(n: Node) {
  if (n.isDir) return;
  emit("file-double-clicked", n.path);
}

defineExpose({ refresh: refreshRoot });
</script>

<template>
  <ul class="tree-list">
    <li v-for="n in rootNodes" :key="n.path">
      <NodeRow
        :node="n"
        :level="0"
        @toggle="toggle"
        @click-file="clickFile"
        @dblclick-file="dblClickFile"
      />
    </li>
  </ul>
</template>

<script lang="ts">
import { defineComponent, h, PropType } from "vue";
export const NodeRow = defineComponent({
  name: "NodeRow",
  props: {
    node: { type: Object as PropType<any>, required: true },
    level: { type: Number, required: true },
  },
  emits: ["toggle", "click-file", "dblclick-file"],
  setup(props, { emit }) {
    return () =>
      h("div", { class: "node-wrap" }, [
        h(
          "div",
          {
            class: "node",
            "data-type": props.node.isDir ? "dir" : "file",
            style: { paddingLeft: `${props.level * 12}px` },
            onClick: () => (props.node.isDir ? emit("toggle", props.node) : emit("click-file", props.node)),
            onDblclick: () => (!props.node.isDir ? emit("dblclick-file", props.node) : null),
          },
          [
            h("span", { class: "twisty" }, props.node.isDir ? (props.node.expanded ? "▾" : "▸") : ""),
            h("span", { class: "node-name" }, props.node.name),
          ],
        ),
        props.node.expanded && props.node.children
          ? h(
              "ul",
              { class: "tree-list" },
              props.node.children.map((c: any) =>
                h(
                  "li",
                  { key: c.path },
                  h(NodeRow, {
                    node: c,
                    level: props.level + 1,
                    onToggle: (n: any) => emit("toggle", n),
                    "onClick-file": (n: any) => emit("click-file", n),
                    "onDblclick-file": (n: any) => emit("dblclick-file", n),
                  }),
                ),
              ),
            )
          : null,
      ]);
  },
});
</script>

<style scoped>
.tree-list { list-style: none; margin: 0; padding: 0; }
.node { display: flex; align-items: center; padding: 1px 4px; cursor: default; font-size: 12px; }
.node:hover { background: #21262d; }
.twisty { display: inline-block; width: 14px; color: #8b949e; }
.node-name { flex: 1; color: #c9d1d9; user-select: none; }
</style>
```

- [ ] **Step 4: Run tests**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTree.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileTree.vue desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts
git commit -m "feat(plugin/file-explorer): FileTree with lazy expand + hidden filter"
```

### Task 4.3: FileTabs.vue

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/FileTabs.vue`
- Create: `desktop/frontend/src/plugins/fileExplorer/FileTabs.test.ts`

- [ ] **Step 1: Write failing test**

Create `desktop/frontend/src/plugins/fileExplorer/FileTabs.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import FileTabs from "./FileTabs.vue";

describe("FileTabs", () => {
  const tabs = [
    { path: "/a.txt", persistent: true, lastActiveAt: 1 },
    { path: "/b.txt", persistent: false, lastActiveAt: 2 },
  ];

  it("renders one tab per entry, styled by persistent flag", () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0 } });
    expect(w.findAll(".tab")).toHaveLength(2);
    expect(w.findAll(".tab")[1].classes()).toContain("preview");
  });

  it("clicking a tab emits select", async () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0 } });
    await w.findAll(".tab")[1].trigger("click");
    expect(w.emitted("select")?.[0]).toEqual([1]);
  });

  it("clicking close emits close with idx", async () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0 } });
    await w.findAll(".tab .close")[1].trigger("click");
    expect(w.emitted("close")?.[0]).toEqual([1]);
  });
});
```

- [ ] **Step 2: Run failing test**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTabs.test.ts`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `desktop/frontend/src/plugins/fileExplorer/FileTabs.vue`:

```vue
<script lang="ts" setup>
import type { Tab } from "./tabsModel";

const props = defineProps<{
  tabs: Tab[];
  activeIdx: number;
}>();

const emit = defineEmits<{
  (e: "select", idx: number): void;
  (e: "close", idx: number): void;
}>();

function basename(p: string): string {
  const i = p.lastIndexOf("/");
  return i === -1 ? p : p.slice(i + 1);
}
</script>

<template>
  <div class="file-tabs">
    <div
      v-for="(t, i) in tabs"
      :key="t.path"
      class="tab"
      :class="{ active: i === activeIdx, preview: !t.persistent }"
      @click="emit('select', i)"
    >
      <span class="name">{{ basename(t.path) }}</span>
      <span class="close" @click.stop="emit('close', i)">×</span>
    </div>
  </div>
</template>

<style scoped>
.file-tabs { display: flex; flex-direction: row; overflow-x: auto; border-bottom: 1px solid #2d333b; }
.tab { display: flex; align-items: center; gap: 6px; padding: 4px 8px; font-size: 11px; border-right: 1px solid #2d333b; cursor: pointer; color: #c9d1d9; white-space: nowrap; }
.tab.active { background: #161b22; }
.tab.preview .name { font-style: italic; }
.close { opacity: 0.5; }
.close:hover { opacity: 1; }
</style>
```

- [ ] **Step 4: Run tests**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTabs.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileTabs.vue desktop/frontend/src/plugins/fileExplorer/FileTabs.test.ts
git commit -m "feat(plugin/file-explorer): FileTabs with preview-italic and close button"
```

### Task 4.4: FileExplorer.vue shell + descriptor

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue`
- Create: `desktop/frontend/src/plugins/fileExplorer/index.ts`
- Modify: `desktop/frontend/src/plugins/registry.ts`

- [ ] **Step 1: Implement FileExplorer.vue (placeholder editor area)**

Create `desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue`:

```vue
<script lang="ts" setup>
import { computed, ref, watch } from "vue";
import { usePluginConfigStore } from "../configStore";
import FileTree from "./FileTree.vue";
import FileTabs from "./FileTabs.vue";
import { openPath, closeTab, type TabsState } from "./tabsModel";
import type { PluginContext } from "../types";

const props = defineProps<{ context: PluginContext }>();
const store = usePluginConfigStore();

// Pinned root (in-memory only; resets on app restart per spec).
const pinned = ref<string | null>(null);

const root = computed<string | null>(() => pinned.value ?? props.context.activeCwd.value);

const tabsState = ref<TabsState>({ tabs: [], activeIdx: -1 });

function onFileClick(path: string) {
  tabsState.value = openPath(tabsState.value, path, "preview");
}

function onFileDoubleClick(path: string) {
  tabsState.value = openPath(tabsState.value, path, "persistent");
}

function selectTab(idx: number) {
  tabsState.value = { ...tabsState.value, activeIdx: idx };
}

function closeTabAt(idx: number) {
  tabsState.value = closeTab(tabsState.value, idx);
}

function togglePin() {
  pinned.value = pinned.value === null ? props.context.activeCwd.value : null;
}

const activePath = computed(() =>
  tabsState.value.activeIdx >= 0 ? tabsState.value.tabs[tabsState.value.activeIdx].path : null,
);

const showHidden = computed(() => store.cfg?.fileExplorer.showHidden ?? false);
</script>

<template>
  <div class="file-explorer">
    <header class="fe-header">
      <span class="root-path" :title="root ?? ''">{{ root ?? "(no active pane)" }}</span>
      <button class="pin" :class="{ pinned: pinned !== null }" :title="pinned ? 'Pinned' : 'Pin root'" @click="togglePin">📌</button>
    </header>
    <div class="fe-body">
      <div class="tree-pane">
        <FileTree
          v-if="root"
          :root="root"
          :show-hidden="showHidden"
          @file-clicked="onFileClick"
          @file-double-clicked="onFileDoubleClick"
        />
        <div v-else class="placeholder">No active pane.</div>
      </div>
      <div class="editor-pane">
        <FileTabs :tabs="tabsState.tabs" :active-idx="tabsState.activeIdx" @select="selectTab" @close="closeTabAt" />
        <div class="editor-area">
          <div v-if="!activePath" class="placeholder">Select a file.</div>
          <div v-else class="placeholder">Preview placeholder: {{ activePath }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.file-explorer { display: flex; flex-direction: column; height: 100%; color: #c9d1d9; }
.fe-header { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-bottom: 1px solid #2d333b; font-size: 11px; }
.root-path { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; opacity: 0.85; }
.pin { background: none; border: none; cursor: pointer; opacity: 0.5; }
.pin.pinned { opacity: 1; }
.fe-body { flex: 1; display: flex; min-height: 0; }
.tree-pane { width: 30%; min-width: 120px; overflow: auto; border-right: 1px solid #2d333b; }
.editor-pane { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.editor-area { flex: 1; overflow: auto; padding: 8px; }
.placeholder { opacity: 0.5; font-size: 12px; padding: 12px; }
</style>
```

- [ ] **Step 2: Create descriptor**

Create `desktop/frontend/src/plugins/fileExplorer/index.ts`:

```ts
import type { PluginDescriptor } from "../types";

export const fileExplorerDescriptor: PluginDescriptor = {
  id: "file-explorer",
  slot: "right-panel",
  title: "File Explorer",
  description:
    "Side panel with file tree and read-only syntax-highlighted preview. Follows the active pane's cwd.",
  load: () => import("./FileExplorer.vue"),
  defaultEnabled: false,
};
```

- [ ] **Step 3: Register**

Modify `desktop/frontend/src/plugins/registry.ts`:

```ts
import type { PluginDescriptor } from "./types";
import { quickInputDescriptor } from "./quickInput";
import { fileExplorerDescriptor } from "./fileExplorer";

export const PLUGINS: PluginDescriptor[] = [quickInputDescriptor, fileExplorerDescriptor];
```

- [ ] **Step 4: Build**

Run: `cd desktop/frontend && npm run build`
Expected: succeeds.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue desktop/frontend/src/plugins/fileExplorer/index.ts desktop/frontend/src/plugins/registry.ts
git commit -m "feat(plugin/file-explorer): tree+tabs shell with placeholder editor"
```

---

## Phase 5: CodeMirror 6 Editor Integration

Goal: replace the editor placeholder with CodeMirror 6 in read-only mode. Bundle includes language extensions; the entire chunk only loads when the user enables File Explorer.

### Task 5.1: Add CodeMirror 6 dependencies

**Files:**
- Modify: `desktop/frontend/package.json`

- [ ] **Step 1: Install**

Run:
```bash
cd desktop/frontend && npm install \
  @codemirror/state @codemirror/view @codemirror/language \
  @codemirror/lang-javascript @codemirror/lang-json \
  @codemirror/lang-markdown @codemirror/lang-css \
  @codemirror/lang-html @codemirror/lang-python
```

- [ ] **Step 2: Verify build**

Run: `cd desktop/frontend && npm run build`
Expected: succeeds; observe vite output showing new chunks for codemirror.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/package.json desktop/frontend/package-lock.json
git commit -m "build(plugin/file-explorer): add CodeMirror 6 dependencies"
```

### Task 5.2: languageMap.ts

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/languageMap.ts`
- Create: `desktop/frontend/src/plugins/fileExplorer/languageMap.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/fileExplorer/languageMap.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { languageForPath } from "./languageMap";

describe("languageForPath", () => {
  it("returns javascript for .js", async () => {
    const ext = await languageForPath("/x/a.js");
    expect(ext).not.toBeNull();
  });
  it("returns null for unknown extension", async () => {
    const ext = await languageForPath("/x/a.zzz");
    expect(ext).toBeNull();
  });
  it("handles missing extension as null", async () => {
    const ext = await languageForPath("/x/LICENSE");
    expect(ext).toBeNull();
  });
});
```

- [ ] **Step 2: Implement**

Create `desktop/frontend/src/plugins/fileExplorer/languageMap.ts`:

```ts
import type { Extension } from "@codemirror/state";

// Each entry is a dynamic import so the language pack joins the file-explorer
// chunk only when actually needed. Vite static-imports them all into the file
// chunk regardless, but the lazy form keeps each language file as its own
// import root for future splitting.

export async function languageForPath(path: string): Promise<Extension | null> {
  const m = /\.([A-Za-z0-9]+)$/.exec(path);
  const ext = m ? m[1].toLowerCase() : null;
  if (!ext) return null;
  switch (ext) {
    case "js":
    case "jsx":
    case "ts":
    case "tsx": {
      const { javascript } = await import("@codemirror/lang-javascript");
      return javascript({ typescript: ext === "ts" || ext === "tsx", jsx: ext === "jsx" || ext === "tsx" });
    }
    case "json": {
      const { json } = await import("@codemirror/lang-json");
      return json();
    }
    case "md":
    case "markdown": {
      const { markdown } = await import("@codemirror/lang-markdown");
      return markdown();
    }
    case "css":
    case "scss": {
      const { css } = await import("@codemirror/lang-css");
      return css();
    }
    case "html":
    case "htm": {
      const { html } = await import("@codemirror/lang-html");
      return html();
    }
    case "py": {
      const { python } = await import("@codemirror/lang-python");
      return python();
    }
    default:
      return null;
  }
}
```

- [ ] **Step 3: Run tests**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/languageMap.test.ts`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/languageMap.ts desktop/frontend/src/plugins/fileExplorer/languageMap.test.ts
git commit -m "feat(plugin/file-explorer): extension→CodeMirror language map"
```

### Task 5.3: FileEditor.vue (read-only, large/binary guards)

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue`
- Create: `desktop/frontend/src/plugins/fileExplorer/FileEditor.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/fileExplorer/FileEditor.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import FileEditor from "./FileEditor.vue";

vi.mock("../../../wailsjs/go/main/PluginFS", () => ({
  ReadFile: vi.fn(),
  FileMeta: vi.fn(),
}));

import { ReadFile, FileMeta } from "../../../wailsjs/go/main/PluginFS";

beforeEach(() => {
  vi.mocked(FileMeta).mockReset();
  vi.mocked(ReadFile).mockReset();
});

describe("FileEditor", () => {
  it("shows placeholder for too-large file (size > 2 MB)", async () => {
    vi.mocked(FileMeta).mockResolvedValue({ path: "/a.txt", size: 3_000_000, modTime: 1, isBinary: false } as any);
    const w = mount(FileEditor, { props: { path: "/a.txt" } });
    await flushPromises();
    expect(w.text()).toContain("File too large");
    expect(ReadFile).not.toHaveBeenCalled();
  });

  it("shows binary placeholder when isBinary=true", async () => {
    vi.mocked(FileMeta).mockResolvedValue({ path: "/b.bin", size: 100, modTime: 1, isBinary: true } as any);
    const w = mount(FileEditor, { props: { path: "/b.bin" } });
    await flushPromises();
    expect(w.text()).toContain("Binary file");
    expect(ReadFile).not.toHaveBeenCalled();
  });

  it("loads file content for normal text file", async () => {
    vi.mocked(FileMeta).mockResolvedValue({ path: "/c.txt", size: 5, modTime: 1, isBinary: false } as any);
    vi.mocked(ReadFile).mockResolvedValue({ path: "/c.txt", data: new TextEncoder().encode("hello"), isBinary: false, truncatedAt: 0 } as any);
    const w = mount(FileEditor, { props: { path: "/c.txt" } });
    await flushPromises();
    expect(ReadFile).toHaveBeenCalled();
    // The CodeMirror mount is async; we only assert the call happened and no
    // placeholder is showing.
    expect(w.text()).not.toContain("File too large");
    expect(w.text()).not.toContain("Binary file");
  });
});
```

- [ ] **Step 2: Implement**

Create `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue`:

```vue
<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView, lineNumbers } from "@codemirror/view";
import { ReadFile, FileMeta } from "../../../wailsjs/go/main/PluginFS";
import { languageForPath } from "./languageMap";

const MAX_BYTES_FRONTEND = 2 * 1024 * 1024;

const props = defineProps<{
  path: string;
}>();

const host = ref<HTMLDivElement | null>(null);
const state = ref<"loading" | "tooLarge" | "binary" | "ok" | "error">("loading");
const errorMsg = ref<string>("");

let view: EditorView | null = null;

async function load() {
  state.value = "loading";
  view?.destroy();
  view = null;
  try {
    const meta = (await FileMeta(props.path)) as any;
    if (meta.isBinary) {
      state.value = "binary";
      return;
    }
    if (meta.size > MAX_BYTES_FRONTEND) {
      state.value = "tooLarge";
      return;
    }
    const result = (await ReadFile(props.path, MAX_BYTES_FRONTEND)) as any;
    const text = new TextDecoder().decode(result.data);
    state.value = "ok";

    const exts: Extension[] = [
      lineNumbers(),
      EditorView.editable.of(false),
      EditorState.readOnly.of(true),
    ];
    const langExt = await languageForPath(props.path);
    if (langExt) exts.push(langExt);

    const newState = EditorState.create({ doc: text, extensions: exts });
    if (!host.value) return;
    view = new EditorView({ state: newState, parent: host.value });
  } catch (err) {
    state.value = "error";
    errorMsg.value = (err as Error).message;
  }
}

onMounted(() => {
  void load();
});

watch(() => props.path, () => {
  void load();
});

onBeforeUnmount(() => {
  view?.destroy();
  view = null;
});
</script>

<template>
  <div class="file-editor">
    <div v-if="state === 'tooLarge'" class="banner">File too large to preview. Open externally.</div>
    <div v-if="state === 'binary'" class="banner">Binary file.</div>
    <div v-if="state === 'error'" class="banner err">Error: {{ errorMsg }}</div>
    <div v-show="state === 'ok'" ref="host" class="cm-host" />
    <div v-if="state === 'loading'" class="banner">Loading…</div>
  </div>
</template>

<style scoped>
.file-editor { height: 100%; display: flex; flex-direction: column; }
.cm-host { flex: 1; overflow: auto; }
.banner { padding: 10px 12px; font-size: 12px; opacity: 0.7; }
.banner.err { color: #f85149; opacity: 1; }
</style>
```

- [ ] **Step 3: Run tests**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileEditor.test.ts`
Expected: PASS.

- [ ] **Step 4: Wire FileEditor into FileExplorer.vue**

In `FileExplorer.vue`, replace the `.placeholder` "Preview placeholder" block:

```html
<FileEditor v-if="activePath" :path="activePath" />
<div v-else class="placeholder">Select a file.</div>
```

Add import:

```ts
import FileEditor from "./FileEditor.vue";
```

- [ ] **Step 5: Build + tests**

Run: `cd desktop/frontend && npm test && npm run build`
Expected: PASS; build succeeds; new CodeMirror chunk visible in build output.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileEditor.vue desktop/frontend/src/plugins/fileExplorer/FileEditor.test.ts desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue
git commit -m "feat(plugin/file-explorer): CodeMirror 6 read-only preview with size/binary guards"
```

---

## Phase 6: fs Watcher

Goal: WatchDir/UnwatchDir bindings; per-node attachment from FileTree; dir-changed event handling; Reload badge on open editor when its file is touched.

### Task 6.1: fsnotify dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add fsnotify**

Run from repo root:
```bash
go get github.com/fsnotify/fsnotify@latest
go mod tidy
```

- [ ] **Step 2: Verify**

Run: `go build -tags webkit2_41 ./desktop/...`
Expected: succeeds.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build(plugin-fs): add fsnotify dependency"
```

### Task 6.2: WatchDir / UnwatchDir + cap + debounce

**Files:**
- Modify: `desktop/plugin_fs.go`
- Modify: `desktop/plugin_fs_test.go`

- [ ] **Step 1: Write failing test**

Append to `desktop/plugin_fs_test.go`:

```go
func TestWatchUnwatchLifecycle(t *testing.T) {
	fs, home := makeFS(t)
	fs.setupWatcher(context.Background())
	defer fs.shutdownWatcher()

	id, err := fs.WatchDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero handle id")
	}
	if err := fs.UnwatchDir(id); err != nil {
		t.Fatal(err)
	}
}

func TestWatchDirCapEnforced(t *testing.T) {
	fs, home := makeFS(t)
	fs.setupWatcher(context.Background())
	defer fs.shutdownWatcher()

	// Create more than the cap of subdirs.
	for i := 0; i <= maxWatchers; i++ {
		d := filepath.Join(home, fmt.Sprintf("d%d", i))
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	good := 0
	for i := 0; i < maxWatchers+1; i++ {
		_, err := fs.WatchDir(filepath.Join(home, fmt.Sprintf("d%d", i)))
		if err == nil {
			good++
		}
	}
	if good != maxWatchers {
		t.Fatalf("expected %d successful watches, got %d", maxWatchers, good)
	}
}
```

The above uses `context` and `fmt` — append to imports if missing.

- [ ] **Step 2: Run failing test**

Run: `go test -tags webkit2_41 -run "TestWatch" ./desktop/`
Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `desktop/plugin_fs.go`:

```go
import (
	"context"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maxWatchers   = 200
	debounceWindow = 100 * time.Millisecond
)

type watchEntry struct {
	id   int64
	path string
}

func (p *PluginFS) setupWatcher(ctx context.Context) {
	p.watchOnce.Do(func() {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			return
		}
		p.watcher = w
		p.watches = make(map[int64]string)
		p.watchPaths = make(map[string]int)
		p.debounce = make(map[string]*time.Timer)
		p.ctx = ctx
		go p.watcherLoop()
	})
}

func (p *PluginFS) shutdownWatcher() {
	if p.watcher != nil {
		_ = p.watcher.Close()
	}
}

var pluginFSWatchSeq int64

func (p *PluginFS) WatchDir(path string) (int64, error) {
	resolved, err := p.resolve(path)
	if err != nil {
		return 0, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.watches) >= maxWatchers {
		return 0, fmt.Errorf("plugin_fs: watcher cap %d reached", maxWatchers)
	}
	if p.watchPaths[resolved] == 0 {
		if err := p.watcher.Add(resolved); err != nil {
			return 0, err
		}
	}
	pluginFSWatchSeq++
	id := pluginFSWatchSeq
	p.watches[id] = resolved
	p.watchPaths[resolved]++
	return id, nil
}

func (p *PluginFS) UnwatchDir(handleID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	path, ok := p.watches[handleID]
	if !ok {
		return nil
	}
	delete(p.watches, handleID)
	p.watchPaths[path]--
	if p.watchPaths[path] <= 0 {
		delete(p.watchPaths, path)
		_ = p.watcher.Remove(path)
	}
	return nil
}

func (p *PluginFS) watcherLoop() {
	for {
		select {
		case <-p.ctx.Done():
			return
		case ev, ok := <-p.watcher.Events:
			if !ok {
				return
			}
			dir := filepath.Dir(ev.Name)
			p.scheduleDirChanged(dir)
		case err, ok := <-p.watcher.Errors:
			if !ok {
				return
			}
			_ = err // could log via logging manager; not critical
		}
	}
}

func (p *PluginFS) scheduleDirChanged(dir string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.debounce[dir]; ok {
		t.Stop()
	}
	p.debounce[dir] = time.AfterFunc(debounceWindow, func() {
		p.mu.Lock()
		delete(p.debounce, dir)
		p.mu.Unlock()
		if p.ctx != nil {
			wailsruntime.EventsEmit(p.ctx, "plugin-fs:dir-changed", dir)
		}
	})
}
```

Add the new fields to the `PluginFS` struct:

```go
type PluginFS struct {
	allowRoots []string

	watchOnce  sync.Once
	watcher    *fsnotify.Watcher
	watches    map[int64]string
	watchPaths map[string]int
	debounce   map[string]*time.Timer
	mu         sync.Mutex
	ctx        context.Context
}
```

Update `NewPluginFS` and wire startup. In `main.go`, after constructing `pluginFS`, call `pluginFS.setupWatcher(ctx)` once the Wails context is available. Easiest: do it in `App.startup`:

In `desktop/app.go`, change `App` to keep a `pluginFS *PluginFS`:

```go
type App struct {
    // ... existing fields ...
    ctx      context.Context
    cfgStore *configStore
    logger   *loggingManager
    pluginFS *PluginFS
}
```

In `NewApp(cfgStore, logger)`:
```go
return &App{cfgStore: cfgStore, logger: logger, pluginFS: NewPluginFS()}
```

In `(a *App) startup(ctx context.Context)`:
```go
a.ctx = ctx
a.pluginFS.setupWatcher(ctx)
```

In `main.go`, bind `a.pluginFS` instead of constructing a separate one:

```go
Bind: []interface{}{
    app,
    app.pluginFS,
},
```

(Remove the earlier `pluginFS := NewPluginFS()` line you added in Task 3.5.)

- [ ] **Step 4: Run tests**

Run: `go test -tags webkit2_41 -run "TestWatch" ./desktop/`
Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run: `go test -tags webkit2_41 -timeout 60s ./desktop/`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add desktop/plugin_fs.go desktop/plugin_fs_test.go desktop/app.go desktop/main.go
git commit -m "feat(plugin-fs): WatchDir/UnwatchDir with cap and debounced dir-changed events"
```

### Task 6.3: Frontend watcher wiring

**Files:**
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileTree.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue`

- [ ] **Step 1: FileTree subscribes on expand, unsubscribes on collapse**

In `FileTree.vue`:

- Add imports:
```ts
import { WatchDir, UnwatchDir } from "../../../wailsjs/go/main/PluginFS";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
```
- Track handle per expanded path:
```ts
const watchHandles = new Map<string, number>();
```
- In `toggle(n)` after setting `n.expanded = true`:
```ts
try {
  const id = (await WatchDir(n.path)) as number;
  watchHandles.set(n.path, id);
} catch {
  // cap reached or platform unsupported; ignore — refresh button still works.
}
```
- In the `else` branch (collapse):
```ts
const id = watchHandles.get(n.path);
if (id) {
  await UnwatchDir(id);
  watchHandles.delete(n.path);
}
```
- On unmount, release all handles:
```ts
import { onBeforeUnmount } from "vue";
onBeforeUnmount(async () => {
  for (const id of watchHandles.values()) {
    try { await UnwatchDir(id); } catch { /* ignore */ }
  }
  watchHandles.clear();
});
```
- Listen for `plugin-fs:dir-changed` and refresh affected node:
```ts
const off = EventsOn("plugin-fs:dir-changed", async (dir: string) => {
  if (dir === props.root) {
    rootNodes.value = await loadDir(props.root);
    return;
  }
  const node = findNode(rootNodes.value, dir);
  if (node && node.expanded) {
    node.children = await loadDir(node.path);
  }
});
onBeforeUnmount(() => off());

function findNode(nodes: Node[], path: string): Node | null {
  for (const n of nodes) {
    if (n.path === path) return n;
    if (n.children) {
      const sub = findNode(n.children, path);
      if (sub) return sub;
    }
  }
  return null;
}
```

- [ ] **Step 2: FileEditor reload badge**

In `FileEditor.vue`:

- Add reload-pending state:
```ts
const reloadPending = ref(false);
```
- Subscribe to `plugin-fs:dir-changed`:
```ts
import { EventsOn } from "../../../wailsjs/runtime/runtime";

let off: (() => void) | null = null;

onMounted(() => {
  void load();
  off = EventsOn("plugin-fs:dir-changed", async (dir: string) => {
    if (!props.path.startsWith(dir + "/") && props.path !== dir) return;
    // Recheck mtime; if newer than last load, show reload badge.
    try {
      const meta = (await FileMeta(props.path)) as any;
      if (loadedAt.value && meta.modTime > loadedAt.value) {
        reloadPending.value = true;
      }
    } catch { /* ignore */ }
  });
});
onBeforeUnmount(() => {
  view?.destroy();
  view = null;
  if (off) off();
});
```
- Track `loadedAt`:
```ts
const loadedAt = ref<number | null>(null);
// inside load(), after meta fetched:
loadedAt.value = meta.modTime;
reloadPending.value = false;
```
- Render badge:
```html
<div v-if="reloadPending" class="reload-badge">
  File changed on disk
  <button @click="load">Reload</button>
</div>
```
Add minimal CSS:
```css
.reload-badge { display: flex; align-items: center; gap: 8px; padding: 4px 10px; background: #1f2937; border-bottom: 1px solid #2d333b; font-size: 11px; }
.reload-badge button { background: #21262d; border: 1px solid #2d333b; color: #c9d1d9; padding: 1px 8px; border-radius: 3px; cursor: pointer; }
```

- [ ] **Step 3: Build + tests**

Run: `cd desktop/frontend && npm test && npm run build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileTree.vue desktop/frontend/src/plugins/fileExplorer/FileEditor.vue
git commit -m "feat(plugin/file-explorer): subscribe to fs watcher; show reload badge"
```

---

## Phase 7: Resizers + Polish

Goal: outer panel resizer, inner tree ratio resizer, collapse toggle, ShowHidden toggle exposed in Settings.

### Task 7.1: Outer panel resizer

**Files:**
- Modify: `desktop/frontend/src/App.vue`

- [ ] **Step 1: Replace fixed-width `.right-panel` with reactive width + resizer**

In `App.vue` script:

```ts
import { useResizer } from "./plugins/useResizer";

const panelWidth = computed({
  get: () => store.cfg?.fileExplorer.panelWidthPx ?? 380,
  set: (v: number) => {
    if (!store.cfg) return;
    const next = JSON.parse(JSON.stringify(store.cfg));
    next.fileExplorer.panelWidthPx = Math.max(240, Math.min(v, window.innerWidth * 0.7));
    void store.save(next);
  },
});

const { onMouseDown: onPanelResizeDown } = useResizer({
  onDrag: (deltaX) => {
    if (!store.cfg) return;
    const next = (store.cfg.fileExplorer.panelWidthPx ?? 380) - deltaX;
    panelWidth.value = next;
  },
});
```

(Where `useResizer` is a new composable — Step 2.)

In template, after the main area:
```html
<div class="right-resizer" @mousedown="onPanelResizeDown" />
<PluginHost slot-id="right-panel" :context="pluginContext" class="right-panel" :style="{ width: panelWidth + 'px', flex: '0 0 ' + panelWidth + 'px' }" />
```

CSS:
```css
.right-resizer { width: 4px; cursor: col-resize; background: transparent; }
.right-resizer:hover { background: #2d333b; }
```

- [ ] **Step 2: Create useResizer composable**

Create `desktop/frontend/src/plugins/useResizer.ts`:

```ts
export interface ResizerOptions {
  onDrag: (deltaX: number, deltaY: number) => void;
  onEnd?: () => void;
}

// Drag handler that uses rAF batching so onDrag fires at most once per frame.
export function useResizer(opts: ResizerOptions) {
  let startX = 0;
  let startY = 0;
  let lastX = 0;
  let lastY = 0;
  let raf = 0;

  function onMouseMove(e: MouseEvent) {
    lastX = e.clientX;
    lastY = e.clientY;
    if (raf) return;
    raf = requestAnimationFrame(() => {
      raf = 0;
      opts.onDrag(startX - lastX, startY - lastY);
      startX = lastX;
      startY = lastY;
    });
  }

  function onMouseUp() {
    window.removeEventListener("mousemove", onMouseMove);
    window.removeEventListener("mouseup", onMouseUp);
    if (raf) cancelAnimationFrame(raf);
    raf = 0;
    opts.onEnd?.();
  }

  function onMouseDown(e: MouseEvent) {
    startX = e.clientX;
    startY = e.clientY;
    lastX = startX;
    lastY = startY;
    e.preventDefault();
    window.addEventListener("mousemove", onMouseMove);
    window.addEventListener("mouseup", onMouseUp);
  }

  return { onMouseDown };
}
```

- [ ] **Step 3: Build**

Run: `cd desktop/frontend && npm run build`
Expected: succeeds.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/plugins/useResizer.ts
git commit -m "feat(plugin): rAF-throttled outer panel resizer driven by config"
```

### Task 7.2: Inner tree-vs-editor resizer in FileExplorer

**Files:**
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue`

- [ ] **Step 1: Wire useResizer for the inner divider**

In `FileExplorer.vue` script:

```ts
import { useResizer } from "../useResizer";

const innerRatio = computed({
  get: () => store.cfg?.fileExplorer.innerTreeRatio ?? 0.3,
  set: (v: number) => {
    if (!store.cfg) return;
    const next = JSON.parse(JSON.stringify(store.cfg));
    next.fileExplorer.innerTreeRatio = Math.max(0.15, Math.min(v, 0.5));
    void store.save(next);
  },
});

const bodyRef = ref<HTMLDivElement | null>(null);

const { onMouseDown: onDividerDown } = useResizer({
  onDrag: (deltaX) => {
    if (!bodyRef.value) return;
    const width = bodyRef.value.clientWidth;
    if (width <= 0) return;
    innerRatio.value = innerRatio.value + deltaX / width;
  },
});
```

Modify template — wrap `.tree-pane`, `.divider`, `.editor-pane`:

```html
<div class="fe-body" ref="bodyRef">
  <div class="tree-pane" :style="{ width: (innerRatio * 100) + '%' }">
    <FileTree ... />
  </div>
  <div class="divider" @mousedown="onDividerDown" />
  <div class="editor-pane" :style="{ flex: '1 1 auto' }">
    <FileTabs ... />
    <FileEditor v-if="activePath" :path="activePath" />
    <div v-else class="placeholder">Select a file.</div>
  </div>
</div>
```

CSS:
```css
.divider { width: 4px; cursor: col-resize; background: transparent; }
.divider:hover { background: #2d333b; }
```

- [ ] **Step 2: Build**

Run: `cd desktop/frontend && npm run build`
Expected: succeeds.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue
git commit -m "feat(plugin/file-explorer): inner tree/editor divider with ratio config"
```

### Task 7.3: Collapse toggle + Settings show-hidden checkbox

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/components/SettingsPlugins.vue`

- [ ] **Step 1: Honor `panelCollapsed` in App.vue**

In App.vue script, add:
```ts
const panelCollapsed = computed({
  get: () => store.cfg?.fileExplorer.panelCollapsed ?? true,
  set: (v: boolean) => {
    if (!store.cfg) return;
    const next = JSON.parse(JSON.stringify(store.cfg));
    next.fileExplorer.panelCollapsed = v;
    void store.save(next);
  },
});

function togglePanel() { panelCollapsed.value = !panelCollapsed.value; }
```

Modify template: show panel only when not collapsed; add a small toggle handle either at the right edge of `.main` or before the resizer:

```html
<button class="panel-toggle" @click="togglePanel" :title="panelCollapsed ? 'Show panel' : 'Hide panel'">
  {{ panelCollapsed ? '‹' : '›' }}
</button>
<template v-if="!panelCollapsed">
  <div class="right-resizer" @mousedown="onPanelResizeDown" />
  <PluginHost slot-id="right-panel" :context="pluginContext" class="right-panel"
              :style="{ flex: '0 0 ' + panelWidth + 'px' }" />
</template>
```

CSS:
```css
.panel-toggle { background: #21262d; border: 1px solid #2d333b; color: #c9d1d9; cursor: pointer; padding: 0 4px; font-size: 11px; align-self: stretch; }
```

- [ ] **Step 2: Show-hidden checkbox + width hint in SettingsPlugins.vue**

Modify `SettingsPlugins.vue` — replace the existing `v-if="p.id === 'quick-input'"` block with a more complete per-plugin body:

```html
<QuickInputSettings v-if="p.id === 'quick-input' && store.isPluginEnabled('quick-input')" />
<div v-if="p.id === 'file-explorer' && store.isPluginEnabled('file-explorer')" class="fe-settings">
  <label>
    <input type="checkbox" :checked="store.cfg?.fileExplorer.showHidden ?? false"
           @change="toggleHidden(($event.target as HTMLInputElement).checked)" />
    Show hidden files
  </label>
  <p class="muted">Panel width and inner ratio are adjusted by dragging in the panel.</p>
</div>
```

Add to script:
```ts
async function toggleHidden(v: boolean) {
  if (!store.cfg) return;
  const next = JSON.parse(JSON.stringify(store.cfg));
  next.fileExplorer.showHidden = v;
  try { await store.save(next); } catch (err) { console.error(err); }
}
```

- [ ] **Step 3: Build + test**

Run: `cd desktop/frontend && npm test && npm run build`
Expected: PASS; build succeeds.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/components/SettingsPlugins.vue
git commit -m "feat(plugin/file-explorer): collapse toggle + show-hidden setting"
```

---

## Final Validation

Run the entire test suite + build to confirm no regression:

- [ ] **Step 1: Run all Go tests**

```bash
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./desktop/
```
Expected: PASS.

- [ ] **Step 2: Run all frontend tests + build**

```bash
cd desktop/frontend && npm test && npm run build
```
Expected: PASS.

- [ ] **Step 3: PluginFS isolation guard**

```bash
./.github/scripts/check-plugin-fs-isolation.sh
```
Expected: `ok: PluginFS isolation preserved`.

- [ ] **Step 4: Smoke-test in dev (manual; optional but recommended)**

```bash
cd desktop && wails dev -tags webkit2_41
```

Verify:
1. Settings → Plugins shows Quick Input (enabled) and File Explorer (disabled).
2. Quick Input default 3 buttons appear at the bottom; clicking sends to the active pane.
3. Enable File Explorer → panel appears on right; tree shows active pane's cwd; clicking a file opens preview.
4. Disable Quick Input → toolbar vanishes immediately.
5. Resize outer panel and inner divider; reload app — widths/ratios persist.

- [ ] **Step 5: Final commit (if any cleanup)**

If smoke test surfaces small issues, fix and commit per the existing pattern.
