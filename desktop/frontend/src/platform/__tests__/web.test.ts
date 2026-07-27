import { describe, it, expect, beforeEach } from 'vitest'
import { createWebPlatform } from '../web'

describe('web platform', () => {
  it('caps: localPty=false autoUpdate=false pluginHost=false windowControls=false', () => {
    const p = createWebPlatform()
    expect(p.caps.localPty).toBe(false)
    expect(p.caps.autoUpdate).toBe(false)
    expect(p.caps.pluginHost).toBe(false)
    expect(p.caps.windowControls).toBe(false)
    expect(p.caps.systemClipboard).toBe(true)
    expect(p.caps.fileDialog).toBe(true)
  })

  describe('relay + sessions bridges', () => {
    beforeEach(() => {
      localStorage.clear()
    })

    it('relay.load reads atterm.relay', () => {
      localStorage.setItem('atterm.relay', JSON.stringify({ baseURL: 'http://x', sessionToken: 't' }))
      const p = createWebPlatform()
      return expect(p.relay.load()).resolves.toMatchObject({ url: 'http://x', token: 't' })
    })

    it('relay.save writes atterm.relay with web shape', async () => {
      await createWebPlatform().relay.save({
        url: 'http://y',
        token: 'tok2',
        session_expires_at: 123,
        allow_insecure_relay: true,
        remote_permission: 'full',
        last_email: '',
        connected: false,
      })
      const raw = localStorage.getItem('atterm.relay')
      expect(raw).not.toBeNull()
      expect(JSON.parse(raw as string)).toMatchObject({
        baseURL: 'http://y',
        sessionToken: 'tok2',
        expiresAt: 123,
        allowInsecure: true,
      })
      // must NOT write to the mobile/Capacitor key
      expect(localStorage.getItem('atterm.relay.session')).toBeNull()
    })

    it('sessions.getPins reads pinned_session_ids from localStorage', () => {
      localStorage.setItem('atterm.pinned_session_ids.value', JSON.stringify(['a', 'b']))
      return expect(createWebPlatform().sessions.getPins()).resolves.toEqual(['a', 'b'])
    })

    it('sessions.setPins writes localStorage', async () => {
      await createWebPlatform().sessions.setPins(['x'])
      expect(localStorage.getItem('atterm.pinned_session_ids.value')).toBe('["x"]')
    })

    it('sessions.listShells returns empty', () => {
      return expect(createWebPlatform().sessions.listShells()).resolves.toEqual([])
    })
  })
})
