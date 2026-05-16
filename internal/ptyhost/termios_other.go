//go:build !darwin && !linux

package ptyhost

// applyTermiosTweaks is a no-op on platforms without POSIX termios
// (windows). The Windows console-host pty already speaks UTF-8 natively
// via WT_SESSION / VT processing; the macOS/Linux IUTF8 quirk does not
// apply here.
func applyTermiosTweaks(fd uintptr) {}
