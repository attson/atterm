import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { bucketRange, useTrafficWindow } from './useTrafficWindow'

afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('traffic time windows', () => {
  it('uses UTC buckets across midnight with an equal previous comparison window', () => {
    const range = bucketRange(Date.parse('2026-10-19T23:59:00Z'), 3, 60)
    expect(range.days).toEqual(['2026-10-19T23:59:00Z', '2026-10-20T00:00:00Z', '2026-10-20T00:01:00Z'])
    expect(range.previousDays).toHaveLength(3)
    expect(range.from).toBe('2026-10-19T23:56:00Z')
    expect(range.to).toBe('2026-10-20T00:02:00Z')
  })

  it('polls live views, pauses while hidden, and stops on unmount', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date('2026-10-19T12:02:30Z'))
    const load = vi.fn().mockResolvedValue(undefined)
    let window!: ReturnType<typeof useTrafficWindow>
    const wrapper = mount(defineComponent({ setup() { window = useTrafficWindow(load); return () => h('div') } }))
    await vi.advanceTimersByTimeAsync(10000)
    expect(load).not.toHaveBeenCalled()
    window.mode.value = 'realtime'
    await flushPromises()
    expect(window.range.value.days).toHaveLength(60)
    expect(window.bucket.value).toBe('minute')
    expect(load).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(10000)
    expect(load).toHaveBeenCalledTimes(2)
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    await vi.advanceTimersByTimeAsync(10000)
    expect(load).toHaveBeenCalledTimes(2)
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(load).toHaveBeenCalledTimes(3)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(20000)
    expect(load).toHaveBeenCalledTimes(3)
  })

  it('loads changed filters during an in-flight poll and drills into one hour', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-19T12:02:30Z'))
    const load = vi.fn().mockImplementation(() => new Promise<void>(() => {}))
    let window!: ReturnType<typeof useTrafficWindow>
    const wrapper = mount(defineComponent({ setup() { window = useTrafficWindow(load); return () => h('div') } }))
    window.mode.value = 'realtime'
    await nextTick()
    window.minutes.value = 15
    await nextTick()
    expect(load).toHaveBeenCalledTimes(2)
    expect(window.range.value.days).toHaveLength(15)
    window.mode.value = 'summary'
    window.drillDown('2026-10-18')
    await nextTick()
    expect(window.mode.value).toBe('intraday')
    expect(window.range.value.days).toHaveLength(24)
    window.drillDown('2026-10-18T03:00:00Z')
    await nextTick()
    expect(window.bucket.value).toBe('minute')
    expect(window.range.value.days).toHaveLength(60)
    expect(window.range.value.currentFrom).toBe('2026-10-18T03:00:00Z')
    wrapper.unmount()
  })
})
