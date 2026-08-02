# SSH 主机清单 E2EE 跨端同步(切片 3)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use checkbox (`- [ ]`).

**Goal:** 把 SSH 主机清单(非敏感字段 + 凭据)整体用 account_key seal 成密文,作为一个 prefssync value 跨端同步,relay 全程只见密文;account_key 不可用时仅本地不上传。

**Architecture:** 新增 `desktop/ssh_sync.go` 做 seal/open 编解码;`syncedKeys` 加 `ssh_hosts_encrypted`;`appConfigAdapter` 注入 account_key closure,对该 key 特殊处理(read→seal,write→open);切片 2 的 CRUD 触发 `markPrefDirtyAndPush`。

**Tech Stack:** Go 1.23(`~/sdk/go1.23.12`),`internal/e2eecrypto`(SealUnsequenced/DeriveSessionKey),`internal/prefssync`,`github.com/google/uuid`。

**环境备注:** Go 命令前 `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12"`。desktop 测试 embed 产物切片 1 已生成。

---

## File Structure

**新建:**
- `desktop/ssh_sync.go` — sshSyncPayload/sealSSHHosts/openSSHHosts + 固定 UUID/frameType
- `desktop/ssh_sync_test.go`

**修改:**
- `internal/prefssync/sync.go` — syncedKeys 加 `ssh_hosts_encrypted`
- `desktop/prefssync_adapter.go` — appConfigAdapter 注入 account_key closure + 新 key 的 read/write
- `desktop/app.go` — 构造 adapter 时传 account_key closure
- `desktop/ssh_hosts_store.go` — Add/Update/Delete 后 markPrefDirtyAndPush
- `desktop/prefssync_adapter_test.go` 或新测试 — adapter 新 key 集成

---

## Task 1: prefssync 白名单加 key

**Files:** Modify `internal/prefssync/sync.go`(syncedKeys, 51 行附近)

- [ ] **Step 1: 加 key**

```go
var syncedKeys = []string{
	"locale_preference",
	"quick_templates",
	"notifications_enabled",
	"ai_notifications_only",
	"command_notify_threshold_seconds",
	"shell_integration_enabled",
	"pinned_session_ids",
	"ssh_hosts_encrypted",
}
```

- [ ] **Step 2: 跑 prefssync 现有测试确认无回归**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./internal/prefssync/ -count=1`
Expected: ok(若有断言 key 数量的测试需同步更新——见下)。

- [ ] **Step 3: 若测试断言了 key 列表/数量,更新它**

Run: `grep -n "SyncedKeys\|syncedKeys\|len(" internal/prefssync/sync_test.go | head`
若存在对 SyncedKeys() 长度或内容的断言,加入 `ssh_hosts_encrypted`。重跑 Step 2 至 ok。

- [ ] **Step 4: Commit**

```bash
git add internal/prefssync/sync.go internal/prefssync/sync_test.go
git commit -m "feat(prefssync): 同步白名单加 ssh_hosts_encrypted

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: ssh_sync seal/open 往返(TDD)

**Files:** Create `desktop/ssh_sync.go`, `desktop/ssh_sync_test.go`

- [ ] **Step 1: 写失败测试**

`desktop/ssh_sync_test.go`:

```go
package main

import (
	"crypto/rand"
	"strings"
	"testing"
)

func testAccountKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpenSSHHostsRoundTrip(t *testing.T) {
	key := testAccountKey(t)
	hosts := []SSHHost{{ID: "1", Host: "h", User: "u", AuthKind: "password", Alias: "box"}}
	creds := map[string]sshCredential{"1": {Password: "secret-pw"}}

	blob, err := sealSSHHosts(key, hosts, creds)
	if err != nil {
		t.Fatalf("sealSSHHosts: %v", err)
	}
	if blob == nil {
		t.Fatal("expected ciphertext, got nil")
	}
	// relay must only see ciphertext — no plaintext host/password leakage.
	if strings.Contains(string(blob), "secret-pw") || strings.Contains(string(blob), "box") {
		t.Fatalf("plaintext leaked into sealed blob: %s", blob)
	}

	gotHosts, gotCreds, err := openSSHHosts(key, blob)
	if err != nil {
		t.Fatalf("openSSHHosts: %v", err)
	}
	if len(gotHosts) != 1 || gotHosts[0].ID != "1" || gotHosts[0].Alias != "box" {
		t.Fatalf("hosts mismatch: %+v", gotHosts)
	}
	if gotCreds["1"].Password != "secret-pw" {
		t.Fatalf("cred mismatch: %+v", gotCreds)
	}
}

func TestSealSSHHostsEmptyAccountKeySkips(t *testing.T) {
	blob, err := sealSSHHosts(nil, []SSHHost{{ID: "1"}}, map[string]sshCredential{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if blob != nil {
		t.Fatalf("empty account key must skip (nil blob), got %s", blob)
	}
}

func TestOpenSSHHostsWrongKeyFails(t *testing.T) {
	blob, err := sealSSHHosts(testAccountKey(t), []SSHHost{{ID: "1"}}, map[string]sshCredential{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := openSSHHosts(testAccountKey(t), blob); err == nil {
		t.Fatal("open with wrong key must fail")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'SSHHosts.*RoundTrip|SealSSHHosts|OpenSSHHosts' 2>&1 | head`
