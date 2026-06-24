# Relay 实例注册 + 账号级节点选择设计(阶段二 · 子项目 B)

日期:2026-06-24
状态:已评审,待写实现计划

## 0. 背景与定位

**阶段二(多实例实时路由)的子项目 B**。阶段二拆为 A(realm 身份 + E2EE 重锚定,已实现,PR #238)→ **B(本设计)** → C(客户端节点选择器 + 路由)。B **栈在 A 之上**,依赖 A 的 `realm_id` 与登录 finalize 响应。

阶段二采用 **"统一登录域名 + 账号级用户可选节点"** 模型。硬约束:一个会话的两端(桌面 `/uplink` + 手机 `/client`)必须落在同一节点才能配对(会话注册表是各节点内存态),故"所选节点"是**账号级、跨设备共享**的(存共享 DB),不是每设备临时选。

**B 是纯 relay,无客户端改动**:relay 建好实例注册/心跳、节点列表、账号级选择、登录下发所选节点,但客户端还不据此路由(C 才做选择器 UI + 连接路由)。对现有单机客户端零行为变化。

## 1. B-1:实例注册表 + 心跳

- 新增表 `relay_instances(instance_id TEXT PRIMARY KEY, public_url TEXT NOT NULL, last_heartbeat INTEGER NOT NULL)`。
- 每节点配 `ATTERM_RELAY_INSTANCE_PUBLIC_URL`(客户端可直达的对外域名,如 `https://relay-tokyo.example.com`)。**`instance_id` = 该 public_url 本身**(唯一且即客户端要连的身份,无需另造 id)。
- userstore 新增(新文件 `relay_instances.go`):
  - `type RelayInstance struct { InstanceID, PublicURL string; LastHeartbeat int64 }`
  - `UpsertInstanceHeartbeat(ctx, instanceID, publicURL string, nowUnix int64) error` — `INSERT ... ON CONFLICT(instance_id) DO UPDATE SET public_url=excluded.public_url, last_heartbeat=excluded.last_heartbeat`。
  - `ListLiveInstances(ctx, minHeartbeat int64) ([]RelayInstance, error)` — `WHERE last_heartbeat >= ?`,按 instance_id 排序。
- 启动后心跳 goroutine(沿用现有 ticker 模式,如 `main.go` 的 Feishu sweep):每 30s `UpsertInstanceHeartbeat`。存活窗口 90s(`minHeartbeat = now - 90`)。
- **未配 `INSTANCE_PUBLIC_URL`(单机/开发)→ 不注册心跳、不参与节点选择**;此时 `ListLiveInstances` 为空,登录按 B-2 退化(见下)。

## 2. B-2:账号级节点选择(user_home)

- 新增表 `user_home(user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, instance_id TEXT NOT NULL, updated_at INTEGER NOT NULL)`。
- userstore 新增(新文件 `user_home.go`):
  - `GetUserHome(ctx, userID string) (instanceID string, ok bool, err error)` — 无行 ok=false。
  - `SetUserHome(ctx, userID, instanceID string) error` — upsert。
- 选择是**账号级**:所有设备共享;C 的切换写这里、全账号生效。
- **首登默认(user_home 未设)**:自动分配为**当前处理登录的节点**(它知道自己的 `InstancePublicURL` = instance_id),写入 user_home 并在登录响应返回。开箱即用;单机天然只有一个节点。
  - 若本节点未配 `INSTANCE_PUBLIC_URL`(单机无注册)→ 不写 user_home,登录响应 `home_instance_url` 为空(单机客户端直连配置的 relay,本就不需要 home)。
- **新设备登录已选账号** → 读 user_home 返回既有选择(不覆盖)。

## 3. B-3:API + 登录下发 + /healthz

### 登录 finalize 下发 home_instance_url
`internal/relay/opaque_auth.go` 的 `loginFinalizeResponse`(已含 `realm_id`)加 `HomeInstanceURL string json:"home_instance_url"`。`handleLoginFinalize` 在编码前解析 user_home:
1. `GetUserHome(userID)`:
   - 有且对应实例**存活**(在 `ListLiveInstances` 中)→ 返回其 public_url。
   - 有但实例**已死**(心跳过期)→ **返回空 `home_instance_url`**(让客户端 C 提示重选;不擅自搬迁)。【采纳方案 a】
   - 无 → 按 B-2 首登默认:若本节点已注册,`SetUserHome(userID, 本节点instance_id)` 后返回本节点 public_url;否则返回空。
- realm 解析与 home 解析都需要 `h.store`(已有)+ userID(已有)。

### 节点列表 API
`GET /api/nodes`(会话鉴权,`s.requireSession`)→ `{ "nodes": [{"instance_id": "...", "public_url": "..."}] }`,来源 `ListLiveInstances`。**ping 延迟由客户端 C 侧自测**,本 API 只给列表。

### 设置节点 API
`PUT /api/me/home`(会话鉴权,body `{"instance_id": "..."}`)→ 校验 instance_id 在存活列表中(否则 400)→ `SetUserHome(userID, instanceID)`。供 C 的选择器调用。

### /healthz 实例身份
`internal/relay/health_http.go` 的 `handleHealthz` 响应(现为 `{ok, version}`)加 `instance_id` / `public_url`(空配置时为空),便于运维辨认节点。

## 4. B-4:Server 配置 + 范围 + 测试

### Server 配置
`internal/relay/server.go` 的 `Config` 加 `InstancePublicURL string`(= instance_id)。`cmd/atterm-relay/main.go` 用 `envOr("ATTERM_RELAY_INSTANCE_PUBLIC_URL", "")` 读取并填入 Config + 启动心跳 goroutine(仅当非空)。

### 范围
**纯 relay,无客户端改动**。B 交付注册/心跳/列表/选择/下发;客户端不据此路由(C 才做)。现有单机:登录自动分配唯一节点(或空),客户端忽略 home 字段直到 C → 零行为变化。

### 测试
- userstore 双后端契约:`UpsertInstanceHeartbeat` + `ListLiveInstances`(过期过滤);`GetUserHome`/`SetUserHome`。
- relay:登录响应 `home_instance_url`(首登自动分配、已选沿用、死节点返回空三态);`GET /api/nodes` 鉴权 + 列表;`PUT /api/me/home` 鉴权 + 存活校验 + 写入;`/healthz` 含实例身份。

## 5. 运维配置(子项目 B)

- **每节点配 `ATTERM_RELAY_INSTANCE_PUBLIC_URL`**(各节点不同,= 该节点客户端可直达的域名)。这是 B 引入的、唯一"每节点不同"的配置。
- 所有节点仍共享同一 `ATTERM_RELAY_DB_DSN`(Plan 1/2)与同一 realm(A,自动)。
- 单机/开发:不设 `INSTANCE_PUBLIC_URL` → 不注册、不选择,行为同今。
- 统一登录域名(LB/anycast 覆盖各节点,或指定节点)仍是部署侧的事;认证无状态写共享 DB,任意节点可承载登录并据 user_home 下发 home。

## 6. 后续(子项目 C —— 不在本子项目)

- 三端节点选择器 UI:拉 `GET /api/nodes`、**客户端侧测 ping 延迟**展示、显示当前选择、切换调 `PUT /api/me/home`(**切换全账号生效**,所有设备重连)。
- 客户端读登录响应的 `home_instance_url`,把 `/uplink`、`/client` 连到该节点;`home_instance_url` 为空时提示选择;归属变化时重连。
- E2EE 已按 `realm_id` 锚定(A),切节点/换域名不影响解密。
- 见设计 spec(A)§7。

## 7. 受影响代码(子项目 B 概览)

- `internal/userstore/`:`0009_node_selection`(sqlite)/`0008_node_selection`(postgres)迁移建 `relay_instances` + `user_home` 两表;`relay_instances.go`(`RelayInstance` + `UpsertInstanceHeartbeat` + `ListLiveInstances`);`user_home.go`(`GetUserHome`/`SetUserHome`);Store 接口加方法;契约测试。
- `internal/relay/`:`opaque_auth.go`(`loginFinalizeResponse` 加 `HomeInstanceURL` + 解析逻辑);新增 `nodes_http.go`(`GET /api/nodes`、`PUT /api/me/home`);`server.go`(Config 加 `InstancePublicURL`、注册两路由);`health_http.go`(/healthz 实例身份)。
- `cmd/atterm-relay/main.go`:`ATTERM_RELAY_INSTANCE_PUBLIC_URL` env + 填 Config + 心跳 goroutine。
