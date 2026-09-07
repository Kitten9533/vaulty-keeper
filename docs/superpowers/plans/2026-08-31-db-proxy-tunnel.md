# Database Proxy Tunnel Implementation Plan

English | [中文](2026-08-31-db-proxy-tunnel.zh-CN.md)

> **Historical plan source, 2026-08-31; lifecycle annotated 2026-09-07. Non-executable: do not replay.** Retains all tasks, proposed implementation and test requirements. Unchecked items are neither a current backlog nor retroactively certified passes. Current operations: [DB architecture](../../db-proxy-architecture.md), [examples](../../db-proxy-examples.md). The [design](../specs/2026-08-31-db-proxy-tunnel-design.md) documents differences including PG frontend authentication, go-mysql, tokens and ports.
>
> The former requirement to invoke `subagent-driven-development` or `executing-plans` task-by-task belonged only to that session and has expired. Mongo exclusion is historical scope only; see the successor [Mongo design](../specs/2026-09-07-mongodb-tunnel-design.md) and [current guide/validation](../../mongodb-tunnel-guide.md). Do not run real-connection, key, build or test examples to complete this old checklist.

**Goal:** Add a DB tunnel proxy: encrypt URLs on the host using an independent DB key and `~/.vaulty/db.json`; start TCP tunnels alongside serve; let isolated AI use native psql/mysql/redis-cli against proxy ports while real credentials are injected during handshakes. The historical goal was that AI would never see the DSN.

**Architecture:** Extend one serve daemon with the existing masked HTTP bridge and a TCP listener per connection. Proposed authentication injection: PG fake server requests cleartext while a real client completes authentication via pgproto3; MySQL handwritten two-ended handshake/auth-response substitution, referencing go-mysql; Redis substituted AUTH and handwritten RESP forwarding. Token locations: PG user, MySQL username, Redis first AUTH.

**Tech stack:** Go; proposed pgx/v5 pgproto3, xdg-go/scram for PG SCRAM, and go-mysql as reference/client (Task 6). Redis uses handwritten RESP without a dependency.

Historical [spec](../specs/2026-08-31-db-proxy-tunnel-design.md).

## File Structure

- Modify `internal/apollo/keyring.go`: DB constants, `DBKey()`, `GenerateAndStoreDBKey()`, reusing platform keyrings.
- Create `internal/dbproxy/store.go` and `store_test.go`: db.json, URL encryption, schemes and ports.
- Create `internal/dbproxy/tunnel.go`: listeners, token gating and dispatch.
- Create `redis.go`, `postgres.go`, `mysql.go` in that package: RESP, pgproto3/SCRAM, handwritten MySQL handshake.
- Create `internal/dbproxy/tunnel_test.go`.
- Modify `internal/bridge/bridge.go`: `/api/db/list`, inject DBDir through config.
- Create `internal/cli/db.go`: init/add/list/rm/shell.
- Modify `internal/cli/remote.go`: `dblist` reads `/api/db/list` from containers.
- Modify `internal/cli/cli.go`: dispatch db and add usage.
- Modify README/AGENTS documentation.

## Task 1: DB Key

**Files:** Modify `internal/apollo/keyring.go`, `internal/apollo/key_test.go`.

- [ ] Add `DBKeychainAccount = "db-key"`, `EnvDBKey = "VAULTY_KEEPER_DB_KEY"`, `DBKey()` with env-before-keyring resolution and Base64/32-byte validation, and `GenerateAndStoreDBKey(force bool)`, mirroring snapshot-key functions.
- [ ] Test environment override and refusal to regenerate an existing key without force.
- [ ] Require `go test ./internal/apollo/...` to pass.

## Task 2: DB Store

**Files:** Create `internal/dbproxy/store.go`, `store_test.go`.

- [ ] Define `Conn{Name, Type, URL string, Port int}` with URL in memory only, `Store{Conns map[string]storedConn}`, and stored `{url_cipher, nonce string, port int}`.
- [ ] Map postgres/postgresql to postgres, mysql to mysql, redis/rediss to redis in `ConnTypeFromURL(raw string) (string, error)`; reject others.
- [ ] Implement `encryptConn/decryptConn` using AES-256-GCM like Apollo.
- [ ] Implement `Load(path string, key func() ([]byte, error))`, `Add(path, name, rawURL string, port int)`, `Remove`, and `List`. Encrypt writes with file 0600/directory 0700; list name/type/port, not URL.
- [ ] Allocate through `AllocPort(store, requested int, used map[int]bool, base int)` from base 15432, advancing past conflicts.
- [ ] Test add/remove/list round-trips, scheme recognition, conflict advancement and no plaintext URL in files.

