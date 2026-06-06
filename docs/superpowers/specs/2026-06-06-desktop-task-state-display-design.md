# Desktop task state display (design)

Date: 2026-06-06
Status: Draft (design phase); pending implementation plan
Roadmap item: P-personal — frontend continuation of the session attention
track. Builds on `2026-06-06-session-attention-model-design.md` (relay
backend already implemented on `feat/session-attention-backend`).

## 1. Goal

The desktop client does not render `task_state`. The tab bar shows a
constant green dot regardless of whether a session is running, waiting
for input, completed, or failed. There is no surface that aggregates
sessions across hosts or surfaces which sessions need attention. Now
that the relay backend ships `task_state`, `attention_at`, `unread`,
`type`, and `summary` on `SessionInfo` and `MetaPayload`, the desktop
should consume them.

This spec covers three surfaces, plus a switchable visual preset:

1. A new collapsible **task sidebar** on the left of the desktop window,
   grouped by host, with state-colored icons, animated running
   indicator, unread dots, mark-read controls, and a "completed (seen)"
   fold.
2. **`TabBar.vue`** replaces its static green dot with a state-driven
   icon (per the active preset) and adds an unread `●` next to the
   title.
3. **`RemoteSessionsDialog.vue`** is upgraded from host-only grouping
   to the same state-rich rendering as the sidebar.

A user-switchable **two-preset visual system** — `Vivid` (default;
colorful + animated + type icons) and `Quiet` (muted; only running is
animated; no type icons in sidebar/dialog) — applies to all three
surfaces uniformly.

After this lands:

- The user can scan all sessions at a glance and see which need
  attention without opening tabs.
- The unread inbox model (delivered by the backend) becomes
  user-visible on the desktop.
- Mark-read flows hit the existing `POST /api/sessions/seen`.

Out of scope:

- Mobile `MobileSessionList.vue` rendering — keep current layout.
- Free theming or per-state color customization — only two fixed
  presets.
- Seen tracking for local-only sessions never uplinked to the relay —
  they render `task_state` but no unread treatment.
- OS notification reform — Web Push already covers this.
- driver/viewer overlay restyle — orthogonal layer.

## 2. Architecture

```
┌── desktop/frontend/src/ ─────────────────────────────────────────────┐
│                                                                       │
│  lib/                                                                 │
│    taskState.ts (new)                                                 │
│      type TaskState = 'idle' | 'running' | 'waiting_input'            │
│                    | 'completed' | 'failed'                           │
│                    | 'disconnected' | 'closed'                        │
│      type PresetId = 'vivid' | 'quiet'                                │
│      interface TaskStatePreset { ... }                                │
│      export const presets: Record<PresetId, TaskStatePreset>          │
│    sessionGroup.ts (new) — extract groupSessionsByHost                │
│                                                                       │
│  composables/                                                         │
│    useTaskPreset.ts (new)                                             │
│      reactive activePreset from Settings; localStorage fallback       │
│      writes document.documentElement.dataset.taskPreset               │
│    useSessions.ts (new)                                               │
│      merges localList + remoteList → byHost / remoteByHost /          │
│      unreadByHost / primaryStateForHost / totalUnread / completedSeen │
│                                                                       │
│  components/                                                          │
│    TaskStateIcon.vue (new) — atomic state visual                      │
│    TaskGroupedList.vue (new) — host groups + rows + fold + emits      │
│    TaskSidebar.vue (new) — collapsible left rail                      │
│    TabBar.vue (modified) — TaskStateIcon + unread •                   │
│    RemoteSessionsDialog.vue (modified) — uses TaskGroupedList         │
│    SettingsDialog.vue (modified) — new "Task display" section         │
│                                                                       │
│  platform/types.ts (modified)                                         │
│    Settings + { taskPreset: PresetId; taskSidebarCollapsed: boolean } │
│                                                                       │
│  App.vue (modified)                                                   │
│    layout adds TaskSidebar; localList/remoteList → useSessions()      │
│    Cmd/Ctrl+B hotkey toggles sidebar                                  │
│                                                                       │
│  lib/api.ts (modified)                                                │
│    + markSessionsSeen(opts: {ids: string[]} | {all: true})            │
│                                                                       │
│  i18n/messages/{en,zh-CN}.ts (modified)                               │
│    + tasks.sidebar.title, tasks.preset.{vivid,quiet}.{name,desc},     │
│      tasks.markAllRead, tasks.markRead, tasks.completedFold,          │
│      tasks.unreadCount, tasks.settings.section                        │
└───────────────────────────────────────────────────────────────────────┘
```

Three vertically-stacked layers each with one job:

- **`TaskStateIcon`** — pure visual; `(state, preset) → glyph + color +
  animation`. Knows nothing about sessions or stores.
