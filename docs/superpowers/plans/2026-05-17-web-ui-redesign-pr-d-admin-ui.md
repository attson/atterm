# Web UI Redesign — PR D: Admin static UI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Restore `/admin/` as a static page under `web/admin/`, consistent with the rest of the layout-shell pages, and add promote/demote UI in the Users tab. The `newStaticHandler` gate redirects non-admin requests to `/` (already-logged-in users) or `/login.html` (anonymous).

**Architecture:** `web/admin/index.html` is a static page that uses the same layout shell pattern as `index.html`/`settings.html` (PR B). `web/admin/admin.js` owns shared tab switching + Config panel. Two split modules — `admin-invitations.js` and `admin-users.js` — own per-panel logic, mirroring the settings.js / settings-sessions.js / settings-danger.js split from PR C. The relay's static handler gains a `/admin/` gate: anonymous → `/login.html`; logged-in non-admin → `/` (they have no business at the admin UI); admin → serve files.

**Tech Stack:** Static ESM modules under `web/admin/`, served by the existing `http.FileServer(http.Dir(webDir))`. No new Go deps; no new web deps.

**Spec:** `docs/superpowers/specs/2026-05-17-web-ui-redesign-design.md` (sections "Information Architecture" → Admin sub-tabs + "Frontend: Layout shell + Page rewrites" → `web/admin/`).

---

## File Map

**Create:**
- `web/admin/index.html` — layout-shell page with 3 sub-tabs (Invitations / Users / Config) + 3 panels
- `web/admin/admin.js` — tab switching + hash routing + Config panel logic
- `web/admin/admin-invitations.js` — Invitations panel: create + list + refresh
- `web/admin/admin-users.js` — Users panel: list + promote/demote + reset password + disable
- `web/admin/admin.test.mjs` — assert page structure + endpoint wiring

**Modify:**
- `internal/relay/server.go::newStaticHandler` — add the `/admin/` gate (302 → `/login.html` for anonymous, 302 → `/` for non-admin)
- `internal/relay/server_test.go` (or wherever newStaticHandler is tested) — add gate tests
- `web/sw.js` — add the 4 new files to ASSETS; hash bump

**Delete:** none.

---

## Phase 1 — Relay gate

### Task 1 — `/admin/` gate in `newStaticHandler`

**Files:**
- Modify: `internal/relay/server.go::newStaticHandler`
- Modify (or create): a test file with the new gate cases

- [ ] **Step 1: Edit `newStaticHandler`**

Find the function around `internal/relay/server.go:477`:

```go
func newStaticHandler(resolver *IdentityResolver, webDir string) http.Handler {
    fs := http.FileServer(http.Dir(webDir))
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/" || r.URL.Path == "/index.html" {
            if resolver != nil {
                p := resolver.Resolve(r)
                if !p.IsUser() {
                    http.Redirect(w, r, "/login.html", http.StatusFound)
                    return
                }
            }
        }
        fs.ServeHTTP(w, r)
    })
}
```

Add the admin gate immediately after the index gate, before `fs.ServeHTTP`:

```go
        if r.URL.Path == "/admin/" || r.URL.Path == "/admin/index.html" {
            if resolver != nil {
                p := resolver.Resolve(r)
                if p.Kind == PrincipalNone {
                    http.Redirect(w, r, "/login.html", http.StatusFound)
                    return
                }
                if p.Kind != PrincipalAdmin {
                    http.Redirect(w, r, "/", http.StatusFound)
                    return
                }
            }
        }
        fs.ServeHTTP(w, r)
```

Note: ONLY the bare path `/admin/` and `/admin/index.html` are gated. Subresources (`/admin/admin.js`, `/admin/admin.css`) are served unconditionally — they're just static skeleton code, the API endpoints under `/admin/api/*` are the real auth boundary.

- [ ] **Step 2: Write tests for the gate**

Find where `newStaticHandler` is currently tested:

```bash
grep -rln "newStaticHandler\|TestStatic" internal/relay/ 2>&1
```

If a test file exists, append. If not, create `internal/relay/static_handler_test.go`:

