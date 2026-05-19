# Unified title bar across macOS / Windows / Linux

## Problem

The desktop app currently renders a top toolbar (`App.vue` `<header class="topbar">`) that sits below the OS-provided window chrome. The toolbar contains: "AT Term" brand text, session count, uplink status, a remote-sessions icon button, and a settings icon button. On every platform the OS title bar already shows "AT Term", so two horizontal strips compete for vertical space and the app looks heavier than peer products (IDEs, modern terminals) that merge their toolbar into the window chrome.

Goal: render a single merged title bar that contains the existing toolbar content and absorbs the role of the native title bar. Same visual model on all three platforms; native window controls (red/yellow/green on macOS; min/max/close on Windows and Linux) live in the same row as the toolbar content.

## Non-goals

- Redesign of what the toolbar contains. Same brand-removal aside, the controls stay (status text, remote icon, settings icon).
- Reflow of TabBar into the merged bar (the row immediately below the toolbar). Stays as a separate row.
- Logo asset work. Brand text is removed; no replacement icon ships in this change.
- macOS fullscreen padding collapse, Linux custom resize edges, Wayland validation. Listed as known limitations and deferred.

## Approach

One Vue component (`TitleBar.vue`) replaces the existing `<header class="topbar">`. It detects platform at runtime via Wails `Environment()` and renders three variants from one template:

- `darwin` — left `padding-left: 80px` for the native traffic-light overlay; right side keeps the existing status text and two icon buttons. No custom window control buttons.
- `windows` / `linux` — no left padding; right side keeps status + icon buttons and additionally renders a `WindowControls` subcomponent (min / max-restore / close) calling `runtime.WindowMinimise()` / `WindowToggleMaximise()` / `Quit()`.

Go side splits `main.go` into platform-specific files via build tags. Each file exports a `platformOptions() *options.App` returning only its platform-specific fields; the shared `main.go` merges them into the base options.

Platform detection is runtime in the frontend (shared `dist` is embedded into all three platform binaries) and compile-time in Go. Build-time frontend detection was considered and rejected — it would force `wails.json` build hooks to plumb an env var into Vite for ~1-2 KB of dead-code elimination, and complicate test setup. The runtime cost is one async `Environment()` call at mount.

## Architecture

### Go side

New files under `desktop/`:

- `main_darwin.go` (build tag `//go:build darwin`) — returns `*options.App` with `Mac: &mac.Options{ TitleBar: mac.TitleBarHiddenInset() }` and any darwin-only appearance settings. Existing `darwinMenu()` moves into this file.
- `main_windows.go` (build tag `//go:build windows`) — returns `*options.App` with `Frameless: true` plus any `Windows: &windows.Options{…}` we determine necessary during implementation.
- `main_linux.go` (build tag `//go:build linux`) — returns `*options.App` with `Frameless: true`.

`main.go` keeps the shared `options.App` literal (title, width, height, asset server, background color, callbacks, bind) and merges fields from `platformOptions()` before calling `wails.Run`. The current inline `if stdruntime.GOOS == "darwin"` block disappears.

`TitleBarHiddenInset` is the Wails preset for `TitlebarAppearsTransparent + HideTitle + FullSizeContent`, which is exactly the "content extends under traffic lights, no system title text" configuration we want.

### Frontend side

New files under `desktop/frontend/src/components/`:

- `TitleBar.vue` — sole owner of the merged bar markup and CSS. Props mirror what the current `<header class="topbar">` reads from App.vue state:
  - `status: 'loading' | 'ready' | 'error'`
  - `errorMsg: string`
  - `sessionCount: number`
  - `remoteEndpoint: Endpoint | null`
  - `availableRemoteCount: number`
  - `updateBadge: boolean`
  Emits: `open-remote`, `open-settings`. Internal state: `os` ref (`'darwin' | 'windows' | 'linux' | null`), populated in `onMounted` from `Environment()`.
- `WindowControls.vue` — only rendered when `os !== 'darwin'`. Renders three buttons calling `runtime.WindowMinimise()`, `runtime.WindowToggleMaximise()`, `runtime.Quit()`. Reads/writes `isMaximized` through a shared `useWindowMaximized` composable (see below).
- `composables/useWindowMaximized.ts` — exports a module-level shared ref. Initial value from `runtime.WindowIsMaximised()`; flipped locally by `WindowControls` when the max button is clicked. App.vue subscribes to apply the Windows 8 px overflow padding (see Windows section). Module-level means a single source of truth without prop drilling or event plumbing.

Modified files:

- `desktop/main.go` — described above.
- `desktop/frontend/src/App.vue` — delete the `<header class="topbar">` block (lines ~728-781 in the current file, includes the two inline SVGs) and the matching `.topbar` / `.brand` / `.status` / `.icon-btn` / `.badge` / `.dot` CSS in the scoped style block. Replace with `<TitleBar :status="status" :error-msg="errorMsg" :session-count="sessionCount" :remote-endpoint="remoteEndpoint" :available-remote-count="availableRemote.length" :update-badge="updateBadge" @open-remote="showRemote = true" @open-settings="showSettings = true" />`.

### Data flow

App.vue keeps existing reactivity for `status`, `errorMsg`, `sessionCount`, `remoteEndpoint`, `availableRemote`, `updateBadge` (single source of truth). TitleBar is a pure presentational component — no store reads, no business state. WindowControls is self-contained and only interacts with Wails runtime.

