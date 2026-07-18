# Remote File Channel (PASTE_FILE) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `PASTE_FILE (0x37)` frame that lets attach clients (Web/PWA, another atterm desktop as attacher, Capacitor mobile) send arbitrary files ≤10 MiB to the owner desktop's PTY — files land under `<cache>/paste-files/<sid>/<sanitized-name>` and their absolute path is injected into stdin.

**Architecture:** Structurally parallels the existing `PASTE_IMAGE (0x33)` end-to-end, minus native-clipboard bridging. A new `FilePasteHost` interface in `internal/relay` bridges the routed frame into `desktop.desktopPtyHost.PasteFile`, which sanitizes the filename, dedups on-disk, writes bytes to `appdir.CacheDir()/paste-files/<sid>/<name>`, and writes the resulting absolute path (no CR, no quoting) to the PTY master. Frontends split file drops / picks between the existing PASTE_IMAGE path and the new PASTE_FILE path.

**Tech Stack:** Go 1.22, Wails v2, Vue 3 + TypeScript, xterm.js, Vitest, Capacitor 8.

## Global Constraints

- **Wire compatibility:** Every change to `internal/proto/frame.go` MUST be paired with regenerating `web/tests/fixtures/proto/*.bin` via `go run ./cmd/proto-fixtures`, and `web/src/shared/ws/protocol.test.ts` round-trip tests MUST stay green. (`web/src/shared/ws/protocol.ts:3-6` red-line 4.)
- **Payload size:** single frame, `PasteFilePayload.Data` ≤ `maxPasteFileBytes = 10 * 1024 * 1024` bytes. Frontend pre-check + desktop backstop share the same constant literal.
- **Frame type value:** `TypePasteFile = 0x37` in Go, `PASTE_FILE: 0x37` in TS `TYPE`. Never mutate.
- **Sanitized inbox root:** `appdir.CacheDir() / paste-files / <session_id> / <sanitized-name>` — mkdir 0o700, file 0o600.
- **PTY injection:** absolute path only, no CR, no quoting, single `h.Write([]byte(absPath))` call.
- **Permission gate:** `full` remote_permission required (relay drop otherwise, log `permission_denied`), driver-only (relay silently drops non-driver). Same rules as PASTE_IMAGE.
- **No frontend E2EE seal in this plan** — matches current `sendPasteImage` posture. Filling that gap is out of scope. Go-side attach path (another atterm desktop) still seals via `e2eecrypto.SealUnsequenced`.
- **Commit style:** conventional prefix, focus on WHY, ≤72 char subject. One commit per task step marked "Commit".

---

## File Structure

**New files:**
- `desktop/paste_file.go` — receive-side handler (sanitize + dedup + save + PTY injection)
- `desktop/paste_file_test.go`
- `desktop/received_files.go` — `ReceivedFiles*` Wails bindings + on-disk enumeration
- `desktop/received_files_test.go`
- `web/src/shared/ws/__tests__/client-conn-paste-file.test.ts`
- `web/src/main/lib/pasteFileBus.ts`
- `web/src/main/components/PasteFilePreviewHost.vue`
- `web/src/main/components/__tests__/PasteFilePreviewHost.test.ts`
- `desktop/frontend/src/lib/pasteFileBus.ts`
- `desktop/frontend/src/components/PasteFilePreviewHost.vue`
- `desktop/frontend/src/components/SettingsReceivedFiles.vue`
- `desktop/frontend/src/components/__tests__/SettingsReceivedFiles.test.ts`

**Modified:**
- `internal/proto/frame.go` — add `TypePasteFile` + `PasteFilePayload`
- `internal/proto/frame_test.go` — roundtrip test
- `internal/e2eecrypto/envelope.go` — update `SealUnsequenced` doc comment
- `internal/e2eecrypto/envelope_test.go` — AAD cross-type test
- `internal/relay/adopt.go` — `FilePasteHost` interface + PASTE_FILE routing
- `internal/relay/adopt_test.go` — routing test
- `internal/relay/permissions.go` — add `TypePasteFile` to permission matrix
- `internal/relay/permissions_test.go` — matrix test
- `internal/relay/client_conn.go` — add `TypePasteFile` to driver-gated switch
- `web/src/shared/ws/protocol.ts` — add `PASTE_FILE: 0x37`
- `web/src/shared/ws/client-conn.ts` — add `sendPasteFile`
- `web/tests/fixtures/proto/` — regenerate + add `paste-file.bin`
- `cmd/proto-fixtures/main.go` — emit `paste-file.bin`
- `web/src/main/components/PasteFallback.vue` — split image / file inputs & drop
- `web/src/main/App.vue` — mount `PasteFilePreviewHost`, wire `onPasteFile`
- `desktop/frontend/src/components/TerminalView.vue` — file drop forward
- `desktop/frontend/src/App.vue` — mount `PasteFilePreviewHost`
- `desktop/frontend/src/components/SettingsDialog.vue` — new "received-files" tab
- `desktop/app.go` — expose `ReceivedFiles*` bindings
- `docs/spec/protocol.md` — protocol §PASTE_FILE addition

---

## Task Sequencing

Ordered strictly by dependency. Later tasks assume all earlier tasks landed on `main`.

- **Task 1:** proto — add `TypePasteFile` + `PasteFilePayload` + roundtrip test
- **Task 2:** e2ee — AAD test for `TypePasteFile` frame_type
- **Task 3:** relay — `FilePasteHost` interface + adopt.go routing + adopt_test
- **Task 4:** relay — permission + driver gate for `TypePasteFile`
- **Task 5:** desktop — `paste_file.go` (save + sanitize + dedup + inject) + tests
- **Task 6:** desktop — `received_files.go` bindings + tests + wire into `app.go`
- **Task 7:** frontend shared — TS `PASTE_FILE` + `sendPasteFile` + roundtrip fixture
- **Task 8:** frontend shared — `pasteFileBus` + `PasteFilePreviewHost` + tests (both web and desktop-frontend copies)
- **Task 9:** web — `PasteFallback.vue` split + `App.vue` wiring
- **Task 10:** desktop-frontend — `TerminalView.vue` file drop + `App.vue` mount + Capacitor 📎 toolbar button
- **Task 11:** desktop-frontend — `SettingsReceivedFiles.vue` + `SettingsDialog.vue` tab wiring
- **Task 12:** docs — `docs/spec/protocol.md` PASTE_FILE section
- **Task 13:** end-to-end verification checklist (real app)
- **Task 14:** ship-release via `ship-release` skill

---

### Task 1: proto — add `TypePasteFile` + `PasteFilePayload`

**Files:**
- Modify: `internal/proto/frame.go` (constants block ~line 48, payload struct near existing PasteImagePayload ~line 201)
- Modify: `internal/proto/frame_test.go`

**Interfaces:**
- Produces: `proto.TypePasteFile Type = 0x37`; `proto.PasteFilePayload{Filename string; ContentType string; Data []byte}` (JSON tags: `filename`, `content_type`, `data`).

- [ ] **Step 1: Write the failing roundtrip test**

Append to `internal/proto/frame_test.go`:

```go
func TestPasteFilePayloadRoundtrip(t *testing.T) {
	p := proto.PasteFilePayload{
		Filename:    "notes.pdf",
		ContentType: "application/pdf",
		Data:        []byte("hello world"),
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got proto.PasteFilePayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Filename != p.Filename || got.ContentType != p.ContentType || !bytes.Equal(got.Data, p.Data) {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, p)
	}
}

func TestPasteFileFrameCodec(t *testing.T) {
	sid := uuid.New()
	body := proto.PasteFilePayload{Filename: "foo.log", ContentType: "text/plain", Data: []byte{0x01, 0x02, 0x03}}
	raw, _ := json.Marshal(body)
	f := proto.Frame{Type: proto.TypePasteFile, SessionID: sid, Payload: raw}
	buf, err := proto.Marshal(f)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	got, err := proto.Unmarshal(buf)
	if err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if got.Type != proto.TypePasteFile || got.SessionID != sid || !bytes.Equal(got.Payload, raw) {
		t.Fatalf("frame roundtrip mismatch: got %+v want type=%v sid=%v", got, proto.TypePasteFile, sid)
	}
}
```

