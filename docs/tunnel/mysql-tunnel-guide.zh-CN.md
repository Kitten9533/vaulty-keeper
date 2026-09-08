# MySQL 隧道指南

> 中文 | [English](mysql-tunnel-guide.md)
>
> 当前 MySQL 隧道接口，2026-09-07 按源码核对（工作区行为，非新测试运行）。统一安全边界见[安全模型](../security-model.zh-CN.md)；三协议共享机制见[架构指南](../db-proxy-architecture.zh-CN.md)；已准备夹具与生命周期见[示例指南](../db-proxy-examples.zh-CN.md)。

## 连接模型

注册一个后端 MySQL 端点（普通用户名/密码）。宿主把 URL 与专属 128 位 token 加密保存在 DB store；连接名/类型/端口/状态元数据不加密。客户端连接代理端口时把**隧道 token 当作用户名**，密码任意占位；代理用注册凭据向后端认证，支持 `mysql_native_password` 或 `caching_sha2_password`（含 RSA full authentication）。

后端认证完成后代理双向转发原始协议字节：无查询白名单、无结果脱敏。后端角色决定 token 持有者实际能做什么。后端参与自身认证，因此真实密码会经过宿主到后端这一段交换。

MySQL 对**新旧注册**都接受专属 token **或当前 serve 的全局 bridge token**。`db regen` 只轮换专属 token，全局兜底仍然有效。

## 认证与数据流

```text
客户端                         serve                        后端
  | 虚拟 token（用户名）           |                             |
  +----------------------------->| 校验 token                   |
  |                              | 连接 + 后端认证              |
  |                              |（mysql_native_password /     |
  |                              |  caching_sha2_password）     |
  |                              +---------------------------->|
  |                              |<----------------------------+
  | 查询                         |                             |
  +----------------------------->+---------------------------->|
  |<-----------------------------+<----------------------------+
```

客户端把隧道 token 当作用户名发送，密码任意占位。serve 校验后，用注册凭据连接后端并认证，支持 `mysql_native_password` 或 `caching_sha2_password`（含 RSA full authentication）。此后双向都是原始协议字节；代理不解析、不过滤查询。

## 注册 URL

接受 `mysql://`。URL 携带后端用户名、密码、主机、端口和数据库。可选后端 TLS 用 `?tls=true`（可选 `tlsCAFile=<path>`）配置，见下。

隧道端口在 `db add`（`--port`）时指定，或从 15432 起自动分配；自动分配只检查已注册端口，不探测 OS 端口占用。同名 `db add` 省略 `--port` 时保留原端口，但替换 URL、生成新 token 并把连接重置为启用。

## 后端 TLS

**`?tls=true`（原 C01）：**设置后代理通告 `CLIENT_SSL` capability 位，将宿主到后端连接升级为 TLS，并用升级后的连接做认证与原始转发。加 `tlsCAFile=<path>`（PEM CA 文件，普通文件 ≤1 MiB）以信任私有/自签 CA；不加则按系统根验证后端证书。验证失败即终止连接，不会不安全降级。

证据状态：该修复有单元测试，另有一次一次性原生 TLS 查询（MySQL 8、`require_secure_transport=ON`、自签 CA）报告非空 `Ssl_cipher`（TLSv1.3）且业务查询通过——但该证据**未被本仓库的集成测试固化**。依赖前请对真实 TLS 后端重新验证。前端代理这一段无论是否启用后端 TLS 都是明文；请用 localhost 或隔离可信网络。

## 客户端设置

仅当确实缺失时，由人工操作者执行 `vaulty-keeper db init` 初始化宿主 DB 密钥。非空 `VAULTY_KEEPER_DB_KEY` 优先于 keyring，须 Base64 解码为 32 字节，出错不回退。在运行客户端的机器上安装 MySQL `mysql` 客户端；vaulty-keeper 与 agent 镜像默认都不提供。

以下人工工作流预留隧道端口 `15441`，假设后端数据库为 `shop`。这是用法示例，不是已执行的夹具：

```sh
# 人工宿主终端：通过 stdin 提供后端 URL，不要放进 argv。
vaulty-keeper db add mysql-orders --port 15441
vaulty-keeper db test mysql-orders
vaulty-keeper serve --addr 127.0.0.1:8970
```

`db add` 后在 stdin 提示符粘贴授权后端 URL（如 `mysql://sha2user:sha2pass@127.0.0.1:3306/shop`）并回车。当前提示符**会回显**输入；避免录制该终端。先注册再启动 serve：仅当启动时 store 存在且 DB 密钥可用，才会创建 DB watcher。

`serve` 保持运行。在另一个**宿主**终端（同一 store/密钥上下文）获取仅含代理的连接信息：

```sh
vaulty-keeper db list
vaulty-keeper db connect mysql-orders
vaulty-keeper db connect mysql-orders --cmd
```

