package main

import (
	"context"
	"io"
	"os"
	"path"
	"sync"

	"github.com/attson/atterm/internal/sftpfs"
)

type sftpTransferClient interface {
	ListDir(context.Context, string) (sftpfs.Listing, error)
	Lstat(context.Context, string) (os.FileInfo, error)
	OpenReader(context.Context, string) (sftpfs.ReadHandle, error)
	CreateStagedWriter(context.Context, string) (sftpfs.StagedWriter, error)
	Mkdir(context.Context, string) (sftpfs.FileMetaInfo, error)
	Remove(context.Context, string, bool) error
}

type sftpTransferEndpoint struct {
	hostID  string
	client  sftpTransferClient
	release func()

	closeOne sync.Once
}

func newSFTPTransferEndpointForClient(hostID string, client sftpTransferClient, release func()) transferEndpoint {
	return &sftpTransferEndpoint{hostID: hostID, client: client, release: release}
}

func (e *sftpTransferEndpoint) ID() string { return "ssh:" + e.hostID }

func (e *sftpTransferEndpoint) Clean(name string) (string, error) {
	return sftpResolvePath(name)
}

func (e *sftpTransferEndpoint) Join(base string, parts ...string) string {
	return path.Join(append([]string{base}, parts...)...)
}

func (e *sftpTransferEndpoint) Base(name string) string { return path.Base(name) }

func (e *sftpTransferEndpoint) Lstat(ctx context.Context, name string) (transferEntry, error) {
	clean, err := e.Clean(name)
	if err != nil {
		return transferEntry{}, err
	}
	info, err := e.client.Lstat(ctx, clean)
	if err != nil {
		return transferEntry{}, err
	}
	return sftpTransferEntry(clean, info), nil
}

func sftpTransferEntry(name string, info os.FileInfo) transferEntry {
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

func (e *sftpTransferEndpoint) List(ctx context.Context, name string) ([]transferEntry, bool, error) {
	clean, err := e.Clean(name)
	if err != nil {
		return nil, false, err
	}
	listing, err := e.client.ListDir(ctx, clean)
	if err != nil {
		return nil, false, err
	}
	entries := make([]transferEntry, 0, len(listing.Entries))
	for _, item := range listing.Entries {
		entries = append(entries, transferEntry{SourcePath: path.Join(clean, item.Name)})
	}
	return entries, listing.Truncated, nil
}

func (e *sftpTransferEndpoint) OpenReader(ctx context.Context, name string) (io.ReadCloser, error) {
	clean, err := e.Clean(name)
	if err != nil {
		return nil, err
	}
	return e.client.OpenReader(ctx, clean)
}

func (e *sftpTransferEndpoint) CreateStagedWriter(ctx context.Context, name string) (transferStagedWriter, error) {
	clean, err := e.Clean(name)
	if err != nil {
		return nil, err
	}
	return e.client.CreateStagedWriter(ctx, clean)
}

func (e *sftpTransferEndpoint) Mkdir(ctx context.Context, name string) error {
	clean, err := e.Clean(name)
	if err != nil {
		return err
	}
	_, err = e.client.Mkdir(ctx, clean)
	return err
}

func (e *sftpTransferEndpoint) Remove(ctx context.Context, name string, recursive bool) error {
	clean, err := e.Clean(name)
	if err != nil {
		return err
	}
	return e.client.Remove(ctx, clean, recursive)
}

func (e *sftpTransferEndpoint) Close() error {
	e.closeOne.Do(func() {
		if e.release != nil {
			e.release()
		}
	})
	return nil
}
