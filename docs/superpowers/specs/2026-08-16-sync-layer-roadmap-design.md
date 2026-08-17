# 配置同步层作为主线 —— v0.5 / v0.6 / v0.7 方向 — design

Date: 2026-08-16
Status: Accepted — item 19 delivered; items 20-32 pending.
See also: [docs/roadmap.md](../../roadmap.md) · [2026-08-10 desk widget](./2026-08-10-desk-widget-design.md) · [2026-08-16 hook-driven task state](./2026-08-16-hook-driven-task-state-design.md) · [2026-08-04 refactor roadmap](../plans/2026-08-04-refactor-roadmap.md)

## 0. Summary

atterm 的 P0/P1/P2 已全部完成，v0.4.x 以修复和重构收尾。本设计确定之后三个
版本的方向：**把已经存在但只喂了 8 个 key 的配置同步引擎当作主线，围绕它补
本地终端基本功和 SSH 主机能力**。

选这条主线的原因是：`internal/prefssync` + `desktop/ssh_sync.go` 已经跑通了
「per-key LWW 同步 + `account_key` sealed 信封」的完整机制，但目前只同步 8 个
key。而 roadmap Backlog 里堆着的字体、字号、启动目录、环境变量、快捷键自定义，
本质上都是这台引擎缺的内容物；#335 落地的 SSH 主机与密钥则是同一台引擎的另一
类内容物。把它们当成两串独立待办去做，会重复解决同一个持久化与跨端问题；当成
一条主线做，每补一项能力都同时提升跨端可用性。

## 1. Goals

- 让 atterm 在不登录 relay 的情况下，本地终端能力没有明显缺口（scrollback 搜
  索、字体与外观、profile、快捷键）。
- 让登录 relay 后，用户的终端配置、profile、SSH 主机与密钥在多台设备之间自动
  一致，且 relay 在密码学上读不到其中的敏感部分。
- 让 SSH 主机管理从「记住地址」扩到「能干活」：端口转发、跳板机、文件传输、
  多机执行。
- 不引入任何强制账号或订阅门槛；未登录 = 全功能纯本地。

## 2. Non-Goals

**注意力调度线不在本设计范围内。** 挂件形态 B / C 与自定义形象包、`running`
会话置顶排序、尚未接入的 hook 事件（`SessionStart` / `SubagentStart|Stop` /
`Pre|PostCompact`）都属于 [desk widget](./2026-08-10-desk-widget-design.md) 与
[hook-driven task state](./2026-08-16-hook-driven-task-state-design.md) 那条线，
各自的 spec 已经点名为后续项。这是**主动取舍，不是遗漏**：本阶段的精力压在配置
与主机能力上，注意力线维持现状并按需修 bug。

其余不做的：

- 不扩 AI 会话托管能力：不加新 agent 集成、不做语音输入、不做手表端、不做
  localhost 预览。远程接管单个 AI 会话这一场景已有多个外部实现（Anthropic 官方
  Claude Code Remote Control、Warp 的多 agent 同屏、Happy、Omnara），继续加码
  收益有限。
- 原 P3（单 session 分享 / request-control 审批 / 审计日志）降级到 backlog。
  当前 claim 式控制权抢占已够用，审计日志在单用户场景没有消费者。
- 原 P4（可选持久化历史 / 命令级回放）暂缓。命令级回放与「默认不持久化终端
  历史」的既有姿态冲突，且实现成本高于其使用频次。
- 不引入任何 relay 可读的同步路径。不做「明文 fallback 让服务端顺便处理一下」
  的口子。

## 3. 现状盘点

2026-08-16 翻代码确认的事实，后面的排期建立在这些之上。

**已具备**

- `internal/prefssync/sync.go`：per-key LWW 同步引擎，`Adapter` 读写本地、
  `RelayClient` 走 `GET/PUT /api/me/preferences`，冲突按 `UpdatedAtLocal` /
  `UpdatedAt` 时间戳解。
