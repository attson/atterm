import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsProfilesMobile from "./SettingsProfilesMobile.vue";
import { resetI18nForTest } from "../i18n";
import { en } from "../i18n/messages/en";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";
import { setAccountKeyProvider } from "../lib/account-key";
import type { ProfileView } from "../lib/syncedBlobs";

// The crypto envelope (openUnsequencedFrame / the Go-generated golden
// vector) is task 1's contract and is pinned in syncedBlobs.test.ts. This
// component's own job is: gate by platform, read the right localStorage
// key, hand the right account key to the reader, and render what comes
// back correctly (including the syncEnv distinction) — none of which
// requires a real sealed blob, so the reader is mocked here.
const openProfilesBlob = vi.fn();
vi.mock("../lib/syncedBlobs", async () => {
  const actual = await vi.importActual<typeof import("../lib/syncedBlobs")>("../lib/syncedBlobs");
  return {
    ...actual,
    openProfilesBlob: (...a: unknown[]) => openProfilesBlob(...a),
  };
});

const ACCOUNT_KEY = new Uint8Array(32).fill(7);
const RAW_VALUE = "ZmFrZS1zZWFsZWQtYmxvYg=="; // arbitrary — openProfilesBlob is mocked

function setSyncedValue(raw: string | undefined): void {
  if (raw === undefined) {
    localStorage.removeItem("atterm.profiles_encrypted.value");
  } else {
    localStorage.setItem("atterm.profiles_encrypted.value", JSON.stringify(raw));
  }
}

let mounted: ReturnType<typeof mount>[] = [];

function mountPanel(capsOverrides: Record<string, unknown> = { capacitor: true }) {
  const base = createFakePlatform();
  __setPlatformForTests({ ...base, caps: { ...base.caps, wailsBindings: false, capacitor: false, ...capsOverrides } });
  const w = mount(SettingsProfilesMobile);
  mounted.push(w);
  return w;
}

function sampleProfiles(): { profiles: ProfileView[]; defaultProfileId: string } {
  return {
    defaultProfileId: "p-synced",
    profiles: [
      {
        id: "p-synced",
        name: "Synced Profile",
        shell: "/bin/zsh",
        cwd: "/home/u/work",
        startupCmd: "tmux attach",
        syncEnv: true,
        env: { FOO: "bar" },
      },
      {
        id: "p-unsynced",
        name: "Unsynced Profile",
        shell: "/bin/bash",
        cwd: "/home/u",
        startupCmd: "",
        syncEnv: false,
        env: undefined,
      },
      {
        id: "p-synced-empty-env",
        name: "Synced Empty Env Profile",
        shell: "/bin/fish",
        cwd: "",
        startupCmd: "",
        syncEnv: true,
        env: {},
      },
    ],
  };
}

