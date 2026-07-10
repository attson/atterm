package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/attson/atterm/internal/appdir"
	"github.com/google/uuid"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ReceivedFilesSummary is the aggregate on-disk view of files delivered by
// remote clients via PASTE_FILE. Bound to the frontend Settings tab.
type ReceivedFilesSummary struct {
	TotalBytes int64                       `json:"total_bytes"`
	Sessions   []ReceivedFilesSessionEntry `json:"sessions"`
}

// ReceivedFilesSessionEntry groups received files by originating session.
type ReceivedFilesSessionEntry struct {
	SessionID   string              `json:"session_id"`
	SessionName string              `json:"session_name"`
	Bytes       int64               `json:"bytes"`
	Files       []ReceivedFileEntry `json:"files"`
}

// ReceivedFileEntry is a single received file on disk.
type ReceivedFileEntry struct {
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes"`
	ReceivedAt int64  `json:"received_at"` // nanoseconds since epoch
}

func receivedFilesRoot() (string, error) {
	base, err := appdir.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "paste-files"), nil
}

// ReceivedFilesList enumerates every session directory under the paste-files
// cache root and returns per-session file lists plus an aggregate byte total.
func (a *App) ReceivedFilesList() (ReceivedFilesSummary, error) {
	root, err := receivedFilesRoot()
	if err != nil {
		return ReceivedFilesSummary{}, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return ReceivedFilesSummary{}, nil
		}
		return ReceivedFilesSummary{}, err
	}

	summary := ReceivedFilesSummary{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := uuid.Parse(e.Name()); err != nil {
			continue
		}
		sd := filepath.Join(root, e.Name())
		files, err := os.ReadDir(sd)
		if err != nil {
			continue
		}
		entry := ReceivedFilesSessionEntry{SessionID: e.Name()}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			entry.Files = append(entry.Files, ReceivedFileEntry{
				Name:       f.Name(),
				Bytes:      info.Size(),
				ReceivedAt: info.ModTime().UnixNano(),
			})
			entry.Bytes += info.Size()
		}
		if len(entry.Files) == 0 {
			continue
		}
		sort.Slice(entry.Files, func(i, j int) bool {
			return entry.Files[i].ReceivedAt > entry.Files[j].ReceivedAt
		})
		summary.Sessions = append(summary.Sessions, entry)
		summary.TotalBytes += entry.Bytes
	}
	sort.Slice(summary.Sessions, func(i, j int) bool {
		return summary.Sessions[i].SessionID < summary.Sessions[j].SessionID
	})
	return summary, nil
}

// ReceivedFilesClearAll removes the entire paste-files cache root.
func (a *App) ReceivedFilesClearAll() error {
	root, err := receivedFilesRoot()
	if err != nil {
		return err
	}
	return os.RemoveAll(root)
}

// ReceivedFilesClearSession removes a single session's inbox directory.
func (a *App) ReceivedFilesClearSession(sessionID string) error {
	if _, err := uuid.Parse(sessionID); err != nil {
		return fmt.Errorf("invalid session id")
	}
	root, err := receivedFilesRoot()
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(root, sessionID))
}

// ReceivedFilesDelete removes a single received file inside a session's
// inbox. The filename is validated to be a plain basename (no separators,
// no traversal) to prevent escaping the inbox root.
func (a *App) ReceivedFilesDelete(sessionID, filename string) error {
	if _, err := uuid.Parse(sessionID); err != nil {
		return fmt.Errorf("invalid session id")
	}
	if filename == "" ||
		strings.ContainsRune(filename, '/') ||
		strings.ContainsRune(filename, '\\') ||
		strings.ContainsRune(filename, filepath.Separator) ||
		filename != filepath.Clean(filename) ||
		filename == "." || filename == ".." {
		return fmt.Errorf("invalid filename")
	}
	root, err := receivedFilesRoot()
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(root, sessionID, filename))
}

// ReceivedFilesOpenDir opens the cache root in the OS file browser.
func (a *App) ReceivedFilesOpenDir() error {
	root, err := receivedFilesRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	wailsruntime.BrowserOpenURL(a.ctx, "file://"+root)
	return nil
}
