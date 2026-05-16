import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const src = readFileSync("web/settings.html", "utf8");

test("settings.html references auth.js", () => {
    assert.ok(src.includes("./auth.js"), "settings.html must import auth.js");
});

test("settings.html has create-token-form", () => {
    assert.ok(src.includes('id="create-token-form"'), "must have create-token-form");
});

test("settings.html has change-password-form", () => {
    assert.ok(src.includes('id="change-password-form"'), "must have change-password-form");
});

test("settings.html has logout button", () => {
    assert.ok(src.includes('id="logout"'), "must have logout button");
});

test("settings.html has token list container", () => {
    assert.ok(src.includes('id="token-list"'), "must have token-list element");
});

test("settings.html references POST /api/me/tokens", () => {
    assert.ok(src.includes("/api/me/tokens"), "must reference /api/me/tokens");
    assert.ok(
        src.includes('"POST"') || src.includes("'POST'") || src.includes("method: \"POST\"") || src.includes("method: 'POST'"),
        "must use POST method for token creation"
    );
});

test("settings.html references POST /api/me/password", () => {
    assert.ok(src.includes("/api/me/password"), "must reference /api/me/password");
});
