#!/usr/bin/env bash
# vaulty-keeper DB 隧道端到端测试（macOS + Docker Desktop）
#
# 启动 postgres + MySQL(8.4，含模拟 shop 业务库) + redis 三个容器 + 注册连接 + 起
# serve，然后从容器内经 host.docker.internal 走隧道验证原生客户端查询（token 门控）。
# MySQL 优先用真 mysql:8.4（国内源）；拉不到时回退 mariadb:11。
#
# 用法（仓库根目录）:
#   scripts/dbtest.sh          # 启动并测试，测完保持运行，打印连接方式
#   scripts/dbtest.sh --clean  # 停掉 serve 并删除容器/临时目录
#
# 环境依赖: docker、python3、bin/vaulty-keeper（先 make build）
set -euo pipefail

BIN="${BIN:-$(pwd)/bin/vaulty-keeper}"
[ -x "$BIN" ] || { echo "未找到 $BIN，请先 make build"; exit 1; }
command -v docker >/dev/null || { echo "需要 docker"; exit 1; }

DBDIR=/tmp/vaulty-keeper-dbtest
SNAPDIR=/tmp/vaulty-keeper-dbtest-snap
DBKEY="$(python3 -c 'import base64;print(base64.b64encode(b"D"*32).decode())')"
BRIDGE_ADDR=0.0.0.0:8972

PG_IMG=postgres:17.6-alpine
RD_IMG=redis:7
MY_IMG=mysql:8.4                                          # 官方源优先
MY_FALLBACK=docker.m.daocloud.io/library/mysql:8.4       # 国内源兜底
MY_FALLBACK2=docker.m.daocloud.io/library/mariadb:11

# 动态分配空闲宿主端口（Docker Desktop 删除容器后端口释放有时会卡死，避免硬编码）
free_port() {
  python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'
}
PGP="$(free_port)"; MYP="$(free_port)"; RDP="$(free_port)"
TUN_PG=15432; TUN_MY=15435; TUN_NATIVE=15436; TUN_RD=15434  # 隧道端口

cleanup() {
  pkill -f 'bin/vaulty-keeper serve' 2>/dev/null || true
  docker rm -f aipg aimysql8 aimariadb airedis 2>/dev/null || true
  rm -rf "$DBDIR" "$SNAPDIR" /tmp/vaulty-keeper-dbtest-serve.log
}

if [ "${1:-}" = "--clean" ]; then
  cleanup
  echo "已清理：serve 已停、容器已删"
  exit 0
fi

cleanup

# ---- MySQL 镜像：官方源优先，失败依次回退国内 mysql:8.4 / mariadb ----
echo "准备 MySQL 镜像（优先官方 mysql:8.4）..."
if ! docker image inspect "$MY_IMG" >/dev/null 2>&1; then
  docker pull "$MY_IMG" >/dev/null 2>&1 || {
    echo "  官方源失败，回退 $MY_FALLBACK"
    MY_IMG="$MY_FALLBACK"
    docker pull "$MY_IMG" >/dev/null 2>&1 || {
      echo "  再回退 $MY_FALLBACK2"
      MY_IMG="$MY_FALLBACK2"
      docker pull "$MY_IMG" >/dev/null
    }
  }
fi
echo "MySQL 镜像: $MY_IMG"
case "$MY_IMG" in
  *mariadb*) MY_FLAVOR=mariadb ;;
  *)         MY_FLAVOR=mysql ;;
esac

# ---- 镜像 ----
for img in "$PG_IMG" "$RD_IMG"; do
  docker image inspect "$img" >/dev/null 2>&1 || docker pull "$img" >/dev/null
done

# ---- 启动容器 ----
docker run -d --name aipg -e POSTGRES_PASSWORD=pgpass -e POSTGRES_USER=app -e POSTGRES_DB=appdb \
  -p 127.0.0.1:$PGP:5432 "$PG_IMG" >/dev/null
if [ "$MY_FLAVOR" = "mysql" ]; then
  docker run -d --name aimysql8 -e MYSQL_ROOT_PASSWORD=rootpass -p 127.0.0.1:$MYP:3306 \
    "$MY_IMG" --mysql-native-password=ON >/dev/null
