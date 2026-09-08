# 数据库隧道架构

[English](db-proxy-architecture.md) | 中文

当前 PG/MySQL/Redis 行为，2026-09-07 按源码核对。下图是说明图，不是运行环境快照或新测试证据。准备步骤和显式端口见[用法及合成夹具](db-proxy-examples.zh-CN.md)；URL 选项、客户端设置与排错见逐协议操作指南（[PostgreSQL](tunnel/postgres-tunnel-guide.zh-CN.md) / [MySQL](tunnel/mysql-tunnel-guide.zh-CN.md) / [Redis](tunnel/redis-tunnel-guide.zh-CN.md)）；统一安全边界见[安全模型](security-model.zh-CN.md)。

MongoDB 单独维护：[MongoDB 8 指南](tunnel/mongodb-tunnel-guide.zh-CN.md) 负责固定端点、持续命令感知转发、虚拟用户 `vaulty`、专属 token 作为 SCRAM-SHA-256 密码、无全局兜底、已审查 CRUD/只读聚合和脱敏控制元数据。下方原始字节转发图不适用于 MongoDB。任何协议都不承诺即时撤销会话。

## 总览

```text
隔离客户端域                             宿主
psql / mysql / redis-cli / GUI            vaulty-keeper serve
  | 代理 token + 隧道端口                    |
  +---------------------------------------> TCP 监听
                                             | 解密注册 URL
                                             | 使用注册凭据认证后端
                                             +------------------> PG / MySQL / Redis
  <---------------- 查询结果、错误、原始协议流量 --------------------------------+

remote list/get/compare/dblist ------------> HTTP 桥（/api 需要全局 token）
                                             | Apollo 掩码读取 / DB 元数据

宿主准备：db add / db connect / db test、DB 密钥及存储
客户端执行：原生客户端使用受控交付的代理凭据
```

`serve` 在一个进程内提供 HTTP 和 TCP 服务。每条数据库注册连接有独立隧道端口，与 HTTP 端口无关。后端可以是 loopback 测试容器、内网服务器或云数据库。容器中的 `127.0.0.1` 指容器自身；Docker Desktop 或 Linux `host-gateway` 配置可提供 `host.docker.internal` 宿主路由。

## 存储与输入

```text
人工宿主终端：db add <name>，随后通过 stdin 输入后端 URL
  | 当前终端输入会回显；管道不会删除生产端 shell 的历史
  v
DB 密钥：非空 VAULTY_KEEPER_DB_KEY 优先，否则读取系统密钥库
  | AES-256-GCM
  v
db.json（0600）：URL 和专属 token 加密；名称/类型/端口/状态元数据可读
  | db connect / db test / serve 的 Resolve 在宿主解密
  v
通过宿主到数据库的连接完成后端认证
```

加密承诺覆盖存储的 URL/token 值，不覆盖所有字段或明文产物。快照 value 也加密，但导入源文件、导出/下载、CLI 编辑临时文件及独立 `aes.json` 的 key/IV 配置可能含明文。宿主 CLI/UI 也能解密；明文并非只存在于 `serve`。进程退出不等于安全擦除。

三把存储密钥在独立配置时彼此独立。新格式敏感快照值使用敏感值密钥；旧格式敏感值可能仍回退快照密钥。非空环境变量优先于系统密钥库，必须是解码后 32 字节的 Base64；错误覆盖值不会自动回退。重新生成密钥可能使原数据无法解密，应先由人工私下检查密钥来源。同用户进程不在存储防护边界内。完整密钥和明文生命周期及独立外部 AES 层见[安全模型](security-model.zh-CN.md)。

stdin 使 URL 不进入 vaulty-keeper 的 argv，但 `printf '真实 URL' | ...` 仍可能暴露于生产端 shell 历史、跟踪输出或进程参数。示例行内凭据仅为合成数据。人工注册目前是回显的单行提示，不是隐藏密码提示。不要把真实 URL 放入 AI 消息、脚本或录制终端。`aes gen-key` 会打印生成的秘密，不是掩码诊断。

## 认证与数据流