不要把真实后端 URL/密码放进 AI 消息、shell history 或命令行参数。AI 应使用隧道连接信息，而不是 `db show`、加密 store、宿主密钥或直接后端 shell。

客户端调用形态（token 作用户名，占位密码 `x`）：

```sh
mysql --no-defaults -h127.0.0.1 -P 15441 -u <TOKEN> -px --ssl-mode=DISABLED
```

`--ssl-mode=DISABLED` 只作用于明文前端这一段，不会关闭后端 `?tls=true`。有界读示例：

```sh
mysql --no-defaults -h127.0.0.1 -P 15441 -u <TOKEN> -px --ssl-mode=DISABLED \
  -e 'SELECT COUNT(*) FROM shop.orders WHERE qty >= 2;'
```

GUI 字段（MySQL Workbench / DBeaver）使用代理主机/端口与虚拟凭据：主机 `127.0.0.1`、端口 `15441`、用户 `<TOKEN>`、密码 `x`、数据库 `shop`。GUI 驱动需另行安装。

容器链接替换为 `host.docker.internal`，但 `db connect --container` 只改打印地址。无密钥容器只能运行 `vaulty-keeper remote dblist` 取元数据；在宿主生成隧道命令，只交付获授权凭据。

## 支持的操作

MySQL 客户端/GUI 对后端账号能做的事，隧道都转发，无逐命令策略：

- DDL、DML、SELECT 与管理命令全部原始转发；代理不是 SQL 防火墙。
- 写操作与破坏性操作只由后端授权与 token 持有者授权决定。`CREATE` 被后端授权拒绝时在后端失败，不是代理过滤。
- `CURRENT_USER()` 可能显示注册的后端账号。
- 查询结果、错误与 catalog 原样透传、不脱敏，可能含秘密。

## 协议限制

- 只有在 token 验证与后端认证成功后才开始原始字节转发。
- 无查询白名单、无只读强制、无结果脱敏。
- 前端传输明文（无前端 SSL）；`?tls=true` 只保护后端段。
- MySQL 原生认证包含 `caching_sha2_password` RSA full authentication，发生在宿主到后端这一段。
- `db regen`/`db off` 不会终止已建立会话；轮换只影响新连接，监听关闭发生在 watcher 约两秒一次的同步时。

## 安全与操作

- `db regen mysql-orders` 为新连接轮换专属 token；全局 bridge token 仍可认证此连接，因此单独轮换不是对全局路径的撤销。之后重新分发生成链接。
- `db off mysql-orders` 在下次 watcher 同步关闭监听，不承诺终止现有会话；`db on mysql-orders` 恢复。
- 隧道 token 只交给获授权 agent/工具；`db connect` 输出是含凭据的命令。
- 代理日志与部分协议错误可能带后端地址或服务端消息；分享前私下检查并脱敏。
- `db shell mysql-orders` 在宿主打开直接后端客户端（stdin-TTY 门禁）。密码使用子进程环境变量，但 MySQL 主机与用户仍可能出现在 argv 中。不是脱敏隧道会话，不适合 agent。

## 安全排错

| 症状 | 安全下一步 |
|---|---|
| `db test` 失败 | 人工私下检查注册后端凭据、主机/端口可达性与 TLS flag。成功输出可能含真实用户名/数据库 |
| 启用状态却没有监听 | 检查自有 serve 启动、启动时 store/密钥可用性、`VAULTY_KEEPER_DB_DIR`、绑定接口与防火墙 |
| 隧道认证失败 | 宿主获取当前专属 token（或使用有效全局 token）；客户端用 token 作用户名、占位密码、正确端口/数据库。查轮换/重新注册历史 |
| `?tls=true` 连接失败 | 检查 CA 信任/`tlsCAFile`（普通文件 ≤1 MiB）、服务端 `require_secure_transport`、主机名与 TLS 版本。绝不绕过必需 TLS；前端一段无论如何都是明文 |
| 读成功，写/DDL 被拒 | 最小权限账号的正常表现；检查后端授权。不要仅为让示例通过而授予管理权限 |
| `Broken` 条目 | 表示存储 URL 解密失败，不是 token 错误。人工检查密钥来源；token 解密失败由 Resolve 单独报告 |

`db show` 打印解密后的真实 URL；`db shell` 启动直接后端客户端。它们的 stdin-TTY 检查不确立人工身份。请使用 `db test`、元数据与授权有界查询，不要寻找秘密。

源码核对：[MySQL handler/TLS](../../internal/dbproxy/mysql.go)、[隧道分发](../../internal/dbproxy/tunnel.go)、[store/Resolve](../../internal/dbproxy/store.go)、[CLI/db 链接](../../internal/cli/db.go)、[watcher 启动](../../internal/cli/remote.go)。C01 TLS 修复有单元测试；其一次性原生 TLS 查询证据未被集成测试固化——依赖前请对真实 TLS 后端重新验证。本指南未运行任何运行时测试或真实数据操作。
