# 导入 `~/.ssh/config` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 从 `~/.ssh/config` 解析主机条目，用户预览勾选后导入 atterm 主机清单；识别 `ProxyJump` / `ProxyCommand` 并拒绝对这类主机直连。

**Architecture:** 解析放纯 Go 包 `internal/sshconfig`（注入 `Opener` 以便测 `Include`）；桌面端加预览/导入两个 binding 与合并语义；前端在既有 `SshHostsPanel.vue` 里加导入抽屉。

**Tech Stack:** Go + Wails v2 bindings + Vue 3 + Vitest

**Spec:** [`docs/superpowers/specs/2026-08-17-ssh-config-import-design.md`](../specs/2026-08-17-ssh-config-import-design.md)

## Global Constraints

- **带 `ProxyJump` / `ProxyCommand` 的主机导入后必须拒绝直连**，并且**不得发起任何 dial**。忽略跳板配置去直连是错的默认值：这类主机通常网络上不可直达，直连要么超时要么连上别的东西（design §5.3）。
- **不读取 `IdentityFile` 指向的私钥文件。** 只记录路径。读私钥是需要用户明确授权的动作，不能作为「导入主机清单」的副作用（design §5.2）。
- **`ProxyCommand` 永远不执行**——它是任意命令，执行等于 RCE 面。
- **导入是两步**：先 preview（含跳过原因），用户勾选后才写入。主机清单会同步到其它设备，误导入的代价不对称（design §5.1）。
- **跳过的条目必须带原因并显示出来**，不得静默丢弃。用户 config 里 20 台只导入 12 台时，另外 8 台去哪了必须看得见。
- **重名更新时保留 `KeyID` / `Tags` / `Note`**——用户在 atterm 里加的东西不该被 ssh config 覆盖。
- **导入不写 keychain，不碰任何凭据。**
- 通配块（`Host *` / `Host prod-*`）参与求值但自身不产生导入条目；求值顺序是 ssh 的 first-wins。
- 新增字段随 `ssh_hosts_encrypted` 整块 sealed JSON 同步（AAD tag `0xF0`）。**第 21 项的教训：不要假定同步链路通，要验证。**

---

### Task 1: `internal/sshconfig` 解析器

**Files:**
- Create: `internal/sshconfig/sshconfig.go`
- Create: `internal/sshconfig/sshconfig_test.go`

**Interfaces:**
- Produces:
  ```go
  type Entry struct {
      Alias, HostName, User, Port string
      IdentityFile, ProxyJump, ProxyCommand string
  }
  type Skipped struct{ Alias, Reason string }
  type Opener interface{ Open(path string) (io.ReadCloser, error) }
  func Parse(r io.Reader, base string, opener Opener) ([]Entry, []Skipped, error)
  ```
  `base` 是解析相对 `Include` 路径的根（生产传 `~/.ssh`）。

- [ ] **Step 1: 写失败的测试**

`internal/sshconfig/sshconfig_test.go`。用内存 `Opener`：

```go
type memFS map[string]string

func (m memFS) Open(p string) (io.ReadCloser, error) {
	s, ok := m[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(s)), nil
}
```

必须覆盖：

