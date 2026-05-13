# Terminal Themes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add built-in desktop terminal themes, selectable in Settings and persisted as the user's global preference.

**Architecture:** Go persists and validates only a theme id in `appConfig`; Vue owns the xterm theme objects and app CSS variables. `App.vue` keeps the selected theme reactive, passes it to all panes, and applies app shell variables so existing terminals update without reconnecting.

**Tech Stack:** Go/Wails v2 backend, Vue 3 + TypeScript frontend, xterm.js `ITheme`, Vitest source-level component tests, Go unit tests.

---

## File Structure

- Modify `desktop/config.go`: add supported terminal theme ids, `TerminalTheme` persistence field, validation helper, and default helper.
- Create `desktop/config_theme_test.go`: cover config defaulting and supported id validation.
- Modify `desktop/app.go`: expose `GetTerminalTheme` and `SetTerminalTheme` Wails methods.
- Create `desktop/app_theme_test.go`: cover API persistence, invalid id rejection, and unrelated config preservation.
- Create `desktop/frontend/src/lib/terminalThemes.ts`: frontend theme registry and fallback helper.
- Create `desktop/frontend/src/lib/terminalThemes.test.ts`: cover all built-in ids and fallback behavior.
- Modify `desktop/frontend/src/lib/api.ts`: add typed Wails wrappers for terminal theme preference.
- Modify `desktop/frontend/src/App.vue`: load current preference, apply app variables, and pass the xterm theme down.
- Modify `desktop/frontend/src/components/PaneGrid.vue`: accept and forward the xterm theme.
- Modify `desktop/frontend/src/components/TerminalView.vue`: accept theme prop, use it at creation, watch for updates, and remove hard-coded black backgrounds.
- Modify `desktop/frontend/src/components/TerminalView.test.ts`: source-level tests for theme prop/watch and CSS variable backgrounds.
- Modify `desktop/frontend/src/components/SettingsDialog.vue`: add immediate-save terminal theme select.
- Modify `desktop/frontend/src/components/SettingsDialog.test.ts`: source-level tests for theme select wiring.

---

### Task 1: Backend Config Theme Defaults

**Files:**
- Create: `desktop/config_theme_test.go`
- Modify: `desktop/config.go`

- [ ] **Step 1: Write the failing tests**

Create `desktop/config_theme_test.go`:

```go
package main

import "testing"

func TestTerminalThemeOrDefaultFallsBackToClassic(t *testing.T) {
	tests := []struct {
		name string
		cfg  appConfig
	}{
		{name: "empty", cfg: appConfig{}},
		{name: "unknown", cfg: appConfig{TerminalTheme: "gruvbox"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.TerminalThemeOrDefault(); got != terminalThemeClassic {
				t.Fatalf("TerminalThemeOrDefault() = %q; want %q", got, terminalThemeClassic)
			}
		})
	}
}

func TestTerminalThemeOrDefaultAcceptsSupportedThemes(t *testing.T) {
	for _, theme := range supportedTerminalThemes() {
		cfg := appConfig{TerminalTheme: theme}
		if got := cfg.TerminalThemeOrDefault(); got != theme {
			t.Fatalf("TerminalThemeOrDefault(%q) = %q; want %q", theme, got, theme)
		}
	}
}

func TestIsSupportedTerminalTheme(t *testing.T) {
	for _, theme := range supportedTerminalThemes() {
		if !isSupportedTerminalTheme(theme) {
			t.Fatalf("isSupportedTerminalTheme(%q) = false; want true", theme)
		}
	}
	if isSupportedTerminalTheme("") {
		t.Fatalf("isSupportedTerminalTheme(empty) = true; want false")
	}
	if isSupportedTerminalTheme("classic ") {
		t.Fatalf("isSupportedTerminalTheme(classic with space) = true; want false")
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test -tags webkit2_41 ./desktop/ -run 'TestTerminalTheme|TestIsSupportedTerminalTheme' -count=1
```

Expected: FAIL because `TerminalThemeOrDefault`, `terminalThemeClassic`, `supportedTerminalThemes`, and `isSupportedTerminalTheme` are undefined.

- [ ] **Step 3: Implement config support**

Patch `desktop/config.go`:

```go
const (
	terminalThemeClassic       = "classic"
	terminalThemeNord          = "nord"
	terminalThemeSolarizedDark = "solarized-dark"
	terminalThemeDaylight      = "daylight"
)

var terminalThemes = map[string]struct{}{
	terminalThemeClassic:       {},
	terminalThemeNord:          {},
	terminalThemeSolarizedDark: {},
	terminalThemeDaylight:      {},
}
```

