import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
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
});
