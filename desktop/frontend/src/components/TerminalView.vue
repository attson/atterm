<script lang="ts" setup>
import { computed, inject, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Terminal } from "xterm";
import type { ITheme } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import { WebglAddon } from "xterm-addon-webgl";
import { SessionConnection, type Status } from "../lib/connection";
import type { Endpoint } from "../lib/api";
import { formatReplayProgress, progressPercent, type ReplayProgress } from "../lib/replayProgress";
import { copyTerminalSelection, isTerminalCopyShortcut } from "../lib/terminalCopy";
import { TERMINAL_FONT_FAMILY } from "../lib/terminalFont";
import { shouldNotify } from "../lib/terminalBell";
import {
  CommandTracker,
  shouldNotifyCommand,
  formatElapsed,
} from "../lib/commandFinish";
import {
  canSendSelection,
  clampContextMenuPosition,
  isPasteAllowed,
  prepareSendPayload,
} from "../lib/terminalContextMenu";
import { pasteFromClipboard } from "../lib/terminalPaste";
import { stripC1Controls } from "../lib/stripC1Controls";
import { createFocusReportCoalescer, type FocusReportCoalescer } from "../lib/focusReportCoalescer";
import { installModifierScrollGuard } from "../lib/terminalKeyGuard";
import { broadcastCommandFinished, getHostInfo, getUserHomeDir, getWebglRendererEnabled, showNotification } from "../lib/api";
import { useTerminalLinkProvider } from "../composables/useTerminalLinkProvider";
import { cellInLink, detectLinks, mapBufferLineCells, normalizeForOpen, type LinkMatch } from "../lib/terminalLinks";
import { cellCoordsAt } from "../lib/terminalCellCoords";
import { collectContextMenuItems } from "../plugins/contextMenuItems";
import { descriptorsForSlot } from "../plugins/registry";
import { usePluginConfigStore } from "../plugins/configStore";
import type { ContextMenuPlugin, MenuItem, PluginContext } from "../plugins/types";
import { useI18n } from "../i18n/useI18n";
import { effectiveTemplates, type QuickTemplate } from "../lib/templates";
import { usePlatform } from "../platform";

const props = withDefaults(
  defineProps<{
    endpoint: Endpoint;
    sessionId: string;
    active?: boolean;
    focused?: boolean;
    avoidTopRightBadge?: boolean;
    sessionLabel?: string;
    // The PTY's known size at the time of attach (from SessionInfo).
    // When this matches the local xterm's fit dimensions, we skip the
    // initial RESIZE so cross-attached remote shells don't see a
    // gratuitous SIGWINCH (which some prompt themes turn into a stray
    // '%' via PROMPT_EOL_MARK). Undefined → treat as unknown and
    // send the resize anyway (safe fallback).
    expectedCols?: number;
    expectedRows?: number;
    remotePermission?: string;
    theme: ITheme;
    commandNotifyThresholdSec?: number;
    isLocalSession?: boolean;
    // True while the user is dragging a pane splitter. FitAddon still runs
    // so xterm stays visually correct, but we skip the PTY RESIZE until the
    // drag ends — the child process gets one SIGWINCH on mouseup instead
    // of dozens during the drag.
    resizeSuspended?: boolean;
  }>(),
  { active: true, focused: false, avoidTopRightBadge: false, commandNotifyThresholdSec: 10, isLocalSession: true, resizeSuspended: false }
);

const emit = defineEmits<{
  (e: "toast", message: string): void;
}>();
const { t } = useI18n();

const termContainer = ref<HTMLDivElement | null>(null);
const status = ref<Status>("connecting");
const replayProgress = ref<ReplayProgress | null>(null);
const menuOpen = ref(false);
const menuX = ref(0);
const menuY = ref(0);
const menuHasSelection = ref(false);
const pasteBusy = ref(false);
const menuRef = ref<HTMLDivElement | null>(null);
// Driver state: true = our IN/RESIZE go through, FitAddon sizes xterm to the
// container. false = viewer: xterm.cols/rows locked to PTY's reported dims
// from META. Local panes are always the driver. Remote panes start as viewer
// so the overlay shows immediately — META corrects to driver if the relay
// confirms us. Without this, a restored remote pane that the relay can't
// auto-promote (e.g. uplink proxy sub left the session driverless) would
// look like driver mode (no overlay) yet have every IN frame dropped by the
// relay, stranding the user with no way to type and no hint to take control.
const isDriver = ref(props.isLocalSession ?? true);
const ptyCols = ref<number | null>(null);
const ptyRows = ref<number | null>(null);
// driverHostname is the human-readable name of whoever currently holds the
// driver role (broadcast via META.driver_client_name). Used in the viewer
// overlay's sub-line ("by <hostname>"). Empty when nobody or self drives.
const driverHostname = ref("");
// localHostname is this machine's hostname (from getHostInfo). Sent as
// client_name in ATTACH and CLAIM_DRIVER so other clients can label us.
const localHostname = ref("");

