# Desktop shortcut settings — design

Date: 2026-05-22
Target: `desktop/` (Wails desktop app)
Status: design

## Problem

Terminal navigation shortcuts (split / close pane / new tab / focus pane / switch
tab) are hard-coded in `desktop/frontend/src/composables/useTerminalShortcuts.ts`.
Users can't rebind, disable, or even discover them without reading the source.
Quick Input button hotkeys are already user-configurable, but the navigation
shortcuts that ship with the app are not.

This spec adds a new **Shortcuts** tab to `SettingsDialog.vue` that lets the
user view, rebind, disable, and reset terminal navigation shortcuts.

Out of scope (explicitly):

- Global conflict detection across plugins (Quick Input / Translate). Each
  plugin keeps its own hotkey namespace. Cross-namespace collisions are
  resolved by event consumption order at runtime — they will not surface as
  conflict warnings in this tab.
- Import / export of shortcut config files.
- Multi-binding per action (one binding per action; an action is either bound
  to exactly one chord or disabled).

## Scope

12 atomic actions are exposed (group / action / default binding):

| Group | Action ID | Default binding |
|---|---|---|
| Pane | `pane.split-vertical-new` | `Mod+KeyN` |
| Pane | `pane.split-vertical-pick` | `Mod+Alt+KeyN` |
| Pane | `pane.split-horizontal-new` | `Mod+Shift+KeyN` |
| Pane | `pane.split-horizontal-pick` | `Mod+Alt+Shift+KeyN` |
| Pane | `pane.close` | `Mod+KeyW` |
| Pane | `pane.focus-left` | `Mod+Alt+ArrowLeft` |
| Pane | `pane.focus-right` | `Mod+Alt+ArrowRight` |
| Pane | `pane.focus-up` | `Mod+Alt+ArrowUp` |
| Pane | `pane.focus-down` | `Mod+Alt+ArrowDown` |
| Tab | `tab.new` | `Mod+KeyT` |
| Tab | `tab.prev` | `Mod+Shift+BracketLeft` |
| Tab | `tab.next` | `Mod+Shift+BracketRight` |

`Mod` is a platform-agnostic token — it maps to `metaKey` on macOS and
`ctrlKey` elsewhere. This matches the existing `detectMod()` behavior in
`useTerminalShortcuts.ts` so per-platform defaults stay stable through the
new pipeline.

## Binding format

A binding is a string `"Mod+Alt+Shift+<code>"` with these rules:

- Token order is fixed: `Mod` first if present, then `Alt`, then `Shift`,
  then exactly one `<code>` token.
- `<code>` is a `KeyboardEvent.code` from a known whitelist:
  `KeyA`–`KeyZ`, `Digit0`–`Digit9`, `ArrowLeft|Right|Up|Down`,
  `BracketLeft|BracketRight`, `Minus`, `Equal`, `Backquote`, `Comma`,
  `Period`, `Slash`, `Semicolon`, `Quote`, `Backslash`.
- A binding must contain at least one modifier AND exactly one `<code>`.
  Rationale: a single bare character would intercept normal typing; pure
  modifier chords cannot fire.
- The empty string `""` is a sentinel for "disabled" — that action is not
  routed at runtime.

Using `code` (not `key`) preserves the existing layout-independent behavior:
on macOS, `⌥N` produces `e.key === "˜"` but `e.code === "KeyN"`. See
`useTerminalShortcuts.test.ts:52`.

## Persistence

Add to `desktop/plugin_config.go`:

```go
type PluginConfig struct {
    QuickInput   QuickInputConfig
    FileExplorer FileExplorerConfig
    Translate    TranslateConfig
    Shortcuts    ShortcutsConfig    `json:"shortcuts"`
}

type ShortcutsConfig struct {
    // actionID -> binding string. Only contains entries the user has changed
    // from default. An empty value means "disabled". An absent key means
    // "use default for this action".
    Bindings map[string]string `json:"bindings"`
}
```

Sparse-by-default has two consequences:

1. If a future release improves a default binding, users who never touched
   that action follow the new default automatically.
2. Removing an entry from the JSON resets that action to default.

`applyDefaults`: if `Shortcuts.Bindings == nil`, initialize to `map[string]string{}`.
Do not seed defaults — the frontend registry owns them.

`ValidatePluginConfig`: for each entry in `Bindings`, validate the value
matches the binding regex (or is empty). Reject malformed entries.

`ValidatePluginConfig` does **not** validate that `actionID` is a known
action. Unknown action IDs are silently dropped at load time on the frontend
so retired actions don't brick `SetPluginConfig` for the rest of the config.

## File layout

**New files:**

