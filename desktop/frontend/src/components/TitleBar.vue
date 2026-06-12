<script lang="ts" setup>
import { computed, onMounted, ref } from "vue";
import { usePlatform } from "../platform";
import WindowControls from "./WindowControls.vue";
import { setMaximized, useWindowMaximized } from "../composables/useWindowMaximized";
import type { Endpoint } from "../lib/api";
import type { TaskState } from "../lib/taskState";
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
  currentTitle?: string;
  currentTaskState?: TaskState | null;
}>();

const isRunning = computed(() => props.currentTaskState === "running");

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
    :class="{ 'is-running': isRunning }"
    data-testid="titlebar-root"
    :style="rootStyle"
    @dblclick.self="onTitleDblClick"
  >
    <div
      class="window-title"
      data-testid="titlebar-current-title"
      :title="currentTitle || ''"
      @dblclick.self="onTitleDblClick"
    >{{ currentTitle || '' }}</div>
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
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 6px 14px;
  background: var(--panel);
  border-bottom: 1px solid var(--border);
  flex: 0 0 auto;
  /* Wails uses --wails-draggable (not -webkit-app-region) under frameless
     webviews on Linux/Windows; mac TitleBarHiddenInset has native drag, so
     this is a no-op there. The property cascades to children. */
  --wails-draggable: drag;
}
/* Absolute-center the title against the WINDOW, not the flex sub-range
   between the traffic-lights padding and the right-side status/icons. With
   asymmetric flanking content the flex midpoint drifts off-center; absolute
   positioning pins it at the geometric center regardless. */
.window-title {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  max-width: 50%;
  font-size: 12px;
  color: var(--fg);
  font-weight: 500;
  text-align: center;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  padding: 0 8px;
}
.status {
  font-size: 12px;
  color: var(--fg-dim);
  --wails-draggable: no-drag;
  flex: 0 0 auto;
  /* Pull status + icons to the right edge of the flex row now that the
     centered title no longer acts as a flex spacer. */
  margin-left: auto;
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
/* Running indicator at the titlebar bottom: a transparent-base track with a
   long green wave (720px wide) traveling L→R; one wave is always either
   on-screen or just entering, no dead window between cycles.
   Pseudo-element is always rendered (animation runs continuously) with
   opacity 0 by default, so re-entering "running" reveals an already-in-
   motion wave (no cold start / mid-blob pop-in). `.is-running` flips
   opacity to 1; the 0.5s ease transition handles graceful fade in/out.
   Pattern: 200px transparent gutter + 720px wave (symmetric ramp ↔ peak
   ↔ ramp) + 200px transparent gutter = 1120px tile, repeat-x. Animation
   shifts by exactly one tile width per cycle for a seamless loop. */
.titlebar::after {
  content: "";
  position: absolute;
  left: 0;
  right: 0;
  bottom: -1px;
  height: 3px;
  background: repeating-linear-gradient(90deg,
    transparent 0px,
    transparent 200px,
    rgba(74, 222, 128, 0.3) 320px,
    #4ade80 500px,
    #bbf7d0 560px,
    #4ade80 620px,
    rgba(74, 222, 128, 0.3) 800px,
    transparent 920px,
    transparent 1120px);
  background-size: 1120px 100%;
  box-shadow: 0 -1px 6px rgba(74, 222, 128, 0.4);
  animation: titlebar-running-sweep 3.5s linear infinite;
  opacity: 0;
  transition: opacity 0.5s ease;
  pointer-events: none;
}
.titlebar.is-running::after { opacity: 1; }
@keyframes titlebar-running-sweep {
  /* positive shift = LEFT-TO-RIGHT. shift by exactly one tile (1120px). */
  0% { background-position: 0 0; }
  100% { background-position: 1120px 0; }
}
@media (prefers-reduced-motion: reduce) {
  .titlebar::after {
    animation: none;
    background: #4ade80;
    transition: opacity 0.25s ease;
  }
}
</style>
