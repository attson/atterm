import { describe, expect, test } from 'vitest'
import type { AdminTrafficRow } from '@shared/api/types'
import { categorySummaries, directionSeries, timelineSeries, trafficRange, userSummaries } from './trafficModel'

const rows: AdminTrafficRow[] = [
  { user_id: 'u1', email: 'a@example.com', day: '2026-09-20', frame_type: 3, frame_type_name: 'OUT', category: 'terminal', direction: 1, bytes: 100, frames: 10 },
  { user_id: 'u1', email: 'a@example.com', day: '2026-09-21', frame_type: 2, frame_type_name: 'IN', category: 'terminal', direction: 0, bytes: 50, frames: 5 },
  { user_id: 'u1', email: 'a@example.com', day: '2026-09-19', frame_type: 3, frame_type_name: 'OUT', category: 'terminal', direction: 1, bytes: 75, frames: 7 },
  { user_id: 'u2', email: 'b@example.com', day: '2026-09-21', frame_type: 5, frame_type_name: 'META', category: 'state', direction: 1, bytes: 50, frames: 20 },
]

describe('trafficModel', () => {
  test('builds equal current and previous UTC ranges', () => {
    const range = trafficRange(2, new Date('2026-09-21T12:00:00Z'))
    expect(range).toEqual({
      from: '2026-09-18',
      currentFrom: '2026-09-20',
      to: '2026-09-21',
      days: ['2026-09-20', '2026-09-21'],
      previousDays: ['2026-09-18', '2026-09-19'],
    })
  })

  test('derives user share and real period-over-period trend', () => {
    const range = trafficRange(2, new Date('2026-09-21T12:00:00Z'))
    const users = userSummaries(rows, new Set(['u1', 'u2']), range)
    expect(users[0]).toMatchObject({ id: 'u1', bytes: 150, previousBytes: 75, trend: 100, share: 75 })
    expect(users[1]).toMatchObject({ id: 'u2', bytes: 50, trend: null, share: 25 })
  })

  test('aggregates category, timeline and direction from the same rows', () => {
    const current = rows.filter((row) => row.day !== '2026-09-19')
    expect(categorySummaries(current).map((item) => [item.key, item.bytes])).toEqual([
      ['terminal', 150],
      ['state', 50],
    ])
    expect(timelineSeries(current, ['2026-09-20', '2026-09-21'], 'group', 'all', 'bytes', false)[0].values).toEqual([100, 50])
    expect(directionSeries(current, ['2026-09-20', '2026-09-21'], 'bytes')).toEqual({ inbound: [0, 50], outbound: [100, 50] })
  })

  test('assigns distinct colors to current relay categories', () => {
    const categories = ['config', 'fs', 'preview'].map((category, index) => ({
      user_id: 'u1',
      day: '2026-09-21',
      category,
      direction: index % 2,
      bytes: 10,
      frames: 1,
    })) satisfies AdminTrafficRow[]

    const summaries = categorySummaries(categories)
    expect(new Set(summaries.map((item) => item.color)).size).toBe(3)
    expect(summaries.every((item) => item.color !== '#8b949e')).toBe(true)
  })
})
