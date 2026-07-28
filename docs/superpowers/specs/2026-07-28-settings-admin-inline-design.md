# SettingsDialog 补齐 + 管理员面板内嵌 — design

Date: 2026-07-28
Status: Drafted — awaiting user review before plan.

## 0. Summary

3 个跟随 v0.3.18/v0.3.19 web-align-desktop 一并被清理的用户功能（改密码 /
注销账号 / 浏览器 Push 通知）需要以 SettingsDialog tab 形态回到主 App.vue
里，让 web 用户同样能用。同时把独立的 `web/admin.html` 管理员面板搬进主
App.vue 作为 **"admin 视图"**——不是弹窗，是主区域的另一种展示模式——用
TabBar 右侧新按钮切换。顺带在 SettingsDialog 底部显示当前版本号，以及
把在 web 上没意义的几个 tab（Relay / 诊断 / 接收文件）隐藏。

一次做完，删掉 `web/admin.html` + `web/src/admin/*` + `web/src/settings/*`
里被删掉的 API exports 都恢复。

## 1. Goals

- 恢复 3 个用户级安全 / 通知功能到 SettingsDialog：
  - 修改密码（stepup token + 旧密码验证）
  - 注销账号（stepup token + 邮箱二次确认）
  - 浏览器 Push 通知启用 / 禁用 / 测试
- 管理员面板（邀请 / 用户 / 配置 / 飞书）内嵌进主 App.vue：
  - TabBar 右侧加 admin 按钮（仅 `me.is_admin=true` 显示）
  - 点击后主区域从 PaneGrid 切换到 AdminPanel（sidebar + TabBar 保留）
  - 再次点或点任一 session tab 退出 admin 视图
- 版本号显示在 SettingsDialog 底部
- Web 上隐藏没意义的 SettingsDialog tab：Relay / 诊断 / 接收文件
- 删除独立 `web/admin.html` 入口

## 2. Non-goals

- **不做**"管理员用 SettingsDialog 里的一个 tab"—— admin 是独立视图不是设置
  子项（用户明确的 UX 选择）
- **不做**移动 Capacitor MobileApp.vue 上暴露 admin 按钮（mobile 有自己的 shell，
  admin 用桌面/web 操作即可）
- **不改**后端 API：`changePassword` / `deleteMe` / stepup token / push
  subscribe / admin 系列 endpoints 都已存在，只是前端调用被清理了
- **不做**账号迁移、忘记密码流程（本次范围内没这两个功能）
- **不做**多次输错密码锁定 UI（后端有 rate limit 就够）
- **不重构** admin 4 个 tab 的现有 UI；照搬 `web/src/admin/tabs/*.vue`

## 3. 现状

### 3.1 SettingsDialog 现有 tab

`desktop/frontend/src/components/SettingsDialog.vue` 已包含（按顺序）：

- 通用 (`SettingsGeneral.vue`)
- 会话栏 (`SettingsTasks.vue`)
- Relay (`SettingsRelay.vue`) — 设置 relay URL / token
- 已登录设备 (`SettingsDevices.vue`)
- 快捷模板 (`SettingsTemplates.vue`)
- 诊断 (`SettingsDiagnostics.vue`) — 展示 Wails 端环境 + 日志导出
- 飞书集成 (`SettingsFeishu.vue`)
- 接收文件 (`SettingsReceivedFiles.vue`)
- 插件 / 快捷键 (`SettingsPlugins/Shortcuts.vue`) — 已经通过
  `v-if="caps.pluginHost"` 隐藏于 web / mobile
- 更新 (`SettingsUpdates.vue`) — 已通过 `v-if="caps.autoUpdate"` 隐藏

### 3.2 被删的 3 个功能

Task 6.3（0a49740, f731e11）删的：

- `web/src/settings/tabs/ChangePassword.vue` — 走 stepup token + 修改密码
- `web/src/settings/tabs/DangerZone.vue` — stepup token + 邮箱输入二次确认 + 注销
- `web/src/settings/tabs/Notifications.vue` — Push 启用 / 禁用 / 测试
- API exports 一并被删：
  - `web/src/shared/api/me.ts::changePassword` `deleteMe`
    `listSessions` `revokeSession` `signOutOthers`
  - `web/src/shared/api/push.ts::testPush`
  - `web/src/shared/api/push-flow.ts`（整个文件）
  - `web/src/shared/api/stepup.ts::requestStepUpToken`（整个文件）

