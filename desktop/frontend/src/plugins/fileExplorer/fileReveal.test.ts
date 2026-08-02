import { describe, expect, it, beforeEach } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { useFileRevealStore } from "./fileReveal";

describe("fileReveal store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("request sets pending", () => {
    const s = useFileRevealStore();
    expect(s.pending).toBe(null);
    s.request("/a/b.txt");
    expect(s.pending).toBe("/a/b.txt");
  });

  it("consume returns and clears pending", () => {
    const s = useFileRevealStore();
    s.request("/a/b.txt");
    expect(s.consume()).toBe("/a/b.txt");
    expect(s.pending).toBe(null);
  });

  it("consume returns null when nothing pending", () => {
    const s = useFileRevealStore();
    expect(s.consume()).toBe(null);
  });
});
