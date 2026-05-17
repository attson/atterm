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
