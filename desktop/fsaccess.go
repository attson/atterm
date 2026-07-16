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

type fsAccess struct {
	// allowRoots holds the set of directories accepted as containers for path
	// arguments. PluginFS seeds this from $HOME plus active local session cwds.
	allowRoots []string

	watchOnce    sync.Once
	watcher      *fsnotify.Watcher
	watches      map[int64]string
	watchPaths   map[string]int
	debounce     map[string]*time.Timer
	watchSeq     int64 // guarded by mu
	mu           sync.Mutex
	ctx          context.Context
	onDirChanged func(string)
}

func newFSAccess(allowRoots []string) *fsAccess {
	roots := append([]string(nil), allowRoots...)
	return &fsAccess{allowRoots: roots}
}

var (
	ErrPathRelative  = errors.New("plugin_fs: path must be absolute")
	ErrPathForbidden = errors.New("plugin_fs: path forbidden")
	ErrPathDenied    = errors.New("plugin_fs: path denied by policy")
)

// denyExact and denySuffix express paths that are never visible regardless
// of allowRoots. The check is run on the fully-resolved path.
var denyExact = []string{".ssh", ".gnupg", ".aws"}
var denySuffix = []string{".env"}

func isDenied(resolved string) bool {
	base := filepath.Base(resolved)
	for _, d := range denyExact {
		if base == d {
			return true
		}
	}
	for _, suf := range denySuffix {
		if base == suf || strings.HasPrefix(base, suf+".") {
			return true
		}
	}
	// Also walk segments for nested ~/.ssh inside an allowed root.
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

// resolve normalizes path, follows symlinks, and checks against allowRoots
// and deny patterns. Returns the cleaned absolute path on success.
func (a *fsAccess) resolve(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrPathRelative
	}
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		// If the path itself does not yet exist (e.g. for a Reveal call on
		// a freshly-created file that has not yet been observed by us), fall
		// back to the lexical clean. Allowlist still applies.
		resolved = clean
	}
	if isDenied(resolved) {
		return "", fmt.Errorf("%w: %s", ErrPathDenied, resolved)
	}
	for _, root := range a.allowRoots {
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

const (
	maxReadBytesHard = 5 * 1024 * 1024 // server-side hard cap (5 MB)
	binaryProbeBytes = 4096            // bytes inspected for NUL -> binary
	readChunkMax     = 256 * 1024
)

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

type fileChunk struct {
	Path        string `json:"path"`
	Offset      int64  `json:"offset"`
	Data        []byte `json:"data"`
	ContentType string `json:"contentType"`
	EOF         bool   `json:"eof"`
}

var osReadDir = func(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

// DirEntry is a serialized representation of one directory entry.
type DirEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"modTime,omitempty"` // unix ms
}

func (a *fsAccess) listDir(path string) ([]DirEntry, error) {
	resolved, err := a.resolve(path)
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
		out = append(out, DirEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
		})
	}
	return out, nil
}

func (a *fsAccess) readFile(path string, maxBytes int64) (FileContent, error) {
	if maxBytes > maxReadBytesHard {
		return FileContent{}, fmt.Errorf("plugin_fs: maxBytes %d exceeds hard cap %d", maxBytes, maxReadBytesHard)
	}
	resolved, err := a.resolve(path)
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
	f, err := os.Open(resolved)
	if err != nil {
		return FileContent{}, err
	}
	defer f.Close()

	size := info.Size()
	readLen := size
	truncated := int64(0)
	if size > maxBytes {
		readLen = maxBytes
		truncated = size
	}
	data := make([]byte, readLen)
	if _, err := io.ReadFull(f, data); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return FileContent{}, err
	}
	return FileContent{Path: resolved, Data: data, IsBinary: isBinary(data), TruncatedAt: truncated}, nil
}

func (a *fsAccess) fileMeta(path string) (FileMetaInfo, error) {
	resolved, err := a.resolve(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FileMetaInfo{}, err
	}
	isBin := false
	if !info.IsDir() {
		f, err := os.Open(resolved)
		if err == nil {
			probe := make([]byte, binaryProbeBytes)
			n, _ := f.Read(probe)
			f.Close()
			isBin = isBinary(probe[:n])
		}
	}
	return FileMetaInfo{
		Path:     resolved,
		Size:     info.Size(),
		ModTime:  info.ModTime().UnixMilli(),
		IsBinary: isBin,
	}, nil
}

func (a *fsAccess) readChunk(path string, offset int64, length int64) (fileChunk, error) {
	if offset < 0 {
		return fileChunk{}, fmt.Errorf("plugin_fs: offset must be non-negative")
	}
	if length < 0 {
		return fileChunk{}, fmt.Errorf("plugin_fs: length must be non-negative")
	}
	if length > readChunkMax {
		length = readChunkMax
	}
	resolved, err := a.resolve(path)
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
	f, err := os.Open(resolved)
	if err != nil {
		return fileChunk{}, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return fileChunk{}, err
	}
	data := make([]byte, length)
	n, err := f.Read(data)
	if err != nil && !errors.Is(err, io.EOF) {
		return fileChunk{}, err
	}
	data = data[:n]
	contentType := "application/octet-stream"
	if len(data) > 0 {
		contentType = http.DetectContentType(data)
	}
	return fileChunk{
		Path:        resolved,
		Offset:      offset,
		Data:        data,
		ContentType: contentType,
		EOF:         offset+int64(n) >= info.Size(),
	}, nil
}

func isBinary(data []byte) bool {
	probe := data
	if int64(len(probe)) > binaryProbeBytes {
		probe = probe[:binaryProbeBytes]
	}
	for _, b := range probe {
		if b == 0 {
			return true
		}
	}
	return false
}

const (
	maxWatchers    = 200
	debounceWindow = 100 * time.Millisecond
)

func (a *fsAccess) setupWatcher(ctx context.Context, onDirChanged func(string)) {
	a.watchOnce.Do(func() {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			return
		}
		a.watcher = w
		a.watches = make(map[int64]string)
		a.watchPaths = make(map[string]int)
		a.debounce = make(map[string]*time.Timer)
		a.ctx = ctx
		a.onDirChanged = onDirChanged
		go a.watcherLoop(ctx, w)
	})
}

func (a *fsAccess) shutdownWatcher() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.watcher != nil {
		_ = a.watcher.Close()
		a.watcher = nil
	}
	for _, timer := range a.debounce {
		timer.Stop()
	}
	a.debounce = nil
	a.watches = nil
	a.watchPaths = nil
}

func (a *fsAccess) watchDir(path string) (int64, error) {
	resolved, err := a.resolve(path)
	if err != nil {
		return 0, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.watcher == nil {
		return 0, fmt.Errorf("plugin_fs: watcher not available")
	}
	if len(a.watches) >= maxWatchers {
		return 0, fmt.Errorf("plugin_fs: watcher cap %d reached", maxWatchers)
	}
	if a.watchPaths[resolved] == 0 {
		if err := a.watcher.Add(resolved); err != nil {
			return 0, err
		}
	}
	a.watchSeq++
	id := a.watchSeq
	a.watches[id] = resolved
	a.watchPaths[resolved]++
	return id, nil
}

func (a *fsAccess) unwatchDir(handleID int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	path, ok := a.watches[handleID]
	if !ok {
		return nil
	}
	delete(a.watches, handleID)
	a.watchPaths[path]--
	if a.watchPaths[path] <= 0 {
		delete(a.watchPaths, path)
		if a.watcher != nil {
			_ = a.watcher.Remove(path)
		}
	}
	return nil
}

func (a *fsAccess) watcherLoop(ctx context.Context, watcher *fsnotify.Watcher) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			dir := filepath.Dir(ev.Name)
			a.scheduleDirChanged(dir)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			_ = err
		}
	}
}

func (a *fsAccess) scheduleDirChanged(dir string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.debounce == nil {
		return
	}
	if t, ok := a.debounce[dir]; ok {
		t.Stop()
	}
	a.debounce[dir] = time.AfterFunc(debounceWindow, func() {
		a.mu.Lock()
		delete(a.debounce, dir)
		callback := a.onDirChanged
		a.mu.Unlock()
		if callback != nil {
			callback(dir)
		}
	})
}
