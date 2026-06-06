// QuickTemplate is one row of the user's quick-template list. A click on the
// rendered button sends `text` + '\r' through SessionConnection.sendInput
// immediately (no preview dialog). `hotkey` is desktop-only and ignored here.
//
// Mirror of desktop/frontend/src/lib/templates.ts — changes here MUST
// be reflected there (and in desktop/quick_templates.go).
export interface QuickTemplate {
  id: string     // crypto.randomUUID() at first creation; stable thereafter
  label: string  // button text, ≤ 24 chars recommended
  text: string   // payload (no trailing CR; renderer appends one)
  hotkey?: string // desktop-only; ignored on web
}

// DEFAULT_TEMPLATES seeds the list on first use. IDs are deterministic
// "default-X" strings so resetting and re-seeding doesn't churn DOM keys.
// Order defines the initial button arrangement.
export const DEFAULT_TEMPLATES: readonly QuickTemplate[] = Object.freeze([
  { id: 'default-yes',      label: 'yes',      text: 'yes' },
  { id: 'default-ok',       label: 'ok',       text: 'ok' },
  { id: 'default-continue', label: 'continue', text: 'continue' },
  { id: 'default-commit',   label: 'commit',   text: 'commit' },
  { id: 'default-push',     label: 'push',     text: 'push' },
  { id: 'default-release',  label: 'release',  text: 'release' },
  { id: 'default-1',        label: '1',        text: '1' },
  { id: 'default-2',        label: '2',        text: '2' },
  { id: 'default-3',        label: '3',        text: '3' },
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

const STORAGE_KEY = 'atterm.templates'

// webTemplateStorage is the web's localStorage-backed bridge. Same
// shape as desktop's TemplateBridge so the bar can consume it
// generically via effectiveTemplates().
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
