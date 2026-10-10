import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsProfiles from "./SettingsProfiles.vue";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform, fakeEventBus } from "../platform/__tests__/_fakePlatform";
import { __setBindingsForTest } from "../lib/api";
import type { SessionProfile } from "../lib/api";
import { en } from "../i18n/messages/en";

describe("SettingsProfiles", () => {
  // Named so assertions below can inspect call args (e.g. what SetProfiles
  // was actually persisted with) the same way
  // SettingsTerminalAppearance.test.ts keeps getTerminalFontSizeMock around.
  let getProfilesMock: ReturnType<typeof vi.fn>;
  let setProfilesMock: ReturnType<typeof vi.fn>;
  let getDefaultProfileIDMock: ReturnType<typeof vi.fn>;
  let setDefaultProfileIDMock: ReturnType<typeof vi.fn>;

  const oneProfile: SessionProfile = {
    id: "p1",
    name: "Work",
    shell: "/bin/zsh",
    cwd: "/Users/me/work",
    startup_cmd: "",
    sync_env: false,
  };

  beforeEach(() => {
    __setPlatformForTests(createFakePlatform());
    getProfilesMock = vi.fn().mockResolvedValue([oneProfile]);
    setProfilesMock = vi.fn().mockResolvedValue(undefined);
    getDefaultProfileIDMock = vi.fn().mockResolvedValue("");
    setDefaultProfileIDMock = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({
      GetProfiles: getProfilesMock,
      SetProfiles: setProfilesMock,
      GetDefaultProfileID: getDefaultProfileIDMock,
      SetDefaultProfileID: setDefaultProfileIDMock,
    } as any);
  });

  afterEach(() => {
    __setBindingsForTest(undefined);
    __setPlatformForTests(null);
  });

  async function mountProfiles() {
    const w = mount(SettingsProfiles);
    await flushPromises();
    return w;
  }

  it("lists the saved profiles", async () => {
    const w = await mountProfiles();
    expect(w.find('[data-testid="profile-row-p1"]').exists()).toBe(true);
    expect(w.text()).toContain("Work");
  });

  it("explains that env stays on this machine by default", async () => {
    const w = await mountProfiles();
    // sync_env off is the default (§5.1 of the design doc) — the panel must
    // say so somewhere in the intro copy or the per-profile editor, not just
    // leave a bare unlabeled checkbox.
    expect(w.text()).toMatch(/this machine|machine-local|stays on this machine/i);
  });

  it("adds a new profile and persists it", async () => {
    getProfilesMock.mockResolvedValue([]);
    const w = await mountProfiles();
    await w.find('[data-testid="profile-add"]').trigger("click");
    await w.find('[data-testid="profile-edit-name"]').setValue("Personal");
    await w.find('[data-testid="profile-edit-save"]').trigger("click");
    await flushPromises();

    expect(setProfilesMock).toHaveBeenCalledTimes(1);
    const saved = setProfilesMock.mock.calls[0][0] as SessionProfile[];
    expect(saved).toHaveLength(1);
    expect(saved[0].name).toBe("Personal");
    // sync_env must default to false/falsy for a freshly added profile.
    expect(saved[0].sync_env).toBeFalsy();
  });

  it("groups profile name and shell without pulling long fields into the row", async () => {
    const w = await mountProfiles();
    await w.find('[data-testid="profile-edit-p1"]').trigger("click");
    const grid = w.get('[data-testid="profile-primary-grid"]');

    expect(grid.find('[data-testid="profile-edit-name"]').exists()).toBe(true);
    expect(grid.find('[data-testid="profile-edit-shell"]').exists()).toBe(true);
    expect(grid.find('[data-testid="profile-edit-cwd"]').exists()).toBe(false);
  });

  it("edits an existing profile in place", async () => {
    const w = await mountProfiles();
    await w.find('[data-testid="profile-edit-p1"]').trigger("click");
    await w.find('[data-testid="profile-edit-name"]').setValue("Work (renamed)");
    await w.find('[data-testid="profile-edit-save"]').trigger("click");
    await flushPromises();

    expect(setProfilesMock).toHaveBeenCalledTimes(1);
    const saved = setProfilesMock.mock.calls[0][0] as SessionProfile[];
    expect(saved).toHaveLength(1);
    expect(saved[0].id).toBe("p1");
    expect(saved[0].name).toBe("Work (renamed)");
  });

  it("deletes a profile", async () => {
    const w = await mountProfiles();
    await w.find('[data-testid="profile-delete-p1"]').trigger("click");
    await flushPromises();

    expect(setProfilesMock).toHaveBeenCalledTimes(1);
    const saved = setProfilesMock.mock.calls[0][0] as SessionProfile[];
    expect(saved).toHaveLength(0);
    expect(w.find('[data-testid="profile-row-p1"]').exists()).toBe(false);
  });

  it("sets and shows the default profile", async () => {
    const w = await mountProfiles();
    await w.find('[data-testid="profile-set-default-p1"]').trigger("click");
    await flushPromises();

    expect(setDefaultProfileIDMock).toHaveBeenCalledWith("p1");
    expect(w.find('[data-testid="profile-default-badge"]').exists()).toBe(true);
  });

  // Deleting the profile that is currently the default must not leave a
  // dangling DefaultProfileID pointing at nothing (see task-4 judgment call
  // + desktop/profiles.go resolveDefaultProfileID, which already handles the
  // inbound-sync side of this).
  it("clears the default when the default profile is deleted", async () => {
    getDefaultProfileIDMock.mockResolvedValue("p1");
    const w = await mountProfiles();
    await w.find('[data-testid="profile-delete-p1"]').trigger("click");
    await flushPromises();

    expect(setDefaultProfileIDMock).toHaveBeenCalledWith("");
  });

  // Regression for the "a failed delete still clears the default" finding:
  // persist()'s error path calls loadProfiles() to revert local state, and
  // that reload restores defaultProfileId to "p1" (since the delete never
  // actually reached the server) — a naive re-check of
  // `defaultProfileId.value === id` after persist() would then look true
  // and fire a second, unrelated, *successful* setDefaultProfileID("") call
  // even though the delete failed and nothing should have changed.
  it("does not clear the default when deleting the default profile fails to persist", async () => {
    getDefaultProfileIDMock.mockResolvedValue("p1");
    setProfilesMock.mockRejectedValueOnce(new Error("network down"));
    const w = await mountProfiles();
    await w.find('[data-testid="profile-delete-p1"]').trigger("click");
    await flushPromises();

    expect(setProfilesMock).toHaveBeenCalledTimes(1);
    expect(setDefaultProfileIDMock).not.toHaveBeenCalled();
    // The failed delete's revert (loadProfiles) should have put p1 back.
    expect(w.find('[data-testid="profile-row-p1"]').exists()).toBe(true);
    expect(w.text()).toContain("network down");
  });

  it("reloads from Go on prefs:changed without persisting", async () => {
    const events = fakeEventBus();
    __setPlatformForTests({ ...createFakePlatform(), events });
    mount(SettingsProfiles);
    await flushPromises();
    getProfilesMock.mockClear();
    setProfilesMock.mockClear();

    events.emit("prefs:changed", undefined);
    await flushPromises();

    expect(getProfilesMock).toHaveBeenCalled();
    expect(setProfilesMock).not.toHaveBeenCalled();
  });

  function peerSessions() {
    return [
      { session_id: "s-view", host_id: "host-view", host: "Hidden Viewer", remote_permission: "view" },
      { session_id: "s-zulu", host_id: "host-zulu", host: "Zulu Laptop", remote_permission: "control" },
      { session_id: "s-alpha-1", host_id: "host-alpha", host: "Alpha Desktop", remote_permission: "full" },
      { session_id: "s-alpha-2", host_id: "host-alpha", host: "Alpha Desktop", remote_permission: "full" },
    ];
  }

  function mountPeerProfiles(createSessionWithProfile = vi.fn().mockResolvedValue("created-session")) {
    const platform = createFakePlatform();
    platform.sessions.listRemoteSessions = vi.fn().mockResolvedValue(peerSessions());
    platform.sessions.createSessionWithProfile = createSessionWithProfile;
    __setPlatformForTests(platform);
    return { wrapper: mount(SettingsProfiles), createSessionWithProfile };
  }

  it("shows one custom-dropdown option per control/full Peer host", async () => {
    const { wrapper } = mountPeerProfiles();
    await flushPromises();

    const picker = wrapper.get('[data-testid="profile-peer-host"]');
    await picker.get('[data-testid="select-trigger"]').trigger("click");
    expect(picker.findAll('[data-testid="select-option"]').map((option) => option.text())).toEqual([
      "Alpha Desktop",
      "Zulu Laptop",
    ]);
    expect(wrapper.text()).not.toContain("Hidden Viewer");
  });

  it("guards rapid duplicate Peer actions and emits the created session id", async () => {
    let resolveCreate!: (sessionID: string) => void;
    const create = vi.fn(() => new Promise<string>((resolve) => { resolveCreate = resolve; }));
    const { wrapper } = mountPeerProfiles(create);
    await flushPromises();

    const button = wrapper.get('[data-testid="profile-peer-open-p1"]');
    void button.trigger("click");
    await button.trigger("click");
    expect(create).toHaveBeenCalledOnce();
    expect(create).toHaveBeenCalledWith("host-alpha", "p1");

    resolveCreate("created-session");
    await flushPromises();
    expect(wrapper.emitted("session-created")).toEqual([["created-session"]]);
  });

  it("surfaces a stable Peer timeout error without retrying", async () => {
    const create = vi.fn().mockRejectedValue(new Error("timeout"));
    const { wrapper } = mountPeerProfiles(create);
    await flushPromises();

    await wrapper.get('[data-testid="profile-peer-open-p1"]').trigger("click");
    await flushPromises();
    expect(create).toHaveBeenCalledOnce();
    expect(wrapper.get('[data-testid="profile-peer-error"]').text()).toBe(en.settings.profiles.peerOpenErrors.timeout);
    expect(wrapper.emitted("session-created")).toBeUndefined();
  });
});
