package main

import (
	"encoding/binary"
	"errors"
	"unicode/utf16"

	"github.com/attson/atterm/internal/termius"
	"github.com/danieljoos/wincred"
)

func readTermiusKeys() ([][]byte, error) {
	keys := [][]byte{}
	// keytar uses a slash target; atterm's keyring library uses a colon, so
	// generic keyring.Get would address a different credential on Windows.
	for _, service := range []string{"Termius", "termius-app", "Termius (MAS)"} {
		credential, e := wincred.GetGenericCredential(service + "/localKey")
		if e != nil {
			continue
		}
		raw := credential.CredentialBlob
		if key, e := termius.DecodeKey(string(raw)); e == nil {
			keys = append(keys, key)
			continue
		}
		if len(raw)%2 == 0 {
			units := make([]uint16, len(raw)/2)
			for i := range units {
				units[i] = binary.LittleEndian.Uint16(raw[i*2:])
			}
			if key, e := termius.DecodeKey(string(utf16.Decode(units))); e == nil {
				keys = append(keys, key)
			}
		}
	}
	if len(keys) == 0 {
		return nil, errors.New("termius_local_key_unavailable")
	}
	return keys, nil
}
