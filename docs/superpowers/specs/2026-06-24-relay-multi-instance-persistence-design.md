# Relay 多实例持久化改造设计

日期:2026-06-24
状态:阶段一已评审,待实现;阶段二为后续设计纲要

## 1. 背景与目标

当前 relay 是单机进程,持久化分散在三处:

- **SQLite `users.db`**(`internal/userstore`):用户、session token、OPAQUE 状态、E2EE account_key 信封、preferences、飞书绑定等。`modernc.org/sqlite` 纯 Go 驱动 + 裸 SQL,`SetMaxOpenConns(1)` 串行化。
- **`relay.json`**(`internal/relay/admin_config.go`):限流、`allowed_origins`、飞书开关与加密密钥、VAPID subject、debug 开关。部分字段 admin API 运行时可改。
- **`web-push.json`**(`internal/webpush/persist.go`):VAPID 公私钥对 + 各用户 web push 订阅。

目标:让 relay 支持**多实例水平扩展**,多个实例共享同一份数据,便于多机部署与数据共享;同时保留单机/本地零依赖运行。

### 关键约束(来自代码现状调研)

relay 不只是数据库读写服务,它是有大量内存态的**实时连接路由器**:桌面端经 `/uplink` 把会话 ANNOUNCE 到**所连实例**的内存注册表(`session/registry.go`、`relay/uplink_conn.go`),远程端经 `/client` 从**所连实例**的注册表 ATTACH。桌面与远程若落在不同实例则配不上、收不到帧。目前无任何 Redis/NATS/跨实例 pub-sub,也无实例间发现机制。

认证类端点(login/signup/pairing)在登录时尚无 userID(只有 email),会落在任意实例并写 `users`/`sessions` 表。**故数据库必须支持任意实例并发读写同一份数据 —— 不能是主从只读副本**(Litestream 等单写复制方案出局)。

客户端通过单个 base URL **直连** relay(桌面 `wss://.../uplink`+`/client`,手机 `https://.../api`+`/client`),`docker-compose.yml` 本就预期前面挂一个仅做 TLS 终结的反代(Caddy/nginx/Cloudflare/Tailscale)。relay 未解析 `X-Forwarded-For`(限流按 `RemoteAddr`)。

## 2. 阶段划分

经评审,改造拆为两阶段,**本 spec 实现阶段一,阶段二仅记录设计纲要**:

- **阶段一(本次,低风险、独立有价值)**:持久化层抽象(SQLite+Postgres)+ 配置入库 + 跨库搬迁工具。交付后多实例可**共享同一份数据与配置**,单机照常完整可用 —— 即"多机配置与数据共享"的字面诉求。此阶段不解决跨实例实时配对:多实例此时适合"共享数据"类用途,实时会话仍建议单活。
- **阶段二(后续单独立项,见 §6)**:逻辑 realm 身份 + 实例注册/心跳 + 归属调度 + 客户端感知分片(每实例独立域名)+ E2EE 按 realm 重锚定。让多实例真正服务"同一 user 的桌面+手机"实时路由,涉及三端协同,风险与工作量都大得多。

---

# 阶段一:持久化层

## 3. 详细设计

### 3.1 存储后端抽象(SQLite + Postgres)

- 定义 `userstore.Store` 接口,覆盖现有 `SQLiteStore` 的全部约 30 个公开方法(用户、session、pairing、invitation、claim token、opaque、preferences、seen、飞书绑定/pending、admin 等)。仓库已有 `store_iface_test.go`,在此基础上补全。
- 两套实现:
  - `sqlite`:现有逻辑保留。
  - `postgres`:新增,驱动用 `jackc/pgx`(纯 Go,`database/sql` 兼容)。
- **去除泄漏抽象**:`store.go:82` 的 `DB() *sql.DB` 暴露底层句柄,需移除或收敛 —— 所有 SQL 收进各实现内部,调用方只依赖接口。
- 配置选择后端:
  - `ATTERM_RELAY_DB_DRIVER=sqlite|postgres`(默认 `sqlite`)。
  - `ATTERM_RELAY_DB_DSN`:Postgres 连接串;sqlite 继续用 `--config-dir` 推导 `users.db` 路径。
- **迁移文件分方言两套目录**:`migrations/sqlite/*.sql` 与 `migrations/postgres/*.sql`,各自维护。SQLite 与 Postgres 的 SQL 差异(占位符 `?` vs `$N`、`BLOB` vs `BYTEA`、`INTEGER` 时间戳 vs `BIGINT`/`timestamptz`、`AUTOINCREMENT`、pragma、UPSERT)在 SQL 层隔离,Go 层共用迁移 runner(按文件名排序、`schema_migrations` 记录已应用)。**不**追求单套兼容 SQL。

