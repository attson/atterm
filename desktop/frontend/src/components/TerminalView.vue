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
  if (term) conn.sendResize(term.cols, term.rows);
}

onMounted(() => {
  ensureTerm();
  startConnection();
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
