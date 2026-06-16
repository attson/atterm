// OPAQUE step-up handshake driver. Sensitive endpoints (DELETE /api/me,
// password change, admin reset) require a fresh step_up_token issued by
// /api/auth/stepup/init + /finalize. requestStepUpToken() runs both
// stages and returns the token; callers attach it as X-Step-Up-Token on
// the actual request.
//
// The protocol bytes are identical to login init/finalize — this file
// just hits different endpoints. Keep in sync with auth.ts.

import { apiFetch } from './client'
import { KE2 } from '@cloudflare/opaque-ts'
import {
  SERVER_IDENTITY,
  getOpaqueConfig,
  newOpaqueClient,
} from '@shared/lib/opaque'

interface StepUpInitResp {
  login_response: string
  session_id: string
}
interface StepUpFinalizeResp {
  user_id: string
  step_up_token: string
  expires_in_sec: number
}

function bytesToB64Std(b: Uint8Array): string {
  return btoa(String.fromCharCode(...b))
}

function b64StdToBytes(s: string): Uint8Array {
  const bin = atob(s)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

function numberArrayToB64(b: number[]): string {
  return bytesToB64Std(new Uint8Array(b))
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
  const cfg = getOpaqueConfig()
  const client = newOpaqueClient()

  const ke1OrErr = await client.authInit(password)
  if (ke1OrErr instanceof Error) throw ke1OrErr

  const init = await postJSON<StepUpInitResp>('/api/auth/stepup/init', {
    email,
    login_ke: numberArrayToB64(ke1OrErr.serialize()),
  })

  const ke2Bytes = Array.from(b64StdToBytes(init.login_response))
  const ke2 = KE2.deserialize(cfg, ke2Bytes)

  const finishOrErr = await client.authFinish(ke2, SERVER_IDENTITY, email)
  if (finishOrErr instanceof Error) {
    throw new Error('invalid credentials')
  }

  const final = await postJSON<StepUpFinalizeResp>('/api/auth/stepup/finalize', {
    email,
    session_id: init.session_id,
    login_ke3: numberArrayToB64(finishOrErr.ke3.serialize()),
  })
  return final.step_up_token
}