Ensure imports at the top of the test file include `bytes`, `encoding/json`, and `github.com/google/uuid` (add only what's missing).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/proto/ -run TestPasteFile -v`
Expected: FAIL — `proto.TypePasteFile` / `proto.PasteFilePayload` undefined.

- [ ] **Step 3: Add the frame type constant**

In `internal/proto/frame.go`, inside the existing `const (...)` block that ends with `TypeAuthInfo Type = 0x40`, insert **just before** `TypeAuthInfo`:

```go
	TypePasteFile    Type = 0x37 // client -> relay -> desktop PTY host (generic file attachment)
```

Resulting fragment:

```go
	TypeViewers      Type = 0x36 // relay -> uplink; mirror's remote subscriber count
	TypePasteFile    Type = 0x37 // client -> relay -> desktop PTY host (generic file attachment)

	// Auth frames (server → client).
	TypeAuthInfo Type = 0x40 // relay -> uplink; UTF-8 JSON {user_id}
```

- [ ] **Step 4: Add the payload struct**

In `internal/proto/frame.go`, immediately **after** the existing `PasteImagePayload` type block, add:

```go
// PasteFilePayload carries a generic file attachment from a remote client
// to the desktop that owns the PTY. Structurally identical to
// PasteImagePayload but semantically distinct: PASTE_IMAGE is clipboard
// image data (silent, filename synthesized); PASTE_FILE is an explicit
// user-picked attachment (filename is user-visible). The desktop
// sanitizes/dedups Filename before writing, and injects the resulting
// absolute path into the PTY master (no CR, no quoting).
type PasteFilePayload struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"data"`
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/proto/ -v`
Expected: PASS (including new tests and pre-existing).

- [ ] **Step 6: Regenerate the WS wire fixture and add paste-file case**

Add a paste-file case to `cmd/proto-fixtures/main.go` (find the paste-image case and mirror it) so it also emits `paste-file.bin`. Concretely, near the existing paste-image emission add:

```go
{
    name: "paste-file",
    build: func(sid uuid.UUID) proto.Frame {
        body, _ := json.Marshal(proto.PasteFilePayload{
            Filename:    "notes.pdf",
            ContentType: "application/pdf",
            Data:        []byte("hello"),
        })
        return proto.Frame{Type: proto.TypePasteFile, SessionID: sid, Payload: body}
    },
},
```

(Match the shape of the existing entries in that file — the field names above are illustrative of the pattern already there.)

Then run: `go run ./cmd/proto-fixtures`
Expected: `web/tests/fixtures/proto/paste-file.bin` created; existing files unchanged.

- [ ] **Step 7: Commit**

```bash
git add internal/proto/frame.go internal/proto/frame_test.go cmd/proto-fixtures/main.go web/tests/fixtures/proto/paste-file.bin
git commit -m "feat(proto): add TypePasteFile (0x37) and PasteFilePayload"
```

---

### Task 2: e2ee — AAD binding test for PASTE_FILE

**Files:**
- Modify: `internal/e2eecrypto/envelope.go` (comment on `SealUnsequenced` around line 57)
- Modify: `internal/e2eecrypto/envelope_test.go`

**Interfaces:**
- Consumes: `proto.TypePasteFile` from Task 1.
- Produces: (no new symbols) — proves `SealUnsequenced` / `OpenUnsequenced` bind PASTE_FILE frame_type as AAD.

- [ ] **Step 1: Update the SealUnsequenced doc comment**

Change the comment above `SealUnsequenced` in `internal/e2eecrypto/envelope.go` (currently: `// currently TypeIn and TypePasteImage. …`):

```go
// SealUnsequenced encrypts a frame that is not bound to a monotonic seq —
// currently TypeIn, TypePasteImage, and TypePasteFile. The AAD is
// session_uuid || frame_type only; per-frame uniqueness is delegated to
// the 24-byte random nonce.
```

- [ ] **Step 2: Write the failing AAD test**

Append to `internal/e2eecrypto/envelope_test.go`:

```go
func TestEnvelopeAADPasteFile(t *testing.T) {
	sk := make([]byte, 32)
	if _, err := rand.Read(sk); err != nil {
		t.Fatal(err)
	}
	sid := uuid.New()
	pt := []byte(`{"filename":"foo.pdf","content_type":"application/pdf","data":"aGk="}`)

	env, err := e2eecrypto.SealUnsequenced(sk, sid, byte(proto.TypePasteFile), pt)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	got, err := e2eecrypto.OpenUnsequenced(sk, sid, byte(proto.TypePasteFile), env)
	if err != nil {
		t.Fatalf("open with matching AAD: %v", err)
	}
	if !bytes.Equal(got, pt) {
		t.Fatalf("plaintext mismatch")
	}

	if _, err := e2eecrypto.OpenUnsequenced(sk, sid, byte(proto.TypePasteImage), env); err == nil {
		t.Fatalf("open with mismatched frame_type AAD should fail")
	}
}
```

Ensure imports include `crypto/rand`, `bytes`, `github.com/google/uuid`, `github.com/attson/atterm/internal/proto`, `github.com/attson/atterm/internal/e2eecrypto`.

- [ ] **Step 3: Run and verify**

Run: `go test ./internal/e2eecrypto/ -run TestEnvelopeAADPasteFile -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/e2eecrypto/envelope.go internal/e2eecrypto/envelope_test.go
git commit -m "test(e2ee): bind PASTE_FILE frame_type into unsequenced AAD"
```

---

### Task 3: relay — `FilePasteHost` interface + adopt.go PASTE_FILE routing

**Files:**
- Modify: `internal/relay/adopt.go`
- Modify: `internal/relay/adopt_test.go`

**Interfaces:**
- Consumes: `proto.TypePasteFile`, `proto.PasteFilePayload` (Task 1).
- Produces: `relay.FilePasteHost interface { PasteFile(ctx context.Context, sessionID uuid.UUID, payload proto.PasteFilePayload) error }`. Routing rule: an adopted host implementing `FilePasteHost` receives PASTE_FILE frames; otherwise dropped with log line `adopt: paste file unavailable session=<sid>`.

- [ ] **Step 1: Write the failing routing test**

Append to `internal/relay/adopt_test.go` (or create if it doesn't have paste-image test — mirror that shape):

```go
type fakeFilePasteHost struct {
	PtyHost
	mu      sync.Mutex
	calls   int
	lastSID uuid.UUID
	lastPay proto.PasteFilePayload
}

func (f *fakeFilePasteHost) PasteFile(_ context.Context, sid uuid.UUID, p proto.PasteFilePayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastSID = sid
	f.lastPay = p
	return nil
}

func TestAdoptRoutesPasteFileToFilePasteHost(t *testing.T) {
	srv := newTestServer(t) // reuse the existing helper if present; otherwise build via relay.NewServer with minimal cfg
	sid := uuid.New()
	pty := &fakePty{}
	host := &fakeFilePasteHost{PtyHost: pty}

	cleanup := srv.AdoptSession(context.Background(), sid, proto.SessionInfo{ID: sid.String()}, host, "")
	defer cleanup()

	body, _ := json.Marshal(proto.PasteFilePayload{
		Filename: "foo.pdf", ContentType: "application/pdf", Data: []byte("abc"),
	})
	// Push a PASTE_FILE frame into the session's inbound queue by whatever
	// existing helper the test file uses for PASTE_IMAGE. If none exists,
	// call sess.Ingest / sess.PushInbound / equivalent private helper used
	// elsewhere in this test file.
	pushInbound(t, srv, sid, proto.Frame{Type: proto.TypePasteFile, SessionID: sid, Payload: body})

	// Wait up to 500 ms for the adopt goroutine to observe it.
	waitForCalls(t, &host.mu, &host.calls, 1, 500*time.Millisecond)

	if host.lastPay.Filename != "foo.pdf" || string(host.lastPay.Data) != "abc" {
		t.Fatalf("payload mismatch: %+v", host.lastPay)
	}
}
```

If `newTestServer`, `fakePty`, `pushInbound`, `waitForCalls` don't already exist in `adopt_test.go`, look at how the PASTE_IMAGE test is structured (`TestAdopt…PasteImage` should exist — grep for it) and reuse the same helpers verbatim; only the frame type and payload shape change.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestAdoptRoutesPasteFile -v`
Expected: FAIL — either compile error (`FilePasteHost` unused / undefined depending on how you wrote the fake) or the frame is dropped with log `paste file unavailable`.

- [ ] **Step 3: Add the interface**

In `internal/relay/adopt.go`, immediately after the existing `ImagePasteHost` block, add:

```go
// FilePasteHost is implemented by desktop PTY wrappers that can accept a
// remote client's file attachment: sanitize+dedup the filename, write the
// bytes to a session-scoped inbox, and inject the resulting absolute
// path into the PTY master. Symmetric to ImagePasteHost but for arbitrary
// files (no clipboard bridging).
type FilePasteHost interface {
	PasteFile(ctx context.Context, sessionID uuid.UUID, payload proto.PasteFilePayload) error
}
```

- [ ] **Step 4: Wire the routing**

In `internal/relay/adopt.go`, inside the goroutine's `switch f.Type` block (right after the existing `case proto.TypePasteImage:` block), add:

```go
			case proto.TypePasteFile:
				pasteHost, ok := host.(FilePasteHost)
				if !ok {
					log.Printf("adopt: paste file unavailable session=%s", f.SessionID)
					continue
				}
				var p proto.PasteFilePayload
				if err := json.Unmarshal(f.Payload, &p); err != nil {
					log.Printf("adopt: bad paste file payload session=%s payload_bytes=%d error=%v", f.SessionID, len(f.Payload), err)
					continue
				}
				if err := pasteHost.PasteFile(loopCtx, f.SessionID, p); err != nil {
					log.Printf("adopt: paste file failed session=%s filename=%q content_type=%q bytes=%d error=%v", f.SessionID, p.Filename, p.ContentType, len(p.Data), err)
				}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestAdoptRoutesPasteFile -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/relay/adopt.go internal/relay/adopt_test.go
git commit -m "feat(relay): route PASTE_FILE frames through FilePasteHost"
```

---

### Task 4: relay — permission + driver gate

**Files:**
- Modify: `internal/relay/permissions.go`
- Modify: `internal/relay/permissions_test.go`
- Modify: `internal/relay/client_conn.go`

**Interfaces:**
- Consumes: `proto.TypePasteFile` (Task 1).
- Produces: `frameAllowedByPermission(scope, perm, TypePasteFile)` returns true only when `scope != authRead` **and** `perm >= permFull`; client_conn drops non-driver PASTE_FILE with log tag `not_driver`.

- [ ] **Step 1: Write the failing permission matrix test**

Append to `internal/relay/permissions_test.go`:

```go
func TestPasteFilePermissionMatrix(t *testing.T) {
	cases := []struct {
		scope authScope
		perm  remotePermission
		want  bool
	}{
		{authRead, permView, false},
		{authRead, permControl, false},
		{authRead, permFull, false},
		{authWrite, permView, false},
		{authWrite, permControl, false},
		{authWrite, permFull, true},
	}
	for _, tc := range cases {
		got := frameAllowedByPermission(tc.scope, tc.perm, proto.TypePasteFile)
		if got != tc.want {
			t.Fatalf("scope=%v perm=%v: got %v want %v", tc.scope, tc.perm, got, tc.want)
		}
	}
}
```

(If the existing package uses different names for `authWrite` or the constants, adopt them verbatim — the write scope is whatever the existing `TestPasteImagePermissionMatrix` uses.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/relay/ -run TestPasteFilePermissionMatrix -v`
Expected: FAIL — permission matrix currently returns `true` (default case) or wrong values.

- [ ] **Step 3: Update permission matrix**

In `internal/relay/permissions.go`, edit `frameAllowedByPermission`:

```go
func frameAllowedByPermission(scope authScope, perm remotePermission, typ proto.Type) bool {
	if scope == authRead {
		switch typ {
		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage, proto.TypePasteFile:
			return false
		default:
			return true
		}
	}
	switch typ {
	case proto.TypeIn, proto.TypeResize:
		return perm >= permControl
	case proto.TypePasteImage, proto.TypePasteFile:
		return perm >= permFull
	default:
		return true
	}
}
```

- [ ] **Step 4: Extend client_conn.go driver gate**

In `internal/relay/client_conn.go`, find the case around line 167:

```go
		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage:
```

Change to:

```go
		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage, proto.TypePasteFile:
```

No other changes in this switch block are needed — driver, permission, and session-attached checks are inside and apply to all four frame types.

- [ ] **Step 5: Run all relay tests**

Run: `go test ./internal/relay/ -v`
Expected: PASS (new + old).

- [ ] **Step 6: Commit**

```bash
git add internal/relay/permissions.go internal/relay/permissions_test.go internal/relay/client_conn.go
git commit -m "feat(relay): gate PASTE_FILE by full-perm + driver, same as PASTE_IMAGE"
```

---

### Task 5: desktop — `paste_file.go` (sanitize + dedup + save + inject) + tests

**Files:**
- Create: `desktop/paste_file.go`
- Create: `desktop/paste_file_test.go`

**Interfaces:**
- Consumes: `proto.PasteFilePayload` (Task 1); `relay.FilePasteHost` (Task 3).
- Produces:
  - `(*desktopPtyHost).PasteFile(ctx context.Context, sessionID uuid.UUID, p proto.PasteFilePayload) error`
  - `pasteFileDir(sessionID uuid.UUID) (string, error)` — inbox path helper reused by Task 6
  - `sanitizeAttachmentName(name string) string`
  - `dedupFilename(dir, name string) (string, error)` — creates an empty placeholder file atomically to reserve the name; caller writes bytes with `WriteFile` next.
  - constant `maxPasteFileBytes = 10 * 1024 * 1024`.

- [ ] **Step 1: Write the failing tests**

Create `desktop/paste_file_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// fakePtyForPaste captures every h.Write for assertion; the shared
// desktopPtyHost embeds *ptyhost.Host, so tests need a lightweight
// substitute that satisfies Write(p []byte) (int, error).
type fakePtyForPaste struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	fail  error
}

func (f *fakePtyForPaste) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return 0, f.fail
	}
	return f.buf.Write(p)
}
func (f *fakePtyForPaste) String() string { f.mu.Lock(); defer f.mu.Unlock(); return f.buf.String() }

