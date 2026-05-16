package ptyhost

import "golang.org/x/sys/unix"

// applyTermiosTweaks sets IUTF8 on the PTY so the kernel line discipline
// treats multi-byte UTF-8 input as one character (relevant for backspace
// and continuation-byte echo). macOS uses TIOCGETA / TIOCSETA.
func applyTermiosTweaks(fd uintptr) {
	t, err := unix.IoctlGetTermios(int(fd), unix.TIOCGETA)
	if err != nil {
		return
	}
	t.Iflag |= unix.IUTF8
	_ = unix.IoctlSetTermios(int(fd), unix.TIOCSETA, t)
}
