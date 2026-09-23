<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Activity, Network, Radio, RefreshCw } from "lucide-vue-next";
import { getMeTraffic } from "@shared/api/me";
import type { MeTrafficResponse } from "@shared/api/types";
import { useI18n } from "../i18n/useI18n";

const { t } = useI18n();
const rangeDays = ref(7);
const loading = ref(false);
const error = ref(false);
const traffic = ref<MeTrafficResponse | null>(null);

function utcDay(offset: number): string {
  const date = new Date();
  date.setUTCHours(0, 0, 0, 0);
  date.setUTCDate(date.getUTCDate() + offset);
  return date.toISOString().slice(0, 10);
}

async function load(): Promise<void> {
  loading.value = true;
  error.value = false;
  try {
    traffic.value = await getMeTraffic(utcDay(-(rangeDays.value - 1)), utcDay(0));
  } catch {
    traffic.value = null;
    error.value = true;
  } finally {
    loading.value = false;
  }
}

function setRange(days: number): void {
  if (rangeDays.value === days) return;
  rangeDays.value = days;
  void load();
}

const relayIn = computed(() => traffic.value?.relay.reduce((sum, row) => sum + row.bytes_in, 0) ?? 0);
const relayOut = computed(() => traffic.value?.relay.reduce((sum, row) => sum + row.bytes_out, 0) ?? 0);
const directSent = computed(() => traffic.value?.direct.reduce((sum, row) => sum + row.bytes_sent, 0) ?? 0);
const directReceived = computed(() => traffic.value?.direct.reduce((sum, row) => sum + row.bytes_received, 0) ?? 0);
const attempts = computed(() => traffic.value?.direct.reduce((sum, row) => sum + row.attempts, 0) ?? 0);
const successes = computed(() => traffic.value?.direct.reduce((sum, row) => sum + row.successes, 0) ?? 0);
const fallbacks = computed(() => traffic.value?.direct.reduce((sum, row) => sum + row.fallbacks, 0) ?? 0);
const successRate = computed(() => attempts.value ? Math.min(100, (successes.value / attempts.value) * 100) : 0);

const daily = computed(() => {
  const relay = new Map((traffic.value?.relay ?? []).map((row) => [row.day, row.bytes_in + row.bytes_out]));
  const direct = new Map((traffic.value?.direct ?? []).map((row) => [row.day, row.bytes_sent + row.bytes_received]));
  return Array.from({ length: rangeDays.value }, (_, index) => {
    const day = utcDay(index - rangeDays.value + 1);
    return { day, relay: relay.get(day) ?? 0, direct: direct.get(day) ?? 0 };
  });
});
const dailyMax = computed(() => Math.max(1, ...daily.value.flatMap((row) => [row.relay, row.direct])));
const hasTraffic = computed(() => relayIn.value + relayOut.value + directSent.value + directReceived.value > 0 || attempts.value > 0);

function humanBytes(value: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let result = value;
  let index = 0;
  while (result >= 1024 && index < units.length - 1) {
    result /= 1024;
    index++;
  }
  const digits = index === 0 ? 0 : result >= 100 ? 0 : result >= 10 ? 1 : 2;
  return `${result.toFixed(digits)} ${units[index]}`;
}

function compactNumber(value: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);
}

onMounted(load);
</script>

<template>
  <div class="traffic-dashboard" data-testid="account-traffic-dashboard">
    <div class="traffic-heading">
      <div>
        <h3>{{ t("settings.account.traffic.title") }}</h3>
        <p>{{ t("settings.account.traffic.period", { days: rangeDays }) }}</p>
      </div>
      <div class="traffic-actions">
        <div class="range-control" role="group" :aria-label="t('settings.account.traffic.range')">
          <button v-for="days in [7, 30, 90]" :key="days" type="button" :class="{ active: rangeDays === days }" :data-testid="`traffic-range-${days}`" @click="setRange(days)">
            {{ days }}{{ t("settings.account.traffic.dayUnit") }}
          </button>
        </div>
        <button class="refresh-button" type="button" :title="t('settings.account.traffic.refresh')" :aria-label="t('settings.account.traffic.refresh')" :disabled="loading" @click="load">
          <RefreshCw :class="{ spinning: loading }" />
        </button>
      </div>
    </div>

    <p v-if="error" class="traffic-state error" role="alert">
      {{ t("settings.account.traffic.loadFailed") }}
      <button type="button" @click="load">{{ t("settings.account.traffic.retry") }}</button>
    </p>
    <div v-else-if="loading && !traffic" class="traffic-state">{{ t("common.loading") }}</div>
    <template v-else>
      <div class="traffic-kpis">
        <div data-testid="relay-traffic-total">
          <span><Network />{{ t("settings.account.traffic.relay") }}</span>
          <strong>{{ humanBytes(relayIn + relayOut) }}</strong>
          <small>{{ t("settings.account.traffic.inOut", { incoming: humanBytes(relayIn), outgoing: humanBytes(relayOut) }) }}</small>
        </div>
        <div data-testid="direct-traffic-total">
          <span><Radio />{{ t("settings.account.traffic.p2p") }}</span>
          <strong>{{ humanBytes(directSent + directReceived) }}</strong>
          <small>{{ t("settings.account.traffic.sentReceived", { sent: humanBytes(directSent), received: humanBytes(directReceived) }) }}</small>
        </div>
        <div data-testid="direct-success-rate">
          <span><Activity />{{ t("settings.account.traffic.successRate") }}</span>
          <strong>{{ successRate.toFixed(attempts ? 1 : 0) }}%</strong>
          <small>{{ t("settings.account.traffic.connectionCounts", { attempts: compactNumber(attempts), successes: compactNumber(successes), fallbacks: compactNumber(fallbacks) }) }}</small>
        </div>
      </div>

      <div class="trend" :aria-label="t('settings.account.traffic.dailyTrend')">
        <div class="trend-legend">
          <span><i class="relay-dot"></i>{{ t("settings.account.traffic.relay") }}</span>
          <span><i class="direct-dot"></i>{{ t("settings.account.traffic.p2p") }}</span>
        </div>
        <div v-if="hasTraffic" class="trend-bars">
          <div v-for="row in daily" :key="row.day" class="trend-day" :title="`${row.day} · Relay ${humanBytes(row.relay)} · P2P ${humanBytes(row.direct)}`">
            <div class="bar-pair">
              <i class="relay-bar" :style="{ height: `${Math.max(row.relay ? 3 : 0, (row.relay / dailyMax) * 100)}%` }"></i>
              <i class="direct-bar" :style="{ height: `${Math.max(row.direct ? 3 : 0, (row.direct / dailyMax) * 100)}%` }"></i>
            </div>
            <span v-if="rangeDays <= 7">{{ row.day.slice(5).replace("-", "/") }}</span>
          </div>
        </div>
        <p v-else class="empty-state">{{ t("settings.account.traffic.empty") }}</p>
      </div>
    </template>
  </div>