### 3.2 配置入库的数据模型

新增三类存储(各写一条分方言迁移):

1. **`relay_config`(单例,id=1)** —— 承载 `relay.json` 现有字段:
   `allowed_origins`、`rate_limit_per_minute`、`max_connections_per_key`、`feishu_enabled`、`feishu_encrypt_key`、`feishu_base_url`、`vapid_subject`、`debug`、`debug_payload`。
   含一个单调递增的 `version`(或 `updated_at`)列用于缓存失效。admin API 改写此表,不再写文件。
2. **`web_push_keys`(单例)** —— VAPID 公私钥对。沿用现有 `opaque_server_state` 的单例模式:首次启动若不存在则生成一次、入库,所有实例共读。
3. **`web_push_subscriptions`(per user)** —— 把 `web-push.json` 的订阅 map 拆成行,主键 `(user_id, endpoint)`,字段含 `p256dh`、`auth`、`created_at`。订阅/退订写库,推送时按 `user_id` 读库。

### 3.3 运行时一致性(无 pub-sub 的最终一致)

admin 在实例 A 改了配置(如 `allowed_origins`),实例 B 的内存缓存需更新。既然不引消息总线,采用**短 TTL 内存缓存**:

- 各实例对 `relay_config` 维护内存缓存,每 5–10s(或按 `version` 列变化)刷新。
- 热路径(每请求读 `allowed_origins`、限流阈值、飞书 cipher)读内存缓存,不打库。
- admin 改动在一个 TTL 窗口内传播到全实例 —— 对这些配置项最终一致足够。

per-instance 语义保留:`rate_limit_per_minute` / `max_connections_per_key` 的计数器本就是各实例独立的内存态(`relay/limits.go`),集群总量 = 阈值 × 实例数。此点写入部署文档。

### 3.4 飞书加密密钥入库 —— 已知安全风险(有意识取舍)

`feishu_encrypt_key` 是用来 AEAD 加密 DB 内飞书凭据字段(`internal/userstore/secret_encrypt.go`)的根密钥。按本设计它与它所加密的密文**存于同一个数据库**。

**风险**:数据库一旦泄露(备份外流、注入、运维误操作),解密密钥就在同库,飞书凭据的静态加密形同虚设("保险箱钥匙贴在保险箱门上")。

**取舍**:为换取部署简化(一个 DSN 即可,无需额外分发 env secret),接受此风险,密钥入库。**这是经评审的有意识决定,非疏忽。** 若未来安全要求提高,可将该密钥改为 `ATTERM_FEISHU_ENCRYPT_KEY` 环境变量注入而不入库(代码已有该 env 读取点),其余配置仍留库。

> 对比:VAPID 私钥虽也是 secret 但入库,理由是项目已有 `opaque_server_state` 将服务端 AKE 私钥入库的先例,且 VAPID 私钥不解密本库其他数据,风险可接受。

### 3.5 跨库搬迁子命令

新增 `atterm-relay migrate --from <dsn> --to <dsn>`:

- 两端都通过 `Store` 接口打开(`sqlite://...` 或 `postgres://...`),天然支持 SQLite↔Postgres **双向**搬迁,服务后续选型切换。
- 按外键依赖顺序全量读源、写目标。一次性离线操作。

### 3.6 阶段一不做的事(YAGNI / 已砍)

- **不做**从旧 `relay.json` / `web-push.json` 自动导入 DB 的 bootstrap 逻辑。多机从空库起步即可;空库启动时正常初始化默认配置、首次生成 VAPID 密钥对入库(常规启动逻辑,非数据迁移)。现有单机若要保数据,用 §3.5 搬迁子命令处理 SQLite 部分。
- **不做**任何阶段二内容:实例间转发、消息总线、归属调度、客户端分片、E2EE 重锚定。

## 4. 阶段一部署说明(文档交付)

- 多实例共享同一个 `ATTERM_RELAY_DB_DSN`(Postgres)。
- 明确文档化:`rate_limit` / `max_connections` 为 per-instance,集群总量 = 阈值 × 实例数。
- **明确告知阶段一的能力边界**:跨实例的实时会话配对尚未实现(见 §6);多实例阶段适合共享数据/配置与水平承载认证类无状态请求,实时终端会话仍建议单活,或等阶段二。

