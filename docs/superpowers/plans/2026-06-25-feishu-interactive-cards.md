# Feishu Interactive Cards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把飞书卡片从只读通知升级为双向交互——快捷按钮/直接回复注入正在运行的 Agent PTY、Agent 提问渲染成选项按钮、卡片附 session 标识与输出摘要。

**Architecture:** 复用现有「键盘输入 → PTY」链路。两种部署模式的注入都归一为「构造 `proto.TypeIn` 帧 → session.SendInbound → agent writer pump → PTY」。ModeLocal 复用 `relayHost.SendLocalInbound`；ModeRelay 在 relay 的卡片回调处理里对 `registry.Get(sid).SendInbound`。AskQuestion 的选项数据本就在 hook payload，当前被丢弃，解析出来即可。

**Tech Stack:** Go（gamesh 无关，纯 internal/desktop/relay）；飞书 interactive card JSON；`internal/ringbuf` scrollback；`internal/proto` 帧编解码。

---

## File Structure

- `internal/feishu/card.go` — 卡片渲染。新增 inject 按钮、AskQuestion 卡片、上下文行、输出摘要渲染。**主战场**。
- `internal/feishu/card_test.go` — 卡片渲染快照测试（新建，当前无此文件）。
- `desktop/feishu/hook_adapter.go` — 解析 AskUserQuestion 的 options（当前丢弃）。
- `desktop/feishu/hook_adapter_test.go` — 补带 options 的解析用例。
- `desktop/feishu/dispatcher.go` — 事件结构体加字段（Title/Cwd/Options/FailureCount/OutputTail）。
- `desktop/feishu/service.go` — `handleCardAction` 实现 inject 分支；`SessionLookup` 接口加 `Inject`。
- `desktop/feishu/hook_server.go` — `SessionLookup` 接口定义处加 `Inject` 方法。
- `desktop/relay_host.go` — `relayHost` 实现 `Inject`（复用 `SendLocalInbound`）；调度点补字段。
- `internal/session/session.go` — 加 `TailOutput(n int) []byte` 暴露 scrollback。
- `internal/feishu/service.go` — relay 卡片回调处理 inject（ModeRelay 注入）。
- `internal/feishu/event.go` — card.action 解析出 inject 的 text 字段。

---

## Task 1: Session 暴露 scrollback 尾部（输出摘要数据源）

**Files:**
- Modify: `internal/session/session.go`
- Test: `internal/session/session_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/session/session_test.go` 末尾追加：

```go
func TestSession_TailOutput(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	s.PushOut(1, []byte("line one\n"))
	s.PushOut(2, []byte("line two\n"))
	got := string(s.TailOutput(5))
	if got != "two\n" {
		t.Fatalf("TailOutput(5) = %q, want %q", got, "two\n")
	}
	all := string(s.TailOutput(1000))
	if all != "line one\nline two\n" {
		t.Fatalf("TailOutput(1000) = %q, want full buffer", all)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/session/ -run TestSession_TailOutput -v`
Expected: FAIL — `s.TailOutput undefined`

- [ ] **Step 3: 实现 TailOutput**

在 `internal/session/session.go` 的 `func (s *Session) Info()` 附近（约 line 294）加：

```go
// TailOutput returns up to the last n bytes of scrollback. Used to attach a
// short output summary to Feishu cards. Returns nil when n <= 0 or empty.
func (s *Session) TailOutput(n int) []byte {
	return s.scroll.TailBytes(n)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/session/ -run TestSession_TailOutput -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "feat(session): expose TailOutput for Feishu card output summary"
```

---

## Task 2: 解析 AskUserQuestion 的 options（当前被丢弃）

**Files:**
- Modify: `desktop/feishu/hook_adapter.go`
- Test: `desktop/feishu/hook_adapter_test.go`

- [ ] **Step 1: 写失败测试**

在 `desktop/feishu/hook_adapter_test.go` 末尾追加（真实 AskUserQuestion payload 带 options）：

```go
func TestClaudeCodeAdapter_AskUserQuestionOptions(t *testing.T) {
	raw := json.RawMessage(`{
	  "matcher": {"type":"idle_prompt", "tool":"AskUserQuestion"},
	  "prompt_id": "p1",
	  "context": {"tool_input": {"questions": [
	    {"question": "Deploy now?", "header": "Deploy", "multiSelect": false,
	     "options": [
	       {"label": "Yes, deploy", "description": "ship it"},
	       {"label": "No, wait", "description": "hold"}
	     ]}
	  ]}}
	}`)
	ev, ok := (&claudeCodeAdapter{}).Parse(raw, "")
	if !ok {
		t.Fatal("expected emit")
	}
	if ev.QuestionText != "Deploy now?" {
		t.Fatalf("QuestionText = %q", ev.QuestionText)
	}
	if len(ev.Options) != 2 || ev.Options[0].Label != "Yes, deploy" || ev.Options[1].Label != "No, wait" {
		t.Fatalf("Options = %+v", ev.Options)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./desktop/feishu/ -run TestClaudeCodeAdapter_AskUserQuestionOptions -v`
Expected: FAIL — `ev.Options undefined`

- [ ] **Step 3: 给 WaitingInputEvent 加 Options + 解析**

