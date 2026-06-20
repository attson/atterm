package main

import (
	"crypto/tls"
	"net/http"
	"time"
)

// relayHTTPClient returns an *http.Client for talking to the relay. When
// allowInsecure is set the client skips TLS certificate verification, which is
// what lets the desktop reach a relay serving a self-signed certificate — the
// default for atterm-relay quick-start. timeout==0 leaves the client without a
// timeout (required for the long-lived uplink WebSocket handshake).
//
// The transport is cloned from http.DefaultTransport so proxy, keep-alive and
// HTTP/2 settings are preserved; only TLSClientConfig is overridden.
func relayHTTPClient(allowInsecure bool, timeout time.Duration) *http.Client {
	c := &http.Client{Timeout: timeout}
	if allowInsecure {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		// #nosec G402 -- opt-in: the user explicitly enabled "trust self-signed
		// certificate" for this relay. Plain wss:// with a trusted CA still
		// verifies normally (allowInsecure=false).
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		c.Transport = tr
	}
	return c
}
