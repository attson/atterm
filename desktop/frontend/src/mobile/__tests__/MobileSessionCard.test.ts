import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MobileSessionCard from '../MobileSessionCard.vue'
import type { RemoteSession } from '../../platform/types'

const base: RemoteSession = {
  session_id: 's1', host_id: 'h1', host: 'box1', user: 'me',
  title: 'zsh', cwd: '/Users/me/proj', cols: 80, rows: 24,
  task_state: 'running', current_command: '/usr/local/bin/claude --foo',
}

describe('MobileSessionCard', () => {
  it('renders the short command label and shortened cwd', () => {
    const w = mount(MobileSessionCard, { props: { session: base, home: '/Users/me' } })
    expect(w.text()).toContain('claude')
    expect(w.text()).not.toContain('/usr/local/bin/claude --foo')
    expect(w.text()).toContain('proj')
  })

  it('renders host·user on the helper line', () => {
    const w = mount(MobileSessionCard, { props: { session: base, home: '/Users/me' } })
    expect(w.text()).toContain('box1')
    expect(w.text()).toContain('me')
  })

  it('shows the unread dot and ✓ only when session.unread is true', () => {
    const seen = mount(MobileSessionCard, { props: { session: { ...base, unread: false }, home: '/' } })
    expect(seen.find('[data-testid="unread-dot"]').exists()).toBe(false)
    expect(seen.find('[data-testid="row-mark-read"]').exists()).toBe(false)

    const unread = mount(MobileSessionCard, { props: { session: { ...base, unread: true }, home: '/' } })
    expect(unread.find('[data-testid="unread-dot"]').exists()).toBe(true)
    expect(unread.find('[data-testid="row-mark-read"]').exists()).toBe(true)
  })

  it('emits open when the card body is tapped', async () => {
    const w = mount(MobileSessionCard, { props: { session: base, home: '/' } })
    await w.find('[data-testid="card-body"]').trigger('click')
    expect(w.emitted('open')).toBeTruthy()
    expect(w.emitted('open')![0]![0]).toEqual(base)
  })

  it('emits markSeen with this session id when ✓ is tapped, does not emit open', async () => {
    const session = { ...base, unread: true }
    const w = mount(MobileSessionCard, { props: { session, home: '/' } })
    await w.find('[data-testid="row-mark-read"]').trigger('click')
    expect(w.emitted('markSeen')).toBeTruthy()
    expect(w.emitted('markSeen')![0]![0]).toEqual({ ids: ['s1'] })
    expect(w.emitted('open')).toBeFalsy()
  })
})

describe('MobileSessionCard AI title', () => {
  it('shows AI title in cmd span for ai session', () => {
    const w = mount(MobileSessionCard, {
      props: {
        session: {
          session_id: 'a', host_id: 'h', host: 'mac', user: 'me',
          cwd: '/p', title: 'Improve sales order list styling',
          current_command: 'claude', type: 'ai',
          task_state: 'running', cols: 80, rows: 24,
        },
        home: '/Users/me',
      },
    })
    expect(w.get('.cmd').text()).toBe('Improve sales order list styling')
  })

  it('falls back to commandLabel for shell session', () => {
    const w = mount(MobileSessionCard, {
      props: {
        session: {
          session_id: 'a', host_id: 'h', host: 'mac', user: 'me',
          cwd: '/p', title: 'irrelevant', current_command: 'zsh',
          type: 'shell', task_state: 'idle', cols: 80, rows: 24,
        },
        home: '/Users/me',
      },
    })
    expect(w.get('.cmd').text()).toBe('zsh')
  })
})
