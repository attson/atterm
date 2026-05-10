<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref, watch, nextTick } from "vue";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import { SessionConnection, type Status } from "../lib/connection";
import type { Endpoint } from "../lib/api";

const props = withDefaults(
  defineProps<{
    endpoint: Endpoint;
    sessionId: string;
    active?: boolean;
    focused?: boolean;
    // The PTY's known size at the time of attach (from SessionInfo).
    // When this matches the local xterm's fit dimensions, we skip the
    // initial RESIZE so cross-attached remote shells don't see a
    // gratuitous SIGWINCH (which some prompt themes turn into a stray
    // '%' via PROMPT_EOL_MARK). Undefined → treat as unknown and
    // send the resize anyway (safe fallback).
    expectedCols?: number;
    expectedRows?: number;
  }>(),
  { active: true, focused: false }
);

const termContainer = ref<HTMLDivElement | null>(null);
const status = ref<Status>("connecting");

let term: Terminal | null = null;
let fit: FitAddon | null = null;
let conn: SessionConnection | null = null;
let resizeObserver: ResizeObserver | null = null;

function safeFit() {
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
        "[atterm] suspicious term size after fit",
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
    theme: { background: "#000000" },
    convertEol: false,
    allowProposedApi: true,
  });
  fit = new FitAddon();
  term.loadAddon(fit);
  term.open(termContainer.value!);
  safeFit();
  term.onData((data) => conn?.sendInput(data));
  term.onResize(({ cols, rows }) => conn?.sendResize(cols, rows));

  resizeObserver = new ResizeObserver(() => safeFit());
  resizeObserver.observe(termContainer.value!);
}

function startConnection() {
  if (!term) return;
  conn = new SessionConnection(props.endpoint, props.sessionId, {
    onOutput: (data) => term?.write(data),
    onClose: (info) => {
      term?.write(
        `\r\n\x1b[33m[atterm] session ended (exit ${info.exit_code})\x1b[0m\r\n`
      );
    },
    onStatus: (s) => {
      status.value = s;
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
  conn?.detach();
  conn = null;
  resizeObserver?.disconnect();
  resizeObserver = null;
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
    }
  }
);
</script>

<template>
  <div class="term-view" :class="{ focused }">
    <div ref="termContainer" class="term"></div>
    <div v-if="active && status !== 'attached'" class="overlay">
      <span v-if="status === 'connecting'">connecting…</span>
      <span v-else-if="status === 'reconnecting'" class="warn">reconnecting…</span>
      <span v-else-if="status === 'ended'" class="dim">session ended</span>
      <span v-else-if="status === 'error'" class="bad">connection error</span>
    </div>
  </div>
</template>

<style scoped>
.term-view {
  position: absolute;
  inset: 0;
  background: #000;
  overflow: hidden;
}
.term {
  position: absolute;
  inset: 0;
  padding: 6px 8px;
}
.overlay {
  position: absolute;
  top: 8px;
  right: 12px;
  background: rgba(13, 17, 23, 0.85);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 4px 10px;
  font-size: 12px;
  color: var(--fg-dim);
  pointer-events: none;
}
.overlay .warn { color: #d29922; }
.overlay .bad { color: var(--bad); }
.overlay .dim { color: var(--fg-dim); }
.term-view.focused {
  box-shadow: inset 0 0 0 1px var(--accent);
}
</style>