Add the field to `appConfig`:

```go
	// TerminalTheme is the user's global desktop terminal theme preference.
	// Unknown values fall back to classic so older configs remain usable.
	TerminalTheme string `json:"terminal_theme,omitempty"`
```

Add helpers below `RemotePermissionOrDefault`:

```go
func (c appConfig) TerminalThemeOrDefault() string {
	if isSupportedTerminalTheme(c.TerminalTheme) {
		return c.TerminalTheme
	}
	return terminalThemeClassic
}

func isSupportedTerminalTheme(theme string) bool {
	_, ok := terminalThemes[theme]
	return ok
}

func supportedTerminalThemes() []string {
	return []string{
		terminalThemeClassic,
		terminalThemeNord,
		terminalThemeSolarizedDark,
		terminalThemeDaylight,
	}
}
```

- [ ] **Step 4: Run the tests and verify they pass**

Run:

```bash
go test -tags webkit2_41 ./desktop/ -run 'TestTerminalTheme|TestIsSupportedTerminalTheme' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/config.go desktop/config_theme_test.go
git commit -m "add terminal theme config defaults"
```

---

### Task 2: Backend Theme Preference API

**Files:**
- Create: `desktop/app_theme_test.go`
- Modify: `desktop/app.go`

- [ ] **Step 1: Write the failing tests**

Create `desktop/app_theme_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/proto"
)

func newThemeTestApp(t *testing.T, cfg appConfig) *App {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	// Keep Linux CI and developer machines away from real user config dirs.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	return &App{cfgStore: &configStore{cfg: cfg}}
}

func TestGetTerminalThemeReturnsDefault(t *testing.T) {
	a := newThemeTestApp(t, appConfig{})
	if got := a.GetTerminalTheme(); got != terminalThemeClassic {
		t.Fatalf("GetTerminalTheme() = %q; want %q", got, terminalThemeClassic)
	}
}

func TestSetTerminalThemePersistsAndPreservesConfig(t *testing.T) {
	autoCheck := false
	a := newThemeTestApp(t, appConfig{
		RelayURL:            "wss://relay.example.com",
		RelayToken:          "secret-token",
		AllowInsecureRelay:  true,
		RemotePermission:    proto.RemotePermissionControl,
		AutoCheckUpdates:    &autoCheck,
		LastCheckAt:         123,
		SkipVersion:         "v9.9.9",
		TerminalTheme:       terminalThemeClassic,
	})

	if err := a.SetTerminalTheme(terminalThemeNord); err != nil {
		t.Fatalf("SetTerminalTheme() error = %v", err)
	}
	if got := a.GetTerminalTheme(); got != terminalThemeNord {
		t.Fatalf("GetTerminalTheme() = %q; want %q", got, terminalThemeNord)
	}

	cfg := a.cfgStore.Get()
	if cfg.RelayURL != "wss://relay.example.com" ||
		cfg.RelayToken != "secret-token" ||
		!cfg.AllowInsecureRelay ||
		cfg.RemotePermission != proto.RemotePermissionControl ||
		cfg.AutoCheckUpdates != &autoCheck ||
		cfg.LastCheckAt != 123 ||
		cfg.SkipVersion != "v9.9.9" {
		t.Fatalf("SetTerminalTheme changed unrelated config: %#v", cfg)
	}
	if cfg.TerminalTheme != terminalThemeNord {
		t.Fatalf("TerminalTheme = %q; want %q", cfg.TerminalTheme, terminalThemeNord)
	}

	data, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatalf("ReadFile(configPath()) error = %v", err)
	}
	if !strings.Contains(string(data), `"terminal_theme": "nord"`) {
		t.Fatalf("persisted config missing terminal theme: %s", data)
	}
}

func TestSetTerminalThemeRejectsUnknownTheme(t *testing.T) {
	a := newThemeTestApp(t, appConfig{TerminalTheme: terminalThemeClassic})

	if err := a.SetTerminalTheme("gruvbox"); err == nil {
		t.Fatalf("SetTerminalTheme(unknown) error = nil; want error")
	}
	if got := a.GetTerminalTheme(); got != terminalThemeClassic {
		t.Fatalf("GetTerminalTheme() = %q; want %q", got, terminalThemeClassic)
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
go test -tags webkit2_41 ./desktop/ -run 'Test(Get|Set)TerminalTheme' -count=1
```

Expected: FAIL because `GetTerminalTheme` and `SetTerminalTheme` are undefined.

- [ ] **Step 3: Implement Wails methods**

Add to `desktop/app.go` after `SetRelayConfig`:

```go
// GetTerminalTheme returns the user's global terminal theme preference.
func (a *App) GetTerminalTheme() string {
	if a.cfgStore == nil {
		return terminalThemeClassic
	}
	return a.cfgStore.Get().TerminalThemeOrDefault()
}

// SetTerminalTheme persists the user's global terminal theme preference.
func (a *App) SetTerminalTheme(theme string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	theme = strings.TrimSpace(theme)
	if !isSupportedTerminalTheme(theme) {
		return fmt.Errorf("bad terminal theme: %s", theme)
	}
	cfg := a.cfgStore.Get()
	cfg.TerminalTheme = theme
	return a.cfgStore.Set(cfg)
}
```

- [ ] **Step 4: Run the tests and verify they pass**

Run:

```bash
gofmt -w desktop/config.go desktop/config_theme_test.go desktop/app.go desktop/app_theme_test.go
go test -tags webkit2_41 ./desktop/ -run 'TestTerminalTheme|TestIsSupportedTerminalTheme|Test(Get|Set)TerminalTheme' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/app.go desktop/app_theme_test.go
git commit -m "add terminal theme preference api"
```

---

### Task 3: Frontend Theme Registry

**Files:**
- Create: `desktop/frontend/src/lib/terminalThemes.ts`
- Create: `desktop/frontend/src/lib/terminalThemes.test.ts`

- [ ] **Step 1: Write the failing tests**

Create `desktop/frontend/src/lib/terminalThemes.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import {
  DEFAULT_TERMINAL_THEME_ID,
  TERMINAL_THEMES,
  getTerminalTheme,
  isTerminalThemeID,
  type TerminalThemeID,
} from "./terminalThemes";

describe("terminal themes", () => {
  test("registers the built-in themes in settings order", () => {
    expect(TERMINAL_THEMES.map((theme) => theme.id)).toEqual([
      "classic",
      "nord",
      "solarized-dark",
      "daylight",
    ]);
  });

  test("falls back to classic for unknown ids", () => {
    expect(DEFAULT_TERMINAL_THEME_ID).toBe("classic");
    expect(getTerminalTheme("gruvbox").id).toBe("classic");
    expect(getTerminalTheme("").id).toBe("classic");
  });

  test("identifies valid theme ids", () => {
    expect(isTerminalThemeID("nord")).toBe(true);
    expect(isTerminalThemeID("nord ") ).toBe(false);
    expect(isTerminalThemeID("unknown")).toBe(false);

    const typed: TerminalThemeID = getTerminalTheme("solarized-dark").id;
    expect(typed).toBe("solarized-dark");
  });

  test("themes include xterm colors and app variables", () => {
    for (const theme of TERMINAL_THEMES) {
      expect(theme.xtermTheme.background).toMatch(/^#/);
      expect(theme.xtermTheme.foreground).toMatch(/^#/);
      expect(theme.xtermTheme.cursor).toMatch(/^#/);
      expect(theme.appVars["--bg"]).toMatch(/^#/);
      expect(theme.appVars["--panel"]).toMatch(/^#/);
      expect(theme.appVars["--terminal-bg"]).toMatch(/^#/);
    }
  });
});
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
cd desktop/frontend
npm run test -- src/lib/terminalThemes.test.ts
```

Expected: FAIL because `terminalThemes.ts` does not exist.

- [ ] **Step 3: Implement the registry**

Create `desktop/frontend/src/lib/terminalThemes.ts`:

