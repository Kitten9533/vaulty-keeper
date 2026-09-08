# vaulty-keeper 文档索引

> 中文 | [English](README.md)

`vaulty-keeper` 文档的入口与现行归属图。保持索引精简：下表每项是其主题的单一现行位置。历史实现记录归档在 git tag `docs-superpowers-archive`，不在此列出。

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
| [cli-reference](cli-reference.md) · [中文](cli-reference.zh-CN.md) | 完整命令参考：apollo 快照工具、aes 辅助命令、其他命令。 |
| [apollo-snapshot-guide](apollo-snapshot-guide.md) · [中文](apollo-snapshot-guide.zh-CN.md) | 快照实现：加密文件布局、双密钥设计、敏感检测、掩码/指纹、显式 safe 放行、已测示例。 |
| [ui-guide](ui-guide.md) · [中文](ui-guide.zh-CN.md) | Web UI：导航、字段、确认流程、AES 分层、UI 内数据库隧道。 |
| [db-proxy-architecture](db-proxy-architecture.md) · [中文](db-proxy-architecture.zh-CN.md) | PG/MySQL/Redis 隧道架构：什么在哪里运行、凭据注入、时序。 |
| [db-proxy-examples](db-proxy-examples.md) · [中文](db-proxy-examples.zh-CN.md) | DB 用法示例与合成夹具（文档工作期间按源码核对，未重新执行）。 |
| [mongodb-tunnel-guide](tunnel/mongodb-tunnel-guide.md) · [中文](tunnel/mongodb-tunnel-guide.zh-CN.md) | MongoDB 8 隧道：精确 URL 选项、安全限制、排错、带日期验证矩阵。 |
| [postgres-tunnel-guide](tunnel/postgres-tunnel-guide.md) · [中文](tunnel/postgres-tunnel-guide.zh-CN.md) | PostgreSQL 隧道：连接模型、注册 URL、客户端设置、限制、排错。 |
| [mysql-tunnel-guide](tunnel/mysql-tunnel-guide.md) · [中文](tunnel/mysql-tunnel-guide.zh-CN.md) | MySQL 隧道：连接模型、注册 URL、后端 TLS、客户端设置、排错。 |
| [redis-tunnel-guide](tunnel/redis-tunnel-guide.md) · [中文](tunnel/redis-tunnel-guide.zh-CN.md) | Redis 隧道：连接模型、注册 URL、客户端设置、限制、排错。 |
| [container-isolation](container-isolation.md) · [中文](container-isolation.zh-CN.md) | Docker/agent 与宿主密钥、密文隔离，以及不用 Docker 的替代方案。 |

## 原则

- 每项主题只有一个现行位置。安全模型只存在于 `security-model.md`；指南链接它而不是复制它。
- 所有面向用户的内容默认英文，配 `.zh-CN.md`，顶部有语言切换链接。
- README 负责安装/入门，AGENTS 负责 agent 约束/检查，指南维护现行用法。
- 工作被取代后文档即成为历史；用 git tag 归档，而不是留在树内像现行一样。