### Drag region

Root container of `TitleBar.vue` carries `-webkit-app-region: drag`. All interactive children (icon buttons, WindowControls buttons, status hover area if it ever becomes interactive) carry `-webkit-app-region: no-drag`. On Windows, snap zones (top/left/right) are detected automatically by the OS from the draggable region — no extra code.

On Windows, double-clicking the title bar to toggle maximize is added via `@dblclick.self` on the root container so it only fires when the user hits empty space.

## Platform-specific behavior

### macOS

- Wails options: `Mac: &mac.Options{ TitleBar: mac.TitleBarHiddenInset() }`.
- CSS: `padding-left: 80px` on the TitleBar root.
- Traffic lights remain native overlays — no JS interaction needed.
- Fullscreen: traffic lights disappear and the 80 px padding becomes empty space. Known limitation in v1; documented in the in-app behavior. Future fix: subscribe to a fullscreen-state signal and collapse padding to 0.

### Windows

- Wails options: `Frameless: true`. Windows-specific options (e.g. `DisableWindowIcon`) determined during implementation if a default behavior misbehaves.
- WindowControls renders min/max/close on the right edge.
- Maximized-window overflow fix: when `useWindowMaximized()` is true, App.vue applies an 8 px inset (e.g. via a `.is-maximized` class on `.app` setting `padding: 8px`) to compensate for the borderless-window overflow on each screen edge. The inset removes itself when restored.
- Double-click on TitleBar root toggles maximize (`@dblclick.self`).
- Close button calls `runtime.Quit()` which triggers `OnBeforeClose` and the existing confirm-quit flow.

### Linux

- Wails options: `Frameless: true`.
- WindowControls renders the same three buttons as Windows.
- X11 is the primary target; GNOME + KDE + at least one tiling WM in manual verification.
- Known limitation: no edge-resize handles (GTK frameless drops them). User must use the maximize button or WM keyboard shortcuts. Documented in release notes.
- Known limitation: Wayland behavior depends on compositor support for client-side decoration. Validated best-effort; not blocking.

## Error / fallback handling

- `Environment()` returns falsy or throws → fall back to `linux` rendering (no padding, draw three buttons). Logged with `console.warn`.
- `runtime.WindowMinimise / WindowToggleMaximise / Quit / WindowIsMaximised` missing or throwing → wrap each call in try/catch. On error: button disabled, `console.warn`, no UI crash.
- Initial `isMaximized` defaults to `false` if `WindowIsMaximised()` throws.

## Testing

### Vitest

- `TitleBar.test.ts`
  - With `Environment()` mocked to return darwin/windows/linux:
    - assert root container's `padding-left` matches platform (80 px on darwin, 0 elsewhere)
    - assert `WindowControls` is rendered iff `os !== 'darwin'`
  - Prop pass-through:
    - `sessionCount=3` renders "3 sessions"
    - `sessionCount=1` renders "1 session"
    - `remoteEndpoint` truthy renders "· uplink on"
    - `status='error'` renders `errorMsg` in the bad-color span
    - `availableRemoteCount=2` renders badge "2"; `=0` renders no badge
    - `updateBadge=true` renders the settings dot
  - Events:
    - clicking remote button emits `open-remote`
    - clicking settings button emits `open-settings`
    - `remoteEndpoint=null` disables the remote button
- `WindowControls.test.ts`
  - With `runtime.WindowMinimise / WindowToggleMaximise / Quit` mocked: click each button → corresponding mock called exactly once.
  - `useWindowMaximized` returns `true` on mount → restore-variant icon rendered. Click max → composable ref flips, icon updates to maximize-variant.
  - Any runtime API throwing → button disabled, no thrown error propagates.
- `App.test.ts`
  - Replace existing `.topbar` / `.brand` query assertions with TitleBar stub assertions, keep existing coverage green.

### Go

Build-tagged platform files contain only struct construction; no business logic to unit-test. Existing `desktop/*_test.go` suite must stay green.

### Manual verification

To be run before merge:

- macOS (Apple Silicon, latest stable macOS):
  - Traffic lights visible and centered on the merged bar; no left-side clipping.
  - Title bar dragable; icon buttons and status text do not initiate window drag.
  - Double-click empty area to the right of traffic lights triggers zoom (system behavior).
  - Entering fullscreen leaves the 80 px padding visible (known limitation, acceptance check that it does not block use).
- Windows (Win11 primary; Win10 if available):
  - Three buttons positioned at the right edge; hover highlight matches platform conventions; min/max/close behave correctly.
  - Maximized window does not overflow screen edges (8 px padding fix verified).
  - Double-clicking the title bar toggles maximize.
  - Dragging to top/left/right edge triggers snap zones.
  - Close button with active sessions opens the confirm-quit dialog.
- Linux (X11 primary; GNOME and KDE; one tiling WM):
  - Three buttons functional, same behavior as Windows.
  - Title bar dragable.
  - Known: edge-resize unavailable, only maximize button or WM shortcuts work — confirm documented and acceptable.
  - Wayland: best-effort spot check; document any compositor-specific anomalies.

## Out of scope (deferred)

- macOS fullscreen padding auto-collapse.
- Linux custom resize-edge regions.
- Wayland compatibility hardening.
- Custom traffic-light hover/active states on macOS.
- Logo / app-icon asset replacing the removed brand text.
