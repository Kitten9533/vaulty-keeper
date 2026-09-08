# MongoDB 隧道实施计划

> [English](2026-09-07-mongodb-tunnel.md) | 中文
>
> **带日期执行记录，2026-09-07。不可执行或重放此清单。** 保留该会话的实施决策、任务证据与缺口。原 `subagent-driven-development` / `executing-plans` 指令、master 许可及主负责人/worker 分工随该会话结束失效，不授权后续工作。现行操作与验证状态由 [MongoDB 指南](../../mongodb-tunnel-guide.zh-CN.md#验证状态)维护，不由此清单维护。

**目标：** 按[已确认设计](../specs/2026-09-07-mongodb-tunnel-design.zh-CN.md) 实现代管凭据的 MongoDB 8 隧道。

**架构：** 终止前端 token SCRAM，独立认证单个后端，并在连接全程保留命令感知分帧。执行递归策略与有归属范围的游标校验；重建控制回包，不改写业务文档。

**技术栈：** Go、现有 xdg SCRAM、官方 MongoDB 驱动 v2.9.0 的公开 BSON 包、现有 CLI/UI、隔离的 MongoDB 8 与原生客户端。

**证据出处（2026-09-07）：** 实施会话报告实现、隔离原生测试、仓库测试/race/vet 和重建完成。对象是基于 `7273bb21e8347777058a17fb95a16aa2a17a36dc` 的未提交 MongoDB 工作树，平台为 macOS 12.4 Intel，夹具 MongoDB 8.0.13、驱动 v2.9.0。仅基础 commit 无法复现这些未提交改动；此处没有精确受测树摘要或归档原始日志。下方勾选保留当时报告，不是新验证。首轮独立发现已修正、补回归并本地复核；自动独立复审不可用，实际 MongoDB TLS 与人工 TTY shell 未验证。后续结果与待验项只在指南矩阵维护，不改写这里的带日期证据。该 Mongo 工作后续已随 **v0.8.0**（2026-09-07）发布。

## 历史文件分工

| 范围 | 文件与职责 |
|---|---|
| 协议 | `internal/dbproxy/mongodb_config.go`、`mongodb_wire.go`、`mongodb_auth.go` 及对应 `_test.go`：URL 校验、分帧、后端 TLS/SCRAM |
| 策略 | `internal/dbproxy/mongodb_policy.go` 与 `mongodb_policy_test.go`：递归命令/命名空间检查及重建回包 |
| 编排 | `internal/dbproxy/mongodb.go`、`mongodb_test.go`、`internal/dbproxy/tunnel.go`、`tunnel_test.go`：前端 SCRAM、命令循环、共享且有范围的游标、集合类型检查 |
| 产品 | `internal/dbproxy/store.go`、`testconn.go`、`links.go`；`internal/cli/db.go`；`internal/ui/db.go`、`static/app.js`、`static/index.html`；`internal/i18n/i18n.go`，及现有相邻测试 |
| 集成 | `scripts/mongotest.sh`、使用 `mongointegration` tag 的 `internal/dbproxy/mongodb_integration_test.go`：standalone/固定单节点副本集夹具，可选容器内原生 mongosh |
| 文档 | 本双语计划/设计/指南，以及双语 README、数据库架构/示例和 UI 指南的链接/范围更新 |

当时设计决定不用驱动 `x/*` 或内部线协议/认证包，运行时仅公开 BSON，tagged 测试使用官方原生 Go 客户端。会话将依赖/代码交各自负责人，文档单独分工，AGENTS 归主负责人，不做无关协议重构或 commit。这些分工和许可仅为历史上下文，不代表当前文件归属或授权。

## 历史清单

下方所有祈使句和勾选/未勾选结果均属于 2026-09-07。命令仅作为证据标识保留，不是执行指令；不得代入真实注册 URL、密钥或存储。未勾选项是当时的证据缺口，不是第二份实时验证队列。

## 1. 配置、分帧与认证

- [x] 在 `mongodb_config.go` / `mongodb_config_test.go` 实现严格 URL 校验及表驱动测试，覆盖[完整白名单](../../mongodb-tunnel-guide.zh-CN.md#注册-url)、认证库优先级、转义、重复/未知选项、TLS 冲突和超时边界。
- [x] 在 `mongodb_wire.go` / `mongodb_wire_test.go` 实现分帧及回归测试，覆盖长度/BSON/标志位/序列、解码前深度 100、未认证消息 1 MiB、完整帧 48,000,000 字节和出站 type-1 批量写。
- [x] 在 `mongodb_auth.go` 实现独立 SHA-256/SHA-1 后端认证、预期副本集名校验、无不安全重试的验证 TLS、有界交换和通用错误。保留默认 5 秒连接超时同时约束操作的行为。
- [x] 主负责人报告假后端 TLS 单测已通过完整证书/主机名验证（`TestMongoAuthNetworkAndTLS`）；这不是实际 MongoDB TLS 夹具执行结果。
- [x] 最终 `make test` 及 race/vet 门禁包含全部协议/认证测试。定向重跑可用 `go test ./internal/dbproxy -run TestMongo -count=1`，检查选中的测试，不能把空选择器结果当作证据。

## 2. 策略与元数据

- [x] 实现默认拒绝命令/字段策略及测试，支持常见读取、需要服务端写入确认的 CRUD、只读聚合和普通 `lsid`，排除事务/可重试写入/`w:0` 及受保护数据库/命名空间。
- [x] 实现递归阶段/表达式/命名空间检查，包含嵌套 lookup/union/facet、元数据/角色自省和服务端 JS 拒绝，同时区分业务字面量（`mongodb_policy.go` / `mongodb_policy_test.go`）。
- [x] 重建带类型的控制/错误/元数据回包，包括写错误；保留业务 BSON 类型/值。主负责人报告的原生集成覆盖策略/错误脱敏及元数据分页。
- [x] 完整套件包含策略回归；修正独立审查发现后，本地复核了控制回包和递归策略处理。

## 3. 编排与游标归属

- [x] 实现前端 `vaulty`/专属 token 在 `admin` 的 SCRAM-SHA-256，无全局 token 兜底。经过检查的 hello 支持 backpressure 版本 `"2"`、maxTimeMS/会话/read-preference 字段及虚拟副本集名 `vaulty`，不暴露实际主机。
- [x] 实现持续命令循环和调用方元数据 `getMore` 原始命令追踪。注册表按目标/token 归属，跨 socket 校验命名空间/会话，不使用 socket 本地状态。限制：每目标/token 1,024 个游标、共享总计 16,384 个、30 分钟未使用过期、128 个准入连接（包含监控/已建立 socket）。
- [x] 直接/嵌套引用要求后端 `listCollections` 检查成功；允许普通集合或有效检查后确认不存在的命名空间，拒绝视图/时序及无法确认的元数据。在 `mongodb_test.go` 添加编排回归，并添加原生视图拒绝用例。
- [x] 接入监听分发/生命周期。主负责人报告的集成覆盖普通会话、错误/缺失 token、轮换和监听开关；不代表即时撤销既有会话。
- [x] 验证最终 race 结果，复核游标范围、清理、元数据分页及缺失集合行为。原生集成验证了连接池替换原 socket 后继续 getMore，不代表已证明所有连接池场景。

## 4. 存储、CLI 与 UI

- [x] 实现存储/类型识别、TestConn 认证探测及 token 生命周期；原生测试使用显式临时加密存储及合成测试密钥。
- [x] 实现共享 Mongo 链接、CLI 帮助/命令和 UI 控件/翻译，并配套测试（`mongodb_links_test.go`、`internal/cli/mongodb_test.go`、`internal/ui/mongodb_test.go`）。生成的 `mongosh '<URI>'` 命令按设计只在 argv 暴露代理 token 凭据，不含后端 URI。客户端 helper 别名不放宽注册 URL 校验。
- [x] 实现 TTY-only 直连 `db shell`：固定启动脚本、子进程专用 `VAULTY_KEEPER_MONGODB_URI`、连接前删除、URI 不进入 argv/交互全局作用域。Node 测试覆盖成功/失败清理；实际交互 TTY 使用仍未验证。
- [x] 通过 `make test` 运行最终 CLI/UI/i18n 测试和 `node scripts/check-ui.mjs`，通过后重建嵌入式 UI。
- [ ] 若要声称交互支持已实测，在获授权的人工 TTY 手动验证直连 `db shell`；与原生隧道 mongosh 测试证据分开。

## 5. 隔离的原生 MongoDB 8 测试

- [x] 实现 `scripts/mongotest.sh`，使用 `mongo:8.0.13`，提供 `--replica-set` 和 `--mongosh` 模式。需要 Docker/Go/OpenSSL；使用容器内原生 mongosh、随机夹具凭据、loopback 端口和仅用于夹具的环境 URL。
- [x] 进程内 Go 测试使用显式 `t.TempDir()` 存储和合成测试密钥，不访问实际用户存储或 Keychain，不需要/改变 HOME。脚本关闭 tracing，退出/中断时只删除带本次归属标签的容器及自己的临时文件。
- [x] 主负责人报告 `bash scripts/mongotest.sh --mongosh` 和 `bash scripts/mongotest.sh --replica-set --mongosh` 已通过。两者覆盖原生 Go + mongosh；探测四种后端机制/认证库组合，SHA-256/admin 场景完整覆盖带类型 CRUD/批量/聚合/游标/元数据/会话/策略/生命周期。详见[矩阵](../../mongodb-tunnel-guide.zh-CN.md#验证状态)。
- [ ] 实际 MongoDB TLS 夹具未运行。声称原生 TLS 验收前，需添加/运行隔离证书 Mongo 夹具；假后端证书测试是独立证据。

测试工具导出 `VAULTY_MONGO_TEST_URI`、`VAULTY_MONGO_TEST_CONTAINER` 和 `VAULTY_MONGO_TEST_REPLICA_SET`，然后执行 `go test -mod=readonly -tags=mongointegration ./internal/dbproxy -run '^TestMongoIntegration($|DriverHello$|DriverSASL$)' -count=1 -timeout=180s`。没有夹具 URI 时真实用例跳过。此命令禁止使用实际注册连接。

## 6. 文档与验收

- [x] 将双语设计/指南与当前配置、转发、集合权限、链接/shell 行为及实际测试模式对齐。保留操作超时提醒和注册/客户端 URL 区分。为 README/架构/示例/UI 指南添加简短链接，不改旧图。
- [x] 保留边界：业务数据不改写、可信 DBA 定义变更、不保证完整自省/GUI 兼容、前端明文、上游日志不脱敏及既有会话语义。注明通过结果的报告来源并保留证据缺口。
- [x] 更新 AGENTS，补充 Mongo 特有行为及隔离验收命令。
- [x] 运行 `make test`、`go test -race ./...`、`go vet ./...` 后执行 `make build`，均已通过；副本集原生集成也通过了 `GOFLAGS=-race` 检查。
- [x] 完成首轮独立审查，修正发现并补回归测试，完成最终本地源码复核。
- [ ] 自动独立复审不可用，不描述为通过。
- [x] 记录实际结果及剩余限制。2026-09-07 实施会话报告未暂存、提交或推送；这不描述后续仓库状态。
