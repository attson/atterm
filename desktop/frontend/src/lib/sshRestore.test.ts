import { describe, it, expect } from "vitest";
import { classifySSHRestore } from "./sshRestore";
import type { RecoveryPaneSnapshot } from "./api";

function snap(o: Partial<RecoveryPaneSnapshot>): RecoveryPaneSnapshot {
  return { slot: 0, shell: "", ...o };
}

describe("classifySSHRestore", () => {
  it("reconnects a saved-host SSH pane by ssh_host_id", () => {
    const r = classifySSHRestore(snap({ ssh_host_id: "host-1", shell: "ssh", title: "ssh u@h" }));
    expect(r).toEqual({ kind: "reconnect", hostId: "host-1" });
  });

  it("marks an ad-hoc SSH pane (ssh title, no host id) as adhoc", () => {
    const r = classifySSHRestore(snap({ title: "ssh u@10.0.0.9", shell: "ssh" }));
    expect(r).toEqual({ kind: "adhoc" });
  });

  it("treats a plain local shell as local", () => {
    expect(classifySSHRestore(snap({ shell: "/bin/zsh", title: "zsh" }))).toEqual({ kind: "local" });
    expect(classifySSHRestore(snap({ shell: "bash" }))).toEqual({ kind: "local" });
  });

  it("does not misclassify a command that merely contains ssh", () => {
    // e.g. running `sshuttle` or a path — must not start with "ssh "
    expect(classifySSHRestore(snap({ shell: "sshuttle" }))).toEqual({ kind: "local" });
  });

  it("saved-host id wins even if title is not an ssh string", () => {
    expect(classifySSHRestore(snap({ ssh_host_id: "h", shell: "bash" }))).toEqual({
      kind: "reconnect",
      hostId: "h",
    });
  });
});
