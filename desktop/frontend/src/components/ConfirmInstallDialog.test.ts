import { afterEach, describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import { initI18n, resetI18nForTest } from "../i18n";
import ConfirmInstallDialog from "./ConfirmInstallDialog.vue";
import source from "./ConfirmInstallDialog.vue?raw";

afterEach(() => resetI18nForTest());

describe("ConfirmInstallDialog", () => {
  test("defines version and session count props", () => {
    expect(source).toContain("version: string");
    expect(source).toContain("localCount: number");
    expect(source).toContain("remoteCount: number");
  });

  test("emits confirm and cancel", () => {
    expect(source).toMatch(/\(e:\s*"confirm"\)\s*:\s*void/);
    expect(source).toMatch(/\(e:\s*"cancel"\)\s*:\s*void/);
  });

  test("renders Chinese count messages without English plural suffixes", async () => {
    await initI18n({
      loadPreference: async () => "zh-CN",
      getLanguages: () => ["zh-CN"],
      listenLanguageChange: () => () => undefined,
    });

    const wrapper = mount(ConfirmInstallDialog, {
      props: { version: "1.2.3", localCount: 2, remoteCount: 2 },
    });

    expect(wrapper.text()).toContain("结束 2 个本地 shell 会话");
    expect(wrapper.text()).toContain("从 2 个远端会话分离");
    expect(wrapper.text()).not.toContain("会话s");
  });
});
