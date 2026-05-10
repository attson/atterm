# Pane Split Layouts — Design

**Status**: Approved (brainstorming) → ready for implementation plan
**Date**: 2026-05-10
**Scope**: desktop frontend only; backend/protocol unchanged

## Goal

Let the desktop app show 1, 2 (left/right or top/bottom), or 4 (2×2) terminals
inside a single tab, and add iTerm-style keyboard shortcuts to enter and
manage those layouts. Each pane is an independent local PTY session, or an
attached existing local/remote session picked by the user.

Today the app is one PTY per tab. After this change a tab is a layout
container; its panes are sessions.

## Non-goals

- Recursive nested splits (tmux-style trees). Only the four fixed layouts.
- Drag-resizable splitters. Panes share space evenly via CSS Grid.
- Cross-tab "drag a tab into a pane" UI.
- Persisting layouts across app restarts. State is in-memory.
- Backend / protocol changes.

## Glossary

- **Layout**: one of `single`, `vertical` (left/right), `horizontal` (top/bottom),
  `grid2x2` (2×2).
- **Pane**: a fixed slot in a layout. Holds a `sessionId` (local or remote) or
  is empty (`null` after a session ends or picker is canceled).
- **Tab**: a layout + its pane array + an active-pane index. The unit displayed
  in the top tab bar.

## Data model

```ts
type LayoutKind = "single" | "vertical" | "horizontal" | "grid2x2";

interface Pane {
  sessionId: string | null;   // null = empty slot
  remote: boolean;            // routes to remote endpoint vs local mini-relay
}

interface Tab {
  id: string;                 // frontend uuid, Vue key only
  layout: LayoutKind;
  panes: Pane[];              // length matches layout: 1 / 2 / 2 / 4
  activePaneIdx: number;      // keyboard-focused pane
}
```

Pane index → grid position is fixed per layout:

| Layout       | indices                                     |
|--------------|---------------------------------------------|
| `single`     | `[0]`                                       |
| `vertical`   | `[0=left, 1=right]`                         |
| `horizontal` | `[0=top, 1=bottom]`                         |
| `grid2x2`    | `[0=TL, 1=TR, 2=BL, 3=BR]`                  |

`App.vue` holds:

```ts
const tabs = ref<Tab[]>([]);
const currentTabId = ref<string | null>(null);
```

`localList` / `remoteList` (from polling) remain, but become *lookups*: used by
the session picker, by cwd inheritance, and to detect sessions that have
disappeared so we can null their pane slot.

URL hash changes from `#/s/<sessionId>` to `#/t/<tabId>`.

### Invariants

1. A `sessionId` MUST NOT appear twice in the same tab's `panes`. Same id may
   appear in different tabs.
2. Closing a tab closes every local session in its panes; remote-pane entries
   are only detached.
3. If a session disappears from polling, its pane slot becomes `null`. Layout
   is not reduced automatically.
4. If `tabs` becomes empty, `App.vue` auto-starts a fresh `single` tab — same
   safety net the current code has.

## Component structure

```
App.vue                                tabs / endpoints / global shortcuts
 ├─ TabBar.vue                         tabs (was: sessions); shows layout icon
 ├─ PaneGrid.vue        ★ new          renders 1/2/4 cells via CSS Grid
 │   └─ TerminalView.vue × N           one xterm + SessionConnection per pane
 ├─ SessionPickerDialog.vue ★ new      pick existing local/remote session
 ├─ SettingsDialog.vue                 unchanged
 └─ RemoteSessionsDialog.vue           unchanged (still opens remote-as-tab)

composables/
 └─ useTerminalShortcuts.ts ★ new      document capture-phase keydown router

lib/
 ├─ types.ts            ★ new          LayoutKind / Pane / Tab
 └─ layout.ts           ★ new          pure functions (transition / close / focus)
```

### `PaneGrid.vue`

```ts
defineProps<{
  tab: Tab;
  endpointFor: (pane: Pane) => Endpoint | null;
  active: boolean;     // is this tab the currently visible one
}>();

defineEmits<{
  (e: "set-active-pane", paneIdx: number): void;
  (e: "close-pane",      paneIdx: number): void;
  (e: "tab-empty"): void;   // last pane closed → App.vue closes tab
}>();
```

CSS template is purely declarative; no JS sizing:

```css
.grid.single     { grid-template: "a"; }
.grid.vertical   { grid-template: "a b" / 1fr 1fr; }
.grid.horizontal { grid-template: "a" 1fr "b" 1fr; }
.grid.grid2x2    { grid-template: "a b" 1fr "c d" 1fr / 1fr 1fr; }
```

