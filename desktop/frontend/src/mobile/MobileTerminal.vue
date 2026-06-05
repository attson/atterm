<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import { WebglAddon } from 'xterm-addon-webgl'
import 'xterm/css/xterm.css'
import { SessionConnection, type Endpoint } from '../lib/connection'
import { effectiveTemplates, type QuickTemplate } from '../lib/templates'
import { effectiveAuxKeys, type AuxKey } from '../lib/auxKeys'
import { Camera, CameraSource, CameraResultType } from '@capacitor/camera'
import TemplatePreviewDialog from '../components/TemplatePreviewDialog.vue'
import { TERMINAL_FONT_FAMILY } from '../lib/terminalFont'
import type { RemoteSession } from '../platform/types'
import { usePlatform } from '../platform'
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
const isDriver = ref(true)
const controlMode = ref(false)
const pasteOpen = ref(false)
const pasteText = ref('')
const templates = ref<readonly QuickTemplate[]>([])
const pendingTemplate = ref<QuickTemplate | null>(null)
// Increments every time the user tries to input while tap-protect is on
// (controlMode off but otherwise eligible). The banner uses the value as a
// :key so each bump restarts its shake animation.
const protectBump = ref(0)
let protectClearTimer: ReturnType<typeof setTimeout> | null = null
const platform = usePlatform()
let term: Terminal | null = null
let fit: FitAddon | null = null
let conn: SessionConnection | null = null
let ro: ResizeObserver | null = null

function decode(data: Uint8Array): string {
  return new TextDecoder().decode(data)
}

// fitIfDriver re-fits the terminal to its container, but only when we own the
// PTY (driver). Viewers lock to the PTY's advertised cols/rows instead (see
// onMeta) so they mirror the driver exactly. fit() fires term.onResize, which
// forwards a RESIZE to the PTY — correct for the driver, wrong for a viewer.
function fitIfDriver() {
  if (!term || !fit || !isDriver.value) return
  try { fit.fit() } catch { /* container not laid out yet */ }
}

const auxKeys = ref<readonly AuxKey[]>([])

const canControl = computed(() => (props.info.remote_permission || 'full') !== 'view')
const canSend = computed(() => canControl.value && controlMode.value && isDriver.value)
const protectActive = computed(() => canControl.value && isDriver.value && !controlMode.value)

function nudgeProtect() {
  if (!protectActive.value) return
  protectBump.value++
  if (protectClearTimer) clearTimeout(protectClearTimer)
  protectClearTimer = setTimeout(() => { protectClearTimer = null }, 500)
}

function refreshInputMode() {
  if (term) term.options.disableStdin = !canSend.value
}

function sendRaw(seq: string) {
  if (!canSend.value) { nudgeProtect(); return }
  conn?.sendInput(seq)
}

// onImeInput handles direct (non-composition) on-screen-keyboard input that
// xterm drops on iOS — see the registration site in onMounted for the why.
function onImeInput(ev: Event) {
  const e = ev as InputEvent
  if (e.isComposing) return
  if (e.inputType === 'insertText' && e.data) {
    // Take ownership: stop xterm's bubble-phase handler so the character is
    // sent exactly once (by us), never zero or twice.
    e.stopImmediatePropagation()
    sendRaw(e.data)
    if (term?.textarea) term.textarea.value = ''
  }
}

function sendAux(seq: string) { sendRaw(seq) }
function onTemplateClick(tpl: QuickTemplate) {
  if (!canSend.value) { nudgeProtect(); return }
  pendingTemplate.value = tpl
}
function confirmTemplate(tpl: QuickTemplate) {
  pendingTemplate.value = null
  sendRaw(`${tpl.text}\r`)
}
function cancelTemplate() { pendingTemplate.value = null }
function takeControl() {
  if (!canControl.value) return
  // Flip controlMode on at the same time — tapping "Take control" is a
  // clear intent to actually drive the session; making the user toggle a
  // second checkbox in the panel below before any key works is confusing.
  // The toggle stays visible so users who want to lock input mid-session
  // (e.g. to scroll back without firing keystrokes) can disable it again.
  controlMode.value = true
  conn?.claimDriver()
}

