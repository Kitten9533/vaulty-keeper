# 数据库隧道使用示例

[English](db-proxy-examples.md) | 中文

适用于当前工作区的 PG/MySQL/Redis。下方完整合成步骤已按源码核对，2026-09-07 文档更正期间**没有执行**；预期结果不是测试输出。配套见[架构](db-proxy-architecture.zh-CN.md)及统一[安全模型](security-model.zh-CN.md)；URL 选项、客户端设置与排错见逐协议操作指南（[PostgreSQL](tunnel/postgres-tunnel-guide.zh-CN.md) / [MySQL](tunnel/mysql-tunnel-guide.zh-CN.md) / [Redis](tunnel/redis-tunnel-guide.zh-CN.md)）。MongoDB 8 有独立[指南和历史验证矩阵](tunnel/mongodb-tunnel-guide.zh-CN.md)：固定单端点、`vaulty` 加专属密码 token、无全局兜底、持续命令/元数据检查，不承诺完整 GUI 支持或即时撤销会话。

**`scripts/dbtest.sh` 已隔离重构，可以安全运行（C02 完成）：** 它按 PID 与容器标签跟踪自己启动的 serve 和容器，使用每次运行独立的临时目录和假 HOME、合成密钥，`--clean` 只清理登记的运行。历史版本会宽泛终止匹配的 serve 进程、删除固定容器 `aipg`/`aimysql8`/`aimariadb`/`airedis` 及固定 `/tmp/vaulty-keeper-dbtest*` 目录/日志，并可能覆盖真实 HOME 的 bridge-token——当前脚本均不再适用。其镜像是 PostgreSQL `17.6-alpine`、MySQL `8.0`（历史为 8.0.46，不是 8.4/MariaDB）和 Redis `7`。

## 合成夹具

只在可丢弃的本地开发环境、仓库根目录执行，预先备好当前构建的 `bin/vaulty-keeper`。前提：Bash、Node、有权拉取/运行镜像的 Docker，以及 PATH 中的宿主 `psql`、MySQL 8 `mysql`、`redis-cli`。数据库镜像和仓库 agent 镜像不会安装这些宿主客户端。源码构建需要 Go，`make test` 还需要 Node。最新发布 **v0.8.0** 已包含当前 DB 隧道与 MongoDB 行为并打包 `docs/` 指南，其预编译归档是这里配方对应的参考版本；更早的 0.6.0 归档缺少链接的 `docs/`，也不代表当前工作区功能。

下方密码/数据全是公开合成夹具，不是生产输入。步骤创建不绑定宿主目录的临时数据库容器、唯一临时 HOME/存储/日志、显式合成密钥及自己管理的 serve PID；退出只清理这些资源，包括容器匿名卷。不运行 `db init`，不使用真实 vault，不调用 Keychain。不要用真实凭据/密钥替换。Docker 镜像拉取/缓存会保留，不停止 Docker 或无关容器。镜像 tag 不是不可变 digest，需逐字节复现时应记录解析后的 digest。

| 注册名 | 后端 loopback 端口 | 数据库/账号 | 隧道端口 | 数据/权限 |
|---|---|---|---|---|
| `pgdb` | 59918 | `appdb` / `app` | 15432 | `t` 两行；SELECT、INSERT |
| `pg-readonly` | 59918 | `appdb` / `app_ro` | 15437 | 同一 `t`；仅 SELECT |
| `mysql-orders` | 59919 | `shop` / `sha2user` | 15441 | `orders` 三行；仅 SELECT |
| `mysql-billing` | 59919 | `shop_billing` / `sha2user` | 15442 | `invoices` 一行；仅 SELECT |
| `mysql-reporting` | 59919 | `shop_reporting` / `sha2user` | 15443 | `totals` 一行；仅 SELECT |
| `mysql-native` | 59919 | `shop` / `nativeuser` | 15436 | 同一 `orders`；仅 SELECT |
| `cache` | 59920 | Redis 库 `0` | 15434 | `demo=ready`；夹具密码账号可写 |