- `syncedKeys` 当前 8 项：`locale_preference`、`quick_templates`、
  `notifications_enabled`、`ai_notifications_only`、
  `command_notify_threshold_seconds`、`shell_integration_enabled`、
  `pinned_session_ids`、`ssh_hosts_encrypted`。
- `desktop/ssh_sync.go`：主机 + 凭据 + 私钥 + passphrase 打包成一个 blob，用
  `e2eecrypto.DeriveSessionKey(accountKey, sshHostsSyncSessionID)` 封装，AAD
  tag `0xF0`（仅绑定 AAD，不上 relay wire）。`accountKey` 为空时返回
  `(nil, nil)`，调用方跳过同步，绝不发明文。
- `desktop/config.go` 的 `PrefsMeta` / `PrefsSeedMarkers`：per-key 同步状态与
  首登播种标记。
- **终端链接已完成**：`composables/useTerminalLinkProvider.ts` + `lib/terminalLinks.ts`
  提供 URL 单击直接打开与软换行 URL 拼接（S1，PR #281），本地文件路径点击在文件
  浏览器中定位预览（S2）。**此项不再是缺口。**
- `FS_REQUEST` / `FS_RESPONSE` / `FS_EVENT` 帧 + `desktop/fs_host.go` +
  `plugins/fileExplorer`：已有的远程文件浏览与编辑通道。
- `lib/shortcutBindings.ts` + `SettingsShortcuts.vue`：快捷键 action 表与自定
  义 UI，绑定存在 `Plugins.shortcuts.bindings`（本地持久化）。

**缺口**

- 无 `xterm-addon-search`（xterm v5 下的无 scope 包名；非 v6 的 `@xterm/addon-search`）：scrollback 不可搜索。
- ~~`TerminalView.vue` 的 `scrollback: 5000` 是硬编码常量（#343 为抗输出洪水从更
  大的值下调而来），用户无法按机器性能调整。~~
  ~~无字号 / 行高 / 光标样式配置；字体族固定在 `lib/terminalFont.ts` 常量里。~~
  2026-08-17 补记：item 20 已交付——字体族头部、字号、行高、光标样式/闪烁、
  scrollback 五项均已变为持久化的用户设置并实时应用到面板，见
  `2026-08-17-terminal-appearance-design.md`。本节其余是 item 20 之前的点时
  快照，保留供参照；`docs/roadmap.md` 是活的 tracker。
- 无 profile 概念：shell、启动目录、环境变量三者散落，其中后两者尚不存在。
- `terminal_theme`、`default_shell`、快捷键绑定**已在本地持久化但不在
  `syncedKeys` 里**，换台机器要重配。
- macOS 安装包未签名 / 未公证（roadmap P1.8 未完成）。

## 4. 内容物分层

同步的东西按「泄露后是否有实际损失」分层，这条判据决定它走明文路径还是 sealed
路径：

| 层 | 内容 | 路径 | 状态 |
|---|---|---|---|
| L0 | locale、快捷模板、通知偏好、shell 集成、置顶 | 明文 LWW | 已同步 |
| L1 偏好 | 主题、字体族 / 字号 / 行高、光标样式、scrollback 行数、快捷键绑定表 | 明文 LWW | 已接同步（2026-08-17 已有配置接入同步） |
| L2 环境 | profile：名称 + shell + 启动目录 + 环境变量 + 启动命令 | **sealed** | 待建 |
| L3 凭据 | SSH 主机、密钥、known_hosts；后续增加端口转发规则、跳板机链 | sealed | 机制已通，内容待扩 |

`webgl_renderer_enabled`（渲染器开关）不在 L1 表里：它不是偏好，是本机适配——正确值取决于
本机 GPU 驱动（`TerminalView.vue` 记着 Linux + NVIDIA 专有驱动 + X11/WebKitGTK 的已知输入延迟，
#48）。同步这个键等于把一台机器上的适配值传染给驱动完全不同的另一台机器，详见
[2026-08-17 已有配置接入同步 design](./2026-08-17-prefs-sync-l1-design.md) §4.2。

