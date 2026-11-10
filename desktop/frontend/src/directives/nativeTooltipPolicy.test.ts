import { describe, expect, test } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { dirname, extname, join } from "node:path";
import { fileURLToPath } from "node:url";

function vueFiles(root: string): string[] {
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    const path = join(root, entry.name);
    return entry.isDirectory() ? vueFiles(path) : extname(path) === ".vue" ? [path] : [];
  });
}

describe("native tooltip policy", () => {
  test("native elements use v-tooltip instead of the HTML title attribute", () => {
    const srcRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
    const roots = [srcRoot, join(srcRoot, "../../../web/src")];
    const violations = roots.flatMap((root) => vueFiles(root)).flatMap((path) => {
      const source = readFileSync(path, "utf8");
      return [...source.matchAll(/<([a-z][\w-]*)\b[^>]*\s(?::|v-bind:)?title\s*=/gs)]
        // iframe title is its accessible name, not a native tooltip.
        .filter((match) => match[1] !== "n-card" && match[1] !== "iframe")
        .map((match) => `${path}: <${match[1]}>`);
    });

    expect(violations).toEqual([]);
  });
});
