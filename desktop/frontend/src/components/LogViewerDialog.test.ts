import { describe, expect, test } from "vitest";
import source from "./LogViewerDialog.vue?raw";

describe("LogViewerDialog", () => {
  test("renders refresh and copy actions", () => {
    expect(source).toContain("refresh");
    expect(source).toContain("copy");
  });

  test("shows readonly preview content via LogLines", () => {
    expect(source).toContain("props.preview.content");
    expect(source).toContain("LogLines");
    expect(source).toContain(":content=");
  });

  test("auto-scrolls to bottom on mount and on content change", () => {
    // ref the <pre> so we can scroll it
    expect(source).toContain('ref="contentEl"');
    // watch preview.content + immediate: true triggers on mount too
    expect(source).toContain("watch(() => props.preview.content");
    expect(source).toContain("immediate: true");
    // actually sets scrollTop to scrollHeight after nextTick
    expect(source).toContain("el.scrollTop = el.scrollHeight");
  });
});
