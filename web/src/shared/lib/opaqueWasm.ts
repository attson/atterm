// Browser-side OPAQUE client backed by the Go bytemare/opaque module compiled
// to WASM (see cmd/opaque-wasm). This replaces @cloudflare/opaque-ts, which is
// NOT cross-client interoperable with the Go desktop + relay (different client
// AKE keypair derivation). The WASM speaks the exact same protocol bytes as the
// desktop, so an account registered here can be logged into from the desktop
// and vice-versa.
//
// All byte payloads are standard-base64 strings — the SAME shape as the wire
// fields (registration_ke, registration_response, registration_record,
// login_ke, login_response, login_ke3) — so the api layer passes them through
// without re-encoding. The account-key AEAD wrap (Argon2id + XChaCha20) and
// session-content decrypt stay in JS (noble) in ./opaque.ts; the WASM only runs
// the OPAQUE protocol crypto + message (de)serialization.
//
// Loading is lazy + singleton: the ~1MB (gzip) binary is fetched only when an
// auth action first happens, never at page boot.

interface RegisterInitResult {
  handle: number
  ke1: string
}
interface RegisterFinishResult {
  record: string
  exportKey: string
}
interface LoginInitResult {
  handle: number
  ke1: string
}
interface LoginFinishResult {
  ke3: string
  exportKey: string
  sessionKey: string
}
type MaybeError<T> = T | { error: string }

interface AttermOpaque {
  registerInit(password: string): MaybeError<RegisterInitResult>
  registerFinish(handle: number, ke2: string, serverId: string, clientId: string): MaybeError<RegisterFinishResult>
  loginInit(password: string): MaybeError<LoginInitResult>
  loginFinish(handle: number, ke2: string, serverId: string, clientId: string): MaybeError<LoginFinishResult>
}

let readyPromise: Promise<AttermOpaque> | null = null

// loadGoRuntime injects Go's wasm_exec.js (a classic script that defines the
// `Go` global). Browser-only; tests that exercise the wasm use a node helper.
async function loadGoRuntime(): Promise<void> {
  if ((globalThis as Record<string, unknown>).Go) return
  if (typeof document === 'undefined') {
    throw new Error('opaque wasm: no DOM to load the Go runtime')
  }
  const { default: execUrl } = await import('./wasm_exec.js?url')
  await new Promise<void>((resolve, reject) => {
    const s = document.createElement('script')
    s.src = execUrl
    s.onload = () => resolve()
    s.onerror = () => reject(new Error('opaque wasm: failed to load wasm_exec.js'))
    document.head.appendChild(s)
  })
}

function init(): Promise<AttermOpaque> {
  if (readyPromise) return readyPromise
  readyPromise = (async () => {
    await loadGoRuntime()
    const { default: wasmUrl } = await import('./opaque.wasm?url')
    const Go = (globalThis as Record<string, unknown>).Go as new () => {
      importObject: WebAssembly.Imports
      run(instance: WebAssembly.Instance): Promise<void>
    }
    const go = new Go()
    let result: WebAssembly.WebAssemblyInstantiatedSource
    try {
      result = await WebAssembly.instantiateStreaming(fetch(wasmUrl), go.importObject)
    } catch {
      // Fallback when the server doesn't send Content-Type: application/wasm.
      const resp = await fetch(wasmUrl)
      result = await WebAssembly.instantiate(await resp.arrayBuffer(), go.importObject)
    }
    // main() sets globalThis.attermOpaque then blocks on select{}, so the API
    // is present as soon as run() yields — no polling needed.
    void go.run(result.instance)
    const api = (globalThis as Record<string, unknown>).attermOpaque as AttermOpaque | undefined
    if (!api) throw new Error('opaque wasm did not initialize')
    return api
  })()
  return readyPromise
}

function unwrap<T>(res: MaybeError<T>): T {
  if (res && typeof res === 'object' && 'error' in res) {
    throw new Error((res as { error: string }).error)
  }
  return res as T
}

export async function opaqueRegisterInit(password: string): Promise<RegisterInitResult> {
  return unwrap((await init()).registerInit(password))
}
export async function opaqueRegisterFinish(
  handle: number,
  ke2: string,
  serverId: string,
  clientId: string,
): Promise<RegisterFinishResult> {
  return unwrap((await init()).registerFinish(handle, ke2, serverId, clientId))
}
export async function opaqueLoginInit(password: string): Promise<LoginInitResult> {
  return unwrap((await init()).loginInit(password))
}
export async function opaqueLoginFinish(
  handle: number,
  ke2: string,
  serverId: string,
  clientId: string,
): Promise<LoginFinishResult> {
  return unwrap((await init()).loginFinish(handle, ke2, serverId, clientId))
}
