import { mount } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TranslatePanel from "./TranslatePanel.vue";
import { useTranslatePanelStore } from "./panelStore";
import type { TranslateProvider } from "./providers/types";

const fakeProvider: TranslateProvider = {
  translate: vi.fn(async (text, target) => ({ translated: `[${target}] ${text}`, detectedSrcLang: "en" })),
};

// Stub Teleport so teleported content is rendered inline and findable by wrapper.find().
const mountOptions = {
  attachTo: document.body,
  global: { stubs: { Teleport: { template: "<div><slot /></div>" } } },
};

describe("TranslatePanel", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("renders nothing when store.visible is false", () => {
    const w = mount(TranslatePanel, mountOptions);
    expect(w.find('[data-testid="translate-panel"]').exists()).toBe(false);
    w.unmount();
  });

  it("renders source + translated when state populated", async () => {
    const store = useTranslatePanelStore();
    store.configure({ provider: fakeProvider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("hello world");
    await new Promise((r) => setTimeout(r, 0));
    const w = mount(TranslatePanel, mountOptions);
    expect(w.text()).toContain("hello world");
    expect(w.text()).toContain("[zh-CN] hello world");
    w.unmount();
  });

  it("close button hides panel", async () => {
    const store = useTranslatePanelStore();
    store.configure({ provider: fakeProvider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("x");
    const w = mount(TranslatePanel, mountOptions);
    await w.find('[data-testid="translate-close"]').trigger("click");
    expect(store.visible).toBe(false);
    w.unmount();
  });

  it("target dropdown change triggers re-translate", async () => {
    const store = useTranslatePanelStore();
    store.configure({ provider: fakeProvider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("x");
    const w = mount(TranslatePanel, mountOptions);
    await w.find('[data-testid="translate-target"]').setValue("ja");
    // Pinia/Vue tick + provider resolved
    await new Promise((r) => setTimeout(r, 0));
    expect(store.targetLang).toBe("ja");
    expect(store.result?.translated).toBe("[ja] x");
    w.unmount();
  });
});
