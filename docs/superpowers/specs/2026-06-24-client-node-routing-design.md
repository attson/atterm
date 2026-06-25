# 客户端连接路由到 home 节点设计(阶段二 · 子项目 C1)

日期:2026-06-24
状态:已评审,待写实现计划

## 0. 背景与定位

**阶段二(多实例实时路由)子项目 C 的第一部分(C1)**。C 经评审再拆为:

- **C1(本设计)** — 连接路由:客户端读 `home_instance_url`,把有状态 WS 路由到该节点。
- **C2(后续)** — 节点选择器 UI(`GET /api/nodes` + 客户端测 ping + `PUT /api/me/home` 切换全账号生效 + 重连),三端。

C1 **栈在 B 之上**,依赖 B 的登录响应 `home_instance_url`、`relay_instances`/`user_home`(B 已在登录时自动分配 home = 登录所在节点)。**C1 一落地,多实例即真正可用**:客户端连到各自 home 会合,无需 UI。

阶段二模型:统一登录域名 + 账号级用户可选节点。**登录/无状态 API 走配置的入口域名**(任意节点/LB,写共享 DB);**只有有状态 WS(桌面 `/uplink`、移动/Web `/client`)路由到 home 节点**。E2EE 已按 `realm_id` 锚定(A),故换节点域名不破解密。

## 1. C1-1:home_instance_url 下发到客户端

relay 已在 `loginFinalizeResponse` 返回 `home_instance_url`(B,`internal/relay/opaque_auth.go:136`),但客户端尚未接收。

- **e2eeclient**(桌面用):B 只改了 relay 端,SDK 未动。C1 要在 `internal/e2eeclient/client.go`:`LoginResult`(~:97)加 `HomeInstanceURL string`;**SDK 的 wire `loginFinalizeResponse` 结构加 `HomeInstanceURL string json:"home_instance_url"`**(它是 SDK 自己的解码结构,B 没给它加);`Login()` 解析处(~:241)`HomeInstanceURL: finResp.HomeInstanceURL`。
- **移动**(`desktop/frontend/src/platform/capacitor.ts`):`opaqueLogin` 的 finBody 类型与返回值加 `home_instance_url`(~:57/:115)。
- **Web**:登录 finalize 解析处取 `home_instance_url`。
- **注册路径暂不下发 home**:B 只给了 login finalize;`registerFinalizeResponse` 无 `home_instance_url`。**不重开 B 的 handler**。新账号 home 为空 → 走回退(§4),下次登录即获 home。

## 2. C1-2:桌面 uplink 路由到 home

- `desktop/config.go`:`appConfig` 加 `RelayHomeInstanceURL string json:"relay_home_instance_url,omitempty"`(`RelayRealmID` 之后)。
- `desktop/app.go` 两条登录路径(`LoginRemoteRelay` ~:590、`RegisterRemoteRelay` ~:677):`cfg.RelayHomeInstanceURL = res.HomeInstanceURL`(注册路径 res 无 home → 空),随 `cfgStore.Set` 持久化。
- `desktop/app.go` `applyRelayUplink`(~:374-404):计算 **dial URL**:
  - `cfg.RelayHomeInstanceURL` 非空 → 用它(把 `https://host` 转 `wss://host`、`http`→`ws`);
  - 否则 → `cfg.RelayURL`(现状,已是 ws/wss)。
  - `uplink.go:244` 仍 dial `dialURL + "/uplink"`,但 `newUplink` 收到的是上面算出的 dialURL。
- 抽一个纯函数做 URL 推导(便于测试),如 `uplinkDialURL(homeURL, relayURL string) string`。

## 3. C1-3:Web/移动 /client 路由到 home

- `web/src/shared/api/relay-config.ts`:`RelayConfig` 加 `homeInstanceURL?: string`(`realmId` 之后);`loadRelayConfig`/`saveRelayConfig` 保留该字段(沿用 A 的 `realmId` 条件展开模式,避免 `exactOptionalPropertyTypes` 写 undefined)。
- 登录后把 `home_instance_url` 存进 config blob(web `persistSession` 链路、移动 `capacitor.ts` 的 `secureStorage.set` 链路)。
- `web/src/shared/ws/client-conn.ts` 的 `wsUrl(path)`(~:350):若当前 relay 配置的 `homeInstanceURL` 非空 → 从它构造 WS(`https`→`wss`/`http`→`ws` + host),否则现状(mobile-app 分支用 `baseURL`、浏览器分支用 `location`)。把 https→ws 的转换抽成一个小工具函数便于测试。
- 跨节点 WS 的 `Origin` 由共享 `allowed_origins`(Plan 2,集群一致)统一放行,无 CORS 问题。

## 4. C1-4:空 home 回退 + 测试

- **`home_instance_url` 为空时**(注册 / 单机未配节点 / B 返回死 home 的空)→ **用现状**:桌面 `cfg.RelayURL`、web/移动 `baseURL`/`location`。保证单机零变化;死 home 时仍连入口域名(下次登录重解析)。
- **测试**:
  - e2eeclient:`LoginResult.HomeInstanceURL` 正确解析(扩展现有 register+login 往返测试,断言 login 结果带 home)。
  - 桌面:`uplinkDialURL` 纯函数 —— home 非空返回 `wss://home/`(https→wss)、空返回 RelayURL;Go 单测。
  - Web:`wsUrl` 在 `homeInstanceURL` 非空时返回从它派生的 WS URL、空时返回现状;TS 单测(vue-tsc + 现有测试框架)。

## 5. 范围与约束

- **只路由有状态 WS**(桌面 /uplink、移动/Web /client)到 home;登录/API 仍走入口域名。
- **空 home 回退**保证单机/注册/死 home 不破。
- 不引消息总线、不改 relay(C1 纯客户端 + e2eeclient SDK;relay 的 home 下发 B 已做)。
- Go 1.23.0 不变;前端 vue-tsc 干净。

## 6. 受影响代码(C1 概览)

- `internal/e2eeclient/client.go`:`LoginResult.HomeInstanceURL` + wire 确认 + 解析。
- `desktop/config.go`:`RelayHomeInstanceURL`。
- `desktop/app.go`:两登录路径设 home;`applyRelayUplink` 用 `uplinkDialURL`;新增 `uplinkDialURL` 纯函数 + 测试。
- `desktop/frontend/src/platform/capacitor.ts`:`opaqueLogin` 解析并返回 `home_instance_url`,存入 config。
- `web/src/shared/api/relay-config.ts`:`homeInstanceURL` 字段 + load/save 保留。
- `web/src/shared/api/auth.ts`(或登录解析处):取 `home_instance_url` 存入 config。
- `web/src/shared/ws/client-conn.ts`:`wsUrl` 按 `homeInstanceURL` 路由 + https→ws 工具 + 测试。

## 7. 后续(C2 —— 不在本子项目)

- 三端节点选择器 UI:`GET /api/nodes` + 客户端测 ping 延迟 + 显示当前选择 + 切换 `PUT /api/me/home`(全账号生效)+ 切换后重连到新 home。桌面 `SettingsRelay.vue`、Web `settings/tabs/Relay.vue`、移动 setup。
- 空 home(死节点/未选)→ C2 选择器提示"必须重选"(B 的 opus 评审已记此交接点)。
- 见 B 设计 spec §6。
