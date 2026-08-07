<script lang="ts" setup>
import { computed, inject, markRaw, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Terminal } from "xterm";
import type { ITheme } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import { WebglAddon } from "xterm-addon-webgl";
import { Camera, CameraResultType, CameraSource } from "@capacitor/camera";
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
  effectiveRemotePermission,
  isPasteAllowed,
  prepareSendPayload,
} from "../lib/terminalContextMenu";
import { isCtrlVKeydownPaste, pasteFromClipboard } from "../lib/terminalPaste";
import { pasteImageBus } from "../lib/pasteImageBus";
import { pasteFileBus } from "../lib/pasteFileBus";
import { dispatchPastedFile } from "../lib/pasteFileDispatch";
import { GetPasteboardFileURLs } from "../../wailsjs/go/main/App";
import { stripC1Controls } from "../lib/stripC1Controls";
import { isMac } from "../lib/modKey";
import { createFocusReportCoalescer, type FocusReportCoalescer } from "../lib/focusReportCoalescer";
import { installModifierScrollGuard } from "../lib/terminalKeyGuard";
import { broadcastCommandFinished, getHostInfo, getUserHomeDir, getWebglRendererEnabled, showNotification } from "../lib/api";
import { useTerminalLinkProvider } from "../composables/useTerminalLinkProvider";
import { cellInLink, detectLinks, shouldActivateLink, mapBufferLineCells, normalizeForOpen, type LinkMatch } from "../lib/terminalLinks";
import { cellCoordsAt, readXtermCellSize } from "../lib/terminalCellCoords";
import { wordBoundaryAt } from "../lib/wordBoundary";
import { collectContextMenuItems } from "../plugins/contextMenuItems";
import { descriptorsForSlot } from "../plugins/registry";
import { usePluginConfigStore } from "../plugins/configStore";
import type { ContextMenuPlugin, MenuItem, PluginContext } from "../plugins/types";
import { useI18n } from "../i18n/useI18n";
import { type QuickTemplate } from "../lib/templates";
import { effectiveAuxKeys, type AuxKey } from "../lib/auxKeys";
import {
  installTouchDebugDump,
  isTouchDebugEnabled,
  logTouch,
  readTouchDebugFlag,
  setTouchDebugEnabled,
} from "../lib/touchScrollDebug";
import { useQuickTemplates } from "../composables/useQuickTemplates";
import { usePlatform } from "../platform";
import { useFileRevealStore } from "../plugins/fileExplorer/fileReveal";
import TerminalSelectionPopover from "./TerminalSelectionPopover.vue";

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
const filePicker = ref<HTMLInputElement | null>(null);
const imagePicker = ref<HTMLInputElement | null>(null);
const pasteConfirmOpen = ref(false);
const pasteConfirmText = ref("");
// Driver state: true = our IN/RESIZE go through, FitAddon sizes xterm to the
// container. false = viewer: xterm.cols/rows locked to PTY's reported dims
// from META. Local panes are always the driver. Remote panes start as viewer
// so the overlay shows immediately — META corrects to driver if the relay
// confirms us. Without this, a restored remote pane that the relay can't
// auto-promote (e.g. uplink proxy sub left the session driverless) would
// look like driver mode (no overlay) yet have every IN frame dropped by the
// relay, stranding the user with no way to type and no hint to take control.
const isDriver = ref(props.isLocalSession ?? true);
// Ref to the viewer-overlay's "Take control" button. Focused whenever the
// overlay appears in an active pane so the browser's native Space/Enter →
// button-click handles takeover. Without this, viewer mode blurs the IME
// textarea (see syncTerminalInputMode) and focus falls to document.body —
// the .term-container keydown listener never fires and Space appears dead.
const takeControlBtnRef = ref<HTMLButtonElement | null>(null);
const ptyCols = ref<number | null>(null);
const ptyRows = ref<number | null>(null);
// driverHostname is the human-readable name of whoever currently holds the
// driver role (broadcast via META.driver_client_name). Used in the viewer
// overlay's sub-line ("by <hostname>"). Empty when nobody or self drives.
const driverHostname = ref("");
// localHostname is this machine's hostname (from getHostInfo). Sent as
// client_name in ATTACH and CLAIM_DRIVER so other clients can label us.
const localHostname = ref("");

const auxKeys = ref<readonly AuxKey[]>([]);
const keyboardWanted = ref(false);
const imeFocused = ref(false);
const softKeyboardOpening = ref(false);
const softKeyboardOpen = computed(() => imeFocused.value || softKeyboardOpening.value);
const softKeyboardCollapsed = computed(() => !softKeyboardOpen.value);
type SelectionMode = "idle" | "pressing" | "selecting" | "dragging";
const selectionMode = ref<SelectionMode>("idle");
const selectionPopover = ref({
  visible: false,
  x: 0,
  y: 0,
  arrowDir: "down" as "down" | "up",
  copying: false,
  sending: false,
});
const platform = usePlatform();
const fileRevealStore = useFileRevealStore();

let term: Terminal | null = null;
let fit: FitAddon | null = null;
let conn: SessionConnection | null = null;
let pluginInputSender: ((text: string) => void) | null = null;
let isAlive = true;
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

