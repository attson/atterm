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

  it("uploads into the directory selected in the tree, and says so on the button", async () => {
    // The one write this source offers has to land where the user is looking.
    // Targeting the root unconditionally would mean that for every non-root
    // login the upload goes to a directory they cannot write.
    const SFTPWriteFile = vi.fn().mockResolvedValue({ path: "/srv/app/notes.md", size: 1, modTime: 1, isBinary: false });
    const SFTPListDir = vi.fn().mockImplementation(async (_id: string, path: string) => ({
      path,
      entries: path === "/" ? [{ name: "srv", isDir: true }] : [{ name: "keep", isDir: false }],
      truncated: false,
      total: 1,
    }));
    bind({ SFTPWriteFile, SFTPListDir });
    const context = localContext();
    const wrapper = mountPanel(context);
    await flushPromises();
    await wrapper.get('[data-test="fs-source"]').setValue("plain");
    await flushPromises();

    // Before anything is selected the target is the root, and the button says so.
    expect(wrapper.get('[data-test="ssh-upload"]').attributes("title")).toContain("/");

    await wrapper.get('.node[title="/srv"]').trigger("click");
    await flushPromises();

    // The label names the directory that will actually be written to.
    expect(wrapper.get('[data-test="ssh-upload"]').attributes("title")).toContain("/srv");

    const input = wrapper.get('[data-test="ssh-upload-input"]');
    Object.defineProperty(input.element, "files", {
      value: [{ name: "notes.md", arrayBuffer: () => Promise.resolve(new Uint8Array([7]).buffer) }],
      configurable: true,
    });
    await input.trigger("change");
    await flushPromises();

    expect(SFTPWriteFile).toHaveBeenCalledWith("plain", "/srv/notes.md", [7], 0, true);
    const toast = (context.showToast as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0] as string;
    expect(toast).toContain("/srv");
    // The written directory is re-listed, and /srv is still open: this source
    // has no change notification, so if the panel did not do this the file
    // would simply not appear.
    expect(SFTPListDir.mock.calls.filter(([, p]) => p === "/srv").length).toBeGreaterThan(1);
    expect(wrapper.find('.node[title="/srv/keep"]').exists()).toBe(true);
  });

  it("shows why a host could not be reached instead of an empty tree", async () => {
    // "no saved credential", "host key not in known_hosts", "the host is down",
    // "sftp-server is not installed" are ordinary first-run outcomes here. Each
    // of those sentences is written on the Go side; swallowing the rejection
    // means no user ever meets one.
    const SFTPListDir = vi.fn().mockRejectedValue(
      new Error("no saved credential for build-01; open a terminal on it once to unlock the key"),
    );
    bind({ SFTPListDir });
    const wrapper = mountPanel(localContext());
    await flushPromises();
    await wrapper.get('[data-test="fs-source"]').setValue("plain");
    await flushPromises();

    const banner = wrapper.find('[data-test="tree-error"]');
    expect(banner.exists()).toBe(true);
    expect(banner.text()).toContain("no saved credential");
    // And a way to try again, because "try again to reconnect" is one of the
    // messages the Go side hands up.
    expect(wrapper.find('[data-test="tree-error-retry"]').exists()).toBe(true);

    const calls = SFTPListDir.mock.calls.length;
    await wrapper.get('[data-test="tree-error-retry"]').trigger("click");
    await flushPromises();
    expect(SFTPListDir.mock.calls.length).toBeGreaterThan(calls);
  });

  it("refuses to delete a remote directory rather than recursively wiping it", async () => {
    // Roadmap item 28 is scoped to browse + single-file transfer. There is no
    // trash on the far side, so a recursive delete behind one confirm dialog
    // is the one action here that nothing can walk back.
    const SFTPRemove = vi.fn().mockResolvedValue(undefined);
    const SFTPListDir = vi.fn().mockImplementation(async (_id: string, path: string) => ({
      path,
      entries: path === "/" ? [{ name: "srv", isDir: true }] : [],
      truncated: false,
      total: 1,
    }));
    bind({ SFTPRemove, SFTPListDir });
    const context = localContext();
    const wrapper = mountPanel(context);
    await flushPromises();
    await wrapper.get('[data-test="fs-source"]').setValue("plain");
    await flushPromises();

    await wrapper.get('.node[title="/srv"]').trigger("contextmenu");
    await flushPromises();
    await wrapper.get('[data-test="menu-delete"]').trigger("click");
    await flushPromises();

    // No confirmation is offered at all: agreeing to a delete that is then
    // refused teaches the user that the dialog means nothing.
    expect(wrapper.find('[data-test="btn-trash"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="btn-hard"]').exists()).toBe(false);
    expect(SFTPRemove).not.toHaveBeenCalled();
    const toast = (context.showToast as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0] as string;
    expect(toast).toMatch(/permanently/i);
    expect(toast).toContain("/srv");
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

    // "/" because nothing has been selected in the tree yet — that is the
    // fallback, not the only target the panel can reach (see the test above).
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

    // Again the root, because the tree has no selection.
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
