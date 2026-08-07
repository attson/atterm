package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func makeRemoteFS(t *testing.T) (*remoteFS, *fsAccess, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("evalsymlinks tempdir: %v", err)
	}
	// Sealing-in-effect posture by default; the deny case builds its own.
	access := newFSAccess([]string{root}, false)
	fs := newRemoteFS(access, nil)
	t.Cleanup(fs.close)
	return fs, access, root
}

// makeRemoteFSDenyingEnv builds the keyless-remote posture: no account
// key, so no sealing, so .env stays denied.
func makeRemoteFSDenyingEnv(t *testing.T) (*remoteFS, *fsAccess, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("evalsymlinks tempdir: %v", err)
	}
	access := newFSAccess([]string{root}, true)
	fs := newRemoteFS(access, nil)
	t.Cleanup(fs.close)
	return fs, access, root
}

func decodeFSResponse(t *testing.T, f proto.Frame) proto.FSResponsePayload {
	t.Helper()
	if f.Type != proto.TypeFSResponse {
		t.Fatalf("frame type = %v, want FS_RESPONSE", f.Type)
	}
	// Keyless by default: makeRemoteFS builds an agent with no account
	// key, so responses come back as a single plaintext segment.
	resp, err := proto.DecodeFSResponse(f.Payload, nil, f.SessionID)
	if err != nil {
		t.Fatalf("decode FS_RESPONSE: %v", err)
	}
	return resp
}

