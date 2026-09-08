# PostgreSQL 隧道指南

> 中文 | [English](postgres-tunnel-guide.md)
>
> 当前 PostgreSQL 隧道接口，2026-09-07 按源码核对（工作区行为，非新测试运行）。统一安全边界见[安全模型](security-model.zh-CN.md)；三协议共享机制见[架构指南](db-proxy-architecture.zh-CN.md)；已准备夹具与生命周期见[示例指南](db-proxy-examples.zh-CN.md)。

## 连接模型

注册一个后端 PostgreSQL 端点（普通用户名/密码）。宿主把 URL 与专属 128 位 token 加密保存在 DB store；连接名/类型/端口/状态元数据不加密。这不是"输入文件、导出、终端输出等宿主明文制品不可能存在"的承诺。客户端连接代理端口时把**隧道 token 当作用户名**，密码任意占位（生成链接用 `x`）；代理用注册的用户名、密码和数据库向后端认证。

后端认证完成后代理双向转发原始协议字节：无查询白名单、无结果脱敏。因此后端角色决定 token 持有者实际能做什么。后端参与自身认证（按服务端要求使用 SCRAM-SHA-256/MD5/明文），真实密码可能以明文认证形式经过宿主到后端这一段。不要把它描述成"凭据永不离开宿主"。

PostgreSQL 对**新旧注册**都接受专属 token **或当前 serve 的全局 bridge token**。`db regen` 只轮换专属 token，全局兜底仍然有效。

## 注册 URL

`postgres://` 和 `postgresql://` 都接受。URL 携带后端用户名、密码、主机、端口和数据库；`sslmode` 等查询选项委托给宿主到后端连接上的 PostgreSQL 客户端库。隧道 URI 上的客户端 `sslmode=disable` 是另一回事，只影响前端这一侧。

隧道端口在 `db add`（`--port`）时指定，或从 15432 起自动分配；自动分配只检查已注册端口，不探测 OS 端口占用。同名 `db add` 省略 `--port` 时保留原端口，但替换 URL、生成新 token 并把连接重置为启用。

## 客户端设置

仅当确实缺失时，由人工操作者执行 `vaulty-keeper db init` 初始化宿主 DB 密钥。非空 `VAULTY_KEEPER_DB_KEY` 优先于 keyring，须 Base64 解码为 32 字节，出错不回退。在运行客户端的机器上安装 `psql`；vaulty-keeper 与 agent 镜像默认都不提供。

以下人工工作流预留隧道端口 `15432`，假设后端数据库为 `appdb`。这是用法示例，不是已执行的夹具：

```sh
# 人工宿主终端：通过 stdin 提供后端 URL，不要放进 argv。
vaulty-keeper db add pgdb --port 15432
vaulty-keeper db test pgdb
vaulty-keeper serve --addr 127.0.0.1:8970
```

`db add` 后在 stdin 提示符粘贴授权后端 URL（如 `postgres://app:pgpass@127.0.0.1:5432/appdb`）并回车。当前提示符**会回显**输入；避免录制该终端。先注册再启动 serve：仅当启动时 store 存在且 DB 密钥可用，才会创建 DB watcher。`serve --dir` 选择快照目录；`VAULTY_KEEPER_DB_DIR` 选择 DB store。

`serve` 保持运行。在另一个**宿主**终端（同一 store/密钥上下文）获取仅含代理的连接信息：

```sh
vaulty-keeper db list
vaulty-keeper db connect pgdb
vaulty-keeper db connect pgdb --cmd
```

不要把真实后端 URL/密码放进 AI 消息、shell history 或命令行参数。AI 应使用隧道连接信息，而不是 `db show`、加密 store、宿主密钥或直接后端 shell。

客户端 URI 形态：

```text
postgresql://<专属TOKEN>:x@127.0.0.1:<隧道端口>/<数据库>?sslmode=disable&connect_timeout=5
```

`<...>` 是占位符，不是测试凭据。代理忽略隧道密码，任意占位均可。有界读示例：

```sh
psql 'postgresql://<TOKEN>:x@127.0.0.1:15432/appdb?sslmode=disable' \
  -c 'SELECT id, name FROM public.t ORDER BY id LIMIT 10;'
```

