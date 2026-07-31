// Wails 绑定桩。demo 走 web/mock 平台(main.web.ts → createMockPlatform),
// 正常不会触达这些函数;但 App.vue 的静态依赖链里可能残留对
// wailsjs/go/main/App 与 wailsjs/runtime/runtime 的具名 import,需要这些导出
// 存在以通过打包(config.mjs 的 alias 把两个模块都指向本文件)。
//
// 具名清单来自 `grep wailsjs/{go/main/App,runtime/runtime}` 的汇总:
//   App:     GetPluginConfig SetPluginConfig GetAppVersion GetPasteboardFileURLs
//   runtime: BrowserOpenURL Environment EventsEmit EventsOn
//            WindowMinimise WindowShow WindowUnminimise
//
// runtime 事件/窗口类导出 no-op;App 绑定类里会被 mock 平台绕过,若真被调用
// 则抛错以便定位(而非静默返回坏数据)。

function notImplemented(name: string) {
  return (..._args: unknown[]): never => {
    throw new Error(`[demo] wails binding '${name}' called in mock environment`)
  }
}

// ----- wailsjs/go/main/App -----
export const GetPluginConfig = notImplemented('GetPluginConfig')
export const SetPluginConfig = notImplemented('SetPluginConfig')
export const GetAppVersion = notImplemented('GetAppVersion')
export const GetPasteboardFileURLs = notImplemented('GetPasteboardFileURLs')

// ----- wailsjs/runtime/runtime -----
export const EventsOn = (..._args: unknown[]) => () => {}
export const EventsEmit = (..._args: unknown[]) => {}
export const WindowMinimise = (..._args: unknown[]) => {}
export const WindowShow = (..._args: unknown[]) => {}
export const WindowUnminimise = (..._args: unknown[]) => {}
export const Environment = async () => ({ buildType: 'web', platform: 'web', arch: '' })
export const BrowserOpenURL = (url: string) => {
  if (typeof window !== 'undefined') window.open(url, '_blank', 'noopener')
}

export default {}
