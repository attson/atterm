# SaaS user accounts for atterm-relay (design)

Date: 2026-05-15
Status: Approved (design phase); pending implementation plan

## 1. Goal

Replace the relay's shared-token authentication with a per-user account
system. After this lands:

- Operators run one relay; each end-user has their own account.
- Each user sees only their own sessions and the sessions they hold an
  active share link to.
- Desktop / CLI uplinks authenticate as a specific user via a per-user API
  token issued from the web UI.
- The existing `ATTERM_TOKEN` and `ATTERM_READ_ONLY_TOKENS` are removed.
  `ATTERM_ADMIN_TOKEN` remains as the sole operator-side credential.

Out of scope (see §8.1): organizations, teams, roles, billing, quotas,
email verification, password reset email, OAuth, multi-relay clustering,
audit logs.

## 2. Architecture

```
┌────────────────────────── relay process (single binary) ────────────────────┐
│                                                                             │
│  cmd/atterm-relay         Bootstrap: parse env / flags → wire userstore     │
│      │                                                                      │
│      ▼                                                                      │
│  internal/relay/                                                            │
│      identity.go (new)    resolveIdentity → Principal                       │
│      auth_http.go (new)   /api/auth/{signup,login,logout} /api/me/*         │
│      csrfmw.go (new)      require-CSRF middleware                           │
│      server.go            HTTP routes + Principal-gated handlers            │
│      admin_http.go        invite / user admin pages                         │
│      client_conn.go       list/attach filtered by owner_user_id             │
│      uplink_conn.go       token → user, set Session.OwnerUserID             │
│      permissions.go       share-link Principal is enforced read-only        │
│                                                                             │
│  internal/userstore/ (new — only package that touches SQLite)               │
│      store.go             DB handle, migration runner, txn helper           │
│      users.go             CRUD + argon2id password hashing                  │
│      invitations.go       create / consume / expire                         │
│      apitokens.go         issue / revoke; sha256(token) stored              │
│      sharelinks.go        issue / revoke; bound to session_id               │
│      websessions.go       cookie session table + sliding expire             │
│                                                                             │
│  internal/session/        Session adds OwnerUserID string field             │
│  internal/proto/          New frame AUTH_INFO (unused Type byte)            │
│  internal/webpush/        subStore key = user_id (not tokenHash)            │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘

web/  (vanilla JS, multi-page)
  index.html        terminal & sessions list (cookie-gated)
  login.html        email + password
  signup.html       invite code + email + password
  settings.html     API tokens / share links / change password
  app.js            fetch with cookie; 401 → redirect /login.html
```

### 2.1 Module-boundary rules

These are non-negotiable and must hold in every PR landing this spec.

1. `internal/userstore` is the only package that imports a SQLite driver
   or speaks SQL. It exposes a `Store` interface.
2. `internal/relay` depends on the `Store` interface, not its concrete
   implementation. `cmd/atterm-relay` wires the concrete `SQLiteStore` in.
3. `internal/session.Session` gains `OwnerUserID string` (empty string =
   anonymous; not produced by this spec but kept available for future
   non-account paths).
4. Wire protocol stays at `Version = 1`. `AUTH_INFO` is a new frame type
   on an unused `Type` byte; existing frame payloads are unchanged.
   `docs/spec/protocol.md` is updated in lockstep.
5. Dependency direction: `cmd/ → internal/relay → internal/userstore`,
   `internal/relay → internal/session`. No reverse imports.

### 2.2 HTTP surface

```
POST   /api/auth/signup      {email, password, invite_code}     → set cookie
POST   /api/auth/login       {email, password}                  → set cookie
POST   /api/auth/logout                                         → clear cookie
GET    /api/me                                                  → user info, csrf_token
GET    /api/me/tokens                                           → list (no plaintext)
POST   /api/me/tokens        {name}                             → returns plaintext atk_… once
DELETE /api/me/tokens/:id                                       → revoke
GET    /api/me/sessions/:sid/shares                             → list shares
POST   /api/me/sessions/:sid/shares {expires_at?}               → returns plaintext shr_… once
DELETE /api/me/sessions/:sid/shares/:share_id                   → revoke

/admin/api/invitations                                          → create / list / revoke
/admin/api/users                                                → list / reset password / disable
```

