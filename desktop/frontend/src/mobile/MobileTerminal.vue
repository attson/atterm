<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import { WebglAddon } from 'xterm-addon-webgl'
import 'xterm/css/xterm.css'
import { SessionConnection, type Endpoint } from '../lib/connection'
import type { RemoteSession } from '../platform/types'
import { useI18n } from '../i18n/useI18n'

const props = defineProps<{
  endpoint: Endpoint
  sessionId: string
  info: RemoteSession
  active: boolean
}>()
const emit = defineEmits<{ (e: 'ended'): void; (e: 'tokenInvalid'): void; (e: 'meta', m: { cwd?: string; title?: string }): void }>()
const { t } = useI18n()

const container = ref<HTMLDivElement | null>(null)
const imageInput = ref<HTMLInputElement | null>(null)
const isDriver = ref(true)
const controlMode = ref(false)
const pasteOpen = ref(false)
const pasteText = ref('')
let term: Terminal | null = null
let fit: FitAddon | null = null
let conn: SessionConnection | null = null

function decode(data: Uint8Array): string {
  return new TextDecoder().decode(data)
}

const AUX_KEYS: { id: string; label: string; seq: string }[] = [
  { id: 'enter', label: 'enter', seq: '\r' },
  { id: 'esc', label: 'esc', seq: '\x1b' },
  { id: 'tab', label: 'tab', seq: '\t' },
  { id: 'ctrl-c', label: '⌃C', seq: '\x03' },
  { id: 'ctrl-d', label: '⌃D', seq: '\x04' },
  { id: 'arrow-up', label: '↑', seq: '\x1b[A' },
  { id: 'arrow-down', label: '↓', seq: '\x1b[B' },
  { id: 'arrow-left', label: '←', seq: '\x1b[D' },
  { id: 'arrow-right', label: '→', seq: '\x1b[C' },
]
const QUICK_TEXTS = ['y', 'n', 'yes', 'no', 'continue']

const canControl = computed(() => (props.info.remote_permission || 'full') !== 'view')
const canSend = computed(() => canControl.value && controlMode.value && isDriver.value)

function refreshInputMode() {
  if (term) term.options.disableStdin = !canSend.value
}

function sendRaw(seq: string) {
  if (!canSend.value) return
  conn?.sendInput(seq)
}

function sendAux(seq: string) { sendRaw(seq) }
function sendQuick(text: string) { sendRaw(`${text}\r`) }
function takeControl() {
  if (!canControl.value) return
  conn?.claimDriver()
}

async function openPasteConfirm() {
  if (!canSend.value) return
  pasteText.value = ''
  pasteOpen.value = true
  try {
    pasteText.value = await (navigator.clipboard?.readText?.() ?? Promise.resolve(''))
  } catch {
    pasteText.value = ''
  }
}

function confirmPaste() {
  if (pasteText.value) sendRaw(pasteText.value)
  pasteOpen.value = false
}

function openImagePicker() {
  if (!canSend.value) return
  imageInput.value?.click()
}

async function onImagePicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  // Reset the input synchronously so picking the same file twice still fires
  // a 'change' event next time.
  input.value = ''
  if (!file || !canSend.value) return
  try {
    await conn?.sendPasteImage(file, file.name || 'mobile-image')
  } catch {
    // sendPasteImage already routes status='error' to MobileApp via onStatus;
    // a separate toast here would be redundant.
  }
}

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
  term.onData((s: string) => sendRaw(s))
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
      refreshInputMode()
      if (term) {
        if (isMe && canControl.value && term.cols > 0 && term.rows > 0) {
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
  refreshInputMode()
})

