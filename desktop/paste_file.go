package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const maxPasteFileBytes = 10 * 1024 * 1024

// PasteFile handles a PASTE_FILE frame routed via relay.FilePasteHost.
// It sanitizes and dedups the filename, writes bytes to
// <cache>/paste-files/<sid>/<name>, and injects the resulting absolute
// path (no CR, no quoting) into the PTY master via h.Write.
func (h *desktopPtyHost) PasteFile(ctx context.Context, sessionID uuid.UUID, p proto.PasteFilePayload) error {
	logDebug("paste", "request %s", pasteFileLogDetails(sessionID, p))
	absPath, err := savePastedFile(sessionID, p)
	if err != nil {
		logError("paste", "save_failed %s error=%v", pasteFileLogDetails(sessionID, p), err)
		return err
	}
	logInfo("paste", "saved %s path=%q", pasteFileLogDetails(sessionID, p), absPath)
	if _, err := h.Write([]byte(absPath)); err != nil {
		logError("paste", "path_write_failed session=%s path=%q error=%v", sessionID, absPath, err)
		return err
	}
	return nil
}

func pasteFileLogDetails(sessionID uuid.UUID, p proto.PasteFilePayload) string {
	return fmt.Sprintf("session=%s filename=%q content_type=%q bytes=%d",
		sessionID, p.Filename, p.ContentType, len(p.Data))
}

// pasteFileDir returns the on-disk directory for a session's received files.
// Also reused by received_files.go to enumerate.
func pasteFileDir(sessionID uuid.UUID) (string, error) {
	base, err := appdir.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "paste-files", sessionID.String()), nil
}

func savePastedFile(sessionID uuid.UUID, p proto.PasteFilePayload) (string, error) {
	if len(p.Data) == 0 {
		return "", fmt.Errorf("paste file: empty")
	}
	if len(p.Data) > maxPasteFileBytes {
		return "", fmt.Errorf("paste file: too large (%d bytes)", len(p.Data))
	}
	dir, err := pasteFileDir(sessionID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := sanitizeAttachmentName(p.Filename)
	absPath, err := dedupFilename(dir, name)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(absPath, p.Data, 0o600); err != nil {
		return "", err
	}
	return absPath, nil
}

// sanitizeAttachmentName reduces user-supplied filenames to a safe basename:
// no directory parts, no control chars, NFC-normalized, ≤128 chars,
// Windows reserved names prefixed with '_', empty → "file".
func sanitizeAttachmentName(raw string) string {
	// Fold Windows separators to forward slash, then take the last segment.
	name := strings.ReplaceAll(raw, "\\", "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)

	// Strip control chars, NUL, and any remaining path separators.
	name = strings.Map(func(r rune) rune {
		if r == 0 || unicode.IsControl(r) || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)

	name = norm.NFC.String(name)

	if name == "" || name == "." || name == ".." {
		return "file"
	}

	// Length cap: 128 runes, preserve extension when possible.
	if runes := []rune(name); len(runes) > 128 {
		ext := filepath.Ext(name)
		extRunes := []rune(ext)
		if len(extRunes) > 32 {
			ext = ""
			extRunes = nil
		}
		stem := []rune(strings.TrimSuffix(name, ext))
		keep := 128 - len(extRunes)
		if keep < 1 {
			keep = 1
		}
		if keep > len(stem) {
			keep = len(stem)
		}
		name = string(stem[:keep]) + ext
	}

	// Windows reserved names (case-insensitive), with or without extension.
	upper := strings.ToUpper(name)
	upperStem := strings.TrimSuffix(upper, filepath.Ext(upper))
	switch upperStem {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}

	return name
}

// dedupFilename returns an available absolute path in dir. If name is free,
// returns dir/name; otherwise dir/base (N).ext for N=1..999. Uses O_EXCL to
// atomically reserve the name (creating a zero-byte placeholder). Callers
// then write the real bytes with os.WriteFile which overwrites the placeholder.
func dedupFilename(dir, name string) (string, error) {
	tryCreate := func(p string) bool {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return false
		}
		_ = f.Close()
		return true
	}

	candidate := filepath.Join(dir, name)
	if tryCreate(candidate) {
		return candidate, nil
	}

	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 1; n <= 999; n++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		if tryCreate(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("paste file: giving up after 999 dedup attempts")
}
