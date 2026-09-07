# MongoDB Tunnel Implementation Plan

> English | [中文](2026-09-07-mongodb-tunnel.zh-CN.md)
>
> **Dated execution record, 2026-09-07. Non-executable: do not replay this checklist.** Retained for implementation decisions, task evidence and gaps from that session. The original `subagent-driven-development`/`executing-plans` instruction, `master` permission and lead/worker assignments expired with that session; they grant no authority to later work. Current operation and validation status belong to the [MongoDB guide](../../mongodb-tunnel-guide.md#verification-status), not this checklist.

**Goal:** Implement the [approved design](../specs/2026-09-07-mongodb-tunnel-design.md) for a credential-mediating MongoDB 8 tunnel.

**Architecture:** Terminate frontend token SCRAM, authenticate independently to one backend, and retain command-aware framing throughout the connection. Enforce recursive policy and scoped cursors; rebuild control replies without rewriting business documents.

**Tech Stack:** Go, existing xdg SCRAM, official MongoDB driver v2.9.0 public BSON package only, existing CLI/UI, isolated MongoDB 8 and native clients.

**Evidence provenance (2026-09-07):** The implementation session reported completion, isolated native tests, repository tests/race/vet and rebuild. Its checkout was an uncommitted MongoDB working tree based on `7273bb21e8347777058a17fb95a16aa2a17a36dc`, on macOS 12.4 Intel, using MongoDB 8.0.13 fixtures and driver v2.9.0. The base commit alone does not reproduce those uncommitted changes; no exact tested-tree digest or archived raw log is supplied here. Checked items below retain that session's reports, not new validation. Initial independent findings were fixed with regressions and locally reviewed; automated independent follow-up review was unavailable. Actual MongoDB TLS and manual TTY shell were unverified. Later results and outstanding checks are maintained only in the guide's matrix, without rewriting this dated evidence.

## Historical File Map

| Area | Files and responsibility |
|---|---|
| Protocol | `internal/dbproxy/mongodb_config.go`, `mongodb_wire.go`, `mongodb_auth.go` and matching `_test.go`: URL validation, framing, backend TLS/SCRAM |
| Policy | `internal/dbproxy/mongodb_policy.go` and `mongodb_policy_test.go`: recursive command/namespace checks and reconstructed replies |
| Orchestration | `internal/dbproxy/mongodb.go` and `mongodb_test.go`, `internal/dbproxy/tunnel.go` and `tunnel_test.go`: frontend SCRAM, command loop, shared scoped cursors and collection-type checks |
| Product | `internal/dbproxy/store.go`, `testconn.go`, `links.go`; `internal/cli/db.go`; `internal/ui/db.go`, `static/app.js`, `static/index.html`; `internal/i18n/i18n.go`, with existing neighboring tests |
| Integration | `scripts/mongotest.sh`, `internal/dbproxy/mongodb_integration_test.go` using `mongointegration` tag: standalone/fixed single-node replica-set fixtures, optional container-native mongosh |
| Documentation | This bilingual plan/design/guide; bilingual README, DB architecture/examples and UI guide link/scope updates |

The design decision was to avoid driver `x/*` and internal wire/auth packages: runtime public BSON only, tagged tests using the official native Go client. The session assigned dependency/code changes to their owners, documentation separately, and AGENTS to its lead, with no unrelated protocol refactoring or commits. Those assignments and permissions are historical context, not current file ownership or authorization.

## Historical Checklist

All imperative wording and checked/unchecked outcomes below belong to 2026-09-07. Commands are retained as evidence identifiers, not instructions to execute; never substitute real registered URLs, keys or storage. Unchecked items describe evidence gaps at that date, not a second live validation queue.

## 1. Configuration, Framing And Authentication

- [x] Implement strict URL validation and table tests for the [exact allowlist](../../mongodb-tunnel-guide.md#registered-url), auth-source precedence, escaping, duplicate/unknown options, TLS conflicts and timeout bounds in `mongodb_config.go` / `mongodb_config_test.go`.
- [x] Implement framing and regression tests for lengths/BSON/flags/sequences, predecode depth 100, 1 MiB unauthenticated messages, 48,000,000-byte frames and outbound type-1 write batches in `mongodb_wire.go` / `mongodb_wire_test.go`.
- [x] Implement independent SHA-256/SHA-1 backend auth, expected replica-set name checks, verified TLS without insecure retry, bounded exchanges and generic errors in `mongodb_auth.go`. Keep the default 5-second connect timeout also bounding operations.
- [x] Lead reports fake-backend TLS unit tests passing with full certificate/hostname validation (`TestMongoAuthNetworkAndTLS`); this is not an actual MongoDB TLS fixture run.
- [x] Include all protocol/auth tests in final `make test` and race/vet gates. For focused reruns use `go test ./internal/dbproxy -run TestMongo -count=1`; inspect selected tests rather than accepting an empty selector as evidence.

## 2. Policy And Metadata

- [x] Implement default-deny command/field policy and tests for common reads, acknowledged CRUD, read aggregation and ordinary `lsid`, excluding transactions/retryable writes/`w:0` and protected databases/namespaces.
- [x] Implement recursive stage/expression/namespace checks, including nested lookup/union/facet, metadata/role introspection and server-side JS rejection, while distinguishing literal business data (`mongodb_policy.go` / `mongodb_policy_test.go`).
- [x] Reconstruct typed control/error/metadata replies, including write errors; preserve business BSON types/values. Lead-reported native integration covers policy/error sanitation and metadata pagination.
- [x] Run policy regressions with the complete suite and locally review control-response and recursive-policy handling after fixing the independent findings.

## 3. Orchestration And Cursor Ownership

- [x] Implement frontend `vaulty`/dedicated-token SCRAM-SHA-256 on `admin`, with no global-token fallback. Reviewed hello supports backpressure version `"2"`, maxTimeMS/session/read-preference fields and virtual set name `vaulty`, exposing no actual hosts.
- [x] Implement the persistent command loop and caller-side original-command tracking for metadata `getMore`. Registry ownership follows target/token with namespace/session checks across sockets, not socket-local state. Limits: 1,024 cursors per target/token scope, 16,384 shared, 30-minute unused expiry, 128 admitted connections including monitoring/established sockets.
- [x] Require successful backend `listCollections` checks for direct/nested references; allow ordinary collections or absent namespaces after a valid check, reject views/time-series or unverifiable metadata. Add orchestration regressions in `mongodb_test.go` and native view rejection cases.
- [x] Integrate listener dispatch/lifecycle. Lead-reported integration covers normal sessions, wrong/absent tokens, rotation and listener off/on; these do not imply immediate revocation of established sessions.
- [x] Verify final race results and review cursor scope, cleanup, metadata pagination and missing collections. Native integration verifies getMore after the connection pool replaces the original socket; this is not a claim about all pool scenarios.

## 4. Store, CLI And UI

- [x] Implement store/type detection, TestConn authentication probe and token lifecycle; native tests exercise explicit temporary encrypted stores and synthetic test keys.
- [x] Implement shared Mongo links, CLI help/commands and UI controls/translations with neighboring tests (`mongodb_links_test.go`, `internal/cli/mongodb_test.go`, `internal/ui/mongodb_test.go`). Generated `mongosh '<URI>'` commands intentionally expose only the proxy's token credential in argv, never the backend URI. Client helper aliases do not relax registered-URL validation.
- [x] Implement TTY-only direct `db shell` with constant startup script, child-only `VAULTY_KEEPER_MONGODB_URI`, removal before connect and no URI in argv/interactive global scope. Node-based tests exercise success/failure cleanup; actual interactive TTY use remains unverified.
- [x] Run final CLI/UI/i18n tests and `node scripts/check-ui.mjs` via `make test`; rebuild the embedded UI after checks pass.
- [ ] Manually verify direct `db shell` in an authorized human TTY if claiming interactive support as tested; keep that evidence separate from native tunnel mongosh tests.

## 5. Isolated Native MongoDB 8 Tests

- [x] Implement `scripts/mongotest.sh` with `mongo:8.0.13`, `--replica-set` and `--mongosh` modes. Require Docker/Go/OpenSSL; use container-native mongosh, random fixture credentials, loopback ports and fixture-only environment URLs.
- [x] Use explicit `t.TempDir()` stores and synthetic test keys in the in-process Go harness, with no actual user store or Keychain access. It does not require/change HOME. The script disables tracing and removes only its ownership-labelled container and own temporary files on exit/interruption.
- [x] Lead reports `bash scripts/mongotest.sh --mongosh` and `bash scripts/mongotest.sh --replica-set --mongosh` passing. Both cover native Go + mongosh; four backend mechanism/auth-source combinations are probed, with full typed CRUD/batch/aggregation/cursor/metadata/session/policy/lifecycle coverage in SHA-256/admin. See the [matrix](../../mongodb-tunnel-guide.md#verification-status).
- [ ] Actual MongoDB TLS fixture has not run. Add/run an isolated certificate-backed Mongo fixture before claiming native TLS acceptance; fake-backend certificate tests are separate evidence.

The harness exports `VAULTY_MONGO_TEST_URI`, `VAULTY_MONGO_TEST_CONTAINER` and `VAULTY_MONGO_TEST_REPLICA_SET`, then runs `go test -mod=readonly -tags=mongointegration ./internal/dbproxy -run '^TestMongoIntegration($|DriverHello$|DriverSASL$)' -count=1 -timeout=180s`. Without a fixture URI the live case skips. Never use actual registered connections for this command.

## 6. Documentation And Acceptance

- [x] Reconcile bilingual design/guide with current config, relay, collection privileges, link/shell behavior and actual harness modes. Keep the operation-timeout warning and registered/client URL distinction. Add concise README/architecture/examples/UI guide links without rewriting old diagrams.
- [x] Preserve limits: unchanged business data, trusted DBA definition changes, no complete introspection/GUI compatibility, plaintext frontend, unredacted upstream logs and established-session semantics. Attribute reported passes and retain explicit evidence gaps.
- [x] Update AGENTS with Mongo-specific behavior and isolated test commands.
- [x] Run `make test`, `go test -race ./...`, `go vet ./...`, then `make build`; all passed. Replica-set native integration also passed with `GOFLAGS=-race`.
- [x] Complete initial independent review, correct its findings, add regressions and perform final local source review of the changes.
- [ ] Automated independent follow-up review was unavailable; do not describe it as passed.
- [x] Record actual results and remaining limitations. The 2026-09-07 implementation session reported no staging, commits or pushes; this does not describe later repository state.
