# Web 主入口布局对齐 desktop — design

Date: 2026-07-27
Status: Drafted — awaiting user review before plan.

## 0. Summary

Web 主入口 (`web/src/main`) 目前是"两态"简单路由：`SessionList`（host 分组卡片）
或 `TerminalView`（单会话全屏）。桌面前端 (`desktop/frontend`) 已经是完整的
`TabBar + TaskSidebar + PaneSplit` 模型，具备 tab 拖拽、pane 分裂、group-by
(host/state)、collapse、pin 组、SessionRowMenu 等能力，且 mobile Capacitor 完全
复用了这套代码。

本设计让 web 主入口的 UI 布局、交互模型与状态管理与 desktop/mobile 完全对齐，
路径是：**不搬组件、不建 shared 包**，而是给 `desktop/frontend` 新增第三个
platform 实现 `platform/web.ts` + 入口 `main.web.ts`，让 web 的 `main.html`
挂到 desktop/frontend 上。session 置顶信息 (`PinnedSessionIDs`) 作为其中一小
块，通过接入既有 `PrefsSyncEngine`（第 7 个 synced key）实现全端同步。

Non-main 入口 (`login.html`, `signup.html`, `setup.html`, `admin.html`,
`firstrun.html`) 保留，继续由 `web/src/shared` 支撑。`web/src/settings/*`
删除——settings 内嵌进主入口的 `SettingsDialog`。

## 1. Goals

- Web 主入口的布局、组件、交互与 desktop 完全一致：TabBar + TaskSidebar +
  PaneSplit + SessionRowMenu + pin 组 + collapse + group-by。
- Tabs / panes 持久化到 localStorage，浏览器刷新 / 重开能恢复。
- URL `#/session/<sid>` 直链继续支持；hash ↔ active pane 双向同步。
- Pin 全端同步：desktop / mobile / web 上的 pin 集合通过 relay
  `/api/me/preferences` 实现 LWW 收敛；任一端都能 pin/unpin。
- 移动 web (viewport width < 768px) 自适应：TaskSidebar 变覆盖式 drawer，
  顶部汉堡菜单触发；同一改动惠及 desktop 窗口拖窄。
- 三端 (desktop/mobile/web) 共用同一份 `App.vue` 及子组件；platform 差异
  通过既有 `Platform` interface 隔离。

## 2. Non-goals

- **不搬组件到独立 shared 包**：等第 4 个前端出现再抽（YAGNI）。
- **不做 feature flag 灰度**：web 主入口切换是一次性动作，旧文件直接删。
- **不做多浏览器窗口跨窗口同步**：每个窗口独立 tabs（`windowId` 分区），
  未来若需要再加 BroadcastChannel。
- **不做 pin 的 per-item 冲突处理**：沿用 `PrefsSyncEngine` 整 list LWW；两端
  同时改的实际概率极低。
- **不清理 stale pinned session_id**：session 消失后 pin id 保留（跟 desktop
  原有 §4.1 stale-id 语义一致）；session 回来自动重现。
- **不在 web 上支持本地 shell**：`caps.localPty=false`；相关 UI 隐藏。
- **不改 relay 后端**：`user_preferences(user_id, key, value_json, updated_at)`
  已经泛化，pin 只是新 key。
- **不改 non-main 入口** (`login.html` 等)：这些是未登录/管理场景，与 desktop
  内嵌对话框职责不同，保留 web 独有实现。
- **不做 Session 或 URL 携带的置顶元数据（label / note / order）**：pin 就是
  boolean 集合。

## 3. 现状

### 3.1 Web 主入口现状

- `web/src/main/App.vue` (228 行)：`inSession=false` 展示 `SessionList`，
  `inSession=true` 展示 `TerminalView`（占满）+ 顶部返回按钮。
- `web/src/main/components/`：`SessionList.vue` (248 行 host 分组卡片) /
  `TerminalView.vue` (350 行 xterm) / `ShortcutBar.vue` / `PasteFallback.vue` /
  `PasteImagePreviewHost.vue` / `PasteFilePreviewHost.vue` /
  `InstallHint.vue`；总计 7 个组件 1404 行。
