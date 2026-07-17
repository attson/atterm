# Remote File Explorer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the desktop File Explorer work against remote sessions with the same read-only browsing, preview, media/PDF, watch, and open-external behavior as local sessions.

**Architecture:** Add additive `FS_REQUEST` / `FS_RESPONSE` / `FS_EVENT` protocol frames over the existing `/client` and `/uplink` WebSocket topology. Keep `PluginFS` local-only by extracting a desktop-local read-only filesystem helper used by both Wails `PluginFS` and the new remote uplink handler. The frontend File Explorer consumes a `FileSystemBridge` selected from active pane locality; remote bridges reuse the active `SessionConnection`.

**Tech Stack:** Go 1.23, Wails v2, Vue 3, TypeScript, Vitest, xterm.js, nhooyr WebSocket, fsnotify.

---

## Scope Check

This is one user-facing feature that crosses several layers. The plan keeps each layer independently testable and commits after each green stage:

1. Preserve and extract local filesystem behavior.
2. Add protocol types.
3. Add frontend RPC primitives.
4. Add relay request routing.
5. Add desktop host handling.
6. Refactor File Explorer to use local/remote bridges.

## File Structure

Create:

- `desktop/fsaccess.go` — desktop-local read-only filesystem helper shared by local `PluginFS` and remote host handler.
- `desktop/fsaccess_test.go` — resolver/list/read/meta/chunk/watch tests for the shared helper.
- `desktop/remote_fs.go` — dispatches `FS_REQUEST` on the owning desktop and emits `FS_RESPONSE` / `FS_EVENT`.
- `desktop/remote_fs_test.go` — permission, dispatch, watch, and chunk tests for remote host side.
- `internal/relay/fs_router.go` — request/watch routing map from `{session_id, request_id|watch_id}` to one client writer channel.
- `internal/relay/fs_router_test.go` — routing and cleanup tests.
- `desktop/frontend/src/plugins/fileExplorer/fsBridge.ts` — `FileSystemBridge` type plus local bridge adapter.
- `desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.ts` — remote bridge backed by `SessionConnection`.
- `desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.test.ts` — remote bridge tests.

Modify:

- `desktop/plugin_fs.go` — delegate path/list/read/meta/watch operations to `fsaccess`.
- `desktop/plugin_fs_server.go` — delegate path resolution to `fsaccess`.
- `desktop/uplink.go` — receive `FS_REQUEST`, call `remoteFS`, send `FS_RESPONSE` / `FS_EVENT`.
- `internal/proto/frame.go` — add frame constants and payload structs.
- `internal/relay/client_conn.go` — accept `FS_REQUEST` after attach, enforce permission, register route, forward upstream, and write targeted responses.
- `internal/relay/uplink_conn.go` — forward `FS_RESPONSE` / `FS_EVENT` to `fsRouter`.
- `desktop/frontend/src/lib/proto.ts` and `web/src/shared/ws/protocol.ts` — add protocol constants.
- `desktop/frontend/src/lib/connection.ts` — add pending request map, `sendFSRequest`, and FS event subscription.
- `desktop/frontend/src/App.vue` — provide a session connection registry and active pane locality to plugin context.
- `desktop/frontend/src/components/TerminalView.vue` — register/unregister the live `SessionConnection` in the registry.
- `desktop/frontend/src/plugins/types.ts` and `desktop/frontend/src/plugins/usePluginContext.ts` — expose `activeIsRemote` and active connection lookup.
- `desktop/frontend/src/plugins/fileExplorer/*.vue` — replace direct `platform.pluginHost!.fs` calls with selected `FileSystemBridge`.
- `docs/spec/protocol.md` — document new frame types and E2EE posture.
- `.github/scripts/check-plugin-fs-isolation.sh` — keep behavior; tests must prove it still passes.

---

### Task 1: Extract Shared Desktop Filesystem Helper

**Files:**
- Create: `desktop/fsaccess.go`
- Create: `desktop/fsaccess_test.go`
- Modify: `desktop/plugin_fs.go`
- Modify: `desktop/plugin_fs_server.go`
- Test: `desktop/plugin_fs_test.go`
- Test: `desktop/plugin_fs_server_test.go`

- [ ] **Step 1: Write failing helper tests**

Create `desktop/fsaccess_test.go`:

```go
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeFSAccess(t *testing.T) (*fsAccess, string) {
	t.Helper()
	home := t.TempDir()
	return newFSAccess([]string{home}), home
}

func TestFSAccessResolveRejectsSymlinkEscapeAndDenylist(t *testing.T) {
	fs, home := makeFSAccess(t)
	outside := t.TempDir()
	link := filepath.Join(home, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := fs.resolve(link); err == nil {
		t.Fatal("expected symlink escape rejection")
	}
	envPath := filepath.Join(home, "app", ".env.local")
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := fs.resolve(envPath)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected denylist rejection, got %v", err)
	}
}

func TestFSAccessListReadMetaAndChunk(t *testing.T) {
	fs, home := makeFSAccess(t)
	if err := os.WriteFile(filepath.Join(home, "hello.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.listDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "hello.txt" || entries[0].IsDir {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	meta, err := fs.fileMeta(filepath.Join(home, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Size != 11 || meta.IsBinary {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	content, err := fs.readFile(filepath.Join(home, "hello.txt"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if string(content.Data) != "hello" || content.TruncatedAt != 11 {
		t.Fatalf("unexpected content: %+v", content)
	}
	chunk, err := fs.readChunk(filepath.Join(home, "hello.txt"), 6, 5)
	if err != nil {
		t.Fatal(err)
	}
	if string(chunk.Data) != "world" || !chunk.EOF {
		t.Fatalf("unexpected chunk: %+v", chunk)
	}
}

func TestFSAccessWatchDirLifecycle(t *testing.T) {
	fs, home := makeFSAccess(t)
	fs.setupWatcher(context.Background(), func(string) {})
	defer fs.shutdownWatcher()
	id, err := fs.watchDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero watch id")
	}
	if err := fs.unwatchDir(id); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run helper tests and verify failure**

Run: `go test -tags webkit2_41 ./desktop/ -run 'TestFSAccess'`

Expected: FAIL with compile errors such as `undefined: fsAccess` and `undefined: newFSAccess`.

- [ ] **Step 3: Implement `fsaccess` helper**

Create `desktop/fsaccess.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const maxChunkBytes = 256 * 1024

type fileChunk struct {
	Path        string `json:"path"`
	Data        []byte `json:"data"`
	Offset      int64  `json:"offset"`
	Length      int64  `json:"length"`
	EOF         bool   `json:"eof"`
	ContentType string `json:"contentType,omitempty"`
}

type fsAccess struct {
	allowRoots []string
	watchOnce  sync.Once
	watcher    *fsnotify.Watcher
	watches    map[int64]string
	watchPaths map[string]int
	debounce   map[string]*time.Timer
	watchSeq   int64
	mu         sync.Mutex
	ctx        context.Context
	onChanged  func(string)
}

func newFSAccess(allowRoots []string) *fsAccess {
	return &fsAccess{allowRoots: append([]string(nil), allowRoots...)}
}

func (f *fsAccess) resolve(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrPathRelative
	}
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		resolved = clean
	}
	if isDenied(resolved) {
		return "", fmt.Errorf("%w: %s", ErrPathDenied, resolved)
	}
	for _, root := range f.allowRoots {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			resolvedRoot = root
		}
		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrPathForbidden, resolved)
}

