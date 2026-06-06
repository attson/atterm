import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount } from "@vue/test-utils";
import PdfPreview from "./PdfPreview.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;
beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
});
afterEach(() => __setPlatformForTests(null));

describe("PdfPreview", () => {
  it("renders <object> with the asset URL and application/pdf type", () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockReturnValueOnce("/pluginfs/P");
    const w = mount(PdfPreview, { props: { path: "/x/doc.pdf" } });
    const obj = w.find("object");
    expect(obj.exists()).toBe(true);
    expect(obj.attributes("data")).toBe("/pluginfs/P");
    expect(obj.attributes("type")).toBe("application/pdf");
  });

  it("renders a BinaryBanner inside the <object> as fallback", () => {
    const w = mount(PdfPreview, { props: { path: "/x/doc.pdf" } });
    // BinaryBanner is the <object>'s fallback child; jsdom shows it whether or
    // not the OS PDF plugin is present.
    expect(w.text()).toContain("Inline preview unavailable");
  });
});
