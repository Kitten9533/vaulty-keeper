# Database Tunnel Usage Examples

English | [中文](db-proxy-examples.zh-CN.md)

PG/MySQL/Redis examples for the current working tree. The complete synthetic recipe below was source-checked, **not executed** during the 2026-09-07 documentation correction. Expected results are not captured test output. See [architecture](db-proxy-architecture.md) and the canonical [security model](security-model.md); per-protocol operation guides cover URL options, client setup and troubleshooting ([PostgreSQL](tunnel/postgres-tunnel-guide.md) / [MySQL](tunnel/mysql-tunnel-guide.md) / [Redis](tunnel/redis-tunnel-guide.md)). MongoDB 8 has a separate [guide and historical validation matrix](tunnel/mongodb-tunnel-guide.md): one fixed endpoint, `vaulty` plus dedicated password token, no global fallback, persistent command/metadata checks, and no promise of complete GUI support or immediate session revocation.

**`scripts/dbtest.sh` is isolated and safe to run (C02 done):** it tracks its own serve PID and containers by label, uses unique per-run temp dirs and a fake HOME with synthetic keys, and `--clean` removes only its registered runs. The historical version broadly killed matching serve processes, removed fixed containers `aipg`/`aimysql8`/`aimariadb`/`airedis` and fixed `/tmp/vaulty-keeper-dbtest*` dirs/logs, and could overwrite the real HOME bridge-token — none of that applies to the current script. Its images are PostgreSQL `17.6-alpine`, MySQL `8.0` (historically 8.0.46, not 8.4/MariaDB) and Redis `7`.

## Synthetic Fixture

Run only in a disposable local development environment, from the repository root, with a current `bin/vaulty-keeper` already built. Prerequisites: Bash, Node, Docker with permission to pull/run images, and host `psql`, MySQL 8 `mysql`, and `redis-cli` on PATH. A database image or the repository agent image does not install these host clients. Building from source needs Go; `make test` also needs Node. The latest release **v0.8.0** includes the current DB tunnel and MongoDB behavior and bundles the `docs/` guides; its prebuilt archives are the matching reference for the recipes here. The older 0.6.0 archives lack linked `docs/` files and do not establish current working-tree feature availability.

All passwords/data here are public synthetic fixtures, never production input. The recipe creates disposable database containers without host bind mounts, a unique temporary HOME/store/log, explicit synthetic keys, and its own serve PID. It cleans only those resources, including the containers' anonymous volumes, on exit. It does not run `db init`, use the real vault or call Keychain. Do not replace its credentials/keys with real ones. Docker image pulls/cache remain; Docker itself and unrelated containers are not stopped. Image tags are not immutable digests; record resolved digests for byte-identical future reproductions.

| Registration | Backend loopback port | Database/account | Tunnel port | Data/grants |
|---|---|---|---|---|
| `pgdb` | 59918 | `appdb` / `app` | 15432 | `t`, two rows; SELECT and INSERT |
| `pg-readonly` | 59918 | `appdb` / `app_ro` | 15437 | Same `t`; SELECT only |
| `mysql-orders` | 59919 | `shop` / `sha2user` | 15441 | `orders`, three rows; SELECT only |
| `mysql-billing` | 59919 | `shop_billing` / `sha2user` | 15442 | `invoices`, one row; SELECT only |
| `mysql-reporting` | 59919 | `shop_reporting` / `sha2user` | 15443 | `totals`, one row; SELECT only |
| `mysql-native` | 59919 | `shop` / `nativeuser` | 15436 | Same `orders`; SELECT only |
| `cache` | 59920 | Redis database `0` | 15434 | `demo=ready`; fixture password account is writable |

HTTP bridge: `127.0.0.1:8972`. All ports must be unused. The preflight only checks current occupancy, not a reservation; a race or later bind failure must be investigated, never resolved by killing unrelated processes. This fixture intentionally uses plaintext loopback transport, so it is not a TLS demonstration. **MySQL `?tls=true` is fixed** (was C01: `CLIENT_SSL` capability + TLS-upgraded forwarding; a one-off native TLS query passed, but that evidence is not pinned by an integration test in this repo — re-verify before relying on it; add `tlsCAFile=<path>` for a private/self-signed CA). Do not disable required TLS on a real service.

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

The shared deterministic storage key is strictly a fixture shortcut, not the production three-key model. The script uses its disposable global token to demonstrate current PG/MySQL/Redis acceptance on **new** registrations. Normally obtain and distribute each connection's dedicated token. This recipe is not a tested replacement for C02, a complete security suite, a TLS test or a container-client test. A nonzero negative case alone does not prove permission enforcement unless successful queries on the same connection and the actual denial are also checked.

## Human Host Workflow

For an existing authorized database, first have the human operator initialize the host DB key with `vaulty-keeper db init` if genuinely missing. Nonempty `VAULTY_KEEPER_DB_KEY` takes precedence over the keyring (Base64, 32 decoded bytes); a bad override does not fall back. Do not regenerate keys as a first troubleshooting step, export real keys into an agent environment, or use fixture keys for real data.

In host terminal A:

```sh
vaulty-keeper db add pg-readonly --port 15437
vaulty-keeper db test pg-readonly
vaulty-keeper serve --addr 127.0.0.1:8970
```

