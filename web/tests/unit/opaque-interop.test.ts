// Cross-client OPAQUE interop test: drives the SAME bytemare WASM client the
// browser uses (@shared/lib/opaqueWasm, loaded here via the node helper)
// against a real running atterm-relay exposed via $ATTERM_RELAY_URL. This is
// the gap that hid the cloudflare<->bytemare incompatibility: it proves the web
// client interoperates with the Go relay/desktop.
//
// HOW TO RUN
//   1. Build the wasm: GOOS=js GOARCH=wasm go build -o web/src/shared/lib/opaque.wasm ./cmd/opaque-wasm
//      and copy $(go env GOROOT)/{misc,lib}/wasm/wasm_exec.js to web/src/shared/lib/
//      (scripts/build-web.sh does both).
//   2. Start an atterm-relay (e.g. ./atterm-relay --addr :7099 --https-addr "").
//   3. ATTERM_RELAY_URL=http://localhost:7099 npx vitest run tests/unit/opaque-interop.test.ts
//   The cross-CLIENT proof (web register -> desktop login) lives in the CI
//   shell step + internal/e2eeclient; this test proves web client <-> relay.

import { describe, it, expect } from 'vitest'
import {
  SERVER_IDENTITY,
  defaultKDFParams,
  unwrapWithPassword,
  wrapAccountKey,
} from '@shared/lib/opaque'
import { loadWasmOpaque, expectOk } from '../helpers/wasmNode'

const RELAY_URL = process.env.ATTERM_RELAY_URL
const CLAIM_TOKEN = process.env.ATTERM_OPAQUE_CLAIM_TOKEN ?? ''
const describeIfRelay = RELAY_URL ? describe : describe.skip

function freshEmail(): string {
  return `interop-${Math.random().toString(36).slice(2, 10)}@example.com`
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const r = await fetch(`${RELAY_URL}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) throw new Error(`${path} → ${r.status} ${await r.text()}`)
  return (await r.json()) as T
}

describeIfRelay('OPAQUE interop: WASM (bytemare) client ↔ Go relay', () => {
  const email = freshEmail()
  const password = 'hunter2-interop'
  const accountKey = new Uint8Array(32)
  crypto.getRandomValues(accountKey)

  it('registers + logs in via the WASM client; account_key round-trips', async () => {
    const O = await loadWasmOpaque()

    // ---------- REGISTRATION ----------
    const ri = expectOk(O.registerInit(password))
    const regInit = await post<{ registration_response: string }>('/api/auth/register/init', {
      email,
      registration_ke: ri.ke1,
    })
    const rf = expectOk(O.registerFinish(ri.handle, regInit.registration_response, SERVER_IDENTITY, email))
    const wrap = wrapAccountKey(password, accountKey, defaultKDFParams())
    const finBody: Record<string, unknown> = {
      email,
      registration_record: rf.record,
      account_key_wrap: wrap,
    }
    if (CLAIM_TOKEN) finBody.claim_token = CLAIM_TOKEN
    const regFinal = await post<{ user_id: string }>('/api/auth/register/finalize', finBody)
    expect(regFinal.user_id).toBeTruthy()

    // ---------- LOGIN ----------
    const li = expectOk(O.loginInit(password))
    const logInit = await post<{ login_response: string; session_id: string }>('/api/auth/login/init', {
      email,
      login_ke: li.ke1,
    })
    const lf = expectOk(O.loginFinish(li.handle, logInit.login_response, SERVER_IDENTITY, email))
    const logFinal = await post<{ user_id: string; account_key_wrap: Parameters<typeof unwrapWithPassword>[1] }>(
      '/api/auth/login/finalize',
      { email, session_id: logInit.session_id, login_ke3: lf.ke3 },
    )
    expect(logFinal.user_id).toBe(regFinal.user_id)

    // ---------- UNWRAP ----------
    const recovered = unwrapWithPassword(password, logFinal.account_key_wrap)
    expect(recovered).toEqual(accountKey)
  }, 30_000)
})
