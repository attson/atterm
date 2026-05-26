import { afterEach, describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import { initI18n, resetI18nForTest } from "../i18n";
import ConfirmQuitDialog from "./ConfirmQuitDialog.vue";
import source from "./ConfirmQuitDialog.vue?raw";

afterEach(() => resetI18nForTest());

describe("ConfirmQuitDialog", () => {
  test("defines localCount and remoteCount props", () => {
    expect(source).toContain("localCount: number");
    expect(source).toContain("remoteCount: number");
  });

  test("emits confirm and cancel", () => {
    expect(source).toMatch(/\(e:\s*"confirm"\)\s*:\s*void/);
    expect(source).toMatch(/\(e:\s*"cancel"\)\s*:\s*void/);
  });

  test("renders count-driven copy and a quit button", () => {
    expect(source).toContain("sessions.endLocalSession");
    expect(source).toContain("sessions.detachRemoteSession");
    expect(source).toContain("sessions.quit");
    expect(source).toContain("common.cancel");
  });

  test("applies primary.danger styling when local count > 0", () => {
    expect(source).toMatch(/:class="\{[^}]*danger:\s*localCount\s*>\s*0/);
    expect(source).toContain('class="primary"');
  });

  test("backdrop click cancels", () => {
    expect(source).toContain('@click.self="$emit(\'cancel\')"');
  });

  test("renders Chinese count messages without English plural suffixes", async () => {
    await initI18n({
      loadPreference: async () => "zh-CN",
      getLanguages: () => ["zh-CN"],
      listenLanguageChange: () => () => undefined,
    });

    const wrapper = mount(ConfirmQuitDialog, { props: { localCount: 2, remoteCount: 2 } });

    expect(wrapper.text()).toContain("结束 2 个本地 shell 会话");
    expect(wrapper.text()).toContain("从 2 个远端会话分离");
    expect(wrapper.text()).not.toContain("会话s");
  });
});
