import { describe, expect, it, vi } from "vitest";
import { createTerminalOscLinkController } from "./terminalOscLinks";

const range = {
  start: { x: 1, y: 1 },
  end: { x: 8, y: 1 },
};

describe("createTerminalOscLinkController", () => {
  it("leaves a plain OSC 8 click available for terminal selection", () => {
    const openLink = vi.fn();
    const controller = createTerminalOscLinkController({
      isMac: false,
      getDownPos: () => ({ x: 10, y: 10 }),
      resolveVisibleLink: () => null,
      openLink,
      clearSelection: vi.fn(),
      onError: vi.fn(),
    });

    controller.handler.activate(
      new MouseEvent("click", { clientX: 10, clientY: 10 }),
      "https://example.test/full",
      range,
    );

    expect(openLink).not.toHaveBeenCalled();
  });

  it("opens a valid HTTP OSC 8 target on Ctrl+click", () => {
    const openLink = vi.fn();
    const clearSelection = vi.fn();
    const controller = createTerminalOscLinkController({
      isMac: false,
      getDownPos: () => ({ x: 10, y: 10 }),
      resolveVisibleLink: () => null,
      openLink,
      clearSelection,
      onError: vi.fn(),
    });

    controller.handler.activate(
      new MouseEvent("click", { ctrlKey: true, clientX: 10, clientY: 10 }),
      "https://example.test/full",
      range,
    );

    expect(openLink).toHaveBeenCalledWith({
      start: 0,
      end: 25,
      text: "https://example.test/full",
      kind: "http",
    });
    expect(clearSelection).toHaveBeenCalledOnce();
  });

  it("strips prose accidentally included after an OSC 8 URL", () => {
    const openLink = vi.fn();
    const controller = createTerminalOscLinkController({
      isMac: false,
      getDownPos: () => ({ x: 10, y: 10 }),
      resolveVisibleLink: () => null,
      openLink,
      clearSelection: vi.fn(),
      onError: vi.fn(),
    });

    controller.handler.activate(
      new MouseEvent("click", { ctrlKey: true, clientX: 10, clientY: 10 }),
      "http://git.example.test/project/merge_requests/876), 通过验证",
      range,
    );

    expect(openLink).toHaveBeenCalledWith({
      start: 0,
      end: 50,
      text: "http://git.example.test/project/merge_requests/876",
      kind: "http",
    });
  });

  it("strips URL-encoded prose after an OSC 8 Markdown URL", () => {
    const openLink = vi.fn();
    const controller = createTerminalOscLinkController({
      isMac: false,
      getDownPos: () => ({ x: 10, y: 10 }),
      resolveVisibleLink: () => null,
      openLink,
      clearSelection: vi.fn(),
      onError: vi.fn(),
    });
    const encoded =
      "http://git.hzwxbz2.cn/back_end/advertisement/ad-ai-platform-front/" +
      "merge_requests/893),%20%E6%97%A0%E5%86%B2%E7%AA%81";

    const expected = {
      start: 0,
      end: 84,
      text: "http://git.hzwxbz2.cn/back_end/advertisement/ad-ai-platform-front/merge_requests/893",
      kind: "http",
    };
    for (const target of [encoded, encoded.replace("),", "%29,")]) {
      controller.handler.activate(
        new MouseEvent("click", { ctrlKey: true, clientX: 10, clientY: 10 }),
        target,
        range,
      );
    }
    controller.handler.hover!(
      new MouseEvent("mousemove"),
      encoded.replace("),", "%29,"),
      range,
    );

    expect(openLink).toHaveBeenCalledTimes(2);
    expect(openLink).toHaveBeenNthCalledWith(1, expected);
    expect(openLink).toHaveBeenNthCalledWith(2, expected);
    expect(controller.linkAtBufferCell(0, 3)).toEqual(expected);
  });

  it.each([
    ["https://example.test/docs/My%20File", 35],
    ["https://en.wikipedia.org/wiki/Foo_%28bar%29", 43],
  ])("preserves a valid encoded OSC 8 URL: %s", (url, end) => {
    const openLink = vi.fn();
    const controller = createTerminalOscLinkController({
      isMac: false,
      getDownPos: () => ({ x: 10, y: 10 }),
      resolveVisibleLink: () => null,
      openLink,
      clearSelection: vi.fn(),
      onError: vi.fn(),
    });

    controller.handler.activate(
      new MouseEvent("click", { ctrlKey: true, clientX: 10, clientY: 10 }),
      url,
      range,
    );

    expect(openLink).toHaveBeenCalledWith({ start: 0, end, text: url, kind: "http" });
  });

  it("prefers a complete visible link over an overlapping OSC 8 prefix", () => {
    const openLink = vi.fn();
    const complete = {
      start: 0,
      end: 39,
      text: "https://example.test/project/merge_requests/72",
      kind: "http" as const,
    };
    const controller = createTerminalOscLinkController({
      isMac: false,
      getDownPos: () => ({ x: 10, y: 10 }),
      resolveVisibleLink: () => complete,
      openLink,
      clearSelection: vi.fn(),
      onError: vi.fn(),
    });

    controller.handler.activate(
      new MouseEvent("click", { ctrlKey: true, clientX: 10, clientY: 10 }),
      "https://example.test/project/",
      range,
    );

    expect(openLink).toHaveBeenCalledWith(complete);
  });

  it("exposes a hovered OSC 8 label to right-click hit testing", () => {
    const controller = createTerminalOscLinkController({
      isMac: false,
      getDownPos: () => null,
      resolveVisibleLink: () => null,
      openLink: vi.fn(),
      clearSelection: vi.fn(),
      onError: vi.fn(),
    });

    controller.handler.hover!(
      new MouseEvent("mousemove"),
      "https://example.test/full",
      range,
    );

    expect(controller.linkAtBufferCell(0, 3)).toEqual({
      start: 0,
      end: 25,
      text: "https://example.test/full",
      kind: "http",
    });
    expect(controller.linkAtBufferCell(0, 7)).toEqual({
      start: 0,
      end: 25,
      text: "https://example.test/full",
      kind: "http",
    });
    expect(controller.linkAtBufferCell(0, 8)).toBeNull();
    expect(controller.linkAtBufferCell(1, 3)).toBeNull();

    controller.handler.leave!(
      new MouseEvent("mouseleave"),
      "https://example.test/full",
      range,
    );
    expect(controller.linkAtBufferCell(0, 3)).toBeNull();
  });

  it("rejects non-HTTP OSC 8 targets", () => {
    const openLink = vi.fn();
    const controller = createTerminalOscLinkController({
      isMac: false,
      getDownPos: () => null,
      resolveVisibleLink: () => null,
      openLink,
      clearSelection: vi.fn(),
      onError: vi.fn(),
    });

    controller.handler.activate(
      new MouseEvent("click", { ctrlKey: true }),
      "javascript:alert(1)",
      range,
    );

    expect(openLink).not.toHaveBeenCalled();
  });
});
