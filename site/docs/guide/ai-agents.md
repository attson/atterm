---
description: 在 AT Term 中跟踪和恢复 Claude Code、Codex、Aider、Gemini CLI 会话，并通过飞书卡片远程处理 AskUserQuestion。
---

# AI Agent 与 Feishu

## 启动与识别

在 AT Term 的 shell 中照常运行 `claude`、`codex`、`aider` 或 `gemini`。会话侧栏会显示对应类型和运行状态；命令结束、失败或等待输入时，可以通过桌面通知、Web Push 或 Feishu 回到正确的 session。

| Agent | 自动识别 | 重启后恢复原对话 | Feishu AskUserQuestion |
|---|---:|---|---:|
| Claude Code | 是 | 已捕获会话 id 时使用 `claude --resume` | 是 |
| Codex | 是 | 已捕获会话 id 时使用 `codex resume` | 否 |
| Aider | 是 | 重放完整启动命令，不保证恢复聊天历史 | 否 |
| Gemini CLI | 是 | 暂不支持 | 否 |

恢复遵循“宁可不恢复，也不恢复错对话”：AT Term 只有在能确认会话 id 时才注入 resume；否则在原 cwd 打开普通 shell。启动命令中的其他参数会保留。

## 从侧栏跟踪 AI 任务

- 置顶长任务，或按类型、标题、cwd 和当前命令搜索。
- `waiting input` 表示需要你处理问题或权限；`completed` / `failed` 表示命令已结束。
- 用快捷模板发送 `yes`、`continue` 或数字选项前，先确认当前客户端已经取得 driver 且会话允许控制。
- 桌面挂件可以只显示 AI 会话，也可以和构建、测试等普通任务一起显示。

## 配置 Feishu

Feishu 远程回答目前只覆盖 Claude Code：

1. 在桌面端打开 **Settings → Feishu**，选择本机直连或 Relay 模式并完成配置。
2. 在 AT Term 会话中启动 Claude Code。锚点卡会持续更新输出，并提供输入、`Ctrl-C`、`Ctrl-D`、Esc、Enter 与结束操作。
3. Claude Code 调用 AskUserQuestion 时，卡片切换为单选、多选和自定义输入表单；提交后恢复普通控制栏。

首次在本机遇到 AskUserQuestion 权限提示时，选择一次 **“Yes, and don't ask again”**。否则权限对话框会先吃掉远程提交的按键，表单答案无法送达问题。

本机模式的凭据保存在系统凭据存储；Relay 模式由管理员配置并加密保存。不要在终端输出、截图或故障报告中粘贴 App Secret。