历史提交 `0a49740^` 保留完整旧代码可供拷贝。

### 3.3 独立 admin 入口

- `web/admin.html` + `web/src/admin/App.vue` (89 行 topbar + tab 路由)
- `web/src/admin/tabs/`：Invitations (180) / Users (214) / Config (164) /
  FeishuConfig (134) —— 共 692 行
- `web/vite.config.ts::rollupOptions.input.admin` 是这个 HTML entry
- 后端 `/api/admin/*` endpoint 没动过

### 3.4 版本号

- Wails 端：`main.Version` 通过 `-X main.Version=$VERSION` 注入
- 已有 `desktop/diagnostics.go::AppVersion` 字段，通过 `GetDiagnostics()`
  返回，但太重（整个 diagnostics 结构）
- Web 端：`web/src/shared/api/version.ts::fetchVersion()` 调 `/api/version`
  返回 relay 版本（因为 web bundle 由 relay 服务，二者始终同版本）
- 目前 App.vue 里没有版本号显示

### 3.5 管理员身份判定

`web/src/shared/api/types.ts::RelayMe.is_admin: boolean` 已存在；
`platform.relay.fetchMe()` 已经在 App.vue 里调过。可直接用。

## 4. 设计

### 4.1 总体架构改动

| 位置 | 动作 | 说明 |
|---|---|---|
| `desktop/frontend/src/components/SettingsAccount.vue` | 新增 | 改密码 + 注销；从 `0a49740^:web/src/settings/tabs/{ChangePassword,DangerZone}.vue` 拷合并 |
| `desktop/frontend/src/components/SettingsGeneral.vue` | 修改 | 尾部追加 Push 通知块 |
| `desktop/frontend/src/components/SettingsDialog.vue` | 修改 | 加"账号" tab；4 tab 加 `v-if="caps.wailsBindings"`（Relay / 诊断 / 接收文件；飞书按 4.7 判定）；底部显示版本号 |
| `desktop/frontend/src/components/admin/Invitations.vue` | 新增 | 拷 `web/src/admin/tabs/Invitations.vue` |
| `desktop/frontend/src/components/admin/Users.vue` | 新增 | 拷 `web/src/admin/tabs/Users.vue` |
| `desktop/frontend/src/components/admin/Config.vue` | 新增 | 拷 `web/src/admin/tabs/Config.vue` |
| `desktop/frontend/src/components/admin/FeishuConfig.vue` | 新增 | 拷 `web/src/admin/tabs/FeishuConfig.vue` |
| `desktop/frontend/src/components/AdminPanel.vue` | 新增 | 4 tab 容器，作为主区域视图 |
| `desktop/frontend/src/components/TabBar.vue` | 修改 | ⚙ 左侧加 admin 按钮，`v-if="isAdmin"` |
| `desktop/frontend/src/App.vue` | 修改 | 新 `adminViewOpen: Ref<boolean>` + `isAdmin: Ref<boolean>`；主区域条件渲染 AdminPanel / PaneGrid |
| `desktop/frontend/src/platform/types.ts::SystemBridge` | 修改 | 加 `getAppVersion(): Promise<string>` |
| `desktop/frontend/src/platform/wails.ts` | 修改 | 实现 `getAppVersion` → 新 Wails binding `GetAppVersion()` |
| `desktop/frontend/src/platform/capacitor.ts` | 修改 | 实现 `getAppVersion` → 硬编码 `'dev'`（capacitor 版本注入是后续 work） |
| `desktop/frontend/src/platform/web.ts` | 修改 | 实现 `getAppVersion` → `fetchVersion()` from `@webshared/api/version` |
| `desktop/app.go` | 修改 | 新方法 `GetAppVersion() string { return Version }` |
| `web/src/shared/api/me.ts` | 修改 | 恢复 `changePassword`（historic 版本，走 stepup） + `deleteMe` |
| `web/src/shared/api/push.ts` | 修改 | 恢复 `testPush` |
| `web/src/shared/api/push-flow.ts` | 新增（恢复） | `enablePushFlow` `disablePushFlow` |
| `web/src/shared/api/stepup.ts` | 新增（恢复） | `requestStepUpToken` |
| `web/admin.html` | 删除 | |
| `web/src/admin/` | 删除 | 整个目录 |
| `web/vite.config.ts` | 修改 | 从 `rollupOptions.input` 移除 `admin` 条目 |
| `docs/spec/architecture.md` | 修改 | 记录 admin 内嵌新架构 |