## Task 3: CLI DB Commands

**Files:** Create `internal/cli/db.go`; modify `internal/cli/cli.go`.

- [ ] Dispatch init/add/list/rm/shell from `runDB(args)`.
- [ ] `db init [--force]` calls `apollo.GenerateAndStoreDBKey`.
- [ ] `db add <name> [--port N]` reads DSN only from stdin and calls `dbproxy.Add`; non-TTY missing input returns an error.
- [ ] `db list [--json]` reads local storage, with proposed bridge fallback when db.json/key is absent.
- [ ] Add `db rm <name> --yes`.
- [ ] `db shell <name>` rejects non-TTY; decrypt URL and execute psql/mysql/redis-cli.
- [ ] Add `case "db": return runDB(...)` and usage.
- [ ] Test stdin add, URL-free list and non-TTY shell rejection.

## Task 4: Redis Tunnel

**Files:** Create `internal/dbproxy/redis.go`, `tunnel.go`.

- [ ] Implement `StartTunnels(ctx, store, addr string, token string, log)`: net.Listen per connection, dispatch by type after accept.
- [ ] Parse first client RESP array and validate `AUTH <token>`, reply +OK, dial backend (TLS for rediss), send real AUTH unless no password, then splice with bidirectional `io.Copy`.
- [ ] Use an in-memory fake Redis AUTH/ECHO server to test gating, backend authentication and transparent queries.

## Task 5: PostgreSQL Tunnel

**Files:** Create `internal/dbproxy/postgres.go`.

- [ ] Proposed frontend pgproto3 Backend: answer SSLRequest with N, validate StartupMessage user against token, send `AuthenticationCleartextPassword`, ignore PasswordMessage contents, then send AuthenticationOk, ParameterStatus, BackendKeyData and ReadyForQuery.
- [ ] Backend pgproto3 Frontend: dial URL host/port/sslmode with TLS as configured, send real user/database StartupMessage, handle cleartext password, MD5 calculation or xdg SCRAM SASL until AuthenticationOk + ReadyForQuery.
- [ ] Splice when both sides are ready.
- [ ] Test real local Docker PG or mark skipped; at minimum test token and frontend sequence with a fake pgproto3 client.

## Task 6: MySQL Tunnel

**Files:** Create `internal/dbproxy/mysql.go`.

- [ ] Frontend: HandshakeV10 with generated salt, native_password plugin, no advertised SSL; validate HandshakeResponse41 username against token, send OK.
- [ ] Backend: read HandshakeV10, calculate plugin response (native SHA1 twice + salt XOR, caching_sha2 fast SHA256 XOR, full authentication plaintext over TLS or RSA public-key flow without TLS), send response, handle auth-more-data until OK.
- [ ] Splice raw bytes.
- [ ] Test fake native_password backend; Docker MySQL optional.

## Task 7: Serve And Bridge Integration

**Files:** Modify `internal/bridge/bridge.go`, `internal/cli/remote.go`, `internal/cli/cli.go`.

- [ ] Add `DBDir string` to bridge.Config; existing token gate protects `/api/db/list`, returning `[{name,type,port}]`.
- [ ] In runServe generate bridge-style token, start tunnels, then `bridge.Start` with that token.
- [ ] Add remote dblist: read `VAULTY_KEEPER_BRIDGE_ADDR/TOKEN`, then GET `/api/db/list`.
- [ ] Test bridge handler and local serve + Redis + remote dblist end-to-end.

## Task 8: Documentation And Closeout

**Files:** Modify README/AGENTS.

- [ ] README DB tunnel section: commands, examples, security boundaries and container connections.
- [ ] AGENTS DB commands and DB security layer.
- [ ] Require `make build`, `go test ./...`, `go vet ./...`, `go test -race ./...` all green.

## Explicit Non-Goals

MongoDB tunnel; proxy-enforced read-only access; pooling/query caching; multi-statement reuse.
