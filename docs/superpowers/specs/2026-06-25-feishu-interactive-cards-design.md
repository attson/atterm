# Feishu 交互卡片升级（双向交互 + 信息丰富 + AskQuestion 卡片）— design

Date: 2026-06-25
Status: Drafted — awaiting user review before plan.

## 0. Summary

把飞书集成从「只读通知」升级为「双向交互」。当前卡片（见
`internal/feishu/card.go`）只有两个按钮——「跳回打开 session」（深链）和
「确认」（仅把卡片置灰，**不向 session 回传任何东西**）。本设计在不引入新
传输协议的前提下，新增三类能力：

1. **飞书内回复/操作即注入 PTY**——用户在飞书点快捷按钮或直接回复消息，
   等价于在终端里敲入对应文本 + 回车，送回正在运行的 Agent session。
2. **卡片信息更丰富**——每张卡片加 session 标识/项目、命令输出摘要（非
   E2EE 会话）、相对时间与连续失败次数。
3. **新增 Agent 提问（AskQuestion）卡片**——当 Agent 通过
   `AskUserQuestion` 工具提问时，把每个选项 `label` 渲染成一个快捷按钮，
   点击即把该选项注入 PTY。这是对现有「等待输入」橙色卡片的**场景升级**，
   不是并存的第四种来源。

本设计是 `2026-06-17-feishu-hook-question-design.md`（飞书 hook + desktop
直发 + 双模式 dispatcher 的奠基设计）的演进。它复用该设计建立的全部基础
设施（HookAdapter、dispatcher、ModeLocal/ModeRelay 双模式），不重述其内容。

## 1. Goals

- 飞书快捷按钮 + 直接回复消息 → 注入正在运行的 Agent session 的 PTY。
- **两种部署模式都支持**注入（ModeLocal 与 ModeRelay）。
- 新增 AskQuestion 卡片：解析 `AskUserQuestion` 的 options，每个选项一个按钮。
- 卡片加上下文行：session 标识/项目、相对时间、失败计数。
- 命令完成卡片附输出摘要（仅非 E2EE 会话）。
- 不引入新传输协议——ModeRelay 复用现有 `proto.TypeIn` 远程键盘链路。

## 2. Non-goals

- 卡片内嵌输入框（飞书 card 2.0 `input` 组件）——本期不做，自由文本走
  「直接回复消息」通道即可（用户已明确跳过 input 组件）。
- sealed（E2EE「仅本机可见」）卡片的注入——sealed 卡片维持现状（只有
  「跳回 / 确认」），敏感操作仍只在本机进行，符合 E2EE 安全直觉。
- Mobile / web 客户端发起飞书消息——它们只作为 session viewer（沿用奠基设计）。

## 3. 背景：两条已有的入站通道 + 两种模式的关键差异

飞书 → desktop 已有两条入站通道（`desktop/feishu/longconn.go`）：

- **IM 文本消息** `OnP2MessageReceiveV1` → `OnBindMessage`（当前仅用于 `/bind`）。
- **卡片按钮动作** `card.action.trigger` → `OnCardAction`（当前 `handleCardAction`
  是**空实现**，`desktop/feishu/service.go:202` 所有参数被 `_ =` 丢弃）。

飞书卡片本身**始终本机直发飞书开放平台**（`internal/feishu/client.go:64`
写死 `open-apis/im/v1/messages`），不经过 relay。两种模式只在「token / binding
从哪来」「卡片回调到哪」上不同：

| | **ModeLocal** | **ModeRelay** |
|---|---|---|
| 卡片回调入口 | 本机长连接 `OnCardAction` | relay HTTP `ServeHTTPEvents`（`internal/relay/feishu_http.go:47`）|
| 当前回调行为 | 空实现 | relay `HandleEvent` 就地返回 ack 卡片，**不通知本机** |
| 注入 PTY 可行性 | 直接可做 | 需新建 relay→本机下行（见 §4.2） |

## 4. 设计：三条链路

核心洞察：飞书的「回复 / 选项」本质就是**一段键盘输入**。系统已有成熟的
「键盘输入 → PTY」链路（远程 viewer 用），两种模式都复用它。

### 4.1 注入链路 · ModeLocal

实现现在空着的 `handleCardAction`（`desktop/feishu/service.go:202`）：

- 解析卡片 `value`：`{kind:"inject", session_id, text}`。
- `sessions`（即 `a.host`，见 `desktop/app.go:2058`）按 session_id 取 PTY，
  `pty.Write([]byte(text))`。范本：`desktop/relay_host.go:556` 注入 resume 命令。
- 「直接回复消息」：发卡片时记录 `message_id → session_id`，IM 回复事件用
  `parent_id` 反查 → 同样 `pty.Write`。

### 4.2 注入链路 · ModeRelay（复用 TypeIn，无新协议）

relay 已有「client → relay → agent」的 IN 帧下行：远程 viewer 的键盘输入经
`sess.SendInbound(f)`（`internal/relay/client_conn.go:186`）进 session 的
`Inbound()` 队列 → agent writer pump（`agent_conn.go:87`）写 `proto.TypeIn`
帧 → 本机 agent 收帧写 PTY。飞书回调复用这同一条路：

- relay `HandleEvent`（`internal/feishu/service.go`）处理 `card.action.trigger`
  时，除返回 ack 卡片外，新增：用 `value.session_id` → `registry.Get(sid)`
  （`internal/session/registry.go:120`）→ `sess.SendInbound(EncodeIn([]byte(text)))`
  （`internal/session/session.go:1567`，帧类型 `proto.TypeIn = 0x02`）。
