import { describe, expect, test } from "vitest";
import source from "./SettingsDialog.vue?raw";

function styleBlockFor(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  return match?.[1] ?? "";
}

describe("SettingsDialog shell", () => {
  test("imports the five tab subcomponents", () => {
    expect(source).toContain('import SettingsGeneral from "./SettingsGeneral.vue"');
    expect(source).toContain('import SettingsRelay from "./SettingsRelay.vue"');
    expect(source).toContain('import SettingsLogging from "./SettingsLogging.vue"');
    expect(source).toContain('import SettingsUpdates from "./SettingsUpdates.vue"');
    expect(source).toContain('import SettingsPlugins from "./SettingsPlugins.vue"');
  });

  test("renders sidebar nav with the five category labels", () => {
    expect(source).toContain('class="settings-nav"');
    expect(source).toContain(">General<");
    expect(source).toContain(">Relay<");
    expect(source).toContain(">Logging<");
    expect(source).toContain(">Updates<");
    expect(source).toContain(">Plugins<");
  });

  test("tracks the active tab and switches via sidebar clicks", () => {
    expect(source).toMatch(/activeTab\s*=\s*ref<["']general["']\s*\|\s*["']relay["']\s*\|\s*["']logging["']\s*\|\s*["']updates["']\s*\|\s*["']plugins["']/);
    expect(source).toContain('@click="switchTab(\'general\')"');
    expect(source).toContain('@click="switchTab(\'relay\')"');
    expect(source).toContain('@click="switchTab(\'logging\')"');
    expect(source).toContain('@click="switchTab(\'updates\')"');
    expect(source).toContain('@click="switchTab(\'plugins\')"');
  });

  test("renders the pinned footer only for the relay tab", () => {
    expect(source).toContain('v-if="activeTab === \'relay\'"');
    expect(source).toContain('class="settings-footer"');
    expect(source).toContain(">cancel<");
    expect(source).toContain("relayRef?.canSave");
    expect(source).toContain("relayRef?.saveLabel");
  });

  test("hosts sub-dialogs and listens for tab events", () => {
    expect(source).toContain('@open-log-viewer="openLogViewer"');
    expect(source).toContain('@request-install="onForceInstallClick"');
    expect(source).toContain("ConfirmInstallDialog");
    expect(source).toContain("LogViewerDialog");
  });

  test("confirms before discarding unsaved relay changes on tab switch", () => {
    expect(source).toContain("relayDirty");
    expect(source).toContain("pendingTab");
    expect(source).toMatch(/showDiscardConfirm\s*=/);
  });

  test("dialog uses fixed wider size and pinned column layout", () => {
    const dialogStyle = styleBlockFor(".settings-dialog");
    expect(dialogStyle).toMatch(/width\s*:\s*720px/);
    expect(dialogStyle).toMatch(/height\s*:\s*540px/);
    const navStyle = styleBlockFor(".settings-nav");
    expect(navStyle).toMatch(/width\s*:\s*160px/);
  });
});
