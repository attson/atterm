import { afterEach, describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import source from "./SessionInputHistoryPanel.vue?raw";
import SessionInputHistoryPanel from "./SessionInputHistoryPanel.vue";

const submittedAt = Date.UTC(2026, 10, 10, 12, 34, 56);

function mountPanel(overrides: Partial<{
  loading: boolean;
  items: Array<{ text: string; submitted_at_unix_ms: number }>;
  truncated: boolean;
  error: "locked" | "failed" | null;
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
    document.body.innerHTML = "";
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
});
