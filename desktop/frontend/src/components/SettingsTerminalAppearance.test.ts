import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsTerminalAppearance from "./SettingsTerminalAppearance.vue";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform, fakeEventBus } from "../platform/__tests__/_fakePlatform";
import { __setBindingsForTest } from "../lib/api";

describe("SettingsTerminalAppearance", () => {
  // Mirrors createFakePlatform() (caps.wailsBindings: true), plus the six
  // getter/setter pairs this component's onMounted load path touches. Values
  // match the Go-side defaults from Task 1 so a freshly mounted panel
  // reflects "today's behavior unchanged". Kept as a named object (not
  // re-created inline) so the prefs:changed test below can assert on the
  // same getter/setter mock instances after a reload.
  let getTerminalFontSizeMock: ReturnType<typeof vi.fn>;
  let setTerminalFontSizeMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    __setPlatformForTests(createFakePlatform());
    getTerminalFontSizeMock = vi.fn().mockResolvedValue(13);
    setTerminalFontSizeMock = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({
      GetTerminalFontHead: vi.fn().mockResolvedValue(""),
      SetTerminalFontHead: vi.fn().mockResolvedValue(undefined),
      GetTerminalFontSize: getTerminalFontSizeMock,
      SetTerminalFontSize: setTerminalFontSizeMock,
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

  // Task 4 (prefs-sync-l1 §7.2): a remote Pull re-fires "prefs:changed", the
  // same event a local Push fires after commit. This panel must re-read from
  // Go so an open panel picks up the remote value — but must not persist
  // what it just read, or the read-then-write would ping-pong the value
  // between devices forever.
  it("reloads from Go on prefs:changed without persisting", async () => {
    const events = fakeEventBus();
    __setPlatformForTests({ ...createFakePlatform(), events });
    mount(SettingsTerminalAppearance);
    await flushPromises();
    getTerminalFontSizeMock.mockClear();
    setTerminalFontSizeMock.mockClear();

    events.emit("prefs:changed", undefined);
    await flushPromises();

    expect(getTerminalFontSizeMock).toHaveBeenCalled();
    expect(setTerminalFontSizeMock).not.toHaveBeenCalled();
  });
});
