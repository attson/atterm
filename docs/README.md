# docs/ · 工程文档

这里服务仓库贡献者和编码 agent。终端用户文档位于
[`site/docs/guide/`](../site/docs/guide/)，构建后发布到
<https://attson.github.io/atterm/guide/>。

## 权威层级

同一事实出现冲突时，按下面的顺序判断：

1. 当前代码、测试和 wire 常量是可执行事实。
2. `docs/spec/` 描述当前稳定行为，发现漂移必须随实现修正。
3. `AGENTS.md` 只常驻跨模块红线、开工入口和验证命令。
4. `docs/tasks/` 是某次需求的 intent/spec 工作区，不自动代表已经上线。
5. `docs/superpowers/specs/` 保存设计背景和历史取舍；常被代码注释引用，默认冻结。
6. `docs/superpowers/plans/` 与 `docs/plans-history.md` 是实施过程和历史索引，不是现行规范。

版本、PR 和 feature 上线时间只查 `git log` / GitHub，不写进 `AGENTS.md` 或 spec
正文。Spec 可以提兼容边界，但不维护 release changelog。

## 目录

```text
docs/
├── spec/                    当前工程规范（活文档）
│   ├── architecture.md      系统边界、组件、数据流和运行时拓扑
│   ├── protocol.md          terminal frame、HTTP、E2EE、Peer/Rendezvous wire
│   ├── auth.md              OPAQUE、session token、account_key 与权限
│   ├── conventions.md       Go/TS 约定、测试、日志、发布与常见实现规则
│   ├── component-style.md   共享前端的视觉、交互和可访问性规则
│   ├── feishu.md            飞书模式、anchor card 与 AskUserQuestion
│   ├── site.md              VitePress 文档站和交互 demo
│   ├── termius-import.md    Termius 本机迁移边界
│   └── traffic.md           Relay 流量统计口径
├── tasks/                   需求澄清与实现 spec；完成后保留证据，不当现状引用
├── superpowers/specs/       冻结的设计与取舍记录
├── superpowers/plans/       阶段性实施计划和 roadmap
├── features/                独立 feature 的配置/运维说明
├── feishu/                  飞书部署与手工验收资料
├── roadmap.md               产品方向和未完成 gate
└── plans-history.md         已归档计划索引
```

## 写在哪里

| 内容 | 位置 | 更新方式 |
| --- | --- | --- |
| 用户安装、配置和排障 | `site/docs/guide/` | 面向任务写教程，不暴露内部实现细节 |
| 当前架构、协议或安全约束 | `docs/spec/<domain>.md` | 随代码覆盖式更新，永远描述当前真相 |
| 跨模块、易回归且必须常驻的红线 | `AGENTS.md` | 保持短而明确，并链接详细 spec |
| 一次需求的范围、验收与方案 | `docs/tasks/YYYY-MM-DD/<slug>/` | 实施工作区；完成后不反向冒充规范 |
| 需要保留的设计背景 | `docs/superpowers/specs/` | 新增新文档；已有文件默认不改 |
| 长周期多阶段实施 | `docs/superpowers/plans/` | 仅在确有跨 PR 路线图时新增或续写 |
| 单 feature 配置说明 | `docs/features/` | 与实现和用户 guide 各自保持对应视角 |

`docs/spec/` 回答“系统现在如何工作以及什么不能破坏”；`site/docs/guide/` 回答
“用户如何完成一件事”。两边可以描述同一 feature，但不能互相复制整段内部细节。

## 维护流程

1. 先读 `AGENTS.md` 和目标模块 spec，再从测试与代码确认当前行为。
2. 协议、鉴权、E2EE、配置 schema 或跨端共享行为变更，必须和实现同一个提交更新 spec。
3. 新增用户可见能力时，同时判断 `site/docs/guide/` 是否需要教程或 FAQ。
4. 不把 task spec 的计划态直接复制进正式 spec；只写已经实现并验证的行为。
5. 不通过修改冻结设计稿来“修正历史”；现行行为写进 `docs/spec/`，历史原因仍留原处。
6. 文档变更至少运行 `git diff --check`，并验证新增的本地 Markdown 链接。

查当前能力和上线时间：

```bash
git log --oneline -30
gh pr list --state merged --limit 30
```
