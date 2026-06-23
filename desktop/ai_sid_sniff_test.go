package main

import "testing"

// computeResumeArgs must never produce a command-replay fallback for aider —
// the recovery design refuses to resume without a precise id.
func TestComputeResumeArgs_AiderReturnsNil(t *testing.T) {
	if got := computeResumeArgs("aider", "", "aider --model gpt-4"); got != nil {
		t.Fatalf("aider with no id: want nil, got %v", got)
	}
	if got := computeResumeArgs("aider", "some-id", "aider"); got != nil {
		t.Fatalf("aider with id: want nil (no id-based resume), got %v", got)
	}
}

func TestComputeResumeArgs_ClaudeCodex(t *testing.T) {
	cases := []struct {
		kind, sid string
		want      []string
	}{
		{"claude", "abc", []string{"claude", "--resume", "abc"}},
		{"codex", "xyz", []string{"codex", "resume", "xyz"}},
		{"claude", "", nil}, // no id → no resume
		{"unknown", "abc", nil},
	}
	for _, c := range cases {
		got := computeResumeArgs(c.kind, c.sid, "")
		if len(got) != len(c.want) {
			t.Fatalf("%s/%q: got %v want %v", c.kind, c.sid, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s/%q: got %v want %v", c.kind, c.sid, got, c.want)
			}
		}
	}
}

func TestClassifyAIKindFromCommand(t *testing.T) {
	cases := map[string]string{
		"claude --permission-mode bypassPermissions": "claude",
		"codex":                     "codex",
		"sudo claude":               "claude",
		"FOO=bar claude --resume x": "claude",
		"/usr/local/bin/claude":     "claude",
		"ls -G":                     "",
		"vim":                       "",
		"":                          "",
	}
	for cmd, want := range cases {
		if got := classifyAIKindFromCommand(cmd); got != want {
			t.Fatalf("classify(%q) = %q want %q", cmd, got, want)
		}
	}
}
