import { describe, expect, it, vi } from "vitest";
import { useTerminalLinkProvider } from "./useTerminalLinkProvider";

function makeFakeTerm(lineText: string) {
  let provider: {
    provideLinks: (y: number, cb: (links: unknown[] | undefined) => void) => void;
  } | null = null;
  const dispose = vi.fn();
  return {
    term: {
      registerLinkProvider(p: typeof provider) {
        provider = p;
        return { dispose };
      },
      buffer: {
        active: {
          getLine(_y: number) {
            return { translateToString: (_trim: boolean) => lineText };
          },
        },
      },
    } as unknown as import("xterm").Terminal,
    getProvider: () => provider!,
    dispose,
  };
}

describe("useTerminalLinkProvider", () => {
  it("provides one ILink per detectLinks match on the requested line", () => {
    const f = makeFakeTerm("see https://x.test now");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "/Users/me",
      openURL,
      onError: vi.fn(),
    });

    let received: any[] | undefined;
    f.getProvider().provideLinks(1, (links) => (received = links as any[]));
    expect(received).toHaveLength(1);
    expect(received![0].text).toBe("https://x.test");
    expect(received![0].range.start.y).toBe(1);
    expect(received![0].range.end.y).toBe(1);
    expect(received![0].decorations).toEqual({ underline: true, pointerCursor: true });
  });

  it("activate ignores click without modifier", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError: vi.fn(),
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(new MouseEvent("click", {}), "https://x.test");
    expect(openURL).not.toHaveBeenCalled();
  });

  it("activate with Mod opens URL", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError: vi.fn(),
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(
      new MouseEvent("click", { metaKey: true }),
      "https://x.test",
    );
    expect(openURL).toHaveBeenCalledWith("https://x.test");
  });

  it("activate for ~/ without homeDir calls onError, not openURL", async () => {
    const f = makeFakeTerm("cd ~/Projects/foo");
    const openURL = vi.fn().mockResolvedValue(undefined);
    const onError = vi.fn();
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError,
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(
      new MouseEvent("click", { metaKey: true }),
      "~/Projects/foo",
    );
    expect(openURL).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith("terminal.link.openFailedNoHome");
  });

  it("activate surfaces openURL rejection via onError", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockRejectedValue(new Error("boom"));
    const onError = vi.fn();
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError,
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(
      new MouseEvent("click", { metaKey: true }),
      "https://x.test",
    );
    expect(onError).toHaveBeenCalledWith("terminal.link.openFailed");
  });

  it("returns a disposable that forwards to xterm", () => {
    const f = makeFakeTerm("");
    const d = useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL: vi.fn(),
      onError: vi.fn(),
    });
    d.dispose();
    expect(f.dispose).toHaveBeenCalled();
  });

  it("calls back with undefined when no matches on the line", () => {
    const f = makeFakeTerm("nothing here");
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL: vi.fn(),
      onError: vi.fn(),
    });
    const cb = vi.fn();
    f.getProvider().provideLinks(1, cb);
    expect(cb).toHaveBeenCalledWith(undefined);
  });
});
