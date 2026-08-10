package main

import (
	"encoding/json"
	"testing"
)

func TestWidgetConfigDefaults(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()

	if c.Widget.Enabled {
		t.Fatal("widget must default to disabled — it spawns a second process")
	}
	if c.Widget.Collapsed {
		t.Fatal("widget must default to expanded so the first launch shows the list")
	}
	if c.Widget.WindowX != -1 || c.Widget.WindowY != -1 {
		t.Fatalf("unset position must be (-1,-1), got (%d,%d)", c.Widget.WindowX, c.Widget.WindowY)
	}
	if c.Widget.MutedUntilUnix != 0 {
		t.Fatalf("widget must not start muted, got %d", c.Widget.MutedUntilUnix)
	}
	if c.Widget.AIOnly {
		// The widget's whole point is that OSC 133 gives it every command, not
		// just one vendor's agent; filtering has to be opt-in.
		t.Fatal("aiOnly must default to off")
	}
}

func TestWidgetConfigDefaultsPreserveDraggedPosition(t *testing.T) {
	// applyDefaults runs on every load; it must not stomp a real position.
	var c PluginConfig
	c.Widget.WindowX = 1400
	c.Widget.WindowY = 820
	c.applyDefaults()

	if c.Widget.WindowX != 1400 || c.Widget.WindowY != 820 {
		t.Fatalf("dragged position clobbered: (%d,%d)", c.Widget.WindowX, c.Widget.WindowY)
	}
}

func TestWidgetConfigJSONRoundtrip(t *testing.T) {
	var orig PluginConfig
	orig.applyDefaults()
	orig.Widget.Enabled = true
	orig.Widget.Collapsed = true
	orig.Widget.WindowX = -1600 // a display left of the primary one
	orig.Widget.WindowY = 40
	orig.Widget.MutedUntilUnix = 1_800_000_000
	orig.Widget.AIOnly = true

	blob, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back PluginConfig
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Widget != orig.Widget {
		t.Fatalf("roundtrip mismatch:\n got %+v\nwant %+v", back.Widget, orig.Widget)
	}
}

func TestValidatePluginConfig_WidgetAcceptsNegativeScreenCoords(t *testing.T) {
	// A monitor to the left of / above the primary display yields negative
	// logical coordinates; rejecting those would strand the widget for anyone
	// with a multi-display setup.
	var c PluginConfig
	c.applyDefaults()
	c.Widget.WindowX = -1920
	c.Widget.WindowY = -300

	if err := ValidatePluginConfig(c); err != nil {
		t.Fatalf("negative screen coords must be accepted: %v", err)
	}
}

func TestValidatePluginConfig_WidgetRejectsAbsurdCoords(t *testing.T) {
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
			c.Widget.WindowX = tc.x
			c.Widget.WindowY = tc.y
			if err := ValidatePluginConfig(c); err == nil {
				t.Fatalf("expected rejection for (%d,%d)", tc.x, tc.y)
			}
		})
	}
}

func TestValidatePluginConfig_WidgetRejectsNegativeMute(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()
	c.Widget.MutedUntilUnix = -1
	if err := ValidatePluginConfig(c); err == nil {
		t.Fatal("expected rejection for negative mutedUntilUnix")
	}
}