describe("SettingsProfilesMobile", () => {
  beforeEach(() => {
    resetI18nForTest();
    localStorage.clear();
    setAccountKeyProvider(null);
    openProfilesBlob.mockReset();
  });

  afterEach(() => {
    // usePolledAccountKey starts a real setInterval when a view mounts
    // without an account key (the "locked" tests below) — unmount so it
    // doesn't keep firing into later tests.
    for (const w of mounted) w.unmount();
    mounted = [];
    setAccountKeyProvider(null);
  });

  // ---- platform gate: same shell mounts on capacitor, wails desktop, and a
  // plain browser (web) tab — this view must render only under capacitor. ----

  it("renders nothing and never reads local storage under a wails-desktop platform shape", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    const w = mountPanel({ wailsBindings: true, capacitor: false });
    await flushPromises();
    expect(w.find('[data-testid="mobile-profiles-panel"]').exists()).toBe(false);
    expect(w.text()).toBe("");
    expect(openProfilesBlob).not.toHaveBeenCalled();
  });

  it("renders nothing under a plain web (browser tab) platform shape", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    const w = mountPanel({ wailsBindings: false, capacitor: false });
    await flushPromises();
    expect(w.find('[data-testid="mobile-profiles-panel"]').exists()).toBe(false);
    expect(openProfilesBlob).not.toHaveBeenCalled();
  });

  it("mounts and renders under the capacitor platform shape", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockReturnValue(sampleProfiles());
    const w = mountPanel({ capacitor: true });
    await flushPromises();
    expect(w.find('[data-testid="mobile-profiles-panel"]').exists()).toBe(true);
  });

  // ---- account key not ready yet ----

  it('shows the "locked" state and never calls the reader when there is no account key', async () => {
    setSyncedValue(RAW_VALUE);
    const w = mountPanel();
    await flushPromises();
    expect(w.find('[data-testid="mobile-profiles-locked"]').text()).toBe(en.settings.mobileSync.locked);
    expect(openProfilesBlob).not.toHaveBeenCalled();
  });

  // ---- nothing synced yet ----

  it('shows the "no data" state when the account key is present but nothing has synced', async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    const w = mountPanel();
    await flushPromises();
    expect(w.find('[data-testid="mobile-profiles-no-data"]').text()).toBe(en.settings.mobileSync.noData);
    expect(openProfilesBlob).not.toHaveBeenCalled();
  });

  // ---- reader failure ----

  it("shows an error message when the reader throws (wrong key / corrupted blob)", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockImplementation(() => {
      throw new Error("openProfilesBlob: could not decrypt (wrong account key or corrupted blob)");
    });
    const w = mountPanel();
    await flushPromises();
    expect(w.find('[data-testid="mobile-profiles-error"]').text()).toContain("could not decrypt");
  });

  // ---- empty profile list ----

  it('shows the "empty" state when the blob opens but has zero profiles', async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockReturnValue({ profiles: [], defaultProfileId: "" });
    const w = mountPanel();
    await flushPromises();
    expect(w.find('[data-testid="mobile-profiles-empty"]').text()).toBe(en.settings.mobileProfiles.empty);
  });

  // ---- the core distinction this task exists to pin ----

  it('reads the account key and profiles_encrypted key, and calls the reader with exactly those', async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockReturnValue(sampleProfiles());
    mountPanel();
    await flushPromises();
    expect(openProfilesBlob).toHaveBeenCalledWith(ACCOUNT_KEY, RAW_VALUE);
  });

  it('syncEnv=false renders "not synced from that machine", never "none"', async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockReturnValue(sampleProfiles());
    const w = mountPanel();
    await flushPromises();

    const items = w.findAll('[data-testid="mobile-profile-item"]');
    const unsynced = items.find((i) => i.text().includes("Unsynced Profile"))!;
    const envCell = unsynced.find('[data-testid="mobile-profile-env"]');
    expect(envCell.text()).toBe(en.settings.mobileProfiles.envNotSynced);
    expect(envCell.text()).not.toBe(en.settings.mobileProfiles.envNone);
  });

  it('syncEnv=true with an empty env object renders "none", not "not synced"', async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockReturnValue(sampleProfiles());
    const w = mountPanel();
    await flushPromises();

    const items = w.findAll('[data-testid="mobile-profile-item"]');
    const emptyEnv = items.find((i) => i.text().includes("Synced Empty Env Profile"))!;
    const envCell = emptyEnv.find('[data-testid="mobile-profile-env"]');
    expect(envCell.text()).toBe(en.settings.mobileProfiles.envNone);
    expect(envCell.text()).not.toBe(en.settings.mobileProfiles.envNotSynced);
  });

  it("syncEnv=true with values renders the actual key=value pairs", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockReturnValue(sampleProfiles());
    const w = mountPanel();
    await flushPromises();

    const items = w.findAll('[data-testid="mobile-profile-item"]');
    const synced = items.find((i) => i.text().includes("Synced Profile") && !i.text().includes("Empty"))!;
    expect(synced.find('[data-testid="mobile-profile-env"]').text()).toContain("FOO=bar");
  });

  it("marks the default profile with a badge and no others", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockReturnValue(sampleProfiles());
    const w = mountPanel();
    await flushPromises();

    const items = w.findAll('[data-testid="mobile-profile-item"]');
    const badged = items.filter((i) => i.find('[data-testid="mobile-profile-default-badge"]').exists());
    expect(badged).toHaveLength(1);
    expect(badged[0].text()).toContain("Synced Profile");
  });

  // ---- read-only: no editing affordance at all ----

  it("renders no buttons or inputs anywhere in the panel", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockReturnValue(sampleProfiles());
    const w = mountPanel();
    await flushPromises();

    expect(w.findAll("button")).toHaveLength(0);
    expect(w.findAll("input")).toHaveLength(0);
    expect(w.findAll("textarea")).toHaveLength(0);
  });
});
