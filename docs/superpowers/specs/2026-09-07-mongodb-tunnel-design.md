# MongoDB Tunnel Design

> English | [中文](2026-09-07-mongodb-tunnel-design.zh-CN.md)
>
> **Accepted design decisions, recorded 2026-09-07.** This document owns the design rationale and supported boundary, not session authorization or a live test-status ledger. Current use and validation are maintained in the [MongoDB guide](../../mongodb-tunnel-guide.md#verification-status); the [dated execution record](../plans/2026-09-07-mongodb-tunnel.md) retains implementation evidence and its provenance. Do not execute this document as a plan.

## Goal And Boundaries

Add a MongoDB 8 tunnel for one fixed endpoint with ordinary username/password authentication. Clients receive a dedicated tunnel token, never the registered backend credentials. Integrate with the existing encrypted connection store, per-connection listeners, CLI and Web UI without changing PostgreSQL/MySQL/Redis behavior.

The client authenticates as virtual user `vaulty`, with the dedicated token as its SCRAM-SHA-256 password and `authSource=admin`. This is separate from backend authentication. The backend uses the registered username/password and SCRAM-SHA-256 or SCRAM-SHA-1; auth source precedence is explicit `authSource`, then URL database, then `admin`. An omitted URL database selects `test` for business commands, not for the default authentication source.

Frontend URIs include `directConnection=true&retryWrites=false`. MongoDB uses its dedicated token; do not inherit other protocols' global bridge-token fallback. Backend roles limit accessible data and writes; the proxy additionally restricts commands. Register a least-privileged business account, preferably read-only when writes are unnecessary.

## Architecture

```text
native client
  -> fixed proxy endpoint: virtual hello + token SCRAM-SHA-256
  -> bounded framing, command policy, namespace and cursor checks
  -> independent backend SCRAM, verified TLS when configured
  -> registered MongoDB 8 endpoint
  <- reconstructed control replies / unchanged business documents
```

Use existing `github.com/xdg-go/scram` and the public BSON package from `go.mongodb.org/mongo-driver/v2` at v2.9.0. Do not import driver `x/*`, internal authentication/wire packages, or the full client as the tunnel engine. MongoDB requires a persistent framed, command-aware relay after authentication, not the other protocols' raw `splice`.

Framing validates lengths, BSON, request/response IDs, flags and document sequences. Support OP_MSG and the limited legacy hello needed for connection establishment, not general legacy query operations. Reject compression, exhaust and fire-and-forget before forwarding. BSON depth is limited to 100 before decode; unauthenticated messages to 1 MiB and full frames to 48,000,000 bytes. Upstream write batches use type-1 document sequences. Bound reads, writes and authentication; malformed peers must not trigger unbounded allocation or spill payloads into errors.

Reviewed hello fields include `backpressure: "2"`, `maxTimeMS`, ordinary `lsid` and `$readPreference`. For fixed replica sets the frontend publishes virtual `setName: "vaulty"`, never actual host lists/set names. Mechanism discovery returns frontend SCRAM-SHA-256 only and only when requested. The shared admission limit is 128 connections, including established and monitoring sockets.

The exact registered-URL allowlist and defaults are maintained in the [guide](../../mongodb-tunnel-guide.md#registered-url). TLS must verify certificates and hostnames; a failed handshake never retries without TLS or disables verification. `replicaSet` validates the expected set name only, without discovery/failover. Current `connectTimeoutMS` also bounds each upstream command; changing that behavior requires updating both guide languages and tests.

## Supported Surface

Support common `find`, `count`, `distinct`, acknowledged `insert`/`update`/`delete`, `findAndModify`, read aggregation, cursors and ordinary logical session IDs. This is a reviewed subset of command fields and operators, not every option on those commands. Limited ping/version and database/collection/index listings are reconstructed for ordinary client navigation.

Exclude transactions, retryable writes, unacknowledged writes (`w:0`), compression, SRV/multiple seeds, topology failover, external authentication, full administration and diagnostic introspection. No promise of complete `mongosh`, native GUI or driver feature compatibility.

The default-deny policy recursively validates command fields, stages, expressions and referenced namespaces, including nested `$lookup`, `$unionWith` and `$facet`. Block business access to `admin`, `local`, `config` and `system.*`; frontend authentication on `admin` is not permission to read its data. Block `explain`, metadata stages such as `$collStats` even when nested, `$$USER_ROLES`, server-side JavaScript execution, and unreviewed views. Literal business data is not an expression context.

The orchestration layer verifies ordinary collection types for every referenced namespace before forwarding, including nested aggregation sources; syntax checks alone cannot detect a view's hidden pipeline. The backend account therefore needs `listCollections` privilege on the affected business databases. Existing namespaces must be type `collection`, excluding views and time-series collections. Missing namespaces can be read/created through ordinary CRUD only after a successful metadata check; denied or unverifiable metadata fails closed. Trusted concurrent DDL remains outside the guarantee.

## Replies And Cursors

Rebuild hello/authentication/control replies and known error structures from typed allowlists. Do not echo backend usernames, passwords, hostnames, topology, raw URL, backend error strings or unreviewed diagnostic fields in frontend authentication/control responses or proxy logs. Reconstruct top-level errors, `writeErrors` and `writeConcernError`; business result documents and literal values remain untouched, including BSON types and nested fields.

Keep cursor ownership at registered target + authenticated token identity + logical session scope, with namespace and original command kind. A client pool may continue a cursor on a different socket within the same scope. Reject cross-target/token/session cursor use; do not key ownership only by TCP socket or by numeric cursor ID. Record whether a cursor originated from `listCollections` or `listIndexes`: all later `getMore` batches must use that original metadata schema, never the business-document pass-through path. Handle exhaustion, `killCursors`, session end and cleanup without retaining stale ownership indefinitely.

The implemented caller supplies the recorded original command when reconstructing `getMore`. Cursor limits are 1,024 per registered-target/token scope and 16,384 across shared state, with session ownership checked independently; unused entries expire after 30 minutes.

## Security And Lifecycle

- The frontend leg is plaintext transport with SCRAM authentication. Use localhost or an isolated trusted network; upstream TLS does not encrypt client-to-proxy traffic.
- Trusted DBAs changing collection/view definitions, including check/use races, are outside the guarantee. Backend privileges and operational control remain necessary.
- Secrets already present in business documents are outside the guarantee. This is credential mediation and restricted command access, not general data redaction.
- Upstream MongoDB audit/diagnostic logs are not redacted by this proxy.
- Same-user access to host keys/store remains outside the isolation guarantee. Keep agents in a domain without those files or key access.
- `db regen` changes authentication for new connections, not credentials already accepted on established sessions. `db off` closes listeners after reconciliation; it does not promise to kill established sessions. Do not describe either as immediate session revocation.

## Evidence Ownership

The [dated execution record](../plans/2026-09-07-mongodb-tunnel.md) retains the 2026-09-07 task reports, command identifiers and uncommitted-worktree provenance. It is historical evidence, not a current task queue or permission to reuse that session's branch/worker assignments. The fixture design requires synthetic environment-derived URLs and owned MongoDB 8 containers; the in-process harness uses an explicit temporary store and synthetic test key, not a temporary HOME, real registered connections or Keychain. Fixture credentials/tokens are randomized; the store test key is deterministic.

The [guide verification matrix](../../mongodb-tunnel-guide.md#verification-status) is the single maintained location for current acceptance results and gaps. Do not infer native MongoDB TLS from fake-backend certificate tests, manual interactive TTY shell from Node startup-script tests, or independent follow-up review from local review. Updating design prose does not renew earlier passing results; the execution record preserves the original gaps without duplicating a live matrix here.
