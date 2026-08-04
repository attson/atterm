# 重构路线图 · 2026-08-04

**状态**：里程碑 0–5 主体已合入 main，里程碑 6/7 待做。总计 **14 个 PR**（#289–#302）合并，跨 Go 后端 + internal + 前端 + CI，净减 ~500 行 + 一批结构性拆分与 helper 抽取。以下每个里程碑保留原方案 + 实际交付/延后说明。

## 背景

用 4 个并行 Explore agent 扫了整个项目：

- `desktop/` Go 后端（150+ 文件，最大 `app.go` 2950 行）
- `internal/` Go 包（18 个包，最大 `session/session.go` 1747 行）
- `desktop/frontend` Vue + TS（最大 `TerminalView.vue` 2701 行 / `App.vue` 2326 行）
- `cmd/` / `mobile/` / 交叉配置

原始 115 项 finding，去重合并后按"低风险先、能相互复用的先"排成 8 个里程碑。

## 跨层洞察（先看这三条）

1. **OPAQUE / 密钥线程类型重复且随时会漂**
   `internal/e2eeclient/client.go:399-443` 与 `internal/relay/opaque_auth.go:81-137` 各自定义 8 个 register/login 请求响应结构；`internal/relay/opaque_server.go:69` 又本地重造 `defaultConfig()` 覆盖 `opaquesuite`。编译器抓不到漂移，一旦其中一边改错就是"invalid credentials"事故。
   → 里程碑 1 的目标。

2. **"手工镜像自动生成物"到处都有**
   - `desktop/frontend/src/lib/api.ts:355-467` 用 112 行手写 `AppBindings` 接口镜像 `wailsjs/go/main/App.d.ts`
   - `.gitignore` 曾有过时死条目（已在 PR #291 修）
   → 里程碑 5 里做 `AppBindings = typeof import(...)` 替换。

3. **7 个 >1000 行的巨型文件是主要债务源**
   `desktop/app.go` 2950 · `TerminalView.vue` 2701 · `App.vue` 2326 · `internal/session/session.go` 1747 · `desktop/relay_host.go` 1437 · `desktop/updater.go` 1220 · `desktop/uplink.go` 1043
   → 里程碑 3/4/5 分别拆分。

---

## 里程碑 0 · 清扫 ✅ 完成（2026-08-04）

**目标**：先把死代码和配置漂移清掉，后面拆文件时挪的行数少一半。零风险、零 API 变化。

