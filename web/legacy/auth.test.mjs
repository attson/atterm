import { test, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";

// auth.js uses module-level `cachedCSRF`. We need to reset it between tests.
// Strategy: re-import fresh each test is not possible with static ESM.
// Instead we reset by calling getMe() with a response that has no csrf_token
// at the start of each test that needs a clean state.
import { authFetch, getMe, login, signup, logout } from "./auth.js";

// ── helpers ──────────────────────────────────────────────────────────────────

let origFetch;
let origLocation;

beforeEach(() => {
    origFetch = globalThis.fetch;
    origLocation = globalThis.location;
});

afterEach(() => {
    globalThis.fetch = origFetch;
    globalThis.location = origLocation;
});

/** Fake a successful GET /api/me that sets cachedCSRF to the given value. */
async function seedCSRF(token) {
    globalThis.fetch = async () => ({
        ok: true,
        status: 200,
        json: async () => ({ user_id: "u1", email: "a@b.com", csrf_token: token }),
    });
    await getMe();
}

/** Fake a successful GET /api/me with no csrf_token (clears cache). */
async function clearCSRF() {
    globalThis.fetch = async () => ({
        ok: true,
        status: 200,
        json: async () => ({ user_id: "u1", email: "a@b.com" }),
    });
    await getMe();
}

// ── tests ─────────────────────────────────────────────────────────────────────

test("authFetch adds X-CSRF-Token for mutating verbs when csrf is cached", async () => {
    // First seed the CSRF cache via getMe().
    await seedCSRF("tok-abc");

    // Now capture what headers the next POST sends.
    const captured = { headers: null };
    globalThis.fetch = async (url, init) => {
        captured.headers = init.headers;
        return { ok: true, status: 200 };
    };

    await authFetch("/api/me/tokens", { method: "POST", body: "{}" });

    // Headers is a Headers instance — convert to plain object for assertion.
    const csrfHeader = captured.headers.get("X-CSRF-Token");
    assert.equal(csrfHeader, "tok-abc");
});

test("authFetch redirects on 401", async () => {
    // Clear cache so the test's POST doesn't accidentally succeed
    await clearCSRF();

    // Stub fetch to return 401
    globalThis.fetch = async () => ({ ok: false, status: 401 });

    // Spy on location.assign
    const assigned = [];
    globalThis.location = { assign: (url) => assigned.push(url) };

    // authFetch throws after calling location.assign
    await assert.rejects(
        () => authFetch("/api/protected", { method: "GET" }),
        /redirected to login/,
    );

    assert.equal(assigned.length, 1);
    assert.equal(assigned[0], "/login.html");
});

test("getMe caches csrf_token from response", async () => {
    // getMe() call with a specific csrf_token
    globalThis.fetch = async () => ({
        ok: true,
        status: 200,
        json: async () => ({ user_id: "u1", email: "a@b.com", csrf_token: "csrf-xyz" }),
    });
    const me = await getMe();
    assert.equal(me.user_id, "u1");

    // Subsequent mutating request should carry the cached token
    const captured = { headers: null };
    globalThis.fetch = async (url, init) => {
        captured.headers = init.headers;
        return { ok: true, status: 200 };
    };

    await authFetch("/api/anything", { method: "POST", body: "{}" });

    assert.equal(captured.headers.get("X-CSRF-Token"), "csrf-xyz");
});
