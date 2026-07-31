# AT Term GitHub Pages 站点 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建一个 VitePress GitHub Pages 站点,首页复用真实 atterm 前端 `App.vue` 并用 mock 后端(platform / WebSocket / 文件系统三层拦截)让用户直接体验,同时承接 README 文档,经 GitHub Actions 发布到 `attson.github.io/atterm/`。

**Architecture:** 站点在仓库根 `site/`,用 VitePress。首页 `HomeDemo.vue` import 真实 `desktop/frontend/src/App.vue`,在挂载前 `initPlatform(createMockPlatform)` 注入 mock 平台;全局 `WebSocket` 被一个 `MockSocket` 替换,用真实 `lib/proto.ts` 帧协议回放终端输出并处理交互回显;文件浏览器注入内存假 fs。vite alias 把 `@`/`@webshared`/`wailsjs/*` 指到真实源码或 mock stub。零侵入现有前端源码。

**Tech Stack:** VitePress 1.6.x、Vue 3.5、Naive UI、xterm 5.3、Pinia 3、vitest;复用 `desktop/frontend/src` 与 `web/src/shared` 源码;GitHub Actions Pages 部署。

**参考实现:** `~/GolandProjects/atstarter/site`(同作者姊妹项目,已上线),其 `docs/.vitepress/config.mjs`、`HomeDemo.vue`、`mockWailsApp.mjs`、`.github/workflows/pages.yml` 是直接可借鉴的模板。

**Spec:** `docs/superpowers/specs/2026-07-31-github-pages-site-design-draft.md`

---

## 关键代码事实(实现时必读,避免臆测)

- **前端共用**:`web/index.html` 挂 `desktop/frontend/src/main.web.ts` → `App.vue`。核心界面全在 `desktop/frontend/src`。
- **Platform 注入**:`desktop/frontend/src/platform/index.ts` 的 `initPlatform(factory)`。`Platform` 接口在 `platform/types.ts`;web 真实实现 `platform/web.ts`(mock 模板)。
- **App.vue 启动链实际调用**(grep 确认):`$platform.caps`、`$platform.relay.load()`、`$platform.relay.fetchMe()`、`$platform.sessions.listRemoteSessions()`、`$platform.system.getEnvironment()`、`$platform.system.showNotification`、`$platform.events.on/emit`、`$platform.templates`、`$platform.auxKeys`、`$platform.pluginHost.getPluginConfig()`(经 `plugins/configStore.ts` 的 `usePluginConfigStore().load()`)。
- **两条 WebSocket**(都在 `desktop/frontend/src/lib/connection.ts`,底层 `new WebSocket`):
  - `SessionListConnection`(会话列表流)— 发 `TYPE.LIST`,收 `TYPE.LIST_RESP`(JSON 会话数组)。
  - `SessionConnection`(单会话终端)— 发 `TYPE.ATTACH`/`TYPE.IN`/`TYPE.RESIZE`;收 `TYPE.OUT`(PTY 字节);文件走 `TYPE.FS_REQUEST`/`TYPE.FS_RESPONSE`。
- **帧协议** `desktop/frontend/src/lib/proto.ts`:6 字节头(`VERSION`+type+u32BE payloadLen)+ 16 字节 sid + payload。有 `encodeFrame`/`decodeFrame`/`TYPE`/`uuidParse`/`encodeText`。**mock 复用它,不自己造帧。**
- **文件浏览器**:`plugins/fileExplorer/`,由 `caps.pluginHost` 门控,面板启用看 `pluginStore.isPluginEnabled("file-explorer")`(即 `cfg.fileExplorer.enabled`)。fs bridge 接口 `plugins/fileExplorer/fsBridge.ts` 的 `FileSystemBridge`;远程实现 `remoteSessionFS.ts` 的 `createRemoteSessionFS(conn)`。**mock 直接实现 `FileSystemBridge` 注入,不走 ws 文件帧。**
- **PluginConfig 形状**:`wailsjs/go/models` 的 `main.PluginConfig`,含 `fileExplorer.{enabled,showHidden,showLineNumbers,panelWidthPx,panelCollapsed}` 与 `translate.{enabled,...}`、`shortcuts`。

## 决策(锁定 spec 中保留的选择)

- **mockSocket 用「patch 全局 `WebSocket`」**:一次拦截同时覆盖 `SessionListConnection` 与 `SessionConnection`,统一在 `HomeDemo` 生命周期内 patch/restore。
- **文件浏览器用「直接注入 `FileSystemBridge`」**:不通过 ws 文件帧。若 App 只从 `createRemoteSessionFS(conn)` 造 fs,则在 alias 层把 `remoteSessionFS` 换成 mock 工厂(见 Task 9)。

## File Structure

```
site/
├── package.json
├── package-lock.json                # npm install 生成
├── .gitignore
└── docs/
    ├── index.md
    ├── guide/{index,remote-takeover,e2ee,deploy-relay,ai-agents,faq}.md
    └── .vitepress/
        ├── config.mjs
        └── theme/
            ├── index.js
            ├── custom.css
            └── components/
                ├── HomeDemo.vue
                └── mock/
                    ├── proto-reexport.ts     # 从真实 lib/proto 再导出,便于 mock 用
                    ├── fakeSessions.ts
                    ├── fakeSessions.test.ts
                    ├── replayScripts.ts
                    ├── fakeCommands.ts
                    ├── fakeCommands.test.ts
                    ├── mockFs.ts
                    ├── mockFs.test.ts
                    ├── mockSocket.ts
                    ├── mockPlatform.ts
                    └── wailsStub.ts
.github/workflows/pages.yml
```

---

### Task 1: site 骨架 + VitePress 起步

**Files:**
- Create: `site/package.json`
- Create: `site/.gitignore`
- Create: `site/docs/index.md`(临时最小首页,后续 Task 10 覆盖)
- Create: `site/docs/.vitepress/config.mjs`

- [ ] **Step 1: 写 `site/package.json`**

```json
{
  "name": "atterm-site",
  "private": true,
  "type": "module",
  "scripts": {
    "docs:dev": "vitepress dev docs",
    "docs:build": "vitepress build docs",
    "docs:preview": "vitepress preview docs",
    "test": "vitest run docs/.vitepress/theme/components/mock"
  },
  "devDependencies": {
    "vitepress": "^1.6.3",
    "vitest": "^2.1.8"
  },
  "dependencies": {
    "vue": "3.5.13",
    "pinia": "3.0.4",
    "naive-ui": "2.40.4",
    "xterm": "5.3.0",
    "xterm-addon-fit": "0.8.0",
    "vfonts": "0.1.0",
    "@noble/ciphers": "^2.2.0",
    "@noble/hashes": "^2.2.0"
  }
}
```

> 依赖版本对齐 `web/package.json`,保证 dedupe 后与真实前端同一套运行时。

- [ ] **Step 2: 写 `site/.gitignore`**

```
node_modules/
docs/.vitepress/dist/
docs/.vitepress/cache/
```

- [ ] **Step 3: 写临时 `site/docs/index.md`**

```markdown
---
layout: home
hero:
  name: AT Term
  text: 带远程接管的跨平台终端
  tagline: 桌面端启动的 shell / AI 任务,离开电脑后从手机、浏览器继续查看和输入。
---
```

- [ ] **Step 4: 写 `site/docs/.vitepress/config.mjs`(先不含 demo alias,Task 9 再补)**

```javascript
import { defineConfig } from 'vitepress'

// 项目页部署在 https://attson.github.io/atterm/,base 必须与仓库名一致,
// 否则静态资源 404。
export default defineConfig({
  base: '/atterm/',
  lang: 'zh-CN',
  title: 'AT Term',
  description: '带远程接管能力的跨平台终端(桌面 + 浏览器 + 手机)',
  themeConfig: {
    nav: [
      { text: '首页', link: '/' },
      { text: '文档', link: '/guide/' },
      { text: '部署 Relay', link: '/guide/deploy-relay' },
      { text: '下载', link: 'https://github.com/attson/atterm/releases/latest' },
    ],
    sidebar: {
      '/guide/': [
        {
          text: '使用文档',
          items: [
            { text: '介绍与快速上手', link: '/guide/' },
            { text: '远程接管与会话侧栏', link: '/guide/remote-takeover' },
            { text: '端到端加密与安全', link: '/guide/e2ee' },
            { text: '部署 Relay', link: '/guide/deploy-relay' },
            { text: 'AI Agent 与 Feishu', link: '/guide/ai-agents' },
            { text: 'FAQ / 故障排查', link: '/guide/faq' },
          ],
        },
      ],
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/attson/atterm' }],
    search: { provider: 'local' },
  },
})
```

