# Web UI Redesign — PR B: layout shell + 3 pages

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Move the topnav (Home / Settings / [Admin] / Sign Out) out of each page's hand-written HTML into a single `web/layout.js` module that injects it at runtime. Index, settings (and later admin) all consume the same shell. Visually equivalent to today — this PR sets up the seam for PR C/D.

**Architecture:** A new ESM module `web/layout.js` reads a `<meta name="page" content="...">` tag, calls `GET /api/me` to check `is_admin`, and synchronously renders the topbar contents (brand block + topnav + Sign Out button) into an empty `<header id="topbar">` placeholder. Index page-specific chrome (`#session-title`, `#back`, `#status`) moves out of the brand-block — `#status` becomes an absolutely-positioned overlay; `#session-title` and `#back` move into the main app area where `app.js` already binds them.

**Tech Stack:** Pure ESM JS (no build step), runs in the browser. Loaded as `<script type="module" src="./layout.js">`. `auth.js`'s existing `getMe()` is reused (it caches CSRF). No new deps.

**Spec:** `docs/superpowers/specs/2026-05-17-web-ui-redesign-design.md`

---

## File Map

**Create:**
- `web/layout.js` — `renderShell()` (auto-invoked on module load); renders #topbar via DOM API
- `web/layout.test.mjs` — unit tests for layout.js using jsdom-like assertions on the rendered HTML

**Modify:**
- `web/index.html` — `<meta name="page" content="home">`, empty `<header id="topbar">`, `<script type="module" src="./layout.js">`. Remove the hand-written brand+nav+signout HTML. Keep `#session-title`, `#back`, `#status` as page elements, repositioned (see Task 3).
- `web/settings.html` — same shell swap. Remove inline `<nav>` and the Sign Out button.
- `web/style.css` — new `#status` overlay positioning. `.brand-block` rules already exist; layout.js produces matching DOM so existing styles apply.
- `web/sw.js` — add `./layout.js` to `ASSETS`; CACHE name hash auto-bumps (sw-cache-bump test reports the new value).
- `web/auth.js` — `getMe()` already exists; PR B uses it as-is. Verify the cached `cachedCSRF` is shared between layout.js and page code (it is — both import from `./auth.js`).
- `web/nav.test.mjs` — rewrite assertions: pages should now contain the placeholder + meta + layout.js import, NOT the literal `<nav>` HTML.
- `web/app.js` — remove the duplicate `#logout` click handler (layout.js owns Sign Out now).

**Delete:** nothing (layout.js absorbs functionality; old HTML lines are deletions inside existing files).

---

## Task 1 — `web/layout.js` (the shell module)

**Files:**
- Create: `web/layout.js`
- Test: `web/layout.test.mjs` (Task 2)

- [ ] **Step 1: Write layout.js**

Create `web/layout.js`:

```js
// Shared layout shell for authenticated pages (index, settings, admin).
//
// Each page declares its identity with <meta name="page" content="home"> /
// "settings" / "admin", an empty <header id="topbar"></header>, and a
// <script type="module" src="./layout.js"></script>. This module renders
// the brand + topnav + sign-out button into #topbar, with Admin tab
// shown only when GET /api/me indicates the current user has is_admin=true.

import { getMe, logout } from "./auth.js";
import { fetchVersionLabel } from "./app-core.js";

const PAGE = document.querySelector('meta[name="page"]')?.content || "";

// Render synchronously with a placeholder Admin slot so the brand+nav is
// visible immediately. /api/me decides whether to actually show Admin.
renderTopbar(PAGE, false);

getMe()
    .then((me) => { renderTopbar(PAGE, !!me?.is_admin); })
    .catch(() => { /* authFetch already redirected to /login.html on 401 */ });

fetchVersionLabel(fetch).then((label) => {
    const el = document.getElementById("version");
    if (el) el.textContent = label;
});

function renderTopbar(active, isAdmin) {
    const topbar = document.getElementById("topbar");
    if (!topbar) return;
    topbar.innerHTML = `
      <div class="brand-block">
        <div class="brand">AT Term</div>
        <div class="version" id="version">version dev</div>
      </div>
      <nav class="topnav" aria-label="Primary">
        <a href="/" class="${active === "home" ? "active" : ""}"${active === "home" ? ' aria-current="page"' : ""}>Home</a>
        <a href="/settings.html" class="${active === "settings" ? "active" : ""}"${active === "settings" ? ' aria-current="page"' : ""}>Settings</a>
        ${isAdmin ? `<a href="/admin/" class="${active === "admin" ? "active" : ""}"${active === "admin" ? ' aria-current="page"' : ""}>Admin</a>` : ""}
      </nav>
      <button id="signout" class="ghost-btn" type="button">Sign out</button>`;
    const btn = document.getElementById("signout");
    if (btn) btn.addEventListener("click", () => logout());
}
```

