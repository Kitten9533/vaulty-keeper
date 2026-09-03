# 数据库隧道代理实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 ai-tools 增加"数据库隧道代理"：URL 加密存 host（独立 DB 密钥 + `~/.ai-tools/db.json`），`serve` 同时起 TCP 隧道，容器内 AI 用原生客户端（psql/mysql/redis-cli）连代理端口，代理在握手阶段注入真实凭据；AI 永远看不到 DSN。

**Architecture:** 扩展 `ai-tools serve` 一个守护进程：HTTP 掩码桥（已有）+ 每连接一个 TCP 隧道。隧道按库实现认证注入：PG（假 server 要 cleartext + 真实 client 完成认证，pgproto3）、MySQL（手写 handshake 双端 + 认证应答替换，go-mysql 参考）、Redis（代发 AUTH + 转发，手写 RESP）。token 门控：PG 的 user 字段 / MySQL 的 username 字段 / Redis 的 AUTH 首命令。

**Tech Stack:** Go，新增依赖 `github.com/jackc/pgx/v5`（pgproto3）、`github.com/xdg-go/scram`（PG SCRAM）、`github.com/go-mysql-org/go-mysql`（仅参考/客户端，见任务6）。Redis 手写 RESP 零依赖。

参考 spec：`docs/superpowers/specs/2026-08-31-db-proxy-tunnel-design.md`

---

## 文件结构

- `internal/apollo/keyring.go`（改）：加 DB 密钥常量 + `DBKey()` + `GenerateAndStoreDBKey()`（复用平台 keyring）
- `internal/dbproxy/store.go`（新）：db.json 读写、URL 加密/解密、scheme 识别、端口分配
- `internal/dbproxy/store_test.go`（新）
- `internal/dbproxy/tunnel.go`（新）：隧道服务器框架（监听、token 门控、按类型分发）
- `internal/dbproxy/redis.go`（新）：Redis 隧道
- `internal/dbproxy/postgres.go`（新）：PG 隧道（pgproto3 + SCRAM）
- `internal/dbproxy/mysql.go`（新）：MySQL 隧道（手写 handshake）
- `internal/dbproxy/tunnel_test.go`（新）
- `internal/bridge/bridge.go`（改）：加 `/api/db/list` 端点（cfg 注入 DBDir）
- `internal/cli/db.go`（新）：`db init/add/list/rm/shell` 子命令
- `internal/cli/remote.go`（改）：`remote` 增加 `dblist`（容器侧读 `/api/db/list`）
- `internal/cli/cli.go`（改）：挂 `db` 命令 + usage
- `README.md`、`AGENTS.md`（改）：文档

---

## Task 1: DB 密钥（apollo keyring）

**Files:** Modify `internal/apollo/keyring.go`, `internal/apollo/key_test.go`

- [ ] 在 keyring.go 加常量 `DBKeychainAccount = "db-key"`、`EnvDBKey = "AI_TOOLS_DB_KEY"`，函数 `DBKey()`（env 优先→keyring，32 字节 base64 校验）、`GenerateAndStoreDBKey(force bool)`——镜像 `SnapshotKey`/`GenerateAndStoreKey` 的实现
- [ ] key_test.go 加测试：`DBKey` env 覆盖读取、`GenerateAndStoreDBKey` 幂等拒绝
- [ ] `go test ./internal/apollo/...` 通过

## Task 2: dbproxy store（db.json）

**Files:** Create `internal/dbproxy/store.go`, `internal/dbproxy/store_test.go`

- [ ] 结构：`Conn{Name, Type, URL string, Port int}`（URL 只在内存出现）、`Store{Conns map[string]storedConn}`，storedConn = `{url_cipher, nonce string, port int}`
- [ ] `ConnTypeFromURL(raw string) (string, error)`：postgres/postgresql→postgres；mysql→mysql；redis/rediss→redis；其余报错
- [ ] `encryptConn/decryptConn`（AES-256-GCM，apollo 同款代码）
- [ ] `Load(path string, key func() ([]byte, error))`、`Add(path, name, rawURL string, port int)`（加密写回，0600，MkdirAll 0700）、`Remove`、`List`（只回 name/type/port，不回 URL）
- [ ] 端口分配：`AllocPort(store, requested int, used map[int]bool, base int)` 从 base=15432 递增避开冲突
- [ ] 测试：加/删/列往返、URL 识别、端口冲突顺延、文件不含明文 URL

## Task 3: CLI `db` 子命令

**Files:** Create `internal/cli/db.go`; Modify `internal/cli/cli.go`