```go
func TestWildcardBlockAppliesButIsNotImported(t *testing.T) {
	cfg := "Host *\n  User deploy\n\nHost web1\n  HostName 10.0.0.1\n"
	entries, _, err := Parse(strings.NewReader(cfg), "/base", memFS{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(entries), entries)
	}
	if entries[0].Alias != "web1" || entries[0].User != "deploy" {
		t.Fatalf("wildcard User not applied: %+v", entries[0])
	}
}

func TestFirstValueWins(t *testing.T) {
	// ssh_config semantics: the FIRST obtained value for a keyword wins.
	cfg := "Host web1\n  User alice\n\nHost *\n  User deploy\n"
	entries, _, _ := Parse(strings.NewReader(cfg), "/base", memFS{})
	if entries[0].User != "alice" {
		t.Fatalf("later block overrode earlier: %+v", entries[0])
	}
}

func TestHostNameDefaultsToAlias(t *testing.T) {
	entries, _, _ := Parse(strings.NewReader("Host box\n"), "/base", memFS{})
	if entries[0].HostName != "box" {
		t.Fatalf("want HostName=box, got %q", entries[0].HostName)
	}
}

func TestMatchBlockSkippedWithReason(t *testing.T) {
	cfg := "Match host bastion\n  User root\n"
	entries, skipped, _ := Parse(strings.NewReader(cfg), "/base", memFS{})
	if len(entries) != 0 {
		t.Fatalf("Match block must not import: %+v", entries)
	}
	if len(skipped) != 1 || skipped[0].Reason == "" {
		t.Fatalf("want one skipped with a reason, got %+v", skipped)
	}
}

func TestIncludeResolvesRelativeToBase(t *testing.T) {
	fsys := memFS{"/base/extra": "Host inc1\n  HostName 10.9.9.9\n"}
	entries, _, _ := Parse(strings.NewReader("Include extra\n"), "/base", fsys)
	if len(entries) != 1 || entries[0].Alias != "inc1" {
		t.Fatalf("include not resolved: %+v", entries)
	}
}

func TestIncludeCycleIsReportedNotHung(t *testing.T) {
	fsys := memFS{"/base/a": "Include a\n"}
	_, skipped, err := Parse(strings.NewReader("Include a\n"), "/base", fsys)
	if err != nil {
		t.Fatalf("cycle must not error out: %v", err)
	}
	if len(skipped) == 0 {
		t.Fatal("cycle must produce a Skipped entry")
	}
}

func TestProxyDirectivesCaptured(t *testing.T) {
	cfg := "Host db\n  HostName 10.0.0.5\n  ProxyJump bastion\n"
	entries, _, _ := Parse(strings.NewReader(cfg), "/base", memFS{})
	if entries[0].ProxyJump != "bastion" {
		t.Fatalf("ProxyJump not captured: %+v", entries[0])
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/sshconfig/
```
Expected: 编译失败（包不存在）。

- [ ] **Step 3: 实现解析器**

要点：
- 关键词**大小写不敏感**（ssh 的行为），值大小写敏感。
- 行内 `#` 之后是注释；支持 `Key Value` 与 `Key=Value` 两种分隔。
- 值可加引号（`User "dep loy"`），去引号。
- 一个 `Host` 行可带多个模式（`Host web1 web2`）——每个具体模式各产出一个条目。
- first-wins：对每个待求值的具体别名，按文件顺序遍历所有匹配的块，**只有该关键词尚未取到值时才写入**。
- 通配匹配用 `path.Match` 语义（`*` / `?`），并处理否定模式 `!pattern`（匹配到否定即该块不适用）。
- `Include` 深度上限 16、访问过的绝对路径记入 set 防环；超限或成环产出 `Skipped`。
- glob 展开结果**排序**后处理，保证可测。

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/sshconfig/ -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): parse ssh_config with wildcard, Include and Match handling"
```

---

### Task 2: 桌面端字段、合并语义与直连闸门

**Files:**
- Modify: `desktop/ssh_hosts_store.go`（`SSHHost` 加字段）
- Create: `desktop/ssh_config_import.go`
- Create: `desktop/ssh_config_import_test.go`
- Modify: `desktop/ssh_host.go`（`NewSshSessionByID` 加代理闸门）
- Modify: `desktop/app_ssh_test.go`（闸门测试）

**Interfaces:**
- Consumes: `sshconfig.Parse` from Task 1
- Produces:
  ```go
  // SSHHost gains:
  IdentityFile string `json:"identity_file,omitempty"`
  ProxyJump    string `json:"proxy_jump,omitempty"`
  ProxyCommand string `json:"proxy_command,omitempty"`

  type SSHConfigImportPreview struct {
      Entries []SSHHost `json:"entries"`
      Skipped []struct{ Alias, Reason string } `json:"skipped"`
      Note    string `json:"note"`
  }
  func (a *App) PreviewSSHConfigImport() (SSHConfigImportPreview, error)
  func (a *App) ImportSSHHosts(hosts []SSHHost) (int, error)
  ```

- [ ] **Step 1: 写失败的测试**

`desktop/ssh_config_import_test.go`：

```go
func TestImportPreservesUserEditsOnRename(t *testing.T) {
	existing := SSHHost{
		ID: "keep-me", Alias: "web1", Host: "old.example",
		User: "old", KeyID: "key-1",
		Tags: []string{"prod"}, Note: "my note",
	}
	incoming := SSHHost{Alias: "web1", Host: "new.example", User: "new"}

	merged := mergeImportedHost(existing, incoming)

	if merged.ID != "keep-me" {
		t.Fatalf("ID must be stable, got %q", merged.ID)
	}
	if merged.Host != "new.example" || merged.User != "new" {
		t.Fatalf("config fields must update: %+v", merged)
	}
	if merged.KeyID != "key-1" {
		t.Fatalf("KeyID must survive re-import, got %q", merged.KeyID)
	}
	if len(merged.Tags) != 1 || merged.Tags[0] != "prod" {
		t.Fatalf("Tags must survive re-import: %+v", merged.Tags)
	}
	if merged.Note != "my note" {
		t.Fatalf("Note must survive re-import, got %q", merged.Note)
	}
}

