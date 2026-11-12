package termius

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang/snappy"
	"golang.org/x/crypto/nacl/secretbox"
)

func TestExplicitIdentityReferencesPreserveSameUserHosts(t *testing.T) {
	raw := tables{
		"hosts":          {{"id": uint64(1), "address": "192.0.2.1", "label": "one", "ssh_config": map[string]interface{}{"local_id": uint64(11)}, "group": uint64(9)}, {"id": uint64(2), "address": "192.0.2.1", "label": "two", "ssh_config": uint64(12)}, {"id": uint64(3), "address": "192.0.2.3", "ssh_config": uint64(13)}},
		"ssh_configs":    {{"id": uint64(21), "local_id": uint64(11), "identity": uint64(31), "proxycommand": map[string]interface{}{"id": nil, "local_id": nil}}, {"id": uint64(12), "identity": uint64(32)}, {"id": uint64(13), "identity": uint64(99)}},
		"ssh_identities": {{"id": uint64(31), "username": "root", "password": "one pw "}, {"id": uint64(32), "username": "root", "password": "two pw"}},
		"groups":         {{"id": uint64(9), "label": "group"}},
	}
	data, e := buildData(raw)
	if e != nil {
		t.Fatal(e)
	}
	if len(data.Hosts) != 3 || data.Hosts[0].Password != "one pw " || data.Hosts[1].Password != "two pw" || data.Hosts[2].Password != "" || !data.Hosts[2].Blocked {
		t.Fatal("identity references were guessed or host records collapsed")
	}
	if data.Hosts[0].Blocked || len(data.Hosts[0].Tags) != 1 {
		t.Fatal("empty proxy reference blocked direct host")
	}
}
func TestLatestBusinessRevisionAndDeletion(t *testing.T) {
	data, e := buildData(tables{"hosts": {
		{"id": "1", "updated_at": "2026-01-02", "label": "new", "address": "192.0.2.1"},
		{"id": "1", "updated_at": "2026-01-01", "label": "old", "address": "192.0.2.2"},
		{"id": "2", "updated_at": "2026-01-01", "address": "192.0.2.3"},
		{"id": "2", "updated_at": "2026-01-02", "deleted": true},
	}})
	if e != nil || len(data.Hosts) != 1 || data.Hosts[0].Name != "new" {
		t.Fatal("old or deleted business revision was imported", e)
	}
}
func TestUnsupportedProxyIsNotImportedAsDirectConnection(t *testing.T) {
	data, e := buildData(tables{"hosts": {{"id": "1", "address": "192.0.2.1", "ssh_config": "2"}}, "ssh_configs": {{"id": "2", "identity": "3", "proxycommand": map[string]interface{}{"id": "4"}}}, "ssh_identities": {{"id": "3", "username": "root", "password": "pw"}}})
	if e != nil || !data.Hosts[0].Blocked {
		t.Fatal("proxy silently dropped", e)
	}
}
func sealTest(s string, key [32]byte) string {
	nonce := [24]byte{1, 2, 3}
	b := append([]byte{4, 1}, nonce[:]...)
	return base64.StdEncoding.EncodeToString(secretbox.Seal(b, []byte(s), &nonce, &key))
}
func TestDecryptionAuthenticatesAndPreservesEmptyAndWhitespace(t *testing.T) {
	key := [32]byte{7}
	for _, plain := range []string{"", " secret \n", "中文"} {
		cipher := sealTest(plain, key)
		v, e := decryptNode(cipher, key[:], 0)
		if e != nil || v != plain {
			t.Fatal("plaintext changed", e)
		}
		if ValidateKey(cipher, make([]byte, 32)) == nil {
			t.Fatal("wrong key accepted")
		}
	}
	b, _ := base64.StdEncoding.DecodeString(sealTest("secret", key))
	b[1] = 2
	if _, e := decryptNode(base64.StdEncoding.EncodeToString(b), key[:], 0); e == nil {
		t.Fatal("unknown envelope accepted")
	}
}
func TestV8NodeSerializedObject(t *testing.T) {
	// Generated with Node's v8.serialize, independent of this decoder.
	raw, e := hex.DecodeString("ff0f6f22026964490222056c6162656c63062d4e87652c672204726566736f22026964491622086c6f63616c5f696449187b0222047461677341022201612201622400022205656d7074792200220566616c7365467b06")
	if e != nil {
		t.Fatal(e)
	}
	v, e := decodeStoredValue(raw, 0)
	if e != nil {
		t.Fatal(e)
	}
	row := v.(map[string]interface{})
	if row["label"] != "中文本" || row["empty"] != "" || row["false"] != false {
		t.Fatalf("bad V8 values: %+v", row)
	}
	compressed := append([]byte{255, 17, 2}, snappy.Encode(nil, raw)...)
	if _, e := decodeStoredValue(compressed, 0); e != nil {
		t.Fatal(e)
	}
	outer := append([]byte{255, 21, 254}, make([]byte, 12)...)
	outer = append(outer, raw...)
	if _, e := decodeStoredValue(outer, 0); e != nil {
		t.Fatal(e)
	}
}
func varint(n uint64) []byte { return binary.AppendUvarint(nil, n) }
func logicalRecord(b []byte) []byte {
	h := make([]byte, 7)
	binary.LittleEndian.PutUint16(h[4:], uint16(len(b)))
	h[6] = 1
	binary.LittleEndian.PutUint32(h, checksum(append([]byte{1}, b...)))
	return append(h, b...)
}
func batch(seq uint64, kind byte, key, value []byte) []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint64(b, seq)
	binary.LittleEndian.PutUint32(b[8:], 1)
	b = append(b, kind)
	b = append(b, varint(uint64(len(key)))...)
	b = append(b, key...)
	if kind == 1 {
		b = append(b, varint(uint64(len(value)))...)
		b = append(b, value...)
	}
	return logicalRecord(b)
}
func TestCurrentDatabaseExcludesOldLogsAndTombstones(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("CURRENT", []byte("MANIFEST-000001\n"))
	write("MANIFEST-000001", logicalRecord([]byte{2, 3}))
	write("000002.log", batch(99, 1, []byte("old-log"), []byte("historical")))
	wal := batch(1, 1, []byte("deleted"), []byte("old"))
	wal = append(wal, batch(2, 0, []byte("deleted"), nil)...)
	wal = append(wal, batch(3, 1, []byte("current"), []byte("current"))...)
	write("000003.log", wal)
	view, e := readDatabase(dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := view["old-log"]; ok {
		t.Fatal("obsolete WAL was imported")
	}
	if !view["deleted"].deleted || string(view["current"].value) != "current" {
		t.Fatal("latest state not respected")
	}
	wal[len(wal)-1] ^= 1
	write("000003.log", wal)
	if _, e := readDatabase(dir); e == nil || !strings.Contains(e.Error(), "checksum") {
		t.Fatal("corruption accepted", e)
	}
}
func TestReadMissingProfileDoesNotRequestKey(t *testing.T) {
	_, e := Read(t.TempDir(), func() ([][]byte, error) {
		t.Fatal("keychain accessed without a profile")
		return nil, errors.New("unexpected")
	})
	if e == nil {
		t.Fatal("missing profile accepted")
	}
}

func TestChromiumGeneratedIndexedDB(t *testing.T) {
	data, e := Read("testdata/chromium", func() ([][]byte, error) { t.Fatal("plaintext fixture requested a key"); return nil, nil })
	if e != nil {
		t.Fatal(e)
	}
	if len(data.Hosts) != 1 {
		t.Fatalf("deleted host returned: %d", len(data.Hosts))
	}
	h := data.Hosts[0]
	if h.Name != "中文主机" || h.Address != "192.0.2.1" || h.Port != "2222" || h.Username != "root" || h.Password != "synthetic-password" || h.Blocked || len(h.Tags) != 1 {
		t.Fatalf("Chromium profile was not reconstructed: %+v", h)
	}
}

func TestCompactedLevelDBTable(t *testing.T) {
	view, e := readDatabase("testdata/leveldb")
	if e != nil {
		t.Fatal(e)
	}
	if string(view["current"].value) != "live-value" {
		t.Fatal("compacted table not read")
	}
	if r, ok := view["deleted"]; ok && !r.deleted {
		t.Fatal("compacted deletion resurrected")
	}
}

func TestAmbiguousRevisionDoesNotGuessCredentials(t *testing.T) {
	_, e := buildData(tables{"ssh_identities": {{"id": "identity", "username": "root", "password": "first"}, {"id": "identity", "username": "root", "password": "second"}}})
	if e == nil || !strings.Contains(e.Error(), "ambiguous") {
		t.Fatal("conflicting identities were silently guessed", e)
	}
	if compareRevision("10", "9") <= 0 {
		t.Fatal("numeric revision ordered lexically")
	}
}

func TestGroupPathRetainsParentOrderAndRepeatedLabels(t *testing.T) {
	data, err := buildData(tables{
		"hosts":          {{"id": "h", "address": "localhost", "group": "c", "ssh_config": "s"}},
		"ssh_configs":    {{"id": "s", "identity": "i"}},
		"ssh_identities": {{"id": "i", "username": "root"}},
		"groups":         {{"id": "c", "label": "重复", "parent_group": map[string]interface{}{"id": "p", "local_id": uint64(2)}}, {"id": "p", "label": "重复", "parent_group": "r"}, {"id": "r", "label": "公司/研发"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := data.Hosts[0].GroupPath
	if len(path) != 3 || path[0] != "公司/研发" || path[1] != "重复" || path[2] != "重复" {
		t.Fatalf("hierarchy lost: %v", path)
	}
}

func TestMissingParentGroupBlocksImport(t *testing.T) {
	data, err := buildData(tables{"hosts": {{"id": "h", "address": "localhost", "group": "child"}}, "groups": {{"id": "child", "label": "child", "parent_group": "missing"}}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range data.Hosts[0].Warnings {
		if warning == "invalid_group" {
			found = true
		}
	}
	if !data.Hosts[0].Blocked || !found {
		t.Fatal("missing parent silently flattened")
	}
}

func TestForwardLabelPreserved(t *testing.T) {
	data, err := buildData(tables{
		"hosts":    {{"id": "h", "address": "localhost"}},
		"pf_rules": {{"id": "f", "host": "h", "label": "写库 [广告2库] 9112", "pf_type": "Local Rule", "local_port": "9112", "hostname": "db.internal", "remote_port": "3306"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Hosts[0].Forwards) != 1 || data.Hosts[0].Forwards[0].Name != "写库 [广告2库] 9112" {
		t.Fatal("forward label lost")
	}
}
