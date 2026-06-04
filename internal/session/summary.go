package session

import (
	"regexp"
	"strings"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/ringbuf"
)

const (
	summaryTailBytes   = 8 * 1024 // bytes pulled from the scroll buffer
	summaryLineLimit   = 32       // last N lines kept after stripping
	summaryErrorLimit  = 5        // max error lines extracted on failure
	summaryOutputBytes = 4 * 1024 // RecentOutput max length on the wire
	summaryLineCap     = 512      // skip individual lines longer than this
)

// errorWordRE matches "error" / "failed" / "fatal" / "panic" / "traceback" /
// "exception" / "errored" as a whole word, case-insensitive. The (?i) prefix
// is local to this regex; \b uses ASCII word boundaries.
var errorWordRE = regexp.MustCompile(`(?i)\b(errored?|fail(ed)?|fatal|panic|traceback|exception)\b`)

// errorPrefixRE matches lines that start (after leading whitespace) with a
// known severity prefix followed by ':'.
var errorPrefixRE = regexp.MustCompile(`^\s*(?i:error|panic|fatal|warning|warn):\s`)

// computeSummary builds a SessionSummary from the tail of buf as of `now`.
// isFailure controls whether ErrorLines is populated. Returns non-nil even
// when the buffer is empty; the JSON omitempty tags drop empty fields on
// the wire.
func computeSummary(buf *ringbuf.Buffer, now time.Time, isFailure bool) *proto.SessionSummary {
	raw := buf.TailBytes(summaryTailBytes)
	clean := StripANSI(raw)
	lines := splitLastN(clean, summaryLineLimit)
	output := joinTruncated(lines, summaryOutputBytes)

	s := &proto.SessionSummary{
		RecentOutput: output,
		CapturedAt:   now.Unix(),
	}
	if isFailure {
		s.ErrorLines = extractErrorLines(lines, summaryErrorLimit)
	}
	return s
}

// splitLastN splits b on '\n' and returns up to the last n non-empty lines
// (after trimming trailing '\r').
func splitLastN(b []byte, n int) []string {
	if len(b) == 0 || n <= 0 {
		return nil
	}
	all := strings.Split(string(b), "\n")
	out := make([]string, 0, n)
	for i := len(all) - 1; i >= 0; i-- {
		line := strings.TrimRight(all[i], "\r")
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= n {
			break
		}
	}
	// Reverse to chronological order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// joinTruncated joins lines with '\n'. If the joined length exceeds maxBytes,
// drops from the FRONT (oldest) until it fits.
func joinTruncated(lines []string, maxBytes int) string {
	if len(lines) == 0 {
		return ""
	}
	joined := strings.Join(lines, "\n")
	if len(joined) <= maxBytes {
		return joined
	}
	// Drop oldest lines until under the limit.
	i := 0
	for i < len(lines) {
		joined = strings.Join(lines[i+1:], "\n")
		if len(joined) <= maxBytes {
			return joined
		}
		i++
	}
	// Single remaining line still too long — return a hard tail crop.
	last := lines[len(lines)-1]
	if len(last) > maxBytes {
		return last[len(last)-maxBytes:]
	}
	return last
}

// extractErrorLines returns up to `limit` lines from `lines` that match
// either the word regex or the prefix regex. Lines longer than summaryLineCap
// or pure-whitespace are skipped. Order is preserved (input order).
// Duplicates are NOT deduplicated — repeated "error" lines from a stack
// trace are useful context.
func extractErrorLines(lines []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if len(line) > summaryLineCap {
			continue
		}
		if errorPrefixRE.MatchString(line) || errorWordRE.MatchString(line) {
			out = append(out, line)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
