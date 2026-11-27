import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { __setBindingsForTest, type AppBindings, type SSHHost } from "../../lib/api";
import type { FileSystemBridge } from "../../plugins/fileExplorer/fsBridge";
import SftpPane from "./SftpPane.vue";
import { createPaneState } from "./sftpWorkspaceModel";

function fakeFS(overrides: Partial<FileSystemBridge> = {}): FileSystemBridge {
  return {
    identity: "local",
    listDir: vi.fn().mockResolvedValue([]),
    watchDir: vi.fn().mockResolvedValue(1),
    unwatchDir: vi.fn().mockResolvedValue(undefined),
    readFile: vi.fn().mockResolvedValue({ path: "/file", data: [], isBinary: false }),
    fileMeta: vi.fn().mockResolvedValue({ path: "/file", size: 1, modTime: 1, isBinary: false }),
    openExternal: vi.fn().mockResolvedValue(undefined),
    assetUrlFor: vi.fn().mockResolvedValue(""),
    onDirChanged: vi.fn(() => () => {}),
    writeFile: vi.fn().mockResolvedValue({ path: "/file", size: 1, modTime: 1, isBinary: false }),
    createFile: vi.fn().mockResolvedValue({ path: "/file", size: 0, modTime: 1, isBinary: false }),
    rename: vi.fn().mockResolvedValue({ path: "/file2", size: 1, modTime: 1, isBinary: false }),
    remove: vi.fn().mockResolvedValue(undefined),
    mkdir: vi.fn().mockResolvedValue({ path: "/dir", size: 0, modTime: 1, isBinary: false }),
    trash: vi.fn().mockResolvedValue(undefined),
    dispose: vi.fn(),
    ...overrides,
  };
}

const hosts: SSHHost[] = [
  { id: "build", alias: "Build server", host: "build.example", user: "deploy", auth_kind: "password" },
];

function setSFTPBindings(overrides: Partial<AppBindings> = {}) {
  __setBindingsForTest({
    OpenSFTPSource: vi.fn().mockResolvedValue({ host_id: "build", initial_dir: "/srv" }),
    SFTPListDir: vi.fn().mockResolvedValue({ path: "/srv", entries: [], truncated: false, total: 0 }),
    SFTPFileMeta: vi.fn().mockResolvedValue({ path: "/srv/f", size: 1, modTime: 1, isBinary: false }),
    SFTPReadFile: vi.fn().mockResolvedValue({ path: "/srv/f", data: [], isBinary: false }),
    SFTPWriteFile: vi.fn(),
    SFTPCreateFile: vi.fn(),
    SFTPMkdir: vi.fn(),
    SFTPRename: vi.fn(),
    SFTPRemove: vi.fn(),
    SFTPDisconnect: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  } as unknown as AppBindings);
}

function mountPane(
  localFS: FileSystemBridge,
  state = createPaneState({ kind: "local" }, "/home/me"),
  localHome = "/home/me",
) {
  return mount(SftpPane, {
    props: {
      state,
      hosts,
      localFS,
      localHome,
    },
    global: {
      stubs: {
        FileEditor: {
          props: ["fs", "path"],
          template: '<div data-test="file-editor-stub">{{ path }}</div>',
        },
      },
    },
  });
}