`TerminalView` already has a `ResizeObserver` + guarded `safeFit`; CSS Grid
resizes propagate without explicit notification.

### `SessionPickerDialog.vue`

```ts
defineProps<{
  excludeSessionIds: string[];     // ids already in the current tab
  localSessions: SessionInfo[];
  remoteSessions: SessionInfo[];
}>();

defineEmits<{
  (e: "pick", payload: { sessionId: string; remote: boolean }): void;
  (e: "close"): void;              // ESC / cancel
}>();
```

### `useTerminalShortcuts.ts`

Single composable; subscribes once at `App.vue` mount and disposes on unmount.
Uses `document.addEventListener("keydown", h, { capture: true })` so it runs
before xterm. Calls `e.preventDefault() + e.stopPropagation()` on match.

```ts
useTerminalShortcuts({
  onSplitVertical:   (mode: "new" | "pick") => void,
  onSplitHorizontal: (mode: "new" | "pick") => void,
  onClosePane:       () => void,
  onFocusPane:       (dir: "left" | "right" | "up" | "down") => void,
  onNewTab:          () => void,
  onSwitchTab:       (delta: number) => void,
});
```

Mod resolution: `navigator.platform.includes("Mac") ? "Meta" : "Control"`.

### `TerminalView.vue` change

Add a `focused: boolean` prop (rendered as a 1px accent border). Existing
behavior (mount, fit, attach, focus on `active` flip) stays the same. The
`active` prop continues to mean "owning tab is visible"; `focused` is per-pane.

### `TabBar.vue` change

Tab title = the active pane's session's `shortTitle`. A small icon next to the
title indicates layout when not `single`. Close button on a tab closes the
whole tab (== close every local pane in it).

## Shortcuts

| Key (`Mod` = ⌘ on macOS, Ctrl elsewhere) | Action |
|------------------------------------------|--------|
| `Mod+D`                | split vertical (new shell)           |
| `Mod+Shift+D`          | split horizontal (new shell)         |
| `Mod+Alt+D`            | split vertical (open picker)         |
| `Mod+Alt+Shift+D`      | split horizontal (open picker)       |
| `Mod+W`                | close current pane (single → tab)    |
| `Mod+Alt+←/→/↑/↓`      | focus neighbor pane                  |
| `Mod+T`                | new tab                              |
| `Mod+Shift+[` / `]`    | previous / next tab                  |

## Layout transition rules

### `transitionLayout(layout, activeIdx, dir) → { layout, panes, activeIdx, newPaneIdx }`

Pure function. Returns the new tab shape with **exactly one** new empty pane
slot inserted at `newPaneIdx`, which the caller fills with a session id (new
shell, or picked).

Behavior by current layout:

**From `single` (1 pane).** Direction matters here because the result has only
2 panes and they can be arranged either way.

| `dir`        | result                                     |
|--------------|--------------------------------------------|
| `vertical`   | `vertical`; new pane at idx 1 (right)      |
| `horizontal` | `horizontal`; new pane at idx 1 (bottom)   |

**From `vertical` or `horizontal` (2 panes).** Promote to `grid2x2` with
exactly **3 panes filled** and 1 quadrant empty. Direction is ignored: from
2 panes there is only one geometrically valid place to put a third in the
fixed 2×2 grid. Existing panes are renumbered into 2×2 indices, and the new
pane fills the slot in the active pane's row/column.

`vertical → grid2x2` (existing idx `0=left, 1=right` → 2×2 `0=TL, 1=TR`):

| activeIdx (in `vertical`) | new active (in `grid2x2`) | newPaneIdx |
|---------------------------|---------------------------|------------|
| 0 (left)                  | 2 (BL)                    | 2 (BL)     |
| 1 (right)                 | 3 (BR)                    | 3 (BR)     |

> Active jumps to the new pane (matches iTerm — focus follows the just-created
> pane so the user can immediately type into it).

`horizontal → grid2x2` (existing idx `0=top, 1=bottom` → 2×2 `0=TL, 2=BL`):

| activeIdx (in `horizontal`) | new active (in `grid2x2`) | newPaneIdx |
|-----------------------------|---------------------------|------------|
| 0 (top)                     | 1 (TR)                    | 1 (TR)     |
| 1 (bottom)                  | 3 (BR)                    | 3 (BR)     |

**From `grid2x2` (4 occupied panes).** No-op; surface a status-bar "pane full"
toast. Direction ignored.

