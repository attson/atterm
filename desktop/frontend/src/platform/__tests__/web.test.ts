import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createWebPlatform } from '../web'
import { getCurrentAccountKey, setAccountKeyProvider } from '../../lib/account-key'
import { saveAccountKey } from '@webshared/api/account-key'

vi.mock('@webshared/api/version', () => ({
  fetchVersion: vi.fn().mockResolvedValue('v0.3.19'),
}))

vi.mock('@webshared/api/auth', () => ({
  logout: vi.fn().mockResolvedValue(undefined),
}))

describe('web platform', () => {
  afterEach(() => {
    setAccountKeyProvider(null)
    vi.restoreAllMocks()
  })

  it('caps: localPty=false autoUpdate=false pluginHost=false windowControls=false', () => {
    const p = createWebPlatform()
    expect(p.caps.localPty).toBe(false)
    expect(p.caps.autoUpdate).toBe(false)
    expect(p.caps.pluginHost).toBe(false)
    expect(p.caps.windowControls).toBe(false)
    expect(p.caps.systemClipboard).toBe(true)
    expect(p.caps.fileDialog).toBe(true)
    expect(p.caps.capacitor).toBe(false)
  })

  describe('relay + sessions bridges', () => {
    beforeEach(() => {
      localStorage.clear()
      sessionStorage.clear()
      setAccountKeyProvider(null)
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

    it('relay.logout delegates to the shared web auth logout helper', async () => {
      const { logout } = await import('@webshared/api/auth')
      await createWebPlatform().relay.logout?.()
      expect(logout).toHaveBeenCalledOnce()
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

    it('sessions.listRelaySessions fetches /api/me/sessions with bearer auth', async () => {
      localStorage.setItem('atterm.relay', JSON.stringify({ baseURL: 'https://relay.example', sessionToken: 'tok' }))
      const rows = [{ id_hash: 'h1', user_agent: 'UA', ip_prefix: '1.2.3', created_at: 1, expires_at: 2, is_current: true }]
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
        new Response(JSON.stringify(rows), { status: 200, headers: { 'Content-Type': 'application/json' } }),
      )
      await expect(createWebPlatform().sessions.listRelaySessions?.()).resolves.toEqual(rows)
      expect(fetchSpy).toHaveBeenCalledWith(
        'https://relay.example/api/me/sessions',
        expect.objectContaining({
          credentials: 'omit',
          headers: expect.any(Headers),
        }),
      )
      const headers = (fetchSpy.mock.calls[0][1] as RequestInit).headers as Headers
      expect(headers.get('Authorization')).toBe('Bearer tok')
    })

    it('sessions.revokeRelaySession DELETEs the encoded session hash', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }))
      await createWebPlatform().sessions.revokeRelaySession?.('h/1')
      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/me/sessions/h%2F1',
        expect.objectContaining({ method: 'DELETE', credentials: 'omit' }),
      )
    })

    it('sessions.signOutOtherRelaySessions POSTs and returns the deleted count', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
        new Response(JSON.stringify({ deleted: 3 }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
      )
      await expect(createWebPlatform().sessions.signOutOtherRelaySessions?.()).resolves.toEqual({ deleted: 3 })
      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/me/sessions/sign-out-others',
        expect.objectContaining({ method: 'POST', credentials: 'omit' }),
      )
    })

    it('registers the web account_key provider for shared terminal connections', () => {
      const key = new Uint8Array(32).map((_, i) => i)
      saveAccountKey(key)
      createWebPlatform()
      expect(getCurrentAccountKey()).toEqual(key)
    })
  })

  describe('events + templates + system bridges', () => {
    beforeEach(() => {
      localStorage.clear()
    })

    it('events on/emit/off roundtrip', () => {
      const p = createWebPlatform()
      const fn = vi.fn()
      const off = p.events.on('x', fn)
      p.events.emit('x', 'v')
      expect(fn).toHaveBeenCalledWith('v')
      off()
      p.events.emit('x', 'v2')
      expect(fn).toHaveBeenCalledTimes(1)
    })

    it('templates load/save/clear via localStorage', async () => {
      const p = createWebPlatform()
      await p.templates.save([{ id: '1', label: 'l1', text: 't1' }])
      const list = await p.templates.load()
      expect(list.length).toBe(1)
      expect(localStorage.getItem('atterm.quick_templates.value')).not.toBeNull()
      await p.templates.clear()
      expect(localStorage.getItem('atterm.quick_templates.value')).toBe('[]')
      expect(await p.templates.load()).toEqual([])
    })

    it('templates loadHidden/saveHidden via localStorage', async () => {
      const p = createWebPlatform()
      expect(await p.templates.loadHidden()).toBe(false)
      await p.templates.saveHidden(true)
      expect(await p.templates.loadHidden()).toBe(true)
      expect(localStorage.getItem('atterm.templates_hidden.value')).not.toBeNull()
    })

    it('auxKeys load/save/clear via localStorage', async () => {
      const p = createWebPlatform()
      await p.auxKeys.save([{ id: 'a1', label: 'esc', seq: '\x1b' }])
      const list = await p.auxKeys.load()
      expect(list).toEqual([{ id: 'a1', label: 'esc', seq: '\x1b' }])
      expect(localStorage.getItem('atterm.aux_keys.value')).not.toBeNull()
      await p.auxKeys.clear()
      expect(await p.auxKeys.load()).toEqual([])
    })

    it('system.getEnvironment returns buildType=web', async () => {
      const info = await createWebPlatform().system.getEnvironment()
      expect(info?.buildType).toBe('web')
    })

    it('system.openExternalURL calls window.open', async () => {
      const spy = vi.spyOn(window, 'open').mockImplementation(() => null)
      await createWebPlatform().system.openExternalURL('https://example.com')
      expect(spy).toHaveBeenCalledWith('https://example.com', '_blank', 'noopener')
      spy.mockRestore()
    })

    it('system.getClipboardPaste returns kind=none when navigator.clipboard is missing', async () => {
      const info = await createWebPlatform().system.getClipboardPaste()
      expect(info.kind).toBe('none')
    })

    it('system.getClipboardPaste reads text via navigator.clipboard.readText', async () => {
      const original = (navigator as any).clipboard
      Object.defineProperty(navigator, 'clipboard', {
        value: { readText: vi.fn().mockResolvedValue('pasted') },
        configurable: true,
      })
      const info = await createWebPlatform().system.getClipboardPaste()
      expect(info).toEqual({ kind: 'text', text: 'pasted' })
      Object.defineProperty(navigator, 'clipboard', { value: original, configurable: true })
    })

    it('system.getAppVersion delegates to @webshared/api/version fetchVersion', async () => {
      const { fetchVersion } = await import('@webshared/api/version')
      const version = await createWebPlatform().system.getAppVersion()
      expect(fetchVersion).toHaveBeenCalledOnce()
      expect(version).toBe('v0.3.19')
    })
  })
})
