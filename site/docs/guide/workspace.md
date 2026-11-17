---
description: 使用 AT Term 的标签页、分屏、会话侧栏、任务状态、终端搜索和恢复功能组织本地与远程终端工作。
---

# 工作区与会话

AT Term 把一个任务现场组织成 **标签页、pane 和会话**：标签页用于切换工作主题，一个标签页可以放多个 pane，每个 pane 连接一个本地或远程会话。关闭 pane 只会按当前会话类型执行关闭或 detach；关闭仍在运行的本机会话前，界面会提示风险。

![AT Term 工作区中的会话侧栏、标签页与终端](/manual/workspace.png)

## 标签页与分屏

1. 点标签栏的 `+`，或按 `Mod+T` 新建本地 shell 标签页。
2. 在当前 pane 按 `Mod+N` 垂直分屏，按 `Mod+Shift+N` 水平分屏。新 shell 会尽量继承当前本地 pane 的工作目录。
3. 按 `Mod+Alt+方向键` 在 pane 之间移动焦点；按 `Mod+W` 关闭当前 pane。
4. 把会话侧栏中的会话拖到 pane，可在当前位置打开或替换会话；拖动 pane 的手柄可以重新排布已有会话。

`Mod` 在 macOS 上是 Command，在 Windows/Linux 上是 Ctrl。默认快捷键可以在 **Settings → Keyboard shortcuts** 修改。

## 用会话侧栏找到任务

侧栏把本机与远端会话按主机或状态分组，并显示当前命令、工作目录、任务状态和未读变化。

- 点会话行：如果它已经在某个 pane 中，直接聚焦该 pane；否则在新标签页打开。
- 右键选择“置顶”：会话进入顶部置顶组，并在重启恢复后迁移到新会话 id。
- 按 `Mod+Shift+F`：聚焦侧栏搜索，按标题、cwd 或当前命令过滤；`Esc` 清空。
- 按 `Mod+B`：折叠或展开侧栏。

任务状态来自 shell integration：运行中、等待输入、已完成、失败和断开会在桌面、Web 与移动端保持一致。状态只说明当前任务阶段，不会暂停本机进程。

## 搜索和调整终端

- `Mod+F` 搜索当前终端的 scrollback；它不会搜索侧栏。
- `Mod+Shift+=` / `Mod+-` 调大或调小终端字号。
- `Mod+Enter` 切换窗口最大化。
- 每个 pane 右上角可以选择终端主题；全局外观在 **Settings → General** 设置。

## 恢复未完成的现场

桌面端会持续保存 tab、pane、cwd 和可确认的 AI 会话标识。非正常退出或重新启动后，在恢复对话框中选择要恢复的标签页：

- 本地 shell 在原 cwd 创建新 PTY。
- 仍存活的远端会话按 session id 重新 attach；不可用时保留断开占位，避免误连到别的任务。
- 已识别的 Claude Code 或 Codex 会话会在第一次 shell prompt 后恢复原对话；无法可靠识别时只恢复普通 shell。

下一步可配置 [SSH 与文件](/guide/ssh-files)，或用 [效率工具与快捷键](/guide/productivity) 减少重复输入。
