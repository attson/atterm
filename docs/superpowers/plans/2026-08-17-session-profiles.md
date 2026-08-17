# 会话配置档 profile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 引入命名的会话配置档（shell / 启动目录 / 环境变量 / 启动命令），新建 tab 或 split 时可选，跨桌面设备 sealed 同步，环境变量默认不出本机。

**Architecture:** profile 存 `appConfig.Profiles`，作为**单个 sealed 键** `profiles_encrypted` 走已有的 `prefssync` 管线——复用 `desktop/ssh_sync.go` 的形状（固定虚拟 session UUID 派生 key + `SealUnsequenced` + 独立 AAD tag + `accountKey` 为空时跳过同步）。会话创建时把 profile 的字段填进 `NewSessionReq`，启动命令走既有的 `SetOnFirstPrompt` 路径。

**Tech Stack:** Go 1.23 + Wails v2 + Vue 3 + TypeScript + Vitest

**Spec:** [`docs/superpowers/specs/2026-08-17-session-profiles-design.md`](../specs/2026-08-17-session-profiles-design.md)

## Global Constraints

- **新 AAD tag 是 `0xF1`。** `0xF0` 已被 SSH 主机同步占用。红线 #22：每个 sealed 命名空间必须有唯一的鉴别字节，否则不同类型的信封可以互相替换重放——**这是密码学后果，不是文档洁癖**。
- **`accountKey` 为空时绝不发明文。** `sealProfiles` 返回 `(nil, nil)`，调用方跳过同步。照抄 `ssh_sync.go` 的处理，不要自创 fallback。
- **env 合并按 profile id，不是整体替换**（design §5.1）：入站有 env → 采用；入站无 env 且本地有 → **保留本地**；本地无该 id → 新增；本地有而入站无该 id → 删除。规则的效果是 env 只会被显式同步的 env 覆盖，永远不会被空值清空。写错这条，「默认不同步」就退化成「pull 一次丢光」。
- **启动命令必须走 `desktop/relay_host.go::SetOnFirstPrompt` 直接写 PTY**，不许改成前端 `sendInput` 一次发 `"<cmd>\r"` —— Codex 把 CR 当 paste 解，这个教训在 #63 → #110 → #129 踩了三次（红线 #28）。
- **`TERM` 不允许被 profile env 覆盖。** `relay_host.go` 现在用 `terminalEnvForXterm(os.Environ())` 设置终端相关变量；profile env 覆盖 `TERM` 会破坏渲染。
- **优先级**：显式指定的 profile > 默认 profile > 现状（`default_shell` + HOME）。`default_shell`（第 21 项接的同步键）保留，它是没有 profile 时的回退，不是被取代。
- **跨进程注册点**（design §3）：`prefssync.syncedKeys`、relay `allowedPreferenceKeys`（kind `string`）、adapter 的 `ReadValue`/`WriteValue`、`isPrefCustomized`、`protocol.md` 的 AAD 表、`AGENTS.md` 红线 #22。TS 镜像**不加**（桌面专属键，理由同 `ssh_hosts_encrypted`）。relay 白名单有跨包测试守着，漏了会红。
- 前端改动要重建 `internal/relay/web-dist/`（pre-commit 钩子不看这条路径，CI 的 drift gate 会红）。
- Go 命令在仓库根跑，需 `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH`；前端命令在 `desktop/frontend/` 跑。

---

### Task 1: AAD tag 注册表与守卫

**先做这个**，因为它给后面三个任务提供 `0xF1` 的落点，并把「漏登记 AAD tag」这类错误从人工纪律变成测试。

现状：AAD 鉴别字节来自两处——`internal/proto` 的帧类型常量（`0x03`/`0x05`/`0x12`/`0x35`/`0x37`/`0x38`/`0x39`/`0x3a`），以及合成的仅绑 AAD 标签（`desktop/ssh_sync.go` 的 `sshSyncFrameType = 0xF0`）。**没有任何一处集中登记**，所以今天写不出守卫。

**Files:**
- Create: `internal/e2eecrypto/aadtags.go`
- Create: `internal/e2eecrypto/aadtags_test.go`
- Modify: `desktop/ssh_sync.go`（`sshSyncFrameType` 改引用注册表）

**Interfaces:**
- Produces: `e2eecrypto.AADTags map[byte]string`、`e2eecrypto.AADTagSSHHosts`、`e2eecrypto.AADTagProfiles`

- [ ] **Step 1: 写失败的测试**

