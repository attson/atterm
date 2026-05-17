// web/auth.js — ESM module; loaded by login.html / signup.html / settings.html / app.js.
let cachedCSRF = "";

export async function authFetch(url, init = {}) {
    const method = (init.method || "GET").toUpperCase();
    const headers = new Headers(init.headers || {});
    if (method !== "GET" && method !== "HEAD") {
        if (cachedCSRF) headers.set("X-CSRF-Token", cachedCSRF);
        if (!headers.has("Content-Type") && init.body) {
            headers.set("Content-Type", "application/json");
        }
    }
    const res = await fetch(url, { ...init, headers, credentials: "same-origin" });
    if (res.status === 401) {
        location.assign("/login.html");
        throw new Error("redirected to login");
    }
    return res;
}

export async function getMe() {
    const res = await authFetch("/api/me");
    if (!res.ok) throw new Error("getMe " + res.status);
    const j = await res.json();
    cachedCSRF = j.csrf_token || "";
    return j;
}

export async function login(email, password) {
    const res = await authFetch("/api/auth/login", {
        method: "POST", body: JSON.stringify({ email, password }),
    });
    if (!res.ok) throw new Error("login " + res.status);
    return await getMe();  // populate csrf cache
}

export async function signup(email, password, invite_code) {
    const res = await authFetch("/api/auth/signup", {
        method: "POST",
        body: JSON.stringify({ email, password, invite_code }),
    });
    if (!res.ok) throw new Error("signup " + res.status);
    return await getMe();
}

export async function logout() {
    await authFetch("/api/auth/logout", { method: "POST" });
    cachedCSRF = "";
    location.assign("/login.html");
}
