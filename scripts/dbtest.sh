#!/usr/bin/env bash
# vaulty-keeper DB 隧道端到端测试（隔离，只动自己创建的资源）
#
# 启动 postgres + MySQL(8.0，含模拟 shop 业务库) + redis 三个容器 + 注册连接 + 起
# serve，然后从容器内经 host.docker.internal 走隧道验证原生客户端查询（token 门控）。
#
# 隔离保证：
#   - 每次运行使用唯一临时目录（mktemp -d）、唯一容器名（vaulty-dbtest-<pid>-<kind>）
#     和唯一端口（可经环境变量覆盖）；不删除任何固定名称的容器/目录。
#   - 只停自己启动的 serve 进程（记录 PID，并按命令行与临时目录双重核对），从不 pkill。
#   - serve 运行在假的 HOME 下，bridge token 写入临时目录，绝不读写真实 ~/.vaulty。
#   - DB 密钥与所有账号密码均为合成值，经环境变量显式传入，不访问真实 keyring。
#   - 中途出错自动清理本运行创建的资源；--clean 清理所有本脚本登记过的运行。
#
# 用法（仓库根目录）:
#   scripts/dbtest.sh           # 启动并测试，测完保持运行（serve+容器仍在）
#   scripts/dbtest.sh --clean   # 清理本脚本登记并仍在运行的 serve/容器/临时目录
#
# 可覆盖（环境变量）: BIN, PGP/MYP/RDP（数据库宿主端口）, TUN_PG/TUN_MY/TUN_NATIVE/TUN_RD
#                     （隧道端口）, BRIDGE_PORT（serve 端口）, TMPDIR
# 环境依赖: docker、python3、bin/vaulty-keeper（先 make build）
set -euo pipefail
umask 077

BIN="${BIN:-$(pwd)/bin/vaulty-keeper}"
[ -x "$BIN" ] || { echo "未找到 ${BIN}，请先 make build"; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "需要 docker"; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "需要 python3"; exit 1; }
docker version >/dev/null 2>&1 || { echo "Docker daemon 不可用"; exit 1; }

TMPBASE="${TMPDIR:-/tmp}"
TMPBASE="${TMPBASE%/}"
TMPBASE="${TMPBASE:-/tmp}"
RUNSDIR="$TMPBASE/vaulty-dbtest-runs"

# ---- 每次运行唯一资源：临时目录 / 容器名前缀 / 运行标识 ----
run_id="$(python3 -c 'import secrets; print(secrets.token_hex(6))')"
prefix="vaulty-dbtest-$$"
tmp="$(mktemp -d "$TMPBASE/vaulty-dbtest.XXXXXXXX")"
ENTRY="$RUNSDIR/$run_id.env"

# 动态分配空闲宿主端口（Docker Desktop 删除容器后端口释放有时会卡死，避免硬编码）
free_port() {
  python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'
}
PGP="${PGP:-$(free_port)}"; MYP="${MYP:-$(free_port)}"; RDP="${RDP:-$(free_port)}"
TUN_PG="${TUN_PG:-$(free_port)}"; TUN_MY="${TUN_MY:-$(free_port)}"
TUN_NATIVE="${TUN_NATIVE:-$(free_port)}"; TUN_RD="${TUN_RD:-$(free_port)}"
BRIDGE_PORT="${BRIDGE_PORT:-$(free_port)}"
BRIDGE_ADDR="0.0.0.0:$BRIDGE_PORT"

# 合成密钥（随机 32 字节，仅用于本运行），不触碰真实 keyring
DBKEY="$(python3 -c 'import base64,secrets;print(base64.b64encode(secrets.token_bytes(32)).decode())')"

PG_IMG=postgres:17.6-alpine
RD_IMG=redis:7
MY_IMG=dockerproxy.net/library/mysql:8.0                  # 本地已有（8.0.46），无则默认源拉取

# 清理指定运行创建的资源：只停“命令行与临时目录匹配”的 serve PID，
# 只删带本运行归属标签的容器，只删本运行的临时目录。
cleanup_run() {
  local rid="$1" pfx="$2" tdir="$3" spid="$4" name owner cmd i
  if [ -n "$spid" ] && [ "$spid" -gt 0 ] 2>/dev/null; then
    cmd="$(ps -p "$spid" -o command= 2>/dev/null || true)"
    case "$cmd" in
      *"vaulty-keeper serve"*"$tdir"*)
        kill "$spid" 2>/dev/null || true
        for i in $(seq 1 50); do
          kill -0 "$spid" 2>/dev/null || break
          sleep 0.1
        done
        kill -9 "$spid" 2>/dev/null || true
        ;;
    esac
  fi
  for name in "$pfx-pg" "$pfx-mysql" "$pfx-redis"; do
    owner="$(docker inspect --format '{{ index .Config.Labels "vaulty.dbtest" }}' "$name" 2>/dev/null || true)"
    if [ "$owner" = "$rid" ]; then
      docker rm -f "$name" >/dev/null 2>&1 || true
    fi
  done
  if [ -n "$tdir" ] && [ -d "$tdir" ]; then
    rm -rf -- "$tdir"
  fi
}