- [ ] **Step 2: Verify the file loads under node test runner**

The actual DOM rendering can't be tested directly under `node --test` (no DOM). Task 2 mocks `document` to test the rendering function.

- [ ] **Step 3: Commit**

```bash
git add web/layout.js
git commit -m "web: layout.js — shared topnav shell renderer"
```

---

## Task 2 — `web/layout.test.mjs`

**Files:**
- Create: `web/layout.test.mjs`

- [ ] **Step 1: Write the test file**

Create `web/layout.test.mjs`:

```js
// layout.js auto-runs on import (it queries `document`). To test it under
// node --test we capture the rendered HTML by stubbing the global `document`
// before the import and the network calls.

import test from "node:test";
import assert from "node:assert/strict";

function setupDOM({ pageMeta = "home" } = {}) {
    const elements = new Map();
    const topbar = makeEl("topbar");
    elements.set("topbar", topbar);
    const versionEl = makeEl("version");
    // version may be re-queried after innerHTML rewrites; tests fetch via the topbar
    globalThis.document = {
        querySelector: (sel) => {
            if (sel === 'meta[name="page"]') return { content: pageMeta };
            return null;
        },
        getElementById: (id) => elements.get(id) || null,
    };
    return { topbar, elements };
}

function makeEl(id) {
    return {
        id,
        innerHTML: "",
        textContent: "",
        addEventListener() {},
    };
}

function stubFetch(meResponse) {
    globalThis.fetch = async (url) => {
        if (url === "/api/me") {
            return {
                ok: true,
                async json() { return meResponse; },
                status: 200,
            };
        }
        if (url === "/api/version") {
            return { ok: true, async json() { return { version: "v0.0.0-test" }; } };
        }
        return { ok: false, status: 404 };
    };
}

test("renderTopbar shows Admin link when /api/me reports is_admin=true", async () => {
    const { topbar } = setupDOM({ pageMeta: "home" });
    stubFetch({ user_id: "u1", email: "a@example.com", is_admin: true, csrf_token: "tok" });

    // Bypass module-load caching from previous tests by appending a query string.
    await import(`./layout.js?case=admin-true-${Date.now()}`);

    // Wait one microtask tick for the fetched-me handler to run.
    await new Promise((r) => setTimeout(r, 0));

    assert.match(topbar.innerHTML, /href="\/admin\/"/);
    assert.match(topbar.innerHTML, /href="\/"/);
    assert.match(topbar.innerHTML, /href="\/settings\.html"/);
    assert.match(topbar.innerHTML, /id="signout"/);
});

test("renderTopbar hides Admin link when is_admin=false", async () => {
    const { topbar } = setupDOM({ pageMeta: "settings" });
    stubFetch({ user_id: "u1", email: "a@example.com", is_admin: false, csrf_token: "tok" });

    await import(`./layout.js?case=admin-false-${Date.now()}`);
    await new Promise((r) => setTimeout(r, 0));

    assert.doesNotMatch(topbar.innerHTML, /href="\/admin\/"/);
    assert.match(topbar.innerHTML, /class="active"[^>]*aria-current="page"[^>]*>Settings/);
});

test("renderTopbar synchronously shows brand and Sign out before /api/me resolves", async () => {
    const { topbar } = setupDOM({ pageMeta: "home" });
    // Fetch that never resolves — simulate slow /api/me.
    globalThis.fetch = () => new Promise(() => {});

    await import(`./layout.js?case=sync-${Date.now()}`);

    // The synchronous render call has already happened.
    assert.match(topbar.innerHTML, /AT Term/);
    assert.match(topbar.innerHTML, /id="signout"/);
});
```

