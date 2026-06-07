# Desktop task sidebar UX trio (design)

Date: 2026-06-07
Status: Draft (design phase); pending implementation plan
Roadmap item: P-personal — third iteration on session task state, this
time pure desktop frontend polish after a few weeks of using v0.2.41.

## 1. Goal

Three small but compounding UX changes to the desktop task sidebar that
shipped in v0.2.41 (`docs/superpowers/specs/2026-06-06-desktop-task-state-display-design.md`):

1. **Drop the AI / test / build / deploy type icon.** The `Type` chip
   added in P2.11 and rendered in the sidebar (Vivid preset) and the tab
   bar (P2.11) duplicates information the command name already conveys
   — "claude" in the row IS the AI tag — and adds visual noise next to
   the state spinner. User feedback after living with it: 没意义.
2. **Show `cwd` in each task row.** Currently the row shows
   `state icon | command | (unread)`. Cwd is the single most useful
   disambiguator when multiple sessions run the same command (e.g. four
   `claude` sessions in different repos), and the data is already on
   `RemoteSession.cwd`. Add it inline after the command, dimmed, with a
   smart truncation.
3. **Drag-resize sidebar width.** The fixed 240px width is fine for
   short commands but cuts off long `cwd` paths and long command names
   the moment we add the cwd display. Add a thin right-edge drag handle
   so users can widen the sidebar; persist the width via the existing
   Wails settings binding pattern.

After this lands:

- Sidebar rows show `<state icon> claude · ~/code/atterm   <unread●>`.
- Tab bar tabs no longer carry the small svg type icon between the
  state dot and the title.
- The sidebar's right edge has a 4px hit zone (`cursor: ew-resize`) that
  the user can drag to set any width between 180px and 480px. Width
  persists across desktop restarts.

Out of scope:

- Removing the `Type` field from `proto.SessionInfo` or any backend code.
  `Type` is still used for session classification logic (e.g. the
  P2.11 sticky-classification rule and the silence-heuristic
  `isAttentionType` check). This spec only removes its **visual**
  surface.
- Type icon in the Settings → Task display preview block — it goes
  away naturally because `TaskStateIcon` no longer renders the type
  SVG, and the preview just embeds `TaskStateIcon`.
- Mobile UI. Mobile renders task cards differently and there is no
  separate "type icon" affordance to remove there.
- Web UI. Web list groups by host and does not surface task_state at
  all.

## 2. Architecture

```
desktop/frontend/src/
  lib/taskState.ts (modified)
    - drop showTypeIcon field from TaskStatePreset interface
    - drop the showTypeIcon argument from makePreset()
    - drop the showTypeIcon true/false from Vivid/Quiet
    (purely subtractive; presets still differ on spinner duration,
     pulse, text opacity)

  components/TaskStateIcon.vue (modified)
    - remove the <svg class="task-type"> block + its computed
    - remove the type prop (no consumer needs it after this change)
    - rename file? No — same name, smaller surface

  components/TabBar.vue (modified)
    - remove the existing <span class="type-icon"> SVG block (P2.11)
    - drop typeForTab() helper and related imports from sessionType.ts
      that become unused
    - <TaskStateIcon> already in place from v0.2.41 stays; no longer
      receives :type prop

  components/TaskGroupedList.vue (modified)
    - replace the single .cmd span with a <span class="cmd-and-cwd"> two-
      element block: command (bold/normal) + cwd (dim, ellipsis)
    - cwd helper: see §3
    - drop :type prop from <TaskStateIcon> usage

  components/TaskSidebar.vue (modified)
    - add a <div class="resize-handle"> on the right edge when not
      collapsed, with pointerdown/move/up handling
    - width is driven by a CSS var (e.g. --task-sidebar-width) instead
      of the fixed 240px class
    - drop :type prop from <TaskStateIcon> usage in the rail

  components/TabBar.test.ts (modified)
    - drop assertions on type-icon rendering

  lib/api.ts (modified)
    - new wrappers: getTaskSidebarWidth(): Promise<number>
                    setTaskSidebarWidth(px: number): Promise<void>

  composables/useTaskPreset.ts (no change — preset shape change drops
    a field but consumers don't read it via name; if anyone reads
    .showTypeIcon at runtime, that becomes a compile error and we fix
    them in the same edit)

desktop/
  app.go (modified)
    - new GetTaskSidebarWidth() int + SetTaskSidebarWidth(px int) error
      methods, mirroring the existing GetTaskSidebarCollapsed pair
    - validation: clamp to [180, 480] in SetTaskSidebarWidth before
      writing to cfgStore

  config.go (modified)
    - add TaskSidebarWidth int field to appConfig
    - default helper TaskSidebarWidthOrDefault() returning 240 when
      stored value is 0 (i.e. never set or explicitly zeroed)
```

