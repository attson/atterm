# 代码约定

> **Audience**: 改 Go / 前端代码的工程师
> **Last updated**: 2026-06-10
> **Status**: stable
> **See also**: [component-style.md](./component-style.md) · [architecture.md](./architecture.md)

## 包结构

### Go

```
cmd/<binary>/main.go        二进制入口，仅做配置 + wire-up，业务逻辑放 internal/
internal/<package>/         可复用业务包，不依赖 cmd/ 或 desktop/
desktop/                    Wails 桌面 app（package main，但比 cmd/ 大）
```

依赖方向规则：

```
cmd/* → internal/*           ✓
desktop/ → internal/*        ✓
internal/* → 其他 internal/* ✓ (按层次：proto < ringbuf < session < relay; ptyhost 独立; agent → ptyhost+proto+relay)
internal/* → cmd/* 或 desktop/   ✗ 禁止反向依赖
```

如果 desktop 端需要 internal 包暴露新方法，**改 internal 包加 method**（如 `relay.Server.AdoptSession`、`Registry()`），不要在 desktop/ 写 wrapper 后再桥到 internal/。

### TypeScript

```
desktop/frontend/src/
├── main.ts             Vue 入口
├── App.vue             根组件
├── components/         全部 *.vue，每个组件单文件
├── composables/        Vue 3 setup helpers (e.g. useTerminalShortcuts.ts)
├── i18n/               desktop 前端中英 messages + useI18n()
├── lib/                pure TS 模块（无 Vue 依赖）
│   ├── proto.ts        协议层
│   ├── connection.ts   WS 连接 + 重连
│   ├── types.ts        共享类型定义（LayoutKind / Pane / Tab 等）
│   ├── layout.ts       纯函数（pane 布局 transition / close / focus 导航）
│   └── api.ts          Wails bindings 包装
├── platform/           Wails / Capacitor / browser 适配层
└── plugins/            右侧插件槽与内置插件（file explorer / quick input / translate）
```

`lib/` 不依赖 Vue（便于单元测试、未来共享给 web/）。组件不直接 fetch / WebSocket，全走 `lib/`。`lib/layout.ts` 这种纯函数模块用 vitest 跑单测，TDD 增量覆盖。

`web/src/` 是独立的 Vue 3 + TypeScript + Naive UI MPA：

```
web/src/
├── main/ login/ signup/ setup/ settings/ admin/
└── shared/              api、ws、i18n、theme、Topbar、mobile guard
```

Web 客户端协议层在 `web/src/shared/ws/`，不要再新增 legacy `web/app.js` 路径。

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
- 系统错误：`log.Printf("...")` 用标准库 log，**不引入** logrus/zap

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
  rate-limit 返回 429、invalid/expired/used 统一 404；
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
- Pairing UI 测试：`desktop/frontend/src/components/__tests__/PairingPanel.test.ts`
  覆盖生成 / 二维码渲染 / 过期倒计时；`mobile` 端 setup 流程的扫码 → consume
  → 写入 secure storage 的 happy path 覆盖在 `web/tests/unit/setup-pair-*.ts`

## 风格细则

### Go

- `gofmt` / `goimports`，提交前 `go vet -tags webkit2_41 ./...` 必须 0 warning
- 不引入新依赖除非有必要；现有依赖：`creack/pty`, `google/uuid`, `nhooyr.io/websocket`, `golang.org/x/term`, `wails/v2`
- 不写日志框架；用 `log.Printf` + 短前缀 `"uplink:"`、`"agent:"`、`"desktop relay:"`
- channel 缓冲深度写常量：`subscriberQueueDepth = 256`
- 锁顺序：`registry.mu` > `Session.mu`，避免反向 lock；持锁尽量短

### TypeScript

- strict mode 必开（`tsconfig.json` 已设）
- 不写 `any`；除非 wails generated 文件无可避免
- 类型在 `interface` 写公共 schema；`type` 用于 union / 别名
- 模板里数据结构的对齐：`<TerminalView :endpoint="…" :session-id="…" :active="…" />` kebab-case，`<script>` 用 camelCase
- 不在组件内做副作用（fetch/WS）跨 mount 边界；`onMounted/onUnmounted` 配对清理

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

### Sticky non-shell session type

`internal/session/applyOSC133Locked` 在收到 OSC 133 `C`（命令开始）时调
`ClassifyCommand(cmd)`：返回 `shell` 时**不**覆盖 `s.meta.Type`，返回
non-shell 时才覆盖。这条 sticky 规则保证：

- `go test` → `code editor`（shell）的切换不会把 type 从 `test` 退回
  `shell`，否则任务卡片的类型 chip 会闪烁。
- 一旦升过 `ai` / `test` / `build` / `deploy`，整个 session 生命周期都会保
  持那个类型直到 session 关闭。

只在 `applyOSC133Locked` 一处实现这个语义；不要在 frontend 端再做一层
"sticky" 补丁，否则两层规则会互相打架。`type` 同样需要走 MetaPayload 广播
路径（P2.12 之前漏了这点，导致已连接的 subscriber 拿不到实时类型变化）。

## Commit 风格

参考现有 git log：

```
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
- ❌ 自动更新缺少 Ed25519/SHA256 校验时继续安装（缺公钥也必须 fail-closed）
- ❌ 把 `ATTERM_UPDATE_SIGNING_PRIVATE_KEY` 写进仓库、日志或 release artifact
- ❌ 公网 relay 默认允许弱 token/空鉴权、缺失 `--origins` 或弱 admin token；需要 `--dev-insecure` 才能放开这些限制
- ❌ 桌面端默认允许非 loopback `ws://`；只能由用户在 Settings 显式打开 insecure mode
- ❌ `web/` 引入 CDN script/style；浏览器客户端必须使用 Vite 打包出来的同源 assets，避免 CSP/PWA 回归
- ❌ 服务端接受 `?token=` 鉴权，或让浏览器长期把 secret 留在地址栏、日志或可分享 URL 中；手工打开 web 页面只能用 `#token=...` fragment bootstrap，WS 鉴权必须用 `Sec-WebSocket-Protocol`
- ❌ 把用户凭据明文（密码、邀请码、API token、cookie 值）写进数据库、日志或任何持久化路径——全部以 sha256/argon2id 散列存储，明文仅在签发时返回一次
- ❌ 把远程权限只做成 UI 提示；relay 和 desktop host 都必须实际拦截越权帧

## Release 签名与发版

GitHub `prod` environment 需要两个 secrets：

- `ATTERM_UPDATE_VERIFY_PUBLIC_KEY`：base64 Ed25519 公钥（32 bytes），构建桌面 app 时通过 ldflags 注入 `main.UpdateVerifyPublicKey`
- `ATTERM_UPDATE_SIGNING_PRIVATE_KEY`：base64 Ed25519 私钥（64 bytes），只在 release job 里用于签 `SHA256SUMS`

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