describe("SftpPane", () => {
  beforeEach(() => setSFTPBindings());
  afterEach(() => __setBindingsForTest(undefined));

  it("offers local and saved hosts, preflights SSH, then lists its returned login directory", async () => {
    const OpenSFTPSource = vi.fn().mockResolvedValue({ host_id: "build", initial_dir: "/srv/work" });
    const SFTPListDir = vi.fn().mockResolvedValue({ path: "/srv/work", entries: [], truncated: false, total: 0 });
    setSFTPBindings({ OpenSFTPSource, SFTPListDir });
    const wrapper = mountPane(fakeFS());
    await flushPromises();

    await wrapper.get('[data-test="source-picker"] [data-testid="select-trigger"]').trigger("click");
    const labels = wrapper.findAll('[data-testid="select-option"]').map((option) => option.text());
    expect(labels[0]).toBe("Local machine");
    expect(labels[1]).toContain("Build server");
    expect(labels[1]).toContain("deploy@build.example");
    await wrapper.findAll('[data-testid="select-option"]')[1].trigger("click");
    await flushPromises();

    expect(OpenSFTPSource).toHaveBeenCalledWith("build", { host: "", fingerprint: "" });
    expect(SFTPListDir).toHaveBeenCalledWith("build", "/srv/work");
    expect(wrapper.get('[data-test="path-input"]').attributes("value")).toBe("/srv/work");
    wrapper.unmount();
  });

  it("navigates, filters, sorts, and keeps multi-selection stable", async () => {
    const listDir = vi.fn(async (path: string) => path === "/home/me"
      ? [
          { name: "z.txt", isDir: false, size: 2, modTime: 2 },
          { name: "Projects", isDir: true, size: 0, modTime: 1 },
          { name: "a.txt", isDir: false, size: 1, modTime: 3 },
        ]
      : [{ name: "inside.txt", isDir: false, size: 1, modTime: 1 }]);
    const wrapper = mountPane(fakeFS({ listDir }));
    await flushPromises();

    expect(wrapper.findAll('[data-test="file-row"]').map((row) => row.text())).toEqual([
      expect.stringContaining("Projects"),
      expect.stringContaining("a.txt"),
      expect.stringContaining("z.txt"),
    ]);
    await wrapper.get('[data-path="/home/me/Projects"]').trigger("dblclick");
    await flushPromises();
    expect(wrapper.get('[data-test="path-input"]').attributes("value")).toBe("/home/me/Projects");
    await wrapper.get('[data-test="nav-back"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-test="path-input"]').attributes("value")).toBe("/home/me");
    await wrapper.get('[data-test="nav-forward"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-test="nav-up"]').trigger("click");
    await flushPromises();

    await wrapper.get('[data-test="row-select-/home/me/z.txt"]').setValue(true);
    await wrapper.get('[data-test="row-select-/home/me/a.txt"]').setValue(true);
    await wrapper.get('[data-test="sort-size"]').trigger("click");
    expect(wrapper.get('[data-test="row-select-/home/me/z.txt"]').attributes("checked")).toBeDefined();
    expect(wrapper.get('[data-test="row-select-/home/me/a.txt"]').attributes("checked")).toBeDefined();

    await wrapper.get('[data-test="query-input"]').setValue("Z.TXT");
    expect(wrapper.findAll('[data-test="file-row"]')).toHaveLength(1);
    expect(wrapper.get('[data-test="file-row"]').text()).toContain("z.txt");
  });

  it("reloads a parent-provided local cwd and ignores the stale initial request", async () => {
    let rejectRoot!: (error: Error) => void;
    const rootRequest = new Promise<never>((_resolve, reject) => { rejectRoot = reject; });
    const listDirDetailed = vi.fn((path: string) => path === "/"
      ? rootRequest
      : Promise.resolve({
          entries: [{ name: "Projects", isDir: true, size: 0, modTime: 1 }],
          truncated: false,
          total: 1,
        }));
    const wrapper = mountPane(
      fakeFS({ listDirDetailed }),
      createPaneState({ kind: "local" }, "/"),
      "/home/me",
    );

    await wrapper.setProps({ state: createPaneState({ kind: "local" }, "/home/me") });
    await flushPromises();
    rejectRoot(new Error("plugin_fs: path forbidden: /"));
    await flushPromises();

    expect(listDirDetailed).toHaveBeenNthCalledWith(1, "/");
    expect(listDirDetailed).toHaveBeenNthCalledWith(2, "/home/me");
    expect(wrapper.find('[data-test="load-error"]').exists()).toBe(false);
    expect(wrapper.get('[data-test="file-row"]').text()).toContain("Projects");
  });

  it("shows truncated and retryable failure states without changing the source or path", async () => {
    let attempt = 0;
    const localFS = fakeFS({
      listDirDetailed: vi.fn(async () => {
        attempt++;
        if (attempt === 1) throw new Error("offline");
        return { entries: [{ name: "one", isDir: false }], truncated: true, total: 3000 };
      }),
    });
    const wrapper = mountPane(localFS);
    await flushPromises();

    expect(wrapper.get('[data-test="load-error"]').text()).toContain("offline");
    expect(wrapper.get('[data-test="path-input"]').attributes("value")).toBe("/home/me");
    await wrapper.get('[data-test="retry"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-test="truncated"]').text()).toContain("1 of 3000");
    expect(wrapper.get('[data-test="file-row"]').text()).toContain("one");
  });

  it("creates, renames, deletes, and opens files through the active bridge", async () => {
    const mkdir = vi.fn().mockResolvedValue({ path: "/home/me/new-dir", size: 0, modTime: 1, isBinary: false });
    const rename = vi.fn().mockResolvedValue({ path: "/home/me/renamed.txt", size: 1, modTime: 1, isBinary: false });
    const trash = vi.fn().mockResolvedValue(undefined);
    const localFS = fakeFS({
      listDir: vi.fn().mockResolvedValue([{ name: "note.txt", isDir: false, size: 1, modTime: 1 }]),
      mkdir,
      rename,
      trash,
    });
    const wrapper = mountPane(localFS);
    await flushPromises();

    await wrapper.get('[data-test="create-dir"]').trigger("click");
    await wrapper.get('[data-test="action-input"]').setValue("new-dir");
    await wrapper.get('[data-test="action-confirm"]').trigger("click");
    await flushPromises();
    expect(mkdir).toHaveBeenCalledWith("/home/me/new-dir");

    await wrapper.get('[data-test="row-select-/home/me/note.txt"]').setValue(true);
    await wrapper.get('[data-test="rename"]').trigger("click");
    await wrapper.get('[data-test="action-input"]').setValue("renamed.txt");
    await wrapper.get('[data-test="action-confirm"]').trigger("click");
    await flushPromises();
    expect(rename).toHaveBeenCalledWith("/home/me/note.txt", "/home/me/renamed.txt");

    await wrapper.get('[data-test="row-select-/home/me/note.txt"]').setValue(true);
    await wrapper.get('[data-test="delete"]').trigger("click");
    await wrapper.get('[data-test="action-confirm"]').trigger("click");
    await flushPromises();
    expect(trash).toHaveBeenCalledWith("/home/me/note.txt");

    await wrapper.get('[data-path="/home/me/note.txt"]').trigger("dblclick");
    await flushPromises();
    expect(wrapper.get('[data-test="file-editor-stub"]').text()).toBe("/home/me/note.txt");
  });

  it("requires a second explicit confirmation before recursively deleting a remote directory", async () => {
    const SFTPRemove = vi.fn().mockResolvedValue(undefined);
    setSFTPBindings({
      SFTPListDir: vi.fn().mockResolvedValue({
        path: "/srv",
        entries: [{ name: "release", isDir: true, size: 0, modTime: 1 }],
        truncated: false,
        total: 1,
      }),
      SFTPRemove,
    });
    const wrapper = mountPane(fakeFS());
    await flushPromises();

    await wrapper.get('[data-test="source-picker"] [data-testid="select-trigger"]').trigger("click");
    await wrapper.findAll('[data-testid="select-option"]')[1].trigger("click");
    await flushPromises();
    await wrapper.get('[data-test="row-select-/srv/release"]').setValue(true);
    await wrapper.get('[data-test="delete"]').trigger("click");
    await wrapper.get('[data-test="action-confirm"]').trigger("click");
    await flushPromises();

    expect(SFTPRemove).not.toHaveBeenCalled();
    expect(wrapper.get('[data-test="remote-delete-confirm"]').text()).toContain("release");

    await wrapper.get('[data-test="remote-delete-confirm-button"]').trigger("click");
    await flushPromises();
    expect(SFTPRemove).toHaveBeenCalledWith("build", "/srv/release", true);
    wrapper.unmount();
  });

  it("offers context-menu transfer and writes endpoint-only internal drag data", async () => {
    const wrapper = mountPane(fakeFS({
      listDir: vi.fn().mockResolvedValue([{ name: "note.txt", isDir: false, size: 1, modTime: 1 }]),
    }));
    await flushPromises();

    await wrapper.get('[data-path="/home/me/note.txt"]').trigger("contextmenu", { clientX: 12, clientY: 18 });
    await wrapper.get('[data-test="context-transfer"]').trigger("click");
    expect(wrapper.emitted("transfer-selection")).toHaveLength(1);

    const setData = vi.fn();
    await wrapper.get('[data-path="/home/me/note.txt"]').trigger("dragstart", {
      dataTransfer: { setData, effectAllowed: "" },
    });
    expect(setData).toHaveBeenCalledWith("application/x-atterm-sftp-paths", JSON.stringify({
      endpoint: { kind: "local" },
      paths: ["/home/me/note.txt"],
    }));
  });
});