## 5. 阶段一测试策略

- **Store 契约测试**:同一套接口测试对 `sqlite` 与 `postgres` 两个后端各跑一遍,保证行为一致(Postgres 用 testcontainers 或 CI 起的实例)。
- **配置入库测试**:`relay_config` 读写、TTL 缓存刷新、admin 改动可见性。
- **web push 测试**:VAPID 单例生成幂等、订阅增删、按用户读取。
- **搬迁子命令测试**:造 SQLite 数据 → 搬到 Postgres → 校验全量一致;反向同验。

## 6. 阶段一受影响代码(概览)

- `internal/userstore/`:抽 `Store` 接口、新增 postgres 实现、迁移分方言、移除 `DB()`。
- `internal/relay/admin_config.go` 及 AdminConfigStore:配置源从文件改为 `relay_config` 表 + TTL 缓存。
- `internal/webpush/persist.go` 及 Service:VAPID 密钥与订阅从 `web-push.json` 改为 DB 表。
- `cmd/atterm-relay/main.go`:后端选择(env/flag)、新增 `migrate` 子命令、去掉 JSON 文件装载路径。

---

# 阶段二:多实例实时路由(后续设计纲要,本次不实现)

记录在此以免后续重踩。采用**客户端感知分片(每实例独立域名)**,稳定性优于"应用层转发"与"哑反代 sticky"(后者在桌面/手机跨网络场景下因拿不到稳定 hash key 而根本配不对,已否决)。

### 6.1 逻辑 realm 身份(E2EE 锚定的关键约束)

**调研确认的硬约束**:桌面 `desktop/account_key_store.go:33-40` 的 `accountKeyAccount()` 把 E2EE account_key 在 keychain 按 `{relayOrigin}|{userID}` 存(注释明说故意按 origin 隔离不同 relay)。**每实例独立域名 → origin 不同 → account_key 被孤立 → 远程会话无法解密。** 移动端 secureStorage 用单 key `atterm.relay.account-key`(不按 origin,基本不受影响但需对齐);web 用 sessionStorage 不跨导航(无影响)。详见 [[account-key-origin-scoped]]。

**解法**:引入稳定的**逻辑 relay 身份 `realm_id`**(共享 DB 单例,全实例同值,首次启动生成或配置),登录响应下发。客户端 E2EE 状态按 `realm_id` 锚定,**不按物理实例域名**:
- 桌面 keychain key 改为 `{realmId}|{userID}`;需一次性把存量 `{origin|userID}` 迁移到 `{realmId|userID}`。
- 登录响应新增 `relay_realm_id` 与 `home_instance_url` 字段(`internal/relay/auth.go` 响应结构 + 三端解析)。
- E2EE 设计文档(`2026-06-15-relay-e2ee-design.md`)需更新为"按逻辑身份锚定"。

### 6.2 实例注册与归属调度

- **实例自身**:配 `ATTERM_INSTANCE_PUBLIC_URL`(客户端可直达的独立域名);在 DB `relay_instances(instance_id, public_url, last_heartbeat)` 注册并心跳。
- **归属表** `user_home(user_id → instance_id)`:首次登录从在线实例中分配(轮询/最少负载),之后固定;归属实例心跳超时则 lazy 重分配。
- 同一 user 的桌面与手机用同一 `user_home` → 天然落同实例,**无需实例间转发**。

### 6.3 客户端感知分片流程

- **引导入口**:客户端首次连任意地址(轮询 DNS 或任一实例域名);认证走任意实例(写共享 DB)。
- 登录响应返回 `home_instance_url`;客户端据此把后续 `/uplink`、`/client` 连到归属实例,当前非归属则切换。
- 因 E2EE 按 `realm_id` 锚定,切换物理域名/归属漂移不影响解密。
- 归属实例故障:会话断,客户端重连引导地址 → 重分配归属 → 重建会话(语义同单实例故障)。

### 6.4 阶段二受影响面(概览)

- DB:`relay_instances`、`user_home` 两表 + realm 单例。
- relay:实例注册/心跳、归属分配、登录响应下发归属与 realm、`/healthz` 增 instance 身份。
- 三端客户端:登录解析归属与 realm、连接重定向/重连、E2EE 按 realm 锚定 + 桌面 keychain 迁移。
- 文档:E2EE 锚定更新、每实例独立域名的部署拓扑(StatefulSet + per-pod service/独立域名)。
