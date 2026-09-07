//go:build !linux

package main

const codexProcessRolloutLookupSupported = false

func codexRolloutOwnedByProcess(_ int, _ map[string]codexRolloutFileInfo) (string, bool) {
	return "", false
}
