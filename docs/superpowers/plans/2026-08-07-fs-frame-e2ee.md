# FS Frame E2EE Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make file contents, paths, and agent-side error strings unreadable by the relay, then lift the `.env` deny wherever it cannot leak.

**Architecture:** FS payloads change from single JSON blobs to a segmented binary format: `count(1B) || (len(4B BE) || bytes)*`. Segment 0 stays plaintext JSON with only the fields the relay routes on. Later segments are XChaCha20-Poly1305 envelopes built by the existing `e2eecrypto` helpers, AAD-bound to each frame's own type byte. File bytes ride raw inside the envelope instead of being base64'd into JSON, so transfer size drops slightly versus today.

**Tech Stack:** Go (`internal/proto`, `internal/e2eecrypto`, `desktop/`), TypeScript (`desktop/frontend/src/lib`), `@noble/ciphers` XChaCha20-Poly1305, Vitest, Go testing.

## Global Constraints

- AAD frame_type bytes: `FS_REQUEST=0x38`, `FS_RESPONSE=0x39`, `FS_EVENT=0x3a`. Each frame uses its own type byte. Never reuse an allocated byte (AGENTS.md red line #22).
- Every field moved into an envelope MUST be zeroed in the plaintext struct before encoding (AGENTS.md red line #23).
- Seal failure with a key present is fail-closed: return an error response, never fall back to plaintext. This diverges from the OUT/META fallback documented in `protocol.md` §612 and is deliberate.
- `no key = no encryption` still holds: a keyless agent emits single-segment plaintext.
- A keyed sender always emits its sealed segment, even when the struct is empty. Segment count signals key state, not payload presence.
- The relay never gains a key. Its payload access switches to segment 0 only, via the key-free `proto.DecodeFSHead` / `EncodeFSHead` helpers; it reads `request_id`, `op`, `client_id`, `ok`, `watch_id` and nothing else.
- Local sessions (`createLocalFSBridge`, Wails direct binding) never produce FS frames and must not be affected.
- No backward compatibility. Desktop and iOS app ship together.
- Segment lengths bounded by `proto.maxPayload` (16 MiB).

---

### Task 1: Segment codec in proto

**Files:**
- Create: `internal/proto/fs_segments.go`
- Test: `internal/proto/fs_segments_test.go`

**Interfaces:**
- Produces: `func EncodeSegments(segments [][]byte) ([]byte, error)` and `func DecodeSegments(payload []byte) ([][]byte, error)`. Used by every later Go task.

- [ ] **Step 1: Write the failing test**

```go
package proto

import (
	"bytes"
	"testing"
)

func TestSegmentsRoundTrip(t *testing.T) {
	cases := [][][]byte{
		{[]byte(`{"a":1}`)},
		{[]byte(`{"a":1}`), []byte("sealed")},
		{[]byte(`{"a":1}`), []byte("sealed"), []byte("content")},
		{[]byte(`{}`), {}},
	}
	for i, segs := range cases {
		encoded, err := EncodeSegments(segs)
		if err != nil {
			t.Fatalf("case %d: encode: %v", i, err)
		}
		decoded, err := DecodeSegments(encoded)
		if err != nil {
			t.Fatalf("case %d: decode: %v", i, err)
		}
		if len(decoded) != len(segs) {
			t.Fatalf("case %d: got %d segments, want %d", i, len(decoded), len(segs))
		}
		for j := range segs {
			if !bytes.Equal(decoded[j], segs[j]) {
				t.Fatalf("case %d seg %d: got %q want %q", i, j, decoded[j], segs[j])
			}
		}
	}
}

func TestSegmentsRejectsMalformed(t *testing.T) {
	valid, _ := EncodeSegments([][]byte{[]byte("ab"), []byte("cd")})
	cases := map[string][]byte{
		"empty":            {},
		"zero segments":    {0x00},
		"truncated prefix": valid[:3],
		"truncated body":   valid[:len(valid)-1],
		"trailing bytes":   append(append([]byte{}, valid...), 0xff),
	}
	for name, payload := range cases {
		if _, err := DecodeSegments(payload); err == nil {
			t.Fatalf("%s: expected error, got nil", name)
		}
	}
}

func TestEncodeSegmentsRejectsOversized(t *testing.T) {
	if _, err := EncodeSegments([][]byte{make([]byte, maxPayload+1)}); err == nil {
		t.Fatal("expected error for oversized segment")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proto/ -run TestSegments -v`
Expected: FAIL — `undefined: EncodeSegments`

- [ ] **Step 3: Write the implementation**

```go
package proto

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// FS frame payloads are segmented rather than a single JSON document so
// file bytes can travel raw inside an AEAD envelope instead of being
// base64'd into JSON. Segment 0 is always plaintext JSON carrying the
// fields the relay routes on; later segments are sealed envelopes.
//
//	payload := segment_count(1B) || segment*
//	segment := length(4B BE) || bytes
var (
	ErrFSSegmentsMalformed = errors.New("proto: malformed FS segments")
	ErrFSSegmentTooLarge   = errors.New("proto: FS segment exceeds max payload")
)

func EncodeSegments(segments [][]byte) ([]byte, error) {
	if len(segments) == 0 {
		return nil, fmt.Errorf("%w: need at least one segment", ErrFSSegmentsMalformed)
	}
	if len(segments) > 255 {
		return nil, fmt.Errorf("%w: %d segments", ErrFSSegmentsMalformed, len(segments))
	}
	total := 1
	for _, s := range segments {
		if len(s) > maxPayload {
			return nil, ErrFSSegmentTooLarge
		}
		total += 4 + len(s)
	}
	out := make([]byte, 0, total)
	out = append(out, byte(len(segments)))
	for _, s := range segments {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(s)))
		out = append(out, lenBuf[:]...)
		out = append(out, s...)
	}
	return out, nil
}

func DecodeSegments(payload []byte) ([][]byte, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("%w: empty payload", ErrFSSegmentsMalformed)
	}
	count := int(payload[0])
	if count == 0 {
		return nil, fmt.Errorf("%w: zero segments", ErrFSSegmentsMalformed)
	}
	rest := payload[1:]
	segments := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		if len(rest) < 4 {
			return nil, fmt.Errorf("%w: truncated length prefix at segment %d", ErrFSSegmentsMalformed, i)
		}
		n := int(binary.BigEndian.Uint32(rest[:4]))
		if n > maxPayload {
			return nil, ErrFSSegmentTooLarge
		}
		rest = rest[4:]
		if len(rest) < n {
			return nil, fmt.Errorf("%w: truncated body at segment %d", ErrFSSegmentsMalformed, i)
		}
		seg := make([]byte, n)
		copy(seg, rest[:n])
		segments = append(segments, seg)
		rest = rest[n:]
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%w: %d trailing bytes", ErrFSSegmentsMalformed, len(rest))
	}
	return segments, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/proto/ -run TestSegments -v && go test ./internal/proto/ -run TestEncodeSegments -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/proto/fs_segments.go internal/proto/fs_segments_test.go
git commit -m "feat(proto): segmented payload codec for FS frames"
```

---

### Task 2: Sealed field structs and FS payload encode/decode

**Files:**
- Create: `internal/proto/fs_sealed.go`
- Test: `internal/proto/fs_sealed_test.go`
- Modify: `internal/proto/frame.go` — add `Data []byte` split comment to `FileContent` / `FSChunkPayload` (no field changes)

**Interfaces:**
- Consumes: `EncodeSegments` / `DecodeSegments` from Task 1.
- Produces:
  - `func EncodeFSRequest(p FSRequestPayload, sessionKey []byte, sessionID uuid.UUID) ([]byte, error)`
  - `func DecodeFSRequest(payload []byte, sessionKey []byte, sessionID uuid.UUID) (FSRequestPayload, error)`
  - `func EncodeFSResponse(p FSResponsePayload, sessionKey []byte, sessionID uuid.UUID) ([]byte, error)`
  - `func DecodeFSResponse(payload []byte, sessionKey []byte, sessionID uuid.UUID) (FSResponsePayload, error)`
  - `func EncodeFSEvent(p FSEventPayload, sessionKey []byte, sessionID uuid.UUID) ([]byte, error)`
  - `func DecodeFSEvent(payload []byte, sessionKey []byte, sessionID uuid.UUID) (FSEventPayload, error)`
  - A nil `sessionKey` means "no key": encode single-segment plaintext, decode accepts either shape.

- [ ] **Step 1: Write the failing test**

```go
package proto

import (
	"testing"

	"github.com/google/uuid"
)

func testKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func TestFSResponseSealedRoundTrip(t *testing.T) {
	sid := uuid.New()
	key := testKey()
	in := FSResponsePayload{
		RequestID: "r1",
		OK:        true,
		Entries:   []DirEntry{{Name: ".env", IsDir: false, Size: 12}},
		Content:   &FileContent{Path: "/home/u/.env", Data: []byte("SECRET=1"), IsBinary: false},
		Error:     "boom /home/u/.env",
	}
	encoded, err := EncodeFSResponse(in, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	segs, err := DecodeSegments(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 {
		t.Fatalf("got %d segments, want 3", len(segs))
	}
	plain := string(segs[0])
	for _, secret := range []string{".env", "SECRET=1", "/home/u", "boom"} {
		if contains(plain, secret) {
			t.Fatalf("segment 0 leaked %q: %s", secret, plain)
		}
	}
	out, err := DecodeFSResponse(encoded, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out.RequestID != "r1" || !out.OK {
		t.Fatalf("routing fields lost: %+v", out)
	}
	if len(out.Entries) != 1 || out.Entries[0].Name != ".env" {
		t.Fatalf("entries lost: %+v", out.Entries)
	}
	if out.Content == nil || string(out.Content.Data) != "SECRET=1" || out.Content.Path != "/home/u/.env" {
		t.Fatalf("content lost: %+v", out.Content)
	}
	if out.Error != "boom /home/u/.env" {
		t.Fatalf("error lost: %q", out.Error)
	}
}

func TestFSResponsePlaintextWhenNoKey(t *testing.T) {
	sid := uuid.New()
	in := FSResponsePayload{RequestID: "r2", OK: true, Entries: []DirEntry{{Name: "a.txt"}}}
	encoded, err := EncodeFSResponse(in, nil, sid)
	if err != nil {
		t.Fatal(err)
	}
	segs, err := DecodeSegments(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1", len(segs))
	}
	out, err := DecodeFSResponse(encoded, nil, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 || out.Entries[0].Name != "a.txt" {
		t.Fatalf("entries lost: %+v", out.Entries)
	}
}

func TestFSSealedAADIsFrameBound(t *testing.T) {
	sid := uuid.New()
	key := testKey()
	encoded, err := EncodeFSResponse(FSResponsePayload{RequestID: "r3", OK: true}, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	// A response envelope must not open as a request envelope.
	if _, err := DecodeFSRequest(encoded, key, sid); err == nil {
		t.Fatal("response envelope opened under FS_REQUEST AAD")
	}
}

func TestFSSealedRejectsWrongSession(t *testing.T) {
	key := testKey()
	encoded, err := EncodeFSResponse(FSResponsePayload{RequestID: "r4", OK: true}, key, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeFSResponse(encoded, key, uuid.New()); err == nil {
		t.Fatal("envelope opened under a different session id")
	}
}

func TestFSRequestSealedRoundTrip(t *testing.T) {
	sid := uuid.New()
	key := testKey()
	in := FSRequestPayload{RequestID: "q1", Op: "read_file", Path: "/home/u/.env", MaxBytes: 1024}
	encoded, err := EncodeFSRequest(in, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	segs, _ := DecodeSegments(encoded)
	if contains(string(segs[0]), ".env") {
		t.Fatalf("segment 0 leaked path: %s", segs[0])
	}
	if !contains(string(segs[0]), "read_file") {
		t.Fatalf("segment 0 must keep op for the relay gate: %s", segs[0])
	}
	out, err := DecodeFSRequest(encoded, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != "/home/u/.env" || out.Op != "read_file" || out.MaxBytes != 1024 {
		t.Fatalf("round trip lost fields: %+v", out)
	}
}

func TestFSEventSealedRoundTrip(t *testing.T) {
	sid := uuid.New()
	key := testKey()
	encoded, err := EncodeFSEvent(FSEventPayload{WatchID: "7", Path: "/home/u/secrets", Event: "changed"}, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	segs, _ := DecodeSegments(encoded)
	if contains(string(segs[0]), "secrets") {
		t.Fatalf("segment 0 leaked path: %s", segs[0])
	}
	out, err := DecodeFSEvent(encoded, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != "/home/u/secrets" || out.WatchID != "7" || out.Event != "changed" {
		t.Fatalf("round trip lost fields: %+v", out)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proto/ -run TestFS -v`
Expected: FAIL — `undefined: EncodeFSResponse`

- [ ] **Step 3: Write the implementation**

```go
package proto

import (
	"encoding/json"
	"fmt"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/google/uuid"
)

// Sealed field sets. Whole structs go into the envelope rather than
// individual fields: entries[].Name sits beside entries[].Size, and
// splitting them would create index-paired plaintext/ciphertext arrays
// where a mismatch corrupts data silently. The relay reads none of
// these fields, so hiding the metadata alongside costs nothing.

type SealedFSRequestFields struct {
	Path    string `json:"path,omitempty"`
	NewPath string `json:"new_path,omitempty"`
}

type SealedFSResponseFields struct {
	Entries []DirEntry      `json:"entries,omitempty"`
	Meta    *FileMetaInfo   `json:"meta,omitempty"`
	Error   string          `json:"error,omitempty"`
	Content *FileContent    `json:"content,omitempty"`
	Chunk   *FSChunkPayload `json:"chunk,omitempty"`
}

type SealedFSEventFields struct {
	Path string `json:"path,omitempty"`
}

func EncodeFSRequest(p FSRequestPayload, sessionKey []byte, sessionID uuid.UUID) ([]byte, error) {
	if len(sessionKey) == 0 {
		body, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		return EncodeSegments([][]byte{body})
	}
	sealedBody, err := json.Marshal(SealedFSRequestFields{Path: p.Path, NewPath: p.NewPath})
	if err != nil {
		return nil, err
	}
	env, err := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(TypeFSRequest), sealedBody)
	if err != nil {
		return nil, fmt.Errorf("seal fs request: %w", err)
	}
	// Red line #23: zero the plaintext copies of everything sealed.
	p.Path = ""
	p.NewPath = ""
	head, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return EncodeSegments([][]byte{head, env})
}

func DecodeFSRequest(payload []byte, sessionKey []byte, sessionID uuid.UUID) (FSRequestPayload, error) {
	segs, err := DecodeSegments(payload)
	if err != nil {
		return FSRequestPayload{}, err
	}
	var out FSRequestPayload
	if err := json.Unmarshal(segs[0], &out); err != nil {
		return FSRequestPayload{}, err
	}
	if len(segs) == 1 {
		return out, nil
	}
	if len(sessionKey) == 0 {
		return FSRequestPayload{}, fmt.Errorf("fs request is sealed but no session key is available")
	}
	body, err := e2eecrypto.OpenUnsequenced(sessionKey, sessionID, byte(TypeFSRequest), segs[1])
	if err != nil {
		return FSRequestPayload{}, fmt.Errorf("open fs request: %w", err)
	}
	var fields SealedFSRequestFields
	if err := json.Unmarshal(body, &fields); err != nil {
		return FSRequestPayload{}, err
	}
	out.Path = fields.Path
	out.NewPath = fields.NewPath
	return out, nil
}

func EncodeFSResponse(p FSResponsePayload, sessionKey []byte, sessionID uuid.UUID) ([]byte, error) {
	if len(sessionKey) == 0 {
		body, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		return EncodeSegments([][]byte{body})
	}

	// Split the raw bytes out so they ride segment 2 unencoded instead of
	// being base64'd into the sealed JSON.
	var raw []byte
	fields := SealedFSResponseFields{
		Entries: p.Entries,
		Meta:    p.Meta,
		Error:   p.Error,
	}
	if p.Content != nil {
		clone := *p.Content
		raw = clone.Data
		clone.Data = nil
		fields.Content = &clone
	}
	if p.Chunk != nil {
		clone := *p.Chunk
		raw = clone.Data
		clone.Data = nil
		fields.Chunk = &clone
	}
	sealedBody, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	metaEnv, err := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(TypeFSResponse), sealedBody)
	if err != nil {
		return nil, fmt.Errorf("seal fs response: %w", err)
	}

	head := FSResponsePayload{RequestID: p.RequestID, OK: p.OK, WatchID: p.WatchID}
	headBody, err := json.Marshal(head)
	if err != nil {
		return nil, err
	}
	segments := [][]byte{headBody, metaEnv}
	if raw != nil {
		contentEnv, err := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(TypeFSResponse), raw)
		if err != nil {
			return nil, fmt.Errorf("seal fs content: %w", err)
		}
		segments = append(segments, contentEnv)
	}
	return EncodeSegments(segments)
}

func DecodeFSResponse(payload []byte, sessionKey []byte, sessionID uuid.UUID) (FSResponsePayload, error) {
	segs, err := DecodeSegments(payload)
	if err != nil {
		return FSResponsePayload{}, err
	}
	var out FSResponsePayload
	if err := json.Unmarshal(segs[0], &out); err != nil {
		return FSResponsePayload{}, err
	}
	if len(segs) == 1 {
		return out, nil
	}
	if len(sessionKey) == 0 {
		return FSResponsePayload{}, fmt.Errorf("fs response is sealed but no session key is available")
	}
	body, err := e2eecrypto.OpenUnsequenced(sessionKey, sessionID, byte(TypeFSResponse), segs[1])
	if err != nil {
		return FSResponsePayload{}, fmt.Errorf("open fs response: %w", err)
	}
	var fields SealedFSResponseFields
	if err := json.Unmarshal(body, &fields); err != nil {
		return FSResponsePayload{}, err
	}
	out.Entries = fields.Entries
	out.Meta = fields.Meta
	out.Error = fields.Error
	out.Content = fields.Content
	out.Chunk = fields.Chunk
	if len(segs) > 2 {
		raw, err := e2eecrypto.OpenUnsequenced(sessionKey, sessionID, byte(TypeFSResponse), segs[2])
		if err != nil {
			return FSResponsePayload{}, fmt.Errorf("open fs content: %w", err)
		}
		switch {
		case out.Content != nil:
			out.Content.Data = raw
		case out.Chunk != nil:
			out.Chunk.Data = raw
		}
	}
	return out, nil
}

func EncodeFSEvent(p FSEventPayload, sessionKey []byte, sessionID uuid.UUID) ([]byte, error) {
	if len(sessionKey) == 0 {
		body, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		return EncodeSegments([][]byte{body})
	}
	sealedBody, err := json.Marshal(SealedFSEventFields{Path: p.Path})
	if err != nil {
		return nil, err
	}
	env, err := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(TypeFSEvent), sealedBody)
	if err != nil {
		return nil, fmt.Errorf("seal fs event: %w", err)
	}
	p.Path = ""
	head, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return EncodeSegments([][]byte{head, env})
}

func DecodeFSEvent(payload []byte, sessionKey []byte, sessionID uuid.UUID) (FSEventPayload, error) {
	segs, err := DecodeSegments(payload)
	if err != nil {
		return FSEventPayload{}, err
	}
	var out FSEventPayload
	if err := json.Unmarshal(segs[0], &out); err != nil {
		return FSEventPayload{}, err
	}
	if len(segs) == 1 {
		return out, nil
	}
	if len(sessionKey) == 0 {
		return FSEventPayload{}, fmt.Errorf("fs event is sealed but no session key is available")
	}
	body, err := e2eecrypto.OpenUnsequenced(sessionKey, sessionID, byte(TypeFSEvent), segs[1])
	if err != nil {
		return FSEventPayload{}, fmt.Errorf("open fs event: %w", err)
	}
	var fields SealedFSEventFields
	if err := json.Unmarshal(body, &fields); err != nil {
		return FSEventPayload{}, err
	}
	out.Path = fields.Path
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/proto/ -v`
Expected: PASS (all)

- [ ] **Step 5: Commit**

```bash
git add internal/proto/fs_sealed.go internal/proto/fs_sealed_test.go
git commit -m "feat(proto): sealed field sets and FS payload codecs"
```

---

### Task 3: Key-free head helpers and relay adaptation

**Files:**
- Modify: `internal/proto/fs_sealed.go` — add `EncodeFSHead` / `DecodeFSHead`
- Modify: `internal/relay/client_conn.go:269` (op gate), `:355` (`sendFSClientError`)
- Modify: `internal/relay/fs_router.go:158` (response routing), `:179` (duplicate rewrite), `:201` (event routing)
- Test: `internal/relay/fs_router_test.go`

**Interfaces:**
- Consumes: Task 1's `EncodeSegments` / `DecodeSegments`.
- Produces: `func EncodeFSHead(v any) ([]byte, error)` — single-segment plaintext; `func DecodeFSHead(payload []byte, v any) error` — parses segment 0, ignores the rest. Neither takes a key.

The relay must never hold a key, but it does need `request_id` / `op` / `ok` / `watch_id`. These two helpers give it exactly that and nothing more.

- [ ] **Step 1: Write the failing test**

```go
package relay

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestFSRouterRoutesSealedResponse(t *testing.T) {
	r := newFSRouter()
	sid := uuid.New()
	out := make(chan proto.Frame, 1)
	r.registerRequest(fsRouteKey{sessionID: sid, id: "req-1"}, fsClientRoute{out: out}, "read_file")

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	payload, err := proto.EncodeFSResponse(proto.FSResponsePayload{
		RequestID: "req-1",
		OK:        true,
		Content:   &proto.FileContent{Path: "/secret/.env", Data: []byte("K=V")},
	}, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !r.routeResponse(proto.Frame{Type: proto.TypeFSResponse, SessionID: sid, Payload: payload}) {
		t.Fatal("sealed response was not routed")
	}
	got := <-out
	if string(got.Payload) != string(payload) {
		t.Fatal("relay altered the sealed payload")
	}
}

func TestFSHeadHelpersIgnoreSealedSegments(t *testing.T) {
	sid := uuid.New()
	key := make([]byte, 32)
	payload, err := proto.EncodeFSRequest(proto.FSRequestPayload{
		RequestID: "q1", Op: "read_file", Path: "/secret/.env",
	}, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	var head proto.FSRequestPayload
	if err := proto.DecodeFSHead(payload, &head); err != nil {
		t.Fatal(err)
	}
	if head.Op != "read_file" || head.RequestID != "q1" {
		t.Fatalf("head lost routing fields: %+v", head)
	}
	if head.Path != "" {
		t.Fatalf("head must not expose the sealed path, got %q", head.Path)
	}
}
```

Adjust `newFSRouter` / `registerRequest` names to match the actual unexported API in `fs_router.go` if they differ.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/relay/ -run TestFS -v`
Expected: FAIL — `undefined: proto.DecodeFSHead`

- [ ] **Step 3: Implement**

In `internal/proto/fs_sealed.go`:

```go
// EncodeFSHead / DecodeFSHead give the relay access to segment 0 without
// a key. The relay routes on request_id / watch_id and gates on op, but
// must never see a path or a file byte, so these deliberately have no
// key parameter and ignore every segment past the first.
func EncodeFSHead(v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return EncodeSegments([][]byte{body})
}

func DecodeFSHead(payload []byte, v any) error {
	segs, err := DecodeSegments(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(segs[0], v)
}
```

In `internal/relay/client_conn.go:269`, swap the unmarshal:

```go
			var request proto.FSRequestPayload
			if err := proto.DecodeFSHead(f.Payload, &request); err != nil || request.RequestID == "" || request.Op == "" || f.SessionID != sess.ID {
```

In `sendFSClientError` (`client_conn.go:355`), swap the marshal:

```go
	payload, err := proto.EncodeFSHead(proto.FSResponsePayload{
		RequestID: requestID,
		Error:     message,
	})
```

In `internal/relay/fs_router.go:158` and `:201`, swap both unmarshals to `proto.DecodeFSHead(f.Payload, &payload)`.

At `fs_router.go:179`, the duplicate-watch branch currently mutates and re-marshals. Replace the whole frame with a single-segment error — `OK=false` means the sealed segments hold nothing worth keeping:

```go
					if existing, exists := r.watches[watchKey]; exists && existing.out != route.out {
						errBody, encErr := proto.EncodeFSHead(proto.FSResponsePayload{
							RequestID: payload.RequestID,
							OK:        false,
							Error:     "duplicate_watch_id",
						})
						if encErr == nil {
							f.Payload = errBody
						}
					} else {
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/relay/ ./internal/proto/ -v && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/proto/fs_sealed.go internal/relay/client_conn.go internal/relay/fs_router.go internal/relay/fs_router_test.go
git commit -m "feat(relay): route FS frames on segment 0 without a key"
```

---

### Task 4: Agent-side sealing and opening

**Files:**
- Modify: `desktop/remote_fs.go` — `remoteFS` struct, `newRemoteFS`, `remoteFSResponseFrame`, `onDirChanged`, `handleRemoteFSRequest`
- Modify: `desktop/uplink.go:196` (construction), `desktop/uplink.go:429` (`TypeFSRequest` case)
- Test: `desktop/remote_fs_test.go`

**Interfaces:**
- Consumes: `proto.EncodeFSResponse` / `DecodeFSRequest` / `EncodeFSEvent` from Task 2.
- Produces: `newRemoteFS(access *fsAccess, accountKey func() []byte) *remoteFS`; `remoteFSResponseFrame(sessionID uuid.UUID, payload proto.FSResponsePayload, sessionKey []byte) proto.Frame`.
- `remoteFS.sessionKey(sessionID uuid.UUID) []byte` derives the per-session key, returning nil when no account key is unlocked.

- [ ] **Step 1: Write the failing test**

```go
func TestRemoteFSSealsResponseWhenKeyed(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	fs.accountKey = func() []byte { return key }

	path := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(path, []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	sid := uuid.New()
	frame := fs.handle(sid, proto.FSRequestPayload{RequestID: "s1", Op: "read_file", Path: path, MaxBytes: 1024})

	segs, err := proto.DecodeSegments(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 {
		t.Fatalf("got %d segments, want 3", len(segs))
	}
	if strings.Contains(string(frame.Payload), "TOPSECRET") {
		t.Fatal("file bytes present in plaintext on the wire")
	}
	if strings.Contains(string(segs[0]), "secret.txt") {
		t.Fatal("path present in the routing segment")
	}

	sk, err := e2eecrypto.DeriveSessionKey(key, sid)
	if err != nil {
		t.Fatal(err)
	}
	out, err := proto.DecodeFSResponse(frame.Payload, sk, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out.Content == nil || string(out.Content.Data) != "TOPSECRET" {
		t.Fatalf("content did not survive: %+v", out.Content)
	}
}

func TestRemoteFSPlaintextWhenKeyless(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	path := filepath.Join(root, "plain.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	frame := fs.handle(uuid.New(), proto.FSRequestPayload{RequestID: "p1", Op: "read_file", Path: path, MaxBytes: 1024})
	segs, err := proto.DecodeSegments(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1", len(segs))
	}
}

func TestRemoteFSSealsEventPath(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	fs.accountKey = func() []byte { return key }
	sid := uuid.New()
	if _, err := fs.watchDir(sid, root); err != nil {
		t.Fatal(err)
	}
	fs.onDirChanged(root)
	select {
	case frame := <-fs.events():
		if strings.Contains(string(frame.Payload), root) {
			t.Fatal("event path present in plaintext")
		}
		sk, _ := e2eecrypto.DeriveSessionKey(key, sid)
		out, err := proto.DecodeFSEvent(frame.Payload, sk, sid)
		if err != nil {
			t.Fatal(err)
		}
		if out.Path != root {
			t.Fatalf("path did not survive: %q", out.Path)
		}
	default:
		t.Fatal("no event emitted")
	}
}
```

Update `makeRemoteFS` in the same file to construct via `newRemoteFS(access, nil)`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./desktop/ -run TestRemoteFS -v`
Expected: FAIL — `fs.accountKey undefined`

- [ ] **Step 3: Write the implementation**

In `desktop/remote_fs.go`:

```go
type remoteFS struct {
	access  *fsAccess
	eventCh chan proto.Frame
	// accountKey returns the unlocked E2EE account key, or nil when the
	// agent is keyless. Sealing is decided solely by this — never by any
	// inbound field — so the relay cannot downgrade a sealed session.
	accountKey func() []byte
	// ... existing fields unchanged
}

func newRemoteFS(access *fsAccess, accountKey func() []byte) *remoteFS {
	// ... existing body, plus:
	//   accountKey: accountKey,
}

// sessionKey derives the per-session key, or nil when keyless.
func (fs *remoteFS) sessionKey(sessionID uuid.UUID) []byte {
	if fs == nil || fs.accountKey == nil {
		return nil
	}
	ak := fs.accountKey()
	if len(ak) < e2eecrypto.SessionKeySize {
		return nil
	}
	sk, err := e2eecrypto.DeriveSessionKey(ak, sessionID)
	if err != nil {
		return nil
	}
	return sk
}
```

`remoteFSResponseFrame` gains the key and becomes fail-closed:

```go
func remoteFSResponseFrame(sessionID uuid.UUID, payload proto.FSResponsePayload, sessionKey []byte) proto.Frame {
	body, err := proto.EncodeFSResponse(payload, sessionKey, sessionID)
	if err != nil {
		// Fail closed: a seal error must never degrade to plaintext,
		// because the .env deny is lifted on the basis of sealing being
		// in effect. Emit a keyless error response with no path in it.
		body, _ = proto.EncodeFSResponse(proto.FSResponsePayload{
			RequestID: payload.RequestID,
			OK:        false,
			Error:     "response encoding failed",
		}, nil, sessionID)
	}
	return proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: body}
}
```

Update its three call sites (`remote_fs.go:177` and the two branches in `handleRemoteFSRequest`) to pass `fs.sessionKey(sessionID)`; for the `fs == nil` branch pass `nil`.

`onDirChanged` uses the event encoder:

```go
	for _, watch := range watches {
		payload, err := proto.EncodeFSEvent(proto.FSEventPayload{
			WatchID: strconv.FormatInt(watch.handleID, 10),
			Path:    path,
			Event:   "changed",
		}, fs.sessionKey(watch.session), watch.session)
		if err != nil {
			continue
		}
		// ... unchanged
	}
```

In `desktop/uplink.go:196`: `remoteFS := newRemoteFS(newFSAccess(remoteFSAllowRoots()), u.accountKey)`.

In `desktop/uplink.go:429`, replace the `json.Unmarshal` with the sealed decoder:

```go
		case proto.TypeFSRequest:
			req, err := proto.DecodeFSRequest(f.Payload, remoteFS.sessionKey(f.SessionID), f.SessionID)
			if err != nil {
				log.Printf("desktop-uplink: fs_request_decode_failed session=%s error=%v", f.SessionID, err)
				continue
			}
			if !handleRemoteFSRequest(connCtx, out, f.SessionID, u.rawRemotePermission, remoteFS, req) {
				return nil
			}
```

- [ ] **Step 4: Run tests**

Run: `go test ./desktop/ -run TestRemoteFS -v && go build ./...`
Expected: PASS, build clean

- [ ] **Step 5: Commit**

```bash
git add desktop/remote_fs.go desktop/uplink.go desktop/remote_fs_test.go
git commit -m "feat(desktop): seal FS responses and events, open sealed requests"
```

---

### Task 5: TypeScript envelope sealing and segment codec

**Files:**
- Modify: `desktop/frontend/src/lib/opaque.ts` — add `sealUnsequenced`, export `deriveSessionKey`
- Create: `desktop/frontend/src/lib/fsSegments.ts`
- Test: `desktop/frontend/src/lib/fsSegments.test.ts`

**Interfaces:**
- Produces:
  - `sealUnsequenced(accountKey: Uint8Array, sessionUUID: string, frameType: number, plaintext: Uint8Array): Uint8Array`
  - `openUnsequencedFrame(accountKey: Uint8Array, sessionUUID: string, frameType: number, envelope: Uint8Array): Uint8Array | null`
  - `encodeSegments(segments: Uint8Array[]): Uint8Array`
  - `decodeSegments(payload: Uint8Array): Uint8Array[] | null`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { encodeSegments, decodeSegments } from "./fsSegments";
import { sealUnsequenced, openUnsequencedFrame } from "./opaque";

const SID = "11111111-2222-3333-4444-555555555555";
const KEY = new Uint8Array(32).map((_, i) => i);

describe("fs segments", () => {
  it("round-trips one, two and three segments", () => {
    for (const segs of [
      [new Uint8Array([1, 2, 3])],
      [new Uint8Array([1]), new Uint8Array([2, 2])],
      [new Uint8Array([1]), new Uint8Array([2]), new Uint8Array([3, 3, 3])],
    ]) {
      const decoded = decodeSegments(encodeSegments(segs));
      expect(decoded).not.toBeNull();
      expect(decoded!.length).toBe(segs.length);
      decoded!.forEach((d, i) => expect(Array.from(d)).toEqual(Array.from(segs[i])));
    }
  });

  it("rejects malformed payloads", () => {
    const valid = encodeSegments([new Uint8Array([1, 2]), new Uint8Array([3, 4])]);
    expect(decodeSegments(new Uint8Array())).toBeNull();
    expect(decodeSegments(new Uint8Array([0]))).toBeNull();
    expect(decodeSegments(valid.slice(0, 3))).toBeNull();
    expect(decodeSegments(valid.slice(0, valid.length - 1))).toBeNull();
    expect(decodeSegments(new Uint8Array([...valid, 0xff]))).toBeNull();
  });
});

describe("sealUnsequenced", () => {
  it("round-trips with openUnsequencedFrame", () => {
    const plaintext = new TextEncoder().encode("SECRET=1");
    const env = sealUnsequenced(KEY, SID, 0x39, plaintext);
    expect(env[0]).toBe(0x01);
    expect(env.length).toBe(plaintext.length + 41);
    const opened = openUnsequencedFrame(KEY, SID, 0x39, env);
    expect(opened).not.toBeNull();
    expect(new TextDecoder().decode(opened!)).toBe("SECRET=1");
  });

  it("fails to open under a different frame type", () => {
    const env = sealUnsequenced(KEY, SID, 0x39, new TextEncoder().encode("x"));
    expect(openUnsequencedFrame(KEY, SID, 0x38, env)).toBeNull();
  });

  it("fails to open under a different session id", () => {
    const env = sealUnsequenced(KEY, SID, 0x39, new TextEncoder().encode("x"));
    expect(openUnsequencedFrame(KEY, "99999999-2222-3333-4444-555555555555", 0x39, env)).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/fsSegments.test.ts`
Expected: FAIL — cannot resolve `./fsSegments`

- [ ] **Step 3: Write the implementation**

`desktop/frontend/src/lib/fsSegments.ts`:

```ts
// Mirror of internal/proto/fs_segments.go. FS payloads are segmented so
// file bytes ride raw inside an AEAD envelope rather than being base64'd
// into JSON: payload := count(1B) || (len(4B BE) || bytes)*
const MAX_SEGMENT = 16 * 1024 * 1024;

export function encodeSegments(segments: Uint8Array[]): Uint8Array {
  if (segments.length === 0 || segments.length > 255) {
    throw new Error(`fs segments: bad segment count ${segments.length}`);
  }
  let total = 1;
  for (const s of segments) {
    if (s.length > MAX_SEGMENT) throw new Error("fs segments: segment too large");
    total += 4 + s.length;
  }
  const out = new Uint8Array(total);
  const view = new DataView(out.buffer);
  out[0] = segments.length;
  let offset = 1;
  for (const s of segments) {
    view.setUint32(offset, s.length, false);
    offset += 4;
    out.set(s, offset);
    offset += s.length;
  }
  return out;
}

export function decodeSegments(payload: Uint8Array): Uint8Array[] | null {
  if (payload.length < 1) return null;
  const count = payload[0];
  if (count === 0) return null;
  const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength);
  const segments: Uint8Array[] = [];
  let offset = 1;
  for (let i = 0; i < count; i++) {
    if (offset + 4 > payload.length) return null;
    const n = view.getUint32(offset, false);
    if (n > MAX_SEGMENT) return null;
    offset += 4;
    if (offset + n > payload.length) return null;
    segments.push(payload.slice(offset, offset + n));
    offset += n;
  }
  if (offset !== payload.length) return null;
  return segments;
}
```

In `opaque.ts`, alongside the existing `openOutFrame` (which already builds the same AAD), add:

```ts
/** sealUnsequenced mirrors internal/e2eecrypto/envelope.go seal():
 *  cipher_id(0x01) || nonce(24B random) || XChaCha20-Poly1305 ciphertext.
 *  AAD = session_uuid(16B) || frame_type(1B). This is the first code on
 *  the client that encrypts rather than decrypts; the cross-language
 *  vectors in fsCrypto.vectors.test.ts are the control on it. */
export function sealUnsequenced(
  accountKey: Uint8Array,
  sessionUUID: string,
  frameType: number,
  plaintext: Uint8Array,
): Uint8Array {
  const sk = deriveSessionKey(accountKey, sessionUUID)
  const aad = new Uint8Array(17)
  aad.set(uuidStringToBytes(sessionUUID), 0)
  aad[16] = frameType
  const nonce = randomBytes(24)
  const ciphertext = xchacha20poly1305(sk, nonce, aad).encrypt(plaintext)
  const out = new Uint8Array(1 + nonce.length + ciphertext.length)
  out[0] = 0x01
  out.set(nonce, 1)
  out.set(ciphertext, 1 + nonce.length)
  return out
}

export function openUnsequencedFrame(
  accountKey: Uint8Array,
  sessionUUID: string,
  frameType: number,
  envelope: Uint8Array,
): Uint8Array | null {
  if (envelope.length < 41 || envelope[0] !== 0x01) return null
  try {
    const sk = deriveSessionKey(accountKey, sessionUUID)
    const aad = new Uint8Array(17)
    aad.set(uuidStringToBytes(sessionUUID), 0)
    aad[16] = frameType
    const nonce = envelope.slice(1, 25)
    return xchacha20poly1305(sk, nonce, aad).decrypt(envelope.slice(25))
  } catch {
    return null
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/lib/fsSegments.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/fsSegments.ts desktop/frontend/src/lib/fsSegments.test.ts desktop/frontend/src/lib/opaque.ts
git commit -m "feat(frontend): segment codec and client-side envelope sealing"
```

---

### Task 6: Cross-language vectors

**Files:**
- Create: `desktop/frontend/src/lib/fsCrypto.vectors.test.ts`
- Create: `internal/proto/fs_vectors_test.go`

**Interfaces:**
- Consumes: Task 2's Go codecs, Task 5's TS helpers.

These are the highest-value tests in the plan: the two implementations are independent, and only a fixture produced by one and opened by the other proves they agree.

- [ ] **Step 1: Generate the Go-produced fixture**

Run this throwaway program and copy its output:

```bash
cd /Users/attson/code/github.com.attson/atterm && cat > /tmp/genvec.go <<'EOF'
package main

import (
	"encoding/base64"
	"fmt"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/google/uuid"
)

func main() {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	sid := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	sk, _ := e2eecrypto.DeriveSessionKey(key, sid)
	env, _ := e2eecrypto.SealUnsequenced(sk, sid, 0x39, []byte("SECRET=1"))
	fmt.Println(base64.StdEncoding.EncodeToString(env))
}
EOF
go run /tmp/genvec.go && rm /tmp/genvec.go
```

- [ ] **Step 2: Write the TS test with that fixture**

```ts
import { describe, expect, it } from "vitest";
import { openUnsequencedFrame, sealUnsequenced } from "./opaque";

const SID = "11111111-2222-3333-4444-555555555555";
const KEY = new Uint8Array(32).map((_, i) => i);

// Produced by Go: e2eecrypto.SealUnsequenced(DeriveSessionKey(KEY, SID), SID, 0x39, "SECRET=1")
const GO_ENVELOPE_B64 = "<paste Step 1 output>";

function b64(s: string): Uint8Array {
  const bin = atob(s);
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

describe("cross-language envelope vectors", () => {
  it("opens a Go-sealed envelope", () => {
    const opened = openUnsequencedFrame(KEY, SID, 0x39, b64(GO_ENVELOPE_B64));
    expect(opened).not.toBeNull();
    expect(new TextDecoder().decode(opened!)).toBe("SECRET=1");
  });

  it("emits an envelope Go can open (printed for the Go fixture)", () => {
    const env = sealUnsequenced(KEY, SID, 0x38, new TextEncoder().encode("/home/u/.env"));
    // Keep in sync with internal/proto/fs_vectors_test.go
    console.log("TS_ENVELOPE_B64:", btoa(String.fromCharCode(...env)));
    expect(env[0]).toBe(0x01);
  });
});
```

- [ ] **Step 3: Run it, capture the TS-produced fixture**

Run: `cd desktop/frontend && npx vitest run src/lib/fsCrypto.vectors.test.ts`
Expected: first test PASS; copy the `TS_ENVELOPE_B64:` line from stdout.

- [ ] **Step 4: Write the Go test with the TS fixture**

```go
package proto

import (
	"encoding/base64"
	"testing"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/google/uuid"
)

// Produced by TS: sealUnsequenced(KEY, SID, 0x38, "/home/u/.env")
// Keep in sync with desktop/frontend/src/lib/fsCrypto.vectors.test.ts
const tsEnvelopeB64 = "<paste Step 3 output>"

func TestOpensTSSealedEnvelope(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	sid := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	sk, err := e2eecrypto.DeriveSessionKey(key, sid)
	if err != nil {
		t.Fatal(err)
	}
	env, err := base64.StdEncoding.DecodeString(tsEnvelopeB64)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e2eecrypto.OpenUnsequenced(sk, sid, byte(TypeFSRequest), env)
	if err != nil {
		t.Fatalf("could not open TS-sealed envelope: %v", err)
	}
	if string(out) != "/home/u/.env" {
		t.Fatalf("got %q", out)
	}
}
```

- [ ] **Step 5: Run both suites and commit**

Run: `go test ./internal/proto/ -run TestOpensTS -v && cd desktop/frontend && npx vitest run src/lib/fsCrypto.vectors.test.ts`
Expected: PASS both

```bash
git add internal/proto/fs_vectors_test.go desktop/frontend/src/lib/fsCrypto.vectors.test.ts
git commit -m "test: cross-language envelope vectors for FS frames"
```

---

### Task 7: Frontend wiring

**Files:**
- Modify: `desktop/frontend/src/lib/connection.ts` — `sendFSRequest` (line ~505), `handleFSResponse` (line ~779), `handleFSEvent` (line ~795)
- Test: `desktop/frontend/src/lib/connection.fs-e2ee.test.ts`

**Interfaces:**
- Consumes: Task 5's `encodeSegments` / `decodeSegments` / `sealUnsequenced` / `openUnsequencedFrame`; `getCurrentAccountKey` from `./account-key`.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { setAccountKeyProvider } from "./account-key";
import { decodeSegments } from "./fsSegments";
import { openUnsequencedFrame } from "./opaque";
import { SessionConnection } from "./connection";
import { decodeFrame, TYPE } from "./proto";

const KEY = new Uint8Array(32).map((_, i) => i);
const SESSION_ID = "11111111-2222-3333-4444-555555555555";

// Minimal fake WebSocket: records every frame sent so the test can pull
// the FS_REQUEST payload back out. decodeFrame lives in ./proto.
class FakeWS {
  static OPEN = 1;
  readyState = 1;
  sent: Uint8Array[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  send(data: ArrayBuffer) {
    this.sent.push(new Uint8Array(data));
  }
  close() {
    this.readyState = 3;
  }
}

async function makeAttachedConn() {
  const ws = new FakeWS();
  vi.stubGlobal("WebSocket", vi.fn(() => ws) as unknown as typeof WebSocket);
  const conn = new SessionConnection("ws://x", SESSION_ID, {}, { clientName: "test", remote: true });
  conn.attach();
  ws.onopen?.();
  await Promise.resolve();
  return {
    conn,
    sessionId: SESSION_ID,
    // Payloads of FS_REQUEST frames only, in send order.
    get sentPayloads() {
      return ws.sent
        .map((b) => decodeFrame(b))
        .filter((f) => f.type === TYPE.FS_REQUEST)
        .map((f) => f.payload);
    },
  };
}

describe("sendFSRequest sealing", () => {
  beforeEach(() => setAccountKeyProvider(() => KEY));
  afterEach(() => setAccountKeyProvider(null));

  it("seals path into segment 1 and keeps op plaintext", async () => {
    const { conn, sentPayloads, sessionId } = await makeAttachedConn();
    void conn.sendFSRequest({ op: "read_file", path: "/home/u/.env", max_bytes: 1024 });
    const segs = decodeSegments(sentPayloads.at(-1)!);
    expect(segs).not.toBeNull();
    expect(segs!.length).toBe(2);
    const head = JSON.parse(new TextDecoder().decode(segs![0]));
    expect(head.op).toBe("read_file");
    expect(head.path ?? "").toBe("");
    const opened = openUnsequencedFrame(KEY, sessionId, 0x38, segs![1]);
    expect(JSON.parse(new TextDecoder().decode(opened!)).path).toBe("/home/u/.env");
  });

  it("falls back to a single plaintext segment without a key", async () => {
    setAccountKeyProvider(() => null);
    const { conn, sentPayloads } = await makeAttachedConn();
    void conn.sendFSRequest({ op: "list_dir", path: "/home/u" });
    expect(decodeSegments(sentPayloads.at(-1)!)!.length).toBe(1);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/connection.fs-e2ee.test.ts`
Expected: FAIL — payload is plain JSON, `decodeSegments` returns null

- [ ] **Step 3: Implement**

In `sendFSRequest`, replace `const encoded = encodeText(JSON.stringify(payload));` with:

```ts
    const encoded = encodeFSRequestPayload(payload, this.sessionId);
```

and add the module-level helper:

```ts
// Mirror of proto.EncodeFSRequest. op / request_id / client_id stay in
// segment 0 because the relay gates on them (isReadOnlyFSOperation).
function encodeFSRequestPayload(payload: FSRequest, sessionId: string): Uint8Array {
  const accountKey = getCurrentAccountKey();
  if (!accountKey) {
    return encodeSegments([encodeText(JSON.stringify(payload))]);
  }
  const sealed = encodeText(JSON.stringify({ path: payload.path, new_path: payload.new_path }));
  const env = sealUnsequenced(accountKey, sessionId, TYPE.FS_REQUEST, sealed);
  const head = { ...payload };
  delete head.path;
  delete head.new_path;
  return encodeSegments([encodeText(JSON.stringify(head)), env]);
}
```

`handleFSResponse` decodes segments before parsing:

```ts
  private handleFSResponse(payload: Uint8Array): void {
    const segs = decodeSegments(payload);
    if (!segs) return;
    let response: FSResponse;
    try {
      const parsed = JSON.parse(decodeText(segs[0]));
      if (!isFSResponse(parsed)) return;
      response = parsed;
    } catch {
      return;
    }
    if (segs.length > 1) {
      const accountKey = getCurrentAccountKey();
      if (!accountKey) return;
      const meta = openUnsequencedFrame(accountKey, this.sessionId, TYPE.FS_RESPONSE, segs[1]);
      if (!meta) return;
      try {
        Object.assign(response, JSON.parse(decodeText(meta)));
      } catch {
        return;
      }
      if (segs.length > 2) {
        const raw = openUnsequencedFrame(accountKey, this.sessionId, TYPE.FS_RESPONSE, segs[2]);
        if (!raw) return;
        // Go marshals []byte as base64; remoteSessionFS decodes it that way.
        const b64 = btoa(String.fromCharCode(...raw));
        if (response.content) response.content.data = b64;
        else if (response.chunk) response.chunk.data = b64;
      }
    }
    const pending = this.pendingFSRequests.get(response.request_id);
    if (!pending) return;
    window.clearTimeout(pending.timer);
    this.pendingFSRequests.delete(response.request_id);
    pending.resolve(response);
  }
```

`handleFSEvent` follows the same shape: `decodeSegments`, parse segment 0, and when a segment 1 exists open it under `TYPE.FS_EVENT` and assign `path` before the existing field validation runs.

- [ ] **Step 4: Run tests**

Run: `cd desktop/frontend && npx vitest run && npx vue-tsc --noEmit`
Expected: PASS, typecheck clean

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/connection.ts desktop/frontend/src/lib/connection.fs-e2ee.test.ts
git commit -m "feat(frontend): seal FS requests, open sealed responses and events"
```

---

### Task 8: Conditional `.env` deny

**Files:**
- Modify: `desktop/fsaccess.go:34-72` (`fsAccess`, `newFSAccess`, `isDenied`)
- Modify: `desktop/plugin_fs.go:100`, `desktop/uplink.go:196`
- Test: `desktop/fsaccess_test.go`, `desktop/plugin_fs_test.go`, `desktop/fsaccess_write_test.go`, `desktop/remote_fs_test.go`

**Interfaces:**
- Consumes: Task 4's `remoteFS` key plumbing.
- Produces: `newFSAccess(allowRoots []string, denyEnv bool) *fsAccess`.

- [ ] **Step 1: Rewrite the four existing deny tests as a matrix**

```go
func TestFSAccessEnvDenyIsConditional(t *testing.T) {
	home := t.TempDir()
	envFile := filepath.Join(home, "app", ".env.local")
	if err := os.MkdirAll(filepath.Dir(envFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	ssh := filepath.Join(home, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		denyEnv   bool
		wantDenied bool
	}{
		{"env allowed when sealing is in effect", false, false},
		{"env denied when keyless", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newFSAccess([]string{home}, tc.denyEnv)
			_, err := a.resolve(envFile)
			gotDenied := errors.Is(err, ErrPathDenied)
			if gotDenied != tc.wantDenied {
				t.Fatalf("denied=%v want %v (err=%v)", gotDenied, tc.wantDenied, err)
			}
			// .ssh is unconditional in both directions.
			if _, err := a.resolve(ssh); !errors.Is(err, ErrPathDenied) {
				t.Fatalf(".ssh must always be denied, got %v", err)
			}
		})
	}
}

func TestEnvDenyCoversAllVariants(t *testing.T) {
	home := t.TempDir()
	a := newFSAccess([]string{home}, true)
	for _, name := range []string{".env", ".env.local", ".env.example"} {
		p := filepath.Join(home, name)
		if err := os.WriteFile(p, []byte("X=1"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := a.resolve(p); !errors.Is(err, ErrPathDenied) {
			t.Fatalf("%s: expected deny, got %v", name, err)
		}
	}
}
```

Delete the now-superseded `.env` assertions from `TestFSAccessResolve` (`fsaccess_test.go:40-48`) and `TestResolveRejectsDenyPattern` (`plugin_fs_test.go:85-94`); keep their `.ssh` assertions. Update `TestWriteFileDenied` and `TestRemoteFSDeniedPathReturnsError` to construct with `denyEnv=true`, and update every other `newFSAccess(...)` call site in tests to the two-argument form.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./desktop/ -run 'TestFSAccessEnvDeny|TestEnvDenyCovers' -v`
Expected: FAIL — `too many arguments to newFSAccess`

- [ ] **Step 3: Implement**

```go
type fsAccess struct {
	allowRoots []string
	// denyEnv keeps .env* unreadable. Set on the remote path whenever
	// sealing is not in effect, because that is exactly when the file
	// would cross the relay in the clear. Never set for the local Wails
	// path, which produces no frames at all.
	denyEnv bool
	// ... existing fields
}

func newFSAccess(allowRoots []string, denyEnv bool) *fsAccess {
	roots := append([]string(nil), allowRoots...)
	return &fsAccess{allowRoots: roots, denyEnv: denyEnv}
}

// denyExact is unconditional: these never cross either path.
var denyExact = []string{".ssh", ".gnupg", ".aws"}

// envDenySuffix is gated on fsAccess.denyEnv.
var envDenySuffix = []string{".env"}

func (a *fsAccess) isDenied(resolved string) bool {
	base := filepath.Base(resolved)
	for _, d := range denyExact {
		if base == d {
			return true
		}
	}
	if a.denyEnv {
		for _, suf := range envDenySuffix {
			if base == suf || strings.HasPrefix(base, suf+".") {
				return true
			}
		}
	}
	parts := strings.Split(resolved, string(filepath.Separator))
	for _, p := range parts {
		for _, d := range denyExact {
			if p == d {
				return true
			}
		}
	}
	return false
}
```

Change `resolve` to call `a.isDenied(resolved)`. Call sites:
- `plugin_fs.go:100`: `newFSAccess([]string{home}, false)`
- `uplink.go:196`: build the access with `denyEnv` from key availability:

```go
	accountKeyUnlocked := u.accountKey != nil && len(u.accountKey()) >= e2eecrypto.SessionKeySize
	remoteFS := newRemoteFS(newFSAccess(remoteFSAllowRoots(), !accountKeyUnlocked), u.accountKey)
```

- [ ] **Step 4: Run the full Go suite**

Run: `go test ./... && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/fsaccess.go desktop/plugin_fs.go desktop/uplink.go desktop/fsaccess_test.go desktop/plugin_fs_test.go desktop/fsaccess_write_test.go desktop/remote_fs_test.go
git commit -m "feat(desktop): lift .env deny where sealing is in effect"
```

---

### Task 9: Documentation

**Files:**
- Modify: `docs/spec/protocol.md` — §E2EE 信封 AAD table, §"Plaintext / E2EE posture"
- Modify: `AGENTS.md:68` (§22 allocated bytes)

- [ ] **Step 1: Add the three AAD rows**

In the §E2EE 信封 table after the `0x35` row:

```markdown
| `0x38` `FS_REQUEST` | 分段 payload 的 segment 1 | JSON `SealedFSRequestFields { path, new_path }` |
| `0x39` `FS_RESPONSE` | segment 1（元数据）+ segment 2（文件原始字节） | JSON `SealedFSResponseFields { entries, meta, error, content, chunk }`；segment 2 是不经 base64 的原始字节 |
| `0x3a` `FS_EVENT` | 分段 payload 的 segment 1 | JSON `SealedFSEventFields { path }` |
```

- [ ] **Step 2: Replace the posture paragraph**

Replace the §"Plaintext / E2EE posture" paragraph (`protocol.md:437`) with a description of the segmented format: segment 0 keeps `request_id` / `op` / `client_id` / `ok` / `watch_id` in the clear so the relay can gate on `isReadOnlyFSOperation` and route; everything else is sealed. Note that a keyed sender always emits its sealed segment so segment count signals key state, that seal failure is fail-closed (diverging from §612's fallback), and that `.env*` is readable remotely only while sealing is in effect.

- [ ] **Step 3: Update AGENTS.md §22**

Append to the allocated list: `/ FS_REQUEST=0x38 / FS_RESPONSE=0x39 / FS_EVENT=0x3a`.

- [ ] **Step 4: Verify no stale claims remain**

Run: `grep -n "明文" docs/spec/protocol.md | grep -i "fs_"`
Expected: no line still calling FS payloads plaintext.

- [ ] **Step 5: Commit**

```bash
git add docs/spec/protocol.md AGENTS.md
git commit -m "docs: FS frames are sealed; allocate AAD bytes 0x38-0x3a"
```

---

### Task 10: End-to-end verification

- [ ] **Step 1: Full Go suite**

Run: `go test ./... && go vet ./...`
Expected: PASS

- [ ] **Step 2: Full frontend suite and typecheck**

Run: `cd desktop/frontend && npx vitest run && npx vue-tsc --noEmit`
Expected: PASS, clean

- [ ] **Step 3: Build the desktop app**

Run: `go build ./... && cd desktop/frontend && npm run build`
Expected: clean

- [ ] **Step 4: Manual check against a real remote session**

Attach from the phone to a remote session, open the file explorer, and
confirm: a normal text file previews correctly, `.env` is readable while
the account key is unlocked, directory listings render, and editing a
file still saves. Then lock the key (or run a keyless dev build) and
confirm `.env` returns a deny error while other files still work.
