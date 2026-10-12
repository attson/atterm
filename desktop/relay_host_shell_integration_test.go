package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/attson/atterm/desktop/shellintegration"
)

func TestMergeShellIntegrationPlanBashLoginStarts(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	home := t.TempDir()
	rc := filepath.Join(home, "test.rc")
	if err := os.WriteFile(rc, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	argv, env := mergeShellIntegrationPlan(
		[]string{bash, "--noprofile", "-l"},
		[]string{"HOME=" + home, "PATH=/usr/bin:/bin"},
		shellintegration.Plan{Shell: "bash", ExtraArgs: []string{"--rcfile", rc, "-i"}},
	)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader("printf atterm-ready\nexit\n")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash login with shell integration failed: %v", err)
	}
	if string(output) != "atterm-ready" {
		t.Fatalf("bash output = %q; want atterm-ready", output)
	}
}

func TestMergeShellIntegrationPlanAppliesEnvAndArgs(t *testing.T) {
	baseEnv := []string{"PATH=/bin", "TERM=xterm-256color"}
	baseArgv := []string{"/bin/zsh"}
	plan := shellintegration.Plan{
		ExtraEnv:  []string{"ZDOTDIR=/tmp/x", "ATTERM_SHELL_INTEGRATION=1"},
		ExtraArgs: []string{"--rcfile", "/tmp/r"},
	}
	gotArgv, gotEnv := mergeShellIntegrationPlan(baseArgv, baseEnv, plan)
	wantArgv := []string{"/bin/zsh", "--rcfile", "/tmp/r"}
	if !reflect.DeepEqual(gotArgv, wantArgv) {
		t.Fatalf("argv = %v; want %v", gotArgv, wantArgv)
	}
	sort.Strings(gotEnv)
	wantEnv := append([]string{}, baseEnv...)
	wantEnv = append(wantEnv, plan.ExtraEnv...)
	sort.Strings(wantEnv)
	if !reflect.DeepEqual(gotEnv, wantEnv) {
		t.Fatalf("env = %v; want %v", gotEnv, wantEnv)
	}
}

func TestMergeShellIntegrationPlanZeroPlanIsIdentity(t *testing.T) {
	baseEnv := []string{"PATH=/bin"}
	baseArgv := []string{"/bin/zsh"}
	gotArgv, gotEnv := mergeShellIntegrationPlan(baseArgv, baseEnv, shellintegration.Plan{})
	if !reflect.DeepEqual(gotArgv, baseArgv) {
		t.Fatalf("argv changed for zero plan: %v != %v", gotArgv, baseArgv)
	}
	if !reflect.DeepEqual(gotEnv, baseEnv) {
		t.Fatalf("env changed for zero plan: %v != %v", gotEnv, baseEnv)
	}
}
