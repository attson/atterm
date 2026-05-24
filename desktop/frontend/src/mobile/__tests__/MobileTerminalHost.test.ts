import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MobileTerminalHost from '../MobileTerminalHost.vue'
import type { RemoteSession } from '../../platform/types'

const mk = (id: string, title: string, over: Partial<RemoteSession> = {}): RemoteSession =>
  ({ session_id: id, host_id: 'h', host: 'box', user: 'me', title, cols: 80, rows: 24, ...over })

const stubs = {
  MobileTerminal: {
    name: 'MobileTerminal',
    props: ['active', 'sessionId'],
    emits: ['meta'],
    template: '<div class="mt" :data-active="active" :data-sid="sessionId"></div>',
  },
}

const baseProps = {
  endpoint: { url: 'wss://r', token: 'atk_t' },
  openTerminals: [
    { sessionId: 'a', info: mk('a', 'claude') },
    { sessionId: 'b', info: mk('b', 'codex') },
  ],
  activeSessionId: 'a',
}

describe('MobileTerminalHost', () => {
  it('renders one tab per open terminal + a MobileTerminal per open terminal', () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    expect(w.findAll('[data-testid="term-tab"]').length).toBe(2)
    expect(w.findAll('.mt').length).toBe(2)
  })

  it('only the active terminal is visibly active; both stay mounted', () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    const actives = w.findAll('.mt').map((m) => m.attributes('data-active'))
    expect(actives.filter((a) => a === 'true').length).toBe(1)
    expect(w.findAll('.mt').length).toBe(2)   // both mounted (keepalive)
  })

  it('emits switch when a non-active tab is tapped', async () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    await w.findAll('[data-testid="term-tab"]')[1]!.trigger('click')
    expect(w.emitted('switch')![0]).toEqual(['b'])
  })

  it('emits close when a tab × is tapped', async () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    await w.find('[data-testid="tab-close-a"]').trigger('click')
    expect(w.emitted('close')![0]).toEqual(['a'])
  })

  it('emits back from the back button', async () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    await w.find('[data-testid="term-back"]').trigger('click')
    expect(w.emitted('back')).toBeTruthy()
  })

  it('labels tabs with the basename of the live cwd, falling back to the title', () => {
    const props = {
      ...baseProps,
      openTerminals: [
        { sessionId: 'a', info: mk('a', '/bin/zsh', { cwd: '/Users/me/proj' }) },
        { sessionId: 'b', info: mk('b', '/bin/zsh') }, // no cwd → falls back to title
      ],
    }
    const w = mount(MobileTerminalHost, { props, global: { stubs } })
    const labels = w.findAll('[data-testid="term-tab"] .lbl').map((n) => n.text())
    expect(labels).toEqual(['proj', 'zsh']) // basename of cwd, else basename of the shell command
  })

  it('shows user@host of the active session in the header', () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    expect(w.find('[data-testid="term-who"]').text()).toBe('me@box')
  })

  it('bubbles a terminal META update up as (sessionId, meta)', () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    const terms = w.findAllComponents({ name: 'MobileTerminal' })
    terms[0]!.vm.$emit('meta', { cwd: '/x/y' })
    expect(w.emitted('meta')![0]).toEqual(['a', { cwd: '/x/y' }])
  })
})