func TestIdentityFileSetsKeyAuthWithoutKeyID(t *testing.T) {
	e := sshconfig.Entry{Alias: "box", HostName: "10.0.0.1", IdentityFile: "~/.ssh/id_ed25519"}
	h := hostFromEntry(e)
	if h.AuthKind != "key" {
		t.Fatalf("want auth_kind=key, got %q", h.AuthKind)
	}
	if h.KeyID != "" {
		t.Fatalf("import must not invent a KeyID, got %q", h.KeyID)
	}
	if h.IdentityFile != "~/.ssh/id_ed25519" {
		t.Fatalf("IdentityFile path must be recorded verbatim, got %q", h.IdentityFile)
	}
}
```

`desktop/app_ssh_test.go` 追加——**这条是本任务的核心断言**：

```go
func TestProxyJumpHostRefusesDirectConnect(t *testing.T) {
	a := newTestApp(t) // 沿用该文件既有的构造方式
	h := SSHHost{ID: "p1", Alias: "db", Host: "10.0.0.5", User: "root", ProxyJump: "bastion"}
	// 用该文件既有的方式把 h 放进 store

	_, err := a.NewSshSessionByID("p1")
	if err == nil {
		t.Fatal("must refuse to connect a ProxyJump host directly")
	}
	if !strings.Contains(err.Error(), "ProxyJump") {
		t.Fatalf("error must name the reason, got %v", err)
	}
}
```

> 实现者注意：`newTestApp` / store 注入方式以 `desktop/app_ssh_test.go` 里**既有**的写法为准，不要新造。若该文件没有可复用的构造，照最近的同类测试改写并在报告里说明。

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./desktop/ -run 'TestImportPreserves|TestIdentityFile|TestProxyJumpHostRefuses' -tags webkit2_41
```

- [ ] **Step 3: 加字段与合并/映射函数**

`desktop/ssh_config_import.go` 实现 `hostFromEntry`、`mergeImportedHost`、`PreviewSSHConfigImport`、`ImportSSHHosts`。

- `PreviewSSHConfigImport` 读 `~/.ssh/config`（不存在或不可读时返回**可读的错误说明**，不是空列表）；`base` 传 `~/.ssh`；把 `Entry` 映射成 `SSHHost`（不分配 ID，ID 在 `ImportSSHHosts` 里按是否重名决定）。
- `Note` 字段填一句「只导入 atterm 用得到的字段」，前端显示在预览页脚（design §7.3）。
- `ImportSSHHosts` 按 `Alias` 查既有主机：命中则 `mergeImportedHost` 后走 `UpdateSSHHost`，否则分配 `uuid.New()` 走新增路径。**都不传凭据。**
- 结束后 `markSSHHostsDirty()`。

- [ ] **Step 4: 加直连闸门**

`desktop/ssh_host.go` 的 `NewSshSessionByID`，在查到 `found` 之后、**任何凭据读取与 dial 之前**：