HTTP 桥为 `127.0.0.1:8972`，所有端口必须空闲。预检查只检查当时占用，不预留端口；发生竞争或后续绑定失败时应排查，不得终止无关进程。本夹具有意使用明文 loopback，因此不是 TLS 演示。**MySQL `?tls=true` 已修复**（原 C01：`CLIENT_SSL` 能力位 + TLS 升级转发；一次性原生 TLS 查询通过，但该证据未被本仓库集成测试固化——依赖前请复测；私有/自签 CA 加 `tlsCAFile=<路径>`）。不得关闭真实服务必需的 TLS。

```sh
bash <<'BASH'
set +x
set -euo pipefail
umask 077
for tool in docker node psql mysql redis-cli; do command -v "$tool" >/dev/null; done
BIN="$PWD/bin/vaulty-keeper"
test -x "$BIN"
node <<'JS'
const net = require('node:net');
const ports = [59918, 59919, 59920, 8972, 15432, 15437, 15441, 15442, 15443, 15436, 15434];
const servers = [];
Promise.all(ports.map(port => new Promise((resolve, reject) => {
  const server = net.createServer();
  servers.push(server);
  server.once('error', reject).listen(port, '127.0.0.1', resolve);
}))).then(() => servers.forEach(server => server.close())).catch(error => {
  console.error(error.message);
  process.exit(1);
});
JS
LAB=$(mktemp -d "${TMPDIR:-/tmp}/vaulty-docs.XXXXXX")
PG=''
MY=''
RD=''
SERVE_PID=''
cleanup() {
  if [ -n "$SERVE_PID" ]; then
    kill "$SERVE_PID" 2>/dev/null || true
    wait "$SERVE_PID" 2>/dev/null || true
  fi
  for cid in "$PG" "$MY" "$RD"; do
    if [ -n "$cid" ]; then docker rm -fv "$cid" >/dev/null 2>&1 || true; fi
  done
  rm -rf "$LAB"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p "$LAB/home" "$LAB/db" "$LAB/snapshots"
KEY=$(node -e 'process.stdout.write(Buffer.alloc(32, 68).toString("base64"))')
VK=(env -i "PATH=$PATH" "HOME=$LAB/home" "VAULTY_KEEPER_DB_DIR=$LAB/db"
  "VAULTY_KEEPER_APOLLO_DIR=$LAB/snapshots" "VAULTY_KEEPER_DB_KEY=$KEY"
  "VAULTY_KEEPER_APOLLO_KEY=$KEY" "VAULTY_KEEPER_SENSITIVE_KEY=$KEY"
  VAULTY_KEEPER_LANG=en "$BIN")
PG=$(docker create --label "vaulty.docs=$LAB" -p 127.0.0.1:59918:5432 \
  -e POSTGRES_PASSWORD=fixture-root -e POSTGRES_DB=appdb postgres:17.6-alpine)
docker start "$PG" >/dev/null
MY=$(docker create --label "vaulty.docs=$LAB" -p 127.0.0.1:59919:3306 \
  -e MYSQL_ROOT_PASSWORD=fixture-root mysql:8.0.46)
docker start "$MY" >/dev/null
RD=$(docker create --label "vaulty.docs=$LAB" -p 127.0.0.1:59920:6379 \
  redis:7 redis-server --requirepass redispass)
docker start "$RD" >/dev/null
ready() {
  for attempt in {1..90}; do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  return 1
}
ready docker exec -e PGPASSWORD=fixture-root "$PG" psql -X -h127.0.0.1 -U postgres -d appdb -c 'SELECT 1'
ready docker exec "$MY" mysql --no-defaults -h127.0.0.1 -uroot -pfixture-root -e 'SELECT 1'
ready docker exec "$RD" redis-cli -a redispass PING
docker exec -i "$PG" psql -X -v ON_ERROR_STOP=1 -U postgres -d appdb <<'SQL'
CREATE TABLE public.t (id integer PRIMARY KEY, name text NOT NULL);
INSERT INTO public.t VALUES (1, 'alice'), (2, 'bob');
CREATE ROLE app LOGIN PASSWORD 'pgpass';
CREATE ROLE app_ro LOGIN PASSWORD 'ropass';
GRANT CONNECT ON DATABASE appdb TO app, app_ro;
GRANT USAGE ON SCHEMA public TO app, app_ro;
GRANT SELECT, INSERT ON public.t TO app;
GRANT SELECT ON public.t TO app_ro;
SQL
docker exec -i "$MY" mysql --no-defaults -uroot -pfixture-root <<'SQL'
CREATE DATABASE shop;
CREATE TABLE shop.orders (id INT PRIMARY KEY, qty INT NOT NULL);
INSERT INTO shop.orders VALUES (1, 1), (2, 2), (3, 5);
CREATE DATABASE shop_billing;
CREATE TABLE shop_billing.invoices (id INT PRIMARY KEY, amount INT NOT NULL);
INSERT INTO shop_billing.invoices VALUES (1, 700);
CREATE DATABASE shop_reporting;
CREATE TABLE shop_reporting.totals (id INT PRIMARY KEY, qty INT NOT NULL);
INSERT INTO shop_reporting.totals VALUES (1, 8);
CREATE USER 'sha2user'@'%' IDENTIFIED WITH caching_sha2_password BY 'sha2pass';
CREATE USER 'nativeuser'@'%' IDENTIFIED WITH mysql_native_password BY 'nativepass';
GRANT SELECT ON shop.* TO 'sha2user'@'%', 'nativeuser'@'%';
GRANT SELECT ON shop_billing.* TO 'sha2user'@'%';
GRANT SELECT ON shop_reporting.* TO 'sha2user'@'%';
SQL
docker exec "$RD" redis-cli -a redispass SET demo ready
printf '%s' 'postgres://app:pgpass@127.0.0.1:59918/appdb' | "${VK[@]}" db add pgdb --port 15432
printf '%s' 'postgres://app_ro:ropass@127.0.0.1:59918/appdb' | "${VK[@]}" db add pg-readonly --port 15437
printf '%s' 'mysql://sha2user:sha2pass@127.0.0.1:59919/shop' | "${VK[@]}" db add mysql-orders --port 15441
printf '%s' 'mysql://sha2user:sha2pass@127.0.0.1:59919/shop_billing' | "${VK[@]}" db add mysql-billing --port 15442
printf '%s' 'mysql://sha2user:sha2pass@127.0.0.1:59919/shop_reporting' | "${VK[@]}" db add mysql-reporting --port 15443
printf '%s' 'mysql://nativeuser:nativepass@127.0.0.1:59919/shop' | "${VK[@]}" db add mysql-native --port 15436
printf '%s' 'redis://:redispass@127.0.0.1:59920/0' | "${VK[@]}" db add cache --port 15434
"${VK[@]}" db list
"${VK[@]}" serve --addr 127.0.0.1:8972 --dir "$LAB/snapshots" >"$LAB/serve.log" 2>&1 &
SERVE_PID=$!
ready test -s "$LAB/home/.vaulty/bridge-token"
kill -0 "$SERVE_PID"
# Only this disposable serve's global token, not the real HOME token.
TOKEN=$(node -e 'process.stdout.write(require("node:fs").readFileSync(process.argv[1], "utf8").trim())' "$LAB/home/.vaulty/bridge-token")
PGURI="postgresql://$TOKEN:x@127.0.0.1:15432/appdb?sslmode=disable&connect_timeout=5"
ROURI="postgresql://$TOKEN:x@127.0.0.1:15437/appdb?sslmode=disable&connect_timeout=5"
ready psql -X "$PGURI" -c 'SELECT 1'
ready psql -X "$ROURI" -c 'SELECT 1'
for port in 15441 15442 15443 15436; do
  ready mysql --no-defaults -h127.0.0.1 -P "$port" -u "$TOKEN" -px --ssl-mode=DISABLED --connect-timeout=5 -e 'SELECT 1'
done
ready redis-cli -h 127.0.0.1 -p 15434 -a "$TOKEN" PING
test "$(redis-cli -h 127.0.0.1 -p 15434 -a "$TOKEN" --no-auth-warning PING)" = PONG
psql -X "$PGURI" -c 'SELECT id, name FROM public.t ORDER BY id LIMIT 10'
psql -X "$ROURI" -c 'SELECT current_user, count(*) FROM public.t'
# Expected: app_ro and 2. Each denial below must exit nonzero.
if psql -X "$ROURI" -v ON_ERROR_STOP=1 -c 'SELECT rolpassword FROM pg_authid'; then exit 1; fi
if psql -X "$ROURI" -v ON_ERROR_STOP=1 -c "INSERT INTO public.t VALUES (3, 'denied')"; then exit 1; fi
if psql -X 'postgresql://WRONG:x@127.0.0.1:15432/appdb?sslmode=disable&connect_timeout=5' -c 'SELECT 1'; then exit 1; fi
mysql --no-defaults -h127.0.0.1 -P15441 -u "$TOKEN" -px --ssl-mode=DISABLED \
  -e 'SELECT COUNT(*) FROM shop.orders WHERE qty >= 2;'
# Expected: 2. Native authentication uses the same prepared shop data.
mysql --no-defaults -h127.0.0.1 -P15436 -u "$TOKEN" -px --ssl-mode=DISABLED \
  -e 'SELECT id, qty FROM shop.orders ORDER BY id LIMIT 10;'
mysql --no-defaults -h127.0.0.1 -P15442 -u "$TOKEN" -px --ssl-mode=DISABLED \
  -e 'SELECT id, amount FROM shop_billing.invoices ORDER BY id LIMIT 10;'
mysql --no-defaults -h127.0.0.1 -P15443 -u "$TOKEN" -px --ssl-mode=DISABLED \
  -e 'SELECT id, qty FROM shop_reporting.totals ORDER BY id LIMIT 10;'
if mysql --no-defaults -h127.0.0.1 -P15441 -u "$TOKEN" -px --ssl-mode=DISABLED \
  -e 'CREATE TABLE shop.demo (id INT PRIMARY KEY);'; then exit 1; fi
# Expected: CREATE denied by backend grants, not proxy SQL filtering.
redis-cli -h 127.0.0.1 -p 15434 -a "$TOKEN" --no-auth-warning GET demo
redis-cli -h 127.0.0.1 -p 15434 -a "$TOKEN" --no-auth-warning SET last_sync fixture-only
"${VK[@]}" db connect pg-readonly
# Prints the dedicated token for scoped distribution; no client command is auto-evaluated.
BASH
```

