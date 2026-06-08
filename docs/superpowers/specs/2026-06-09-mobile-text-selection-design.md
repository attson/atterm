# Mobile Text Selection — Long-Press to Copy or Send

## Goal

Add long-press text selection on the mobile terminal so the user can:

1. Long-press a word in the xterm grid → that word becomes selected.
2. **Without lifting**, continue dragging the finger to extend the selection
   across cells / lines. Once the finger lifts, the selection is fixed —
   re-grabbing an endpoint to reshape it is out of scope (iOS-style two-handle
   draggable selection would need a visible-handle component and is deferred).
3. Tap a floating popover above the selection to **copy** the text to the
   clipboard or **send** it to the PTY (with desktop-equivalent `\r` semantics).

The feature is strictly gated by the existing protect / control model:
selection only activates when `canSend === true` (driver + control mode on +
not view-only). Viewers and the protect-mode-off state stay non-interactive,
matching the current "no input on the terminal unless control is engaged"
contract.

## Non-goals

- No double-tap selection (long-press is the iOS conventional gesture; adding
  another mode adds learning cost without value).
- No "select all" — terminal scrollback is large; a global select has no
  realistic mobile use case.
- No magnifying-loupe overlay (iOS 14+ dropped this interaction).
- No system share sheet integration ("translate", "look up", "search Web") —
  out of scope; copy + send is the brief.
- No cut / replace — terminal grid is read-only.
- No new global toast component — a local toast inside `MobileTerminal.vue`
  is enough until a second surface needs it.
- No change to the desktop terminal selection (it already has right-click
  copy/send).

## Architecture

```
desktop/frontend/src/
├── lib/
│   ├── wordBoundary.ts             ← new; pure word-boundary helper
│   ├── wordBoundary.test.ts        ← new
│   ├── terminalCellCoords.ts       ← new; (clientX,clientY) → (col,row)
│   └── terminalCellCoords.test.ts  ← new
└── mobile/
    ├── MobileTerminal.vue          ← wire long-press + popover; ~80 LOC added
    ├── MobileSelectionPopover.vue  ← new; floating iOS-style popover
    └── __tests__/
        ├── MobileSelectionPopover.test.ts        ← new
        └── MobileTerminal.test.ts                ← extend (current file)
```

### Approach: reuse xterm.js selection

xterm.js v5 exposes the public API:

- `term.select(col, row, length)` — programmatic selection
- `term.selectLines(start, end)` — line-range selection
- `term.getSelection()` / `hasSelection()` / `clearSelection()`
- `term.onSelectionChange` — event

The selection **highlight rendering** is done by xterm's WebGL renderer; we
only own the **gesture** (long-press, drag) and the **word-boundary math**.

A fully-custom selection overlay (option B in brainstorming) was rejected:
2-3× code for no user-visible gain. A native-browser Range-based approach
(option C) is impossible — xterm renders to `<canvas>`, browser Selection
cannot target canvas pixels.

### Component responsibilities

| Unit | Owns | Depends on |
|---|---|---|
| `lib/wordBoundary.ts` | `(line: string, col: number) → {start, len}` per Unicode word rules | nothing |
| `lib/terminalCellCoords.ts` | `(clientX, clientY, term, viewport) → {col,row} \| null` | xterm internal dimensions (with fallback) |
| `MobileSelectionPopover.vue` | Floating popover render + button click events | nothing |
| `MobileTerminal.vue` | Gesture state machine, popover positioning, copy/send dispatch, toast | xterm, `wordBoundary`, `terminalCellCoords`, `lib/terminalCopy.copyTerminalSelection`, `lib/terminalContextMenu.prepareSendPayload`, Capacitor Haptics (best-effort), `useI18n` |

## Gesture / selection state machine

