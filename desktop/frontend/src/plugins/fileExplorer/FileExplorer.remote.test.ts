import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { computed, ref, shallowRef } from "vue";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import FileExplorer from "./FileExplorer.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";
import type { SessionConnection } from "../../lib/connection";
import type { PluginContext } from "../types";

describe("FileExplorer filesystem bridge", () => {
  const platform = createFakePlatform();

  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    __setPlatformForTests(platform);
  });

  afterEach(() => {
    __setPlatformForTests(null);
  });

  it("uses the active remote connection to render the remote directory", async () => {
    const sendFSRequest = vi.fn().mockResolvedValue({
      request_id: "list-root",
      ok: true,
      entries: [{ name: "remote-only.txt", isDir: false }],
    });
    const connection = {
      sendFSRequest,
      onFSEvent: vi.fn(() => () => {}),
    } as unknown as SessionConnection;
    const context: PluginContext = {
      activePane: ref(null),
      activeSessionId: computed(() => "remote-session"),
      activeIsRemote: computed(() => true),
      activeSessionConnection: computed(() => connection),
      activeEndpoint: computed(() => null),
      activeCwd: computed(() => "/remote/project"),
      terminalThemeId: computed(() => "classic"),
      send: vi.fn(),
      showToast: vi.fn(),
    };

    const wrapper = mount(FileExplorer, { props: { context } });
    await flushPromises();

    expect(sendFSRequest).toHaveBeenCalledWith({ op: "list_dir", path: "/remote/project" });
    expect(platform.pluginHost!.fs.listDir).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("remote-only.txt");
  });

  it("keeps the local bridge and pinned root when switching local sessions", async () => {
    const activeSessionID = ref<string | null>("local-one");
    const activeConnection = shallowRef<SessionConnection | null>({} as SessionConnection);
    const activeCwd = ref<string | null>("/local/one");
    (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    const context: PluginContext = {
      activePane: ref(null),
      activeSessionId: computed(() => activeSessionID.value),
      activeIsRemote: computed(() => false),
      activeSessionConnection: computed(() => activeConnection.value),
      activeEndpoint: computed(() => null),
      activeCwd: computed(() => activeCwd.value),
      terminalThemeId: computed(() => "classic"),
      send: vi.fn(),
      showToast: vi.fn(),
    };

    const wrapper = mount(FileExplorer, { props: { context } });
    await flushPromises();
    await wrapper.find(".pin").trigger("click");

    activeSessionID.value = "local-two";
    activeConnection.value = {} as SessionConnection;
    activeCwd.value = "/local/two";
    await flushPromises();

    expect(wrapper.find(".root-path").text()).toBe("/local/one");
    expect(platform.pluginHost!.fs.listDir).toHaveBeenCalledTimes(1);
    expect(platform.events.on).toHaveBeenCalledTimes(1);
  });
});
