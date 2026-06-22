import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

const { fake } = vi.hoisted(() => ({
  fake: {
    events: { on: vi.fn().mockReturnValue(() => {}), off: vi.fn(), emit: vi.fn() },
  },
}))

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

vi.mock('../../platform', () => ({
  usePlatform: () => fake,
}))

import SettingsRelay from '../SettingsRelay.vue'
import * as api from '../../lib/api'

function baseRelayConfig() {
  return {
    url: 'wss://r.example.com',
    token: '',
    session_expires_at: 0,
    allow_insecure_relay: false,
    disable_e2ee: false,
    remote_permission: 'full' as const,
    last_email: 'u@example.com',
    connected: false,
  }
}

beforeEach(() => {
  vi.spyOn(api, 'getRelayConfig').mockResolvedValue(baseRelayConfig() as never)
  vi.spyOn(api, 'fetchRelayMe').mockResolvedValue({ user_id: '', email: '' } as never)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SettingsRelay password prefill', () => {
  it('prefills the password input from LoadSavedRelayPassword on mount', async () => {
    vi.spyOn(api, 'loadSavedRelayPassword').mockResolvedValue('hunter2')
    const w = mount(SettingsRelay)
    await flushPromises()
    const pw = w.find('#relay-password').element as HTMLInputElement
    expect(pw.value).toBe('hunter2')
  })

  it('leaves the password input empty when nothing is stored', async () => {
    vi.spyOn(api, 'loadSavedRelayPassword').mockResolvedValue('')
    const w = mount(SettingsRelay)
    await flushPromises()
    const pw = w.find('#relay-password').element as HTMLInputElement
    expect(pw.value).toBe('')
  })

  it('does not call LoadSavedRelayPassword when last_email is empty', async () => {
    vi.spyOn(api, 'getRelayConfig').mockResolvedValue({
      ...baseRelayConfig(),
      last_email: '',
    } as never)
    const spy = vi.spyOn(api, 'loadSavedRelayPassword').mockResolvedValue('hunter2')
    const w = mount(SettingsRelay)
    await flushPromises()
    expect(spy).not.toHaveBeenCalled()
    const pw = w.find('#relay-password').element as HTMLInputElement
    expect(pw.value).toBe('')
  })
})

describe('SettingsRelay post-login password retention', () => {
  it('keeps password.value populated after a successful login', async () => {
    vi.spyOn(api, 'loadSavedRelayPassword').mockResolvedValue('')
    vi.spyOn(api, 'probeRelayVersion').mockResolvedValue(undefined as never)
    vi.spyOn(api, 'loginRemoteRelay').mockResolvedValue(undefined as never)
    // After login the component re-reads the persisted config; second call
    // returns the same shape with a token to mimic a logged-in state.
    let firstCall = true
    vi.spyOn(api, 'getRelayConfig').mockImplementation(async () => {
      const cfg = baseRelayConfig() as ReturnType<typeof baseRelayConfig>
      if (!firstCall) cfg.token = 'session-tok'
      firstCall = false
      return cfg as never
    })

    const w = mount(SettingsRelay)
    await flushPromises()

    // Drive the form via the named inputs and the component's exposed
    // save() method (SettingsRelay defineExposes save, canSave, ...).
    await w.find('#relay-host').setValue('r.example.com')
    await w.find('#relay-email').setValue('u@example.com')
    await w.find('#relay-password').setValue('hunter2')
    await (w.vm as unknown as { save: () => Promise<void> }).save()
    await flushPromises()

    const pw = w.find('#relay-password').element as HTMLInputElement
    expect(pw.value).toBe('hunter2')
  })
})

describe('SettingsRelay remembers password on failed connect', () => {
  it('calls rememberRelayPassword when probe fails with a non-empty password', async () => {
    vi.spyOn(api, 'loadSavedRelayPassword').mockResolvedValue('')
    vi.spyOn(api, 'probeRelayVersion').mockRejectedValue(new Error('EOF'))
    const remember = vi.spyOn(api, 'rememberRelayPassword').mockResolvedValue(undefined as never)
    vi.spyOn(api, 'setRelayConfig').mockResolvedValue(undefined as never)

    const w = mount(SettingsRelay)
    await flushPromises()

    await w.find('#relay-host').setValue('r.example.com')
    await w.find('#relay-email').setValue('u@example.com')
    await w.find('#relay-password').setValue('hunter2')
    await (w.vm as unknown as { save: () => Promise<void> }).save()
    await flushPromises()

    expect(remember).toHaveBeenCalledWith('hunter2')
  })

  it('does not call rememberRelayPassword when probe fails with empty password', async () => {
    vi.spyOn(api, 'loadSavedRelayPassword').mockResolvedValue('')
    vi.spyOn(api, 'probeRelayVersion').mockRejectedValue(new Error('EOF'))
    const remember = vi.spyOn(api, 'rememberRelayPassword').mockResolvedValue(undefined as never)
    vi.spyOn(api, 'setRelayConfig').mockResolvedValue(undefined as never)

    const w = mount(SettingsRelay)
    await flushPromises()

    await w.find('#relay-host').setValue('r.example.com')
    await w.find('#relay-email').setValue('u@example.com')
    // intentionally leave password empty
    await (w.vm as unknown as { save: () => Promise<void> }).save()
    await flushPromises()

    expect(remember).not.toHaveBeenCalled()
  })

  it('clears the dirty flag after a failed connect so closing Settings does not prompt', async () => {
    vi.spyOn(api, 'loadSavedRelayPassword').mockResolvedValue('')
    vi.spyOn(api, 'probeRelayVersion').mockRejectedValue(new Error('EOF'))
    vi.spyOn(api, 'rememberRelayPassword').mockResolvedValue(undefined as never)
    vi.spyOn(api, 'setRelayConfig').mockResolvedValue(undefined as never)

    const w = mount(SettingsRelay)
    await flushPromises()

    // Type a new host different from the initial baseRelayConfig().url so the
    // dirty computed would normally flip true.
    await w.find('#relay-host').setValue('new-host.example.com')
    await w.find('#relay-password').setValue('hunter2')
    expect((w.vm as unknown as { dirty: boolean }).dirty).toBe(true)

    await (w.vm as unknown as { save: () => Promise<void> }).save()
    await flushPromises()

    // After save() (which routed through rememberInputs on probe failure),
    // the snapshot was refreshed, so dirty is back to false.
    expect((w.vm as unknown as { dirty: boolean }).dirty).toBe(false)
  })
})
