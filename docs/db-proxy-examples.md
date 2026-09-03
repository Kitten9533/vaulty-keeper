# ai-tools 数据库隧道 · 使用示例

> 全部命令都在本地 Docker 测试环境实测过（`scripts/dbtest.sh` 起的）。
> 端口/token 每次跑脚本会变，示例里写的是当次值，套用请先 `db list` 和 `cat ~/.ai-tools/bridge-token` 换成你的。
> 配套：架构图解 `docs/db-proxy-architecture.md`。

---

## 1 · 一分钟上手

```sh
# ① 建 DB 密钥（Keychain；env 兜底 AI_TOOLS_DB_KEY）
ai-tools db init

# ② 注册连接（URL 走 stdin，不进命令行）
printf 'postgres://app:pgpass@127.0.0.1:59918/appdb' | ai-tools db add pgdb
printf 'mysql://sha2user:sha2pass@127.0.0.1:59919/shop' | ai-tools db add mysql-orders
printf 'redis://:redispass@127.0.0.1:59920/0' | ai-tools db add cache

# ③ 看连接清单（只列名字/类型/端口，不返回 URL）
ai-tools db list
#   cache (redis) :15434
#   mysql-orders (mysql) :15441
#   pgdb (postgres) :15432

# ④ 起 serve（掩码桥 + 全部隧道；token 写 ~/.ai-tools/bridge-token）
ai-tools serve --addr 0.0.0.0:8972
#   serve 会持续运行并每 2 秒同步 db.json：之后 db add / db rm 都即时生效，不用重启

# ⑤ 让 ai-tools 生成带 token 的完整客户端命令，直接复制执行
ai-tools db connect pgdb
#   # pgdb (postgres) — token 是 bridge token，不是真实数据库密码（隧道端口 15432）
#   原始隧道链接（AI / 其他工具可据此自行转换）:
#     postgresql://5d321a50...@127.0.0.1:15432/appdb
#   psql / libpq（psql、pgAdmin、DBeaver 均可粘贴）:
#     postgresql://5d321a50...@127.0.0.1:15432/appdb
#   DBeaver / DataGrip / IntelliJ（JDBC）:
#     jdbc:postgresql://127.0.0.1:15432/appdb?user=5d321a50...
#   pgAdmin4（填字段）: Host=127.0.0.1 Port=15432 Database=appdb Username=5d321a50... 密码留空
ai-tools db connect cache
#   ... Redis Insight / DBeaver URL + redis-cli 命令
ai-tools db connect pgdb --cmd        # 只要单行命令（脚本用）
ai-tools db connect pgdb --container  # 换成 host.docker.internal
```

## 2 · 多个同类连接（3 个 MySQL，各自独立隧道）

```sh
# 同一个 MySQL 服务器、3 个不同库 → 3 条连接、3 个端口
printf 'mysql://u:p@host:3306/shop'           | ai-tools db add mysql-orders    --port 15441
printf 'mysql://u:p@host:3306/shop_billing'   | ai-tools db add mysql-billing   --port 15442
printf 'mysql://u:p@host:3306/shop_reporting' | ai-tools db add mysql-reporting --port 15443

ai-tools db list
#   mysql-orders    (mysql) :15441
#   mysql-billing   (mysql) :15442
#   mysql-reporting (mysql) :15443

# 每个连接独立操作（端口不同，互不影响）
ai-tools db connect mysql-orders     # mysql ... -P 15441 ... shop
ai-tools db connect mysql-billing    # mysql ... -P 15442 ... shop_billing
ai-tools db connect mysql-reporting  # mysql ... -P 15443 ... shop_reporting
```

`--port` 不指定就自动分配（基准 15432 递增，冲突自动顺延）；手动指定时不能和其他连接撞车。

## 3 · 各种客户端的连法

先认识这些客户端是什么（`ai-tools db connect` 输出的每个名字）：

| 客户端 | 是什么 | 适合谁 |
|---|---|---|
| **psql** | PostgreSQL **自带的命令行**客户端（装 PostgreSQL 就有） | 命令行党 / 脚本 |
| **mysql** | MySQL **自带的命令行**客户端（装 MySQL 就有） | 命令行党 / 脚本 |
| **redis-cli** | Redis **自带的命令行**客户端（装 Redis 就有） | 命令行党 / 脚本 |
| **DBeaver** | 通用数据库**图形**工具（一款软件连 PG/MySQL/Redis…），用 JDBC 链接 | 图形界面党 |
| **pgAdmin4** | PostgreSQL **官方图形**工具 | 图形界面党 |
| **MySQL Workbench** | MySQL **官方图形**工具 | 图形界面党 |
| **Redis Insight** | Redis **官方图形**工具 | 图形界面党 |

> 命令行客户端（psql/mysql/redis-cli）都是"装数据库时自带的黑窗口工具"，不用单独安装；
> 图形工具（DBeaver/pgAdmin4/Workbench/Insight）是要单独下载安装的软件。

### psql（PG）
```sh
TOKEN=$(cat ~/.ai-tools/bridge-token)
# 本机
psql "postgresql://$TOKEN@127.0.0.1:15432/appdb"
# 容器/AI 隔离域
psql "postgresql://$TOKEN@host.docker.internal:15432/appdb"
```
- token 放 **user** 字段，**不用密码**（隧道 trust 风格放行）
- 连上后实际会话是注册 URL 里的真实账号（如 `app`），库也是注册的库

### mysql（MySQL）
```sh
TOKEN=$(cat ~/.ai-tools/bridge-token)
mysql -h 127.0.0.1 -P 15441 -u "$TOKEN" -px --ssl-mode=DISABLED shop
```
- token 放 **username**，密码任意（`-px` 就是任意）
- `--ssl-mode=DISABLED`：客户端↔隧道是明文段；隧道↔真实库的 TLS 由注册 URL 决定（`?tls=true`）

