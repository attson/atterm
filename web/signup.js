import { signup } from "./auth.js";

document.getElementById("signup-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const err = document.getElementById("error");
    err.hidden = true;
    try {
        await signup(
            document.getElementById("email").value,
            document.getElementById("password").value,
            document.getElementById("invite").value,
        );
        location.assign("/");
    } catch (e) {
        err.textContent = "Sign-up failed. Check your invite code and try again.";
        err.hidden = false;
    }
});