```
states: idle → pressing → selecting → dragging → selecting → idle
                                              ↘___________↗
                                                   tap up

idle
  ├─ pointerdown (canSend === true)
  │    → start 500 ms pressTimer
  │    → record press anchor (clientX, clientY, col, row)
  │    → state = pressing
  └─ pointerdown (canSend === false) → existing nudgeProtect(); stay idle

pressing
  ├─ pointermove > 8 px → cancel pressTimer; state = idle
  ├─ pointerup before timer → cancel; state = idle (treated as plain tap)
  └─ pressTimer fires →
       cellCoordsAt(anchor.x, anchor.y) → (col, row)
       lineText = term.buffer.active.getLine(row).translateToString()
       wb = wordBoundaryAt(lineText, col)
       if wb.len === 0 → state = idle (long-pressed whitespace)
       else:
         term.select(wb.start, row, wb.len)
         Haptics.impact({ style: Light }).catch(() => {})
         term.options.disableStdin = true
         viewport.style.touchAction = 'none'  // suppress pan-y during drag
         popover.visible = true; updatePopoverFromSelection()
         state = selecting

selecting
  ├─ pointermove (dx² + dy² > 16) → state = dragging
  ├─ pointerup → stay (drag never started; selection stays)
  └─ tap on popover button OR pointerdown outside selection → handled below

dragging
  ├─ pointermove → cellCoordsAt → recompute selection range
  │     · single-row range → term.select(start, row, len)
  │     · multi-row range  → term.selectLines(rowA, rowB)
  │     · clientY in top    24 px of viewport → scrollLines(-3) every 60 ms
  │     · clientY in bottom 24 px of viewport → scrollLines(+3) every 60 ms
  └─ pointerup → stopEdgeScroll(); state = selecting

selecting / dragging  →  on any of {COPY, SEND, CANCEL, outside-tap, scroll}
  → exitSelection()

function exitSelection() {
  term.clearSelection()
  term.options.disableStdin = !canSend.value
  viewport.style.touchAction = 'pan-y'  // restore
  popover.visible = false
  stopEdgeScroll()
  state = idle
}
```

### Key thresholds

| Knob | Value | Why |
|---|---|---|
| Long-press duration | 500 ms | iOS system long-press parity |
| Pre-press jitter cancel | 8 px | matches iOS default |
| Post-press drag start | 4 px | tighter once committed to selection |
| Edge-scroll zone | 24 px from top/bottom of viewport | finger comfortably reachable |
| Edge-scroll rate | 3 lines / 60 ms | smooth without overshoot |

### Popover

```
+---------------------+
| 复制 │ 发送 │ ×    |   ← 11 px text, 4×11 padding, ~21 px tall
+---------------------+
         ▾              ← arrow points down at selection center
```

Visual: dark `#2b2c30`, 8 px radius, 1 px `#3f4046` button separators, "发送"
in `#60a5fa` to signal the primary action. Box-shadow `0 6px 20px rgba(0,0,0,.4)`.

Touch hit-slop: each button's visual is 11 px text + 4×11 padding (≈21 px tall),
but the `<button>` gets transparent margin on top/bottom to bring the actual
tap region to ≈36 px tall. The popover container's pointer-event hit area
matches its visual bounds.

Positioning (calculated in `MobileTerminal.vue`):

- `selectionBox = term.getSelectionPosition()` (xterm API) → `{startCol, startRow, endCol, endRow}`
- Convert to pixel rect via `terminalCellCoords` (reverse direction helper):
  `bbox = { x: min, y: min, w, h }` in viewport coords
- Popover center-X = `bbox.x + bbox.w / 2`
- Default: popover anchored **above** selection — its bottom edge sits at
  `bbox.y - 6 px` (the 6 px gap is for the arrow).
- Flip: if the popover would not fit above (`bbox.y - 6 - popoverHeight < 8`),
  anchor **below** instead — its top edge sits at `bbox.y + bbox.h + 6 px`
  and the arrow flips to point up.
- Clamp horizontal: `popover.left = clamp(centerX - popoverWidth/2, 8, viewportWidth - popoverWidth - 8)`.

The component receives the resolved `y` as a single value plus `arrowDir`
(`'down'` for anchored-above, `'up'` for anchored-below); CSS interprets `y`
as either `bottom` (down arrow) or `top` (up arrow).

Component contract:

```ts
defineProps<{
  visible: boolean
  x: number             // viewport coord, px — popover horizontal center
  y: number             // viewport coord, px — interpreted as `bottom` when
                        // arrowDir='down', as `top` when arrowDir='up'
  arrowDir: 'down' | 'up'
  copying: boolean
  sending: boolean
}>()
defineEmits<{
  (e: 'copy'): void
  (e: 'send'): void
  (e: 'cancel'): void
}>()
```

### Wiring inside `MobileTerminal.vue`

New state:

```ts
type SelMode = 'idle' | 'pressing' | 'selecting' | 'dragging'
const selMode = ref<SelMode>('idle')
const popover = reactive({
  visible: false, x: 0, y: 0, arrowDir: 'down' as 'down' | 'up',
  copying: false, sending: false,
})
const toastText = ref<string | null>(null)
let pressTimer: ReturnType<typeof setTimeout> | null = null
let pressAnchor: { x: number; y: number } | null = null
let dragAnchor: { col: number; row: number } | null = null
let edgeScrollTimer: ReturnType<typeof setInterval> | null = null
```