Expected: FAIL(`undefined: sealSSHHosts` / `openSSHHosts`)。

- [ ] **Step 3: 实现 ssh_sync.go**

```go
package main

import (
	"encoding/base64"
	"encoding/json"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/google/uuid"
)

// sshHostsSyncSessionID is the fixed virtual session UUID used to derive the
// key and bind the AAD for host-list sync. It is NOT a real session — just a
// stable namespace so the sealed blob is bound to this purpose. Value is an
// arbitrary fixed UUID; do not change it or existing synced blobs won't open.
var sshHostsSyncSessionID = uuid.MustParse("55480000-0000-4000-8000-000000000001")

// sshSyncFrameType is an AAD-only tag for host-list sync seals. It never
// appears on the relay wire (not a proto frame type) — it only binds the AAD.
const sshSyncFrameType = 0xF0

type sshSyncPayload struct {
	Hosts []sshSyncHost `json:"hosts"`
}

type sshSyncHost struct {
	Host SSHHost       `json:"host"`
	Cred sshCredential `json:"cred"`
}

// sealSSHHosts packs the host list + credentials and seals it with the
// account key. Returns (nil, nil) when accountKey is empty — the caller treats
// that as "skip sync" (local-only, never send plaintext to the relay).
func sealSSHHosts(accountKey []byte, hosts []SSHHost, creds map[string]sshCredential) (json.RawMessage, error) {
	if len(accountKey) == 0 {
		return nil, nil
	}
	payload := sshSyncPayload{}
	for _, h := range hosts {
		payload.Hosts = append(payload.Hosts, sshSyncHost{Host: h, Cred: creds[h.ID]})
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	sessionKey, err := e2eecrypto.DeriveSessionKey(accountKey, sshHostsSyncSessionID)
	if err != nil {
		return nil, err
	}
	ct, err := e2eecrypto.SealUnsequenced(sessionKey, sshHostsSyncSessionID, sshSyncFrameType, plain)
	if err != nil {
		return nil, err
	}
	// Store as a JSON string of base64(ciphertext) so it round-trips as a
	// prefssync value.
	b64, err := json.Marshal(base64.StdEncoding.EncodeToString(ct))
	if err != nil {
		return nil, err
	}
	return b64, nil
}

// openSSHHosts decrypts a synced blob back into hosts + credentials keyed by
// host ID.
func openSSHHosts(accountKey []byte, value json.RawMessage) ([]SSHHost, map[string]sshCredential, error) {
	var b64 string
	if err := json.Unmarshal(value, &b64); err != nil {
		return nil, nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, nil, err
	}
	sessionKey, err := e2eecrypto.DeriveSessionKey(accountKey, sshHostsSyncSessionID)
	if err != nil {
		return nil, nil, err
	}
	plain, err := e2eecrypto.OpenUnsequenced(sessionKey, sshHostsSyncSessionID, sshSyncFrameType, ct)
	if err != nil {
		return nil, nil, err
	}
	var payload sshSyncPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, nil, err
	}
	hosts := make([]SSHHost, 0, len(payload.Hosts))
	creds := make(map[string]sshCredential, len(payload.Hosts))
	for _, sh := range payload.Hosts {
		hosts = append(hosts, sh.Host)
		creds[sh.Host.ID] = sh.Cred
	}
	return hosts, creds, nil
}
```

- [ ] **Step 4: 运行测试通过**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'SSHHosts.*RoundTrip|SealSSHHosts|OpenSSHHosts' -v 2>&1 | tail`
Expected: PASS(3 用例)。

- [ ] **Step 5: Commit**

```bash
gofmt -w desktop/ssh_sync.go desktop/ssh_sync_test.go
git add desktop/ssh_sync.go desktop/ssh_sync_test.go
git commit -m "feat(desktop): SSH 主机清单 seal/open E2EE 编解码

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: adapter 注入 account_key + 新 key read/write(TDD)

