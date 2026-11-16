import { mount, type VueWrapper } from "@vue/test-utils";
import { defineComponent, h, nextTick, ref, watch } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SessionConnection } from "../lib/connection";
import { TYPE, decodeFrame, decodeText, encodeFrame, encodeText, uuidParse } from "../lib/proto";
import { useTerminalSubscriptionActive } from "./useTerminalSubscriptionActive";

const sid = "11111111-2222-3333-4444-555555555555";

class Socket {
  static instances: Socket[] = [];
  static OPEN = 1;
  readyState = 0;
  binaryType = "";
  sent: Uint8Array[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  constructor() { Socket.instances.push(this); }
  send(bytes: Uint8Array) { this.sent.push(bytes); }
  open() { this.readyState = 1; this.onopen?.(); }
  close() { this.readyState = 3; this.onclose?.(); }
  output(seq: number, text: string) {
    const data = encodeText(text);
    const payload = new Uint8Array(8 + data.length);
    new DataView(payload.buffer).setBigUint64(0, BigInt(seq));
    payload.set(data, 8);
    this.onmessage?.({ data: encodeFrame(TYPE.OUT, uuidParse(sid), payload).buffer } as MessageEvent);
  }
}

let hidden = false;
const wrappers: VueWrapper[] = [];

function terminal(local = false, active = true, pauseInBackground = true) {
  const tabActive = ref(active);
  const pauseEnabled = ref(pauseInBackground);
  const output: string[] = [];
  const connection = new SessionConnection({ url: "wss://relay.example", session_token: "token" }, sid, {
    onOutput: (bytes) => output.push(decodeText(bytes)),
  }, { remote: !local });
  let subscriptionActive!: ReturnType<typeof useTerminalSubscriptionActive>;
  const wrapper = mount(defineComponent({
    setup() {
      subscriptionActive = useTerminalSubscriptionActive(() => tabActive.value, () => local, () => pauseEnabled.value);
      watch(subscriptionActive, (value) => value ? connection.attach() : connection.suspend(), { immediate: true });
      return () => h("div");
    },
  }));
  wrappers.push(wrapper);
  return { wrapper, tabActive, pauseEnabled, connection, output, get active() { return subscriptionActive.value; } };
}

async function pageHidden(value: boolean) {
  hidden = value;
  document.dispatchEvent(new Event("visibilitychange"));
  await nextTick();
}

beforeEach(() => {
  hidden = false;
  Socket.instances = [];
  vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);
  vi.stubGlobal("WebSocket", Socket);
});
afterEach(() => {
  for (const wrapper of wrappers.splice(0)) wrapper.unmount();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("remote terminal subscription visibility", () => {
  it("defaults to retaining a remote background subscription when no preference is supplied", () => {
    hidden = true;
    const wrapper = mount(defineComponent({
      setup() {
        const active = useTerminalSubscriptionActive(() => false, () => false);
        return () => h("div", active.value ? "connected" : "paused");
      },
    }));
    wrappers.push(wrapper);
    expect(wrapper.text()).toBe("connected");
  });

  it("keeps remote connections alive in inactive tabs and hidden pages when the switch is off", async () => {
    const view = terminal(false, false, false);
    const socket = Socket.instances[0];
    expect(socket).toBeDefined();
    socket.open();
    await pageHidden(true);
    expect(view.active).toBe(true);
    expect(socket.readyState).toBe(1);
    view.connection.detach();
  });

  it("applies switch changes immediately without opening a replacement while a remote tab stays hidden", async () => {
    const view = terminal(false, false, false);
    const first = Socket.instances[0];
    first.open();
    view.pauseEnabled.value = true;
    await nextTick();
    expect(first.readyState).toBe(3);
    await pageHidden(true);
    view.pauseEnabled.value = false;
    await nextTick();
    expect(Socket.instances).toHaveLength(2);
    expect(view.active).toBe(true);
    view.connection.detach();
  });

  it("closes hidden remote output and resumes from its last accepted output cursor", async () => {
    const view = terminal();
    const first = Socket.instances[0];
    first.open();
    first.output(7, "before");
    await pageHidden(true);
    expect(first.readyState).toBe(3);
    expect(view.active).toBe(false);
    first.output(8, "stale");
    await pageHidden(false);
    const resumed = Socket.instances[1];
    resumed.open();
    expect(JSON.parse(decodeText(decodeFrame(resumed.sent[0]).payload)).since_seq).toBe(7);
    resumed.output(8, "after");
    expect(view.output).toEqual(["before", "after"]);
    view.connection.detach();
  });

  it("does not open a remote subscription while initially hidden or restore an inactive tab", async () => {
    hidden = true;
    const view = terminal();
    expect(Socket.instances).toHaveLength(0);
    view.tabActive.value = false;
    await pageHidden(false);
    expect(Socket.instances).toHaveLength(0);
    view.tabActive.value = true;
    await nextTick();
    expect(Socket.instances).toHaveLength(1);
    view.connection.detach();
  });

  it("keeps local terminals subscribed on page hiding and still pauses inactive terminal tabs", async () => {
    const view = terminal(true);
    const socket = Socket.instances[0];
    socket.open();
    await pageHidden(true);
    expect(socket.readyState).toBe(1);
    view.tabActive.value = false;
    await nextTick();
    expect(socket.readyState).toBe(3);
    view.connection.detach();
  });

  it("pauses every visible split pane and removes page listeners on unmount", async () => {
    const left = terminal();
    const right = terminal();
    await pageHidden(true);
    expect(left.active).toBe(false);
    expect(right.active).toBe(false);
    expect(Socket.instances.map((socket) => socket.readyState)).toEqual([3, 3]);
    left.wrapper.unmount();
    await pageHidden(false);
    expect(left.active).toBe(false);
    expect(right.active).toBe(true);
    expect(Socket.instances).toHaveLength(3);
    left.connection.detach();
    right.connection.detach();
  });
});
