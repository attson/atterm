import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { trafficRange, type TrafficRange } from '../components/admin/trafficModel'

export type TrafficMode = 'realtime' | 'intraday' | 'summary'
export function utcTimestamp(ms: number): string { return new Date(ms).toISOString().replace('.000Z', 'Z') }

export function bucketRange(start: number, count: number, seconds: number): TrafficRange {
  const step = seconds * 1000
  const keys = (offset: number) => Array.from({ length: count }, (_, i) => utcTimestamp(start + (i + offset) * step))
  return { from: utcTimestamp(start - count * step), currentFrom: utcTimestamp(start), to: utcTimestamp(start + count * step), days: keys(0), previousDays: keys(-count) }
}

export function useTrafficWindow(load: () => Promise<void>) {
  const mode = ref<TrafficMode>('summary')
  const minutes = ref(60)
  const date = ref(new Date().toISOString().slice(0, 10))
  const granularity = ref<'minute' | 'hour'>('hour')
  const hour = ref<number | null>(null)
  const rangeDays = ref(7)
  const now = ref(Date.now())
  const bucket = computed(() => mode.value === 'summary' ? undefined : mode.value === 'realtime' ? 'minute' : granularity.value)
  const seconds = computed(() => bucket.value === 'hour' ? 3600 : 60)
  const range = computed(() => {
    if (mode.value === 'summary') return trafficRange(rangeDays.value, new Date(now.value))
    if (mode.value === 'realtime') return bucketRange(Math.floor(now.value / 60000) * 60000 - (minutes.value - 1) * 60000, minutes.value, 60)
    const chosenDate = Date.parse(`${date.value}T00:00:00Z`)
    const start = (Number.isFinite(chosenDate) ? chosenDate : Math.floor(now.value / 86400000) * 86400000) + (hour.value ?? 0) * 3600000
    return bucketRange(start, hour.value !== null ? 60 : granularity.value === 'minute' ? 1440 : 24, seconds.value)
  })
  const autoRefresh = computed(() => mode.value === 'realtime' || (mode.value === 'intraday' && date.value === new Date(now.value).toISOString().slice(0, 10)))
  // Keep refresh independent of Vue render cadence and avoid overlapping polls.
  let timer: ReturnType<typeof setInterval> | undefined
  let pending = 0
  async function refresh() {
    pending++
    try { now.value = Date.now(); await load() } finally { pending-- }
  }
  function onVisible() { if (!document.hidden && autoRefresh.value && !pending) void refresh() }
  watch([mode, minutes, date, granularity, hour], () => { void refresh() })
  onMounted(() => {
    timer = setInterval(() => { if (!document.hidden && autoRefresh.value && !pending) void refresh() }, 10000)
    document.addEventListener('visibilitychange', onVisible)
  })
  onBeforeUnmount(() => {
    if (timer) clearInterval(timer)
    document.removeEventListener('visibilitychange', onVisible)
  })
  function drillDown(key: string) {
    if (mode.value === 'summary') { date.value = key.slice(0, 10); granularity.value = 'hour'; hour.value = null; mode.value = 'intraday' }
    else if (bucket.value === 'hour' && Date.parse(key) >= Math.floor(now.value / 86400000) * 86400000 - 6 * 86400000) { date.value = key.slice(0, 10); hour.value = new Date(key).getUTCHours(); granularity.value = 'minute' }
  }
  return { mode, minutes, date, granularity, hour, rangeDays, bucket, seconds, range, now, autoRefresh, refresh, drillDown }
}