复用固定存储密钥仅是夹具简化，不是生产三密钥模型。脚本使用临时全局 token，展示 PG/MySQL/Redis 对**新**注册连接的实际接受行为。正常使用应获取并分发连接专属 token。步骤不是经过测试的 C02 替代脚本、完整安全套件、TLS 测试或容器客户端测试。负向用例非零退出本身不能证明权限限制成立，还需核对同连接成功查询和具体拒绝原因。

## 人工宿主流程

已有授权数据库由人工先检查宿主 DB 密钥，确实缺失时才运行 `vaulty-keeper db init`。非空 `VAULTY_KEEPER_DB_KEY` 优先于密钥库（Base64 解码后 32 字节），错误覆盖值不回退。不要把重新生成密钥作为排错第一步，不要把真实密钥导入 agent 环境，也不要用夹具密钥存真实数据。

宿主终端 A：

```sh
vaulty-keeper db add pg-readonly --port 15437
vaulty-keeper db test pg-readonly
vaulty-keeper serve --addr 127.0.0.1:8970
```

`db add` 后在 stdin 提示中输入授权 URL 并回车，当前输入**会回显**，应避免录制该终端。stdin 不会清除上游 `echo`/`printf` 命令历史。URL/token 加密存储，但明文输入文件、导出和宿主进程内存要单独考虑。`db test` 检查后端连接/认证，不检查隧道监听或只读权限；PG/MySQL 成功输出可能包含真实用户名/数据库。

