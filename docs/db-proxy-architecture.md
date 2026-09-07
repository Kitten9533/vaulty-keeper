# Database Tunnel Architecture

English | [中文](db-proxy-architecture.zh-CN.md)

Current PG/MySQL/Redis behavior, checked against source on 2026-09-07. These diagrams are explanatory, not a running-environment snapshot or new test evidence. See [usage and synthetic fixtures](db-proxy-examples.md) for preparation and explicit ports, and the [security model](security-model.md) for the canonical security boundary.

MongoDB is deliberately separate: the [MongoDB 8 guide](mongodb-tunnel-guide.md) owns its fixed-endpoint, persistent command-aware relay, virtual user `vaulty`, dedicated token as SCRAM-SHA-256 password, no global fallback, reviewed CRUD/read aggregation and sanitized control metadata. The raw-splice diagrams below do not apply to MongoDB. No protocol promises immediate session revocation.

## Overview

```text
Isolated client domain                    Host
psql / mysql / redis-cli / GUI            vaulty-keeper serve
  | proxy token + tunnel port               |
  +---------------------------------------> TCP listeners
                                             | resolve encrypted registered URL
                                             | authenticate to registered backend
                                             +------------------> PG / MySQL / Redis
  <---------------- query results, errors, raw protocol traffic ------------------+

remote list/get/compare/dblist ------------> HTTP bridge (/api requires global token)
                                             | masked Apollo reads / DB metadata

Host-only preparation: db add / db connect / db test, DB key and store
Client-side execution: native clients using deliberately delivered proxy credentials
```

`serve` hosts HTTP and TCP services in one process. Each registered database has a separate tunnel port, independent of the HTTP port. Backends may be loopback test containers, intranet servers or cloud databases. Container-side `127.0.0.1` means the container itself; `host.docker.internal` is a host route when supplied by Docker Desktop or Linux `host-gateway` configuration.

## Storage And Input

```text
Human host terminal: db add <name>, then enter the backend URL on stdin
  | current terminal input echoes; pipes do not erase their producer's shell history
  v
DB key: nonempty VAULTY_KEEPER_DB_KEY override, otherwise platform keyring
  | AES-256-GCM
  v
db.json (0600): encrypted URL and dedicated token; readable name/type/port/state metadata
  | db connect / db test / serve Resolve decrypt on the host
  v
Backend authentication over the host-to-database connection
```

The encryption promise covers stored URL/token values, not every field or every plaintext artifact. Snapshot values are also encrypted, but imported source files, exports/downloads, CLI editing temporary files and the separate `aes.json` key/IV configuration can contain plaintext. Host CLI/UI operations can decrypt values too; `serve` is not the only process where plaintext can exist. Process exit is not a secure-erasure guarantee.

The three storage keys are independent when independently provisioned. New sensitive snapshot values use the sensitive key; legacy sensitive values may still fall back to the snapshot key. A nonempty environment override takes precedence over the keyring and must decode from Base64 to 32 bytes; a bad override does not fall back. Check the key source privately before regenerating a key and losing access to existing data. Same-user processes are outside this storage boundary. See the [security model](security-model.md) for key and plaintext lifecycle details, including the separate external AES layer.

Stdin keeps the URL out of vaulty-keeper's argv, but `printf 'real URL' | ...` still exposes it in the producing shell's history and possibly tracing/process arguments. Inline credentials in the examples are synthetic only. Human registration currently uses an echoed line prompt, not a hidden password prompt. Do not put real URLs into AI messages, scripts or recorded terminals. `aes gen-key` prints generated secrets; it is not a masked diagnostic.

## Authentication And Data Flow

| Protocol | Client to tunnel | Tunnel to backend | After authentication |
|---|---|---|---|
| PostgreSQL | Token in username; password ignored (`x` in generated links); trust-style `AuthenticationOk` | Registered username, password and database; SCRAM-SHA-256/MD5/cleartext authentication as requested by server | Raw byte forwarding |
| MySQL | Token in username; arbitrary placeholder password; no frontend SSL | Registered credentials; `mysql_native_password` or `caching_sha2_password` including RSA full authentication | Raw byte forwarding; backend TLS via `?tls=true` (optional `tlsCAFile`) |
| Redis | First command must be `AUTH` with token (password field in generated URI; placeholder user `x`) | Registered AUTH and database SELECT | Raw byte forwarding |