**From `grid2x2` with empty slots (3 or fewer occupied).** This state only
arises after closing a pane. Pressing a split shortcut fills the *first* empty
slot (lowest idx) with the new pane and sets active to it. Direction is
ignored, same reason as above. (If the user wants the new pane in a specific
position, they should rearrange via close-and-resplit.)

### `closePane(layout, panes, closeIdx) → { layout, panes, activeIdx }`

| from       | rule                                                                                       |
|------------|--------------------------------------------------------------------------------------------|
| single     | caller closes the tab                                                                      |
| vertical   | → `single` with the surviving pane                                                         |
| horizontal | → `single` with the surviving pane                                                         |
| grid2x2    | keep `grid2x2`, set the closed slot to `null`. When 2 panes remain, reduce to `vertical` if remaining indices are `{0,1}` or `{2,3}`, else to `horizontal` (`{0,2}` or `{1,3}`), else (diagonals `{0,3}` / `{1,2}`) collapse to `vertical` (left/right of the surviving sessions, sorted by index). When only 1 remains, `single`. |

This deliberately never closes a session you didn't ask to close.

### `focusNeighbor(layout, activeIdx, dir) → number | null`

Static lookup tables per layout:

```ts
const NEIGHBOR = {
  single:     { 0: {} },
  vertical:   { 0: { right: 1 }, 1: { left: 0 } },
  horizontal: { 0: { down: 1 },  1: { up: 0 } },
  grid2x2:    {
    0: { right: 1, down: 2 },
    1: { left:  0, down: 3 },
    2: { up:    0, right: 3 },
    3: { up:    1, left: 2 },
  },
};
```

`null` (no neighbor in that direction) is a no-op.

## cwd inheritance

When `Mod+D` / `Mod+Shift+D` triggers a new shell:

1. Look up the active pane's session in `localList`. If found, use its `cwd`
   (poll keeps this fresh from `/proc`).
2. Pass it as `NewSessionReq.cwd`. Backend already supports this field.
3. Remote-active or empty cwd → leave field empty; Go backend uses the desktop
   process's cwd as fallback (existing behavior).

Picker-mode splits skip cwd inheritance: the picked session already has its
own cwd.

## Edge cases

- **Picker canceled**: layout stays in its just-promoted state; the new pane is
  `null`. User can pick later via right-click (future) or just close it.
- **Session ends in background**: poll detects the missing id; the pane slot
  becomes `null`. Layout is not reduced.
- **Same-tab duplicates**: picker excludes the current tab's session ids.
- **All tabs closed**: `App.vue` auto-starts a `single` tab (current behavior
  preserved).
- **macOS application menu**: default Cocoa menu eats `⌘W`/`⌘Q`/`⌘M`/`⌘H`
  before webview sees them. `desktop/main.go` (macOS-only branch) installs an
  empty / accelerator-stripped menu so the webview owns these combos. Linux
  and Windows webviews already pass these keys through; no change there.
- **Same session in two tabs**: allowed. Relay supports multi-subscriber
  fan-out; both panes will mirror.

## Files to change

| #  | File                                                              | Type | Why |
|----|-------------------------------------------------------------------|------|-----|
| 1  | `desktop/frontend/src/lib/types.ts`                               | + new | LayoutKind, Pane, Tab |
| 2  | `desktop/frontend/src/lib/layout.ts`                              | + new | transitionLayout / closePane / focusNeighbor |
| 3  | `desktop/frontend/src/lib/layout.test.ts`                         | + new | unit tests for the above |
| 4  | `desktop/frontend/src/composables/useTerminalShortcuts.ts`        | + new | global key router |
| 5  | `desktop/frontend/src/components/PaneGrid.vue`                    | + new | grid renderer |
| 6  | `desktop/frontend/src/components/SessionPickerDialog.vue`         | + new | pick existing session |
| 7  | `desktop/frontend/src/components/TerminalView.vue`                | M    | add `focused` prop, accent border |
| 8  | `desktop/frontend/src/components/TabBar.vue`                      | M    | render tabs (not sessions); layout icon |
| 9  | `desktop/frontend/src/App.vue`                                    | M    | tabs model; hash → `#/t/<tabId>`; wire shortcuts; cwd inheritance |
| 10 | `desktop/main.go`                                                 | M    | macOS-only: install empty menu |
| 11 | `desktop/frontend/package.json` + `vite.config.ts`                | M    | add `vitest` dev dep |

Backend (`internal/*`, `relay_host.go`, `uplink.go`, protocol): unchanged.

