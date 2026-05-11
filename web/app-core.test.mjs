import test from "node:test";
import assert from "node:assert/strict";

import {
  apiURL,
  buildDownloadURL,
  canRegisterServiceWorker,
  detectClientMode,
  formatHost,
  isIOSWebKit,
  shouldAutoScrollToBottom,
  shouldShowInstallHint,
  parseSessionRoute,
  shortcutInput,
  tokenFromLocation,
  versionLabel,
  wsURL,
} from "./app-core.js";

test("tokenFromLocation stores query token and returns it", () => {
  const stored = [];
  const storage = {
    getItem: () => "old-token",
    setItem: (key, value) => stored.push([key, value]),
  };

  const token = tokenFromLocation("https://relay.example.com/?token=new-token", storage);

  assert.equal(token, "new-token");
  assert.deepEqual(stored, [["atterm-token", "new-token"]]);
});

test("tokenFromLocation falls back to stored token", () => {
  const storage = {
    getItem: (key) => (key === "atterm-token" ? "stored-token" : null),
    setItem: () => assert.fail("setItem should not be called"),
  };

  assert.equal(tokenFromLocation("https://relay.example.com/", storage), "stored-token");
});

test("wsURL follows page protocol and appends token query", () => {
  assert.equal(
    wsURL("https:", "relay.example.com", "/client", "tok en"),
    "wss://relay.example.com/client?token=tok%20en",
  );
  assert.equal(
    wsURL("http:", "127.0.0.1:8080", "/client", ""),
    "ws://127.0.0.1:8080/client",
  );
});

test("apiURL appends token query", () => {
  assert.equal(apiURL("/api/sessions", "dev"), "/api/sessions?token=dev");
  assert.equal(apiURL("/api/sessions", ""), "/api/sessions");
});

test("parseSessionRoute accepts only session routes", () => {
  assert.equal(
    parseSessionRoute("#/s/11111111-1111-4111-8111-111111111111"),
    "11111111-1111-4111-8111-111111111111",
  );
  assert.equal(parseSessionRoute("#/settings"), null);
  assert.equal(parseSessionRoute("#/s/not-a-uuid"), null);
});

test("shortcutInput maps mobile terminal buttons to exact control sequences", () => {
  assert.equal(shortcutInput("esc"), "\x1b");
  assert.equal(shortcutInput("tab"), "\t");
  assert.equal(shortcutInput("ctrl-c"), "\x03");
  assert.equal(shortcutInput("ctrl-d"), "\x04");
  assert.equal(shortcutInput("left"), "\x1b[D");
  assert.equal(shortcutInput("down"), "\x1b[B");
  assert.equal(shortcutInput("up"), "\x1b[A");
  assert.equal(shortcutInput("right"), "\x1b[C");
  assert.equal(shortcutInput("unknown"), "");
});

test("formatHost prefers user@host and falls back to unknown host", () => {
  assert.equal(formatHost({ user: "alice", host: "mbp" }), "alice@mbp");
  assert.equal(formatHost({ user: "alice" }), "alice");
  assert.equal(formatHost({ host: "mbp" }), "mbp");
  assert.equal(formatHost({}), "unknown host");
});

test("buildDownloadURL creates GitHub release asset URL", () => {
  assert.equal(
    buildDownloadURL("https://github.com/attson/atterm/releases/tag/v0.1.6", "v0.1.6", "atterm.zip"),
    "https://github.com/attson/atterm/releases/download/v0.1.6/atterm.zip",
  );
});

import { sessionTitle } from "./app-core.js";

test("sessionTitle prefers command and short id", () => {
  assert.equal(
    sessionTitle({ id: "11111111-1111-4111-8111-111111111111", command: "/bin/zsh" }),
    "/bin/zsh · 11111111",
  );
  assert.equal(
    sessionTitle({ id: "22222222-2222-4222-8222-222222222222", title: "vim" }),
    "vim · 22222222",
  );
});

test("shouldAutoScrollToBottom keeps attach replay pinned to bottom", () => {
  assert.equal(shouldAutoScrollToBottom({ userScrolledUp: false, isReplay: true }), true);
  assert.equal(shouldAutoScrollToBottom({ userScrolledUp: false, isReplay: false }), true);
  assert.equal(shouldAutoScrollToBottom({ userScrolledUp: true, isReplay: false }), false);
  assert.equal(shouldAutoScrollToBottom({ userScrolledUp: true, isReplay: true }), true);
});


test("versionLabel formats current app version", () => {
  assert.equal(versionLabel("v0.1.9"), "version v0.1.9");
  assert.equal(versionLabel(""), "version dev");
  assert.equal(versionLabel(undefined), "version dev");
});

test("detectClientMode classifies touch/coarse pointer as mobile web", () => {
  assert.equal(detectClientMode({ coarsePointer: true, maxTouchPoints: 0 }), "mobile-web");
  assert.equal(detectClientMode({ coarsePointer: false, maxTouchPoints: 2, width: 390 }), "mobile-web");
});

test("detectClientMode classifies fine pointer without touch as desktop web", () => {
  assert.equal(detectClientMode({ coarsePointer: false, maxTouchPoints: 0 }), "desktop-web");
  assert.equal(detectClientMode({ coarsePointer: false, maxTouchPoints: 2, width: 1440 }), "desktop-web");
});

test("isIOSWebKit detects iPhone Safari and excludes other iOS browsers", () => {
  const safari = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1";
  const chrome = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/120.0 Mobile/15E148 Safari/604.1";
  assert.equal(isIOSWebKit(safari), true);
  assert.equal(isIOSWebKit(chrome), false);
});

test("shouldShowInstallHint only shows for non-standalone iOS Safari", () => {
  const userAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1";
  assert.equal(shouldShowInstallHint({ userAgent }), true);
  assert.equal(shouldShowInstallHint({ userAgent, standalone: true }), false);
  assert.equal(shouldShowInstallHint({ userAgent, dismissed: true }), false);
});

test("canRegisterServiceWorker requires HTTPS except loopback development", () => {
  const serviceWorker = {};
  assert.equal(canRegisterServiceWorker({ protocol: "https:", hostname: "relay.example.com", serviceWorker }), true);
  assert.equal(canRegisterServiceWorker({ protocol: "http:", hostname: "127.0.0.1", serviceWorker }), true);
  assert.equal(canRegisterServiceWorker({ protocol: "http:", hostname: "192.168.1.10", serviceWorker }), false);
});
