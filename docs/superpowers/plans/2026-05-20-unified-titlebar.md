# Unified title bar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge the desktop app's top toolbar (`<header class="topbar">` in `App.vue`) into the OS title bar across macOS, Windows, and Linux — one row holding session count, uplink status, remote-sessions button, settings button, plus self-drawn min/max/close on Win+Linux.

**Architecture:** Go side splits `main.go` into `main_{darwin,windows,linux}.go` via build tags; each exports `platformOptions()` returning platform-specific `*options.App` fields that `main.go` merges. Frontend adds three files: `composables/useWindowMaximized.ts` (shared module-level ref tracking Win/Linux maximize state), `components/WindowControls.vue` (three buttons calling Wails runtime), `components/TitleBar.vue` (sole owner of the merged bar, detects platform via `Environment()` at mount). `App.vue` removes its `<header class="topbar">` block and mounts `<TitleBar>` instead.

**Tech Stack:** Wails v2.12.0 (Go), Vue 3.4 + TypeScript, Vitest 1.6 + @vue/test-utils, vite 5.2. Wails runtime API used: `Environment`, `WindowMinimise`, `WindowToggleMaximise`, `WindowIsMaximised`, `Quit`.

**Spec:** `docs/superpowers/specs/2026-05-20-unified-titlebar-design.md`

---

## File Structure

**New files**

- `desktop/main_darwin.go` — `//go:build darwin`. Holds `darwinMenu()` (moved from `main.go`) and `platformOptions()` returning `*options.App` with `Menu` set and `Mac` set to enable `TitleBarHiddenInset`.
- `desktop/main_windows.go` — `//go:build windows`. `platformOptions()` returns `*options.App` with `Frameless: true` and `Windows: &windows.Options{ DisableFramelessWindowDecorations: false }`.
- `desktop/main_linux.go` — `//go:build linux`. `platformOptions()` returns `*options.App` with `Frameless: true`.
- `desktop/frontend/src/composables/useWindowMaximized.ts` — exports a `useWindowMaximized()` returning a shared module-level `Ref<boolean>`, initialized once from `WindowIsMaximised()`. A `setMaximized(v: boolean)` helper for components that call `WindowToggleMaximise` and want to flip locally.
- `desktop/frontend/src/composables/useWindowMaximized.test.ts` — Vitest for the composable.
- `desktop/frontend/src/components/WindowControls.vue` — Min / Max-Restore / Close buttons calling Wails runtime. Reads/writes via `useWindowMaximized()`.
- `desktop/frontend/src/components/WindowControls.test.ts` — Vitest for clicks calling Wails runtime APIs and icon swap on maximize state.
- `desktop/frontend/src/components/TitleBar.vue` — root merged-bar component. Props: status/errorMsg/sessionCount/remoteEndpoint/availableRemoteCount/updateBadge. Emits `open-remote`, `open-settings`. Detects platform on mount, applies left padding on darwin, renders `WindowControls` on win/linux.
- `desktop/frontend/src/components/TitleBar.test.ts` — Vitest covering platform variants and prop transmission.

**Modified files**

- `desktop/main.go` — remove platform-specific inline block and `darwinMenu()`; call `platformOptions()` and merge.
- `desktop/frontend/src/App.vue` — replace `<header class="topbar">…</header>` (lines ~728-781) with `<TitleBar … />`; remove the matching scoped CSS for `.topbar`, `.brand`, `.status`, `.icon-btn`, `.badge`, `.dot` (now owned by `TitleBar.vue`). Read `useWindowMaximized()` to add `is-maximized` class for the 8 px overflow inset.
- `desktop/frontend/src/App.test.ts` — replace `.topbar` source-grep with checks that `App.vue` mounts `<TitleBar` and wires the right emits.

---

## Task 1: Split `main.go` into per-OS build-tagged option files (no behavior change)

This is a pure refactor that sets up the structure for later platform-specific behavior. After this task, `wails dev` on macOS must work identically to before.

**Files:**
- Create: `desktop/main_darwin.go`
- Create: `desktop/main_windows.go`
- Create: `desktop/main_linux.go`
- Modify: `desktop/main.go`

- [ ] **Step 1: Create `desktop/main_darwin.go`**

```go
//go:build darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
)

// platformOptions returns macOS-specific Wails options merged into the
// shared options.App in main.go.
func platformOptions() *options.App {
	return &options.App{
		Menu: darwinMenu(),
	}
}

// darwinMenu installs a custom menu that keeps native App + Edit submenus
// (Hide / Quit / Cut / Copy / Paste / Select All) but omits the Window
// submenu, where Cocoa would bind ⌘W / ⌘M — we need ⌘W for "close pane"
// and don't want to claim ⌘M either.
func darwinMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	return m
}
```

- [ ] **Step 2: Create `desktop/main_windows.go`**

```go
//go:build windows

package main

import "github.com/wailsapp/wails/v2/pkg/options"

// platformOptions returns Windows-specific Wails options merged into the
// shared options.App in main.go.
func platformOptions() *options.App {
	return &options.App{}
}
```

- [ ] **Step 3: Create `desktop/main_linux.go`**

```go
//go:build linux

package main

import "github.com/wailsapp/wails/v2/pkg/options"

// platformOptions returns Linux-specific Wails options merged into the
// shared options.App in main.go.
func platformOptions() *options.App {
	return &options.App{}
}
```

- [ ] **Step 4: Rewrite `desktop/main.go` to use the shared + platform split**

Replace the current file contents with:

