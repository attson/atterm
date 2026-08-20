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

// TestSealOpenSSHHostsRoundTripForwards closes design-doc §7.3 for roadmap
// item 26: Forwards is a []ForwardRule, not a plain string like item 25's
// three fields, so a shallow copy or a nil-vs-empty slice slip is a live
// possibility here in a way it was not before. Every field of the rule below
// gets a distinctive, unmistakable value so the assertions can't pass by
// accident (an empty field on both sides would prove nothing).
func TestSealOpenSSHHostsRoundTripForwards(t *testing.T) {
	key := testAccountKey(t)
	const (
		canaryRuleID     = "CANARY-rule-id-9f3a"
		canaryKind       = "remote"
		canaryBindAddr   = "CANARY-bind-addr-10.99.0.1"
		canaryBindPort   = "CANARY-bind-port-55221"
		canaryTargetHost = "CANARY-target-host-db.internal.example"
		canaryTargetPort = "CANARY-target-port-54329"
		canaryNote       = "CANARY-note-do-not-drop-me"
	)
	hosts := []SSHHost{{
		ID:       "1",
		Host:     "h",
		User:     "u",
		AuthKind: "key",
		Forwards: []ForwardRule{{
			ID:         canaryRuleID,
			Kind:       canaryKind,
			BindAddr:   canaryBindAddr,
			BindPort:   canaryBindPort,
			TargetHost: canaryTargetHost,
			TargetPort: canaryTargetPort,
			Note:       canaryNote,
		}},
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
	if len(got.Forwards) != 1 {
		t.Fatalf("expected 1 forward rule, got %d: %+v", len(got.Forwards), got.Forwards)
	}
	gotRule := got.Forwards[0]
	if gotRule.ID != canaryRuleID {
		t.Errorf("Forwards[0].ID did not survive round trip: got %q, want %q", gotRule.ID, canaryRuleID)
	}
	if gotRule.Kind != canaryKind {
		t.Errorf("Forwards[0].Kind did not survive round trip: got %q, want %q", gotRule.Kind, canaryKind)
	}
	if gotRule.BindAddr != canaryBindAddr {
		t.Errorf("Forwards[0].BindAddr did not survive round trip: got %q, want %q", gotRule.BindAddr, canaryBindAddr)
	}
	if gotRule.BindPort != canaryBindPort {
		t.Errorf("Forwards[0].BindPort did not survive round trip: got %q, want %q", gotRule.BindPort, canaryBindPort)
	}
	if gotRule.TargetHost != canaryTargetHost {
		t.Errorf("Forwards[0].TargetHost did not survive round trip: got %q, want %q", gotRule.TargetHost, canaryTargetHost)
	}
	if gotRule.TargetPort != canaryTargetPort {
		t.Errorf("Forwards[0].TargetPort did not survive round trip: got %q, want %q", gotRule.TargetPort, canaryTargetPort)
	}
	if gotRule.Note != canaryNote {
		t.Errorf("Forwards[0].Note did not survive round trip: got %q, want %q", gotRule.Note, canaryNote)
	}
}

// TestSealOpenSSHHostsRoundTripForwardsEmpty confirms a host with no
// forwarding rules at all round-trips without error — Forwards is
// `omitempty`, so a nil slice in must not become a non-nil empty slice (or
// vice versa) that trips up a caller doing a nil check downstream.
func TestSealOpenSSHHostsRoundTripForwardsEmpty(t *testing.T) {
	key := testAccountKey(t)
	hosts := []SSHHost{{
		ID:       "1",
		Host:     "h",
		User:     "u",
		AuthKind: "key",
	}}

	blob, err := sealSSHHosts(key, hosts, map[string]sshCredential{}, nil, nil)
	if err != nil {
		t.Fatalf("sealSSHHosts: %v", err)
	}

	gotHosts, _, _, _, err := openSSHHosts(key, blob)
	if err != nil {
		t.Fatalf("openSSHHosts: %v", err)
	}
	if len(gotHosts) != 1 {
		t.Fatalf("expected 1 host, got %d: %+v", len(gotHosts), gotHosts)
	}
	if len(gotHosts[0].Forwards) != 0 {
		t.Errorf("expected no forward rules, got %d: %+v", len(gotHosts[0].Forwards), gotHosts[0].Forwards)
	}
}
