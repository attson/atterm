# Mobile pairing via QR code (design)

Date: 2026-05-31
Status: Draft (design phase); pending implementation plan
Roadmap item: P1.6

## 1. Goal

Let an authenticated desktop user hand a fresh atterm mobile install its
relay URL and an API token by scanning a QR code, instead of typing the
relay URL and pasting a `atk_…` token by hand.

After this lands:

- A logged-in desktop user opens Settings → Relay → "Pair mobile",
  clicks a button, and sees a QR code.
- A first-run mobile install opens MobileSetup, taps "Scan QR", points
  the camera at the desktop screen, and the app jumps to its main
  screen connected to the same relay account.
- The credential handed to the mobile is a **new dedicated API token**
  bound to the same user (path C from brainstorming), recorded with
  `source = 'pairing'` for future audit; revoking it does not affect
  the desktop token.

Out of scope:

- Universal Link / App Link configuration (no AASA file, no signed
  app entitlements).
- QR scanning in the web (PWA) setup page — web users still type URL
  and token by hand.
- A "manage active tokens" UI (revoke individual `atk_…` from settings).
- A "paired devices list" on desktop (Presence is its own roadmap item).
- Multi-user / org pairing flows (sharing a session with another user).

## 2. Architecture

```
┌── desktop (Wails / Vue) ────────┐  ┌── relay (Go) ───────────────┐  ┌── mobile (Capacitor / Vue) ┐
│                                  │  │                              │  │                            │
│ SettingsRelay.vue                │  │ /api/pair/create  (auth'd)   │  │ MobileSetup.vue            │
│  └ PairingPanel.vue (new)        │──▶ pairing.CreateToken(uid, 5m) │  │  └ [Scan QR] button        │
│      • POST /api/pair/create     │  │   → returns {token, exp}     │  │      ↓                     │
│      • render QR (qrcode lib)    │  │                              │  │  @capacitor-mlkit/         │
│      • countdown / regen         │  │ pairing_tokens table (new)   │  │   barcode-scanning         │
│                                  │  │  hash, prefix, user_id,      │  │      ↓                     │
│ lib/api.ts                       │  │  created_at, expires_at,     │  │  PairingConsume.vue (new)  │
│  └ createPairingToken()          │  │  consumed_at                 │  │      • parse URL ?t=...    │
│                                  │  │                              │  │      • POST /api/pair/     │
│                                  │  │ /api/pair/consume (public)   │◀─┤        consume             │
│                                  │  │  → consume hash atomically   │  │      • save to             │
│                                  │  │  → mint atk_… (source=       │  │        localStorage[       │
│                                  │  │    'pairing')                │  │        'atterm.relay']     │
│                                  │  │  → return {relay_url,        │  │      • route to main       │
│                                  │  │     api_token, user}         │  │                            │
└──────────────────────────────────┘  └──────────────────────────────┘  └────────────────────────────┘
```

Three independent units, each testable on its own through a documented
HTTP contract.

## 3. Pairing token model

### 3.1 Token format

- Plaintext: `pair_` + base64url(32 random bytes) → ~47 chars.
- Storage: `sha256(plaintext)` (hex) in `pairing_tokens.token_hash`.
- Plaintext is returned to the desktop **once** at creation time and
  never persisted server-side, mirroring the `api_tokens` / invitation
  pattern in `internal/userstore/`.

### 3.2 Database schema

New table in the same SQLite database (`users.db`):

```sql
CREATE TABLE pairing_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL,           -- first 12 chars of plaintext, for audit only
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   INTEGER NOT NULL,        -- unix seconds
  expires_at   INTEGER NOT NULL,
  consumed_at  INTEGER                  -- NULL = unused
);
CREATE INDEX idx_pairing_tokens_user ON pairing_tokens(user_id);
```

Migration registered alongside existing `userstore` migrations.

### 3.3 `api_tokens` extension

Add one nullable column to record where each token came from:

```sql
ALTER TABLE api_tokens ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
```

Values: `'manual'` (existing rows + future web/CLI mints) and
`'pairing'` (tokens created by `/api/pair/consume`). No UI consumes
this yet; it is a forward-compatible audit column.

### 3.4 Lifecycle rules

- TTL: **5 minutes**, set at creation.
- Single use: `consume` atomically transitions `consumed_at` from NULL
  to `now()`. Concurrent consumers race — exactly one wins, the rest
  receive `pair_invalid`.
- Errors are deliberately undifferentiated to the caller: invalid,
  expired, and already-consumed all return the same HTTP 404 + JSON
  body `{"code":"pair_invalid"}` to prevent oracle attacks. Server-side
  logs may distinguish.

## 4. Relay HTTP API

### 4.1 `POST /api/pair/create`

Mints a new pairing token for the authenticated user.

- **Auth**: standard `requireUser` middleware (existing Bearer or
  session cookie). Returns 401 if not logged in.
- **Request body**: empty (future-proofed for `ttl_seconds`, `note`,
  but unused in v0.4).
