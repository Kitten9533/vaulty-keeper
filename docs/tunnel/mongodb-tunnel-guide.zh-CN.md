# MongoDB 隧道指南

> 中文 | [English](mongodb-tunnel-guide.md)
>
> 当前 MongoDB 8 接口，2026-09-07 按源码核对。本指南维护下方带日期的历史验证矩阵；文档更正不构成新的测试/构建证据。通用安全依据见[安全模型](../security-model.zh-CN.md)。

## 连接模型

注册一个使用普通用户名/密码的后端 MongoDB 端点。宿主通过现有 DB 存储加密保存 URL 和专属 token；连接名称/类型/端口/状态元数据未加密。这不承诺输入文件、导出、终端输出或其他宿主明文产物不存在。客户端连接代理端口，使用虚拟用户名 `vaulty`，以**连接专属隧道 token 作为密码**，采用 SCRAM-SHA-256 和 `authSource=admin`。token 可以授予代理访问权限，虽然不是真实后端密码，仍应作为凭据保护。UI 的 DB-connect GET 无需写 token 也会返回可用 token/链接，这不是无害的掩码元数据。

代理独立执行后端 SCRAM-SHA-256 或 SCRAM-SHA-1 认证。后端用户名、密码、认证库和 TLS 设置来自注册 URL，而非客户端虚拟凭据。使用专用最小权限后端账号；代理命令白名单不会把可写账号变成只读。除业务读写权限外，账号还需要各业务库的 `listCollections` 权限，以便代理验证普通集合类型。MongoDB 不沿用其他隧道的全局 bridge token 兜底。

## 认证与数据流

```text
客户端                         serve                        后端
  | SCRAM：用户 vaulty            |                             |
  | + 专属 token                 |                             |
  +----------------------------->| 校验专属 token              |
  |                              | + 后端 SCRAM 认证           |
  |                              +---------------------------->|
  |                              |<----------------------------+
  | 命令帧                       |                             |
  +----------------------------->| 白名单 + 元数据检查         |
  |                              +---------------------------->|
  |<-----------------------------+<----------------------------+
  | 控制回包脱敏；业务文档不改写                                  |
```

客户端以用户 `vaulty` 向代理认证，专属 token 作为 SCRAM-SHA-256 密码（`authSource=admin`）。serve 校验 token 后，用注册凭据独立执行后端 SCRAM-SHA-256/SHA-1 认证。与 PG/MySQL/Redis 不同，认证后**不做原始字节拼接转发**：每个命令帧都要通过已审查白名单与元数据策略后才转发，控制回包重建/脱敏，业务文档原样通过。

## 注册 URL

仅接受 `mongodb://`，包含单个主机名或方括号包裹的 IPv6 地址。默认端口 `27017`，显式端口范围 `1..65535`。用户名和密码均不可为空。凭据及选项值中的保留字符需进行百分号编码。不支持 SRV、seed 列表、URL fragment 或外部认证。

URL 路径指定默认业务库，省略时为 `test`。后端认证库优先级为显式 `authSource`、非空 URL 数据库、`admin`。数据库/认证库名称必须是有效 UTF-8，长度 1..63 字节，不能包含 `/`、`\`、`.`、空格、`"`、`$`、NUL、`:`、`*`、`<`、`>`、`|`、`?`、制表符、CR 和 LF。

以下是当前注册 URL 完整查询参数白名单；名称和布尔值区分大小写。重复 key 和所有其他选项均被拒绝。

