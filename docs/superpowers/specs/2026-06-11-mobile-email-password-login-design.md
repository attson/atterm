# Mobile Email/Password Login (replace API token entry)

**Status**: design / approved-by-user
**Date**: 2026-06-11
**Owner**: attson

## Background

Today the mobile setup screen (`desktop/frontend/src/mobile/MobileSetup.vue`) offers two
paths:

1. **QR scan** — scan a `pair_xxx` URL from a logged-in desktop, exchange it for a
   session token via `POST /api/pair/consume`, store the token in Keychain.
2. **Manual** — paste a relay URL plus an API token (`atk_…`) and store it
   directly.

The "Relay Auth: Remove Token Mode" spec (#76, #77, [relay-auth-token-removal])
already shipped: there is no more `atk_…` token to paste. The manual field is
a dead end — it accepts any string and the first authenticated call fails with
`relay_unauthorized`.

The desktop side (`SettingsRelay.vue`, PR #145) already uses email + password
via the Wails-bound `LoginRemoteRelay`, which POSTs `/api/auth/login` and
persists the returned `session_token` + `last_email` into the desktop config.

This change brings the mobile setup screen in line: replace the manual-token
input with email + password fields that drive the same `/api/auth/login`
endpoint, called over HTTP from Capacitor.

## Goals

- Mobile setup form fields: relay URL, email, password — matching the desktop
  Settings → Relay form.
- QR pairing path is **unchanged** — it already returns a `session_token` and
  works.
- On successful login, persist the session token + the last email + the
  password into the iOS Keychain so re-login is one tap.
- A "hard logout" from `MobileSettings`: call `POST /api/auth/logout` to revoke
  the server-side session, clear the local token, **keep** URL/email/password
  pre-filled for next login.
- No backend changes. `/api/auth/login` and `/api/auth/logout` already exist
  and are covered by `internal/relay/auth_http_test.go`.

## Non-goals

- Signup on mobile (no invite-code form). Account creation happens on the
  desktop or the relay's web UI.
- Auto-relogin on token expiry. When the relay returns 401, the user is sent
  back to the setup screen with a pre-filled form and taps Login explicitly —
  no silent token refresh.
- Biometric (Face ID) gate before the saved password is used. Keychain's
  default access class is the only protection.
- "Forget saved credentials" button. Wipe via app uninstall.
- Refactoring `SettingsRelay.vue` into a shared component (rejected approach B).
- Adding a Capacitor-side Go plugin for unified auth (rejected approach C).

## Architecture overview

```
┌──── MobileSetup.vue ───────────────────────────────┐
│ inputs: scheme + host, email, password,            │
│         allowInsecure (http:// only)               │
│ submit → platform.relay.login(url, email,          │
│                                password,           │
│                                allowInsecure)      │
└─────────────────────┬──────────────────────────────┘
                      │
              capacitor.ts relay.login
                      │
                      ▼
        POST {url}/api/auth/login
        body: {email, password}
                      │
                      ▼
        ← 200 {session_token, expires_at, user{id,email}}
                      │
                      ▼
   Keychain writes (single secureStorage backend):
     STORAGE_KEY      ← RelayConfig{url, token=session_token,
                                    session_expires_at,
                                    allow_insecure_relay,
                                    remote_permission: 'full',
                                    last_email: user.email,
                                    connected: false}
     PASSWORD_KEY     ← password   (new key)
                      │
                      ▼
       fetchMe() → identity confirmed → emit('connected')
```

QR pairing path is untouched: `PairingConsume.vue` still calls
`platform.relay.consumePairing`, which calls `/api/pair/consume` and stores the
resulting session token. No password is persisted for the QR path.

## RelayBridge interface changes

`desktop/frontend/src/platform/types.ts` adds two optional methods to
`RelayBridge`:

```ts
export interface RelayBridge {
  // ... existing fields ...
  /** Mobile-only. Login with email + password, persist the returned session
   *  token + email + password to Keychain. Throws codes:
   *  'invalid_credentials' | 'rate_limited' | 'invalid_email' |
   *  'cannot_reach_relay' | string  (other HTTP errors include status). */
  login?(url: string, email: string, password: string, allowInsecure: boolean): Promise<void>
  /** Mobile-only. POST /api/auth/logout (best-effort, network errors ignored)
   *  and clear the local session token. Keeps URL + email + password. */
  logout?(): Promise<void>
}
```

`consumePairing` and `setUplinkPaused` are already optional / platform-specific;
`login` and `logout` follow the same precedent. Wails does not implement them
(desktop's email/password lives in `SettingsRelay.vue` and goes through the
existing `LoginRemoteRelay` Wails binding — unchanged).

## Capacitor implementation

`desktop/frontend/src/platform/capacitor.ts`:

- New constant `PASSWORD_KEY = 'atterm.relay.password'` — separate Keychain
  entry to avoid bloating `RelayConfig` (which is a wire type also used by
  Go-side code via `wailsjs/go/models`).
- `relay.load()` is unchanged in shape — it still returns `RelayConfig`. The
  saved password is read via a new `relay.loadSavedPassword()` helper exposed
  on `RelayBridge` (a thin wrapper over `secureStorage.get(PASSWORD_KEY)`).
  Exposing it on the bridge keeps tests platform-agnostic and avoids importing
  `secureStorage` directly inside `MobileSetup.vue`.
- `relay.login(url, email, password, allowInsecure)`:
  1. POST `{base}/api/auth/login` with `{email, password}`, `credentials:'omit'`.
  2. On 401 → throw `Error('invalid_credentials')`.
  3. On 429 → throw `Error('rate_limited')`.
  4. On other non-2xx → throw `Error('http_'+status)`.
  5. On network failure → throw `Error('cannot_reach_relay')`.
  6. On success: parse `{session_token, expires_at, user}`, write to Keychain:
     - `STORAGE_KEY` ← new `RelayConfig` with `token=session_token`,
       `session_expires_at`, `last_email=user.email`,
       `allow_insecure_relay=allowInsecure`, other fields preserved or
       defaulted.
     - `PASSWORD_KEY` ← `password`.
- `relay.logout()`:
  1. Read current config; if `token` present, POST `{base}/api/auth/logout`
     with `Authorization: Bearer <token>`. Ignore non-2xx and network errors.
  2. Write a new `RelayConfig` with `token = ''` and `session_expires_at = 0`,
     preserving every other field of the existing `RelayConfig` — notably
     `url`, `last_email`, `allow_insecure_relay`, `remote_permission`. Do NOT
     touch `PASSWORD_KEY`.

## MobileSetup.vue changes

- Remove the `relay-token` input field and the `mobile.apiToken*` strings it
  references.
- Add an email field (`type="email"`, `autocomplete="username"`, `data-testid="relay-email"`).
- Add a password field with a show/hide toggle button, mirroring
  `SettingsRelay.vue`'s pattern (`type="password" | "text"`,
  `autocomplete="current-password"`, `data-testid="relay-password"`).
- `onMounted`: load `cfg` as today, pre-fill URL + `cfg.last_email` into email
  field, and pre-fill password from the new Keychain key.
- `onConnect()`:
  1. Validate URL via existing `validateRelayBase`.
  2. Require non-empty email and password (error keys: `mobile.emailRequired`,
     `mobile.passwordRequired`).
  3. Call `platform.relay.login(url, email, password, allowInsecure)`.
  4. On `invalid_credentials` → set `error.value = t('mobile.invalidCredentials')`.
  5. On `rate_limited` → `t('mobile.rateLimited')`.
  6. On `cannot_reach_relay` or other → reuse `mobile.cannotReachRelay`.
  7. On success: `await platform.relay.fetchMe()` then `emit('connected')`.
- QR scan section (button + PairingConsume) unchanged.
- The `reason='token_invalid'` banner stays — when the relay 401s during a
  list call, `MobileSessionList` already emits `token-invalid` and `MobileApp`
  shows `MobileSetup` with the banner. With email + password pre-filled, the
  user just taps Login.

## MobileApp.vue / MobileSettings.vue changes

- `MobileSettings.vue` already has a logout button that emits `logout`.
  No UI change.
- `MobileApp.onLogout()`: before resetting view state, `await
  platform.relay.logout()`. If `logout` is undefined (e.g., Wails build that
  doesn't ship this UI), skip — the optional method is the right shape.
- The existing comment at MobileApp.vue:95 ("preserves the saved config") is
  rewritten to reflect the new semantics: token is cleared, URL/email/password
  are kept.

## i18n strings

Added to `desktop/frontend/src/i18n/messages/zh-CN.ts` and `en.ts`:

| Key | zh-CN | en |
|---|---|---|
| `mobile.email` | 邮箱 | Email |
| `mobile.password` | 密码 | Password |
| `mobile.passwordShow` | 显示密码 | Show password |
| `mobile.passwordHide` | 隐藏密码 | Hide password |
| `mobile.emailRequired` | 请输入邮箱 | Email required |
| `mobile.passwordRequired` | 请输入密码 | Password required |
| `mobile.invalidCredentials` | 邮箱或密码错误 | Invalid email or password |
| `mobile.rateLimited` | 操作过于频繁，请稍后再试 | Too many attempts, please try later |
| `mobile.loginButton` | 登录 | Log in |

Deleted (or marked as no-longer-referenced — repo doesn't gate on i18n
unused-key drift today):

- `mobile.apiToken`
- `mobile.apiTokenRequired`
- `mobile.apiTokenInvalid`

`mobile.tokenInvalidBanner` stays — the banner copy ("Session expired, please
log in again") is still accurate.

## Files touched

| File | Change |
|---|---|
| `desktop/frontend/src/platform/types.ts` | add optional `login`, `logout`, `loadSavedPassword` on `RelayBridge` |
| `desktop/frontend/src/platform/capacitor.ts` | implement them; new `PASSWORD_KEY` constant; new helper for password read |
| `desktop/frontend/src/platform/wails.ts` | leave `login` / `logout` unimplemented (optional methods); no code change |
| `desktop/frontend/src/mobile/MobileSetup.vue` | remove token field; add email + password fields with show/hide; rewire `onConnect` |
| `desktop/frontend/src/mobile/MobileApp.vue` | `await platform.relay.logout()` in `onLogout()`; update comment |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | new keys, drop unused ones |
| `desktop/frontend/src/i18n/messages/en.ts` | same |
| `desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts` | rewrite for new fields and login flow |
| `desktop/frontend/src/platform/__tests__/capacitor.test.ts` | add login + logout round-trip + error cases |
| `desktop/frontend/src/mobile/__tests__/MobileSettings.test.ts` | extend: logout calls `platform.relay.logout()` |
| (if needed) `desktop/frontend/src/mobile/__tests__/MobileApp.test.ts` | extend onLogout flow |

Not touched:

- Backend Go code (`internal/relay/auth_http.go`, etc.) — endpoints already exist.
- `desktop/frontend/src/components/SettingsRelay.vue` — desktop email/password
  flow unchanged.
- `desktop/frontend/src/mobile/PairingConsume.vue` — QR pairing unchanged.
- `LoginRemoteRelay` Wails binding (`desktop/app.go`, `lib/api.ts`) — unchanged.

## Security notes

- **Password persisted in Keychain.** This is a deliberate trade-off for mobile
  ergonomics (typing passwords on a phone is friction). iOS Keychain protects
  against off-device extraction via the device's secure enclave for the default
  access class. On a jailbroken / compromised device the password can be read,
  same as for any password manager. The desktop counterpart (`SettingsRelay.vue`)
  intentionally keeps the password in memory only and clears on success — we are
  not changing that.
- **Logout calls `/api/auth/logout` best-effort.** If the network is gone the
  local token is still wiped so the user is fully signed out from the device's
  point of view. The server-side session lingers until the relay's
  `DefaultSessionTTL` expires.
- **No autoplay-on-launch login.** If we silently POST `/api/auth/login` on
  cold start with the stored password, a stolen unlocked phone yields immediate
  relay access without any "did you mean to log in" prompt. Requiring an
  explicit Login tap (with pre-filled fields) preserves the same friction the
  user is used to.

## Tests

New / updated:

- **`MobileSetup.test.ts`**:
  - Renders email + password inputs (not the token input).
  - Pre-fills email from `cfg.last_email` and password from `PASSWORD_KEY`.
  - Calls `platform.relay.login` with the entered values on submit.
  - Maps `invalid_credentials` → `mobile.invalidCredentials` error copy.
  - Maps `rate_limited` → `mobile.rateLimited`.
  - Banner shown when `reason='token_invalid'`.
- **`capacitor.test.ts`**:
  - `relay.login` POSTs to `/api/auth/login` with `credentials:'omit'`, parses
    body, writes Keychain.
  - `relay.login` on 401 → throws `'invalid_credentials'`.
  - `relay.login` on 429 → throws `'rate_limited'`.
  - `relay.logout` POSTs to `/api/auth/logout` with Bearer header, wipes token
    in `RelayConfig`, **keeps** password.
  - `relay.logout` swallows network errors but still wipes local token.
- **`MobileSettings.test.ts`** / **`MobileApp.test.ts`**:
  - Logout click triggers `platform.relay.logout()`.
  - After logout, navigating to setup shows pre-filled email + password.

Backend: no new tests — `auth_http_test.go` already covers `/api/auth/login`
and `/api/auth/logout`.

## Suggested PR breakdown (handed to writing-plans)

Single PR is appropriate — the change is contained in `frontend/src/platform/`
and `frontend/src/mobile/`. Suggested commit sequence inside the PR:

1. Platform interface: add optional `login` / `logout` / `loadSavedPassword`;
   Capacitor implements via HTTP; Wails skips. + unit tests.
2. `MobileSetup.vue`: swap token field for email + password + show/hide
   toggle; wire `onConnect` to `platform.relay.login`; pre-fill from Keychain.
   + unit tests + i18n strings.
3. `MobileApp.vue`: call `platform.relay.logout()` in `onLogout`. + test.
4. Delete unused i18n keys (`mobile.apiToken*`).

Verification before completion: `npm run -w desktop/frontend test` green, plus
manual run of the iOS Capacitor build (the iOS app bundles `dist-capacitor`,
per [[feedback_mobile_app_source_root]]).

## Open questions

None at design time.