- 无 TabBar / TaskSidebar / SessionRowMenu / pin group / collapse / group-by。
- URL routing：`#/session/<sid>[?focus=input][&permission=view]`，`parseSessionRoute` /
  `formatSessionRoute` 在 `web/src/main/lib/sessionRoute.ts`。
- 独立 HTML 入口：`login.html`, `signup.html`, `setup.html`, `admin.html`,
  `firstrun.html`, `settings.html`（每个入口独立 `main.ts` + Vue app）。
- `web/src/shared/`：`api/client` (`apiFetch` + 401 → `/login.html`),
  `theme`, `i18n`, `connhealth`, `ws`, `sync/prefsSync` (6 synced keys),
  `components/{Topbar, ConnHealthPill, ConnHealthDrawer}`。

### 3.2 Desktop 前端现状

- `desktop/frontend/src/App.vue` (1667 行) + `TaskSidebar.vue` (471) +
  `TaskGroupedList.vue` (618) + `SessionRowMenu.vue` (147) + `TabBar`,
  `PaneSplit`, 多组 composables (pin/collapse/groupby/preset/recovery)。
- `Platform` interface + 工厂注入：`platform/index.ts::initPlatform(factory)`；
  两套入口 `main.ts`（desktop）与 `main.capacitor.ts`（mobile）；两套 platform
  实现 `platform/wails.ts` (198) 与 `platform/capacitor.ts` (640)。
- `Capabilities`: `localPty` 未定义（当前靠 `sessions.newSession !== undefined`
  隐式判定 mobile）、`autoUpdate`, `pluginHost`, `windowControls`,
  `systemClipboard`, `notifications`, `fileDialog`；已在 UI 里做过分支
  (`SettingsDialog.vue::v-if="caps.pluginHost"` 等)。
- Session 置顶 `useSessionPins.ts` (125 行)：module-level `pinnedIds` +
  Wails RPC `Get/SetPinnedSessionIds` + 300ms debounce persist。
- Recovery：`useRecoverySnapshot::executeRestore` 已经把 remote-pane 与
  local-pane 分支拆好 (`if (snap.remote && snap.session_id) { ... }
  else { newSession(...) }`)；web 只会走 remote 分支。
- Tabs 内部状态无 URL routing 概念（recovery 靠 `recovery.json` via Wails）。

### 3.3 现有全端同步

- `internal/prefssync/sync.go`：`syncedKeys` = `locale_preference`,
  `quick_templates`, `notifications_enabled`, `ai_notifications_only`,
  `command_notify_threshold_seconds`, `shell_integration_enabled`（6 项）。
- `PrefsSyncEngine`：per-key LWW，`Pull` / `MarkDirty` / `Push` /
  `SeedFromLocal`。
- Desktop 端 `appConfigAdapter` 桥接 `configStore`；HTTP relay client 命中
  `/api/me/preferences` GET/PUT。
- TS 镜像：`desktop/frontend/src/lib/prefsSync.ts`（Wails 与 Capacitor 共用）+
  `web/src/shared/sync/prefsSync.ts`（web-only）。
- Capacitor 触发：`prefsSync.capacitor.ts::notifyLocalChange(key)` 内部
  `markDirty + push`。
- Desktop 触发：Go 侧 setter 尾部 `a.markPrefDirtyAndPush(key)`；push 成功
  `EventsEmit(a.ctx, "prefs:changed")`；前端 `main.ts::EventsOn('prefs:changed')`
  已订阅。

### 3.4 本地 pane 的 session_id 在 relay 上有效

Desktop uplink `writeAnnounce` (`desktop/uplink.go:580`) 会把本机所有 pane
的 `SessionInfo`（包含 session_id、host、cwd 等）ANNOUNCE 到 relay。其它端
拉 session list 时看得到这些"来自桌面 host X"的 session。故 pin id
即使来自"另一台桌面的本地 pane"，只要那台桌面 uplink 在线，其它端就能匹配
并渲染。

