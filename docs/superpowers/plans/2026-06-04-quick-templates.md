# AI quick-action templates — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the mobile-only hardcoded `QUICK_TEXTS` quick-text row with a cross-platform user-editable list of `QuickTemplate`s. Each template's button opens a preview modal, sends `text + '\r'` through the existing `sendInput` path on confirm. Desktop persists via Wails / `appConfig`; mobile and web persist via `localStorage`. Defaults seed 10 entries (y / n / yes / no / continue / approve / deny / retry / /test / /diff).

**Architecture:** One shared TS interface (`QuickTemplate { id, label, text }`) consumed by three persistence implementations behind a `TemplateBridge` on `Platform`. One shared preview-dialog component and one shared template-bar markup across the three terminal views. Desktop adds a Settings → Templates tab as the only edit surface in v0.5; mobile / web ship defaults only.

**Tech Stack:** Go stdlib (Wails bindings + JSON config). TypeScript + Vue 3 (no new deps). vitest for frontend, `go test` for backend.

**Reference spec:** `docs/superpowers/specs/2026-06-04-quick-templates-design.md`

---

## File map

### Backend (Go)
- **Create:** `desktop/quick_templates.go` — `QuickTemplate` struct + `GetQuickTemplates` / `SetQuickTemplates` Wails bindings.
- **Create:** `desktop/quick_templates_test.go` — round-trip + clear test.
- **Modify:** `desktop/config.go` — add `QuickTemplates []QuickTemplate` field.

### Frontend shared model
- **Create:** `desktop/frontend/src/lib/templates.ts` — `QuickTemplate`, `DEFAULT_TEMPLATES`, `effectiveTemplates`.
- **Create:** `desktop/frontend/src/lib/__tests__/templates.test.ts`.
- **Create:** `web/src/shared/templates.ts` — verbatim copy of the desktop module.

### Platform bridge
- **Modify:** `desktop/frontend/src/platform/types.ts` — add `TemplateBridge` and `Platform.templates`.
- **Modify:** `desktop/frontend/src/platform/wails.ts` — implement `templates` via the two new Wails bindings.
- **Modify:** `desktop/frontend/src/platform/capacitor.ts` — implement `templates` via `localStorage['atterm.templates']`.
- **Modify:** `desktop/frontend/src/platform/__tests__/capacitor.test.ts` — three new cases.
- **Modify:** `desktop/frontend/src/platform/__tests__/_fakePlatform.ts` — add a `templates` stub.

### TS bindings shim (desktop)
- **Modify:** `desktop/frontend/src/lib/api.ts` — declare the two new `AppBindings` methods.

### Web shared
- **Modify:** `web/src/shared/api/client.ts` (or wherever the web platform abstractions live; verify in T3) — wire `templates` bridge backed by localStorage.

### Renderers
- **Create:** `desktop/frontend/src/components/TemplatePreviewDialog.vue`.
- **Create:** `desktop/frontend/src/components/__tests__/TemplatePreviewDialog.test.ts`.
- **Modify:** `desktop/frontend/src/mobile/MobileTerminal.vue` — replace `QUICK_TEXTS` quickbar with template bar + preview dialog.
- **Modify:** `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts` — drop the QUICK_TEXTS assertion (`mobile-quick-y` no longer exists), add template-bar tests.
- **Modify:** `desktop/frontend/src/components/TerminalView.vue` — add template bar + dialog above the status row.
- **Modify:** `desktop/frontend/src/components/TerminalView.test.ts` — template-bar mount assertion.
- **Modify:** `web/src/main/components/TerminalView.vue` (verify name; if web has no per-session terminal view yet, defer the web bar to a separate task at the end and document the skip).

### Settings editor (desktop only)
- **Create:** `desktop/frontend/src/components/SettingsTemplates.vue`.
- **Create:** `desktop/frontend/src/components/__tests__/SettingsTemplates.test.ts`.
- **Modify:** `desktop/frontend/src/components/SettingsDialog.vue` — register the 8th tab.

### i18n
- **Modify:** `desktop/frontend/src/i18n/messages/en.ts` — add `settings.templates.*`.
- **Modify:** `desktop/frontend/src/i18n/messages/zh-CN.ts` — same shape, Chinese.

---

## Task 1: Shared TS model `lib/templates.ts` (test-first)

**Files:**
- Create: `desktop/frontend/src/lib/templates.ts`
- Create: `desktop/frontend/src/lib/__tests__/templates.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/lib/__tests__/templates.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { DEFAULT_TEMPLATES, effectiveTemplates } from '../templates'

describe('DEFAULT_TEMPLATES', () => {
  it('exposes the 10 starter entries', () => {
    expect(DEFAULT_TEMPLATES).toHaveLength(10)
  })

  it('has stable default- IDs', () => {
    for (const t of DEFAULT_TEMPLATES) {
      expect(t.id).toMatch(/^default-/)
    }
  })

  it('has unique IDs', () => {
    const ids = new Set(DEFAULT_TEMPLATES.map(t => t.id))
    expect(ids.size).toBe(DEFAULT_TEMPLATES.length)
  })

  it('includes the canonical AI tokens', () => {
    const texts = DEFAULT_TEMPLATES.map(t => t.text)
    for (const expected of ['y', 'n', 'yes', 'no', 'continue', 'approve', 'deny', 'retry', '/test', '/diff']) {
      expect(texts).toContain(expected)
    }
  })
})

describe('effectiveTemplates', () => {
  it('returns persisted list when non-empty', async () => {
    const bridge = { load: async () => [{ id: 'x', label: 'x', text: 'x' }] }
    const got = await effectiveTemplates(bridge)
    expect(got).toEqual([{ id: 'x', label: 'x', text: 'x' }])
  })

  it('returns DEFAULT_TEMPLATES when persisted list is empty', async () => {
    const bridge = { load: async () => [] }
    expect(await effectiveTemplates(bridge)).toBe(DEFAULT_TEMPLATES)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/lib/__tests__/templates.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `templates.ts`**

Create `desktop/frontend/src/lib/templates.ts`:

```ts
// QuickTemplate is one row of the user's quick-template list. Sent
// verbatim through SessionConnection.sendInput when the user confirms
// the preview; the renderer appends a single '\r' so the PTY treats
// it as a complete line (matches the legacy sendQuick behaviour).
export interface QuickTemplate {
  id: string     // crypto.randomUUID() at first creation; stable thereafter
  label: string  // button text, ≤ 24 chars recommended
  text: string   // payload (no trailing CR; renderer appends one)
}