const pluginSessionConnections = inject<Map<string, SessionConnection> | null>(
  "atterm:pluginSessionConnections",
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
// Press origin of the most recent left mousedown on the terminal grid, used to
// distinguish a plain link click from the end of a drag-select in mouseup.
let linkClickDownPos: { x: number; y: number } | null = null;
let copyKeyTarget: HTMLDivElement | null = null;
let imeInputTarget: HTMLTextAreaElement | null = null;
let terminalTouchViewport: HTMLElement | null = null;
let selectionPressTimer: ReturnType<typeof setTimeout> | null = null;
let selectionPressAnchor: { x: number; y: number } | null = null;
let selectionDragAnchor: { start: number; end: number; row: number } | null = null;
let selectionEdgeScrollTimer: ReturnType<typeof setInterval> | null = null;
let selectionEdgeScrollDir: -1 | 1 | 0 = 0;
let lastBlockedTerminalTouchAt = 0;
let lastKeyboardControlPointerAt = 0;
let lastSoftKeyboardToggleAt = -Infinity;
let keyboardControlClickSuppressedUntil = 0;
let lastShortcutTouchActionAt = 0;
let softKeyboardOpenFocusTimers: number[] = [];
let keyboardScrollRestoreTimers: number[] = [];
let pendingKeyboardScrollPosition: ScrollPosition | null = null;
// Latched true as soon as the user touches / wheels the terminal viewport
// after a keyboard-open cycle starts. All snap-to-bottom + restore-to-bottom
// paths bail on this so the user's scroll intent wins over the mobile
// "keep prompt visible while soft keyboard animates" heuristic. Reset on
// showSoftKeyboard / hideSoftKeyboard (fresh cycle).
let userScrolledDuringKeyboardCycle = false;

// Touch-scroll debug instrumentation lives in lib/touchScrollDebug.ts;
// this component just latches the opt-in flag at mount and calls
// logTouch(...) at instrumented sites. touchState() stays here because
// it closes over reactive refs (selectionMode/imeFocused/etc.) that
// only exist inside setup.
function touchState() {
  return {
    selMode: selectionMode.value,
    imeFocused: imeFocused.value,
    softKeyboardOpen: softKeyboardOpen.value,
    softKeyboardOpening: softKeyboardOpening.value,
    keyboardWanted: keyboardWanted.value,
    userScrolledDuringKeyboardCycle,
    scrollTop: terminalTouchViewport?.scrollTop ?? null,
    scrollHeight: terminalTouchViewport?.scrollHeight ?? null,
    clientHeight: terminalTouchViewport?.clientHeight ?? null,
    activeEl: document.activeElement?.className || document.activeElement?.tagName || null,
  };
}
const LONG_PRESS_MS = 500;
const PRESS_JITTER_PX = 8;
const POST_PRESS_DRAG_PX = 4;
const EDGE_PX = 24;
const EDGE_SCROLL_LINES = 3;
const EDGE_SCROLL_INTERVAL_MS = 60;
const TOUCH_COMPAT_MOUSE_BLOCK_MS = 800;
const KEYBOARD_CONTROL_BLUR_GRACE_MS = 500;
const KEYBOARD_CONTROL_CLICK_SUPPRESS_MS = 800;
const KEYBOARD_SCROLL_RESTORE_DELAYS_MS = [80, 180, 360, 700];
const SOFT_KEYBOARD_TOGGLE_DEDUP_MS = 350;
const SOFT_KEYBOARD_OPEN_RETRY_DELAYS_MS = [60, 180, 360];
const SOFT_KEYBOARD_OPEN_GRACE_MS = 900;
const SHORTCUT_TAP_MOVE_PX = 10;

type ShortcutPointerState = {
  pointerId: number;
  startX: number;
  startY: number;
  cancelled: boolean;
};
let shortcutPointerState: ShortcutPointerState | null = null;

type KeyboardControlTouchState = {
  startX: number;
  startY: number;
  cancelled: boolean;
  scrollTarget: HTMLElement | null;
  startScrollLeft: number;
};
let keyboardControlTouchState: KeyboardControlTouchState | null = null;

type ScrollPosition = {
  viewportY: number;
  atBottom: boolean;
};

function isLiveTerminal(): boolean {
  return isAlive && term !== null && termContainer.value !== null;
}

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
const showAuxKeyBar = computed(() =>
  !platform.caps.wailsBindings && !platform.caps.localPty && auxKeys.value.length > 0
);
const auxKeysCanSend = computed(() =>
  isDriver.value && isPasteAllowed(status.value, props.remotePermission)
);
const pasteBlobCanSend = computed(() =>
  auxKeysCanSend.value && effectiveRemotePermission(props.remotePermission) === "full"
);
const bottomBarCount = computed(() =>
  (templatesHidden.value ? 0 : 1) + (showAuxKeyBar.value ? 1 : 0)
);
const terminalBottom = computed(() => `${bottomBarCount.value * 30}px`);
const templateBarBottom = computed(() => showAuxKeyBar.value ? "30px" : "0");

function focusTerminalIfDriver() {
  if (!isDriver.value) return;
  term?.focus();
}

function focusTerminalForPaneActivation() {
  if (platform.caps.wailsBindings || platform.caps.localPty || keyboardWanted.value) {
    focusTerminalIfDriver();
  }
}

function syncTerminalInputMode() {
  if (term) term.options.disableStdin = !isDriver.value;
  if (!imeInputTarget) return;
  imeInputTarget.readOnly = !isDriver.value;
  imeInputTarget.tabIndex = isDriver.value ? 0 : -1;
  if (!isDriver.value) {
    clearSoftKeyboardOpenFocusTimers();
    softKeyboardOpening.value = false;
    keyboardWanted.value = false;
    if (document.activeElement === imeInputTarget) {
      imeInputTarget.blur();
      term?.blur();
    }
  }
}

function captureScrollPosition(): ScrollPosition | null {
  const buffer = term?.buffer.active;
  if (!buffer) return null;
  return {
    viewportY: buffer.viewportY,
    atBottom: buffer.viewportY >= buffer.baseY,
  };
}

function applyScrollPosition(scrollPosition: ScrollPosition | null) {
  logTouch("applyScrollPosition:entry", { scrollPosition, ...touchState() });
  if (!scrollPosition) return;
  if (!term || !isAlive) return;
  if (userScrolledDuringKeyboardCycle) { logTouch("applyScrollPosition:bailUserScrolled"); return; }
  if (scrollPosition.atBottom) {
    logTouch("applyScrollPosition:scrollToBottom");
    term.scrollToBottom();
  } else {
    logTouch("applyScrollPosition:scrollToLine", { line: scrollPosition.viewportY });
    term.scrollToLine(scrollPosition.viewportY);
  }
}

function onViewportUserScrollIntent() {
  logTouch("onViewportUserScrollIntent", { ...touchState() });
  userScrolledDuringKeyboardCycle = true;
  clearKeyboardScrollRestoreTimers();
  pendingKeyboardScrollPosition = null;
}

// Xterm 5.3 already handles touch scroll via Viewport.handleTouchStart +
// handleTouchMove (both registered on the .xterm root). We only need to
// signal user scroll intent so the soft-keyboard scroll-restore chain
// (up to ~1.6 s of forced snap-to-bottom) stops fighting the user's swipe.
// touchmove is the right signal (not touchstart — a plain tap to open the
// keyboard shouldn't count as "scroll intent"). Selection-drag path is
// unaffected because it also expresses itself through touchmove and its
// scroll updates should also short-circuit the restore chain.
function onTermTouchMoveForRestoreCancel(event: TouchEvent) {
  logTouch("onTermTouchMoveForRestoreCancel", {
    touches: event.touches.length,
    y: event.touches[0]?.pageY,
    ...touchState(),
  });
  onViewportUserScrollIntent();
}

function clearKeyboardScrollRestoreTimers() {
  for (const timer of keyboardScrollRestoreTimers) window.clearTimeout(timer);
  keyboardScrollRestoreTimers = [];
}

function clearSoftKeyboardOpenFocusTimers() {
  for (const timer of softKeyboardOpenFocusTimers) window.clearTimeout(timer);
  softKeyboardOpenFocusTimers = [];
}

function restorePendingKeyboardScrollPosition() {
  applyScrollPosition(pendingKeyboardScrollPosition);
}

function restoreScrollPositionAfterKeyboardToggle(scrollPosition: ScrollPosition | null) {
  if (!scrollPosition) return;
  clearKeyboardScrollRestoreTimers();
  pendingKeyboardScrollPosition = scrollPosition;
  const lastDelay = KEYBOARD_SCROLL_RESTORE_DELAYS_MS[KEYBOARD_SCROLL_RESTORE_DELAYS_MS.length - 1];
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      restorePendingKeyboardScrollPosition();
    });
  });
  for (const delay of KEYBOARD_SCROLL_RESTORE_DELAYS_MS) {
    keyboardScrollRestoreTimers.push(window.setTimeout(() => {
      restorePendingKeyboardScrollPosition();
      if (delay === lastDelay) {
        pendingKeyboardScrollPosition = null;
        keyboardScrollRestoreTimers = [];
      }
    }, delay));
  }
}

function hideSoftKeyboard() {
  const scrollPosition = captureScrollPosition();
  clearSoftKeyboardOpenFocusTimers();
  softKeyboardOpening.value = false;
  keyboardWanted.value = false;
  imeInputTarget?.blur();
  term?.blur();
  void platform.system.hideSoftKeyboard?.();
  userScrolledDuringKeyboardCycle = false;
  restoreScrollPositionAfterKeyboardToggle(scrollPosition);
}

function scrollToBottomForKeyboardOpen() {
  logTouch("scrollToBottomForKeyboardOpen:entry", { ...touchState() });
  if (!term) return;
  if (userScrolledDuringKeyboardCycle) { logTouch("scrollToBottomForKeyboardOpen:bailUserScrolled"); return; }
  term.scrollToBottom();
  restoreScrollPositionAfterKeyboardToggle({
    viewportY: term.buffer.active.baseY,
    atBottom: true,
  });
}

function showSoftKeyboard() {
  if (!isDriver.value) return;
  userScrolledDuringKeyboardCycle = false;
  keyboardWanted.value = true;
  scheduleSoftKeyboardOpenFocusRetries();
  scrollToBottomForKeyboardOpen();
  focusTerminalIfDriver();
}

function scheduleSoftKeyboardOpenFocusRetries() {
  clearSoftKeyboardOpenFocusTimers();
  softKeyboardOpening.value = true;
  for (const delay of SOFT_KEYBOARD_OPEN_RETRY_DELAYS_MS) {
    softKeyboardOpenFocusTimers.push(window.setTimeout(() => {
      if (!keyboardWanted.value) return;
      if (!imeFocused.value) focusTerminalIfDriver();
      scrollToBottomForKeyboardOpen();
    }, delay));
  }
  softKeyboardOpenFocusTimers.push(window.setTimeout(() => {
    softKeyboardOpening.value = false;
    if (!imeFocused.value) keyboardWanted.value = false;
    softKeyboardOpenFocusTimers = [];
  }, SOFT_KEYBOARD_OPEN_GRACE_MS));
}

function onKeyboardControlPointerDown() {
  lastKeyboardControlPointerAt = Date.now();
}

function keyboardControlTouchDistanceSquared(touch: Touch) {
  if (!keyboardControlTouchState) return 0;
  const dx = touch.clientX - keyboardControlTouchState.startX;
  const dy = touch.clientY - keyboardControlTouchState.startY;
  return dx * dx + dy * dy;
}

function onKeyboardControlTouchStart(event: TouchEvent) {
  onKeyboardControlPointerDown();
  const touch = event.touches[0];
  keyboardControlTouchState = touch
    ? { startX: touch.clientX, startY: touch.clientY, cancelled: false, scrollTarget: null, startScrollLeft: 0 }
    : null;
}

function onAuxKeyboardControlTouchStart(event: TouchEvent) {
  onKeyboardControlTouchStart(event);
  if (!keyboardControlTouchState) return;
  const target = event.currentTarget instanceof Element
    ? event.currentTarget.closest('.aux-key-scroll')
    : null;
  if (!(target instanceof HTMLElement)) return;
  keyboardControlTouchState.scrollTarget = target;
  keyboardControlTouchState.startScrollLeft = target.scrollLeft;
}

function onKeyboardControlTouchMove(event: TouchEvent) {
  if (!keyboardControlTouchState) return;
  const touch = event.touches[0];
  if (!touch) return;
  const scrollTarget = keyboardControlTouchState.scrollTarget;
  if (scrollTarget) {
    scrollTarget.scrollLeft = keyboardControlTouchState.startScrollLeft + keyboardControlTouchState.startX - touch.clientX;
  }
  if (keyboardControlTouchDistanceSquared(touch) > SHORTCUT_TAP_MOVE_PX * SHORTCUT_TAP_MOVE_PX) {
    keyboardControlTouchState.cancelled = true;
  }
}

function onKeyboardControlTouchCancel() {
  keyboardControlTouchState = null;
}

function shouldRunKeyboardControlTouchAction(event: TouchEvent) {
  const touch = event.changedTouches[0];
  const shouldRun =
    keyboardControlTouchState !== null &&
    !keyboardControlTouchState.cancelled &&
    (!touch || keyboardControlTouchDistanceSquared(touch) <= SHORTCUT_TAP_MOVE_PX * SHORTCUT_TAP_MOVE_PX);
  keyboardControlTouchState = null;
  return shouldRun;
}

function onKeyboardControlTouchEnd(event: TouchEvent, action: () => void) {
  onKeyboardControlPointerDown();
  if (!shouldRunKeyboardControlTouchAction(event)) return;
  event.preventDefault();
  keyboardControlClickSuppressedUntil = Date.now() + KEYBOARD_CONTROL_CLICK_SUPPRESS_MS;
  action();
}

function onKeyboardControlClick(action: () => void) {
  if (Date.now() < keyboardControlClickSuppressedUntil) return;
  action();
}