```go
package relay

import (
    "context"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"

    "github.com/attson/atterm/internal/userstore"
)

// fakeWebDir creates a temp directory with placeholder files for / and /admin/
// so http.FileServer has something to serve. Returns the dir.
func fakeWebDir(t *testing.T) string {
    t.Helper()
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>home</html>"), 0o644); err != nil {
        t.Fatal(err)
    }
    if err := os.MkdirAll(filepath.Join(dir, "admin"), 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(dir, "admin", "index.html"), []byte("<html>admin</html>"), 0o644); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(dir, "admin", "admin.js"), []byte("/* admin */"), 0o644); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(dir, "login.html"), []byte("<html>login</html>"), 0o644); err != nil {
        t.Fatal(err)
    }
    return dir
}

func TestStaticHandler_AdminGate_AnonymousRedirectsToLogin(t *testing.T) {
    dir := fakeWebDir(t)
    store, _ := userstore.Open(context.Background(), ":memory:")
    defer store.Close()
    resolver := NewIdentityResolver(store)
    handler := newStaticHandler(resolver, dir)

    req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusFound {
        t.Fatalf("status=%d; want 302", rec.Code)
    }
    if got := rec.Header().Get("Location"); got != "/login.html" {
        t.Errorf("Location=%q; want /login.html", got)
    }
}

func TestStaticHandler_AdminGate_NonAdminRedirectsToHome(t *testing.T) {
    dir := fakeWebDir(t)
    ctx := context.Background()
    store, _ := userstore.Open(ctx, ":memory:")
    defer store.Close()
    u, _ := store.CreateUser(ctx, "u@example.com", "passphrase-1234")
    secret, _ := store.CreateWebSession(ctx, u.ID, "ua", "")
    resolver := NewIdentityResolver(store)
    handler := newStaticHandler(resolver, dir)

    req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
    req.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusFound {
        t.Fatalf("status=%d; want 302", rec.Code)
    }
    if got := rec.Header().Get("Location"); got != "/" {
        t.Errorf("Location=%q; want /", got)
    }
}

func TestStaticHandler_AdminGate_AdminServesPage(t *testing.T) {
    dir := fakeWebDir(t)
    ctx := context.Background()
    store, _ := userstore.Open(ctx, ":memory:")
    defer store.Close()
    u, _ := store.CreateUser(ctx, "a@example.com", "passphrase-1234")
    _ = store.SetUserAdmin(ctx, u.ID, true)
    secret, _ := store.CreateWebSession(ctx, u.ID, "ua", "")
    resolver := NewIdentityResolver(store)
    handler := newStaticHandler(resolver, dir)

    req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
    req.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("status=%d; want 200; body=%s", rec.Code, rec.Body.String())
    }
    if !contains(rec.Body.String(), "admin") {
        t.Errorf("body did not contain admin shell HTML; got %q", rec.Body.String())
    }
}

func TestStaticHandler_AdminSubresources_NotGated(t *testing.T) {
    dir := fakeWebDir(t)
    store, _ := userstore.Open(context.Background(), ":memory:")
    defer store.Close()
    resolver := NewIdentityResolver(store)
    handler := newStaticHandler(resolver, dir)

    // No cookie — anonymous request.
    req := httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusOK {
        t.Errorf("admin subresource status=%d; want 200 (static skeleton is fine to serve to anyone; API endpoints are the real auth boundary)", rec.Code)
    }
}

func contains(haystack, needle string) bool {
    for i := 0; i+len(needle) <= len(haystack); i++ {
        if haystack[i:i+len(needle)] == needle { return true }
    }
    return false
}
```

(If the file already exists with similar helpers, reuse them.)

- [ ] **Step 3: Run tests**

```bash
go test -run "TestStaticHandler_AdminGate|TestStaticHandler_AdminSubresources" -v ./internal/relay/ 2>&1 | tail -20
go test -count=1 -timeout 90s ./internal/relay/ 2>&1 | tail -5
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/relay/server.go internal/relay/static_handler_test.go
git commit -m "relay: /admin/ gate redirects non-admin to /, anonymous to /login.html"
```

---

## Phase 2 — Static admin UI

### Task 2 — `web/admin/index.html` shell

**Files:**
- Create: `web/admin/index.html`

- [ ] **Step 1: Write the file**

