package main

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// notifySendMissingOnce throttles the "install notify-send" warning to a
// single log line per process lifetime, regardless of how many bells fire.
var notifySendMissingOnce sync.Once

// showNotification routes a (title, body) pair to the platform-native
// notification system. Returns nil unconditionally on success or graceful
// failure (missing tool, unsupported OS); errors are logged, not propagated.
//
// enabled and run are parameters so tests can drive gating + arg construction
// without bringing up a Wails runtime or actually shelling out.
func showNotification(
	ctx context.Context,
	enabled func() bool,
	run func(ctx context.Context, spec commandSpec) error,
	title, body string,
) error {
	if !enabled() {
		return nil
	}
	spec, ok := nativeNotifySpec(title, body)
	if !ok {
		return nil
	}
	return run(ctx, spec)
}

func nativeNotifySpec(title, body string) (commandSpec, bool) {
	return nativeNotifySpecForGOOS(runtime.GOOS, title, body)
}

func nativeNotifySpecForGOOS(goos, title, body string) (commandSpec, bool) {
	switch goos {
	case "darwin":
		// AppleScript notifications are attributed to the script runner, so
		// clicking them can activate Script Editor instead of AT Term.
		return commandSpec{}, false
	case "linux":
		return linuxNotifySpec(title, body), true
	case "windows":
		return windowsNotifySpec(title, body), true
	default:
		return commandSpec{}, false
	}
}

func linuxNotifySpec(title, body string) commandSpec {
	return commandSpec{name: "notify-send", args: []string{title, body}}
}

func windowsNotifySpec(title, body string) commandSpec {
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms",
		"Add-Type -AssemblyName System.Drawing",
		"$n = New-Object System.Windows.Forms.NotifyIcon",
		"$n.Icon = [System.Drawing.SystemIcons]::Information",
		"$n.Visible = $true",
		"$n.ShowBalloonTip(5000, " + powerShellSingleQuotedString(title) +
			", " + powerShellSingleQuotedString(body) +
			", [System.Windows.Forms.ToolTipIcon]::Info)",
		"Start-Sleep -Seconds 1",
		"$n.Dispose()",
	}, "; ")
	return commandSpec{name: "powershell.exe", args: []string{"-NoProfile", "-Command", script}}
}

// runNativeNotify is the production runner. Looks up the binary on PATH
// (logging once if notify-send is the missing one), shells out with the
// caller's context, and absorbs failures into a single log line.
func runNativeNotify(ctx context.Context, spec commandSpec) error {
	if _, err := exec.LookPath(spec.name); err != nil {
		if spec.name == "notify-send" {
			notifySendMissingOnce.Do(func() {
				logWarn("notify", "notify-send not on PATH; install libnotify-bin to enable bell notifications (error=%v)", err)
			})
		}
		return nil
	}
	cmd := exec.CommandContext(ctx, spec.name, spec.args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logWarn("notify", "%s failed error=%v output=%q", spec.name, err, string(out))
	}
	return nil
}
