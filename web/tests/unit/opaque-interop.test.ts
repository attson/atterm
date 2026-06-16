// Cross-language OPAQUE interop test. Speaks the same protocol the Go
// SDK in internal/e2eeclient drives, against a real running atterm-relay
// exposed via $ATTERM_RELAY_URL.
//
// HOW TO RUN
//   1. Start an atterm-relay in another terminal with a dev config:
//        ATTERM_RELAY_LISTEN=:7099 \
//        ATTERM_RELAY_BOOTSTRAP_ADMIN_EMAIL=interop@local \
//        ATTERM_RELAY_DEBUG=1 \
//        ./atterm-relay
//      The first boot prints a claim token; you only need it if you
//      want the registered user to be admin, this test does not.
//   2. Either set ATTERM_OPAQUE_CLAIM_TOKEN to that printed token, or
//      leave it unset (the test will register a regular user).
//   3. Set ATTERM_RELAY_URL and run the test:
//        ATTERM_RELAY_URL=http://localhost:7099 \
//        npx vitest run tests/unit/opaque-interop.test.ts
//
// CI INTEGRATION (follow-up): a small shell wrapper in
// .github/workflows/build.yaml can `go build cmd/atterm-relay`, launch
// it on a free port, set ATTERM_RELAY_URL, and run this test. Until
// that wraps up the test SKIPS when the env var is missing so normal
// `npm test` runs stay green.

import { describe, it, expect } from 'vitest'
import {
  OpaqueClient,
  RegistrationResponse,
  KE2,
  ScryptMemHardFn,
} from '@cloudflare/opaque-ts'
import {
  SERVER_IDENTITY,
  defaultKDFParams,
  getOpaqueConfig,
  newOpaqueClient,
  unwrapWithPassword,
  wrapAccountKey,
  type AccountKeyWrap,
} from '@shared/lib/opaque'

const RELAY_URL = process.env.ATTERM_RELAY_URL
const CLAIM_TOKEN = process.env.ATTERM_OPAQUE_CLAIM_TOKEN ?? ''

const describeIfRelay = RELAY_URL ? describe : describe.skip

// Test fixtures use a fresh random email per run so successive
// invocations against the same relay don't collide. The relay's
// register/finalize returns 409 on email-taken.
function freshEmail(): string {
  const suffix = Math.random().toString(36).slice(2, 10)
  return `interop-${suffix}@example.com`
}

function bytesToB64Std(b: Uint8Array): string {
  return btoa(String.fromCharCode(...b))
}

function numberArrayToB64(b: number[]): string {
  return bytesToB64Std(new Uint8Array(b))
}

async function relayPost<T>(path: string, body: unknown): Promise<T> {
  const r = await fetch(`${RELAY_URL}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) {
    const text = await r.text()
    throw new Error(`${path} → ${r.status} ${text}`)
  }
  return (await r.json()) as T
}

interface RegisterInitResp {
  registration_response: string
}
interface RegisterFinalizeResp {
  user_id: string
  session_token: string
}
interface LoginInitResp {
  login_response: string
  session_id: string
}
interface LoginFinalizeResp {
  user_id: string
  session_token: string
  account_key_wrap: AccountKeyWrap
}

describeIfRelay('OPAQUE interop: TS client ↔ Go relay', () => {
  const email = freshEmail()
  const password = 'hunter2-interop'
  // Random 32-byte account_key. The server stores its wrap blob blindly
  // and returns it at login; we then unwrap locally to assert that the
  // bytes survived the round trip.
  const accountKey = new Uint8Array(32)
  crypto.getRandomValues(accountKey)

  it('registers + logs in via OPAQUE round-trip; recovered account_key matches', async () => {
    const cfg = getOpaqueConfig()
    // ---------- REGISTRATION ----------
    const client = newOpaqueClient()
    const ke1OrErr = await client.registerInit(password)
    if (ke1OrErr instanceof Error) throw ke1OrErr
    const ke1 = ke1OrErr

    const initResp = await relayPost<RegisterInitResp>(
      '/api/auth/register/init',
      { email, registration_ke: numberArrayToB64(ke1.serialize()) },
    )
    expect(initResp.registration_response.length).toBeGreaterThan(0)

    const ke2Bytes = Array.from(b64Decode(initResp.registration_response))
    const ke2 = RegistrationResponse.deserialize(cfg, ke2Bytes)

    const finOrErr = await client.registerFinish(ke2, SERVER_IDENTITY, email)
    if (finOrErr instanceof Error) throw finOrErr
    const { record } = finOrErr

    const wrap = wrapAccountKey(password, accountKey, defaultKDFParams())
    const finBody: Record<string, unknown> = {
      email,
      registration_record: numberArrayToB64(record.serialize()),
      account_key_wrap: wrap,
    }
    if (CLAIM_TOKEN) finBody.claim_token = CLAIM_TOKEN
    const finResp = await relayPost<RegisterFinalizeResp>(
      '/api/auth/register/finalize',
      finBody,
    )
    expect(finResp.user_id).toBeTruthy()
    expect(finResp.session_token).toBeTruthy()
    const registeredUserID = finResp.user_id

    // ---------- LOGIN ----------
    const client2 = newOpaqueClient()
    const ke1LoginOrErr = await client2.authInit(password)
    if (ke1LoginOrErr instanceof Error) throw ke1LoginOrErr
    const ke1Login = ke1LoginOrErr

    const loginInit = await relayPost<LoginInitResp>('/api/auth/login/init', {
      email,
      login_ke: numberArrayToB64(ke1Login.serialize()),
    })
    expect(loginInit.session_id).toBeTruthy()

    const ke2LoginBytes = Array.from(b64Decode(loginInit.login_response))
    const ke2Login = KE2.deserialize(cfg, ke2LoginBytes)

    const finishOrErr = await client2.authFinish(ke2Login, SERVER_IDENTITY, email)
    if (finishOrErr instanceof Error) throw finishOrErr
    const { ke3 } = finishOrErr

    const loginFinal = await relayPost<LoginFinalizeResp>(
      '/api/auth/login/finalize',
      {
        email,
        session_id: loginInit.session_id,
        login_ke3: numberArrayToB64(ke3.serialize()),
      },
    )
    expect(loginFinal.user_id).toBe(registeredUserID)
    expect(loginFinal.session_token).toBeTruthy()
    expect(loginFinal.account_key_wrap).toBeDefined()

    // ---------- UNWRAP ----------
    const recovered = unwrapWithPassword(password, loginFinal.account_key_wrap)
    expect(recovered).toEqual(accountKey)
  }, 30_000)
})

function b64Decode(s: string): Uint8Array {
  const bin = atob(s)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}
