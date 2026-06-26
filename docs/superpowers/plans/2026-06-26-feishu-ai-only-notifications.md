# Feishu AI-Only Notifications Toggle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 飞书通知新增「仅 AI 会话通知」开关（默认开启），开启时命令完成 + 等待输入通知只对 AI 会话发送。

**Architecture:** 把「该不该通知」抽成纯函数 `shouldNotifySession(type, aiOnly)`；偏好存 desktop config（`AINotificationsOnly *bool`，nil 默认 true），复用 `NotificationsEnabled` 的 config/app/prefssync 模板；过滤接在 `relay_host.go` 的两个 dispatch 分支；前端 SettingsFeishu.vue 加 toggle。

**Tech Stack:** Go（desktop 包）；Vue3 + TS（wailsjs 绑定 4 层）；测试 go test + vitest。

**环境：** 默认 `go` 1.19 不可用，**用 `/home/attson/sdk/go1.24.13/bin/go` 和 `gofmt`**。build 用 `go build -tags webkit2_41 -o /dev/null ./desktop/`。前端 `npm`（desktop/frontend/）。gofmt-on-save 钩子：Edit 被拒就重读再编辑。

---

## File Structure

- `desktop/relay_host.go` — `shouldNotifySession` 纯函数 + 两个 dispatch 分支接守卫。
- `desktop/relay_host_test.go` — shouldNotifySession 单测。
- `desktop/config.go` — `AINotificationsOnly *bool` + `AINotificationsOnlyOrDefault()`。
- `desktop/config_test.go`（或现有 config 测试文件）— OrDefault 测试。
- `desktop/app.go` — `GetAINotificationsOnly`/`SetAINotificationsOnly`。
- `desktop/prefssync_adapter.go` — 3 处 switch 加 `ai_notifications_only` case（ReadValue/WriteValue/isPrefCustomized）。
- `desktop/frontend/wailsjs/go/main/App.{d.ts,js}` — DownloadVersion 风格手加 GetAINotificationsOnly/SetAINotificationsOnly 绑定。
- `desktop/frontend/src/lib/api.ts` — wrapper（仿 getNotificationsEnabled）。
- `desktop/frontend/src/platform/{types.ts,wails.ts}` — platform 层（若 SettingsFeishu 从 platform 拿；否则直接从 api 拿，见 Task 5 探索）。
- `desktop/frontend/src/components/SettingsFeishu.vue` — toggle UI。
- `desktop/frontend/src/components/SettingsFeishu.test.ts` — toggle 测试。
- `desktop/frontend/src/i18n/messages/{en,zh-CN}.ts` — i18n。

---

## Task 1: shouldNotifySession 纯函数

**Files:**
- Modify: `desktop/relay_host.go`
- Test: `desktop/relay_host_test.go`

- [ ] **Step 1: 写失败测试**

在 `desktop/relay_host_test.go` 末尾追加（确认它 import 了 testing；若 SessionTypeAI 需要，import `internal/session`——但测试用字面量 "ai"/"shell" 更简单，不依赖常量）：

```go
func TestShouldNotifySession(t *testing.T) {
	cases := []struct {
		name        string
		sessionType string
		aiOnly      bool
		want        bool
	}{
		{"ai session, ai-only on", "ai", true, true},
		{"shell session, ai-only on", "shell", true, false},
		{"ai session, ai-only off", "ai", false, true},
		{"shell session, ai-only off", "shell", false, true},
		{"empty type, ai-only on", "", true, false},
	}
	for _, c := range cases {
		if got := shouldNotifySession(c.sessionType, c.aiOnly); got != c.want {
			t.Errorf("%s: shouldNotifySession(%q, %v) = %v, want %v",
				c.name, c.sessionType, c.aiOnly, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestShouldNotifySession -v`
Expected: FAIL — `shouldNotifySession` undefined

- [ ] **Step 3: 实现**

在 `desktop/relay_host.go` 加（放文件靠近其他 helper 处；确认它已 import `internal/session`——relay_host.go 已 import，用 `session.SessionTypeAI`）：

