# Accountless P2P Remote Access Implementation Plan

> Status: Proposed
> Date: 2026-09-22
> Prototype: `docs/prototypes/peer-connect/index.html`

## 1. Goal

在保留现有 Relay 账户模式的前提下，新增一套不依赖 atterm 账户的 Peer 连接模式：

- 桌面主机以本地设备身份发布本机终端会话。
- 新设备通过已有受信设备签发的邀请获得访问能力。
- 一个有邀请权限的设备可预签发一批单次邀请，之后逐个分享。
- 所有受信设备组成一个本地创建的 Peer Space；没有中心主副本，配置通过设备间 anti-entropy 最终一致。
- 无 Relay 时仍同步现有 portable preferences、profiles 与 SSH hosts；设备本地配置和密钥材料不进入同步。
- 邀请与连接路径解耦，同一套信任可使用官方/自建 Rendezvous、Cloudflare Quick Tunnel，以及后续 LAN、IPv6、手工地址等路径。
- Rendezvous 只负责发现与信令，不承载终端字节。
- WebRTC DataChannel 是默认数据路径；Quick Tunnel 后续可作为账户无关的加密 WebSocket 回退路径。
- Relay 与 Peer 可以独立启用。后续 Hybrid 模式优先 Peer，失败时回退 Relay。

这套模式解决的是“脱离 Relay 账户体系”，不承诺所有网络环境都能纯直连。MVP 不提供 TURN；对称 NAT 等无法建立直连的场景会明确报错，并在后续由 Quick Tunnel 或用户自行配置的 TURN/Relay 路径解决。

## 2. Product Model

四层必须彼此独立：

| Layer | Responsibility | Trust source | Carries terminal bytes |
|---|---|---|---|
| Peer trust | 设备身份、邀请、授权、撤销、权限 | 本地设备密钥与签名 capability | No |
| Reachability | Rendezvous、Quick Tunnel、LAN、IPv6 | 仅提供地址/交换信令，不决定授权 | Depends on route |
| Session transport | WebRTC DataChannel、Quick Tunnel WSS、Relay WS | 必须通过 Peer 或 Relay 各自鉴权 | Yes |
| Config replication | signed op log、version vector、snapshot | Peer Space membership + sync/vault epoch key | No terminal bytes |

连接路径：

```text
                               +--------------------------+
                               | official/self-hosted     |
new device -- signed invite -->| Rendezvous (signal only) |--+
      |                        +--------------------------+  |
      |                                                       v
      |        +-----------------------------------+    WebRTC DataChannel
      +------->| Quick Tunnel to local peer gateway|----------+ direct preferred
               +-----------------------------------+          |
                         | encrypted WSS fallback             v
                         +----------------------------> desktop peer host

Relay account login ----------------------------------> existing Relay path
```

“Peer mode”描述信任模型；“Direct / Quick Tunnel / Relay”描述当前实际数据路径。UI 不把经过 Cloudflare 的 WSS 回退标成“直连”。

## 3. Non-goals

MVP 不包含：

- 用 Peer 身份替代或迁移现有 OPAQUE Relay 账户。
- 在 Peer 模式中复用 `account_key`、Relay session token 或 `/api/pair/*` token。
- 密码恢复、云端设备目录、云端备份 Peer 私钥。
- Rendezvous 代存配置或充当“隐藏的中心数据库”。没有任意两台设备同时在线时，不承诺立即同步。
- TURN/SFU、自建 overlay network、libp2p DHT。
- 主机离线时访问终端；邀请可预签，但主机仍必须在线才能兑换和 attach。
- 任意受信设备默认拥有继续邀请他人的权力；该能力由单独的 `can_invite` capability 控制。
- 一个安装同时加入多个 Peer Space。MVP 每个安装只有一个 active Space；多身份/工作与个人空间后续再扩。
- 第一版同步 Web Push subscription、Relay 登录态、Relay URL/密码/token、Peer 私钥、邀请 secret/consume ledger。
- 第一版通过 Peer 传文件、远程文件浏览、Web Preview、远程创建 session。MVP 只开放 list/attach/view/control/resize。
- 把 Quick Tunnel 当作有 SLA 的平台服务；它始终标为实验性第三方路径。

## 4. Security Model

### 4.1 Identities

- 每个安装生成独立 Peer device identity，与 Relay 账户完全无关。
- 首台设备创建不可变的 signed genesis document（`space_id`、初始 admin、协议版本）；其 hash 是信任锚。后续设备拿到 space membership certificate，而不是分别加入每台主机的孤立 trust domain。
- 身份签名算法使用 P-256 ECDSA；会话密钥协商使用临时 P-256 ECDH。
- Desktop 私钥保存在 OS keyring；iOS 使用新的 Keychain plugin；浏览器使用 IndexedDB 中不可导出的 WebCrypto `CryptoKey`。
- `peer_id = base64url(SHA-256(canonical public key))`。
- 每台 desktop 仍是自己本地 session 的最终 owner，可在 Space grant 之上进一步收窄权限；Space membership 本身不覆盖 session owner 的决定。
- 不导入、不导出、不派生现有 Relay `account_key`。Peer 私钥、邀请 secret、会话密钥不得进入 URL query、日志或 Relay 配置。

P-256 的选择是为了让 Go、Safari/WKWebView、Chromium 和 Firefox 都能使用系统/WebCrypto 实现，不为前端新增密码学依赖。具体 key encoding、签名格式和跨语言向量必须在 P0 spike 后钉死。

### 4.2 Invitation and grant

邀请分两层，解决 Quick Tunnel URL 每次变化与“预签发”之间的冲突：

1. `CapabilityTicket`：不可变、预先签名、与网络路径无关。
2. `ConnectionBundle`：分享时加入当前 route hints，再由签发设备签名。