```go
package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// Version is set at build time via -ldflags -X main.Version=<tag>.
// Empty / "dev" disables the auto-update subsystem.
var Version = "dev"

func main() {
	cfgStore := loadConfig()
	logger, err := newDesktopLoggingManager(cfgStore.Get(), Version)
	if err != nil {
		println("Error:", err.Error())
		return
	}
	defer logger.Close()

	app := NewApp(cfgStore, logger)

	opts := &options.App{
		Title:  "AT Term",
		Width:  1100,
		Height: 720,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 13, G: 17, B: 23, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose: func(ctx context.Context) bool {
			return app.beforeClose(ctx, func() {
				wailsruntime.EventsEmit(ctx, "before-close")
			})
		},
		Bind: []interface{}{
			app,
			app.pluginFS,
		},
	}

	mergePlatformOptions(opts, platformOptions())

	if err := wails.Run(opts); err != nil {
		println("Error:", err.Error())
	}
}

// mergePlatformOptions copies fields that are non-zero on `p` into `into`.
// Only the fields actually set by any platform implementation need to be
// listed here.
func mergePlatformOptions(into *options.App, p *options.App) {
	if p == nil {
		return
	}
	if p.Menu != nil {
		into.Menu = p.Menu
	}
	if p.Mac != nil {
		into.Mac = p.Mac
	}
	if p.Windows != nil {
		into.Windows = p.Windows
	}
	if p.Linux != nil {
		into.Linux = p.Linux
	}
	if p.Frameless {
		into.Frameless = p.Frameless
	}
}
```

Note: removed `runtime "runtime"` and the inline `if stdruntime.GOOS == "darwin"` block. The `Menu` field is now wired through `platformOptions()`.

- [ ] **Step 5: Build to verify both compile paths**

Run: `go build ./desktop/...`

Expected: exits 0, no errors. (Builds the host OS — darwin in our case. Cross-compiling Wails to other platforms requires CGO+toolchain and is outside this verification.)

- [ ] **Step 6: Run existing Go tests**

Run: `go test ./desktop/...`

Expected: all pass (existing `app_test.go`, `config_test.go`, etc.).

- [ ] **Step 7: Manual smoke — `wails dev` works**

Run from `desktop/`: `wails dev`

Expected: app opens, sessions start, no visible difference from before. Close it after a quick poke.

- [ ] **Step 8: Commit**

```bash
git add desktop/main.go desktop/main_darwin.go desktop/main_windows.go desktop/main_linux.go
git commit -m "refactor(desktop): split main.go platform options into build-tagged files"
```

---

## Task 2: Add `useWindowMaximized` composable

A shared module-level `Ref<boolean>` tracking the Win/Linux maximize state. Read by `WindowControls` (to render the right icon) and by `App.vue` (to add the 8 px maximized padding inset). Module-level keeps it a single source of truth without prop drilling.

**Files:**
- Create: `desktop/frontend/src/composables/useWindowMaximized.ts`
- Test: `desktop/frontend/src/composables/useWindowMaximized.test.ts`

- [ ] **Step 1: Write the failing test**

`desktop/frontend/src/composables/useWindowMaximized.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../wailsjs/runtime/runtime", () => ({
  WindowIsMaximised: vi.fn(),
}));

import { WindowIsMaximised } from "../../wailsjs/runtime/runtime";

describe("useWindowMaximized", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.resetModules();
  });

  it("returns the same ref instance on every call (module-level singleton)", async () => {
    vi.mocked(WindowIsMaximised).mockResolvedValue(false);
    const { useWindowMaximized } = await import("./useWindowMaximized");
    const a = useWindowMaximized();
    const b = useWindowMaximized();
    expect(a).toBe(b);
  });

  it("initializes ref from WindowIsMaximised()", async () => {
    vi.mocked(WindowIsMaximised).mockResolvedValue(true);
    const { useWindowMaximized } = await import("./useWindowMaximized");
    const r = useWindowMaximized();
    // Wait a microtask for the init promise to resolve.
    await Promise.resolve();
    await Promise.resolve();
    expect(r.value).toBe(true);
  });

  it("defaults to false when WindowIsMaximised throws", async () => {
    vi.mocked(WindowIsMaximised).mockRejectedValue(new Error("nope"));
    const { useWindowMaximized } = await import("./useWindowMaximized");
    const r = useWindowMaximized();
    await Promise.resolve();
    await Promise.resolve();
    expect(r.value).toBe(false);
  });

  it("setMaximized flips the shared ref", async () => {
    vi.mocked(WindowIsMaximised).mockResolvedValue(false);
    const { useWindowMaximized, setMaximized } = await import("./useWindowMaximized");
    const r = useWindowMaximized();
    setMaximized(true);
    expect(r.value).toBe(true);
    setMaximized(false);
    expect(r.value).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run from `desktop/frontend/`: `npm test -- src/composables/useWindowMaximized.test.ts`

Expected: FAIL with module-not-found for `./useWindowMaximized`.

- [ ] **Step 3: Implement the composable**

`desktop/frontend/src/composables/useWindowMaximized.ts`:

```ts
import { ref, type Ref } from "vue";
import { WindowIsMaximised } from "../../wailsjs/runtime/runtime";

const isMaximized = ref(false);

let initStarted = false;
function initOnce() {
  if (initStarted) return;
  initStarted = true;
  // Wails runtime may be unavailable in tests or on first paint; default
  // to false if the call rejects.
  Promise.resolve()
    .then(() => WindowIsMaximised())
    .then((v) => {
      isMaximized.value = !!v;
    })
    .catch(() => {
      isMaximized.value = false;
    });
}

export function useWindowMaximized(): Ref<boolean> {
  initOnce();
  return isMaximized;
}

