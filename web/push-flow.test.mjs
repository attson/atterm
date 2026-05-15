import { test } from "node:test";
import assert from "node:assert/strict";
import { enablePushFlow } from "./app.js";

function makeFakes({ permission = "granted", subscribeOK = true, fetchOK = true, fetchStatus = 200, keyResponse = { key: "AAAA" } } = {}) {
  const calls = { fetch: [], subscribe: 0, requestPermission: 0 };
  const fakes = {
    notification: {
      permission,
      requestPermission: async () => {
        calls.requestPermission++;
        return permission;
      },
    },
    registration: {
      pushManager: {
        subscribe: async () => {
          calls.subscribe++;
          if (!subscribeOK) throw new Error("nope");
          return {
            endpoint: "https://push.example/abc",
            getKey: (name) => new Uint8Array([1, 2, 3, 4]),
            toJSON: () => ({ endpoint: "https://push.example/abc", keys: { p256dh: "AQID", auth: "AQID" } }),
          };
        },
      },
    },
    fetch: async (url, opts) => {
      calls.fetch.push({ url, opts });
      if (!fetchOK) throw new Error("network");
      if (url.endsWith("/api/push/key")) {
        return { ok: fetchStatus === 200, status: fetchStatus, json: async () => keyResponse };
      }
      return { ok: fetchStatus === 200, status: fetchStatus, json: async () => ({ ok: true }) };
    },
    token: "tok",
  };
  return { fakes, calls };
}

test("enablePushFlow happy path posts subscription", async () => {
  const { fakes, calls } = makeFakes();
  const result = await enablePushFlow(fakes);
  assert.equal(result.ok, true);
  assert.equal(calls.requestPermission, 1);
  assert.equal(calls.subscribe, 1);
  const subscribeCall = calls.fetch.find((c) => c.url.endsWith("/api/push/subscribe"));
  assert.ok(subscribeCall, "missing /api/push/subscribe call");
});

test("enablePushFlow denied permission returns failure without /api/push/key", async () => {
  const { fakes, calls } = makeFakes({ permission: "denied" });
  const result = await enablePushFlow(fakes);
  assert.equal(result.ok, false);
  assert.equal(result.reason, "denied");
  assert.equal(calls.fetch.length, 0);
});

test("enablePushFlow surfaces 503 server-disabled", async () => {
  const { fakes } = makeFakes({ fetchStatus: 503 });
  const result = await enablePushFlow(fakes);
  assert.equal(result.ok, false);
  assert.equal(result.reason, "disabled");
});

test("enablePushFlow handles subscribe throw with reason 'subscribe-failed'", async () => {
  const { fakes } = makeFakes({ subscribeOK: false });
  const result = await enablePushFlow(fakes);
  assert.equal(result.ok, false);
  assert.equal(result.reason, "subscribe-failed");
});
