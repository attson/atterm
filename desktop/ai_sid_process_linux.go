//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const codexProcessRolloutLookupSupported = true

// codexRolloutOwnedByProcess returns the one candidate rollout currently open
// by rootPID or one of its descendants. The PTY child is normally a shell and
// Codex is one or two generations below it, so checking only rootPID is not
// sufficient.
func codexRolloutOwnedByProcess(rootPID int, files map[string]codexRolloutFileInfo) (string, bool) {
	if rootPID <= 0 || len(files) == 0 {
		return "", false
	}
	byPath := make(map[string]string, len(files))
	for sid, file := range files {
		byPath[filepath.Clean(file.Path)] = sid
	}

	matched := make(map[string]struct{})
	queue := []int{rootPID}
	seen := make(map[int]struct{})
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}

		fds, _ := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
		for _, fd := range fds {
			target, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, fd.Name()))
			if err != nil {
				continue
			}
			if sid, ok := byPath[filepath.Clean(target)]; ok {
				matched[sid] = struct{}{}
			}
		}

		children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pid, pid))
		if err != nil {
			continue
		}
		for _, field := range strings.Fields(string(children)) {
			child, err := strconv.Atoi(field)
			if err == nil && child > 0 {
				queue = append(queue, child)
			}
		}
	}

	if len(matched) != 1 {
		return "", false
	}
	for sid := range matched {
		return sid, true
	}
	return "", false
}
