package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type localTransferEndpoint struct {
	access *fsAccess
}

func newLocalTransferEndpoint(access *fsAccess) transferEndpoint {
	return &localTransferEndpoint{access: access}
}

func (e *localTransferEndpoint) ID() string { return "local" }

func (e *localTransferEndpoint) Clean(name string) (string, error) {
	if !filepath.IsAbs(name) {
		return "", ErrPathRelative
	}
	clean := filepath.Clean(name)
	if e.access.isDenied(clean) {
		return "", fmt.Errorf("%w: %s", ErrPathDenied, clean)
	}

	// Resolve the parent, not the final component. The engine must see a final
	// symlink through Lstat so it can skip it instead of following it.
	for _, root := range e.access.allowRoots {
		rootClean := filepath.Clean(root)
		if clean == rootClean {
			return e.access.resolve(rootClean)
		}
	}
	parent, err := e.access.resolve(filepath.Dir(clean))
	if err != nil {
		return "", err
	}
	target := filepath.Join(parent, filepath.Base(clean))
	if e.access.isDenied(target) {
		return "", fmt.Errorf("%w: %s", ErrPathDenied, target)
	}
	return target, nil
}

func (e *localTransferEndpoint) Join(base string, parts ...string) string {
	return filepath.Join(append([]string{base}, parts...)...)
}

func (e *localTransferEndpoint) Base(name string) string { return filepath.Base(name) }

func (e *localTransferEndpoint) Lstat(_ context.Context, name string) (transferEntry, error) {
	clean, err := e.Clean(name)
	if err != nil {
		return transferEntry{}, err
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return transferEntry{}, err
	}
	return localTransferEntry(clean, info), nil
}

func localTransferEntry(name string, info os.FileInfo) transferEntry {
	entry := transferEntry{SourcePath: name, Size: info.Size(), ModTime: info.ModTime().UnixMilli()}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		entry.Kind = transferSkipped
		entry.SkipReason = "symlink"
	case info.IsDir():
		entry.Kind = transferDir
	case info.Mode().IsRegular():
		entry.Kind = transferFile
	default:
		entry.Kind = transferSkipped
		entry.SkipReason = "unsupported file type"
	}
	return entry
}

func (e *localTransferEndpoint) List(ctx context.Context, name string) ([]transferEntry, bool, error) {
	clean, err := e.Clean(name)
	if err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	entries, err := os.ReadDir(clean)
	if err != nil {
		return nil, false, err
	}
	out := make([]transferEntry, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		child := filepath.Join(clean, entry.Name())
		info, err := os.Lstat(child)
		if err != nil {
			return nil, false, err
		}
		out = append(out, localTransferEntry(child, info))
	}
	return out, false, nil
}

func (e *localTransferEndpoint) OpenReader(ctx context.Context, name string) (io.ReadCloser, error) {
	entry, err := e.Lstat(ctx, name)
	if err != nil {
		return nil, err
	}
	if entry.Kind != transferFile {
		return nil, fmt.Errorf("open %s: not a regular file", name)
	}
	file, err := os.Open(entry.SourcePath)
	if err != nil {
		return nil, err
	}
	return newLocalWatchedFile(ctx, file), nil
}

func (e *localTransferEndpoint) CreateStagedWriter(ctx context.Context, name string) (transferStagedWriter, error) {
	target, err := e.Clean(name)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".atterm-transfer-*.partial")
	if err != nil {
		return nil, err
	}
	return &localStagedWriter{
		file:   newLocalWatchedFile(ctx, file),
		tmp:    file.Name(),
		target: target,
	}, nil
}

func (e *localTransferEndpoint) Mkdir(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clean, err := e.Clean(name)
	if err != nil {
		return err
	}
	return os.Mkdir(clean, 0o755)
}

func (e *localTransferEndpoint) Remove(ctx context.Context, name string, recursive bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clean, err := e.Clean(name)
	if err != nil {
		return err
	}
	if recursive {
		return os.RemoveAll(clean)
	}
	return os.Remove(clean)
}

func (e *localTransferEndpoint) Close() error { return nil }

type localWatchedFile struct {
	file     *os.File
	stop     chan struct{}
	closeOne sync.Once
	closeErr error
}

func newLocalWatchedFile(ctx context.Context, file *os.File) *localWatchedFile {
	h := &localWatchedFile{file: file, stop: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			_ = h.Close()
		case <-h.stop:
		}
	}()
	return h
}

func (f *localWatchedFile) Read(p []byte) (int, error)  { return f.file.Read(p) }
func (f *localWatchedFile) Write(p []byte) (int, error) { return f.file.Write(p) }
func (f *localWatchedFile) Sync() error                 { return f.file.Sync() }

func (f *localWatchedFile) Close() error {
	f.closeOne.Do(func() {
		close(f.stop)
		f.closeErr = f.file.Close()
	})
	return f.closeErr
}

type localStagedWriter struct {
	file   *localWatchedFile
	tmp    string
	target string

	finishOne sync.Once
	finishErr error
}

func (w *localStagedWriter) Write(p []byte) (int, error) { return w.file.Write(p) }

func (w *localStagedWriter) Commit(ctx context.Context, replace bool) error {
	w.finishOne.Do(func() {
		w.finishErr = w.commit(ctx, replace)
	})
	return w.finishErr
}

func (w *localStagedWriter) commit(ctx context.Context, replace bool) error {
	if err := ctx.Err(); err != nil {
		_ = w.abortFile()
		return err
	}
	if err := w.file.Sync(); err != nil {
		_ = w.abortFile()
		return err
	}
	if err := w.file.Close(); err != nil {
		_ = w.abortFile()
		return err
	}

	if !replace {
		if err := os.Link(w.tmp, w.target); err != nil {
			_ = os.Remove(w.tmp)
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("%w: %s", ErrAlreadyExists, w.target)
			}
			return err
		}
		return os.Remove(w.tmp)
	}
	if err := os.Rename(w.tmp, w.target); err == nil {
		return nil
	}
	if err := os.Remove(w.target); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(w.tmp)
		return err
	}
	if err := os.Rename(w.tmp, w.target); err != nil {
		_ = os.Remove(w.tmp)
		return err
	}
	return nil
}

func (w *localStagedWriter) Abort() error {
	w.finishOne.Do(func() {
		w.finishErr = w.abortFile()
	})
	return w.finishErr
}

func (w *localStagedWriter) abortFile() error {
	closeErr := w.file.Close()
	removeErr := os.Remove(w.tmp)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}
