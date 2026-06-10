# Settings → Relay: 状态 Pill 4 态 + 表单扁平化统一保存

> **Audience**: 实施者
> **Last updated**: 2026-06-10
> **Status**: design / approved
> **See also**: `desktop/frontend/src/components/SettingsRelay.vue` · `desktop/app.go`

## 背景

两个用户报的 UX 问题合并修：

1. **误导性绿色 pill**：当前 `SettingsRelay.vue:79` 的 `statusPill` computed 的 fallback 分支无条件返回「uplink 运行中」绿色，即便 URL 是 `http//`（语法错）/ 网络挂 / 服务端无响应。用户看到绿色以为已连接 relay，实际根本没连上。
2. **冗余的「连接远程 relay」卡片**：远程登录 form 套了一层带标题 + 副标题 + 「登录」按钮的卡，跟同页其他字段（URL、远端会话权限）的扁平输入框风格不一致。用户希望邮箱/密码也是扁平字段，没有独立「登录」按钮，全部由底部「保存并连接」一键提交。

## 目标

1. Pill 准确反映 4 个状态（待配置 / relay 无效 / 正在连接 / 已连接）
2. 表单结构扁平：邮箱/密码与 URL 同等输入框样式，无 card 包裹
3. 删除独立「登录」按钮 + 「连接远程 relay」副标题 + 相关 i18n key
4. 「保存并连接」按钮承担完整流程：URL 格式校验 → `/api/version` 探测（Wails Go 方法） → 登录（若 email+password 填了）→ SetRelayConfig → 启动 uplink

## 非目标

- 不引入后端 `uplink:error` / `uplink:disconnect` 事件（YAGNI；当前的 `AUTH_INFO` 事件足够支撑 4 态 pill）
- 不做 URL 实时输入校验（仅在按钮点击时校验 + 探测）
- 不动 mobile / web / 其他登录入口
- 不持久化密码

## 设计

### 1. Pill 4 态

`SettingsRelay.vue::statusPill` computed 重写：

| 顺序 | 条件 | cls | 文案 i18n key |
|---|---|---|---|
| 1 | `connectedEmail \|\| connectedUserID` | `on` 绿 | `connectedAs` |
| 2 | `!url \|\| paused` | `off` 灰 | `notConfigured` / `paused` |
| 3 | 不是合法 ws/wss/http/https URL (语法错) | `error` 红 | `relayInvalid` |
| 4 | 合法 URL + 还没 AUTH_INFO | `warn` 黄 | `connecting` |

判定顺序：已连接（绿）→ 未配置/暂停（灰）→ 格式无效（红）→ 正在连接（黄）。

URL 合法性纯前端校验：

```ts
function isValidRelayUrl(s: string): boolean {
  try {
    const u = new URL(s.trim());
    if (!u.host) return false;
    return ["http:", "https:", "ws:", "wss:"].includes(u.protocol);
  } catch {
    return false;
  }
}
```

CSS 加两个新状态类（已有 `.on` 绿 + `.off` 灰）：

```css
.status-pill.warn { color: var(--warn); }
.status-pill.warn .dot { color: var(--warn); }
.status-pill.error { color: var(--bad); }
.status-pill.error .dot { color: var(--bad); }
```

`--warn` 若不存在用现有 design tokens 中的近似黄/橙；`--bad` 已有（错误红）。

### 2. 表单结构扁平化

#### 删除

- `<section class="login-card">` 整个 section（包含标题 `loginTitle`、副标题 `loginHint`、子标签 `email` / `password`、`login-actions` 按钮组）
- 独立「登录」按钮 + 关联的 `login()` 函数 + `loginInProgress` ref + `loginError` ref + `loginSuccess` ref
- 整个 `<style scoped>` 里 `.login-card / .login-form / .login-btn / .login-actions / .login-error / .login-ok` 规则

#### 新结构

邮箱和密码字段升级为 top-level 字段，跟 URL 同级（同样 `.field-label` + `.password-field` 包裹密码）：

