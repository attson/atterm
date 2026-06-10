# Settings → Relay Auth State Restore

> **Audience**: 实施者
> **Last updated**: 2026-06-10
> **Status**: design / approved
> **See also**: `desktop/frontend/src/components/SettingsRelay.vue` · `desktop/app.go` · `docs/spec/auth.md`

## 背景

用户登录远程 relay 成功后，关掉 Settings 再重新打开：

- 绿色 pill「● 已连接为 attson1@outlook.com」消失
- 邮箱表单字段是空的
- 用户读起来"像是又没登录了"，但 uplink 实际还在用持久化的 session_token 正常连着

原因：`SettingsRelay.vue:97-106` 的「已连接为」pill 只靠 `relay:auth-info` 事件填充——这个事件在 uplink (重)连时一次性发出。Settings 对话框是事件之后开的就会错过。同时邮箱字段是个 transient `ref("")`，每次组件重新挂载都是空白。

## 目标

1. 不依赖时序：Settings 一打开就**主动**显示当前登录身份
2. 邮箱字段预填（用户希望减少手动重输）
3. 密码不保存（用户明确不需要，且这是安全基线）

## 设计

### 数据持久化

`appConfig`（`desktop/config.go`）新增字段：

```go
// RelayLastEmail caches the email used in the most recent successful
// LoginRemoteRelay call. Only used to prefill the Settings → Relay form
// on next open; never used for authentication itself. Plaintext is fine
// because the email is non-secret and lives next to the session_token
// in the same config.json.
RelayLastEmail string `json:"relay_last_email,omitempty"`
```

写入位置：`desktop/app.go::LoginRemoteRelay` 在 `SetRelayConfig` 成功后顺手 `cfg.RelayLastEmail = email`。

### Wails-bound 类型

`RelayConfig` Wails struct（`desktop/app.go`）和 TS 接口（`desktop/frontend/src/lib/api.ts`）各加一个字段：

```go
LastEmail string `json:"last_email"`
```

```ts
export interface RelayConfig {
  // ...existing...
  last_email: string;
}
```

`desktop/frontend/wailsjs/go/models.ts` 同步（手编，遵循已有的 RelayConfig 类）。

`GetRelayConfig` 读出 `cfg.RelayLastEmail` 填进响应；`SetRelayConfig` 不接收用户传入的 LastEmail（防止外部覆盖；这字段只能由 `LoginRemoteRelay` 写）。

### 前端行为

`SettingsRelay.vue::onMounted`（在现有的 `loadConfig` 之后）：

```ts
// Prefill email from persisted config (set by LoginRemoteRelay on
// last successful login). Password stays empty for security.
email.value = cfg.last_email ?? "";

// Show the "logged in as X" pill immediately without waiting for the
// uplink's relay:auth-info event. The event listener below stays
// registered as a fallback for the first-login-without-reload case.
if (cfg.token) {
  try {
    const me = await fetchRelayMe();
    connectedEmail.value = me.email || "";
    connectedUserID.value = me.id || "";
  } catch {
    // Token rejected (401) or network error — pill stays empty; uplink
    // will produce a more accurate state via its event stream.
  }
}
```

`relay:auth-info` 事件 listener 保留——首次登录时主动调 `fetchRelayMe()` 也是 OK 的，但事件仍是事实来源（用户名 / 头像变更等）。

### 范围

| 文件 | 改动 |
|---|---|
| `desktop/config.go` | + `RelayLastEmail` 字段 |
| `desktop/app.go` | + `RelayConfig.LastEmail`；`GetRelayConfig` / `LoginRemoteRelay` 读写它 |
| `desktop/frontend/src/lib/api.ts` | + `RelayConfig.last_email` |
| `desktop/frontend/wailsjs/go/models.ts` | + `last_email` 字段 |
| `desktop/frontend/src/components/SettingsRelay.vue` | + onMounted 预填 + 主动 fetchRelayMe |
| `desktop/frontend/src/components/SettingsRelay.test.ts` | + 测试用例 |

**不动**：

- 密码不保存（不引入 Keychain / Wails secure storage）
- 不动 mobile (`MobileSetup.vue`) / web (`web/src/login/`)
- 不动 setup 流程
- 不引入 i18n 新 key（已有的够用）

## 测试

`SettingsRelay.test.ts` 增加两个用例（沿用源码 `?raw` substring 断言模式）：

1. **测试 onMounted 在有 session token 时主动调 fetchRelayMe**：断言源码包含 `if (cfg.token)` + `await fetchRelayMe()`
2. **测试 email 字段从 cfg.last_email 预填**：断言源码包含 `email.value = cfg.last_email`

行为层面的 mount + 调用断言留给手工测试（这文件历来用 substring 而非组件 mount）。

手工测试清单：

- [ ] 登录远程 relay → 关 Settings → 重开 Settings → 立刻看到「已连接为 X」pill + 邮箱字段已填
- [ ] 重启桌面 App → 打开 Settings → uplink 仍在用 token 自动连 → 立刻看到「已连接为 X」+ 邮箱已填
- [ ] 切换不同账户登录 → cfg.last_email 跟着更新
- [ ] 配置 session token 被服务端撤销（401）→ pill 不出现，邮箱仍预填（方便快速重新登录）

## 安全考量

- `RelayLastEmail` 在 `~/.config/atterm/config.json` 明文。邮箱本身不是秘密，且已经和 `RelaySessionToken` 同文件，威胁模型一致。
- 密码不存（即便存 OS Keychain 也是个本质偏离 session_token 模型的设计；session_token 已经能做到"不要求用户每次输密码"）。
- `SetRelayConfig` 拒绝接收 `LastEmail`（避免外部代码意外覆盖）。

## 行为变更摘要

| 项 | 状态 |
|---|---|
| 登录成功后再开 Settings | ✅ 立刻看到「已连接为 X」+ 邮箱预填 |
| 桌面重启后再开 Settings | ✅ 同上（session_token 还有效时） |
| Token 被服务端撤销 | pill 不显示；邮箱仍预填便于重登 |
| 密码 | 永不保存（不变） |
| 移动端 / web | 不变 |
| 配置兼容 | 完全向后兼容：老配置文件没 `relay_last_email` 字段时 omitempty 自动跳过 |