- [ ] **Step 5: 安装依赖并验证空站点能 build**

Run:
```bash
cd site && npm install && npm run docs:build
```
Expected: build 成功,产出 `site/docs/.vitepress/dist/`,无报错。

- [ ] **Step 6: Commit**

```bash
cd ~/GolandProjects/atterm
git add site/package.json site/package-lock.json site/.gitignore site/docs/index.md site/docs/.vitepress/config.mjs
git commit -m "feat(site): scaffold VitePress site skeleton"
```

---

### Task 2: 假会话数据 `fakeSessions.ts`

**Files:**
- Create: `site/docs/.vitepress/theme/components/mock/fakeSessions.ts`
- Test: `site/docs/.vitepress/theme/components/mock/fakeSessions.test.ts`

参考 `desktop/frontend/src/platform/types.ts` 的 `RemoteSession` 字段。

- [ ] **Step 1: 写失败测试 `fakeSessions.test.ts`**

```typescript
import { describe, it, expect } from 'vitest'
import { fakeSessions, IDLE_SESSION_ID } from './fakeSessions'

describe('fakeSessions', () => {
  it('covers all task states', () => {
    const states = fakeSessions.map((s) => s.task_state)
    expect(states).toContain('running')
    expect(states).toContain('waiting_input')
    expect(states).toContain('completed')
    expect(states).toContain('failed')
    expect(states).toContain('idle')
  })

  it('every session has a 36-char uuid session_id and required fields', () => {
    for (const s of fakeSessions) {
      expect(s.session_id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/)
      expect(typeof s.title).toBe('string')
      expect(typeof s.host).toBe('string')
      expect(s.cols).toBeGreaterThan(0)
      expect(s.rows).toBeGreaterThan(0)
    }
  })

  it('spans two hosts', () => {
    const hosts = new Set(fakeSessions.map((s) => s.host))
    expect(hosts.size).toBe(2)
  })

  it('exposes the idle session id present in the list', () => {
    expect(fakeSessions.some((s) => s.session_id === IDLE_SESSION_ID)).toBe(true)
    expect(fakeSessions.find((s) => s.session_id === IDLE_SESSION_ID)?.task_state).toBe('idle')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd site && npx vitest run docs/.vitepress/theme/components/mock/fakeSessions.test.ts`
Expected: FAIL,`Cannot find module './fakeSessions'`。

- [ ] **Step 3: 写 `fakeSessions.ts`**

```typescript
import type { RemoteSession } from '@/platform/types'

// 固定 uuid,mock 各处用它们做 sid → 脚本/命令表映射。
export const CODEX_SESSION_ID = '11111111-1111-4111-8111-111111111111'
export const CLAUDE_SESSION_ID = '22222222-2222-4222-8222-222222222222'
export const BUILD_SESSION_ID = '33333333-3333-4333-8333-333333333333'
export const GOTEST_SESSION_ID = '44444444-4444-4444-8444-444444444444'
export const IDLE_SESSION_ID = '55555555-5555-4555-8555-555555555555'

const HOST_MBP = 'macbook-pro'
const HOST_DEV = 'dev-server'
const now = 1_753_900_000 // 固定时间戳,避免测试非确定性

export const fakeSessions: RemoteSession[] = [
  {
    session_id: CODEX_SESSION_ID, host_id: HOST_MBP, host: HOST_MBP, user: 'you',
    title: 'codex refactor auth', cwd: '~/projects/atterm', cols: 120, rows: 32,
    started_at: now - 300, task_state: 'running', current_command: 'codex exec "refactor auth"',
    command_started_at: now - 60, type: 'codex', remote_permission: 'full',
  },
  {
    session_id: CLAUDE_SESSION_ID, host_id: HOST_MBP, host: HOST_MBP, user: 'you',
    title: 'claude fix flaky test', cwd: '~/projects/atterm', cols: 120, rows: 32,
    started_at: now - 600, task_state: 'waiting_input', current_command: 'claude',
    command_started_at: now - 120, type: 'claude', remote_permission: 'full',
  },
  {
    session_id: BUILD_SESSION_ID, host_id: HOST_MBP, host: HOST_MBP, user: 'you',
    title: 'npm run build', cwd: '~/projects/atterm/web', cols: 100, rows: 28,
    started_at: now - 900, task_state: 'completed', current_command: 'npm run build',
    command_started_at: now - 200, command_ended_at: now - 188, command_duration_ms: 12000,
    command_exit_code: 0, type: 'shell', remote_permission: 'full',
  },
  {
    session_id: GOTEST_SESSION_ID, host_id: HOST_DEV, host: HOST_DEV, user: 'you',
    title: 'go test ./...', cwd: '~/srv/atterm', cols: 110, rows: 30,
    started_at: now - 1200, task_state: 'failed', current_command: 'go test ./...',
    command_started_at: now - 400, command_ended_at: now - 380, command_duration_ms: 20000,
    command_exit_code: 1, type: 'shell', remote_permission: 'full',
  },
  {
    session_id: IDLE_SESSION_ID, host_id: HOST_DEV, host: HOST_DEV, user: 'you',
    title: 'zsh', cwd: '~/srv/atterm', cols: 110, rows: 30,
    started_at: now - 1500, task_state: 'idle', type: 'shell', remote_permission: 'full',
  },
]
```

> 若 `RemoteSession` 缺某必填字段导致 TS 报错,按 `platform/types.ts` 实际定义补默认值;可选字段(`?`)不写。

- [ ] **Step 4: 运行确认通过**

Run: `cd site && npx vitest run docs/.vitepress/theme/components/mock/fakeSessions.test.ts`
Expected: PASS(4 个用例)。

> 若 `@/` alias 在 vitest 下不解析,先加最小 `site/vitest.config.ts`:
> ```typescript
> import { defineConfig } from 'vitest/config'
> import { fileURLToPath, URL } from 'node:url'
> export default defineConfig({
>   resolve: { alias: {
>     '@': fileURLToPath(new URL('../desktop/frontend/src', import.meta.url)),
>     '@webshared': fileURLToPath(new URL('../web/src/shared', import.meta.url)),
>   } },
> })
> ```
> 路径相对 `site/` 根。

- [ ] **Step 5: Commit**

```bash
git add site/docs/.vitepress/theme/components/mock/fakeSessions.ts site/docs/.vitepress/theme/components/mock/fakeSessions.test.ts site/vitest.config.ts
git commit -m "feat(site): add fake session fixtures for home demo"
```

---

### Task 3: 假命令响应表 `fakeCommands.ts`

**Files:**
- Create: `site/docs/.vitepress/theme/components/mock/fakeCommands.ts`
- Test: `site/docs/.vitepress/theme/components/mock/fakeCommands.test.ts`

- [ ] **Step 1: 写失败测试**

```typescript
import { describe, it, expect } from 'vitest'
import { runFakeCommand } from './fakeCommands'

describe('runFakeCommand', () => {
  it('returns output for known commands', () => {
    expect(runFakeCommand('pwd').output).toContain('~/srv/atterm')
    expect(runFakeCommand('whoami').output).toContain('you')
    expect(runFakeCommand('ls').output).toContain('README.md')
    expect(runFakeCommand('echo hi').output).toBe('hi\r\n')
    expect(runFakeCommand('help').output.length).toBeGreaterThan(0)
  })

  it('reports not-found for unknown commands', () => {
    const r = runFakeCommand('frobnicate')
    expect(r.output).toContain('command not found')
    expect(r.longRunning).toBeFalsy()
  })

  it('flags a long-running AI command that drives task state', () => {
    const r = runFakeCommand('codex exec "add feature"')
    expect(r.longRunning).toBe(true)
    expect(r.finalState).toBe('completed')
    expect(r.steps.length).toBeGreaterThan(0)
  })

  it('ignores empty input', () => {
    expect(runFakeCommand('   ').output).toBe('')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd site && npx vitest run docs/.vitepress/theme/components/mock/fakeCommands.test.ts`