### 4.2 TabBar admin 按钮

- 位置：`TabBar.vue` 最右侧，⚙ 按钮**左侧**
- 图标：`ShieldUser` (lucide-vue-next) 或 `Shield`
- 可见性：`v-if="isAdmin"`（`isAdmin` 由 App.vue 通过 prop 传入；App.vue 从
  `platform.relay.fetchMe()` 拿 `me.is_admin`）
- Active 态：admin 视图打开时按钮加 `.active` 类（背景加深）
- 事件：`@click` emit `toggle-admin` 到 App.vue

### 4.3 App.vue 主区域切换

```vue
<div class="tab-area">
  <TabBar :admin-open="adminViewOpen" :is-admin="isAdmin"
          @toggle-admin="adminViewOpen = !adminViewOpen" ... />
  <AdminPanel v-if="adminViewOpen" />
  <PaneGrid v-else ... />
</div>
```

- 用 `v-if / v-else`（不是 `v-show`）以节省内存 & 避免 admin 与 pane 内容
  同时挂载。**Acceptance criteria**：切回 PaneGrid 时终端 state 无损（xterm
  实例挂载到新 DOM 后 buffer / cursor / scroll 位置恢复）。
- 若实现发现 remount 导致内容丢失，改用 `v-show`（放在实现阶段决定）。
- 点 TabBar 里任一 session tab → `adminViewOpen = false` 自动切回。
- **不清空** tabs / activeTab / URL hash。

### 4.4 SettingsAccount tab

菜单里位置：紧跟 "通用" 之后。

内部 3 块：

1. **邮箱只读展示** — `{{ me.email }}`
2. **修改密码** — 表单：`旧密码 / 新密码 / 确认新密码`。
   - 前端校验：`新密码.length >= 8`；`新密码 === 确认新密码`
   - 提交流程（stepup）：
     1. `POST /api/me/stepup { password: 旧密码 }` → `{ token, expires_in }`
     2. `PUT /api/me/password { new_password }` + `X-Step-Up-Token: <token>`
     3. 成功后 toast "密码已更新"
3. **注销账号（危险区）** — 折叠展开，内容独立于上面的"修改密码" section：
   - 展开后显示两个 input：`当前密码` + `输入你的邮箱以确认`
   - `确认删除` 按钮 `disabled` 直到：`密码非空` **且** `邮箱输入 === me.email`
   - 点击后弹原生 `confirm("此操作不可恢复。")`
   - 确认后：
     1. `POST /api/me/stepup { password: 当前密码 }` → `{ token }`
     2. `DELETE /api/me` + `X-Step-Up-Token: <token>`
     3. 成功后清理 localStorage、跳 `/login.html`

**错误处理表**：

| Case | 处理 |
|---|---|
| 前端校验失败 | inline 红字，不发请求 |
| stepup 401（密码错） | toast "旧密码不正确" |
| PUT/DELETE 401（token 过期） | toast "会话已过期，请重新登录" |
| 429 rate limit | toast "请稍后重试" |
| 网络错误 | toast "网络错误" |

### 4.5 Push 通知加进 SettingsGeneral

`SettingsGeneral.vue` 尾部追加：

```vue
<section v-if="caps.notifications">
  <h3>{{ t('settings.general.pushNotifications.title') }}</h3>
  <label>
    <input type="checkbox" v-model="pushEnabled" @change="onPushToggle">
    {{ t('settings.general.pushNotifications.enable') }}
  </label>
  <button v-if="pushEnabled" @click="onTestPush">
    {{ t('settings.general.pushNotifications.test') }}
  </button>
</section>
```