| 协议 | 客户端到隧道 | 隧道到后端 | 认证后 |
|---|---|---|---|
| PostgreSQL | 用户名放 token；密码忽略（生成链接使用 `x`）；trust 风格 `AuthenticationOk` | 注册用户名、密码和数据库；按服务端要求执行 SCRAM-SHA-256/MD5/明文认证 | 原始字节转发 |
| MySQL | 用户名放 token；密码任意占位；前端无 SSL | 注册凭据；`mysql_native_password` 或 `caching_sha2_password`，含 RSA 完整认证 | 原始字节转发；后端 TLS 用 `?tls=true`（可选 `tlsCAFile`） |
| Redis | 首命令必须为携带 token 的 `AUTH`（生成 URI 的密码字段，用户名占位 `x`） | 注册 AUTH 和数据库 SELECT | 原始字节转发 |

```text
客户端                         serve                        后端
  | 虚拟 token                   |                             |
  +----------------------------->| 校验 token                   |
  |                              | 连接 + 后端认证              |
  |                              +---------------------------->|
  |                              |<----------------------------+
  | 查询                         |                             |
  +----------------------------->+---------------------------->|
  |<-----------------------------+<----------------------------+
```

客户端不必知道注册密码，但后端必然参与自己的认证。Redis AUTH 和 PostgreSQL 明文认证可能在后端链路传输真实密码，不能描述成凭据永不离开宿主。前端是明文传输，后端 TLS 不保护前端链路。仅用于 loopback 或隔离可信网络，并单独实施网络限制。

**MySQL `?tls=true`（原 C01）：** 设置 `tls=true` 时代理会声明 `CLIENT_SSL` 能力位，将后端连接升级为 TLS，并用升级后的连接完成认证和原始转发。如需信任私有/自签 CA，加 `tlsCAFile=<路径>`（PEM CA 文件，≤1 MiB）；不指定时按系统根证书验证后端证书。曾做过一次性原生 TLS 查询（MySQL 8、`require_secure_transport=ON`、自签 CA），`Ssl_cipher` 非空（TLSv1.3）且业务查询通过，但该证据未被本仓库的集成测试固化——依赖它之前请对真实 TLS 后端复测。明文模式不变。PostgreSQL/Redis 各有自己的 TLS 路径，本节不为其提供新的原生 TLS 证据。

## Token 与监听

| 控制 | 当前效果 |
|---|---|
| `db add` | 生成加密存储的 128 位随机专属 token，默认开启 |
| PG/MySQL/Redis 认证 | **新注册和旧连接都接受**专属 token **或当前 serve 的全局 bridge token** |
| `db connect` | 需要本地 DB 密钥和 Resolve 的宿主命令；有专属 token 就打印它，旧条目缺失时打印全局 token |
| `db regen` | 为后续连接认证替换专属 token；不轮换全局 token，不终止已建立会话 |
| `db off` / `db rm` | 同步时关闭监听；不主动终止已建立会话 |
| `db on` | 同步时尝试恢复监听；enabled 是期望配置，不是健康证明 |
| 同名 `db add` | 替换 URL、生成新 token、重置为开启；未指定 `--port` 时保留已存端口。需重新分发 token，必要时显式恢复关闭状态 |

每次接受连接都会 Resolve；正在握手的连接可能已持有旧 token。轮换不是全局会话撤销屏障。监听已开启时修改存储端口，不会自动重新绑定该监听：先关闭并等待端口停止，再开启，或重启自己管理的 serve 进程。既有会话需另行处置。

```text
serve 启动
  +-- 生成全局 token；HTTP 桥写 ~/.vaulty/bridge-token（0600）并打印
  +-- 启动时已有 DB 存储且 DB 密钥可用？
        是：启动 watcher -> 首次同步 -> 约每 2 秒重复
            尝试监听启用条目；记录失败
        否：仅 HTTP 桥；之后首次注册/配置密钥需要重启 serve
```

DB 存储由 `VAULTY_KEEPER_DB_DIR` 或默认位置选择；`serve --dir` 指定的是**快照**目录，不是 DB 目录。`serve --addr` 的 host 部分也决定 DB 监听接口。`db connect --container` 只改**打印地址**，不改监听或防火墙。只有明确配置接口/防火墙控制时才使用 `0.0.0.0`，它会将 HTTP 和 DB 端口暴露到 loopback 之外。自动端口分配从 15432 起跳过已注册端口，不探测 OS 占用。HTTP 启动成功或 UI enabled 标记不能证明 DB 监听/后端健康。

## 访问实际授予什么

PG/MySQL/Redis 对后续协议流量不实施查询白名单或结果脱敏。注册专用最小权限账号；禁止写入时需在后端授予只读权限。SQL 可以暴露真实会话属主（`current_user`、`CURRENT_USER()`）、服务器地址及高权目录。业务数据、错误或服务器配置可能含秘密，包括明文密码。生成链接不泄漏注册密码，不代表查询不能返回秘密。

