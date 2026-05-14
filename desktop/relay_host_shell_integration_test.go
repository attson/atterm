package main

import (
	"reflect"
	"sort"
	"testing"

	"github.com/attson/atterm/desktop/shellintegration"
)

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