Expected: FAIL,模块不存在。

- [ ] **Step 3: 写 `fakeCommands.ts`**

```typescript
// 假命令响应表:供 idle 会话的 mock 终端解析用户输入。output 用 \r\n 换行
// 以匹配 PTY 语义。longRunning 命令返回分步输出 + 最终任务状态,由 mockSocket
// 播放并触发通知。
export interface FakeCommandResult {
  output: string
  longRunning?: boolean
  steps?: string[]        // 逐步输出(打字机),仅 longRunning
  finalState?: 'completed' | 'failed'
}

const LS = [
  'README.md  main.go  package.json  logo.png  src/',
].join('')

export function runFakeCommand(raw: string): FakeCommandResult {
  const line = raw.trim()
  if (!line) return { output: '' }
  const [cmd, ...rest] = line.split(/\s+/)
  const arg = rest.join(' ')

  switch (cmd) {
    case 'pwd':    return { output: '~/srv/atterm\r\n' }
    case 'whoami': return { output: 'you\r\n' }
    case 'ls':     return { output: LS + '\r\n' }
    case 'echo':   return { output: (stripQuotes(arg)) + '\r\n' }
    case 'cat':    return { output: catFile(arg) }
    case 'date':   return { output: 'Fri Jul 31 10:00:00 UTC 2026\r\n' }
    case 'help':   return { output: helpText() }
    case 'clear':  return { output: '\x1b[2J\x1b[H' }
    case 'codex':
    case 'claude':
    case 'aider':
      return {
        output: '',
        longRunning: true,
        finalState: 'completed',
        steps: [
          `\x1b[36m▸ ${cmd}\x1b[0m starting…\r\n`,
          'reading project files…\r\n',
          'planning changes…\r\n',
          'applying patch to auth.go…\r\n',
          '\x1b[32m✓ done\x1b[0m (3 files changed)\r\n',
        ],
      }
    default:
      return { output: `zsh: command not found: ${cmd}\r\n` }
  }
}

function stripQuotes(s: string): string {
  return s.replace(/^["']|["']$/g, '')
}

function catFile(name: string): string {
  if (name === 'README.md') return '# atterm-demo\r\n\r\nA fake project for the site demo.\r\n'
  if (name === 'package.json') return '{\r\n  "name": "atterm-demo"\r\n}\r\n'
  return `cat: ${name}: No such file or directory\r\n`
}

function helpText(): string {
  return [
    'demo shell — try:',
    '  ls  pwd  whoami  echo <text>  cat README.md  date  clear',
    '  codex/claude/aider <task>   (演示任务状态 + 通知)',
    '',
  ].join('\r\n')
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd site && npx vitest run docs/.vitepress/theme/components/mock/fakeCommands.test.ts`
Expected: PASS(4 个用例)。

- [ ] **Step 5: Commit**

```bash
git add site/docs/.vitepress/theme/components/mock/fakeCommands.ts site/docs/.vitepress/theme/components/mock/fakeCommands.test.ts
git commit -m "feat(site): add fake command table for interactive terminal demo"
```

---

### Task 4: 回放脚本 `replayScripts.ts`

**Files:**
- Create: `site/docs/.vitepress/theme/components/mock/replayScripts.ts`

- [ ] **Step 1: 写 `replayScripts.ts`**(无独立单测,数据由 Task 8 集成用;仅需能 import)

```typescript
import {
  CODEX_SESSION_ID, CLAUDE_SESSION_ID, BUILD_SESSION_ID, GOTEST_SESSION_ID, IDLE_SESSION_ID,
} from './fakeSessions'

// 每个会话 attach 后终端播放的初始输出(打字机分块)。idle 会话给一个空
// prompt,等待用户交互。字符串含 ANSI,终端会渲染颜色。
export const replayScripts: Record<string, string[]> = {
  [CODEX_SESSION_ID]: [
    '\x1b[1m$ codex exec "refactor auth"\x1b[0m\r\n',
    '\x1b[36m▸ codex\x1b[0m analysing repository…\r\n',
    'found 12 files referencing auth\r\n',
    'proposing changes to internal/auth/*.go …\r\n',
    '\x1b[33m⠋ working…\x1b[0m\r\n',
  ],
  [CLAUDE_SESSION_ID]: [
    '\x1b[1m$ claude\x1b[0m\r\n',
    'I found the flaky test in session_test.go.\r\n',
    'Should I (1) add a retry or (2) fix the race?\r\n',
    '\x1b[33m❯ waiting for your input…\x1b[0m ',
  ],
  [BUILD_SESSION_ID]: [
    '\x1b[1m$ npm run build\x1b[0m\r\n',
    'vite v5.4.11 building for production…\r\n',
    '✓ 342 modules transformed.\r\n',
    '\x1b[32m✓ built in 12.0s\x1b[0m\r\n',
    '$ ',
  ],
  [GOTEST_SESSION_ID]: [
    '\x1b[1m$ go test ./...\x1b[0m\r\n',
    'ok   atterm/internal/relay   0.42s\r\n',
    '\x1b[31m--- FAIL: TestSessionReplay (0.10s)\x1b[0m\r\n',
    '\x1b[31mFAIL\x1b[0m atterm/internal/session\r\n',
    '$ ',
  ],
  [IDLE_SESSION_ID]: [
    'Last login: Fri Jul 31 10:00 on ttys001\r\n',
    "type \x1b[36mhelp\x1b[0m to see demo commands.\r\n",
    '\x1b[32myou@dev-server\x1b[0m ~/srv/atterm $ ',
  ],
}

export const PROMPT = '\x1b[32myou@dev-server\x1b[0m ~/srv/atterm $ '
```

- [ ] **Step 2: 验证可编译 import**

Run: `cd site && npx vitest run docs/.vitepress/theme/components/mock/ 2>&1 | tail -5`
Expected: 现有测试仍 PASS(此文件仅被 import,不引入新失败)。

- [ ] **Step 3: Commit**

```bash
git add site/docs/.vitepress/theme/components/mock/replayScripts.ts
git commit -m "feat(site): add per-session terminal replay scripts"
```

---

### Task 5: 内存假文件系统 `mockFs.ts`

**Files:**
- Create: `site/docs/.vitepress/theme/components/mock/mockFs.ts`
- Test: `site/docs/.vitepress/theme/components/mock/mockFs.test.ts`

先读接口:`desktop/frontend/src/plugins/fileExplorer/fsBridge.ts` 的 `FileSystemBridge` 与 `desktop/frontend/src/platform/types.ts` 的 `DirEntry`/`FileContent`/`FileMetaInfo`。**实现前用 Read 确认这三个类型的确切字段,以下测试/实现按当前已知字段写,若不符按实际调整。**

- [ ] **Step 1: 写失败测试 `mockFs.test.ts`**

```typescript
import { describe, it, expect } from 'vitest'
import { createMockFs } from './mockFs'

describe('createMockFs', () => {
  it('lists the demo project root', async () => {
    const fs = createMockFs()
    const entries = await fs.listDir('~/projects/atterm-demo')
    const names = entries.map((e) => e.name)
    expect(names).toContain('README.md')
    expect(names).toContain('main.go')
    expect(names).toContain('src')
  })

  it('reads a text file', async () => {
    const fs = createMockFs()
    const f = await fs.readFile('~/projects/atterm-demo/README.md')
    // FileContent 的文本字段名以实际类型为准(可能是 content / text / data)
    expect(JSON.stringify(f)).toContain('atterm-demo')
  })

  it('writes then reads back', async () => {
    const fs = createMockFs()
    await fs.writeFile('~/projects/atterm-demo/README.md', textToBytes('changed'), 0, false)
    const f = await fs.readFile('~/projects/atterm-demo/README.md')
    expect(JSON.stringify(f)).toContain('changed')
  })

  it('mkdir + createFile + rename + remove', async () => {
    const fs = createMockFs()
    await fs.mkdir('~/projects/atterm-demo/newdir')
    await fs.createFile('~/projects/atterm-demo/newdir/a.txt')
    await fs.rename('~/projects/atterm-demo/newdir/a.txt', '~/projects/atterm-demo/newdir/b.txt')
    const entries = await fs.listDir('~/projects/atterm-demo/newdir')
    expect(entries.map((e) => e.name)).toEqual(['b.txt'])
    await fs.remove('~/projects/atterm-demo/newdir/b.txt', false)
    const after = await fs.listDir('~/projects/atterm-demo/newdir')
    expect(after.length).toBe(0)
  })
})

function textToBytes(s: string): Uint8Array {
  return new TextEncoder().encode(s)
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd site && npx vitest run docs/.vitepress/theme/components/mock/mockFs.test.ts`
Expected: FAIL,模块不存在。

- [ ] **Step 3: 实现 `mockFs.ts`**

按 `FileSystemBridge`(remote 版即 `RemoteFileSystemBridge`)接口实现,背后一棵内存树。以下是骨架,**字段名对齐实际类型定义**(`DirEntry.name/is_dir/size/mod_time`、`FileContent` 的文本/字节字段、`FileMetaInfo` 的 `mod_time` 等 —— 实现时以 Read 到的定义为准):

```typescript
import type { FileSystemBridge } from '@/plugins/fileExplorer/fsBridge'
import type { DirEntry, FileContent, FileMetaInfo } from '@/platform/types'

interface Node {
  name: string
  dir: boolean
  data?: Uint8Array          // 文件内容
  children?: Map<string, Node>
  modTime: number
}

const ROOT = '~/projects/atterm-demo'

function seed(): Node {
  const enc = new TextEncoder()
  const file = (name: string, text: string): Node => ({ name, dir: false, data: enc.encode(text), modTime: 1 })
  const root: Node = { name: 'atterm-demo', dir: true, modTime: 1, children: new Map() }
  const src: Node = { name: 'src', dir: true, modTime: 1, children: new Map() }
  src.children!.set('app.ts', file('app.ts', 'export const app = () => "hi"\n'))
  src.children!.set('util.ts', file('util.ts', 'export const add = (a:number,b:number)=>a+b\n'))
  root.children!.set('README.md', file('README.md', '# atterm-demo\n\nA fake project for the demo.\n'))
  root.children!.set('main.go', file('main.go', 'package main\n\nfunc main() { println("hi") }\n'))
  root.children!.set('package.json', file('package.json', '{\n  "name": "atterm-demo"\n}\n'))
  root.children!.set('logo.png', pngNode())
  root.children!.set('src', src)
  return root
}

function pngNode(): Node {
  // 1x1 透明 PNG
  const b64 = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=='
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  return { name: 'logo.png', dir: false, data: bytes, modTime: 1 }
}

export function createMockFs(): FileSystemBridge {
  const root = seed()

  function resolve(path: string): { parent: Node | null; node: Node | null; name: string } {
    const rel = path.replace(ROOT, '').replace(/^\/+/, '')
    if (rel === '') return { parent: null, node: root, name: 'atterm-demo' }
    const parts = rel.split('/')
    let cur: Node = root
    for (let i = 0; i < parts.length - 1; i++) {
      const next = cur.children?.get(parts[i])
      if (!next || !next.dir) return { parent: null, node: null, name: parts[parts.length - 1] }
      cur = next
    }
    const name = parts[parts.length - 1]
    return { parent: cur, node: cur.children?.get(name) ?? null, name }
  }

  const toEntry = (n: Node): DirEntry => (
    { name: n.name, is_dir: n.dir, size: n.data?.byteLength ?? 0, mod_time: n.modTime } as unknown as DirEntry
  )
  const meta = (n: Node): FileMetaInfo => (
    { mod_time: n.modTime, size: n.data?.byteLength ?? 0 } as unknown as FileMetaInfo
  )

  const bridge: FileSystemBridge = {
    identity: 'demo',
    async listDir(path) {
      const { node } = resolve(path)
      if (!node?.dir) throw new Error('not a directory')
      return Array.from(node.children!.values())
        .sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name))
        .map(toEntry)
    },
    async readFile(path) {
      const { node } = resolve(path)
      if (!node || node.dir) throw new Error('not a file')
      const text = new TextDecoder().decode(node.data ?? new Uint8Array())
      return { content: text, truncated: false, size: node.data?.byteLength ?? 0 } as unknown as FileContent
    },
    async fileMeta(path) {
      const { node } = resolve(path)
      if (!node) throw new Error('not found')
      return meta(node)
    },
    async writeFile(path, data) {
      const { parent, name, node } = resolve(path)
      const bytes = data instanceof Uint8Array ? data : new Uint8Array(data as number[])
      if (node) { node.data = bytes; node.modTime++; return meta(node) }
      if (!parent) throw new Error('no parent')
      const n: Node = { name, dir: false, data: bytes, modTime: 1 }
      parent.children!.set(name, n)
      return meta(n)
    },
    async createFile(path) {
      const { parent, name } = resolve(path)
      if (!parent) throw new Error('no parent')
      const n: Node = { name, dir: false, data: new Uint8Array(), modTime: 1 }
      parent.children!.set(name, n)
      return meta(n)
    },
    async mkdir(path) {
      const { parent, name } = resolve(path)
      if (!parent) throw new Error('no parent')
      const n: Node = { name, dir: true, modTime: 1, children: new Map() }
      parent.children!.set(name, n)
      return meta(n)
    },
    async rename(from, to) {
      const src = resolve(from)
      const dst = resolve(to)
      if (!src.node || !src.parent || !dst.parent) throw new Error('bad rename')
      src.parent.children!.delete(src.name)
      src.node.name = dst.name
      dst.parent.children!.set(dst.name, src.node)
      return meta(src.node)
    },
    async remove(path) {
      const { parent, name } = resolve(path)
      parent?.children!.delete(name)
    },
    async trash(path) {
      const { parent, name } = resolve(path)
      parent?.children!.delete(name)
    },
    assetUrlFor(path) {
      const { node } = resolve(path)
      if (!node?.data) return ''
      const blob = new Blob([node.data])
      return URL.createObjectURL(blob)
    },
    revokeAssetUrl(url) {
      if (url.startsWith('blob:')) URL.revokeObjectURL(url)
    },
  } as unknown as FileSystemBridge

  return bridge
}
```

> **实现纪律**:先 Read `fsBridge.ts` 拿到 `FileSystemBridge` 精确成员(方法名、`watchDir`/`unwatchDir`/`openExternal` 是否必需、`identity` 是否存在),缺哪个补哪个(watch 类返回 no-op)。`DirEntry`/`FileContent`/`FileMetaInfo` 字段名同理以 Read 为准,把上面 `as unknown as` 处替换成真实字段。

- [ ] **Step 4: 运行确认通过**

Run: `cd site && npx vitest run docs/.vitepress/theme/components/mock/mockFs.test.ts`
Expected: PASS(4 个用例)。若因字段名报错,先修类型再跑。

- [ ] **Step 5: Commit**

```bash
git add site/docs/.vitepress/theme/components/mock/mockFs.ts site/docs/.vitepress/theme/components/mock/mockFs.test.ts
git commit -m "feat(site): add in-memory mock filesystem for file explorer demo"
```

---

### Task 6: wails stub `wailsStub.ts`

**Files:**
- Create: `site/docs/.vitepress/theme/components/mock/wailsStub.ts`

用途:App.vue 及其依赖里对 `wailsjs/go/main/App` 和 `wailsjs/runtime/runtime` 的静态 import,在 web/demo 路径下不会被调用,但需存在以通过打包。参考 atstarter 的 `mockWailsApp.mjs` / `mockWailsRuntime.mjs`。

- [ ] **Step 1: 先摸清被 import 了什么**

Run:
```bash
cd ~/GolandProjects/atterm/desktop/frontend
grep -rn "wailsjs/go/main/App\|wailsjs/runtime/runtime" src/ | grep -v test
```
记录被 import 的具名导出。

- [ ] **Step 2: 写 `wailsStub.ts`**,为每个被 import 的名字导出一个抛错或 no-op 的桩

```typescript
// Wails 绑定桩:demo 走 web/mock 平台,永不触达这些函数。保留具名导出以通过
// 打包;若被误调,抛错以便定位(而非静默返回坏数据)。
function notImplemented(name: string) {
  return (..._args: unknown[]): never => {
    throw new Error(`[demo] wails binding '${name}' called in mock environment`)
  }
}

// 按 Step 1 grep 到的具名导出补齐。示例:
export const GetEndpoint = notImplemented('GetEndpoint')
export const GetHostInfo = notImplemented('GetHostInfo')
// runtime.ts 常见:
export const EventsOn = (..._a: unknown[]) => () => {}
export const EventsEmit = (..._a: unknown[]) => {}
export const WindowSetTitle = (..._a: unknown[]) => {}
export const BrowserOpenURL = (url: string) => { window.open(url, '_blank', 'noopener') }
export default {}
```

> 具体导出名以 Step 1 结果为准,一一补齐;runtime 事件类导出 no-op,App 绑定类默认抛错。

- [ ] **Step 3: Commit**

```bash
git add site/docs/.vitepress/theme/components/mock/wailsStub.ts
git commit -m "feat(site): add wails binding stubs for demo bundling"
```

---

### Task 7: mock WebSocket `mockSocket.ts`

**Files:**
- Create: `site/docs/.vitepress/theme/components/mock/mockSocket.ts`

先 Read `desktop/frontend/src/lib/proto.ts`(帧编解码)与 `lib/connection.ts` 的 `SessionListConnection`(发 `LIST`、期望 `LIST_RESP`)、`SessionConnection`(发 `ATTACH`/`IN`/`RESIZE`,期望 `OUT`)确认帧流。

- [ ] **Step 1: 写 `mockSocket.ts`**

```typescript
import { encodeFrame, decodeFrame, TYPE, uuidParse, encodeText, NIL_SID } from '@/lib/proto'
import { fakeSessions, IDLE_SESSION_ID } from './fakeSessions'
import { replayScripts, PROMPT } from './replayScripts'
import { runFakeCommand } from './fakeCommands'

type Listener = (ev: any) => void

// 一个最小 WebSocket 替身:根据首帧类型区分「会话列表连接」与「单会话连接」。
// 不做真实网络。用真实 proto 帧编解码,保证前端解析路径与线上一致。
export class MockSocket {
  static onNotify: ((title: string, body: string) => void) | null = null

  url: string
  readyState = 0 // CONNECTING
  binaryType = 'arraybuffer'
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3

  private listeners: Record<string, Listener[]> = {}
  private sid: Uint8Array = NIL_SID
  private idleBuffer = ''

  constructor(url: string, _protocols?: string | string[]) {
    this.url = url
    setTimeout(() => {
      this.readyState = MockSocket.OPEN
      this.emit('open', {})
    }, 0)
  }

  addEventListener(type: string, fn: Listener) { (this.listeners[type] ||= []).push(fn) }
  removeEventListener(type: string, fn: Listener) {
    this.listeners[type] = (this.listeners[type] || []).filter((f) => f !== fn)
  }
  set onopen(fn: Listener) { this.addEventListener('open', fn) }
  set onmessage(fn: Listener) { this.addEventListener('message', fn) }
  set onclose(fn: Listener) { this.addEventListener('close', fn) }
  set onerror(fn: Listener) { this.addEventListener('error', fn) }

  private emit(type: string, ev: any) { for (const fn of this.listeners[type] || []) fn(ev) }
  private deliver(bytes: Uint8Array) {
    this.emit('message', { data: bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) })
  }

  send(data: ArrayBuffer | Uint8Array | string) {
    if (typeof data === 'string') return
    const bytes = data instanceof Uint8Array ? data : new Uint8Array(data)
    const frame = decodeFrame(bytes)
    if (!frame) return
    switch (frame.type) {
      case TYPE.LIST:
        this.deliver(encodeFrame(TYPE.LIST_RESP, NIL_SID, encodeText(JSON.stringify(fakeSessions))))
        break
      case TYPE.ATTACH: {
        this.sid = frame.sid
        const sidStr = uuidStringify(frame.sid)
        const script = replayScripts[sidStr] || []
        let delay = 60
        for (const chunk of script) {
          setTimeout(() => this.deliver(encodeFrame(TYPE.OUT, frame.sid, encodeText(chunk))), delay)
          delay += 220
        }
        break
      }
      case TYPE.IN: {
        const sidStr = uuidStringify(frame.sid)
        if (sidStr !== IDLE_SESSION_ID) return // 非 idle 会话不接受输入
        this.handleInput(frame.sid, new TextDecoder().decode(frame.payload))
        break
      }
      default:
        break
    }
  }

  private handleInput(sid: Uint8Array, s: string) {
    const out = (t: string) => this.deliver(encodeFrame(TYPE.OUT, sid, encodeText(t)))
    for (const ch of s) {
      if (ch === '\r' || ch === '\n') {
        out('\r\n')
        const res = runFakeCommand(this.idleBuffer)
        this.idleBuffer = ''
        if (res.longRunning && res.steps) {
          let d = 120
          for (const step of res.steps) { setTimeout(() => out(step), d); d += 300 }
          setTimeout(() => {
            out('\r\n' + PROMPT)
            MockSocket.onNotify?.('Task completed', 'codex exec finished (3 files changed)')
          }, d)
        } else {
          if (res.output) out(res.output)
          out(PROMPT)
        }
      } else if (ch === '\x7f') { // backspace
        if (this.idleBuffer.length) { this.idleBuffer = this.idleBuffer.slice(0, -1); out('\b \b') }
      } else {
        this.idleBuffer += ch
        out(ch) // echo
      }
    }
  }

  close() { this.readyState = MockSocket.CLOSED; this.emit('close', { code: 1000 }) }
}

function uuidStringify(sid: Uint8Array): string {
  const h = Array.from(sid).map((b) => b.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0,8)}-${h.slice(8,12)}-${h.slice(12,16)}-${h.slice(16,20)}-${h.slice(20)}`
}

// 在 demo 容器生命周期内替换全局 WebSocket,返回 restore 函数。
export function installMockWebSocket(): () => void {
  const original = (globalThis as any).WebSocket
  ;(globalThis as any).WebSocket = MockSocket as unknown as typeof WebSocket
  return () => { (globalThis as any).WebSocket = original }
}
```

> **实现纪律**:`decodeFrame` 的返回结构、`encodeText` 是否存在、`TYPE.LIST` 的 payload 期望格式,以 Read `proto.ts` 为准;`SessionListConnection` 期望的 `LIST_RESP` payload 是否为 `{items:[...]}` 还是裸数组,Read `connection.ts` 的 `f.type === TYPE.LIST_RESP` 解析分支确认后对齐(与 fakeSessions 序列化格式一致)。若 `uuidParse`/`uuidStringify` 已有工具,复用之。

- [ ] **Step 2: 验证可编译**

Run: `cd site && npx vitest run docs/.vitepress/theme/components/mock/ 2>&1 | tail -5`
Expected: 现有 mock 测试仍 PASS(本文件仅需编译通过)。

- [ ] **Step 3: Commit**

```bash
git add site/docs/.vitepress/theme/components/mock/mockSocket.ts
git commit -m "feat(site): add mock WebSocket with replay and interactive echo"
```

---

### Task 8: mock 平台 `mockPlatform.ts`

**Files:**
- Create: `site/docs/.vitepress/theme/components/mock/mockPlatform.ts`

以 `desktop/frontend/src/platform/web.ts` 为模板。**先 Read 它和 `platform/types.ts` 的 `Platform`/`Capabilities`/各 Bridge 接口**,逐字段实现。

- [ ] **Step 1: 写 `mockPlatform.ts`**

```typescript
import type { Platform, Capabilities } from '@/platform/types'
import { fakeSessions } from './fakeSessions'
import { createMockFs } from './mockFs'
import { MockSocket } from './mockSocket'

const CAPS: Capabilities = {
  localPty: false,
  autoUpdate: false,
  pluginHost: true,          // 打开文件浏览器 + 插件面板
  windowControls: false,
  systemClipboard: true,
  notifications: typeof Notification !== 'undefined',
  fileDialog: false,
  wailsBindings: false,
  capacitor: false,
}

// 内存 KV,替代 localStorage,避免污染真实站点存储
const mem = new Map<string, string>()

export function createMockPlatform(): Platform {
  const events = makeEventBus()

  // 通知回调接到 MockSocket:长命令完成时弹通知
  MockSocket.onNotify = (title, body) => { void platform.system.showNotification(title, body) }

  const platform: Platform = {
    caps: CAPS,
    relay: {
      async load() {
        return {
          url: 'https://demo.atterm.local', token: 'demo-token',
          session_expires_at: 4_102_444_800, allow_insecure_relay: false,
          remote_permission: 'full', last_email: 'you@example.com', connected: true,
        } as any
      },
      async save() {},
      async clear() {},
      async fetchMe() {
        return { user: { id: 'demo', email: 'you@example.com' } } as any
      },
      async logout() {},
    } as any,
    sessions: {
      async closeSession() {},
      async listShells() { return [] },
      async listRemoteSessions() { return fakeSessions },
      async markSessionsSeen() {},
      async getPins() { const v = mem.get('pins'); return v ? JSON.parse(v) : [] },
      async setPins(ids: string[]) { mem.set('pins', JSON.stringify(ids)); events.emit('prefs:remote-changed', undefined) },
      async listRelaySessions() { return [] },
      async revokeRelaySession() {},
      async signOutOtherRelaySessions() { return { count: 0 } as any },
    } as any,
    system: {
      async showNotification(title: string, body: string) {
        try {
          if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
            new Notification(title, { body }); return
          }
        } catch { /* fall through to toast */ }
        events.emit('demo:toast', { title, body })
      },
      async getClipboardPaste() { return { kind: 'none' } as any },
      async openExternalURL(url: string) { window.open(url, '_blank', 'noopener') },
      async getEnvironment() { return { buildType: 'web', platform: navigator.userAgent, arch: '' } },
      async getAppVersion() { return { version: 'demo', tag: 'demo' } as any },
    } as any,
    events,
    templates: {
      async load() { return [] },
      async save() {},
      async clear() {},
      async loadHidden() { return false },
      async saveHidden() {},
    } as any,
    auxKeys: {
      async load() { return [] },
      async save() {},
      async clear() {},
    } as any,
    pluginHost: {
      async getPluginConfig() {
        return {
          fileExplorer: { enabled: true, showHidden: false, showLineNumbers: true, panelWidthPx: 380, panelCollapsed: false },
          translate: { enabled: false },
          shortcuts: {},
        } as any
      },
      async setPluginConfig() {},
      fs: createMockFs() as any,
    } as any,
  }

  return platform
}

