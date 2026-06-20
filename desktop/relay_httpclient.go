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
// The transport is cloned from http.DefaultTransport (preserving proxy and
// keep-alive settings) but HTTP/2 is forced OFF: this client is also handed to
// the uplink's websocket.Dial, and a WebSocket Upgrade cannot run over HTTP/2.
// The relay's TLS listener advertises h2 via ALPN, so a default (h2-capable)
// transport would negotiate HTTP/2 and the upgrade handshake would fail. The
// REST endpoints all work over HTTP/1.1 too, so a single HTTP/1.1 client is
// correct for every relay call.
func relayHTTPClient(allowInsecure bool, timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// Disable HTTP/2: clear the ALPN auto-upgrade map and stop force-attempting
	// h2 on TLS dials. Required for the WebSocket upgrade to succeed.
	tr.ForceAttemptHTTP2 = false
	tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}

	cfg := tr.TLSClientConfig
	if cfg == nil {
		cfg = &tls.Config{} // #nosec G402 -- InsecureSkipVerify set conditionally below
	} else {
		cfg = cfg.Clone()
	}
	// Pin the ALPN offer to HTTP/1.1. The relay's TLS listener advertises both
	// "h2" and "http/1.1"; disabling h2 on the transport is not enough — the
	// cloned DefaultTransport can still offer "h2" in the ClientHello, so the
	// server negotiates HTTP/2 and (a) the WebSocket upgrade fails and (b) the
	// REST client mis-parses the server's h2 SETTINGS frame as a malformed
	// HTTP/1.1 response. Offering only http/1.1 removes that ambiguity.
	cfg.NextProtos = []string{"http/1.1"}
	if allowInsecure {
		// #nosec G402 -- opt-in: the user explicitly enabled "trust self-signed
		// certificate" for this relay. A trusted-CA relay still verifies
		// normally (allowInsecure=false leaves verification on).
		cfg.InsecureSkipVerify = true
	}
	tr.TLSClientConfig = cfg
	return &http.Client{Timeout: timeout, Transport: tr}
}
