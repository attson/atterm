package main

import (
	"encoding/json"
	"github.com/attson/atterm/internal/termius"
	"strings"
	"testing"
)

func TestTermiusImportIncludesExactJumpAndProxyCredentials(t *testing.T) {
	a := newHostsTestApp(t)
	data := &termius.Data{Hosts: []termius.Host{
		{ID: "target", Name: "target", Address: "192.0.2.2", Port: "22", Username: "root", Password: "target-secret", JumpIDs: []string{"hop"}},
		{ID: "hop", Name: "duplicate", Address: "192.0.2.1", Port: "22", Username: "ops", Password: "hop-secret", Proxy: &termius.Proxy{Kind: "http", Host: "127.0.0.1", Port: "8080", Username: "proxy-user", Password: "proxy-secret"}},
	}}
	preview := a.stageTermiusImport(data)
	encoded, _ := json.Marshal(preview)
	if strings.Contains(string(encoded), "proxy-secret") {
		t.Fatal("proxy secret escaped preview")
	}
	report, err := a.ImportTermius(preview.Token, []string{"target"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Hosts != 2 {
		t.Fatalf("missing dependency: %+v", report)
	}
	hosts := a.ListSSHHosts()
	var target, hop SSHHost
	for _, h := range hosts {
		if h.Alias == "target" {
			target = h
		} else {
			hop = h
		}
	}
	// Deliberately prepend a duplicate alias/address with another credential.
	unrelated := hop
	unrelated.ID = "wrong"
	unrelated.User = "other"
	hops, err := resolveJumpHops(target, append([]SSHHost{unrelated}, hosts...))
	if err != nil || len(hops) != 1 || hops[0].ID != hop.ID {
		t.Fatalf("wrong jump resolved: %v", err)
	}
	if hop.ProxyCommand != nativeProxyMarker {
		t.Fatal("older clients could silently ignore native proxy")
	}
	if refused, _ := hostRunsProxyCommand(hop); refused {
		t.Fatal("native proxy marker blocked current client")
	}
	legacy := hop
	legacy.Proxy = nil
	if refused, _ := hostRunsProxyCommand(legacy); !refused {
		t.Fatal("missing proxy metadata allowed direct fallback")
	}
	credential, err := sshCredentialSlot(hop.ID).Load()
	if err != nil || credential.Password != "hop-secret" || credential.ProxyPassword != "proxy-secret" {
		t.Fatal("wrong proxy credential storage", err)
	}
	plain, _ := json.Marshal(a.cfgStore.Get())
	if strings.Contains(string(plain), "proxy-secret") {
		t.Fatal("proxy password persisted in config")
	}
	if err := a.UpdateSSHHost(hop, &sshCredential{Password: "updated"}); err != nil {
		t.Fatal(err)
	}
	credential, _ = sshCredentialSlot(hop.ID).Load()
	if credential.ProxyPassword != "proxy-secret" {
		t.Fatal("editing SSH password lost proxy password")
	}
	proxy, err := sshProxyForHost(hop)
	if err != nil || proxy.Password != "proxy-secret" {
		t.Fatal("proxy dial lost credential", err)
	}
}
func TestTermiusBlockedHopDisablesDestination(t *testing.T) {
	a := newHostsTestApp(t)
	p := a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{{ID: "target", JumpIDs: []string{"hop"}}, {ID: "hop", KeyID: "missing"}}})
	for _, h := range p.Hosts {
		if !h.Blocked {
			t.Fatal("invalid key in jump dependency not propagated")
		}
	}
	if _, err := a.ImportTermius(p.Token, []string{"target"}, nil); err == nil {
		t.Fatal("blocked dependency imported")
	}
}

func TestTermiusJumpCycleAndExistingRouteConflictAreRetryable(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		t.Run(map[bool]string{false: "conflict", true: "cycle"}[cycle], func(t *testing.T) {
			a := newHostsTestApp(t)
			hop := termius.Host{ID: "hop", Name: "bastion", Address: "192.0.2.1", Port: "22", Username: "ops", Password: "pw"}
			if cycle {
				hop.JumpIDs = []string{"target"}
			} else {
				_, err := a.AddSSHHost(SSHHost{Alias: hop.Name, Host: hop.Address, Port: hop.Port, User: hop.Username, AuthKind: "password", ProxyCommand: "do-not-run"}, sshCredential{Password: "existing"})
				if err != nil {
					t.Fatal(err)
				}
			}
			p := a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{{ID: "target", Name: "target", Address: "192.0.2.2", Port: "22", Username: "root", Password: "pw", JumpIDs: []string{"hop"}}, hop}})
			if _, err := a.ImportTermius(p.Token, []string{"target"}, nil); err == nil {
				t.Fatal("invalid dependency accepted")
			}
			if a.termiusImport.Load() == nil {
				t.Fatal("failed validation consumed preview")
			}
			for _, h := range a.ListSSHHosts() {
				if h.Alias == "target" {
					t.Fatal("partially wrote destination")
				}
			}
		})
	}
}
func TestProxySyncKeepsPasswordSealed(t *testing.T) {
	key := testAccountKey(t)
	host := SSHHost{ID: "host", Proxy: &SSHProxy{Kind: "socks5", Host: "localhost", Port: "1080", Username: "proxy-user"}}
	sealed, err := sealSSHHosts(key, []SSHHost{host}, map[string]sshCredential{"host": {Password: "ssh-password-canary", ProxyPassword: "proxy-password-canary"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "proxy-password-canary") {
		t.Fatal("plaintext sync")
	}
	hosts, creds, _, _, err := openSSHHosts(key, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if hosts[0].Proxy.Kind != "socks5" || creds["host"].ProxyPassword != "proxy-password-canary" || creds["host"].Password != "ssh-password-canary" {
		t.Fatal("proxy config or credential lost in sync")
	}
}
