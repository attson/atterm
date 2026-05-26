import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../platform'
import { createFakePlatform } from '../platform/__tests__/_fakePlatform'
import { setMaximized } from '../composables/useWindowMaximized'
import WindowControls from './WindowControls.vue'
import { en } from '../i18n/messages/en'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  setMaximized(false)
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})

afterEach(() => {
  __setPlatformForTests(null)
})

describe('WindowControls', () => {
  it('renders three buttons: minimise, maximise/restore, close', () => {
    const w = mount(WindowControls)
    expect(w.find('[data-testid="window-min"]').exists()).toBe(true)
    expect(w.find('[data-testid="window-max"]').exists()).toBe(true)
    expect(w.find('[data-testid="window-close"]').exists()).toBe(true)
  })

  it('min button calls platform.system.windowMinimize', async () => {
    const w = mount(WindowControls)
    await w.get('[data-testid="window-min"]').trigger('click')
    expect(platform.system.windowMinimize).toHaveBeenCalledTimes(1)
  })

  it('max button calls platform.system.windowToggleMaximize and flips the shared ref', async () => {
    const w = mount(WindowControls)
    await w.get('[data-testid="window-max"]').trigger('click')
    expect(platform.system.windowToggleMaximize).toHaveBeenCalledTimes(1)
    await flushPromises()
    expect(w.get('[data-testid="window-max"]').attributes('aria-label')).toBe(en.common.restore)
  })

  it('close button calls platform.system.quit', async () => {
    const w = mount(WindowControls)
    await w.get('[data-testid="window-close"]').trigger('click')
    expect(platform.system.quit).toHaveBeenCalledTimes(1)
  })

  it('when started maximized, max button starts in restore variant', async () => {
    setMaximized(true)
    const w = mount(WindowControls)
    await flushPromises()
    expect(w.get('[data-testid="window-max"]').attributes('aria-label')).toBe(en.common.restore)
  })

  it('if a runtime call throws, the button does not propagate the error', async () => {
    vi.mocked(platform.system.windowMinimize!).mockImplementation(() => {
      throw new Error('runtime gone')
    })
    const w = mount(WindowControls)
    await expect(
      w.get('[data-testid="window-min"]').trigger('click'),
    ).resolves.toBeUndefined()
  })
})
