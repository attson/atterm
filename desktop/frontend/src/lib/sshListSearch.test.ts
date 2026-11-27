import { describe, expect, it } from "vitest";
import type { SSHHost, SSHKey } from "./api/ssh";
import { matchesSSHHost, matchesSSHKey } from "./sshListSearch";

describe("sshListSearch", () => {
  it("matches a host across identity, tag, and group fields", () => {
    const host: SSHHost = {
      id: "h1",
      alias: "Production API",
      host: "10.0.0.8",
      user: "deploy",
      auth_kind: "key",
      tags: ["urgent"],
      group_path: ["Cloud", "AWS"],
    };

    expect(matchesSSHHost(host, " production ")).toBe(true);
    expect(matchesSSHHost(host, "10.0.0.8")).toBe(true);
    expect(matchesSSHHost(host, "DEPLOY")).toBe(true);
    expect(matchesSSHHost(host, "urgent")).toBe(true);
    expect(matchesSSHHost(host, "aws")).toBe(true);
    expect(matchesSSHHost(host, "missing")).toBe(false);
  });

  it("matches keys by name and key type", () => {
    const key: SSHKey = {
      id: "k1",
      name: "GitHub Attson",
      key_type: "ED25519",
    };

    expect(matchesSSHKey(key, " github ")).toBe(true);
    expect(matchesSSHKey(key, "ed25519")).toBe(true);
    expect(matchesSSHKey(key, "rsa")).toBe(false);
  });
});
