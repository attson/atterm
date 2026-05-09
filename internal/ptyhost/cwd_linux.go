//go:build linux

package ptyhost

import (
	"os"
	"strconv"
)

// readCwd resolves the child process's current working directory by following
// /proc/<pid>/cwd, which is a symlink the kernel maintains.
func readCwd(pid int) string {
	dst, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	if err != nil {
		return ""
	}
	return dst
}
