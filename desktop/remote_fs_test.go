package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func makeRemoteFS(t *testing.T) (*remoteFS, *fsAccess, string) {
	t.Helper()
	root := t.TempDir()
	access := newFSAccess([]string{root})
	fs := newRemoteFS(access)
	t.Cleanup(fs.close)
	return fs, access, root
}

func decodeFSResponse(t *testing.T, f proto.Frame) proto.FSResponsePayload {
	t.Helper()
	if f.Type != proto.TypeFSResponse {
		t.Fatalf("frame type = %v, want FS_RESPONSE", f.Type)
	}
	var resp proto.FSResponsePayload
	if err := json.Unmarshal(f.Payload, &resp); err != nil {
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
		var event proto.FSEventPayload
		if err := json.Unmarshal(f.Payload, &event); err != nil {
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
	fs, _, root := makeRemoteFS(t)
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

	frame := fs.handle(uuid.New(), proto.FSRequestPayload{RequestID: "open-1", Op: "open_external", Path: linkPath})
	resp := decodeFSResponse(t, frame)
	if !resp.OK {
		t.Fatalf("open_external failed: %s", resp.Error)
	}
	if len(opened) != 1 || opened[0] != resolvedRealPath {
		t.Fatalf("openExternalPath calls = %+v, want [%q]", opened, resolvedRealPath)
	}

	frame = fs.handle(uuid.New(), proto.FSRequestPayload{RequestID: "open-denied", Op: "open_external", Path: t.TempDir()})
	resp = decodeFSResponse(t, frame)
	if resp.OK {
		t.Fatalf("expected forbidden response, got %+v", resp)
	}
	if len(opened) != 1 {
		t.Fatalf("openExternalPath called after failed resolve: %+v", opened)
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
