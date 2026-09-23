<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { NSelect, NSpin, useMessage } from 'naive-ui'
import {
  Activity,
  ArrowDownRight,
  ArrowUpRight,
  ChevronDown,
  Clock3,
  Eye,
  Gauge,
  Minus,
  Network,
  Radio,
  RefreshCw,
  Search,
  Server,
  Users,
  X,
} from 'lucide-vue-next'
import { ApiError } from '@shared/api/client'
import { getAdminHealth, getTrafficStats } from '@shared/api/admin'
import type { AdminHealthResponse, AdminTrafficRow } from '@shared/api/types'
import { useI18n } from '@shared/i18n/useI18n'
import {
  categorySummaries,
  dailyTotals,
  directionSeries,
  selectedRows,
  timelineSeries,
  totalMetric,
  trafficRange,
  trendPercent,
  userIdentity,
  userSummaries,
  type DirectionFilter,
  type TimelineDimension,
  type TrafficMetric,
} from './trafficModel'

const { t } = useI18n()
const message = useMessage()

const rows = ref<AdminTrafficRow[]>([])
const health = ref<AdminHealthResponse | null>(null)
const loading = ref(false)
const rangeDays = ref(7)
const metric = ref<TrafficMetric>('bytes')
const dimension = ref<TimelineDimension>('group')
const direction = ref<DirectionFilter>('all')
const selectedUserIDs = ref<string[]>([])
const selectionInitialized = ref(false)
const userFilterOpen = ref(false)
const userQuery = ref('')
const userTableUserID = ref<string | null>(null)
const categoryFilter = ref('all')
const drawerUserID = ref<string | null>(null)
const selectorRoot = ref<HTMLElement | null>(null)

const range = computed(() => trafficRange(rangeDays.value))
const allUsers = computed(() => userIdentity(rows.value))
const selectedSet = computed(() => new Set(selectedUserIDs.value))
const currentRows = computed(() => selectedRows(rows.value, selectedSet.value, new Set(range.value.days)))
const previousRows = computed(() => selectedRows(rows.value, selectedSet.value, new Set(range.value.previousDays)))
const selectedUsers = computed(() => allUsers.value.filter((user) => selectedSet.value.has(user.id)))
const filteredSelectorUsers = computed(() => {
  const query = userQuery.value.trim().toLowerCase()
  return allUsers.value.filter((user) => !query || user.email.toLowerCase().includes(query) || user.id.toLowerCase().includes(query))
})
const allSelected = computed(() => allUsers.value.length > 0 && selectedUserIDs.value.length === allUsers.value.length)

const totalBytes = computed(() => totalMetric(currentRows.value, 'bytes'))
const totalFrames = computed(() => totalMetric(currentRows.value, 'frames'))
const outboundBytes = computed(() => totalMetric(currentRows.value.filter((row) => row.direction === 1), 'bytes'))
const inboundBytes = computed(() => totalMetric(currentRows.value.filter((row) => row.direction === 0), 'bytes'))
const previousTotalBytes = computed(() => totalMetric(previousRows.value, 'bytes'))
const previousTotalFrames = computed(() => totalMetric(previousRows.value, 'frames'))
const previousOutbound = computed(() => totalMetric(previousRows.value.filter((row) => row.direction === 1), 'bytes'))
const previousInbound = computed(() => totalMetric(previousRows.value.filter((row) => row.direction === 0), 'bytes'))

const kpis = computed(() => [
  { key: 'total', label: t('admin.traffic.kpiTotal'), value: humanBytes(totalBytes.value), trend: trendPercent(totalBytes.value, previousTotalBytes.value) },
  { key: 'out', label: t('admin.traffic.kpiOutbound'), value: humanBytes(outboundBytes.value), trend: trendPercent(outboundBytes.value, previousOutbound.value) },
  { key: 'in', label: t('admin.traffic.kpiInbound'), value: humanBytes(inboundBytes.value), trend: trendPercent(inboundBytes.value, previousInbound.value) },
  { key: 'frames', label: t('admin.traffic.kpiFrames'), value: compactNumber(totalFrames.value), trend: trendPercent(totalFrames.value, previousTotalFrames.value) },
])

const categories = computed(() => categorySummaries(currentRows.value))
const chartSeries = computed(() => dimension.value === 'direction'
  ? []
  : timelineSeries(
      currentRows.value,
      range.value.days,
      dimension.value,
      direction.value,
      metric.value,
      selectedUsers.value.length > 1,
    ))
const chartDirection = computed(() => directionSeries(currentRows.value, range.value.days, metric.value))
const chartMax = computed(() => Math.max(1, ...chartSeries.value.flatMap((series) => series.values), ...chartDirection.value.inbound, ...chartDirection.value.outbound))
const stackedMax = computed(() => Math.max(1, ...range.value.days.map((_, index) => chartSeries.value.reduce((sum, series) => sum + series.values[index], 0))))
const visibleMatrixSeries = computed(() => chartSeries.value.slice(0, 28))
const isMatrix = computed(() => selectedUsers.value.length > 1 && dimension.value !== 'direction')

const userTableRows = computed(() => {
  const source = categoryFilter.value === 'all' ? rows.value : rows.value.filter((row) => (row.category || 'other') === categoryFilter.value)
  return userSummaries(source, selectedSet.value, range.value)
})
const userTableOptions = computed(() => userTableRows.value.map((user) => ({
  label: `${user.email} · ${user.id}`,
  value: user.id,
})))
const userRows = computed(() => userTableUserID.value
  ? userTableRows.value.filter((user) => user.id === userTableUserID.value)
  : userTableRows.value)
const drawerUser = computed(() => userRows.value.find((user) => user.id === drawerUserID.value) || null)
const drawerRows = computed(() => currentRows.value.filter((row) => row.user_id === drawerUserID.value))
const drawerDays = computed(() => dailyTotals(drawerRows.value, range.value.days, 'bytes'))
const drawerDayMax = computed(() => Math.max(1, ...drawerDays.value))
const drawerFrames = computed(() => {
  const values = new Map<string, { name: string; category: string; bytes: number; frames: number }>()
  for (const row of drawerRows.value) {
    const name = row.frame_type_name || String(row.frame_type ?? '?')
    const cell = values.get(name) || { name, category: row.category || 'other', bytes: 0, frames: 0 }
    cell.bytes += row.bytes
    cell.frames += row.frames
    values.set(name, cell)
  }
  return [...values.values()].sort((a, b) => b.bytes - a.bytes).slice(0, 8)
})

