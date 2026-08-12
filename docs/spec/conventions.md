# 代码约定

> **Audience**: 改 Go / 前端代码的工程师
> **Last updated**: 2026-07-29
> **Status**: stable
> **See also**: [component-style.md](./component-style.md) · [architecture.md](./architecture.md)

## 包结构

### Go

```text
cmd/<binary>/main.go        二进制入口，仅做配置 + wire-up，业务逻辑放 internal/
internal/<package>/         可复用业务包，不依赖 cmd/ 或 desktop/
desktop/                    Wails 桌面 app（package main，但比 cmd/ 大）
```

依赖方向规则：

```text
cmd/* → internal/*           ✓
desktop/ → internal/*        ✓
internal/* → 其他 internal/* ✓ (按层次：proto < ringbuf < session < relay; ptyhost 独立; agent → ptyhost+proto+relay)
internal/* → cmd/* 或 desktop/   ✗ 禁止反向依赖
```

如果 desktop 端需要 internal 包暴露新方法，**改 internal 包加 method**（如 `relay.Server.AdoptSession`、`Registry()`），不要在 desktop/ 写 wrapper 后再桥到 internal/。

### TypeScript

```text
desktop/frontend/src/
├── main.ts             Vue 入口
├── App.vue             根组件
├── components/         全部 *.vue，每个组件单文件
├── composables/        Vue 3 setup helpers (e.g. useTerminalShortcuts.ts,
│                       useSessionPins.ts, useCollapsedGroups.ts — 后两者是
│                       module-level ref 共享状态模式，见下「跨组件共享状态」)
├── i18n/               desktop 前端中英 messages + useI18n()
├── lib/                pure TS 模块（无 Vue 依赖）
│   ├── proto.ts        协议层
│   ├── connection.ts   WS 连接 + 重连
│   ├── types.ts        共享类型定义（LayoutKind / Pane / Tab 等）
│   ├── layout.ts       纯函数（pane 布局 transition / close / focus 导航）
│   └── api.ts          Wails bindings 包装
├── platform/           Wails / Capacitor / browser 适配层
└── plugins/            右侧插件槽与内置插件（fileExplorer：本地/远程双源切换 +
                        编辑 + CRUD + 回收站 / translate；legacy quick input
                        已删，由 QuickTemplate 取代）
```

`lib/` 不依赖 Vue（便于单元测试、未来共享给 web/）。组件不直接 fetch / WebSocket，全走 `lib/`。`lib/layout.ts` 这种纯函数模块用 vitest 跑单测，TDD 增量覆盖。

`web/src/` 是 Vue 3 + TypeScript + Naive UI。登录、注册、setup、firstrun 仍是独立 MPA 小页面；主体验 `index.html` 不再维护第二套 UI，而是通过 `main-web.ts` 挂载桌面同一份 `App.vue`：

```text
web/src/
├── main-web.ts          index.html 入口，import desktop/frontend/src/main.web.ts
├── login/ signup/ setup/ firstrun/
└── shared/              api、ws、i18n、theme、Topbar、mobile guard、prefs sync
```

Web 客户端协议层在 `web/src/shared/ws/`，不要再新增 legacy `web/app.js` 路径。改主界面的会话列表、侧栏、右键菜单、终端、Settings 或 Admin 时，优先改 `desktop/frontend/src/App.vue` / `components/**` / `platform/web.ts`；不要复活 `web/src/main/`、`web/src/settings/`、`web/src/admin/` 的第二套主界面。

## 命名

| 实体 | 风格 | 例 |
|------|------|----|
| Go 包名 | 小写单词，无下划线 | `relay`, `ptyhost`, `hostid` |
| Go 公共 type | PascalCase，描述性 | `SessionInfo`, `PtyHost`, `OpenPayload` |
| Go 公共 func | PascalCase，动词起头 | `AdoptSession`, `SubscribeLocal` |
| Go 私有 | camelCase | `mirrorState`, `streamingLocal` |
| Vue 组件 | PascalCase 文件 + kebab-case 标签 | `TabBar.vue` → `<TabBar>` |
| TS 类型 | PascalCase，interface 优先 | `Endpoint`, `RelayConfig` |
| TS 常量 | UPPER_SNAKE 仅给协议常量 | `TYPE.OUT`, `VERSION` |

## 注释

- Go：每个公共 type/func 写包注释 + Godoc 风格说明，**侧重 why**
- 不要写 commit-specific 注释（`// added for issue #123`、`// see ticket FOO-456`）
- 不要写描述代码做什么的注释（自解释）；写**约束**（"caller must hold mu"、"runs forever until ctx canceled"）
- 不要写"添加于 X 时间"类时间戳，git blame 已经有了

例：

```go
// AdoptSession registers an in-process PTY as a relay session, bypassing the
// /agent WebSocket entirely. It is the desktop app's hook for surfacing
// locally-spawned PTYs to the same xterm.js code path that handles remote
// sessions.
//
// The returned cleanup must be called exactly once. The PtyHost is NOT closed
// by cleanup — its lifecycle stays with the caller.
```

不写：

```go
// AdoptSession adopts a session.  // ← 重复函数名
// added in Phase 1.5              // ← commit info
// 2026-05-09: refactor             // ← 时间戳
```

