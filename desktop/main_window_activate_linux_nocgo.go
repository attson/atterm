//go:build linux && !cgo

package main

import "fmt"

func requestMainWindowActivation() error {
	return fmt.Errorf("X11 activation unavailable without cgo")
}
