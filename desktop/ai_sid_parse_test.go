package main

import (
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
	got := claudeWatchDir("/Users/me/code/foo", time.Now(), "/HOME")
	want := "/HOME/.claude/projects/-Users-me-code-foo"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
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
