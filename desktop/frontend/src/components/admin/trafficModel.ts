import type { AdminTrafficRow } from '@shared/api/types'

export type TrafficMetric = 'bytes' | 'frames'
export type TimelineDimension = 'group' | 'frame' | 'direction'
export type DirectionFilter = 'all' | 'out' | 'in'

export interface TrafficRange {
  from: string
  currentFrom: string
  to: string
  days: string[]
  previousDays: string[]
}

export interface TrafficUserSummary {
  id: string
  email: string
  bytes: number
  frames: number
  outbound: number
  inbound: number
  previousBytes: number
  trend: number | null
  share: number
  activeDays: number
}

export interface TrafficSeries {
  key: string
  label: string
  userID?: string
  userLabel?: string
  category?: string
  color: string
  values: number[]
  total: number
}

export interface CategorySummary {
  key: string
  label: string
  color: string
  bytes: number
  frames: number
  outbound: number
  inbound: number
  share: number
}

const CATEGORY_COLORS: Record<string, string> = {
  terminal: '#58a6ff',
  state: '#39c5cf',
  config: '#d29922',
  fs: '#bc8cff',
  preview: '#db6d28',
  // Older relays used these category names. Keep their colors stable during
  // rolling upgrades while current relays emit config/fs/preview.
  filesystem: '#bc8cff',
  service: '#db6d28',
  control: '#d29922',
  other: '#8b949e',
}

const SERIES_COLORS = ['#58a6ff', '#39c5cf', '#bc8cff', '#db6d28', '#d29922', '#3fb950', '#f85149', '#79c0ff']

function dateAtUTC(day: string): Date {
  return new Date(`${day}T00:00:00Z`)
}

function dayString(date: Date): string {
  return date.toISOString().slice(0, 10)
}

function addDays(day: string, amount: number): string {
  const date = dateAtUTC(day)
  date.setUTCDate(date.getUTCDate() + amount)
  return dayString(date)
}

export function inclusiveDays(from: string, to: string): string[] {
  const result: string[] = []
  for (let day = from; day <= to; day = addDays(day, 1)) result.push(day)
  return result
}

export function trafficRange(days: number, now = new Date()): TrafficRange {
  const to = dayString(now)
  const currentFrom = addDays(to, -(days - 1))
  const from = addDays(currentFrom, -days)
  return {
    from,
    currentFrom,
    to,
    days: inclusiveDays(currentFrom, to),
    previousDays: inclusiveDays(from, addDays(currentFrom, -1)),
  }
}

export function userIdentity(rows: AdminTrafficRow[]): { id: string; email: string }[] {
  const users = new Map<string, string>()
  for (const row of rows) users.set(row.user_id, row.email || row.user_id)
  return [...users].map(([id, email]) => ({ id, email })).sort((a, b) => a.email.localeCompare(b.email))
}

export function selectedRows(rows: AdminTrafficRow[], userIDs: Set<string>, days: Set<string>): AdminTrafficRow[] {
  return rows.filter((row) => userIDs.has(row.user_id) && !!row.day && days.has(row.day))
}

function metricValue(row: AdminTrafficRow, metric: TrafficMetric): number {
  return metric === 'bytes' ? row.bytes : row.frames
}

export function totalMetric(rows: AdminTrafficRow[], metric: TrafficMetric): number {
  return rows.reduce((sum, row) => sum + metricValue(row, metric), 0)
}

export function trendPercent(current: number, previous: number): number | null {
  if (previous === 0) return current === 0 ? 0 : null
  return ((current - previous) / previous) * 100
}

export function userSummaries(
  rows: AdminTrafficRow[],
  userIDs: Set<string>,
  range: TrafficRange,
): TrafficUserSummary[] {
  const currentDays = new Set(range.days)
  const previousDays = new Set(range.previousDays)
  const users = userIdentity(rows).filter((user) => userIDs.has(user.id))
  const totals = users.map((user) => {
    const own = rows.filter((row) => row.user_id === user.id)
    const current = own.filter((row) => !!row.day && currentDays.has(row.day))
    const previous = own.filter((row) => !!row.day && previousDays.has(row.day))
    const bytes = totalMetric(current, 'bytes')
    const previousBytes = totalMetric(previous, 'bytes')
    return {
      id: user.id,
      email: user.email,
      bytes,
      frames: totalMetric(current, 'frames'),
      outbound: totalMetric(current.filter((row) => row.direction === 1), 'bytes'),
      inbound: totalMetric(current.filter((row) => row.direction === 0), 'bytes'),
      previousBytes,
      trend: trendPercent(bytes, previousBytes),
      share: 0,
      activeDays: new Set(current.map((row) => row.day)).size,
    }
  })
  const total = totals.reduce((sum, user) => sum + user.bytes, 0)
  for (const user of totals) user.share = total ? (user.bytes / total) * 100 : 0
  return totals.sort((a, b) => b.bytes - a.bytes || a.email.localeCompare(b.email))
}

