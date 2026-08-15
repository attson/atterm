# Hook 驱动的 task_state：让 AI 会话状态不再靠猜

Date: 2026-08-16
Status: Design

## Background

`task_state` 是整个产品的中枢状态：侧栏图标、挂件情绪与徽标、未读计数、飞书卡片、
自动展开、会话排序，全部读它。

对普通 shell，它有权威来源——OSC 133 的 `C`（命令开始）和 `D`（命令结束带 exit code）
由 shell integration 精确上报。**但对 AI 会话没有**：

- Claude Code v2.x / Codex 都是内联渲染，不进 alt-screen，终端模式无从判别
- `claude` / `codex` 对 shell 而言是**一条长期运行的命令**，整个会话期间 OSC 133 的
  `D` 永远不会触发

于是 `internal/session/silence.go` 用**输出静默**来推断：`running` 状态下超过 5 秒没有
「有效输出」就翻成 `waiting_input`。它的本意是抓「AI 答完了，正停在输入框等你」——那确实
是最该提醒用户的时刻。

**但静默无法区分两件事**：

| 现象 | 终端看到的 | 想抓的？ |
|---|---|---|
| Claude 答完了，在等你输入 | 没有输出 | ✅ |
| Claude 正在思考 / 跑不打印东西的工具 / 等网络 | 没有输出 | ❌ 误伤 |

结果是 AI 会话干活时状态在 `running` ↔ `waiting_input` 之间反复翻转。下游全部跟着抖：
侧栏图标闪烁、挂件 live/now 横跳、挂件反复自动展开、飞书卡片重复推送。

2026-08-15 修掉了其中一个更糟的 bug（恢复侧累加器没有时间窗口，导致连纯光标闪烁都能
把状态推回 `running`，见 `6232b1d`），但**启发式的固有分辨率极限没有变**：真实工作中
输出间隔超过 5 秒，状态照样翻转。

### 关键发现：权威信号一直都在，只是没接

`desktop/hookinstall` 早就在给 Claude Code 装五个 hook（`Notification` / `PreToolUse` /
`UserPromptSubmit` / `Stop` / `PostToolUse`），`atterm-hook` 把事件 POST 到桌面进程，
`desktop/feishu/hook_server.go` 解析后分发。

也就是说 **AI 客户端本来就在权威地告诉我们「这一轮开始了」和「我在等你」**——但这些事件
今天只喂给了飞书卡片，完全没有接到 `task_state` 上。改 `TaskState` 的地方只有
`session.go` / `silence.go` / `osc133.go` 三处，没有 hook 的份。

### Codex 现状（2026-08-16 实测，codex-cli 0.146.0）

| 事实 | 来源 |
|---|---|
| `hooks` 特性已是 `stable` 且默认 `true` | `codex features list` |
| 事件名与 Claude 高度重合：`PreToolUse` / `PostToolUse` / `UserPromptSubmit` / `Stop` / `PermissionRequest` / `SessionStart` / `SessionEnd` / `SubagentStart\|Stop` / `Pre\|PostCompact` | 二进制符号 |
| payload 字段：`session_id` / `turn_id` / `cwd` / `hook_event_name` / `model` / `transcript_path` / `tool_name` / `tool_input` | 二进制符号 |
| payload 走 **stdin**（错误码里有 `stdin_error`，且与 `SHELL -lc` 同现） | 二进制符号 |
| 配置在 `~/.codex/hooks.json`，handler 类型只有 `command` | 二进制符号 |

结论：`atterm-hook` 现有的「读 stdin」逻辑对两家通用，不需要分支。

## Goals

- AI 会话的 `running` / `waiting_input` 由 AI 客户端上报，不再由输出静默推断
- 同时覆盖 Claude Code 和 Codex
- 没装 hook 的会话（旧版本 CLI、容器里跑的、其它 AI 客户端）行为不变

## Non-goals

- 不新增 `TaskState` 枚举值（不引入 `thinking` 之类）
- 不改动 `task_state` 的下游语义（未读、通知、卡片一律照旧）
- 不处理 `SessionStart` / `SubagentStart|Stop` / `Pre|PostCompact`
- 不做「running 置顶」的排序改动（那是独立的后续项，依赖本设计落地）

## Architecture

### 权威归属：`internal/session`

三个候选落点，选 A：

| | 方案 | 取舍 |
|---|---|---|
| **A ✅** | Session 上加 `hookDriven` 闩锁 + `ApplyHookState`，`rescheduleSilenceTimerLocked` 见闩锁不武装 | 「关掉启发式」成为**结构性事实**而非调用顺序约定；mirror/ANNOUNCE 传播自动生效；可在 session 包内单测 |
| B | desktop 层把 hook 翻译成 `UpdateMeta` 调用 | `UpdateMeta` 内部就会重新武装计时器，最终仍需在 Session 上加闩锁——绕一圈回到 A |
| C | 直接在 `desktop/feishu/hook_server.go` 里改状态 | 把核心状态机埋进飞书包，`internal/session` 不知道谁在改自己 |

