package logging

import (
	"io"
	"strings"
	"time"
)

// StdlibWriter returns an io.Writer suitable for log.SetOutput. Each write is
// treated as one record and wrapped in the standard line format at the given
// level and tag.
//
// This is the safety net for output we do not control: third-party
// dependencies (Wails, net/http) and any call site not yet migrated still land
// in the log file with a parseable shape instead of a bare line.
//
// Callers must also call log.SetFlags(0) so the stdlib does not prepend its
// own timestamp on top of ours.
//
// Records written this way bypass the level threshold — the stdlib has no
// level, and dropping unattributed output would hide exactly the third-party
// errors this exists to capture.
func StdlibWriter(l Level, tag string) io.Writer {
	return &stdlibWriter{level: l, tag: tag}
}

type stdlibWriter struct {
	level Level
	tag   string
}

func (w *stdlibWriter) Write(p []byte) (int, error) {
	write(FormatLine(time.Now(), w.level, w.tag, strings.TrimRight(string(p), "\n")))
	return len(p), nil
}
