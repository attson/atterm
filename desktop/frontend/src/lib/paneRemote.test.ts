import { describe, expect, it } from "vitest";
import { resolvePaneRemote } from "./paneRemote";

describe("resolvePaneRemote", () => {
  it("keeps a remote request for a session this machine does not have", () => {
    expect(resolvePaneRemote("s1", ["local-a"], true)).toEqual({
      remote: true,
      corrected: false,
    });
  });

  // The invariant that makes `remote: true` safe — a local session is always
  // already on screen, so it never reaches this branch — is not enforced
  // anywhere. If it breaks, the pane would attach through the relay endpoint,
  // which does not have the session, and render as an empty pane. Trust the
  // local session list instead and flag it.
  it("overrides a remote request for a session that lives here", () => {
    expect(resolvePaneRemote("local-a", ["local-a"], true)).toEqual({
      remote: false,
      corrected: true,
    });
  });

  // An explicit local request is the caller carrying a pane's own flag across
  // (a detach, a drag); it is authoritative and never second-guessed.
  it("leaves an explicit local request alone", () => {
    expect(resolvePaneRemote("s1", [], false)).toEqual({
      remote: false,
      corrected: false,
    });
  });
});
