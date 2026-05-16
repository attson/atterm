import { login } from "./auth.js";
import { fetchVersionLabel } from "./app-core.js";

fetchVersionLabel(fetch).then((label) => {
    const el = document.getElementById("version");
    if (el) el.textContent = label;
});

document.getElementById("login-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const err = document.getElementById("error");
    err.hidden = true;
    try {
        await login(document.getElementById("email").value, document.getElementById("password").value);
        location.assign("/");
    } catch (e) {
        err.textContent = "Invalid email or password.";
        err.hidden = false;
    }
});