Existing handlers stay; selection logic mounts as **separate** listeners on
`.xterm-viewport` (added in `onMounted` after `term.open()`). The existing
`onTermPointerDown` keeps owning keyboard-collapse + protect-banner shake;
selection handlers run in addition.

Functions added:

```ts
function onSelPointerDown(ev: PointerEvent)
function onSelPointerMove(ev: PointerEvent)
function onSelPointerUp(ev: PointerEvent)
function onSelPointerCancel(ev: PointerEvent)
function onCopy()
function onSend()
function onCancel()                      // popover ×
function onDocumentClick(ev: MouseEvent) // outside-click → exitSelection
function exitSelection()
function updatePopoverFromSelection()
function ensureEdgeScroll(dir: -1 | 1)
function stopEdgeScroll()
function showToast(msg: string)
```

`onSelectionChange` (registered on `term.onSelectionChange`) calls
`updatePopoverFromSelection()` while `selMode !== 'idle'` so the popover
follows live during a drag.

`watch(canSend)` already exists; extend it to call `exitSelection()` when
`canSend` flips false mid-selection (control mode disengaged during selecting).

### Data flow — "user long-presses 'npm test'"

```
1. PTY shows "$ npm test" on row 12, cols 2-9
2. User taps & holds cell (col=4, row=12) for 500 ms
3. pressTimer fires:
     cellCoordsAt(touchX, touchY) → {col:4, row:12}
     getLine(12).translateToString() → "$ npm test"
     wordBoundaryAt("$ npm test", 4) → {start:2, len:3}    (the "npm" word)
     term.select(2, 12, 3)                                  ← xterm draws blue
     Haptics.impact()
     term.options.disableStdin = true
     viewport.touchAction = 'none'
     popover.visible = true; updatePopoverFromSelection()
4. User drags to col=9 (end of "test"):
     term.select(2, 12, 8)                                  ← now selects "npm test"
5. User taps "Send" on popover:
     getSelection() → "npm test"
     prepareSendPayload("npm test") → "npm test\r"
     sendRaw("npm test\r") → conn.sendInput → PTY executes
     exitSelection()
```

Same flow for "Copy" — replaces `sendRaw` with
`copyTerminalSelection(term)` + `showToast(t("mobile.copied"))`.

## Error handling

- `copyTerminalSelection` rejects (clipboard permission denied / fallback
  unavailable) → catch → `showToast(t("terminal.copyFailed"))`. Existing i18n
  key reused.
- `wordBoundaryAt` returns `len === 0` (long-press on whitespace) → silent
  drop; state returns to `idle`. No popover.
- `cellCoordsAt` returns `null` (touch outside viewport) → silent drop.
- Haptics plugin unavailable (web preview / test env) → `.catch(() => {})`.
- `term.select` / `selectLines` throws on bad coords → catch; `exitSelection()`.
- `term.getSelectionPosition()` returns undefined (no selection) →
  `updatePopoverFromSelection` no-op; popover stays at last position until
  next event.

## Testing

### `lib/wordBoundary.test.ts`

| Case | Input | Expected |
|---|---|---|
| middle of alnum word | `"git status -v"`, col=2 | `{0, 3}` |
| start of next word | `"git status -v"`, col=4 | `{4, 6}` |
| punctuation run | `"--foo"`, col=1 | `{0, 2}` |
| whitespace | `"hi  there"`, col=2 | `{2, 0}` |
| line end | `"abc"`, col=2 | `{0, 3}` |
| col out of range | `"abc"`, col=10 | `{3, 0}` |
| empty line | `""`, col=0 | `{0, 0}` |
| CJK character | `"读 hello"`, col=0 | `{0, 1}` |
| word with `_` and digits | `"foo_bar123 baz"`, col=5 | `{0, 10}` |

### `lib/terminalCellCoords.test.ts`

| Case | Setup | Expected |
|---|---|---|
| cell (0,0) | rect 0,0,800,600; cellW=8, cellH=16; clientX=4, clientY=8 | `{col:0, row:0}` |
| cell (3,2) | clientX=28, clientY=40 | `{col:3, row:2}` |
| scrollTop offset | scrollTop=160; clientY=8 | row += 10 |
| out of right | clientX=900 | `null` |
| out of bottom | clientY=700 | `null` |
| negative coords | clientX=-5 | `null` |

