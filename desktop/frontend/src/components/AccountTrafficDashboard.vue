<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Activity, ArrowDownRight, ArrowUpRight, Gauge, Minus, Network, Radio, RefreshCw } from "lucide-vue-next";
import { getMeTraffic } from "@shared/api/me";
import type { AdminTrafficRow, MeTrafficResponse } from "@shared/api/types";
import type { MessageKey } from "../i18n";
import { useI18n } from "../i18n/useI18n";
import {
  categorySummaries,
  directionSeries,
  timelineSeries,
  totalMetric,
  trafficRange,
  trendPercent,
  type DirectionFilter,
  type TimelineDimension,
  type TrafficMetric,
} from "./admin/trafficModel";

const { t } = useI18n();
const rangeDays = ref(7);
const metric = ref<TrafficMetric>("bytes");
const dimension = ref<TimelineDimension>("group");
const direction = ref<DirectionFilter>("all");
const loading = ref(false);
const error = ref(false);
const traffic = ref<MeTrafficResponse | null>(null);
const range = computed(() => trafficRange(rangeDays.value));

async function load(): Promise<void> {
  loading.value = true;
  error.value = false;
  try {
    traffic.value = await getMeTraffic(range.value.from, range.value.to);
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

const relayRows = computed<AdminTrafficRow[]>(() => {
  if (traffic.value?.relay_detail?.length) {
    return traffic.value.relay_detail.map((row) => ({ ...row, user_id: "self" }));
  }
  // Rolling upgrades can briefly pair this UI with a relay that only has
  // daily totals. Keep totals available while hiding unavailable dimensions.
  return (traffic.value?.relay ?? []).flatMap((row) => [
    { user_id: "self", day: row.day, direction: 0, bytes: row.bytes_in, frames: row.frames_in, category: "other" },
    { user_id: "self", day: row.day, direction: 1, bytes: row.bytes_out, frames: row.frames_out, category: "other" },
  ]);
});

const currentDays = computed(() => new Set(range.value.days));
const previousDays = computed(() => new Set(range.value.previousDays));
const currentRows = computed(() => relayRows.value.filter((row) => !!row.day && currentDays.value.has(row.day)));
const previousRows = computed(() => relayRows.value.filter((row) => !!row.day && previousDays.value.has(row.day)));
const relayTotal = computed(() => totalMetric(currentRows.value, "bytes"));
const relayOut = computed(() => totalMetric(currentRows.value.filter((row) => row.direction === 1), "bytes"));
const relayIn = computed(() => totalMetric(currentRows.value.filter((row) => row.direction === 0), "bytes"));
const relayFrames = computed(() => totalMetric(currentRows.value, "frames"));

const kpis = computed(() => [
  { key: "total", label: t("settings.account.traffic.kpiTotal"), value: humanBytes(relayTotal.value), trend: trendPercent(relayTotal.value, totalMetric(previousRows.value, "bytes")) },
  { key: "out", label: t("settings.account.traffic.kpiOutbound"), value: humanBytes(relayOut.value), trend: trendPercent(relayOut.value, totalMetric(previousRows.value.filter((row) => row.direction === 1), "bytes")) },
  { key: "in", label: t("settings.account.traffic.kpiInbound"), value: humanBytes(relayIn.value), trend: trendPercent(relayIn.value, totalMetric(previousRows.value.filter((row) => row.direction === 0), "bytes")) },
  { key: "frames", label: t("settings.account.traffic.kpiFrames"), value: compactNumber(relayFrames.value), trend: trendPercent(relayFrames.value, totalMetric(previousRows.value, "frames")) },
]);

const currentDirect = computed(() => (traffic.value?.direct ?? []).filter((row) => currentDays.value.has(row.day)));
const previousDirect = computed(() => (traffic.value?.direct ?? []).filter((row) => previousDays.value.has(row.day)));
const directSent = computed(() => currentDirect.value.reduce((sum, row) => sum + row.bytes_sent, 0));
const directReceived = computed(() => currentDirect.value.reduce((sum, row) => sum + row.bytes_received, 0));
const directBytes = computed(() => directSent.value + directReceived.value);
const previousDirectBytes = computed(() => previousDirect.value.reduce((sum, row) => sum + row.bytes_sent + row.bytes_received, 0));
const attempts = computed(() => currentDirect.value.reduce((sum, row) => sum + row.attempts, 0));
const successes = computed(() => currentDirect.value.reduce((sum, row) => sum + row.successes, 0));
const fallbacks = computed(() => currentDirect.value.reduce((sum, row) => sum + row.fallbacks, 0));
const successRate = computed(() => attempts.value ? Math.min(100, (successes.value / attempts.value) * 100) : 0);
const offloadShare = computed(() => {
  const total = relayTotal.value + directBytes.value;
  return total ? (directBytes.value / total) * 100 : 0;
});

const directKpis = computed(() => [
  { key: "direct", icon: Radio, label: t("settings.account.traffic.p2pTraffic"), value: humanBytes(directBytes.value), hint: t("settings.account.traffic.sentReceived", { sent: humanBytes(directSent.value), received: humanBytes(directReceived.value) }), trend: trendPercent(directBytes.value, previousDirectBytes.value) },
  { key: "share", icon: Gauge, label: t("settings.account.traffic.offloadShare"), value: `${offloadShare.value.toFixed(1)}%`, hint: t("settings.account.traffic.offloadHint"), trend: null },
  { key: "success", icon: Activity, label: t("settings.account.traffic.successRate"), value: `${successRate.value.toFixed(attempts.value ? 1 : 0)}%`, hint: t("settings.account.traffic.connectionCounts", { attempts: compactNumber(attempts.value), successes: compactNumber(successes.value), fallbacks: compactNumber(fallbacks.value) }), trend: null },
  { key: "fallback", icon: ArrowDownRight, label: t("settings.account.traffic.fallbacks"), value: compactNumber(fallbacks.value), hint: t("settings.account.traffic.fallbackHint"), trend: null },
]);

const categories = computed(() => categorySummaries(currentRows.value));
const chartSeries = computed(() => dimension.value === "direction"
  ? []
  : timelineSeries(currentRows.value, range.value.days, dimension.value, direction.value, metric.value, false));
const chartDirection = computed(() => directionSeries(currentRows.value, range.value.days, metric.value));
const chartMax = computed(() => Math.max(1, ...chartDirection.value.inbound, ...chartDirection.value.outbound));
const stackedMax = computed(() => Math.max(1, ...range.value.days.map((_, index) => chartSeries.value.reduce((sum, series) => sum + series.values[index], 0))));

const frameRows = computed(() => {
  const groups = new Map<string, { name: string; category: string; bytes: number; frames: number }>();
  for (const row of currentRows.value) {
    const name = row.frame_type_name || String(row.frame_type ?? "?");
    const key = `${row.frame_type ?? name}:${row.category ?? "other"}`;
    const cell = groups.get(key) ?? { name, category: row.category ?? "other", bytes: 0, frames: 0 };
    cell.bytes += row.bytes;
    cell.frames += row.frames;
    groups.set(key, cell);
  }
  return [...groups.values()].sort((a, b) => b.bytes - a.bytes).slice(0, 8);
});

function humanBytes(value: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let result = value;
  let index = 0;
  while (result >= 1024 && index < units.length - 1) { result /= 1024; index++; }
  const digits = index === 0 ? 0 : result >= 100 ? 0 : result >= 10 ? 1 : 2;
  return `${result.toFixed(digits)} ${units[index]}`;
}

function compactNumber(value: number): string {
  return new Intl.NumberFormat(undefined, { notation: value >= 10_000 ? "compact" : "standard", maximumFractionDigits: 1 }).format(value);
}

function trendText(value: number | null): string {
  if (value === null) return t("settings.account.traffic.trendNew");
  if (Math.abs(value) < 0.05) return t("settings.account.traffic.trendFlat");
  return `${value > 0 ? "+" : ""}${value.toFixed(1)}%`;
}

function formatMetric(value: number): string { return metric.value === "bytes" ? humanBytes(value) : compactNumber(value); }
function shortDay(day: string): string { return day.slice(5).replace("-", "/"); }
function categoryName(key: string): string {
  const translated = t(`settings.account.traffic.categories.${key}` as MessageKey);
  return translated === `settings.account.traffic.categories.${key}` ? key : translated;
}
function showDayLabel(index: number): boolean {
  const step = Math.max(1, Math.floor(range.value.days.length / 6));
  return index === 0 || index === range.value.days.length - 1 || index % step === 0;
}
function linePoints(values: number[]): string {
  const count = Math.max(1, values.length - 1);
  return values.map((value, index) => `${34 + (index / count) * 932},${204 - (value / chartMax.value) * 168}`).join(" ");
}

onMounted(load);
</script>

<template>
  <div class="traffic-dashboard" data-testid="account-traffic-dashboard">
    <div class="traffic-heading">
      <div><h3>{{ t("settings.account.traffic.title") }}</h3><p>{{ t("settings.account.traffic.period", { days: rangeDays }) }}</p></div>
      <div class="traffic-actions">
        <div class="segmented" role="group" :aria-label="t('settings.account.traffic.range')">
          <button v-for="days in [7, 30, 90]" :key="days" type="button" :class="{ active: rangeDays === days }" :data-testid="`traffic-range-${days}`" @click="setRange(days)">{{ days }}{{ t("settings.account.traffic.dayUnit") }}</button>
        </div>
        <button class="icon-button" type="button" :title="t('settings.account.traffic.refresh')" :aria-label="t('settings.account.traffic.refresh')" :disabled="loading" @click="load"><RefreshCw :class="{ spinning: loading }" /></button>
      </div>
    </div>

    <p v-if="error" class="traffic-state error" role="alert">{{ t("settings.account.traffic.loadFailed") }}<button type="button" @click="load">{{ t("settings.account.traffic.retry") }}</button></p>
    <div v-else-if="loading && !traffic" class="traffic-state">{{ t("common.loading") }}</div>
    <template v-else>
      <div class="kpi-strip" data-testid="relay-kpis">
        <div v-for="kpi in kpis" :key="kpi.key" class="kpi-cell" :data-testid="kpi.key === 'total' ? 'relay-traffic-total' : `relay-kpi-${kpi.key}`">
          <span>{{ kpi.label }}</span><div><strong>{{ kpi.value }}</strong><small :class="{ up: kpi.trend !== null && kpi.trend > 0, down: kpi.trend !== null && kpi.trend < 0 }"><ArrowUpRight v-if="kpi.trend !== null && kpi.trend > 0" /><ArrowDownRight v-else-if="kpi.trend !== null && kpi.trend < 0" /><Minus v-else />{{ trendText(kpi.trend) }}</small></div>
        </div>
      </div>

      <section class="analysis-panel">
        <div class="panel-head">
          <div><span>{{ t("settings.account.traffic.coreAnalysis") }}</span><h4>{{ t("settings.account.traffic.timelineTitle") }}</h4></div>
          <div class="analysis-controls">
            <div><small>{{ t("settings.account.traffic.view") }}</small><div class="segmented compact"><button type="button" :class="{ active: dimension === 'group' }" data-testid="traffic-view-group" @click="dimension = 'group'">{{ t("settings.account.traffic.groupTimeline") }}</button><button type="button" :class="{ active: dimension === 'frame' }" data-testid="traffic-view-frame" @click="dimension = 'frame'">{{ t("settings.account.traffic.frameTimeline") }}</button><button type="button" :class="{ active: dimension === 'direction' }" data-testid="traffic-view-direction" @click="dimension = 'direction'">{{ t("settings.account.traffic.directionTrend") }}</button></div></div>
            <div v-if="dimension !== 'direction'"><small>{{ t("settings.account.traffic.colDirection") }}</small><div class="segmented compact"><button v-for="item in (['all', 'out', 'in'] as DirectionFilter[])" :key="item" type="button" :class="{ active: direction === item }" @click="direction = item">{{ t(`settings.account.traffic.direction.${item}`) }}</button></div></div>
            <div><small>{{ t("settings.account.traffic.metric") }}</small><div class="segmented compact"><button type="button" :class="{ active: metric === 'bytes' }" data-testid="traffic-metric-bytes" @click="metric = 'bytes'">{{ t("settings.account.traffic.bytes") }}</button><button type="button" :class="{ active: metric === 'frames' }" data-testid="traffic-metric-frames" @click="metric = 'frames'">{{ t("settings.account.traffic.frames") }}</button></div></div>
          </div>
        </div>
        <div v-if="!currentRows.length" class="chart-empty">{{ t("settings.account.traffic.empty") }}</div>
        <div v-else-if="dimension === 'direction'" class="line-chart" data-testid="traffic-direction-chart">
          <svg viewBox="0 0 1000 230" preserveAspectRatio="none" role="img" :aria-label="t('settings.account.traffic.directionTrend')"><g class="grid-lines"><line v-for="y in [36, 78, 120, 162, 204]" :key="y" x1="34" :y1="y" x2="966" :y2="y" /></g><polyline class="line outbound" :points="linePoints(chartDirection.outbound)" /><polyline class="line inbound" :points="linePoints(chartDirection.inbound)" /></svg>
          <div class="legend"><span><i class="outbound"></i>{{ t("settings.account.traffic.dirOut") }}</span><span><i class="inbound"></i>{{ t("settings.account.traffic.dirIn") }}</span></div>
        </div>
        <div v-else class="bar-chart" data-testid="traffic-stacked-chart"><div v-for="(day, dayIndex) in range.days" :key="day" class="bar-column"><div class="bar-stack"><i v-for="series in chartSeries.slice(0, 8)" :key="series.key" :style="{ height: `${(series.values[dayIndex] / stackedMax) * 100}%`, background: series.color }" :title="`${series.label} · ${day} · ${formatMetric(series.values[dayIndex])}`"></i></div><span>{{ showDayLabel(dayIndex) ? shortDay(day) : '' }}</span></div></div>
        <div v-if="dimension !== 'direction' && currentRows.length" class="legend series-legend"><span v-for="series in chartSeries.slice(0, 8)" :key="series.key"><i :style="{ background: series.color }"></i>{{ dimension === "group" ? categoryName(series.label) : series.label }}</span></div>
      </section>

      <div class="detail-grid">
        <section class="detail-panel"><div class="panel-head"><div><h4>{{ t("settings.account.traffic.categorySummary") }}</h4><p>{{ t("settings.account.traffic.totalComposition") }}</p></div></div><div v-if="categories.length" class="category-list"><div v-for="category in categories" :key="category.key"><span class="category-name"><i :style="{ background: category.color }"></i><strong>{{ categoryName(category.key) }}</strong><em>{{ category.share.toFixed(1) }}%</em></span><span class="meter"><i :style="{ width: `${category.share}%`, background: category.color }"></i></span><span class="category-value"><strong>{{ humanBytes(category.bytes) }}</strong><small>{{ t("settings.account.traffic.outIn", { out: humanBytes(category.outbound), incoming: humanBytes(category.inbound) }) }}</small></span></div></div><p v-else class="detail-empty">{{ t("settings.account.traffic.empty") }}</p></section>
        <section class="detail-panel"><div class="panel-head"><div><h4>{{ t("settings.account.traffic.frameBreakdown") }}</h4><p>{{ t("settings.account.traffic.topFrames") }}</p></div></div><div v-if="frameRows.length" class="frame-table-wrap"><table><thead><tr><th>{{ t("settings.account.traffic.colFrameType") }}</th><th>{{ t("settings.account.traffic.colBytes") }}</th><th>{{ t("settings.account.traffic.colFrames") }}</th></tr></thead><tbody><tr v-for="frame in frameRows" :key="`${frame.name}:${frame.category}`"><td><strong>{{ frame.name }}</strong><small>{{ categoryName(frame.category) }}</small></td><td>{{ humanBytes(frame.bytes) }}</td><td>{{ compactNumber(frame.frames) }}</td></tr></tbody></table></div><p v-else class="detail-empty">{{ t("settings.account.traffic.empty") }}</p></section>
      </div>

      <section class="direct-panel">
        <div class="panel-head"><div><span>{{ t("settings.account.traffic.p2p") }}</span><h4>{{ t("settings.account.traffic.directQuality") }}</h4></div><small><Network />{{ t("settings.account.traffic.aggregateOnly") }}</small></div>
        <div class="direct-strip"><div v-for="item in directKpis" :key="item.key" :data-testid="item.key === 'direct' ? 'direct-traffic-total' : item.key === 'success' ? 'direct-success-rate' : undefined"><span><component :is="item.icon" />{{ item.label }}</span><strong>{{ item.value }}</strong><small>{{ item.hint }}</small><em v-if="item.trend !== null">{{ trendText(item.trend) }}</em></div></div>
        <div class="success-meter" :title="`${successRate.toFixed(1)}%`"><i :style="{ width: `${successRate}%` }"></i></div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.traffic-dashboard { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
.traffic-heading, .panel-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.traffic-heading h3, .panel-head > div > span { margin: 0; font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--fg-dim); }
.traffic-heading p, .panel-head p { margin: 4px 0 0; color: var(--fg-dim); font-size: 11px; }
.panel-head h4 { margin: 2px 0 0; color: var(--fg); font-size: 13px; font-weight: 650; }
.traffic-actions { display: flex; align-items: center; gap: 6px; }
.segmented { display: inline-flex; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
.segmented button { min-width: 40px; height: 28px; padding: 0 8px; border: 0; border-right: 1px solid var(--border); background: transparent; color: var(--fg-dim); cursor: pointer; font-size: 11px; white-space: nowrap; }
.segmented button:last-child { border-right: 0; }
.segmented button.active { background: var(--accent); color: #0d1117; font-weight: 600; }
.segmented.compact button { min-width: 34px; height: 24px; padding: 0 6px; font-size: 10px; }
.icon-button { width: 28px; height: 28px; display: grid; place-items: center; border: 1px solid var(--border); border-radius: 6px; background: transparent; color: var(--fg-dim); cursor: pointer; }
.icon-button svg { width: 14px; height: 14px; }
.icon-button:disabled { opacity: 0.5; }
.spinning { animation: traffic-spin 0.8s linear infinite; }
.kpi-strip, .direct-strip { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
.kpi-cell, .direct-strip > div { min-width: 0; padding: 10px 12px; border-right: 1px solid var(--border); }
.kpi-cell:last-child, .direct-strip > div:last-child { border-right: 0; }
.kpi-cell > span, .direct-strip span { color: var(--fg-dim); font-size: 10px; }
.kpi-cell > div { display: flex; align-items: baseline; justify-content: space-between; gap: 6px; margin-top: 6px; }
.kpi-cell strong, .direct-strip strong { color: var(--fg); font-size: 17px; font-weight: 650; }
.kpi-cell small { display: flex; align-items: center; color: var(--fg-dim); font-size: 9px; white-space: nowrap; }
.kpi-cell small svg { width: 10px; height: 10px; }
.kpi-cell small.up { color: var(--warn, #d29922); }
.kpi-cell small.down { color: var(--good, #3fb950); }
.analysis-panel, .direct-panel { border-top: 1px solid var(--border); padding-top: 12px; }
.analysis-controls { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
.analysis-controls > div { display: flex; flex-direction: column; align-items: flex-end; gap: 3px; }
.analysis-controls small { color: var(--fg-dim); font-size: 9px; text-transform: uppercase; letter-spacing: 0.05em; }
.bar-chart { display: flex; align-items: stretch; gap: 2px; height: 150px; margin-top: 12px; }
.bar-column { flex: 1 1 0; min-width: 2px; display: flex; flex-direction: column; align-items: center; }
.bar-stack { width: 100%; height: 128px; display: flex; flex-direction: column-reverse; justify-content: flex-start; }
.bar-stack i { display: block; width: min(11px, 84%); min-height: 0; margin: 0 auto; }
.bar-stack i:first-child { border-radius: 2px 2px 0 0; }
.bar-column > span { min-height: 12px; margin-top: 4px; color: var(--fg-dim); font-size: 8px; white-space: nowrap; }
.line-chart { position: relative; height: 174px; margin-top: 10px; }
.line-chart svg { width: 100%; height: 150px; overflow: visible; }
.grid-lines line { stroke: var(--border); stroke-width: 1; vector-effect: non-scaling-stroke; }
.line { fill: none; stroke-width: 2; vector-effect: non-scaling-stroke; }
.line.outbound, .legend i.outbound { stroke: #58a6ff; background: #58a6ff; }
.line.inbound, .legend i.inbound { stroke: #3fb950; background: #3fb950; }
.legend { display: flex; flex-wrap: wrap; justify-content: center; gap: 10px; color: var(--fg-dim); font-size: 9px; }
.legend span { display: inline-flex; align-items: center; gap: 4px; }
.legend i { width: 7px; height: 7px; border-radius: 2px; }
.series-legend { margin-top: 6px; }
.chart-empty, .detail-empty, .traffic-state { min-height: 96px; margin: 0; display: flex; align-items: center; justify-content: center; color: var(--fg-dim); font-size: 12px; }
.detail-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 16px; border-top: 1px solid var(--border); padding-top: 12px; }
.detail-panel { min-width: 0; }
.category-list { margin-top: 8px; }
.category-list > div { display: grid; grid-template-columns: minmax(90px, 0.9fr) minmax(70px, 1fr) minmax(96px, auto); align-items: center; gap: 8px; min-height: 34px; border-bottom: 1px solid color-mix(in srgb, var(--border) 65%, transparent); }
.category-name { display: flex; align-items: center; min-width: 0; gap: 5px; font-size: 10px; }
.category-name > i { width: 7px; height: 7px; border-radius: 2px; flex: 0 0 auto; }
.category-name strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.category-name em { color: var(--fg-dim); font-style: normal; }
.meter { height: 4px; overflow: hidden; background: color-mix(in srgb, var(--border) 55%, transparent); border-radius: 2px; }
.meter i { display: block; height: 100%; }
.category-value { text-align: right; }
.category-value strong, .category-value small { display: block; }
.category-value strong { color: var(--fg); font-size: 10px; }
.category-value small { color: var(--fg-dim); font-size: 8px; white-space: nowrap; }
.frame-table-wrap { overflow-x: auto; margin-top: 8px; }
table { width: 100%; border-collapse: collapse; font-size: 10px; }
th { color: var(--fg-dim); font-weight: 500; text-align: left; }
th, td { padding: 6px 5px; border-bottom: 1px solid color-mix(in srgb, var(--border) 65%, transparent); white-space: nowrap; }
th:not(:first-child), td:not(:first-child) { text-align: right; }
td strong, td small { display: block; }
td small { color: var(--fg-dim); font-size: 8px; }
.direct-panel .panel-head > small { display: flex; align-items: center; gap: 4px; color: var(--fg-dim); font-size: 9px; }
.direct-panel .panel-head > small svg { width: 12px; height: 12px; }
.direct-strip { margin-top: 9px; }
.direct-strip span { display: flex; align-items: center; gap: 4px; }
.direct-strip span svg { width: 12px; height: 12px; }
.direct-strip strong, .direct-strip small, .direct-strip em { display: block; margin-top: 5px; }
.direct-strip small { min-height: 24px; color: var(--fg-dim); font-size: 9px; line-height: 1.35; overflow-wrap: anywhere; }
.direct-strip em { color: var(--fg-dim); font-size: 9px; font-style: normal; }
.success-meter { height: 3px; background: color-mix(in srgb, var(--border) 65%, transparent); }
.success-meter i { display: block; height: 100%; background: #3fb950; transition: width 160ms ease; }
.traffic-state.error { color: var(--bad); gap: 8px; }
.traffic-state button { border: 0; background: transparent; color: var(--accent); cursor: pointer; padding: 0; }
@keyframes traffic-spin { to { transform: rotate(360deg); } }
@media (max-width: 760px) {
  .traffic-heading, .panel-head { flex-direction: column; }
  .traffic-actions { width: 100%; justify-content: space-between; }
  .analysis-controls { width: 100%; justify-content: flex-start; }
  .analysis-controls > div { align-items: flex-start; }
  .kpi-strip, .direct-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .kpi-cell:nth-child(2), .direct-strip > div:nth-child(2) { border-right: 0; }
  .kpi-cell:nth-child(-n+2), .direct-strip > div:nth-child(-n+2) { border-bottom: 1px solid var(--border); }
  .detail-grid { grid-template-columns: 1fr; }
}
@media (max-width: 460px) {
  .segmented.compact button { min-width: 30px; padding: 0 4px; }
  .kpi-cell > div { align-items: flex-start; flex-direction: column; }
}
</style>