## 错误处理

### Go

- 使用 `fmt.Errorf("...%w", err)` wrap，调用方用 `errors.Is/As`
- 不要 `panic` 在请求处理路径；只能在 `init` / `main` 启动失败用
- 用户可见错误：通过 binding 返回，前端展示
- 系统错误：走 `internal/logging`（`logging.Warn("uplink", "...")`；desktop 的
  package main 有 `logDebug/logInfo/logWarn/logError` 短别名）。**不引入**
  logrus/zap —— `internal/logging` 是标准库上的一层薄封装，只负责统一
  `TS LEVEL [tag] message` 格式和写盘阈值。裸 `log.Printf` 由
  `internal/logging` 的 AST 回归测试拦截

```go
if err != nil {
    return uuid.Nil, fmt.Errorf("open pty: %w", err)
}
```

### TypeScript

- 异步路径用 `try/catch`，把上下文 wrap 进 Error
- 网络层错误带 URL：`throw new Error(\`fetch ${url}: ${e?.message ?? e}\`)`
- UI 层错误：写到 `errorMsg` ref 给用户看，不要 console.error 然后 swallow

## 测试

### Go

- 测试文件 `*_test.go` 同包
- e2e 测试放被测组件目录下（`desktop/uplink_e2e_test.go`，**不**单独 test/ 目录）
- 测试名 `TestXxx` PascalCase
- 用 `testing.Short()` 跳过慢测试
- e2e 必须自包含：起 server、起 client、断言完整流程
- 安全边界必须有测试：admin token 强度 / `--dev-insecure` / CSRF / 限流 / owner-binding 在
  `cmd/atterm-relay/main_test.go` 与 `internal/relay/*_test.go`；用户系统 CRUD 与
  argon2 timing 在 `internal/userstore/*_test.go`；桌面 ws/wss 策略在
  `desktop/relay_security_test.go`；owner remote permission 要同时覆盖 relay 拦截
  与 desktop uplink 本机写 PTY 前拦截；自动更新签名/hash 校验在 `desktop/updater_test.go`
- session 行为分类与摘要必须有测试：`internal/session/classify_test.go` 锁住
  ai/test/build/deploy 关键字识别 + wrapper 剥离 + sticky-non-shell；
  `internal/session/summary_test.go` 覆盖 success/failure 抽取、ANSI 清洗、字节
  上限；`internal/session/ansistrip_test.go` 覆盖 CSI/OSC/ESC X 三种 escape
  shape 与截断输入；`internal/session/session_test.go` 集成测试断言 OSC 133 D
  事件之后 SessionInfo.Summary 已填充且最近一帧 META 携带相同 payload
- pairing token 必须有测试：`internal/userstore/pairing_test.go` 覆盖
  CreatePairingToken / ConsumePairingToken 的 atomic `used_at` 和过期分支；
  `internal/relay/pair_http_test.go` 覆盖 owner 鉴权、consumer 无鉴权、
  rate-limit 返回 `429`、invalid/expired/used 统一 `404`；
  `desktop/app_pairing_test.go` 验证桌面 CreatePairingToken binding 携带 Bearer
  并解析响应
- health endpoint 必须有契约测试：`internal/relay/health_http_test.go` 覆盖
  `/healthz` 字段、`/admin/health` 鉴权、HealthPayload 的 mobile origin 兼容性
  逻辑和 warning 列表

例：`desktop/uplink_e2e_test.go::TestUplinkE2E` / `TestTwoHostsCrossAttach` 是模板。

### TS / Vue

- `desktop/frontend` 使用 Vitest + jsdom；纯函数、composables、组件都放 `src/**/*.test.ts`
- `web` 使用 Vitest + happy-dom，测试放 `web/tests/unit/**`；安全/PWA 合约测试放 `web/tests/contract/*.test.mjs`
- `mobile` 的 Capacitor wrapper 脚本测试使用 Node test runner：`npm test`
- 新增用户可见文案时必须同步 desktop/web 的中英 messages；测试尽量断言 i18n key 或渲染后的文本，不要重新引入硬编码英文
- AI quick templates 测试：`desktop/frontend/src/lib/__tests__/templates.test.ts`
  覆盖 `effectiveTemplates` + 9 项默认列表的 id 稳定性；
  `SettingsTemplates.test.ts` 锁住增删改 + reset + hotkey 字段 + show-bar 开关；
  mobile / desktop / web TerminalView 测试断言点击模板按钮**直接** `sendInput(text + '\r')`，
  无预览对话框；desktop 还要覆盖 hotkey 解析（`Mod`/`Alt`/`Shift`+key）+ 仅 focused pane 响应
- 移动端模板/快捷键事件总线测试：`MobileSettings.test.ts` 验证保存模板/aux 键后
  emit `mobile:shortcutsChanged`；`MobileTerminal.test.ts` 验证订阅事件 → reload bar
- 移动端 IME 测试：`MobileTerminal.test.ts` 用 jsdom InputEvent 验证
  `insertText && !isComposing` 触发 `sendInput`，`insertCompositionText` 不被劫持
