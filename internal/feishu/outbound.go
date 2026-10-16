// outbound.go implements the per-session outbound chunker that turns PTY
// (shell) or hook (AI) events into rate-limited PATCH calls against an
// anchor card. Two concerns kept separate:
//
//	ShellRoller — bytes in, bounded markdown out (last ≤3KB AND ≤12 lines).
//	Chunker     — wraps a roller with a 100ms-window throttle, diff-skip,
//	              and a flush callback that does the actual PATCH.
//
// The chunker is clock-injectable for tests; production code uses
// NewChunker(flush) which wires the real clock.
package feishu

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/attson/atterm/internal/session"
)

const (
	rollerMaxBytes        = 3 * 1024
	rollerMaxLines        = 12
	rollerHistoryMaxBytes = 50 * 1024
	rollerHistoryMaxLines = 300
	chunkerFlushPeriod    = 100 * time.Millisecond
	chunkerBufferBytes    = 4 * 1024
)

// ShellRoller accumulates bounded ANSI-stripped history and renders one
// ≤5KB, ≤30-line page on demand. Not goroutine-safe; the chunker owns one.
type ShellRoller struct {
	lines       []string
	lineOpen    bool // true when the last element of lines is an unterminated (no \n) line
	pendingCR   bool // CR may be the first half of CRLF or a cursor return
	stripper    session.ANSIStreamStripper
	utf8Pending []byte
	pageEnd     int // exclusive absolute line index; -1 follows the latest page
}

func NewShellRoller() *ShellRoller {
	return &ShellRoller{pageEnd: -1}
}

// Append takes raw PTY bytes (possibly with ANSI), strips, and merges into
// the rolling per-line buffer. A line ends at `\n`; partial lines (no
// terminator) are kept "open" so the next Append's leading bytes merge in.
func (r *ShellRoller) Append(data []byte) {
	clean := r.stripper.Strip(data)
	if len(clean) == 0 {
		return
	}
	chunk := r.decodeTerminalText(clean)
	if chunk == "" {
		return
	}

	// PTYs use both CRLF and bare CR. A bare CR means "cursor to column 0",
	// not a printable character; feeding it to Feishu markdown renders as a
	// wide blank indent. Project the stream into lines before rendering:
	// CRLF closes the line, while CR followed by text replaces the current
	// open line (the useful approximation for prompts/progress redraws).
	for chunk != "" {
		i := strings.IndexAny(chunk, "\r\n")
		if i < 0 {
			r.appendVisible(chunk)
			break
		}
		r.appendVisible(chunk[:i])
		switch chunk[i] {
		case '\r':
			r.pendingCR = true
		case '\n':
			r.finishLine()
		}
		chunk = chunk[i+1:]
	}

	r.trimHistory()
}

func (r *ShellRoller) trimHistory() {
	removed := 0
	if len(r.lines) > rollerHistoryMaxLines {
		removed = len(r.lines) - rollerHistoryMaxLines
		r.lines = r.lines[removed:]
	}
	for historyBytes(r.lines) > rollerHistoryMaxBytes && len(r.lines) > 1 {
		r.lines = r.lines[1:]
		removed++
	}
	if len(r.lines) == 1 && len(r.lines[0]) > rollerHistoryMaxBytes {
		r.lines[0] = trimUTF8Front(r.lines[0], rollerHistoryMaxBytes)
	}
	if removed == 0 || r.pageEnd < 0 {
		return
	}
	r.pageEnd -= removed
	if r.pageEnd < 1 {
		r.pageEnd = min(rollerMaxLines, len(r.lines))
	}
}

func historyBytes(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	total := len(lines) - 1
	for _, line := range lines {
		total += len(line)
	}
	return total
}

func (r *ShellRoller) appendVisible(text string) {
	if text == "" {
		return
	}
	if r.pendingCR {
		if r.lineOpen && len(r.lines) != 0 {
			r.lines[len(r.lines)-1] = ""
		}
		r.pendingCR = false
	}
	if !r.lineOpen {
		r.lines = append(r.lines, "")
		r.lineOpen = true
	}
	r.lines[len(r.lines)-1] += text
}

func (r *ShellRoller) finishLine() {
	// A newline after CR is the normal PTY CRLF pair. With no open content,
	// retain one empty line just as strings.Split did previously.
	r.pendingCR = false
	if !r.lineOpen {
		r.lines = append(r.lines, "")
	}
	r.lineOpen = false
}

