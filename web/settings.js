import { authFetch, getMe, logout } from "./auth.js";
import { fetchVersionLabel } from "./app-core.js";

fetchVersionLabel(fetch).then((label) => {
    const el = document.getElementById("version");
    if (el) el.textContent = label;
});

async function loadTokens() {
    const res = await authFetch("/api/me/tokens");
    const tokens = await res.json();
    const list = document.getElementById("token-list");
    list.innerHTML = "";
    if (!tokens || tokens.length === 0) {
        const li = document.createElement("li");
        li.id = "token-list-empty";
        li.style.cssText = "color:var(--fg-dim);font-family:inherit;border:none;background:none;padding:0;";
        li.textContent = "No tokens yet.";
        list.appendChild(li);
        return;
    }
    for (const t of tokens) {
        if (t.revoked_at) continue; // skip revoked
        const li = document.createElement("li");
        const meta = document.createElement("span");
        meta.className = "token-meta";
        const created = new Date(t.created_at).toLocaleDateString();
        meta.textContent = `${t.name} · ${t.prefix}… · ${created}`;
        const btn = document.createElement("button");
        btn.className = "btn-danger";
        btn.textContent = "Revoke";
        btn.onclick = async () => {
            btn.disabled = true;
            await authFetch(`/api/me/tokens/${t.id}`, { method: "DELETE" });
            loadTokens();
        };
        li.appendChild(meta);
        li.appendChild(btn);
        list.appendChild(li);
    }
}

document.getElementById("create-token-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const name = document.getElementById("token-name").value.trim();
    if (!name) return;
    const res = await authFetch("/api/me/tokens", {
        method: "POST",
        body: JSON.stringify({ name }),
    });
    const data = await res.json();
    document.getElementById("plaintext-display").textContent = data.plaintext;
    document.getElementById("plaintext-section").hidden = false;
    document.getElementById("token-name").value = "";
    loadTokens();
});

document.getElementById("copy-token-btn").addEventListener("click", () => {
    const text = document.getElementById("plaintext-display").textContent;
    navigator.clipboard.writeText(text).catch(() => {});
});

document.getElementById("change-password-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const current = document.getElementById("current-password").value;
    const next = document.getElementById("new-password").value;
    const errEl = document.getElementById("password-error");
    errEl.hidden = true;
    const res = await authFetch("/api/me/password", {
        method: "POST",
        body: JSON.stringify({ current_password: current, new_password: next }),
    });
    if (res.ok) {
        alert("Password changed. You are signed out of other devices.");
        location.assign("/login.html");
    } else {
        let msg = "Failed (status " + res.status + ")";
        try {
            const body = await res.json();
            if (body.error === "current_password_wrong") msg = "Current password is incorrect.";
            else if (body.error === "password_weak") msg = "New password must be at least 12 characters.";
        } catch (_) {}
        errEl.textContent = msg;
        errEl.hidden = false;
    }
});

document.getElementById("logout").addEventListener("click", () => logout());

// Guard: redirect to login if not authenticated, then load tokens.
await getMe();
await loadTokens();
