# 远程接管与会话侧栏

## 远程接管(lazy 同步)

- 桌面连上 relay 后,其他浏览器 / 桌面端 / iOS app 可 attach 同一会话;**默认 viewer 模式,按空格 take over 才能写**。
- 远程没人看时不上传 PTY 字节;权限由桌面端的 `remote_permission` 字段定义(`view` / `control` / `full`),relay 和 host 双重强制。
- 远程接管伴随 viewer 数量徽章(👁 N)与连接健康指示(RTT pill + 抽屉详情)。

## 会话侧栏

- 右键会话行 → 「置顶」把重要会话抽到顶部虚拟 📌 组;集合按 `session_id` 存桌面本地 config,跨重启保留。
- Header 内联搜索框(`Cmd/Ctrl+F` 聚焦,`Esc` 清空)按 title / cwd / current_command 即时过滤;折叠组命中时自动展开,全无匹配时显示空态提示。
- 置顶跨重启迁移:本机会话重启后被 recovery 重新 spawn 拿新 sid 时,pin 自动从旧 sid 迁到新 sid,不再漂成孤儿。

> 首页的[交互 demo](/) 就是这套侧栏 + 终端的真实界面 —— 可以直接切换会话、搜索、置顶、在 idle 会话里敲命令。

## 任务状态闭环

- OSC 133 推导 running / waiting-input / completed / failed / disconnected 状态,三端同步。
- 命令完成触发系统通知、Web Push、出站 webhook(飞书 / generic JSON);payload 带 session id / 任务类型 / summary。
- AI 任务控制台:自动给 `codex` / `claude` / `gemini` / `aider` / `go test` / `docker build` / `kubectl` 等命令打 type chip;失败卡片附 error 行;可配快捷模板(`yes / ok / continue / commit / push / release / 1 / 2 / 3` 默认,hotkey 可绑)。

## 多通道通知

任务关键节点(命令完成、AI 等输入)会触发:

- **系统通知**:桌面原生通知,点击直接路由到对应 session(通知带 `session_id` payload)。
- **Web Push**:浏览器 / PWA 订阅后收命令完成推送(iOS 有平台限制,详见项目 `docs/features/web-push.md`)。
- **飞书卡片**:锚点卡片承载 AI 会话,支持远程回答(见 [AI Agent 与 Feishu](/guide/ai-agents))。
- **出站 webhook**:generic JSON,payload 带 session id / 任务类型 / summary。

## 会话恢复

桌面端把 tab / pane 结构 + 已捕获的 AI sid 持续写入 `~/.config/atterm/recovery.json`。下次启动弹恢复对话框,可挑选恢复哪些 tab:

- **本机 shell** — 用原 cwd 重 spawn 一个新 PTY。
- **远端 pane** — 用旧 `session_id` 直接 rebind 到 relay;relay 上 session 还活就接回去,挂了就显示 `disconnected` 占位、保留标题。
- **AI 会话** — Go 端在恢复 shell 第一次 prompt 时直接写 `claude --resume <sid>` / `codex resume <sid>`,不用手敲。

对话框可在 Settings → General 关闭。

## 移动端 / Web / PWA

- Web 端 Vue 3 + TS + Naive UI;主入口复用桌面 `App.vue`(tabs / panes / 侧栏 / Settings / Admin 内嵌),中英双语。
- Web 终端支持浏览器端辅助键条(Enter / Esc / Tab / `Ctrl-C` / `Ctrl-D` / 方向键)以及显式选择图片 / 文件粘贴进远端 PTY(需要当前连接是 driver 且 session `remote_permission=full`)。
- iOS Capacitor 壳:QR 扫码配对(5min 一次性 token)/ 手动登录 / Keychain 凭据持久化 / 防误触模式 / 中文输入法补获 / 隐藏键盘辅助条 / viewer 锁 PTY 尺寸。
