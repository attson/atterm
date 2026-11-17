# AT Term 站点

VitePress 站点,发布到 https://attson.github.io/atterm/。首页嵌入真实
`desktop/frontend/src/App.vue`,用 mock 后端(platform / WebSocket / 文件系统)
让访客体验:切换会话、终端回放、在 idle 会话敲命令、浏览并预览文件。真实 App
只在访客点击「开始体验」后加载,首屏使用 1200×630 的真实产品截图。

## 本地开发

```bash
cd site
npm install
npm run docs:dev      # http://localhost:5173/atterm/
npm run docs:build    # 产出 docs/.vitepress/dist
npm run docs:preview   # 预览构建产物
npm test               # mock 层单测(vitest)
npm run test:e2e       # 构建 + preview + Playwright 桌面/移动 smoke test
```

demo 复用 `../desktop/frontend/src` 与 `../web/src/shared` 源码,通过 vite alias
接入,零侵入前端。mock 层全部在 `docs/.vitepress/theme/components/mock/`:

| 文件 | 职责 |
|------|------|
| `mockPlatform.ts` | 照 `platform/web.ts` 实现的 mock `Platform`,已登录+已连接直达主界面，并提供输入历史 |
| `mockSocket.ts` | 替换全局 `WebSocket`:会话列表快照、终端回放、排队输入、交互回显、driver 授予、`PASTE_IMAGE` / `FS_REQUEST` 文件帧 |
| `mockGoApp.ts` | 桌面 binding 桩：终端外观设置、SSH 主机与批量 Snippet 等 App 级能力 |
| `mockInputHistory.ts` | 按 session 保存最近 200 条已提交命令 |
| `mockEvents.ts` | Platform 与 Go binding 共用事件总线，发送 Snippet 运行进度等事件 |
| `mockFs.ts` | 内存假文件树;`createMockRemoteFS` 响应 remote 会话的文件浏览器 |
| `fakeSessions.ts` | 5 个覆盖全状态的假会话(running/waiting/completed/failed/idle) |
| `replayScripts.ts` | 每会话 attach 后的终端回放脚本 |
| `fakeCommands.ts` | idle 会话的假命令响应表 |
| `wailsStub.ts` | `wailsjs/*` 静态 import 的桩(demo 不走 Wails) |

关键实现要点:
- **首页延迟加载**:`HomeDemo` 初始只渲染产品截图与单一体验入口;点击后才注入
  mock 并动态 import App chunk。卸载时恢复 WebSocket/Go/platform/i18n 全局状态。
  ESM 加载失败会缓存 rejection,因此重试使用一次性 query 标记做干净 reload,再自动
  启动 Demo,不是在同页重复已失败的 `import()`。
- **两条 WebSocket** 都被 mock:`SessionListConnection`(`/client-sessions`,推
  会话快照)与 `SessionConnection`(`/client`,终端 + 文件帧)。
- **remote 会话的文件浏览器走 `FS_REQUEST`/`FS_RESPONSE` 帧**(不是
  `pluginHost.fs`),由 `mockSocket` 调 `createMockRemoteFS().handleFSRequest`。
- **idle 会话经 META 帧授予 driver** 才能交互输入(真实 atterm 中 remote 会话
  默认 viewer 只读)。
- **终端输入保持顺序**：attach 初始回放和长任务输出期间先排队输入，批量模板动作按命令
  分隔，避免回放、输出和后续命令交叉；回车提交同步写入输入历史。Shell 行编辑器识别
  Left/Right/Home/End/Delete/Backspace，ANSI 控制序列不会进入命令文本。
- **AI 命令可交互**：裸 `codex` / `claude` / `aider` 进入对应的模拟交互提示符，后续输入
  作为 agent 指令处理，`/exit` 返回 shell；`codex exec <task>` 保留一次性长任务演示。
- **AI 会话可粘贴图片**：焦点位于终端时粘贴系统剪贴板图片，真实 `TerminalView` 会编码
  `PASTE_IMAGE` 帧；AI 交互态在当前输入行显示 `[Image #N]`，问题直接接在 token 后输入，
  原始 base64 不保存、不上传。Linux `Ctrl+V` 走 `mockGoApp` 的浏览器 Clipboard bridge；
  文件管理器复制出的图片路径会转成附件且不在终端回显文件名或本地绝对路径。粘贴后右侧
  toast 短暂显示缩略图和文件名，悬停 token 时在 token 附近显示大图；提交后输入历史保留
  `[Image #N]` token 和用户问题，与真实产品一致。
- **Teleport 浮层不离开 Demo**：`HomeDemo` 把 Snippet 弹窗限制在 Demo 框与当前视口的
  交集内，并在 resize/scroll 时同步边界；连接质量 drawer 也使用 Demo 内绝对定位。

## 部署

push 到 `main` 触发 `.github/workflows/pages.yml` 自动构建发布。

**首次部署前**:仓库 Settings → Pages → Source 选「GitHub Actions」,否则
`deploy-pages` 步骤会 403 失败。CI 先跑 mock 单测,再安装 Playwright Chromium;
E2E 步骤只构建一次并把同一份 `dist` 交给 Pages 上传。

`docs/.vitepress/config.mjs` 的 `base: '/atterm/'` 必须与仓库名一致,否则静态
资源 404。CI 会先装 `desktop/frontend` 与 `web` 依赖(demo 复用其源码),再装
`site` 依赖并 build。

## SEO 与分享元数据

`config.mjs` 为每个页面生成 canonical、Open Graph 和 Twitter Card 标签，首页另带
`SoftwareApplication` JSON-LD；中文与英文分别输出 `zh_CN` / `en_US` 和独立
canonical，`sitemap` 固定使用
`https://attson.github.io/atterm/`。新增 guide 时必须在 frontmatter 提供唯一的
`description`，不要复用站点默认描述。分享图直接使用
`docs/public/atterm-demo-poster.png` 的 1200×630 产品界面截图，Twitter 使用
`summary_large_image`。

GitHub Project Pages 不能控制 origin 根路径 `/robots.txt`，因此不在本仓库伪造
`/atterm/robots.txt`；索引控制依赖逐页 meta，sitemap 需通过搜索站长工具提交。

Relay 自带的 `web/*.html` 是登录和运行时应用壳，不是公开内容站，统一使用
`noindex, nofollow`；搜索入口只指向本站和 GitHub 仓库。
