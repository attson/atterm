import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { computed, ref } from "vue";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import FileExplorer from "./FileExplorer.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";
import type { SessionConnection } from "../../lib/connection";
import type { PluginContext } from "../types";

describe("FileExplorer remote filesystem", () => {
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
});