# 中途退出（出错/中断）自动清理本运行资源；正常“保持运行”时保留。
SERVE_PID=""
KEEP_RUNNING=0
cleanup_on_exit() {
  local status=$?
  if [ "$status" -ne 0 ] || [ "$KEEP_RUNNING" != 1 ]; then
    cleanup_run "$run_id" "$prefix" "$tmp" "$SERVE_PID"
    rm -f "$ENTRY"
  fi
  exit "$status"
}
trap cleanup_on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# ---- 参数：仅支持 --clean，其余为用法错误 ----
if [ "${1:-}" = "--clean" ]; then
  if [ -d "$RUNSDIR" ]; then
    for f in "$RUNSDIR"/*.env; do
      [ -e "$f" ] || continue
      RUN_ID= PREFIX= TMP_RUN= SERVE_PID=
      # shellcheck disable=SC1090
      . "$f"
      cleanup_run "${RUN_ID:-}" "${PREFIX:-}" "${TMP_RUN:-}" "${SERVE_PID:-}"
      rm -f "$f"
    done
    rmdir "$RUNSDIR" 2>/dev/null || true
  fi
  echo "已清理：本脚本登记的 serve 已停、容器已删、临时目录已删"
  exit 0
fi
if [ $# -gt 0 ]; then
  echo "未知参数: $*  （仅支持 --clean）" >&2
  exit 2
fi

mkdir -p -m 700 "$RUNSDIR"
printf 'RUN_ID=%s\nPREFIX=%s\nTMP_RUN=%s\nSERVE_PID=\n' "$run_id" "$prefix" "$tmp" >"$ENTRY"

# ---- 镜像（本地已有则跳过，否则默认源拉取）----
for img in "$PG_IMG" "$MY_IMG" "$RD_IMG"; do
  docker image inspect "$img" >/dev/null 2>&1 || docker pull "$img" >/dev/null
done
echo "镜像: PG=$PG_IMG MySQL=$MY_IMG Redis=$RD_IMG"

# ---- 启动容器（唯一名称 + 归属标签）----
docker run -d --name "$prefix-pg" --label "vaulty.dbtest=$run_id" \
  -e POSTGRES_PASSWORD=pgpass -e POSTGRES_USER=app -e POSTGRES_DB=appdb \
  -p 127.0.0.1:$PGP:5432 "$PG_IMG" >/dev/null
docker run -d --name "$prefix-mysql" --label "vaulty.dbtest=$run_id" \
  -e MYSQL_ROOT_PASSWORD=rootpass -p 127.0.0.1:$MYP:3306 \
  "$MY_IMG" >/dev/null
docker run -d --name "$prefix-redis" --label "vaulty.dbtest=$run_id" \
  -p 127.0.0.1:$RDP:6379 "$RD_IMG" redis-server --requirepass redispass >/dev/null

echo "等待数据库就绪 ..."
pg_ok=no; my_ok=no; rd_ok=no
for i in $(seq 1 60); do
  docker exec "$prefix-pg" pg_isready -U app >/dev/null 2>&1 && pg_ok=yes
  docker exec "$prefix-mysql" mysqladmin -h127.0.0.1 -u root -prootpass ping >/dev/null 2>&1 && my_ok=yes
  docker exec "$prefix-redis" redis-cli -a redispass ping >/dev/null 2>&1 && rd_ok=yes
  { [ "$pg_ok" = yes ] && [ "$my_ok" = yes ] && [ "$rd_ok" = yes ]; } && break
  sleep 1
done
[ "$pg_ok" = yes ] || { echo "postgres 未就绪"; exit 1; }
[ "$my_ok" = yes ] || { echo "MySQL 未就绪"; exit 1; }
[ "$rd_ok" = yes ] || { echo "redis 未就绪"; exit 1; }
echo "三个数据库就绪"

# ---- 种子数据 ----
docker exec "$prefix-pg" psql -U app -d appdb -c "CREATE TABLE t(id int, name text); INSERT INTO t VALUES (1,'alice'),(2,'bob');" >/dev/null
# 模拟业务库 shop：customers/products/orders + 两种认证账号
docker exec "$prefix-mysql" mysql -u root -prootpass -e "
CREATE DATABASE IF NOT EXISTS shop;
USE shop;
CREATE TABLE IF NOT EXISTS customers (id INT PRIMARY KEY, name VARCHAR(50), city VARCHAR(50), vip TINYINT);
CREATE TABLE IF NOT EXISTS products  (id INT PRIMARY KEY, name VARCHAR(50), price DECIMAL(10,2), stock INT);
CREATE TABLE IF NOT EXISTS orders    (id INT PRIMARY KEY, customer_id INT, product_id INT, qty INT, created_at DATE);
INSERT INTO customers VALUES (1,'zhangsan','shanghai',1),(2,'lisi','beijing',0),(3,'wangwu','guangzhou',1),(4,'zhaoliu','shenzhen',0),(5,'sunqi','hangzhou',1);
INSERT INTO products VALUES (1,'keyboard',299.00,120),(2,'monitor',1299.00,45),(3,'mouse',99.00,300),(4,'headset',499.00,80);
INSERT INTO orders VALUES (1,1,2,1,'2026-08-01'),(2,3,1,2,'2026-08-03'),(3,2,4,1,'2026-08-05'),(4,5,3,5,'2026-08-10'),(5,1,1,1,'2026-08-15'),(6,4,2,1,'2026-08-20');
CREATE USER IF NOT EXISTS 'sha2user'@'%' IDENTIFIED BY 'sha2pass';
CREATE USER IF NOT EXISTS 'nativeuser'@'%' IDENTIFIED WITH mysql_native_password BY 'nativepass';
GRANT SELECT ON shop.* TO 'sha2user'@'%';
GRANT SELECT ON shop.* TO 'nativeuser'@'%';
FLUSH PRIVILEGES;" >/dev/null 2>&1 || true
echo "种子数据就绪（pg: t 表；MySQL: shop 库 customers/products/orders）"

# ---- 注册连接（显式 --dir + 合成密钥环境变量）----
export VAULTY_KEEPER_DB_DIR="$tmp/db" VAULTY_KEEPER_DB_KEY="$DBKEY"
mkdir -p "$tmp/db" "$tmp/snap" "$tmp/home"
printf 'postgres://app:pgpass@127.0.0.1:%s/appdb' "$PGP" | "$BIN" db add pgdb --dir "$tmp/db" --port "$TUN_PG"
printf 'redis://:redispass@127.0.0.1:%s/0' "$RDP"    | "$BIN" db add cache --dir "$tmp/db" --port "$TUN_RD"
printf 'mysql://sha2user:sha2pass@127.0.0.1:%s/shop' "$MYP"   | "$BIN" db add mysqltest   --dir "$tmp/db" --port "$TUN_MY"
printf 'mysql://nativeuser:nativepass@127.0.0.1:%s/shop' "$MYP" | "$BIN" db add mysqlnative --dir "$tmp/db" --port "$TUN_NATIVE"
"$BIN" db on --all --dir "$tmp/db"
echo "连接已注册："; "$BIN" db list --dir "$tmp/db"

# ---- 起 serve（假 HOME：token 写临时目录，不碰真实 ~/.vaulty）----
SERVE_LOG="$tmp/serve.log"
TOKEN_FILE="$tmp/home/.vaulty/bridge-token"
HOME="$tmp/home" VAULTY_KEEPER_DB_DIR="$tmp/db" VAULTY_KEEPER_DB_KEY="$DBKEY" \
  nohup "$BIN" serve --addr "$BRIDGE_ADDR" --dir "$tmp/snap" >"$SERVE_LOG" 2>&1 &
SERVE_PID=$!
printf 'RUN_ID=%s\nPREFIX=%s\nTMP_RUN=%s\nSERVE_PID=%s\n' "$run_id" "$prefix" "$tmp" "$SERVE_PID" >"$ENTRY"

echo "等待 serve 就绪 ..."
ready=no
for i in $(seq 1 150); do
  if [ -s "$TOKEN_FILE" ] && grep -q "listening" "$SERVE_LOG" 2>/dev/null; then
    ready=yes; break
  fi
  if ! kill -0 "$SERVE_PID" 2>/dev/null; then
    echo "serve 提前退出:"; cat "$SERVE_LOG" 2>/dev/null || true
    exit 1
  fi
  sleep 0.2
done
[ "$ready" = yes ] || { echo "serve 就绪超时（30s）:"; cat "$SERVE_LOG" 2>/dev/null || true; exit 1; }
TOKEN="$(cat "$TOKEN_FILE")"
echo "serve 已启动（日志 ${SERVE_LOG}，token 文件 ${TOKEN_FILE}）"
export VAULTY_KEEPER_BRIDGE_ADDR="http://127.0.0.1:$BRIDGE_PORT" VAULTY_KEEPER_BRIDGE_TOKEN="$TOKEN"

# ---- 容器内经 host.docker.internal 走隧道验证 ----
echo
echo "================ 正向测试（客户端只带 token，不知道真实凭据）================"
echo "-- PostgreSQL（token 放 user 字段，真实侧 SCRAM）"
docker run --rm "$PG_IMG" psql "postgresql://${TOKEN}@host.docker.internal:${TUN_PG}/appdb" -c "SELECT id,name FROM t ORDER BY id;"
echo "-- MySQL caching_sha2_password（MySQL8 默认认证，RSA 全认证）"
docker run --rm "$MY_IMG" mysql -h host.docker.internal -P $TUN_MY -u "$TOKEN" -pxxx --ssl-mode=DISABLED --batch \
  -e "SELECT COUNT(*) AS orders, SUM(qty) AS total_qty FROM shop.orders;"
echo "-- MySQL mysql_native_password（AuthSwitch 切插件）"
docker run --rm "$MY_IMG" mysql -h host.docker.internal -P $TUN_NATIVE -u "$TOKEN" -pxxx --ssl-mode=DISABLED --batch \
  -e "SELECT city, COUNT(*) AS cnt FROM shop.customers GROUP BY city;"
echo "-- Redis（token 放 AUTH）"
docker run --rm "$RD_IMG" sh -c 'redis-cli -h host.docker.internal -p '"$TUN_RD"' -a "'"$TOKEN"'" --no-auth-warning set hello world >/dev/null; \
  redis-cli -h host.docker.internal -p '"$TUN_RD"' -a "'"$TOKEN"'" --no-auth-warning get hello'

echo
echo "================ 负向测试（错 token 一律拒绝）================"
docker run --rm "$PG_IMG" psql "postgresql://WRONG@host.docker.internal:${TUN_PG}/appdb" -tc "SELECT 1;" >/dev/null 2>&1 \
  && echo "  PG: 错误（应拒绝）" || echo "  PG: 正确拒绝 ✓"
# redis-cli 对 ERR 回复也可能返回非 0，只看输出内容判断（避免 pipefail 干扰）
RD_OUT="$(docker run --rm "$RD_IMG" redis-cli -h host.docker.internal -p $TUN_RD -a WRONG --no-auth-warning ping 2>&1 || true)"
if echo "$RD_OUT" | grep -qi "ERR\|closed\|refused"; then
  echo "  Redis: 正确拒绝 ✓"
else
  echo "  Redis: 错误（应拒绝）: $RD_OUT"
fi
docker run --rm "$MY_IMG" mysql -h host.docker.internal -P $TUN_MY -u WRONG -pxxx --ssl-mode=DISABLED -e "SELECT 1;" >/dev/null 2>&1 \
  && echo "  MySQL: 错误（应拒绝）" || echo "  MySQL: 正确拒绝 ✓"

# ---- 掩码桥：容器内 AI 视角（不配 DB 密钥）----
echo
echo "================ 掩码桥（AI 视角，无 DB 密钥）================"
echo "-- host 侧模拟：remote dblist / db list 走桥"
env -u VAULTY_KEEPER_DB_KEY -u VAULTY_KEEPER_DB_DIR HOME="$tmp/home" "$BIN" remote dblist
echo "-- 容器内视角（真实走 host.docker.internal）"
docker run --rm -e VAULTY_KEEPER_BRIDGE_ADDR="http://host.docker.internal:$BRIDGE_PORT" -e VAULTY_KEEPER_BRIDGE_TOKEN="$TOKEN" \
  -e HOME=/tmp -e VAULTY_KEEPER_DB_DIR=/tmp/none "$MY_IMG" sh -c \
  'echo "（容器内已就绪：连接名见 host 侧 remote dblist 输出）"'

KEEP_RUNNING=1
echo
echo "============================================================"
echo " 环境已就绪，保持运行。你可以这样测："
echo "  隧道端口: PG ${TUN_PG} / MySQL(sha2) ${TUN_MY} / MySQL(native) ${TUN_NATIVE} / Redis ${TUN_RD}"
echo "  TOKEN: $TOKEN"
echo "  serve: http://127.0.0.1:$BRIDGE_PORT  (掩码桥 remote list/dblist)"
echo "  在容器/隔离域里:"
echo "    psql     \"postgresql://\$TOKEN@host.docker.internal:${TUN_PG}/appdb\""
echo "    mysql    -h host.docker.internal -P $TUN_MY -u \$TOKEN -pxxx --ssl-mode=DISABLED shop"
echo "    redis-cli -h host.docker.internal -p $TUN_RD -a \$TOKEN"
echo "  本地 db list:  VAULTY_KEEPER_DB_DIR=$tmp/db VAULTY_KEEPER_DB_KEY=... $BIN db list"
echo "  结束:          scripts/dbtest.sh --clean"
echo "============================================================"