Use the same layout-shell pattern as PR B. Reference `web/settings.html` for the meta + topbar placeholder + script-import structure.

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<meta name="page" content="admin" />
<title>AT Term · admin</title>
<link rel="stylesheet" href="../style.css" />
<style>
.admin-page {
  min-height: 100vh;
  background: radial-gradient(circle at top left, #1e1b4b 0, var(--bg) 42%);
  padding: 2rem 1rem;
}
.admin-wrap {
  max-width: 980px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}
.admin-card {
  padding: 1.5rem 2rem;
  background: var(--panel);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 18px;
  display: flex;
  flex-direction: column;
  gap: 1rem;
  box-shadow: 0 18px 60px rgba(0, 0, 0, 0.36);
}
.admin-card h2 {
  margin: 0;
  font-size: 1rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--fg-dim);
}
.admin-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.admin-table th, .admin-table td {
  text-align: left;
  padding: 0.45rem 0.5rem;
  border-bottom: 1px solid var(--border);
  vertical-align: top;
}
.admin-table th {
  font-weight: 600;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-dim);
}
.admin-table td code {
  font-size: 11px;
  color: var(--fg-dim);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.admin-secret {
  background: rgba(217, 153, 33, 0.12);
  border: 1px solid var(--warn, #d29922);
  border-radius: 6px;
  padding: 0.75rem;
}
.admin-secret code {
  display: block;
  padding: 0.4rem 0.5rem;
  background: rgba(0, 0, 0, 0.3);
  border-radius: 3px;
  user-select: all;
  word-break: break-all;
  font-size: 13px;
  margin: 0.4rem 0;
}
.admin-error { color: var(--bad); margin: 0.5rem 0; min-height: 1.2em; font-size: 13px; }
.admin-row { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; }
</style>
</head>
<body class="admin-page">
<header id="topbar"></header>

<div class="admin-wrap">
  <nav class="subtabs" aria-label="Admin sections">
    <a href="#invitations" data-tab="invitations" class="subtab active">Invitations</a>
    <a href="#users" data-tab="users" class="subtab">Users</a>
    <a href="#config" data-tab="config" class="subtab">Config</a>
  </nav>

  <section id="panel-invitations" class="admin-card" data-panel="invitations">
    <h2>Create invitation</h2>
    <div class="admin-row">
      <input id="inv-note" placeholder="note (optional)" style="flex:1;min-width:180px" />
      <input id="inv-count" type="number" min="1" max="50" value="1" title="how many invites (1-50)" style="width:80px" />
      <input id="inv-expires" type="datetime-local" title="expires at; defaults to now + 7 days" />
      <button id="inv-create" type="button">Create</button>
    </div>
    <div id="inv-secret"></div>
    <div id="inv-error" class="admin-error"></div>

    <h2>All invitations <button id="inv-refresh" type="button">refresh</button></h2>
    <table class="admin-table" id="inv-table">
      <thead><tr><th>Prefix</th><th>Note</th><th>Created</th><th>Expires</th><th>Consumed</th><th>By</th></tr></thead>
      <tbody></tbody>
    </table>
  </section>

  <section id="panel-users" class="admin-card" data-panel="users" hidden>
    <h2>All users <button id="users-refresh" type="button">refresh</button></h2>
    <div id="users-secret"></div>
    <table class="admin-table" id="users-table">
      <thead><tr><th>Email</th><th>ID</th><th>Created</th><th>Status</th><th>Actions</th></tr></thead>
      <tbody></tbody>
    </table>
    <div id="users-error" class="admin-error"></div>
  </section>

  <section id="panel-config" class="admin-card" data-panel="config" hidden>
    <h2>Runtime limits <button id="config-load" type="button">reload</button></h2>
    <div id="config-form" hidden>
      <div class="admin-row">
        <label style="min-width:260px">Rate limit (requests/min per IP+token):</label>
        <input id="cfg-rate" type="number" style="width:100px" />
        <span class="muted" id="cfg-rate-eff"></span>
      </div>
      <div class="admin-row">
        <label style="min-width:260px">Max WS connections (per IP+token):</label>
        <input id="cfg-conn" type="number" style="width:100px" />
        <span class="muted" id="cfg-conn-eff"></span>
      </div>
      <p class="muted">
        <strong>0</strong> = use built-in default; <strong>negative</strong> = disable the limit.
        Changes apply immediately and persist to the admin config file.
      </p>
      <div class="admin-row">
        <button id="config-save" type="button">Save</button>
        <span class="muted">Version: <code id="cfg-version"></code></span>
      </div>
    </div>
    <pre id="config-out" style="display:none"></pre>
    <div id="config-error" class="admin-error"></div>
  </section>
</div>

<p class="page-version" id="version">version dev</p>
<script type="module" src="../layout.js"></script>
<script type="module" src="./admin.js"></script>
<script type="module" src="./admin-invitations.js"></script>
<script type="module" src="./admin-users.js"></script>
</body>
</html>
```

Key points:
- The layout-shell topbar comes from `../layout.js` (note relative path: admin/ is a subdir, so `../layout.js` and `../style.css`).
- Subtabs use the existing `.subtabs` styling from PR C.
- Each panel mirrors the structure of the original `adminPageHTML` (from the deleted PR A const). API endpoints unchanged.

- [ ] **Step 2: Verify the file serves**

```bash
go build -o /tmp/atterm-relay-prd ./cmd/atterm-relay
/tmp/atterm-relay-prd --addr 127.0.0.1:18111 --web web --dev-insecure > /tmp/relay-prd.log 2>&1 &
PID=$!
sleep 1
curl -isI http://127.0.0.1:18111/admin/index.html | head -3
# Expected: 302 (anonymous → /login.html — admin gate from Task 1)
kill $PID 2>/dev/null; wait $PID 2>/dev/null
```

The 302 confirms the gate is working. To actually see the page rendered you'd need an admin cookie — defer to the manual smoke in Task 9.

- [ ] **Step 3: Commit**

```bash
git add web/admin/index.html
git commit -m "web(admin): static shell with Invitations/Users/Config subtabs"
```

---

### Task 3 — `web/admin/admin.js` (tab switching + Config panel)

**Files:**
- Create: `web/admin/admin.js`

- [ ] **Step 1: Write the file**

```js
// admin.js — shared tab switching + Config panel logic.
//
// Companion modules:
//   admin-invitations.js  — Invitations panel
//   admin-users.js        — Users panel (including promote/demote)
//
// All three modules load in parallel and only touch DOM ids inside
// their own panel; they don't coordinate. authFetch from ../auth.js
// adds the CSRF header automatically.

import { authFetch } from "../auth.js";

const TABS = ["invitations", "users", "config"];

function showTab(name) {
    if (!TABS.includes(name)) name = "invitations";
    for (const t of TABS) {
        const link = document.querySelector(`.subtab[data-tab="${t}"]`);
        const panel = document.querySelector(`[data-panel="${t}"]`);
        if (!link || !panel) continue;
        if (t === name) {
            link.classList.add("active");
            panel.hidden = false;
        } else {
            link.classList.remove("active");
            panel.hidden = true;
        }
    }
    // Config panel loads lazily on first activation.
    if (name === "config" && !configLoaded) {
        loadConfig();
    }
}

function activeFromHash() {
    const h = (location.hash || "").replace(/^#/, "");
    return TABS.includes(h) ? h : "invitations";
}

document.addEventListener("DOMContentLoaded", () => {
    showTab(activeFromHash());
});
window.addEventListener("hashchange", () => {
    showTab(activeFromHash());
});

// ─── Config panel ──────────────────────────────────────────────────

let configLoaded = false;
const cfgRate = () => document.getElementById("cfg-rate");
const cfgConn = () => document.getElementById("cfg-conn");
const cfgRateEff = () => document.getElementById("cfg-rate-eff");
const cfgConnEff = () => document.getElementById("cfg-conn-eff");
const cfgVersion = () => document.getElementById("cfg-version");
const configForm = () => document.getElementById("config-form");
const configErr = () => document.getElementById("config-error");
const configOut = () => document.getElementById("config-out");

async function loadConfig() {
    configErr().textContent = "";
    try {
        const res = await authFetch("/admin/api/config");
        if (!res.ok) throw new Error("HTTP " + res.status);
        const c = await res.json();
        cfgRate().value = c.rate_limit_per_minute ?? 0;
        cfgConn().value = c.max_connections_per_key ?? 0;
        cfgRateEff().textContent = `(effective: ${c.effective_rate_limit_per_minute ?? "?"})`;
        cfgConnEff().textContent = `(effective: ${c.effective_max_connections_per_key ?? "?"})`;
        cfgVersion().textContent = c.version ?? "";
        configForm().hidden = false;
        configOut().style.display = "none";
        configLoaded = true;
    } catch (e) {
        configErr().textContent = "Failed to load config: " + e.message;
    }
}

async function saveConfig() {
    configErr().textContent = "";
    const body = {
        rate_limit_per_minute: parseInt(cfgRate().value, 10) || 0,
        max_connections_per_key: parseInt(cfgConn().value, 10) || 0,
    };
    try {
        const res = await authFetch("/admin/api/config", {
            method: "PUT",
            body: JSON.stringify(body),
        });
        if (!res.ok) throw new Error("HTTP " + res.status);
        const c = await res.json();
        configOut().textContent = JSON.stringify(c, null, 2);
        configOut().style.display = "block";
        // Refresh effective values.
        cfgRateEff().textContent = `(effective: ${c.effective_rate_limit_per_minute ?? "?"})`;
        cfgConnEff().textContent = `(effective: ${c.effective_max_connections_per_key ?? "?"})`;
    } catch (e) {
        configErr().textContent = "Save failed: " + e.message;
    }
}

document.getElementById("config-load")?.addEventListener("click", loadConfig);
document.getElementById("config-save")?.addEventListener("click", saveConfig);
```

- [ ] **Step 2: Commit**

```bash
git add web/admin/admin.js
git commit -m "web(admin): tab switching + Config panel (rate limit / max connections)"
```

(Manual smoke deferred to Task 9.)

---

### Task 4 — `web/admin/admin-invitations.js`

**Files:**
- Create: `web/admin/admin-invitations.js`

- [ ] **Step 1: Write**

```js
// admin-invitations.js — Invitations panel: create + list + refresh.

import { authFetch } from "../auth.js";

const $ = (id) => document.getElementById(id);
const fmt = (s) => (s ? new Date(s).toLocaleString() : "");

async function createInvite() {
    const note = $("inv-note").value.trim();
    const count = Math.max(1, Math.min(50, parseInt($("inv-count").value, 10) || 1));
    const expRaw = $("inv-expires").value;
    const body = { count };
    if (note) body.note = note;
    if (expRaw) body.expires_at = new Date(expRaw).toISOString();

    $("inv-error").textContent = "";
    try {
        const res = await authFetch("/admin/api/invitations", { method: "POST", body: JSON.stringify(body) });
        if (!res.ok) throw new Error("HTTP " + res.status);
        const data = await res.json();
        const invites = data.invites || [data];
        const wrap = $("inv-secret");
        wrap.innerHTML = "";
        for (const inv of invites) {
            const div = document.createElement("div");
            div.className = "admin-secret";
            const p = document.createElement("p");
            p.textContent = `New invite${inv.note ? ` (${inv.note})` : ""} — copy it now, it's only shown once.`;
            const code = document.createElement("code");
            code.textContent = inv.plaintext;
            div.appendChild(p);
            div.appendChild(code);
            wrap.appendChild(div);
        }
        $("inv-note").value = "";
        await refreshInvites();
    } catch (e) {
        $("inv-error").textContent = "Create failed: " + e.message;
    }
}

async function refreshInvites() {
    $("inv-error").textContent = "";
    try {
        const res = await authFetch("/admin/api/invitations");
        if (!res.ok) throw new Error("HTTP " + res.status);
        const rows = (await res.json()) || [];
        const tbody = $("inv-table").querySelector("tbody");
        tbody.innerHTML = "";
        for (const r of rows) {
            const tr = document.createElement("tr");
            tr.innerHTML = `
              <td><code>${r.code_prefix || ""}</code></td>
              <td>${r.note || ""}</td>
              <td>${fmt(r.created_at)}</td>
              <td>${fmt(r.expires_at)}</td>
              <td>${fmt(r.consumed_at)}</td>
              <td>${r.consumed_by || ""}</td>`;
            tbody.appendChild(tr);
        }
    } catch (e) {
        $("inv-error").textContent = "Load failed: " + e.message;
    }
}

$("inv-create")?.addEventListener("click", createInvite);
$("inv-refresh")?.addEventListener("click", refreshInvites);

// Initial fetch.
refreshInvites();
```

- [ ] **Step 2: Commit**

```bash
git add web/admin/admin-invitations.js
git commit -m "web(admin): Invitations panel — create + list + refresh"
```

---

### Task 5 — `web/admin/admin-users.js` (with promote/demote)

**Files:**
- Create: `web/admin/admin-users.js`

- [ ] **Step 1: Write**

```js
// admin-users.js — Users panel: list + per-user actions (promote /
// demote / reset password / disable).

import { authFetch } from "../auth.js";

const $ = (id) => document.getElementById(id);
const fmt = (s) => (s ? new Date(s).toLocaleString() : "");

async function refreshUsers() {
    $("users-error").textContent = "";
    try {
        const res = await authFetch("/admin/api/users");
        if (!res.ok) throw new Error("HTTP " + res.status);
        const rows = (await res.json()) || [];
        const tbody = $("users-table").querySelector("tbody");
        tbody.innerHTML = "";
        for (const u of rows) {
            const tr = document.createElement("tr");
            const status = u.disabled_at
                ? `<span style="color:var(--bad)">disabled ${fmt(u.disabled_at)}</span>`
                : u.is_admin
                    ? '<span style="color:var(--accent)">admin</span>'
                    : "active";
            const actionBtns = u.disabled_at ? "" : actionButtons(u);
            tr.innerHTML = `
              <td>${u.email}</td>
              <td><code>${u.id}</code></td>
              <td>${fmt(u.created_at)}</td>
              <td>${status}</td>
              <td>${actionBtns}</td>`;
            tbody.appendChild(tr);
        }
        // Wire button clicks (rebuilt every refresh, so always fresh).
        tbody.querySelectorAll("button[data-action]").forEach((btn) => {
            btn.addEventListener("click", () => onAction(btn));
        });
    } catch (e) {
        $("users-error").textContent = "Load failed: " + e.message;
    }
}

function actionButtons(u) {
    const adminBtn = u.is_admin
        ? `<button data-action="demote" data-uid="${u.id}">Demote</button>`
        : `<button data-action="promote" data-uid="${u.id}">Promote</button>`;
    return `${adminBtn}
      <button data-action="reset" data-uid="${u.id}">Reset password</button>
      <button data-action="disable" data-uid="${u.id}" class="btn-danger">Disable</button>`;
}

async function onAction(btn) {
    const action = btn.dataset.action;
    const uid = btn.dataset.uid;
    btn.disabled = true;
    $("users-error").textContent = "";
    $("users-secret").innerHTML = "";
    try {
        if (action === "promote") {
            const res = await authFetch(`/admin/api/users/${uid}/admin`, { method: "POST" });
            if (!res.ok) throw new Error(await errText(res));
        } else if (action === "demote") {
            const res = await authFetch(`/admin/api/users/${uid}/admin`, { method: "DELETE" });
            if (!res.ok) throw new Error(await errText(res));
        } else if (action === "reset") {
            const res = await authFetch(`/admin/api/users/${uid}/reset-password`, { method: "POST" });
            if (!res.ok) throw new Error(await errText(res));
            const data = await res.json();
            showSecret(`Password reset — temporary password (copy now):`, data.plaintext);
        } else if (action === "disable") {
            if (!confirm("Disable this user? They will be signed out and can no longer log in.")) {
                btn.disabled = false;
                return;
            }
            const res = await authFetch(`/admin/api/users/${uid}/disable`, { method: "POST" });
            if (!res.ok) throw new Error(await errText(res));
        }
        await refreshUsers();
    } catch (e) {
        $("users-error").textContent = `${action} failed: ${e.message}`;
        btn.disabled = false;
    }
}

async function errText(res) {
    try {
        const j = await res.json();
        return j.error || `HTTP ${res.status}`;
    } catch (_) {
        return `HTTP ${res.status}`;
    }
}

function showSecret(label, plaintext) {
    const wrap = $("users-secret");
    const div = document.createElement("div");
    div.className = "admin-secret";
    const p = document.createElement("p");
    p.textContent = label;
    const code = document.createElement("code");
    code.textContent = plaintext;
    div.appendChild(p);
    div.appendChild(code);
    wrap.appendChild(div);
}

$("users-refresh")?.addEventListener("click", refreshUsers);

// Initial fetch.
refreshUsers();
```

- [ ] **Step 2: Commit**

```bash
git add web/admin/admin-users.js
git commit -m "web(admin): Users panel — list + promote/demote/reset/disable"
```

---

## Phase 3 — Tests + sw + ship

### Task 6 — `web/admin/admin.test.mjs`

**Files:**
- Create: `web/admin/admin.test.mjs`

- [ ] **Step 1: Write**

```js
// PR D: admin static UI under web/admin/. Assert the structural
// contract — page metadata, panels, script imports, endpoint wiring.
//
// Runtime DOM tests would need jsdom; instead we statically grep the
// source for the endpoints each module hits. The admin API itself is
// covered by internal/relay/admin_http_test.go.

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

test("admin/index.html declares <meta name=\"page\" content=\"admin\">", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    assert.match(html, /<meta\s+name="page"\s+content="admin"/);
});

test("admin/index.html has empty #topbar placeholder for layout.js", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    assert.match(html, /<header\s+id="topbar"\s*>\s*<\/header>/);
});

test("admin/index.html imports layout.js (one dir up)", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    assert.match(html, /<script\s+type="module"\s+src="\.\.\/layout\.js"/);
});

