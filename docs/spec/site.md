# Demo 站点（site/）

> **Audience**: 维护 `site/`(GitHub Pages 首页交互 demo)的工程师
> **Last updated**: 2026-07-31
> **Status**: stable
> **See also**: [architecture.md](./architecture.md) · [protocol.md](./protocol.md) · [component-style.md](./component-style.md)

`site/` 是一个 VitePress 站点,发布到 GitHub Pages(<https://attson.github.io/atterm/>)。它有两个职责:

1. **文档站** —— 面向用户的介绍、快速上手、部署、E2EE、AI agent 等(`docs/guide/*.md`),README 精简后详细内容都在这里。
2. **首页交互 demo** —— 在浏览器里嵌入**真实**的 `desktop/frontend/src/App.vue`,用 mock 后端驱动,让访客无需本地 relay 就能体验会话侧栏、终端回放、交互输入、文件浏览、快捷模板。

## 核心原则:零侵入

demo 复用真实前端源码(`desktop/frontend/src` + `web/src/shared`),**不改动它们一行**。所有差异都收敛在 `site/docs/.vitepress/theme/components/mock/` 与 `config.mjs` 的 vite alias 里。前端迭代后 demo 自动跟随,唯一维护点是 mock 层。

> 代价:mock 层与真实前端的接口(Platform / proto 帧 / FS 帧 / caps 门控)耦合。前端改这些接口时,mock 层要同步。改动集中在一处,且有 vitest 单测兜底数据结构。

## 目录

```text
site/
├── package.json                 vitepress + 复用 web 的运行时依赖(vue/naive-ui/xterm/pinia/@noble)
├── vitest.config.ts             mock 层单测的 alias(与 config.mjs 对齐)
└── docs/
    ├── index.md                 首页:hero + <HomeDemo/> + 徽章 + 特性 + 下载
    ├── guide/*.md               文档页(从 README 提炼)
    └── .vitepress/
        ├── config.mjs           base:'/atterm/'、nav/sidebar、vite alias、dedupe、ssr.noExternal
        └── theme/
            ├── index.js         extends 默认主题,注册 HomeDemo + custom.css + Pinia
            ├── custom.css       首页版式 + 暗色背景统一 + hero 间距
            └── components/
                ├── HomeDemo.vue 挂载前注入 mock,再动态 import 真实 App.vue
                └── mock/        mock 层(见下)
```

## Mock 层:三处拦截

真实 App.vue 的后端依赖收敛在三个边界,mock 层各拦一处。

### ① Platform(会话数据 / 侧栏 / 设置 / 文件系统)

`mockPlatform.ts` 照 `desktop/frontend/src/platform/web.ts` 实现 `createMockPlatform(): Platform`,返回同形的 `{ caps, relay, sessions, system, events, templates, auxKeys, pluginHost }`,但数据来自内存:

- `relay.load()` 返回**已登录 + 已连接**的假 config,让 App 跳过登录直达主界面。`GetRelayConfig`(见 mockGoApp)返回 `connected + remote_proxy_url`,驱动远程会话列表连接。
- `sessions.listRemoteSessions()` 返回 `fakeSessions.ts`(5 个会话覆盖 running/waiting_input/completed/failed/idle,分 dev-server / macbook-pro 两个远程 host 组)。
- `pluginHost.fs` 接 `mockFs.ts` 的内存假文件树(见 ③)。
- `templates.load()` 返回 `DEFAULT_TEMPLATES`(快捷模板条:yes/ok/continue/commit/push/release)。

**caps 门控**(决定 App 走哪条 boot 链、显示哪些 UI):

| cap | 值 | 作用 |
|---|---|---|
| `localPty` | `true` | 显示「+ 新建 tab」按钮;boot 走桌面链的 auto-startNewTab |
| `wailsBindings` | `true` | 走桌面 boot 链(`getEndpoint`/`getHostInfo`/...),而非 web 简化链 |
| `pluginHost` | `true` | 打开文件浏览器 + 插件面板 |
| `windowControls` | `true` | 显示顶栏 TitleBar(会话标题 / 会话数 / uplink 状态 / 窗口控制) |
| `notifications` | 依浏览器 | 命令完成通知(降级为页面内 toast) |

### ② WebSocket(终端字节 / 会话列表 / 交互输入 / 文件帧)

真实前端的 `SessionListConnection`(`/client-sessions`)与 `SessionConnection`(`/client`)底层都 `new WebSocket()`。`mockSocket.ts` 用一个 `MockSocket` **替换全局 `WebSocket`**(`installMockWebSocket()`,在 `HomeDemo` 生命周期内 patch/restore),一次覆盖两条连接。用**真实 `lib/proto.ts` 帧编解码**,保证前端解析路径与线上一致。

行为(按 url host 与帧类型分流):

- **本机 vs 远程列表**:`GetEndpoint()` 返回 `ws://local.demo`(本机),relay 的 `remote_proxy_url` 是 `ws://remote.demo`。MockSocket 按 url 里是否含 `local.demo` 区分:本机列表推 `localSessions`,远程列表推 `fakeSessions`(都是 `SessionInfo[]` 形状,用 `id` 字段)。
- **回放**:收到 `ATTACH` 帧后,按 sid 找 `replayScripts.ts`(codex/claude/npm/go 各有脚本);无专属脚本的(新建本机会话)走 `IDLE_REPLAY`。用 `OUT` 帧(8 字节 big-endian seq + data)带打字机节奏发回。
- **交互输入**:收 `IN` 帧 → 逐字 echo;回车 → 查 `fakeCommands.ts`(ls/pwd/cat/echo/help 有假输出;codex/claude 触发 running→completed + 通知)。
- **driver 授予**:所有会话 attach 后发 `META` 帧把 `driver_client_id` 设为该连接的 client_id,让访客能输入(真实 atterm 里 remote 会话默认 viewer 只读,需 take over)。
- **文件帧**:收 `FS_REQUEST` 帧 → `createMockRemoteFS().handleFSRequest()` 生成 `FS_RESPONSE`。remote 会话的文件浏览器走这条路(见 ③)。

### ③ 文件系统(文件浏览器)

文件浏览器按活动会话归属选 fs bridge(`FileExplorer.vue`):
- `caps.pluginHost` 且 identity=local → `createLocalFSBridge(platform.pluginHost)` → mock 的 `pluginHost.fs`。
- remote 会话 → `createRemoteSessionFS(conn)` → 走 `SessionConnection` 的 `FS_REQUEST`/`FS_RESPONSE` 帧。

`mockFs.ts` 提供两者的数据源,背后是同一棵内存假文件树(`~/projects/atterm-demo`:README.md / main.go / package.json / logo.png / src/):
- `createMockPluginFs()` — `pluginHost.fs` 那套(local)。
- `createMockRemoteFS().handleFSRequest(req)` — 响应 remote 会话的 FS 帧,`data` 一律 base64(Go `[]byte` JSON 编码)。图片预览走 `read_chunk` → blob URL。

## 关键决策(踩过的坑)

- **本机会话必须走 auto-startNewTab,不预置在侧栏**。App.vue 的 `onSidebarOpen` → `openRemoteAsTab` 硬编码 `remote:true`;若预置一个「未打开」的本机会话在侧栏,点它会新建 remote:true pane,而 `tabSummaries` 用 `findSessionInfo(sid, remote=true)` 查 remoteList 找不到(本机会话在 localList)→ tab「(空)」+ 标「远端」。正解:`localSessions` 初始为空,由 boot 的 auto-startNewTab(caps.localPty)走 `spawnLocalShell` 正确创建(pane.remote=false)。
- **web tabs 快照 restore 被彻底关闭**。`config.mjs` alias `lib/webTabsSnapshot` → `webTabsSnapshotStub.ts`,`loadSnapshot` 恒 null、`saveSnapshot` no-op。否则用户浏览器残留的旧 web 模式快照会被 `restoreFromWebSnapshot`(硬编码 remote:true)恢复成 remote:true pane,重现「(空)+远端」。demo 是桌面模式,本就不该用 web 快照机制。
- **OPAQUE wasm 被桩替换**。`config.mjs` alias `lib/opaqueWasm` → `opaqueWasmStub.ts`。`opaqueWasm.ts` 会 `import './wasm_exec.js?url'`,而 `wasm_exec.js`/`opaque.wasm` 是 gitignore 的构建产物,CI checkout 后不存在 → 打包解析失败。demo 不走 OPAQUE 登录,桩掉整个模块。
- **`wailsjs/*` 被桩替换**。`wailsStub.ts` 提供 `wailsjs/go/main/App` 与 `wailsjs/runtime/runtime` 的具名导出(demo 走 web/mock 平台,不触达 Wails 绑定;残留静态 import 需兜底以通过打包)。
- **App 根节点必须 `height:100%`**。demo 框固定高度 + `overflow:hidden`,否则终端 + 模板条内容溢出被裁,快捷模板条看不见(`custom.css` 约束 `.home-demo-frame :deep(.app)`)。

## vite alias(`config.mjs`)

匹配顺序:**桩类 alias(`^.*wailsjs/...`、`^.*lib/opaqueWasm`、`^.*lib/webTabsSnapshot`)必须排在 `@`/`@shared` 之前**,否则被前缀 alias 抢先。

```text
@/…            → desktop/frontend/src/…      复用桌面前端源码
@shared/@webshared → web/src/shared/…        复用共享层
wailsjs/*      → mock/wailsStub.ts
lib/opaqueWasm → mock/opaqueWasmStub.ts
lib/webTabsSnapshot → mock/webTabsSnapshotStub.ts
dedupe: vue/pinia/naive-ui/xterm/xterm-addon-fit/vfonts → site/node_modules(避免重复实例)
ssr.noExternal: naive-ui/xterm/…(浏览器端组件,SSR 阶段不外部化)
```

## 部署

`.github/workflows/pages.yml`:push 到 `main`(paths 含 `site/**`、`desktop/frontend/src/**`、`web/src/shared/**`)触发。CI 先装 `desktop/frontend` + `web` + `site` 依赖(demo 复用前两者源码,其 import 的三方包需在解析路径上),跑 `npm test`(mock 层 vitest),`npm run docs:build`,再 `deploy-pages`。

- `config.mjs` 的 `base: '/atterm/'` 必须与仓库名一致,否则静态资源 404。
- 首次部署前需在仓库 Settings → Pages → Source 选「GitHub Actions」(否则 `deploy-pages` 报 403)。
- 纯 README 改动不触发(paths 不含),不会重新部署站点。

## 测试

`site/docs/.vitepress/theme/components/mock/*.test.ts`(vitest):守 `fakeSessions`(状态覆盖/字段形状)、`fakeCommands`(命中/未命中)、`mockFs`(listDir/readFile/write/chunk)、`mockSocket`(帧往返/回放/echo)、`mockGoApp`(本机会话增删)。端到端(真实 App 能否渲染 + 交互)由本地 `docs:dev` / `docs:preview` 人工过一遍。
