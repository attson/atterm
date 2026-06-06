import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'

const { fake } = vi.hoisted(() => {
  return {
    fake: {
      templates: {
        load: vi.fn().mockResolvedValue([]),
        save: vi.fn().mockResolvedValue(undefined),
        clear: vi.fn().mockResolvedValue(undefined),
        loadHidden: vi.fn().mockResolvedValue(false),
        saveHidden: vi.fn().mockResolvedValue(undefined),
      },
      events: {
        emit: vi.fn(),
        on: vi.fn().mockReturnValue(() => {}),
      },
    },
  }
})

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

vi.mock('../../platform', () => ({
  usePlatform: () => fake,
  __fake: fake,
}))

import SettingsTemplates from '../SettingsTemplates.vue'

describe('SettingsTemplates', () => {
  it('renders the loaded templates as rows', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'a', label: 'A', text: 'a-text' },
      { id: 'b', label: 'B', text: 'b-text' },
    ])
    const w = mount(SettingsTemplates)
    await flushPromises()
    expect(w.findAll('[data-testid^="template-row-"]')).toHaveLength(2)
  })

  it('calls save when a new template is added to an existing list', async () => {
    const { __fake } = await import('../../platform') as any
    const save = vi.fn().mockResolvedValue(undefined)
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'existing', label: 'existing', text: 'existing-text' },
    ])
    __fake.templates.save = save
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-add"]').trigger('click')
    await w.find('[data-testid="template-edit-label"]').setValue('approve')
    await w.find('[data-testid="template-edit-text"]').setValue('approve')
    await w.find('[data-testid="template-edit-save"]').trigger('click')
    await flushPromises()

    expect(save).toHaveBeenCalled()
    const list = save.mock.calls[save.mock.calls.length - 1][0]
    expect(list).toHaveLength(2)
    expect(list[1]).toMatchObject({ label: 'approve', text: 'approve' })
    expect(list[1].id).toBeTruthy()
  })

  it('renders bundled DEFAULT_TEMPLATES when stored list is empty', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.templates.load = vi.fn().mockResolvedValue([])
    const w = mount(SettingsTemplates)
    await flushPromises()
    // DEFAULT_TEMPLATES bundles 9 entries (yes / ok / continue / commit /
    // push / release / 1 / 2 / 3). Editor mirrors them when storage is empty.
    expect(w.findAll('[data-testid^="template-row-"]').length).toBeGreaterThanOrEqual(9)
  })

  it('persists the visible default set on first add from empty storage', async () => {
    const { __fake } = await import('../../platform') as any
    const save = vi.fn().mockResolvedValue(undefined)
    __fake.templates.load = vi.fn().mockResolvedValue([])
    __fake.templates.save = save
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-add"]').trigger('click')
    await w.find('[data-testid="template-edit-label"]').setValue('approve')
    await w.find('[data-testid="template-edit-text"]').setValue('approve')
    await w.find('[data-testid="template-edit-save"]').trigger('click')
    await flushPromises()

    expect(save).toHaveBeenCalled()
    const list = save.mock.calls[save.mock.calls.length - 1][0]
    // First customization locks in the defaults + the new entry.
    expect(list.length).toBeGreaterThanOrEqual(10)
    expect(list[list.length - 1]).toMatchObject({ label: 'approve', text: 'approve' })
  })

  it('clears storage on Reset to defaults', async () => {
    const { __fake } = await import('../../platform') as any
    const clear = vi.fn().mockResolvedValue(undefined)
    __fake.templates.load = vi.fn().mockResolvedValue([{ id: 'x', label: 'x', text: 'x' }])
    __fake.templates.clear = clear
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-reset"]').trigger('click')
    await w.find('[data-testid="template-reset-confirm"]').trigger('click')
    await flushPromises()

    expect(clear).toHaveBeenCalled()
  })
})
