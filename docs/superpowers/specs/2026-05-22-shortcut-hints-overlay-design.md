# Shortcut hints overlay — design

Date: 2026-05-22
Target: `desktop/frontend/` (Wails desktop app)
Status: design

## Problem

The desktop shortcut settings tab (separate spec) lets users view and rebind
the 12 terminal navigation shortcuts, but it's discovery-on-demand: the user
has to open Settings to remind themselves. We want a low-friction "peek"
gesture so a user can glance at all current shortcuts without disrupting
their flow.

This spec adds a **hold-Mod-to-peek** overlay: long-press the platform Mod
key (`Cmd` on mac, `Ctrl` elsewhere) for 3 seconds → a centered translucent
panel appears listing all shortcuts with their current bindings. Release the
key (or press any non-modifier) and it disappears.

Out of scope:

- Discovering plugin hotkeys (Quick Input, Translate). The overlay covers
  only the 12 atterm navigation actions from the registry — these are the
  bindings users actually map to mental models like "Cmd+T for new tab."
- A modifier-aware "Sketch-style" filter (showing only chords that start
  with the held modifier set). The overlay shows all 12, always.
- A "pinned" or "always visible" cheat sheet.
- Mobile / web frontend integration.

## Trigger and timing

**Show conditions** — start a 3000 ms timer on `keydown` when:
- `e.key` matches the platform modifier name (`"Meta"` on mac, `"Control"`
  elsewhere), AND
- No other modifier is already held (no Alt, Shift, or the wrong-platform
  modifier), AND
- The timer is not already running, AND
- `e.repeat === false` (OS auto-repeat does not start a new timer; the
  existing timer keeps running).

Timer fires → overlay shows.

**Cancel conditions** — clear the timer (whether the overlay is showing or
still pending) when:
- The mod key is released (`keyup` matching the platform mod), OR
- Any non-modifier key is pressed (`keydown` with a code that isn't a
  pure modifier key — `KeyA`–`Z`, `Digit*`, arrows, brackets, etc.), OR
- Another modifier joins (`keydown` for Alt or Shift while waiting; user is
  building a chord), OR
- The window loses focus (`window` `blur` event — necessary because mac
  drops `keyup` events when the app loses focus mid-press).

If the overlay is currently visible when any cancel condition fires, hide it
synchronously.

**Modifier choice**: only the platform Mod triggers the long-press. Alt and
Shift are skipped — user intent says "Ctrl/Cmd."

## UI

