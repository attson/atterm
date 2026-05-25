<script lang="ts" setup>
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  localCount: number;
  remoteCount: number;
}>();

defineEmits<{
  (e: "confirm"): void;
  (e: "cancel"): void;
}>();

const { t } = useI18n();

const plural = (n: number) => (n === 1 ? "" : "s");
</script>

<template>
  <div class="backdrop" @click.self="$emit('cancel')">
    <div class="dialog">
      <h2>{{ t("sessions.quitTitle") }}</h2>

      <p>{{ t("sessions.closingWill") }}</p>

      <ul>
        <li v-if="localCount > 0">
          {{ t("sessions.endLocalSession", { count: localCount, plural: plural(localCount) }) }}
          <span class="dim">{{ t("sessions.runningProcessesTerminated") }}</span>
        </li>
        <li v-if="remoteCount > 0">
          {{ t("sessions.detachRemoteSession", { count: remoteCount, plural: plural(remoteCount) }) }}
          <span class="dim">{{ t("sessions.remotePtyKeepsRunning") }}</span>
        </li>
      </ul>

      <p v-if="localCount > 0" class="warn">{{ t("sessions.saveWorkFirst") }}</p>

      <div class="row">
        <button @click="$emit('cancel')">{{ t("common.cancel") }}</button>
        <button
          class="primary"
          :class="{ danger: localCount > 0 }"
          @click="$emit('confirm')"
        >
          {{ t("sessions.quit") }}
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
