package feishu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestNewService_RelayUsesConfiguredHTTPClient locks the self-signed-relay fix:
// the relay-backed binding store must make its REST calls through
// ServiceConfig.RelayHTTPClient (which production wires to the insecure-capable,
// ALPN-pinned relay client). Without the wiring the store builds its own default
// http.Client and the request never reaches the configured transport.
func TestNewService_RelayUsesConfiguredHTTPClient(t *testing.T) {
	var hits int
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		hits++
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"configured":false,"bound":false}`)),
			Header:     make(http.Header),
		}, nil
	})

	svc, err := NewService(ServiceConfig{
		Mode:            ModeRelay,
		RelayURL:        "https://relay.invalid", // unreachable: only the fake transport must answer
		RelayToken:      func() string { return "tok" },
		RelayHTTPClient: &http.Client{Transport: rt},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := svc.Store().Get(context.Background()); !errors.Is(err, ErrLocalBindingNotFound) {
		t.Fatalf("Get via configured client: got %v", err)
	}
	if hits != 1 {
		t.Fatalf("configured RelayHTTPClient not used: transport hits=%d", hits)
	}
}