// Quick-action templates rendered as a row of buttons above the status bar.
// effectiveTemplates falls back to DEFAULT_TEMPLATES when persisted list
// is empty so the bar is never empty on a fresh install.
const templates = ref<readonly QuickTemplate[]>([]);
const templatesHidden = ref(false);
const platform = usePlatform();

let term: Terminal | null = null;
let fit: FitAddon | null = null;
let conn: SessionConnection | null = null;
// Coalesces spurious blur→refocus focus-report flaps so a stray `\x1b[O`
// doesn't cancel the child TUI's in-flight turn. See focusReportCoalescer.ts.
let focusCoalescer: FocusReportCoalescer | null = null;

// Map<sessionId, (text) => void> provided by App.vue. Plugins use it to
// reuse the active driver SessionConnection for input. Absent (undefined)
// when this TerminalView is rendered outside the plugin-aware App.
const pluginInputSenders = inject<Map<string, (text: string) => void> | null>(
  "atterm:pluginInputSenders",
  null,
);

// pluginContext is provided by App.vue (Task 14). Null when TerminalView is
// rendered outside the plugin-aware App (e.g. tests, standalone embedding).
const pluginContext = inject<PluginContext>("atterm:pluginContext", null as unknown as PluginContext);

// Menu items contributed by context-menu plugins. Populated on each right-click.
const pluginMenuItems = ref<MenuItem[]>([]);
const menuLinkHit = ref<LinkMatch | null>(null);

let resizeObserver: ResizeObserver | null = null;
let linkProviderDisposer: { dispose(): void } | null = null;
let cachedHomeDir = "";
let copyKeyTarget: HTMLDivElement | null = null;

const MENU_WIDTH = 150;
const MENU_HEIGHT = 150;

const menuCanPaste = computed(() => isPasteAllowed(status.value, props.remotePermission));
const menuCanSend = computed(() =>
  canSendSelection({
    hasSelection: menuHasSelection.value,
    status: status.value,
    permission: props.remotePermission,
    isDriver: isDriver.value,
  }),
);

function handleViewerKeydown(event: KeyboardEvent) {
  if (isDriver.value) return; // driver mode passes through
  // Only intercept bare space (no modifiers) so Cmd+C copy, arrow-key scroll,
  // and other existing shortcuts still work in viewer mode. disableStdin on
  // the terminal already blocks the IN forwarding path for other keys.
  if (event.key === " " && !event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey) {
    event.preventDefault();
    event.stopPropagation();
    conn?.claimDriver();
  }
}

function takeControl() {
  conn?.claimDriver();
}

async function handleCopyShortcut(e: KeyboardEvent) {
  if (!term || !isTerminalCopyShortcut(e)) return;
  e.preventDefault();
  e.stopPropagation();
  try {
    await copyTerminalSelection(term);
  } catch (err) {
    console.warn("[AT Term] failed to copy terminal selection", err);
  }
}

async function handleImagePaste(e: ClipboardEvent) {
  const item = Array.from(e.clipboardData?.items || []).find((i) => i.type.startsWith("image/"));
  if (!item) return;
  const file = item.getAsFile();
  if (!file) return;
  e.preventDefault();
  e.stopPropagation();
  try {
    await conn?.sendPasteImage(file, file.name || "clipboard-image");
  } catch (err) {
    console.warn("[AT Term] failed to paste terminal image", err);
  }
}

function closeContextMenu() {
  menuOpen.value = false;
  pluginMenuItems.value = [];
  menuLinkHit.value = null;
}

