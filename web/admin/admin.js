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