async function openPasteConfirm() {
  if (!canSend.value) { nudgeProtect(); return }
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

// base64ToFile decodes a Camera base64 result into a File so it rides the
// existing sendPasteImage path unchanged.
function base64ToFile(b64: string, mime: string, name: string): File {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  return new File([bytes], name, { type: mime })
}

async function openImagePicker() {
  if (!canSend.value) { nudgeProtect(); return }
  try {
    // Camera.getPhoto's Prompt sheet labels are localizable, unlike the
    // native <input type=file> sheet (whose text follows the iOS system
    // language, not the app). source=Prompt offers library + camera.
    const photo = await Camera.getPhoto({
      source: CameraSource.Prompt,
      resultType: CameraResultType.Base64,
      quality: 90,
      promptLabelHeader: t('mobile.image.prompt'),
      promptLabelPhoto: t('mobile.image.fromLibrary'),
      promptLabelPicture: t('mobile.image.takePhoto'),
      promptLabelCancel: t('common.cancel'),
    })
    if (!photo.base64String || !canSend.value) return
    const ext = photo.format || 'jpeg'
    const file = base64ToFile(photo.base64String, `image/${ext}`, `mobile-image.${ext}`)
    await conn?.sendPasteImage(file, file.name)
  } catch {
    // User cancelled the picker, or sendPasteImage failed (which already
    // routes status='error' to MobileApp). Either way, nothing to add here.
  }
}

onMounted(() => {
  term = new Terminal({ fontFamily: TERMINAL_FONT_FAMILY, fontSize: 12, convertEol: false, cursorBlink: true })
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
  term.onData((s: string) => sendRaw(s))
  term.onResize(({ cols, rows }) => conn?.sendResize(cols, rows))

  // Mobile IME fallback. On iOS, characters typed through the on-screen
  // keyboard that are NOT part of a composition — Chinese full-width
  // punctuation (，。？！), digits, and space from the 9-key layout — arrive
  // as `input` events with inputType 'insertText' and isComposing=false, but
  // xterm's own handler drops them (only pinyin→Hanzi via insertCompositionText
  // gets forwarded). Capture the event before xterm's bubble-phase listener,
  // send the data ourselves, and stop propagation so xterm can't double-send.
  // Composition input (insertCompositionText) is left to xterm untouched.
  const ta = term.textarea
  if (ta) ta.addEventListener('input', onImeInput, { capture: true })

  // The first fit() on iOS often runs before the WebView has settled the
  // .term box (safe-area + keyboard layout land a frame or two later), which
  // produced the "half-height until you switch tabs" bug. A ResizeObserver
  // re-fits on every container size change — initial layout, rotation,
  // keyboard show/hide — so the grid always matches the real viewport. Only
  // the driver fits; viewers are locked to the PTY size in onMeta.
  ro = new ResizeObserver(() => fitIfDriver())
  ro.observe(container.value!)
  fitIfDriver()

  conn = new SessionConnection(props.endpoint, props.sessionId, {
    onOutput: (data) => term?.write(decode(data)),
    onMeta: (meta) => {
      emit('meta', { cwd: meta.cwd, title: meta.title })
      // Viewer: lock our grid to the PTY's real cols/rows so we mirror the
      // driver instead of letting fit pick a narrower width that reflows the
      // output and overflows the screen.
      if (!isDriver.value && term && meta.cols && meta.rows
          && (term.cols !== meta.cols || term.rows !== meta.rows)) {
        try { term.resize(meta.cols, meta.rows) } catch { /* ignore bad dims */ }
      }
    },
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
      // Became driver: fit to OUR (phone) viewport first so cols/rows match
      // the narrow screen, then push that size to the PTY — every other viewer
      // (e.g. the desktop owner) reflows to match. Without the fit the PTY
      // could keep the previous driver's wide cols and overflow the phone.
      if (isMe && canControl.value && term) {
        fitIfDriver()
        if (term.cols > 0 && term.rows > 0) conn?.sendResize(term.cols, term.rows)
      }
    },
  })
  conn.attach()
  refreshInputMode()
  effectiveTemplates(platform.templates).then((list) => { templates.value = list })
  effectiveAuxKeys(platform.auxKeys).then((list) => { auxKeys.value = list })
})

