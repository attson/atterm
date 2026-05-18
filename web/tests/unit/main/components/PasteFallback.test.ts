import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import PasteFallback from '@/main/components/PasteFallback.vue'

describe('PasteFallback.vue', () => {
  it('emits paste-text and closes on submit', async () => {
    const wrapper = mount(PasteFallback, { props: { open: true } })
    await wrapper.get('textarea').setValue('hello\nworld')
    await wrapper.get('form').trigger('submit')

    expect(wrapper.emitted('paste-text')?.[0]).toEqual(['hello\nworld'])
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('emits close on cancel', async () => {
    const wrapper = mount(PasteFallback, { props: { open: true } })
    await wrapper.get('[data-testid="paste-cancel"]').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('emits paste-image with the picked file', async () => {
    const wrapper = mount(PasteFallback, { props: { open: true } })
    const file = new File(['fake-png'], 'shot.png', { type: 'image/png' })
    const input = wrapper.get('[data-testid="paste-image-file"]').element as HTMLInputElement
    Object.defineProperty(input, 'files', { value: [file], configurable: true })
    await wrapper.get('[data-testid="paste-image-file"]').trigger('change')

    expect(wrapper.emitted('paste-image')?.[0]?.[0]).toBe(file)
  })

  it('renders nothing when open is false', () => {
    const wrapper = mount(PasteFallback, { props: { open: false } })
    expect(wrapper.find('form').exists()).toBe(false)
  })
})