| Key | 允许值 / 行为 |
|---|---|
| `authSource` | 非空数据库名，覆盖上述默认优先级；不接受 `$external` |
| `authMechanism` | `SCRAM-SHA-256` 或 `SCRAM-SHA-1`；省略时优先使用声明支持的 SHA-256，否则使用支持的 SHA-1；服务端省略机制列表时使用 SHA-1 |
| `tls`、`ssl` | 字面量 `true` 或 `false`；两个别名同时出现时必须一致；默认 `false` |
| `tlsCAFile` | 非空的宿主 PEM CA 文件路径，必须启用 TLS；当前读取器要求普通文件且不超过 1 MiB，并用它作为信任池 |
| `connectTimeoutMS` | 整数 `1..120000`，默认 `5000`；当前还限制每次上游命令耗时，而不只是建立连接 |
| `directConnection` | 提供时只接受字面量 `true`；省略时也仍是固定端点 |
| `replicaSet` | 非空预期副本集名称，不含斜杠、反斜杠、逗号、解析器列出的空白字符（空格/tab/CR/LF）及 NUL；校验后端 hello，不进行发现/故障转移 |

TLS 验证后端证书及主机名，默认使用系统信任，也可使用配置的 CA 文件。验证失败即终止连接建立，不自动明文重试或不安全降级。拒绝 `tlsInsecure`、`tlsAllowInvalidCertificates` 等绕过选项。显式选定认证机制后，不能在认证失败时悄悄替换机制。

`retryWrites`、`appName`、`compressors`、`readPreference`、`socketTimeoutMS`、`serverSelectionTimeoutMS` 和其他未列出的 key **不能用于注册 URL**，即使普通 MongoDB 驱动接受它们。客户端隧道 URI 是另一个接口。

命令截止时间意味着默认配置下长聚合或 `getMore` 可能在 5 秒时失败。它不是 MongoDB 的 `maxTimeMS`，写入超时也不能证明写入未生效。不要自动重放写入；不支持可重试写入。

## 客户端配置

MongoDB 使用现有 DB 流程。由人工检查宿主 DB 密钥，确实缺失时才执行 `vaulty-keeper db init`。非空 `VAULTY_KEEPER_DB_KEY` 优先于系统密钥库，必须是解码后 32 字节的 Base64，错误时不回退。重新生成前私下检查来源，不把真实密钥导入 agent 环境。在运行客户端的机器安装 `mongosh`，vaulty-keeper 和 agent 镜像默认不提供它。

以下人工流程使用显式隧道端口 `15438`，假设授权后端业务库是 `businessdb`。先检查 OS 端口可用性，按注册信息调整数据库。这是用法示例，不是已执行夹具：

```sh
# 人工宿主终端：后端 URL 通过 stdin 提供，不放入 argv。
vaulty-keeper db add mongo-app --port 15438
vaulty-keeper db on mongo-app
vaulty-keeper db test mongo-app
vaulty-keeper serve --addr 127.0.0.1:8970
```

`db add` 后在 stdin 提示中粘贴授权后端 URL 并回车。当前输入**会回显**，应避免录制终端。stdin 不会从 shell 历史删除生产端的 `printf 'URL'`。先注册再启动 serve：只有启动时存储已存在且 DB 密钥可用，才创建 DB watcher。仅桥模式的 serve 在首次注册/配置密钥后需重启。`serve --dir` 选择快照，`VAULTY_KEEPER_DB_DIR` 选择 DB 存储。

`serve` 持续运行。在使用相同存储/密钥上下文的另一个**宿主**终端获取只含代理信息的连接配置：

```sh
vaulty-keeper db list
vaulty-keeper db connect mongo-app
vaulty-keeper db connect mongo-app --container
```

不要把真实后端 URL/密码放入 AI 消息、shell 历史或命令行参数。AI 应使用隧道连接信息，不使用 `db show`、加密存储、宿主密钥或直连后端 shell。

客户端 URI 形式为：

```text
mongodb://vaulty:<DEDICATED_TOKEN>@127.0.0.1:<TUNNEL_PORT>/<DATABASE>?authSource=admin&authMechanism=SCRAM-SHA-256&directConnection=true&retryWrites=false
```

`<...>` 是占位符，不是实测凭据或已分配端口。使用注册连接返回的端口/数据库。即使用 `--container`，`db connect` 也需要宿主的 DBKey/Resolve；应在宿主生成，再受控交付代理凭据到客户端域。`remote dblist` 只返回元数据，不返回 token。不要挂载宿主密钥让无密钥容器运行 `db connect`。

