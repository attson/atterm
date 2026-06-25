package shellintegration

import (
	"strings"
	"testing"
)

func TestEmbeddedSnippetsArePresent(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"zsh", zshSnippet},
		{"bash", bashSnippet},
		{"fish", fishSnippet},
		{"pwsh", pwshSnippet},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.content) == 0 {
				t.Fatalf("%s snippet is empty", c.name)
			}
		})
	}
}

func TestZshSnippetHasGuardAndHookRegistration(t *testing.T) {
	if !strings.Contains(zshSnippet, `ATTERM_SHELL_INTEGRATION`) {
		t.Fatalf("zsh snippet missing ATTERM_SHELL_INTEGRATION guard")
	}
	if !strings.Contains(zshSnippet, "preexec_functions") {
		t.Fatalf("zsh snippet does not append to preexec_functions")
	}
	if !strings.Contains(zshSnippet, "precmd_functions") {
		t.Fatalf("zsh snippet does not append to precmd_functions")
	}
	if !strings.Contains(zshSnippet, `\033]133`) && !strings.Contains(zshSnippet, `\x1b]133`) {
		t.Fatalf("zsh snippet does not emit OSC 133 sequences")
	}
	// Must emit the FULL command line ($3), not zsh's size-truncated $2 form.
	// $2 drops the trailing token (e.g. loses "bypassPermissions" from
	// "claude --permission-mode bypassPermissions"), which broke AI-session
	// resume flag preservation. $3 is the complete, alias-expanded command.
	if !strings.Contains(zshSnippet, "133;C;%s") || !strings.Contains(zshSnippet, `${3:-`) {
		t.Fatalf("zsh snippet must emit the full ($3) preexec command in OSC 133;C, not the truncated $2 form")
	}
}

func TestBashSnippetHasGuardAndHookRegistration(t *testing.T) {
	if !strings.Contains(bashSnippet, `ATTERM_SHELL_INTEGRATION`) {
		t.Fatalf("bash snippet missing ATTERM_SHELL_INTEGRATION guard")
	}
	if !strings.Contains(bashSnippet, "PROMPT_COMMAND") {
		t.Fatalf("bash snippet does not chain into PROMPT_COMMAND")
	}
	if !strings.Contains(bashSnippet, "DEBUG") {
		t.Fatalf("bash snippet does not trap DEBUG for preexec")
	}
	if !strings.Contains(bashSnippet, "133;C;%s") || !strings.Contains(bashSnippet, "BASH_COMMAND") {
		t.Fatalf("bash snippet does not include BASH_COMMAND in OSC 133;C")
	}
}

func TestFishSnippetHasGuardAndEventHooks(t *testing.T) {
	if !strings.Contains(fishSnippet, "__atterm_loaded") {
		t.Fatalf("fish snippet missing __atterm_loaded guard")
	}
	if !strings.Contains(fishSnippet, "fish_preexec") {
		t.Fatalf("fish snippet missing fish_preexec hook")
	}
	if !strings.Contains(fishSnippet, "fish_postexec") {
		t.Fatalf("fish snippet missing fish_postexec hook")
	}
	if !strings.Contains(fishSnippet, "133;C;%s") || !strings.Contains(fishSnippet, "string join") {
		t.Fatalf("fish snippet does not include the preexec command in OSC 133;C")
	}
}

func TestPwshSnippetHasGuardAndPromptWrapper(t *testing.T) {
	if !strings.Contains(pwshSnippet, "ATTERM_SHELL_INTEGRATION") {
		t.Fatalf("pwsh snippet missing ATTERM_SHELL_INTEGRATION guard")
	}
	if !strings.Contains(pwshSnippet, "function prompt") && !strings.Contains(pwshSnippet, "function global:prompt") {
		t.Fatalf("pwsh snippet does not wrap the prompt function")
	}
	if !strings.Contains(pwshSnippet, "PSReadLine") && !strings.Contains(pwshSnippet, "AddToHistoryHandler") && !strings.Contains(pwshSnippet, "133;C") {
		t.Fatalf("pwsh snippet does not emit OSC 133;C on preexec (either via PSReadLine handler or inline)")
	}
}
