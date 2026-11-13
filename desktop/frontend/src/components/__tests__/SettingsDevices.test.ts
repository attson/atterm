import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
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
    { id_hash: 'h-me', user_agent: 'AT-Term-Desktop/v0.6.10 (darwin/arm64)', ip_prefix: '1.2.3', created_at: 1_700_000_000_000, expires_at: 1_710_000_000_000, last_seen_at: Date.now(), is_online: true, is_current: true },
    { id_hash: 'h-other', user_agent: 'UA-B', ip_prefix: '4.5.6', created_at: 1_700_100_000_000, expires_at: 1_710_100_000_000, last_seen_at: Date.now() - 3_600_000, is_online: false, is_current: false },
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
    expect(rows[0].find('.activity.online').exists()).toBe(true)
    expect(rows[0].text()).toContain('AT Term Desktop · macOS · v0.6.10')
    expect(w.text()).toContain('settings.devices.onlineSummary[online=1,total=2]')
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

  it('refreshes presence every 30 seconds and stops after unmount', async () => {
    vi.useFakeTimers()
    try {
      const platform = usePlatform()
      const listSpy = (platform.sessions as any).listRelaySessions as ReturnType<typeof vi.fn>
      const w = mount(SettingsDevices)
      await flushPromises()
      expect(listSpy).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(30_000)
      await flushPromises()
      expect(listSpy).toHaveBeenCalledTimes(2)
      w.unmount()
      await vi.advanceTimersByTimeAsync(30_000)
      expect(listSpy).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })

  it('revokes the selected device only after in-app confirmation, even when native confirm is unavailable', async () => {
    const platform = usePlatform()
    const revokeSpy = (platform.sessions as any).revokeRelaySession as ReturnType<typeof vi.fn>
    const listSpy = (platform.sessions as any).listRelaySessions as ReturnType<typeof vi.fn>
    const w = mount(SettingsDevices)
    await flushPromises()
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    listSpy.mockResolvedValueOnce(baseRows().slice(0, 1))
    const revokeBtn = w.findAll('.danger-btn')[0]
    await revokeBtn.trigger('click')
    expect(revokeSpy).not.toHaveBeenCalled()
    expect(w.get('[role=alertdialog]').text()).toContain('settings.devices.revokeConfirm[ua=UA-B]')
    await w.get('[data-testid=device-confirm]').trigger('click')
    await flushPromises()
    expect(w.find('[role=alertdialog]').exists()).toBe(false)
    expect(w.findAll('.device-row')).toHaveLength(1)
    expect(window.confirm).not.toHaveBeenCalled()
    expect(revokeSpy).toHaveBeenCalledWith('h-other')
    expect(listSpy).toHaveBeenCalledTimes(2) // initial + post-revoke
  })

  it.each(['cancel', 'escape', 'backdrop'])('dismissing revoke via %s keeps the device signed in', async (method) => {
    const platform = usePlatform()
    const revokeSpy = (platform.sessions as any).revokeRelaySession as ReturnType<typeof vi.fn>
    const w = mount(SettingsDevices)
    await flushPromises()
    await w.findAll('.danger-btn')[0].trigger('click')
    await flushPromises()
    if (method === 'cancel') await w.get('[data-testid=device-cancel]').trigger('click')
    else if (method === 'escape') await w.get('[role=alertdialog]').trigger('keydown', { key: 'Escape' })
    else await w.get('.confirmation-backdrop').trigger('click')
    expect(w.find('[role=alertdialog]').exists()).toBe(false)
    expect(w.findAll('.device-row')).toHaveLength(2)
    expect(revokeSpy).not.toHaveBeenCalled()
  })

  it('sign-out-others requires in-app confirmation then reloads', async () => {
    const platform = usePlatform()
    const signSpy = (platform.sessions as any).signOutOtherRelaySessions as ReturnType<typeof vi.fn>
    const listSpy = (platform.sessions as any).listRelaySessions as ReturnType<typeof vi.fn>
    const w = mount(SettingsDevices)
    await flushPromises()
    const btns = w.findAll('button.secondary')
    const signOut = btns.find((b) => b.text().includes('settings.devices.signOutOthers'))!
    await signOut.trigger('click')
    expect(signSpy).not.toHaveBeenCalled()
    expect(w.get('[role=alertdialog]').text()).toContain('settings.devices.signOutOthersConfirm')
    await w.get('[data-testid=device-confirm]').trigger('click')
    await flushPromises()
    expect(signSpy).toHaveBeenCalledTimes(1)
    expect(listSpy).toHaveBeenCalledTimes(2)
  })

  it('canceling sign-out-others leaves all devices signed in', async () => {
    const platform = usePlatform()
    const w = mount(SettingsDevices)
    await flushPromises()
    await w.get('button.secondary').trigger('click')
    await w.get('[data-testid=device-cancel]').trigger('click')
    expect(w.find('[role=alertdialog]').exists()).toBe(false)
    expect(w.findAll('.device-row')).toHaveLength(2)
    expect(platform.sessions.signOutOtherRelaySessions).not.toHaveBeenCalled()
  })

  it('focuses cancel, keeps Tab inside the confirmation, and restores focus on Escape', async () => {
    const w = mount(SettingsDevices, { attachTo: document.body })
    await flushPromises()
    const trigger = w.get<HTMLButtonElement>('.danger-btn')
    trigger.element.focus()
    await trigger.trigger('click')
    const cancel = w.get('[data-testid=device-cancel]')
    const confirm = w.get('[data-testid=device-confirm]')
    expect(document.activeElement).toBe(cancel.element)
    await cancel.trigger('keydown', { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(confirm.element)
    await confirm.trigger('keydown', { key: 'Tab' })
    expect(document.activeElement).toBe(cancel.element)
    await cancel.trigger('keydown', { key: 'Escape' })
    expect(document.activeElement).toBe(trigger.element)
  })

  it('shows a failed revoke and allows another attempt', async () => {
    const platform = usePlatform()
    vi.mocked(platform.sessions.revokeRelaySession!).mockRejectedValueOnce(new Error('relay unavailable'))
    const w = mount(SettingsDevices)
    await flushPromises()
    await w.get('.danger-btn').trigger('click')
    await w.get('[data-testid=device-confirm]').trigger('click')
    await flushPromises()
    expect(w.get('.error').text()).toBe('relay unavailable')
    await w.get('.icon-btn').trigger('click')
    await flushPromises()
    expect(w.get<HTMLButtonElement>('.danger-btn').element.disabled).toBe(false)
    await w.get('.danger-btn').trigger('click')
    expect(w.find('[role=alertdialog]').exists()).toBe(true)
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
