<script setup lang="ts">
import { computed } from 'vue'
import type { ConnHealthSnapshot } from '../connhealth/connhealth'

interface DrawerLabels {
  title?: string
  rttNow?: string
  rttP50P95?: string
  bytesIn?: string
  bytesOut?: string
  state?: string
  reconnectsLastHour?: string
  reconnectsTime?: string
  reconnectsReason?: string
  reconnectsDowntime?: string
  seqGaps?: string
  close?: string
}

const props = defineProps<{
  health: ConnHealthSnapshot
  open: boolean
  labels?: DrawerLabels
}>()

defineEmits<{ (e: 'close'): void }>()

const t = computed<Required<DrawerLabels>>(() => ({
  title: props.labels?.title ?? 'Connection health',
  rttNow: props.labels?.rttNow ?? 'RTT (now)',
  rttP50P95: props.labels?.rttP50P95 ?? 'p50 / p95 (5 min)',
  bytesIn: props.labels?.bytesIn ?? '↓ in',
  bytesOut: props.labels?.bytesOut ?? '↑ out',
  state: props.labels?.state ?? 'State',
  reconnectsLastHour: props.labels?.reconnectsLastHour ?? 'Reconnects (1 h)',
  reconnectsTime: props.labels?.reconnectsTime ?? 'time',
  reconnectsReason: props.labels?.reconnectsReason ?? 'reason',
  reconnectsDowntime: props.labels?.reconnectsDowntime ?? 'downtime',
  seqGaps: props.labels?.seqGaps ?? 'Seq gaps observed:',
  close: props.labels?.close ?? 'Close',
}))

const sparkPath = computed(() => {
  const samples = props.health.rtt_samples
  if (samples.length < 2) return ''
  const w = 240,
    h = 60,
    pad = 4
  const xs = (i: number) => pad + (i / (samples.length - 1)) * (w - 2 * pad)
  const values = samples.map((s) => s.rtt_ms)
  const minV = Math.min(...values)
  const maxV = Math.max(...values)
  const span = Math.max(1, maxV - minV)
  const ys = (v: number) => pad + (1 - (v - minV) / span) * (h - 2 * pad)
  return samples
    .map((s, i) => `${i === 0 ? 'M' : 'L'} ${xs(i).toFixed(1)} ${ys(s.rtt_ms).toFixed(1)}`)
    .join(' ')
})

const fmtKBs = (n: number) => `${(n / 1024).toFixed(1)} KB/s`
const fmtDownTime = (ms: number) => (ms < 1000 ? `${ms} ms` : `${Math.round(ms / 1000)} s`)
const fmtAt = (ms: number) => new Date(ms).toLocaleTimeString()
</script>

<template>
  <aside v-if="open" class="drawer" role="dialog" :aria-label="t.title">
    <header class="head">
      <span>{{ t.title }}</span>
      <button class="close" type="button" :aria-label="t.close" @click="$emit('close')">×</button>
    </header>

    <section class="rtt">
      <div class="row">
        <span class="metric-label">{{ t.rttNow }}</span>
        <span class="metric-value">{{ health.rtt.last_ms ?? '—' }} ms</span>
      </div>
      <div class="row">
        <span class="metric-label">{{ t.rttP50P95 }}</span>
        <span class="metric-value"
          >{{ health.rtt.p50_ms ?? '—' }} / {{ health.rtt.p95_ms ?? '—' }} ms</span
        >
      </div>
      <svg width="240" height="60" class="spark" aria-hidden="true">
        <path :d="sparkPath" fill="none" stroke="currentColor" stroke-width="1.5" />
      </svg>
    </section>

    <section class="bytes">
      <div class="row">
        <span class="metric-label">{{ t.bytesIn }}</span>
        <span class="metric-value">{{ fmtKBs(health.bytes.in_per_sec) }}</span>
      </div>
      <div class="row">
        <span class="metric-label">{{ t.bytesOut }}</span>
        <span class="metric-value">{{ fmtKBs(health.bytes.out_per_sec) }}</span>
      </div>
    </section>

    <section class="recon">
      <div class="row">
        <span class="metric-label">{{ t.state }}</span>
        <span class="metric-value">{{ health.state }}</span>
      </div>
      <div class="row">
        <span class="metric-label">{{ t.reconnectsLastHour }}</span>
        <span class="metric-value">{{ health.reconnect.count_last_hour }}</span>
      </div>
      <table v-if="health.reconnect.history.length > 0" class="reconn-table">
        <thead>
          <tr>
            <th>{{ t.reconnectsTime }}</th>
            <th>{{ t.reconnectsReason }}</th>
            <th>{{ t.reconnectsDowntime }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="ev in health.reconnect.history" :key="ev.at_ms">
            <td>{{ fmtAt(ev.at_ms) }}</td>
            <td>{{ ev.reason }}</td>
            <td>{{ fmtDownTime(ev.duration_ms) }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <section v-if="health.seq_gaps > 0" class="gaps">
      {{ t.seqGaps }} {{ health.seq_gaps }}
    </section>
  </aside>
</template>

<style scoped>
.drawer {
  position: fixed;
  top: 36px;
  right: 8px;
  z-index: 1100;
  width: 280px;
  background: var(--bg, #fff);
  color: var(--fg, #111);
  border: 1px solid var(--border, #ddd);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
  padding: 10px 12px;
  font-size: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-weight: 600;
}
.close {
  background: transparent;
  border: 0;
  cursor: pointer;
  font-size: 16px;
  line-height: 1;
  color: inherit;
}
.row {
  display: flex;
  justify-content: space-between;
  padding: 2px 0;
}
.metric-label {
  color: var(--fg-dim, #666);
}
.metric-value {
  font-variant-numeric: tabular-nums;
}
.spark {
  color: var(--good, #16a34a);
  display: block;
}
.reconn-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 11px;
}
.reconn-table th,
.reconn-table td {
  text-align: left;
  padding: 2px 4px;
  border-top: 1px solid var(--border, #eee);
}
.gaps {
  color: var(--warn, #d97706);
}
</style>
