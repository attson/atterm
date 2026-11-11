//go:build !windows && !linux

package main

import (
	"errors"
	"github.com/attson/atterm/internal/termius"
	"github.com/zalando/go-keyring"
)

func readTermiusKeys() ([][]byte, error) {
	keys := [][]byte{}
	for _, service := range []string{"termius-app", "Termius", "Termius (MAS)", "termius", "com.termius-dmg.mac", "com.termius.mac"} {
		value, e := keyring.Get(service, "localKey")
		if e != nil {
			continue
		}
		if key, e := termius.DecodeKey(value); e == nil {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil, errors.New("termius_local_key_unavailable")
	}
	return keys, nil
}