在 `desktop/feishu/hook_adapter.go` 改 `WaitingInputEvent`：

```go
// QuestionOption is one selectable answer from an AskUserQuestion tool call.
type QuestionOption struct {
	Label       string
	Description string
}

type WaitingInputEvent struct {
	QuestionText string
	Options      []QuestionOption // populated only for AskUserQuestion idle_prompt
	DedupKey     string
}
```

把 `extractAskUserQuestion` 改成同时返回 question 文本和 options：

```go
func extractAskUserQuestion(ctx json.RawMessage) (string, []QuestionOption) {
	var p struct {
		ToolInput struct {
			Question  string `json:"question"`
			Questions []struct {
				Question string `json:"question"`
				Options  []struct {
					Label       string `json:"label"`
					Description string `json:"description"`
				} `json:"options"`
			} `json:"questions"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(ctx, &p); err == nil {
		// Newer AskUserQuestion schema: questions[].options[].
		if len(p.ToolInput.Questions) > 0 {
			q0 := p.ToolInput.Questions[0]
			opts := make([]QuestionOption, 0, len(q0.Options))
			for _, o := range q0.Options {
				opts = append(opts, QuestionOption{Label: o.Label, Description: o.Description})
			}
			if q0.Question != "" {
				return q0.Question, opts
			}
		}
		// Older flat schema: tool_input.question.
		if p.ToolInput.Question != "" {
			return p.ToolInput.Question, nil
		}
	}
	var fallback struct {
		ToolInput json.RawMessage `json:"tool_input"`
	}
	_ = json.Unmarshal(ctx, &fallback)
	return compactJSONOneLine(fallback.ToolInput), nil
}
```

更新 `Parse` 的 `idle_prompt` 分支与 `mkEvent` 来携带 options：

```go
	case "idle_prompt":
		if in.Matcher.Tool != "AskUserQuestion" {
			return WaitingInputEvent{}, false
		}
		q, opts := extractAskUserQuestion(in.Context)
		if q == "" {
			q = "Claude is waiting on a question."
		}
		ev := mkEvent(in, q)
		ev.Options = opts
		return ev, true
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./desktop/feishu/ -run TestClaudeCode -v`
Expected: PASS（含原有 `TestClaudeCodeAdapter_AskUserQuestionEmits`）

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/hook_adapter.go desktop/feishu/hook_adapter_test.go
git commit -m "feat(feishu): parse AskUserQuestion options from hook payload"
```

---

## Task 3: 卡片渲染——inject 按钮 + 重试 + 已发送回显

**Files:**
- Modify: `internal/feishu/card.go`
- Test: `internal/feishu/card_test.go`（新建）

- [ ] **Step 1: 写失败测试**

新建 `internal/feishu/card_test.go`：

```go
package feishu

import (
	"testing"

	"github.com/google/uuid"
)

func actions(c Card) []any {
	els := c.Card["elements"].([]any)
	last := els[len(els)-1].(map[string]any)
	return last["actions"].([]any)
}

func buttonText(b any) string {
	m := b.(map[string]any)
	return m["text"].(map[string]any)["content"].(string)
}

func TestCommandFinishedCard_FailureHasRetry(t *testing.T) {
	c := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: uuid.New(), ExitCode: 1, ElapsedMS: 2000, Label: "go test",
		LastCommand: "go test ./...",
	})
	var labels []string
	for _, b := range actions(c) {
		labels = append(labels, buttonText(b))
	}
	want := []string{"跳回打开 session", "重试", "确认"}
	if len(labels) != 3 || labels[0] != want[0] || labels[1] != want[1] || labels[2] != want[2] {
		t.Fatalf("buttons = %v, want %v", labels, want)
	}
	// 重试按钮注入上次命令 + 换行。
	retry := actions(c)[1].(map[string]any)["value"].(map[string]any)
	if retry["kind"] != "inject" || retry["text"] != "go test ./...\n" {
		t.Fatalf("retry value = %+v", retry)
	}
}

func TestCommandFinishedCard_SuccessNoRetry(t *testing.T) {
	c := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: uuid.New(), ExitCode: 0, ElapsedMS: 1000, Label: "ls",
	})
	if got := len(actions(c)); got != 2 {
		t.Fatalf("success buttons = %d, want 2 (跳回/确认)", got)
	}
}

func TestSealedCard_NoInjectButton(t *testing.T) {
	c := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: uuid.New(), SealedBody: []byte("x"),
	})
	for _, b := range actions(c) {
		if v, ok := b.(map[string]any)["value"].(map[string]any); ok {
			if v["kind"] == "inject" {
				t.Fatal("sealed card must not contain inject buttons")
			}
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/feishu/ -run 'TestCommandFinishedCard|TestSealedCard' -v`
Expected: FAIL — `CommandFinishedInput.LastCommand undefined` / 按钮数量不符

- [ ] **Step 3: 给 input 加 LastCommand，重构 actionRow 支持 inject 按钮**

在 `internal/feishu/card.go` 给 `CommandFinishedInput` 加字段：

```go
type CommandFinishedInput struct {
	SessionID   uuid.UUID
	ExitCode    int
	ElapsedMS   int
	Label       string
	LastCommand string // for the retry button; empty disables retry
	SealedBody  []byte
}
```

