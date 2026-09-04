# 数据库隧道代理（db-proxy-tunnel）设计

日期：2026-08-31
状态：已确认（用户逐项拍板）

## 目标

让容器/隔离域里的 AI 能通过**原生数据库客户端**（psql / mysql / redis-cli 等）查询数据库并拿到数据，但数据库连接 URL（地址、账号、密码）**绝不暴露给 AI**。URL 只以密文形式存在 host 的 vaulty-keeper 里。

一句话：**只加密链接 URL + 做一个懂协议的隧道代理，在握手阶段注入真实凭据**。不做数据库数据加密，代理层不强制只读。

## 范围

- **做**：PostgreSQL、MySQL、Redis 三个数据库的认证注入隧道
- **不做**：MongoDB（无成熟 Go 代理库，SCRAM 双端终止成本高；用户自行在 host 用 mongosh 查询，凭据不出 host）
- **不做**：代理层强制只读（只读靠注册的 URL 本身是只读账号）；数据库数据加密

## 架构总览

```
[Docker 容器：AI agent]
  psql "postgresql://$TOKEN:x@host.docker.internal:15432/appdb"
  redis-cli -a "$TOKEN" -p 15433
  mysql -h host.docker.internal -P 15434 -u "$TOKEN" -p任意
        │   TCP（带 token 门控）
        ▼
[Host: vaulty-keeper serve --addr 0.0.0.0:8970]
  HTTP 桥（原有 /api/*，db list 走这里）＋ DB 隧道监听（每连接一个 TCP 端口）
  隧道：校验 token → 用 db.json 里的真实 URL 连真实库（含 TLS 配置）
        → 握手注入真实凭据 → 之后纯字节转发
        ▼
  真实数据库
```

- 容器侧只拿到查询结果，从不接触 DSN/凭据
- 连接名（`prod`、`cache`）不保密，AI 用 `vaulty-keeper db list` 经 HTTP 桥可见（返回连接名、类型、隧道端口；host 部分与 `VAULTY_KEEPER_BRIDGE_ADDR` 的 host 一致，容器内即 `host.docker.internal`）
- 复用现有 serve 的 token 门控 + 限速 + 失败 backoff 体系；`internal/dbproxy` 包承载实现，TCP 隧道监听地址跟随 `--addr`

## 存储与密钥

- `vaulty-keeper db init`：创建独立 DB 密钥（macOS Keychain / Windows 凭据管理器，env 兜底 `VAULTY_KEEPER_DB_KEY`）——与快照密钥、敏感值密钥独立：快照密钥泄露也解不开 DSN
- `~/.vaulty/db.json`（0600，每连接 URL 用 AES-256-GCM 单独加密）：

```json
{
  "connections": {
    "prod":  { "url_cipher": "<base64 AES-256-GCM>", "port": 15432 },
    "cache": { "url_cipher": "<base64 AES-256-GCM>", "port": 15433 }
  }
}
```

- 类型从 URL scheme 自动识别：`postgres://`/`postgresql://` → postgres，`mysql://` → mysql，`redis://`/`rediss://` → redis；`--type` 不需要
- `port` 可选；未指定时 serve 启动自动分配（基准 15432，按注册顺序递增），冲突则顺延

## 命令面（本地与容器内同形）

```sh
vaulty-keeper db init                                        # 建 DB 密钥
printf 'postgres://u:p@db.example.com:5432/mydb' \
  | vaulty-keeper db add prod [--port 15432]                 # DSN 走 stdin，不进 argv/shell history
vaulty-keeper db list [--json]                               # 连接名 + 类型 + 代理地址
vaulty-keeper db rm <name> --yes
vaulty-keeper db shell <name>                                # host 上用解密后的 URL 拉起原生客户端（TTY-only）
```

- `db add` 的 DSN 只从 stdin 读取；非 TTY 下无 stdin 输入则报错提示用法
- `db shell`：PG→psql、MySQL→mysql、Redis→redis-cli、Mongo→mongosh；非 TTY 一律拒绝（与 reveal/edit 同一门禁）；用于用户在 host 自查，不涉及代理

## 隧道机制（按库）