```ts
import type { ITheme } from "xterm";

export type TerminalThemeID = "classic" | "nord" | "solarized-dark" | "daylight";

export interface TerminalThemeDefinition {
  id: TerminalThemeID;
  label: string;
  description: string;
  xtermTheme: ITheme;
  appVars: Record<string, string>;
}

export const DEFAULT_TERMINAL_THEME_ID: TerminalThemeID = "classic";

export const TERMINAL_THEMES: TerminalThemeDefinition[] = [
  {
    id: "classic",
    label: "AT Term Classic",
    description: "Current black terminal look.",
    xtermTheme: {
      background: "#000000",
      foreground: "#c9d1d9",
      cursor: "#c9d1d9",
      cursorAccent: "#000000",
      selectionBackground: "#264f78",
      black: "#000000",
      red: "#f85149",
      green: "#3fb950",
      yellow: "#d29922",
      blue: "#58a6ff",
      magenta: "#bc8cff",
      cyan: "#39c5cf",
      white: "#b1bac4",
      brightBlack: "#6e7681",
      brightRed: "#ff7b72",
      brightGreen: "#56d364",
      brightYellow: "#e3b341",
      brightBlue: "#79c0ff",
      brightMagenta: "#d2a8ff",
      brightCyan: "#56d4dd",
      brightWhite: "#f0f6fc",
    },
    appVars: {
      "--bg": "#0d1117",
      "--panel": "#161b22",
      "--border": "#30363d",
      "--fg": "#c9d1d9",
      "--fg-dim": "#8b949e",
      "--accent": "#58a6ff",
      "--good": "#3fb950",
      "--bad": "#f85149",
      "--terminal-bg": "#000000",
      "--terminal-grid": "#11161d",
      "--terminal-overlay": "rgba(13, 17, 23, 0.85)",
    },
  },
  {
    id: "nord",
    label: "Nord / Arctic",
    description: "Cool low-contrast dark palette.",
    xtermTheme: {
      background: "#2e3440",
      foreground: "#d8dee9",
      cursor: "#d8dee9",
      cursorAccent: "#2e3440",
      selectionBackground: "#434c5e",
      black: "#3b4252",
      red: "#bf616a",
      green: "#a3be8c",
      yellow: "#ebcb8b",
      blue: "#81a1c1",
      magenta: "#b48ead",
      cyan: "#88c0d0",
      white: "#e5e9f0",
      brightBlack: "#4c566a",
      brightRed: "#bf616a",
      brightGreen: "#a3be8c",
      brightYellow: "#ebcb8b",
      brightBlue: "#81a1c1",
      brightMagenta: "#b48ead",
      brightCyan: "#8fbcbb",
      brightWhite: "#eceff4",
    },
    appVars: {
      "--bg": "#242933",
      "--panel": "#2e3440",
      "--border": "#4c566a",
      "--fg": "#d8dee9",
      "--fg-dim": "#9aa7bd",
      "--accent": "#88c0d0",
      "--good": "#a3be8c",
      "--bad": "#bf616a",
      "--terminal-bg": "#2e3440",
      "--terminal-grid": "#242933",
      "--terminal-overlay": "rgba(36, 41, 51, 0.88)",
    },
  },
  {
    id: "solarized-dark",
    label: "Solarized Dark",
    description: "Classic warm low-contrast dark palette.",
    xtermTheme: {
      background: "#002b36",
      foreground: "#839496",
      cursor: "#93a1a1",
      cursorAccent: "#002b36",
      selectionBackground: "#073642",
      black: "#073642",
      red: "#dc322f",
      green: "#859900",
      yellow: "#b58900",
      blue: "#268bd2",
      magenta: "#d33682",
      cyan: "#2aa198",
      white: "#eee8d5",
      brightBlack: "#002b36",
      brightRed: "#cb4b16",
      brightGreen: "#586e75",
      brightYellow: "#657b83",
      brightBlue: "#839496",
      brightMagenta: "#6c71c4",
      brightCyan: "#93a1a1",
      brightWhite: "#fdf6e3",
    },
    appVars: {
      "--bg": "#00212b",
      "--panel": "#073642",
      "--border": "#28535f",
      "--fg": "#93a1a1",
      "--fg-dim": "#657b83",
      "--accent": "#268bd2",
      "--good": "#859900",
      "--bad": "#dc322f",
      "--terminal-bg": "#002b36",
      "--terminal-grid": "#001e26",
      "--terminal-overlay": "rgba(0, 33, 43, 0.9)",
    },
  },
  {
    id: "daylight",
    label: "Daylight",
    description: "Light palette for bright environments.",
    xtermTheme: {
      background: "#faf4e8",
      foreground: "#403832",
      cursor: "#403832",
      cursorAccent: "#faf4e8",
      selectionBackground: "#d9c7a8",
      black: "#403832",
      red: "#c94f4f",
      green: "#4d8a3f",
      yellow: "#b9831d",
      blue: "#2f6db3",
      magenta: "#8b5fa8",
      cyan: "#2f8f8a",
      white: "#ede0cc",
      brightBlack: "#7a6d61",
      brightRed: "#d56565",
      brightGreen: "#5d9f4d",
      brightYellow: "#c9922c",
      brightBlue: "#3d7fcf",
      brightMagenta: "#9b6db8",
      brightCyan: "#3aa39d",
      brightWhite: "#fffaf0",
    },
    appVars: {
      "--bg": "#f4ead8",
      "--panel": "#fff8ed",
      "--border": "#d8c7ad",
      "--fg": "#403832",
      "--fg-dim": "#776b5f",
      "--accent": "#2f6db3",
      "--good": "#4d8a3f",
      "--bad": "#c94f4f",
      "--terminal-bg": "#faf4e8",
      "--terminal-grid": "#e5d6bd",
      "--terminal-overlay": "rgba(255, 248, 237, 0.92)",
    },
  },
];

export function isTerminalThemeID(id: string): id is TerminalThemeID {
  return TERMINAL_THEMES.some((theme) => theme.id === id);
}

export function getTerminalTheme(id: string): TerminalThemeDefinition {
  return TERMINAL_THEMES.find((theme) => theme.id === id) ?? TERMINAL_THEMES[0];
}
```

