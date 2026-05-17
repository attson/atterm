// PR B introduces web/layout.js — a shared topnav shell. layout.js
// exports `renderTopbar(doc, active, isAdmin)` and `init(doc, fetchImpl)`
// so tests can drive it with stubbed DOM and network instead of the
// module-auto-load path.

import test from "node:test";
import assert from "node:assert/strict";

import { renderTopbar, init } from "./layout.js";

function makeEl() {
    const el = {
        id: "",
        innerHTML: "",
        textContent: "",
        addEventListener() {},
    };
    return el;
}

function makeDoc({ page = "home", elements = {} } = {}) {
    const map = new Map();
    for (const [id, el] of Object.entries(elements)) {
        el.id = id;
        map.set(id, el);
    }
    return {
        querySelector(sel) {
            if (sel === 'meta[name="page"]') return { content: page };
            return null;
        },
        getElementById(id) {
            return map.get(id) || null;
        },
    };
}

test("renderTopbar shows Admin link when isAdmin=true and marks Home active", () => {
    const topbar = makeEl();
    const doc = makeDoc({ elements: { topbar } });
    renderTopbar(doc, "home", true);
    assert.match(topbar.innerHTML, /href="\/"\s+class="active"[^>]*aria-current="page"/);
    assert.match(topbar.innerHTML, /href="\/settings\.html"/);
    assert.match(topbar.innerHTML, /href="\/admin\/"/);
    assert.match(topbar.innerHTML, /id="signout"/);
});

test("renderTopbar hides Admin link when isAdmin=false", () => {
    const topbar = makeEl();
    const doc = makeDoc({ elements: { topbar } });
    renderTopbar(doc, "settings", false);
    assert.doesNotMatch(topbar.innerHTML, /href="\/admin\/"/);
    assert.match(topbar.innerHTML, /href="\/settings\.html"\s+class="active"[^>]*aria-current="page"/);
});

test("renderTopbar with active=admin marks Admin link active when isAdmin=true", () => {
    const topbar = makeEl();
    const doc = makeDoc({ elements: { topbar } });
    renderTopbar(doc, "admin", true);
    assert.match(topbar.innerHTML, /href="\/admin\/"\s+class="active"[^>]*aria-current="page"/);
});

test("renderTopbar no-ops when #topbar missing (defensive)", () => {
    const doc = makeDoc({ elements: {} });
    // Should not throw.
    renderTopbar(doc, "home", true);
});

test("init renders synchronously before /api/me resolves, then re-renders with admin info", async () => {
    const topbar = makeEl();
    const doc = makeDoc({ page: "home", elements: { topbar } });

    // Slow /api/me that resolves to is_admin=true.
    let resolveMe;
    const mePromise = new Promise((resolve) => { resolveMe = resolve; });
    const fetchImpl = async (url) => {
        if (url === "/api/me") return await mePromise;
        if (url === "/api/version") return { ok: true, async json() { return { version: "v0-test" }; } };
        return { ok: false, status: 404 };
    };
    // auth.js's getMe uses the global fetch; we have to stub the global
    // because layout.js calls getMe() (not fetchImpl) for the /api/me path.
    const originalFetch = globalThis.fetch;
    globalThis.fetch = fetchImpl;

    const initPromise = init(doc, fetchImpl);

    // Synchronously, brand + nav (without Admin) + sign-out should already
    // be present, because renderTopbar(doc, page, false) ran before await.
    assert.match(topbar.innerHTML, /AT Term/);
    assert.match(topbar.innerHTML, /id="signout"/);
    assert.doesNotMatch(topbar.innerHTML, /href="\/admin\/"/);

    // Resolve /api/me; admin link should now appear after the await.
    resolveMe({
        ok: true,
        status: 200,
        async json() { return { user_id: "u1", email: "a@example.com", is_admin: true, csrf_token: "tok" }; },
    });
    await initPromise;
    assert.match(topbar.innerHTML, /href="\/admin\/"/);

    globalThis.fetch = originalFetch;
});

test("init silently tolerates /api/me failure (auth.js redirects internally)", async () => {
    const topbar = makeEl();
    const doc = makeDoc({ page: "home", elements: { topbar } });

    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (url) => {
        if (url === "/api/me") {
            // auth.js's authFetch redirects on 401 and throws — we just
            // emulate the throw here.
            throw new Error("simulated 401 redirect");
        }
        if (url === "/api/version") {
            return { ok: true, async json() { return { version: "v0-test" }; } };
        }
        return { ok: false, status: 404 };
    };

    // Stub location so the simulated redirect inside auth.js doesn't
    // blow up under node.
    globalThis.location = { assign() {} };

    // Must not throw.
    await init(doc, globalThis.fetch);

    // Brand still rendered from the sync first-paint.
    assert.match(topbar.innerHTML, /AT Term/);

    globalThis.fetch = originalFetch;
    delete globalThis.location;
});
