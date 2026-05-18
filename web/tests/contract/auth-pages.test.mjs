import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const distRoot = path.resolve(here, "..", "..", "..", "internal", "relay", "web-dist");

function readDist(rel) {
  return readFileSync(path.join(distRoot, rel), "utf8");
}

test("login.html exists and mounts a Vue app", () => {
  const html = readDist("login.html");
  assert.match(html, /<div id="app">/, "login.html must mount #app");
  assert.match(html, /\/src\/login\/main\.ts|\/assets\/login.*\.js/, "login.html must reference the login entry script");
});

test("signup.html exists and mounts a Vue app", () => {
  const html = readDist("signup.html");
  assert.match(html, /<div id="app">/, "signup.html must mount #app");
  assert.match(html, /\/src\/signup\/main\.ts|\/assets\/signup.*\.js/, "signup.html must reference the signup entry script");
});

test("login.html does not leak any token in the URL", () => {
  const html = readDist("login.html");
  assert.doesNotMatch(html, /\?token=/, "login.html must not contain ?token= (red-line 9)");
});

test("signup.html does not leak any token in the URL", () => {
  const html = readDist("signup.html");
  assert.doesNotMatch(html, /\?token=/, "signup.html must not contain ?token=");
});

test("auth HTMLs reference /api/auth endpoints only via JS bundle", () => {
  for (const name of ["login.html", "signup.html"]) {
    const html = readDist(name);
    assert.doesNotMatch(html, /<form[^>]+action=/i, `${name} must not declare a form action attribute`);
  }
});
