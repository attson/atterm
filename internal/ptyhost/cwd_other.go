//go:build !linux && !darwin

package ptyhost

// readCwd is a stub on platforms where /proc isn't available. Cwd-driven tab
// titles will fall back to the command name on these systems.
func readCwd(pid int) string { return "" }
