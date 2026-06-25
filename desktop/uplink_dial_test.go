package main

import "testing"

func TestUplinkDialURL(t *testing.T) {
	cases := []struct {
		home, relay, want string
	}{
		{"", "wss://relay.example", "wss://relay.example"},                       // empty home → relayURL
		{"https://node-1.example", "wss://relay.example", "wss://node-1.example"}, // https → wss
		{"http://node-2.example", "wss://relay.example", "ws://node-2.example"},   // http → ws
		{"wss://node-3.example", "wss://relay.example", "wss://node-3.example"},   // already wss
		{"node-4.example", "wss://relay.example", "wss://node-4.example"},         // bare host → wss
		{"https://node-5.example/", "wss://relay.example", "wss://node-5.example"}, // trailing slash trimmed
	}
	for _, c := range cases {
		if got := uplinkDialURL(c.home, c.relay); got != c.want {
			t.Errorf("uplinkDialURL(%q, %q) = %q, want %q", c.home, c.relay, got, c.want)
		}
	}
}
