# Redis Tunnel Guide

> [中文](redis-tunnel-guide.zh-CN.md) | English
>
> Current Redis tunnel interface, source-checked on 2026-09-07 (working-tree behavior; not a new test run). The general security authority is the [security model](../security-model.md); the shared three-protocol mechanism lives in the [architecture guide](../db-proxy-architecture.md); prepared fixtures and lifecycle live in the [examples guide](../db-proxy-examples.md).

## Connection Model

Register one backend Redis endpoint with the backend AUTH password (and optional database index). The host keeps the URL and a dedicated 128-bit token encrypted in the DB store; connection name/type/port/state metadata is not encrypted. The client connects to a proxy port and its **first command must be `AUTH` with the tunnel token**; the proxy validates it, then authenticates to the backend with the registered AUTH password and SELECTs the registered database before forwarding raw protocol bytes.

After backend authentication the proxy forwards raw RESP traffic in both directions: no command allowlist, no result redaction. Backend ACLs and the token holder's authorization therefore define what is possible.

Redis accepts either the dedicated token **or the current serve global bridge token** for both newly registered and old connections. `db regen` rotates only the dedicated token; the global fallback remains valid.

## Authentication And Data Flow

```text
Client                         serve                        Backend
  | AUTH <token> (first cmd)      |                             |
  +----------------------------->| validate token               |
  |                              | connect + backend AUTH       |
  |                              | + SELECT registered db       |
  |                              +---------------------------->|
  |                              |<----------------------------+
  | PING / GET ...                |                             |
  +----------------------------->+---------------------------->|
  |<-----------------------------+<----------------------------+
```

The client's **first command must be `AUTH` with the tunnel token**. serve validates it, then authenticates to the backend with the registered AUTH password and SELECTs the registered database. Afterwards both directions are raw RESP protocol bytes; the proxy does not parse or filter commands.

## Registered URL

`redis://` is accepted for plaintext and `rediss://` for backend TLS. The URL carries the backend AUTH password and database index; the standard form is `redis://:PASSWORD@HOST:PORT/INDEX` (empty username, password in the password field). Client-side tokens use a separate frontend URI, not the registered URL.

The tunnel port is chosen at `db add` (`--port`) or auto-allocated starting at 15432; automatic allocation checks registered ports only, not OS occupancy. Same-name `db add` retains the stored port when `--port` is omitted but replaces the URL, creates a fresh token and resets `enabled` to false.

## Client Setup

Have the human operator initialize the host DB key with `vaulty-keeper db init` only if genuinely missing. A nonempty `VAULTY_KEEPER_DB_KEY` takes precedence over the keyring, must decode from Base64 to 32 bytes, and does not fall back on errors. Install `redis-cli` on the machine that runs it; neither vaulty-keeper nor the agent image supplies it by default.

The following human workflow reserves tunnel port `15434` and assumes backend database `0`. It is a usage example, not an executed fixture:

```sh
# Human host terminal: supply the backend URL through stdin, not argv.
vaulty-keeper db add cache --port 15434
vaulty-keeper db on cache
vaulty-keeper db test cache
vaulty-keeper serve --addr 127.0.0.1:8970
```

After `db add`, paste the authorized backend URL (e.g. `redis://:redispass@127.0.0.1:6379/0`) at the stdin prompt and press Enter. The prompt currently **echoes** input; avoid a recorded terminal. Register before starting serve: the DB watcher is created only when the store exists and the DB key is available at startup.

`serve` stays running. In another **host** terminal with the same store/key context, obtain proxy-only connection details:

```sh
vaulty-keeper db list
vaulty-keeper db connect cache
vaulty-keeper db connect cache --cmd
```

Do not put real backend URLs/passwords into AI messages, shell history or command-line arguments. An AI should use the tunnel connection information, not `db show`, the encrypted store, host keys or a direct backend shell.

The client invocation shape is (token as the AUTH password):

