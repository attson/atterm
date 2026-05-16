import { computed, type ComputedRef, type Ref } from "vue";
import type { Endpoint } from "../lib/api";
import type { SessionInfo } from "../lib/connection";
import type { Pane } from "../lib/types";
import type { PluginContext } from "./types";

export interface PluginContextInputs {
  activePane: Ref<Pane | null>;
  endpointForPane: (pane: Pane) => Endpoint | null;
  sessionInfoForPane: (pane: Pane) => SessionInfo | null;
  sendToSession: (sessionId: string, endpoint: Endpoint, text: string) => void;
  showToast: (msg: string) => void;
}

export function createPluginContext(inputs: PluginContextInputs): PluginContext {
  const activeSessionId = computed(() => inputs.activePane.value?.sessionId ?? null);

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
      inputs.showToast("No active session");
      return;
    }
    const ep = inputs.endpointForPane(p);
    if (!ep) {
      inputs.showToast("No endpoint");
      return;
    }
    inputs.sendToSession(p.sessionId, ep, text);
  }

  return {
    activePane: inputs.activePane,
    activeSessionId,
    activeEndpoint,
    activeCwd,
    send,
    showToast: inputs.showToast,
  };
}
