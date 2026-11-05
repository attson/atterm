import { computed, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'
import { inclusiveDays, type TrafficRange } from '../components/admin/trafficModel'
import { addCalendarDays, dateInTimeZone, systemTimeZone, zonedDateTimeToUtc } from '../lib/timeZone'

export type TrafficMode = 'realtime' | 'intraday' | 'summary'
export function utcTimestamp(ms: number): string { return new Date(ms).toISOString().replace('.000Z', 'Z') }

export function bucketRange(start: number, count: number, seconds: number): TrafficRange {
  const step = seconds * 1000
  const keys = (offset: number) => Array.from({ length: count }, (_, i) => utcTimestamp(start + (i + offset) * step))
  return { from: utcTimestamp(start - count * step), currentFrom: utcTimestamp(start), to: utcTimestamp(start + count * step), days: keys(0), previousDays: keys(-count) }
}

export function reportingTrafficRange(days: number, now: Date, timeZone: string): TrafficRange {
  const to = dateInTimeZone(now, timeZone)
  const currentFrom = addCalendarDays(to, -(days - 1))
  const from = addCalendarDays(currentFrom, -days)
  return { from, currentFrom, to, days: inclusiveDays(currentFrom, to), previousDays: inclusiveDays(from, addCalendarDays(currentFrom, -1)) }
}

function bucketRangeBetween(start: number, end: number, seconds: number): TrafficRange {
  const step = seconds * 1000
  const count = Math.max(0, Math.round((end - start) / step))
  return bucketRange(start, count, seconds)
}

export function useTrafficWindow(load: () => Promise<void>, reportingTimeZone: Ref<string> = ref(systemTimeZone())) {
  const mode = ref<TrafficMode>('summary')
  const minutes = ref(60)
  const date = ref(dateInTimeZone(Date.now(), reportingTimeZone.value))
  const granularity = ref<'minute' | 'hour'>('hour')
  const hour = ref<string | null>(null)
  const rangeDays = ref(7)
  const now = ref(Date.now())
  const bucket = computed(() => mode.value === 'summary' ? undefined : mode.value === 'realtime' ? 'minute' : granularity.value)
  const seconds = computed(() => bucket.value === 'hour' ? 3600 : 60)
  const range = computed(() => {
    if (mode.value === 'summary') return reportingTrafficRange(rangeDays.value, new Date(now.value), reportingTimeZone.value)
    if (mode.value === 'realtime') return bucketRange(Math.floor(now.value / 60000) * 60000 - (minutes.value - 1) * 60000, minutes.value, 60)
    const chosenDay = /^\d{4}-\d{2}-\d{2}$/.test(date.value) ? date.value : dateInTimeZone(now.value, reportingTimeZone.value)
    const dayStart = zonedDateTimeToUtc(chosenDay, 0, 0, reportingTimeZone.value)
    const nextDayStart = zonedDateTimeToUtc(addCalendarDays(chosenDay, 1), 0, 0, reportingTimeZone.value)
    if (hour.value !== null) {
      const start = Date.parse(hour.value)
      return bucketRange(start, 60, 60)
    }
    return bucketRangeBetween(dayStart, nextDayStart, seconds.value)
  })
  const autoRefresh = computed(() => mode.value === 'realtime' || (mode.value === 'intraday' && date.value === dateInTimeZone(now.value, reportingTimeZone.value)))
  // Keep refresh independent of Vue render cadence and avoid overlapping polls.
  let timer: ReturnType<typeof setInterval> | undefined
  let pending = 0
  async function refresh() {
    pending++
    try { now.value = Date.now(); await load() } finally { pending-- }
  }
  function onVisible() { if (!document.hidden && autoRefresh.value && !pending) void refresh() }
  watch([mode, minutes, date, granularity, hour, reportingTimeZone], () => { void refresh() })
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
    else if (bucket.value === 'hour' && Date.parse(key) >= now.value - 7 * 86400000) { hour.value = key; granularity.value = 'minute' }
  }
  return { mode, minutes, date, granularity, hour, rangeDays, bucket, seconds, range, now, autoRefresh, refresh, drillDown }
}
