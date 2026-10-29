import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { computed, ref } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import FileExplorer from "./FileExplorer.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";
import type { PluginContext } from "../types";

describe("FileExplorer — follows the active session", () => {
  const platform = createFakePlatform();

  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    __setPlatformForTests(platform);
    (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockResolvedValue([]);
  });

  afterEach(() => __setPlatformForTests(null));

  it("does not expose an independent SSH file source picker", async () => {
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
    expect(wrapper.find('[data-test="fs-source"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="ssh-upload"]').exists()).toBe(false);
    expect(platform.pluginHost!.fs.listDir).toHaveBeenCalledWith("/local");
  });
});