三条约束：

1. **L2 / L3 一律 sealed**，复用 `ssh_sync.go` 的 `DeriveSessionKey` 模式，每个
   sealed 命名空间分配独立的 AAD tag。这批 tag 不上 relay wire，但仍需在本设计
   与 `docs/spec/protocol.md` 的 sealed 信封表里登记，避免跨命名空间重放（同红
   线 #22 的纪律）。
2. **同步是可选的**。未配置 relay 或未登录时，所有配置纯本地可用，功能不减
   （红线 #1 本地优先）。
3. **L1 明文与 L2/L3 sealed 的分界不许模糊**。主题字号泄露无损失，环境变量与
   私钥泄露有损失。新增配置项落哪一层，按这条判。

## 5. 排期

这是方向文档，不是实现计划。下表每一项在动工前各自写自己的 design + plan；本节
只定内容与顺序。同一版本内的条目允许并行或调序，跨版本的顺序是有依赖的：v0.6
的主机能力依赖 v0.5 建立的 profile 与同步内容物模型。

本节的编号是文档内局部编号；[`docs/roadmap.md`](../../roadmap.md) 把它们登记为
全局第 19–32 项（P5 / P6 / P7 三节），勾选状态以 roadmap 为准。

### v0.5 —— 补齐本地终端基本功，并把已存在的配置接入同步

| # | 条目 | 说明 |
|---|---|---|
| 1 | scrollback 搜索 | `xterm-addon-search`，`Mod+KeyF`（macOS `Cmd+F`，其它平台 `Ctrl+F`）打开，匹配高亮 + 上下跳转 + 计数；会话侧栏搜索改绑 `Cmd/Ctrl+Shift+F` |
| 2 | 终端外观设置 | 字体族 / 字号 / 行高 / 光标样式与闪烁 / **scrollback 行数**（把 `TerminalView.vue` 的硬编码 5000 提成配置，保留 5000 为默认）。改 `lib/terminalFont.ts` 时必须保持红线 #13 的 CJK-first 字体栈顺序 |
| 3 | 已有配置接入同步 | `terminal_theme` + `default_shell` 加进 `syncedKeys`；快捷键绑定从 `Plugins.shortcuts.bindings` 拆成独立 key `shortcut_bindings` 后接入。首次接入必须走 `PrefsSeedMarkers` 播种路径，不能直接 pull 覆盖本地既有配置 |
| 4 | profile（会话配置档） | name + shell + 启动目录 + 环境变量 + 启动命令；新建 tab / split 时可选 profile，可设默认。一条覆盖 Backlog 的「默认 shell 设置改进」「启动目录设置」「环境变量设置」三项。sealed 同步 |
| 5 | OSC 133 click-to-move-cursor | 提示符内点击移动光标。`internal/session/applyOSC133Locked` 已有完整 OSC 133 解析，此项只需前端补 click → 光标位移 |
| 6 | macOS 分发改善 | Homebrew cask 分发（`brew install --cask atterm`），以及 `install-darwin.sh` 内 `xattr -d com.apple.quarantine` + 安装文档/FAQ 的 Gatekeeper 说明 |

### v0.6 —— SSH 主机从「记住地址」到「能干活」

全部挂在已有 sealed vault 上，不新建同步机制。

| # | 条目 | 说明 |
|---|---|---|
| 7 | 导入 `~/.ssh/config` | 已有配置的用户零成本把主机清单搬进来，排在这一期最前 |
| 8 | 端口转发 | 本地 / 远程 / 动态 SOCKS 三种；规则存进 host 记录随 vault 同步；活跃隧道状态面板 |
| 9 | ProxyJump / 跳板机链 | 单跳与多跳；链路配置存进 host 记录随 vault 同步；从 `~/.ssh/config` 导入时一并识别 `ProxyJump` / `ProxyCommand` |
| 10 | SFTP 浏览与传输 | 复用 `FS_REQUEST` / `FS_RESPONSE` + fileExplorer，给它加第三个数据源（本机 / 远端 host / SSH host），避免另写一套文件浏览 UI |
| 11 | snippets 多机执行 | `quick_templates` 已同步，扩成「选中 N 台主机 → 执行 → 汇总输出」 |