function makeEventBus() {
  const map = new Map<string, Set<(d: unknown) => void>>()
  return {
    on(name: string, handler: (d: unknown) => void) {
      let set = map.get(name); if (!set) { set = new Set(); map.set(name, set) }
      set.add(handler); return () => set!.delete(handler)
    },
    emit(name: string, data: unknown) {
      for (const fn of Array.from(map.get(name) || [])) { try { fn(data) } catch { /* ignore */ } }
    },
  }
}
```

> **实现纪律**:每个 Bridge 的方法签名以 `platform/types.ts` 为准,`as any` 只用于收窄不了的返回体;`RelayConfig`/`RelayMe`/`PluginConfig` 的确切字段以 Read 到的类型为准。`getPluginConfig` 返回体必须让 `configStore.isPluginEnabled('file-explorer')` 为 true。若 App 通过 `createRemoteSessionFS(conn)` 而非 `pluginHost.fs` 取远程 fs,则在 Task 9 的 alias 里把 `remoteSessionFS` 换成返回 `createMockFs()` 的工厂(二选一,按 Read `App.vue` / fileExplorer 入口确定实际取法)。

- [ ] **Step 2: Commit**

```bash
git add site/docs/.vitepress/theme/components/mock/mockPlatform.ts
git commit -m "feat(site): add mock platform for home demo"
```

---

### Task 9: HomeDemo.vue + config.mjs alias

**Files:**
- Create: `site/docs/.vitepress/theme/components/HomeDemo.vue`
- Create: `site/docs/.vitepress/theme/index.js`
- Create: `site/docs/.vitepress/theme/custom.css`
- Modify: `site/docs/.vitepress/config.mjs`(补 `vite.resolve.alias` + `dedupe` + `ssr.noExternal`)

- [ ] **Step 1: 写 `HomeDemo.vue`**

```vue
<script setup>
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { initPlatform } from '@/platform'
import { createMockPlatform } from './mock/mockPlatform'
import { installMockWebSocket } from './mock/mockSocket'
import FrontendApp from '@/App.vue'
import '@/style.css'

