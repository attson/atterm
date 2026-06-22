# Desktop Relay Password Persistence Design

## Goal

After the user logs into a relay from the desktop app, the password field in
`SettingsRelay.vue` is repopulated on subsequent launches so the user does not
have to retype the password when the session token expires or when they reopen
the form. Behavior matches the existing mobile (Capacitor) flow added in
`2026-06-11-mobile-email-password-login-design.md` and
`2026-06-01-mobile-secure-storage-design.md`.

## Approach

Persist the relay password through the existing `internal/safekeyring` package
in a separate keychain slot, mirroring the shape of `account_key_store.go`. On
login success the password is written; when the Settings → Relay form mounts it
is read back and prefilled. No silent re-login: the user still clicks
**Connect** to authenticate, exactly as on mobile.

The relay server is **not** modified. No new endpoints, no refresh token, no
token TTL change.

## Storage

New file `desktop/relay_password_store.go`, structurally a sibling of
`desktop/account_key_store.go`:

```go
// service: "com.atterm.relay-password.v1" + appdir.KeychainSuffix()
// account: relayOrigin + "|" + email
//   - when either relayOrigin or email is empty, an internal helper returns
//     "" and the load/save/clear functions become no-ops (load returns
//     ("", nil); save and clear return nil). Same pattern as
//     accountKeyAccount in account_key_store.go.

func loadRelayPassword(relayOrigin, email string) (string, error)
func saveRelayPassword(relayOrigin, email, password string) error
func clearRelayPassword(relayOrigin, email string) error
```

`relayOrigin` is normalized the same way as in `account_key_store.go`
(`strings.TrimRight(strings.TrimSpace(...), "/")`). The `.v1` suffix in the
service name leaves room to migrate the format later without colliding with old
entries.

Storage medium is whatever `safekeyring` resolves to: OS keychain on a signed
build, 0600 file in `appdir.ConfigDir()` on `wails dev` / unsigned builds /
tests. Inherits all of safekeyring's degradation behavior — no new logic.

## App.go Bindings

One new wails binding on `*App`:

```go
// LoadSavedRelayPassword reads the password persisted for the relay/email
// currently in cfgStore. Returns ("", nil) when nothing is stored, when
// RelayURL or RelayLastEmail is empty, or when the keychain entry is absent.
// Keychain errors are logged but surfaced as ("", nil) so the UI just shows
// an empty field.
func (a *App) LoadSavedRelayPassword() (string, error)
```

`LoginRemoteRelay` and `RegisterRemoteRelay` each gain a single new line,
placed after `cfgStore.Set(...)` has successfully written `RelayLastEmail` and
`RelaySessionUserID` (i.e. at the tail of the success path, after both
`setAccountKey` and the email persistence have completed):

```go
if err := saveRelayPassword(origin, email, password); err != nil {
    log.Printf("desktop: save relay password: %v", err)
}
```

A `saveRelayPassword` failure does **not** fail the login: the user already has
a valid session token and account_key; failing to cache the password just means
the next launch's password field is empty, not that the user lost access.

`SetRelayConfig` is **not** changed to clear old password entries when the URL
changes. The slot key is `origin + "|" + email`, so changing either field
simply addresses a different slot; stale entries for retired origins are
harmless and left in place.

There is no `ForgetSavedRelayPassword` binding and no "forget saved password"
UI control. The user can overwrite the stored value by logging in again with a
different password (same email); a stored password for an email that is never
reused becomes inert keychain garbage with no practical consequence.

## Frontend Changes

`desktop/frontend/src/components/SettingsRelay.vue`:

- Inside the existing `onMounted` block, after the `await getRelayConfig()`
  call that populates `email.value` from `cfg.last_email`, read
  `await LoadSavedRelayPassword()` into `password.value` when `email.value` is
  non-empty.
- The post-login statement in `SettingsRelay.vue` that currently clears
  `password.value = ""` after a successful Connect is **removed**, so the
  field continues to display the password the user just successfully used —
  matching the mobile flow's behavior.
- The existing password-visibility toggle (eye icon) and `type="password"`
  default are unchanged; the prefilled value is still rendered as dots until
  the user clicks the eye.

`desktop/frontend/src/lib/api.ts` exports a thin wrapper around the new wails
binding. No other call sites change.

## Data Flow

**Login success (first time or password change):**

1. User submits Connect form.
2. `LoginRemoteRelay(url, email, password, allowInsecure)` runs the OPAQUE
   exchange, stores the session token in `RelayConfig`, persists `account_key`
   via `account_key_store.go`, writes `RelayLastEmail` / `RelaySessionUserID`
   to `cfgStore`.
3. After step 2 succeeds, `saveRelayPassword(origin, email, password)` writes
   to the new slot. Failure is logged, not returned.
4. Frontend leaves `password.value` populated.

**Settings panel opens:**

1. `getRelayConfig()` loads `RelayURL`, `RelayLastEmail`, etc., as today.
2. If `RelayLastEmail` is non-empty, the panel calls
   `LoadSavedRelayPassword()` and assigns the result to `password.value`.
