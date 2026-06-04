# AI quick-action templates (design)

Date: 2026-06-04
Status: Draft (design phase); pending implementation plan
Roadmap item: P2.13

## 1. Goal

Turn the mobile-only hardcoded "y / n / yes / no / continue" quick-text bar
into a cross-platform user-editable list of named **quick templates**.
Each template is one click → preview modal → send to PTY through the
existing `sendInput` path. Defaults seed the list with the canonical
AI-CLI tokens (`approve`, `deny`, `continue`, `retry`, `/test`,
`/diff`, plus the existing `y / n / yes / no` ASCII shortcuts), and
the desktop Settings dialog gains a Templates tab for editing.

After this lands:

- A single `QuickTemplate` data model lives across desktop config.json,
  mobile localStorage, and web localStorage. Cross-machine sync is
  explicitly out of scope — each end keeps its own list.
- Desktop's `TerminalView` and mobile's `MobileTerminal` both render
  a horizontal template bar that's `disabled` under the existing
  `canSend` (driver + controlMode + non-view-only) gate.
- Tapping a template opens a small "Send preview" modal showing the
  exact text; Enter sends, Esc cancels.
- Desktop `Settings → Templates` lets the user add / edit / reorder /
  delete templates and reset to defaults. Mobile and web v0.5 ship
  defaults only; their editor lands later if anyone needs it.

Out of scope:

- Cross-device sync of templates (each end persists independently).
- Mobile / web template editor UI (defaults only; edit on desktop).
- Keyboard shortcuts to invoke templates (future).
- Import / export of template lists.
- Send-without-preview toggle per template (always previews to keep
  the contract simple).

## 2. Architecture

```
┌── shared data model (TS) ─────────────────────────────────────────────┐
│  QuickTemplate { id, label, text }                                    │
│  DEFAULT_TEMPLATES — 10-entry seed list                               │
└───────────────────────────────────────────────────────────────────────┘
                                  │
        ┌─────────────────────────┼─────────────────────────┐
        ▼                         ▼                         ▼
┌── desktop (Wails) ───┐  ┌── mobile (Capacitor) ─┐  ┌── web (PWA) ───────┐
│  Persistence:        │  │  Persistence:         │  │  Persistence:      │
│   Go config.json     │  │   localStorage        │  │   localStorage     │
│   appConfig          │  │   key 'atterm.        │  │   key 'atterm.     │
│   .QuickTemplates    │  │   templates'          │  │   templates'       │
│                      │  │                       │  │                    │
│  Wails bindings:     │  │  TemplateBridge       │  │  TemplateBridge    │
│   GetQuickTemplates  │  │  (capacitor.ts)       │  │  (web local impl)  │
│   SetQuickTemplates  │  │                       │  │                    │
│                      │  │                       │  │                    │
│  Renderer:           │  │  Renderer:            │  │  Renderer:         │
│   TerminalView.vue   │  │  MobileTerminal.vue   │  │  TerminalView.vue  │
│   adds .template-bar │  │  replaces existing    │  │  adds              │
│   above status-bar   │  │  .quickbar with the   │  │  .template-bar     │
│                      │  │  template bar         │  │  (defaults only)   │
│                      │  │                       │  │                    │
│  Editor:             │  │  Editor:              │  │  Editor:           │
│   Settings →         │  │   none (v0.5)         │  │   none (v0.5)      │
│   Templates tab      │  │                       │  │                    │
│   (SettingsTemplates │  │                       │  │                    │
│    .vue)             │  │                       │  │                    │
└──────────────────────┘  └───────────────────────┘  └────────────────────┘
```

The shape `QuickTemplate { id, label, text }` is the single source of
truth. Three persistence layers, three renderers, but one TS interface
and one JSON shape on disk.

## 3. Data model

### 3.1 Type definition

`desktop/frontend/src/lib/templates.ts` (new):

