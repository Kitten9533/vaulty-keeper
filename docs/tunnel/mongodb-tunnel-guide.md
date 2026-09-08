# MongoDB Tunnel Guide

> [中文](mongodb-tunnel-guide.zh-CN.md) | English
>
> Current MongoDB 8 interface, source-checked on 2026-09-07. This guide owns the dated historical validation matrix below; documentation corrections do not constitute a new test/build run. The general security authority is the [security model](../security-model.md).

## Connection Model

Register one backend MongoDB endpoint with a normal username/password. The host keeps the URL and dedicated token encrypted using the existing DB store; connection name/type/port/state metadata is not encrypted. This is not a promise that input files, exports, terminal output or other host plaintext artifacts cannot exist. The client connects to a proxy port with virtual username `vaulty` and a **dedicated tunnel token as its password**, using SCRAM-SHA-256 and `authSource=admin`. This token grants access through the proxy, so treat it as a credential even though it is not the backend password. The UI's DB-connect GET also returns usable tokens/links without requiring its write token; that output is not harmless masked metadata.

The proxy performs independent backend SCRAM-SHA-256 or SCRAM-SHA-1 authentication. Backend username, password, auth source and TLS settings come from the registered URL, not from the client's virtual credentials. Use a dedicated least-privileged backend account; the proxy's command allowlist does not make a writable account read-only. The account needs `listCollections` privilege on each business database so the proxy can verify ordinary collection types, in addition to its business read/write permissions. MongoDB does not use the other tunnels' global bridge-token fallback.

## Authentication And Data Flow

```text
Client                         serve                        Backend
  | SCRAM: user vaulty           |                             |
  | + dedicated token            |                             |
  +----------------------------->| validate dedicated token    |
  |                              | + backend SCRAM auth        |
  |                              +---------------------------->|
  |                              |<----------------------------+
  | command frame                |                             |
  +----------------------------->| allowlist + metadata check  |
  |                              +---------------------------->|
  |<-----------------------------+<----------------------------+
  | sanitized control replies; business documents unchanged    |
```

The client authenticates to the proxy as user `vaulty` with the dedicated token as its SCRAM-SHA-256 password (`authSource=admin`). serve validates the token, then performs its own backend SCRAM-SHA-256/SHA-1 authentication using the registered credentials. Unlike PG/MySQL/Redis there is **no raw byte splicing after authentication**: every command frame is validated against the reviewed allowlist and metadata policy before forwarding, and control replies are reconstructed/sanitized while business documents pass unchanged.

## Registered URL

Only `mongodb://` with one hostname or bracketed IPv6 address is accepted. The default port is `27017`; explicit ports must be `1..65535`. Username and password must both be nonempty. Percent-encode reserved characters in credentials and option values. No SRV, seed list, URL fragment or external authentication.

The URL path selects the default business database (`test` when omitted). Backend authentication source precedence is explicit `authSource`, then a nonempty URL database, then `admin`. Database/auth-source names must be valid UTF-8, 1..63 bytes, and exclude `/`, `\`, `.`, space, `"`, `$`, NUL, `:`, `*`, `<`, `>`, `|`, `?`, tab, CR and LF.

These are the complete currently accepted registered-URL query keys; names and boolean values are case-sensitive. Duplicate keys and all other options are rejected.

| Key | Accepted value / behavior |
|---|---|
| `authSource` | Nonempty database name, overriding the precedence above; `$external` is not accepted |
| `authMechanism` | `SCRAM-SHA-256` or `SCRAM-SHA-1`; if omitted, prefer SHA-256 from the advertised mechanisms, otherwise supported SHA-1; an absent mechanism list uses SHA-1 |
| `tls`, `ssl` | Literal `true` or `false`; aliases must agree if both present; default `false` |
| `tlsCAFile` | Nonempty host-side PEM CA file path, requiring TLS; current reader requires a regular file no larger than 1 MiB and uses it as the trust pool |
| `connectTimeoutMS` | Integer `1..120000`, default `5000`; currently also the deadline for each upstream command, not just connection establishment |
| `directConnection` | Only literal `true`, if supplied; endpoint remains fixed when omitted |
| `replicaSet` | Nonempty expected set name, excluding slash, backslash, comma, whitespace listed by the parser (space/tab/CR/LF) and NUL; validates backend hello, without discovery/failover |