const toast = ref(null)
let restoreWs = null

onMounted(() => {
  restoreWs = installMockWebSocket()
  const platform = initPlatform(createMockPlatform)
  platform.events.on('demo:toast', (d) => {
    toast.value = d
    setTimeout(() => { toast.value = null }, 4000)
  })
})
onBeforeUnmount(() => { restoreWs?.() })
</script>

<template>
  <ClientOnly>
    <section class="home-demo" aria-label="AT Term interactive demo">
      <p class="home-demo-hint">👇 这是一个纯前端 demo,数据都是假的,随便玩 —— 切换会话、在 idle 的 zsh 里敲 <code>help</code>、打开右侧文件面板。</p>
      <div class="home-demo-frame">
        <FrontendApp />
      </div>
      <transition name="fade">
        <div v-if="toast" class="home-demo-toast">
          <strong>{{ toast.title }}</strong><span>{{ toast.body }}</span>
        </div>
      </transition>
    </section>
  </ClientOnly>
</template>

<style scoped>
.home-demo { width: min(1248px, calc(100vw - 24px)); margin: 20px auto 0; }
.home-demo-hint { font-size: 13px; opacity: .8; margin: 0 0 8px; }
.home-demo-frame {
  position: relative; height: 640px; overflow: hidden;
  border: 1px solid var(--vp-c-divider); border-radius: 12px;
  box-shadow: 0 12px 40px rgba(0,0,0,.18); background: #0b1020;
}
.home-demo-frame :deep(*) { box-sizing: border-box; }
.home-demo-toast {
  position: absolute; right: 16px; bottom: 16px; z-index: 20;
  display: flex; flex-direction: column; gap: 2px;
  padding: 10px 14px; border-radius: 8px; background: #1b2030; color: #fff;
  box-shadow: 0 6px 24px rgba(0,0,0,.4); font-size: 13px;
}
.fade-enter-active,.fade-leave-active { transition: opacity .3s; }
.fade-enter-from,.fade-leave-to { opacity: 0; }
@media (max-width: 720px) {
  .home-demo-frame { height: 560px; overflow-x: auto; }
  .home-demo-frame :deep(.app) { min-width: 1100px; }
}
</style>
```

> **实现纪律**:`FrontendApp` 是否接受 `embedded` prop,以 Read `App.vue` 的 `defineProps` 为准 —— 有则传 `<FrontendApp embedded />`,无则如上用固定尺寸容器包裹(不改 App.vue)。挂载前必须 `initPlatform`,否则 `usePlatform()` 抛错。

- [ ] **Step 2: 写 `theme/index.js`**

```javascript
import DefaultTheme from 'vitepress/theme'
import HomeDemo from './components/HomeDemo.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('HomeDemo', HomeDemo)
  },
}
```

- [ ] **Step 3: 写 `theme/custom.css`**(首页版式,占位最小集,Task 10 会用到这些类)

```css
.tech-home .badge-strip { display:flex; justify-content:center; margin:24px 0; }
.tech-home .badge-wrap { display:flex; flex-wrap:wrap; gap:10px; justify-content:center; }
.tech-home .badge { display:flex; flex-direction:column; align-items:center; padding:10px 16px;
  border:1px solid var(--vp-c-divider); border-radius:10px; background:var(--vp-c-bg-soft); }
