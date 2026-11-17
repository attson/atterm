import { beforeEach, describe, expect, test, vi } from "vitest";
import {
  DEFAULT_SESSION_LABEL_COLORS,
  SESSION_LABEL_APPEARANCE_STORAGE_KEY,
  __resetForTests,
  useSessionLabelAppearance,
} from "./useSessionLabelAppearance";

beforeEach(() => {
  localStorage.clear();
  __resetForTests();
  vi.restoreAllMocks();
});

describe("useSessionLabelAppearance", () => {
  test("defaults to configurable category colors", () => {
    const appearance = useSessionLabelAppearance();
    expect(appearance.mode.value).toBe("category");
    expect(appearance.colors.value).toEqual(DEFAULT_SESSION_LABEL_COLORS);
  });

  test("normalizes invalid stored members independently", () => {
    localStorage.setItem(SESSION_LABEL_APPEARANCE_STORAGE_KEY, JSON.stringify({
      mode: "neutral",
      colors: { ssh: "#123456", codex: "red", claude: "#abcdef" },
    }));

    const appearance = useSessionLabelAppearance();
    expect(appearance.mode.value).toBe("neutral");
    expect(appearance.colors.value).toEqual({
      ssh: "#123456",
      codex: DEFAULT_SESSION_LABEL_COLORS.codex,
      claude: "#ABCDEF",
    });
  });

  test("falls back member-by-member for malformed storage", () => {
    localStorage.setItem(SESSION_LABEL_APPEARANCE_STORAGE_KEY, "not-json");
    expect(useSessionLabelAppearance().colors.value).toEqual(DEFAULT_SESSION_LABEL_COLORS);
  });

  test("switching modes preserves customized colors", () => {
    const appearance = useSessionLabelAppearance();
    expect(appearance.setColor("ssh", "#112233")).toBe(true);
    expect(appearance.setMode("neutral")).toBe(true);
    expect(appearance.colors.value.ssh).toBe("#112233");
    expect(appearance.setMode("category")).toBe(true);
    expect(appearance.colors.value.ssh).toBe("#112233");
  });

  test("persists changes across a fresh store load", () => {
    const appearance = useSessionLabelAppearance();
    appearance.setColor("claude", "#123456");
    appearance.setMode("neutral");

    __resetForTests();
    const reloaded = useSessionLabelAppearance();
    expect(reloaded.mode.value).toBe("neutral");
    expect(reloaded.colors.value.claude).toBe("#123456");
  });

  test("resets one color or all colors", () => {
    const appearance = useSessionLabelAppearance();
    appearance.setColor("ssh", "#112233");
    appearance.setColor("codex", "#445566");
    appearance.resetColor("ssh");
    expect(appearance.colors.value.ssh).toBe(DEFAULT_SESSION_LABEL_COLORS.ssh);
    expect(appearance.colors.value.codex).toBe("#445566");
    appearance.resetColors();
    expect(appearance.colors.value).toEqual(DEFAULT_SESSION_LABEL_COLORS);
  });

  test("a persistence failure keeps the last valid state", () => {
    const appearance = useSessionLabelAppearance();
    appearance.setColor("ssh", "#112233");
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });

    expect(appearance.setMode("neutral")).toBe(false);
    expect(appearance.mode.value).toBe("category");
    expect(appearance.colors.value.ssh).toBe("#112233");
    expect(appearance.saveError.value).toBe(true);
  });
});
