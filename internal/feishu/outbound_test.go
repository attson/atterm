package feishu

import (
	"strings"
	"testing"
	"time"
)

func TestShellRoller_KeepsLast5KBOr30Lines(t *testing.T) {
	r := NewShellRoller()
	for i := 0; i < 100; i++ {
		r.Append([]byte("line " + strings.Repeat("x", 10) + "\n"))
	}
	out := r.Render()
	const openFence = "```\n"
	const closeFence = "\n```"
	if !strings.HasPrefix(out, openFence) || !strings.HasSuffix(out, closeFence) {
		t.Fatalf("expected code-fenced output, got: %q", out)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(out, openFence), closeFence)
	lines := strings.Split(body, "\n")
	if len(lines) != 30 {
		t.Errorf("got %d lines, want exactly 30 (rolling should drop the oldest 70)", len(lines))
	}
	if len(body) > 5*1024 {
		t.Errorf("body got %d bytes, want ≤5KB", len(body))
	}
	if lines[0] != "line "+strings.Repeat("x", 10) {
		t.Errorf("first surviving line content unexpected: %q", lines[0])
	}
	if lines[29] != "line "+strings.Repeat("x", 10) {
		t.Errorf("last surviving line content unexpected: %q", lines[29])
	}
}

func TestShellRoller_MergesPartialLines(t *testing.T) {
	r := NewShellRoller()
	r.Append([]byte("hello "))
	r.Append([]byte("world\n"))
	r.Append([]byte("next line\n"))
	out := r.Render()
	const openFence = "```\n"
	const closeFence = "\n```"
	body := strings.TrimSuffix(strings.TrimPrefix(out, openFence), closeFence)
	lines := strings.Split(body, "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), lines)
	}
	if lines[0] != "hello world" {
		t.Errorf("partial-line merge failed; line 0 = %q, want %q", lines[0], "hello world")
	}
	if lines[1] != "next line" {
		t.Errorf("post-merge new-line tracking failed; line 1 = %q, want %q", lines[1], "next line")
	}
}

func TestShellRoller_TruncatesByteCapEvenWithFewLines(t *testing.T) {
	r := NewShellRoller()
	r.Append([]byte(strings.Repeat("a", 10*1024) + "\n"))
	out := r.Render()
	// Fenced output: "```\n" + body + "\n```" — body itself must be ≤5KB.
	const openFence = "```\n"
	const closeFence = "\n```"
	if !strings.HasPrefix(out, openFence) || !strings.HasSuffix(out, closeFence) {
		t.Fatalf("expected code-fenced output, got: %q", out[:min(40, len(out))])
	}
	body := strings.TrimSuffix(strings.TrimPrefix(out, openFence), closeFence)
	if len(body) > 5*1024 {
		t.Errorf("body got %d bytes, want ≤5KB", len(body))
	}
}

func TestShellRoller_StripsANSI(t *testing.T) {
	r := NewShellRoller()
	r.Append([]byte("\x1b[31mhello\x1b[0m world\n"))
	out := r.Render()
	if !strings.Contains(out, "hello world") {
		t.Errorf("ANSI not stripped, got: %q", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("ESC byte leaked through: %q", out)
	}
}

func TestChunkerThrottle_FlushesEvery100ms(t *testing.T) {
	clock := newFakeClock()
	calls := 0
	ch := NewChunkerWithClock(func(body string) { calls++ }, clock)
	ch.PushBytes([]byte("a\n"))
	clock.advance(50)
	ch.PushBytes([]byte("b\n"))
	if calls != 0 {
		t.Errorf("expected 0 flushes before 100ms, got %d", calls)
	}
	clock.advance(60) // total 110ms
	ch.Tick()
	if calls != 1 {
		t.Errorf("expected 1 flush after 110ms, got %d", calls)
	}
}

func TestChunkerDiffSkip(t *testing.T) {
	clock := newFakeClock()
	calls := 0
	ch := NewChunkerWithClock(func(body string) { calls++ }, clock)
	ch.PushBytes([]byte("same content\n"))
	clock.advance(110)
	ch.Tick()
	ch.PushBytes(nil) // no change
	clock.advance(110)
	ch.Tick()
	if calls != 1 {
		t.Errorf("expected 1 flush (diff-skip), got %d", calls)
	}
}

type fakeClock struct {
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time { return f.now }
func (f *fakeClock) advance(ms int) { f.now = f.now.Add(time.Duration(ms) * time.Millisecond) }

func TestAIRoller_KeepsLast5Turns(t *testing.T) {
	r := NewAIRoller()
	for i := 0; i < 8; i++ {
		r.OnUserPrompt("prompt " + string(rune('A'+i)))
		r.OnAssistantFinal("reply " + string(rune('A'+i)))
	}
	out := r.Render()
	// 5 turn pairs ~ 10 sections. Specifically: prompt D..H should remain;
	// A..C should have rolled off.
	if strings.Contains(out, "prompt A") {
		t.Errorf("prompt A should have rolled off")
	}
	if !strings.Contains(out, "prompt H") {
		t.Errorf("prompt H should be present")
	}
}

func TestAIRoller_NestsToolCalls(t *testing.T) {
	r := NewAIRoller()
	r.OnUserPrompt("fix it")
	r.OnToolStart("Bash")
	r.OnToolEnd("Bash", "exit code 0")
	r.OnAssistantFinal("done")
	out := r.Render()
	if !strings.Contains(out, "Bash") {
		t.Errorf("missing tool name in output: %q", out)
	}
	if !strings.Contains(out, "exit code 0") {
		t.Errorf("missing tool body: %q", out)
	}
}

func TestAIChunker_FlushesUserPrompt(t *testing.T) {
	clock := newFakeClock()
	calls := 0
	var lastBody string
	ch := NewAIChunkerWithClock(func(body string) { calls++; lastBody = body }, clock)
	ch.PushTurn(TurnUserPromptEvent{Text: "hi"})
	// Pushing alone doesn't satisfy the 100ms window since lastFlush was
	// initialized to clk.Now() — Tick after advancing should flush.
	clock.advance(110)
	ch.Tick()
	if calls != 1 {
		t.Errorf("expected 1 flush, got %d", calls)
	}
	if !strings.Contains(lastBody, "hi") {
		t.Errorf("body missing 'hi': %q", lastBody)
	}
}
