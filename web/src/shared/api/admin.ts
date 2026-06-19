import { apiFetch } from './client'
import type {
  AdminConfig,
  AdminConfigUpdate,
  AdminUserRow,
  FeishuAdminConfig,
  FeishuAdminConfigUpdate,
  InvitationCreateBatchResponse,
  InvitationCreated,
  InvitationRow,
  ResetPasswordResponse,
} from './types'

// User listing + role + status (admin/api/users).
export async function listUsers(): Promise<AdminUserRow[]> {
  const { data } = await apiFetch<AdminUserRow[]>('/admin/api/users')
  return data
}

export async function resetUserPassword(id: string): Promise<ResetPasswordResponse> {
  const { data } = await apiFetch<ResetPasswordResponse>(
    `/admin/api/users/${encodeURIComponent(id)}/reset-password`,
    { method: 'POST' },
  )
  return data
}

export async function disableUser(id: string): Promise<void> {
  await apiFetch(`/admin/api/users/${encodeURIComponent(id)}/disable`, { method: 'POST' })
}

export async function promoteUser(id: string): Promise<void> {
  await apiFetch(`/admin/api/users/${encodeURIComponent(id)}/admin`, { method: 'POST' })
}

export async function demoteUser(id: string): Promise<void> {
  await apiFetch(`/admin/api/users/${encodeURIComponent(id)}/admin`, { method: 'DELETE' })
}

// Invitations (admin/api/invitations).
export async function listInvitations(): Promise<InvitationRow[]> {
  const { data } = await apiFetch<InvitationRow[]>('/admin/api/invitations')
  return data
}

export interface CreateInvitationRequest {
  count: number
  note?: string
  expires_at?: string  // RFC3339; omit to let server default to now + 7 days.
}

// createInvitation normalises both server response shapes (single object
// when count==1, {invites: [...]} when count>1) to a flat array. Callers
// always get InvitationCreated[].
export async function createInvitation(req: CreateInvitationRequest): Promise<InvitationCreated[]> {
  const { data } = await apiFetch<InvitationCreated | InvitationCreateBatchResponse>(
    '/admin/api/invitations',
    { method: 'POST', body: JSON.stringify(req) },
  )
  if (data && typeof data === 'object' && 'invites' in data) {
    return data.invites
  }
  return [data as InvitationCreated]
}

// Runtime limits (admin/api/config).
export async function getAdminConfig(): Promise<AdminConfig> {
  const { data } = await apiFetch<AdminConfig>('/admin/api/config')
  return data
}

export async function setAdminConfig(update: AdminConfigUpdate): Promise<AdminConfig> {
  const { data } = await apiFetch<AdminConfig>('/admin/api/config', {
    method: 'PUT',
    body: JSON.stringify(update),
  })
  return data
}

// Feishu integration (admin/api/feishu).
export async function getFeishuAdminConfig(): Promise<FeishuAdminConfig> {
  const { data } = await apiFetch<FeishuAdminConfig>('/admin/api/feishu')
  return data
}

export async function setFeishuAdminConfig(update: FeishuAdminConfigUpdate): Promise<FeishuAdminConfig> {
  const { data } = await apiFetch<FeishuAdminConfig>('/admin/api/feishu', {
    method: 'PUT',
    body: JSON.stringify(update),
  })
  return data
}

// generateFeishuKey asks the server for a fresh base64 32-byte key. The caller
// must PUT it back to persist + apply.
export async function generateFeishuKey(): Promise<string> {
  const { data } = await apiFetch<{ encrypt_key: string }>('/admin/api/feishu/generate-key', {
    method: 'POST',
  })
  return data.encrypt_key
}