建议 wire format：

```text
atp1.<base64url(exact-json-bytes)>.<base64url(p1363-signature)>
```

签名直接覆盖原始 JSON bytes，验证端不得重新序列化 JSON，因此不依赖自制 canonical JSON。

`CapabilityTicket` 最少包含：

```json
{
  "v": 1,
  "invite_id": "uuid",
  "batch_id": "uuid",
  "space_id": "...",
  "redemption_peer_id": "...",
  "space_genesis_hash": "...",
  "issuer_peer_id": "...",
  "issuer_membership": "...",
  "pairing_secret": "32-byte base64url",
  "issued_at": 0,
  "expires_at": 0,
  "max_uses": 1,
  "permission": "view|control|full",
  "allowed_session_ids": [],
  "can_invite": false
}
```

`ConnectionBundle` 包含 ticket、`routes[]`、`created_at` 和 bundle signature。route hint 可包含：

- `rendezvous`: HTTPS/WSS base URL + opaque topic/peer id
- `quick_tunnel`: temporary HTTPS/WSS base URL
- 后续 `lan`: host/port + certificate hint

规则：

- 默认生成 5 个彼此独立、单次使用、24 小时有效的 ticket；数量和有效期可改。
- 权限不能超过 issuer grant；`allowed_session_ids` 只能收窄。
- 每张 ticket 指定签发设备为唯一 `redemption_peer_id`；该设备本地原子记录 `invite_id` consumed count。其它成员只能把兑换请求路由给签发设备，不能代为核销。
- 这条限制是严格单次消费的必要条件：离线分区下若多个副本都能独立核销，就无法阻止同一 ticket 在两个分区各成功一次。
- 签发设备离线时，预签 ticket 无需用户现场点击授权，但必须等该设备上线才能兑换。未来可增加显式 M-of-N redemption authority，不在 MVP 内。
- 批量撤销只有传播到对应 redemption device 后才能阻止兑换。已复制到外部但核销设备尚未收到撤销的 delegated invitation 仍可能在过期前使用，UI 必须提示这一点。
- 兑换成功后，签发设备签发 `DeviceGrant`/membership certificate，绑定 space、client public key、permission、session scope、`can_invite`、`can_sync_secrets`、签发/过期时间和 serial。
- 后续连接使用 grant + client 私钥 proof，不再消耗邀请。
- 删除 trusted device 会撤销其 grant serial；若该设备可签邀请，同时撤销其未兑换的 delegated tickets。

### 4.3 Handshake and record encryption

不能只依赖 WebRTC DTLS。恶意 Rendezvous 可以建立两个 DTLS 连接并转发应用数据，因此 Peer 层还需要绑定设备身份的端到端握手：

1. 双方生成临时 P-256 ECDH key。
2. Client hello 携带 invite proof 或 DeviceGrant、client identity、nonce 和 ephemeral key。
3. Host 验证 ticket/grant、使用次数和权限，并签名完整 transcript。
4. Client 验证 host identity 与 Space membership chain 均锚定到 invitation 中的 genesis hash，再签名完整 transcript。
5. ECDH secret 经 HKDF-SHA256 派生独立的 client-to-host / host-to-client keys。
6. 后续每个 `proto.Marshal(Frame)` 先进入 Peer encrypted-record layer，再进入 DataChannel/WSS。

record layer 使用现有前端依赖已支持的 XChaCha20-Poly1305；每方向独立 key，nonce 由随机 connection prefix + 单调 counter 构造。AAD 至少绑定 peer protocol version、connection id、双方 peer id、方向和 counter。counter 回退、重复或越窗直接断开。

DataChannel 消息按 16 KiB fragment 发送，带 message id/index/count/total length；重组上限沿用 `proto` 的 16 MiB，超限、重复 fragment、超时未完成都断开。必须用 `bufferedAmountLowThreshold`/Pion 等价机制做 backpressure，不能无界缓存 PTY 输出。

### 4.4 Effective permission

```text
effective = min(session.remote_permission, device_grant.permission, invite scope)
```

MVP frame allowlist：

| Permission | Allowed client -> host frames |
|---|---|
| view | `LIST`, `ATTACH`, `PING` |
| control | view + `IN`, `RESIZE`, `CLAIM_DRIVER` |
| full | MVP 与 control 相同；后续才逐项开放 paste/file/preview/create |

权限在两个边界执行：

- Peer transport/router 收到远端 frame 时先拦截。
- `desktop/uplink.go` 写 PTY 或调用本机能力前再次按 peer principal 拦截。

`allowed_session_ids` 为空表示主机当前所有本地会话；非空时 `LIST_RESP` 和所有 session frame 都过滤。任何 Relay 镜像会话不得经 Peer 再分享。去重和路由仍以 `session_id` 为权威，不按 `host_id`。

### 4.5 Peer Space governance

Peer Space 没有“服务器上的用户记录”。所有决策都是签名 operation，并在设备相遇时复制：

- Genesis 只作为不可变信任锚，不代表创建者是运行时 leader。当前 admin 集合来自已收敛的 governance log；创建者离线后其它 admin 仍能签发和撤销。
- 普通成员可以写自己有权限的配置 collection。
- `can_invite` 成员可以签发 depth=1 的 child membership；不能扩大自身权限。
- admin 可以发布 member/grant revocation；revocation 使用 deny-wins，不能被普通配置 LWW 覆盖。
- 删除成员触发 sync/vault epoch rotation。新 epoch key 分别用每个仍有效成员的 public wrapping key 封装。
- 两个 admin 在分区中并发产生同一 `previous_epoch` 的 rotation 时，以 operation hash 的确定性顺序选出一个 winner；losing branch 在下次 anti-entropy 后丢弃并重新基于 winner 旋转。
- 被撤销设备仍可能读取撤销前已经拿到的数据；rotation 只保证 forward secrecy，不能让已泄露信息失忆。