### v0.7 —— 同步层收口

| # | 条目 |
|---|---|
| 12 | 同步状态可见：同步指示器、「此项已在另一台设备更新」提示、手动 push / pull |
| 13 | 配置导出 / 导入（明文 JSON 文件，不经 relay），用户不依赖 relay 也能迁移全部配置 |
| 14 | 移动端消费 profile 与 SSH 主机清单 |

### 阻塞于外部凭据（不占版本位）

- macOS codesign + notarize workflow、Windows code signing workflow（原 P1.8）。
  需要 Apple Developer 证书与 Windows 签名证书，凭据到位后随时插队。v0.5 的
  条目 6 是不依赖这些凭据的替代方案，不是它的等价物。

## 6. 风险与缓解

1. **profile 的环境变量会离开本机**。虽然 sealed，但确实上传到 relay。缓解：
   per-profile 提供「不同步此 profile 的环境变量」开关，且**默认不同步**，用户
   显式打开才走网络。
2. **SFTP 复用 FS 协议可能不适配**。`FS_REQUEST` 是按本机文件系统的延迟特性
   设计的，SSH 远端的往返延迟与大目录列举行为不同。缓解：v0.6 先只做只读浏览
   + 单文件上传下载，验证通过再扩 CRUD；不适配就退回独立通道，止损范围限制在
   一个 PR 内。
3. **端口转发引入长连接**。与 lazy uplink 的「远程静默时不传字节」语义（红线
   #2）无关，但需独立的生命周期管理，不要把隧道状态混进 uplink 的订阅计数。
4. **条目 3 的接入可能覆盖老设备本地配置**。LWW 语义下，一台从未同步过的设备
   首次登录若直接 pull，会用另一台的值覆盖本地。必须走已有的
   `PrefsSeedMarkers` 播种路径（`app_relay.go` 的 `SeedFromLocal` + `Push`）。
5. **快捷键绑定拆 key 会动 `Plugins` 结构**。拆出 `shortcut_bindings` 独立 key
   时需要一次性迁移：读到老结构就搬进新 key 并清掉旧字段。
6. **scrollback 行数放开会重新引入 #343 修掉的问题**。#343 把 scrollback 下调
   到 5000 是为了让终端在输出洪水下存活；这不是一层独立的「洪水保护」机制，
   下调 scrollback 本身就是当时唯一的缓解手段，此外并无别的机制兜底（详见
   `2026-08-17-terminal-appearance-design.md` §3 发现 2）。放开为配置项时必须
   保留上限约束与默认值，并在设置项旁说明调高的内存代价。

## 7. 验证

- 每个同步 key 的接入都要有 prefssync 层的单测（本地更新 → dirty → push；远端
  更新 → pull → 本地生效；同时更新 → 时间戳裁决）。
- profile 的 seal / open 走与 `ssh_sync_test.go` 同构的往返测试，并覆盖
  `accountKey` 为空时不产生任何网络写入。
- 终端外观与搜索走 `desktop/frontend` 的 Vitest。scrollback 不存在独立的连接层
  洪水保护可测（见 §6 风险 6），验证只需覆盖上限约束（20000）与默认值不变；
  不再补「确认洪水保护仍生效」这类不可执行的测试项。
- OSC 133 click-to-move-cursor 需要在真实 shell（zsh + fish）下手动验证一次，
  因为它依赖 shell 侧的 `cl=line` 扩展支持。
- SSH 相关条目在本机起 sshd 容器做集成验证，不依赖外部主机。