// decodeTerminalText joins UTF-8 code points split across PTY frames and
// drops invalid bytes plus non-layout C0 controls. Passing an incomplete or
// orphan control byte through encoding/json makes Feishu render U+FFFD.
func (r *ShellRoller) decodeTerminalText(clean []byte) string {
	data := clean
	if len(r.utf8Pending) != 0 {
		data = make([]byte, 0, len(r.utf8Pending)+len(clean))
		data = append(data, r.utf8Pending...)
		data = append(data, clean...)
		r.utf8Pending = nil
	}

	out := make([]byte, 0, len(data))
	for len(data) != 0 {
		if data[0] < utf8.RuneSelf {
			c := data[0]
			if c == '\n' || c == '\r' || c == '\t' || (c >= 0x20 && c != 0x7f) {
				out = append(out, c)
			}
			data = data[1:]
			continue
		}
		if !utf8.FullRune(data) {
			r.utf8Pending = append(r.utf8Pending[:0], data...)
			break
		}
		rn, size := utf8.DecodeRune(data)
		if rn != utf8.RuneError || size != 1 {
			out = append(out, data[:size]...)
		}
		data = data[size:]
	}
	return string(out)
}

// NavigatePage moves between fixed 30-line history pages. The latest page is
// live; older pages keep an absolute end cursor so fresh PTY output does not
// replace the lines the user is reading.
func (r *ShellRoller) NavigatePage(direction string) bool {
	if len(r.lines) == 0 {
		return false
	}
	switch direction {
	case "previous":
		end := r.pageEnd
		if end < 0 {
			end = len(r.lines)
		}
		if end <= rollerMaxLines {
			return false
		}
		r.pageEnd = end - rollerMaxLines
		return true
	case "next":
		if r.pageEnd < 0 {
			return false
		}
		next := r.pageEnd + rollerMaxLines
		if next > len(r.lines)-rollerMaxLines {
			r.pageEnd = -1
		} else {
			r.pageEnd = next
		}
		return true
	default:
		return false
	}
}

// Render returns the selected history page as escaped markdown capped at
// rollerMaxBytes. Feishu mobile automatically collapses long fenced code
// blocks, so terminal output is deliberately rendered as a short message
// preview instead of a code block.
func (r *ShellRoller) Render() string {
	if len(r.lines) == 0 {
		return ""
	}
	end := len(r.lines)
	if r.pageEnd >= 0 && r.pageEnd < end {
		end = r.pageEnd
	}
	start := max(0, end-rollerMaxLines)
	body := strings.Join(r.lines[start:end], "\n")
	if len(body) > rollerMaxBytes {
		body = trimUTF8Front(body, rollerMaxBytes)
		if i := strings.IndexByte(body, '\n'); i >= 0 {
			body = body[i+1:]
		}
	}
	pageCount := (len(r.lines) + rollerMaxLines - 1) / rollerMaxLines
	pageLabel := ""
	if pageCount > 1 {
		page := pageCount
		if r.pageEnd >= 0 {
			page = (r.pageEnd + rollerMaxLines - 1) / rollerMaxLines
		}
		pageLabel = fmt.Sprintf("第 %d/%d 页\n\n", page, pageCount)
	}
	return pageLabel + renderTerminalPreview(body)
}

func renderTerminalPreview(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = escapeTerminalMarkdown(preserveTerminalSpacing(line))
	}
	preview := strings.Join(lines, "\n")
	if len(preview) <= rollerMaxBytes {
		return preview
	}
	return trimUTF8Front(preview, rollerMaxBytes)
}

func preserveTerminalSpacing(line string) string {
	line = strings.ReplaceAll(line, "\t", "\u00a0\u00a0\u00a0\u00a0")
	for strings.Contains(line, "  ") {
		line = strings.ReplaceAll(line, "  ", "\u00a0\u00a0")
	}
	return line
}

func escapeTerminalMarkdown(line string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"~", "\\~",
		"[", "\\[",
		"]", "\\]",
		"<", "\\<",
		">", "\\>",
	).Replace(line)
}

// FlushFunc is the callback the chunker invokes when it has new body
// content to PATCH. Implementations should be non-blocking (≤ ms);
// network I/O must be done asynchronously by the caller.
type FlushFunc func(body string)

// Clock is a minimal abstraction so tests can drive time.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Chunker buffers Append calls and flushes when either of two triggers
// fires: the buffer reaches chunkerBufferBytes (≥4KB, via PushBytes), or
// the 100ms throttle window has elapsed (via Tick). Not goroutine-safe;
// the caller (FeishuSubscriber) must serialize PushBytes/Tick calls.
type Chunker struct {
	roller    *ShellRoller
	flush     FlushFunc
	clock     Clock
	lastFlush time.Time
	bufBytes  int
	dirty     bool
	lastBody  string
}

