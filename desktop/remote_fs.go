package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

type remoteFS struct {
	access  *fsAccess
	eventCh chan proto.Frame

	cancel context.CancelFunc
	// driverClientID returns the current local driver client id for a session.
	// It is required for open_external because that op triggers owner-machine OS
	// behavior rather than a read-only filesystem response.
	driverClientID func(uuid.UUID) (string, bool)

	mu             sync.Mutex
	watchesByID    map[string]remoteFSWatch
	watchIDsByPath map[string]map[string]struct{}
}

type remoteFSWatch struct {
	handleID int64
	path     string
	session  uuid.UUID
}

func newRemoteFS(access *fsAccess) *remoteFS {
	ctx, cancel := context.WithCancel(context.Background())
	fs := &remoteFS{
		access:         access,
		eventCh:        make(chan proto.Frame, 64),
		cancel:         cancel,
		watchesByID:    make(map[string]remoteFSWatch),
		watchIDsByPath: make(map[string]map[string]struct{}),
	}
	access.setupWatcher(ctx, fs.onDirChanged)
	return fs
}

func (fs *remoteFS) close() {
	fs.cancel()
	fs.access.shutdownWatcher()
}

func remoteFSAllowRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{home}
}

func (fs *remoteFS) eventsChan() <-chan proto.Frame {
	return fs.eventCh
}

func (fs *remoteFS) events() <-chan proto.Frame {
	return fs.eventsChan()
}

func (fs *remoteFS) handle(sessionID uuid.UUID, req proto.FSRequestPayload) proto.Frame {
	response := proto.FSResponsePayload{RequestID: req.RequestID, OK: true}
	var err error

	switch req.Op {
	case "list_dir":
		var entries []DirEntry
		entries, err = fs.access.listDir(req.Path)
		response.Entries = mapDirEntries(entries)
	case "file_meta":
		var meta FileMetaInfo
		meta, err = fs.access.fileMeta(req.Path)
		if err == nil {
			response.Meta = &proto.FileMetaInfo{
				Path:     meta.Path,
				Size:     meta.Size,
				ModTime:  meta.ModTime,
				IsBinary: meta.IsBinary,
			}
		}
	case "read_file":
		var content FileContent
		content, err = fs.access.readFile(req.Path, req.MaxBytes)
		if err == nil {
			response.Content = &proto.FileContent{
				Path:        content.Path,
				Data:        content.Data,
				IsBinary:    content.IsBinary,
				TruncatedAt: content.TruncatedAt,
			}
		}
	case "read_chunk":
		var chunk fileChunk
		chunk, err = fs.access.readChunk(req.Path, req.Offset, req.Length)
		if err == nil {
			response.Chunk = &proto.FSChunkPayload{
				Path:        chunk.Path,
				Data:        chunk.Data,
				Offset:      chunk.Offset,
				Length:      int64(len(chunk.Data)),
				EOF:         chunk.EOF,
				ContentType: chunk.ContentType,
			}
		}
	case "watch_dir":
		var watchID string
		watchID, err = fs.watchDir(sessionID, req.Path)
		response.WatchID = watchID
	case "unwatch_dir":
		err = fs.unwatchDir(req.WatchID)
	case "open_external":
		if err = fs.requireOpenExternalDriver(sessionID, req.ClientID); err == nil {
			err = fs.openExternal(req.Path)
		}
	case "write_file":
		var meta FileMetaInfo
		meta, err = fs.access.writeFile(req.Path, req.Data, req.ExpectedModTime, req.CreateIfMissing)
		if err == nil {
			response.Meta = &proto.FileMetaInfo{
				Path:     meta.Path,
				Size:     meta.Size,
				ModTime:  meta.ModTime,
				IsBinary: meta.IsBinary,
			}
		}
	case "create_file":
		var meta FileMetaInfo
		meta, err = fs.access.createFile(req.Path)
		if err == nil {
			response.Meta = &proto.FileMetaInfo{
				Path:    meta.Path,
				Size:    meta.Size,
				ModTime: meta.ModTime,
			}
		}
	case "rename":
		var meta FileMetaInfo
		meta, err = fs.access.renamePath(req.Path, req.NewPath)
		if err == nil {
			response.Meta = &proto.FileMetaInfo{
				Path:    meta.Path,
				Size:    meta.Size,
				ModTime: meta.ModTime,
			}
		}
	case "remove":
		err = fs.access.removePath(req.Path, req.Recursive)
	case "mkdir":
		var meta FileMetaInfo
		meta, err = fs.access.mkdir(req.Path)
		if err == nil {
			response.Meta = &proto.FileMetaInfo{
				Path:    meta.Path,
				Size:    meta.Size,
				ModTime: meta.ModTime,
			}
		}
	case "trash":
		err = fs.access.trashPath(req.Path)
	default:
		err = fmt.Errorf("remote_fs: unsupported op %q", req.Op)
	}
	if err != nil {
		response.OK = false
		response.Error = err.Error()
		response.Entries = nil
		response.Meta = nil
		response.Content = nil
		response.Chunk = nil
		response.WatchID = ""
	}
	return remoteFSResponseFrame(sessionID, response)
}

