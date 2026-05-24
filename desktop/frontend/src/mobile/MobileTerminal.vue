<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import { WebglAddon } from 'xterm-addon-webgl'
import 'xterm/css/xterm.css'
import { SessionConnection, type Endpoint } from '../lib/connection'
import type { RemoteSession } from '../platform/types'

const props = defineProps<{
  endpoint: Endpoint
  sessionId: string
  info: RemoteSession
  active: boolean
}>()
const emit = defineEmits<{ (e: 'ended'): void; (e: 'tokenInvalid'): void; (e: 'meta', m: { cwd?: string; title?: string }): void }>()

const container = ref<HTMLDivElement | null>(null)
const isDriver = ref(true)
let term: Terminal | null = null
let fit: FitAddon | null = null
let conn: SessionConnection | null = null

function decode(data: Uint8Array): string {
  return new TextDecoder().decode(data)
}

const AUX_KEYS: { label: string; seq: string }[] = [
  { label: 'esc', seq: '\x1b' },
  { label: 'tab', seq: '\t' },
  { label: '⌃C', seq: '\x03' },
  { label: '↑', seq: '\x1b[A' },
  { label: '↓', seq: '\x1b[B' },
  { label: '←', seq: '\x1b[D' },
  { label: '→', seq: '\x1b[C' },
]
function sendAux(seq: string) { conn?.sendInput(seq) }
function takeControl() { conn?.claimDriver() }

onMounted(() => {
  term = new Terminal({ fontSize: 12, convertEol: false, cursorBlink: true })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(container.value!)
  // GPU renderer — the DOM renderer repaints every row on each scroll frame,
  // which makes touch scrollback stutter on the phone. Load after open() so the
  // context binds to the live <canvas>; fall back to DOM on construction or
  // runtime context loss.
  try {
    const webgl = new WebglAddon()
    webgl.onContextLoss(() => webgl.dispose())
    term.loadAddon(webgl)
  } catch (err) {
    console.warn('[AT Term] WebGL renderer unavailable, falling back to DOM', err)
  }
  // Restore iOS inertial scrolling. xterm's own touchmove handler (on the
  // .xterm root) does 1:1 scrollTop tracking + preventDefault, which kills
  // native fling/momentum. Stop touchmove from bubbling to that handler so the
  // viewport's native overflow scroll takes over; xterm still re-renders rows
  // from the resulting 'scroll' events.
  container.value!.querySelector('.xterm-viewport')
    ?.addEventListener('touchmove', (e) => e.stopPropagation(), { passive: true })
  try { fit.fit() } catch { /* not laid out yet */ }
  term.onData((s: string) => conn?.sendInput(s))
  term.onResize(({ cols, rows }) => conn?.sendResize(cols, rows))

  conn = new SessionConnection(props.endpoint, props.sessionId, {
    onOutput: (data) => term?.write(decode(data)),
    onMeta: (meta) => emit('meta', { cwd: meta.cwd, title: meta.title }),
    onClose: () => emit('ended'),
    // Defensive: route a hard 'error' status to setup. NOTE: SessionConnection
    // reconnect-loops (status 'reconnecting') on a WS auth close rather than
    // emitting 'error', so the *primary* token-invalid guard is the HTTP path
    // (listRemoteSessions/fetchMe 401 → MobileApp.onTokenInvalid). Mapping the
    // WS auth-close code to token-invalid is a PR-D follow-up.
    onStatus: (s) => { if (s === 'error') emit('tokenInvalid') },
    onDriverChange: (_id, isMe) => {
      isDriver.value = isMe
      if (term) {
        term.options.disableStdin = !isMe
        if (isMe && term.cols > 0 && term.rows > 0) {
          // Became driver: push our (phone) size so the PTY — and every other
          // viewer, e.g. the desktop owner — follows. Without this the PTY
          // keeps the previous driver's (wider) dims and the desktop viewer
          // never shrinks to match the phone.
          conn?.sendResize(term.cols, term.rows)
        }
      }
    },
  })
  conn.attach()
})

watch(() => props.active, (now) => {
  if (now) {
    // xterm could not measure while hidden (v-show); re-fit + focus on activate.
    requestAnimationFrame(() => { try { fit?.fit() } catch { /* */ } ; term?.focus() })
  }
})

onBeforeUnmount(() => {
  conn?.detach()
  conn = null
  term?.dispose()
  term = null
  fit = null
})
</script>

<template>
  <div class="mobile-term">
    <div ref="container" class="term"></div>
    <div v-if="!isDriver" class="viewer-overlay">
      <div class="viewer-card">
        <div class="viewer-title">remote has control</div>
        <button class="take-control" data-testid="mobile-take-control" @click="takeControl">Take control</button>
      </div>
    </div>
    <div class="kbbar">
      <button v-for="k in AUX_KEYS" :key="k.label" class="key" @click="sendAux(k.seq)">{{ k.label }}</button>
    </div>
  </div>
</template>

<style scoped>
.mobile-term { display: flex; flex-direction: column; height: 100%; background: #000; position: relative; }
.viewer-overlay { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; background: rgba(0,0,0,.55); }
.viewer-card { display: flex; flex-direction: column; align-items: center; gap: 10px; }
.viewer-title { color: #e6e7ea; font-size: 0.9rem; }
.take-control { padding: 8px 16px; border: none; border-radius: 8px; background: #3b82f6; color: #fff; font-weight: 600; }
.term { flex: 1; min-height: 0; }
/* Smooth, inertial scrollback on iOS: pan-y keeps the fling momentum (and
   disables double-tap/pinch zoom over the terminal), -webkit-overflow-scrolling
   is the legacy momentum flag, and overscroll-behavior stops the scroll from
   chaining to the page (which abruptly halts the fling at the edges). */
.term :deep(.xterm-viewport) {
  touch-action: pan-y;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior: contain;
}
/* xterm parks its opacity:0 input <textarea> on the cursor cell (for IME
   positioning); iOS draws the native blinking caret there, doubling up with
   xterm's own block cursor. Hide the native one. */
.term :deep(.xterm-helper-textarea) { caret-color: transparent; }
.kbbar { height: 42px; border-top: 1px solid #1e2638; background: #0b1020; display: flex; align-items: center; gap: 6px; padding: 0 8px; overflow-x: auto; }
.key { flex: 0 0 auto; height: 28px; min-width: 34px; padding: 0 9px; border-radius: 7px; background: #11182b; border: 1px solid #1e2638; color: #8d93a3; font-size: 0.75rem; font-family: ui-monospace, Menlo, monospace; }
</style>
