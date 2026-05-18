import { describe, it, expect, vi, beforeEach } from 'vitest'

const fetchMock = vi.fn()
;(globalThis as { fetch: typeof fetch }).fetch = fetchMock as unknown as typeof fetch

import { getPushKey, subscribePush, unsubscribePush, testPush } from '@shared/api/push'

function jsonOk(body: unknown) {
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
}

function jsonError(status: number, body: unknown) {
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
}

describe('push api', () => {
  beforeEach(() => {
    fetchMock.mockReset()
  })

  it('getPushKey returns the VAPID key string', async () => {
    fetchMock.mockReturnValueOnce(jsonOk({ key: 'BLAH' }))
    expect(await getPushKey()).toBe('BLAH')
    expect(fetchMock).toHaveBeenCalledWith('/api/push/key', expect.objectContaining({ credentials: 'same-origin' }))
  })

  it('subscribePush POSTs JSON body with endpoint + keys', async () => {
    fetchMock.mockReturnValueOnce(jsonOk({ ok: true }))
    await subscribePush({ endpoint: 'https://example/abc', keys: { p256dh: 'aaa', auth: 'bbb' } })
    const body = JSON.parse((fetchMock.mock.calls[0]![1] as RequestInit).body as string)
    expect(body).toEqual({ endpoint: 'https://example/abc', keys: { p256dh: 'aaa', auth: 'bbb' } })
  })

  it('unsubscribePush POSTs only the endpoint', async () => {
    fetchMock.mockReturnValueOnce(jsonOk({ ok: true }))
    await unsubscribePush('https://example/abc')
    const body = JSON.parse((fetchMock.mock.calls[0]![1] as RequestInit).body as string)
    expect(body).toEqual({ endpoint: 'https://example/abc' })
  })

  it('testPush returns the sent count', async () => {
    fetchMock.mockReturnValueOnce(jsonOk({ sent: 3 }))
    expect(await testPush()).toBe(3)
  })

  it('getPushKey throws ApiError on 503 (push disabled)', async () => {
    fetchMock.mockReturnValueOnce(jsonError(503, { error: 'web push disabled' }))
    await expect(getPushKey()).rejects.toThrow(/web push disabled/i)
  })
})
