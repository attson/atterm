# AI 会话 OSC 标题：会话栏显示 AI 工具设置的标题

> **Status**: draft
> **Date**: 2026-06-10
> **Scope**: PTY 字节流 OSC 0/1/2 解析 + 4 处会话栏 + Wails 窗口标题
> **No protocol change**: 复用现有 `SessionInfo.Title` / `MetaPayload.Title`

## 1. 目标 / 非目标

### 目标

- 桌面 TabBar、桌面右侧 TaskSidebar/TaskGroupedList、移动端 MobileSessionCard、Web SessionList **共 4 处会话栏**，在 `session.type === 'ai'` 时显示由 AI 工具通过 OSC 0/1/2 写入的窗口标题（例：`Remove token auth from relay login (node)`）。
- 桌面 Wails 主窗口标题跟随**当前激活 tab** 的 AI 标题；非 AI 会话或无标题时回退到 `"AT Term"`。
- 后端对所有会话解析 OSC 0/1/2 并复用既有的 META/ANNOUNCE 广播路径，让 viewer/mirror/remote/replay 自动同步。

### 非目标

- 不动协议帧类型（`SessionInfo.Title` / `MetaPayload.Title` 字段已存在）。
- 不区分 "icon title" 与 "window title"：atterm 是 1 PTY = 1 session = 1 tab，无需双字段。OSC 0/1/2 统一写入单一 `title`，last-writer-wins。
- 不改 shell 会话的显示：`type !== 'ai'` 时各显示层保持现状（cwd basename / 命令名）。
- 不抽通用 OSC parser；OSC 0/1/2 与现有 OSC 133 处理点并列、独立缓冲。

## 2. 背景

### 现状

- `proto.SessionInfo.Title` / `proto.MetaPayload.Title` 字段已存在并已参与广播。`internal/session/session.go::UpdateMeta` / `UpdateCwdTitle` 已有差值比较 + META 广播逻辑。
- `Title` 当前**只**在 `desktop/relay_host.go` 创建会话时被填为 `req.Command`，PTY 流里的 OSC 标题序列**完全未解析**。
- 4 处会话栏的当前显示逻辑：
  - 桌面 TabBar (`desktop/frontend/src/components/TabBar.vue::shortTitle`)：cwd basename，否则命令首 token。
  - 桌面 TaskGroupedList (`components/TaskGroupedList.vue` + `lib/sessionLabel.ts::commandLabel`)：命令首 token（path 剥离）。
  - 移动 MobileSessionCard：同 `commandLabel`。
  - Web SessionList (`web/src/main/components/SessionList.vue`)：`s.command || unknownCommand`，模板直接 inline。
- AI 会话判定：`internal/session/classify.go::ClassifyCommand` 把 `codex` / `claude` / `gemini` / `aider` 归到 `SessionTypeAI`。`SessionInfo.Type` 已通过 META 广播（红线 14）。

### iTerm2 参考（决策依据）

iTerm2 / XTerm 的三件套：

- `ESC ]0;<str>ST` — 同时设 window title 与 icon (tab) title。
- `ESC ]1;<str>ST` — 仅 icon (tab) title。
- `ESC ]2;<str>ST` — 仅 window title。

`ST` = `BEL (0x07)` **或** `ESC \ (0x1b 0x5c)`。

iTerm2 内部维护 `terminalIconName` / `terminalWindowName` 两个独立变量，并提供 "OSC 0/2 影响全窗口 vs 仅当前 session" 的设置。atterm 的会话模型不需要这层区分，统一合并到单 `title`。

iTerm2 的私有 OSC 1337（CursorShape / SetMark / CurrentDir / ClearScrollback…）与标题三件套**无关**，本设计不涉及。

