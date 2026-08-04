import { bindings } from "./_bindings";
import type {
  FeishuCredentials,
  FeishuRemoteTerminalSettings,
  FeishuStatusResp,
  HookInstallState,
} from "./_bindings";

export type {
  FeishuCredentials,
  FeishuRemoteTerminalSettings,
  FeishuStatusResp,
  HookInstallState,
} from "./_bindings";

export function getFeishuModePref(): Promise<string> {
  return bindings().GetFeishuModePref();
}

export function setFeishuModePref(pref: string): Promise<void> {
  return bindings().SetFeishuModePref(pref);
}

export function getFeishuEffectiveMode(): Promise<string> {
  return bindings().GetFeishuEffectiveMode();
}

export function getFeishuRemoteTerminalSettings(): Promise<FeishuRemoteTerminalSettings> {
  return bindings().GetFeishuRemoteTerminalSettings();
}

export function setFeishuRemoteTerminalSettings(enabled: boolean, autoAttach: string): Promise<void> {
  return bindings().SetFeishuRemoteTerminalSettings(enabled, autoAttach);
}

export function getFeishuStatus(): Promise<FeishuStatusResp> {
  return bindings().GetFeishuStatus();
}

export function setFeishuCredentials(c: FeishuCredentials): Promise<void> {
  return bindings().SetFeishuCredentials(c);
}

export function beginFeishuPair(): Promise<string> {
  return bindings().BeginFeishuPair();
}

export function deleteFeishuBinding(): Promise<void> {
  return bindings().DeleteFeishuBinding();
}

// sendFeishuTestCard renders and sends one notification card to the bound
// OpenID through the live token + IM path, so the user can confirm delivery
// from Settings. scenario ∈ {command_success, command_failure, command_sealed,
// waiting_input}. Rejects with the backend error message on any failure.
export function sendFeishuTestCard(scenario: string): Promise<void> {
  return bindings().SendFeishuTestCard(scenario);
}

// getHookInstallState returns the current Claude Code hook auto-install
// health. The backend silently runs Install when enabled + unhealthy +
// not already attempted in the debounce window, so the returned state
// reflects the post-repair situation.
export function getHookInstallState(): Promise<HookInstallState> {
  return bindings().GetHookInstallState();
}

// setHookInstallEnabled persists the toggle and either installs or
// uninstalls. Errors propagate so the Retry button in Settings can
// surface them.
export function setHookInstallEnabled(on: boolean): Promise<void> {
  return bindings().SetHookInstallEnabled(on);
}
