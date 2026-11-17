# Demo 站点（site/）

> **Audience**: 维护 `site/`(GitHub Pages 首页交互 demo)的工程师
> **Last updated**: 2026-11-17
> **Status**: stable
> **See also**: [architecture.md](./architecture.md) · [protocol.md](./protocol.md) · [component-style.md](./component-style.md)

`site/` 是一个 VitePress 站点,发布到 GitHub Pages(<https://attson.github.io/atterm/>)。它有两个职责:

1. **产品手册** —— 面向产品用户与 Relay / Peer 运维者，按开始使用、桌面工作区、远程接管、自托管和参考资料组织中英文内容(`docs/guide/*.md` 与 `docs/en/guide/*.md`)；README 精简后详细内容都在这里。
2. **首页交互 demo** —— 首页先展示 1200×630 真实产品截图和单一「开始体验」入口；访客点击后才在浏览器里嵌入**真实**的 `desktop/frontend/src/App.vue`,用 mock 后端驱动,无需本地 relay 即可体验会话侧栏、终端回放、交互输入、文件浏览、快捷模板。

## 核心原则:零侵入

demo 复用真实前端源码(`desktop/frontend/src` + `web/src/shared`),**不改动它们一行**。所有差异都收敛在 `site/docs/.vitepress/theme/components/mock/` 与 `config.mjs` 的 vite alias 里。前端迭代后 demo 自动跟随,唯一维护点是 mock 层。

> 代价:mock 层与真实前端的接口(Platform / proto 帧 / FS 帧 / caps 门控)耦合。前端改这些接口时,mock 层要同步。改动集中在一处,且有 vitest 单测兜底数据结构。

## 目录

```text
site/
├── package.json                 vitepress + 复用 web 的运行时依赖(vue/naive-ui/xterm/pinia/@noble)
├── vitest.config.ts             mock 层单测的 alias(与 config.mjs 对齐)
└── docs/
    ├── index.md                 中文首页:hero + <HomeDemo/> + 连接路径 + 下载
    ├── guide/*.md               中文产品手册(总览 + 任务章节，保留既有公开 URL)
    ├── en/index.md              英文首页
    ├── en/guide/*.md            与中文信息架构对应的英文产品手册
    └── .vitepress/
        ├── config.mjs           base:'/atterm/'、nav/sidebar、vite alias、dedupe、ssr.noExternal
        └── theme/
            ├── index.js         extends 默认主题,注册 HomeDemo + custom.css + Pinia
            ├── custom.css       首页版式 + 暗色背景统一 + hero 间距
            └── components/
                ├── HomeDemo.vue 用户点击体验入口后注入 mock,再动态 import 真实 App.vue
                ├── ReleaseDownloads.vue 中英共用的稳定 release 直链与校验入口
                └── mock/        mock 层(见下)
```

## 产品手册信息架构

- `/guide/` 与 `/en/guide/` 是统一产品手册总览；安装内容分别位于
  `/guide/getting-started` 与 `/en/guide/getting-started`。
- 侧栏固定按五组组织：开始使用、桌面工作区、远程接管、自托管、参考资料；英文使用
  对应的 `Get started`、`Desktop workspace`、`Remote access`、`Self-hosting`、`Reference`。
- 既有专题 URL（remote-takeover、e2ee、deploy-relay、deploy-rendezvous、ai-agents、faq）
  直接成为手册章节，不复制第二份内容，也不因分组调整迁移路径。
- 核心工作流使用稳定的任务型 URL：`workspace`、`ssh-files`、`productivity`、`settings` 与
  `remote-clients`。中文和英文页面一一对应；平台专属能力必须明确标注 desktop / Web / PWA /
  iOS 的适用范围，不能把 capability-gated 设置泛化到所有客户端。
- 自托管手册除 Relay、Rendezvous 与 E2EE 外，使用 `admin` 章节承载邀请、用户角色、运行时
  配置、Feishu、Traffic 和健康检查；公开手册只说明稳定运维行为，数据库表、wire tag 与内部
  handler 仍留在 `docs/spec/`。
- 中英文在同一次改动中维护相同的信息架构。新增 guide 必须同时加入对应 locale 的侧栏，
  提供唯一 frontmatter `description`，并覆盖构建、路由和移动端无溢出验收。
- 手册截图存放在 `docs/public/manual/`，只选能显著降低理解成本的关键流程。截图必须来自首页
  复用的真实 `App.vue` demo 和仓库 mock 数据，不能包含本机真实路径、账号、主机或凭据；当前
  基线为工作区分屏、文件浏览与输入历史三张。
- E2E 必须机械比较中英文侧栏 path（去掉 `/en` 后一致），并遍历产品手册正文内链确认返回成功；
  VitePress build 的 dead-link 检查与这项浏览器契约共同防止导航或 locale 漂移。
- VitePress 1.6 的 locale 专属 `nav` / `sidebar` 必须放在顶层
  `locales.<locale>.themeConfig`；`themeConfig.locales` 不会把这些字段合并到当前页面主题，
  会导致导航和侧栏静默不渲染。全局搜索与社交链接仍放在顶层 `themeConfig`。

## Mock 层:三处拦截

真实 App.vue 的后端依赖收敛在三个边界,mock 层各拦一处。

首页初始态不得 import App chunk。`HomeDemo` 在「开始体验」点击后才安装 mock、初始化
platform/i18n 并动态导入真实 App；卸载或失败时恢复 WebSocket、`window.go`、platform 与
i18n，取消事件订阅和 timer，不删除同源 localStorage。移动端直接使用真实 App 已有的窄屏
布局，不为 demo 强制桌面最小宽度。

VitePress 构建会把动态导入的 `desktop/frontend/src/style.css` 提取进公共 CSS；其中全局
`html/body` 的桌面深色背景、文字色和 14px 基线不得泄漏到文档站。`custom.css` 必须用
更高 specificity 把站点画布绑定到 `--vp-c-bg` / `--vp-c-text-1` 和 16px，`HomeDemo` 再在
`.app` 内恢复桌面端颜色、字体与 14px 基线。真实 App 位于 `.vp-doc` 内时还要隔离文档标题
规则；例如输入历史标题必须清除 VitePress `h2` 的上边框和 `24px` 顶部 padding，保持 38px
工具栏高度。亮色和暗色都必须验证，不能只看默认暗色。

App chunk 请求失败后，浏览器会缓存该 ESM dynamic import 的 rejection，同页再次 `import()`
不会重新发请求。因此错误态的「重试」必须用一次性 `atterm-demo-retry` query 标记执行干净
reload；新页面先移除标记，再自动启动 Demo。不要改回同页直接 import 的假重试。

真实 App 的 Snippet 弹窗会 Teleport 到 `body`。Demo 运行时必须由 `HomeDemo` 维护页面级
边界变量，把该浮层限制在 Demo 框与当前视口的交集内，并在 resize/scroll 时同步；卸载时
移除 class、CSS 变量、observer 和 listener。连接质量 drawer 同样不得越出 Demo 框。

### ① Platform(会话数据 / 侧栏 / 设置 / 文件系统)

`mockPlatform.ts` 照 `desktop/frontend/src/platform/web.ts` 实现 `createMockPlatform(): Platform`,返回同形的 `{ caps, relay, sessions, system, events, templates, auxKeys, pluginHost }`,但数据来自内存:

- `relay.load()` 返回**已登录 + 已连接**的假 config,让 App 跳过登录直达主界面。`GetRelayConfig`(见 mockGoApp)返回 `connected + remote_proxy_url`,驱动远程会话列表连接。
- `sessions.listRemoteSessions()` 返回 `fakeSessions.ts`(5 个会话覆盖 running/waiting_input/completed/failed/idle,分 dev-server / macbook-pro 两个远程 host 组)。
- `sessions.getInputHistory()` 从 `mockInputHistory.ts` 返回每个 session 最近 200 条已提交命令，关闭本机会话时同步清理。
- `pluginHost.fs` 接 `mockFs.ts` 的内存假文件树(见 ③)。
- `templates.load()` 返回 `DEFAULT_TEMPLATES`(快捷模板条:yes/ok/continue/commit/push/release)。
- `events` 与 `mockGoApp.ts` 共用 `mockEvents.ts`；`ListSSHHosts` 返回示例主机，`RunSnippetOnHosts` 通过该总线发送运行进度。
- 终端主题、字体、字号、行高、光标和 scrollback 的 setter/getter 必须成对实现，避免设置页调用真实 binding 时抛 `is not a function`。
- Linux 浏览器的 `Ctrl+V` 会走桌面剪贴板桥；`mockGoApp.GetClipboardPastePayload` 必须用 Clipboard API 适配出与真实 Wails binding 相同的 image/text payload，不能只依赖浏览器 `paste` 事件。

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
- **交互输入**:收 `IN` 帧后由内存行编辑器维护文本与光标位置；Left/Right/Home/End/Delete/Backspace 只改变编辑状态和终端显示，ANSI 控制序列不得进入命令文本。回车后查 `fakeCommands.ts`(ls/pwd/cat/echo/help 有假输出)并写入输入历史。裸 `codex`/`claude`/`aider` 进入可继续输入、以 `/exit` 返回 shell 的模拟交互态；带参数的一次性 AI 命令才走 running→completed + 通知。attach 初始回放和长任务输出期间必须排队输入；连续批量模板动作必须保持命令边界，不能拼成一条命令。
- **图片粘贴**:真实 `TerminalView` 从系统剪贴板取得图片并通过现有 `PASTE_IMAGE (0x33)` 帧发送。MockSocket 校验 `image/*`、合法非空 base64 与 10 MiB 上限，不保留 base64、不上传、不持久化。agent 交互态把 `[Image #N]` 作为待提交 token 留在当前输入行，用户问题直接接在其后；token 不进入 agent 命令文本，但提交后必须与问题一起写入输入历史，回车或取消后清空，终端内不显示文件名或解释性成功日志。粘贴时复用真实 `PasteImagePreviewHost` 在右侧显示 5 秒缩略图 toast；悬停 token 时复用真实终端图片预览，在 token 附近显示大图。shell 态保留 MIME/大小诊断。Linux Clipboard API 只返回本地图片路径/`file://` URI 时，mock bridge 用 240×160 内存占位图片转成附件，不暴露绝对路径。非法 payload 显示错误但不击穿会话。
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

`.github/workflows/pages.yml`:push 到 `main`(paths 含 `site/**`、`desktop/frontend/src/**`、`web/src/shared/**`)触发。CI 先装 `desktop/frontend` + `web` + `site` 依赖(demo 复用前两者源码,其 import 的三方包需在解析路径上),跑 `npm test`(mock 层 vitest)，安装 Playwright Chromium，再用 `npm run test:e2e` 完成一次 build + preview + 浏览器验收；该步骤留下的 `dist` 直接交给 `deploy-pages`，不重复构建。

- `config.mjs` 的 `base: '/atterm/'` 必须与仓库名一致,否则静态资源 404。
- 首次部署前需在仓库 Settings → Pages → Source 选「GitHub Actions」(否则 `deploy-pages` 报 403)。
- 纯 README 改动不触发(paths 不含),不会重新部署站点。

## SEO 与索引边界

- `config.mjs::transformHead` 按页面输出 canonical、Open Graph 与 Twitter Card；首页
  额外输出 `SoftwareApplication` JSON-LD。中文页使用 `zh_CN`，英文 `/en/` 使用
  `en_US`，各自 canonical 必须使用公开站点路径，不能指向 Relay 或 Vite 开发地址。
- VitePress sitemap 以 `https://attson.github.io/atterm/` 为公开索引入口。GitHub
  Project Pages 无法控制 origin 根路径 `/robots.txt`，不要添加无效的
  `/atterm/robots.txt`；sitemap 需通过搜索站长工具提交。新增 guide 必须提供唯一的
  frontmatter `description`，避免所有搜索结果复用站点默认摘要。
- `web/*.html` 是用户自托管 Relay 的鉴权与应用壳，包含登录态和运行时界面，必须保留
  `noindex, nofollow`；不要把这些页面 canonical 到某个具体 Relay 实例。
- 分享图固定为 `site/docs/public/atterm-demo-poster.png` 的 1200×630 真实产品界面截图，
  Twitter card 使用 `summary_large_image`。标题与描述必须和 README、GitHub About 的
  产品定位一致，不把已退休功能写进关键词或摘要。

## 首页内容与下载边界

- Hero 用一句产品定位加一组能力标签，不再用长段 tagline 罗列场景。中英文首页的
  `hero.highlights` 必须语义对称，只列已落地能力；当前覆盖 AI 任务状态、远程查看与
  接管、SSH 主机管理、文件浏览与编辑、端到端加密、配置同步和全平台支持。标签通过
  `HeroHighlights.vue` 注入 `home-hero-info-after`，在窄屏自然换行且不得造成横向滚动。
- 首页必须区分本地终端/SSH、账户 Relay、Peer Space 三条连接路径；AI 能力按 agent
  分开陈述，不能把 Claude Code 独有的 Notification hook / 飞书 AskUserQuestion 或
  Claude/Codex 的 session-id 恢复泛化成所有 agent 都支持。
- 推荐桌面构件使用 GitHub latest release 的稳定名称直链：
  `AT-Term-darwin-{arm64,amd64}.zip`、`AT-Term-linux-{amd64,arm64}.tar.gz`、
  `AT-Term-windows-amd64.zip`。版本化 dmg/deb/exe 等构件留在 Releases 列表选择。
- 下载区必须同时提供 `SHA256SUMS` 与 `SHA256SUMS.sig`。自动更新是强制验签再校验 hash；
  手动下载只提供同一份校验材料，不能表述成浏览器会自动完成验证。
- macOS 构件在完成签名/公证前就地提示 Gatekeeper；iOS 没有公开 App Store/TestFlight
  构件时只链接源码构建说明，不创建不存在的下载入口。

## 测试

`site/docs/.vitepress/theme/components/mock/*.test.ts`(vitest):守 `fakeSessions`(状态覆盖/字段形状)、`fakeCommands`(命中/未命中/交互 agent)、`mockFs`(listDir/readFile/write/chunk)、`mockSocket`(帧往返/回放/排队输入/行编辑/agent 进入退出/图片粘贴/echo)、`mockGoApp`(本机会话增删/健康快照/终端外观/SSH 主机/浏览器剪贴板/全局恢复)。当前基线为 40 个单测。

`site/tests/e2e/*.spec.ts`(Playwright):对构建产物验收首屏定位与中英文能力标签、首屏不请求 App chunk、单一体验入口、默认 Shell 终端回放、AI 会话图片粘贴与后续输入、输入历史、SSH Snippet、主题切换、浮层边界、加载失败后干净重试、卸载恢复全局绑定且保留用户 localStorage、390px/1024px 无横向溢出、亮色/暗色画布与主题变量一致、控制台错误、稳定下载直链、中英文路由、locale/canonical、1200×630 分享图。当前基线为 13 个 E2E；CI 必须执行，人工预览只补视觉检查，不替代 smoke test。