- 移动端 fit / viewer 锁尺寸测试：mock `ResizeObserver` + xterm `resize` 验证
  viewer 收到 `meta.cols/rows` 时 `term.resize(cols, rows)`、driver 不锁
- Web 终端辅助键 / 文件选择测试：`desktop/frontend/src/components/TerminalView.test.ts`
  覆盖 browser/web 下 aux key row 渲染、`Ctrl-C` 等按钮直接 `sendInput`、
  图片按钮触发 `sendPasteImage`、文件按钮触发 `sendPasteFile`，以及 view/control
  权限和 viewer 状态下按钮被 gate。桌面/Wails 路径不应显示 browser-only aux row。
- Pairing UI 测试：`desktop/frontend/src/components/__tests__/PairingPanel.test.ts`
  覆盖生成 / 二维码渲染 / 过期倒计时；`mobile` 端 setup 流程的扫码 → consume
  → 写入 secure storage 的 happy path 覆盖在 `web/tests/unit/setup-pair-*.ts`

## 风格细则

### Go

- `gofmt` / `goimports`，提交前 `go vet -tags webkit2_41 ./...` 必须 0 warning
- 不引入新依赖除非有必要；现有依赖：`creack/pty`, `google/uuid`, `nhooyr.io/websocket`, `golang.org/x/term`, `wails/v2`
- 不写日志框架；用 `internal/logging` 的 `Debug/Info/Warn/Error(tag, format, args...)`。
  旧的 `"uplink: "`、`"agent: "` 消息前缀已改成 tag 参数，不要再往消息里写前缀
- channel 缓冲深度写常量：`subscriberQueueDepth = 256`
- 锁顺序：`registry.mu` > `Session.mu`，避免反向 lock；持锁尽量短

### TypeScript

- strict mode 必开（`tsconfig.json` 已设）
- 不写 `any`；除非 wails generated 文件无可避免
- 类型在 `interface` 写公共 schema；`type` 用于 union / 别名
- 模板里数据结构的对齐：`<TerminalView :endpoint="…" :session-id="…" :active="…" />` kebab-case，`<script>` 用 camelCase
- 不在组件内做副作用（fetch/WS）跨 mount 边界；`onMounted/onUnmounted` 配对清理

#### 跨组件共享状态：module-level ref composable

多个不相关组件需要读写同一份状态（例：会话置顶集合要同时给 `TaskSidebar`、
`TaskGroupedList`、`SessionRowMenu` 用；分组折叠状态要给 header 和列表项用）时，
**不要** provide/inject 或把状态提到共同祖先再逐层传 props/emit。约定写法是
在 composable 文件的 module 顶层声明一个 `ref`，`export` 出的函数闭包引用它：

```ts
// useSessionPins.ts
const pinnedIds: Ref<Set<string>> = ref(new Set());   // module-level，全应用单例

export function useSessionPins() {
  function pin(sid: string) {
    pinnedIds.value = new Set(pinnedIds.value).add(sid);  // 见下「铁规」
    schedulePersist();
  }
  return { pinnedIds, pin, unpin, toggle, rename, flushNow };
}
```

任意组件调用 `useSessionPins()` 拿到的都是**同一个** `pinnedIds` ref——这是
"module-level singleton"而非 Vue 的依赖注入机制，`useCollapsedGroups.ts` 用
的是同一模式，`useSessionPins.ts` 是照抄它落地的（`session-bar-pin-design.md`
明确写"沿用 `useCollapsedGroups` 模式"）。

**铁规**：

- **不要**直接 `pinnedIds.value.add(...)` / `.delete(...)` 做同一 `Set` 实例的
  原地 mutation——Vue 的响应式对 `ref<Set>` 走引用相等判定，同实例 mutation
  不会触发依赖更新。必须 `pinnedIds.value = new Set(pinnedIds.value)` 先 clone
  出新实例再增删，赋值本身触发响应式。
- 需要跨重启持久化的字段（如 `PinnedSessionIDs`），写操作要 debounce
  （`useSessionPins.ts` 用 300ms）+ 提供 `flushNow()` 给调用方在关键时刻
  （如 recovery `executeRestore` 收尾）强制落盘，不要只依赖 debounce 定时器。
- 什么时候才用这个模式：**只**当状态是"整个前端进程内唯一一份、多个非父子
  组件都要读写"时才用；单一组件内部状态还是用 `ref` + props/emit，不要为了
  "统一写法"到处开 module-level singleton。

### CSS

- 用 CSS 变量（`--bg`、`--accent` 等，定义在 `style.css :root`）
- scoped style 在每个 `.vue` 单文件里
- desktop 前端不引入 CSS 框架（Tailwind / Bootstrap）；web 前端使用 Naive UI + `web/src/shared/tokens.css`
- 组件视觉、控件复用、Settings 表单和浮层规范见 `docs/spec/component-style.md`

### 移动端字体栈（iOS 26 兼容）

iOS 26 WebKit 把 `-apple-system` / `system-ui` 声明为含 CJK 覆盖、但实际渲染
时不 fall through 到后续 family，结果 CJK 字符显示成 `[?]` 方框。修复模式：

