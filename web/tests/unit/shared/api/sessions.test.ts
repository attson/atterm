import { describe, it, expect, vi, beforeEach } from 'vitest'
import { listSessions } from '@shared/api/sessions'
import { clearCsrfToken } from '@shared/api/client'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('sessions /api/sessions', () => {
  beforeEach(() => {
    clearCsrfToken()
    vi.restoreAllMocks()
  })

  it('listSessions GETs /api/sessions and returns the array', async () => {
    const rows = [
      {
        id: '11111111-2222-3333-4444-555555555555',
        command: 'bash',
        cwd: '/home/me',
        title: '',
        cols: 80,
        rows: 24,
        started_at: 1700000000000,
        host_id: 'host-1',
        host: 'laptop.local',
        user: 'me',
      },
    ]
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, rows))
    vi.stubGlobal('fetch', fetchMock)

    const result = await listSessions()

    expect(result).toEqual(rows)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/sessions')
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method || 'GET').toBe('GET')
  })

  it('listSessions returns an empty array when server returns []', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, []))
    vi.stubGlobal('fetch', fetchMock)

    const result = await listSessions()

    expect(result).toEqual([])
  })
})