## Test plan

### Unit tests (vitest, `lib/layout.test.ts`)

- `transitionLayout`:
  - From `single`: both directions × the only activeIdx (=0). Assert resulting
    layout, panes length, `activeIdx`, `newPaneIdx`.
  - From `vertical` / `horizontal`: every activeIdx × both directions; assert
    direction-agnostic — both directions from the same start state produce the
    same `(layout, activeIdx, newPaneIdx)`.
  - From `grid2x2` (full): both directions are no-op (return value flagged so
    caller can show toast).
  - From `grid2x2` with 1, 2, or 3 empty slots: new pane fills lowest-idx
    empty slot; active jumps to it.
- `closePane`:
  - `single` close → caller closes tab (return value flagged).
  - `vertical` / `horizontal` close either idx → `single` with surviving pane.
  - `grid2x2` (4 full) close any idx → `grid2x2` (3 full, 1 null), active
    follows surviving pane (e.g. lowest-idx surviving).
  - `grid2x2` (3 full) close → reduce to `vertical` / `horizontal` based on
    which two indices remain (per the table in §"Layout transition rules").
  - Diagonal-pair case (`{0,3}` or `{1,2}`) → `vertical`, sessions ordered
    left-to-right.
- `focusNeighbor`: every (layout, activeIdx, dir) returns expected idx or
  null for boundaries.

If `desktop/frontend` lacks vitest, add it as a dev dep with the standard Vue 3
configuration. No new runtime dependencies.

### Existing tests

- `desktop/uplink_e2e_test.go` — unchanged, must still pass. Validates that
  protocol hasn't drifted.
- `go vet -tags webkit2_41 ./...` — must stay clean.
- `cd desktop/frontend && npm run build` — type-check + Vite build must stay
  clean.

### Manual verification (before claiming done)

1. Single tab → `Mod+D` → right pane spawns new shell with parent's cwd.
2. From `vertical`, press `Mod+D` again → `grid2x2`, new bottom-of-active-column.
3. From `single`, `Mod+Shift+D` → `horizontal`. Then `Mod+D` → `grid2x2`.
4. `Mod+Alt+D` → picker; pick a local session → fills slot. Pick a remote
   session → fills slot, ends rendered through remote endpoint.
5. `Mod+Alt+←/→/↑/↓` traverses 2×2 panes; boundary attempts are no-ops.
6. `Mod+W`: `grid2x2` (4) → `grid2x2` (3, one empty) → `vertical`/`horizontal`
   (2) → `single` → tab closes → auto-restart `single`.
7. Shell exits naturally inside a pane: pane shows `[atterm] session ended`,
   slot empties, layout unchanged.
8. Disconnect remote relay: remote pane shows `reconnecting…`; local panes
   unaffected (red line #1).
9. macOS: `⌘W` reaches the JS handler, doesn't close the OS window. `⌘Q`
   still quits — that's intentional (only strip the menu accelerators we
   conflict with, leave Quit alone, or strip and re-add Quit on a non-`⌘Q`
   binding — decide during impl, default = leave Quit on `⌘Q`).
10. Linux + Windows: every shortcut works with `Ctrl` instead of `⌘`. Shells
    that interpret `Ctrl+D` as EOF are unaffected because we capture before
    xterm.

## Risks

- **xterm key swallowing**: capture-phase listener must run before xterm's
  own handlers in every mounted `TerminalView`. Verified per-pane in step 1
  of manual verification.
- **First-frame fit race when promoting layouts**: at most one new
  `TerminalView` mounts per transition (single→vertical, vertical→grid2x2,
  etc.), but the surviving panes get re-laid-out by CSS Grid the same frame.
  `safeFit` already guards `width<2`; if flicker appears, wrap the initial
  fit in `nextTick(() => safeFit())` inside `PaneGrid`.
- **macOS menu strip**: stripping more than needed could break user
  expectations (Cmd+Q quit, Cmd+H hide). Only strip Cmd+W and Cmd+M; keep
  Cmd+Q and Cmd+H as Cocoa defaults.
- **Picker race**: while picker is open, polling can remove or add sessions.
  Picker re-renders from props; selecting a since-vanished id is rejected
  (the dialog filters against fresh `localSessions`/`remoteSessions`).

## Out of scope (revisit later)

- Drag-resizable splitters
- Recursive split trees
- Persisting layouts across restarts (would live in `~/.config/atterm/config.json`)
- Drag tabs between panes / drag panes between tabs
- Per-pane right-click menu (would replace ESC-canceled-picker recovery path)