test("admin/index.html has all 3 sub-tab anchors", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    for (const tab of ["invitations", "users", "config"]) {
        const re = new RegExp(`<a [^>]*data-tab="${tab}"`);
        assert.match(html, re);
    }
});

test("admin/index.html has all 3 panels", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    for (const panel of ["invitations", "users", "config"]) {
        const re = new RegExp(`<section [^>]*data-panel="${panel}"`);
        assert.match(html, re);
    }
});

test("admin/index.html imports admin.js + admin-invitations.js + admin-users.js", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    assert.match(html, /src="\.\/admin\.js"/);
    assert.match(html, /src="\.\/admin-invitations\.js"/);
    assert.match(html, /src="\.\/admin-users\.js"/);
});

test("admin.js calls /admin/api/config (GET + PUT)", () => {
    const js = readFileSync("web/admin/admin.js", "utf8");
    assert.match(js, /\/admin\/api\/config/);
    assert.match(js, /method:\s*"PUT"/);
});

test("admin-invitations.js calls POST /admin/api/invitations + GET list", () => {
    const js = readFileSync("web/admin/admin-invitations.js", "utf8");
    assert.match(js, /\/admin\/api\/invitations/);
    assert.match(js, /method:\s*"POST"/);
});

test("admin-users.js wires promote (POST) + demote (DELETE) admin endpoints", () => {
    const js = readFileSync("web/admin/admin-users.js", "utf8");
    assert.match(js, /\/admin\/api\/users\/.+\/admin/);
    assert.match(js, /method:\s*"POST"/);
    assert.match(js, /method:\s*"DELETE"/);
});

