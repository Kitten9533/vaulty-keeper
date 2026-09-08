# Redis 隧道指南

> 中文 | [English](redis-tunnel-guide.md)
>
> 当前 Redis 隧道接口，2026-09-07 按源码核对（工作区行为，非新测试运行）。统一安全边界见[安全模型](../security-model.zh-CN.md)；三协议共享机制见[架构指南](../db-proxy-architecture.zh-CN.md)；已准备夹具与生命周期见[示例指南](../db-proxy-examples.zh-CN.md)。

## 连接模型

注册一个后端 Redis 端点（后端 AUTH 密码及可选数据库索引）。宿主把 URL 与专属 128 位 token 加密保存在 DB store；连接名/类型/端口/状态元数据不加密。客户端连接代理端口后**第一条命令必须是带隧道 token 的 `AUTH`**；代理验证后，用注册的 AUTH 密码向后端认证并 SELECT 注册的数据库，然后开始转发原始协议字节。

后端认证完成后代理双向转发原始 RESP 流量：无命令白名单、无结果脱敏。后端 ACL 与 token 持有者授权决定实际可做什么。

Redis 对**新旧注册**都接受专属 token **或当前 serve 的全局 bridge token**。`db regen` 只轮换专属 token，全局兜底仍然有效。

## 认证与数据流

```text
客户端                         serve                        后端
  | AUTH <token>（首命令）        |                             |
  +----------------------------->| 校验 token                   |
  |                              | 连接 + 后端 AUTH             |
  |                              | + SELECT 注册的数据库        |
  |                              +---------------------------->|
  |                              |<----------------------------+
  | PING / GET ...                |                             |
  +----------------------------->+---------------------------->|
  |<-----------------------------+<----------------------------+
```

客户端**第一条命令必须是带隧道 token 的 `AUTH`**。serve 校验后，用注册的 AUTH 密码向后端认证并 SELECT 注册的数据库。此后双向都是原始 RESP 协议字节；代理不解析、不过滤命令。

## 注册 URL

明文用 `redis://`，后端 TLS 用 `rediss://`。URL 携带后端 AUTH 密码与数据库索引；标准形式为 `redis://:PASSWORD@HOST:PORT/INDEX`（用户名留空，密码放 password 字段）。客户端侧 token 走独立的前端 URI，不是注册 URL。

隧道端口在 `db add`（`--port`）时指定，或从 15432 起自动分配；自动分配只检查已注册端口，不探测 OS 端口占用。同名 `db add` 省略 `--port` 时保留原端口，但替换 URL、生成新 token 并把连接重置为启用。

## 客户端设置

仅当确实缺失时，由人工操作者执行 `vaulty-keeper db init` 初始化宿主 DB 密钥。非空 `VAULTY_KEEPER_DB_KEY` 优先于 keyring，须 Base64 解码为 32 字节，出错不回退。在运行客户端的机器上安装 `redis-cli`；vaulty-keeper 与 agent 镜像默认都不提供。

以下人工工作流预留隧道端口 `15434`，假设后端数据库为 `0`。这是用法示例，不是已执行的夹具：

```sh
# 人工宿主终端：通过 stdin 提供后端 URL，不要放进 argv。
vaulty-keeper db add cache --port 15434
vaulty-keeper db test cache
vaulty-keeper serve --addr 127.0.0.1:8970
```

`db add` 后在 stdin 提示符粘贴授权后端 URL（如 `redis://:redispass@127.0.0.1:6379/0`）并回车。当前提示符**会回显**输入；避免录制该终端。先注册再启动 serve：仅当启动时 store 存在且 DB 密钥可用，才会创建 DB watcher。

`serve` 保持运行。在另一个**宿主**终端（同一 store/密钥上下文）获取仅含代理的连接信息：

```sh
vaulty-keeper db list
vaulty-keeper db connect cache
vaulty-keeper db connect cache --cmd
```

不要把真实后端 URL/密码放进 AI 消息、shell history 或命令行参数。AI 应使用隧道连接信息，而不是 `db show`、加密 store、宿主密钥或直接后端 shell。

