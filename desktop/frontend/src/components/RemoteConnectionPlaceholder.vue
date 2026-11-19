<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { CircleAlert, CirclePause, LoaderCircle, Settings } from "lucide-vue-next";
import type { RemoteConnectionReason } from "../lib/remoteConnectionFeedback";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{ reason: RemoteConnectionReason; active: boolean }>();
const emit = defineEmits<{ (e: "open-settings"): void }>();
const { t } = useI18n();
const startedAt = Date.now();
const now = ref(startedAt);
let timer: ReturnType<typeof setInterval> | undefined;
const pending = computed(() => props.reason === "initializing" || props.reason === "connecting");
const elapsedSeconds = computed(() => Math.max(0, Math.floor((now.value - startedAt) / 1000)));
const elapsed = computed(() => `${Math.floor(elapsedSeconds.value / 60).toString().padStart(2, "0")}:${(elapsedSeconds.value % 60).toString().padStart(2, "0")}`);
const title = computed(() => {
  if (pending.value) return t("terminal.remoteConnection.connecting");
  if (props.reason === "paused") return t("terminal.remoteConnection.paused");
  if (props.reason === "signed-out") return t("terminal.remoteConnection.signIn");
  return t("terminal.remoteConnection.unavailable");
});
const reasonText = computed(() => {
  switch (props.reason) {
    case "initializing": return t("terminal.remoteConnection.initializingReason");
    case "connecting": return t("terminal.remoteConnection.connectingReason");
    case "paused": return t("terminal.remoteConnection.pausedReason");
    case "unconfigured": return t("terminal.remoteConnection.unconfiguredReason");
    case "signed-out": return t("terminal.remoteConnection.signInReason");
    case "proxy-unavailable": return t("terminal.remoteConnection.channelReason");
    default: return t("terminal.remoteConnection.unavailableReason");
  }
});

watch(() => props.active, (active) => {
  clearInterval(timer);
  timer = undefined;
  if (!active) return;
  // Date-based elapsed time stays accurate after background timer throttling.
  now.value = Date.now();
  timer = setInterval(() => { now.value = Date.now(); }, 1000);
}, { immediate: true });
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <div class="remote-connection-placeholder" data-testid="remote-connection-placeholder">
    <div class="connection-copy" role="status" :aria-busy="pending">
      <LoaderCircle v-if="pending" :size="24" class="connection-spinner" aria-hidden="true" />
      <CirclePause v-else-if="reason === 'paused'" :size="24" aria-hidden="true" />
      <CircleAlert v-else :size="24" aria-hidden="true" />
      <h2>{{ title }}</h2>
      <p>{{ reasonText }}</p>
    </div>
    <span class="connection-elapsed">{{ t("terminal.remoteConnection.elapsed", { duration: elapsed }) }}</span>
    <p v-if="pending && elapsedSeconds >= 10" class="connection-long-wait">
      {{ t("terminal.remoteConnection.longWait") }}
    </p>
    <button type="button" data-testid="remote-connection-settings" @click.stop="emit('open-settings')">
      <Settings :size="14" aria-hidden="true" />
      {{ t("terminal.remoteConnection.settings") }}
    </button>
  </div>
</template>

<style scoped>
.remote-connection-placeholder {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 24px;
  overflow: auto;
  color: var(--fg-dim);
  text-align: center;
  font-size: 12px;
}
.connection-copy {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  max-width: 360px;
}
h2 { margin: 0; color: var(--fg); font-size: 14px; font-weight: 500; }
p { margin: 0; line-height: 1.6; }
.connection-spinner { color: var(--accent); animation: connection-spin 1s linear infinite; }
.connection-elapsed { font-variant-numeric: tabular-nums; }
.connection-long-wait { max-width: 360px; }
button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 32px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
  color: var(--fg);
  cursor: pointer;
  font: inherit;
}
button:hover { border-color: var(--accent); }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
@keyframes connection-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .connection-spinner { animation: none; } }
</style>
