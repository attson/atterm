import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsConfigIO from "./SettingsConfigIO.vue";
import { resetI18nForTest } from "../i18n";
import { en } from "../i18n/messages/en";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";
import type { ImportPreview, ImportReport } from "../lib/api";

const exportConfig = vi.fn();
const previewConfigImport = vi.fn();
const applyConfigImport = vi.fn();
vi.mock("../lib/api", async () => {
  const actual = await vi.importActual<typeof import("../lib/api")>("../lib/api");
  return {
    ...actual,
    exportConfig: (...a: unknown[]) => exportConfig(...a),
    previewConfigImport: (...a: unknown[]) => previewConfigImport(...a),
    applyConfigImport: (...a: unknown[]) => applyConfigImport(...a),
  };
});

function mountPanel(wailsBindings = true) {
  const platform = { ...createFakePlatform(), caps: { ...createFakePlatform().caps, wailsBindings } };
  __setPlatformForTests(platform);
  return mount(SettingsConfigIO);
}

// jsdom's input.files is read-only; override it, then fire change (same
// trick SshHostsPanel.test.ts uses for its own file-picker input).
async function pickFile(w: ReturnType<typeof mount>, file: File) {
  const input = w.find('[data-testid="configio-import-file-input"]');
  Object.defineProperty(input.element, "files", { value: [file], configurable: true });
  await input.trigger("change");
  await flushPromises();
  await flushPromises();
}

function samplePreview(overrides: Partial<ImportPreview> = {}): ImportPreview {
  return {
    changes: [
      { key: "terminal_theme", action: "add" },
      { key: "profile:abc", action: "replace", detail: "shell" },
      { key: "default_shell", action: "unchanged" },
    ],
    skipped: ["default_shell: default shell not found: /usr/bin/fish"],
    ...overrides,
  };
}

function sampleReport(overrides: Partial<ImportReport> = {}): ImportReport {
  return {
    applied: [{ key: "terminal_theme", action: "add" }],
    skipped: [],
    ...overrides,
  };
}