**Files:** Modify `desktop/prefssync_adapter.go`, `desktop/app.go`, `desktop/ssh_hosts_store_test.go`(新集成测试)

- [ ] **Step 1: 写失败测试**

在 `desktop/ssh_hosts_store_test.go` 追加(复用 newHostsTestApp 但要带 account_key 的 adapter)。改为直接测 adapter:

```go
func TestAdapterSSHHostsEncryptedRoundTrip(t *testing.T) {
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	cs := newTestConfigStore(t)
	a := &App{cfgStore: cs}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	a.accountKey = key

	// Add a host via the store (writes config + keyring).
	h, err := a.AddSSHHost(SSHHost{Host: "h", User: "u", AuthKind: "password"}, sshCredential{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	adapter := newAppConfigAdapter(cs, a.accountKeyForSync)

	// ReadValue seals: value must be non-empty and must NOT contain plaintext.
	val, ok := adapter.ReadValue("ssh_hosts_encrypted")
	if !ok {
		t.Fatal("expected ReadValue ok with account key")
	}
	if strings.Contains(string(val), "pw") || strings.Contains(string(val), h.ID) {
		t.Fatalf("plaintext leaked: %s", val)
	}

	// Wipe local state, then WriteValue must restore hosts + credential.
	cfg := cs.Get()
	cfg.SSHHosts = nil
	_ = cs.Set(cfg)
	_ = safekeyring.Delete(sshCredentialService(), h.ID)

	if err := adapter.WriteValue("ssh_hosts_encrypted", val); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}
	if got := cs.Get().SSHHosts; len(got) != 1 || got[0].ID != h.ID {
		t.Fatalf("hosts not restored: %+v", got)
	}
	raw, err := safekeyring.Get(sshCredentialService(), h.ID)
	if err != nil || !strings.Contains(raw, "pw") {
		t.Fatalf("credential not restored: %v %q", err, raw)
	}
}

func TestAdapterSSHHostsNoAccountKeySkips(t *testing.T) {
	cs := newTestConfigStore(t)
	a := &App{cfgStore: cs} // accountKey nil
	adapter := newAppConfigAdapter(cs, a.accountKeyForSync)

	if _, ok := adapter.ReadValue("ssh_hosts_encrypted"); ok {
		t.Fatal("no account key must skip ReadValue (ok=false)")
	}
	// WriteValue with no key is a no-op, not an error.
	if err := adapter.WriteValue("ssh_hosts_encrypted", json.RawMessage(`"x"`)); err != nil {
		t.Fatalf("WriteValue no-op expected, got %v", err)
	}
}
```

在该测试文件 import 补 `strings`、`encoding/json`。

- [ ] **Step 2: 运行确认失败**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'TestAdapterSSHHosts' 2>&1 | head`
Expected: FAIL(`newAppConfigAdapter` 参数不符 / `accountKeyForSync` 未定义)。

- [ ] **Step 3: 实现**

**3a.** `desktop/app.go` 加一个读 account_key 的方法(加锁):

```go
// accountKeyForSync returns a copy of the unlocked E2EE account key, or nil
// when E2EE is not active. Used by the prefssync adapter to seal/open the
// SSH host list.
func (a *App) accountKeyForSync() []byte {
	a.accountKeyMu.Lock()
	defer a.accountKeyMu.Unlock()
	if len(a.accountKey) == 0 {
		return nil
	}
	out := make([]byte, len(a.accountKey))
	copy(out, a.accountKey)
	return out
}
```

> 确认 `accountKeyMu` 是 App 字段(切片 1 探索见过 `a.accountKeyMu.Lock()`）。若字段名不同,用实际的。

**3b.** `desktop/prefssync_adapter.go`:改 struct + 构造函数签名 + 新 key 分支。

```go
type appConfigAdapter struct {
	store      *configStore
	accountKey func() []byte
}