After `db add`, enter the authorized URL at its stdin prompt and press Enter; the current prompt **echoes** input. Avoid recording that terminal. Stdin does not erase history from an upstream `echo`/`printf` command. Store the URL/token encrypted, but account for plaintext input files, exports and host process memory separately. `db test` checks backend connectivity/authentication, not tunnel listening or read-only grants; PG/MySQL success output can include the real username/database.

`serve` remains running. In host terminal B using the same DB store/key context:

```sh
vaulty-keeper db list --json
vaulty-keeper db connect pg-readonly
vaulty-keeper db connect pg-readonly --cmd
```

Inspect the generated command before executing it; it contains a usable proxy token that can enter argv/history/logs. PG/MySQL use the token as username with placeholder password `x`; Redis uses it in the first AUTH/password field. GUI fields use the proxy host/port and these virtual credentials, not backend credentials. PostgreSQL JDBC uses `jdbc:postgresql://127.0.0.1:15437/appdb?user=<TOKEN>&password=x`; Redis Insight uses `redis://x:<TOKEN>@127.0.0.1:15434/0`. These are templates, not literal executable URLs; fill the generated connection's actual token/database/port. Install GUI drivers separately; DBeaver Redis support depends on edition/plugin availability.

## Host And Container Separation

For container access, the human host operator must select a reachable bind interface and firewall policy for **both** bridge and DB ports. Loopback-only serve is not ordinarily reachable from the Docker VM. `0.0.0.0` exposes all interfaces; token authentication is not transport encryption or egress isolation. Register before starting serve; if it started without a DB store or available key, restart it after setup so the watcher starts.

Generate container commands **on the host**:

```sh
vaulty-keeper db connect pg-readonly --container
```

Deliver only the authorized proxy command/token to the container, then run its native client there. `--container` only changes the printed hostname to `host.docker.internal`. It does not change listener bindings. `db connect` always needs local DBKey/Resolve and is not a keyless remote command. Do not mount host keys to make it work. Linux Docker needs the `host-gateway` host mapping; clients must be installed inside that container.

With an intentionally supplied bridge address/token, a container can run `vaulty-keeper remote dblist` for names/types/ports/state only; `db list` attempts that fallback when local listing fails. This does not retrieve dedicated tokens. The global bridge token grants access to **all reachable PG/MySQL/Redis registrations, new and old**, including allowed writes; it is not a masks-only capability. Compose does not restrict all egress to the bridge; the entrypoint prints only a `<set>`/`<unset>` marker for the token, never the token itself, but the token still travels to the container via the environment. Project mounts, persistent history and logs can expose credentials. See the [security model](security-model.md) before granting container access.

## Lifecycle And Troubleshooting

```sh
vaulty-keeper db regen pg-readonly
vaulty-keeper db off pg-readonly
vaulty-keeper db on pg-readonly
```

These are separate controls, not a sequence that revokes all sessions. Rotation changes dedicated-token authentication for subsequent connections; already accepted handshakes/sessions may retain old state. The global token still works for PG/MySQL/Redis. Off/rm closes listeners on the watcher's approximately two-second sync, without actively ending established sessions. Redistribute new tokens after regen or same-name add. Same-name add retains the port if omitted, **creates a new token and resets enabled to true**, even if previously off. If changing the port, stop the listener before updating and re-enable it afterward; an active listener is not automatically rebound to the new stored port.

| Symptom | Safe next check |
|---|---|
| No listener despite enabled state | Check owned serve startup, DB store/key availability at startup, matching `VAULTY_KEEPER_DB_DIR`, bind interface, firewall and OS port occupancy; `serve --dir` selects snapshots, not DB storage |
| Port conflict | Allocation checks registered ports only. Choose an unused explicit port, update clients, and restart the affected listener; do not kill unrelated processes |
| `Broken` metadata | Means stored URL decryption failed, not a generic bad token. Human checks key source; token decryption failures are separately reported by Resolve |
| Wrong token / stale command | Obtain the current dedicated token on the host; check correct connection/port and rotation/re-registration history, without reading real URLs or keys |
| Read succeeds, write/catalog read denied | Check backend least-privilege grants; denial is expected for the fixture read-only accounts. Do not grant admin privileges merely to make an example pass |
| MySQL TLS failure | Verify CA trust/`tlsCAFile`, server `require_secure_transport`, hostname/`ServerName` and TLS version; `?tls=true` itself is fixed and unit-tested (native evidence not pinned). Never bypass required TLS |
| Need deeper diagnostics | Human privately inspects relevant logs, sharing only redacted summaries. Old protocol handler errors can include backend addresses/messages; logs are not universally sanitized |

`db show` prints the decrypted real URL; `db shell` launches a direct backend client, not the token tunnel. Their stdin-TTY checks do not establish human identity or prevent stdout capture. These are human host operations, not agent troubleshooting steps; do not fabricate a TTY. Use `db test`, metadata and authorized bounded queries without seeking secrets. The proxy does not redact arbitrary query results.

Source checks: [CLI](../internal/cli/db.go), [startup](../internal/cli/remote.go), [store](../internal/dbproxy/store.go), [listeners](../internal/dbproxy/tunnel.go), [MySQL TLS](../internal/dbproxy/mysql.go), [old script](../scripts/dbtest.sh), [Dockerfile](../Dockerfile). No runtime tests, builds, release operations or real-data operations were run for this correction.