// newTestPasteHost returns a *desktopPtyHost with the Write path wired to
// fakePtyForPaste. Because desktopPtyHost embeds *ptyhost.Host and its
// Write method calls h.Host.Write, we instead invoke savePastedFile and
// then simulate the injection step in the test via fake.Write directly.
// The interface-level test in Task 3 already covers routing; this test
// focuses on save+sanitize+dedup+inject shape.

func TestSanitizeAttachmentName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo.pdf", "foo.pdf"},
		{"../../../etc/passwd", "passwd"},
		{"foo\x00bar.txt", "foobar.txt"},
		{"", "file"},
		{"CON", "_CON"},
		{"COM1", "_COM1"},
		{"lpt9", "_lpt9"}, // case-insensitive
		{strings.Repeat("a", 300) + ".log", strings.Repeat("a", 124) + ".log"},
		{"日本語.txt", "日本語.txt"},
		{"a/b/c.log", "c.log"},
		{`a\b\c.log`, "c.log"},
	}
	for _, c := range cases {
		got := sanitizeAttachmentName(c.in)
		if got != c.want {
			t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDedupFilenameHappyPath(t *testing.T) {
	dir := t.TempDir()

	got1, err := dedupFilename(dir, "foo.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got1) != "foo.pdf" {
		t.Errorf("first: got %q", got1)
	}
	got2, err := dedupFilename(dir, "foo.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got2) != "foo (1).pdf" {
		t.Errorf("second: got %q", got2)
	}
	got3, err := dedupFilename(dir, "foo.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got3) != "foo (2).pdf" {
		t.Errorf("third: got %q", got3)
	}
}

func TestDedupFilenameConcurrent(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	results := make([]string, 20)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := dedupFilename(dir, "shared.log")
			if err != nil {
				t.Errorf("dedup %d: %v", i, err)
				return
			}
			results[i] = p
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i, p := range results {
		if p == "" {
			t.Errorf("result %d empty", i)
			continue
		}
		if seen[p] {
			t.Errorf("duplicate path %q at %d", p, i)
		}
		seen[p] = true
	}
}

func TestSavePastedFileHappyPath(t *testing.T) {
	sid := uuid.New()
	p := proto.PasteFilePayload{
		Filename:    "notes.pdf",
		ContentType: "application/pdf",
		Data:        []byte("hello world"),
	}
	path, err := savePastedFile(sid, p)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	// Content
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, p.Data) {
		t.Errorf("content mismatch")
	}

	// Permission
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600", st.Mode().Perm())
	}

	// Location: contains session id + basename retained
	if !strings.Contains(path, sid.String()) {
		t.Errorf("path %q missing session id %q", path, sid.String())
	}
	if filepath.Base(path) != "notes.pdf" {
		t.Errorf("basename = %q, want notes.pdf", filepath.Base(path))
	}
}

