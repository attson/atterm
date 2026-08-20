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
      // Defaults to the desktop shape. The web build aliases `@` to this same
      // source tree and Capacitor mounts the same shell, so "run on hosts" is
      // gated on wailsBindings; a test that wants the web/iOS shape flips it.
      caps: { wailsBindings: true },
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

  it('passes the template label and text to the run panel in the right order', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.caps = { wailsBindings: true }
    // The two props are adjacent and both strings, so a swap typechecks and
    // every other test stays green — while the runtime consequence is running
    // the template's LABEL as a shell command on N remote hosts. Distinct,
    // unmistakable values are the whole point of this test.
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'a', label: 'Restart nginx', text: 'systemctl restart nginx' },
    ])
    const w = mount(SettingsTemplates)
    await flushPromises()

    await w.find('[data-testid="template-run-a"]').trigger('click')
    await flushPromises()

    const panel = w.findComponent({ name: 'SnippetRunPanel' })
    expect(panel.exists()).toBe(true)
    expect(panel.props('snippetLabel')).toBe('Restart nginx')
    expect(panel.props('snippetText')).toBe('systemctl restart nginx')
  })

  it('hides "run on hosts" where the Wails bindings do not exist (web, iOS)', async () => {
    const { __fake } = await import('../../platform') as any
    __fake.templates.load = vi.fn().mockResolvedValue([
      { id: 'a', label: 'A', text: 'a-text' },
    ])

    __fake.caps = { wailsBindings: true }
    const desktop = mount(SettingsTemplates)
    await flushPromises()
    expect(desktop.find('[data-testid="template-run-a"]').exists()).toBe(true)

    // Same component, web/Capacitor shape. Without the gate the button renders,
    // the modal opens, listSSHHosts throws app.wailsBindingsNotReady, and the
    // user is left looking at a developer's error message in a dead dialog.
    __fake.caps = { wailsBindings: false }
    const web = mount(SettingsTemplates)
    await flushPromises()
    expect(web.find('[data-testid="template-run-a"]').exists()).toBe(false)
    // The rest of the tab still works everywhere — the gate is on the button,
    // not the tab.
    expect(web.find('[data-testid="template-row-a"]').exists()).toBe(true)
  })
})
