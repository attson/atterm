package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
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

func appendJsonl(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
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

func TestResolveFreshClaudeSessionID(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "11111111-1111-1111-1111-111111111111.jsonl")
	fresh := filepath.Join(dir, "22222222-2222-2222-2222-222222222222.jsonl")
	writeJsonl(t, old, `{"type":"user"}`)
	writeJsonl(t, fresh, `{"type":"user"}`)

	now := time.Now()
	if err := os.Chtimes(old, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	since := now.Add(-3 * time.Second)

	if sid, ok := resolveFreshClaudeSessionID(dir, since); !ok || sid != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("single fresh: got (%q,%v) want fresh uuid", sid, ok)
	}

	// A second active file → ambiguous (left to title matching).
	fresh2 := filepath.Join(dir, "33333333-3333-3333-3333-333333333333.jsonl")
	writeJsonl(t, fresh2, `{"type":"user"}`)
	if _, ok := resolveFreshClaudeSessionID(dir, since); ok {
		t.Fatal("two active files must be ambiguous (no result)")
	}

	// All old → no result.
	if err := os.Chtimes(fresh, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fresh2, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolveFreshClaudeSessionID(dir, since); ok {
		t.Fatal("no active file must yield no result")
	}
}

func TestScanCodexJsonlForUserTitle(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rollout-2026-07-29T13-18-54-019fac4f-bd07-7630-8ea5-30ed7515a488.jsonl")
	writeJsonl(t, p,
		`{"type":"session_meta","payload":{"id":"019fac4f-bd07-7630-8ea5-30ed7515a488"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"修改site展示页面，同步最新内容"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"noise"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"  更新截图资源\n\n"}}`,
	)

	title, ok := scanCodexJsonlForUserTitle(p)
	if !ok || title != "修改site展示页面，同步最新内容" {
		t.Fatalf("got (%q,%v), want first user message", title, ok)
	}
}

func TestScanCodexJsonlForUserTitle_IgnoresEmptyAndLargeLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rollout-2026-07-29T13-18-54-019fac4f-bd07-7630-8ea5-30ed7515a488.jsonl")
	huge := `{"type":"response_item","payload":{"type":"function_call_output","output":"` + strings.Repeat("x", 2<<20) + `"}}`
	writeJsonl(t, p,
		huge,
		`{"type":"event_msg","payload":{"type":"user_message","message":"first"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"   "}}`,
	)

	title, ok := scanCodexJsonlForUserTitle(p)
	if !ok || title != "first" {
		t.Fatalf("got (%q,%v), want first non-empty user message", title, ok)
	}
}