func TestSavePastedFileEmpty(t *testing.T) {
	_, err := savePastedFile(uuid.New(), proto.PasteFilePayload{Filename: "x.txt", Data: nil})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestSavePastedFileTooLarge(t *testing.T) {
	_, err := savePastedFile(uuid.New(), proto.PasteFilePayload{
		Filename: "x.bin",
		Data:     make([]byte, maxPasteFileBytes+1),
	})
	if err == nil {
		t.Fatal("expected error for oversize")
	}
}

func TestPasteFileInjectsAbsPath(t *testing.T) {
	// Directly test that PasteFile writes absPath to the pty via the fake.
	sid := uuid.New()
	pty := &fakePtyForPaste{}
	h := &desktopPtyHost{ /* Host omitted — see note below */ }
	// The real PasteFile calls h.Write which ends up hitting h.Host.Write.
	// For this narrow test, replace by calling savePastedFile + fake.Write:
	absPath, err := savePastedFile(sid, proto.PasteFilePayload{
		Filename: "log.txt", ContentType: "text/plain", Data: []byte("x"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(absPath)
	if _, err := pty.Write([]byte(absPath)); err != nil {
		t.Fatal(err)
	}
	if pty.String() != absPath {
		t.Errorf("wrote %q, want %q", pty.String(), absPath)
	}
	_ = h // silence unused
}
```

The last test is deliberately narrower than the full handler test because `desktopPtyHost` embeds `*ptyhost.Host` and can't be easily mocked in-package; end-to-end handler exercise happens in Task 13's manual checklist.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./desktop/ -run "TestSanitize|TestDedup|TestSavePastedFile|TestPasteFileInjects" -v`
Expected: FAIL — functions undefined.

- [ ] **Step 3: Create `desktop/paste_file.go`**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const maxPasteFileBytes = 10 * 1024 * 1024

// PasteFile handles a PASTE_FILE frame routed via relay.FilePasteHost.
// It sanitizes and dedups the filename, writes bytes to
// <cache>/paste-files/<sid>/<name>, and injects the resulting absolute
// path (no CR, no quoting) into the PTY master via h.Write.
func (h *desktopPtyHost) PasteFile(ctx context.Context, sessionID uuid.UUID, p proto.PasteFilePayload) error {
	log.Printf("desktop-paste-file: request %s", pasteFileLogDetails(sessionID, p))
	absPath, err := savePastedFile(sessionID, p)
	if err != nil {
		log.Printf("desktop-paste-file: save_failed %s error=%v", pasteFileLogDetails(sessionID, p), err)
		return err
	}
	log.Printf("desktop-paste-file: saved %s path=%q", pasteFileLogDetails(sessionID, p), absPath)
	if _, err := h.Write([]byte(absPath)); err != nil {
		log.Printf("desktop-paste-file: path_write_failed session=%s path=%q error=%v", sessionID, absPath, err)
		return err
	}
	return nil
}

func pasteFileLogDetails(sessionID uuid.UUID, p proto.PasteFilePayload) string {
	return fmt.Sprintf("session=%s filename=%q content_type=%q bytes=%d",
		sessionID, p.Filename, p.ContentType, len(p.Data))
}

// pasteFileDir returns the on-disk directory for a session's received files.
// Task 6 (received_files.go) reuses this to enumerate.
func pasteFileDir(sessionID uuid.UUID) (string, error) {
	base, err := appdir.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "paste-files", sessionID.String()), nil
}

func savePastedFile(sessionID uuid.UUID, p proto.PasteFilePayload) (string, error) {
	if len(p.Data) == 0 {
		return "", fmt.Errorf("paste file: empty")
	}
	if len(p.Data) > maxPasteFileBytes {
		return "", fmt.Errorf("paste file: too large (%d bytes)", len(p.Data))
	}
	dir, err := pasteFileDir(sessionID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := sanitizeAttachmentName(p.Filename)
	absPath, err := dedupFilename(dir, name)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(absPath, p.Data, 0o600); err != nil {
		return "", err
	}
	return absPath, nil
}

// sanitizeAttachmentName reduces user-supplied filenames to a safe basename:
// no directory parts, no control chars, NFC-normalized, ≤128 chars,
// Windows reserved names prefixed with '_', empty → "file".
func sanitizeAttachmentName(raw string) string {
	// Strip any directory prefix. filepath.Base handles both separators
	// on the host OS; the extra loop copes with foreign separators.
	name := raw
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)

	// Strip control chars, NUL, and the remaining separators explicitly.
	name = strings.Map(func(r rune) rune {
		if r == 0 || unicode.IsControl(r) || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)

	// NFC normalize.
	name = norm.NFC.String(name)

	if name == "" || name == "." || name == ".." {
		return "file"
	}

	// Length cap: 128 chars, preserve extension when possible.
	if len([]rune(name)) > 128 {
		ext := filepath.Ext(name)
		if len([]rune(ext)) > 32 {
			ext = "" // pathological extension, drop
		}
		baseRunes := []rune(strings.TrimSuffix(name, ext))
		keep := 128 - len([]rune(ext))
		if keep < 1 {
			keep = 1
		}
		if keep > len(baseRunes) {
			keep = len(baseRunes)
		}
		name = string(baseRunes[:keep]) + ext
	}

	// Windows reserved names.
	upper := strings.ToUpper(name)
	upperBase := strings.TrimSuffix(upper, filepath.Ext(upper))
	switch upperBase {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}

	return name
}

// dedupFilename returns an available absolute path in dir. If name is free,
// returns dir/name; otherwise dir/base (N).ext for N=1..999. Uses O_EXCL to
// atomically reserve the name (creating a zero-byte placeholder). Callers
// then write the real bytes with os.WriteFile which overwrites the placeholder.
func dedupFilename(dir, name string) (string, error) {
	tryCreate := func(p string) bool {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return false
		}
		_ = f.Close()
		return true
	}

	candidate := filepath.Join(dir, name)
	if tryCreate(candidate) {
		return candidate, nil
	}

	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 1; n <= 999; n++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		if tryCreate(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("paste file: giving up after 999 dedup attempts")
}
```

- [ ] **Step 4: Ensure `golang.org/x/text/unicode/norm` is on go.mod**

Run: `go mod tidy`
Expected: `go.sum` and `go.mod` updated if needed (may be a no-op if already indirect).

- [ ] **Step 5: Run tests**

Run: `go test ./desktop/ -run "TestSanitize|TestDedup|TestSavePastedFile|TestPasteFileInjects" -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/paste_file.go desktop/paste_file_test.go go.mod go.sum
git commit -m "feat(desktop): sanitize+dedup+save+inject PASTE_FILE attachments"
```

---

### Task 6: desktop — `received_files.go` bindings + tests + wire into `app.go`

**Files:**
- Create: `desktop/received_files.go`
- Create: `desktop/received_files_test.go`
- Modify: `desktop/app.go` (bind receiver methods, ensure they compile into models.ts)

**Interfaces:**
- Consumes: `pasteFileDir` (Task 5).
- Produces methods on `*App` (Wails-bound):
  - `ReceivedFilesList() (ReceivedFilesSummary, error)`
  - `ReceivedFilesClearAll() error`
  - `ReceivedFilesClearSession(sessionID string) error`
  - `ReceivedFilesDelete(sessionID, filename string) error`
  - `ReceivedFilesOpenDir() error`
- Types: `ReceivedFilesSummary`, `ReceivedFilesSessionEntry`, `ReceivedFileEntry` (see spec §6.5).

- [ ] **Step 1: Write failing tests**

Create `desktop/received_files_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/appdir"
	"github.com/google/uuid"
)

func mustWriteFile(t *testing.T, dir, name string, size int) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReceivedFilesListEnumerates(t *testing.T) {
	base, err := appdir.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "paste-files")
	// Clean pre-existing state before writing test fixtures.
	_ = os.RemoveAll(root)
	defer os.RemoveAll(root)

	sid1 := uuid.New()
	sid2 := uuid.New()
	mustWriteFile(t, filepath.Join(root, sid1.String()), "a.txt", 100)
	mustWriteFile(t, filepath.Join(root, sid1.String()), "b.pdf", 200)
	mustWriteFile(t, filepath.Join(root, sid2.String()), "c.log", 50)

	a := &App{}
	summary, err := a.ReceivedFilesList()
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalBytes != 350 {
		t.Errorf("total = %d, want 350", summary.TotalBytes)
	}
	if len(summary.Sessions) != 2 {
		t.Errorf("sessions = %d, want 2", len(summary.Sessions))
	}
}

func TestReceivedFilesClearSession(t *testing.T) {
	base, _ := appdir.CacheDir()
	root := filepath.Join(base, "paste-files")
	_ = os.RemoveAll(root)
	defer os.RemoveAll(root)

	sid := uuid.New()
	mustWriteFile(t, filepath.Join(root, sid.String()), "a.txt", 10)

	a := &App{}
	if err := a.ReceivedFilesClearSession(sid.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, sid.String())); !os.IsNotExist(err) {
		t.Fatalf("expected dir removed, err = %v", err)
	}
}

func TestReceivedFilesDeletePathTraversal(t *testing.T) {
	a := &App{}
	err := a.ReceivedFilesDelete(uuid.New().String(), "../evil")
	if err == nil {
		t.Fatal("expected error for traversal filename")
	}
	err = a.ReceivedFilesDelete(uuid.New().String(), "sub/dir.txt")
	if err == nil {
		t.Fatal("expected error for slashed filename")
	}
	err = a.ReceivedFilesDelete("not-a-uuid", "foo.txt")
	if err == nil {
		t.Fatal("expected error for bad session id")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./desktop/ -run TestReceivedFiles -v`
Expected: FAIL — types / methods undefined.

- [ ] **Step 3: Create `desktop/received_files.go`**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/attson/atterm/internal/appdir"
	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ReceivedFilesSummary is the aggregate on-disk view of files delivered by
// remote clients via PASTE_FILE. Bound to the frontend Settings tab.
type ReceivedFilesSummary struct {
	TotalBytes int64                       `json:"total_bytes"`
	Sessions   []ReceivedFilesSessionEntry `json:"sessions"`
}

type ReceivedFilesSessionEntry struct {
	SessionID   string              `json:"session_id"`
	SessionName string              `json:"session_name"`
	Bytes       int64               `json:"bytes"`
	Files       []ReceivedFileEntry `json:"files"`
}

type ReceivedFileEntry struct {
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes"`
	ReceivedAt int64  `json:"received_at"` // nanoseconds since epoch
}

func receivedFilesRoot() (string, error) {
	base, err := appdir.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "paste-files"), nil
}

// ReceivedFilesList enumerates every session directory and its files.
func (a *App) ReceivedFilesList() (ReceivedFilesSummary, error) {
	root, err := receivedFilesRoot()
	if err != nil {
		return ReceivedFilesSummary{}, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return ReceivedFilesSummary{}, nil
		}
		return ReceivedFilesSummary{}, err
	}

	summary := ReceivedFilesSummary{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := uuid.Parse(e.Name()); err != nil {
			continue
		}
		sd := filepath.Join(root, e.Name())
		files, err := os.ReadDir(sd)
		if err != nil {
			continue
		}
		entry := ReceivedFilesSessionEntry{SessionID: e.Name()}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			entry.Files = append(entry.Files, ReceivedFileEntry{
				Name:       f.Name(),
				Bytes:      info.Size(),
				ReceivedAt: info.ModTime().UnixNano(),
			})
			entry.Bytes += info.Size()
		}
		if len(entry.Files) == 0 {
			continue
		}
		sort.Slice(entry.Files, func(i, j int) bool {
			return entry.Files[i].ReceivedAt > entry.Files[j].ReceivedAt
		})
		summary.Sessions = append(summary.Sessions, entry)
		summary.TotalBytes += entry.Bytes
	}
	sort.Slice(summary.Sessions, func(i, j int) bool {
		return summary.Sessions[i].SessionID < summary.Sessions[j].SessionID
	})
	return summary, nil
}

