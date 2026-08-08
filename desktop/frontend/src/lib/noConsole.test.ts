import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

// The project deliberately runs without ESLint (see package.json), so this
// stands in for a `no-console` rule.
//
// A console.* call is invisible in a bug report: it lands in devtools nobody
// has open and never reaches desktop.log, which is the file users actually
// send. lib/log.ts routes the same call into both.

const SRC = resolve(__dirname, "..");

/** Files allowed to call console directly, with the reason. */
const ALLOWED = new Map<string, string>([
  ["lib/log.ts", "the logger itself — this is where console output comes from"],
]);

const CONSOLE_CALL = /\bconsole\.(log|warn|error|info|debug|trace)\s*\(/;

function sourceFiles(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      if (entry === "node_modules" || entry === "__tests__") continue;
      sourceFiles(full, out);
      continue;
    }
    if (!/\.(ts|vue)$/.test(entry)) continue;
    if (/\.test\.ts$/.test(entry)) continue;
    out.push(full);
  }
  return out;
}

describe("no direct console use", () => {
  it("routes every log through lib/log.ts", () => {
    const offenders: string[] = [];

    for (const file of sourceFiles(SRC)) {
      const rel = relative(SRC, file).split("\\").join("/");
      if (ALLOWED.has(rel)) continue;

      const lines = readFileSync(file, "utf8").split("\n");
      lines.forEach((line, i) => {
        if (CONSOLE_CALL.test(line)) offenders.push(`${rel}:${i + 1}: ${line.trim()}`);
      });
    }

    expect(
      offenders,
      "use logDebug/logInfo/logWarn/logError from lib/log.ts so the record " +
        "reaches desktop.log, not just devtools:\n  " + offenders.join("\n  "),
    ).toEqual([]);
  });
});
