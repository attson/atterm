// OPAQUE WASM 客户端桩。真实 opaqueWasm.ts 会 `import './wasm_exec.js?url'`
// 加载一个 ~1MB 的 Go WASM(登录/注册用),而 wasm_exec.js / opaque.wasm 是
// gitignore 的构建产物 —— CI checkout 后不存在,导致打包解析失败。
//
// demo 走 mock 平台,已「登录 + 已连接」,永不触发 OPAQUE 注册/登录流程,所以
// 用桩替换整个模块(config.mjs 的 alias)。若真被调用则抛错以便定位。
//
// 注意:纯 JS 的 ./opaque.ts(noble AEAD 解密,被 client-conn/connection 用)
// 不加载 wasm,不在此桩替换范围内 —— 它照常走真实实现(demo 里 account key
// 未解锁,解密路径会自然跳过)。

function notImplemented(name: string) {
  return async (..._args: unknown[]): Promise<never> => {
    throw new Error(`[demo] OPAQUE '${name}' called in mock environment (login is stubbed out)`)
  }
}

export const opaqueRegisterInit = notImplemented('opaqueRegisterInit')
export const opaqueRegisterFinish = notImplemented('opaqueRegisterFinish')
export const opaqueLoginInit = notImplemented('opaqueLoginInit')
export const opaqueLoginFinish = notImplemented('opaqueLoginFinish')
