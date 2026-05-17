// Shared layout shell for authenticated pages (index, settings, admin).
//
// Each page declares its identity with <meta name="page" content="home"> /
// "settings" / "admin", an empty <header id="topbar"></header>, and a
// <script type="module" src="./layout.js"></script>. This module renders
// the brand + topnav + sign-out button into #topbar, with Admin tab
// shown only when GET /api/me indicates the current user has is_admin=true.

import { getMe, logout } from "./auth.js";
import { fetchVersionLabel } from "./app-core.js";

export function renderTopbar(doc, active, isAdmin) {
    const topbar = doc.getElementById("topbar");
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
    const btn = doc.getElementById("signout");
    if (btn) btn.addEventListener("click", () => logout());
}

export async function init(doc = document, fetchImpl = fetch) {
    const page = doc.querySelector('meta[name="page"]')?.content || "";
    // Synchronous first paint so the brand + nav + sign-out are visible
    // immediately, even while /api/me is in flight.
    renderTopbar(doc, page, false);
    try {
        const me = await getMe();
        renderTopbar(doc, page, !!me?.is_admin);
    } catch {
        // authFetch / getMe redirects to /login.html on 401; nothing to do.
    }
    fetchVersionLabel(fetchImpl).then((label) => {
        const el = doc.getElementById("version");
        if (el) el.textContent = label;
    });
}

if (typeof document !== "undefined") {
    init();
}