新增一个构造 inject 按钮的 helper（放在 `actionRow` 附近）：

```go
func injectButton(sessionID uuid.UUID, label, text string) map[string]any {
	return map[string]any{
		"tag":  "button",
		"text": map[string]any{"tag": "plain_text", "content": label},
		"type": "default",
		"value": map[string]any{
			"kind":       "inject",
			"session_id": sessionID.String(),
			"text":       text,
		},
	}
}

func jumpButton(sessionID uuid.UUID) map[string]any {
	return map[string]any{
		"tag":  "button",
		"text": map[string]any{"tag": "plain_text", "content": "跳回打开 session"},
		"type": "primary",
		"url":  deepLink(sessionID),
	}
}

func ackButton(sessionID uuid.UUID, event string) map[string]any {
	return map[string]any{
		"tag":  "button",
		"text": map[string]any{"tag": "plain_text", "content": "确认"},
		"type": "default",
		"value": map[string]any{
			"kind":       "ack",
			"session_id": sessionID.String(),
			"event":      event,
		},
	}
}
```

把现有 `actionRow` 改成只组装传入的按钮：

```go
func actionRowOf(buttons ...map[string]any) map[string]any {
	actions := make([]any, 0, len(buttons))
	for _, b := range buttons {
		actions = append(actions, b)
	}
	return map[string]any{"tag": "action", "actions": actions}
}
```

在 `RenderCommandFinishedCard` 非 sealed 分支末尾改用新按钮（sealed 分支保持只有 jump+ack，不加 inject）：

```go
	buttons := []map[string]any{jumpButton(in.SessionID)}
	if in.ExitCode != 0 && in.LastCommand != "" {
		buttons = append(buttons, injectButton(in.SessionID, "重试", in.LastCommand+"\n"))
	}
	buttons = append(buttons, ackButton(in.SessionID, "command_finished"))
```

把该分支 `elements` 里原来的 `actionRow(in.SessionID, "command_finished")` 替换为 `actionRowOf(buttons...)`。sealed 分支里 `actionRow(...)` 替换为 `actionRowOf(jumpButton(in.SessionID), ackButton(in.SessionID, "command_finished"))`。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/feishu/ -run 'TestCommandFinishedCard|TestSealedCard' -v`
Expected: PASS

- [ ] **Step 5: 删除旧 actionRow（若不再被引用）并跑全包测试**

把 `RenderWaitingInputCard` 里的 `actionRow(in.SessionID, "waiting_input")` 也替换为 `actionRowOf(jumpButton(in.SessionID), injectButton(in.SessionID, "继续", "\n"), ackButton(in.SessionID, "waiting_input"))`，然后删除旧 `actionRow` 函数。

Run: `go test ./internal/feishu/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/feishu/card.go internal/feishu/card_test.go
git commit -m "feat(feishu): inject/retry/continue buttons on command + waiting cards"
```

---

## Task 4: AskQuestion 卡片（blue，每个选项一个按钮）

**Files:**
- Modify: `internal/feishu/card.go`
- Modify: `desktop/feishu/dispatcher.go`（事件加 Options 字段 + 渲染分流）
- Test: `internal/feishu/card_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/feishu/card_test.go` 追加：

```go
func TestAskQuestionCard_OneButtonPerOption(t *testing.T) {
	c := RenderAskQuestionCard(AskQuestionInput{
		SessionID: uuid.New(),
		Question:  "Deploy now?",
		Options: []AskOption{
			{Label: "Yes", InjectText: "1\n"},
			{Label: "No", InjectText: "2\n"},
		},
	})
	if c.Card["header"].(map[string]any)["template"] != "blue" {
		t.Fatal("AskQuestion card must be blue")
	}
	var labels []string
	for _, b := range actions(c) {
		labels = append(labels, buttonText(b))
	}
	// 跳回 + Yes + No（自由回复用引导文本，不是按钮）。
	if len(labels) != 3 || labels[1] != "Yes" || labels[2] != "No" {
		t.Fatalf("labels = %v", labels)
	}
	yes := actions(c)[1].(map[string]any)["value"].(map[string]any)
	if yes["kind"] != "inject" || yes["text"] != "1\n" {
		t.Fatalf("yes value = %+v", yes)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/feishu/ -run TestAskQuestionCard -v`
Expected: FAIL — `RenderAskQuestionCard undefined`

- [ ] **Step 3: 实现 AskQuestion 卡片**

> **⚠️ R1 — 注入策略未定（见 spec §7）**：claude TUI 如何接受选项选择官方文档未明确。本任务把每个选项的 `InjectText` 作为**单点可调字段**由调用方（Task 5 dispatcher）填充，渲染层不假设格式。实测后只需改 dispatcher 的填充逻辑，无需动卡片渲染。

在 `internal/feishu/card.go` 加：

```go
type AskOption struct {
	Label       string
	Description string
	InjectText  string // bytes to write into the PTY when this option is tapped
}

type AskQuestionInput struct {
	SessionID uuid.UUID
	Question  string
	Options   []AskOption
}

func RenderAskQuestionCard(in AskQuestionInput) Card {
	elements := []any{
		map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": "**Agent 在向你提问：**\n" + in.Question},
		},
	}
	if len(in.Options) > 0 {
		var sb strings.Builder
		for i, o := range in.Options {
			if o.Description != "" {
				fmt.Fprintf(&sb, "%d. **%s** — %s\n", i+1, o.Label, o.Description)
			} else {
				fmt.Fprintf(&sb, "%d. **%s**\n", i+1, o.Label)
			}
		}
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": sb.String()},
		})
	}
	elements = append(elements, map[string]any{
		"tag":  "note",
		"elements": []any{map[string]any{"tag": "plain_text", "content": "或直接回复本消息以自由作答"}},
	})
	buttons := []map[string]any{jumpButton(in.SessionID)}
	for _, o := range in.Options {
		buttons = append(buttons, injectButton(in.SessionID, o.Label, o.InjectText))
	}
	elements = append(elements, actionRowOf(buttons...))
	return Card{
		MsgType: "interactive",
		Card: map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Agent 提问"},
				"template": "blue",
			},
			"elements": elements,
		},
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/feishu/ -run TestAskQuestionCard -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/card.go internal/feishu/card_test.go
git commit -m "feat(feishu): AskQuestion card with one button per option"
```

---

## Task 5: dispatcher 分流 + InjectText 填充（含 R1 实测点）

**Files:**
- Modify: `desktop/feishu/dispatcher.go`
- Test: `desktop/feishu/dispatcher_test.go`

- [ ] **Step 1: 写失败测试**

在 `desktop/feishu/dispatcher_test.go` 追加（断言带 options 走 AskQuestion 卡）：

```go
func TestDispatch_AskQuestionRendersOptionButtons(t *testing.T) {
	var sentCard internalfeishu.Card
	d := newTestDispatcher(t, func(c internalfeishu.Card) { sentCard = c })
	d.DispatchWaitingInput(context.Background(), WaitingInputDispatchEvent{
		SessionID:    uuid.New(),
		Source:       WaitingSourceHook,
		QuestionText: "Deploy?",
		Options: []QuestionOption{
			{Label: "Yes", Description: "go"},
			{Label: "No", Description: "stop"},
		},
	})
	if sentCard.Card["header"].(map[string]any)["template"] != "blue" {
		t.Fatalf("expected blue AskQuestion card, got %+v", sentCard.Card["header"])
	}
}
```

> 注：`newTestDispatcher` 是测试辅助——若 `dispatcher_test.go` 已有等价的卡片捕获辅助，复用之；否则按现有 IMClient 打桩模式构造，断言传给 `SendInteractiveToOpenID` 的卡片 body 反序列化后的 header.template。先读 `dispatcher_test.go` 现有辅助再决定。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./desktop/feishu/ -run TestDispatch_AskQuestion -v`
Expected: FAIL — `WaitingInputDispatchEvent.Options undefined`

