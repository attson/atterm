package main

import (
	"fmt"
	"github.com/attson/atterm/internal/sshclient"
)

// Older clients know ProxyCommand but not native proxy metadata. This marker
// makes them refuse the imported route instead of silently connecting directly.
const nativeProxyMarker = "atterm:native-proxy:v1"

func isNativeProxy(h SSHHost) bool { return h.ProxyCommand == nativeProxyMarker && h.Proxy != nil }

// sshProxyForHost never borrows the SSH password for a proxy login.
func sshProxyForHost(h SSHHost) (*sshclient.ProxyConfig, error) {
	if h.Proxy == nil {
		return nil, nil
	}
	credential, err := sshCredentialSlot(h.ID).Load()
	if err != nil {
		return nil, fmt.Errorf("load network proxy credential: %w", err)
	}
	return &sshclient.ProxyConfig{Kind: h.Proxy.Kind, Host: h.Proxy.Host, Port: h.Proxy.Port, Username: h.Proxy.Username, Password: credential.ProxyPassword}, nil
}
