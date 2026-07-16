import { ref } from "vue";
import { describe, expect, test } from "vitest";
import { createPluginContext } from "./usePluginContext";
import type { SessionConnection } from "../lib/connection";

describe("createPluginContext", () => {
  test("exposes the active remote pane and its live session connection", () => {
    const activePane = ref({ sessionId: "remote-session", remote: true });
    const connection = {} as SessionConnection;
    const context = createPluginContext({
      activePane,
      endpointForPane: () => null,
      sessionInfoForPane: () => null,
      sessionConnectionForPane: (pane) =>
        pane.sessionId === "remote-session" ? connection : null,
      sendToSession: () => {},
      showToast: () => {},
      terminalThemeId: ref("classic"),
    });

    expect(context.activeIsRemote.value).toBe(true);
    expect(context.activeSessionConnection.value).toBe(connection);
  });
});