安全治理与普通配置不能共用一条 LWW 规则。成员撤销、capability 收窄、invite consume 是 deny/consume-wins；外观偏好等用户配置才使用 CRDT merge。

## 5. Architecture and Packages

### 5.1 Go

新增建议：

- `internal/peerproto/`
  - ticket/grant/bundle 的 typed models 与严格 parser
  - handshake transcript、encrypted record、fragment codec
  - 跨语言 golden vectors
- `internal/peercrypto/`
  - P-256 key encoding/sign/verify、ECDH、HKDF、record keys
  - 不接触 Relay `account_key`
- `internal/peerstore/`
  - Peer Space、trusted devices、invite ledger、revocations、epoch envelopes 的接口和 file-backed desktop 实现
  - 原子写、0600 权限、schema version；secret-equivalent 字段由 keyring-backed storage key 加密
- `internal/configsync/`
  - signed mutation、HLC/version vector、CRDT merge、anti-entropy、snapshot/compaction
  - transport-independent；Relay 与 Peer 只是不同 replication adapter
- `internal/peertransport/`
  - Pion host、DataChannel adapter、ICE lifecycle
  - 只实现 transport primitives，不 import `desktop/`
- `internal/rendezvous/`
  - accountless encrypted signaling mailbox
- `cmd/atterm-rendezvous/`
  - 可独立自建的最小服务
- `internal/quicktunnel/`
  - 后续 cloudflared lifecycle、URL extraction、timeouts

修改建议：

- `desktop/uplink.go`
  - 继续作为所有跨远程 session 行为的 owner
  - 接收 authenticated peer principal，把 Peer transport 接到本机 session/router
  - 第二次执行 permission/scope gate
- `desktop/app.go`
  - 暴露 Peer config、identity、invitation、trusted-device bindings
- `desktop/config.go`
  - 只保存非秘密配置；私钥与 invitation secret 不写普通 config
- `desktop/prefs_sync_loop.go` / `internal/prefssync/`
  - 迁移为统一 `configsync` engine 的 Relay adapter，不能让 Relay 与 Peer 两个 engine 各自回写造成同步环
- `internal/proto/`
  - 复用现有 v1 frame codec；MVP 不分配新 Type
  - `docs/spec/protocol.md` 新增“Peer transport envelope”章节，不改变现有 WS payload

P3 开始前先做一个小型 refactor spike，验证现有 local relay/session router 的复用点。目标是把 `internal/relay/client_conn.go` 中与 WebSocket 无关的 frame dispatch 提取为 transport-neutral handler，由现有 WS 和 Peer adapter 共用；不复制一套 LIST/ATTACH/replay/driver state machine。若 spike 证明该改动会扩大现有 Relay 风险，则改用 `desktop/uplink.go` 的窄 adapter，但同样禁止复制 `internal/session` 状态。

### 5.2 TypeScript/client

新增建议：

- `desktop/frontend/src/lib/peer/types.ts`
- `desktop/frontend/src/lib/peer/identity.ts`
- `desktop/frontend/src/lib/peer/invitation.ts`
- `desktop/frontend/src/lib/peer/handshake.ts`
- `desktop/frontend/src/lib/peer/transport.ts`
- `desktop/frontend/src/lib/peer/routeManager.ts`
- `desktop/frontend/src/lib/peer/configSync.ts`
- `desktop/frontend/src/components/SettingsPeer.vue`
- `desktop/frontend/src/components/PeerInviteDialog.vue`
- `desktop/frontend/src/components/PeerJoinDialog.vue`

重构建议：

- `lib/connection.ts` 引入最小 `BinaryFrameTransport` 接口：open/close/send/message/status。
- 现有 WebSocket 是一个 adapter；RTCDataChannel 和 Peer WSS 各是一个 adapter。
- `SessionConnection`/`SessionListConnection` 继续解析同一份 proto frame，不感知 WebRTC SDP。
- `platform/types.ts` 增加 `PeerBridge`，Wails/Capacitor/Web 分别实现 identity storage 与 host capability。
- `App.vue` 按 `session_id` 合并 Relay 与 Peer 的同一会话，并保存 route candidates；不得按 `host_id` 去重。

浏览器/WebView 约束：

- WebRTC、WebCrypto 和 IndexedDB 均要求安全上下文；公网 HTTP 明确拒绝。
- Quick Tunnel URL 只是 signaling/WSS endpoint。Web UI 保持在稳定 HTTPS origin，避免每次 `trycloudflare.com` hostname 改变导致浏览器 identity storage 换 origin。
- iOS identity 走 native Keychain plugin，并遵守 Capacitor 8 的三件套注册规则。

## 6. Decentralized Configuration Sync

### 6.1 Why the current engine cannot be reused unchanged

`internal/prefssync` 当前依赖 Relay `GET/PUT /api/me/preferences` 返回权威 `updated_at`，冲突由服务器时间戳裁决。这不是多主复制模型。Peer 模式必须把“本地配置读写”和“通过哪条网络同步”分开：

```text
                        +--------------------+
local setters --------->| configsync engine  |<--------- peer signed op log
                        | canonical replica  |
Settings/UI <-----------|                    |<--------> Relay adapter (optional)
                        +--------------------+
```

不能同时保留两个会直接 `WriteValue` 的同步 engine；否则 Relay pull 与 Peer merge 会互相回写、产生振荡或把较新值覆盖。迁移后只有 `configsync` 可以应用远端状态，现有 Relay API 只是一个 compatibility adapter。

### 6.2 Replication unit

每个 mutation 是不可变、签名的 typed record：

