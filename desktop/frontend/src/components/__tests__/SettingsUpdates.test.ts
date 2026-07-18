import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach, type MockInstance } from 'vitest'
import { nextTick } from 'vue'

const { fake } = vi.hoisted(() => ({
  fake: {
    events: { on: vi.fn().mockReturnValue(() => {}), off: vi.fn(), emit: vi.fn() },
  },
}))

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({
    t: (k: string, params?: Record<string, unknown>) => {
      if (!params) return k
      const pairs = Object.entries(params).map(([kk, vv]) => `${kk}=${vv}`).join(',')
      return `${k}[${pairs}]`
    },
  }),
}))

vi.mock('../../platform', () => ({
  usePlatform: () => fake,
}))

import SettingsUpdates from '../SettingsUpdates.vue'
import * as api from '../../lib/api'

function baseState(overrides: Partial<api.UpdateState> = {}): api.UpdateState {
  return {
    current: 'v0.1.0',
    latest: 'v0.2.168',
    available: true,
    notes: '',
    checking: false,
    last_check_at: 1_700_000_000,
    downloading: false,
    download_pct: 0,
    ready: false,
    error: '',
    asset_url: 'https://example.test/asset.tar.gz',
    asset_size: 1024,
    download_dir: '/tmp',
    download_path: '/tmp/v0.2.168-file.tar.gz',
    lines: [],
    downloaded_exists: false,
    ...overrides,
  }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.spyOn(api, 'getAutoCheckUpdates').mockResolvedValue(true as never)
  vi.spyOn(api, 'getUpdateGHProxyURL').mockResolvedValue('' as never)
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('SettingsUpdates cancel + redownload', () => {
  let confirmSpy: MockInstance<[message?: string], boolean>

  beforeEach(() => {
    confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  afterEach(() => {
    confirmSpy.mockRestore()
  })

  it('renders Cancel (N%) button while downloading and clicking calls cancelDownload', async () => {
    vi.spyOn(api, 'getUpdateState').mockResolvedValue(
      baseState({ downloading: true, download_pct: 42 }) as never,
    )
    const cancelSpy = vi.spyOn(api, 'cancelDownload').mockResolvedValue()
    const w = mount(SettingsUpdates)
    await flushPromises()

    const btn = w.find('button.primary.danger')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toContain('settings.updates.cancelDownload')
    expect(btn.text()).toContain('pct=42')
    await btn.trigger('click')
    await flushPromises()
    expect(cancelSpy).toHaveBeenCalledTimes(1)
  })

  it('renders both Install & Restart and Redownload while Ready', async () => {
    vi.spyOn(api, 'getUpdateState').mockResolvedValue(
      baseState({ ready: true }) as never,
    )
    const w = mount(SettingsUpdates)
    await flushPromises()

    const btns = w.findAll('button')
    const install = btns.find((b) => b.text().includes('settings.updates.forceInstallRestart'))
    const redl = btns.find((b) => b.text().includes('settings.updates.redownload'))
    expect(install?.exists()).toBe(true)
    expect(redl?.exists()).toBe(true)
  })

  it('clicking Redownload calls forceRedownload with the current latest tag', async () => {
    vi.spyOn(api, 'getUpdateState').mockResolvedValue(
      baseState({ ready: true, latest: 'v0.2.168' }) as never,
    )
    const forceSpy = vi.spyOn(api, 'forceRedownload').mockResolvedValue()
    const w = mount(SettingsUpdates)
    await flushPromises()

    const redl = w.findAll('button').find((b) => b.text().includes('settings.updates.redownload'))!
    await redl.trigger('click')
    await flushPromises()
    expect(forceSpy).toHaveBeenCalledWith('v0.2.168')
  })

  it('lazy-hit prompts and calls forceRedownload on confirm=true', async () => {
    // First mount: available + not ready. Second poll: downloaded_exists=true.
    const getSpy = vi.spyOn(api, 'getUpdateState')
      .mockResolvedValueOnce(baseState({ ready: false, available: true }) as never)
      .mockResolvedValueOnce(
        baseState({ ready: true, downloaded_exists: true, latest: 'v0.2.168' }) as never,
      )
    vi.spyOn(api, 'startDownload').mockResolvedValue()
    const forceSpy = vi.spyOn(api, 'forceRedownload').mockResolvedValue()
    confirmSpy.mockReturnValue(true)

    const w = mount(SettingsUpdates)
    await flushPromises()

    // Click the primary "Download" button — triggers clickInFlight.
    const dl = w.findAll('button').find((b) => b.text().includes('settings.updates.downloadVersion'))!
    await dl.trigger('click')
    await flushPromises()

    // Advance the poll interval (2000ms) so getUpdateState fires again.
    await vi.advanceTimersByTimeAsync(2100)
    await flushPromises()

    expect(getSpy).toHaveBeenCalledTimes(2)
    expect(confirmSpy).toHaveBeenCalled()
    expect(forceSpy).toHaveBeenCalledWith('v0.2.168')
  })

  it('lazy-hit with confirm=false does not call forceRedownload', async () => {
    vi.spyOn(api, 'getUpdateState')
      .mockResolvedValueOnce(baseState({ ready: false, available: true }) as never)
      .mockResolvedValueOnce(
        baseState({ ready: true, downloaded_exists: true, latest: 'v0.2.168' }) as never,
      )
    vi.spyOn(api, 'startDownload').mockResolvedValue()
    const forceSpy = vi.spyOn(api, 'forceRedownload').mockResolvedValue()
    confirmSpy.mockReturnValue(false)

    const w = mount(SettingsUpdates)
    await flushPromises()
    const dl = w.findAll('button').find((b) => b.text().includes('settings.updates.downloadVersion'))!
    await dl.trigger('click')
    await vi.advanceTimersByTimeAsync(2100)
    await flushPromises()

    expect(confirmSpy).toHaveBeenCalled()
    expect(forceSpy).not.toHaveBeenCalled()
  })

  it('spurious downloaded_exists (no click) does not prompt', async () => {
    // Mount with ready=false. Poll flips downloaded_exists=true without a
    // click having happened. Watcher must NOT fire the confirm.
    vi.spyOn(api, 'getUpdateState')
      .mockResolvedValueOnce(baseState({ ready: false }) as never)
      .mockResolvedValueOnce(
        baseState({ ready: true, downloaded_exists: true }) as never,
      )
    const forceSpy = vi.spyOn(api, 'forceRedownload').mockResolvedValue()

    const w = mount(SettingsUpdates)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2100)
    await flushPromises()

    expect(confirmSpy).not.toHaveBeenCalled()
    expect(forceSpy).not.toHaveBeenCalled()
    // Prevent unused-variable warnings.
    void nextTick
    void w
  })
})