else
  docker run -d --name aimysql8 -e MARIADB_ROOT_PASSWORD=rootpass -p 127.0.0.1:$MYP:3306 \
    "$MY_IMG" >/dev/null
fi
docker run -d --name airedis -p 127.0.0.1:$RDP:6379 "$RD_IMG" redis-server --requirepass redispass >/dev/null

echo "等待数据库就绪 ..."
for i in $(seq 1 60); do
  pg_ok=no; my_ok=no; rd_ok=no
  docker exec aipg pg_isready -U app >/dev/null 2>&1 && pg_ok=yes
  if [ "$MY_FLAVOR" = "mysql" ]; then
    docker exec aimysql8 mysqladmin -h127.0.0.1 -u root -prootpass ping >/dev/null 2>&1 && my_ok=yes
  else
    docker exec aimysql8 mariadb-admin -h127.0.0.1 -u root -prootpass ping >/dev/null 2>&1 && my_ok=yes
  fi
  docker exec airedis redis-cli -a redispass ping >/dev/null 2>&1 && rd_ok=yes
  [ "$pg_ok" = yes ] && [ "$my_ok" = yes ] && [ "$rd_ok" = yes ] && break
  sleep 1
done
[ "$pg_ok" = yes ] || { echo "postgres 未就绪"; exit 1; }
[ "$my_ok" = yes ] || { echo "MySQL 未就绪"; exit 1; }
[ "$rd_ok" = yes ] || { echo "redis 未就绪"; exit 1; }
echo "三个数据库就绪"

# ---- 种子数据 ----
docker exec aipg psql -U app -d appdb -c "CREATE TABLE t(id int, name text); INSERT INTO t VALUES (1,'alice'),(2,'bob');" >/dev/null
if [ "$MY_FLAVOR" = "mysql" ]; then
  # 模拟业务库 shop：customers/products/orders + 两种认证账号
  docker exec aimysql8 mysql -u root -prootpass -e "
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
else
  docker exec aimysql8 mariadb -h127.0.0.1 -u root -prootpass -e \
    "CREATE DATABASE IF NOT EXISTS appdb; CREATE TABLE appdb.t(id INT, name VARCHAR(50)); INSERT INTO appdb.t VALUES (1,'carol'),(2,'dave');" >/dev/null
fi
echo "种子数据就绪（pg: t 表；MySQL: shop 库 customers/products/orders）"

# ---- 注册连接 ----
export VAULTY_KEEPER_DB_DIR="$DBDIR" VAULTY_KEEPER_DB_KEY="$DBKEY"
mkdir -p "$DBDIR" "$SNAPDIR"
printf 'postgres://app:pgpass@127.0.0.1:%s/appdb' "$PGP" | "$BIN" db add pgdb --dir "$DBDIR" --port $TUN_PG
printf 'redis://:redispass@127.0.0.1:%s/0' "$RDP"    | "$BIN" db add cache --dir "$DBDIR" --port $TUN_RD
if [ "$MY_FLAVOR" = "mysql" ]; then
  printf 'mysql://sha2user:sha2pass@127.0.0.1:%s/shop' "$MYP"   | "$BIN" db add mysqltest   --dir "$DBDIR" --port $TUN_MY
  printf 'mysql://nativeuser:nativepass@127.0.0.1:%s/shop' "$MYP" | "$BIN" db add mysqlnative --dir "$DBDIR" --port $TUN_NATIVE
else
  printf 'mysql://app:nativepass@127.0.0.1:%s/appdb' "$MYP" | "$BIN" db add mysqldb --dir "$DBDIR" --port $TUN_MY
fi
echo "连接已注册："; "$BIN" db list --dir "$DBDIR"

# ---- 起 serve（掩码桥 + 隧道）----
nohup "$BIN" serve --addr "$BRIDGE_ADDR" --dir "$SNAPDIR" >/tmp/vaulty-keeper-dbtest-serve.log 2>&1 &
sleep 1
TOKEN="$(cat ~/.vaulty/bridge-token)"
grep -q "listening" /tmp/vaulty-keeper-dbtest-serve.log || { echo "serve 启动失败:"; cat /tmp/vaulty-keeper-dbtest-serve.log; exit 1; }
echo "serve 已启动（日志 /tmp/vaulty-keeper-dbtest-serve.log）"

