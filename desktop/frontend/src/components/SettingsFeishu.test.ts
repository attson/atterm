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
});
