# Auth — Atterm Session Token + OPAQUE + E2EE 钥匙环

> **Audience**: 实现或审计 atterm 鉴权层与端到端加密的工程师
> **Last updated**: 2026-07-24
> **Status**: stable
> **See also**: [protocol.md](./protocol.md) · [architecture.md](./architecture.md) · [../superpowers/specs/2026-06-15-relay-e2ee-design.md](../superpowers/specs/2026-06-15-relay-e2ee-design.md)

v0.3.x：登录响应新增 `realm_id` + `home_instance_url`（多实例路由，见 §4.3）；`remote_permission=full` 现在也门控远程文件浏览器的 `FS_REQUEST`（见 §2、§10）。详见 [architecture.md](./architecture.md) §Relay 多实例架构。

## 目录

1. [设计目标与历史](#1-设计目标与历史)
2. [概念模型](#2-概念模型)
3. [数据模型](#3-数据模型)
4. [鉴权流](#4-鉴权流)
5. [传输层](#5-传输层)
6. [requireSession 中间件](#6-requiresession-中间件)
7. [公开路由白名单](#7-公开路由白名单)
8. [Bootstrap admin 详解](#8-bootstrap-admin-详解)
9. [安全考量](#9-安全考量)
10. [错误码字典](#10-错误码字典)
11. [客户端实现要点](#11-客户端实现要点)
12. [OPAQUE 与 account_key（E2EE 钥匙环）](#12-opaque-与-account_keye2ee-钥匙环)

---

## 1. 设计目标与历史

atterm 早期同时存在三套独立的鉴权凭据：长期 API token (`atk_…`)、全局 share-secret (`Config.Token`)、HTTP-only cookie 加 CSRF。三套机制各自的代码路径、错误模型、客户端存储方式都不同，每加一个 endpoint 都要决定走哪一条，是 bug 的高发源。

`relay-auth-token-removal` 系列（PR #133-#137，spec 见 [2026-06-09-relay-auth-token-removal-design.md](../superpowers/specs/2026-06-09-relay-auth-token-removal-design.md)）把这三套合并为一条：

- 所有客户端通过邮箱 + 密码登录获得 `session_token`
- `session_token` 在客户端持久化（localStorage / Keychain / 桌面 config），在请求中通过 `Authorization: Bearer` 或 `Sec-WebSocket-Protocol: atterm-token.<token>` 携带
- 服务端用 `requireSession` 中间件统一拦截，命中即把 `*User` 注入 request context

这份文档是该模型的唯一权威源——所有其他 spec（`protocol.md`、`architecture.md`、`AGENTS.md`、`README.md`）在涉及鉴权时反向链接到这里。

## 2. 概念模型

```mermaid
flowchart LR
    User[(User<br/>account)]
    Session[session_token<br/>30 day TTL]
    Pairing[pairing_token<br/>5 min one-shot]
    Inv[invitation_code]
    Bearer[Bearer / subprotocol]
    Mw[requireSession]
    Ctx["*User in ctx"]

    Inv -->|POST /signup| User
    User -->|POST /login| Session
    User -->|POST /pair/create| Pairing
    Pairing -->|POST /pair/consume| Session
    Session --> Bearer
    Bearer --> Mw
    Mw --> Ctx
```

| 概念 | 生命周期 | 持久化 | 用途 |
|---|---|---|---|
| `User` | 永久（直到 admin disable / delete） | `users` 表 | 身份载体 |
| `session_token` | 30 天 TTL（无滑动续期） | `sessions` 表（DB 存 sha256，明文仅响应一次） | 客户端日常凭据 |
| `pairing_token` | 5 分钟，一次性消费 | `pairing_tokens` 表 | QR 配对、跨设备引导 |
| `invitation_code` | 7 天 TTL，一次性消费 | `invitations` 表 | 邀请新用户注册 |

`remote_permission`（`view` / `control` / `full`，session owner 在 `SessionInfo` 里发布，见 [protocol.md](./protocol.md) §权限矩阵）不是账号鉴权的一部分，但门控同一 session 内的能力上限：`full` 是唯一允许发起远程文件浏览器 `FS_REQUEST`（浏览/编辑/CRUD/回收站）的等级，`internal/relay/client_conn.go` 在校验完 session token 之后再按此字段拒绝或放行（`permission_denied`，见 §10）。

## 3. 数据模型

数据库 schema 来源：`internal/userstore/migrations/0001_init.sql`。

### 3.1 `users`

```sql
CREATE TABLE users (
    id          TEXT PRIMARY KEY,
    email       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    auth_mode   TEXT NOT NULL DEFAULT 'opaque',  -- migration 0003 DROP 了旧 password_hash 列
    is_admin    INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL,
    disabled_at INTEGER
);
```

- 密码走 OPAQUE：口令材料存 `user_opaque_records.record`（见 §12.3），relay 没有 `password_hash`、也没有 bcrypt/argon2id 口令哈希回退路径。`CreateOpaqueUser` 建行、`StoreOpaqueRecord` 写记录
- `disabled_at` 非空时，所有 `LookupSession` 调用对该用户的 session 都返回 `ErrSessionInvalid`，相当于踢出所有设备

### 3.2 `sessions`

```sql
CREATE TABLE sessions (
    id_hash      TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL DEFAULT 0,
    user_agent   TEXT NOT NULL DEFAULT '',
    ip_prefix    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);
```

- `id_hash` 是明文 `session_token` 的 sha256-hex；明文只在 `CreateSession` 返回值里出现一次，之后 DB 只留哈希
- `last_seen_at` 当前未由 requireSession 主动更新（保留字段供未来 last-active UI 使用）
- 列出 user 的活跃 session 用 `sessions_user_idx`；过期扫描 sweep 用 `sessions_expires_idx`

### 3.3 `pairing_tokens`

```sql
CREATE TABLE pairing_tokens (
    token_hash   TEXT PRIMARY KEY,
    prefix       TEXT NOT NULL,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    consumed_at  INTEGER
);
CREATE INDEX pairing_tokens_user_idx ON pairing_tokens(user_id);
```

- `consumed_at` 非空表示已被消费；同一 token 二次 `POST /api/pair/consume` 返回 `409 Conflict`
- TTL 5 分钟（`internal/userstore/pairing.go::DefaultPairingTTL`）

## 4. 鉴权流

每条流给一个 mermaid 时序图加步骤列表。所有 endpoint 在 §10 错误码字典中列出失败响应。

### 4.1 Bootstrap admin（首次启动）

设置 `ATTERM_BOOTSTRAP_ADMIN_EMAIL` 时，relay 每次启动为该 email mint 一个一次性 **claim token** 并打印到日志。密码认证走 OPAQUE（relay 不接收明文密码），所以 bootstrap 不再用密码 env——claim token 是 OPAQUE 注册的「会合凭据」，操作员用它完成注册后自动获得 admin 角色。

```mermaid
sequenceDiagram
    participant E as 环境变量
    participant R as atterm-relay (启动)
    participant DB as users + claim_tokens
    participant Op as 操作员 (浏览器 /signup.html)
    E->>R: ATTERM_BOOTSTRAP_ADMIN_EMAIL
    R->>DB: CreateClaimToken(email, "admin", 7d)
    DB-->>R: claim_token (plaintext)
    R-->>Op: 日志: "bootstrap-admin: claim token for <email>: <token>"
    Op->>R: POST /api/auth/register/{init,finalize}<br/>{email, OPAQUE record, claim_token}
    R->>DB: 校验 token 邮箱匹配 → CreateOpaqueUser → 消费 token → SetUserAdmin
    DB-->>Op: session_token (登录态)
```

步骤：

1. 操作员在启动 relay 前设置 `ATTERM_BOOTSTRAP_ADMIN_EMAIL`（公网监听必需，除非 `--dev-insecure`）
2. relay 启动时 `cmd/atterm-relay/bootstrap_admin.go::bootstrapAdmin` 调 `CreateClaimToken(email, "admin", 7d)`，把明文 token 打印到日志（7 天有效、单次使用）
3. 操作员在 `/signup.html` 用该 email + 自设密码完成 OPAQUE 注册，并把 claim token 填进「邀请码 / claim token」框；`register/finalize`（`internal/relay/opaque_auth.go`）校验 token 邮箱匹配 → `CreateOpaqueUser` → `ConsumeClaimToken` → `SetUserAdmin`
4. 注册即登录，后续可 `curl -H "Authorization: Bearer <session_token>" http://relay/api/me` 验证

注意：claim token 只能用于**注册新账号**——若该 email 已注册，`CreateOpaqueUser` 返回 `ErrEmailTaken`（409），token 无法消费；此时改用 SQL 直接提权（`UPDATE users SET is_admin=1 WHERE email=…`）。详见 §8。

### 4.2 注册（OPAQUE，邮箱 + 密码，可选 claim token）

> 注册走 OPAQUE 两步握手，relay 不接收明文密码。旧的密码端点 `POST /api/auth/signup` 已下线（返回 410）。完整套件 / `account_key_wrap` / 时序见 §12。

要点（`internal/relay/opaque_auth.go`）：

1. 客户端 `POST /api/auth/register/init` → `POST /api/auth/register/finalize`；密码只在客户端参与 OPAQUE。
2. finalize body 带 `email`、OPAQUE `RegistrationRecord`、`account_key_wrap`，以及**可选** `claim_token`。
3. `claim_token` 只用于授予角色（bootstrap admin，见 §4.1 / §8）；注册本身**不被邀请码门控**，留空也能注册。
4. 成功一次性返回 `session_token` + user 摘要；客户端持久化（web → sessionStorage / localStorage；mobile → Keychain；desktop → `appConfig.RelaySessionToken`）。

### 4.3 登录（OPAQUE，邮箱 + 密码）

> 旧的密码端点 `POST /api/auth/login`（argon2id `VerifyPassword`）已下线（返回 410）。

要点：

1. `POST /api/auth/login/init` → `POST /api/auth/login/finalize`；本地用密码跑 OPAQUE，relay 全程接触不到密码或口令哈希。
2. finalize 返回 `session_token` + `account_key_wrap`（客户端本地解开拿回 32B `account_key`）+ user 摘要 + `realm_id` + `home_instance_url`（v0.3.x 多实例栈新增，见下）。
3. 后续请求带 `Authorization: Bearer <session_token>`；WS 升级带 `Sec-WebSocket-Protocol: atterm-token.<session_token>`。
4. 特权操作（`DELETE /api/me` 等）需 step-up：再走一次 `/api/auth/stepup/{init,finalize}` 换 60s 单次有效的 `step_up_token`（`internal/relay/opaque_stepup.go`）。

完整 OPAQUE 套件、endpoint、数据模型与时序见 §12。`GET /api/me`（Bearer）→ `LookupSession(sha256(token))` → user 摘要，这部分与认证方式无关。

**登录响应新增字段（v0.3.x，多实例路由）**：`internal/relay/opaque_auth.go::loginFinalizeResponse` 在 `session_token` / `account_key_wrap` / `user` 之外，携带：

| 字段 | 来源 | 客户端行为 |
|------|------|------------|
| `realm_id` | `internal/userstore/realm.go` 的 `relay_realm_state`（集群共享、永不变） | 只读；用于校验自己连的是同一 realm（跨实例 E2EE `account_key` 派生一致性）。客户端**不应**接受来自不同 `realm_id` 的 `account_key_wrap` 混用 |
| `home_instance_url` | `internal/relay/node_home.go::resolveHomeInstanceURL`，按用户偏好或任选一个活跃实例 | 客户端登录后若当前连接的 relay ≠ `home_instance_url`，自行发起 reconnect 到该实例；`PUT /api/me/home` 可改用户偏好 |

`register/finalize` 响应（`registerFinalizeResponse`）目前只带 `realm_id`（无 `home_instance_url`——新用户尚无偏好，首次登录才落地）。多实例部署细节（`relay_instances` 心跳、`GET /api/nodes`）见 [architecture.md](./architecture.md) §Relay 多实例架构。

### 4.4 配对（QR + 一次性 pairing token）

让一台已登录设备给一台新设备颁发 session，不让新设备输入密码。

```mermaid
sequenceDiagram
    participant Old as 已登录设备
    participant R as Relay
    participant DB as pairing_tokens + sessions
    participant New as 新设备 (无凭据)
    Old->>R: POST /api/pair/create<br/>(Authorization: Bearer ...)
    R->>DB: CreatePairingToken(user.ID, 5min TTL)
    DB-->>R: pair_token (plaintext)
    R-->>Old: 200 {token, expires_at, qr_url}
    Note over Old: 在屏幕上显示 QR 码
    New->>New: 用相机扫码<br/>读取 pair_token
    New->>R: POST /api/pair/consume {token}
    R->>DB: ConsumePairingToken(sha256(token))
    DB-->>R: user / ErrPairingNotFound / ErrPairingExpired / ErrPairingConsumed
    R->>DB: CreateSession(user.ID, ua, "", DefaultSessionTTL)
    DB-->>R: session_token (plaintext)
    R-->>New: 200 {session_token, expires_at, user, relay_url}
    Note over New: persist session_token
```

步骤：

1. 老设备 `POST /api/pair/create`（受 requireSession 保护，需 owner Bearer）
2. relay 生成 5 分钟一次性 pair token，记录到 `pairing_tokens`
3. 老设备把明文 token 嵌入 QR URL 显示在屏幕上
4. 新设备扫码，提取 token，`POST /api/pair/consume`（公开 endpoint，无凭据）
5. relay 原子地：标记 pair token consumed → mint 新设备的 session token → 返回
6. 新设备就是一台已登录设备

错误码：见 §10。

### 4.5 撤销

三条路径：

| 触发者 | 接口 | 影响范围 |
|---|---|---|
| 用户自己（当前设备） | `POST /api/auth/logout` | 当前 session_token 失效 |
| 用户自己（其他设备） | `DELETE /api/me/sessions/{id_hash}` | 指定 session 失效 |
| 用户自己（一键登出） | `POST /api/me/sessions/sign-out-others` | 当前 session 以外全部失效 |
| Admin | `POST /api/admin/users/{id}/reset-password` | 用户密码重置 + 所有 session 失效 |
| 系统 | `expires_at < now` | session 失效（懒检查，下次 requireSession 命中即返回 401） |

logout 的最简姿势：

```mermaid
sequenceDiagram
    participant C as Client
    participant R as Relay
    participant DB as sessions
    C->>R: POST /api/auth/logout<br/>(Authorization: Bearer ...)
    R->>DB: DeleteSession(sha256(token))
    DB-->>R: ok
    R-->>C: 204 No Content
    Note over C: clear localStorage / Keychain<br/>redirect to /login
```

## 5. 传输层

### 5.1 HTTP `Authorization: Bearer <token>`

桌面 / 移动 / 服务端到服务端的请求都使用这种姿势。token 是明文 `ses_…`（4 字符前缀 + 43 字符 base64url）。

```http
GET /api/me HTTP/1.1
Host: relay.example.com
Authorization: Bearer ses_abc123def456...
```

### 5.2 WS `Sec-WebSocket-Protocol: atterm-token.<token>`

浏览器 WebSocket API 不允许设置 `Authorization` header，所以 WS 升级时 token 走子协议头。relay 接受两种格式：

- 原文：`atterm-token.<plaintext>`
- base64url 编码：`atterm-token-b64.<base64url(plaintext)>`

后者用于 token 含子协议非法字符的极端情况（实际中不会发生，但客户端可以选）。

```http
GET /client HTTP/1.1
Host: relay.example.com
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Key: ...
Sec-WebSocket-Version: 13
Sec-WebSocket-Protocol: atterm-token.ses_abc123def456...
```

服务器升级响应必须在 `Sec-WebSocket-Protocol` 头里回选与请求里出现的同一个子协议（`atterm-token.<...>` 或 `atterm-token-b64.<...>`），否则浏览器 abort 连接。

### 5.3 拒绝的姿势

- `?token=<...>` URL query — 永远拒绝。query 会进访问日志、HTTP referrer、浏览器历史，泄露面太大
- `Cookie: atterm_session=...` — 永远拒绝。Phase 1 删除 cookie 路径，避免 CSRF 攻击面和跨站请求歧义
- `X-Auth-Token: <...>` 或其他自定义 header — 不读

## 6. requireSession 中间件

实现位置：`internal/relay/auth.go`。所有受保护路由在 mux 注册时被这个中间件包裹。

行为：

1. **提取**：调用 `tokenFromRequest(r)`，按优先级尝试三种来源：
   - `Authorization: Bearer <token>` HTTP header
   - `Sec-WebSocket-Protocol: atterm-token.<token>` 子协议头
   - `Sec-WebSocket-Protocol: atterm-token-b64.<base64url(token)>` 子协议头
   提取不到则返回 `401 unauthorized`
2. **哈希查表**：sha256(plaintext) 得到 `id_hash`，查 `sessions` 表
3. **校验**：要求 `expires_at > now` 且关联 user 的 `disabled_at IS NULL`
4. **注入**：把 `*User` 写入 request context（key 为 `userCtxKey{}`），调用 inner handler
5. **未命中**：返回 `401 unauthorized`，**不区分** "token 不存在" / "已过期" / "user disabled" — 避免给攻击者额外信号

handler 内部用 `UserFromContext(r.Context())` 读取注入的用户。

错误响应统一为：

```http
HTTP/1.1 401 Unauthorized
Content-Type: text/plain

unauthorized
```

## 7. 公开路由白名单

只有这些 endpoint 不被 `requireSession` 包裹：

| 方法 | 路径 | 公开原因 |
|---|---|---|
| `POST` | `/api/auth/register/{init,finalize}` | 新用户尚无凭据（OPAQUE 注册） |
| `POST` | `/api/auth/login/{init,finalize}` | 凭据交换入口（OPAQUE 登录） |
| `POST` | `/api/pair/consume` | 新设备尚无凭据，pair token 自身就是临时凭据 |
| `GET` | `/healthz` | 监控探针；不暴露任何内部状态 |
| `GET` | `/version` | 客户端版本协商 |
| `GET` | `/`, `/login.html`, `/signup.html`, 等静态资源 | 静态 SPA bootstrap |

所有其他 endpoint（含 `/api/me/*`, `/api/pair/create`, `/api/auth/logout`, `/api/push/*`, `/api/webhooks/*`, `/api/admin/*`, `/api/sessions`, `/api/sessions/seen`, WS `/agent`, `/uplink`, `/client`, `/client-sessions`）都通过 `requireSession`。

部分 endpoint 在 `requireSession` 之外还套一层 `requireAdmin`（`internal/relay/admin_http.go`），要求 `user.IsAdmin == true`。

## 8. Bootstrap admin 详解

### 8.1 触发条件

- 环境变量 `ATTERM_BOOTSTRAP_ADMIN_EMAIL` 非空、是合法邮箱 → relay 每次启动 mint 一个 `role="admin"` 的 claim token。
- 未设置 → 跳过（仍正常启动，首个 admin 需经其他途径，如 SQL `UPDATE users SET is_admin=1`）。

不再有 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` 及其密码强度校验——密码随 OPAQUE 迁移（#159）移到客户端，relay 永不接收明文密码。

### 8.2 日志输出格式

`cmd/atterm-relay/bootstrap_admin.go` 用 `log.Printf` 输出三行：

```
2026/06/19 12:34:56 bootstrap-admin: claim token for you@example.com (expires in 7 days):
2026/06/19 12:34:56     clm_abc123def456...
2026/06/19 12:34:56 bootstrap-admin: complete OPAQUE registration via POST /api/auth/register/finalize with this token in claim_token to receive admin role
```

注意：

- claim token 明文只在日志出现一次，不可找回；过期（7 天）或丢失就重启 relay 重新 mint
- 单次使用：注册消费后即作废（`ConsumeClaimToken` 的原子 UPDATE 串行化并发 finalize）
- token 只能用于注册**新** email；已注册的 email 无法消费（注册返回 409 email taken）

### 8.3 CI / 桌面 App 对接

CI 场景：

```bash
docker run -d \
  -e ATTERM_BOOTSTRAP_ADMIN_EMAIL=admin@ci.local \
  -e ATTERM_ORIGINS=https://ci.example.com \
  -p 8080:8080 atterm-relay
# 等几秒，从日志抓 claim token：
docker logs <container> | grep -A1 'claim token for' | tail -1 | awk '{print $NF}'
# 把它喂给注册脚本/客户端的 OPAQUE register 流程（claim_token 字段）
```

桌面 App 场景：本地 mini-relay 走**独立**的 bootstrap 路径——`desktop/relay_host.go::startRelayHost` 用 `local@atterm.local` + `appConfig.LocalAdminPassword`（首次启动随机生成）调 `bootstrapLocalAdmin` 直接建用户 + mint session（loopback only，不经公网 claim-token 流程）；session token 持有在 `relayHost.sessionToken`，前端通过 Wails `GetEndpoint()` 拿到。

## 9. 安全考量

### 9.1 token 存储

DB 只存 sha256(plaintext) — 数据库 dump 不能直接得到可用 token。

哈希算法选 sha256 而非 argon2id 的原因：session token 本身是 32 字节随机 + base64url 编码，熵足够大（256 bit），不需要加盐慢哈希。argon2id 留给密码（用户选的低熵字符串）。

明文 token 仅在生成时返回给客户端一次，relay 内存中也不长留。

### 9.2 SameSite / CSRF / cookie — 为什么都不用

Phase 1 之前 atterm 用 HttpOnly cookie + CSRF middleware，意图是：

- Cookie 让浏览器自动带凭据（用户体验好）
- CSRF middleware 防止跨站请求伪造

session_token 模型放弃了 cookie，原因：

- 客户端有三种（桌面、Web、iOS Capacitor），三者中只有 Web 是浏览器；桌面 / iOS 都无 cookie jar 概念
- 统一用 Bearer / subprotocol 后，三端的请求路径完全一致，少一套分支逻辑
- 没有 cookie 自动携带，就没有 CSRF 攻击面，整套 CSRF middleware 可以删除

代价：客户端必须显式管理 token 存储（不再"自动"）。

### 9.3 移动端 Keychain 存储

iOS Capacitor app 通过 `desktop/frontend/src/platform/capacitor.ts::STORAGE_KEY = "atterm.relay.session"` 在 iOS Keychain 中保存 session token。Keychain 由 iOS 系统加密，应用沙盒外不可读。

Web 浏览器没有等价存储——只能用 localStorage。XSS 风险：单一可信源（atterm 自家域）+ 严格 CSP（在 relay `internal/relay/server.go` 设置）让风险可控；但仍弱于 Keychain。

### 9.4 桌面 App localAdminPassword 在 config.json 的取舍

桌面 App 启动 mini-relay 时需要一个稳定密码做 bootstrap（每次启动从 `local@atterm.local` 登录）。该密码持久化在 `~/.config/atterm/config.json` 的 `local_admin_password` 字段，明文存储。

权衡：

- 只 `localhost` 监听，外部无法访问该 relay
- 密码泄露的风险等价于 config.json 本身的文件系统权限（用户家目录）
- 用 OS Keychain 存可以提升安全级别，但开发复杂度 + 平台依赖增加；当前接受现状

跟踪项见 `docs/roadmap.md` §鉴权后续。

## 10. 错误码字典

所有失败响应。

| 状态码 | 路径 / 上下文 | 触发条件 | 客户端建议 |
|---|---|---|---|
| `401` | 任意 requireSession 保护的 endpoint | token 缺失 / 哈希查不到 / `expires_at < now` / user disabled | 清本地 token，跳登录 |
| `400` | `POST /api/auth/login/{init,finalize}` | JSON / OPAQUE 消息解析失败 | 校验表单 |
| `401` | `POST /api/auth/login/finalize` | OPAQUE 校验失败（密码错 / 用户不存在，不区分） | 提示"邮箱或密码错" |
| `401` | `POST /api/auth/register/finalize` | `claim_token` 无效 / 过期 / 已用 / 邮箱不匹配 | 重新获取 claim token |
| `409` | `POST /api/auth/register/finalize` | 邮箱已被注册 | 提示用户去登录 |
| `410` | `POST /api/auth/{login,signup}`（旧密码端点） | 端点已下线 | 升级客户端走 OPAQUE |
| `400` | `POST /api/pair/consume` | JSON 解析失败、token 空 | 重新扫码 |
| `404` | `POST /api/pair/consume` | token 不存在 | 重新让老设备生成 |
| `409` | `POST /api/pair/consume` | token 已被消费 | 重新让老设备生成 |
| `410` | `POST /api/pair/consume` | token 已过期（5 分钟 TTL 用完） | 重新让老设备生成 |
| `403` | `POST /api/admin/*` | user.IsAdmin == false | 提示无权限 |
| `204` | `POST /api/auth/logout` | 正常 | 清本地 token |
| `500` | 任何 endpoint | DB 故障等 | 重试或提示后端故障 |

WS 升级失败统一表现为 HTTP 状态码（在 upgrade 之前），客户端在 `onerror` 中读取 status code。

**`FS_REQUEST`（帧协议层，非 HTTP 状态码）**：鉴权通过、session 已 attach 后，`internal/relay/client_conn.go` 还会按 `remote_permission` 拒绝，回一个 `FS_RESPONSE{error}` 帧而非断连：

| `error` 值 | 触发条件 | 客户端建议 |
|---|---|---|
| `invalid_request` | payload 解析失败 / `request_id`、`op` 缺失 / `session_id` 不匹配 | 视为客户端 bug，不重试 |
| `permission_denied` | `remote_permission != full`；或 `open_external` 且 scope 非 write | 隐藏/禁用远程文件浏览器入口 |
| `upstream_unavailable` | 对应桌面 host 未连接 mirror（`DriverFromUpstream()` 为假） | 提示"owner 设备离线"，可重试 |
| `driver_required` | `open_external` 但当前客户端不是 driver | 提示"需先取得 driver" |

完整 `FS_REQUEST`/`FS_RESPONSE`/`FS_EVENT` 帧 schema 与权限矩阵见 [protocol.md](./protocol.md)。

## 11. 客户端实现要点

### 桌面（Wails + Vue）

- 远程 relay 登录：`desktop/app.go::LoginRemoteRelay` Wails 方法，POST 到 `/api/auth/login`，把响应 `session_token` 写入 `appConfig.RelaySessionToken`
- 本地 mini-relay：`desktop/relay_host.go::startRelayHost` 启动时 bootstrap 本地 admin，`relayHost.sessionToken` 持有
- HTTP 请求统一带 Bearer：`desktop/app.go` 中的 `FetchRelayMe` / `CreatePairingToken` 等方法
- WS upstream：`desktop/uplink.go::runOnce` 通过 HTTP header 设 `Authorization: Bearer`

### Web (`web/src/`)

- 统一入口 `web/src/shared/api/client.ts::apiFetch` — 自动读取 localStorage 的 `RelayConfig.sessionToken`，挂 Bearer header
- 登录持久化 `web/src/shared/api/auth.ts::login` — 把响应写入 `saveRelayConfig`
- 401 拦截：`apiFetch` 检测到 401 自动 `clearRelayConfig()` + 跳 `/login.html?next=...`
- 浏览器 WS：`web/src/shared/ws/client-conn.ts::openWS` 通过子协议传 token

### iOS Capacitor (`desktop/frontend/src/`)

- 配对消费：`desktop/frontend/src/platform/capacitor.ts::consumePairing` 解析 `{session_token, expires_at, relay_url, user}`
- Keychain 存储：`STORAGE_KEY = 'atterm.relay.session'` 在 `capacitor.ts:6`
- HTTP 请求：用 `Authorization: Bearer`，`credentials: 'omit'` 显式禁 cookie

---

## 12. OPAQUE 与 account_key（E2EE 钥匙环）

§1–11 描述的是 **服务端可信** 的 session 鉴权层：relay 看得到密码哈希、session token 哈希、和明文请求内容。E2EE（M1–M6 系列，至 v0.2.110 落地）把内容保护从 transport 层延伸到 relay 不可读的端到端密文，靠两个独立的密钥：

- **OPAQUE PAKE** — 注册 / 登录 / step-up 时双方协商出 session key，relay 全程**接触不到密码或派生的口令哈希**。
- **`account_key`** — 客户端本地随机生成的 32 字节内容主密钥；用密码派生的 wrap key + XChaCha20-Poly1305 封装存 relay 一份 `account_key_wrap` blob，登录时本地解开。所有 sealed 字段（`OUT`、`SessionInfo.Sealed`、`MetaPayload.Sealed`、`CommandEventPayload.SealedBody`）都用 `HKDF-SHA256(account_key, "atterm-session-v1" ‖ session_uuid)` 派生的 session key AEAD 封装。

完整威胁模型、cipher suite 论证、key 派生链、AAD 鉴别表、按里程碑的实现状态见 [../superpowers/specs/2026-06-15-relay-e2ee-design.md](../superpowers/specs/2026-06-15-relay-e2ee-design.md)。本章只覆盖 **auth.md 层面要点**：协议套件、相关 endpoint、数据库字段、step-up token、客户端持久化路径。

### 12.1 OPAQUE 协议套件

两端硬绑定的套件：

| 参数 | 值 | 备注 |
|------|-----|------|
| Key exchange | `OPAQUE_P256`（P-256-SHA256） | 选 P-256 是 [@cloudflare/opaque-ts](https://github.com/cloudflare/opaque-ts) 唯一支持的，覆盖浏览器/桌面/iOS |
| Memory-hard function | `Scrypt` | 同上；bytemare 默认是 Argon2id 但 TS 端不支持 |
| `ServerIdentity` | `"atterm-relay"`（UTF-8） | 双端硬编码；绑进 AKE transcript |
| `ClientIdentity` | 用户邮箱 | 同上；防止 evil-server pinning |
| 实现 | Go：`bytemare/opaque@v0.10.0`；TS：`@cloudflare/opaque-ts@^0.7.5` | 两端跨语言互操作有 contract test |

**红线（也是 [AGENTS.md](../../AGENTS.md) §20）**：改套件必须两端一起改。Go 一边换 ristretto255 / TS 一边升新版本都会让握手当场断在 `unsupported KE2 length`。

### 12.2 OPAQUE endpoint

实际路径**不带** `/opaque/` 段（见 `internal/relay/opaque_auth.go::Register`）：

| 路径 | 用途 | 输入 | 输出 |
|------|-----|------|------|
| `POST /api/auth/register/init` | 注册第一步 | `email`、`opaque.RegistrationRequest`（base64） | `opaque.RegistrationResponse`（base64） |
| `POST /api/auth/register/finalize` | 注册第二步 | `email`、`opaque.RegistrationRecord`、`account_key_wrap`（见 §12.3）、**可选** `claim_token` | `session_token` + `*User` 摘要 |
| `POST /api/auth/login/init` | 登录第一步 | `email`、`opaque.KE1` | `opaque.KE2`（含 envelope） |
| `POST /api/auth/login/finalize` | 登录第二步 | `email`、`opaque.KE3` | `session_token`、`account_key_wrap`、`*User` |
| `POST /api/auth/stepup/init` | 特权操作再认证 | 同 login/init | KE2 |
| `POST /api/auth/stepup/finalize` | 特权操作再认证 | 同 login/finalize | `step_up_token`（60 秒、单次有效） |

注册不再用 `invitation_code` 门控；`register/finalize` 的可选 `claim_token`（operator 启动时打印，见 §4.1）只用于授予角色（如 admin）。旧的密码端点 `POST /api/auth/{login,signup}` 返回 410。

实现：`internal/relay/opaque_server.go`（套件 + persistence）、`internal/relay/opaque_auth.go`（register/login HTTP handler）、`internal/relay/opaque_stepup.go`（step-up）。

### 12.3 数据模型（OPAQUE / E2EE 扩展）

§3 已经列了 `users` / `websessions` / `pairing_tokens` / 邀请码 / webhooks 几张表。OPAQUE + E2EE 由 `migrations/0003_opaque_auth.sql` 引入：

| 表 / 字段 | 类型 | 来源 | 备注 |
|------|------|------|------|
| `users.auth_mode` | TEXT NOT NULL DEFAULT 'opaque' | migration 0003 | 旧 `password_hash` 列在同一个 migration 里被 DROP 掉；OPAQUE 之后没有 bcrypt 回退路径 |
| `user_opaque_records.record` | BLOB | OPAQUE 注册时 `bytemare/opaque.RegistrationRecord` 的 serialize 输出 | 服务端永远只有这份；丢了等于密码丢了 |
| `user_account_key_wraps.{wrapped,nonce,salt,kdf_params,method}` | BLOB / TEXT | 客户端 `AccountKeyWrap{ method, wrapped, nonce, salt, kdf_params }` | 见 §12.4。`PRIMARY KEY (user_id, method)`，方便未来加 `method = 'recovery_code'` 等多重 wrap |
| `opaque_server_state.{oprf_seed,server_ake_sk,server_ake_pk,suite}` | BLOB / TEXT | 首次启动时随机生成 | 单行（`id = 1`），跨重启稳定；丢了等于所有用户得重新注册 |

**Step-up token 是 in-memory, 不入库**：`internal/relay/opaque_stepup.go` 维护一个进程内 map `{token → (userID, expiresAt)}`。明文 token 格式 `stepup_<base64url(32 random bytes)>`，60 秒 TTL，单次有效（`ConsumeStepUpToken` 在校验通过后立即 `delete` 表项）。Relay 重启 = step-up 池清零，用户需重新走 init/finalize；该取舍是有意的（重启窗口本身就是异常状态，且 in-memory 比 DB token 难偷）。

### 12.4 `account_key_wrap` blob

客户端注册时本地随机生成 32 字节 `account_key`，用密码派生 wrap key 把它封装：

```text
salt        = randomBytes(16)
wrap_key    = Argon2id(password, salt, m=64 MiB, t=3, p=1, dkLen=32)
nonce       = randomBytes(24)
ciphertext  = XChaCha20-Poly1305.encrypt(
                key       = wrap_key,
                nonce     = nonce,
                aad       = "atterm-account-key-v1",
                plaintext = account_key,
              )

AccountKeyWrap = {
  "method":     "password",
  "wrapped":    base64(ciphertext),
  "nonce":      base64(nonce),
  "salt":       base64(salt),
  "kdf_params": "{alg:argon2id, m:65536, t:3, p:1}",
}
```

注册 finalize 时上传，登录 finalize 时下发。AAD 字符串硬编码两端，防止把 wrap blob 错误地当成别的密文解开。改 KDF 参数时两端同时改，旧 wrap 自带参数所以历史 wrap 不受影响（参数读自 `kdf_params`）。

### 12.5 鉴权流（OPAQUE 注册 / 登录 / step-up）

```text
注册：
  Client                              Relay
  ───────────────────────────────────────────────────
  生成 RegistrationRequest(password)
  ─── POST /api/auth/register/init ──►
                                      email 唯一性检查
                                      bytemare/opaque.Server.RegistrationResponse
  ◄── RegistrationResponse ──────────
  RegistrationRecord(server_resp,
                     server_identity="atterm-relay",
                     client_identity=email)
  生成 account_key = randomBytes(32)
  AccountKeyWrap(password, account_key)
  ─ POST /api/auth/register/finalize ►
                                      (可选 claim_token → 校验+提权)
                                      持久化 users.opaque_record + account_key_wrap
                                      生成 session_token (明文返回一次)
  ◄── { session_token, user } ───────
  本地 saveAccountKey(account_key)


登录：
  Client                              Relay
  ───────────────────────────────────────────────────
  KE1 = ake1(password)
  ─── POST /api/auth/login/init ─────►
                                      ake1 + opaque_record → KE2
  ◄── KE2 ────────────────────────────
  KE3 = ake3(KE2)
  ─── POST /api/auth/login/finalize ─►
                                      ake4(KE3) → 验通过
                                      新 session_token (明文返回一次)
  ◄── { session_token, account_key_wrap, user }
  account_key = unwrapWithPassword(password, account_key_wrap)
  saveAccountKey(account_key)


step-up（DELETE /api/me 等特权操作）：
  Client                              Relay
  ───────────────────────────────────────────────────
  KE1 = ake1(password)
  ─── POST /auth/stepup/init ────────►  （走和登录一样的 OPAQUE，但 finalize 路径不同）
  ◄── KE2 ────────────────────────────
  KE3 = ake3(KE2)
  ─── POST /auth/stepup/finalize ────►
                                      生成 step_up_token (60s)
  ◄── { step_up_token } ─────────────
  ─── DELETE /api/me ────────────────►
       Authorization: Bearer <session>
       X-Step-Up-Token: <step_up_token>
                                      校验 token_sha256 + expires_at + consumed_at
                                      标记 consumed_at = now
                                      执行 delete
```

实现指针：

- `internal/relay/opaque_auth.go::handleRegisterFinalize` 写入 `opaque_record` + `account_key_wrap` 在同一个事务里。
- `internal/relay/opaque_stepup.go::MintStepUpToken` 生成 32 字节随机 token、塞进进程内 map（不入库）、明文返回一次；`ConsumeStepUpToken` 校验通过后立即从 map 删掉。
- `internal/relay/me_delete_http.go::handleDeleteMe` 拦截 `X-Step-Up-Token`，缺失或失败 → `403 step_up_required`。

### 12.6 安全考量（仅 auth 层相关，详细模型见 e2ee design）

- **没有密码恢复**：忘记密码 = admin reset = 新 `opaque_record` + 新 `account_key_wrap` + 客户端重新生成 `account_key`。旧会话的 ringbuf 密文（如果 relay 持久化了）永久不可解。这是单用户自托管定位下的设计选择；不要给"备用问题 / 邮件链接"加分支，那会让 relay 重新拿到内容密钥。
- **OPAQUE record 不可重置**：服务端不存密码、不存能恢复密码的中间产物。被偷库的攻击者仍然要花暴破 OPAQUE envelope 的代价，不是简单的 sha256 撞库。
- **`account_key_wrap` 是客户端 secret，不应给到第三方**：admin 后台不能查；只在 login finalize 响应里下发给账号本人。
- **step-up token**：60s + 单次有效 + sha256 入库。明文只在 finalize 响应里出现，客户端立刻塞进 `X-Step-Up-Token` header 用掉，不要持久化。
- **`account_key` 永远不进 URL / 日志 / IndexedDB / postMessage 广播**（[AGENTS.md](../../AGENTS.md) §21）。SW 解密 push body 只能通过 `MessageChannel` 跟可见 client 协商一次性 reply。

### 12.7 客户端实现要点（OPAQUE / `account_key`）

| 平台 | OPAQUE 客户端 | account_key 持久化 |
|------|---------------|-------------------|
| Go（desktop） | `internal/e2eeclient`（bytemare/opaque@0.10.0 wrapper） | `zalando/go-keyring`（macOS Keychain / Linux Secret Service / Windows Credential Manager） |
| Web | `web/src/shared/lib/opaque.ts`（cloudflare/opaque-ts + noble/argon2id + noble/xchacha20-poly1305） | `sessionStorage["atterm.account-key"]`，tab 关掉就丢 |
| iOS Capacitor | `desktop/frontend/src/lib/opaque.ts`（同 web） | `mobile/ios/.../AttermSecureStorage.swift` Keychain plugin（红线 #15：plugin 必须装在 `mobile/package.json`） |

跨平台都遵守同一份 cache 注册表 `desktop/frontend/src/lib/account-key.ts`：上层模块 `setAccountKeyProvider()` 注册取 key 的函数，业务代码用 `getCurrentAccountKey()` 同步拿，避免每次解密都 round-trip 到 platform layer。

平台特异 fallback：

- **Desktop**（`desktop/account_key_store.go`）：`zalando/go-keyring` 调系统 Secret Service（macOS Keychain / Linux libsecret / Windows Credential Manager）。注册/登录成功后 `saveAccountKey(relayOrigin, userID, key)` 写入；boot 时 `loadAccountKey` 读出装到 `App.accountKey`（受 `accountKeyMu` 互斥保护），前端通过 `GetAccountKey` Wails binding 同步取。容器内无 D-Bus、钥匙串临时锁、Linux 桌面缺 `gnome-keyring-daemon` 等失败路径，按 `account_key_store.go:49` 注释「fall back to the in-memory state」处理——`account_key` 本次进程内继续可用，下次启动钥匙串不可用时则要求重新登录，但不会让 app 启动失败。
- **iOS**（`mobile/ios/App/App/Plugins/SecureStorage/AttermSecureStorage.swift`）：自定义 Capacitor plugin 用 `SecItemAdd` / `kSecAttrAccessibleAfterFirstUnlock`。**必须**按 [AGENTS.md](../../AGENTS.md) §15 在 `MainViewController.capacitorDidLoad()` 里 `bridge?.registerPluginInstance(...)` 注册；漏注册时 `desktop/frontend/src/platform/capacitor.ts` 检测到 `PLUGIN_NOT_AVAILABLE` 同样降级为 in-memory（行为同 Desktop fallback：本次可用，重启即丢）。CI 不构建 iOS，这条只能在真机 / 模拟器复测（参见 v0.2.36 / PR #101 修复）。
- **Web**：`sessionStorage["atterm.account-key"]` 写完即可用，无失败路径；tab close 自然清空，符合 §12.6 「不持久化到 IndexedDB」的红线。

### 12.8 已知 gap

完整列表见 e2ee design §"Known gaps after v0.2.110"。auth 层面值得记一笔的：

- **没有密码找回** — 见 §12.6，单用户自托管定位下不计划做。
- **OPAQUE 套件选择是"够用最广"而非"最强"** — P-256-Scrypt 是浏览器侧 TS 库唯一支持的；想升 ristretto255-Argon2id 需要 Cloudflare TS 库或自己实现，外部审计后再评。
- **`account_key_wrap` 的 wrap KDF 参数固定**（64 MiB / t=3 / p=1） — 老设备（旧 iPhone）可能慢；想做按设备协商需要在 `account_key_wrap.kdf_params` 里加 capability 字段并发回去存。M7-audit 列入。
- **step-up token 只覆盖 `DELETE /api/me`** — 未来若加"修改邮箱"、"修改密码"、"轮转 `account_key`" 等特权操作应该都过 step-up 门。
