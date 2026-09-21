import { beforeEach, describe, expect, test, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { NMessageProvider, NSelect } from 'naive-ui'
import { h } from 'vue'
import type { AdminTrafficRow } from '@shared/api/types'
import { trafficRange } from './trafficModel'

vi.mock('@shared/api/admin', () => ({
  getTrafficStats: vi.fn(),
  getAdminHealth: vi.fn(),
}))

import Traffic from './Traffic.vue'
import { getAdminHealth, getTrafficStats } from '@shared/api/admin'

const getTrafficStatsMock = getTrafficStats as unknown as ReturnType<typeof vi.fn>
const getAdminHealthMock = getAdminHealth as unknown as ReturnType<typeof vi.fn>

function mountTraffic() {
  return mount({
    render: () => h(NMessageProvider, null, { default: () => h(Traffic) }),
  })
}

function currentRows(): AdminTrafficRow[] {
  const range = trafficRange(7)
  return [
    { user_id: 'u1', email: 'alice@example.com', day: range.days[0], frame_type: 3, frame_type_name: 'OUT', category: 'terminal', direction: 1, bytes: 2048, frames: 5 },
    { user_id: 'u1', email: 'alice@example.com', day: range.previousDays[0], frame_type: 3, frame_type_name: 'OUT', category: 'terminal', direction: 1, bytes: 1024, frames: 3 },
    { user_id: 'u2', email: 'bob@example.com', day: range.days[1], frame_type: 5, frame_type_name: 'META', category: 'state', direction: 1, bytes: 1024, frames: 8 },
  ]
}

describe('Traffic', () => {
  beforeEach(() => {
    getTrafficStatsMock.mockReset()
    getAdminHealthMock.mockReset()
    getTrafficStatsMock.mockResolvedValue({ view: 'detail', bucket: 'day', from: '', to: '', rows: currentRows() })
    getAdminHealthMock.mockResolvedValue({
      version: 'test',
      uptime_seconds: 10,
      https: true,
      active_uplinks: 2,
      active_sessions: 3,
      relay_instances: 1,
      traffic_flush_interval_seconds: 60,
      generated_at: new Date().toISOString(),
    })
  })

  test('loads one daily detail dataset and renders platform and user scopes', async () => {
    const wrapper = mountTraffic()
    await flushPromises()

    expect(getTrafficStatsMock).toHaveBeenCalledTimes(1)
    expect(getTrafficStatsMock).toHaveBeenCalledWith(expect.objectContaining({ view: 'detail', bucket: 'day' }))
    expect(wrapper.find('[data-test="platform-status"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="matrix-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="traffic-user-table"]').exists()).toBe(true)
  })

  test('supports clearing and restoring the complete user selection', async () => {
    const wrapper = mountTraffic()
    await flushPromises()

    await wrapper.find('[data-test="user-filter-trigger"]').trigger('click')
    await wrapper.find('[data-test="toggle-all-users"]').trigger('click')
    expect(wrapper.find('[data-test="traffic-selection-empty"]').exists()).toBe(true)

    await wrapper.find('[data-test="restore-users-empty"]').trigger('click')
    expect(wrapper.find('[data-test="traffic-selection-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="matrix-chart"]').exists()).toBe(true)
  })

  test('only-user selection changes the chart without another request', async () => {
    const wrapper = mountTraffic()
    await flushPromises()
    getTrafficStatsMock.mockClear()

    await wrapper.find('[data-test="user-filter-trigger"]').trigger('click')
    await wrapper.find('[data-test="only-u1"]').trigger('click')

    expect(getTrafficStatsMock).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="stacked-chart"]').exists()).toBe(true)
  })

  test('switches timeline views and metrics without redundant refetching', async () => {
    const wrapper = mountTraffic()
    await flushPromises()
    getTrafficStatsMock.mockClear()

    await wrapper.find('[data-test="view-frame"]').trigger('click')
    await wrapper.find('[data-test="view-direction"]').trigger('click')
    await wrapper.find('[data-test="metric-frames"]').trigger('click')

    expect(wrapper.find('[data-test="direction-chart"]').exists()).toBe(true)
    expect(getTrafficStatsMock).not.toHaveBeenCalled()
  })

  test('refetches when the date range changes', async () => {
    const wrapper = mountTraffic()
    await flushPromises()
    getTrafficStatsMock.mockClear()

    await wrapper.find('[data-test="range-30"]').trigger('click')
    await flushPromises()

    expect(getTrafficStatsMock).toHaveBeenCalledTimes(1)
    expect(getTrafficStatsMock).toHaveBeenCalledWith(expect.objectContaining({ view: 'detail', bucket: 'day' }))
  })

  test('opens a real user drilldown drawer', async () => {
    const wrapper = mountTraffic()
    await flushPromises()

    await wrapper.find('[data-test="open-user-u1"]').trigger('click')
    expect(wrapper.find('[data-test="traffic-user-drawer"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="traffic-user-drawer"]').text()).toContain('alice@example.com')
  })

  test('filters the user detail table with a searchable dropdown', async () => {
    const wrapper = mountTraffic()
    await flushPromises()

    const select = wrapper.findComponent(NSelect)
    expect(select.attributes('data-test')).toBe('user-detail-filter')
    expect(select.props('filterable')).toBe(true)
    expect(select.props('clearable')).toBe(true)

    select.vm.$emit('update:value', 'u1')
    await flushPromises()
    expect(wrapper.get('[data-test="traffic-user-table"]').text()).toContain('alice@example.com')
    expect(wrapper.get('[data-test="traffic-user-table"]').text()).not.toContain('bob@example.com')

    select.vm.$emit('update:value', null)
    await flushPromises()
    expect(wrapper.get('[data-test="traffic-user-table"]').text()).toContain('bob@example.com')
  })
})
