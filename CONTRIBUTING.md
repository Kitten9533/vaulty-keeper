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
- Historical records (`docs/superpowers/`) are dated evidence with a successor
  link; do not rewrite them as current instructions and do not replay their
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
