# Feishu — 远程终端与 AskUserQuestion form 子系统

> **Audience**: 实现或审计 atterm 飞书集成路径的工程师(anchor card 生命周期 / AskUserQuestion 表单式远程回答 / local vs relay 模式分流)
> **Last updated**: 2026-10-15
> **Status**: shipped through v0.2.171
> **See also**: [architecture.md](./architecture.md) · [protocol.md](./protocol.md) · [auth.md](./auth.md) · [../superpowers/specs/2026-07-10-askuserquestion-form-flow-spec.md](../superpowers/specs/2026-07-10-askuserquestion-form-flow-spec.md)

## 目录

1. [设计目标与历史](#1-设计目标与历史)
2. [两种模式:local vs relay](#2-两种模式local-vs-relay)
3. [Anchor card 生命周期](#3-anchor-card-生命周期)
4. [AskUserQuestion form flow](#4-askuserquestion-form-flow)
5. [Hook payload 与 tool_use 桥接](#5-hook-payload-与-tool_use-桥接)
6. [已知限制与 workaround](#6-已知限制与-workaround)
7. [错误码与常见故障](#7-错误码与常见故障)
8. [代码入口速查](#8-代码入口速查)

---

## 1. 设计目标与历史

飞书集成经历了三个阶段:

1. **独立通知卡**:早期 relay 通用 webhook 路径已经下线;当前由 desktop 飞书 service 在 local / relay 两种模式下统一发送命令完成、等待输入和 Agent 提问卡片。
2. **anchor card 作为远程终端**(v0.2.15x+,PR 系列 `feishu-as-terminal`):把 IM 卡片升级成**持续更新的锚点卡** —— body 流式承载 assistant 输出、卡片自带输入框 + `^C/^D/Esc/Enter/结束` 按钮直接把用户操作送回本地 pty。用户不用打开桌面就能远程回复正在等待的 AI 会话。
3. **AskUserQuestion 表单式回答**(v0.2.171,PR #261):claude-code 触发 AskUserQuestion tool 时,锚点卡片挂载一份 form container —— 每题一行下拉 + 自定义输入框,提交后系统按反向工程的按键模型自动送 stroke 到本地 claude TUI。

设计一致性:所有阶段共用**一个 `internal/feishu` client**,同一 anchor card 上的四个子元素(`anchor_body_md` / `anchor_input` / `anchor_buttons` / `anchor_askform`)状态互相独立,DELETE 和 CREATE 全部幂等。

## 2. 两种模式:local vs relay

| 维度 | local 模式 | relay 模式 |
|---|---|---|
| 网络路径 | 桌面 app 直连飞书 Open Platform(LongConn) | 桌面 app → 中央 relay(`cmd/atterm-relay`)→ 飞书 |
| 应用凭据存储 | 本机钥匙串(macOS Keychain / Linux Secret Service / Windows Credential Manager) | Relay `users.db` 里的字段级加密 + DB `relay_config` 表的 `AdminConfig.Feishu`(结构体定义在 `internal/relay/admin_config.go`;SQL 迁移在 `internal/userstore/migrations/`;SQLite/Postgres 双后端) |
| 事件订阅 | 桌面 app 直接跑 `feishu-sdk-go` LongConn subscriber | Relay 跑 subscriber,通过 `/uplink` 转发到桌面 |
| 适用场景 | 单人自用 / 内网 / 无公网 relay 时 | 多设备访问 / 团队共享 relay / 桌面不常在线 |
| 配置入口 | 桌面 Settings → Feishu → Local | 桌面 Settings → Feishu → Relay(实际写 `/admin/api/feishu`) |

**模式切换是显式的**(桌面 Settings 有 mode selector),两种模式的 credential 不共享。切换模式不迁移凭据,需要在新模式下重新绑定飞书应用。

Local 模式保存凭据时先向飞书换取一次 `tenant_access_token` 做校验；校验失败不覆盖原配置。校验成功后清理旧 token 缓存并立即重建 LongConn，无需重启桌面 app。仅轮换同一 App ID 的 secret 时保留已有绑定；App ID 改变时必须清除旧 `open_id`，由用户重新执行 `/bind`（飞书 `open_id` 是 app-scoped）。

设置页“解绑”只清除 `open_id`、绑定时间和 disabled 状态，保留四项应用凭据、通知开关与远程接管设置；同时必须调用 `OnRemoteTerminalToggle(false)` 拆除运行中的 subscriber 并归档旧 Anchor Card，避免重新绑定时被误判为已经挂载。解绑后可直接生成新短码重新 `/bind`。Local Keyring 与 Relay DB 必须保持相同语义。彻底删除 Relay 配置属于管理操作，不复用用户侧解绑入口。

Local 短码绑定成功后必须触发一次存量会话回填。原因是远程接管设置会跨解绑保留：若开关原本已经开启，重新绑定只改变 `open_id`，不会再产生 toggle 事件。`ServiceConfig.OnBound` 在 `SetBound` 成功后调用 `relayHost.OnRemoteTerminalToggle(true)`，按当前 `SessionAutoAttach` 扫描 live sessions；无效短码或持久化失败不得触发。

### 2.1 连接状态

Settings → Feishu 每 3 秒刷新一次事件连接状态。Local 模式的状态直接来自飞书 SDK LongConn 的物理 WebSocket 生命周期回调，而不是根据“已保存凭据”推断：`disconnected`、`connecting`、`connected`、`reconnecting`、`error`。最近一次连接错误随状态一起展示；连接成功后清除旧错误。Relay 模式的 WebSocket 位于 relay 进程，desktop 无法直接观测，因此明确显示 `relay_managed`，不伪装成 `connected`。

“长连接已连接”只表示飞书事件可以到达当前进程，不代表 CardKit / IM HTTP API 调用一定成功；发卡、更新卡片的 token 或权限错误仍由对应操作和日志单独报告。多个本地 AT Term 实例若用同一飞书应用同时建立 LongConn，飞书可能把 `/bind` 事件投递给任一实例；开发调试时应只保留一个消费实例，否则某实例生成的短码可能被另一实例收到并判定无效。

### 2.2 独立通知卡开关

Settings → Feishu 的“自动发送的通知卡片”提供 5 个本机持久化开关，local / relay 模式共用同一策略：

| 配置值 | 卡片 | 判定 |
|---|---|---|
| `command_success` | 命令完成（成功） | command event 未 sealed 且 exit code = 0 |
| `command_failure` | 命令完成（失败） | command event 未 sealed 且 exit code != 0 |
| `command_sealed` | 命令完成（仅本机可见） | command event 带 sealed body；优先级高于 exit code |
| `waiting_input` | 等待输入 | waiting-input event 不带结构化选项 |
| `ask_question` | Agent 提问 | waiting-input event 带结构化选项 |

旧配置没有这些字段时全部按开启处理，保证升级后通知行为不变。“仅 AI 会话通知”是独立的会话范围过滤，会与卡片类型开关叠加。远程终端 Anchor Card 仍只受“启用飞书远程接管”控制；设置页手动测试发送也不受自动通知开关限制。

**红线**:
- Local 模式的钥匙串存储在 `desktop/feishu/binding_store_local.go`,不要走 sqlite。
- Relay 模式的字段加密走 `internal/relay/admin_config.go::AdminConfig.Feishu.EncryptKey`(base64 32B),持久化在 DB `relay_config` 表(SQLite/Postgres,由 `ATTERM_RELAY_DB_DRIVER`/`ATTERM_RELAY_DB_DSN` 选后端;`relay.json` 已完全退役,不再读写),GET 只回显末 4 位。见 [AGENTS.md 红线 #26](../../AGENTS.md#关键设计原则红线)。
- `userstore.Open` 允许 secret cipher 为 nil(即使 relay 没启用飞书也能启动),仅飞书 CRUD 在 cipher nil 时报错;不要把 `ATTERM_FEISHU_ENCRYPT_KEY` 重设为启动必填。

## 3. Anchor card 生命周期

### 3.1 四个子元素

一张 anchor card(Feishu CardKit v2 `schema: "2.0"`,`cardkit:card:write` scope)包含四个 `element_id`:

| element_id | 内容 | mounted-态标记 |
|---|---|---|
| `anchor_body_md` | Markdown 流式输出(assistant text + status preamble) | 始终存在,不删 |
| `anchor_input` | 单行 text 输入框,提交后触发 `input` card action | `CardAnchor.CurrentInputID != ""` |
| `anchor_buttons` | shell: `上页 / 下页 / 中断 / 结束`;AI:`中断 / Esc / Enter / 结束` | `CardAnchor.ButtonsMounted` |
| `anchor_askform` | AskUserQuestion form container(仅在 tool 触发时挂) | `CardAnchor.FormMounted` |

**三件独立**:input / buttons / form 各自 gate 在自己的 mounted 标记,DELETE 和 CREATE 幂等。挂 form 时先 DELETE input 和 buttons(form 独占交互面),拆 form 时按 mounted 标记独立重建 input 和 buttons。

### 3.2 状态转换

```mermaid
stateDiagram-v2
    [*] --> Idle : SendAnchorCard(初始 body + input + buttons)
    Idle --> Idle : PatchAnchor(body 流式)
    Idle --> FormMounted : InsertAnchorForm\n(先 DELETE input + buttons)
    FormMounted --> FormMounted : (用户在飞书 form 上操作,不影响服务端状态)
    FormMounted --> Idle : DeleteAnchorForm\n(独立重建 input + buttons)
    Idle --> [*] : ArchiveAnchor(grey card,输入框 + 按钮 strip 掉)
```

### 3.3 SendMu 序列化

`CardAnchor.SendMu` 是同一 anchor 的 send 序列化锁。**每一个** CREATE / DELETE / PATCH / UpdateCardElement 都在 SendMu 内分配 `PatchSeq` 并发出 —— 飞书对同一 card 的 op 用 monotonic sequence 强校验(`code=300317 sequence compare failed`),SendMu 保证同一 anchor 下不会有 seq 交叉。

流式 body PATCH 频率高(事件驱动),要确保它和 form / input / buttons 的 op 都排队走 SendMu。PTY / hook chunker 最快每 100ms 产生一个 body 快照,但 CardKit HTTP 往返通常明显更慢;`latestAnchorPatchQueue` 保证每张卡最多一个 body PATCH 在途,等待期间只保留最新快照,禁止把每个中间态都排进 `SendMu`。否则输入框的 DELETE+CREATE 和新输出会被旧 PATCH 队列挡住,表现为提交后数秒才回显。detach 时必须先 Stop queue、丢弃 pending body,再归档卡片,避免归档后被旧 body PATCH 覆盖。

Anchor body 顶部状态行同时显示任务态、运行时长和最近一次正文刷新的本地时间,例如 `活跃 · 已 1m · 最后输出 20:49:32`。尚未收到正文时省略“最后输出”。运行时长只在正文或任务态真实变化时顺带刷新;禁止为了推进运行时长对空闲卡片做周期 PATCH,因为飞书客户端可能在 sibling body 更新时重建 `anchor_input`,清空用户尚未提交的草稿。

Local LongConn 的 `card.action.trigger` 必须同步返回非 nil 的空 `CardActionTriggerResponse`,明确结束飞书客户端的控件 busy 状态;PTY 注入、输入框 DELETE+CREATE 和 body PATCH 仍走异步路径。返回 nil 虽然 WS status 仍是 200,但客户端可能一直显示发送中,直到后续 CardKit 操作触发重绘。

Shell anchor 不是完整终端模拟器,但必须正确投影 PTY 的行控制:`CRLF` 结束一行,独立 `CR` 表示光标回到第 0 列并由后续文本覆盖当前 open line。不能把 `\r` 原样写进 Markdown code fence;bash 在关闭 bracketed-paste (`CSI ?2004l`) 后常发一个独立 `\r`,飞书会把它渲染成下一行开头的大段空白。`session.Subscribe()` 交付的 `TypeOut.Payload` 是 wire payload,前 8 字节为 big-endian sequence;飞书 subscriber 必须先调 `proto.DecodeOut`,只把解出的 PTY data 交给 roller。直接消费 `frame.Payload` 会在 sequence 低字节为 `0x0a` / `0x0d` / `0x20` 时伪造换行、回车和缩进。

Shell roller 保留最近 300 行 / 50KB 的有界历史,但卡片只承担消息预览:每页最多 12 行 / 3KB,使用转义后的普通 Markdown,不使用 fenced code block。原因是飞书移动端会把长代码块强制折叠为“N 行代码”,每次查看都要二次点击;短预览比模拟完整终端更符合消息交互。连续空格转为 NBSP 以尽量保留终端列布局,Markdown 控制符必须转义。默认最新页跟随输出;“上页”会把页尾固定在当前历史索引,因此后续 PTY 输出不会把正在阅读的页面顶走;“下页”向新输出移动,到达最新页后恢复自动跟随。分页 action 必须经 `FeishuSubscriber.drainLoop` 与 `PushBytes` 串行,只 PATCH body,不向 PTY 注入任何字节。

Anchor controls 以手机宽度为基准,一行最多四列。Shell 默认只保留 `上页 / 下页 / 中断(^C) / 结束`,因为输入框提交已经隐含 Enter,`exit` 可替代低频 ^D;AI 不需要分页,显示 `中断(^C) / Esc / Enter / 结束`。不要再把分页和全部终端按键同时塞进一行,否则飞书移动端会把标签全部截成省略号。

同一 PTY 从 shell 启动 AI CLI 时必须原地升级 anchor:先把 `anchorRuntime.aiMode` 置为 true,让已经在 drain 中的 shell flush 失效；再清空 `lastInner` / `lastOutputAt` 并立即 render,移除旧的“第 N/N 页”正文；最后挂 `AIChunker` 并把按钮切为 AI controls。顺序不能反,否则慢到达的 shell PATCH 会把已经切换的 AI 正文覆盖回去。

结构化 turn 有两个来源:Claude 由 `UserPromptSubmit` / `Stop` / `PreToolUse` / `PostToolUse` hooks 进入 `AIChunker`;Codex 由已经绑定到当前 PTY 的 rollout JSONL 增量读取 `item_completed.UserMessage` / `AgentMessage`,并从 `response_item.function_call(name=request_user_input)` 提取交互式问题。Codex 的绑定 SID 优先来自 `SessionStart` hook payload（内层 Codex `session_id` 与外层 `ATTERM_SESSION_ID` 直接配对）；rollout 进程归属/文件变化只为旧版或无 hook 环境兜底。Codex 新会话解析 SID 时首个 turn 往往已经落盘,允许回放当前文件；恢复会话从 EOF 开始,禁止把历史对话整批重发到卡片。新版 Codex 可能由独立 app-server daemon 持有 rollout fd；fallback 仅在同 cwd 只有一个 resolver 时使用新建/变更文件，多个同 cwd resolver 时仍宁可不抓也不能串会话。

Claude `AskUserQuestion` 与 Codex `request_user_input` 可复用同一套飞书 form,但不能复用同一套 PTY 按键序列:Claude 每题完成后还会进入 review 页并用 `1` 确认;Codex 的数字键会直接提交当前选项并前进,最后一题立即完成,自定义内容则走“方向键选中目标 → Tab 进入 notes → 文本 → Enter”。`AskFormQuestion.InputMode` 必须跟随 form 挂载到 `CardAnchor.PendingForm`,提交时按 mode 分流;禁止在 Claude 的 `buildQuestionStrokes` 上叠加 Codex 分支。

### 3.4 Takeover 中途开启的存量回填

`OnRemoteTerminalToggle(true)` 会立即扫描当前 live session,按 `SessionAutoAttach` 回填 anchor card:`all` 处理全部会话,`ai` 只处理已经分类为 AI 的会话,`none` 不处理。关闭→开启、以及开启状态下修改自动接入策略都会触发扫描。创建卡片涉及飞书网络请求,每个会话仍通过 `lazyAttachIfMissing` 异步执行,设置保存不会被阻塞。

立即扫描之外仍保留三个懒触发入口,用于覆盖扫描时尚未分类为 AI、首次创建卡片失败、或恢复后已经收到真实输入的会话:

- `sess.SetOnTaskStateChange` 回调里,每次任务态转换都尝试 `h.lazyAttachIfMissing(ctx, sess, sid)`；恢复会话在真实输入前由 `feishuUserActivated` 门控跳过。
- `Dispatcher.SetOnTurnMissingChunker` 注册 `h.onTurnMissingChunker` —— 一个 `TurnEvent` 到达但该 session 还没有 `AIChunker` 时(即中途开启 takeover、`NewSession` 时没注册 subscriber 的会话)触发同一条懒回填路径；它同样检查恢复会话的 `feishuUserActivated`。
- 仅 `NewSessionReq.recovered=true` 的会话注册 `sess.SetOnInput`:首个成功进入 inbound queue 的 `TypeIn` 触发同一条懒回填路径;`RESIZE`、队列已满而被拒绝的输入都不触发。

这些入口都收敛到 `lazyAttachIfMissing → attachFeishuSubscriberForAutoAttach`,并用 `tryStartLazyAttach` / `clearLazyAttachInFlight` 做 per-session in-flight 门控,防止开关扫描、任务态转换、输入和 TurnEvent 并发触发时重复 attach。`auto_attach=ai` 最终依据 `sess.Info().Type` 判定,不会因普通 shell 的 task-state 或输入事件误建卡。

**行为**:用户在会话跑起来之后才打开 remote-terminal takeover,不需要重启会话或再执行命令,符合自动接入策略的旧会话会立即收到 anchor card。若扫描时 AI 会话尚未完成分类,会在后续分类/任务态变化或下一个 turn 事件时补上;触发懒补的当次 turn body 可能赶不上异步建卡,之后的 turn 会正常流式更新。

恢复会话是例外：`executeRestore` 创建的本地 shell 在 `NewSessionReq.recovered=true` 下跳过 `NewSession` 末尾的 eager attach，避免应用启动时恢复多个空闲 pane 就向飞书群发一批空 Anchor Card。恢复 AI 自动注入的 resume 命令会产生 AI 分类、任务态和 hook turn 事件，这些都属于启动机制，在首个成功入队的真实 `TypeIn` 前统一由 `feishuUserActivated` 门控抑制；首个真实输入会先打开门控再立即懒回填。这个抑制不影响用户手动开启接管或重新绑定时对当前 live sessions 的显式扫描。

## 4. AskUserQuestion form flow

### 4.1 Form 挂载

Hook `PreToolUse(matcher=AskUserQuestion)` 到达时,桌面 app 走以下步骤挂 form:

```mermaid
sequenceDiagram
    autonumber
    participant claude as claude-code(本地 pty)
    participant hook as atterm-hook CLI
    participant app as desktop app
    participant feishu as 飞书 Open Platform
    participant user as 用户(飞书 DM)

    claude->>hook: PreToolUse hook trigger
    hook->>app: POST /hook (payload includes tool_use.input.questions)
    app->>app: extractAllAskUserQuestions → []AskFormQuestion
    app->>app: acquire anchor.SendMu

    Note over app: mountSeq = atomic.AddInt64(&anchor.PatchSeq, 1)
    app->>feishu: DELETE anchor_input(mounted 则删)
    app->>feishu: DELETE anchor_buttons(mounted 则删)
    app->>feishu: CREATE anchor_askform<br/>(widget element_ids 带 _<mountSeq> 后缀)
    app->>app: anchor.FormMounted = true<br/>anchor.PendingForm = specs
    app->>app: release SendMu

    feishu-->>user: form 在飞书 DM 上可见
```

Widget element_id 命名规则(每次挂 form 换新):

- 单选下拉:`askform_q<i>_sel_<mountSeq>` (`tag: "select_static"`)
- 多选下拉:`askform_q<i>_sel_<mountSeq>` (`tag: "multi_select_static"`)
- 自定义输入:`askform_q<i>_txt_<mountSeq>` (`tag: "input"`)

`<mountSeq>` 是 mount 时 `PatchSeq` 的当前值,每次挂 form 换新 —— 见 [§6.2 飞书 form widget 状态缓存](#62-飞书-form-widget-状态缓存)。

### 4.2 用户提交后的 stroke plan

用户在飞书 form 上填完点提交,飞书回调 card action(`kind: "form"`)。桌面 app 走以下步骤按题构造 stroke 序列送到本地 claude pty:

```
handleAskFormSubmit(sessionID, operatorOpenID, formValue)
  ├─ anchor.SendMu.Lock() → snapshot anchor.PendingForm → Unlock
  ├─ parseAskFormSlots(formValue) → []askFormSlot
  │      每 slot 含:idx / sel(string,单选) / selMulti([]string,多选) / txt(自定义)
  ├─ for each slot:
  │      qStrokes := buildQuestionStrokes(question, slot)
  │      strokes = append(strokes, qStrokes...)
  ├─ strokes = append(strokes, '1')  // Review page: "Submit answers" 是选项 1
  └─ Router.InjectKeystrokesBySession(sessionID, operatorOpenID, strokes, 350ms)
        // 每键一个 SendInput,首键 inline,余键 goroutine 内 sleep+send
```

### 4.3 每题类型的 stroke 序列

`buildQuestionStrokes` 按 (isMulti, hasCustom) 组合选分支:

| 分支 | Stroke 序列 | Advance 键 |
|---|---|---|
| 单选,无 custom | `<digit>` | 数字键自身触发 select + confirm + advance(claude TUI 特性) |
| 多选,无 custom | `<digit>*M`(每选中一个 label 一个 digit) | Tab(`\t`,0x09):cursor 在 checkbox row 时 Tab advance |
| 单选,有 custom | `<typeIdx> <text runes> <space>`(尾部空格是牺牲字符,见 §6.1) | Enter(`\r`,0x0d):siH.onSubmit 用 siH 自己的 local 状态 |
| 多选,有 custom | `<digit>*M <typeIdx> <↓>×(typeIdx-1) <text runes> <space> <↓>` | Enter(0x0d):走光标到 Submit button,触发容器的 `if (X && Y) { Y(D); return; }` |
| Review page(所有题末尾) | `1` | (合并进主 strokes 序列尾) |

`typeIdx = len(Options) + 1` —— Type-something 在选项列表倒数第二位(最后一位是 "Chat about this")。

**详细按键模型 + 反编译验证 + 拒绝方案清单**见:

- `~/.claude/projects/-Users-attson-code-github-com-attson-atterm/memory/feedback_askform_key_model.md` — 完整血泪史 + 所有推翻的假设(Tab-to-advance 单选 / Ctrl+Return via LF 0x0a / Right-arrow pump / 各种 delay 值)
- [../superpowers/specs/2026-07-10-askuserquestion-form-flow-spec.md](../superpowers/specs/2026-07-10-askuserquestion-form-flow-spec.md) §3-4 — shipped design 记录

### 4.4 Stroke 间隔与序列化

`Router.InjectKeystrokesBySession(sessionID, operator, strokes, 350ms)`:

- 首键 inline(能 surface reject 如 PTY inbound queue 满)
- 剩余键 goroutine 里 `time.Sleep(350ms)` 后 `SendInput`
- 350ms 是硬性下限:小于该值会撞 React state race,大于也无害(4 题 form 大约 4-5 秒)

**红线**:不要把 350ms 降下来省用户等待时间。曾试 80 / 150 / 200 / 350 / 700ms,只有 ≥350ms 稳定。见 memory 里"delay tuning 是死路"章节。

### 4.5 Form 拆除

用户提交 form 成功后,桌面 app:

1. `s.deleteAnchorForm(anchor)` — DELETE `anchor_askform` element(飞书 client 侧 form 消失)
2. 等 claude 处理完 tool_use,发出 Stop hook(assistant 生成 tool_result 后的最终 turn)
3. Stop hook 走 `dispatcher.DispatchTurn(sid, TurnAssistantFinal(Text=..., FormQuestions=nil))`
4. `onAskForm(sid, nil)` 触发 `relay_host.updateAnchorAskForm(sid, nil)` → `deleteAnchorForm`(幂等)+ 按 mounted 标记独立 CREATE 回 input 和 buttons

如果用户不提交、claude 也 moved on,Stop hook 依然触发第 4 步,拆 form 并恢复。

## 5. Hook payload 与 tool_use 桥接

### 5.1 claude-code hook 契约

atterm 桌面在 pty 环境注入两个变量:

- `ATTERM_SESSION_ID` — session id,让 hook payload 认得回哪个 session
- `ATTERM_HOOK_ENDPOINT` — `http://127.0.0.1:<mini-relay port>/hook`,`atterm-hook` CLI POST 到这里

atterm 拦截的 hook 事件类型:

| 事件 | Matcher | 用途 |
|---|---|---|
| `PreToolUse` | `AskUserQuestion` | 挂 anchor form + 存 questions |
| `PreToolUse` | (其他 tool) | body 里 "🔧 Tool: X" 标记(可选流式提示) |
| `PostToolUse` | 所有 | body 里显示 tool 结果尾 |
| `Notification` | `permission_prompt` / `idle_prompt` / `elicitation` | body 添加 "⏸ 等待输入" preamble + 触发 WaitingInput card action(anchor 已 live 时抑制,避免重复) |
| `Stop` | — | 最终 assistant text 覆盖 body + 拆 form(如挂) |
| `UserPromptSubmit` | — | 检测 transcript_path 变化,若换会话则 reset AIRoller |

**AskUserQuestion 特殊:不触发 Notification hook**。必须 hook `PreToolUse(matcher=AskUserQuestion)` 才能拿到 question + options 结构。这是 memory `project_claude_code_askquestion_no_notification.md` 记录的经验。

### 5.2 Extract questions

`desktop/feishu/hook_adapter.go::extractAllAskUserQuestions` 从 `tool_input.questions[]` 抽出:

```go
type AskUserQuestionEntry struct {
    Question    string
    MultiSelect bool
    Options     []QuestionOption
}
```

- `MultiSelect` 来自 tool_input 里 `multiSelect: true`(用户在 claude-code prompt 里显式标注多选题)
- `Options` 里的 `Label` 就是飞书 form 下拉里显示的字符串,回收 formValue 时按 label 反查 index(1-based)

## 6. 已知限制与 workaround

### 6.1 claude Type-something 稳定丢最后一字符

**症状**:用户在飞书 form 的 custom text 里填 "项目管理",claude 收到 "项目管"(缺 "理")。用户手动键盘输入也丢 —— 是 claude TUI 层面的 bug,不是 stroke 序列问题。

**Workaround**(已内置):`buildQuestionStrokes` 里给每段 custom text 尾部追加一个空格作为**牺牲字符**(`text := sl.txt + " "`)。claude 丢掉的就是空格,用户原文完整。UTF-8 rune 或 ASCII 都一样管用。

**不要拆掉**这个空格 workaround。曾试过:

- 增大 stroke delay(80ms → 350ms → 700ms,全丢)
- 每题最后 rune 前塞 Right-arrow pump(不解决问题)
- 用 Ctrl+Return via LF 0x0a 绕开 blur race(ink 不识别 0x0a 为 Ctrl+Return,当普通 Enter 处理)

都不通,详见 memory `feedback_askform_key_model.md`。这是 upstream 的锅,workaround 是最优解。

### 6.2 飞书 form widget 状态缓存

**症状**:同一 anchor card 上连续触发 2 次 AskUserQuestion,第 2 次 form 出现时下拉和 txt 里加载了上一次 submission 的选择/文本 —— 让用户以为答案没换成功。

**根因**:飞书 CardKit v2 client 按 `element_id` 缓存 widget 状态,即使 DELETE + CREATE 用同一 `element_id` client 也不清缓存。

**Workaround**(已内置):`RenderAskQuestionForm(sessionID, questions, mountSeq int64)` 里每个 widget 的 `element_id` 都带 `_<mountSeq>` 后缀,`mountSeq` 来自当次挂 form 时 `PatchSeq` 分配的新值。每次挂 form widget id 都是全新的,client 认成新 widget,不加载缓存。

**红线**:不要把 `_<mountSeq>` 后缀去掉,也不要只旋转部分 widget id —— 部分同名部分不同名会让 form 上一半空一半有历史值。

### 6.3 AskUserQuestion permission grant 前置

**症状**:第 1 次远程回答 form 时,form 提交后 claude 没走到最后一题就 tool wedges。抓 log 看,stroke 序列头两键(`1\r`)被 permission dialog 消费。

**根因**:claude-code 首次触发 AskUserQuestion 时 TUI 会弹一个 permission dialog("Allow using AskUserQuestion?");用户没先在本地按 "Yes, and don't ask again",dialog 每次都吃前两个 stroke。

**Workaround**(用户操作,一次性):本地 claude-code 首次遇到 AskUserQuestion 权限对话框,选 "Yes, and don't ask again"。之后不再弹,stroke 序列头两键就能正常送到 TUI。

memory `feedback_askform_permission_grant.md` 有详细指引。

### 6.4 其他

- **飞书 API rate limit**:飞书对 `cardkit:card:write` 有 QPS 上限,body 流式 PATCH 频率过高会拿 `code=99991672`(rate limited)。事件驱动的 PATCH 靠 `internal/feishu.AIRoller` 合并 chunk,空闲时不发状态心跳。改动流式频率前测一下 rate limit。
- **飞书 element 20 字符限制**:`element_id` 最长 20 字符,只能字母数字下划线。widget id `askform_q<i>_sel_<mountSeq>` 里 `<mountSeq>` 数字大时可能超限 —— 但 PatchSeq 一般 <100 每张卡片,实际不会撞到。发生时会拿 `code=300301 elementID format error`,监控日志里出现应该考虑改用 hash 而非 seq。

## 7. 错误码与常见故障

| 飞书错误码 | 含义 | 常见触发 | 处理 |
|---|---|---|---|
| `300301` | `elementID format error` | element_id 超 20 字符或有非法字符 | 检查 mount seq 是否过大;widget id 生成规则可能要改 |
| `300313` | `not find elementID` | PATCH / DELETE 引用了不存在的 element | 通常是 anchor 状态和飞书 client 状态不一致(可能是 CREATE 失败了但 mounted 标记没回滚);先 grep `feishu-anchor-form: CREATE failed` 看是否有前置错误 |
| `300315` | `unknown property` | JSON 里带了飞书不识别的 property(比如 `select_static.can_input`) | 别再试 combobox 属性,飞书 select_static 没有 free-input 支持(memory 里有实测) |
| `300317` | `sequence compare failed` | 同一 card 的 seq 交叉,飞书拒了 op | 确认所有 op 在 SendMu 内分配 seq;不要在没锁的地方 `atomic.AddInt64(&anchor.PatchSeq, 1)` 后跨 goroutine 再发 |
| `230099` | `tag "action" not supported in schema 2.0` | v1 schema 的 action container 用在了 v2 卡片 | 用 column_set + button 替代 |
| `code=99991672` | rate limited | body PATCH 频率过高 | 加节流或减少心跳频率 |

**故障排查思路**:

1. 抓桌面 log:`grep -E "feishu-anchor|askform" desktop.log`
2. 看 form 挂 / 拆 / restore 是否都有配对的成功日志(`inserted` / `removed` / `input restored` / `buttons restored`)
3. 抓 `askform submit sid=... q=N strokes=M action=X` 那行,action=0 是 stroke 发出成功;非 0 说明 Router 拒了(权限 / session 已结束等)
4. 如果 claude 收到答案但不完整:trailing space workaround 是否被拆(§6.1)? mountSeq 是否每次不同(§6.2)?

## 8. 代码入口速查

见 [AGENTS.md 何时改哪里](../../AGENTS.md#何时改哪里)对应行:

| 场景 | 入口 |
|---|---|
| Anchor card 生命周期 | `desktop/relay_host.go` — `updateAnchorAskForm` / `deleteAnchorForm` / `swapAnchorButtons` |
| Takeover 中途开启懒回填(§3.4) | `desktop/relay_host.go` — `lazyAttachIfMissing` / `onTurnMissingChunker` / `OnRemoteTerminalToggle` |
| Stroke plan 逻辑 | `desktop/feishu/service.go` — `buildQuestionStrokes` / `handleAskFormSubmit` / `parseAskFormSlots` |
| Form 渲染 | `internal/feishu/anchor_card.go` — `RenderAskQuestionForm` / `NewInputElement` / `NewDefaultButtonsElement` |
| Card index 状态 | `internal/feishu/cardindex.go` — `CardAnchor` 结构 + `SendMu` / `PatchSeq` / mounted 标记 |
| Router / stroke dispatch | `internal/feishu/router.go` — `InjectKeystrokesBySession` |
| Hook 解析 | `desktop/feishu/hook_adapter.go` — `extractAllAskUserQuestions` / `ParseTurn` |
| Local / relay 分流 | `desktop/app.go`(mode router)+ `desktop/feishu/binding_store_local.go`(local)+ `internal/relay/admin_http.go`(relay `/admin/api/feishu`) |
| Long-conn subscriber | `desktop/feishu/longconn.go` — `newLarkRuntime` / `extractCardActionFields` |

Manual probe 脚本(需 `-tags manual_probe`,不入 CI):`desktop/feishu/manual_probe_test.go`。用来直接对着开发飞书 app 发真 API,验证 CardKit 组件语法。改 form 渲染 / 组件属性时最快的 sanity check。
