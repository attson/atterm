# SSH 主机清单持久化 + 凭据加密(切片 2)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** 让 atterm 能保存 SSH 主机清单(非敏感字段入 config.json,敏感凭据入系统钥匙串),从清单一键连接,并管理 known_hosts 条目。

**Architecture:** `SSHHost`(非敏感)存 `appConfig.SSHHosts`;凭据 JSON blob 存 `safekeyring`(account=主机 ULID)。`NewSshSessionByID` 按 ID 重组后复用切片 1 的 `OpenSSHSession`。切片 1 的即席连接路径保留。

**Tech Stack:** Go 1.23(用 `~/sdk/go1.23.12`),`github.com/oklog/ulid/v2`(已在依赖),`internal/safekeyring`,Wails binding,Vue 3 + TS。

**环境备注:** 本机默认 `go` 是 1.19;跑 Go 命令前 `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12"`。前端用 `export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH"`。desktop 测试依赖两个 embed 产物(切片 1 已生成:`desktop/hookinstall/atterm-hook` 与 `desktop/frontend/dist/index.html` 占位),若缺则先 `bash scripts/build-hook-binary.sh` 并放占位 dist。

---

## File Structure

**新建:**
- `desktop/ssh_hosts_store.go` — SSHHost/sshCredential 类型 + List/Add/Update/Delete
- `desktop/ssh_hosts_store_test.go`
- `desktop/ssh_known_hosts.go` — KnownHostEntry + List/RemoveKnownHost
- `desktop/ssh_known_hosts_test.go`
- `desktop/frontend/src/components/SshHostsPanel.vue` — 主机清单 CRUD UI
- `desktop/frontend/src/components/SshHostsPanel.test.ts`

**修改:**
- `desktop/config.go` — `appConfig` 加 `SSHHosts []SSHHost`
- `desktop/ssh_host.go` — 新增 `NewSshSessionByID`
- `desktop/app.go` — 新增 `errCredentialMissing` 常量
- `desktop/frontend/src/lib/api.ts` — 加 SSHHost 类型 + CRUD/连接 binding 包装
- 前端入口挂 SshHostsPanel(定位后)

---

## Task 1: appConfig 加 SSHHosts 字段

**Files:** Modify `desktop/config.go`(`appConfig` 结构,196 行附近 `PinnedSessionIDs` 旁)

- [ ] **Step 1: 加字段**

在 `appConfig` 里 `PinnedSessionIDs` 附近加:

```go
	// SSHHosts is the saved SSH host list (non-secret fields only).
	// Credentials live in the keyring keyed by SSHHost.ID, never here.
	SSHHosts []SSHHost `json:"ssh_hosts,omitempty"`
```

- [ ] **Step 2: 编译验证(SSHHost 还未定义,预期报错,Task 2 定义后消解)**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go build ./desktop/... 2>&1 | grep -v "no matching files\|frontend/dist"`
Expected: 报 `undefined: SSHHost`(符合预期,Task 2 补类型)。不 commit,直接进 Task 2。

---

## Task 2: ssh_hosts_store 类型 + Add/List(TDD)

**Files:** Create `desktop/ssh_hosts_store.go`, `desktop/ssh_hosts_store_test.go`

- [ ] **Step 1: 写失败测试(Add 生成 ID + List 读回 + 凭据入 keyring)**

`desktop/ssh_hosts_store_test.go`:

```go
package main

import (
	"encoding/json"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
)

func newHostsTestApp(t *testing.T) *App {
	t.Helper()
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	return &App{cfgStore: newTestConfigStore(t)}
}

