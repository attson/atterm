import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ShortcutBar from '@/main/components/ShortcutBar.vue'

describe('ShortcutBar.vue', () => {
  it('emits input("\\x1b") on Esc click', async () => {
    const wrapper = mount(ShortcutBar)
    await wrapper.get('[data-shortcut="esc"]').trigger('click')
    expect(wrapper.emitted('input')?.[0]).toEqual(['\x1b'])
  })

  it('emits input("\\t") on Tab click', async () => {
    const wrapper = mount(ShortcutBar)
    await wrapper.get('[data-shortcut="tab"]').trigger('click')
    expect(wrapper.emitted('input')?.[0]).toEqual(['\t'])
  })

  it('emits input("\\x03") on Ctrl-C click', async () => {
    const wrapper = mount(ShortcutBar)
    await wrapper.get('[data-shortcut="ctrl-c"]').trigger('click')
    expect(wrapper.emitted('input')?.[0]).toEqual(['\x03'])
  })

  it('emits input("\\x04") on Ctrl-D click', async () => {
    const wrapper = mount(ShortcutBar)
    await wrapper.get('[data-shortcut="ctrl-d"]').trigger('click')
    expect(wrapper.emitted('input')?.[0]).toEqual(['\x04'])
  })

  it('emits CSI A/B/C/D for arrow keys', async () => {
    const wrapper = mount(ShortcutBar)
    await wrapper.get('[data-shortcut="up"]').trigger('click')
    expect(wrapper.emitted('input')?.[0]).toEqual(['\x1b[A'])
    await wrapper.get('[data-shortcut="down"]').trigger('click')
    expect(wrapper.emitted('input')?.[1]).toEqual(['\x1b[B'])
    await wrapper.get('[data-shortcut="right"]').trigger('click')
    expect(wrapper.emitted('input')?.[2]).toEqual(['\x1b[C'])
    await wrapper.get('[data-shortcut="left"]').trigger('click')
    expect(wrapper.emitted('input')?.[3]).toEqual(['\x1b[D'])
  })

  it('emits copy and paste events on the dedicated buttons', async () => {
    const wrapper = mount(ShortcutBar)
    await wrapper.get('[data-testid="copy"]').trigger('click')
    expect(wrapper.emitted('copy')).toBeTruthy()
    await wrapper.get('[data-testid="paste"]').trigger('click')
    expect(wrapper.emitted('paste')).toBeTruthy()
  })
})