```html
<label class="field-label" for="relay-url">{{ t("settings.relay.url") }}</label>
<input id="relay-url" v-model="url" ... />

<label class="field-label" for="relay-email">{{ t("settings.relay.email") }}</label>
<input id="relay-email" v-model="email" type="email" autocomplete="username" ... />

<label class="field-label" for="relay-password">{{ t("settings.relay.password") }}</label>
<div class="password-field">
  <input id="relay-password" v-model="password" :type="showPassword ? 'text' : 'password'"
         autocomplete="current-password" ... />
  <button class="password-toggle" @click="showPassword = !showPassword" ...>
    <!-- eye / eye-off SVG (沿用 v0.2.63 已加的样式) -->
  </button>
</div>

<label class="field-label" for="relay-permission">{{ t("settings.relay.remotePermission") }}</label>
<SelectDropdown id="relay-permission" v-model="remotePermission" ... />

<!-- 已有的 AllowInsecure 切换、Pause 切换等保持原状 -->
```

「保存并连接」按钮仍在 dialog footer（不在 SettingsRelay 内部），由父 `SettingsDialog.vue` 调用 `defineExpose` 出来的 `save()`。

### 3. 「保存并连接」流程

`SettingsRelay.vue::save()` 重写为统一 promise 链：

```ts
async function save() {
  saving.value = true;
  error.value = "";

  // 1. URL format validation
  if (!isValidRelayUrl(url.value)) {
    error.value = t("settings.relay.relayInvalid");
    saving.value = false;
    return;
  }

  // 2. /api/version probe via Wails Go method
  try {
    await probeRelayVersion(url.value);
  } catch (e: any) {
    error.value = t("settings.relay.versionProbeFailed", { reason: String(e?.message ?? e) });
    saving.value = false;
    return;
  }

  // 3. Decide login vs reuse-token vs error
  const hasCreds = !!(email.value && password.value);
  const hasExistingToken = !!token.value;

  if (hasCreds) {
    try {
      await loginRemoteRelay(url.value, email.value, password.value);
    } catch (e: any) {
      error.value = t("settings.relay.loginFailedInline", { reason: String(e?.message ?? e) });
      saving.value = false;
      return;
    }
    password.value = "";  // wipe after use (security)
  } else if (hasExistingToken) {
    // No credentials provided but session token exists — update only other fields.
    try {
      await setRelayConfig({
        url: url.value,
        token: token.value,
        session_expires_at: 0,
        allow_insecure_relay: allowInsecureRelay.value,
        remote_permission: remotePermission.value,
        last_email: "",  // ignored by SetRelayConfig per session-token spec
      });
    } catch (e: any) {
      error.value = e?.message ?? String(e);
      saving.value = false;
      return;
    }
  } else {
    error.value = t("settings.relay.credentialsRequired");
    saving.value = false;
    return;
  }

  // 4. Refresh from store to pick up updated last_email + token + connected
  const cfg = await getRelayConfig();
  token.value = cfg.token;
  email.value = cfg.last_email ?? "";  // backfill in case login succeeded
  snapshotPersisted();
  saving.value = false;
  emit("relay-config-changed");
}
```

### 4. 新 Wails Go 方法

`desktop/app.go` 加 `ProbeRelayVersion`：

```go
// ProbeRelayVersion does a lightweight GET <relayURL>/api/version to verify
// the URL points at an atterm relay. Returns nil if the response is 200 and
// the JSON body has a non-empty "version" field. Otherwise returns an error
// the frontend surfaces as "无法连接到 relay" inline beneath the URL field.
//
// This is the same auth-less endpoint /healthz and /version that the
// session-token spec marks public. No credentials are sent.
func (a *App) ProbeRelayVersion(relayURL string) error {
    relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
    if relayURL == "" {
        return fmt.Errorf("relay url is empty")
    }
    httpURL, _, err := relayLoginEndpoints(relayURL)
    if err != nil {
        return fmt.Errorf("invalid relay url: %w", err)
    }
    ctx := a.ctx
    if ctx == nil {
        ctx = context.Background()
    }
    req, err := http.NewRequestWithContext(ctx, "GET", httpURL+"/api/version", nil)
    if err != nil {
        return err
    }
    client := &http.Client{Timeout: 5 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("connect: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("status %d", resp.StatusCode)
    }
    var out struct {
        Version string `json:"version"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return fmt.Errorf("not an atterm relay (decode): %w", err)
    }
    if out.Version == "" {
        return fmt.Errorf("not an atterm relay (no version field)")
    }
    return nil
}
```

`desktop/frontend/src/lib/api.ts` 加对应 TS binding；`wailsjs/go/main/App.d.ts` 和 `App.js` 手编同步。

### 5. i18n 改动

**新增 key**（en + zh-CN）：

```ts
// en
connecting: "Connecting…",
relayInvalid: "Invalid relay address",
versionProbeFailed: "Failed to reach relay: {reason}",
loginFailedInline: "Login failed: {reason}",
credentialsRequired: "Email and password required",

