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

// TestComputeResumeArgs_ClaudePreservesFlags verifies the recovered claude
// resume command carries the flags the user originally launched with
// (--permission-mode, --model, ...) — the reported bug was that they were lost.
func TestComputeResumeArgs_ClaudePreservesFlags(t *testing.T) {
	eq := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	cases := []struct {
		name, cmd string
		want      []string
	}{
		{
			"permission-mode preserved",
			"claude --permission-mode bypassPermissions",
			[]string{"claude", "--resume", "id1", "--permission-mode", "bypassPermissions"},
		},
		{
			"dangerously-skip + model preserved",
			"claude --dangerously-skip-permissions --model opus",
			[]string{"claude", "--resume", "id1", "--dangerously-skip-permissions", "--model", "opus"},
		},
		{
			// Second-recovery cycle: the previously injected `--resume <old>`
			// must be stripped so we don't double-resume, flags kept.
			"strip prior --resume, keep flag",
			"claude --resume oldsid --permission-mode bypassPermissions",
			[]string{"claude", "--resume", "id1", "--permission-mode", "bypassPermissions"},
		},
		{
			"strip --continue",
			"claude --continue --model opus",
			[]string{"claude", "--resume", "id1", "--model", "opus"},
		},
		{
			"strip --resume= form",
			"claude --resume=oldsid --model opus",
			[]string{"claude", "--resume", "id1", "--model", "opus"},
		},
		{
			// --resume followed by another flag must NOT swallow that flag.
			"resume without value does not eat next flag",
			"claude --resume --verbose",
			[]string{"claude", "--resume", "id1", "--verbose"},
		},
		{
			"env prefix + wrapper skipped",
			"FOO=bar sudo claude --permission-mode plan",
			[]string{"claude", "--resume", "id1", "--permission-mode", "plan"},
		},
		{
			"binary path token dropped",
			"/usr/local/bin/claude --model opus",
			[]string{"claude", "--resume", "id1", "--model", "opus"},
		},
		{
			"no flags",
			"claude",
			[]string{"claude", "--resume", "id1"},
		},
		{
			"empty command line falls back",
			"",
			[]string{"claude", "--resume", "id1"},
		},
	}
	for _, c := range cases {
		got := computeResumeArgs("claude", "id1", c.cmd)
		if !eq(got, c.want) {
			t.Fatalf("%s: computeResumeArgs(claude, id1, %q) = %v want %v", c.name, c.cmd, got, c.want)
		}
	}
}

// Flag preservation is claude-only: codex keeps its bare resume invocation
// regardless of the original command line.
func TestComputeResumeArgs_CodexIgnoresFlags(t *testing.T) {
	got := computeResumeArgs("codex", "xyz", "codex --model o3")
	want := []string{"codex", "resume", "xyz"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
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
