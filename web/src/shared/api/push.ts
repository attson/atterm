import { apiFetch } from './client'

export interface PushSubscriptionPayload {
  endpoint: string
  keys: {
    p256dh: string
    auth: string
  }
}

export async function getPushKey(): Promise<string> {
  const { data } = await apiFetch<{ key: string }>('/api/push/key')
  return data.key
}

export async function subscribePush(sub: PushSubscriptionPayload): Promise<void> {
  await apiFetch<{ ok: boolean }>('/api/push/subscribe', {
    method: 'POST',
    body: JSON.stringify(sub),
  })
}

export async function unsubscribePush(endpoint: string): Promise<void> {
  await apiFetch<{ ok: boolean }>('/api/push/unsubscribe', {
    method: 'POST',
    body: JSON.stringify({ endpoint }),
  })
}

export async function testPush(): Promise<number> {
  const { data } = await apiFetch<{ sent: number }>('/api/push/test', { method: 'POST' })
  return data.sent
}