Uses a stubbed `term` exposing minimal `cols`, `rows`, and a `getCellSize(term)`
adapter (the helper itself encapsulates the `_core._renderService.dimensions`
read with a `fontSize × lineHeight` fallback).

### `MobileSelectionPopover.test.ts`

| Case | Assertion |
|---|---|
| `visible=false` | `.popover` not in DOM |
| renders 3 buttons | copy / send / × present |
| each click emits | `copy` / `send` / `cancel` |
| `copying=true` | copy button `disabled` |
| `sending=true` | send button `disabled` |
| `x` / `y` props | translate to `left` / `bottom` inline styles |
| `arrowDir=up` | arrow element gets the up-variant class |

### `MobileTerminal.test.ts` (extend)

Existing test infra mocks xterm; reuse and add:

| Case | Assertion |
|---|---|
| `canSend=false` + long-press | no popover; `term.select` never called |
| `canSend=true` + long-press 500 ms | `term.select(start, row, len)` called; popover visible |
| post-press drag > 4 px | `term.select` re-called with new range |
| post-press multi-row drag | `term.selectLines(...)` called |
| tap COPY | `clipboard.writeText` called; `term.clearSelection`; popover hidden; toast shown |
| tap SEND | `conn.sendInput` called with `payload + '\r'`; popover hidden |
| tap CANCEL | popover hidden; `term.clearSelection`; no copy/send |
| outside-click | popover hidden; `term.clearSelection` |
| copy throws | toast shows `terminal.copyFailed` |
| `canSend` flips false mid-selection | `exitSelection()` fires |
| scroll on `.xterm-viewport` while selecting | `exitSelection()` fires |

### Manual verification (iOS device / simulator)

- [ ] driver + control on → long-press a word → blue highlight + popover appears above
- [ ] drag endpoint to next-line word → selection extends multi-row
- [ ] drag into top/bottom 24 px → scrollback auto-scrolls
- [ ] tap outside → selection clears
- [ ] tap Copy → switch to Notes → paste → text intact
- [ ] tap Send `ls -la` → terminal runs the command
- [ ] view-only session → long-press → nothing happens (no popover)
- [ ] driver + control off → long-press → existing protect-banner shake fires; no popover
- [ ] non-selection touch → `.xterm-viewport` scroll still works
- [ ] long-press while keyboard is up → keyboard collapses, popover then appears

## Edge cases & guards

- iOS system long-press menu (`-webkit-touch-callout`) must be suppressed on
  the viewport — add `user-select: none; -webkit-touch-callout: none;` to
  `.xterm-viewport` style scope. Without this, both the system menu and our
  popover would race.
- `touch-action: pan-y` (existing) must be flipped to `none` during selection
  and restored on `exitSelection`. Otherwise drag fights with native scroll.
- `disableStdin = true` during selection prevents the on-screen keyboard
  capturing characters mid-drag. Restored to `!canSend.value` on exit.
- Driver loss mid-selection (relay reassigns driver to another client) →
  `canSend` flips false → `watch(canSend)` calls `exitSelection()`. The user's
  selection silently clears; this matches the desktop driver-handoff behaviour.
- WebGL renderer dispose (`onBeforeUnmount`): pending `pressTimer` and
  `edgeScrollTimer` must be cleared, listeners removed, document click listener
  removed.

## Open risks

1. **xterm internal touch behaviour**: xterm 5.x ships mouse-based selection.
   On touch devices, `mousedown` is synthesized after a tap, which could
   trigger xterm's own selection start before our pressTimer fires. Mitigation
   plan: the implementation's first task is a real-device spike — open xterm,
   long-press, see whether xterm's own selection appears. If so, register our
   pointer listeners on `.xterm-viewport` with `{ capture: true }` and call
   `stopPropagation()` once `selMode !== 'idle'`. If that's insufficient,
   override `term.attachCustomKeyEventHandler` style — defer to spike.
2. **`_core._renderService.dimensions`** is xterm internal API. We guard with
   a `fontSize × lineHeight` fallback (1.0 line-height, fontWidthScale 0.6)
   that yields a degraded but functional result. xterm 6.x upgrade requires
   a regression pass on selection.
3. **`getSelectionPosition()`** signature changed between xterm versions; the
   wrapper reads coords defensively (`undefined → no-op`) so the popover stays
   stationary rather than crashing if the API moves.