- [ ] **Step 2: Run tests**

```bash
node --test web/layout.test.mjs 2>&1 | tail -10
```

Expected: all 3 PASS.

The tricky part: layout.js auto-runs on import. The `?case=…` cache-busting in the import URL lets us re-import a fresh copy each test. If node's loader caches by URL, this works. If not, the third test may fail because state leaked from the second.

If you hit caching issues, switch to a sub-process pattern: each test spawns `node --eval "import('./web/layout.js')"` with the stubs set in the env. Simpler: split the rendering function out of the module-load auto-run.

If splitting is simpler, change `layout.js` to export `renderTopbar` and `init` separately:

```js
export function renderTopbar(active, isAdmin) { /* ... */ }

export async function init(doc = document, fetchImpl = fetch) {
    const page = doc.querySelector('meta[name="page"]')?.content || "";
    renderTopbar(page, false);
    try {
        const me = await getMe();
        renderTopbar(page, !!me?.is_admin);
    } catch { /* authFetch handles 401 redirect */ }
    fetchVersionLabel(fetchImpl).then((label) => {
        const el = doc.getElementById("version");
        if (el) el.textContent = label;
    });
}

if (typeof document !== "undefined") {
    init();
}
```

Then tests can `import { renderTopbar, init } from "./layout.js"` and pass mocked `doc` directly — no re-import gymnastics. **Prefer this shape if the auto-import-once approach hits caching trouble.**

- [ ] **Step 3: Commit**

```bash
git add web/layout.test.mjs web/layout.js
git commit -m "web: layout.test.mjs — render shell with admin gating"
```

(If you changed layout.js to the export-init shape, include it in this commit.)

---

## Task 3 — Convert `web/index.html` to the layout shell

**Files:**
- Modify: `web/index.html`
- Modify: `web/style.css` (move `#status` to absolute overlay)
- Modify: `web/app.js` (remove duplicate `#logout` handler)

- [ ] **Step 1: Update `web/index.html`**

Replace the existing topbar block:

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
<meta name="theme-color" content="#0b1020" />
<meta name="apple-mobile-web-app-capable" content="yes" />
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
<meta name="apple-mobile-web-app-title" content="AT Term" />
<meta name="page" content="home" />
<title>AT Term</title>
<link rel="manifest" href="manifest.webmanifest" />
<link rel="apple-touch-icon" href="icon.png" />
<link rel="icon" href="icon.svg" type="image/svg+xml" />
<link rel="stylesheet" href="vendor/xterm/xterm.css" />
<link rel="stylesheet" href="style.css" />
</head>
<body>
<div id="app-shell">
  <header id="topbar"></header>

  <div id="page-bar">
    <button id="back" class="ghost-btn" hidden aria-label="Back to sessions">←</button>
    <div class="subtitle" id="session-title">mobile relay</div>
    <div class="status" id="status">disconnected</div>
  </div>

  ...rest of index.html (install-hint, main, etc.) unchanged...
```

`#topbar` is now empty — layout.js fills it. `#page-bar` is a NEW container holding the three page-specific chrome elements (`#back`, `#session-title`, `#status`). The Sign Out button is GONE from this file (layout owns it).

Also add the layout.js import at the bottom alongside the existing app.js import:

```html
<script type="module" src="app.js"></script>
<script type="module" src="layout.js"></script>
```

Order doesn't matter (both are ES modules).

- [ ] **Step 2: Add `#page-bar` and `.status` overlay CSS in `style.css`**

