// Package termius reads a local Termius profile without modifying it. It owns
// no application storage or UI, so migration can also run in a standalone tool.
package termius

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/secretbox"
)

// Key keeps secret material out of generic JSON previews and diagnostics.
type Key struct {
	ID, Name   string
	PrivateKey string `json:"-"`
	Passphrase string `json:"-"`
}

// Forward is a saved rule; importing it must never start a listener.
type Forward struct{ Name, Kind, BindAddr, BindPort, TargetHost, TargetPort string }

// Proxy retains a native network route while excluding its password from JSON.
type Proxy struct {
	Kind, Host, Port, Username string
	Password                   string `json:"-"`
}

// Host preserves source identity rather than collapsing hosts by address.
type Host struct {
	ID, Name, Address, Port, Username, KeyID string
	Password                                 string `json:"-"`
	Tags                                     []string
	GroupPath                                []string
	Forwards                                 []Forward
	Warnings                                 []string
	Blocked                                  bool
	JumpIDs                                  []string
	Proxy                                    *Proxy
}

// Data contains only current host definitions and their explicitly linked credentials.
type Data struct {
	Hosts []Host
	Keys  []Key
}

// Locate accepts either a profile directory or its IndexedDB subdirectory.
func Locate(path string) (string, error) {
	for _, p := range []string{path, filepath.Join(path, "IndexedDB", "file__0.indexeddb.leveldb"), filepath.Join(path, "file__0.indexeddb.leveldb")} {
		if s, e := os.Stat(filepath.Join(p, "CURRENT")); e == nil && s.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", errors.New("Termius database not found; select its local data directory")
}

// Discover returns every existing profile so side-by-side installations are not silently mixed.
func Discover(home, config, roaming string) []string {
	candidates := []string{filepath.Join(config, "Termius"), filepath.Join(home, "snap", "termius-app", "current", ".config", "Termius"), filepath.Join(home, "Library", "Application Support", "Termius")}
	if roaming != "" {
		candidates = append(candidates, filepath.Join(roaming, "Termius"))
	}
	sandbox, _ := filepath.Glob(filepath.Join(home, "Library", "Containers", "*ermius*", "Data", "Library", "Application Support", "Termius"))
	candidates = append(candidates, sandbox...)
	result := []string{}
	seen := map[string]bool{}
	for _, p := range candidates {
		if dir, e := Locate(p); e == nil && !seen[dir] {
			seen[dir] = true
			result = append(result, dir)
		}
	}
	return result
}

// DecodeKey validates a user-supplied base64 localKey without logging its value.
func DecodeKey(value string) ([]byte, error) {
	key, e := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if e != nil || len(key) != 32 {
		return nil, errors.New("Termius localKey must be base64 encoding of 32 bytes")
	}
	return key, nil
}
func envelope(s string) ([]byte, bool) {
	if len(s) < 56 || len(s) > maxValue*2 {
		return nil, false
	}
	b, e := base64.StdEncoding.Strict().DecodeString(s)
	return b, e == nil && len(b) >= 42 && b[0] == 4 && b[1] == 1
}
func firstEnvelope(node interface{}, depth int) []byte {
	if depth > 64 {
		return nil
	}
	switch v := node.(type) {
	case string:
		if b, ok := envelope(v); ok {
			return b
		}
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if b := firstEnvelope(v[k], depth+1); b != nil {
				return b
			}
		}
	case []interface{}:
		for _, x := range v {
			if b := firstEnvelope(x, depth+1); b != nil {
				return b
			}
		}
	}
	return nil
}
func openEnvelope(b []byte, key []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("invalid Termius localKey length")
	}
	var k [32]byte
	copy(k[:], key)
	var nonce [24]byte
	copy(nonce[:], b[2:26])
	plain, ok := secretbox.Open(nil, b[26:], &nonce, &k)
	if !ok {
		return "", errors.New("Termius decryption failed; the localKey does not match this profile or data is damaged")
	}
	return string(plain), nil
}
func decryptNode(node interface{}, key []byte, depth int) (interface{}, error) {
	if depth > 64 {
		return nil, errors.New("Termius object exceeds nesting limit")
	}
	switch v := node.(type) {
	case string:
		if b, ok := envelope(v); ok {
			return openEnvelope(b, key)
		}
		// An encrypted-looking value with an unrecognised version must not become
		// a literal password. Plaintext fields that happen to be base64 are valid.
		if len(v) >= 56 {
			if b, e := base64.StdEncoding.Strict().DecodeString(v); e == nil && len(b) >= 42 && (b[0] == 4 || (b[0] > 0 && b[0] <= 16 && b[1] == 1)) {
				return nil, errors.New("unsupported Termius encryption header")
			}
		}
		return v, nil
	case map[string]interface{}:
		out := map[string]interface{}{}
		for k, x := range v {
			y, e := decryptNode(x, key, depth+1)
			if e != nil {
				return nil, e
			}
			out[k] = y
		}
		return out, nil
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, x := range v {
			y, e := decryptNode(x, key, depth+1)
			if e != nil {
				return nil, e
			}
			out[i] = y
		}
		return out, nil
	default:
		return node, nil
	}
}