1. **ASCII mono 在前，CJK fallback 在后**：终端字体栈用 `'SF Mono', 'JetBrains
   Mono', Consolas, 'Liberation Mono', monospace, 'PingFang SC', 'Hiragino Sans
   GB'`。共享常量在 `desktop/frontend/src/lib/terminalFont.ts` 的
   `TERMINAL_FONT_FAMILY`，desktop TerminalView 和 mobile MobileTerminal 都
   import 它，避免两端漂移。
2. **UI 字体栈：`PingFang SC` 必须在 `-apple-system` 之前**（或干脆删掉
   `-apple-system`）；iOS 26 上把 `-apple-system` 放在 CJK 之前就会触发回
   归。`mobile/.../style.css` / `web/src/shared/tokens.css` 都受此约束。
3. **Emoji 用内联 SVG，不要依赖 U+FE0F 变体选择器**：iOS 26 `Apple Color
   Emoji` 对 U+26A0 (⚠) 等 BMP 字符的 emoji presentation 不可靠。涉及
   emoji 的 UI 元素（PWA install 提示、警告徽章）改为 lucide-style 内联
   SVG。

回归验证：在 iOS 26 simulator Safari 上手工跑一份 CJK + emoji 字体探针页（最小
的一组 `<span style="font-family: …">` 测试），别只看桌面浏览器；这条规则的来
源就是桌面看着没问题、iOS 26 上变 `[?]` 的真实回归。

### Capacitor 8 plugin 注册

Capacitor 8 把 plugin 发现从 ObjC runtime 改成了 SwiftPM + `CAPBridgedPlugin` 协议。
旧式的 `.m` + `CAP_PLUGIN` 宏在 Capacitor 8 **是 no-op**，不要再写。

**自定义 plugin（写在 `mobile/ios/App/App/Plugins/` 下的）**：

1. Swift 类 conform `CAPBridgedPlugin`：

   ```swift
   @objc(AttermSecureStoragePlugin)
   public class AttermSecureStoragePlugin: CAPPlugin, CAPBridgedPlugin {
       public let identifier = "AttermSecureStoragePlugin"
       public let jsName = "AttermSecureStorage"
       public let pluginMethods: [CAPPluginMethod] = [
           CAPPluginMethod(name: "set", returnType: CAPPluginReturnPromise),
           // ...
       ]
       @objc func set(_ call: CAPPluginCall) { ... }
   }
   ```

2. App-local plugin **不走 auto-discovery**，必须在
   `MainViewController.swift` 的 `capacitorDidLoad()` 里显式注册：

   ```swift
   class MainViewController: CAPBridgeViewController {
       override open func capacitorDidLoad() {
           bridge?.registerPluginInstance(AttermSecureStoragePlugin())
       }
   }
   ```

   `Main.storyboard` 的根 VC 要指到 `MainViewController`（`customModule="App"`）。

3. Xcode 工程的 Sources build phase 要包含 `.swift` 文件
   （`project.pbxproj` 手改：PBXBuildFile + PBXFileReference + PBXGroup +
   PBXSourcesBuildPhase）。`cap sync` 不处理 App target 的 Sources。

**第三方 plugin（`@capacitor/camera` / `@capacitor/keyboard` / `@capacitor-mlkit/barcode-scanning`）**：

1. 同时装到两个地方：
   - `desktop/frontend/package.json`（让 TypeScript `import` 解析）
   - `mobile/package.json`（让 `cap sync` 注册 native）
2. `cd mobile && npm run ios:open`（已串好 `npm install` + `cap sync` + 打开 Xcode）。
3. Xcode 里 `File → Packages → Resolve Package Versions`（首次拉新 plugin）。
4. 验证：
   - `mobile/ios/App/App/capacitor.config.json` 的 `packageClassList` 包含新 plugin
     （如 `CAPCameraPlugin` / `KeyboardPlugin` / `BarcodeScannerPlugin`）。
   - `mobile/ios/App/CapApp-SPM/Package.swift` 有对应 SPM 依赖。
   - JS 端 `Capacitor.isPluginAvailable('<jsName>')` 返回 `true`。
5. 需要权限的，在 `mobile/ios/App/App/Info.plist` 加 usage description string
   （如 `NSCameraUsageDescription` / `NSPhotoLibraryUsageDescription` /
   `NSPhotoLibraryAddUsageDescription`）。

**CI 不 build iOS**，这些改动必须真机验证。

### 移动端 IME insertText 接管

iOS 中文九宫格的 `，。？！` / 数字 / 空格走 `input` 事件
（`inputType='insertText'`、`isComposing=false`），xterm 的 handler 不 forward。
`MobileTerminal` 在 `term.textarea` 的 **capture 阶段**监听 `input`：

```ts
const ta = term.textarea
ta?.addEventListener('input', (ev) => {
  const e = ev as InputEvent
  if (e.isComposing) return                       // composition 路径留给 xterm
  if (e.inputType === 'insertText' && e.data) {
    e.stopImmediatePropagation()                  // 阻止 xterm 二次处理
    sendRaw(e.data)
    if (term?.textarea) term.textarea.value = ''
  }
}, { capture: true })
```

**铁规**：

- 只处理 `insertText`（直接输入）。`insertCompositionText`（拼音→汉字 composition
  结果）一律不碰，让 xterm 自己处理。否则中文字会双发。
