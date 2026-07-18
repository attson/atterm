import { describe, expect, test, vi } from "vitest";
import { dispatchPastedFile, type DispatchOptions } from "./pasteFileDispatch";

type PartialItem = {
  kind: string;
  type: string;
  getAsFile: () => File | null;
};

function makeItem(type: string, file: File | null): PartialItem {
  return { kind: "file", type, getAsFile: () => file };
}

function makeFile(name: string, mime: string, size = 8): File {
  const bytes = new Uint8Array(size);
  return new File([bytes], name, { type: mime });
}

function baseOpts(overrides: Partial<DispatchOptions>): DispatchOptions {
  const noopConn = {
    sendPasteImage: vi.fn(async () => true),
    sendPasteFile: vi.fn(async () => true),
  };
  return {
    items: [],
    isLocalSession: true,
    conn: noopConn,
    paste: vi.fn(),
    getLocalPaths: vi.fn(async () => []),
    onFileToast: vi.fn(),
    ...overrides,
  } as DispatchOptions;
}

describe("dispatchPastedFile", () => {
  test("local: single file URL → injects quoted path, no upload", async () => {
    const paste = vi.fn();
    const getLocalPaths = vi.fn(async () => ["/Users/a/docker-compose.yml"]);
    const sendPasteImage = vi.fn(async () => true);
    const sendPasteFile = vi.fn(async () => true);
    const onFileToast = vi.fn();
    const items = [makeItem("image/png", makeFile("clip.png", "image/png"))] as unknown as DataTransferItem[];

    const result = await dispatchPastedFile(baseOpts({
      items,
      isLocalSession: true,
      conn: { sendPasteImage, sendPasteFile },
      paste,
      getLocalPaths,
      onFileToast,
    }));

    expect(result).toBe("path-injected");
    expect(paste).toHaveBeenCalledTimes(1);
    expect(paste).toHaveBeenCalledWith("'/Users/a/docker-compose.yml'");
    expect(sendPasteImage).not.toHaveBeenCalled();
    expect(sendPasteFile).not.toHaveBeenCalled();
    expect(onFileToast).not.toHaveBeenCalled();
  });

  test("local: multiple file URLs with spaces → single joined paste", async () => {
    const paste = vi.fn();
    const getLocalPaths = vi.fn(async () => ["/tmp/A B", "/tmp/C D"]);
    const items = [makeItem("application/pdf", makeFile("A B", "application/pdf"))] as unknown as DataTransferItem[];

    const result = await dispatchPastedFile(baseOpts({
      items,
      getLocalPaths,
      paste,
    }));

    expect(result).toBe("path-injected");
    expect(paste).toHaveBeenCalledTimes(1);
    expect(paste).toHaveBeenCalledWith("'/tmp/A B' '/tmp/C D'");
  });

  test("local: image clipboard w/ no URLs → sendPasteImage fallback", async () => {
    const paste = vi.fn();
    const sendPasteImage = vi.fn(async () => true);
    const sendPasteFile = vi.fn(async () => true);
    const items = [makeItem("image/png", makeFile("clip.png", "image/png"))] as unknown as DataTransferItem[];

    const result = await dispatchPastedFile(baseOpts({
      items,
      getLocalPaths: vi.fn(async () => []),
      conn: { sendPasteImage, sendPasteFile },
      paste,
    }));

    expect(result).toBe("image-sent");
    expect(sendPasteImage).toHaveBeenCalledTimes(1);
    expect(sendPasteImage).toHaveBeenCalledWith(expect.any(Blob), "clip.png");
    expect(sendPasteFile).not.toHaveBeenCalled();
    expect(paste).not.toHaveBeenCalled();
  });

  test("local: non-image file w/ no URLs → sendPasteFile + toast", async () => {
    const paste = vi.fn();
    const sendPasteImage = vi.fn(async () => true);
    const sendPasteFile = vi.fn(async () => true);
    const onFileToast = vi.fn();
    const items = [makeItem("application/pdf", makeFile("notes.pdf", "application/pdf", 32))] as unknown as DataTransferItem[];

    const result = await dispatchPastedFile(baseOpts({
      items,
      getLocalPaths: vi.fn(async () => []),
      conn: { sendPasteImage, sendPasteFile },
      paste,
      onFileToast,
    }));

    expect(result).toBe("file-sent");
    expect(sendPasteFile).toHaveBeenCalledTimes(1);
    expect(sendPasteFile).toHaveBeenCalledWith(expect.any(Blob), "notes.pdf");
    expect(sendPasteImage).not.toHaveBeenCalled();
    expect(onFileToast).toHaveBeenCalledWith("notes.pdf", 32);
    expect(paste).not.toHaveBeenCalled();
  });

  test("remote: image item → skips local-path probe, sendPasteImage runs", async () => {
    const getLocalPaths = vi.fn(async () => ["/should/not/be/used"]);
    const sendPasteImage = vi.fn(async () => true);
    const items = [makeItem("image/png", makeFile("clip.png", "image/png"))] as unknown as DataTransferItem[];

    const result = await dispatchPastedFile(baseOpts({
      items,
      isLocalSession: false,
      getLocalPaths,
      conn: { sendPasteImage, sendPasteFile: vi.fn(async () => true) },
    }));

    expect(result).toBe("image-sent");
    expect(getLocalPaths).not.toHaveBeenCalled();
    expect(sendPasteImage).toHaveBeenCalledTimes(1);
  });

  test("oversize → skipped, no side effects", async () => {
    const big = makeFile("huge.bin", "application/octet-stream", 8);
    Object.defineProperty(big, "size", { value: 11 * 1024 * 1024 });
    const paste = vi.fn();
    const sendPasteFile = vi.fn(async () => true);
    const sendPasteImage = vi.fn(async () => true);
    const items = [makeItem("application/octet-stream", big)] as unknown as DataTransferItem[];

    const result = await dispatchPastedFile(baseOpts({
      items,
      conn: { sendPasteImage, sendPasteFile },
      paste,
    }));

    expect(result).toBe("skipped");
    expect(paste).not.toHaveBeenCalled();
    expect(sendPasteFile).not.toHaveBeenCalled();
    expect(sendPasteImage).not.toHaveBeenCalled();
  });

  test("no file item at all → skipped", async () => {
    const stringItem = { kind: "string", type: "text/plain", getAsFile: () => null };
    const items = [stringItem] as unknown as DataTransferItem[];

    const result = await dispatchPastedFile(baseOpts({ items }));

    expect(result).toBe("skipped");
  });
});
