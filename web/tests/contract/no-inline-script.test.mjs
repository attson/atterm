import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";

const distRoot = path.join("internal", "relay", "web-dist");

function walkHtml(dir, acc = []) {
  for (const name of readdirSync(dir)) {
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) {
      walkHtml(full, acc);
    } else if (name.endsWith(".html")) {
      acc.push(full);
    }
  }
  return acc;
}

// Matches inline <script>...</script> with a non-empty body that is not
// just whitespace. External and module scripts (<script src=...> or
// <script type="module" src=...>) are fine; only the inline form is
// banned per CSP script-src 'self' policy.
const INLINE_SCRIPT_RE = /<script\b(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/gi;

for (const file of walkHtml(distRoot)) {
  test(`${path.relative(distRoot, file)} has no inline <script> content`, () => {
    const html = readFileSync(file, "utf8");
    let match;
    while ((match = INLINE_SCRIPT_RE.exec(html)) !== null) {
      const body = match[1].trim();
      if (body.length > 0) {
        assert.fail(
          `Inline <script> found in ${file}: ` +
          `${body.slice(0, 120)}${body.length > 120 ? "…" : ""}`,
        );
      }
    }
  });
}
