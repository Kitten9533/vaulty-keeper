# Contributing to vaulty-keeper

> [中文](CONTRIBUTING.zh-CN.md) | English

Thanks for considering a contribution. This file covers how to build, test and
submit changes. The [documentation index](docs/README.md) maps the docs; the
[security model](docs/security-model.md) is the canonical reference for security
boundaries. Report security issues via [SECURITY.md](SECURITY.md), not a public
issue.

## Repository layout

```
internal/aesx    AES-256-GCM encrypt/decrypt (Java CryptoUtil compatible)
internal/apollo  snapshot storage, dual-key encryption, masking, fingerprints
internal/app     application orchestration
internal/bridge  masking proxy shared by serve/remote
internal/cli     command tree and TTY guards
internal/dbproxy database tunnels (PG/MySQL/Redis/MongoDB)
internal/i18n    en/zh UI and CLI text
internal/ui      loopback-only web UI (static assets are go:embed)
scripts/         frontend/docs checks and isolated DB test scripts
tools/javaref    Java reference for AES interop vectors
```

## Requirements

- Go 1.26+ and Make.
- Node.js is required only for the static checks inside `make test`
  (`scripts/check-ui.mjs`, `scripts/check-docs.mjs`), not for building.
- The DB tunnel test scripts use Docker (`scripts/dbtest.sh`,
  `scripts/mongotest.sh`); they are isolated and do not touch a real
  `~/.vaulty` or your keyring.

## Build and test

```sh
make build          # → bin/vaulty-keeper
make test           # frontend + docs static checks, then go test ./...
make install        # symlink to ~/.local/bin/vaulty-keeper
```

Run the broader checks before submitting anything that touches concurrency,
terminals, the frontend or docs:

```sh
go test -race ./...
go vet ./...
go vet -tags=mongointegration ./internal/dbproxy
bash -n scripts/dbtest.sh scripts/mongotest.sh
node scripts/check-ui.mjs
node scripts/check-docs.mjs
```

`git diff --check` must stay clean.

## Test coverage and DB fixtures

These are coverage pointers, not evidence of a run during this documentation
update; run relevant checks against the exact source revision after code
changes. MongoDB's dated evidence and remaining gaps live in the
[MongoDB guide](docs/mongodb-tunnel-guide.md).

- `internal/aesx`: byte-for-byte aligned with vectors from `tools/javaref/CryptoUtil.java` (Java 8 reference implementation; GCM is deterministic), plus key-length validation, wrong key/iv, invalid base64.
- `internal/apollo`: real pasted samples (incl. glued lines), comments, first `=`, URL params not split, encrypted snapshot on disk (no plaintext in file, 0600), diffs, sensitive detection.
- `internal/cli`: mixed argument order, import auto-naming, reveal (sensitive plaintext / explicit `--key`/`--iv` external ciphertext / multi-key JSON), edit (fake editor script), list/compare JSON, gen-key usability, aes `--name` list, completion.
- Regenerate Java vectors: `cd tools/javaref && javac CryptoUtil.java && java CryptoUtil encrypt <key> <iv> <plaintext>`

Isolated DB fixture scripts (Docker + Python 3 + a built binary; synthetic credentials and explicit temporary storage/test keys only, never a real `~/.vaulty` or keyring):

- **MongoDB**: entry points `bash scripts/mongotest.sh --mongosh` and `bash scripts/mongotest.sh --replica-set --mongosh`.
- **`scripts/dbtest.sh` is isolated and safe to run (C02 done).** The current script tracks its own serve PID and containers by label, uses a per-run temp dir and a fake HOME with synthetic keys, and `--clean` tears down only the resources it registered — it no longer broadly pkills serve processes, deletes fixed containers (`aipg`, `aimysql8`, `aimariadb`, `airedis`), or overwrites the real `~/.vaulty/bridge-token` as the historical version did. Read its header before use; keep it out of CI.

Historical script interface, **not a quick-start recommendation**:

```sh
make build
./scripts/dbtest.sh          # start and test; environment stays up, prints connection info
./scripts/dbtest.sh --clean  # teardown: stop serve, remove containers
```

Its actual fixtures are PostgreSQL `postgres:17.6-alpine`, MySQL `dockerproxy.net/library/mysql:8.0` (the recorded local image was 8.0.46, not 8.4/MariaDB) and Redis `redis:7`. Backend, tunnel and bridge ports are allocated dynamically per run (overridable via `PGP`/`TUN_PG`/... environment variables), so inspect the printed connection info; the prepared data and queries are:

| Registration | Prepared data / query |
|---|---|
| `pgdb` | `appdb.t`, `SELECT id,name FROM t ORDER BY id;` |
| `mysqltest` / `mysqlnative` | `shop.customers`, `products`, `orders`; `SELECT COUNT(*) FROM shop.orders;` |
| `cache` | Authenticated Redis; `PING`, synthetic `SET`/`GET` |

The script uses a separate DB directory/key, not the host-default `db shell` context. Its commands/log output are fixture-specific historical examples, not proof that every client/configuration works. See the [DB examples guide](docs/db-proxy-examples.md) for native-client setup and positive/negative queries; review logs before sharing because upstream metadata and access tokens may be present.

## Code conventions

- Follow the existing package layout and naming; keep changes minimal and
  root-cause, not refactors of unrelated code.
- After editing `internal/ui/static/*` (HTML/CSS/JS) you **must** run
  `make test` and then `make build`: the frontend is embedded with `go:embed`,
  and `make test` runs the JS syntax / DOM id / variable-shadowing / i18n-key
  checks before the Go tests.
- Do not shadow the global i18n helper `t()` in frontend code; it breaks
  rendering and the static checks catch it.
- Add or update Go tests for behavior changes; run `go test -race ./...` when
  the change involves concurrency or terminals.
- Never introduce secrets into code, tests, docs or commits. Tests use
  synthetic values and isolated temporary storage, never real `~/.vaulty`,
  keyring entries or real DSNs.

## Documentation conventions

- User-facing docs are **English-default with a `.zh-CN.md` pair**, a
  language-switch link at the top of each file, and matching content in both
  languages. `AGENTS.md` is the single exception (agent constraints, Chinese
  only).
- Keep one current home per subject: the security model lives only in
  `docs/security-model.md`; guides link it instead of duplicating it. Update
  `docs/README.md` when you add, move or retire a guide.
- Historical implementation records (plans/designs) are archived under a git
  tag (e.g. `docs-superpowers-archive`), not committed to the tree; do not
  rewrite archived records as current instructions and do not replay their
  commands against real data.
- If a change alters behavior, update the matching docs in the same change and
  make the doc describe the current source, not an old promise.

## Release process (maintainers)

Releases are cut by maintainers only:

1. Bump `Version` in `internal/cli/cli.go`.
2. `make release` — cross-compiles archives into `release/` and writes
   `release/sha256sums.txt`. It **deletes and recreates `release/`**, so never
   run it casually.
3. Push a `v*` tag; CI runs the test job and publishes release assets
   (idempotent if the tag release already exists).
4. The npm channel (`npm/`) is built from the same release archives via
   `npm/scripts/build.mjs` and published by a maintainer with the official
   registry.

See the GitHub Actions workflow in `.github/workflows/ci.yml` for the exact
gates.