- **`TaskGroupedList`** — list rendering; `sessions → host groups +
  rows + completed fold + mark-read emits`.
- **`TaskSidebar`** / **`RemoteSessionsDialog`** — shells; collapse
  behavior / modal chrome.

The two consuming surfaces (sidebar and dialog) share
`TaskGroupedList`, so changes to row layout, completed-fold behavior,
or mark-read affordances stay in one file.

## 3. Visual library

### Preset shape

```ts
interface TaskStatePreset {
  id: PresetId
  i18nKey: string                                  // 'tasks.preset.vivid'
  colorOf(state: TaskState): string                // hex
  glyphOf(state: TaskState): 'spinner' | string    // 'spinner' or '◐ ✓ ✗ ·'
  spinnerDurationMs(state: TaskState): number      // running only
  animatePulse(state: TaskState): boolean          // waiting_input pulse, Vivid only
  showTypeIcon: boolean                            // Vivid true, Quiet false
  textOpacity: number                              // 1.0 / 0.75
}
```

### Preset table

| state | Vivid | Quiet |
|---|---|---|
| `running` | cyan `#06b6d4` spinner, 1500ms/rev | muted cyan `#4b8a93` spinner, 2500ms/rev |
| `waiting_input` | orange `#f59e0b` `◐` + opacity pulse (1.2s) | muted orange `#b88239` static `◐` |
| `completed` | green `#22c55e` `✓` | muted green `#4a8b6a` `✓` |
| `failed` | red `#ef4444` `✗` | muted red `#a04b4b` `✗` |
| `idle` | gray `#6b7280` `·` | gray `#6b7280` `·` |
| `disconnected` | gray dim `·` (opacity 0.5) | gray dim `·` |
| `closed` | gray dim `·` | gray dim `·` |

### Implementation notes

- **Running animation**: a single 12px SVG of a 3/4 arc with a gap,
  rotated via CSS `animation: spin <duration> linear infinite`. Same
  SVG across presets, different `--spin-duration` and `--state-color`
  CSS custom properties.
- **Waiting pulse**: CSS keyframes on opacity (0.5 ↔ 1.0, 1.2s
  ease-in-out infinite alternate), applied only when the preset's
  `animatePulse('waiting_input')` is true.
- **Unread dot**: a 6px `●` at the right end of each row; fill inherits
  `colorOf(state)` so the dot signals state + unread simultaneously, no
  second color system.
- **Type icon**: when `showTypeIcon` and the session's `type` is
  `'ai' | 'test' | 'build' | 'deploy'`, render a 12px SVG icon **reusing
  the existing P2.11 paths from `TabBar.vue`** inline between state
  icon and command text. Otherwise omit. `TabBar.vue`'s own type icon
  rendering is unchanged regardless of preset (its own surface; the
  preset's `showTypeIcon` toggle is scoped to `TaskStateIcon`, i.e.
  sidebar + dialog).
- **Color tokens**: scope CSS variables via
  `:root[data-task-preset="vivid"] { --task-running: ...; --task-waiting: ...; ... }`
  and analogous for `quiet`. `useTaskPreset` writes the dataset
  attribute on `<html>` so the rest of the UI flips instantly without
  per-component reactivity. Switching is instant (no transition
  animation — cheap, predictable).

## 4. Components

### 4.1 `TaskStateIcon.vue`

Props: `{ state: TaskState; type?: 'ai'|'test'|'build'|'deploy'; size?: number; preset?: TaskStatePreset }`.
Renders one inline-block element. Defaults `size: 12`, `preset` taken
from `useTaskPreset()`. Never looks at the session shape, never reads
stores — easy to use in mockups and tests.

### 4.2 `TaskGroupedList.vue`

Props:

```ts
{
  byHost: Record<string, RemoteSession[]>
  unreadByHost: Record<string, number>
  primaryStateForHost: (hostId: string) => TaskState
  completedSeen: RemoteSession[]                 // fold contents
  showHostHeader?: boolean                        // sidebar true; dialog true
}
```

Emits:

- `open(session)` — row click; consumer decides "switch existing tab"
  vs "open new".
- `markSeen({ all: true } | { ids: string[] })` — host header
  `✓ 全部` and per-row `✓` action.

Layout:

```
▼ <host>   ●N ◐M ✗K   [未读 <unread>]   [✓ 全部]
  <icon> <command>    <state text>     <type chip?>     <unread ●> <✓>
  ...
▶ 已完成 <N> · 点击展开
  <expanded rows…>           [全部标已读]
```

Host header is sticky within the scroll container. Completed-fold is
collapsed by default; expanding reveals rows + a per-section
`[全部标已读]` button. When everything in the fold has been marked,
the fold disappears.