- [ ] **Step 3: 事件加 Options + 渲染分流 + InjectText 填充**

在 `desktop/feishu/dispatcher.go` 给 `WaitingInputDispatchEvent` 加：

```go
type WaitingInputDispatchEvent struct {
	SessionID      uuid.UUID
	IdleForSeconds int
	Source         WaitingSource
	QuestionText   string
	Options        []QuestionOption // non-empty → render AskQuestion card
	DedupKey       string
}
```

在 `DispatchWaitingInput` 渲染卡片处分流：有 Options 走 `RenderAskQuestionCard`，否则走 `RenderWaitingInputCard`。填充 InjectText：

```go
// R1: claude TUI 选项选择的注入格式实测后在此调整。当前默认注入序号 + 回车。
func optionInjectText(i int, _ QuestionOption) string {
	return fmt.Sprintf("%d\n", i+1)
}
```

把 `[]QuestionOption` 映射成 `[]internalfeishu.AskOption`，每个 `InjectText: optionInjectText(i, o)`，再调 `internalfeishu.RenderAskQuestionCard(...)`。

> `QuestionOption` 已在 Task 2 定义于 `desktop/feishu/hook_adapter.go`，同包可直接引用。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./desktop/feishu/ -run TestDispatch -v`
Expected: PASS

- [ ] **Step 5: ⚠️ R1 实测——确定真实注入格式**

手动验证（无法纯单测）：起一个 claude session，让它调 `AskUserQuestion`，从飞书卡片点选项按钮，观察 PTY 是否正确选中。
- 若序号 `1\n` 不生效，试方向键序列：`optionInjectText` 改为返回 `strings.Repeat("\x1b[B", i) + "\r"`（从首项向下移 i 次 + 回车）。
- 记录实测结论到 `optionInjectText` 上方注释，并在 Step 6 提交信息里写明实测方式。

- [ ] **Step 6: Commit**

```bash
git add desktop/feishu/dispatcher.go desktop/feishu/dispatcher_test.go
git commit -m "feat(feishu): route AskUserQuestion to option-button card (inject fmt verified)"
```

---

## Task 6: ModeLocal 注入——handleCardAction 接通 PTY

**Files:**
- Modify: `desktop/feishu/hook_server.go`（SessionLookup 接口加 Inject）
- Modify: `desktop/feishu/service.go`（handleCardAction inject 分支）
- Modify: `desktop/relay_host.go`（relayHost.Inject 实现）
- Test: `desktop/feishu/service_test.go`

- [ ] **Step 1: 写失败测试**

在 `desktop/feishu/service_test.go` 追加（注入器打桩，断言写入内容）：

```go
type fakeInjector struct {
	gotSID  uuid.UUID
	gotText string
	err     error
}

