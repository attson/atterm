package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

type remoteFS struct {
	access  *fsAccess
	eventCh chan proto.Frame
	// accountKey returns the unlocked E2EE account key, or nil when the
	// agent is keyless. Whether we seal depends only on this — never on
	// any inbound field — so the relay cannot downgrade a sealed session
	// by tampering with a request.
	accountKey func() []byte

	cancel context.CancelFunc
	// driverClientID returns the current local driver client id for a session.
	// It is required for open_external because that op triggers owner-machine OS
	// behavior rather than a read-only filesystem response.
	driverClientID func(uuid.UUID) (string, bool)

	// exec runs one request and produces its response frame. Nil in
	// production, where handle dispatches to execute. Tests replace it to
	// stand in for a filesystem slow enough to matter (SFTP round trips, a
	// network mount, a sleeping external drive).
	exec func(uuid.UUID, proto.FSRequestPayload) proto.Frame

	mu             sync.Mutex
	watchesByID    map[string]remoteFSWatch
	watchIDsByPath map[string]map[string]struct{}
}

type remoteFSWatch struct {
	handleID int64
	path     string
	session  uuid.UUID
}

func newRemoteFS(access *fsAccess, accountKey func() []byte) *remoteFS {
	ctx, cancel := context.WithCancel(context.Background())
	fs := &remoteFS{
		access:         access,
		accountKey:     accountKey,
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

func (fs *remoteFS) events() <-chan proto.Frame {
	return fs.eventCh
}

// sessionKey derives the per-session key for sealing, or nil when this
// agent holds no account key. A nil result puts the encoders on the
// plaintext path, which is also what keeps the .env deny switched on.
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

// handle runs one request through the executor in force for this remoteFS.
// Callers must assume it can block for as long as the underlying filesystem
// takes; it is never called from the uplink read loop (see fsWorkerPool).
func (fs *remoteFS) handle(sessionID uuid.UUID, req proto.FSRequestPayload) proto.Frame {
	if fs.exec != nil {
		return fs.exec(sessionID, req)
	}
	return fs.execute(sessionID, req)
}

func (fs *remoteFS) execute(sessionID uuid.UUID, req proto.FSRequestPayload) proto.Frame {
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
	return remoteFSResponseFrame(sessionID, response, fs.sessionKey(sessionID))
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
		payload, err := proto.EncodeFSEvent(proto.FSEventPayload{
			WatchID: strconv.FormatInt(watch.handleID, 10),
			Path:    path,
			Event:   "changed",
		}, fs.sessionKey(watch.session), watch.session)
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

func remoteFSResponseFrame(sessionID uuid.UUID, payload proto.FSResponsePayload, sessionKey []byte) proto.Frame {
	body, err := proto.EncodeFSResponse(payload, sessionKey, sessionID)
	if err != nil {
		// Fail closed. Unlike the OUT/META paths (protocol.md §612), a
		// seal error here must never degrade to plaintext: the .env deny
		// is lifted on the basis that sealing is in effect, so a silent
		// fallback would put secrets on the wire exactly when the guard
		// is down. Emit a keyless error carrying no path instead.
		body, _ = proto.EncodeFSHead(proto.FSResponsePayload{
			RequestID: payload.RequestID,
			OK:        false,
			Error:     "response encoding failed",
		})
	}
	return proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: body}
}

// fsRequestsPerSession bounds how many filesystem requests one session may
// have in flight at once. Bounded is the point: an unbounded fan-out lets a
// single wedged remote filesystem pile up work without limit, and one global
// budget would let one session's stuck listing starve every other session.
//
// The budget is per session, so remote clients browsing the same session
// share it. Four is comfortably above what the file explorer generates — it
// awaits each op, and even its chunked asset reads are sequential — so on a
// local filesystem the cap is unreachable in practice; it bites only when
// operations are slow enough that the user needs to be told.
const fsRequestsPerSession = 4

// fsBusyError is what a caller gets when its session is already at its
// budget. We refuse instead of queueing on purpose: queueing hides the
// latency until the user concludes their click did nothing, and a refusal
// they can see beats a wait they cannot.
const fsBusyError = "filesystem busy: too many requests in flight for this session"

// fsWorkerPool runs filesystem requests off the uplink read loop. That loop
// also carries every session's keystrokes, so anything slow on it (an SFTP
// round trip, a network mount, a sleeping external drive) reads to the user
// as a frozen terminal.
//
// Lifetime is the connection's: workers stop writing once ctx is done. A
// worker already inside a filesystem call cannot be interrupted — it runs to
// completion and then drops its response — so nothing here waits on one.
type fsWorkerPool struct {
	ctx              context.Context
	out              chan<- proto.Frame
	remotePermission string
	fs               *remoteFS
	limit            int

	mu       sync.Mutex
	inflight map[uuid.UUID]int
}

func newFSWorkerPool(ctx context.Context, out chan<- proto.Frame, remotePermission string, fs *remoteFS, limit int) *fsWorkerPool {
	if limit <= 0 {
		limit = fsRequestsPerSession
	}
	return &fsWorkerPool{
		ctx:              ctx,
		out:              out,
		remotePermission: remotePermission,
		fs:               fs,
		limit:            limit,
		inflight:         make(map[uuid.UUID]int),
	}
}

// submit hands one request to a worker and returns immediately. It never
// blocks on the filesystem, and it never blocks waiting for a slot.
func (p *fsWorkerPool) submit(sessionID uuid.UUID, req proto.FSRequestPayload) {
	if !p.acquire(sessionID) {
		p.sendBusy(sessionID, req)
		return
	}
	go func() {
		defer p.release(sessionID)
		handleRemoteFSRequest(p.ctx, p.out, sessionID, p.remotePermission, p.fs, req)
	}()
}

func (p *fsWorkerPool) acquire(sessionID uuid.UUID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inflight[sessionID] >= p.limit {
		return false
	}
	p.inflight[sessionID]++
	return true
}

func (p *fsWorkerPool) release(sessionID uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inflight[sessionID] <= 1 {
		delete(p.inflight, sessionID)
		return
	}
	p.inflight[sessionID]--
}

// sendBusy answers a refused request. Sealed like any other response, and
// non-blocking: the read loop calls this, so a backed-up writer must not
// stall it. A dropped busy response degrades to the client's own request
// timeout, which is the same outcome a stalled link produces anyway.
func (p *fsWorkerPool) sendBusy(sessionID uuid.UUID, req proto.FSRequestPayload) {
	frame := remoteFSResponseFrame(sessionID, proto.FSResponsePayload{
		RequestID: req.RequestID,
		OK:        false,
		Error:     fsBusyError,
	}, p.fs.sessionKey(sessionID))
	select {
	case p.out <- frame:
	default:
		logWarn("uplink", "out chan full; dropping fs busy response session=%s", sessionID)
	}
}

// handleRemoteFSRequest executes one request and queues its response. It runs
// on a pool worker, never on the uplink read loop: the permission and
// availability checks are cheap, but fs.handle is not.
func handleRemoteFSRequest(ctx context.Context, out chan<- proto.Frame, sessionID uuid.UUID, remotePermission string, fs *remoteFS, req proto.FSRequestPayload) bool {
	var frame proto.Frame
	if remotePermission != proto.RemotePermissionFull {
		frame = remoteFSResponseFrame(sessionID, proto.FSResponsePayload{
			RequestID: req.RequestID,
			OK:        false,
			Error:     "remote filesystem requires full remote permission",
		}, fs.sessionKey(sessionID))
	} else if fs == nil {
		// fs is nil here, so sessionKey() returns nil and this goes out
		// as single-segment plaintext. The message carries no path.
		frame = remoteFSResponseFrame(sessionID, proto.FSResponsePayload{
			RequestID: req.RequestID,
			OK:        false,
			Error:     "remote filesystem unavailable",
		}, nil)
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
