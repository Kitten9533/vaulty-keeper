# Database Proxy Tunnel Design

English | [中文](2026-08-31-db-proxy-tunnel-design.zh-CN.md)

> **Historical three-protocol design source, 2026-08-31; lifecycle annotated 2026-09-07. Non-executable.** Retains then-approved tradeoffs, proposed protocols/dependencies and test requirements without certifying all as implemented. Current use/boundaries: [DB architecture](../../db-proxy-architecture.md), [examples](../../db-proxy-examples.md). The [historical plan](../plans/2026-08-31-db-proxy-tunnel.md) preserves the evolution.
>
> **Superseded assumptions:** PostgreSQL now uses a trust-style frontend; MySQL uses a handwritten handshake, not a go-mysql dependency. Ports are allocated at registration without OS occupancy detection. Containers use `remote dblist`; a host with local keys generates connection commands. stdin does not remove upstream shell history; tokens do not cancel plaintext-network risk, and raw forwarding cannot guarantee secret-free business results/backend errors. Qualify MySQL TLS using the current guide. Global/dedicated-token and established-session semantics belong to current architecture, not this single-token proposal. The Mongo exclusion was only the 2026-08-31 scope; see the successor [Mongo design](2026-09-07-mongodb-tunnel-design.md) and [current guide/validation](../../mongodb-tunnel-guide.md). Do not execute registration, key access or client examples below.

Date: 2026-08-31. Status at that time: approved by the user item-by-item; a historical decision record, not authorization for later sessions.

## Goal

Enable AI in a container/isolation domain to query databases through native clients such as psql/mysql/redis-cli and receive data, while never exposing database URLs (address, username, password) to AI. The original goal stored URLs only as ciphertext in host vaulty-keeper.

In brief: encrypt connection URLs and use a protocol-aware tunnel to inject real credentials during handshakes. Do not encrypt database data or enforce read-only access in the proxy.

## Scope

- Include PostgreSQL, MySQL and Redis authentication-injection tunnels.
- Exclude MongoDB: the historical rationale was no mature Go proxy library and the cost of terminating SCRAM at both ends; the user would query with mongosh on the host, retaining credentials there.
- Exclude proxy-enforced read-only access (register a read-only account instead) and database-data encryption.

## Architecture Overview

```text
[Docker container: AI agent]
  psql "postgresql://$TOKEN:x@host.docker.internal:15432/appdb"
  redis-cli -a "$TOKEN" -p 15433
  mysql -h host.docker.internal -P 15434 -u "$TOKEN" -p<anything>
        | TCP with token gating
        v
[Host: vaulty-keeper serve --addr 0.0.0.0:8970]
  Existing HTTP /api/* bridge (including DB listing) + TCP port per connection
  Validate token -> connect using the real URL in db.json, including TLS options
                 -> inject credentials during handshake -> raw byte forwarding
        v
  Real database
```

- The intended container boundary exposed query results, not DSNs/credentials.
- Connection names such as `prod`/`cache` are not secret. The proposal used `vaulty-keeper db list` through the HTTP bridge to return name/type/tunnel port, with the same host as `VAULTY_KEEPER_BRIDGE_ADDR`, or `host.docker.internal` inside a container.
- Reuse serve token gating, throttling and failure backoff. Put implementation in `internal/dbproxy`; TCP listeners follow `--addr`.

## Storage And Keys

- `vaulty-keeper db init` creates an independent DB key using macOS Keychain / Windows Credential Manager, with `VAULTY_KEEPER_DB_KEY` as the proposed environment fallback. It is independent of snapshot/sensitive keys so a snapshot-key leak cannot decrypt DSNs.
- Proposed `~/.vaulty/db.json` (0600), with each connection URL separately encrypted by AES-256-GCM:

```json
{
  "connections": {
    "prod":  { "url_cipher": "<base64 AES-256-GCM>", "port": 15432 },
    "cache": { "url_cipher": "<base64 AES-256-GCM>", "port": 15433 }
  }
}
```

- Infer type from scheme: postgres/postgresql, mysql, redis/rediss. No `--type` needed.
- Port was proposed as optional, allocated at serve startup from 15432 in registration order, advancing on conflict.

