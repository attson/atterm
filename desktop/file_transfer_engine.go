package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

const transferCopyBufferSize = 256 << 10

type transferKind string

const (
	transferFile    transferKind = "file"
	transferDir     transferKind = "dir"
	transferSkipped transferKind = "skipped"
)

type transferEntry struct {
	SourcePath   string
	RelativePath string
	Kind         transferKind
	Size         int64
	ModTime      int64
	SkipReason   string
}

type transferEndpoint interface {
	ID() string
	Clean(string) (string, error)
	Join(string, ...string) string
	Base(string) string
	Lstat(context.Context, string) (transferEntry, error)
	List(context.Context, string) ([]transferEntry, bool, error)
	OpenReader(context.Context, string) (io.ReadCloser, error)
	CreateStagedWriter(context.Context, string) (transferStagedWriter, error)
	Mkdir(context.Context, string) error
	Remove(context.Context, string, bool) error
	Close() error
}

type transferStagedWriter interface {
	io.Writer
	Commit(context.Context, bool) error
	Abort() error
}

type transferConflict string

const (
	conflictSkip    transferConflict = "skip"
	conflictReplace transferConflict = "replace"
	conflictRename  transferConflict = "rename"
)

type FileTransferProgress struct {
	State        string `json:"state"`
	CurrentPath  string `json:"current_path"`
	BytesDone    int64  `json:"bytes_done"`
	BytesTotal   int64  `json:"bytes_total"`
	EntriesDone  int    `json:"entries_done"`
	EntriesTotal int    `json:"entries_total"`
}

type FileTransferResult struct {
	State            string                     `json:"state"`
	Error            string                     `json:"error,omitempty"`
	Warnings         []string                   `json:"warnings,omitempty"`
	HostKey          *FileTransferHostKeyPrompt `json:"host_key,omitempty"`
	CredentialHostID string                     `json:"credential_host_id,omitempty"`
}

func buildTransferManifest(ctx context.Context, src transferEndpoint, paths []string) ([]transferEntry, error) {
	manifest := make([]transferEntry, 0, len(paths))
	for _, raw := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		clean, err := src.Clean(raw)
		if err != nil {
			return nil, err
		}
		if err := appendManifestEntry(ctx, src, clean, src.Base(clean), &manifest); err != nil {
			return nil, err
		}
	}
	return manifest, nil
}

func appendManifestEntry(ctx context.Context, src transferEndpoint, sourcePath, relativePath string, manifest *[]transferEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, err := src.Lstat(ctx, sourcePath)
	if err != nil {
		return err
	}
	entry.SourcePath = sourcePath
	entry.RelativePath = path.Clean(strings.ReplaceAll(relativePath, "\\", "/"))
	*manifest = append(*manifest, entry)
	if entry.Kind != transferDir {
		return nil
	}

	children, truncated, err := src.List(ctx, sourcePath)
	if err != nil {
		return err
	}
	if truncated {
		return fmt.Errorf("transfer directory %s: listing is truncated", sourcePath)
	}
	sort.Slice(children, func(i, j int) bool { return children[i].SourcePath < children[j].SourcePath })
	for _, child := range children {
		childPath := child.SourcePath
		if childPath == "" {
			name := child.RelativePath
			if name == "" {
				return fmt.Errorf("transfer directory %s: child has no path", sourcePath)
			}
			childPath = src.Join(sourcePath, name)
		}
		if err := appendManifestEntry(ctx, src, childPath, path.Join(entry.RelativePath, src.Base(childPath)), manifest); err != nil {
			return err
		}
	}
	return nil
}