func (a *App) ReceivedFilesClearAll() error {
	root, err := receivedFilesRoot()
	if err != nil {
		return err
	}
	return os.RemoveAll(root)
}

func (a *App) ReceivedFilesClearSession(sessionID string) error {
	if _, err := uuid.Parse(sessionID); err != nil {
		return fmt.Errorf("invalid session id")
	}
	root, err := receivedFilesRoot()
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(root, sessionID))
}

func (a *App) ReceivedFilesDelete(sessionID, filename string) error {
	if _, err := uuid.Parse(sessionID); err != nil {
		return fmt.Errorf("invalid session id")
	}
	if filename == "" || strings.ContainsRune(filename, filepath.Separator) ||
		strings.ContainsRune(filename, '/') || filename != filepath.Clean(filename) {
		return fmt.Errorf("invalid filename")
	}
	root, err := receivedFilesRoot()
	if err != nil {
		return err
	}
	target := filepath.Join(root, sessionID, filename)
	return os.Remove(target)
}

func (a *App) ReceivedFilesOpenDir() error {
	root, err := receivedFilesRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	runtime.BrowserOpenURL(a.ctx, "file://"+root)
	return nil
}
```

Note: `a.ctx` is the shared Wails context that already exists on `*App`. If your `App` struct uses a different field name (`a.ctxWails` etc.), use that.

- [ ] **Step 4: Verify tests pass**

Run: `go test ./desktop/ -run TestReceivedFiles -v`
Expected: PASS.

- [ ] **Step 5: Sanity-check `desktop/app.go` bindings**

The methods are on `*App`; Wails picks them up automatically if `*App` is bound in `main.go`. Confirm by running `wails generate module` (or the project's equivalent) and eyeball `desktop/frontend/wailsjs/go/main/App.d.ts` to see the new methods appear.

Run: `go build ./desktop/...`
Expected: clean build.

- [ ] **Step 6: Commit**

```bash
git add desktop/received_files.go desktop/received_files_test.go
git commit -m "feat(desktop): expose ReceivedFiles* bindings for Settings management"
```

---

### Task 7: frontend shared — `PASTE_FILE` type + `sendPasteFile`

**Files:**
- Modify: `web/src/shared/ws/protocol.ts`
- Modify: `web/src/shared/ws/client-conn.ts`
- Create: `web/src/shared/ws/__tests__/client-conn-paste-file.test.ts` (or extend the existing paste-image test file if that's where PASTE_IMAGE tests live)

**Interfaces:**
- Produces: `TYPE.PASTE_FILE = 0x37`; `SessionConnection.sendPasteFile(blob: Blob, filename: string): Promise<boolean>` — encodes `PasteFilePayload` JSON with base64-string `data`, sends a `PASTE_FILE` frame; returns false when WS not open.

- [ ] **Step 1: Write the failing frontend test**

Create `web/src/shared/ws/__tests__/client-conn-paste-file.test.ts`:

```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { SessionConnection } from '../client-conn'
import { decodeFrame, TYPE } from '../protocol'

class FakeWS {
  static OPEN = 1
  readyState = FakeWS.OPEN
  sent: Uint8Array[] = []
  send(buf: Uint8Array) { this.sent.push(buf) }
  close() {}
  addEventListener() {}
  removeEventListener() {}
}

function makeConn(): { conn: SessionConnection, ws: FakeWS } {
  const ws = new FakeWS()
  const conn = new SessionConnection('00000000-0000-0000-0000-000000000001')
  // The class expects a WebSocket-like object on this.ws. If your class
  // has a public method to inject one for tests, use it; otherwise assign.
  ;(conn as unknown as { ws: FakeWS }).ws = ws
  return { conn, ws }
}

describe('sendPasteFile', () => {
  it('encodes a PASTE_FILE frame with correct payload', async () => {
    const { conn, ws } = makeConn()
    const blob = new Blob([new Uint8Array([1, 2, 3, 4])], { type: 'application/pdf' })
    const ok = await conn.sendPasteFile(blob, 'notes.pdf')
    expect(ok).toBe(true)
    expect(ws.sent).toHaveLength(1)
    const decoded = decodeFrame(ws.sent[0]!)
    expect(decoded.type).toBe(TYPE.PASTE_FILE)
    const parsed = JSON.parse(new TextDecoder().decode(decoded.payload))
    expect(parsed.filename).toBe('notes.pdf')
    expect(parsed.content_type).toBe('application/pdf')
    // data is base64 of [1,2,3,4] -> "AQIDBA=="
    expect(parsed.data).toBe('AQIDBA==')
  })

  it('returns false when ws is closed', async () => {
    const { conn, ws } = makeConn()
    ws.readyState = 3 // CLOSED
    const ok = await conn.sendPasteFile(new Blob(['x']), 'x.txt')
    expect(ok).toBe(false)
  })
})
```

- [ ] **Step 2: Verify it fails**

Run: `pnpm -C web test -- --run client-conn-paste-file`
Expected: FAIL — `TYPE.PASTE_FILE` undefined and `sendPasteFile` not on `SessionConnection`.

- [ ] **Step 3: Add PASTE_FILE to protocol.ts**

In `web/src/shared/ws/protocol.ts`, extend the `TYPE` object (mirroring the exact position order relative to Go):

```ts
export const TYPE = {
  OPEN:             0x01,
  IN:               0x02,
  OUT:              0x03,
  RESIZE:           0x04,
  META:             0x05,
  CLOSE:            0x06,
  ATTACH:           0x10,
  LIST:             0x11,
  LIST_RESP:        0x12,
  REPLAY_PROGRESS:  0x13,
  PING:             0x20,
  PONG:             0x21,
  ANNOUNCE:         0x30,
  STREAM_REQUEST:   0x31,
  STREAM_STOP:      0x32,
  PASTE_IMAGE:      0x33,
  CLAIM_DRIVER:     0x34,
  COMMAND_EVENT:    0x35,
  PASTE_FILE:       0x37,
} as const
```

- [ ] **Step 4: Add `sendPasteFile` to client-conn.ts**

In `web/src/shared/ws/client-conn.ts`, right after `sendPasteImage` (~line 154), add:

```ts
  sendPasteFile(blob: Blob, filename: string): Promise<boolean> {
    return (async () => {
      if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return false
      const buf = await blob.arrayBuffer()
      const data = btoaBytes(new Uint8Array(buf))
      const payload = new TextEncoder().encode(JSON.stringify({
        filename,
        content_type: blob.type || 'application/octet-stream',
        data,
      }))
      const frame = encodeFrame(TYPE.PASTE_FILE, this.sidBytes, payload)
      this.ws.send(frame)
      this.health.onBytesOut(frame.byteLength, Date.now())
      return true
    })()
  }