GUI/JDBC 字段使用代理主机/端口与虚拟凭据：主机 `127.0.0.1`、端口 `15432`、用户 `<TOKEN>`、密码 `x`、数据库 `appdb`。JDBC URL 模板：`jdbc:postgresql://127.0.0.1:15432/appdb?user=<TOKEN>&password=x`。GUI 驱动需另行安装。

容器链接替换为 `host.docker.internal`，但 `db connect --container` 只改打印地址。`serve --addr` 的宿主部分决定监听接口；仅 loopback 的 serve 通常无法从 Docker VM 访问。无密钥容器只能运行 `vaulty-keeper remote dblist` 取元数据；在宿主生成隧道命令，只交付获授权凭据。

## 支持的操作

psql/JDBC/psycopg 等对后端账号能做的事，隧道都转发，无逐命令策略：

- DDL、DML、SELECT 与管理命令全部原始转发；代理不是 SQL 防火墙。
- 写操作与破坏性操作只由后端授权与 token 持有者授权决定，代理不干预。
- 会话身份是注册的后端账号：`SELECT current_user` 显示真实会话属主，不是代理 token。
- 后端错误消息、catalog 与查询结果原样透传、不脱敏，可能含秘密（包括明文密码）。

## 协议限制

- 只有在 token 验证与后端认证成功后才开始原始字节转发；没有 MongoDB relay 那样的命令感知分帧。
- 无查询白名单、无只读强制、无结果脱敏。注册只读账号连接自然只读。
- 后端 TLS 委托给客户端库（注册 URL 里的 `sslmode`）。前端代理这一段是明文；请用 localhost 或隔离可信网络。
- 真实密码可能出现在宿主到后端这一段（如明文/SCRAM 交换）；生成的客户端链接只含代理 token、不含后端密码，但查询结果仍可能带回秘密。
- `db regen`/`db off` 不会终止已建立会话；轮换只影响新连接，监听关闭发生在 watcher 约两秒一次的同步时。

## 安全与操作

- `db regen pgdb` 为新连接轮换专属 token；全局 bridge token 仍可认证此连接，因此单独轮换不是对全局路径的撤销。之后重新分发生成链接。
- `db off pgdb` 在下次 watcher 同步关闭监听，不承诺终止现有会话；`db on pgdb` 恢复。这是配置控制，不是会话撤销。
- 隧道 token 只交给获授权 agent/工具；`db connect` 输出是含凭据的命令。容器入口脚本只打印 `<set>`/`<unset>` 标记，从不打印 token 本身，但环境传递的 token 仍是凭据。
- 代理日志含连接名、来源地址与处理错误；部分协议错误可能带后端地址或服务端消息。分享前私下检查并脱敏。
- `db shell pgdb` 在宿主打开直接后端 `psql`（stdin-TTY 门禁）：真实后端上下文，不是脱敏隧道会话，不适合 agent。

## 安全排错

| 症状 | 安全下一步 |
|---|---|
| `db test` 失败 | 人工私下检查注册后端凭据、主机/端口可达性与 `sslmode`。`db test` 检查后端连通/认证，不检查隧道监听或只读授权；成功输出可能含真实用户名/数据库 |
| 启用状态却没有监听 | 检查自有 serve 启动、启动时 store/密钥可用性、`VAULTY_KEEPER_DB_DIR`、绑定接口与防火墙；enabled 元数据不是健康状态 |
| 隧道认证失败 | 宿主获取当前专属 token（或使用有效全局 token）；客户端用 token 作用户名、占位密码、正确端口与数据库。查轮换/重新注册历史 |
| 读成功，写/catalog 读被拒 | 最小权限账号的正常表现；检查后端授权。不要仅为让示例通过而授予管理权限 |
| `Broken` 条目 | 表示存储 URL 解密失败，不是 token 错误。人工检查密钥来源；token 解密失败由 Resolve 单独报告 |
| 端口冲突 | 选一个未占用显式端口并更新客户端；不要杀无关进程 |

`db show` 打印解密后的真实 URL；`db shell` 启动直接后端客户端。它们的 stdin-TTY 检查不确立人工身份。请使用 `db test`、元数据与授权有界查询，不要寻找秘密。

源码核对：[PostgreSQL handler](../internal/dbproxy/postgres.go)、[隧道分发](../internal/dbproxy/tunnel.go)、[store/Resolve](../internal/dbproxy/store.go)、[CLI/db 链接](../internal/cli/db.go)、[watcher 启动](../internal/cli/remote.go)。本指南未运行任何运行时测试或真实数据操作。
