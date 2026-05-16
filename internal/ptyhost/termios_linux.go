package ptyhost

import "golang.org/x/sys/unix"

// Linux uses TCGETS / TCSETS for termios.
const (
	termiosGetReq = unix.TCGETS
	termiosSetReq = unix.TCSETS
)