func runTransfer(
	ctx context.Context,
	src, dst transferEndpoint,
	manifest []transferEntry,
	dstDir string,
	policy transferConflict,
	report func(FileTransferProgress),
) FileTransferResult {
	if policy != conflictSkip && policy != conflictReplace && policy != conflictRename {
		return failedTransfer(fmt.Errorf("unsupported conflict policy %q", policy))
	}
	cleanDstDir, err := dst.Clean(dstDir)
	if err != nil {
		return failedTransfer(err)
	}
	if err := validateTransferDestination(src, dst, manifest, cleanDstDir); err != nil {
		return failedTransfer(err)
	}

	progress := FileTransferProgress{State: "running", EntriesTotal: len(manifest)}
	for _, entry := range manifest {
		if entry.Kind == transferFile {
			progress.BytesTotal += entry.Size
		}
	}
	emitTransferProgress(report, progress)

	result := FileTransferResult{State: "completed", Warnings: []string{}}
	remapped := map[string]string{}
	skippedDirs := map[string]bool{}
	for _, entry := range manifest {
		if err := ctx.Err(); err != nil {
			return cancelledTransfer(result.Warnings, progress, report)
		}
		progress.CurrentPath = entry.RelativePath

		if hasSkippedAncestor(entry.RelativePath, skippedDirs) {
			finishTransferEntry(&progress, entry, report)
			continue
		}
		if entry.Kind == transferSkipped {
			reason := entry.SkipReason
			if reason == "" {
				reason = "unsupported file type"
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %s", entry.RelativePath, reason))
			finishTransferEntry(&progress, entry, report)
			continue
		}

		mappedRelative := remapTransferRelative(entry.RelativePath, remapped)
		target := dst.Join(cleanDstDir, mappedRelative)
		existing, statErr := dst.Lstat(ctx, target)
		exists := statErr == nil
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return failTransferWithProgress(statErr, result.Warnings, progress, report)
		}

		if entry.Kind == transferDir {
			if exists && existing.Kind == transferDir {
				finishTransferEntry(&progress, entry, report)
				continue
			}
			if exists {
				switch policy {
				case conflictSkip:
					skippedDirs[entry.RelativePath] = true
					finishTransferEntry(&progress, entry, report)
					continue
				case conflictReplace:
					if err := dst.Remove(ctx, target, true); err != nil {
						return failTransferWithProgress(err, result.Warnings, progress, report)
					}
				case conflictRename:
					candidate, candidateRel, err := nextTransferName(ctx, dst, cleanDstDir, mappedRelative)
					if err != nil {
						return failTransferWithProgress(err, result.Warnings, progress, report)
					}
					remapped[entry.RelativePath] = candidateRel
					target = candidate
				}
			}
			if err := dst.Mkdir(ctx, target); err != nil {
				return failTransferWithProgress(err, result.Warnings, progress, report)
			}
			finishTransferEntry(&progress, entry, report)
			continue
		}

		replace := false
		if exists {
			switch policy {
			case conflictSkip:
				finishTransferEntry(&progress, entry, report)
				continue
			case conflictReplace:
				replace = true
			case conflictRename:
				var candidateRel string
				target, candidateRel, err = nextTransferName(ctx, dst, cleanDstDir, mappedRelative)
				_ = candidateRel
				if err != nil {
					return failTransferWithProgress(err, result.Warnings, progress, report)
				}
			}
		}

		reader, err := src.OpenReader(ctx, entry.SourcePath)
		if err != nil {
			return failTransferWithProgress(err, result.Warnings, progress, report)
		}
		writer, err := dst.CreateStagedWriter(ctx, target)
		if err != nil {
			_ = reader.Close()
			return failTransferWithProgress(err, result.Warnings, progress, report)
		}
		counter := &transferProgressWriter{writer: writer, progress: &progress, report: report}
		_, copyErr := io.CopyBuffer(counter, &contextTransferReader{ctx: ctx, reader: reader}, make([]byte, transferCopyBufferSize))
		closeErr := reader.Close()
		if copyErr != nil || closeErr != nil {
			_ = writer.Abort()
			if errors.Is(copyErr, context.Canceled) || errors.Is(copyErr, context.DeadlineExceeded) || ctx.Err() != nil {
				return cancelledTransfer(result.Warnings, progress, report)
			}
			return failTransferWithProgress(errors.Join(copyErr, closeErr), result.Warnings, progress, report)
		}
		if err := writer.Commit(ctx, replace); err != nil {
			_ = writer.Abort()
			if ctx.Err() != nil {
				return cancelledTransfer(result.Warnings, progress, report)
			}
			return failTransferWithProgress(err, result.Warnings, progress, report)
		}
		if after, err := src.Lstat(ctx, entry.SourcePath); err == nil && (after.Size != entry.Size || after.ModTime != entry.ModTime) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: source changed during transfer", entry.RelativePath))
		}
		progress.EntriesDone++
		emitTransferProgress(report, progress)
	}

	progress.State = "completed"
	progress.CurrentPath = ""
	emitTransferProgress(report, progress)
	return result
}