TLS verifies the backend certificate and hostname using system trust by default or the configured CA file. Verification failures terminate connection setup; there is no automatic plaintext retry or insecure downgrade. `tlsInsecure`, `tlsAllowInvalidCertificates` and similar bypasses are rejected. When a mechanism is explicitly selected, it is not silently replaced after authentication failure.

`retryWrites`, `appName`, `compressors`, `readPreference`, `socketTimeoutMS`, `serverSelectionTimeoutMS` and every other unlisted key are **not accepted in the registered URL**, even if a normal MongoDB driver accepts them. Client tunnel URI options are a separate interface.

The command deadline means a long aggregation or `getMore` may fail at 5 seconds with default configuration. It is not MongoDB `maxTimeMS`, and a timed-out write is not proof that nothing was applied. Do not automatically replay a write; retryable writes are unsupported.

## Client Setup

Use the existing DB workflow for MongoDB. Have the human operator initialize the host DB key with `vaulty-keeper db init` only if genuinely missing. A nonempty `VAULTY_KEEPER_DB_KEY` takes precedence over the keyring, must decode from Base64 to 32 bytes, and does not fall back on errors. Check its source privately before regenerating a key; do not export real keys into an agent environment. Install `mongosh` on the machine that will run the client; neither vaulty-keeper nor the agent image supplies it by default.

The following human workflow reserves tunnel port `15438` and assumes the authorized backend business database is `businessdb`. Check OS port availability first and adapt the database to your registration. It is a usage example, not an executed fixture:

```sh
# Human host terminal: supply the backend URL through stdin, not argv.
vaulty-keeper db add mongo-app --port 15438
vaulty-keeper db on mongo-app
vaulty-keeper db test mongo-app
vaulty-keeper serve --addr 127.0.0.1:8970
```

After `db add`, paste the authorized backend URL at the stdin prompt and press Enter. The prompt currently **echoes** input; avoid a recorded terminal. Stdin does not remove a producing `printf 'URL'` command from shell history. Register before starting serve: the DB watcher is created only when the store exists and the DB key is available at startup. A bridge-only serve needs restarting after the first registration/key setup. `serve --dir` selects snapshots; `VAULTY_KEEPER_DB_DIR` selects the DB store.

`serve` stays running. In another **host** terminal with the same store/key context, obtain proxy-only connection details:

```sh
vaulty-keeper db list
vaulty-keeper db connect mongo-app
vaulty-keeper db connect mongo-app --container
```

Do not put real backend URLs/passwords into AI messages, shell history or command-line arguments. An AI should use the tunnel connection information, not `db show`, the encrypted store, host keys or a direct backend shell.

The client URI shape is:

```text
mongodb://vaulty:<DEDICATED_TOKEN>@127.0.0.1:<TUNNEL_PORT>/<DATABASE>?authSource=admin&authMechanism=SCRAM-SHA-256&directConnection=true&retryWrites=false
```

`<...>` values are placeholders, not tested credentials or assigned ports. Use the port/database returned for the registered connection. `db connect` requires the host's DBKey/Resolve even with `--container`; generate there and deliberately deliver only proxy credentials to the client domain. `remote dblist` returns metadata, not tokens. Do not mount host keys to make a keyless container run `db connect`.

Container links substitute `host.docker.internal`, but `--container` changes only printed addresses. The host part of `serve --addr` controls DB listener interfaces. Loopback-only serve is not ordinarily reachable from a Docker VM; a reachable bind plus firewall policy is required. `0.0.0.0` also exposes HTTP and DB ports on other interfaces, and Compose does not enforce a sole bridge egress path. Linux Docker needs a host-gateway mapping. Do not append backend `authSource`, replica-set details or TLS settings to the frontend URI.

For native clients/GUI connection fields, set host/port to the proxy, user to `vaulty`, password to the dedicated token, mechanism to SCRAM-SHA-256, auth database to `admin`, direct connection on and retryable writes off. `db connect` intentionally displays a token-bearing URI and a ready-to-run `mongosh '<URI>'` command for agents/tools. Running that command places the **tunnel token** in argv; it does not use an environment-based launcher and contains no real backend URI. Treat the token as an access credential and limit its exposure. For human use, supplying the token through a `mongosh` password prompt is safer than putting it in argv/history.

For the `15438` / `businessdb` registration above, this complete human command prompts for the **dedicated proxy token**, not the backend password:

```sh
mongosh 'mongodb://127.0.0.1:15438/businessdb?authSource=admin&authMechanism=SCRAM-SHA-256&directConnection=true&retryWrites=false' \
  --username vaulty --password
```

After connecting, a bounded read of an authorized ordinary `orders` collection is:

```javascript
db.getSiblingDB('businessdb').runCommand({
  find: 'orders',
  filter: {},
  projection: { _id: 1, status: 1 },
  limit: 20,
  batchSize: 20,
  singleBatch: true,
  maxTimeMS: 2000
})
```

This returns at most 20 documents in one batch, possibly none; it does not create seed data. Choose a permitted collection and fields without secrets. `maxTimeMS` bounds server execution, not all network/wall time; the upstream command deadline still applies. This password-prompt/read example has not been manually executed in this documentation pass and is not direct-shell TTY acceptance evidence.

`db shell mongo-app` is a separate direct backend workflow reserved operationally for a human. It launches `mongosh --nodb --shell --eval` with a constant startup script and the real URI in child-only `VAULTY_KEEPER_MONGODB_URI`, never in argv. The script removes that environment entry before connecting/entering the interactive session, also on connection failure, and does not put the URI in interactive global scope. This is not secure erasure or protection from same-user process access. The gate checks stdin TTY status, not human identity or stdout capture; agents must not fabricate a TTY or invoke this plaintext/direct-backend path. It is not an AI tunnel or a sanitized proxy session. The startup contract has Node-based tests, but actual interactive TTY use has not been manually verified.

## Supported Operations

| Area | Supported subset |
|---|---|
| Reads | Common `find`, `count`, `distinct`, filters/projections/sorts |
| Writes | Server-acknowledged `insert`, `update`, `delete`, `findAndModify`, subject to backend roles and reviewed options; acknowledgment is not human approval |
| Aggregation | Reviewed read stages/expressions, including recursively checked lookup/union/facet sources; no arbitrary pipeline support |
| Cursors/sessions | Business and sanitized metadata batches, `getMore`, `killCursors`, ordinary logical session IDs; not transactions |
| Navigation | Limited ping/version and sanitized database/collection/index listings; not complete administration or introspection |

Unknown commands, fields, stages and expressions are rejected. Business access to `admin`, `local`, `config`, `system.*`, views, time-series collections, `explain`, metadata stages such as `$collStats` (also when nested), `$$USER_ROLES` and server-side JavaScript execution is excluded. The proxy uses backend `listCollections` to check directly and indirectly referenced namespaces. Existing namespaces must have type `collection`; missing namespaces can be read or created through ordinary CRUD after a successful metadata check. Denied or unverifiable metadata fails closed. A successful connection test/ping alone does not establish the required collection privileges.

Transactions, retryable writes, `w:0`, compression, SRV/multiple endpoints, failover, external authentication and full admin/diagnostic features are not supported. Common unsupported fields include `comment` and `collation`; commands `create` and `createIndexes` are also rejected. Creating an absent ordinary collection through allowed CRUD after a metadata check does not authorize these explicit administration commands. GUI tools may send unsupported introspection commands even when CRUD works. Neither all native GUI features nor all MongoDB driver/session semantics are guaranteed.

Metadata is intentionally reduced. The relay records a cursor's original command and uses it for `getMore`, so `listCollections`/`listIndexes` continuation batches retain their original metadata filtering schema. Cursor ownership follows registered target/token and logical session, not socket; namespace/session mismatches are rejected even across pooled connections.

## Protocol Limits

- The persistent relay validates every frame; it never switches to raw byte splicing. BSON nesting is checked to depth 100 before decoding. Unauthenticated messages are limited to 1 MiB; full frames to 48,000,000 bytes. Upstream insert/update/delete batches use OP_MSG type-1 document sequences rather than one oversized BSON array.
- Cursor tracking permits 1,024 entries per registered-target/token scope, 16,384 across the shared tunnel state, with session ownership checked separately. Entries expire after 30 minutes without use. The shared connection admission limit is 128, covering handshake and established/monitoring connections, not just simultaneous SCRAM exchanges.
- Frontend hello accepts reviewed `backpressure: "2"`, `maxTimeMS`, ordinary `lsid` and `$readPreference` fields. A fixed replica-set endpoint publishes virtual `setName: "vaulty"`, not its real set name or host list. `saslSupportedMechs` is returned only when requested and contains frontend SCRAM-SHA-256 only.
- Lowercase option aliases accepted by native client helpers apply to their virtual-token connection, not to the registered backend URL parser. Client credentials remain `vaulty` + dedicated token; client `retryWrites=false` does not add `retryWrites` or lowercase aliases to the registered-URL whitelist.

