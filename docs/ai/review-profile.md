# AT Term Review Profile

## 业务域

- 项目名称：`atterm`（AT Term）
- 项目定位：跨平台终端模拟器与内建会话云同步，支持桌面、Web 和移动端跨设备 attach、查看历史与继续输入。
- 主要调用方：桌面用户、浏览器与移动端用户、自托管 relay 管理员。
- 核心业务对象：PTY session、host、relay account、remote permission、driver/viewer、Peer Space、SSH host、恢复快照。
- 关键状态流转：session 创建/发布/attach/stream/close；viewer/driver 切换；远程权限 `view`/`control`/`full`；AI 会话捕获与恢复。

## 关键约定

- 桌面本地路径必须离线可用；所有远程 relay 逻辑集中在 `desktop/uplink.go`，本地会话不得依赖 uplink 状态。
- Lazy 上传不可退化：远程无订阅者时不上传 PTY 字节，必须保留 subscriber 0→1 和 N→0 的 STREAM_REQUEST/STOP 语义。
- attach、路由和去重以 `session_id` 为权威，禁止按共享的 `host_id` 去重。
- `internal/proto.Version` 保持 1；wire 变更只能新增未占用类型或兼容字段，并同步 `docs/spec/protocol.md`。
- 依赖方向固定为 `desktop/ -> internal/*`；`internal/*` 禁止 import `desktop/`。
- owner 决定远程权限，relay 与 desktop host 必须对本机输入、resize、粘贴、文件系统等敏感动作双重执法。
- 公网 relay 必须 fail closed：安全 origin、bootstrap、HTTPS、OPAQUE、更新验签等既有边界不得以开发便利绕过。
- OPAQUE 三端统一使用 `github.com/bytemare/opaque` 与 `internal/opaquesuite` 配置；不得替换单端实现或新增密码恢复通道。
- `account_key` 不得离开主线程/Keychain/Keyring，不得进入 URL、日志、IndexedDB、广播消息或子进程。
- sealed payload 必须使用注册表中的唯一 AAD type；seal 成功后必须清空相应明文字段，禁止 relay fallback 泄漏。
- Web 主入口复用 `desktop/frontend/src/`；禁止恢复第二套主 UI，禁止引入 CDN 依赖。
- 共享前端影响 Web bundle 时必须运行 `./scripts/build-web.sh` 并提交 `internal/relay/web-dist/` 的确定性产物。
- 更新 asset 必须先验 Ed25519 `SHA256SUMS.sig` 再验 SHA256；任一材料缺失或不匹配均拒绝安装。
- PTY 初始 winsize、移动端 IME、ResizeObserver/viewer 尺寸锁定、WebSocket 同步异常隔离等耦合规则以 `AGENTS.md` 为硬约束。
- 桌面启动 fatal 必须进入 `setStartupFatalError` 和 StartupError UI；禁止新增裸 `log.Fatalf`。

## 高风险路径

- `internal/proto/`, `docs/spec/protocol.md`：wire 编号、payload 和兼容性边界。
- `internal/relay/`, `cmd/atterm-relay/`, `internal/userstore/`：公网入口、鉴权、权限、路由、配置和多实例状态。
- `internal/e2eecrypto/`, `internal/e2eeclient/`, `internal/opaquesuite/`, `cmd/opaque-wasm/`：密钥、AAD、OPAQUE 与跨端互通。
- `desktop/uplink.go`, `desktop/uplink_*.go`, `desktop/relay_host.go`：Lazy 上传、远程执法与本机 PTY 写入。
- `desktop/updater*.go`, `.github/workflows/build.yml`：自动更新验签、签名、公证和发布产物。
- `desktop/recovery_*`, `desktop/ai_sid_*`, `desktop/frontend/src/composables/useRecoverySnapshot.ts`：AI conversation 身份与错误恢复泄漏风险。
- `desktop/frontend/src/components/TerminalView.vue`：输入法、winsize、driver/viewer、粘贴和跨端共享行为。
- `desktop/feishu/`, `internal/feishu/`：CardKit 时序、表单状态和远程交互输入。
- `internal/userstore/migrations/`：SQLite/Postgres 双后端迁移与历史数据兼容。