func validateTransferDestination(src, dst transferEndpoint, manifest []transferEntry, dstDir string) error {
	if src.ID() != dst.ID() {
		return nil
	}
	for _, entry := range manifest {
		if strings.Contains(entry.RelativePath, "/") {
			continue
		}
		target, err := dst.Clean(dst.Join(dstDir, entry.RelativePath))
		if err != nil {
			return err
		}
		source, err := src.Clean(entry.SourcePath)
		if err != nil {
			return err
		}
		normalizedTarget := normalizeTransferPath(target)
		normalizedSource := strings.TrimSuffix(normalizeTransferPath(source), "/")
		if normalizedTarget == normalizedSource || (entry.Kind == transferDir && strings.HasPrefix(normalizedTarget, normalizedSource+"/")) {
			return fmt.Errorf("cannot copy %s into itself or its descendant", entry.SourcePath)
		}
	}
	return nil
}

func normalizeTransferPath(p string) string {
	return strings.TrimSuffix(strings.ReplaceAll(p, "\\", "/"), "/")
}

func remapTransferRelative(relative string, mappings map[string]string) string {
	bestSource := ""
	for source := range mappings {
		if relative == source || strings.HasPrefix(relative, source+"/") {
			if len(source) > len(bestSource) {
				bestSource = source
			}
		}
	}
	if bestSource == "" {
		return relative
	}
	suffix := strings.TrimPrefix(relative, bestSource)
	return strings.TrimSuffix(mappings[bestSource], "/") + suffix
}

func hasSkippedAncestor(relative string, skipped map[string]bool) bool {
	for prefix := range skipped {
		if relative == prefix || strings.HasPrefix(relative, prefix+"/") {
			return true
		}
	}
	return false
}

func nextTransferName(ctx context.Context, dst transferEndpoint, dstDir, relative string) (string, string, error) {
	dir, base := path.Split(relative)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for n := 1; ; n++ {
		candidateRel := path.Join(dir, fmt.Sprintf("%s copy %d%s", stem, n, ext))
		candidate := dst.Join(dstDir, candidateRel)
		if _, err := dst.Lstat(ctx, candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, candidateRel, nil
		} else if err != nil {
			return "", "", err
		}
	}
}

type contextTransferReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextTransferReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if err == nil {
		if ctxErr := r.ctx.Err(); ctxErr != nil {
			return n, ctxErr
		}
	}
	return n, err
}

type transferProgressWriter struct {
	writer   io.Writer
	progress *FileTransferProgress
	report   func(FileTransferProgress)
}

func (w *transferProgressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.progress.BytesDone += int64(n)
	emitTransferProgress(w.report, *w.progress)
	return n, err
}

func finishTransferEntry(progress *FileTransferProgress, entry transferEntry, report func(FileTransferProgress)) {
	if entry.Kind == transferFile {
		progress.BytesDone += entry.Size
	}
	progress.EntriesDone++
	emitTransferProgress(report, *progress)
}

func emitTransferProgress(report func(FileTransferProgress), progress FileTransferProgress) {
	if report != nil {
		report(progress)
	}
}

func failedTransfer(err error) FileTransferResult {
	return FileTransferResult{State: "failed", Error: err.Error(), Warnings: []string{}}
}

func failTransferWithProgress(err error, warnings []string, progress FileTransferProgress, report func(FileTransferProgress)) FileTransferResult {
	progress.State = "failed"
	emitTransferProgress(report, progress)
	return FileTransferResult{State: "failed", Error: err.Error(), Warnings: warnings}
}

func cancelledTransfer(warnings []string, progress FileTransferProgress, report func(FileTransferProgress)) FileTransferResult {
	progress.State = "cancelled"
	emitTransferProgress(report, progress)
	return FileTransferResult{State: "cancelled", Warnings: warnings}
}
