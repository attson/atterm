package session

import "testing"

func TestDetectInteractiveShellCommandRecognizesBareSupportedShell(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{command: "bash", want: "bash"},
		{command: "zsh", want: "zsh"},
		{command: "fish", want: "fish"},
		{command: "pwsh", want: "pwsh"},
		{command: "powershell", want: "pwsh"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got, ok := DetectInteractiveShellCommand(tt.command)
			if !ok || got != tt.want {
				t.Fatalf("DetectInteractiveShellCommand(%q) = (%q, %v), want (%q, true)", tt.command, got, ok, tt.want)
			}
		})
	}
}

func TestDetectInteractiveShellCommandRecognizesSafeInteractiveForms(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{command: "/bin/bash -i", want: "bash"},
		{command: "bash --login", want: "bash"},
		{command: "/usr/bin/zsh -l", want: "zsh"},
		{command: "zsh -il", want: "zsh"},
		{command: "fish --interactive", want: "fish"},
		{command: "fish -il", want: "fish"},
		{command: `C:\PowerShell\pwsh.EXE -NOEXIT`, want: "pwsh"},
		{command: "PowerShell.exe -NoExit", want: "pwsh"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got, ok := DetectInteractiveShellCommand(tt.command)
			if !ok || got != tt.want {
				t.Fatalf("DetectInteractiveShellCommand(%q) = (%q, %v), want (%q, true)", tt.command, got, ok, tt.want)
			}
		})
	}
}

func TestDetectInteractiveShellCommandRejectsTaskAndAmbiguousForms(t *testing.T) {
	commands := []string{
		"",
		"   ",
		"zsh build.zsh",
		"bash deploy.sh",
		"bash -c 'make'",
		"bash -i -c 'make'",
		"fish -c 'echo ok'",
		"fish --command 'echo ok'",
		"pwsh -Command Get-Process",
		"pwsh -NoExit -Command Get-Process",
		"pwsh -File task.ps1",
		"pwsh -EncodedCommand ZQBjAGgAbwA=",
		"pwsh -NonInteractive",
		"docker compose up",
		"nu",
		"ZSH",
		"zsh --unknown",
		"bash -s",
		"zsh | cat",
		"bash && echo ok",
	}

	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			got, ok := DetectInteractiveShellCommand(command)
			if ok || got != "" {
				t.Fatalf("DetectInteractiveShellCommand(%q) = (%q, %v), want (\"\", false)", command, got, ok)
			}
		})
	}
}