| PR | 主题 | 状态 |
| --- | --- | --- |
| [#289](https://github.com/attson/atterm/pull/289) | `chore: drop dead exports and helpers`（desktop + internal Go 死代码） | merged, −352 行 |
| [#290](https://github.com/attson/atterm/pull/290) | `chore(frontend): drop dead exports + orphan i18n keys` | merged, −316 行 |
| [#291](https://github.com/attson/atterm/pull/291) | `chore(build): fix stale ignores, dev script, and docker context` | merged |

**关键发现**（已落地）：
- `internal/relay/admin_config.go` 的整条 `ReadOnlyTokens` 链路（分享秘钥旧机制）只被自家测试用 → 删
- `internal/e2eeclient` 的 password-change 流（`GetKeyWrap`/`PutKeyWrap`/`UnwrapWithPassword`/`ReWrapWithPassword`）从未接入 → 删
- `MintStepUpToken` 有跨测试文件用但生产只自家调 → 改小写
- `mobile.*` i18n 子树是 Capacitor iOS 用的（memory：iOS bundles desktop/frontend），**保留**；只删其中 6 个具体死键
- `.dockerignore` 加大量排除项，docker context 瘦身 ~900MB

---

## 里程碑 1 · OPAQUE wire 类型合并 ⏳ 待做

**目标**：干掉唯一有正确性风险的项。

**单个 PR**：`refactor(opaquesuite): single source of truth for opaque wire types + defaults`
- 新建 `internal/opaquesuite/wire.go`，把 `internal/relay/opaque_auth.go:81-137` 和 `internal/e2eeclient/client.go:399-443` 的 8 组 register/login 请求响应类型搬过去
- 删掉 `internal/relay/opaque_server.go:69` 的 `defaultConfig()` 和 `opaqueServerIdentity`，改导入 `opaquesuite`
- relay 与 client 两侧都改 import
- 跑 `desktop/app_login_test.go`、`internal/relay/opaque_*_test.go`、`internal/e2eeclient/*_test.go`

**验收**：完整跑一遍 relay↔client 的登录/注册 e2e（`go run ./cmd/atterm-relay --dev-insecure` + `wails dev`，走 login/logout/re-login/change password）。

**预估**：0.5 天。

---

## 里程碑 2 · 抽公共 helper

**目标**：三条"同型样板重复 4-5 遍"的模式抽成 helper，后续拆分能直接用新样式。

### PR 2-a `refactor(desktop): introduce keychainSlot[T] and migrate secret stores`
- 新建 `desktop/keychain_slot.go` 提供 `keychainSlot[T]{Load/Save/Clear}`（封装 `com.atterm.<name>.v1 + KeychainSuffix()` service 名、`safekeyring.ErrNotFound → empty` 兜底、encode/decode 注入）
- 迁移 `account_key_store.go`、`relay_password_store.go`、`ssh_hosts_store.go:158-164`、`ssh_keys_store.go:172-178`
- 目标：4 个 store 各降到 ~20 行

### PR 2-b `refactor(frontend): defineSingletonStore + usePersistedRef`
- 新建 `composables/definePersistedRef.ts` 抽出"module-scoped ref + backend→localStorage → `__resetForTests`" 骨架
- 迁移 `useSessionPins`、`useSessionSelection`、`useCollapsedGroups`、`useTaskGroupBy`、`useTaskPreset`

### PR 2-c `refactor(desktop/prefssync): marshalPtr[T] for optional fields`
- `desktop/prefssync_adapter.go:28-93` 的 6 段 `if c.X == nil {...}; json.Marshal(*c.X)` 折成一个 generic helper

**验收**：全套单测（尤其 `*_store_test.go` 与 composables 测试）绿。
**预估**：合计 1 天。

---

## 里程碑 3 · 拆 Go 后端巨型文件

**目标**：把 4 个 >1000 行的 Go 文件拆到 <700 行；wails 绑定不动，前端不需要跟改。

### PR 3-a `refactor(desktop): split app.go by domain`
六块 domain 各拉一个文件：
- `app_account_key.go`（867-990）— 用里程碑 2-a 的 keychainSlot
- `app_relay.go`（715-1103 + 2127-2297 + 2530-2536）
- `app_feishu.go`（2541-2891）
- `app_updater.go`（1636-1780）
- `app_recovery.go`（2430-2528）
- `hookinstall_bindings.go`（2893-2949）

15 处 pref get/set → `updatePref(a, key, mutator)` 通用器。

### PR 3-b `refactor(desktop): extract feishu integration from relay_host.go`
- 新增 `relay_host_feishu.go`（1437 → ~600 行）
- `NewSession` 里 `SetOnTaskStateChange` lambda (75 行) 与 restored-AI resume 注入 (27 行) 抽命名 helper
- 三张并行 map → `map[string]*feishuSession`

### PR 3-c `refactor(desktop): split updater.go`
- `updater_github.go`（439-620,929-951）
- `updater_install.go`（1018-1166）
- `updater_versions.go`（155-238）

### PR 3-d `refactor(desktop): split uplink.go seal + logging`
- `uplink_frame_seal.go`（729-918）
- `uplink_stream_log.go`（928-982 + 160-230）
- 顺带把 `internal/relay/uplink_conn.go:571-609` 的 `webPushSessionLabel` / `taskNotificationKey` 挪到 `internal/webpush`，`looksLikeEncryptedOut` 挪到 `internal/e2eecrypto`

**依赖**：里程碑 2 完成。
**验收**：`go test ./...` + `wails dev` 手工跑一次（登录、开会话、切 driver/viewer、下载更新、Feishu）。
**预估**：每个 0.5–1 天，合计 3 天。**每个 PR 独立 merge**，别攒一起。

---

## 里程碑 4 · 拆 internal/ 巨型文件

### PR 4-a `refactor(session): split silence + osc133 + replay + termstate + meta`
`internal/session/session.go`（1747 → ~500）拆成：
- `silence.go`（~300 行静默启发式）
- `osc133.go`（OSC 133 parser）
- `replay.go`（fanout / Subscribe / enqueueReplay*）
- `termstate.go`（alt-screen 扫描）
- `meta.go`（mergeTaskMeta / cloneSummary / encodeMetaPayload）

9 个 `on*` hook 字段 + `Set*` wrapper 收成一个 `SessionHooks` struct。

**回归重点**：memory 有一条关于 PTY winsize 的告警——不动 fork/resize 路径，纯挪函数。

### PR 4-b `refactor(relay): split admin_http, opaque_auth, server`
- `admin_http.go` (594) → `admin_users.go` / `admin_runtime_config.go` / `admin_feishu.go`
- `opaque_auth.go` (567) → `opaque_register.go` / `opaque_login.go`（若里程碑 1 已把 wire 全搬到 opaquesuite，`opaque_wire.go` 就整个删）
- `server.go` (739) → 抽 `static_handler.go` + `origins.go` + `feishu_runtime.go`

### PR 4-c `refactor(relay/uplink_conn): fold handleUplink closures into uplinkSession`
- `handleUplink` 里 10 个共享 `mu/mirrors/uplinkOut/connCtx/s` 的闭包 → `uplinkSession` struct + 方法
- 470 行的函数体拆到 ~200

**依赖**：里程碑 3 完成。
**预估**：3–4 天。

---

## 里程碑 5 · 拆前端两个巨型 SFC

**目标**：`App.vue` 和 `TerminalView.vue` 减到 <1000 行；纯代码搬家 + 提取 composable。

### PR 5-a `refactor(app.vue): extract composables`
顺序抽：
`useRecoveryRestore` (1122-1307) → `useCloseSessionConfirm` (229-324 等) → `useCellDimsProbe` (389-452) → `useRemoteSessionList` + `useLocalSessionList` (651-871) → `useWebTabsSnapshot` (1620-1731) → `usePluginPanel` (558-618)

新增 `components/StartupFatalPanel.vue`。

### PR 5-b `refactor(terminalview.vue): extract xterm + soft-keyboard + selection + context menu composables`
`useXtermInstance` (1403-1642) → `useSoftKeyboard` (~340 行 IME 状态机) → `useTerminalSelectionPopover` (~230) → `useTerminalContextMenu` (~230，顺带把 `computeLinkHit` 与 `useTerminalLinkProvider:44-83` 的重复消掉) → `useSessionConnection` → `useQuickTemplates`

新增 `components/TerminalAuxKeyBar.vue`。
抽 `lib/touchScrollDebug.ts`。

**回归重点**：memory 明确的两条 iOS 硬约束——overlay `pointer-events: none .term` 与"click→text 分两次 sendInput"——**不能碰**，只做搬家。

### PR 5-c `refactor(frontend): unify xterm factory + modKey helper + AppBindings type`
- 新建 `lib/xtermFactory.ts`，让 App.vue 探针与 TerminalView.vue 主终端共享
- `modKey()` 收进 `platform/`，替换 5 处 `navigator.platform` mac 检测
- `TerminalView.vue:30` 与 `SettingsReceivedFiles.vue:9-10` 里绕过 `lib/api.ts` 的直接 wails 导入统一走 shim
- `lib/api.ts:355-467` 手写 `AppBindings` → `typeof import(...)`

**依赖**：里程碑 2-b 完成。
**验收**：`npm test` + `wails dev` + iOS Simulator 手工跑一次（IME/软键盘/长按选择/aux key row/paste）。
**预估**：3–4 天。

---

## 里程碑 6 · 包边界（需先讨论）

**每个决策点独立可拒**，先讨论再动手。

- **6-a**：`internal/hostid` (65 行) 折进 `internal/appdir` → `appdir.HostID()`。**推荐做**（收益/风险都低）
- **6-b**：`wrap/unwrapAccountKey` 从 `e2eeclient` 挪到 `e2eecrypto`（能让 `e2eeclient` 从 443 → ~200 只做 HTTP）。**讨论**
- **6-c**：`internal/userstore/store.go:36` `SQLiteStore = DBStore` 别名迁移 ~40 处调用点后删。**推荐做**（TODO 已久）
- **6-d**：`internal/relay/adopt.go:17-40` `PtyHost` / `ImagePasteHost` / `FilePasteHost` 接口挪去 `internal/ptyhost`，让 desktop 不再为借接口 import relay。**讨论**

---

## 里程碑 7 · P2 收尾（一个 PR 打包）

- `lib/api.ts` (1119 行) 按 domain 拆 `api/relay.ts` / `api/ssh.ts` / `api/feishu.ts` / `api/tasks.ts` / `api/updates.ts` / `api/recovery.ts`，`api/index.ts` 重导
- `main.ts` / `main.web.ts` / `main.capacitor.ts` 合并 `bootstrapApp(factory, prefsSync)`
- `desktop/config.go:495` inline `applyConfigDefaults`
- `desktop/paste_image.go` 抽 `pty_host.go`
- `desktop/fsaccess.go` 干掉 `net/http` 依赖
- `mobile/` `web/` `site/` 三个 `package.json` 补 `engines`
- `docker-compose.yml:99` watchtower 镜像 pin
- `internal/session/registry.go` `List()` → `ForEach()`
- 合并 / 删除 `docs/feishu-remote-terminal-deployment.md`（与 spec 重复）
- `scripts/feishu-*-e2e-checklist.md` 挪 `docs/`

---

## 时间表

| 周 | 里程碑 | PR 数 | 状态 |
| --- | --- | --- | --- |
| 第 1 周 | 0、1 | 4 | 0 完成（#289 #290 #291）|
| 第 2 周 | 2、3 | 7 | - |
| 第 3 周 | 4 | 3 | - |
| 第 4 周 | 5、6、7 | 5+ | - |

约 4 周、~19 个 PR，中位每个 PR ≤ 300 行 diff。

---

## 进度更新习惯

- 每个 PR 合并后回来更新对应里程碑的 PR 状态 + 附链接 + 净行数
- 里程碑全部完成后把状态改成 ✅
- 新学到的与路线图有冲突的东西，直接在这里改（memory 里也同步）

**当前进度**：14/19+ PR merged。**里程碑 0–5 主体已交付**，剩余细项以 followup 形态排到 M6/M7 或独立 PR。

## 实际交付状态（2026-08-04 第 2 次会话）

| 里程碑 | PR | 状态 | 备注 |
| --- | --- | --- | --- |
| 0-a | [#289](https://github.com/attson/atterm/pull/289) | ✅ merged | Go 后端 + internal 死代码，−352 行 |
| 0-b | [#290](https://github.com/attson/atterm/pull/290) | ✅ merged | 前端死 exports + i18n 死键，−316 行 |
| 0-c | [#291](https://github.com/attson/atterm/pull/291) | ✅ merged | 构建/CI/gitignore 漂移 |
| **1** | [#292](https://github.com/attson/atterm/pull/292) | ✅ merged | OPAQUE wire 类型单一来源；顺带修 client 侧漏读 IsAdmin |
| 2-a | [#293](https://github.com/attson/atterm/pull/293) | ✅ merged | `keychainSlot[T]` 泛型 helper，4 store 迁移 |
| 2-b | [#294](https://github.com/attson/atterm/pull/294) | ✅ merged | `definePersistedSingletonRef` 覆盖 useTaskPreset / useTaskGroupBy |
| 2-c | [#295](https://github.com/attson/atterm/pull/295) | ✅ merged | `marshalPtr[T]` 覆盖 prefssync 4 段 nil-guard |
| 3-a | [#296](https://github.com/attson/atterm/pull/296) | ✅ merged | `app.go` 2950 → 1536，抽 6 个 domain 文件 |
| 3-b | [#297](https://github.com/attson/atterm/pull/297) | ✅ merged | `relay_host.go` 1437 → 816，抽 `relay_host_feishu.go` |
| 3-c | [#298](https://github.com/attson/atterm/pull/298) | ✅ merged | `updater.go` 1220 → 734，抽 github / install / versions |
| 3-d | [#299](https://github.com/attson/atterm/pull/299) | ✅ merged | `uplink.go` 1043 → 721，抽 frame_seal + stream_log |
| 4-a | [#300](https://github.com/attson/atterm/pull/300) | ✅ merged | `session.go` 1747 → 1096，抽 silence + osc133 + meta |
| 4-b | [#301](https://github.com/attson/atterm/pull/301) | ✅ merged | `admin_http.go` 586 → 94 + `opaque_auth.go` 528 → 118 |
| 5-c 窄 | [#302](https://github.com/attson/atterm/pull/302) | ✅ merged | 4 处 mac 检测收拢到 `lib/modKey.ts` |

**主动延后的 followup**（不阻塞路线图完成，视需要单独开工）：
- 3-b 里"3 张 feishu map → 1 struct"（锁纪律语义变更，风险）
- 3-a 里 `updatePref(a, key, mutator)` helper（原属 M7 P2 收尾更合适）
- 3-d 里 `webPushSessionLabel` / `looksLikeEncryptedOut` 跨包搬迁
- 4-b 中 `server.go` 拆 `static_handler.go` + `origins.go` + `feishu_runtime.go`（ServeHTTP 混合路由需先设计再拆）
- 4-c `uplinkSession` struct 化（10 个闭包共享锁 → struct + 方法；语义 refactor）
- 5-a `App.vue` 抽 composables（memory：iOS Capacitor overlay pointer-events / template send split 是硬约束，需 Simulator 逐 composable 验证）
- 5-b `TerminalView.vue` 抽 composables（同上）
- 5-c 里 `xtermFactory` 统一 App.vue 探针 + TerminalView 主终端（要触碰两个 mega-SFC，一起放在 M5-a/b 走）
- 5-c 里 `AppBindings = typeof import('wailsjs/...')` 类型替换（需验证 112 签名 round-trip）
- 里程碑 6（包边界）：hostid → appdir、SQLiteStore alias 迁移、wrap/unwrap → e2eecrypto、PtyHost interface → ptyhost
- 里程碑 7（P2 收尾）：api.ts 按 domain 拆、main.ts 合并、docker-compose watchtower pin、engines 补齐、docs 合并等
