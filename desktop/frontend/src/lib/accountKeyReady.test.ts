import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { defineComponent, h } from "vue";
import { mount } from "@vue/test-utils";
import { usePolledAccountKey } from "./accountKeyReady";
import { setAccountKeyProvider } from "./account-key";

// usePolledAccountKey's onBeforeUnmount cleanup only matters if something
// pins it: an empty cleanup body (clearInterval call dropped) still leaves
// every *other* test in this file green, because nothing else observes
// whether the interval is still ticking after the component using it goes
// away. The last three roadmap items each shipped exactly this bug at
// least once (a composable's cleanup silently doing nothing). This uses
// real Vue lifecycle hooks (onMounted/onBeforeUnmount only fire inside an
// actual component instance), so it mounts a minimal host component
// rather than calling the composable bare.
const TestHost = defineComponent({
  setup() {
    const accountKey = usePolledAccountKey(300, 10);
    return () => h("div", accountKey.value ? "unlocked" : "locked");
  },
});

describe("usePolledAccountKey", () => {
  beforeEach(() => {
    setAccountKeyProvider(null);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    setAccountKeyProvider(null);
  });

  it("starts a poll interval when mounted without an account key", () => {
    const w = mount(TestHost);
    expect(vi.getTimerCount()).toBe(1);
    w.unmount();
  });

  it("stops polling once unmounted — the exact bug this test exists to catch", () => {
    const w = mount(TestHost);
    expect(vi.getTimerCount()).toBe(1);
    w.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("does not touch timers at all once the account key is already present at mount", () => {
    setAccountKeyProvider(() => new Uint8Array(32).fill(3));
    const w = mount(TestHost);
    expect(vi.getTimerCount()).toBe(0);
    expect(w.text()).toBe("unlocked");
    w.unmount();
  });

  it("stops polling and resolves once the account key becomes available mid-poll", async () => {
    const w = mount(TestHost);
    expect(vi.getTimerCount()).toBe(1);
    setAccountKeyProvider(() => new Uint8Array(32).fill(5));
    await vi.advanceTimersByTimeAsync(300);
    expect(vi.getTimerCount()).toBe(0);
    expect(w.text()).toBe("unlocked");
    w.unmount();
  });

  it("gives up after maxAttempts and settles on locked rather than polling forever", async () => {
    const w = mount(TestHost);
    await vi.advanceTimersByTimeAsync(300 * 10);
    expect(vi.getTimerCount()).toBe(0);
    expect(w.text()).toBe("locked");
    w.unmount();
  });
});
