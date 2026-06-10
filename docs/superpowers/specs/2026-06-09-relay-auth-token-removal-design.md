# Relay Auth: Remove Token Mode, Unify on Session Token

**Status**: design / approved-by-user
**Date**: 2026-06-09
**Owner**: attson

## Background

Relay currently supports three credential paths layered on top of each other:

1. **Email + password login** → `Set-Cookie: atterm_session` (30 days, HttpOnly). Used by web browsers.
2. **Pairing code** (`pair_xxx`, 5 min one-shot) → consumed by a new device to mint a long-lived API token. Used by the QR scan flow on mobile.
3. **API token** (`atk_xxx`, indefinite) → carried via `Authorization: Bearer ...` or `Sec-WebSocket-Protocol: atterm-token.<token>`. Used by `cmd/atterm-agent`, the desktop uplink, and the mobile app for all HTTP/WS calls.

In addition there is a parallel **global share-secret** layer (`Config.Token` / `ReadOnlyTokens` / `ReadOnlyTokenHashes`) — a deploy-time secret that gates the WS endpoints `/agent`, `/uplink`, `/client`, `/client-sessions`, and `/api/sessions` regardless of user identity. Today's clients pass it in the same `Authorization` header they would use for an `atk_` token, which conflates the two systems.

The product goal: **only email + password may grant relay access; remove the "token mode" entirely**. After the change, every credential the clients hold is a short-lived, revocable session token tied to a specific logged-in user. Pairing codes survive — but only as "old device authenticates a new device's session", not as a long-lived token vending machine.

## Goals

- Single rule: anyone reaching relay has logged in with email + password (directly, or transitively via a pairing code minted by someone who did).
- Three clients (desktop, web, iOS) use the same transport: `Authorization: Bearer <session_token>` (HTTP) or `Sec-WebSocket-Protocol: atterm-token.<session_token>` (browser WS).
- No cookies. No CSRF middleware. No `atk_` table. No global share-secret. No `cmd/atterm-agent`.
- Self-hosted single-user project — no backward compatibility for existing tokens or sessions. Operator drops the old database and redeploys.

## Non-goals

- Cross-tab token sync, refresh tokens, OAuth, SSO.
- Token rotation policy beyond the existing 30-day session lifetime.
- Migration of any existing `atk_` token or `web_sessions` row to the new schema.

## Architecture overview

```
┌──── Desktop App / Web Browser / iOS App ────┐
│ 1. POST /api/auth/login {email,password}    │
│ 2. ← 200 {session_token, expires_at, user}  │
│ 3. Client persists session_token            │
│    · desktop: encrypted config (Wails)      │
│    · web:     localStorage                  │
│    · iOS:     Keychain                      │
│ 4. All subsequent HTTP / WS:                │
│    HTTP → Authorization: Bearer <token>     │
│    WS   → Sec-WebSocket-Protocol:           │
│           atterm-token.<token>              │
└──────────────────────────────────────────────┘
                  ↕
                relay
  - requireSession middleware:
      sha256(token) → look up sessions.id_hash
      verify expires_at, not revoked
      inject *User into request context
```

Pairing semantics shift from "mint a long-lived token" to "old device hands a new device a fresh session":

```
old device (logged in) → POST /api/pair/create → pair_xxx (5 min one-shot)
new device (no creds)  → POST /api/pair/consume {token: pair_xxx}
                       ← 200 {session_token, expires_at, user, relay_url}
```

Removed entirely: `api_tokens` table + `/api/me/tokens*` + CSRF middleware + `users.csrf_secret` + `pairing_tokens.source` + `Set-Cookie atterm_session` + `Config.Token` / `ReadOnlyTokens` / `ReadOnlyTokenHashes` + `cmd/atterm-agent/` + `internal/agent/`.

## HTTP API changes

### Kept, with signature changes

