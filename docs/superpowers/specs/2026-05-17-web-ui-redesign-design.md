# Browser-side UI Redesign (User + Admin)

**Date:** 2026-05-17
**Status:** Approved

## Goal

Bring the browser-side experience — login, signup, terminal home, settings, admin — onto one polished GitHub-Dark visual system, and migrate admin authentication from a shared `ATTERM_ADMIN_TOKEN` env to a `users.is_admin` role driven by normal session login. Replace the standalone 1990s-style `/admin/` HTML with a static page that shares the same layout shell as the rest of the site, and reorganize settings + admin into top sub-tabs so adding sections later is straightforward.

## Motivation

The current state has three problems:

1. **Discovery.** `/settings.html` (API tokens, password) had no entry point from `/index.html` until v0.1.70 added a top nav; `/admin/` still has none and uses an unrelated visual style.
2. **Two visual languages.** `/admin/` renders as a white system-ui document while everything else is GitHub Dark. They are clearly different products to a user.
3. **Admin auth coupling.** Admin access is gated by a shared `ATTERM_ADMIN_TOKEN` Bearer secret. Re-entered on every page refresh, no per-actor audit trail, and the page-side token input is the only "admin login UI" the project has. Single-user project — we don't need a second credential system parallel to user accounts.

## Non-Goals

- A push-notifications section in settings (keeps the existing bell button in the index topbar).
- Profile, email-change, insecure-mode toggle in settings.
- An "About" or "Help" page.
- Changes to xterm theme logic, WebGL renderer, ptyhost, shell-integration.
- A build pipeline for `web/` (stays pure static ESM).
- Renaming the `web_sessions` table (the user-facing label changes to "Signed-in devices" but the DB stays as-is).

## Information Architecture

### Shared top nav (rendered by `web/layout.js`)

Visible on every authenticated page (`/`, `/settings.html`, `/admin/`):

```
[ AT Term ]   [ Home ]  [ Settings ]  [ Admin* ]                        [ Sign out ]
                                          ▲
                                          shown only when GET /api/me returns is_admin=true
```

- The active page link gets `class="active"`.
- "Admin" link is rendered client-side based on `/api/me`; the actual route gate is server-side.
- Sign Out is part of the nav (previously a separate button in the index topbar / a section button inside settings — both are removed in favor of one consolidated control).

### Settings sub-tabs

```
┌─ Settings ─────────────────────────────────────────┐
│ [ API Tokens ] [ Change Password ] [ Signed-in     │
│                                      devices ]     │
│                                      [ Danger      │
│                                        zone ]      │
├────────────────────────────────────────────────────┤
│ <active tab content>                               │
└────────────────────────────────────────────────────┘
```

1. **API Tokens** — existing CRUD, unchanged behavior, restyled with new components.
2. **Change Password** — existing form, restyled.
3. **Signed-in devices** — new. Lists the current user's `web_sessions` rows: device label (derived from user_agent), IP prefix, created_at, expires_at, and a Revoke action per row. Footer button: "Sign out everywhere except this device".
4. **Danger zone** — new. Single destructive action: delete account. Two-step confirm (must type full email). Hard delete (relies on existing `ON DELETE CASCADE` for invitations / api_tokens / web_sessions).

### Admin sub-tabs

```
┌─ Admin ────────────────────────────────────────────┐
│ [ Invitations ]  [ Users ]  [ Config ]             │
├────────────────────────────────────────────────────┤
│ <active tab content>                               │
└────────────────────────────────────────────────────┘
```

1. **Invitations** — existing create / list, restyled.
2. **Users** — existing list + reset-password + disable; **new** Promote / Demote admin action per row. Self-demote is rejected with 400.
3. **Config** — existing rate-limit + max-connections, restyled.

The current token input box at the top is removed; auth is the regular user session cookie.

## Visual Design Tokens

GitHub-Dark refined. All `web/*.html` consume the same `--*` custom properties from `style.css` so settings, admin, and login share one palette.

```css
:root {
  /* surfaces */
  --bg:          #0d1117;
  --surface:     #161b22;
  --surface-2:   #1c2129;       /* hover, nested */
  --border:      #30363d;
  --border-mute: #21262d;       /* table row separators */

  /* text */
  --fg:          #c9d1d9;
  --fg-mute:     #8b949e;

  /* accent + status */
  --accent:      #58a6ff;
  --accent-bg:   rgba(88,166,255,.12);
  --good:        #3fb950;
  --bad:         #f85149;
  --warn:        #d29922;
}
```

