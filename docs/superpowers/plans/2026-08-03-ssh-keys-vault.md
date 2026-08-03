# SSH 密钥库(Keys Vault)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use checkbox (`- [ ]`).

**Goal:** 引入独立 SSH 密钥库:Key 实体(name+私钥+passphrase)独立 CRUD,主机用 KeyID 引用(删内嵌私钥),Key 与主机清单同一 E2EE blob 同步,SSH 面板加 Hosts/Keys tab。

**Architecture:** 新 `desktop/ssh_keys_store.go`(Key CRUD + 引用保护 + 私钥校验);SSHHost 加 KeyID、sshCredential 删私钥;OpenSSHSession 按 KeyID 取私钥;sshSyncPayload 加 Keys;adapter 同步 hosts+keys;前端 SshHostsPanel 加 tab + Key 下拉。

**Tech Stack:** Go 1.23(`~/sdk/go1.23.12`),`golang.org/x/crypto/ssh`,`oklog/ulid/v2`,`internal/safekeyring`,Vue 3 + TS。

**环境备注:** Go 命令前 `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12"`;前端 `export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH"`。embed 产物已生成。

---

## File Structure

**新建:** `desktop/ssh_keys_store.go` + `_test.go`
**修改:** `desktop/config.go`(SSHKeys)、`desktop/ssh_hosts_store.go`(SSHHost.KeyID, sshCredential 删私钥)、`desktop/ssh_host.go`(key 认证)、`desktop/ssh_sync.go`(payload+Keys)、`desktop/prefssync_adapter.go`(keys 同步)、`desktop/app.go`(错误常量)、前端 `api.ts` + `SshHostsPanel.vue`

---

## Task 1: config 加 SSHKeys + SSHHost 加 KeyID + sshCredential 删私钥

**Files:** Modify `desktop/config.go`, `desktop/ssh_hosts_store.go`

- [ ] **Step 1: config.go 加 SSHKeys 字段**

在 `SSHHosts []SSHHost` 后加:
```go
	// SSHKeys is the saved SSH key vault (non-secret fields only).
	// Private keys live in the keyring keyed by SSHKey.ID, never here.
	SSHKeys []SSHKey `json:"ssh_keys,omitempty"`
```

- [ ] **Step 2: ssh_hosts_store.go — SSHHost 加 KeyID,sshCredential 删私钥字段**

SSHHost struct 加:
```go
	KeyID    string `json:"key_id,omitempty"` // when auth_kind=="key"
```
并把 AuthKind 注释改为 `// "password" | "key"`。

sshCredential 改为(删 PrivateKey/Passphrase):
```go
type sshCredential struct {
	Password string `json:"password,omitempty"`
}
```

- [ ] **Step 3: 编译(SSHKey 未定义,预期报错,Task 2 补)**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go build ./desktop/... 2>&1 | grep -v "no matching\|frontend/dist" | head`
Expected: 报 `undefined: SSHKey` + 可能报 sshCredential.PrivateKey 引用处(ssh_host.go / ssh_sync.go)。这些在后续 Task 修。不 commit,进 Task 2。

---

## Task 2: ssh_keys_store — Key CRUD + 私钥校验 + 引用保护(TDD)

**Files:** Create `desktop/ssh_keys_store.go`, `desktop/ssh_keys_store_test.go`

- [ ] **Step 1: 写失败测试**

`desktop/ssh_keys_store_test.go`:
```go
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
)

func testKeyPEM(t *testing.T) string {
	t.Helper()
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	der := x509.MarshalPKCS1PrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
}

func newKeysTestApp(t *testing.T) *App {
	t.Helper()
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	return &App{cfgStore: newTestConfigStore(t)}
}

func TestAddSSHKeyParsesTypeAndStores(t *testing.T) {
	a := newKeysTestApp(t)
	k, err := a.AddSSHKey("aws", testKeyPEM(t), "")
	if err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}
	if k.ID == "" || k.Name != "aws" || k.KeyType != "RSA" {
		t.Fatalf("unexpected key: %+v", k)
	}
	list := a.ListSSHKeys()
	if len(list) != 1 || list[0].ID != k.ID {
		t.Fatalf("list mismatch: %+v", list)
	}
	raw, err := safekeyring.Get(sshKeyService(), k.ID)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	var sec sshKeySecret
	_ = json.Unmarshal([]byte(raw), &sec)
	if !strings.Contains(sec.PrivateKey, "PRIVATE KEY") {
		t.Fatalf("private key not stored: %q", sec.PrivateKey)
	}
}

