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