- `stopImmediatePropagation` 必须有，否则 xterm 的 bubble-phase handler 也会发一次。
- capture 阶段必须在 xterm 注册的 input handler 之前生效，所以 capture=true。

### WebSocket 同步异常隔离

`SessionListConnection.openWS()` / `SessionConnection.openWS()`（都在
`desktop/frontend/src/lib/connection.ts`）调 `new WebSocket(url, protocols)`
必须包 try/catch，把异常路由到 `handleOpenFailure`：

```ts
private openWS(): void {
  if (this.detached) return;
  const auth = webSocketAuth(this.endpoint, "/client-sessions");
  let ws: WebSocket;
  try {
    ws = auth.protocols ? new WebSocket(auth.url, auth.protocols) : new WebSocket(auth.url);
  } catch (e) {
    this.handleOpenFailure(e, auth);
    return;
  }
  // … 正常路径
}

private handleOpenFailure(e: unknown, auth: { url: string; protocols?: string[] }): void {
  const err = e as { name?: string; message?: string } | null;
  console.error("[SessionListConnection] new WebSocket failed", {
    url: auth.url,
    protocols: auth.protocols,
    name: err?.name,
    message: err?.message ?? String(e),
  });
  this.ws = null;
  this.handlers.onStatus?.("error");
  if (this.detached) return;
  const delay = Math.min(8000, 500 * Math.pow(2, this.reconnectAttempts++));
  this.reconnectTimer = window.setTimeout(() => this.openWS(), delay);
}
```

**为什么是同步抛异常**：WebKit 对下列条件**同步**抛
`SyntaxError: The string did not match the expected pattern.`（DOMException），
**不**走异步 `onclose`：

- url scheme 不是 `ws://` / `wss://`（例如配置漂成了 `https://`）
- url 含非法字符（极少见，但比如 host 残留 `[::1]` 但格式错乱）
- subprotocol 字符集越界（RFC 6455 token：`!#$%&'*+-.^_\`|~` + ALPHA + DIGIT；
  我们的 `SUBPROTOCOL_SAFE` 兜底过滤，但兜底也可能漏，比如未来扩展时）

**为什么必须接住**：`App.vue::onMounted` 的 boot 链是 `await refreshTerminalTheme()`
→ `await getEndpoint()` → `await getHostInfo()` → `connectLocalSessionList(...)`（同步）
→ `await refreshRelayConfig()`（其中又同步 `connectRemoteSessionList(...)`）。
如果 `connectLocalSessionList` / `connectRemoteSessionList` 里 `new WebSocket`
抛出，**整个 await chain 立即解开**，落进 App.vue 的 catch，把启动卡在
"正在启动第一个会话…" + titlebar 红字 "The string did not match the expected pattern."。
而且因为 `localEndpoint` 已经在 `getEndpoint()` 后赋值，主区显示空 PaneGrid
loading 状态，整个 app 看着像挂了。

**铁规**：

- `new WebSocket(...)` 出现的所有点都必须 try/catch；目前只有 `connection.ts`
  里两处 `openWS`。新加 WS 调用时一并加保护。
- failure path 必须走 `onStatus("error")` + 指数退避重连（500 × 2^attempts，上限 8s），
  不能直接 `throw e` 给调用者。
- `console.error` 一定要带 `{ url, protocols, name, message }`，下次出问题
  好定位是哪种非法值。
- WebKit 的 message 是 `"The string did not match the expected pattern."`；
  其它浏览器 message 可能不同。**只看 `name === 'SyntaxError'`**，不要 grep 文本。

回归覆盖：`connection.test.ts` 的 `describe("openWS sync-throw isolation", ...)`
用 source-text grep 守住 try/catch 包裹 + `handleOpenFailure` + `onStatus("error")` +
`setTimeout(() => this.openWS(), delay)` 同时存在。删任何一条都会被测试抓住。

### 启动 try/catch 分阶段

`App.vue::onMounted` 里多步 boot 调用必须用 `let bootStage = ""` 在每一步**前**
赋值；catch 时把 `${bootStage}: ${e.name}: ${e.message}` 写进
`errorMsg.value` + `console.error('[boot] step "${bootStage}" failed', { … })`。

**六步顺序固定**（v0.3.x 新增第 4 步 `loadRecoverySnapshot`，在 `getHostInfo`
与 `connectLocalSessionList` 之间；插错顺序会让 recovery 快照在会话列表建立前
还没就绪，pin 迁移拿不到旧 sid）：

```ts
let bootStage = "";
try {
  bootStage = "refreshTerminalTheme";
  await refreshTerminalTheme();
  bootStage = "getEndpoint";
  localEndpoint.value = await getEndpoint();
  bootStage = "getHostInfo";
  const info = await getHostInfo();
  localHostID.value = info.host_id;
  bootStage = "loadRecoverySnapshot";
  await loadRecoverySnapshot();
  bootStage = "connectLocalSessionList";
  connectLocalSessionList(localEndpoint.value);
  bootStage = "refreshRelayConfig";
  await refreshRelayConfig();
} catch (e: any) {
  const name = e?.name ?? "Error";
  const msg = e?.message ?? String(e);
  console.error(`[boot] step "${bootStage}" failed`, {
    name, message: msg, stack: e?.stack,
  });
  status.value = "error";
  errorMsg.value = `${bootStage}: ${name}: ${msg}` || i18nT("app.wailsBindingsUnavailable");
  return;
}
// boot 末尾（无论上面是否抛出）再拉一次 Go 侧致命错误，见下「Go 侧 StartupError」
const startupErr = await GetStartupError();
if (startupErr?.fatal) showStartupFailure(startupErr);
```