func (f *fakeInjector) Exists(uuid.UUID) bool { return true }
func (f *fakeInjector) Inject(sid uuid.UUID, text string) error {
	f.gotSID, f.gotText = sid, text
	return f.err
}

func TestHandleCardAction_InjectWritesText(t *testing.T) {
	inj := &fakeInjector{}
	s := newTestService(t, inj) // 见 Step 3 注
	sid := uuid.New()
	s.handleCardAction(context.Background(), sid.String(), "inject", "", "1\n")
	if inj.gotSID != sid || inj.gotText != "1\n" {
		t.Fatalf("inject got sid=%s text=%q", inj.gotSID, inj.gotText)
	}
}
```

> `newTestService` 注：读 `service_test.go` 现有 service 构造辅助，传入 `Sessions: inj`。若无辅助，直接 `&Service{cfg: ServiceConfig{Sessions: inj}}` 构造最小实例（handleCardAction 只用到 s.cfg.Sessions）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./desktop/feishu/ -run TestHandleCardAction_Inject -v`
Expected: FAIL — `handleCardAction` 签名不匹配 / `SessionLookup` 无 Inject

- [ ] **Step 3: SessionLookup 加 Inject 方法**

在 `desktop/feishu/hook_server.go` 的接口定义：

```go
type SessionLookup interface {
	Exists(sid uuid.UUID) bool
	// Inject writes text into the session's PTY as if typed by the user.
	Inject(sid uuid.UUID, text string) error
}
```

`noOpSessionLookup`（`service.go:284`）补：

```go
func (noOpSessionLookup) Inject(uuid.UUID, string) error { return nil }
```

`Service.Exists` 旁补（`service.go:127` 附近）——若 Service 自身被当 SessionLookup 用：

```go
func (s *Service) Inject(uuid.UUID, string) error { return nil }
```

- [ ] **Step 4: handleCardAction 实现 inject 分支**

改 `desktop/feishu/service.go` 的 `OnCardAction` 回调签名传入 text，并实现 `handleCardAction`。先看 `longconn.go` 的 `extractCardActionFields` 是否取了 text——若没取，加解析 `v["text"].(string)`，并把 `OnCardAction` 签名加一个 `text string` 参数（`longconn.go:181` 的 `extractCardActionFields` 返回值 + `service.go:154` 回调）。

```go
func (s *Service) handleCardAction(ctx context.Context, sessionID, kind, event, text string) {
	if kind != "inject" || text == "" {
		return
	}
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return
	}
	if err := s.cfg.Sessions.Inject(sid, text); err != nil {
		log.Printf("feishu: card inject session=%s: %v", sid, err)
	}
}
```

- [ ] **Step 5: relayHost.Inject 实现（复用 SendLocalInbound）**

在 `desktop/relay_host.go` 加（`SendLocalInbound` 附近，约 line 321 后）：

```go
// Inject writes text into a local session's PTY by sending it as a TypeIn
// frame down the same path remote-viewer keystrokes use.
func (h *relayHost) Inject(id uuid.UUID, text string) error {
	return h.SendLocalInbound(id, proto.Frame{
		Type:      proto.TypeIn,
		SessionID: id,
		Payload:   []byte(text),
	})
}
```

- [ ] **Step 6: 运行测试 + 全包编译**

Run: `go test ./desktop/feishu/ -run TestHandleCardAction_Inject -v && go build ./...`
Expected: PASS + build 成功

- [ ] **Step 7: Commit**

```bash
git add desktop/feishu/hook_server.go desktop/feishu/service.go desktop/relay_host.go desktop/feishu/longconn.go desktop/feishu/service_test.go
git commit -m "feat(feishu): ModeLocal card-button inject into session PTY"
```

---

## Task 7: ModeRelay 注入——relay 卡片回调写 TypeIn 帧

**Files:**
- Modify: `internal/feishu/event.go`（解析 card.action 的 text）
- Modify: `internal/feishu/service.go`（HandleEvent inject 分支）
- Test: `internal/feishu/service_test.go`

- [ ] **Step 1: 写失败测试**

先读 `internal/feishu/service.go` 的 `HandleEvent` 与 `HandleResult`、`internal/feishu/event.go` 的 `CardAction` 结构。在 `internal/feishu/service_test.go` 追加：断言收到 `kind:"inject"` 的 card.action 时，`HandleResult` 带出 `Inject{SessionID, Text}`（供 relay HTTP 层调用 registry）。

```go
func TestHandleEvent_CardInjectSurfacesInjection(t *testing.T) {
	// 构造一个 kind:"inject" 的 card.action.trigger 事件 body（复用本文件已有
	// 的 ack card.action 测试夹具，把 kind 改 inject 并加 text 字段）。
	// 断言 res.Inject != nil && res.Inject.Text == "1\n"。
}
```

