import { ref } from "vue";

// Module-scoped reactive set of session ids whose remote Web Preview is
// currently open. TerminalView owns the preview but the badge that hosts the
// preview controls lives in PaneGrid; this shared state lets PaneGrid remove the
// redundant host label from the badge DOM (v-if) while the preview is open.
//
// Removing the label nodes — rather than a `:has()` rule or `display:none` —
// is deliberate: both left the right-anchored, shrink-to-fit badge collapsed in
// the WebKit build Wails ships until a manual window resize forced a reflow.
// A DOM node removal reliably invalidates the badge's layout on the same tick.
const activeIds = ref<Set<string>>(new Set());

export function useServicePreviewActive() {
  return activeIds;
}

export function setServicePreviewActive(sessionId: string, active: boolean): void {
  if (!sessionId) return;
  const has = activeIds.value.has(sessionId);
  if (active === has) return;
  const next = new Set(activeIds.value);
  if (active) next.add(sessionId);
  else next.delete(sessionId);
  activeIds.value = next;
}
