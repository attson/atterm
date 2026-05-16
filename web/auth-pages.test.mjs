import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

test("login.html references auth.js and has login form", () => {
    const src = readFileSync("web/login.html", "utf8");
    assert.ok(src.includes("./auth.js"), "login.html should import auth.js");
    assert.ok(src.includes('id="login-form"'), "should have login-form id");
    assert.ok(src.includes('type="email"'), "should have email input");
    assert.ok(src.includes('type="password"'), "should have password input");
});

test("signup.html references auth.js and has invite_code field", () => {
    const src = readFileSync("web/signup.html", "utf8");
    assert.ok(src.includes("./auth.js"));
    assert.ok(src.includes("invite") || src.includes("invite_code"));
    assert.ok(src.includes('id="signup-form"') || src.includes('id="login-form"'));
});
