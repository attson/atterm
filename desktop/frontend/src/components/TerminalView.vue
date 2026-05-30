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
import { broadcastCommandFinished, getHostInfo, getWebglRendererEnabled, showNotification } from "../lib/api";
import { collectContextMenuItems } from "../plugins/contextMenuItems";
import { descriptorsForSlot } from "../plugins/registry";
import { usePluginConfigStore } from "../plugins/configStore";
import type { ContextMenuPlugin, MenuItem, PluginContext } from "../plugins/types";
import { useI18n } from "../i18n/useI18n";

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
  }>(),
  { active: true, focused: false, avoidTopRightBadge: false, commandNotifyThresholdSec: 10, isLocalSession: true }
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
// from META. Starts optimistic; first META corrects it.
const isDriver = ref(true);
const ptyCols = ref<number | null>(null);
const ptyRows = ref<number | null>(null);
// driverHostname is the human-readable name of whoever currently holds the
// driver role (broadcast via META.driver_client_name). Used in the viewer
// overlay's sub-line ("by <hostname>"). Empty when nobody or self drives.
const driverHostname = ref("");
// localHostname is this machine's hostname (from getHostInfo). Sent as
// client_name in ATTACH and CLAIM_DRIVER so other clients can label us.
const localHostname = ref("");

let term: Terminal | null = null;
let fit: FitAddon | null = null;
let conn: SessionConnection | null = null;

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

let resizeObserver: ResizeObserver | null = null;
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

async function ensureTerm() {
  if (term) return;
  term = new Terminal({
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
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
  term.onData((data) => {
    const { cleaned, dropped } = stripC1Controls(data);
    if (dropped.length > 0) {
      console.warn("[AT Term] dropped C1 control chars from terminal input", {
        droppedCodepoints: dropped.map((cp) => "U+" + cp.toString(16).toUpperCase().padStart(4, "0")),
        originalLength: data.length,
        cleanedLength: cleaned.length,
      });
    }
    if (cleaned) conn?.sendInput(cleaned);
  });
  term.onResize(({ cols, rows }) => {
    if (!isDriver.value) return; // viewer's local resize is FitAddon-suppressed anyway
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
    { clientName: localHostname.value }
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
  document.addEventListener("mousedown", onDocumentMouseDown);
  document.addEventListener("keydown", onDocumentKeyDown);
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
  document.removeEventListener("mousedown", onDocumentMouseDown);
  document.removeEventListener("keydown", onDocumentKeyDown);
  pluginInputSenders?.delete(props.sessionId);
  conn?.detach();
  conn = null;
  resizeObserver?.disconnect();
  resizeObserver = null;
  copyKeyTarget?.removeEventListener("keydown", handleCopyShortcut, { capture: true } as EventListenerOptions);
  copyKeyTarget?.removeEventListener("keydown", handleViewerKeydown, { capture: true } as EventListenerOptions);
  copyKeyTarget?.removeEventListener("paste", handleImagePaste, { capture: true } as EventListenerOptions);
  copyKeyTarget = null;
  term?.dispose();
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
  inset: 0;
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