容器链接将主机替换为 `host.docker.internal`，但 `--container` 只改打印地址。`serve --addr` 的 host 部分决定 DB 监听接口。Docker VM 通常无法访问仅 loopback 监听的 serve，需要可达绑定和防火墙规则。`0.0.0.0` 也会在其他接口暴露 HTTP/DB 端口，Compose 未强制唯一桥出口。Linux Docker 需要 host-gateway 映射。不要向前端 URI 追加后端 `authSource`、副本集详情或 TLS 设置。

原生客户端/GUI 字段中，主机/端口填写代理，用户为 `vaulty`，密码为专属 token，机制为 SCRAM-SHA-256，认证库为 `admin`，开启直连并关闭可重试写入。`db connect` 按设计向 agent/工具显示含 token 的 URI 及可直接运行的 `mongosh '<URI>'` 命令。执行该命令会把**隧道 token** 放进 argv；它不是环境变量启动器，也不含真实后端 URI。token 仍是访问凭据，应限制其扩散。人工使用时，通过 `mongosh` 密码提示输入 token 比放入 argv/history 更安全。

针对上方 `15438` / `businessdb` 注册配置，以下完整人工命令会提示输入**专属代理 token**，不是后端密码：

```sh
mongosh 'mongodb://127.0.0.1:15438/businessdb?authSource=admin&authMechanism=SCRAM-SHA-256&directConnection=true&retryWrites=false' \
  --username vaulty --password
```

连接后，对授权的普通 `orders` 集合进行限量读取：

```javascript
db.getSiblingDB('businessdb').runCommand({
  find: 'orders',
  filter: {},
  projection: { _id: 1, status: 1 },
  limit: 20,
  batchSize: 20,
  singleBatch: true,
  maxTimeMS: 2000
})
```

单批最多返回 20 份文档，可能为空；不会创建种子数据。选择有权限且不含秘密的集合/字段。`maxTimeMS` 限制服务端执行，不限制所有网络/总耗时，上游命令截止时间仍适用。本次文档更正未人工执行该密码提示/读取示例，它不是直连 shell 的 TTY 验收证据。

`db shell mongo-app` 是另一个在操作规则上仅供人工使用的直连后端流程。它以固定启动脚本调用 `mongosh --nodb --shell --eval`，真实 URI 只通过子进程 `VAULTY_KEEPER_MONGODB_URI` 传入，不进入 argv。脚本在连接/进入交互会话前删除该环境项，连接失败时也已删除，且不把 URI 放入交互全局作用域。这不等于安全擦除，也不防同用户进程读取。门禁检查 stdin TTY 状态，不识别真人或阻止 stdout 捕获；agent 不得伪造 TTY 或调用该明文/后端直连路径。它不是 AI 隧道或代理脱敏会话。启动约定有 Node 测试覆盖，但尚未人工验证实际交互 TTY 使用。

## 支持操作

| 范围 | 支持子集 |
|---|---|
| 读取 | 常见 `find`、`count`、`distinct`、过滤/投影/排序 |
| 写入 | 需要服务端写入确认的 `insert`、`update`、`delete`、`findAndModify`，受后端角色和已审查选项限制；acknowledged 不是人工批准 |
| 聚合 | 已审查的只读阶段/表达式，包括递归检查的 lookup/union/facet 来源；不支持任意 pipeline |
| 游标/会话 | 业务及脱敏元数据批次、`getMore`、`killCursors`、普通逻辑会话 ID；不含事务 |
| 导航 | 有限 ping/版本和脱敏的数据库/集合/索引列表；不是完整管理或自省 |