async function openContextMenu(e: MouseEvent) {
  if (!term) return;
  const pos = clampContextMenuPosition(
    e.clientX,
    e.clientY,
    MENU_WIDTH,
    MENU_HEIGHT,
    window.innerWidth,
    window.innerHeight,
  );
  const selection = term.getSelection();
  menuHasSelection.value = !!selection;
  menuX.value = pos.left;
  menuY.value = pos.top;
  pluginMenuItems.value = [];
  menuLinkHit.value = computeLinkHit(e);
  menuOpen.value = true;

  // Collect context-menu plugin items and append after the menu is shown.
  if (pluginContext) {
    const cfgStore = usePluginConfigStore();
    const enabledPlugins: ContextMenuPlugin[] = [];
    for (const d of descriptorsForSlot("context-menu")) {
      if (!cfgStore.isPluginEnabled(d.id)) continue;
      try {
        const mod = await d.load();
        enabledPlugins.push((mod as { default: ContextMenuPlugin }).default);
      } catch (err) {
        console.error(`[AT Term] failed to load context-menu plugin ${d.id}`, err);
      }
    }
    pluginMenuItems.value = await collectContextMenuItems(enabledPlugins, pluginContext, selection);
  }
}

function onDocumentMouseDown(e: MouseEvent) {
  if (!menuOpen.value) return;
  const target = e.target;
  if (target instanceof Node && menuRef.value?.contains(target)) return;
  closeContextMenu();
}

function onDocumentKeyDown(e: KeyboardEvent) {
  if (e.key === "Escape") closeContextMenu();
}

// computeLinkHit converts a right-click MouseEvent into the LinkMatch that
// covers the clicked cell, or null when the click isn't on any detected link.
// Reuses detectLinks so the menu agrees with what the hover provider drew.
function computeLinkHit(e: MouseEvent): LinkMatch | null {
  if (!term) return null;
  const viewport = termContainer.value;
  if (!viewport) return null;
  const hit = cellCoordsAt(e.clientX, e.clientY, term, viewport);
  if (!hit) return null;
  const line = term.buffer.active.getLine(hit.row);
  if (!line) return null;
  // hit.col is a cell column; map detected string-index spans to cell columns
  // so wide glyphs (CJK, emoji) before the link don't throw off hit-testing.
  const { text, cellStart } = mapBufferLineCells(line, term.cols);
  return detectLinks(text).find((m) => cellInLink(hit.col, m, cellStart)) ?? null;
}

async function onMenuOpenLink() {
  const hit = menuLinkHit.value;
  closeContextMenu();
  if (!hit) return;
  const url = normalizeForOpen(hit, cachedHomeDir);
  if (!url) {
    emit("toast", t("terminal.link.openFailedNoHome"));
    return;
  }
  try {
    await platform.system.openExternalURL(url);
  } catch (err) {
    console.warn("[AT Term] open link failed", err);
    emit("toast", t("terminal.link.openFailed"));
  }
}

async function onMenuCopyLink() {
  const hit = menuLinkHit.value;
  closeContextMenu();
  if (!hit) return;
  try {
    await navigator.clipboard.writeText(hit.text);
  } catch (err) {
    console.warn("[AT Term] copy link failed", err);
    emit("toast", t("terminal.copyFailed"));
  }
}

async function onMenuCopy() {
  closeContextMenu();
  if (!term) return;
  try {
    await copyTerminalSelection(term);
  } catch (err) {
    console.warn("[AT Term] failed to copy terminal selection", err);
    emit("toast", t("terminal.copyFailed"));
  }
}

async function onMenuPaste() {
  if (!term || !conn || pasteBusy.value) return;
  pasteBusy.value = true;
  console.info("[AT Term] terminal menu paste requested", {
    status: status.value,
    remotePermission: props.remotePermission ?? "full",
  });
  try {
    const result = await pasteFromClipboard({
      term,
      conn,
      status: status.value,
      remotePermission: props.remotePermission,
    });
    if (!result.ok && (result.reasonKey || result.reason)) {
      emit("toast", result.reasonKey ? t(result.reasonKey) : result.reason!);
    }
  } catch (err: any) {
    console.warn("[AT Term] failed to paste from terminal menu", err);
    emit("toast", err?.message ?? t("terminal.pasteFailed"));
  } finally {
    pasteBusy.value = false;
    closeContextMenu();
  }
}

function applyViewerSize() {
  if (!term) return;
  if (isDriver.value) {
    // Driver path: re-engage FitAddon (term.onResize → sendResize fires from here).
    safeFit();
    return;
  }
  const cols = ptyCols.value;
  const rows = ptyRows.value;
  if (typeof cols === "number" && typeof rows === "number" && cols > 0 && rows > 0) {
    if (term.cols !== cols || term.rows !== rows) {
      term.resize(cols, rows);
    }
  }
}

function onMenuSend() {
  closeContextMenu();
  if (!term || !conn) return;
  const payload = prepareSendPayload(term.getSelection());
  if (payload === null) return;
  conn.sendInput(payload);
}