```go
// shouldNotifySession reports whether a Feishu notification should fire for a
// session of the given workload type, honoring the "AI sessions only"
// preference. aiOnly=false → always notify; aiOnly=true → only ai sessions.
func shouldNotifySession(sessionType string, aiOnly bool) bool {
	if !aiOnly {
		return true
	}
	return sessionType == session.SessionTypeAI
}
```

- [ ] **Step 4: 运行确认通过**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestShouldNotifySession -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/relay_host.go desktop/relay_host_test.go
git commit -m "feat(desktop): shouldNotifySession honors AI-only preference"
```

---

## Task 2: config AINotificationsOnly

**Files:**
- Modify: `desktop/config.go`
- Test: `desktop/config_test.go`（若不存在则在现有 config 测试文件，先 grep `AutoCheckUpdatesOrDefault` 找现有 config 测试位置）

- [ ] **Step 1: 写失败测试**

先 `grep -rn "AutoCheckUpdatesOrDefault\|NotificationsEnabledOrDefault" desktop/*_test.go` 找现有 config 测试文件，在那里追加（仿现有 OrDefault 测试）：

```go
func TestAINotificationsOnlyOrDefault(t *testing.T) {
	// nil → true (默认开启)
	if got := (appConfig{}).AINotificationsOnlyOrDefault(); !got {
		t.Errorf("nil AINotificationsOnly = %v, want true (default on)", got)
	}
	f := false
	if got := (appConfig{AINotificationsOnly: &f}).AINotificationsOnlyOrDefault(); got {
		t.Errorf("&false = %v, want false", got)
	}
	tr := true
	if got := (appConfig{AINotificationsOnly: &tr}).AINotificationsOnlyOrDefault(); !got {
		t.Errorf("&true = %v, want true", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestAINotificationsOnlyOrDefault -v`
Expected: FAIL — `AINotificationsOnly` / `AINotificationsOnlyOrDefault` undefined

- [ ] **Step 3: 实现**

在 `desktop/config.go` 的 `appConfig` struct 加字段（放在 `NotificationsEnabled` 附近，仿其注释风格）：

```go
	// AINotificationsOnly, when true, limits Feishu notifications to AI
	// sessions (shell sessions are skipped). Nil means "never set" and
	// defaults to true.
	AINotificationsOnly *bool `json:"ai_notifications_only,omitempty"`
```

加方法（仿 `NotificationsEnabledOrDefault`）：

```go
// AINotificationsOnlyOrDefault returns the user's preference, defaulting to
// true (only AI sessions notify) when unset.
func (c appConfig) AINotificationsOnlyOrDefault() bool {
	if c.AINotificationsOnly == nil {
		return true
	}
	return *c.AINotificationsOnly
}
```

- [ ] **Step 4: 运行确认通过**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestAINotificationsOnlyOrDefault -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/config.go desktop/*_test.go
git commit -m "feat(desktop): AINotificationsOnly config (defaults on)"
```

---

## Task 3: App 桥接 + prefssync 白名单

**Files:**
- Modify: `desktop/app.go`, `desktop/prefssync_adapter.go`
- Test: `desktop/app_*_test.go`（找现有 NotificationsEnabled 测试）

- [ ] **Step 1: 先读现有模板**

`cat desktop/app.go` 看 `GetNotificationsEnabled`（1606）/`SetNotificationsEnabled`（1614-1623）的完整写法（含 cfgStore.Get/Set + markPrefDirtyAndPush）。`cat desktop/prefssync_adapter.go` 看 `notifications_enabled` 在 ReadValue/WriteValue 的 case，`app.go` 的 `isPrefCustomized`（~1914）的 case。

- [ ] **Step 2: 写失败测试**

`grep -rn "SetNotificationsEnabled\|GetNotificationsEnabled" desktop/*_test.go` 找现有测试，仿照在那里加 Get/Set 往返测试：

```go
func TestAINotificationsOnly_GetSetRoundTrip(t *testing.T) {
	a := newTestApp(t) // 用现有测试构造 App 的辅助；以实际为准
	// 默认 true
	if !a.GetAINotificationsOnly() {
		t.Fatal("default GetAINotificationsOnly = false, want true")
	}
	if err := a.SetAINotificationsOnly(false); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if a.GetAINotificationsOnly() {
		t.Fatal("after Set(false), Get = true")
	}
}
```

> `newTestApp`/App 构造以现有 NotificationsEnabled 测试的方式为准，照搬。若现有测试不便构造 App，可只测 config 层（Task 2 已覆盖），App 桥接靠 build + 手动验证——但优先照现有 App 测试模式写。

- [ ] **Step 3: 运行确认失败**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestAINotificationsOnly_GetSetRoundTrip -v`
Expected: FAIL — `GetAINotificationsOnly`/`SetAINotificationsOnly` undefined

- [ ] **Step 4: 实现 app.go**

在 `GetNotificationsEnabled`/`SetNotificationsEnabled` 附近加（仿它们）：

```go
// GetAINotificationsOnly returns the current persisted preference (default true).
func (a *App) GetAINotificationsOnly() bool {
	if a.cfgStore == nil {
		return true
	}
	return a.cfgStore.Get().AINotificationsOnlyOrDefault()
}

// SetAINotificationsOnly persists the user's toggle.
func (a *App) SetAINotificationsOnly(enabled bool) error {
	if a.cfgStore == nil {
		return nil
	}
	cfg := a.cfgStore.Get()
	cfg.AINotificationsOnly = &enabled
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markPrefDirtyAndPush("ai_notifications_only")
	return nil
}
```

> `GetNotificationsEnabled` 的 nil-cfgStore 守卫和 Set 的写法以现有实际为准，照搬其结构（上面是仿写，确认 cfgStore 字段名/Set 签名一致）。

- [ ] **Step 5: prefssync 3 处 case（白名单，否则不同步）**

`desktop/prefssync_adapter.go`：
- ReadValue 的 switch 加（仿 `shell_integration_enabled`）：
  ```go
  case "ai_notifications_only":
  	if c.AINotificationsOnly == nil {
  		return nil, false
  	}
  	b, _ := json.Marshal(*c.AINotificationsOnly)
  	return b, true
  ```
- WriteValue 的 switch 加：
  ```go
  case "ai_notifications_only":
  	var b bool
  	if err := json.Unmarshal(value, &b); err != nil {
  		return err
  	}
  	c.AINotificationsOnly = &b
  ```
  （WriteValue 末尾如何持久化 c，看现有 case 怎么收尾——可能有统一的 `a.store.Set(c)`，照现有模式。）

`desktop/app.go` 的 `isPrefCustomized`（~1914）switch 加：
```go
	case "ai_notifications_only":
		return c.AINotificationsOnly != nil
```

- [ ] **Step 6: 运行测试 + build**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run 'TestAINotificationsOnly|TestShouldNotify' && /home/attson/sdk/go1.24.13/bin/go build -tags webkit2_41 -o /dev/null ./desktop/`
Expected: PASS + build OK

- [ ] **Step 7: gofmt + Commit**

Run: `/home/attson/sdk/go1.24.13/bin/gofmt -l desktop/app.go desktop/prefssync_adapter.go`（无输出）

```bash
git add desktop/app.go desktop/prefssync_adapter.go desktop/*_test.go
git commit -m "feat(desktop): Get/SetAINotificationsOnly + prefs sync"
```

---

## Task 4: relay_host 两分支接守卫

**Files:**
- Modify: `desktop/relay_host.go`

- [ ] **Step 1: 先读触发点**

`cat desktop/relay_host.go` 看 `SetOnTaskStateChange`（约 line 524）的两个 dispatch 分支：`Completed/Failed`（约 542）和 `WaitingInput`（约 559）。两分支都在 `go func(){ info := sess.Info(); ... }()` goroutine 内。确认 `h.cfg`（`*configStore`）字段名。

- [ ] **Step 2: 接守卫**

在 Completed/Failed 分支的 goroutine 内、`info := sess.Info()` 之后、Dispatch 之前加：
```go
if !shouldNotifySession(info.Type, h.cfg.Get().AINotificationsOnlyOrDefault()) {
	return
}
```
在 WaitingInput 分支的 goroutine 内同样加（它也有 `info := sess.Info()`；确认是 goroutine 内 info 之后）。两处都加，确保命令完成 + 等待输入都被过滤。

> 若 WaitingInput 分支当前没有 `info := sess.Info()`（直接 dispatch sid），则在它的 goroutine 内先取 `info := sess.Info()` 再判断。读实际代码决定。

- [ ] **Step 3: build + 全 desktop 测试**

Run: `/home/attson/sdk/go1.24.13/bin/go build -tags webkit2_41 -o /dev/null ./desktop/ && /home/attson/sdk/go1.24.13/bin/go test ./desktop/ && /home/attson/sdk/go1.24.13/bin/go vet ./desktop/`
Expected: build OK + 全 PASS + vet 干净

- [ ] **Step 4: gofmt + Commit**

Run: `/home/attson/sdk/go1.24.13/bin/gofmt -l desktop/relay_host.go`（无输出）

```bash
git add desktop/relay_host.go
git commit -m "feat(feishu): skip non-AI session notifications when AI-only is on"
```

---

## Task 5: 前端 toggle + 4 层绑定 + i18n

**Files:**
- Modify: `desktop/frontend/wailsjs/go/main/App.{d.ts,js}`, `src/lib/api.ts`, `src/components/SettingsFeishu.vue`, `src/i18n/messages/{en,zh-CN}.ts`
- Test: `desktop/frontend/src/components/SettingsFeishu.test.ts`

- [ ] **Step 1: 先探索前端模板**

- `cat desktop/frontend/src/lib/api.ts` 看 `getNotificationsEnabled`（~652）/`setNotificationsEnabled`（~656）+ `AppBindings.GetNotificationsEnabled`（~308）interface。
- `cat desktop/frontend/wailsjs/go/main/App.d.ts`/`App.js` 看 GetNotificationsEnabled 绑定（手加新绑定照它）。
- `cat desktop/frontend/src/components/SettingsFeishu.vue` 看现有 hook toggle 怎么写（checkbox + onToggle + ref + onMounted 读初值），从哪 import API（直接 `../lib/api` 还是 platform）。
- `cat desktop/frontend/src/components/SettingsFeishu.test.ts` 看测试风格（源码字符串断言 or 组件挂载）。
- 看 `src/i18n/messages/en.ts` 和 `zh-CN.ts` 的 `settings.feishu` 段。

- [ ] **Step 2: 加 wailsjs 绑定（手动）**

`App.d.ts` 加：
```ts
export function GetAINotificationsOnly():Promise<boolean>;
export function SetAINotificationsOnly(arg1:boolean):Promise<void>;
```
`App.js` 加（仿 GetNotificationsEnabled）：
```js
export function GetAINotificationsOnly() {
  return window['go']['main']['App']['GetAINotificationsOnly']();
}
export function SetAINotificationsOnly(arg1) {
  return window['go']['main']['App']['SetAINotificationsOnly'](arg1);
}
```

- [ ] **Step 3: api.ts wrapper**

`AppBindings` interface 加 `GetAINotificationsOnly(): Promise<boolean>` + `SetAINotificationsOnly(enabled: boolean): Promise<void>`。加 export（仿 getNotificationsEnabled）：
```ts
export function getAINotificationsOnly(): Promise<boolean> {
  return bindings().GetAINotificationsOnly();
}
export function setAINotificationsOnly(enabled: boolean): Promise<void> {
  return bindings().SetAINotificationsOnly(enabled);
}
```
（若 SettingsFeishu 从 platform 层拿 API，则 platform/types.ts + wails.ts 也加 `aiNotificationsOnly` getter/setter；Step 1 探索决定。多数 settings 组件直接从 `../lib/api` 拿，优先直接。）

- [ ] **Step 4: 写失败测试**

`SettingsFeishu.test.ts` 仿现有风格加测试。若是源码字符串风格：断言 `SettingsFeishu.vue` 源码含 `getAINotificationsOnly`/`setAINotificationsOnly`/`aiOnlyNotifications`（i18n key）/checkbox。若是挂载风格：mock api 默认 true，断言 toggle 渲染 + 切换调 setAINotificationsOnly。
先 `npm run test -- SettingsFeishu` 确认新断言 FAIL。

- [ ] **Step 5: SettingsFeishu.vue toggle**

加一个 ref（默认 true）+ onMounted 读 `getAINotificationsOnly()` + onToggle 调 `setAINotificationsOnly`。template 加 toggle（仿现有 hook toggle 的 label/checkbox 结构）：
```html
<label class="ai-only__toggle">
  <input type="checkbox" :checked="aiOnlyNotifications" @change="onToggleAIOnly" />
  {{ t('settings.feishu.aiOnlyNotifications') }}
</label>
```
import `getAINotificationsOnly`/`setAINotificationsOnly`（从 ../lib/api，或 platform，依 Step 1）。

- [ ] **Step 6: i18n**

`en.ts` 的 `settings.feishu` 加 `aiOnlyNotifications: "Notify for AI sessions only"`。`zh-CN.ts` 加 `aiOnlyNotifications: "仅 AI 会话通知"`。

- [ ] **Step 7: 测试 + build**

Run（desktop/frontend/）: `npm run test -- SettingsFeishu`（PASS）+ `npm run test`（全量不破坏）+ `npm run build`（TS + 构建通过）

- [ ] **Step 8: Commit**

```bash
git add desktop/frontend/wailsjs/go/main/App.d.ts desktop/frontend/wailsjs/go/main/App.js desktop/frontend/src/lib/api.ts desktop/frontend/src/components/SettingsFeishu.vue desktop/frontend/src/components/SettingsFeishu.test.ts desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
# 若改了 platform 层也 add
git commit -m "feat(settings): Feishu AI-only notifications toggle"
```

---

## Task 6: 全量回归

- [ ] **Step 1: 后端全量**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ && /home/attson/sdk/go1.24.13/bin/go build -tags webkit2_41 -o /dev/null ./desktop/ && /home/attson/sdk/go1.24.13/bin/go vet ./desktop/`
Expected: 全 PASS / build OK / vet 干净

- [ ] **Step 2: 前端全量**

Run（desktop/frontend/）: `npm run test && npm run build`
Expected: 全 PASS / build OK

---

## Self-Review Notes

- **Spec §4.1 shouldNotifySession 纯函数** → Task 1（5 case 含 empty type）。
- **Spec §4.2 config AINotificationsOnly + OrDefault（默认 true）** → Task 2。
- **Spec §4.3 app Get/Set** → Task 3。
- **Spec §5 prefssync 白名单** → Task 3 Step 5（3 处 case：ReadValue/WriteValue/isPrefCustomized）。
- **Spec §4.4 relay_host 两分支守卫** → Task 4（命令完成 + 等待输入都过滤）。
- **Spec §4.5 前端 toggle + i18n** → Task 5（4 层绑定 + SettingsFeishu toggle + en/zh-CN）。
- **类型一致性**：`shouldNotifySession(string, bool) bool`（Task 1）、`AINotificationsOnly *bool` / `AINotificationsOnlyOrDefault()`（Task 2）、`GetAINotificationsOnly`/`SetAINotificationsOnly`（Task 3/5）、prefs key `"ai_notifications_only"`（Task 2 json tag / Task 3 prefssync / Task 5 binding）—— 全程一致。
- **最大不确定点**：Task 3 的 App 测试构造（`newTestApp`）+ prefssync WriteValue 的持久化收尾（看现有 case 实际），Task 5 的前端 API 来源层（api vs platform）+ 测试风格（源码字符串 vs 挂载）—— 各 Task Step 1 都要求先探索现有实际，按实际接。
