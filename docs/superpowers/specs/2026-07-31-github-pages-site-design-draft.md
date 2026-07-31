# AT Term GitHub Pages 站点 — 设计文档

- **Date**: 2026-07-31
- **Status**: draft(设计已获用户逐节批准)
- **Topic**: 为 atterm 建一个 GitHub Pages 站点,首页可直接体验产品(复用真实前端 + mock 后端)
- **参考**: `~/GolandProjects/atstarter/site`(同作者的姊妹项目,已上线的 VitePress + 内嵌交互 demo)

## 1. 目标与范围

### 1.1 目标

1. 为 atterm 建一个正式站点,发布到 GitHub Pages(项目页 `https://attson.github.io/atterm/`)。
2. **首页可直接体验**:嵌入真实的 atterm 前端界面(桌面/web 共用的 `App.vue`),通过 mock 后端让它在纯静态环境里跑起来,无需任何服务端。用户能:切换会话、在终端里敲命令看输出、搜索/置顶会话、开设置/切主题、浏览并编辑文件(假文件系统)。
3. 站点承接 README 的完整文档内容(架构、远程接管、E2EE、部署 relay、AI agent 等),做成多页文档站。**README 精简作为后续独立动作**,本设计只负责把 site 建好。

### 1.2 明确不做(YAGNI)

- 不做真实 relay attach 握手、真 websocket、真 E2EE 解密 —— demo 全部走 mock。
- 不改动任何 `desktop/frontend` / `web` 源码 —— site 只做"外挂",零侵入。
- 首版只做中文(`zh-CN`);英文站后续再说。
- 不在本设计里精简 README(留作后续动作)。
- 文件浏览器只做"内存假文件系统",不接真实远程文件 channel。

### 1.3 成功标准

- `npm --prefix site run docs:build` 成功产出静态站点。
- 本地 `docs:dev` 打开首页,默认进入已登录/已连接的主界面(不出现登录页),能完成上述所有交互。
- push 到 main 后 GitHub Actions 自动构建并发布到 `attson.github.io/atterm/`,资源无 404。

## 2. 关键背景:atterm 前端架构(决定 mock 切入点)

- **web 与 desktop 共用核心前端**:`web/index.html` 挂载 `desktop/frontend/src/main.web.ts`,后者启动 `desktop/frontend/src/App.vue`。会话侧栏、终端、设置、文件浏览器都在 `desktop/frontend/src`。
- **Platform 抽象层**:核心前端通过 `initPlatform(factory)` 注入一个 `Platform` 对象,所有平台差异(数据来源、通知、剪贴板、文件系统)都收敛在这个接口后面。
  - `Platform = { caps, relay, sessions, system, events, templates, auxKeys, updater?, pluginHost? }`
  - web 平台的真实实现见 `desktop/frontend/src/platform/web.ts` —— 这是 mock 的**直接模板**。
- **终端字节流不走 platform**,走 websocket:`web/src/shared/ws/client-conn.ts`(`SessionConnection`),attach 会话后接收 PTY 字节帧。
- **文件浏览器**(`desktop/frontend/src/plugins/fileExplorer/`)由 `caps.pluginHost` 门控,数据经一个 `RemoteFileSystemBridge` 接口取得,有两种实现:
  - 本地:`platform.pluginHost.fs`(Wails)
  - 远程:`createRemoteSessionFS(conn)`(`remoteSessionFS.ts`,走 ws 文件帧)
  - 两者同形,mock 只需实现同一个 bridge 接口。

**结论**:mock 有三个干净切入点 —— ① Platform 层(会话/侧栏/设置)、② websocket 层(终端字节 + 交互回显)、③ RemoteFileSystemBridge(文件浏览器)。全部零侵入源码。

## 3. 站点结构与技术栈

- **技术栈**:VitePress ^1.6.3(与参考项目一致)。
- **位置**:仓库根新建 `site/`,独立 `package.json`,不污染现有前端。

