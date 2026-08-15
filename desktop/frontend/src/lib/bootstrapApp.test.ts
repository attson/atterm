import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import source from "./bootstrapApp.ts?raw";

describe("bootstrapApp logging", () => {
  // Renderer logs are batched and flushed on a timer, so the last batch — the
  // one covering a crash or a close — only survives if the teardown handlers
  // are installed. They belong first, before any step that can fail.
  it("installs the flush handlers before anything that can fail", () => {
    // Anchor inside the function body: the doc comment above it lists the same
    // steps in prose and would otherwise match first.
    const body = source.slice(source.indexOf("export async function bootstrapApp"));
    const install = body.indexOf("installLogFlushHandlers()");
    const firstAwait = body.indexOf("await initI18n");
    expect(install).toBeGreaterThan(-1);
    expect(install).toBeLessThan(firstAwait);
  });

  // Without a record on the happy path, an empty ui-* section in the log file
  // cannot be told apart from a broken bridge to the Go logger.
  it("emits a boot heartbeat so an empty log section is unambiguous", () => {
    expect(source).toMatch(/logInfo\("boot", "renderer ready"/);
  });
});
