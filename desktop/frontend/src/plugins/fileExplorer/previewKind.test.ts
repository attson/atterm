import { describe, expect, it } from "vitest";
import { previewKind, isDualMode } from "./previewKind";

describe("previewKind", () => {
  const cases: Array<[string, string]> = [
    ["/p/photo.png", "image"],
    ["/p/photo.JPG", "image"],
    ["/p/anim.gif", "image"],
    ["/p/icon.webp", "image"],
    ["/p/sprite.bmp", "image"],
    ["/p/favicon.ico", "image"],
    ["/p/logo.svg", "svg"],
    ["/p/clip.mp4", "video"],
    ["/p/clip.WebM", "video"],
    ["/p/clip.mkv", "video"],
    ["/p/clip.mov", "video"],
    ["/p/track.mp3", "audio"],
    ["/p/track.wav", "audio"],
    ["/p/track.ogg", "audio"],
    ["/p/track.flac", "audio"],
    ["/p/track.m4a", "audio"],
    ["/p/doc.pdf", "pdf"],
    ["/p/readme.md", "markdown"],
    ["/p/CHANGELOG.MD", "markdown"],
    ["/p/notes.markdown", "markdown"],
    ["/p/main.go", "code"],
    ["/p/script.sh", "code"],
    ["/p/Dockerfile", "code"],
    ["/p/Makefile", "code"],
    ["/p/notes.txt", "code"],
    ["/p/no-ext", "code"],
  ];
  for (const [path, want] of cases) {
    it(`${path} → ${want}`, () => {
      expect(previewKind(path, /*isBinary*/ false)).toBe(want);
    });
  }

  it("binary + unknown ext → binary-unknown", () => {
    expect(previewKind("/p/blob.dat", true)).toBe("binary-unknown");
  });

  it("binary + image ext still → image", () => {
    expect(previewKind("/p/foo.png", true)).toBe("image");
  });

  it("text + unknown ext defaults to code (let CodeViewer show its binary banner)", () => {
    expect(previewKind("/p/blob.dat", false)).toBe("code");
  });
});

describe("isDualMode", () => {
  it("svg has dual mode", () => { expect(isDualMode("svg")).toBe(true); });
  it("markdown has dual mode", () => { expect(isDualMode("markdown")).toBe(true); });
  it("code does not", () => { expect(isDualMode("code")).toBe(false); });
  it("image does not", () => { expect(isDualMode("image")).toBe(false); });
  it("pdf does not", () => { expect(isDualMode("pdf")).toBe(false); });
});
