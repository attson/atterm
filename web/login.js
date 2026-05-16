import { login } from "./auth.js";

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