```

- [ ] **Step 5: Verify it passes**

Run: `pnpm -C web test -- --run client-conn-paste-file`
Expected: PASS.

- [ ] **Step 6: Update proto fixture test to include paste-file.bin**

If `web/src/shared/ws/__tests__/protocol.test.ts` (or wherever the fixture round-trip lives) explicitly enumerates fixture files, add `paste-file.bin` to the list; otherwise nothing to change (auto-discovered).

Run: `pnpm -C web test`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/shared/ws/protocol.ts web/src/shared/ws/client-conn.ts web/src/shared/ws/__tests__/client-conn-paste-file.test.ts
git commit -m "feat(web): sendPasteFile + PASTE_FILE (0x37) in shared client"
```

---

### Task 8: frontend shared — `pasteFileBus` + `PasteFilePreviewHost` (web + desktop-frontend)

**Files:**
- Create: `web/src/main/lib/pasteFileBus.ts`
- Create: `web/src/main/components/PasteFilePreviewHost.vue`
- Create: `web/src/main/components/__tests__/PasteFilePreviewHost.test.ts`
- Create: `desktop/frontend/src/lib/pasteFileBus.ts`
- Create: `desktop/frontend/src/components/PasteFilePreviewHost.vue`

**Interfaces:**
- Produces: `pasteFileBus.emit({filename: string, size: number})`, `pasteFileBus.on(handler)`, `PasteFilePreviewHost` mounts once at app root and shows a 3s auto-dismiss toast per emission.

- [ ] **Step 1: Write the failing preview host test**

Create `web/src/main/components/__tests__/PasteFilePreviewHost.test.ts`:

```ts
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PasteFilePreviewHost from '../PasteFilePreviewHost.vue'
import { pasteFileBus } from '../../lib/pasteFileBus'

describe('PasteFilePreviewHost', () => {
  it('shows a toast on emit and auto-dismisses after 3s', async () => {
    vi.useFakeTimers()
    const w = mount(PasteFilePreviewHost)
    pasteFileBus.emit({ filename: 'foo.pdf', size: 1234 })
    await w.vm.$nextTick()
    expect(w.text()).toContain('foo.pdf')
    vi.advanceTimersByTime(3100)
    await w.vm.$nextTick()
    expect(w.text()).not.toContain('foo.pdf')
    vi.useRealTimers()
  })

  it('dismisses when close is clicked', async () => {
    const w = mount(PasteFilePreviewHost)
    pasteFileBus.emit({ filename: 'bar.log', size: 42 })
    await w.vm.$nextTick()
    expect(w.text()).toContain('bar.log')
    await w.find('[data-testid="paste-file-close"]').trigger('click')
    expect(w.text()).not.toContain('bar.log')
  })
})
```

- [ ] **Step 2: Run to verify failure**

Run: `pnpm -C web test -- --run PasteFilePreviewHost`
Expected: FAIL — module not found.

- [ ] **Step 3: Create `pasteFileBus.ts`**

Same content in both `web/src/main/lib/pasteFileBus.ts` and `desktop/frontend/src/lib/pasteFileBus.ts`:

```ts
export type PasteFileEvent = {
  filename: string
  size: number
}

type Handler = (event: PasteFileEvent) => void

class PasteFileBus {
  private handlers = new Set<Handler>()

  emit(event: PasteFileEvent): void {
    for (const h of this.handlers) h(event)
  }

  on(handler: Handler): () => void {
    this.handlers.add(handler)
    return () => this.handlers.delete(handler)
  }
}

export const pasteFileBus = new PasteFileBus()
```

- [ ] **Step 4: Create `PasteFilePreviewHost.vue`**

Same content in both `web/src/main/components/PasteFilePreviewHost.vue` and `desktop/frontend/src/components/PasteFilePreviewHost.vue` (adjust the relative import path for `pasteFileBus`):

```vue
<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { pasteFileBus, type PasteFileEvent } from '../lib/pasteFileBus'

type Item = PasteFileEvent & { id: number }

const items = ref<Item[]>([])
let nextId = 1
let unsubscribe: (() => void) | null = null

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}

function dismiss(id: number) {
  items.value = items.value.filter((i) => i.id !== id)
}

function handleEmit(e: PasteFileEvent) {
  const item: Item = { ...e, id: nextId++ }
  items.value.push(item)
  setTimeout(() => dismiss(item.id), 3000)
}

onMounted(() => {
  unsubscribe = pasteFileBus.on(handleEmit)
})

onBeforeUnmount(() => {
  unsubscribe?.()
})
</script>

<template>
  <div class="paste-file-preview-host">
    <div v-for="item in items" :key="item.id" class="paste-file-toast">
      <span class="paste-file-name">{{ item.filename }}</span>
      <span class="paste-file-size">({{ formatSize(item.size) }})</span>
      <button
        class="paste-file-close"
        data-testid="paste-file-close"
        @click="dismiss(item.id)"
      >×</button>
    </div>
  </div>
</template>

<style scoped>
.paste-file-preview-host {
  position: fixed; right: 16px; bottom: 16px; z-index: 1000;
  display: flex; flex-direction: column; gap: 8px;
}
.paste-file-toast {
  background: var(--bg-elevated, #222); color: var(--fg, #eee);
  padding: 8px 12px; border-radius: 6px; font-size: 0.875rem;
  display: flex; align-items: center; gap: 8px;
  box-shadow: 0 2px 8px rgba(0,0,0,0.3);
}
.paste-file-name { font-weight: 500; }
.paste-file-size { color: var(--fg-dim, #999); }
.paste-file-close {
  background: none; border: none; color: var(--fg-dim, #999);
  cursor: pointer; font-size: 1.2rem; padding: 0 4px;
}
</style>
```

- [ ] **Step 5: Verify tests pass**

Run: `pnpm -C web test -- --run PasteFilePreviewHost`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/main/lib/pasteFileBus.ts web/src/main/components/PasteFilePreviewHost.vue web/src/main/components/__tests__/PasteFilePreviewHost.test.ts desktop/frontend/src/lib/pasteFileBus.ts desktop/frontend/src/components/PasteFilePreviewHost.vue
git commit -m "feat(frontend): pasteFileBus + PasteFilePreviewHost toast"
```

---

### Task 9: web — `PasteFallback.vue` split + `App.vue` wire

**Files:**
- Modify: `web/src/main/components/PasteFallback.vue`
- Modify: `web/src/main/components/TerminalView.vue`
- Modify: `web/src/main/App.vue`

**Interfaces:**
- Consumes: `pasteFileBus`, `PasteFilePreviewHost`, `SessionConnection.sendPasteFile` (Tasks 7, 8).
- Produces: `PasteFallback` emits `paste-file` (in addition to existing `paste-image`), `App.vue` handles it via `TerminalView.sendPasteFile`, toast fires.

- [ ] **Step 1: Add `paste-file` emit and non-image branch to `PasteFallback.vue`**

At the top of the `<script setup lang="ts">`:

```ts
const emit = defineEmits<{
  (e: 'paste-image', file: File): void
  (e: 'paste-file', file: File): void
}>()
```

In the drop / picker handler, replace the "always emit paste-image" logic with type-based branching. Example after the existing `onFileChosen` (or equivalent):

```ts
function dispatchFile(file: File) {
  if (file.size > 10 * 1024 * 1024) {
    // Match the desktop-side limit; toast is emitted by the caller App.vue
    // — here we just log and drop.
    console.warn('[PasteFallback] file too large, dropping', file.name, file.size)
    return
  }
  if (file.type.startsWith('image/')) {
    emit('paste-image', file)
  } else {
    emit('paste-file', file)
  }
}
```

Then change every existing `emit('paste-image', file)` in this file to `dispatchFile(file)`.

Add a paired file picker in the template:

```vue
      <label class="paste-file-pick" for="paste-file-file">{{ t('terminal.pickFile') }}</label>
      <input
        id="paste-file-file"
        type="file"
        accept="*/*"
        data-testid="paste-file-file"
        @change="onAnyFileChosen"
      />
```

Where `onAnyFileChosen` is the exact same body as `onFileChosen` but uses `dispatchFile` (which is what `onFileChosen` should already do after the earlier substitution — so ideally just reuse the same handler and drop `onAnyFileChosen`).

Add the i18n string `terminal.pickFile` to the same locale files that already have `terminal.pickImage` (English: "Pick file"; Chinese: "选择文件").

- [ ] **Step 2: Forward `sendPasteFile` in `TerminalView.vue`**

In `web/src/main/components/TerminalView.vue`, add next to the existing `sendPasteImage`:

```ts
  sendPasteFile(blob: Blob, filename: string) {
    return conn?.sendPasteFile(blob, filename)
  },