```
site/
├── package.json                    # vitepress + 复用 web 的运行时依赖(vue/naive-ui/xterm/pinia/@noble 等)
├── .gitignore                      # node_modules/  docs/.vitepress/dist/  docs/.vitepress/cache/
└── docs/
    ├── index.md                    # 首页:hero + 交互 demo + 特性 + 截图 + 下载
    ├── guide/
    │   ├── index.md                #   介绍与快速上手(方式 A/B/C)
    │   ├── remote-takeover.md      #   远程接管 + 会话侧栏 + 通知
    │   ├── e2ee.md                 #   端到端加密 + 安全模型 + 远程权限
    │   ├── deploy-relay.md         #   部署 relay(Docker Compose / Go / Admin / 多实例)
    │   ├── ai-agents.md            #   AI agent 识别 + Feishu 远程终端
    │   └── faq.md                  #   FAQ / 故障排查
    └── .vitepress/
        ├── config.mjs              # base:'/atterm/'、lang、nav、sidebar、vite alias、dedupe
        └── theme/
            ├── index.js            # extends default theme,注册 custom.css
            ├── custom.css          # 首页样式(hero/badge/截图框/特性卡/场景卡)
            └── components/
                ├── HomeDemo.vue    # 嵌入真实 App.vue
                └── mock/
                    ├── mockPlatform.ts   # 照 web.ts 抄,数据换内存假数据
                    ├── mockSocket.ts     # 假 WebSocket:回放 + 交互回显
                    ├── mockFs.ts         # RemoteFileSystemBridge + 内存假文件树
                    ├── fakeSessions.ts   # 预置会话数组
                    ├── replayScripts.ts  # 每个会话的终端回放脚本
                    ├── fakeCommands.ts   # idle 会话的假命令响应表
                    └── wailsStub.ts      # wailsjs/* 静态 import 的兜底 stub
```

文档页内容从 README(`README.md`)和 `docs/spec/architecture.md`、`docs/spec/auth.md`、`docs/spec/protocol.md`、`docs/spec/feishu.md` 提炼。

## 4. 首页交互 demo:mock 架构

**原则**:两/三层拦截、零侵入源码。site 只提供 mock 实现,通过 vite alias 和 platform 注入接进真实前端。前端迭代后 demo 自动跟随,唯一维护点是 mock 层。

### 4.1 HomeDemo.vue

- 直接 import 真实 `desktop/frontend/src/App.vue` 和其样式/主题 CSS(参考 atstarter 的 `HomeDemo.vue`)。
- 在挂载前调用 `initPlatform(createMockPlatform)` 注入 mock 平台。
- 以 `embedded` 形式渲染 App(参考 atstarter 用 `<FrontendApp embedded />`;若 atterm App.vue 无 embedded prop,则用固定尺寸容器 + `overflow` 包裹,不改源码)。
- 顶部放一行提示:"这是纯前端 demo,数据都是假的,随便玩"。

### 4.2 ① Mock Platform(`mockPlatform.ts`)

照 `desktop/frontend/src/platform/web.ts` 结构复制 `createMockPlatform(): Platform`:

- `caps`:`localPty:false`、`wailsBindings:false`、`capacitor:false`,但 **`pluginHost:true`**(打开文件浏览器面板入口),`notifications` 依浏览器能力。
- `relay.load()`:返回一个**已登录 + 已连接**的假 `RelayConfig`(`url`、`token`、`connected:true`、`remote_permission:'full'`),让 App 跳过登录直达主界面。`fetchMe()` 返回假用户。
- `sessions.listRemoteSessions()`:返回 `fakeSessions.ts` 的预置数组(见 §5)。`markSessionsSeen` / `getPins` / `setPins` 走内存,不落 localStorage。
- `templates` / `auxKeys`:内存实现,返回少量预置模板。
- `system.showNotification`:优先浏览器原生 `Notification`,不可用时退化为页面内 toast(用于演示"任务完成通知")。`openExternalURL` 用 `window.open`。
- `pluginHost.fs`:接 `mockFs.ts`(见 §4.4)。

### 4.3 ② Mock WebSocket(`mockSocket.ts`)

终端连接走 `client-conn.ts` 的真实 `SessionConnection`,它内部 `new WebSocket(url)`。demo 环境用一个 `MockSocket` 类替换全局 `WebSocket`(仅在 demo 容器生命周期内 patch,或通过 vite alias 替换 `client-conn` 的 wsUrl/socket 工厂 —— 实现时选侵入更小的一种,在 plan 阶段定)。

