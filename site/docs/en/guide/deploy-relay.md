---
description: Self-host AT Term Relay with Docker Compose, HTTPS, SQLite or PostgreSQL for authenticated, end-to-end encrypted remote terminal takeover.
---

# Deploy Relay

## Docker Compose

Start Relay and inspect its bootstrap output:

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_ORIGINS='https://relay.example.com' \
docker compose up -d atterm-relay
docker compose logs atterm-relay
```

Compose exposes plaintext HTTP on `:8080`. Put a TLS-terminating Caddy, nginx, Cloudflare, or Tailscale proxy in front of it. Browser authentication must run in HTTPS or on `localhost`; Relay never falls back to a self-signed certificate.

Relay can also terminate TLS directly when real certificate files are supplied through `ATTERM_TLS_CERT` and `ATTERM_TLS_KEY` together with `--https-addr :8443`. Keep plain `:8080` for a loopback proxy backend or trusted internal transport only.

Most runtime settings live in the `relay_config` database row and can be changed in Admin without restarting. VAPID subject and process-local rate/connection limits are the exceptions.

## Storage

The bundled Compose stack uses PostgreSQL. For multiple Relay instances, point every instance at the same external PostgreSQL database. A single instance may use SQLite.

| Variable | Purpose |
|---|---|
| `ATTERM_RELAY_DB_DRIVER` | `sqlite` or `postgres`; multi-instance requires PostgreSQL |
| `ATTERM_RELAY_DB_DSN` | SQLite path or PostgreSQL DSN |
| `ATTERM_RELAY_CONFIG_DIR` | Persistent directory used by SQLite mode |
| `ATTERM_RELAY_INSTANCE_PUBLIC_URL` | Public URL for this instance; required in a multi-instance deployment |

## Core environment variables

| Variable | Purpose |
|---|---|
| `ATTERM_BOOTSTRAP_ADMIN_EMAIL` | Prints a one-time claim token for this email; required on public listeners unless `--dev-insecure` |
| `ATTERM_ORIGINS` | Exact browser Origin allowlist; required for public deployment |
| `ATTERM_RELAY_PORT` | Host port used by Compose, default `8080` |
| `ATTERM_FEISHU_ENCRYPT_KEY` | Optional 32-byte base64 key; Admin can generate one later |
| `ATTERM_FEISHU_BASE_URL` | Feishu Open Platform base URL |
| `ATTERM_VAPID_SUBJECT` | Web Push subject; changing it requires restart |
| `ATTERM_RATE_LIMIT_PER_MINUTE` | Per-instance HTTP/WS upgrade limit |
| `ATTERM_MAX_CONNECTIONS_PER_KEY` | Per-instance active WebSocket limit |

To opt into Watchtower-based container updates, run `docker compose --profile auto-update up -d`. This mounts the Docker socket; leave the profile disabled when automatic container replacement is not desired.

## Run from Go

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_ORIGINS='https://relay.example.com' \
go run ./cmd/atterm-relay --addr :8080
```

For loopback development only:

```bash
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure
```

Without `--web`, Relay serves the embedded `internal/relay/web-dist`. To test a workspace Web build, run `cd web && npm run build`, then pass `--web web/dist` from the repository root.

## Bootstrap admin

Passwords never enter Relay because authentication uses OPAQUE. When `ATTERM_BOOTSTRAP_ADMIN_EMAIL` is set, each startup prints a seven-day, single-use claim token. Register a new account with the same email at `/signup.html` and put the token in the invitation/claim field. The new account becomes admin and consumes the token.

The token cannot upgrade an existing account. Promote an existing user directly in the database:

```sql
UPDATE users SET is_admin=1 WHERE email='you@example.com';
```

## Admin console

An admin sees an Admin link after login.

- **Config** controls rate limits, per-key connections, Origin allowlist, and detailed logging.
- **Feishu** controls the integration, master encryption key, and API base URL. Enabling or disabling applies without restart.
- **Invitations / Users** manages invitations, accounts, and roles.

Feishu routes remain registered but return a disabled response when the integration is off. The master key is stored in the database, never returned in full, and rotation requires an explicit force because old bindings become unreadable.

## Multiple instances

1. Point every instance at the same external PostgreSQL database.
2. Give each instance its own `ATTERM_RELAY_INSTANCE_PUBLIC_URL`.
3. The first startup creates one permanent cluster `realm_id`; every instance heartbeats into `relay_instances`.
4. Login returns `realm_id` and a preferred `home_instance_url`; the client reconnects itself.
5. `GET /api/nodes` lists active instances for node selection.

`realm_id` comes only from the shared database because it affects E2EE key derivation. Instances do not gossip or connect to one another; shared configuration and registry state stay in PostgreSQL.
