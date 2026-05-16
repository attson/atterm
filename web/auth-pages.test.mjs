import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

test("login.html loads login.js and has login form", () => {
    const html = readFileSync("web/login.html", "utf8");
    assert.ok(html.includes('src="./login.js"'), "login.html should load login.js");
    assert.ok(html.includes('id="login-form"'), "should have login-form id");
    assert.ok(html.includes('type="email"'), "should have email input");
    assert.ok(html.includes('type="password"'), "should have password input");

    const js = readFileSync("web/login.js", "utf8");
    assert.ok(js.includes("./auth.js"), "login.js should import auth.js");
});

test("login.html links to /signup.html so invited users can find the signup page", () => {
    const src = readFileSync("web/login.html", "utf8");
    assert.ok(src.includes('href="/signup.html"'),
        "login.html should link to /signup.html for invitees");
});

test("signup.html loads signup.js and has invite_code field", () => {
    const html = readFileSync("web/signup.html", "utf8");
    assert.ok(html.includes('src="./signup.js"'), "signup.html should load signup.js");
    assert.ok(html.includes("invite") || html.includes("invite_code"));
    assert.ok(html.includes('id="signup-form"') || html.includes('id="login-form"'));

    const js = readFileSync("web/signup.js", "utf8");
    assert.ok(js.includes("./auth.js"), "signup.js should import auth.js");
});