// Read asks for candidate keys through the caller's platform adapter. Candidate
// keys are verified against authenticated ciphertext, not chosen by service name.
func Read(dir string, candidates func() ([][]byte, error)) (*Data, error) {
	dir, e := Locate(dir)
	if e != nil {
		return nil, e
	}
	view, e := readDatabase(dir)
	if e != nil {
		return nil, e
	}
	raw, e := readTables(view)
	if e != nil {
		return nil, e
	}
	sample := firstEnvelope(mapTables(raw), 0)
	var key []byte
	if sample != nil {
		keys, e := candidates()
		if e != nil {
			return nil, e
		}
		defer func() {
			for _, k := range keys {
				clear(k)
			}
		}()
		for _, k := range keys {
			if _, e := openEnvelope(sample, k); e == nil {
				key = k
				break
			}
		}
		if key == nil {
			return nil, errors.New("no Termius localKey could decrypt this profile; unlock the system keychain or enter its localKey manually")
		}
	}
	decoded := tables{}
	for name, rows := range raw {
		for _, row := range rows {
			node, e := decryptNode(row, key, 0)
			if e != nil {
				return nil, e
			}
			obj := node.(map[string]interface{})
			if encrypted, ok := obj["__encrypted_record"]; ok {
				s, ok := encrypted.(string)
				if !ok {
					return nil, errors.New("invalid encrypted record")
				}
				var parsed map[string]interface{}
				if json.Unmarshal([]byte(s), &parsed) != nil {
					return nil, errors.New("unsupported encrypted Termius record")
				}
				obj = parsed
			}
			decoded[name] = append(decoded[name], obj)
		}
	}
	return buildData(decoded)
}
func mapTables(t tables) map[string]interface{} {
	out := map[string]interface{}{}
	for name, rows := range t {
		items := make([]interface{}, len(rows))
		for i, r := range rows {
			items[i] = r
		}
		out[name] = items
	}
	return out
}
func text(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	default:
		return ""
	}
}
func field(r map[string]interface{}, names ...string) string {
	for _, n := range names {
		if s := text(r[n]); s != "" {
			return s
		}
	}
	return ""
}
func entity(r map[string]interface{}) string {
	if s := text(r["id"]); s != "" {
		return "id:" + s
	}
	if s := text(r["local_id"]); s != "" {
		return "local:" + s
	}
	return ""
}

type rowIndex struct {
	ids, local map[string]map[string]interface{}
}