`internal/e2eecrypto/aadtags_test.go`：

```go
func TestAADTagsAreUnique(t *testing.T) {
	seen := map[string]byte{}
	for b, name := range AADTags {
		if prev, dup := seen[name]; dup {
			t.Errorf("name %q used for both 0x%02x and 0x%02x", name, prev, b)
		}
		seen[name] = b
	}
	// map keys are bytes, so duplicate bytes are impossible by construction —
	// that is the point of keying by byte rather than listing pairs.
}

func TestAADTagsMatchProtocolDoc(t *testing.T) {
	// The registry is the code's view; docs/spec/protocol.md's sealed-envelope
	// table is what a reader (and redline #22) treats as authoritative. If they
	// diverge, a new sealed namespace can silently reuse a discriminator byte,
	// which is what lets one envelope type be replayed in another's place.
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "spec", "protocol.md"))
	if err != nil {
		t.Fatalf("read protocol.md: %v", err)
	}
	documented := map[byte]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\|\\s*`0x([0-9a-fA-F]{2})`").FindAllSubmatch(raw, -1) {
		v, err := strconv.ParseUint(string(m[1]), 16, 8)
		if err != nil {
			t.Fatalf("bad byte %q in protocol.md: %v", m[1], err)
		}
		documented[byte(v)] = true
	}
	if len(documented) == 0 {
		t.Fatal("parsed zero bytes out of protocol.md — the table format changed, fix this regex rather than deleting the test")
	}
	for b, name := range AADTags {
		if !documented[b] {
			t.Errorf("AAD tag 0x%02x (%s) is used in code but absent from protocol.md's sealed envelope table — add a row there (redline #22)", b, name)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/e2eecrypto/ -run TestAADTags`
Expected: FAIL —— `AADTags` 未定义。

- [ ] **Step 3: 建注册表**

> **事后订正（final fix wave，见 progress.md F7）：** 下面这份清单漏了两个已经在用的字节——`0x02`
> `IN`（`desktop/uplink_frame_seal.go` 里 `byte(f.Type)` 变量取值，不是字面量，第一轮 grep 没解析出来）
> 和 `0x33` `PASTE_IMAGE`（`desktop/uplink_open_test.go` 有往返测试）。代码里的 `aadtags.go` 已经
> 补上这两行；这里的快照特意保持原样不改，连同下面 protocol.md 片段一起，作为「枚举类清单第一遍
> 几乎总漏」这条教训的记录——照抄这份快照去建下一个 sealed 命名空间的人，应该先读这条订正。

`internal/e2eecrypto/aadtags.go`：

```go
package e2eecrypto

// AAD discriminator bytes for sealed envelopes.
//
// Every sealed namespace gets a unique byte. It is mixed into the AEAD's
// additional data alongside the session UUID, which is what stops an envelope
// of one type being replayed in place of another (AGENTS.md redline #22).
//
// Most values are the protocol frame type that carries the envelope. Two are
// synthetic: they never appear on the relay wire and exist only to give a
// preference-sync payload its own discriminator.
//
// Adding a namespace means adding it here AND adding a row to
// docs/spec/protocol.md's sealed-envelope table — aadtags_test.go fails if the
// two disagree.
const (
	AADTagOut          byte = 0x03
	AADTagMeta         byte = 0x05
	AADTagListResp     byte = 0x12
	AADTagCommandEvent byte = 0x35
	AADTagPasteFile    byte = 0x37
	AADTagFSRequest    byte = 0x38
	AADTagFSResponse   byte = 0x39
	AADTagFSEvent      byte = 0x3a

	// Synthetic, AAD-only — no wire frame carries these.
	AADTagSSHHosts byte = 0xF0
	AADTagProfiles byte = 0xF1
)

var AADTags = map[byte]string{
	AADTagOut:          "OUT",
	AADTagMeta:         "META",
	AADTagListResp:     "LIST_RESP",
	AADTagCommandEvent: "COMMAND_EVENT",
	AADTagPasteFile:    "PASTE_FILE",
	AADTagFSRequest:    "FS_REQUEST",
	AADTagFSResponse:   "FS_RESPONSE",
	AADTagFSEvent:      "FS_EVENT",
	AADTagSSHHosts:     "ssh_hosts_encrypted sync",
	AADTagProfiles:     "profiles_encrypted sync",
}
```

- [ ] **Step 4: 补 protocol.md 的两行**

在 §E2EE 信封表（约 612-617 行）后补：