- [ ] **Step 4: Run the tests and verify they pass**

Run:

```bash
cd desktop/frontend
npm run test -- src/lib/terminalThemes.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/terminalThemes.ts desktop/frontend/src/lib/terminalThemes.test.ts
git commit -m "add frontend terminal theme registry"
```

---

### Task 4: Frontend API And App Theme State

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/components/PaneGrid.vue`

- [ ] **Step 1: Write failing source-level tests for API and app wiring**

Create `desktop/frontend/src/App.theme.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import appSource from "./App.vue?raw";
import apiSource from "./lib/api.ts?raw";
import paneSource from "./components/PaneGrid.vue?raw";

describe("App terminal theme wiring", () => {
  test("api exposes terminal theme preference bindings", () => {
    expect(apiSource).toContain("GetTerminalTheme(): Promise<string>");
    expect(apiSource).toContain("SetTerminalTheme(themeID: string): Promise<void>");
    expect(apiSource).toContain("export function getTerminalThemePreference()");
    expect(apiSource).toContain("export function setTerminalThemePreference(themeID: string)");
  });

  test("App loads theme preference and applies app variables", () => {
    expect(appSource).toContain("getTerminalThemePreference");
    expect(appSource).toContain("currentTerminalThemeID");
    expect(appSource).toContain("currentTerminalTheme");
    expect(appSource).toContain("themeStyle");
    expect(appSource).toContain(":style=\"themeStyle\"");
  });

  test("App passes the xterm theme to PaneGrid and PaneGrid forwards it", () => {
    expect(appSource).toContain(":terminal-theme=\"currentTerminalTheme.xtermTheme\"");
    expect(paneSource).toContain("terminalTheme: TerminalThemeDefinition[\"xtermTheme\"]");
    expect(paneSource).toContain(":theme=\"terminalTheme\"");
  });
});
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
cd desktop/frontend
npm run test -- src/App.theme.test.ts
```

Expected: FAIL because the API wrappers and theme state do not exist.

- [ ] **Step 3: Add API wrappers**

Patch `desktop/frontend/src/lib/api.ts`:

```ts
interface AppBindings {
  GetEndpoint(): Promise<Endpoint>;
  GetHostInfo(): Promise<HostInfo>;
  NewSession(req: NewSessionReq): Promise<NewSessionResp>;
  CloseSession(sessionID: string): Promise<void>;
  ListShells(): Promise<string[]>;
  GetRelayConfig(): Promise<RelayConfig>;
  SetRelayConfig(cfg: RelayConfig): Promise<void>;
  GetTerminalTheme(): Promise<string>;
  SetTerminalTheme(themeID: string): Promise<void>;
  GetUpdateState(): Promise<UpdateState>;
  CheckUpdate(): Promise<void>;
  StartDownload(): Promise<void>;
  InstallUpdate(): Promise<void>;
  GetAutoCheckUpdates(): Promise<boolean>;
  SetAutoCheckUpdates(enabled: boolean): Promise<void>;
}
```

Add exports near the relay config helpers:

```ts
export function getTerminalThemePreference(): Promise<string> {
  return bindings().GetTerminalTheme();
}

export function setTerminalThemePreference(themeID: string): Promise<void> {
  return bindings().SetTerminalTheme(themeID);
}
```

- [ ] **Step 4: Add App theme state**

Patch imports in `desktop/frontend/src/App.vue`:

```ts
import {
  closeSession,
  getEndpoint,
  getHostInfo,
  getRelayConfig,
  getTerminalThemePreference,
  getUpdateState,
  listShells,
  newSession,
} from "./lib/api";
import {
  DEFAULT_TERMINAL_THEME_ID,
  getTerminalTheme,
  type TerminalThemeID,
} from "./lib/terminalThemes";
```

Add state near other top-level refs:

```ts
const currentTerminalThemeID = ref<TerminalThemeID>(DEFAULT_TERMINAL_THEME_ID);
const currentTerminalTheme = computed(() => getTerminalTheme(currentTerminalThemeID.value));
const themeStyle = computed(() => currentTerminalTheme.value.appVars);
```

Add a loader function:

```ts
async function refreshTerminalTheme() {
  const themeID = await getTerminalThemePreference();
  currentTerminalThemeID.value = getTerminalTheme(themeID).id;
}

