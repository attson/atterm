# 客户端连接路由到 home 节点实现计划(阶段二 · 子项目 C1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 客户端读登录响应的 `home_instance_url`,把有状态 WS(桌面 `/uplink`、移动/Web `/client`)路由到该节点;登录/API 仍走配置入口域名;空 home 回退到现状。

**Architecture:** e2eeclient `LoginResult` 增 `HomeInstanceURL`(SDK 解析);桌面用纯函数 `uplinkDialURL` 从 home(http(s)→ws(s))或回退 `RelayURL` 算出 uplink dial URL;Web `RelayConfig` 增 `homeInstanceURL`,`wsUrl` 优先按它路由 `/client`;移动 `capacitor.ts` 解析并存 `home_instance_url`。E2EE 已按 realm 锚定(A),换节点域名不破解密。

**Tech Stack:** Go(e2eeclient + desktop)、TS(web/移动 Vue + Capacitor)。

## Global Constraints

- **依赖 B(已栈在 B 之上)**:relay 登录 finalize 已返回 `home_instance_url`(opaque_auth.go:136);`GET /api/nodes`、`PUT /api/me/home` 已存在(C2 用)。
- **只路由有状态 WS** 到 home(桌面 /uplink、移动/Web /client);登录/API 仍走入口域名。
- **空 home 回退**:`home_instance_url` 为空(注册 / 单机未配 / B 返回死 home 的空)→ 桌面用 `cfg.RelayURL`、web/移动用 `baseURL`/`location`。单机零变化。
- **注册路径暂不下发 home**:relay `registerFinalizeResponse` 无 home;只有 `LoginResult` 增 `HomeInstanceURL`,`RegisterResult` 不增。
- **http(s)→ws(s) 转换**:home 是 `https://host` 形式的 public_url;dial 用 `wss://host/<path>`。
- **不改 relay**(C1 纯客户端 + e2eeclient SDK)。Go 1.23.0 不变;前端 vue-tsc 干净。

---

## File Structure

- `internal/e2eeclient/client.go`(修改):`LoginResult.HomeInstanceURL` + SDK wire + Login 解析
- `internal/e2eeclient/client_test.go`(修改):往返断言 home
- `desktop/uplink_dial.go`(新增):`uplinkDialURL` 纯函数;`desktop/uplink_dial_test.go`(新增)
- `desktop/config.go`(修改):`RelayHomeInstanceURL`
- `desktop/app.go`(修改):`applyRelayUplink` 用 `uplinkDialURL`;`LoginRemoteRelay` 设 home
- `web/src/shared/api/relay-config.ts`(修改):`homeInstanceURL` 字段 + load 保留
- `web/src/shared/ws/client-conn.ts`(修改):`wsUrl` 按 home 路由 + `wsFromHttpURL` 工具
- web 登录解析处(修改):存 `home_instance_url`(`web/src/shared/api/auth.ts` 的 `persistSession` 链路)
- `desktop/frontend/src/platform/capacitor.ts`(修改):`opaqueLogin` 解析并存 home

---

### Task 1: e2eeclient LoginResult.HomeInstanceURL

**Files:**
- Modify: `internal/e2eeclient/client.go`
- Test: `internal/e2eeclient/client_test.go`

**Interfaces:**
- Produces: `e2eeclient.LoginResult.HomeInstanceURL string`(桌面登录路径 res.HomeInstanceURL 可读)。

- [ ] **Step 1: 写失败测试(扩展现有往返测试)**

