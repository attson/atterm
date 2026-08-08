package main

import (
	"strings"
	"time"

	"github.com/attson/atterm/internal/logging"
)

// Frontend log records are batched in the renderer and flushed across the
// Wails boundary in bulk. The limits below bound what one flush can do to the
// log file — a runaway loop in the UI must not be able to fill the disk or
// wedge the write path.
const (
	frontendLogMaxBatch  = 512
	frontendLogMaxMsg    = 4096
	frontendLogMaxTagLen = 32
	// frontendLogTagPrefix namespaces renderer tags so they can never collide
	// with a Go-side tag. Applied here rather than in the renderer so there is
	// exactly one place that decides it.
	frontendLogTagPrefix = "ui-"
)

// FrontendLogRecord is one log line produced by the renderer.
type FrontendLogRecord struct {
	// TimestampMS is Date.now() at the moment the record was created, not when
	// it was flushed — batching must not reorder or smear the timeline.
	TimestampMS int64  `json:"timestamp_ms"`
	Level       string `json:"level"`
	Tag         string `json:"tag"`
	Message     string `json:"message"`
}

// AppendFrontendLogs writes renderer log records into the same desktop log
// file the Go side uses, so a user reporting "it wouldn't start" hands over one
// file that contains both halves of the story.
//
// Malformed records are skipped rather than rejected: a broken log line must
// never surface as an error in the UI. The return value is the number of
// records actually written, which the renderer only uses for its own
// diagnostics.
func (a *App) AppendFrontendLogs(records []FrontendLogRecord) int {
	if len(records) > frontendLogMaxBatch {
		records = records[:frontendLogMaxBatch]
	}
	written := 0
	for _, rec := range records {
		level, ok := logging.ParseLevel(rec.Level)
		if !ok {
			continue
		}
		tag := sanitizeFrontendLogTag(rec.Tag)
		if tag == "" {
			continue
		}
		msg := strings.TrimRight(rec.Message, "\n")
		if msg == "" {
			continue
		}
		// Newlines would break the one-record-per-line contract the viewer's
		// parser depends on, so fold them into a visible marker instead.
		msg = strings.ReplaceAll(msg, "\n", " ⏎ ")
		if len(msg) > frontendLogMaxMsg {
			msg = msg[:frontendLogMaxMsg] + " …(truncated)"
		}
		logging.EmitAt(frontendLogTime(rec.TimestampMS), level, tag, msg)
		written++
	}
	return written
}

// sanitizeFrontendLogTag keeps renderer tags to the same lowercase-kebab shape
// the Go side uses, then namespaces them. Returns "" for anything unusable.
func sanitizeFrontendLogTag(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == '_', r == ' ', r == '.', r == ':':
			b.WriteRune('-')
		}
	}
	tag := strings.Trim(b.String(), "-")
	if tag == "" {
		return ""
	}
	if len(tag) > frontendLogMaxTagLen {
		tag = tag[:frontendLogMaxTagLen]
	}
	return frontendLogTagPrefix + tag
}

// frontendLogTime converts the renderer's epoch milliseconds, falling back to
// now for a missing or nonsensical value so a bad clock cannot produce log
// lines dated 1970.
func frontendLogTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Now()
	}
	t := time.UnixMilli(ms)
	if t.Year() < 2000 {
		return time.Now()
	}
	return t
}