func (f *fsAccess) listDir(path string) ([]DirEntry, error) {
	resolved, err := f.resolve(path)
	if err != nil {
		return nil, err
	}
	entries, err := osReadDir(resolved)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, DirEntry{Name: e.Name(), IsDir: e.IsDir(), Size: info.Size(), ModTime: info.ModTime().UnixMilli()})
	}
	return out, nil
}

func (f *fsAccess) readFile(path string, maxBytes int64) (FileContent, error) {
	if maxBytes > maxReadBytesHard {
		return FileContent{}, fmt.Errorf("plugin_fs: maxBytes %d exceeds hard cap %d", maxBytes, maxReadBytesHard)
	}
	resolved, err := f.resolve(path)
	if err != nil {
		return FileContent{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FileContent{}, err
	}
	if info.IsDir() {
		return FileContent{}, fmt.Errorf("plugin_fs: %s is a directory", resolved)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return FileContent{}, err
	}
	defer file.Close()
	readLen := info.Size()
	truncated := int64(0)
	if readLen > maxBytes {
		readLen = maxBytes
		truncated = info.Size()
	}
	data := make([]byte, readLen)
	if _, err := io.ReadFull(file, data); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return FileContent{}, err
	}
	return FileContent{Path: resolved, Data: data, IsBinary: bytesLookBinary(data), TruncatedAt: truncated}, nil
}

