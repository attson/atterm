package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Wails marshals a nil Go slice to JSON `null`, not `[]`. Frontend code that
// reasonably does `(await getX()).length` then throws a TypeError — and it
// throws on the *empty* case, which is usually the one nobody mocks. This bit
// the ssh_config import preview: `Skipped` was only ever appended to, so a
// config with nothing skipped crashed the drawer while nine tests passed,
// because every mock hard-coded `skipped: []`.
//
// ListSSHHosts and GetPinnedSessionIds already convert nil to an empty slice
// for exactly this reason. This test pins the rest of the slice-returning
// bound methods to the same contract so the next one added does not have to
// rediscover it in a UI crash.
func TestBoundSliceMethodsNeverMarshalNull(t *testing.T) {
	cs := newTestConfigStore(t)
	a := &App{cfgStore: cs}

	cases := []struct {
		name string
		call func() any
	}{
		{"GetPinnedSessionIds", func() any { return a.GetPinnedSessionIds() }},
		{"ListSSHHosts", func() any { return a.ListSSHHosts() }},
		{"ListSSHKeys", func() any { return a.ListSSHKeys() }},
		{"GetPasteboardFileURLs", func() any { return a.GetPasteboardFileURLs() }},
		{"ListShells", func() any { return a.ListShells() }},
		{"ListActiveForwards", func() any { return a.ListActiveForwards() }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.call())
			if err != nil {
				t.Fatalf("marshal %s: %v", tc.name, err)
			}
			if string(raw) == "null" {
				t.Fatalf("%s marshals to null; the frontend sees null and "+
					"`.length` throws. Return an empty slice instead.", tc.name)
			}
			if !strings.HasPrefix(string(raw), "[") {
				t.Fatalf("%s should marshal to a JSON array, got %s", tc.name, raw)
			}
		})
	}
}
