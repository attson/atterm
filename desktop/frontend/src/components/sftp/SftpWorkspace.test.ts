import { defineComponent, h, nextTick } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import {
  __setBindingsForTest,
  type AppBindings,
  type FileTransferJob,
  type StartFileTransferReq,
} from "../../lib/api";
import { __setPlatformForTests, type Platform } from "../../platform";
import { createFakePlatform, fakeEventBus } from "../../platform/__tests__/_fakePlatform";
import SftpWorkspace from "./SftpWorkspace.vue";
import { createPaneState, type SftpPaneState } from "./sftpWorkspaceModel";

const refreshPane = vi.fn();
const retrySourcePane = vi.fn();

const SftpPaneStub = defineComponent({
  name: "SftpPane",
  props: ["state", "side"],
  emits: ["update:state", "transfer-selection", "host-key-required"],
  setup(props, { expose }) {
    expose({ refresh: refreshPane, retrySource: retrySourcePane });
    return () => h("div", {
      "data-test": `pane-${props.side}`,
      "data-source": props.state.source.kind === "ssh" ? props.state.source.hostId : "local",
    });
  },
});

function bindings(overrides: Partial<AppBindings> = {}): AppBindings {
  return {
    ListSFTPHosts: vi.fn().mockResolvedValue([
      { id: "left-host", alias: "Left", host: "left.example", user: "me", auth_kind: "password" },
      { id: "right-host", alias: "Right", host: "right.example", user: "me", auth_kind: "password" },
    ]),
    GetUserHomeDir: vi.fn().mockResolvedValue("/home/me"),
    ListFileTransfers: vi.fn().mockResolvedValue([]),
    StartFileTransfer: vi.fn().mockResolvedValue("job-1"),
    CancelFileTransfer: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  } as unknown as AppBindings;
}

function makePlatform(): Platform {
  const platform = createFakePlatform();
  platform.events = fakeEventBus();
  return platform;
}

function mountWorkspace(platform = makePlatform()): VueWrapper {
  __setPlatformForTests(platform);
  return mount(SftpWorkspace, {
    global: { stubs: { SftpPane: SftpPaneStub } },
  });
}

function paneState(hostId: string, cwd: string, selected: string[] = []): SftpPaneState {
  return {
    ...createPaneState({ kind: "ssh", hostId }, cwd),
    selectedPaths: new Set(selected),
  };
}

async function setPaneStates(wrapper: VueWrapper) {
  const panes = wrapper.findAllComponents(SftpPaneStub);
  panes[0].vm.$emit("update:state", paneState("left-host", "/from", ["/from/a.txt"]));
  panes[1].vm.$emit("update:state", paneState("right-host", "/to"));
  await nextTick();
}

async function confirmTransfer(wrapper: VueWrapper, conflict: "skip" | "replace" | "rename" = "skip") {
  if (conflict !== "skip") await wrapper.get(`[data-test="conflict-${conflict}"]`).setValue();
  await wrapper.get('[data-test="conflict-confirm"]').trigger("click");
  await flushPromises();
}

