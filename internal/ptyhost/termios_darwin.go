package ptyhost

import "golang.org/x/sys/unix"

// macOS uses TIOCGETA / TIOCSETA for termios.
const (
	termiosGetReq = unix.TIOCGETA
	termiosSetReq = unix.TIOCSETA
)
