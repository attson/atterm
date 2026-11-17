import { describe, it, expect, beforeEach } from 'vitest'
import { createMockGoApp, installMockGoApp, readMockClipboardPaste } from './mockGoApp'
import { localSessions, LOCAL_HOST } from './localSessions'

// 每个用例前把 localSessions 复位成只有预置的那一个,避免相互污染。
const PRESET_ID = '66666666-6666-4666-8666-666666666666'
beforeEach(() => {
  localSessions.length = 0
  localSessions.push({
    id: PRESET_ID, command: '/bin/zsh', cwd: '~', title: 'zsh', cols: 100, rows: 30,
    started_at: 1_753_900_000, host_id: LOCAL_HOST, host: LOCAL_HOST, user: 'you',
    task_state: 'idle', remote_permission: 'full',
  })
})

describe('mockGoApp local session lifecycle', () => {
  it('boot chain methods return non-throwing shapes', async () => {
    const app = createMockGoApp()
    expect((await app.GetEndpoint()).url).toContain('local')
    expect((await app.GetHostInfo()).host_id).toBe(LOCAL_HOST)
    expect((await app.LoadRecoverySnapshot()).tabs).toEqual([])
    expect(await app.GetRecoveryDialogEnabled()).toBe(false)
    expect((await app.GetRelayConfig()).connected).toBe(true)
    expect(await app.ListShells()).toContain('/bin/zsh')
    expect(await app.GetUplinkHealth()).toEqual({
      state: 'connected',
      rtt: { last_ms: 18, p50_ms: 18, p95_ms: 24 },
      rtt_samples: [],
      reconnect: { count_last_hour: 0, last_at_ms: null, last_reason: '', history: [] },
      bytes: { in_per_sec: 320, out_per_sec: 1280 },
      seq_gaps: 0,
    })
  })

  it('supports appearance changes and saved SSH hosts used by the real app UI', async () => {
    const app = createMockGoApp()
    await app.SetTerminalTheme('dracula')
    await app.SetTerminalFontHead('JetBrains Mono')
    await app.SetTerminalFontSize(16)
    await app.SetTerminalLineHeight(1.4)
    await app.SetTerminalCursorStyle('bar')
    await app.SetTerminalCursorBlink(false)
    await app.SetTerminalScrollback(5000)

    expect(await app.GetTerminalTheme()).toBe('dracula')
    expect(await app.GetTerminalFontHead()).toBe('JetBrains Mono')
    expect(await app.GetTerminalFontSize()).toBe(16)
    expect(await app.GetTerminalLineHeight()).toBe(1.4)
    expect(await app.GetTerminalCursorStyle()).toBe('bar')
    expect(await app.GetTerminalCursorBlink()).toBe(false)
    expect(await app.GetTerminalScrollback()).toBe(5000)
    await expect(app.ListSSHHosts()).resolves.toEqual(expect.arrayContaining([
      expect.objectContaining({ id: 'demo-staging', alias: 'staging', host: 'staging.example.com' }),
    ]))
  })

  it('adapts a browser clipboard image to the desktop paste payload', async () => {
    const image = new Blob(['hello'], { type: 'image/png' })
    const payload = await readMockClipboardPaste({
      read: async () => [{
        types: ['image/png'],
        getType: async () => image,
      }],
    })

    expect(payload).toEqual({
      kind: 'image',
      filename: 'clipboard-image.png',
      content_type: 'image/png',
      data_base64: 'aGVsbG8=',
    })

    const copiedFile = await readMockClipboardPaste({
      read: async () => [{ types: ['text/plain'], getType: async () => new Blob() }],
      readText: async () => 'file:///home/attson/Pictures/%E7%B4%A0%E6%9D%90/demo-shot.jpeg',
    })
    expect(copiedFile).toEqual({
      kind: 'image',
      filename: 'demo-shot.jpeg',
      content_type: 'image/png',
      data_base64: expect.stringMatching(/^iVBOR/),
    })
    if (copiedFile.kind !== 'image') throw new Error('expected image payload')
    const png = Buffer.from(copiedFile.data_base64, 'base64')
    expect(png.readUInt32BE(16)).toBe(240)
    expect(png.readUInt32BE(20)).toBe(160)

    await expect(readMockClipboardPaste({
      readText: async () => '/home/attson/Documents/release-notes.txt',
    })).resolves.toEqual({
      kind: 'text',
      text: '/home/attson/Documents/release-notes.txt',
    })
  })

  it('NewSession appends a local session', async () => {
    const app = createMockGoApp()
    const before = localSessions.length
    const { session_id } = await app.NewSession({ command: '/bin/zsh', cwd: '~', cols: 80, rows: 24 })
    expect(localSessions.length).toBe(before + 1)
    expect(session_id).toMatch(/^[0-9a-f-]{36}$/)
    expect(localSessions.find((s) => s.id === session_id)?.host).toBe(LOCAL_HOST)
  })

  it('CloseSession removes a local session', async () => {
    const app = createMockGoApp()
    await app.CloseSession(PRESET_ID)
    expect(localSessions.find((s) => s.id === PRESET_ID)).toBeUndefined()
  })

  it('restores the previous global Wails binding after the demo unmounts', () => {
    const previous = { sentinel: true }
    const g = globalThis as any
    g.go = { main: { App: previous } }

    const restore = installMockGoApp()
    expect(g.go.main.App).not.toBe(previous)

    restore()
    expect(g.go.main.App).toBe(previous)
  })
})
