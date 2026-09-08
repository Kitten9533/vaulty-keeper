# vaulty-keeper 文档索引

> 中文 | [English](README.md)

`vaulty-keeper` 文档的入口与现行归属图。保持索引精简：下表每项是其主题的单一现行位置；历史记录保持带日期并链接后继。

## 入口

| 文档 | 职责 |
|---|---|
| [../README.md](../README.md) · [中文](../README.zh-CN.md) | 安装、快速开始与命令参考（面向人工）。 |
| [../CONTRIBUTING.md](../CONTRIBUTING.md) · [中文](../CONTRIBUTING.zh-CN.md) | 构建、测试与贡献方式；文档约定。 |
| [../SECURITY.md](../SECURITY.md) · [中文](../SECURITY.zh-CN.md) | 安全报告策略与边界摘要；链接安全模型。 |
| [../AGENTS.md](../AGENTS.md) | Agent 操作约束与检查（单一入口中文文件）。 |

## 现行指南（英文 + 中文配对）

| 指南 | 覆盖 |
|---|---|
| [security-model](security-model.md) · [中文](security-model.zh-CN.md) | **统一安全边界**：信任层级、明文生命周期、token、容器限制、各协议限制、验证状态。 |
| [apollo-snapshot-guide](apollo-snapshot-guide.md) · [中文](apollo-snapshot-guide.zh-CN.md) | 快照实现：加密文件布局、双密钥设计、敏感检测、掩码/指纹、显式 safe 放行、已测示例。 |
| [ui-guide](ui-guide.md) · [中文](ui-guide.zh-CN.md) | Web UI：导航、字段、确认流程、AES 分层、UI 内数据库隧道。 |
| [db-proxy-architecture](db-proxy-architecture.md) · [中文](db-proxy-architecture.zh-CN.md) | PG/MySQL/Redis 隧道架构：什么在哪里运行、凭据注入、时序。 |
| [db-proxy-examples](db-proxy-examples.md) · [中文](db-proxy-examples.zh-CN.md) | DB 用法示例与合成夹具（文档工作期间按源码核对，未重新执行）。 |
| [mongodb-tunnel-guide](mongodb-tunnel-guide.md) · [中文](mongodb-tunnel-guide.zh-CN.md) | MongoDB 8 隧道：精确 URL 选项、安全限制、排错、带日期验证矩阵。 |

## 历史记录（`superpowers/`，英文 + 中文配对）

实现设计/计划作为带日期证据保留。它们**不是**现行指令，通常**不可重放**；有后继的各自链接现行指南。不要对真实数据重放其中的命令。

| 记录 | 状态 / 后继 |
|---|---|
| [specs/2026-08-25-claude-style-web-ui-design](superpowers/specs/2026-08-25-claude-style-web-ui-design.md) | 初版 UI 的历史来源；后继：[ui-guide](ui-guide.md)。 |
| [plans/2026-08-25-local-web-ui](superpowers/plans/2026-08-25-local-web-ui.md) | 历史执行记录；不可重放；后继：[ui-guide](ui-guide.md)。 |
| [specs/2026-08-25-full-ui-migration-design](superpowers/specs/2026-08-25-full-ui-migration-design.md) · [plans/2026-08-25-full-ui-migration](superpowers/plans/2026-08-25-full-ui-migration.md) | UI 迁移决策/历史；后继：[ui-guide](ui-guide.md)。 |
| [specs/2026-08-25-env-appid-and-snapshot-delete](superpowers/specs/2026-08-25-env-appid-and-snapshot-delete.md) | 已实施的 env/AppID/删除决策；仍有用的命名/身份约束。 |
| [specs/2026-08-31-db-proxy-tunnel-design](superpowers/specs/2026-08-31-db-proxy-tunnel-design.md) · [plans/2026-08-31-db-proxy-tunnel](superpowers/plans/2026-08-31-db-proxy-tunnel.md) | 历史三协议隧道设计；后继：[db-proxy-architecture](db-proxy-architecture.md) / [mongodb-tunnel-guide](mongodb-tunnel-guide.md)。 |
| [specs/2026-09-07-mongodb-tunnel-design](superpowers/specs/2026-09-07-mongodb-tunnel-design.md) · [plans/2026-09-07-mongodb-tunnel](superpowers/plans/2026-09-07-mongodb-tunnel.md) | 已接受的 Mongo 设计决策 + 带日期执行证据；现行行为与矩阵：[mongodb-tunnel-guide](mongodb-tunnel-guide.md)。 |

## 原则

- 每项主题只有一个现行位置。安全模型只存在于 `security-model.md`；指南链接它而不是复制它。
- 所有面向用户的内容默认英文，配 `.zh-CN.md`，顶部有语言切换链接。
- README 负责安装/入门，AGENTS 负责 agent 约束/检查，指南维护现行用法，记录维护带日期证据。
- 工作被取代后文档即成为历史；为证据保留并链接后继，而不是静默删除或改写成好像现行。