```sh
redis-cli -h 127.0.0.1 -p 15434 -a <TOKEN> --no-auth-warning
```

The generated link uses placeholder user `x`; redis-cli sends the token as its first AUTH. Bounded reads look like:

```sh
redis-cli -h 127.0.0.1 -p 15434 -a <TOKEN> --no-auth-warning GET demo
```

GUI fields (Redis Insight) use the proxy host/port with the virtual credentials: `redis://x:<TOKEN>@127.0.0.1:15434/0`. Install GUI tools separately; DBeaver Redis support depends on edition/plugin availability.

Container links substitute `host.docker.internal`, but `db connect --container` changes only printed addresses. A keyless container can run `vaulty-keeper remote dblist` for metadata only; generate tunnel commands on the host and deliver only the authorized credentials.

## Supported Operations

Whatever redis-cli/GUI can do against the backend, the tunnel forwards after the initial AUTH:

- `PING`, reads, writes and administrative commands are all raw-forwarded; the proxy is not a command firewall.
- Backend ACLs and the registered account's permissions govern what is possible; the proxy does not redact values.
- The registered backend password crosses the host-to-backend leg as the backend `AUTH`; the frontend leg uses only the tunnel token.

## Protocol Limits

- The first command from the client must be `AUTH` with a valid token. Both `AUTH <token>` and `AUTH <user> <token>` are accepted; the last argument is the token. The proxy then SELECTs the registered URL's database index (skipped when it is `0`); the client URI path is not applied.
- No command allowlist, no read-only enforcement, no result redaction.
- Frontend transport is plaintext; `rediss://` configures backend TLS only. Use localhost or an isolated trusted network.
- Established sessions are not terminated by `db regen`/`db off`; rotation affects new connections, listener shutdown happens on the watcher's roughly two-second reconciliation.

## Security And Operations

- `db regen cache` rotates the dedicated token for new connections; the global bridge token still authenticates this connection, so rotation alone is not revocation of the global path. Redistribute generated links afterwards.
- `db off cache` closes the listener on the next watcher sync without promising to terminate existing sessions; `db on cache` restores it.
- Give tunnel tokens only to authorized agents/tools; `db connect` output is a credential-bearing command.
- Proxy logs and some protocol errors can include server messages; inspect privately and redact before sharing.
- `db shell cache` opens a direct backend client on the host (stdin-TTY guarded): real backend context, not a sanitized tunnel session. Not for agents.

## Safe Troubleshooting

| Symptom | Safe next step |
|---|---|
| `db test` fails | Human privately checks registered backend AUTH password, host/port reachability and `rediss://` if TLS is required. Success output confirms backend auth, not tunnel listening |
| No listener despite enabled state | Check owned serve startup, store/key availability at startup, `VAULTY_KEEPER_DB_DIR`, bind interface and firewall |
| `NOAUTH` / authentication error at the tunnel | Host obtains the current dedicated token (or uses the valid global token); client sends it as the first AUTH with the correct port/database. Check rotation/re-registration history |
| Wrong database selected | Confirm the registered URL's database index and that the client does not issue `SELECT` before AUTH |
| `Broken` entry | Means stored URL decryption failed, not a bad token. Human checks key source; token decryption failures are reported separately by Resolve |
| Port conflict | Choose an unused explicit port and update clients; do not kill unrelated processes |

`db show` prints the decrypted real URL; `db shell` launches a direct backend client. Their stdin-TTY checks do not establish human identity. Use `db test`, metadata and authorized bounded queries without seeking secrets.

Source checks: [Redis handler](../../internal/dbproxy/redis.go), [tunnel dispatch](../../internal/dbproxy/tunnel.go), [store/Resolve](../../internal/dbproxy/store.go), [CLI/db links](../../internal/cli/db.go), [watcher startup](../../internal/cli/remote.go). No runtime tests or real-data operations were run for this guide.
