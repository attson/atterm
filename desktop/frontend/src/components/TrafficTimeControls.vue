<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '../i18n/useI18n'
import { resolvedLocale } from '../i18n'
import type { TrafficMode } from '../composables/useTrafficWindow'
import { addCalendarDays, dateInTimeZone, formatLocalDateTime, systemTimeZone } from '../lib/timeZone'
const { t } = useI18n()
const mode = defineModel<TrafficMode>('mode', { required: true })
const minutes = defineModel<number>('minutes', { required: true })
const date = defineModel<string>('date', { required: true })
const granularity = defineModel<'minute' | 'hour'>('granularity', { required: true })
const hour = defineModel<string | null>('hour', { required: true })
const props = defineProps<{ updatedAt?: string; autoRefresh: boolean; now: number; failed?: boolean; reportingTimeZone: string }>()
const displayTimeZone = systemTimeZone()
const updatedAtLabel = computed(() => props.updatedAt
  ? formatLocalDateTime(props.updatedAt, resolvedLocale.value, displayTimeZone)
  : '')
const today = computed(() => dateInTimeZone(props.now, props.reportingTimeZone))
const minimumDate = computed(() => addCalendarDays(today.value, -(granularity.value === 'minute' ? 6 : 89)))
function changeGranularity(value: 'minute' | 'hour') {
  granularity.value = value
  hour.value = null
  if (date.value < minimumDate.value) date.value = minimumDate.value
}
function previousDay() {
  date.value = addCalendarDays(today.value, -1)
  hour.value = null
}
</script>
<template>
  <div class="time-controls" data-testid="traffic-time-controls">
    <div class="time-row">
      <div class="time-segments" role="group" :aria-label="t('settings.account.traffic.time.mode')">
        <button v-for="item in (['realtime', 'intraday', 'summary'] as const)" :key="item" type="button" :class="{ active: mode === item }" :data-testid="`traffic-mode-${item}`" @click="mode = item">{{ t(`settings.account.traffic.time.${item}`) }}</button>
      </div>
      <template v-if="mode === 'realtime'">
        <div class="time-segments"><button v-for="n in [15, 60]" :key="n" type="button" :class="{ active: minutes === n }" @click="minutes = n">{{ t('settings.account.traffic.time.lastMinutes', { count: n }) }}</button></div>
      </template>
      <template v-if="mode === 'intraday'">
        <div class="time-segments"><button type="button" @click="date = today; hour = null">{{ t('settings.account.traffic.time.today') }}</button><button type="button" @click="previousDay">{{ t('settings.account.traffic.time.yesterday') }}</button></div>
        <input v-model="date" type="date" :min="minimumDate" :max="today" :aria-label="t('settings.account.traffic.time.date')" @change="hour = null; date = date < minimumDate ? minimumDate : date > today ? today : date || today" />
        <div class="time-segments"><button v-for="g in (['hour', 'minute'] as const)" :key="g" type="button" :class="{ active: granularity === g }" @click="changeGranularity(g)">{{ t(`settings.account.traffic.time.${g}`) }}</button></div>
        <button v-if="hour !== null" type="button" @click="hour = null; granularity = 'hour'">{{ t('settings.account.traffic.time.backToDay') }}</button>
      </template>
    </div>
    <p v-if="mode !== 'summary'" role="status">
      <span v-if="autoRefresh" class="live-dot"></span>{{ autoRefresh ? t('settings.account.traffic.time.autoRefresh') : t('settings.account.traffic.time.historical') }}
      · {{ t('settings.account.traffic.time.reportingZone', { zone: reportingTimeZone }) }} · {{ t('settings.account.traffic.time.displayZone', { zone: displayTimeZone }) }} · {{ t(`settings.account.traffic.time.${granularity === 'hour' && mode === 'intraday' ? 'hour' : 'minute'}`) }}
      <span v-if="updatedAtLabel"> · {{ t('settings.account.traffic.time.updated', { time: updatedAtLabel }) }}</span>
      <span v-if="props.failed" class="time-error"> · {{ t('settings.account.traffic.time.stale') }}</span>
    </p>
    <p v-if="mode !== 'summary'">{{ t('settings.account.traffic.time.retention') }}</p>
  </div>
</template>
<style scoped>
.time-controls { display: flex; flex-direction: column; gap: 7px; min-width: 0; }
.time-row { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.time-segments { display: inline-flex; border: 1px solid var(--border); border-radius: 7px; overflow: hidden; }
button, input { height: 30px; border: 1px solid var(--border); background: var(--bg); color: var(--fg); border-radius: 6px; padding: 0 10px; font: inherit; font-size: 12px; }
button { cursor: pointer; }
.time-segments button { border: 0; border-radius: 0; color: var(--fg-dim); }
button:hover { color: var(--accent); }
button.active { background: var(--accent); color: var(--bg); }
input { color-scheme: dark; }
p { margin: 0; font-size: 11px; color: var(--fg-dim); }
.live-dot { display: inline-block; width: 6px; height: 6px; margin-right: 5px; border-radius: 50%; background: var(--good); }
.time-error { color: var(--bad); }
</style>