代理日志包含连接名、来源地址和 handler 错误；旧协议的部分错误会包含后端地址或服务端消息。它们不是统一脱敏或完整逐查询审计流。应私下查看、脱敏后分享，不得从日志、存储或目录中搜寻秘密。

HTTP 掩码桥没有写入 API，连显式 safe 的 Apollo 值也会掩码。但其全局 token **并非无害**：同一 token 也授予 PG/MySQL/Redis 访问权，包括注册账号允许的写入。本地 CLI/UI 是不同接口：显式 safe 值可通过 GET 返回明文，UI DB-connect GET 无需 UI 写 token 即可返回可用 token/链接。`remote dblist` 只返回元数据，不返回 token。`remote get` 打印掩码，list/compare 提供指纹。指纹基于同一 HMAC 密钥下的归一化值，截断为八字节；相同是高置信信号，不是原始字节等值证明。显示长度使用 UTF-8 字节，尽管标签写着 `chars`。快照流程见 [Apollo 指南](apollo-snapshot-guide.zh-CN.md)。

TTY 门禁检查 stdin 是否为终端，不验证真人身份或 stdout 去向；伪终端也能满足检查。agent 不得调用明文出口（`db show`、直连 `db shell`、reveal/export/decrypt），不得伪造 TTY 或使用宿主密钥绕过操作约束。

## Docker 角色与暴露

数据库夹具和 agent 隔离是不同角色。数据库镜像带原生客户端，不代表宿主或 agent 镜像已安装。仓库 Dockerfile 在 Go 构建阶段编译 vaulty-keeper，运行镜像包含 Node、git 和非 root agent 用户。agent CLI（含 Codex）可选安装。数据库客户端/驱动需在执行域另行安装。

Compose 丢弃 capabilities、启用 `no-new-privileges`，挂载项目及持久化 agent home，不主动挂载宿主密钥/存储或 Docker socket。它**没有**强制桥为唯一网络出口。项目挂载内的秘密仍可访问；广泛出口、宿主服务、容器逃逸及宿主同用户访问需另行控制。entrypoint 对 bridge token 只打印 `<set>`/`<unset>` 占位标记，从不输出 token 本身；但交付给容器的凭据仍可能留在持久化历史/日志中。只交付经过授权的代理凭据，不交付宿主 DB 密钥或后端真实 URL。只授权一条连接时，不得把全局 token 当成单连接凭据分发。

**`scripts/dbtest.sh` 已隔离重构，可以安全运行（C02 完成）。** 当前脚本按 PID 与容器标签跟踪自己启动的 serve 和容器，使用每次运行独立的临时目录/容器名和假 HOME、合成密钥，`--clean` 只清理登记的运行——不再宽泛 pkill、不使用固定容器名（`aipg`、`aimysql8`、`aimariadb`、`airedis`）、不覆盖真实 HOME 的 bridge-token。使用前先读脚本头注释。镜像是 `postgres:17.6-alpine`、MySQL **8.0**（`dockerproxy.net/library/mysql:8.0`，历史为 8.0.46）和 `redis:7`，不是 MySQL 8.4/MariaDB。[示例](db-proxy-examples.zh-CN.md) 提供未执行的合成步骤作为替代走查，不是硬性要求。

## 源码与证据

现行行为依据：[存储/Resolve](../internal/dbproxy/store.go)、[监听同步和分发](../internal/dbproxy/tunnel.go)、[PostgreSQL](../internal/dbproxy/postgres.go)、[MySQL](../internal/dbproxy/mysql.go)、[Redis](../internal/dbproxy/redis.go)、[CLI 输入/链接/shell](../internal/cli/db.go)、[serve 启动](../internal/cli/remote.go)、[取钥匙](../internal/apollo/keyring.go)、[UI 连接输出](../internal/ui/db.go)、[Compose](../docker-compose.yml) 和 [entrypoint](../docker/agent-entrypoint.sh)。

本指南描述工作区实现。最新发布 **v0.8.0** 已包含当前 DB 隧道与 MongoDB 行为并打包 `docs/` 指南；预编译 0.6.0 归档先于两者，且缺少所链接的 `docs/`。本次文档更正未生成发布包，未运行运行时测试、真实 TLS 测试或人工 TTY 检查。带日期的历史 Mongo 验证矩阵仅由 [Mongo 指南](tunnel/mongodb-tunnel-guide.zh-CN.md#验证状态) 维护。