拒绝未知命令、字段、阶段和表达式。禁止业务访问 `admin`、`local`、`config`、`system.*`、视图、时序集合、`explain`、包括嵌套在内的 `$collStats` 等元数据阶段、`$$USER_ROLES` 及服务端 JavaScript 执行。代理通过后端 `listCollections` 检查直接和间接引用的命名空间。已有命名空间必须是 `collection` 类型；元数据检查成功后，不存在的命名空间允许通过普通 CRUD 读取或创建。元数据查询被拒绝或无法确认时拒绝操作。连接测试/ping 成功本身不能证明集合权限足够。

不支持事务、可重试写入、`w:0`、压缩、SRV/多端点、故障转移、外部认证和完整管理/诊断功能。常见不支持字段包括 `comment`、`collation`，`create`、`createIndexes` 命令也被拒绝。通过元数据检查后用允许的 CRUD 创建缺失普通集合，不等于允许这些显式管理命令。即使 CRUD 可用，GUI 也可能发送不支持的自省命令。不保证全部原生 GUI 功能或所有 MongoDB 驱动/会话语义。

元数据会被有意缩减。转发层记录游标的原始命令并用于 `getMore`，因此 `listCollections`/`listIndexes` 后续批次沿用原始元数据过滤 schema。游标归属按注册目标/token 和逻辑会话而非 socket 维护；即使在连接池中切换连接，命名空间/会话不匹配也会被拒绝。

## 协议限制

- 持续转发时逐帧校验，不切换成原始字节拼接。BSON 解码前检查嵌套深度上限 100。未认证消息上限 1 MiB，完整帧上限 48,000,000 字节。上游 insert/update/delete 批量写使用 OP_MSG type-1 文档序列，不合并成一个超大的 BSON 数组。
- 游标注册表限制每个注册目标/token 范围 1,024 项，共享隧道状态总计 16,384 项，会话归属单独检查。30 分钟未使用的记录过期。共享连接准入上限为 128，覆盖握手和已建立/监控连接，不仅是同时进行的 SCRAM 交换。
- 前端 hello 接受经过检查的 `backpressure: "2"`、`maxTimeMS`、普通 `lsid` 和 `$readPreference` 字段。固定副本集端点发布虚拟 `setName: "vaulty"`，不发布真实副本集名或主机列表。仅在请求时返回 `saslSupportedMechs`，且只包含前端 SCRAM-SHA-256。
- 原生客户端 helper 接受的小写选项别名只适用于其虚拟 token 连接，不适用于注册后端 URL 解析器。客户端仍使用 `vaulty` + 专属 token；客户端的 `retryWrites=false` 不会把 `retryWrites` 或小写别名加入注册 URL 白名单。

## 安全与运维

- 前端是明文传输加 SCRAM 认证，仅限 localhost 或隔离可信网络；后端 TLS 不保护这段链路。不得直接暴露到不可信网络。
- 认证/控制回包、已知错误结构和代理日志不得泄漏真实后端凭据、主机或拓扑。通用错误会刻意省略后端诊断细节。
- 业务文档不脱敏。注册账号有权读取的文档如果已含秘密，也能读出这些秘密；这不在保证内。
- 可信 DBA 修改集合/视图定义，包括检查与使用之间的竞争，不在保证内。需限制后端角色并保留运维控制。
- 上游 MongoDB 日志不由代理脱敏。人工运维应私下检查后端诊断，不把含秘密的日志贴进 AI 会话。
- 宿主密钥/存储不能防同用户权限的恶意进程。agent 应运行在不能接触这些资源的独立隔离域。
- `db regen mongo-app` 为新连接轮换 token；已认证会话保留既有语义。`db off mongo-app` 在通常约两秒的同步后关闭监听，不承诺终止既有会话。`db on mongo-app` 恢复监听。这些控制不是即时会话撤销。
- 同名 `db add` 未指定端口时保留原端口，但替换 URL、生成新 token 并把 `enabled` 重置为 false。需重新分发 token；若要监听须再次显式开启。修改活跃监听端口时，先关闭并等待端口停止，再更新/开启，或重启自己管理的 serve。enabled 是配置，不是健康；自动分配排除注册端口，不探测 OS 占用。已接受的握手可能保留先前 Resolve 状态。
- 与 MongoDB 不同，PG/MySQL/Redis 对新旧注册连接均接受全局 bridge token，轮换专属 token 不撤销该访问权。容器 entrypoint 对 bridge token 只打印 `<set>`/`<unset>` 占位标记，从不输出 token 本身；经环境变量交付的 token 或生成链接仍不得当成无害日志。

