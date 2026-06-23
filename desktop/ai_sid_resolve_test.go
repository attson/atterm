package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeAITitle(t *testing.T) {
	cases := map[string]string{
		"修复AI会话恢复问题":               "修复AI会话恢复问题",
		"⠂ Claude Code":            "", // spinner + generic placeholder
		"✻ 改造画布组件":                 "改造画布组件",
		"Claude Code":              "", // generic placeholder
		"claude code":              "", // case-insensitive placeholder
		"":                         "",
		"\x1b[1mBold Title\x1b[0m": "Bold Title",
		"  spaced   out  ":         "spaced out",
		"⠿⠿ Investigate issue":     "Investigate issue",
	}
	for in, want := range cases {
		if got := normalizeAITitle(in); got != want {
			t.Errorf("normalizeAITitle(%q) = %q want %q", in, got, want)
		}
	}
}

func writeJsonl(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanJsonlForAITitle(t *testing.T) {
	dir := t.TempDir()

	// last ai-title wins, noise ignored.
	p := filepath.Join(dir, "a.jsonl")
	writeJsonl(t, p,
		`{"type":"user","message":{"content":"hi"}}`,
		`{"type":"ai-title","aiTitle":"First","sessionId":"sid-1"}`,
		`{"type":"assistant"}`,
		`{"type":"ai-title","aiTitle":"Second","sessionId":"sid-1"}`,
	)
	sid, title, ok := scanJsonlForAITitle(p)
	if !ok || sid != "sid-1" || title != "Second" {
		t.Fatalf("got (%q,%q,%v) want (sid-1,Second,true)", sid, title, ok)
	}

	// no ai-title record.
	p2 := filepath.Join(dir, "b.jsonl")
	writeJsonl(t, p2, `{"type":"user"}`, `{"type":"assistant"}`)
	if _, _, ok := scanJsonlForAITitle(p2); ok {
		t.Fatal("expected ok=false when no ai-title record")
	}

	// megabyte-long line before the ai-title must not break the scanner.
	p3 := filepath.Join(dir, "c.jsonl")
	huge := `{"type":"assistant","text":"` + strings.Repeat("x", 2<<20) + `"}`
	writeJsonl(t, p3, huge, `{"type":"ai-title","aiTitle":"Big","sessionId":"sid-big"}`)
	sid, title, ok = scanJsonlForAITitle(p3)
	if !ok || sid != "sid-big" || title != "Big" {
		t.Fatalf("huge line: got (%q,%q,%v) want (sid-big,Big,true)", sid, title, ok)
	}
}

func TestResolveClaudeSessionID(t *testing.T) {
	dir := t.TempDir()
	alpha := filepath.Join(dir, "11111111-1111-1111-1111-111111111111.jsonl")
	beta := filepath.Join(dir, "22222222-2222-2222-2222-222222222222.jsonl")
	writeJsonl(t, alpha, `{"type":"ai-title","aiTitle":"Alpha task","sessionId":"sid-alpha"}`)
	// beta omits sessionId → must fall back to the filename stem.
	writeJsonl(t, beta, `{"type":"ai-title","aiTitle":"Beta task"}`)

	if sid, ok := resolveClaudeSessionID(dir, "Alpha task", nil); !ok || sid != "sid-alpha" {
		t.Fatalf("alpha: got (%q,%v) want (sid-alpha,true)", sid, ok)
	}
	// spinner-prefixed pane title still matches; filename fallback id.
	if sid, ok := resolveClaudeSessionID(dir, "✻ Beta task", nil); !ok || sid != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("beta: got (%q,%v) want (filename uuid,true)", sid, ok)
	}
	if _, ok := resolveClaudeSessionID(dir, "Claude Code", nil); ok {
		t.Fatal("placeholder title must not match")
	}
	if _, ok := resolveClaudeSessionID(dir, "Nonexistent", nil); ok {
		t.Fatal("unknown title must not match")
	}
}

func TestResolveClaudeSessionID_AmbiguousNewestWins(t *testing.T) {
	dir := t.TempDir()
	older := filepath.Join(dir, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.jsonl")
	newer := filepath.Join(dir, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb.jsonl")
	writeJsonl(t, older, `{"type":"ai-title","aiTitle":"Same","sessionId":"sid-old"}`)
	writeJsonl(t, newer, `{"type":"ai-title","aiTitle":"Same","sessionId":"sid-new"}`)

	base := time.Now()
	if err := os.Chtimes(older, base, base.Add(-1*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, base, base); err != nil {
		t.Fatal(err)
	}
	if sid, ok := resolveClaudeSessionID(dir, "Same", nil); !ok || sid != "sid-new" {
		t.Fatalf("ambiguous: got (%q,%v) want newest sid-new", sid, ok)
	}

	// equal mtimes → ambiguous, no result.
	if err := os.Chtimes(older, base, base); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolveClaudeSessionID(dir, "Same", nil); ok {
		t.Fatal("equal-mtime collision must be ambiguous (no result)")
	}
}
