package sftpfs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sync"

	"github.com/pkg/sftp"
)

const (
	transferTempPrefix = ".atterm-transfer-"
	transferTempSuffix = ".partial"
)

type ReadHandle interface {
	io.Reader
	io.Closer
}

type StagedWriter interface {
	io.Writer
	Commit(context.Context, bool) error
	Abort() error
}

// Getwd returns the login directory reported by the SFTP server.
func (c *Client) Getwd(ctx context.Context) (string, error) {
	var cwd string
	if err := c.do(ctx, func() error {
		var err error
		cwd, err = c.sc.Getwd()
		return err
	}); err != nil {
		return "", fmt.Errorf("sftp getwd: %w", err)
	}
	return cwd, nil
}

// Lstat reads metadata without following the final symlink.
func (c *Client) Lstat(ctx context.Context, name string) (os.FileInfo, error) {
	p, err := cleanPath(name)
	if err != nil {
		return nil, err
	}
	var info os.FileInfo
	if err := c.do(ctx, func() error {
		var err error
		info, err = c.sc.Lstat(p)
		return err
	}); err != nil {
		return nil, translate("lstat", p, err)
	}
	return info, nil
}

// OpenReader returns a streaming reader whose context cancellation closes the
// remote handle. The caller still owns Close on the normal path.
func (c *Client) OpenReader(ctx context.Context, name string) (ReadHandle, error) {
	p, err := cleanPath(name)
	if err != nil {
		return nil, err
	}
	var file *sftp.File
	if err := c.do(ctx, func() error {
		var openErr error
		file, openErr = c.sc.Open(p)
		if openErr == nil && ctx.Err() != nil {
			_ = file.Close()
			return ctx.Err()
		}
		return openErr
	}); err != nil {
		return nil, translate("open", p, err)
	}
	return newWatchedFile(ctx, file), nil
}

// CreateStagedWriter opens a random partial file beside target. Commit is the
// only operation that can publish it at target; Abort removes it.
func (c *Client) CreateStagedWriter(ctx context.Context, target string) (StagedWriter, error) {
	p, err := cleanPath(target)
	if err != nil {
		return nil, err
	}
	tmp, err := transferTempPath(path.Dir(p))
	if err != nil {
		return nil, translate("stage", p, err)
	}

	var file *sftp.File
	if err := c.do(ctx, func() error {
		var openErr error
		file, openErr = c.sc.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if openErr == nil && ctx.Err() != nil {
			_ = file.Close()
			_ = c.sc.Remove(tmp)
			return ctx.Err()
		}
		return openErr
	}); err != nil {
		return nil, translate("stage", p, err)
	}

	return &stagedWriter{
		client: c,
		file:   newWatchedFile(ctx, file),
		target: p,
		tmp:    tmp,
	}, nil
}

func transferTempPath(dir string) (string, error) {
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return path.Join(dir, transferTempPrefix+hex.EncodeToString(suffix[:])+transferTempSuffix), nil
}

type watchedFile struct {
	file     *sftp.File
	stop     chan struct{}
	closeOne sync.Once
	closeErr error
}

func newWatchedFile(ctx context.Context, file *sftp.File) *watchedFile {
	h := &watchedFile{file: file, stop: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			_ = h.Close()
		case <-h.stop:
		}
	}()
	return h
}

func (h *watchedFile) Read(p []byte) (int, error)  { return h.file.Read(p) }
func (h *watchedFile) Write(p []byte) (int, error) { return h.file.Write(p) }

func (h *watchedFile) sync() error { return h.file.Sync() }

func (h *watchedFile) Close() error {
	h.closeOne.Do(func() {
		close(h.stop)
		h.closeErr = h.file.Close()
	})
	return h.closeErr
}

type stagedWriter struct {
	client *Client
	file   *watchedFile
	target string
	tmp    string

	finishOne sync.Once
	finishErr error
}

func (w *stagedWriter) Write(p []byte) (int, error) { return w.file.Write(p) }

func (w *stagedWriter) Commit(ctx context.Context, replace bool) error {
	w.finishOne.Do(func() {
		w.finishErr = w.commit(ctx, replace)
	})
	return w.finishErr
}

func (w *stagedWriter) commit(ctx context.Context, replace bool) error {
	if err := ctx.Err(); err != nil {
		_ = w.abortFile()
		return err
	}
	if err := w.file.sync(); err != nil && !isUnsupportedSFTP(err) {
		_ = w.abortFile()
		return translate("write", w.target, err)
	}
	if err := w.file.Close(); err != nil {
		_ = w.abortFile()
		return translate("write", w.target, err)
	}

	err := w.client.do(ctx, func() error {
		if !replace {
			if _, statErr := w.client.sc.Lstat(w.target); statErr == nil {
				return ErrAlreadyExists
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			}
			return w.client.sc.Rename(w.tmp, w.target)
		}

		if w.client.posixRename {
			return w.client.sc.PosixRename(w.tmp, w.target)
		}
		if _, statErr := w.client.sc.Lstat(w.target); statErr == nil {
			return ErrAtomicReplace
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		return w.client.sc.Rename(w.tmp, w.target)
	})
	if err != nil {
		_ = w.client.sc.Remove(w.tmp)
		return translate("write", w.target, err)
	}
	return nil
}

func isUnsupportedSFTP(err error) bool {
	var status *sftp.StatusError
	return errors.As(err, &status) && status.FxCode() == sftp.ErrSSHFxOpUnsupported
}

func (w *stagedWriter) Abort() error {
	w.finishOne.Do(func() {
		w.finishErr = w.abortFile()
	})
	return w.finishErr
}

func (w *stagedWriter) abortFile() error {
	closeErr := w.file.Close()
	removeErr := w.client.sc.Remove(w.tmp)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}