func NewChunker(flush FlushFunc) *Chunker {
	return NewChunkerWithClock(flush, realClock{})
}

func NewChunkerWithClock(flush FlushFunc, clk Clock) *Chunker {
	return &Chunker{
		roller:    NewShellRoller(),
		flush:     flush,
		clock:     clk,
		lastFlush: clk.Now(),
	}
}

// PushBytes feeds shell PTY bytes. Returns immediately; flushes happen on
// Tick or when the buffer reaches chunkerBufferBytes.
func (c *Chunker) PushBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	c.roller.Append(data)
	c.bufBytes += len(data)
	c.dirty = true
	if c.bufBytes >= chunkerBufferBytes {
		c.flushNow(c.clock.Now())
	}
}

// Tick drains the buffer if the flush window has elapsed. Called by the
// FeishuSubscriber on a ticker; lets the chunker flush even when no new
// bytes arrive.
func (c *Chunker) Tick() {
	now := c.clock.Now()
	if !c.dirty {
		return
	}
	if now.Sub(c.lastFlush) < chunkerFlushPeriod {
		return
	}
	c.flushNow(now)
}

// NavigatePage changes the shell history page and immediately flushes the
// resulting snapshot. The FeishuSubscriber serializes this with PushBytes.
func (c *Chunker) NavigatePage(direction string) bool {
	if !c.roller.NavigatePage(direction) {
		return false
	}
	c.dirty = true
	c.flushNow(c.clock.Now())
	return true
}

func (c *Chunker) flushNow(now time.Time) {
	body := c.roller.Render()
	if body == c.lastBody {
		c.dirty = false
		c.bufBytes = 0
		c.lastFlush = now
		return
	}
	c.lastBody = body
	c.dirty = false
	c.bufBytes = 0
	c.lastFlush = now
	c.flush(body)
}

const aiRollerMaxTurns = 5

// AIRoller assembles a markdown body from per-turn hook events. Each
// "turn" is one assistant response, optionally with nested tool calls.
// The roller keeps the last aiRollerMaxTurns turns; older turns roll off.
// Not goroutine-safe; the AIChunker owns one.
type AIRoller struct {
	turns []*aiTurn
}

type aiTurn struct {
	userPrompt   string
	tools        []aiTool
	assistantMsg string
	completed    bool
}

type aiTool struct {
	name string
	body string
}

func NewAIRoller() *AIRoller {
	return &AIRoller{}
}

// currentTurn returns the in-progress turn, allocating one if needed.
func (r *AIRoller) currentTurn() *aiTurn {
	if len(r.turns) == 0 || r.turns[len(r.turns)-1].completed {
		r.turns = append(r.turns, &aiTurn{})
		if len(r.turns) > aiRollerMaxTurns {
			r.turns = r.turns[len(r.turns)-aiRollerMaxTurns:]
		}
	}
	return r.turns[len(r.turns)-1]
}

func (r *AIRoller) OnUserPrompt(text string) {
	t := r.currentTurn()
	t.userPrompt = text
}

func (r *AIRoller) OnToolStart(name string) {
	t := r.currentTurn()
	t.tools = append(t.tools, aiTool{name: name})
}

func (r *AIRoller) OnToolEnd(name, body string) {
	t := r.currentTurn()
	// Match by last open tool with this name; tolerate out-of-order arrivals.
	for i := len(t.tools) - 1; i >= 0; i-- {
		if t.tools[i].name == name && t.tools[i].body == "" {
			t.tools[i].body = body
			return
		}
	}
	// No matching open tool — append as-is.
	t.tools = append(t.tools, aiTool{name: name, body: body})
}

func (r *AIRoller) OnAssistantFinal(text string) {
	t := r.currentTurn()
	t.assistantMsg = text
	t.completed = true
}

func (r *AIRoller) Render() string {
	if len(r.turns) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, t := range r.turns {
		if t.userPrompt != "" {
			sb.WriteString("👤 ")
			sb.WriteString(t.userPrompt)
			sb.WriteString("\n\n")
		}
		if t.assistantMsg != "" || t.completed {
			sb.WriteString("🤖 ")
			sb.WriteString(t.assistantMsg)
			sb.WriteString("\n")
		}
		for _, tool := range t.tools {
			sb.WriteString("  ▸ ")
			sb.WriteString(tool.name)
			sb.WriteString("\n")
			if tool.body != "" {
				sb.WriteString("    ```\n    ")
				// Indent each line in the tool body by 4 spaces.
				sb.WriteString(strings.ReplaceAll(tool.body, "\n", "\n    "))
				sb.WriteString("\n    ```\n")
			}
		}
		sb.WriteString("\n")
	}
	out := sb.String()
	// Cap at rollerMaxBytes the same way ShellRoller does, from the front.
	if len(out) > rollerMaxBytes {
		out = trimUTF8Front(out, rollerMaxBytes)
	}
	return out
}

