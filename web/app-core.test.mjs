import test from "node:test";
import assert from "node:assert/strict";

import {
  apiURL,
  buildDownloadURL,
  canRegisterServiceWorker,
  detectClientMode,
  formatReplayProgress,
  formatHost,
  groupSessionsByHost,
  isIOSWebKit,
  shouldAutoScrollToBottom,
  shouldShowInstallHint,
  tokenURLWithoutSecret,
  relayBaseURLFromLocation,
  parseSessionRoute,
  persistInsecureMode,
  normalizeRelayBaseURL,
  insecureModeFromStorage,
  keyboardInsetFromViewport,
  shortcutInput,
  shouldFocusTerminalAfterInput,
  versionLabel,
  webSocketAuth,
} from "./app-core.js";

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

test("webSocketAuth returns cookie-auth URL without subprotocol", () => {
  assert.deepEqual(
    webSocketAuth("https:", "relay.example.com", "/client"),
    { url: "wss://relay.example.com/client", protocols: undefined },
  );
  assert.deepEqual(
    webSocketAuth("http:", "localhost:8080", "/client"),
    { url: "ws://localhost:8080/client", protocols: undefined },
  );
});

test("webSocketAuth can target a configured relay base URL for bundled webviews", () => {
  assert.deepEqual(
    webSocketAuth("capacitor:", "localhost", "/client", "https://relay.example.com"),
    { url: "wss://relay.example.com/client", protocols: undefined },
  );
  assert.deepEqual(
    webSocketAuth("capacitor:", "localhost", "/client", "http://127.0.0.1:8080"),
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

test("shouldFocusTerminalAfterInput skips refocus on mobile-web to avoid soft keyboard", () => {
  assert.equal(shouldFocusTerminalAfterInput("mobile-web"), false);
  assert.equal(shouldFocusTerminalAfterInput("desktop-web"), true);
  assert.equal(shouldFocusTerminalAfterInput(undefined), true);
});

test("keyboardInsetFromViewport returns 0 when no keyboard is up", () => {
  assert.equal(
    keyboardInsetFromViewport({ innerHeight: 800, viewport: { height: 800, offsetTop: 0 } }),
    0,
  );
});

test("keyboardInsetFromViewport returns the obscured height when keyboard is open", () => {
  // 800-tall layout, 500-tall visible viewport pinned to top -> 300px keyboard.
  assert.equal(
    keyboardInsetFromViewport({ innerHeight: 800, viewport: { height: 500, offsetTop: 0 } }),
    300,
  );
});

test("keyboardInsetFromViewport accounts for viewport offsetTop (Android URL bar shifts)", () => {
  // visible region starts 40px down and is 500 tall -> 260px keyboard.
  assert.equal(
    keyboardInsetFromViewport({ innerHeight: 800, viewport: { height: 500, offsetTop: 40 } }),
    260,
  );
});

test("keyboardInsetFromViewport ignores sub-pixel rounding noise", () => {
  // height very close to innerHeight should not jitter into a 1px lift.
  assert.equal(
    keyboardInsetFromViewport({ innerHeight: 800, viewport: { height: 799.6, offsetTop: 0 } }),
    0,
  );
});

test("keyboardInsetFromViewport returns 0 without a visualViewport", () => {
  assert.equal(keyboardInsetFromViewport({ innerHeight: 800 }), 0);
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

function makeSession(overrides = {}) {
  return {
    id: "s",
    command: "bash",
    cwd: "/",
    title: "",
    cols: 80,
    rows: 24,
    started_at: 0,
    host_id: "",
    host: "",
    user: "",
    remote_permission: "",
    ...overrides,
  };
}

test("groupSessionsByHost returns [] for empty input", () => {
  assert.deepEqual(groupSessionsByHost([]), []);
});

test("groupSessionsByHost groups by host_id and sorts by hostname ascending", () => {
  const sessions = [
    makeSession({ id: "a1", host_id: "hidA", host: "mac-mini", started_at: 1 }),
    makeSession({ id: "b1", host_id: "hidB", host: "attson-air", started_at: 1 }),
    makeSession({ id: "a2", host_id: "hidA", host: "mac-mini", started_at: 2 }),
  ];
  const groups = groupSessionsByHost(sessions);
  assert.deepEqual(groups.map((g) => g.hostname), ["attson-air", "mac-mini"]);
  assert.deepEqual(groups[0].sessions.map((s) => s.id), ["b1"]);
  assert.deepEqual(groups[1].sessions.map((s) => s.id), ["a1", "a2"]);
});

test("groupSessionsByHost places empty host_id sessions into trailing __unknown__ group", () => {
  const sessions = [
    makeSession({ id: "u1", host_id: "", host: "" }),
    makeSession({ id: "z1", host_id: "hidZ", host: "zeta" }),
    makeSession({ id: "a1", host_id: "hidA", host: "alpha" }),
  ];
  const groups = groupSessionsByHost(sessions);
  assert.deepEqual(groups.map((g) => g.key), ["hidA", "hidZ", "__unknown__"]);
  assert.equal(groups[2].hostname, "unknown host");
  assert.deepEqual(groups[2].sessions.map((s) => s.id), ["u1"]);
});

test("groupSessionsByHost picks display hostname from the freshest started_at", () => {
  const sessions = [
    makeSession({ id: "old", host_id: "hidX", host: "old-name", started_at: 100 }),
    makeSession({ id: "new", host_id: "hidX", host: "new-name", started_at: 200 }),
  ];
  const groups = groupSessionsByHost(sessions);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].hostname, "new-name");
});

test("groupSessionsByHost falls back to 'unknown host' when host is empty across the bucket", () => {
  const sessions = [
    makeSession({ id: "x1", host_id: "hidX", host: "" }),
    makeSession({ id: "x2", host_id: "hidX", host: "" }),
  ];
  const groups = groupSessionsByHost(sessions);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].key, "hidX");
  assert.equal(groups[0].hostname, "unknown host");
});

test("groupSessionsByHost collapses every empty-host_id session into one __unknown__ group", () => {
  const sessions = [
    makeSession({ id: "u1", host_id: "" }),
    makeSession({ id: "u2", host_id: "" }),
  ];
  const groups = groupSessionsByHost(sessions);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].key, "__unknown__");
  assert.deepEqual(groups[0].sessions.map((s) => s.id), ["u1", "u2"]);
});