### PostgreSQL — pgproto3（pgx/v5 内置）

- 客户端连接后：代理以"假 server"身份与客户端完成握手（认证任意/校验 token），同时以真实 URL 的 user/password 与真实库完成握手（`sslmode` 等按 URL）
- 两端都进入 ready 后，splice 成裸 TCP 双向转发
- **token 门控**：客户端 startup message 的 `user` 字段 == bridge token（`postgresql://<TOKEN>@host.docker.internal:15432/appdb`）；不匹配直接断开
- 数据库名、真实账号密码一律用 URL 里的，忽略客户端传入

### MySQL — go-mysql-org/go-mysql 的 server + client 包

- `server.NewCustomizedConn` + 自定义 handler：客户端侧完成握手（token 放 username 字段校验），同时用 URL 真实凭据 dial 真实库完成认证
- 支持 `mysql_native_password`、`caching_sha2_password`、`sha256_password`（go-mysql server 包已内置）
- 双方认证完成后 splice 裸 TCP 转发
- **token 门控**：客户端 handshake response 的 username == bridge token；密码字段任意

### Redis — tidwall/redcon 或手写 RESP

- accept 后先向真实库发 `AUTH <真实密码>`（`rediss://` 则 TLS 拨号），读 +OK
- **token 门控**：客户端首命令必须是 `AUTH <TOKEN>`（`redis-cli -a "$TOKEN"`）；校验后回复 +OK，再转发客户端后续字节
- 之后纯字节转发

## Token 门控与安全

- 隧道监听地址跟随 serve 的 `--addr`：默认 `127.0.0.1`；容器要连需 `--addr 0.0.0.0:8970`，隧道同样绑 0.0.0.0
- 0.0.0.0 暴露风险由 token 门控抵消：无 token 的局域网用户连上即被断开；token 128 位随机（与现有 HTTP 桥同款），暴力不可行
- 所有隧道连接写审计日志（时间、来源 IP、连接名、是否通过校验），不记录 SQL/数据内容
- 真实凭据只在 host 内存中出现，永不落盘明文、永不回传容器

## 错误处理

- 隧道 accept 后：token 校验失败 → 记录 + 断开（不泄露原因细节给对端）
- 真实库连接失败（网络/DNS/认证）→ 记录 host 日志，对端断开
- 隧道中途断连 → 对端同步断开，释放资源
- serve 启动时端口冲突 → 该连接隧道起失败，打印告警，其余照常
- db.json 损坏 / 密钥缺失 → 隧道整体不启动，HTTP 桥照常（db list 返回错误）

## 测试与验证

- 单元测试：db.json 加密/解密、URL scheme 识别、端口分配、token 校验逻辑
- 集成测试（可用 docker 起的真实 PG/MySQL/Redis 或本地实例）：
  - psql / mysql / redis-cli 通过隧道完成真实查询，host 侧 URL 不出现在任何日志/回包
  - token 错误/缺失 → 连接被拒
  - 每个库的认证模式各测一次
- `go test ./...`、`go vet ./...`、`go test -race ./...`
- 容器端到端：host 起 serve（0.0.0.0），容器内经 host.docker.internal 用原生客户端查询成功

## 依赖新增

- `github.com/jackc/pgx/v5`（仅用其 pgproto3 包；原独立 pgproto3 仓库已归档并入 pgx）
- `github.com/go-mysql-org/go-mysql`（server + client 包）
- Redis 侧优先手写最小 RESP（AUTH 两个命令），避免引入 go-redis

## 里程碑

1. v1：db 存储层（init/add/list/rm + db.json + DB 密钥）+ Redis 隧道（最简，先打通全链路）
2. v2：PostgreSQL 隧道（pgproto3）
3. v3：MySQL 隧道（go-mysql）
4. v4：容器端到端验证 + README/AGENTS.md 文档 + `db shell`

## 明确不做（防范围蔓延）

- MongoDB 代理（用户 host 自用 mongosh）
- 代理层 SQL 解析 / 只读强制（靠 DB 账号权限）
- 数据库数据加密
- 连接池 / 多语句复用 / 查询缓存