行为：

- `pushEnabled` initial：查询 `navigator.serviceWorker.controller` +
  `PushManager.getSubscription()`；有订阅则勾选
- `onPushToggle(true)`：
  1. `Notification.requestPermission()` → 若非 `'granted'` → toast "浏览器拒绝了通知权限"
  2. `getPushKey()` → 从 relay 拿 VAPID public key
  3. `enablePushFlow(getPushKey)` — 恢复的 `push-flow.ts` 里
- `onPushToggle(false)`：`disablePushFlow()`
- `onTestPush()`：`testPush()` → relay 通过 push 发一条 "AT Term 测试通知"

**caps.notifications 判定**：
- Wails: `true`（desktop 有系统通知）
- Capacitor: `false`（有自己的推送通道，不走 Web Push）——只 web 生效
- Web: `typeof Notification !== 'undefined'`（v0.3.19 已改）

### 4.6 SettingsDialog tab 可见性

`caps.wailsBindings=false`（web / capacitor）时隐藏：

| Tab | 隐藏原因 |
|---|---|
| Relay | web 由 relay 直接服务，无需配 relay URL；capacitor 用扫码 pair |
| 诊断 | dumps Wails-side env / log paths，web 上是空的 |
| 接收文件 | 客户端本地文件系统能力，浏览器无 |

`SettingsFeishu` **待定**：飞书集成如果是配"当前用户账号跟飞书 bot 绑定"就 web 也保留；如果是"desktop uplink 到 feishu"就 hide。实现阶段 grep `SettingsFeishu.vue` 判定，若不明确则暂时**在 web 上隐藏**（保守）。

### 4.7 版本号显示

- 位置：`SettingsDialog.vue` 底部（tab 内容之后、`Close` 按钮之前），右下角
- 样式：`font-size: 11px; color: var(--text-muted); text-align: right`
- 内容：`AT Term v0.3.19`（本 spec 落地时按当前 tag）
- 数据来源：`platform.system.getAppVersion()`（新加）
  - Wails: `bindings().GetAppVersion()` → Go `main.Version`
  - Capacitor: `'dev'`（capacitor 打包时未注入；未来 work）
  - Web: `await fetchVersion()` from `@webshared/api/version`

### 4.8 Admin 视图内部

`AdminPanel.vue` 结构：

```vue
<template>
  <div class="admin-panel">
    <div class="admin-tabs">
      <button v-for="tab in tabs" :key="tab.key"
              @click="active = tab.key"
              :class="{ active: active === tab.key }"
              :data-test="`admin-tab-${tab.key}`">
        {{ tab.label }}
      </button>
    </div>
    <div class="admin-body">
      <Invitations  v-if="active === 'invitations'" />
      <Users        v-if="active === 'users'" />
      <Config       v-if="active === 'config'" />
      <FeishuConfig v-if="active === 'feishu'" />
    </div>
  </div>
</template>
```

- Tab 状态**不持久化**：下次打开 admin 视图默认到 "邀请"
- 每个 tab 组件的 API 调用（`apiFetch` 到 `/api/admin/*`）无需改，`@webshared`
  路径已存在
- i18n：admin 组件用的 `admin.*` 命名空间在 `web/src/shared/i18n/messages/*`
  已存在，desktop/frontend 通过 `@shared` alias 能 resolve；实现阶段验证
  key 是否同名

### 4.9 权限失效场景

- 用户 admin 权限被撤销（`me.is_admin` 从 `true` → `false`）：
  - `isAdmin` ref 变 false → TabBar admin 按钮消失
  - 若此时 `adminViewOpen=true`：`watch(isAdmin, (v) => { if (!v) adminViewOpen = false })` 自动切回 PaneGrid
- 未登录用户：`me` fetch 失败 → `isAdmin=false` → 按钮不显示

### 4.10 Recovery snapshot 兼容

现有 `useRecoverySnapshot` 记录 tabs / panes，**不涉及** `adminViewOpen` 状态。
admin 视图打开状态不持久化——刷新页面回到默认（PaneGrid 显示）。这是刻意
的：admin 是"临时进去看东西"的模式，不该常驻。

