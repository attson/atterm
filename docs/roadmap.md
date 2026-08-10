# 路线图

## 状态速览 (2026-08-11)

- **v0.4.3** 已发布(2026-08-04)。桌面 dmg / Linux deb+tar / Windows exe+zip 全平台构件已 upload,SHA256SUMS + GPG 签名附。
- **桌面挂件**(Desk Widget)已落地:置顶悬浮小窗显示所有会话的运行 / 失败 / 等待输入状态,点行跳转对应 tab。走插件模式(Settings → 插件),可选"仅 AI 会话"。设计与踩坑记录见 [`docs/superpowers/specs/2026-08-10-desk-widget-design.md`](./superpowers/specs/2026-08-10-desk-widget-design.md),不变量见 AGENTS.md 红线 #37。
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

## P2：v0.5 AI 任务控制台

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

## P3：v0.6 协作能力

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

## P4：v0.7+ 历史与回放

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

## Backlog

- [ ] 字体选择
- [ ] 字号和行高设置
- [ ] 主题导入导出
- [ ] 默认 shell 设置改进
- [ ] 启动目录设置
- [ ] 环境变量设置
- [ ] 完整快捷键自定义
- [ ] Webhook 规则自定义
- [ ] 插件工作流动作
- [ ] Linux rpm package
- [ ] Linux AppImage package

## 鉴权后续

来自 [docs/spec/auth.md](./spec/auth.md) 拆分时识别的迭代项：

- [ ] session refresh / proactive renewal（30 天 TTL 滑动续期或显式 refresh endpoint）
- [ ] 移动端 expires_at 触发预刷新（目前 `RelayConfig.session_expires_at` 已存但未使用）
- [ ] `/login.html?next=...` redirect 的 CSRF / open-redirect audit
- [ ] 桌面 App `localAdminPassword` 改用 OS Keychain 而非明文 `config.json`