### redis-cli / Redis Insight
```sh
TOKEN=$(cat ~/.ai-tools/bridge-token)
redis-cli -h 127.0.0.1 -p 15434 -a "$TOKEN" --no-auth-warning
```
- token 放 **AUTH 首命令**（URL 形式 `redis://:$TOKEN@127.0.0.1:15434/0` 也支持）

### GUI（DBeaver / pgAdmin4 / Redis Insight）
| 软件 | 填法 |
|---|---|
| DBeaver | JDBC URL：`jdbc:postgresql://127.0.0.1:15432/appdb?user=<TOKEN>` |
| pgAdmin4 | Host `127.0.0.1` / Port `15432` / DB `appdb` / Username `<TOKEN>` / 密码留空 |
| Redis Insight | 连接 URL：`redis://:<TOKEN>@127.0.0.1:15434/0` |

## 4 · 容器内 AI 视角（模拟）

```sh
# 容器里（agent 隔离域 / 任意容器）连 host 隧道：
TOKEN=...   # AI_TOOLS_BRIDGE_TOKEN，由 compose 注入
psql "postgresql://$TOKEN@host.docker.internal:15432/appdb"
redis-cli -h host.docker.internal -p 15434 -a "$TOKEN"
# 查隧道清单（不配 DB 密钥也能读，走掩码桥）
ai-tools remote dblist
ai-tools db list            # 无本地 store 时自动经桥读取
# 直接拿现成命令
ai-tools db connect pgdb --container
```

## 5 · 读写权限（代理不强制，靠账号）

```sh
# 只读账号注册的连接 → 只能 SELECT，写被拒（实测）
mysql -h 127.0.0.1 -P 15441 -u "$TOKEN" -px --ssl-mode=DISABLED \
  -e "CREATE TABLE shop.demo(...);"
#   ERROR 1142 ... CREATE command denied to user 'sha2user'  ✅ 被拒

# 想给 AI 写权限 → 注册一个有写权限的账号 URL
printf 'mysql://root:rootpass@127.0.0.1:59919/shop' | ai-tools db add mysql-admin --port 15444
```
原则：**注册的账号权限 = AI 在隧道里的权限上限**。安全姿势是注册专用只读账号（如 `app_ro`），名字本身不敏感。

## 6 · 安全验证（AI 能看到/拿不到什么）

```sh
TOKEN=$(cat ~/.ai-tools/bridge-token)
# 负向：错 token 一律拒绝
psql "postgresql://WRONG@127.0.0.1:15432/appdb" -c "SELECT 1;"
#   server closed the connection

# 能看到真实账号名（真实会话固有属性）
psql "postgresql://$TOKEN@127.0.0.1:15432/appdb" -c "SELECT current_user;"
#   app

# 拿不到密码：受限账号读哈希被拒
psql "postgresql://$TOKEN@127.0.0.1:15437/appdb" -c "SELECT rolpassword FROM pg_authid;"
#   ERROR: permission denied for table pg_authid

# 审计日志（成功/拒绝，无 SQL/凭据）
grep dbproxy /tmp/ai-tools-dbtest-serve.log
#   dbproxy: pgdb: 127.0.0.1:xxx: authenticated, tunnel open
#   dbproxy: pgdb: 127.0.0.1:xxx: invalid bridge token in user field
```

## 7 · 自动化脚本（把命令拿去用）

```sh
#!/usr/bin/env bash
set -euo pipefail
TOKEN=$(cat ~/.ai-tools/bridge-token)

# 用 db connect 生成命令，替换主机/端口后执行
read_orders() {
  docker run --rm mysql:8.4 mysql -h host.docker.internal \
    -P 15441 -u "$TOKEN" -px --ssl-mode=DISABLED --batch \
    -e "SELECT COUNT(*) FROM shop.orders WHERE qty >= 2;"
}
read_orders   # → 2（qty>=2 的订单数）

# Redis 写缓存
redis-cli -h 127.0.0.1 -p 15434 -a "$TOKEN" --no-auth-warning set last_sync "$(date -u +%FT%TZ)"
```

## 8 · 检查与排错（db test / db show）

```sh
# db test：验证注册的连接能否认证成功（AI 安全，不打印 URL/密码/地址）
ai-tools db test mydb
#   OK: mydb (postgres) user=app db=appdb
ai-tools db test bad-db
#   FAIL: bad-db: 连接失败：无法到达 PostgreSQL（请检查 URL 的 host/port）
#   修复：printf '<正确URL>' | ai-tools db add bad-db（端口保持不变）   ← 端口不变，DBeaver 不用改

# db show：自己核对注册的真实 URL（仅你的 TTY，AI/脚本一律拒绝）
ai-tools db show mydb
#   name: mydb
#   type: postgres
#   port: 15432
#   url:  postgres://app:pgpass@127.0.0.1:59918/appdb
```

管理操作：

```sh
ai-tools db rm mysql-billing --yes          # 删连接（同时隧道消失）
ai-tools db shell cache                     # host 上开交互客户端（TTY-only，凭据走 env）
ai-tools db list --json                     # JSON 输出（AI 友好）

# 排错
grep dbproxy /tmp/ai-tools-dbtest-serve.log # 看连接审计
lsof -nP -iTCP:15441 -sTCP:LISTEN            # 端口是否在听
```

---

*示例环境：`scripts/dbtest.sh`（Docker 起 PG/MySQL/Redis + 注册连接 + 起 serve，`--clean` 收尾）。*