| Endpoint | Current | After |
|---|---|---|
| `POST /api/auth/login` | in `{email, password}`; out `Set-Cookie atterm_session` + body `{user}` | out: **no Set-Cookie**, body `{session_token, expires_at, user}`. `session_token` is the plaintext id of a row in `sessions` (formerly `web_sessions`). |
| `POST /api/auth/logout` | reads cookie → revokes | reads `Authorization: Bearer <token>` → revokes that session row |
| `POST /api/auth/register` / `POST /api/auth/setup` | first-time bootstrap | also returns `{session_token, expires_at, user}` |
| `POST /api/pair/create` | requires cookie or Bearer | requires Bearer session_token; response unchanged |
| `POST /api/pair/consume` | out `{api_token: atk_xxx, relay_url, user}` | out **`{session_token, expires_at, relay_url, user}`** — consuming a pair code is equivalent to logging in |
| `GET /api/me`, `me_sessions_http.go`, `me_delete_http.go` | cookie | Bearer session_token (response shapes unchanged) |

### Deleted

| Endpoint | File |
|---|---|
| `POST /api/me/tokens` | `auth_http.go` (~line 392) |
| `GET /api/me/tokens` | same |
| `DELETE /api/me/tokens/:id` | same |
| Any CSRF endpoints | `csrfmw.go` and mount sites |

### Middleware

A single new `requireSession` middleware replaces today's `authorize*` family:

```go
// internal/relay/auth.go (rewritten)
func (s *Server) requireSession(h http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        tok := tokenFromRequest(r) // Bearer or atterm-token.* subprotocol
        if tok == "" {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        sess, user, ok := s.store.LookupSession(r.Context(), tok)
        if !ok || sess.ExpiresAt.Before(time.Now()) {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        ctx := context.WithValue(r.Context(), userCtxKey{}, user)
        h(w, r.WithContext(ctx))
    }
}

func UserFromContext(ctx context.Context) (*store.User, bool) { ... }
```

Token extraction (`tokenFromRequest`, `tokenFromSubprotocol`) is unchanged — it already supports all three transport encodings (`Authorization: Bearer ...`, `atterm-token.<raw>`, `atterm-token-b64.<base64url>`).

### Error responses

- 401 returns `{"error": "unauthorized"}`. Clients see 401 → wipe stored session_token → redirect to login.
- Pair consume failures remain 409 (already consumed) / 410 (expired). Front-end copy unchanged.

### Route ↔ middleware mapping

| File:line | Current | After |
|---|---|---|
| `server.go:310` `/agent` | `authorizeWithScope(r, s.cfg.Token, s.cfg.ReadOnlyTokens)` | `s.requireSession`; user_id attached to agent connection metadata |
| `server.go:358` `/uplink` | same | `s.requireSession`; session owner = user |
| `server.go:400` `/client` | `authorizeClientWebSocketWithConfig` | `s.requireSession`; viewer/driver logic uses user identity |
| `server.go:439` `/client-sessions` | same | `s.requireSession` |
| `server.go:476` `/api/sessions` | `authorizeClientWithConfig` | `s.requireSession` |
| `/api/auth/login`, `/api/auth/register`, `/api/auth/setup`, `/api/pair/consume`, `/healthz`, `/version` | public | public (no middleware) |

