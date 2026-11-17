---
description: 按平台了解 AT Term 的通用、任务、Relay、Peer、插件、模板、更新、诊断和设备设置入口。
---

# 设置

点标签栏右侧齿轮打开设置。设置页按当前客户端能力显示入口，所以桌面、浏览器和 iOS 的列表并不完全相同。

## 按目标查找设置

| 目标 | 设置入口 |
|---|---|
| 终端主题、字体、shell、通知阈值、恢复和后台订阅 | **General** |
| 会话状态分组与任务显示 | **Tasks** |
| 桌面连接账号制服务、远程权限与直连偏好 | **Relay** |
| 加入或管理可信设备组 | **Peer** |
| 已登录设备、撤销设备会话 | **Devices** |
| 文件浏览器、桌面挂件等扩展 | **Plugins** |
| 应用快捷键 | **Keyboard shortcuts** |
| 终端底部快捷回复 | **Quick templates** |
| 检查并手动安装更新 | **Updates** |
| 日志、诊断包和配置导入导出 | **Diagnostics** |
| Claude Code 飞书远程交互 | **Feishu** |
| 远程接收的文件 | **Received files** |

## 平台差异

- **桌面端**：提供本地 PTY、Relay、插件、快捷键、更新、诊断、Feishu 和接收文件等完整设置；SSH hosts 从标签栏的专用面板管理。
- **Web / PWA**：提供账号、任务、设备、模板等适用于浏览器的设置；没有本机 PTY、桌面插件或桌面更新入口。
- **iOS**：在 Web 能力上增加扫码配对、Keychain 和移动辅助键；SSH hosts 页面只用于查看桌面同步的主机信息，不能在 iPhone 上直接建立本地 SSH 会话。
- **Peer**：只有当前平台提供 Peer bridge 时才显示。

## 配置与凭据

桌面端的 **Diagnostics → Import & export** 可以迁移偏好和 SSH 主机定义，但不会导出 Relay 密码、session token、SSH 密码或私钥。导入前会显示变更预览，确认后才应用；地址发生变化的已有 SSH 主机会清除旧凭据，避免把凭据发送到错误主机。

自动更新只在后台检查。安装与重启必须由你在 **Updates** 中明确触发并确认；更新包会先验签再校验 hash。