.tech-home .badge-k { font-weight:700; }
.tech-home .badge-v { font-size:12px; opacity:.75; }
.tech-home .feature-cards { display:grid; grid-template-columns:repeat(auto-fit,minmax(220px,1fr));
  gap:16px; margin:24px 0; }
.tech-home .feature-card { padding:18px; border:1px solid var(--vp-c-divider); border-radius:12px;
  background:var(--vp-c-bg-soft); }
.tech-home .feature-card h3 { margin:0 0 8px; }
.tech-home .downloads { display:grid; grid-template-columns:repeat(auto-fit,minmax(200px,1fr)); gap:16px; }
.tech-home .download-card { display:block; padding:20px; border:1px solid var(--vp-c-divider);
  border-radius:12px; text-decoration:none; background:var(--vp-c-bg-soft); }
.tech-home .download-card .os { font-weight:700; font-size:18px; }
.tech-home .download-card .os-sub { font-size:12px; opacity:.7; }
```

- [ ] **Step 4: 给 `config.mjs` 补 vite alias/dedupe/ssr**

在 `defineConfig({...})` 里加入 `vite` 段(参考 atstarter config.mjs):

```javascript
  vite: {
    resolve: {
      alias: [
        { find: /^@\/(.*)$/, replacement: fileURLToPath(new URL('../../../desktop/frontend/src/$1', import.meta.url)) },
        { find: /^@webshared\/(.*)$/, replacement: fileURLToPath(new URL('../../../web/src/shared/$1', import.meta.url)) },
        { find: /^@shared\/(.*)$/, replacement: fileURLToPath(new URL('../../../web/src/shared/$1', import.meta.url)) },
        { find: /wailsjs\/go\/main\/App$/, replacement: fileURLToPath(new URL('./theme/components/mock/wailsStub.ts', import.meta.url)) },
        { find: /wailsjs\/runtime\/runtime$/, replacement: fileURLToPath(new URL('./theme/components/mock/wailsStub.ts', import.meta.url)) },
      ],
      dedupe: ['vue', 'pinia', 'naive-ui', 'xterm', 'xterm-addon-fit', 'vfonts'],
    },
    ssr: {
      // App.vue 及其依赖是浏览器端组件,SSR 阶段不预打包/执行
      noExternal: ['naive-ui', 'xterm', 'xterm-addon-fit', 'vfonts'],
    },
  },
