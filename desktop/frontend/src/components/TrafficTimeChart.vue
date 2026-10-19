<script setup lang="ts">
import { computed, ref } from 'vue'
import type { AdminTrafficRow } from '@shared/api/types'
import { useI18n } from '../i18n/useI18n'
import { directionSeries, timelineSeries, type DirectionFilter, type TimelineDimension, type TrafficMetric } from './admin/trafficModel'
const props = defineProps<{ rows: AdminTrafficRow[]; dates: string[]; seconds: number; realtime: boolean; dimension: TimelineDimension; direction: DirectionFilter; metric: TrafficMetric; serverTime?: string }>()
const emit = defineEmits<{ drillDown: [key: string] }>()
const { t } = useI18n()
const hover = ref<number | null>(null)
const serverNow = computed(() => props.serverTime ? Date.parse(props.serverTime) : Date.now())
const directional = computed(() => directionSeries(props.rows, props.dates, 'bytes'))
const series = computed(() => props.dimension === 'direction'
  ? (() => { const data = directionSeries(props.rows, props.dates, props.metric); return [
      { key: 'out', label: t('settings.account.traffic.dirOut'), color: '#58a6ff', values: data.outbound },
      { key: 'in', label: t('settings.account.traffic.dirIn'), color: '#39c5cf', values: data.inbound },
    ] })()
  : timelineSeries(props.rows, props.dates, props.dimension, props.direction, props.metric, false))
