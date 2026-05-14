# Settings Select Dropdown Redesign

## Goal

Replace the two native `<select>` elements in the desktop Settings dialog with a custom Vue component so the closed and expanded states both match the app's dark theme. Today the trigger is themed but the popup menu falls back to the OS-native list, which looks out of place inside SettingsDialog.

In scope:
- `SettingsGeneral.vue` terminal theme picker
- `SettingsRelay.vue` remote session permissions picker

Out of scope:
- Other form controls (inputs, checkboxes) in the Settings dialog
- Any pickers outside the desktop Settings dialog

## Component: `SelectDropdown.vue`

New file at `desktop/frontend/src/components/SelectDropdown.vue`.

### Props

| Prop         | Type                                                  | Required | Notes                                              |
|--------------|-------------------------------------------------------|----------|----------------------------------------------------|
| `modelValue` | `string`                                              | yes      | Currently selected option value (v-model target).  |
| `options`    | `{ value: string; label: string; description?: string }[]` | yes      | Order is preserved in the menu.                    |
| `disabled`   | `boolean`                                             | no       | Defaults to `false`. Blocks open + interaction.    |
| `ariaLabel`  | `string`                                              | no       | Forwarded to the trigger button for screen readers.|

### Emits

- `update:modelValue(value: string)` — fired when the user picks a different option. The component does not fire when the user re-selects the current option.

### Trigger button (closed state)

- Dimensions: `height: 32px`, `padding: 6px 10px`, `border-radius: 6px`
- Colors: `background: var(--bg)`, `color: var(--fg)`, `border: 1px solid var(--border)`
- Right-aligned chevron-down SVG (12px), color `var(--fg-dim)`; rotates 180° while open
- Hover: border becomes `var(--fg-dim)`, chevron color becomes `var(--fg)`
- Focus-visible: existing `box-shadow: 0 0 0 2px var(--accent)` ring
- Shows the selected option's `label` only (not `description`) to avoid overflow
- Truncates with `text-overflow: ellipsis` if the label is wider than the trigger

### Popover menu (open state)

- Absolutely positioned directly below the trigger, same width
- `background: var(--bg)`, `border: 1px solid var(--border)`, `border-radius: 6px`, soft shadow (`0 6px 16px rgba(0,0,0,0.35)`)
- `z-index` above SettingsDialog content (use `1000`, SettingsDialog dialog itself sits below this)
- Each option:
  - Two-line layout: label (`13px`, `var(--fg)`) on top, description (`12px`, `var(--fg-dim)`) on second line
  - Options without a description render single-line
  - Padding `8px 10px`
- Highlight (hover or keyboard): `background: var(--hover)`; falls back to `rgba(255,255,255,0.05)` if `--hover` is not defined
- Selected option: left edge 2px accent bar (`var(--accent)`); label uses the default foreground color
- `max-height: 240px`, `overflow-y: auto` when the option list is taller

### Interactions

- Trigger click or pressing Enter/Space while focused opens the menu and highlights the currently selected option
- Clicking an option fires `update:modelValue` and closes the menu
- Outside-click, Esc, or focusing another element closes the menu without changing the value
- Arrow Up / Arrow Down move the highlight; wraps at the ends
- Home / End jump to the first / last option
- Enter on a highlighted option selects it and closes
- Tab while open: close the menu and let focus continue naturally
- When closed, focus returns to the trigger

### Disabled state

- Trigger `cursor: not-allowed`, `opacity: 0.6`
- Click, Enter/Space, and keyboard navigation are all no-ops
- Cannot open the menu

### Accessibility

- Trigger is a `<button type="button">` with `aria-haspopup="listbox"` and `aria-expanded`
- Menu is a `<ul role="listbox">`; options are `<li role="option">` with `aria-selected` on the current value
- The highlighted-but-not-yet-selected option is referenced via `aria-activedescendant` on the trigger while open
- `ariaLabel` prop sets `aria-label` on the trigger when provided

## Integration

### `SettingsGeneral.vue`

Replace:

```html
<select v-model="selected" :disabled="saving" @change="onChange">
  <option v-for="theme in TERMINAL_THEMES" :key="theme.id" :value="theme.id">
    {{ theme.label }} — {{ theme.description }}
  </option>
</select>
```

with:

```html
<SelectDropdown
  v-model="selected"
  :options="themeOptions"
  :disabled="saving"
  aria-label="terminal theme"
  @update:modelValue="onChange"
/>
```

Add a computed `themeOptions` that maps `TERMINAL_THEMES` into `{ value: theme.id, label: theme.label, description: theme.description }`.

Note: `@change` on a native select becomes `@update:modelValue` on the new component. `onChange` is unchanged — it already reads from `selected.value` after Vue applies the v-model.

### `SettingsRelay.vue`

Replace:

```html
<select v-model="remotePermission" :disabled="saving">
  <option value="view">view only — remote clients can watch output</option>
  <option value="control">control — allow input and resize</option>
  <option value="full">full — allow input, resize, and image paste</option>
</select>
```

with:

```html
<SelectDropdown
  v-model="remotePermission"
  :options="permissionOptions"
  :disabled="saving"
  aria-label="remote session permissions"
/>
```

Declare `permissionOptions` as a `const` (not reactive — it never changes):

```ts
const permissionOptions = [
  { value: "view",    label: "view only", description: "remote clients can watch output" },
  { value: "control", label: "control",   description: "allow input and resize" },
  { value: "full",    label: "full",      description: "allow input, resize, and image paste" },
];
```

The label/description split is no longer joined by an em-dash — the two-line layout in the menu carries the same information visually.

## Tests

### `SelectDropdown.test.ts` (new)

- Renders the selected option's label in the trigger
- Trigger click opens the menu; clicking outside closes it without emitting
- Selecting a different option emits `update:modelValue` with the new value once
- Selecting the current option closes the menu but does not emit
- Arrow Down + Enter selects the next option; wraps at the end
- Esc closes the menu and restores focus to the trigger
- `disabled` blocks the menu from opening on click or Enter

### `SettingsGeneral.test.ts` (update)

The current test interacts with `<select>.value` and `@change`. Update it to:
- Click the trigger to open the dropdown
- Click the target option
- Assert the same downstream effect (`setTerminalThemePreference` was called with the chosen id; `terminal-theme-changed` event emitted; etc.)

### `SettingsRelay.test.ts` (update)

Same shape of change — replace the native-select interaction with click-trigger + click-option.

Other Settings tests (`SettingsLogging`, `SettingsUpdates`, `SettingsDialog`) do not touch these dropdowns and stay as-is.

## Non-Goals

- Multi-select, search/filter inside the dropdown, async option loading
- Replacing native selects anywhere outside the Settings dialog
- Animations beyond a simple show/hide (no slide/fade transitions in this pass)
