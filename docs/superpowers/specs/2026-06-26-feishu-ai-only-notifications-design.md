# 飞书通知「仅 AI 会话」开关 — design

Date: 2026-06-26
Status: Drafted — awaiting user review before plan.

## 0. Summary

飞书通知新增「仅 AI 会话通知」开关，**默认开启**。开启时，飞书的命令完成 + 等待输入通知**只对 AI 会话发送**，非 AI（shell）会话不发——避免每条 shell 命令都触发飞书通知的噪音。

## 1. Goals

- 设置页飞书区新增「仅 AI 会话通知」toggle，**默认开启**。
- 开启时：命令完成 + 等待输入通知**仅 AI 会话**发送；shell 会话两类通知都跳过。
- 关闭时：恢复现有行为（所有会话都通知）。
- 偏好持久化 + 跨设备同步（复用现有 `markPrefDirtyAndPush` 机制）。

## 2. Non-goals

- 不区分通知子类型（命令完成 vs 等待输入分别开关）——一个开关统管两类。
- 不改通知卡片内容 / 飞书集成本身。
- 不动现有 `NotificationsEnabled` 总开关（新开关是它之下的过滤维度，二者独立：总开关关则全不发；总开关开 + AI-only 开则只发 AI）。

## 3. 现状（改动基线）

- **通知触发点**：`desktop/relay_host.go:524` 的 `SetOnTaskStateChange` 回调。`Completed/Failed` 分支 → `DispatchCommandFinished`；`WaitingInput` 分支 → `DispatchWaitingInput`。两分支都在 goroutine 内调 `sess.Info()`（锁已释放）。
- **会话类型**：`sess.Info().Type`（`proto.SessionInfo.Type`，字符串 `"ai"`/`"shell"`/...）。常量 `session.SessionTypeAI`。relay_host 已 import `internal/session`。
- **偏好模板**：`desktop/config.go` 的 `AutoCheckUpdates *bool` + `AutoCheckUpdatesOrDefault()`；`NotificationsEnabled *bool` + `NotificationsEnabledOrDefault()`。`desktop/app.go` 的 `GetNotificationsEnabled`/`SetNotificationsEnabled`（1606-1623）含 `markPrefDirtyAndPush("notifications_enabled")`。
- **前端开关模板**：`SettingsFeishu.vue` 现有 hook toggle（checkbox + 后端 Get/Set API）。

## 4. 设计

### 4.1 过滤决策（纯函数，可测核心）

`desktop` 包内新增纯函数：

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

### 4.2 后端 config `desktop/config.go`

- `appConfig` 加 `AINotificationsOnly *bool \`json:"ai_notifications_only,omitempty"\``。
- 加 `AINotificationsOnlyOrDefault() bool`：nil → **true（默认开启）**；否则透传。

### 4.3 App 桥接 `desktop/app.go`

- `GetAINotificationsOnly() bool` → `cfgStore.Get().AINotificationsOnlyOrDefault()`（仿 GetNotificationsEnabled）。
- `SetAINotificationsOnly(enabled bool) error` → 设 `cfg.AINotificationsOnly = &enabled` + 持久化 + `markPrefDirtyAndPush("ai_notifications_only")`。

### 4.4 过滤接线 `desktop/relay_host.go`

`SetOnTaskStateChange` 的两个 dispatch 分支（Completed/Failed 和 WaitingInput），各在 goroutine 内、取到 `info := sess.Info()` 后加守卫：

```go
if !shouldNotifySession(info.Type, h.cfg.Get().AINotificationsOnlyOrDefault()) {
	return
}
```

`h.cfg`（`*configStore`）在 goroutine 内读 config 安全（锁已释放）。

### 4.5 前端 `desktop/frontend/src/components/SettingsFeishu.vue`

- 加一个 toggle（仿现有 hook toggle）：绑 `getAINotificationsOnly`/`setAINotificationsOnly`（platform/api wrapper，仿 notifications 现有 wrapper）。
- 初始加载时读当前值，默认 true。
- i18n key（en + zh-CN）：`settings.feishu.aiOnlyNotifications` 标签 + 说明。

## 5. 跨设备同步

`markPrefDirtyAndPush("ai_notifications_only")` 复用现有偏好同步：开关变更随其他偏好一起 push 到 relay，多设备一致。需确认 prefs sync 的字段白名单是否需要登记新 key（看 `markPrefDirtyAndPush` / prefssync 实现，若有白名单则加 `ai_notifications_only`）。

## 6. 测试

- `desktop/*_test.go` `shouldNotifySession`：ai+on→true、shell+on→false、ai+off→true、shell+off→true。
- `config` `AINotificationsOnlyOrDefault`：nil→true、`&true`→true、`&false`→false。
- `app` Get/Set 往返 + 默认 true（仿现有 NotificationsEnabled 测试）。
- 前端 `SettingsFeishu.test.ts`：toggle 渲染、默认开、切换调 setAINotificationsOnly。
- relay_host 闭包不强求单测（决策已由 shouldNotifySession 纯函数覆盖）。

## 7. 实现顺序

1. `shouldNotifySession` 纯函数 + 测试。
2. config `AINotificationsOnly` + OrDefault + 测试。
3. app Get/Set 桥接 + 测试 + prefssync 白名单（若需）。
4. relay_host 两分支接守卫。
5. 前端 wrapper（api/platform）+ SettingsFeishu toggle + i18n + 测试。
6. 全量回归。
