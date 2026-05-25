import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'

import LanguageSelect from '@shared/components/LanguageSelect.vue'
import { initI18n, resetI18nForTest } from '@shared/i18n'

describe('LanguageSelect.vue', () => {
  beforeEach(() => {
    localStorage.clear()
    resetI18nForTest()
    initI18n({ getLanguages: () => ['en-US'], listenLanguageChange: () => () => undefined })
  })

  it('renders the language label and persists zh-CN preference', async () => {
    const wrapper = mount(LanguageSelect)

    expect(wrapper.text()).toContain('Language')

    await wrapper.get('[data-testid="language-select"]').setValue('zh-CN')

    expect(localStorage.getItem('atterm.locale')).toBe('zh-CN')
  })
})