## 安全排错

| 症状 | 安全操作 |
|---|---|
| 注册时 `invalid MongoDB connection configuration` | 人工核对注册 URL 白名单：单端点、凭据百分号编码、合法名称/端口、无重复 key、大小写敏感布尔值、不含客户端专用 `retryWrites`、`appName` 或 SRV。只分享合成结构，不分享真实 URL |
| 无监听/选择服务器超时 | 检查自己管理的 serve 启动、watcher 前提、配置端口/接口、防火墙/容器宿主路由。enabled 元数据和 `db test` 成功都不证明前端在监听 |
| 前端认证失败 | 宿主获取当前专属 token；客户端使用 `vaulty`、SCRAM-SHA-256、`authSource=admin` 及正确代理端口。无全局 bridge token 或后端密码兜底；核对轮换/重注册 |
| 后端认证/`db test` 失败 | 人工私下核对注册后端凭据、authSource 优先级、SCRAM 机制和预期副本集名。不得在 AI 会话用 `db show`/直连 shell 探测或发送原始上游日志 |
| 策略/角色拒绝，常见 code `13` | 核对脱敏消息和命令形状，不只看数字：仅 code 13 不能区分代理策略和后端权限。移除不支持的 `comment`/`collation` 或 GUI 管理探测；核对业务命名空间/普通集合及必要的 `listCollections` 权限。不要仅为通过命令就扩大权限或绕过检查 |
| TLS 建立失败 | 人工核对宿主 CA 路径/文件限制、证书信任/主机名和端点配置。不使用不安全选项或明文降级。假后端 TLS 测试是历史证据，不是实际 MongoDB TLS 验收 |
| 长读取/`getMore` 或写入超时 | 上游命令默认截止时间 5000 ms；限制读取范围，私下评估允许的 `connectTimeoutMS`。写入可能已生效：先通过授权读取核对结果，再由人工决定是否重试，绝不盲目重放 |
| `Broken` 条目/token 解密错误 | `Broken` 表示 URL 解密失败，token 解密由 Resolve 单独报错。人工先核对环境变量覆盖和密钥库来源，再考虑重新生成或重注册，并处理新 token/开启状态 |

只报告脱敏错误类别、操作形状、客户端版本和代理端口/状态。业务文档和上游日志可能包含秘密。集合元数据被拒或无法确认时应拒绝操作；ping 成功不是放宽策略的理由。

## 验证状态

以下为 **2026-09-07 实现会话的历史证据**，来源保留于实现记录与设计（归档在 git tag `docs-superpowers-archive`）。平台为 macOS 12.4 Intel、Docker Desktop；MongoDB 8.0.13 standalone 及已认证固定单节点副本集。被测状态是**基于 `7273bb21e8347777058a17fb95a16aa2a17a36dc` 的未提交 Mongo 工作区**，不是该 commit 本身，也不是发布二进制。此处未记录精确 dirty-tree 摘要及其余工具补丁版本，不能仅凭基线 SHA 推断可复现。该 Mongo 工作后续已随 **v0.8.0**（2026-09-07）发布。

已归档的设计解释已接受决策，已归档的计划保留执行来源，不构成长效 lead/worker 分工或可复用分支/提交许可。本指南是带日期验证矩阵的现行维护位置。下表每个通过项都是历史结果，**文档更正期间未重跑**；上方示例未执行。Mongo 工作已包含在已发布的 **v0.8.0** 中，其归档打包了 `docs/` 指南；更早的 0.6.0 包先于 MongoDB，也未附带所链接的 `docs/`。