## 4. 设计

### 4.1 总体架构

新增 `desktop/frontend/src/platform/web.ts`（web platform 实现，~300 行）
和 `desktop/frontend/src/main.web.ts`（web 入口，~30 行）。修改
`web/vite.config.ts`：`main.html` 的入口指向
`../../desktop/frontend/src/main.web.ts`。

**删除**：`web/src/main/{App,SessionList,TerminalView,ShortcutBar,InstallHint,
PasteFallback,PasteImagePreviewHost,PasteFilePreviewHost}.vue` +
`web/src/main/main.ts` + `web/src/main/lib/` + `web/src/settings/*`。

**保留**：`web/src/{login,signup,setup,admin,firstrun}/*` 全部独立 HTML 入口
+ `web/src/shared/*`（`api/client`, `theme`, `i18n`, `connhealth`, `ws`,
`sync/prefsSync`, `components/{Topbar, ConnHealthPill, ConnHealthDrawer}`
继续被 non-main 入口使用）。

### 4.2 `platform/web.ts` 详细接口

#### 4.2.1 `caps: Capabilities`

```ts
{
  localPty: false,          // 浏览器无 PTY（本 spec 新增字段）
  autoUpdate: false,        // 页面刷新即"更新"
  pluginHost: false,        // 无 Wails plugin host
  windowControls: false,    // 无系统窗口控制
  systemClipboard: true,    // navigator.clipboard
  notifications: true,      // Notification API + Web Push（web 已有）
  fileDialog: true,         // <input type=file>
}
```

`Capabilities.localPty` 是本 spec 新增字段；filled `true` for Wails,
`false` for Capacitor 与 Web。UI 层的
"新建本地 shell" / "在此 pane 开本地 shell" 从
`sessions.newSession != null` 判断改成 `caps.localPty`。

#### 4.2.2 `relay: RelayBridge`

- `load()` / `save()` / `clear()` → localStorage 键 `atterm.relay.session`（web
  现有键，语义不变）
- `fetchMe()` → GET `/api/me`（via `@shared/api/client::apiFetch`）
- `consumePairing()` → POST `/api/pair/consume`
- `login()` / `logout()` / `loadSavedPassword()` → **不实现**（web 未登录
  态由 `login.html` 独立处理）
- `setUplinkPaused()` → **不实现**（web 不是 uplink 源）

#### 4.2.3 `sessions: SessionBridge`

- `newSession` → **省略字段**（同 mobile）
- `closeSession(sid)` → DELETE `/api/sessions/:sid`
- `listShells()` → 返回 `[]`
- `listRemoteSessions()` → GET `/api/sessions`（映射为 `RemoteSession[]`）
- `markSessionsSeen()` → POST `/api/sessions/seen`
- `getPins()` / `setPins(ids)` → **本 spec 新增字段**（见 §4.5）

#### 4.2.4 `system: SystemBridge`

- `showNotification()` → `new Notification(title, {body})` + web push 回退
  (web 已有实现，从 `web/src/shared` 迁进 platform)
- `getClipboardPaste()` → `navigator.clipboard.readText/read`
- `openExternalURL(url)` → `window.open(url, '_blank', 'noopener')`
- `getEnvironment()` → `{ buildType: 'web', platform: navigator.userAgent,
  arch: '' }`
- `pickLogFilePath` / `windowMinimize` / … / `quit` → **不实现**

#### 4.2.5 `events: EventBus`

极小 EventEmitter：`Map<string, Set<handler>>` + `on(name, fn) → () => off()` +
`emit(name, data)`。本 spec 唯一新增的事件名是 `prefs:changed`（其它跨组件
事件本 spec 不定义，各 platform 若已通过别的通道传（如 Wails
`EventsOn`）继续维持）。

#### 4.2.6 `templates` / `auxKeys`

localStorage：`atterm.templates.value` / `atterm.aux_keys.value`
（与 mobile Capacitor 实现一致）。

`templates` 借道 `PrefsSyncEngine`：写入 localStorage 后
`notifyLocalChange('quick_templates')` 触发 push。