function onTerminalThemeChanged(themeID: string) {
  currentTerminalThemeID.value = getTerminalTheme(themeID).id;
}
```

In the existing startup `onMounted` flow, call `await refreshTerminalTheme()` before setting `status.value = "ready"` or before opening the first session. If the code currently uses `Promise.all`, include `getTerminalThemePreference()` in that startup read and assign through `getTerminalTheme(themeID).id`.

Change the root template:

```vue
<div class="app" :style="themeStyle">
```

Pass props to Settings and PaneGrid:

```vue
<PaneGrid
  v-for="t in tabs"
  v-show="t.id === currentTabId"
  :key="t.id"
  :tab="t"
  :endpoint-for="endpointFor"
  :session-info-for="paneSessionInfo"
  :active="t.id === currentTabId"
  :terminal-theme="currentTerminalTheme.xtermTheme"
  @set-active-pane="(idx) => (t.activePaneIdx = idx)"
  @close-pane="(idx) => closePaneAt(t, idx)"
/>
```

```vue
<SettingsDialog
  v-if="showSettings"
  :local-session-count="localSessionCount"
  :remote-session-count="remoteSessionCount"
  :terminal-theme-id="currentTerminalThemeID"
  @terminal-theme-changed="onTerminalThemeChanged"
  @relay-config-changed="refreshRelayConfig"
  @close="showSettings = false; refreshRelayConfig()"
/>
```

- [ ] **Step 5: Wire PaneGrid prop**

Patch `desktop/frontend/src/components/PaneGrid.vue` imports:

```ts
import type { TerminalThemeDefinition } from "../lib/terminalThemes";
```

Add prop:

```ts
  terminalTheme: TerminalThemeDefinition["xtermTheme"];
```

Forward it:

```vue
<TerminalView
  v-if="pane.sessionId && endpointFor(pane)"
  :endpoint="endpointFor(pane)!"
  :session-id="pane.sessionId"
  :active="active"
  :focused="active && idx === tab.activePaneIdx"
  :expected-cols="sessionInfoFor(pane)?.cols"
  :expected-rows="sessionInfoFor(pane)?.rows"
  :avoid-top-right-badge="pane.remote"
  :theme="terminalTheme"