func TestAddSSHKeyRejectsInvalidPEM(t *testing.T) {
	a := newKeysTestApp(t)
	if _, err := a.AddSSHKey("bad", "not a key", ""); err == nil {
		t.Fatal("expected invalid key error")
	}
}

func TestDeleteSSHKeyInUseRejected(t *testing.T) {
	a := newKeysTestApp(t)
	k, _ := a.AddSSHKey("aws", testKeyPEM(t), "")
	// wire a host referencing it
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "h1", Host: "h", User: "u", AuthKind: "key", KeyID: k.ID, Alias: "box"}}
	_ = a.cfgStore.Set(cfg)

	err := a.DeleteSSHKey(k.ID)
	if err == nil || !strings.Contains(err.Error(), "box") {
		t.Fatalf("expected in-use error naming host, got %v", err)
	}
	if len(a.ListSSHKeys()) != 1 {
		t.Fatal("key should not be deleted while in use")
	}
}

func TestDeleteSSHKeyUnreferenced(t *testing.T) {
	a := newKeysTestApp(t)
	k, _ := a.AddSSHKey("aws", testKeyPEM(t), "")
	if err := a.DeleteSSHKey(k.ID); err != nil {
		t.Fatalf("DeleteSSHKey: %v", err)
	}
	if len(a.ListSSHKeys()) != 0 {
		t.Fatal("key not removed")
	}
	if _, err := safekeyring.Get(sshKeyService(), k.ID); err == nil {
		t.Fatal("secret should be gone")
	}
}

func TestUpdateSSHKeyKeepsPrivateKeyWhenBlank(t *testing.T) {
	a := newKeysTestApp(t)
	k, _ := a.AddSSHKey("aws", testKeyPEM(t), "")
	if err := a.UpdateSSHKey(k.ID, "aws-renamed", "", ""); err != nil {
		t.Fatalf("UpdateSSHKey: %v", err)
	}
	if a.ListSSHKeys()[0].Name != "aws-renamed" {
		t.Fatal("name not updated")
	}
	if _, err := safekeyring.Get(sshKeyService(), k.ID); err != nil {
		t.Fatal("private key should be kept")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'SSHKey' 2>&1 | head`
Expected: FAIL(undefined: AddSSHKey/ListSSHKeys/DeleteSSHKey/UpdateSSHKey/sshKeyService/sshKeySecret/SSHKey)。

- [ ] **Step 3: 实现 ssh_keys_store.go**

```go
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/safekeyring"
	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/ssh"
)

// SSHKey is one key in the vault. Non-secret fields live in config.json; the
// private key + passphrase live in the keyring keyed by ID.
type SSHKey struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	KeyType string `json:"key_type,omitempty"`
}

// sshKeySecret is JSON-encoded into a single keyring entry keyed by key ID.
type sshKeySecret struct {
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase,omitempty"`
}

func sshKeyService() string {
	return "com.atterm.ssh-key.v1" + appdir.KeychainSuffix()
}

// parseKeyType parses the PEM (optionally with passphrase) and returns a
// normalized algorithm name. An unparsable key is an error.
func parseKeyType(pemStr, passphrase string) (string, error) {
	var signer ssh.Signer
	var err error
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(pemStr), []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(pemStr))
	}
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	switch t := signer.PublicKey().Type(); {
	case t == "ssh-rsa":
		return "RSA", nil
	case t == "ssh-ed25519":
		return "ED25519", nil
	case strings.HasPrefix(t, "ecdsa-"):
		return "ECDSA", nil
	default:
		return t, nil
	}
}

func (a *App) ListSSHKeys() []SSHKey {
	if a.cfgStore == nil {
		return []SSHKey{}
	}
	ks := a.cfgStore.Get().SSHKeys
	if ks == nil {
		return []SSHKey{}
	}
	return ks
}