```text
Client                         serve                        Backend
  | virtual token                |                             |
  +----------------------------->| validate token               |
  |                              | connect + backend auth       |
  |                              +---------------------------->|
  |                              |<----------------------------+
  | query                        |                             |
  +----------------------------->+---------------------------->|
  |<-----------------------------+<----------------------------+
```

The client need not know the registered password, but the backend necessarily participates in its own authentication. Redis AUTH and PostgreSQL cleartext authentication can transmit real passwords on that backend leg. Do not describe this as credentials never leaving the host. Frontend transport is plaintext; backend TLS does not protect it. Use loopback or an isolated trusted network and enforce network restrictions separately.

**MySQL `?tls=true` (was C01):** when `tls=true` is set the proxy advertises the `CLIENT_SSL` capability bit, upgrades the backend connection with TLS and uses the upgraded connection for authentication and raw forwarding. Add `tlsCAFile=<path>` (a PEM CA file, ≤1 MiB) to trust a private or self-signed CA; without it the backend certificate is verified against system roots. A one-off native TLS query (MySQL 8, `require_secure_transport=ON`, self-signed CA) reported a non-empty `Ssl_cipher` (TLSv1.3) and passed business queries, but that evidence is not pinned by an integration test in this repo — re-verify against a real TLS backend before relying on it. Plaintext mode is unchanged. PostgreSQL/Redis have their own TLS paths; this section supplies no new native TLS evidence for them.

## Tokens And Listeners

| Control | Current effect |
|---|---|
| `db add` | Generates an encrypted, random 128-bit dedicated token and defaults to enabled |
| Authentication for PG/MySQL/Redis | Accepts either the dedicated token **or the current serve global bridge token, for both newly registered and old connections** |
| `db connect` | Host command requiring the local DB key and Resolve; prints the dedicated token when present, otherwise the global token for legacy entries |
| `db regen` | Replaces the dedicated token for subsequent connection authentication; does not rotate the global token or terminate established sessions |
| `db off` / `db rm` | Reconciliation closes the listener; established sessions are not actively terminated |
| `db on` | Reconciliation attempts to restore listening; enabled is desired configuration, not proof of health |
| Same-name `db add` | Replaces the URL, generates a new token and resets to enabled; retains the stored port when `--port` is omitted. Redistribute the new token and explicitly restore off state if needed |

Resolve happens for each accepted connection; an in-flight handshake may already hold the previous token. Rotation is not a global-session revocation barrier. Changing a stored port while a listener is active does not automatically rebind that existing listener: turn it off, wait for closure, then on, or restart the owned serve process. Plan separately for existing sessions.

```text
serve startup
  +-- generate global token; HTTP bridge writes ~/.vaulty/bridge-token (0600) and prints it
  +-- DB store exists AND DB key available at startup?
        yes: start watcher -> initial sync -> repeat about every 2 seconds
             attempt listeners for enabled entries; log failures
        no:  bridge-only; first later registration/key setup requires serve restart
```

DB storage uses `VAULTY_KEEPER_DB_DIR` (or the default); `serve --dir` selects the **snapshot** directory, not the DB directory. The host part of `serve --addr` also selects DB listener interfaces. `db connect --container` only changes the **printed address**, not listeners or firewall rules. Use `0.0.0.0` only with deliberate interface/firewall controls: it exposes both HTTP and DB ports beyond loopback. Automatic port allocation skips registered ports starting at 15432 but does not probe OS port occupancy. A successful HTTP startup or enabled UI badge does not prove a DB listener or backend is healthy.

## What Access Actually Grants

PG/MySQL/Redis pass subsequent protocol traffic through without a query allowlist or result redaction. Register dedicated least-privileged accounts; use backend read-only grants when writes must be prohibited. SQL may reveal the real session owner (`current_user`, `CURRENT_USER()`), server addresses and privileged catalogs. Arbitrary business data, errors or server configuration can contain secrets, including plaintext passwords. Preventing disclosure of the registered password in a generated link is not proof that queries cannot return secrets.

