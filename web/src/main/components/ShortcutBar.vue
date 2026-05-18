<script setup lang="ts">
const emit = defineEmits<{
  (e: 'input', text: string): void
  (e: 'copy'): void
  (e: 'paste'): void
}>()

const SHORTCUTS: Record<string, string> = {
  esc: '\x1b',
  tab: '\t',
  'ctrl-c': '\x03',
  'ctrl-d': '\x04',
  up: '\x1b[A',
  down: '\x1b[B',
  right: '\x1b[C',
  left: '\x1b[D',
}

function onShortcut(e: Event) {
  const target = (e.target as HTMLElement).closest('button[data-shortcut]')
  if (!target) return
  const name = target.getAttribute('data-shortcut') || ''
  const seq = SHORTCUTS[name]
  if (seq !== undefined) emit('input', seq)
}
</script>

<template>
  <div class="shortcut-bar" aria-label="terminal shortcuts" @click="onShortcut">
    <button data-shortcut="esc" type="button">Esc</button>
    <button data-shortcut="tab" type="button">Tab</button>
    <button data-shortcut="ctrl-c" type="button">Ctrl-C</button>
    <button data-shortcut="ctrl-d" type="button">Ctrl-D</button>
    <button data-shortcut="left" type="button">←</button>
    <button data-shortcut="down" type="button">↓</button>
    <button data-shortcut="up" type="button">↑</button>
    <button data-shortcut="right" type="button">→</button>
    <button type="button" data-testid="copy" @click.stop="emit('copy')">Copy</button>
    <button type="button" data-testid="paste" @click.stop="emit('paste')">Paste</button>
  </div>
</template>

<style scoped>
.shortcut-bar {
  display: flex;
  gap: 0.25rem;
  padding: 0.5rem;
  background: var(--panel);
  border-top: 1px solid var(--border);
  overflow-x: auto;
  flex-shrink: 0;
}
.shortcut-bar button {
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 0.4rem 0.6rem;
  font: inherit;
  font-size: 0.875rem;
  white-space: nowrap;
  cursor: pointer;
}
.shortcut-bar button:hover { border-color: var(--accent); color: var(--accent); }
</style>
