import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsTerminalAppearance from "./SettingsTerminalAppearance.vue";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";
import { __setBindingsForTest } from "../lib/api";

describe("SettingsTerminalAppearance", () => {
  // Mirrors createFakePlatform() (caps.wailsBindings: true), plus the six
  // getter/setter pairs this component's onMounted load path touches. Values
  // match the Go-side defaults from Task 1 so a freshly mounted panel
  // reflects "today's behavior unchanged".
  beforeEach(() => {
    __setPlatformForTests(createFakePlatform());
    __setBindingsForTest({
      GetTerminalFontHead: vi.fn().mockResolvedValue(""),
      SetTerminalFontHead: vi.fn().mockResolvedValue(undefined),
      GetTerminalFontSize: vi.fn().mockResolvedValue(13),
      SetTerminalFontSize: vi.fn().mockResolvedValue(undefined),
      GetTerminalLineHeight: vi.fn().mockResolvedValue(1.0),
      SetTerminalLineHeight: vi.fn().mockResolvedValue(undefined),
      GetTerminalCursorStyle: vi.fn().mockResolvedValue("block"),
      SetTerminalCursorStyle: vi.fn().mockResolvedValue(undefined),
      GetTerminalCursorBlink: vi.fn().mockResolvedValue(true),
      SetTerminalCursorBlink: vi.fn().mockResolvedValue(undefined),
      GetTerminalScrollback: vi.fn().mockResolvedValue(5000),
      SetTerminalScrollback: vi.fn().mockResolvedValue(undefined),
    } as any);
  });

  afterEach(() => {
    __setBindingsForTest(undefined);
    __setPlatformForTests(null);
  });

  async function mountAppearance() {
    const w = mount(SettingsTerminalAppearance);
    await flushPromises();
    return w;
  }

  it("renders all five appearance controls", async () => {
    const w = await mountAppearance();
    expect(w.find('[data-test="terminal-font"]').exists()).toBe(true);
    expect(w.find('[data-test="terminal-font-size"]').exists()).toBe(true);
    expect(w.find('[data-test="terminal-line-height"]').exists()).toBe(true);
    expect(w.find('[data-test="terminal-cursor-style"]').exists()).toBe(true);
    expect(w.find('[data-test="terminal-cursor-blink"]').exists()).toBe(true);
    expect(w.find('[data-test="terminal-scrollback"]').exists()).toBe(true);
  });

  it("shows a per-pane memory estimate that tracks the scrollback value", async () => {
    const w = await mountAppearance();
    const input = w.find('[data-test="terminal-scrollback"]');
    await input.setValue("20000");
    // 20000 lines * 2.75 KB ≈ 55 MB
    expect(w.find('[data-test="terminal-scrollback-hint"]').text()).toContain("55");
  });

  it("emits appearance-changed when the font size changes", async () => {
    const w = await mountAppearance();
    await w.find('[data-test="terminal-font-size"]').setValue("16");
    await w.find('[data-test="terminal-font-size"]').trigger("change");
    const ev = w.emitted("appearance-changed");
    expect(ev).toBeTruthy();
    expect((ev!.at(-1)![0] as any).fontSize).toBe(16);
  });
});
