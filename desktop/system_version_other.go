//go:build !darwin && !linux && !windows

package main

func systemVersion() string {
	return ""
}
