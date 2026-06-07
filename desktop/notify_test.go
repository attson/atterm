package main

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestNativeNotifySpecSkipsDarwinShellFallback(t *testing.T) {
	spec, ok := nativeNotifySpecForGOOS("darwin", "AT Term", "Bell in atterm")

	if ok {
		t.Fatalf("ok = true with spec %#v; want false so macOS notifications stay attributed to AT Term", spec)
	}
}

func TestLinuxNotifySpec(t *testing.T) {
	spec := linuxNotifySpec("AT Term", "Bell in atterm")

	if spec.name != "notify-send" {
		t.Fatalf("name = %q; want notify-send", spec.name)
	}
	if len(spec.args) != 2 || spec.args[0] != "AT Term" || spec.args[1] != "Bell in atterm" {
		t.Fatalf("args = %#v; want [title, body]", spec.args)
	}
}

func TestWindowsNotifySpecQuotesArgsAndUsesNotifyIcon(t *testing.T) {
	spec := windowsNotifySpec("AT Term", "Bell in atterm")

	if spec.name != "powershell.exe" {
		t.Fatalf("name = %q; want powershell.exe", spec.name)
	}
	if !strings.Contains(spec.args[len(spec.args)-1], "NotifyIcon") {
		t.Fatalf("script does not reference NotifyIcon: %q", spec.args[len(spec.args)-1])
	}
	if !strings.Contains(spec.args[len(spec.args)-1], "'AT Term'") {
		t.Fatalf("script does not single-quote title: %q", spec.args[len(spec.args)-1])
	}
	if !strings.Contains(spec.args[len(spec.args)-1], "'Bell in atterm'") {
		t.Fatalf("script does not single-quote body: %q", spec.args[len(spec.args)-1])
	}
}

func TestShowNotificationSkipsWhenDisabled(t *testing.T) {
	called := false
	err := showNotification(
		context.Background(),
		func() bool { return false },
		func(_ context.Context, _ commandSpec) error {
			called = true
			return nil
		},
		"title", "body",
	)
	if err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if called {
		t.Fatal("runner was invoked even though enabled returned false")
	}
}

func TestShowNotificationCallsRunnerWhenEnabled(t *testing.T) {
	var got commandSpec
	err := showNotification(
		context.Background(),
		func() bool { return true },
		func(_ context.Context, spec commandSpec) error {
			got = spec
			return nil
		},
		"AT Term", "Bell in atterm",
	)
	if err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if runtime.GOOS == "darwin" {
		if got.name != "" {
			t.Fatalf("runner was invoked on darwin with %#v; want native Wails notifications only", got)
		}
		return
	}
	if got.name == "" {
		t.Fatal("runner was not invoked")
	}
}
