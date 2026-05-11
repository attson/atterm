<script lang="ts" setup>
const props = defineProps<{
  version: string;
  localCount: number;
  remoteCount: number;
}>();

defineEmits<{
  (e: "confirm"): void;
  (e: "cancel"): void;
}>();

function plural(n: number, word: string) {
  return n === 1 ? `1 ${word}` : `${n} ${word}s`;
}
</script>

<template>
  <div class="backdrop" @click.self="$emit('cancel')">
    <div class="dialog">
      <h2>install AT Term {{ version }}</h2>

      <p>
        AT Term will quit and relaunch on the new version.
        <template v-if="localCount > 0 || remoteCount > 0"> This will:</template>
      </p>

      <ul v-if="localCount > 0 || remoteCount > 0">
        <li v-if="localCount > 0">
          End {{ plural(localCount, "local shell session") }}
          <span class="dim">(running processes will be terminated)</span>
        </li>
        <li v-if="remoteCount > 0">
          Detach from {{ plural(remoteCount, "remote session") }}
          <span class="dim">(the remote PTY keeps running on its host)</span>
        </li>
      </ul>

      <p v-if="localCount > 0" class="warn">Save your work first.</p>

      <div class="row">
        <button @click="$emit('cancel')">cancel</button>
        <button
          class="primary"
          :class="{ danger: localCount > 0 }"
          @click="$emit('confirm')"
        >
          {{ localCount > 0 ? "force install" : "install & restart" }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 110;
}
.dialog {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: 460px;
  max-width: calc(100vw - 32px);
  display: flex; flex-direction: column; gap: 12px;
}
.dialog h2 {
  margin: 0; font-size: 14px; font-weight: 600;
  letter-spacing: 0.05em; text-transform: uppercase; color: var(--fg-dim);
}
.dialog p { margin: 0; font-size: 13px; color: var(--fg); line-height: 1.5; }
.dialog ul {
  margin: 0; padding-left: 18px; font-size: 13px; color: var(--fg);
  line-height: 1.6;
}
.dialog .dim { color: var(--fg-dim); font-size: 12px; }
.dialog .warn { color: #d29922; font-size: 12px; }
.row {
  display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px;
}
.primary {
  background: var(--accent); color: #0d1117; border-color: var(--accent);
  font-weight: 600;
}
.primary:hover { background: #79b8ff; border-color: #79b8ff; color: #0d1117; }
.primary.danger {
  background: var(--bad); color: #0d1117; border-color: var(--bad);
}
.primary.danger:hover { background: #ff6f6a; border-color: #ff6f6a; color: #0d1117; }
</style>