```text
SyncOp {
  space_id
  schema_version
  op_id = device_id + monotonic_counter
  actor_device_id
  hlc = physical_ms + logical
  collection
  record_id
  kind = set | delete | membership | revoke | rotate_epoch
  payload_ciphertext
  payload_hash
  causal_context (compact version vector)
  signature
}
```

- 每个 device counter 必须单调持久化，回退视为 identity/store corruption 并停止同步。
- HLC 只用于并发用户配置的确定性 tie-break；因果先后由 version vector 判断。
- 入站 physical time 相对本机最多接受有限偏移，防止错误时钟永久压过所有后续修改。
- mutation 在签名之后不可变；Relay adapter 的旧 snapshot 没有 Peer signature，只能在本地转成 actor=`relay:<realm_id>` 的 compatibility op。
- payload 先按 collection schema 校验，再应用；未知 schema 保留但不解释，方便滚动升级。

### 6.3 Merge rules

没有全局 leader，也不在冲突时弹窗。所有正常设备收到相同 op set 后必须得到相同结果：

| Data shape | Rule |
|---|---|
| Scalar preference | causal-newer wins；并发时 `(HLC, actor_device_id)` LWW |
| Map/entity collection | record-id scoped remove-wins observed map；不同记录并发编辑都保留 |
| Ordered collection | entity 使用 OR-map；顺序使用可稳定比较的 position id，不同步整个数组覆盖 |
| Local applicability | 保留合法远端值，但本机不适用时不启用，例如不存在的 `default_shell` |
| Membership/revocation | 独立 governance log；deny-wins，不走普通 LWW |
| Invitation consumption | 仅 redemption device 的原子 ledger 有权决定 |

`quick_templates`、profiles、SSH hosts 必须逐记录合并，不能继续把整个数组/加密 blob 当成一个 LWW value，否则 A 设备新增记录、B 设备编辑另一条记录时必然丢数据。第一版可不做同一 profile 内的逐字段 CRDT，但必须做到 record-level merge。

### 6.4 Anti-entropy protocol

受信 Peer 建立连接后，在独立的 reliable ordered `atterm-sync-v1` DataChannel 上同步，避免大配置阻塞 terminal input/output：

1. 双方交换 `space_id`、schema range、version vector、collection digest、current key epochs。
2. 按 version vector 请求缺失 ops，固定条数/字节上限分批发送。
3. 验签、校验 capability/schema、持久化 op，再 materialize 本地 view。
4. 返回 durable ack vector；不能在落盘前 ack。
5. op log 超阈值时生成带覆盖 vector 的签名 snapshot。只有 snapshot 覆盖本机已知因果历史时才能 compact。

离线语义：

- 本地修改立即生效并进入 pending op log。
- 任意两台设备在线并能建立 Peer connection 时双向 anti-entropy，不区分“主/从”。
- 小规模 Space 默认与所有当前可达成员同步；成员较多后再按 version-vector lag 选择固定 fanout，不能依赖某一台永远在线的 hub。
- A 与 C 从未同时在线也能经 B 传播 ops。
- 所有设备同时离线时没有同步；Rendezvous 不保存 op/snapshot。
- iOS/Web 受后台生命周期限制，只保证 app 前台或系统允许网络活动时参与；Peer-only 不假装具备 Relay/Web Push 的后台即时性。
- 长期离线设备回来后若所需 ops 已 compact，获取完整 snapshot + snapshot 之后的 ops。

WSS fallback 使用同一 encrypted record codec，并给 sync channel 最低调度优先级：input/control > terminal output > config sync。同步流量不能增加 session subscriber count，也不能破坏 lazy PTY stream。

### 6.5 Bootstrap and key hierarchy

新设备兑换 invitation 时，从 `redemption_peer_id` 获取：

- Space membership certificate 与 governance head。
- 当前 portable-config snapshot + version vector。
- 当前 `sync_epoch_key` envelope。
- 仅当 grant 含 `can_sync_secrets` 时，获取 `vault_epoch_key` envelope 和允许的 secret collections。

普通配置与敏感 vault 分开轮换，避免“能同步主题”自动等于“能拿 SSH 私钥”。密钥层级：

```text
device identity key          never synced
  -> wraps sync_epoch_key    portable preferences/config
  -> wraps vault_epoch_key   opted-in profiles env / SSH credentials and private keys
```

Peer channel 已端到端加密，但同步 payload 仍使用 epoch key 封装后再进入 op log，这样本地 snapshots、未来可选的第三方 store-and-forward 也不会变成明文。AAD 必须包含 `space_id || collection || record_id || op_id || epoch`，并在 `internal/e2eecrypto/aadtags.go`/`docs/spec/protocol.md` 注册独立 namespace，禁止与 Relay account envelopes 交叉重放。

### 6.6 What syncs

| Class | Examples | Peer default |
|---|---|---|
| Portable scalar | locale、terminal theme/font/cursor/scrollback、shortcuts、task display prefs | On |
| Portable collections | quick templates、profiles metadata、SSH host metadata | On, record-level merge |
| Sensitive vault | profile env、SSH password/passphrase/private key | Off；用户显式开启且目标成员有 `can_sync_secrets` |
| Machine-local | WebGL renderer、window state、log path、download path、local shell/path applicability | Never |
| Connection/account | Relay URL/password/session token、`account_key`、Rendezvous runtime state、Quick Tunnel URL | Never |
| Peer secrets | device private key、invite secret/consume ledger、session keys | Never |
| Ephemeral/session | scrollback、PTY bytes、recovery snapshot、current driver、notification subscription | Never |

`pinned_session_ids` 只对同一 `session_id` 在其它设备上仍有意义时展示；孤儿 pin 可保留但不制造 placeholder。Web Push 不属于去中心化能力，Peer-only 模式只能做本地通知。

