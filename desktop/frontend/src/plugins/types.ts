import type { ComputedRef, Ref } from "vue";
import type { Endpoint } from "../lib/api";
import type { Pane } from "../lib/types";

/** Public API surface exposed to plugins via the plugin context. */
export interface PluginContext {
  activePane: Ref<Pane | null>;
  activeSessionId: ComputedRef<string | null>;
  activeEndpoint: ComputedRef<Endpoint | null>;
  activeCwd: ComputedRef<string | null>;
  send: (text: string) => void;
  showToast: (msg: string) => void;
}
