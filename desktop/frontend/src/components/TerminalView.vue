<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Terminal } from "xterm";
import type { ITheme } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import { SessionConnection, type Status } from "../lib/connection";
import type { Endpoint } from "../lib/api";
import { formatReplayProgress, progressPercent, type ReplayProgress } from "../lib/replayProgress";
import { copyTerminalSelection, isTerminalCopyShortcut } from "../lib/terminalCopy";
import { clampContextMenuPosition, isPasteAllowed } from "../lib/terminalContextMenu";
import { pasteFromClipboard } from "../lib/terminalPaste";

const props = withDefaults(
  defineProps<{
    endpoint: Endpoint;
    sessionId: string;
    active?: boolean;
    focused?: boolean;
    avoidTopRightBadge?: boolean;
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
  }>(),
  { active: true, focused: false, avoidTopRightBadge: false }
);

const emit = defineEmits<{
  (e: "toast", message: string): void;
}>();

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

let term: Terminal | null = null;
let fit: FitAddon | null = null;
let conn: SessionConnection | null = null;
let resizeObserver: ResizeObserver | null = null;
let copyKeyTarget: HTMLDivElement | null = null;

const MENU_WIDTH = 150;
const MENU_HEIGHT = 76;

const menuCanPaste = computed(() => isPasteAllowed(status.value, props.remotePermission));

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
}

function openContextMenu(e: MouseEvent) {
  if (!term) return;
  const pos = clampContextMenuPosition(
    e.clientX,
    e.clientY,
    MENU_WIDTH,
    MENU_HEIGHT,
    window.innerWidth,
    window.innerHeight,
  );
  menuHasSelection.value = !!term.getSelection();
  menuX.value = pos.left;
  menuY.value = pos.top;
  menuOpen.value = true;
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
    emit("toast", "copy failed");
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
    if (!result.ok && result.reason) {
      emit("toast", result.reason);
    }
  } catch (err: any) {
    console.warn("[AT Term] failed to paste from terminal menu", err);
    emit("toast", err?.message ?? "paste failed");
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

function ensureTerm() {
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
  const keyTarget = termContainer.value!;
  copyKeyTarget = keyTarget;
  keyTarget.addEventListener("keydown", handleCopyShortcut, { capture: true });
  keyTarget.addEventListener("paste", handleImagePaste, { capture: true });
  safeFit();
  term.onData((data) => conn?.sendInput(data));
  term.onResize(({ cols, rows }) => {
    if (!isDriver.value) return; // viewer's local resize is FitAddon-suppressed anyway
    conn?.sendResize(cols, rows);
  });

  resizeObserver = new ResizeObserver(() => safeFit());
  resizeObserver.observe(termContainer.value!);
}

function startConnection() {
  if (!term) return;
  conn = new SessionConnection(props.endpoint, props.sessionId, {
    onOutput: (data) => term?.write(data),
    onClose: (info) => {
      term?.write(
        `\r\n\x1b[33m[AT Term] session ended (exit ${info.exit_code})\x1b[0m\r\n`
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
    onDriverChange: (_driverID, isMe) => {
      const wasDriver = isDriver.value;
      isDriver.value = isMe;
      if (term) term.options.disableStdin = !isMe;
      applyViewerSize();
      if (wasDriver !== isMe) {
        emit("toast", isMe ? "you are now the driver" : "you are now a viewer");
      }
    },
  });
  conn.attach();
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

onMounted(() => {
  ensureTerm();
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
  conn?.detach();
  conn = null;
  resizeObserver?.disconnect();
  resizeObserver = null;
  copyKeyTarget?.removeEventListener("keydown", handleCopyShortcut, { capture: true } as EventListenerOptions);
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
      <span v-else-if="status === 'connecting'">connecting…</span>
      <span v-else-if="status === 'reconnecting'" class="warn">reconnecting…</span>
      <span v-else-if="status === 'ended'" class="dim">session ended</span>
      <span v-else-if="status === 'error'" class="bad">connection error</span>
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
        <button class="term-context-item" :disabled="!menuHasSelection" @click="onMenuCopy">copy</button>
        <button class="term-context-item" :disabled="!menuCanPaste || pasteBusy" @click="onMenuPaste">paste</button>
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
