package main

import (
	"net/http"
	"testing"
	"time"
)

// The uplink hands this client to websocket.Dial, and a WebSocket upgrade
// cannot run over HTTP/2 — so the transport must have h2 disabled regardless of
// the insecure flag, otherwise the relay's ALPN-advertised h2 is negotiated and
// the handshake fails.
func TestRelayHTTPClientDisablesHTTP2(t *testing.T) {
	for _, allowInsecure := range []bool{false, true} {
		c := relayHTTPClient(allowInsecure, 0)
		tr, ok := c.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("allowInsecure=%v: Transport is %T, want *http.Transport", allowInsecure, c.Transport)
		}
		if tr.ForceAttemptHTTP2 {
			t.Errorf("allowInsecure=%v: ForceAttemptHTTP2 is true, want false", allowInsecure)
		}
		if tr.TLSNextProto == nil {
			t.Errorf("allowInsecure=%v: TLSNextProto is nil (h2 auto-upgrade enabled), want empty non-nil map", allowInsecure)
		}
	}
}

func TestRelayHTTPClientInsecureSkipsVerify(t *testing.T) {
	secure := relayHTTPClient(false, 0)
	if cfg := secure.Transport.(*http.Transport).TLSClientConfig; cfg != nil && cfg.InsecureSkipVerify {
		t.Error("allowInsecure=false: InsecureSkipVerify is true, want verification enabled")
	}

	insecure := relayHTTPClient(true, 5*time.Second)
	cfg := insecure.Transport.(*http.Transport).TLSClientConfig
	if cfg == nil || !cfg.InsecureSkipVerify {
		t.Error("allowInsecure=true: InsecureSkipVerify not set")
	}
	if insecure.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", insecure.Timeout)
	}
}