func TestAddSSHHostPersistsAndReadsBack(t *testing.T) {
	a := newHostsTestApp(t)
	h, err := a.AddSSHHost(SSHHost{
		Alias: "box", Host: "h", Port: "22", User: "u", AuthKind: "password",
	}, sshCredential{Password: "pw"})
	if err != nil {
		t.Fatalf("AddSSHHost: %v", err)
	}
	if h.ID == "" {
		t.Fatal("expected generated ID")
	}

	list := a.ListSSHHosts()
	if len(list) != 1 || list[0].ID != h.ID || list[0].Host != "h" {
		t.Fatalf("unexpected list: %+v", list)
	}
	// config.json must not carry the secret.
	if list[0].AuthKind != "password" {
		t.Fatalf("auth kind lost: %+v", list[0])
	}

	// credential must be readable from keyring by ID.
	raw, err := safekeyring.Get(sshCredentialService(), h.ID)
	if err != nil {
		t.Fatalf("keyring get: %v", err)
	}
	var cred sshCredential
	if err := json.Unmarshal([]byte(raw), &cred); err != nil {
		t.Fatal(err)
	}
	if cred.Password != "pw" {
		t.Fatalf("password mismatch: %q", cred.Password)
	}
}
```

> `newTestConfigStore(t)` 已存在(`desktop/prefssync_adapter_test.go`)。

- [ ] **Step 2: 运行确认失败**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run TestAddSSHHost 2>&1 | head`
Expected: FAIL(`undefined: AddSSHHost` / `SSHHost` / `sshCredential` / `sshCredentialService`)。

- [ ] **Step 3: 实现 store(类型 + Add + List + service 名)**

`desktop/ssh_hosts_store.go`:

```go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/safekeyring"
	"github.com/oklog/ulid/v2"
)

// SSHHost is the non-secret part of a saved host, persisted in config.json.
// Credentials live in the keyring keyed by ID, never here.
type SSHHost struct {
	ID       string `json:"id"`
	Alias    string `json:"alias,omitempty"`
	Host     string `json:"host"`
	Port     string `json:"port,omitempty"`
	User     string `json:"user"`
	AuthKind string `json:"auth_kind"`
	Group    string `json:"group,omitempty"`
	Note     string `json:"note,omitempty"`
}

// sshCredential is JSON-encoded into a single keyring entry keyed by host ID.
type sshCredential struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

func sshCredentialService() string {
	return "com.atterm.ssh-credential.v1" + appdir.KeychainSuffix()
}

// ListSSHHosts returns the saved hosts (non-secret fields).
func (a *App) ListSSHHosts() []SSHHost {
	if a.cfgStore == nil {
		return []SSHHost{}
	}
	hosts := a.cfgStore.Get().SSHHosts
	if hosts == nil {
		return []SSHHost{}
	}
	return hosts
}

// AddSSHHost generates an ID, stores non-secret fields in config and the
// credential in the keyring. On keyring failure it rolls back the config add.
func (a *App) AddSSHHost(h SSHHost, cred sshCredential) (SSHHost, error) {
	if a.cfgStore == nil {
		return SSHHost{}, fmt.Errorf("config store not ready")
	}
	h.ID = ulid.Make().String()

	if err := storeSSHCredential(h.ID, cred); err != nil {
		return SSHHost{}, fmt.Errorf("store credential: %w", err)
	}

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = append(cfg.SSHHosts, h)
	if err := a.cfgStore.Set(cfg); err != nil {
		_ = safekeyring.Delete(sshCredentialService(), h.ID) // roll back
		return SSHHost{}, err
	}
	return h, nil
}

func storeSSHCredential(id string, cred sshCredential) error {
	blob, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	return safekeyring.Set(sshCredentialService(), id, string(blob))
}
```

- [ ] **Step 4: 运行测试通过**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run TestAddSSHHost -v 2>&1 | tail`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
gofmt -w desktop/ssh_hosts_store.go desktop/ssh_hosts_store_test.go desktop/config.go
git add desktop/ssh_hosts_store.go desktop/ssh_hosts_store_test.go desktop/config.go
git commit -m "feat(desktop): SSH 主机清单 Add/List + 凭据入 keyring

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Update / Delete + 凭据生命周期(TDD)

**Files:** Modify `desktop/ssh_hosts_store.go`, `desktop/ssh_hosts_store_test.go`

- [ ] **Step 1: 写失败测试**

追加:

```go
func TestUpdateSSHHostKeepsCredentialWhenNil(t *testing.T) {
	a := newHostsTestApp(t)
	h, _ := a.AddSSHHost(SSHHost{Host: "h", User: "u", AuthKind: "password"}, sshCredential{Password: "pw"})

	// Change non-secret field, cred=nil → credential untouched.
	h.User = "u2"
	if err := a.UpdateSSHHost(h, nil); err != nil {
		t.Fatalf("UpdateSSHHost: %v", err)
	}
	if a.ListSSHHosts()[0].User != "u2" {
		t.Fatal("user not updated")
	}
	raw, err := safekeyring.Get(sshCredentialService(), h.ID)
	if err != nil || raw == "" {
		t.Fatalf("credential should remain: %v", err)
	}
}