func makeIndex(rows []map[string]interface{}) (rowIndex, error) {
	idx := rowIndex{map[string]map[string]interface{}{}, map[string]map[string]interface{}{}}
	for _, r := range rows {
		if deleted(r) {
			continue
		}
		for i, key := range []string{"id", "local_id"} {
			s := text(r[key])
			if s == "" {
				continue
			}
			m := idx.ids
			if i == 1 {
				m = idx.local
			}
			if old := m[s]; old != nil && entity(old) != entity(r) {
				return idx, errors.New("ambiguous Termius record IDs")
			}
			if old := m[s]; old == nil || field(r, "updated_at") > field(old, "updated_at") {
				m[s] = r
			}
		}
	}
	return idx, nil
}
func (idx rowIndex) ref(v interface{}) map[string]interface{} {
	if r, ok := v.(map[string]interface{}); ok {
		if s := text(r["id"]); s != "" {
			if found := idx.ids[s]; found != nil {
				return found
			}
		}
		return idx.local[text(r["local_id"])]
	}
	s := text(v)
	if r := idx.ids[s]; r != nil {
		return r
	}
	return idx.local[s]
}
func deleted(r map[string]interface{}) bool {
	for _, k := range []string{"deleted", "is_deleted"} {
		if b, ok := r[k].(bool); ok && b {
			return true
		}
	}
	value := field(r, "deleted_at")
	return value != "" && value != "0"
}
func validPort(s string) bool { n, e := strconv.Atoi(s); return e == nil && n > 0 && n <= 65535 }
func compareRevision(a, b string) int {
	if x, e := strconv.ParseFloat(a, 64); e == nil {
		if y, e := strconv.ParseFloat(b, 64); e == nil {
			if x < y {
				return -1
			}
			if x > y {
				return 1
			}
			return 0
		}
	}
	if x, e := time.Parse(time.RFC3339Nano, a); e == nil {
		if y, e := time.Parse(time.RFC3339Nano, b); e == nil {
			if x.Before(y) {
				return -1
			}
			if x.After(y) {
				return 1
			}
			return 0
		}
	}
	return strings.Compare(a, b)
}

