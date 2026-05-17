// PR D: admin static UI under web/admin/. Assert the structural
// contract — page metadata, panels, script imports, endpoint wiring.
//
// Runtime DOM tests would need jsdom; instead we statically grep the
// source for the endpoints each module hits. The admin API itself is
// covered by internal/relay/admin_http_test.go.

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

test("admin/index.html declares <meta name=\"page\" content=\"admin\">", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    assert.match(html, /<meta\s+name="page"\s+content="admin"/);
});

test("admin/index.html has empty #topbar placeholder for layout.js", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    assert.match(html, /<header\s+id="topbar"\s*>\s*<\/header>/);
});

test("admin/index.html imports layout.js (one dir up)", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    assert.match(html, /<script\s+type="module"\s+src="\.\.\/layout\.js"/);
});

test("admin/index.html has all 3 sub-tab anchors", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    for (const tab of ["invitations", "users", "config"]) {
        const re = new RegExp(`<a [^>]*data-tab="${tab}"`);
        assert.match(html, re);
    }
});

test("admin/index.html has all 3 panels", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    for (const panel of ["invitations", "users", "config"]) {
        const re = new RegExp(`<section [^>]*data-panel="${panel}"`);
        assert.match(html, re);
    }
});

test("admin/index.html imports admin.js + admin-invitations.js + admin-users.js", () => {
    const html = readFileSync("web/admin/index.html", "utf8");
    assert.match(html, /src="\.\/admin\.js"/);
    assert.match(html, /src="\.\/admin-invitations\.js"/);
    assert.match(html, /src="\.\/admin-users\.js"/);
});

test("admin.js calls /admin/api/config (GET + PUT)", () => {
    const js = readFileSync("web/admin/admin.js", "utf8");
    assert.match(js, /\/admin\/api\/config/);
    assert.match(js, /method:\s*"PUT"/);
});

test("admin-invitations.js calls POST /admin/api/invitations + GET list", () => {
    const js = readFileSync("web/admin/admin-invitations.js", "utf8");
    assert.match(js, /\/admin\/api\/invitations/);
    assert.match(js, /method:\s*"POST"/);
});

test("admin-users.js wires promote (POST) + demote (DELETE) admin endpoints", () => {
    const js = readFileSync("web/admin/admin-users.js", "utf8");
    assert.match(js, /\/admin\/api\/users\/.+\/admin/);
    assert.match(js, /method:\s*"POST"/);
    assert.match(js, /method:\s*"DELETE"/);
});

test("admin-users.js wires reset-password + disable endpoints", () => {
    const js = readFileSync("web/admin/admin-users.js", "utf8");
    assert.match(js, /\/admin\/api\/users\/.+\/reset-password/);
    assert.match(js, /\/admin\/api\/users\/.+\/disable/);
});
