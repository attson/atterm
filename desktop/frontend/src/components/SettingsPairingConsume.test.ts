import { mount, flushPromises } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SettingsPairingConsume, { parseScanned } from "./SettingsPairingConsume.vue";

const consumePairing = vi.fn();
const save = vi.fn();

vi.mock("../platform", () => ({
  usePlatform: () => ({
    relay: { consumePairing, save, load: vi.fn() },
  }),
}));

vi.mock("../i18n/useI18n", () => ({
  useI18n: () => ({ t: (k: string) => k }),
}));

describe("SettingsPairingConsume", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("rejects URL with missing t param", async () => {
    const wrapper = mount(SettingsPairingConsume, {
      props: { scannedUrl: "https://relay.example.com/pair" },
    });
    await flushPromises();
    expect(wrapper.find('[data-testid="pair-error"]').exists()).toBe(true);
    expect(consumePairing).not.toHaveBeenCalled();
  });

  it("happy path: consumes pairing, saves config, emits connected", async () => {
    consumePairing.mockResolvedValue({
      relay_url: "https://relay.example.com",
      session_token: "sess_NEW",
      expires_at: 1735689600,
      user: { id: "u1", email: "alice@example.com" },
      realm_id: "realm-1",
      home_instance_url: "https://home.example.com",
    });
    const wrapper = mount(SettingsPairingConsume, {
      props: { scannedUrl: "https://relay.example.com/pair?t=pair_VALID" },
    });
    await flushPromises();
    expect(consumePairing).toHaveBeenCalledWith("https://relay.example.com", "pair_VALID", undefined);
    expect(save).toHaveBeenCalledWith(expect.objectContaining({
      url: "https://relay.example.com",
      token: "sess_NEW",
      session_expires_at: 1735689600,
      realmId: "realm-1",
      homeInstanceURL: "https://home.example.com",
    }));
    expect(wrapper.emitted("connected")).toBeTruthy();
  });
});

describe("parseScanned", () => {
  it("parses URL with t= and k= into wrapKey Uint8Array(32)", () => {
    const wk = new Uint8Array(32).fill(0x55);
    const kB64Url = btoa(String.fromCharCode(...wk))
      .replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
    const url = `https://relay.example/pair?t=pair_abc&k=${kB64Url}`;
    const got = parseScanned(url, false);
    expect(typeof got).toBe("object");
    expect((got as any).token).toBe("pair_abc");
    expect((got as any).wrapKey).toBeInstanceOf(Uint8Array);
    expect((got as any).wrapKey.length).toBe(32);
  });

  it("rejects k= that is not 32 bytes", () => {
    expect(parseScanned("https://relay.example/pair?t=x&k=YQ", false)).toBe("pair_invalid_url");
  });
});
