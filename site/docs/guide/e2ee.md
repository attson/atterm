---
description: 了解 AT Term 的 OPAQUE 登录、account_key 管理、端到端加密信封、远程权限与自托管 Relay 安全边界。
---

# 端到端加密与安全

## 账号与 account key

新账号在 `signup.html` 或客户端注册入口使用 OPAQUE。密码只参与客户端与 Relay 的 OPAQUE 交互，Relay 不接收或保存明文密码。注册时客户端生成独立的 32 字节 `account_key`，上传的只是密码保护后的包裹结果；登录成功后由客户端在本地解锁。

解锁后的 key 只保存在当前客户端的安全边界内：Web 使用 tab 级 `sessionStorage`，桌面使用系统 keyring，iOS 使用 Keychain。它不进入 URL、日志、IndexedDB 或 Relay 明文存储。

只要 `account_key` 解锁，host 就会自动启用 E2EE：

- 终端 OUT 字节、会话标题 / cwd / 当前命令、任务摘要、命令完成 push body 都封装上链,relay 收到的就是密文。
- 浏览器、桌面和移动端都在本地解密；如果浏览器没有可用的已解锁页面，后台通知只显示不含敏感内容的通用文案。

## Relay 能看到什么

Relay 需要看到 session id、时间戳、连接和路由元数据，才能把密文送到正确客户端；它看不到被 sealed 的终端输出、标题、cwd、当前命令、任务摘要或命令完成通知正文。所有支持的客户端使用同一套 OPAQUE 实现与账户密钥格式，登录后的内容在本地解密。

没有可绕过 E2EE 的密码找回。忘记密码后，管理员重置会生成新的 `account_key`，旧 sealed 内容永久不可解。删除账号等高风险操作还会要求重新进行一次 OPAQUE step-up，并使用短时单次 token。

## 选择远程权限

桌面端 Settings 可以为本机 session 设置远程权限:

| 权限 | 远程用户可以做什么 |
|------|--------------------|
| `view` | 只能查看输出和历史 |
| `control` | 可以输入和 resize |
| `full` | `control` + 粘贴图片 / 选择文件 / 远程文件浏览器 |

权限由桌面端 `remote_permission` 设置决定;relay 和 desktop host 都强制执行。`view` 用户即使有完整账号,也只能查看输出,不能输入 / resize / 粘贴图片或文件;`control` 可以输入但仍不能触发文件类动作。

## 安全模型

AT Term 的默认策略是 fail-closed:

- 公网 relay 必须提供 `ATTERM_BOOTSTRAP_ADMIN_EMAIL`(缺失则拒绝启动,除非 `--dev-insecure`);admin 通过启动日志里的一次性 claim token 完成 OPAQUE 注册获得。密码走 OPAQUE,relay 永不接收明文密码。
- 公网 relay 必须使用明确的 `ATTERM_ORIGINS`。
- 服务端鉴权不接受 `?token=` 参数;session token 通过 `Authorization: Bearer` 或浏览器 / 桌面 WebSocket 的 `Sec-WebSocket-Protocol` 传递,避免写进 URL。
- session token(`ses_…`)以 sha256 哈希存储,明文只在登录 / 配对响应里返回给客户端一次,由客户端自行持久化。
- web 客户端只加载同源静态资源,不依赖 CDN。
- relay 默认启用 CSP、Referrer-Policy、nosniff、Permissions-Policy 等安全头。
- relay 按远端 IP 和认证后的 token hash 做限流与连接数限制。
- 桌面端默认拒绝非 loopback 的明文 `ws://` relay;可信内网需要在 Settings 手动开启 insecure mode。
- 自动更新必须通过 Ed25519 签名和 SHA256 校验;缺公钥、缺签名、签名不匹配或 hash 不匹配都会失败。

部署侧的 TLS、bootstrap 和 Origin 要求见 [部署 Relay](/guide/deploy-relay)，管理员操作见 [管理后台](/guide/admin)。
