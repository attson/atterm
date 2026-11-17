---
description: Deploy the accountless AT Term Rendezvous discovery and signaling service for Peer Space with TLS, Origin checks, rate limits, and contract verification.
---

# Deploy Rendezvous

Rendezvous provides accountless discovery and signaling for Peer Space. It has no user database, does not read Relay configuration, does not store membership or synchronized settings, and never forwards terminal traffic. Once WebRTC connects, the data path bypasses Rendezvous.

Configure clients with only the service origin, such as `https://rendezvous.example.com`. Clients derive `/v1/connect`, `/healthz`, and `/metrics`; do not include an API path in the configured address.

## Production requirements

- Public entry points use HTTPS/WSS with TLS 1.3 or newer.
- Browser Origins use an exact allowlist.
- Direct TLS requires a real certificate and has no self-signed fallback.
- A plaintext proxy backend listens on loopback. A container may use `--allow-non-loopback-proxy` only when host publication or the container network keeps it private.
- `--dev-insecure` is for local development only.

Native desktop clients normally omit `Origin` but must still pass the P-256 challenge. Browsers always send Origin and receive `403` before upgrade when it is not allowed.

## Docker with direct TLS

```bash
docker build -f Dockerfile.rendezvous -t atterm-rendezvous:local .
```

Copy certificates into a directory readable by the fixed container UID/GID `10001`. Do not directly mount Let's Encrypt `live/` symlinks.

```bash
sudo install -d -o 10001 -g 10001 -m 750 /srv/atterm-rendezvous/tls
sudo install -o 10001 -g 10001 -m 640 \
  /etc/letsencrypt/live/rendezvous.example.com/fullchain.pem \
  /srv/atterm-rendezvous/tls/fullchain.pem
sudo install -o 10001 -g 10001 -m 640 \
  /etc/letsencrypt/live/rendezvous.example.com/privkey.pem \
  /srv/atterm-rendezvous/tls/privkey.pem
```

A renewal hook must repeat the copies and restart the container.

```bash
docker run --rm \
  -p 8443:8443 \
  -e ATTERM_RENDEZVOUS_ORIGINS='https://app.example.com,capacitor://localhost' \
  -e ATTERM_RENDEZVOUS_TLS_CERT=/tls/fullchain.pem \
  -e ATTERM_RENDEZVOUS_TLS_KEY=/tls/privkey.pem \
  -v /srv/atterm-rendezvous/tls:/tls:ro \
  atterm-rendezvous:local
```

Configure the client with `https://rendezvous.example.com:8443`, or omit the port when standard 443 forwards to the container.

## TLS reverse proxy

Run the plaintext backend on loopback:

```bash
ATTERM_RENDEZVOUS_ORIGINS='https://app.example.com' \
go run ./cmd/atterm-rendezvous \
  --addr 127.0.0.1:8081 \
  --behind-tls-proxy
```

Caddy configuration:

```text
rendezvous.example.com {
    reverse_proxy 127.0.0.1:8081
}
```

The Compose `rendezvous` profile publishes the container backend only on host loopback. Do not change it to `0.0.0.0:8081`. `--behind-tls-proxy` does not trust `X-Forwarded-For`; enforce public per-IP limits in the edge proxy while process limits continue protecting total connections, topics, and mailboxes.

## Capacity settings

| Variable | Default | Purpose |
|---|---:|---|
| `ATTERM_RENDEZVOUS_ADDR` | `:8443` | Listen address |
| `ATTERM_RENDEZVOUS_ORIGINS` | none | Exact production Origin list |
| `ATTERM_RENDEZVOUS_TLS_CERT` | none | Direct-TLS certificate PEM |
| `ATTERM_RENDEZVOUS_TLS_KEY` | none | Direct-TLS private key PEM |
| `ATTERM_RENDEZVOUS_MAX_CONNECTIONS` | `1024` | Global connections |
| `ATTERM_RENDEZVOUS_MAX_CONNECTIONS_PER_IP` | `32` | Connections per source IP |
| `ATTERM_RENDEZVOUS_MAX_CONNECTIONS_PER_TOPIC` | `32` | Presence entries per opaque topic |
| `ATTERM_RENDEZVOUS_MAX_MAILBOX_PER_TOPIC` | `128` | Ephemeral mailbox entries per topic |
| `ATTERM_RENDEZVOUS_MAX_MAILBOX_GLOBAL` | `4096` | Global ephemeral mailbox entries |
| `ATTERM_RENDEZVOUS_MAX_RECENT_MESSAGES` | `8192` | Retry deduplication records |
| `ATTERM_RENDEZVOUS_MAX_MESSAGES_PER_MINUTE_PER_IP` | `240` | Publish rate per IP |
| `ATTERM_RENDEZVOUS_LOG_LEVEL` | `INFO` | `DEBUG`, `INFO`, `WARN`, or `ERROR` |

Presence, mailbox, and retry records live only in memory and disappear on restart. Mailbox and deduplication TTL is 120 seconds; Rendezvous is not durable store-and-forward.

## Contract verification

Run the same black-box suite used by the project:

```bash
go run ./cmd/atterm-rendezvous-contract \
  --url https://rendezvous.example.com \
  --origin https://app.example.com
```

The result should report protocol version 1 plus successful live delivery, mailbox delivery, retry deduplication, oversize rejection, and private metrics. For a local plaintext instance, use `--url http://127.0.0.1:8081 --allow-insecure-loopback`.

## STUN, TURN, and privacy

The default STUN server is `stun:stun.cloudflare.com:3478`. Users can replace or disable it. Rendezvous is not TURN; restrictive NATs require a separately configured `turn:` or `turns:` service. TURN credentials stay in the local system keyring and are not sent to Rendezvous or synchronized through Peer Space.

Rendezvous can observe source IP, opaque topics/presence, sizes, and timing, but signaling payloads are encrypted. STUN sees source IP and timing. A user-selected TURN service sees endpoint IPs, timing, and encrypted WebRTC traffic size. WebRTC peers necessarily learn ICE candidate addresses. Metrics and logs omit topics, presence, public keys, message IDs, and payloads.

Browser registration requires a secure context, WebCrypto, and `RTCPeerConnection`. Public HTTP fails before a peer identity or WebRTC connection is created; `localhost` is the only development exception.

For joining a Peer Space, importing a connection bundle, and taking over a session, see [Remote takeover and sessions](/en/guide/remote-takeover) and [Cross-device clients](/en/guide/remote-clients).