#### 4.2.7 `updater` / `pluginHost`

不定义（返回 undefined）；UI 通过 `caps.autoUpdate=false` /
`caps.pluginHost=false` 隐藏所有相关入口。

### 4.3 Tabs / Panes / URL routing

#### 4.3.1 存储

- Key：`atterm.web.tabs.v1.<windowId>`（version 前缀便于 schema 升级）
- `<windowId>`：每浏览器窗口 boot 时生成 UUID 存 `sessionStorage`（同 tab 页
  刷新保留、跨窗口不共享）
- Schema：
  ```
  {
    tabs: [
      { id, layout: 'single' | 'v-split' | 'h-split' | ...,
        active_pane_idx,
        col_ratio?, row_ratio?,
        panes: [{ slot, session_id, host_id?, sealed? }, ...] }
    ],
    active_tab_id: string
  }
  ```
- 只存 remote panes；web 无 local pane
- Debounce：tabs/panes 变化后 300ms 写入（与 `useSessionPins` 同节奏）

#### 4.3.2 Boot 流程

1. `main.web.ts` mount `App.vue` 前：读 localStorage 拿快照
2. `App.vue::onMounted`：`platform.sessions.listRemoteSessions()` 拉一次
3. 对快照里每个 pane：
   - 匹配到远端 session → 正常渲染（自动重连 xterm）
   - 匹配不到 → 保留 tab / pane 结构，pane 渲染为 `disconnected` 占位
     （沿用 desktop `lastSeenInfo`）
4. 快照为空 & URL hash 为空 → 显示 sidebar 全屏空状态（同 desktop "no active
   tab"）
5. 快照为空 & URL hash 有 `#/session/<sid>` → 新建单 pane tab 指向该 sid

#### 4.3.3 URL routing (`#/session/<sid>[?focus=input][&permission=view]`)

- **hash → tabs**：
  - 已有 tab 的 active pane 指向该 sid → 激活该 tab
  - 已有 tab 的非 active pane 指向该 sid → 激活该 tab + 切到那个 pane
  - 无匹配 → 新建单 pane tab 指向该 sid
  - `focus=input` / `permission=view` 沿用 web 现有 `parseSessionRouteAction`
- **tabs → hash**：
  - Active tab / active pane 变化 → `history.replaceState({}, '',
    formatSessionRoute(currentSid))`（不加历史条目）
  - 无 active pane → `location.hash = '#/'`
- **地址栏手改 hash**：`hashchange` 事件 → "hash → tabs" 流程

#### 4.3.4 多浏览器窗口

- **各自独立**：每窗口有独立 `windowId`，独立 tab set
- 不做 BroadcastChannel 跨窗口同步
- 若用户在两个窗口关闭同一个 session tab，各自独立处理，不互相通知

#### 4.3.5 Recovery 语义差异

- 不 fork 任何东西：web 无本地 pane
- Desktop `executeRestore` 的 `snap.remote && snap.session_id` 分支即 web
  需要的全部路径
- Pin migration (`pins.rename(oldSid, newSid)`) 在 web 上永不触发（local pane
  才换 sid）
- 若快照里意外出现 `snap.remote===false` 条目（例如从 desktop 导入的旧数据）：
  因 `caps.localPty=false` 直接跳过，视为 disconnected 占位

#### 4.3.6 沿用 desktop 组件（零改动）

- TabBar 拖拽重排（PR bedbf0f/161fade）
- Tab 关闭、pane split（水平/垂直）、pane 关闭、pane focus
- TaskSidebar group-by (host/state) / collapse / pin group / SessionRowMenu
  右键置顶
- 拖拽 sidebar 宽度、折叠 sidebar 到 rail

#### 4.3.7 因 `caps.localPty=false` 隐藏

- "新建本地 shell" tab（TabBar `+` 菜单中相关子项；保留"新建远程 tab"）
- Pane 分裂后"在此 pane 开本地 shell"选项（保留"选远端 session"选项）

#### 4.3.8 窄屏 drawer 化