func buildData(t tables) (*Data, error) {
	// Different IndexedDB keys can carry revisions of the same business entity.
	// Select the latest revision before resolving IDs or applying deletions.
	for name, rows := range t {
		latest := map[string]map[string]interface{}{}
		var keyless []map[string]interface{}
		for _, row := range rows {
			id := entity(row)
			if id == "" {
				keyless = append(keyless, row)
				continue
			}
			old := latest[id]
			if old == nil {
				latest[id] = row
				continue
			}
			comparison := compareRevision(field(row, "updated_at"), field(old, "updated_at"))
			if comparison > 0 {
				latest[id] = row
			}
			if comparison == 0 && !reflect.DeepEqual(old, row) {
				return nil, errors.New("ambiguous Termius record revisions")
			}
		}
		normalized := keyless
		ids := make([]string, 0, len(latest))
		for id := range latest {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			normalized = append(normalized, latest[id])
		}
		t[name] = normalized
	}

	indexes := map[string]rowIndex{}
	for name, rows := range t {
		idx, e := makeIndex(rows)
		if e != nil {
			return nil, e
		}
		indexes[name] = idx
	}
	data := &Data{Hosts: []Host{}, Keys: []Key{}}
	keys := map[string]bool{}
	for _, row := range t["keys"] {
		if deleted(row) {
			continue
		}
		id := entity(row)
		if id == "" || keys[id] {
			continue
		}
		private := field(row, "private_key")
		if private == "" {
			continue
		}
		keys[id] = true
		data.Keys = append(data.Keys, Key{ID: id, Name: field(row, "label", "title"), PrivateKey: private, Passphrase: field(row, "passphrase")})
	}
	seen := map[string]bool{}
	for _, row := range t["hosts"] {
		if deleted(row) {
			continue
		}
		id := entity(row)
		if id == "" {
			return nil, errors.New("Termius host is missing a source ID")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		cfg := indexes["ssh_configs"].ref(row["ssh_config"])
		identity := indexes["ssh_identities"].ref(cfg["identity"])
		if identity == nil {
			identity = indexes["ssh_identities"].ref(row["identity"])
		}
		h := Host{ID: id, Name: field(row, "label", "title"), Address: field(row, "address", "host", "hostname"), Port: field(cfg, "port"), Username: field(identity, "username"), Password: field(identity, "password"), Tags: []string{}, Warnings: []string{}, Forwards: []Forward{}}
		if h.Port == "" {
			h.Port = "22"
		}
		if n, e := strconv.ParseUint(h.Address, 10, 32); e == nil {
			h.Address = net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n)).String()
		}
		if h.Name == "" {
			h.Name = h.Address
		}
		if h.Address == "" || strings.ContainsAny(h.Address, "\x00\r\n\t ") || !validPort(h.Port) {
			h.Blocked = true
			h.Warnings = append(h.Warnings, "invalid_address")
		}
		if k := indexes["keys"].ref(identity["ssh_key"]); k != nil && keys[entity(k)] {
			h.KeyID = entity(k)
		} else if referencePresent(identity["ssh_key"]) {
			h.Blocked = true
			h.Warnings = append(h.Warnings, "missing_key")
		}
		if h.Username == "" {
			h.Blocked = true
			h.Warnings = append(h.Warnings, "missing_username")
		}
		if h.Password == "" && h.KeyID == "" {
			h.Warnings = append(h.Warnings, "missing_credentials")
		}
		resolveRoute(&h, row, cfg, indexes, t["host_chains"])
		if field(cfg, "startup_snippet") != "" || referencePresent(cfg["startup_snippet"]) {
			h.Warnings = append(h.Warnings, "startup_skipped")
		}
		visited := map[string]bool{}
		group := indexes["groups"].ref(row["group"])
		for group != nil {
			gid := entity(group)
			if gid == "" || visited[gid] {
				h.Blocked = true
				h.Warnings = append(h.Warnings, "invalid_group")
				break
			}
			visited[gid] = true
			if label := field(group, "label"); label != "" {
				h.Tags = append(h.Tags, label)
				h.GroupPath = append([]string{label}, h.GroupPath...)
			}
			if cfg == nil && referencePresent(group["ssh_config"]) {
				h.Blocked = true
				h.Warnings = append(h.Warnings, "inherited_config")
			}
			parent := group["parent_group"]
			if !referencePresent(parent) {
				parent = group["parent"]
			}
			if !referencePresent(parent) {
				parent = group["group"]
			}
			group = indexes["groups"].ref(parent)
			if referencePresent(parent) && group == nil {
				h.Blocked = true
				h.Warnings = append(h.Warnings, "invalid_group")
			}
		}
		for _, link := range t["tag_hosts"] {
			if deleted(link) {
				continue
			}
			if entity(indexes["hosts"].ref(link["host"])) == id {
				tag := indexes["tags"].ref(link["tag"])
				if label := field(tag, "label"); label != "" {
					h.Tags = append(h.Tags, label)
				}
			}
		}
		sort.Strings(h.Tags)
		h.Tags = uniqueStrings(h.Tags)
		for _, rule := range t["pf_rules"] {
			if deleted(rule) || entity(indexes["hosts"].ref(rule["host"])) != id {
				continue
			}
			f := Forward{Name: field(rule, "label"), Kind: "local", BindAddr: field(rule, "bound_address"), BindPort: field(rule, "local_port"), TargetHost: field(rule, "hostname"), TargetPort: field(rule, "remote_port")}
			if f.BindAddr == "" {
				f.BindAddr = "127.0.0.1"
			}
			switch strings.ToLower(field(rule, "pf_type")) {
			case "local rule":
			case "remote rule":
				f.Kind = "remote"
			case "dynamic rule":
				f.Kind = "dynamic"
			default:
				h.Warnings = append(h.Warnings, "forward_skipped")
				continue
			}
			if !validPort(f.BindPort) || (f.Kind != "dynamic" && (!validPort(f.TargetPort) || f.TargetHost == "")) {
				h.Warnings = append(h.Warnings, "forward_skipped")
				continue
			}
			h.Forwards = append(h.Forwards, f)
		}
		data.Hosts = append(data.Hosts, h)
	}
	validateRoutes(data)
	sort.Slice(data.Hosts, func(i, j int) bool { return data.Hosts[i].ID < data.Hosts[j].ID })
	sort.Slice(data.Keys, func(i, j int) bool { return data.Keys[i].ID < data.Keys[j].ID })
	if len(data.Hosts) == 0 && len(data.Keys) == 0 {
		return nil, errors.New("no current Termius hosts or private keys found")
	}
	return data, nil
}
func referencePresent(v interface{}) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case string:
		return x != ""
	case bool:
		return x
	case float64:
		return x != 0
	case int64:
		return x != 0
	case uint64:
		return x != 0
	case map[string]interface{}:
		for _, value := range x {
			if referencePresent(value) {
				return true
			}
		}
		return false
	case []interface{}:
		return len(x) > 0
	}
	return true
}
func uniqueStrings(s []string) []string {
	out := s[:0]
	for _, v := range s {
		if len(out) == 0 || v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// ValidateKey proves that a candidate belongs to a profile before UI import.
func ValidateKey(ciphertext string, key []byte) error {
	b, ok := envelope(ciphertext)
	if !ok {
		return fmt.Errorf("not a supported Termius envelope")
	}
	_, e := openEnvelope(b, key)
	return e
}