### 4.3 `TaskSidebar.vue`

Left rail with two widths driven by `Settings.taskSidebarCollapsed`:

- **Expanded `~240px`**: header (`任务` + `Cmd+B` button) +
  `<TaskGroupedList :byHost :unreadByHost :primaryStateForHost
  :completedSeen showHostHeader />` + a bottom `[全部标已读]` button
  when `totalUnread > 0`.
- **Collapsed `32px`**: chevron-right expand button at top + a
  vertical stack of `TaskStateIcon`s (urgency-sorted, one per session,
  max ~20 visible then `+N` overflow indicator) + a total-unread badge
  at top. Click any icon → expand + scroll to that row.

Behavior:

- `Cmd/Ctrl+B` global hotkey toggles (when no input is focused; reuse
  existing hotkey infrastructure).
- Collapsed state persisted in `Settings.taskSidebarCollapsed`.

### 4.4 `TabBar.vue` changes

Replace the static `<span class="dot">●</span>` with
`<TaskStateIcon :state="t.activeSession?.task_state"
:type="t.activeSession?.type" />`. The existing "orange = remote" dot
semantic is preserved by passing a `remote` prop (or wrapping with a
small orange ring overlay when `t.activeRemote && !t.disconnected`).

After the title span, when `t.activeSession?.unread === true`, render a
6px `●` colored to the same state token. Layout/spacing matches the
current 4px gap between title and close button.

The existing P2.11 type icon inside each tab is preserved regardless of
preset — TabBar is a separate surface from the sidebar/dialog, and
removing its type icon is out of scope.

### 4.5 `RemoteSessionsDialog.vue` changes

