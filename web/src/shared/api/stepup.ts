// OPAQUE step-up handshake driver. Sensitive endpoints (DELETE /api/me,
// password change, admin reset) require a fresh step_up_token issued by
// /api/auth/stepup/init + /finalize. requestStepUpToken() runs both
// stages and returns the token; callers attach it as X-Step-Up-Token on
// the actual request.
//
// The protocol bytes are identical to login init/finalize — this file
// just hits different endpoints. Keep in sync with auth.ts.

import { apiFetch } from './client'
import { SERVER_IDENTITY } from '@shared/lib/opaque'
import { opaqueLoginInit, opaqueLoginFinish } from '@shared/lib/opaqueWasm'

interface StepUpInitResp {
  login_response: string
  session_id: string
}
interface StepUpFinalizeResp {
  user_id: string
  step_up_token: string
  expires_in_sec: number
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const { data } = await apiFetch<T>(path, {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return data
}

/** Run a full OPAQUE step-up handshake for the given credentials and
 * return the short-lived step_up_token. The TS client detects wrong
 * password during the KE2 MAC check; both that and a server-side 401
 * surface as the stable string 'invalid credentials'.
 *
 * Throws on network / 5xx; the caller is expected to surface a generic
 * "couldn't verify identity" message and let the user retry. */
export async function requestStepUpToken(
  email: string,
  password: string,
): Promise<string> {
  const { handle, ke1 } = await opaqueLoginInit(password)

  const init = await postJSON<StepUpInitResp>('/api/auth/stepup/init', {
    email,
    login_ke: ke1,
  })

  let ke3: string
  try {
    ;({ ke3 } = await opaqueLoginFinish(handle, init.login_response, SERVER_IDENTITY, email))
  } catch {
    throw new Error('invalid credentials')
  }

  const final = await postJSON<StepUpFinalizeResp>('/api/auth/stepup/finalize', {
    email,
    session_id: init.session_id,
    login_ke3: ke3,
  })
  return final.step_up_token
}
