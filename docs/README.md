# docs/ · 开发者文档

面向仓库贡献者 / AI 编码 agent 的规范、路线图与历史归档。**用户面向**的文档
在 [`site/docs/`](../site/docs/)(VitePress 站点,发布到 https://attson.github.io/atterm/)。

## 目录

```
docs/
├── spec/                    权威规范(每份都被代码或 AGENTS.md 直接引用)
│   ├── architecture.md      系统总览 + 组件边界 + phase 完成度
│   ├── protocol.md          帧协议(TypeIn / TypeOut / TypeMeta / TypeAnnounce / ...)
│   ├── auth.md              OPAQUE + account_key + session_token 全流程
│   ├── feishu.md            飞书集成的 mode / 事件 / 卡片
│   ├── conventions.md       命名 / 提交 / 分支约定
│   ├── component-style.md   前端组件风格
│   └── site.md              VitePress 站点构建约定
├── feishu/                  飞书部署 + 手工 e2e 清单
├── features/                单 feature 说明(shell-integration / web-push)
├── superpowers/
│   ├── specs/               每个 feature 的设计草案(为什么这样做),代码注释
│   │                        直接引用,不要动
│   └── plans/               仅剩活着的路线图(2026-08-04-refactor-roadmap.md);
│                            110 个历史 implementation plan 已归档到 plans-history.md
├── plans-history.md         109 个历史 plan 一句话索引(git 保完整原文)
└── roadmap.md               产品级 P0-P4 路线图
```

## "写在哪里" 决策树

新增内容前先想:**这个文档服务谁?**

| 谁读 | 写在哪 | 例子 |
| --- | --- | --- |
| 终端用户 / 想用 atterm 的人 | `site/docs/guide/` | 部署 relay / 快速上手 / FAQ |
| 未来开发者 / AI agent(架构决策) | `docs/spec/<domain>.md`(覆盖式改) | protocol 增加新帧、auth 换 OPAQUE 版本 |
| 未来开发者(某 feature 为什么这样做) | `docs/superpowers/specs/YYYY-MM-DD-<slug>-design.md`(冻结,只加不改) | 加新 feature 时的 design draft |
| 单 feature 用户可见配置 | `docs/features/<feature>.md` | Web Push / shell integration |
| 手工 e2e / 部署清单 | `docs/feishu/` 之类 topic 子目录 | 手工验收步骤 |
| 产品路线图 | `docs/roadmap.md`(整体), `docs/superpowers/plans/2026-08-04-refactor-roadmap.md`(单个大 initiative) | v0.5 P2 |
| 已完成的临时性 checklist / task 分解 | **不写** —— PR description + git log 就够 | 每 slice 的分工 |

## `docs/` vs `site/docs/`

| | `docs/` | `site/docs/` |
| --- | --- | --- |
| 谁读 | 贡献者 / AI agent | 终端用户 |
| 发布 | GitHub 仓库直接读 | 构建为 VitePress 站,发到 gh-pages |
| 内容风格 | 严格 / 内部术语 / 引用代码位置 | 教程 / 面向问题 / 中文口语化 |
| 是否被代码 grep | ✅ 常常 | ❌ 极少 |
| 何时同步 | 各自演进,同一 feature 描述会有重复,但严禁互抄 | |

**规则**:同一件事在两处都写。`docs/spec/` 描述**如何实现**;`site/docs/guide/` 描述**如何使用**。用户读 guide 不需要知道 protocol 帧格式;开发者读 spec 不需要知道 UI 按钮在哪。有交叉的地方(如 E2EE 概念)两边都简写各自侧重,不要 include 或互 link 引导跨读。

## 历史 plans

每个 v0.1~v0.3 时期的大 feature 都有一份 `docs/superpowers/plans/YYYY-MM-DD-<slug>.md`(implementation checklist)。这些 plan **在 work merge 后 = 墓碑**,活着的只有 [`2026-08-04-refactor-roadmap.md`](./superpowers/plans/2026-08-04-refactor-roadmap.md)(当前重构路线图)。

其余 109 个已归档为一句话索引 [`plans-history.md`](./plans-history.md)。需要看原文:

```bash
git log --diff-filter=D -- docs/superpowers/plans/YYYY-MM-DD-topic.md
git show <sha>:docs/superpowers/plans/YYYY-MM-DD-topic.md
```

## 更新习惯

- `docs/spec/*.md` 是**活的**,feature 变了就覆盖式改
- `docs/superpowers/specs/*.md` 是**冻结的**,只加新的、不改旧的
- `docs/superpowers/plans/*.md` **不新增**(除非启动一个新的多 PR initiative,才写一份带追加交付表的活路线图,类似 refactor-roadmap)
- `AGENTS.md` 只放**结构性事实**(仓库布局 / 何时改哪里 / 红线),**不放**版本流水账
- `docs/roadmap.md` 每次里程碑闭合后回来打勾 + 加一行状态
