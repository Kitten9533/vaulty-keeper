# vaulty-keeper

> [中文](README.zh-CN.md) | English

Personal AI toolbox (single Go binary). Snapshot values and registered database URLs/tunnel tokens are encrypted at rest. This does not cover every local file: the AES key/IV list is plaintext JSON, and import sources, exports/downloads and editor temporary files can contain plaintext. `vaulty-keeper ui` serves a loopback-only web UI for snapshots, AES and database connections. OS key storage and optional native clients require platform facilities.

Start here for installation and examples; the [documentation index](docs/README.md) links current guides. The [security model](docs/security-model.md) is the canonical reference for security boundaries, [SECURITY.md](SECURITY.md) covers reporting, and [CONTRIBUTING.md](CONTRIBUTING.md) covers building and contributing.

## Documentation map

Every user-facing doc is English-default with a `.zh-CN.md` sibling linked at its top. The database tunnel is the flagship feature; its implementation and usage live in the three `db-*` guides below.

| What you want | Read |
|---|---|
| Full command reference (apollo / aes / misc) | [docs/cli-reference.md](docs/cli-reference.md) |
| DB tunnel: architecture, sequence, credentials | [docs/db-proxy-architecture.md](docs/db-proxy-architecture.md) |
| DB tunnel: usage examples & fixtures | [docs/db-proxy-examples.md](docs/db-proxy-examples.md) |
| PostgreSQL tunnel: setup, options, troubleshooting | [docs/tunnel/postgres-tunnel-guide.md](docs/tunnel/postgres-tunnel-guide.md) |
| MySQL tunnel: setup, TLS, troubleshooting | [docs/tunnel/mysql-tunnel-guide.md](docs/tunnel/mysql-tunnel-guide.md) |
| Redis tunnel: setup, options, troubleshooting | [docs/tunnel/redis-tunnel-guide.md](docs/tunnel/redis-tunnel-guide.md) |
| MongoDB 8 tunnel: options, limits, verification matrix | [docs/tunnel/mongodb-tunnel-guide.md](docs/tunnel/mongodb-tunnel-guide.md) |
| Container / agent isolation | [docs/container-isolation.md](docs/container-isolation.md) |
| Web UI: pages, fields, confirmation flows | [docs/ui-guide.md](docs/ui-guide.md) |
| Apollo snapshots: implementation walkthrough | [docs/apollo-snapshot-guide.md](docs/apollo-snapshot-guide.md) |
| Security boundary (canonical) | [docs/security-model.md](docs/security-model.md) |
| Build, test, contribute | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Security reporting | [SECURITY.md](SECURITY.md) |

## Quick start

**Option 1: Install via npm** (recommended; Node.js 18+ required):

```sh
npm install -g vaulty-keeper
```

The npm channel downloads the prebuilt binary through the package manager rather than a browser, so macOS Gatekeeper and Windows SmartScreen do not flag it as a downloaded file — no "unidentified developer" / "Windows protected your PC" prompt. It installs the same Go binary as the releases below.