`serve` 持续运行。在相同 DB 存储/密钥上下文的宿主终端 B：

```sh
vaulty-keeper db list --json
vaulty-keeper db connect pg-readonly
vaulty-keeper db connect pg-readonly --cmd
```

执行前检查生成的命令；其中含可用代理 token，可能进入 argv/历史/日志。PG/MySQL 使用 token 用户名及占位密码 `x`；Redis 在首条 AUTH/密码字段使用 token。GUI 填代理 host/端口及虚拟凭据，不填后端凭据。PostgreSQL JDBC 为 `jdbc:postgresql://127.0.0.1:15437/appdb?user=<TOKEN>&password=x`；Redis Insight 为 `redis://x:<TOKEN>@127.0.0.1:15434/0`。这是模板，不是可原样执行的 URL，需使用生成连接的真实 token/数据库/端口。GUI 驱动另行安装；DBeaver 的 Redis 支持取决于版本/插件。

## 宿主与容器分工

容器访问时，人工宿主操作者必须为桥和 DB **两类端口**选择可达接口及防火墙规则。Docker VM 通常不能访问仅 loopback 监听的 serve。`0.0.0.0` 暴露所有接口；token 认证不是传输加密或出口隔离。先注册再启动 serve；若启动时没有 DB 存储或密钥不可用，准备完成后需重启以启动 watcher。