test("admin-users.js wires reset-password + disable endpoints", () => {
    const js = readFileSync("web/admin/admin-users.js", "utf8");
    assert.match(js, /\/admin\/api\/users\/.+\/reset-password/);
    assert.match(js, /\/admin\/api\/users\/.+\/disable/);
});
```

- [ ] **Step 2: Run**

```bash
node --test web/admin/admin.test.mjs 2>&1 | tail -10
```

Expected: 10 PASS.

- [ ] **Step 3: Commit**

```bash
git add web/admin/admin.test.mjs
git commit -m "web(admin): structural + endpoint-wiring tests"
```

---

### Task 7 — sw cache: precache admin/ files

**Files:**
- Modify: `web/sw.js`
- Modify: `web/sw-cache-bump.test.mjs` (only if the resolver doesn't already understand subpath assets — see note below)

- [ ] **Step 1: Add admin/ files to ASSETS**

In `web/sw.js`:

```js
const ASSETS = [
  "./",
  "./admin/admin-invitations.js",
  "./admin/admin-users.js",
  "./admin/admin.js",
  "./app-core.js",
  "./app.js",
  ...
];
```

(Alphabetical at the top alongside the other modules. Admin's index.html is gated, so don't precache it.)

- [ ] **Step 2: Verify the hash resolver understands subpaths**

The sw-cache-bump test reads each ASSETS path relative to `web/`. If `"./admin/admin.js"` resolves to `web/admin/admin.js`, no change is needed. Run:

```bash
node --test web/sw-cache-bump.test.mjs 2>&1 | tail -10
```

The test will either:
- Fail with the new expected hash → paste into `CACHE` (most likely), OR
- Fail with `ENOENT: no such file or directory, open 'web/./admin/admin.js'` → the resolver is using `path.join("web", asset)` which is fine but maybe a duplicate slash trip-up. Inspect `web/sw-cache-bump.test.mjs` for how it joins; should be path.join which collapses the `./`.

If you hit a resolver problem, fix `sw-cache-bump.test.mjs` to normalize the path (`asset.replace(/^\.\//, "")` before `path.join("web", asset)`).

- [ ] **Step 3: Update CACHE**

Paste the new 8-hex hash into `web/sw.js`:

```js
const CACHE = "at-term-web-<new hash>";
```

- [ ] **Step 4: Full test sweep**

```bash
node --test web/*.test.mjs web/admin/*.test.mjs 2>&1 | tail -8
```

Expected: 0 failures.

- [ ] **Step 5: Commit**

```bash
git add web/sw.js [web/sw-cache-bump.test.mjs if you fixed the resolver]
git commit -m "web(sw): precache admin/* modules; cache hash bump"
```

---

### Task 8 — Final sweep + manual smoke

- [ ] **Step 1: Go + web tests**

```bash
go test -count=1 -timeout 120s ./... 2>&1 | tail -10
node --test web/*.test.mjs web/admin/*.test.mjs 2>&1 | tail -8
```

Expected: 0 failures in either.

- [ ] **Step 2: End-to-end smoke**

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL=you@example.com \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web --dev-insecure
```

Open `http://127.0.0.1:8080/login.html`, log in:

- [ ] Visit `/admin/` directly — page renders (admin gate allowed admin in).
- [ ] Topnav shows brand + Home + Settings + **Admin** + Sign Out.
- [ ] "Invitations" tab is active by default. Click "Create" → invite plaintext appears in the secret box. Refresh button reloads the list.
- [ ] Click "Users". Bootstrap admin row shows status "admin". Other users (if any) show Promote button. Click Promote on a non-admin → status flips to "admin", button becomes Demote.
- [ ] Click "Config". Default values load. Change rate limit, Save → response JSON appears.
- [ ] Click "Sign out" → redirect to /login.html.

- [ ] **Step 3: Verify gates**

In an incognito window (no cookie):

```bash
curl -is http://127.0.0.1:8080/admin/ | head -3
# Expected: 302 Location: /login.html
```

As a non-admin user (sign up via invite):

```bash
# After login as non-admin:
curl -is -b "atterm_session=<the cookie>" http://127.0.0.1:8080/admin/ | head -3
# Expected: 302 Location: /
```

Both gates behave as designed.

- [ ] **Step 4: Mark ready to ship.**

---

### Task 9 — Ship `v0.1.76`

- [ ] **Step 1: Push + PR**

```bash
git push -u origin feat/admin-static-ui

gh pr create --title "feat(web): static admin UI with promote/demote + /admin/ gate" --body "$(cat <<'EOF'
## Summary

PR D of the web UI redesign. Restores /admin/ as a static page consistent with the layout shell, and adds the promote/demote UI for the Users panel.

- web/admin/index.html — layout-shell page with 3 sub-tabs (Invitations / Users / Config)
- web/admin/admin.js — tab switching + Config panel (rate limit / max connections)
- web/admin/admin-invitations.js — create + list + refresh
- web/admin/admin-users.js — list with status, promote/demote/reset/disable per row
- web/admin/admin.test.mjs — structural + endpoint-wiring tests
- internal/relay/server.go::newStaticHandler — gate /admin/ (anonymous → /login.html, non-admin → /)
- internal/relay/static_handler_test.go — gate tests
- web/sw.js — precache admin/* modules; hash bump

## Test plan

- [x] go test ./... — all OK
- [x] node --test web/*.test.mjs web/admin/*.test.mjs — all OK
- [ ] After deploy: hard-reload PWA; visit /admin/ as admin — page renders, all 3 tabs work
- [ ] Anonymous → /admin/ — 302 /login.html
- [ ] Non-admin user → /admin/ — 302 /
- [ ] Promote a user → table updates, button flips to Demote
- [ ] Demote the only other admin → succeeds; demote yourself → 400 cannot_demote_self
EOF
)"
```

- [ ] **Step 2: Squash + tag**

```bash
gh pr merge <number> --squash
git fetch origin main
SHA=$(gh pr view <number> --json mergeCommit -q .mergeCommit.oid)
git tag v0.1.76 $SHA
git push origin v0.1.76
git push origin --delete feat/admin-static-ui
gh run list --limit 3
```

---

## Done Criteria

- All 9 tasks complete with green commits.
- All Go + web tests pass.
- v0.1.76 tag pushed; Release workflow succeeded.
- /admin/ renders for admin users with working 3-tab UI; gate redirects others correctly.
- PR E (design tokens audit + login/signup polish) can be written.

## Out of Scope

- Design tokens audit + login/signup polish — PR E.
- New admin features beyond what was already in the deleted adminPageHTML + promote/demote.
- Removing the static handler's defensive `contains()` helper — small enough to leave inline.