func trimUTF8Front(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// AIChunker is the AI-side analogue of Chunker; it owns an AIRoller and
// applies the same 100ms throttle + diff-skip rules. Goroutine-safe: a
// sync.Mutex protects all mutable fields, and the flush callback is invoked
// without holding the lock to avoid serialising HTTP PATCH calls.
type AIChunker struct {
	mu                 sync.Mutex
	roller             *AIRoller
	flush              FlushFunc
	clock              Clock
	lastFlush          time.Time
	dirty              bool
	lastBody           string
	lastTranscriptPath string // most recent non-empty transcript_path seen; change → reset roller
}

func NewAIChunker(flush FlushFunc) *AIChunker {
	return NewAIChunkerWithClock(flush, realClock{})
}

func NewAIChunkerWithClock(flush FlushFunc, clk Clock) *AIChunker {
	return &AIChunker{
		roller:    NewAIRoller(),
		flush:     flush,
		clock:     clk,
		lastFlush: clk.Now(), // same init-fix as Chunker (Task 6)
	}
}

func (c *AIChunker) PushTurn(ev any) {
	c.mu.Lock()
	// Conversation boundary: a UserPromptSubmit with a transcript_path
	// different from the last one signals a new claude conversation on the
	// same atterm session (e.g. user exited claude and ran it again). Reset
	// the roller so old turns roll off cleanly instead of piling up.
	if e, ok := ev.(TurnUserPromptEvent); ok && e.TranscriptPath != "" && e.TranscriptPath != c.lastTranscriptPath {
		c.roller = NewAIRoller()
		c.lastTranscriptPath = e.TranscriptPath
	}
	switch e := ev.(type) {
	case TurnUserPromptEvent:
		c.roller.OnUserPrompt(e.Text)
	case TurnToolStartEvent:
		c.roller.OnToolStart(e.ToolName)
	case TurnToolEndEvent:
		c.roller.OnToolEnd(e.ToolName, e.ToolBody)
	case TurnAssistantFinalEvent:
		c.roller.OnAssistantFinal(e.Text)
	default:
		c.mu.Unlock()
		return
	}
	c.dirty = true
	body, shouldFlush := c.computeFlushLocked()
	c.mu.Unlock()
	if shouldFlush {
		c.flush(body)
	}
}

func (c *AIChunker) Tick() {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return
	}
	body, shouldFlush := c.computeFlushLocked()
	c.mu.Unlock()
	if shouldFlush {
		c.flush(body)
	}
}

// computeFlushLocked decides whether to flush and, if so, returns the body to
// send. Caller must hold c.mu. Updates lastFlush, lastBody, and dirty
// regardless of whether the flush callback will actually be invoked.
func (c *AIChunker) computeFlushLocked() (body string, shouldFlush bool) {
	now := c.clock.Now()
	if now.Sub(c.lastFlush) < chunkerFlushPeriod {
		return "", false
	}
	rendered := c.roller.Render()
	c.lastFlush = now
	c.dirty = false
	if rendered == c.lastBody {
		return "", false // diff-skip: no change since last flush
	}
	c.lastBody = rendered
	return rendered, true
}

// Per-turn dispatch types so PushTurn doesn't depend on the desktop/feishu
// TurnEvent (this package is internal to relay, not desktop).
// TurnUserPromptEvent is the roller's view of a user-submitted prompt. The
// optional TranscriptPath field carries claude's per-conversation JSONL
// path — a change signals a new conversation, which the AIChunker uses
// to reset the roller so old turns don't blur into the fresh dialog.
type TurnUserPromptEvent struct {
	Text           string
	TranscriptPath string
}
type TurnToolStartEvent struct{ ToolName string }
type TurnToolEndEvent struct{ ToolName, ToolBody string }
type TurnAssistantFinalEvent struct{ Text string }

// PatchWithRetry runs patch once, retries once after a 1s backoff on
// transient errors (5xx). card-gone errors and auth-class errors return
// immediately so the caller can take terminal action (drop from index /
// refresh token). The function returns the last error encountered.
func PatchWithRetry(patch func() error) error {
	err := patch()
	if err == nil {
		return nil
	}
	if IsCardGoneError(err) {
		return err
	}
	var ace *AuthClassError
	if errors.As(err, &ace) {
		return err
	}
	time.Sleep(time.Second)
	return patch()
}
