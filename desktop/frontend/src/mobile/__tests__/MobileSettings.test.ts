import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { initI18n, resetI18nForTest } from '../../i18n'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSettings from '../MobileSettings.vue'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(async () => {
  vi.clearAllMocks()
  resetI18nForTest()
  await initI18n({ getLanguages: () => ['en-US'], listenLanguageChange: () => () => undefined })
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})
afterEach(() => { __setPlatformForTests(null); resetI18nForTest() })

describe('MobileSettings', () => {
  it('renders language, both editors, and logout', async () => {
    const w = mount(MobileSettings)
    await flushPromises()
    expect(w.find('[data-testid="settings-language"]').exists()).toBe(true)
    expect(w.find('[data-testid="settings-templates"]').exists()).toBe(true)
    expect(w.find('[data-testid="settings-auxkeys"]').exists()).toBe(true)
    expect(w.find('[data-testid="settings-logout"]').exists()).toBe(true)
  })

  it('seeds the editors with defaults when storage is empty', async () => {
    const w = mount(MobileSettings)
    await flushPromises()
    // effectiveTemplates / effectiveAuxKeys fall back to defaults on empty load
    expect(w.find('[data-testid="settings-templates-row-default-y"]').exists()).toBe(true)
    expect(w.find('[data-testid="settings-auxkeys-row-aux-enter"]').exists()).toBe(true)
  })

  it('logout emits logout and does NOT clear the saved relay config', async () => {
    const w = mount(MobileSettings)
    await flushPromises()
    await w.find('[data-testid="settings-logout"]').trigger('click')
    expect(w.emitted('logout')).toBeTruthy()
    expect(platform.relay.clear).not.toHaveBeenCalled()
  })

  it('back emits back', async () => {
    const w = mount(MobileSettings)
    await flushPromises()
    await w.find('[data-testid="settings-back"]').trigger('click')
    expect(w.emitted('back')).toBeTruthy()
  })

  it('deleting a template row persists the shortened list', async () => {
    const w = mount(MobileSettings)
    await flushPromises()
    await w.find('[data-testid="settings-templates-delete-default-y"]').trigger('click')
    expect(platform.templates.save).toHaveBeenCalled()
    const saved = (platform.templates.save as ReturnType<typeof vi.fn>).mock.calls.at(-1)![0]
    expect(saved.find((t: { id: string }) => t.id === 'default-y')).toBeUndefined()
  })

  it('reset templates clears storage', async () => {
    const w = mount(MobileSettings)
    await flushPromises()
    await w.find('[data-testid="settings-templates-reset"]').trigger('click')
    await w.find('[data-testid="settings-templates-reset-confirm"]').trigger('click')
    expect(platform.templates.clear).toHaveBeenCalled()
  })

  it('adding an aux key parses escape notation into raw bytes before saving', async () => {
    const w = mount(MobileSettings)
    await flushPromises()
    await w.find('[data-testid="settings-auxkeys-add"]').trigger('click')
    await w.find('[data-testid="settings-auxkeys-edit-label"]').setValue('F1')
    await w.find('[data-testid="settings-auxkeys-edit-value"]').setValue('\\x1bOP')
    await w.find('[data-testid="settings-auxkeys-edit-save"]').trigger('click')
    expect(platform.auxKeys.save).toHaveBeenCalled()
    const saved = (platform.auxKeys.save as ReturnType<typeof vi.fn>).mock.calls.at(-1)![0]
    const added = saved.find((k: { label: string }) => k.label === 'F1')
    expect(added.seq).toBe('\x1bOP') // \x1b decoded to a real ESC byte
  })
})
