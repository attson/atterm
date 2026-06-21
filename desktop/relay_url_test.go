package main

import "testing"

// TestRelayHTTPBase locks the wss/ws -> https/http scheme conversion the
// relay REST calls (incl. the Feishu binding store) depend on. Go's
// http.Client rejects "wss"/"ws" with "unsupported protocol scheme", so the
// stored WebSocket relay URL must be rewritten before any HTTP request.
func TestRelayHTTPBase(t *testing.T) {
	cases := map[string]string{
		"wss://121.43.40.128:23303": "https://121.43.40.128:23303",
		"ws://localhost:8080":       "http://localhost:8080",
		"https://relay.example":     "https://relay.example",
		"http://relay.example":      "http://relay.example",
		"":                          "",
	}
	for in, want := range cases {
		if got := relayHTTPBase(in); got != want {
			t.Errorf("relayHTTPBase(%q) = %q; want %q", in, got, want)
		}
	}
}
