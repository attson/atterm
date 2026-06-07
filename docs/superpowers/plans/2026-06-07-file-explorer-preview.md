# File Explorer Multi-Type Highlight + Binary Previews — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the desktop File Explorer plugin so it highlights ~20 more languages (Go / Rust / C-family / Java / PHP / SQL / XML / YAML / Vue / Sass / shell / TOML / Dockerfile / Ruby / Lua / properties / diff / Swift / kt-scala-clike fallback) and inline-previews images, audio, video, and PDFs instead of only showing a "binary" banner.

**Architecture:** Frontend `FileEditor.vue` becomes a thin dispatcher that routes by `previewKind(path)` to one of four sibling components (`CodeViewer`, `ImagePreview`, `MediaPreview`, `PdfPreview`). Code still goes through the existing 5 MB-capped `fs.readFile` JSON path; media goes through a new Wails `AssetServer.Handler` mounted on `*PluginFS` which streams files (with Range support) at `/pluginfs/<base64-path>` after running each request through the existing `PluginFS.resolve()` security check.

**Tech Stack:** Vue 3 + TypeScript + Vite + Vitest; CodeMirror 6 (`@codemirror/lang-*` + `@codemirror/legacy-modes` + `@codemirror/language`); Go (Wails v2 `assetserver.Options.Handler` + stdlib `net/http`).

**Spec:** `docs/superpowers/specs/2026-06-07-file-explorer-preview-design.md`

---

## File Structure

**Backend (Go):**
- Create `desktop/plugin_fs_server.go` — `ServeHTTP` handler on `*PluginFS`.
- Create `desktop/plugin_fs_server_test.go` — handler tests.
- Modify `desktop/main.go` — wire `assetserver.Options.Handler: app.pluginFS`.
- Modify `desktop/app.go` if `pluginFS` is not already a field on `App` (verify in Task 1).

**Frontend platform bridge:**
- Modify `desktop/frontend/src/platform/types.ts` — add `openExternal` + `assetUrlFor` to `PluginHostBridge.fs`.
- Modify `desktop/frontend/src/platform/wails.ts` — implement both.
- Modify `desktop/frontend/src/platform/__tests__/_fakePlatform.ts` — stubs.

**File-explorer plugin:**
- Create `desktop/frontend/src/plugins/fileExplorer/previewKind.ts` + `previewKind.test.ts`.
- Create `desktop/frontend/src/plugins/fileExplorer/BinaryBanner.vue` + `BinaryBanner.test.ts`.
- Create `desktop/frontend/src/plugins/fileExplorer/CodeViewer.vue` + `CodeViewer.test.ts` (extract from current `FileEditor.vue`).
- Rewrite `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue` as dispatcher + `FileEditor.test.ts` for dispatch.
- Create `desktop/frontend/src/plugins/fileExplorer/ImagePreview.vue` + `ImagePreview.test.ts`.
- Create `desktop/frontend/src/plugins/fileExplorer/MediaPreview.vue` + `MediaPreview.test.ts`.
- Create `desktop/frontend/src/plugins/fileExplorer/PdfPreview.vue` + `PdfPreview.test.ts`.
- Modify `desktop/frontend/src/plugins/fileExplorer/tabsModel.ts` + `tabsModel.test.ts` — add `viewMode` field for SVG dual-mode.
- Modify `desktop/frontend/src/plugins/fileExplorer/FileTabs.vue` — add SVG view toggle button.
- Modify `desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue` — pass `viewMode` + handler.
- Modify `desktop/frontend/src/plugins/fileExplorer/languageMap.ts` + `languageMap.test.ts` — new cases.
- Modify `desktop/frontend/package.json` — add CodeMirror lang packs + legacy-modes.

**i18n:**
- Modify `desktop/frontend/src/i18n/en.ts` and `zh.ts` (or wherever the existing `plugins.fileExplorer.*` keys live — `git grep "plugins.fileExplorer.binary"` to confirm) — add `openInSystem`, `previewError`, `unsupportedPreview`, `showAsCode`, `showAsRender`.

**Fixtures:**
- Create `desktop/frontend/src/plugins/fileExplorer/__fixtures__/sample.png` (8×8 PNG, <100 B).
- Create `desktop/frontend/src/plugins/fileExplorer/__fixtures__/sample.svg` (small inline `<circle>`).

---

## Task 1: Backend asset handler (`/pluginfs/<base64-path>`)

**Files:**
- Create: `desktop/plugin_fs_server.go`
- Create: `desktop/plugin_fs_server_test.go`
- Modify: `desktop/main.go:30-50`

### Steps

- [ ] **Step 1: Write the failing handler test (table-driven)**

Create `desktop/plugin_fs_server_test.go`:

```go
package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func encodePath(p string) string {
	return base64.URLEncoding.EncodeToString([]byte(p))
}

func TestServeHTTP_ReturnsFileBytes(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(path)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(resolved), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "hello world" {
		t.Errorf("body=%q", rr.Body.String())
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control=%q want no-store", got)
	}
}

func TestServeHTTP_RejectsNonGet(t *testing.T) {
	fs, _ := makeFS(t)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(m, "/pluginfs/"+encodePath("/whatever"), nil)
		rr := httptest.NewRecorder()
		fs.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status=%d want 405", m, rr.Code)
		}
	}
}

func TestServeHTTP_HeadIsAllowed(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "hello.txt")
	_ = os.WriteFile(path, []byte("hello"), 0o644)
	resolved, _ := filepath.EvalSymlinks(path)
	req := httptest.NewRequest(http.MethodHead, "/pluginfs/"+encodePath(resolved), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("HEAD should have empty body, got %d bytes", rr.Body.Len())
	}
}

func TestServeHTTP_RejectsBadPrefix(t *testing.T) {
	fs, _ := makeFS(t)
	req := httptest.NewRequest(http.MethodGet, "/anything-else", nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestServeHTTP_RejectsBadBase64(t *testing.T) {
	fs, _ := makeFS(t)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/!!!not-base64!!!", nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestServeHTTP_RejectsOutsideRoot(t *testing.T) {
	fs, _ := makeFS(t)
	outside := t.TempDir()
	path := filepath.Join(outside, "leak.txt")
	_ = os.WriteFile(path, []byte("nope"), 0o644)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(path), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d want 403", rr.Code)
	}
}

func TestServeHTTP_RejectsDenyPattern(t *testing.T) {
	fs, home := makeFS(t)
	ssh := filepath.Join(home, ".ssh")
	_ = os.Mkdir(ssh, 0o700)
	keyPath := filepath.Join(ssh, "id_rsa")
	_ = os.WriteFile(keyPath, []byte("secret"), 0o600)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(keyPath), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d want 403", rr.Code)
	}
}

func TestServeHTTP_RejectsMissingFile(t *testing.T) {
	fs, home := makeFS(t)
	missing := filepath.Join(home, "nope.txt")
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(missing), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestServeHTTP_SupportsRange(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "big.bin")
	body := strings.Repeat("ABCDEFGHIJ", 100) // 1000 bytes
	_ = os.WriteFile(path, []byte(body), 0o644)
	resolved, _ := filepath.EvalSymlinks(path)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(resolved), nil)
	req.Header.Set("Range", "bytes=10-19")
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusPartialContent {
		t.Fatalf("status=%d want 206", rr.Code)
	}
	if rr.Body.String() != "ABCDEFGHIJ" {
		t.Errorf("body=%q", rr.Body.String())
	}
}

func TestServeHTTP_RejectsDirectory(t *testing.T) {
	fs, home := makeFS(t)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(home), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d want 403", rr.Code)
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run: `cd desktop && go test ./... -run ServeHTTP -count=1 -v`
Expected: `undefined: (*PluginFS).ServeHTTP` (compile error).

- [ ] **Step 3: Implement the handler**

Create `desktop/plugin_fs_server.go`:

```go
package main

