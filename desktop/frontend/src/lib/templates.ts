// QuickTemplate is one row of the user's quick-template list. A click on the
// rendered button sends `text` first, then a standalone '\r' one tick later,
// through SessionConnection.sendInput (no preview dialog). The two-step send
// matters for raw-mode TUIs like Codex that treat a single "text\r" payload
// as a paste — the trailing CR would land inside the paste body as a literal
// newline instead of being read as Enter. On desktop, an optional hotkey
// string (e.g. "Alt+1", "Mod+Shift+P") triggers the same send when the
// active pane is focused; mobile/web ignore the field.
export interface QuickTemplate {
  id: string     // crypto.randomUUID() at first creation; stable thereafter
  label: string  // button text, ≤ 24 chars recommended
  text: string   // payload (no trailing CR; renderer appends one)
  hotkey?: string // desktop-only; free-form e.g. "Alt+1" or "Mod+Shift+P"
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
