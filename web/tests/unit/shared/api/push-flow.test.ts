import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@shared/api/push', () => ({
  getPushKey: vi.fn(),
  subscribePush: vi.fn(),
  unsubscribePush: vi.fn(),
  testPush: vi.fn(),
}))

import { enablePushFlow, disablePushFlow } from '@shared/api/push-flow'
import { getPushKey, subscribePush, unsubscribePush } from '@shared/api/push'

function makeRegistration(opts: { subscribe?: () => Promise<unknown>; getSubscription?: () => Promise<unknown> }) {
  return {
    pushManager: {
      subscribe: opts.subscribe ?? vi.fn(),
      getSubscription: opts.getSubscription ?? vi.fn(),
    },
  } as unknown as ServiceWorkerRegistration
}

function makeNotification(perm: NotificationPermission, request?: () => Promise<NotificationPermission>): { permission: NotificationPermission; requestPermission: () => Promise<NotificationPermission> } {
  return {
    permission: perm,
    requestPermission: request ?? (() => Promise.resolve(perm)),
  }
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('enablePushFlow', () => {
  it('returns ok=true on happy path', async () => {
    ;(getPushKey as ReturnType<typeof vi.fn>).mockResolvedValue('VAPID')
    ;(subscribePush as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const sub = {
      endpoint: 'https://example/abc',
      toJSON: () => ({ endpoint: 'https://example/abc', keys: { p256dh: 'aaa', auth: 'bbb' } }),
    }
    const reg = makeRegistration({ subscribe: () => Promise.resolve(sub) })

    const result = await enablePushFlow({
      notification: makeNotification('granted'),
      registration: reg,
    })
    expect(result).toEqual({ ok: true })
    expect(subscribePush).toHaveBeenCalledWith({ endpoint: 'https://example/abc', keys: { p256dh: 'aaa', auth: 'bbb' } })
  })

  it('returns ok=false reason=denied when user rejects the permission prompt', async () => {
    const result = await enablePushFlow({
      notification: makeNotification('default', () => Promise.resolve('denied')),
      registration: makeRegistration({}),
    })
    expect(result).toEqual({ ok: false, reason: 'denied' })
  })

  it('returns reason=denied when permission is already permanently denied', async () => {
    const result = await enablePushFlow({
      notification: makeNotification('denied'),
      registration: makeRegistration({}),
    })
    expect(result).toEqual({ ok: false, reason: 'denied' })
  })

  it('returns reason=disabled when getPushKey throws "web push disabled"', async () => {
    ;(getPushKey as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('web push disabled'))
    const result = await enablePushFlow({
      notification: makeNotification('granted'),
      registration: makeRegistration({}),
    })
    expect(result).toEqual({ ok: false, reason: 'disabled' })
  })

  it('returns reason=subscribe-failed when pushManager.subscribe throws', async () => {
    ;(getPushKey as ReturnType<typeof vi.fn>).mockResolvedValue('VAPID')
    const reg = makeRegistration({ subscribe: () => Promise.reject(new Error('boom')) })
    const result = await enablePushFlow({
      notification: makeNotification('granted'),
      registration: reg,
    })
    expect(result).toEqual({ ok: false, reason: 'subscribe-failed' })
  })

  it('returns reason=subscribe-rejected when relay POST fails', async () => {
    ;(getPushKey as ReturnType<typeof vi.fn>).mockResolvedValue('VAPID')
    ;(subscribePush as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('500'))
    const sub = {
      endpoint: 'https://example/abc',
      toJSON: () => ({ endpoint: 'https://example/abc', keys: { p256dh: 'aaa', auth: 'bbb' } }),
    }
    const reg = makeRegistration({ subscribe: () => Promise.resolve(sub) })

    const result = await enablePushFlow({
      notification: makeNotification('granted'),
      registration: reg,
    })
    expect(result).toEqual({ ok: false, reason: 'subscribe-rejected' })
  })
})

describe('disablePushFlow', () => {
  it('unsubscribes locally and POSTs /api/push/unsubscribe', async () => {
    const unsubscribe = vi.fn().mockResolvedValue(true)
    const reg = makeRegistration({
      getSubscription: () => Promise.resolve({
        endpoint: 'https://example/abc',
        unsubscribe,
      }),
    })

    const result = await disablePushFlow({ registration: reg })
    expect(result).toEqual({ ok: true })
    expect(unsubscribe).toHaveBeenCalled()
    expect(unsubscribePush).toHaveBeenCalledWith('https://example/abc')
  })

  it('returns ok=true even when there is no existing subscription', async () => {
    const reg = makeRegistration({ getSubscription: () => Promise.resolve(null) })
    const result = await disablePushFlow({ registration: reg })
    expect(result).toEqual({ ok: true })
    expect(unsubscribePush).not.toHaveBeenCalled()
  })
})