**Option 2: Download a prebuilt binary** (no Go or Node.js required): grab the archive for your platform (macos/linux × x86_64/arm64, windows × x86_64) from [Releases](https://github.com/Kitten9533/vaulty-keeper/releases), extract, and put `vaulty-keeper` on your PATH (`vaulty-keeper.exe` on Windows; archive names include the version). Each release also attaches `sha256sums.txt` with the archive checksums.

The binaries are not code-signed, so a **browser-downloaded** archive may be blocked at first run:

- **macOS** — Gatekeeper shows "cannot be opened because the developer cannot be verified" (or "Apple cannot check it for malicious software" on Apple Silicon): right-click the binary and choose *Open*, or remove the quarantine attribute once with `xattr -cr /path/to/vaulty-keeper`.
- **Windows** — SmartScreen shows "Windows protected your PC" / unknown publisher: click *More info* → *Run anyway*, or unblock once in PowerShell with `Unblock-File vaulty-keeper.exe`.

Installing via npm or building from source avoids these prompts, because the file never carries a browser-download marker.

This README describes the current source workspace. The latest release is **v0.8.0** (2026-09-07): it includes MongoDB 8 tunnel support and bundles both READMEs, LICENSE, AGENTS.md and the `docs/` guides (index, security model and five guides, English + Chinese). Archives built from the current tree additionally bundle CONTRIBUTING.md, SECURITY.md and the newer `cli-reference`, `container-isolation` and per-protocol tunnel (`postgres` / `mysql` / `redis`) guides; that packaging has not been published in a release yet. Historical implementation records are archived under git tag `docs-superpowers-archive`, not in this tree or the archives. Older archives such as **0.6.0** do not include `docs/`; for those, browse the matching tag's `docs/` in the [source repository](https://github.com/Kitten9533/vaulty-keeper). Guides in this workspace are not evidence for binaries older than v0.8.0.

The following setup is for a human on the host and creates local keys/state; it is not an isolated test or an instruction for an agent to access real secrets:

```sh
vaulty-keeper apollo init      # first run: create snapshot key (macOS Keychain / Windows Credential Manager / Linux Secret Service)
vaulty-keeper sensitive init   # first run: create sensitive-value key
vaulty-keeper ui               # long-running; use another terminal for later commands
```

**Option 3: Build from source** (Go 1.26+, Git and Make; Node.js is also required for `make test`):

```sh
git clone https://github.com/Kitten9533/vaulty-keeper.git
cd vaulty-keeper
make build          # → bin/vaulty-keeper
make install        # symlink to ~/.local/bin/vaulty-keeper
make test           # unit tests (incl. Java↔Go interop vectors)
```

Ensure `~/.local/bin` is on PATH, or use `bin/vaulty-keeper`. Maintainers can use `make release` to cross-compile archives, but it **deletes and recreates `release/`**; it is not a quick-start check and does not publish anything. The shell examples below use POSIX shell syntax; use WSL/Git Bash or adapt them for PowerShell.

## Manual operation

Running `vaulty-keeper` with no arguments prints the full command tree and performs first-run initialization automatically: creates the data directories (`~/.vaulty/`, `~/.vaulty/apollo/`, 0700), seeds a `default` AES key/iv entry (`~/.vaulty/aes.json`, 0600), and checks key initialization — the snapshot and sensitive-value keys always, and the DB key only when a DB store already exists (`~/.vaulty/db.json`); the DB key is otherwise created on first `db init`. Missing keys are offered for initialization on your TTY, or a hint is printed when not on a TTY. For manual CRUD, the recommended entry point is `vaulty-keeper ui` (local web UI covering all snapshot and AES features), or the subcommands below.

```sh
vaulty-keeper            # print full command tree
vaulty-keeper <cmd> -h   # full help for a subcommand (syntax + flags)
```

## Local web UI

```sh
vaulty-keeper ui
vaulty-keeper ui --dir /path/to/snapshots --port 8080
vaulty-keeper ui --no-open
vaulty-keeper ui --allow-plaintext    # explicitly enable plaintext endpoints (see below)
```

- Listens on `127.0.0.1` only; never exposed to the LAN.
- A random access token gates non-GET operations. **Open the full URL printed at startup**, such as `http://127.0.0.1:8080/?t=<token>`; reads do not require this UI token. Even an explicit `--port` may increment when busy, so use the printed port. A token is not proof that the caller is human.
- **Plaintext endpoints are disabled by default** (`reveal`/`export`/plaintext edit/AES decrypt/real DB URL return 403 unless enabled, even with the token); restart with `--allow-plaintext` to enable. This is not a blanket no-plaintext guarantee: snapshot GET returns explicitly safe values, and **GET `/api/db/connect` returns usable tunnel tokens/links without the UI token**. Local callers may therefore obtain database access, not just masked metadata.
- Startup prints a warning: do **not** send the token-bearing URL to AI/scripts, logs, or shell history.
- Includes snapshot browse/search/CRUD, import, env comparison, plaintext edit, export (download or copy), AES encrypt/decrypt (manual key/iv), snapshot and sensitive key initialization, **database tunnels** (register/test connections, generate client commands, rotate tunnel tokens, view the real URL with `--allow-plaintext`).
- Confirmation varies by action: AES transform and View URL issue requests directly; a request's `confirm: true` field is not an independent human confirmation. Plaintext responses use `Cache-Control: no-store`, which does not prevent capture, clipboard copies or downloads.
- The app does not deliberately persist snapshot contents in browser storage; displayed values, downloads, extensions and browser/OS capture remain outside that guarantee. See the UI guide for actual navigation and field availability.
- The UI defaults to **English** and can switch to Chinese (中文). Browser preferences and best-effort shared-file synchronization can diverge from the CLI (see [Language](#language-ui--cli)).
- On macOS, startup tries to reuse a matching UI tab in supported browsers before opening a new one; `--no-open` disables browser opening.
>
> A page-by-page guide to the UI (features & usage) lives in **[`docs/ui-guide.md`](docs/ui-guide.md)** ([中文版](docs/ui-guide.zh-CN.md)).

## Language (UI & CLI)

The tool provides English and Chinese UI/CLI text, with a shared preference file but different resolution rules.

- The **web UI** prefers its browser's `localStorage`; without a local choice it tries the shared setting, then English. Changes attempt to write `~/.vaulty/prefs.json` (0600), but synchronization can fail, for example without a valid UI token.
- The **CLI** localizes the command tree and many help/runtime messages; it need not match a browser's saved language.
- `vaulty-keeper lang` prints the current language; `vaulty-keeper lang zh` or `vaulty-keeper lang en` writes the shared preference (works on a non-TTY too).
- `VAULTY_KEEPER_LANG=en|zh` overrides the file (highest priority).
- Resolution order: `VAULTY_KEEPER_LANG` → `~/.vaulty/prefs.json` → default `en`.

The example assumes no `VAULTY_KEEPER_LANG` override and an initial English preference:

```sh
vaulty-keeper lang            # → language: en
vaulty-keeper lang zh         # write the shared Chinese preference
vaulty-keeper lang            # → 语言：zh
vaulty-keeper help            # help tree is now in Chinese too
```

Some prompts, flag descriptions, shell-completion descriptions and low-level library errors remain English.

## CLI at a glance

The full command reference (apollo snapshot tool, aes encrypt/decrypt, misc) lives in **[\`docs/cli-reference.md\`](docs/cli-reference.md)** ([中文版](docs/cli-reference.zh-CN.md)). Every command self-documents: \`vaulty-keeper <cmd> -h\`.

| Area | Commands |
|---|---|
| Snapshots | \`apollo init\` · \`sensitive init\` · \`apollo import/list/get/set/unset/mark/compare/reveal/edit/export/rm\` |
| AES (Java CryptoUtil compatible) | \`aes list\` · \`aes gen-key\` · \`aes add\` · \`aes encrypt\` · \`aes decrypt\` |
| Web UI | \`ui\` |
| Masking proxy | \`serve\` · \`remote list/get/compare\` |
| DB tunnels | \`db init/add/list/test/connect/show/rm/shell/regen/on/off\` |
| Other | \`completion\` · \`lang\` · \`version\` |

Snapshot reads are masked for non-TTY output unless a key is explicitly marked safe; plaintext exits require a stdin TTY (accident-prevention gate, not human identity). The next section is the database tunnel — the flagship feature; run `vaulty-keeper <cmd> -h` for exact flags.
## Database tunnel proxy (AI queries with tunnel credentials)

> Full ASCII diagrams (what's in Docker / where credentials live / auth injection for three DBs / security boundary / sequence) live in **[`docs/db-proxy-architecture.md`](docs/db-proxy-architecture.md)** ([中文版](docs/db-proxy-architecture.zh-CN.md)).
> Multi-connection / native-client / container / permission examples and their fixture prerequisites live in **[`docs/db-proxy-examples.md`](docs/db-proxy-examples.md)** ([中文版](docs/db-proxy-examples.zh-CN.md)); check each example's evidence and version scope.
> Per-protocol operation guides: **[PostgreSQL](docs/tunnel/postgres-tunnel-guide.md)** · **[MySQL](docs/tunnel/mysql-tunnel-guide.md)** · **[Redis](docs/tunnel/redis-tunnel-guide.md)** ([中文版各一篇](docs/README.zh-CN.md)).
> MongoDB 8's fixed-endpoint command-aware tunnel, exact URL options, security limits and verification matrix: **[MongoDB tunnel guide](docs/tunnel/mongodb-tunnel-guide.md)** ([中文版](docs/tunnel/mongodb-tunnel-guide.zh-CN.md)). The older diagrams/examples below cover PG/MySQL/Redis.

Lets an AI in a container/isolated domain query databases with **native clients** (psql / mysql / redis-cli / mongosh) using tunnel credentials instead of the real backend URL. URLs are encrypted on the host with the independent DB key (`VAULTY_KEEPER_DB_KEY` / OS secret store). `serve` opens one TCP tunnel per connection and authenticates to the backend. PG/MySQL/Redis then forward raw bytes; MongoDB retains a framed command allowlist and reconstructs control replies. Business data is not redacted; see each protocol's security boundary.

```
[Docker container: AI agent]
  psql "postgresql://$PG_TOKEN:x@host.docker.internal:15432/appdb"
  mysql -h host.docker.internal -P 15435 -u "$MYSQL_TOKEN" -px --ssl-mode=DISABLED
  redis-cli -h host.docker.internal -a "$REDIS_TOKEN" -p 15434
        ▼ TCP
[Host: vaulty-keeper serve --addr 0.0.0.0:8970]
  HTTP masking bridge (existing) + one TCP tunnel per connection (validate token → connect real DB with decrypted URL → inject real credentials → forward)
        ▼
  real databases
```

The diagram is illustrative, not fixture setup: each token and port must match a registered connection. PG/MySQL/Redis forward upstream business replies/errors and can expose backend metadata; they are not general response sanitizers.

**Human host walkthrough: synthetic PostgreSQL**

Requires Docker, the current source-built binary on PATH and native `psql` on the host. This writes a demo registration to the host's default DB store; do not reuse an existing connection name. Reserve backend port **25432**, tunnel **15432** and bridge **8970**; automatic allocation does not check OS port availability. The fixed demo container name must be unused; Docker refuses an existing name instead of deleting it. All inline credentials below are synthetic, not a real-secret input pattern.

Host terminal 1:

```sh
vaulty-keeper db init   # first run only; do not force regeneration of an existing key
docker run -d --name vaulty-readme-pg --rm \
  -e POSTGRES_USER=app -e POSTGRES_PASSWORD=synthetic-demo-pass -e POSTGRES_DB=appdb \
  -p 127.0.0.1:25432:5432 postgres:17.6-alpine
docker exec vaulty-readme-pg pg_isready -U app -d appdb
```

Wait until `pg_isready` reports accepting connections, then continue in terminal 1:

```sh
printf '%s\n' 'postgres://app:synthetic-demo-pass@127.0.0.1:25432/appdb?sslmode=disable' \
  | vaulty-keeper db add readme-orders --port 15432
vaulty-keeper db list
vaulty-keeper serve --addr 127.0.0.1:8970   # long-running; wait for listener output
```

Host terminal 2:

```sh
vaulty-keeper db connect readme-orders --cmd
```

Run the printed `psql` command in terminal 2, then enter `SELECT 1;` (expected result `1`) and `\q`. The printed token is an access credential. Optional lifecycle commands, also in terminal 2:

```sh
vaulty-keeper db regen readme-orders   # redistribute the newly generated client command
vaulty-keeper db off readme-orders    # listener stops on the watcher's next sync
vaulty-keeper db on readme-orders
```

To clean up this demo, stop only the `serve` you started with Ctrl-C in terminal 1, then run in terminal 2:

```sh
vaulty-keeper db rm readme-orders --yes
docker stop vaulty-readme-pg   # --rm removes this demo container
```

For real registration a human can run `vaulty-keeper db add <name>` and paste the URL at its stdin prompt; input currently **echoes on the terminal**. Piping a URL keeps it out of vaulty-keeper's argv, but a literal `printf 'URL'` still appears in upstream shell history. Use a trusted input source and avoid recorded terminals; agents must not retrieve real URLs for registration. Use `--all` instead of a connection name for all-entry `regen`/`on`/`off` operations, not `name [--all]` literally.

- Type is auto-detected from the URL scheme: `postgres://`/`postgresql://`, `mysql://`, `redis://`/`rediss://`, `mongodb://`
- **Multiple connections of the same type**: one name and independent tunnel port each, subject to available ports/resources. Assign explicit ports or auto-allocate at `db add`; fetch client commands per connection on the host.
- **Host vs container**: `db connect <name> --container` must run on the host with the local DB store/key. Deliver the generated command/token only to an authorized client. A keyless container uses `remote dblist` (or `db list` fallback) for metadata; it cannot generate dedicated tokens with `db connect`. Never mount host keys to make that work. `--container` only changes the printed host, not listener binding; container access needs a reachable, restricted host interface.
- **Watcher prerequisite**: `serve` starts DB watching only if the DB store exists and its key is available at startup. If you first register a DB after a bridge-only start, restart `serve` and redistribute its new global token. An active watcher syncs additions/removals/on/off every 2 seconds; token changes are read for new connections. Changing an existing port may require listener restart (`off`, wait for closure, then `on`).
- **Tunnels are on by default**; `db off <name>` closes one listener and `db off --all` closes all on the next watcher sync. `db on` re-enables them; list output shows off state and the UI offers per-row Open/Close controls.
- `vaulty-keeper db connect <name>` prints a **ready-to-run token-filled command** (psql/mysql/redis-cli/mongosh); `--container` uses `host.docker.internal`, `--host` selects a host, `--cmd` prints one line. PG/MySQL use token-as-user with placeholder password `x`; Redis uses token-as-password with placeholder user `x`; MongoDB uses **user `vaulty`, dedicated token as SCRAM-SHA-256 password, `authSource=admin`**, plus `directConnection=true&retryWrites=false`. Generated mongosh commands put only the tunnel URI/token in argv, not the backend URI; the token is an access credential intended for the authorized agent, not harmless public data.
- `vaulty-keeper db regen <name>` (or `db regen --all`) rotates dedicated 128-bit tokens. Redistribute generated links afterwards; established sessions remain, and the global token is unaffected. **New and legacy PG/MySQL/Redis connections accept either global or dedicated token**; the CLI's preference for dedicated tokens does not disable global access. MongoDB accepts only its dedicated token.
- Same-name `db add` retains the port if omitted, but creates a fresh token and resets the connection to enabled. Redistribute client links and review exposure afterwards. UI enabled/off is saved configuration, not listener/backend health; `Broken` means registered URL decryption failed, while token decryption can fail separately during resolution.
- **Credential injection**: PG uses trust-style frontend authentication; MySQL replaces the auth response (`mysql_native_password` / `caching_sha2_password`); Redis sends backend `AUTH`; MongoDB independently authenticates using the registered backend SHA-256/SHA-1 credentials/auth source. Tunnel clients never need the real password.
- **Backend TLS**: PG delegates `sslmode` to its client library; Redis uses `rediss://`. **MySQL `?tls=true` negotiates TLS with the backend** (C01 fix: unit-tested; a one-off native TLS query against MySQL 8 with `require_secure_transport=ON` reported non-empty `Ssl_cipher` and TLSv1.3 over the tunnel, but that evidence is not pinned by an integration test — re-verify against a real TLS backend before relying on it); add `tlsCAFile=<path>` to trust a private/self-signed CA. MongoDB implements certificate/hostname verification for `tls=true`/`ssl=true` with optional `tlsCAFile`; actual MongoDB TLS remains unverified. Client-to-proxy transport is plaintext; use localhost or an isolated trusted network.
- **Read-only control**: the proxy does not enforce read-only; registering a URL with a read-only account is naturally read-only
- `vaulty-keeper db shell <name>` opens an installed native client directly on the host (stdin-TTY guarded). Passwords use child environment variables; MySQL/Redis host and MySQL user can still appear in argv. MongoDB passes its backend URI in a temporary child environment variable, removed by the startup script. This is not sanitized tunnel access.
- **MongoDB 8** supports common reads, acknowledged CRUD (server write acknowledgement, not human approval), reviewed read aggregation and cursors at one fixed endpoint. The backend account needs `listCollections` privilege for ordinary collection checks; no views/time-series, transactions, retryable writes, SRV/failover, compression or full admin/GUI compatibility. Common unsupported options/commands include `comment`, `collation`, `create` and `createIndexes`. Use bounded reads such as `db.getCollection('orders').find({}).limit(5)` on an allowed collection. The [guide](docs/tunnel/mongodb-tunnel-guide.md) owns complete password-prompt commands, registered-backend vs client-URI options and troubleshooting; error code 13 alone cannot distinguish proxy policy from backend role denial.

**Security boundary**

This is a summary; the [security model](docs/security-model.md) owns the complete boundary, including UI token exposure and protocol-specific limits.

- Tunnel listen addresses follow `--addr`, default `127.0.0.1`; container access requires a reachable interface. Restrict plaintext listeners to trusted isolated networks. PG/MySQL validate token-as-user and Redis validates AUTH with dedicated/global token support; MongoDB validates its dedicated token as the virtual user's password, with no global fallback. Limited Mongo hello/monitoring is available before client auth, not business commands.
- Close unused listeners with `db off` and restore them with `db on`; state persists. Token rotation governs new connections, and listener shutdown does not promise to terminate established sessions.
- Registration URLs are encrypted at rest. Mongo authentication/control replies and proxy errors/logs omit backend credentials/hosts, but business documents are untouched. Trusted DBA definition changes and upstream Mongo logs are outside the proxy guarantee; direct human `db shell` is not a sanitized tunnel session.
- Dedicated tunnel tokens are 128-bit random and rotatable with `db regen`. Give them only to authorized agents/tools. Global bridge-token fallback applies to PG/MySQL/Redis, not MongoDB; possessing a tunnel token grants database access within backend roles and proxy policy.

## Safe usage guide for AI / scripts

### Security model overview

**Masking defaults and stdin-TTY guards reduce accidental disclosure; they do not authenticate a human or contain a same-user process.** The [security model](docs/security-model.md) is authoritative; the following is an entry-point summary and operator checklist.

| Layer | Mechanism |
|---|---|
| At-rest encryption | Snapshot values and registered DB URLs/tokens are encrypted; metadata, plaintext AES key/IV JSON, input/export/editor files are not covered. Independent new snapshot encryption uses snapshot/sensitive keys; legacy sensitive ciphertext has a snapshot-key fallback. |
| Trust boundary | OS key storage is not a same-user process boundary. Put host keys/ciphertext outside an untrusted agent's permissions; container mounts, privileges and network access must also be reviewed. |
| Masking proxy | Snapshot API values are always masked, including safe values. Bridge list/compare JSON includes length/fingerprints; `remote get` prints only the mask. The global token also grants PG/MySQL/Redis tunnel access. |
| AI reads | Non-TTY local reads expose explicitly safe values only. TTY `get` prints plaintext; explicit plaintext commands check stdin TTY, not caller identity/stdout. Agents must not use those exits on real secrets or fabricate TTYs. |
| AI writes | Writes encrypt stored values but can replace/delete data and change visibility. They require task authorization; import overwrite and sensitive-to-safe marking have additional guards. |
| DB tunnels | Encrypted registered URLs and dedicated tokens; PG/MySQL use token-as-user, Redis token-as-password, MongoDB user `vaulty` + token-as-password with no global fallback. MongoDB keeps command-aware framing and sanitized control replies, not business-data redaction. `db regen` affects new connections; `db on/off` toggles persisted listener state, not guaranteed termination of established sessions. See the [Mongo guide](docs/tunnel/mongodb-tunnel-guide.md) for scope and evidence. |
| Web UI | Loopback-only; non-GET operations require the UI token, explicit plaintext routes also require `--allow-plaintext`. GET can return safe values and usable DB tokens without UI authentication. Failed token checks have capped linear delay, not exponential backoff. |
| Fingerprints | Same-key HMAC-SHA256 over normalized values, truncated to 8 bytes; keyed comparison resists offline guessing without the key but is not proof of byte equality. Lengths are UTF-8 bytes. |
| Consistency checks | Use masked `compare`; use bridge list/compare JSON when fingerprints are needed. Do not retrieve real plaintext merely to compare it. |

Non-TTY support is command-specific; plaintext exits are deliberately refused and writes may need explicit flags. `--json` is not universal (including the no-difference `apollo compare --json` text result). Plaintext on stdout can enter conversation context, session logs and sync systems. Treat token-bearing commands as credentials, too.

**Routine reads and authorized writes (not a blanket grant of permission)**
- `apollo list <env> --appid xx [--json]` — non-TTY unmarked values show `*** (n chars)`
- `apollo compare <a> <b> --appid xx --appid-to yy [--json]` — non-TTY unmarked values masked + length
- `apollo get <env> <key> --appid xx` — non-TTY unmarked values are masked; TTY can print plaintext
- `apollo set/unset/mark`, `init`, `rm --yes` — state-changing operations; review scope, values and overwrite/delete effects first
- `remote list|get|compare` — read via the masking proxy, **masked only, always** (even for keys marked safe)
- `db list` / `remote dblist` — connection name/type/port only, **never the URL**
- `db add` — writes an encrypted URL and fresh token; stdin does not erase upstream history or terminal echo. Same-name registration resets token/enabled state. Real URLs must be supplied by a trusted human, not retrieved by an agent.

**Keys to allowlist for the AI**: mark them safe explicitly first, then the AI can read plaintext (e.g. `APP_NAME`, `LOG_LEVEL` — values you know contain nothing sensitive):
- `apollo set <env> <key> <value> --appid xx --plain` (marks while setting)
- `apollo mark <env> <key> --appid xx --plain` (marks only, value unchanged)

**Mis-mark guard**: with `set --plain` / `mark --plain`, if the key name or value matches the sensitive rules (password/token/secret/JWT/credential-bearing URI), **non-TTY is always refused** and TTY requires a second confirmation — prevents accidentally marking a sensitive key safe and leaking it to the AI.

**Plaintext exits (stdin-TTY guarded; not for agents handling real secrets)**
- `apollo reveal <env> <key> --appid xx` → decrypted plaintext
- `apollo export <env> --appid xx` → everything in plaintext, even with `--copy`
- `apollo edit <env> --appid xx` → plaintext editor file and whole-snapshot replacement
- `apollo list/compare --reveal` → plaintext
- `aes decrypt` → plaintext
- `db show` / `db shell` → real URL or direct backend access; not sanitized tunnel sessions

`--yes` does not bypass the stdin-TTY guard. It does not follow that an AI can never obtain plaintext: TTY presence is not identity, stdout may be redirected, safe values are intentionally visible, and same-user processes can access key material. Agents must not bypass these operational boundaries; humans must account for logs, temporary files, editor backups, exports and clipboard copies.

Other notes:
- **Do not give real encryption keys to agents**: this includes `VAULTY_KEEPER_APOLLO_KEY`, `VAULTY_KEEPER_SENSITIVE_KEY`, `VAULTY_KEEPER_DB_KEY`, `VAULTY_KEEPER_AES_KEY` and `VAULTY_KEEPER_AES_IV`. Do not place them in agent environments, argv or history. Default OS storage and 0600 AES JSON do not isolate same-user processes. Use a separate permission domain where necessary; this is an operating rule, not a promise enforced by key storage.
- CLI `import` asks on a TTY before overwrite; scripts must explicitly pass `--force`. Authorized replacement still discards omitted entries and existing marks, so review it as a full replacement.
- To compare environments, use masked `compare`; equal lengths alone do not establish equal values. Bridge fingerprints give a same-key normalized comparison signal without retrieving plaintext.

## Verification

Test-coverage pointers and the isolated DB fixture scripts (`scripts/dbtest.sh`, `scripts/mongotest.sh`) live in [CONTRIBUTING.md](CONTRIBUTING.md); MongoDB's dated verification matrix lives in the [MongoDB guide](docs/tunnel/mongodb-tunnel-guide.md#verification-status). Run relevant checks against the exact source revision after code changes.
