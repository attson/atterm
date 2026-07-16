import { computed, type ComputedRef, type Ref } from "vue";
import type { Endpoint } from "../lib/api";
import type { SessionConnection, SessionInfo } from "../lib/connection";
import type { Pane } from "../lib/types";
import type { PluginContext } from "./types";
import { t } from "../i18n";

export interface PluginContextInputs {
  activePane: Ref<Pane | null>;
  endpointForPane: (pane: Pane) => Endpoint | null;
  sessionInfoForPane: (pane: Pane) => SessionInfo | null;
  sessionConnectionForPane: (pane: Pane) => SessionConnection | null;
  sendToSession: (sessionId: string, endpoint: Endpoint, text: string) => void;
  showToast: (msg: string) => void;
  terminalThemeId: Ref<string> | ComputedRef<string>;
}

export function createPluginContext(inputs: PluginContextInputs): PluginContext {
  const activeSessionId = computed(() => inputs.activePane.value?.sessionId ?? null);
  const activeIsRemote = computed(() => !!inputs.activePane.value?.remote);

  const activeSessionConnection = computed<SessionConnection | null>(() => {
    const p = inputs.activePane.value;
    return p ? inputs.sessionConnectionForPane(p) : null;
  });

  const activeEndpoint = computed<Endpoint | null>(() => {
    const p = inputs.activePane.value;
    return p ? inputs.endpointForPane(p) : null;
  });

  const activeCwd = computed<string | null>(() => {
    const p = inputs.activePane.value;
    if (!p) return null;
    const info = inputs.sessionInfoForPane(p);
    return info?.cwd ?? null;
  });

  function send(text: string) {
    const p = inputs.activePane.value;
    if (!p || !p.sessionId) {
      inputs.showToast(t("app.noActiveSession"));
      return;
    }
    const ep = inputs.endpointForPane(p);
    if (!ep) {
      inputs.showToast(t("app.noEndpoint"));
      return;
    }
    inputs.sendToSession(p.sessionId, ep, text);
  }

  const terminalThemeId = computed(() => inputs.terminalThemeId.value);

  return {
    activePane: inputs.activePane,
    activeSessionId,
    activeIsRemote,
    activeSessionConnection,
    activeEndpoint,
    activeCwd,
    terminalThemeId,
    send,
    showToast: inputs.showToast,
  };
}