> 实现者注：照搬本文件现有 ack card.action 测试的事件构造，仅改 `value.kind` 与加 `value.text`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/feishu/ -run TestHandleEvent_CardInject -v`
Expected: FAIL — `HandleResult.Inject undefined`

- [ ] **Step 3: CardAction 解析 text + HandleResult 带出 Injection**

`internal/feishu/event.go` 的 `card.action.trigger` 解析（约 line 153）给 CardAction 加 `Text string` 字段并解析 `value.text`。

`internal/feishu/service.go` 给 `HandleResult` 加：

```go
type Injection struct {
	SessionID string
	Text      string
}
// HandleResult 加字段：
//   Inject *Injection // non-nil → relay should write text into the session
```

`HandleEvent` 的 `card.action.trigger` 分支（约 line 159）：

```go
	case "card.action.trigger":
		if env.CardAction == nil {
			return &HandleResult{Reason: "ignored_card_action"}, nil
		}
		switch env.CardAction.Kind {
		case "ack":
			ack := RenderAckUpdateCard(AckUpdateInput{Event: env.CardAction.Event, SessionID: env.CardAction.SessionID})
			return &HandleResult{CardUpdate: &ack}, nil
		case "inject":
			ack := RenderAckUpdateCard(AckUpdateInput{Event: "inject", SessionID: env.CardAction.SessionID})
			return &HandleResult{
				CardUpdate: &ack,
				Inject:     &Injection{SessionID: env.CardAction.SessionID, Text: env.CardAction.Text},
			}, nil
		default:
			return &HandleResult{Reason: "ignored_card_action"}, nil
		}
```

- [ ] **Step 4: relay HTTP 层消费 Injection → registry.SendInbound**

在 `internal/relay/feishu_http.go` 的 `ServeHTTPEvents`（`resp.CardUpdate != nil` 处理处，约 line 77）之前，加对 `resp.Inject` 的处理：

```go
	if resp.Inject != nil {
		if sid, err := uuid.Parse(resp.Inject.SessionID); err == nil {
			if sess, ok := h.registry.Get(sid); ok {
				_ = sess.SendInbound(proto.Frame{
					Type:      proto.TypeIn,
					SessionID: sid,
					Payload:   []byte(resp.Inject.Text),
				})
			}
		}
		// 仍要回 CardUpdate（已发送回显），落到下方分支。
	}
```

> 实现者注（已核实）：`FeishuHTTPHandler`（`internal/relay/feishu_http.go:19`）当前只有 `store`+`svc`，**没有 registry**。`internal/feishu` 是纯渲染/事件包，**不要**让它依赖 `session.Registry`。正确做法：给 `FeishuHTTPHandler` 加 `registry *session.Registry` 字段，在 `NewFeishuHTTPHandler` 签名加该参数，并在 relay server 两处构造点（`server.go:203` 和 `server.go:434`）传入 `s.registry`。注入逻辑留在 HTTP 层（如上 Step 4 代码），`HandleEvent` 只负责把 `Inject` 经 `HandleResult` 带出来。这是本任务唯一的接线扩展。

- [ ] **Step 5: 运行测试 + 全包编译**

Run: `go test ./internal/feishu/ ./internal/relay/ -run 'Inject|Feishu' -v && go build ./...`
Expected: PASS + build 成功

- [ ] **Step 6: Commit**

```bash
git add internal/feishu/event.go internal/feishu/service.go internal/feishu/service_test.go internal/relay/feishu_http.go
git commit -m "feat(feishu): ModeRelay card-button inject via TypeIn frame to session"
```

---

## Task 8: 卡片信息丰富——上下文行 + 失败计数 + 输出摘要

**Files:**
- Modify: `internal/feishu/card.go`（渲染上下文行 + 摘要）
- Modify: `desktop/feishu/dispatcher.go`（事件加字段 + 失败计数）
- Modify: `desktop/relay_host.go`（调度点补 Title/Cwd/OutputTail）
- Test: `internal/feishu/card_test.go`, `desktop/feishu/dispatcher_test.go`

- [ ] **Step 1: 写失败测试（卡片渲染上下文行 + 摘要）**

在 `internal/feishu/card_test.go` 追加：

```go
func TestCommandFinishedCard_ContextAndSummary(t *testing.T) {
	c := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: uuid.New(), ExitCode: 1, ElapsedMS: 2000, Label: "go test",
		SessionTitle: "atterm", Cwd: "~/atterm", FailureCount: 3,
		OutputTail: "FAIL: foo_test.go:12\n",
	})
	blob := mustJSON(t, c)
	for _, want := range []string{"atterm", "~/atterm", "连续第 3 次", "FAIL: foo_test.go:12"} {
		if !strings.Contains(blob, want) {
			t.Fatalf("card missing %q in %s", want, blob)
		}
	}
}
```

> `mustJSON` 用 `encoding/json` Marshal 卡片为 string 的小辅助；若 card_test.go 已有等价物则复用。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/feishu/ -run TestCommandFinishedCard_Context -v`
Expected: FAIL — 字段 undefined

- [ ] **Step 3: CommandFinishedInput 加上下文字段 + 渲染**