function shouldPreserveSoftKeyboardForControlPointer() {
  return Date.now() - lastKeyboardControlPointerAt <= KEYBOARD_CONTROL_BLUR_GRACE_MS;
}

function shouldRunSoftKeyboardToggle() {
  const now = Date.now();
  if (now - lastSoftKeyboardToggleAt < SOFT_KEYBOARD_TOGGLE_DEDUP_MS) return false;
  lastSoftKeyboardToggleAt = now;
  return true;
}

function toggleSoftKeyboard() {
  if (!shouldRunSoftKeyboardToggle()) return;
  if (softKeyboardCollapsed.value) {
    showSoftKeyboard();
  } else {
    hideSoftKeyboard();
  }
}

function shortcutPointerDistanceSquared(event: PointerEvent) {
  if (!shortcutPointerState) return 0;
  const dx = event.clientX - shortcutPointerState.startX;
  const dy = event.clientY - shortcutPointerState.startY;
  return dx * dx + dy * dy;
}

function onShortcutPointerDown(event: PointerEvent) {
  if (event.pointerType === "mouse" && event.button !== 0) return;
  if (event.pointerType === "touch") return;
  event.stopPropagation();
  shortcutPointerState = {
    pointerId: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    cancelled: false,
  };
}

function onShortcutPointerMove(event: PointerEvent) {
  if (!shortcutPointerState || shortcutPointerState.pointerId !== event.pointerId) return;
  if (shortcutPointerDistanceSquared(event) > SHORTCUT_TAP_MOVE_PX * SHORTCUT_TAP_MOVE_PX) {
    shortcutPointerState.cancelled = true;
  }
}

function clearShortcutPointerState(event?: PointerEvent) {
  if (event) event.stopPropagation();
  shortcutPointerState = null;
}

function onShortcutPointerCancel(event: PointerEvent) {
  if (!shortcutPointerState || shortcutPointerState.pointerId !== event.pointerId) return;
  clearShortcutPointerState(event);
}

function shouldSendShortcutPointer(event: PointerEvent) {
  if (!shortcutPointerState || shortcutPointerState.pointerId !== event.pointerId) return false;
  event.preventDefault();
  event.stopPropagation();
  if (Date.now() - lastShortcutTouchActionAt < KEYBOARD_CONTROL_CLICK_SUPPRESS_MS) {
    clearShortcutPointerState(event);
    return false;
  }
  const shouldSend =
    !shortcutPointerState.cancelled &&
    shortcutPointerDistanceSquared(event) <= SHORTCUT_TAP_MOVE_PX * SHORTCUT_TAP_MOVE_PX;
  clearShortcutPointerState(event);
  return shouldSend;
}

function handleViewerKeydown(event: KeyboardEvent) {
  if (isDriver.value) return; // driver mode passes through
  // Only fire for the pane the user is actually looking at. Multiple
  // TerminalView instances all register this document listener; without
  // this gate a viewer-mode pane in a background tab would grab Space
  // meant for the foreground pane / input.
  if (!props.active) return;
  // Skip when the user is typing in an editable target (sidebar search,
  // settings, etc.) — Space belongs to that input, not us.
  const t = event.target as HTMLElement | null;
  if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
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

async function handleCtrlVKeydownPaste(e: KeyboardEvent) {
  if (!term || !conn || !isCtrlVKeydownPaste(e)) return;
  // Stop xterm from also forwarding `\x16` to the PTY (the TUI would
  // otherwise also read the clipboard and we'd double-paste).
  e.preventDefault();
  e.stopPropagation();
  if (pasteBusy.value) return;
  pasteBusy.value = true;
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
  } catch (err) {
    console.warn("[AT Term] failed to paste on Ctrl+V", err);
  } finally {
    pasteBusy.value = false;
  }
}

async function handleImagePaste(e: ClipboardEvent) {
  const items = Array.from(e.clipboardData?.items || []);
  const anyFile = items.find((i) => i.kind === "file");
  if (!anyFile) return;
  e.preventDefault();
  e.stopPropagation();
  const activeTerm = term;
  const activeConn = conn;
  if (!activeTerm || !activeConn) return;
  try {
    // In a local session, the NSPasteboard often carries the original file
    // URL (Finder Copy). dispatchPastedFile probes for those paths via the
    // Wails bridge and injects them straight into xterm, skipping the
    // PASTE_FILE upload so the shell sees the source path — not the
    // paste-files/<sid>/ inbox that the "remote clients" inbox semantics
    // still label as remote-received.
    await dispatchPastedFile({
      items,
      isLocalSession: props.isLocalSession ?? true,
      conn: {
        sendPasteImage: (blob, filename) => {
          pasteImageBus.emit(blob, filename);
          return activeConn.sendPasteImage(blob, filename);
        },
        sendPasteFile: (blob, filename) => activeConn.sendPasteFile(blob, filename),
      },
      paste: (s) => activeTerm.paste(s),
      getLocalPaths: async () => {
        try {
          return (await GetPasteboardFileURLs()) ?? [];
        } catch (err) {
          console.warn("[AT Term] GetPasteboardFileURLs failed", err);
          return [];
        }
      },
      onFileToast: (name, size) => pasteFileBus.emit(name, size),
    });
  } catch (err) {
    console.warn("[AT Term] paste dispatch failed", err);
  }
}

// iOS on-screen keyboards send some direct text (notably Chinese 9-key
// punctuation, numbers, and space) through the textarea `input` event without
// xterm forwarding it. Take over only the non-composition insertText case so
// pinyin -> Hanzi composition remains xterm-owned.
//
// Desktop (Wails / local-PTY web) MUST skip this path: xterm already handles
// the physical-keyboard keydown and calls term.onData(" "); the browser then
// also inserts the char into the hidden textarea and fires `input`, which
// without this gate would run onImeInput and sendInput a second time —
// pressing space once produces two spaces, breaking TUI checkbox toggling.
function onImeInput(event: InputEvent) {
  if (platform.caps.wailsBindings || platform.caps.localPty) return;
  if (event.inputType !== "insertText") return;
  if (event.isComposing) return;
  const data = event.data;
  if (!data) return;
  if (!auxKeysCanSend.value) return;
  event.stopImmediatePropagation();
  conn?.sendInput(data);
  const target = event.target as HTMLTextAreaElement | null;
  if (target && "value" in target) target.value = "";
}

function onImeFocus(event: FocusEvent) {
  logTouch("onImeFocus", { keyboardWanted: keyboardWanted.value, isDriver: isDriver.value });
  imeFocused.value = true;
  if (isDriver.value) {
    if (keyboardWanted.value) scrollToBottomForKeyboardOpen();
    return;
  }
  const target = event.target as HTMLTextAreaElement | null;
  target?.blur();
  term?.blur();
}

function onImeBlur() {
  logTouch("onImeBlur", { keyboardWanted: keyboardWanted.value });
  imeFocused.value = false;
  if (keyboardWanted.value && shouldPreserveSoftKeyboardForControlPointer()) {
    scheduleSoftKeyboardOpenFocusRetries();
    return;
  }
  if (keyboardWanted.value && softKeyboardOpening.value) return;
  keyboardWanted.value = false;
}

function shouldBlockTerminalPointerFocus(event: PointerEvent) {
  if (platform.caps.wailsBindings || platform.caps.localPty) return false;
  if (event.pointerType === "mouse") return false;
  return true;
}

function shouldBlockTerminalTouchFocus(event: TouchEvent) {
  if (platform.caps.wailsBindings || platform.caps.localPty) return false;
  return event.touches.length > 0;
}

function shouldHideSoftKeyboardForTerminalPointer(event: PointerEvent) {
  if (platform.caps.wailsBindings || platform.caps.localPty) return false;
  if (!softKeyboardOpen.value) return false;
  if (event.pointerType === "mouse") return false;
  return true;
}

function onTermPointerDown(event: PointerEvent) {
  logTouch("onTermPointerDown:entry", {
    pointerType: event.pointerType,
    y: event.clientY,
    target: (event.target as Element | null)?.className || (event.target as Element | null)?.tagName || null,
    ...touchState(),
  });
  const shouldHideKeyboard = shouldHideSoftKeyboardForTerminalPointer(event);
  const shouldBlockFocus = shouldBlockTerminalPointerFocus(event);
  logTouch("onTermPointerDown:decision", { shouldHideKeyboard, shouldBlockFocus });
  if (shouldHideKeyboard || shouldBlockFocus) lastBlockedTerminalTouchAt = Date.now();
  if (imeFocused.value && imeInputTarget && document.activeElement === imeInputTarget) {
    if (shouldHideKeyboard) {
      event.preventDefault();
      event.stopPropagation();
      hideSoftKeyboard();
      onSelectionPointerDown(event);
    }
    return;
  }
  if (shouldHideKeyboard) {
    event.preventDefault();
    event.stopPropagation();
    hideSoftKeyboard();
    onSelectionPointerDown(event);
    return;
  }
  if (shouldBlockFocus) {
    event.preventDefault();
    event.stopPropagation();
  }
  onSelectionPointerDown(event);
}

