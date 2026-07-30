import { afterEach, describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import { initI18n, resetI18nForTest } from "../i18n";
import ConfirmCloseSessionDialog from "./ConfirmCloseSessionDialog.vue";
import source from "./ConfirmCloseSessionDialog.vue?raw";

afterEach(() => resetI18nForTest());

describe("ConfirmCloseSessionDialog", () => {
  test("accepts AI and running risk props and emits confirm/cancel", () => {
    expect(source).toContain("isAi: boolean");
    expect(source).toContain("isRunning: boolean");
    expect(source).toMatch(/\(e:\s*"confirm"\)\s*:\s*void/);
    expect(source).toMatch(/\(e:\s*"cancel"\)\s*:\s*void/);
  });

  test("renders both warning reasons for an active AI session", async () => {
    await initI18n({
      loadPreference: async () => "zh-CN",
      getLanguages: () => ["zh-CN"],
      listenLanguageChange: () => () => undefined,
    });

    const wrapper = mount(ConfirmCloseSessionDialog, {
      props: {
        title: "atterm",
        isAi: true,
        isRunning: true,
        isRemote: false,
      },
    });

    expect(wrapper.text()).toContain("关闭会话？");
    expect(wrapper.text()).toContain("AI 会话");
    expect(wrapper.text()).toContain("运行中的会话");
    expect(wrapper.text()).toContain("atterm");
  });
});