// PluginFS HTTP handler — serves bytes for paths the File Explorer plugin has
// already validated through ListDir / FileMeta. Mounted on the Wails asset
// server so the webview can address local files via stable URLs.
//
// URL form:   /pluginfs/<base64.URLEncoding(absolute-path)>
// Allowed methods: GET, HEAD.
//
// SECURITY (red-line #11): every request runs through PluginFS.resolve()
// which enforces the same allowRoots + denylist as ReadFile. This file is part
// of the same package so the existing CI isolation check
// (.github/scripts/check-plugin-fs-isolation.sh) keeps it inside the desktop
// boundary.

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"strings"
)

const pluginFSURLPrefix = "/pluginfs/"

// ServeHTTP implements http.Handler. Wails routes any asset request that
// doesn't match the embedded SPA to this handler.
func (p *PluginFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, pluginFSURLPrefix) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	encoded := strings.TrimPrefix(r.URL.Path, pluginFSURLPrefix)
	raw, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		http.Error(w, "bad path encoding", http.StatusBadRequest)
		return
	}

	resolved, err := p.resolve(string(raw))
	if err != nil {
		switch {
		case errors.Is(err, ErrPathRelative), errors.Is(err, ErrPathForbidden), errors.Is(err, ErrPathDenied):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			http.Error(w, "forbidden", http.StatusForbidden)
		}
		return
	}

	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "stat failed", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "is a directory", http.StatusForbidden)
		return
	}

	f, err := os.Open(resolved)
	if err != nil {
		http.Error(w, "open failed", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Cache-Control", "no-store")
	// http.ServeContent emits Content-Type from ext, Accept-Ranges: bytes, and
	// handles If-Modified-Since / Range correctly.
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
```

- [ ] **Step 4: Run the tests and verify they pass**

Run: `cd desktop && go test ./... -run ServeHTTP -count=1 -v`
Expected: every `TestServeHTTP_*` test passes.

- [ ] **Step 5: Wire handler into main.go**

Edit `desktop/main.go` lines 31-50 — change the AssetServer block from:

```go
AssetServer: &assetserver.Options{
    Assets: assets,
},
```

to:

```go
AssetServer: &assetserver.Options{
    Assets:  assets,
    Handler: app.pluginFS,
},
```

(`app.pluginFS` already exists — verify by running `grep -n pluginFS desktop/app.go`; if the field is named differently use that field instead.)

- [ ] **Step 6: Verify the desktop build still compiles**

Run: `cd desktop && go vet ./...`
Expected: clean.

Run: `cd desktop && go build ./...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add desktop/plugin_fs_server.go desktop/plugin_fs_server_test.go desktop/main.go
git commit -m "feat(desktop): pluginfs http handler for inline media previews"
```

---

## Task 2: Platform bridge — `fs.openExternal` and `fs.assetUrlFor`

**Files:**
- Modify: `desktop/frontend/src/platform/types.ts:122-132`
- Modify: `desktop/frontend/src/platform/wails.ts` (the `fs:` block — find via `grep -n "fs:" desktop/frontend/src/platform/wails.ts`)
- Modify: `desktop/frontend/src/platform/__tests__/_fakePlatform.ts:106-112`
- Modify: `desktop/plugin_fs.go` — add `OpenExternal` to the bindings exposed by Wails (already exists; verify in step 1).

### Steps

- [ ] **Step 1: Verify `OpenExternal` is already bound**

Run: `grep -n "OpenExternal" desktop/plugin_fs.go`
Expected: matches at lines around 281. (Already present per spec.) `main.go` binds `app.pluginFS` so the method is auto-exposed.

Run: `cd desktop && wails generate module` *if* the Wails bindings TS file (`desktop/frontend/wailsjs/go/main/PluginFS.d.ts`) doesn't already list `OpenExternal`.
Check it: `grep -n "OpenExternal" desktop/frontend/wailsjs/go/main/PluginFS.d.ts || echo MISSING`.
If missing: regenerate. If present: skip.

- [ ] **Step 2: Add the two new bridge methods to the interface**

In `desktop/frontend/src/platform/types.ts`, find the `fs: {` block (around line 125) and replace it with:

```ts
  fs: {
    listDir(path: string): Promise<DirEntry[]>
    watchDir(path: string): Promise<number>      // returns watch id
    unwatchDir(id: number): Promise<void>         // takes watch id
    readFile(path: string, maxBytes?: number): Promise<FileContent>
    fileMeta(path: string): Promise<FileMetaInfo>
    openExternal(path: string): Promise<void>
    /** Returns a same-origin URL that resolves to the file via the Wails
     *  AssetServer.Handler at /pluginfs/<base64.URLEncoding(path)>. The URL
     *  is stable for the lifetime of the file path; no expiry. */
    assetUrlFor(path: string): string
  }
```

- [ ] **Step 3: Implement in wails.ts**

Locate the `fs:` block in `desktop/frontend/src/platform/wails.ts` (use `grep -n "fs:" desktop/frontend/src/platform/wails.ts`). Inside that block add:

```ts
      openExternal: (path: string) => PluginFS.OpenExternal(path),
      assetUrlFor: (path: string) => {
        // base64 URL encoding (RFC 4648 §5), matches Go's base64.URLEncoding.
        const bytes = new TextEncoder().encode(path);
        let bin = "";
        for (const b of bytes) bin += String.fromCharCode(b);
        const b64 = btoa(bin).replace(/\+/g, "-").replace(/\//g, "_");
        return "/pluginfs/" + b64;
      },
```

(Use the same `PluginFS` import already at the top of the file — check `grep -n "from \"../../wailsjs/go/main/PluginFS\"" desktop/frontend/src/platform/wails.ts`.)

- [ ] **Step 4: Add stubs to the test fake**

In `desktop/frontend/src/platform/__tests__/_fakePlatform.ts` find the `fs:` block (around line 106) and add inside it:

```ts
        openExternal: vi.fn().mockResolvedValue(undefined),
        assetUrlFor: vi.fn((p: string) => "/pluginfs/" + btoa(p)),
```

- [ ] **Step 5: Run platform tests**

Run: `cd desktop/frontend && npm test -- --run src/platform`
Expected: existing platform tests pass.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/platform/types.ts desktop/frontend/src/platform/wails.ts desktop/frontend/src/platform/__tests__/_fakePlatform.ts
git add desktop/frontend/wailsjs/go/main/PluginFS.d.ts desktop/frontend/wailsjs/go/main/PluginFS.js 2>/dev/null || true
git commit -m "feat(desktop): expose fs.openExternal + fs.assetUrlFor on plugin host bridge"
```

---

## Task 3: `previewKind.ts` — pure file-type classifier

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/previewKind.ts`
- Create: `desktop/frontend/src/plugins/fileExplorer/previewKind.test.ts`

### Steps

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/fileExplorer/previewKind.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { previewKind } from "./previewKind";

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
```

- [ ] **Step 2: Run the test, verify failure**

Run: `cd desktop/frontend && npm test -- --run previewKind`
Expected: `Cannot find module './previewKind'`.

- [ ] **Step 3: Implement the classifier**

Create `desktop/frontend/src/plugins/fileExplorer/previewKind.ts`:

```ts
export type PreviewKind = "code" | "image" | "svg" | "video" | "audio" | "pdf" | "binary-unknown";

const IMAGE_EXTS = new Set(["png", "jpg", "jpeg", "gif", "webp", "bmp", "ico"]);
const VIDEO_EXTS = new Set(["mp4", "webm", "mkv", "mov"]);
const AUDIO_EXTS = new Set(["mp3", "wav", "ogg", "flac", "m4a"]);

function basename(path: string): string {
  const i = path.lastIndexOf("/");
  return i >= 0 ? path.slice(i + 1) : path;
}

function extOf(name: string): string | null {
  const i = name.lastIndexOf(".");
  if (i <= 0) return null; // ".bashrc" → null (dotfile, no real ext)
  return name.slice(i + 1).toLowerCase();
}

/** Decide which preview component should handle `path`.
 *
 *  `isBinary` is the backend `fileMeta.isBinary` flag (NUL byte in first 4 KB).
 *  - Known media extensions win regardless of `isBinary`.
 *  - For an unknown extension, fall back to `code` so CodeViewer can show its
 *    existing too-large / binary banners. The `binary-unknown` kind is only
 *    returned for unknown-ext AND isBinary, where neither preview applies.
 */
export function previewKind(path: string, isBinary: boolean): PreviewKind {
  const name = basename(path);
  const ext = extOf(name);
  if (ext === "svg") return "svg";
  if (ext && IMAGE_EXTS.has(ext)) return "image";
  if (ext && VIDEO_EXTS.has(ext)) return "video";
  if (ext && AUDIO_EXTS.has(ext)) return "audio";
  if (ext === "pdf") return "pdf";
  if (isBinary) return "binary-unknown";
  return "code";
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `cd desktop/frontend && npm test -- --run previewKind`
Expected: all cases pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/previewKind.ts desktop/frontend/src/plugins/fileExplorer/previewKind.test.ts
git commit -m "feat(file-explorer): previewKind classifier"
```

---

## Task 4: `BinaryBanner.vue` — shared fallback banner with "Open in System"

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/BinaryBanner.vue`
- Create: `desktop/frontend/src/plugins/fileExplorer/BinaryBanner.test.ts`
- Modify: i18n file (`desktop/frontend/src/i18n/en.ts` + `zh.ts` — or wherever existing `plugins.fileExplorer.binary` lives; find with `git grep -l "plugins.fileExplorer.binary" desktop/frontend/src/i18n`).

### Steps

- [ ] **Step 1: Add i18n keys**

For each i18n file located by the grep above, add under `plugins.fileExplorer`:

```ts
openInSystem: "Open in System App",   // en
previewError: "Preview unavailable: {message}",  // en
unsupportedPreview: "Inline preview unavailable for this file type.",  // en
showAsCode: "View as code",   // en
showAsRender: "View as image",  // en
```

…and corresponding 中文 entries in `zh.ts`:

```ts
openInSystem: "用系统应用打开",
previewError: "预览失败：{message}",
unsupportedPreview: "此文件类型暂不支持内嵌预览。",
showAsCode: "看源码",
showAsRender: "看渲染",
```

- [ ] **Step 2: Write the failing test**

Create `desktop/frontend/src/plugins/fileExplorer/BinaryBanner.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount } from "@vue/test-utils";
import BinaryBanner from "./BinaryBanner.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;
beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
});
afterEach(() => __setPlatformForTests(null));

describe("BinaryBanner", () => {
  it("renders the default message and an open-in-system button", () => {
    const w = mount(BinaryBanner, { props: { path: "/x/blob.dat" } });
    expect(w.text()).toContain("Inline preview unavailable");
    expect(w.find("button").exists()).toBe(true);
  });

  it("uses an override message when provided", () => {
    const w = mount(BinaryBanner, { props: { path: "/x/blob.dat", message: "boom" } });
    expect(w.text()).toContain("boom");
  });

  it("clicking the button calls fs.openExternal with the path", async () => {
    const w = mount(BinaryBanner, { props: { path: "/x/blob.dat" } });
    await w.find("button").trigger("click");
    expect(platform.pluginHost!.fs.openExternal).toHaveBeenCalledWith("/x/blob.dat");
  });
});
```

- [ ] **Step 3: Run test, verify failure**

Run: `cd desktop/frontend && npm test -- --run BinaryBanner`
Expected: `Cannot find module './BinaryBanner.vue'`.

- [ ] **Step 4: Implement the component**

Create `desktop/frontend/src/plugins/fileExplorer/BinaryBanner.vue`:

```vue
<script lang="ts" setup>
import { usePlatform } from "../../platform";
import { useI18n } from "../../i18n/useI18n";

const props = defineProps<{ path: string; message?: string }>();
const platform = usePlatform();
const { t } = useI18n();

async function openExternal() {
  try {
    await platform.pluginHost!.fs.openExternal(props.path);
  } catch {
    // The user already sees an error banner; nothing else useful to do here.
  }
}
</script>

<template>
  <div class="binary-banner">
    <span class="msg">{{ message ?? t("plugins.fileExplorer.unsupportedPreview") }}</span>
    <button class="open-btn" @click="openExternal">
      {{ t("plugins.fileExplorer.openInSystem") }}
    </button>
  </div>
</template>

<style scoped>
.binary-banner {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 18px 20px;
  font-size: 13px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.7));
}
.msg { flex: 0 1 auto; }
.open-btn {
  background: var(--ed-shell-bg, #22272e);
  border: 1px solid var(--ed-border, #444c56);
  color: var(--ed-row-fg, #adbac7);
  padding: 3px 10px;
  border-radius: 3px;
  font-size: 12px;
  cursor: pointer;
}
.open-btn:hover { background: var(--ed-row-hover, rgba(173, 186, 199, 0.1)); }
</style>
```

- [ ] **Step 5: Run test, verify pass**

Run: `cd desktop/frontend && npm test -- --run BinaryBanner`
Expected: all 3 pass.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/BinaryBanner.vue desktop/frontend/src/plugins/fileExplorer/BinaryBanner.test.ts desktop/frontend/src/i18n
git commit -m "feat(file-explorer): BinaryBanner shared fallback + i18n keys"
```

---

## Task 5: Extract `CodeViewer.vue` from `FileEditor.vue`

The current `FileEditor.vue` (148 lines of script + 80 lines of style) holds all the CodeMirror logic. Copy it verbatim to `CodeViewer.vue` and port the test alongside; `FileEditor.vue` itself becomes the dispatcher in Task 6.

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/CodeViewer.vue` (copy of current `FileEditor.vue`)
- Create: `desktop/frontend/src/plugins/fileExplorer/CodeViewer.test.ts` (copy of current `FileEditor.test.ts` with `FileEditor` → `CodeViewer`)

### Steps

- [ ] **Step 1: Copy the file verbatim**

```bash
cp desktop/frontend/src/plugins/fileExplorer/FileEditor.vue desktop/frontend/src/plugins/fileExplorer/CodeViewer.vue
```

- [ ] **Step 2: Copy and rename the test**

```bash
cp desktop/frontend/src/plugins/fileExplorer/FileEditor.test.ts desktop/frontend/src/plugins/fileExplorer/CodeViewer.test.ts
```

Then edit `desktop/frontend/src/plugins/fileExplorer/CodeViewer.test.ts` and replace all occurrences of `FileEditor` with `CodeViewer` (5 occurrences: the import path, the `import FileEditor`, the `describe("FileEditor"`, and two `mount(FileEditor, ...)` calls).

- [ ] **Step 3: Run the new test, verify pass**

Run: `cd desktop/frontend && npm test -- --run CodeViewer`
Expected: same 3 tests pass under the new name.

- [ ] **Step 4: Confirm FileEditor tests still pass (unchanged for now)**

Run: `cd desktop/frontend && npm test -- --run FileEditor`
Expected: same 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/CodeViewer.vue desktop/frontend/src/plugins/fileExplorer/CodeViewer.test.ts
git commit -m "refactor(file-explorer): extract CodeViewer from FileEditor (no behavior change)"
```

---

## Task 6: Rewrite `FileEditor.vue` as a dispatcher

After this task `FileEditor.vue` only inspects the path and renders the right child component. CodeMirror, image, audio, video, and PDF preview components do not exist yet (image et al. come in later tasks) — for now the dispatcher imports them as stubs and `tooLarge`/`binary`/non-code paths route to `CodeViewer` until specialized components ship.

Actually: the dispatcher and specialized components ship in a strict order — at the end of Task 6 the dispatcher knows *how* to route (and tests assert the routing decision via a mounted `<slot>` stub for the still-unbuilt children), but the only real child that exists is `CodeViewer`. Tasks 7–9 each replace one stub with a real component.

**Files:**
- Rewrite: `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue`
- Rewrite: `desktop/frontend/src/plugins/fileExplorer/FileEditor.test.ts`

### Steps

- [ ] **Step 1: Write the failing dispatcher test**

Replace `desktop/frontend/src/plugins/fileExplorer/FileEditor.test.ts` with:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import FileEditor from "./FileEditor.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;

beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
});
afterEach(() => __setPlatformForTests(null));

function mountFE(path: string, meta: { size: number; isBinary: boolean }) {
  (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
    path, size: meta.size, modTime: 1, isBinary: meta.isBinary,
  });
  return mount(FileEditor, {
    props: { path, showLineNumbers: false, theme: "dimmed", viewMode: "code" },
    global: {
      stubs: {
        CodeViewer: { template: '<div data-test="kind-code" />' },
        ImagePreview: { template: '<div data-test="kind-image" />' },
        MediaPreview: { template: '<div data-test="kind-media" />' },
        PdfPreview: { template: '<div data-test="kind-pdf" />' },
        BinaryBanner: { template: '<div data-test="kind-banner" />' },
      },
    },
  });
}

describe("FileEditor (dispatcher)", () => {
  it("routes .png to ImagePreview", async () => {
    const w = mountFE("/x/photo.png", { size: 1000, isBinary: true });
    await flushPromises();
    expect(w.find('[data-test="kind-image"]').exists()).toBe(true);
  });

  it("routes .mp4 to MediaPreview", async () => {
    const w = mountFE("/x/clip.mp4", { size: 100_000, isBinary: true });
    await flushPromises();
    expect(w.find('[data-test="kind-media"]').exists()).toBe(true);
  });

  it("routes .mp3 to MediaPreview", async () => {
    const w = mountFE("/x/track.mp3", { size: 100_000, isBinary: true });
    await flushPromises();
    expect(w.find('[data-test="kind-media"]').exists()).toBe(true);
  });

  it("routes .pdf to PdfPreview", async () => {
    const w = mountFE("/x/doc.pdf", { size: 200_000, isBinary: true });
    await flushPromises();
    expect(w.find('[data-test="kind-pdf"]').exists()).toBe(true);
  });

  it("routes .go to CodeViewer", async () => {
    const w = mountFE("/x/main.go", { size: 500, isBinary: false });
    await flushPromises();
    expect(w.find('[data-test="kind-code"]').exists()).toBe(true);
  });

  it("routes unknown-binary to BinaryBanner", async () => {
    const w = mountFE("/x/blob.dat", { size: 200, isBinary: true });
    await flushPromises();
    expect(w.find('[data-test="kind-banner"]').exists()).toBe(true);
  });

  it("routes svg to CodeViewer when viewMode=code", async () => {
    const w = mountFE("/x/logo.svg", { size: 200, isBinary: false });
    await flushPromises();
    expect(w.find('[data-test="kind-code"]').exists()).toBe(true);
  });
});
```

- [ ] **Step 2: Run test, verify failure**

Run: `cd desktop/frontend && npm test -- --run FileEditor`
Expected: failures on every `routes …` case (current FileEditor is the monolithic code viewer; the new tests stub child components that don't render).

- [ ] **Step 3: Rewrite `FileEditor.vue` as the dispatcher**

Replace the entire contents of `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue` with:

```vue
<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import { usePlatform } from "../../platform";
import { previewKind, type PreviewKind } from "./previewKind";
import CodeViewer from "./CodeViewer.vue";
import ImagePreview from "./ImagePreview.vue";
import MediaPreview from "./MediaPreview.vue";
import PdfPreview from "./PdfPreview.vue";
import BinaryBanner from "./BinaryBanner.vue";
import { useI18n } from "../../i18n/useI18n";

const props = defineProps<{
  path: string;
  showLineNumbers: boolean;
  theme: "dimmed" | "light";
  /** SVG dual-mode toggle: "code" → highlight, "render" → ImagePreview. */
  viewMode: "code" | "render";
}>();

const platform = usePlatform();
const fs = platform.pluginHost!.fs;
const { t } = useI18n();

const kind = ref<PreviewKind | null>(null);
const error = ref<string>("");

let off: (() => void) | null = null;

async function resolveKind() {
  kind.value = null;
  error.value = "";
  try {
    const meta = (await fs.fileMeta(props.path)) as { isBinary: boolean };
    kind.value = previewKind(props.path, meta.isBinary);
  } catch (e) {
    error.value = (e as Error).message;
  }
}

onMounted(() => {
  void resolveKind();
  off = platform.events.on("plugin-fs:dir-changed", () => {
    // The dispatch decision depends only on the file path + binary-ness, both
    // of which are recomputed by CodeViewer on the dir-changed event already.
    // No re-dispatch is needed here.
  });
});

watch(() => props.path, () => { void resolveKind(); });

onBeforeUnmount(() => { if (off) off(); });
</script>

<template>
  <div class="file-editor-host">
    <div v-if="error" class="banner err">
      {{ t("plugins.fileExplorer.errorPrefix", { message: error }) }}
    </div>
    <template v-else-if="kind === 'code' || (kind === 'svg' && viewMode === 'code')">
      <CodeViewer
        :path="path"
        :show-line-numbers="showLineNumbers"
        :theme="theme"
      />
    </template>
    <template v-else-if="kind === 'image' || (kind === 'svg' && viewMode === 'render')">
      <ImagePreview :path="path" :theme="theme" />
    </template>
    <template v-else-if="kind === 'video' || kind === 'audio'">
      <MediaPreview :path="path" :kind="kind" />
    </template>
    <template v-else-if="kind === 'pdf'">
      <PdfPreview :path="path" />
    </template>
    <template v-else-if="kind === 'binary-unknown'">
      <BinaryBanner :path="path" />
    </template>
  </div>
</template>

<style scoped>
.file-editor-host {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--ed-editor-bg, #22272e);
}
.banner { padding: 18px 20px; font-size: 13px; }
.err { color: var(--ed-error, #f47067); }
</style>
```

- [ ] **Step 4: Create empty placeholder components so dispatch tests pass**

Until Tasks 7-9 ship, create minimal placeholder components that just render `t('plugins.fileExplorer.unsupportedPreview')`. Each will be rewritten in its own task.

Create `desktop/frontend/src/plugins/fileExplorer/ImagePreview.vue`:

```vue
<script lang="ts" setup>
defineProps<{ path: string; theme: "dimmed" | "light" }>();
</script>
<template><div class="placeholder">image preview placeholder</div></template>
<style scoped>.placeholder { padding: 18px; color: var(--ed-muted, #888); }</style>
```

Create `desktop/frontend/src/plugins/fileExplorer/MediaPreview.vue`:

```vue
<script lang="ts" setup>
defineProps<{ path: string; kind: "audio" | "video" }>();
</script>
<template><div class="placeholder">media preview placeholder</div></template>
<style scoped>.placeholder { padding: 18px; color: var(--ed-muted, #888); }</style>
```

Create `desktop/frontend/src/plugins/fileExplorer/PdfPreview.vue`:

```vue
<script lang="ts" setup>
defineProps<{ path: string }>();
</script>
<template><div class="placeholder">pdf preview placeholder</div></template>
<style scoped>.placeholder { padding: 18px; color: var(--ed-muted, #888); }</style>
```

- [ ] **Step 5: Update `FileExplorer.vue` to pass viewMode**

The `tabsModel` change (Task 10) adds `viewMode`. For Task 6 just hardcode `"code"` so the dispatcher props are satisfied. Find the `<FileEditor>` block in `desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue` (around line 141) and add a `:view-mode="'code'"` prop:

```vue
<FileEditor
  v-if="activePath"
  :path="activePath"
  :show-line-numbers="showLineNumbers"
  :theme="explorerTheme"
  :view-mode="'code'"
/>
```

- [ ] **Step 6: Run dispatcher + CodeViewer tests, verify pass**

Run: `cd desktop/frontend && npm test -- --run FileEditor`
Expected: all 7 dispatch tests pass.

Run: `cd desktop/frontend && npm test -- --run CodeViewer`
Expected: still passing.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileEditor.vue desktop/frontend/src/plugins/fileExplorer/FileEditor.test.ts desktop/frontend/src/plugins/fileExplorer/ImagePreview.vue desktop/frontend/src/plugins/fileExplorer/MediaPreview.vue desktop/frontend/src/plugins/fileExplorer/PdfPreview.vue desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue
git commit -m "refactor(file-explorer): FileEditor as preview dispatcher"
```

---

## Task 7: `ImagePreview.vue` — fit / 1:1 toggle

**Files:**
- Rewrite: `desktop/frontend/src/plugins/fileExplorer/ImagePreview.vue`
- Create: `desktop/frontend/src/plugins/fileExplorer/ImagePreview.test.ts`

### Steps

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/fileExplorer/ImagePreview.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount } from "@vue/test-utils";
import ImagePreview from "./ImagePreview.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;
beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
});
afterEach(() => __setPlatformForTests(null));

describe("ImagePreview", () => {
  it("uses fs.assetUrlFor for the <img> src", () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockReturnValueOnce(
      "/pluginfs/AAAA",
    );
    const w = mount(ImagePreview, { props: { path: "/x/photo.png", theme: "dimmed" } });
    expect(w.find("img").attributes("src")).toBe("/pluginfs/AAAA");
  });

  it("toggles fit ↔ native on click", async () => {
    const w = mount(ImagePreview, { props: { path: "/x/photo.png", theme: "dimmed" } });
    expect(w.find(".img-host").classes()).toContain("fit");
    await w.find("img").trigger("click");
    expect(w.find(".img-host").classes()).toContain("native");
  });

  it("falls back to BinaryBanner on <img> error", async () => {
    const w = mount(ImagePreview, { props: { path: "/x/broken.png", theme: "dimmed" } });
    await w.find("img").trigger("error");
    expect(w.text()).toContain("Inline preview unavailable");
  });
});
```

- [ ] **Step 2: Run the test, verify failure**

Run: `cd desktop/frontend && npm test -- --run ImagePreview`
Expected: failures (placeholder template doesn't have an `img`).

- [ ] **Step 3: Implement ImagePreview**

Replace `desktop/frontend/src/plugins/fileExplorer/ImagePreview.vue`:

```vue
<script lang="ts" setup>
import { ref, computed } from "vue";
import { usePlatform } from "../../platform";
import BinaryBanner from "./BinaryBanner.vue";

const props = defineProps<{ path: string; theme: "dimmed" | "light" }>();
const platform = usePlatform();
const src = computed(() => platform.pluginHost!.fs.assetUrlFor(props.path));

const mode = ref<"fit" | "native">("fit");
const failed = ref(false);

function toggle() { mode.value = mode.value === "fit" ? "native" : "fit"; }
function onError() { failed.value = true; }
</script>

<template>
  <BinaryBanner v-if="failed" :path="path" />
  <div v-else class="img-host" :class="mode">
    <img :src="src" alt="" @click="toggle" @error="onError" />
  </div>
</template>

<style scoped>
.img-host {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: auto;
  background: var(--ed-editor-bg, #22272e);
  padding: 12px;
}
.img-host.fit img {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
  cursor: zoom-in;
}
.img-host.native img {
  width: auto;
  height: auto;
  image-rendering: pixelated;
  cursor: zoom-out;
}
</style>
```

- [ ] **Step 4: Run test, verify pass**

Run: `cd desktop/frontend && npm test -- --run ImagePreview`
Expected: all 3 pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/ImagePreview.vue desktop/frontend/src/plugins/fileExplorer/ImagePreview.test.ts
git commit -m "feat(file-explorer): ImagePreview with fit/native toggle"
```

---

## Task 8: `MediaPreview.vue` — audio + video

**Files:**
- Rewrite: `desktop/frontend/src/plugins/fileExplorer/MediaPreview.vue`
- Create: `desktop/frontend/src/plugins/fileExplorer/MediaPreview.test.ts`

### Steps

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/fileExplorer/MediaPreview.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount } from "@vue/test-utils";
import MediaPreview from "./MediaPreview.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;
beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
});
afterEach(() => __setPlatformForTests(null));

describe("MediaPreview", () => {
  it("renders a <video> tag for kind=video", () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockReturnValueOnce("/pluginfs/V");
    const w = mount(MediaPreview, { props: { path: "/x/c.mp4", kind: "video" } });
    expect(w.find("video").exists()).toBe(true);
    expect(w.find("video").attributes("src")).toBe("/pluginfs/V");
    expect(w.find("video").attributes("preload")).toBe("metadata");
  });

  it("renders an <audio> tag for kind=audio", () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockReturnValueOnce("/pluginfs/A");
    const w = mount(MediaPreview, { props: { path: "/x/t.mp3", kind: "audio" } });
    expect(w.find("audio").exists()).toBe(true);
    expect(w.find("audio").attributes("src")).toBe("/pluginfs/A");
  });

  it("falls back to BinaryBanner on media error", async () => {
    const w = mount(MediaPreview, { props: { path: "/x/c.mp4", kind: "video" } });
    await w.find("video").trigger("error");
    expect(w.text()).toContain("Inline preview unavailable");
  });
});
```

- [ ] **Step 2: Run, verify failure**

Run: `cd desktop/frontend && npm test -- --run MediaPreview`
Expected: failures on `find('video')` because placeholder is just a div.

- [ ] **Step 3: Implement**

Replace `desktop/frontend/src/plugins/fileExplorer/MediaPreview.vue`:

```vue
<script lang="ts" setup>
import { ref, computed } from "vue";
import { usePlatform } from "../../platform";
import BinaryBanner from "./BinaryBanner.vue";

const props = defineProps<{ path: string; kind: "audio" | "video" }>();
const platform = usePlatform();
const src = computed(() => platform.pluginHost!.fs.assetUrlFor(props.path));

const failed = ref(false);
function onError() { failed.value = true; }
</script>

<template>
  <BinaryBanner v-if="failed" :path="path" />
  <div v-else class="media-host">
    <video
      v-if="kind === 'video'"
      :src="src"
      controls
      preload="metadata"
      @error="onError"
    />
    <audio
      v-else
      :src="src"
      controls
      preload="metadata"
      @error="onError"
    />
  </div>
</template>

<style scoped>
.media-host {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--ed-editor-bg, #22272e);
  padding: 16px;
}
video {
  max-width: 100%;
  max-height: 100%;
  background: #000;
  outline: none;
}
audio {
  width: min(480px, 100%);
}
</style>
```

- [ ] **Step 4: Run tests, verify pass**

Run: `cd desktop/frontend && npm test -- --run MediaPreview`
Expected: all 3 pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/MediaPreview.vue desktop/frontend/src/plugins/fileExplorer/MediaPreview.test.ts
git commit -m "feat(file-explorer): MediaPreview for audio + video"
```

---

## Task 9: `PdfPreview.vue` — `<object>` with fallback

**Files:**
- Rewrite: `desktop/frontend/src/plugins/fileExplorer/PdfPreview.vue`
- Create: `desktop/frontend/src/plugins/fileExplorer/PdfPreview.test.ts`

### Steps

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/fileExplorer/PdfPreview.test.ts`:

```ts
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
```

- [ ] **Step 2: Run, verify failure**

Run: `cd desktop/frontend && npm test -- --run PdfPreview`
Expected: failures (placeholder has no `<object>`).

- [ ] **Step 3: Implement**

Replace `desktop/frontend/src/plugins/fileExplorer/PdfPreview.vue`:

```vue
<script lang="ts" setup>
import { computed } from "vue";
import { usePlatform } from "../../platform";
import BinaryBanner from "./BinaryBanner.vue";

const props = defineProps<{ path: string }>();
const platform = usePlatform();
const src = computed(() => platform.pluginHost!.fs.assetUrlFor(props.path));
</script>

<template>
  <div class="pdf-host">
    <object :data="src" type="application/pdf">
      <!-- Shown if the browser can't render PDFs natively. -->
      <BinaryBanner :path="path" />
    </object>
  </div>
</template>

<style scoped>
.pdf-host {
  flex: 1 1 auto;
  display: flex;
  background: var(--ed-editor-bg, #22272e);
}
object {
  flex: 1 1 auto;
  width: 100%;
  height: 100%;
  border: none;
}
</style>
```

- [ ] **Step 4: Run, verify pass**

Run: `cd desktop/frontend && npm test -- --run PdfPreview`
Expected: both pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/PdfPreview.vue desktop/frontend/src/plugins/fileExplorer/PdfPreview.test.ts
git commit -m "feat(file-explorer): PdfPreview via <object> with fallback"
```

---

## Task 10: SVG dual-mode — `tabsModel.viewMode` + FileTabs toggle

**Files:**
- Modify: `desktop/frontend/src/plugins/fileExplorer/tabsModel.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/tabsModel.test.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileTabs.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue`

### Steps

- [ ] **Step 1: Extend tabsModel test**

In `desktop/frontend/src/plugins/fileExplorer/tabsModel.test.ts` add (after the existing describe block — or at the bottom of the file):

```ts
import { setViewMode } from "./tabsModel";

describe("tabsModel.setViewMode", () => {
  it("updates the active tab's viewMode", () => {
    const s = openPath({ tabs: [], activeIdx: -1 }, "/x/logo.svg", "persistent");
    expect(s.tabs[0].viewMode).toBe("code");
    const next = setViewMode(s, "render");
    expect(next.tabs[0].viewMode).toBe("render");
  });

  it("is a no-op when there's no active tab", () => {
    const s: TabsState = { tabs: [], activeIdx: -1 };
    expect(setViewMode(s, "render")).toBe(s);
  });
});
```

- [ ] **Step 2: Run, verify failure**

Run: `cd desktop/frontend && npm test -- --run tabsModel`
Expected: `setViewMode` does not exist.

- [ ] **Step 3: Extend `tabsModel.ts`**

In `desktop/frontend/src/plugins/fileExplorer/tabsModel.ts`:

(a) Change the `Tab` interface (lines 1-6) from

```ts
export interface Tab {
  path: string;
  persistent: boolean;
  // Activation order ts, larger = more recent. Used for LRU eviction.
  lastActiveAt: number;
}
```

to

```ts
export type ViewMode = "code" | "render";

export interface Tab {
  path: string;
  persistent: boolean;
  // Activation order ts, larger = more recent. Used for LRU eviction.
  lastActiveAt: number;
  /** Code-vs-render toggle. Only meaningful for SVG today; harmless on others. */
  viewMode: ViewMode;
}
```

(b) In `openPath`, every place a new `Tab` literal is created (3 spots), add `viewMode: "code"`:

```ts
// Line 33 (previewIdx case):
next.tabs[previewIdx] = { path, persistent: false, lastActiveAt: now, viewMode: "code" };

// Line 41 (append case):
next.tabs.push({ path, persistent: kind === "persistent", lastActiveAt: now, viewMode: "code" });
```

(When an existing tab is re-opened we keep its current `viewMode` — no change to the `existingIdx` branch.)

(c) Append the new function at the bottom of the file:

```ts
export function setViewMode(state: TabsState, mode: ViewMode): TabsState {
  if (state.activeIdx < 0) return state;
  const next = clone(state);
  next.tabs[state.activeIdx].viewMode = mode;
  return next;
}
```

- [ ] **Step 4: Run, verify pass**

Run: `cd desktop/frontend && npm test -- --run tabsModel`
Expected: existing + 2 new tests pass.

- [ ] **Step 5: Wire viewMode into FileExplorer.vue**

In `desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue`:

(a) Above the `const activePath = ...` line, add:

```ts
const activeViewMode = computed<"code" | "render">(() => {
  const i = tabsState.value.activeIdx;
  return i >= 0 ? tabsState.value.tabs[i].viewMode : "code";
});
function onToggleViewMode() {
  tabsState.value = setViewMode(tabsState.value, activeViewMode.value === "code" ? "render" : "code");
}
```

(b) Update the import line that already pulls from `./tabsModel`:

```ts
import { openPath, closeTab, setViewMode, type TabsState } from "./tabsModel";
```

(c) Update the `<FileEditor>` block (set in Task 6) — replace `:view-mode="'code'"` with `:view-mode="activeViewMode"`:

```vue
<FileEditor
  v-if="activePath"
  :path="activePath"
  :show-line-numbers="showLineNumbers"
  :theme="explorerTheme"
  :view-mode="activeViewMode"
/>
```

(d) Pass the toggle handler and active-path's extension info into `<FileTabs>`:

```vue
<FileTabs
  :tabs="tabsState.tabs"
  :active-idx="tabsState.activeIdx"
  :view-mode="activeViewMode"
  @select="selectTab"
  @close="closeTabAt"
  @toggle-view-mode="onToggleViewMode"
/>
```

- [ ] **Step 6: Add the toggle button to FileTabs**

In `desktop/frontend/src/plugins/fileExplorer/FileTabs.vue`:

(a) Add `viewMode` to the props block and `toggle-view-mode` to the emits. Show the toggle only when the active tab's path ends in `.svg` (case-insensitive). Locate the props/emits and template; the existing structure (read it via `Read` first) will tell you whether the file uses script-setup. Assuming script-setup (the rest of the plugin does):

```ts
const props = defineProps<{
  tabs: { path: string; persistent: boolean; viewMode: "code" | "render" }[];
  activeIdx: number;
  viewMode: "code" | "render";
}>();
const emit = defineEmits<{
  (e: "select", idx: number): void;
  (e: "close", idx: number): void;
  (e: "toggle-view-mode"): void;
}>();

const activeIsSvg = computed(() => {
  const i = props.activeIdx;
  if (i < 0) return false;
  return /\.svg$/i.test(props.tabs[i].path);
});
```

(b) Add a button to the right side of the tab row template:

```vue
<button
  v-if="activeIsSvg"
  class="view-toggle"
  :title="viewMode === 'code'
    ? t('plugins.fileExplorer.showAsRender')
    : t('plugins.fileExplorer.showAsCode')"
  @click="emit('toggle-view-mode')"
>
  {{ viewMode === 'code'
    ? t('plugins.fileExplorer.showAsRender')
    : t('plugins.fileExplorer.showAsCode') }}
</button>
```

…and the corresponding minimal CSS:

```css
.view-toggle {
  background: none;
  border: 1px solid var(--ed-border, #444c56);
  color: var(--ed-row-fg, #adbac7);
  font-size: 11px;
  padding: 1px 8px;
  border-radius: 3px;
  margin-left: auto;
  cursor: pointer;
}
.view-toggle:hover { background: var(--ed-row-hover, rgba(173, 186, 199, 0.1)); }
```

- [ ] **Step 7: Extend `FileTabs.test.ts` for the toggle**

Locate the existing `desktop/frontend/src/plugins/fileExplorer/FileTabs.test.ts` (read it first), then append:

```ts
it("shows the SVG view toggle when the active tab is an .svg", () => {
  const w = mount(FileTabs, {
    props: {
      tabs: [{ path: "/x/logo.svg", persistent: true, viewMode: "code" }],
      activeIdx: 0,
      viewMode: "code",
    },
  });
  expect(w.find(".view-toggle").exists()).toBe(true);
});

it("emits toggle-view-mode on click", async () => {
  const w = mount(FileTabs, {
    props: {
      tabs: [{ path: "/x/logo.svg", persistent: true, viewMode: "code" }],
      activeIdx: 0,
      viewMode: "code",
    },
  });
  await w.find(".view-toggle").trigger("click");
  expect(w.emitted("toggle-view-mode")?.length).toBe(1);
});

it("hides the toggle for non-svg active tab", () => {
  const w = mount(FileTabs, {
    props: {
      tabs: [{ path: "/x/main.go", persistent: true, viewMode: "code" }],
      activeIdx: 0,
      viewMode: "code",
    },
  });
  expect(w.find(".view-toggle").exists()).toBe(false);
});
```

If the existing test file already mounts `FileTabs` with the old tab shape (no `viewMode`), update its `tabs:` array literal to include `viewMode: "code"` everywhere.

- [ ] **Step 8: Run all file-explorer tests, verify pass**

Run: `cd desktop/frontend && npm test -- --run plugins/fileExplorer`
Expected: every test passes.

- [ ] **Step 9: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/tabsModel.ts desktop/frontend/src/plugins/fileExplorer/tabsModel.test.ts desktop/frontend/src/plugins/fileExplorer/FileTabs.vue desktop/frontend/src/plugins/fileExplorer/FileTabs.test.ts desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue
git commit -m "feat(file-explorer): SVG code/render toggle"
```

---

## Task 11: Install new CodeMirror packages

**Files:**
- Modify: `desktop/frontend/package.json`
- Modify: `desktop/frontend/package-lock.json`

### Steps

- [ ] **Step 1: Install the packages**

Run from `desktop/frontend`:

```bash
npm install \
  @codemirror/lang-go \
  @codemirror/lang-rust \
  @codemirror/lang-cpp \
  @codemirror/lang-java \
  @codemirror/lang-php \
  @codemirror/lang-sql \
  @codemirror/lang-xml \
  @codemirror/lang-yaml \
  @codemirror/lang-vue \
  @codemirror/lang-sass \
  @codemirror/legacy-modes \
  @codemirror/language
```

(`@codemirror/language` may already be transitively present — but the legacy `streamLanguage.define()` symbol lives there and we want it pinned in `dependencies`.)

- [ ] **Step 2: Verify install**

Run: `ls desktop/frontend/node_modules/@codemirror/ | grep -E '(lang-|legacy-modes|language)'`
Expected: every package above appears in the listing.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/package.json desktop/frontend/package-lock.json
git commit -m "deps(file-explorer): add codemirror lang packs + legacy modes"
```

---

## Task 12: Extend `languageMap.ts`

**Files:**
- Modify: `desktop/frontend/src/plugins/fileExplorer/languageMap.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/languageMap.test.ts`

### Steps

- [ ] **Step 1: Extend tests**

Replace `desktop/frontend/src/plugins/fileExplorer/languageMap.test.ts` with:

```ts
import { describe, expect, it } from "vitest";
import { languageForPath } from "./languageMap";

describe("languageForPath — existing", () => {
  it("returns javascript for .js", async () => {
    expect(await languageForPath("/x/a.js")).not.toBeNull();
  });
  it("returns null for unknown extension", async () => {
    expect(await languageForPath("/x/a.zzz")).toBeNull();
  });
  it("missing extension is null when not a known basename", async () => {
    expect(await languageForPath("/x/LICENSE")).toBeNull();
  });
});

describe("languageForPath — new extensions", () => {
  const exts = [
    "go", "rs",
    "c", "cc", "cpp", "cxx", "h", "hpp", "hh", "m", "mm",
    "java", "kt", "kts", "scala",
    "php", "sql",
    "xml", "xsd", "xsl", "plist", "svg",
    "yml", "yaml",
    "vue", "sass",
    "sh", "bash", "zsh", "fish", "ksh",
    "toml", "rb", "lua",
    "ini", "properties", "conf",
    "diff", "patch", "swift",
  ];
  for (const ext of exts) {
    it(`returns a language for .${ext}`, async () => {
      expect(await languageForPath(`/x/file.${ext}`)).not.toBeNull();
    });
  }
});

describe("languageForPath — basename matches", () => {
  for (const base of ["Dockerfile", "Gemfile", "Rakefile", "Makefile", "GNUmakefile"]) {
    it(`returns a language for basename ${base}`, async () => {
      expect(await languageForPath(`/x/${base}`)).not.toBeNull();
    });
  }
});

describe("languageForPath — case insensitive", () => {
  it("treats uppercase extension same as lowercase", async () => {
    expect(await languageForPath("/x/main.GO")).not.toBeNull();
    expect(await languageForPath("/x/data.YAML")).not.toBeNull();
  });
});
```

- [ ] **Step 2: Run, verify failures**

Run: `cd desktop/frontend && npm test -- --run languageMap`
Expected: many failures (existing impl returns null for the new exts).

- [ ] **Step 3: Rewrite `languageMap.ts`**

Replace `desktop/frontend/src/plugins/fileExplorer/languageMap.ts`:

```ts
import type { Extension } from "@codemirror/state";

// Each entry is a dynamic import so the language pack joins the file-explorer
// chunk only when actually needed. Vite splits each `await import(...)` into
// its own chunk root.

function basenameOf(path: string): string {
  const i = path.lastIndexOf("/");
  return i >= 0 ? path.slice(i + 1) : path;
}

async function streamFrom(modeImport: Promise<unknown>, modeKey: string): Promise<Extension> {
  const [{ StreamLanguage }, mod] = await Promise.all([
    import("@codemirror/language"),
    modeImport,
  ]);
  const mode = (mod as Record<string, unknown>)[modeKey];
  return StreamLanguage.define(mode as Parameters<typeof StreamLanguage.define>[0]);
}

export async function languageForPath(path: string): Promise<Extension | null> {
  const base = basenameOf(path);
  // Basename matches (no extension or special).
  switch (base) {
    case "Dockerfile":
      return streamFrom(import("@codemirror/legacy-modes/mode/dockerfile"), "dockerFile");
    case "Gemfile":
    case "Rakefile":
      return streamFrom(import("@codemirror/legacy-modes/mode/ruby"), "ruby");
    case "Makefile":
    case "GNUmakefile":
      // CodeMirror 6 has no makefile mode; clike is close enough for tabs +
      // comments + strings + variables.
      return streamFrom(import("@codemirror/legacy-modes/mode/clike"), "c");
  }

  const m = /\.([A-Za-z0-9]+)$/.exec(base);
  const ext = m ? m[1].toLowerCase() : null;
  if (!ext) return null;

  switch (ext) {
    // Existing 6 — preserved verbatim.
    case "js":
    case "jsx":
    case "ts":
    case "tsx": {
      const { javascript } = await import("@codemirror/lang-javascript");
      return javascript({ typescript: ext === "ts" || ext === "tsx", jsx: ext === "jsx" || ext === "tsx" });
    }
    case "json": {
      const { json } = await import("@codemirror/lang-json");
      return json();
    }
    case "md":
    case "markdown": {
      const { markdown } = await import("@codemirror/lang-markdown");
      return markdown();
    }
    case "css":
    case "scss": {
      const { css } = await import("@codemirror/lang-css");
      return css();
    }
    case "html":
    case "htm": {
      const { html } = await import("@codemirror/lang-html");
      return html();
    }
    case "py": {
      const { python } = await import("@codemirror/lang-python");
      return python();
    }

    // New — official lang packs.
    case "go": {
      const { go } = await import("@codemirror/lang-go");
      return go();
    }
    case "rs": {
      const { rust } = await import("@codemirror/lang-rust");
      return rust();
    }
    case "c":
    case "cc":
    case "cpp":
    case "cxx":
    case "h":
    case "hpp":
    case "hh":
    case "m":
    case "mm": {
      const { cpp } = await import("@codemirror/lang-cpp");
      return cpp();
    }
    case "java": {
      const { java } = await import("@codemirror/lang-java");
      return java();
    }
    case "php": {
      const { php } = await import("@codemirror/lang-php");
      return php();
    }
    case "sql": {
      const { sql } = await import("@codemirror/lang-sql");
      return sql();
    }
    case "xml":
    case "xsd":
    case "xsl":
    case "plist":
    case "svg": {
      const { xml } = await import("@codemirror/lang-xml");
      return xml();
    }
    case "yml":
    case "yaml": {
      const { yaml } = await import("@codemirror/lang-yaml");
      return yaml();
    }
    case "vue": {
      const { vue } = await import("@codemirror/lang-vue");
      return vue();
    }
    case "sass": {
      const { sass } = await import("@codemirror/lang-sass");
      return sass();
    }

    // New — legacy modes (StreamLanguage).
    case "sh":
    case "bash":
    case "zsh":
    case "fish":
    case "ksh":
      return streamFrom(import("@codemirror/legacy-modes/mode/shell"), "shell");
    case "toml":
      return streamFrom(import("@codemirror/legacy-modes/mode/toml"), "toml");
    case "rb":
      return streamFrom(import("@codemirror/legacy-modes/mode/ruby"), "ruby");
    case "lua":
      return streamFrom(import("@codemirror/legacy-modes/mode/lua"), "lua");
    case "ini":
    case "properties":
    case "conf":
      return streamFrom(import("@codemirror/legacy-modes/mode/properties"), "properties");
    case "diff":
    case "patch":
      return streamFrom(import("@codemirror/legacy-modes/mode/diff"), "diff");
    case "swift":
      return streamFrom(import("@codemirror/legacy-modes/mode/swift"), "swift");
    case "kt":
    case "kts":
    case "scala":
      return streamFrom(import("@codemirror/legacy-modes/mode/clike"), "kotlin");

    default:
      return null;
  }
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `cd desktop/frontend && npm test -- --run languageMap`
Expected: every test passes.

- [ ] **Step 5: Verify build still type-checks**

Run: `cd desktop/frontend && npx vue-tsc --noEmit`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/languageMap.ts desktop/frontend/src/plugins/fileExplorer/languageMap.test.ts
git commit -m "feat(file-explorer): expand languageMap with 20+ languages + basename matches"
```

---

## Task 13: Verify pass

**Files:** none (smoke).

### Steps

- [ ] **Step 1: Run the full frontend test suite**

Run: `cd desktop/frontend && npm test`
Expected: all green.

- [ ] **Step 2: Run the full desktop Go test suite**

Run: `cd desktop && go test ./...`
Expected: all green.

- [ ] **Step 3: Type-check + build the frontend**

Run: `cd desktop/frontend && npm run build`
Expected: clean.

- [ ] **Step 4: Manual smoke (UI)**

Boot the desktop app (`cd desktop && wails dev` or whichever local-run command the project uses — check `desktop/wails.json` or the `run` skill). In the File Explorer panel, open a directory and verify each of:

1. A `.go` file syntax-highlights (red keywords, green strings) — confirms Task 11+12.
2. A `.yaml` file syntax-highlights — confirms YAML pack.
3. A `Dockerfile` (no extension) syntax-highlights — confirms basename match.
4. A `.png` displays inline — click to toggle to 1:1, click back — confirms Task 7.
5. A `.mp3` shows the audio player with controls and plays — confirms Task 8.
6. A `.mp4` shows the video player with controls, seek bar advances — confirms Task 8 + Range support from Task 1.
7. A `.pdf` renders inline (mac/Windows) — confirms Task 9.
8. A `.svg` opens as code; clicking "View as image" in the tab bar swaps to render; clicking back returns to code — confirms Task 10.
9. An arbitrary unknown binary (`.bin`, random bytes) shows the "Inline preview unavailable" banner with "Open in System App" button; clicking the button opens the system handler — confirms Task 4.

- [ ] **Step 5: Commit a verify note (optional)**

If anything required follow-up tweaks during the smoke, commit them; otherwise nothing to commit here.

---

## Self-Review

Before handing the plan off:

1. **Spec coverage** — every spec section maps to a task:
   - Architecture §1 dispatcher → Task 6
   - Architecture §2 asset handler → Task 1
   - Architecture §3 highlight extension → Tasks 11+12
   - Architecture §4 image preview → Task 7
   - Architecture §5 media preview → Task 8
   - Architecture §6 PDF preview → Task 9
   - Architecture §7 error matrix → Tasks 4 (BinaryBanner) + 7/8/9 (per-component error handlers)
   - Architecture §8 tests → Tasks 1, 3, 4, 5, 6, 7, 8, 9, 10, 12, 13
   - SVG dual mode (§3 + §4) → Task 10
   - Security § → Task 1 step 3 (resolve() reuse) + isolation CI keeps coverage

2. **Placeholder scan** — no "TBD"/"TODO"/"implement later" remain; every code step shows actual code.

3. **Type consistency** — `PreviewKind` union same across `previewKind.ts` and dispatcher; `ViewMode` defined once in `tabsModel.ts`; props match between dispatcher and child components (`path`, `theme`, `viewMode`, `kind`).

---
