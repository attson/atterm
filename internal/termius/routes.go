package termius

import "strings"

func blockRoute(h *Host, reason string) { h.Blocked = true; h.Warnings = append(h.Warnings, reason) }

func resolveRoute(h *Host, row, cfg map[string]interface{}, indexes map[string]rowIndex, chains []map[string]interface{}) {
	if referencePresent(cfg["proxycommand"]) {
		p := indexes["proxies"].ref(cfg["proxycommand"])
		kind := strings.ToLower(field(p, "type"))
		switch kind {
		case "socks5", "socks 5", "socks5 proxy":
			kind = "socks5"
		case "http", "http proxy":
			kind = "http"
		default:
			kind = ""
		}
		identity := indexes["ssh_identities"].ref(p["identity"])
		if p == nil || deleted(p) || kind == "" || field(p, "hostname") == "" || strings.ContainsAny(field(p, "hostname"), "\x00\r\n\t ") || !validPort(field(p, "port")) || (referencePresent(p["identity"]) && (identity == nil || deleted(identity))) {
			blockRoute(h, "unsupported_proxy")
		} else {
			h.Proxy = &Proxy{Kind: kind, Host: field(p, "hostname"), Port: field(p, "port"), Username: field(identity, "username"), Password: field(identity, "password")}
		}
	}
	var route interface{}
	for _, value := range []interface{}{cfg["jump_host"], cfg["host_chain"], row["host_chain"]} {
		if !referencePresent(value) {
			continue
		}
		if route != nil {
			blockRoute(h, "invalid_jump")
			return
		}
		route = value
	}
	for _, chain := range chains {
		if deleted(chain) || cfg == nil || entity(indexes["ssh_configs"].ref(chain["ssh_config"])) != entity(cfg) {
			continue
		}
		if route != nil {
			blockRoute(h, "invalid_jump")
			return
		}
		route = chain["hosts_chain"]
	}
	if route == nil {
		return
	}
	var refs []interface{}
	if values, ok := route.([]interface{}); ok {
		refs = values
	} else if chain := indexes["host_chains"].ref(route); chain != nil {
		if deleted(chain) {
			blockRoute(h, "invalid_jump")
			return
		}
		refs, _ = chain["hosts_chain"].([]interface{})
	} else {
		refs = []interface{}{route}
	}
	if len(refs) == 0 {
		blockRoute(h, "invalid_jump")
		return
	}
	for _, ref := range refs {
		hop := indexes["hosts"].ref(ref)
		if hop == nil || deleted(hop) {
			blockRoute(h, "invalid_jump")
			return
		}
		h.JumpIDs = append(h.JumpIDs, entity(hop))
	}
	// Each proxy is local to its owning host; no silently reordered mixed route.
	if h.Proxy != nil {
		blockRoute(h, "invalid_jump")
	}
}

func validateRoutes(data *Data) {
	hosts := map[string]*Host{}
	for i := range data.Hosts {
		hosts[data.Hosts[i].ID] = &data.Hosts[i]
	}
	for i := range data.Hosts {
		root := &data.Hosts[i]
		count := 0
		expanded := 0
		var walk func(*Host, map[string]bool) bool
		walk = func(h *Host, path map[string]bool) bool {
			if h == nil || h.Blocked || path[h.ID] {
				return false
			}
			path[h.ID] = true
			defer delete(path, h.ID)
			for _, id := range h.JumpIDs {
				count++
				if count > 10 || !walk(hosts[id], path) {
					return false
				}
			}
			if h.Proxy != nil && expanded > 0 {
				return false
			}
			expanded++
			return true
		}
		if !root.Blocked && !walk(root, map[string]bool{}) {
			blockRoute(root, "invalid_jump")
		}
	}
}
