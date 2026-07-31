# AT Term 站点

VitePress 站点,发布到 https://attson.github.io/atterm/。首页嵌入真实
`desktop/frontend/src/App.vue`,用 mock 后端(platform / WebSocket / 文件系统)
让访客直接体验:切换会话、终端回放、在 idle 会话敲命令、浏览并预览文件。

## 本地开发

```bash
cd site
npm install
npm run docs:dev      # http://localhost:5173/atterm/
npm run docs:build    # 产出 docs/.vitepress/dist
npm run docs:preview   # 预览构建产物
npm test               # mock 层单测(vitest)
```

demo 复用 `../desktop/frontend/src` 与 `../web/src/shared` 源码,通过 vite alias
接入,零侵入前端。mock 层全部在 `docs/.vitepress/theme/components/mock/`:

| 文件 | 职责 |
|------|------|
| `mockPlatform.ts` | 照 `platform/web.ts` 实现的 mock `Platform`,已登录+已连接直达主界面 |
| `mockSocket.ts` | 替换全局 `WebSocket`:会话列表快照、终端回放、交互回显、driver 授予、`FS_REQUEST` 文件帧 |
| `mockFs.ts` | 内存假文件树;`createMockRemoteFS` 响应 remote 会话的文件浏览器 |
| `fakeSessions.ts` | 5 个覆盖全状态的假会话(running/waiting/completed/failed/idle) |
| `replayScripts.ts` | 每会话 attach 后的终端回放脚本 |
| `fakeCommands.ts` | idle 会话的假命令响应表 |
| `wailsStub.ts` | `wailsjs/*` 静态 import 的桩(demo 不走 Wails) |

关键实现要点:
- **两条 WebSocket** 都被 mock:`SessionListConnection`(`/client-sessions`,推
  会话快照)与 `SessionConnection`(`/client`,终端 + 文件帧)。
- **remote 会话的文件浏览器走 `FS_REQUEST`/`FS_RESPONSE` 帧**(不是
  `pluginHost.fs`),由 `mockSocket` 调 `createMockRemoteFS().handleFSRequest`。
- **idle 会话经 META 帧授予 driver** 才能交互输入(真实 atterm 中 remote 会话
  默认 viewer 只读)。

## 部署

push 到 `main` 触发 `.github/workflows/pages.yml` 自动构建发布。

**首次部署前**:仓库 Settings → Pages → Source 选「GitHub Actions」,否则
`deploy-pages` 步骤会 403 失败。

`docs/.vitepress/config.mjs` 的 `base: '/atterm/'` 必须与仓库名一致,否则静态
资源 404。CI 会先装 `desktop/frontend` 与 `web` 依赖(demo 复用其源码),再装
`site` 依赖并 build。

## 待后续

README(仓库根)精简、把详细内容迁到本站,作为独立动作,尚未执行。
