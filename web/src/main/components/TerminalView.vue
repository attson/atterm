<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { Terminal, type ITheme } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import 'xterm/css/xterm.css'
import { NAlert } from 'naive-ui'
import { SessionConnection, type SessionStatus } from '@shared/ws/client-conn'
import { useI18n } from '@shared/i18n/useI18n'

const props = defineProps<{
  sessionId: string
  focusInput?: boolean
}>()

const emit = defineEmits<{
  (e: 'request-paste'): void
  (e: 'request-copy'): void
}>()

const status = ref<SessionStatus>('connecting')
const replay = ref<{ bytes: number; total_bytes: number } | null>(null)
const isDriver = ref(true)
const termContainer = ref<HTMLDivElement | null>(null)
const { t } = useI18n()

let term: Terminal | null = null
let fit: FitAddon | null = null
let conn: SessionConnection | null = null
let resizeObserver: ResizeObserver | null = null

const theme: ITheme = { background: '#000000' }
const statusLabel = computed(() => t(`terminal.statuses.${status.value}`))

function pct(): number {
  if (!replay.value || replay.value.total_bytes === 0) return 0
  return Math.min(100, Math.round((replay.value.bytes / replay.value.total_bytes) * 100))
}

function safeFit() {
  if (!fit || !term) return
  try {
    fit.fit()
  } catch {
    /* container not laid out yet */
  }
}

function buildTerm() {
  term = new Terminal({
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
    fontSize: 13,
    theme,
    cursorBlink: true,
    convertEol: false,
    scrollback: 20000,
    allowProposedApi: true,
  })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(termContainer.value!)
  safeFit()
  if (props.focusInput) {
    term.focus()
  }

  term.onData((data) => {
    conn?.sendInput(data)
  })
  term.onResize(({ cols, rows }) => {
    conn?.sendResize(cols, rows)
  })

  resizeObserver = new ResizeObserver(() => safeFit())
  resizeObserver.observe(termContainer.value!)
}

function buildConn() {
  conn = new SessionConnection(props.sessionId, {
    onOutput: (data) => {
      term?.write(data)
    },
    onMeta: (_meta) => {
      // Future: surface PTY title / cwd in the page bar. PR-E keeps it simple.
    },
    onClose: (info) => {
      term?.write(`\r\n\x1b[33m[AT Term] ${t('terminal.ended', { code: info.exit_code })}\x1b[0m\r\n`)
    },
    onReplayProgress: (p) => {
      const phase = String((p as { phase?: unknown }).phase)
      if (phase === 'end') {
        replay.value = null
      } else {
        replay.value = {
          bytes: Number((p as { bytes?: unknown }).bytes ?? 0),
          total_bytes: Number((p as { total_bytes?: unknown }).total_bytes ?? 0),
        }
      }
    },
    onStatus: (s) => {
      status.value = s
    },
    onDriverChange: (_id, isMe) => {
      isDriver.value = isMe
      if (term) term.options.disableStdin = !isMe
    },
  })
  conn.attach()
}

function takeControl() {
  conn?.claimDriver()
}

onMounted(() => {
  buildTerm()
  buildConn()
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  resizeObserver = null
  conn?.detach()
  conn = null
  term?.dispose()
  term = null
  fit = null
})

watch(
  () => props.sessionId,
  () => {
    conn?.detach()
    conn = null
    term?.reset()
    replay.value = null
    buildConn()
  },
)

watch(
  () => props.focusInput,
  (focus) => {
    if (focus) term?.focus()
  },
)

defineExpose({
  sendInput(s: string) {
    conn?.sendInput(s)
  },
  sendPasteImage(blob: Blob, filename?: string) {
    return conn?.sendPasteImage(blob, filename)
  },
  copySelection() {
    if (!term) return
    const sel = term.getSelection()
    if (sel) {
      void navigator.clipboard.writeText(sel).catch(() => {})
    }
  },
})
</script>

<template>
  <section class="term-view">
    <n-alert
      v-if="status === 'lost'"
      type="warning"
      :show-icon="true"
      class="lost-banner"
      data-testid="lost-banner"
    >
      {{ t('terminal.cannotReachRelay') }}
      <a href="/setup.html">{{ t('terminal.tapToChangeConfig') }}</a>
    </n-alert>
    <div class="term-wrap">
      <div ref="termContainer" class="term"></div>
      <div
        v-if="replay"
        class="replay-overlay"
        data-testid="replay-progress"
      >
        <div class="replay-text">{{ t('terminal.loadingHistory', { percent: pct() }) }}</div>
        <div class="replay-track" aria-hidden="true">
          <div class="replay-fill" :style="{ width: pct() + '%' }"></div>
        </div>
      </div>
      <div v-if="!isDriver" class="viewer-overlay" data-testid="viewer-overlay">
        <div class="viewer-card">
          <div class="viewer-title">{{ t('terminal.remoteControl') }}</div>
          <button class="take-control" data-testid="take-control" @click="takeControl">{{ t('terminal.takeControl') }}</button>
        </div>
      </div>
    </div>
    <p class="status-line" data-testid="status-line">{{ statusLabel }}</p>
  </section>
</template>

<style scoped>
.term-view {
  display: flex;
  flex-direction: column;
  /* flex:1 + min-height:0 so we stretch inside our flex-column parent
     (App.vue's .home-main uses min-height not height, which means a
     child's height:100% degrades to content-auto and the xterm box
     collapses). Stretch via flex instead. */
  flex: 1;
  min-height: 0;
  background: var(--bg);
  color: var(--fg);
}
.term-wrap { position: relative; flex: 1; min-height: 0; }
.term { position: absolute; inset: 0; }
.replay-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.7);
  color: var(--fg);
  gap: 0.5rem;
  pointer-events: none;
}
.replay-text { font-size: 0.875rem; }
.replay-track { width: 200px; height: 4px; background: var(--border); border-radius: 2px; overflow: hidden; }
.replay-fill { height: 100%; background: var(--accent); transition: width 0.1s linear; }
.viewer-overlay {
  position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
  background: rgba(0, 0, 0, 0.6);
}
.viewer-card { display: flex; flex-direction: column; gap: 0.75rem; align-items: center; }
.viewer-title { font-size: 0.9rem; color: var(--fg); }
.take-control {
  padding: 0.5rem 1rem; border: none; border-radius: 8px;
  background: var(--accent, #3b82f6); color: #fff; font-weight: 600; cursor: pointer;
}
.status-line {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.75rem;
  color: var(--fg-dim);
  padding: 0.25rem 0.5rem;
  margin: 0;
}
.lost-banner {
  margin: 0;
  border-radius: 0;
}
</style>
