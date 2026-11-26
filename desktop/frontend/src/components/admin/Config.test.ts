import { describe, expect, test } from "vitest";
import source from "./Config.vue?raw";

describe("admin Config layout", () => {
  test("groups limits, debug controls, and origins into distinct form sections", () => {
    expect(source).toContain('class="config-section limits-section"');
    expect(source).toContain('class="config-section debug-section"');
    expect(source).toContain('class="config-section origins-section"');
    expect(source).toMatch(/\.debug-controls\s*\{[\s\S]*?grid-template-columns:/);
    expect(source).toMatch(/\.config-section \+ \.config-section\s*\{[\s\S]*?border-top:/);
  });

  test("keeps the layout responsive without nesting section cards", () => {
    expect(source).toContain("@media (max-width: 760px)");
    expect(source).toMatch(/\.limits-grid,[\s\S]*?\.debug-controls\s*\{\s*grid-template-columns:\s*1fr/);
    expect(source).not.toContain("<n-card");
  });

  test("loads, saves, and renders the direct signaling switch", () => {
    expect(source).toContain("directSignalInput.value = c.direct_signal_enabled");
    expect(source).toContain("direct_signal_enabled: directSignalInput.value");
    expect(source).toContain('data-testid="cfg-direct-signal"');
    expect(source).toContain("admin.config.directSignal");
  });
});
