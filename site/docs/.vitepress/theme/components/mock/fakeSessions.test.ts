import { describe, it, expect } from 'vitest'
import { fakeSessions, IDLE_SESSION_ID } from './fakeSessions'

describe('fakeSessions', () => {
  it('covers all task states', () => {
    const states = fakeSessions.map((s) => s.task_state)
    expect(states).toContain('running')
    expect(states).toContain('waiting_input')
    expect(states).toContain('completed')
    expect(states).toContain('failed')
    expect(states).toContain('idle')
  })

  it('every session has a 36-char uuid session_id and required fields', () => {
    for (const s of fakeSessions) {
      expect(s.session_id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/)
      expect(typeof s.title).toBe('string')
      expect(typeof s.host).toBe('string')
      expect(s.cols).toBeGreaterThan(0)
      expect(s.rows).toBeGreaterThan(0)
    }
  })

  it('spans two hosts', () => {
    const hosts = new Set(fakeSessions.map((s) => s.host))
    expect(hosts.size).toBe(2)
  })

  it('exposes the idle session id present in the list', () => {
    expect(fakeSessions.some((s) => s.session_id === IDLE_SESSION_ID)).toBe(true)
    expect(fakeSessions.find((s) => s.session_id === IDLE_SESSION_ID)?.task_state).toBe('idle')
  })
})
