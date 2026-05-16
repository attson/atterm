//go:build !darwin && !linux

package ptyhost

// Other platforms (windows, freebsd, etc.) — termios is either unavailable
// or the constants differ. Default to 0 which causes IoctlGetTermios to
// fail; the caller already ignores the error so behavior reduces to "no
// IUTF8 tweak", matching pre-fix behavior on those platforms.
const (
	termiosGetReq = 0
	termiosSetReq = 0
)
