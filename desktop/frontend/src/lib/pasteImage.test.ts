import { describe, expect, test } from "vitest";
import { pasteImageBlockReason } from "./connection";

describe("paste image diagnostics", () => {
  test("explains why image paste cannot be sent", () => {
    expect(pasteImageBlockReason(undefined, 12)).toBe("websocket is not open");
    expect(pasteImageBlockReason(0, 12)).toBe("websocket is not open");
    expect(pasteImageBlockReason(1, 11 * 1024 * 1024)).toMatch(/image too large/);
    expect(pasteImageBlockReason(1, 12)).toBeNull();
  });
});