func mapDirEntries(entries []DirEntry) []proto.DirEntry {
	out := make([]proto.DirEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, proto.DirEntry{
			Name:    entry.Name,
			IsDir:   entry.IsDir,
			Size:    entry.Size,
			ModTime: entry.ModTime,
		})
	}
	return out
}

func (fs *remoteFS) watchDir(sessionID uuid.UUID, path string) (string, error) {
	resolved, err := fs.access.resolve(path)
	if err != nil {
		return "", err
	}
	handleID, err := fs.access.watchDir(resolved)
	if err != nil {
		return "", err
	}
	watchID := strconv.FormatInt(handleID, 10)

	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.watchesByID[watchID] = remoteFSWatch{handleID: handleID, path: resolved, session: sessionID}
	if fs.watchIDsByPath[resolved] == nil {
		fs.watchIDsByPath[resolved] = make(map[string]struct{})
	}
	fs.watchIDsByPath[resolved][watchID] = struct{}{}
	return watchID, nil
}

func (fs *remoteFS) unwatchDir(watchID string) error {
	fs.mu.Lock()
	watch, ok := fs.watchesByID[watchID]
	if ok {
		delete(fs.watchesByID, watchID)
		if ids := fs.watchIDsByPath[watch.path]; ids != nil {
			delete(ids, watchID)
			if len(ids) == 0 {
				delete(fs.watchIDsByPath, watch.path)
			}
		}
	}
	fs.mu.Unlock()
	if !ok {
		return nil
	}
	return fs.access.unwatchDir(watch.handleID)
}

func (fs *remoteFS) openExternal(path string) error {
	resolved, err := fs.access.resolve(path)
	if err != nil {
		return err
	}
	return openExternalPath(resolved)
}

func (fs *remoteFS) requireOpenExternalDriver(sessionID uuid.UUID, clientID string) error {
	if clientID == "" || fs.driverClientID == nil {
		return fmt.Errorf("driver_required")
	}
	driverID, ok := fs.driverClientID(sessionID)
	if !ok || driverID == "" || driverID != clientID {
		return fmt.Errorf("driver_required")
	}
	return nil
}

func (fs *remoteFS) onDirChanged(path string) {
	fs.mu.Lock()
	watches := make([]remoteFSWatch, 0, len(fs.watchIDsByPath[path]))
	for id := range fs.watchIDsByPath[path] {
		if watch, ok := fs.watchesByID[id]; ok {
			watches = append(watches, watch)
		}
	}
	fs.mu.Unlock()

	for _, watch := range watches {
		payload, err := json.Marshal(proto.FSEventPayload{
			WatchID: strconv.FormatInt(watch.handleID, 10),
			Path:    path,
			Event:   "changed",
		})
		if err != nil {
			continue
		}
		frame := proto.Frame{Type: proto.TypeFSEvent, SessionID: watch.session, Payload: payload}
		select {
		case fs.eventCh <- frame:
		default:
		}
	}
}

func remoteFSResponseFrame(sessionID uuid.UUID, payload proto.FSResponsePayload) proto.Frame {
	body, err := json.Marshal(payload)
	if err != nil {
		body, _ = json.Marshal(proto.FSResponsePayload{
			RequestID: payload.RequestID,
			OK:        false,
			Error:     err.Error(),
		})
	}
	return proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: body}
}

func handleRemoteFSRequest(ctx context.Context, out chan<- proto.Frame, sessionID uuid.UUID, remotePermission string, fs *remoteFS, req proto.FSRequestPayload) bool {
	var frame proto.Frame
	if remotePermission != proto.RemotePermissionFull {
		frame = remoteFSResponseFrame(sessionID, proto.FSResponsePayload{
			RequestID: req.RequestID,
			OK:        false,
			Error:     "remote filesystem requires full remote permission",
		})
	} else if fs == nil {
		frame = remoteFSResponseFrame(sessionID, proto.FSResponsePayload{
			RequestID: req.RequestID,
			OK:        false,
			Error:     "remote filesystem unavailable",
		})
	} else {
		frame = fs.handle(sessionID, req)
	}
	select {
	case out <- frame:
		return true
	case <-ctx.Done():
		return false
	}
}
