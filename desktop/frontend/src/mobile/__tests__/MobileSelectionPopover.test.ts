import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MobileSelectionPopover from '../MobileSelectionPopover.vue'

const baseProps = {
  visible: true,
  x: 120,
  y: 80,
  arrowDir: 'down' as const,
  copying: false,
  sending: false,
}

describe('MobileSelectionPopover', () => {
  it('does not render when visible=false', () => {
    const w = mount(MobileSelectionPopover, { props: { ...baseProps, visible: false } })
    expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
  })

  it('renders copy / send / cancel buttons', () => {
    const w = mount(MobileSelectionPopover, { props: baseProps })
    expect(w.find('[data-testid="selection-popover-copy"]').exists()).toBe(true)
    expect(w.find('[data-testid="selection-popover-send"]').exists()).toBe(true)
    expect(w.find('[data-testid="selection-popover-cancel"]').exists()).toBe(true)
  })

  it('emits copy on copy tap', async () => {
    const w = mount(MobileSelectionPopover, { props: baseProps })
    await w.find('[data-testid="selection-popover-copy"]').trigger('click')
    expect(w.emitted('copy')).toBeTruthy()
  })

  it('emits send on send tap', async () => {
    const w = mount(MobileSelectionPopover, { props: baseProps })
    await w.find('[data-testid="selection-popover-send"]').trigger('click')
    expect(w.emitted('send')).toBeTruthy()
  })

  it('emits cancel on × tap', async () => {
    const w = mount(MobileSelectionPopover, { props: baseProps })
    await w.find('[data-testid="selection-popover-cancel"]').trigger('click')
    expect(w.emitted('cancel')).toBeTruthy()
  })

  it('disables copy when copying=true', () => {
    const w = mount(MobileSelectionPopover, { props: { ...baseProps, copying: true } })
    expect((w.find('[data-testid="selection-popover-copy"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('disables send when sending=true', () => {
    const w = mount(MobileSelectionPopover, { props: { ...baseProps, sending: true } })
    expect((w.find('[data-testid="selection-popover-send"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('positions itself using x and y when arrowDir=down', () => {
    const w = mount(MobileSelectionPopover, { props: { ...baseProps, x: 120, y: 80, arrowDir: 'down' } })
    const styleAttr = w.find('[data-testid="selection-popover"]').attributes('style') || ''
    expect(styleAttr).toContain('left: 120px')
    expect(styleAttr).toContain('bottom: 80px')
  })

  it('positions itself using top when arrowDir=up', () => {
    const w = mount(MobileSelectionPopover, { props: { ...baseProps, x: 120, y: 80, arrowDir: 'up' } })
    const styleAttr = w.find('[data-testid="selection-popover"]').attributes('style') || ''
    expect(styleAttr).toContain('left: 120px')
    expect(styleAttr).toContain('top: 80px')
  })
})
