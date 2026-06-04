import { mount } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import TemplatePreviewDialog from '../TemplatePreviewDialog.vue'

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

const sample = { id: 'default-test', label: '/test', text: '/test' }

describe('TemplatePreviewDialog', () => {
  it('renders nothing when template is null', () => {
    const w = mount(TemplatePreviewDialog, { props: { template: null } })
    expect(w.find('[data-testid="template-preview"]').exists()).toBe(false)
  })

  it('renders the template text when set', () => {
    const w = mount(TemplatePreviewDialog, { props: { template: sample } })
    expect(w.find('[data-testid="template-preview"]').exists()).toBe(true)
    expect(w.find('[data-testid="template-preview"]').text()).toContain('/test')
  })

  it('emits confirm when the Send button is clicked', async () => {
    const w = mount(TemplatePreviewDialog, { props: { template: sample } })
    await w.find('[data-testid="template-preview-confirm"]').trigger('click')
    expect(w.emitted('confirm')).toBeTruthy()
    expect(w.emitted('confirm')![0]).toEqual([sample])
  })

  it('emits cancel when the Cancel button is clicked', async () => {
    const w = mount(TemplatePreviewDialog, { props: { template: sample } })
    await w.find('[data-testid="template-preview-cancel"]').trigger('click')
    expect(w.emitted('cancel')).toBeTruthy()
  })

  it('emits cancel when the backdrop is clicked', async () => {
    const w = mount(TemplatePreviewDialog, { props: { template: sample } })
    await w.find('.dialog-backdrop').trigger('click')
    expect(w.emitted('cancel')).toBeTruthy()
  })
})
