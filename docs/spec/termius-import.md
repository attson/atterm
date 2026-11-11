# Termius 本机迁移

桌面 SSH 管理面板提供「从 Termius 导入」。用户先退出 Termius，选择数据目录，扫描、勾选并确认。扫描前检查 Termius 主进程及 Helper；仍在运行（含托盘后台）或进程检查失败时，阻止读取数据库与钥匙串并提示重试。检测到多个安装目录时必须明确选择，不合并不同安装的数据。Web 和移动端不提供此入口。

## 模块边界

- `internal/termius`：只读 LevelDB / Chromium IndexedDB、解密、按引用构建迁移模型。无 desktop、relay、账号同步或目标存储依赖，便于以后独立成迁移工具。
- `desktop/termius_keyring_*.go`：读取 Termius 的系统凭据。Linux 按 keytar 的 `service/account` 属性搜索所有集合，不使用 go-keyring 的 `username` 属性；Windows 按 keytar 的 `service/localKey` target 读取，而非 atterm keyring 使用的冒号 target。
- `desktop/termius_import.go`：脱敏预览、一次性确认、写入现有 SSH 存储。
- `TermiusImportDrawer.vue`：目录选择、可选 localKey、主机/密钥分栏预览和导入反馈。支持按名称、地址、用户名及标签搜索，并叠加全部/可导入/不可导入/已存在状态筛选（各状态显示总数，已存在优先归类）；批量勾选仅作用于当前筛选的可导入项目，保留筛选外选择。底部统计随主机自动包含的密钥，并按源 ID 去重。中英文文案位于共享 i18n。

源 `localKey` 是 base64 编码的 32 字节 NaCl secretbox key。已知字段信封为 `04 01 || nonce(24) || MAC+ciphertext`，使用 XSalsa20-Poly1305。候选钥匙串条目必须通过真实密文认证验证；不按服务名优先级盲选。用户也可手动输入 localKey，不是 Termius 账号密码。

## 数据与行为约束

1. 不打开源数据库的写入接口，不修改源文件。依据 CURRENT / MANIFEST 的当前表文件、有效 WAL、序列号及 tombstone 还原当前记录；校验 CRC32C，支持 Snappy。扫描期间文件变化、块损坏或未知格式都报错。
2. 只从当前 `hosts` 表导入，不用连接历史补齐主机。业务记录按 `id`（无 id 则 local_id）和 updated_at 去重；同版本存在矛盾记录则报错。
3. 凭据沿 `host → ssh_config → identity → ssh_key` 的明确引用解析，支持 id/local_id。禁止按用户名、标签或物理相邻位置猜配。
4. 预览只返回元数据、凭据是否存在及源 ID。密码、私钥、口令及 localKey 不进入日志、URL、浏览器持久化或导出文件。确认仅回传源 ID；敏感内容保留在 Go 内存中，取消或确认后释放预览，10 分钟自动过期。
5. 同一预览只允许一次确认消费。校验失败保留预览；写入失败清理新增凭据、恢复配置并允许重试。旧预览取消不能清掉新预览。
6. 主机地址、端口、用户名及别名均与已有主机一致时跳过，不覆盖已有凭据。相同公钥指纹复用已有私钥；密钥名称不是匹配依据。
7. `proxies` 表中的 HTTP CONNECT / SOCKS5 代理映射到原生网络代理，绝不执行源 `content` 或任意 ProxyCommand。代理密码进入 host credential 的 `proxy_password`，元数据仅含类型、地址、端口和用户名；沿用已有加密 SSH 同步信封。终端、SFTP 和隧道共用代理路径，连接失败不回退直连。导入代理主机同时写入保留标记 `proxy_command=atterm:native-proxy:v1`，新版仅在配有原生代理元数据时识别此标记；旧版会按不支持的 ProxyCommand 拒绝，避免忽略新字段后误直连。
8. `host_chains.ssh_config → ssh_configs` + 有序 `hosts_chain` 引用映射为跳板链；同时兼容明确的 `jump_host` / `host_chain` 引用。选目标时自动包含跳板及其密钥。目标保存为 `ProxyJump=atterm-id:<saved-id>`，运行时按 ID 精确解析，避免同名别名或同地址记录导致串凭据。缺引用、环、超过 10 跳或不能保持顺序的代理/跳板混合路线禁用导入；已有跳板路线不同则拒绝覆盖并保留预览。
9. 主机分组按根到叶顺序独立保存为 `group_path` 数组，保留任意深度、同名父子和名称中的斜杠；标签继续保存到 `tags`。主机页逐层展示子分组及直属主机，分组数量包含后代主机。父级按 Termius `parent_group` 引用解析（兼容 `parent` / `group`）；缺失或循环引用禁用对应主机。旧版已导入的主机在再次确认导入时补齐空路径或仅保存了末级分组的路径，不改凭据、路由或其它配置，也不重复创建；完整路径和不匹配的自定义路径不覆盖。合法的 local/remote/dynamic 转发规则保存到已有隧道配置，`pf_rules.label` 保存为隧道 `note`，导入不启动隧道。再次导入时可按类型、绑定地址/端口及目标地址/端口的唯一对应关系补齐旧隧道的空名称，保留规则 ID、现有名称和配置；重复端点不猜测匹配。
10. 目标使用现有 SSH credential/key slot，沿用 atterm 本机凭据存储行为（含其已有的系统钥匙串不可用回退），不新增另一套持久化机制。

