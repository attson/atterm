import { afterEach, describe, expect, test, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import source from "./SessionInputHistoryPanel.vue?raw";
import SessionInputHistoryPanel from "./SessionInputHistoryPanel.vue";
import type { SessionHistoryImagePreview } from "../composables/useTerminalImagePreview";
import type { ClipboardWriter } from "../lib/terminalCopy";

const submittedAt = Date.UTC(2026, 10, 10, 12, 34, 56);

function mountPanel(overrides: Partial<{
  loading: boolean;
  items: Array<{ text: string; submitted_at_unix_ms: number }>;
  truncated: boolean;
  error: "locked" | "failed" | null;
  clipboard: ClipboardWriter;
  resolveImage: (text: string, submittedAtUnixMS: number, occurrence: number) => SessionHistoryImagePreview | null;
}> = {}) {
  return mount(SessionInputHistoryPanel, {
    attachTo: document.body,
    props: {
      loading: false,
      items: [],
      truncated: false,
      error: null,
      ...overrides,
    },
  });
}

describe("SessionInputHistoryPanel", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    document.body.innerHTML = "";
  });

  test("copies only the selected record's original multiline text through the native clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const text = "  inspect this\n\nkeep spacing  ";
    const wrapper = mountPanel({
      items: [
        { text, submitted_at_unix_ms: submittedAt },
        { text: "another record", submitted_at_unix_ms: submittedAt - 60_000 },
      ],
      clipboard: { writeText },
    });

    const records = wrapper.findAll("[data-test='session-input-history-item']");
    await records[1].get("[data-test='session-input-history-copy']").trigger("click");
    await flushPromises();

    expect(writeText).toHaveBeenCalledTimes(1);
    expect(writeText).toHaveBeenCalledWith(text);
    expect(records[1].get('[role="status"]').text()).toBe("Copied");
    expect(records[0].find('[role="status"]').exists()).toBe(false);
    expect(wrapper.emitted("close")).toBeUndefined();
  });

  test("copies through the browser clipboard when no native writer is supplied", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const wrapper = mountPanel({ items: [{ text: "browser input", submitted_at_unix_ms: submittedAt }] });

    await wrapper.get("[data-test='session-input-history-copy']").trigger("click");
    await flushPromises();

    expect(writeText).toHaveBeenCalledTimes(1);
    expect(writeText).toHaveBeenCalledWith("browser input");
    expect(wrapper.get('[role="status"]').text()).toBe("Copied");
  });

  test("shows copy failure and lets the user retry", async () => {
    const writeText = vi.fn().mockRejectedValueOnce(new Error("denied")).mockResolvedValue(undefined);
    const wrapper = mountPanel({
      items: [{ text: "retry this input", submitted_at_unix_ms: submittedAt }],
      clipboard: { writeText },
    });
    const button = wrapper.get("[data-test='session-input-history-copy']");

    await button.trigger("click");
    await flushPromises();
    expect(wrapper.get('[role="status"]').text()).toBe("copy failed");
    await button.trigger("click");
    await flushPromises();
    expect(wrapper.get('[role="status"]').text()).toBe("Copied");
    expect(writeText).toHaveBeenCalledTimes(2);
  });

  test("copies cached images and text together and offers explicit text-only copy if images are gone", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const writeHTML = vi.fn().mockResolvedValue(undefined);
    const text = "look [Image #1] here";
    const wrapper = mountPanel({
      items: [{ text, submitted_at_unix_ms: submittedAt }],
      clipboard: { writeText, writeHTML },
      resolveImage: () => ({ file: new Blob(["png"], { type: "image/png" }), name: "cached.png" }),
    });
    await wrapper.get("[data-test='session-input-history-copy']").trigger("click");
    await vi.waitFor(() => expect(writeHTML).toHaveBeenCalledWith(text, expect.stringContaining('data:image/png;base64,cG5n')));
    await flushPromises();
    expect(wrapper.get('[role="status"]').text()).toBe("Copied");

    await wrapper.setProps({ resolveImage: () => null });
    await wrapper.get("[data-test='session-input-history-copy']").trigger("click");
    await flushPromises();
    expect(wrapper.get('[role="status"]').text()).toBe("Original images are no longer cached.");
    expect(writeText).not.toHaveBeenCalled();
    await wrapper.get("[data-test='session-input-history-copy-text']").trigger("click");
    await flushPromises();
    expect(writeText).toHaveBeenCalledWith(text);
    expect(wrapper.get('[role="status"]').text()).toBe("Copied");
  });

  test("shows chronological multiline records with relative and exact local time", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(submittedAt + 5 * 60_000);
    const items = [
      { text: "newest line one\nnewest line two", submitted_at_unix_ms: submittedAt },
      { text: "older", submitted_at_unix_ms: submittedAt - 60_000 },
    ];

    const wrapper = mountPanel({ items });
    const records = wrapper.findAll("[data-test='session-input-history-item']");

    expect(records).toHaveLength(2);
    expect(records[0].text()).toContain("older");
    expect(records[1].find(".session-input-history-text").text()).toBe("newest line one\nnewest line two");
    expect(records[1].find("time").text()).toMatch(/5 minutes ago/i);
    await records[1].find("time").trigger("focus");
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe(new Date(submittedAt).toLocaleString());
  });

  test("opens at the newest records at the bottom", async () => {
    const scrollHeight = vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(640);
    const wrapper = mountPanel({
      items: [
        { text: "newest", submitted_at_unix_ms: submittedAt },
        { text: "older", submitted_at_unix_ms: submittedAt - 60_000 },
      ],
    });

    await vi.waitFor(() => {
      expect((wrapper.find(".session-input-history-body").element as HTMLElement).scrollTop).toBe(640);
    });
    scrollHeight.mockRestore();
  });

  test("keeps the header fixed while the records body scrolls and text preserves whitespace", () => {
    const wrapper = mountPanel({
      items: [{ text: "one\ntwo", submitted_at_unix_ms: submittedAt }],
    });

    expect(wrapper.find(".session-input-history-header").exists()).toBe(true);
    expect(wrapper.find(".session-input-history-body").exists()).toBe(true);
    expect(source).toMatch(/\.session-input-history-body\s*\{[\s\S]*?overflow-y:\s*auto/);
    expect(source).toMatch(/\.session-input-history-text\s*\{[\s\S]*?white-space:\s*pre-wrap/);
  });

  test("renders loading, empty, locked, failure, and truncation states", async () => {
    const wrapper = mountPanel({ loading: true });
    expect(wrapper.find("[data-test='session-input-history-loading']").exists()).toBe(true);

    await wrapper.setProps({ loading: false });
    expect(wrapper.find("[data-test='session-input-history-empty']").exists()).toBe(true);

    await wrapper.setProps({ error: "locked" });
    expect(wrapper.find("[data-test='session-input-history-locked']").exists()).toBe(true);
    expect(wrapper.find("[data-test='session-input-history-retry']").exists()).toBe(false);

    await wrapper.setProps({ error: "failed" });
    expect(wrapper.find("[data-test='session-input-history-error']").exists()).toBe(true);
    expect(wrapper.find("[data-test='session-input-history-retry']").exists()).toBe(true);

    await wrapper.setProps({
      error: null,
      truncated: true,
      items: [{ text: "kept", submitted_at_unix_ms: submittedAt }],
    });
    expect(wrapper.find("[data-test='session-input-history-truncated']").exists()).toBe(true);
  });

  test("emits close and retry commands", async () => {
    const wrapper = mountPanel({ error: "failed" });

    await wrapper.find("[data-test='session-input-history-close']").trigger("click");
    await wrapper.find("[data-test='session-input-history-retry']").trigger("click");

    expect(wrapper.emitted("close")).toHaveLength(1);
    expect(wrapper.emitted("retry")).toHaveLength(1);
  });

  test("previews locally cached history images on hover and keyboard focus", async () => {
    const image = new Blob(["image"], { type: "image/png" });
    const resolveImage = vi.fn((text: string, _submittedAtUnixMS: number, occurrence: number) => (
      text.includes("[Image #1]") && occurrence === 0
        ? { file: image, name: "history.png" }
        : null
    ));
    vi.stubGlobal("URL", {
      createObjectURL: vi.fn(() => "blob:history-preview"),
      revokeObjectURL: vi.fn(),
    });
    const wrapper = mountPanel({
      items: [{ text: "[Image #1] inspect this", submitted_at_unix_ms: submittedAt }],
      resolveImage,
    });

    const trigger = wrapper.find("[data-test='session-input-history-image']");
    expect(trigger.exists()).toBe(true);
    await trigger.trigger("mouseenter");
    expect(URL.createObjectURL).toHaveBeenCalledWith(image);
    expect(document.body.querySelector("[data-test='session-input-history-image-preview'] img")?.getAttribute("alt"))
      .toBe("history.png");

    await trigger.trigger("mouseleave");
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:history-preview");
    expect(document.body.querySelector("[data-test='session-input-history-image-preview']")).toBeNull();

    await trigger.trigger("focus");
    expect(document.body.querySelector("[data-test='session-input-history-image-preview']")).not.toBeNull();
    await wrapper.unmount();
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(2);
  });

  test("keeps unmatched image placeholders as plain history text", () => {
    const resolveImage = vi.fn(() => null);
    const wrapper = mountPanel({
      items: [{ text: "before [Image #1] after", submitted_at_unix_ms: submittedAt }],
      resolveImage,
    });

    expect(wrapper.find("[data-test='session-input-history-image']").exists()).toBe(false);
    expect(wrapper.find(".session-input-history-text").text()).toBe("before [Image #1] after");
    expect(resolveImage).toHaveBeenCalledWith("before [Image #1] after", submittedAt, 0);
  });

  test("rechecks the bounded cache before creating a history preview URL", async () => {
    const image = new Blob(["image"], { type: "image/png" });
    let available = true;
    const resolveImage = vi.fn(() => available ? { file: image, name: "history.png" } : null);
    vi.stubGlobal("URL", {
      createObjectURL: vi.fn(() => "blob:history-preview"),
      revokeObjectURL: vi.fn(),
    });
    const wrapper = mountPanel({
      items: [{ text: "[Image #1] inspect this", submitted_at_unix_ms: submittedAt }],
      resolveImage,
    });

    expect(wrapper.find("[data-test='session-input-history-image']").exists()).toBe(true);
    available = false;
    await wrapper.find("[data-test='session-input-history-image']").trigger("mouseenter");

    expect(URL.createObjectURL).not.toHaveBeenCalled();
    expect(document.body.querySelector("[data-test='session-input-history-image-preview']")).toBeNull();
  });
});
