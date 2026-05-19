import { setActivePinia, createPinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useTranslatePanelStore } from "./panelStore";
import { TranslateError, type TranslateProvider } from "./providers/types";

function fakeProvider(): { provider: TranslateProvider; calls: Array<{ text: string; target: string }> } {
  const calls: Array<{ text: string; target: string }> = [];
  return {
    calls,
    provider: {
      translate: vi.fn(async (text, target) => {
        calls.push({ text, target });
        return { translated: `[${target}] ${text}`, detectedSrcLang: "en" };
      }),
    },
  };
}

describe("translatePanelStore", () => {
  beforeEach(() => setActivePinia(createPinia()));
  afterEach(() => vi.restoreAllMocks());

  it("openWithSource sets state and dispatches translate", async () => {
    const { provider, calls } = fakeProvider();
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("hello");
    expect(store.visible).toBe(true);
    expect(store.source).toBe("hello");
    expect(store.targetLang).toBe("zh-CN");
    expect(store.loading).toBe(false);
    expect(store.result).toEqual({ translated: "[zh-CN] hello", detectedSrcLang: "en" });
    expect(store.history.length).toBe(1);
    expect(calls).toEqual([{ text: "hello", target: "zh-CN" }]);
  });

  it("CJK source auto-targets 'en'", async () => {
    const { provider } = fakeProvider();
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("你好");
    expect(store.targetLang).toBe("en");
  });

  it("history caps at 5, newest first", async () => {
    const { provider } = fakeProvider();
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    for (let i = 0; i < 7; i++) await store.openWithSource(`text-${i}`);
    expect(store.history.length).toBe(5);
    expect(store.history[0].source).toBe("text-6");
    expect(store.history[4].source).toBe("text-2");
  });

  it("error sets state.error, does not push history", async () => {
    const provider: TranslateProvider = {
      translate: async () => { throw new TranslateError("auth", "bad key"); },
    };
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("hello");
    expect(store.error?.code).toBe("auth");
    expect(store.history.length).toBe(0);
    expect(store.result).toBeNull();
  });

  it("changeTarget re-translates with new lang", async () => {
    const { provider, calls } = fakeProvider();
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("hello");
    await store.changeTarget("ja");
    expect(store.targetLang).toBe("ja");
    expect(store.result?.translated).toBe("[ja] hello");
    expect(calls).toEqual([
      { text: "hello", target: "zh-CN" },
      { text: "hello", target: "ja" },
    ]);
  });

  it("second openWithSource aborts in-flight translation", async () => {
    let abortedSignal: AbortSignal | null = null;
    const provider: TranslateProvider = {
      translate: (text, target, opts) => new Promise((resolve, reject) => {
        opts.signal.addEventListener("abort", () => {
          abortedSignal = opts.signal;
          reject(new TranslateError("aborted", "user cancelled"));
        });
        setTimeout(() => resolve({ translated: `[${target}] ${text}`, detectedSrcLang: "en" }), 10_000);
      }),
    };
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    const first = store.openWithSource("first");
    await Promise.resolve();  // let the inner promise hook up
    await store.openWithSource("second");  // triggers abort on first
    await expect(first).resolves.toBeUndefined();
    expect(abortedSignal?.aborted).toBe(true);
    expect(store.source).toBe("second");
  });

  it("close hides panel but retains state", async () => {
    const { provider } = fakeProvider();
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("hello");
    store.close();
    expect(store.visible).toBe(false);
    expect(store.source).toBe("hello");
    expect(store.result?.translated).toBe("[zh-CN] hello");
  });
});