## 忽略路径

- `desktop/frontend/wailsjs/go/main/`：Wails 生成绑定；审查其生成源和调用方。
- `internal/relay/web-dist/`：确定性生成产物；通常审查源文件并用 drift gate 验证，密钥泄露或异常外链除外。
- `**/node_modules/`, `web/dist/`, `desktop/frontend/dist*/`：依赖或本地构建产物。

## 独立发布范围

- `cmd/atterm-relay/`：自托管 relay 服务，可独立部署。
- `desktop/`：Wails 桌面应用，按操作系统打包、签名和发布。
- `mobile/`：Capacitor iOS 应用，依赖同步后的 native plugin 注册。
- `cmd/atterm-rendezvous/`：独立 rendezvous 服务。

## 领域术语

- Lazy 上传：只有远程 subscriber 存在时，desktop 才向 relay 上传 PTY 输出。
- owner：本机 PTY 所在 host；远程权限策略的最终决定者。
- driver/viewer：运行时输入角色；与 `remote_permission` 策略正交。
- uplink：desktop 到 relay 的 host/agent 连接，不等于本地 session 路径。
- sealed：由 `account_key` 派生的 session key 加密、并绑定 frame type AAD 的 payload。
- home instance：用户偏好的 relay 实例，不是强绑定；共享状态通过数据库协调。
- sidebar-viewer：本机 host 上由另一实例发布、以 remote pane 展示的第三种恢复状态。

## 重点检查项

### 业务约束

- session 列表、置顶、恢复和 attach 是否始终以 `session_id` 维持身份。
- driver/viewer、远程权限降级、断线重连和订阅暂停是否保持既有状态语义。
- AI SID 无法精确解析时必须保持空值，不得猜测或恢复到另一段 conversation。

### 架构边界

- desktop 本地功能是否因 relay 不可达而失败，或把远程职责散落到 `desktop/uplink.go` 之外。
- relay 公网入口是否完整执行 origin、身份、session scope、权限、限流和连接数校验。
- 出站或本机敏感动作是否限制合法目标，并在 relay 与 owner host 重验权限/lease。
- 前端认证材料是否停留在允许的主线程安全存储，Service Worker 与 widget 不得持有 `account_key`。

### 数据与兼容性

- protocol 只能 additive 演进；旧客户端应能忽略新 opaque 字段并保持明确 fallback。
- userstore schema 需同时验证 SQLite/Postgres，迁移要支持已有数据库并有失败回滚语义。
- E2EE sealed 字段、AAD 注册表、协议表和明文字段清零必须同步。
- relay 配置以 DB singleton 为准；热应用和明确需重启项不得混淆。

### 外部依赖

- 飞书 CardKit 更新必须保持元素 mounted 状态独立、sequence 单调和 mount-specific element id。
- Capacitor plugin 必须安装在 `mobile/package.json`、实现桥接协议并显式注册 app-local plugin。
- Web/Capacitor OPAQUE WASM 必须由共享脚本可复现构建，不得引入另一实现。

### 测试重点

- 前端共享组件同时覆盖 desktop 与 Web 语义；有行为分支时覆盖普通、边界和恢复/重连状态。
- wire、权限和加密改动必须含正反授权、错误 AAD/身份、旧客户端或无 key 路径。
- uplink 改动必须覆盖无 subscriber、首个订阅、最后订阅离开和重连。
- UI 改动检查 desktop/Web/mobile 响应布局、hover/focus/touch 与文本不溢出。

## 触发关键词检查

- `host_id` + filter/dedupe/map key：确认没有替代 `session_id` 做会话身份。
- `STREAM_REQUEST`, `STREAM_STOP`, `subscriber`：检查 legacy/compressed 生命周期独立且 Lazy 上传不回归。
- `Sealed`, `SealedBody`, `AAD`, `account_key`：检查唯一 type、明文清零、日志与存储泄漏。
- `remote_permission`, `driver`, `FS_REQUEST`, `PASTE_`：检查 relay 与 host 双重执法及降权竞态。
- `WebSocket(`：检查构造同步异常被捕获并进入状态与重连路径。
- `log.Fatalf`：desktop 初始化失败应改走 StartupError。
- `desktop/frontend/src/`, `web/src/`：判断是否需要同步重建 `internal/relay/web-dist/`。
- `CAP_PLUGIN`, `registerPluginInstance`, `mobile/package.json`：检查 Capacitor 8 三件套。