WebSocket entry points (`/agent`, `/uplink`, `/client`) are unchanged at
the wire level. Only their HTTP-upgrade-time identity resolution changes.

## 3. Data model

SQLite file at `${ATTERM_RELAY_CONFIG_DIR}/users.db`, WAL mode. Schema is
applied via embedded `.sql` files in `internal/userstore`, tracked in a
`schema_migrations` table.

```sql
CREATE TABLE users (
    id             TEXT PRIMARY KEY,                -- ULID
    email          TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash  TEXT NOT NULL,                   -- argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
    csrf_secret    BLOB NOT NULL,                   -- 32B random
    created_at     INTEGER NOT NULL,
    disabled_at    INTEGER
);

CREATE TABLE invitations (
    code_hash      TEXT PRIMARY KEY,                -- sha256(code)
    created_by     TEXT NOT NULL,                   -- 'admin'
    created_at     INTEGER NOT NULL,
    expires_at     INTEGER,
    consumed_at    INTEGER,
    consumed_by    TEXT REFERENCES users(id),
    note           TEXT
);

CREATE TABLE api_tokens (
    id             TEXT PRIMARY KEY,                -- ULID, exposed in UI
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    token_hash     TEXT NOT NULL UNIQUE,            -- sha256(token)
    token_prefix   TEXT NOT NULL,                   -- 'atk_' + first 8 chars
    created_at     INTEGER NOT NULL,
    last_used_at   INTEGER,
    revoked_at     INTEGER
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id) WHERE revoked_at IS NULL;

CREATE TABLE share_links (
    id             TEXT PRIMARY KEY,                -- ULID
    owner_user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id     TEXT NOT NULL,
    token_hash     TEXT NOT NULL UNIQUE,
    token_prefix   TEXT NOT NULL,                   -- 'shr_' + first 8 chars
    created_at     INTEGER NOT NULL,
    expires_at     INTEGER,
    revoked_at     INTEGER
);
CREATE INDEX idx_share_links_session ON share_links(session_id) WHERE revoked_at IS NULL;

CREATE TABLE web_sessions (                        -- browser login sessions
    id_hash        TEXT PRIMARY KEY,                -- sha256(cookie value)
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at     INTEGER NOT NULL,
    expires_at     INTEGER NOT NULL,
    user_agent     TEXT,
    ip_prefix      TEXT                             -- IPv4 /24 or IPv6 /48
);
CREATE INDEX idx_web_sessions_user ON web_sessions(user_id);
CREATE INDEX idx_web_sessions_expires ON web_sessions(expires_at);
```

Notes:
- Password hashing uses `golang.org/x/crypto/argon2`, parameters
  `time=3, memory=64MiB, threads=2, key_len=32`. The encoded format
  carries the parameters so future tuning does not invalidate old hashes.
- All credentials (invite codes, API tokens, share tokens, cookie values)
  are stored as `sha256(plaintext)`. Plaintext is returned only at issue
  time.
- Terminal sessions are not persisted; only their identity (owner) lives
  in memory on the running relay. A share link whose target session has
  vanished returns 404 — this is intended.
- IDs use ULID (`github.com/oklog/ulid/v2`).

## 4. Identity resolution

Single function at HTTP / WS-upgrade boundary:

```go
type PrincipalKind uint8
const (
    PrincipalNone PrincipalKind = iota
    PrincipalUser
    PrincipalShare
    PrincipalAdmin
)

type Principal struct {
    Kind      PrincipalKind
    UserID    string  // User
    OrgID     string  // reserved; empty for this spec — see §11
    TokenID   string  // User via API token (empty when cookie)
    ShareID   string  // Share
    SessionID string  // Share: bound session
    Scope     authScope
}
```

Resolution order (first match wins):

