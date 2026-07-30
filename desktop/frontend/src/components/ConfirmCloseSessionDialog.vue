<script lang="ts" setup>
import { useI18n } from "../i18n/useI18n";

const props = withDefaults(defineProps<{
  title: string;
  isAi: boolean;
  isRunning: boolean;
  isRemote: boolean;
  count?: number;
}>(), {
  count: 1,
});

defineEmits<{
  (e: "confirm"): void;
  (e: "cancel"): void;
}>();

const { t } = useI18n();
</script>

<template>
  <div class="backdrop" @click.self="$emit('cancel')">
    <div class="dialog" role="dialog" aria-modal="true">
      <h2>{{ t("sessions.closeSessionTitle") }}</h2>
      <p>
        {{ count > 1
          ? t("sessions.closeSessionManyBody", { count })
          : t("sessions.closeSessionBody", { title }) }}
      </p>
      <ul>
        <li v-if="isAi">{{ t("sessions.closeAiSessionWarning") }}</li>
        <li v-if="isRunning">{{ t("sessions.closeRunningSessionWarning") }}</li>
        <li>{{ t(isRemote ? "sessions.closeRemoteDetachHint" : "sessions.closeLocalTerminateHint") }}</li>
      </ul>
      <div class="row">
        <button type="button" @click="$emit('cancel')">{{ t("common.cancel") }}</button>
        <button type="button" class="primary danger" @click="$emit('confirm')">
          {{ t("sessions.closeSessionConfirm") }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 111;
}
.dialog {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: 430px;
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
.row {
  display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px;
}
.primary {
  background: var(--accent); color: #0d1117; border-color: var(--accent);
  font-weight: 600;
}
.primary.danger {
  background: var(--bad); color: #0d1117; border-color: var(--bad);
}
.primary.danger:hover { background: #ff6f6a; border-color: #ff6f6a; color: #0d1117; }
</style>
