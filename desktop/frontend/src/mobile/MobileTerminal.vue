<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import 'xterm/css/xterm.css'
import { SessionConnection, type Endpoint } from '../lib/connection'
import type { RemoteSession } from '../platform/types'

const props = defineProps<{
  endpoint: Endpoint
  sessionId: string
  info: RemoteSession
  active: boolean
}>()
const emit = defineEmits<{ (e: 'ended'): void; (e: 'tokenInvalid'): void }>()

const container = ref<HTMLDivElement | null>(null)
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

onMounted(() => {
  term = new Terminal({ fontSize: 12, convertEol: false, cursorBlink: true })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(container.value!)
  try { fit.fit() } catch { /* not laid out yet */ }
  term.onData((s: string) => conn?.sendInput(s))
  term.onResize(({ cols, rows }) => conn?.sendResize(cols, rows))

  conn = new SessionConnection(props.endpoint, props.sessionId, {
    onOutput: (data) => term?.write(decode(data)),
    onClose: () => emit('ended'),
    onStatus: (s) => { if (s === 'error') emit('tokenInvalid') },
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
    <div class="kbbar">
      <button v-for="k in AUX_KEYS" :key="k.label" class="key" @click="sendAux(k.seq)">{{ k.label }}</button>
    </div>
  </div>
</template>

<style scoped>
.mobile-term { display: flex; flex-direction: column; height: 100%; background: #000; }
.term { flex: 1; min-height: 0; }
.kbbar { height: 42px; border-top: 1px solid #1e2638; background: #0b1020; display: flex; align-items: center; gap: 6px; padding: 0 8px; overflow-x: auto; }
.key { flex: 0 0 auto; height: 28px; min-width: 34px; padding: 0 9px; border-radius: 7px; background: #11182b; border: 1px solid #1e2638; color: #8d93a3; font-size: 0.75rem; font-family: ui-monospace, Menlo, monospace; }
</style>