function onMenuClear() {
  closeContextMenu();
  if (!term) return;
  term.clear();
}

function safeFit() {
  // In viewer mode, FitAddon must not size the terminal — the PTY dims drive
  // term.cols/rows via applyViewerSize. Skip the fit entirely.
  if (!isDriver.value) return;
  if (!fit || !termContainer.value) return;
  // fit() crashes with NaN dims when the container is display:none. Guard.
  const rect = termContainer.value.getBoundingClientRect();
  if (rect.width < 2 || rect.height < 2) return;
  try {
    fit.fit();
  } catch {
    /* ignore initial-mount races */
  }
  // Diagnostic: terminal sometimes rendered at default 24 rows when
  // FitAddon's proposeDimensions saw "auto" on the parent's computed
  // height during a layout race. Surface to the console so we can spot it.
  if (term && termContainer.value) {
    const r = termContainer.value.getBoundingClientRect();
    if (term.rows < Math.floor(r.height / 30)) {
      // Heuristic: if cell can fit > N rows but term has way fewer, fit failed.
      console.warn(
        "[AT Term] suspicious term size after fit",
        { containerW: r.width, containerH: r.height, cols: term.cols, rows: term.rows },
      );
    }
  }
}

function scrollToBottomAfterWriteQueue() {
  const current = term;
  if (!current) return;
  current.write("", () => {
    if (term === current) current.scrollToBottom();
  });
}

async function ensureTerm() {
  if (term) return;
  term = new Terminal({
    fontFamily: TERMINAL_FONT_FAMILY,
    fontSize: 13,
    cursorBlink: true,
    scrollback: 20000,
    theme: props.theme,
    convertEol: false,
    allowProposedApi: true,
  });
  fit = new FitAddon();
  term.loadAddon(fit);
  term.open(termContainer.value!);
  // Keep a bare Ctrl/⌘ press (e.g. to mod-click a link in the scrollback) from
  // scrolling the viewport to the prompt. CJK IMEs deliver such keydowns as
  // keyCode 229, which xterm 5.3 otherwise answers with scrollToBottom().
  installModifierScrollGuard(term);
  // GPU-rasterized renderer eliminates the cell-ghosting the DOM renderer
  // shows on light terminal themes (most visible when remote TUIs like
  // Claude Code repaint dense RGB diff blocks). Load after open() so the
  // WebGL context attaches to the live <canvas>; fall back to DOM on
  // construction failure or runtime context loss.
  //
  // Disabled by default on Linux because NVIDIA proprietary + X11 +
  // WebKitGTK schedules the cursor / last-cell paint a frame or two late,
  // which surfaces as visible typing lag even though CPU stays idle (#48).
  // Users on AMD/Intel or who run light-themed dense-TUI workloads can
  // re-enable WebGL from Settings.
  let webglEnabled = true;
  try {
    webglEnabled = await getWebglRendererEnabled();
  } catch {
    // Wails binding unavailable (e.g. test or pre-init): keep WebGL off
    // so the bug-affected platform stays smooth by default. The user can
    // opt back in from Settings once the binding is reachable.
    webglEnabled = false;
  }
  if (webglEnabled) {
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => webgl.dispose());
      term.loadAddon(webgl);
    } catch (err) {
      console.warn("[AT Term] WebGL renderer unavailable, falling back to DOM", err);
    }
  }
  const keyTarget = termContainer.value!;
  copyKeyTarget = keyTarget;
  keyTarget.addEventListener("keydown", handleCopyShortcut, { capture: true });
  keyTarget.addEventListener("keydown", handleViewerKeydown, { capture: true });
  keyTarget.addEventListener("paste", handleImagePaste, { capture: true });
  safeFit();
  focusCoalescer = createFocusReportCoalescer({ send: (d) => conn?.sendInput(d) });
  term.onData((data) => {
    const { cleaned, dropped } = stripC1Controls(data);
    if (dropped.length > 0) {
      console.warn("[AT Term] dropped C1 control chars from terminal input", {
        droppedCodepoints: dropped.map((cp) => "U+" + cp.toString(16).toUpperCase().padStart(4, "0")),
        originalLength: data.length,
        cleanedLength: cleaned.length,
      });
    }
    if (cleaned) focusCoalescer?.handle(cleaned);
  });
  term.onResize(({ cols, rows }) => {
    if (!isDriver.value) return; // viewer's local resize is FitAddon-suppressed anyway
    if (props.resizeSuspended) return; // mid pane-splitter drag: defer the PTY RESIZE until mouseup
    conn?.sendResize(cols, rows);
  });

  let lastBellAt = 0;
  term.onBell(() => {
    const focused = typeof document !== "undefined" && document.hasFocus();
    if (!shouldNotify(Date.now(), lastBellAt, focused)) return;
    lastBellAt = Date.now();
    void showNotification("AT Term", t("terminal.bellNotification", { session: props.sessionLabel || "session" }));
  });

  const cmdTracker = new CommandTracker();
  try {
    term.parser.registerOscHandler(133, (payload) => {
      const ev = cmdTracker.onOsc133(payload, Date.now());
      if (!ev) return false;
      const focused = typeof document !== "undefined" && document.hasFocus();
      const passed = shouldNotifyCommand(ev, {
        focused,
        thresholdSec: props.commandNotifyThresholdSec ?? 10,
        isLocal: props.isLocalSession ?? true,
      });
      if (!passed) return false;
      void showNotification(
        "AT Term",
        t("terminal.commandFinishedNotification", {
          exitCode: ev.exitCode,
          elapsed: formatElapsed(ev.elapsedMs),
          session: props.sessionLabel || "session",
        }),
      );
      void broadcastCommandFinished(
        props.sessionId,
        ev.exitCode,
        ev.elapsedMs,
        props.sessionLabel || "session",
      );
      return false;
    });
  } catch (err) {
    console.warn("[AT Term] OSC 133 handler registration failed", err);
  }

  try {
    cachedHomeDir = await getUserHomeDir();
  } catch {
    cachedHomeDir = "";
  }
  linkProviderDisposer = useTerminalLinkProvider({
    term,
    isMac,
    getHomeDir: () => cachedHomeDir,
    openURL: (u) => platform.system.openExternalURL(u),
    onError: (key) => emit("toast", t(key)),
  });

  resizeObserver = new ResizeObserver(() => safeFit());
  resizeObserver.observe(termContainer.value!);
}

