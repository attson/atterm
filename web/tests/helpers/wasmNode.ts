// Loads the bytemare OPAQUE WASM client (web/src/shared/lib/opaque.wasm) in a
// Node/vitest process. The production loader (@shared/lib/opaqueWasm) is
// browser-only (it injects a <script>); tests instead eval Go's wasm_exec.js
// via node:vm and instantiate from a file buffer. Only used by relay-gated
// interop tests, so the .wasm need only exist when those actually run.

import fs from 'node:fs'
import vm from 'node:vm'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

interface AttermOpaque {
  registerInit(password: string): { handle: number; ke1: string } | { error: string }
  registerFinish(
    handle: number,
    ke2: string,
    serverId: string,
    clientId: string,
  ): { record: string; exportKey: string } | { error: string }
  loginInit(password: string): { handle: number; ke1: string } | { error: string }
  loginFinish(
    handle: number,
    ke2: string,
    serverId: string,
    clientId: string,
  ): { ke3: string; exportKey: string; sessionKey: string } | { error: string }
}

const libDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../src/shared/lib')

let cached: AttermOpaque | null = null

export async function loadWasmOpaque(): Promise<AttermOpaque> {
  if (cached) return cached
  // Defines globalThis.Go.
  vm.runInThisContext(fs.readFileSync(path.join(libDir, 'wasm_exec.js'), 'utf8'))
  const Go = (globalThis as Record<string, unknown>).Go as new () => {
    importObject: WebAssembly.Imports
    run(instance: WebAssembly.Instance): void
  }
  const go = new Go()
  const bytes = fs.readFileSync(path.join(libDir, 'opaque.wasm'))
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject)
  go.run(instance) // sets globalThis.attermOpaque, then blocks on select{}
  const api = (globalThis as Record<string, unknown>).attermOpaque as AttermOpaque | undefined
  if (!api) throw new Error('opaque wasm did not initialize')
  cached = api
  return api
}

export function expectOk<T>(res: T | { error: string }): T {
  if (res && typeof res === 'object' && 'error' in res) {
    throw new Error((res as { error: string }).error)
  }
  return res as T
}
