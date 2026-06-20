import { beforeEach, afterEach, describe, expect, test, vi } from "vitest";
import { __setPlatformForTests } from '../platform'
import { createFakePlatform } from '../platform/__tests__/_fakePlatform'
import source from "./SettingsRelay.vue?raw";

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})

afterEach(() => {
  __setPlatformForTests(null)
})

describe("SettingsRelay", () => {
  test("loads relay config and exposes save through defineExpose", () => {
    expect(source).toContain("getRelayConfig");
    expect(source).toContain("setRelayConfig");
    expect(source).toContain("defineExpose");
    expect(source).toContain("save,");
  });

  test("renders url, login form, permissions SelectDropdown, insecure toggle, and status pill", () => {
    // The scheme is no longer typed; the input takes a bare host.
    expect(source).toContain(["placeholder", '"relay.example.com"'].join("="));
    expect(source).toContain("settings.relay.remotePermissions");
    expect(source).toContain("import SelectDropdown");
    expect(source).toContain("<SelectDropdown");
    expect(source).toContain('v-model="remotePermission"');
    expect(source).toContain("settings.relay.insecureMode");
    expect(source).toContain("settings.relay.connecting");
  });

  test("derives the relay scheme from insecure mode instead of asking the user to type it", () => {
    // Bare host is stored; the scheme prefix is computed.
    expect(source).toContain("const host = ref");
    expect(source).toContain("function stripScheme");
    expect(source).toContain('allowInsecureRelay.value ? "http://" : "https://"');
    // A non-editable prefix renders the derived scheme next to the input.
    expect(source).toContain('class="url-scheme"');
    expect(source).toContain("{{ urlScheme }}");
    // The reconstructed URL is what the backend calls receive.
    expect(source).toContain("probeRelayVersion(fullUrl.value)");
  });

  test("insecure-mode toggle sits on the relay url label row", () => {
    expect(source).toContain('class="url-label-row"');
    expect(source).toContain('class="insecure-inline"');
  });

  test("register is reached through a muted switch link, not a segmented toggle", () => {
    // The old segmented radiogroup is gone.
    expect(source).not.toContain('class="mode-toggle"');
    expect(source).not.toContain('role="radiogroup"');
    // A single switch link toggles the mode.
    expect(source).toContain('class="auth-switch"');
    expect(source).toContain("toggleAuthMode");
    expect(source).toContain("settings.relay.noAccountPrompt");
    expect(source).toContain("settings.relay.haveAccountPrompt");
  });

  test("emits dirty whenever a field diverges from the persisted snapshot", () => {
    expect(source).toMatch(/\(e:\s*"dirty",\s*value:\s*boolean\)\s*:\s*void/);
    expect(source).toContain('emit("dirty"');
  });

  test("emits relay-config-changed after a successful save", () => {
    expect(source).toMatch(/\(e:\s*"relay-config-changed"\)\s*:\s*void/);
    expect(source).toContain('emit("relay-config-changed")');
  });

  test("shows insecure warning paragraph when ws is allowed", () => {
    expect(source).toContain("settings.relay.insecureWarning");
  });

  // Session-token login (Phase 3): email + password are now top-level fields.
  // The save() flow drives LoginRemoteRelay; no standalone Log in button.
  test("renders email + password fields bound to LoginRemoteRelay via save()", () => {
    expect(source).toContain('id="relay-email"');
    expect(source).toContain('id="relay-password"');
    expect(source).toContain("settings.relay.email");
    expect(source).toContain("settings.relay.password");
    expect(source).toContain("loginRemoteRelay");
  });

  test("clears password after successful login", () => {
    expect(source).toContain("password.value = \"\"");
  });

  test("renders Uplink ON/OFF toggle bound to RelayPaused", () => {
    // source must have a toggle bound to `paused` ref
    expect(source).toContain("paused");
    // on-change handler calls setUplinkPaused
    expect(source).toContain("setUplinkPaused");
  });

  test("does not have a 'disconnect' button", () => {
    // The disconnect() function and its button must be removed
    expect(source).not.toContain("disconnect()");
    expect(source).not.toContain("Disconnect");
    expect(source).not.toContain([">", "disconnect", "<"].join(""));
  });

  test("does not surface the legacy atk_ paste-token UI", () => {
    // Old shared-token input + warning are gone.
    expect(source).not.toContain("settings.relay.apiToken");
    expect(source).not.toContain("settings.relay.tokenWarning");
    expect(source).not.toContain("settings.relay.openInBrowser");
    expect(source).not.toContain("atk_");
    expect(source).not.toContain("tokenWarning");
  });

  // Task 8.1 tests
  test("status row shows user_id_short when AUTH_INFO received but email not yet fetched", () => {
    // Must listen for the relay:auth-info event via platform.events.on
    expect(source).toContain("platform.events.on('relay:auth-info'");
    // Must show the localized connected-as message based on a slice of user_id
    expect(source).toContain("settings.relay.connectedAs");
    expect(source).toContain("connectedUserID");
    // Should slice connectedUserID to short form (8 chars)
    expect(source).toMatch(/connectedUserID.*slice\(0,\s*8\)|slice\(0,\s*8\).*connectedUserID/);
  });

  test("status row shows email once /api/me fetch completes", () => {
    // Must call fetchRelayMe (the api.ts wrapper)
    expect(source).toContain("fetchRelayMe");
    // Must have connectedEmail ref
    expect(source).toContain("connectedEmail");
    // Status text should prefer email when available
    expect(source).toMatch(/connectedEmail[\s\S]*settings\.relay\.connectedAs|settings\.relay\.connectedAs[\s\S]*connectedEmail/);
  });

  test("does not import from wailsjs runtime", () => {
    expect(source).not.toContain("wailsjs/runtime");
    expect(source).not.toContain("BrowserOpenURL");
    expect(source).not.toContain("EventsOn");
  });

  test("uses platform for openExternalURL", () => {
    expect(platform.system.openExternalURL).toBeDefined();
  });

  test("uses platform.events.on for relay:auth-info", () => {
    expect(platform.events.on).toBeDefined();
  });

  test("setup wizard is removed in favor of inline email/password login", () => {
    // The paste-token-era wizard is gone; the Log in button is the equivalent verification path.
    expect(source).not.toContain('data-testid="relay-setup-wizard"');
    expect(source).not.toContain("wizardRecovery");
    expect(source).not.toContain("relaySetupWizard");
  });

  test("password input has show/hide toggle", () => {
    // toggle ref + binding present (use whatever 'source' mechanism the other tests use)
    expect(source).toContain("showPassword");
    expect(source).toContain(':type="showPassword ? \'text\' : \'password\'"');
    // toggle button present with aria-pressed bound to the ref
    expect(source).toContain('class="password-toggle"');
    expect(source).toContain(':aria-pressed="showPassword"');
    // i18n keys for the toggle's accessible label
    expect(source).toContain("settings.relay.passwordShow");
    expect(source).toContain("settings.relay.passwordHide");
  });

  test("onMounted prefills email from cfg.last_email", () => {
    expect(source).toContain("cfg.last_email");
    expect(source).toContain("email.value = cfg.last_email");
  });

  test("onMounted eagerly fetches /api/me when a session token is configured", () => {
    expect(source).toContain("if (cfg.token)");
    expect(source).toContain("fetchRelayMe()");
    expect(source).toContain("connectedEmail.value = me.email");
  });

  test("statusPill has 4 states with warn and error classes", () => {
    expect(source).toContain('cls: "warn"');
    expect(source).toContain('cls: "error"');
    expect(source).toContain("settings.relay.connecting");
    expect(source).toContain("settings.relay.relayInvalid");
  });

  test("save() probes /api/version via ProbeRelayVersion before login", () => {
    expect(source).toContain("probeRelayVersion");
    expect(source).toContain("settings.relay.versionProbeFailed");
  });

  test("isValidRelayUrl rejects non-http(s)/ws(s) and empty hosts", () => {
    expect(source).toContain("function isValidRelayUrl");
    expect(source).toContain('"http:"');
    expect(source).toContain('"https:"');
    expect(source).toContain('"ws:"');
    expect(source).toContain('"wss:"');
  });

  test("login card and standalone login button are gone", () => {
    expect(source).not.toContain('data-testid="relay-login-form"');
    expect(source).not.toContain("settings.relay.loginTitle");
    expect(source).not.toContain("settings.relay.loginHint");
    expect(source).not.toContain("settings.relay.loginInProgress");
    expect(source).not.toContain("const loginInProgress");
    expect(source).not.toContain("const loginSuccess");
  });

  test("save() handles three credential paths", () => {
    expect(source).toContain("loginRemoteRelay");
    expect(source).toContain("setRelayConfig");
    expect(source).toContain("settings.relay.credentialsRequired");
  });

  test("offers register mode that calls registerRemoteRelay with claim_token", () => {
    // The OPAQUE register flow lives behind a mode toggle next to the
    // email field. When the user picks "register" the save() handler
    // must route through registerRemoteRelay and forward claimToken.
    expect(source).toContain('authMode');
    expect(source).toContain('"register"');
    expect(source).toContain('registerRemoteRelay');
    expect(source).toContain('claimToken');
    expect(source).toContain('settings.relay.modeRegister');
    expect(source).toContain('settings.relay.modeLogin');
    expect(source).toContain('settings.relay.claimToken');
  });

  test("claim_token input is only rendered in register mode", () => {
    // The claim-token field would confuse login users — show it only
    // when authMode === 'register'.
    expect(source).toMatch(/v-if="authMode === ['"]register['"]"/);
    expect(source).toContain('relay-claim-token');
  });
});