数据流：

```
claude/codex ──stdin JSON──> atterm-hook ──HTTP POST──> HookServer
                                                            │
                                              (agent_kind 分发到 adapter)
                                                            ▼
                                              relayHost: sid ──> Session
                                                            ▼
                                              Session.ApplyHookState(state)
                                                 ├─ hookDriven = true（闩锁）
                                                 ├─ meta.TaskState = state
                                                 ├─ waiting 时 bump AttentionAt
                                                 └─ 广播 META
```

`HookServer` 新增一个窄接口（照抄现有 `SessionLookup` 的模式），由 `relayHost` 实现，
所以 `feishu` 包只多认识一个接口、不碰 session 内部。

### 事件映射

| 语义 | Claude Code | Codex | → `task_state` |
|---|---|---|---|
| 这一轮开始 | `UserPromptSubmit` | `UserPromptSubmit` | `running` |
| 工具调用中 | `PreToolUse` / `PostToolUse` | 同名 | `running` |
| 这一轮说完 | `Stop` | `Stop` | `waiting_input` + bump `AttentionAt` |
| 需要批准 | `Notification` | `PermissionRequest` | `waiting_input` + bump `AttentionAt` |

`Stop` → `waiting_input` 与今天启发式产生的语义一致，下游（未读 / 挂件 / 卡片）无需改动。
未列出的事件一律忽略：turn 级事件已经覆盖状态机，多解一个就多一处会漂移的地方。

### 闩锁生命周期

```
第一个 hook 事件      → hookDriven = true，静默计时器永不再武装
OSC 133 D（AI 命令退出）→ 清闩锁 + 落 completed/failed，回到普通 shell 的启发式
会话关闭              → 清闩锁
```

**闩锁前的窗口仍归启发式管**——刚开的 `claude` 在你提交第一个 prompt 之前不会有任何 hook
事件，此时启发式正确地把它落到 `waiting_input`。第一个 `UserPromptSubmit` 到达时才切换权威。

### 入口与安装

`agent_kind` 目前硬编码在 `cmd/atterm-hook/main.go:64`。改成 argv：

```
atterm-hook --agent claude-code    # ~/.claude/settings.json
atterm-hook --agent codex          # ~/.codex/hooks.json
```

两边都显式传，不留隐式默认——安装器是唯一的写入方，没有兼容旧命令行的必要
（`hookinstall` 每次启动都会重写并修复）。

codex 侧只写**用户级** `~/.codex/hooks.json`：repo-local 的 `.codex/config.toml` 在交互式
会话里不生效（[openai/codex#17532](https://github.com/openai/codex/issues/17532)）。

`ATTERM_SESSION_ID` 由 PTY 环境注入，codex 派生的 hook 命令同样继承，会话归属无需额外工作。

## Edge cases

| 情况 | 处理 |
|---|---|
| AI 命令退出后到达的滞后 `Stop` | hook 只在会话当前分类为 `ai` 时生效，不会把普通 shell 打成 `waiting_input` |
| hook 中途静默失效（用户手改配置、CLI 崩溃） | 会话卡在最后一个状态。这是「hook 全权」的**已知代价**，用户已在设计时确认接受 |
| 同一会话并发的 hook POST | Session 互斥锁覆盖；事件本身幂等（都是赋值） |
| mirror / 远程会话 | hook 只到达运行 AI 的那台机器，状态经 ANNOUNCE 正常传播，无需改动 |
| 没装 hook 的 AI 会话 | 永不闩锁，完全走今天的启发式 |

## Testing

| 层 | 用例 |
|---|---|
| `internal/session` | 闩锁后计时器不再武装；四条映射各自落对状态；`waiting` 会 bump `AttentionAt`；OSC 133 D 清闩锁；非 `ai` 会话拒绝事件 |
| adapter | 两家的真实 payload 各自解析出正确事件；未知 `hook_event_name` 安全忽略 |
| `hookinstall` | codex `hooks.json` 的写入、幂等、以及不破坏用户已有的其它 hook 条目 |
| `atterm-hook` | `--agent` 解析；缺失时的行为 |

变异验证重点：去掉闩锁后，「计时器不武装」的测试必须变红——否则那条测试没有真正锁住
启发式被关闭这件事。

## Rollout

无 feature flag。`hookinstall` 在启动时自动重写两份配置，用户下次启动 AI 会话即生效。
唯一的用户可见变化是状态更准，没有需要迁移的持久化数据。