const max = computed(() => Math.max(1, ...props.dates.map((_, i) => series.value.reduce((sum, s) => sum + s.values[i], 0))))
const complete = (i: number) => Date.parse(props.dates[i]) + props.seconds * 1000 <= serverNow.value
const pendingIndex = computed(() => props.dates.findIndex(key => Date.parse(key) <= serverNow.value && Date.parse(key) + props.seconds * 1000 > serverNow.value))
const latestComplete = computed(() => { for (let i = props.dates.length - 1; i >= 0; i--) if (complete(i)) return i; return -1 })
const rateOut = computed(() => latestComplete.value < 0 ? 0 : directional.value.outbound[latestComplete.value] / props.seconds)
const rateIn = computed(() => latestComplete.value < 0 ? 0 : directional.value.inbound[latestComplete.value] / props.seconds)
const peak = computed(() => Math.max(0, ...directional.value.outbound.map((v, i) => complete(i) ? (v + directional.value.inbound[i]) / props.seconds : 0)))
function bytes(value: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']; let n = value; let i = 0
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(i ? 1 : 0)} ${units[i]}`
}
const format = (v: number) => props.metric === 'bytes' ? bytes(v) : new Intl.NumberFormat().format(v)
const pointX = (i: number) => 24 + (i / Math.max(1, props.dates.length - 1)) * 952
function points(values: number[]): string { return values.map((v, i) => `${pointX(i)},${216 - v / max.value * 184}`).join(' ') }
function move(event: MouseEvent) {
  const rect = (event.currentTarget as SVGElement).getBoundingClientRect()
  hover.value = Math.max(0, Math.min(props.dates.length - 1, Math.round(((event.clientX - rect.left) / rect.width * 1000 - 24) / 952 * (props.dates.length - 1))))
}
const index = computed(() => hover.value ?? (pendingIndex.value >= 0 ? pendingIndex.value : Math.max(0, props.dates.length - 1)))
const selectedKey = computed(() => props.dates[index.value])
const minuteAvailable = computed(() => Date.parse(selectedKey.value) >= Math.floor(serverNow.value / 86400000) * 86400000 - 6 * 86400000)
const selectedEnd = computed(() => new Date(Date.parse(selectedKey.value) + props.seconds * 1000).toISOString().slice(11, 16))
const selectedRows = computed(() => props.rows.filter(row => row.day === selectedKey.value))
const selectedFrames = computed(() => selectedRows.value.reduce((sum, row) => sum + row.frames, 0))
const selectedTotal = computed(() => (directional.value.outbound[index.value] || 0) + (directional.value.inbound[index.value] || 0))
function clickChart(event: MouseEvent) { move(event); emit('drillDown', selectedKey.value) }
function seriesName(label: string) {
  if (props.dimension !== 'group') return label
  return t(`settings.account.traffic.categories.${label}` as Parameters<typeof t>[0])
}
</script>
<template>
  <div class="time-chart" data-testid="traffic-time-chart">
    <div class="rate-strip" v-if="realtime">
      <div><span>{{ t('settings.account.traffic.time.outRate') }}</span><strong>{{ bytes(rateOut) }}/s</strong></div>
      <div><span>{{ t('settings.account.traffic.time.inRate') }}</span><strong>{{ bytes(rateIn) }}/s</strong></div>
      <div><span>{{ t('settings.account.traffic.time.peakRate') }}</span><strong>{{ bytes(peak) }}/s</strong></div>
      <small>{{ t('settings.account.traffic.time.rateHint') }}</small>
    </div>
    <div class="chart-scale"><span>{{ format(max) }}</span><span>0</span></div>
    <svg viewBox="0 0 1000 230" preserveAspectRatio="none" role="img" :aria-label="t('settings.account.traffic.timelineTitle')" @mousemove="move" @mouseleave="hover = null" @click="clickChart">
      <g class="grid"><line v-for="y in [32, 78, 124, 170, 216]" :key="y" x1="24" :y1="y" x2="976" :y2="y" /></g>
      <template v-if="realtime || dimension === 'direction' || seconds === 60">
        <polyline v-for="s in series" :key="s.key" :points="points(s.values)" fill="none" :stroke="s.color" stroke-width="2" vector-effect="non-scaling-stroke" />
      </template>
      <template v-else>
        <g v-for="(key, i) in dates" :key="key" :opacity="complete(i) ? 1 : 0.5">
          <rect v-for="(s, j) in series" :key="s.key" :x="pointX(i) - 7" :y="216 - series.slice(0, j + 1).reduce((sum, p) => sum + p.values[i], 0) / max * 184" width="14" :height="s.values[i] / max * 184" :fill="s.color" />
        </g>
      </template>
      <rect v-if="pendingIndex >= 0" :x="pointX(pendingIndex) - 4" y="28" width="8" height="192" class="pending" />
      <line v-if="hover !== null" :x1="pointX(index)" :x2="pointX(index)" y1="28" y2="216" class="cursor" />

    </svg>
    <div class="time-axis"><span v-for="key in dates.filter((_, i) => i === 0 || i === dates.length - 1 || i % Math.max(1, Math.ceil(dates.length / 6)) === 0)" :key="key">{{ key.slice(11, 16) }}</span></div>
    <div class="tooltip" aria-live="polite">
      <strong>{{ selectedKey?.replace('T', ' ').replace('Z', '') }}–{{ selectedEnd }} UTC</strong>
      <span v-if="dates.length && !complete(index) && serverNow >= Date.parse(selectedKey)">{{ t('settings.account.traffic.time.pending') }}</span>
      <span>{{ t('settings.account.traffic.dirOut') }} {{ bytes(directional.outbound[index] || 0) }}</span>
      <span>{{ t('settings.account.traffic.dirIn') }} {{ bytes(directional.inbound[index] || 0) }}</span>
      <span>{{ selectedFrames }} {{ t('settings.account.traffic.frames') }}</span>
      <span>{{ bytes(selectedTotal / seconds) }}/s</span>
      <button v-if="seconds === 3600" type="button" :disabled="!minuteAvailable" @click="emit('drillDown', selectedKey)">{{ t(minuteAvailable ? 'settings.account.traffic.time.drillDown' : 'settings.account.traffic.time.noMinuteHistory') }}</button>
    </div>
    <div class="time-legend"><span v-for="s in series.slice(0, 12)" :key="s.key"><i :style="{ background: s.color }"></i>{{ seriesName(s.label) }} <small v-if="hover !== null">{{ format(s.values[index]) }}</small></span></div>
    <p v-if="!rows.length" class="empty">{{ t('settings.account.traffic.time.noDetail') }}</p>
  </div>
</template>
<style scoped>
.time-chart { position: relative; min-width: 0; margin-top: 10px; }
.rate-strip { display:flex; flex-wrap:wrap; gap: 24px; padding: 10px 12px; border-bottom: 1px solid var(--border); }
.rate-strip div { display:flex; flex-direction:column; gap:4px; }
.rate-strip span, .rate-strip small { font-size: 11px; color: var(--fg-dim); }
.rate-strip strong { font-size:18px; font-variant-numeric:tabular-nums; }
svg { width:100%; height:230px; display:block; cursor:crosshair; }
.grid line { stroke:var(--border); stroke-width:1; }
.time-axis { display:flex; justify-content:space-between; padding:0 2.4% 8px; color:var(--fg-dim); font-size:11px; font-variant-numeric:tabular-nums; }
.pending { fill:var(--accent); fill-opacity:0.1; stroke:var(--accent); stroke-dasharray:3 3; }
.cursor { stroke:var(--fg-dim); stroke-dasharray:4 4; }
.chart-scale { position:absolute; right:0; top:5px; font-size:10px; color:var(--fg-dim); pointer-events:none; }
.chart-scale span + span { display:none; }
.rate-strip + .chart-scale { top:82px; }
.tooltip, .time-legend { display:flex; flex-wrap:wrap; align-items:center; gap:8px 14px; font-size:11px; color:var(--fg-dim); padding:8px 12px; }
.tooltip { background:var(--bg); border-top:1px solid var(--border); }
.tooltip strong { font-weight:500; color:var(--fg); }
.tooltip button { color:var(--accent); border:1px solid var(--border); background:transparent; border-radius:5px; padding:4px 8px; cursor:pointer; }
.tooltip button:disabled { cursor:default; color:var(--fg-dim); }
.time-legend span { display:inline-flex; align-items:center; gap:5px; }
.time-legend i { width:7px; height:7px; border-radius:2px; }
.empty { text-align:center; font-size:12px; color:var(--fg-dim); }
</style>