Viewer/driver behavior (PR #76 / #77) is unaffected — it derives roles from user identity, never from a read-only token.

## Database schema (no migration, fresh deploy)

The operator stops the old relay, deletes its database file, and starts the new binary. New schema goes in directly — no `ALTER TABLE` / `DROP TABLE` code.

Target schema:

- `users(id, email, password_hash, created_at, ...)` — **no `csrf_secret`**
- `sessions(id_hash, user_id, expires_at, last_seen_at, user_agent, ...)` — same shape as the old `web_sessions`, just renamed
- `pairing_tokens(token_hash, user_id, expires_at, consumed_at)` — **no `source`** column
- **No `api_tokens` table**

`internal/userstore/` Go code follows: rename `WebSession` → `Session`, rename `CreateWebSession` / `LookupWebSession` / `RevokeWebSession` accordingly, delete `LookupAPIToken` / `CreateAPIToken` / `ListAPITokens` / `RevokeAPIToken`.

## Client changes

### Desktop (Wails + Vue)

| File | Change |
|---|---|
| `desktop/config.go:39-40` | rename field `RelayToken` → `RelaySessionToken`; storage location unchanged |
| `desktop/app.go:978-1006` (`FetchRelayMe`) | unchanged transport (`Authorization: Bearer ...`); token contents are session_token now |
| `desktop/app.go:1019-1047` (`CreatePairingToken`) | unchanged transport |
| `desktop/app.go:254-280` (`SetRelayConfig`) | input field renamed |
| `desktop/uplink.go:182` | `hdr["Authorization"] = []string{"Bearer " + u.token}` unchanged; suggest renaming `u.token` → `u.sessionToken` |
| **new**: Login Wails method | desktop Settings dialog exposes "remote relay email/password login"; calls `/api/auth/login`, writes `session_token` to config |
| **delete** | any UI that accepts a pasted long-lived token; any CSRF code |

### Web (`web/src/`, Vue + Naive UI)

| File | Change |
|---|---|
| `web/src/shared/api/auth.ts:8` | no longer relies on `Set-Cookie`; reads `session_token` from response body, writes to localStorage |
| `web/src/shared/api/client.ts:48-97` (`apiFetch`) | **remove web/mobile branch**: always read session_token from storage, always add `Authorization: Bearer <token>` |
| `web/src/shared/api/client.ts:58-74` | `credentials: 'same-origin'` → `'omit'` |
| `web/src/shared/api/client.ts:20-28` | **delete** CSRF token cache and `X-CSRF-Token` header injection |
| `web/src/shared/api/relay-config.ts` | localStorage key `relay_session_token`; expose `get/set/clear` helpers |
| **new** 401 interceptor | any 401 → clear localStorage → redirect to `/login` |
| Browser WS connect site | use `Sec-WebSocket-Protocol: atterm-token.<token>` (browsers cannot set the Authorization header on WS) |

### iOS / Capacitor

| File | Change |
|---|---|
| `desktop/frontend/src/platform/secureStorage.ts:37-45` | Keychain key renamed to `session_token`; drop any `relay_token` / `api_token` aliases |
| `desktop/frontend/src/platform/capacitor.ts:102-115` (`consumePairing`) | response type `{api_token}` → `{session_token, expires_at, user, relay_url}`; persist to Keychain |
| `desktop/frontend/src/platform/capacitor.ts:96,133` | `Authorization: Bearer ${cfg.token}` unchanged (token contents are session_token) |
| `desktop/frontend/src/platform/capacitor.ts:97,108,134` | `credentials: 'omit'` unchanged |
| `desktop/frontend/src/mobile/PairingConsume.vue:38-46` | parse `session_token` field and store it |
| Old Keychain key migration | **none** — operator redeploys, mobile user re-scans pair code |

### Cross-client

- WS upgrade keeps the two transports it already supports (Bearer header for native clients, `Sec-WebSocket-Protocol: atterm-token.<token>` for browsers).
- Any "API token" / "long-lived token" / "atk_" wording in UI / i18n / component names → "session token".

## Desktop App local-relay UX change

Removing `Config.Token` is the one user-visible behavior change beyond the credential type:

- Previously the desktop app auto-injected a random share-secret into its own local relay at startup, so the app worked without any login.
- After this change, the local relay requires a session_token too. First launch flow:
  1. desktop app pops a "set up your local atterm account" dialog (email + password)
  2. calls the local relay's `/api/auth/setup` (or `register`) → receives `session_token` → writes it to the Wails config
  3. subsequent launches read the token from config; if expired, the login dialog re-opens
- CI / scripted-bootstrap: keep the existing `ATTERM_BOOTSTRAP_ADMIN_EMAIL` / `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` env vars (`bootstrap_admin.go`). Extend the relay startup so when those are present it auto-creates the admin user and prints (or writes) a one-time bootstrap session_token.

## Deletion checklist

Repo / binaries:

- `cmd/atterm-agent/` directory
- `internal/agent/` directory
- any `--token` flag handling in `Dockerfile.relay`

Relay backend:

- `internal/relay/csrfmw.go` (whole file)
- in `internal/relay/auth.go`: delete `authScope`, `authorize`, `authorizeClient`, `authorizeWithScope`, `authorizeClientWithConfig`, `authorizeClientWebSocketWithConfig`, `authorizeWithScopeAndHashes`, `authorizeWithScopeAndHashesFromToken`, `tokenEqual`, `tokenMatchesHash` — keep `tokenFromRequest`, `tokenFromRequestNoQuery`, `tokenFromSubprotocol`
- in `internal/relay/auth_http.go`: delete `/api/me/tokens` (POST/GET/DELETE) handlers and the `CSRFToken(...)` line at ~332
- `Config.Token`, `Config.ReadOnlyTokens`, `Config.ReadOnlyTokenHashes` struct fields
- `cmd/atterm-relay/main.go`: matching `--token` / `--readonly-tokens` flags and env vars
- `internal/userstore/`: `APITokens` table / struct / CRUD; `csrf_secret` field; `pairing_tokens.source` field

Frontend:

- "Paste API token" inputs, "Manage tokens" pages, related i18n strings
- `web/src/shared/api/client.ts` CSRF cache and `X-CSRF-Token` injection
- any `relay_token` / `apiToken` / `atk_` literal → renamed to `session_token` / `sessionToken`

Docs:

- `AGENTS.md:14-15` (atterm-agent entry) and routing table at line 126
- `README.md` references to `atterm-agent` and `ATTERM_TOKEN`
- `docs/spec/architecture.md`, `docs/spec/protocol.md`: token-mode sections rewritten as session-token sections
- `scripts/dev.sh:8` `ATTERM_TOKEN=dev go run ./cmd/atterm-agent` example

## Tests

New / rewritten:

- `internal/relay/auth_test.go` — rewritten for `requireSession`: all three token transports, expired session, revoked session, missing token → 401
- `internal/relay/auth_http_test.go` — `POST /api/auth/login` response includes `session_token` / `expires_at` / `user`; no Set-Cookie header
- `internal/relay/pair_http_test.go` — consume returns `session_token` instead of `api_token`
- `internal/userstore/` — `Session` CRUD (re-uses existing web_sessions tests, renamed)

Deleted:

- `csrfmw_test.go`
- `me_*_test.go` cases for token management (keep session tests)
- tests for the deleted `authorize*` functions

Updated:

- every `internal/relay/*_test.go` that built a `Config{Token: "..."}` → build a real user + session and put the token in the request header instead
- web `tests/contract/*.mjs` and Wails frontend tests follow suit

## Documentation

- `README.md` "connect to relay" rewritten: register → login → session token / pairing code
- `AGENTS.md`: drop `cmd/atterm-agent` entry and its routing-table row
- `docs/spec/protocol.md`: drop the `atk_` section, add a `session_token` section
- new short "first-time deploy bootstrap" section: `ATTERM_BOOTSTRAP_ADMIN_*` env vars seed the first user on a fresh database

## Suggested PR breakdown (handed to writing-plans)

1. **PR-1 backend auth rewrite**: new `requireSession` + delete `authorize*` + delete `csrfmw` + delete `api_tokens` + `/api/auth/login` returns body / no cookie + pair consume returns session_token + fresh schema
2. **PR-2 remove `cmd/atterm-agent` + `internal/agent/`**: small, independent
3. **PR-3 desktop client**: config field rename, login dialog, local-relay bootstrap
4. **PR-4 web client**: localStorage + Bearer + 401 interceptor + CSRF removal
5. **PR-5 mobile Capacitor**: Keychain key rename, pairing consume parses new fields

Each PR runs the relevant unit + contract tests. End-to-end behavior verified manually before release (per the project's "verification-before-completion" rule).

## Open questions

None at design time. The clarifications during brainstorming covered: scope (atk_ only or atk_ + share-secret), agent fate (delete), browser token storage (localStorage), mobile transport (Bearer over HTTPS not required for the JS layer), and migration approach (drop database, redeploy).