## 5. 测试

### 5.1 单元测试（vitest）

新写：
- `SettingsAccount.test.ts`：改密码前端校验、注销输入邮箱不匹配 disable 按钮
- `admin/AdminPanel.test.ts`：tab 切换、初始 tab、无权限时不显示（虽然渲染由父组件控制，这里做防御）
- `TabBar.test.ts` 追加：`isAdmin=true` 显示按钮 / `isAdmin=false` 隐藏 / active 类正确
- `App.test.ts` 追加：`adminViewOpen=true` 时 PaneGrid 不渲染 & AdminPanel 渲染；点 session tab 自动关闭

复用：既有 `TaskSidebar` / `SettingsDialog` 测试无需改

### 5.2 手动 smoke（v0.3.20 后跑）

- 打开 web 主入口
- SettingsDialog 底部显示 `AT Term v0.3.20`
- 账号 tab 存在；改密码 stepup 走通；注销 二确 走通
- 通用 tab 底部 Push 通知开关，勾选 → 浏览器权限弹窗 → 订阅成功；测试按钮出 通知
- Relay / 诊断 / 接收文件 tab 在 web 上不显示
- 以管理员账号：TabBar 右侧看到 admin 按钮，点击主区域切到 AdminPanel（4 tab），
  点 session tab 切回 PaneGrid 终端 state 完好
- 非管理员账号：无 admin 按钮
- 访问 `/admin.html` → 404（已删）

## 6. 迁移与回滚

### 6.1 PR 拆分（3 个）

1. **PR-A · 恢复 3 功能 + 版本号 + tab 可见性**
   - 恢复 `web/src/shared/api/{me,push,push-flow,stepup}.ts` exports
   - 新 `SettingsAccount.vue`
   - `SettingsGeneral.vue` 加 Push 块
   - `platform.system.getAppVersion` 三端实现 + `GetAppVersion` Wails binding + `desktop/app.go` 方法
   - `SettingsDialog.vue` 加账号 tab、版本号显示、Relay/诊断/接收文件 gate 在 `caps.wailsBindings`；飞书按 §4.6 判定（暂时隐藏于 web）
   - 单元测试
2. **PR-B · Admin 视图**
   - 4 admin 组件拷贝到 `desktop/frontend/src/components/admin/`
   - 新 `AdminPanel.vue`
   - `TabBar.vue` 加 admin 按钮
   - `App.vue` 主区域切换
   - 单元测试
3. **PR-C · 清理 /admin.html**
   - 删 `web/admin.html`、`web/src/admin/`
   - `web/vite.config.ts` 去掉 admin input
   - 相关 test 清理
   - `docs/spec/architecture.md` 更新

**A 与 B 相互独立可并行**；C 依赖 B（若 B 没 merge，删 /admin.html 会让管理员无入口）。

### 6.2 回滚

- 3 PR 各自可 revert；PR-C revert 后 `/admin.html` 依然工作（因为文件在 git 历史）
- 若 stepup 后端有变动导致 PR-A 里改密码 flow 挂：单独把 `SettingsAccount.vue` 里的改密码块隐藏（保留 UI 结构，disabled）

### 6.3 破坏性变更

- `/admin.html` 直连 URL 失效 — 已声明
- Admin bookmark 需重建（用户重新去主页 + 点 admin 按钮）
- 无其它破坏性变更（其它 3 恢复功能是重现，non-breaking add）

## 7. 关键路径速览

- Admin 视图入口：`TabBar.vue::admin button` → `App.vue::adminViewOpen`
- 主区域切换：`App.vue tab-area v-if/v-else`
- 版本号数据源：`platform.system.getAppVersion()`
- 密码 / 注销 stepup：`web/src/shared/api/stepup.ts::requestStepUpToken` (恢复) → `X-Step-Up-Token` header
- Push 通知启用：`web/src/shared/api/push-flow.ts::enablePushFlow` (恢复) →
  navigator.serviceWorker + PushManager
- Web tab 可见性：`v-if="caps.wailsBindings"`（v0.3.19 已定义）
