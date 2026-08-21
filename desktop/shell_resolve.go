package main

import (
	"os"
	"runtime"
)

// shellPriorityOrder calls yield once per candidate shell name, in priority
// order, stopping as soon as yield returns false.
//
// It exists so a locally-created tab (App.ListShells, which the frontend
// uses to build the "new tab" menu) and a mobile-created one
// (defaultShellForNewSession in session_create_handler.go, which has no
// frontend to consult) agree on what "the default shell" means. They used
// to be two independently written copies of the same priority list — a
// review caught that the session-create test suite stayed green even with
// the configured-default-shell branch deleted from its copy, because
// nothing exercised the two paths against each other. One shared order
// removes that seam: both callers now read the same seven lines, so a
// change to the priority list (or a bug in it) affects, and is visible on,
// both.
//
// Order: the user's configured default_shell (unless "auto"), then $SHELL
// (skipped on Windows, which has no equivalent env convention here), then a
// short list of well-known shell names. Callers are responsible for
// resolving each candidate against PATH (exec.LookPath) — this function
// only knows the order, not which candidates actually exist on this
// machine.
func shellPriorityOrder(cfgStore *configStore, yield func(shell string) bool) {
	if cfgStore != nil {
		if configured := cfgStore.Get().DefaultShellOrDefault(); configured != defaultShellAuto {
			if !yield(configured) {
				return
			}
		}
	}
	if runtime.GOOS != "windows" {
		if !yield(os.Getenv("SHELL")) {
			return
		}
	}
	candidates := []string{"bash", "zsh", "fish", "sh"}
	if runtime.GOOS == "windows" {
		candidates = windowsShellCandidates()
	}
	for _, c := range candidates {
		if !yield(c) {
			return
		}
	}
}
