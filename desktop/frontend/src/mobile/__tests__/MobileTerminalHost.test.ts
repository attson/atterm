import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MobileTerminalHost from '../MobileTerminalHost.vue'
import type { RemoteSession } from '../../platform/types'

const mk = (id: string, title: string): RemoteSession =>
  ({ session_id: id, host_id: 'h', host: 'box', user: 'me', title, cols: 80, rows: 24 })

const stubs = { MobileTerminal: { props: ['active', 'sessionId'], template: '<div class="mt" :data-active="active" :data-sid="sessionId"></div>' } }

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
})