`MockSocket` 行为:
- **attach 握手**:收到 attach 帧后,按当前会话 id 找到 `replayScripts.ts` 里的脚本。
- **回放**:把脚本按打字机节奏(逐块 + 小延迟)作为 PTY 输出帧发回,驱动终端渲染。`running` 会话回放到"进行中"停住,`completed`/`failed` 回放到结束态。
- **交互回显**(仅 idle 的 `zsh` 会话):收到用户输入帧 → 逐字 echo;遇回车 → 解析整行,查 `fakeCommands.ts`:
  - 命中(`ls`/`pwd`/`cat`/`echo`/`help`/`whoami` 等)→ 返回预置假输出 + 新 prompt。
  - 特定"长命令"(如 `codex ...`)→ 发送一段带 OSC 133 的输出,驱动任务状态 `running → completed`,并触发 `system.showNotification`。
  - 未命中 → `command not found` + 新 prompt。
- 必须正确实现终端要求的帧协议(参考 `web/src/shared/ws/protocol.ts` / `lib/connection.ts` 的帧编码)。

### 4.4 ③ Mock 文件系统(`mockFs.ts`)

实现 `RemoteFileSystemBridge` 接口(与 `plugins/fileExplorer/remoteSessionFS.ts` 同形),背后是内存假文件树:

```
~/projects/atterm-demo/
├── README.md          # markdown 预览
├── main.go            # 代码高亮 + 可编辑保存
├── package.json       # json 高亮
├── logo.png           # 图片预览(内置一张小图 base64)
└── src/
    ├── app.ts
    └── util.ts
```

- 支持:`listDir`(懒加载)、`readFile`、`fileMeta`、`writeFile`(写回内存)、`createFile`、`rename`、`remove`、`mkdir`、`trash`(回收站)。
- asset URL(图片/pdf 预览):用 `Blob` + `URL.createObjectURL`,`revokeAssetUrl` 时释放。
- 挂到 `platform.pluginHost.fs`,并让文件面板默认可打开;标注"演示用假文件系统"。

### 4.5 ④ vite alias(`config.mjs`,参考 atstarter)

- `@` → `desktop/frontend/src`;`@shared`/`@webshared` → `web/src/shared`(复用真实源码)。
- `wailsjs/go/main/App`、`wailsjs/runtime/runtime` → `mock/wailsStub.ts`(App.vue 里有静态 import,需兜底)。
- `dedupe: ['vue','pinia','naive-ui','xterm','xterm-addon-fit','vfonts']` → 全部解析到 `site/node_modules`,避免从 frontend 路径解析出重复实例。
- 视需要把 prefsSync / connhealth 等会主动发网络请求的模块也 alias 成 mock 空实现,避免 demo 里发真请求报错(实现时按报错逐个补,在 plan 阶段列清单)。

## 5. 假数据与故事线

### 5.1 预置会话(`fakeSessions.ts`)

分两个 host 组体现"多机器",覆盖全部任务状态:

| 会话 title | host 组 | task_state | 演示点 |
|---|---|---|---|
| `codex refactor auth`(📌 置顶) | macbook-pro | `running` | 置顶组 + 正跑 AI 任务(输出滚动) |
| `claude fix flaky test` | macbook-pro | `waiting_input` | AI 等输入(侧栏 attention 高亮) |
| `npm run build` | macbook-pro | `completed` | 命令完成(退出码 0、耗时) |
| `go test ./...` | dev-server | `failed` | 失败态(退出码 1,红色) |
| `zsh` | dev-server | `idle` | 空闲 shell,可自由敲命令 |

每个会话带 `cwd`、`current_command`、`summary`、`started_at` 等字段,数据形状对齐 `RemoteSession` 类型。

### 5.2 默认画面

- 已登录 + 已连接,直达主界面(无登录页)。
- 默认选中 `codex refactor auth`,终端回放一段 codex 工作输出(打字机效果)。
- 侧栏展示 5 会话的状态色 / 摘要 / 置顶 📌 组 / 两个 host 分组。

### 5.3 可玩交互清单(验收用)