```

并在文件顶部加 `import { fileURLToPath } from 'node:url'`。

> 路径基准:`config.mjs` 在 `site/docs/.vitepress/`,`../../../` 回到仓库根。`@/` alias 里 `../../../desktop/...` 对应 `<root>/desktop/...` —— 实现时用 `docs:dev` 打开验证解析,按报错调整层级。

- [ ] **Step 5: 冒烟 —— 起 dev server 手测**

Run: `cd site && npm run docs:dev`
打开 `http://localhost:5173/atterm/`,验证首页 demo 渲染出真实界面(侧栏 + 终端)。若白屏/报错,按控制台缺失的 platform 方法或网络请求逐个补 mock/alias(记录到 spec §9 风险表对应项)。**这是 mock 完整性的关键关卡。**

- [ ] **Step 6: 验证 build 通过**

Run: `cd site && npm run docs:build`
Expected: 成功产出 dist。SSR 若报 `window is not defined`,把触发的组件包进 `<ClientOnly>`(HomeDemo 已包)或补 `ssr.noExternal`。

- [ ] **Step 7: Commit**

```bash
git add site/docs/.vitepress/theme site/docs/.vitepress/config.mjs
git commit -m "feat(site): embed real App.vue as interactive home demo"
```

---

### Task 10: 首页正式内容 `index.md`

**Files:**
- Modify: `site/docs/index.md`

- [ ] **Step 1: 覆盖 `index.md`**(hero + demo + 徽章 + 特性 + 下载)

```markdown
---
layout: home
hero:
  name: AT Term
  text: 带远程接管的跨平台终端
  tagline: 桌面端启动的 shell、Codex、Claude 等长任务,离开电脑后从手机、浏览器或另一台电脑继续查看和输入。启用 E2EE 后,输出对 relay 全程不可读。
  actions:
    - theme: brand
      text: 下载最新版
      link: https://github.com/attson/atterm/releases/latest
    - theme: alt
      text: 使用文档
      link: /guide/
    - theme: alt
      text: 部署 Relay
      link: /guide/deploy-relay
---

<script setup>
import HomeDemo from './.vitepress/theme/components/HomeDemo.vue'
</script>

<div class="tech-home">

<HomeDemo />

<section class="badge-strip">
  <div class="badge-wrap">
    <div class="badge"><span class="badge-k">E2EE</span><span class="badge-v">端到端加密</span></div>
    <div class="badge"><span class="badge-k">3 平台</span><span class="badge-v">macOS / Linux / Windows</span></div>
    <div class="badge"><span class="badge-k">OSC 133</span><span class="badge-v">任务状态推导</span></div>
    <div class="badge"><span class="badge-k">MCP</span><span class="badge-v">AI / CLI 控制</span></div>
    <div class="badge"><span class="badge-k">Web Push</span><span class="badge-v">Feishu / webhook 通知</span></div>
    <div class="badge"><span class="badge-k">Apache-2.0</span><span class="badge-v">开源</span></div>
  </div>
</section>

## 核心能力

<div class="feature-cards">
  <div class="feature-card"><h3>远程接管(lazy 同步)</h3><p>桌面连上 relay 后,手机 / 浏览器 / 另一台桌面可 attach 同一会话;默认 viewer,take over 才能写。无人看时不上传字节。</p></div>
  <div class="feature-card"><h3>会话状态与侧栏</h3><p>OSC 133 推导 running / waiting / done / failed;侧栏可搜索、置顶、按 host 分组,attention 高亮 AI 等输入的会话。</p></div>
  <div class="feature-card"><h3>多通道通知</h3><p>命令完成、AI 等输入触发系统通知 / Web Push / 飞书卡片 / 出站 webhook,payload 带 session id 与摘要。</p></div>
  <div class="feature-card"><h3>AI Agent 识别</h3><p>Claude Code / Codex / Aider / Gemini 自动识别:命令分类、resume 注入、Notification hook 自动安装。</p></div>
  <div class="feature-card"><h3>端到端加密</h3><p>account_key 只在客户端持有;输出 / 标题 / cwd / 摘要在 relay 侧全程密文,自托管不外泄。</p></div>
  <div class="feature-card"><h3>远程文件浏览</h3><p>浏览 owner 机器文件系统、编辑保存、全套 CRUD + 回收站,双源切换本地 / 远程。</p></div>
</div>

## 下载

<div class="downloads">
  <a class="download-card" href="https://github.com/attson/atterm/releases/latest" target="_blank" rel="noreferrer"><div class="os">macOS</div><div class="os-sub">.dmg / .zip · Intel / Apple Silicon</div></a>
  <a class="download-card" href="https://github.com/attson/atterm/releases/latest" target="_blank" rel="noreferrer"><div class="os">Linux</div><div class="os-sub">.deb / .tar.gz · amd64 / arm64</div></a>
  <a class="download-card" href="https://github.com/attson/atterm/releases/latest" target="_blank" rel="noreferrer"><div class="os">Windows</div><div class="os-sub">.exe / .zip · amd64</div></a>
</div>

</div>
```

- [ ] **Step 2: dev 验证首页整体渲染**

Run: `cd site && npm run docs:dev` → 打开首页,确认 hero、demo、徽章、特性卡、下载区都在,无布局崩坏。

- [ ] **Step 3: Commit**

```bash
git add site/docs/index.md
git commit -m "feat(site): flesh out landing page content"
```

---

### Task 11: 文档页(从 README 提炼)

**Files:**
- Create: `site/docs/guide/index.md`
- Create: `site/docs/guide/remote-takeover.md`
- Create: `site/docs/guide/e2ee.md`
- Create: `site/docs/guide/deploy-relay.md`
- Create: `site/docs/guide/ai-agents.md`
- Create: `site/docs/guide/faq.md`

内容来源:`README.md` 对应章节 + `docs/spec/architecture.md`/`auth.md`/`protocol.md`/`feishu.md`。**从 README 提炼、精炼改写为文档语气,不逐字复制营销句;保留命令块与配置示例的准确性。**

