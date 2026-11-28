//go:build !windows

package main

import "os"

func replaceLocalFileAtomically(source, target string) error {
	return os.Rename(source, target)
}