3. User sees their email and password already filled in; one click on Connect
   re-runs OPAQUE login.

**Login failure (wrong password, network error, OPAQUE reject):**

`saveRelayPassword` lives after the success path's `setAccountKey` call, so a
failed login never reaches it. The previously persisted password is preserved
untouched.

**Relay URL or email changes:**

The slot key is `origin + "|" + email`, so a new (origin, email) combination
addresses a fresh, empty slot. The old slot is left in place and never read
again. No explicit cleanup.

## Error Handling

| Situation | Behavior |
|---|---|
| Keychain absent (unsigned / dev / tests) | `safekeyring` falls back to 0600 file in `appdir.ConfigDir()`, transparent to caller |
| `saveRelayPassword` returns error | `log.Printf`, login still succeeds |
| `loadRelayPassword` returns error | Logged, binding returns `("", nil)` → field is empty |
| `safekeyring.ErrNotFound` | Treated as "no password stored", returns `("", nil)`, not an error |
| Empty `relayOrigin` or `email` | `loadRelayPassword` returns `("", nil)`; `saveRelayPassword` is a no-op returning `nil` |
| User upgrades from a version without persistence | First launch returns `("", nil)`; next successful login starts persisting. No migration code. |

## Testing

**Go unit tests** (`desktop/relay_password_store_test.go`, new):

- `TestSaveLoadRelayPassword_RoundTrip` — write then read back the same value.
- `TestLoadRelayPassword_NotFound` — returns `("", nil)` not an error.
- `TestSaveRelayPassword_EmptyOriginOrEmail_NoOp` — neither slot is created;
  verified via `safekeyring.SetFileDirForTest` + inspecting the JSON store.
- `TestClearRelayPassword_RoundTrip` — write, clear, load returns `("", nil)`.
- `TestClearRelayPassword_NotFound_NoError` — clearing a missing slot is fine.

These rely on the existing `TestMain` in `desktop/main_testmain_test.go` that
already calls `safekeyring.UseFileStore()` + `SetFileDirForTest(t.TempDir())`.

**Go integration test** (extend or add `desktop/app_relay_test.go`):

- Spin up an OPAQUE-capable `httptest` relay (reuse the pattern in existing
  e2eeclient tests).
- Call `LoginRemoteRelay(url, "alice@example.com", "hunter2", false)`.
- Assert `LoadSavedRelayPassword()` returns `"hunter2"`.
- Call `LoginRemoteRelay` again with the same email but `"new-pw"`; assert the
  stored password is now `"new-pw"` (overwrite semantics).
- Force the fake relay to reject login (401); assert `LoadSavedRelayPassword`
  still returns the **previously** stored value (failed login does not corrupt
  the slot).

**Frontend tests** (`SettingsRelay.test.ts`, extend or create alongside
existing test files):

- Mount with `LoadSavedRelayPassword` mocked to return `"hunter2"`; assert the
  password `<input>` value is `"hunter2"`.
- Mount with `LoadSavedRelayPassword` mocked to return `""`; assert the input
  is empty.
- After a successful `LoginRemoteRelay`, assert `password.value` is **not**
  cleared (regression test for the removed `password.value = ""` line).

**Not tested here:**

- `internal/safekeyring` itself — already covered in
  `internal/safekeyring/safekeyring_test.go`.
- OPAQUE login mechanics — already covered in `internal/e2eeclient`.
- Mobile / Capacitor — no changes in this design.

## Security Note

This design persists the user's OPAQUE password at rest, which weakens the
strongest property of the OPAQUE model ("password never leaves the device's
volatile memory"). The mitigating facts:

1. The keychain slot used (`safekeyring`) is the same medium that already
   stores the unlocked `account_key`. An attacker who can read the keychain
   already has the E2EE master key, so they can decrypt all relay-mirrored
   session data without needing the password. The marginal cost of adding the
   password is the ability to **reuse credentials on other systems** if the
   user reuses passwords — a real but bounded risk users opting into this UX
   are accepting.
2. The 0600-file fallback (used on unsigned builds and in `wails dev`) is
   plaintext-at-rest. The same posture already applies to `account_key` and to
   the session token. This change does not introduce a new exposure surface,
   only a new datum in an existing surface.
3. Mobile already ships this trade-off; this change brings the desktop in line
   with an already-accepted product decision.

No `Set-Cookie`-style scoping is required because the keychain slot is local
to the OS user; no `secret leak` log scrubbing is needed because the password
is never logged (only `log.Printf("desktop: save relay password: %v", err)` —
the error, not the value).

## Out of Scope

- Server-side refresh tokens or extending `DefaultSessionTTL`.
- A "Forget saved password" UI control.
- A "Log out" button on desktop (mobile-only; desktop has no equivalent and
  none is added).
- Auto re-login on session expiry / 401 — the user still clicks Connect.
- Cleanup of stale keychain entries when the user retires an old relay URL or
  email.
- Migration of any data — there is none to migrate.