客户端调用形态（token 作为 AUTH 密码）：

```sh
redis-cli -h 127.0.0.1 -p 15434 -a <TOKEN>
```

生成链接使用占位用户 `x`；redis-cli 把 token 作为第一条 AUTH 发送。有界读示例：

```sh
redis-cli -h 127.0.0.1 -p 15434 -a <TOKEN> --no-auth-warning GET demo
```

GUI 字段（Redis Insight）使用代理主机/端口与虚拟凭据：`redis://x:<TOKEN>@127.0.0.1:15434/0`。GUI 工具需另行安装；DBeaver 的 Redis 支持取决于版本/插件。

容器链接替换为 `host.docker.internal`，但 `db connect --container` 只改打印地址。无密钥容器只能运行 `vaulty-keeper remote dblist` 取元数据；在宿主生成隧道命令，只交付获授权凭据。

## 支持的操作

redis-cli/GUI 对后端能做的事，初始 AUTH 后隧道都转发：

- `PING`、读、写与管理命令全部原始转发；代理不是命令防火墙。
- 后端 ACL 与注册账号权限决定实际能力；代理不脱敏值。
- 注册的后端密码以后端 `AUTH` 形式经过宿主到后端这一段；前端一段只用隧道 token。

## 协议限制

- 客户端第一条命令必须是带有效 token 的 `AUTH`；认证后代理 SELECT 注册的数据库。
- 无命令白名单、无只读强制、无结果脱敏。
- 前端传输明文；`rediss://` 只配置后端 TLS。请用 localhost 或隔离可信网络。
- `db regen`/`db off` 不会终止已建立会话；轮换只影响新连接，监听关闭发生在 watcher 约两秒一次的同步时。

## 安全与操作

- `db regen cache` 为新连接轮换专属 token；全局 bridge token 仍可认证此连接，因此单独轮换不是对全局路径的撤销。之后重新分发生成链接。
- `db off cache` 在下次 watcher 同步关闭监听，不承诺终止现有会话；`db on cache` 恢复。
- 隧道 token 只交给获授权 agent/工具；`db connect` 输出是含凭据的命令。
- 代理日志与部分协议错误可能带服务端消息；分享前私下检查并脱敏。
- `db shell cache` 在宿主打开直接后端客户端（stdin-TTY 门禁）：真实后端上下文，不是脱敏隧道会话，不适合 agent。

## 安全排错

| 症状 | 安全下一步 |
|---|---|
| `db test` 失败 | 人工私下检查注册后端 AUTH 密码、主机/端口可达性，需 TLS 时用 `rediss://`。成功输出确认后端认证，不代表隧道在监听 |
| 启用状态却没有监听 | 检查自有 serve 启动、启动时 store/密钥可用性、`VAULTY_KEEPER_DB_DIR`、绑定接口与防火墙 |
| 隧道端 `NOAUTH` / 认证错误 | 宿主获取当前专属 token（或使用有效全局 token）；客户端把它作为第一条 AUTH，用正确端口/数据库。查轮换/重新注册历史 |
| 选错数据库 | 确认注册 URL 的数据库索引，且客户端不在 AUTH 之前执行 `SELECT` |
| `Broken` 条目 | 表示存储 URL 解密失败，不是 token 错误。人工检查密钥来源；token 解密失败由 Resolve 单独报告 |
| 端口冲突 | 选一个未占用显式端口并更新客户端；不要杀无关进程 |

`db show` 打印解密后的真实 URL；`db shell` 启动直接后端客户端。它们的 stdin-TTY 检查不确立人工身份。请使用 `db test`、元数据与授权有界查询，不要寻找秘密。

源码核对：[Redis handler](../../internal/dbproxy/redis.go)、[隧道分发](../../internal/dbproxy/tunnel.go)、[store/Resolve](../../internal/dbproxy/store.go)、[CLI/db 链接](../../internal/cli/db.go)、[watcher 启动](../../internal/cli/remote.go)。本指南未运行任何运行时测试或真实数据操作。
