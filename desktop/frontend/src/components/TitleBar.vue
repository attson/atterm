<script lang="ts" setup>
import { errText, logWarn } from "../lib/log";
import { computed, onMounted, ref } from "vue";
import { usePlatform } from "../platform";
import WindowControls from "./WindowControls.vue";
import { setMaximized, useWindowMaximized } from "../composables/useWindowMaximized";
import { getRelayConfig, type Endpoint } from "../lib/api";
import type { TaskState } from "../lib/taskState";
import { useI18n } from "../i18n/useI18n";
import { useUplinkHealth } from "../composables/useUplinkHealth";
import ConnHealthPill from "@shared/components/ConnHealthPill.vue";
import ConnHealthDrawer from "@shared/components/ConnHealthDrawer.vue";

const platform = usePlatform();
const { t } = useI18n();

const connHealthDrawerOpen = ref(false);
const connHealth = useUplinkHealth({ fast: () => connHealthDrawerOpen.value });

const pillLabels = computed(() => ({
  connecting: t("connHealth.pillLabelConnecting"),
  reconnecting: t("connHealth.pillLabelReconnecting"),
  off: t("connHealth.pillLabelOff"),
}));

const drawerLabels = computed(() => ({
  title: t("connHealth.drawerTitle"),
  rttNow: t("connHealth.drawerRttNow"),
  rttP50P95: t("connHealth.drawerRttP50P95"),
  bytesIn: t("connHealth.drawerBytesIn"),
  bytesOut: t("connHealth.drawerBytesOut"),
  state: t("connHealth.drawerState"),
  reconnectsLastHour: t("connHealth.drawerReconnectsLastHour"),
  reconnectsTime: t("connHealth.drawerReconnectsTime"),
  reconnectsReason: t("connHealth.drawerReconnectsReason"),
  reconnectsDowntime: t("connHealth.drawerReconnectsDowntime"),
  seqGaps: t("connHealth.drawerSeqGaps"),
  close: t("common.close"),
}));

type Status = "loading" | "ready" | "error";

const props = defineProps<{
  status: Status;
  errorMsg: string;
  sessionCount: number;
  remoteEndpoint: Endpoint | null;
  currentTitle?: string;
  currentTaskState?: TaskState | null;
}>();

const isRunning = computed(() => props.currentTaskState === "running");

// e2eeDisabled tracks the per-desktop "stop sealing" toggle. The chip
// is always visible while this is true so the user doesn't forget the
// agent is sending plaintext to the relay. Loaded once from
// getRelayConfig() then kept in sync via the e2ee-mode-changed event.
const e2eeDisabled = ref(false);
const e2eeChipTitle = computed(() => t("titlebar.e2eeOffTitle"));
const e2eeChipText = computed(() => t("titlebar.e2eeOffChip"));

onMounted(async () => {
  try {
    const cfg = await getRelayConfig();
    e2eeDisabled.value = (cfg as { disable_e2ee?: boolean }).disable_e2ee ?? false;
  } catch {
    // Pre-uplink boot can fail this call; the event listener below
    // will catch up once the backend is ready.
  }
});

platform.events.on("e2ee-mode-changed", (data) => {
  const next = (data as { disabled?: boolean })?.disabled;
  if (typeof next === "boolean") {
    e2eeDisabled.value = next;
  }
});

// Default to linux if Environment() fails — gives users window controls
// rather than locking them out of min/max/close.
const os = ref<"darwin" | "windows" | "linux">("linux");

onMounted(async () => {
  try {
    const info = await platform.system.getEnvironment();
    if (info == null) {
      logWarn("titlebar", "getEnvironment() returned null, falling back to linux");
      return;
    }
    const p = (info.platform ?? "").toLowerCase();
    if (p === "darwin" || p === "windows" || p === "linux") {
      os.value = p;
    } else {
      logWarn("titlebar", "unknown platform, falling back to linux", { platform: String(p) });
    }
  } catch (e) {
    logWarn("titlebar", "getEnvironment() failed, falling back to linux", { error: errText(e) });
  }
});

const rootStyle = computed(() => ({
  "padding-left": os.value === "darwin" ? "80px" : undefined,
}));

const showWindowControls = computed(() => os.value !== "darwin");

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
    <div
      v-if="e2eeDisabled"
      class="e2ee-off-chip"
      role="status"
      data-testid="titlebar-e2ee-off"
      :title="e2eeChipTitle"
    >
      <span class="chip-icon" aria-hidden="true">⚠</span>
      <span>{{ e2eeChipText }}</span>
    </div>
    <ConnHealthPill
      v-if="remoteEndpoint"
      :health="connHealth"
      :labels="pillLabels"
      class="titlebar-conn-pill"
      data-testid="titlebar-conn-health-pill"
      @click="connHealthDrawerOpen = !connHealthDrawerOpen"
    />
    <ConnHealthDrawer
      v-if="remoteEndpoint"
      :health="connHealth"
      :open="connHealthDrawerOpen"
      :labels="drawerLabels"
      @close="connHealthDrawerOpen = false"
    />
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

/* E2EE-off chip — present only when the user has paused outbound
   sealing. Amber-yellow to read as "warning, not fatal"; user can
   still hover to see the long-form tooltip. Stays out of the way
   when E2EE is on (v-if). */
.e2ee-off-chip {
  --wails-draggable: no-drag;
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-left: 8px;
  padding: 2px 8px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.4;
  color: #1f1300;
  background: #f4b740;
  border-radius: 999px;
  letter-spacing: 0.02em;
  white-space: nowrap;
}
.e2ee-off-chip .chip-icon {
  font-size: 12px;
  line-height: 1;
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