func recvRemoteFSEvent(t *testing.T, fs *remoteFS) proto.FSEventPayload {
	t.Helper()
	select {
	case f := <-fs.events():
		if f.Type != proto.TypeFSEvent {
			t.Fatalf("event frame type = %v, want FS_EVENT", f.Type)
		}
		event, err := proto.DecodeFSEvent(f.Payload, fs.sessionKey(f.SessionID), f.SessionID)
		if err != nil {
			t.Fatalf("decode FS_EVENT: %v", err)
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for FS_EVENT")
		return proto.FSEventPayload{}
	}
}

func assertNoRemoteFSEvent(t *testing.T, fs *remoteFS) {
	t.Helper()
	select {
	case f := <-fs.events():
		t.Fatalf("unexpected FS event frame: %+v", f)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRemoteFSListDirAndReadChunkSuccess(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	sessionID := uuid.New()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello remote fs"), 0o644); err != nil {
		t.Fatal(err)
	}

	listFrame := fs.handle(sessionID, proto.FSRequestPayload{RequestID: "list-1", Op: "list_dir", Path: root})
	listResp := decodeFSResponse(t, listFrame)
	if !listResp.OK {
		t.Fatalf("list_dir failed: %s", listResp.Error)
	}
	if listResp.RequestID != "list-1" {
		t.Fatalf("request_id = %q", listResp.RequestID)
	}
	if len(listResp.Entries) != 1 || listResp.Entries[0].Name != "note.txt" || listResp.Entries[0].Size != 15 {
		t.Fatalf("unexpected entries: %+v", listResp.Entries)
	}

	readFrame := fs.handle(sessionID, proto.FSRequestPayload{
		RequestID: "chunk-1",
		Op:        "read_chunk",
		Path:      filepath.Join(root, "note.txt"),
		Offset:    6,
		Length:    6,
	})
	readResp := decodeFSResponse(t, readFrame)
	if !readResp.OK {
		t.Fatalf("read_chunk failed: %s", readResp.Error)
	}
	if readResp.Chunk == nil {
		t.Fatal("expected chunk payload")
	}
	if string(readResp.Chunk.Data) != "remote" {
		t.Fatalf("chunk data = %q", string(readResp.Chunk.Data))
	}
	if readResp.Chunk.Offset != 6 || readResp.Chunk.Length != int64(len(readResp.Chunk.Data)) || readResp.Chunk.EOF {
		t.Fatalf("unexpected chunk metadata: %+v", readResp.Chunk)
	}
}

func TestRemoteFSDeniedPathReturnsError(t *testing.T) {
	// Keyless remote: .env would cross the relay in the clear, so it is
	// denied. See TestRemoteFSEnvReadableWhenSealing for the other half.
	fs, _, root := makeRemoteFSDenyingEnv(t)
	path := filepath.Join(root, ".env")
	if err := os.WriteFile(path, []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}

	frame := fs.handle(uuid.New(), proto.FSRequestPayload{RequestID: "denied-1", Op: "file_meta", Path: path})
	resp := decodeFSResponse(t, frame)
	if resp.OK {
		t.Fatalf("expected denied response, got %+v", resp)
	}
	if resp.RequestID != "denied-1" || !strings.Contains(resp.Error, "denied") {
		t.Fatalf("unexpected denied response: %+v", resp)
	}
}

func TestRemoteFSOpenExternalResolvesBeforeHelper(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	realPath := filepath.Join(root, "real.txt")
	linkPath := filepath.Join(root, "link.txt")
	if err := os.WriteFile(realPath, []byte("open me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	resolvedRealPath, err := filepath.EvalSymlinks(realPath)
	if err != nil {
		t.Fatal(err)
	}

	var opened []string
	orig := openExternalPath
	openExternalPath = func(path string) error {
		opened = append(opened, path)
		return nil
	}
	t.Cleanup(func() { openExternalPath = orig })

	sessionID := uuid.New()
	req := proto.FSRequestPayload{RequestID: "open-1", ClientID: "driver-1", Op: "open_external", Path: linkPath}
	fs.driverClientID = func(id uuid.UUID) (string, bool) {
		if id != sessionID {
			return "", false
		}
		return "driver-1", true
	}
	frame := fs.handle(sessionID, req)
	resp := decodeFSResponse(t, frame)
	if !resp.OK {
		t.Fatalf("open_external failed: %s", resp.Error)
	}
	if len(opened) != 1 || opened[0] != resolvedRealPath {
		t.Fatalf("openExternalPath calls = %+v, want [%q]", opened, resolvedRealPath)
	}

	deniedReq := proto.FSRequestPayload{RequestID: "open-denied", ClientID: "driver-1", Op: "open_external", Path: t.TempDir()}
	frame = fs.handle(sessionID, deniedReq)
	resp = decodeFSResponse(t, frame)
	if resp.OK {
		t.Fatalf("expected forbidden response, got %+v", resp)
	}
	if len(opened) != 1 {
		t.Fatalf("openExternalPath called after failed resolve: %+v", opened)
	}
}

func TestRemoteFSOpenExternalRequiresCurrentDriverClientID(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	path := filepath.Join(root, "open.txt")
	if err := os.WriteFile(path, []byte("open me"), 0o644); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.New()
	fs.driverClientID = func(id uuid.UUID) (string, bool) {
		if id != sessionID {
			return "", false
		}
		return "driver-1", true
	}

	var opened []string
	orig := openExternalPath
	openExternalPath = func(path string) error {
		opened = append(opened, path)
		return nil
	}
	t.Cleanup(func() { openExternalPath = orig })

	cases := []struct {
		name     string
		clientID string
		wantErr  string
	}{
		{name: "missing", clientID: "", wantErr: "driver_required"},
		{name: "mismatch", clientID: "viewer-1", wantErr: "driver_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := proto.FSRequestPayload{RequestID: tc.name, ClientID: tc.clientID, Op: "open_external", Path: path}
			resp := decodeFSResponse(t, fs.handle(sessionID, req))
			if resp.OK || resp.Error != tc.wantErr {
				t.Fatalf("open_external response = %+v, want error %q", resp, tc.wantErr)
			}
		})
	}
	if len(opened) != 0 {
		t.Fatalf("openExternalPath called for non-driver request: %+v", opened)
	}

	req := proto.FSRequestPayload{RequestID: "driver", ClientID: "driver-1", Op: "open_external", Path: path}
	resp := decodeFSResponse(t, fs.handle(sessionID, req))
	if !resp.OK {
		t.Fatalf("driver open_external failed: %s", resp.Error)
	}
	if len(opened) != 1 {
		t.Fatalf("openExternalPath calls = %+v, want one call", opened)
	}
}

func TestRemoteFSWatchDirEmitsEventWithReturnedWatchID(t *testing.T) {
	fs, access, root := makeRemoteFS(t)
	sessionID := uuid.New()

	frame := fs.handle(sessionID, proto.FSRequestPayload{RequestID: "watch-1", Op: "watch_dir", Path: root})
	resp := decodeFSResponse(t, frame)
	if !resp.OK {
		t.Fatalf("watch_dir failed: %s", resp.Error)
	}
	if resp.WatchID == "" {
		t.Fatal("expected watch_id")
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	access.onDirChanged(resolvedRoot)
	event := recvRemoteFSEvent(t, fs)
	if event.WatchID != resp.WatchID || event.Path != resolvedRoot || event.Event != "changed" {
		t.Fatalf("unexpected FS_EVENT: %+v, watch response %+v", event, resp)
	}
}

func TestRemoteFSUnwatchDirRemovesWatchMapping(t *testing.T) {
	fs, access, root := makeRemoteFS(t)
	sessionID := uuid.New()

	watchResp := decodeFSResponse(t, fs.handle(sessionID, proto.FSRequestPayload{RequestID: "watch-1", Op: "watch_dir", Path: root}))
	if !watchResp.OK {
		t.Fatalf("watch_dir failed: %s", watchResp.Error)
	}
	unwatchResp := decodeFSResponse(t, fs.handle(sessionID, proto.FSRequestPayload{RequestID: "unwatch-1", Op: "unwatch_dir", WatchID: watchResp.WatchID}))
	if !unwatchResp.OK {
		t.Fatalf("unwatch_dir failed: %s", unwatchResp.Error)
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	access.onDirChanged(resolvedRoot)
	assertNoRemoteFSEvent(t, fs)
}

func TestRemoteFSPermissionGateRequiresFullPermission(t *testing.T) {
	out := make(chan proto.Frame, 1)
	ok := handleRemoteFSRequest(context.Background(), out, uuid.New(), proto.RemotePermissionControl, nil, proto.FSRequestPayload{
		RequestID: "permission-1",
		Op:        "list_dir",
		Path:      "/tmp",
	})
	if !ok {
		t.Fatal("expected permission response to be queued")
	}
	resp := decodeFSResponse(t, <-out)
	if resp.OK || resp.RequestID != "permission-1" || !strings.Contains(resp.Error, "full") {
		t.Fatalf("unexpected permission response: %+v", resp)
	}
}

func TestRemoteFSPermissionGateRejectsEmptyAndUnknownPermissionBeforeFSWork(t *testing.T) {
	for _, remotePermission := range []string{"", "bogus"} {
		t.Run("permission_"+remotePermission, func(t *testing.T) {
			out := make(chan proto.Frame, 1)
			ok := handleRemoteFSRequest(context.Background(), out, uuid.New(), remotePermission, nil, proto.FSRequestPayload{
				RequestID: "permission",
				Op:        "list_dir",
				Path:      "/tmp",
			})
			if !ok {
				t.Fatal("expected permission response to be queued")
			}
			resp := decodeFSResponse(t, <-out)
			if resp.OK || resp.RequestID != "permission" || !strings.Contains(resp.Error, "full") {
				t.Fatalf("unexpected permission response for %q: %+v", remotePermission, resp)
			}
			if strings.Contains(resp.Error, "unavailable") {
				t.Fatalf("permission %q reached fs availability check: %+v", remotePermission, resp)
			}
		})
	}
}

func TestRemoteFSWriteFileSuccess(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	frame := fs.handle(uuid.New(), proto.FSRequestPayload{
		RequestID:       "r",
		Op:              "write_file",
		Path:            target,
		Data:            []byte("new"),
		ExpectedModTime: info.ModTime().UnixMilli(),
	})
	resp := decodeFSResponse(t, frame)
	if !resp.OK {
		t.Fatalf("expected ok, got %q", resp.Error)
	}
	if resp.Meta == nil || resp.Meta.Size != 3 {
		t.Fatalf("unexpected meta: %+v", resp.Meta)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("body = %q, want %q", got, "new")
	}
}

func TestRemoteFSWriteFileStaleModTime(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	frame := fs.handle(uuid.New(), proto.FSRequestPayload{
		RequestID:       "r",
		Op:              "write_file",
		Path:            target,
		Data:            []byte("nope"),
		ExpectedModTime: 1,
	})
	resp := decodeFSResponse(t, frame)
	if resp.OK {
		t.Fatal("expected stale_modtime failure")
	}
	if !strings.Contains(resp.Error, "stale_modtime") {
		t.Fatalf("expected stale_modtime error, got %q", resp.Error)
	}
}

func TestRemoteFSCreateRenameRemoveMkdir(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	sid := uuid.New()

	respFor := func(payload proto.FSRequestPayload) proto.FSResponsePayload {
		return decodeFSResponse(t, fs.handle(sid, payload))
	}

	created := respFor(proto.FSRequestPayload{RequestID: "1", Op: "create_file", Path: filepath.Join(root, "a")})
	if !created.OK {
		t.Fatalf("create_file: %s", created.Error)
	}
	if created.Meta == nil {
		t.Fatal("create_file meta missing")
	}

	renamed := respFor(proto.FSRequestPayload{
		RequestID: "2", Op: "rename",
		Path:    filepath.Join(root, "a"),
		NewPath: filepath.Join(root, "b"),
	})
	if !renamed.OK {
		t.Fatalf("rename: %s", renamed.Error)
	}

	mk := respFor(proto.FSRequestPayload{RequestID: "3", Op: "mkdir", Path: filepath.Join(root, "sub")})
	if !mk.OK {
		t.Fatalf("mkdir: %s", mk.Error)
	}

	rm := respFor(proto.FSRequestPayload{RequestID: "4", Op: "remove", Path: filepath.Join(root, "b")})
	if !rm.OK {
		t.Fatalf("remove: %s", rm.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "b")); err == nil {
		t.Fatal("remove left file behind")
	}
}

func TestRemoteFSUnsupportedOpStillReports(t *testing.T) {
	fs, _, _ := makeRemoteFS(t)
	frame := fs.handle(uuid.New(), proto.FSRequestPayload{
		RequestID: "u", Op: "not_a_real_op",
	})
	resp := decodeFSResponse(t, frame)
	if resp.OK {
		t.Fatal("expected error")
	}
	if !strings.Contains(resp.Error, "unsupported op") {
		t.Fatalf("expected unsupported op error, got %q", resp.Error)
	}
}

func TestRemoteFSPermissionGateBlocksWrite(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := make(chan proto.Frame, 1)
	ok := handleRemoteFSRequest(context.Background(), out, uuid.New(), proto.RemotePermissionControl, fs, proto.FSRequestPayload{
		RequestID: "p",
		Op:        "write_file",
		Path:      target,
		Data:      []byte("y"),
	})
	if !ok {
		t.Fatal("expected response queued")
	}
	resp := decodeFSResponse(t, <-out)
	if resp.OK {
		t.Fatal("expected permission-denied")
	}
	if !strings.Contains(resp.Error, "full") {
		t.Fatalf("expected full-permission error, got %q", resp.Error)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "x" {
		t.Fatalf("file mutated despite gate: %q", got)
	}
}

func fsSealTestKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

func TestRemoteFSSealsResponseWhenKeyed(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	key := fsSealTestKey()
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

func TestRemoteFSSealsErrorPath(t *testing.T) {
	// Agent-side errors embed the resolved path; they must be sealed too.
	fs, _, root := makeRemoteFS(t)
	key := fsSealTestKey()
	fs.accountKey = func() []byte { return key }
	sid := uuid.New()
	missing := filepath.Join(root, "nope.txt")
	frame := fs.handle(sid, proto.FSRequestPayload{RequestID: "e1", Op: "read_file", Path: missing, MaxBytes: 1024})
	if strings.Contains(string(frame.Payload), "nope.txt") {
		t.Fatal("error message leaked the path in plaintext")
	}
	sk, _ := e2eecrypto.DeriveSessionKey(key, sid)
	out, err := proto.DecodeFSResponse(frame.Payload, sk, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out.OK || !strings.Contains(out.Error, "nope.txt") {
		t.Fatalf("error did not survive sealing: %+v", out)
	}
}

func TestRemoteFSSealsEventPath(t *testing.T) {
	fs, access, root := makeRemoteFS(t)
	key := fsSealTestKey()
	fs.accountKey = func() []byte { return key }
	sid := uuid.New()
	if _, err := fs.watchDir(sid, root); err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	access.onDirChanged(resolvedRoot)
	select {
	case frame := <-fs.events():
		if strings.Contains(string(frame.Payload), resolvedRoot) {
			t.Fatal("event path present in plaintext")
		}
		sk, _ := e2eecrypto.DeriveSessionKey(key, sid)
		out, err := proto.DecodeFSEvent(frame.Payload, sk, sid)
		if err != nil {
			t.Fatal(err)
		}
		if out.Path != resolvedRoot {
			t.Fatalf("path did not survive: %q", out.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("no event emitted")
	}
}

func TestRemoteFSEnvReadableWhenSealing(t *testing.T) {
	// The keyed remote posture: sealing is in effect, so .env is served —
	// and its bytes never appear in plaintext on the wire.
	fs, _, root := makeRemoteFS(t)
	key := fsSealTestKey()
	fs.accountKey = func() []byte { return key }
	path := filepath.Join(root, ".env")
	if err := os.WriteFile(path, []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	sid := uuid.New()
	frame := fs.handle(sid, proto.FSRequestPayload{RequestID: "env-1", Op: "read_file", Path: path, MaxBytes: 1024})
	if strings.Contains(string(frame.Payload), "SECRET=1") {
		t.Fatal(".env bytes present in plaintext on the wire")
	}
	sk, err := e2eecrypto.DeriveSessionKey(key, sid)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := proto.DecodeFSResponse(frame.Payload, sk, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Content == nil || string(resp.Content.Data) != "SECRET=1" {
		t.Fatalf("expected .env to be readable, got %+v (err=%q)", resp.Content, resp.Error)
	}
}