- [ ] `runDB(args)` 分发：init/add/list/rm/shell
- [ ] `db init [--force]`：`apollo.GenerateAndStoreDBKey`
- [ ] `db add <name> [--port N]`：DSN 只从 stdin 读（非 TTY 无输入→报错提示），`dbproxy.Add`
- [ ] `db list [--json]`：本地读 store（无 db.json/无密钥时 fallback 到 bridge `/api/db/list`）
- [ ] `db rm <name> --yes`
- [ ] `db shell <name>`：TTY-only（非 TTY 拒绝），解密 URL 后 exec 原生客户端（psql/mysql/redis-cli）
- [ ] cli.go：`case "db": return runDB(...)` + usage 行
- [ ] 测试：db add 从 stdin 输入、list 掩 URL、非 TTY shell 拒绝

## Task 4: Redis 隧道

**Files:** Create `internal/dbproxy/redis.go`; `internal/dbproxy/tunnel.go`

- [ ] `tunnel.go`：`StartTunnels(ctx, store, addr string, token string, log)` — 每连接起一个 `net.Listen`，accept 后按 type 分发到对应 handler
- [ ] `redis.go`：读客户端首命令（RESP 数组解析，`AUTH <token>` 校验）→ 回 +OK → dial 真实（rediss 走 TLS）→ 发 `AUTH <真实密码>`（URL 无密码则跳过）→ splice `io.Copy` 双向
- [ ] 测试：起一个本地 fake redis server（内存实现 AUTH+ECHO），走隧道验证 token 门控、真实认证、查询透传

## Task 5: PostgreSQL 隧道

**Files:** Create `internal/dbproxy/postgres.go`

- [ ] 客户端侧（假 server，pgproto3 Backend）：收 SSLRequest 回 'N'；收 StartupMessage 校验 `user==token`；发 `AuthenticationCleartextPassword`；收 PasswordMessage 忽略；发 `AuthenticationOk` + 几个 `ParameterStatus` + `BackendKeyData` + `ReadyForQuery`
- [ ] 真实侧（pgproto3 Frontend）：按 URL host/port/sslmode dial（TLS）；发 StartupMessage(user=真实, database=真实)；循环收消息处理：Cleartext→PasswordMessage(真密码)；MD5→计算；SASL→xdg-go/scram 客户端流程；直到 AuthenticationOk + ReadyForQuery
- [ ] 两侧就绪后 splice 裸字节
- [ ] 测试：本地起真 PG（docker）或跳过标记；至少测 token 校验、假 server 握手消息序列（用 pgproto3 假 client 对拍）

## Task 6: MySQL 隧道

**Files:** Create `internal/dbproxy/mysql.go`

- [ ] 客户端侧（假 server）：发 HandshakeV10（自产 salt，plugin=native_password，不广告 SSL）；收 HandshakeResponse41 校验 username==token；发 OK 包
- [ ] 真实侧：读真实 server HandshakeV10；按 plugin 计算认证应答（native：SHA1 两次 + salt XOR；caching_sha2 fast：SHA256 XOR；full auth：TLS 下明文 / 非 TLS 走 RSA 公钥）；发 HandshakeResponse41；处理 auth-more-data；直到 OK
- [ ] splice 裸字节
- [ ] 测试：fake mysql server 对拍（native_password），docker mysql 可选

## Task 7: serve 集成 + 桥 `db list` + 容器侧

**Files:** Modify `internal/bridge/bridge.go`, `internal/cli/remote.go`, `internal/cli/cli.go`

- [ ] bridge.Config 加 `DBDir string`；`/api/db/list` 端点（token 门控已有）返回 `[{name,type,port}]`
- [ ] `runServe`：生成 token（与 bridge 同款）→ 先起 dbproxy.StartTunnels(ctx,...) → 再 bridge.Start(ctx, cfg.Token=...)
- [ ] `remote` 加 `dblist` 子命令：读 `AI_TOOLS_BRIDGE_ADDR/TOKEN` → GET `/api/db/list`
- [ ] 测试：bridge handler 单测 db/list；端到端本地 serve + redis 隧道 + remote dblist

## Task 8: 文档与收尾

**Files:** Modify `README.md`, `AGENTS.md`

- [ ] README：「数据库隧道代理」章节（命令、用法示例、安全边界、容器内连接示例）
- [ ] AGENTS.md：命令清单加 db 命令；安全模型补 DB 隧道层
- [ ] `make build` + `go test ./...` + `go vet ./...` + `go test -race ./...` 全绿

## 明确不做

MongoDB 隧道；代理层只读强制；连接池/查询缓存；多语句复用。
