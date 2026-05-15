import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";

function loadSWInVM() {
  const code = readFileSync(new URL("./sw.js", import.meta.url), "utf8");
  const listeners = {};
  const ctx = {
    self: {
      addEventListener(name, fn) { listeners[name] = fn; },
      skipWaiting: () => {},
      clients: { claim: () => {} },
      registration: { showNotification: null },
    },
    caches: {
      open: async () => ({ addAll: async () => {} }),
      keys: async () => [],
      match: async () => undefined,
      delete: async () => {},
    },
    location: { origin: "https://test", pathname: "/" },
    URL,
    fetch: async () => ({}),
    Promise,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return { ctx, listeners };
}

test("push event with valid JSON calls showNotification with body/tag/data", async () => {
  const { ctx, listeners } = loadSWInVM();
  const calls = [];
  ctx.self.registration.showNotification = async (title, options) => {
    calls.push({ title, options });
  };
  const event = {
    data: { json: () => ({ title: "AT Term · atterm", body: "Command finished · exit 0 · 12s", tag: "sid-1", data: { exitCode: 0 } }) },
    waitUntil: (p) => p,
  };
  await listeners["push"](event);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].title, "AT Term · atterm");
  assert.equal(calls[0].options.body, "Command finished · exit 0 · 12s");
  assert.equal(calls[0].options.tag, "sid-1");
  assert.deepEqual(calls[0].options.data, { exitCode: 0 });
});

test("push event with non-JSON data uses fallback notification", async () => {
  const { ctx, listeners } = loadSWInVM();
  const calls = [];
  ctx.self.registration.showNotification = async (title, options) => {
    calls.push({ title, options });
  };
  const event = {
    data: { json: () => { throw new Error("not json"); } },
    waitUntil: (p) => p,
  };
  await listeners["push"](event);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].title, "AT Term");
  assert.equal(calls[0].options.body, "Command finished.");
});

test("push event with no data uses fallback notification", async () => {
  const { ctx, listeners } = loadSWInVM();
  const calls = [];
  ctx.self.registration.showNotification = async (title, options) => {
    calls.push({ title, options });
  };
  const event = { waitUntil: (p) => p };
  await listeners["push"](event);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].title, "AT Term");
});
