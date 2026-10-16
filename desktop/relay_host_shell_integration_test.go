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

func TestMergeShellIntegrationPlanBashLoginLoadsRcfile(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	home := t.TempDir()
	rc := filepath.Join(home, "test.rc")
	if err := os.WriteFile(rc, []byte("printf '\\033]133;A\\007'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	argv, env := mergeShellIntegrationPlan(
		[]string{bash, "--noprofile", "-l"},
		[]string{"HOME=" + home, "PATH=/usr/bin:/bin"},
		shellintegration.Plan{Shell: "bash", ExtraArgs: []string{"--rcfile", rc, "-i"}},
	)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader("exit\n")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash login with shell integration failed: %v", err)
	}
	if !strings.Contains(string(output), "\x1b]133;A\x07") {
		t.Fatalf("bash login did not execute --rcfile, output = %q", output)
	}
	for _, arg := range argv {
		if arg == "-l" || arg == "--login" {
			t.Fatalf("bash argv retained login flag that disables --rcfile: %v", argv)
		}
	}
	if !containsString(env, "ATTERM_BASH_EMULATE_LOGIN=1") {
		t.Fatalf("bash env missing login emulation marker: %v", env)
	}
}

func TestBashLoginShellIntegrationEmitsCommandBoundary(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	plan := shellintegration.Prepare(bash, true, "real-bash-probe")
	if plan.Cleanup == nil {
		t.Fatal("bash integration plan has no cleanup")
	}
	defer plan.Cleanup()

	argv, env := mergeShellIntegrationPlan(
		[]string{bash, "-l"},
		[]string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "TERM=xterm-256color"},
		plan,
	)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader("printf atterm-ready\\n\nexit\n")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("integrated bash failed: %v; output=%q", err, output)
	}
	if !strings.Contains(string(output), "\x1b]133;C;printf atterm-ready") {
		t.Fatalf("integrated bash omitted command boundary, output=%q argv=%v", output, argv)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