```markdown
| `0xF0` （合成，不上 wire） | `ssh_hosts_encrypted` 偏好值 | JSON `sshSyncPayload { hosts, keys }` |
| `0xF1` （合成，不上 wire） | `profiles_encrypted` 偏好值 | JSON `profilesSyncPayload { profiles }` |
```

若表格的列格式与上面不同，照抄既有行的列数与写法。

- [ ] **Step 5: `ssh_sync.go` 改引用注册表**

把 `const sshSyncFrameType = 0xF0` 换成使用 `e2eecrypto.AADTagSSHHosts`，删掉本地常量。行为不变，但从此只有一个真值来源。

- [ ] **Step 6: 跑测试并提交**

Run: `go vet ./... && go test ./internal/e2eecrypto/ ./desktop/ -timeout 180s`

```bash
git add internal/e2eecrypto/aadtags.go internal/e2eecrypto/aadtags_test.go \
        desktop/ssh_sync.go docs/spec/protocol.md
git commit -m "feat(e2ee): centralise AAD discriminator bytes and pin them to the spec"
```

---

### Task 2: profile 模型、seal/open 与合并

**Files:**
- Create: `desktop/profiles.go`（模型 + seal/open + 合并）
- Create: `desktop/profiles_test.go`
- Modify: `desktop/config.go`（`Profiles` + `DefaultProfileID` 字段）
- Modify: `desktop/app.go`（CRUD 绑定）

**Interfaces:**
- Consumes: Task 1 的 `e2eecrypto.AADTagProfiles`
- Produces:
  - `SessionProfile` struct（字段见 design §4）
  - `sealProfiles(accountKey []byte, profiles []SessionProfile) (json.RawMessage, error)`（`accountKey` 空 → `(nil, nil)`）
  - `openProfiles(accountKey []byte, blob json.RawMessage) ([]SessionProfile, error)`
  - `mergeProfiles(local, incoming []SessionProfile) []SessionProfile`（§5.1 的四条规则）
  - `App.GetProfiles() []SessionProfile` / `SetProfiles([]SessionProfile) error` / `GetDefaultProfileID() string` / `SetDefaultProfileID(string) error`

- [ ] **Step 1: 写失败的测试 —— 合并规则四条分支**

`desktop/profiles_test.go`：

```go
func TestMergeProfilesEnvRules(t *testing.T) {
	env := map[string]string{"FOO": "bar"}

	t.Run("incoming env wins when present", func(t *testing.T) {
		local := []SessionProfile{{ID: "a", Name: "A", Env: map[string]string{"OLD": "1"}}}
		incoming := []SessionProfile{{ID: "a", Name: "A", Env: env}}
		got := mergeProfiles(local, incoming)
		if got[0].Env["FOO"] != "bar" || got[0].Env["OLD"] != "" {
			t.Errorf("incoming env must replace local: %v", got[0].Env)
		}
	})

	t.Run("local env survives when incoming has none", func(t *testing.T) {
		local := []SessionProfile{{ID: "a", Name: "A", Env: env}}
		incoming := []SessionProfile{{ID: "a", Name: "A-renamed"}}
		got := mergeProfiles(local, incoming)
		if got[0].Env["FOO"] != "bar" {
			t.Error("an unsynced env must not be cleared by a pull that carries none")
		}
		if got[0].Name != "A-renamed" {
			t.Error("non-env fields must still take the incoming value")
		}
	})

	t.Run("profile absent locally is added", func(t *testing.T) {
		got := mergeProfiles(nil, []SessionProfile{{ID: "b", Name: "B"}})
		if len(got) != 1 || got[0].ID != "b" {
			t.Errorf("got %v", got)
		}
	})

	t.Run("profile absent from incoming is deleted", func(t *testing.T) {
		local := []SessionProfile{{ID: "a"}, {ID: "b"}}
		got := mergeProfiles(local, []SessionProfile{{ID: "a"}})
		if len(got) != 1 || got[0].ID != "a" {
			t.Errorf("a profile deleted on the other machine must go away here too: %v", got)
		}
	})
}

func TestSealProfilesSkipsWithoutAccountKey(t *testing.T) {
	blob, err := sealProfiles(nil, []SessionProfile{{ID: "a"}})
	if err != nil || blob != nil {
		t.Fatalf("no account key must mean skip-sync, never plaintext: blob=%v err=%v", blob, err)
	}
}

// 事后订正（final fix wave，见 progress.md F8/F18）：下面这个测试与 Step 3
// 的说明自相矛盾——Step 3 说 seal 之前要清空未开 SyncEnv 的 profile 的
// Env，但这里种下的 profile 没有 SyncEnv 字段（也就是 false）却断言 Env 在
// seal 之后还在。原实现的作者按这个测试的字面意思去做，把 stripUnsyncedEnv
// 写成一个独立、非强制调用的函数，直到复核时才发现矛盾。最终定案是倒过来
// 判的：sealProfiles 内部无条件裁剪，这个测试本身错了。代码里
// `desktop/profiles_test.go` 的 `TestSealProfilesRoundTrip` 已经改成显式设
// `SyncEnv: true` 来保 Env 存活，并新增了 `TestSealProfilesStripsEnvWhenSyncEnvFalse`
// 盯住相反的分支。这份快照保留原样、不回填，留作「测试代码本身可能是错的
// 那一半」的例子。
func TestSealProfilesRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	in := []SessionProfile{{ID: "a", Name: "Work", Shell: "/bin/zsh", Env: map[string]string{"K": "V"}}}
	blob, err := sealProfiles(key, in)
	if err != nil || blob == nil {
		t.Fatalf("seal: %v", err)
	}
	out, err := openProfiles(key, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(out) != 1 || out[0].Name != "Work" || out[0].Env["K"] != "V" {
		t.Errorf("round trip lost data: %+v", out)
	}
}

func TestOpenProfilesRejectsWrongAADTag(t *testing.T) {
	// Sealing under the SSH namespace and opening under the profiles one must
	// fail — that is the whole point of a per-namespace discriminator byte.
	key := make([]byte, 32)
	sealed, err := sealSSHHosts(key, nil, nil, nil, nil)
	if err != nil || sealed == nil {
		t.Skip("ssh seal unavailable in this shape")
	}
	if _, err := openProfiles(key, sealed); err == nil {
		t.Error("an ssh_hosts envelope must not open as a profiles envelope")
	}
}
```

