import { describe, it, expect, beforeEach } from 'vitest'
import { createMockGoApp } from './mockGoApp'
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
})
