import { computed, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import { useQuickTemplates } from './useQuickTemplates'

describe('useQuickTemplates collision gate', () => {
  it('hides and blocks a quick template shadowed by a snippet id', async () => {
    const blockedIDs = ref<ReadonlySet<string>>(new Set(['duplicate']))
    const send = vi.fn()
    const state = useQuickTemplates({
      send,
      confirmRequired: computed(() => false),
      focused: () => true,
      templatesBridge: {
        load: vi.fn().mockResolvedValue([
          { id: 'duplicate', label: 'Quick copy', text: 'danger', hotkey: 'Alt+1' },
          { id: 'safe', label: 'Safe', text: 'safe' },
        ]),
        save: vi.fn(),
        clear: vi.fn(),
        loadHidden: vi.fn().mockResolvedValue(false),
        saveHidden: vi.fn(),
      },
      isEnabled: (template) => !blockedIDs.value.has(template.id),
    })

    await state.reload()

    expect(state.templates.value.map((template) => template.id)).toEqual(['safe'])
    state.requestTemplateSend({ id: 'duplicate', label: 'Quick copy', text: 'danger', hotkey: 'Alt+1' })
    state.onTemplateHotkey(new KeyboardEvent('keydown', { key: '1', altKey: true }))
    expect(send).not.toHaveBeenCalled()
  })
})
