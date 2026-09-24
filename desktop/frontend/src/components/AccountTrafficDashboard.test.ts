import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../i18n/useI18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => params
      ? `${key}[${Object.entries(params).map(([name, value]) => `${name}=${value}`).join(',')}]`
      : key,
  }),
}))

vi.mock('@shared/api/me', () => ({ getMeTraffic: vi.fn() }))

import { getMeTraffic } from '@shared/api/me'
import AccountTrafficDashboard from './AccountTrafficDashboard.vue'

const getMeTrafficMock = getMeTraffic as unknown as ReturnType<typeof vi.fn>

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-09-23T12:00:00Z'))
  getMeTrafficMock.mockReset()
  getMeTrafficMock.mockResolvedValue({
    from: '2026-09-10',
    to: '2026-09-23',
    relay: [{ day: '2026-09-23', bytes_in: 1024, bytes_out: 2048, frames_in: 2, frames_out: 3 }],
    relay_detail: [
      { day: '2026-09-23', frame_type: 3, frame_type_name: 'OUT', category: 'terminal', direction: 1, bytes: 2048, frames: 3 },
      { day: '2026-09-23', frame_type: 5, frame_type_name: 'META', category: 'state', direction: 0, bytes: 1024, frames: 2 },
    ],
    direct: [{ day: '2026-09-23', attempts: 4, successes: 3, fallbacks: 1, bytes_sent: 4096, bytes_received: 512 }],
  })
})

describe('AccountTrafficDashboard', () => {
  it('loads and renders Relay and P2P account totals', async () => {
    const wrapper = mount(AccountTrafficDashboard)
    await flushPromises()

    expect(getMeTrafficMock).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="relay-traffic-total"]').text()).toContain('3.00 KB')
    expect(wrapper.get('[data-testid="relay-kpi-frames"]').text()).toContain('5')
    expect(wrapper.get('[data-testid="direct-traffic-total"]').text()).toContain('4.50 KB')
    expect(wrapper.get('[data-testid="direct-success-rate"]').text()).toContain('75.0%')
  })

  it('switches between category, frame, and direction analysis', async () => {
    const wrapper = mount(AccountTrafficDashboard)
    await flushPromises()

    expect(wrapper.find('[data-testid="traffic-stacked-chart"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('OUT')

    await wrapper.get('[data-testid="traffic-view-frame"]').trigger('click')
    expect(wrapper.find('[data-testid="traffic-stacked-chart"]').exists()).toBe(true)

    await wrapper.get('[data-testid="traffic-metric-frames"]').trigger('click')
    await wrapper.get('[data-testid="traffic-view-direction"]').trigger('click')
    expect(wrapper.find('[data-testid="traffic-direction-chart"]').exists()).toBe(true)
  })

  it('reloads when the range changes', async () => {
    const wrapper = mount(AccountTrafficDashboard)
    await flushPromises()
    await wrapper.get('[data-testid="traffic-range-30"]').trigger('click')
    await flushPromises()

    expect(getMeTrafficMock).toHaveBeenCalledTimes(2)
    const [from, to] = getMeTrafficMock.mock.calls[1]
    const elapsedDays = (Date.parse(to) - Date.parse(from)) / 86_400_000 + 1
    expect(elapsedDays).toBe(60)
  })

  it('shows a retry state after a failed request', async () => {
    getMeTrafficMock.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(AccountTrafficDashboard)
    await flushPromises()

    expect(wrapper.text()).toContain('settings.account.traffic.loadFailed')
  })
})
