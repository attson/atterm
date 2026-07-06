package feishu

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// A single atterm session can run multiple back-to-back claude conversations
// (user exits claude, then runs it again in the same PTY). Without a reset
// signal, AIRoller would accumulate old turns across conversations and the
// anchor body would blur old + new content together. Each new conversation
// has its own transcript JSONL path; when the path changes on
// UserPromptSubmit, the roller drops old turns and starts fresh.
func TestAIChunker_ResetsOnTranscriptPathChange(t *testing.T) {
	clock := newFakeClock()
	var lastBody string
	ch := NewAIChunkerWithClock(func(body string) { lastBody = body }, clock)

	// First conversation: two turns
	ch.PushTurn(TurnUserPromptEvent{Text: "first prompt", TranscriptPath: "/x/a.jsonl"})
	ch.PushTurn(TurnAssistantFinalEvent{Text: "first reply"})
	clock.advance(110)
	ch.Tick()
	if !strings.Contains(lastBody, "first prompt") || !strings.Contains(lastBody, "first reply") {
		t.Fatalf("first conversation didn't render: %q", lastBody)
	}

	// New conversation begins — transcript path changes. Old turns must drop.
	ch.PushTurn(TurnUserPromptEvent{Text: "second prompt", TranscriptPath: "/x/b.jsonl"})
	clock.advance(110)
	ch.Tick()
	if strings.Contains(lastBody, "first prompt") || strings.Contains(lastBody, "first reply") {
		t.Errorf("expected roller reset on transcript change; still see first-round content: %q", lastBody)
	}
	if !strings.Contains(lastBody, "second prompt") {
		t.Errorf("second-round prompt missing: %q", lastBody)
	}
}

// The reset must NOT fire when transcript path is empty (older payloads /
// events without the field) or unchanged.
func TestAIChunker_KeepsRollerWhenTranscriptPathStable(t *testing.T) {
	clock := newFakeClock()
	var lastBody string
	ch := NewAIChunkerWithClock(func(body string) { lastBody = body }, clock)

	ch.PushTurn(TurnUserPromptEvent{Text: "first", TranscriptPath: "/x/a.jsonl"})
	ch.PushTurn(TurnAssistantFinalEvent{Text: "reply-a"})
	ch.PushTurn(TurnUserPromptEvent{Text: "second", TranscriptPath: "/x/a.jsonl"}) // same path — no reset
	ch.PushTurn(TurnAssistantFinalEvent{Text: "reply-b"})
	ch.PushTurn(TurnUserPromptEvent{Text: "third"}) // empty path — no reset (backward compat with older events)
	clock.advance(110)
	ch.Tick()
	for _, want := range []string{"first", "reply-a", "second", "reply-b", "third"} {
		if !strings.Contains(lastBody, want) {
			t.Errorf("stable-path run should keep %q, got: %q", want, lastBody)
		}
	}
}

func TestAIChunker_ConcurrentPushTickRaceClean(t *testing.T) {
	clock := newFakeClock()
	var calls int32
	ch := NewAIChunkerWithClock(func(body string) { atomic.AddInt32(&calls, 1) }, clock)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			ch.PushTurn(TurnUserPromptEvent{Text: "p" + strconv.Itoa(i)})
		}(i)
		go func() {
			defer wg.Done()
			ch.Tick()
		}()
	}
	wg.Wait()
	// We don't assert call count — just that no race detector trips.
	_ = calls
}

func TestPatchWithRetry_OneBackoffOn5xx(t *testing.T) {
	calls := 0
	patch := func() error {
		calls++
		if calls == 1 {
			return fmt.Errorf("cardkit patch: code=500 msg=server error")
		}
		return nil
	}
	err := PatchWithRetry(patch)
	if err != nil {
		t.Fatalf("expected success on retry, got %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial + 1 retry)", calls)
	}
}

func TestPatchWithRetry_GivesUpAfterRetry(t *testing.T) {
	calls := 0
	patch := func() error {
		calls++
		return fmt.Errorf("cardkit patch: code=500 msg=server error")
	}
	err := PatchWithRetry(patch)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial + 1 retry)", calls)
	}
}

func TestPatchWithRetry_NoRetryOnCardGone(t *testing.T) {
	calls := 0
	patch := func() error {
		calls++
		return fmt.Errorf("cardkit patch: code=230030 msg=card not found")
	}
	_ = PatchWithRetry(patch)
	if calls != 1 {
		t.Errorf("should not retry on card-gone error, calls = %d", calls)
	}
}