function startConnection() {
  if (!term) return;
  conn = new SessionConnection(
    props.endpoint,
    props.sessionId,
    {
      onOutput: (data) => term?.write(data),
      onClose: (info) => {
        term?.write(
          `\r\n\x1b[33m${t("terminal.sessionEndedBanner", { exitCode: info.exit_code })}\x1b[0m\r\n`
        );
      },
      onStatus: (s) => {
        status.value = s;
      },
      onReplayProgress: (progress) => {
        replayProgress.value = progress.phase === "end" ? null : progress;
        if (progress.phase === "end") scrollToBottomAfterWriteQueue();
      },
      onMeta: (meta) => {
        if (typeof meta?.cols === "number") ptyCols.value = meta.cols;
        if (typeof meta?.rows === "number") ptyRows.value = meta.rows;
        applyViewerSize();
      },
      onDriverChange: (_driverID, isMe, driverName) => {
        const wasDriver = isDriver.value;
        isDriver.value = isMe;
        driverHostname.value = isMe ? "" : driverName;
        if (term) term.options.disableStdin = !isMe;
        applyViewerSize();
        if (wasDriver !== isMe) {
          emit("toast", isMe ? t("terminal.driverNow") : t("terminal.viewerNow"));
        }
      },
    },
    { clientName: localHostname.value, remote: !props.isLocalSession }
  );
  conn.attach();
  // Register a driver-side input sender for this session so plugins
  // (Quick Input) can pipe text through this same driver connection.
  // A fresh SessionConnection would attach as a viewer and have its
  // IN frames dropped by the relay.
  pluginInputSenders?.set(props.sessionId, (text: string) => conn?.sendInput(text));
  // Skip the no-op RESIZE if our fit landed on the same size the relay
  // already knows about. Net effect: locally-spawned shells (PTY born at
  // predicted dims) and cross-attached shells whose owner happens to be
  // the same size get zero startup SIGWINCH. Mismatched sizes still send,
  // accepting the SIGWINCH cost — that's the cross-client cost iTerm
  // doesn't incur because it doesn't have this attach-existing model.
  if (
    term &&
    (props.expectedCols !== term.cols || props.expectedRows !== term.rows)
  ) {
    conn.sendResize(term.cols, term.rows);
  }
}