效果：

- titlebar 错误从 `The string did not match the expected pattern.` 升级为
  `connectLocalSessionList: SyntaxError: The string did not match the expected pattern.`，
  一眼定位是哪步。
- `console.error` 把 `stack` 一并打出来，在 dev / WebInspector 里能跳到行号。
- bootStage 不会重置 —— 若 catch 在第 4 步触发，`bootStage === "connectLocalSessionList"`
  就是失败点。

**为什么不拆成多个 try/catch**：每个 catch 都要重复同样的 status / errorMsg 赋值，
噪声大；用 `bootStage` 变量是单 catch + 阶段标签，可读性最好。

**反例**：

- 把 `bootStage = "..."` 放在 `await` 调用**之后**——那 await 抛异常时
  bootStage 还是上一步的名字，误报。
- 用 `try { step1 } catch (e) { return } try { step2 } catch (e) { return }`
  这种分散 catch——5 个调用就 5 段相同的错误处理，写错一个就回归。
- 把 `bootStage` 局部化到 try 内（`let bootStage = ""` 放 try 里）——
  catch 拿不到。

### 日志：级别口径与 tag

全仓库一个格式、一个真源：`internal/logging`。desktop 把它接到轮转文件，relay
接到 stderr（`docker logs`），前端 `lib/log.ts` 批量回传给 Go 写进同一个
`desktop.log`。设计见
[`docs/superpowers/specs/2026-08-08-project-wide-logging-design.md`](../superpowers/specs/2026-08-08-project-wide-logging-design.md)。

```
2026/08/08 15:04:05.123 DEBUG [pty-input] write n=1 hex=1b LONE-ESC
2026/08/08 15:04:05.140 INFO  [uplink] connected, sent ANNOUNCE (3 session(s))
```

**级别口径**（Go 与前端同一套）：

| 级别 | 含义 | 例子 |
|---|---|---|
| `ERROR` | 用户要做的事失败了、用户可感知、且没有重试 | 启动致命、keychain 写失败、更新验签失败、粘贴文件落盘失败 |
| `WARN` | 失败但自动降级/重试，或异常但不影响主流程 | uplink 重连、飞书卡片 patch 重试、丢帧、丢弃损坏的 recovery 快照、越权入站帧 |
| `INFO` | 生命周期与状态迁移，低频，每行人能看懂 | uplink connected、session created、shell integration enabled、恢复注入 resume |
| `DEBUG` | 逐帧/逐字节/逐轮询的内部细节 | `inbound_recv`、`stream_out_progress`、repaint nudge、pty-input hex |

**写盘阈值**默认 `INFO`（`config.json` 的 `log_level`，Settings → Logging 可改，
热生效）。所以高频日志一律 `DEBUG`——平时不写盘，排查时一个下拉切过去就有。
注意 Settings 里两个下拉不是一回事：**日志级别**决定写不写进文件，viewer 里的
**显示**只过滤已经写进去的内容。

**`EmitForced` 只给"自带开关"的三处用**：`pty-input` 字节追踪（Settings 勾选框）、
relay 的 `debugOn()`（admin UI 开关）、`ATTERM_DEBUG_SILENCE=1`。它们各自的开关
**就是**门控，再叠一层全局级别只会让人勾了开关却看不到输出。新代码不要随手用。

**tag 用小写 kebab、按子系统**：`app` `boot` `config` `uplink` `uplink-stream`
`relay` `relay-host` `relay-client` `relay-agent` `relay-uplink` `relay-admin`
`relay-adopt` `relay-config` `relay-feishu` `relay-debug` `session` `silence`
`pty` `pty-input` `repaint` `recovery` `ai-sid` `feishu` `feishu-anchor`
`feishu-form` `feishu-hook` `feishu-card` `feishu-turn` `shell-integration`
`paste` `updater` `notify` `keychain` `e2ee` `prefs` `plugin` `webpush` `events`
`opaque` `hookinstall` `remote-proxy` `migrate` `bootstrap` `keychain`。
前端传裸 tag，`ui-` 前缀由 Go 侧 `AppendFrontendLogs` 统一加，前端 tag 因此
永远不会和 Go 的撞名。

**两条回归测试守着**，加新代码时它们会先叫：
- `internal/logging/nostdlib_test.go` —— AST 扫全仓库，禁裸 `log.Printf/Fatal/...`
  （只放行 `cmd/atterm-relay/main.go` 的 `log.Fatal*`，那是 fail-closed 启动检查）
- `desktop/frontend/src/lib/noConsole.test.ts` —— 禁 `console.*`（只放行
  `lib/log.ts` 自己）。项目没有 ESLint，这个测试就是 `no-console` 规则