1. Cookie `atterm_session=<sid>` → lookup `sha256(sid)` in `web_sessions`,
   not expired, user not disabled → `Principal{User, write}`. Sliding:
   if `last_used > 24h`, push `expires_at` to now + 30d.
2. `Authorization: Bearer <token>` or `Sec-WebSocket-Protocol:
   atterm-token.<token>` / `atterm-token-b64.<...>`:
   - If `constantTimeEqual(token, ATTERM_ADMIN_TOKEN)` →
     `Principal{Admin, write}`.
   - Else lookup `sha256(token)` in `api_tokens`, not revoked, user not
     disabled → `Principal{User, write, TokenID}`. Bump `last_used_at`
     asynchronously.
3. `Sec-WebSocket-Protocol: atterm-share.<share_token>` →
   `sha256(token)` in `share_links`, not revoked, not expired →
   `Principal{Share, read, SessionID}`.
4. Otherwise `Principal{None}`.

### 4.1 Cookie

- Name `atterm_session`, attributes `HttpOnly; Secure; SameSite=Lax;
  Path=/`. `Secure` is suppressed only when the request is over loopback
  HTTP (dev mode).
- Value: 32 random bytes (`crypto/rand`), base64url. Stored as
  `sha256(value)`.
- Lifetime: 30 days with sliding renewal.

### 4.2 CSRF

- All mutating routes under `/api/*` (except `/api/auth/login` and
  `/api/auth/signup`) require `X-CSRF-Token`. Value equals first 16 bytes
  of `base64url(sha256(cookie_value || users.csrf_secret))`. Frontend
  reads its current CSRF token from `GET /api/me`.
- `/api/auth/login` and `/api/auth/signup` rely on `SameSite=Lax` for
  cross-site form-post mitigation.
- WebSocket upgrades are not CSRF-protected; they rely on the Origin
  allowlist and `Sec-WebSocket-Protocol` credentials.
- Middleware `csrfmw.Require(h)` is mandatory on every mutating handler;
  a test enumerates the mux and fails if any non-GET/HEAD route is
  unwrapped.

### 4.3 Entry-point gate

| Entry | Allowed Principal |
|---|---|
| `GET /api/me/*` | User |
| `GET /api/sessions` | User (filtered to `OwnerUserID == UserID`) |
| `GET/WS /client?session=<id>` | User (owner) or Share (matching SessionID; read-only enforced) |
| `WS /uplink` | User from API token (TokenID non-empty); cookie source rejected |
| `WS /agent` | User from API token |
| `/admin/*` | Admin |
| `POST /api/auth/{signup,login}` | None (public) |

Rejecting cookie principals at `/uplink` and `/agent` prevents a logged-in
browser from impersonating a desktop publisher.

## 5. Key flows

### 5.1 Admin issues an invite

```
POST /admin/api/invitations  (Bearer ATTERM_ADMIN_TOKEN)
  body: {expires_at?, note?}
   → crypto/rand → 16B → base32 → 'inv_XXXX…'
   → sha256(code) into invitations.code_hash
   → return plaintext once
```

### 5.2 User signup with invite

```
POST /api/auth/signup {email, password, invite_code}
  1. validate email format; password length ≥ 12
  2. sha256(invite_code) → lookup invitations: not consumed, not expired
  3. argon2id(password)
  4. txn:
       INSERT users (id, email, password_hash, csrf_secret, created_at)
       UPDATE invitations SET consumed_at=now, consumed_by=user_id
         WHERE code_hash=? AND consumed_at IS NULL
       (row count 0 = race lost → roll back, 400)
  5. createWebSession → Set-Cookie
  6. 200 {user_id, email}
```

Errors (kept indistinguishable to avoid existence-leak):

| Case | HTTP | body |
|---|---|---|
| Bad invite format | 400 | `{error:"invite_invalid"}` |
| Invite consumed / expired | 400 | `{error:"invite_invalid"}` |
| Email taken | 409 | `{error:"email_taken"}` |
| Password too weak | 400 | `{error:"password_weak"}` |

### 5.3 Login / logout