- [ ] **Step 2: 跑测试确认失败** —— `go test ./desktop/ -run 'TestMergeProfiles|TestSealProfiles|TestOpenProfiles'`，编译错误。

- [ ] **Step 3: 实现模型与 seal/open**

`desktop/profiles.go`。`SessionProfile` 字段照 design §4。seal/open 照抄 `desktop/ssh_sync.go` 的结构：固定虚拟 UUID（新生成一个，注释写明「不要改，改了旧密文打不开」）+ `e2eecrypto.DeriveSessionKey` + `SealUnsequenced(sessionKey, uuid, e2eecrypto.AADTagProfiles, plain)`。

seal 之前对每个 `SyncEnv == false` 的 profile 拷贝一份并清空 `Env`——**不要就地改传入的切片**，那会把用户本机的 env 也抹掉。

- [ ] **Step 4: 实现 `mergeProfiles`** 按 §5.1 四条规则，以入站顺序为准输出。

- [ ] **Step 5: config 字段与 CRUD 绑定**

`appConfig` 加 `Profiles []SessionProfile` 与 `DefaultProfileID string`（都 `omitempty`）。`app.go` 加四个绑定，写入走 `a.updatePref("profiles_encrypted", ...)`；`SetProfiles` 校验：ID 非空且唯一、Name 非空。`detachMaps` 里给每个 profile 的 `Env` map 做深拷贝（红线：`configStore.Get()` 返回浅拷贝）。

- [ ] **Step 6: 跑测试并提交**

Run: `go vet ./... && go test ./desktop/ -timeout 180s`

```bash
git add desktop/profiles.go desktop/profiles_test.go desktop/config.go desktop/app.go
git commit -m "feat(desktop): session profile model with sealed sync and env merge"
```

---

### Task 3: 接入 prefssync 并应用到会话创建

**Files:**
- Modify: `internal/prefssync/sync.go`（`syncedKeys`）
- Modify: `internal/userstore/preferences.go`（`allowedPreferenceKeys`）
- Modify: `desktop/prefssync_adapter.go`（`ReadValue`/`WriteValue`，写侧走 `mergeProfiles`）
- Modify: `desktop/app.go`（`isPrefCustomized`）
- Modify: `desktop/relay_host.go`（应用 profile：shell / cwd / env / 启动命令）
- Test: `desktop/prefssync_adapter_test.go`、`desktop/profiles_test.go`

**Interfaces:**
- Consumes: Task 2 的 `sealProfiles` / `openProfiles` / `mergeProfiles`

