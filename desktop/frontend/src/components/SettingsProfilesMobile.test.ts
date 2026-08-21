import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsProfilesMobile from "./SettingsProfilesMobile.vue";
import source from "./SettingsProfilesMobile.vue?raw";
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

function mountPanel(
  capsOverrides: Record<string, unknown> = { capacitor: true },
  sessionsOverrides: Record<string, unknown> = {},
) {
  const base = createFakePlatform();
  __setPlatformForTests({
    ...base,
    caps: { ...base.caps, wailsBindings: false, capacitor: false, ...capsOverrides },
    sessions: { ...base.sessions, ...sessionsOverrides },
  });
  const w = mount(SettingsProfilesMobile);
  mounted.push(w);
  return w;
}

function sampleHosts() {
  return [
    { session_id: "s1", host_id: "host-a", host: "Alice's Mac", user: "alice", title: "", cols: 80, rows: 24 },
    { session_id: "s2", host_id: "host-b", host: "Bob's PC", user: "bob", title: "", cols: 80, rows: 24 },
    // A second session on host-a must not produce a duplicate host option.
    { session_id: "s3", host_id: "host-a", host: "Alice's Mac", user: "alice", title: "", cols: 80, rows: 24 },
  ];
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

  // ---- read-only: no *editing* affordance (task 6 legitimately adds an
  // "open with profile" action, which the design docs distinguish from
  // editing — see design doc §6 non-goals vs task-6-brief.md). ----

  it("renders no text inputs or textareas anywhere in the panel (still nothing to edit)", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockReturnValue(sampleProfiles());
    const w = mountPanel({ capacitor: true }, { listRemoteSessions: vi.fn().mockResolvedValue(sampleHosts()) });
    await flushPromises();

    expect(w.findAll("input")).toHaveLength(0);
    expect(w.findAll("textarea")).toHaveLength(0);
    // The host <select> and per-profile "Open" <button> are the one
    // interactive affordance this task adds; buttons alone don't let you
    // change a profile's fields.
    expect(w.findAll("button").length).toBeGreaterThan(0);
  });

  // ---- the "open with profile" flow (task 6) ----

  describe("open with profile", () => {
    function mountReady(sessionsOverrides: Record<string, unknown> = {}) {
      setAccountKeyProvider(() => ACCOUNT_KEY);
      setSyncedValue(RAW_VALUE);
      openProfilesBlob.mockReturnValue(sampleProfiles());
      return mountPanel(
        { capacitor: true },
        { listRemoteSessions: vi.fn().mockResolvedValue(sampleHosts()), ...sessionsOverrides },
      );
    }

    it("shows a 'no hosts' hint and no host select when listRemoteSessions returns nothing", async () => {
      const w = mountReady({ listRemoteSessions: vi.fn().mockResolvedValue([]) });
      await flushPromises();
      expect(w.find('[data-testid="mobile-profiles-no-hosts"]').text()).toBe(en.settings.mobileProfiles.noHosts);
      expect(w.find('[data-testid="mobile-profiles-host-select"]').exists()).toBe(false);
    });

    it("dedupes hosts by host_id and offers one option per desktop", async () => {
      const w = mountReady();
      await flushPromises();
      const options = w.findAll('[data-testid="mobile-profiles-host-select"] option');
      expect(options).toHaveLength(2);
      expect(options.map((o) => o.text())).toEqual(["Alice's Mac", "Bob's PC"]);
    });

    it("picking a profile and a target host calls createSessionWithProfile with those two ids", async () => {
      const createSessionWithProfile = vi.fn().mockResolvedValue("new-session-id");
      const w = mountReady({ createSessionWithProfile });
      await flushPromises();

      await w.find('[data-testid="mobile-profiles-host-select"]').setValue("host-b");
      const items = w.findAll('[data-testid="mobile-profile-item"]');
      const target = items.find((i) => i.text().includes("Synced Profile") && !i.text().includes("Empty"))!;
      await target.find('[data-testid="mobile-profile-open"]').trigger("click");
      await flushPromises();

      expect(createSessionWithProfile).toHaveBeenCalledTimes(1);
      expect(createSessionWithProfile).toHaveBeenCalledWith("host-b", "p-synced");
    });

    it("emits session-created with the resolved session id on success", async () => {
      const createSessionWithProfile = vi.fn().mockResolvedValue("new-session-id");
      const w = mountReady({ createSessionWithProfile });
      await flushPromises();

      const items = w.findAll('[data-testid="mobile-profile-item"]');
      const target = items.find((i) => i.text().includes("Synced Profile") && !i.text().includes("Empty"))!;
      await target.find('[data-testid="mobile-profile-open"]').trigger("click");
      await flushPromises();

      expect(w.emitted("session-created")).toEqual([["new-session-id"]]);
    });

    it("two rapid taps on the same Open button produce exactly one request", async () => {
      let resolveCreate!: (id: string) => void;
      const createSessionWithProfile = vi.fn(
        () => new Promise<string>((resolve) => { resolveCreate = resolve; }),
      );
      const w = mountReady({ createSessionWithProfile });
      await flushPromises();

      const items = w.findAll('[data-testid="mobile-profile-item"]');
      const target = items.find((i) => i.text().includes("Synced Profile") && !i.text().includes("Empty"))!;
      const btn = target.find('[data-testid="mobile-profile-open"]');
      // Two taps back to back, deliberately NOT awaited between them, so
      // both DOM click events dispatch before Vue's reactive re-render has
      // a chance to flip the button's `disabled` attribute. This isolates
      // the in-script guard (creating.value !== null) from the `:disabled`
      // binding, which on its own would also stop a second *awaited* click
      // but says nothing about a real double-tap landing inside one frame.
      void btn.trigger("click");
      await btn.trigger("click");

      expect(createSessionWithProfile).toHaveBeenCalledTimes(1);

      resolveCreate("s-done");
      await flushPromises();
      expect(w.emitted("session-created")).toEqual([["s-done"]]);
    });

    it("a timeout error is surfaced and does not retry", async () => {
      const createSessionWithProfile = vi.fn().mockRejectedValue(new Error("timeout"));
      const w = mountReady({ createSessionWithProfile });
      await flushPromises();

      const items = w.findAll('[data-testid="mobile-profile-item"]');
      const target = items.find((i) => i.text().includes("Synced Profile") && !i.text().includes("Empty"))!;
      await target.find('[data-testid="mobile-profile-open"]').trigger("click");
      await flushPromises();

      expect(createSessionWithProfile).toHaveBeenCalledTimes(1);
      expect(w.find('[data-testid="mobile-profiles-create-error"]').text())
        .toBe(en.settings.mobileProfiles.openErrors.timeout);
      expect(w.emitted("session-created")).toBeUndefined();
    });

    it("permission_denied renders an actionable message, not the raw code", async () => {
      const createSessionWithProfile = vi.fn().mockRejectedValue(new Error("permission_denied"));
      const w = mountReady({ createSessionWithProfile });
      await flushPromises();

      const items = w.findAll('[data-testid="mobile-profile-item"]');
      const target = items.find((i) => i.text().includes("Synced Profile") && !i.text().includes("Empty"))!;
      await target.find('[data-testid="mobile-profile-open"]').trigger("click");
      await flushPromises();

      const errText = w.find('[data-testid="mobile-profiles-create-error"]').text();
      expect(errText).toBe(en.settings.mobileProfiles.openErrors.permission_denied);
      expect(errText).not.toBe("permission_denied");
    });

    it("an unrecognized error message (e.g. desktop's raw NewSession failure) falls into the generic bucket", async () => {
      const createSessionWithProfile = vi.fn().mockRejectedValue(new Error("exec: \"nope\": file not found"));
      const w = mountReady({ createSessionWithProfile });
      await flushPromises();

      const items = w.findAll('[data-testid="mobile-profile-item"]');
      const target = items.find((i) => i.text().includes("Synced Profile") && !i.text().includes("Empty"))!;
      await target.find('[data-testid="mobile-profile-open"]').trigger("click");
      await flushPromises();

      const errText = w.find('[data-testid="mobile-profiles-create-error"]').text();
      expect(errText).toContain("exec: \"nope\": file not found");
    });
  });

  // ---- platform gate applies to the open flow too ----

  it("never calls createSessionWithProfile or listRemoteSessions under a non-capacitor platform shape", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openProfilesBlob.mockReturnValue(sampleProfiles());
    const listRemoteSessions = vi.fn().mockResolvedValue(sampleHosts());
    const createSessionWithProfile = vi.fn().mockResolvedValue("s1");
    const w = mountPanel({ wailsBindings: true, capacitor: false }, { listRemoteSessions, createSessionWithProfile });
    await flushPromises();

    expect(w.find('[data-testid="mobile-profiles-panel"]').exists()).toBe(false);
    expect(listRemoteSessions).not.toHaveBeenCalled();
    expect(createSessionWithProfile).not.toHaveBeenCalled();
  });

  // Same guard as SettingsSSHHostsMobile.test.ts's credential-leak pair: a
  // bypass that re-derives the payload with the raw crypto primitives
  // instead of going through the mocked openProfilesBlob would be invisible
  // to any assertion on rendered output here, so the only thing that
  // catches it is proving the import never happens. ProfileView carries no
  // credential field, but this keeps both mobile views symmetric.
  it("does not import the raw crypto primitives directly (must go through openProfilesBlob only)", () => {
    expect(source).not.toMatch(/from\s+["']\.\.\/lib\/opaque["']/);
    expect(source).not.toContain("openUnsequencedFrame");
    expect(source).not.toContain("b64ToBytes");
  });
});