Proxy logs include connection names, source addresses and handler errors; some old protocol errors include backend addresses or server messages. They are not a universally sanitized or complete per-query audit stream. Inspect privately and redact before sharing; do not scrape logs, stores or catalogs for secrets.

The HTTP mask bridge does not expose mutation APIs and masks even explicitly safe Apollo values. This does **not** make its global token harmless: that same token grants PG/MySQL/Redis database access, including writes allowed by the registered account. Local CLI/UI are different interfaces: explicitly safe values can appear in GET responses, and the UI DB-connect GET returns usable tokens/links without requiring the UI write token. `remote dblist` returns metadata, not tokens. `remote get` prints a mask; list/compare carry fingerprints. Fingerprints use normalized values under the same HMAC key, truncated to eight bytes; equality is a high-confidence signal, not proof of original-byte equality. Displayed lengths use UTF-8 bytes despite the `chars` label. See the [Apollo guide](apollo-snapshot-guide.md) for snapshot workflows.

TTY gates test stdin terminal status, not human identity or stdout destination. A pseudo-terminal can satisfy that test. Agents must not invoke plaintext exits (`db show`, direct `db shell`, reveal/export/decrypt), fabricate a TTY or use host keys to bypass the operational rule.

## Docker Roles And Exposure

Database fixtures and agent isolation are separate roles. A database image contains its own native clients; that does not install clients on the host or in the agent image. The repository Dockerfile builds vaulty-keeper inside a Go build stage, then packages Node, git and a non-root agent user. Agent CLI installation (including Codex) is optional. Install needed database clients/drivers separately in the execution domain.

Compose drops capabilities, enables `no-new-privileges`, mounts the project and persistent agent home, and does not intentionally mount host keys/store or the Docker socket. It does **not** enforce the bridge as the only network exit. Mounted project secrets remain reachable; broad network egress, host services, container escapes and same-user host access require separate controls. The entrypoint currently prints the actual bridge token, and persistent history/logs can retain it. Deliver only authorized proxy credentials, never host DB keys or real backend URLs. If only one connection is authorized, do not distribute the global token as though it were scoped to that connection.

**`scripts/dbtest.sh` is isolated and safe to run (C02 done).** The current script tracks its own serve PID and containers by label, uses unique per-run temp dirs/container names and a fake HOME with synthetic keys, and `--clean` tears down only registered runs — no broad pkill, no fixed container names (`aipg`, `aimysql8`, `aimariadb`, `airedis`), no real-HOME bridge-token overwrite. Read its header before use. Its images are `postgres:17.6-alpine`, MySQL **8.0** (`dockerproxy.net/library/mysql:8.0`, historically 8.0.46) and `redis:7`, not MySQL 8.4/MariaDB. The [examples](db-proxy-examples.md) provide an unexecuted synthetic recipe as an alternative walkthrough, not a requirement.

## Source And Evidence

Current behavior is grounded in [store/Resolve](../internal/dbproxy/store.go), [listener reconciliation and dispatch](../internal/dbproxy/tunnel.go), [PostgreSQL](../internal/dbproxy/postgres.go), [MySQL](../internal/dbproxy/mysql.go), [Redis](../internal/dbproxy/redis.go), [CLI input/link/shell handling](../internal/cli/db.go), [serve startup](../internal/cli/remote.go), [key lookup](../internal/apollo/keyring.go), [UI connect output](../internal/ui/db.go), [Compose](../docker-compose.yml) and [entrypoint](../docker/agent-entrypoint.sh).

This guide describes the working-tree implementation, not proof of inclusion in prebuilt 0.6.0 packages. Existing release archives lack the linked `docs/` guides; use the matching source checkout until packaging is corrected (C03). No release/package generation, runtime test, real TLS test or manual TTY check was performed for these documentation corrections. The dated historical Mongo validation matrix is maintained only in the [Mongo guide](mongodb-tunnel-guide.md#verification-status).