func (a *App) AddSSHKey(name, privateKeyPEM, passphrase string) (SSHKey, error) {
	if a.cfgStore == nil {
		return SSHKey{}, fmt.Errorf("config store not ready")
	}
	kt, err := parseKeyType(privateKeyPEM, passphrase)
	if err != nil {
		return SSHKey{}, err
	}
	k := SSHKey{ID: ulid.Make().String(), Name: name, KeyType: kt}
	if err := storeSSHKeySecret(k.ID, sshKeySecret{PrivateKey: privateKeyPEM, Passphrase: passphrase}); err != nil {
		return SSHKey{}, fmt.Errorf("store key: %w", err)
	}
	cfg := a.cfgStore.Get()
	cfg.SSHKeys = append(cfg.SSHKeys, k)
	if err := a.cfgStore.Set(cfg); err != nil {
		_ = safekeyring.Delete(sshKeyService(), k.ID)
		return SSHKey{}, err
	}
	a.markSSHHostsDirty()
	return k, nil
}

func (a *App) UpdateSSHKey(id, name, privateKeyPEM, passphrase string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	idx := -1
	for i := range cfg.SSHKeys {
		if cfg.SSHKeys[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no such key: %s", id)
	}
	cfg.SSHKeys[idx].Name = name
	// Only overwrite the private key when a new PEM is supplied.
	if privateKeyPEM != "" {
		kt, err := parseKeyType(privateKeyPEM, passphrase)
		if err != nil {
			return err
		}
		cfg.SSHKeys[idx].KeyType = kt
		if err := storeSSHKeySecret(id, sshKeySecret{PrivateKey: privateKeyPEM, Passphrase: passphrase}); err != nil {
			return fmt.Errorf("store key: %w", err)
		}
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markSSHHostsDirty()
	return nil
}

func (a *App) DeleteSSHKey(id string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	// Reference guard: refuse if any host uses this key.
	var users []string
	for _, h := range cfg.SSHHosts {
		if h.AuthKind == "key" && h.KeyID == id {
			name := h.Alias
			if name == "" {
				name = h.User + "@" + h.Host
			}
			users = append(users, name)
		}
	}
	if len(users) > 0 {
		return fmt.Errorf("key in use by: %s", strings.Join(users, ", "))
	}
	out := cfg.SSHKeys[:0:0]
	found := false
	for _, k := range cfg.SSHKeys {
		if k.ID == id {
			found = true
			continue
		}
		out = append(out, k)
	}
	if !found {
		return fmt.Errorf("no such key: %s", id)
	}
	cfg.SSHKeys = out
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	if err := safekeyring.Delete(sshKeyService(), id); err != nil && err != safekeyring.ErrNotFound {
		return fmt.Errorf("delete key secret: %w", err)
	}
	a.markSSHHostsDirty()
	return nil
}

func storeSSHKeySecret(id string, sec sshKeySecret) error {
	blob, err := json.Marshal(sec)
	if err != nil {
		return err
	}
	return safekeyring.Set(sshKeyService(), id, string(blob))
}
```

- [ ] **Step 4: 运行测试通过**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'SSHKey' -v 2>&1 | tail -15`
Expected: PASS(5 用例)。

- [ ] **Step 5: Commit**

```bash
gofmt -w desktop/ssh_keys_store.go desktop/ssh_keys_store_test.go desktop/config.go desktop/ssh_hosts_store.go
git add desktop/ssh_keys_store.go desktop/ssh_keys_store_test.go desktop/config.go desktop/ssh_hosts_store.go
git commit -m "feat(desktop): SSH 密钥库 CRUD + 私钥校验 + 引用保护

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: 连接路径按 KeyID 取私钥(TDD)

**Files:** Modify `desktop/ssh_host.go`, `desktop/app.go`, `desktop/app_ssh_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `desktop/app_ssh_test.go`:
```go
func TestNewSshSessionByIDKeyAuth(t *testing.T) {
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	addr, _ := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	k, err := a.AddSSHKey("k", testKeyPEM(t), "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "h1", Host: host, Port: port, User: "u", AuthKind: "key", KeyID: k.ID}}
	_ = a.cfgStore.Set(cfg)

	// Unknown host key on first connect proves the key was resolved and used.
	_, err = a.NewSshSessionByID("h1")
	var hk *HostKeyUnknownError
	if !errorsAs(err, &hk) {
		t.Fatalf("expected HostKeyUnknownError (key resolved), got %v", err)
	}
}

func TestNewSshSessionByIDKeyMissing(t *testing.T) {
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "h1", Host: "h", User: "u", AuthKind: "key", KeyID: "gone"}}
	_ = a.cfgStore.Set(cfg)
	_, err := a.NewSshSessionByID("h1")
	if err == nil || err.Error() != errKeyMissing {
		t.Fatalf("expected errKeyMissing, got %v", err)
	}
}
```
> `errorsAs` = 用 `errors.As`(文件已 import errors);若无则直接用 `errors.As`。`testKeyPEM` 复用 Task 2 helper(同包)。

- [ ] **Step 2: 运行确认失败**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'TestNewSshSessionByIDKey' 2>&1 | head`
Expected: FAIL(errKeyMissing 未定义 / key 认证未实现)。

- [ ] **Step 3: 实现**

`desktop/app.go` 加常量(errCredentialMissing 旁):
```go
// errKeyMissing is returned when a host references a key ID that no longer
// exists in the vault.
const errKeyMissing = "ssh_key_missing"
```

`desktop/ssh_host.go::NewSshSessionByID` 改造凭据解析段。找到现有从 keyring 取 password 的段落,替换为按 auth_kind 分流:
```go
	req := SSHConnectReq{
		Host: found.Host, Port: found.Port, User: found.User,
		AuthKind:      found.AuthKind,
		AcceptHostKey: false,
	}
	switch found.AuthKind {
	case "key":
		raw, err := safekeyring.Get(sshKeyService(), found.KeyID)
		if err != nil {
			return NewSessionResp{}, errors.New(errKeyMissing)
		}
		var sec sshKeySecret
		if err := json.Unmarshal([]byte(raw), &sec); err != nil {
			return NewSessionResp{}, errors.New(errKeyMissing)
		}
		req.PrivateKey = sec.PrivateKey
		req.Passphrase = sec.Passphrase
	default: // "password"
		raw, err := safekeyring.Get(sshCredentialService(), id)
		if err != nil {
			return NewSessionResp{}, errors.New(errCredentialMissing)
		}
		var cred sshCredential
		if err := json.Unmarshal([]byte(raw), &cred); err != nil {
			return NewSessionResp{}, errors.New(errCredentialMissing)
		}
		req.Password = cred.Password
	}
	return a.NewSshSession(req)
```
> 注意:`SSHConnectReq` 仍需 AuthKind 值让 OpenSSHSession 选 auth 分支。OpenSSHSession 里 `case "privateKey"` 要改为 `case "key"`(见 Step 3b)。

- [ ] **Step 3b: OpenSSHSession 的 auth 分支改 key**

`desktop/ssh_host.go::OpenSSHSession` 里:
```go
	switch req.AuthKind {
	case "key":
		auth = sshclient.PrivateKeyAuth{PEM: []byte(req.PrivateKey), Passphrase: req.Passphrase}
	default:
		auth = sshclient.PasswordAuth{Password: req.Password}
	}
```
(即席对话框 NewSshDialog 仍传 auth_kind;下方 Task 6 会把前端的 privateKey 改成 key 语义。为兼容即席"粘私钥",NewSshDialog 传 auth_kind="key" + PrivateKey 内容即可走此分支。)

- [ ] **Step 4: 运行测试通过**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'TestNewSshSessionByID' -v 2>&1 | tail`
Expected: PASS(含既有 + 新 key 用例)。

- [ ] **Step 5: Commit**

```bash
gofmt -w desktop/ssh_host.go desktop/app.go desktop/app_ssh_test.go
git add desktop/ssh_host.go desktop/app.go desktop/app_ssh_test.go
git commit -m "feat(desktop): 连接按 KeyID 取私钥(key 认证)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: 同步 payload 加 Keys(方案 A)(TDD)

**Files:** Modify `desktop/ssh_sync.go`, `desktop/prefssync_adapter.go`, `desktop/ssh_sync_test.go`, `desktop/ssh_hosts_store_test.go`

- [ ] **Step 1: 写失败测试(seal/open 往返带 keys)**

追加到 `desktop/ssh_sync_test.go`:
```go
func TestSealOpenWithKeys(t *testing.T) {
	key := testAccountKey(t)
	const canaryPK = "CANARY-PRIVATE-KEY-do-not-leak-0123456789"
	hosts := []SSHHost{{ID: "h1", Host: "h", User: "u", AuthKind: "key", KeyID: "k1"}}
	keys := []SSHKey{{ID: "k1", Name: "aws", KeyType: "RSA"}}
	keySecrets := map[string]sshKeySecret{"k1": {PrivateKey: canaryPK}}

	blob, err := sealSSHHosts(key, hosts, map[string]sshCredential{}, keys, keySecrets)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(string(blob), canaryPK) {
		t.Fatalf("private key leaked: %s", blob)
	}
	gotHosts, _, gotKeys, gotSecrets, err := openSSHHosts(key, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(gotHosts) != 1 || gotHosts[0].KeyID != "k1" {
		t.Fatalf("hosts: %+v", gotHosts)
	}
	if len(gotKeys) != 1 || gotKeys[0].Name != "aws" {
		t.Fatalf("keys: %+v", gotKeys)
	}
	if gotSecrets["k1"].PrivateKey != canaryPK {
		t.Fatalf("secret: %+v", gotSecrets)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'TestSealOpenWithKeys' 2>&1 | head`
Expected: FAIL(签名不符)。

- [ ] **Step 3: 改 ssh_sync.go — payload + seal/open 签名**

payload 加:
```go
type sshSyncPayload struct {
	Hosts []sshSyncHost `json:"hosts"`
	Keys  []sshSyncKey  `json:"keys"`
}
type sshSyncKey struct {
	Key        SSHKey `json:"key"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase,omitempty"`
}
```
`sealSSHHosts` 新签名:
```go
func sealSSHHosts(accountKey []byte, hosts []SSHHost, creds map[string]sshCredential, keys []SSHKey, keySecrets map[string]sshKeySecret) (json.RawMessage, error) {
	if len(accountKey) == 0 {
		return nil, nil
	}
	payload := sshSyncPayload{}
	for _, h := range hosts {
		payload.Hosts = append(payload.Hosts, sshSyncHost{Host: h, Cred: creds[h.ID]})
	}
	for _, k := range keys {
		sec := keySecrets[k.ID]
		payload.Keys = append(payload.Keys, sshSyncKey{Key: k, PrivateKey: sec.PrivateKey, Passphrase: sec.Passphrase})
	}
	// ... 其余(marshal/derive/seal/base64)不变
}
```
`openSSHHosts` 新签名:
```go
func openSSHHosts(accountKey []byte, value json.RawMessage) ([]SSHHost, map[string]sshCredential, []SSHKey, map[string]sshKeySecret, error) {
	// ... decode/open/unmarshal 不变,末尾:
	hosts := make([]SSHHost, 0, len(payload.Hosts))
	creds := make(map[string]sshCredential, len(payload.Hosts))
	for _, sh := range payload.Hosts {
		hosts = append(hosts, sh.Host)
		creds[sh.Host.ID] = sh.Cred
	}
	keys := make([]SSHKey, 0, len(payload.Keys))
	secrets := make(map[string]sshKeySecret, len(payload.Keys))
	for _, sk := range payload.Keys {
		keys = append(keys, sk.Key)
		secrets[sk.Key.ID] = sshKeySecret{PrivateKey: sk.PrivateKey, Passphrase: sk.Passphrase}
	}
	return hosts, creds, keys, secrets, nil
}
```

- [ ] **Step 4: 改 prefssync_adapter.go — ssh_hosts_encrypted 分支带 keys**

ReadValue 的 case 里,组 keySecrets 并传入:
```go
	case "ssh_hosts_encrypted":
		key := a.accountKey()
		if len(key) == 0 {
			return nil, false
		}
		creds := make(map[string]sshCredential, len(c.SSHHosts))
		for _, h := range c.SSHHosts {
			if raw, err := safekeyring.Get(sshCredentialService(), h.ID); err == nil {
				var cr sshCredential
				if json.Unmarshal([]byte(raw), &cr) == nil {
					creds[h.ID] = cr
				}
			}
		}
		keySecrets := make(map[string]sshKeySecret, len(c.SSHKeys))
		for _, k := range c.SSHKeys {
			if raw, err := safekeyring.Get(sshKeyService(), k.ID); err == nil {
				var sec sshKeySecret
				if json.Unmarshal([]byte(raw), &sec) == nil {
					keySecrets[k.ID] = sec
				}
			}
		}
		blob, err := sealSSHHosts(key, c.SSHHosts, creds, c.SSHKeys, keySecrets)
		if err != nil || blob == nil {
			return nil, false
		}
		return blob, true
```
WriteValue 的 case 里,open 五返回值并落地 keys:
```go
	case "ssh_hosts_encrypted":
		key := a.accountKey()
		if len(key) == 0 {
			return nil
		}
		hosts, creds, keys, keySecrets, err := openSSHHosts(key, value)
		if err != nil {
			return err
		}
		for id, cr := range creds {
			blob, mErr := json.Marshal(cr)
			if mErr != nil {
				return mErr
			}
			if sErr := safekeyring.Set(sshCredentialService(), id, string(blob)); sErr != nil {
				return sErr
			}
		}
		for id, sec := range keySecrets {
			blob, mErr := json.Marshal(sec)
			if mErr != nil {
				return mErr
			}
			if sErr := safekeyring.Set(sshKeyService(), id, string(blob)); sErr != nil {
				return sErr
			}
		}
		c.SSHHosts = hosts
		c.SSHKeys = keys
```

- [ ] **Step 5: 更新既有 adapter 测试(openSSHHosts 五返回值)**

`desktop/ssh_hosts_store_test.go::TestAdapterSSHHostsEncryptedRoundTrip` 若直接调 openSSHHosts 需改;它调的是 adapter.ReadValue/WriteValue(不直接调 open),应无需改。运行确认。

- [ ] **Step 6: 运行测试通过**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'TestSealOpen|TestAdapterSSHHosts|SSHKey' -v 2>&1 | tail -20`
Expected: 全 PASS。

- [ ] **Step 7: Commit**

```bash
gofmt -w desktop/ssh_sync.go desktop/prefssync_adapter.go desktop/ssh_sync_test.go
git add desktop/ssh_sync.go desktop/prefssync_adapter.go desktop/ssh_sync_test.go
git commit -m "feat(desktop): Keys 与主机清单同一 E2EE blob 同步(方案A)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: 全量 Go 测试 + 修复残留编译错误

- [ ] **Step 1: 全量编译 + 测试**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./internal/... ./desktop/... -count=1 2>&1 | grep -vE "feishu (service|mode|long)" | tail -12`
Expected: 全 ok。若有 sshCredential.PrivateKey/Passphrase 的残留引用(ssh_host.go OpenSSHSession 的即席路径),它们现在从 req 字段取(SSHConnectReq 仍有 PrivateKey/Passphrase),不受影响;若报错按提示修。

- [ ] **Step 2: go vet**

Run: `go vet ./desktop/... 2>&1 | grep -v "no matching\|frontend/dist"`
Expected: 无输出。

- [ ] **Step 3: Commit(若有修复)**

```bash
git add -A && git commit -m "fix(desktop): Keys 改造后残留编译修复" || echo "无需修复"
```

---

## Task 6: 前端 api.ts + 类型改造

**Files:** Modify `desktop/frontend/src/lib/api.ts`

- [ ] **Step 1: 改 SSHHost/SSHCredential 类型 + 加 SSHKey**

SSHHost 的 auth_kind 改 `"password" | "key"`,加 `key_id?`:
```ts
export interface SSHHost {
  id: string; alias?: string; host: string; port?: string; user: string;
  auth_kind: "password" | "key"; key_id?: string;
  group?: string; note?: string;
}
```
SSHConnectReq 保留 private_key/passphrase(即席对话框用),auth_kind 改 `"password" | "key"`。
加:
```ts
export interface SSHKey { id: string; name: string; key_type?: string; }
```

- [ ] **Step 2: 加 Key CRUD 绑定**

AppBindings 接口加:
```ts
  ListSSHKeys(): Promise<SSHKey[]>;
  AddSSHKey(name: string, privateKeyPEM: string, passphrase: string): Promise<SSHKey>;
  UpdateSSHKey(id: string, name: string, privateKeyPEM: string, passphrase: string): Promise<void>;
  DeleteSSHKey(id: string): Promise<void>;
```
导出包装:
```ts
export function listSSHKeys(): Promise<SSHKey[]> { return bindings().ListSSHKeys(); }
export function addSSHKey(name: string, pem: string, passphrase: string): Promise<SSHKey> { return bindings().AddSSHKey(name, pem, passphrase); }
export function updateSSHKey(id: string, name: string, pem: string, passphrase: string): Promise<void> { return bindings().UpdateSSHKey(id, name, pem, passphrase); }
export function deleteSSHKey(id: string): Promise<void> { return bindings().DeleteSSHKey(id); }
```

- [ ] **Step 3: 类型检查(仅看 api.ts 无新错误)**

Run: `cd desktop/frontend && export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH" && npx vue-tsc --noEmit 2>&1 | grep "lib/api.ts" || echo "api.ts OK"`
> 注意:改 auth_kind 为 "key" 会让 SshHostsPanel.vue 现有 "privateKey" 用法类型报错——Task 7 修组件。此步只确认 api.ts 自身。

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/lib/api.ts
git commit -m "feat(frontend): api.ts Key CRUD 绑定 + SSHHost key_id/auth_kind 改造"
```

---

## Task 7: 前端 SshHostsPanel — Hosts/Keys tab + Key 下拉(TDD)

**Files:** Modify `desktop/frontend/src/components/SshHostsPanel.vue`, `SshHostsPanel.test.ts`

- [ ] **Step 1: 更新/新增测试**

在 `SshHostsPanel.test.ts`:
1. mock 加 `listSSHKeys/addSSHKey/updateSSHKey/deleteSSHKey`(默认 listSSHKeys→[])。
2. 现有"添加主机"测试:主机表单认证默认 password 不变,断言不变。
3. 新增 Keys tab 测试:
```ts
it("切到 Keys tab 显示密钥并可添加", async () => {
  listSSHKeys.mockResolvedValue([{ id: "k1", name: "aws", key_type: "RSA" }]);
  addSSHKey.mockResolvedValueOnce({ id: "k2", name: "gcp", key_type: "RSA" });
  const w = mount(SshHostsPanel);
  await flushPromises();
  await w.find('[data-test="ssh-tab-keys"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("aws");
  await w.find('[data-test="ssh-key-new"]').trigger("click");
  await w.vm.$nextTick();
  await w.find('[data-test="ssh-key-name"]').setValue("gcp");
  await w.find('[data-test="ssh-key-pem"]').setValue("-----BEGIN...-----");
  await w.find('[data-test="ssh-key-submit"]').trigger("click");
  await flushPromises();
  expect(addSSHKey).toHaveBeenCalledWith("gcp", "-----BEGIN...-----", "");
});
```
4. 新增主机表单选 Key 测试:切认证到 Key,下拉出现 listSSHKeys 的项。

- [ ] **Step 2: 运行确认失败**

Run: `cd desktop/frontend && export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH" && npx vitest run src/components/SshHostsPanel.test.ts 2>&1 | tail`
Expected: FAIL(tab/key 元素不存在)。

- [ ] **Step 3: 改 SshHostsPanel.vue**

3a. 顶部 tab:头部 brand 区改为两个 tab 按钮(`ssh-tab-hosts` / `ssh-tab-keys`),`activeTab` ref 控制 body 显示 Hosts 网格 or Keys 网格。计数徽章跟随当前 tab。

3b. Keys 网格 + 滑出表单:复用主机卡片视觉;Key 卡片显示 name + key_type;编辑/删除按钮;删除报错(被引用)显示在 error 区。New Key/Edit 抽屉字段:name(`ssh-key-name`)、私钥 PEM textarea(`ssh-key-pem`)、passphrase(`ssh-key-passphrase`);提交 `ssh-key-submit` 调 addSSHKey/updateSSHKey;新建入口 `ssh-key-new`。onMounted 同时 reload hosts + keys。

3c. 主机表单认证改造:分段 `Password | Key`(替换原 Password|Private key);Key 时渲染下拉 `<select data-test="ssh-add-keyid">`,options=keys(空库显示提示文案);save 时 auth_kind 用 "password"|"key",key 时带 key_id、不带私钥字段。fAuthKind 类型改 `"password" | "key"`,删 fPrivateKey/fPassphrase 相关(私钥不在主机表单)。

3d. 连接/子标题:subtitle 里 `h.auth_kind === "key" ? "key" : "password"`。

- [ ] **Step 4: 运行测试通过**

Run: `cd desktop/frontend && export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH" && npx vitest run src/components/SshHostsPanel.test.ts 2>&1 | tail`
Expected: PASS。

- [ ] **Step 5: 全前端 SSH 测试 + 类型检查 + 构建**

Run: `cd desktop/frontend && export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH" && npx vitest run src/components/SshHostsPanel.test.ts src/components/NewSshDialog.test.ts src/components/TabBar.test.ts && npm run build 2>&1 | tail -3`
Expected: 测试全 PASS,build ✓。
> NewSshDialog 若因 auth_kind "privateKey"→"key" 类型报错,把它的私钥分支 auth_kind 值改为 "key"(即席粘私钥仍走 key 分支)。

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/SshHostsPanel.vue desktop/frontend/src/components/SshHostsPanel.test.ts desktop/frontend/src/components/NewSshDialog.vue desktop/frontend/src/components/NewSshDialog.test.ts
git commit -m "feat(frontend): SSH 面板 Hosts/Keys tab + 密钥库 UI + 主机 Key 下拉

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: 收尾 — wails 绑定 + AGENTS.md

- [ ] **Step 1: 重新生成 wails 绑定 + 完整构建**

Run: `cd desktop/frontend && export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH" && npm run build 2>&1 | tail -3`
(build 会经 vue-tsc;wailsjs 绑定由 wails 生成——若无 wails CLI,手动在 App.d.ts/App.js 补 Key 方法签名,与现有 SSH binding 风格一致。)

- [ ] **Step 2: 更新 AGENTS.md**

在 SSH 切片记录后追加:SSH 密钥库——`desktop/ssh_keys_store.go`(Key 实体 CRUD,私钥入 `com.atterm.ssh-key.v1` keyring,校验+引用保护);主机 auth_kind 改 password|key 用 key_id 引用;Key 与主机同一 `ssh_hosts_encrypted` blob 同步;前端 SSH 面板 Hosts/Keys tab。spec `docs/superpowers/specs/2026-08-03-ssh-keys-vault-design-draft.md`。

- [ ] **Step 3: 手动冒烟(需 GUI)**

wails dev → Keys tab 添加密钥 → Hosts 新建主机选该 Key → 连接;删除被引用的 Key 应被拒绝并提示主机名。

- [ ] **Step 4: 最终 commit**

```bash
git add -A && git commit -m "chore: Keys 密钥库 wails 绑定 + AGENTS.md 记录"
```

---

## Self-Review

**Spec coverage:**
- Key 实体 CRUD + 私钥校验 + 引用保护 → Task 2 ✅
- 主机 auth_kind password|key + KeyID 引用 → Task 1/3 ✅
- 内嵌私钥删除 → Task 1(sshCredential 删字段) ✅
- 连接按 KeyID 取私钥 + errKeyMissing → Task 3 ✅
- Key 与主机同一 blob E2EE 同步 → Task 4 ✅
- 前端 Hosts/Keys tab + Key 下拉 → Task 7 ✅
- api 绑定 → Task 6 ✅

**Placeholder scan:** 无 TBD。Task 3b/5 的兼容说明是实现指引,非占位。

**Type consistency:** `SSHKey`/`sshKeySecret`/`sshKeyService`(Task 2 定义,3/4 用)、`sealSSHHosts` 五参新签名(Task 4 定义,adapter 用)、`openSSHHosts` 五返回值(Task 4,adapter 用)、`errKeyMissing`(Task 3)、`AddSSHKey/ListSSHKeys/UpdateSSHKey/DeleteSSHKey`(Task 2 定义,前端 Task 6/7 用)、auth_kind "key"(全栈一致)、key_id(Task 1 Go / Task 6 TS)—— 一致。
