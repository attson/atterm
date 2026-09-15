import { describe, expect, test, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { h } from "vue";

vi.mock("@shared/api/admin", () => ({
  getTrafficStats: vi.fn(),
}));

import Traffic from "./Traffic.vue";
import { getTrafficStats } from "@shared/api/admin";

const getTrafficStatsMock = getTrafficStats as unknown as ReturnType<typeof vi.fn>;

// Traffic calls useMessage() which needs an <n-message-provider> ancestor;
// wrap it the same way AdminPanel does in production.
function mountTraffic() {
  return mount({
    render: () => h(NMessageProvider, null, { default: () => h(Traffic) }),
  });
}

describe("Traffic", () => {
  beforeEach(() => {
    getTrafficStatsMock.mockReset();
  });

  test("loads group view on mount and renders rows", async () => {
    getTrafficStatsMock.mockResolvedValue({
      view: "group",
      from: "2026-09-15",
      to: "2026-09-15",
      rows: [
        { user_id: "u1", email: "a@b.c", category: "terminal", direction: 1, bytes: 2048, frames: 5 },
      ],
    });

    const w = mountTraffic();
    await flushPromises();

    expect(getTrafficStatsMock).toHaveBeenCalledWith({ view: "group" });
    // Table rendered (data present) rather than the empty state.
    expect(w.find('[data-test="traffic-empty"]').exists()).toBe(false);
    expect(w.find('[data-test="traffic-table"]').exists()).toBe(true);
  });

  test("shows the empty state when no rows come back", async () => {
    getTrafficStatsMock.mockResolvedValue({
      view: "group",
      from: "2026-09-15",
      to: "2026-09-15",
      rows: [],
    });

    const w = mountTraffic();
    await flushPromises();

    expect(w.find('[data-test="traffic-empty"]').exists()).toBe(true);
    expect(w.find('[data-test="traffic-table"]').exists()).toBe(false);
  });

  test("switching the view re-fetches with the new view", async () => {
    getTrafficStatsMock.mockResolvedValue({ view: "group", from: "", to: "", rows: [] });
    const w = mountTraffic();
    await flushPromises();
    getTrafficStatsMock.mockClear();

    // Click the "Detail" button (first in the group). Buttons render i18n
    // labels; find by role/text is brittle, so click the first n-button.
    const buttons = w.findAll("button");
    await buttons[0].trigger("click");
    await flushPromises();

    expect(getTrafficStatsMock).toHaveBeenCalledWith({ view: "detail" });
  });
});