## Verification Evidence

```bash
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./desktop/
cd desktop/frontend && npm test && npm run build
cd web && npm run build && npm test && npm run test:contract
cd mobile && npm test
./scripts/check-web-dist.sh
git diff --check
```

- 运行条件：Go 1.23+、Node 20（`.nvmrc`）；drift-gated WASM 还要求 CI 固定的精确 Go patch；Linux Wails 使用 `webkit2_41` tag。
- 常见环境失败：缺 native WebKit/Wails 依赖、Node/Go patch 不匹配、未安装 npm 依赖；需与测试断言失败、TypeScript 错误和 drift diff 区分。
- 无法本地验证时需要的证据：目标平台 CI、签名/公证结果、真实 relay 互通测试、必要的桌面/移动端人工交互记录。

## Risk & Compatibility

- Breaking change 判断：改变现有 frame payload 含义/编号、鉴权传输、session 身份、密钥派生、权限语义或旧客户端 fallback 均属破坏兼容。
- 迁移要求：数据库变更需双后端 migration；配置变更需区分首启播种、DB 权威值、热应用和重启项。
- 发布影响：共享前端需更新 embed；桌面更新需签名验签链；release tag 必须按 HEAD 可达的 SemVer line 计算。
- 安全/权限：重点关注 account key、密码/token、sealed 明文、跨 session/type replay、未授权本机动作与公网明文传输。
- 性能/容量：关注 scrollback pacing、连接/请求上限、压缩 seq、文件传输 hard cap、relay 多实例进程内限流。

## 跨仓库联动

- 无已知必须同步的外部代码仓库；桌面、Web、mobile、relay 与生成 WASM/embed 均在本仓库内联动。
- 外部部署配置、反向代理和飞书应用配置可能需要运营同步，具体 owner 为 `NEEDS_INPUT`。

## Review Routing

- 默认 owner：无 `CODEOWNERS` 或已知默认 owner；`NEEDS_INPUT: 确认维护团队/模块负责人`。
- 需要额外评审：wire/E2EE/OPAQUE、鉴权权限、数据库迁移、更新签名、发布 tag、多实例路由、恢复身份算法。
- 升级路径：`NEEDS_INPUT: 确认安全、发布与架构变更的人工升级路径`。
- 应 request changes：违反“关键约定”、缺少兼容/迁移证据，或共享 Web 产物存在 drift。
- 可接受残余风险：无已知项目级豁免；平台专属人工验证缺失需由 owner 明确认领。

## Verdict 调整

- 违反 `AGENTS.md` 红线、造成 conversation 串线、密钥/明文泄漏、越权本机动作、更新绕过验签或 wire 破坏兼容时，按 blocker 处理。
- 共享前端已修改但 `internal/relay/web-dist/` 未同步时，至少为合并前必须修复的问题。

## Code Pointers

- `AGENTS.md`：项目红线与踩坑记录；review 判断的首要规则来源。
- `docs/spec/`：当前架构、认证、协议、约定和组件样式的正式规范。
- `internal/proto/`, `internal/e2eecrypto/aadtags.go`：wire 常量和 AAD 注册表。
- `desktop/uplink.go`, `internal/relay/`：跨远程链路与 relay 执法。
- `desktop/frontend/src/main.web.ts`, `web/src/main-web.ts`：共享 Web 主入口边界。
- `scripts/build-web.sh`, `scripts/check-web-dist.sh`：embed 同步和确定性 drift gate。

## 维护规则

- 仅把已有项目文档、重复事故或 owner 确认的长期规则沉淀到 profile，不吸收单次 finding。
- 目录结构、验证命令、发布方式、owner 或跨仓库联动变化时同步更新本文件。
- 通用风格与通用 review 流程留给共享 skill，本文件只记录 atterm 特有判断依据。