```
POST /api/auth/login {email, password}
  1. lookup users WHERE email=? AND disabled_at IS NULL
  2. argon2id verify (always run, even on missing email, to flatten timing)
  3. on success: createWebSession → Set-Cookie
  4. 200 {user_id, email}

POST /api/auth/logout
  1. read cookie sid
  2. DELETE FROM web_sessions WHERE id_hash = sha256(sid)
  3. clear cookie
```

Failed login is rate-limited per `(IP, sha256(email))`, 10/5min, with a
minimum 200ms response delay.

### 5.4 API token issuance

```
POST /api/me/tokens {name}
  1. crypto/rand 32B → base64url → 'atk_' + body  (≈50 chars total)
  2. token_prefix = 'atk_' + first 8 body chars
  3. token_hash = sha256(plaintext)
  4. INSERT api_tokens (id, user_id, name, token_hash, token_prefix, created_at)
  5. return plaintext exactly once
```

Revoke:
```
DELETE /api/me/tokens/:id
  → UPDATE api_tokens SET revoked_at=now WHERE id=? AND user_id=?
```

Active uplink connections using a revoked token are not actively kicked;
they fail on next reconnect / keepalive. This reuses existing teardown.

### 5.5 First uplink binds session to user

```
desktop → wss://relay/uplink (subprotocol atterm-token.<atk_…>)
  relay.acceptUplink:
    1. resolveIdentity → Principal{User, UserID, TokenID}
       reject if Kind != User or TokenID empty (rejects cookie / share / admin)
    2. read HELLO frame → host_id
    3. connection-scope OwnerUserID = Principal.UserID
  on session publish:
    session.NewSession(...).OwnerUserID = connection.OwnerUserID

GET /api/sessions  (cookie principal)
  return [s for s in registry if s.OwnerUserID == Principal.UserID
                              or s.SessionID has active share for Principal]
```