- Viewport width < 768px：`TaskSidebar` 变覆盖式 drawer（默认收起，顶部
  汉堡菜单展开）
- 同一改动惠及 desktop 窗口拖窄的场景
- 具体交互（图标位置、动画曲线、backdrop 点击关）在实现阶段决定；spec 锁
  "窄屏 drawer 化 + 汉堡菜单" 这一约束

### 4.4 移动端 (Capacitor) 影响

**澄清**：Capacitor 挂载的是 `desktop/frontend/src/mobile/MobileApp.vue`
（153 行独立 mobile shell + `Mobile*` 组件系列），**不是** desktop 的
`App.vue`。这是 mobile iOS app 的刻意选择（terminal UX 与 desktop 差异
大）。所以本 spec 的 "web 布局对齐 desktop" 影响面：

- **Web**：挂 `App.vue`（desktop shell）
- **Desktop**：继续挂 `App.vue`（不变）
- **Mobile Capacitor**：继续挂 `MobileApp.vue`（不变）
- 窄屏 drawer 化（§4.3.8）是 `App.vue::TaskSidebar` 的责任；受益的是
  **web 在窄浏览器视口**（含手机浏览器访问 web 页面）+ desktop 拖窄窗口，
  **不影响** Capacitor mobile app（那有独立 mobile UI）
- `useSessionPins` platform 化后：
  - Wails: 走 `bindings().Get/SetPinnedSessionIds`（不变）
  - Web: 走 localStorage + prefsSync
  - Capacitor: 若 `MobileApp` 需要展示 pin 组，同样接入（但 `MobileSessionList`
    目前不使用 pin，需要单独设计入口；本 spec 不覆盖 mobile pin UI，
    仅保证 Capacitor 上 pin 的**存储/同步链路**可工作，pin 管理在 desktop 或
    web 上操作后同步到 mobile 是 no-op（mobile 端不渲染 pin 组）——除非有独立
    需求扩展 MobileSessionList）

### 4.5 Pin 全端同步

#### 4.5.1 Go 侧

- **`internal/prefssync/sync.go`**：`syncedKeys` 加 `"pinned_session_ids"`
- **`desktop/prefssync_adapter.go`**：
  - `ReadValue` 加 `case "pinned_session_ids":
    b, _ := json.Marshal(c.PinnedSessionIDs); return b, true`
  - `WriteValue` 加 `case "pinned_session_ids": var ids []string; if err :=
    json.Unmarshal(value, &ids); err != nil { return err }; c.PinnedSessionIDs
    = ids`
- **`desktop/app.go`**：
  - `isPrefCustomized` 加 `case "pinned_session_ids": return
    len(c.PinnedSessionIDs) > 0`
  - `SetPinnedSessionIds` 尾部调 `a.markPrefDirtyAndPush("pinned_session_ids")`
- **Relay** 零改动

#### 4.5.2 `SessionBridge` 新增

```ts
interface SessionBridge {
  // ...existing...
  getPins(): Promise<string[]>
  setPins(ids: string[]): Promise<void>
}
```

Wails 实现：`bindings().GetPinnedSessionIds()` /
`bindings().SetPinnedSessionIds(ids)`

Capacitor / Web 实现：
- `getPins()` → 读 localStorage `atterm.pinned_session_ids.value`（`[]` 默认）
- `setPins(ids)` → 写 localStorage + `notifyLocalChange('pinned_session_ids')`
  触发 push
- Pull 端通知：在 `PrefsSyncEngine.pull()` 完成、写完本地 value 与 meta 之后，
  由**调用方**（`main.web.ts` / `main.capacitor.ts` 里现有的 `void prefsSync.
  pull().catch(...)`）在 promise then 里追加 `platform.events.emit('prefs:
  changed')`。不改 `PrefsSyncEngine` 本体（保持 sync 引擎与事件系统解耦）。

#### 4.5.3 前端 `useSessionPins.ts`

- 底层 `getPinnedSessionIds` / `setPinnedSessionIds` 从 `../lib/api`
  改成 `platform.sessions.getPins/setPins`