```ts
export interface QuickTemplate {
  id: string     // crypto.randomUUID() at first creation; stable thereafter
  label: string  // button display, ≤ 24 chars recommended (validated soft)
  text: string   // sent verbatim; renderer appends a single '\r'
}

// DEFAULT_TEMPLATES is the seed used when persistence returns an empty
// list. Order is preserved to define the default button arrangement.
// IDs are deterministic strings (not UUIDs) so re-seeding after a reset
// keeps the same set without churn.
export const DEFAULT_TEMPLATES: QuickTemplate[] = [
  { id: 'default-y',        label: 'y',         text: 'y' },
  { id: 'default-n',        label: 'n',         text: 'n' },
  { id: 'default-yes',      label: 'yes',       text: 'yes' },
  { id: 'default-no',       label: 'no',        text: 'no' },
  { id: 'default-continue', label: 'continue',  text: 'continue' },
  { id: 'default-approve',  label: 'approve',   text: 'approve' },
  { id: 'default-deny',     label: 'deny',      text: 'deny' },
  { id: 'default-retry',    label: 'retry',     text: 'retry' },
  { id: 'default-test',     label: '/test',     text: '/test' },
  { id: 'default-diff',     label: '/diff',     text: '/diff' },
]
```

Mirrored Go struct in `internal/proto/quicktemplate.go` (new — kept
in `internal/proto` rather than `desktop` so the same names live with
other shared shapes):

```go
type QuickTemplate struct {
    ID    string `json:"id"`
    Label string `json:"label"`
    Text  string `json:"text"`
}
```

The Go side never sends templates anywhere — they live entirely in
`appConfig`. The struct is in `internal/proto` only for shape parity;
if that feels overweight, putting it on `desktop/quick_templates.go`
is equally fine. Defer to the implementer's call when wiring.

### 3.2 Web mirrors