- **Response 200**:
  ```json
  {
    "token": "pair_AbCd…",
    "expires_at": 1748689200,
    "qr_url": "https://relay.example.com/pair?t=pair_AbCd…"
  }
  ```
  `qr_url` is computed server-side from the request's `Host` header
  with `X-Forwarded-Proto` respected when running behind a reverse
  proxy, so the desktop never has to guess the externally reachable
  scheme/host. The same derivation feeds `relay_url` in the consume
  response (§4.2) — one helper, one source of truth.
- **Rate limit**: per-user, 10 creates / minute (cheap; not a hot
  path).

### 4.2 `POST /api/pair/consume`

Exchanges a pairing token for credentials.

- **Auth**: **none**. The pairing token itself is the bearer credential.
  This is the same trust model as OAuth Device Code Flow, GitHub mobile
  device linking, and Signal/WhatsApp device pairing.
- **Request body**:
  ```json
  { "token": "pair_AbCd…" }
  ```
- **Response 200**:
  ```json
  {
    "relay_url": "https://relay.example.com",
    "api_token": "atk_XyZw…",
    "user": { "id": 7, "name": "Alice", "email": "alice@example.com" }
  }
  ```
- **Response 404** (token invalid, expired, or already consumed):
  ```json
  { "code": "pair_invalid" }
  ```
- **Rate limit**: 10 attempts / minute / IP (defense in depth against
  online guessing; the token's 256-bit entropy already makes brute
  force impractical).

`relay_url` in the response is the relay's own canonical public URL
(same source as `qr_url` host above). Mobile uses it as the value to
store in `atterm.relay.url`, decoupling the saved config from whatever
host the mobile happened to hit.

### 4.3 Internal flow inside consume

1. Look up `token_hash = sha256(plaintext)` in `pairing_tokens`.
2. Reject if row missing, `expires_at < now`, or `consumed_at != NULL`.
3. Atomically `UPDATE pairing_tokens SET consumed_at = now() WHERE
   id = ? AND consumed_at IS NULL`. If `rowsAffected = 0`, lost the
   race → reject.
4. Generate a new API token via existing `userstore.CreateAPIToken(
   userID, source='pairing')`.
5. Return the response.

## 5. Desktop changes (`desktop/frontend/`)

### 5.1 New component `PairingPanel.vue`

Embedded inside `SettingsRelay.vue` as a labeled section under the
existing relay configuration card. UI states:

- **Idle**: section title "Pair mobile device" + paragraph "Scan this
  code with atterm on your phone to connect it to the same account."
  + `[ Generate QR code ]` button.
- **Active**: QR image (240×240, `<img :src="dataUrl">`), `pair_…`
  token shown below in a small monospaced font with a copy button (for
  the rare case where camera fails and user falls back to manual),
  countdown text "Expires in 4:32", `[ Regenerate ]` button.
- **Expired**: QR dimmed, countdown shows "Expired", regenerate
  button highlighted.
- **Error**: inline alert with the relay error message, reuses
  existing notification helper.

Countdown uses `setInterval(1s)` driven by `expires_at - now()`. State
discarded when the settings panel closes — no persistence.

### 5.2 API wrapper

In `desktop/frontend/src/lib/api.ts`:

```ts
export function createPairingToken(): Promise<{
  token: string;
  expires_at: number;
  qr_url: string;
}> {
  // Wails binding (App.CreatePairingToken in desktop/app.go) — the Go side
  // holds the relay URL and API token in cfgStore and signs the request,
  // mirroring the existing FetchRelayMe pattern.
  return bindings().CreatePairingToken();
}
```

### 5.3 QR rendering

Add dev dependency: `qrcode@^1.5` (≈ 50 kB minified, pure JS, no
native). Render via:

```ts
import QRCode from 'qrcode';
const dataUrl = await QRCode.toDataURL(qr_url, { width: 240, margin: 1 });
```

### 5.4 Wails binding

A new Wails binding `App.CreatePairingToken` is added to
`desktop/app.go`, following the pattern set by `App.FetchRelayMe`. It
reads the relay URL and API token from the existing config store,
calls `POST /api/pair/create` with a Bearer header, and returns the
JSON response to the renderer. No other Go code is changed.

## 6. Mobile changes (`desktop/frontend/src/mobile/`)

### 6.1 New dependency

`@capacitor-mlkit/barcode-scanning` (iOS + Android), wired into the
Capacitor config. Camera permission strings added to
`mobile/ios/App/App/Info.plist` (`NSCameraUsageDescription`).

### 6.2 `MobileSetup.vue` updates

The current single-form UI grows a prominent primary action:

```
┌─────────────────────────────┐
│  Connect to atterm relay    │
│                             │
│  [   Scan QR code   ]       │  ← new primary button
│                             │
│  ── or enter manually ──    │
│                             │
│  Relay URL: [____________]  │  ← existing fields, kept
│  Token:     [____________]  │
│  [ Allow insecure ] [ ] no  │
│  [ Connect ]                │
└─────────────────────────────┘
```

