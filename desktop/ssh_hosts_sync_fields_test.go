package main

import "testing"

// TestSealOpenSSHHostsRoundTripProxyFields exists to close design-doc §7
// risk 1 for roadmap item 25: the assumption that adding fields to SSHHost
// "just works" because the vault is one sealed JSON blob must be *proven*,
// not assumed — roadmap item 21 found the relay's allowedPreferenceKeys
// whitelist silently rejecting nine new keys, and ssh_hosts_encrypted itself
// stayed broken for months on top of that. This test walks the same real
// seal -> sync-payload -> open path as TestSealOpenSSHHostsRoundTrip above,
// but with IdentityFile / ProxyJump / ProxyCommand populated with
// distinctive, unmistakable values so the assertions can't pass by accident
// (an empty string on both sides would prove nothing).
func TestSealOpenSSHHostsRoundTripProxyFields(t *testing.T) {
	key := testAccountKey(t)
	const (
		canaryIdentityFile = "CANARY-identity-file-/home/u/.ssh/id_ed25519_bastion"
		canaryProxyJump    = "CANARY-proxyjump-bastion.example.internal"
		canaryProxyCommand = "CANARY-proxycommand-ssh -W %h:%p jumpbox.example.internal"
	)
	hosts := []SSHHost{{
		ID:           "1",
		Host:         "h",
		User:         "u",
		AuthKind:     "key",
		IdentityFile: canaryIdentityFile,
		ProxyJump:    canaryProxyJump,
		ProxyCommand: canaryProxyCommand,
	}}

	blob, err := sealSSHHosts(key, hosts, map[string]sshCredential{}, nil, nil)
	if err != nil {
		t.Fatalf("sealSSHHosts: %v", err)
	}
	if blob == nil {
		t.Fatal("expected ciphertext, got nil")
	}

	gotHosts, _, _, _, err := openSSHHosts(key, blob)
	if err != nil {
		t.Fatalf("openSSHHosts: %v", err)
	}
	if len(gotHosts) != 1 {
		t.Fatalf("expected 1 host, got %d: %+v", len(gotHosts), gotHosts)
	}
	got := gotHosts[0]
	if got.IdentityFile != canaryIdentityFile {
		t.Errorf("IdentityFile did not survive round trip: got %q, want %q", got.IdentityFile, canaryIdentityFile)
	}
	if got.ProxyJump != canaryProxyJump {
		t.Errorf("ProxyJump did not survive round trip: got %q, want %q", got.ProxyJump, canaryProxyJump)
	}
	if got.ProxyCommand != canaryProxyCommand {
		t.Errorf("ProxyCommand did not survive round trip: got %q, want %q", got.ProxyCommand, canaryProxyCommand)
	}
}
