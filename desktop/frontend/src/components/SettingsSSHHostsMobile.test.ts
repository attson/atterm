import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsSSHHostsMobile from "./SettingsSSHHostsMobile.vue";
import source from "./SettingsSSHHostsMobile.vue?raw";
import { resetI18nForTest } from "../i18n";
import { en } from "../i18n/messages/en";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";
import { setAccountKeyProvider } from "../lib/account-key";
import type { SSHHostView, SSHKeyView } from "../lib/syncedBlobs";

// See SettingsProfilesMobile.test.ts for why the crypto reader is mocked
// here rather than exercised through a real sealed blob: that contract is
// task 1's (syncedBlobs.test.ts, pinned against a Go-generated vector).
const openSSHHostsBlob = vi.fn();
vi.mock("../lib/syncedBlobs", async () => {
  const actual = await vi.importActual<typeof import("../lib/syncedBlobs")>("../lib/syncedBlobs");
  return {
    ...actual,
    openSSHHostsBlob: (...a: unknown[]) => openSSHHostsBlob(...a),
  };
});

const ACCOUNT_KEY = new Uint8Array(32).fill(9);
const RAW_VALUE = "ZmFrZS1zZWFsZWQtaG9zdHM="; // arbitrary — openSSHHostsBlob is mocked

function setSyncedValue(raw: string | undefined): void {
  if (raw === undefined) {
    localStorage.removeItem("atterm.ssh_hosts_encrypted.value");
  } else {
    localStorage.setItem("atterm.ssh_hosts_encrypted.value", JSON.stringify(raw));
  }
}

let mounted: ReturnType<typeof mount>[] = [];

function mountPanel(capsOverrides: Record<string, unknown> = { capacitor: true }) {
  const base = createFakePlatform();
  __setPlatformForTests({ ...base, caps: { ...base.caps, wailsBindings: false, capacitor: false, ...capsOverrides } });
  const w = mount(SettingsSSHHostsMobile);
  mounted.push(w);
  return w;
}

function sampleHosts(): { hosts: SSHHostView[]; keys: SSHKeyView[] } {
  return {
    keys: [{ id: "k1", name: "deploy-key", keyType: "ED25519" }],
    hosts: [
      {
        id: "h1",
        alias: "box1",
        host: "box1.example.com",
        port: "22",
        user: "root",
        authKind: "password",
        keyId: "",
        tags: ["prod"],
        note: "primary",
        hasJumpChain: false,
        isProxyCommandHost: false,
      },
      {
        id: "h2",
        alias: "box2",
        host: "box2.internal",
        port: "22",
        user: "deploy",
        authKind: "key",
        keyId: "k1",
        tags: [],
        note: "",
        hasJumpChain: true,
        isProxyCommandHost: false,
      },
      {
        id: "h3",
        alias: "box3",
        host: "box3.internal",
        port: "22",
        user: "deploy",
        authKind: "password",
        keyId: "",
        tags: [],
        note: "",
        hasJumpChain: false,
        isProxyCommandHost: true,
      },
      // Both flags set: no host in the sealed payload can reach this shape
      // from a plain SSH config alone (a ProxyCommand host is a leaf, not
      // itself dialed through a jump chain) -- but the two are independent
      // booleans on the wire, so a corrupted or future payload could set
      // both. ProxyCommand is the one that matters: "not dialable" must win
      // over "reaches via a jump chain", never the reverse.
      {
        id: "h4",
        alias: "box4",
        host: "box4.internal",
        port: "22",
        user: "deploy",
        authKind: "password",
        keyId: "",
        tags: [],
        note: "",
        hasJumpChain: true,
        isProxyCommandHost: true,
      },
    ],
  };
}

