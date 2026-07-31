# 部署 Relay

## Docker Compose

最简单的部署方式:

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
  docker compose up -d atterm-relay
docker compose logs atterm-relay
```

compose 默认只起明文 HTTP `:8080`,**前面要挂一个 TLS 终止反代**(Cloudflare/Caddy/nginx/Tailscale)。浏览器经反代的 `https://relay.<你的域名>` 访问,没有 admin 时会自动进入首次安装页创建管理员。

> **为什么必须 HTTPS**:OPAQUE 登录用浏览器 WebCrypto,而它只在「安全上下文」(HTTPS 或 `localhost`) 可用,明文 HTTP 在公网 IP 上无法登录。**relay 不再自带自签证书**——浏览器直连的 TLS 必须来自真证书或前置反代。
>
> - **前置反代(推荐)**:Caddy/nginx/Tailscale/Cloudflare 终止 TLS,反代到 `:8080` HTTP 端口。
> - **relay 直接跑 HTTPS**:提供真证书 `ATTERM_TLS_CERT`/`ATTERM_TLS_KEY`,并加 `--https-addr :8443`。
> - **`:8080` 是明文 HTTP 端口**:仅供反代后端或内网用,浏览器直连它登录不了。
> - 仅本机临时用:`ssh -L 8080:127.0.0.1:8080 <host>` 后开 `http://localhost:8080`。

大多数配置已下沉到管理后台(Admin → Config / Feishu),持久化在 DB 表 `relay_config`(SQLite 或 Postgres,取决于 `ATTERM_RELAY_DB_DRIVER`),运行时即可修改、无需重启(VAPID subject 除外)。

> **存储后端**:`docker-compose.yml` 默认起一个 bundled Postgres 与 relay 一起跑(`ATTERM_RELAY_DB_DRIVER=postgres`)。多实例部署把 `ATTERM_RELAY_DB_DRIVER=postgres` + `ATTERM_RELAY_DB_DSN` 指向同一个**外部** Postgres 即可。若要单实例 SQLite,设 `ATTERM_RELAY_DB_DRIVER=sqlite` + `ATTERM_RELAY_DB_DSN=<config-dir>/users.db`。

## 核心环境变量

| 变量 | 用途 |
|------|------|
| `ATTERM_BOOTSTRAP_ADMIN_EMAIL` | 启动时为该邮箱打印一次性 claim token;用它注册一个**新**账号即获得 admin。公网监听必须设置(除非 `--dev-insecure`) |
| `ATTERM_ORIGINS` | 浏览器 Origin 白名单;公网部署必须设成真实域名 |
| `ATTERM_RELAY_PORT` | 宿主机端口,默认 `8080` |
| `ATTERM_RELAY_DB_DRIVER` | `sqlite`(默认)或 `postgres`。多实例部署必须 `postgres` |
| `ATTERM_RELAY_DB_DSN` | SQLite 下是文件路径,Postgres 下是 `postgres://user:pw@host:port/db?sslmode=...` DSN |
| `ATTERM_RELAY_CONFIG_DIR` | SQLite 模式下的持久化目录,默认 `./data/atterm-relay` |
| `ATTERM_RELAY_INSTANCE_PUBLIC_URL` | 本实例外部可达 URL;多实例部署必填,单实例可省 |

可选环境变量(不设也能启动;现在更推荐在管理后台配置):

| 变量 | 用途 |
|------|------|
| `ATTERM_FEISHU_ENCRYPT_KEY` | 飞书应用凭据 AEAD 静态加密密钥(32 字节 base64)。**不再必填**——在 Admin → Feishu 里可一键「生成」 |
| `ATTERM_FEISHU_BASE_URL` | 飞书 Open Platform 基础 URL,默认 `https://open.feishu.cn` |
| `ATTERM_VAPID_SUBJECT` | Web Push VAPID subject,默认 `mailto:noreply@atterm.local`(改动需重启) |
| `ATTERM_RATE_LIMIT_PER_MINUTE` | 每个 IP 的请求与 WS upgrade 分钟限额(**per-instance**,改后需重启) |
| `ATTERM_MAX_CONNECTIONS_PER_KEY` | 每个 IP 的活跃 WebSocket 连接上限(同上,**per-instance**) |

公网示例:

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_ORIGINS='https://relay.example.com' \
docker compose up -d atterm-relay
```

如果希望 Docker Hub `latest` 更新后自动拉取并重启 relay:

```bash
docker compose --profile auto-update up -d
```

该模式使用 watchtower,并需要挂载 Docker socket;不需要自动更新时,不要启用这个 profile。

## Go 直接运行

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_ORIGINS='https://relay.example.com' \
go run ./cmd/atterm-relay --addr :8080
```

本地开发可以临时跳过 Origin 校验(loopback 时 bootstrap env 可省略):

```bash
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure
```

`--web` 省略时使用 `internal/relay/web-dist/` 的内嵌 web 构建产物;需要测试当前工作区的 web 改动时,先 `cd web && npm run build`,再从仓库根目录传 `--web web/dist`。

## Bootstrap admin

密码认证已切换到 OPAQUE(密码只在客户端本地参与协议,**relay 永不接收明文密码**),因此 bootstrap 不再用密码 env,而是用一次性 **claim token**:

- **`ATTERM_BOOTSTRAP_ADMIN_EMAIL` 未设置**:relay 正常启动,不打印 claim token、也不创建 admin。可手动提权某用户(`UPDATE users SET is_admin=1 WHERE email='you@…';`)。
- **email 已设置**:relay 每次启动都为该 email 打印一条 7 天有效、**单次使用**的 claim token。在 `/signup.html` 用**该邮箱注册一个新账号**并把 token 填进「邀请码 / claim token」框,注册完成即创建用户并提为 admin(token 随即作废)。

claim token 只能用于**注册新账号**——若该邮箱已注册过,token 无法消费,此时改用上面的 SQL 直接提权。

## 管理后台(Admin)

以 admin 账号登录后,顶部导航出现 **Admin**,大部分运行时配置都在这里改、保存即生效、无需重启:

- **Config**:速率限制、每 key 连接上限、Origin 白名单、详细日志开关。
- **Feishu**:飞书集成开关 + 加密密钥(可一键「生成」)+ Open Platform base URL。保存后 relay 即可热接入飞书,无需重启;关闭即拆除。
- **Invitations / Users**:邀请记录与用户/角色管理。

> 需重启才生效的:VAPID subject、per-instance 的 `ATTERM_RATE_LIMIT_PER_MINUTE` / `ATTERM_MAX_CONNECTIONS_PER_KEY`。

## 多实例部署

需要跨机 HA / 就近节点路由时:

1. 起 N 个 relay 实例,全部指同一外部 Postgres(`ATTERM_RELAY_DB_DRIVER=postgres` + `ATTERM_RELAY_DB_DSN`)。
2. 每个实例设自己独有的 `ATTERM_RELAY_INSTANCE_PUBLIC_URL`。
3. Relay 首次启动写入 `relay_realm_state`(`realm_id` 全集群共享,永不变,直接影响 E2EE `account_key` 派生),并把自己心跳到 `relay_instances` 表。
4. 用户登录任意实例,响应携带 `realm_id` + `home_instance_url`;客户端拿到 `home_instance_url` 后自行 reconnect。
5. `GET /api/nodes` 返回活跃实例列表,供客户端做节点切换 UI。

> **红线**:`realm_id` 只从 DB 读、不从 env 覆盖——同一物理集群必须共享同一 realm,否则 E2EE 会跨实例失效;实例之间也不要拉 gossip / 直连,所有共享状态走 DB。
