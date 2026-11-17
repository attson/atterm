import { describe, expect, it, vi } from 'vitest'

import { createMockPlatform } from './mockPlatform'
import { MockSocket } from './mockSocket'
import { encodeFrame, encodeText, TYPE, uuidParse } from '@/lib/proto'
import { localSessions, LOCAL_HOST } from './localSessions'

describe('mock platform shared app bridges', () => {
  it('provides direct-connection preferences and ordered snippet persistence', async () => {
    const platform = createMockPlatform()

    expect(await platform.directConnection.load()).toBe(false)
    await platform.directConnection.save(true)
    expect(await platform.directConnection.load()).toBe(true)

    const snippets = [
      { id: 'deploy', label: 'Deploy', text: 'make build\nmake deploy' },
      { id: 'logs', label: 'Logs', text: 'journalctl -f' },
    ]
    await platform.snippets.save(snippets)
    expect(await platform.snippets.load()).toEqual(snippets)
  })

  it('returns commands submitted through the local demo session as input history', async () => {
    const sessionID = '77777777-7777-4777-8777-777777777777'
    localSessions.push({
      id: sessionID, command: '/bin/zsh', cwd: '~', title: 'zsh', cols: 100, rows: 30,
      started_at: 1_753_900_000, host_id: LOCAL_HOST, host: LOCAL_HOST, user: 'you',
    })
    const platform = createMockPlatform()
    const socket = new MockSocket('ws://local.demo/client')
    await vi.waitFor(() => expect(socket.readyState).toBe(MockSocket.OPEN))

    socket.send(encodeFrame(TYPE.IN, uuidParse(sessionID), encodeText('help\r')))

    await expect(platform.sessions.getInputHistory?.(sessionID)).resolves.toMatchObject({
      items: [{ text: 'help' }],
      truncated: false,
    })
    await expect(platform.sessions.getInputHistory?.('00000000-0000-4000-8000-000000000000')).resolves.toEqual({
      items: [],
      truncated: false,
    })
    socket.close()
    localSessions.splice(localSessions.findIndex((session) => session.id === sessionID), 1)
  })
})