watch(canSend, refreshInputMode)

watch(() => props.active, (now) => {
  if (now) {
    // Reload bars so edits made on the settings page (which the user reaches
    // and returns from without remounting this v-show'd terminal) show up.
    effectiveTemplates(platform.templates).then((list) => { templates.value = list })
    effectiveAuxKeys(platform.auxKeys).then((list) => { auxKeys.value = list })
    // xterm could not measure while hidden (v-show); re-fit + focus on activate.
    requestAnimationFrame(() => { fitIfDriver(); term?.focus() })
  }
})

onBeforeUnmount(() => {
  ro?.disconnect()
  ro = null
  conn?.detach()
  conn = null
  term?.dispose()
  term = null
  fit = null
  if (protectClearTimer) { clearTimeout(protectClearTimer); protectClearTimer = null }
})

function onTermPointerDown() {
  // xterm swallows keystrokes when disableStdin === true, so a tap on the
  // terminal area with controlMode off would produce no visible feedback.
  // Bumping the protect banner gives the user a clear reason.
  if (!canSend.value) nudgeProtect()
}
</script>

<template>
  <div class="mobile-term">
    <div
      ref="container"
      class="term"
      :class="{ inert: !isDriver }"
      @pointerdown="onTermPointerDown"
    ></div>
    <div v-if="!isDriver" class="viewer-overlay">
      <div class="viewer-card">
        <div class="viewer-title">{{ t('terminal.remoteHasControl') }}</div>
        <button v-if="canControl" type="button" class="take-control" data-testid="mobile-take-control" @click.stop="takeControl">{{ t('terminal.takeControl') }}</button>
        <div v-else class="view-only-copy" data-testid="mobile-view-only-overlay">{{ t('mobile.viewOnly') }}</div>
      </div>
    </div>
    <div class="control-panel" data-testid="mobile-control-panel">
      <div v-if="!canControl" class="view-only" data-testid="mobile-view-only">{{ t('mobile.viewOnly') }}</div>
      <div
        v-if="protectActive"
        :key="protectBump"
        class="protect-banner"
        :class="{ shaking: protectBump > 0 }"
        data-testid="mobile-protect-banner"
      >{{ t('mobile.protectMode.banner') }}</div>
      <label class="control-toggle">
        <input
          v-model="controlMode"
          data-testid="mobile-control-toggle"
          type="checkbox"
          :disabled="!canControl || !isDriver"
        />
        <span>{{ t('mobile.controlMode') }}</span>
      </label>
      <div class="template-bar" data-testid="template-bar">
        <button
          v-for="tpl in templates"
          :key="tpl.id"
          class="template-btn"
          :class="{ inert: !canSend }"
          :data-testid="`template-btn-${tpl.id}`"
          @click="onTemplateClick(tpl)"
        >{{ tpl.label }}</button>
      </div>
      <div class="kbbar">
        <button
          v-for="k in auxKeys"
          :key="k.id"
          class="key"
          :class="{ inert: !canSend }"
          :data-testid="`mobile-key-${k.id}`"
          @click="sendAux(k.seq)"
        >{{ k.label }}</button>
        <button class="key paste" :class="{ inert: !canSend }" data-testid="mobile-paste" @click="openPasteConfirm">{{ t('mobile.pasteClipboard') }}</button>
        <button class="key paste" :class="{ inert: !canSend }" data-testid="mobile-image" @click="openImagePicker">{{ t('mobile.pasteImage') }}</button>
      </div>
      <div v-if="pasteOpen" class="paste-confirm" data-testid="mobile-paste-confirm-panel">
        <textarea v-model="pasteText" :placeholder="t('mobile.pastePreview')" rows="2"></textarea>
        <button type="button" data-testid="mobile-paste-cancel" @click="pasteOpen = false">{{ t('common.cancel') }}</button>
        <button type="button" data-testid="mobile-paste-confirm" :disabled="!canSend || !pasteText" @click="confirmPaste">{{ t('mobile.pasteConfirm') }}</button>
      </div>
    </div>
    <TemplatePreviewDialog
      :template="pendingTemplate"
      @confirm="confirmTemplate"
      @cancel="cancelTemplate"
    />
  </div>
