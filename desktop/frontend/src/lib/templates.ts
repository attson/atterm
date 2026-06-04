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
