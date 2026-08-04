<script lang="ts" setup>
import { ref } from "vue";
import type { StartupError } from "../lib/api";
import { useI18n } from "../i18n/useI18n";

// StartupFatalPanel renders the fullscreen error surface shown when the Go
// side reports a startup-fatal error (config load failed, mini-relay
// couldn't bind, keychain unlock refused, ...). The user's only recourse
// is to copy the diagnostic bundle and quit, so the panel is intentionally
// minimal: message + optional log path + Copy diagnostics + Quit.

const props = defineProps<{ fatal: StartupError }>();
const emit = defineEmits<{ (e: "quit"): void }>();

const { t } = useI18n();
const copyStatus = ref("");

async function copyFailure() {
  const text = [
    "AT Term startup failed",
    props.fatal.message,
    props.fatal.log_path ? `log: ${props.fatal.log_path}` : "",
  ]
    .filter(Boolean)
    .join("\n");
  try {
    if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
    await navigator.clipboard.writeText(text);
    copyStatus.value = t("app.startupFailureCopied");
  } catch {
    copyStatus.value = t("terminal.copyFailed");
  }
}
</script>

<template>
  <div class="startup-fatal" data-testid="startup-fatal" role="alert">
    <section class="startup-fatal-panel">
      <h1>{{ t("app.startupFailureTitle") }}</h1>
      <p>{{ t("app.startupFailureIntro") }}</p>
      <pre>{{ fatal.message }}</pre>
      <div v-if="fatal.log_path" class="startup-fatal-log">
        <span>{{ t("app.startupFailureLogPath") }}</span>
        <code>{{ fatal.log_path }}</code>
      </div>
      <div class="startup-fatal-actions">
        <button @click="copyFailure">{{ t("app.startupFailureCopy") }}</button>
        <button class="danger" @click="emit('quit')">{{ t("sessions.quit") }}</button>
      </div>
      <p v-if="copyStatus" class="startup-fatal-copy-status">{{ copyStatus }}</p>
    </section>
  </div>
</template>

<style scoped>
.startup-fatal {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: #0d1117;
}
.startup-fatal-panel {
  width: min(680px, 100%);
  display: flex;
  flex-direction: column;
  gap: 14px;
  color: var(--fg);
}
.startup-fatal-panel h1 {
  margin: 0;
  font-size: 20px;
  font-weight: 650;
}
.startup-fatal-panel p {
  margin: 0;
  color: var(--fg-dim);
  line-height: 1.5;
}
.startup-fatal-panel pre {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  padding: 12px;
  border: 1px solid #30363d;
  border-radius: 6px;
  background: #161b22;
  color: #ffb3b3;
  font-size: 12px;
  line-height: 1.45;
}
.startup-fatal-log {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: 8px;
  align-items: baseline;
  color: var(--fg-dim);
  font-size: 12px;
}
.startup-fatal-log code {
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--fg);
}
.startup-fatal-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.startup-fatal-actions button {
  height: 30px;
  padding: 0 12px;
  border: 1px solid #30363d;
  border-radius: 6px;
  background: #21262d;
  color: var(--fg);
  cursor: pointer;
}
.startup-fatal-actions button:hover { background: #30363d; }
.startup-fatal-actions button.danger {
  border-color: #8b2e2e;
  color: #ffb3b3;
}
.startup-fatal-copy-status {
  margin: 0;
  color: var(--fg-dim);
  font-size: 12px;
}
</style>