| 检查 | 状态 / 范围 |
|---|---|
| `bash scripts/mongotest.sh --mongosh` | 已通过：MongoDB 8.0.13 standalone，原生 Go 驱动和容器内原生 `mongosh` |
| `bash scripts/mongotest.sh --replica-set --mongosh` | 已通过：MongoDB 8.0.13 已认证固定单节点副本集；不是多节点故障转移 |
| `GOFLAGS=-race bash scripts/mongotest.sh --replica-set --mongosh` | 竞态检测通过，包含连接池替换原 socket 后继续读取游标 |
| 后端认证 | 已通过：SHA-256/SHA-1 与 `admin`/`businessdb` 认证库交叉的四种组合；完整业务套件在 SHA-256/admin 场景运行 |
| 业务/代理集成 | 已通过：带类型 BSON CRUD、批量操作、只读聚合、业务/元数据分页、普通会话、虚拟身份、策略/错误脱敏、错误/缺失 token、轮换及监听开关 |
| TLS 单测 | 假后端的完整证书/主机名验证已通过；实际 MongoDB TLS 夹具**未运行** |
| 直连 `db shell` | 固定脚本/子进程环境约定由 Node 测试覆盖；实际交互 TTY 会话**未人工验证** |
| 仓库门禁 | 已通过：`make test`（含 UI 检查）、`go test -race ./...`、`go vet ./...`、`go vet -tags=mongointegration ./internal/dbproxy`、`make build` |
| 审查 | 首轮独立审查发现的问题已修正并补回归，最终本地源码复核已完成；自动独立复审不可用，不记为通过 |

脚本还支持 `bash scripts/mongotest.sh`（仅 Go 驱动）和 `bash scripts/mongotest.sh --replica-set`（Go 驱动、已认证单节点副本集）。前置条件为 Docker、Go 和 OpenSSL；`--mongosh` 使用夹具镜像内的客户端，无需宿主安装。每次运行创建随机 loopback 夹具，通过 `VAULTY_MONGO_TEST_URI` 及 `VAULTY_MONGO_TEST_CONTAINER` / `VAULTY_MONGO_TEST_REPLICA_SET` 提供合成配置，只清理带本次归属标签的容器及临时资源。

进程内 Go 测试使用显式 `t.TempDir()` 存储和 `store_test.go` 中固定合成的 `testKey(t)`；随机生成的是夹具密码和隧道 token，不是该存储密钥。它**不需要也不创建临时 HOME**，不读取真实用户存储，不调用 Keychain。不得用真实注册 URL 替换夹具变量。脚本执行：

```sh
go test -mod=readonly -tags=mongointegration ./internal/dbproxy -run '^TestMongoIntegration($|DriverHello$|DriverSASL$)' -count=1 -timeout=180s
```

不提供夹具变量就运行 tagged tests 会跳过真实集成用例，不能作为原生 MongoDB 已通过的证据。上述通过矩阵不代表完整 GUI/自省兼容、实际 MongoDB TLS 或交互 shell 已验证。

本指南源码核对：[注册 URL 解析器](../../internal/dbproxy/mongodb_config.go)、[后端认证/TLS/截止时间](../../internal/dbproxy/mongodb_auth.go)、[持续转发](../../internal/dbproxy/mongodb.go)、[命令/元数据策略](../../internal/dbproxy/mongodb_policy.go)、[客户端链接](../../internal/dbproxy/links.go)、[CLI 提示/直连 shell](../../internal/cli/db.go)、[watcher 启动](../../internal/cli/remote.go)、[存储生命周期](../../internal/dbproxy/store.go)及[原生夹具](../../scripts/mongotest.sh)。MySQL TLS 与 `dbtest.sh` 隔离状态见[安全模型](../security-model.zh-CN.md#8--验证状态)，不在本指南。
