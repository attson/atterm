import { describe, it, expect, beforeEach } from "vitest";
import {
  getCurrentAccountKey,
  setAccountKeyProvider,
} from "./account-key";

describe("account-key provider", () => {
  beforeEach(() => {
    setAccountKeyProvider(null);
  });

  it("returns null when no provider is registered", () => {
    expect(getCurrentAccountKey()).toBeNull();
  });

  it("returns whatever the provider returns", () => {
    const key = new Uint8Array(32).fill(7);
    setAccountKeyProvider(() => key);
    expect(getCurrentAccountKey()).toBe(key);
  });

  it("swallows a throwing provider and returns null", () => {
    setAccountKeyProvider(() => {
      throw new Error("boom");
    });
    expect(getCurrentAccountKey()).toBeNull();
  });

  it("respects null returns (logged out)", () => {
    setAccountKeyProvider(() => null);
    expect(getCurrentAccountKey()).toBeNull();
  });
});
