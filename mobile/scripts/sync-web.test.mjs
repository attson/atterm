import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { syncWebAssets } from "./sync-web.mjs";

test("syncWebAssets copies runtime web assets and skips tests", async () => {
  const root = await mkdtemp(join(tmpdir(), "atterm-mobile-sync-"));
  const src = join(root, "web");
  const dest = join(root, "www");
  await mkdir(join(src, "vendor", "xterm"), { recursive: true });
  await writeFile(join(src, "index.html"), "<html></html>");
  await writeFile(join(src, "app.js"), "console.log('app');");
  await writeFile(join(src, "app-core.test.mjs"), "should not copy");
  await writeFile(join(src, "vendor", "xterm", "xterm.js"), "vendor asset");

  try {
    const result = await syncWebAssets(src, dest);

    assert.equal(result.copied.includes("index.html"), true);
    assert.equal(result.copied.includes("app.js"), true);
    assert.equal(result.copied.includes("vendor/xterm/xterm.js"), true);
    assert.equal(result.copied.includes("app-core.test.mjs"), false);
    assert.equal(await readFile(join(dest, "vendor", "xterm", "xterm.js"), "utf8"), "vendor asset");
    await assert.rejects(readFile(join(dest, "app-core.test.mjs"), "utf8"));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