describe("SftpWorkspace", () => {
  beforeEach(() => {
    refreshPane.mockReset();
    retrySourcePane.mockReset();
    __setBindingsForTest(bindings());
  });

  afterEach(() => {
    __setBindingsForTest(undefined);
    __setPlatformForTests(null);
    vi.restoreAllMocks();
  });

  it("keeps both remote sources independent and starts the selected direction with skip as the default", async () => {
    const StartFileTransfer = vi.fn().mockResolvedValue("job-1");
    __setBindingsForTest(bindings({ StartFileTransfer }));
    const wrapper = mountWorkspace();
    await flushPromises();
    await setPaneStates(wrapper);

    expect(wrapper.get('[data-test="pane-left"]').attributes("data-source")).toBe("left-host");
    expect(wrapper.get('[data-test="pane-right"]').attributes("data-source")).toBe("right-host");
    await wrapper.get('[data-test="transfer-right"]').trigger("click");

    expect((wrapper.get('[data-test="conflict-skip"]').element as HTMLInputElement).checked).toBe(true);
    await confirmTransfer(wrapper);
    expect(StartFileTransfer).toHaveBeenCalledWith({
      source: { kind: "ssh", host_id: "left-host" },
      destination: { kind: "ssh", host_id: "right-host" },
      paths: ["/from/a.txt"],
      destination_dir: "/to",
      conflict: "skip",
      accepted_host_key: { host: "", fingerprint: "" },
    });
  });

  it.each(["replace", "rename"] as const)("uses the explicit %s conflict choice", async (conflict) => {
    const StartFileTransfer = vi.fn().mockResolvedValue("job-1");
    __setBindingsForTest(bindings({ StartFileTransfer }));
    const wrapper = mountWorkspace();
    await flushPromises();
    await setPaneStates(wrapper);

    await wrapper.get('[data-test="transfer-right"]').trigger("click");
    await confirmTransfer(wrapper, conflict);
    expect((StartFileTransfer.mock.calls[0][0] as StartFileTransferReq).conflict).toBe(conflict);
  });

  it("subscribes before listing, keeps a newer event snapshot, cancels jobs, and refreshes the completed destination", async () => {
    const order: string[] = [];
    const events = fakeEventBus();
    const on = events.on.bind(events);
    events.on = vi.fn((name, handler) => {
      order.push(`on:${name}`);
      return on(name, handler);
    });
    let resolveList!: (jobs: FileTransferJob[]) => void;
    const ListFileTransfers = vi.fn(() => {
      order.push("list");
      return new Promise<FileTransferJob[]>((resolve) => { resolveList = resolve; });
    });
    const CancelFileTransfer = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest(bindings({ ListFileTransfers, CancelFileTransfer }));
    const platform = { ...makePlatform(), events };
    const wrapper = mountWorkspace(platform);

    expect(order).toEqual(["on:sftp:transfer-progress", "list"]);
    platform.events.emit("sftp:transfer-progress", {
      id: "job-1", state: "running", current_path: "a.txt", bytes_done: 8,
      bytes_total: 10, entries_done: 0, entries_total: 1, warnings: [],
    } satisfies FileTransferJob);
    resolveList([{
      id: "job-1", state: "queued", current_path: "", bytes_done: 0,
      bytes_total: 0, entries_done: 0, entries_total: 0, warnings: [],
    }]);
    await flushPromises();
    expect(wrapper.get('[data-test="transfer-job-job-1"]').text()).toContain("80%");

    await wrapper.get('[data-test="cancel-job-job-1"]').trigger("click");
    expect(CancelFileTransfer).toHaveBeenCalledWith("job-1");

    await setPaneStates(wrapper);
    await wrapper.get('[data-test="transfer-right"]').trigger("click");
    await confirmTransfer(wrapper);
    platform.events.emit("sftp:transfer-progress", {
      id: "job-1", state: "completed", current_path: "a.txt", bytes_done: 10,
      bytes_total: 10, entries_done: 1, entries_total: 1, warnings: [],
    } satisfies FileTransferJob);
    await nextTick();
    expect(refreshPane).toHaveBeenCalledTimes(1);
  });

  it("retries sequential TOFU prompts with only the exact accepted host/fingerprint pair", async () => {
    const first = { Host: "jump:22", Fingerprint: "SHA256:jump", HopIndex: 1, HopName: "jump" };
    const second = { Host: "target:22", Fingerprint: "SHA256:target", HopIndex: 2, HopName: "Right" };
    const StartFileTransfer = vi.fn()
      .mockRejectedValueOnce(first)
      .mockRejectedValueOnce(second)
      .mockResolvedValueOnce("job-1");
    __setBindingsForTest(bindings({ StartFileTransfer }));
    const wrapper = mountWorkspace();
    await flushPromises();
    await setPaneStates(wrapper);
    await wrapper.get('[data-test="transfer-right"]').trigger("click");
    await confirmTransfer(wrapper);

    expect(wrapper.get('[data-test="sftp-host-tofu"]').text()).toContain("SHA256:jump");
    await wrapper.get('[data-test="sftp-accept-hostkey"]').trigger("click");
    await flushPromises();
    expect(StartFileTransfer.mock.calls[1][0].accepted_host_key).toEqual({ host: "jump:22", fingerprint: "SHA256:jump" });
    expect(wrapper.get('[data-test="sftp-host-tofu"]').text()).toContain("SHA256:target");

    await wrapper.get('[data-test="sftp-accept-hostkey"]').trigger("click");
    await flushPromises();
    expect(StartFileTransfer.mock.calls[2][0].accepted_host_key).toEqual({ host: "target:22", fingerprint: "SHA256:target" });
  });

  it("retries a pane source connection with the exact accepted host key pair", async () => {
    const wrapper = mountWorkspace();
    await flushPromises();
    wrapper.findAllComponents(SftpPaneStub)[1].vm.$emit("host-key-required", {
      hostId: "right-host",
      prompt: { host: "jump:22", fingerprint: "SHA256:jump", hopIndex: 1, hopName: "jump" },
    });
    await nextTick();

    await wrapper.get('[data-test="sftp-accept-hostkey"]').trigger("click");
    expect(retrySourcePane).toHaveBeenCalledWith({ host: "jump:22", fingerprint: "SHA256:jump" });
  });

  it("uses internal and native drops as local-to-pane transfers", async () => {
    let nativeDrop!: (paths: string[], point?: { x: number; y: number }) => void;
    const platform = makePlatform();
    platform.nativeFileDrop = {
      onFileDrop: (handler) => { nativeDrop = handler; return () => {}; },
      readSSHKeyFile: vi.fn(),
    };
    const StartFileTransfer = vi.fn().mockResolvedValue("job-1");
    __setBindingsForTest(bindings({ StartFileTransfer }));
    const wrapper = mountWorkspace(platform);
    await flushPromises();
    await setPaneStates(wrapper);

    const payload = JSON.stringify({ endpoint: { kind: "ssh", host_id: "left-host" }, paths: ["/from/a.txt"] });
    await wrapper.get('[data-test="drop-right"]').trigger("drop", {
      dataTransfer: { getData: (kind: string) => kind === "application/x-atterm-sftp-paths" ? payload : "" },
    });
    await confirmTransfer(wrapper);
    expect(StartFileTransfer.mock.calls[0][0]).toMatchObject({
      source: { kind: "ssh", host_id: "left-host" }, destination: { kind: "ssh", host_id: "right-host" },
      paths: ["/from/a.txt"], destination_dir: "/to",
    });

    Object.defineProperty(document, "elementFromPoint", {
      configurable: true,
      value: vi.fn().mockReturnValue(wrapper.get('[data-test="drop-right"]').element),
    });
    nativeDrop(["/tmp/report.zip"], { x: 20, y: 30 });
    await nextTick();
    await confirmTransfer(wrapper);
    expect(StartFileTransfer.mock.calls[1][0]).toMatchObject({
      source: { kind: "local" }, destination: { kind: "ssh", host_id: "right-host" },
      paths: ["/tmp/report.zip"], destination_dir: "/to",
    });
  });
});