const activityRows = computed(() => userRows.value.slice(0, 12).map((user) => {
  const own = currentRows.value.filter((row) => row.user_id === user.id)
  return { user, values: dailyTotals(own, range.value.days, 'bytes') }
}))
const activityMax = computed(() => Math.max(1, ...activityRows.value.flatMap((item) => item.values)))
const categoryDensity = computed(() => categories.value.map((category) => ({
  ...category,
  density: category.frames ? category.bytes / category.frames : 0,
})).sort((a, b) => b.density - a.density))
const densityMax = computed(() => Math.max(1, ...categoryDensity.value.map((item) => item.density)))

async function load() {
  loading.value = true
  const requestedRange = trafficRange(rangeDays.value)
  try {
    const [trafficResult, healthResult] = await Promise.allSettled([
      getTrafficStats({ view: 'detail', bucket: 'day', from: requestedRange.from, to: requestedRange.to }),
      getAdminHealth(),
    ])
    if (trafficResult.status === 'rejected') throw trafficResult.reason
    const traffic = trafficResult.value
    const platform = healthResult.status === 'fulfilled' ? healthResult.value : null
    rows.value = traffic.rows
    health.value = platform
    const ids = userIdentity(traffic.rows).map((user) => user.id)
    if (!selectionInitialized.value) {
      selectedUserIDs.value = ids
      selectionInitialized.value = true
    } else {
      const valid = new Set(ids)
      selectedUserIDs.value = selectedUserIDs.value.filter((id) => valid.has(id))
    }
  } catch (error) {
    if (error instanceof ApiError) message.error(t('admin.traffic.loadFailed'))
    rows.value = []
  } finally {
    loading.value = false
  }
}

function setRange(days: number) {
  if (rangeDays.value === days) return
  rangeDays.value = days
  categoryFilter.value = 'all'
  void load()
}

function setDimension(next: TimelineDimension) {
  dimension.value = next
}

function toggleUser(id: string) {
  selectedUserIDs.value = selectedSet.value.has(id)
    ? selectedUserIDs.value.filter((value) => value !== id)
    : [...selectedUserIDs.value, id]
}

function onlyUser(id: string) {
  selectedUserIDs.value = [id]
}

function selectAllUsers() {
  selectedUserIDs.value = allUsers.value.map((user) => user.id)
}

function clearAllUsers() {
  selectedUserIDs.value = []
}

function onDocumentPointer(event: PointerEvent) {
  if (userFilterOpen.value && !selectorRoot.value?.contains(event.target as Node)) userFilterOpen.value = false
}

function trendText(value: number | null): string {
  if (value === null) return t('admin.traffic.trendNew')
  if (Math.abs(value) < 0.05) return t('admin.traffic.trendFlat')
  return `${value > 0 ? '+' : ''}${value.toFixed(1)}%`
}

function humanBytes(value: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let result = value
  let index = 0
  while (result >= 1024 && index < units.length - 1) {
    result /= 1024
    index++
  }
  return `${result.toFixed(index === 0 ? 0 : result >= 100 ? 0 : result >= 10 ? 1 : 2)} ${units[index]}`
}

function compactNumber(value: number): string {
  return new Intl.NumberFormat(undefined, { notation: value >= 10000 ? 'compact' : 'standard', maximumFractionDigits: 1 }).format(value)
}

function formatMetric(value: number): string {
  return metric.value === 'bytes' ? humanBytes(value) : compactNumber(value)
}

function categoryName(key: string): string {
  const translated = t(`admin.traffic.categories.${key}`)
  return translated === `admin.traffic.categories.${key}` ? key : translated
}

function shortDay(day: string): string {
  return day.slice(5).replace('-', '/')
}

function linePoints(values: number[]): string {
  const count = Math.max(1, values.length - 1)
  return values.map((value, index) => `${64 + (index / count) * 904},${276 - (value / chartMax.value) * 224}`).join(' ')
}

function matrixColor(value: number): string {
  const alpha = value === 0 ? 0.06 : 0.18 + (value / chartMax.value) * 0.72
  return `rgba(88, 166, 255, ${alpha})`
}

function activityColor(value: number): string {
  const alpha = value === 0 ? 0.05 : 0.18 + (value / activityMax.value) * 0.72
  return `rgba(57, 197, 207, ${alpha})`
}

async function openDrawer(id: string) {
  drawerUserID.value = id
  await nextTick()
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointer)
  void load()
})
onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocumentPointer))
</script>