- Centered overlay above all other content (`z-index` above the settings
  dialog's, which is 100; pick 200).
- Backdrop: full-viewport semi-transparent black (`rgba(0,0,0,0.5)`). Click
  on backdrop does nothing — release the modifier to dismiss.
- Center panel: ~480px wide, dark background (`var(--panel)`), 1px border
  (`var(--border)`), 8px border radius, 20px padding.
- Header: small uppercase "Keyboard Shortcuts" in `var(--fg-dim)`.
- Two sections: **Pane** (9 rows) and **Tab** (3 rows), in the order from
  the registry.
- Each row: two columns via `grid-template-columns: 120px 1fr`:
  - Left: formatted chord string in monospace, right-aligned padding
  - Right: action label
- Disabled actions (binding `""`) render with the chord column showing `—`
  and the row in `var(--fg-dim)`.
- Entry/exit transition: 100ms fade only (no transform); avoids flash but
  keeps it snappy.

## Chord formatting

Add a pure function `formatChord(binding: string, mod: Mod): string` to
`lib/shortcutBindings.ts`.

Behavior:
- Empty string `""` → return `""` (caller renders `—`).
- Parse the binding with `parse()`. If `parse()` returns null or the
  parsed object has no code → return the binding string unchanged (defensive;
  shouldn't happen if the binding came from a validated source).
- On `mod === "Meta"` (mac), render with Unicode symbols:
  - `Mod` → `⌘`
  - `Alt` → `⌥`
  - `Shift` → `⇧`
  - codes: `KeyA`–`Z` → `A`–`Z`; `Digit0`–`9` → `0`–`9`; arrows →
    `←`/`→`/`↑`/`↓`; `BracketLeft` → `[`; `BracketRight` → `]`; punctuation
    → its literal character (`Minus` → `-`, `Equal` → `=`, `Backquote` →
    `` ` ``, `Comma` → `,`, `Period` → `.`, `Slash` → `/`,
    `Semicolon` → `;`, `Quote` → `'`, `Backslash` → `\`).
  - Modifiers concatenated with no separator: `⌘⌥N`.
- On `mod === "Control"` (non-mac), render with `+`-separated text:
  - `Mod` → `Ctrl`
  - `Alt` → `Alt`
  - `Shift` → `Shift`
  - codes: same per-code mapping as above.
  - Joined with `+`: `Ctrl+Alt+N`.

This pure function is exported alongside `serialize`/`parse`. Single source
of truth for chord display — the settings tab can adopt it later if we want
prettier hotkey cells.

## Composable

Add `desktop/frontend/src/composables/useLongPressModifier.ts`:

```ts
import { onScopeDispose } from "vue";
import type { Mod } from "../lib/shortcutBindings";

export interface LongPressOptions {
  mod: Mod;
  thresholdMs?: number;  // default 3000
  onShow: () => void;
  onHide: () => void;
}

export function useLongPressModifier(opts: LongPressOptions): void
```

Internal state:
- `timer: number | null`
- `showing: boolean`

Helpers:
- `modKeyName = opts.mod === "Meta" ? "Meta" : "Control"`

`keydown` (capture phase) handler:
1. If `e.key === modKeyName` AND `!e.repeat` AND no other modifier is already
   held (`!e.altKey && !e.shiftKey` and the wrong-platform mod isn't pressed
   — though that's implied by being in this branch since `e.key === modKeyName`
   means *this* mod is what just went down):
   - If `timer === null && !showing`: start `setTimeout(onShowInternal, threshold)`.
2. Else (some other key, possibly with our mod already held):
   - Clear timer.
   - If `showing`: call `opts.onHide()`; `showing = false`.

`keyup` (capture phase) handler:
- If `e.key === modKeyName`:
  - Clear timer.
  - If `showing`: call `opts.onHide()`; `showing = false`.

`window.blur` handler:
- Clear timer; if `showing` then `onHide` + `showing = false`.

`onShowInternal`:
- `timer = null`; `showing = true`; call `opts.onShow()`.

Cleanup via `onScopeDispose`:
- Clear timer.
- Remove document keydown/keyup capture listeners.
- Remove window blur listener.

The composable doesn't own UI state — it emits callbacks. `ShortcutHints.vue`
maintains its own `visible: ref(false)` synced via `onShow`/`onHide`.

Capture phase is used so the existing `useTerminalShortcuts` capture
listener (which `stopPropagation`s normal chords) doesn't block us — both
capture listeners fire independently.

## Component

`desktop/frontend/src/components/ShortcutHints.vue`:

```vue
<script setup lang="ts">
import { computed, ref } from "vue";
import { usePluginConfigStore } from "../plugins/configStore";
import {
  ACTIONS,
  formatChord,
  resolvedBindings,
  type Mod,
} from "../lib/shortcutBindings";
import { useLongPressModifier } from "../composables/useLongPressModifier";

function detectMod(): Mod {
  if (typeof navigator === "undefined") return "Control";
  return navigator.platform?.toLowerCase().includes("mac") ? "Meta" : "Control";
}

const props = defineProps<{
  // Tests inject "Control"; production uses detectMod()
  mod?: Mod;
  // Tests can shorten the threshold
  thresholdMs?: number;
}>();

const mod = props.mod ?? detectMod();
const store = usePluginConfigStore();
const visible = ref(false);

useLongPressModifier({
  mod,
  thresholdMs: props.thresholdMs ?? 3000,
  onShow: () => { visible.value = true; },
  onHide: () => { visible.value = false; },
});

const bindings = computed(() => resolvedBindings(store.cfg?.shortcuts?.bindings ?? {}));
const paneActions = ACTIONS.filter((a) => a.group === "pane");
const tabActions = ACTIONS.filter((a) => a.group === "tab");

function chordFor(actionId: string) {
  const b = bindings.value[actionId] ?? "";
  return formatChord(b, mod);
}
function isDisabled(actionId: string) {
  return (bindings.value[actionId] ?? "") === "";
}
</script>
```

Template renders backdrop + center panel + two sections via `v-if="visible"`
with a Vue `<Transition name="fade">` wrapping the backdrop. Rows are
populated from `ACTIONS` filtered by group, with `chordFor(action.id)` and
`action.label`. Disabled rows show `—` instead of the chord and have a
`.disabled` class.

## App integration

In `App.vue`:
- Import `ShortcutHints`
- Mount it once at the top level of the template, outside any conditional
  rendering: `<ShortcutHints />`

It does its own document-level listener setup; no prop wiring needed.

## Testing

**`useLongPressModifier.test.ts`** (use `vi.useFakeTimers()`):

- `keydown(Control)` then `vi.advanceTimersByTime(3000)` → `onShow` called once.
- `keydown(Control)` then `vi.advanceTimersByTime(2500)` then `keyup(Control)` →
  `onShow` NOT called; `onHide` NOT called.
- `keydown(Control)` then `vi.advanceTimersByTime(1000)` then `keydown(N)` →
  `onShow` NOT called.
- `keydown(Control)` then `vi.advanceTimersByTime(3000)` then `keydown(N)` →
  `onShow` called, then `onHide` called.
- `keydown(Control)` then `vi.advanceTimersByTime(3000)` then `keyup(Control)` →
  `onShow` called, then `onHide` called.
- `keydown(Control)` then `vi.advanceTimersByTime(3000)` then `window.blur` →
  `onShow` called, then `onHide` called.
- `keydown(Control, repeat=true)` while no existing timer → no timer started.
- `keydown(Control, repeat=true)` while existing timer running → timer keeps running.
- `keydown(Control)` then `keydown(Alt)` before 3000ms → timer canceled,
  `onShow` not called.
- `keydown(Control)` while Alt is already held → no timer started.

**`shortcutBindings.test.ts`** additions for `formatChord`:

- `formatChord("Mod+KeyN", "Meta")` → `"⌘N"`
- `formatChord("Mod+Alt+Shift+KeyN", "Meta")` → `"⌘⌥⇧N"`
- `formatChord("Mod+Shift+BracketRight", "Meta")` → `"⌘⇧]"`
- `formatChord("Mod+Alt+ArrowLeft", "Meta")` → `"⌘⌥←"`
- `formatChord("Mod+KeyN", "Control")` → `"Ctrl+N"`
- `formatChord("Mod+Alt+Shift+KeyN", "Control")` → `"Ctrl+Alt+Shift+N"`
- `formatChord("Mod+Shift+BracketRight", "Control")` → `"Ctrl+Shift+]"`
- `formatChord("", "Meta")` → `""`
- `formatChord("KeyN", "Meta")` → `"KeyN"` (defensive — parse fails)

**`ShortcutHints.test.ts`**:

- Renders nothing initially (visible is false).
- After programmatic visible toggle (via component internals or by faking
  longpress), renders 12 rows in 2 groups; default chords use mac symbols
  when `mod="Meta"` and text when `mod="Control"`.
- Disabled actions (set `store.cfg.shortcuts.bindings = { "pane.close": "" }`)
  show `—` and have `.disabled` class.

## Error handling and edge cases

- **Settings dialog open**: long-press still triggers; hints render on top
  of the dialog (z-index 200 > 100). Acceptable; users can release Mod.
- **Settings → Shortcuts capture cell active**: HotkeyCaptureCell has its
  own capture-phase keydown listener. Both ours and theirs receive the
  events; our timer starts and may fire after 3s while they wait. UX is
  awkward but not broken — release Mod hides hints, capture cell continues.
- **`e.repeat` semantics across platforms**: on mac, holding a single
  modifier doesn't typically generate repeats; on Windows/Linux some
  implementations do. Either way, repeats don't reset our state.
- **wrongMod pressed**: if mac user happens to press Control (the wrong-mod
  on mac), we don't start a timer because `e.key` won't match `"Meta"`.
- **No store**: if `store.cfg` is null (loading), `chordFor` will fall back
  to defaults via `resolvedBindings({})`. Rendering still works.

## Notes for implementation

- Reuse existing `Mod` type and `detectMod` shape from previous tasks.
  `detectMod` in `useTerminalShortcuts.ts` and `SettingsShortcuts.vue` is
  already duplicated — this is the third repeat. We'll DRY it as a follow-up
  in a small commit (not blocking this feature).
- The composable's listeners are document-level; ensure cleanup via
  `onScopeDispose` so tests with `effectScope` work the same way they do for
  `useTerminalShortcuts`.
- `<ShortcutHints />` should be the last child in `App.vue`'s template so
  its overlay reliably stacks above other absolutely-positioned children
  even before z-index resolution.