- 本机 agent 侧无需改动——它本就把收到的 `TypeIn` 帧写进 PTY。
- 「直接回复消息」同理，relay 侧维护 `message_id → session_id` 映射。

### 4.3 AskQuestion 数据源（现成，当前被丢弃）

`AskUserQuestion` 的 hook payload 结构：
```json
{ "tool_input": { "questions": [
  { "question": "...", "header": "...", "multiSelect": false,
    "options": [ { "label": "...", "description": "..." } ] } ] } }
```
当前 `extractAskUserQuestion`（`desktop/feishu/hook_adapter.go:106`）**只取了
`tool_input.question` 一个字段，把 `questions[].options` 整个丢掉了**。本设计
扩展 `WaitingInputEvent` 携带结构化选项，让 dispatcher 能渲染选项按钮。

## 5. 卡片类型总览（升级后）

| 卡片 | 触发 | 模板色 | 内容 | 按钮行 |
|------|------|--------|------|--------|
| 命令完成·成功 | OSC 133 退出码 0 | green | 上下文行 + 输出摘要 | 跳回 / 确认 |
| 命令完成·失败 | 退出码 ≠0 | red | 上下文行 + 输出摘要 + 失败计数 | 跳回 / **重试** / 确认 |
| 仅本机可见 | sealed (E2EE) | grey | 维持现状 | 跳回 / 确认（不注入）|
| **Agent 提问** | hook `AskUserQuestion` | blue | 问题文本 + 选项描述 | 跳回 / **[每个选项一个按钮]** / 自由回复提示 |
| 等待输入（纯闲置）| 启发式 idle | orange | 上下文行 | 跳回 / **继续** / 确认 |

按钮 `value` 统一为 `{kind:"inject", session_id, text}`（ack 保持
`{kind:"ack", ...}` 兼容旧卡片）。点击注入后卡片回显「已发送：<label>」并禁用
按钮，复用 `RenderAckUpdateCard` 机制扩展（避免重复点击）。

**等待输入（纯闲置）**：纯启发式闲置没有明确选项，只给「继续」+「跳回」+
「确认」；复杂回复引导用户用「直接回复消息」。

## 6. 信息丰富：字段来源与接线

新字段需在 `desktop/relay_host.go` 的调度点（`DispatchCommandFinished` /
`DispatchWaitingInput` 调用处，约 `relay_host.go:520-535`）补充进
`CommandFinishedEvent` / `WaitingInputDispatchEvent`，再透传到 `card.go`：

| 字段 | 来源 | 约束 |
|------|------|------|
| session 标识 | `proto.SessionInfo.Title` + `.Cwd`（host 侧已有）| 全模式可得 |
| 相对时间 | dispatch 时间戳 | 全模式可得 |
| 连续失败计数 | 新增 per-session 失败计数器（dispatcher 内存）| 全模式可得 |
| 输出摘要（最后 3-5 行）| **见 R2 风险** | 仅非 sealed 会话 |

## 7. 风险与未决点（须在实现期处理，不靠猜）

- **R1（高）AskQuestion 选项注入策略**：claude TUI 如何接受选项选择，官方
  interactive-mode 文档**未明确**（只说 Left/Right 在 dialog tabs 间切换）。
  实现期必须**实测**确定注入内容：数字键序号？方向键序列 `\x1b[B` + 回车？
  文本 label 匹配?这是 AskQuestion 卡片的成败关键。注入策略应做成可调整的
  单点函数，便于实测后修正。
- **R2（中）输出摘要来源**：本机 host 是否保留 scrollback ring buffer 待确认。
  若无，则输出摘要要么新增 ring buffer（有成本），要么本期砍掉。实现第一步
  先确认 `internal/ptyhost` / `internal/session` 有无可读的近期输出缓冲。
- **R3（中）message_id→session_id 映射**：「直接回复消息」依赖发卡时记录映射。
  两种模式各需一份（ModeLocal 本机内存 / ModeRelay relay 内存），需考虑过期
  清理（卡片对应的 session 结束后清除）。
- **R4（低）按钮幂等**：注入后禁用按钮，但飞书可能重复投递回调；注入侧应对
  同一 (message_id, button) 去重，避免重复写 PTY。

## 8. 实现顺序建议（每步可独立验证）

1. **ModeLocal 注入地基**：实现 `handleCardAction` 的 inject 分支 +
   命令完成失败卡的「重试」按钮。最小闭环，验证「点按钮→PTY」。
2. **AskQuestion 卡片**：扩展 hook_adapter 解析 options → 扩展事件 → 新增
   blue 卡片渲染 + 选项按钮。**含 R1 实测**。
3. **ModeRelay 注入**：relay `HandleEvent` 接 `SendInbound(TypeIn)` 下行。
4. **直接回复消息**：两种模式的 message_id 映射 + parent_id 反查。
5. **信息丰富**：上下文行 + 失败计数 +（视 R2）输出摘要。

## 9. 测试策略

- `internal/feishu/card_test.go`：每种卡片的渲染快照（按钮数量/value/模板色），
  含 AskQuestion 的多选项渲染、sealed 卡不含 inject 按钮。
- `desktop/feishu/hook_adapter_test.go`：补 AskUserQuestion **带 options** 的
  真实 payload 解析用例（当前测试只有 `question` 无 `options`）。
- `desktop/feishu/service_test.go`：`handleCardAction` inject 分支调用 PTY 注入
  （注入器接口打桩，断言写入内容 = 选项 label/重试命令 + 换行）。
- relay 侧：`HandleEvent` 收到 inject 卡片回调 → 断言对应 session 收到 TypeIn 帧。
- AskQuestion 选项注入策略（R1）：实测后补一个固定 golden 注入字节序列的回归测试。
