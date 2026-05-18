export interface MeResponse {
  user_id: string
  email: string
  is_admin?: boolean
  csrf_token?: string
}

export interface AuthSuccess {
  user_id: string
  email: string
}

export interface VersionResponse {
  version: string
}

export interface ApiTokenRow {
  id: string
  name: string
  prefix: string
  created_at: string
  last_used_at?: string
  revoked_at?: string
}

export interface ApiTokenCreated {
  id: string
  plaintext: string
  prefix: string
  created_at: string
}

export interface SessionRow {
  id_hash: string
  user_agent: string
  ip_prefix: string
  created_at: number  // unix ms
  expires_at: number  // unix ms
  is_current: boolean
}

export interface SignOutOthersResponse {
  deleted: number
}

export interface AdminUserRow {
  id: string
  email: string
  created_at: string
  disabled_at?: string
  is_admin: boolean
}

export interface InvitationRow {
  code_prefix: string
  note: string
  created_at: string
  expires_at?: string
  consumed_at?: string
  consumed_by?: string
}

export interface InvitationCreated {
  plaintext: string
  code_prefix: string
  note: string
  expires_at?: string
  created_at: string
}

// /admin/api/invitations returns InvitationCreated when count == 1 and
// {invites: InvitationCreated[]} when count > 1. The helper normalises
// both shapes into an InvitationCreated[].
export interface InvitationCreateBatchResponse {
  invites: InvitationCreated[]
}

export interface AdminConfig {
  rate_limit_per_minute: number
  max_connections_per_key: number
  default_rate_limit_per_minute: number
  default_max_connections_per_key: number
  version: string
}

export interface AdminConfigUpdate {
  rate_limit_per_minute: number
  max_connections_per_key: number
}

export interface ResetPasswordResponse {
  plaintext: string
}
