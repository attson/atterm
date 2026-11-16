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
      snippets: {
        load: vi.fn().mockResolvedValue([]),
        save: vi.fn().mockResolvedValue(undefined),
        clear: vi.fn().mockResolvedValue(undefined),
      },
      promptHistory: {
        status: vi.fn().mockResolvedValue({ enabled: true, count: 4 }),
        query: vi.fn().mockResolvedValue([]),
        setEnabled: vi.fn().mockResolvedValue(undefined),
        clear: vi.fn().mockResolvedValue(undefined),
      },
      events: {
        emit: vi.fn(),
        on: vi.fn().mockReturnValue(() => {}),
      },
      // Defaults to the desktop shape. The web build aliases `@` to this same
      // source tree and Capacitor mounts the same shell, so "run on hosts" is
      // gated on wailsBindings; a test that wants the web/iOS shape flips it.
      caps: { wailsBindings: true },
    },
  }
})

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string, params?: Record<string, unknown>) => params?.count === undefined ? k : `${k}:${params.count}` }),
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

  it('separates Quick and Snippet records into accessible tabs', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'quick-a', label: 'Quick A', text: 'echo quick' },
    ])
    __fake.snippets.load = vi.fn().mockResolvedValue([
      { id: 'snippet-a', label: 'Snippet A', text: 'echo snippet' },
    ])
    const w = mount(SettingsTemplates)
    await flushPromises()

    expect(w.find('[data-testid="template-kind-tabs"]').attributes('role')).toBe('tablist')
    expect(w.find('[data-testid="template-tab-quick"]').attributes('aria-selected')).toBe('true')
    expect(w.find('[data-testid="template-row-quick-a"]').exists()).toBe(true)
    expect(w.find('[data-testid="template-row-snippet-a"]').exists()).toBe(false)

    await w.find('[data-testid="template-tab-snippet"]').trigger('click')

    expect(w.find('[data-testid="template-tab-snippet"]').attributes('aria-selected')).toBe('true')
    expect(w.find('[data-testid="template-row-quick-a"]').exists()).toBe(false)
    expect(w.find('[data-testid="template-row-snippet-a"]').exists()).toBe(true)
  })

  it('shows footer actions for the active template type', async () => {
    const w = mount(SettingsTemplates)
    await flushPromises()

    expect(w.find('[data-testid="template-add"]').text()).toBe('settings.templates.addQuick')
    expect(w.find('[data-testid="template-reset"]').exists()).toBe(true)

    await w.find('[data-testid="template-tab-snippet"]').trigger('click')

    expect(w.find('[data-testid="template-add"]').text()).toBe('settings.templates.addSnippet')
    expect(w.find('[data-testid="template-reset"]').exists()).toBe(false)
  })

  it('shows a type-specific empty state inside an empty tab', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'quick-a', label: 'Quick A', text: 'echo quick' },
    ])
    __fake.snippets.load = vi.fn().mockResolvedValue([])
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-tab-snippet"]').trigger('click')

    expect(w.find('[data-testid="template-empty"]').text()).toBe('settings.templates.emptySnippet')
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

  it('creates a snippet without a hotkey and saves it to the independent collection', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.templates.load = vi.fn().mockResolvedValue([{ id: 'quick', label: 'Quick', text: 'ok' }])
    __fake.snippets.load = vi.fn().mockResolvedValue([])
    const saveSnippets = vi.fn().mockResolvedValue(undefined)
    __fake.snippets.save = saveSnippets
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-tab-snippet"]').trigger('click')
    await w.find('[data-testid="template-add"]').trigger('click')
    expect(w.find('[data-testid="template-kind-snippet"]').classes()).toContain('active')
    expect(w.find('[data-testid="template-edit-hotkey"]').exists()).toBe(false)
    await w.find('[data-testid="template-edit-label"]').setValue('Deploy')
    await w.find('[data-testid="template-edit-text"]').setValue('make build\nmake deploy')
    await w.find('[data-testid="template-edit-save"]').trigger('click')
    await flushPromises()

    expect(saveSnippets).toHaveBeenLastCalledWith([
      expect.objectContaining({ label: 'Deploy', text: 'make build\nmake deploy' }),
    ])
    expect(w.find('[data-testid^="template-row-"][data-kind="snippet"]').exists()).toBe(true)
  })

  it('converts a quick template to snippet with the same id at the target collection end', async () => {
    const { __fake } = await import('../../platform') as any
    const saveQuick = vi.fn().mockResolvedValue(undefined)
    const saveSnippets = vi.fn().mockResolvedValue(undefined)
    __fake.templates.load = vi.fn().mockResolvedValue([{ id: 'move-me', label: 'Deploy', text: 'deploy', hotkey: 'Alt+1' }])
    __fake.templates.save = saveQuick
    __fake.snippets.load = vi.fn().mockResolvedValue([{ id: 'existing', label: 'Existing', text: 'echo existing' }])
    __fake.snippets.save = saveSnippets
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-edit-move-me"]').trigger('click')
    await w.find('[data-testid="template-kind-snippet"]').trigger('click')
    await w.find('[data-testid="template-edit-save"]').trigger('click')
    await flushPromises()

    expect(saveQuick).toHaveBeenLastCalledWith([])
    expect(saveSnippets).toHaveBeenLastCalledWith([
      { id: 'existing', label: 'Existing', text: 'echo existing' },
      { id: 'move-me', label: 'Deploy', text: 'deploy' },
    ])
    expect(w.find('[data-testid="template-tab-snippet"]').attributes('aria-selected')).toBe('true')
    expect(w.find('[data-testid="template-row-move-me"]').exists()).toBe(true)
  })

  it('keeps the quick source when saving the target snippet fails during conversion', async () => {
    const { __fake } = await import('../../platform') as any
    const saveQuick = vi.fn().mockResolvedValue(undefined)
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'move-me', label: 'Deploy', text: 'deploy' },
    ])
    __fake.templates.save = saveQuick
    __fake.snippets.load = vi.fn().mockResolvedValue([])
    __fake.snippets.save = vi.fn().mockRejectedValue(new Error('storage full'))
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-edit-move-me"]').trigger('click')
    await w.find('[data-testid="template-kind-snippet"]').trigger('click')
    await w.find('[data-testid="template-edit-save"]').trigger('click')
    await flushPromises()

    expect(saveQuick).not.toHaveBeenCalled()
    expect(w.find('[data-testid="template-edit-text"]').exists()).toBe(true)
    expect(w.find('.error').text()).toContain('storage full')
  })

  it('shows the snippet for a duplicate id and removes the quick copy on the next save', async () => {
    const { __fake } = await import('../../platform') as any
    const saveQuick = vi.fn().mockResolvedValue(undefined)
    const saveSnippets = vi.fn().mockResolvedValue(undefined)
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'duplicate', label: 'Quick copy', text: 'echo quick' },
      { id: 'keep', label: 'Keep', text: 'echo keep' },
    ])
    __fake.templates.save = saveQuick
    __fake.snippets.load = vi.fn().mockResolvedValue([
      { id: 'duplicate', label: 'Snippet copy', text: 'echo snippet' },
    ])
    __fake.snippets.save = saveSnippets
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-tab-snippet"]').trigger('click')
    const duplicateRows = w.findAll('[data-testid="template-row-duplicate"]')
    expect(duplicateRows).toHaveLength(1)
    expect(duplicateRows[0].attributes('data-kind')).toBe('snippet')
    expect(duplicateRows[0].text()).toContain('Snippet copy')

    await w.find('[data-testid="template-edit-duplicate"]').trigger('click')
    await w.find('[data-testid="template-edit-save"]').trigger('click')
    await flushPromises()

    expect(saveQuick).toHaveBeenLastCalledWith([
      { id: 'keep', label: 'Keep', text: 'echo keep' },
    ])
    expect(saveSnippets).toHaveBeenLastCalledWith([
      { id: 'duplicate', label: 'Snippet copy', text: 'echo snippet' },
    ])
  })

  it('resetting quick defaults preserves snippets', async () => {
    const { __fake } = await import('../../platform') as any
    const clearQuick = vi.fn().mockResolvedValue(undefined)
    const clearSnippets = vi.fn().mockResolvedValue(undefined)
    __fake.templates.clear = clearQuick
    __fake.snippets.load = vi.fn().mockResolvedValue([{ id: 'keep', label: 'Keep', text: 'echo keep' }])
    __fake.snippets.clear = clearSnippets
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-reset"]').trigger('click')
    await w.find('[data-testid="template-reset-confirm"]').trigger('click')
    await flushPromises()

    expect(clearQuick).toHaveBeenCalledOnce()
    expect(clearSnippets).not.toHaveBeenCalled()
    await w.find('[data-testid="template-tab-snippet"]').trigger('click')
    expect(w.find('[data-testid="template-row-keep"]').exists()).toBe(true)
  })

  it('keeps invalid snippet text in the editor and does not persist', async () => {
    const { __fake } = await import('../../platform') as any
    const saveSnippets = vi.fn().mockResolvedValue(undefined)
    __fake.snippets.save = saveSnippets
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-add"]').trigger('click')
    await w.find('[data-testid="template-kind-snippet"]').trigger('click')
    await w.find('[data-testid="template-edit-label"]').setValue('Deploy')
    await w.find('[data-testid="template-edit-text"]').setValue('')
    await w.find('[data-testid="template-edit-save"]').trigger('click')
    await flushPromises()

    expect(saveSnippets).not.toHaveBeenCalled()
    expect(w.find('[data-testid="template-edit-text"]').exists()).toBe(true)
    expect(w.find('.error').exists()).toBe(true)
  })

  it('reloads when the platform fires prefs:changed (relay pull landed while pane is open)', async () => {
    const { __fake } = await import('../../platform') as any
    let handler: (() => void) | null = null
    const off = vi.fn()
    __fake.events.on = vi.fn((event: string, h: () => void) => {
      if (event === 'prefs:changed') handler = h
      return off
    })
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'old', label: 'old', text: 'old-text' },
    ])
    const w = mount(SettingsTemplates)
    await flushPromises()
    expect(w.find('[data-testid="template-row-old"]').exists()).toBe(true)
    expect(handler).not.toBeNull()

    // Simulate prefsSync pull: adapter now holds a fresh list.
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'synced', label: 'synced-from-relay', text: 'synced-text' },
    ])
    handler!()
    await flushPromises()

    expect(w.find('[data-testid="template-row-old"]').exists()).toBe(false)
    expect(w.find('[data-testid="template-row-synced"]').exists()).toBe(true)

    w.unmount()
    expect(off).toHaveBeenCalled()
  })

  it('keeps execution actions out of the settings editor', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'a', label: 'A', text: 'a-text' },
    ])
    const web = mount(SettingsTemplates)
    await flushPromises()
    expect(web.find('[data-testid="template-run-a"]').exists()).toBe(false)
    expect(web.find('[data-testid="template-row-a"]').exists()).toBe(true)
  })

  it('loads and toggles desktop AI prompt history suggestions', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.caps = { wailsBindings: true }
    __fake.promptHistory.status = vi.fn().mockResolvedValue({ enabled: true, count: 4 })
    __fake.promptHistory.setEnabled = vi.fn().mockResolvedValue(undefined)
    const w = mount(SettingsTemplates)
    await flushPromises()

    expect(w.find('[data-testid="prompt-history-count"]').text()).toContain('4')
    await w.find('[data-testid="prompt-history-toggle"]').setValue(false)
    await flushPromises()
    expect(__fake.promptHistory.setEnabled).toHaveBeenCalledWith(false)
    expect(__fake.events.emit).toHaveBeenCalledWith('promptHistory:changed', null)
  })

  it('requires confirmation before clearing prompt history', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.caps = { wailsBindings: true }
    __fake.promptHistory.status = vi.fn()
      .mockResolvedValueOnce({ enabled: true, count: 4 })
      .mockResolvedValueOnce({ enabled: true, count: 0 })
    __fake.promptHistory.clear = vi.fn().mockResolvedValue(undefined)
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="prompt-history-clear"]').trigger('click')
    expect(__fake.promptHistory.clear).not.toHaveBeenCalled()
    await w.find('[data-testid="prompt-history-clear-confirm"]').trigger('click')
    await flushPromises()
    expect(__fake.promptHistory.clear).toHaveBeenCalledOnce()
    expect(w.find('[data-testid="prompt-history-count"]').text()).toContain('0')
  })
})
