import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { computed, ref } from "vue";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import FileExplorer from "./FileExplorer.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";
import { __setBindingsForTest } from "../../lib/api";
import type { PluginContext } from "../types";

// The saved hosts as the *backend* reports them for this source. The
// ProxyCommand host is absent because the Go side removed it — atterm never
// runs an arbitrary proxy command, so that host cannot be connected at all and
// a row for it would only produce a failure the user cannot act on. The
// exclusion itself is pinned in desktop/sftp_source_test.go; what this file
// pins is that the panel offers exactly what it is handed.
const HOSTS = [
  { id: "plain", alias: "build-01", host: "10.0.0.1", user: "root", auth_kind: "password" },
  { id: "jump", alias: "behind-bastion", host: "10.0.0.2", user: "root", auth_kind: "password", proxy_jump: "plain" },
];

function localContext(): PluginContext {
  return {
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
}

describe("FileExplorer — the SSH host data source", () => {
  const platform = createFakePlatform();
  let panel: ReturnType<typeof mount> | null = null;

  // Unmount before the bindings go: the SSH source releases the host's
  // connection on teardown, and doing that against a cleared binding surface
  // is noise this suite does not need to reproduce.
  function mountPanel(context: PluginContext) {
    panel = mount(FileExplorer, { props: { context } });
    return panel;
  }

  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    __setPlatformForTests(platform);
    (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockResolvedValue([]);
  });

  afterEach(() => {
    panel?.unmount();
    panel = null;
    __setPlatformForTests(null);
    __setBindingsForTest(undefined);
  });

  function bind(overrides: Record<string, unknown> = {}) {
    __setBindingsForTest({
      ListSFTPHosts: vi.fn().mockResolvedValue(HOSTS),
      SFTPListDir: vi.fn().mockResolvedValue({ path: "/", entries: [], truncated: false, total: 0 }),
      SFTPFileMeta: vi.fn(),
      SFTPReadFile: vi.fn(),
      SFTPWriteFile: vi.fn(),
      SFTPCreateFile: vi.fn(),
      SFTPMkdir: vi.fn(),
      SFTPRename: vi.fn(),
      SFTPRemove: vi.fn(),
      SFTPDisconnect: vi.fn().mockResolvedValue(undefined),
      ...overrides,
    } as never);
  }

  it("offers every connectable saved host as a source, and nothing else", async () => {
    bind();
    const wrapper = mountPanel(localContext());
    await flushPromises();

    const select = wrapper.get('[data-test="fs-source"]');
    const labels = select.findAll("option").map((o) => o.text());
    expect(labels).toEqual(["Active pane", "build-01", "behind-bastion"]);
    // A ProxyCommand host never reaches the list; the backend removed it.
    expect(labels.some((l) => /corkscrew/i.test(l))).toBe(false);
  });

  it("hides the picker entirely when there are no SSH hosts", async () => {
    bind({ ListSFTPHosts: vi.fn().mockResolvedValue([]) });
    const wrapper = mountPanel(localContext());
    await flushPromises();
    expect(wrapper.find('[data-test="fs-source"]').exists()).toBe(false);
  });

  it("lists the chosen host's filesystem instead of the local one", async () => {
    const SFTPListDir = vi.fn().mockResolvedValue({
      path: "/",
      entries: [{ name: "srv", isDir: true }, { name: "motd", isDir: false }],
      truncated: false,
      total: 2,
    });
    bind({ SFTPListDir });
    const wrapper = mountPanel(localContext());
    await flushPromises();

    await wrapper.get('[data-test="fs-source"]').setValue("plain");
    await flushPromises();

    expect(SFTPListDir).toHaveBeenCalledWith("plain", "/");
    expect(wrapper.text()).toContain("motd");
    // The local pane's cwd is a path on *this* machine and must not become the
    // root of a tree on another one.
    expect(wrapper.get(".root-path").text()).toBe("/");
  });

  it("tells the user when a listing was truncated", async () => {
    // 2 of 3000: the failure this exists to prevent is a user believing the
    // directory holds two files.
    const SFTPListDir = vi.fn().mockResolvedValue({
      path: "/",
      entries: [{ name: "a", isDir: false }, { name: "b", isDir: false }],
      truncated: true,
      total: 3000,
    });
    bind({ SFTPListDir });
    const wrapper = mountPanel(localContext());
    await flushPromises();

    await wrapper.get('[data-test="fs-source"]').setValue("plain");
    await flushPromises();

    const notice = wrapper.find('[data-test="listing-truncated"]');
    expect(notice.exists()).toBe(true);
    expect(notice.text()).toContain("3000");
    expect(notice.text()).toContain("2");
  });

  it("does not show a truncation notice for a complete listing", async () => {
    bind({
      SFTPListDir: vi.fn().mockResolvedValue({
        path: "/", entries: [{ name: "a", isDir: false }], truncated: false, total: 1,
      }),
    });
    const wrapper = mountPanel(localContext());
    await flushPromises();
    await wrapper.get('[data-test="fs-source"]').setValue("plain");
    await flushPromises();

    expect(wrapper.find('[data-test="listing-truncated"]').exists()).toBe(false);
  });

  it("refuses an upload onto an existing path with something the user can act on", async () => {
    const SFTPWriteFile = vi.fn().mockRejectedValue(new Error("already_exists: /app.conf"));
    bind({ SFTPWriteFile });
    const context = localContext();
    const wrapper = mountPanel(context);
    await flushPromises();
    await wrapper.get('[data-test="fs-source"]').setValue("plain");
    await flushPromises();

    const input = wrapper.get('[data-test="ssh-upload-input"]');
    const file = {
      name: "app.conf",
      arrayBuffer: () => Promise.resolve(new Uint8Array([1, 2]).buffer),
    };
    Object.defineProperty(input.element, "files", { value: [file], configurable: true });
    await input.trigger("change");
    await flushPromises();

    expect(SFTPWriteFile).toHaveBeenCalledWith("plain", "/app.conf", [1, 2], 0, true);
    const toast = (context.showToast as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0] as string;
    expect(toast).toMatch(/already exists on the remote host/);
    expect(toast).toMatch(/Rename or delete it first/);
    // The raw protocol token would read as a crash rather than as the
    // deliberate stop it is.
    expect(toast).not.toMatch(/^already_exists/);
  });

  it("uploads to a free path and says so", async () => {
    const SFTPWriteFile = vi.fn().mockResolvedValue({ path: "/notes.md", size: 2, modTime: 1, isBinary: false });
    bind({ SFTPWriteFile });
    const context = localContext();
    const wrapper = mountPanel(context);
    await flushPromises();
    await wrapper.get('[data-test="fs-source"]').setValue("plain");
    await flushPromises();

    const input = wrapper.get('[data-test="ssh-upload-input"]');
    Object.defineProperty(input.element, "files", {
      value: [{ name: "notes.md", arrayBuffer: () => Promise.resolve(new Uint8Array([9]).buffer) }],
      configurable: true,
    });
    await input.trigger("change");
    await flushPromises();

    expect(SFTPWriteFile).toHaveBeenCalledWith("plain", "/notes.md", [9], 0, true);
    const toast = (context.showToast as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0] as string;
    expect(toast).toContain("notes.md");
  });

  it("releases the host when the user switches back to the active pane", async () => {
    const SFTPDisconnect = vi.fn().mockResolvedValue(undefined);
    bind({ SFTPDisconnect });
    const wrapper = mountPanel(localContext());
    await flushPromises();

    await wrapper.get('[data-test="fs-source"]').setValue("plain");
    await flushPromises();
    await wrapper.get('[data-test="fs-source"]').setValue("");
    await flushPromises();

    // Otherwise a browse holds a login (and its keepalive) open on the remote
    // for the rest of the app's life.
    expect(SFTPDisconnect).toHaveBeenCalledWith("plain");
  });
});