function sendTemplate(tpl: QuickTemplate) {
  // Send the text and Enter as two separate writes one tick apart. Codex (and
  // other raw-mode TUIs) treats a bundled "text\r" payload as a paste — the
  // trailing CR becomes a literal newline in the prompt instead of submitting.
  // Same fix landed on the legacy quickInput plugin in #63 before it was
  // removed; the regression slipped in with the QuickTemplate rewrite.
  conn?.sendInput(tpl.text);
  const c = conn;
  window.setTimeout(() => c?.sendInput("\r"), 16);
}

// Wheel-to-horizontal-scroll for the template bar. Trackpads emit deltaX
// natively when scrolled sideways, but a vertical mouse wheel — the common
// case when the cursor is hovering over a one-row strip — only emits deltaY.
// Folding deltaY into scrollLeft gives users a working scroll without
// reaching for shift. Listener is .passive (we don't preventDefault), so
// xterm's keyboard/mouse pipeline downstream is untouched.
function onTemplateBarWheel(e: WheelEvent) {
  const el = e.currentTarget as HTMLElement | null;
  if (!el) return;
  const delta = e.deltaY !== 0 ? e.deltaY : e.deltaX;
  if (delta === 0) return;
  el.scrollLeft += delta;
}

// Re-read the persisted template list + hidden flag. Wired to the
// 'quickTemplates:changed' event the Settings page emits so an open
// terminal updates immediately, without a remount.
async function reloadTemplates() {
  templates.value = await effectiveTemplates(platform.templates);
  templatesHidden.value = await platform.templates.loadHidden();
}

// parseHotkey turns a user-typed string like "Mod+1", "Alt+Shift+P", "Mod+/"
// into modifier flags + a single key. "Mod" maps to ⌘ on macOS, Ctrl elsewhere
// (matches the shortcuts system convention). Returns null for unparseable input.
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

const isMac = typeof navigator !== "undefined" && /Mac/i.test(navigator.platform);

function hotkeyMatches(e: KeyboardEvent, h: { mod: boolean; alt: boolean; shift: boolean; key: string }): boolean {
  const modPressed = isMac ? e.metaKey : e.ctrlKey;
  if (h.mod !== modPressed) return false;
  if (h.alt !== e.altKey) return false;
  if (h.shift !== e.shiftKey) return false;
  return e.key.toLowerCase() === h.key;
}

