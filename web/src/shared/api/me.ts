import { apiFetch } from './client'
import { requestStepUpToken } from './stepup'
import type { MeResponse } from './types'

// getMe fetches the current user. The relay no longer issues CSRF
// tokens (session_token in Authorization: Bearer is sufficient
// proof-of-intent), so the historical csrf_token round-trip is gone.
export async function getMe(): Promise<MeResponse> {
  const { data } = await apiFetch<MeResponse>('/api/me')
  return data
}

// Password (settings → Change Password tab).
//
// This intentionally restores the pre-stepup shape: POST /api/me/password
// with { current_password, new_password }, no X-Step-Up-Token. Spec §4.4
// documents an OPAQUE step-up flow (see deleteMe below for the pattern), but
// the restored client uses the historic single-request form because that's
// what the desktop UI shipped before the admin/settings rewrite. The relay
// endpoint currently returns 410 on live, so this call fails end-to-end
// today — tracked as a follow-up to reintroduce the step-up handshake on
// both sides. describeAccountError in SettingsAccount.vue still has
// step_up_required / step_up_invalid branches so re-adding the token here
// won't need any UI churn.
export async function changePassword(
  current_password: string,
  new_password: string,
): Promise<void> {
  await apiFetch('/api/me/password', {
    method: 'POST',
    body: JSON.stringify({ current_password, new_password }),
  })
}

// Account deletion (settings → Danger zone tab). The relay's
// /api/me handler requires an X-Step-Up-Token issued by a fresh
// OPAQUE step-up handshake (M1i-enforce); we drive it here so the
// caller only needs to supply email + password.
export async function deleteMe(email: string, password: string): Promise<void> {
  const stepUpToken = await requestStepUpToken(email, password)
  await apiFetch('/api/me', {
    method: 'DELETE',
    headers: { 'X-Step-Up-Token': stepUpToken },
    body: JSON.stringify({ email }),
  })
}