func newAppConfigAdapter(s *configStore, accountKey func() []byte) *appConfigAdapter {
	return &appConfigAdapter{store: s, accountKey: accountKey}
}
```

在 `ReadValue` 的 switch 里,`pinned_session_ids` case 之后加:

```go
	case "ssh_hosts_encrypted":
		key := a.accountKey()
		if len(key) == 0 {
			return nil, false // E2EE inactive → local only, never sync
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
		blob, err := sealSSHHosts(key, c.SSHHosts, creds)
		if err != nil || blob == nil {
			return nil, false
		}
		return blob, true
```

在 `WriteValue` 的 switch 里,`pinned_session_ids` case 之后、`default` 之前加:

```go
	case "ssh_hosts_encrypted":
		key := a.accountKey()
		if len(key) == 0 {
			return nil // no key → ignore inbound sync silently
		}
		hosts, creds, err := openSSHHosts(key, value)
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
		c.SSHHosts = hosts
		return a.store.Set(c)
```

在 `prefssync_adapter.go` import 补 `github.com/attson/atterm/internal/safekeyring`。

**3c.** `desktop/app.go:363` 更新构造调用:

```go
	adapter := newAppConfigAdapter(a.cfgStore, a.accountKeyForSync)
```

- [ ] **Step 4: 运行测试通过**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'TestAdapterSSHHosts' -v 2>&1 | tail`
Expected: PASS(2 用例)。

- [ ] **Step 5: Commit**

```bash
gofmt -w desktop/prefssync_adapter.go desktop/app.go desktop/ssh_hosts_store_test.go
git add desktop/prefssync_adapter.go desktop/app.go desktop/ssh_hosts_store_test.go
git commit -m "feat(desktop): prefssync adapter 支持 ssh_hosts_encrypted(seal/open)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: CRUD 触发同步 push(TDD)

**Files:** Modify `desktop/ssh_hosts_store.go`, `desktop/ssh_hosts_store_test.go`

- [ ] **Step 1: 写失败测试(Add/Delete 后 meta.Dirty)**

```go
func TestCRUDMarksSSHSyncDirty(t *testing.T) {
	a := newHostsTestApp(t)
	// prefsSync nil in this harness → markPrefDirtyAndPush is a no-op on push,
	// but MarkDirty writes meta. Wire a minimal engine-less dirty check via
	// the adapter's meta. Simplest: assert config PrefsMeta after CRUD.
	h, err := a.AddSSHHost(SSHHost{Host: "h", User: "u", AuthKind: "password"}, sshCredential{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.cfgStore.Get().PrefsMeta["ssh_hosts_encrypted"].Dirty {
		t.Fatal("Add should mark ssh_hosts_encrypted dirty")
	}

	// reset dirty, then Delete
	cfg := a.cfgStore.Get()
	m := cfg.PrefsMeta["ssh_hosts_encrypted"]
	m.Dirty = false
	cfg.PrefsMeta["ssh_hosts_encrypted"] = m
	_ = a.cfgStore.Set(cfg)

	if err := a.DeleteSSHHost(h.ID); err != nil {
		t.Fatal(err)
	}
	if !a.cfgStore.Get().PrefsMeta["ssh_hosts_encrypted"].Dirty {
		t.Fatal("Delete should mark ssh_hosts_encrypted dirty")
	}
}
```

> **注:** `markPrefDirtyAndPush` 走 `a.prefsSync.MarkDirty`,当 `a.prefsSync == nil`(测试)时直接 return,不写 meta。所以要让 CRUD 写 meta dirty 独立于 prefsSync。**改用**:CRUD 里调一个 `a.markSSHHostsDirty()`,它直接写 config.PrefsMeta 的 dirty(不依赖 prefsSync),再尝试 push。见 Step 3。

- [ ] **Step 2: 运行确认失败**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run TestCRUDMarksSSHSyncDirty 2>&1 | head`
Expected: FAIL(dirty 未被置位)。

- [ ] **Step 3: 实现 — CRUD 后标记 dirty 并 push**

在 `desktop/ssh_hosts_store.go` 加辅助:

```go
// markSSHHostsDirty flags the synced host-list value dirty in config (so a
// later Push uploads it) and, when a sync engine is wired, kicks a push. It
// writes meta directly so it works even before the engine exists (tests).
func (a *App) markSSHHostsDirty() {
	if a.cfgStore == nil {
		return
	}
	cfg := a.cfgStore.Get()
	if cfg.PrefsMeta == nil {
		cfg.PrefsMeta = map[string]prefsMetaEntry{}
	}
	m := cfg.PrefsMeta["ssh_hosts_encrypted"]
	m.Dirty = true
	m.UpdatedAtLocal = nowUnixMilli()
	cfg.PrefsMeta["ssh_hosts_encrypted"] = m
	_ = a.cfgStore.Set(cfg)

	if a.prefsSync != nil {
		a.markPrefDirtyAndPush("ssh_hosts_encrypted")
	}
}
```

> `nowUnixMilli()`:若项目已有等价 helper 用它;否则用 `time.Now().UnixMilli()`(import time)。确认 `prefsMetaEntry` 是 config.go 里的类型(切片 1 探索见过它:`UpdatedAtLocal int64` + `Dirty bool`)。

在 `AddSSHHost` 的 `return h, nil` 前、`UpdateSSHHost` 的 `return a.cfgStore.Set(...)` 改为先 set 再 mark、`DeleteSSHHost` 成功后,分别加 `a.markSSHHostsDirty()`。具体:

- AddSSHHost:`a.cfgStore.Set(cfg)` 成功后、`return h, nil` 前加 `a.markSSHHostsDirty()`。
- UpdateSSHHost:把结尾 `return a.cfgStore.Set(cfg)` 改为:
  ```go
  if err := a.cfgStore.Set(cfg); err != nil {
      return err
  }
  a.markSSHHostsDirty()
  return nil
  ```
- DeleteSSHHost:`safekeyring.Delete` 成功分支后、`return nil` 前加 `a.markSSHHostsDirty()`。

- [ ] **Step 4: 运行测试通过 + 全 store 测试无回归**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./desktop/ -run 'TestCRUDMarksSSHSyncDirty|TestAddSSHHost|TestUpdateSSHHost|TestDeleteSSHHost' -v 2>&1 | tail`
Expected: 全 PASS。

- [ ] **Step 5: Commit**

```bash
gofmt -w desktop/ssh_hosts_store.go desktop/ssh_hosts_store_test.go
git add desktop/ssh_hosts_store.go desktop/ssh_hosts_store_test.go
git commit -m "feat(desktop): SSH 主机 CRUD 触发 ssh_hosts_encrypted 同步

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: 全量验证 + 收尾

**Files:** Modify `AGENTS.md`

- [ ] **Step 1: 全量 Go 测试 + vet**

Run: `export PATH="$HOME/sdk/go1.23.12/bin:$PATH" GOROOT="$HOME/sdk/go1.23.12" && go test ./internal/... ./desktop/... -count=1 2>&1 | tail && go vet ./internal/prefssync/... ./desktop/... 2>&1 | grep -v "no matching files\|frontend/dist"`
Expected: 全 ok,vet 无输出。

- [ ] **Step 2: 前端 SSH 相关测试(确认未破坏)**

Run: `cd desktop/frontend && export PATH="$HOME/.nvm/versions/node/v20.20.0/bin:$PATH" && npx vitest run src/components/SshHostsPanel.test.ts src/components/NewSshDialog.test.ts 2>&1 | tail`
Expected: PASS。

- [ ] **Step 3: 更新 AGENTS.md**

在切片 2 记录后追加切片 3:主机清单 E2EE 跨端同步(`desktop/ssh_sync.go` 用 account_key `SealUnsequenced` 把整个主机清单+凭据 seal 成密文,作为 prefssync value `ssh_hosts_encrypted` 同步,relay 只见密文;account_key 空时仅本地不上传;整列表 LWW)。spec `docs/superpowers/specs/2026-08-03-ssh-sync-slice3-design-draft.md`。

- [ ] **Step 4: 手动冒烟(需两台设备 + relay)**

两台桌面端登录同一账号(启用 E2EE):A 添加主机 → B 稍后 Pull 后 SshHostsPanel 出现该主机且能连;验证 relay 侧存的 value 是密文(不含明文 host/password)。

- [ ] **Step 5: 最终 commit**

```bash
git add AGENTS.md
git commit -m "docs: AGENTS.md 记录 SSH E2EE 跨端同步(切片3)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- 整个清单+凭据 seal 成一个 value → Task 2 sealSSHHosts ✅
- relay 只见密文 → Task 2/3 测试断言无明文泄漏 ✅
- account_key 空仅本地不上传 → Task 2(seal 返回 nil)+ Task 3(ReadValue ok=false)✅
- 远端 open 写回 config+keyring → Task 3 WriteValue ✅
- CRUD 触发同步 → Task 4 markSSHHostsDirty ✅
- 白名单加 key → Task 1 ✅
- 整列表 LWW(复用 prefssync)→ 无需额外代码,引擎既有 ✅

**Placeholder scan:** `sshHostsSyncSessionID` 已给定固定 UUID 值(`55480000-...-0001`);Task 3a/4 的字段名核对注(accountKeyMu / prefsMetaEntry / nowUnixMilli)是实现时确认项,非占位。

**Type consistency:** `sealSSHHosts`/`openSSHHosts`(Task 2 定义、Task 3 用)、`newAppConfigAdapter(store, accountKey)` 新签名(Task 3b 定义、3c 调用一致)、`accountKeyForSync`(Task 3a 定义、3c/测试用)、`markSSHHostsDirty`(Task 4 定义、CRUD 用)、`ssh_hosts_encrypted` key(Task 1/2/3/4 一致)、`sshCredentialService`/`SSHHost`/`sshCredential`(切片 2 复用)—— 一致。
