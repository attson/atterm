# Relay 流量统计

个人 Settings Relay 面板与管理侧复用时间窗口控件和分时图表，提供实时、分时、汇总三种模式。

- 实时：最近 15 / 60 分钟，按 UTC 分钟统计，每 10 秒刷新。隐藏页面暂停轮询，回到前台补查。
- 分时：选择 UTC 日期，按小时或分钟查看；点击小时进入该小时的 60 分钟明细。
- 汇总：保留 7 / 30 / 90 天日统计与上一周期对比；点击日柱进入分时。
- 速率为最近完整分钟的字节数 / 60；峰值也只使用完整分钟。当前时间桶标记「统计中」。
- 个人类别摘要、帧排行与管理侧用户排行均跟随当前窗口；管理侧保留用户选择。
- Peer / direct P2P 数据仍只在汇总模式展示，不能从日总量推导分钟明细。

## 采集与持久化

`internal/relay/traffic_meter.go` 在记录 wire frame 时就按实际 UTC 分钟分桶，每 10 秒批量刷盘、正常退出时补刷。
`userstore.AddTrafficDeltas` 在同一个事务里累计分钟、小时、日三种粒度；日归属来自采集时间，不能取刷盘当天。
SQLite / Postgres 使用相同的 additive UPSERT 语义；多实例查询共享 DB 聚合，不合并某一节点尚未落库的内存增量。
单实例崩溃或刷盘失败可能丢失尚未落库的一批数据；刷新接口不触发刷盘。

Relay 流量同时按认证登录会话归因：`client_id` 是 `sessions.id_hash`（SHA-256 后的会话 ID，不是 bearer token），`client_name` / `client_kind` 由该会话登录时保存的 User-Agent 归一化。它表达的是“登录客户端”，不是不可复制的硬件身份；同一个 token 被复制后仍计入同一客户端。历史流量无法反推来源，迁移后使用空 `client_id`，前端显示为“升级前 / 未识别”。

`relay_traffic_daily` 主键为 `(user_id, day, client_id, frame_type, direction)`；`relay_traffic_buckets` 主键为 `(user_id, bucket, started_at, client_id, frame_type, direction)`，`bucket` 为 minute / hour。
按 UTC 日期边界清理：minute 删除早于 7 天前零点的数据，hour 删除早于 90 天前零点的数据。
启动时及之后每小时清理，日汇总不受清理影响。旧日统计无法反推分时；升级之前的明细为空。

## HTTP 查询

既有 `/api/me/traffic` 与 `/admin/api/traffic` 的默认日统计响应、日期查询参数不变。
新增 `bucket=minute|hour` 时，`from` / `to` 使用 RFC3339 时间，区间为 `[from, to)`，最大 48 小时。
`from` 向下对齐时间桶起点；时间范围须在该粒度保留期内，`to` 最晚为明天 UTC 零点，以允许查询今天全天。

个人响应的 `relay_detail[].day`、管理响应的 `rows[].day` 在分时模式使用 UTC RFC3339 桶起点。
个人响应不返回 user id/email，查询身份只取认证上下文，忽略调用方传入的 user_id。
个人响应的 `relay_detail[]` 额外返回 `client_id`、`client_name`、`client_kind`，Settings 按客户端汇总出站、入站、总量与占比；既有 `relay[]` 日总量仍跨客户端求和，因此升级前后的总量口径不变。
管理侧支持既有 detail / group / summary 维度（分时默认 detail），可用 user_id 限定账号。

分时响应额外返回：

| 字段 | 含义 |
|---|---|
| bucket / bucket_seconds | 时间粒度及每桶秒数 |
| server_time | 响应生成时的服务器 UTC 时间，用于识别未完成桶 |
| updated_at | 此查询结果最近一次流量写入时间；无数据时为空，不能当作全平台采集水位 |
| retention_from | 此粒度当前保留期的 UTC 起点 |
| flush_interval_seconds | 批量入库间隔，当前为 10 秒 |

未返回的桶在图表中补零；无明细时明确说明暂无记录，不将旧日总量插入分钟桶。
自动刷新不会清空成功数据；失败时显示刷新错误和旧数据的写入时间。
平台健康信息与流量统计时间各自显示，当前节点会话数不能标作全平台实时会话数。