describe("SettingsSSHHostsMobile", () => {
  beforeEach(() => {
    resetI18nForTest();
    localStorage.clear();
    setAccountKeyProvider(null);
    openSSHHostsBlob.mockReset();
  });

  afterEach(() => {
    for (const w of mounted) w.unmount();
    mounted = [];
    setAccountKeyProvider(null);
  });

  // ---- platform gate: same shell mounts on capacitor, wails desktop, and a
  // plain browser (web) tab — this view must render only under capacitor. ----

  it("renders nothing under a wails-desktop platform shape", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    const w = mountPanel({ wailsBindings: true, capacitor: false });
    await flushPromises();
    expect(w.find('[data-testid="mobile-hosts-panel"]').exists()).toBe(false);
    expect(w.text()).toBe("");
    expect(openSSHHostsBlob).not.toHaveBeenCalled();
  });

  it("renders nothing under a plain web (browser tab) platform shape", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    const w = mountPanel({ wailsBindings: false, capacitor: false });
    await flushPromises();
    expect(w.find('[data-testid="mobile-hosts-panel"]').exists()).toBe(false);
    expect(openSSHHostsBlob).not.toHaveBeenCalled();
  });

  it("mounts and renders under the capacitor platform shape", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockReturnValue(sampleHosts());
    const w = mountPanel({ capacitor: true });
    await flushPromises();
    expect(w.find('[data-testid="mobile-hosts-panel"]').exists()).toBe(true);
  });

  // ---- account key / sync state ----

  it('shows the "locked" state and never calls the reader when there is no account key', async () => {
    setSyncedValue(RAW_VALUE);
    const w = mountPanel();
    await flushPromises();
    expect(w.find('[data-testid="mobile-hosts-locked"]').text()).toBe(en.settings.mobileSync.locked);
    expect(openSSHHostsBlob).not.toHaveBeenCalled();
  });

  it('shows the "no data" state when nothing has synced yet', async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    const w = mountPanel();
    await flushPromises();
    expect(w.find('[data-testid="mobile-hosts-no-data"]').text()).toBe(en.settings.mobileSync.noData);
    expect(openSSHHostsBlob).not.toHaveBeenCalled();
  });

  it("shows an error message when the reader throws", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockImplementation(() => {
      throw new Error("openSSHHostsBlob: could not decrypt (wrong account key or corrupted blob)");
    });
    const w = mountPanel();
    await flushPromises();
    expect(w.find('[data-testid="mobile-hosts-error"]').text()).toContain("could not decrypt");
  });

  it('shows the "empty" state when the blob opens but has zero hosts', async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockReturnValue({ hosts: [], keys: [] });
    const w = mountPanel();
    await flushPromises();
    expect(w.find('[data-testid="mobile-hosts-empty"]').text()).toBe(en.settings.mobileHosts.empty);
  });

  it("calls the reader with exactly the account key and the ssh_hosts_encrypted value", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockReturnValue(sampleHosts());
    mountPanel();
    await flushPromises();
    expect(openSSHHostsBlob).toHaveBeenCalledWith(ACCOUNT_KEY, RAW_VALUE);
  });

  // ---- host rendering: alias/host/user/port/tags/note ----

  it("renders alias, host, user, port, tags, and note", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockReturnValue(sampleHosts());
    const w = mountPanel();
    await flushPromises();

    const items = w.findAll('[data-testid="mobile-host-item"]');
    const h1 = items.find((i) => i.text().includes("box1"))!;
    expect(h1.text()).toContain("box1.example.com");
    expect(h1.find('[data-testid="mobile-host-user"]').text()).toBe("root");
    expect(h1.find('[data-testid="mobile-host-port"]').text()).toBe("22");
    expect(h1.find('[data-testid="mobile-host-tags"]').text()).toContain("prod");
    expect(h1.find('[data-testid="mobile-host-note"]').text()).toBe("primary");
  });

  // ---- jump-chain vs ProxyCommand: distinct, and mutually exclusive in the
  // fixture data used here (ProxyCommand always wins when both are set) ----

  it("marks a jump-chain host distinctly from a plain host", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockReturnValue(sampleHosts());
    const w = mountPanel();
    await flushPromises();

    const items = w.findAll('[data-testid="mobile-host-item"]');
    const h1 = items.find((i) => i.text().includes("box1"))!;
    const h2 = items.find((i) => i.text().includes("box2"))!;
    expect(h1.find('[data-testid="mobile-host-jump-chain-badge"]').exists()).toBe(false);
    expect(h2.find('[data-testid="mobile-host-jump-chain-badge"]').exists()).toBe(true);
    expect(h2.find('[data-testid="mobile-host-jump-chain-badge"]').text()).toBe(en.settings.mobileHosts.jumpChain);
  });

  it("marks a ProxyCommand host distinctly, and never as a jump chain", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockReturnValue(sampleHosts());
    const w = mountPanel();
    await flushPromises();

    const items = w.findAll('[data-testid="mobile-host-item"]');
    const h3 = items.find((i) => i.text().includes("box3"))!;
    expect(h3.find('[data-testid="mobile-host-proxy-command-badge"]').exists()).toBe(true);
    expect(h3.find('[data-testid="mobile-host-proxy-command-badge"]').text()).toBe(en.settings.mobileHosts.proxyCommand);
    expect(h3.find('[data-testid="mobile-host-jump-chain-badge"]').exists()).toBe(false);
  });

  it('when a host has both flags set, shows "not dialable" (ProxyCommand) and never "reaches via a jump chain"', async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockReturnValue(sampleHosts());
    const w = mountPanel();
    await flushPromises();

    const items = w.findAll('[data-testid="mobile-host-item"]');
    const h4 = items.find((i) => i.text().includes("box4"))!;
    expect(h4.find('[data-testid="mobile-host-proxy-command-badge"]').exists()).toBe(true);
    expect(h4.find('[data-testid="mobile-host-jump-chain-badge"]').exists()).toBe(false);
  });

  it("resolves a host's keyId to the synced key's name", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockReturnValue(sampleHosts());
    const w = mountPanel();
    await flushPromises();

    const items = w.findAll('[data-testid="mobile-host-item"]');
    const h2 = items.find((i) => i.text().includes("box2"))!;
    expect(h2.find('[data-testid="mobile-host-key"]').text()).toBe("deploy-key");
  });

  // ---- read-only: no editing affordance, no credential surface ----

  it("renders no buttons or inputs anywhere in the panel", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockReturnValue(sampleHosts());
    const w = mountPanel();
    await flushPromises();

    expect(w.findAll("button")).toHaveLength(0);
    expect(w.findAll("input")).toHaveLength(0);
    expect(w.findAll("textarea")).toHaveLength(0);
  });

  it("never renders a credential/secret field name or value", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    openSSHHostsBlob.mockReturnValue(sampleHosts());
    const w = mountPanel();
    await flushPromises();

    const html = w.html().toLowerCase();
    expect(html).not.toContain("credential");
    expect(html).not.toContain("passphrase");
    expect(html).not.toContain("private_key");
    expect(html).not.toContain("privatekey");
  });

  // The field-name grep above only proves this fixture's known fields never
  // render — it says nothing about a future bypass. Two concrete bypass
  // shapes, both of which passed the whole suite before this pair existed:

  // (a) an indiscriminate per-item dump (e.g. adding
  // `<span>{{ JSON.stringify(h) }}</span>` per host) renders whatever
  // extra properties the reader handed back, including any a future
  // refactor accidentally leaves on SSHHostView/SSHKeyView. Plant CANARY
  // values as those extra fields — mirroring how task 1's
  // syncedBlobs.test.ts pins the crypto layer with the same trick — and
  // assert the rendered HTML never contains them. This is a render-layer
  // guard that mocking openSSHHostsBlob would otherwise leave uncovered.
  it("never renders planted secret-shaped fields even if the reader hands back extra properties (indiscriminate-dump guard)", async () => {
    setAccountKeyProvider(() => ACCOUNT_KEY);
    setSyncedValue(RAW_VALUE);
    const base = sampleHosts();
    openSSHHostsBlob.mockReturnValue({
      keys: [{ ...base.keys[0], secret: "CANARY-KEY-SECRET" }],
      hosts: [
        { ...base.hosts[0], password: "CANARY-HOST-PASSWORD", privateKey: "CANARY-PRIVATE-KEY" },
        ...base.hosts.slice(1),
      ],
    });
    const w = mountPanel();
    await flushPromises();
    expect(w.html()).not.toContain("CANARY-");
  });

  // (b) re-deriving the payload inside the component with the raw crypto
  // primitives (openUnsequencedFrame/b64ToBytes from lib/opaque) instead of
  // going through the mocked reader is invisible to any assertion on
  // rendered output in this file: the mock stands in for openSSHHostsBlob,
  // but a bypass that calls the raw primitives itself, against the fake
  // RAW_VALUE used throughout this suite, just no-ops or throws quietly and
  // never touches what those assertions check. The only thing that catches
  // it is proving the import never happens at all.
  it("does not import the raw crypto primitives directly (must go through openSSHHostsBlob only)", () => {
    expect(source).not.toMatch(/from\s+["']\.\.\/lib\/opaque["']/);
    expect(source).not.toContain("openUnsequencedFrame");
    expect(source).not.toContain("b64ToBytes");
  });
});
