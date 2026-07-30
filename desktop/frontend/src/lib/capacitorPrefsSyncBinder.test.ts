import { describe, expect, it, vi } from 'vitest'
import { fakeEventBus } from '../platform/__tests__/_fakePlatform'
import { bindCapacitorPrefsSync } from './capacitorPrefsSyncBinder'

describe('bindCapacitorPrefsSync', () => {
  it('pulls on bootstrap, foreground, and auth restore', async () => {
    const events = fakeEventBus()
    const emit = vi.spyOn(events, 'emit')
    const prefsSync = { pull: vi.fn().mockResolvedValue(undefined) }
    let appStateHandler: (state: { isActive: boolean }) => void = () => {
      throw new Error('appStateChange listener was not registered')
    }
    const appStateSource = {
      addListener: vi.fn((event: string, handler: (state: { isActive: boolean }) => void) => {
        expect(event).toBe('appStateChange')
        appStateHandler = handler
      }),
    }

    bindCapacitorPrefsSync({ events }, prefsSync, appStateSource)
    await vi.waitFor(() => expect(emit).toHaveBeenCalledWith('prefs:changed', undefined))
    expect(prefsSync.pull).toHaveBeenCalledTimes(1)

    appStateHandler({ isActive: false })
    await Promise.resolve()
    expect(prefsSync.pull).toHaveBeenCalledTimes(1)

    appStateHandler({ isActive: true })
    await vi.waitFor(() => expect(prefsSync.pull).toHaveBeenCalledTimes(2))

    events.emit('relay:auth-restored', undefined)
    await vi.waitFor(() => expect(prefsSync.pull).toHaveBeenCalledTimes(3))

    events.emit('prefs:remote-changed', undefined)
    await vi.waitFor(() => expect(prefsSync.pull).toHaveBeenCalledTimes(4))
    expect(emit).toHaveBeenCalledWith('prefs:changed', undefined)
  })
})
