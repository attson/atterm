import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const html = readFileSync("web/settings.html", "utf8");
const js = readFileSync("web/settings.js", "utf8");

test("settings.html loads settings.js, which imports auth.js", () => {
    assert.ok(html.includes('src="./settings.js"'), "settings.html must load settings.js");
    assert.ok(js.includes("./auth.js"), "settings.js must import auth.js");
});

test("settings.html has create-token-form", () => {
    assert.ok(html.includes('id="create-token-form"'), "must have create-token-form");
});

test("settings.html has change-password-form", () => {
    assert.ok(html.includes('id="change-password-form"'), "must have change-password-form");
});

test("settings.html has logout button", () => {
    assert.ok(html.includes('id="logout"'), "must have logout button");
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
