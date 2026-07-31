// Wails 绑定桩。demo 走 web/mock 平台(main.web.ts → createMockPlatform),
// 正常不会触达这些函数;但 App.vue 的静态依赖链(lib/api.ts 等)里残留对
// wailsjs/go/main/App 与 wailsjs/runtime/runtime 的具名 import,需要这些导出
// 存在以通过打包(config.mjs 的 alias 把两个模块都指向本文件)。
//
// 具名清单由 `node scan wailsjs imports` 扫描 desktop/frontend/src 得到。
// runtime 事件/窗口类导出 no-op;App 绑定类被 mock 平台绕过,若真被调用则抛
// 错以便定位(而非静默返回坏数据)。若前端新增了 wailsjs 具名 import 导致
// 打包报 "not exported",把新名字补到对应分组即可。

function notImplemented(name: string) {
  return (..._args: unknown[]): never => {
    throw new Error(`[demo] wails binding '${name}' called in mock environment`)
  }
}
const noop = (..._args: unknown[]) => {}

// ----- wailsjs/go/main/App -----
export const GetPasteboardFileURLs = async () => [] as string[]
export const GetPluginConfig = notImplemented('GetPluginConfig')
export const SetPluginConfig = notImplemented('SetPluginConfig')
export const ReceivedFilesClearAll = notImplemented('ReceivedFilesClearAll')
export const ReceivedFilesClearSession = notImplemented('ReceivedFilesClearSession')
export const ReceivedFilesDelete = notImplemented('ReceivedFilesDelete')
export const ReceivedFilesList = async () => [] as unknown[]
export const ReceivedFilesOpenDir = notImplemented('ReceivedFilesOpenDir')

// ----- wailsjs/runtime/runtime -----
export const EventsOn = (..._args: unknown[]) => () => {}
export const EventsEmit = noop
export const WindowMinimise = noop
export const WindowShow = noop
export const WindowUnminimise = noop
export const WindowSetTitle = noop
export const WindowToggleMaximise = noop
export const WindowIsMaximised = async () => false
export const Quit = noop
export const Environment = async () => ({ buildType: 'web', platform: 'web', arch: '' })
export const BrowserOpenURL = (url: string) => {
  if (typeof window !== 'undefined') window.open(url, '_blank', 'noopener')
}
export const InitializeNotifications = noop
export const IsNotificationAvailable = async () => false
export const CheckNotificationAuthorization = async () => false
export const RequestNotificationAuthorization = async () => false
export const SendNotification = noop

export default {}
