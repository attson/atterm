// Settings → Danger zone: hard-delete account. UI requires email
// match (typo-protection) AND current password (anti-CSRF defense if
// the cookie is somehow stolen). Server also enforces both checks
// plus the last-admin guard.

import { authFetch } from "./auth.js";

const form = document.getElementById("delete-account-form");
const emailEl = document.getElementById("delete-email");
const pwdEl = document.getElementById("delete-password");
const errEl = document.getElementById("delete-error");

function showErr(msg) {
    if (!errEl) return;
    errEl.hidden = false;
    errEl.textContent = msg;
}

if (form) {
    form.addEventListener("submit", async (e) => {
        e.preventDefault();
        errEl.hidden = true;
        if (!confirm("Permanently delete this account? This cannot be undone.")) return;
        try {
            const res = await authFetch("/api/me", {
                method: "DELETE",
                body: JSON.stringify({ email: emailEl.value.trim(), password: pwdEl.value }),
            });
            if (res.status === 204) {
                // Cookie was cleared by the server. Send the user back to
                // the login page; account is gone.
                location.assign("/login.html");
                return;
            }
            let msg = `Delete failed (status ${res.status})`;
            try {
                const body = await res.json();
                if (body.error === "email_mismatch") msg = "Email doesn't match — type your exact email.";
                else if (body.error === "password_incorrect") msg = "Password is incorrect.";
                else if (body.error === "last_admin") msg = "You're the last admin — promote another user first.";
            } catch (_) {}
            showErr(msg);
        } catch (e) {
            showErr("Network error: " + e.message);
        }
    });
}
