# AI Agent 与 Feishu

## AI agent 支持

会话内启动的 AI CLI 会被自动归类并显示对应 type chip:

| Agent | type chip | resume 注入 | session jsonl 嗅探 | Notification hook | 飞书 AskUserQuestion 远程回答 |
|---|---|---|---|---|---|
| **Claude Code** (`claude`) | ✅ | `claude --resume <sid>` | `~/.claude/projects/<cwd>/<sid>.jsonl` | ✅ 自动安装 + 健康巡检 | ✅ 锚点卡上表单选择,提交后自动按键送 pty |
| **Codex** (`codex`) | ✅ | `codex resume <sid>` | `~/.codex/sessions/YYYY/MM/DD/rollout-*-<sid>.jsonl` | ⏳ 自动安装路径未做 | — |
| **Aider** (`aider`) | ✅ | 直接重放上一条 command line | —(无稳定 jsonl 协议) | — | — |
| **Gemini CLI** (`gemini`) | ✅ | —(暂无) | — | — | — |
| 其他(`go test` / `docker build` / `kubectl` …) | ✅ 命令分类 | — | — | — | — |

权限审批 / AskUserQuestion 等待这两条信号目前仅 Claude Code 走 Notification hook 路径;Codex 走 jsonl 监听是后续规划。飞书 AskUserQuestion 表单式远程回答仅覆盖 Claude Code。

AI sid 抓取在 session 启动时自动进行(OSC 133 D 事件触发分类 + claude/codex 的 session jsonl 文件 mtime 跟踪),抓不到就退化为普通 shell 恢复、不注入 resume ——「抓不到」优于「抓错的对话」。

## Feishu — 远程终端 + AskUserQuestion 远程回答

`claude-code` 在 atterm session 内运行时,其 PreToolUse / Notification hook 触发 `atterm-hook` CLI(`ATTERM_SESSION_ID` 和 `ATTERM_HOOK_ENDPOINT` 环境变量已自动注入每个 PTY)。CLI 把 hook payload POST 到桌面进程,桌面进程按以下路径向飞书 DM 推送和接收:

- **anchor card(锚点卡片)**:一张持续更新的 IM 卡,body 流式接收 assistant 输出,输入框和 `^C / ^D / Esc / Enter / 结束` 按钮把用户操作直接送回本地 pty。
- **AskUserQuestion form**:`claude-code` 触发 AskUserQuestion 时,锚点卡片上生成对应表单(每题一行 select_static 下拉 + 自定义输入框,支持单选和多选)。用户在飞书上填完提交,桌面进程按反向工程的 TUI 按键模型送回本地 pty,claude 收到答案后 form 消失、锚点卡片恢复到普通输入 + 按钮态。
- **前置要求**:本机 `claude-code` 首次遇到 AskUserQuestion 权限对话框时,选 "Yes, and don't ask again" 一次;否则每次都要在飞书回答前先在本地授权。
- **local vs relay**:`internal/feishu` 支持两种部署 —— local 走本机 LongConn 直连飞书(credentials 存钥匙串),relay 走中央 relay 服务(credentials 存 relay 的 `users.db` + `AdminConfig.Feishu`)。桌面 Settings → Feishu 里可切换。

完整设计与按键模型见项目 [docs/spec/feishu.md](https://github.com/attson/atterm/blob/main/docs/spec/feishu.md)。

## 让同事查看会话

如需让同事 attach 查看,通过 admin 后台为其创建一个账号邀请(`inv_…`),对方注册后即可用自己的账号登录 relay 查看会话。relay 级别的共享只读 token 已在用户账号版本中移除;权限控制现在通过桌面端的 `remote_permission` 字段实现(见 [端到端加密与安全](/guide/e2ee))。
