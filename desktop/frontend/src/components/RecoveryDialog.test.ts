import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import { initI18n, resetI18nForTest } from "../i18n";
import RecoveryDialog from "./RecoveryDialog.vue";
import source from "./RecoveryDialog.vue?raw";
import type { RecoverySnapshot } from "../lib/api";

beforeEach(async () => {
  // Use English so text assertions are stable.
  await initI18n({
    loadPreference: async () => "en",
    getLanguages: () => ["en"],
    listenLanguageChange: () => () => undefined,
  });
});
afterEach(() => resetI18nForTest());

function makeSnap(opts: Partial<RecoverySnapshot> = {}): RecoverySnapshot {
  return {
    version: 1,
    host_id: "h",
    clean_shutdown: false,
    saved_at_unix: Math.floor(Date.now() / 1000) - 300,
    tabs: [
      {
        id: "t1",
        layout: "single",
        active_pane_idx: 0,
        col_ratio: 0.5,
        row_ratio: 0.5,
        panes: [
          {
            slot: 0,
            shell: "/bin/zsh",
            last_cwd: "/Users/x/code/foo",
            session_type: "shell",
            title: "foo",
          },
        ],
      },
      {
        id: "t2",
        layout: "vertical",
        active_pane_idx: 0,
        col_ratio: 0.5,
        row_ratio: 0.5,
        panes: [
          {
            slot: 0,
            shell: "/bin/zsh",
            last_cwd: "/Users/x/code/bar",
            session_type: "ai",
            title: "Refactor",
            ai: { kind: "claude", session_id: "abc-123", captured_at_unix: 1750000000 },
          },
          {
            slot: 1,
            shell: "/bin/zsh",
            last_cwd: "/Users/x/code/bar",
            session_type: "shell",
            title: "bar",
          },
        ],
      },
    ],
    ...opts,
  };
}

describe("RecoveryDialog (source)", () => {
  test("declares snapshot + open props", () => {
    expect(source).toContain("snapshot: RecoverySnapshot");
    expect(source).toContain("open: boolean");
  });
  test("emits restore + discard", () => {
    expect(source).toMatch(/"restore"/);
    expect(source).toMatch(/"discard"/);
  });
  test("references core i18n keys", () => {
    expect(source).toContain("recovery.dialog.title");
    expect(source).toContain("recovery.dialog.btnRestoreAll");
    expect(source).toContain("recovery.dialog.btnRestoreSelected");
    expect(source).toContain("recovery.dialog.btnDiscard");
  });
  test("renders pane badge selectors for all states", () => {
    expect(source).toContain("recovery.dialog.badgeResumable");
    expect(source).toContain("recovery.dialog.badgeFresh");
    expect(source).toContain("recovery.dialog.badgeShell");
    expect(source).toContain("recovery.dialog.badgeUnclassified");
  });
  test("uses data-testid hooks for buttons", () => {
    expect(source).toContain('data-testid="btn-restore"');
    expect(source).toContain('data-testid="btn-discard"');
  });
});

describe("RecoveryDialog (mount)", () => {
  test("emits restore with full picks when all checkboxes are on", async () => {
    const w = mount(RecoveryDialog, { props: { snapshot: makeSnap(), open: true } });
    await w.find('[data-testid="btn-restore"]').trigger("click");
    const ev = w.emitted("restore");
    expect(ev?.[0]?.[0]).toHaveLength(2);
  });

  test("emits discard from secondary button", async () => {
    const w = mount(RecoveryDialog, { props: { snapshot: makeSnap(), open: true } });
    await w.find('[data-testid="btn-discard"]').trigger("click");
    expect(w.emitted("discard")?.length).toBe(1);
  });

  test("unchecking a tab reduces emit count", async () => {
    const w = mount(RecoveryDialog, { props: { snapshot: makeSnap(), open: true } });
    const checkboxes = w.findAll('input[type=checkbox]');
    await checkboxes[0].setValue(false);
    await w.find('[data-testid="btn-restore"]').trigger("click");
    const ev = w.emitted("restore");
    expect(ev?.[0]?.[0]).toHaveLength(1);
  });

  test("disables restore button when no tabs selected", async () => {
    const w = mount(RecoveryDialog, { props: { snapshot: makeSnap(), open: true } });
    const checkboxes = w.findAll('input[type=checkbox]');
    for (const cb of checkboxes) await cb.setValue(false);
    const btn = w.find('[data-testid="btn-restore"]');
    expect(btn.attributes("disabled")).toBeDefined();
  });

  test("hidden when open=false", () => {
    const w = mount(RecoveryDialog, { props: { snapshot: makeSnap(), open: false } });
    expect(w.find('[data-testid="btn-restore"]').exists()).toBe(false);
  });
});
