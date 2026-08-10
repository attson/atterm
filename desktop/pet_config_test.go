package main

import (
	"encoding/json"
	"testing"
)

func TestPetConfigDefaults(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()

	if c.Pet.Enabled {
		t.Fatal("pet must default to disabled — it spawns a second process")
	}
	if c.Pet.Collapsed {
		t.Fatal("pet must default to expanded so the first launch shows the list")
	}
	if c.Pet.WindowX != -1 || c.Pet.WindowY != -1 {
		t.Fatalf("unset position must be (-1,-1), got (%d,%d)", c.Pet.WindowX, c.Pet.WindowY)
	}
	if c.Pet.MutedUntilUnix != 0 {
		t.Fatalf("pet must not start muted, got %d", c.Pet.MutedUntilUnix)
	}
}

func TestPetConfigDefaultsPreserveDraggedPosition(t *testing.T) {
	// applyDefaults runs on every load; it must not stomp a real position.
	var c PluginConfig
	c.Pet.WindowX = 1400
	c.Pet.WindowY = 820
	c.applyDefaults()

	if c.Pet.WindowX != 1400 || c.Pet.WindowY != 820 {
		t.Fatalf("dragged position clobbered: (%d,%d)", c.Pet.WindowX, c.Pet.WindowY)
	}
}

func TestPetConfigJSONRoundtrip(t *testing.T) {
	var orig PluginConfig
	orig.applyDefaults()
	orig.Pet.Enabled = true
	orig.Pet.Collapsed = true
	orig.Pet.WindowX = -1600 // a display left of the primary one
	orig.Pet.WindowY = 40
	orig.Pet.MutedUntilUnix = 1_800_000_000

	blob, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back PluginConfig
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Pet != orig.Pet {
		t.Fatalf("roundtrip mismatch:\n got %+v\nwant %+v", back.Pet, orig.Pet)
	}
}

func TestValidatePluginConfig_PetAcceptsNegativeScreenCoords(t *testing.T) {
	// A monitor to the left of / above the primary display yields negative
	// logical coordinates; rejecting those would strand the pet for anyone
	// with a multi-display setup.
	var c PluginConfig
	c.applyDefaults()
	c.Pet.WindowX = -1920
	c.Pet.WindowY = -300

	if err := ValidatePluginConfig(c); err != nil {
		t.Fatalf("negative screen coords must be accepted: %v", err)
	}
}

func TestValidatePluginConfig_PetRejectsAbsurdCoords(t *testing.T) {
	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"x too small", -40000, 0},
		{"x too large", 40000, 0},
		{"y too small", 0, -40000},
		{"y too large", 0, 40000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c PluginConfig
			c.applyDefaults()
			c.Pet.WindowX = tc.x
			c.Pet.WindowY = tc.y
			if err := ValidatePluginConfig(c); err == nil {
				t.Fatalf("expected rejection for (%d,%d)", tc.x, tc.y)
			}
		})
	}
}

func TestValidatePluginConfig_PetRejectsNegativeMute(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()
	c.Pet.MutedUntilUnix = -1
	if err := ValidatePluginConfig(c); err == nil {
		t.Fatal("expected rejection for negative mutedUntilUnix")
	}
}
