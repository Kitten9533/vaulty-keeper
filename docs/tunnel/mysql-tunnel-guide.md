# MySQL Tunnel Guide

> [中文](mysql-tunnel-guide.zh-CN.md) | English
>
> Current MySQL tunnel interface, source-checked on 2026-09-07 (working-tree behavior; not a new test run). The general security authority is the [security model](../security-model.md); the shared three-protocol mechanism lives in the [architecture guide](../db-proxy-architecture.md); prepared fixtures and lifecycle live in the [examples guide](../db-proxy-examples.md).

## Connection Model

Register one backend MySQL endpoint with a normal username/password. The host keeps the URL and a dedicated 128-bit token encrypted in the DB store; connection name/type/port/state metadata is not encrypted. The client connects to a proxy port with the **tunnel token as the username** and an arbitrary placeholder password; the proxy authenticates to the backend with the registered credentials using `mysql_native_password` or `caching_sha2_password` (including RSA full authentication).

After backend authentication the proxy forwards raw protocol bytes in both directions: no query allowlist, no result redaction. Backend roles therefore define what a token holder can actually do. The backend participates in its own authentication, so the real password crosses the host-to-backend leg as part of that exchange.

MySQL accepts either the dedicated token **or the current serve global bridge token** for both newly registered and old connections. `db regen` rotates only the dedicated token; the global fallback remains valid.

## Authentication And Data Flow

```text
Client                         serve                        Backend
  | virtual token (username)      |                             |
  +----------------------------->| validate token               |
  |                              | connect + backend auth       |
  |                              | (mysql_native_password /     |
  |                              |  caching_sha2_password)      |
  |                              +---------------------------->|
  |                              |<----------------------------+
  | query                         |                             |
  +----------------------------->+---------------------------->|
  |<-----------------------------+<----------------------------+
```

The client sends the tunnel token as the username with any placeholder password. serve validates it, connects to the backend with the registered credentials and authenticates using `mysql_native_password` or `caching_sha2_password` (including RSA full authentication). Afterwards both directions are raw protocol bytes; the proxy does not parse or filter queries.

## Registered URL

`mysql://` is the accepted scheme. The URL carries the backend username, password, host, port and database. Optional backend TLS is configured with `?tls=true` (and optionally `tlsCAFile=<path>`); see below.

The tunnel port is chosen at `db add` (`--port`) or auto-allocated starting at 15432; automatic allocation checks registered ports only, not OS occupancy. Same-name `db add` retains the stored port when `--port` is omitted but replaces the URL, creates a fresh token and resets the connection to enabled.

## Backend TLS

**`?tls=true` (was C01):** with this flag the proxy advertises the `CLIENT_SSL` capability bit, upgrades the host-to-backend connection with TLS and uses the upgraded connection for authentication and raw forwarding. Add `tlsCAFile=<path>` (a PEM CA file, regular file ≤1 MiB) to trust a private or self-signed CA; without it the backend certificate is verified against system roots. Verification failures terminate the connection; there is no insecure downgrade.

Evidence status: the fix is unit-tested, and a one-off native TLS query (MySQL 8, `require_secure_transport=ON`, self-signed CA) reported a non-empty `Ssl_cipher` (TLSv1.3) and passed business queries — but that evidence is **not pinned by an integration test in this repo**. Re-verify against a real TLS backend before relying on it. The frontend proxy leg is always plaintext regardless; use localhost or an isolated trusted network.

## Client Setup

Have the human operator initialize the host DB key with `vaulty-keeper db init` only if genuinely missing. A nonempty `VAULTY_KEEPER_DB_KEY` takes precedence over the keyring, must decode from Base64 to 32 bytes, and does not fall back on errors. Install the MySQL `mysql` client on the machine that runs it; neither vaulty-keeper nor the agent image supplies it by default.

The following human workflow reserves tunnel port `15441` and assumes a backend database `shop`. It is a usage example, not an executed fixture:

```sh
# Human host terminal: supply the backend URL through stdin, not argv.
vaulty-keeper db add mysql-orders --port 15441
vaulty-keeper db test mysql-orders
vaulty-keeper serve --addr 127.0.0.1:8970
```

After `db add`, paste the authorized backend URL (e.g. `mysql://sha2user:sha2pass@127.0.0.1:3306/shop`) at the stdin prompt and press Enter. The prompt currently **echoes** input; avoid a recorded terminal. Register before starting serve: the DB watcher is created only when the store exists and the DB key is available at startup.

`serve` stays running. In another **host** terminal with the same store/key context, obtain proxy-only connection details:

```sh
vaulty-keeper db list
vaulty-keeper db connect mysql-orders
vaulty-keeper db connect mysql-orders --cmd
```