function onTermTouchStart(event: TouchEvent) {
  logTouch("onTermTouchStart:entry", {
    touches: event.touches.length,
    y: event.touches[0]?.pageY,
    target: (event.target as Element | null)?.className || (event.target as Element | null)?.tagName || null,
    ...touchState(),
  });
  if (!shouldBlockTerminalTouchFocus(event)) { logTouch("onTermTouchStart:earlyReturn:!shouldBlockTerminalTouchFocus"); return; }
  lastBlockedTerminalTouchAt = Date.now();
  if (!softKeyboardOpen.value) { logTouch("onTermTouchStart:earlyReturn:!softKeyboardOpen"); return; }
  logTouch("onTermTouchStart:hideKeyboardBranch", { ...touchState() });
  // preventDefault alone stops the default focus that would reopen the
  // on-screen keyboard; hideSoftKeyboard blurs the IME textarea. We do NOT
  // stopPropagation — xterm's own touchstart (registered on .xterm root,
  // a descendant of .term) needs to fire so Viewport._lastTouchY is
  // initialized against THIS touch's pageY. Without that init, xterm's
  // subsequent touchmove computes delta against 0 and scroll clamps to
  // top, and the user sees "first swipe does nothing." This path fires
  // whenever softKeyboardOpen is true, which includes cases where iOS
  // dropped a blur event and imeFocused is stale — so blanket
  // stopPropagation here made those swipes intermittently dead too.
  // Xterm's touchstart is passive (preventDefault a no-op there) and
  // does not focus anything, so letting it through is safe.
  event.preventDefault();
  hideSoftKeyboard();
}

function onTermMouseDown(event: MouseEvent) {
  // Record the press origin so onTerminalMouseUp can tell a plain click on a
  // link from the tail of a drag-select. Done before any early return so it
  // runs on desktop (wails/localPty) too.
  if (event.button === 0) linkClickDownPos = { x: event.clientX, y: event.clientY };
  if (platform.caps.wailsBindings || platform.caps.localPty) return;
  if (Date.now() - lastBlockedTerminalTouchAt > TOUCH_COMPAT_MOUSE_BLOCK_MS) return;
  event.preventDefault();
  event.stopPropagation();
}

function stopTerminalTouchMove(event: TouchEvent) {
  event.stopPropagation();
}

function clearSelectionPressTimer() {
  if (!selectionPressTimer) return;
  clearTimeout(selectionPressTimer);
  selectionPressTimer = null;
}

function resolveSelectionViewport(event?: Event): HTMLElement | null {
  if (terminalTouchViewport && document.contains(terminalTouchViewport)) return terminalTouchViewport;
  const target = event?.target as HTMLElement | null;
  const fromTarget = target?.closest?.(".xterm-viewport") as HTMLElement | null;
  terminalTouchViewport = fromTarget ?? termContainer.value?.querySelector<HTMLElement>(".xterm-viewport") ?? null;
  return terminalTouchViewport;
}

function exitSelection() {
  if (term) {
    try { term.clearSelection(); } catch { /* ignore stale xterm selection */ }
    syncTerminalInputMode();
  }
  if (terminalTouchViewport) terminalTouchViewport.style.touchAction = "pan-y";
  selectionPopover.value.visible = false;
  selectionMode.value = "idle";
  clearSelectionPressTimer();
  selectionPressAnchor = null;
  selectionDragAnchor = null;
  stopSelectionEdgeScroll();
}

function readSelectionBbox(pos: { start: { x: number; y: number }; end: { x: number; y: number } }) {
  if (!term || !terminalTouchViewport) return null;
  const { width: cw, height: ch } = readXtermCellSize(term);
  if (!cw || !ch) return null;
  const rect = terminalTouchViewport.getBoundingClientRect();
  const scrollTop = terminalTouchViewport.scrollTop ?? 0;
  const left = rect.left + pos.start.x * cw;
  const top = rect.top + pos.start.y * ch - scrollTop;
  const width = pos.start.y === pos.end.y ? (pos.end.x - pos.start.x) * cw : rect.width;
  const height = (pos.end.y - pos.start.y + 1) * ch;
  return { x: left, y: top, width, height };
}

function updateSelectionPopoverFromSelection() {
  if (!term) return;
  const pos = term.getSelectionPosition?.();
  if (!pos) {
    selectionPopover.value.visible = false;
    return;
  }
  const dim = readSelectionBbox(pos);
  if (!dim) {
    selectionPopover.value.visible = false;
    return;
  }
  const popoverHeight = 28;
  const fitsAbove = dim.y - 6 - popoverHeight >= 8;
  selectionPopover.value.arrowDir = fitsAbove ? "down" : "up";
  selectionPopover.value.x = dim.x + dim.width / 2;
  selectionPopover.value.y = fitsAbove
    ? (window.innerHeight - dim.y + 6)
    : (dim.y + dim.height + 6);
  selectionPopover.value.visible = true;
}

function onSelectionPointerDown(event: PointerEvent) {
  logTouch("onSelectionPointerDown:entry", {
    pointerType: event.pointerType,
    isPrimary: event.isPrimary,
    y: event.clientY,
    ...touchState(),
  });
  if (!auxKeysCanSend.value || !term) { logTouch("onSelectionPointerDown:earlyReturn"); return; }
  if (selectionMode.value === "selecting" || selectionMode.value === "dragging") {
    exitSelection();
  }
  const viewport = resolveSelectionViewport(event);
  if (!viewport) return;
  selectionPressAnchor = { x: event.clientX, y: event.clientY };
  selectionMode.value = "pressing";
  clearSelectionPressTimer();
  selectionPressTimer = setTimeout(() => {
    selectionPressTimer = null;
    if (selectionMode.value !== "pressing" || !selectionPressAnchor || !term) return;
    const hit = cellCoordsAt(selectionPressAnchor.x, selectionPressAnchor.y, term, viewport);
    if (!hit) {
      selectionMode.value = "idle";
      return;
    }
    let line = "";
    try {
      line = term.buffer.active.getLine(hit.row)?.translateToString(true) ?? "";
    } catch {
      line = "";
    }
    const boundary = wordBoundaryAt(line, hit.col);
    if (boundary.len === 0) {
      selectionMode.value = "idle";
      return;
    }
    try {
      term.select(boundary.start, hit.row, boundary.len);
    } catch {
      selectionMode.value = "idle";
      return;
    }
    term.options.disableStdin = true;
    viewport.style.touchAction = "none";
    selectionMode.value = "selecting";
    selectionDragAnchor = {
      start: boundary.start,
      end: boundary.start + boundary.len - 1,
      row: hit.row,
    };
    updateSelectionPopoverFromSelection();
  }, LONG_PRESS_MS);
}

function onSelectionPointerMove(event: PointerEvent) {
  if (isTouchDebugEnabled() && selectionMode.value !== "idle") {
    logTouch("onSelectionPointerMove", { selMode: selectionMode.value, y: event.clientY });
  }
  if (selectionMode.value === "pressing" && selectionPressAnchor) {
    const dx = event.clientX - selectionPressAnchor.x;
    const dy = event.clientY - selectionPressAnchor.y;
    if (dx * dx + dy * dy > PRESS_JITTER_PX * PRESS_JITTER_PX) {
      clearSelectionPressTimer();
      selectionMode.value = "idle";
      selectionPressAnchor = null;
    }
    return;
  }
  if (selectionMode.value !== "selecting" && selectionMode.value !== "dragging") return;
  if (!term || !terminalTouchViewport || !selectionDragAnchor || !selectionPressAnchor) return;
  if (selectionMode.value === "selecting") {
    const dx = event.clientX - selectionPressAnchor.x;
    const dy = event.clientY - selectionPressAnchor.y;
    if (dx * dx + dy * dy < POST_PRESS_DRAG_PX * POST_PRESS_DRAG_PX) return;
    selectionMode.value = "dragging";
  }
  const current = cellCoordsAt(event.clientX, event.clientY, term, terminalTouchViewport);
  if (!current) return;
  const [rowStart, rowEnd] = selectionDragAnchor.row <= current.row
    ? [selectionDragAnchor.row, current.row]
    : [current.row, selectionDragAnchor.row];
  try {
    if (rowStart === rowEnd) {
      const colStart = Math.min(selectionDragAnchor.start, current.col);
      const colEnd = Math.max(selectionDragAnchor.end, current.col);
      term.select(colStart, rowStart, colEnd - colStart + 1);
    } else {
      term.selectLines(rowStart, rowEnd);
    }
  } catch {
    /* keep previous selection */
  }
  const rect = terminalTouchViewport.getBoundingClientRect();
  let dir: -1 | 1 | 0 = 0;
  if (event.clientY - rect.top < EDGE_PX) dir = -1;
  else if (rect.bottom - event.clientY < EDGE_PX) dir = 1;
  if (dir === 0) stopSelectionEdgeScroll();
  else ensureSelectionEdgeScroll(dir);
}

function onSelectionPointerUp() {
  if (selectionMode.value === "pressing") {
    clearSelectionPressTimer();
    selectionMode.value = "idle";
    selectionPressAnchor = null;
    return;
  }
  if (selectionMode.value === "dragging") {
    selectionMode.value = "selecting";
    selectionDragAnchor = null;
    stopSelectionEdgeScroll();
    updateSelectionPopoverFromSelection();
  }
}

function onSelectionPointerCancel() {
  stopSelectionEdgeScroll();
  selectionDragAnchor = null;
  if (selectionMode.value === "pressing") {
    clearSelectionPressTimer();
    selectionMode.value = "idle";
    selectionPressAnchor = null;
    return;
  }
  if (selectionMode.value === "dragging") selectionMode.value = "selecting";
}

function onSelectionViewportScroll() {
  if (selectionMode.value !== "idle") exitSelection();
}