- [ ] **Step 1: 写失败的测试**

- adapter 往返：`ReadValue("profiles_encrypted")` 在无 `accountKey` 时返回 `(nil, false)`；有 key 时返回密文，`WriteValue` 能开回来。
- `isPrefCustomized("profiles_encrypted")`：无 profile → false；有 → true。
- **`WriteValue` 走合并而非替换**：本地 profile 有 env、入站没有 → env 存活。
- 优先级：`NewSessionReq` 指定 profile → 用它的 shell/cwd；未指定但有默认 profile → 用默认的；都没有 → `default_shell` + HOME。
- `TERM` 保护：profile env 含 `TERM` 时最终 env 里的 `TERM` 仍是 `terminalEnvForXterm` 设的值。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 注册点接入**

`syncedKeys` 加 `"profiles_encrypted"`；relay `allowedPreferenceKeys` 加 `"profiles_encrypted": preferenceKindString`（sealed blob 是 base64 字符串，与 `ssh_hosts_encrypted` 同 kind）。

**跨包测试 `preferences_synced_keys_test.go` 会自动检查这两处是否一致**——如果只加了一处，它会红并点名。这是第 21 项那次事故补的守卫，本项是它第一次真正派上用场。

adapter 加 case；`WriteValue` 侧必须 `openProfiles` → `mergeProfiles(local, incoming)` → 写回，**不是直接替换**。

- [ ] **Step 4: 会话创建应用 profile**

`NewSessionReq` 加 `ProfileID string`。`relay_host.go::NewSession` 里：解析 profile（显式 > 默认 > 无）→ shell 覆盖 `Command`、cwd 覆盖 `Cwd`、env 合并进 `terminalEnvForXterm(os.Environ())` 的结果（**profile 优先，但 `TERM` 保护**）。

启动命令走既有的 `SetOnFirstPrompt`（红线 #28），**不要**在前端发。

- [ ] **Step 5: 跑测试并提交**

Run: `go vet ./... && go test ./... -timeout 300s`

```bash
git add internal/prefssync/sync.go internal/userstore/preferences.go \
        desktop/prefssync_adapter.go desktop/prefssync_adapter_test.go \
        desktop/app.go desktop/relay_host.go desktop/profiles_test.go
git commit -m "feat(prefs): sync session profiles and apply them at session creation"
```

---

### Task 4: 前端 —— profile 管理与新建会话选择

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`（四个包装）
- Create: `desktop/frontend/src/components/SettingsProfiles.vue` + 测试
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`（挂新面板）
- Modify: `desktop/frontend/src/App.vue`（新建 tab / split 时带 profile）
- Modify: `docs/roadmap.md`、`internal/relay/web-dist/`

- [ ] **Step 1: api 包装**（照 `getTerminalThemePreference` 的一行风格）

- [ ] **Step 2: 写失败的测试**

`SettingsProfiles.test.ts`：列出 profile、新增、编辑、删除、设默认；`sync_env` 开关默认关且 UI 说明「关闭时环境变量只留在这台机器」。沿用 `SettingsTerminalAppearance.test.ts` 的 mount 与 mock 约定（先读那个文件）。

- [ ] **Step 3: 实现面板** —— 参照 `SettingsTerminalAppearance.vue` 的结构（该项目一节一个组件）。

- [ ] **Step 4: 新建会话时选 profile** —— tab/split 的新建入口加 profile 选择（默认用默认 profile），把 `profileId` 传进 `NewSessionReq`。

- [ ] **Step 5: 验证** —— `npm test && npm run build`

- [ ] **Step 6: 重建 web 产物** —— 仓库根 `nvm use 20 && ./scripts/build-web.sh`

- [ ] **Step 7: 勾 roadmap** —— 第 22 项的复选框；同时把 Backlog 里「默认 shell 设置改进」「启动目录设置」「环境变量设置」三条标注为已被第 22 项覆盖。

- [ ] **Step 8: 手动验证**（能跑 GUI 才做，跑不了如实报告未执行）

1. 建 profile（指定 shell + cwd + 启动命令）→ 新开 tab，三者都生效
2. 设为默认 → 不选 profile 新开 tab 也生效
3. profile env 里放 `TERM=dumb` → 终端渲染正常，`TERM` 未被覆盖
4. 关闭 `sync_env` 的 profile → 两台机器间 env 不传播，其余字段传播

- [ ] **Step 9: 提交**
