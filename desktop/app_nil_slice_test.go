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
		{"GetProfiles", func() any { return a.GetProfiles() }},
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

// A store with nothing saved is the case that matters — a populated store
// hides the bug, which is precisely why it survived so long.
func TestGetProfilesEmptyStoreMarshalsEmptyArray(t *testing.T) {
	a := &App{cfgStore: newTestConfigStore(t)}
	raw, err := json.Marshal(a.GetProfiles())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Fatalf("want [], got %s", raw)
	}
}

// The nil-App / nil-store guard path must honour the same contract as the
// happy path; it is the one a caller hits before the store is wired.
func TestGetProfilesNilStoreMarshalsEmptyArray(t *testing.T) {
	var a *App
	raw, err := json.Marshal(a.GetProfiles())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Fatalf("want [], got %s", raw)
	}
}