func TestDeleteSSHHostClearsCredential(t *testing.T) {
	a := newHostsTestApp(t)
	h, _ := a.AddSSHHost(SSHHost{Host: "h", User: "u", AuthKind: "password"}, sshCredential{Password: "pw"})

	if err := a.DeleteSSHHost(h.ID); err != nil {
		t.Fatalf("DeleteSSHHost: %v", err)
	}
	if len(a.ListSSHHosts()) != 0 {
		t.Fatal("host not removed")
	}
	if _, err := safekeyring.Get(sshCredentialService(), h.ID); err == nil {
		t.Fatal("credential should be gone")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'TestUpdateSSHHost|TestDeleteSSHHost' 2>&1 | head`
Expected: FAIL(`undefined: UpdateSSHHost` / `DeleteSSHHost`)。

- [ ] **Step 3: 实现 Update / Delete**

追加到 `ssh_hosts_store.go`:

```go
// UpdateSSHHost replaces the non-secret fields of the host with matching ID.
// If cred is non-nil the credential is replaced too; nil leaves it untouched.
func (a *App) UpdateSSHHost(h SSHHost, cred *sshCredential) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	idx := -1
	for i := range cfg.SSHHosts {
		if cfg.SSHHosts[i].ID == h.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no such host: %s", h.ID)
	}
	if cred != nil {
		if err := storeSSHCredential(h.ID, *cred); err != nil {
			return fmt.Errorf("store credential: %w", err)
		}
	}
	cfg.SSHHosts[idx] = h
	return a.cfgStore.Set(cfg)
}

// DeleteSSHHost removes the host and its credential. Missing credential is
// treated as already deleted.
func (a *App) DeleteSSHHost(id string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	out := cfg.SSHHosts[:0:0]
	found := false
	for _, hh := range cfg.SSHHosts {
		if hh.ID == id {
			found = true
			continue
		}
		out = append(out, hh)
	}
	if !found {
		return fmt.Errorf("no such host: %s", id)
	}
	cfg.SSHHosts = out
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	if err := safekeyring.Delete(sshCredentialService(), id); err != nil && err != safekeyring.ErrNotFound {
		return fmt.Errorf("delete credential: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: 运行测试通过**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'TestUpdateSSHHost|TestDeleteSSHHost' -v 2>&1 | tail`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
gofmt -w desktop/ssh_hosts_store.go desktop/ssh_hosts_store_test.go
git add desktop/ssh_hosts_store.go desktop/ssh_hosts_store_test.go
git commit -m "feat(desktop): SSH 主机 Update/Delete + 凭据生命周期

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: NewSshSessionByID(按 ID 取凭据连接)(TDD)

**Files:** Modify `desktop/ssh_host.go`, `desktop/app.go`(errCredentialMissing), `desktop/app_ssh_test.go`

- [ ] **Step 1: 写失败测试**

在 `desktop/app_ssh_test.go` 追加。复用切片 1 的 `startSSHTestServer`。App 需 host + cfgStore + keyring:

```go
func TestNewSshSessionByIDConnectsWithStoredCred(t *testing.T) {
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	addr, _ := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	h, err := a.AddSSHHost(SSHHost{Host: host, Port: port, User: "u", AuthKind: "password"},
		sshCredential{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := a.NewSshSessionByID(h.ID)
	if err != nil {
		t.Fatalf("NewSshSessionByID: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("empty session id")
	}
}

func TestNewSshSessionByIDMissingCredential(t *testing.T) {
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}

	// Add a host row directly to config WITHOUT a credential.
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "noCred", Host: "h", User: "u", AuthKind: "password"}}
	_ = a.cfgStore.Set(cfg)

	_, err := a.NewSshSessionByID("noCred")
	if err == nil || err.Error() != errCredentialMissing {
		t.Fatalf("expected errCredentialMissing, got %v", err)
	}
}
```

在该测试文件 import 补 `github.com/attson/atterm/internal/safekeyring`。

- [ ] **Step 2: 运行确认失败**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run TestNewSshSessionByID 2>&1 | head`
Expected: FAIL(`undefined: NewSshSessionByID` / `errCredentialMissing`)。

- [ ] **Step 3: 加错误常量 + 实现**

`desktop/app.go` 在 `errCodeHostKeyUnknown` 常量旁加:

```go
// errCredentialMissing is returned by NewSshSessionByID when the host has no
// stored credential; the frontend prompts the user to supply one.
const errCredentialMissing = "ssh_credential_missing"
```

`desktop/ssh_host.go` 追加:

```go
// NewSshSessionByID looks up a saved host + its credential and connects,
// reusing OpenSSHSession. Returns errCredentialMissing when no credential is
// stored so the frontend can prompt for one.
func (a *App) NewSshSessionByID(id string) (NewSessionResp, error) {
	if a.host == nil {
		return NewSessionResp{}, fmt.Errorf("relay host not ready")
	}
	var found *SSHHost
	for _, h := range a.ListSSHHosts() {
		if h.ID == id {
			hh := h
			found = &hh
			break
		}
	}
	if found == nil {
		return NewSessionResp{}, fmt.Errorf("no such host: %s", id)
	}

	raw, err := safekeyring.Get(sshCredentialService(), id)
	if err != nil {
		return NewSessionResp{}, fmt.Errorf(errCredentialMissing)
	}
	var cred sshCredential
	if err := json.Unmarshal([]byte(raw), &cred); err != nil {
		return NewSessionResp{}, fmt.Errorf(errCredentialMissing)
	}

	req := SSHConnectReq{
		Host: found.Host, Port: found.Port, User: found.User,
		AuthKind:      found.AuthKind,
		Password:      cred.Password,
		PrivateKey:    cred.PrivateKey,
		Passphrase:    cred.Passphrase,
		AcceptHostKey: false,
	}
	return a.NewSshSession(req)
}
```

在 `ssh_host.go` import 补 `encoding/json`、`github.com/attson/atterm/internal/safekeyring`。

> **注:** `NewSshSessionByID` 委托 `NewSshSession`,自动继承切片 1 的 known_hosts TOFU 行为(未知主机返回 HostKeyUnknownError)。测试里用 FixedHostKey? 不 —— NewSshSession 用真实 known_hosts 回调 + a.sshKnownHostsPath;`TestNewSshSessionByIDConnectsWithStoredCred` 已设 sshKnownHostsPath 为空 temp 文件,首次连接会因未知主机返回 HostKeyUnknownError。**修正测试**:该用例应先接受 host key —— 改为断言返回 HostKeyUnknownError,或给 store 的连接路径也支持 accept。为保持切片 2 聚焦,改测试断言:首次连接返回 *HostKeyUnknownError(证明凭据取出并进入连接流程)。见 Step 3b。

- [ ] **Step 3b: 修正连接用例的断言(TOFU 首连)**

把 `TestNewSshSessionByIDConnectsWithStoredCred` 的断言改为:凭据被取出并进入连接(首连未知主机 → HostKeyUnknownError):

```go
	_, err = a.NewSshSessionByID(h.ID)
	var hkErr *HostKeyUnknownError
	if !errors.As(err, &hkErr) {
		t.Fatalf("expected HostKeyUnknownError (cred resolved, TOFU prompt), got %v", err)
	}
```

import 补 `errors`。函数名保留但语义=“凭据解析并进入连接流程”。（真正连上属手动冒烟。）

- [ ] **Step 4: 运行测试通过**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run TestNewSshSessionByID -v 2>&1 | tail`
Expected: PASS(两个用例)。

- [ ] **Step 5: Commit**

```bash
gofmt -w desktop/ssh_host.go desktop/app.go desktop/app_ssh_test.go
git add desktop/ssh_host.go desktop/app.go desktop/app_ssh_test.go
git commit -m "feat(desktop): NewSshSessionByID 按 ID 取凭据连接

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: known_hosts 管理(List / Remove)(TDD)

**Files:** Create `desktop/ssh_known_hosts.go`, `desktop/ssh_known_hosts_test.go`

- [ ] **Step 1: 写失败测试**

`desktop/ssh_known_hosts_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAndRemoveKnownHosts(t *testing.T) {
	dir := t.TempDir()
	kh := filepath.Join(dir, "known_hosts")
	// Two plain (non-hashed) entries.
	content := "host-a ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAA\n" +
		"host-b ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBBB\n"
	if err := os.WriteFile(kh, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{sshKnownHostsPath: kh}

	entries, err := a.ListKnownHosts()
	if err != nil {
		t.Fatalf("ListKnownHosts: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if err := a.RemoveKnownHost("host-a"); err != nil {
		t.Fatalf("RemoveKnownHost: %v", err)
	}
	entries, _ = a.ListKnownHosts()
	if len(entries) != 1 || entries[0].Host != "host-b" {
		t.Fatalf("host-a not removed: %+v", entries)
	}
}

func TestKnownHostsMissingFileIsEmpty(t *testing.T) {
	a := &App{sshKnownHostsPath: filepath.Join(t.TempDir(), "nope")}
	entries, err := a.ListKnownHosts()
	if err != nil {
		t.Fatalf("ListKnownHosts on missing file: %v", err)
	}
	if len(entries) != 0 {
		t.Fatal("expected empty")
	}
	if err := a.RemoveKnownHost("x"); err != nil {
		t.Fatalf("RemoveKnownHost idempotent: %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'KnownHosts' 2>&1 | head`
Expected: FAIL(`undefined: ListKnownHosts` / `RemoveKnownHost`)。

- [ ] **Step 3: 实现**

`desktop/ssh_known_hosts.go`:

```go
package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// KnownHostEntry is one line of ~/.ssh/known_hosts surfaced to the frontend.
type KnownHostEntry struct {
	Host        string `json:"host"`
	Fingerprint string `json:"fingerprint"`
}

func (a *App) knownHostsPath() string {
	if a.sshKnownHostsPath != "" {
		return a.sshKnownHostsPath
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".ssh", "known_hosts")
	}
	return ""
}

// ListKnownHosts parses the known_hosts file into entries. A missing file
// yields an empty list (not an error).
func (a *App) ListKnownHosts() ([]KnownHostEntry, error) {
	path := a.knownHostsPath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []KnownHostEntry{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []KnownHostEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		host := fields[0]
		fp := ""
		if _, _, key, _, _, err := ssh.ParseKnownHosts([]byte(line)); err == nil && key != nil {
			fp = ssh.FingerprintSHA256(key)
		}
		out = append(out, KnownHostEntry{Host: host, Fingerprint: fp})
	}
	if out == nil {
		out = []KnownHostEntry{}
	}
	return out, sc.Err()
}

// RemoveKnownHost drops every line whose first field matches host and rewrites
// the file. Missing file / no match is a no-op (idempotent).
func (a *App) RemoveKnownHost(host string) error {
	path := a.knownHostsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && fields[0] == host {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	if out != "" {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0600)
}
```

> **注:** `ssh.ParseKnownHosts` 单行解析取指纹;解析失败(如 hashed 条目)时指纹留空但仍列出 host 原文。删除按第一字段精确匹配 —— hashed(`|1|...`)条目按原文匹配,不做反哈希(记为已知限制)。

- [ ] **Step 4: 运行测试通过**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'KnownHosts' -v 2>&1 | tail`
Expected: PASS(两个用例)。

- [ ] **Step 5: Commit**

```bash
gofmt -w desktop/ssh_known_hosts.go desktop/ssh_known_hosts_test.go
git add desktop/ssh_known_hosts.go desktop/ssh_known_hosts_test.go
git commit -m "feat(desktop): known_hosts 条目 List/Remove 管理

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: 前端 api.ts 绑定

**Files:** Modify `desktop/frontend/src/lib/api.ts`

- [ ] **Step 1: 加类型 + 接口方法 + 导出函数**

在 `SSHConnectReq` 之后加:

```ts
export interface SSHHost {
  id: string;
  alias?: string;
  host: string;
  port?: string;
  user: string;
  auth_kind: "password" | "privateKey";
  group?: string;
  note?: string;
}

export interface SSHCredential {
  password?: string;
  private_key?: string;
  passphrase?: string;
}

export interface KnownHostEntry {
  host: string;
  fingerprint: string;
}
```

在 `AppBindings` 接口里加(NewSshSession 附近):

```ts
  NewSshSessionByID(id: string): Promise<NewSessionResp>;
  ListSSHHosts(): Promise<SSHHost[]>;
  AddSSHHost(h: SSHHost, cred: SSHCredential): Promise<SSHHost>;
  UpdateSSHHost(h: SSHHost, cred: SSHCredential | null): Promise<void>;
  DeleteSSHHost(id: string): Promise<void>;
  ListKnownHosts(): Promise<KnownHostEntry[]>;
  RemoveKnownHost(host: string): Promise<void>;
```

在 `newSshSession` 导出函数之后加导出包装:

```ts
export function newSshSessionByID(id: string): Promise<NewSessionResp> {
  return bindings().NewSshSessionByID(id);
}
export function listSSHHosts(): Promise<SSHHost[]> {
  return bindings().ListSSHHosts();
}
export function addSSHHost(h: SSHHost, cred: SSHCredential): Promise<SSHHost> {
  return bindings().AddSSHHost(h, cred);
}
export function updateSSHHost(h: SSHHost, cred: SSHCredential | null): Promise<void> {
  return bindings().UpdateSSHHost(h, cred);
}
export function deleteSSHHost(id: string): Promise<void> {
  return bindings().DeleteSSHHost(id);
}
export function listKnownHosts(): Promise<KnownHostEntry[]> {
  return bindings().ListKnownHosts();
}
export function removeKnownHost(host: string): Promise<void> {
  return bindings().RemoveKnownHost(host);
}
```

- [ ] **Step 2: 类型检查(仅看本文件无新错误)**

Run: `cd desktop/frontend && export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH" && npx vue-tsc --noEmit 2>&1 | grep "lib/api.ts" | head`
Expected: 无 api.ts 相关错误(预存 web/shared 错误无关)。

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/lib/api.ts
git commit -m "feat(frontend): api.ts 增 SSH 主机 CRUD + known_hosts 绑定

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: 前端 SshHostsPanel 组件(TDD)

**Files:** Create `desktop/frontend/src/components/SshHostsPanel.vue`, `SshHostsPanel.test.ts`

- [ ] **Step 1: 写失败测试**

`SshHostsPanel.test.ts`(mock `../lib/api`):

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SshHostsPanel from "./SshHostsPanel.vue";

const listSSHHosts = vi.fn();
const addSSHHost = vi.fn();
const deleteSSHHost = vi.fn();
const newSshSessionByID = vi.fn();
vi.mock("../lib/api", () => ({
  listSSHHosts: (...a: unknown[]) => listSSHHosts(...a),
  addSSHHost: (...a: unknown[]) => addSSHHost(...a),
  deleteSSHHost: (...a: unknown[]) => deleteSSHHost(...a),
  newSshSessionByID: (...a: unknown[]) => newSshSessionByID(...a),
}));

beforeEach(() => {
  listSSHHosts.mockReset().mockResolvedValue([]);
  addSSHHost.mockReset();
  deleteSSHHost.mockReset();
  newSshSessionByID.mockReset();
});

describe("SshHostsPanel", () => {
  it("挂载时加载主机列表", async () => {
    listSSHHosts.mockResolvedValueOnce([
      { id: "1", host: "h", user: "u", auth_kind: "password", alias: "box" },
    ]);
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    expect(wrapper.text()).toContain("box");
  });

  it("点击某主机连接触发 newSshSessionByID 并 emit connected", async () => {
    listSSHHosts.mockResolvedValueOnce([
      { id: "1", host: "h", user: "u", auth_kind: "password", alias: "box" },
    ]);
    newSshSessionByID.mockResolvedValueOnce({ session_id: "s1" });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await wrapper.find('[data-test="ssh-connect-1"]').trigger("click");
    await flushPromises();
    expect(newSshSessionByID).toHaveBeenCalledWith("1");
    expect(wrapper.emitted("connected")?.[0]).toEqual(["s1"]);
  });

  it("添加主机后刷新列表", async () => {
    addSSHHost.mockResolvedValueOnce({ id: "2", host: "h2", user: "u2", auth_kind: "password" });
    const wrapper = mount(SshHostsPanel);
    await flushPromises();
    await wrapper.find('[data-test="ssh-add-host"]').setValue("h2");
    await wrapper.find('[data-test="ssh-add-user"]').setValue("u2");
    await wrapper.find('[data-test="ssh-add-password"]').setValue("pw");
    await wrapper.find('[data-test="ssh-add-submit"]').trigger("click");
    await flushPromises();
    expect(addSSHHost).toHaveBeenCalledWith(
      expect.objectContaining({ host: "h2", user: "u2", auth_kind: "password" }),
      expect.objectContaining({ password: "pw" }),
    );
  });
});
```

- [ ] **Step 2: 运行确认失败**

Run: `cd desktop/frontend && export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH" && npx vitest run src/components/SshHostsPanel.test.ts 2>&1 | tail`
Expected: FAIL(组件不存在)。

- [ ] **Step 3: 实现 SshHostsPanel.vue**

按测试契约:挂载 `listSSHHosts` 填列表(显示 alias/host);每行有 `data-test="ssh-connect-<id>"` 连接按钮 → `newSshSessionByID(id)` → emit `connected`;添加区含 `ssh-add-host`/`ssh-add-user`/`ssh-add-password`/`ssh-add-submit` → `addSSHHost` 后刷新。删除按钮调 `deleteSSHHost` 后刷新。样式复用现有面板 CSS 变量(参考 `NewSshDialog.vue` / `SessionPickerDialog.vue`)。emit:`(e:"connected", id:string)`、`(e:"close")`。

```vue
<script lang="ts" setup>
import { ref, onMounted } from "vue";
import {
  listSSHHosts, addSSHHost, deleteSSHHost, newSshSessionByID,
  type SSHHost,
} from "../lib/api";

const emit = defineEmits<{
  (e: "connected", sessionId: string): void;
  (e: "close"): void;
}>();

const hosts = ref<SSHHost[]>([]);
const errorMsg = ref("");
const nHost = ref("");
const nPort = ref("22");
const nUser = ref("");
const nPassword = ref("");
const nAlias = ref("");

async function reload() {
  hosts.value = await listSSHHosts();
}
onMounted(reload);

async function connect(id: string) {
  errorMsg.value = "";
  try {
    const resp = await newSshSessionByID(id);
    emit("connected", resp.session_id);
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

async function add() {
  errorMsg.value = "";
  try {
    await addSSHHost(
      { id: "", alias: nAlias.value, host: nHost.value, port: nPort.value || "22",
        user: nUser.value, auth_kind: "password" },
      { password: nPassword.value },
    );
    nHost.value = ""; nUser.value = ""; nPassword.value = ""; nAlias.value = ""; nPort.value = "22";
    await reload();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

async function remove(id: string) {
  await deleteSSHHost(id);
  await reload();
}
</script>

<template>
  <div class="ssh-hosts">
    <h2>SSH Hosts</h2>
    <ul>
      <li v-for="h in hosts" :key="h.id">
        <span class="name">{{ h.alias || (h.user + "@" + h.host) }}</span>
        <button :data-test="`ssh-connect-${h.id}`" @click="connect(h.id)">Connect</button>
        <button :data-test="`ssh-delete-${h.id}`" @click="remove(h.id)">Delete</button>
      </li>
    </ul>

    <div class="add">
      <input data-test="ssh-add-alias" v-model="nAlias" placeholder="alias" />
      <input data-test="ssh-add-host" v-model="nHost" placeholder="host" />
      <input data-test="ssh-add-port" v-model="nPort" placeholder="port" />
      <input data-test="ssh-add-user" v-model="nUser" placeholder="user" />
      <input data-test="ssh-add-password" type="password" v-model="nPassword" placeholder="password" />
      <button class="primary" data-test="ssh-add-submit" @click="add">Add Host</button>
    </div>

    <p v-if="errorMsg" class="error" data-test="ssh-hosts-error">{{ errorMsg }}</p>
    <div class="row end"><button @click="$emit('close')">Close</button></div>
  </div>
</template>

<style scoped>
.ssh-hosts { display: flex; flex-direction: column; gap: 10px; padding: 16px; }
.ssh-hosts h2 { margin: 0; font-size: 14px; text-transform: uppercase; color: var(--fg-dim); }
.ssh-hosts ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.ssh-hosts li { display: flex; align-items: center; gap: 8px; }
.ssh-hosts .name { flex: 1; color: var(--fg); font-size: 13px; }
.add { display: flex; flex-wrap: wrap; gap: 6px; }
.add input { background: var(--bg); border: 1px solid var(--border); border-radius: 4px; padding: 5px 7px; color: var(--fg); font-size: 12px; }
.error { color: var(--bad); font-size: 12px; margin: 0; }
.row.end { display: flex; justify-content: flex-end; }
.primary { background: var(--accent); color: #0d1117; border-color: var(--accent); font-weight: 600; }
</style>
```

- [ ] **Step 4: 运行测试通过**

Run: `cd desktop/frontend && export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH" && npx vitest run src/components/SshHostsPanel.test.ts 2>&1 | tail`
Expected: PASS(3 用例)。

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SshHostsPanel.vue desktop/frontend/src/components/SshHostsPanel.test.ts
git commit -m "feat(frontend): SSH 主机清单面板(CRUD + 一键连)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: 挂载入口 + 全量验证 + 收尾

**Files:** Modify `desktop/frontend/src/App.vue`(挂 SshHostsPanel), `AGENTS.md`

- [ ] **Step 1: 在 App.vue 挂载 SshHostsPanel**

复用切片 1 的 TabBar `new-ssh` 模式:把 `showSshDialog` 场景扩展为"主机面板"。最小做法:加 `showSshHosts` ref + import SshHostsPanel + template 挂载 `<SshHostsPanel v-if="showSshHosts" @connected="onSshConnected" @close="showSshHosts=false" />`,并在 TabBar SSH 按钮改为打开面板(或新增一个入口)。`onSshConnected` 切片 1 已存在,直接复用。

具体:
- import:`import SshHostsPanel from "./components/SshHostsPanel.vue";`
- ref:`const showSshHosts = ref(false);`
- TabBar `@new-ssh` 改为 `showSshHosts = true`(主机面板含"临时连接"入口可后续加;切片 2 先让面板成为主入口)
- template 挂载面板

- [ ] **Step 2: 前端全量测试(SSH 相关必过,预存 4 文件失败无关)**

Run: `cd desktop/frontend && export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH" && npx vitest run src/components/SshHostsPanel.test.ts src/components/NewSshDialog.test.ts src/components/TabBar.test.ts 2>&1 | tail`
Expected: 全 PASS。

- [ ] **Step 3: 全量 Go 测试 + vet**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./internal/sshclient/... ./desktop/... -count=1 2>&1 | tail && go vet ./desktop/... 2>&1 | grep -v "no matching files\|frontend/dist"`
Expected: 全 ok,vet 无输出。

- [ ] **Step 4: 更新 AGENTS.md**

在 SSH 切片 1 记录那句后追加切片 2:主机清单持久化(`desktop/ssh_hosts_store.go`,非敏感入 config.json / 凭据 JSON blob 入 `com.atterm.ssh-credential.v1` keyring,account=主机 ULID)、`NewSshSessionByID` 按 ID 取凭据连接、known_hosts 管理(`desktop/ssh_known_hosts.go`)、前端 `SshHostsPanel.vue`。spec `docs/superpowers/specs/2026-08-02-ssh-host-store-slice2-design-draft.md`。

- [ ] **Step 5: 手动冒烟(需真实 SSH 主机 + wails dev)**

`wails dev` → SSH 面板 → 添加主机(存密码)→ 从清单连 → 手机 attach;删主机后确认 keyring 凭据清除;known_hosts 面板删条目后重连重新 TOFU。

- [ ] **Step 6: 最终 commit**

```bash
git add desktop/frontend/src/App.vue AGENTS.md
git commit -m "feat: SSH 主机清单面板接入 App + AGENTS.md 记录(切片2)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- 主机 CRUD → Task 2/3 ✅
- 数据模型(别名/host/port/user/auth/group/note + ID)→ Task 2 SSHHost ✅
- 凭据入 keyring(account=ID)→ Task 2 storeSSHCredential ✅
- 从清单连(按 ID 取凭据)→ Task 4 NewSshSessionByID ✅
- 未存凭据 errCredentialMissing → Task 4 ✅
- known_hosts 管理 → Task 5 ✅
- 切片 1 即席路径保留 → NewSshSession 未改,NewSshSessionByID 是新增 ✅
- 前端 CRUD + 一键连 → Task 7 ✅

**Placeholder scan:** 无 TBD/TODO。Task 4 Step 3b 是对 3a 测试断言的显式修正(TOFU 首连语义),非占位。

**Type consistency:** `SSHHost`(Task 2 Go / Task 6 TS 一致字段)、`sshCredential`/`SSHCredential`、`sshCredentialService()`(Task 2 定义、Task 3/4 用)、`NewSshSessionByID`(Task 4 定义、Task 6/7 用)、`errCredentialMissing`(Task 4)、`ListKnownHosts/RemoveKnownHost`(Task 5 定义、Task 6 用)、`onSshConnected`/`openSshTab`(切片 1 已有,Task 8 复用)—— 一致。