- 新增内部函数 `reloadFromStore()`（把 `loadOnce` 里从 store 拉数据的逻辑
  抽出、绕过 `loaded` guard），export 成 `useSessionPins().reload()`
- `useSessionPins()` 首次调用时，除了 `loadOnce()`，还挂一个
  `platform.events.on('prefs:changed', reloadFromStore)`；模块 unmount 场景
  不存在（module-level ref 生命周期同 app），返回的 off 函数丢弃即可
- Wails 侧：`main.ts::EventsOn('prefs:changed')` 里已把这个 Go event
  转发到别处，本 spec 追加 `platform.events.emit('prefs:changed')`
- Capacitor / Web 侧：`main.capacitor.ts` / `main.web.ts` 里 `prefsSync.
  pull()` 与 `push()` 的 promise then 里 `platform.events.emit('prefs:
  changed')`

#### 4.5.4 冲突与 stale-id

- **冲突**：整 list LWW（沿用 `PrefsSyncEngine`）；两端并发 pin 不同 session
  的实际概率极低
- **Stale-id**：pin 了一个"另一台桌面 A 的本地 pane 的 sid"，但 A 离线时其他
  端匹配不到 → 显示为空 pinned group（沿用 §4.1 stale-id 语义）；A 上线后
  自动重现

### 4.6 需要在 desktop 端做的小改造

为让 caps 显式化 "无本地 PTY"，同时把 events 桥接到 platform，涉及：

- `platform/types.ts::Capabilities` 新增 `localPty: boolean`
- `platform/wails.ts` 填 `localPty: true`
- `platform/capacitor.ts` 填 `localPty: false`
- 3-4 处 `sessions.newSession != null` 判断改成 `caps.localPty`：
  - `App.vue` 的"新建本地 tab" 按钮
  - `PaneSplit` 里的 "本地 shell" 选项
  - `useRecoverySnapshot` 里对 `snap.remote===false` pane 的恢复分支入口
    （web 上不该走）
- `main.ts::EventsOn('prefs:changed')` 追加 `platform.events.emit('prefs:changed')`

这些是"显式化 + 桥接"，行为不变。

## 5. 测试

### 5.1 新写

- `desktop/frontend/src/platform/__tests__/web.test.ts`：与
  `capacitor.test.ts` 同规模。覆盖：
  - `caps` 各字段返回值
  - `relay` bridge：`load/save/clear/fetchMe/consumePairing`
  - `sessions` bridge：`closeSession/listRemoteSessions/markSessionsSeen/
    getPins/setPins`
  - `events`：on/emit/off 语义与 unsubscribe 泄漏检查
  - `templates` / `auxKeys` localStorage 读写
- `desktop/frontend/src/lib/webTabsSnapshot.test.ts`：
  - 快照读写：`saveSnapshot(snap)` → `loadSnapshot()` roundtrip
  - Hash routing 双向：`hashToTabs` 三种匹配路径 + `activePaneToHash`
  - Multi-pane matching 与 disconnected 占位
  - `windowId` 分区隔离（两个窗口的 tab set 不互相污染）
- `desktop/frontend/src/composables/useSessionPins.test.ts` 补 case：
  - `prefs:changed` 事件触发 `loadOnce({force: true})`
  - Wails / Capacitor / Web 三种 platform 下的 setPins 行为对齐

### 5.2 复用

- `App.test.ts` / `App.merge.test.ts` / `App.tabReorder.test.ts` /
  `App.closeTabOptimistic.test.ts` 已用 platform mock；在 CI 里额外跑一次
  以 web platform mock 为背景的场景（覆盖 `caps.localPty=false` 下的
  按钮隐藏 / recovery 分支跳过）
- `web/tests/unit/shared/prefsSync.test.ts`：`SYNCED_KEYS` 加
  `'pinned_session_ids'` 后自动扩展

### 5.3 UI 回归项

- **窄屏 drawer**：viewport < 768px 时 sidebar 变 drawer；汉堡菜单可切换；
  drawer 打开时 tab 主区域不横向滚动