- Delete the local `groupSessionsByHost`; consume
  `useSessions().remoteByHost` (a filtered view that excludes the local
  host's sessions).
- Replace inline host-header + row markup with `<TaskGroupedList
  :byHost="remoteByHost" :unreadByHost :primaryStateForHost
  :completedSeen showHostHeader />`.
- Add a top-right `[全部标已读]` button (scoped to remote sessions:
  emits `markSeen({ ids: remoteUnreadIds })`).
- The `open(session)` emit maps to the dialog's existing
  `openRemoteSession()` handler (opens as a new tab, closes the
  dialog).
- Modal chrome and existing dialog tests unchanged.

### 4.6 `SettingsDialog.vue` new section

New section `任务状态显示`:

- Radio group: `Vivid` (default) / `Quiet`. Each option shows preset
  name + a one-line description + a live preview block containing four
  `<TaskStateIcon>` of varying states rendered with that preset.
- Toggle: `默认展开任务侧栏` — boolean; mirrors
  `taskSidebarCollapsed` (inverted phrasing).

Selecting a preset writes immediately to settings and (via
`useTaskPreset`) the document's `data-task-preset` attribute updates,
so the rest of the UI reflects the change instantly.

### 4.7 `App.vue` changes

- Replace inline `localList` / `remoteList` refs with
  `const { byHost, remoteByHost, unreadByHost, primaryStateForHost,
  totalUnread, completedSeen } = useSessions()`. Raw list refs remain
  internal to `useSessions`.
- Layout: add `<TaskSidebar>` as a sibling of the main pane area,
  positioned left of `<TabBar>` + pane grid via a flex row.
- Hotkey wiring: route `Cmd/Ctrl+B` to `taskSidebarCollapsed` toggle.

## 5. Data composable: `useSessions.ts`

Input sources (already in `App.vue`):

- `localList: Ref<RemoteSession[]>` — sessions hosted on this desktop's
  local PTY layer (Wails endpoint).
- `remoteList: Ref<RemoteSession[]>` — sessions advertised by the
  relay (`/client-sessions`).

Merge rule:

- Index by `session_id`. When the same id appears in both, **the relay
  version wins** because only the relay carries `unread` and
  `attention_at`.
- A session present only in `localList` (e.g. this PTY isn't uplinked)
  keeps `unread === undefined`. Components render its `task_state` and
  `type` but suppress the unread dot and mark-read affordances.

Derived outputs (all `computed`):

```ts
{
  all: ComputedRef<RemoteSession[]>
  byHost: ComputedRef<Record<string, RemoteSession[]>>
  remoteByHost: ComputedRef<Record<string, RemoteSession[]>>  // filters out local host
  unreadByHost: ComputedRef<Record<string, number>>
  primaryStateForHost(hostId: string): TaskState
  totalUnread: ComputedRef<number>
  completedSeen: ComputedRef<RemoteSession[]>                 // drives fold
}
```

`primaryStateForHost` returns the most urgent state by:

```
waiting_input ▸ failed ▸ running ▸ completed ▸ idle ▸ disconnected ▸ closed
```

within sessions of that host. Used by host headers and (in collapsed
sidebar) by rail order.

Within a host, rows are sorted: unread-first → same urgency →
`last_output_at` desc tie-break.

## 6. Mark-read flow

`lib/api.ts` adds:

```ts
export async function markSessionsSeen(
  opts: { ids: string[] } | { all: true }
): Promise<void>
```

Implementation: POST `/api/sessions/seen`, body the opts object, via
the existing authenticated fetch wrapper (same pattern as other
mutating `/api/*` calls — cookie + `X-CSRF-Token`).

Trigger sites:

| UI | call |
|---|---|
| Row hover `✓` / context "标记已读" | `markSessionsSeen({ ids: [session.id] })` |
| Host header `✓ 全部` | `markSessionsSeen({ ids: hostUnreadIds })` |
| Sidebar bottom / Dialog top `[全部标已读]` | `markSessionsSeen({ all: true })` |
| Completed-fold per-section `[全部标已读]` | `markSessionsSeen({ ids: foldUnreadIds })` |

ATTACH already marks seen server-side (Task 6 of the backend plan), so
opening a tab triggers **no extra API call**.

Failure handling: error toast via the existing notification surface.
The next session-list push from the relay corrects any drift, so
transient failures need no retries.

## 7. Settings & persistence

`platform/types.ts` `Settings` shape gains:

```ts
taskPreset: PresetId           // default 'vivid'
taskSidebarCollapsed: boolean  // default false
```

Persistence reuses the desktop's existing settings storage
(Wails-backed config). For dev/browser preview without Wails,
`useTaskPreset` falls back to `localStorage` under the same keys.

`useTaskPreset` watches the settings store and updates
`document.documentElement.dataset.taskPreset` on change, so the CSS
variable scope flips instantly without per-component re-renders.

## 8. Testing

### Unit (vitest)

- **`taskState.ts`** — table-driven test for both presets × all
  `task_state` values, asserting `colorOf` / `glyphOf` /
  `spinnerDurationMs` / `animatePulse` / `showTypeIcon` match §3.
- **`useSessions.ts`** — deterministic inputs → snapshot of `byHost`,
  `unreadByHost`, `primaryStateForHost`, `totalUnread`, `completedSeen`.
  Cover (a) same `session_id` in both lists → relay wins, (b)
  local-only with `unread` undefined, (c) urgency priority of
  `primaryStateForHost`, (d) within-host row sort.
- **`useTaskPreset.ts`** — change settings → `dataset.taskPreset`
  updates; `localStorage` fallback when no Wails; round-trip.

### Component (vitest + `@vue/test-utils`)

- **`TaskStateIcon.vue`** — each `(state, preset)` pair renders the
  correct class + CSS variable values + animation directives.
- **`TaskGroupedList.vue`** — given fixtures, renders expected host
  groups + completed fold; row click emits `open(session)`; each `✓`
  button emits the correct `markSeen` payload.
- **`TaskSidebar.vue`** — expand/collapse toggle; `Cmd/Ctrl+B`
  keystroke flips state; `taskSidebarCollapsed` persisted;
  collapsed-rail icon list reflects urgency order.
- **`TabBar.vue`** — existing tests pass; new assertion that
  `TaskStateIcon` receives the active session's `task_state` and that
  an unread dot renders when `unread === true`.
- **`SettingsDialog.vue`** — selecting Vivid then Quiet writes the
  right value into settings; live preview block shows two distinct
  renderings.
- **`RemoteSessionsDialog.vue`** — existing tests pass; new assertion
  that the dialog renders via `TaskGroupedList` and the top
  `[全部标已读]` button calls `markSessionsSeen({ all: true })`.

### Visual sanity

Snapshot tests of `TaskStateIcon` and `TaskGroupedList` under each
preset (JSDOM, serialized HTML compared) catch accidental visual
regressions without a screenshot harness.

## 9. Migration & compatibility

Frontend-only on the desktop binary. No protocol, no storage, no
migrations. Existing settings missing `taskPreset` /
`taskSidebarCollapsed` default to `vivid` and `false` (expanded).

If the relay the desktop connects to has **not** deployed the
session-attention backend yet, sessions arrive with `unread`
undefined and `attention_at = 0`. The UI degrades gracefully:

- `task_state` icons still render (`task_state` has been on the
  protocol for a long time).
- No unread dots appear.
- The completed-fold is empty.
- Mark-read controls still render but `POST /api/sessions/seen`
  returns 404; the API call wraps in `try/catch` and surfaces a
  one-line "mark-read unavailable; please update relay" toast.

This keeps the desktop usable against an older relay.