Find the existing register+login round-trip test in `internal/e2eeclient/client_test.go` (the one asserting `lg.RealmID == "test-realm"`). The test relay is built with `NewOpaqueAuthHandler(store, opaqueSrv, "", "test-realm", "")` — change the 5th arg (instancePublicURL) to a non-empty value `"https://node-1.example"` so the login resolves a home, then after the successful `Login`, add:
```go
	if lg.HomeInstanceURL != "https://node-1.example" {
		t.Fatalf("login HomeInstanceURL = %q, want https://node-1.example", lg.HomeInstanceURL)
	}
```
(The relay auto-assigns the serving node as home on first login — see subproject B `resolveHomeInstanceURL` — so with instancePublicURL `"https://node-1.example"` the login response's `home_instance_url` is that value.)

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/e2eeclient/ -run TestClient -v`
Expected: FAIL（`lg.HomeInstanceURL` 为空,因 SDK 未解析）或编译失败（`HomeInstanceURL` 未定义）。

- [ ] **Step 3: 实现**

In `internal/e2eeclient/client.go`:
- Add `HomeInstanceURL string` to `LoginResult` (after `RealmID` at ~line 102):
  ```go
  type LoginResult struct {
  	UserID          string
  	SessionToken    string
  	Email           string
  	AccountKey      []byte // 32 bytes
  	RealmID         string
  	HomeInstanceURL string
  }
  ```
- Add `HomeInstanceURL string json:"home_instance_url"` to the SDK's wire `loginFinalizeResponse` struct (~line 435, after its `RealmID` field):
  ```go
  type loginFinalizeResponse struct {
  	UserID         string         `json:"user_id"`
  	SessionToken   string         `json:"session_token"`
  	AccountKeyWrap accountKeyWrap `json:"account_key_wrap"`
  	RealmID        string         `json:"realm_id"`
  	HomeInstanceURL string        `json:"home_instance_url"`
  }
  ```
  (Match the existing field types/names in that struct — `accountKeyWrap` may be a different type name; only ADD the `HomeInstanceURL` line.)
- In the `Login` result construction (~line 237-241), add `HomeInstanceURL: finResp.HomeInstanceURL`:
  ```go
  	return &LoginResult{
  		UserID:          finResp.UserID,
  		SessionToken:    finResp.SessionToken,
  		AccountKey:      accountKey,
  		RealmID:         finResp.RealmID,
  		HomeInstanceURL: finResp.HomeInstanceURL,
  	}, nil
  ```
  Do NOT touch the Register path / `RegisterResult` (register doesn't return home).

- [ ] **Step 4: 运行,确认通过**

Run: `GOTOOLCHAIN=local go test ./internal/e2eeclient/ -run TestClient -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/e2eeclient/client.go internal/e2eeclient/client_test.go
git commit -m "feat(e2eeclient): parse home_instance_url into LoginResult"
```

---

### Task 2: 桌面 uplink 路由到 home

**Files:**
- Create: `desktop/uplink_dial.go`、`desktop/uplink_dial_test.go`
- Modify: `desktop/config.go`、`desktop/app.go`

**Interfaces:**
- Consumes: `e2eeclient.LoginResult.HomeInstanceURL`(Task 1,经桌面 relay client 的 `res`)。
- Produces: `uplinkDialURL(homeInstanceURL, relayURL string) string`;config `RelayHomeInstanceURL`。

- [ ] **Step 1: 写失败测试**

Create `desktop/uplink_dial_test.go`:
```go
package main

import "testing"

func TestUplinkDialURL(t *testing.T) {
	cases := []struct {
		home, relay, want string
	}{
		{"", "wss://relay.example", "wss://relay.example"},                      // empty home → relayURL
		{"https://node-1.example", "wss://relay.example", "wss://node-1.example"}, // https → wss
		{"http://node-2.example", "wss://relay.example", "ws://node-2.example"},   // http → ws
		{"wss://node-3.example", "wss://relay.example", "wss://node-3.example"},   // already wss
		{"node-4.example", "wss://relay.example", "wss://node-4.example"},         // bare host → wss
		{"https://node-5.example/", "wss://relay.example", "wss://node-5.example"}, // trailing slash trimmed
	}
	for _, c := range cases {
		if got := uplinkDialURL(c.home, c.relay); got != c.want {
			t.Errorf("uplinkDialURL(%q, %q) = %q, want %q", c.home, c.relay, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./desktop/ -run TestUplinkDialURL -v`
Expected: 编译失败（`undefined: uplinkDialURL`）。

- [ ] **Step 3: 实现 uplinkDialURL**

Create `desktop/uplink_dial.go`:
```go
package main

import "strings"

// uplinkDialURL returns the ws(s) base URL to dial for /uplink. When the user
// has a home instance (multi-instance node selection), the stateful uplink
// routes there: the home is an http(s) public URL, converted to ws(s). When
// empty (single-instance / register / dead-home), it falls back to relayURL
// (already ws/wss). The trailing "/uplink" is appended by the uplink dialer.
func uplinkDialURL(homeInstanceURL, relayURL string) string {
	h := strings.TrimRight(strings.TrimSpace(homeInstanceURL), "/")
	if h == "" {
		return relayURL
	}
	switch {
	case strings.HasPrefix(h, "https://"):
		return "wss://" + strings.TrimPrefix(h, "https://")
	case strings.HasPrefix(h, "http://"):
		return "ws://" + strings.TrimPrefix(h, "http://")
	case strings.HasPrefix(h, "wss://"), strings.HasPrefix(h, "ws://"):
		return h
	default:
		return "wss://" + h
	}
}
```

- [ ] **Step 4: config 字段 + applyRelayUplink + login 接线**

In `desktop/config.go`, add to `appConfig` (after `RelayRealmID` at ~line 65):
```go
	// RelayHomeInstanceURL is the user's home relay node for this realm
	// (from the login response home_instance_url). The stateful /uplink WS
	// dials this node; empty falls back to RelayURL (single-instance).
	RelayHomeInstanceURL string `json:"relay_home_instance_url,omitempty"`
```
In `desktop/app.go` `applyRelayUplink` (~line 374-404), change the `newUplink` line to dial the derived URL:
```go
	dialURL := uplinkDialURL(cfg.RelayHomeInstanceURL, cfg.RelayURL)
	a.uplink = newUplink(dialURL, cfg.RelaySessionToken, cfg.RemotePermissionOrDefault(), a.host, a.recordRelayError, a.agentSealAccountKey, cfg.AllowInsecureRelay)
	go a.uplink.Run(uplinkCtx)
	log.Printf("desktop: uplink configured for %s", dialURL)
```
(Keep the existing guards and `validateRelayEndpoint(cfg.RelayURL, ...)` unchanged — validation gates on the configured URL; the home is server-provided.)
In `desktop/app.go` `LoginRemoteRelay` (~line 589-590), set the home beside the realm:
```go
		cfg.RelaySessionUserID = res.UserID
		cfg.RelayRealmID = res.RealmID
		cfg.RelayHomeInstanceURL = res.HomeInstanceURL
```
(Do NOT change `RegisterRemoteRelay` ~line 676-677 — `RegisterResult` has no `HomeInstanceURL`; register leaves `RelayHomeInstanceURL` empty → fallback.)

- [ ] **Step 5: 运行 + 回归 + 提交**

Run:
```bash
GOTOOLCHAIN=local go test ./desktop/ -run TestUplinkDialURL -v
GOTOOLCHAIN=local go build ./... && GOTOOLCHAIN=local go test ./desktop/
```
Expected: PASS（`desktop/hookinstall` 预存基线失败除外）。

```bash
git add desktop/uplink_dial.go desktop/uplink_dial_test.go desktop/config.go desktop/app.go
git commit -m "feat(desktop): route uplink to home instance when set"
```

---

### Task 3: Web /client 路由到 home

**Files:**
- Modify: `web/src/shared/api/relay-config.ts`、`web/src/shared/ws/client-conn.ts`、`web/src/shared/api/auth.ts`
- Test: 相应 `*.test.ts`（沿用现有前端测试框架）

**Interfaces:**
- Consumes: relay 登录 finalize 的 `home_instance_url`。
- Produces: `RelayConfig.homeInstanceURL?: string`;`wsUrl(path)` 按 home 路由。

- [ ] **Step 1: RelayConfig 加字段 + load 保留**

In `web/src/shared/api/relay-config.ts`:
- Add to the `RelayConfig` interface (after `realmId?: string`):
  ```ts
  // homeInstanceURL is the user's home relay node for this realm (from the
  // login response home_instance_url). The stateful /client WS routes here
  // when set; empty falls back to baseURL/location. Not written by register.
  homeInstanceURL?: string
  ```
- In `loadRelayConfig`'s returned object (where `realmId` is conditionally spread), add the same pattern:
  ```ts
      ...(parsed.realmId !== undefined ? { realmId: parsed.realmId } : {}),
      ...(parsed.homeInstanceURL !== undefined ? { homeInstanceURL: parsed.homeInstanceURL } : {}),
  ```
  (`saveRelayConfig` is `JSON.stringify(cfg)` — it persists the field automatically.)

- [ ] **Step 2: wsUrl 按 home 路由 + 工具函数 + 测试**

In `web/src/shared/ws/client-conn.ts`, add a helper and route `wsUrl` via home. Replace the existing `wsUrl` function with:
```ts
// wsFromHttpURL converts an http(s) base URL to a ws(s) URL for the given path.
function wsFromHttpURL(httpURL: string, path: string): string {
  const u = new URL(httpURL)
  const proto = u.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${u.host}${path}`
}

export function wsUrl(path: string): string {
  const cfg = loadRelayConfig()
  // Route the stateful WS to the user's home node when set (multi-instance).
  if (cfg?.homeInstanceURL) {
    return wsFromHttpURL(cfg.homeInstanceURL, path)
  }
  if (isMobileApp()) {
    const baseStr = cfg?.baseURL
    if (!baseStr) throw new ApiError(0, 'relay_not_configured', null)
    return wsFromHttpURL(baseStr, path)
  }
  if (typeof location === 'undefined') return `ws://localhost${path}`
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}${path}`
}
```
(Confirm `loadRelayConfig`, `isMobileApp`, `ApiError` are already imported in this file — they are, since the current `wsUrl` uses them. Keep imports as-is.)
Add a test next to the existing client-conn tests (find the test file via `ls web/src/shared/ws/*.test.ts`; if none, create `web/src/shared/ws/client-conn.test.ts`):
```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { wsUrl } from './client-conn'
import { saveRelayConfig, clearRelayConfig } from '@shared/api/relay-config'

describe('wsUrl home routing', () => {
  beforeEach(() => clearRelayConfig())

  it('routes to homeInstanceURL when set', () => {
    saveRelayConfig({ baseURL: 'https://relay.example', sessionToken: 't', expiresAt: null, allowInsecure: false, homeInstanceURL: 'https://node-1.example' })
    expect(wsUrl('/client')).toBe('wss://node-1.example/client')
  })

  it('falls back to baseURL/location when home unset', () => {
    saveRelayConfig({ baseURL: 'https://relay.example', sessionToken: 't', expiresAt: null, allowInsecure: false })
    // In jsdom, location.host drives the non-mobile branch; assert it does NOT use a home node.
    expect(wsUrl('/client').endsWith('/client')).toBe(true)
    expect(wsUrl('/client')).not.toContain('node-1.example')
  })
})
```
(Adjust the import paths / test runner imports to match the project's existing web test files — check an existing `web/src/**/*.test.ts` for the exact `vitest` import style and path aliases.)

- [ ] **Step 3: 登录存 home_instance_url**

In `web/src/shared/api/auth.ts`, find the login finalize parse (where `realm_id` is read into the config — the `persistSession` call after a successful login). The login finalize response type (`LoginFinalizeResp` or similar) needs `home_instance_url: string`; thread it into the stored config:
- Extend the parsed response type to include `home_instance_url?: string`.
- In `persistSession` (which already takes an optional `realmId`), add an optional `homeInstanceURL` param and spread it conditionally (mirror the `realmId` handling added in subproject A):
  ```ts
  function persistSession(sessionToken: string, realmId?: string, homeInstanceURL?: string): void {
    const existing = loadRelayConfig()
    const effectiveRealmId = realmId ?? existing?.realmId
    const effectiveHome = homeInstanceURL ?? existing?.homeInstanceURL
    saveRelayConfig({
      baseURL: existing?.baseURL ?? '',
      allowInsecure: existing?.allowInsecure ?? false,
      sessionToken,
      expiresAt: null,
      ...(effectiveRealmId !== undefined ? { realmId: effectiveRealmId } : {}),
      ...(effectiveHome !== undefined ? { homeInstanceURL: effectiveHome } : {}),
    })
  }
  ```
- At the login call site, pass `final.home_instance_url` as the third arg (where `final.realm_id` is the second).

- [ ] **Step 4: typecheck + test + 提交**

Run the web typecheck + tests (check `web/package.json` scripts — likely `npm run -s typecheck` / `npx vue-tsc --noEmit` and `npx vitest run` in `web/`):
```bash
cd web && npx vue-tsc --noEmit && npx vitest run src/shared/ws/ ; cd ..
```
Expected: typecheck clean; wsUrl home-routing test PASS.

```bash
git add web/src/shared/api/relay-config.ts web/src/shared/ws/client-conn.ts web/src/shared/api/auth.ts web/src/shared/ws/client-conn.test.ts
git commit -m "feat(web): route /client WS to home instance when set"
```

---

### Task 4: 移动解析并存 home_instance_url

**Files:**
- Modify: `desktop/frontend/src/platform/capacitor.ts`

**Interfaces:**
- Consumes: relay 登录 finalize 的 `home_instance_url`。
- Produces: 移动端把 `home_instance_url` 存入 `wsUrl` 读取的 relay 配置(`homeInstanceURL`)。

- [ ] **Step 1: opaqueLogin 解析 + 返回 home**

In `desktop/frontend/src/platform/capacitor.ts`:
- Extend `opaqueLogin`'s `finBody` cast (~line 108-114) and return type (~line 57) + return value (~line 115) to include `home_instance_url: string`:
  ```ts
  // finBody cast:
  account_key_wrap: AccountKeyWrap
  realm_id: string
  home_instance_url: string
  // ...
  // return:
  return { user_id: finBody.user_id, session_token: finBody.session_token, account_key: accountKey, realm_id: finBody.realm_id, home_instance_url: finBody.home_instance_url }
  ```

- [ ] **Step 2: 把 home 存进 wsUrl 读取的配置**

VERIFY the storage path: `web/src/shared/ws/client-conn.ts` `wsUrl` reads `loadRelayConfig()` (localStorage `atterm.relay`). The mobile login flow (`relay.login` in capacitor.ts, ~line 373-392) persists relay config — trace where it writes the config that `wsUrl` reads, and add `homeInstanceURL: result.home_instance_url` there (mirroring how `realmId` was stored in subproject A). If the mobile flow writes to a different store than `loadRelayConfig` reads, ensure `homeInstanceURL` lands where `wsUrl`/`loadRelayConfig` will see it (the same `RelayConfig` blob in localStorage). Do NOT change `ACCOUNT_KEY_KEY` / account-key storage.

- [ ] **Step 3: typecheck + 提交**

Run the desktop/frontend typecheck (`desktop/frontend` — `npx vue-tsc --noEmit` or the package's typecheck script):
```bash
cd desktop/frontend && npx vue-tsc --noEmit ; cd ../..
```
Expected: no type errors from the added `home_instance_url`/`homeInstanceURL` fields.

```bash
git add desktop/frontend/src/platform/capacitor.ts
git commit -m "feat(mobile): parse and store home_instance_url for /client routing"
```

---

## 收尾

- [ ] 全量 Go 回归:`GOTOOLCHAIN=local go test ./internal/... ./desktop/ ./cmd/...`(desktop/hookinstall 预存基线除外)。前端 `vue-tsc` 干净 + web vitest 绿。
- [ ] 手测多节点:两节点各配不同 `ATTERM_RELAY_INSTANCE_PUBLIC_URL`、同一 PG;桌面登录 → 日志 `uplink configured for wss://<home>`;Web 登录 → DevTools 看 `/client` WS 连到 home host。
- [ ] 单机回退:不配 `INSTANCE_PUBLIC_URL` → home 为空 → 桌面连 `cfg.RelayURL`、web 连 location,行为同今。

## 后续(C2 —— 不在本 plan)

- 三端节点选择器 UI:`GET /api/nodes` + 客户端测 ping + 显示当前选择 + 切换 `PUT /api/me/home`(全账号生效)+ 切换后重连到新 home。桌面 `SettingsRelay.vue`、Web `settings/tabs/Relay.vue`、移动 setup。空 home → 提示"必须重选"。见 C1 spec §7 / B spec §6。