All copy goes through the existing i18n keys (PR #84 zh/en); the spec
uses English placeholders for clarity.

Tapping "Scan QR" calls the barcode plugin. On a successful scan the
view delegates to a new step (`PairingConsume.vue`).

### 6.3 New view `PairingConsume.vue`

Receives the scanned URL string. Steps:

1. Parse the URL. Reject if scheme is not `https` (or `http` plus the
   user explicitly enabled "Allow insecure" in setup), if the host is
   empty, or if `t=` query param is missing.
2. Show a "Pairing…" spinner.
3. `POST <scanned_origin>/api/pair/consume { token: t }`.
4. On 200: write to `localStorage['atterm.relay']` via the existing
   `platform.relay.save()` helper (`capacitor.ts:36`), reusing
   whatever defaults the manual flow already applies for
   `allow_insecure_relay`, `remote_permission`, and `connected`. The
   pairing path only contributes `url` and `token`; it never changes
   the other fields' defaults.
   Navigate to the mobile home route. Show a 1-line toast "Connected
   as <user.name>".
5. On 404 `pair_invalid`: show an error card with two CTAs — "Scan
   again" and "Enter manually" (collapses back to the manual form
   with whatever fields the user had typed preserved).
6. On network error: show "Couldn't reach relay" + retry / manual.

### 6.4 Permission scope after pairing

The new mobile token inherits the same scope as any other `atk_…` for
that user — the existing `remote_permission` setting governs whether
the mobile can drive or only view sessions. v0.4 does not differentiate
"mobile" tokens from "desktop" tokens at the permission layer; the
`source` column is informational only.

## 7. Errors and observability

### 7.1 Public error codes (returned to caller)

| Code            | HTTP | Meaning                                          |
|-----------------|------|--------------------------------------------------|
| `pair_invalid`  | 404  | Token unknown, expired, or already consumed.     |
| `rate_limited`  | 429  | Too many `create` or `consume` attempts.         |
| `unauthorized`  | 401  | `create` without a valid user session/token.     |

The intentional ambiguity of `pair_invalid` is the design (anti-oracle).
Mobile UI surfaces it as a single user-facing message.

### 7.2 Server-side logs

`pairing.go` emits structured log lines on each event:

- `pair_create user=<id> prefix=<12char> ttl=300s`
- `pair_consume_ok prefix=<…> user=<id> client_ip=<…>`
- `pair_consume_miss prefix=<…> reason=<expired|consumed|unknown>
  client_ip=<…>` (reason logged here even though the API hides it).

Existing relay metrics (Prometheus counters) gain
`atterm_pairing_create_total{result}` and
`atterm_pairing_consume_total{result}`.

## 8. Testing

### 8.1 `internal/userstore/pairing_test.go`

- create returns plaintext, never persists it
- create then consume returns the expected user
- consume twice → second call errors `ErrPairingInvalid`
- consume after TTL → errors
- consume of garbage / wrong-prefix string → errors
- concurrent consumes (`t.Parallel`, 50 goroutines) → exactly one
  succeeds, 49 fail
- create writes `api_tokens.source = 'pairing'` on the resulting
  `CreateAPIToken` call (verified through the userstore API, not
  raw SQL)

### 8.2 `internal/relay/pair_http_test.go`

End-to-end HTTP tests using the existing relay test harness:

- `POST /api/pair/create` without auth → 401
- with auth → 200 with token, `qr_url` host matches request `Host`
- `POST /api/pair/consume` with valid token → 200 with usable
  `api_token`; the returned `api_token` then authenticates as the
  expected user against `/api/me`
- consume the same token twice → second is 404 `pair_invalid`
- consume an unknown token → 404 `pair_invalid`
- rate limit on consume returns 429 after the 11th attempt in a
  minute from one IP

### 8.3 Frontend component tests

- `PairingPanel.vue` (desktop): mocks `createPairingToken()`, asserts
  idle → active → expired transitions and that the QR `<img>` `src`
  contains a non-empty data URL.
- `PairingConsume.vue` (mobile): mocks fetch and asserts each of the
  branches in §6.3, including the `localStorage` write.

### 8.4 Manual / smoke (documented in spec, not gated)

- Pair from a real Wails desktop to a real iOS Capacitor build,
  visually confirm a token round-trips and the home screen loads.

## 9. Migration and rollout

- The new table and column ship in one userstore migration.
- Default `source = 'manual'` keeps existing rows correct.
- No config flags required; pairing is enabled as soon as the new
  endpoints exist.
- Mobile users on an older app build continue to see only the manual
  form — the new button only ships with the updated mobile bundle.

## 10. Non-goals revisited

This spec deliberately does not:

- Build any per-token revocation UI. The `source` column is groundwork
  only; the existing relay token table has no UI today, and adding one
  is its own roadmap effort.
- Reuse the desktop's existing `atk_…` (path B from brainstorming).
  Path C was chosen for simpler protocol and independent revocation.
- Use the desktop's uplink WebSocket for pairing. HTTP suffices and
  keeps the surface area small.
- Cover the web-PWA QR scanning path (the existing manual form remains
  the supported flow for the browser build).