Do not put real backend URLs/passwords into AI messages, shell history or command-line arguments. An AI should use the tunnel connection information, not `db show`, the encrypted store, host keys or a direct backend shell.

The client invocation shape is (token as user, placeholder password `x`):

```sh
mysql --no-defaults -h127.0.0.1 -P 15441 -u <TOKEN> -px --ssl-mode=DISABLED
```

`--ssl-mode=DISABLED` only applies to the plaintext frontend leg; it does not disable backend `?tls=true`. Bounded reads look like:

```sh
mysql --no-defaults -h127.0.0.1 -P 15441 -u <TOKEN> -px --ssl-mode=DISABLED \
  -e 'SELECT COUNT(*) FROM shop.orders WHERE qty >= 2;'
```

GUI fields (MySQL Workbench / DBeaver) use the proxy host/port and the virtual credentials: host `127.0.0.1`, port `15441`, user `<TOKEN>`, password `x`, database `shop`. Install GUI drivers separately.

Container links substitute `host.docker.internal`, but `db connect --container` changes only printed addresses. A keyless container can run `vaulty-keeper remote dblist` for metadata only; generate tunnel commands on the host and deliver only the authorized credentials.

## Supported Operations

Whatever the MySQL client/GUI can do against a backend account, the tunnel forwards. There is no per-command policy:

- DDL, DML, SELECT and administrative commands are all raw-forwarded; the proxy is not a SQL firewall.
- Writes and destructive operations are governed solely by backend grants and the token holder's authorization. A `CREATE` denied by backend grants fails at the backend, not by proxy filtering.
- `CURRENT_USER()` can reveal the registered backend account.
- Query results, errors and catalogs pass through unredacted and can contain secrets.

## Protocol Limits

- Raw byte forwarding starts only after successful token validation and backend authentication.
- No query allowlist, no read-only enforcement, no result redaction.
- Frontend transport is plaintext (no frontend SSL); backend TLS via `?tls=true` does not protect the frontend leg.
- MySQL native auth methods include `caching_sha2_password` RSA full authentication on the host-to-backend leg.
- Established sessions are not terminated by `db regen`/`db off`; rotation affects new connections, listener shutdown happens on the watcher's roughly two-second reconciliation.

## Security And Operations

- `db regen mysql-orders` rotates the dedicated token for new connections; the global bridge token still authenticates this connection, so rotation alone is not revocation of the global path. Redistribute generated links afterwards.
- `db off mysql-orders` closes the listener on the next watcher sync without promising to terminate existing sessions; `db on mysql-orders` restores it.
- Give tunnel tokens only to authorized agents/tools; `db connect` output is a credential-bearing command.
- Proxy logs and some protocol errors can include backend addresses or server messages; inspect privately and redact before sharing.
- `db shell mysql-orders` opens a direct backend client on the host (stdin-TTY guarded). Passwords use child environment variables, but the MySQL host and user can still appear in argv. Not a sanitized tunnel session; not for agents.

## Safe Troubleshooting

| Symptom | Safe next step |
|---|---|
| `db test` fails | Human privately checks registered backend credentials, host/port reachability and TLS flags. Success output can include the real username/database |
| No listener despite enabled state | Check owned serve startup, store/key availability at startup, `VAULTY_KEEPER_DB_DIR`, bind interface and firewall |
| Authentication fails at the tunnel | Host obtains the current dedicated token (or uses the valid global token); client uses token-as-user with a placeholder password and the correct port/database. Check rotation/re-registration history |
| `?tls=true` connection fails | Verify CA trust/`tlsCAFile` (regular file ≤1 MiB), server `require_secure_transport`, hostname and TLS version. Never bypass required TLS; the frontend leg is plaintext regardless |
| Read succeeds, write/DDL denied | Expected for least-privilege accounts; check backend grants. Do not grant admin privileges merely to make an example pass |
| `Broken` entry | Means stored URL decryption failed, not a bad token. Human checks key source; token decryption failures are reported separately by Resolve |

`db show` prints the decrypted real URL; `db shell` launches a direct backend client. Their stdin-TTY checks do not establish human identity. Use `db test`, metadata and authorized bounded queries without seeking secrets.

Source checks: [MySQL handler/TLS](../../internal/dbproxy/mysql.go), [tunnel dispatch](../../internal/dbproxy/tunnel.go), [store/Resolve](../../internal/dbproxy/store.go), [CLI/db links](../../internal/cli/db.go), [watcher startup](../../internal/cli/remote.go). The C01 TLS fix is unit-tested; its one-off native TLS query evidence is not pinned by an integration test — re-verify against a real TLS backend before relying on it. No runtime tests or real-data operations were run for this guide.
