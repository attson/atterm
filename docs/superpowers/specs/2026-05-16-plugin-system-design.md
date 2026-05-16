# Plugin System (Phase 1) Design

**Date:** 2026-05-16
**Status:** Draft

## Goal

Introduce a plugin system in the desktop app that adds optional UI modules on top of the terminal, without bloating the default bundle or the default startup path. Ship two first-party plugins in this phase:

1. **Quick Input** — a bottom toolbar of user-defined buttons that send text to the active pane (e.g. `ok`, `continue`, `发布` for AI sessions).
2. **File Explorer** — a right-side panel with a file tree plus a read-only CodeMirror 6 preview, scoped to the current active pane's cwd.

The same framework is meant to host future plugins by adding directories under `desktop/frontend/src/plugins/` and an entry in the registry — no new infrastructure each time.

## Motivation

The desktop app today is a focused terminal: tabs, panes, sessions, remote attach. As workflows around it grow (AI agents that want canned replies; quick file lookup that today requires a separate VS Code window), users want lightweight extensions that live next to the terminal rather than in a separate tool.

Building these as ad-hoc features in `App.vue` would couple them tightly to terminal layout code and balloon the startup bundle (the file viewer needs CodeMirror 6, ~200 KB gzipped, that a user who only wants Quick Input shouldn't pay for). A plugin framework gives each module its own directory, its own state, its own backend bindings, and — critically — its own JS chunk loaded on demand.

## Non-Goals

- **Third-party plugins.** All plugins in this and the foreseeable phase are developed in this repository. No manifest schema, no marketplace, no sandboxing model, no signing flow.
- **Web client plugins.** The vanilla `web/` client does not gain a settings UI or plugin host in this phase. Plugins live in `desktop/` only.
- **Mobile plugins.** Same reason as web.
- **Independent plugin distribution** (download/verify/install separately from the app). Plugins ship inside the same release zip. "Install/uninstall" semantics in this phase mean **enable/disable** — runtime dynamic import vs not.
- **A second plugin API surface beyond Vue.** A plugin is a Vue component plus optional Go bindings co-located in the repo; we do not invent an abstract `PluginAPI` interface this phase.
- **File editing in File Explorer.** Preview only. Editing was considered and rejected for scope.
- **Filesystem watcher across the entire tree.** Watchers are bound to expanded directory nodes only, with a hard cap.
- **Cross-pane broadcast for Quick Input.** Buttons send to the current active pane; never fan out.
- **Hotkeys in conflict-resolution UI beyond rejection.** Settings refuses to save conflicting hotkeys; no auto-rebind suggestions.

## Architecture

### Plugin model

A plugin is a self-contained module under `desktop/frontend/src/plugins/<id>/`. It exposes a default Vue component, an optional Settings sub-component, and (when needed) a Go binding group in `desktop/`. Plugins are referenced from a central registry but loaded via `dynamic import()`, so disabled plugins do not appear in the main JS bundle.

There are exactly two render surfaces ("slots") in this phase:

- `right-panel` — a vertical column on the right side of the main area, width-resizable, collapse/expand toggle. Hosts at most one plugin at a time. `File Explorer` claims this slot.
- `bottom-toolbar` — a fixed 32 px horizontal strip below `PaneGrid`. Hosts at most one plugin at a time. `Quick Input` claims this slot.

If a slot's only plugin is disabled, the slot has `display:none` and contributes zero layout cost.

### Directory layout

```
desktop/frontend/src/
├── plugins/                              ← new
│   ├── registry.ts                       plugin descriptors with lazy loaders
│   ├── PluginHost.vue                    renders a single slot, manages enable state
│   ├── types.ts                          Plugin / Slot / Context interfaces
│   ├── usePluginContext.ts               composable exposing active pane + send()
│   ├── configStore.ts                    Pinia store for PluginConfig
│   ├── quickInput/
│   │   ├── index.ts                      descriptor for registry
│   │   ├── QuickInputBar.vue             slot UI
│   │   ├── QuickInputSettings.vue        settings sub-tab UI
│   │   ├── useQuickInputHotkeys.ts       keyboard binding capture
│   │   ├── defaults.ts                   built-in button presets
│   │   └── *.test.ts
│   └── fileExplorer/
│       ├── index.ts
│       ├── FileExplorer.vue              root component (tree + tabs + editor)
│       ├── FileTree.vue
│       ├── FileTabs.vue
│       ├── FileEditor.vue                CodeMirror 6 read-only host
│       ├── languageMap.ts                extension → language extension
│       └── *.test.ts
├── components/SettingsPlugins.vue        ← new 5th tab in SettingsDialog
└── App.vue                               ← mounts two PluginHost instances
```

```
desktop/
├── plugin_fs.go                          ← new: ListDir / ReadFile / FileMeta / Watch* / Reveal* / OpenExternal
├── plugin_fs_test.go                     ← new
├── app.go                                ← register PluginFS bindings + plugin config getters
├── config.go                             ← extend Config with PluginConfig
└── config_plugin_test.go                 ← new
```

### Registry & dynamic import

```ts
// plugins/types.ts
export type PluginSlot = "right-panel" | "bottom-toolbar";

export interface PluginDescriptor {
  id: "quick-input" | "file-explorer";
  slot: PluginSlot;
  title: string;        // shown in Settings → Plugins
  description: string;
  load: () => Promise<{ default: Component }>;
  defaultEnabled: boolean;
}

// plugins/registry.ts
export const PLUGINS: PluginDescriptor[] = [
  {
    id: "quick-input",
    slot: "bottom-toolbar",
    title: "Quick Input",
    description: "Bottom toolbar of user-defined buttons that send text to the active pane.",
    load: () => import("./quickInput/QuickInputBar.vue"),
    defaultEnabled: true,
  },
  {
    id: "file-explorer",
    slot: "right-panel",
    title: "File Explorer",
    description: "Side panel with file tree and read-only syntax-highlighted preview.",
    load: () => import("./fileExplorer/FileExplorer.vue"),
    defaultEnabled: false,
  },
];
```

Vite splits each `import("./quickInput/QuickInputBar.vue")` and `import("./fileExplorer/FileExplorer.vue")` into its own chunk together with that plugin's transitive dependencies. The File Explorer chunk includes CodeMirror 6 and its language extensions, so a user with File Explorer disabled never pays the 200 KB+ download or the parse/eval cost on startup.

### PluginContext

Plugins do not reach into `App.vue` state directly. They consume a context object provided by `usePluginContext`:

```ts
export interface PluginContext {
  activePane: Ref<Pane | null>;
  activeSessionId: ComputedRef<string | null>;
  activeEndpoint: ComputedRef<Endpoint | null>;
  send: (text: string) => void;          // routed through SessionConnection.sendInput
  showToast: (msg: string) => void;
  getPaneCwd: (sessionId: string) => Promise<string>;
}
```

This is the only API plugins need in phase 1. New surfaces (e.g. command-finished events, terminal selection text) join the interface as further plugins demand them.

### PluginHost.vue

A single PluginHost renders one slot:

```html
<!-- in App.vue -->
<div class="main-row">
  <PaneGrid />
  <PluginHost slot-id="right-panel" />
</div>
<PluginHost slot-id="bottom-toolbar" />
```

Internally:

- Reads `enabledPlugins` from `configStore` and filters `PLUGINS` by `slot === props.slotId && enabled`.
- For each plugin, calls `load()` lazily (only on first enable). Caches the resolved component for the session.
- Renders `<component :is="...">`; passes `PluginContext` as a prop.
- On `load()` rejection: catches, calls `showToast("Plugin <title> failed to load")`, flips the plugin's enabled state back to false in the config store, persists.

### App.vue layout changes

The current layout is `TabBar` over a `main` area that contains `PaneGrid`. The new layout introduces a right-side column and a bottom strip:

```
.app-root (column flex)
├── TabBar
├── .main-row (row flex, grows)
│   ├── PaneGrid (flex: 1)
│   ├── .right-resizer (4 px, cursor: col-resize)   [only when right-panel slot has a plugin enabled]
│   └── PluginHost[slot=right-panel] (width: panelWidthPx)
└── PluginHost[slot=bottom-toolbar] (height: 32 px when present, 0 otherwise)
```

The `.right-resizer` updates CSS width during drag. `TerminalView.vue`'s existing `ResizeObserver` notices the container change and calls `FitAddon.fit()` → `sendResize`; the existing `expectedCols/Rows` skip-on-match guard from red line #6 absorbs duplicate frames. The resize handler is additionally rAF-throttled inside the drag handler so `fit()` runs at most once per animation frame rather than once per `mousemove`.

## Plugin 1: Quick Input

### Behavior

- Renders a horizontal row of buttons inside `bottom-toolbar`. Order matches `buttons[]` config.
- Each button on click calls `context.send(button.send + (button.appendNewline ? "\n" : ""))`.
- `send` is plain-text input identical to a user keystroke; it flows through `SessionConnection.sendInput` → `MSG_IN` frame → existing `desktop/uplink.go` write path. Owner permission gates and read-only relay tokens apply automatically.
- Hover tooltip shows the literal `send` string (with `\n` rendered visibly) plus the hotkey if any.
- Overflow: the toolbar scrolls horizontally; never wraps (would push terminal area up unpredictably).
- The bar is **display-only** in this phase. Adding, removing, editing, or reordering buttons happens exclusively in Settings → Plugins → Quick Input.

### Config schema

```go
// in desktop/config.go
type QuickInputConfig struct {
    Enabled bool                `json:"enabled"`
    Buttons []QuickInputButton  `json:"buttons"`
}

type QuickInputButton struct {
    ID            string `json:"id"`
    Label         string `json:"label"`
    Send          string `json:"send"`
    AppendNewline bool   `json:"appendNewline"`
    Hotkey        string `json:"hotkey,omitempty"`
}
```

Default `Buttons` (set by `Config.applyDefaults` on first run, when `Buttons == nil`):

```
{id: <uuid>, label: "ok",       send: "ok",       appendNewline: true}
{id: <uuid>, label: "continue", send: "continue", appendNewline: true}
{id: <uuid>, label: "发布",     send: "发布",     appendNewline: true}
```

### Hotkeys

A `useQuickInputHotkeys` composable installs a capture-phase keydown handler distinct from `useTerminalShortcuts`. It only listens for `Alt+<digit>` and `Alt+<letter>` combinations to avoid conflicting with existing `Meta/Ctrl+...` shortcuts used by pane/tab management.

Hotkey strings in config are stored as `Alt+1`, `Alt+P`, etc. Empty means no hotkey.

Settings rejects saving a hotkey that:
- duplicates another Quick Input button, or
- collides with the fixed Cmd/Ctrl shortcuts in `useTerminalShortcuts`.

Hotkey conflict checking is purely a Settings-time concern; the runtime handler simply iterates buttons and triggers the first matching one.

### Settings UI

`SettingsPlugins.vue` contains a Quick Input sub-section with:

- An "Enabled" checkbox bound directly to `quickInput.enabled` (immediate apply).
- A buttons table:
  - Drag handle, label input, send-text input, newline checkbox, hotkey-capture input, delete button.
  - An "Add button" row appends an empty entry with a fresh UUID.
- A "Save" button to commit buttons-array changes.
- Same dirty-tracking pattern as `SettingsRelay`: navigating away with unsaved changes triggers the existing discard-confirm dialog.

The split between "checkbox = immediate" and "buttons = draft + save" mirrors how Relay separates `enabled` (immediate) from form fields (draft). This is intentional: enable/disable toggles are reversible single clicks; buttons are multi-field forms where you don't want each keystroke persisted.

## Plugin 2: File Explorer

### Behavior

- Lives in `right-panel`. Internal sub-layout: a 32 px header row, then a horizontally split body with `FileTree` on the left and `FileTabs` + `FileEditor` on the right.
- Header shows the current root directory (truncated, full path in tooltip) plus three icon buttons: refresh, "jump to active pane cwd", and "collapse all".
- Default root directory is the active pane's cwd. When the user clicks a different pane, the tree root follows. A 📌 button in the header pins the current root; while pinned, switching panes does not change the root. Pin state is in-memory only — it resets to "follow" on app restart.
- Single-click on a file: opens it in a **preview** tab. If another preview tab is already open, it's replaced rather than added (VS Code behavior).
- Double-click on a file: opens it as a **persistent** tab (won't be replaced by the next single-click).
- Single-click on a directory: toggles expand/collapse.
- Right-click menu (files): Copy path, Copy relative path, Insert path into terminal (writes the relative path into the active pane's PTY, no newline), Reveal in Finder/Explorer, Open externally.
- Right-click menu (directories): Copy path, Reveal, "Set as root here".
- Hidden files (`.git`, `node_modules`, dotfiles) are filtered unless `showHidden` is true.

### File preview

Files are rendered in CodeMirror 6 configured for read-only:

- `EditorState.readOnly.of(true)` and `EditorView.editable.of(false)`.
- Line numbers, syntax highlighting, search.
- Bundled languages: javascript/typescript, json, markdown, css, html, python. Other extensions fall back to plain text with no highlighting.

Limits:

- Files larger than 2 MB are not loaded into the editor. The preview area shows a placeholder with an "Open externally" button.
- Files detected as binary (first 4 KB contains a NUL byte) show a "Binary file" placeholder.
- If a watched file is modified on disk while previewed, a small badge appears in the corresponding tab with a "Reload" button. We do not auto-reload — that would move the user's reading position. Read-only means no dirty state to merge.

### Tab management

The `FileTabs` row supports:

- Preview tab: italic label, rendered in place; replaced on next single-click selection.
- Persistent tab: rendered with normal styling; created by double-click or via right-click "Keep open".
- Close button per tab. Closing the preview tab clears the preview slot.
- Cap of 8 tabs total. When opening would exceed 8, the oldest non-persistent tab (or, if all are persistent, the least-recently-active persistent tab) is closed first.

### fs watcher

`plugin_fs.go` exposes `WatchDir(path) (handleID, error)` and `UnwatchDir(handleID) error`. Wraps `fsnotify.Watcher`.

- Watching is **per node**, not recursive. When the user expands a directory in `FileTree`, the frontend calls `WatchDir`; when the node is collapsed (or unmounted), it calls `UnwatchDir`.
- A process-wide hard cap of **200 active watchers**. If `WatchDir` would exceed this, it returns `ErrTooManyWatchers`. Frontend handles this by falling back to "refresh button only" mode for the offending node and logging a warning. The user can manually refresh; they can also disable watchers entirely for a session with a future setting (not in phase 1).
- Coalescing: a 100 ms debounce window per directory collects bursts of create/modify/delete events into a single Wails event `plugin-fs:dir-changed` carrying the directory path.
- When the file currently shown in the editor is in a changed directory, the frontend re-fetches `FileMeta` for it; if mtime moved forward, it shows the "Reload" badge.

### Backend bindings (`PluginFS`)

```go
type PluginFS struct {
    allowRoots []string  // computed at call time: $HOME + cwds of currently active sessions
}

type DirEntry struct {
    Name    string `json:"name"`
    IsDir   bool   `json:"isDir"`
    Size    int64  `json:"size,omitempty"`
    ModTime int64  `json:"modTime,omitempty"`  // unix ms
}

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

func (p *PluginFS) ListDir(path string) ([]DirEntry, error)
func (p *PluginFS) ReadFile(path string, maxBytes int64) (FileContent, error)
func (p *PluginFS) FileMeta(path string) (FileMetaInfo, error)
func (p *PluginFS) WatchDir(path string) (int64, error)
func (p *PluginFS) UnwatchDir(handleID int64) error
func (p *PluginFS) RevealInOS(path string) error
func (p *PluginFS) OpenExternal(path string) error
```

The frontend caps `maxBytes` at 2 MB; the backend additionally rejects requests above 5 MB. `FileContent.TruncatedAt` is set when truncation occurs so the UI can render a "file truncated, open externally" hint.

### Path allowlist

Every binding that accepts a `path` parameter runs the same validation before any I/O:

1. The path must be absolute (no `~`, no relative).
2. The path is resolved via `filepath.EvalSymlinks` before further checks. Any symlink escape outside the allow-roots is rejected.
3. The resolved path must be a child of one of the **allow-roots**:
   - The user's home directory (`os.UserHomeDir()`).
   - The cwd of every currently active local session (collected from the existing session map).
4. The path must not equal or be a child of any *deny pattern*: `~/.ssh`, `~/.gnupg`, `~/.aws`, anything matching `.env`/`.env.*` at the file level.

A violation returns `ErrPathForbidden`; the frontend shows a generic "Access denied" toast.

These checks live in `plugin_fs.go`'s internal `resolve(path)` function and are exercised by `plugin_fs_test.go` against every escape technique we can enumerate.

### Config schema

```go
type FileExplorerConfig struct {
    Enabled        bool    `json:"enabled"`
    PanelWidthPx   int     `json:"panelWidthPx"`
    PanelCollapsed bool    `json:"panelCollapsed"`
    InnerTreeRatio float64 `json:"innerTreeRatio"`
    ShowHidden     bool    `json:"showHidden"`
}
```

Default values when missing: `panelWidthPx=380`, `panelCollapsed=true`, `innerTreeRatio=0.3`, `showHidden=false`.

The two resizers (outer panel width and inner tree ratio) update CSS during drag and throttle-write (trailing 300 ms) to config. A double-click on a divider restores the default. Bounds: `panelWidthPx ∈ [240, viewport*0.7]`, `innerTreeRatio ∈ [0.15, 0.5]`. PinnedCwd is not in config; it is in-memory state in the plugin's local store.

## Settings integration

### Plugins tab

A 5th tab `plugins` is added to `SettingsDialog.vue`. The tab body:

```
Plugins are loaded on demand. Disabled plugins do not affect
startup time or memory.

┌────────────────────────────────────────────────────────┐
│ ☑ Quick Input                                  ▾       │
│   <description>                                        │
│   <buttons table when expanded>                        │
├────────────────────────────────────────────────────────┤
│ ☐ File Explorer                                 ▾       │
│   <description>                                        │
│   ☐ Show hidden files                                  │
│   Panel width: 380 px (resize at view)                 │
│   Inner tree ratio: 30% (resize at view)               │
└────────────────────────────────────────────────────────┘
```

Each plugin row has a top-level enable checkbox (immediate apply) and an expand/collapse triangle. Expanding shows plugin-specific settings.

### Wails bindings

```go
// in app.go
func (a *App) GetPluginConfig() PluginConfig
func (a *App) SetPluginConfig(c PluginConfig) error
```

`SetPluginConfig` validates the payload (UUID uniqueness; numeric bounds; hotkey format and conflicts), writes config via the existing atomic save, then emits `plugin-config-changed` (full new PluginConfig).

### Frontend store

`plugins/configStore.ts` is a Pinia store with `cfg: Ref<PluginConfig | null>`, `load()`, and `save(next)`. It subscribes to `plugin-config-changed` events from Wails and updates its `cfg` ref. All plugin components and `PluginHost` consume the same store; nobody calls `GetPluginConfig` directly outside the store.

### Dirty/save model

Per-plugin settings come in two flavors:

- **Immediate**: enabled checkbox, show-hidden checkbox. Toggled values save instantly.
- **Draft + Save**: Quick Input buttons array. The draft is held locally in `QuickInputSettings.vue`; Save commits it; navigating away with a dirty draft triggers the existing discard-confirm modal pattern.

Resizer drags (panel width, inner tree ratio) write directly through `save()` with trailing throttle and are not part of any draft.

## Red-line alignment

Inspection of every numbered red line in `AGENTS.md`:

| # | Red line                                          | Compliance                                                                                                                                                  |
| - | ------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1 | Local-first                                       | Plugins live in `desktop/`. No relay dependency. Quick Input send → existing local PTY path. File Explorer is purely local fs.                              |
| 2 | Lazy upload                                       | Quick Input flows through `SessionConnection.sendInput`, same code path as keystrokes; existing STREAM_REQUEST/STOP semantics unaffected.                   |
| 3 | session_id is authoritative                       | `PluginContext.activeSessionId` is the only handle plugins receive. host_id is never used.                                                                  |
| 4 | Protocol backward compatibility                   | **No new proto frames.** Quick Input reuses `MSG_IN`. File Explorer is Wails-only and never crosses the wire.                                               |
| 5 | internal/ does not import desktop/                | `plugin_fs.go` lives in `desktop/`. No code is added under `internal/`. Direction `desktop/ → internal/*` preserved.                                        |
| 6 | PTY winsize set at fork; coalesce RESIZE          | Panel resize updates CSS only; the existing `ResizeObserver` + `expectedCols/Rows` skip-on-match guard in `TerminalView.vue` absorbs duplicate frames; the drag handler additionally rAF-throttles `fit()`.       |
| 7 | Updater is user-initiated                         | Updater untouched.                                                                                                                                          |
| 8 | Auto-update must verify signatures                | No third-party distribution introduced. Signing flow unchanged.                                                                                             |
| 9 | Public-relay safe defaults                        | Relay code untouched.                                                                                                                                       |
| 10 | Web client does not depend on CDN                | `web/` is not modified. No new external assets.                                                                                                             |
| 11 | Remote permission is owner's; enforced relay-side | Quick Input send goes through `SessionConnection.sendInput`, automatically subject to both relay-side and uplink-side read-only gates. File Explorer bindings are local-only and are not reachable via uplink (see Risks). |
| 12 | REPLAY_PROGRESS must remain                       | Not touched.                                                                                                                                                |

The single area requiring vigilance is #11: `plugin_fs.go` must not become a vector via uplink. Mitigations:

- `plugin_fs.go` exports methods only through `wails.Bind`; nothing in `uplink.go` or `internal/relay` calls them.
- A CI step (`grep -r 'PluginFS' desktop/uplink*.go internal/`) asserts that no code in the uplink or internal package references `PluginFS`. The grep being non-empty fails the build.
- A doc-comment red line in `plugin_fs.go` itself states "do not expose via uplink or relay; local Wails binding only".

## Risks and mitigations

| Risk                                                                                       | Mitigation                                                                                                                                                                                 |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| fs watcher handle exhaustion (user expands a tree with deep `node_modules` directories)   | Watchers are per-node only; hard cap of 200; over-cap falls back to manual refresh with a warning. No recursive watches.                                                                   |
| CodeMirror chunk fails to load (e.g. partial download, stale service worker)              | `import().catch()` flips the plugin back to disabled and shows a toast. The user can retry by toggling enable.                                                                             |
| Path-allowlist escape via symlinks or `..`                                                 | `filepath.EvalSymlinks` is mandatory before allowlist check. Tests cover every escape pattern we enumerate (parent-traversal, symlink-out, absolute-injection, deny-pattern paths).        |
| Hotkey conflicts                                                                            | Settings rejects conflicting hotkey assignments at save time. Runtime handler is order-insensitive.                                                                                        |
| Large file preview hang                                                                     | `FileMeta` is called before `ReadFile`. Files > 2 MB never reach the editor; UI shows "Open externally".                                                                                  |
| Bottom toolbar steals vertical space from terminal                                          | Fixed 32 px height. When disabled or empty, `display:none` (0 px). Existing `predictCellDims` recomputes PTY size on any layout change.                                                   |
| Panel resize triggers a flood of RESIZE frames during drag                                  | Drag updates CSS only; existing `ResizeObserver` plus `expectedCols/Rows` skip-on-match guard absorb duplicates; drag handler is rAF-throttled.                                            |
| Settings tab list overflows on narrow Linux windows                                         | Settings tab bar already has overflow handling (see existing `SettingsDialog.vue` patterns); we follow the same conventions for the 5th tab.                                              |
| Read-only file preview goes stale while user is reading                                     | fs watcher emits `dir-changed`; affected tabs show a non-blocking badge with a Reload button. No auto-reload (preserves scroll position).                                                  |

## Test matrix

| File                                          | Coverage                                                                                                       |
| --------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `plugins/PluginHost.test.ts`                  | enable toggle triggers lazy import/unmount; load failure flips enable back to false; slot routing.             |
| `plugins/configStore.test.ts`                 | `load()` populates ref; `save()` round-trips; `plugin-config-changed` event updates ref.                       |
| `quickInput/QuickInputBar.test.ts`            | click → send with/without newline; empty button list renders nothing; toast on read-only relay rejection.      |
| `quickInput/QuickInputSettings.test.ts`       | add/edit/delete/reorder; hotkey conflict rejection; dirty/save and discard flow.                               |
| `quickInput/useQuickInputHotkeys.test.ts`     | Alt+digit triggers send; coexists with `useTerminalShortcuts` (no double-handling).                            |
| `fileExplorer/FileTree.test.ts`               | lazy node expansion; hidden-file filtering; watcher attach/detach on expand/collapse; over-cap fallback.       |
| `fileExplorer/FileEditor.test.ts`             | large file rendered as placeholder; binary file rendered as placeholder; external modify shows Reload badge.   |
| `fileExplorer/FileTabs.test.ts`               | preview→persistent transition; 8-tab LRU eviction.                                                              |
| `desktop/plugin_fs_test.go`                   | allowlist enforcement (every escape pattern); deny-pattern paths; truncation; binary detection; atomic Watch lifecycle; cap enforcement. |
| `desktop/config_plugin_test.go`               | default injection on first run; SetPluginConfig validation (UUID uniqueness, numeric bounds, hotkey format); atomic write; graceful upgrade from configs missing newer fields. |
| `desktop/app_plugin_test.go`                  | end-to-end `GetPluginConfig`/`SetPluginConfig` round-trip; `plugin-config-changed` event emission.             |

CI command: existing `go vet -tags webkit2_41 ./... && go test -tags webkit2_41 -timeout 60s ./desktop/ && (cd desktop/frontend && npm run build && npm test)`.

## Incremental delivery

The plan splits into seven independently-reviewable steps. Each step ends in a green build, green tests, and a usable (if not feature-complete) state.

1. **Plugin framework skeleton.** Add `plugins/registry.ts`, `PluginHost.vue`, `types.ts`, `configStore.ts`, `SettingsPlugins.vue` (empty placeholder rows). Extend `Config` with empty `PluginConfig`. Wire `App.vue` layout (two empty PluginHost mounts, both `display:none`). Tests for `PluginHost` and `configStore`.
2. **Quick Input complete.** Add `quickInput/*` files, defaults, hotkey composable, Settings sub-tab. End state: ship-ready Quick Input plugin enabled by default.
3. **`plugin_fs.go` foundation.** Add `ListDir`, `ReadFile`, `FileMeta`, path allowlist, plus all-pattern unit tests. No frontend changes yet.
4. **File Explorer tree + tabs (no editor yet, no watcher).** Add `FileTree`, `FileTabs`, `FileExplorer` shell. Files clicked show a placeholder (no CodeMirror yet). Tabs work, preview/persistent transition works.
5. **CodeMirror 6 editor integration.** Add `FileEditor.vue`, language map, large/binary guards. End state: File Explorer fully usable as read-only viewer, with manual refresh button only.
6. **fs watcher.** Add `WatchDir`/`UnwatchDir` bindings, frontend wiring on expand/collapse, dir-changed event handler, reload badge.
7. **Resizers and polish.** Outer panel resizer, inner tree ratio resizer, PinnedCwd in-memory state, collapse toggle.

Each step's PR description should call out the step number and link this design.
