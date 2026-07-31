# 端到端加密与安全

## 启用端到端加密

新账号在注册时(`signup.html` / 桌面 Settings → Remote relay → Register)走 OPAQUE 流程,浏览器 / 桌面端在本地随机生成 32 字节 `account_key`,用 Argon2id 派生的 wrap key + XChaCha20-Poly1305 封装成 wrap blob 上传,relay 只看到 wrap 不看密码。登录时同样在本地 OPAQUE 后用密码解 wrap 拿回 `account_key`,存 sessionStorage / Keychain / Keyring。

只要 `account_key` 解锁,agent 就会自动开 E2EE:

- 终端 OUT 字节、会话标题 / cwd / 当前命令、任务摘要、命令完成 push body 都封装上链,relay 收到的就是密文。
- 浏览器 / 桌面 / 移动端在本地解密;service worker 命令完成通知会通过 `MessageChannel` 找到可见 tab 解密,渲染出富文本(无可见 tab 时退化到通用文案)。
- Webhook 接收端(飞书 / generic JSON)此时只看到 `Session command finished` + 一段 base64 `sealed_body`;要解开就拿 `account_key` 自己 AEAD-open。

## 加密细节

- OPAQUE aPAKE 注册 / 登录 / step-up(P-256-SHA256 + Scrypt);服务器从不接触明文密码。
- 32 字节 `account_key` 随机生成、用 Argon2id 派生的 wrap key + XChaCha20-Poly1305 封装存 relay;客户端用密码当场解开后存 sessionStorage / Keychain / Keyring。
- 终端输出 / 标题 / cwd / 当前命令 / 任务摘要 / 命令完成 push body 在 agent 端用 HKDF-SHA256(account_key) 派生的 session key 封装;relay 转发不解开,只看 routing 必需的 session id / 时间戳。AAD 由 `uuid || frame_type` 鉴别帧类型,防止 cross-type 替换重放。

> 特殊门控:硬删除账号 `DELETE /api/me` 走 step-up(再走一次 OPAQUE login 换 60s 一次性 token)。**没有密码找回**——忘记密码只能 admin reset,相当于换一把新 `account_key`,旧会话的 sealed 内容永久不可解;这是单用户自托管定位下的设计选择。

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

完整鉴权模型(含 bootstrap、pairing、错误码)见项目 [docs/spec/auth.md](https://github.com/attson/atterm/blob/main/docs/spec/auth.md)。