</template>

<style scoped>
.mobile-term { display: flex; flex-direction: column; height: 100%; background: #000; position: relative; }
.viewer-overlay { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; background: rgba(0,0,0,.55); }
.viewer-card { display: flex; flex-direction: column; align-items: center; gap: 10px; }
.viewer-title { color: #e6e7ea; font-size: 0.9rem; }
.take-control { padding: 8px 16px; border: none; border-radius: 8px; background: #3b82f6; color: #fff; font-weight: 600; }
.view-only-copy { color: #fbbf24; font-size: 0.82rem; }
/* Clip the xterm WebGL <canvas> to the terminal box. On iOS the GPU canvas
   has its own compositing layer that otherwise paints over the control panel
   below it, hiding the protect-mode banner. */
.term { flex: 1; min-height: 0; overflow: hidden; position: relative; z-index: 0; }
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
/* position+z-index keeps the whole panel (incl. the protect banner) above the
   terminal's GPU canvas layer; flex-shrink:0 stops it being squeezed to zero
   when the keyboard resizes the viewport. */
.control-panel { position: relative; z-index: 1; flex: 0 0 auto; border-top: 1px solid #1e2638; background: #0b1020; padding: 7px 8px calc(7px + env(safe-area-inset-bottom)); display: flex; flex-direction: column; gap: 7px; }
.control-toggle { display: inline-flex; align-items: center; gap: 7px; color: #cbd5e1; font-size: 0.78rem; user-select: none; }
.control-toggle input { accent-color: #3b82f6; }
.view-only { border: 1px solid rgba(251,191,36,.34); border-radius: 8px; padding: 6px 8px; color: #fbbf24; background: rgba(251,191,36,.09); font-size: 0.75rem; }
.protect-banner { border: 1px solid rgba(251,191,36,.34); border-radius: 8px; padding: 6px 8px; color: #fbbf24; background: rgba(251,191,36,.09); font-size: 0.75rem; line-height: 1.35; transition: border-color .2s, background .2s; }
.protect-banner.shaking { animation: protect-shake 0.4s ease-in-out; border-color: #fbbf24; background: rgba(251,191,36,.18); }
@keyframes protect-shake {
  0%   { transform: translateX(0); }
  20%  { transform: translateX(-4px); }
  40%  { transform: translateX(4px); }
  60%  { transform: translateX(-3px); }
  80%  { transform: translateX(2px); }
  100% { transform: translateX(0); }
}
.kbbar { display: flex; align-items: center; gap: 6px; overflow-x: auto; }
.key { flex: 0 0 auto; height: 28px; min-width: 34px; padding: 0 9px; border-radius: 7px; background: #11182b; border: 1px solid #1e2638; color: #cbd5e1; font-size: 0.75rem; font-family: var(--font-mono); }
.paste { font-family: inherit; min-width: 56px; }
.key.inert, .template-btn.inert { opacity: .45; color: #64748b; cursor: not-allowed; }
.paste-confirm button:disabled { opacity: .45; color: #64748b; }
.template-bar { display: flex; align-items: center; gap: 6px; overflow-x: auto; padding: 4px 0; border-top: 1px solid #1e2638; }
.template-btn { flex: 0 0 auto; height: 28px; min-width: 34px; padding: 0 9px; border-radius: 7px; background: #11182b; border: 1px solid #1e2638; color: #cbd5e1; font-size: 0.75rem; font-family: var(--font-mono); }
.paste-confirm { display: grid; grid-template-columns: 1fr auto auto; gap: 6px; align-items: center; }
.paste-confirm textarea { min-width: 0; resize: vertical; border-radius: 8px; border: 1px solid #1e2638; background: #020617; color: #e2e8f0; padding: 6px 8px; font: 0.78rem ui-monospace, Menlo, monospace; }
.paste-confirm button { height: 30px; border-radius: 7px; border: 1px solid #1e2638; background: #11182b; color: #cbd5e1; padding: 0 10px; }
</style>
