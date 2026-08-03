package sshclient

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// KnownHostsCallback returns an ssh.HostKeyCallback backed by the known_hosts
// file at path. On an unknown host it calls onUnknown(host, fingerprint); if
// that returns true (TOFU accept) the key is appended to the file and the
// connection is allowed. A key that is present but differs (possible MITM) is
// always rejected, regardless of onUnknown.
func KnownHostsCallback(path string, onUnknown func(host, fingerprint string) bool) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		base, err := knownhosts.New(path)
		if err != nil {
			if os.IsNotExist(err) {
				// Missing file → everything is unknown; go straight to TOFU.
				return handleUnknown(path, hostname, key, onUnknown)
			}
			return fmt.Errorf("sshclient: load known_hosts: %w", err)
		}
		err = base(hostname, remote, key)
		if err == nil {
			return nil // known & matches
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) == 0 {
				return handleUnknown(path, hostname, key, onUnknown)
			}
			// Want non-empty → recorded but different → possible MITM.
			return fmt.Errorf("sshclient: host key mismatch for %s (possible MITM)", hostname)
		}
		return err
	}
}

func handleUnknown(path, hostname string, key ssh.PublicKey, onUnknown func(string, string) bool) error {
	fp := ssh.FingerprintSHA256(key)
	if onUnknown == nil || !onUnknown(hostname, fp) {
		return fmt.Errorf("sshclient: host key not accepted for %s", hostname)
	}
	if err := appendKnownHost(path, hostname, key); err != nil {
		return fmt.Errorf("sshclient: persist host key: %w", err)
	}
	return nil
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	_, err = f.WriteString(line + "\n")
	return err
}