### 6.7 Relay/Peer coexistence and migration

- `configsync` local replica 是唯一 canonical state，Relay/Peer 都是 adapter，不互为上游下游。
- 创建新 Space 时把当前 portable config 作为 genesis 后的第一批 signed ops。
- 加入已有 Space 时先保留一份本地 pre-join snapshot，再以 Space snapshot 初始化；本机已有 customized portable values 显示为“待导入”，只有用户确认后才生成新 ops。不能让一台新设备的默认值或旧值静默覆盖整个 Space。
- 首次升级把现有 `PrefsMeta` + 当前值 seed 成 local signed ops；已存在 Relay 账号时先完成一次 pull，再生成 migration snapshot，避免把旧本地默认值覆盖 Relay 自定义。
- Relay pull 项映射成 compatibility ops；Peer winner 需要同步到 Relay 时走现有 PUT，Relay echo 通过 value hash 去重，不再生成一轮本地 mutation。
- Relay 的 whole-blob `profiles_encrypted`/`ssh_hosts_encrypted` 与 Peer record-level collection 并存期间，由 adapter materialize/拆分；迁移完成前保留 round-trip tests。
- 用户关闭 Relay 后只停 Relay adapter，不删除 local replica、Space keys 或 pending ops。
- 用户退出/删除 Peer Space 只移除 Peer membership 与 Space keys，不清除本地业务配置；是否同时清配置必须另行确认。

## 7. Rendezvous Service

MVP Rendezvous 是无账户、无持久数据库的短期信令服务：

- Host 维持到服务的 WSS registration，使用 peer identity challenge signature 证明 peer id 所有权。
- Client 向高熵 opaque topic 投递 offer/ICE；Host 回 answer/ICE。
- 信令 payload 在客户端与主机间加密，服务只看到 topic、大小、时间和来源 IP，不看到 SDP candidate 内容。
- mailbox TTL 120 秒；单 message 64 KiB；单 topic、单 IP、全局连接数均有限制。
- 不接受 terminal `proto.Frame`，不提供 store-and-forward，不提供 TURN。
- 必须配置 TLS、Origin allowlist、rate limits；生产环境不支持明文公网 HTTP。
- 官方与自建服务使用同一协议；客户端只把 URL 当配置，不把官方域名写死在 crypto transcript。

建议接口在 spec 阶段定为 WebSocket-only，避免 polling 与 WS 两套状态机。服务可独立部署，也可由官方基础设施反代到固定路径。

## 8. Quick Tunnel

Quick Tunnel 是可选路径，不属于 atterm 账户或官方 Rendezvous：

- Desktop 在 loopback 随机端口启动 peer gateway。
- `cloudflared tunnel --url http://127.0.0.1:<port>` 建立临时公网入口。
- 解析 `https://*.trycloudflare.com`，启动超时 30 秒，退出超时 5 秒；进程退出或 app 关闭后 URL 失效。
- gateway 同时支持加密 signaling 和加密 binary WSS。
- 优先通过 tunnel 信令建立 WebRTC；若 DataChannel 在限定时间内失败，可在用户允许时沿同一 WSS 继续传 encrypted records。
- WSS 回退状态显示为“经 Quick Tunnel”，不能显示为“P2P 直连”。Cloudflare 能观察 IP、连接时间与流量大小，但不能读取 terminal frame。

落地前必须完成独立 spike：

- Windows/macOS/Linux 进程管理和 URL 输出格式。
- WebSocket upgrade、长连接和 idle timeout 实测。
- `cloudflared` Apache-2.0 NOTICE、二进制体积、平台签名/公证与 release asset 供应链。
- 是否随 app 打包。若未通过供应链 gate，首版只支持自动发现系统已有 `cloudflared`，UI 明确提示缺失，不静默下载未验签二进制。

## 9. Settings Information Architecture

保留 Settings 弹窗，不改成独立页面：

- Desktop 调整为自适应约 `1040-1080 x 700-720`，受 viewport max 限制。
- Mobile 保持 full-screen。
- sidebar、content、footer 都使用 `min-height: 0`；只让内容区滚动，确保底部 Realm/advanced sections 可达。

导航调整：

- `Relay 账户`
  - 登录/注册、官方或自建 Relay
  - Relay E2EE/同步/fallback 设置
  - 已登录设备列表与逐台/其它/当前设备退出
- `Peer 连接`
  - 本设备 Peer identity/fingerprint
  - Peer Space id、成员/权限、sync/vault key epoch
  - Host 开关与当前可达性
  - Official/custom/disabled Rendezvous
  - Quick Tunnel 状态
  - Trusted devices、权限、撤销
  - Pre-signed invitation batch、QR/copy/revoke
  - Join existing host
  - Config sync 状态：last sync、pending ops、conflict count、手动 sync

实现上移除 standalone `devices` tab，把 `SettingsDevices.vue` 作为 Relay Account 内部 section 复用；不要复制第二份设备列表逻辑。`SettingsPeer.vue` 在 Desktop 提供 host + client controls，在 Web/Capacitor 只提供 client/join 与本设备 identity controls。

Session UI 增加实际 route indicator：`Direct`、`Quick Tunnel`、`Relay`。这只是连接状态，不进入 session identity，也不改变以 `session_id` 为准的合并规则。

## 10. Delivery Plan

### P0 - Spec, threat model, and compatibility spikes

Deliverables:

- [ ] 新增 `docs/superpowers/specs/2026-09-22-accountless-p2p-design.md`，冻结 ticket/grant/handshake/record/rendezvous/config-sync schemas。
- [ ] Go + Browser P-256 sign/verify/ECDH golden-vector spike，覆盖 Safari/WKWebView target。
- [ ] Pion DataChannel host ↔ browser spike，验证 ordered/reliable、fragment size、backpressure、ICE restart。
- [ ] local relay frame-dispatch reuse spike，记录选定 adapter 边界。
- [ ] 用三设备分区模型验证 HLC/version-vector/CRDT 决策，冻结 Relay compatibility adapter 行为。
- [ ] Quick Tunnel WSS capability/packaging spike，结果不阻塞 MVP。
- [ ] 为 threat model 列出资产、攻击者能力、泄露面和 fail-closed 行为。

Acceptance:

- Go/Chromium/WebKit 对同一 vectors 的 key encoding、signature、ECDH、HKDF 和 AEAD 结果一致。
- 恶意 signaling relay 无法伪装 host/client 或读取 Peer record plaintext。
- A/B/C 在离线并发修改后交换同一 op set，最终 materialized config byte-for-byte 一致。
- 明确记录哪些 metadata 对 Rendezvous、STUN、Cloudflare 可见。
- 在 spike 通过前不进入 UI 实现。

### P1 - Peer Space identity, trust store, and pre-signed invitations

Deliverables:

- [ ] `peercrypto`、`peerproto`、`peerstore` typed APIs 与 tests。
- [ ] Peer Space create/join、membership certificate、capability delegation 与 governance log。
- [ ] Desktop keyring identity + encrypted local trust store。
- [ ] Web non-exportable WebCrypto identity；iOS Keychain identity plugin。
- [ ] ticket batch create/list/revoke/consume ledger。
- [ ] DeviceGrant issuance、delegation depth 1、permission narrowing、deny-wins revocation 和 epoch rotation。
- [ ] Wails/Platform typed bindings；所有敏感值日志 redaction tests。

Acceptance:

- 重启后 device identity 稳定；清除 identity 是显式 destructive action。
- 10 个并发 redemption 对单次 ticket 只有一个成功；非 redemption device 一律不能核销。
- revoked/expired/unknown/consumed 对外返回同一错误。
- ticket 可在没有当前 route URL 时预生成，之后能组合 Rendezvous 或 Quick Tunnel bundle。
- Relay `account_key` 与 Peer store 之间没有 import、copy 或派生路径。

### P2 - Decentralized config replica

Deliverables:

- [ ] `internal/configsync` signed op、HLC、version vector、merge 和 materialized view。
- [ ] scalar LWW register、record-level remove-wins map、stable ordered collection。
- [ ] sync/vault key epochs、per-member envelopes、snapshot/compaction。
- [ ] current `prefssync.Adapter` values 到 new replica 的 one-time migration。
- [ ] Relay compatibility adapter；替换双 engine 直接回写为一个 canonical engine。
- [ ] profiles/SSH hosts 从 whole blob 到 record-level representation 的兼容桥。

Acceptance:

- 三设备随机乱序/重复/延迟投递同一 op set 后状态一致，property test 连续运行至少 10,000 seeds。
- 删除与并发更新按 remove-wins 收敛；不同 records 的并发修改都保留。
- 被撤销成员不能解开新 epoch 的 sync/vault payload；无 `can_sync_secrets` 成员永远拿不到 vault key envelope。
- Relay-only 升级后现有 18 个 synced keys 不丢值，旧客户端仍可通过现有 API 同步。
- Peer adapter 关闭或无在线 peer 时本地 setter 仍立即成功，pending ops 跨重启保留。

### P3 - Authenticated Peer transport core

Deliverables:

- [ ] Pion host + native browser RTC client。
- [ ] 双向 authenticated handshake、encrypted record、fragmentation 和 backpressure。
- [ ] transport-neutral frame adapter 复用现有 `proto.Frame` 和 session state machine。
- [ ] Peer principal、session scope、effective permission 双重 enforcement。
- [ ] 只开放 MVP frame allowlist；其余 frame fail closed。
- [ ] in-memory signaling integration tests，不依赖公网。
- [ ] 独立 `atterm-sync-v1` DataChannel 和优先级/backpressure，不计入 terminal subscriber。

Acceptance:

- 可 list、attach、加载 scrollback、view、claim driver、input、resize、close client connection。
- 0 个 Peer subscriber 时不发送 PTY OUT；0→1/N→0 lazy lifecycle 与现有 Relay 一致。
- 大 scrollback 仍有 `REPLAY_PROGRESS` 且客户端不卡在 connecting。
- 篡改 transcript、重放 record、counter 回退、超限 fragment、越权 frame 均断开且不写 PTY。
- Peer 连接失败不影响本地 terminal，也不改变 Relay uplink 状态。
- terminal 高输出时 config sync 能最终完成；config snapshot 传输不能阻塞 input/control。

### P4 - Accountless Rendezvous

Deliverables:

- [ ] `internal/rendezvous` 与 `cmd/atterm-rendezvous`。
- [ ] encrypted WSS signaling protocol、TTL mailbox、limits、origin/TLS policy。
- [ ] Desktop host registration/reconnect；client offer/answer/trickle ICE。
- [ ] Space member presence 与 peer-to-peer sync dialing；presence 不暴露配置内容，也不成为成员真相源。
- [ ] official/custom/disabled config，默认 STUN config 与隐私说明。
- [ ] self-host deployment docs、health endpoint 和 metrics（无 payload logging）。

Acceptance:

- Rendezvous 无 user/account/token database，重启只丢失临时信令。
- 抓包和 server logs 中无 invite secret、grant private material、SDP plaintext、terminal bytes。
- 服务不可用时 Peer host/local terminal 正常；客户端得到可区分的 signaling failure。
- 官方和自建实现跑同一 contract suite。

### P5 - Shared client, join UX, and Peer sync (MVP feature complete)

Deliverables:

- [ ] `BinaryFrameTransport` + WS/RTC adapters。
- [ ] Web/Wails/Capacitor route-aware session list and attach。
- [ ] QR scan、paste invitation、fingerprint confirmation、join result。
- [ ] route-independent invitation batch UI 与 trusted-device revoke UI。
- [ ] bootstrap snapshot、background anti-entropy、pending/conflict/status events。
- [ ] stable secure-origin handling；invite secret 仅进 URL fragment/deep-link payload，不进 query。
- [ ] 中英文文案、keyboard/focus/mobile safe-area states。

Acceptance:

- 全新 iOS/Web/Desktop client 不登录 Relay 即可通过 invitation attach 桌面 session。
- 同一 ticket 通过 official 和 self-hosted Rendezvous 都能兑换。
- 刷新/重启后使用 DeviceGrant 重连，不再次消耗 invitation。
- A 改主题、B 新增 template、C 删除一个 profile 后，即使三者不同时在线，也能经中间设备最终收敛。
- profiles/SSH secrets 默认不传播；显式开启后只到 `can_sync_secrets` devices。
- view/control 权限在 UI 与 host 实际行为一致。
- 旧 Relay-only 用户不创建 Peer identity、不多开网络连接、不改变现有行为。

### P6 - Settings integration and Relay account consolidation

Deliverables:

- [ ] `SettingsDialog.vue` 自适应尺寸与滚动布局。
- [ ] Relay config、Account 与 `SettingsDevices` 合并为 `Relay 账户` tab。
- [ ] 新增 `Peer 连接` tab，并按 platform capability 隐藏 host-only controls。
- [ ] Peer Space members、sync status/pending/conflict、secret-sync policy 与 key epoch 状态。
- [ ] dirty-state protection 覆盖 Relay 和 Peer 表单；即时动作不错误标 dirty。
- [ ] prototype 中所有 loading/empty/error/expired/revoked/offline states 对应到真实实现。

Acceptance:

- Desktop 1280x720、1440x900、宽屏和 Mobile 320/390px viewport 均可滚到最后一项。
- 无横向 overflow、按钮文字截断、嵌套 card 或 footer 遮挡。
- 已登录设备只出现在 Relay 账户；Peer trusted devices 不与 Relay sessions 混列。
- Playwright screenshot + scroll assertions 通过。

P5 与 P6 可以在 code ownership 不冲突时并行，但合并顺序为 transport/client 先、最终 IA 后。

MVP release gate 到此。MVP 明确显示 “Direct connection may be unavailable on restrictive NAT; no TURN fallback configured”，并显示“配置只会在至少两台设备同时可达时同步”。

### P7 - Quick Tunnel optional route

Deliverables:

- [ ] cloudflared process lifecycle、binary discovery/packaging gate。
- [ ] local peer gateway、route bundle refresh、QR regeneration。
- [ ] WebRTC-over-tunnel signaling。
- [ ] optional encrypted WSS terminal fallback，复用同一 Peer handshake/record codec。
- [ ] visible route/privacy/temporary URL status 与诊断日志。

Acceptance:

- 不配置 Rendezvous 也能通过 Quick Tunnel invitation 完成 join。
- 直连成功后终端 bytes 不再经过 Quick Tunnel。
- 强制 ICE 失败时，用户允许 WSS fallback 才切换；UI 显示 “Quick Tunnel”，不是 “Direct”。
- cloudflared 崩溃、URL 轮换和 app quit 均正确清理，旧 URL/ticket 不泄露到日志。
- 未安装 cloudflared 时错误可操作，本地/Relay 路径不受影响。

### P8 - Hybrid route selection and Relay fallback

Deliverables:

- [ ] `routeManager` 同时维护 Peer direct、Quick Tunnel、Relay candidates。
- [ ] 按 `session_id` 合并同一会话，保留各 route health，不生成重复 sidebar item。
- [ ] policy：Direct Peer > user-enabled Quick Tunnel > user-enabled Relay。
- [ ] attach 前切换、连接中降级、重连回升策略；禁止双 driver/input duplicate。
- [ ] route indicator 与 diagnostics。

Acceptance:

- Direct 建立后不重复订阅 Relay stream，保持 lazy upload。
- Direct 中断只发生一次受控切换，输入不重复、OUT seq 不倒退。
- Relay 未登录时 Hybrid 自动退化为 Peer-only，不弹账户登录阻塞。
- 两条路径返回同一 `session_id` 时只展示一条。

### P9 - Full capability expansion and hardening

按独立 PR 逐项开放，每项都需 permission + record size + cancellation tests：

- [ ] paste image/file。
- [ ] remote file explorer。
- [ ] remote session create from profile。
- [ ] remote web preview 独立 byte channel。
- [ ] optional user-configured TURN，明确标为 relayed ICE path。
- [ ] LAN/mDNS、manual address、IPv6 route hints；完全无公网模式。
- [ ] grant expiry renewal、trust export/import（仍不含私钥明文）。
- [ ] soak/fuzz/chaos、mobile background/foreground reconnect、电池与流量测量。

## 11. Test Strategy

### Unit and vectors

- Go/TS cross-language golden vectors：key encoding、signature、ticket、grant、transcript、HKDF、record、fragment。
- Fuzz parsers：invite、bundle、handshake、record、fragment、signaling envelope。
- Fake clock：expiry、TTL、revocation、counter/replay windows。
- Permission table exhaustive test：每个现有 `proto.Type` 必须显式 allow/deny；新增 Type 默认 deny。
- CRDT property tests：交换律、结合律、幂等性；随机 op 顺序/重复/丢包重试后收敛。
- Governance tests：并发 revocation/rotation、stale epoch、无权限 actor、counter rollback 全部 fail closed。