export function categorySummaries(rows: AdminTrafficRow[]): CategorySummary[] {
  const groups = new Map<string, Omit<CategorySummary, 'share'>>()
  for (const row of rows) {
    const key = row.category || 'other'
    const cell = groups.get(key) || {
      key,
      label: key,
      color: CATEGORY_COLORS[key] || CATEGORY_COLORS.other,
      bytes: 0,
      frames: 0,
      outbound: 0,
      inbound: 0,
    }
    cell.bytes += row.bytes
    cell.frames += row.frames
    if (row.direction === 1) cell.outbound += row.bytes
    else cell.inbound += row.bytes
    groups.set(key, cell)
  }
  const total = [...groups.values()].reduce((sum, group) => sum + group.bytes, 0)
  return [...groups.values()]
    .map((group) => ({ ...group, share: total ? (group.bytes / total) * 100 : 0 }))
    .sort((a, b) => b.bytes - a.bytes)
}

function rowDimension(row: AdminTrafficRow, dimension: Exclude<TimelineDimension, 'direction'>): { key: string; label: string; category: string } {
  if (dimension === 'group') {
    const category = row.category || 'other'
    return { key: category, label: category, category }
  }
  const label = row.frame_type_name || String(row.frame_type ?? '?')
  return { key: String(row.frame_type ?? label), label, category: row.category || 'other' }
}

export function timelineSeries(
  rows: AdminTrafficRow[],
  dates: string[],
  dimension: Exclude<TimelineDimension, 'direction'>,
  direction: DirectionFilter,
  metric: TrafficMetric,
  separateUsers: boolean,
): TrafficSeries[] {
  const dateIndex = new Map(dates.map((day, index) => [day, index]))
  const series = new Map<string, TrafficSeries>()
  for (const row of rows) {
    const index = row.day ? dateIndex.get(row.day) : undefined
    if (index === undefined) continue
    if (direction === 'out' && row.direction !== 1) continue
    if (direction === 'in' && row.direction !== 0) continue
    const dim = rowDimension(row, dimension)
    const key = separateUsers ? `${row.user_id}:${dim.key}` : dim.key
    let cell = series.get(key)
    if (!cell) {
      cell = {
        key,
        label: dim.label,
        userID: separateUsers ? row.user_id : undefined,
        userLabel: separateUsers ? row.email || row.user_id : undefined,
        category: dim.category,
        color: dimension === 'group' ? (CATEGORY_COLORS[dim.category] || CATEGORY_COLORS.other) : SERIES_COLORS[series.size % SERIES_COLORS.length],
        values: Array(dates.length).fill(0),
        total: 0,
      }
      series.set(key, cell)
    }
    const value = metricValue(row, metric)
    cell.values[index] += value
    cell.total += value
  }
  return [...series.values()].sort((a, b) => b.total - a.total)
}

export function directionSeries(rows: AdminTrafficRow[], dates: string[], metric: TrafficMetric): { inbound: number[]; outbound: number[] } {
  const index = new Map(dates.map((day, i) => [day, i]))
  const inbound = Array(dates.length).fill(0) as number[]
  const outbound = Array(dates.length).fill(0) as number[]
  for (const row of rows) {
    const i = row.day ? index.get(row.day) : undefined
    if (i === undefined) continue
    ;(row.direction === 1 ? outbound : inbound)[i] += metricValue(row, metric)
  }
  return { inbound, outbound }
}

export function dailyTotals(rows: AdminTrafficRow[], dates: string[], metric: TrafficMetric): number[] {
  const result = Array(dates.length).fill(0) as number[]
  const index = new Map(dates.map((day, i) => [day, i]))
  for (const row of rows) {
    const i = row.day ? index.get(row.day) : undefined
    if (i !== undefined) result[i] += metricValue(row, metric)
  }
  return result
}

export function categoryLabel(key: string): string {
  return key
}
