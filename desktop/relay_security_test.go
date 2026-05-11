package main

import (
	"strings"
	"testing"
)

func TestValidateRelayEndpointRejectsPublicWSByDefault(t *testing.T) {
	err := validateRelayEndpoint("ws://relay.example.com:8080", false)
	if err == nil || !strings.Contains(err.Error(), "insecure") {
		t.Fatalf("err = %v; want insecure ws rejection", err)
	}
}

func TestValidateRelayEndpointAllowsPublicWSWhenUserOptedIn(t *testing.T) {
	if err := validateRelayEndpoint("ws://relay.example.com:8080", true); err != nil {
		t.Fatalf("validateRelayEndpoint err: %v", err)
	}
}

func TestValidateRelayEndpointAllowsLoopbackWS(t *testing.T) {
	for _, raw := range []string{
		"ws://localhost:8080",
		"ws://127.0.0.1:8080",
		"ws://[::1]:8080",
	} {
		if err := validateRelayEndpoint(raw, false); err != nil {
			t.Fatalf("validateRelayEndpoint(%q) err: %v", raw, err)
		}
	}
}

func TestValidateRelayEndpointRejectsUnsupportedScheme(t *testing.T) {
	err := validateRelayEndpoint("http://relay.example.com:8080", true)
	if err == nil || !strings.Contains(err.Error(), "ws:// or wss://") {
		t.Fatalf("err = %v; want unsupported scheme", err)
	}
}