```go
if found.ProxyJump != "" || found.ProxyCommand != "" {
	return NewSessionResp{}, fmt.Errorf(
		"host %q needs a jump host (ProxyJump %q); jump-host support is roadmap item 27 and not implemented yet",
		found.Alias, found.ProxyJump)
}
```

位置很重要：必须在读 keychain 与 dial 之前返回，测试要能断言**没有发起连接**。

- [ ] **Step 5: 跑测试确认通过并提交**

```bash
go test ./desktop/ -tags webkit2_41
git add desktop/
git commit -m "feat(ssh): import ssh_config hosts, and refuse direct connect for proxied hosts"
```

---

### Task 3: 前端导入抽屉

**Files:**
- Modify: `desktop/frontend/src/components/SshHostsPanel.vue`
- Modify: `desktop/frontend/src/components/SshHostsPanel.test.ts`

**Interfaces:**
- Consumes: `PreviewSSHConfigImport()` / `ImportSSHHosts(hosts)` from Task 2

- [ ] **Step 1: 写失败的测试**

在 `SshHostsPanel.test.ts` 追加。**照该文件既有的 mock 方式**（不要新造 mock 框架）：

- 点「从 ~/.ssh/config 导入」→ 调用 `PreviewSSHConfigImport`，渲染出条目与跳过原因。
- **跳过原因必须出现在 DOM 里**——断言跳过条目的 alias 与 reason 文本可见。
- 默认**不全选**；不勾选直接确认时 `ImportSSHHosts` 不被调用，或以空数组调用且不写入。
- 带 `proxy_jump` 的条目渲染出可见标记（文案含「跳板」或 `ProxyJump`）。
- `PreviewSSHConfigImport` reject 时显示错误文案，不是空列表。

- [ ] **Step 2: 跑测试确认失败**

```bash
cd desktop/frontend && npm test -- --run SshHostsPanel
```

- [ ] **Step 3: 实现抽屉**

沿用面板里既有私钥导入抽屉的结构与样式（`FileUp` / `Upload` 图标已引入）。要点：

- 列表每行一个 checkbox，**默认全不选**。
- 带跳板的行显示标记与一句「需跳板，暂不可连接（第 27 项）」。
- 跳过区独立成块，逐条显示 alias + reason。
- 页脚显示后端返回的 `note`。
- 确认后调 `ImportSSHHosts(选中项)`，成功后刷新列表并提示导入条数。

- [ ] **Step 4: 跑测试确认通过**

```bash
cd desktop/frontend && npm test -- --run SshHostsPanel
```

- [ ] **Step 5: 提交**

```bash
git add desktop/frontend/src/components/
git commit -m "feat(frontend): ssh_config import drawer with skip reasons and proxy markers"
```

---

### Task 4: 同步验证与 roadmap

**Files:**
- Create: `desktop/ssh_hosts_sync_fields_test.go`
- Modify: `docs/roadmap.md`

- [ ] **Step 1: 写同步往返测试**

第 21 项的教训：新字段能不能真的同步过去，**必须验证而不是假定**。那次 relay 白名单拒收所有新键，功能断了几个月无人知晓。

写一条测试，走**真实的 seal → 同步载荷 → open** 路径（照 `desktop/ssh_sync_test.go` 与 `desktop/profiles_test.go` 里既有的写法），断言 `IdentityFile` / `ProxyJump` / `ProxyCommand` 三个字段往返后**值不丢**。

> 实现者注意：如果既有测试是直接调 `sealSSHHosts`/`openSSHHosts` 一类函数，就沿用；不要为此新造 relay 端到端框架。若发现字段其实过不去，**这是真 bug，报告出来**，不要改测试迁就。

- [ ] **Step 2: 跑测试**

```bash
go test ./desktop/ -run 'SSHHosts' -tags webkit2_41 -v
```

- [ ] **Step 3: roadmap**

第 25 项三条勾选，并**如实标注**：`ProxyJump` / `ProxyCommand` 为「已识别并阻止直连，跳板连接见第 27 项」。不要写成已支持跳板。

- [ ] **Step 4: 提交**

```bash
git add desktop/ docs/roadmap.md
git commit -m "test(ssh): prove imported proxy fields survive a sync round-trip"
```