- **URL 直链**：书签指向 `#/session/<sid>` 打开时正确激活/新建 tab
- **多窗口独立**：同一浏览器两个窗口的 tabs 集互不影响
- **未登录跳转**：apiFetch 401 → `location.href = '/login.html?next=...'`
  路径仍工作（platform.relay 底层就是 `apiFetch`）
- **pin 三端同步**：desktop pin → web 拉到并展示；web pin → desktop 拉到
  并展示；mobile 同理

## 6. 迁移与回滚

### 6.1 PR 拆分（增量安全）

1. **PR 1（Go 侧）**：`syncedKeys` 加 `"pinned_session_ids"`；
   `appConfigAdapter` 加 case；`isPrefCustomized` 加 case；
   `SetPinnedSessionIds` 尾部 `markPrefDirtyAndPush`
2. **PR 2（Capabilities.localPty 显式化）**：`platform/types.ts` +
   `platform/wails.ts` + `platform/capacitor.ts` + 3-4 处 UI 判断切换
3. **PR 3（`useSessionPins` platform 化）**：底层从 `lib/api` 改到
   `platform.sessions.getPins/setPins`；新增 `platform.events.on(
   'prefs:changed')` 订阅；desktop `main.ts` 事件桥接
4. **PR 4（`platform/web.ts` + `main.web.ts`）**：只加文件，无入口切换
5. **PR 5（窄屏 drawer 化）**：`TaskSidebar` 加 responsive drawer 模式；
   同时惠及 desktop 拖窄
6. **PR 6（切换点）**：`web/vite.config.ts` main entry 切换 +
   `web/src/main/*` 删除 + `web/src/settings/*` 删除（settings 走
   desktop SettingsDialog）

PR 1-5 全部是"加"，不改现有 web 主入口行为；只有 PR 6 是唯一切换点。

### 6.2 回滚

PR 6 若上线后发现问题 → revert PR 6 即可，PR 1-5 全部保留可继续运行。

Pin 同步链路本身在 PR 1-3 上线后即工作（desktop/mobile 之间），不依赖 PR 6。

### 6.3 破坏性变更（用户可感知）

- **`settings.html` 直连 URL 失效**：改在主界面 ⚙ 里打开 SettingsDialog
- **修改密码 / 注销账号 / push 通知管理 UI 一并下线**：`web/settings.html`
  是这几项功能唯一的入口，desktop 的 SettingsDialog 本身不包含它们，因此随
  `web/src/settings/*` 删除一并失去（对应 API 封装
  `web/src/shared/api/me.ts::changePassword/deleteMe`、
  `web/src/shared/api/push-flow.ts`、`push.ts::testPush` 也已删除，不再有
  调用方）。需要这些功能的用户可直接调用 `/api/me/{password,delete}` 及
  push 相关端点；如需 UI，后续可在 SettingsDialog 里补回
- **`#/session/<sid>` 直链继续有效**（§4.3.3 保证）
- **localStorage 键新增**：`atterm.web.tabs.v1.<windowId>` +
  `atterm.pinned_session_ids.{value,meta}`（不破坏旧键）
- **旧 web/src/main 用的 localStorage 键**（`atterm.relay.session` 等）沿用
  不变

## 7. 关键路径速览

- Web platform 桥接：`desktop/frontend/src/platform/web.ts` (~300 行)
- Web 入口：`desktop/frontend/src/main.web.ts` (~30 行)
- Vite 配置：`web/vite.config.ts` `main` 入口切换
- Go 同步 key：`internal/prefssync/sync.go::syncedKeys` +
  `desktop/prefssync_adapter.go` case
- Pin 同步：`desktop/frontend/src/composables/useSessionPins.ts` +
  `platform.sessions.getPins/setPins` + `platform.events.on('prefs:changed')`
- Tabs 快照：`desktop/frontend/src/lib/webTabsSnapshot.ts` (新增)
- 窄屏 drawer：`desktop/frontend/src/components/TaskSidebar.vue`（加
  responsive drawer 模式）