Find the existing `.status` rule (around `style.css:76`) and the topbar/brand rules. Add a `#page-bar` rule that places it directly under the layout topbar:

```css
#page-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 12px;
  background: rgba(5, 7, 13, 0.86);
  border-bottom: 1px solid rgba(148, 163, 184, 0.18);
}
```

`.status` keeps its `margin-left: auto` so it stays right-aligned within `#page-bar`.

(If your design preference is to keep `#status` floating over the terminal instead of in a sub-bar, replace the above with `.status { position: absolute; top: …; right: …; }` and drop the `#page-bar` wrapper from Step 1. Stick with one design — don't mix.)

- [ ] **Step 3: Remove the duplicate `#logout` handler from `app.js`**

`app.js` currently has (around line 607 in the section starting with `if (_isBrowser) {`):

```js
document.getElementById("logout").addEventListener("click", () => logout());
```

Delete this line. Layout.js's `#signout` button now owns sign-out. The old `#logout` element no longer exists in the DOM.

Also remove the `import { authFetch, getMe, logout } from "./auth.js";` line if `logout` becomes unused in app.js after this deletion — but check first (`grep -n "logout" web/app.js`); `logout` may still be referenced by some session error handler.

- [ ] **Step 4: Verify the page looks right in a browser (manual smoke)**

```bash
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web --dev-insecure
```

Open `http://127.0.0.1:8080/login.html`, log in as the bootstrap admin user (set env first), confirm:
- After login `/` shows: brand+nav+sign-out at top, `#page-bar` row below with the connection status, terminal area below that
- Clicking "Settings" goes to `/settings.html`
- Clicking Sign out logs you out
- Resize the window: nothing wraps weirdly

- [ ] **Step 5: Commit**

```bash
git add web/index.html web/style.css web/app.js
git commit -m "web(index): consume layout shell; #status moves to page-bar"
```

---

## Task 4 — Convert `web/settings.html` to the layout shell

**Files:**
- Modify: `web/settings.html`

- [ ] **Step 1: Update `web/settings.html`**

Find the current head + body. Add the meta tag, swap the inline nav for the placeholder + layout.js import.

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="page" content="settings" />
<title>AT Term · settings</title>
<link rel="stylesheet" href="style.css" />
</head>
<body class="settings-page">
<header id="topbar"></header>

<div class="settings-wrap">
  ...the settings-card sections (API tokens, change password) stay as-is...
</div>

<script type="module" src="layout.js"></script>
<script type="module" src="settings.js"></script>
</body>
</html>
```

Concretely:
- REMOVE the existing topnav block (the `<nav class="topnav">` and its anchors).
- REMOVE the Sign Out button inside settings if any.
- REMOVE any `id="logout"` button.
- INSERT `<meta name="page" content="settings">` in head.
- INSERT empty `<header id="topbar">` at top of body.
- INSERT `<script type="module" src="layout.js">` near the end.

- [ ] **Step 2: Check `settings.js` doesn't reference removed DOM elements**

```bash
grep -n "getElementById" web/settings.js
```

If it grabs `#logout`, remove that line (layout owns it now).

- [ ] **Step 3: Manual smoke**

Restart the dev server, navigate to `/settings.html`, confirm:
- Topnav appears via layout.js
- Settings card content renders
- Sign out works
- "Home" link returns to `/`

- [ ] **Step 4: Commit**

```bash
git add web/settings.html web/settings.js
git commit -m "web(settings): consume layout shell"
```

(If you didn't touch settings.js, omit it from the add.)

---

## Task 5 — Rewrite `web/nav.test.mjs` for the new structure

**Files:**
- Modify: `web/nav.test.mjs`

The current nav.test.mjs greps for `<nav class="topnav">` in each page's HTML. After PR B the nav is injected at runtime, so the static HTML check no longer applies. Rewrite to assert the placeholder + meta + import shape.

- [ ] **Step 1: Rewrite the test file**

```js
// PR B moved the topnav from each page's hand-written HTML into
// web/layout.js. The static HTML now contains a meta page identifier,
// an empty <header id="topbar"> placeholder, and a layout.js import.
// Runtime rendering correctness is covered by web/layout.test.mjs;
// this file just asserts the placeholder contract on each authenticated
// page.

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const PAGES = [
    { path: "web/index.html", page: "home" },
    { path: "web/settings.html", page: "settings" },
];

for (const { path, page } of PAGES) {
    test(`${path} declares <meta name="page" content="${page}">`, () => {
        const src = readFileSync(path, "utf8");
        const re = new RegExp(`<meta\\s+name="page"\\s+content="${page}"`);
        assert.match(src, re);
    });

    test(`${path} has empty #topbar placeholder for layout.js`, () => {
        const src = readFileSync(path, "utf8");
        assert.match(src, /<header\s+id="topbar"\s*>\s*<\/header>/);
        // Must NOT contain a hard-coded topnav anymore.
        assert.doesNotMatch(src, /<nav\s+class="topnav"/);
    });

    test(`${path} imports layout.js as an ES module`, () => {
        const src = readFileSync(path, "utf8");
        assert.match(src, /<script\s+type="module"\s+src="layout\.js"/);
    });
}
```

- [ ] **Step 2: Run**

```bash
node --test web/nav.test.mjs 2>&1 | tail -10
```

Expected: all 6 tests PASS (3 per page × 2 pages).

- [ ] **Step 3: Commit**

```bash
git add web/nav.test.mjs
git commit -m "web(test): nav.test.mjs asserts placeholder contract instead of inline HTML"
```

---

## Task 6 — Bump sw cache, add layout.js to ASSETS

**Files:**
- Modify: `web/sw.js`

- [ ] **Step 1: Add layout.js to ASSETS**

Edit `web/sw.js`:

```js
const ASSETS = [
  "./",
  "./app-core.js",
  "./app.js",
  "./layout.js",
  "./login.js",
  "./signup.js",
  "./settings.js",
  "./style.css",
  "./vendor/xterm/xterm.css",
  "./vendor/xterm/xterm.js",
  "./vendor/xterm-addon-fit/xterm-addon-fit.js",
  "./manifest.webmanifest",
  "./icon.png",
  "./icon.svg",
];
```

`layout.js` slotted alphabetically after `app.js`.

- [ ] **Step 2: Run web tests; sw-cache-bump will report the new hash**

```bash
node --test web/*.test.mjs 2>&1 | grep -E "ℹ tests|ℹ fail|sw\.js CACHE name"
```

Expected: exactly ONE failing test — `sw.js CACHE name embeds the current ASSETS content hash` — with an error message telling you what to paste:

```
... is stale. Replace it with "at-term-web-<8 hex>" ...
```

- [ ] **Step 3: Paste the new hash into sw.js**

```js
const CACHE = "at-term-web-<paste the 8 hex chars>";
```

- [ ] **Step 4: Re-run tests, confirm green**

```bash
node --test web/*.test.mjs 2>&1 | tail -8
```

Expected: ALL tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/sw.js
git commit -m "web(sw): precache layout.js; CACHE hash bump"
```

---

## Task 7 — Full local test sweep + manual smoke

- [ ] **Step 1: Full Node test suite**

```bash
node --test web/*.test.mjs 2>&1 | tail -8
```

Expected: ALL tests PASS (layout.test + nav.test + sw-cache-bump + everything else).

- [ ] **Step 2: Full Go test suite (regression check — we didn't touch Go but pages serve through the relay)**

```bash
go test -count=1 -timeout 90s ./... 2>&1 | tail -10
```

Expected: all packages OK.

- [ ] **Step 3: Manual smoke against a live relay**

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL=you@example.com \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web --dev-insecure
```

Open `http://127.0.0.1:8080/login.html`, log in:

- [ ] After login `/` shows brand+nav+sign-out at the top
- [ ] `#page-bar` shows the connection status; terminal area renders normally
- [ ] Admin tab IS visible (the bootstrap user is admin)
- [ ] Click "Settings" → `/settings.html` — same nav appears, "Settings" is active, "Home" link works
- [ ] Click "Admin" → `/admin/` → returns 404 (PR D ships the UI; expected for now)
- [ ] Click "Sign out" → returns to `/login.html`
- [ ] Log in as a NON-admin user → Admin tab is NOT rendered

Create a non-admin user via the admin API to test the last case:

```bash
# (after logging in as admin, grab cookie + CSRF from /api/me)
curl -X POST -b "atterm_session=…" -H "X-CSRF-Token: …" \
  -H "Content-Type: application/json" \
  -d '{"note":"smoke-test"}' \
  http://127.0.0.1:8080/admin/api/invitations
# use the returned invite code at /signup.html
```

- [ ] **Step 4: Mark this checkpoint** — proceed to ship-release.

---

## Task 8 — Ship `v0.1.74` via the ship-release skill

- [ ] **Step 1: Open the PR**

Branch is `feat/web-layout-shell` (already created from main). Push it:

```bash
git push -u origin feat/web-layout-shell
```

- [ ] **Step 2: Open PR via gh**

```bash
gh pr create --title "feat(web): layout shell renders shared topnav for index + settings" --body "$(cat <<'EOF'
## Summary

PR B of the web UI redesign. The Home / Settings / [Admin] / Sign Out chrome is now rendered at runtime by web/layout.js instead of being copy-pasted into each page's HTML. Visually unchanged; sets up the seam for PR C (settings redesign) and PR D (admin UI).

- `web/layout.js`: shared shell module. Reads `<meta name="page">`, calls `getMe()`, renders brand+topnav+sign-out into the page's `<header id="topbar">` placeholder. Admin link is shown only when `me.is_admin === true`.
- `web/index.html` and `web/settings.html`: meta + placeholder + layout.js import; remove the hand-written topnav HTML.
- Index #status / #back / #session-title move out of the brand-block into a new `#page-bar` row so they don't compete with the shared layout slots.
- `web/nav.test.mjs`: rewritten to assert the placeholder contract; `web/layout.test.mjs`: new tests covering the runtime render including admin gating.
- `web/sw.js`: `layout.js` added to ASSETS; CACHE hash bumped (caught by sw-cache-bump test).

## Test plan

- [ ] `node --test web/*.test.mjs` passes locally
- [ ] `go test ./...` passes locally
- [ ] After deploy: admin login → topbar shows Home / Settings / Admin / Sign Out; non-admin login → Admin link hidden
- [ ] Settings page shows the same nav with Settings active
- [ ] Sign out works from either page
- [ ] `/admin/` still 404s (PR D ships the static UI)
EOF
)"
```

- [ ] **Step 3: Squash merge**

```bash
gh pr merge <number> --squash
```

- [ ] **Step 4: Tag v0.1.74**

```bash
git fetch origin main
LATEST=$(git tag --list --sort=-v:refname | head -1)  # expect v0.1.73
NEXT=v0.1.74
git tag $NEXT $(gh pr view <number> --json mergeCommit -q .mergeCommit.oid)
git push origin $NEXT
git push origin --delete feat/web-layout-shell
gh run list --limit 3
```

- [ ] **Step 5: Confirm CI**

Watch the Release workflow at `https://github.com/attson/atterm/actions`. Once green, the deploy goes live. Hard-reload an existing browser tab to pick up the new sw cache.

---

## Done Criteria

- All 8 tasks complete with green commits.
- `node --test web/*.test.mjs` and `go test ./...` both pass.
- v0.1.74 tag pushed, Release workflow succeeded.
- Admin user can navigate Home ↔ Settings ↔ Sign Out via the new shared topbar; non-admin users see no Admin link.
- PR C (settings sub-tabs + API additions) can be written.

## Out of Scope (for PR B — handled later)

- Admin UI itself: `web/admin/` static files come in PR D.
- Settings sub-tabs + Signed-in devices + Danger zone: PR C.
- Design tokens / login + signup polish: PR E.
- Push notification icon redesign: leave the existing index-topbar bell button alone.