## Security And Operations

- Frontend transport is plaintext with SCRAM authentication. Restrict it to localhost or an isolated trusted network; backend TLS does not secure this leg. Do not expose it directly to an untrusted network.
- Authentication/control replies, known error structures and proxy logs must not reveal real backend credentials, host or topology. Generic errors deliberately omit backend diagnostic details.
- Business documents are not redacted. Secrets already stored there can be read with the registered account's permissions; this is outside the guarantee.
- Trusted DBA changes to collection/view definitions, including check/use races, are outside the guarantee. Keep backend roles and operational controls restrictive.
- Upstream MongoDB logs are not redacted by the proxy. A human operator should inspect backend diagnostics privately, not copy secret-bearing logs into an AI session.
- Host keys/store are not protected from a hostile process with the same user access. Run agents in a separate isolation domain without those resources.
- `db regen mongo-app` rotates the token for new connections; existing authenticated sessions retain their established semantics. `db off mongo-app` closes listeners after the normal approximately two-second reconciliation, without promising to terminate existing sessions. `db on mongo-app` restores listening. These controls are not immediate session revocation.
- Same-name `db add` preserves the stored port when omitted but replaces the URL, generates a new token and resets `enabled` to false. Redistribute the new token and turn the tunnel on again if it should listen. If changing the port of an active listener, turn it off and wait for closure before updating/re-enabling, or restart the owned serve process. Enabled is configuration, not health; automatic allocation excludes registered ports, not OS occupancy. Already accepted handshakes can retain previously resolved state.
- Unlike MongoDB, PG/MySQL/Redis accept the global bridge token for both new and old registrations. Rotating a dedicated token does not remove that access. The container entrypoint prints only a `<set>`/`<unset>` marker for the bridge token, never the token itself; do not treat environment-delivered tokens or generated links as harmless logs.

## Safe Troubleshooting

| Symptom | Safe next step |
|---|---|
| `invalid MongoDB connection configuration` at registration | Human checks the registered-URL allowlist: one endpoint, credentials percent-encoded, valid names/port, no duplicate keys, case-sensitive booleans, no client-only `retryWrites`, `appName` or SRV. Share a synthetic structure, never the real URL |
| No listener / selection timeout | Check owned serve startup, watcher prerequisites, configured port and bind interface, firewall/container host route. Enabled metadata and successful `db test` do not prove frontend listening |
| Frontend authentication fails | Host obtains current dedicated token; client uses user `vaulty`, SCRAM-SHA-256, `authSource=admin`, correct proxy port. No global bridge token or backend password fallback; check rotation/re-registration |
| Backend authentication / `db test` fails | Human privately checks registered backend credentials, authSource precedence, selected SCRAM mechanism and expected replica-set name. Do not probe with `db show`/direct shell in an AI session or send raw upstream logs |
| Policy or role denial, often code `13` | Inspect the sanitized message and command shape, not just the number: code 13 alone cannot distinguish proxy policy from backend permissions. Remove unsupported `comment`/`collation` or GUI admin probes; check business namespace/ordinary collection and required `listCollections` privilege. Do not broaden roles or bypass checks just to make a command pass |
| TLS setup fails | Human checks host-side CA path/file limits, certificate trust/hostname and endpoint configuration. No insecure flags or plaintext downgrade. Fake-backend TLS tests are historical evidence, not actual MongoDB TLS acceptance |
| Long read / `getMore` or write times out | Default upstream command deadline is 5000 ms; bound reads and review permitted `connectTimeoutMS` privately. A write may already have applied: reconcile its outcome using authorized reads before any human retry decision; never blindly replay it |
| `Broken` entry / token decryption error | `Broken` means URL decryption failure; Resolve reports token decryption separately. Human checks environment override versus keyring source before key regeneration or re-registration, accounting for new token/enabled state |

Report only sanitized error category, operation shape, client version and proxy-side port/state. Business documents and upstream logs may contain secrets. Denied or unverifiable collection metadata fails closed; ping success is not a reason to relax policy.

## Verification Status