**viewer 有两个维度**:级别阈值 + **子系统(tag)**。tag 下拉是从日志内容**实测**出来的
(`logTagOptions`),不是硬编码列表,所以永远不会和代码里的 tag 脱节;因为 tag 是
`feishu-*` / `relay-*` / `ui-*` 家族式的,同族有 2 个以上成员时会额外给一个
`<前缀>*` 选项。标签带条数,方便先看出「是哪个子系统在刷屏」再决定筛谁。

**静默 catch 要不要补日志——一条规则**:满足下面任一条才补,否则保持沉默。

| 补 | 场景 | 例 |
|---|---|---|
| ✅ WARN | 订阅者/回调抛异常(隔离是对的,但那是 bug,沉默 = 永远发现不了) | `platform/web.ts` 的 `emit`、`connection.ts` 的 fs handler |
| ✅ WARN | 安全相关的静默降级 | sealed 字段打不开 → 回落明文;`account_key` 恢复失败 |
| ✅ WARN | 用户的意图静默没生效(写失败 + 回读也失败 → UI 显示的是假状态) | 飞书开关、记住密码 |
| ✅ WARN | 数据被整块丢弃 | `LIST_RESP` 解析/解密失败 |
| ✅ DEBUG | 状态陈旧且无其它出口,但高频 | 远程会话轮询失败、health 轮询失败 |
| ❌ | 别处已有可见出口(`state.error`、轮询暴露、UI error 态) | `SettingsUpdates` 的一批 |
| ❌ | DOM/环境能力探测 | `setPointerCapture`、clipboard、Notification 权限 |
| ❌ | 读一个从未设置过的偏好,走默认值 | `getTaskSidebarCollapsed` |
| ❌ | 清理已经死掉的资源 | 对已关闭的 ws 再 `close()` |

写这条表的原因:全前端有 ~79 处 catch 体内只有注释,**逐条判断的结论比"全加"或
"全不加"都有价值** —— 全加会把日志淹掉,全不加就是现在这批 bug 藏身的地方。

**红线 #21 在日志侧的落点**：前端 `LogFields` 只收基本类型（不接受任意对象，
避免 `{ req }` 把密码整个序列化进去），且 key 命中 `/pass|token|key|secret|cred|auth/i`
的值一律替换成 `***`。

### Go 侧 StartupError（前端 bootStage 的配套半边）

前端 `bootStage` 只能捕获**前端**抛出的异常。Go 侧的致命初始化失败（relay
host 起不来、日志系统初始化失败）历史上是 `log.Fatalf`——整个进程崩溃退出，
webview 来不及展示任何 UI，用户只看到 app 消失。v0.3.x 把这类路径统一改成
非崩溃：

- `desktop/app.go::setStartupFatalError(msg, logPath)` 把失败记进
  `startupFatal *StartupError{Fatal, Message, LogPath}` 字段，函数
  **return** 而不是 `log.Fatalf`，Wails 主循环继续跑，webview 正常起来。
- 前端在 boot 链末尾（无论上面 `bootStage` try/catch 是否已经失败）调用
  `GetStartupError()`，非空且 `Fatal` 为真时展示可复制的失败信息
  + 日志路径（`app.startupFailureCopy` i18n key）。
- **不要**再在 `desktop/` 初始化路径里加裸 `log.Fatalf`；任何"没这个就跑不动"
  的失败都要走 `setStartupFatalError` + `return`（AGENTS.md 红线 #19 / #35）。

这两端（`bootStage` + `StartupError`）合起来才是完整的启动失败可观测设计——
只读一半会漏掉另一半覆盖的失败场景。

### Sticky non-shell session type

`internal/session/applyOSC133Locked` 在收到 OSC 133 `C`（命令开始）时调
`ClassifyCommand(cmd)`：返回 `shell` 时**不**覆盖 `s.meta.Type`，返回
non-shell 时才覆盖。这条 sticky 规则保证：

- `go test` → `code editor`（shell）的切换不会把 type 从 `test` 退回
  `shell`，否则任务卡片的类型 chip 会闪烁。
- `test` / `build` / `deploy` 对后续普通 shell 命令保持 sticky；`ai` 是例外：
  顶层 AI 命令发出 OSC 133 `D` 后，下一条普通 shell 命令可把 type 退回
  `shell`。每个新的顶层 AI OSC 133 `C` 仍必须单独通知 desktop，以切换
  recovery resolver generation，不能因为 type 已经是 `ai` 而吞掉。

只在 `applyOSC133Locked` 一处实现这个语义；不要在 frontend 端再做一层
"sticky" 补丁，否则两层规则会互相打架。`type` 同样需要走 MetaPayload 广播
路径（P2.12 之前漏了这点，导致已连接的 subscriber 拿不到实时类型变化）。

## Commit 风格

参考现有 git log：

```text
ci: github actions to build linux/amd64 + darwin/arm64 desktop

  - 缩进列表说明改动
  - 多行 body
  - subject 用小写动词起头，≤ 72 字符
```

类型前缀：`fix:` / `feat:` / `refactor:` / `ci:` / `docs:` / `chore:`（非强制但鼓励）。

**不要**：
- 在 commit msg 写 "Co-Authored-By: Claude" / AI 生成痕迹
- subject 末尾加句号
- 写"WIP"/"checkpoint"风格 commit（应当在合并前 squash 干净）

