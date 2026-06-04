package session

import (
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/ringbuf"
)

func feed(buf *ringbuf.Buffer, text string) {
	buf.Push(ringbuf.Chunk{Seq: buf.LatestSeq() + 1, Data: []byte(text)})
}

func TestComputeSummary_FailureExtractsErrorLines(t *testing.T) {
	buf := ringbuf.New(64 * 1024)
	feed(buf, "go: building...\n")
	feed(buf, "FAIL\tpackage [build failed]\n")
	feed(buf, "error: something specific\n")
	feed(buf, "ok\n")
	got := computeSummary(buf, time.Unix(123, 0), true)
	if got == nil {
		t.Fatal("nil summary")
	}
	if got.CapturedAt != 123 {
		t.Errorf("CapturedAt = %d, want 123", got.CapturedAt)
	}
	if !strings.Contains(got.RecentOutput, "FAIL") || !strings.Contains(got.RecentOutput, "error: something specific") {
		t.Errorf("RecentOutput missing data: %q", got.RecentOutput)
	}
	if len(got.ErrorLines) < 2 {
		t.Fatalf("ErrorLines too short: %v", got.ErrorLines)
	}
	joined := strings.Join(got.ErrorLines, "\n")
	if !strings.Contains(joined, "FAIL") || !strings.Contains(joined, "error: something specific") {
		t.Errorf("ErrorLines missing entries: %v", got.ErrorLines)
	}
}

func TestComputeSummary_SuccessNoErrorLines(t *testing.T) {
	buf := ringbuf.New(64 * 1024)
	feed(buf, "FAIL would otherwise extract\n")
	feed(buf, "error: would otherwise extract\n")
	feed(buf, "ok\n")
	got := computeSummary(buf, time.Unix(1, 0), false)
	if got == nil {
		t.Fatal("nil summary")
	}
	if len(got.ErrorLines) != 0 {
		t.Fatalf("expected no error lines on success, got %v", got.ErrorLines)
	}
}

func TestComputeSummary_StripsAnsiInRecentOutput(t *testing.T) {
	buf := ringbuf.New(64 * 1024)
	feed(buf, "\x1b[31mboom\x1b[0m\n")
	got := computeSummary(buf, time.Unix(1, 0), false)
	if got == nil {
		t.Fatal("nil summary")
	}
	if !strings.Contains(got.RecentOutput, "boom") {
		t.Errorf("output missing payload: %q", got.RecentOutput)
	}
	if strings.Contains(got.RecentOutput, "\x1b") {
		t.Errorf("output still contains ESC: %q", got.RecentOutput)
	}
}

func TestComputeSummary_RespectsByteLimit(t *testing.T) {
	buf := ringbuf.New(64 * 1024)
	for i := 0; i < 600; i++ {
		feed(buf, "0123456789\n") // 11 bytes each → 6 600 bytes total
	}
	got := computeSummary(buf, time.Unix(1, 0), false)
	if got == nil {
		t.Fatal("nil summary")
	}
	if n := len(got.RecentOutput); n > summaryOutputBytes {
		t.Fatalf("RecentOutput len = %d, want <= %d", n, summaryOutputBytes)
	}
	if !strings.HasSuffix(got.RecentOutput, "0123456789") {
		t.Errorf("expected the newest line to be kept, got tail %q", got.RecentOutput[len(got.RecentOutput)-20:])
	}
}

func TestExtractErrorLines_OrderAndLimit(t *testing.T) {
	lines := []string{
		"info: starting",
		"error: a",
		"info: still running",
		"FAIL: b",
		"panic: c",
		"error: d",
		"fatal: e",
		"error: f",
	}
	got := extractErrorLines(lines, 5)
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
	wantFirst := []string{"error: a", "FAIL: b", "panic: c", "error: d", "fatal: e"}
	for i, w := range wantFirst {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestExtractErrorLines_SkipsLongLinesAndBlank(t *testing.T) {
	long := strings.Repeat("x", 600) + " error here"
	got := extractErrorLines([]string{"", "   ", long, "error: kept"}, 5)
	if len(got) != 1 || got[0] != "error: kept" {
		t.Fatalf("got %v", got)
	}
}
