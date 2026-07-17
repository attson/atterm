package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	trashpkg "github.com/attson/atterm/internal/trash"
)

// Sentinel errors — every one is also formatted with a stable prefix so the
// remote transport layer surfaces it as a machine-parseable Error string.
var (
	ErrStaleModTime  = errors.New("stale_modtime")
	ErrAlreadyExists = errors.New("already_exists")
	ErrNotFound      = errors.New("not_found")
	ErrIsDirectory   = errors.New("is_directory")
	ErrNotADirectory = errors.New("not_a_directory")
)

// writeFile writes data atomically to path (tmp + rename). expectedModTime=0
// disables the CAS check; createIfMissing=true allows creating a new file
// that has never existed. Otherwise the target must exist and its ModTime
// must equal expectedModTime, else ErrStaleModTime is returned.
func (a *fsAccess) writeFile(path string, data []byte, expectedModTime int64, createIfMissing bool) (FileMetaInfo, error) {
	if int64(len(data)) > maxWriteBytesHard {
		return FileMetaInfo{}, fmt.Errorf("write_denied: exceeds %d bytes", maxWriteBytesHard)
	}
	resolved, err := a.resolve(path)
	if err != nil {
		return FileMetaInfo{}, err
	}

	info, statErr := osStat(resolved)
	switch {
	case statErr == nil:
		if info.IsDir() {
			return FileMetaInfo{}, fmt.Errorf("%w", ErrIsDirectory)
		}
		if expectedModTime != 0 && info.ModTime().UnixMilli() != expectedModTime {
			return FileMetaInfo{}, fmt.Errorf("%w: current=%d", ErrStaleModTime, info.ModTime().UnixMilli())
		}
	case errors.Is(statErr, os.ErrNotExist):
		if !createIfMissing {
			return FileMetaInfo{}, fmt.Errorf("%w: %s", ErrNotFound, resolved)
		}
	default:
		return FileMetaInfo{}, statErr
	}

	dir := filepath.Dir(resolved)
	tmp, err := osCreateTemp(dir, ".atterm-tmp-*")
	if err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	tmpName := tmp.Name()
	commit := false
	defer func() {
		_ = tmp.Close()
		if !commit {
			_ = osRemove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	if err := osRename(tmpName, resolved); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	commit = true

	newInfo, err := osStat(resolved)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return FileMetaInfo{
		Path:     resolved,
		Size:     newInfo.Size(),
		ModTime:  newInfo.ModTime().UnixMilli(),
		IsBinary: isBinary(data),
	}, nil
}

// createFile creates path as an empty file. Fails with ErrAlreadyExists if
// anything is already at path.
func (a *fsAccess) createFile(path string) (FileMetaInfo, error) {
	resolved, err := a.resolve(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	if _, err := osStat(resolved); err == nil {
		return FileMetaInfo{}, fmt.Errorf("%w: %s", ErrAlreadyExists, resolved)
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileMetaInfo{}, err
	}
	if err := osWriteFile(resolved, nil, 0o644); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	info, err := osStat(resolved)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return FileMetaInfo{
		Path:    resolved,
		Size:    info.Size(),
		ModTime: info.ModTime().UnixMilli(),
	}, nil
}

// renamePath moves from → to. Both endpoints go through resolve() so allowRoots
// and deny gates apply symmetrically.
func (a *fsAccess) renamePath(from, to string) (FileMetaInfo, error) {
	src, err := a.resolve(from)
	if err != nil {
		return FileMetaInfo{}, err
	}
	dst, err := a.resolve(to)
	if err != nil {
		return FileMetaInfo{}, err
	}
	if _, err := osStat(src); errors.Is(err, os.ErrNotExist) {
		return FileMetaInfo{}, fmt.Errorf("%w: %s", ErrNotFound, src)
	} else if err != nil {
		return FileMetaInfo{}, err
	}
	if _, err := osStat(dst); err == nil {
		return FileMetaInfo{}, fmt.Errorf("%w: %s", ErrAlreadyExists, dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileMetaInfo{}, err
	}
	if err := osRename(src, dst); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	info, err := osStat(dst)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return FileMetaInfo{
		Path:    dst,
		Size:    info.Size(),
		ModTime: info.ModTime().UnixMilli(),
	}, nil
}

// removePath deletes path. recursive=false uses os.Remove (fails on non-empty
// directories); recursive=true uses os.RemoveAll.
func (a *fsAccess) removePath(path string, recursive bool) error {
	resolved, err := a.resolve(path)
	if err != nil {
		return err
	}
	if _, err := osStat(resolved); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, resolved)
	} else if err != nil {
		return err
	}
	if recursive {
		if err := osRemoveAll(resolved); err != nil {
			return fmt.Errorf("write_denied: %w", err)
		}
		return nil
	}
	if err := osRemove(resolved); err != nil {
		return fmt.Errorf("write_denied: %w", err)
	}
	return nil
}

// mkdir creates a single directory. Parent must already exist. Fails with
// ErrAlreadyExists if anything is already at path.
func (a *fsAccess) mkdir(path string) (FileMetaInfo, error) {
	resolved, err := a.resolve(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	if _, err := osStat(resolved); err == nil {
		return FileMetaInfo{}, fmt.Errorf("%w: %s", ErrAlreadyExists, resolved)
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileMetaInfo{}, err
	}
	if err := osMkdir(resolved, 0o755); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	info, err := osStat(resolved)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return FileMetaInfo{
		Path:    resolved,
		Size:    info.Size(),
		ModTime: info.ModTime().UnixMilli(),
	}, nil
}

// trashPath moves the file to the OS trash. Returns trashpkg.ErrUnavailable
// when no supported trash command exists on the host, so callers can offer a
// hard-delete fallback.
func (a *fsAccess) trashPath(path string) error {
	resolved, err := a.resolve(path)
	if err != nil {
		return err
	}
	if _, err := osStat(resolved); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, resolved)
	} else if err != nil {
		return err
	}
	if err := trashpkg.Send(resolved); err != nil {
		if errors.Is(err, trashpkg.ErrUnavailable) {
			return err
		}
		return fmt.Errorf("write_denied: %w", err)
	}
	return nil
}