<template>
  <div class="traffic-dashboard">
    <div class="traffic-titlebar">
      <div>
        <h1>{{ t('admin.traffic.title') }}</h1>
        <p>{{ t('admin.traffic.timezone') }}</p>
      </div>
      <button class="icon-button" type="button" :aria-label="t('admin.traffic.refresh')" :title="t('admin.traffic.refresh')" :disabled="loading" data-test="traffic-refresh" @click="load">
        <RefreshCw :class="{ spinning: loading }" />
      </button>
    </div>

    <n-spin :show="loading">
      <section class="scope-section platform-section">
        <div class="scope-heading">
          <div>
            <span>{{ t('admin.traffic.platformScope') }}</span>
            <h2>{{ t('admin.traffic.platformStatus') }}</h2>
          </div>
          <small><Radio />{{ t('admin.traffic.globalRealtime') }}</small>
        </div>
        <div class="platform-strip" data-test="platform-status">
          <div class="platform-cell platform-health">
            <span><Activity />{{ t('admin.traffic.relayStatus') }}</span>
            <strong>{{ health ? t('admin.traffic.online') : t('admin.traffic.unknown') }}</strong>
            <small>{{ health?.version || '-' }}</small>
          </div>
          <div class="platform-cell">
            <span><Server />{{ t('admin.traffic.activeSessions') }}</span>
            <strong>{{ health?.active_sessions ?? '-' }}</strong>
            <small>{{ t('admin.traffic.liveValue') }}</small>
          </div>
          <div class="platform-cell">
            <span><Network />{{ t('admin.traffic.relayInstances') }}</span>
            <strong>{{ health?.relay_instances ?? '-' }}</strong>
            <small>{{ t('admin.traffic.liveInstances') }}</small>
          </div>
          <div class="platform-cell">
            <span><Gauge />{{ t('admin.traffic.activeUplinks') }}</span>
            <strong>{{ health?.active_uplinks ?? '-' }}</strong>
            <small>{{ t('admin.traffic.flushEvery', { seconds: health?.traffic_flush_interval_seconds ?? 60 }) }}</small>
          </div>
        </div>
        <div class="platform-strip" data-test="direct-metrics">
          <div class="platform-cell">
            <span><Network />{{ t('admin.traffic.directAttempts') }}</span>
            <strong>{{ health ? compactNumber(health.direct_attempts) : '-' }}</strong>
            <small>{{ health?.direct_signal_enabled ? t('admin.traffic.directEnabled') : t('admin.traffic.directDisabled') }}</small>
          </div>
          <div class="platform-cell">
            <span><Activity />{{ t('admin.traffic.directSuccesses') }}</span>
            <strong>{{ health ? compactNumber(health.direct_successes) : '-' }}</strong>
            <small>{{ t('admin.traffic.directSuccessesHint') }}</small>
          </div>
          <div class="platform-cell">
            <span><ArrowDownRight />{{ t('admin.traffic.directFallbacks') }}</span>
            <strong>{{ health ? compactNumber(health.direct_fallbacks) : '-' }}</strong>
            <small>{{ t('admin.traffic.directFallbacksHint') }}</small>
          </div>
          <div class="platform-cell">
            <span><Gauge />{{ t('admin.traffic.directBytesAvoided') }}</span>
            <strong>{{ health ? humanBytes(health.direct_bytes_avoided) : '-' }}</strong>
            <small>{{ t('admin.traffic.directBytesAvoidedHint') }}</small>
          </div>
        </div>
      </section>

      <section class="scope-section user-section">
        <div class="user-heading">
          <div>
            <span>{{ t('admin.traffic.userScope') }}</span>
            <h2>{{ t('admin.traffic.userAnalysis') }}</h2>
          </div>
          <div class="filterbar">
            <div class="segmented" role="group" :aria-label="t('admin.traffic.range')">
              <button v-for="days in [7, 30, 90]" :key="days" type="button" :class="{ active: rangeDays === days }" :data-test="`range-${days}`" @click="setRange(days)">{{ t('admin.traffic.days', { days }) }}</button>
            </div>
            <div ref="selectorRoot" class="user-selector">
              <button class="selector-trigger" type="button" :class="{ active: userFilterOpen }" data-test="user-filter-trigger" @click="userFilterOpen = !userFilterOpen">
                <Users />
                <span v-if="allSelected">{{ t('admin.traffic.allUsers') }}</span>
                <span v-else-if="selectedUsers.length === 1">{{ selectedUsers[0].email }}</span>
                <span v-else>{{ t('admin.traffic.selectedUsers', { selected: selectedUsers.length, total: allUsers.length }) }}</span>
                <ChevronDown />
              </button>
              <div v-if="userFilterOpen" class="selector-popover" role="dialog" :aria-label="t('admin.traffic.filterUsers')">
                <div class="selector-head">
                  <div><strong>{{ t('admin.traffic.filterUsers') }}</strong><span>{{ t('admin.traffic.selectedCount', { selected: selectedUsers.length, total: allUsers.length }) }}</span></div>
                  <button type="button" data-test="toggle-all-users" @click="allSelected ? clearAllUsers() : selectAllUsers()">{{ allSelected ? t('admin.traffic.clearAll') : t('admin.traffic.selectAll') }}</button>
                </div>
                <label class="search-box"><Search /><input v-model="userQuery" :placeholder="t('admin.traffic.searchUsers')" /></label>
                <div class="selector-options">
                  <div v-for="user in filteredSelectorUsers" :key="user.id" class="selector-option">
                    <label><input type="checkbox" :checked="selectedSet.has(user.id)" @change="toggleUser(user.id)" /><span><strong>{{ user.email }}</strong><small>{{ user.id }}</small></span></label>
                    <button type="button" :data-test="`only-${user.id}`" @click="onlyUser(user.id)">{{ t('admin.traffic.only') }}</button>
                  </div>
                  <p v-if="!filteredSelectorUsers.length">{{ t('admin.traffic.noUserMatch') }}</p>
                </div>
                <div class="selector-foot">
                  <button type="button" data-test="restore-users" @click="selectAllUsers">{{ t('admin.traffic.restoreAll') }}</button>
                  <button class="primary" type="button" @click="userFilterOpen = false">{{ t('admin.traffic.done') }}</button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div v-if="!selectedUsers.length" class="selection-empty" data-test="traffic-selection-empty">
          <Users />
          <h3>{{ t('admin.traffic.noUsersSelected') }}</h3>
          <button type="button" data-test="restore-users-empty" @click="selectAllUsers">{{ t('admin.traffic.restoreAllUsers') }}</button>
        </div>

        <template v-else>
          <div v-if="!allSelected" class="selection-banner">
            <Users />
            <strong>{{ t('admin.traffic.selectedUserCount', { count: selectedUsers.length }) }}</strong>
            <span>{{ selectedUsers.slice(0, 3).map((user) => user.email).join(', ') }}</span>
            <button type="button" :aria-label="t('admin.traffic.restoreAll')" @click="selectAllUsers"><X /></button>
          </div>

          <div class="kpi-strip">
            <div v-for="kpi in kpis" :key="kpi.key" class="kpi-cell">
              <span>{{ kpi.label }}</span>
              <div><strong>{{ kpi.value }}</strong><small :class="{ up: kpi.trend !== null && kpi.trend > 0, down: kpi.trend !== null && kpi.trend < 0 }">
                <ArrowUpRight v-if="kpi.trend !== null && kpi.trend > 0" />
                <ArrowDownRight v-else-if="kpi.trend !== null && kpi.trend < 0" />
                <Minus v-else />{{ trendText(kpi.trend) }}
              </small></div>
            </div>
          </div>

          <section class="timeline-panel">
            <div class="panel-head timeline-head">
              <div>
                <span>{{ t('admin.traffic.coreAnalysis') }}</span>
                <h3>{{ t('admin.traffic.timelineTitle') }}</h3>
                <p>{{ t('admin.traffic.timelineScope', { count: selectedUsers.length }) }}</p>
              </div>
              <div class="timeline-controls">
                <div><small>{{ t('admin.traffic.view') }}</small><div class="segmented"><button type="button" :class="{ active: dimension === 'group' }" data-test="view-group" @click="setDimension('group')">{{ t('admin.traffic.groupTimeline') }}</button><button type="button" :class="{ active: dimension === 'frame' }" data-test="view-frame" @click="setDimension('frame')">{{ t('admin.traffic.frameTimeline') }}</button><button type="button" :class="{ active: dimension === 'direction' }" data-test="view-direction" @click="setDimension('direction')">{{ t('admin.traffic.directionTrend') }}</button></div></div>
                <div v-if="dimension !== 'direction'"><small>{{ t('admin.traffic.colDirection') }}</small><div class="segmented"><button v-for="item in (['all', 'out', 'in'] as DirectionFilter[])" :key="item" type="button" :class="{ active: direction === item }" @click="direction = item">{{ t(`admin.traffic.direction.${item}`) }}</button></div></div>
                <div><small>{{ t('admin.traffic.metric') }}</small><div class="segmented"><button type="button" :class="{ active: metric === 'bytes' }" data-test="metric-bytes" @click="metric = 'bytes'">{{ t('admin.traffic.bytes') }}</button><button type="button" :class="{ active: metric === 'frames' }" data-test="metric-frames" @click="metric = 'frames'">{{ t('admin.traffic.frames') }}</button></div></div>
              </div>
            </div>
            <div class="chart-meta"><span>{{ isMatrix ? t('admin.traffic.multiUserMatrix') : dimension === 'direction' ? t('admin.traffic.directionTrend') : t('admin.traffic.stackedTimeline') }}</span><small>{{ t('admin.traffic.bucketCount', { count: range.days.length }) }}</small></div>

            <div v-if="!currentRows.length" class="chart-empty">{{ t('admin.traffic.empty') }}</div>
            <div v-else-if="dimension === 'direction'" class="svg-chart" data-test="direction-chart">
              <svg viewBox="0 0 1000 320" preserveAspectRatio="none" role="img" :aria-label="t('admin.traffic.directionTrend')">
                <g class="grid-lines"><line v-for="y in [52, 108, 164, 220, 276]" :key="y" x1="64" :y1="y" x2="968" :y2="y" /></g>
                <polyline class="line outbound" :points="linePoints(chartDirection.outbound)" />
                <polyline class="line inbound" :points="linePoints(chartDirection.inbound)" />
                <g v-for="(day, index) in range.days" :key="day"><text v-if="index === 0 || index === range.days.length - 1 || index % Math.max(1, Math.floor(range.days.length / 6)) === 0" :x="64 + (index / Math.max(1, range.days.length - 1)) * 904" y="305" text-anchor="middle">{{ shortDay(day) }}</text></g>
              </svg>
              <div class="legend"><span><i class="outbound"></i>{{ t('admin.traffic.dirOut') }}</span><span><i class="inbound"></i>{{ t('admin.traffic.dirIn') }}</span></div>
            </div>
            <div v-else-if="isMatrix" class="matrix-chart" data-test="matrix-chart">
              <div class="matrix-grid" :style="{ '--columns': String(range.days.length) }">
                <template v-for="series in visibleMatrixSeries" :key="series.key">
                  <div class="matrix-label" :title="`${series.userLabel} · ${categoryName(series.label)}`"><strong>{{ series.userLabel }}</strong><span>{{ dimension === 'group' ? categoryName(series.label) : series.label }}</span></div>
                  <div class="matrix-cells"><i v-for="(value, index) in series.values" :key="index" :style="{ background: matrixColor(value) }" :title="`${series.userLabel} · ${series.label} · ${range.days[index]} · ${formatMetric(value)}`"></i></div>
                </template>
              </div>
              <div class="matrix-axis" :style="{ '--columns': String(range.days.length) }"><span v-for="day in range.days" :key="day">{{ shortDay(day) }}</span></div>
            </div>
            <div v-else class="bar-chart" data-test="stacked-chart">
              <div v-for="(day, dayIndex) in range.days" :key="day" class="bar-column">
                <div class="bar-stack">
                  <i v-for="series in chartSeries.slice(0, 8)" :key="series.key" :style="{ height: `${(series.values[dayIndex] / stackedMax) * 100}%`, background: series.color }" :title="`${series.label} · ${formatMetric(series.values[dayIndex])}`"></i>
                </div>
                <span>{{ shortDay(day) }}</span>
              </div>
            </div>
            <div v-if="dimension !== 'direction' && currentRows.length" class="series-legend"><span v-for="series in chartSeries.slice(0, 8)" :key="series.key"><i :style="{ background: series.color }"></i>{{ dimension === 'group' ? categoryName(series.label) : series.label }}</span></div>

            <div v-if="dimension !== 'direction' && chartSeries.length" class="timeline-table-wrap">
              <table class="timeline-table">
                <thead><tr><th>{{ t('admin.traffic.colUser') }}</th><th>{{ dimension === 'group' ? t('admin.traffic.colCategory') : t('admin.traffic.colFrameType') }}</th><th>{{ t('admin.traffic.total') }}</th><th v-for="day in range.days" :key="day">{{ shortDay(day) }}</th></tr></thead>
                <tbody><tr v-for="series in chartSeries.slice(0, 16)" :key="series.key"><td>{{ series.userLabel || t('admin.traffic.selectedAggregate') }}</td><td>{{ dimension === 'group' ? categoryName(series.label) : series.label }}</td><td>{{ formatMetric(series.total) }}</td><td v-for="(value, index) in series.values" :key="index">{{ formatMetric(value) }}</td></tr></tbody>
              </table>
            </div>
          </section>

          <div class="lower-grid">
            <section class="flat-panel category-panel">
              <div class="panel-head"><div><h3>{{ t('admin.traffic.categorySummary') }}</h3><p>{{ t('admin.traffic.totalComposition') }}</p></div><button v-if="categoryFilter !== 'all'" type="button" @click="categoryFilter = 'all'">{{ t('admin.traffic.clear') }}</button></div>
              <div class="category-list">
                <button v-for="category in categories" :key="category.key" type="button" :class="{ active: categoryFilter === category.key }" @click="categoryFilter = categoryFilter === category.key ? 'all' : category.key">
                  <span class="category-name"><i :style="{ background: category.color }"></i><strong>{{ categoryName(category.key) }}</strong><em>{{ category.share.toFixed(1) }}%</em></span>
                  <span class="meter"><i :style="{ width: `${category.share}%`, background: category.color }"></i></span>
                  <span class="category-value"><strong>{{ humanBytes(category.bytes) }}</strong><small>{{ t('admin.traffic.outIn', { out: humanBytes(category.outbound), in: humanBytes(category.inbound) }) }}</small></span>
                </button>
              </div>
            </section>

            <section class="flat-panel user-table-panel">
              <div class="panel-head table-panel-head">
                <div><h3>{{ t('admin.traffic.userDetail') }}</h3><p>{{ t('admin.traffic.userCount', { count: userRows.length }) }}</p></div>
                <n-select
                  v-model:value="userTableUserID"
                  class="user-detail-select"
                  data-test="user-detail-filter"
                  size="small"
                  filterable
                  clearable
                  :options="userTableOptions"
                  :placeholder="t('admin.traffic.selectOrSearchUser')"
                >
                  <template #empty>{{ t('admin.traffic.noUserMatch') }}</template>
                </n-select>
              </div>
              <div class="data-table-wrap">
                <table class="data-table" data-test="traffic-user-table">
                  <thead><tr><th>{{ t('admin.traffic.colUser') }}</th><th>{{ t('admin.traffic.totalTraffic') }}</th><th>{{ t('admin.traffic.share') }}</th><th>{{ t('admin.traffic.periodTrend') }}</th><th>{{ t('admin.traffic.activeDays') }}</th><th></th></tr></thead>
                  <tbody><tr v-for="user in userRows" :key="user.id"><td><strong>{{ user.email }}</strong><small>{{ user.id }}</small></td><td>{{ humanBytes(user.bytes) }}</td><td><span class="share"><i :style="{ width: `${user.share}%` }"></i></span>{{ user.share.toFixed(1) }}%</td><td :class="{ positive: user.trend !== null && user.trend > 0, negative: user.trend !== null && user.trend < 0 }">{{ trendText(user.trend) }}</td><td>{{ user.activeDays }} / {{ rangeDays }}</td><td><button class="icon-button small" type="button" :aria-label="t('admin.traffic.viewUser', { user: user.email })" :data-test="`open-user-${user.id}`" @click="openDrawer(user.id)"><Eye /></button></td></tr></tbody>
                </table>
              </div>
            </section>
          </div>

          <section class="diagnostics">
            <div class="panel-head"><div><h3>{{ t('admin.traffic.diagnostics') }}</h3><p>{{ t('admin.traffic.diagnosticsRange', { days: rangeDays }) }}</p></div></div>
            <div class="diagnostic-grid">
              <div class="diagnostic-block">
                <h4>{{ t('admin.traffic.dailyActivity') }}</h4>
                <div class="activity-grid" :style="{ '--columns': String(range.days.length) }">
                  <template v-for="item in activityRows" :key="item.user.id"><span :title="item.user.email">{{ item.user.email }}</span><div><i v-for="(value, index) in item.values" :key="index" :style="{ background: activityColor(value) }" :title="`${range.days[index]} · ${humanBytes(value)}`"></i></div></template>
                </div>
              </div>
              <div class="diagnostic-block">
                <h4>{{ t('admin.traffic.frameDensity') }}</h4>
                <div class="density-list"><div v-for="item in categoryDensity" :key="item.key"><span>{{ categoryName(item.key) }}</span><div><i :style="{ width: `${(item.density / densityMax) * 100}%`, background: item.color }"></i></div><strong>{{ humanBytes(item.density) }}/{{ t('admin.traffic.frameUnit') }}</strong></div></div>
              </div>
            </div>
          </section>
        </template>
      </section>
    </n-spin>

    <div v-if="drawerUser" class="drawer-backdrop" data-test="traffic-user-drawer" @click.self="drawerUserID = null">
      <aside class="user-drawer">
        <div class="drawer-head"><div><span>{{ t('admin.traffic.userTraffic') }}</span><h3>{{ drawerUser.email }}</h3><small>{{ drawerUser.id }}</small></div><button class="icon-button" type="button" :aria-label="t('common.close')" @click="drawerUserID = null"><X /></button></div>
        <div class="drawer-kpis"><div><span>{{ t('admin.traffic.totalTraffic') }}</span><strong>{{ humanBytes(drawerUser.bytes) }}</strong></div><div><span>{{ t('admin.traffic.periodTrend') }}</span><strong>{{ trendText(drawerUser.trend) }}</strong></div><div><span>{{ t('admin.traffic.kpiFrames') }}</span><strong>{{ compactNumber(drawerUser.frames) }}</strong></div></div>
        <section><h4><Clock3 />{{ t('admin.traffic.dailyTraffic') }}</h4><div class="drawer-bars"><div v-for="(value, index) in drawerDays" :key="range.days[index]"><i :style="{ height: `${(value / drawerDayMax) * 100}%` }" :title="`${range.days[index]} · ${humanBytes(value)}`"></i><span>{{ shortDay(range.days[index]) }}</span></div></div></section>
        <section><h4><Activity />{{ t('admin.traffic.frameBreakdown') }}</h4><table><thead><tr><th>{{ t('admin.traffic.colFrameType') }}</th><th>{{ t('admin.traffic.colCategory') }}</th><th>{{ t('admin.traffic.colBytes') }}</th><th>{{ t('admin.traffic.colFrames') }}</th></tr></thead><tbody><tr v-for="frame in drawerFrames" :key="frame.name"><td>{{ frame.name }}</td><td>{{ categoryName(frame.category) }}</td><td>{{ humanBytes(frame.bytes) }}</td><td>{{ compactNumber(frame.frames) }}</td></tr></tbody></table></section>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.traffic-dashboard { width: 100%; max-width: 1540px; margin: 0 auto; color: var(--fg); }