## Command Surface

The original proposal intended the same local/container shape. Historical syntax, not runnable instructions; the inline URL is synthetic. The original stdin/history claim is retained here as an explicitly incorrect historical assumption: piping input does not erase the invoking shell's history.

```sh
vaulty-keeper db init
printf 'postgres://u:p@db.example.com:5432/mydb' \
  | vaulty-keeper db add prod [--port 15432]
vaulty-keeper db list [--json]
vaulty-keeper db rm <name> --yes
vaulty-keeper db shell <name>
```

- Read add DSNs only from stdin; non-TTY without input returns usage/error.
- `db shell` launches native host clients using decrypted URLs: PG/psql, MySQL/mysql, Redis/redis-cli, Mongo/mongosh. Reject non-TTY like reveal/edit; this host self-check is not the proxy path.

## Tunnel Mechanisms

### PostgreSQL: pgproto3 From pgx/v5

- Act as a frontend server to complete client authentication/token validation; independently authenticate to the real database with the registered user/password and URL `sslmode`.
- When both sides are ready, splice raw bidirectional TCP.
- Require startup `user` to equal the bridge token, e.g. `postgresql://<TOKEN>@host.docker.internal:15432/appdb`; disconnect on mismatch.
- Use registered database/user/password and ignore client-supplied alternatives.

### MySQL: Proposed go-mysql Server And Client Packages

- Use `server.NewCustomizedConn` and a custom handler to authenticate the frontend token in username while dialing the backend with registered credentials.
- The proposal relied on built-in `mysql_native_password`, `caching_sha2_password` and `sha256_password` support.
- Splice after both sides authenticate.
- Require frontend username equal to bridge token; ignore the password field.

### Redis: redcon Or Minimal Handwritten RESP

- On accept, send backend `AUTH <real password>` and read +OK; use TLS for rediss.
- Require first client command `AUTH <TOKEN>` (`redis-cli -a "$TOKEN"`); reply +OK on validation, then forward subsequent bytes.
- Use raw forwarding thereafter.

## Token Gating And Security

Historical intended guarantees, qualified by the lifecycle note above:

- Listen on serve's `--addr`, default `127.0.0.1`; for containers, `--addr 0.0.0.0:8970` also binds tunnels to all interfaces.
- The proposal claimed token gating offset all-interface exposure: disconnect LAN callers without a token; use random 128-bit tokens like the HTTP bridge to resist brute force.
- Audit every connection by time/source IP/name/validation result, without SQL or data logging.
- Real credentials were intended to appear only in host memory, never plaintext files or container replies.

## Error Handling

- Invalid token: log and disconnect, withholding details from the peer.
- Backend network/DNS/authentication failure: host log and peer disconnect.
- Mid-tunnel disconnect: close the other side and release resources.
- Startup port conflict: warn for that listener; others continue.
- Damaged db.json or missing key: do not start tunnels; HTTP bridge continues, DB listing returns an error.

## Testing And Verification

These were requirements, not passes:

- Unit tests: encrypted-store round-trip, scheme detection, allocation and token validation.
- Native Docker/local PG/MySQL/Redis queries via tunnels, with no registered URL in logs/replies.
- Wrong/missing token rejection and each database authentication mode.
- `go test ./...`, `go vet ./...`, `go test -race ./...`.
- Container end-to-end: host serve on all interfaces, clients using host.docker.internal.

## Proposed Dependencies

- `github.com/jackc/pgx/v5`, only pgproto3; the standalone pgproto3 repository was archived into pgx.
- `github.com/go-mysql-org/go-mysql`, server + client.
- Prefer minimal handwritten RESP for Redis AUTH, avoiding go-redis.

## Milestones

1. v1: DB storage/init/add/list/rm/key plus Redis, the simplest full path.
2. v2: PostgreSQL via pgproto3.
3. v3: MySQL via go-mysql.
4. v4: container end-to-end, README/AGENTS documentation and `db shell`.

## Explicit Non-Goals

- MongoDB proxy; host-local mongosh instead.
- Proxy SQL parsing/read-only enforcement; rely on account permissions.
- Database-data encryption.
- Pooling, multi-statement reuse or query caching.