Session-ID-based dedup (red line #3) is unchanged. OwnerUserID is
per-session, not per-host; a machine can run multiple desktop instances
logged in as different users.

### 5.6 Admin password reset

```
POST /admin/api/users/:id/reset-password   (Bearer ATTERM_ADMIN_TOKEN)
  1. crypto/rand 16B → base64url → 'tmp_xxxx…'
  2. argon2id(tmp) → UPDATE users SET password_hash=?, csrf_secret=randomblob(32)
     (rotating csrf_secret invalidates any active CSRF tokens for this user)
  3. DELETE FROM web_sessions WHERE user_id=?    (force re-login)
  4. return plaintext tmp password once; admin relays it to the user
     out-of-band
```

No email path. This is acceptable because signup is invite-only (§5.2):
the admin already has a side channel with the user.

### 5.7 Share link

```
POST /api/me/sessions/<sid>/shares  {expires_at?}
  1. require sid ∈ in-memory session registry and OwnerUserID == UserID
  2. crypto/rand 24B → base64url → 'shr_' + body
  3. INSERT share_links (id, owner_user_id, session_id, token_hash, token_prefix, expires_at)
  4. return plaintext URL: '<relay>/#share=shr_…'

visitor → wss://relay/client (subprotocol atterm-share.<shr_…>)
  resolveIdentity → Principal{Share, SessionID=sid, Scope=read}
  attach via existing read-only enforcement
```

Revoke:
```
DELETE /api/me/sessions/<sid>/shares/<share_id>
  → UPDATE share_links SET revoked_at=now
```

Active visitor is dropped on next frame (existing read-only enforcement
re-checks principal).

## 6. Security invariants

These are testable assertions; the test suite enforces them.

**SEC-1.** Credential plaintext (passwords, invite codes, API tokens,
share tokens, cookie values) never enters the database, log files, or
any long-term storage. `Secret[T]` newtype's `String()` returns only the
prefix.

**SEC-2.** argon2id parameters are constants in code; the stored hash
format carries the parameters, so future tuning does not invalidate old
hashes.

**SEC-3.** Login response timing is independent of whether the email
exists. Missing-email path runs the same argon2id work against a fixed
dummy hash.

**SEC-4.** Every mutating route on the mux (non-GET/HEAD) must be wrapped
in `csrfmw.Require`. A test enumerates `http.ServeMux` and fails if any
route is bare.

**SEC-5.** Rate limits and failure-response floor:

| Entry | Key | Limit |
|---|---|---|
| `/api/auth/login` failed | IP + sha256(email) | 10 / 5min |
| `/api/auth/signup` | IP | 5 / hour |
| Invite-code failed | IP | 10 / hour |

Failed responses delay to a minimum of 200 ms (random jitter ±50 ms).

**SEC-6.** On non-loopback listen, relay refuses to start unless
`ATTERM_ADMIN_TOKEN` is set and has ≥ 32 chars of entropy AND
`ATTERM_ORIGINS` is set. Exception: `--dev-insecure` (red line #9).

**SEC-7.** Identity decisions read the database every request. No
in-memory cache. Local SQLite query is < 1 ms; revisit only if profiling
shows it as a bottleneck.

### 6.1 Bootstrap

A fresh deployment has no users and no invites. Path:

1. Operator starts relay → log line:
   `admin token: set (sha256: …); user store: 0 users, 0 invites`
2. Operator hits `/admin/` with `ATTERM_ADMIN_TOKEN` → creates an invite.
3. Sends invite to the first user.
4. User signs up at `/signup?invite=inv_…`.
5. User generates an API token at `/settings.html` and pastes it into the
   desktop client.

Admin is never a `users` row. `ATTERM_ADMIN_TOKEN` is an operator-side
break-glass credential, kept separate from the application's user model.

## 7. Removal of legacy paths

This release removes all shared-token paths. No deprecation period, no
warning fallback (single-user project; see `feedback_no_backward_compat`).

Deletions:
- `cmd/atterm-relay/main.go`: drop reads of `ATTERM_TOKEN`,
  `ATTERM_READ_ONLY_TOKENS`, and related startup checks.
- `internal/relay/auth.go`: drop the "empty token = allow write"
  shortcut and the `readOnlyTokens` / `readOnlyHashes` parameters; only
  `resolveIdentity` remains.
- `internal/relay/admin_config.go`: drop `ReadOnlyTokenHashes` field.
- `internal/relay/permissions.go`: drop the read-only-token branch; the
  read-only path is now exercised solely by share-link Principals.
- `web/index.html`: drop the token entry panel; route unauthenticated
  requests to `/login.html`.
- `README.md`, `AGENTS.md`, `docs/spec/architecture.md`: drop all
  mentions of `ATTERM_TOKEN` and `ATTERM_READ_ONLY_TOKENS`; update
  start-up examples.

Retained operator env:
`ATTERM_ADMIN_TOKEN`, `ATTERM_ORIGINS`, `ATTERM_RELAY_PORT`,
`ATTERM_RELAY_CONFIG_DIR`, `ATTERM_RATE_LIMIT_PER_MINUTE`,
`ATTERM_MAX_CONNECTIONS_PER_KEY`, `ATTERM_RELAY_URL`,
`ATTERM_RELAY_TOKEN`.

Zero-value compatibility kept where free: `desktop/config.go`
`RelayPaused bool` defaults to false; old `config.json` deserializes
unchanged. SQLite `schema_migrations` is intra-process schema versioning,
unrelated to releases.

## 8. Desktop and CLI changes

### 8.1 SettingsRelay redesign

- Label `token` → `API token`. Placeholder `atk_xxxx…`.
- Hint line `Generate at <relay-url>/settings.html` with an
  `Open in browser` button (Wails `BrowserOpenURL`).
- On paste, if the value does not start with `atk_`, show a red hint
  ("This doesn't look like an API token."). Non-blocking; save still
  allowed.
- Status row now reads `connected as <email>` when uplink is up,
  sourced from the new `AUTH_INFO` frame.

### 8.2 Fix "disconnect erases config"

Root cause: in `desktop/config.go`, `RelayURL == ""` carries two meanings
(unconfigured vs. user-paused), so the only way the frontend can
"disconnect" is to clear the URL.

Resolution:

```go
type appConfig struct {
    // existing fields unchanged ...
    RelayURL           string `json:"relay_url,omitempty"`
    RelayToken         string `json:"relay_token,omitempty"`
    AllowInsecureRelay bool   `json:"allow_insecure_relay,omitempty"`
    RemotePermission   string `json:"remote_permission,omitempty"`

    // new: user-toggled pause. zero value (false) preserves the
    // existing "has URL → connect" behavior, so old config.json
    // deserializes without migration code.
    RelayPaused bool `json:"relay_paused,omitempty"`
}
```

`applyRelayConfig` gates on `cfg.RelayURL == "" || cfg.RelayPaused`. New
Wails binding `SetUplinkPaused(paused bool)` toggles the pause field
without touching URL / token / insecure / permission. The frontend
replaces the existing "disconnect" button with a top-of-pane ON/OFF
toggle bound to `cfg.RelayPaused`; URL and token inputs stay populated
while paused.

Status pill matrix:

| URL | RelayPaused | uplink | pill |
|---|---|---|---|
| empty | any | stopped | `not configured` |
| set | false | running | `uplink running` |
| set | false | retrying | `connecting…` / error |
| set | true | stopped | `paused (config kept)` |

This fix is independent of the SaaS work and may land as its own PR
before the rest of the spec.

### 8.3 Auth error feedback

On uplink close, the relay sends a close-frame `Reason` enum. The
desktop maps the reason to a Wails event `relay:auth-error` and surfaces
a banner with an `Open settings` action:

| Reason | Banner |
|---|---|
| `auth_invalid_token` | "Invalid or revoked API token. Generate a new one in web settings." |
| `auth_user_disabled` | "Account disabled. Contact your relay admin." |
| (other / network) | existing reconnect spinner |

### 8.4 `AUTH_INFO` frame

Red line #4 permits new frame `Type` bytes. After successful uplink
authentication, the relay sends an `AUTH_INFO` frame whose payload is
UTF-8 JSON:

```json
{"user_id": "01HX…", "user_email": "alice@example.com"}
```

`internal/proto.Version` stays at 1 because semantics of existing frames
do not change. `docs/spec/protocol.md` records the new frame with its
Type byte, payload schema, and forward-compat note (unknown JSON keys
must be ignored).

### 8.5 CLI agent

`cmd/atterm-agent --token` keeps its name (its semantics — "string to
ship to the relay" — are unchanged). `--help` text mentions API tokens
from `/settings.html`. Environment variable reads stay the same.

### 8.6 Single profile

The desktop config remains flat (`relay_url`, `relay_token`,
`relay_paused`). Multi-profile / account-switching is out of scope. When
multi-profile is needed in the future, migrate the schema in one shot;
do not pre-abstract.

### 8.7 Web push: subscriptions become user-scoped

Today, `internal/webpush/subscription.go` keys subscriptions by
`tokenHash`. After this spec lands, a cookie-based browser has no token,
and share-link visitors would otherwise inherit subscription rights.

Changes:

- `subStore.byID` is keyed by `user_id`.
- `AddSubscription(userID, sub)` / `RemoveSubscription(userID, endpoint)`
  / `ByUser(userID)` replace their tokenHash-based counterparts.
- `DispatchCommandFinished` takes the originating session's
  `OwnerUserID` and fans out only to that user's subscriptions, instead
  of broadcasting across all tokens.
- `/api/push/subscribe` and `/api/push/test` reject share principals
  (403).
- Persistence (`web-push.json`) schema changes from
  `{tokenHash: [subs]}` to `{userID: [subs]}`. On startup, if an existing
  file has the legacy schema, it is renamed to
  `web-push.json.legacy-<ts>` and a fresh registry is initialized. Users
  re-enable notifications.

This closes a real isolation hole; without it, command-finished events
fan out across all users on the relay and share-link visitors can
subscribe to a stream of someone else's future events.

## 9. Test strategy

Standard `testing` package; no new test libraries.

### 9.1 `internal/userstore`

| Test | Asserts |
|---|---|
| `TestCreateUser_HashesPassword` | argon2id verify ok; row's password_hash does not contain plaintext |
| `TestCreateUser_DuplicateEmail` | second insert returns `ErrEmailTaken` |
| `TestCreateInvitation_OneShotConsume` | 100 concurrent consumes, exactly one succeeds |
| `TestInvitationExpired` | expired invite returns `ErrInviteInvalid` and is unchanged |
| `TestCreateAPIToken_ReturnsPlaintextOnce` | return contains `atk_`; readback does not |
| `TestAPIToken_RevokedRejected` | revoked token lookup returns `ErrTokenRevoked` |
| `TestShareLink_ExpiresAt` | expiry boundary ±1 s |
| `TestShareLink_BoundToSession` | token only attaches its bound session |
| `TestWebSession_SlidingExpire` | sliding window pushes expires_at forward |
| `TestMigrate_FromEmpty` | clean directory → applies 0001_init.sql |
| `TestMigrate_Idempotent` | re-run is a no-op |
| `TestCSRFSecret_IsRandom` | 100 fresh users have distinct csrf_secrets |

In-memory SQLite via `modernc.org/sqlite` (new pure-Go dependency, no CGO).

### 9.2 `internal/relay` identity / gates

`identity_test.go`: table-driven matrix over cookie / Authorization /
Sec-WebSocket-Protocol combinations, expected `PrincipalKind` and
`Scope`. Includes "cookie wins over api token if both present".

- `TestUplinkRejectsCookie`: cookie principal at `/uplink` → reject.
- `TestClient_FilterByOwner`: two sessions for two users; cookie user
  sees only their own.
- `TestClient_ShareCanAttachOnlyBoundSession`: share token bound to
  session A cannot attach session B.
- `TestClient_ShareReadOnlyEnforcement`: share principal sending `IN`
  is dropped by relay and (defense in depth) by desktop uplink.

### 9.3 CSRF and rate limiting

`csrf_test.go`:
- Missing `X-CSRF-Token` on a mutating route → 403.
- Wrong token → 403.
- Login / signup do not require it.
- Mux enumeration test: every non-GET/HEAD handler must be CSRF-wrapped.

`limits_user_test.go`:
- 11 failed logins from same `(IP, email-hash)` → 429.
- 6 signups same IP within an hour → 429.
- 11 failed invite consumptions same IP within an hour → 429.

### 9.4 Web push

- `dispatch_test.go`: command-finished event with `OwnerUserID=A` reaches
  user A's subscriptions only; user B's subscriptions are untouched.
- `web_push_http_test.go`: share principal's `POST /api/push/subscribe`
  returns 403.
- Legacy persistence test: starting with a `{tokenHash: [...]}` JSON file
  renames it to `.legacy-<ts>` and the registry is empty.

### 9.5 Desktop

Go:
- `desktop/uplink_e2e_test.go`: fake relay closes with reason
  `auth_invalid_token`; desktop emits Wails event `relay:auth-error`
  with `reason=invalid_token`.
- `desktop/config_test.go`: deserializing old config.json without
  `relay_paused` field yields `RelayPaused == false`.
- `desktop/app_test.go`: `SetUplinkPaused(true)` then `GetRelayConfig`
  returns URL/token unchanged and `Connected == false`.

Frontend (`desktop/frontend/src/components/SettingsRelay.test.ts`):
- Toggle pause off then on retains URL / token in the inputs.
- Paste `atk_…` → no warning; paste old format → red warning.
- Unconfigured (URL empty) state shows the top banner.

### 9.6 No-regression baseline

The following must continue to pass:
```
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./...
node --test web/*.test.mjs
cd desktop/frontend && npm run build
```
Particularly:
- `internal/proto` codec tests (red line #4 — wire unchanged).
- `desktop/uplink_e2e_test.go` lazy uplink protocol (red line #2).

### 9.7 Acceptance walkthrough

End-to-end manual checklist for release verification:

1. Start relay with `ATTERM_ADMIN_TOKEN`; open `/admin/`; create invite.
2. Sign up `user_a` with the invite; generate an API token at
   `/settings.html`.
3. Paste API token into desktop; observe status `connected as user_a@…`.
4. As `user_a`, list sessions in the web UI; attach one.
5. Generate a share link; open in private browser; attach as read-only;
   send `IN` → relay drops it.
6. Revoke share link; private browser reconnect fails.
7. Toggle desktop SettingsRelay OFF → uplink stops, inputs retained;
   toggle ON → uplink resumes immediately.
8. Disable `user_a` via `/admin/`; desktop uplink fails on reconnect
   with `Account disabled` banner.
9. Sign up `user_b`; verify `user_b` does not see `user_a`'s sessions.
10. Enable Web push notifications as `user_a`; trigger a long command;
    only `user_a`'s browser receives the push. `user_b`'s browser does
    not.

## 10. Known risks

| Risk | Mitigation |
|---|---|
| SQLite WAL on NFS / overlay volumes | README warns against placing `ATTERM_RELAY_CONFIG_DIR` on NFS; on `PRAGMA journal_mode=WAL` failure, fall back to truncate mode and log a warning |
| argon2id memory pressure under concurrent logins | `golang.org/x/sync/semaphore` caps concurrent argon2 to `runtime.NumCPU()`; bounded queue of 32, beyond which 503 |
| Credentials leaking via logs or panics | All credential types are `Secret[T]` newtypes; `String()` returns only prefix |
| Session vanished while share link is open | Documented behavior: share link → 404 |
| `web_sessions` table growth | Background goroutine deletes rows where `expires_at < now` every hour |

## 11. Future extensions (not in scope)

| Future | Where the seam is |
|---|---|
| Organizations / teams / roles | `Principal` already carries an unused string slot for org id; session ownership pivots from user to org |
| Email-based password reset | Add SMTP config, `password_resets` table |
| OAuth providers | New resolution step before cookie; new `oauth_identities` table |
| Multi-relay clustering | `internal/userstore.Store` is an interface, swap SQLite for Postgres; introduce Redis pub/sub for cross-node session fan-out |
| Audit log | New `audit_log` table; wrap mutating handlers in audit middleware |
| User-to-user session grants (not just share links) | New `session_grants` table with `(session_id, grantee_user_id, scope)` |
| Desktop multi-profile | `desktop/config.go` schema migration to `relays: []ProfileConfig` |
| iOS device-code flow | New `/api/auth/device-code` endpoint, reuses cookie store |

Reserved structural choices that aid future work without abstracting now:
- `Principal` carries optional `OrgID` (empty for this spec).
- `internal/userstore.Store` is an interface, not a struct.
- `api_tokens.last_used_at` updates flow through a buffered channel and
  a single committer goroutine; swapping the store does not move the
  call sites.
- CSRF middleware lives in its own subpackage (`internal/relay/csrfmw`).
- `AUTH_INFO` payload is UTF-8 JSON; new keys can be added without a
  protocol version bump.

## 12. Implementation order

Each item below is independently reviewable and intended to land as one
PR (or a small set of PRs). P8 may land before the rest.

```
P1   internal/userstore: schema, migrations, CRUD, unit tests
P2   internal/relay: resolveIdentity, Principal, CSRF middleware
P3   HTTP API: /api/auth/*, /api/me/*, admin invite/user pages
P4   /uplink, /agent, /client filtering by Principal; remove ATTERM_TOKEN
     and ATTERM_READ_ONLY_TOKENS
P4.5 internal/webpush: key by user_id; dispatch filtered by OwnerUserID;
     reject share principals; legacy-file rename
P5   /api/me/sessions/:id/shares + share-token attach flow
P6   web/ frontend: login / signup / settings pages, cookie flow
P7   AUTH_INFO frame + docs/spec/protocol.md
P8   desktop SettingsRelay redesign + RelayPaused toggle (may land first)
P9   desktop status row + auth-error feedback
P10  release notes, README, AGENTS.md, docs/spec/architecture.md updates
```