button, input { font: inherit; letter-spacing: 0; }
button { color: inherit; }
.traffic-titlebar { display: flex; align-items: flex-start; gap: 16px; margin-bottom: 14px; }
.traffic-titlebar h1 { margin: 0 0 4px; font-size: 18px; font-weight: 650; }
.traffic-titlebar p, .panel-head p { margin: 0; color: var(--fg-dim); font-size: 11px; }
.traffic-titlebar > button { margin-left: auto; }
.icon-button { width: 32px; height: 32px; padding: 0; border: 1px solid var(--border); border-radius: 6px; background: var(--panel); display: inline-grid; place-items: center; cursor: pointer; }
.icon-button:hover { border-color: var(--fg-dim); background: color-mix(in srgb, var(--panel) 88%, white); }
.icon-button svg { width: 15px; height: 15px; }
.icon-button.small { width: 27px; height: 27px; }
.spinning { animation: spin 800ms linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.scope-section { min-width: 0; }
.scope-heading, .user-heading { display: flex; align-items: flex-end; gap: 18px; min-height: 54px; padding: 0 2px 9px; }
.scope-heading > div > span, .user-heading > div > span, .panel-head > div > span { color: var(--fg-dim); font: 700 9px ui-monospace, Menlo, monospace; text-transform: uppercase; }
.scope-heading h2, .user-heading h2 { margin: 2px 0 0; font-size: 14px; }
.scope-heading > small { margin-left: auto; color: var(--good); display: flex; align-items: center; gap: 5px; font: 10px ui-monospace, Menlo, monospace; }
.scope-heading > small svg { width: 13px; }
.platform-strip, .kpi-strip { border: 1px solid var(--border); border-radius: 8px; background: var(--panel); display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); overflow: hidden; }
.platform-strip + .platform-strip { margin-top: 8px; }
.platform-cell, .kpi-cell { min-width: 0; min-height: 78px; padding: 12px 15px; border-left: 1px solid var(--border); display: flex; flex-direction: column; justify-content: space-between; }
.platform-cell:first-child, .kpi-cell:first-child { border-left: 0; }
.platform-cell > span, .kpi-cell > span { color: var(--fg-dim); display: flex; align-items: center; gap: 6px; font-size: 10px; }
.platform-cell svg { width: 13px; height: 13px; }
.platform-cell strong { font: 650 20px ui-monospace, Menlo, monospace; }
.platform-cell small { color: var(--fg-dim); font-size: 9px; }
.platform-health { background: color-mix(in srgb, var(--good) 5%, transparent); }
.platform-health strong { color: var(--good); }
.user-section { margin-top: 22px; padding-top: 4px; border-top: 1px solid var(--border); }
.user-heading { align-items: center; padding-top: 10px; }
.user-heading > div:first-child { min-width: 210px; }
.filterbar { margin-left: auto; display: flex; align-items: center; gap: 7px; flex-wrap: wrap; justify-content: flex-end; }
.segmented { display: inline-flex; align-items: center; gap: 2px; padding: 2px; border: 1px solid var(--border); border-radius: 7px; background: color-mix(in srgb, var(--bg) 75%, transparent); }
.segmented button { height: 26px; padding: 0 9px; border: 0; border-radius: 5px; background: transparent; color: var(--fg-dim); cursor: pointer; font-size: 11px; white-space: nowrap; }
.segmented button:hover { color: var(--fg); }
.segmented button.active { color: var(--fg); background: color-mix(in srgb, var(--panel) 82%, white); }
.user-selector { position: relative; }
.selector-trigger { width: min(260px, 42vw); height: 32px; padding: 0 9px; border: 1px solid var(--border); border-radius: 6px; background: var(--panel); display: flex; align-items: center; gap: 7px; cursor: pointer; }
.selector-trigger.active, .selector-trigger:hover { border-color: var(--fg-dim); }
.selector-trigger svg { width: 14px; flex: 0 0 auto; }
.selector-trigger span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-align: left; }
.selector-popover { position: absolute; z-index: 20; top: calc(100% + 6px); right: 0; width: min(360px, calc(100vw - 32px)); border: 1px solid color-mix(in srgb, var(--border) 75%, white); border-radius: 7px; background: var(--bg); box-shadow: 0 18px 46px #0009; overflow: hidden; }
.selector-head, .selector-foot { display: flex; align-items: center; gap: 10px; padding: 11px 12px; border-bottom: 1px solid var(--border); }
.selector-head > div { flex: 1; min-width: 0; }
.selector-head strong, .selector-head span { display: block; }
.selector-head span { margin-top: 2px; color: var(--fg-dim); font-size: 10px; }
.selector-head button, .selector-foot button, .flat-panel .panel-head button, .selection-empty button { border: 1px solid var(--border); border-radius: 5px; background: var(--panel); padding: 5px 8px; cursor: pointer; font-size: 11px; }
.selector-head button { border: 0; color: var(--accent); background: transparent; }
.search-box { margin: 10px 12px; height: 31px; border: 1px solid var(--border); border-radius: 5px; display: flex; align-items: center; gap: 7px; padding: 0 8px; background: var(--panel); }
.search-box svg { width: 13px; color: var(--fg-dim); }
.search-box input { width: 100%; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--fg); }
.selector-options { max-height: 278px; overflow-y: auto; padding: 0 7px 7px; }
.selector-option { min-height: 49px; padding: 5px 7px; display: flex; align-items: center; gap: 8px; border-radius: 5px; }
.selector-option:hover { background: color-mix(in srgb, var(--panel) 70%, transparent); }
.selector-option label { flex: 1; min-width: 0; display: flex; align-items: center; gap: 9px; cursor: pointer; }
.selector-option label > span { min-width: 0; }
.selector-option strong, .selector-option small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.selector-option small { color: var(--fg-dim); margin-top: 2px; }
.selector-option button { border: 0; background: transparent; color: var(--accent); cursor: pointer; font-size: 10px; }
.selector-options > p { padding: 16px; color: var(--fg-dim); text-align: center; }
.selector-foot { justify-content: flex-end; border-top: 1px solid var(--border); border-bottom: 0; }
.selector-foot .primary { background: var(--accent); color: #08111e; border-color: var(--accent); }
.selection-banner { min-height: 38px; margin-bottom: 10px; padding: 0 10px; border: 1px solid color-mix(in srgb, var(--accent) 40%, var(--border)); border-radius: 6px; background: color-mix(in srgb, var(--accent) 8%, transparent); display: flex; align-items: center; gap: 8px; }
.selection-banner > svg { width: 14px; color: var(--accent); }
.selection-banner > span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg-dim); }
.selection-banner > button { margin-left: auto; width: 25px; height: 25px; border: 0; background: transparent; cursor: pointer; }
.selection-banner > button svg { width: 14px; }
.selection-empty { min-height: 260px; border: 1px dashed var(--border); border-radius: 8px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--fg-dim); text-align: center; }
.selection-empty > svg { width: 38px; height: 38px; padding: 9px; border-radius: 50%; background: color-mix(in srgb, var(--accent) 12%, transparent); color: var(--accent); }
.selection-empty h3 { margin: 0; color: var(--fg); font-size: 14px; }
.kpi-cell > div { display: flex; align-items: flex-end; gap: 8px; }
.kpi-cell strong { font: 650 21px ui-monospace, Menlo, monospace; }
.kpi-cell small { display: inline-flex; align-items: center; gap: 2px; color: var(--fg-dim); font-size: 9px; }
.kpi-cell small svg { width: 11px; }
.kpi-cell small.up { color: var(--warn); }
.kpi-cell small.down { color: var(--good); }
.timeline-panel, .flat-panel, .diagnostics { margin-top: 14px; border: 1px solid var(--border); border-radius: 8px; background: var(--panel); overflow: hidden; }
.panel-head { min-height: 62px; padding: 12px 14px; display: flex; align-items: flex-start; gap: 14px; border-bottom: 1px solid var(--border); }
.panel-head h3 { margin: 2px 0 3px; font-size: 13px; }
.timeline-head { min-height: 82px; }
.timeline-controls { margin-left: auto; display: flex; align-items: flex-end; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.timeline-controls > div { display: flex; flex-direction: column; gap: 4px; }
.timeline-controls small { color: var(--fg-dim); font-size: 9px; }
.chart-meta { min-height: 34px; padding: 0 14px; background: color-mix(in srgb, var(--bg) 55%, transparent); display: flex; align-items: center; gap: 8px; color: var(--fg-dim); font-size: 10px; }
.chart-meta span { padding: 3px 6px; border: 1px solid color-mix(in srgb, var(--accent) 35%, transparent); border-radius: 4px; color: var(--accent); background: color-mix(in srgb, var(--accent) 8%, transparent); }
.chart-empty { min-height: 320px; display: grid; place-items: center; color: var(--fg-dim); }
.svg-chart { position: relative; height: 340px; padding: 10px 12px 0; }
.svg-chart svg { width: 100%; height: 300px; overflow: visible; }
.svg-chart text { fill: var(--fg-dim); font-size: 9px; }
.grid-lines line { stroke: var(--border); stroke-width: 1; vector-effect: non-scaling-stroke; }
.line { fill: none; stroke-width: 2; vector-effect: non-scaling-stroke; }
.line.outbound { stroke: var(--accent); }
.line.inbound { stroke: #39c5cf; }
.legend, .series-legend { display: flex; align-items: center; justify-content: center; gap: 14px; flex-wrap: wrap; padding: 6px 14px 12px; color: var(--fg-dim); font-size: 10px; }
.legend span, .series-legend span { display: flex; align-items: center; gap: 5px; }
.legend i, .series-legend i { width: 9px; height: 7px; border-radius: 2px; }
.legend i.outbound { background: var(--accent); }.legend i.inbound { background: #39c5cf; }
.matrix-chart { min-height: 330px; padding: 15px 14px 7px; overflow-x: auto; }
.matrix-grid { min-width: 720px; display: grid; grid-template-columns: 190px minmax(510px, 1fr); gap: 3px 8px; align-items: center; }
.matrix-label { min-width: 0; display: flex; gap: 5px; font-size: 9px; overflow: hidden; }
.matrix-label strong, .matrix-label span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.matrix-label strong { max-width: 105px; }.matrix-label span { color: var(--fg-dim); }
.matrix-cells, .matrix-axis { display: grid; grid-template-columns: repeat(var(--columns), minmax(12px, 1fr)); gap: 2px; }
.matrix-cells i { height: 11px; border-radius: 2px; }
.matrix-axis { min-width: 720px; margin: 7px 0 0 198px; padding-right: 8px; }
.matrix-axis span { color: var(--fg-dim); text-align: center; font-size: 8px; overflow: hidden; }
.bar-chart { height: 320px; padding: 18px 20px 0 54px; display: flex; align-items: flex-end; gap: clamp(3px, 1.1vw, 13px); border-bottom: 1px solid var(--border); background: repeating-linear-gradient(to bottom, transparent 0, transparent 63px, color-mix(in srgb, var(--border) 65%, transparent) 64px); }
.bar-column { flex: 1; min-width: 7px; height: 100%; display: flex; flex-direction: column; align-items: center; justify-content: flex-end; gap: 7px; }
.bar-stack { width: min(34px, 82%); height: 260px; display: flex; flex-direction: column-reverse; justify-content: flex-start; }
.bar-stack i { display: block; min-height: 0; }
.bar-column > span { height: 18px; color: var(--fg-dim); font-size: 8px; white-space: nowrap; }
.timeline-table-wrap { max-height: 330px; overflow: auto; border-top: 1px solid var(--border); }
table { border-collapse: collapse; width: 100%; }
.timeline-table { min-width: 1050px; font-size: 9px; }
th { color: var(--fg-dim); font-weight: 600; text-align: left; }
.timeline-table th, .timeline-table td { height: 34px; padding: 0 9px; border-right: 1px solid var(--border); border-bottom: 1px solid var(--border); white-space: nowrap; text-align: right; }
.timeline-table th:first-child, .timeline-table td:first-child, .timeline-table th:nth-child(2), .timeline-table td:nth-child(2) { text-align: left; }
.timeline-table thead { position: sticky; top: 0; background: var(--bg); z-index: 2; }
.lower-grid { display: grid; grid-template-columns: minmax(280px, .7fr) minmax(520px, 1.5fr); gap: 14px; }
.flat-panel .panel-head { min-height: 58px; }
.flat-panel .panel-head > button { margin-left: auto; }
.category-list { padding: 7px; }
.category-list > button { width: 100%; min-height: 52px; padding: 7px 8px; border: 1px solid transparent; border-radius: 5px; background: transparent; display: grid; grid-template-columns: minmax(0, 1fr) 82px; grid-template-areas: 'name value' 'meter value'; gap: 5px 9px; cursor: pointer; text-align: left; }
.category-list > button:hover { background: color-mix(in srgb, var(--bg) 45%, transparent); }
.category-list > button.active { border-color: color-mix(in srgb, var(--accent) 45%, transparent); background: color-mix(in srgb, var(--accent) 8%, transparent); }
.category-name { grid-area: name; display: flex; align-items: center; gap: 6px; min-width: 0; }
.category-name > i { width: 7px; height: 7px; border-radius: 2px; }.category-name strong { font-size: 10px; }.category-name em { margin-left: auto; color: var(--fg-dim); font: normal 9px ui-monospace, Menlo, monospace; }
.meter { grid-area: meter; height: 3px; border-radius: 2px; background: var(--border); overflow: hidden; }.meter i { display: block; height: 100%; }
.category-value { grid-area: value; display: flex; flex-direction: column; justify-content: center; text-align: right; }.category-value strong { font: 600 11px ui-monospace, Menlo, monospace; }.category-value small { margin-top: 3px; color: var(--fg-dim); font-size: 8px; }
.table-panel-head { align-items: center; }.user-detail-select { width: min(290px, 46%); margin-left: auto; }
.data-table-wrap { overflow-x: auto; }
.data-table { min-width: 720px; font-size: 10px; }
.data-table th, .data-table td { height: 42px; padding: 0 10px; border-bottom: 1px solid var(--border); white-space: nowrap; }
.data-table td:first-child strong, .data-table td:first-child small { display: block; max-width: 190px; overflow: hidden; text-overflow: ellipsis; }.data-table td:first-child small { color: var(--fg-dim); margin-top: 2px; font-size: 8px; }
.data-table td:last-child { text-align: right; }.positive { color: var(--warn); }.negative { color: var(--good); }
.share { display: inline-block; width: 55px; height: 3px; margin-right: 6px; background: var(--border); vertical-align: middle; }.share i { display: block; height: 100%; background: var(--accent); }
.diagnostics { margin-top: 14px; }
.diagnostic-grid { display: grid; grid-template-columns: 1.35fr 1fr; }
.diagnostic-block { min-width: 0; min-height: 230px; padding: 14px; border-left: 1px solid var(--border); }.diagnostic-block:first-child { border-left: 0; }
.diagnostic-block h4 { margin: 0 0 14px; font-size: 11px; }
.activity-grid { overflow-x: auto; display: grid; grid-template-columns: 120px minmax(400px, 1fr); gap: 4px 8px; align-items: center; }
.activity-grid > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg-dim); font-size: 9px; }.activity-grid > div { display: grid; grid-template-columns: repeat(var(--columns), minmax(8px, 1fr)); gap: 2px; }.activity-grid i { height: 16px; border-radius: 2px; }
.density-list > div { min-height: 38px; display: grid; grid-template-columns: 90px minmax(70px, 1fr) 84px; align-items: center; gap: 8px; font-size: 9px; }.density-list > div > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.density-list > div > div { height: 4px; background: var(--border); }.density-list i { display: block; height: 100%; }.density-list strong { color: var(--fg-dim); text-align: right; font-weight: 500; }
.drawer-backdrop { position: fixed; z-index: 100; inset: 0; background: #0008; display: flex; justify-content: flex-end; }
.user-drawer { width: min(520px, 100vw); height: 100%; overflow-y: auto; border-left: 1px solid var(--border); background: var(--bg); box-shadow: -18px 0 50px #0008; }
.drawer-head { min-height: 92px; padding: 18px; border-bottom: 1px solid var(--border); display: flex; gap: 12px; }.drawer-head > div { min-width: 0; }.drawer-head > button { margin-left: auto; }.drawer-head span, .drawer-head small { color: var(--fg-dim); font-size: 10px; }.drawer-head h3 { margin: 5px 0; font-size: 16px; overflow-wrap: anywhere; }
.drawer-kpis { display: grid; grid-template-columns: repeat(3, 1fr); border-bottom: 1px solid var(--border); }.drawer-kpis > div { min-width: 0; min-height: 74px; padding: 13px; border-left: 1px solid var(--border); }.drawer-kpis > div:first-child { border-left: 0; }.drawer-kpis span, .drawer-kpis strong { display: block; }.drawer-kpis span { color: var(--fg-dim); font-size: 9px; }.drawer-kpis strong { margin-top: 10px; font: 600 14px ui-monospace, Menlo, monospace; }
.user-drawer section { padding: 16px 18px; border-bottom: 1px solid var(--border); }.user-drawer section h4 { margin: 0 0 14px; display: flex; align-items: center; gap: 7px; font-size: 11px; }.user-drawer section h4 svg { width: 14px; color: var(--accent); }
.drawer-bars { height: 190px; display: flex; align-items: flex-end; gap: 4px; }.drawer-bars > div { flex: 1; height: 100%; display: flex; flex-direction: column; justify-content: flex-end; align-items: center; gap: 5px; }.drawer-bars i { width: min(18px, 75%); min-height: 1px; background: var(--accent); border-radius: 2px 2px 0 0; }.drawer-bars span { color: var(--fg-dim); font-size: 7px; white-space: nowrap; }
.user-drawer table { font-size: 9px; }.user-drawer th, .user-drawer td { height: 34px; padding: 0 6px; border-bottom: 1px solid var(--border); text-align: right; }.user-drawer th:first-child, .user-drawer td:first-child, .user-drawer th:nth-child(2), .user-drawer td:nth-child(2) { text-align: left; }
button:focus-visible, input:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }

@media (max-width: 1100px) {
  .timeline-head { align-items: stretch; flex-direction: column; }
  .timeline-controls { margin-left: 0; justify-content: flex-start; }
  .lower-grid { grid-template-columns: 1fr; }
  .diagnostic-grid { grid-template-columns: 1fr; }.diagnostic-block { border-left: 0; border-top: 1px solid var(--border); }.diagnostic-block:first-child { border-top: 0; }
}
@media (max-width: 760px) {
  .traffic-titlebar { padding-top: 2px; }
  .scope-heading, .user-heading { align-items: flex-start; flex-direction: column; gap: 8px; }
  .scope-heading > small, .filterbar { margin-left: 0; }
  .filterbar { width: 100%; justify-content: flex-start; }
  .selector-trigger { width: min(100%, 310px); }
  .platform-strip, .kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .platform-cell:nth-child(3), .kpi-cell:nth-child(3) { border-left: 0; border-top: 1px solid var(--border); }.platform-cell:nth-child(4), .kpi-cell:nth-child(4) { border-top: 1px solid var(--border); }
  .timeline-controls { width: 100%; }.timeline-controls > div { width: 100%; }.timeline-controls .segmented { width: 100%; overflow-x: auto; }.timeline-controls .segmented button { flex: 1; }
  .matrix-grid { grid-template-columns: 135px minmax(500px, 1fr); }.matrix-axis { margin-left: 143px; }
  .bar-chart { padding-left: 15px; overflow-x: auto; }.bar-column { min-width: 18px; }
  .table-panel-head { align-items: flex-start; flex-direction: column; }.user-detail-select { width: 100%; margin-left: 0; }
  .drawer-kpis { grid-template-columns: 1fr; }.drawer-kpis > div { min-height: 58px; border-left: 0; border-top: 1px solid var(--border); }.drawer-kpis > div:first-child { border-top: 0; }
}
@media (prefers-reduced-motion: reduce) { .spinning { animation: none; } }
</style>
