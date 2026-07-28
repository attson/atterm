// shared/api/push-flow.ts — orchestrated enable/disable for web push.
// Dependencies (Notification, ServiceWorkerRegistration) are injected so
// vitest can drive each branch with plain object mocks. Low-level HTTP
// shape lives in ./push.

import { getPushKey, subscribePush, unsubscribePush, type PushSubscriptionPayload } from './push'

export type EnableReason = 'denied' | 'disabled' | 'key-failed' | 'subscribe-failed' | 'subscribe-rejected'

export interface EnableDeps {
  notification: { permission: NotificationPermission; requestPermission: () => Promise<NotificationPermission> }
  registration: ServiceWorkerRegistration
}

export interface DisableDeps {
  registration: ServiceWorkerRegistration
}

export type EnableResult = { ok: true } | { ok: false; reason: EnableReason }

function base64UrlToUint8Array(input: string): Uint8Array {
  // Drop chars until length%4 ∈ {0,2,3} — length%4===1 is never a valid base64
  // tail and would throw in strict decoders like jsdom/happy-dom.
  let s = input.replace(/-/g, '+').replace(/_/g, '/')
  while (s.length % 4 === 1) s = s.slice(0, -1)
  const pad = '='.repeat((4 - (s.length % 4)) % 4)
  const raw = atob(s + pad)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

export async function enablePushFlow(deps: EnableDeps): Promise<EnableResult> {
  if (deps.notification.permission === 'denied') return { ok: false, reason: 'denied' }
  if (deps.notification.permission !== 'granted') {
    const next = await deps.notification.requestPermission()
    if (next !== 'granted') return { ok: false, reason: 'denied' }
  }

  let key: string
  try {
    key = await getPushKey()
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    if (/disabled/i.test(msg)) return { ok: false, reason: 'disabled' }
    return { ok: false, reason: 'key-failed' }
  }

  let sub: PushSubscription
  try {
    sub = await deps.registration.pushManager.subscribe({
      userVisibleOnly: true,
      // TS 5.7+ made Uint8Array generic over its backing buffer, which no
      // longer structurally matches BufferSource's ArrayBuffer-only view
      // under desktop/frontend's newer TypeScript (this file type-checks
      // clean on web's older compiler without the cast). PushManager
      // accepts any BufferSource at runtime; only the type needs narrowing.
      applicationServerKey: base64UrlToUint8Array(key) as BufferSource,
    })
  } catch {
    return { ok: false, reason: 'subscribe-failed' }
  }

  const asJson = (sub as unknown as { toJSON?: () => PushSubscriptionPayload }).toJSON?.()
  const payload = (asJson ?? (sub as unknown as PushSubscriptionPayload))
  try {
    await subscribePush({
      endpoint: payload.endpoint,
      keys: { p256dh: payload.keys.p256dh, auth: payload.keys.auth },
    })
  } catch {
    return { ok: false, reason: 'subscribe-rejected' }
  }
  return { ok: true }
}

export async function disablePushFlow(deps: DisableDeps): Promise<{ ok: true }> {
  const sub = await deps.registration.pushManager.getSubscription()
  if (sub) {
    try {
      await sub.unsubscribe()
    } catch {
      // browser-side unsubscribe failures are not fatal — the relay copy is the authoritative state
    }
    try {
      await unsubscribePush(sub.endpoint)
    } catch {
      // ditto
    }
  }
  return { ok: true }
}
