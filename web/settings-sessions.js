// Settings → Signed-in devices: list, revoke single, sign-out-others.
//
// Loaded as a separate module so the API Tokens / Change Password
// panel logic stays in settings.js. All three modules co-load and
// access the same DOM ids without coordination.

import { authFetch } from "./auth.js";

const listEl = document.getElementById("sessions-list");
const errEl = document.getElementById("sessions-error");
const signOutOthersBtn = document.getElementById("sign-out-others");

function fmtDate(ms) {
    if (!ms) return "";
    return new Date(ms).toLocaleString();
}

function fmtUA(ua) {
    if (!ua) return "Unknown device";
    // Coarse simplification — full UA strings are noisy.
    if (ua.includes("Firefox")) return "Firefox";
    if (ua.includes("Edg/")) return "Edge";
    if (ua.includes("Chrome")) return "Chrome";
    if (ua.includes("Safari")) return "Safari";
    return ua.length > 40 ? ua.slice(0, 40) + "…" : ua;
}

async function loadSessions() {
    if (!listEl) return;
    try {
        const res = await authFetch("/api/me/sessions");
        if (!res.ok) throw new Error("HTTP " + res.status);
        const rows = await res.json();
        if (!rows || rows.length === 0) {
            listEl.innerHTML = '<li id="sessions-empty">No active sessions.</li>';
            return;
        }
        listEl.innerHTML = "";
        for (const row of rows) {
            const li = document.createElement("li");
            const ua = document.createElement("div");
            ua.className = "session-ua";
            ua.textContent = fmtUA(row.user_agent) + (row.is_current ? "  (this device)" : "");
            const meta = document.createElement("div");
            meta.className = "session-meta";
            meta.textContent = `signed in ${fmtDate(row.created_at)} · ${row.ip_prefix || "ip unknown"}`;
            li.appendChild(ua);
            li.appendChild(meta);
            if (!row.is_current) {
                const btn = document.createElement("button");
                btn.className = "btn-danger";
                btn.textContent = "Revoke";
                btn.addEventListener("click", () => revokeSession(row.id_hash, btn));
                li.appendChild(btn);
            } else {
                const tag = document.createElement("span");
                tag.className = "session-current";
                tag.textContent = "current";
                li.appendChild(tag);
            }
            listEl.appendChild(li);
        }
    } catch (e) {
        listEl.innerHTML = "";
        showErr("Failed to load sessions: " + e.message);
    }
}

async function revokeSession(idHash, btn) {
    btn.disabled = true;
    try {
        const res = await authFetch(`/api/me/sessions/${encodeURIComponent(idHash)}`, { method: "DELETE" });
        if (res.status !== 204) throw new Error("HTTP " + res.status);
        await loadSessions();
    } catch (e) {
        btn.disabled = false;
        showErr("Revoke failed: " + e.message);
    }
}

async function signOutOthers() {
    if (!signOutOthersBtn) return;
    if (!confirm("Sign out everywhere except this device?")) return;
    signOutOthersBtn.disabled = true;
    try {
        const res = await authFetch("/api/me/sessions/sign-out-others", { method: "POST" });
        if (!res.ok) throw new Error("HTTP " + res.status);
        await loadSessions();
    } catch (e) {
        showErr("Sign-out-others failed: " + e.message);
    } finally {
        signOutOthersBtn.disabled = false;
    }
}

function showErr(msg) {
    if (!errEl) return;
    errEl.hidden = false;
    errEl.textContent = msg;
}

// Load when the Sessions tab is first shown — also on initial page
// load if that's the active tab. Cheapest: just fetch on module init.
loadSessions();

if (signOutOthersBtn) {
    signOutOthersBtn.addEventListener("click", signOutOthers);
}

// Refresh when the user switches into the Sessions tab.
window.addEventListener("hashchange", () => {
    if (location.hash === "#sessions") loadSessions();
});
