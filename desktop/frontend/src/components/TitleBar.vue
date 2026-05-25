<script lang="ts" setup>
import { computed, onMounted, ref } from "vue";
import { usePlatform } from "../platform";
import WindowControls from "./WindowControls.vue";
import { setMaximized, useWindowMaximized } from "../composables/useWindowMaximized";
import type { Endpoint } from "../lib/api";
import { useI18n } from "../i18n/useI18n";

const platform = usePlatform();
const { t } = useI18n();

type Status = "loading" | "ready" | "error";

const props = defineProps<{
  status: Status;
  errorMsg: string;
  sessionCount: number;
  remoteEndpoint: Endpoint | null;
  availableRemoteCount: number;
  updateBadge: boolean;
}>();

defineEmits<{
  (e: "open-remote"): void;
  (e: "open-settings"): void;
}>();

// Default to linux if Environment() fails — gives users window controls
// rather than locking them out of min/max/close.
const os = ref<"darwin" | "windows" | "linux">("linux");

onMounted(async () => {
  try {
    const info = await platform.system.getEnvironment();
    if (info == null) {
      console.warn("[TitleBar] getEnvironment() returned null, falling back to linux");
      return;
    }
    const p = (info.platform ?? "").toLowerCase();
    if (p === "darwin" || p === "windows" || p === "linux") {
      os.value = p;
    } else {
      console.warn("[TitleBar] unknown platform, falling back to linux:", p);
    }
  } catch (e) {
    console.warn("[TitleBar] getEnvironment() failed, falling back to linux", e);
  }
});

const rootStyle = computed(() => ({
  "padding-left": os.value === "darwin" ? "80px" : undefined,
}));

const showWindowControls = computed(() => os.value !== "darwin");

const remoteTitle = computed(() =>
  props.remoteEndpoint
    ? t("terminal.remoteSessionsAvailable", { count: props.availableRemoteCount })
    : t("terminal.connectRelayForRemote"),
);

const isMaximized = useWindowMaximized();

const sessionStatusKey = computed(() =>
  props.sessionCount === 1 ? "terminal.sessionStatusOne" : "terminal.sessionStatusMany",
);

function onTitleDblClick() {
  // macOS' system zoom-on-dblclick fires off NSWindow events that the
  // WebKit view eats under TitleBarHiddenInset, so we drive maximize
  // ourselves on all three platforms.
  void platform.system.windowToggleMaximize?.();
  setMaximized(!isMaximized.value);
}
</script>

<template>
  <header
    class="titlebar"
    data-testid="titlebar-root"
    :style="rootStyle"
    @dblclick.self="onTitleDblClick"
  >
    <div class="status">
      <template v-if="status === 'loading'">{{ t("terminal.starting") }}</template>
      <template v-else-if="status === 'error'">
        <span class="bad">{{ errorMsg }}</span>
      </template>
      <template v-else>
        {{ t(sessionStatusKey, { count: sessionCount }) }}
        <span v-if="remoteEndpoint" class="dim"> · {{ t("terminal.uplinkOn") }}</span>
      </template>
    </div>
    <button
      class="icon-btn"
      type="button"
      data-testid="titlebar-remote"
      :title="remoteTitle"
      :disabled="!remoteEndpoint"
      @click="$emit('open-remote')"
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="16" height="16"
        viewBox="0 0 24 24"
        fill="none" stroke="currentColor"
        stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
        aria-hidden="true"
      >
        <path d="M2 16.1A5 5 0 0 1 5.9 20" />
        <path d="M2 12.05A9 9 0 0 1 9.95 20" />
        <path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6" />
        <line x1="2" y1="20" x2="2.01" y2="20" />
      </svg>
      <span v-if="availableRemoteCount > 0" class="badge">{{ availableRemoteCount }}</span>
    </button>
    <button
      class="icon-btn"
      type="button"
      data-testid="titlebar-settings"
      :title="t('terminal.relaySettings')"
      @click="$emit('open-settings')"
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="16" height="16"
        viewBox="0 0 24 24"
        fill="none" stroke="currentColor"
        stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
        aria-hidden="true"
      >
        <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
        <circle cx="12" cy="12" r="3" />
      </svg>
      <span v-if="updateBadge" class="dot"></span>
    </button>
    <WindowControls v-if="showWindowControls" />
  </header>
</template>

<style scoped>
.titlebar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  background: var(--panel);
  border-bottom: 1px solid var(--border);
  flex: 0 0 auto;
  /* Wails uses --wails-draggable (not -webkit-app-region) under frameless
     webviews on Linux/Windows; mac TitleBarHiddenInset has native drag, so
     this is a no-op there. The property cascades to children. */
  --wails-draggable: drag;
}
.status {
  margin-left: auto;
  font-size: 12px;
  color: var(--fg-dim);
  --wails-draggable: no-drag;
}
.status .bad { color: var(--bad); }
.status .dim { color: var(--good); }

.icon-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  color: var(--fg-dim);
  line-height: 1;
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
  transition: color 120ms, background 120ms;
  --wails-draggable: no-drag;
}
.icon-btn svg { display: block; }
.icon-btn:hover:not(:disabled) { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.icon-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.icon-btn .badge {
  position: absolute; top: -2px; right: -2px;
  background: #d29922; color: #0d1117; font-size: 9px; font-weight: 700;
  border-radius: 10px; padding: 1px 5px; line-height: 1.3;
  min-width: 16px; text-align: center;
}
.icon-btn .dot {
  position: absolute; top: 2px; right: 2px;
  width: 6px; height: 6px;
  background: #d29922;
  border-radius: 50%;
}
</style>
