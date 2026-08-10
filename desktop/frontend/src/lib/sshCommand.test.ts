import { describe, it, expect } from "vitest";
import { sshCommandFor } from "./sshCommand";

describe("sshCommandFor", () => {
  it("omits the port flag on the default port", () => {
    expect(sshCommandFor({ user: "root", host: "10.0.0.1", port: "22" })).toBe("ssh root@10.0.0.1");
  });

  it("omits the port flag when no port is set", () => {
    expect(sshCommandFor({ user: "root", host: "10.0.0.1", port: "" })).toBe("ssh root@10.0.0.1");
  });

  it("includes the port flag on a non-default port", () => {
    expect(sshCommandFor({ user: "deploy", host: "example.com", port: "2222" })).toBe(
      "ssh -p 2222 deploy@example.com",
    );
  });

  it("trims stray whitespace around the fields", () => {
    expect(sshCommandFor({ user: " root ", host: " box ", port: " 2222 " })).toBe("ssh -p 2222 root@box");
  });

  it("drops the user prefix when no user is recorded", () => {
    expect(sshCommandFor({ user: "", host: "box", port: "22" })).toBe("ssh box");
  });
});