</template>

<style scoped>
.traffic-dashboard { display: flex; flex-direction: column; gap: 12px; }
.traffic-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.traffic-heading h3 { margin: 0; font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--fg-dim); }
.traffic-heading p { margin: 4px 0 0; color: var(--fg-dim); font-size: 12px; }
.traffic-actions { display: flex; align-items: center; gap: 6px; }
.range-control { display: inline-flex; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
.range-control button { min-width: 40px; height: 28px; padding: 0 8px; border: 0; border-right: 1px solid var(--border); background: transparent; color: var(--fg-dim); cursor: pointer; font-size: 11px; }
.range-control button:last-child { border-right: 0; }
.range-control button.active { background: var(--accent); color: #0d1117; font-weight: 600; }
.refresh-button { width: 28px; height: 28px; display: grid; place-items: center; border: 1px solid var(--border); border-radius: 6px; background: transparent; color: var(--fg-dim); cursor: pointer; }
.refresh-button svg { width: 14px; height: 14px; }
.refresh-button:disabled { opacity: 0.5; }
.spinning { animation: traffic-spin 0.8s linear infinite; }
.traffic-kpis { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
.traffic-kpis > div { min-width: 0; padding: 10px 12px; border-right: 1px solid var(--border); }
.traffic-kpis > div:last-child { border-right: 0; }
.traffic-kpis span { display: flex; align-items: center; gap: 5px; color: var(--fg-dim); font-size: 11px; }
.traffic-kpis span svg { width: 13px; height: 13px; flex: 0 0 auto; }
.traffic-kpis strong { display: block; margin-top: 6px; color: var(--fg); font-size: 18px; font-weight: 650; }
.traffic-kpis small { display: block; margin-top: 3px; color: var(--fg-dim); font-size: 10px; line-height: 1.35; overflow-wrap: anywhere; }
.trend { min-height: 104px; border-top: 1px solid var(--border); padding-top: 10px; }
.trend-legend { display: flex; justify-content: flex-end; gap: 12px; height: 16px; color: var(--fg-dim); font-size: 10px; }
.trend-legend span { display: flex; align-items: center; gap: 4px; }
.trend-legend i { width: 7px; height: 7px; border-radius: 2px; }
.relay-dot, .relay-bar { background: #58a6ff; }
.direct-dot, .direct-bar { background: #3fb950; }
.trend-bars { display: flex; align-items: stretch; gap: 2px; height: 76px; margin-top: 4px; }
.trend-day { flex: 1 1 0; min-width: 2px; display: flex; flex-direction: column; align-items: center; }
.bar-pair { height: 58px; width: 100%; display: flex; align-items: flex-end; justify-content: center; gap: 1px; }
.bar-pair i { width: min(7px, 42%); min-height: 0; border-radius: 2px 2px 0 0; }
.trend-day > span { margin-top: 4px; color: var(--fg-dim); font-size: 9px; white-space: nowrap; }
.traffic-state, .empty-state { margin: 0; min-height: 72px; display: flex; align-items: center; justify-content: center; color: var(--fg-dim); font-size: 12px; }
.traffic-state.error { color: var(--bad); gap: 8px; }
.traffic-state button { border: 0; background: transparent; color: var(--accent); cursor: pointer; padding: 0; }
@keyframes traffic-spin { to { transform: rotate(360deg); } }
@media (max-width: 620px) {
  .traffic-heading { flex-direction: column; }
  .traffic-actions { width: 100%; justify-content: space-between; }
  .traffic-kpis { grid-template-columns: 1fr; }
  .traffic-kpis > div { border-right: 0; border-bottom: 1px solid var(--border); }
  .traffic-kpis > div:last-child { border-bottom: 0; }
}
</style>