# ---- 容器内经 host.docker.internal 走隧道验证 ----
echo
echo "================ 正向测试（客户端只带 token，不知道真实凭据）================"
echo "-- PostgreSQL（token 放 user 字段，真实侧 SCRAM）"
docker run --rm "$PG_IMG" psql "postgresql://${TOKEN}@host.docker.internal:${TUN_PG}/appdb" -c "SELECT id,name FROM t ORDER BY id;"
if [ "$MY_FLAVOR" = "mysql" ]; then
  echo "-- MySQL caching_sha2_password（MySQL8 默认认证，RSA 全认证）"
  docker run --rm "$MY_IMG" mysql -h host.docker.internal -P $TUN_MY -u "$TOKEN" -pxxx --ssl-mode=DISABLED --batch \
    -e "SELECT COUNT(*) AS orders, SUM(qty) AS total_qty FROM shop.orders;"
  echo "-- MySQL mysql_native_password（AuthSwitch 切插件）"
  docker run --rm "$MY_IMG" mysql -h host.docker.internal -P $TUN_NATIVE -u "$TOKEN" -pxxx --ssl-mode=DISABLED --batch \
    -e "SELECT city, COUNT(*) AS cnt FROM shop.customers GROUP BY city;"
else
  echo "-- MySQL（mariadb，mysql_native_password）"
  docker run --rm "$MY_IMG" mariadb -h host.docker.internal -P $TUN_MY -u "$TOKEN" -pxxx --skip-ssl --batch \
    -e "SELECT id,name FROM appdb.t ORDER BY id;"
fi
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
docker run --rm "$MY_IMG" ${MY_FLAVOR} -h host.docker.internal -P $TUN_MY -u WRONG -pxxx --ssl-mode=DISABLED -e "SELECT 1;" >/dev/null 2>&1 \
  && echo "  MySQL: 错误（应拒绝）" || echo "  MySQL: 正确拒绝 ✓"

# ---- 掩码桥：容器内 AI 视角（不配 DB 密钥）----
echo
echo "================ 掩码桥（AI 视角，无 DB 密钥）================"
export VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8972 VAULTY_KEEPER_BRIDGE_TOKEN="$TOKEN"
echo "-- host 侧模拟：remote dblist / db list 走桥"
env -u VAULTY_KEEPER_DB_KEY -u VAULTY_KEEPER_DB_DIR HOME=/tmp/none "$BIN" remote dblist
echo "-- 容器内视角（真实走 host.docker.internal）"
docker run --rm -e VAULTY_KEEPER_BRIDGE_ADDR=http://host.docker.internal:8972 -e VAULTY_KEEPER_BRIDGE_TOKEN="$TOKEN" \
  -e HOME=/tmp -e VAULTY_KEEPER_DB_DIR=/tmp/none "$MY_IMG" sh -c \
  'echo "（容器内已就绪：连接名见 host 侧 remote dblist 输出）"'

echo
echo "============================================================"
echo " 环境已就绪，保持运行。你可以这样测："
echo "  隧道端口: PG ${TUN_PG} / MySQL(sha2) ${TUN_MY} / MySQL(native) ${TUN_NATIVE} / Redis ${TUN_RD}"
echo "  TOKEN: $TOKEN"
echo "  在容器/隔离域里:"
echo "    psql     \"postgresql://\$TOKEN@host.docker.internal:${TUN_PG}/appdb\""
echo "    mysql    -h host.docker.internal -P $TUN_MY -u \$TOKEN -pxxx --ssl-mode=DISABLED shop"
echo "    redis-cli -h host.docker.internal -p $TUN_RD -a \$TOKEN"
echo "  本地 db list:  VAULTY_KEEPER_DB_DIR=$DBDIR VAULTY_KEEPER_DB_KEY=... $BIN db list"
echo "  掩码桥:        http://host.docker.internal:8972  (remote list/dblist)"
echo "  结束:          scripts/dbtest.sh --clean"
echo "============================================================"