### Integration

- Pion host ↔ headless Chromium/WebKit client。
- NAT matrix：host candidate、server-reflexive、blocked UDP、symmetric NAT simulation。
- Rendezvous restart、packet reorder、duplicate ICE、late answer、host reconnect。
- Quick Tunnel process fake + opt-in real smoke test。
- Relay + Peer simultaneous listing/attach and lazy stream assertions。
- A↔B、B↔C、A/C 不直连的 anti-entropy 传播；离线编辑、snapshot bootstrap、compaction 后旧设备回归。
- Relay compatibility adapter 与 Peer adapter 同时开启，验证无 echo loop、无 whole-blob 数据丢失。

### Client/UI

- Platform contract tests for Wails/Web/Capacitor identity storage and invite flows。
- Settings 真实尺寸截图、滚动到底、mobile safe area、expired invite countdown。
- Sync UI 覆盖 never synced、pending、syncing、converged、conflict、stale member、key rotation failed。
- URL tests prove secret only appears after `#` and is stripped from errors/analytics/logging。
- `new RTCPeerConnection`/`new WebSocket` 同步异常都进入统一 failure/retry state，不击穿 app boot。

### Release gates

- `go test -race ./internal/peer... ./internal/configsync/... ./internal/rendezvous/... ./desktop/...`
- `go vet -tags webkit2_41 ./...`
- `cd desktop/frontend && npm run build && npm test`
- `cd web && npm run build && npm test && npm run test:contract`
- `cd mobile && npm test`
- WebKit/iOS manual matrix before enabling by default。
- Peer feature flag initially default off；internal/beta soak 后再默认展示入口。

## 12. Risk Register

| Risk | Impact | Mitigation / gate |
|---|---|---|
| Symmetric NAT / UDP blocked | WebRTC direct fails | MVP 明示无 TURN；P7 Quick Tunnel WSS；后续 user-configured TURN |
| Invite copied/replayed | Unauthorized device enrollment | 256-bit secret、short expiry、仅 issuer/redemption device 原子 consume、batch/device revoke |
| Delegated issuer compromised | Can mint valid child invitations | `can_invite` off by default、depth 1、permission narrowing、issuer revoke invalidates descendants |
| No online overlap | Config changes remain pending | 明确 eventually-consistent 语义；显示 pending/last-sync；Rendezvous 不偷偷存配置 |
| Concurrent config edits | Silent lost update | causal context + deterministic CRDT；collections record-level merge；property tests |
| Clock skew / forged future HLC | One actor dominates LWW | bounded physical skew、logical clock、device revoke；security decisions never use LWW |
| Revocation during partition | Revoked device remains active in stale partition | deny-wins when observed、epoch rotation、UI shows stale members；document unavoidable partition window |
| Concurrent epoch rotation | Space forks and devices cannot decrypt | same-parent deterministic winner、losing branch rebase、recovery tests |
| Sync/vault key confusion | Non-secret member receives credentials | separate epoch keys/capabilities/AAD namespaces；secret sync off by default |
| Op-log/tombstone growth | Disk/memory growth | signed snapshots、ack vectors、bounded retention；stale device uses full bootstrap |
| Malicious Rendezvous MITM | Host spoofing or terminal disclosure | Signed ephemeral ECDH transcript + application record encryption；signal payload encrypted |
| DTLS identity not bound | Two-leg proxy reads app frames | Never treat DTLS alone as authorization; Peer handshake keys every record |
| Browser insecure context | WebCrypto/WebRTC unavailable | HTTPS/localhost only; fail before join with actionable state |
| DataChannel size/backpressure | Memory growth or Safari disconnect | 16 KiB fragments、bounded reassembly、bufferedAmount watermarks |
| Lazy stream regression | PTY bytes uploaded while idle | subscriber lifecycle integration tests; one logical subscription across route changes |
| Permission bypass | Remote PTY/filesystem access | transport gate + desktop host gate; exhaustive frame matrix; unknown Type deny |
| Quick Tunnel outage/change | Optional connection path unavailable | never required for local/Relay/Rendezvous; capability spike; visible third-party status |
| cloudflared supply chain | Unsigned executable execution | package from pinned upstream artifact with checksums/signing review, or system binary only |
| `account_key` leakage | Breaks existing Relay E2EE | separate packages/storage/API; no Peer function accepts account key; redaction tests |
| Duplicate Relay/Peer routes | Duplicate input/subscription | merge by `session_id`; one active route lease; explicit handover state machine |
| Browser origin changes | Device identity appears lost | stable HTTPS UI origin; Quick Tunnel is endpoint only, never identity storage origin |
| Host trust DB loss | Old grants/replay ledger unavailable | fail closed; identity backup is out of scope; UI explains reset invalidates all devices |

## 13. Rollout Order

Recommended PR sequence:

1. Spec + crypto/WebRTC/frame-router/config-merge spikes.
2. Peer Space crypto/proto/store and cross-language vectors.
3. Decentralized config replica + current Relay sync compatibility adapter.
4. Pion transport + in-memory signaling + permission enforcement + sync DataChannel.
5. Accountless Rendezvous service and deployment docs.
6. Shared client transport + join/bootstrap/reconnect/anti-entropy flow.
7. Settings IA, invitation/trusted-device/sync UI; ship behind feature flag.
8. MVP beta soak and security review.
9. Quick Tunnel signaling and encrypted WSS fallback.
10. Hybrid Peer-first/Relay-fallback route manager.
11. Full capabilities and LAN/manual/no-public-infrastructure routes.

每个 PR 必须保持现有 Relay-only flow 可独立运行；任何 Peer 初始化失败都只能降低 Peer 状态，不能阻止 desktop boot、local PTY、Relay uplink 或 Settings 打开。
