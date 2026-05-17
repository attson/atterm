import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const html = readFileSync("web/settings.html", "utf8");
const js = readFileSync("web/settings.js", "utf8");

test("settings.html loads settings.js, which imports auth.js", () => {
    assert.ok(html.includes('src="./settings.js"'), "settings.html must load settings.js");
    assert.ok(js.includes("./auth.js"), "settings.js must import auth.js");
});

test("settings.js does NOT wait on DOMContentLoaded (modules are deferred)", () => {
    // ES modules execute after DOM ready, so registering a
    // DOMContentLoaded listener at module top level attaches AFTER the
    // event already fired and never runs. showTab must be invoked
    // directly. See v0.1.78 fix.
    assert.doesNotMatch(js, /addEventListener\(\s*["']DOMContentLoaded["']/,
        "settings.js must NOT register a DOMContentLoaded listener — modules are deferred");
});

test("settings.html has create-token-form", () => {
    assert.ok(html.includes('id="create-token-form"'), "must have create-token-form");
});

test("settings.html has change-password-form", () => {
    assert.ok(html.includes('id="change-password-form"'), "must have change-password-form");
});


test("settings.html has token list container", () => {
    assert.ok(html.includes('id="token-list"'), "must have token-list element");
});

test("settings.js POSTs to /api/me/tokens", () => {
    assert.ok(js.includes("/api/me/tokens"), "must reference /api/me/tokens");
    assert.ok(
        js.includes('"POST"') || js.includes("'POST'") || js.includes("method: \"POST\"") || js.includes("method: 'POST'"),
        "must use POST method for token creation"
    );
});

test("settings.js references POST /api/me/password", () => {
    assert.ok(js.includes("/api/me/password"), "must reference /api/me/password");
});

test("settings.html has all 4 sub-tab anchors with data-tab", () => {
    for (const tab of ["api-tokens", "change-password", "sessions", "danger"]) {
        const re = new RegExp(`<a [^>]*data-tab="${tab}"`);
        assert.match(html, re, `must have data-tab="${tab}"`);
    }
});

test("settings.html has all 4 panels with data-panel", () => {
    for (const panel of ["api-tokens", "change-password", "sessions", "danger"]) {
        const re = new RegExp(`<section [^>]*data-panel="${panel}"`);
        assert.match(html, re, `must have section with data-panel="${panel}"`);
    }
});

test("settings.html includes settings-sessions.js + settings-danger.js", () => {
    assert.match(html, /src="\.\/?settings-sessions\.js"/, "must include settings-sessions.js");
    assert.match(html, /src="\.\/?settings-danger\.js"/, "must include settings-danger.js");
});

test("settings-sessions.js calls /api/me/sessions", () => {
    const sessionsJs = readFileSync("web/settings-sessions.js", "utf8");
    assert.match(sessionsJs, /\/api\/me\/sessions/, "must reference /api/me/sessions");
    assert.match(sessionsJs, /sign-out-others/, "must reference sign-out-others");
});

test("settings-danger.js sends DELETE /api/me with email + password", () => {
    const dangerJs = readFileSync("web/settings-danger.js", "utf8");
    assert.match(dangerJs, /authFetch\("\/api\/me"/, "must authFetch /api/me");
    assert.match(dangerJs, /method:\s*"DELETE"/, "must use DELETE method");
    assert.match(dangerJs, /\bemail\b/, "must reference email");
    assert.match(dangerJs, /\bpassword\b/, "must reference password");
});

