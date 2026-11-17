---
description: 使用 AT Term Relay 管理后台创建邀请、管理用户与角色、调整运行时配置、配置飞书、查看流量和检查健康状态。
---

# 管理后台

用 bootstrap claim token 注册的第一个管理员登录后，主界面会显示 **Admin**。管理后台与普通终端界面共用登录态，但只有 admin 角色能读取或修改管理数据。

## 邀请

在 **Invitations** 中创建一次性邀请码并立即保存：完整 token 只在创建响应中显示，之后列表只保留状态与元数据。把邀请码单独发给目标用户，不要放进公开 issue、日志或截图。

## 用户与角色

**Users** 可以筛选账号、禁用/恢复用户以及提升或撤销 admin。系统不允许撤销最后一个管理员，先提升另一名用户再操作。

密码重置会让用户失去旧 `account_key`，因此旧的 E2EE sealed 内容不可恢复。先让用户确认这个结果，再执行重置；AT Term 不提供能绕过该边界的密码找回链接。

## Config

**Config** 管理以下运行时设置：

- 浏览器 Origin 精确 allowlist；iOS Capacitor 客户端需要 `capacitor://localhost`。
- 请求速率和每个 IP + token 的 WebSocket 连接上限。
- 按 scope 开启的 DEBUG 日志和自动关闭时间。Payload scope 可能包含终端输入输出，必须单独显式开启，排障结束立即关闭。

Origin 与 debug 保存后热生效。VAPID subject 以及进程级 rate / connection limit 的环境变量需要重启对应实例。多实例共享数据库时，DB-backed 配置由各实例轮询刷新；进程级设置仍逐实例管理。

## Feishu

**Feishu** 用于启用 Relay 托管的飞书桥、设置 Open Platform base URL，并生成或录入 32 字节 base64 主密钥。页面只回显密钥末四位，完整密钥不会被 GET 返回或写入日志。

轮换主密钥会让已有飞书绑定无法解密，因此默认拒绝覆盖；只有确认重新绑定的影响后才使用 force rotation。启用、禁用和普通配置保存后热生效。

## Traffic 与健康检查

**Traffic** 按用户查看连接与流量构成，用于定位异常使用量，不展示 E2EE 终端明文。

运维探针使用：

- `/healthz`：基础存活检查。
- `/admin/health`：给管理员查看的健康页。
- `/admin/api/health`：带管理员鉴权的结构化健康数据。

部署完成后至少验证：普通用户看不到 Admin、管理员能读取配置、非法 Origin 被拒绝、合法 Web/桌面客户端仍能登录和 attach。部署和多实例步骤见 [部署 Relay](/guide/deploy-relay)，密钥边界见 [端到端加密与安全](/guide/e2ee)。
