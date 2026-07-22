package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeParseSid(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"valid", "0d03a640-2884-41bb-84b1-be79969a114a.jsonl", "0d03a640-2884-41bb-84b1-be79969a114a", true},
		{"wrong ext", "0d03a640-2884-41bb-84b1-be79969a114a.json", "", false},
		{"not uuid", "hello.jsonl", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sid, ok := claudeParseSid(tc.in)
			if ok != tc.ok || sid != tc.want {
				t.Fatalf("got (%q,%v) want (%q,%v)", sid, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCodexParseSid(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"valid", "rollout-2026-06-13T15-28-31-019ebfe1-df9e-79b1-881e-431db7cb6af6.jsonl", "019ebfe1-df9e-79b1-881e-431db7cb6af6", true},
		{"missing prefix", "2026-06-13T15-28-31-019ebfe1-df9e-79b1-881e-431db7cb6af6.jsonl", "", false},
		{"wrong ext", "rollout-2026-06-13T15-28-31-019ebfe1-df9e-79b1-881e-431db7cb6af6.json", "", false},
		{"short", "rollout-x.jsonl", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sid, ok := codexParseSid(tc.in)
			if ok != tc.ok || sid != tc.want {
				t.Fatalf("got (%q,%v) want (%q,%v)", sid, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestClaudeWatchDir(t *testing.T) {
	cases := []struct {
		name string
		cwd  string
		want string
	}{
		{
			// Plain path: only '/' separators to encode.
			name: "plain",
			cwd:  "/Users/me/code/foo",
			want: "/HOME/.claude/projects/-Users-me-code-foo",
		},
		{
			// Real git-worktree path. Claude Code encodes EVERY non-alphanumeric
			// char to '-', not just '/'. So the dot in "/.claude" becomes a
			// double dash, and '_' / '+' in the branch name also become '-'.
			// The old "ReplaceAll('/', '-')" only handled separators and missed
			// this dir entirely → AI session id never resolved (ai=- in logs)
			// and the worktree pane could not be --resumed after a restart.
			name: "worktree with dot underscore plus",
			cwd:  "/home/attson/GolandProjects/ad-ai-toolkit/.claude/worktrees/feature+edits_multi_image_20260624",
			want: "/HOME/.claude/projects/-home-attson-GolandProjects-ad-ai-toolkit--claude-worktrees-feature-edits-multi-image-20260624",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := claudeWatchDir(tc.cwd, time.Now(), "/HOME")
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestCodexWatchDir(t *testing.T) {
	tm := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	got := codexWatchDir("/x", tm, "/HOME")
	want := "/HOME/.codex/sessions/2026/06/13"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCodexRolloutSidsSkipsSubagentSessions(t *testing.T) {
	dir := t.TempDir()
	userSid := "019f8a8c-dfb3-7891-bca6-88f1f482d2d1"
	subagentSid := "019f8aed-8f6a-7fe0-96ed-0a4bd18f3bfe"
	write := func(name, line string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(
		"rollout-2026-07-22T23-58-35-"+userSid+".jsonl",
		`{"type":"session_meta","payload":{"id":"`+userSid+`","thread_source":"user","originator":"codex-tui"}}`,
	)
	write(
		"rollout-2026-07-23T01-44-12-"+subagentSid+".jsonl",
		`{"type":"session_meta","payload":{"id":"`+subagentSid+`","thread_source":"subagent","originator":"codex-tui","source":{"subagent":{"thread_spawn":{"parent_thread_id":"`+userSid+`"}}}}}`,
	)

	got := codexRolloutSids(dir)
	if _, ok := got[userSid]; !ok {
		t.Fatalf("missing user sid %s in %v", userSid, got)
	}
	if _, ok := got[subagentSid]; ok {
		t.Fatalf("subagent sid %s must not be resumable: %v", subagentSid, got)
	}
}
