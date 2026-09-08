# vaulty-keeper Security Model

> [中文](security-model.zh-CN.md) | English
>
> Canonical reference for security boundaries. Guides link here instead of restating the full model. Source-checked against the current working tree on 2026-09-07; documentation corrections are not test evidence (see [Verification status](#verification-status)).

## 1 · Threat model at a glance

| Layer | Mechanism | What it actually prevents |
|---|---|---|
| 0 · same-user process | OS key storage (`Keychain`, Credential Manager, Secret Service) and 0600 files | Nothing against a process running as the same user (including an AI shell). It prevents *other* users, other machines and accidental plaintext, not a deliberately hostile same-user process. A same-user process can read keys the same way the tool itself does (`security find-generic-password -w`, reading `~/.vaulty/aes.json`). |
| 1 · masking + stdin-TTY guards | Non-TTY reads mask values unless explicitly safe; plaintext exits require stdin TTY | Accidental disclosure in scripts/AI pipelines, not human identity and not stdout protection. TTY `get` prints plaintext directly. |
| 2 · masking proxy (`serve`/`remote`) | Snapshot API values are always masked, even keys marked safe; every `/api` endpoint needs the bridge token | A client that only talks to the bridge cannot read snapshot plaintext. The bridge token is *not* metadata-only: it also authorizes PG/MySQL/Redis tunnels. |
| 3 · separate domain (container / account / VM) | Keys and ciphertext are outside the domain's reach; no mounts, reduced privileges | A deliberately hostile same-user AI, provided the domain cannot touch keys/ciphertext and its network exposure is controlled. |

Layer 0 means the tool's own guarantees cannot hold against a hostile same-user process. Defenses below that line are operating rules and accident prevention, not a promise against that adversary.

## 2 · Encryption at rest: scope and exceptions

**Encrypted at rest**: stored snapshot values (AES-256-GCM, independent random nonce per item) and registered database URLs/tunnel tokens (`~/.vaulty/db.json`), both 0600. Three independent keys when independently provisioned (snapshot key / sensitive-value key / DB key).

**Not covered by the at-rest guarantee** (plaintext locations exist by design or use):

- The AES key/IV list `~/.vaulty/aes.json` is plaintext JSON (0600).
- Import sources, editor temporary files (and editor backups), exports/downloads, clipboard content, terminal output and process memory.
- The upstream shell command that feeds stdin: consuming stdin does not erase `printf 'URL'` / `echo 'URL'` in shell history.
- Business data returned by database tunnels is not redacted or encrypted.

New sensitive snapshot values use the sensitive-value key; legacy sensitive ciphertext may still fall back to the snapshot key (`internal/apollo/store.go`). The dual-key separation claim applies to independently encrypted new data.

Key resolution: a nonempty environment override takes precedence over the platform secret store (`internal/apollo/keyring.go`); an invalid override does not fall back to the keyring. Keys must decode from Base64 to exactly 32 bytes. Regenerating a key can make existing data unreadable — check the configured source before regenerating.

## 3 · TTY gate is accident prevention, not human identity

`isTerminal()` checks only whether **stdin** is a TTY (`internal/cli/cli.go`). It does not check the operator or stdout.

- Plaintext exits (`apollo reveal`/`export`/`edit`, `list|compare --reveal`, `aes decrypt`, `db show`) require stdin TTY; `--yes` does not bypass that check. This refuses scripts/AI pipelines but does not prove a human is present.
- TTY `apollo get` prints plaintext directly (`internal/cli/cli.go`). stdout can be redirected.
- A same-user process can obtain key material regardless of TTY state (layer 0).

**Operating rule for agents**: never invoke plaintext exits on real secrets, never fabricate a TTY, and treat token-bearing output as credentials.

## 4 · Output authorization: safe flag vs secret classification

These are two independent axes:

- **Secret classification** (`--secret`, auto-detected by key name / credential-bearing URI / JWT shape) selects which key encrypts the value. It is persisted on import/set; reads do not rewrite it.
- **Safe flag** (`set --plain` / `mark --plain`) is an explicit user decision that authorizes plaintext output for non-TTY reads and UI GET (`internal/apollo/store.go:36,119`). Auto-detected items are never safe by default.

A safe value is *authorized plaintext output*, not merely a non-secret classification. GET can therefore return explicit safe values. UI GET also returns usable database connection tokens/links without the UI token (`internal/ui/db.go`) — these grant database access, not just masked metadata.

`--plain` mis-mark guard: setting a safe flag on a key whose name/value matches the sensitive rules is refused on non-TTY and requires a second confirmation on TTY.

## 5 · Tokens and their lifecycle

| Token | Guards | Scope | Rotation / revocation |
|---|---|---|---|
| UI token (fresh 128-bit per `ui` start) | Non-GET UI operations | Loopback UI only | Ends with the process; no shared state. Explicit plaintext routes additionally need `--allow-plaintext`, else 403 even with the token. |
| Bridge token (`~/.vaulty/bridge-token`, 0600) | All `/api` endpoints of `serve` | Snapshot mask reads **and** PG/MySQL/Redis tunnel access, for new and legacy connections (`tokenOKAny(user, globalToken, connToken)` in `postgres.go`/`mysql.go`/`redis.go`) | Regenerated on each `serve` start. Failed checks add 50 ms per failure, capped at 2 s (linear, not exponential backoff). |
| Dedicated DB tunnel token (128-bit per connection) | The one registered connection | PG/MySQL/Redis accept either dedicated or global token; MongoDB accepts **only** its dedicated token (no global fallback) | `db regen` rotates it; the old token stops working for **new** connections. Established sessions are not terminated. The global token is unaffected. |
| Same-name `db add` | — | Keeps the existing tunnel port if omitted, but creates a fresh token and resets the connection to enabled (`internal/dbproxy/store.go`) | Redistribute client links and review exposure after re-registration. |

`db on/off` toggles persisted listener state (watcher sync ~2 s); it does not promise to terminate established sessions. `db regen` and `db off` therefore are not immediate session revocation.

## 6 · Container and network boundary

- The supplied `docker-compose.yml` does **not** mount `~/.vaulty`, the OS secret store, `~/.ssh` or the Docker socket; it uses a non-root user, `cap_drop: ALL` and `no-new-privileges`. It is a starting point, not a measured prevention rate (Docker escape and daemon privileges remain).
- Compose does **not** restrict all egress to the bridge. The entrypoint prints only a `<set>`/`<unset>` marker for the bridge token, never the token itself (`docker/agent-entrypoint.sh`); the token reaches the container via the environment, where mounted projects, history and logs can still expose it. Do not share those logs.
- The bridge token grants PG/MySQL/Redis tunnel access, so granting it is granting database access, not just metadata. Project mounts, persistent history and logs can expose credentials.
- Tunnel listeners follow `serve --addr`; `0.0.0.0` exposes plaintext HTTP and tunnels to reachable networks. Token checks do not encrypt transport. Use `127.0.0.1` or a firewall-restricted interface.
- The proxy does not enforce read-only (register a read-only account if desired) and does not redact business data.

## 7 · Protocol-specific limits

- **PG**: delegates `sslmode` to the client library. **Redis**: `rediss://` for TLS.
- **MySQL `?tls=true` is fixed** (was C01): the proxy advertises `CLIENT_SSL`, uses the TLS-upgraded connection for authentication and forwarding, and accepts `tlsCAFile` for a private/self-signed CA. It was verified in one native TLS query against MySQL 8 with `require_secure_transport=ON` and a self-signed CA (non-empty `Ssl_cipher`, TLSv1.3, business queries through the tunnel), but that evidence is **not reproducible from the repo** — no integration test pins it (only fake-backend unit tests). Treat the capability as fixed and unit-tested; re-verify against a real TLS backend before relying on it. Do not disable required TLS on a real backend.
- **MongoDB 8**: fixed single endpoint, user `vaulty` + dedicated token as SCRAM password, `authSource=admin`, `directConnection=true&retryWrites=false`, no global fallback; dual-side auth plus a persistent command allowlist (not post-auth byte forwarding). Views, time-series, transactions, retryable writes, `comment`/`collation`/`create`/`createIndexes` are not open. Error code 13 alone cannot distinguish proxy policy from backend role denial. Full detail and limits: [mongodb-tunnel-guide.md](mongodb-tunnel-guide.md).
- Client-to-proxy transport is plaintext; use localhost or an isolated trusted network.

## 8 · Verification status

Recorded, not repeated here: the dated MongoDB 8.0.13 standalone/fixed-replica-set matrix (unit/race/vet/build, native Go driver, container `mongosh`) lives in [mongodb-tunnel-guide.md](mongodb-tunnel-guide.md#verification-status) and is historical — not a new run during documentation work.

Still **unverified**: actual MongoDB TLS (fake backend cert/hostname tests only), manual interactive `db shell`, and an automated independent re-review. MySQL TLS is **fixed** (C01: `CLIENT_SSL` capability + TLS-upgraded connection through auth and forwarding) but the native-TLS evidence is **not reproducible from the repo** (fake-backend tests only; the one-off real TLS query was not pinned as an integration test). `scripts/dbtest.sh` **is isolated now** (C02 done): per-run unique temp dir/containers, PID+label tracking, fake HOME and synthetic keys, `--clean` removes only its own registered resources; the historical version's broad pkill and real-HOME token overwrite no longer apply.

Documentation corrections cannot claim compile/test/browser/runtime/production evidence unless that evidence was actually executed and recorded at the matching source revision.
