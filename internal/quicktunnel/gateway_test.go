package quicktunnel

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestGatewayBindsRandomLoopbackPort(t *testing.T) {
	t.Parallel()

	gateway, err := OpenGateway(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "peer gateway")
	}))
	if err != nil {
		t.Fatalf("OpenGateway: %v", err)
	}

	host, port, err := net.SplitHostPort(gateway.Address())
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", gateway.Address(), err)
	}
	if host != "127.0.0.1" || port == "0" || port == "" {
		t.Fatalf("gateway address = %q, want allocated IPv4 loopback port", gateway.Address())
	}

	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(gateway.Origin())
	if err != nil {
		t.Fatalf("GET gateway: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read gateway response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "peer gateway" {
		t.Fatalf("gateway response = %d %q", resp.StatusCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := gateway.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if conn, err := gateway.listener.Accept(); err == nil {
		_ = conn.Close()
		t.Fatal("original gateway listener still accepts connections after Close")
	}
}

func TestGatewayNilHandlerDeniesRequests(t *testing.T) {
	t.Parallel()

	gateway, err := OpenGateway(nil)
	if err != nil {
		t.Fatalf("OpenGateway: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = gateway.Close(ctx)
	}()

	resp, err := (&http.Client{Timeout: time.Second}).Get(gateway.Origin())
	if err != nil {
		t.Fatalf("GET gateway: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("nil-handler status = %d, want 404", resp.StatusCode)
	}
}
