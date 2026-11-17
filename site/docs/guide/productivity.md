---
description: 使用 AT Term 的输入历史、快捷模板、Snippet 批量执行和可配置快捷键减少重复终端操作。
---

# 效率工具与快捷键

## 重新使用输入历史

每个会话保留自己的已提交输入。点 pane 右上角的历史按钮，或按 `Mod+Shift+H` 打开面板；可以查看、复制并重新发送最近输入，不必在长 scrollback 里寻找命令。

![AT Term 输入历史面板](/manual/input-history.png)

输入历史按 session 隔离，并以有界快照持久化；恢复本地 shell 或重新连接已保存的 SSH 会话时，会承接上一代 session 的记录。远程会话的历史由拥有该会话的 host 提供。

## 快捷模板

终端底部模板栏用于发送常用短回复或命令。默认包含 `yes`、`ok`、`continue`、`commit`、`push`、`release` 和数字选项；点击后直接发往当前可输入的终端。

在 **Settings → Quick templates** 中可以：

- 新增、编辑、排序或隐藏模板。
- 为模板绑定 hotkey。
- 隐藏整个模板栏。

模板不会绕过 viewer/driver 或远程权限检查。内容会立即发送，长命令和破坏性命令更适合保存为 Snippet 并在运行前检查目标。

## Snippet 与批量执行

把多行脚本或经常重复的诊断命令保存为 Snippet。终端底部的 **Snippets** 菜单可以把它发送到当前会话，也可以选择多个已保存 SSH 主机批量执行并查看每台主机的进度和结果。

批量运行前先确认主机集合和脚本正文。Termius 导入可以一并迁移独立 Snippet，但不会把主机的启动脚本自动变成可手动执行的 Snippet。

## 默认快捷键

| 操作 | 默认快捷键 |
|---|---|
| 新建标签页 | `Mod+T` |
| 上一个 / 下一个标签页 | `Mod+Shift+[` / `Mod+Shift+]` |
| 垂直 / 水平分屏 | `Mod+N` / `Mod+Shift+N` |
| 关闭当前 pane | `Mod+W` |
| 聚焦相邻 pane | `Mod+Alt+方向键` |
| 搜索终端输出 | `Mod+F` |
| 打开输入历史 | `Mod+Shift+H` |
| 折叠会话侧栏 | `Mod+B` |
| 聚焦侧栏搜索 | `Mod+Shift+F` |
| 增大 / 减小字号 | `Mod+Shift+=` / `Mod+-` |
| 切换最大化 | `Mod+Enter` |

桌面端可在 **Settings → Keyboard shortcuts** 重绑或禁用快捷键。若自定义键位与 shell、浏览器或操作系统冲突，以最先捕获该组合的一方为准。