Historical implementation-session evidence dated **2026-09-07**, retained from the implementation record and design (archived under git tag `docs-superpowers-archive`). Platform: macOS 12.4 Intel with Docker Desktop; MongoDB 8.0.13 standalone and authenticated fixed single-node replica set. The tested state was an **uncommitted Mongo worktree based on `7273bb21e8347777058a17fb95a16aa2a17a36dc`**, not that commit alone and not a released binary. An exact dirty-tree digest and remaining tool patch versions were not recorded here; do not infer reproducibility from the base SHA alone. This Mongo work was later released in **v0.8.0** (2026-09-07).

The archived design explains accepted decisions; the archived plan retains execution provenance, not standing lead/worker assignments or reusable branch/commit permissions. This guide is the current home of the dated validation matrix. Every pass below is historical, **not rerun for the documentation correction**; examples above are unexecuted. The Mongo work is included in released **v0.8.0**, whose archives bundle the `docs/` guides; the older 0.6.0 packages predate MongoDB and lack the linked `docs/` files.

| Check | Status / scope |
|---|---|
| `bash scripts/mongotest.sh --mongosh` | Passed: MongoDB 8.0.13 standalone, native Go driver and container-native `mongosh` |
| `bash scripts/mongotest.sh --replica-set --mongosh` | Passed: authenticated, fixed single-node replica set on MongoDB 8.0.13; not multi-node failover |
| `GOFLAGS=-race bash scripts/mongotest.sh --replica-set --mongosh` | Passed with the race detector, including cursor continuation after pool connection replacement |
| Backend authentication | Passed: SHA-256/SHA-1 crossed with `admin`/`businessdb` auth sources (four combinations); full business suite runs in the SHA-256/admin case |
| Business/proxy integration | Passed: typed BSON CRUD, batches, read aggregation, business/metadata paging, normal sessions, virtual identity, policy/error sanitation, wrong/absent token, rotation and listener off/on |
| TLS unit tests | Passed against fake backends with full certificate/hostname validation; actual MongoDB TLS fixture **not run** |
| Direct `db shell` | Constant-script/child-env contract covered through Node tests; actual interactive TTY session **not manually verified** |
| Repository gates | Passed: `make test` (including UI checks), `go test -race ./...`, `go vet ./...`, `go vet -tags=mongointegration ./internal/dbproxy`, `make build` |
| Review | Initial independent findings fixed with regression tests; final local source review completed. Automated independent follow-up review was unavailable, not passed |

The script also supports `bash scripts/mongotest.sh` (Go driver only) and `bash scripts/mongotest.sh --replica-set` (Go driver, authenticated single-node replica set). Prerequisites are Docker, Go and OpenSSL; `--mongosh` uses the fixture image's client, not a host installation. Each run creates a random loopback fixture, supplies synthetic URLs through `VAULTY_MONGO_TEST_URI` with `VAULTY_MONGO_TEST_CONTAINER` / `VAULTY_MONGO_TEST_REPLICA_SET`, and cleans only its ownership-labelled container and temporary resources.

The in-process Go harness uses an explicit `t.TempDir()` store and the deterministic synthetic `testKey(t)` from `store_test.go`; fixture passwords and tunnel tokens are random, not that store key. It does **not** require or create a temporary HOME, read the actual user store, or call Keychain. Do not substitute real registered URLs for fixture variables. The script runs:

```sh
go test -mod=readonly -tags=mongointegration ./internal/dbproxy -run '^TestMongoIntegration($|DriverHello$|DriverSASL$)' -count=1 -timeout=180s
```

Running the tagged tests without fixture variables skips the live integration case and is not evidence of a native MongoDB pass. No complete GUI/introspection compatibility, actual MongoDB TLS or interactive shell validation is implied by the passing matrix.

Source checks for this guide: [registered URL parser](../../internal/dbproxy/mongodb_config.go), [backend auth/TLS/deadline](../../internal/dbproxy/mongodb_auth.go), [persistent relay](../../internal/dbproxy/mongodb.go), [command/metadata policy](../../internal/dbproxy/mongodb_policy.go), [client links](../../internal/dbproxy/links.go), [CLI prompt/direct shell](../../internal/cli/db.go), [watcher startup](../../internal/cli/remote.go), [store lifecycle](../../internal/dbproxy/store.go) and [native fixture](../../scripts/mongotest.sh). MySQL TLS and `dbtest.sh` isolation status live in the [security model](../security-model.md#8--verification-status), not here.
