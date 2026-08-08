package main

import (
	"strings"
	"testing"
	"time"
)

func TestAppendFrontendLogsWritesPrefixedTag(t *testing.T) {
	buf := captureLogSink(t)
	a := &App{}

	ts := time.Date(2026, 8, 8, 15, 4, 5, 123000000, time.Local)
	n := a.AppendFrontendLogs([]FrontendLogRecord{
		{TimestampMS: ts.UnixMilli(), Level: "WARN", Tag: "term", Message: "resize while detached"},
	})

	if n != 1 {
		t.Fatalf("written = %d, want 1", n)
	}
	want := "2026/08/08 15:04:05.123 WARN  [ui-term] resize while detached"
	if !strings.Contains(buf.String(), want) {
		t.Fatalf("got %q, want it to contain %q", buf.String(), want)
	}
}

// The renderer sends a bare tag; the ui- namespace is applied here so a
// frontend tag can never be confused with a Go one.
func TestAppendFrontendLogsSanitizesTag(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"boot", "ui-boot"},
		{"  Boot  ", "ui-boot"},
		{"file_explorer", "ui-file-explorer"},
		{"paste.image", "ui-paste-image"},
		{"[weird]", "ui-weird"},
		{strings.Repeat("x", 64), "ui-" + strings.Repeat("x", 32)},
		{"", ""},
		{"!!!", ""},
	}
	for _, tc := range cases {
		if got := sanitizeFrontendLogTag(tc.in); got != tc.want {
			t.Errorf("sanitizeFrontendLogTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAppendFrontendLogsSkipsInvalidRecords(t *testing.T) {
	buf := captureLogSink(t)
	a := &App{}

	n := a.AppendFrontendLogs([]FrontendLogRecord{
		{Level: "TRACE", Tag: "term", Message: "unknown level"},
		{Level: "INFO", Tag: "", Message: "no tag"},
		{Level: "INFO", Tag: "term", Message: ""},
		{Level: "INFO", Tag: "term", Message: "kept"},
	})

	if n != 1 {
		t.Fatalf("written = %d, want 1", n)
	}
	out := buf.String()
	for _, unwanted := range []string{"unknown level", "no tag"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("invalid record leaked: %q", out)
		}
	}
	if !strings.Contains(out, "[ui-term] kept") {
		t.Errorf("valid record missing: %q", out)
	}
}

// One record must stay one line, or the viewer's line-oriented parser starts
// rendering fragments as untagged raw text.
func TestAppendFrontendLogsFoldsNewlines(t *testing.T) {
	buf := captureLogSink(t)
	a := &App{}

	a.AppendFrontendLogs([]FrontendLogRecord{
		{Level: "ERROR", Tag: "boot", Message: "step failed\n  at foo\n  at bar"},
	})

	out := buf.String()
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("expected a single line, got %q", out)
	}
	if !strings.Contains(out, "step failed ⏎   at foo ⏎   at bar") {
		t.Fatalf("newlines not folded: %q", out)
	}
}

func TestAppendFrontendLogsTruncatesLongMessages(t *testing.T) {
	buf := captureLogSink(t)
	a := &App{}

	a.AppendFrontendLogs([]FrontendLogRecord{
		{Level: "INFO", Tag: "term", Message: strings.Repeat("x", frontendLogMaxMsg+500)},
	})

	out := buf.String()
	if !strings.Contains(out, "…(truncated)") {
		t.Fatalf("long message not truncated: %d bytes", len(out))
	}
	if len(out) > frontendLogMaxMsg+200 {
		t.Fatalf("truncated line still too long: %d bytes", len(out))
	}
}

func TestAppendFrontendLogsCapsBatch(t *testing.T) {
	buf := captureLogSink(t)
	a := &App{}

	records := make([]FrontendLogRecord, frontendLogMaxBatch+100)
	for i := range records {
		records[i] = FrontendLogRecord{Level: "INFO", Tag: "term", Message: "line"}
	}
	if n := a.AppendFrontendLogs(records); n != frontendLogMaxBatch {
		t.Fatalf("written = %d, want %d", n, frontendLogMaxBatch)
	}
	if got := strings.Count(buf.String(), "\n"); got != frontendLogMaxBatch {
		t.Fatalf("lines written = %d, want %d", got, frontendLogMaxBatch)
	}
}

func TestAppendFrontendLogsRespectsThreshold(t *testing.T) {
	buf := captureLogSink(t)
	setTestLogLevelWarn(t)
	a := &App{}

	a.AppendFrontendLogs([]FrontendLogRecord{
		{Level: "DEBUG", Tag: "term", Message: "noisy"},
		{Level: "ERROR", Tag: "term", Message: "loud"},
	})

	out := buf.String()
	if strings.Contains(out, "noisy") {
		t.Errorf("DEBUG record written below threshold: %q", out)
	}
	if !strings.Contains(out, "loud") {
		t.Errorf("ERROR record missing: %q", out)
	}
}

func TestFrontendLogTimeFallsBackToNow(t *testing.T) {
	for _, ms := range []int64{0, -1, 1000} {
		got := frontendLogTime(ms)
		if got.Year() < 2000 {
			t.Errorf("frontendLogTime(%d) = %v, want a fallback to now", ms, got)
		}
	}
}

func TestAppendFrontendLogsEmptyBatch(t *testing.T) {
	captureLogSink(t)
	a := &App{}
	if n := a.AppendFrontendLogs(nil); n != 0 {
		t.Fatalf("written = %d, want 0", n)
	}
}
