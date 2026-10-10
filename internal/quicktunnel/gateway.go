// Package quicktunnel supervises the local endpoint and cloudflared process
// used by the accountless Peer transport. It provides reachability only;
// callers remain responsible for Peer authentication and payload encryption.
package quicktunnel

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	gatewayReadHeaderTimeout = 5 * time.Second
	gatewayIdleTimeout       = 30 * time.Second
	gatewayMaxHeaderBytes    = 1 << 20
)

// Gateway is a bounded Peer HTTP endpoint. OpenGateway uses an ephemeral IPv4
// loopback port; OpenGatewayAt may expose an explicitly configured LAN socket.
type Gateway struct {
	listener net.Listener
	server   *http.Server
	done     chan error

	closeOnce sync.Once
	closeErr  error
}

// OpenGateway starts a loopback HTTP endpoint. A nil handler deliberately
// exposes nothing, so transport lifecycle work cannot accidentally publish a
// diagnostic or default mux before the authenticated Peer handler is wired.
func OpenGateway(handler http.Handler) (*Gateway, error) {
	return OpenGatewayAt("tcp4", "127.0.0.1:0", handler)
}

// OpenGatewayAt starts the same bounded Peer HTTP endpoint on an explicit
// address. Callers use it only after a user enables a manual LAN listener.
func OpenGatewayAt(network, address string, handler http.Handler) (*Gateway, error) {
	if network != "tcp4" && network != "tcp6" {
		return nil, errors.New("unsupported Peer gateway network")
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	if handler == nil {
		handler = http.NotFoundHandler()
	}

	gateway := &Gateway{
		listener: listener,
		server: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: gatewayReadHeaderTimeout,
			IdleTimeout:       gatewayIdleTimeout,
			MaxHeaderBytes:    gatewayMaxHeaderBytes,
		},
		done: make(chan error, 1),
	}
	go func() {
		err := gateway.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		gateway.done <- err
		close(gateway.done)
	}()
	return gateway, nil
}

// Address returns the allocated loopback host:port.
func (g *Gateway) Address() string {
	return g.listener.Addr().String()
}

// Origin returns the HTTP origin for this gateway.
func (g *Gateway) Origin() string {
	return "http://" + g.Address()
}

// Close stops accepting requests and waits for active handlers until ctx
// expires. A timed-out graceful shutdown is followed by a hard server close.
func (g *Gateway) Close(ctx context.Context) error {
	g.closeOnce.Do(func() {
		g.closeErr = g.server.Shutdown(ctx)
		if g.closeErr != nil {
			_ = g.server.Close()
		}
		_ = g.listener.Close()
		<-g.done
	})
	return g.closeErr
}
