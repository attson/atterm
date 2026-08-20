import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SyncStatusIndicator from "./SyncStatusIndicator.vue";
import { resetI18nForTest } from "../i18n";
import { en } from "../i18n/messages/en";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform, fakeEventBus } from "../platform/__tests__/_fakePlatform";
import type { SyncStatus, PullResult } from "../lib/api";

const getSyncStatus = vi.fn();
const syncNow = vi.fn();
vi.mock("../lib/api", async () => {
  const actual = await vi.importActual<typeof import("../lib/api")>("../lib/api");
  return {
    ...actual,
    getSyncStatus: (...a: unknown[]) => getSyncStatus(...a),
    syncNow: (...a: unknown[]) => syncNow(...a),
  };
});

function status(overrides: Partial<SyncStatus> = {}): SyncStatus {
  return { state: "idle", last_synced_at: 0, pending_keys: 0, ...overrides };
}

async function mountIndicator(events = fakeEventBus(), platformOverrides: Record<string, unknown> = {}) {
  __setPlatformForTests({ ...createFakePlatform(), events, ...platformOverrides });
  const w = mount(SyncStatusIndicator);
  await flushPromises();
  return { w, events };
}

describe("SyncStatusIndicator", () => {
  beforeEach(() => {
    resetI18nForTest();
    getSyncStatus.mockReset().mockResolvedValue(status());
    syncNow.mockReset().mockResolvedValue(undefined);
  });

  it("renders each of the four states distinctly, and offline is not styled as an error", async () => {
    for (const state of ["idle", "syncing", "offline", "error"] as const) {
      getSyncStatus.mockResolvedValue(status({ state, last_error: state === "error" ? "boom" : undefined }));
      const { w } = await mountIndicator();
      const el = w.find('[data-testid="sync-state"]');
      expect(el.attributes("data-state")).toBe(state);
      expect(w.find('[data-testid="sync-indicator"]').attributes("data-state")).toBe(state);
    }
  });

  it("offline renders its own label and dot class, distinct from error's", async () => {
    getSyncStatus.mockResolvedValue(status({ state: "offline" }));
    const { w: offlineW } = await mountIndicator();
    expect(offlineW.find('[data-testid="sync-state"]').text()).toBe(en.sync.stateOffline);
    // Offline must not carry the error dot/text color class.
    expect(offlineW.find(".sync-dot").classes()).toContain("state-offline");
    expect(offlineW.find(".sync-dot").classes()).not.toContain("state-error");
    // Offline has no last_error, so no error paragraph renders at all.
    expect(offlineW.find('[data-testid="sync-error"]').exists()).toBe(false);

    getSyncStatus.mockResolvedValue(status({ state: "error", last_error: "network down" }));
    const { w: errorW } = await mountIndicator();
    expect(errorW.find('[data-testid="sync-state"]').text()).toBe(en.sync.stateError);
    expect(errorW.find(".sync-dot").classes()).toContain("state-error");
    expect(errorW.find('[data-testid="sync-error"]').text()).toBe("network down");
  });

  it('renders "never" for last_synced_at: 0, not an epoch date', async () => {
    getSyncStatus.mockResolvedValue(status({ last_synced_at: 0 }));
    const { w } = await mountIndicator();
    const text = w.find('[data-testid="sync-last-synced"]').text();
    expect(text).toBe(en.sync.lastSyncedNever);
    expect(text).not.toMatch(/1970/);
  });

  it("renders a formatted time when last_synced_at is non-zero", async () => {
    getSyncStatus.mockResolvedValue(status({ last_synced_at: Date.UTC(2026, 0, 15, 10, 30) }));
    const { w } = await mountIndicator();
    const text = w.find('[data-testid="sync-last-synced"]').text();
    expect(text).not.toBe(en.sync.lastSyncedNever);
    expect(text).not.toMatch(/1970/);
  });

  it("shows the pending-keys count when non-zero, and hides it at zero", async () => {
    getSyncStatus.mockResolvedValue(status({ pending_keys: 3 }));
    const { w } = await mountIndicator();
    expect(w.find('[data-testid="sync-pending"]').exists()).toBe(true);
    expect(w.find('[data-testid="sync-pending"]').text()).toContain("3");

    getSyncStatus.mockResolvedValue(status({ pending_keys: 0 }));
    const { w: w2 } = await mountIndicator();
    expect(w2.find('[data-testid="sync-pending"]').exists()).toBe(false);
  });

  it('the "sync now" button calls SyncNow and is disabled while syncing', async () => {
    getSyncStatus.mockResolvedValue(status({ state: "idle" }));
    const { w } = await mountIndicator();
    const btn = w.find('[data-testid="sync-now-button"]');
    expect(btn.attributes("disabled")).toBeUndefined();
    await btn.trigger("click");
    await flushPromises();
    expect(syncNow).toHaveBeenCalledTimes(1);

    getSyncStatus.mockResolvedValue(status({ state: "syncing" }));
    const { w: w2 } = await mountIndicator();
    expect(w2.find('[data-testid="sync-now-button"]').attributes("disabled")).toBeDefined();
  });

  it("a sync:pulled event with adopted keys renders a dismissible notice naming them by human-readable name", async () => {
    const { w, events } = await mountIndicator();
    const result: PullResult = { Adopted: ["terminal_font_head", "ssh_hosts_encrypted"], Conflict: null };
    events.emit("sync:pulled", result);
    await flushPromises();

    const notice = w.find('[data-testid="sync-notice-adopted"]');
    expect(notice.exists()).toBe(true);
    expect(notice.text()).toContain(en.sync.keys.terminal_font_head);
    expect(notice.text()).toContain(en.sync.keys.ssh_hosts_encrypted);
    // Raw keys must never leak into the rendered text.
    expect(notice.text()).not.toContain("terminal_font_head");
    expect(notice.text()).not.toContain("ssh_hosts_encrypted");

    await w.find('[data-testid="sync-notice-dismiss"]').trigger("click");
    await flushPromises();
    expect(w.find('[data-testid="sync-notice"]').exists()).toBe(false);
  });

  it("conflicted keys are worded distinctly from adopted keys, and both can render together", async () => {
    const { w, events } = await mountIndicator();
    const result: PullResult = {
      Adopted: ["terminal_theme"],
      Conflict: ["terminal_font_size"],
    };
    events.emit("sync:pulled", result);
    await flushPromises();

    const adopted = w.find('[data-testid="sync-notice-adopted"]');
    const conflict = w.find('[data-testid="sync-notice-conflict"]');
    expect(adopted.exists()).toBe(true);
    expect(conflict.exists()).toBe(true);
    expect(adopted.text()).toContain(en.sync.keys.terminal_theme);
    expect(conflict.text()).toContain(en.sync.keys.terminal_font_size);
    // The two sentences must not be collapsed into one: this pins the
    // actual wording template each paragraph uses, not just which keys end
    // up in it (a swap that keeps each key list on its own <p> but reuses
    // the other's phrasing would otherwise slip past a key-only check).
    expect(adopted.text()).toBe(en.sync.pulledAdopted.replace("{keys}", en.sync.keys.terminal_theme));
    expect(conflict.text()).toBe(en.sync.pulledConflict.replace("{keys}", en.sync.keys.terminal_font_size));
    expect(conflict.text()).not.toContain("Updated from another device");
    expect(adopted.text()).not.toContain("Kept this device's version");
  });

  it("falls back to a humanized label for an unmapped key instead of the raw key or crashing", async () => {
    const { w, events } = await mountIndicator();
    events.emit("sync:pulled", { Adopted: ["some_future_pref"], Conflict: null } satisfies PullResult);
    await flushPromises();

    const notice = w.find('[data-testid="sync-notice-adopted"]');
    expect(notice.exists()).toBe(true);
    expect(notice.text()).toContain("Some Future Pref");
  });

  it("does not render a notice for an empty sync:pulled payload", async () => {
    const { w, events } = await mountIndicator();
    events.emit("sync:pulled", { Adopted: null, Conflict: null } satisfies PullResult);
    await flushPromises();
    expect(w.find('[data-testid="sync-notice"]').exists()).toBe(false);
  });

  it("fetches GetSyncStatus once on mount for first paint", async () => {
    await mountIndicator();
    expect(getSyncStatus).toHaveBeenCalledTimes(1);
  });

  it("a sync:status event updates the indicator without a new GetSyncStatus call", async () => {
    const { w, events } = await mountIndicator();
    expect(getSyncStatus).toHaveBeenCalledTimes(1);

    events.emit("sync:status", status({ state: "error", last_error: "boom" }));
    await flushPromises();

    expect(w.find('[data-testid="sync-state"]').attributes("data-state")).toBe("error");
    expect(getSyncStatus).toHaveBeenCalledTimes(1);
  });

  // Item 29 shipped this bug's twin (SnippetRunPanel); pin it here too.
  it("removes both sync:status and sync:pulled listeners on unmount", async () => {
    const events = fakeEventBus();
    const offSpy = vi.fn();
    const realOn = events.on.bind(events);
    events.on = (event, handler) => {
      const off = realOn(event, handler);
      return () => {
        offSpy(event);
        off();
      };
    };

    const { w } = await mountIndicator(events);
    expect(offSpy).not.toHaveBeenCalled();

    w.unmount();

    expect(offSpy).toHaveBeenCalledWith("sync:status");
    expect(offSpy).toHaveBeenCalledWith("sync:pulled");
    expect(offSpy).toHaveBeenCalledTimes(2);
  });

  it("an unmounted instance no longer reacts to events (no leaked handler)", async () => {
    const { w, events } = await mountIndicator();
    w.unmount();
    // Emitting after unmount must not throw, and (since the handler is
    // gone) nothing to assert on the destroyed wrapper's DOM either -- the
    // absence of a thrown error is the point here.
    expect(() => events.emit("sync:status", status({ state: "error", last_error: "x" }))).not.toThrow();
  });

  // Platform-gate safety net: this component is meaningful on desktop only
  // (design doc §6 "No mobile indicator"; mobile syncs prefs over HTTP via
  // lib/prefsSync.capacitor.ts, a wholly separate path with no engine for
  // this status to describe). The real gate lives in SettingsDialog.vue
  // (`v-if="caps.wailsBindings"`, see SettingsDialog.test.ts), matching
  // SettingsProfiles.vue's precedent -- SettingsDialog skips mounting this
  // component at all on non-wails platforms. This test is the defense-in-
  // depth half: even if some future caller mounts it unguarded on a
  // wailsBindings=false shape (the exact mistake item 29 made for the
  // relay-embed settings button), it must degrade quietly instead of dying
  // on app.wailsBindingsNotReady.
  it("mounts under a wailsBindings=false platform shape without throwing", async () => {
    getSyncStatus.mockReset().mockRejectedValue(new Error("Wails bindings not ready"));
    syncNow.mockReset().mockRejectedValue(new Error("Wails bindings not ready"));
    const webPlatform = { ...createFakePlatform(), caps: { ...createFakePlatform().caps, wailsBindings: false } };
    __setPlatformForTests(webPlatform);
    const w = mount(SyncStatusIndicator);
    await flushPromises();

    expect(w.find('[data-testid="sync-indicator"]').exists()).toBe(true);
    expect(w.find('[data-testid="sync-state"]').text()).toBe(en.sync.stateIdle);

    await w.find('[data-testid="sync-now-button"]').trigger("click");
    await flushPromises();
    expect(w.find('[data-testid="sync-trigger-error"]').exists()).toBe(true);
  });

  it("mounts under a wailsBindings=true platform shape and fetches status normally", async () => {
    getSyncStatus.mockResolvedValue(status({ state: "idle" }));
    const wailsPlatform = { ...createFakePlatform(), caps: { ...createFakePlatform().caps, wailsBindings: true } };
    __setPlatformForTests(wailsPlatform);
    const w = mount(SyncStatusIndicator);
    await flushPromises();

    expect(w.find('[data-testid="sync-state"]').text()).toBe(en.sync.stateIdle);
  });
});