function ensureSelectionEdgeScroll(dir: -1 | 1) {
  if (selectionEdgeScrollDir === dir && selectionEdgeScrollTimer) return;
  stopSelectionEdgeScroll();
  selectionEdgeScrollDir = dir;
  selectionEdgeScrollTimer = setInterval(() => {
    try { term?.scrollLines(dir * EDGE_SCROLL_LINES); } catch { /* ignore */ }
  }, EDGE_SCROLL_INTERVAL_MS);
}

function stopSelectionEdgeScroll() {
  if (selectionEdgeScrollTimer) {
    clearInterval(selectionEdgeScrollTimer);
    selectionEdgeScrollTimer = null;
  }
  selectionEdgeScrollDir = 0;
}

function onDocumentPointerDown(event: PointerEvent) {
  if (shouldHideSoftKeyboardForDocumentPointer(event)) hideSoftKeyboard();
  if (templateConfirmOpen.value) {
    const target = event.target as Element | null;
    if (!target?.closest?.(".template-confirm-panel, .template-bar")) cancelTemplateSend();
  }
  if (selectionMode.value === "idle") return;
  const target = event.target as Element | null;
  const viewport = terminalTouchViewport ?? termContainer.value?.querySelector<HTMLElement>(".xterm-viewport");
  if (target && viewport?.contains(target)) return;
  if (target && typeof target.closest === "function" && target.closest('[data-testid="selection-popover"]')) return;
  exitSelection();
}

function isKeyboardControlSurface(target: EventTarget | null) {
  if (!(target instanceof Element)) return false;
  return Boolean(target.closest('.template-bar, .aux-key-bar, .template-confirm-panel, .paste-confirm-panel'));
}

function shouldHideSoftKeyboardForDocumentPointer(event: PointerEvent) {
  if (platform.caps.wailsBindings || platform.caps.localPty) return false;
  if (!softKeyboardOpen.value) return false;
  if (event.pointerType === "mouse") return false;
  return !isKeyboardControlSurface(event.target);
}

async function onSelectionCopy() {
  if (!term || selectionPopover.value.copying) {
    exitSelection();
    return;
  }
  selectionPopover.value.copying = true;
  let copied = false;
  try {
    copied = await copyTerminalSelection(term);
  } catch (err) {
    console.warn("[AT Term] selection copy failed", err);
  } finally {
    selectionPopover.value.copying = false;
  }
  emit("toast", copied ? t("mobile.selection.copied") : t("mobile.selection.copyFailed"));
  exitSelection();
}