`internal/feishu/card.go` 给 `CommandFinishedInput` 加 `SessionTitle string`、`Cwd string`、`FailureCount int`、`OutputTail string`。在 `RenderCommandFinishedCard` 非 sealed 分支：
- 在主 div 前插一个上下文行 div：``fmt.Sprintf("`%s` · %s", in.SessionTitle, in.Cwd)``（两者为空则跳过）。
- 失败且 `FailureCount > 1` 时在退出码行后追加 `· 连续第 N 次失败`。
- `OutputTail != ""` 时加一个代码块 div（截断复用 `truncateQuestion`）。sealed 分支不加任何上下文/摘要。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/feishu/ -run TestCommandFinishedCard_Context -v`
Expected: PASS

- [ ] **Step 5: dispatcher 失败计数 + 事件加字段**

`desktop/feishu/dispatcher.go` 给 `CommandFinishedEvent` 加 `SessionTitle/Cwd/OutputTail string`。在 dispatcher 内加 per-session 失败计数 map（`muD` 保护）：退出码 ≠0 时 `failCount[sid]++`，==0 时 `delete`。把计数透传到 `CommandFinishedInput.FailureCount`，其余字段透传。

- [ ] **Step 6: relay_host 调度点补字段**

`desktop/relay_host.go` 约 line 520 的 `DispatchCommandFinished` 调用处：从 `sess.Info()` 取 `Title`/`Cwd`，非 sealed 时 `sess.TailOutput(512)` 取摘要（sealed 即 `meta.SealedBody` 非空时传空），填进 `CommandFinishedEvent`。

```go
info := sess.Info() // sess 来自 Registry().Get(sid)，该处已有
var tail string
if len(meta.SealedBody) == 0 {
	tail = string(sess.TailOutput(512))
}
go disp.DispatchCommandFinished(context.Background(), feishu.CommandFinishedEvent{
	SessionID: sid, ExitCode: meta.ExitCode, ElapsedMS: meta.ElapsedMS,
	Label: meta.Label, SealedBody: meta.SealedBody,
	SessionTitle: info.Title, Cwd: info.Cwd, OutputTail: tail,
})
```

> 注：确认该调度点能拿到 `sess`（`Registry().Get(sid)`）。若当前闭包只有 `sid` 无 `sess`，在闭包内先 `sess, ok := h.server.Registry().Get(sid)` 取一次。

- [ ] **Step 7: 运行测试 + 全包测试 + 编译**

Run: `go test ./internal/feishu/ ./desktop/feishu/ -v && go build ./...`
Expected: PASS + build 成功

- [ ] **Step 8: Commit**

```bash
git add internal/feishu/card.go desktop/feishu/dispatcher.go desktop/relay_host.go internal/feishu/card_test.go desktop/feishu/dispatcher_test.go
git commit -m "feat(feishu): enrich cards with session id, failure count, output summary"
```

---

## Task 9: 直接回复消息注入（message_id → session_id 映射）

**Files:**
- Modify: `desktop/feishu/dispatcher.go`（发卡后记录 message_id→sid）
- Modify: `desktop/feishu/service.go`（handleBindMessage 旁加 reply 注入）
- Modify: `desktop/feishu/longconn.go`（IM 消息事件取 parent_id）
- Test: `desktop/feishu/service_test.go`

- [ ] **Step 1: 写失败测试**

断言：记录映射 `(message_id→sid)` 后，收到一条 `parent_id == message_id` 的 IM 文本回复 → 调用 `Inject(sid, text+"\n")`。

```go
func TestReplyMessageInjects(t *testing.T) {
	inj := &fakeInjector{}
	s := newTestService(t, inj)
	sid := uuid.New()
	s.rememberCardMessage("msg-1", sid)
	s.handleReplyMessage(context.Background(), "op", "msg-1", "looks good")
	if inj.gotSID != sid || inj.gotText != "looks good\n" {
		t.Fatalf("got sid=%s text=%q", inj.gotSID, inj.gotText)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./desktop/feishu/ -run TestReplyMessageInjects -v`
Expected: FAIL — `rememberCardMessage`/`handleReplyMessage` undefined

- [ ] **Step 3: Service 加 message 映射 + reply 处理**

在 `desktop/feishu/service.go` 加（带过期清理——简单上限 + FIFO 即可，避免无界增长）：

```go
type cardMsgMap struct {
	mu sync.Mutex
	m  map[string]uuid.UUID
	order []string
}

func (c *cardMsgMap) remember(msgID string, sid uuid.UUID) {
	c.mu.Lock(); defer c.mu.Unlock()
	if c.m == nil { c.m = map[string]uuid.UUID{} }
	if _, ok := c.m[msgID]; !ok {
		c.order = append(c.order, msgID)
		if len(c.order) > 512 { // bound
			delete(c.m, c.order[0]); c.order = c.order[1:]
		}
	}
	c.m[msgID] = sid
}
func (c *cardMsgMap) lookup(msgID string) (uuid.UUID, bool) {
	c.mu.Lock(); defer c.mu.Unlock()
	sid, ok := c.m[msgID]; return sid, ok
}
```

`Service` 加字段 `cardMsgs cardMsgMap`，并加 `rememberCardMessage` / `handleReplyMessage`：

```go
func (s *Service) rememberCardMessage(msgID string, sid uuid.UUID) { s.cardMsgs.remember(msgID, sid) }

func (s *Service) handleReplyMessage(ctx context.Context, _openID, parentID, text string) {
	if parentID == "" || strings.TrimSpace(text) == "" {
		return
	}
	sid, ok := s.cardMsgs.lookup(parentID)
	if !ok {
		return
	}
	_ = s.cfg.Sessions.Inject(sid, text+"\n")
}
```

- [ ] **Step 4: longconn 取 parent_id + 接线**

`desktop/feishu/longconn.go` 的 `OnP2MessageReceiveV1`（约 line 205）取 `ev.Event.Message.ParentId`，把 `OnBindMessage` 回调扩展为也传 parentID + messageID，或新增 `OnReplyMessage(ctx, openID, parentID, text)` 回调。`service.go` 把 `/bind ` 之外、带 parent_id 的消息路由到 `handleReplyMessage`。dispatcher 发卡成功后拿到返回的 message_id 调 `rememberCardMessage`（需 IMClient.Send 返回 message_id——看 `internal/feishu/client.go` 的 Send 是否回 message_id，若无则扩展返回值）。

> 实现者注（已核实）：`SendInteractiveToOpenID`（`internal/feishu/client.go:32`）当前**只返回 `error`**，且 `postIM` 丢弃了响应 body。需改造：`postIM` 解析飞书响应 `{ "data": { "message_id": "..." } }`，`SendInteractiveToOpenID` 签名改为 `(messageID string, err error)`。dispatcher 发卡成功后用返回的 messageID 调 `rememberCardMessage(messageID, sid)`。注意 `SendTextToOpenID`（bind reply 用）若也走同一改造后的 `postIM`，让它忽略返回的 messageID 即可（保持其签名或同步改）。这是本任务的关键接线点。

- [ ] **Step 5: 运行测试 + 编译**

Run: `go test ./desktop/feishu/ -run TestReplyMessage -v && go build ./...`
Expected: PASS + build 成功

- [ ] **Step 6: Commit**

```bash
git add desktop/feishu/service.go desktop/feishu/longconn.go desktop/feishu/dispatcher.go internal/feishu/client.go desktop/feishu/service_test.go
git commit -m "feat(feishu): inject free-text reply to a card back into its session"
```

---

## Task 10: test-send 覆盖新卡片 + 全量回归

**Files:**
- Modify: `desktop/feishu/test_send.go`（新增 AskQuestion + 重试场景）
- Test: `desktop/feishu/test_send_test.go`

- [ ] **Step 1: 给 test-send 加 AskQuestion 场景**

`desktop/feishu/test_send.go` 仿现有 `TestCard*` 常量，加一个 AskQuestion 测试卡（带 2 个选项）与一个失败带「重试」的命令完成卡，确保新卡片能被手动 test-send 验证。补对应 `test_send_test.go` 断言。

- [ ] **Step 2: 运行 feishu 相关全量测试**

Run: `go test ./internal/feishu/ ./desktop/feishu/ ./internal/relay/ -v`
Expected: 全 PASS

- [ ] **Step 3: 全仓编译 + vet**

Run: `go build ./... && go vet ./internal/feishu/ ./desktop/feishu/`
Expected: 无错误

- [ ] **Step 4: Commit**

```bash
git add desktop/feishu/test_send.go desktop/feishu/test_send_test.go
git commit -m "test(feishu): test-send coverage for AskQuestion + retry cards"
```

---

## Self-Review Notes

- **Spec §4.1/4.2 注入链路** → Task 6（Local）+ Task 7（Relay），两者都归一到 TypeIn 帧。
- **Spec §4.3 AskQuestion 数据源** → Task 2（解析 options）+ Task 4（渲染）+ Task 5（分流/注入格式）。
- **Spec §5 卡片总览** → Task 3（inject/retry/continue 按钮）+ Task 4（AskQuestion blue 卡）。
- **Spec §6 信息丰富** → Task 8（上下文行/失败计数/输出摘要）。
- **Spec §7 R1（选项注入策略）** → Task 5 Step 5 显式实测点，注入格式隔离在 `optionInjectText` 单点。
- **Spec §7 R2（输出缓冲）** → 已解决：`Session.scroll *ringbuf.Buffer` + 新增 `TailOutput`（Task 1）。
- **Spec §7 R3（message 映射）** → Task 9，带 512 上限的有界 map。
- **Spec §7 R4（按钮幂等）** → ack 回显禁用按钮 + 飞书回调天然单投递；如需强幂等可在 Task 6/7 加 (msgID,button) 去重，本期按 YAGNI 暂不加，留作后续。
- **类型一致性**：`QuestionOption`（desktop/feishu，Task 2 定义）vs `AskOption`（internal/feishu，Task 4 定义）是两层的不同类型，Task 5 显式做映射，非笔误。`Inject(sid, text)` 签名在 Task 6 接口/实现/桩三处一致。
- **直接回复消息**依赖 `SendInteractiveToOpenID` 返回 message_id（Task 9 Step 4 接线点），是全计划最大的单点扩展，执行时优先确认。