| File | Responsibility |
|---|---|
| `desktop/frontend/src/lib/shortcutBindings.ts` | Pure functions: action registry, `serialize(KeyboardEvent)`, `parse(string)`, `conflictsWith(bindings, binding, exceptActionId)`, `buildRoutingTable(bindings, mod)` |
| `desktop/frontend/src/lib/shortcutBindings.test.ts` | Tests for the above |
| `desktop/frontend/src/components/HotkeyCaptureCell.vue` | A single-cell capture component: shows current binding; when focused, listens for keydown and emits the captured binding |
| `desktop/frontend/src/components/HotkeyCaptureCell.test.ts` | Tests for capture, Esc, Backspace, modifier-only suppression |
| `desktop/frontend/src/components/SettingsShortcuts.vue` | The new tab — grouped table of actions, dirty/save/discard, reset-all, conflict display |
| `desktop/frontend/src/components/SettingsShortcuts.test.ts` | Rendering, conflict detection, reset-all, save payload shape |

**Modified files:**

| File | Change |
|---|---|
| `desktop/plugin_config.go` | Add `ShortcutsConfig` field, defaults, validation |
| `desktop/plugin_config_test.go` | Extend with `Shortcuts.Bindings` cases |
| `desktop/frontend/src/composables/useTerminalShortcuts.ts` | Read bindings from `pluginConfigStore` via a `computed` routing table; remove hard-coded `code` checks |
| `desktop/frontend/src/composables/useTerminalShortcuts.test.ts` | Add cases for injected bindings (rebind, disable) |
| `desktop/frontend/src/components/SettingsDialog.vue` | Add `'shortcuts'` to the `activeTab` union; nav button; pane mount |
| `desktop/frontend/src/components/SettingsDialog.test.ts` | Assert the new nav item exists and switches the pane |

`shortcutBindings.ts` lives in `lib/` rather than `composables/` because it
exports pure functions only — no Vue reactivity. This matches `lib/terminalThemes.ts`
and `lib/terminalCopy.ts`.

`HotkeyCaptureCell` is its own component because it owns capture state
(`capturing | idle`) that `SettingsShortcuts` should not care about. Splitting
also keeps test files focused.

## Data flow

**Load (Settings dialog opens, Shortcuts tab mounted):**

1. `pluginConfigStore.load()` — already exists; populates `store.cfg`.
2. Component initializes a local `draft: Record<string, string>` =
   `clone(store.cfg.shortcuts.bindings ?? {})`.
3. Render: for each action in the registry, the displayed binding is
   `draft[action.id] ?? action.defaultBinding`.

**Edit:**

1. User clicks a hotkey cell → `HotkeyCaptureCell` enters `capturing` state
   and adds a document-level keydown listener (capture phase) so the cell
   wins against everything else in the dialog.
2. On keydown:
   - Modifier-only press: suppress, stay in capturing state.
   - Esc: emit `cancel`, exit capturing.
   - Backspace: emit `update("")` (disable this action), exit capturing.
   - Otherwise: `serialize(e)` and emit `update(<binding>)`, exit capturing.
3. Parent updates `draft[action.id]`, recomputes `dirty` and `conflicts`.

`conflicts` is a `computed`: walk `draft` (merged with defaults for unset
actions), group action IDs by binding value, return groups with size > 1.
Empty bindings ("disabled") are excluded from conflict detection.

**Save:**

1. Build `next = clone(store.cfg)`.
2. For each `(actionId, binding)` in `draft`: if `binding === defaultBinding`,
   drop the entry (normalization keeps the config file lean and lets future
   default changes propagate). Otherwise keep it.
3. Assign `next.shortcuts.bindings = normalized`.
4. `store.save(next)` → `SetPluginConfig` (Wails) → backend persists →
   backend emits `plugin-config-changed` → store updates → routing table
   recomputes → next keystroke uses the new binding.

The Save button is disabled when `!dirty || conflicts.length > 0`.

**Apply (runtime, inside `useTerminalShortcuts`):**

```ts
const mod = opts.mod ?? detectMod();
const route = computed(() => {
  const bindings = store.cfg.value?.shortcuts.bindings ?? {};
  return buildRoutingTable(bindings, mod);
});

function handler(e: KeyboardEvent) {
  const key = serialize(e, mod);
  const action = route.value[key];
  if (!action) return;
  e.preventDefault();
  e.stopPropagation();
  dispatch(action, h, e);
}
```

`buildRoutingTable` merges the registry defaults with user overrides:

- Start with registry defaults: `{ "Mod+KeyN": "pane.split-vertical-new", ... }`
- For each `(actionId, binding)` in user overrides:
  - Remove the action's previous binding from the table (might have been
    a default).
  - If `binding !== ""`, set `table[binding] = actionId`.

A `computed` means changing settings updates the table without tearing down
the document listener.

**Reset:**

- "Reset all to defaults": `draft = {}`. All rows display defaults again;
  dirty = true; user still has to click Save.
- Per-row reset button (a small `↺` next to each cell): `draft[id] = defaultBinding`.
  Doesn't delete the entry — normalization on save handles that — so dirty
  comparison stays consistent.

## Error handling and edge cases

**Capture-time:**