export function setMaximized(v: boolean): void {
  isMaximized.value = v;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `npm test -- src/composables/useWindowMaximized.test.ts`

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/composables/useWindowMaximized.ts desktop/frontend/src/composables/useWindowMaximized.test.ts
git commit -m "feat(desktop): add useWindowMaximized composable"
```

---

## Task 3: Add `WindowControls.vue` (min / max-restore / close buttons)

The Win/Linux-only subcomponent. Three buttons calling Wails runtime. Reads max state via `useWindowMaximized`; on max-button click, calls `WindowToggleMaximise()` and flips the shared ref locally so the icon updates without waiting for an event subscription.

**Files:**
- Create: `desktop/frontend/src/components/WindowControls.vue`
- Test: `desktop/frontend/src/components/WindowControls.test.ts`

- [ ] **Step 1: Write the failing test**

`desktop/frontend/src/components/WindowControls.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";

vi.mock("../../wailsjs/runtime/runtime", () => ({
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn().mockResolvedValue(false),
  Quit: vi.fn(),
}));

import {
  WindowMinimise,
  WindowToggleMaximise,
  Quit,
} from "../../wailsjs/runtime/runtime";
import { setMaximized } from "../composables/useWindowMaximized";
import WindowControls from "./WindowControls.vue";

beforeEach(() => {
  vi.clearAllMocks();
  setMaximized(false);
});

describe("WindowControls", () => {
  it("renders three buttons: minimise, maximise/restore, close", () => {
    const w = mount(WindowControls);
    expect(w.find('[data-testid="window-min"]').exists()).toBe(true);
    expect(w.find('[data-testid="window-max"]').exists()).toBe(true);
    expect(w.find('[data-testid="window-close"]').exists()).toBe(true);
  });

  it("min button calls WindowMinimise", async () => {
    const w = mount(WindowControls);
    await w.get('[data-testid="window-min"]').trigger("click");
    expect(WindowMinimise).toHaveBeenCalledTimes(1);
  });

  it("max button calls WindowToggleMaximise and flips the shared ref", async () => {
    const w = mount(WindowControls);
    await w.get('[data-testid="window-max"]').trigger("click");
    expect(WindowToggleMaximise).toHaveBeenCalledTimes(1);
    await flushPromises();
    expect(w.get('[data-testid="window-max"]').attributes("aria-label")).toBe("Restore");
  });

  it("close button calls Quit", async () => {
    const w = mount(WindowControls);
    await w.get('[data-testid="window-close"]').trigger("click");
    expect(Quit).toHaveBeenCalledTimes(1);
  });

  it("when started maximized, max button starts in restore variant", async () => {
    setMaximized(true);
    const w = mount(WindowControls);
    await flushPromises();
    expect(w.get('[data-testid="window-max"]').attributes("aria-label")).toBe("Restore");
  });

  it("if a runtime call throws, the button does not propagate the error", async () => {
    vi.mocked(WindowMinimise).mockImplementation(() => {
      throw new Error("runtime gone");
    });
    const w = mount(WindowControls);
    await expect(
      w.get('[data-testid="window-min"]').trigger("click"),
    ).resolves.toBeUndefined();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run from `desktop/frontend/`: `npm test -- src/components/WindowControls.test.ts`

Expected: FAIL with module-not-found for `./WindowControls.vue`.

- [ ] **Step 3: Implement the component**

`desktop/frontend/src/components/WindowControls.vue`:

```vue
<script lang="ts" setup>
import { computed } from "vue";
import {
  WindowMinimise,
  WindowToggleMaximise,
  Quit,
} from "../../wailsjs/runtime/runtime";
import { useWindowMaximized, setMaximized } from "../composables/useWindowMaximized";

const isMaximized = useWindowMaximized();

const maxLabel = computed(() => (isMaximized.value ? "Restore" : "Maximize"));

function safe(fn: () => void) {
  try {
    fn();
  } catch (e) {
    console.warn("[WindowControls] runtime call failed", e);
  }
}

function onMin() {
  safe(() => WindowMinimise());
}

function onMax() {
  safe(() => {
    WindowToggleMaximise();
    setMaximized(!isMaximized.value);
  });
}

function onClose() {
  safe(() => Quit());
}
</script>

<template>
  <div class="window-controls" style="-webkit-app-region: no-drag">
    <button
      class="wc-btn"
      type="button"
      data-testid="window-min"
      aria-label="Minimize"
      @click="onMin"
    >
      <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <rect x="1" y="4.5" width="8" height="1" fill="currentColor" />
      </svg>
    </button>
    <button
      class="wc-btn"
      type="button"
      data-testid="window-max"
      :aria-label="maxLabel"
      @click="onMax"
    >
      <svg v-if="!isMaximized" width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <rect x="1" y="1" width="8" height="8" fill="none" stroke="currentColor" />
      </svg>
      <svg v-else width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <rect x="1" y="3" width="6" height="6" fill="none" stroke="currentColor" />
        <rect x="3" y="1" width="6" height="6" fill="none" stroke="currentColor" />
      </svg>
    </button>
    <button
      class="wc-btn wc-close"
      type="button"
      data-testid="window-close"
      aria-label="Close"
      @click="onClose"
    >
      <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <path d="M1 1 L9 9 M9 1 L1 9" stroke="currentColor" stroke-width="1" fill="none" />
      </svg>
    </button>
  </div>
</template>

<style scoped>
.window-controls {
  display: inline-flex;
  align-items: stretch;
  height: 100%;
  margin-left: 8px;
}
.wc-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  border: none;
  background: transparent;
  color: var(--fg-dim);
  cursor: pointer;
  transition: background 120ms, color 120ms;
}
.wc-btn:hover {
  background: rgba(255, 255, 255, 0.08);
  color: var(--fg);
}
.wc-close:hover {
  background: #e81123;
  color: #fff;
}
</style>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `npm test -- src/components/WindowControls.test.ts`

Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/WindowControls.vue desktop/frontend/src/components/WindowControls.test.ts
git commit -m "feat(desktop): add WindowControls component"
```

---

## Task 4: Add `TitleBar.vue` — sole owner of the merged bar

Holds the brand-removed content (status text, two icon buttons) plus a slot for `WindowControls` on Win/Linux. Detects platform via `Environment()` at mount and applies `padding-left: 80px` on darwin for the traffic lights.

**Files:**
- Create: `desktop/frontend/src/components/TitleBar.vue`
- Test: `desktop/frontend/src/components/TitleBar.test.ts`

- [ ] **Step 1: Write the failing test**

`desktop/frontend/src/components/TitleBar.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";

vi.mock("../../wailsjs/runtime/runtime", () => ({
  Environment: vi.fn(),
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn().mockResolvedValue(false),
  Quit: vi.fn(),
}));

import { Environment, WindowToggleMaximise } from "../../wailsjs/runtime/runtime";
import TitleBar from "./TitleBar.vue";

const baseProps = {
  status: "ready" as const,
  errorMsg: "",
  sessionCount: 0,
  remoteEndpoint: null,
  availableRemoteCount: 0,
  updateBadge: false,
};

beforeEach(() => {
  vi.clearAllMocks();
});

async function mountForPlatform(platform: string, props = {}) {
  vi.mocked(Environment).mockResolvedValue({
    platform,
    arch: "x64",
    buildType: "dev",
  });
  const w = mount(TitleBar, { props: { ...baseProps, ...props } });
  await flushPromises();
  return w;
}

describe("TitleBar platform variants", () => {
  it("on darwin, root has padding-left: 80px and no WindowControls", async () => {
    const w = await mountForPlatform("darwin");
    expect(w.get('[data-testid="titlebar-root"]').attributes("style")).toContain(
      "padding-left: 80px",
    );
    expect(w.find('[data-testid="window-min"]').exists()).toBe(false);
  });

  it("on windows, renders WindowControls and no left padding", async () => {
    const w = await mountForPlatform("windows");
    expect(w.get('[data-testid="titlebar-root"]').attributes("style")).not.toContain(
      "padding-left: 80px",
    );
    expect(w.find('[data-testid="window-min"]').exists()).toBe(true);
    expect(w.find('[data-testid="window-max"]').exists()).toBe(true);
    expect(w.find('[data-testid="window-close"]').exists()).toBe(true);
  });

  it("on linux, renders WindowControls and no left padding", async () => {
    const w = await mountForPlatform("linux");
    expect(w.get('[data-testid="titlebar-root"]').attributes("style")).not.toContain(
      "padding-left: 80px",
    );
    expect(w.find('[data-testid="window-min"]').exists()).toBe(true);
  });

  it("falls back to linux rendering if Environment rejects", async () => {
    vi.mocked(Environment).mockRejectedValue(new Error("no runtime"));
    const w = mount(TitleBar, { props: baseProps });
    await flushPromises();
    expect(w.find('[data-testid="window-min"]').exists()).toBe(true);
  });
});

describe("TitleBar status rendering", () => {
  it("status=loading renders 'starting…'", async () => {
    const w = await mountForPlatform("darwin", { status: "loading" });
    expect(w.text()).toContain("starting");
  });

  it("status=error renders errorMsg in bad span", async () => {
    const w = await mountForPlatform("darwin", { status: "error", errorMsg: "boom" });
    expect(w.find(".bad").text()).toBe("boom");
  });

  it("renders '3 sessions' (plural) for sessionCount=3", async () => {
    const w = await mountForPlatform("darwin", { sessionCount: 3 });
    expect(w.text()).toContain("3 sessions");
  });

  it("renders '1 session' (singular) for sessionCount=1", async () => {
    const w = await mountForPlatform("darwin", { sessionCount: 1 });
    expect(w.text()).toContain("1 session");
    expect(w.text()).not.toContain("1 sessions");
  });

  it("renders '· uplink on' when remoteEndpoint is truthy", async () => {
    const w = await mountForPlatform("darwin", {
      remoteEndpoint: { url: "wss://x", token: "t" },
    });
    expect(w.text()).toContain("uplink on");
  });
});

describe("TitleBar buttons", () => {
  it("remote button is disabled when remoteEndpoint is null", async () => {
    const w = await mountForPlatform("darwin", { remoteEndpoint: null });
    const btn = w.get('[data-testid="titlebar-remote"]');
    expect((btn.element as HTMLButtonElement).disabled).toBe(true);
  });

  it("remote button emits open-remote when clicked", async () => {
    const w = await mountForPlatform("darwin", {
      remoteEndpoint: { url: "wss://x", token: "t" },
    });
    await w.get('[data-testid="titlebar-remote"]').trigger("click");
    expect(w.emitted("open-remote")).toBeTruthy();
  });

  it("settings button emits open-settings when clicked", async () => {
    const w = await mountForPlatform("darwin");
    await w.get('[data-testid="titlebar-settings"]').trigger("click");
    expect(w.emitted("open-settings")).toBeTruthy();
  });

  it("renders availableRemoteCount badge when > 0", async () => {
    const w = await mountForPlatform("darwin", {
      remoteEndpoint: { url: "wss://x", token: "t" },
      availableRemoteCount: 4,
    });
    expect(w.find(".badge").text()).toBe("4");
  });

  it("renders update dot when updateBadge=true", async () => {
    const w = await mountForPlatform("darwin", { updateBadge: true });
    expect(w.find(".dot").exists()).toBe(true);
  });
});

describe("TitleBar double-click maximize (Win/Linux only)", () => {
  it("on windows, double-click on root calls WindowToggleMaximise", async () => {
    const w = await mountForPlatform("windows");
    await w.get('[data-testid="titlebar-root"]').trigger("dblclick");
    expect(WindowToggleMaximise).toHaveBeenCalledTimes(1);
  });

  it("on darwin, double-click on root does NOT call WindowToggleMaximise (system handles zoom)", async () => {
    const w = await mountForPlatform("darwin");
    await w.get('[data-testid="titlebar-root"]').trigger("dblclick");
    expect(WindowToggleMaximise).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run from `desktop/frontend/`: `npm test -- src/components/TitleBar.test.ts`

Expected: FAIL with module-not-found for `./TitleBar.vue`.

- [ ] **Step 3: Implement the component**

`desktop/frontend/src/components/TitleBar.vue`:

```vue
<script lang="ts" setup>
import { computed, onMounted, ref } from "vue";
import { Environment, WindowToggleMaximise } from "../../wailsjs/runtime/runtime";
import WindowControls from "./WindowControls.vue";
import { setMaximized, useWindowMaximized } from "../composables/useWindowMaximized";
import type { Endpoint } from "../lib/api";

type Status = "loading" | "ready" | "error";

const props = defineProps<{
  status: Status;
  errorMsg: string;
  sessionCount: number;
  remoteEndpoint: Endpoint | null;
  availableRemoteCount: number;
  updateBadge: boolean;
}>();

defineEmits<{
  (e: "open-remote"): void;
  (e: "open-settings"): void;
}>();

// Default to linux if Environment() fails — gives users window controls
// rather than locking them out of min/max/close.
const os = ref<"darwin" | "windows" | "linux">("linux");

onMounted(async () => {
  try {
    const info = await Environment();
    const p = (info?.platform ?? "").toLowerCase();
    if (p === "darwin" || p === "windows" || p === "linux") {
      os.value = p;
    } else {
      console.warn("[TitleBar] unknown platform, falling back to linux:", p);
    }
  } catch (e) {
    console.warn("[TitleBar] Environment() failed, falling back to linux", e);
  }
});

const rootStyle = computed(() => ({
  "padding-left": os.value === "darwin" ? "80px" : undefined,
}));

const showWindowControls = computed(() => os.value !== "darwin");

const remoteTitle = computed(() =>
  props.remoteEndpoint
    ? `${props.availableRemoteCount} remote session(s) available`
    : "connect to a relay to see remote sessions",
);

const isMaximized = useWindowMaximized();

function onTitleDblClick() {
  // macOS handles zoom natively in the TitleBarHiddenInset toolbar area;
  // calling WindowToggleMaximise there would double-fire and interfere.
  if (os.value === "darwin") return;
  try {
    WindowToggleMaximise();
    setMaximized(!isMaximized.value);
  } catch (e) {
    console.warn("[TitleBar] WindowToggleMaximise failed", e);
  }
}
</script>

<template>
  <header
    class="titlebar"
    data-testid="titlebar-root"
    :style="rootStyle"
    @dblclick.self="onTitleDblClick"
  >
    <div class="status">
      <template v-if="status === 'loading'">starting…</template>
      <template v-else-if="status === 'error'">
        <span class="bad">{{ errorMsg }}</span>
      </template>
      <template v-else>
        {{ sessionCount }} session{{ sessionCount === 1 ? "" : "s" }}
        <span v-if="remoteEndpoint" class="dim"> · uplink on</span>
      </template>
    </div>
    <button
      class="icon-btn"
      type="button"
      data-testid="titlebar-remote"
      :title="remoteTitle"
      :disabled="!remoteEndpoint"
      @click="$emit('open-remote')"
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="16" height="16"
        viewBox="0 0 24 24"
        fill="none" stroke="currentColor"
        stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
        aria-hidden="true"
      >
        <path d="M2 16.1A5 5 0 0 1 5.9 20" />
        <path d="M2 12.05A9 9 0 0 1 9.95 20" />
        <path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6" />
        <line x1="2" y1="20" x2="2.01" y2="20" />
      </svg>
      <span v-if="availableRemoteCount > 0" class="badge">{{ availableRemoteCount }}</span>
    </button>
    <button
      class="icon-btn"
      type="button"
      data-testid="titlebar-settings"
      title="relay settings"
      @click="$emit('open-settings')"
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="16" height="16"
        viewBox="0 0 24 24"
        fill="none" stroke="currentColor"
        stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
        aria-hidden="true"
      >
        <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
        <circle cx="12" cy="12" r="3" />
      </svg>
      <span v-if="updateBadge" class="dot"></span>
    </button>
    <WindowControls v-if="showWindowControls" />
  </header>
</template>

<style scoped>
.titlebar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  background: var(--panel);
  border-bottom: 1px solid var(--border);
  flex: 0 0 auto;
  -webkit-app-region: drag;
}
.status {
  margin-left: auto;
  font-size: 12px;
  color: var(--fg-dim);
  -webkit-app-region: no-drag;
}
.status .bad { color: var(--bad); }
.status .dim { color: var(--good); }

.icon-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  color: var(--fg-dim);
  line-height: 1;
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
  transition: color 120ms, background 120ms;
  -webkit-app-region: no-drag;
}
.icon-btn svg { display: block; }
.icon-btn:hover:not(:disabled) { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.icon-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.icon-btn .badge {
  position: absolute; top: -2px; right: -2px;
  background: #d29922; color: #0d1117; font-size: 9px; font-weight: 700;
  border-radius: 10px; padding: 1px 5px; line-height: 1.3;
  min-width: 16px; text-align: center;
}
.icon-btn .dot {
  position: absolute; top: 2px; right: 2px;
  width: 6px; height: 6px;
  background: #d29922;
  border-radius: 50%;
}
</style>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `npm test -- src/components/TitleBar.test.ts`

Expected: all 14 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TitleBar.vue desktop/frontend/src/components/TitleBar.test.ts
git commit -m "feat(desktop): add TitleBar component"
```

---

## Task 5: Integrate `TitleBar` into `App.vue`, remove old `<header class="topbar">` and CSS

Wire `TitleBar` as a replacement for the existing toolbar block. Remove the now-dead CSS rules. Update `App.test.ts` source-grep assertions accordingly. After this task, on macOS you'll still see the native title bar above the new TitleBar — that resolves in Task 7.

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/App.test.ts`

- [ ] **Step 1: Update `App.test.ts` assertions for the new structure**

Replace the existing test file content with this version. The auth-banner and quit-confirm checks stay; the topbar checks are removed and replaced with TitleBar mount checks.

`desktop/frontend/src/App.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import source from "./App.vue?raw";
import settingsSource from "./components/SettingsDialog.vue?raw";

describe("tab activation", () => {
  test("gotoTab sets currentTabId before mutating the hash", () => {
    const body = source.match(/function\s+gotoTab\s*\(id:\s*string\)\s*\{([\s\S]*?)\n\}/)?.[1] ?? "";
    const currentIdx = body.indexOf("currentTabId.value = id");
    const hashIdx = body.indexOf("location.hash =");
    expect(currentIdx).toBeGreaterThanOrEqual(0);
    expect(hashIdx).toBeGreaterThanOrEqual(0);
    expect(currentIdx).toBeLessThan(hashIdx);
  });
});

describe("terminal toasts", () => {
  test("wires pane-grid toast events to the existing toast surface", () => {
    expect(source).toContain('@toast="showToast"');
  });
});

describe("auth-error banner", () => {
  test("subscribes to relay:auth-error event on mount", () => {
    expect(source).toContain('EventsOn("relay:auth-error"');
  });

  test("banner section is gated on authError being non-null", () => {
    expect(source).toContain('v-if="authError"');
    expect(source).toContain("auth-error-banner");
  });

  test("banner references auth_invalid_token reason string", () => {
    expect(source).toContain("auth_invalid_token");
  });

  test("banner references auth_user_disabled reason string", () => {
    expect(source).toContain("auth_user_disabled");
  });

  test("banner has an Open settings action", () => {
    expect(source).toContain("Open settings");
    expect(source).toContain("openSettingsRelay");
  });

  test("SettingsDialog receives initial-tab prop for relay navigation", () => {
    expect(source).toContain(":initial-tab=");
    expect(source).toContain("settingsInitialTab");
  });

  test("SettingsDialog supports initialTab prop", () => {
    expect(settingsSource).toContain("initialTab");
  });
});

describe("quit confirmation", () => {
  test("registers the before-close listener and imports confirmQuit", () => {
    expect(source).toContain('EventsOn("before-close"');
    expect(source).toContain("confirmQuit");
    expect(source).toContain("ConfirmQuitDialog");
  });

  test("renders ConfirmQuitDialog wired to existing session counts", () => {
    expect(source).toContain("<ConfirmQuitDialog");
    expect(source).toContain(':local-count="localSessionCount"');
    expect(source).toContain(':remote-count="remoteSessionCount"');
    expect(source).toContain('@confirm="onConfirmQuit"');
    expect(source).toContain('@cancel="onCancelQuit"');
  });

  test("gates the dialog: zero counts call confirmQuit, otherwise open dialog", () => {
    expect(source).toMatch(/localSessionCount\.value\s*===\s*0[^\n]*remoteSessionCount\.value\s*===\s*0/);
    expect(source).toContain("quitDialogOpen.value = true");
  });
});

describe("merged title bar", () => {
  test("uses TitleBar component instead of inline topbar markup", () => {
    expect(source).toContain("<TitleBar");
    expect(source).toContain('import TitleBar from "./components/TitleBar.vue"');
    expect(source).not.toContain('class="topbar"');
    expect(source).not.toContain('class="brand"');
  });

  test("passes status, errorMsg, sessionCount, remoteEndpoint, availableRemoteCount, updateBadge props", () => {
    expect(source).toContain(':status="status"');
    expect(source).toContain(':error-msg="errorMsg"');
    expect(source).toContain(':session-count="sessionCount"');
    expect(source).toContain(':remote-endpoint="remoteEndpoint"');
    expect(source).toContain(':available-remote-count="availableRemote.length"');
    expect(source).toContain(':update-badge="updateBadge"');
  });

  test("wires open-remote and open-settings events", () => {
    expect(source).toContain('@open-remote="showRemote = true"');
    expect(source).toContain('@open-settings="showSettings = true"');
  });
});
```

- [ ] **Step 2: Run the failing test**

Run from `desktop/frontend/`: `npm test -- src/App.test.ts`

Expected: the new `merged title bar` describe block FAILS, existing blocks still pass.

- [ ] **Step 3: Modify `App.vue` — add import**

In `desktop/frontend/src/App.vue`, add this import alongside the existing `import TabBar from "./components/TabBar.vue";` (around line 5):

```ts
import TitleBar from "./components/TitleBar.vue";
```

- [ ] **Step 4: Modify `App.vue` — replace the `<header class="topbar">` block**

Find the block beginning at `<header class="topbar">` (around line 728) and ending at `</header>` (around line 781). Replace the entire block with:

```html
    <TitleBar
      :status="status"
      :error-msg="errorMsg"
      :session-count="sessionCount"
      :remote-endpoint="remoteEndpoint"
      :available-remote-count="availableRemote.length"
      :update-badge="updateBadge"
      @open-remote="showRemote = true"
      @open-settings="showSettings = true"
    />
```

- [ ] **Step 5: Modify `App.vue` — remove dead CSS from the scoped style block**

In the `<style scoped>` block (starting around line 877), delete these rule blocks entirely:

```css
.topbar {
  display: flex; align-items: center; gap: 12px; padding: 10px 16px;
  background: var(--panel); border-bottom: 1px solid var(--border); flex: 0 0 auto;
}
.brand { font-weight: 600; letter-spacing: 0.06em; }
.status { margin-left: auto; font-size: 12px; color: var(--fg-dim); }
.status .bad { color: var(--bad); }
.status .dim { color: var(--good); }

.icon-btn {
  position: relative; display: inline-flex; align-items: center; justify-content: center;
  border: none; background: transparent; color: var(--fg-dim); line-height: 1;
  padding: 6px 8px; border-radius: 6px; cursor: pointer;
  transition: color 120ms, background 120ms;
}
.icon-btn svg { display: block; }
.icon-btn:hover:not(:disabled) { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.icon-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.icon-btn .badge {
  position: absolute; top: -2px; right: -2px;
  background: #d29922; color: #0d1117; font-size: 9px; font-weight: 700;
  border-radius: 10px; padding: 1px 5px; line-height: 1.3;
  min-width: 16px; text-align: center;
}
.icon-btn .dot {
  position: absolute; top: 2px; right: 2px;
  width: 6px; height: 6px;
  background: #d29922;
  border-radius: 50%;
}
```

The remaining `.app`, `.auth-error-banner`, `.main-row`, `.main`, `.right-resizer`, `.panel-toggle`, `.right-panel`, `.bottom-toolbar`, `.empty`, `.toast` rules stay.

- [ ] **Step 6: Run tests to verify they pass**

Run: `npm test -- src/App.test.ts`

Expected: all describe blocks PASS, including the new `merged title bar` block.

- [ ] **Step 7: Run the full frontend test suite to catch regressions**

Run from `desktop/frontend/`: `npm test`

Expected: all tests PASS.

- [ ] **Step 8: Type-check**

Run from `desktop/frontend/`: `npm run build`

Expected: `vue-tsc --noEmit` then `vite build` both succeed.

- [ ] **Step 9: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/App.test.ts
git commit -m "feat(desktop): mount TitleBar in App.vue, drop inline topbar"
```

---

## Task 6: Apply 8 px maximized inset class on `.app` from Win/Linux maximize state

Without this, when the Windows borderless window is maximized, its rect extends 8 px past each screen edge and our content gets clipped. Subscribe `App.vue` to `useWindowMaximized()` and toggle an `is-maximized` class on `.app`. Pure CSS handles the inset.

**Files:**
- Modify: `desktop/frontend/src/App.vue`

- [ ] **Step 1: Add the composable import in `App.vue` `<script setup>`**

After the line `import TitleBar from "./components/TitleBar.vue";`, add:

```ts
import { useWindowMaximized } from "./composables/useWindowMaximized";
```

Then below the existing reactive state declarations (any spot before `onMounted`, e.g. near the other `ref` definitions around line 80-110), add:

```ts
const isMaximized = useWindowMaximized();
```

- [ ] **Step 2: Toggle the class on the root `.app` element**

Find the line:

```html
  <div class="app" :class="`fe-theme-${fileExplorerTheme}`" :style="themeStyle">
```

Change to:

```html
  <div class="app" :class="[`fe-theme-${fileExplorerTheme}`, { 'is-maximized': isMaximized }]" :style="themeStyle">
```

- [ ] **Step 3: Add the CSS rule in the scoped style block**

After the `.app { display: flex; flex-direction: column; height: 100vh; }` rule, append:

```css
.app.is-maximized { padding: 8px; }
```

- [ ] **Step 4: Run the full frontend test suite**

Run from `desktop/frontend/`: `npm test`

Expected: all tests PASS (no behavior change for tests that don't mock WindowIsMaximised — composable will fire but jsdom won't render).

- [ ] **Step 5: Type-check**

Run from `desktop/frontend/`: `npm run build`

Expected: succeeds.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/App.vue
git commit -m "feat(desktop): inset .app by 8px when window is maximized"
```

---

## Task 7: Enable macOS title-bar-hidden-inset in `main_darwin.go`

Wires `Mac.TitleBar = TitleBarHiddenInset()` so the native chrome merges into our `TitleBar`. After this task, on macOS the bar should look like the screenshot in the spec.

**Files:**
- Modify: `desktop/main_darwin.go`

- [ ] **Step 1: Update `platformOptions()` to set `Mac` options**

Replace the file with:

```go
//go:build darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// platformOptions returns macOS-specific Wails options merged into the
// shared options.App in main.go. Mac.TitleBar = TitleBarHiddenInset gives
// us a transparent title bar with full-size content under the traffic
// lights so our TitleBar component can occupy that row.
func platformOptions() *options.App {
	return &options.App{
		Menu: darwinMenu(),
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	}
}

func darwinMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	return m
}
```

- [ ] **Step 2: Build**

Run: `go build ./desktop/...`

Expected: exits 0.

- [ ] **Step 3: Manual verification on macOS**

Run from `desktop/`: `wails dev`

Expected:
- Traffic lights overlay the top-left of the `TitleBar` row, slightly inset (~8 px from left edge).
- No separate native title bar above the merged bar.
- Status text and the two icon buttons are clickable; dragging anywhere else in the bar moves the window.
- Double-clicking the empty area to the right of traffic lights triggers macOS zoom.

If anything looks wrong, fix before committing.

- [ ] **Step 4: Commit**

```bash
git add desktop/main_darwin.go
git commit -m "feat(desktop): merge macOS title bar with content via TitleBarHiddenInset"
```

---

## Task 8: Enable Windows frameless in `main_windows.go`

Switches Windows to a frameless window so our `WindowControls` owns min/max/close.

**Files:**
- Modify: `desktop/main_windows.go`

- [ ] **Step 1: Update `platformOptions()` for Windows frameless**

Replace the file with:

```go
//go:build windows

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// platformOptions returns Windows-specific Wails options. Frameless: true
// removes the native title bar so our TitleBar component owns the full
// top row, including the self-drawn WindowControls (min/max/close).
// DisableFramelessWindowDecorations: false keeps Aero shadow and rounded
// corners on Win11.
func platformOptions() *options.App {
	return &options.App{
		Frameless: true,
		Windows: &windows.Options{
			DisableFramelessWindowDecorations: false,
		},
	}
}
```

- [ ] **Step 2: Build (cross-compile from macOS, optional sanity check)**

If you have the Wails Windows toolchain installed and `wails build -platform windows/amd64` works in this repo:

Run: `wails build -platform windows/amd64`

Expected: build succeeds. (If toolchain missing, skip — Task 1's `go build` already validates syntax for the OS we're on; Step 3 below is the real verification.)

- [ ] **Step 3: Manual verification on Windows**

On a Windows machine, run `wails dev`.

Expected:
- No native title bar; the merged `TitleBar` row sits at the very top.
- Min / Max / Close buttons render on the far right of `TitleBar` and behave correctly.
- Maximized window does not overflow screen edges (the 8 px `.app.is-maximized` inset compensates).
- Restored window has no padding residue.
- Double-clicking the title bar empty area toggles maximize (handled by `onTitleDblClick` in `TitleBar.vue` — verify it fires and the icon flips).
- Dragging the title bar to the top/left/right edge triggers Windows snap.
- Closing the window with active sessions opens the confirm-quit dialog (the close button calls `Quit()`, which fires `OnBeforeClose`).

- [ ] **Step 4: Commit**

```bash
git add desktop/main_windows.go
git commit -m "feat(desktop): merge Windows title bar via Frameless + WindowControls"
```

---

## Task 9: Enable Linux frameless in `main_linux.go`

Switches Linux to a frameless window. Known limitation: edge-resize disappears (documented in spec).

**Files:**
- Modify: `desktop/main_linux.go`

- [ ] **Step 1: Update `platformOptions()` for Linux frameless**

Replace the file with:

```go
//go:build linux

package main

import "github.com/wailsapp/wails/v2/pkg/options"

// platformOptions returns Linux-specific Wails options. Frameless: true
// removes the native chrome so our TitleBar + WindowControls own the
// entire top row. Known limitation: GTK frameless windows lose
// WM-provided edge resize handles — documented in the spec; resize
// requires the maximize button or WM keyboard shortcuts.
func platformOptions() *options.App {
	return &options.App{
		Frameless: true,
	}
}
```

- [ ] **Step 2: Manual verification on Linux (X11)**

On a Linux machine (X11 session, GNOME or KDE), run `wails dev`.

Expected:
- No native title bar; TitleBar row at the top.
- WindowControls renders on the right; min/max/close behave correctly.
- Title bar drags the window.
- Maximize works; 8 px inset compensates if the WM extends the borderless rect (most don't on Linux — the rule is harmless either way).
- Edge resize does NOT work — confirm acceptable per spec.
- Wayland session (if available): best-effort spot check. Behavior depends on compositor; document any anomalies as known limitations, not blockers.

- [ ] **Step 3: Commit**

```bash
git add desktop/main_linux.go
git commit -m "feat(desktop): merge Linux title bar via Frameless + WindowControls"
```

---

## Task 10: Cross-platform manual verification + final clean-up

Final sweep across all three platforms to confirm the spec's "Manual verification" checklist passes, fix any rough edges discovered, and update CLAUDE.md / docs if needed.

**Files:**
- (No required file changes; depends on what manual testing surfaces.)

- [ ] **Step 1: Run full test suite once more**

Run from `desktop/frontend/`: `npm test`
Run from repo root: `go test ./desktop/...`

Expected: all green.

- [ ] **Step 2: Manual checklist — macOS**

- [ ] Traffic lights centered in merged bar, no clipping
- [ ] Drag region works; clicking status / buttons doesn't initiate drag
- [ ] Double-click empty area triggers zoom
- [ ] Fullscreen enter: confirm 80 px left padding remains (known limitation per spec)

- [ ] **Step 3: Manual checklist — Windows**

- [ ] Three buttons render at right edge with correct hover states
- [ ] Min / Max / Close behave correctly
- [ ] Maximized: no screen-edge overflow
- [ ] Restored: no padding residue
- [ ] Double-click title bar toggles maximize
- [ ] Drag to edges triggers snap
- [ ] Close with active sessions → confirm-quit dialog appears

- [ ] **Step 4: Manual checklist — Linux**

- [ ] Three buttons render and function correctly
- [ ] Drag region works
- [ ] Maximize works (edge-resize known limitation confirmed acceptable)
- [ ] At least one of GNOME / KDE / tiling WM verified on X11

- [ ] **Step 5: Address any issues found**

If a platform showed a regression, fix it on its respective build-tag file or in `TitleBar.vue` / `WindowControls.vue`. Commit each fix as its own commit with a descriptive message.

- [ ] **Step 6: Final commit if nothing else to fix**

If Step 5 had no fixes, this task ends without a new commit. Otherwise, the per-issue commits above are the closure.

---

## Out of scope (deferred per spec)

- macOS fullscreen padding auto-collapse.
- Linux custom resize-edge regions.
- Wayland compatibility hardening.
- Custom traffic-light hover states on macOS.
- Logo / app-icon asset replacing removed brand text.
