import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach, type MockInstance } from 'vitest'
import { __setPlatformForTests, usePlatform } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({
    t: (k: string, params?: Record<string, unknown>) => {
      if (!params) return k
      const pairs = Object.entries(params).map(([kk, vv]) => `${kk}=${vv}`).join(',')
      return `${k}[${pairs}]`
    },
  }),
}))

import SettingsDevices from '../SettingsDevices.vue'
import type { RelaySessionRow } from '../../lib/api'

function baseRows(): RelaySessionRow[] {
  return [
    { id_hash: 'h-me',    user_agent: 'UA-A', ip_prefix: '1.2.3', created_at: 1_700_000_000_000, expires_at: 1_710_000_000_000, is_current: true },
    { id_hash: 'h-other', user_agent: 'UA-B', ip_prefix: '4.5.6', created_at: 1_700_100_000_000, expires_at: 1_710_100_000_000, is_current: false },
  ]
}

beforeEach(() => {
  const platform = createFakePlatform()
  ;(platform.sessions as any).listRelaySessions = vi.fn().mockResolvedValue(baseRows())
  ;(platform.sessions as any).revokeRelaySession = vi.fn().mockResolvedValue(undefined)
  ;(platform.sessions as any).signOutOtherRelaySessions = vi.fn().mockResolvedValue({ deleted: 2 })
  __setPlatformForTests(platform)
})

afterEach(() => {
  vi.restoreAllMocks()
  __setPlatformForTests(null)
})

describe('SettingsDevices', () => {
  let confirmSpy: MockInstance<[message?: string], boolean>

  beforeEach(() => {
    confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  afterEach(() => {
    confirmSpy.mockRestore()
  })

  it('renders one row per session; current row shows Current tag; others show Revoke', async () => {
    const w = mount(SettingsDevices)
    await flushPromises()
    const rows = w.findAll('.device-row')
    expect(rows.length).toBe(2)
    // Row 0 is current: has .current-tag, no .danger-btn.
    expect(rows[0].find('.current-tag').exists()).toBe(true)
    expect(rows[0].find('.danger-btn').exists()).toBe(false)
    // Row 1 is non-current: opposite.
    expect(rows[1].find('.current-tag').exists()).toBe(false)
    expect(rows[1].find('.danger-btn').exists()).toBe(true)
  })

  it('clicking refresh calls listRelaySessions again', async () => {
    const platform = usePlatform()
    const listSpy = (platform.sessions as any).listRelaySessions as ReturnType<typeof vi.fn>
    const w = mount(SettingsDevices)
    await flushPromises()
    expect(listSpy).toHaveBeenCalledTimes(1)
    await w.find('.icon-btn').trigger('click')
    await flushPromises()
    expect(listSpy).toHaveBeenCalledTimes(2)
  })

  it('revoke confirm=true calls revokeRelaySession then reloads', async () => {
    const platform = usePlatform()
    const revokeSpy = (platform.sessions as any).revokeRelaySession as ReturnType<typeof vi.fn>
    const listSpy = (platform.sessions as any).listRelaySessions as ReturnType<typeof vi.fn>
    const w = mount(SettingsDevices)
    await flushPromises()
    const revokeBtn = w.findAll('.danger-btn')[0]
    await revokeBtn.trigger('click')
    await flushPromises()
    expect(revokeSpy).toHaveBeenCalledWith('h-other')
    expect(listSpy).toHaveBeenCalledTimes(2) // initial + post-revoke
  })

  it('revoke confirm=false does not call revokeRelaySession', async () => {
    confirmSpy.mockReturnValue(false)
    const platform = usePlatform()
    const revokeSpy = (platform.sessions as any).revokeRelaySession as ReturnType<typeof vi.fn>
    const w = mount(SettingsDevices)
    await flushPromises()
    await w.findAll('.danger-btn')[0].trigger('click')
    await flushPromises()
    expect(revokeSpy).not.toHaveBeenCalled()
  })

  it('sign-out-others confirm=true calls API then reloads', async () => {
    const platform = usePlatform()
    const signSpy = (platform.sessions as any).signOutOtherRelaySessions as ReturnType<typeof vi.fn>
    const listSpy = (platform.sessions as any).listRelaySessions as ReturnType<typeof vi.fn>
    const w = mount(SettingsDevices)
    await flushPromises()
    const btns = w.findAll('button.secondary')
    const signOut = btns.find((b) => b.text().includes('settings.devices.signOutOthers'))!
    await signOut.trigger('click')
    await flushPromises()
    expect(signSpy).toHaveBeenCalledTimes(1)
    expect(listSpy).toHaveBeenCalledTimes(2)
  })

  it('not-authenticated error switches to hint copy; hides header actions', async () => {
    const platform = usePlatform()
    const listSpy = (platform.sessions as any).listRelaySessions as ReturnType<typeof vi.fn>
    listSpy.mockReset()
    listSpy.mockRejectedValue(new Error('not authenticated'))
    const w = mount(SettingsDevices)
    await flushPromises()
    expect(w.text()).toContain('settings.devices.notAuthenticated')
    expect(w.find('.secondary').exists()).toBe(false)
    expect(w.find('.icon-btn').exists()).toBe(false)
  })
})