- Modifier-only keypresses don't produce a binding.
- Tab, Esc, Enter in capture state get `preventDefault` + `stopPropagation`
  so focus and dialog confirm don't fire.
- A `code` outside the whitelist (e.g. `MetaLeft`, `OSLeft`): treat as no-op.
- Bindings without a modifier are rejected at the capture layer (`serialize`
  returns `null`).

**Save-time:**

- Backend rejects malformed bindings via `ValidatePluginConfig`. Error
  surfaces as a toast (existing path).

**Load-time:**

- Backend value with unknown `actionId`: drop on load in `buildRoutingTable`.
  This means a config file from a future or past version still loads.
- Backend value with a malformed binding: rejected before persistence, so
  this case only arises from manual file edits. `parse()` returns `null`
  for malformed strings; the routing table drops the entry.

**OS-reserved keys:**

We do not maintain a blocklist of OS-reserved chords (mac `⌘Q`, `⌘Space`,
`⌥⌘D`, etc.). Reasoning:

- These chords are intercepted by the OS before reaching the webview, so
  setting a binding to one of them is harmless to the app — the binding
  simply never fires.
- A blocklist is impossible to keep complete across mac versions and
  per-user system shortcuts; a partial blocklist would mislead users into
  thinking unlisted reserved keys are "safe."

If a user later asks for a warning, an `osReserved.ts` warning layer can
be added without changing storage or routing.

**Action retirement:**

If a future version removes an action, its entry in the user's
`Shortcuts.Bindings` is silently ignored. Conversely, new actions ship with
defaults via the registry and start working immediately for existing users.

## Testing

**`desktop/plugin_config_test.go` (extend):**

- `applyDefaults` initializes nil `Shortcuts.Bindings` to empty map.
- `ValidatePluginConfig` accepts: `""`, `"Mod+KeyN"`, `"Mod+Alt+Shift+ArrowLeft"`.
- `ValidatePluginConfig` rejects: `"KeyN"` (no modifier), `"Mod+"` (no code),
  `"Foo+KeyN"` (unknown token), `"Mod+KeyN+KeyM"` (two codes).

**`desktop/frontend/src/lib/shortcutBindings.test.ts` (new):**

- `serialize`: events with various modifier combos produce expected token
  order. Use `mod: "Control"` injection to make Mod resolution deterministic.
- `parse(serialize(x))` round-trips.
- `conflictsWith` returns true when two different actions share a binding,
  excludes the action being checked, ignores empty bindings.
- `buildRoutingTable`: defaults plus user overrides merge correctly;
  empty binding removes an action from the table.

**`desktop/frontend/src/composables/useTerminalShortcuts.test.ts` (extend):**

- Keep existing physical-key tests.
- Inject `bindings = { "pane.split-vertical-new": "Mod+KeyJ" }`. Assert
  `Ctrl+J` triggers `onSplitVertical("new")`; `Ctrl+N` does **not** trigger
  anything.
- Inject `bindings = { "pane.close": "" }`. Assert `Ctrl+W` does **not**
  trigger `onClosePane`.

**`desktop/frontend/src/components/HotkeyCaptureCell.test.ts` (new):**

- Click → enters capturing state (assert via emitted event or DOM class).
- `Ctrl+Shift+KeyT` keydown → emits `update("Mod+Shift+KeyT")` (with `mod: Control`).
- Esc → emits `cancel`, exits capturing.
- Backspace → emits `update("")`, exits capturing.
- Modifier-only (`Ctrl` alone) → no emit.
- Bare letter (no modifier) → no emit.

**`desktop/frontend/src/components/SettingsShortcuts.test.ts` (new):**

- Renders 12 rows in two groups; each row shows its current binding.
- Editing one cell to collide with another → row shows "Conflicts with: …";
  Save button is disabled.
- Reset all → all rows back to default display; dirty is true; Save enabled.
- Save → `store.save` called with a `next` whose `shortcuts.bindings` contains
  only entries that differ from defaults.
- Discard → draft snaps back to `store.cfg.shortcuts.bindings`; dirty false.

**`desktop/frontend/src/components/SettingsDialog.test.ts` (extend):**

- New nav item `Shortcuts` exists.
- Clicking it shows `<SettingsShortcuts>` and hides the others.

## Notes for implementation

- `useTerminalShortcuts.ts` currently consumes `pluginConfigStore` indirectly
  (through its caller in `App.vue`). The new version imports the store
  directly inside the composable. Tests need to either mount with a Pinia
  test instance or accept an optional `bindings` argument for injection.
  The latter is simpler and keeps the composable testable without Pinia
  setup overhead — recommended.
- The `dispatch(action, handlers, event)` helper translates an actionId
  back into the right handler call, including `SplitMode` derivation from
  current modifiers. Today this is implicit in the `KeyN` branch; the new
  shape makes it explicit.
- Style: follow `QuickInputSettings.vue` and existing `Settings*.vue`
  files for spacing, button styling, dirty/save pattern.