/>
```

- [ ] **Step 6: Run tests and type check this slice**

Run:

```bash
cd desktop/frontend
npm run test -- src/App.theme.test.ts
npm run build
```

Expected: tests PASS and build PASS after Settings/TerminalView missing prop errors are fixed in later tasks. If build fails only because `SettingsDialog` or `TerminalView` does not yet define the new props/events, continue to Tasks 5 and 6 before rerunning the full build.

- [ ] **Step 7: Commit after buildable wiring is complete**

Do this commit after Tasks 5 and 6 make the frontend build pass:

```bash
git add desktop/frontend/src/lib/api.ts desktop/frontend/src/App.vue desktop/frontend/src/components/PaneGrid.vue desktop/frontend/src/App.theme.test.ts
git commit -m "wire terminal theme state through app"
```

---

### Task 5: TerminalView Theme Updates

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Modify: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 1: Add failing TerminalView tests**

Append to `desktop/frontend/src/components/TerminalView.test.ts`:

```ts
describe("TerminalView themes", () => {
  test("accepts a theme prop and uses it when creating xterm", () => {
    expect(source).toContain("theme: ITheme");
    expect(source).toMatch(/new Terminal\(\{[\s\S]*theme:\s*props\.theme/);
  });

  test("updates existing terminals without recreating the connection", () => {
    expect(source).toMatch(/watch\(\s*\(\)\s*=>\s*props\.theme/);
    expect(source).toContain("term.options.theme = theme");
    expect(source).not.toContain("watch(\n  () => props.theme,\n  () => {\n    ensureTerm()");
  });

  test("uses theme variables for terminal backgrounds", () => {
    expect(styleBlockFor(".term-view")).toMatch(/background\s*:\s*var\(--terminal-bg\)/);
    expect(source).toMatch(/background:\s*var\(--terminal-overlay\)/);
  });
});
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
cd desktop/frontend
npm run test -- src/components/TerminalView.test.ts
```

Expected: FAIL because `theme` prop and watcher do not exist.

- [ ] **Step 3: Implement theme prop and watcher**

Patch `desktop/frontend/src/components/TerminalView.vue` imports:

```ts
import type { ITheme } from "xterm";
```

Add prop:

```ts
    theme: ITheme;
```

Change terminal creation:

```ts
    theme: props.theme,
```

Add watcher near the existing active watcher:

```ts
watch(
  () => props.theme,
  (theme) => {
    if (term) term.options.theme = theme;
  }
);
```

Patch scoped CSS:

```css
.term-view {
  position: absolute;
  inset: 0;
  background: var(--terminal-bg);
  overflow: hidden;
}
```

```css
.overlay {
  position: absolute;
  top: 8px;
  right: 12px;
  background: var(--terminal-overlay);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 4px 10px;
  font-size: 12px;
  color: var(--fg-dim);
  pointer-events: none;
}
```

- [ ] **Step 4: Run the tests and verify they pass**

Run:

```bash
cd desktop/frontend
npm run test -- src/components/TerminalView.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git commit -m "apply terminal themes to xterm views"
```

---

### Task 6: Settings Theme Selector

**Files:**
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Modify: `desktop/frontend/src/components/SettingsDialog.test.ts`

- [ ] **Step 1: Add failing Settings tests**

Append to `desktop/frontend/src/components/SettingsDialog.test.ts`:

```ts
describe("SettingsDialog terminal theme selector", () => {
  test("declares theme props and change event", () => {
    expect(source).toContain("terminalThemeId: string");
    expect(source).toContain('(e: "terminal-theme-changed", themeID: string): void');
  });

  test("loads theme registry and saves theme changes immediately", () => {
    expect(source).toContain("TERMINAL_THEMES");
    expect(source).toContain("setTerminalThemePreference");
    expect(source).toContain("async function onTerminalThemeChange");
    expect(source).toContain('emit("terminal-theme-changed", nextTheme)');
  });

  test("renders a terminal theme select independent of relay save", () => {
    expect(source).toContain("terminal theme");
    expect(source).toContain('v-model="selectedTerminalTheme"');
    expect(source).toContain('@change="onTerminalThemeChange"');
  });
});
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
cd desktop/frontend
npm run test -- src/components/SettingsDialog.test.ts
```

Expected: FAIL because the theme selector is not implemented.

- [ ] **Step 3: Add imports, props, state, and handler**

Patch `desktop/frontend/src/components/SettingsDialog.vue` imports:

```ts
import {
  checkUpdate,
  getAutoCheckUpdates,
  getRelayConfig,
  getTerminalThemePreference,
  getUpdateState,
  installUpdate,
  setAutoCheckUpdates,
  setRelayConfig,
  setTerminalThemePreference,
  startDownload,
  type UpdateState,
} from "../lib/api";
import {
  TERMINAL_THEMES,
  getTerminalTheme,
} from "../lib/terminalThemes";
```

Update props:

```ts
const props = defineProps<{
  localSessionCount: number;
  remoteSessionCount: number;
  terminalThemeId: string;
}>();
```

Update emits:

```ts
const emit = defineEmits<{
  (e: "close"): void;
  (e: "relay-config-changed"): void;
  (e: "terminal-theme-changed", themeID: string): void;
}>();
```

Add state near existing relay refs:

```ts
const selectedTerminalTheme = ref(getTerminalTheme(props.terminalThemeId).id);
const persistedTerminalTheme = ref(getTerminalTheme(props.terminalThemeId).id);
```

In `onMounted`, include `getTerminalThemePreference()` in the initial `Promise.all`:

```ts
    const [cfg, st, ac, themeID] = await Promise.all([
      getRelayConfig(),
      getUpdateState(),
      getAutoCheckUpdates(),
      getTerminalThemePreference(),
    ]);
```

Then assign:

```ts
    selectedTerminalTheme.value = getTerminalTheme(themeID).id;
    persistedTerminalTheme.value = selectedTerminalTheme.value;
```

Add handler:

```ts
async function onTerminalThemeChange() {
  const nextTheme = getTerminalTheme(selectedTerminalTheme.value).id;
  const previousTheme = persistedTerminalTheme.value;
  selectedTerminalTheme.value = nextTheme;
  error.value = "";
  try {
    await setTerminalThemePreference(nextTheme);
    persistedTerminalTheme.value = nextTheme;
    emit("terminal-theme-changed", nextTheme);
  } catch (e: any) {
    selectedTerminalTheme.value = previousTheme;
    emit("terminal-theme-changed", previousTheme);
    error.value = e?.message ?? String(e);
  }
}
```

- [ ] **Step 4: Render the selector**

Add this block after the introductory hint and before relay URL fields:

```vue
<label>terminal theme</label>
<select
  v-model="selectedTerminalTheme"
  :disabled="saving"
  @change="onTerminalThemeChange"
>
  <option
    v-for="theme in TERMINAL_THEMES"
    :key="theme.id"
    :value="theme.id"
  >
    {{ theme.label }} — {{ theme.description }}
  </option>
</select>
<p class="hint">
  Applies to all terminal panes immediately and is saved as your local desktop preference.
</p>
```

- [ ] **Step 5: Run Settings tests**

Run:

```bash
cd desktop/frontend
npm run test -- src/components/SettingsDialog.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/SettingsDialog.vue desktop/frontend/src/components/SettingsDialog.test.ts
git commit -m "add terminal theme selector"
```

---

### Task 7: App Shell Theme CSS

**Files:**
- Modify: `desktop/frontend/src/style.css`
- Modify: `desktop/frontend/src/components/PaneGrid.vue`

- [ ] **Step 1: Add failing source-level tests for CSS variable use**

Create `desktop/frontend/src/theme-css.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import styleSource from "./style.css?raw";
import paneSource from "./components/PaneGrid.vue?raw";

describe("theme css variables", () => {
  test("root defines terminal fallback variables", () => {
    expect(styleSource).toContain("--terminal-bg: #000000");
    expect(styleSource).toContain("--terminal-grid: #11161d");
    expect(styleSource).toContain("--terminal-overlay: rgba(13, 17, 23, 0.85)");
  });

  test("pane grid and cells use theme terminal backgrounds", () => {
    expect(paneSource).toContain("background: var(--terminal-grid)");
    expect(paneSource).toContain("background: var(--terminal-bg)");
  });
});
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
cd desktop/frontend
npm run test -- src/theme-css.test.ts
```

Expected: FAIL because terminal fallback vars and pane backgrounds are still hard-coded or missing.

- [ ] **Step 3: Add CSS fallback variables**

Patch `desktop/frontend/src/style.css` `:root`:

```css
  --terminal-bg: #000000;
  --terminal-grid: #11161d;
  --terminal-overlay: rgba(13, 17, 23, 0.85);
```

- [ ] **Step 4: Update pane backgrounds**

Patch `desktop/frontend/src/components/PaneGrid.vue`:

```css
.pane-grid {
  position: absolute;
  inset: 0;
  display: grid;
  gap: 2px;
  background: var(--terminal-grid);
}
```

```css
.cell {
  position: relative;
  background: var(--terminal-bg);
  overflow: hidden;
}
```

- [ ] **Step 5: Run CSS tests**

Run:

```bash
cd desktop/frontend
npm run test -- src/theme-css.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/style.css desktop/frontend/src/components/PaneGrid.vue desktop/frontend/src/theme-css.test.ts
git commit -m "theme terminal shell backgrounds"
```

---

### Task 8: Full Verification And Fixups

**Files:**
- Potentially modify any files touched above for type, lint, or test fixes.

- [ ] **Step 1: Run all frontend unit tests**

Run:

```bash
cd desktop/frontend
npm run test
```

Expected: PASS.

- [ ] **Step 2: Run frontend type-check and build**

Run:

```bash
cd desktop/frontend
npm run build
```

Expected: PASS with no TypeScript errors.

- [ ] **Step 3: Run desktop Go tests**

Run:

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test -tags webkit2_41 -timeout 60s ./desktop/
```

Expected: PASS.

- [ ] **Step 4: Run project Go vet**

Run:

```bash
go vet -tags webkit2_41 ./...
```

Expected: PASS.

- [ ] **Step 5: Optional manual browser/dev verification**

Run the app:

```bash
cd desktop
wails dev -tags webkit2_41
```

Manual checks:

- Open two panes.
- Open Settings and change terminal theme to `Nord / Arctic`.
- Confirm both panes update colors without reconnecting or clearing scrollback.
- Close and restart app.
- Confirm `Nord / Arctic` remains selected.
- Change to `Daylight`.
- Confirm topbar, dialog, grid gutters, overlays, and terminal text remain readable.

- [ ] **Step 6: Final status check**

Run:

```bash
git status --short
```

Expected: only intentional changes remain. If verification required fixups, commit them:

```bash
git add desktop/config.go desktop/config_theme_test.go desktop/app.go desktop/app_theme_test.go desktop/frontend/src/lib/api.ts desktop/frontend/src/lib/terminalThemes.ts desktop/frontend/src/lib/terminalThemes.test.ts desktop/frontend/src/App.vue desktop/frontend/src/App.theme.test.ts desktop/frontend/src/components/PaneGrid.vue desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts desktop/frontend/src/components/SettingsDialog.vue desktop/frontend/src/components/SettingsDialog.test.ts desktop/frontend/src/style.css desktop/frontend/src/theme-css.test.ts
git commit -m "fix terminal theme verification issues"
```
