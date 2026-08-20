# 路线图

## 状态速览 (2026-08-16)

- **v0.4.19** 已发布(2026-08-16)。桌面 dmg / Linux deb+tar / Windows exe+zip 全平台构件已 upload,SHA256SUMS + GPG 签名附。
- **下一阶段方向已定**:以配置同步层为主线,补本地终端基本功 + SSH 主机能力,见 [`docs/superpowers/specs/2026-08-16-sync-layer-roadmap-design.md`](./superpowers/specs/2026-08-16-sync-layer-roadmap-design.md)。对应下面的 **P5 / P6 / P7**;原 P3(协作)降级到 Backlog、原 P4(历史与回放)暂缓。
- **桌面挂件**(Desk Widget)已落地:置顶悬浮小窗显示所有会话的运行 / 失败 / 等待输入状态,点行跳转对应 tab。走插件模式(Settings → 插件),可选"仅 AI 会话"。设计与踩坑记录见 [`docs/superpowers/specs/2026-08-10-desk-widget-design.md`](./superpowers/specs/2026-08-10-desk-widget-design.md),不变量见 AGENTS.md 红线 #37。
- **task_state 改由 hook 驱动**(#341):Claude Code / Codex 的 `Stop` / `Notification` / `PreToolUse` 等事件接进 `internal/session`,取代输出静默启发式,AI 会话状态不再在 `running` ↔ `waiting_input` 之间抖。见 [`docs/superpowers/specs/2026-08-16-hook-driven-task-state-design.md`](./superpowers/specs/2026-08-16-hook-driven-task-state-design.md)。后续项:`running` 会话置顶排序、挂件形态 B/C、尚未接入的 `SessionStart` / `SubagentStart|Stop` / `Pre|PostCompact`。
- **终端链接已完成**:URL 单击直接打开 + 软换行 URL 拼接(#281),本地文件路径点击在文件浏览器中定位预览。
- P0/P1/P2 核心功能全部完成并稳定;v0.4.x 系列以修复 + 内部重构为主。
- 大规模重构完成:App.vue 2326→1749 (-25%)、TerminalView.vue 抽 2 slice、api.ts 1098→299 (-73%)、Go 巨型文件全套拆分。详见 [`docs/superpowers/plans/2026-08-04-refactor-roadmap.md`](./superpowers/plans/2026-08-04-refactor-roadmap.md)。
- 待办:M5-b 剩余 6 slice(TerminalView composable 抽取,需 iOS Simulator 逐 slice 验证);具体见上述 refactor-roadmap 尾部"剩余待办"表。

## P0：v0.3 核心接管闭环

### 1. 任务状态模型

- [x] 定义会话任务状态：`running`、`waiting_input`、`completed`、`failed`、`idle`、`disconnected`、`closed`
- [x] 基于 OSC 133 事件推导命令生命周期
- [x] 记录当前命令、开始时间、结束时间、运行时长和退出码
- [x] 记录最近输出时间
- [x] 增加等待输入识别规则：`[y/N]`、`[Y/n]`、`continue?`、`proceed?`、`confirm`、`press enter`、`password:`
- [x] 在 relay session metadata 中暴露任务状态
- [x] 将任务状态同步到 desktop、web 和 mobile 客户端
- [x] 增加任务状态流转测试

### 2. 移动端任务首页

- [x] 将移动端优先会话列表改为任务卡片
- [x] 按状态分组任务：`needs_attention`、`running`、`completed`、`failed`、`disconnected`
- [x] 在任务卡片展示 session 标题、host、cwd、当前或最近命令、任务状态、运行时长、最近输出时间和权限模式
- [x] 支持从任务卡片进入终端 attach
- [x] 高亮需要输入的任务
- [x] 高亮失败任务
- [x] 增加无活跃任务空状态
- [x] 增加 relay disconnected 空状态
- [x] 增加移动端视口测试

### 3. 通知深链

- [x] 在 Web Push payload 中包含 session id
- [x] 在 Web Push payload 中包含通知类型
- [x] 支持命令完成通知
- [x] 支持命令失败通知
- [x] 支持等待输入通知
- [x] 支持 idle timeout 通知
- [x] 支持 uplink disconnected 通知
- [x] 点击通知后打开目标 session
- [x] 等待输入通知打开后聚焦终端输入区
- [x] 打开 view-only session 时显示权限提示
- [x] 增加 push payload 路由测试

### 4. 移动端快捷控制面板

- [x] 在移动端终端视图增加控制面板
- [x] 增加 Enter 快捷键
- [x] 增加 Esc 快捷键
- [x] 增加 Tab 快捷键
- [x] 增加 Ctrl-C 快捷键
- [x] 增加 Ctrl-D 快捷键
- [x] 增加方向键快捷键：Up、Down、Left、Right
- [x] 增加快捷文本：`y`、`n`、`yes`、`no`、`continue`
- [x] 增加粘贴确认
- [x] 增加显式控制模式开关
- [x] view-only session 禁用控制按钮
- [x] 增加权限控制测试

### 5. Relay 连接向导

- [x] 在桌面端增加 relay setup wizard
- [x] 增加 relay URL 输入步骤
- [x] 校验 relay 可达性
- [x] 校验 HTTP/HTTPS 和 WS/WSS 兼容性
- [x] 校验 API token
- [x] 校验用户身份
- [x] 校验 uplink 连接状态
- [x] 识别 relay unreachable 错误
- [x] 识别 invalid token 错误
- [x] 识别 origin rejected 错误
- [x] 识别 insecure ws blocked 错误
- [x] 识别 incompatible relay version 错误
- [x] 识别 permission denied 错误
- [x] 为每类失败提供恢复操作
- [x] 增加连接向导状态测试

## P1：v0.4 新用户引导与可信度

### 6. 手机二维码配对

- [x] 在桌面端生成配对二维码
- [x] 在配对流程中包含 relay URL
- [x] 增加短期一次性 pairing token
- [x] 增加移动端 pairing setup route
- [x] 支持用 pairing token 交换移动端凭据
- [x] pairing token 首次使用后失效
- [x] pairing token 超时后失效
- [x] 显示 token 过期错误
- [x] 显示 token 无效错误
- [x] 增加 pairing token 生命周期测试

### 7. Relay 健康检查页

- [x] 增加 relay health page
- [x] 显示 relay version
- [x] 显示 web build version
- [x] 显示 HTTPS/WSS 状态
- [x] 显示已配置 origins
- [x] 显示 bootstrap admin 状态
- [x] 显示 rate limit 设置
- [x] 显示 active uplink 数量
- [x] 显示 mobile origin 兼容状态
- [x] 增加复制诊断信息按钮
- [x] 诊断信息脱敏
- [x] 增加 health payload contract tests

### 8. 桌面安装包签名

> 进度：已有 `.github/scripts/sign-release-checksums.go`（校验和签名），尚缺真正的 macOS codesign/notarize 与 Windows 代码签名（需 Apple Developer 证书 + Windows 签名证书等外部凭据）。

- [ ] 增加 macOS codesign workflow
- [ ] 增加 macOS notarization workflow
- [ ] 增加 Windows code signing workflow
- [ ] 在 CI 中验证已签名 release assets
- [ ] 更新 release asset 命名
- [ ] 增加签名包发布检查清单

### 9. 移动端安全存储

- [x] 将移动端 token 从 localStorage 迁出
- [x] 使用 Keychain 或原生安全存储保存移动端 token
- [x] 尽可能迁移已有 localStorage token
- [x] 迁移后删除 localStorage token
- [x] 收紧 iOS ATS 默认配置
- [x] 将 insecure HTTP mode 保留在显式用户设置后
- [x] 增加 insecure HTTP relay 风险提示
- [x] 增加 token 存储迁移测试

### 10. 诊断信息导出

- [x] 增加桌面端诊断信息导出
- [x] 导出 app version
- [x] 导出 OS version
- [x] 导出脱敏后的 relay URL
- [x] 导出 uplink 状态
- [x] 导出最近 relay 连接错误
- [x] 导出 WebView runtime version
- [x] 导出配置摘要
- [x] 默认不包含终端输出
- [x] 脱敏 API token、cookie 和 authorization headers
- [x] 增加脱敏测试

## P2：AI 任务控制台（已完成）

### 11. AI 与工作流命令识别

- [x] 识别 AI CLI 命令：`codex`、`claude`、`gemini`、`aider`
- [x] 识别测试命令：`go test`、`npm test`、`pnpm test`、`yarn test`、`cargo test`
- [x] 识别构建和部署命令：`docker build`、`docker compose`、`kubectl`、`terraform`
- [x] 增加 session 类型标签：`ai`、`test`、`build`、`deploy`、`shell`
- [x] 在 desktop、web 和 mobile 任务卡片展示类型标签
- [x] 增加命令识别测试

### 12. 结构化任务摘要

- [x] 保存当前命令摘要
- [x] 保存最近命令结果
- [x] 保存退出码
- [x] 保存运行时长
- [x] 保存最近 N 行输出
- [x] 提取最近错误行
- [x] 在移动端任务卡片展示摘要
- [x] 在 web session detail 展示摘要
- [x] 增加摘要提取测试

### 13. AI 快捷操作模板

> 已发：#99 `feat(p2.13)`（编辑器 + 预览 + 跨平台模板栏），#110 移除旧 quickInput，#111 增加热键/直接发送/隐藏开关/刷新默认值。

- [x] 增加快捷操作模板模型
- [x] 增加 approve 内置模板
- [x] 增加 deny 内置模板
- [x] 增加 continue 内置模板
- [x] 增加 run tests 内置模板
- [x] 增加 show diff 内置模板
- [x] 增加 retry 内置模板
- [x] 在 AI session 中展示模板
- [x] 支持用户自定义模板
- [x] 发送前预览模板文本
- [x] 复用现有远程权限校验
- [x] 增加模板发送行为测试

## P3：协作能力（已降级到 Backlog）

> 2026-08-16 决定：整段降级，不排进 v0.5–v0.7。单用户场景下审计日志没有消费者，
> 控制权当前的 claim 式抢占已够用。条目原样保留，将来有需要再捡起来。

### 14. 单 session 分享

- [ ] 增加 session share model
- [ ] 支持将 session 分享给指定用户
- [ ] 支持 view 分享权限
- [ ] 支持 control 分享权限
- [ ] 支持 10 分钟分享有效期
- [ ] 支持 1 小时分享有效期
- [ ] 支持当天分享有效期
- [ ] 支持手动撤销分享
- [ ] 增加分享管理 UI
- [ ] 按 owner 和 share grants 过滤 session 列表
- [ ] 在 relay 强制执行分享权限
- [ ] 在 desktop host 强制执行分享权限
- [ ] 增加分享过期测试
- [ ] 增加分享权限测试

### 15. Presence 与控制权

> 进度：driver/viewer 模式已奠基（#2/#3 driver/viewer 模式 + driver hostname，#76 镜像 session 反映上游 driver，#77 👁 N viewer 计数，#90 overlay 不漏点击）。已覆盖 viewer 跟踪/展示、controller 展示、控制权冲突防护；缺正式的 request-control / owner approve / owner revoke 授权流（当前是 claim 式抢占而非 owner 审批）。

- [ ] 跟踪每个 session 的活跃 viewer
- [ ] 在 desktop、web 和 mobile 客户端展示活跃 viewer
- [ ] 跟踪当前 controller
- [ ] 展示当前 controller
- [ ] 增加 request-control 操作
- [ ] 增加 owner approve-control 操作
- [ ] 增加 owner revoke-control 操作
- [ ] 防止控制权交接状态冲突
- [ ] 增加 presence 生命周期测试

### 16. 审计日志

- [ ] 记录 attach 事件
- [ ] 记录 detach 事件
- [ ] 记录 control granted 事件
- [ ] 记录 control revoked 事件
- [ ] 记录 input-sent 事件但不记录输入内容
- [ ] 记录权限变更事件
- [ ] owner 可查看审计日志
- [ ] admin 可查看审计日志
- [ ] 增加审计日志导出
- [ ] 增加审计记录测试
- [ ] 增加审计授权测试

## P4：历史与回放（已暂缓）

> 2026-08-16 决定：暂缓。命令级回放与「默认不持久化终端历史」的既有姿态冲突，
> 且实现成本高于其使用频次。条目原样保留。

### 17. 可选持久化历史

- [ ] 保持默认不持久化终端历史
- [ ] 增加单 session 历史保留开关
- [ ] 增加本地历史存储
- [ ] 增加历史保留大小上限
- [ ] 增加历史保留时间上限
- [ ] 增加删除历史操作
- [ ] 增加显式 relay 端持久化设置
- [ ] 尽可能加密持久化历史
- [ ] 在 UI 显示 saved-history indicator
- [ ] 增加历史保留测试
- [ ] 增加历史删除测试

### 18. 命令级回放

- [ ] 基于 OSC 133 命令生命周期切分输出
- [ ] 保存命令开始时间
- [ ] 保存命令结束时间
- [ ] 保存命令运行时长
- [ ] 保存命令退出码
- [ ] 保存命令输出片段
- [ ] 增加命令列表视图
- [ ] 增加失败命令过滤
- [ ] 增加命令输出回放视图
- [ ] 增加命令切分测试

## P5：v0.5 本地终端基本功与配置接入同步

> 方向与取舍见 [`2026-08-16-sync-layer-roadmap-design.md`](./superpowers/specs/2026-08-16-sync-layer-roadmap-design.md)。
> 每一项动工前各自写 design + plan。本期内条目可并行或调序。

### 19. scrollback 搜索

- [x] 引入 `xterm-addon-search`(xterm v5 下的无 scope 包名;非 v6 的 `@xterm/addon-search`)
- [x] `Mod+KeyF`(macOS `Cmd+F`,其它平台 `Ctrl+F`)打开搜索栏
- [x] 匹配高亮 + 上下跳转 + 结果计数
- [x] 增加搜索交互测试
- [x] 会话侧栏搜索快捷键随之从 `Cmd/Ctrl+F` 改绑 `Cmd/Ctrl+Shift+F`,避免与新的终端搜索冲突(见 [`site/docs/guide/remote-takeover.md`](../site/docs/guide/remote-takeover.md))

### 20. 终端外观设置

- [x] 字体族选择（保持红线 #13 的 CJK-first 字体栈顺序）
- [x] 字号设置
- [x] 行高设置
- [x] 光标样式与闪烁设置
- [x] scrollback 行数设置（把 `TerminalView.vue` 的硬编码 `5000` 提成配置，默认仍为 5000，保留上限约束）
- [x] scrollback 上限 20000 + 每 pane 内存提示（#343 没有独立的连接层洪水保护——低 scrollback 本身就是当时的缓解手段，见 design doc §3 发现 2；这里改为在设置项里提示每 pane 的内存开销，而不是新增一层不存在的保护）

### 21. 已有配置接入同步

- [x] `terminal_theme` 加进 `prefssync.syncedKeys`
- [x] `default_shell` 加进 `prefssync.syncedKeys`
- [x] 快捷键绑定从 `Plugins.shortcuts.bindings` 拆成独立 key `shortcut_bindings`
- [x] 老结构一次性迁移到新 key 并清掉旧字段
- [x] 首次接入走 `PrefsSeedMarkers` 播种路径；播种顺序是 Pull 先于 `SeedFromLocal`，
      relay 上已有值的 key 会覆盖本地既有配置，只有 relay 上还没有的 key 才保留本地值
      并被播种上传（未决问题，见设计 doc §7.1；`PrefsSeedMarkers` 只保证播种失败可重试，
      不保证"不覆盖"）
- [x] 每个 key 增加 prefssync 往返测试（push / pull / 时间戳裁决）

### 22. profile（会话配置档）

- [x] 增加 profile 模型：名称 + shell + 启动目录 + 环境变量 + 启动命令
- [x] 新建 tab / split 时可选 profile
- [x] 支持设置默认 profile
- [x] profile 走 sealed 同步（复用 `ssh_sync.go` 的 `DeriveSessionKey` 模式，分配独立 AAD tag）
- [x] 新 AAD tag 登记进 `docs/spec/protocol.md` 的 sealed 信封表
- [x] per-profile「不同步此 profile 的环境变量」开关，默认不同步
- [x] `accountKey` 为空时不产生任何网络写入
- [x] 增加 seal / open 往返测试
- [x] 本条覆盖原 Backlog 的「默认 shell 设置改进」「启动目录设置」「环境变量设置」

### 23. OSC 133 click-to-move-cursor（阻塞于 shell 支持，移出排期）

> 2026-08-17 决定：调查后移出 v0.5，与第 8 项签名同样处理——不是不做，是先决条件不在我们手里。
> 完整调查见 [`2026-08-17-click-to-move-cursor-design.md`](./superpowers/specs/2026-08-17-click-to-move-cursor-design.md)。
>
> 终端无法直接设置 shell 行编辑器的光标，只能发方向键。这只在 shell 声明 OSC 133 的
> `cl=line`（「我的提示符输入是单行，方向键在其中安全」）时才可靠，而目前只有 **fish 4.1+ 与
> nushell 0.111+** 声明它，**zsh 与 bash 不声明**。没有声明就发方向键，等于拿用户正在编辑的
> 命令行赌一个无从验证的前提，失败形态是命令被悄悄改成别的、可能回车后才发现。
>
> 该设计还订正了本条原先的一处事实错误：`internal/session/applyOSC133Locked` 在 Go 侧、
> 前端够不着，与本项无关；真正的挂载点是 `TerminalView.vue` 已有的
> `term.parser.registerOscHandler(133, …)`。结论（「只需前端补」）成立，理由不同。

- [ ] 前端补提示符内 click → 光标位移（等 zsh 支持 `cl=line` 后可直接开工）
- [ ] 宽字符（CJK）按字符而非按列折算方向键次数——设计 §7.3 标注为本项最易写错处

### 24. macOS 分发改善（已准备、待启用）

> 设计见 [`2026-08-17-macos-distribution-design.md`](./superpowers/specs/2026-08-17-macos-distribution-design.md)。
>
> **Homebrew 不是 Gatekeeper 的解法。** 本项立项时假设 `brew install --cask` 能免掉隔离标记——这是错的：Homebrew cask 默认给装好的 app 打 `com.apple.quarantine`，刻意模仿浏览器下载；`--no-quarantine` 在 Homebrew 5.0.0 弃用后已被移除，第三方 tap 也没有替代品。未签名的 app 无论走 dmg 还是走 brew，弹的是同一个对话框。cask 用 `caveats` 在安装完成时告诉用户要自己跑 `xattr`，**不加 `postflight` 自动去标记**——在别人机器上不打招呼就绕过 Gatekeeper，和用户读完解释后自己执行 `xattr` 是两回事。
>
> 所以本项实际交付的是：版本化安装、每次发版自动更新的两架构 checksum、一条顺手的升级路径，以及一份说法与事实一致的文档。**Gatekeeper 本身仍归第 8 项**（签名 + 公证，阻塞于 Apple Developer 证书）。
>
> 现状是「已准备、待启用」：`brew install --cask attson/tap/atterm` **现在还装不上**，承载 cask 的 tap 仓库 `attson/homebrew-tap` 与推送用的 token 需要用户亲自创建（对外动作，不在实现者手里）；在那之前 release workflow 的同步步骤按 secret 是否存在自动跳过并打日志说明原因。

启用步骤（**顺序不能反**）：

1. 先建 tap 仓库 `attson/homebrew-tap`（Homebrew 要求仓库名为 `homebrew-<name>`）。**不需要手工放第一个 cask 文件**——下一次打 tag 发版时 release job 会自动渲染并推上去。
2. 仓库建好之后，再把有写权限的 token 存进本仓库 secrets 的 `HOMEBREW_TAP_TOKEN`。反过来先配 token，同步步骤就不再跳过、而是在 `git clone` 一个不存在的仓库时失败，把下一次发版的 release job 弄红。

- [x] Homebrew cask 分发就绪（`packaging/homebrew/atterm.rb.tmpl` + `render-cask.sh` + release workflow 自动同步到 tap；tap 仓库与 token 待用户创建后 `brew install --cask attson/tap/atterm` 才真正可用）
- [x] cask `caveats` 提示未签名 + `xattr` 命令；`zap` 按 macOS 实际路径逐项列举，刻意不删 `users.db` / `keyring-fallback.json`（`account_key` 材料）
- [x] 安装文档 / FAQ 补 Gatekeeper 说明（含订正：brew 装完同样要跑 `xattr`）
- [x] `render-cask_test.sh` 接进 `build-linux`（PR 触发），模板回归不再等到发版才暴露

> 母 spec 还列过一项「`install-darwin.sh` 内 `xattr -d com.apple.quarantine`」。**本项没做这一项，也不需要做**：
> 该行早在本项之前就存在于 `desktop/scripts/install-darwin.sh`，且只覆盖自动更新路径（updater 解包后调用），
> 与首次安装无关。这里记一笔只是说明它已被排除，不是本项的交付物。

## P6：v0.6 SSH 主机能力

> 全部挂在已有 sealed vault 上，不新建同步机制。

### 25. 导入 `~/.ssh/config`

> 设计见 [`2026-08-17-ssh-config-import-design.md`](./superpowers/specs/2026-08-17-ssh-config-import-design.md)。
>
> **识别不等于能连。** `ProxyJump` / `ProxyCommand` 会被解析并随主机记录导入、随 sealed vault 同步，但带这两个字段的主机**拒绝直连**——`NewSshSessionByID` 会在发起 dial 之前直接报错。跳板链路本身是第 27 项，尚未实现；在那之前，导入这类主机只是把配置记下来，不是让它能用。
>
> **订正（第 27 项已完成）：上面这段只剩 `ProxyCommand` 那一半成立。** `ProxyJump` 的主机现在能连——前提是链路上每一跳都已经是保存的主机，见第 27 项。
>
> `IdentityFile` 同理只记路径，atterm 不读取私钥文件内容；`AuthKind` 会置为 `"key"`，但 `KeyID` 留空，要用户自己走既有的导入私钥流程去关联。
>
> 新增的三个字段（`IdentityFile` / `ProxyJump` / `ProxyCommand`）随整块 `ssh_hosts_encrypted` sealed JSON 同步。第 21 项的教训是这类假设必须验证：`desktop/ssh_hosts_sync_fields_test.go` 用真实 seal → open 往返加带 canary 值的用例证明三个字段不丢；relay 的 `allowedPreferenceKeys`（`internal/userstore/preferences.go:37`）本来就已经放行 `ssh_hosts_encrypted`，未受影响。

- [x] 解析主机条目并导入主机清单
- [x] 一并识别 `ProxyJump` / `ProxyCommand`（导入并标记，**不建立跳板连接**——直连主机被拒绝，见上）
- [x] 增加解析测试

### 26. 端口转发

> 设计见 [`2026-08-17-ssh-port-forwarding-design.md`](./superpowers/specs/2026-08-17-ssh-port-forwarding-design.md)。
>
> **跳板机上的转发不支持。** 带 `ProxyJump` / `ProxyCommand` 的主机起隧道会在发起 dial 之前直接被拒——第 27 项的跳板链路还没做，在那之前这类主机连隧道都开不了，更不用说经它转发。
>
> **订正（第 27 项已完成）：`ProxyJump` 的主机现在能起隧道**，走的是和终端完全相同的链路构建逻辑；`ProxyCommand` 的主机仍然被拒。一个前提没变：隧道路径背后没有 TOFU 弹框，所以链路上任何一跳的 host key 未知时会直接拒绝并说明先去终端里接受指纹——第 27 项让这句话真正可执行（在那之前保存的主机根本没法完成 TOFU）。
>
> **SOCKS5 只实现了 `CONNECT`。** 动态转发是自己写的最小 SOCKS5 服务端（标准库没有服务端实现），只支持 `NO AUTHENTICATION` + `CONNECT`，地址类型覆盖 IPv4 / IPv6 / DOMAINNAME；`UDP ASSOCIATE` 与 `BIND` 均未实现，收到时按 RFC 1928 回一个规规矩矩的 `X'07' Command not supported`，不断链、不 panic，但也不代理这两类流量。
>
> **隧道不会随连接自动起。** 打开一个到主机的终端不会顺带拉起它保存的转发规则——一条规则占用一个本地端口，自动起会让「开个终端」变成「悄悄抢了 5432」。所有隧道都要显式调用 `StartForward`。
>
> **连接掉线不会自动重连。** 底层 SSH 连接断开时，规则状态直接标记为已停止并附带原因（在活跃隧道面板可见），不会自己重试重连。
>
> 转发规则随 `SSHHost.Forwards`（`[]ForwardRule`）走既有的整块 `ssh_hosts_encrypted` sealed JSON 同步，没有新建同步机制。第 21 项的教训是这类假设必须验证：`desktop/ssh_hosts_sync_fields_test.go` 新增的 `TestSealOpenSSHHostsRoundTripForwards` 用真实 seal → open 往返、每个字段都带 canary 值证明规则不丢，`TestSealOpenSSHHostsRoundTripForwardsEmpty` 确认没有规则的主机也能正常往返；relay 的 `allowedPreferenceKeys`（`internal/userstore/preferences.go:37`）本来就已经放行 `ssh_hosts_encrypted`——这张表是按 key 整体放行/拒绝，不看 key 内部的字段，所以给 `SSHHost` 加 `Forwards` 字段不需要、也不会触碰这张表。

- [x] 本地转发
- [x] 远程转发
- [x] 动态 SOCKS（仅 `CONNECT`，见上）
- [x] 转发规则存进 host 记录随 vault 同步
- [x] 活跃隧道状态面板
- [x] 隧道生命周期独立管理，不混进 uplink 的订阅计数（红线 #2）

### 27. ProxyJump / 跳板机链

> 设计见 [`2026-08-17-ssh-proxyjump-design.md`](./superpowers/specs/2026-08-17-ssh-proxyjump-design.md)。
>
> **跳板链上每一跳都必须是 atterm 里已保存的主机。** `ProxyJump bastion` 里的 `bastion` 按别名（其次按主机名）在主机清单里查，查不到就拒绝，并明说「请先把它添加为主机」——不会尝试连接。理由是凭据：跳板机自己要认证，而拿目标主机的凭据去连跳板等于把目标机的密码或密钥送给另一台机器，连接时弹框问凭据则要么每次都问、要么等于偷偷造了一条用户没审视过的主机记录。查已保存主机则凭据、端口、用户名一次到位，且都在用户已经看过的地方。`user@host:port` 形式只用来**匹配**已保存主机，不用来凭空构造一台没有凭据的主机。
>
> **`ProxyCommand` 仍然永不执行，本项没有改这一点。** 带 `ProxyCommand` 的主机在终端和隧道两条路上都仍然被拒，文案不变（第 25 项 §5.3：执行任意命令是 RCE 面）。第 25 / 26 项里「带 `ProxyJump` 的主机不能直连 / 不能起隧道」的说法从本项起**不再成立**，两处的界面标记也相应改成显示链路而不是拒绝；`ProxyCommand` 那半仍然成立。
>
> **每一跳独立校验 host key，TOFU 提示必须指名是哪一跳。** 这是本项唯一的安全红线。只校验最终目标意味着一台被替换的跳板可以在中间转发流量，而用户看到的是「目标指纹没变」。因此 `HostKeyUnknownError` 带上了跳序与该跳的名字，弹框会明说「这个指纹属于 bastion-b，是通往 db-1 途中的第 2 跳，不是 db-1 本身」；直连主机（跳序 0）的文案保持原样。相应地，一次「接受」只作用于用户当时看到的那一对 (host, fingerprint)——不是「接受下一个未知的 key」。这个区别不是风格问题：接受的 key 会**立刻写进 known_hosts**，所以一个笼统的接受会替用户从没见过的机器记下密钥，而被替换的那一跳下次连接时根本不会再问。
>
> **成环与深度在发起任何连接之前静态检出**（`a → b → a` 报错说明环路，上限 10 跳），每一跳的凭据也在第一次 dial 之前全部解析完——避免登录了三台跳板之后才发现目标没有凭据。中途某跳失败时，已经建好的前几跳会全部关闭，不会在跳板上留下挂着的会话。
>
> **跳板连接不共享。** 多个目标经同一台跳板时，每条链各建各的连接（设计 §6 风险 2：先不共享，链路语义比端口转发复杂，过早共享会让「哪条链还活着」难以推理）。
>
> 链路配置就是第 25 项加的 `SSHHost.ProxyJump` 字段，随整块 `ssh_hosts_encrypted` sealed JSON 同步，第 26 项的往返测试已经覆盖，**本项没有新增任何同步机制**。

- [x] 单跳
- [x] 多跳链路（`ProxyJump a,b,c` 从左到右依次穿过；成环与超深度在 dial 之前拒绝）
- [x] 链路配置存进 host 记录随 vault 同步（沿用第 25 项字段，无新增同步机制）
- [x] 终端与隧道两条路共用同一套链路构建逻辑
- [x] 每一跳独立校验 host key，TOFU 弹框指名跳序与主机名（保存的主机现在也能完成 TOFU：`NewSshSessionByID` 收下用户接受的那一对 (host, fingerprint)）

### 28. SFTP 浏览与传输

> 设计见 [`2026-08-18-sftp-browse-design.md`](./superpowers/specs/2026-08-18-sftp-browse-design.md)。
>
> **只在桌面端能浏览。** 这个数据源走的是 Wails binding，不是 relay 的 `FS_REQUEST` 通道——远程客户端（手机、浏览器、经 relay 挂上来的另一台桌面）看到的仍然只有本机和远端 host 两个源，SSH host 这一源根本不存在。这是刻意的：要让它经 relay 可用，得往 `FSRequestPayload` / `FSResponsePayload` 加字段（一个源标识、一个截断标记），而红线 #4（`AGENTS.md:17`）不允许改现有帧的 payload 结构。
>
> **只读浏览 + 单文件传输，删除也是单文件。** **远端目录的递归删除刻意没有做**——对面没有回收站，一次确认背后跟着一个不可撤销的递归删除，判断下来是不该上的东西。单文件远程删除是做的。
>
> **没有"存到本地磁盘"这个下载。** 读远端文件是为了喂给预览和编辑器，不是为了在这台机器上另存一份；SSH host 上的图片 / PDF 预览显示一句话而不是内容。
>
> **目录列举上限 2000 条**，超过的会截断，并且在界面上说明被截断了，不是悄悄少给用户看。
>
> 另外两点顺带说清楚：上传目标是文件树上当前选中的那个目录；带 `ProxyCommand` 的主机根本不出现在数据源列表里（连不上），带 `ProxyJump` 的主机正常出现且能用，沿用第 27 项的链路。
>
> **订正：第一条勾选当初论证挪出读循环时举的例子不成立了。** 当时说一次慢的 SFTP 列目录会卡住同一条 uplink 上所有会话的击键——这个具体场景不会发生，因为 SFTP 走的是上面说的 Wails 路径，根本不经过那条读循环。挪出读循环这件事本身仍然是对的、仍然值得做：同样的卡顿会发生在慢的本地磁盘上（网络挂载、休眠的外置盘），而且这是这个数据源将来要经 relay 可用的前提。但当初举的例子已经不适用了，不该让后来者以为它还成立。

- [x] 给 fileExplorer 加第三个数据源（本机 / 远端 host / SSH host；SSH host 这一源仅桌面端可见，见上）
- [x] 只读目录浏览
- [x] 单文件上传（本机 → 远端；无目录同步、无断点续传）。**下载（远端 → 本机磁盘）没有做**：它需要保存对话框与目标目录选择，是一个新界面，已从本项挪出，单独记为第 33 项
- [x] 验证 `FS_REQUEST` 通道在 SSH 往返延迟与大目录下的表现，不适配则退回独立通道 —— **问题问错了地方**：SFTP 走 Wails binding，根本不上 `FS_REQUEST` 通道；真正的阻塞点是桌面端曾把 FS 执行同步跑在 uplink 的读循环里，已挪到有界的每会话工作池，池满时明确返回"忙"而不是排队；relay 的 FS 请求路由表顺带补了 90s TTL 兜底一个永不响应的请求

### 29. snippets 多机执行

- [ ] 选中 N 台主机执行同一 snippet
- [ ] 汇总各主机输出
- [ ] 增加多机执行测试

### 33. SFTP 单文件下载到本机

> 从第 28 项挪出来的一条：那一项原本把「单文件下载（远端 → 本机）」写进 Goals，实际做的只有上传与「读远端文件喂给预览 / 编辑器」，没有任何把远端文件写到本机磁盘的路径——想把文件取下来只能在终端里 `scp`，图片和 PDF 则根本看不了内容。
>
> 挪出来而不是硬塞进第 28 项，是因为落盘要的是一个新界面：保存对话框、目标目录选择、以及重名与覆盖的处理。这与「接上第三个数据源」不是同一件事。设计见 [`2026-08-18-sftp-browse-design.md`](./superpowers/specs/2026-08-18-sftp-browse-design.md) §3（已从 Goals 移到 Non-Goals，并注明是推迟）。

- [ ] 文件树上的远端文件提供「下载到本机」动作
- [ ] 保存对话框选择本机目标目录，处理重名与覆盖
- [ ] 大文件分块落盘，不整份读进内存（沿用第 28 项的读取上限口径）

## P7：v0.7 同步层收口

### 30. 同步状态可见

- [ ] 同步指示器
- [ ] 「此项已在另一台设备更新」提示
- [ ] 手动 push / pull

### 31. 配置导出 / 导入

- [ ] 导出为明文 JSON 文件，不经 relay
- [ ] 从 JSON 文件导入
- [ ] 覆盖原 Backlog 的「主题导入导出」

### 32. 移动端消费 profile 与主机清单

- [ ] 移动端展示并使用 profile
- [ ] 移动端展示 SSH 主机清单

## 阻塞于外部凭据

> 需要 Apple Developer 证书与 Windows 签名证书，凭据到位后随时插队。
> P5 的第 24 项（Homebrew cask）**不缓解 Gatekeeper**，brew 装完照样弹同一个对话框；
> 它改善的是安装与升级体验，没有替代本项的任何部分。

见上文 [P1 第 8 项「桌面安装包签名」](#8-桌面安装包签名)。

## Backlog

- [ ] Webhook 规则自定义
- [ ] 插件工作流动作
- [ ] Linux rpm package
- [ ] Linux AppImage package
- [ ] 完整快捷键自定义（现有 `SettingsShortcuts.vue` 已覆盖大部分 action，剩余是补齐未纳入 action 表的操作）
- [ ] 协作能力（原 P3 整段，见上文）
- [ ] 注意力调度线后续项：`running` 会话置顶排序、挂件形态 B / C 与自定义形象包、尚未接入的 hook 事件

## 鉴权后续

来自 [docs/spec/auth.md](./spec/auth.md) 拆分时识别的迭代项：

- [ ] session refresh / proactive renewal（30 天 TTL 滑动续期或显式 refresh endpoint）
- [ ] 移动端 expires_at 触发预刷新（目前 `RelayConfig.session_expires_at` 已存但未使用）
- [ ] `/login.html?next=...` redirect 的 CSRF / open-redirect audit
- [ ] 桌面 App `localAdminPassword` 改用 OS Keychain 而非明文 `config.json`
