# SettingsDialog 补齐 + 管理员面板内嵌 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** SettingsDialog 补齐改密码/注销/Push 通知；管理员面板从独立 /admin.html 内嵌进主 App.vue 作为主区域视图；SettingsDialog 底部显示版本号；web 上隐藏无意义的 tab。

**Architecture:** 3 PR 分组 (A/B/C)，A 与 B 独立可并行、C 依赖 B。恢复被删的 stepup/push-flow/me API exports；照搬 admin 组件到 desktop/frontend/src/components/admin/；用 `caps.wailsBindings` gate 掉 web 上无意义的 tab；`platform.system.getAppVersion` 三端各自实现。

**Tech Stack:** Vue 3 + TypeScript, Wails Go binding, apiFetch, Vitest.

**Spec:** `docs/superpowers/specs/2026-07-28-settings-admin-inline-design.md`

## Global Constraints

- 用户面向对话中文；code / commit / doc 英文
- 无向后兼容包袱（本项目单用户 —— 见 memory `feedback_no_backward_compat.md`）
- TDD 用在新写代码；纯拷贝任务不做 TDD
- 测试输出 pristine（无 stray warnings 由本次改动引入）
- 每 PR 独立可 merge、独立可回滚
- Admin 组件 API 调用继续用 `apiFetch`（不新增 platform 桥接）
- `SettingsFeishu` 暂时按 `!caps.wailsBindings` gate 隐藏于 web（保守）；若实现阶段判定应保留则在同 PR 里改回

---

## Phase A（PR-A）· 恢复 3 功能 + 版本号 + tab 可见性

### Task A.1: 恢复 `web/src/shared/api/{me,push,push-flow,stepup}.ts` 被删 exports

**Files:**
- Modify: `web/src/shared/api/me.ts` — add `changePassword`, `deleteMe`
- Modify: `web/src/shared/api/push.ts` — add `testPush`
- Create: `web/src/shared/api/push-flow.ts` — `enablePushFlow` + `disablePushFlow`
- Create: `web/src/shared/api/stepup.ts` — `requestStepUpToken`
- Test: `web/tests/unit/shared/api/{me,push,push-flow,stepup}.test.ts`

**Interfaces:**
- Consumes: `apiFetch` from `./client`
- Produces:
  ```ts
  // me.ts
  export function changePassword(newPassword: string, stepUpToken: string): Promise<void>
  export function deleteMe(stepUpToken: string): Promise<void>
  // push.ts
  export function testPush(): Promise<void>
  // push-flow.ts
  export function enablePushFlow(getPushKey: () => Promise<string>): Promise<void>
  export function disablePushFlow(): Promise<void>
  // stepup.ts
  export function requestStepUpToken(password: string): Promise<{ token: string; expires_in: number }>
  ```

- [ ] **Step 1: 从历史提交拷回**

Run: `git show 0a49740^:web/src/shared/api/me.ts > /tmp/me.old.ts` (peek at deleted funcs)

Then for each deleted export, copy the function body (+ imports it needs) into current `web/src/shared/api/me.ts`. Same for `push.ts`. `push-flow.ts` and `stepup.ts` were whole files — copy them wholesale:

```bash
git show 0a49740^:web/src/shared/api/push-flow.ts > web/src/shared/api/push-flow.ts
git show 0a49740^:web/src/shared/api/stepup.ts > web/src/shared/api/stepup.ts
```

For me.ts / push.ts, use Edit tool to splice back the deleted functions verbatim.

- [ ] **Step 2: 恢复对应 tests**

```bash
git show 0a49740^:web/tests/unit/shared/api/me.test.ts > web/tests/unit/shared/api/me.test.ts
git show 0a49740^:web/tests/unit/shared/api/push-flow.test.ts > web/tests/unit/shared/api/push-flow.test.ts
```

If test files already exist (partial), merge instead of overwrite.

- [ ] **Step 3: Run tests**

Run: `cd web && npm test -- shared/api`
Expected: all pass

- [ ] **Step 4: Commit**

