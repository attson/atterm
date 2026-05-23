import { apiFetch } from './client'
import type { WebhookRow } from './types'

export async function listWebhooks(): Promise<WebhookRow[]> {
  const { data } = await apiFetch<WebhookRow[]>('/api/me/webhooks')
  return data
}

export async function createWebhook(input: {
  url: string
  format: 'feishu' | 'generic'
  name: string
  allow_insecure: boolean
}): Promise<WebhookRow> {
  const { data } = await apiFetch<WebhookRow>('/api/me/webhooks', {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return data
}

export async function deleteWebhook(id: string): Promise<void> {
  await apiFetch(`/api/me/webhooks/${encodeURIComponent(id)}`, { method: 'DELETE' })
}