// zh-CN
connecting: "正在连接…",
relayInvalid: "relay 地址无效",
versionProbeFailed: "无法连接到 relay：{reason}",
loginFailedInline: "登录失败：{reason}",
credentialsRequired: "请填邮箱和密码",
```

**删除 key**（不再有独立登录入口）：

- `loginTitle` 「连接远程 relay」
- `loginHint`  「请输入已在该 relay 注册的账号邮箱与密码。」
- `login`      「登录」（按钮文案）
- `loginInProgress` 「登录中…」
- `loginFailed`     「登录失败」（旧的，没参数）
- `loggedIn`        「已连接远程 relay」

注：`email`、`password`、`passwordShow`、`passwordHide` 保留（字段标签还在用）。

### 6. 测试

`SettingsRelay.test.ts` 用源码 substring 断言（沿用现有模式）：

- `statusPill.cls = "warn"` 路径存在（断言 `cls: "warn"`）
- `statusPill.cls = "error"` 路径存在
- `isValidRelayUrl` 函数声明存在
- `probeRelayVersion` 被 import 并被 `save()` 调用
- 「登录」按钮 DOM (`class="login-btn"`) 不再存在
- 「连接远程 relay」副标题 (`loginHint` / `loginTitle`) 不再被引用
- 4 个新 i18n key 被 SFC 引用

`desktop/app_test.go`（或同目录 `app_probe_test.go`）加 `ProbeRelayVersion` 单测：
- 用 `httptest.NewServer` 起一个 fake 返回 `{"version":"v1"}` 的 endpoint → `ProbeRelayVersion` 返回 nil
- 404 → 错误含 "status 404"
- 返回 `{}` 缺 `version` → 错误含 "no version field"
- 不可达 URL（端口关闭）→ 错误含 "connect:"

### 7. CSS 新 status pill 颜色

`SettingsRelay.vue::<style scoped>` 添加：

```css
.status-pill.warn { color: var(--warn, #d97706); }
.status-pill.warn .dot { color: var(--warn, #d97706); }
.status-pill.error { color: var(--bad, #ef4444); }
.status-pill.error .dot { color: var(--bad, #ef4444); }
```

如组件库 design tokens 已有 `--warn`，去掉 fallback。否则用 amber-600 (`#d97706`) / red-500 (`#ef4444`)。

## 文件清单

| 文件 | 改动 |
|---|---|
| `desktop/app.go` | + `ProbeRelayVersion(relayURL string) error` 方法 + 单测 |
| `desktop/frontend/src/lib/api.ts` | + `probeRelayVersion()` TS wrapper + `AppBindings.ProbeRelayVersion` |
| `desktop/frontend/wailsjs/go/main/App.d.ts` | + `ProbeRelayVersion` 声明 |
| `desktop/frontend/wailsjs/go/main/App.js` | + `ProbeRelayVersion` runtime stub |
| `desktop/frontend/src/components/SettingsRelay.vue` | 删 login card、`login()` 函数、`loginInProgress` 等 ref；重写 `save()` 流；改 `statusPill` 4 态；加 warn/error CSS；email/password 升级 top-level 字段 |
| `desktop/frontend/src/components/SettingsRelay.test.ts` | 删旧 login 测试，加 4-态 pill + save 流 + probe 调用断言 |
| `desktop/frontend/src/i18n/messages/en.ts` | + 5 新 key, - 6 旧 key |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | + 5 新 key, - 6 旧 key |

预计 ~200 行变更（删 ~80 + 加 ~120）。

## 行为变更摘要

| 项 | 旧 | 新 |
|---|---|---|
| Pill 状态 | 2 态（on/off） | 4 态（on/warn/error/off） |
| 邮箱/密码输入 | 卡片内子表单 + 独立「登录」按钮 | 扁平输入框 + 由底部「保存并连接」统一提交 |
| URL 校验 | 仅 ws/wss 二选一（保存时验） | 格式校验 + `/api/version` 探测（保存时验） |
| 登录 endpoint 调用时机 | 用户主动点「登录」 | 「保存并连接」时若 email+password 填了就触发 |
| 仅改其他字段（如 paused） | 需要先点「登录」，再点「保存并连接」 | 只点「保存并连接」即可（复用现有 session token） |
| 「连接远程 relay」标题/副标题 | 显示 | 删除 |
