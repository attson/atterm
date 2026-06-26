// outbound.go implements the per-session outbound chunker that turns PTY
// (shell) or hook (AI) events into rate-limited PATCH calls against an
// anchor card. Two concerns kept separate:
//
//	ShellRoller — bytes in, bounded markdown out (last ≤5KB AND ≤30 lines).
//	Chunker     — wraps a roller with a 100ms-window throttle, diff-skip,
//	              and a flush callback that does the actual PATCH.
//
// The chunker is clock-injectable for tests; production code uses
// NewChunker(flush) which wires the real clock.
package feishu

import (
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/session"
)

const (
	rollerMaxBytes     = 5 * 1024
	rollerMaxLines     = 30
	chunkerFlushPeriod = 100 * time.Millisecond
	chunkerBufferBytes = 4 * 1024
)

// ShellRoller accumulates ANSI-stripped output and renders the last
// ≤5KB AND ≤30 lines on demand. Not goroutine-safe; the chunker owns one.
type ShellRoller struct {
	lines    []string
	lineOpen bool // true when the last element of lines is an unterminated (no \n) line
}

func NewShellRoller() *ShellRoller {
	return &ShellRoller{}
}

// Append takes raw PTY bytes (possibly with ANSI), strips, and merges into
// the rolling per-line buffer. A line ends at `\n`; partial lines (no
// terminator) are kept "open" so the next Append's leading bytes merge in.
func (r *ShellRoller) Append(data []byte) {
	clean := session.StripANSI(data)
	if len(clean) == 0 {
		return
	}
	chunk := string(clean)

	// If the last line is open (unterminated), absorb up to the first \n.
	if r.lineOpen && len(r.lines) > 0 {
		nl := strings.IndexByte(chunk, '\n')
		if nl < 0 {
			r.lines[len(r.lines)-1] += chunk
			return
		}
		r.lines[len(r.lines)-1] += chunk[:nl]
		r.lineOpen = false
		chunk = chunk[nl+1:]
	}
	if chunk == "" {
		return
	}

	// Split remaining chunk into lines. A trailing "" element from a
	// terminating \n marks the last line as closed; otherwise the final
	// element is the new "open" line.
	parts := strings.Split(chunk, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
		r.lineOpen = false
	} else {
		r.lineOpen = true
	}
	r.lines = append(r.lines, parts...)

	if len(r.lines) > rollerMaxLines {
		r.lines = r.lines[len(r.lines)-rollerMaxLines:]
	}
}

// Render returns the current rolling window as a markdown code-fenced string
// capped at rollerMaxBytes, so shell output renders as monospace in Feishu.
func (r *ShellRoller) Render() string {
	if len(r.lines) == 0 {
		return ""
	}
	body := strings.Join(r.lines, "\n")
	if len(body) > rollerMaxBytes {
		body = body[len(body)-rollerMaxBytes:]
		if i := strings.IndexByte(body, '\n'); i >= 0 {
			body = body[i+1:]
		}
	}
	return "```\n" + body + "\n```"
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
		out = out[len(out)-rollerMaxBytes:]
	}
	return out
}

// AIChunker is the AI-side analogue of Chunker; it owns an AIRoller and
// applies the same 100ms throttle + diff-skip rules. Goroutine-safe: a
// sync.Mutex protects all mutable fields, and the flush callback is invoked
// without holding the lock to avoid serialising HTTP PATCH calls.
type AIChunker struct {
	mu        sync.Mutex
	roller    *AIRoller
	flush     FlushFunc
	clock     Clock
	lastFlush time.Time
	dirty     bool
	lastBody  string
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
type TurnUserPromptEvent struct{ Text string }
type TurnToolStartEvent struct{ ToolName string }
type TurnToolEndEvent struct{ ToolName, ToolBody string }
type TurnAssistantFinalEvent struct{ Text string }