```bash
git add web/src/shared/api/*.ts web/tests/unit/shared/api/*.test.ts
git commit -m "feat(web-api): restore changePassword/deleteMe/testPush/pushFlow/stepup exports"
```

---

### Task A.2: `platform.system.getAppVersion` 三端

**Files:**
- Modify: `desktop/frontend/src/platform/types.ts::SystemBridge` — add `getAppVersion(): Promise<string>`
- Modify: `desktop/frontend/src/platform/wails.ts` — impl via `bindings().GetAppVersion()`
- Modify: `desktop/frontend/src/platform/capacitor.ts` — impl `async () => 'dev'`
- Modify: `desktop/frontend/src/platform/web.ts` — impl via `fetchVersion()` from `@webshared/api/version`
- Modify: `desktop/frontend/src/platform/__tests__/{wails,capacitor,web}.test.ts` — add coverage
- Modify: `desktop/frontend/src/platform/__tests__/_fakePlatform.ts` — mock
- Modify: `desktop/app.go` — add `GetAppVersion() string { return Version }`
- Modify: `desktop/frontend/src/lib/api.ts` — add TS wrapper (bindings type)

**Interfaces:**
- Consumes: `Version` (Go global), `fetchVersion` (from `@webshared/api/version`)
- Produces: `platform.system.getAppVersion` returns a version string like `"v0.3.19"` or `"dev"`

- [ ] **Step 1: Add Go method**

In `desktop/app.go`:
```go
// GetAppVersion returns the app version injected at build time.
// Empty / "dev" for unbuilt dev runs.
func (a *App) GetAppVersion() string {
    return Version
}
```

- [ ] **Step 2: Regenerate Wails bindings**