参考：
- [iTerm2 Proprietary Escape Codes](https://iterm2.com/documentation-escape-codes.html)
- [iTerm2 Session Title](https://iterm2.com/documentation-session-title.html)

## 3. 整体架构

```
PTY bytes
  ↓ session.Session.Append(data)
applyOSC133Locked  (existing)  →  task_state / summary / type
applyOSCTitleLocked (NEW)      →  s.meta.Title (差值才生效)
  ↓ on change
s.UpdateMeta(MetaPayload{Title: t})  // 既有路径
  ↓ broadcast
local subscribers (desktop driver UI via lib/connection.ts onMeta)
  ↓ uplink ANNOUNCE  (info.Title 同步刷新)
relay mirror session
  ↓ META frame fanout
remote /client subscribers (mobile / web / 第二台 desktop)
  ↓ connection.ts onMeta → SessionInfo.title
  ↓
UI: TabBar / TaskGroupedList / MobileSessionCard / SessionList
    + Wails WindowSetTitle(active tab's title) via platform/wails.ts
```

零协议改动。Replay/reattach 时 viewer 拿到的就是 mirror 的当前 `s.meta`，`title` 已是最新值。

## 4. 后端：`applyOSCTitleLocked`

### 4.1 位置 / 签名

`internal/session/session.go`，与 `applyOSC133Locked` 并列：

```go
// applyOSCTitleLocked scans data for OSC 0/1/2 (icon/window title) sequences
// and updates s.meta.Title in place when the value changes. Called from
// session.Append, same lock window as applyOSC133Locked. Returns true if
// title changed (caller broadcasts META).
//
// All three prefixes (0/1/2) write the same field; atterm has a 1:1 PTY:tab
// mapping so we don't distinguish icon vs window title. Last writer wins.
func (s *Session) applyOSCTitleLocked(data []byte, now time.Time) bool
```

### 4.2 解析规则

- 匹配 `\x1b]<n>;<payload><ST>`，其中 `<n>` ∈ {`0`, `1`, `2`}，`<ST>` ∈ {`\x07`, `\x1b\\`}。
- `<payload>` 长度上界 **256 字节**：超过即视为非法序列，整个序列丢弃（防 OSC bombing）。
- `<payload>` 解析后做轻量 ANSI strip（剥掉残留控制字符），UTF-8 校验，非 valid UTF-8 整段丢。
- 一次 `Append` 含多段 OSC 标题 → 最后完整匹配胜出（last-writer-wins）。
- 与 `s.meta.Title` 相同 → 不更新、不返回 true（无 noisy META）。

### 4.3 跨 Append 缓冲

新增 `s.oscTitleBuf []byte`，与 `s.osc133Buf` **独立**：

- `Append` 内：先把 `oscTitleBuf` 与 `data` 拼接后扫描，扫到完整 OSC 标题序列就消费 + 推进；末尾若有未闭合的 `\x1b]<n>;...` 残留则留入 buf（保留尾巴模式），最大保留 `256 + 8` 字节，再超就丢弃整段 buf。
- buf 操作仿照现有 `keepOSC133Tail`，新增 `keepOSCTitleTail`。

### 4.4 调用点

在 `session.Append` 里，紧跟 `applyOSC133Locked(data, now)` 之后扫一遍 OSC title。两个 helper 都在同一锁窗口里更新 `s.meta`，返回 bool 表示"是否需要广播"。`Append` 拿 OR 后的结果走**一次** META 广播路径（具体广播调用以 `applyOSC133Locked` 的现状为准 —— 当前 OSC 133 的更新经由 `UpdateMeta` / 内部 broadcast 完成；本设计让 OSC title 复用同一出口）。

伪代码：

```go
changedTask := s.applyOSC133Locked(data, now)
changedTitle := s.applyOSCTitleLocked(data, now)
if changedTask || changedTitle {
    // 走与 applyOSC133Locked 现状一致的 META 广播出口，避免双发
}
```

实现时按现有 `applyOSC133Locked` 完成 META 广播的具体方式对齐 —— 不引入新的 broadcast helper 命名。

### 4.5 安全考虑

- 不把 title 传入任何 shell / template / regex 求值；只送 META 字节、Wails `WindowSetTitle` 字符串、Vue 模板 `{{ }}`（默认 HTML 转义）。
- 长度 256 + UTF-8 校验已防恶意载荷。

## 5. 前端：4 处显示层 + 窗口标题

### 5.1 桌面 TabBar (`desktop/frontend/src/components/TabBar.vue`)

新增 helper（与 `shortTitle` 并列）：

```ts
function tabTitle(s: SessionInfo | null): string {
  if (s?.type === 'ai' && s.title) return s.title;
  return shortTitle(s);
}
```

`<span class="title">{{ tabTitle(t.activeSession) }}</span>`。
tooltip (`:title`) 维持现状（含 `command`）。
`max-width: 220px` + `text-overflow: ellipsis` 兜底长标题，不动 CSS。

`TaskStateIcon` SVG 保留 —— title 只换文字部分，状态图标位不变。

### 5.2 桌面 Wails 主窗口标题

`desktop/frontend/src/platform/types.ts` 在 `WindowBridge`（如不存在则新增）加：

```ts
export interface WindowBridge {
  setTitle(title: string): void
}
```

适配实现：

- `desktop/frontend/src/platform/wails.ts`：调用 `@wailsapp/runtime` 的 `WindowSetTitle(title)`。
- `desktop/frontend/src/platform/capacitor.ts`：no-op。
- `desktop/frontend/src/platform/fake.ts`（测试）：记录最近一次 title。

`App.vue` 在 `currentTab` / `currentTab.activeSession.title` / `.type` 变化时：

```ts
watchEffect(() => {
  const s = currentTab.value?.activeSession
  const title = (s?.type === 'ai' && s.title) ? s.title : 'AT Term'
  platform.window?.setTitle(title)
})
```

- **不**叠加 `●/✻` 等前缀：claude 自己已经在 OSC 标题里加好状态字符；二次拼接会出现 `● ● Remove ...`。

### 5.3 桌面 TaskGroupedList + 共享 `sessionLabel.ts`

`desktop/frontend/src/lib/sessionLabel.ts`：

```ts
export interface SessionLike {
  current_command?: string
  title?: string
  session_id: string
  cwd?: string
  type?: string   // NEW: optional, 'ai' | 'shell' | 'test' | 'build' | 'deploy'
}

// aiTitleOrCommand returns the AI-set window title when the session is an
// AI task and a title is available; otherwise the existing short command
// label. Used by desktop TaskGroupedList and mobile MobileSessionCard.
export function aiTitleOrCommand(s: SessionLike): string {
  if (s.type === 'ai' && s.title) return s.title
  return commandLabel(s)
}
```

`TaskGroupedList.vue` 行内 `.cmd` span：`{{ aiTitleOrCommand(s) }}`。tooltip 仍用 `fullCommand` / `rowTitle`，以便用户 hover 看到原始命令。

### 5.4 移动端 `MobileSessionCard.vue`

`.cmd` span 改为 `aiTitleOrCommand(props.session)`。tooltip / `cmd-and-cwd` 其它部分不变。

### 5.5 Web `SessionList.vue`

无共享 `sessionLabel` helper，模板内直接 inline：

```vue
{{ (s.type === 'ai' && s.title) ? s.title : (s.command || t('main.unknownCommand')) }}
```

`.type-chip` 与 `.cwd` 行保持不变。

## 6. 测试

### 6.1 Go (`internal/session/session_test.go`)

- `TestApplyOSCTitle_OSC0_BEL` — `ESC ]0;hello\x07` → `Title=hello`。
- `TestApplyOSCTitle_OSC1_BEL` — OSC 1 走通同字段。
- `TestApplyOSCTitle_OSC2_BEL` / `TestApplyOSCTitle_OSC2_ST` — 覆盖 `ST = ESC \\`。
- `TestApplyOSCTitle_SplitAcrossAppend` — `Append("\x1b]2;par")` + `Append("t two\x07")` → `Title="part two"`。
- `TestApplyOSCTitle_MultipleLastWins` — 同次 Append 多段 OSC，取最后。
- `TestApplyOSCTitle_OverlongDropped` — payload > 256 字节 → title 不变。
- `TestApplyOSCTitle_SameTitleNoChange` — 重发同值 → 返回 false，无 META。
- `TestApplyOSCTitle_AlongsideOSC133` — 同帧含 OSC 133 'D' + OSC 2 → 两个 buf 互不踩，task_state 与 title 都正确更新。
- `TestApplyOSCTitle_AllThreePrefixesUpdateSameField` — 0/1/2 三种前缀连发，最后一个胜出。
- `TestApplyOSCTitle_InvalidUTF8Dropped` — payload 含非法字节 → 整段丢弃。

### 6.2 TypeScript (vitest)

- `TabBar.test.ts` 新增 case：
  - `type='ai' + title='foo'` → tab 显示 `foo`。
  - `type='ai' + title=''` → fallback `shortTitle`（cwd basename）。
  - `type='shell' + title='foo'` → 显示 cwd basename（不显示 title）。
- `TaskGroupedList.test.ts`：AI + title 渲染。
- `mobile/__tests__/MobileSessionList.test.ts`：AI + title 渲染。
- `desktop/frontend/src/App.test.ts`：mock `platform.window.setTitle`，断言激活 tab 变化 / title 变化时被调用为新值；切到非 AI tab 时被调用为 `"AT Term"`。
- Web vitest：`SessionList` 测试加 AI + title 用例。

### 6.3 手测

- 在 desktop 起 `claude` 会话 → tab 文字与窗口标题应自动跟 claude 的 OSC 标题。
- 在 desktop 起 `zsh` 会话 → tab 文字保持 cwd basename（即便 oh-my-zsh 发了 OSC 0）。
- 在 desktop 起两个 AI tab，切 tab → 窗口标题跟随激活 tab。
- 关闭最后一个 AI tab，切到 shell tab → 窗口标题还原 `AT Term`。
- 用手机 attach 同一台 desktop 的 AI 会话 → MobileSessionCard 显示一致的 AI 标题。

## 7. 风险与回滚

- **noisy 高频更新**：claude 在 thinking 时可能每秒多次刷标题。差值去重 + 现有 META client-side pacing（红线 12）兜底；不新增节流。如实测仍 noisy，再加 250ms 防抖单点收敛。
- **OSC 标题里嵌 OSC 133**：极小概率。parser 只在闭合 ST 后才把 payload 当 title；不闭合留 buf；嵌套 ESC 会跟随被丢入 payload，最差是看到怪 title 字符串，不至于污染 OSC 133 状态机（独立 buf）。
- **回滚路径**：前端 4 个显示层各加一行条件分支；后端新增独立 helper + buf 字段，回滚只需 revert PR。`SessionInfo.Title` 字段早就在协议里，回滚不破协议。

## 8. 何时改哪里（速查）

| 触动 | 文件 |
|---|---|
| OSC 0/1/2 解析 | `internal/session/session.go`（新增 `applyOSCTitleLocked` + `oscTitleBuf` + `keepOSCTitleTail`）+ `internal/session/session_test.go` |
| 桌面 TabBar 显示 | `desktop/frontend/src/components/TabBar.vue` + `TabBar.test.ts` |
| 桌面窗口标题 | `desktop/frontend/src/platform/types.ts`（`WindowBridge`）+ `platform/wails.ts`（`runtime.WindowSetTitle`）+ `platform/capacitor.ts`（no-op）+ `platform/fake.ts` + `App.vue` watchEffect + `App.test.ts` |
| 桌面 TaskSidebar | `desktop/frontend/src/lib/sessionLabel.ts`（`SessionLike.type` + `aiTitleOrCommand`）+ `components/TaskGroupedList.vue` + 测试 |
| 移动端 | `desktop/frontend/src/mobile/MobileSessionCard.vue` + `mobile/__tests__/MobileSessionList.test.ts` |
| Web 端 | `web/src/main/components/SessionList.vue` + 对应 vitest |

## 9. 待 review 点（请确认）

无强余项。如果开始实现时发现以下任一情况，需回到本 spec 修订：

1. claude 实际发出的 OSC 序列编号与本设计假设（0/1/2）不一致。
2. `s.broadcastMetaLocked()` 没有现成 helper，需要从 `UpdateMeta` 抽取公共代码。
3. `WindowBridge` 在 `platform/types.ts` 已存在但签名不同。

以上三点都属于"按现状调整命名/装配"层面，不动整体方案。