func (f *fsAccess) fileMeta(path string) (FileMetaInfo, error) {
	resolved, err := f.resolve(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return FileMetaInfo{Path: resolved, Size: info.Size(), ModTime: info.ModTime().UnixMilli(), IsBinary: f.probeBinary(resolved, info.IsDir())}, nil
}

func (f *fsAccess) readChunk(path string, offset int64, length int64) (fileChunk, error) {
	if length <= 0 || length > maxChunkBytes {
		length = maxChunkBytes
	}
	resolved, err := f.resolve(path)
	if err != nil {
		return fileChunk{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fileChunk{}, err
	}
	if info.IsDir() {
		return fileChunk{}, fmt.Errorf("plugin_fs: %s is a directory", resolved)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return fileChunk{}, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fileChunk{}, err
	}
	data := make([]byte, length)
	n, err := file.Read(data)
	if err != nil && !errors.Is(err, io.EOF) {
		return fileChunk{}, err
	}
	data = data[:n]
	return fileChunk{Path: resolved, Data: data, Offset: offset, Length: int64(n), EOF: offset+int64(n) >= info.Size(), ContentType: http.DetectContentType(data)}, nil
}

func (f *fsAccess) probeBinary(path string, isDir bool) bool {
	if isDir {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	probe := make([]byte, binaryProbeBytes)
	n, _ := file.Read(probe)
	return bytesLookBinary(probe[:n])
}

func bytesLookBinary(data []byte) bool {
	probe := data
	if len(probe) > binaryProbeBytes {
		probe = probe[:binaryProbeBytes]
	}
	for _, b := range probe {
		if b == 0 {
			return true
		}
	}
	return false
}

func (f *fsAccess) setupWatcher(ctx context.Context, onChanged func(string)) {
	f.watchOnce.Do(func() {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			return
		}
		f.watcher = w
		f.watches = make(map[int64]string)
		f.watchPaths = make(map[string]int)
		f.debounce = make(map[string]*time.Timer)
		f.ctx = ctx
		f.onChanged = onChanged
		go f.watcherLoop()
	})
}

func (f *fsAccess) shutdownWatcher() {
	if f.watcher != nil {
		_ = f.watcher.Close()
	}
}

func (f *fsAccess) watchDir(path string) (int64, error) {
	resolved, err := f.resolve(path)
	if err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.watcher == nil {
		return 0, fmt.Errorf("plugin_fs: watcher not available")
	}
	if len(f.watches) >= maxWatchers {
		return 0, fmt.Errorf("plugin_fs: watcher cap %d reached", maxWatchers)
	}
	if f.watchPaths[resolved] == 0 {
		if err := f.watcher.Add(resolved); err != nil {
			return 0, err
		}
	}
	f.watchSeq++
	id := f.watchSeq
	f.watches[id] = resolved
	f.watchPaths[resolved]++
	return id, nil
}

func (f *fsAccess) unwatchDir(handleID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	path, ok := f.watches[handleID]
	if !ok {
		return nil
	}
	delete(f.watches, handleID)
	f.watchPaths[path]--
	if f.watchPaths[path] <= 0 {
		delete(f.watchPaths, path)
		_ = f.watcher.Remove(path)
	}
	return nil
}

func (f *fsAccess) watcherLoop() {
	for {
		select {
		case <-f.ctx.Done():
			return
		case ev, ok := <-f.watcher.Events:
			if !ok {
				return
			}
			f.scheduleDirChanged(filepath.Dir(ev.Name))
		case _, ok := <-f.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func (f *fsAccess) scheduleDirChanged(dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.debounce[dir]; ok {
		t.Stop()
	}
	f.debounce[dir] = time.AfterFunc(debounceWindow, func() {
		f.mu.Lock()
		delete(f.debounce, dir)
		f.mu.Unlock()
		if f.onChanged != nil {
			f.onChanged(dir)
		}
	})
}
```

- [ ] **Step 4: Delegate `PluginFS` to `fsAccess`**

Modify `desktop/plugin_fs.go`:

```go
type PluginFS struct {
	access *fsAccess
}

func (p *PluginFS) resolve(path string) (string, error) {
	return p.access.resolve(path)
}

func (p *PluginFS) ListDir(path string) ([]DirEntry, error) {
	return p.access.listDir(path)
}

func (p *PluginFS) ReadFile(path string, maxBytes int64) (FileContent, error) {
	return p.access.readFile(path, maxBytes)
}

func (p *PluginFS) FileMeta(path string) (FileMetaInfo, error) {
	return p.access.fileMeta(path)
}

func NewPluginFS() *PluginFS {
	home, _ := os.UserHomeDir()
	return &PluginFS{access: newFSAccess([]string{home})}
}

func (p *PluginFS) setupWatcher(ctx context.Context) {
	p.access.setupWatcher(ctx, func(dir string) {
		wailsruntime.EventsEmit(ctx, "plugin-fs:dir-changed", dir)
	})
}

func (p *PluginFS) shutdownWatcher() { p.access.shutdownWatcher() }
func (p *PluginFS) WatchDir(path string) (int64, error) { return p.access.watchDir(path) }
func (p *PluginFS) UnwatchDir(handleID int64) error { return p.access.unwatchDir(handleID) }
```

Update tests that construct `PluginFS` directly:

```go
fs := &PluginFS{access: newFSAccess([]string{home})}
```

Remove the watcher fields and watcher methods that moved to `fsAccess` from `PluginFS`.

- [ ] **Step 5: Run local filesystem tests**

Run: `go test -tags webkit2_41 ./desktop/ -run 'TestFSAccess|TestResolve|TestListDir|TestReadFile|TestFileMeta|TestWatch|TestPluginFS'`

Expected: PASS.

- [ ] **Step 6: Run isolation guard**

Run: `./.github/scripts/check-plugin-fs-isolation.sh`

Expected: `ok: PluginFS isolation preserved`.

- [ ] **Step 7: Commit**

```bash
git add desktop/fsaccess.go desktop/fsaccess_test.go desktop/plugin_fs.go desktop/plugin_fs_test.go desktop/plugin_fs_server.go
git commit -m "refactor(plugin-fs): extract shared read-only fs access"
```

---

### Task 2: Add Protocol Frame Types and Payload Structs

**Files:**
- Modify: `internal/proto/frame.go`
- Modify: `desktop/frontend/src/lib/proto.ts`
- Modify: `web/src/shared/ws/protocol.ts`
- Modify: `docs/spec/protocol.md`
- Test: `internal/proto/frame_test.go`

- [ ] **Step 1: Add failing protocol test**

Append to `internal/proto/frame_test.go`:

```go
func TestFSFrameTypeValues(t *testing.T) {
	if TypeFSRequest != 0x38 {
		t.Fatalf("TypeFSRequest = 0x%02x, want 0x38", TypeFSRequest)
	}
	if TypeFSResponse != 0x39 {
		t.Fatalf("TypeFSResponse = 0x%02x, want 0x39", TypeFSResponse)
	}
	if TypeFSEvent != 0x3a {
		t.Fatalf("TypeFSEvent = 0x%02x, want 0x3a", TypeFSEvent)
	}
}
```

- [ ] **Step 2: Run protocol test and verify failure**

Run: `go test ./internal/proto -run TestFSFrameTypeValues`

Expected: FAIL with `undefined: TypeFSRequest`.

- [ ] **Step 3: Add Go protocol constants and payloads**

Modify `internal/proto/frame.go`:

```go
TypeFSRequest  Type = 0x38 // client -> relay -> desktop uplink (remote file explorer)
TypeFSResponse Type = 0x39 // desktop uplink -> relay -> requester client
TypeFSEvent    Type = 0x3a // desktop uplink -> relay -> requester client
```

Add payload structs after `PasteFilePayload`:

```go
type FSRequestPayload struct {
	RequestID string `json:"request_id"`
	Op        string `json:"op"`
	Path      string `json:"path,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
	Offset    int64  `json:"offset,omitempty"`
	Length    int64  `json:"length,omitempty"`
	WatchID   string `json:"watch_id,omitempty"`
}

type FSChunkPayload struct {
	Path        string `json:"path"`
	Data        []byte `json:"data"`
	Offset      int64  `json:"offset"`
	Length      int64  `json:"length"`
	EOF         bool   `json:"eof"`
	ContentType string `json:"contentType,omitempty"`
}

type FSResponsePayload struct {
	RequestID string        `json:"request_id"`
	OK        bool          `json:"ok"`
	Error     string        `json:"error,omitempty"`
	Entries   []DirEntry    `json:"entries,omitempty"`
	Meta      *FileMetaInfo `json:"meta,omitempty"`
	Content   *FileContent  `json:"content,omitempty"`
	Chunk     *FSChunkPayload `json:"chunk,omitempty"`
	WatchID   string        `json:"watch_id,omitempty"`
}

type FSEventPayload struct {
	WatchID string `json:"watch_id"`
	Path    string `json:"path"`
	Event   string `json:"event"`
}
```

If `DirEntry`, `FileMetaInfo`, or `FileContent` are currently desktop-only, define protocol equivalents in `internal/proto/frame.go` with the same JSON field names:

```go
type DirEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"modTime,omitempty"`
}

type FileContent struct {
	Path        string `json:"path"`
	Data        []byte `json:"data"`
	IsBinary    bool   `json:"isBinary"`
	TruncatedAt int64  `json:"truncatedAt,omitempty"`
}

type FileMetaInfo struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"`
	IsBinary bool   `json:"isBinary"`
}
```

- [ ] **Step 4: Update TypeScript protocol constants**

Modify `desktop/frontend/src/lib/proto.ts`:

```ts
FS_REQUEST: 0x38,
FS_RESPONSE: 0x39,
FS_EVENT: 0x3a,
```

Modify `web/src/shared/ws/protocol.ts` with the same constants.

- [ ] **Step 5: Document protocol**

In `docs/spec/protocol.md`, add the three constants to the enum block and add a new section:

```markdown
### `FS_REQUEST` (0x38) / `FS_RESPONSE` (0x39) / `FS_EVENT` (0x3a) — remote file explorer

Desktop File Explorer remote filesystem RPC. `FS_REQUEST` flows client → relay → owning desktop uplink. `FS_RESPONSE` and `FS_EVENT` flow owning desktop uplink → relay → only the requester client.

Remote filesystem access requires `remote_permission=full`; read-only operations do not require driver status, while `open_external` also requires the requester to be the current driver.

The relay routes by `session_id` and `request_id` / `watch_id` and does not broadcast file payloads. This first implementation is authenticated and owner-scoped but not end-to-end encrypted; the relay carries `content` and `chunk` bytes in plaintext.
```

- [ ] **Step 6: Run protocol checks**

Run: `go test ./internal/proto -run TestFSFrameTypeValues`

Expected: PASS.

Run: `cd web && npm run test:contract`

Expected: PASS. If fixtures fail because protocol fixtures include frame type lists, regenerate them using the repository's existing fixture command and include the changed fixture files in the commit.

- [ ] **Step 7: Commit**

```bash
git add internal/proto/frame.go internal/proto/frame_test.go desktop/frontend/src/lib/proto.ts web/src/shared/ws/protocol.ts docs/spec/protocol.md
git commit -m "feat(proto): add remote file explorer frames"
```

---

### Task 3: Add Frontend FS RPC to SessionConnection

**Files:**
- Modify: `desktop/frontend/src/lib/connection.ts`
- Test: `desktop/frontend/src/lib/connection.test.ts`

- [ ] **Step 1: Add failing connection RPC tests**

Create or extend `desktop/frontend/src/lib/connection.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { SessionConnection } from "./connection";
import { TYPE, decodeFrame, encodeFrame, encodeText, uuidParse } from "./proto";

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static OPEN = 1;
  static CONNECTING = 0;
  readyState = FakeWebSocket.CONNECTING;
  binaryType = "arraybuffer";
  onopen: (() => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  sent: Uint8Array[] = [];
  constructor(public url: string, public protocols?: string[]) {
    FakeWebSocket.instances.push(this);
  }
  send(data: Uint8Array) { this.sent.push(data); }
  close() { this.onclose?.(); }
  open() { this.readyState = FakeWebSocket.OPEN; this.onopen?.(); }
  receive(data: Uint8Array) { this.onmessage?.({ data: data.buffer } as MessageEvent); }
}

describe("SessionConnection FS RPC", () => {
  const realWS = globalThis.WebSocket;
  const sid = "11111111-1111-4111-8111-111111111111";

  beforeEach(() => {
    FakeWebSocket.instances = [];
    (globalThis as any).WebSocket = FakeWebSocket;
  });
  afterEach(() => {
    (globalThis as any).WebSocket = realWS;
  });

  it("sends FS_REQUEST and resolves matching FS_RESPONSE", async () => {
    const conn = new SessionConnection({ url: "ws://relay", session_token: "tok" }, sid);
    conn.attach();
    const ws = FakeWebSocket.instances[0]!;
    ws.open();

    const pending = conn.sendFSRequest({ op: "file_meta", path: "/tmp/a" });
    const req = decodeFrame(ws.sent[1]!);
    expect(req.type).toBe(TYPE.FS_REQUEST);
    const body = JSON.parse(new TextDecoder().decode(req.payload));
    expect(body.op).toBe("file_meta");
    expect(body.request_id).toMatch(/^fs-/);

    ws.receive(encodeFrame(TYPE.FS_RESPONSE, uuidParse(sid), encodeText(JSON.stringify({
      request_id: body.request_id,
      ok: true,
      meta: { path: "/tmp/a", size: 1, modTime: 2, isBinary: false },
    }))));

    await expect(pending).resolves.toMatchObject({ ok: true, meta: { path: "/tmp/a" } });
  });

  it("emits FS_EVENT to registered listeners", () => {
    const conn = new SessionConnection({ url: "ws://relay", session_token: "tok" }, sid);
    const seen = vi.fn();
    conn.onFSEvent(seen);
    conn.attach();
    const ws = FakeWebSocket.instances[0]!;
    ws.open();
    ws.receive(encodeFrame(TYPE.FS_EVENT, uuidParse(sid), encodeText(JSON.stringify({
      watch_id: "w1",
      path: "/tmp",
      event: "dir_changed",
    }))));
    expect(seen).toHaveBeenCalledWith({ watch_id: "w1", path: "/tmp", event: "dir_changed" });
  });
});
```

- [ ] **Step 2: Run tests and verify failure**

Run: `cd desktop/frontend && npm test -- src/lib/connection.test.ts`

Expected: FAIL with `TYPE.FS_REQUEST` undefined and `sendFSRequest` not a function.

- [ ] **Step 3: Implement RPC in `connection.ts`**

Add types:

```ts
export interface FSRequest {
  request_id?: string;
  op: "list_dir" | "file_meta" | "read_file" | "read_chunk" | "watch_dir" | "unwatch_dir" | "open_external";
  path?: string;
  max_bytes?: number;
  offset?: number;
  length?: number;
  watch_id?: string;
}

export interface FSResponse {
  request_id: string;
  ok: boolean;
  error?: string;
  entries?: unknown[];
  meta?: unknown;
  content?: unknown;
  chunk?: unknown;
  watch_id?: string;
}

export interface FSEvent {
  watch_id: string;
  path: string;
  event: "dir_changed";
}
```

Add class fields:

```ts
private fsSeq = 0;
private fsPending = new Map<string, { resolve: (r: FSResponse) => void; reject: (e: Error) => void; timer: number }>();
private fsEventHandlers = new Set<(event: FSEvent) => void>();
```

Add methods:

```ts
sendFSRequest(req: FSRequest, timeoutMs = 15000): Promise<FSResponse> {
  if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
    return Promise.reject(new Error("remote filesystem connection is not open"));
  }
  const requestID = req.request_id ?? `fs-${Date.now()}-${++this.fsSeq}`;
  const payload = { ...req, request_id: requestID };
  const promise = new Promise<FSResponse>((resolve, reject) => {
    const timer = window.setTimeout(() => {
      this.fsPending.delete(requestID);
      reject(new Error("remote filesystem request timed out"));
    }, timeoutMs);
    this.fsPending.set(requestID, { resolve, reject, timer });
  });
  this.ws.send(encodeFrame(TYPE.FS_REQUEST, this.sidBytes, encodeText(JSON.stringify(payload))));
  return promise;
}

onFSEvent(handler: (event: FSEvent) => void): () => void {
  this.fsEventHandlers.add(handler);
  return () => this.fsEventHandlers.delete(handler);
}

private rejectPendingFS(reason: string): void {
  for (const [id, pending] of this.fsPending) {
    window.clearTimeout(pending.timer);
    pending.reject(new Error(reason));
    this.fsPending.delete(id);
  }
}
```

Handle frames in `ws.onmessage`:

```ts
} else if (f.type === TYPE.FS_RESPONSE) {
  try {
    const resp = JSON.parse(decodeText(f.payload)) as FSResponse;
    const pending = this.fsPending.get(resp.request_id);
    if (!pending) return;
    window.clearTimeout(pending.timer);
    this.fsPending.delete(resp.request_id);
    pending.resolve(resp);
  } catch {
    /* ignore malformed fs response */
  }
} else if (f.type === TYPE.FS_EVENT) {
  try {
    const event = JSON.parse(decodeText(f.payload)) as FSEvent;
    for (const handler of this.fsEventHandlers) handler(event);
  } catch {
    /* ignore malformed fs event */
  }
}
```

Call `this.rejectPendingFS("remote filesystem connection closed")` in `detach()` and `ws.onclose`.

- [ ] **Step 4: Run connection tests**

Run: `cd desktop/frontend && npm test -- src/lib/connection.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/connection.ts desktop/frontend/src/lib/connection.test.ts
git commit -m "feat(frontend): add session filesystem rpc"
```

---

### Task 4: Add FileSystemBridge and RemoteSessionFSClient

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/fsBridge.ts`
- Create: `desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.ts`
- Create: `desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.test.ts`
- Modify: `desktop/frontend/src/platform/types.ts`

- [ ] **Step 1: Write failing bridge tests**

Create `desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { createRemoteSessionFS } from "./remoteSessionFS";

describe("RemoteSessionFSClient", () => {
  const urls: string[] = [];
  beforeEach(() => {
    vi.spyOn(URL, "createObjectURL").mockImplementation(() => {
      const url = `blob:test-${urls.length}`;
      urls.push(url);
      return url;
    });
    vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
    urls.length = 0;
  });

  it("maps listDir through FS_REQUEST", async () => {
    const conn = { sendFSRequest: vi.fn().mockResolvedValue({ ok: true, entries: [{ name: "a", isDir: false }] }) };
    const fs = createRemoteSessionFS(conn as any);
    await expect(fs.listDir("/x")).resolves.toEqual([{ name: "a", isDir: false }]);
    expect(conn.sendFSRequest).toHaveBeenCalledWith({ op: "list_dir", path: "/x" });
  });

  it("throws response errors", async () => {
    const conn = { sendFSRequest: vi.fn().mockResolvedValue({ ok: false, error: "permission_denied" }) };
    const fs = createRemoteSessionFS(conn as any);
    await expect(fs.fileMeta("/x")).rejects.toThrow("permission_denied");
  });

  it("builds blob URLs from read chunks and revokes old URLs", async () => {
    const conn = { sendFSRequest: vi.fn()
      .mockResolvedValueOnce({ ok: true, chunk: { data: btoa("abc"), offset: 0, length: 3, eof: true, contentType: "text/plain" } }) };
    const fs = createRemoteSessionFS(conn as any);
    const url = await fs.assetUrlFor("/x.txt");
    expect(url).toBe("blob:test-0");
    fs.revokeAssetUrl("/x.txt");
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:test-0");
  });
});
```

- [ ] **Step 2: Run tests and verify failure**

Run: `cd desktop/frontend && npm test -- src/plugins/fileExplorer/remoteSessionFS.test.ts`

Expected: FAIL with missing module `./remoteSessionFS`.

- [ ] **Step 3: Add bridge type and local adapter**

Create `desktop/frontend/src/plugins/fileExplorer/fsBridge.ts`:

```ts
import type { DirEntry, FileContent, FileMetaInfo, PluginHostBridge } from "../../platform/types";

export interface FileSystemBridge {
  readonly identity: string;
  listDir(path: string): Promise<DirEntry[]>;
  watchDir(path: string): Promise<number | string>;
  unwatchDir(id: number | string): Promise<void>;
  readFile(path: string, maxBytes?: number): Promise<FileContent>;
  fileMeta(path: string): Promise<FileMetaInfo>;
  openExternal(path: string): Promise<void>;
  assetUrlFor(path: string): string | Promise<string>;
  revokeAssetUrl?(path: string): void;
}

export function createLocalFSBridge(pluginHost: PluginHostBridge): FileSystemBridge {
  return {
    identity: "local",
    listDir: pluginHost.fs.listDir,
    watchDir: pluginHost.fs.watchDir,
    unwatchDir: pluginHost.fs.unwatchDir as (id: number | string) => Promise<void>,
    readFile: pluginHost.fs.readFile,
    fileMeta: pluginHost.fs.fileMeta,
    openExternal: pluginHost.fs.openExternal,
    assetUrlFor: pluginHost.fs.assetUrlFor,
  };
}
```

- [ ] **Step 4: Implement remote bridge**

Create `desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.ts`:

```ts
import type { SessionConnection } from "../../lib/connection";
import type { DirEntry, FileContent, FileMetaInfo } from "../../platform/types";
import type { FileSystemBridge } from "./fsBridge";

const REMOTE_ASSET_MAX = 50 * 1024 * 1024;
const REMOTE_CHUNK = 256 * 1024;

function decodeB64(data: string): Uint8Array {
  const bin = atob(data || "");
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function ensureOK(resp: any): any {
  if (!resp?.ok) throw new Error(resp?.error || "remote filesystem request failed");
  return resp;
}

export interface RemoteFileSystemBridge extends FileSystemBridge {
  assetUrlFor(path: string): Promise<string>;
}

export function createRemoteSessionFS(conn: SessionConnection, identity = "remote"): RemoteFileSystemBridge {
  const objectURLs = new Map<string, string>();
  return {
    identity,
    async listDir(path: string): Promise<DirEntry[]> {
      const resp = ensureOK(await conn.sendFSRequest({ op: "list_dir", path }));
      return (resp.entries ?? []) as DirEntry[];
    },
    async watchDir(path: string): Promise<string> {
      const resp = ensureOK(await conn.sendFSRequest({ op: "watch_dir", path }));
      return String(resp.watch_id ?? "");
    },
    async unwatchDir(id: number | string): Promise<void> {
      ensureOK(await conn.sendFSRequest({ op: "unwatch_dir", watch_id: String(id) }));
    },
    async readFile(path: string, maxBytes = 2 * 1024 * 1024): Promise<FileContent> {
      const resp = ensureOK(await conn.sendFSRequest({ op: "read_file", path, max_bytes: maxBytes }));
      return resp.content as FileContent;
    },
    async fileMeta(path: string): Promise<FileMetaInfo> {
      const resp = ensureOK(await conn.sendFSRequest({ op: "file_meta", path }));
      return resp.meta as FileMetaInfo;
    },
    async openExternal(path: string): Promise<void> {
      ensureOK(await conn.sendFSRequest({ op: "open_external", path }));
    },
    async assetUrlFor(path: string): Promise<string> {
      const old = objectURLs.get(path);
      if (old) return old;
      const chunks: Uint8Array[] = [];
      let offset = 0;
      let total = 0;
      let contentType = "application/octet-stream";
      for (;;) {
        if (total >= REMOTE_ASSET_MAX) throw new Error("remote asset too large");
        const resp = ensureOK(await conn.sendFSRequest({ op: "read_chunk", path, offset, length: REMOTE_CHUNK }));
        const chunk = resp.chunk;
        const data = decodeB64(chunk?.data ?? "");
        chunks.push(data);
        total += data.length;
        offset += data.length;
        if (chunk?.contentType) contentType = chunk.contentType;
        if (chunk?.eof || data.length === 0) break;
      }
      const blob = new Blob(chunks, { type: contentType });
      const url = URL.createObjectURL(blob);
      objectURLs.set(path, url);
      return url;
    },
    revokeAssetUrl(path: string): void {
      const url = objectURLs.get(path);
      if (!url) return;
      URL.revokeObjectURL(url);
      objectURLs.delete(path);
    },
  };
}
```

- [ ] **Step 5: Run bridge tests**

Run: `cd desktop/frontend && npm test -- src/plugins/fileExplorer/remoteSessionFS.test.ts`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/fsBridge.ts desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.ts desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.test.ts
git commit -m "feat(file-explorer): add filesystem bridge abstraction"
```

---

### Task 5: Expose Active SessionConnection to Plugins

**Files:**
- Modify: `desktop/frontend/src/plugins/types.ts`
- Modify: `desktop/frontend/src/plugins/usePluginContext.ts`
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Test: `desktop/frontend/src/components/TerminalView.test.ts`
- Test: `desktop/frontend/src/plugins/usePluginContext.test.ts`

- [ ] **Step 1: Add failing source-level tests**

Create `desktop/frontend/src/plugins/usePluginContext.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { ref } from "vue";
import { createPluginContext } from "./usePluginContext";

describe("createPluginContext remote filesystem inputs", () => {
  it("exposes activeIsRemote and activeSessionConnection", () => {
    const pane = ref({ sessionId: "s1", remote: true });
    const conn = { sendFSRequest: async () => ({ ok: true }) };
    const ctx = createPluginContext({
      activePane: pane as any,
      endpointForPane: () => ({ url: "ws://x", session_token: "t" }),
      sessionInfoForPane: () => null,
      sessionConnectionForPane: () => conn as any,
      sendToSession: () => {},
      showToast: () => {},
      terminalThemeId: ref("classic"),
    });
    expect(ctx.activeIsRemote.value).toBe(true);
    expect(ctx.activeSessionConnection.value).toBe(conn);
  });
});
```

Append to `desktop/frontend/src/components/TerminalView.test.ts`:

```ts
describe("TerminalView plugin connection registry", () => {
  test("registers SessionConnection for plugin filesystem reuse", () => {
    expect(source).toContain("atterm:pluginSessionConnections");
    expect(source).toMatch(/pluginSessionConnections\?\.set\(props\.sessionId,\s*conn/);
    expect(source).toMatch(/pluginSessionConnections\?\.delete\(props\.sessionId\)/);
  });
});
```

- [ ] **Step 2: Run tests and verify failure**

Run: `cd desktop/frontend && npm test -- src/plugins/usePluginContext.test.ts src/components/TerminalView.test.ts`

Expected: FAIL with `sessionConnectionForPane` not accepted and missing registry strings.

- [ ] **Step 3: Update plugin types**

Modify `desktop/frontend/src/plugins/types.ts`:

```ts
import type { SessionConnection } from "../lib/connection";

export interface PluginContext {
  activePane: Ref<Pane | null>;
  activeSessionId: ComputedRef<string | null>;
  activeEndpoint: ComputedRef<Endpoint | null>;
  activeCwd: ComputedRef<string | null>;
  activeIsRemote: ComputedRef<boolean>;
  activeSessionConnection: ComputedRef<SessionConnection | null>;
  terminalThemeId: ComputedRef<string>;
  send: (text: string) => void;
  showToast: (msg: string) => void;
}
```

- [ ] **Step 4: Update context creator**

Modify `desktop/frontend/src/plugins/usePluginContext.ts`:

```ts
import type { SessionConnection } from "../lib/connection";

export interface PluginContextInputs {
  activePane: Ref<Pane | null>;
  endpointForPane: (pane: Pane) => Endpoint | null;
  sessionInfoForPane: (pane: Pane) => SessionInfo | null;
  sessionConnectionForPane: (pane: Pane) => SessionConnection | null;
  sendToSession: (sessionId: string, endpoint: Endpoint, text: string) => void;
  showToast: (msg: string) => void;
  terminalThemeId: Ref<string> | ComputedRef<string>;
}

const activeIsRemote = computed(() => !!inputs.activePane.value?.remote);
const activeSessionConnection = computed<SessionConnection | null>(() => {
  const p = inputs.activePane.value;
  return p ? inputs.sessionConnectionForPane(p) : null;
});
```

Return `activeIsRemote` and `activeSessionConnection`.

- [ ] **Step 5: Provide connection registry in App and TerminalView**

Modify `desktop/frontend/src/App.vue` near `pluginInputSenders`:

```ts
const pluginSessionConnections = new Map<string, SessionConnection>();
provide("atterm:pluginSessionConnections", pluginSessionConnections);
```

Pass to `createPluginContext`:

```ts
sessionConnectionForPane: (pane) => {
  if (!pane.sessionId) return null;
  return pluginSessionConnections.get(pane.sessionId) ?? null;
},
```

Modify `desktop/frontend/src/components/TerminalView.vue`:

```ts
const pluginSessionConnections = inject<Map<string, SessionConnection> | null>(
  "atterm:pluginSessionConnections",
  null,
);
```

After `conn.attach()`:

```ts
pluginSessionConnections?.set(props.sessionId, conn);
```

In `onBeforeUnmount`:

```ts
pluginSessionConnections?.delete(props.sessionId);
```

- [ ] **Step 6: Run context tests**

Run: `cd desktop/frontend && npm test -- src/plugins/usePluginContext.test.ts src/components/TerminalView.test.ts`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/plugins/types.ts desktop/frontend/src/plugins/usePluginContext.ts desktop/frontend/src/plugins/usePluginContext.test.ts desktop/frontend/src/App.vue desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git commit -m "feat(plugins): expose active session connection"
```

---

### Task 6: Refactor File Explorer to Use Selected FS Bridge

**Files:**
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileTree.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/CodeViewer.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/MarkdownPreview.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/ImagePreview.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/MediaPreview.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/PdfPreview.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/BinaryBanner.vue`
- Test: existing File Explorer tests

- [ ] **Step 1: Add failing test for remote bridge selection**

Create `desktop/frontend/src/plugins/fileExplorer/FileExplorer.remote.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { computed, ref } from "vue";
import FileExplorer from "./FileExplorer.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

vi.mock("./remoteSessionFS", () => ({
  createRemoteSessionFS: vi.fn(() => ({
    identity: "remote:s1",
    listDir: vi.fn().mockResolvedValue([{ name: "remote.txt", isDir: false }]),
    watchDir: vi.fn().mockResolvedValue("w1"),
    unwatchDir: vi.fn().mockResolvedValue(undefined),
    readFile: vi.fn(),
    fileMeta: vi.fn(),
    openExternal: vi.fn(),
    assetUrlFor: vi.fn(),
  })),
}));

describe("FileExplorer remote bridge", () => {
  beforeEach(() => {
    const platform = createFakePlatform();
    platform.pluginHost!.getPluginConfig = vi.fn().mockResolvedValue({
      fileExplorer: { enabled: true, panelWidthPx: 380, panelCollapsed: false, innerTreeRatio: 0.3, showHidden: false, showLineNumbers: false },
      translate: { enabled: false, provider: "", baseUrl: "", apiKey: "", model: "", defaultTargetLang: "" },
      shortcuts: { bindings: {} },
    } as any);
    __setPlatformForTests(platform);
  });
  afterEach(() => __setPlatformForTests(null));

  it("uses remote filesystem when active pane is remote", async () => {
    const context = {
      activePane: ref({ sessionId: "s1", remote: true }),
      activeSessionId: computed(() => "s1"),
      activeEndpoint: computed(() => ({ url: "ws://x", session_token: "t" })),
      activeCwd: computed(() => "/remote"),
      activeIsRemote: computed(() => true),
      activeSessionConnection: computed(() => ({ sendFSRequest: vi.fn() })),
      terminalThemeId: computed(() => "classic"),
      send: vi.fn(),
      showToast: vi.fn(),
    };
    const w = mount(FileExplorer, { props: { context: context as any } });
    await flushPromises();
    expect(w.text()).toContain("remote.txt");
  });
});
```

- [ ] **Step 2: Run test and verify failure**

Run: `cd desktop/frontend && npm test -- src/plugins/fileExplorer/FileExplorer.remote.test.ts`

Expected: FAIL because File Explorer still calls local `platform.pluginHost.fs`.

- [ ] **Step 3: Select bridge in FileExplorer**

Modify `FileExplorer.vue`:

```ts
import { createLocalFSBridge, type FileSystemBridge } from "./fsBridge";
import { createRemoteSessionFS } from "./remoteSessionFS";
import { usePlatform } from "../../platform";

const platform = usePlatform();

const fsBridge = computed<FileSystemBridge | null>(() => {
  if (!props.context.activeIsRemote.value) {
    return platform.pluginHost ? createLocalFSBridge(platform.pluginHost) : null;
  }
  const conn = props.context.activeSessionConnection.value;
  const sid = props.context.activeSessionId.value;
  if (!conn || !sid) return null;
  return createRemoteSessionFS(conn, `remote:${sid}`);
});
```

Pass `fsBridge` to `FileTree` and `FileEditor`. Reset state on identity change:

```ts
watch(() => fsBridge.value?.identity, () => {
  pinned.value = null;
  tabsState.value = { tabs: [], activeIdx: -1 };
});
```

- [ ] **Step 4: Refactor leaf components**

In each leaf component, remove `usePlatform().pluginHost!.fs` and add a prop:

```ts
import type { FileSystemBridge } from "./fsBridge";
const props = defineProps<{ fs: FileSystemBridge; path: string; ... }>();
```

Use `props.fs` for `listDir`, `watchDir`, `unwatchDir`, `readFile`, `fileMeta`, `openExternal`, and `assetUrlFor`.

For `ImagePreview.vue`, `MediaPreview.vue`, and `PdfPreview.vue`, support async asset URLs:

```ts
const src = ref("");
watch(() => props.path, async (path, oldPath) => {
  if (oldPath) props.fs.revokeAssetUrl?.(oldPath);
  src.value = await props.fs.assetUrlFor(path);
}, { immediate: true });
onBeforeUnmount(() => props.fs.revokeAssetUrl?.(props.path));
```

- [ ] **Step 5: Run File Explorer tests**

Run: `cd desktop/frontend && npm test -- src/plugins/fileExplorer`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer
git commit -m "feat(file-explorer): select local or remote filesystem"
```

---

### Task 7: Add Relay FS Router and Client-Side Forwarding

**Files:**
- Create: `internal/relay/fs_router.go`
- Create: `internal/relay/fs_router_test.go`
- Modify: `internal/relay/client_conn.go`
- Modify: `internal/relay/uplink_conn.go`
- Test: `internal/relay/*fs*_test.go`

- [ ] **Step 1: Write failing router tests**

Create `internal/relay/fs_router_test.go`:

```go
package relay

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestFSRouterRoutesResponseToRequesterOnly(t *testing.T) {
	r := newFSRouter()
	sid := uuid.New()
	a := make(chan proto.Frame, 1)
	b := make(chan proto.Frame, 1)
	r.registerRequest(sid, "r1", a)
	r.registerRequest(sid, "r2", b)
	r.routeResponse(proto.Frame{Type: proto.TypeFSResponse, SessionID: sid, Payload: []byte(`{"request_id":"r1","ok":true}`)})
	select {
	case got := <-a:
		if got.Type != proto.TypeFSResponse {
			t.Fatalf("unexpected frame type %v", got.Type)
		}
	default:
		t.Fatal("requester did not receive response")
	}
	select {
	case got := <-b:
		t.Fatalf("other requester received response: %+v", got)
	default:
	}
}

func TestFSRouterRoutesEventsByWatchID(t *testing.T) {
	r := newFSRouter()
	sid := uuid.New()
	out := make(chan proto.Frame, 1)
	r.registerWatch(sid, "w1", out)
	r.routeEvent(proto.Frame{Type: proto.TypeFSEvent, SessionID: sid, Payload: []byte(`{"watch_id":"w1","path":"/x","event":"dir_changed"}`)})
	select {
	case <-out:
	default:
		t.Fatal("watch owner did not receive event")
	}
}
```

- [ ] **Step 2: Run router tests and verify failure**

Run: `go test ./internal/relay -run TestFSRouter`

Expected: FAIL with `undefined: newFSRouter`.

- [ ] **Step 3: Implement router**

Create `internal/relay/fs_router.go`:

```go
package relay

import (
	"encoding/json"
	"sync"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

type fsRouteKey struct {
	sessionID string
	id        string
}

type fsRouter struct {
	mu       sync.Mutex
	requests map[fsRouteKey]chan<- proto.Frame
	watches  map[fsRouteKey]chan<- proto.Frame
}

func newFSRouter() *fsRouter {
	return &fsRouter{
		requests: make(map[fsRouteKey]chan<- proto.Frame),
		watches:  make(map[fsRouteKey]chan<- proto.Frame),
	}
}

func (r *fsRouter) registerRequest(sessionID uuid.UUID, requestID string, out chan<- proto.Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests[fsRouteKey{sessionID: sessionID.String(), id: requestID}] = out
}

func (r *fsRouter) registerWatch(sessionID uuid.UUID, watchID string, out chan<- proto.Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.watches[fsRouteKey{sessionID: sessionID.String(), id: watchID}] = out
}

func (r *fsRouter) unregisterClient(out chan<- proto.Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, v := range r.requests {
		if sameFrameSink(v, out) {
			delete(r.requests, k)
		}
	}
	for k, v := range r.watches {
		if sameFrameSink(v, out) {
			delete(r.watches, k)
		}
	}
}

func (r *fsRouter) routeResponse(f proto.Frame) bool {
	var p proto.FSResponsePayload
	if err := json.Unmarshal(f.Payload, &p); err != nil || p.RequestID == "" {
		return false
	}
	key := fsRouteKey{sessionID: f.SessionID.String(), id: p.RequestID}
	r.mu.Lock()
	out := r.requests[key]
	delete(r.requests, key)
	if p.WatchID != "" && p.OK {
		r.watches[fsRouteKey{sessionID: f.SessionID.String(), id: p.WatchID}] = out
	}
	r.mu.Unlock()
	if out == nil {
		return false
	}
	select {
	case out <- f:
		return true
	default:
		return false
	}
}

func (r *fsRouter) routeEvent(f proto.Frame) bool {
	var p proto.FSEventPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil || p.WatchID == "" {
		return false
	}
	r.mu.Lock()
	out := r.watches[fsRouteKey{sessionID: f.SessionID.String(), id: p.WatchID}]
	r.mu.Unlock()
	if out == nil {
		return false
	}
	select {
	case out <- f:
		return true
	default:
		return false
	}
}

func sameFrameSink(a, b chan<- proto.Frame) bool {
	return a == b
}
```

Add `fsRouter *fsRouter` to `Server` construction. If `Server` already has a constructor path, initialize it there; otherwise lazy-init in a helper `s.fsRoutes()`.

- [ ] **Step 4: Wire client targeted writer**

In `internal/relay/client_conn.go`, add a per-client targeted channel:

```go
targetedOut := make(chan proto.Frame, 16)
defer s.fsRouter.unregisterClient(targetedOut)
```

In `startWriter`, add a select case:

```go
case f := <-targetedOut:
	ctx, cancel := context.WithTimeout(writerCtx, clientWriteWait)
	err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(f))
	cancel()
	if err != nil {
		_ = c.CloseNow()
		return
	}
```

Handle `TypeFSRequest`:

```go
case proto.TypeFSRequest:
	if sess == nil {
		continue
	}
	if sessionRemotePermission(sess) != permFull {
		continue
	}
	var p proto.FSRequestPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil || p.RequestID == "" {
		continue
	}
	if p.Op == "open_external" && !sess.IsDriver(sub) {
		continue
	}
	s.fsRouter.registerRequest(sess.ID, p.RequestID, targetedOut)
	if !sess.SendInbound(f) {
		s.debugf("client fs_request_drop reason=inbound_full session=%s request_id=%s", sess.ID, p.RequestID)
	}
```

- [ ] **Step 5: Wire uplink responses**

In `internal/relay/uplink_conn.go` reader switch:

```go
case proto.TypeFSResponse:
	mu.Lock()
	ms := mirrors[f.SessionID]
	mu.Unlock()
	if ms == nil {
		continue
	}
	s.fsRouter.routeResponse(f)
case proto.TypeFSEvent:
	mu.Lock()
	ms := mirrors[f.SessionID]
	mu.Unlock()
	if ms == nil {
		continue
	}
	s.fsRouter.routeEvent(f)
```

- [ ] **Step 6: Run relay tests**

Run: `go test -tags webkit2_41 ./internal/relay -run 'TestFSRouter|TestClient|TestUplink'`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/relay/fs_router.go internal/relay/fs_router_test.go internal/relay/client_conn.go internal/relay/uplink_conn.go
git commit -m "feat(relay): route remote filesystem rpc"
```

---

### Task 8: Implement Desktop Uplink RemoteFS Host Handler

**Files:**
- Create: `desktop/remote_fs.go`
- Create: `desktop/remote_fs_test.go`
- Modify: `desktop/uplink.go`
- Test: `desktop/remote_fs_test.go`

- [ ] **Step 1: Write failing remote host tests**

Create `desktop/remote_fs_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestRemoteFSListDirAndReadChunk(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "a.txt"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	rfs := newRemoteFS(newFSAccess([]string{home}))
	sid := uuid.New()
	req := proto.FSRequestPayload{RequestID: "r1", Op: "list_dir", Path: home}
	f := rfs.handle(sid, req)
	var resp proto.FSResponsePayload
	if err := json.Unmarshal(f.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || len(resp.Entries) != 1 || resp.Entries[0].Name != "a.txt" {
		t.Fatalf("unexpected list response: %+v", resp)
	}
	req = proto.FSRequestPayload{RequestID: "r2", Op: "read_chunk", Path: filepath.Join(home, "a.txt"), Offset: 2, Length: 3}
	f = rfs.handle(sid, req)
	if err := json.Unmarshal(f.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || string(resp.Chunk.Data) != "cde" {
		t.Fatalf("unexpected chunk response: %+v", resp)
	}
}

func TestRemoteFSRejectsDeniedPath(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".env")
	if err := os.WriteFile(path, []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	rfs := newRemoteFS(newFSAccess([]string{home}))
	f := rfs.handle(uuid.New(), proto.FSRequestPayload{RequestID: "r1", Op: "file_meta", Path: path})
	var resp proto.FSResponsePayload
	if err := json.Unmarshal(f.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Error == "" {
		t.Fatalf("expected error response, got %+v", resp)
	}
}
```

- [ ] **Step 2: Run tests and verify failure**

Run: `go test -tags webkit2_41 ./desktop/ -run TestRemoteFS`

Expected: FAIL with `undefined: newRemoteFS`.

- [ ] **Step 3: Implement remote handler**

Create `desktop/remote_fs.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

type remoteFS struct {
	access *fsAccess
}

func newRemoteFS(access *fsAccess) *remoteFS {
	return &remoteFS{access: access}
}

func (r *remoteFS) handle(sessionID uuid.UUID, req proto.FSRequestPayload) proto.Frame {
	resp := proto.FSResponsePayload{RequestID: req.RequestID}
	switch req.Op {
	case "list_dir":
		entries, err := r.access.listDir(req.Path)
		if err != nil { return r.errorFrame(sessionID, req.RequestID, err) }
		resp.OK = true
		resp.Entries = toProtoDirEntries(entries)
	case "file_meta":
		meta, err := r.access.fileMeta(req.Path)
		if err != nil { return r.errorFrame(sessionID, req.RequestID, err) }
		resp.OK = true
		resp.Meta = toProtoFileMeta(meta)
	case "read_file":
		content, err := r.access.readFile(req.Path, req.MaxBytes)
		if err != nil { return r.errorFrame(sessionID, req.RequestID, err) }
		resp.OK = true
		resp.Content = toProtoFileContent(content)
	case "read_chunk":
		chunk, err := r.access.readChunk(req.Path, req.Offset, req.Length)
		if err != nil { return r.errorFrame(sessionID, req.RequestID, err) }
		resp.OK = true
		resp.Chunk = &proto.FSChunkPayload{Path: chunk.Path, Data: chunk.Data, Offset: chunk.Offset, Length: chunk.Length, EOF: chunk.EOF, ContentType: chunk.ContentType}
	case "watch_dir":
		id, err := r.access.watchDir(req.Path)
		if err != nil { return r.errorFrame(sessionID, req.RequestID, err) }
		resp.OK = true
		resp.WatchID = strconv.FormatInt(id, 10)
	case "unwatch_dir":
		id, _ := strconv.ParseInt(req.WatchID, 10, 64)
		err := r.access.unwatchDir(id)
		if err != nil { return r.errorFrame(sessionID, req.RequestID, err) }
		resp.OK = true
	case "open_external":
		_, err := r.access.resolve(req.Path)
		if err != nil { return r.errorFrame(sessionID, req.RequestID, err) }
		err = openExternalPath(req.Path)
		if err != nil { return r.errorFrame(sessionID, req.RequestID, err) }
		resp.OK = true
	default:
		return r.errorFrame(sessionID, req.RequestID, fmt.Errorf("unsupported fs op %q", req.Op))
	}
	payload, _ := json.Marshal(resp)
	return proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: payload}
}

func (r *remoteFS) errorFrame(sessionID uuid.UUID, requestID string, err error) proto.Frame {
	payload, _ := json.Marshal(proto.FSResponsePayload{RequestID: requestID, OK: false, Error: err.Error()})
	return proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: payload}
}

func toProtoDirEntries(entries []DirEntry) []proto.DirEntry {
	out := make([]proto.DirEntry, len(entries))
	for i, e := range entries {
		out[i] = proto.DirEntry{Name: e.Name, IsDir: e.IsDir, Size: e.Size, ModTime: e.ModTime}
	}
	return out
}

func toProtoFileMeta(m FileMetaInfo) *proto.FileMetaInfo {
	return &proto.FileMetaInfo{Path: m.Path, Size: m.Size, ModTime: m.ModTime, IsBinary: m.IsBinary}
}

func toProtoFileContent(c FileContent) *proto.FileContent {
	return &proto.FileContent{Path: c.Path, Data: c.Data, IsBinary: c.IsBinary, TruncatedAt: c.TruncatedAt}
}
```

Extract `openExternalPath(path string) error` from `PluginFS.OpenExternal` so both local and remote call the same OS implementation after `resolve`.

- [ ] **Step 4: Wire uplink reader**

In `desktop/uplink.go`, construct a `remoteFS` using the same access helper:

```go
remoteFS := newRemoteFS(newFSAccess([]string{mustUserHome()}))
```

If `mustUserHome()` does not exist, add:

```go
func mustUserHome() string {
	home, _ := os.UserHomeDir()
	return home
}
```

In the reader switch:

```go
case proto.TypeFSRequest:
	if normalizeRemotePermission(u.remotePermission) != proto.RemotePermissionFull {
		continue
	}
	var p proto.FSRequestPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		continue
	}
	resp := remoteFS.handle(f.SessionID, p)
	select {
	case out <- resp:
	case <-connCtx.Done():
		return nil
	}
```

For watch events, pass an `onChanged` callback into `fsAccess.setupWatcher` that emits `TypeFSEvent` frames with the server watch id. If this requires mapping resolved path to watch id, add that map inside `remoteFS`.

- [ ] **Step 5: Run remote FS tests**

Run: `go test -tags webkit2_41 ./desktop/ -run 'TestRemoteFS|TestFSAccess|TestPluginFS'`

Expected: PASS.

- [ ] **Step 6: Run isolation guard**

Run: `./.github/scripts/check-plugin-fs-isolation.sh`

Expected: `ok: PluginFS isolation preserved`.

- [ ] **Step 7: Commit**

```bash
git add desktop/remote_fs.go desktop/remote_fs_test.go desktop/uplink.go desktop/plugin_fs.go
git commit -m "feat(desktop): handle remote filesystem requests"
```

---

### Task 9: End-to-End Verification and Polish

**Files:**
- No planned source modifications. If verification fails, return to the specific task that owns the failing file and repeat that task's red/green/commit loop.
- Test: Go, desktop frontend, web contract, isolation guard.

- [ ] **Step 1: Run focused Go tests**

Run:

```bash
go test -tags webkit2_41 ./internal/proto ./internal/relay ./desktop/ -run 'TestFS|TestRemoteFS|TestPluginFS|TestClient|TestUplink'
```

Expected: PASS.

- [ ] **Step 2: Run desktop frontend tests**

Run:

```bash
cd desktop/frontend && npm test -- src/lib/connection.test.ts src/plugins/usePluginContext.test.ts src/components/TerminalView.test.ts src/plugins/fileExplorer
```

Expected: PASS.

- [ ] **Step 3: Run frontend build**

Run: `cd desktop/frontend && npm run build`

Expected: PASS with Vite build output and no TypeScript errors.

- [ ] **Step 4: Run web protocol contract tests**

Run: `cd web && npm run test:contract`

Expected: PASS.

- [ ] **Step 5: Run PluginFS isolation guard**

Run: `./.github/scripts/check-plugin-fs-isolation.sh`

Expected: `ok: PluginFS isolation preserved`.

- [ ] **Step 6: Manual desktop smoke test**

Run the desktop app:

```bash
cd desktop
wails dev -tags webkit2_41
```

Expected:

- Local File Explorer still lists active local cwd.
- Remote pane File Explorer lists the remote cwd, not the viewer machine's same path.
- Text, markdown render, image, audio/video, PDF, and binary fallback previews work for remote files.
- Expanding a watched remote directory refreshes after creating/removing a file on the remote host.
- `remote_permission=view` or `control` shows a permission error instead of listing files.

- [ ] **Step 7: Confirm clean verification state**

Run: `git status --short`

Expected: only intentional committed work remains. If any source file is modified because a verification fix was needed, stop and handle it through the owning task's explicit test and commit steps instead of making a catch-all verification commit.

---

## Final Verification Checklist

Run all of these before claiming completion:

```bash
go test -tags webkit2_41 ./internal/proto ./internal/relay ./desktop/
cd desktop/frontend && npm test
cd desktop/frontend && npm run build
cd web && npm run test:contract
./.github/scripts/check-plugin-fs-isolation.sh
```

Expected:

- All Go tests pass.
- All desktop frontend tests pass.
- Desktop frontend build passes.
- Web protocol contract tests pass.
- PluginFS isolation guard prints `ok: PluginFS isolation preserved`.

## Risk Notes for Implementers

- Do not import `PluginFS` from `desktop/uplink.go` or any `internal/` package.
- Do not broadcast `FS_RESPONSE` or `FS_EVENT`; route only to the requester.
- Do not create a second `/client` attachment for the File Explorer.
- Do not add write/edit/delete file operations.
- Keep `remote_permission=full` enforced both relay-side and desktop-host-side.
- Do not add E2EE claims for file bytes in this phase; document the plaintext relay posture honestly.
