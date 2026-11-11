package termius

import "testing"

func routeFixture() tables {
	return tables{
		"hosts":          {{"id": "target", "address": "192.0.2.2", "ssh_config": "target-cfg"}, {"id": "hop", "address": "192.0.2.1", "ssh_config": "hop-cfg"}},
		"ssh_configs":    {{"id": "target-cfg", "identity": "target-auth"}, {"id": "hop-cfg", "identity": "hop-auth"}},
		"ssh_identities": {{"id": "target-auth", "username": "root", "password": "target-pw"}, {"id": "hop-auth", "username": "ops", "password": "hop-pw"}},
	}
}
func TestProxyAndIndependentJumpTable(t *testing.T) {
	raw := routeFixture()
	raw["host_chains"] = []map[string]interface{}{{"id": "chain", "ssh_config": "target-cfg", "hosts_chain": []interface{}{"hop"}}}
	raw["ssh_configs"][1]["proxycommand"] = "proxy"
	raw["proxies"] = []map[string]interface{}{{"id": "proxy", "type": "http", "hostname": "127.0.0.1", "port": int64(8080), "identity": "proxy-auth"}}
	raw["ssh_identities"] = append(raw["ssh_identities"], map[string]interface{}{"id": "proxy-auth", "username": "proxy-user", "password": "proxy-password"})
	data, err := buildData(raw)
	if err != nil {
		t.Fatal(err)
	}
	var target, hop Host
	for _, h := range data.Hosts {
		if h.ID == "id:target" {
			target = h
		} else {
			hop = h
		}
	}
	if target.Blocked || hop.Blocked || len(target.JumpIDs) != 1 || target.JumpIDs[0] != "id:hop" {
		t.Fatalf("route lost or blocked: %v %v", target.Warnings, hop.Warnings)
	}
	if hop.Proxy == nil || hop.Proxy.Kind != "http" || hop.Proxy.Password != "proxy-password" || hop.Password != "hop-pw" || target.Password != "target-pw" {
		t.Fatal("proxy and SSH credentials were mixed")
	}
}
func TestInvalidJumpReferencesAndCycles(t *testing.T) {
	for _, kind := range []string{"missing", "cycle", "mixed", "unknown-proxy"} {
		t.Run(kind, func(t *testing.T) {
			raw := routeFixture()
			raw["ssh_configs"][0]["jump_host"] = "hop"
			switch kind {
			case "missing":
				raw["ssh_configs"][0]["jump_host"] = "deleted"
			case "cycle":
				raw["ssh_configs"][1]["jump_host"] = "target"
			case "mixed":
				raw["ssh_configs"][0]["proxycommand"] = "proxy"
				raw["proxies"] = []map[string]interface{}{{"id": "proxy", "type": "socks5", "hostname": "localhost", "port": int64(1080)}}
			case "unknown-proxy":
				raw["ssh_configs"][1]["proxycommand"] = "unresolved"
			}
			data, err := buildData(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, h := range data.Hosts {
				if h.ID == "id:target" && !h.Blocked {
					t.Fatal("invalid route became connectable")
				}
			}
		})
	}
}
