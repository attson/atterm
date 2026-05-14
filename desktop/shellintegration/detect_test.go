package shellintegration

import "testing"

func TestDetectShell(t *testing.T) {
	tests := []struct {
		name string
		path string
		want Shell
	}{
		{"empty", "", ShellUnknown},
		{"zsh absolute", "/bin/zsh", ShellZsh},
		{"zsh homebrew", "/opt/homebrew/bin/zsh", ShellZsh},
		{"bash absolute", "/bin/bash", ShellBash},
		{"bash usr", "/usr/bin/bash", ShellBash},
		{"fish", "/opt/homebrew/bin/fish", ShellFish},
		{"pwsh posix", "/usr/local/bin/pwsh", ShellPwsh},
		{"pwsh windows exe", `C:\Program Files\PowerShell\7\pwsh.exe`, ShellPwsh},
		{"powershell exe", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, ShellPwsh},
		{"cmd not supported", `C:\Windows\System32\cmd.exe`, ShellUnknown},
		{"nu not supported", "/opt/homebrew/bin/nu", ShellUnknown},
		{"basename only", "zsh", ShellZsh},
		{"basename only pwsh.exe", "pwsh.exe", ShellPwsh},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectShell(tt.path)
			if got != tt.want {
				t.Fatalf("DetectShell(%q) = %v; want %v", tt.path, got, tt.want)
			}
		})
	}
}