watch(canSend, refreshInputMode)

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
    <div ref="container" class="term" :class="{ inert: !isDriver }"></div>
    <div v-if="!isDriver" class="viewer-overlay">
      <div class="viewer-card">
        <div class="viewer-title">{{ t('terminal.remoteHasControl') }}</div>
        <button v-if="canControl" type="button" class="take-control" data-testid="mobile-take-control" @click.stop="takeControl">{{ t('terminal.takeControl') }}</button>
        <div v-else class="view-only-copy" data-testid="mobile-view-only-overlay">{{ t('mobile.viewOnly') }}</div>
      </div>
    </div>
    <div class="control-panel" data-testid="mobile-control-panel">
      <div v-if="!canControl" class="view-only" data-testid="mobile-view-only">{{ t('mobile.viewOnly') }}</div>
      <label class="control-toggle">
        <input
          v-model="controlMode"
          data-testid="mobile-control-toggle"
          type="checkbox"
          :disabled="!canControl || !isDriver"
        />
        <span>{{ t('mobile.controlMode') }}</span>
      </label>
      <div class="kbbar">
        <button
          v-for="k in AUX_KEYS"
          :key="k.id"
          class="key"
          :data-testid="`mobile-key-${k.id}`"
          :disabled="!canSend"
          @click="sendAux(k.seq)"
        >{{ k.label }}</button>
        <button class="key paste" data-testid="mobile-paste" :disabled="!canSend" @click="openPasteConfirm">{{ t('mobile.pasteClipboard') }}</button>
        <button class="key paste" data-testid="mobile-image" :disabled="!canSend" @click="openImagePicker">{{ t('mobile.pasteImage') }}</button>
        <input
          ref="imageInput"
          data-testid="mobile-image-input"
          type="file"
          accept="image/*"
          class="hidden-file"
          @change="onImagePicked"
        />
      </div>
      <div class="quickbar">
        <button
          v-for="text in QUICK_TEXTS"
          :key="text"
          class="quick"
          :data-testid="`mobile-quick-${text}`"
          :disabled="!canSend"
          @click="sendQuick(text)"
        >{{ text }}</button>
      </div>
      <div v-if="pasteOpen" class="paste-confirm" data-testid="mobile-paste-confirm-panel">
        <textarea v-model="pasteText" :placeholder="t('mobile.pastePreview')" rows="2"></textarea>
        <button type="button" data-testid="mobile-paste-cancel" @click="pasteOpen = false">{{ t('common.cancel') }}</button>
        <button type="button" data-testid="mobile-paste-confirm" :disabled="!canSend || !pasteText" @click="confirmPaste">{{ t('mobile.pasteConfirm') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mobile-term { display: flex; flex-direction: column; height: 100%; background: #000; position: relative; }
.viewer-overlay { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; background: rgba(0,0,0,.55); }
.viewer-card { display: flex; flex-direction: column; align-items: center; gap: 10px; }
.viewer-title { color: #e6e7ea; font-size: 0.9rem; }
.take-control { padding: 8px 16px; border: none; border-radius: 8px; background: #3b82f6; color: #fff; font-weight: 600; }
.view-only-copy { color: #fbbf24; font-size: 0.82rem; }
.term { flex: 1; min-height: 0; }
/* While the viewer overlay is up, swallow pointer events on the terminal so an
   iOS tap on the "Take control" button can't fall through to xterm's hidden
   <textarea> and open the on-screen keyboard. The button itself sits on the
   overlay (a sibling) and stays interactive. */
.term.inert { pointer-events: none; }
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
.control-panel { border-top: 1px solid #1e2638; background: #0b1020; padding: 7px 8px calc(7px + env(safe-area-inset-bottom)); display: flex; flex-direction: column; gap: 7px; }
.control-toggle { display: inline-flex; align-items: center; gap: 7px; color: #cbd5e1; font-size: 0.78rem; user-select: none; }
.control-toggle input { accent-color: #3b82f6; }
.view-only { border: 1px solid rgba(251,191,36,.34); border-radius: 8px; padding: 6px 8px; color: #fbbf24; background: rgba(251,191,36,.09); font-size: 0.75rem; }
.kbbar, .quickbar { display: flex; align-items: center; gap: 6px; overflow-x: auto; }
.key, .quick { flex: 0 0 auto; height: 28px; min-width: 34px; padding: 0 9px; border-radius: 7px; background: #11182b; border: 1px solid #1e2638; color: #cbd5e1; font-size: 0.75rem; font-family: var(--font-mono); }
.quick { font-family: inherit; min-width: 42px; }
.paste { font-family: inherit; min-width: 56px; }
.key:disabled, .quick:disabled, .paste-confirm button:disabled { opacity: .45; color: #64748b; }
.paste-confirm { display: grid; grid-template-columns: 1fr auto auto; gap: 6px; align-items: center; }
.paste-confirm textarea { min-width: 0; resize: vertical; border-radius: 8px; border: 1px solid #1e2638; background: #020617; color: #e2e8f0; padding: 6px 8px; font: 0.78rem ui-monospace, Menlo, monospace; }
.paste-confirm button { height: 30px; border-radius: 7px; border: 1px solid #1e2638; background: #11182b; color: #cbd5e1; padding: 0 10px; }
.hidden-file { position: absolute; width: 1px; height: 1px; opacity: 0; pointer-events: none; left: -9999px; }
</style>