- [ ] **Step 1: `guide/index.md`** — 介绍 + 快速上手

  内容:一句话定位、工作原理 5 行速览、适合谁、快速开始「方式 A 只用桌面端」「方式 B 桌面 + 自托管 relay」「方式 C 源码调试」(取自 README `## 快速开始`)。命令块照抄 README 保证准确。

- [ ] **Step 2: `guide/remote-takeover.md`** — 远程接管 + 会话侧栏 + 通知

  内容:lazy 同步机制、viewer/driver take over、会话侧栏(置顶 / 搜索 / host 分组 / attention)、多通道通知(系统 / Web Push / 飞书 / webhook)。取自 README `## 现在能做什么`(会话侧栏、远程接管段)+ `docs/spec/architecture.md` 相关。

- [ ] **Step 3: `guide/e2ee.md`** — E2EE + 安全模型 + 权限

  内容:account_key 客户端持有、relay 侧密文范围、`## 安全模型`、`### 启用端到端加密`、`### 选择远程权限`。取自 README 对应段 + `docs/spec/auth.md`。

- [ ] **Step 4: `guide/deploy-relay.md`** — 部署 relay

  内容:Docker Compose、Go 直接运行、Bootstrap admin、管理后台、多实例部署。取自 README `## 部署 relay` 全段,命令 / compose 片段照抄。

- [ ] **Step 5: `guide/ai-agents.md`** — AI agent + Feishu

  内容:AI agent 支持(识别 / resume / hook 安装)、会话恢复、Feishu 远程终端 + AskUserQuestion。取自 README `## AI agent 支持`、`### 会话恢复`、`### Feishu` + `docs/spec/feishu.md`、`docs/feishu-remote-terminal-deployment.md`。

- [ ] **Step 6: `guide/faq.md`** — FAQ / 故障排查

  内容:常见问题(command not found、连不上 relay、通知没弹、E2EE 打不开会话等)。若 README 无独立 FAQ,从各段的边界说明与「方式 C 调试」提炼 6-10 条 Q&A。

- [ ] **Step 7: build 验证所有页面与 sidebar 链接**

Run: `cd site && npm run docs:build`
Expected: 成功,无死链警告(VitePress 对 dead link 会报错)。若报死链,修正 nav/sidebar 的 link 与文件名一致。

- [ ] **Step 8: Commit**

```bash
git add site/docs/guide
git commit -m "docs(site): add guide pages distilled from README"
```

---

### Task 12: GitHub Actions 部署 `pages.yml`

**Files:**
- Create: `.github/workflows/pages.yml`

模板取自 `~/GolandProjects/atstarter/.github/workflows/pages.yml`,路径改为 atterm 的前端目录。

- [ ] **Step 1: 写 `.github/workflows/pages.yml`**

```yaml
name: pages

on:
  push:
    branches: [main]
    paths:
      - 'site/**'
      - 'desktop/frontend/src/**'
      - 'desktop/frontend/package.json'
      - 'web/src/shared/**'
      - '.github/workflows/pages.yml'
  workflow_dispatch:

permissions:
  contents: read

concurrency:
  group: pages
  cancel-in-progress: false

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-node@v5
        with:
          node-version: '20'
          cache: npm
          cache-dependency-path: |
            desktop/frontend/package-lock.json
            web/package-lock.json
            site/package-lock.json
      - name: Install desktop frontend deps
        working-directory: desktop/frontend
        run: npm ci
      - name: Install web deps
        working-directory: web
        run: npm ci
      - name: Install site deps
        working-directory: site
        run: npm ci
      - name: Test mock layer
        working-directory: site
        run: npm test
      - name: Build
        working-directory: site
        run: npm run docs:build
      - uses: actions/configure-pages@v6
      - uses: actions/upload-pages-artifact@v5
        with:
          path: site/docs/.vitepress/dist

  deploy:
    needs: build
    permissions:
      pages: write
      id-token: write
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      # 首次部署前需在仓库 Settings → Pages → Source 选择「GitHub Actions」,否则本步骤 403。
      - id: deployment
        uses: actions/deploy-pages@v5
```

> 为什么装 desktop/frontend 与 web 依赖:demo 通过 alias 复用这两处源码,其 import 的三方包(如 `@shared/*` 链上的依赖)需在解析路径上可用。dedupe 保证运行时单例落在 site。若 CI 构建报某包解析失败,把该包补进 `site/package.json` 的 dependencies。

- [ ] **Step 2: 本地模拟 CI 构建路径**

Run:
```bash
cd ~/GolandProjects/atterm/desktop/frontend && npm ci
cd ~/GolandProjects/atterm/web && npm ci
cd ~/GolandProjects/atterm/site && npm ci && npm test && npm run docs:build
```
Expected: 全绿。

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/pages.yml
git commit -m "ci(site): deploy VitePress site to GitHub Pages"
```

---

### Task 13: 交付说明 + 收尾

**Files:**
- Create: `site/README.md`

- [ ] **Step 1: 写 `site/README.md`**

```markdown
# AT Term 站点

VitePress 站点,发布到 https://attson.github.io/atterm/。首页嵌入真实 `desktop/frontend/src/App.vue`,用 mock 后端(platform / WebSocket / 文件系统)让访客直接体验。

## 本地开发

    cd site
    npm install
    npm run docs:dev      # http://localhost:5173/atterm/
    npm run docs:build    # 产出 docs/.vitepress/dist
    npm test              # mock 层单测

demo 复用 `../desktop/frontend/src` 与 `../web/src/shared` 源码,通过 vite alias 接入,零侵入前端。mock 层在 `docs/.vitepress/theme/components/mock/`。

## 部署

push 到 `main` 触发 `.github/workflows/pages.yml` 自动构建发布。
**首次部署前**:仓库 Settings → Pages → Source 选「GitHub Actions」。
`config.mjs` 的 `base: '/atterm/'` 必须与仓库名一致。

## 待后续

README(仓库根)精简、详细内容迁到本站,作为独立动作,尚未执行。
```

- [ ] **Step 2: 全量验证**

Run: `cd site && npm run docs:build && npm test`
Expected: build + 测试全绿。手动过一遍 spec §5.3 交互清单(dev server)。

- [ ] **Step 3: Commit**

```bash
git add site/README.md
git commit -m "docs(site): add site README with dev and deploy notes"
```

- [ ] **Step 4: 推送并触发部署**

```bash
cd ~/GolandProjects/atterm
git push origin main
```
推送后:GitHub 仓库 Settings → Pages → Source 选「GitHub Actions」(首次一次性);到 Actions 页看 `pages` workflow 跑绿;访问 `https://attson.github.io/atterm/` 验证首页与 demo。

---

## Self-Review 覆盖检查(spec → task 映射)

- spec §3 站点结构/技术栈 → Task 1、9
- spec §4.1 HomeDemo → Task 9
- spec §4.2 mock platform → Task 8
- spec §4.3 mock WebSocket(回放 + 回显)→ Task 4、7
- spec §4.4 mock 文件系统 → Task 5、8(挂载)
- spec §4.5 vite alias → Task 9
- spec §5 假数据/故事线 → Task 2、3、4
- spec §6 首页非 demo 区块 → Task 10
- spec §7 部署 → Task 12、13
- spec §8 测试 → Task 2/3/5(单测)、Task 9 Step5(人工冒烟)
- spec §10 交付物 → Task 13

**类型一致性**:`fakeSessions` 用 `RemoteSession`(Task 2、7、8 一致);`createMockFs(): FileSystemBridge`(Task 5、8 一致);`createMockPlatform(): Platform`(Task 8、9 一致);`installMockWebSocket()`/`MockSocket`(Task 7、9 一致);sid 常量在 Task 2 定义,Task 4、7 复用同名导出。

**已知需实现时 Read 确认的点**(已在对应 task 标注实现纪律):`FileSystemBridge` 成员、`DirEntry/FileContent/FileMetaInfo` 字段、`proto.ts` 导出与 `LIST_RESP` payload 格式、`Platform`/各 Bridge 签名、`App.vue` 是否有 `embedded` prop、远程 fs 是否走 `pluginHost.fs` 还是 `createRemoteSessionFS`。
