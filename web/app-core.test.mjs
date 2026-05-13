import test from "node:test";
import assert from "node:assert/strict";

import {
  apiURL,
  buildDownloadURL,
  canRegisterServiceWorker,
  detectClientMode,
  formatReplayProgress,
  formatHost,
  isIOSWebKit,
  shouldAutoScrollToBottom,
  shouldShowInstallHint,
  tokenURLWithoutSecret,
  relayBaseURLFromLocation,
  parseSessionRoute,
  persistInsecureMode,
  normalizeRelayBaseURL,
  insecureModeFromStorage,
  shortcutInput,
  tokenFromLocation,
  versionLabel,
  webSocketAuth,
} from "./app-core.js";

test("tokenFromLocation stores fragment token and returns it", () => {
  const stored = [];
  const storage = {
    getItem: () => "old-token",
    setItem: (key, value) => stored.push([key, value]),
  };

  const token = tokenFromLocation("https://relay.example.com/#token=new-token", storage);

  assert.equal(token, "new-token");
  assert.deepEqual(stored, [["atterm-token", "new-token"]]);
});

test("tokenFromLocation ignores query token", () => {
  const storage = {
    getItem: (key) => (key === "atterm-token" ? "stored-token" : null),
    setItem: () => assert.fail("setItem should not be called"),
  };

  assert.equal(tokenFromLocation("https://relay.example.com/?token=query-token", storage), "stored-token");
});

test("tokenURLWithoutSecret removes token fragment without changing route", () => {
  assert.equal(
    tokenURLWithoutSecret("https://relay.example.com/#token=new-token"),
    "https://relay.example.com/",
  );
  assert.equal(
    tokenURLWithoutSecret("https://relay.example.com/#/s/11111111-1111-4111-8111-111111111111"),
    "https://relay.example.com/#/s/11111111-1111-4111-8111-111111111111",
  );
});

test("tokenFromLocation falls back to stored token", () => {
  const storage = {
    getItem: (key) => (key === "atterm-token" ? "stored-token" : null),
    setItem: () => assert.fail("setItem should not be called"),
  };

  assert.equal(tokenFromLocation("https://relay.example.com/", storage), "stored-token");
});

test("relayBaseURLFromLocation stores fragment relay and returns normalized URL", () => {
  const stored = [];
  const storage = {
    getItem: () => "https://old.example.com",
    setItem: (key, value) => stored.push([key, value]),
  };

  const relay = relayBaseURLFromLocation("capacitor://localhost/#relay=https%3A%2F%2Frelay.example.com%2F", storage);

  assert.equal(relay, "https://relay.example.com");
  assert.deepEqual(stored, [["atterm-relay-url", "https://relay.example.com"]]);
});

test("relayBaseURLFromLocation allows public http only when insecure mode is enabled", () => {
  const storage = {
    getItem: () => null,
    setItem: () => {},
  };

  assert.throws(
    () => relayBaseURLFromLocation("capacitor://localhost/#relay=http%3A%2F%2F121.43.40.128%3A23301", storage),
    /enable insecure mode/,
  );
  assert.equal(
    relayBaseURLFromLocation(
      "capacitor://localhost/#relay=http%3A%2F%2F121.43.40.128%3A23301",
      storage,
      { allowInsecure: true },
    ),
    "http://121.43.40.128:23301",
  );
});

test("relayBaseURLFromLocation falls back to stored relay URL", () => {
  const storage = {
    getItem: (key) => (key === "atterm-relay-url" ? "https://relay.example.com/" : null),
    setItem: () => assert.fail("setItem should not be called"),
  };

  assert.equal(relayBaseURLFromLocation("capacitor://localhost/", storage), "https://relay.example.com");
});

test("relayBaseURLFromLocation ignores invalid stored relay URL", () => {
  const storage = {
    getItem: (key) => (key === "atterm-relay-url" ? "notaurl" : null),
    setItem: () => assert.fail("setItem should not be called"),
  };

  assert.equal(relayBaseURLFromLocation("capacitor://localhost/", storage), "");
});

test("normalizeRelayBaseURL accepts http and https origins only", () => {
  assert.equal(normalizeRelayBaseURL(" https://relay.example.com/path "), "https://relay.example.com");
  assert.equal(normalizeRelayBaseURL("http://127.0.0.1:8080"), "http://127.0.0.1:8080");
  assert.throws(() => normalizeRelayBaseURL("http://121.43.40.128:23301"), /enable insecure mode/);
  assert.equal(
    normalizeRelayBaseURL("http://121.43.40.128:23301", { allowInsecure: true }),
    "http://121.43.40.128:23301",
  );
  assert.equal(normalizeRelayBaseURL(""), "");
  assert.throws(() => normalizeRelayBaseURL("ftp://relay.example.com"), /http or https/);
});

test("insecure mode persists as an explicit opt-in", () => {
  const values = new Map();
  const storage = {
    getItem: (key) => values.get(key) || null,
    setItem: (key, value) => values.set(key, value),
  };

  assert.equal(insecureModeFromStorage(storage), false);
  persistInsecureMode(storage, true);
  assert.equal(insecureModeFromStorage(storage), true);
  persistInsecureMode(storage, false);
  assert.equal(insecureModeFromStorage(storage), false);
});

test("webSocketAuth sends token with subprotocol instead of query", () => {
  assert.deepEqual(
    webSocketAuth("https:", "relay.example.com", "/client", "tok_en-123"),
    { url: "wss://relay.example.com/client", protocols: ["atterm-token.tok_en-123"] },
  );
});

test("webSocketAuth can target a configured relay base URL for bundled webviews", () => {
  assert.deepEqual(
    webSocketAuth("capacitor:", "localhost", "/client", "tok_en-123", "https://relay.example.com"),
    { url: "wss://relay.example.com/client", protocols: ["atterm-token.tok_en-123"] },
  );
  assert.deepEqual(
    webSocketAuth("capacitor:", "localhost", "/client", "", "http://127.0.0.1:8080"),
    { url: "ws://127.0.0.1:8080/client", protocols: undefined },
  );
});

test("apiURL never appends token query", () => {
  assert.equal(apiURL("/api/sessions", "dev"), "/api/sessions");
  assert.equal(apiURL("/api/sessions", ""), "/api/sessions");
});

test("apiURL can target a configured relay base URL without token query", () => {
  assert.equal(
    apiURL("/api/sessions", "secret", "https://relay.example.com"),
    "https://relay.example.com/api/sessions",
  );
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

test("formatReplayProgress formats percent and byte units", () => {
  assert.equal(
    formatReplayProgress({ phase: "chunk", bytes: 1_048_576, total_bytes: 4_194_304 }),
    "loading history 25% · 1.0 MB / 4.0 MB",
  );
  assert.equal(
    formatReplayProgress({ phase: "start", bytes: 512, total_bytes: 0 }),
    "loading history · 512 B",
  );
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