在**宿主**生成容器命令：

```sh
vaulty-keeper db connect pg-readonly --container
```

仅将授权代理命令/token 交付容器，再在容器执行原生客户端。`--container` 只把打印地址改为 `host.docker.internal`，不改监听。`db connect` 总需要本地 DBKey/Resolve，不是无密钥远程命令；不要挂载宿主密钥打通它。Linux Docker 需要 `host-gateway` 映射；客户端需在容器安装。

有意交付 bridge 地址/token 后，容器可用 `vaulty-keeper remote dblist` 获取名称/类型/端口/状态；`db list` 在本地列表失败时会尝试该回退。这不返回专属 token。全局 bridge token 授予**所有可达 PG/MySQL/Redis 新旧注册连接**的访问权，含账号允许的写入，不是仅掩码能力。Compose 未限制所有出口必须经桥；entrypoint 对 token 只打印 `<set>`/`<unset>` 占位标记，从不输出 token 本身，但 token 仍经环境变量进入容器。项目挂载、持久化历史及日志可能暴露凭据。授权容器前阅读[安全模型](security-model.zh-CN.md)。

## 生命周期与排错

```sh
vaulty-keeper db regen pg-readonly
vaulty-keeper db off pg-readonly
vaulty-keeper db on pg-readonly
```

这是三个独立控制，不是撤销全部会话的操作序列。轮换改变后续连接的专属 token 认证；已接受的握手/会话可能保留旧状态。全局 token 对 PG/MySQL/Redis 仍有效。off/rm 在 watcher 约两秒同步时关闭监听，不主动结束已建立会话。regen 或同名 add 后需重新分发 token。同名 add 未指定端口时保留原端口，但**生成新 token 并重置为开启**，即使原先已关闭。修改端口前需停止监听，更新后再开启；活跃监听不会自动重绑到新存储端口。

| 症状 | 安全检查 |
|---|---|
| enabled 但无监听 | 检查自己管理的 serve 启动、启动时 DB 存储/密钥、匹配的 `VAULTY_KEEPER_DB_DIR`、接口、防火墙及 OS 端口占用；`serve --dir` 指快照而非 DB 存储 |
| 端口冲突 | 分配只检查注册端口；选择空闲显式端口、更新客户端并重启对应监听，不终止无关进程 |
| `Broken` 元数据 | 指存储 URL 解密失败，不是泛指 token 错误。人工检查密钥来源；token 解密失败由 Resolve 单独报告 |
| token 错误/旧命令 | 在宿主获取当前专属 token，核对连接/端口及轮换/重注册历史，不读取真实 URL 或密钥 |
| 读成功，写/目录查询被拒 | 核对后端最小权限；夹具只读账号应被拒。不要为让示例通过而授予管理权限 |
| MySQL TLS 失败 | 核对 CA 信任/`tlsCAFile`、服务端 `require_secure_transport`、主机名/`ServerName` 与 TLS 版本；`?tls=true` 本身已修复且有单测（原生证据未固化）。不得绕过必需 TLS |
| 需要深入诊断 | 人工私下查看相关日志，仅分享脱敏摘要。旧协议 handler 错误可能含后端地址/消息，日志并非统一脱敏 |

`db show` 打印解密的真实 URL；`db shell` 启动后端直连客户端，不走 token 隧道。它们的 stdin-TTY 检查不识别真人，也不阻止 stdout 捕获。这是人工宿主操作，不是 agent 排错步骤，不得伪造 TTY。使用 `db test`、元数据和授权的限量查询，不搜寻秘密；代理不脱敏任意查询结果。

源码核对：[CLI](../internal/cli/db.go)、[启动](../internal/cli/remote.go)、[存储](../internal/dbproxy/store.go)、[监听](../internal/dbproxy/tunnel.go)、[MySQL TLS](../internal/dbproxy/mysql.go)、[旧脚本](../scripts/dbtest.sh)、[Dockerfile](../Dockerfile)。本次更正未运行运行时测试、构建、发布或真实数据操作。
