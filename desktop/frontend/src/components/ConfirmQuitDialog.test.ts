import { describe, expect, test } from "vitest";
import source from "./ConfirmQuitDialog.vue?raw";

describe("ConfirmQuitDialog", () => {
  test("defines localCount and remoteCount props", () => {
    expect(source).toContain("localCount: number");
    expect(source).toContain("remoteCount: number");
  });

  test("emits confirm and cancel", () => {
    expect(source).toMatch(/\(e:\s*"confirm"\)\s*:\s*void/);
    expect(source).toMatch(/\(e:\s*"cancel"\)\s*:\s*void/);
  });

  test("renders count-driven copy and a quit button", () => {
    expect(source).toContain("End ");
    expect(source).toContain("local shell session");
    expect(source).toContain("Detach from ");
    expect(source).toContain("remote session");
    expect(source).toMatch(/>\s*quit\s*</);
    expect(source).toMatch(/>\s*cancel\s*</);
  });

  test("applies primary.danger styling when local count > 0", () => {
    expect(source).toMatch(/:class="\{[^}]*danger:\s*localCount\s*>\s*0/);
    expect(source).toContain('class="primary"');
  });

  test("backdrop click cancels", () => {
    expect(source).toContain('@click.self="$emit(\'cancel\')"');
  });
});
