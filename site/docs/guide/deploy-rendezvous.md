# 部署 Rendezvous

Rendezvous 是 Peer Space 的无账户发现与信令服务。它没有用户数据库，不读取 Relay 配置，
不保存 membership 或同步配置，也不转发终端数据。WebRTC 建立后，终端流量不经过它。

官方和自建实例使用完全相同的 v1 协议与 contract suite。客户端配置只保存服务 origin，
例如 `https://rendezvous.example.com`；`/v1/connect`、`/healthz` 和 `/metrics` 由客户端统一派生，
不要把 API path 填进服务地址。

## 生产要求

- 公网入口必须是 HTTPS/WSS，TLS 最低版本为 1.3。
- 必须配置浏览器 Origin 精确 allowlist，例如 `https://app.example.com`。
- 服务直连 TLS 时必须提供真实证书，不生成或接受自签证书回退。
- TLS 反代后端只能监听 loopback；反代必须和 Rendezvous 运行在同一台主机。
- `--dev-insecure` 只用于本机开发，不用于公网部署。

原生桌面客户端通常不发送 `Origin`，但仍必须完成 P-256 challenge。浏览器始终发送 Origin，
不在 allowlist 时会在 WebSocket upgrade 前得到 `403`。

## Docker 直连 TLS

先构建本地镜像：

```bash
docker build -f Dockerfile.rendezvous -t atterm-rendezvous:local .
```

先把证书复制到容器用户（固定 UID/GID `10001`）可读的目录。不要直接挂载
`/etc/letsencrypt/live/<domain>`：其中的文件通常是指向 `archive/` 的 symlink，且私钥默认不允许
非 root 容器用户读取。

```bash
sudo install -d -o 10001 -g 10001 -m 750 /srv/atterm-rendezvous/tls
sudo install -o 10001 -g 10001 -m 640 \
  /etc/letsencrypt/live/rendezvous.example.com/fullchain.pem \
  /srv/atterm-rendezvous/tls/fullchain.pem
sudo install -o 10001 -g 10001 -m 640 \
  /etc/letsencrypt/live/rendezvous.example.com/privkey.pem \
  /srv/atterm-rendezvous/tls/privkey.pem
```

续签 hook 必须重新执行这两条 `install` 并重启容器。然后挂载准备好的目录：

```bash
docker run --rm \
  -p 8443:8443 \
  -e ATTERM_RENDEZVOUS_ORIGINS='https://app.example.com,capacitor://localhost' \
  -e ATTERM_RENDEZVOUS_TLS_CERT=/tls/fullchain.pem \
  -e ATTERM_RENDEZVOUS_TLS_KEY=/tls/privkey.pem \
  -v /srv/atterm-rendezvous/tls:/tls:ro \
  atterm-rendezvous:local
```

客户端服务地址填写 `https://rendezvous.example.com:8443`。若由标准 443 端口转发到容器，
填写 `https://rendezvous.example.com`。

## 同机 TLS 反代

Rendezvous 明文后端只监听 loopback：

```bash
ATTERM_RENDEZVOUS_ORIGINS='https://app.example.com' \
go run ./cmd/atterm-rendezvous \
  --addr 127.0.0.1:8081 \
  --behind-tls-proxy
```

Caddy 示例：

```text
rendezvous.example.com {
    reverse_proxy 127.0.0.1:8081
}
```

`--behind-tls-proxy` 不信任 `X-Forwarded-For`，因此进程内 per-IP 限流看到的是反代地址。
公网逐 IP 限流应在 Caddy、nginx 或边缘网关执行；进程内限制仍用于保护总连接数、topic 和 mailbox。

## 配置与容量

| 环境变量 | 默认值 | 说明 |
|---|---:|---|
| `ATTERM_RENDEZVOUS_ADDR` | `:8443` | 监听地址 |
| `ATTERM_RENDEZVOUS_ORIGINS` | 无 | 生产必填，逗号分隔的精确 Origin |
| `ATTERM_RENDEZVOUS_TLS_CERT` | 无 | 直接 TLS 的证书 PEM |
| `ATTERM_RENDEZVOUS_TLS_KEY` | 无 | 直接 TLS 的私钥 PEM |
| `ATTERM_RENDEZVOUS_MAX_CONNECTIONS` | `1024` | 全局连接上限 |
| `ATTERM_RENDEZVOUS_MAX_CONNECTIONS_PER_IP` | `32` | 每 TCP 来源地址连接上限 |
| `ATTERM_RENDEZVOUS_MAX_CONNECTIONS_PER_TOPIC` | `32` | 每 opaque topic presence 上限 |
| `ATTERM_RENDEZVOUS_MAX_MAILBOX_PER_TOPIC` | `128` | 每 topic 易失 mailbox 条数 |
| `ATTERM_RENDEZVOUS_MAX_MAILBOX_GLOBAL` | `4096` | 全局易失 mailbox 条数 |
| `ATTERM_RENDEZVOUS_MAX_RECENT_MESSAGES` | `8192` | retry 去重记录上限 |
| `ATTERM_RENDEZVOUS_MAX_MESSAGES_PER_MINUTE_PER_IP` | `240` | 每 IP 每分钟 publish 上限 |
| `ATTERM_RENDEZVOUS_LOG_LEVEL` | `INFO` | `DEBUG/INFO/WARN/ERROR` |

所有 presence、mailbox 和 retry 去重记录都只在内存中；进程重启会全部丢失。mailbox 和去重
TTL 是 120 秒，不提供持久 store-and-forward。

## Contract 验收

部署后运行同一套黑盒契约：

```bash
go run ./cmd/atterm-rendezvous-contract \
  --url https://rendezvous.example.com \
  --origin https://app.example.com
```

成功输出只包含协议版本与检查结果：

```json
{"protocol_version":1,"live_delivery":true,"mailbox_delivery":true,"retry_deduplicated":true,"oversize_rejected":true,"metrics_private":true}
```

本机开发实例需要显式允许明文：

```bash
go run ./cmd/atterm-rendezvous-contract \
  --url http://127.0.0.1:8081 \
  --allow-insecure-loopback
```

contract 会验证 health、subprotocol、challenge、presence、在线投递、离线 mailbox、retry 去重、
64 KiB 上限和 metrics 隐私。它不会输出测试使用的 topic、presence、message id 或 payload。

## STUN 与隐私边界

默认 STUN 是 `stun:stun.cloudflare.com:3478`。可改为一个或多个 `stun:`/`stuns:` 地址，
也可禁用。当前版本明确拒绝 `turn:`/`turns:`：Rendezvous 不是 TURN，受限 NAT 下直连仍可能失败。

可观察元数据：

- Rendezvous 可看到来源 IP、opaque topic/presence、消息大小和时间，但看不到加密后的 SDP/ICE 内容；
- STUN 服务可看到来源 IP 和请求时间；
- WebRTC 对端会获得建立直连所需的 ICE candidate 地址；
- `/metrics` 与服务日志不包含 topic、presence、公钥、message id 或 payload。

浏览器客户端只有在安全上下文、WebCrypto 和 `RTCPeerConnection` 三项均可用时才会开始注册。
公网 HTTP 会在生成 Peer 身份或创建 WebRTC 连接前失败；`localhost` 仅用于本机开发。