New tokens over today's set: `--surface-2`, `--border-mute`, `--accent-bg`, `--warn`. Existing names keep their meaning, so live pages don't shift unexpectedly while components migrate.

**Typography**
- UI: `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`
- Mono (tokens, CLI examples, table key columns): `ui-monospace, SFMono-Regular, Menlo, Consolas, monospace`
- Size scale: 11 / 12 / 14 / 16 / 20 px (no other sizes)
- Line height: 1.2 for headings, 1.5 for body

**Spacing**
- `--space-1` 4 / `--space-2` 8 / `--space-3` 12 / `--space-4` 16 / `--space-6` 24 / `--space-8` 32
- All padding / gap MUST reference a token; raw px values are a lint smell.

**Component classes** (added to `style.css`)
- `.card` — surface container (border + radius 8 + padding)
- `.subtabs` / `.subtabs a[.active]` — page-internal top tabs
- `.table` — dense table with `--border-mute` row separators and mono key cells
- `.pill / .pill--ok / .pill--bad / .pill--warn` — status pills
- `.btn / .btn--primary / .btn--danger / .btn--ghost`
- `.input / .input--mono`
- `.section-head` — uppercase 11px label
- `.empty-state` — placeholder block for empty lists

## Backend: Admin Role + Bootstrap

### Schema

New migration `internal/userstore/migrations/0002_admin_role.sql`:

```sql
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
```

SQLite has no BOOLEAN; INTEGER 0/1 is the convention. `DEFAULT 0` ensures existing rows stay non-admin.

### Store interface additions

`internal/userstore/store.go::Store`:

```go
// EnsureAdminUser is idempotent: if a user with this email exists,
// set is_admin=1 and return (created=false, nil) — password is ignored.
// Otherwise create a new user with the given password and is_admin=1
// (created=true). The create path requires the bootstrap password
// strength rule (see "Bootstrap password strength rule"); empty / weak /
// blacklisted plaintext returns ErrPasswordTooWeak. The created flag
// lets the caller emit the right "you can unset the env now" warning.
EnsureAdminUser(ctx, email, plaintext string) (created bool, err error)

// SetUserAdmin flips the is_admin flag for promote/demote APIs.
SetUserAdmin(ctx, userID string, admin bool) error
```

`User` struct gains `IsAdmin bool`. `GetUser` returns it. `ListUsers` (used by admin UI) returns it.

### Principal change

`internal/relay/identity.go`:

- `PrincipalAdmin` Kind keeps its name and `requireAdmin` semantics (handlers don't change).
- Trigger source moves: cookie / API token resolves to a user → if `user.is_admin` then `Principal{Kind: PrincipalAdmin, UserID: ...}`, else `Principal{Kind: PrincipalUser}`.
- `NewIdentityResolver` loses the `adminToken` parameter.
- The Bearer-token branch that previously matched `ATTERM_ADMIN_TOKEN` is deleted outright.

### Bootstrap on startup

In `cmd/atterm-relay/main.go`, after `store.migrate` succeeds:

```go
email := strings.TrimSpace(os.Getenv("ATTERM_BOOTSTRAP_ADMIN_EMAIL"))
pwd   := os.Getenv("ATTERM_BOOTSTRAP_ADMIN_PASSWORD")
if email != "" {
    if _, err := mail.ParseAddress(email); err != nil {
        log.Fatalf("ATTERM_BOOTSTRAP_ADMIN_EMAIL: %v", err)
    }
    created, err := store.EnsureAdminUser(ctx, email, pwd)
    if err != nil {
        log.Fatalf("bootstrap admin user: %v", err)
    }
    if !created && pwd != "" {
        log.Printf("WARN: ATTERM_BOOTSTRAP_ADMIN_PASSWORD set but %s already exists — password ignored. Unset the env to remove it from process state.", email)
    }
    if created {
        log.Printf("WARN: bootstrap created admin user %s — unset ATTERM_BOOTSTRAP_ADMIN_PASSWORD and restart to remove the credential from process state.", email)
    }
}
```

- **Email format validated** via `net/mail.ParseAddress` before any DB work; a bad value fail-fasts at startup.
- Unset email → no-op (relay still serves, but no admin role is provisioned).
- Email set + user exists → mark admin, ignore password, log "promoted existing user to admin"; if password was also set, emit a `WARN` line telling the operator to unset it (the env is now a long-lived residual secret).
- Email set + user missing + password meets strength rule → create user, mark admin, emit a `WARN` line telling the operator to unset the password env now that the account exists.
- Email set + user missing + weak / empty / blacklisted password → `log.Fatalf` so the operator notices the misconfig at startup.
- `EnsureAdminUser` returns `(created bool, err error)` so the calling code can distinguish "promoted" from "created" without re-querying.

### Bootstrap password strength rule

`ATTERM_BOOTSTRAP_ADMIN_PASSWORD` (when used to create a new user) must satisfy a **stricter rule than the everyday `ChangePassword` ≥12-char minimum**, because it lives in an env file / systemd unit and is therefore long-lived plaintext on disk:

- ≥16 characters
- ≥3 distinct character classes (lower / upper / digit / symbol)
- Not in `weakBootstrapPasswordBlacklist` — reuse the existing `weakAdminTokenBlacklist` content from `cmd/atterm-relay/token_strength.go`, renamed/moved; the rule file is salvaged from the otherwise-deleted token_strength.go rather than thrown away.

If the env password fails any rule, `EnsureAdminUser` returns `ErrPasswordTooWeak`, which the bootstrap call upgrades to `log.Fatalf`. (When `EnsureAdminUser` is called for an existing user, the password is ignored entirely so the rule does not apply.)

### Public-listen safety requirement

Today the relay rejects public bind without a strong `ATTERM_ADMIN_TOKEN`. Replace that gate:

- Public listen now requires `ATTERM_BOOTSTRAP_ADMIN_EMAIL` (guarantees an admin path exists).
- `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` strength reuses the existing password rule (`≥12 chars`); no new blacklist.
- `--dev-insecure` still bypasses for loopback dev.

### User-facing API additions (for the new Settings sections)

- `GET    /api/me/sessions` → list current user's `web_sessions` rows. Response fields: `id_hash` (opaque identifier for revoke calls), `user_agent`, `ip_prefix`, `created_at`, `expires_at`, `is_current` (true for the row whose id_hash matches the request's cookie).
- `DELETE /api/me/sessions/{id_hash}` → revoke a specific session. CSRF-gated. Refusing to revoke the current cookie's own session is fine; the UI just doesn't offer a Revoke button on that row.
- `POST   /api/me/sessions/sign-out-others` → delete every `web_sessions` row for this user except the one whose id_hash matches the request's cookie. CSRF-gated.
- `DELETE /api/me` → hard-delete the current user. Body: `{"email": "...", "password": "..."}`. CSRF-gated. **Both checks required:** `email` must equal the user's email (typo-protection); `password` is re-verified via the same `VerifyPassword` path used by `ChangePassword` (so a stolen cookie + CSRF token alone cannot wipe an account — attacker also needs the plaintext password). **Last-admin protection:** if `user.is_admin == true` and `SELECT count(*) FROM users WHERE is_admin=1` returns 1, refuse with 409 `last_admin`. `api_tokens` and `web_sessions` cascade via the existing FK. `invitations.consumed_by` is `REFERENCES users(id)` without cascade (history field), so the handler does `UPDATE invitations SET consumed_by = NULL WHERE consumed_by = ?` then `DELETE FROM users WHERE id = ?` inside one transaction. Server-side `web_sessions` rows for this user are deleted by the cascade; the session cookie is cleared before responding 204.

### Admin API additions

In `internal/relay/admin_http.go`:

- `POST /admin/api/users/{id}/admin` → `SetUserAdmin(id, true)`
- `DELETE /admin/api/users/{id}/admin` → `SetUserAdmin(id, false)`
- **Self-demote** rejected: if `id == principal.UserID` → 400 `cannot_demote_self`.
- **Last-admin protection on demote**: before flipping `is_admin=0`, run `SELECT count(*) FROM users WHERE is_admin=1`; if the result is 1 and the target is that row → 409 `last_admin`. (Self-demote rule already covers the common case; this catches the case where another admin demotes the would-be-last admin who isn't themselves.)
- **Audit log line** for every promote / demote: `log.Printf("admin role change: actor=%s target=%s op=%s", principal.UserID, id, op)`. No new schema; goes to the relay's normal stderr log so an operator grepping logs sees the trail.
- `GET /admin/api/users` response gains `is_admin` field.

### Deletions

- `cmd/atterm-relay/token_strength.go` is renamed to `bootstrap_password_strength.go` and downsized: keep the blacklist + character-class + run-limit helpers (now reused by the bootstrap password rule) and rename the entry point from `validateAdminToken` to `validateBootstrapPassword` with the new ≥16 minimum. The original 4 tests are rewritten against the new entry point and rule; coverage of the blacklist / class / run-limit branches stays intact.
- `ATTERM_ADMIN_TOKEN` env / `--admin-token` CLI flag / `Config.AdminToken`.
- `IdentityResolver.adminToken` field and the Bearer-admin branch in `Resolve`.
- `Server.authorizeAdmin` legacy helper (already only referenced from the about-to-go-away admin page handler).
- `internal/relay/admin_http.go::handleAdminPage` + `adminPageHTML` constant + the `s.mux.HandleFunc("/admin/", ...)` registration.

## Frontend: Layout Shell + Page Rewrites

### `web/layout.js` (new)

```js
import { fetchVersionLabel } from "./app-core.js";
import { logout } from "./auth.js";

const PAGE = document.querySelector('meta[name="page"]')?.content || "";

const me = await fetch("/api/me", { credentials: "same-origin" })
    .then(r => r.ok ? r.json() : null).catch(() => null);

renderTopbar(PAGE, !!me?.is_admin);
fetchVersionLabel(fetch).then(label => {
    const el = document.getElementById("version");
    if (el) el.textContent = label;
});

function renderTopbar(active, isAdmin) {
    const topbar = document.getElementById("topbar");
    topbar.innerHTML = `
      <div class="brand-block">…</div>
      <nav class="topnav" aria-label="Primary">
        <a href="/"             class="${active==='home'?'active':''}">Home</a>
        <a href="/settings.html" class="${active==='settings'?'active':''}">Settings</a>
        ${isAdmin ? `<a href="/admin/" class="${active==='admin'?'active':''}">Admin</a>` : ''}
      </nav>
      <button id="signout" class="btn btn--ghost">Sign out</button>`;
    document.getElementById("signout").addEventListener("click", () => logout());
}
```

Page authors only do:
```html
<meta name="page" content="settings">
<header id="topbar"></header>
<script type="module" src="./layout.js"></script>
```

### Per-page changes

**`web/index.html`**
- Remove the hand-written topbar HTML. Insert the placeholder + layout.js import + page meta.
- `#status` (connection indicator) moves out of the topbar into `#app-shell` as an absolutely-positioned top-right badge so it doesn't compete with the shared topnav.
- `#logout` button removed (Sign Out lives in shared nav).

**`web/settings.html`**
- Same shell swap.
- Body becomes: `<nav class="subtabs">…</nav>` + four `<section class="card">` panels (one per tab, only the active one visible).
- Settings page-internal style block shrinks (most rules graduate to `style.css` component classes).
- Sign Out button inside settings removed.

**`web/admin/index.html`** (new file; takes over from `adminPageHTML` const)
- Same shell pattern.
- Same three panels (Invitations / Users / Config) restyled with new components.
- Token input box removed.

**`web/admin/admin.js`** (new) — extracted from the inline script in `adminPageHTML`, refactored into one init function per panel, no `token` field.

**`web/login.html` / `web/signup.html`** — no topnav (unauthenticated). Visual polish only: card border, spacing, error state typography use tokens.

**`web/style.css`** — adds tokens + component classes; existing rules audited and migrated to tokens (e.g. raw `rgba(15,23,42,.9)` becomes `var(--surface)`).

### Server routing

`internal/relay/server.go::newStaticHandler` extended:

```go
if r.URL.Path == "/admin/" || r.URL.Path == "/admin/index.html" {
    p := resolver.Resolve(r)
    if p.Kind != PrincipalAdmin {
        http.Redirect(w, r, "/", http.StatusFound); return
    }
}
fs.ServeHTTP(w, r)
```

- `handleAdminPage` and the `/admin/` mux registration are deleted.
- `/admin/*.js`, `/admin/*.css` are served by the file server with no extra gate — the shell is a static skeleton; API endpoints enforce admin server-side.
- Top-level CSP (`script-src 'self'`) now applies to `/admin/` too — the per-handler `script-src 'unsafe-inline'` override goes away.

### Service worker

`web/sw.js` `ASSETS` adds `./layout.js`. The new sw-cache-bump test will report the new hash; we paste it into `CACHE`.

## Testing

### Backend (Go)

- `userstore/admin_test.go` (new) — `EnsureAdminUser` for: new user with strong password (`created=true`), existing user (password ignored, `created=false`), missing user with weak / empty / blacklisted password (returns `ErrPasswordTooWeak`).
- `bootstrap_test.go` (new, in `cmd/atterm-relay/`) — env parsing dispatch: unset email → no call; malformed email (no `@`) fatals; email + missing user + ok password → `EnsureAdminUser` called and `created=true` warn line emitted; email + existing user + password env still set → `created=false` warn line emitted; weak password fatals.
- `bootstrap_password_strength_test.go` (renamed) — the 4 token_strength tests retargeted at `validateBootstrapPassword` with the ≥16 minimum.
- `admin_http_test.go` rewrite — replace `testAdminToken` Bearer header with a `bootstrapAdminUser(t, store)` fixture that creates an admin user, a session cookie, and a CSRF token. All current admin tests retarget to cookie auth.
- New tests for `POST /admin/api/users/{id}/admin`, `DELETE …`: success, self-demote 400, last-admin demote 409, audit log line emitted (capture via `log.SetOutput`).
- New tests for `DELETE /api/me`: success path requires correct password and email; wrong password 401; wrong email 400; last-admin self-delete 409; cascade verified (api_tokens + web_sessions gone, invitations.consumed_by NULL).
- `version_test.go::TestRateLimitBadTokensShareIPBucket` — already migrated to `/api/sessions` in PR #35; no change.

### Frontend (Node)

- `web/layout.test.mjs` (new) — `renderShell` with mocked `fetch("/api/me")` returning user vs admin produces expected DOM.
- `web/admin/admin.test.mjs` (new) — admin html has expected placeholders, admin.js calls `/admin/api/users` / `/admin/api/users/{id}/admin` etc.
- `web/nav.test.mjs` extended to cover all three pages: `index.html`, `settings.html`, `admin/index.html` each have meta page + topbar placeholder + layout.js import.
- `web/no-inline-script.test.mjs` automatically covers admin's new files.
- `web/sw-cache-bump.test.mjs` enforces the new asset hash.

## Migration

Operator-visible steps when upgrading the production relay:

1. Pull the new release tag.
2. Edit `.env` / systemd unit / docker-compose:
   - Remove `ATTERM_ADMIN_TOKEN=...`
   - Add `ATTERM_BOOTSTRAP_ADMIN_EMAIL=you@example.com`
   - If your email already has an account, you do NOT need to set `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`.
   - If creating a fresh admin account, also add `ATTERM_BOOTSTRAP_ADMIN_PASSWORD=<≥12-char password>`.
3. Restart relay. `0002_admin_role.sql` auto-applies; bootstrap promotes / creates as appropriate; a `WARN` line in the relay log tells you whether the password env can now be unset.
4. Log in at `/login.html` with your user account. The topnav now shows "Admin". Open `/admin/` to confirm.
5. **If you set `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`**: now unset it from your env file / systemd unit and restart the relay. The relay will continue to operate (the user already exists and stays an admin). Leaving the password in process state is the single largest residual risk introduced by this redesign.

No grace period for the old `ATTERM_ADMIN_TOKEN` — relay code no longer reads it. Documented prominently in release notes.

## Docs

- `README.md` — deploy section: replace every `ATTERM_ADMIN_TOKEN` example / env-table row with the bootstrap pair; add a "Bootstrap admin" subsection explaining the three cases (no env / existing user / new user). Add a **security note**: "After the bootstrap user is created, unset `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` from your env file / systemd unit and restart. Leaving the password in process state means anyone with read access to the env (other services on the host, backups, `/proc/self/environ`) can recover the initial admin credential."
- `AGENTS.md` — security + deploy sections: same swap; replace the admin-token strength paragraph with the bootstrap-password strength rule (≥16 + ≥3 classes + blacklist) and the same "unset after first start" guidance.
- Update inline references: `cmd/atterm-relay/token_strength.go` → `bootstrap_password_strength.go`.

## Roll-out Plan

Five sequential PRs, each independently shippable, each gets its own patch tag.

| PR | Tag (planned) | Scope |
|----|---------------|-------|
| A  | v0.1.72       | Backend: migration + `EnsureAdminUser` + bootstrap env + admin-token removal + promote/demote API |
| B  | v0.1.73       | `web/layout.js` shell + three pages converted to use it (visually equivalent, just nav source moved) |
| C  | v0.1.74       | Settings redesign: subtabs + Signed-in devices + Danger zone (+ backing APIs) |
| D  | v0.1.75       | Admin UI migration: static `web/admin/`, new promote/demote UI, gate in newStaticHandler |
| E  | v0.1.76       | Design tokens audit, login/signup polish, color/spacing migration to tokens |

PR A is the largest single-commit risk (auth refactor + DB migration). PRs B–E are mostly mechanical once A lands.

## Out of Scope

- Push-notifications section in settings; the existing bell button in the index topbar stays.
- Profile / email-change / insecure-mode toggle.
- About / Help page.
- Build pipeline for `web/`.
- Renaming the `web_sessions` table.
- Internationalization of new strings (UI stays English; existing convention).