Run: `cd desktop && wails generate module 2>&1 | tail -5` (or the project's normal binding regen command; if unclear, manually add to `desktop/frontend/wailsjs/go/main/App.d.ts` + `App.js`)

- [ ] **Step 3: Add to SystemBridge interface**

`desktop/frontend/src/platform/types.ts`:
```ts
interface SystemBridge {
  // ...existing
  getAppVersion(): Promise<string>
}
```

- [ ] **Step 4: Wails impl**

`desktop/frontend/src/platform/wails.ts`:
```ts
getAppVersion: async () => {
  const { GetAppVersion } = await import('../../wailsjs/go/main/App')
  return GetAppVersion()
}
```

- [ ] **Step 5: Capacitor impl**

`desktop/frontend/src/platform/capacitor.ts`:
```ts
getAppVersion: async () => 'dev'
```

- [ ] **Step 6: Web impl**

`desktop/frontend/src/platform/web.ts`:
```ts
getAppVersion: async () => {
  const { fetchVersion } = await import('@webshared/api/version')
  return fetchVersion()
}
```

- [ ] **Step 7: Fake + tests**

Update `_fakePlatform.ts` mock (`getAppVersion: vi.fn().mockResolvedValue('dev')`) and add per-platform tests asserting delegation.

- [ ] **Step 8: Run tests**

Run: `cd desktop/frontend && npm test`
Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git commit -m "feat(platform): SystemBridge.getAppVersion across wails/capacitor/web"
```

---

### Task A.3: SettingsAccount tab

**Files:**
- Create: `desktop/frontend/src/components/SettingsAccount.vue`
- Create: `desktop/frontend/src/components/SettingsAccount.test.ts`
- Modify: `desktop/frontend/src/i18n/messages/{en,zh-CN}.ts` — add `settings.account.*` keys

**Interfaces:**
- Consumes: `changePassword`, `deleteMe`, `requestStepUpToken`, `me` (via `platform.relay.fetchMe`)
- Produces: `<SettingsAccount>` Vue component with 3 sections (email display, change password, danger zone)

- [ ] **Step 1: Write failing test scaffold**

`SettingsAccount.test.ts` — assertions:
- Renders me.email as text
- Change password: submit disabled until 3 fields valid (length 8+, match)
- Danger zone: 确认删除 button disabled until password non-empty AND email exact match

- [ ] **Step 2: Run tests — expect FAIL**

- [ ] **Step 3: Implement component**

Copy structural pattern from history:
```bash
git show 0a49740^:web/src/settings/tabs/ChangePassword.vue > /tmp/oldChangePassword.vue
git show 0a49740^:web/src/settings/tabs/DangerZone.vue > /tmp/oldDangerZone.vue
```

Combine into single `SettingsAccount.vue` with sections. Use existing SettingsDialog child style (see `SettingsGeneral.vue` for section pattern). Wire to restored API exports from Task A.1 + stepup flow.

Use `platform.relay.fetchMe()` for `me.email` (already called elsewhere in App.vue — pass as prop or refetch).

- [ ] **Step 4: Add i18n keys**

`en.ts` under `settings.account`:
```
title: 'Account'
email: 'Email'
changePassword: { title: 'Change Password', oldPassword: 'Current password', newPassword: 'New password', confirm: 'Confirm new password', submit: 'Update', successToast: 'Password updated' }
dangerZone: { title: 'Danger Zone', warning: 'Deleting your account is permanent.', currentPassword: 'Current password', typeEmail: 'Type your email to confirm', delete: 'Delete account' }
```

Mirror in `zh-CN.ts`.

- [ ] **Step 5: Run tests — expect PASS**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(settings): SettingsAccount tab (change password + delete account)"
```

---

### Task A.4: SettingsGeneral 加 Push 通知块

**Files:**
- Modify: `desktop/frontend/src/components/SettingsGeneral.vue` — append Push section
- Modify: `desktop/frontend/src/components/SettingsGeneral.test.ts` (if exists) or `LogViewerDialog.test.ts` pattern

**Interfaces:**
- Consumes: `enablePushFlow`, `disablePushFlow`, `testPush`, `getPushKey` (already exists), `caps.notifications`
- Produces: settings general has a "Push 通知" section visible when `caps.notifications`

- [ ] **Step 1: Failing test**

Assert: with `caps.notifications=true`, section renders; with false, not; checkbox toggles trigger flow calls.

- [ ] **Step 2: Implement**

Append `<section v-if="caps.notifications">` to `SettingsGeneral.vue`:
- Compute `pushEnabled` initial from `navigator.serviceWorker.controller?.pushManager?.getSubscription()`
- Wire toggle to enable/disablePushFlow
- Wire "test" button to testPush

Mimic UI style from history's `Notifications.vue`:
```bash
git show 0a49740^:web/src/settings/tabs/Notifications.vue > /tmp/oldNotifications.vue
```

Add i18n `settings.general.pushNotifications.*` keys.

- [ ] **Step 3: Tests pass + Commit**

```bash
git commit -m "feat(settings): push notification section in general tab"
```

---

### Task A.5: SettingsDialog 加账号 tab + 版本号 + hide tabs

**Files:**
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Modify: `desktop/frontend/src/components/SettingsDialog.test.ts` (if exists)

**Interfaces:**
- Consumes: `SettingsAccount`, `platform.system.getAppVersion`, `caps.wailsBindings`
- Produces:
  - Account tab inserted after "General"
  - Bottom-right version label `AT Term vX.Y.Z`
  - Relay / 诊断 / 接收文件 / 飞书集成 tab items gated on `caps.wailsBindings`

- [ ] **Step 1: Import + register SettingsAccount**

Add import in SettingsDialog.vue, add to tab-key registry, add nav item, add content-area v-if.

- [ ] **Step 2: Gate tabs on caps.wailsBindings**

Add `v-if="caps.wailsBindings"` on nav items and body panels for: Relay, Diagnostics, ReceivedFiles, Feishu.

- [ ] **Step 3: Version label at bottom**

Ref that resolves on mount via `platform.system.getAppVersion()`, display in `.settings-footer` div with formatted label like `AT Term v${version}` (empty/`dev` → `AT Term (dev)`).

- [ ] **Step 4: Adjust default `activeTab` if 'relay' was default and it's now hidden on web**

If web hits and `activeTab='relay'`, fallback to `'general'`.

- [ ] **Step 5: Tests pass + Commit**

```bash
git commit -m "feat(settings-dialog): account tab + version label + hide desktop-only tabs on web"
```

---

**PR-A checkpoint**: Bundle A.1–A.5 into PR titled `feat(settings): restore account + push UI, gate desktop-only tabs, show version`. All 5 commits merged as squash. Verify:
- `cd desktop/frontend && npm test` clean
- `cd web && npm test` clean
- `cd web && npm run build` succeeds

---

## Phase B（PR-B）· Admin 视图

### Task B.1: 拷贝 admin 4 tab 组件

**Files:**
- Create: `desktop/frontend/src/components/admin/Invitations.vue`
- Create: `desktop/frontend/src/components/admin/Users.vue`
- Create: `desktop/frontend/src/components/admin/Config.vue`
- Create: `desktop/frontend/src/components/admin/FeishuConfig.vue`

**Interfaces:**
- Consumes: `apiFetch` from `@webshared/api/client` (already aliased in desktop/frontend/vite.config.ts + tsconfig.json)
- Produces: 4 Vue components rendering their respective admin API data

- [ ] **Step 1: Copy files**

```bash
cp web/src/admin/tabs/Invitations.vue  desktop/frontend/src/components/admin/Invitations.vue
cp web/src/admin/tabs/Users.vue         desktop/frontend/src/components/admin/Users.vue
cp web/src/admin/tabs/Config.vue        desktop/frontend/src/components/admin/Config.vue
cp web/src/admin/tabs/FeishuConfig.vue  desktop/frontend/src/components/admin/FeishuConfig.vue
```

- [ ] **Step 2: Verify imports resolve**

Grep each copied file for `import` statements. If any use relative paths like `../` that no longer resolve (e.g. `../lib/xxx`), change to `@webshared/xxx` or fully-qualified path.

Common imports likely: `@shared/api/client` (apiFetch), `@shared/i18n` (t), `naive-ui` components. All should work via existing aliases.

- [ ] **Step 3: Type check**

Run: `cd desktop/frontend && npx vue-tsc --noEmit`
Expected: clean. Fix any errors specific to the moved files.

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(admin): copy 4 admin tab components into desktop/frontend"
```

---

### Task B.2: AdminPanel container

**Files:**
- Create: `desktop/frontend/src/components/AdminPanel.vue`
- Create: `desktop/frontend/src/components/AdminPanel.test.ts`
- Modify: `desktop/frontend/src/i18n/messages/{en,zh-CN}.ts` — add `admin.title` and tab labels if not already mirrored

**Interfaces:**
- Consumes: `Invitations`, `Users`, `Config`, `FeishuConfig`
- Produces: `<AdminPanel>` renders tab bar + selected tab content

- [ ] **Step 1: Failing test**

Assert:
- Default tab is 'invitations'
- Clicking a tab button switches active
- Each tab renders its child component when active

- [ ] **Step 2: Implement**

Structure per spec §4.8. Use `data-test="admin-tab-xxx"` selectors on buttons.

- [ ] **Step 3: Tests pass + Commit**

```bash
git commit -m "feat(admin): AdminPanel container with 4 tabs"
```

---

### Task B.3: TabBar admin button

**Files:**
- Modify: `desktop/frontend/src/components/TabBar.vue`
- Modify: `desktop/frontend/src/components/__tests__/TabBar.test.ts`

**Interfaces:**
- Consumes: props `isAdmin: boolean`, `adminOpen: boolean`
- Produces: emits `toggle-admin`; button visible only when `isAdmin`

- [ ] **Step 1: Failing tests**

- `isAdmin=true` renders `[data-test="admin-button"]`
- `isAdmin=false` doesn't
- `adminOpen=true` → button has `active` class
- click emits `toggle-admin`

- [ ] **Step 2: Implement**

Add props via `defineProps`; add button in template just left of settings (`⚙`). Icon: use lucide-vue-next `ShieldUser`.

- [ ] **Step 3: Tests pass + Commit**

```bash
git commit -m "feat(tabbar): admin button (visible for admins only)"
```

---

### Task B.4: App.vue 主区域切换 + isAdmin state

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/App.test.ts`

**Interfaces:**
- Consumes: `platform.relay.fetchMe`, `AdminPanel`
- Produces: `adminViewOpen: Ref<boolean>` (false initial); `isAdmin: Ref<boolean>` derived from `me`

- [ ] **Step 1: Failing test**

- `isAdmin=true` + `adminViewOpen=true` → AdminPanel rendered, PaneGrid not
- Clicking a session tab (invoke `gotoTab`) sets `adminViewOpen=false`
- When `isAdmin` transitions `true → false`, `adminViewOpen` auto-resets to `false`

- [ ] **Step 2: Implement**

- Add `adminViewOpen = ref(false)` near tabs state
- Add `isAdmin = computed(() => me.value?.is_admin === true)` — ensure `me` ref already exists (via fetchMe result); if not, add
- Wire `<TabBar :is-admin="isAdmin" :admin-open="adminViewOpen" @toggle-admin="adminViewOpen = !adminViewOpen" />`
- Wrap main area: `<AdminPanel v-if="adminViewOpen" /> <PaneGrid v-else ... />`
- In `gotoTab` (or the equivalent tab-click handler): set `adminViewOpen.value = false`
- Watch `isAdmin`: on transition to false, set `adminViewOpen.value = false`

- [ ] **Step 3: Tests pass + Commit**

```bash
git commit -m "feat(app): admin view swaps main area on TabBar toggle"
```

---

**PR-B checkpoint**: Bundle B.1–B.4 into PR titled `feat(admin): inline admin panel as main-area view`. Verify:
- Tests pass
- Manual: open in dev, log in as admin, click admin button — see 4 tabs

---

## Phase C（PR-C）· 清理 /admin.html

### Task C.1: Delete admin standalone entry

**Files:**
- Delete: `web/admin.html`
- Delete: `web/src/admin/` (recursive)
- Modify: `web/vite.config.ts` — remove `admin` from `rollupOptions.input`

- [ ] **Step 1: Grep for cross-references**

```bash
grep -rn "src/admin/\|/admin.html\|admin/App" web/src/ 2>/dev/null | grep -v "src/admin/"
```

Any hits outside `web/src/admin/` need handling before delete (e.g. `Topbar.vue` still linking to `/admin.html`).

- [ ] **Step 2: Delete + vite.config**

```bash
git rm -r web/src/admin/ web/admin.html
```

Edit `web/vite.config.ts::rollupOptions.input`, remove the `admin` entry line.

- [ ] **Step 3: Clean orphaned tests**

```bash
git rm -rf web/tests/unit/admin/ 2>/dev/null || true
```

- [ ] **Step 4: Build + test**

Run: `cd web && npm run build`
Expected: build succeeds.

Run: `cd web && npm test`
Expected: pass.

- [ ] **Step 5: Update architecture doc**

Edit `docs/spec/architecture.md`: any mention of `web/src/admin/` or "admin as standalone HTML entry" — update to "admin 面板由主 App.vue 内嵌为 AdminPanel 视图（TabBar 按钮切换）".

- [ ] **Step 6: Commit**

```bash
git commit -m "chore(web): drop standalone admin.html; admin lives in AdminPanel"
```

---

**PR-C checkpoint**: Solo PR titled `chore(web): drop admin.html; use AdminPanel`. Merge only after PR-B is merged and live-smoked.

---

## Post-Ship

- Live smoke on `cn.atterm.attson.com` after v0.3.20 tag:
  - SettingsDialog 底部见版本号
  - 账号 tab / 通用 tab 底部 Push 块存在
  - Relay / 诊断 / 接收文件 tab 在 web 不显示
  - admin 账号：TabBar 见 admin 按钮，点击见 4 tab 面板；点 session tab 切回
  - 非 admin 账号：无 admin 按钮
  - `/admin.html` 404
