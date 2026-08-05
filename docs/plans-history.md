# Plans 历史归档

`docs/superpowers/plans/` 下 109 个已归档 plan 的一句话索引。这些 plan
是历史 implementation checklist，work 落地后 = 墓碑，详细内容永久保
留在 git 历史里：

    git log --diff-filter=D -- docs/superpowers/plans/YYYY-MM-DD-topic.md
    git show <sha>:docs/superpowers/plans/YYYY-MM-DD-topic.md

活着的路线图/待办见 [docs/superpowers/plans/2026-08-04-refactor-roadmap.md](./superpowers/plans/2026-08-04-refactor-roadmap.md)。

设计文档保留在 [docs/superpowers/specs/](./superpowers/specs/)，被代码
注释直接引用，不要动。

### 2026-08-03 · ssh-sync-slice3

SSH 主机清单 + 凭据整体用 account_key seal 后走 prefssync 跨端同步

**PR**: [#287](https://github.com/attson/atterm/pull/287) · **Spec**: [2026-08-03-ssh-sync-slice3-design-draft.md](./superpowers/specs/2026-08-03-ssh-sync-slice3-design-draft.md)

### 2026-08-03 · ssh-keys-vault

独立 SSH 密钥库；主机按 KeyID 引用；Key 与主机同 E2EE blob 同步

**PR**: [#287](https://github.com/attson/atterm/pull/287) · **Spec**: [2026-08-03-ssh-keys-vault-design-draft.md](./superpowers/specs/2026-08-03-ssh-keys-vault-design-draft.md)

### 2026-08-02 · ssh-host-store-slice2

SSH 主机清单持久化(config + keychain),支持一键连接和 known_hosts 管理

**PR**: [#287](https://github.com/attson/atterm/pull/287) · **Spec**: [2026-08-02-ssh-host-store-slice2-design-draft.md](./superpowers/specs/2026-08-02-ssh-host-store-slice2-design-draft.md)

### 2026-08-02 · ssh-connect-slice1

内建 Go SSH 客户端,把远程 shell 作为 PTY 源接入 AdoptSession + E2EE 管线

**PR**: [#287](https://github.com/attson/atterm/pull/287) · **Spec**: [2026-08-02-ssh-connect-slice1-design-draft.md](./superpowers/specs/2026-08-02-ssh-connect-slice1-design-draft.md)

### 2026-07-31 · s2-terminal-path-reveal

终端点击本地路径 → 在右侧文件浏览器逐级展开定位并预览,不再走 file://

**PR**: [#281](https://github.com/attson/atterm/pull/281) · **Spec**: [2026-07-31-s2-terminal-path-reveal-design-draft.md](./superpowers/specs/2026-07-31-s2-terminal-path-reveal-design-draft.md)

### 2026-07-31 · s1-terminal-links-file-actions

修 toast 遮挡;文件树支持 Ctrl-C/右键复制路径/终端引用;终端 URL 支持单击 + 软换行

**PR**: [#281](https://github.com/attson/atterm/pull/281) · **Spec**: [2026-07-31-s1-terminal-links-file-actions-design-draft.md](./superpowers/specs/2026-07-31-s1-terminal-links-file-actions-design-draft.md)

### 2026-07-31 · github-pages-site

VitePress GitHub Pages 站,首页用真实前端 + mock 后端直接体验,GH Actions 发布

**Spec**: [2026-07-31-github-pages-site-design-draft.md](./superpowers/specs/2026-07-31-github-pages-site-design-draft.md)

### 2026-07-28 · settings-admin-inline

SettingsDialog 补齐改密码/注销/Push;管理员面板内嵌进主 App;底部显示版本号

**PR**: [#274](https://github.com/attson/atterm/pull/274) · **Spec**: [2026-07-28-settings-admin-inline-design.md](./superpowers/specs/2026-07-28-settings-admin-inline-design.md)

### 2026-07-27 · web-layout-align-desktop

Web 主入口切到 desktop 布局(TabBar + TaskSidebar + PaneSplit),顺带全端 pin 同步

**PR**: [#272](https://github.com/attson/atterm/pull/272) · **Spec**: [2026-07-27-web-layout-align-desktop-design.md](./superpowers/specs/2026-07-27-web-layout-align-desktop-design.md)

### 2026-07-25 · sidebar-search-collapse

TaskSidebar 搜索区默认折叠为图标,点击或 Cmd+F 才展开输入框

**Spec**: [2026-07-25-sidebar-search-collapse-design.md](./superpowers/specs/2026-07-25-sidebar-search-collapse-design.md)

### 2026-07-25 · qr-e2ee-pair

QR 配对 URL 携带 AEAD-wrapped account_key,移动端扫码直接解密会话细节

**Spec**: [2026-07-25-qr-e2ee-pair-design.md](./superpowers/specs/2026-07-25-qr-e2ee-pair-design.md)

### 2026-07-24 · tab-drag-reorder

TabBar 支持鼠标/触摸拖拽重排 tab,实时排 + 边缘滚,结果写入 recovery

**Spec**: [2026-07-24-tab-drag-reorder-design.md](./superpowers/specs/2026-07-24-tab-drag-reorder-design.md)

### 2026-07-24 · sidebar-search

左侧会话栏头部内嵌搜索框,按 title/cwd/current_command 大小写无关子串过滤

**Spec**: [2026-07-24-sidebar-search-design.md](./superpowers/specs/2026-07-24-sidebar-search-design.md)

### 2026-07-24 · sidebar-multi-select-and-details

会话行右键详情;Cmd/Ctrl 多选后合并入新 tab 或批量关闭已开面板

**Spec**: [2026-07-24-sidebar-multi-select-and-details-design.md](./superpowers/specs/2026-07-24-sidebar-multi-select-and-details-design.md)

### 2026-07-23 · pinned-session-recovery

桌面重启后置顶的会话保持置顶(本地会话换新 session_id 也不掉 pin)

**Spec**: [2026-07-23-pinned-session-recovery-design.md](./superpowers/specs/2026-07-23-pinned-session-recovery-design.md)

### 2026-07-20 · session-bar-pin

会话行右键「置顶」,置顶会话聚到虚拟「📌 Pinned」组,配置持久化

**Spec**: [2026-07-20-session-bar-pin-design.md](./superpowers/specs/2026-07-20-session-bar-pin-design.md)

### 2026-07-17 · file-explorer-editing

把只读 File Explorer 扩为可编辑/保存/CRUD/垃圾桶(本地 + 远程会话皆可)

**PR**: [#269](https://github.com/attson/atterm/pull/269) · **Spec**: [2026-07-17-file-explorer-editing-design.md](./superpowers/specs/2026-07-17-file-explorer-editing-design.md)

### 2026-07-16 · remote-file-explorer

File Explorer 面向远程会话工作(浏览/预览/媒体/PDF/watch/open-external 一致)

**PR**: [#268](https://github.com/attson/atterm/pull/268) · **Spec**: [2026-07-16-remote-file-explorer-design.md](./superpowers/specs/2026-07-16-remote-file-explorer-design.md)

### 2026-07-10 · remote-file-channel

PASTE_FILE (0x37) 帧,attach 端上传 ≤10 MiB 文件到 owner PTY,注入绝对路径

**PR**: [#266](https://github.com/attson/atterm/pull/266) · **Spec**: [2026-07-10-remote-file-channel-design.md](./superpowers/specs/2026-07-10-remote-file-channel-design.md)

### 2026-07-09 · updater-cancel-and-existing-detect

Settings 更新加「Cancel (N%)」按钮 + 惰性探测已下载归档提示是否重新下载

**PR**: [#263](https://github.com/attson/atterm/pull/263) · **Spec**: [2026-07-09-updater-cancel-and-existing-detect-design.md](./superpowers/specs/2026-07-09-updater-cancel-and-existing-detect-design.md)

### 2026-07-09 · signed-in-devices

Settings 新增「已登录设备」tab,列出账号下所有设备,支持 revoke + sign-out-others

**PR**: [#264](https://github.com/attson/atterm/pull/264) · **Spec**: [2026-07-09-signed-in-devices-design.md](./superpowers/specs/2026-07-09-signed-in-devices-design.md)

### 2026-07-08 · clear-relay-info

Settings → Relay 加「Clear relay info」,一键抹掉全部 relay 状态但不动本地会话

**PR**: [#262](https://github.com/attson/atterm/pull/262) · **Spec**: [2026-07-08-clear-relay-info-design.md](./superpowers/specs/2026-07-08-clear-relay-info-design.md)

### 2026-06-27 · feishu-mode-override

加 FeishuModePref (auto/local/relay) 覆盖「跟随 Relay 登录态」的默认行为

**PR**: [#231](https://github.com/attson/atterm/pull/231) · **Spec**: [2026-06-27-feishu-mode-override-design.md](./superpowers/specs/2026-06-27-feishu-mode-override-design.md)

### 2026-06-27 · feishu-local-remote-terminal

本地模式下「启用飞书远程终端」开关能生效并触发 anchor card

**PR**: [#261](https://github.com/attson/atterm/pull/261) · **Spec**: [2026-06-27-feishu-local-remote-terminal-design.md](./superpowers/specs/2026-06-27-feishu-local-remote-terminal-design.md)

### 2026-06-26 · update-version-line-selector

设置页「软件更新」支持选择更新线(v0.2.x / v0.3.x),规则只升不降

**PR**: [#248](https://github.com/attson/atterm/pull/248) · **Spec**: [2026-06-26-update-version-line-selector-design.md](./superpowers/specs/2026-06-26-update-version-line-selector-design.md)

### 2026-06-26 · session-bar-multi-instance-distinction

同机器多个 atterm 实例的 host 组追加 #1/#2 后缀,按 host_id 排

**PR**: [#254](https://github.com/attson/atterm/pull/254) · **Spec**: [2026-06-26-session-bar-multi-instance-distinction-design.md](./superpowers/specs/2026-06-26-session-bar-multi-instance-distinction-design.md)

### 2026-06-26 · paste-image-preview

任一前端粘图后右上角弹缩略图 toast 确认,5s 自动消,可 hover/关闭/开 lightbox

**PR**: [#252](https://github.com/attson/atterm/pull/252) · **Spec**: [2026-06-26-paste-image-preview-design.md](./superpowers/specs/2026-06-26-paste-image-preview-design.md)

### 2026-06-26 · feishu-as-terminal

飞书 DM 成为轻量远程控制台:anchor card 流式尾输出 + 回复注入 PTY + 权限门

**PR**: [#261](https://github.com/attson/atterm/pull/261) · **Spec**: [2026-06-26-feishu-as-terminal-design.md](./superpowers/specs/2026-06-26-feishu-as-terminal-design.md)

### 2026-06-26 · feishu-ai-only-notifications

飞书通知加「仅 AI 会话」开关(默认开),只对 AI 会话推命令完成/等待输入

**PR**: [#249](https://github.com/attson/atterm/pull/249) · **Spec**: [2026-06-26-feishu-ai-only-notifications-design.md](./superpowers/specs/2026-06-26-feishu-ai-only-notifications-design.md)

### 2026-06-25 · feishu-interactive-cards

飞书卡片从只读通知升级为双向交互:快捷按钮/回复注入 PTY + 问题渲染成选项

**PR**: [#245](https://github.com/attson/atterm/pull/245) · **Spec**: [2026-06-25-feishu-interactive-cards-design.md](./superpowers/specs/2026-06-25-feishu-interactive-cards-design.md)

### 2026-06-24 · relay-userstore-dual-backend

internal/userstore 同时支持 SQLite 与 Postgres,行为一致 + 环境变量选后端

**Spec**: [2026-06-24-relay-multi-instance-persistence-design.md](./superpowers/specs/2026-06-24-relay-multi-instance-persistence-design.md)

### 2026-06-24 · relay-realm-identity

relay 暴露集群级 realm_id;E2EE account_key 按 realm 锚定,可跨节点/域名

**Spec**: [2026-06-24-relay-realm-identity-e2ee-reanchoring-design.md](./superpowers/specs/2026-06-24-relay-realm-identity-e2ee-reanchoring-design.md)

### 2026-06-24 · relay-migrate-subcommand

atterm-relay migrate --from/--to 子命令,忠实拷贝 userstore 后端数据

**Spec**: [2026-06-24-relay-multi-instance-persistence-design.md](./superpowers/specs/2026-06-24-relay-multi-instance-persistence-design.md)

### 2026-06-24 · relay-instance-registry

relay 实例注册表(心跳)+ user_home;登录 finalize 下发 home_instance_url

**Spec**: [2026-06-24-relay-instance-registry-node-selection-design.md](./superpowers/specs/2026-06-24-relay-instance-registry-node-selection-design.md)

### 2026-06-24 · relay-config-into-db

relay.json 与 web-push.json 迁入数据库,多实例共享同一份配置与订阅

**Spec**: [2026-06-24-relay-multi-instance-persistence-design.md](./superpowers/specs/2026-06-24-relay-multi-instance-persistence-design.md)

### 2026-06-24 · client-node-routing

客户端按登录响应的 home_instance_url 路由有状态 WS,空 home 回退到当前入口

**Spec**: [2026-06-24-client-node-routing-design.md](./superpowers/specs/2026-06-24-client-node-routing-design.md)

### 2026-06-23 · desktop-relay-password-persistence

OPAQUE relay 密码走 safekeyring 持久化,SettingsRelay 启动时预填,与 mobile 对齐

**PR**: [#225](https://github.com/attson/atterm/pull/225) · **Spec**: [2026-06-23-desktop-relay-password-persistence-design.md](./superpowers/specs/2026-06-23-desktop-relay-password-persistence-design.md)

### 2026-06-22 · desktop-logging-formatting

桌面日志统一 leveled/tagged 格式;加 PTY 输入 debug 日志;日志查看器上色 + 级别过滤

**PR**: [#224](https://github.com/attson/atterm/pull/224) · **Spec**: [2026-06-22-desktop-logging-formatting-design.md](./superpowers/specs/2026-06-22-desktop-logging-formatting-design.md)

### 2026-06-19 · claude-hook-auto-install

桌面自动落地 atterm-hook,合并 ~/.claude/settings.json 的 Notification 钩;Settings 加状态块

**PR**: [#199](https://github.com/attson/atterm/pull/199) · **Spec**: [2026-06-19-claude-hook-auto-install-design.md](./superpowers/specs/2026-06-19-claude-hook-auto-install-design.md)

### 2026-06-17 · feishu-hook-question

飞书通知走桌面直发;附 claude-code Notification 钩捕的问题文;支持无 relay onboarding

**PR**: [#197](https://github.com/attson/atterm/pull/197) · **Spec**: [2026-06-17-feishu-hook-question-design.md](./superpowers/specs/2026-06-17-feishu-hook-question-design.md)

### 2026-06-17 · feishu-app-integration

自建飞书应用取代 Format=feishu:加密应用凭据 + tenant_access_token 缓存 + 交互卡 + 短码绑定

**PR**: [#196](https://github.com/attson/atterm/pull/196) · **Spec**: [2026-06-17-feishu-app-integration-design.md](./superpowers/specs/2026-06-17-feishu-app-integration-design.md)

### 2026-06-16 · crash-window-session-recovery

桌面重启后恢复上次窗口的 tab/pane + cwd;按外观 AI session ID 自动续 AI 会话

**PR**: [#194](https://github.com/attson/atterm/pull/194) · **Spec**: [2026-06-16-crash-window-session-recovery-design.md](./superpowers/specs/2026-06-16-crash-window-session-recovery-design.md)

### 2026-06-15 · relay-e2ee-m1a-server-opaque

relay 加 OPAQUE 注册/登录接口 + 记录 + account-key wrap blob 存储(仅服务端 slice)

**PR**: [#159](https://github.com/attson/atterm/pull/159) · **Spec**: [2026-06-15-relay-e2ee-design.md](./superpowers/specs/2026-06-15-relay-e2ee-design.md)

### 2026-06-15 · multi-pane-resize

桌面拖动 vertical/horizontal/grid2x2 分隔条调列/行比;双击复位 50/50

**PR**: [#157](https://github.com/attson/atterm/pull/157) · **Spec**: [2026-06-15-multi-pane-resize-design.md](./superpowers/specs/2026-06-15-multi-pane-resize-design.md)

### 2026-06-15 · connection-health-monitoring

监控 WS 链路(RTT/字节率/重连/seq gap),用 pill + 按需 drawer 呈现质量

**PR**: [#156](https://github.com/attson/atterm/pull/156) · **Spec**: [2026-06-15-connection-health-monitoring-design.md](./superpowers/specs/2026-06-15-connection-health-monitoring-design.md)

### 2026-06-13 · terminal-link-detection

识别 xterm 行内 URL / 路径,悬浮装饰,⌘/Ctrl+Click 打开,右键菜单加打开/复制

**PR**: [#152](https://github.com/attson/atterm/pull/152) · **Spec**: [2026-06-13-terminal-link-detection-design.md](./superpowers/specs/2026-06-13-terminal-link-detection-design.md)

### 2026-06-12 · cross-platform-settings-sync

五个账号级偏好跨桌面/移动/Web 经 relay 同步,per-field LWW + 前台拉 + 变化推

**PR**: [#147](https://github.com/attson/atterm/pull/147) · **Spec**: [2026-06-12-cross-platform-settings-sync-design.md](./superpowers/specs/2026-06-12-cross-platform-settings-sync-design.md)

### 2026-06-11 · mobile-email-password-login

移动端把「URL + Token」换为「URL + 邮箱 + 密码」,登录 token 存 Keychain,QR 保留

**PR**: [#146](https://github.com/attson/atterm/pull/146) · **Spec**: [2026-06-11-mobile-email-password-login-design.md](./superpowers/specs/2026-06-11-mobile-email-password-login-design.md)

### 2026-06-10 · relay-uplink-pill-and-form-unify

uplink 4 态 pill(connected/connecting/invalid/off);Settings → Relay 表单摊平合并到「保存并连接」

**PR**: [#143](https://github.com/attson/atterm/pull/143) · **Spec**: [2026-06-10-relay-uplink-pill-and-form-unify-design.md](./superpowers/specs/2026-06-10-relay-uplink-pill-and-form-unify-design.md)

### 2026-06-10 · relay-settings-auth-state

重开 Settings → Relay 时立即显示「已连接为 X」pill + 预填邮箱,不再假装登出

**PR**: [#142](https://github.com/attson/atterm/pull/142) · **Spec**: [2026-06-10-relay-settings-auth-state-design.md](./superpowers/specs/2026-06-10-relay-settings-auth-state-design.md)

### 2026-06-10 · relay-password-toggle

Settings → Relay 的「连接远程 relay」表单密码框加显示/隐藏眼睛

**PR**: [#141](https://github.com/attson/atterm/pull/141) · **Spec**: [2026-06-10-relay-password-toggle-design.md](./superpowers/specs/2026-06-10-relay-password-toggle-design.md)

### 2026-06-10 · docs-revamp

docs/spec/auth.md 成为 auth 唯一来源;瘦身 protocol/architecture;统一 metadata 与 style;清陈旧引用

**PR**: [#138](https://github.com/attson/atterm/pull/138) · **Spec**: [2026-06-10-docs-revamp-design.md](./superpowers/specs/2026-06-10-docs-revamp-design.md)

### 2026-06-10 · ai-session-osc-title

AI 类会话在 TabBar/TaskSidebar/移动/Web + 窗口标题处显示 OSC 0/1/2 写入的 title

**PR**: [#144](https://github.com/attson/atterm/pull/144) · **Spec**: [2026-06-10-ai-session-osc-title-design.md](./superpowers/specs/2026-06-10-ai-session-osc-title-design.md)

### 2026-06-09 · relay-auth-token-removal

relay 只留 email+password → session_token 一条鉴权;pairing 也 mint session token;直接换库

**PR**: [#133](https://github.com/attson/atterm/pull/133) · **Spec**: [2026-06-09-relay-auth-token-removal-design.md](./superpowers/specs/2026-06-09-relay-auth-token-removal-design.md)

### 2026-06-09 · mobile-text-selection

MobileTerminal 加 iOS 式长按选文本 → 拖调整 → 浮层 Copy/Send 到 PTY

**PR**: [#132](https://github.com/attson/atterm/pull/132) · **Spec**: [2026-06-09-mobile-text-selection-design.md](./superpowers/specs/2026-06-09-mobile-text-selection-design.md)

### 2026-06-08 · mobile-session-list-align-desktop

MobileSessionList 对齐 TaskSidebar 分组模型:host↔state 切换/紧凑卡/已完成折叠/未读标

**Spec**: [2026-06-08-mobile-session-list-align-desktop-design.md](./superpowers/specs/2026-06-08-mobile-session-list-align-desktop-design.md)

### 2026-06-07 · task-state-detection-accuracy

侦测 AI/TUI 会话 alt-screen 静默,running → waiting_input(动 AttentionAt)输出回来切回

**PR**: [#124](https://github.com/attson/atterm/pull/124) · **Spec**: [2026-06-07-task-state-detection-accuracy-design.md](./superpowers/specs/2026-06-07-task-state-detection-accuracy-design.md)

### 2026-06-07 · file-explorer-preview

File Explorer 高亮 20 余种语言 + 图/音/视/PDF 内联预览,替代原「binary」提示

**PR**: [#123](https://github.com/attson/atterm/pull/123) · **Spec**: [2026-06-07-file-explorer-preview-design.md](./superpowers/specs/2026-06-07-file-explorer-preview-design.md)

### 2026-06-07 · desktop-task-sidebar-ux-trio

侧边栏三块打磨:去 AI type 图标 + 每行显 cwd 智能截断 + 拖调宽度并持久化

**PR**: [#124](https://github.com/attson/atterm/pull/124) · **Spec**: [2026-06-07-desktop-task-sidebar-ux-trio-design.md](./superpowers/specs/2026-06-07-desktop-task-sidebar-ux-trio-design.md)

### 2026-06-06 · session-attention-backend

relay 加 per-user 已读/未读:attention_at + seen_at + unread + 接管即已读 + 观看时抑制推

**PR**: [#118](https://github.com/attson/atterm/pull/118) · **Spec**: [2026-06-06-session-attention-model-design.md](./superpowers/specs/2026-06-06-session-attention-model-design.md)

### 2026-06-06 · desktop-task-state-display

桌面渲染 task_state/unread/type/summary:可折叠任务栏 + tab 点 + 富化远程会话 + Vivid/Quiet 双套

**PR**: [#118](https://github.com/attson/atterm/pull/118) · **Spec**: [2026-06-06-desktop-task-state-display-design.md](./superpowers/specs/2026-06-06-desktop-task-state-display-design.md)

### 2026-06-04 · session-type-classification

按 OSC 133 命令行给会话打 type ∈ {shell,ai,test,build,deploy},tab/list 显示对应色 chip

**PR**: [#97](https://github.com/attson/atterm/pull/97) · **Spec**: [2026-06-04-session-type-classification-design.md](./superpowers/specs/2026-06-04-session-type-classification-design.md)

### 2026-06-04 · session-summary

OSC 133 D 关闭时抓 ANSI-strip 尾输出 + 错误行入 SessionSummary,失败任务卡下渲染;补 Type 广播

**PR**: [#98](https://github.com/attson/atterm/pull/98) · **Spec**: [2026-06-04-session-summary-design.md](./superpowers/specs/2026-06-04-session-summary-design.md)

### 2026-06-04 · quick-templates

把移动端硬编码 QUICK_TEXTS 换为跨平台可编辑 QuickTemplate 列;每按钮预览后 text+\r 送出

**PR**: [#99](https://github.com/attson/atterm/pull/99) · **Spec**: [2026-06-04-quick-templates-design.md](./superpowers/specs/2026-06-04-quick-templates-design.md)

### 2026-06-01 · mobile-secure-storage

mobile relay 凭据从 localStorage 迁到 iOS Keychain(自建 Capacitor 插件),首读透明迁移

**PR**: [#88](https://github.com/attson/atterm/pull/88) · **Spec**: [2026-06-01-mobile-secure-storage-design.md](./superpowers/specs/2026-06-01-mobile-secure-storage-design.md)

### 2026-06-01 · desktop-diagnostics-export

Wails Settings 加 Diagnostics tab:app/OS/WebView 版本 + 脱敏 relay 信息 + 配置,可 Copy/Export

**PR**: [#89](https://github.com/attson/atterm/pull/89) · **Spec**: [2026-06-01-desktop-diagnostics-export-design.md](./superpowers/specs/2026-06-01-desktop-diagnostics-export-design.md)

### 2026-05-31 · relay-health-page

relay 自带诊断页,一眼确认版本/HTTPS/origins/bootstrap-admin/限流/uplinks,可复制纯文本

**PR**: [#87](https://github.com/attson/atterm/pull/87) · **Spec**: [2026-05-31-relay-health-page-design.md](./superpowers/specs/2026-05-31-relay-health-page-design.md)

### 2026-05-31 · pairing-qr

桌面登录用户生成 5min 一次性 QR,给 mobile 装机塞 relay URL + 专用 token,替代手输

**PR**: [#86](https://github.com/attson/atterm/pull/86) · **Spec**: [2026-05-31-pairing-qr-design.md](./superpowers/specs/2026-05-31-pairing-qr-design.md)

### 2026-05-30 · terminal-right-click-send

桌面终端右键菜单加「发送」,把 xterm 选中内容加 \r 送当前面板 PTY 立即执行

**PR**: [#85](https://github.com/attson/atterm/pull/85) · **Spec**: [2026-05-30-terminal-right-click-send-design.md](./superpowers/specs/2026-05-30-terminal-right-click-send-design.md)

### 2026-05-26 · i18n-english-chinese

桌面 Wails + Capacitor + Web 三端 UI 全套 en/zh-CN i18n,默认跟系统,per-client 持久

**PR**: [#81](https://github.com/attson/atterm/pull/81) · **Spec**: [2026-05-26-i18n-english-chinese-design.md](./superpowers/specs/2026-05-26-i18n-english-chinese-design.md)

### 2026-05-24 · remote-viewer-count

桌面 owner 每会话显 👁 N 徽章,数远端接入数;经 relay 镜像订阅数下发 uplink

**PR**: [#77](https://github.com/attson/atterm/pull/77) · **Spec**: [2026-05-24-remote-viewer-count-design.md](./superpowers/specs/2026-05-24-remote-viewer-count-design.md)

### 2026-05-24 · relay-webhook-notifications

命令完成时 relay 向 session owner 的 webhook(飞书自定义机器人/通用 JSON)POST,与 Web Push 并行

**PR**: [#73](https://github.com/attson/atterm/pull/73) · **Spec**: [2026-05-24-relay-webhook-notifications-design.md](./superpowers/specs/2026-05-24-relay-webhook-notifications-design.md)

### 2026-05-24 · driver-viewer-mirror-reconciliation

relay mirror session 反映 upstream PTY driver,远端 desktop/web/mobile 正确渲染 viewer 覆盖

**PR**: [#76](https://github.com/attson/atterm/pull/76) · **Spec**: [2026-05-24-driver-viewer-mirror-reconciliation-design.md](./superpowers/specs/2026-05-24-driver-viewer-mirror-reconciliation-design.md)

### 2026-05-23 · mobile-pr-c

MobilePlaceholder 换为真实 mobile attach-only 客户端(setup → host 分组列表 → 精简 keepalive 终端)

**PR**: [#72](https://github.com/attson/atterm/pull/72) · **Spec**: [2026-05-23-mobile-pr-c-design.md](./superpowers/specs/2026-05-23-mobile-pr-c-design.md)

### 2026-05-23 · desktop-frontend-platform-pr-b

加 platform/capacitor.ts + 多目标 Vite 构建 + mobile 入口,iOS 模拟器可跑;桌面不变

**PR**: [#71](https://github.com/attson/atterm/pull/71) · **Spec**: [2026-05-23-desktop-frontend-mobile-shell-design.md](./superpowers/specs/2026-05-23-desktop-frontend-mobile-shell-design.md)

### 2026-05-23 · desktop-frontend-platform-pr-a

引入 platform/ 层与 Platform 接口 + Wails 实现,22 处 wailsjs 直接 import 全部走 usePlatform()

**PR**: [#70](https://github.com/attson/atterm/pull/70) · **Spec**: [2026-05-23-desktop-frontend-mobile-shell-design.md](./superpowers/specs/2026-05-23-desktop-frontend-mobile-shell-design.md)

### 2026-05-22 · shortcut-hints-overlay

长按 Cmd (mac) / Ctrl (其它) 3s 弹「hold-Mod peek」快捷键覆盖,读 Settings 同一注册表

**PR**: [#67](https://github.com/attson/atterm/pull/67) · **Spec**: [2026-05-22-shortcut-hints-overlay-design.md](./superpowers/specs/2026-05-22-shortcut-hints-overlay-design.md)

### 2026-05-22 · mobile-relay-base-url

Capacitor iOS 支持连任意 relay(默认 https/wss;显式 opt-in http/ws),粘贴 API token 鉴权

**PR**: [#66](https://github.com/attson/atterm/pull/66) · **Spec**: [2026-05-22-mobile-relay-base-url-design.md](./superpowers/specs/2026-05-22-mobile-relay-base-url-design.md)

### 2026-05-22 · desktop-shortcut-settings

Settings 加 Shortcuts tab,可查看/改绑/禁用/重置 useTerminalShortcuts 的 12 个终端快捷键

**PR**: [#67](https://github.com/attson/atterm/pull/67) · **Spec**: [2026-05-22-desktop-shortcut-settings-design.md](./superpowers/specs/2026-05-22-desktop-shortcut-settings-design.md)

### 2026-05-20 · unified-titlebar

把桌面顶栏合进 OS 标题栏(mac/win/linux),同排装会话数/uplink/远端/设置 + win+linux 自绘窗口按钮

**PR**: [#64](https://github.com/attson/atterm/pull/64) · **Spec**: [2026-05-20-unified-titlebar-design.md](./superpowers/specs/2026-05-20-unified-titlebar-design.md)

### 2026-05-20 · titlebar-dblclick-maximize

去掉 TitleBar onTitleDblClick 的 macOS 专属守卫,三端都用同一路径切最大化

**PR**: [#65](https://github.com/attson/atterm/pull/65) · **Spec**: [2026-05-20-titlebar-dblclick-maximize-design.md](./superpowers/specs/2026-05-20-titlebar-dblclick-maximize-design.md)

### 2026-05-19 · translate-plugin

桌面加插件:右键选文本经 OpenAI 兼容 API 翻译,结果显在 teleport 到 body 的浮层

**PR**: [#56](https://github.com/attson/atterm/pull/56) · **Spec**: [2026-05-19-translate-plugin-design.md](./superpowers/specs/2026-05-19-translate-plugin-design.md)

### 2026-05-18 · web-vue-rewrite-pr-f-pwa

收尾 web Vue 重写:重启 PWA + push flow 迁 TS,legacy/ 全删,加 pwa-cache-scope 契约测试

**PR**: [#51](https://github.com/attson/atterm/pull/51) · **Spec**: [2026-05-17-web-vue-typescript-rewrite-design.md](./superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md)

### 2026-05-17 · web-vue-rewrite-pr-e-terminal

把老 legacy/index.html(会话列表 + xterm) 换为 Vue 3 + Naive UI,TS 重实现 internal/proto

**PR**: [#50](https://github.com/attson/atterm/pull/50) · **Spec**: [2026-05-17-web-vue-typescript-rewrite-design.md](./superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md)

### 2026-05-17 · web-vue-rewrite-pr-d-admin

/admin/index.html 换为 Vue 3 + Naive UI,同路径挂载,继承 /admin/ cookie-redirect gate

**PR**: [#49](https://github.com/attson/atterm/pull/49) · **Spec**: [2026-05-17-web-vue-typescript-rewrite-design.md](./superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md)

### 2026-05-17 · web-vue-rewrite-pr-c-settings

/settings.html(4 tab) 换为 Vue 3 + Naive UI,relay embedded FS 服

**PR**: [#47](https://github.com/attson/atterm/pull/47) · **Spec**: [2026-05-17-web-vue-typescript-rewrite-design.md](./superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md)

### 2026-05-17 · web-vue-rewrite-pr-b-login-signup

/login.html + /signup.html 换 Vue 3 + Naive UI,搭配全套 apiFetch + CSRF-cache + auth helpers

**PR**: [#46](https://github.com/attson/atterm/pull/46) · **Spec**: [2026-05-17-web-vue-typescript-rewrite-design.md](./superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md)

### 2026-05-17 · web-vue-rewrite-pr-a-scaffold

vanilla web 挪到 web/legacy/;新 web/ 用 Vite + Vue 3 + TS + Naive UI 脚手架,go:embed 给 relay

**PR**: [#45](https://github.com/attson/atterm/pull/45) · **Spec**: [2026-05-17-web-vue-typescript-rewrite-design.md](./superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md)

### 2026-05-17 · web-ui-redesign-pr-e-design-tokens

设计 token 系统落地,web/style.css 与内联样式的裸色值全部换成 token,login/signup 打磨

**PR**: [#43](https://github.com/attson/atterm/pull/43) · **Spec**: [2026-05-17-web-ui-redesign-design.md](./superpowers/specs/2026-05-17-web-ui-redesign-design.md)

### 2026-05-17 · web-ui-redesign-pr-d-admin-ui

/admin/ 恢复为 web/admin/ 静态页,Users tab 加 promote/demote,gate 未登录/非管理员重定向

**PR**: [#42](https://github.com/attson/atterm/pull/42) · **Spec**: [2026-05-17-web-ui-redesign-design.md](./superpowers/specs/2026-05-17-web-ui-redesign-design.md)

### 2026-05-17 · web-ui-redesign-pr-c-settings

/settings.html 拆 4 tab(Tokens/Password/Devices/Danger),配套后端 me-sessions + 硬删账号 + 末管理员保护

**PR**: [#41](https://github.com/attson/atterm/pull/41) · **Spec**: [2026-05-17-web-ui-redesign-design.md](./superpowers/specs/2026-05-17-web-ui-redesign-design.md)

### 2026-05-17 · web-ui-redesign-pr-b-layout-shell

页面级 topnav 抽出到 web/layout.js 运行时注入;index/settings/admin 共壳,视觉同今日

**PR**: [#40](https://github.com/attson/atterm/pull/40) · **Spec**: [2026-05-17-web-ui-redesign-design.md](./superpowers/specs/2026-05-17-web-ui-redesign-design.md)

### 2026-05-17 · web-ui-redesign-pr-a-backend-admin

ATTERM_ADMIN_TOKEN 换成 users.is_admin 角色随普通登录用;引 BOOTSTRAP_ADMIN 环境变量首启建/提升

**PR**: [#38](https://github.com/attson/atterm/pull/38) · **Spec**: [2026-05-17-web-ui-redesign-design.md](./superpowers/specs/2026-05-17-web-ui-redesign-design.md)

### 2026-05-16 · saas-user-accounts

atterm-relay 从共享 token 切到 per-user 账号:邀请注册 + cookie 登录 + per-user API token + owner 过滤

**PR**: [#17](https://github.com/attson/atterm/pull/17) · **Spec**: [2026-05-15-saas-user-accounts-design.md](./superpowers/specs/2026-05-15-saas-user-accounts-design.md)

### 2026-05-16 · plugin-system

桌面插件框架 + 首两插件:Quick Input(底栏发文本按钮)+ File Explorer(右栏文件树 + CodeMirror 预览)

**PR**: [#18](https://github.com/attson/atterm/pull/18) · **Spec**: [2026-05-16-plugin-system-design.md](./superpowers/specs/2026-05-16-plugin-system-design.md)

### 2026-05-15 · web-push-notifications

把 OSC 133 命令完成事件经自建 Web Push 送到浏览器/PWA,页面没开也能通知

**PR**: [#13](https://github.com/attson/atterm/pull/13) · **Spec**: [2026-05-15-web-push-notifications-design.md](./superpowers/specs/2026-05-15-web-push-notifications-design.md)

### 2026-05-15 · remote-session-host-grouping

三处远程会话选择面(RemoteSessionsDialog / SessionPicker 远端段 / 网页网格)按 host_id 分组

**PR**: [#11](https://github.com/attson/atterm/pull/11) · **Spec**: [2026-05-15-remote-session-host-grouping-design.md](./superpowers/specs/2026-05-15-remote-session-host-grouping-design.md)

### 2026-05-15 · osc133-shell-integration

PTY spawn 时注入 OSC 133 shell 钩(zsh/bash/fish/pwsh),前端解析,窗口不聚焦且时长过阈值时通知

**PR**: [#10](https://github.com/attson/atterm/pull/10) · **Spec**: [2026-05-14-osc133-shell-integration-design.md](./superpowers/specs/2026-05-14-osc133-shell-integration-design.md)

### 2026-05-14 · settings-select-redesign

桌面 Settings 里两个 <select> 换成自绘 SelectDropdown.vue,触发器与弹层都吻合暗色主题

**Spec**: [2026-05-14-settings-select-redesign-design.md](./superpowers/specs/2026-05-14-settings-select-redesign-design.md)

### 2026-05-14 · settings-redesign

SettingsDialog(~600 LOC/四混杂)重构为左侧栏 + tab 布局 + 四子组件(General/Relay/Logging/Updates)

**Spec**: [2026-05-14-settings-redesign-design.md](./superpowers/specs/2026-05-14-settings-redesign-design.md)

### 2026-05-14 · driver-viewer-mode

引入单驱动运行时角色消除 PTY 尺寸跨端 corruption:一订阅者驱 PTY,余者锁 viewer,空格抢驱动

**PR**: [#2](https://github.com/attson/atterm/pull/2) · **Spec**: [2026-05-14-driver-viewer-mode-design.md](./superpowers/specs/2026-05-14-driver-viewer-mode-design.md)

### 2026-05-14 · bel-notifications

终端面板收到 \x07 且窗口未聚焦时发系统通知,每会话 3s 一次节流

**Spec**: [2026-05-14-bel-notifications-design.md](./superpowers/specs/2026-05-14-bel-notifications-design.md)

### 2026-05-13 · terminal-themes

桌面终端加内置主题,Settings 选,作为用户全局偏好持久化

**Spec**: [2026-05-13-terminal-themes-design.md](./superpowers/specs/2026-05-13-terminal-themes-design.md)

### 2026-05-13 · desktop-logging

桌面日志默认持久化 + 可配路径 + 按大小 rotate + 运行时开关 + 内嵌 log viewer

**Spec**: [2026-05-13-desktop-logging-design.md](./superpowers/specs/2026-05-13-desktop-logging-design.md)

### 2026-05-12 · replay-progress-throttle

接入大回滚会话时显示真实 history 加载进度,并对 replay 输出限速

**Spec**: [2026-05-12-replay-progress-throttle-design.md](./superpowers/specs/2026-05-12-replay-progress-throttle-design.md)

### 2026-05-12 · relay-owner-permissions-admin-config

桌面 owner 远程权限 + relay/host 侧强制;持久 relay admin 配置面,不落盘主 write token

**Spec**: [2026-05-12-relay-owner-permissions-admin-config-design.md](./superpowers/specs/2026-05-12-relay-owner-permissions-admin-config-design.md)

### 2026-05-11 · ios-pwa-support

让 relay 上的 web/ 客户端作为 iPhone Safari / 主屏 PWA,可用于列会话和接入

**Spec**: [2026-05-11-ios-pwa-support-design.md](./superpowers/specs/2026-05-11-ios-pwa-support-design.md)

### 2026-05-10 · pane-split-layouts

桌面加 per-tab pane splits(single / 左右 / 上下 / 2×2),iTerm 式快捷键;每面独立本地 PTY 或接管已有会话

**Spec**: [2026-05-10-pane-split-layouts-design.md](./superpowers/specs/2026-05-10-pane-split-layouts-design.md)

### 2026-05-10 · auto-update

桌面加人工触发的自动更新:查 GitHub Releases → 下载平台包 → 用户确认后替换 binary/bundle 并重启;dev 短路禁用

**Spec**: [2026-05-10-auto-update-design.md](./superpowers/specs/2026-05-10-auto-update-design.md)
