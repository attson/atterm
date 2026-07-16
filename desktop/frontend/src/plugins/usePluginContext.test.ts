import { markRaw, nextTick, reactive, ref } from "vue";
import { describe, expect, test } from "vitest";
import { createPluginContext } from "./usePluginContext";
import type { SessionConnection } from "../lib/connection";

describe("createPluginContext", () => {
  test("exposes the active remote pane and updates when its connection registers", async () => {
    const activePane = ref({ sessionId: "remote-session", remote: true });
    const connection = {} as SessionConnection;
    const connections = reactive(new Map<string, SessionConnection>()) as Map<string, SessionConnection>;
    const context = createPluginContext({
      activePane,
      endpointForPane: () => null,
      sessionInfoForPane: () => null,
      sessionConnectionForPane: (pane) =>
        pane.sessionId ? connections.get(pane.sessionId) ?? null : null,
      sendToSession: () => {},
      showToast: () => {},
      terminalThemeId: ref("classic"),
    });

    expect(context.activeIsRemote.value).toBe(true);
    expect(context.activeSessionConnection.value).toBeNull();

    connections.set("remote-session", markRaw(connection) as SessionConnection);
    await nextTick();
    expect(context.activeSessionConnection.value).toBe(connection);

    connections.delete("remote-session");
    await nextTick();
    expect(context.activeSessionConnection.value).toBeNull();
  });
});
