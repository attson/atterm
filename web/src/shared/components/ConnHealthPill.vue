<script setup lang="ts">
import { computed } from 'vue'
import type { ConnHealthSnapshot } from '../connhealth/connhealth'

interface PillLabels {
  connecting?: string
  reconnecting?: string
  off?: string
  unknownMS?: string
}

const props = defineProps<{
  health: ConnHealthSnapshot
  labels?: PillLabels
}>()

const labels = computed<Required<PillLabels>>(() => ({
  connecting: props.labels?.connecting ?? 'connecting…',
  reconnecting: props.labels?.reconnecting ?? 'reconnecting…',
  off: props.labels?.off ?? 'off',
  unknownMS: props.labels?.unknownMS ?? '—',
}))

const band = computed(() => {
  if (props.health.state === 'reconnecting' || props.health.state === 'connecting') {
    return 'band-reconnecting'
  }
  if (props.health.state === 'closed') return 'band-off'
  const rtt = props.health.rtt.last_ms
  if (rtt === null) return 'band-green' // connected but no sample yet
  if (rtt < 150) return 'band-green'
  if (rtt < 500) return 'band-yellow'
  return 'band-red'
})

const label = computed(() => {
  if (props.health.state === 'reconnecting') return labels.value.reconnecting
  if (props.health.state === 'connecting') return labels.value.connecting
  if (props.health.state === 'closed') return labels.value.off
  const rtt = props.health.rtt.last_ms
  return rtt === null ? labels.value.unknownMS : `${rtt} ms`
})
</script>

<template>
  <button
    class="conn-health-pill"
    :class="band"
    type="button"
    :aria-label="label"
  >
    <span class="dot">●</span>
    <span class="text">{{ label }}</span>
  </button>
</template>

<style scoped>
.conn-health-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  height: 20px;
  border-radius: 999px;
  border: 1px solid var(--border, #ccc);
  background: transparent;
  color: inherit;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  cursor: pointer;
  line-height: 1;
  white-space: nowrap;
}
.conn-health-pill .dot {
  font-size: 8px;
  line-height: 1;
}
.band-green {
  color: var(--good, #16a34a);
  border-color: color-mix(in srgb, var(--good, #16a34a) 35%, transparent);
}
.band-yellow {
  color: var(--warn, #d97706);
  border-color: color-mix(in srgb, var(--warn, #d97706) 35%, transparent);
}
.band-red {
  color: var(--bad, #dc2626);
  border-color: color-mix(in srgb, var(--bad, #dc2626) 35%, transparent);
}
.band-off {
  color: var(--fg-dim, #888);
}
.band-reconnecting {
  color: var(--warn, #d97706);
  animation: conn-health-pulse 1s ease-in-out infinite;
}
@keyframes conn-health-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.35;
  }
}
@media (prefers-reduced-motion: reduce) {
  .band-reconnecting {
    animation: none;
  }
}
</style>
