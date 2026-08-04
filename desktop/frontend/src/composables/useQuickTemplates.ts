import { computed, ref, type ComputedRef, type Ref } from "vue";

import { isMac } from "../lib/modKey";
import { effectiveTemplates, type QuickTemplate } from "../lib/templates";
import type { TemplateBridge } from "../platform/types";

/**
 * useQuickTemplates owns the terminal's quick-template bar state:
 * the loaded list + hidden flag, the "confirm before sending?" flow
 * for web/mobile builds that can't easily undo a keystroke, and the
 * per-template hotkey handler.
 *
 * ⚠️  HARD CONSTRAINT — sendTemplate MUST split "text + Enter" into
 * two separate sendInput calls. Codex (and other raw-mode TUIs) treats
 * a bundled "text\r" payload as a paste — the trailing CR becomes a
 * literal newline in the prompt instead of submitting. This regressed
 * twice (PR #63 fix → PR #110 delete → PR #129 re-fix) and is now
 * pinned in [[feedback_template_send_split_cr]]. Do not "simplify"
 * the setTimeout(...16) into a single sendInput call.
 */
export interface UseQuickTemplatesOpts {
  /** Send a payload down the driver SessionConnection. Called twice per
   *  sendTemplate: once with the template text, once with "\r" after
   *  16ms — see the module-level HARD CONSTRAINT note. */
  send: (text: string) => void;
  /** When true, requestTemplateSend pops the confirm panel instead of
   *  sending immediately. Bound to platform.caps in the caller
   *  (web/mobile without wailsBindings / localPty ⇒ confirm required
   *  so a mis-tap on a phone doesn't fire a destructive command). */
  confirmRequired: ComputedRef<boolean>;
  /** Whether this pane is currently the focused one. onTemplateHotkey
   *  gates on this so multiple mounted TerminalViews don't all fire
   *  on the same keypress. */
  focused: () => boolean;
  /** Platform-side templates persistence (load / loadHidden). */
  templatesBridge: TemplateBridge;
}

export interface UseQuickTemplates {
  templates: Ref<readonly QuickTemplate[]>;
  templatesHidden: Ref<boolean>;
  pendingTemplateConfirm: Ref<QuickTemplate | null>;
  templateConfirmOpen: ComputedRef<boolean>;

  /** Send template text + Enter as two sendInput calls (see hard
   *  constraint above). Called directly from confirmTemplateSend and
   *  the no-confirm branch of requestTemplateSend. */
  sendTemplate(tpl: QuickTemplate): void;
  /** Public entry from template clicks / touches / hotkeys. Pops the
   *  confirm panel when confirmRequired.value, otherwise sends. */
  requestTemplateSend(tpl: QuickTemplate): void;
  cancelTemplateSend(): void;
  confirmTemplateSend(): void;

  /** document-keydown handler; register with capture=true on the
   *  document so ordinary bubble-phase listeners don't consume the
   *  hotkey first. */
  onTemplateHotkey(e: KeyboardEvent): void;

  /** .template-bar wheel handler: fold vertical wheel deltas into
   *  horizontal scroll so a mouse-only user can pan the bar without
   *  reaching for shift. Passive — never preventDefault. */
  onTemplateBarWheel(e: WheelEvent): void;

  /** Re-read templates + hidden flag from the platform. Called from
   *  the caller's 'quickTemplates:changed' listener so an open
   *  terminal reflects Settings edits immediately, no remount. */
  reload(): Promise<void>;
}

function parseHotkey(s: string): { mod: boolean; alt: boolean; shift: boolean; key: string } | null {
  if (!s) return null;
  const parts = s.split("+").map((p) => p.trim()).filter(Boolean);
  if (parts.length < 2) return null;
  let mod = false, alt = false, shift = false, key = "";
  for (const p of parts) {
    const pl = p.toLowerCase();
    if (pl === "mod" || pl === "cmd" || pl === "meta" || pl === "ctrl" || pl === "control") mod = true;
    else if (pl === "alt" || pl === "option") alt = true;
    else if (pl === "shift") shift = true;
    else key = p.toLowerCase();
  }
  return key ? { mod, alt, shift, key } : null;
}

function hotkeyMatches(e: KeyboardEvent, h: { mod: boolean; alt: boolean; shift: boolean; key: string }): boolean {
  const modPressed = isMac() ? e.metaKey : e.ctrlKey;
  if (h.mod !== modPressed) return false;
  if (h.alt !== e.altKey) return false;
  if (h.shift !== e.shiftKey) return false;
  return e.key.toLowerCase() === h.key;
}

export function useQuickTemplates(opts: UseQuickTemplatesOpts): UseQuickTemplates {
  const { send, confirmRequired, focused, templatesBridge } = opts;

  const templates = ref<readonly QuickTemplate[]>([]);
  const templatesHidden = ref(false);
  const pendingTemplateConfirm = ref<QuickTemplate | null>(null);

  const templateConfirmOpen = computed(() => pendingTemplateConfirm.value !== null);

  function sendTemplate(tpl: QuickTemplate): void {
    // Send the text and Enter as two separate writes one tick apart. Codex
    // (and other raw-mode TUIs) treats a bundled "text\r" payload as a paste
    // — the trailing CR becomes a literal newline in the prompt instead of
    // submitting. Same fix landed on the legacy quickInput plugin in #63
    // before it was removed; the regression slipped in with the
    // QuickTemplate rewrite and was re-fixed in #129. See
    // [[feedback_template_send_split_cr]] — do not "simplify" this.
    send(tpl.text);
    window.setTimeout(() => send("\r"), 16);
  }

  function requestTemplateSend(tpl: QuickTemplate): void {
    if (!confirmRequired.value) {
      sendTemplate(tpl);
      return;
    }
    pendingTemplateConfirm.value = tpl;
  }

  function cancelTemplateSend(): void {
    pendingTemplateConfirm.value = null;
  }

  function confirmTemplateSend(): void {
    const tpl = pendingTemplateConfirm.value;
    pendingTemplateConfirm.value = null;
    if (!tpl) return;
    sendTemplate(tpl);
  }

  function onTemplateHotkey(e: KeyboardEvent): void {
    if (!focused()) return;
    for (const tpl of templates.value) {
      const h = parseHotkey(tpl.hotkey || "");
      if (h && hotkeyMatches(e, h)) {
        e.preventDefault();
        e.stopPropagation();
        sendTemplate(tpl);
        return;
      }
    }
  }

  function onTemplateBarWheel(e: WheelEvent): void {
    const el = e.currentTarget as HTMLElement | null;
    if (!el) return;
    // Trackpads on a one-row strip often only emit deltaY — folding it into
    // scrollLeft gives users a working scroll without reaching for shift.
    // Listener stays passive (no preventDefault) so xterm's own mouse
    // pipeline downstream isn't touched.
    const delta = e.deltaY !== 0 ? e.deltaY : e.deltaX;
    if (delta === 0) return;
    el.scrollLeft += delta;
  }

  async function reload(): Promise<void> {
    const [nextTemplates, nextHidden] = await Promise.all([
      effectiveTemplates(templatesBridge),
      templatesBridge.loadHidden(),
    ]);
    templates.value = nextTemplates;
    templatesHidden.value = nextHidden;
  }

  return {
    templates,
    templatesHidden,
    pendingTemplateConfirm,
    templateConfirmOpen,
    sendTemplate,
    requestTemplateSend,
    cancelTemplateSend,
    confirmTemplateSend,
    onTemplateHotkey,
    onTemplateBarWheel,
    reload,
  };
}
