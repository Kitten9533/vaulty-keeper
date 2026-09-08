# PostgreSQL Tunnel Guide

> [中文](postgres-tunnel-guide.zh-CN.md) | English
>
> Current PostgreSQL tunnel interface, source-checked on 2026-09-07 (working-tree behavior; not a new test run). The general security authority is the [security model](../security-model.md); the shared three-protocol mechanism lives in the [architecture guide](../db-proxy-architecture.md); prepared fixtures and lifecycle live in the [examples guide](../db-proxy-examples.md).

## Connection Model

Register one backend PostgreSQL endpoint with a normal username/password. The host keeps the URL and a dedicated 128-bit token encrypted in the DB store; connection name/type/port/state metadata is not encrypted. This is not a promise that input files, exports, terminal output or other host plaintext artifacts cannot exist. The client connects to a proxy port with the **tunnel token as the username** and any placeholder password (generated links use `x`); the proxy authenticates to the backend with the registered username, password and database.

After backend authentication the proxy forwards raw protocol bytes in both directions: no query allowlist, no result redaction. Backend roles therefore define what a token holder can actually do. The backend participates in its own authentication — SCRAM-SHA-256/MD5/cleartext as requested by the server — so the real password can cross the host-to-backend leg in cleartext authentication. Do not describe this as credentials never leaving the host.

PostgreSQL accepts either the dedicated token **or the current serve global bridge token** for both newly registered and old connections. `db regen` rotates only the dedicated token; the global fallback remains valid.

## Authentication And Data Flow

```text
Client                         serve                        Backend
  | virtual token (username)      |                             |
  +----------------------------->| validate token               |
  |                              | connect + backend auth       |
  |                              | (registered credentials)     |
  |                              +---------------------------->|
  |                              |<----------------------------+
  | query                         |                             |
  +----------------------------->+---------------------------->|
  |<-----------------------------+<----------------------------+
```

The client sends the tunnel token as the username with any placeholder password. serve validates it, connects to the backend with the registered username/password/database and authenticates (SCRAM-SHA-256/MD5/cleartext as the server requests). Afterwards both directions are raw protocol bytes; the proxy does not parse or filter queries.

## Registered URL

`postgres://` and `postgresql://` are both accepted. The URL carries the backend username, password, host, port and database; `sslmode` and other query options are delegated to the PostgreSQL client library on the host-to-backend connection. Client-side `sslmode=disable` on the tunnel URI is a separate, frontend-only setting.

The tunnel port is chosen at `db add` (`--port`) or auto-allocated starting at 15432; automatic allocation checks registered ports only, not OS occupancy. Same-name `db add` retains the stored port when `--port` is omitted but replaces the URL, creates a fresh token and resets the connection to enabled.

## Client Setup

Have the human operator initialize the host DB key with `vaulty-keeper db init` only if genuinely missing. A nonempty `VAULTY_KEEPER_DB_KEY` takes precedence over the keyring, must decode from Base64 to 32 bytes, and does not fall back on errors. Install `psql` on the machine that runs the client; neither vaulty-keeper nor the agent image supplies it by default.

The following human workflow reserves tunnel port `15432` and assumes a backend database `appdb`. It is a usage example, not an executed fixture:

```sh
# Human host terminal: supply the backend URL through stdin, not argv.
vaulty-keeper db add pgdb --port 15432
vaulty-keeper db test pgdb
vaulty-keeper serve --addr 127.0.0.1:8970
```

After `db add`, paste the authorized backend URL (e.g. `postgres://app:pgpass@127.0.0.1:5432/appdb`) at the stdin prompt and press Enter. The prompt currently **echoes** input; avoid a recorded terminal. Stdin does not remove a producing `printf 'URL'` command from shell history. Register before starting serve: the DB watcher is created only when the store exists and the DB key is available at startup. `serve --dir` selects snapshots; `VAULTY_KEEPER_DB_DIR` selects the DB store.

`serve` stays running. In another **host** terminal with the same store/key context, obtain proxy-only connection details:

```sh
vaulty-keeper db list
vaulty-keeper db connect pgdb
vaulty-keeper db connect pgdb --cmd
```

Do not put real backend URLs/passwords into AI messages, shell history or command-line arguments. An AI should use the tunnel connection information, not `db show`, the encrypted store, host keys or a direct backend shell.

The client URI shape is:

```text
postgresql://<DEDICATED_TOKEN>:x@127.0.0.1:<TUNNEL_PORT>/<DATABASE>?sslmode=disable&connect_timeout=5
```

`<...>` values are placeholders, not tested credentials. The tunnel password is ignored by the proxy, so any placeholder works. Bounded reads look like:

```sh
psql 'postgresql://<TOKEN>:x@127.0.0.1:15432/appdb?sslmode=disable' \
  -c 'SELECT id, name FROM public.t ORDER BY id LIMIT 10;'
```

GUI/JDBC fields use the proxy host/port and the virtual credentials: host `127.0.0.1`, port `15432`, user `<TOKEN>`, password `x`, database `appdb`. JDBC URL template: `jdbc:postgresql://127.0.0.1:15432/appdb?user=<TOKEN>&password=x`. Install GUI drivers separately.

Container links substitute `host.docker.internal`, but `db connect --container` changes only printed addresses. The host part of `serve --addr` controls listener interfaces; loopback-only serve is not ordinarily reachable from a Docker VM. A keyless container can run `vaulty-keeper remote dblist` for metadata only; generate tunnel commands on the host and deliver only the authorized credentials.

## Supported Operations

Whatever psql/JDBC/psycopg etc. can do against a backend account, the tunnel forwards. There is no per-command policy:

- DDL, DML, SELECT and administrative commands are all raw-forwarded; the proxy is not a SQL firewall.
- Writes and destructive operations are governed solely by backend grants and the token holder's authorization, not by the proxy.
- Session identity is the registered backend account: `SELECT current_user` reveals the real session owner, not the proxy token.
- Backend error messages, catalogs and query results pass through unredacted and can contain secrets, including plaintext passwords.

## Protocol Limits

- Raw byte forwarding starts only after successful token validation and backend authentication; there is no command-aware framing like the MongoDB relay.
- No query allowlist, no read-only enforcement, no result redaction. Registering a read-only account makes the connection naturally read-only.
- Backend TLS is delegated to the client library (`sslmode` in the registered URL). The frontend proxy leg is plaintext; use localhost or an isolated trusted network.
- Real passwords can travel on the host-to-backend leg (e.g. cleartext/SCRAM exchange); the generated client link deliberately contains only the proxy token, never the backend password. That does not make query results secret-free.
- Established sessions are not terminated by `db regen`/`db off`; rotation affects new connections, listener shutdown happens on the watcher's roughly two-second reconciliation.

## Security And Operations

- `db regen pgdb` rotates the dedicated token for new connections; the global bridge token still authenticates this connection, so rotation alone is not revocation of the global path. Redistribute generated links afterwards.
- `db off pgdb` closes the listener on the next watcher sync without promising to terminate existing sessions; `db on pgdb` restores it. These are configuration controls, not session revocation.
- Give tunnel tokens only to authorized agents/tools; `db connect` output is a credential-bearing command. The container entrypoint prints only a `<set>`/`<unset>` marker, never the token itself, but environment-delivered tokens remain credentials.
- Proxy logs include connection names, source addresses and handler errors; some protocol errors can include backend addresses or server messages. Inspect privately and redact before sharing.
- `db shell pgdb` opens a direct backend `psql` on the host (stdin-TTY guarded): real backend context, not a sanitized tunnel session. Not for agents.

## Safe Troubleshooting

| Symptom | Safe next step |
|---|---|
| `db test` fails | Human privately checks registered backend credentials, host/port reachability and `sslmode`. `db test` checks backend connectivity/authentication, not tunnel listening or read-only grants; its success output can include the real username/database |
| No listener despite enabled state | Check owned serve startup, store/key availability at startup, `VAULTY_KEEPER_DB_DIR`, bind interface and firewall; enabled metadata is not health |
| Authentication fails at the tunnel | Host obtains the current dedicated token (or uses the valid global token); client uses token-as-user with placeholder password, correct port and database. Check rotation/re-registration history |
| Read succeeds, write/catalog read denied | Expected for least-privilege accounts; check backend grants. Do not grant admin privileges merely to make an example pass |
| `Broken` entry | Means stored URL decryption failed, not a bad token. Human checks key source; token decryption failures are reported separately by Resolve |
| Port conflict | Choose an unused explicit port and update clients; do not kill unrelated processes |

`db show` prints the decrypted real URL; `db shell` launches a direct backend client. Their stdin-TTY checks do not establish human identity. Use `db test`, metadata and authorized bounded queries without seeking secrets.

Source checks: [PostgreSQL handler](../../internal/dbproxy/postgres.go), [tunnel dispatch](../../internal/dbproxy/tunnel.go), [store/Resolve](../../internal/dbproxy/store.go), [CLI/db links](../../internal/cli/db.go), [watcher startup](../../internal/cli/remote.go). No runtime tests or real-data operations were run for this guide.