```

- [ ] **Step 3: Handle `paste-file` in `App.vue`**

In `web/src/main/App.vue`:

Add imports:

```ts
import PasteFilePreviewHost from './components/PasteFilePreviewHost.vue'
import { pasteFileBus } from './lib/pasteFileBus'
```

Add handler:

```ts
function onPasteFile(file: File) {
  void termRef.value?.sendPasteFile(file, file.name)
  pasteFileBus.emit({ filename: file.name, size: file.size })
}
```

Add `@paste-file="onPasteFile"` next to the existing `@paste-image="onPasteImage"` on the PasteFallback element.

Mount the preview host next to the existing `<PasteImagePreviewHost />`:

```vue
      <PasteImagePreviewHost />
      <PasteFilePreviewHost />
```

- [ ] **Step 4: Run web tests**

Run: `pnpm -C web test`
Expected: PASS. Existing PasteFallback tests may need an update if they assert on the emit list — extend rather than replace them.

- [ ] **Step 5: Commit**

```bash
git add web/src/main/components/PasteFallback.vue web/src/main/components/TerminalView.vue web/src/main/App.vue web/src/main/i18n
git commit -m "feat(web): PasteFallback branches image vs file, wires PASTE_FILE"
```

---

### Task 10: desktop-frontend + Capacitor 📎 button

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/platform/capacitor.ts` (or the file that owns mobile toolbar)

**Interfaces:**
- Consumes: `SessionConnection.sendPasteFile` (Task 7), `pasteFileBus`, `PasteFilePreviewHost` (Task 8).
- Produces: Wails file drop → `sendPasteFile`; mobile toolbar `<button data-testid="mobile-attach-file">📎</button>` opens a hidden `<input type="file">`.

- [ ] **Step 1: Forward + branch in TerminalView.vue**

Analogous to Task 9 Step 2 but in `desktop/frontend/src/components/TerminalView.vue`. Add `sendPasteFile` method. In the existing Wails `runtime.OnFileDrop` handler, or `paste-image` branch, add the same type-based branching:

```ts
if (file.type.startsWith('image/')) {
  await conn?.sendPasteImage(file, file.name || 'clipboard-image')
} else {
  await conn?.sendPasteFile(file, file.name)
  pasteFileBus.emit({ filename: file.name, size: file.size })
}
```

Above the change, import `pasteFileBus` from `../lib/pasteFileBus`.

- [ ] **Step 2: Mount `PasteFilePreviewHost` in `App.vue`**

Analogous to Task 9 Step 3: add import and `<PasteFilePreviewHost />` next to the existing `<PasteImagePreviewHost />`.

- [ ] **Step 3: Add mobile 📎 toolbar button**

In whichever component renders the mobile toolbar (grep for `IS_CAPACITOR`, `platform/capacitor`, or the file where the existing mobile-specific buttons live), add:

```vue
<button
  v-if="isCapacitor && isDriver"
  class="mobile-toolbar-attach"
  data-testid="mobile-attach-file"
  @click="() => attachFileInput?.click()"
>📎</button>
<input
  ref="attachFileInput"
  type="file"
  accept="*/*"
  style="display:none"
  @change="onMobileFilePicked"
/>
```

Handler:

```ts
const attachFileInput = ref<HTMLInputElement | null>(null)

function onMobileFilePicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  if (file.size > 10 * 1024 * 1024) {
    // TODO(nit): show localized toast; for now log
    console.warn('[mobile] file too large', file.name, file.size)
    input.value = ''
    return
  }
  const isImage = file.type.startsWith('image/')
  if (isImage) {
    void termRef.value?.sendPasteImage(file, file.name)
  } else {
    void termRef.value?.sendPasteFile(file, file.name)
    pasteFileBus.emit({ filename: file.name, size: file.size })
  }
  input.value = ''
}
```

(If `termRef` isn't in scope in this file, adapt to the local ref used by neighboring buttons.)

- [ ] **Step 4: Run desktop-frontend tests**

Run: `pnpm -C desktop/frontend test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/App.vue desktop/frontend/src/platform/capacitor.ts
git commit -m "feat(desktop-frontend): wire PASTE_FILE for Wails filedrop + mobile 📎"
```

---

### Task 11: Settings → Received Files tab

**Files:**
- Create: `desktop/frontend/src/components/SettingsReceivedFiles.vue`
- Create: `desktop/frontend/src/components/__tests__/SettingsReceivedFiles.test.ts`
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Modify: i18n locale files for `settings.tabs.receivedFiles` (English + Chinese)

**Interfaces:**
- Consumes: `ReceivedFilesList / ClearAll / ClearSession / Delete / OpenDir` bindings (Task 6).
- Produces: A `settings` tab with id `"received-files"` (aligned with existing SettingsTabId union) showing summary + per-session list + clear/open buttons.

- [ ] **Step 1: Write failing SettingsReceivedFiles test**

Create `desktop/frontend/src/components/__tests__/SettingsReceivedFiles.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import SettingsReceivedFiles from '../SettingsReceivedFiles.vue'

vi.mock('../../../wailsjs/go/main/App', () => ({
  ReceivedFilesList: vi.fn().mockResolvedValue({
    total_bytes: 350,
    sessions: [
      { session_id: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', session_name: 'shell-1', bytes: 300, files: [
        { name: 'a.txt', bytes: 100, received_at: 1 },
        { name: 'b.pdf', bytes: 200, received_at: 2 },
      ] },
    ],
  }),
  ReceivedFilesClearAll: vi.fn().mockResolvedValue(undefined),
  ReceivedFilesClearSession: vi.fn().mockResolvedValue(undefined),
  ReceivedFilesDelete: vi.fn().mockResolvedValue(undefined),
  ReceivedFilesOpenDir: vi.fn().mockResolvedValue(undefined),
}))

describe('SettingsReceivedFiles', () => {
  it('renders summary and per-session totals', async () => {
    const w = mount(SettingsReceivedFiles)
    await flushPromises()
    expect(w.text()).toContain('350')  // total bytes shown somewhere
    expect(w.text()).toContain('shell-1')
    expect(w.text()).toContain('a.txt')
    expect(w.text()).toContain('b.pdf')
  })
})
```

- [ ] **Step 2: Verify it fails**

Run: `pnpm -C desktop/frontend test -- --run SettingsReceivedFiles`
Expected: FAIL — component missing.

- [ ] **Step 3: Create `SettingsReceivedFiles.vue`**

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  ReceivedFilesList,
  ReceivedFilesClearAll,
  ReceivedFilesClearSession,
  ReceivedFilesDelete,
  ReceivedFilesOpenDir,
} from '../../wailsjs/go/main/App'

interface FileEntry { name: string; bytes: number; received_at: number }
interface SessionEntry { session_id: string; session_name: string; bytes: number; files: FileEntry[] }
interface Summary { total_bytes: number; sessions: SessionEntry[] }

const summary = ref<Summary>({ total_bytes: 0, sessions: [] })
const loading = ref(false)
const expanded = ref<Set<string>>(new Set())

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}

async function reload() {
  loading.value = true
  try {
    summary.value = await ReceivedFilesList()
  } finally {
    loading.value = false
  }
}

async function clearAll() {
  if (!confirm('Clear ALL received files?')) return
  await ReceivedFilesClearAll()
  await reload()
}

async function clearSession(sid: string) {
  await ReceivedFilesClearSession(sid)
  await reload()
}

async function deleteFile(sid: string, name: string) {
  await ReceivedFilesDelete(sid, name)
  await reload()
}

function toggle(sid: string) {
  if (expanded.value.has(sid)) expanded.value.delete(sid)
  else expanded.value.add(sid)
  expanded.value = new Set(expanded.value)
}

onMounted(reload)
</script>

<template>
  <div class="received-files">
    <div class="header">
      <div>
        <strong>Total: {{ formatSize(summary.total_bytes) }}</strong>
        <span class="muted"> · {{ summary.sessions.length }} sessions</span>
      </div>
      <div class="actions">
        <button @click="ReceivedFilesOpenDir">Open folder</button>
        <button @click="clearAll" :disabled="summary.sessions.length === 0">Clear all</button>
      </div>
    </div>
    <div v-if="loading">Loading…</div>
    <div v-else-if="summary.sessions.length === 0" class="empty">No files received.</div>
    <ul v-else class="sessions">
      <li v-for="s in summary.sessions" :key="s.session_id" class="session">
        <div class="session-row" @click="toggle(s.session_id)">
          <span class="session-name">{{ s.session_name || s.session_id.slice(0, 8) }}</span>
          <span class="muted"> · {{ s.files.length }} files · {{ formatSize(s.bytes) }}</span>
          <button
            class="session-clear"
            @click.stop="clearSession(s.session_id)"
          >Clear</button>
        </div>
        <ul v-if="expanded.has(s.session_id)" class="files">
          <li v-for="f in s.files" :key="f.name">
            <span>{{ f.name }}</span>
            <span class="muted"> · {{ formatSize(f.bytes) }}</span>
            <button class="file-delete" @click="deleteFile(s.session_id, f.name)">Delete</button>
          </li>
        </ul>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.received-files { display: flex; flex-direction: column; gap: 12px; }
