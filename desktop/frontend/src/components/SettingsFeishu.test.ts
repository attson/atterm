import { describe, expect, test } from "vitest";
import source from "./SettingsFeishu.vue?raw";

describe("SettingsFeishu", () => {
  test("imports the AI-only notifications bindings from the api layer", () => {
    expect(source).toContain("getAINotificationsOnly");
    expect(source).toContain("setAINotificationsOnly");
  });

  test("tracks AI-only state with a ref defaulting to true and reads it on mount", () => {
    expect(source).toContain("aiOnlyNotifications");
    expect(source).toMatch(/aiOnlyNotifications\s*=\s*ref\(true\)/);
    expect(source).toContain("getAINotificationsOnly()");
  });

  test("renders the AI-only checkbox toggle wired to setAINotificationsOnly", () => {
    expect(source).toContain("settings.feishu.aiOnlyNotifications");
    expect(source).toContain('type="checkbox"');
    expect(source).toContain(":checked=\"aiOnlyNotifications\"");
    expect(source).toContain("onToggleAIOnly");
  });

  test("renders and persists all automatic card notification switches", () => {
    expect(source).toContain("getFeishuCardNotificationSettings");
    expect(source).toContain("setFeishuCardNotificationEnabled");
    for (const cardType of [
      "command_success",
      "command_failure",
      "command_sealed",
      "waiting_input",
      "ask_question",
    ]) {
      expect(source).toContain(cardType);
    }
    expect(source).toContain('data-test="feishu-card-notifications"');
  });

  test("imports the remote terminal bindings from the api layer", () => {
    expect(source).toContain("getFeishuRemoteTerminalSettings");
    expect(source).toContain("setFeishuRemoteTerminalSettings");
  });

  test("tracks remote terminal state with refs and reads them on mount", () => {
    expect(source).toContain("remoteTerminalEnabled");
    expect(source).toMatch(/remoteTerminalEnabled\s*=\s*ref\(false\)/);
    expect(source).toContain("sessionAutoAttach");
    expect(source).toMatch(/sessionAutoAttach\s*=\s*ref\('ai'\)/);
    expect(source).toContain("getFeishuRemoteTerminalSettings()");
  });

  test("renders the remote terminal checkbox toggle and autoAttach select", () => {
    expect(source).toContain("settings.feishu.remoteTerminal.enable");
    expect(source).toContain(":checked=\"remoteTerminalEnabled\"");
    expect(source).toContain("onRemoteTerminalToggleChange");
    expect(source).toContain("settings.feishu.remoteTerminal.autoAttach.ai");
    expect(source).toContain("settings.feishu.remoteTerminal.autoAttach.all");
    expect(source).toContain("settings.feishu.remoteTerminal.autoAttach.none");
    expect(source).toContain("onAutoAttachChange");
  });

  test("renders Feishu mode dropdown with all three options", () => {
    expect(source).toContain("settings.feishu.mode.options.auto");
    expect(source).toContain("settings.feishu.mode.options.local");
    expect(source).toContain("settings.feishu.mode.options.relay");
  });

  test("binds Feishu mode dropdown to setFeishuModePref via a change handler", () => {
    expect(source).toContain("onFeishuModeChange");
    expect(source).toContain("setFeishuModePref");
  });

  test("renders effective-mode line and a fallback warning", () => {
    expect(source).toContain("settings.feishu.mode.effective.label");
    expect(source).toContain("settings.feishu.mode.effective.fallbackWarn");
  });

  test("renders and polls the physical Feishu connection state", () => {
    expect(source).toContain('data-test="feishu-connection-status"');
    expect(source).toContain("status.value.connection_state");
    expect(source).toContain("status.connection_error");
    expect(source).toContain("startStatusPolling");
    expect(source).toContain("STATUS_POLL_INTERVAL_MS");
  });
});
