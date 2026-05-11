# Relay Owner Permissions And Admin Config Design

## Goal

Move remote session permissions from relay-only deployment configuration toward the session owner: the desktop app that owns the PTY decides what remote attachers may do. The relay still enforces the decision, and the desktop host applies a second enforcement layer before writing to the local PTY.

Add a relay admin surface for operational configuration. Admin changes must persist across relay restarts when the relay is started with a config path. The main write token remains outside persistent config and can only come from env/flag.

## Non-Goals

- No full user/account system.
- No per-user ACL database.
- No admin override that changes a desktop-owned session permission directly.
- No persistent storage of the main write token.
- No cleartext token storage for newly managed read-only tokens.

## Permission Model

Remote session permissions are capabilities published by the owner in `ANNOUNCE` metadata. The initial product-level permissions are:

| Value | Allows | Blocks |
|-------|--------|--------|
| `view` | list, attach, receive output/history | `IN`, `RESIZE`, `PASTE_IMAGE` |
| `control` | list, attach, receive output/history, `IN`, `RESIZE` | `PASTE_IMAGE` |
| `full` | list, attach, receive output/history, `IN`, `RESIZE`, `PASTE_IMAGE` | none |

A missing permission field defaults to `full` for backward compatibility with existing clients and agents.

Relay token scope and session owner permission combine by intersection:

- Relay write token + `full` session => full control.
- Relay write token + `view` session => view only.
- Relay read-only token + any session permission => view only.
- Unauthenticated request => rejected as today.

The relay is the first security boundary for remote clients. It must drop or reject frames that exceed effective permission before those frames enter the session inbound channel. The desktop uplink is the second boundary and must reject inbound frames that exceed the owner-published local permission before calling `SendLocalInbound`.

## Desktop UX

Settings gets a “remote session permissions” control near relay settings:

- `view only`: safest sharing mode.
- `control`: allow keyboard input and resize from remote clients.
- `full`: also allow remote image paste.

This is a global default for sessions announced by that desktop instance. Per-session override can be added later without changing the wire shape, because `SessionInfo` carries the effective permission per session.

The setting persists in the desktop app config. Changing it triggers a new `ANNOUNCE`, so the relay updates mirror session permissions without restarting either side.

## Protocol And Data Flow

Extend `proto.SessionInfo` with an optional JSON field:

```go
RemotePermission string `json:"remote_permission,omitempty"`
```

`desktop/uplink.go` builds `ANNOUNCE` from `relayHost.Snapshot()` and stamps the current configured permission on each session before JSON encoding.

`internal/relay/uplink_conn.go` reconciles the field into mirror session metadata. Relay client handling uses mirror session permission plus auth scope to decide whether `IN`, `RESIZE`, and `PASTE_IMAGE` are allowed.

For local mini relay sessions, permission defaults to `full` because only the desktop webview talks to the local relay with a random local token.

## Persistent Relay Admin Config

`cmd/atterm-relay` adds:

- `--config <path>` / `ATTERM_RELAY_CONFIG`: optional JSON config path.
- `--admin-token <token>` / `ATTERM_ADMIN_TOKEN`: enables admin API/page when non-empty.

When `--config` is set:

1. Startup reads config if present.
2. Env/flag values can initialize missing config fields, but the main write token remains env/flag only.
3. Admin changes are written back with atomic `tmp + rename` and mode `0600` for new files.
4. Startup warns if an existing config file is readable by group/other.

Persistent config includes operational settings only:

```json
{
  "rate_limit_per_minute": 600,
  "max_connections_per_key": 64,
  "read_only_tokens": [
    { "id": "mobile-view", "hash": "sha256:<base64url>", "created_at": 1770000000 }
  ]
}
```

The main write token is not stored here. It is still configured by `ATTERM_TOKEN` or generated at startup when missing. Admin UI cannot read, set, or rotate it in this phase.

## Read-Only Token Storage

Existing `ATTERM_READ_ONLY_TOKENS` and `--read-only-tokens` remain supported for simple deployments. They are accepted as cleartext startup inputs but are not written back to config as cleartext.

Admin-created read-only tokens are shown once on creation. The relay stores only `sha256:<base64url>` hashes plus metadata. Deletion is by token id. Runtime authentication accepts both startup cleartext tokens and persisted token hashes.

## Admin API And Page

Admin routes are served only when admin token is configured:

- `GET /admin/`: static minimal admin page.
- `GET /admin/api/config`: returns non-secret config and status.
- `PUT /admin/api/config`: updates rate/connection limits.
- `POST /admin/api/read-only-tokens`: creates a read-only token, returns token once.
- `DELETE /admin/api/read-only-tokens/{id}`: deletes a stored read-only token.

Admin authentication accepts `Authorization: Bearer <admin-token>`. Query-token auth is not used for admin API to avoid admin secrets in browser history and reverse proxy logs. The admin page stores the admin token only in memory for the current tab.

## Error Handling

- If admin token is missing, `/admin/*` returns 404 to avoid advertising the surface.
- If config path is missing or unwritable during an admin write, the API returns 500 and leaves runtime config unchanged.
- Invalid admin config returns 400 with a short reason.
- Duplicate read-only token ids return 409.
- Rate/connection limit changes take effect immediately for new requests/connections. Existing connections are not forcibly closed.

## Testing

Add tests before implementation:

1. Relay drops `IN` for a mirror session announced as `view` even with the write token.
2. Relay allows `IN` for `control`, but drops `PASTE_IMAGE`.
3. Read-only relay token remains view-only even when the session is `full`.
4. Desktop uplink refuses to forward inbound frames that exceed configured owner permission.
5. Admin API is 404 without admin token and 401 with the wrong token.
6. Admin config writes persist to disk with no main write token included.
7. Admin-created read-only token authenticates by hash after relay restart.
8. Existing web/PWA and relay tests remain green.

## Rollout

This is backward-compatible at the JSON level because new fields are optional. Existing clients without `remote_permission` continue to behave as `full`. Documentation must make clear that owner permission is the product permission model, while relay read-only tokens remain an operator-level global restriction.
