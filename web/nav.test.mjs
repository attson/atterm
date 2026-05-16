// index.html previously had no entry into /settings.html, so users
// couldn't discover the API tokens UI without typing the URL. Each
// authenticated page now ships the same Home / Settings nav, with
// active state on the current page.

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const indexHtml = readFileSync("web/index.html", "utf8");
const settingsHtml = readFileSync("web/settings.html", "utf8");

function navBlock(html) {
    const m = html.match(/<nav class="topnav"[\s\S]*?<\/nav>/);
    assert.ok(m, "page must have a <nav class=\"topnav\"> block");
    return m[0];
}

test("index.html has a topnav linking to / and /settings.html", () => {
    const nav = navBlock(indexHtml);
    assert.match(nav, /href="\/"/);
    assert.match(nav, /href="\/settings\.html"/);
});

test("index.html marks Home as the active nav item", () => {
    const nav = navBlock(indexHtml);
    assert.match(nav, /<a href="\/" class="active"[^>]*>Home<\/a>/);
    // Settings link must not also be active.
    assert.doesNotMatch(nav, /href="\/settings\.html"[^>]*class="active"/);
});

test("settings.html has a topnav linking to / and /settings.html", () => {
    const nav = navBlock(settingsHtml);
    assert.match(nav, /href="\/"/);
    assert.match(nav, /href="\/settings\.html"/);
});

test("settings.html marks Settings as the active nav item", () => {
    const nav = navBlock(settingsHtml);
    assert.match(nav, /<a href="\/settings\.html" class="active"[^>]*>Settings<\/a>/);
    assert.doesNotMatch(nav, /href="\/"[^>]*class="active"/);
});