Wails bindings regen: `wailsjs/go/main/App.d.ts` and `App.js` will need
the 2 new method stubs (hand-edit, as the project's existing pattern).

i18n: no new strings. Existing `tasks.taskTypes.*` keys stay (still used
by `sessionType.ts` and `displayForType` callers that aren't the icon).

## 3. Cwd display

Helper, in `desktop/frontend/src/lib/shortenCwd.ts` (new file):

```ts
/** Shorten a cwd for display in a tight row. Strategy:
 *   - if path begins with the user's $HOME, replace with `~`
 *   - if the result has more than 2 path segments after that,
 *     show `…/last/two` style: `~/code/atterm` stays; longer
 *     paths like `/Users/attson/code/github.com.attson/atterm` become
 *     `…/github.com.attson/atterm`
 *   - return '' for empty input so callers can v-if it away
 *
 * The full path is always available via the row's `title` attribute
 * for a hover tooltip, so truncation is non-destructive.
 */
export function shortenCwd(cwd: string | undefined, home: string): string {
  if (!cwd) return ''
  let s = cwd
  if (home && (s === home || s.startsWith(home + '/'))) {
    s = '~' + s.slice(home.length)
  }
  const parts = s.split('/').filter(Boolean)
  if (parts.length <= 2) return s.startsWith('~') ? '~/' + parts.slice(parts[0] === '~' ? 1 : 0).join('/') : '/' + parts.join('/')
  // Long path: keep only last two segments under an ellipsis.
  return '…/' + parts.slice(-2).join('/')
}
```

`home` is read once from a Wails binding `getUserHomeDir()` (likely
already exists; if not, add it next to `GetTerminalTheme`). Cached on
mount in `TaskGroupedList.vue`.

Row markup (TaskGroupedList.vue):

```vue
<button class="task-row" data-test="task-row" @click="emit('open', s)">
  <TaskStateIcon :state="(s.task_state as TaskState | undefined) ?? 'idle'" />
  <span class="cmd-and-cwd" :title="rowTitle(s)">
    <span class="cmd">{{ commandLabel(s) }}</span>
    <span v-if="shortenCwd(s.cwd, home)" class="cwd">·&nbsp;{{ shortenCwd(s.cwd, home) }}</span>
  </span>
  <span v-if="s.unread" class="unread-dot">●</span>
  <!-- mark-read button unchanged -->
</button>
```

`rowTitle(s)` returns the full cwd + command for the hover tooltip.

CSS:

```css
.cmd-and-cwd { flex: 1 1 auto; min-width: 0; display: flex; gap: 6px;
               overflow: hidden; align-items: baseline; }
.cmd { white-space: nowrap; text-overflow: ellipsis; overflow: hidden; }
.cwd { color: var(--fg-dim); white-space: nowrap; flex-shrink: 1;
       overflow: hidden; text-overflow: ellipsis; }
```

The flex layout means: when the row is wide, both command and cwd
display in full; as the row narrows, cwd shrinks first (the dim
secondary), then command truncates with ellipsis. Hover gives the full
text via title.

## 4. Drag-resize

State (`TaskSidebar.vue`):

```ts
const widthPx = ref(240)
const minWidth = 180
const maxWidth = 480
let dragOriginX = 0
let dragOriginWidth = 0
let dragging = false

onMounted(async () => {
  try {
    const stored = await getTaskSidebarWidth()
    if (stored > 0) widthPx.value = clampWidth(stored)
  } catch {
    /* default */
  }
})

function clampWidth(px: number): number {
  return Math.max(minWidth, Math.min(maxWidth, px))
}

function onDragStart(e: PointerEvent) {
  if (props.collapsed) return
  dragging = true
  dragOriginX = e.clientX
  dragOriginWidth = widthPx.value
  ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
}

function onDragMove(e: PointerEvent) {
  if (!dragging) return
  const next = clampWidth(dragOriginWidth + (e.clientX - dragOriginX))
  widthPx.value = next
}

async function onDragEnd(e: PointerEvent) {
  if (!dragging) return
  dragging = false
  ;(e.target as HTMLElement).releasePointerCapture(e.pointerId)
  try {
    await setTaskSidebarWidth(widthPx.value)
  } catch {
    /* persistence best-effort */
  }
}
```

Template additions:

```vue
<aside class="task-sidebar" :class="{ collapsed }" :style="!collapsed ? { width: widthPx + 'px' } : undefined">
  <!-- existing expanded/collapsed content unchanged -->
  <div
    v-if="!collapsed"
    class="resize-handle"
    data-test="sidebar-resize-handle"
    @pointerdown="onDragStart"
    @pointermove="onDragMove"
    @pointerup="onDragEnd"
    @pointercancel="onDragEnd"
  />
</aside>
```

CSS:

```css
.task-sidebar { position: relative; }                  /* anchor handle */
.task-sidebar:not(.collapsed) { width: 240px; }        /* fallback when binding unavailable */
.task-sidebar.collapsed { width: 32px; }
.resize-handle {
  position: absolute; top: 0; right: -2px; width: 4px; height: 100%;
  cursor: ew-resize; user-select: none; z-index: 1;
}
.resize-handle:hover { background: rgba(255,255,255,0.06); }
```

The 240px CSS fallback covers the brief window between mount and the
Wails binding returning. Once Wails resolves, the inline `:style` width
overrides the class width.

## 5. Wails persistence

Mirrors the existing `GetTaskSidebarCollapsed` / `SetTaskSidebarCollapsed`
pair from v0.2.41. Add to `desktop/app.go`:

```go
const defaultTaskSidebarWidth = 240
const minTaskSidebarWidth = 180
const maxTaskSidebarWidth = 480

func (a *App) GetTaskSidebarWidth() int {
	if a.cfgStore == nil {
		return defaultTaskSidebarWidth
	}
	w := a.cfgStore.Get().TaskSidebarWidthOrDefault()
	if w < minTaskSidebarWidth || w > maxTaskSidebarWidth {
		return defaultTaskSidebarWidth
	}
	return w
}

func (a *App) SetTaskSidebarWidth(px int) error {
	if a.cfgStore == nil {
		return nil
	}
	if px < minTaskSidebarWidth {
		px = minTaskSidebarWidth
	}
	if px > maxTaskSidebarWidth {
		px = maxTaskSidebarWidth
	}
	cfg := a.cfgStore.Get()
	cfg.TaskSidebarWidth = px
	return a.cfgStore.Set(cfg)
}
```

In `desktop/config.go`:

```go
type appConfig struct {
    // ...existing
    TaskSidebarWidth int `json:"task_sidebar_width,omitempty"`
}

func (c appConfig) TaskSidebarWidthOrDefault() int {
    if c.TaskSidebarWidth == 0 {
        return defaultTaskSidebarWidth
    }
    return c.TaskSidebarWidth
}
```

Hand-edit `wailsjs/go/main/App.d.ts` and `App.js` to add the two new
methods (project pattern documented in earlier session-attention work).

Frontend wrappers in `lib/api.ts`:

```ts
export function getTaskSidebarWidth(): Promise<number> {
  return bindings().GetTaskSidebarWidth()
}
export function setTaskSidebarWidth(px: number): Promise<void> {
  return bindings().SetTaskSidebarWidth(px)
}
```

Add the two methods to the `AppBindings` interface alongside
`GetTaskSidebarCollapsed` / `SetTaskSidebarCollapsed`.

## 6. Testing

### Unit (vitest + @vue/test-utils)

- `shortenCwd.test.ts`: cwd in HOME → `~`; longer paths truncated to
  `…/last/two`; empty input → empty string; exactly-2-segment paths
  preserved.
- `TaskStateIcon.test.ts` (modified): drop the existing test that
  asserts type icon visibility per preset (or replace with "no preset
  renders the type icon").
- `TabBar.test.ts` (modified): drop the type-icon SVG assertion;
  preserve the state-icon + unread-dot assertions added in v0.2.41.
- `TaskSidebar.test.ts` (modified): add a resize test —
  pointerdown + pointermove(+200px) + pointerup → widthPx ends within
  bounds; setTaskSidebarWidth called once with the final px value.
- `TaskGroupedList.test.ts` (modified): row markup contains `.cwd`
  span when session has a `cwd`; not rendered when blank.

### Go

- `desktop/app_test.go` (if it exists) or add: `Get/SetTaskSidebarWidth`
  round-trips through the cfgStore; clamps out-of-range inputs.

### Manual smoke

- Open desktop after upgrade → no AI star icon next to claude tab or
  sidebar row.
- A long-cwd session row truncates the cwd; full path visible on hover.
- Drag right edge → width changes; sidebar still functional at min /
  max; release saves; reopen app → width persists.

## 7. Migration & compatibility

- Pure frontend + a single new persisted int field. No protocol change.
- Existing users on v0.2.41 (which had `showTypeIcon: true` in Vivid)
  will see the icon disappear on next launch; that's the intent.
- The width field defaults to 240 (current value) so existing users see
  no width change until they drag.
- For older `appConfig` JSON files that don't have `task_sidebar_width`,
  `TaskSidebarWidthOrDefault` returns 240.
