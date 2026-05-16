package main

import (
	"reflect"
	"testing"
)

// macOS GUI processes (atterm.app launched from Finder/Dock) inherit a
// minimal PATH; only login shells run /etc/zprofile where path_helper
// extends PATH with /etc/paths.d/* (Docker Desktop, Homebrew). Without
// -l, "docker" reports command-not-found even though Terminal.app finds
// it fine. defaultShellArgs must match Terminal.app's behavior.
func TestDefaultShellArgsAddsLoginFlagForCommonShells(t *testing.T) {
	cases := []struct {
		name    string
		command string
		args    []string
		want    []string
	}{
		{"zsh basename", "zsh", nil, []string{"-l"}},
		{"zsh absolute", "/bin/zsh", nil, []string{"-l"}},
		{"bash absolute", "/bin/bash", nil, []string{"-l"}},
		{"fish in homebrew", "/usr/local/bin/fish", nil, []string{"-l"}},
		{"explicit args win", "/bin/zsh", []string{"-c", "echo hi"}, []string{"-c", "echo hi"}},
		{"plain sh skipped (dash semantics vary)", "/bin/sh", nil, nil},
		{"windows powershell skipped", "powershell.exe", nil, nil},
		{"windows cmd skipped", "cmd.exe", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := defaultShellArgs(tc.command, tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("defaultShellArgs(%q, %v) = %v; want %v",
					tc.command, tc.args, got, tc.want)
			}
		})
	}
}