func TestStartCodexFileResolve_CapturesSidAndMirrorsUserTitle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "work", "repo")
	dir := codexWatchDir(cwd, time.Now(), home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	sess := session.New(uuid.New(), proto.SessionInfo{Cwd: cwd})
	defer sess.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gotSid := make(chan string, 1)
	go startCodexFileResolve(ctx, sess, cwd, func(sid string) {
		gotSid <- sid
	})

	time.Sleep(codexResolveInterval * 2)
	sid := "019fac4f-bd07-7630-8ea5-30ed7515a488"
	writeJsonl(t, filepath.Join(dir, "rollout-2026-07-29T13-18-54-"+sid+".jsonl"),
		`{"type":"session_meta","payload":{"id":"`+sid+`","thread_source":"user"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"修改site展示页面，同步最新内容"}}`,
	)

	select {
	case got := <-gotSid:
		if got != sid {
			t.Fatalf("captured sid = %q, want %q", got, sid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for codex sid capture")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := sess.Info().Title; got == "修改site展示页面，同步最新内容" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session title = %q, want codex user message", sess.Info().Title)
}

func TestStartCodexFileResolve_CapturesResumedExistingRollout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "work", "repo")
	dir := codexWatchDir(cwd, time.Now(), home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	sid := "019fae77-52c1-7201-bbdc-078634559f19"
	path := filepath.Join(dir, "rollout-2026-07-29T23-21-23-"+sid+".jsonl")
	writeJsonl(t, path,
		`{"type":"session_meta","payload":{"id":"`+sid+`","cwd":"`+cwd+`","thread_source":"user"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"web 端这里是不是应该换一种提示"}}`,
	)
	old := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	sess := session.New(uuid.New(), proto.SessionInfo{Cwd: cwd})
	defer sess.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gotSid := make(chan string, 1)
	go startCodexFileResolve(ctx, sess, cwd, func(captured string) {
		gotSid <- captured
	})

	time.Sleep(codexResolveInterval * 2)
	appendJsonl(t, path, `{"type":"event_msg","payload":{"type":"agent_message","message":"resume selected"}}`)

	select {
	case got := <-gotSid:
		if got != sid {
			t.Fatalf("captured sid = %q, want %q", got, sid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for resumed codex sid capture")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := sess.Info().Title; got == "web 端这里是不是应该换一种提示" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session title = %q, want first codex user message", sess.Info().Title)
}

func TestStartCodexKnownTitleResolve_FindsPreviousDaySidAndMirrorsUserTitle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "work", "repo")
	dir := filepath.Join(home, ".codex", "sessions", "2026", "07", "29")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	sid := "019fae95-43eb-7491-9341-05c156228664"
	path := filepath.Join(dir, "rollout-2026-07-29T23-54-05-"+sid+".jsonl")
	writeJsonl(t, path,
		`{"type":"session_meta","payload":{"id":"`+sid+`","cwd":"`+cwd+`","thread_source":"user"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"你在做什么呢"}}`,
	)

	sess := session.New(uuid.New(), proto.SessionInfo{Cwd: cwd})
	defer sess.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go startCodexKnownTitleResolve(ctx, sess, cwd, sid)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := sess.Info().Title; got == "你在做什么呢" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session title = %q, want previous-day codex user message", sess.Info().Title)
}

func TestTrackCodexUserTitle_ReappliesAfterExternalTitleOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-2026-07-30T00-13-54-019faea7-292e-7ad3-a408-4faf2bb8a848.jsonl")
	writeJsonl(t, path,
		`{"type":"session_meta","payload":{"id":"019faea7-292e-7ad3-a408-4faf2bb8a848","cwd":"/Users/attson/code","thread_source":"user"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"web 端这里是不是应该换一种提示"}}`,
	)

	sess := session.New(uuid.New(), proto.SessionInfo{Cwd: "/Users/attson/code"})
	defer sess.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go trackCodexUserTitle(ctx, sess, path)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := sess.Info().Title; got == "web 端这里是不是应该换一种提示" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := sess.Info().Title; got != "web 端这里是不是应该换一种提示" {
		t.Fatalf("initial session title = %q, want codex user message", got)
	}

	sess.UpdateCwdTitle("", "codex")
	if got := sess.Info().Title; got != "codex" {
		t.Fatalf("test setup failed: title after overwrite = %q, want codex", got)
	}

	deadline = time.Now().Add(2 * codexTitleInterval)
	for time.Now().Before(deadline) {
		if got := sess.Info().Title; got == "web 端这里是不是应该换一种提示" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session title after overwrite = %q, want codex user message reapplied", sess.Info().Title)
}

func TestAdvancedSids(t *testing.T) {
	t0 := time.Now()
	prev := map[string]time.Time{"a": t0, "b": t0}

	// b advanced, c newly appeared, a unchanged → {b, c}.
	cur := map[string]time.Time{"a": t0, "b": t0.Add(time.Second), "c": t0.Add(time.Second)}
	got := map[string]bool{}
	for _, s := range advancedSids(prev, cur) {
		got[s] = true
	}
	if len(got) != 2 || !got["b"] || !got["c"] {
		t.Fatalf("advancedSids = %v, want {b,c}", got)
	}

	// nothing changed → empty (idle).
	if a := advancedSids(prev, map[string]time.Time{"a": t0, "b": t0}); len(a) != 0 {
		t.Fatalf("idle: advancedSids = %v, want empty", a)
	}

	// single switch: only the switched-to file advanced.
	a := advancedSids(prev, map[string]time.Time{"a": t0, "b": t0.Add(2 * time.Second)})
	if len(a) != 1 || a[0] != "b" {
		t.Fatalf("single advance = %v, want [b]", a)
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

func TestChooseNextSidContinuous(t *testing.T) {
	cases := []struct {
		name        string
		adv         []string
		titleMatch  string
		lastEmitted string
		want        string
	}{
		{
			name:        "idle: no advances, no switch",
			adv:         nil,
			titleMatch:  "",
			lastEmitted: "sid-a",
			want:        "",
		},
		{
			name:        "heartbeat: only our own jsonl advanced",
			adv:         []string{"sid-a"},
			titleMatch:  "sid-a",
			lastEmitted: "sid-a",
			want:        "",
		},
		{
			name:        "resume: single advance to a different sid AND title agrees → switch",
			adv:         []string{"sid-c"},
			titleMatch:  "sid-c",
			lastEmitted: "sid-a",
			want:        "sid-c",
		},
		{
			name:        "cross-talk: peer's jsonl advanced but our title hasn't moved → no switch",
			adv:         []string{"sid-b"},
			titleMatch:  "sid-a",
			lastEmitted: "sid-a",
			want:        "",
		},
		{
			name:        "cross-talk: peer's jsonl advanced and title is ambiguous → no switch",
			adv:         []string{"sid-b"},
			titleMatch:  "",
			lastEmitted: "sid-a",
			want:        "",
		},
		{
			name:        "ambiguous: multiple advances → fall back to title match",
			adv:         []string{"sid-b", "sid-c"},
			titleMatch:  "sid-c",
			lastEmitted: "sid-a",
			want:        "sid-c",
		},
		{
			name:        "ambiguous: multiple advances and title fails → no switch",
			adv:         []string{"sid-b", "sid-c"},
			titleMatch:  "",
			lastEmitted: "sid-a",
			want:        "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseNextSidContinuous(tc.adv, tc.titleMatch, tc.lastEmitted)
			if got != tc.want {
				t.Errorf("chooseNextSidContinuous(%v, %q, %q) = %q, want %q",
					tc.adv, tc.titleMatch, tc.lastEmitted, got, tc.want)
			}
		})
	}
}
