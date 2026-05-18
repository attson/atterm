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
