import test from "node:test";
import assert from "node:assert/strict";

import { arrayBufferToBase64, copyTerminalSelection, isTerminalCopyShortcut } from "./app-core.js";

function key(opts) {
  return {
    key: "c",
    code: "KeyC",
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    altKey: false,
    ...opts,
  };
}

test("web copy shortcut uses Cmd+C on macOS", () => {
  assert.equal(isTerminalCopyShortcut(key({ metaKey: true }), "mac"), true);
});

test("web copy shortcut uses Ctrl+Shift+C off macOS", () => {
  assert.equal(isTerminalCopyShortcut(key({ ctrlKey: true, shiftKey: true }), "other"), true);
});

test("web copy shortcut does not steal plain Ctrl+C interrupt", () => {
  assert.equal(isTerminalCopyShortcut(key({ ctrlKey: true }), "other"), false);
  assert.equal(isTerminalCopyShortcut(key({ ctrlKey: true }), "mac"), false);
});

test("web copyTerminalSelection writes selected terminal text", async () => {
  const writes = [];
  const copied = await copyTerminalSelection(
    { getSelection: () => "history line" },
    { writeText: async (text) => writes.push(text) },
  );

  assert.equal(copied, true);
  assert.deepEqual(writes, ["history line"]);
});

test("web copyTerminalSelection ignores empty selection", async () => {
  const writes = [];
  const copied = await copyTerminalSelection(
    { getSelection: () => "" },
    { writeText: async (text) => writes.push(text) },
  );

  assert.equal(copied, false);
  assert.deepEqual(writes, []);
});

test("arrayBufferToBase64 encodes binary image payloads", () => {
  assert.equal(arrayBufferToBase64(new Uint8Array([0, 1, 2, 253, 254, 255]).buffer), "AAEC/f7/");
});