function onSelectionSend() {
  if (!term || !conn || selectionPopover.value.sending) {
    exitSelection();
    return;
  }
  selectionPopover.value.sending = true;
  try {
    const payload = prepareSendPayload(term.getSelection());
    if (payload) conn.sendInput(payload);
  } finally {
    selectionPopover.value.sending = false;
    exitSelection();
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

function getTerminalGridElement(): HTMLElement | null {
  const root = term?.element;
  return (
    root?.querySelector<HTMLElement>(".xterm-rows") ??
    root?.querySelector<HTMLElement>(".xterm-screen") ??
    termContainer.value
  );
}

// computeLinkHit converts a right-click MouseEvent into the LinkMatch that
// covers the clicked cell, or null when the click isn't on any detected link.
// Reuses detectLinks so the menu agrees with what the hover provider drew.
function computeLinkHit(e: MouseEvent): LinkMatch | null {
  if (!term) return null;
  const viewport = getTerminalGridElement();
  if (!viewport) return null;
  const hit = cellCoordsAt(e.clientX, e.clientY, term, viewport);
  if (!hit) return null;
  const bufferRow = term.buffer.active.viewportY + hit.row;
  const line = term.buffer.active.getLine(bufferRow);
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
  await openLinkMatch(hit);
}

// Resolve a detected path/file match to an absolute local path, or null when a
// ~/ path can't be resolved (no cached home).
function localPathFromMatch(hit: LinkMatch): string | null {
  const raw = hit.text;
  if (hit.kind === "file") return raw.startsWith("file://") ? raw.slice("file://".length) : raw;
  if (raw.startsWith("/")) return raw;
  if (raw.startsWith("~/") || raw === "~/") {
    if (!cachedHomeDir) return null;
    const home = cachedHomeDir.replace(/\/+$/, "");
    return home + raw.slice(1);
  }
  return null;
}

async function openLinkMatch(hit: LinkMatch) {
  // Local file/path links reveal in the file explorer instead of opening an
  // external app; http(s) links still go to the browser.
  if (hit.kind === "path" || hit.kind === "file") {
    const abs = localPathFromMatch(hit);
    if (abs) {
      fileRevealStore.request(abs);
      return;
    }
    // ~/ with no cached home: fall through to the file:// error path below.
  }
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

function onTerminalMouseUp(e: MouseEvent) {
  if (e.button !== 0) return;
  // Ctrl+click opens the link; plain clicks stay available for selection.
  // Mirrors the xterm link provider's activate() judgment via the shared
  // shouldActivateLink().
  if (!shouldActivateLink(e, linkClickDownPos, isMac())) return;
  const hit = computeLinkHit(e);
  if (!hit) return;
  e.preventDefault();
  e.stopPropagation();
  e.stopImmediatePropagation();
  void openLinkMatch(hit);
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

// syncPtySizeToTerm pushes our xterm dims to the PTY when the two disagree.
// term.onResize is the only other RESIZE source, and FitAddon is a no-op
// when our xterm already has the size it would compute — so a PTY that
// another client resized behind our back never gets corrected on its own.
// The window where that happens: a background tab calls conn.suspend(),
// which closes the WS, so the META carrying the other client's smaller size
// never reaches us and applyViewerSize never mirrors it onto our xterm. On
// re-attach we re-claim the driver role at an unchanged local size, fit()
// changes nothing, onResize stays silent, and the PTY sits at the other
// client's dims until the user happens to resize the window by hand.
function syncPtySizeToTerm() {
  if (!term || !conn) return;
  if (!isDriver.value) return; // the relay drops a viewer's RESIZE anyway
  if (props.resizeSuspended) return; // mid-drag: the falling-edge watch emits the final RESIZE
  const cols = ptyCols.value;
  const rows = ptyRows.value;
  if (typeof cols !== "number" || typeof rows !== "number") return;
  if (cols === term.cols && rows === term.rows) return;
  conn.sendResize(term.cols, term.rows);
}

function applyViewerSize() {
  if (!term) return;
  if (isDriver.value) {
    // Driver path: re-engage FitAddon (term.onResize → sendResize fires from here).
    safeFit();
    // ...but fit() is silent when our size didn't change, so reconcile the
    // PTY explicitly — this is the driver-recovery path described above.
    syncPtySizeToTerm();
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
    disableStdin: !isDriver.value,
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
  if (!isLiveTerminal()) return;
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
  keyTarget.addEventListener("keydown", handleCtrlVKeydownPaste, { capture: true });
  // handleViewerKeydown is attached to document (not keyTarget) so it fires
  // regardless of where focus currently sits. The autofocused take-control
  // button covers the initial transition, but focus drifts as soon as the
  // user clicks elsewhere or switches tabs and comes back — the pane's
  // .term-container never regains focus in viewer mode (IME textarea stays
  // blurred), so a scoped listener would silently die. See onBeforeUnmount
  // for the matching removeEventListener.
  document.addEventListener("keydown", handleViewerKeydown, { capture: true });
  keyTarget.addEventListener("paste", handleImagePaste, { capture: true });
  keyTarget.addEventListener("pointermove", onSelectionPointerMove);
  keyTarget.addEventListener("pointerup", onSelectionPointerUp);
  keyTarget.addEventListener("pointercancel", onSelectionPointerCancel);
  terminalTouchViewport = term.element?.querySelector<HTMLElement>(".xterm-viewport") ?? null;
  // ---- Neuter xterm's built-in touch scroll ----
  // xterm 5.3 Viewport.handleTouchMove is a strict 1:1 finger → scrollTop
  // mapper with no inertia; every touchmove synchronously mutates scrollTop
  // and — since Terminal.ts wraps handleTouchMove with `cancel(ev)` on
  // `false` return — calls preventDefault on the event. That blocks the
  // browser's native scroll (with iOS fling / momentum / high-priority
  // event pipeline) even after the CSS above stacks .xterm-viewport where
  // touches can reach it. Patch both handlers to no-op and return true so
  // xterm never calls cancel → no preventDefault → the browser drives the
  // scroll natively via `touch-action: pan-y` + `-webkit-overflow-scrolling`.
  // Harmless on desktop (no touch → these handlers never fire anyway); no
  // gate needed. See xterm.js #5377.
  try {
    const coreAny = (term as unknown as { _core?: { _viewport?: unknown } })._core;
    const vp = coreAny?._viewport as {
      handleTouchStart?: (ev: TouchEvent) => void;
      handleTouchMove?: (ev: TouchEvent) => boolean;
    } | undefined;
    if (vp) {
      vp.handleTouchStart = () => {};
      vp.handleTouchMove = () => true;
    }
  } catch (err) {
    console.warn("[AT Term] disable xterm touch scroll failed", err);
  }
  // Bootstrap the debug flag once term is up and install the console dump.
  setTouchDebugEnabled(readTouchDebugFlag());
  installTouchDebugDump();
  logTouch("ensureTerm:done", {
    debugEnabled: isTouchDebugEnabled(),
    hasViewport: !!terminalTouchViewport,
    hasXterm: !!term.element,
    xtermChildren: term.element ? Array.from(term.element.children).map(c => c.className) : [],
    ...touchState(),
  });
  // Passive observer listeners on the .xterm root — that's where xterm 5.3
  // registers its own touchstart/touchmove for Viewport._lastTouchY + scroll.
  // If these fire but xterm's own handler didn't run, we know something upstream
  // is stopping propagation before reaching xterm's listeners.
  if (isTouchDebugEnabled() && term.element) {
    const xtermRoot = term.element;
    xtermRoot.addEventListener("touchstart", (ev) => {
      const te = ev as TouchEvent;
      logTouch("observer:xterm.touchstart", {
        touches: te.touches.length,
        y: te.touches[0]?.pageY,
        defaultPrevented: te.defaultPrevented,
      });
    }, { passive: true, capture: false });
    xtermRoot.addEventListener("touchmove", (ev) => {
      const te = ev as TouchEvent;
      const before = terminalTouchViewport?.scrollTop ?? null;
      logTouch("observer:xterm.touchmove:before", {
        touches: te.touches.length,
        y: te.touches[0]?.pageY,
        scrollTopBefore: before,
        defaultPrevented: te.defaultPrevented,
      });
      // Peek scrollTop after this microtask so we see whether xterm's
      // handleTouchMove (also on this same event) actually moved it.
      queueMicrotask(() => {
        const after = terminalTouchViewport?.scrollTop ?? null;
        logTouch("observer:xterm.touchmove:after", { scrollTopAfter: after, diff: (after ?? 0) - (before ?? 0) });
      });
    }, { passive: true, capture: false });
    xtermRoot.addEventListener("touchend", () => logTouch("observer:xterm.touchend"), { passive: true });
    xtermRoot.addEventListener("touchcancel", () => logTouch("observer:xterm.touchcancel"), { passive: true });
  }
  terminalTouchViewport?.addEventListener("touchmove", stopTerminalTouchMove, { passive: true });
  terminalTouchViewport?.addEventListener("scroll", onSelectionViewportScroll);
  if (isTouchDebugEnabled() && terminalTouchViewport) {
    // Log every native scroll event on the viewport so we can see whether
    // scrollTop is being driven by the touch path (xterm.handleTouchMove) or
    // by a snap-to-bottom / restore-position path.
    terminalTouchViewport.addEventListener("scroll", () => {
      logTouch("viewport.scroll", { scrollTop: terminalTouchViewport?.scrollTop, selMode: selectionMode.value });
    }, { passive: true });
  }
  // Wheel on the viewport still fires natively (desktop) — cancel keyboard
  // scroll-restore on wheel too so wheel-scroll during the ~1.6 s window
  // isn't clobbered.
  terminalTouchViewport?.addEventListener("wheel", onViewportUserScrollIntent, { passive: true });
  // touchmove on the .term wrapper signals "user is actually scrolling"
  // (as opposed to tap-to-focus). Cancels the keyboard scroll-restore
  // chain so it doesn't yank the user back to the bottom mid-swipe.
  keyTarget.addEventListener("touchmove", onTermTouchMoveForRestoreCancel, { passive: true });
  imeInputTarget = term.element?.querySelector<HTMLTextAreaElement>("textarea") ?? null;
  syncTerminalInputMode();
  imeInputTarget?.addEventListener("input", onImeInput as EventListener, { capture: true });
  imeInputTarget?.addEventListener("focus", onImeFocus);
  imeInputTarget?.addEventListener("blur", onImeBlur);
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
    void showNotification("AT Term", t("terminal.bellNotification", { session: props.sessionLabel || "session" }), {
      session_id: props.sessionId,
      kind: "bell",
    });
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
        { session_id: props.sessionId, kind: "command_finished" },
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
  if (!isLiveTerminal()) return;
  linkProviderDisposer = useTerminalLinkProvider({
    term,
    isMac: isMac(),
    getHomeDir: () => cachedHomeDir,
    openLink: (m) => openLinkMatch(m),
    onError: (key) => emit("toast", t(key)),
  });

  resizeObserver = new ResizeObserver(() => {
    safeFit();
    restorePendingKeyboardScrollPosition();
  });
  resizeObserver.observe(termContainer.value!);
}

function startConnection() {
  if (!term) return;
  if (conn) {
    conn.attach();
    return;
  }
  conn = markRaw(new SessionConnection(
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
        syncTerminalInputMode();
        applyViewerSize();
        if (isMe && (props.active || props.focused)) nextTick(focusTerminalForPaneActivation);
        if (!isMe && (props.active || props.focused)) nextTick(() => takeControlBtnRef.value?.focus());
        if (wasDriver !== isMe) {
          emit("toast", isMe ? t("terminal.driverNow") : t("terminal.viewerNow"));
        }
      },
    },
    { clientName: localHostname.value, remote: !props.isLocalSession }
  ));
  conn.attach();
  pluginSessionConnections?.set(props.sessionId, conn);
  // Register a driver-side input sender for this session so plugins
  // (Quick Input) can pipe text through this same driver connection.
  // A fresh SessionConnection would attach as a viewer and have its
  // IN frames dropped by the relay.
  pluginInputSender = (text: string) => conn?.sendInput(text);
  pluginInputSenders?.set(props.sessionId, pluginInputSender);
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

// Templates + confirm-panel + hotkeys + wheel-scroll live in
// useQuickTemplates. The send() closure below wraps conn?.sendInput so
// the composable stays session-transport agnostic; the two-call split
// for text + CR lives inside sendTemplate (see [[feedback_template_send_split_cr]]).
const {
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
  reload: reloadQuickTemplates,
} = useQuickTemplates({
  send: (text) => conn?.sendInput(text),
  confirmRequired: computed(
    () => !(platform.caps.wailsBindings || platform.caps.localPty),
  ),
  focused: () => props.focused,
  templatesBridge: platform.templates,
});

function sendAuxPointer(seq: string, event: PointerEvent) {
  if (!shouldSendShortcutPointer(event)) return;
  if (!auxKeysCanSend.value) return;
  conn?.sendInput(seq);
  if (keyboardWanted.value) focusTerminalIfDriver();
}

function sendAuxTouch(seq: string, event: TouchEvent) {
  onKeyboardControlTouchEnd(event, () => {
    if (!auxKeysCanSend.value) return;
    lastShortcutTouchActionAt = Date.now();
    conn?.sendInput(seq);
    if (keyboardWanted.value) focusTerminalIfDriver();
  });
}

async function openPasteConfirm() {
  if (!auxKeysCanSend.value) return;
  pasteConfirmText.value = "";
  pasteConfirmOpen.value = true;
  try {
    pasteConfirmText.value = await (navigator.clipboard?.readText?.() ?? Promise.resolve(""));
  } catch {
    pasteConfirmText.value = "";
  }
}

function closePasteConfirm() {
  pasteConfirmOpen.value = false;
  pasteConfirmText.value = "";
}

function confirmMobilePaste() {
  if (!auxKeysCanSend.value || !pasteConfirmText.value) return;
  conn?.sendInput(pasteConfirmText.value);
  closePasteConfirm();
}

function openFilePicker() {
  if (!pasteBlobCanSend.value) return;
  if (filePicker.value) filePicker.value.value = "";
  filePicker.value?.click();
}

async function onFilePicked(event: Event) {
  if (!pasteBlobCanSend.value) return;
  const input = event.currentTarget as HTMLInputElement | null;
  const file = input?.files?.[0];
  if (!file) return;
  const name = file.name || "file";
  try {
    await conn?.sendPasteFile(file, name);
    pasteFileBus.emit(name, file.size);
  } catch (err: any) {
    console.warn("[AT Term] failed to send picked file", err);
    emit("toast", err?.message ?? t("terminal.pasteFailed"));
  } finally {
    if (input) input.value = "";
  }
}

function openImagePicker() {
  if (!pasteBlobCanSend.value) return;
  if (platform.caps.capacitor) {
    void openNativeImagePicker();
    return;
  }
  if (imagePicker.value) imagePicker.value.value = "";
  imagePicker.value?.click();
}

function base64ToFile(base64: string, mime: string, name: string): File {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return new File([bytes], name, { type: mime });
}

async function openNativeImagePicker() {
  if (!pasteBlobCanSend.value) return;
  try {
    const photo = await Camera.getPhoto({
      source: CameraSource.Prompt,
      resultType: CameraResultType.Base64,
      quality: 90,
      promptLabelHeader: t("mobile.image.prompt"),
      promptLabelPhoto: t("mobile.image.fromLibrary"),
      promptLabelPicture: t("mobile.image.takePhoto"),
      promptLabelCancel: t("common.cancel"),
    });
    if (!photo.base64String || !pasteBlobCanSend.value) return;
    const ext = photo.format || "jpeg";
    const file = base64ToFile(photo.base64String, `image/${ext}`, `mobile-image.${ext}`);
    pasteImageBus.emit(file, file.name);
    await conn?.sendPasteImage(file, file.name);
  } catch (err) {
    const message = String((err as { message?: string })?.message ?? err);
    if (!/cancel/i.test(message)) console.warn("[AT Term] image picker failed:", message);
  }
}

async function onImagePicked(event: Event) {
  if (!pasteBlobCanSend.value) return;
  const input = event.currentTarget as HTMLInputElement | null;
  const file = input?.files?.[0];
  if (!file) return;
  const name = file.name || "picked-image";
  try {
    pasteImageBus.emit(file, name);
    await conn?.sendPasteImage(file, name);
  } catch (err: any) {
    console.warn("[AT Term] failed to send picked image", err);
    emit("toast", err?.message ?? t("terminal.pasteFailed"));
  } finally {
    if (input) input.value = "";
  }
}

// Wheel-to-horizontal-scroll for the template bar. Trackpads emit deltaX
// natively when scrolled sideways, but a vertical mouse wheel — the common
// case when the cursor is hovering over a one-row strip — only emits deltaY.
// Folding deltaY into scrollLeft gives users a working scroll without
// reaching for shift. Listener is .passive (we don't preventDefault), so
// xterm's keyboard/mouse pipeline downstream is untouched.
// Re-read the persisted template list + hidden flag + aux keys. Wired to
// the 'quickTemplates:changed' / 'mobile:shortcutsChanged' events the
// Settings page emits so an open terminal updates immediately, without
// a remount. Delegates templates to reloadQuickTemplates and reloads
// auxKeys inline (aux keys live in the parent for now).
async function reloadShortcutBars() {
  const [_, nextAuxKeys] = await Promise.all([
    reloadQuickTemplates(),
    effectiveAuxKeys(platform.auxKeys),
  ]);
  auxKeys.value = nextAuxKeys;
}

let templatesOff: (() => void) | null = null;
let shortcutsOff: (() => void) | null = null;
let prefsChangedOff: (() => void) | null = null;

onMounted(async () => {
  await ensureTerm();
  if (!isAlive) return;
  // Resolve the local hostname before opening the WS so the very first ATTACH
  // carries the correct client_name. Failure (e.g. Wails not ready in tests
  // or a future browser-only build) falls back to the default in connection.ts.
  try {
    const info = await getHostInfo();
    if (info?.host) localHostname.value = info.host;
  } catch {
    /* fall back to default */
  }
  if (!isAlive) return;
  if (props.active) startConnection();
  reloadShortcutBars();
  templatesOff = platform.events.on("quickTemplates:changed", reloadShortcutBars);
  shortcutsOff = platform.events.on("mobile:shortcutsChanged", reloadShortcutBars);
  // prefsSync (Go / Capacitor / Web) writes synced values straight into the
  // adapter and then fires this event — templates come down that path when
  // the user logs in on a new device. Without this listener the bottom
  // template bar keeps rendering DEFAULT_TEMPLATES / the pre-sync list
  // until a manual edit or a full remount.
  prefsChangedOff = platform.events.on("prefs:changed", reloadShortcutBars);
  document.addEventListener("mousedown", onDocumentMouseDown);
  document.addEventListener("pointerdown", onDocumentPointerDown, { capture: true });
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
    if (!isAlive) return;
    safeFit();
    requestAnimationFrame(() => {
      if (isAlive) safeFit();
    });
  });
});

onBeforeUnmount(() => {
  isAlive = false;
  // Drop every external callback that could re-enter the term BEFORE we
  // touch conn / term. A queued ResizeObserver entry or a stray document
  // listener firing in the same tick used to call safeFit() → fit.fit() on
  // a half-disposed term, and the renderer race in xterm's RenderService
  // would throw synchronously back into Vue's unmount hook.
  resizeObserver?.disconnect();
  resizeObserver = null;
  clearSoftKeyboardOpenFocusTimers();
  softKeyboardOpening.value = false;
  clearKeyboardScrollRestoreTimers();
  pendingKeyboardScrollPosition = null;
  templatesOff?.();
  templatesOff = null;
  shortcutsOff?.();
  shortcutsOff = null;
  prefsChangedOff?.();
  prefsChangedOff = null;
  document.removeEventListener("mousedown", onDocumentMouseDown);
  document.removeEventListener("pointerdown", onDocumentPointerDown, { capture: true } as EventListenerOptions);
  document.removeEventListener("keydown", onDocumentKeyDown);
  document.removeEventListener("keydown", onTemplateHotkey, true);
  copyKeyTarget?.removeEventListener("keydown", handleCopyShortcut, { capture: true } as EventListenerOptions);
  copyKeyTarget?.removeEventListener("keydown", handleCtrlVKeydownPaste, { capture: true } as EventListenerOptions);
  document.removeEventListener("keydown", handleViewerKeydown, { capture: true } as EventListenerOptions);
  copyKeyTarget?.removeEventListener("paste", handleImagePaste, { capture: true } as EventListenerOptions);
  copyKeyTarget?.removeEventListener("pointermove", onSelectionPointerMove);
  copyKeyTarget?.removeEventListener("pointerup", onSelectionPointerUp);
  copyKeyTarget?.removeEventListener("pointercancel", onSelectionPointerCancel);
  copyKeyTarget?.removeEventListener("touchmove", onTermTouchMoveForRestoreCancel);
  copyKeyTarget = null;
  terminalTouchViewport?.removeEventListener("touchmove", stopTerminalTouchMove);
  terminalTouchViewport?.removeEventListener("scroll", onSelectionViewportScroll);
  terminalTouchViewport?.removeEventListener("wheel", onViewportUserScrollIntent);
  terminalTouchViewport = null;
  imeInputTarget?.removeEventListener("input", onImeInput as EventListener, { capture: true } as EventListenerOptions);
  imeInputTarget?.removeEventListener("focus", onImeFocus);
  imeInputTarget?.removeEventListener("blur", onImeBlur);
  imeInputTarget = null;
  if (pluginInputSenders?.get(props.sessionId) === pluginInputSender) {
    pluginInputSenders?.delete(props.sessionId);
  }
  pluginInputSender = null;
  if (pluginSessionConnections?.get(props.sessionId) === conn) {
    pluginSessionConnections?.delete(props.sessionId);
  }
  focusCoalescer?.dispose();
  focusCoalescer = null;
  exitSelection();
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
      if (conn) conn.attach();
      else startConnection();
      // Tab just gained focus — recompute size and let xterm refocus its
      // input so keystrokes go to this term instead of the body.
      nextTick(() => {
        safeFit();
        // conn already exists here, so startConnection() returned early and
        // its expectedCols/expectedRows comparison never ran. Reconcile the
        // PTY in case it shrank while this pane was suspended.
        syncPtySizeToTerm();
        focusTerminalForPaneActivation();
      });
    } else {
      conn?.suspend();
      closeContextMenu();
    }
  }
);

// Pane swap inside an already-active tab: the active-watch above doesn't fire
// (props.active stayed true), so xterm never learns the focus moved. A manual
// mousedown on the pane goes via the DOM and lands focus on xterm's textarea
// naturally; a programmatic set-active-pane (e.g. sidebar click on a session
// living in a multi-pane tab) doesn't. Watch `focused` independently so the
// new active pane's xterm always gets keystrokes.
watch(
  () => props.focused,
  (isFocused) => {
    if (isFocused) {
      nextTick(() => {
        focusTerminalForPaneActivation();
      });
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
    <div
      ref="termContainer"
      class="term"
      :style="{ bottom: terminalBottom }"
      @contextmenu.prevent="openContextMenu"
      @mouseup.capture="onTerminalMouseUp"
      @touchstart.capture="onTermTouchStart"
      @pointerdown.capture="onTermPointerDown"
      @mousedown.capture="onTermMouseDown"
    ></div>
    <TerminalSelectionPopover
      :visible="selectionPopover.visible"
      :x="selectionPopover.x"
      :y="selectionPopover.y"
      :arrow-dir="selectionPopover.arrowDir"
      :copying="selectionPopover.copying"
      :sending="selectionPopover.sending"
      @copy="onSelectionCopy"
      @send="onSelectionSend"
      @cancel="exitSelection"
    />
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
        <button ref="takeControlBtnRef" class="viewer-overlay-btn" data-testid="take-control" @click="takeControl">{{ t("terminal.takeControl") }}</button>
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
      :style="{ bottom: templateBarBottom }"
      @pointerdown.capture="onKeyboardControlPointerDown"
      @wheel.passive="onTemplateBarWheel"
    >
      <button
        v-for="tpl in templates"
        :key="tpl.id"
        class="template-btn"
        :data-testid="`template-btn-${tpl.id}`"
        :title="tpl.hotkey || ''"
        tabindex="-1"
        @touchstart="onKeyboardControlTouchStart"
        @touchmove="onKeyboardControlTouchMove"
        @touchcancel="onKeyboardControlTouchCancel"
        @touchend="onKeyboardControlTouchEnd($event, () => requestTemplateSend(tpl))"
        @pointerdown="onKeyboardControlPointerDown"
        @click="onKeyboardControlClick(() => requestTemplateSend(tpl))"
      >{{ tpl.label }}</button>
    </div>
    <div
      v-if="showAuxKeyBar"
      class="aux-key-bar"
      data-testid="terminal-aux-key-bar"
      @pointerdown.capture="onKeyboardControlPointerDown"
    >
      <input
        ref="filePicker"
        class="file-picker-input"
        data-testid="terminal-file-input"
        type="file"
        @change="onFilePicked"
      />
      <input
        ref="imagePicker"
        class="file-picker-input"
        data-testid="terminal-image-input"
        type="file"
        accept="image/*"
        @change="onImagePicked"
      />
      <button
        type="button"
        class="aux-key-btn keyboard-toggle-btn"
        data-testid="terminal-hide-keyboard"
        tabindex="-1"
        :title="softKeyboardCollapsed ? 'Show keyboard' : 'Hide keyboard'"
        @touchstart.prevent.stop="onAuxKeyboardControlTouchStart"
        @touchmove="onKeyboardControlTouchMove"
        @touchcancel="onKeyboardControlTouchCancel"
        @touchend.stop="onKeyboardControlTouchEnd($event, toggleSoftKeyboard)"
        @pointerdown.stop="onKeyboardControlPointerDown"
        @click.prevent.stop="onKeyboardControlClick(toggleSoftKeyboard)"
      >{{ softKeyboardCollapsed ? '打开键盘 ⌃' : '折叠键盘 ⌄' }}</button>
      <div
        class="aux-key-scroll"
        data-testid="terminal-aux-key-scroll"
        @wheel.passive="onTemplateBarWheel"
      >
        <button
          v-for="key in auxKeys"
          :key="key.id"
          type="button"
          class="aux-key-btn"
          :data-testid="`terminal-aux-key-${key.id}`"
          :disabled="!auxKeysCanSend"
          tabindex="-1"
          @touchstart.prevent="onAuxKeyboardControlTouchStart"
          @touchmove="onKeyboardControlTouchMove"
          @touchcancel="onKeyboardControlTouchCancel"
          @touchend="sendAuxTouch(key.seq, $event)"
          @pointerdown="onShortcutPointerDown"
          @pointermove="onShortcutPointerMove"
          @pointercancel="onShortcutPointerCancel"
          @pointerup="sendAuxPointer(key.seq, $event)"
        >{{ key.label }}</button>
        <button
          type="button"
          class="aux-key-btn"
          data-testid="terminal-pick-image"
          :disabled="!pasteBlobCanSend"
          @touchstart.prevent="onAuxKeyboardControlTouchStart"
          @touchmove="onKeyboardControlTouchMove"
          @touchcancel="onKeyboardControlTouchCancel"
          @touchend="onKeyboardControlTouchEnd($event, openImagePicker)"
          @pointerdown="onKeyboardControlPointerDown"
          @click="onKeyboardControlClick(openImagePicker)"
        >{{ t("terminal.pickImage") }}</button>
        <button
          type="button"
          class="aux-key-btn"
          data-testid="terminal-paste-confirm-open"
          :disabled="!auxKeysCanSend"
          @touchstart.prevent="onAuxKeyboardControlTouchStart"
          @touchmove="onKeyboardControlTouchMove"
          @touchcancel="onKeyboardControlTouchCancel"
          @touchend="onKeyboardControlTouchEnd($event, openPasteConfirm)"
          @pointerdown="onKeyboardControlPointerDown"
          @click="onKeyboardControlClick(openPasteConfirm)"
        >{{ t("mobile.pasteClipboard") }}</button>
        <button
          type="button"
          class="aux-key-btn"
          data-testid="terminal-pick-file"
          :disabled="!pasteBlobCanSend"
          @touchstart.prevent="onAuxKeyboardControlTouchStart"
          @touchmove="onKeyboardControlTouchMove"
          @touchcancel="onKeyboardControlTouchCancel"
          @touchend="onKeyboardControlTouchEnd($event, openFilePicker)"
          @pointerdown="onKeyboardControlPointerDown"
          @click="onKeyboardControlClick(openFilePicker)"
        >{{ t("terminal.pickFile") }}</button>
      </div>
    </div>
    <div
      v-if="templateConfirmOpen"
      class="template-confirm-panel"
      data-testid="template-confirm-panel"
      @pointerdown.capture="onKeyboardControlPointerDown"
      @click.stop
    >
      <div class="template-confirm-copy">
        <div class="template-confirm-label">{{ pendingTemplateConfirm?.label }}</div>
        <div class="template-confirm-text">{{ pendingTemplateConfirm?.text }}</div>
      </div>
      <button
        type="button"
        class="template-confirm-btn"
        data-testid="template-confirm-cancel"
        tabindex="-1"
        @touchstart="onKeyboardControlTouchStart"
        @touchmove="onKeyboardControlTouchMove"
        @touchcancel="onKeyboardControlTouchCancel"
        @touchend="onKeyboardControlTouchEnd($event, cancelTemplateSend)"
        @pointerdown="onKeyboardControlPointerDown"
        @click="onKeyboardControlClick(cancelTemplateSend)"
      >{{ t("common.cancel") }}</button>
      <button
        type="button"
        class="template-confirm-btn primary"
        data-testid="template-confirm-send"
        tabindex="-1"
        @touchstart="onKeyboardControlTouchStart"
        @touchmove="onKeyboardControlTouchMove"
        @touchcancel="onKeyboardControlTouchCancel"
        @touchend="onKeyboardControlTouchEnd($event, confirmTemplateSend)"
        @pointerdown="onKeyboardControlPointerDown"
        @click="onKeyboardControlClick(confirmTemplateSend)"
      >发送</button>
    </div>
    <div
      v-if="pasteConfirmOpen"
      class="paste-confirm-panel"
      data-testid="terminal-paste-confirm-panel"
      @pointerdown.capture="onKeyboardControlPointerDown"
      @click.stop
    >
      <textarea
        v-model="pasteConfirmText"
        class="paste-confirm-input"
        :placeholder="t('mobile.pastePreview')"
        rows="2"
      ></textarea>
      <button
        type="button"
        class="paste-confirm-btn"
        data-testid="terminal-paste-cancel"
        tabindex="-1"
        @touchstart="onKeyboardControlTouchStart"
        @touchmove="onKeyboardControlTouchMove"
        @touchcancel="onKeyboardControlTouchCancel"
        @touchend="onKeyboardControlTouchEnd($event, closePasteConfirm)"
        @pointerdown="onKeyboardControlPointerDown"
        @click="onKeyboardControlClick(closePasteConfirm)"
      >{{ t("common.cancel") }}</button>
      <button
        type="button"
        class="paste-confirm-btn primary"
        data-testid="terminal-paste-confirm"
        :disabled="!auxKeysCanSend || !pasteConfirmText"
        tabindex="-1"
        @touchstart="onKeyboardControlTouchStart"
        @touchmove="onKeyboardControlTouchMove"
        @touchcancel="onKeyboardControlTouchCancel"
        @touchend="onKeyboardControlTouchEnd($event, confirmMobilePaste)"
        @pointerdown="onKeyboardControlPointerDown"
        @click="onKeyboardControlClick(confirmMobilePaste)"
      >{{ t("mobile.pasteConfirm") }}</button>
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
.aux-key-bar {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 30px;
  display: flex;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  padding: 2px 8px;
  border-top: 1px solid var(--border);
  background: var(--panel);
  z-index: 4;
}
.aux-key-scroll {
  min-width: 0;
  flex: 1 1 auto;
  height: 100%;
  display: flex;
  align-items: center;
  gap: 6px;
  overflow-x: auto;
  scrollbar-width: none;
}
.aux-key-scroll::-webkit-scrollbar { display: none; }
.file-picker-input {
  display: none;
}
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
.aux-key-btn {
  flex: 0 0 auto;
  min-width: 34px;
  height: 22px;
  padding: 0 9px;
  border-radius: 6px;
  background: var(--bg);
  border: 1px solid var(--border);
  color: var(--fg);
  font-size: 0.76rem;
  font-family: var(--font-mono);
  cursor: pointer;
}
.aux-key-btn:hover:not(:disabled) {
  border-color: var(--accent);
  color: var(--accent);
}
.aux-key-btn:disabled {
  color: var(--fg-dim);
  cursor: default;
  opacity: 0.55;
}
.keyboard-toggle-btn {
  min-width: 76px;
  font-weight: 600;
}
.template-confirm-panel {
  position: absolute;
  left: 8px;
  right: 8px;
  bottom: calc(60px + env(safe-area-inset-bottom));
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 6px;
  align-items: center;
  padding: 7px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
  z-index: 6;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
}
.template-confirm-copy {
  min-width: 0;
}
.template-confirm-label {
  color: var(--fg);
  font: 600 0.78rem var(--font-mono);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.template-confirm-text {
  margin-top: 2px;
  color: var(--fg-dim);
  font: 0.72rem var(--font-mono);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.template-confirm-btn {
  height: 30px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--fg);
  padding: 0 10px;
  cursor: pointer;
}
.template-confirm-btn.primary {
  border-color: var(--accent);
  color: var(--accent);
}
.paste-confirm-panel {
  position: absolute;
  left: 8px;
  right: 8px;
  bottom: calc(30px + env(safe-area-inset-bottom));
  display: grid;
  grid-template-columns: 1fr auto auto;
  gap: 6px;
  align-items: center;
  padding: 7px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
  z-index: 6;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
}
.paste-confirm-input {
  min-width: 0;
  resize: vertical;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--terminal-bg);
  color: var(--fg);
  padding: 6px 8px;
  font: 0.78rem var(--font-mono);
}
.paste-confirm-btn {
  height: 30px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--fg);
  padding: 0 10px;
  cursor: pointer;
}
.paste-confirm-btn.primary {
  border-color: var(--accent);
  color: var(--accent);
}
.paste-confirm-btn:disabled {
  color: var(--fg-dim);
  border-color: var(--border);
  cursor: default;
  opacity: 0.55;
}
.term :deep(.xterm) {
  /* FitAddon subtracts padding from the xterm element, not this host. */
  padding: 6px 8px;
}
.term :deep(.xterm-viewport) {
  top: 6px;
  right: 8px;
  bottom: 6px;
  left: 8px;
  /* Stack the scrollable viewport ABOVE .xterm-screen (which xterm renders
     as a sibling immediately after — .xterm-screen wins the default stacking
     otherwise, so a finger landing on rendered text never reaches viewport
     and native `touch-action: pan-y` scroll never fires). Transparent bg is
     required because xterm's default is opaque #000, which would hide the
     canvas beneath. See xterm.js #3613 / #512 (scrollbar is unclickable). */
  z-index: 5;
  background: transparent !important;
  touch-action: pan-y;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior: contain;
  user-select: none;
  -webkit-user-select: none;
  -webkit-touch-callout: none;
}
.term :deep(.xterm-helper-textarea) {
  caret-color: transparent;
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
  pointer-events: auto;
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
  pointer-events: auto;
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