1. 切会话 → 终端切到对应回放。
2. 在 idle `zsh` 敲 `ls`/`pwd`/`cat`/`echo`/`help` → 有假输出;跑"长命令" → 状态 `running→completed` + 通知。
3. `Cmd/Ctrl+F` 搜索 → 按 title/cwd 过滤会话(含折叠组展开)。
4. 右键会话 → 置顶,抽到 📌 组。
5. 顶栏 → 打开设置弹窗、切明暗主题。
6. 右侧文件面板 → 浏览假文件树、代码高亮、markdown/图片预览、编辑保存、新建/重命名/删除/回收站。
7. 右侧插件面板 → Quick Input / 翻译占位。

## 6. 首页非 demo 区块(参考 atstarter 首页骨架)

- Hero:`AT Term` + slogan + 按钮(下载 / 文档 / GitHub)。
- 徽章条:`E2EE` / `macOS·Linux·Windows` / `OSC 133 任务状态` / `MCP·AI` / `Web Push·Feishu` / `Apache-2.0`。
- 交互 demo(§4/§5)。
- 核心特性卡片:远程接管(lazy 同步)/ 会话状态与侧栏 / 多通道通知 / AI agent 识别 / 端到端加密。
- 截图区:优先用 README 已有截图;缺失则在 plan 阶段列出待补清单。
- 下载区:三平台 → GitHub Releases。

## 7. 部署(GitHub Actions)

- 新增 `.github/workflows/site.yml`:
  - 触发:push 到 `main`(可加 `paths: site/** desktop/frontend/** web/**`,前端变了 demo 也要重建)+ `workflow_dispatch`。
  - 步骤:checkout → setup-node(20)→ 安装 `site` 及所需前端依赖(demo 复用了 `desktop/frontend` / `web` 的源码,需保证这些包在构建环境可解析;具体装哪些在 plan 阶段定,参考 atstarter CI 的处理)→ `npm run docs:build` → upload-pages-artifact → deploy-pages。
  - 权限:`pages: write`、`id-token: write`。
- `config.mjs` 的 `base: '/atterm/'` 必须与仓库名一致,否则资源 404。
- 需在仓库 Settings → Pages 选 "GitHub Actions" 源(人工一次性操作,写进交付说明)。

## 8. 测试

- 对 mock 层写少量 `vitest` 单测(参考 atstarter 的 `homeDemoData.test.mjs` 等):
  - `fakeSessions`:数量、状态覆盖、字段形状合法。
  - `fakeCommands`:命中/未命中分支。
  - `mockFs`:listDir/readFile/writeFile/rename/trash 基本正确性。
- demo 能否真正渲染真实 App 依赖构建集成,单测只守 mock 数据结构不跑偏;端到端由本地 `docs:dev` 人工过一遍 §5.3 清单。

## 9. 风险与缓解

| 风险 | 缓解 |
|---|---|
| 真实 App.vue 启动链路调用了未预期的 platform / 网络方法,导致 demo 报错白屏 | 逐个报错补 mock/alias;plan 阶段先跑一次 `docs:dev` 摸清启动链真实调用面,再列 mock 清单 |
| 终端帧协议实现不对,回放/回显不显示 | 严格对照 `web/src/shared/ws/protocol.ts` 与 `lib/connection.ts` 的帧编码;先让"回放"跑通,再做"交互回显" |
| CI 里 demo 复用 frontend/web 源码,依赖解析失败 | 参考 atstarter CI 的依赖安装与 dedupe/alias 策略;必要时在 site 装齐运行时依赖 |
| 前端后续迭代破坏 mock 假设(接口改名) | mock 照 web.ts 抄且集中在 `mock/` 目录;接口变更时集中一处修;单测兜底数据结构 |
| `base` / Pages 源配置错误导致 404 | 交付说明写清 `base:'/atterm/'` 与 Pages 源设置 |

## 10. 交付物清单

1. `site/` 全套(package.json、config.mjs、theme、HomeDemo + mock 模块、index.md、guide/*.md)。
2. `.github/workflows/site.yml`。
3. mock 层 vitest 单测。
4. 交付说明:本地开发命令、Pages 源一次性设置、base 说明、"README 精简待后续"备注。
