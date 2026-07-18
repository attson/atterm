import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from "vitest";
import PasteImagePreviewHost from "../PasteImagePreviewHost.vue";
import { _resetHandlersForTest, pasteImageBus } from "../../lib/pasteImageBus";

function makeBlob() {
  return new Blob([new Uint8Array([1, 2, 3])], { type: "image/png" });
}

describe("PasteImagePreviewHost", () => {
  // The bare `ReturnType<typeof vi.spyOn>` form drops the specific overload
  // and trips vue-tsc on Blob → unknown variance. Pin the MockInstance shape
  // to the actual method signatures instead.
  let createSpy: MockInstance<[obj: Blob | MediaSource], string>;
  let revokeSpy: MockInstance<[url: string], void>;
  let urlCounter = 0;

  beforeEach(() => {
    _resetHandlersForTest();
    vi.useFakeTimers();
    urlCounter = 0;
    createSpy = vi
      .spyOn(URL, "createObjectURL")
      .mockImplementation(() => `blob:fake/${++urlCounter}`);
    revokeSpy = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.useRealTimers();
    createSpy.mockRestore();
    revokeSpy.mockRestore();
  });

  it("renders a toast when the bus emits", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "shot.png");
    await flushPromises();

    const toasts = w.findAll('[data-testid="paste-toast"]');
    expect(toasts.length).toBe(1);
    expect(toasts[0].find("img").attributes("src")).toBe("blob:fake/1");
    expect(toasts[0].text()).toContain("shot.png");
    w.unmount();
  });

  it("auto-dismisses the toast after 5 seconds and revokes the url", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(1);

    vi.advanceTimersByTime(5000);
    await flushPromises();

    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/1");
    w.unmount();
  });

  it("pauses the timer on hover and resumes when the cursor leaves", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    const toast = w.find('[data-testid="paste-toast"]');

    vi.advanceTimersByTime(3000);
    await toast.trigger("mouseenter");

    vi.advanceTimersByTime(10_000); // would normally fire
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(1);

    await toast.trigger("mouseleave");
    vi.advanceTimersByTime(5000);
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    w.unmount();
  });

  it("closes the toast immediately on × click", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();

    await w.find('[data-testid="paste-toast-close"]').trigger("click");
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/1");
    w.unmount();
  });

  it("opens a lightbox with an independent url when the thumbnail is clicked", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();

    await w.find('[data-testid="paste-toast"] img').trigger("click");
    const lightbox = w.find('[data-testid="paste-lightbox"]');
    expect(lightbox.exists()).toBe(true);
    expect(lightbox.find("img").attributes("src")).toBe("blob:fake/2");
    expect(createSpy).toHaveBeenCalledTimes(2);
    w.unmount();
  });

  it("closes the lightbox when the backdrop is clicked and revokes its url", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast"] img').trigger("click");

    await w.find('[data-testid="paste-lightbox"]').trigger("click");
    expect(w.find('[data-testid="paste-lightbox"]').exists()).toBe(false);
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/2");
    w.unmount();
  });

  it("closes the lightbox on Esc", async () => {
    const w = mount(PasteImagePreviewHost, { attachTo: document.body });
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast"] img').trigger("click");

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    await flushPromises();
    expect(w.find('[data-testid="paste-lightbox"]').exists()).toBe(false);
    w.unmount();
  });

  it("keeps the lightbox visible after the source toast auto-dismisses", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast"] img').trigger("click");

    vi.advanceTimersByTime(5000);
    await flushPromises();

    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(w.find('[data-testid="paste-lightbox"]').exists()).toBe(true);
    expect(w.find('[data-testid="paste-lightbox"] img').attributes("src")).toBe("blob:fake/2");
    w.unmount();
  });

  it("stacks multiple toasts in arrival order", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    pasteImageBus.emit(makeBlob(), "b.png");
    await flushPromises();

    const toasts = w.findAll('[data-testid="paste-toast"]');
    expect(toasts.length).toBe(2);
    expect(toasts[0].text()).toContain("a.png");
    expect(toasts[1].text()).toContain("b.png");
    w.unmount();
  });

  it("cleans up timers, urls and bus subscription on unmount", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    pasteImageBus.emit(makeBlob(), "b.png");
    await flushPromises();

    w.unmount();

    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/1");
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/2");

    revokeSpy.mockClear();
    pasteImageBus.emit(makeBlob(), "c.png");
    expect(revokeSpy).not.toHaveBeenCalled();
  });

  it("skips the toast (without throwing) when createObjectURL fails", async () => {
    createSpy.mockImplementationOnce(() => {
      throw new Error("oom");
    });
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});

    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();

    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(warn).toHaveBeenCalled();

    warn.mockRestore();
    w.unmount();
  });
});