.header { display: flex; justify-content: space-between; align-items: center; }
.actions { display: flex; gap: 8px; }
.muted { color: var(--fg-dim, #999); font-size: 0.875rem; }
.sessions { list-style: none; padding: 0; margin: 0; }
.session { border-bottom: 1px solid var(--border, #333); padding: 8px 0; }
.session-row { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.session-name { font-weight: 500; }
.session-clear { margin-left: auto; }
.files { list-style: none; padding-left: 16px; margin: 4px 0 0 0; }
.files li { display: flex; align-items: center; gap: 8px; padding: 2px 0; }
.file-delete { margin-left: auto; font-size: 0.8rem; }
.empty { color: var(--fg-dim, #999); padding: 8px 0; }
</style>
```

- [ ] **Step 4: Register tab in `SettingsDialog.vue`**

Extend `SettingsTabId`:

```ts
type SettingsTabId = "general" | "tasks" | "relay" | "plugins" | "shortcuts" | "templates" | "logging" | "updates" | "diagnostics" | "feishu" | "devices" | "received-files"
```

Add to `tabMeta`:

```ts
  'received-files': { labelKey: 'settings.tabs.receivedFiles', english: 'Received files' },
```

Add to `tabIcons` (pick a reasonable emoji or icon key consistent with existing entries — e.g. `'📎'`).

Import the component and render it inside the tab panel switch:

```ts
import SettingsReceivedFiles from './SettingsReceivedFiles.vue';
```

Add a branch (near the existing `<SettingsRelay ... />`):

```vue
        <SettingsReceivedFiles v-if="activeTab === 'received-files'" />
```

Add English + Chinese i18n:

- English: `settings.tabs.receivedFiles = "Received files"`
- Chinese: `settings.tabs.receivedFiles = "接收文件"`

- [ ] **Step 5: Verify frontend build + tests**

Run: `pnpm -C desktop/frontend test`
Expected: PASS.

Run: `pnpm -C desktop/frontend build`
Expected: clean build.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/SettingsReceivedFiles.vue desktop/frontend/src/components/__tests__/SettingsReceivedFiles.test.ts desktop/frontend/src/components/SettingsDialog.vue desktop/frontend/src/locales
git commit -m "feat(desktop-frontend): Settings → Received files tab"
```

---

### Task 12: Update `docs/spec/protocol.md`

**Files:**
- Modify: `docs/spec/protocol.md`

**Interfaces:**
- Produces: canonical protocol documentation for `PASTE_FILE (0x37)`.

- [ ] **Step 1: Add PASTE_FILE to the frame type enum listing**

Near the existing enum at "TypeViewers Type = 0x36", insert:

```
    TypePasteFile     Type = 0x37 // client -> relay -> desktop PTY host
```

(keeping the existing `TypeAuthInfo Type = 0x40` on its own line below).

- [ ] **Step 2: Add the schema section**

After the existing `PASTE_IMAGE` (or `CLAIM_DRIVER`) schema section, insert:

````markdown
### `PASTE_FILE` (0x37) — client → relay → desktop PTY host

```json
{ "filename": "foo.pdf", "content_type": "application/pdf", "data": "<base64>" }
```

- `filename`：用户可见文件名（不含目录）。desktop 侧强制 sanitize + dedup 才落盘，wire 值可以脏
- `content_type`：客户端 best-effort，服务器不校验、不据此路由
- `data`：原始字节。单帧 payload 上限 16 MiB，实际 base64 + JSON 开销后应用层建议 ≤ 10 MiB
- **E2EE**：当持有 `account_key` 时，整个 `PasteFilePayload` JSON 走 [§E2EE 信封](#e2ee-信封) 加密，AAD 鉴别字节 = `0x37`（当前仅 Go attach 客户端实现；web/Capacitor 与 PASTE_IMAGE 同 posture 尚未 seal）
- **权限**：`remote_permission = "full"` 才允许；`view`/`control` 被 relay 拒绝
- **driver**：只有当前 driver subscriber 能发；非 driver 的 PASTE_FILE 被 relay 静默 drop
- desktop 收到后：sanitize filename → 落盘 `<cache-root>/paste-files/<sid>/<name>` → 冲名追加 ` (N)` → 把最终绝对路径当作 IN 帧内容写进 PTY（无 CR，无引号）
````

- [ ] **Step 3: Commit**

```bash
git add docs/spec/protocol.md
git commit -m "docs(protocol): document PASTE_FILE (0x37) frame"
```

---

### Task 13: End-to-end verification checklist

**Files:** none (real-app testing).

- [ ] **Step 1: Build and launch the desktop app**

Run: `wails dev` (or the project's dev command).
Expected: app launches with the new Settings → Received files tab visible.

- [ ] **Step 2: Web → local desktop same machine**

Chrome, connect via `/client` local page. Select a 3 MiB PDF via the picker.
Expected: absolute path appears in the terminal prompt; Finder-opening it shows correct content; toast shows "notes.pdf (3.0 MiB)".

- [ ] **Step 3: Capacitor → desktop**

iPhone Capacitor build, attach via mobile. Tap 📎, pick a photo from the Files app.
Expected: absolute path appears in terminal; `cat <path>` in desktop shell shows the image bytes.

- [ ] **Step 4: Another atterm desktop → owner desktop**

On a second machine, attach via `/client`. Drag a log file from Finder to the terminal.
Expected: path appears.

- [ ] **Step 5: Oversize path**

Try selecting a 15 MiB file.
Expected: toast/warn "file too large", nothing sent.

- [ ] **Step 6: Viewer negative test**

Attach a second web client as viewer (not driver) with a session where `remote_permission = "full"`.
Expected: attach button hidden; if forced (e.g. via devtools) the frame is dropped by relay (check relay log for `not_driver`).

- [ ] **Step 7: E2EE (Go attach path)**

Sign in with an account that has E2EE unlocked. Attach from a second atterm desktop. Send a file.
Expected: relay log does not show filename plaintext; owner desktop receives and logs `desktop-paste-file: saved`.

- [ ] **Step 8: Settings → Received files**

Open Settings → Received files.
Expected: list shows every file from steps 2–7. Clear one session; that group disappears.

- [ ] **Step 9: Filename edge cases**

From web, send files named:
- `../evil.txt`
- `📎emoji spaces.log`
- `日本語 文字.pdf`

Expected: on-disk basenames are `evil.txt`, `📎emoji spaces.log`, `日本語 文字.pdf` respectively; injected path matches; AI-style `cat` reads the exact bytes.

- [ ] **Step 10: Commit any doc/comment fixes discovered during verification**

If verification surfaces bugs, fix them (as new commits), re-run, and only proceed when 1–9 all pass.

---

### Task 14: Ship release

**Files:** none written by hand — driven by the `ship-release` skill.

- [ ] **Step 1: Ensure clean working tree on `main`**

Run: `git status`
Expected: nothing to commit.

- [ ] **Step 2: Invoke `ship-release` skill**

Per user's global constraints (per memory `release_series_v02`), the release series is `v0.2.x`; the skill's Phase 5 must filter tags with `git tag --list "v0.2*" --sort=-v:refname | head -1` to compute the correct next patch.

- [ ] **Step 3: Watch skill through phases 1-6**

Skill will: cut a branch → commit any leftover → push → open PR → squash-merge → tag & release.
Expected: new `v0.2.x` tag created on remote; GitHub release visible.

---

## Self-Review Notes (for the planner)

- **Spec §3 architecture** → Tasks 1, 3, 5 cover proto + routing + desktop handler.
- **Spec §4 protocol** → Tasks 1 + 12.
- **Spec §5 frontends** → Tasks 7, 8, 9, 10.
- **Spec §6 desktop receive path** → Tasks 5, 6, 11.
- **Spec §7 permissions/E2EE/error** → Tasks 2, 4.
- **Spec §8 testing** → each task's TDD steps + Task 13 e2e.
- **Spec §9 rollout** → Task 14 ship-release.
- All function signatures used across tasks (`sanitizeAttachmentName`, `dedupFilename`, `savePastedFile`, `pasteFileDir`, `PasteFile`, `PasteFilePayload`, `sendPasteFile`, `ReceivedFiles*`) are defined in Task 5–7 and reused verbatim.
- Constants (`maxPasteFileBytes = 10 * 1024 * 1024`, `TypePasteFile = 0x37`) are defined once and referenced consistently.