// DEFAULT_TEMPLATES seeds the list on first use. IDs are deterministic
// "default-X" strings so resetting and re-seeding doesn't churn DOM
// keys. Order defines the initial button arrangement.
export const DEFAULT_TEMPLATES: readonly QuickTemplate[] = Object.freeze([
  { id: 'default-y',        label: 'y',        text: 'y' },
  { id: 'default-n',        label: 'n',        text: 'n' },
  { id: 'default-yes',      label: 'yes',      text: 'yes' },
  { id: 'default-no',       label: 'no',       text: 'no' },
  { id: 'default-continue', label: 'continue', text: 'continue' },
  { id: 'default-approve',  label: 'approve',  text: 'approve' },
  { id: 'default-deny',     label: 'deny',     text: 'deny' },
  { id: 'default-retry',    label: 'retry',    text: 'retry' },
  { id: 'default-test',     label: '/test',    text: '/test' },
  { id: 'default-diff',     label: '/diff',    text: '/diff' },
])

// effectiveTemplates returns persisted list if non-empty, otherwise
// the frozen DEFAULT_TEMPLATES. Callers should never see an empty
// list from this helper — defaults are guaranteed. Returning the
// same reference when empty also lets renderers identity-check
// "did the user customize?" for reset affordances.
export async function effectiveTemplates(
  bridge: { load: () => Promise<QuickTemplate[]> }
): Promise<readonly QuickTemplate[]> {
  const stored = await bridge.load()
  return stored.length > 0 ? stored : DEFAULT_TEMPLATES
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/lib/__tests__/templates.test.ts`
Expected: PASS — all 5 cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/templates.ts desktop/frontend/src/lib/__tests__/templates.test.ts
git -c commit.gpgsign=false commit -m "frontend/templates: QuickTemplate type + 10-entry defaults + effectiveTemplates helper"
```

---

## Task 2: Go-side `QuickTemplate` + Wails bindings (test-first)

**Files:**
- Create: `desktop/quick_templates.go`
- Create: `desktop/quick_templates_test.go`
- Modify: `desktop/config.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/quick_templates_test.go`:

```go
package main

import (
	"reflect"
	"testing"
)

func TestGetQuickTemplates_FreshAppReturnsEmpty(t *testing.T) {
	a := newRelayTestApp(t)
	got := a.GetQuickTemplates()
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(got))
	}
}

func TestSetQuickTemplates_RoundTrips(t *testing.T) {
	a := newRelayTestApp(t)
	want := []QuickTemplate{
		{ID: "a", Label: "A", Text: "a-text"},
		{ID: "b", Label: "B", Text: "b-text"},
	}
	if err := a.SetQuickTemplates(want); err != nil {
		t.Fatalf("SetQuickTemplates: %v", err)
	}
	got := a.GetQuickTemplates()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch:\n got=%v\nwant=%v", got, want)
	}
}

func TestSetQuickTemplates_EmptyClears(t *testing.T) {
	a := newRelayTestApp(t)
	_ = a.SetQuickTemplates([]QuickTemplate{{ID: "x", Label: "x", Text: "x"}})
	if err := a.SetQuickTemplates(nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got := a.GetQuickTemplates()
	if len(got) != 0 {
		t.Fatalf("expected empty after clear, got %d", len(got))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestGetQuickTemplates -v`
Expected: FAIL — `undefined: QuickTemplate` / `undefined: GetQuickTemplates`.

- [ ] **Step 3: Add `QuickTemplates` to `appConfig`**

In `desktop/config.go`, find the `appConfig struct` definition (around line 38). Add the new field at the end of the struct, before the closing brace:

```go
type appConfig struct {
	// ...existing fields...
	// QuickTemplates persists the user's quick-action button list. Empty
	// or absent means "use defaults" — the renderer seeds DEFAULT_TEMPLATES
	// in that case. See docs/superpowers/specs/2026-06-04-quick-templates-design.md.
	QuickTemplates []QuickTemplate `json:"quick_templates,omitempty"`
}
```

- [ ] **Step 4: Implement the bindings**

Create `desktop/quick_templates.go`:

```go
package main

// QuickTemplate is one entry of the user's quick-action template list.
// Mirrors the TS interface in desktop/frontend/src/lib/templates.ts —
// changes here MUST be reflected there (and in web/src/shared/templates.ts).
type QuickTemplate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// GetQuickTemplates returns the user's persisted templates. Always
// returns a non-nil slice; an empty slice means "no customisation,
// renderer falls back to DEFAULT_TEMPLATES".
func (a *App) GetQuickTemplates() []QuickTemplate {
	if a.cfgStore == nil {
		return []QuickTemplate{}
	}
	cfg := a.cfgStore.Get()
	if cfg.QuickTemplates == nil {
		return []QuickTemplate{}
	}
	// Return a shallow copy so callers can't mutate config-store state.
	out := make([]QuickTemplate, len(cfg.QuickTemplates))
	copy(out, cfg.QuickTemplates)
	return out
}

// SetQuickTemplates persists list verbatim. Passing nil or an empty
// slice clears storage; next GetQuickTemplates returns []. Caller is
// responsible for any validation (labels non-empty, no duplicate IDs,
// etc.) — this method is a thin pass-through.
func (a *App) SetQuickTemplates(list []QuickTemplate) error {
	if a.cfgStore == nil {
		return nil
	}
	cfg := a.cfgStore.Get()
	if len(list) == 0 {
		cfg.QuickTemplates = nil
	} else {
		cfg.QuickTemplates = list
	}
	return a.cfgStore.Set(cfg)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestGetQuickTemplates -v`
Expected: PASS — 3 tests.

Run the full desktop suite as a regression check:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/config.go desktop/quick_templates.go desktop/quick_templates_test.go
git -c commit.gpgsign=false commit -m "desktop/app: GetQuickTemplates + SetQuickTemplates Wails bindings on appConfig"
```

---

## Task 3: `TemplateBridge` on `Platform` (test-first for the bridge contract)

**Files:**
- Modify: `desktop/frontend/src/platform/types.ts`
- Modify: `desktop/frontend/src/platform/wails.ts`
- Modify: `desktop/frontend/src/platform/capacitor.ts`
- Modify: `desktop/frontend/src/platform/__tests__/capacitor.test.ts`
- Modify: `desktop/frontend/src/platform/__tests__/_fakePlatform.ts`
- Modify: `desktop/frontend/src/lib/api.ts`

- [ ] **Step 1: Write the failing test (capacitor localStorage round-trip)**

Append to `desktop/frontend/src/platform/__tests__/capacitor.test.ts`:

```ts
describe('createCapacitorPlatform — templates', () => {
  beforeEach(() => { localStorage.clear() })

  it('templates.load returns [] when localStorage is empty', async () => {
    const p = createCapacitorPlatform()
    expect(await p.templates.load()).toEqual([])
  })

  it('templates.save then load round-trips a list', async () => {
    const p = createCapacitorPlatform()
    const list = [{ id: 'a', label: 'A', text: 'a-text' }]
    await p.templates.save(list)
    expect(await p.templates.load()).toEqual(list)
  })

  it('templates.clear removes the key', async () => {
    const p = createCapacitorPlatform()
    await p.templates.save([{ id: 'x', label: 'x', text: 'x' }])
    await p.templates.clear()
    expect(localStorage.getItem('atterm.templates')).toBeNull()
    expect(await p.templates.load()).toEqual([])
  })

  it('templates.load returns [] on malformed JSON', async () => {
    localStorage.setItem('atterm.templates', '{not json')
    const p = createCapacitorPlatform()
    expect(await p.templates.load()).toEqual([])
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/platform/__tests__/capacitor.test.ts -t "templates"`
Expected: FAIL — `p.templates` is `undefined`.

- [ ] **Step 3: Add the `TemplateBridge` interface to `types.ts`**

In `desktop/frontend/src/platform/types.ts`, near the other bridge interfaces:

```ts
import type { QuickTemplate } from '../lib/templates'

export interface TemplateBridge {
  load(): Promise<QuickTemplate[]>
  save(list: QuickTemplate[]): Promise<void>
  clear(): Promise<void>
}
```

Then update the `Platform` interface (lines ~128-136):

```ts
export interface Platform {
  caps: Capabilities
  relay: RelayBridge
  sessions: SessionBridge
  system: SystemBridge
  events: EventBus
  templates: TemplateBridge
  updater?: UpdaterBridge
  pluginHost?: PluginHostBridge
}
```

`templates` is required (no `?`) because every renderer needs it; missing it would crash on first paint of any terminal view.

- [ ] **Step 4: Implement on Capacitor**

In `desktop/frontend/src/platform/capacitor.ts`, add to the file scope near the `STORAGE_KEY` constant:

```ts
const TEMPLATES_KEY = 'atterm.templates'
```

Inside the object returned by `createCapacitorPlatform`, add a sibling to `relay` / `sessions`:

```ts
templates: {
  load: async () => {
    if (typeof localStorage === 'undefined') return []
    const raw = localStorage.getItem(TEMPLATES_KEY)
    if (!raw) return []
    try {
      const parsed = JSON.parse(raw)
      return Array.isArray(parsed) ? (parsed as QuickTemplate[]) : []
    } catch {
      return []
    }
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

Add the import at the top of the file:

```ts
import type { QuickTemplate } from '../lib/templates'
```

- [ ] **Step 5: Implement on Wails**

In `desktop/frontend/src/platform/wails.ts`, mirror the bridge inside the `createWailsPlatform()` object literal (sibling of `relay`):

```ts
templates: {
  load: async () => {
    const raw = await bindings().GetQuickTemplates()
    return Array.isArray(raw) ? raw : []
  },
  save: async (list) => {
    await bindings().SetQuickTemplates(list)
  },
  clear: async () => {
    await bindings().SetQuickTemplates([])
  },
},
```

Add the import:

```ts
import type { QuickTemplate } from '../lib/templates'
```

- [ ] **Step 6: Declare the two new Wails bindings on `AppBindings` in `lib/api.ts`**

In `desktop/frontend/src/lib/api.ts`, find the `interface AppBindings` block. Add two methods:

```ts
interface AppBindings {
  // ...existing methods...
  GetQuickTemplates(): Promise<import('./templates').QuickTemplate[]>
  SetQuickTemplates(list: import('./templates').QuickTemplate[]): Promise<void>
}
```

(Using inline `import()` here avoids cluttering the top of the file with a type-only import that's only referenced once. If the file already has many `import type` lines, prefer a top-level `import type { QuickTemplate } from './templates'` and use `QuickTemplate[]` directly.)

- [ ] **Step 7: Update the fake-platform helper used in tests**

In `desktop/frontend/src/platform/__tests__/_fakePlatform.ts`, find the `createFakePlatform` body. Add inside the returned `Platform` object literal:

```ts
templates: {
  load: vi.fn().mockResolvedValue([]),
  save: vi.fn().mockResolvedValue(undefined),
  clear: vi.fn().mockResolvedValue(undefined),
},
```

- [ ] **Step 8: Run all the platform/capacitor tests**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/platform/__tests__/capacitor.test.ts`
Expected: PASS — new templates cases plus all existing ones.

Also run the full vitest suite to catch any places that constructed a `Platform` literal inline without `templates`:

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean.

If `vue-tsc` reports inline `Platform` literals missing `templates`, fix each: either reuse `createFakePlatform()` from `_fakePlatform.ts`, or add a minimal `templates: { load: ..., save: ..., clear: ... }` stub at that callsite.

- [ ] **Step 9: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/platform/types.ts desktop/frontend/src/platform/wails.ts desktop/frontend/src/platform/capacitor.ts desktop/frontend/src/platform/__tests__/capacitor.test.ts desktop/frontend/src/platform/__tests__/_fakePlatform.ts desktop/frontend/src/lib/api.ts
git -c commit.gpgsign=false commit -m "platform: TemplateBridge (Wails + Capacitor + fakePlatform)"
```

---

## Task 4: `TemplatePreviewDialog.vue` component (test-first)

**Files:**
- Create: `desktop/frontend/src/components/TemplatePreviewDialog.vue`
- Create: `desktop/frontend/src/components/__tests__/TemplatePreviewDialog.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/__tests__/TemplatePreviewDialog.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import TemplatePreviewDialog from '../TemplatePreviewDialog.vue'

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

const sample = { id: 'default-test', label: '/test', text: '/test' }

describe('TemplatePreviewDialog', () => {
  it('renders nothing when template is null', () => {
    const w = mount(TemplatePreviewDialog, { props: { template: null } })
    expect(w.find('[data-testid="template-preview"]').exists()).toBe(false)
  })

  it('renders the template text when set', () => {
    const w = mount(TemplatePreviewDialog, { props: { template: sample } })
    expect(w.find('[data-testid="template-preview"]').exists()).toBe(true)
    expect(w.find('[data-testid="template-preview"]').text()).toContain('/test')
  })

  it('emits confirm when the Send button is clicked', async () => {
    const w = mount(TemplatePreviewDialog, { props: { template: sample } })
    await w.find('[data-testid="template-preview-confirm"]').trigger('click')
    expect(w.emitted('confirm')).toBeTruthy()
    expect(w.emitted('confirm')![0]).toEqual([sample])
  })

  it('emits cancel when the Cancel button is clicked', async () => {
    const w = mount(TemplatePreviewDialog, { props: { template: sample } })
    await w.find('[data-testid="template-preview-cancel"]').trigger('click')
    expect(w.emitted('cancel')).toBeTruthy()
  })

  it('emits cancel when the backdrop is clicked', async () => {
    const w = mount(TemplatePreviewDialog, { props: { template: sample } })
    await w.find('.dialog-backdrop').trigger('click')
    expect(w.emitted('cancel')).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/__tests__/TemplatePreviewDialog.test.ts`
Expected: FAIL — component module not found.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/components/TemplatePreviewDialog.vue`:

```vue
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from '../i18n/useI18n'
import type { QuickTemplate } from '../lib/templates'

const props = defineProps<{ template: QuickTemplate | null }>()
const emit = defineEmits<{
  (e: 'confirm', t: QuickTemplate): void
  (e: 'cancel'): void
}>()
const { t } = useI18n()

const open = computed(() => props.template !== null)

function onKeydown(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'Enter') {
    e.preventDefault()
    if (props.template) emit('confirm', props.template)
  } else if (e.key === 'Escape') {
    e.preventDefault()
    emit('cancel')
  }
}

onMounted(() => { window.addEventListener('keydown', onKeydown) })
onBeforeUnmount(() => { window.removeEventListener('keydown', onKeydown) })
</script>

<template>
  <div
    v-if="open"
    class="dialog-backdrop"
    data-testid="template-preview-backdrop"
    @click="emit('cancel')"
  >
    <div class="dialog" data-testid="template-preview" @click.stop>
      <h3>{{ t('settings.templates.preview.title') }}</h3>
      <pre class="preview">{{ template?.text }}</pre>
      <div class="actions">
        <button
          type="button"
          data-testid="template-preview-cancel"
          @click="emit('cancel')"
        >{{ t('common.cancel') }}</button>
        <button
          type="button"
          autofocus
          data-testid="template-preview-confirm"
          @click="if (template) emit('confirm', template)"
        >{{ t('settings.templates.preview.send') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dialog-backdrop {
  position: fixed; inset: 0; z-index: 50;
  background: rgba(0, 0, 0, 0.55);
  display: flex; align-items: center; justify-content: center;
  padding: 1rem;
}
.dialog {
  width: 100%; max-width: 320px;
  background: #11182b; color: #e6e7ea;
  border: 1px solid #1e2638; border-radius: 11px;
  padding: 14px 16px;
  display: flex; flex-direction: column; gap: 10px;
}
.dialog h3 { margin: 0; font-size: 0.95rem; font-weight: 600; }
.preview {
  margin: 0; padding: 8px 10px;
  background: #020617; color: #e2e8f0;
  border: 1px solid #1e2638; border-radius: 8px;
  font-family: var(--font-mono);
  font-size: 0.82rem; line-height: 1.4;
  white-space: pre-wrap; word-break: break-all;
  max-height: 30vh; overflow-y: auto;
}
.actions {
  display: flex; gap: 8px; justify-content: flex-end;
}
.actions button {
  height: 32px; padding: 0 14px;
  border: 1px solid #1e2638; border-radius: 7px;
  background: #11182b; color: #cbd5e1; font-size: 0.82rem;
}
.actions button:last-child {
  background: #2563eb; border-color: #2563eb; color: #ffffff; font-weight: 600;
}
</style>
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/__tests__/TemplatePreviewDialog.test.ts`
Expected: PASS — all 5 cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/TemplatePreviewDialog.vue desktop/frontend/src/components/__tests__/TemplatePreviewDialog.test.ts
git -c commit.gpgsign=false commit -m "frontend/TemplatePreviewDialog: Enter/Esc + backdrop close, emits confirm/cancel"
```

---

## Task 5: Mobile `MobileTerminal.vue` — replace QUICK_TEXTS with template bar (test-first)

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileTerminal.vue`
- Modify: `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts`

- [ ] **Step 1: Write the failing tests**

Append to `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts` (the existing file already mocks `connection` etc.):

```ts
it('mounts the template bar with templates loaded from platform.templates', async () => {
  const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
  await flushPromises()
  // The defaults seed includes default-y; mock platform returns [] which falls back to DEFAULT_TEMPLATES.
  expect(w.find('[data-testid="template-bar"]').exists()).toBe(true)
  expect(w.find('[data-testid="template-btn-default-y"]').exists()).toBe(true)
})

it('opens the preview dialog when a template button is clicked', async () => {
  const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
  await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
  await flushPromises()
  await w.find('[data-testid="template-btn-default-y"]').trigger('click')
  expect(w.find('[data-testid="template-preview"]').exists()).toBe(true)
  expect(w.find('[data-testid="template-preview"]').text()).toContain('y')
})

it('sends template text plus CR when the preview Send button is clicked', async () => {
  const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
  await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
  await flushPromises()
  await w.find('[data-testid="template-btn-default-yes"]').trigger('click')
  await w.find('[data-testid="template-preview-confirm"]').trigger('click')
  expect(sendInput).toHaveBeenCalledWith('yes\r')
})

it('does not render the legacy QUICK_TEXTS row', () => {
  const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
  expect(w.find('[data-testid="mobile-quick-y"]').exists()).toBe(false)
  expect(w.find('[data-testid="mobile-quick-continue"]').exists()).toBe(false)
})
```

If the existing mock doesn't expose `platform.templates`, ensure the `vi.mock('../../platform', () => ({ usePlatform: () => ({ ... }) }))` block includes a `templates` object whose `load` returns `[]`. The previous tests that test paste/image flows already mock `relay` + `sessions`; add a templates field to that returned object.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/MobileTerminal.test.ts -t "template"`
Expected: FAIL — `template-bar` testid not present.

- [ ] **Step 3: Update `MobileTerminal.vue`**

In `desktop/frontend/src/mobile/MobileTerminal.vue`:

(a) Update the script imports. Replace:

```ts
import { SessionConnection, type Endpoint } from '../lib/connection'
```

with the same line plus the templates module + dialog component:

```ts
import { SessionConnection, type Endpoint } from '../lib/connection'
import { effectiveTemplates, type QuickTemplate } from '../lib/templates'
import TemplatePreviewDialog from '../components/TemplatePreviewDialog.vue'
```

(b) Add reactive state alongside the existing `pasteOpen` / `controlMode` refs:

```ts
const templates = ref<readonly QuickTemplate[]>([])
const pendingTemplate = ref<QuickTemplate | null>(null)
```

(c) Inside `onMounted` (just after `conn.attach()` is called), load templates:

```ts
effectiveTemplates(platform.templates).then((list) => { templates.value = list })
```

Note: `platform` is the existing `usePlatform()` result already in scope (other parts of the file use `platform.relay.consumePairing` etc.). If not, add `const platform = usePlatform()` and the import for `usePlatform` at the top.

(d) Add the click and confirm handlers (placed near `openPasteConfirm` / `confirmPaste`):

```ts
function onTemplateClick(t: QuickTemplate) {
  if (!canSend.value) return
  pendingTemplate.value = t
}

function confirmTemplate(t: QuickTemplate) {
  pendingTemplate.value = null
  sendRaw(`${t.text}\r`)
}

function cancelTemplate() {
  pendingTemplate.value = null
}
```

(e) Delete the `QUICK_TEXTS` constant declaration (around line 46), the `sendQuick` function (around line 61), and the `<div class="quickbar">` block in the template (around lines 223-235).

(f) In the template, **above** the existing `<div class="kbbar">` block (the aux-keys row), insert:

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

At the bottom of the template (after `</div>` closing the root container, but inside the root if it's a single root component, OR at the end of the existing root), add the dialog:

```vue
<TemplatePreviewDialog
  :template="pendingTemplate"
  @confirm="confirmTemplate"
  @cancel="cancelTemplate"
/>
```

(g) Add CSS to the `<style scoped>` block:

```css
.template-bar {
  display: flex; align-items: center; gap: 6px;
  overflow-x: auto; padding: 4px 8px;
  border-top: 1px solid #1e2638;
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

Remove the now-unused `.quickbar` rule from the style block (it's typically shared with `.kbbar` — keep the `.kbbar` part).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/MobileTerminal.test.ts`
Expected: PASS — new template tests + all existing tests. Existing tests that referenced `mobile-quick-*` testids should already have been removed in step 1 of this task; if not, scrub them.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/MobileTerminal.vue desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts
git -c commit.gpgsign=false commit -m "mobile/Terminal: replace QUICK_TEXTS with template bar + preview dialog"
```

---

## Task 6: Desktop `TerminalView.vue` — template bar above status row (test-first)

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Modify: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `desktop/frontend/src/components/TerminalView.test.ts` (mount whatever the existing tests mount; the file's pattern should be visible from the top).

If the existing test file's harness isn't sufficient to drive a full TerminalView render (xterm itself is heavy), instead write a static-source check:

```ts
it("includes the template bar markup", async () => {
  const src = await import("node:fs").then((fs) =>
    fs.promises.readFile(
      new URL("../TerminalView.vue", import.meta.url),
      "utf-8",
    ),
  );
  expect(src).toContain('data-testid="template-bar"');
  expect(src).toContain("TemplatePreviewDialog");
});
```

(This mirrors the existing pattern in the test file if it already uses static-source checks — verify by reading the existing tests. If component-level tests are already in place using `@vue/test-utils`, write a proper mount-based test similar to the mobile one. The pattern in `TerminalView.test.ts` should drive the choice.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/TerminalView.test.ts -t "template"`
Expected: FAIL — template-bar markup not present.

- [ ] **Step 3: Update `TerminalView.vue`**

In `desktop/frontend/src/components/TerminalView.vue`:

(a) Add imports near the top of `<script setup>`:

```ts
import { effectiveTemplates, type QuickTemplate } from "../lib/templates";
import TemplatePreviewDialog from "./TemplatePreviewDialog.vue";
import { usePlatform } from "../platform";
```

(If `usePlatform` is already imported, skip.)

(b) Add refs near other refs (templates list + pending):

```ts
const templates = ref<readonly QuickTemplate[]>([]);
const pendingTemplate = ref<QuickTemplate | null>(null);
```

(c) In `onMounted`, after `term.open(...)` (or any existing `platform.X` call), load:

```ts
effectiveTemplates(usePlatform().templates).then((list) => { templates.value = list });
```

If a `platform` const already exists at the script scope (typical: `const platform = usePlatform()`), reuse it.

(d) Add handlers near other functions:

```ts
function onTemplateClick(t: QuickTemplate) {
  if (!canSend.value) return; // assumes canSend exists; verify against existing TerminalView
  pendingTemplate.value = t;
}
function confirmTemplate(t: QuickTemplate) {
  pendingTemplate.value = null;
  conn?.sendInput(`${t.text}\r`);
}
function cancelTemplate() { pendingTemplate.value = null; }
```

Verify the gate variable name. Desktop `TerminalView` may not have a `canSend` computed — it likely just guards on `conn != null` and `!disabledStdin`. Use whatever existing flag distinguishes "can type" from "can't" — e.g. the renderer probably uses `term.options.disableStdin`. Conservative: gate purely on `conn !== null` here; the buttons stay enabled visually if mounted, which is the desktop convention (the existing right-click "send selection" uses the same approach).

(e) Template:

Find the existing `.term-view` markup. Above the existing `<div class="status-bar">`, insert:

```vue
<div class="template-bar" data-testid="template-bar">
  <button
    v-for="t in templates"
    :key="t.id"
    class="template-btn"
    :data-testid="`template-btn-${t.id}`"
    @click="onTemplateClick(t)"
  >{{ t.label }}</button>
</div>
```

At the end of the root element, add:

```vue
<TemplatePreviewDialog
  :template="pendingTemplate"
  @confirm="confirmTemplate"
  @cancel="cancelTemplate"
/>
```

(f) CSS additions to the `<style scoped>` block:

```css
.template-bar {
  flex: 0 0 auto;
  display: flex; align-items: center; gap: 6px;
  overflow-x: auto; padding: 4px 8px;
  border-top: 1px solid var(--border);
  background: var(--panel);
}
.template-btn {
  flex: 0 0 auto;
  height: 26px; padding: 0 10px;
  border-radius: 6px; background: var(--bg);
  border: 1px solid var(--border); color: var(--fg);
  font-size: 0.78rem; font-family: var(--font-mono);
}
.template-btn:hover { border-color: var(--accent); color: var(--accent); }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/TerminalView.test.ts`
Expected: PASS.

Then full vitest as regression:
Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git -c commit.gpgsign=false commit -m "desktop/TerminalView: template bar above status row + preview dialog"
```

---

## Task 7: i18n keys

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

- [ ] **Step 1: Add `settings.templates.*` keys to `en.ts`**

In `desktop/frontend/src/i18n/messages/en.ts`, locate the `settings:` object. Add a sibling block to `diagnostics`:

```ts
    templates: {
      tab: 'Templates',
      intro: 'Each template sends its text to the active session when clicked.',
      add: 'Add template',
      label: 'Label',
      text: 'Send text',
      save: 'Save',
      delete: 'Delete',
      reset: 'Reset to defaults',
      resetConfirm: 'Replace your list with the defaults?',
      preview: {
        title: 'Send preview',
        send: 'Send',
      },
    },
```

- [ ] **Step 2: Mirror in `zh-CN.ts`**

```ts
    templates: {
      tab: '快捷模板',
      intro: '每个模板被点击时会把它的文本发到当前会话。',
      add: '新增模板',
      label: '标签',
      text: '发送文本',
      save: '保存',
      delete: '删除',
      reset: '恢复默认',
      resetConfirm: '用默认列表覆盖当前模板？',
      preview: {
        title: '发送预览',
        send: '发送',
      },
    },
```

- [ ] **Step 3: Verify parity test**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/i18n/i18n.test.ts`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git -c commit.gpgsign=false commit -m "i18n: add settings.templates.* keys (en + zh-CN)"
```

---

## Task 8: `SettingsTemplates.vue` editor + integration into `SettingsDialog` (test-first)

**Files:**
- Create: `desktop/frontend/src/components/SettingsTemplates.vue`
- Create: `desktop/frontend/src/components/__tests__/SettingsTemplates.test.ts`
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/__tests__/SettingsTemplates.test.ts`:

```ts
import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import SettingsTemplates from '../SettingsTemplates.vue'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

vi.mock('../../platform', () => {
  const { createFakePlatform } = require('../../platform/__tests__/_fakePlatform')
  const fake = createFakePlatform()
  return { usePlatform: () => fake, __fake: fake }
})

describe('SettingsTemplates', () => {
  it('renders the loaded templates as rows', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'a', label: 'A', text: 'a-text' },
      { id: 'b', label: 'B', text: 'b-text' },
    ])
    const w = mount(SettingsTemplates)
    await flushPromises()
    expect(w.findAll('[data-testid^="template-row-"]')).toHaveLength(2)
  })

  it('calls save when a new template is added', async () => {
    const { __fake } = await import('../../platform') as any
    const save = vi.fn().mockResolvedValue(undefined)
    __fake.templates.load = vi.fn().mockResolvedValue([])
    __fake.templates.save = save
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-add"]').trigger('click')
    await w.find('[data-testid="template-edit-label"]').setValue('approve')
    await w.find('[data-testid="template-edit-text"]').setValue('approve')
    await w.find('[data-testid="template-edit-save"]').trigger('click')
    await flushPromises()

    expect(save).toHaveBeenCalled()
    const list = save.mock.calls[save.mock.calls.length - 1][0]
    expect(list).toHaveLength(1)
    expect(list[0]).toMatchObject({ label: 'approve', text: 'approve' })
    expect(list[0].id).toBeTruthy()
  })

  it('clears storage on Reset to defaults', async () => {
    const { __fake } = await import('../../platform') as any
    const clear = vi.fn().mockResolvedValue(undefined)
    __fake.templates.load = vi.fn().mockResolvedValue([{ id: 'x', label: 'x', text: 'x' }])
    __fake.templates.clear = clear
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-reset"]').trigger('click')
    await w.find('[data-testid="template-reset-confirm"]').trigger('click')
    await flushPromises()

    expect(clear).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/__tests__/SettingsTemplates.test.ts`
Expected: FAIL — component module not found.

- [ ] **Step 3: Implement `SettingsTemplates.vue`**

Create `desktop/frontend/src/components/SettingsTemplates.vue`:

```vue
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { DEFAULT_TEMPLATES, effectiveTemplates, type QuickTemplate } from '../lib/templates'

const { t } = useI18n()
const platform = usePlatform()

const items = ref<QuickTemplate[]>([])
const editing = ref<{ id: string; label: string; text: string; isNew: boolean } | null>(null)
const resetOpen = ref(false)
const error = ref('')

async function reload() {
  const list = await effectiveTemplates(platform.templates)
  items.value = [...list]
}

onMounted(reload)

async function persist() {
  error.value = ''
  try {
    await platform.templates.save(items.value)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function startAdd() {
  editing.value = { id: crypto.randomUUID(), label: '', text: '', isNew: true }
}
function startEdit(t: QuickTemplate) {
  editing.value = { id: t.id, label: t.label, text: t.text, isNew: false }
}
function cancelEdit() { editing.value = null }
async function saveEdit() {
  if (!editing.value) return
  const e = editing.value
  if (!e.label.trim() || !e.text) return
  if (e.isNew) {
    items.value.push({ id: e.id, label: e.label.trim(), text: e.text })
  } else {
    const idx = items.value.findIndex((x) => x.id === e.id)
    if (idx >= 0) items.value[idx] = { id: e.id, label: e.label.trim(), text: e.text }
  }
  editing.value = null
  await persist()
}
async function deleteItem(id: string) {
  items.value = items.value.filter((x) => x.id !== id)
  await persist()
}
async function moveUp(id: string) {
  const idx = items.value.findIndex((x) => x.id === id)
  if (idx <= 0) return
  ;[items.value[idx - 1], items.value[idx]] = [items.value[idx], items.value[idx - 1]]
  await persist()
}
async function moveDown(id: string) {
  const idx = items.value.findIndex((x) => x.id === id)
  if (idx < 0 || idx >= items.value.length - 1) return
  ;[items.value[idx], items.value[idx + 1]] = [items.value[idx + 1], items.value[idx]]
  await persist()
}
function startReset() { resetOpen.value = true }
async function confirmReset() {
  resetOpen.value = false
  await platform.templates.clear()
  items.value = [...DEFAULT_TEMPLATES]
}
function cancelReset() { resetOpen.value = false }
</script>

<template>
  <div class="tab-pane">
    <p class="hint">{{ t('settings.templates.intro') }}</p>

    <ul class="list">
      <li
        v-for="(it, idx) in items"
        :key="it.id"
        class="row"
        :data-testid="`template-row-${it.id}`"
      >
        <span class="label">{{ it.label }}</span>
        <code class="text">{{ it.text }}</code>
        <div class="actions">
          <button :disabled="idx === 0" @click="moveUp(it.id)">↑</button>
          <button :disabled="idx === items.length - 1" @click="moveDown(it.id)">↓</button>
          <button @click="startEdit(it)">{{ t('settings.templates.save') === 'Save' ? 'Edit' : '编辑' }}</button>
          <button class="del" :data-testid="`template-delete-${it.id}`" @click="deleteItem(it.id)">
            {{ t('settings.templates.delete') }}
          </button>
        </div>
      </li>
    </ul>

    <div v-if="editing" class="edit-row">
      <input v-model="editing.label" :placeholder="t('settings.templates.label')" data-testid="template-edit-label" />
      <input v-model="editing.text" :placeholder="t('settings.templates.text')" data-testid="template-edit-text" />
      <button data-testid="template-edit-save" @click="saveEdit">{{ t('settings.templates.save') }}</button>
      <button @click="cancelEdit">{{ t('common.cancel') }}</button>
    </div>

    <div class="footer">
      <button data-testid="template-add" @click="startAdd">{{ t('settings.templates.add') }}</button>
      <button data-testid="template-reset" @click="startReset">{{ t('settings.templates.reset') }}</button>
      <span v-if="error" class="error">{{ error }}</span>
    </div>

    <div v-if="resetOpen" class="dialog-backdrop" @click="cancelReset">
      <div class="dialog" @click.stop>
        <p>{{ t('settings.templates.resetConfirm') }}</p>
        <div class="actions">
          <button @click="cancelReset">{{ t('common.cancel') }}</button>
          <button data-testid="template-reset-confirm" @click="confirmReset">{{ t('settings.templates.reset') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tab-pane { display: flex; flex-direction: column; gap: 10px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.5; }
.list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.row { display: grid; grid-template-columns: 8rem 1fr auto; gap: 8px; align-items: center; padding: 8px 10px; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; }
.label { font-weight: 600; font-family: var(--font-mono); font-size: 0.85rem; }
.text { font-family: var(--font-mono); font-size: 0.78rem; color: var(--fg-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.actions { display: flex; gap: 4px; }
.actions button { height: 24px; padding: 0 8px; font-size: 0.74rem; }
.actions .del { color: var(--bad); border-color: rgba(248, 81, 73, 0.4); }
.edit-row { display: grid; grid-template-columns: 8rem 1fr auto auto; gap: 8px; padding: 8px 10px; background: var(--panel); border: 1px solid var(--accent); border-radius: 6px; }
.edit-row input { height: 28px; padding: 0 8px; }
.footer { display: flex; gap: 8px; align-items: center; }
.error { color: var(--bad); font-size: 0.75rem; }
.dialog-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.55); display: flex; align-items: center; justify-content: center; z-index: 50; }
.dialog { background: #11182b; padding: 14px 18px; border: 1px solid #1e2638; border-radius: 11px; max-width: 320px; display: flex; flex-direction: column; gap: 10px; color: #e6e7ea; }
</style>
```

Note: the "Edit" button label is hardcoded EN/zh hack (a quick fallback). Better: add an `edit` key under `settings.templates.*` in en.ts / zh-CN.ts and use `t('settings.templates.edit')`. Update Task 7 accordingly. Inline-Edit is simpler; the prefer-readable approach: add `settings.templates.edit` key now in Task 7 and use it here.

- [ ] **Step 4: Wire the new tab in `SettingsDialog.vue`**

Open `desktop/frontend/src/components/SettingsDialog.vue`. Mirror the steps from how P1.10's Diagnostics tab was added (which is in the same file). Four changes:

(a) Add the import next to other `SettingsX` imports near the top:

```ts
import SettingsTemplates from "./SettingsTemplates.vue";
```

(b) Extend the `activeTab` type union (around line 39) and `initialTab` prop union (around line 28) to include `"templates"`:

```ts
const activeTab = ref<"general" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates">(props.initialTab ?? "general");
```

(c) Update `switchTab`'s parameter type union the same way.

(d) In the `<template>` block, add a tab button next to the existing ones (placement: between `shortcuts` and `diagnostics` is a reasonable position):

```vue
<button
  class="tab"
  :class="{ active: activeTab === 'templates' }"
  type="button"
  @click="switchTab('templates')"
>{{ t('settings.templates.tab') }}</button>
```

And the panel:

```vue
<SettingsTemplates v-if="activeTab === 'templates'" />
```

placed in the same conditional-render section as the other tabs' panels.

- [ ] **Step 5: Add the `edit` i18n key**

(Companion to step 3 hint about the inline-edit label.)

Add to `en.ts` under `settings.templates`:

```ts
edit: 'Edit',
```

Add to `zh-CN.ts` under `settings.templates`:

```ts
edit: '编辑',
```

Replace the hardcoded ternary in `SettingsTemplates.vue` line `{{ t('settings.templates.save') === 'Save' ? 'Edit' : '编辑' }}` with:

```vue
{{ t('settings.templates.edit') }}
```

- [ ] **Step 6: Run the tests**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/__tests__/SettingsTemplates.test.ts`
Expected: PASS — 3 cases.

Then the full vitest suite:
Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run`
Expected: PASS.

And vue-tsc:
Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/SettingsTemplates.vue desktop/frontend/src/components/__tests__/SettingsTemplates.test.ts desktop/frontend/src/components/SettingsDialog.vue desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git -c commit.gpgsign=false commit -m "frontend/SettingsTemplates: editor wired into Settings dialog"
```

---

## Task 9: Web shared templates module + optional bar

**Files:**
- Create: `web/src/shared/templates.ts`
- Modify: `web/src/shared/api/client.ts` (or wherever web's platform abstraction lives — see step 1)

- [ ] **Step 1: Reconnoitre web's platform abstraction**

Run: `cd /Users/attson/code/github.com.attson/atterm/web && grep -rn "Platform\|localStorage" src/ | head -20`
This identifies how web is structured. Web likely doesn't have the same `Platform` abstraction as desktop/mobile — it may just hold a Pinia store with relay config. Inspect the result before continuing.

- [ ] **Step 2: Create the shared module**

Create `web/src/shared/templates.ts` with the same content as `desktop/frontend/src/lib/templates.ts` from Task 1 (copy-paste).

- [ ] **Step 3: Add a localStorage helper**

Append to `web/src/shared/templates.ts`:

```ts
const STORAGE_KEY = 'atterm.templates'

export const webTemplateStorage = {
  load(): Promise<QuickTemplate[]> {
    return new Promise((resolve) => {
      if (typeof localStorage === 'undefined') return resolve([])
      const raw = localStorage.getItem(STORAGE_KEY)
      if (!raw) return resolve([])
      try {
        const v = JSON.parse(raw)
        resolve(Array.isArray(v) ? (v as QuickTemplate[]) : [])
      } catch {
        resolve([])
      }
    })
  },
  save(list: QuickTemplate[]): Promise<void> {
    return new Promise((resolve) => {
      if (typeof localStorage === 'undefined') return resolve()
      localStorage.setItem(STORAGE_KEY, JSON.stringify(list))
      resolve()
    })
  },
  clear(): Promise<void> {
    return new Promise((resolve) => {
      if (typeof localStorage === 'undefined') return resolve()
      localStorage.removeItem(STORAGE_KEY)
      resolve()
    })
  },
}
```

- [ ] **Step 4: Web terminal view (optional, only if web has a per-session terminal renderer)**

If `web/src/main/components/` contains a `TerminalView.vue` or similar with an xterm canvas, add the template bar there using the same pattern as Task 6 (a horizontal bar of buttons, click opens the preview dialog, confirm calls the existing send-input path).

If web does **not** yet have a per-session terminal renderer (only a session list), **skip the bar**. Document the skip in the commit message.

- [ ] **Step 5: Run web build**

Run: `cd /Users/attson/code/github.com.attson/atterm/web && npm run build 2>&1 | tail -3`
Expected: build succeeds.

If web has tests:
Run: `cd /Users/attson/code/github.com.attson/atterm/web && npm test 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/shared/templates.ts $(git status -s web/ | awk '{print $2}')
git -c commit.gpgsign=false commit -m "web/templates: shared QuickTemplate module + localStorage helpers"
```

---

## Task 10: Final smoke

**Files:**
- (none — verification only)

- [ ] **Step 1: Full Go suite**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./...`
Expected: PASS.

- [ ] **Step 2: `go vet`**

Run: `cd /Users/attson/code/github.com.attson/atterm && go vet ./...`
Expected: clean.

- [ ] **Step 3: Desktop frontend tests + type-check**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test`
Expected: PASS — every existing test plus the new ones.

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean.

- [ ] **Step 4: Frontend builds**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:wails`
Expected: succeeds.

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:capacitor`
Expected: succeeds.

- [ ] **Step 5: Web build**

Run: `cd /Users/attson/code/github.com.attson/atterm/web && npm run build 2>&1 | tail -3`
Expected: succeeds.

- [ ] **Step 6: Manual smoke (documented, not gating)**

After merging:
1. Run desktop `wails dev`. Settings → Templates tab loads with 10 default rows. Add a new template, edit one, delete one — verify config.json updates.
2. Open a terminal tab — template bar appears above the status row. Click a button → preview dialog → Send → text + `\r` lands at the prompt.
3. Mobile: build the Capacitor bundle (`npm run ios:sync && cap open ios`). MobileTerminal shows the template bar in place of the old `y/n/yes/no/continue` row. The legacy `mobile-quick-y` selector is gone.
4. Verify `canSend` gate: switch to a view-only session → buttons disabled. Toggle control mode off → buttons disabled.

No commit needed — verification gate.

---

## Self-review notes

- **Spec coverage:**
  - §3 data model → Tasks 1 + 2 (TS + Go)
  - §4 platform bridge → Task 3
  - §5.1 template bar → Tasks 5 + 6
  - §5.2 preview dialog → Task 4
  - §5.3 editor → Task 8
  - §5.4 mount sites → Tasks 5 + 6 (web in Task 9, skip if no terminal view)
  - §6 send path → covered by Tasks 5 + 6 calling `sendInput(text + '\r')`
  - §7 errors — `effectiveTemplates` falls back on empty / malformed; editor surfaces save errors inline
  - §8 testing → Tasks 1, 2, 3, 4, 5, 6, 8 each include their own test code
  - §9 i18n → Task 7
  - §10 rollout — pure additive; no migrations; covered without a dedicated task

- **Placeholder scan:** No TBDs. Two runtime navigation hints (Task 6 step 3 mentions verifying the desktop renderer's `canSend` / equivalent gate; Task 9 step 1 instructs the implementer to recon web's structure) — both are real reconnaissance steps, not implementation gaps.

- **Type consistency:** `QuickTemplate` (TS in `lib/templates.ts`, Go in `desktop/quick_templates.go`) — fields `id`/`label`/`text` match. `DEFAULT_TEMPLATES` IDs are `default-{label}` consistently. The `TemplateBridge.load/save/clear` signatures are identical in Wails, Capacitor, and `_fakePlatform`. The `[data-testid="template-..."]` attribute names are consistent between the dialog, the bar, the editor, and the tests.

- **Ordering note:** Tasks 1–4 are dependency leaves; Tasks 5, 6, 8 depend on Tasks 1–4. Task 7 (i18n) can land anytime but `SettingsTemplates` uses the keys, so do it before or interleaved with Task 8. Task 9 (web) is independent of the rest of the front-end work and can land last.

---

Plan complete and saved to `docs/superpowers/plans/2026-06-04-quick-templates.md`. Execution choice:

**1. Subagent-Driven (recommended)** — fresh subagent per task + two-stage review.

**2. Inline Execution** — batch tasks with checkpoints.
