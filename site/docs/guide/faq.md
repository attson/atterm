# FAQ / 故障排查

## 启动子进程报 `command not found`(pnpm / go / nvm)?

AT Term 以登录交互式 shell 启动会话,拿到完整 PATH。如果仍找不到命令,检查该命令是否在你的 shell 配置文件(`.zshrc` / `.bash_profile`)里配置了 PATH,而不是只在非交互 rc 里。

## 浏览器打不开 relay 登录页 / OPAQUE 登录失败?

OPAQUE 登录用浏览器 WebCrypto,只在**安全上下文**(HTTPS 或 `localhost`)可用。公网 IP 上的明文 HTTP 无法登录。解决:

- 前置反代(Caddy/nginx/Cloudflare/Tailscale)终止 TLS,反代到 relay 的 `:8080`。
- 或 relay 直接跑 HTTPS(`ATTERM_TLS_CERT` / `ATTERM_TLS_KEY` + `--https-addr :8443`)。
- 仅本机临时用:`ssh -L 8080:127.0.0.1:8080 <host>` 后开 `http://localhost:8080`(loopback 是安全上下文)。

详见 [部署 Relay](/guide/deploy-relay)。

## relay 启动就退出 / 拒绝启动?

公网监听默认 fail-closed:必须提供 `ATTERM_BOOTSTRAP_ADMIN_EMAIL` 和 `ATTERM_ORIGINS`,缺失则拒绝启动。本地开发可加 `--dev-insecure` 跳过这些校验(不要在公网生产用)。

## 没看到 claim token?

只有设置了 `ATTERM_BOOTSTRAP_ADMIN_EMAIL` 时,relay 才会在每次启动打印一次性 claim token(`docker compose logs atterm-relay` 里找 `bootstrap-admin: claim token …`)。token 7 天有效、单次使用,只能用于**注册新账号**;若该邮箱已注册,改用 SQL 直接提权:`UPDATE users SET is_admin=1 WHERE email='you@…';`。

## 桌面端连不上 relay?

- 确认 relay URL 用的是 HTTPS/WSS 域名(桌面端默认拒绝非 loopback 的明文 `ws://`)。
- 可信内网用明文时,在 Settings 手动开启 insecure mode。
- 检查 relay 的 `ATTERM_ORIGINS` 是否包含你的访问来源。

## 远程能看到会话但不能输入?

远程 attach 默认是 **viewer** 模式,按空格 take over 才能写。此外要看桌面端为该 session 设的 `remote_permission`:`view` 只读、`control` 可输入、`full` 才能粘贴图片 / 文件 / 用远程文件浏览器。见 [端到端加密与安全](/guide/e2ee) 的权限表。

## 命令完成通知没弹出?

- 系统通知:确认浏览器 / 系统授予了通知权限。
- Web Push:需要先在 web / PWA 端订阅;iOS 有平台限制(详见项目 `docs/features/web-push.md`)。
- 飞书:确认 `internal/feishu` 已在桌面 Settings → Feishu 配置并绑定。

## macOS 提示「无法打开,因为无法验证开发者」?

目前的 macOS 构件**未签名 / 未公证**(roadmap 第 8 项,阻塞于 Apple Developer 证书——我们还没有),下载的 dmg / zip 会带上 Gatekeeper 的下载隔离标记,首次直接双击打开就会被拦截。

解决办法,终端执行:

```bash
xattr -dr com.apple.quarantine "/Applications/AT Term.app"
```

这条命令只是去掉这一个 app 的下载隔离标记,不会改动任何系统级安全设置。

用 Homebrew 安装不会触发这个隔离标记,不需要这一步——但 Homebrew cask 分发目前还在准备中,尚未可用(见 [安装指南](/guide/#方式-a-只用桌面端))。这不是签名问题本身的解决——真正的修法是签名 + 公证,目前卡在证书上。

## 忘记密码怎么办?

**没有密码找回**。忘记密码只能由 admin reset,相当于换一把新 `account_key`,旧会话的 E2EE sealed 内容永久不可解 —— 这是单用户自托管定位下的有意设计。

## 恢复对话框太烦?

Settings → General 可关闭恢复对话框,关掉后下次启动直接走 startNewTab。
