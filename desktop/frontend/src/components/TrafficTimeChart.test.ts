import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TrafficTimeChart from './TrafficTimeChart.vue'
import { bucketRange } from '../composables/useTrafficWindow'

describe('minute traffic chart', () => {
  it('zero-fills idle minutes and excludes the open minute from rates and peaks', () => {
    const dates = bucketRange(Date.parse('2026-10-19T12:00:00Z'), 3, 60).days
    const wrapper = mount(TrafficTimeChart, { props: {
      rows: [
        { user_id: 'self', day: dates[1], category: 'terminal', direction: 1, bytes: 6000, frames: 2 },
        { user_id: 'self', day: dates[1], category: 'terminal', direction: 0, bytes: 3000, frames: 1 },
        { user_id: 'self', day: dates[2], category: 'state', direction: 1, bytes: 600000, frames: 10 },
      ], dates, seconds: 60, realtime: true, dimension: 'group', direction: 'all', metric: 'bytes', serverTime: '2026-10-19T12:02:30Z',
    } })
    const rates = wrapper.findAll('.rate-strip strong').map(node => node.text())
    expect(rates).toEqual(['100 B/s', '50 B/s', '150 B/s'])
    expect(wrapper.find('.pending').exists()).toBe(true)
    const points = wrapper.find('polyline').attributes('points')!.split(' ')
    expect(points).toHaveLength(3)
    expect(points[0]).toContain(',216')
    wrapper.unmount()
  })

  it('offers an hour-to-minute drilldown even for an idle hour', async () => {
    const dates = bucketRange(Date.parse('2026-10-19T12:00:00Z'), 1, 3600).days
    const wrapper = mount(TrafficTimeChart, { props: { rows: [], dates, seconds: 3600, realtime: false, dimension: 'group', direction: 'all', metric: 'bytes', serverTime: '2026-10-19T13:00:00Z' } })
    await wrapper.get('.tooltip button').trigger('click')
    expect(wrapper.emitted('drillDown')).toEqual([[dates[0]]])
    wrapper.unmount()
  })
})