## 不要做的事（红线）

- ❌ 在 internal/relay 引用 desktop/ 包（依赖方向倒置）
- ❌ 按 host_id 去重 session（详见 `architecture.md` §会话标识）
- ❌ 直接 `conn.Write` 在多 goroutine（nhooyr/websocket 不允许）
- ❌ 给 OUT 帧 seq 重置（重连续传依赖单调性）
- ❌ 在 PTY 字节流路径加 JSON 解析或 regex 匹配
- ❌ 用 `--no-verify` skip git hook（修复 hook 失败的根因，不是绕过）
- ❌ 给 frontend 加新依赖未经讨论（已有 vue + xterm 已够）
- ❌ 自动更新缺少 `Ed25519`/`SHA256` 校验时继续安装（缺公钥也必须 fail-closed）
- ❌ 把 `ATTERM_UPDATE_SIGNING_PRIVATE_KEY` 写进仓库、日志或 release artifact
- ❌ 公网 relay 默认允许弱 token/空鉴权、缺失 `--origins` 或弱 admin token；需要 `--dev-insecure` 才能放开这些限制
- ❌ 桌面端默认允许非 loopback `ws://`；只能由用户在 Settings 显式打开 insecure mode
- ❌ `web/` 引入 CDN script/style；浏览器客户端必须使用 Vite 打包出来的同源 assets，避免 CSP/PWA 回归
- ❌ 服务端接受 `?token=` 鉴权，或让浏览器长期把 secret 留在地址栏、日志或可分享 URL 中；手工打开 web 页面只能用 `#token=...` fragment bootstrap，WS 鉴权必须用 `Sec-WebSocket-Protocol`
- ❌ 把用户凭据明文（密码、邀请码、API token、cookie 值）写进数据库、日志或任何持久化路径——全部以 `sha256`/`argon2id` 散列存储，明文仅在签发时返回一次
- ❌ 把远程权限只做成 UI 提示；relay 和 desktop host 都必须实际拦截越权帧

## Release 签名与发版

GitHub `prod` environment 需要两个 secrets：

- `ATTERM_UPDATE_VERIFY_PUBLIC_KEY`：base64 `Ed25519` 公钥（32 bytes），构建桌面 app 时通过 ldflags 注入 `main.UpdateVerifyPublicKey`
- `ATTERM_UPDATE_SIGNING_PRIVATE_KEY`：base64 `Ed25519` 私钥（64 bytes），只在 release job 里用于签 `SHA256SUMS`

tag `v*` 触发 `.github/workflows/build.yml`：

1. 多平台 build jobs 产出 updater archives / installer artifacts；
2. release job 下载 artifacts；
3. `.github/scripts/sign-release-checksums.go` 生成 `SHA256SUMS` 和 `SHA256SUMS.sig`；
4. `softprops/action-gh-release` 上传所有 artifacts。

本地查看 Actions 状态：

```bash
export PATH=/opt/homebrew/bin:$PATH
gh run list --repo attson/atterm --limit 10
# 或直接 /opt/homebrew/bin/gh run list --repo attson/atterm --limit 10
```

## 如何修改协议

1. 决定新帧类型字节（看 `internal/proto/frame.go` 已用列表，挑未占用的）
2. 加 `Type` 常量 + payload struct
3. 加发送方实现（agent / uplink / relay 的某条路径）
4. 加接收方分支（在 reader switch 里）
5. 同步 TS 端 `desktop/frontend/src/lib/proto.ts` 的 `TYPE` 枚举（如果是 client 路径用到）
6. 同步 `web/src/shared/ws/protocol.ts` 的 `TYPE` 字典（如果浏览器需要）
7. 更新 `docs/spec/protocol.md` 帧类型表
8. 加 e2e 测试（`desktop/uplink_e2e_test.go` 或新文件）
9. 提交：subject 用 `proto:` 前缀

## 如何加 Wails binding

1. 在 `desktop/app.go` 加 method on `*App`，名字 PascalCase
2. 参数和返回类型用 `desktop/app.go` 内 named type（不要 anonymous struct——wails generator 不喜欢）
3. wails dev / wails build 会自动 generate `desktop/frontend/wailsjs/go/main/App.{ts,js}` + `models.ts`
4. 前端在 `desktop/frontend/src/lib/api.ts` 手写一份对应 wrapper（**不**依赖 generated 文件，走 `window.go.main.App.<Method>` 全局；保持 type interface 与 generated 对齐即可）
5. 错误回传用 `(T, error)` 模式，前端 try/catch

## 如何加新前端组件

1. 文件 `desktop/frontend/src/components/<PascalCase>.vue`
2. 单文件三段：`<script setup lang="ts">`、`<template>`、`<style scoped>`
3. props/emits 用 TypeScript defineProps/defineEmits 显式定义
4. 不在组件里写 fetch/WS——通过 `lib/api` 或 `lib/connection`
5. 样式用 `var(--xxx)` 主题变量，不写硬编码颜色（除特殊视觉如 `#d29922` 远程 amber）
6. 表单控件优先复用现有组件（如 `SelectDropdown`），不要新增系统默认下拉；细则见 `component-style.md`
