import { describe, expect, test } from "vitest";
import source from "./PaneGrid.vue?raw";

describe("PaneGrid viewers badge", () => {
  test("accepts a viewerCountFor prop", () => {
    expect(source).toMatch(/viewerCountFor\?: \(sessionId: string\) => number/);
  });
  test("renders a viewers badge on local panes when count > 0", () => {
    expect(source).toMatch(/class="viewers-badge"/);
    expect(source).toMatch(/!pane\.remote/);
    expect(source).toMatch(/viewerCountFor\?\.\(pane\.sessionId\)/);
  });
  test("lets the SP1 overlay dodge the viewers badge too", () => {
    expect(source).toMatch(/avoid-top-right-badge="pane\.remote \|\|/);
  });
});