`web/src/shared/templates.ts` — copy of `desktop/frontend/src/lib/
templates.ts` (web project doesn't import from `desktop/frontend/`).

## 4. Platform bridge

`desktop/frontend/src/platform/types.ts` gains:

```ts
export interface TemplateBridge {
  load(): Promise<QuickTemplate[]>
  save(list: QuickTemplate[]): Promise<void>
  clear(): Promise<void>
}

export interface Platform {
  // ...existing bridges...
  templates: TemplateBridge
}
```

### 4.1 Wails impl (desktop)

`wails.ts` implements:

```ts
templates: {
  load: async () => bindings().GetQuickTemplates(),
  save: async (list) => bindings().SetQuickTemplates(list),
  clear: async () => bindings().SetQuickTemplates([]),
},
```

Go side (`desktop/app.go`):

```go
// GetQuickTemplates returns the user's persisted list. Returns an
// empty slice if none; the renderer seeds defaults in that case.
func (a *App) GetQuickTemplates() []proto.QuickTemplate

// SetQuickTemplates persists list verbatim. Passing an empty slice
// resets storage (next load returns empty, renderer seeds defaults).
func (a *App) SetQuickTemplates(list []proto.QuickTemplate) error
```

Stored as `appConfig.QuickTemplates []proto.QuickTemplate` with the
JSON tag `"quick_templates,omitempty"`.

### 4.2 Capacitor impl (mobile)

`capacitor.ts`:

```ts
const TEMPLATES_KEY = 'atterm.templates'

templates: {
  load: async () => {
    if (typeof localStorage === 'undefined') return []
    const raw = localStorage.getItem(TEMPLATES_KEY)
    if (!raw) return []
    try { return JSON.parse(raw) as QuickTemplate[] } catch { return [] }
  },
  save: async (list) => {
    if (typeof localStorage === 'undefined') return
    localStorage.setItem(TEMPLATES_KEY, JSON.stringify(list))
  },
  clear: async () => {
    if (typeof localStorage === 'undefined') return
    localStorage.removeItem(TEMPLATES_KEY)
  },
},
```

### 4.3 Web impl

Mirrors capacitor exactly — same localStorage key, same code.

### 4.4 Seed-on-empty helper

`desktop/frontend/src/lib/templates.ts`:

```ts
// effectiveTemplates returns persisted list if non-empty, otherwise
// the defaults. Callers should never see an empty list from this
// helper — defaults are guaranteed.
export async function effectiveTemplates(
  bridge: { load: () => Promise<QuickTemplate[]> }
): Promise<QuickTemplate[]> {
  const stored = await bridge.load()
  return stored.length > 0 ? stored : DEFAULT_TEMPLATES
}
```

The renderer's `onMounted` calls `effectiveTemplates(platform.templates)`
on every terminal mount. We don't auto-save defaults on first load —
storage stays empty until the user adds / edits, keeping the "reset
to defaults" behaviour simple (just clear storage).

## 5. UI

### 5.1 Template bar

A horizontal scrolling row of buttons. Same component on desktop /
mobile / web, differing only in font-size + spacing.

```vue
<div class="template-bar" data-testid="template-bar">
  <button
    v-for="t in templates"
    :key="t.id"
    class="template-btn"
    :data-testid="`template-btn-${t.id}`"
    :disabled="!canSend"
    @click="onTemplateClick(t)"
  >{{ t.label }}</button>
</div>
```

CSS (small adaptations per renderer, mobile shown):

```css
.template-bar {
  display: flex; align-items: center; gap: 6px;
  overflow-x: auto; padding: 4px 8px;
}
.template-btn {
  flex: 0 0 auto;
  height: 28px; min-width: 34px; padding: 0 9px;
  border-radius: 7px; background: #11182b;
  border: 1px solid #1e2638; color: #cbd5e1;
  font-size: 0.75rem; font-family: var(--font-mono);
}
.template-btn:disabled { opacity: .45; color: #64748b; }
```

`onTemplateClick(t)` → shows the preview dialog with `t`. Confirm
calls `conn.sendInput(t.text + '\r')` (matching the existing
`sendQuick` behavior).

### 5.2 Preview dialog component

`TemplatePreviewDialog.vue` (new, shared between desktop and mobile):

```vue
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '../i18n/useI18n'

const props = defineProps<{ template: QuickTemplate | null }>()
const emit = defineEmits<{
  (e: 'confirm', t: QuickTemplate): void
  (e: 'cancel'): void
}>()
const { t } = useI18n()
const open = computed(() => props.template !== null)

function onKeydown(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'Enter') { e.preventDefault(); if (props.template) emit('confirm', props.template) }
  else if (e.key === 'Escape') { e.preventDefault(); emit('cancel') }
}
</script>

<template>
  <div v-if="open" class="dialog-backdrop" @click="emit('cancel')" @keydown="onKeydown" tabindex="0">
    <div class="dialog" data-testid="template-preview" @click.stop>
      <h3>{{ t('settings.templates.preview.title') }}</h3>
      <pre class="preview">{{ template?.text }}</pre>
      <div class="actions">
        <button data-testid="template-preview-cancel" @click="emit('cancel')">
          {{ t('common.cancel') }}
        </button>
        <button data-testid="template-preview-confirm" autofocus
                @click="if (template) emit('confirm', template)">
          {{ t('settings.templates.preview.send') }}
        </button>
      </div>
    </div>
  </div>
</template>
```

Styling: dark backdrop, centered card, ~280px wide, monospace text
preview. Mobile sheet-style variant uses bottom-anchored layout —
defer to the implementer's call; the contract is the testids.

### 5.3 Settings → Templates tab (desktop only)

`SettingsTemplates.vue` (new). Standard Settings layout:

```
┌─ Templates ─────────────────────────────────────────┐
│                                                      │
│  Each template sends its text to the active session  │
│  on click. Disabled when you're a viewer or control  │
│  mode is off.                                        │
│                                                      │
│  ┌──────────────────────────────────────────────┐    │
│  │ y           │ y                  │ ↑ ↓ ✏ 🗑 │    │
│  │ n           │ n                  │ ↑ ↓ ✏ 🗑 │    │
│  │ ...                                           │    │
│  └──────────────────────────────────────────────┘    │
│                                                      │
│  [ + Add template ]      [ Reset to defaults ]       │
│                                                      │
└──────────────────────────────────────────────────────┘
```

Edit / Add are inline (replace the row with two text inputs +
Save/Cancel buttons). Reset opens a confirm dialog. List operations
debounce-save 300ms to platform.templates.save (defensive against
rapid edits).

Wired into `SettingsDialog.vue` as the 8th tab (after Diagnostics);
all the boilerplate matches what P1.10 added.

### 5.4 Where the template bar mounts

- **Desktop TerminalView.vue**: insert `<TemplateBar>` just above
  `<div class="status-bar">`. Layout is column flex; bar is a 1-row
  child with `flex: 0 0 auto`.
- **Mobile MobileTerminal.vue**: REPLACE the existing
  `<div class="quickbar">` block with `<TemplateBar>`. The `QUICK_TEXTS`
  constant and `sendQuick` are deleted. The AUX_KEYS row (enter / esc
  / ctrl-c / arrows / paste / image) stays as-is.
- **Web TerminalView.vue**: same as desktop — bar above status row.

The TemplateBar component itself is small enough that each renderer
inlines it (shared CSS via `var(--font-mono)`); a separate
`TemplateBar.vue` component is fine if the implementer prefers
DRYer code.

## 6. Send path

```
button click
  → templatePending.value = template
  → <TemplatePreviewDialog :template="templatePending">
  → user hits Enter / clicks Send → emit('confirm', t)
  → templatePending.value = null
  → conn.sendInput(t.text + '\r')
```

`canSend` gate is checked at button-disabled level (no need to re-check
on confirm — the dialog can only be opened from a non-disabled button).

`sendInput` is the existing method in `lib/connection.ts`:

```ts
this.ws.send(encodeFrame(TYPE.IN, this.sidBytes, encodeText(s)))
```

Same as paste-text and keyboard input. No new protocol, no new
permission check.

## 7. Errors and observability

- `platform.templates.load()` returning a malformed JSON → caught,
  returns `[]`, defaults are used. No user-visible toast (the
  defaults are usable).
- `platform.templates.save()` failing on disk / quota → bubble the
  error up to the Settings editor and surface in a small inline
  message. Editor rolls back the unsaved change.
- No new log lines on the relay side (templates never leave the
  client). No new metrics.

## 8. Testing

### 8.1 `desktop/frontend/src/lib/__tests__/templates.test.ts`

```ts
describe('effectiveTemplates', () => {
  it('returns persisted list when non-empty', async () => {
    const bridge = { load: async () => [{ id: 'x', label: 'x', text: 'x' }] }
    expect(await effectiveTemplates(bridge)).toEqual([{ id: 'x', label: 'x', text: 'x' }])
  })
  it('returns DEFAULT_TEMPLATES when persisted list is empty', async () => {
    const bridge = { load: async () => [] }
    expect(await effectiveTemplates(bridge)).toEqual(DEFAULT_TEMPLATES)
  })
})

describe('DEFAULT_TEMPLATES', () => {
  it('has stable default- IDs', () => {
    for (const t of DEFAULT_TEMPLATES) {
      expect(t.id).toMatch(/^default-/)
    }
  })
  it('has unique IDs', () => {
    const ids = new Set(DEFAULT_TEMPLATES.map(t => t.id))
    expect(ids.size).toBe(DEFAULT_TEMPLATES.length)
  })
})
```

### 8.2 `desktop/frontend/src/platform/__tests__/capacitor.test.ts` (extended)

Three new cases:

- `templates.load` returns `[]` when localStorage is empty.
- `templates.save` then `templates.load` round-trips a list.
- `templates.clear` removes the key.

### 8.3 `desktop/frontend/src/components/__tests__/TemplatePreviewDialog.test.ts`

- Closed when `template` prop is null.
- Renders the text content when `template` is set.
- Emits `confirm` on Enter and on the Send button click.
- Emits `cancel` on Escape, on backdrop click, and on the Cancel button.

### 8.4 `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts` (extended)

- Mounts with `platform.templates.load()` returning the defaults.
- Clicking the first `[data-testid="template-btn-default-y"]` opens
  the preview dialog (`[data-testid="template-preview"]` visible).
- Confirming the dialog triggers `sendInput('y\r')` on the mocked
  connection.
- The legacy `QUICK_TEXTS` testids (`mobile-quick-y`, …) are absent
  — those buttons no longer exist.

### 8.5 `desktop/frontend/src/components/__tests__/TerminalView.test.ts` (extended)

Same flow for desktop's TerminalView. Confirm `<TemplateBar>` renders
with the loaded templates and that clicks open the preview.

### 8.6 `desktop/frontend/src/components/__tests__/SettingsTemplates.test.ts`

- Renders the loaded list as rows.
- "Add template" inline form appears on click.
- Saving an edit calls `platform.templates.save(...)` with the updated
  list (verify the exact array structure).
- "Reset to defaults" opens a confirm dialog; on confirm calls
  `platform.templates.clear()`.

### 8.7 `desktop/app_quick_templates_test.go`

- `GetQuickTemplates` on a fresh App returns empty slice.
- `SetQuickTemplates([{id, label, text}, ...])` then `GetQuickTemplates`
  returns the same list. Verify via the existing `cfgStore` test helper
  used by other binding tests (`newRelayTestApp(t)`).
- `SetQuickTemplates([])` clears storage; subsequent `GetQuickTemplates`
  returns empty.

## 9. i18n

New keys under `settings.templates`:

| key | en | zh-CN |
|---|---|---|
| `settings.templates.tab` | "Templates" | "快捷模板" |
| `settings.templates.intro` | "Each template sends its text to the active session when clicked." | "每个模板被点击时会把它的文本发到当前会话。" |
| `settings.templates.add` | "Add template" | "新增模板" |
| `settings.templates.label` | "Label" | "标签" |
| `settings.templates.text` | "Send text" | "发送文本" |
| `settings.templates.save` | "Save" | "保存" |
| `settings.templates.delete` | "Delete" | "删除" |
| `settings.templates.reset` | "Reset to defaults" | "恢复默认" |
| `settings.templates.resetConfirm` | "Replace your list with the defaults?" | "用默认列表覆盖当前模板？" |
| `settings.templates.preview.title` | "Send preview" | "发送预览" |
| `settings.templates.preview.send` | "Send" | "发送" |

`common.cancel` already exists.

## 10. Rollout

- Single Wails binding addition + one new Go field. No migrations.
- localStorage on mobile/web reads cleanly when the key is absent
  (returns `[]` → defaults).
- Existing mobile builds that don't have the new bar still work
  fine — they keep their old `QUICK_TEXTS` baked-in until upgraded.
  (This is by design: nothing on the relay needs to change.)
- No feature flag.

## 11. Non-goals revisited

- **No cross-machine sync** — explicit user choice; macOS / Windows /
  iOS / web each have their own list. Operators who want them in sync
  can copy the JSON manually.
- **No mobile editor in v0.5** — defaults only; user edits from
  desktop, then the next time mobile reloads it picks up its own
  (unchanged) localStorage value. Mobile / web editor lands in a
  follow-up if user demand justifies the effort.
- **No keyboard shortcuts** for invoking templates — Enter inside the
  preview dialog is the only keystroke. Hotkeys (e.g. ⌘1 ⌘2 …) are a
  natural future enhancement.
- **No template categories / tags** — a flat list is enough for the
  10-ish item user case. If the list grows past ~30 we'll revisit.
