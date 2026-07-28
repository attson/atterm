import { describe, expect, test, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import AdminPanel from "./AdminPanel.vue";
import Invitations from "./admin/Invitations.vue";
import Users from "./admin/Users.vue";
import Config from "./admin/Config.vue";
import FeishuConfig from "./admin/FeishuConfig.vue";

// The 4 admin tab components make real API calls on mount (apiFetch to
// /api/admin/*). AdminPanel's own contract is tab switching / conditional
// rendering, so stub the children out to keep this test focused and quiet.
const stubs = {
  Invitations: true,
  Users: true,
  Config: true,
  FeishuConfig: true,
};

describe("AdminPanel", () => {
  test("defaults to the invitations tab", () => {
    const w = mount(AdminPanel, { global: { stubs } });
    expect(w.findComponent(Invitations).exists()).toBe(true);
    expect(w.findComponent(Users).exists()).toBe(false);
    expect(w.findComponent(Config).exists()).toBe(false);
    expect(w.findComponent(FeishuConfig).exists()).toBe(false);
    expect(w.get('[data-test="admin-tab-invitations"]').classes()).toContain("active");
  });

  test("clicking a tab button switches the active tab and its class", async () => {
    const w = mount(AdminPanel, { global: { stubs } });

    await w.get('[data-test="admin-tab-users"]').trigger("click");
    expect(w.findComponent(Users).exists()).toBe(true);
    expect(w.findComponent(Invitations).exists()).toBe(false);
    expect(w.get('[data-test="admin-tab-users"]').classes()).toContain("active");
    expect(w.get('[data-test="admin-tab-invitations"]').classes()).not.toContain("active");

    await w.get('[data-test="admin-tab-config"]').trigger("click");
    expect(w.findComponent(Config).exists()).toBe(true);
    expect(w.findComponent(Users).exists()).toBe(false);

    await w.get('[data-test="admin-tab-feishu"]').trigger("click");
    expect(w.findComponent(FeishuConfig).exists()).toBe(true);
    expect(w.findComponent(Config).exists()).toBe(false);
  });

  test("renders each tab's child only while it is active", async () => {
    const w = mount(AdminPanel, { global: { stubs } });

    for (const key of ["invitations", "users", "config", "feishu"] as const) {
      await w.get(`[data-test="admin-tab-${key}"]`).trigger("click");
      const components = [Invitations, Users, Config, FeishuConfig];
      const activeIndex = ["invitations", "users", "config", "feishu"].indexOf(key);
      components.forEach((comp, idx) => {
        expect(w.findComponent(comp).exists()).toBe(idx === activeIndex);
      });
    }
  });

  test("renders exactly 4 tab buttons with the expected data-test hooks", () => {
    const w = mount(AdminPanel, { global: { stubs } });
    expect(w.find('[data-test="admin-tab-invitations"]').exists()).toBe(true);
    expect(w.find('[data-test="admin-tab-users"]').exists()).toBe(true);
    expect(w.find('[data-test="admin-tab-config"]').exists()).toBe(true);
    expect(w.find('[data-test="admin-tab-feishu"]').exists()).toBe(true);
  });

  // Regression: every admin child calls `useMessage()` from naive-ui, which
  // throws unconditionally when it can't find an <n-message-provider> above.
  // Before we wrapped AdminPanel's body in <n-config-provider> +
  // <n-message-provider>, first-clicking the TabBar admin button crashed the
  // whole panel. Stub only the network layer (apiFetch), then mount a real
  // Invitations child so the provider wiring is exercised end-to-end.
  test("mounts real Invitations child without throwing (provider wiring)", async () => {
    vi.resetModules();
    vi.doMock("@shared/api/client", async () => {
      const actual = await vi.importActual<typeof import("@shared/api/client")>(
        "@shared/api/client",
      );
      return {
        ...actual,
        apiFetch: vi.fn(async () => ({ data: [], status: 200, headers: new Headers() })),
      };
    });
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const { default: FreshAdminPanel } = await import("./AdminPanel.vue");
      const w = mount(FreshAdminPanel, {
        global: {
          // Keep the other three tabs stubbed — we only need one real child
          // to prove <n-message-provider> resolves for `useMessage()`.
          stubs: { Users: true, Config: true, FeishuConfig: true },
        },
      });
      await flushPromises();
      expect(w.find('[data-test="admin-tab-invitations"]').exists()).toBe(true);
      // Any `useMessage()` failure surfaces as an unhandled console error from
      // Vue's error handler. Assert the console stayed clean.
      expect(spy).not.toHaveBeenCalled();
    } finally {
      spy.mockRestore();
      vi.doUnmock("@shared/api/client");
      vi.resetModules();
    }
  });
});