import { canEnablePush, pushSupported, base64UrlToUint8Array } from "./app-core.js";

test("pushSupported true when ServiceWorker + PushManager + Notification are present", () => {
  const nav = { serviceWorker: {}, userAgent: "Mozilla/5.0 Chrome/120" };
  const win = { PushManager: function () {}, Notification: function () {} };
  assert.equal(pushSupported(nav, win), true);
});

test("pushSupported false on iOS Safari outside PWA standalone mode", () => {
  const nav = { serviceWorker: {}, userAgent: "Mozilla/5.0 iPhone Safari/16.4", standalone: false };
  const win = { PushManager: function () {}, Notification: function () {} };
  assert.equal(pushSupported(nav, win), false);
});

test("pushSupported true on iOS PWA (standalone)", () => {
  const nav = { serviceWorker: {}, userAgent: "Mozilla/5.0 iPhone Safari/16.4", standalone: true };
  const win = { PushManager: function () {}, Notification: function () {} };
  assert.equal(pushSupported(nav, win), true);
});

test("pushSupported false when PushManager missing", () => {
  const nav = { serviceWorker: {}, userAgent: "Mozilla/5.0" };
  const win = { Notification: function () {} };
  assert.equal(pushSupported(nav, win), false);
});

test("pushSupported false when Notification missing", () => {
  const nav = { serviceWorker: {}, userAgent: "Mozilla/5.0" };
  const win = { PushManager: function () {} };
  assert.equal(pushSupported(nav, win), false);
});

test("canEnablePush rejects denied, allows default and granted", () => {
  assert.equal(canEnablePush("denied"), false);
  assert.equal(canEnablePush("default"), true);
  assert.equal(canEnablePush("granted"), true);
});

test("base64UrlToUint8Array round-trips a known value", () => {
  // base64url-encoded "Hello" => "SGVsbG8"
  const out = base64UrlToUint8Array("SGVsbG8");
  assert.equal(out.length, 5);
  assert.equal(String.fromCharCode(...out), "Hello");
});

test("base64UrlToUint8Array handles missing padding", () => {
  // base64url-encoded "Hello!" => "SGVsbG8h"
  const out = base64UrlToUint8Array("SGVsbG8h");
  assert.equal(String.fromCharCode(...out), "Hello!");
});

import { readFileSync } from "node:fs";

test("no longer reads token from localStorage", () => {
  const appJs = readFileSync(new URL("./app.js", import.meta.url), "utf8");
  const appCoreJs = readFileSync(new URL("./app-core.js", import.meta.url), "utf8");

  // Legacy token storage patterns must be absent from both files.
  assert.equal(appJs.includes('localStorage.getItem("token"'), false, 'app.js must not call localStorage.getItem("token"');
  assert.equal(appJs.includes('localStorage.setItem("token"'), false, 'app.js must not call localStorage.setItem("token"');
  assert.equal(appJs.includes('localStorage.getItem("atterm-token"'), false, 'app.js must not call localStorage.getItem("atterm-token"');
  assert.equal(appJs.includes('localStorage.setItem("atterm-token"'), false, 'app.js must not call localStorage.setItem("atterm-token"');
  assert.equal(appJs.includes("tokenFromLocation"), false, "app.js must not call tokenFromLocation");
  assert.equal(appJs.includes("persistToken"), false, "app.js must not call persistToken");
  assert.equal(appCoreJs.includes("TOKEN_KEY"), false, "app-core.js must not define TOKEN_KEY");
  assert.equal(appCoreJs.includes("export function persistToken"), false, "app-core.js must not export persistToken");
  assert.equal(appCoreJs.includes("export function tokenFromLocation"), false, "app-core.js must not export tokenFromLocation");
  assert.equal(appCoreJs.includes("export function tokenSubprotocol"), false, "app-core.js must not export tokenSubprotocol");
});
