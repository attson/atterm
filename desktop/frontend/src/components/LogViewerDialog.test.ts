import { describe, expect, test } from "vitest";
import source from "./LogViewerDialog.vue?raw";

describe("LogViewerDialog", () => {
  test("renders refresh and copy actions", () => {
    expect(source).toContain("refresh");
    expect(source).toContain("copy");
  });

  test("shows readonly preview content", () => {
    expect(source).toContain("props.preview.content");
    expect(source).toContain("white-space: pre-wrap");
  });
});