## 当前兼容边界

- 支持普通对象、字符串、数字、布尔、数组、日期及非循环引用的 V8 v13–16 数据，以及 Blink trailer / Snappy 包装。
- 不支持外置 IndexedDB blob、硬件不可导出私钥、未知序列化类型或加密套件；报错而不猜测解析。
- 未知代理类型（含 SOCKS4 / HTTPS 代理）、任意代理命令及无法解析的跳板和分组继承配置禁用对应主机导入，避免静默变成直连。网络代理仅支持用于本机直连目标或首个 SSH 跳板，不能在后续跳板处另开本机代理。缺少明确用户名或私钥无法解析时同样禁用；缺密码的普通主机可导入，连接时补凭据。
- 启动命令、known_hosts、命令片段不迁移。不自动信任源主机指纹。
- 数据库输入总量上限 256 MiB，单个块/值展开上限 32 MiB；超过上限报错。
- 已在 Linux 本机配置上完成只读验证：59 台主机、10 个密钥、1 个 HTTP 代理及 4 条跳板链。未自动连接这些真实主机；实际连接由本地 HTTP/SOCKS 测试代理与 SSH 测试服务验证。其它平台/版本仍需实机验证。

## 验证

- `go test ./internal/termius`
- `go test -race -tags webkit2_41 ./desktop -run TestTermius`
- `cd desktop/frontend && npm test -- --run src/components/TermiusImportDrawer.test.ts src/components/SshHostsPanel.test.ts`
- 真实安装验证：退出 Termius → 打开预览 → 核对同为 root 的不同主机、不同安装的数据目录和私钥关联 → 选一台合成测试主机导入并连接 → 再次预览确认已有主机跳过。

## 格式研究来源

参考 [y01and3/termius-export](https://github.com/y01and3/termius-export)、[LXiuu/termius-data-exporter](https://github.com/LXiuu/termius-data-exporter)、[ZacharyZcR/termius-exporter](https://github.com/ZacharyZcR/termius-exporter) 的格式与关联研究，及 Chromium/V8 与 LevelDB 的存储格式。实现为 Go 读取器，不分发上述 Python/JS 项目或其运行环境，也不调用它们的脚本。用于研究的 ccl Chromium reader 展示了 Blink/V8 的编码结构，本仓未复制其解析源码。
