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

// LoginResp is the new shape returned by /api/auth/login and /api/auth/signup
// in Phase 4 of the session-token migration. Phase 5 will retire the user_id
// and email fields here in favor of a more structured user object.
export interface LoginResp {
  session_token: string
  expires_at: number
  user: {
    id: string
    email: string
    is_admin?: boolean
  }
}

export interface VersionResponse {
  version: string
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
  debug: boolean
  debug_payload: boolean
  allowed_origins: string[]
  version: string
}

export interface AdminConfigUpdate {
  rate_limit_per_minute: number
  max_connections_per_key: number
  debug?: boolean
  debug_payload?: boolean
  allowed_origins?: string[]
}

// FeishuAdminConfig is the masked Feishu integration view (GET /admin/api/feishu).
// The plaintext encrypt key is never returned — only whether one is set.
export interface FeishuAdminConfig {
  enabled: boolean
  running: boolean
  base_url: string
  key_set: boolean
  key_last4?: string
}

// FeishuAdminConfigUpdate is the PUT body. Omit encrypt_key to keep the
// existing key (e.g. a plain enable/disable toggle); set force to rotate an
// existing key.
export interface FeishuAdminConfigUpdate {
  enabled: boolean
  encrypt_key?: string
  base_url?: string
  force?: boolean
}

export interface ResetPasswordResponse {
  plaintext: string
}

export interface SessionSummary {
  recent_output?: string
  error_lines?: string[]
  captured_at?: number
}

export interface SessionInfo {
  id: string
  command: string
  cwd: string
  title: string
  cols: number
  rows: number
  started_at: number  // unix ms (Go side emits int64 ms)
  host_id: string
  host: string
  user: string
  remote_permission?: string  // "view" | "control" | "full" (server omits when "full")
  task_state?: TaskState
  current_command?: string
  command_started_at?: number
  command_ended_at?: number
  command_duration_ms?: number
  command_exit_code?: number
  last_output_at?: number
  type?: string
  summary?: SessionSummary
  // sealed is the base64-encoded AEAD envelope produced by the agent
  // when E2EE is unlocked. Mirrors Go's proto.SessionInfo.Sealed
  // (encoding/json marshals []byte as standard base64 with padding).
  // Decryption happens client-side via openSessionFields in
  // @shared/lib/opaque; see api/sessions.ts for the wrapper.
  sealed?: string
}

export type TaskState =
  | 'idle'
  | 'running'
  | 'waiting_input'
  | 'completed'
  | 'failed'
  | 'disconnected'
  | 'closed'
