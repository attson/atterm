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

  it("renders a filename search box that drives recursive tree search", async () => {
    (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockImplementation(async (path: string) => {
      if (path === "/local") {
        return [
          { name: "src", isDir: true },
          { name: "README.md", isDir: false },
        ];
      }
      if (path === "/local/src") {
        return [{ name: "nested-search.ts", isDir: false }];
      }
      return [];
    });
    const context: PluginContext = {
      activePane: ref(null),
      activeSessionId: computed(() => "local-session"),
      activeIsRemote: computed(() => false),
      activeSessionConnection: computed(() => null),
      activeEndpoint: computed(() => null),
      activeCwd: computed(() => "/local"),
      terminalThemeId: computed(() => "classic"),
      send: vi.fn(),
      showToast: vi.fn(),
    };

    const wrapper = mount(FileExplorer, { props: { context } });
    await flushPromises();
    await wrapper.find('[data-test="file-name-search"]').setValue("search");
    await flushPromises();

    expect(wrapper.text()).toContain("nested-search.ts");
  });

  it("rebinds a remote bridge when its connection is replaced for the same session", async () => {
    const sessionID = ref<string | null>("remote-session");
    const firstUnsubscribe = vi.fn();
    const first = {
      sendFSRequest: vi.fn().mockResolvedValue({
        request_id: "first", ok: true, entries: [{ name: "first.txt", isDir: false }],
      }),
      onFSEvent: vi.fn(() => firstUnsubscribe),
    } as unknown as SessionConnection;
    const second = {
      sendFSRequest: vi.fn().mockResolvedValue({
        request_id: "second", ok: true, entries: [{ name: "second.txt", isDir: false }],
      }),
      onFSEvent: vi.fn(() => () => {}),
    } as unknown as SessionConnection;
    const activeConnection = shallowRef<SessionConnection | null>(first);
    const context: PluginContext = {
      activePane: ref(null),
      activeSessionId: computed(() => sessionID.value),
      activeIsRemote: computed(() => true),
      activeSessionConnection: computed(() => activeConnection.value),
      activeEndpoint: computed(() => null),
      activeCwd: computed(() => "/remote/project"),
      terminalThemeId: computed(() => "classic"),
      send: vi.fn(),
      showToast: vi.fn(),
    };

    const wrapper = mount(FileExplorer, { props: { context } });
    await flushPromises();
    activeConnection.value = second;
    await flushPromises();

    expect(second.sendFSRequest).toHaveBeenCalledWith({ op: "list_dir", path: "/remote/project" });
    expect(firstUnsubscribe).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("second.txt");
  });

  it("preserves a pinned root when the same remote session reconnects", async () => {
    const activeCwd = ref<string | null>("/remote");
    const first = {
      sendFSRequest: vi.fn().mockResolvedValue({ request_id: "first", ok: true, entries: [] }),
      onFSEvent: vi.fn(() => () => {}),
    } as unknown as SessionConnection;
    const second = {
      sendFSRequest: vi.fn().mockResolvedValue({ request_id: "second", ok: true, entries: [] }),
      onFSEvent: vi.fn(() => () => {}),
    } as unknown as SessionConnection;
    const activeConnection = shallowRef<SessionConnection | null>(first);
    const context: PluginContext = {
      activePane: ref(null),
      activeSessionId: computed(() => "remote-session"),
      activeIsRemote: computed(() => true),
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
    activeCwd.value = "/new-cwd";
    activeConnection.value = second;
    await flushPromises();

    expect(wrapper.find(".pin").classes()).toContain("pinned");
    expect(wrapper.find(".root-path").text()).toBe("/remote");
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

  it("does not reuse a local cwd while a remote bridge has no cwd", async () => {
    const activeIsRemote = ref(false);
    const activeSessionID = ref<string | null>("local-session");
    const activeCwd = ref<string | null>("/local");
    const remoteSendFSRequest = vi.fn();
    const remoteConnection = {
      sendFSRequest: remoteSendFSRequest,
      onFSEvent: vi.fn(() => () => {}),
    } as unknown as SessionConnection;
    const activeConnection = shallowRef<SessionConnection | null>(null);
    (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    const context: PluginContext = {
      activePane: ref(null),
      activeSessionId: computed(() => activeSessionID.value),
      activeIsRemote: computed(() => activeIsRemote.value),
      activeSessionConnection: computed(() => activeConnection.value),
      activeEndpoint: computed(() => null),
      activeCwd: computed(() => activeCwd.value),
      terminalThemeId: computed(() => "classic"),
      send: vi.fn(),
      showToast: vi.fn(),
    };

    const wrapper = mount(FileExplorer, { props: { context } });
    await flushPromises();
    expect(platform.pluginHost!.fs.listDir).toHaveBeenCalledWith("/local");

    activeCwd.value = null;
    activeSessionID.value = "remote-session";
    activeConnection.value = remoteConnection;
    activeIsRemote.value = true;
    await flushPromises();

    expect(remoteSendFSRequest).not.toHaveBeenCalled();
    expect(wrapper.find(".tree-scroll .placeholder").exists()).toBe(true);
    expect(wrapper.find(".root-path").text()).not.toBe("/local");
  });

  it("keeps a remote cwd fallback when that same remote identity briefly has no cwd", async () => {
    const activeCwd = ref<string | null>("/remote");
    const sendFSRequest = vi.fn().mockResolvedValue({ request_id: "root", ok: true, entries: [] });
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
      activeCwd: computed(() => activeCwd.value),
      terminalThemeId: computed(() => "classic"),
      send: vi.fn(),
      showToast: vi.fn(),
    };

    const wrapper = mount(FileExplorer, { props: { context } });
    await flushPromises();
    activeCwd.value = null;
    await flushPromises();

    expect(wrapper.find(".root-path").text()).toBe("/remote");
    expect(wrapper.find(".tree-scroll .placeholder").exists()).toBe(false);
    expect(sendFSRequest).toHaveBeenCalledWith({ op: "list_dir", path: "/remote" });
  });
});