function onTemplateHotkey(e: KeyboardEvent) {
  // Only the focused pane responds, so multiple mounted TerminalViews don't
  // all fire on the same key press.
  if (!props.focused) return;
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

let templatesOff: (() => void) | null = null;

onMounted(async () => {
  await ensureTerm();
  // Resolve the local hostname before opening the WS so the very first ATTACH
  // carries the correct client_name. Failure (e.g. Wails not ready in tests
  // or a future browser-only build) falls back to the default in connection.ts.
  try {
    const info = await getHostInfo();
    if (info?.host) localHostname.value = info.host;
  } catch {
    /* fall back to default */
  }
  startConnection();
  reloadTemplates();
  templatesOff = platform.events.on("quickTemplates:changed", reloadTemplates);
  document.addEventListener("mousedown", onDocumentMouseDown);
  document.addEventListener("keydown", onDocumentKeyDown);
  // Hotkey handler is a capture-phase listener so it preempts xterm's own
  // keyboard input — the user expects Alt+1 to fire the template, not type "1".
  document.addEventListener("keydown", onTemplateHotkey, true);
  // Re-fit on the next two animation frames. Layout for the cell may not
  // be fully resolved at term.open() time — getComputedStyle('height') on
  // the absolute+inset:0 .term sometimes still reads "auto" right after
  // mount, which makes FitAddon return NaN and bail. By the time we get a
  // second rAF the layout has definitely settled.
  requestAnimationFrame(() => {
    safeFit();
    requestAnimationFrame(() => safeFit());
  });
});

onBeforeUnmount(() => {
  // Drop every external callback that could re-enter the term BEFORE we
  // touch conn / term. A queued ResizeObserver entry or a stray document
  // listener firing in the same tick used to call safeFit() → fit.fit() on
  // a half-disposed term, and the renderer race in xterm's RenderService
  // would throw synchronously back into Vue's unmount hook.
  resizeObserver?.disconnect();
  resizeObserver = null;
  templatesOff?.();
  templatesOff = null;
  document.removeEventListener("mousedown", onDocumentMouseDown);
  document.removeEventListener("keydown", onDocumentKeyDown);
  document.removeEventListener("keydown", onTemplateHotkey, true);
  copyKeyTarget?.removeEventListener("keydown", handleCopyShortcut, { capture: true } as EventListenerOptions);
  copyKeyTarget?.removeEventListener("keydown", handleViewerKeydown, { capture: true } as EventListenerOptions);
  copyKeyTarget?.removeEventListener("paste", handleImagePaste, { capture: true } as EventListenerOptions);
  copyKeyTarget = null;
  pluginInputSenders?.delete(props.sessionId);
  focusCoalescer?.dispose();
  focusCoalescer = null;
  linkProviderDisposer?.dispose();
  linkProviderDisposer = null;
  conn?.detach();
  conn = null;
  // term.dispose() can throw from inside xterm: RenderService schedules a
  // redraw via Promise.resolve().then(); if dispose interleaves with that
  // tick, `this._renderer.value.onRequestRedraw` is undefined and throws.
  // Letting that bubble out of beforeUnmount crashes Vue's update batch —
  // every subsequent component update then errors on `null.emitsOptions`,
  // local tabs go black, settings stops opening. Swallow it.
  try {
    term?.dispose();
  } catch (e) {
    console.warn("[AT Term] xterm dispose race (safe to ignore):", e);
  }
  term = null;
  fit = null;
});

watch(
  () => props.active,
  (isActive) => {
    if (isActive) {
      // Tab just gained focus — recompute size and let xterm refocus its
      // input so keystrokes go to this term instead of the body.
      nextTick(() => {
        safeFit();
        term?.focus();
      });
    } else {
      closeContextMenu();
    }
  }
);

watch(
  () => props.theme,
  (theme) => {
    if (term) term.options.theme = theme;
  }
);

watch(status, (nextStatus) => {
  if (nextStatus !== "attached") closeContextMenu();
});

// Falling edge of resize-suspended: pane splitter drag just ended. We
// suppressed every onResize-driven sendResize during the drag; emit one
// final RESIZE now so the PTY learns the new cols/rows. nextTick lets
// the post-drop layout settle before we read term.cols/rows.
watch(
  () => props.resizeSuspended,
  (next, prev) => {
    if (prev && !next) {
      nextTick(() => {
        if (term && conn) conn.sendResize(term.cols, term.rows);
      });
    }
  },
);
</script>

<template>
  <div class="term-view" :class="{ focused }">
    <div ref="termContainer" class="term" @contextmenu.prevent="openContextMenu"></div>
    <div
      v-if="active && (status !== 'attached' || replayProgress)"
      class="overlay"
      :class="{ 'avoid-top-right-badge': avoidTopRightBadge }"
    >
      <template v-if="replayProgress">
        <span>{{ formatReplayProgress(replayProgress) }}</span>
        <div class="progress-track" aria-hidden="true">
          <div class="progress-fill" :style="{ width: `${progressPercent(replayProgress)}%` }"></div>
        </div>
      </template>
      <span v-else-if="status === 'connecting'">{{ t("terminal.connecting") }}</span>
      <span v-else-if="status === 'reconnecting'" class="warn">{{ t("terminal.reconnecting") }}</span>
      <span v-else-if="status === 'ended'" class="dim">{{ t("terminal.ended") }}</span>
      <span v-else-if="status === 'error'" class="bad">{{ t("terminal.connectionError") }}</span>
    </div>
    <div v-if="!isDriver" class="viewer-overlay" aria-live="polite">
      <div class="viewer-overlay-card">
        <div class="viewer-overlay-title">{{ t("terminal.remoteHasTakenControl") }}</div>
        <div v-if="driverHostname" class="viewer-overlay-host">{{ t("terminal.byHost", { host: driverHostname }) }}</div>
        <div class="viewer-overlay-hint">{{ t("terminal.pressSpaceToTakeBack") }}</div>
        <button class="viewer-overlay-btn" data-testid="take-control" @click="takeControl">{{ t("terminal.takeControl") }}</button>
      </div>
    </div>
    <Teleport to="body">
      <div
        v-if="menuOpen"
        ref="menuRef"
        class="term-context-menu"
        :style="{ left: `${menuX}px`, top: `${menuY}px` }"
        @mousedown.stop
        @click.stop
      >
        <button v-if="menuLinkHit" class="term-context-item" @click="onMenuOpenLink">{{ t("terminal.contextMenu.openLink") }}</button>
        <button v-if="menuLinkHit" class="term-context-item" @click="onMenuCopyLink">{{ t("terminal.contextMenu.copyLink") }}</button>
        <button class="term-context-item" :disabled="!menuHasSelection" @click="onMenuCopy">{{ t("common.copy") }}</button>
        <button class="term-context-item" :disabled="!menuCanPaste || pasteBusy" @click="onMenuPaste">{{ t("common.paste") }}</button>
        <button class="term-context-item" :disabled="!menuCanSend" @click="onMenuSend">{{ t("terminal.sendSelection") }}</button>
        <button class="term-context-item" @click="onMenuClear">{{ t("terminal.clearBuffer") }}</button>
        <button
          v-for="item in pluginMenuItems"
          :key="item.id"
          class="term-context-item"
          :disabled="item.disabled"
          @click="item.onClick(); closeContextMenu()"
        >{{ item.label }}</button>
      </div>
    </Teleport>
    <div
      v-if="!templatesHidden"
      class="template-bar"
      data-testid="template-bar"
      @wheel.passive="onTemplateBarWheel"
    >
      <button
        v-for="tpl in templates"
        :key="tpl.id"
        class="template-btn"
        :data-testid="`template-btn-${tpl.id}`"
        :title="tpl.hotkey || ''"
        @click="sendTemplate(tpl)"
      >{{ tpl.label }}</button>
    </div>
  </div>
</template>

<style scoped>
.term-view {
  position: absolute;
  inset: 0;
  background: var(--terminal-bg);
  overflow: hidden;
}
.term {
  position: absolute;
  left: 0;
  right: 0;
  top: 0;
  bottom: 30px;
}
.template-bar {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 30px;
  display: flex;
  align-items: center;
  gap: 6px;
  overflow-x: auto;
  padding: 2px 8px;
  border-top: 1px solid var(--border);
  background: var(--panel);
  z-index: 4;
  /* Hide the WebKit scrollbar gutter — the row scrolls with wheel/trackpad
     and individual buttons are reachable via keyboard; the scrollbar itself
     just steals 12-15px of vertical space we'd rather give to the terminal. */
  scrollbar-width: none;
}
.template-bar::-webkit-scrollbar { display: none; }
.template-btn {
  flex: 0 0 auto;
  height: 22px;
  padding: 0 10px;
  border-radius: 6px;
  background: var(--bg);
  border: 1px solid var(--border);
  color: var(--fg);
  font-size: 0.76rem;
  font-family: var(--font-mono);
  cursor: pointer;
}
.template-btn:hover {
  border-color: var(--accent);
  color: var(--accent);
}
.term :deep(.xterm) {
  /* FitAddon subtracts padding from the xterm element, not this host. */
  padding: 6px 8px;
}
.overlay {
  position: absolute;
  top: 8px;
  right: 12px;
  background: var(--terminal-overlay);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 4px 10px;
  font-size: 12px;
  color: var(--fg-dim);
  pointer-events: none;
}
.overlay.avoid-top-right-badge {
  top: 34px;
}
.viewer-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
  user-select: none;
  z-index: 5;
}
.viewer-overlay-card {
  background: var(--terminal-overlay);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 14px 22px;
  text-align: center;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.35);
}
.viewer-overlay-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--fg);
}
.viewer-overlay-host {
  margin-top: 4px;
  font-size: 12px;
  color: var(--fg);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  opacity: 0.85;
}
.viewer-overlay-hint {
  margin-top: 6px;
  font-size: 12px;
  color: var(--fg-dim);
}
.viewer-overlay-btn {
  margin-top: 8px;
  padding: 6px 14px;
  border: none;
  border-radius: 8px;
  background: #3b82f6;
  color: #fff;
  font-weight: 600;
  cursor: pointer;
}
.overlay .warn { color: #d29922; }
.overlay .bad { color: var(--bad); }
.overlay .dim { color: var(--fg-dim); }
.progress-track {
  width: 190px;
  height: 4px;
  margin-top: 6px;
  border-radius: 999px;
  background: rgba(148, 163, 184, 0.24);
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, var(--accent), #4ade80);
  transition: width 120ms ease;
}
.term-context-menu {
  position: fixed;
  min-width: 150px;
  padding: 6px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: rgba(13, 17, 23, 0.96);
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.35);
  z-index: 1000;
}
.term-context-item {
  width: 100%;
  border: none;
  background: transparent;
  color: var(--fg);
  text-align: left;
  padding: 8px 10px;
  border-radius: 6px;
  cursor: pointer;
}
.term-context-item:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.06);
}
.term-context-item:disabled {
  color: var(--fg-dim);
  cursor: default;
}
.term-view.focused {
  box-shadow: inset 0 0 0 1px var(--accent);
}
</style>