describe("SettingsConfigIO", () => {
  beforeEach(() => {
    resetI18nForTest();
    exportConfig.mockReset().mockResolvedValue("/tmp/atterm-config-2026-08-21.json");
    previewConfigImport.mockReset().mockResolvedValue(samplePreview());
    applyConfigImport.mockReset().mockResolvedValue(sampleReport());
  });

  // ---- platform gate ----

  it("renders nothing under a wailsBindings=false platform shape, and never calls any config binding", async () => {
    const w = mountPanel(false);
    await flushPromises();
    expect(w.find('[data-testid="configio-panel"]').exists()).toBe(false);
    expect(w.text()).toBe("");
    expect(exportConfig).not.toHaveBeenCalled();
    expect(previewConfigImport).not.toHaveBeenCalled();
    expect(applyConfigImport).not.toHaveBeenCalled();
  });

  it("mounts and renders the panel under a wailsBindings=true platform shape", async () => {
    const w = mountPanel(true);
    await flushPromises();
    expect(w.find('[data-testid="configio-panel"]').exists()).toBe(true);
  });

  // ---- export: env checkbox default ----

  it("the include-local-env checkbox defaults to OFF (unchecked)", async () => {
    const w = mountPanel();
    await flushPromises();
    const checkbox = w.find('[data-testid="configio-env-checkbox"]').element as HTMLInputElement;
    expect(checkbox.checked).toBe(false);
  });

  it("exporting without touching the checkbox calls exportConfig(false)", async () => {
    const w = mountPanel();
    await flushPromises();
    await w.find('[data-testid="configio-export-button"]').trigger("click");
    await flushPromises();
    expect(exportConfig).toHaveBeenCalledWith(false);
  });

  it("checking the include-local-env box before exporting calls exportConfig(true)", async () => {
    const w = mountPanel();
    await flushPromises();
    await w.find('[data-testid="configio-env-checkbox"]').setValue(true);
    await w.find('[data-testid="configio-export-button"]').trigger("click");
    await flushPromises();
    expect(exportConfig).toHaveBeenCalledWith(true);
  });

  it("shows the saved path after a successful export", async () => {
    exportConfig.mockResolvedValue("/tmp/my-config.json");
    const w = mountPanel();
    await flushPromises();
    await w.find('[data-testid="configio-export-button"]').trigger("click");
    await flushPromises();
    expect(w.find('[data-testid="configio-export-status"]').text()).toContain("/tmp/my-config.json");
  });

  it("a cancelled save dialog (empty path, no error) shows no status message", async () => {
    exportConfig.mockResolvedValue("");
    const w = mountPanel();
    await flushPromises();
    await w.find('[data-testid="configio-export-button"]').trigger("click");
    await flushPromises();
    expect(w.find('[data-testid="configio-export-status"]').exists()).toBe(false);
    expect(w.find('[data-testid="configio-export-error"]').exists()).toBe(false);
  });

  it("a failed export shows the error message", async () => {
    exportConfig.mockRejectedValue(new Error("disk full"));
    const w = mountPanel();
    await flushPromises();
    await w.find('[data-testid="configio-export-button"]').trigger("click");
    await flushPromises();
    expect(w.find('[data-testid="configio-export-error"]').text()).toBe("disk full");
  });

  // ---- import: preview before apply ----

  it("picking a file previews it but does not apply it", async () => {
    const w = mountPanel();
    await flushPromises();
    await pickFile(w, new File(['{"atterm_export":1}'], "atterm-config.json"));

    expect(previewConfigImport).toHaveBeenCalledWith('{"atterm_export":1}');
    expect(applyConfigImport).not.toHaveBeenCalled();
    expect(w.find('[data-testid="configio-preview"]').exists()).toBe(true);
  });

  it("renders changes grouped by action, with add/replace details and skipped reasons shown verbatim", async () => {
    const w = mountPanel();
    await flushPromises();
    await pickFile(w, new File(["{}"], "atterm-config.json"));

    const added = w.find('[data-testid="configio-changes-add"]');
    const replaced = w.find('[data-testid="configio-changes-replace"]');
    const unchanged = w.find('[data-testid="configio-changes-unchanged"]');
    expect(added.text()).toContain("terminal_theme");
    expect(replaced.text()).toContain("profile:abc");
    expect(replaced.text()).toContain("shell");
    expect(unchanged.text()).toContain("default_shell");

    const skipped = w.find('[data-testid="configio-skipped"]');
    expect(skipped.text()).toContain("default shell not found: /usr/bin/fish");
  });

  it("apply is not called until the user clicks the explicit Apply button, and reuses the previewed text", async () => {
    const w = mountPanel();
    await flushPromises();
    const text = '{"atterm_export":1,"preferences":{}}';
    await pickFile(w, new File([text], "atterm-config.json"));
    expect(applyConfigImport).not.toHaveBeenCalled();

    await w.find('[data-testid="configio-apply-button"]').trigger("click");
    await flushPromises();

    expect(applyConfigImport).toHaveBeenCalledTimes(1);
    expect(applyConfigImport).toHaveBeenCalledWith(text);
  });

  it("after apply succeeds, the report replaces the preview (they are not shown together)", async () => {
    const w = mountPanel();
    await flushPromises();
    await pickFile(w, new File(["{}"], "atterm-config.json"));
    await w.find('[data-testid="configio-apply-button"]').trigger("click");
    await flushPromises();

    expect(w.find('[data-testid="configio-preview"]').exists()).toBe(false);
    expect(w.find('[data-testid="configio-report"]').exists()).toBe(true);
    expect(w.find('[data-testid="configio-report-applied"]').text()).toContain("terminal_theme");
  });

  it("a failed apply shows the error and keeps the preview visible for retry", async () => {
    applyConfigImport.mockRejectedValue(new Error("config store not ready"));
    const w = mountPanel();
    await flushPromises();
    await pickFile(w, new File(["{}"], "atterm-config.json"));
    await w.find('[data-testid="configio-apply-button"]').trigger("click");
    await flushPromises();

    expect(w.find('[data-testid="configio-apply-error"]').text()).toContain("config store not ready");
    expect(w.find('[data-testid="configio-preview"]').exists()).toBe(true);
    expect(w.find('[data-testid="configio-report"]').exists()).toBe(false);
  });

  it("a malformed file shows the preview error and renders no preview/apply button", async () => {
    previewConfigImport.mockRejectedValue(new Error("parse config export: unexpected EOF"));
    const w = mountPanel();
    await flushPromises();
    await pickFile(w, new File(["not json"], "bad.json"));

    expect(w.find('[data-testid="configio-preview-error"]').text()).toContain("unexpected EOF");
    expect(w.find('[data-testid="configio-preview"]').exists()).toBe(false);
  });

  it("Cancel discards the picked file and preview without applying anything", async () => {
    const w = mountPanel();
    await flushPromises();
    await pickFile(w, new File(["{}"], "atterm-config.json"));
    expect(w.find('[data-testid="configio-preview"]').exists()).toBe(true);

    await w.find('[data-testid="configio-cancel-button"]').trigger("click");
    await flushPromises();

    expect(w.find('[data-testid="configio-preview"]').exists()).toBe(false);
    expect(w.find('[data-testid="configio-import-filename"]').exists()).toBe(false);
    expect(applyConfigImport).not.toHaveBeenCalled();
  });

  it("clicking the picker button opens the hidden file input", async () => {
    const w = mountPanel();
    await flushPromises();
    const input = w.find('[data-testid="configio-import-file-input"]').element as HTMLInputElement;
    const click = vi.spyOn(input, "click");
    await w.find('[data-testid="configio-import-pick-button"]').trigger("click");
    expect(click).toHaveBeenCalled();
  });

  it("uses real i18n copy for the export checkbox label (English fixture)", async () => {
    const w = mountPanel();
    await flushPromises();
    expect(w.text()).toContain(en.settings.configio.export.includeEnvLabel);
  });
});
