// SW bridge: the service worker (notification-click.js) cannot read
// sessionStorage and has no crypto state of its own. When a push
// arrives carrying a base64 `sealedBody`, the SW asks any visible
// page client to decrypt via postMessage; this module installs the
// matching reply handler.
//
// The key never leaves the main thread; we only ship parsed
// plaintext fields back through the MessageChannel port the SW
// provides. Replies are best-effort — the SW races the request
// against a short timeout and falls back to the relay-composed
// plaintext title/body if no client answers in time.

import { loadAccountKey } from './api/account-key'
import { openPushBodyFields, type SealedPushBody } from './lib/opaque'

/** Request type tag — must match the string the SW sends in
 * web/public/notification-click.js. */
export const SW_DECRYPT_REQUEST_TYPE = 'atterm-decrypt-sealed-body'

interface DecryptRequest {
  type: typeof SW_DECRYPT_REQUEST_TYPE
  sessionId: string
  /** base64 std (matches Go's encoding/json default for []byte). */
  sealedBody: string
}

interface DecryptReply {
  fields: SealedPushBody | null
}

function b64ToBytes(s: string): Uint8Array {
  try {
    const norm = s.replace(/-/g, '+').replace(/_/g, '/')
    const pad = norm.length % 4 === 0 ? '' : '='.repeat(4 - (norm.length % 4))
    const bin = atob(norm + pad)
    const out = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
    return out
  } catch {
    return new Uint8Array(0)
  }
}

/** decryptForSW is the pure function the message listener delegates
 * to. Exposed so unit tests can exercise the decrypt path without
 * mocking navigator.serviceWorker. */
export function decryptForSW(req: DecryptRequest): DecryptReply {
  if (!req || typeof req.sessionId !== 'string' || typeof req.sealedBody !== 'string') {
    return { fields: null }
  }
  const ak = loadAccountKey()
  if (!ak) return { fields: null }
  const env = b64ToBytes(req.sealedBody)
  if (env.length === 0) return { fields: null }
  return { fields: openPushBodyFields(env, ak, req.sessionId) }
}

function isDecryptRequest(v: unknown): v is DecryptRequest {
  return (
    typeof v === 'object' &&
    v !== null &&
    (v as { type?: unknown }).type === SW_DECRYPT_REQUEST_TYPE
  )
}

/** installSWBridge wires the main-thread message listener that
 * services SW decrypt requests. Safe to call multiple times — the
 * listener is idempotent. No-op on engines without ServiceWorker. */
export function installSWBridge(): void {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return
  const sw = navigator.serviceWorker
  if (!sw || typeof sw.addEventListener !== 'function') return
  sw.addEventListener('message', (event) => {
    const data = (event as MessageEvent).data
    if (!isDecryptRequest(data)) return
    const port = (event as MessageEvent).ports?.[0]
    if (!port) return
    let reply: DecryptReply
    try {
      reply = decryptForSW(data)
    } catch {
      reply = { fields: null }
    }
    try {
      port.postMessage(reply)
    } catch {
      // port may already be closed if the SW gave up; nothing to do.
    }
  })
}
