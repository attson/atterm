import { describe, expect, test, vi } from "vitest";
import { createDeferredInputSender } from "./deferredInputSender";

describe("deferred input sender", () => {
  test("preserves input order until a connection target is available", () => {
    const sender = createDeferredInputSender();
    const target = vi.fn();

    sender.send("a");
    sender.send("b");
    expect(target).not.toHaveBeenCalled();

    sender.setTarget(target);
    sender.send("c");

    expect(target.mock.calls.map(([value]) => value)).toEqual(["a", "b", "c"]);
  });
});
