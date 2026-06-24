# Relay 逻辑 realm 身份 + E2EE 重锚定设计(阶段二 · 子项目 A)

日期:2026-06-24
状态:已评审,待写实现计划

## 0. 背景与定位

这是**阶段二(多实例实时路由)的子项目 A**,也是阶段二的基础。阶段二经评审拆为三个有依赖顺序、各自独立交付的子项目:

- **A(本设计)** — 逻辑 realm 身份 + E2EE 重锚定。
- **B** — 实例注册 + 账号级用户可选节点(见 §6,后续单独立项)。
- **C** — 客户端节点选择器 + 连接路由(见 §6,后续单独立项)。

阶段一(Plan 1/2/3:双后端 userstore、配置入库、跨库搬迁)已实现(PR #233/#235/#236)。本子项目在其之上构建,**栈在 Plan 3 分支之上**。

### 为什么 A 是基础

多节点采用"统一登录域名 + 账号级用户可选节点"模型(§6):用户会**主动切换节点**(物理域名变化)。当前桌面把 E2EE `account_key` 在 keychain 按 `{relayOrigin}|{userID}` 存(`desktop/account_key_store.go:33`),**域名一变 key 即被孤立 → 远程会话无法解密**(见 [[account-key-origin-scoped]])。子项目 A 把 key 锚定到**整个集群共享的稳定 `realm_id`**,而非物理 origin,使密钥跨节点/域名可移植。即便仍是单实例,A 也无害且有价值(让 key 不再绑死某个 URL)。

## 1. 目标与范围

**目标**:relay 暴露一个稳定的 `realm_id`,认证响应下发它,桌面客户端把 `account_key` 按 `realm_id` 锚定并迁移存量。

**范围(客户端)**:
- **桌面** — 重锚定 + 存量迁移(bug 所在,核心改动)。
- **移动** — 解析并存下 `realm_id` 供后续 C 使用;**不改其密钥存储**(`capacitor.ts` 用单 key `atterm.relay.account-key`,本就"跟用户走、不跟域名走",realm 无关,无 origin 孤立问题)。
- **Web** — sessionStorage 不跨导航、无持久化,**不受影响**;临时持有 `realm_id` 即可。

**不在本子项目**:实例注册、节点选择、连接路由(B/C)。

## 2. A-1:realm 身份(relay 侧)

### 数据
- 新增单例表 `relay_realm_state`:`id INTEGER/BIGINT PRIMARY KEY CHECK (id=1)`、`realm_id TEXT NOT NULL`、`created_at INTEGER/BIGINT NOT NULL`。分方言迁移(SQLite 下一号 `0008_relay_realm.sql`;Postgres `0007_relay_realm.sql`)。
- userstore 新增(沿用 `opaque_server_state` 模式,见 `internal/userstore/opaque.go`):
  - `type RealmState struct { RealmID string; CreatedAt time.Time }`
  - `func (s *DBStore) GetRealmState(ctx) (RealmState, error)` — 无行返回 `ErrRealmStateMissing`。
  - `func (s *DBStore) StoreRealmState(ctx, RealmState) error` — `INSERT ... ON CONFLICT(id) DO UPDATE`(id=1)。
  - 双后端契约测试。

### 生成与覆盖(LoadOrInit)
relay 启动时 `LoadOrInitRealm`(仿 `internal/relay/opaque_server.go:LoadOrInitOpaqueServer`):
1. 读 `ATTERM_RELAY_REALM_ID`(可选 env)。
2. `GetRealmState`:
   - **DB 已有** → 若 env 也设了且与 DB **不一致**:**启动报错退出**(`fatal: ATTERM_RELAY_REALM_ID conflicts with persisted realm`)—— 防止误改而孤立所有 `account_key`。一致或 env 未设 → 用 DB 的值。
   - **DB 无(首启)** → realm_id = env 值(若设),否则 `defaultIDs.New()` 生成一个 ULID;`StoreRealmState` 持久化。
3. 把 `realm_id` 提供给认证层(放入 Server 配置 / OpaqueAuthHandler)。

**多节点**:realm 是集群单行;首个启动的节点生成,其余节点读到同一值。(并发首启的竞态:`ON CONFLICT(id=1)` 幂等;两节点同时生成不同 ULID 时,第二个 INSERT 命中冲突走 DO UPDATE——为避免互相覆盖,首启生成走"INSERT 若不存在,然后重新 Get 以读取最终落地值"语义;实现细节在计划中明确。运维若在意,用 env 显式 pin。)

## 3. A-2:下发 realm_id(relay + SDK + 三端解析)

- **relay**:凡返回 `account_key`(或其 wrap)的认证 finalize 响应都加 `realm_id`:
  - 登录 finalize `loginFinalizeResponse`(`internal/relay/opaque_auth.go:122`)加 `RealmID string json:"realm_id"`,在编码处(~:521)填入。
  - **新账号注册 finalize**:若该路径返回 account_key/wrap,同样加 `realm_id`(实现时确认该响应结构并一并补上)。
- **e2eeclient SDK**:`LoginResult`(`internal/e2eeclient/client.go:97`)加 `RealmID string`;wire 结构 `loginFinalizeResponse`(~:430)加字段;`Login()` 解析处(~:224)赋值。
- **移动**(`desktop/frontend/src/platform/capacitor.ts:108`):finBody 类型加 `realm_id: string`;登录后随会话配置 `secureStorage.set` 存下 `realm_id`(供 C 用);**不改 `ACCOUNT_KEY_KEY` 的存取**。
- **Web**:解析 `realm_id`(可放入现有 relay 配置/同步层),不改 account_key 的 sessionStorage 存取。

## 4. A-3:桌面重锚定 + 存量迁移(核心)

`desktop/account_key_store.go`:
- `accountKeyAccount` 改为按 realm 锚定:返回 `{realmID}|{userID}`(service 名不变 `com.atterm.account-key.v1`)。`load/saveAccountKey`/`clearAccountKeyFor` 的"origin"形参语义改为 realmID(或新增 realm 版函数,旧 origin 版仅迁移时用——见下)。

`desktop/config.go`:
- `appConfig` 加持久化字段 `RelayRealmID string json:"relay_realm_id,omitempty"`(`RelaySessionUserID` 之后)。

`desktop/app.go`:
- **启动加载**(~:288):改用 `loadAccountKey(cfg.RelayRealmID, cfg.RelaySessionUserID)`。`RelayRealmID` 随登录持久化,故重启可离线解锁。
- **登录持久化**(`LoginRemoteRelay` ~:608):`cfg.RelayRealmID = res.RealmID`;`persistAccountKey` 存到 `{realmID}|{userID}`。
- **退役 URL 变化迁移块**(~:531):key 锚定 realm 后,换 URL 不再需要迁移 key;移除该块。

**不做存量迁移**(YAGNI):目前无现有用户(仅开发者本人在用),无需把旧 `{origin}|{userID}` 槽迁到 realm 槽。开发者重新登录一次即在 `{realmID}|{userID}` 槽生成新 key;旧 origin 槽的 key 成无害孤儿(可忽略)。这显著简化 A-3 并省去迁移代码与迁移测试。

## 5. A-4:单实例兼容 + 测试

- **单实例**:realm 照常生成、登录下发、桌面一次性 origin→realm 迁移,对现有用户除一次迁移外无感。
- **测试**:
  - userstore:`Get/StoreRealmState` 双后端契约;`relay_realm_state` 迁移在两后端建表。
  - relay:`LoadOrInitRealm` 幂等(重复启动 realm 不变)、env 一致复用、**env 与 DB 冲突报错**、首启生成持久化。
  - relay/e2eeclient:登录 finalize 响应含 `realm_id`;SDK `LoginResult.RealmID` 正确解析。
  - 桌面:`accountKeyAccount` 按 realm 锚定;登录持久化到 realm 槽、启动用 `RelayRealmID` 加载;退役 URL 变化迁移块(无存量迁移)。

## 6. 运维配置(子项目 A)

A 的核心设计是 **`realm_id` 靠共享 DB 自动一致**,运维负担几乎为零:

- **单实例**:无需任何配置 —— 首启自动生成 `realm_id` 写进 DB,以后复用。
- **多实例**:所有节点本就指向同一个 Postgres(Plan 1/2:`ATTERM_RELAY_DB_DRIVER=postgres` + 同一个 `ATTERM_RELAY_DB_DSN`)。第一个启动的节点生成 `realm_id` 写共享 DB,其余节点读到同一值 → 集群天然一致,**无需逐节点配 realm**。
- **`ATTERM_RELAY_REALM_ID`(可选)**:仅特殊场景使用 —— IaC 显式 pin,或换 DB / 蓝绿迁移时保持同一 realm(让客户端已有 E2EE key 继续有效)。设置时全节点设同值;与 DB 已存值不一致 → 启动报错退出(防误改孤立全部 key)。

整体多节点运维图景(含后续 B/C 引入项,标注归属):

| 配置项 | 引入 | 多节点配置 |
|---|---|---|
| `ATTERM_RELAY_DB_DSN`(同一 PG) | Plan 1/2 | 所有节点同一个(共享用户/token/配置/realm/订阅) |
| 飞书加密 key | Plan 2 | 在 DB,自动共享 |
| `ATTERM_RELAY_REALM_ID` | **A** | 一般不设(DB 自动共享);特殊场景全节点同值 |
| 节点对外域名 / 实例 URL | B(后续) | **每节点不同**(如 `ATTERM_RELAY_INSTANCE_URL=relay-tokyo.example.com`),注册进 `relay_instances` |
| 统一登录域名 | B/C(后续) | 一个稳定入口(LB/anycast 或指定节点;认证无状态可由任意节点承载) |
| TLS 终结 | 现状 | 每节点前挂反代、各自证书 |

**A 阶段唯一可能用到的新配置是可选的 `ATTERM_RELAY_REALM_ID`**;"每节点不同"的节点域名要到 B 才引入。

## 7. 后续(B/C 设计纲要 —— 本子项目不实现,记录备查)

阶段二采用 **"统一登录域名 + 账号级用户可选节点"** 模型(经评审确认):

- **统一登录域名**:用户在稳定入口登录(认证无状态、写共享 DB,可由任意节点/LB 承载)。登录响应带回 `realm_id`(A 已做)+ 用户当前所选节点 URL + 节点列表入口。
- **硬约束 —— 选择粒度是账号,不是连接**:一个会话两端(桌面 uplink + 手机 client)必须落在同一节点才能配对(会话注册表是各节点内存态)。故"所选节点"存在共享 DB 的 `user_home`(账号级),用户所有设备都跟随。

**子项目 B(纯 relay)**:
- `relay_instances(instance_id, public_url, last_heartbeat)` + 心跳;节点列表 API(供客户端拉取,含健康/可达信息)。
- `user_home(user_id → instance_id)`:用户选择持久化;新设备登录读到已有选择则**默认沿用**(用户已确认的行为);归属实例心跳超时则提示重选/惰性重分配。
- 登录响应下发所选节点 URL(无则提示选择)。`/healthz` 加实例身份。

**子项目 C(三端)**:
- 节点选择器 UI:列出节点、**显示 ping 延迟**、显示当前选择、切换;**切换全账号生效**(写 `user_home`,所有设备重连)。
- 客户端把 `/uplink`、`/client` 连到所选节点;归属变化时重连。
- 因 E2EE 已按 `realm_id` 锚定(A),切节点/换域名不影响解密。

**E2EE 关键约束(A 已解决)**:account_key 锚定集群 `realm_id`,故 B/C 的节点切换天然安全。

## 8. 受影响代码(子项目 A 概览)

- `internal/userstore/`:`relay_realm_state` 双方言迁移、`RealmState` + `Get/StoreRealmState` + `ErrRealmStateMissing`、契约测试。
- `internal/relay/`:`LoadOrInitRealm`(realm.go 新增)、`loginFinalizeResponse` + 注册 finalize 响应加 `realm_id`、Server/认证层持有 realm_id。
- `cmd/atterm-relay/main.go`:`ATTERM_RELAY_REALM_ID` env、启动调用 `LoadOrInitRealm`。
- `internal/e2eeclient/client.go`:`LoginResult.RealmID` + wire + 解析。
- `desktop/`:`account_key_store.go`(realm 锚定)、`config.go`(`RelayRealmID`)、`app.go`(启动加载/登录持久化/退役 URL 迁移块;无存量迁移)。
- `desktop/frontend/src/platform/capacitor.ts`:解析并存 `realm_id`(不改密钥存储)。
- Web:解析 `realm_id`(不改 account_key 存储)。
