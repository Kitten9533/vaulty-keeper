# vaulty-keeper

> [中文](README.zh-CN.md) | English

Personal AI toolbox (single Go binary). Snapshot values and registered database URLs/tunnel tokens are encrypted at rest. This does not cover every local file: the AES key/IV list is plaintext JSON, and import sources, exports/downloads and editor temporary files can contain plaintext. `vaulty-keeper ui` serves a loopback-only web UI for snapshots, AES and database connections. OS key storage and optional native clients require platform facilities.

Start here for installation and examples; the [documentation index](docs/README.md) links current guides and historical records. The [security model](docs/security-model.md) is the canonical reference for security boundaries, [SECURITY.md](SECURITY.md) covers reporting, and [CONTRIBUTING.md](CONTRIBUTING.md) covers building and contributing.

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

This README describes the current source workspace. The latest release is **v0.8.0** (2026-09-07): it includes MongoDB 8 tunnel support and bundles both READMEs, LICENSE, AGENTS.md and the `docs/` guides (index, security model and five guides, English + Chinese); historical `docs/superpowers/` records are source-only. The current working tree additionally adds CONTRIBUTING.md/SECURITY.md to future release archives; that packaging change has not been published yet. Older archives such as **0.6.0** do not include `docs/`; for those, browse the matching tag's `docs/` in the [source repository](https://github.com/Kitten9533/vaulty-keeper). Guides in this workspace are not evidence for binaries older than v0.8.0.

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

## vaulty-keeper apollo — Apollo snapshot tool

> A walkthrough of the snapshot implementation (encrypted file layout / dual-key design / sensitive detection / masking & fingerprints / explicit allowlisting) with tested examples lives in **[`docs/apollo-snapshot-guide.md`](docs/apollo-snapshot-guide.md)** ([中文版](docs/apollo-snapshot-guide.zh-CN.md)).

A fallback for when the Apollo Open API is unavailable: copy key-value pairs from the Apollo portal and import them into an encrypted snapshot. AI/script access follows the masking and write permissions below. Snapshots live under `~/.vaulty/apollo/` by default (override with `--dir` or `VAULTY_KEEPER_APOLLO_DIR`).

After human host key initialization above, this complete example creates two snapshots in a new temporary directory using **synthetic values only**. Keep the directory variable in this terminal for all commands:

```sh
DEMO_SNAP_DIR=$(mktemp -d)
printf '%s\n' 'APP_NAME = demo' 'LOG_LEVEL = info' 'SECRET_TOKEN = synthetic-prod' \
  | vaulty-keeper apollo import - --dir "$DEMO_SNAP_DIR" --name prod --appid demo
printf '%s\n' 'APP_NAME = demo' 'LOG_LEVEL = debug' 'SECRET_TOKEN = synthetic-test' \
  | vaulty-keeper apollo import - --dir "$DEMO_SNAP_DIR" --name test --appid demo
vaulty-keeper apollo list prod --dir "$DEMO_SNAP_DIR" --appid demo --json </dev/null
vaulty-keeper apollo compare prod test --dir "$DEMO_SNAP_DIR" --appid demo --appid-to demo --json </dev/null
```

The comparison reports `LOG_LEVEL` and `SECRET_TOKEN` as changed with masked values. This uses host key storage, not an isolated key fixture. The following is a **command reference**, not a script: substitute filenames, names, AppIDs and keys; `<...>`, `[...]` and `a|b` denote placeholders/choices, never literal shell input.

```sh
vaulty-keeper apollo init                          # first run: create snapshot key (OS secret store)
vaulty-keeper sensitive init                       # first run: create sensitive-value key (independent)
vaulty-keeper apollo import prod.txt --appid xx    # parse pasted content; --appid required; --name defaults to file name; existing snapshot needs --force
vaulty-keeper apollo import - --name prod --appid xx   # read from stdin (legacy --app-id still accepted)
vaulty-keeper apollo list                          # list snapshots (env + AppID)
vaulty-keeper apollo list prod --appid xx          # non-TTY: unmarked values masked; --reveal requires stdin TTY
vaulty-keeper apollo list prod --appid xx --json   # JSON output (AI-friendly)
vaulty-keeper apollo get prod --appid xx SOME_KEY  # non-TTY: plaintext only for keys explicitly marked safe, everything else masked
vaulty-keeper apollo set prod --appid xx SOME_KEY value
vaulty-keeper apollo set prod --appid xx SOME_KEY value --plain    # explicitly mark as safe: AI/scripts may read plaintext
vaulty-keeper apollo set prod --appid xx SOME_KEY value --secret   # sensitive classification; not safe for default non-TTY output
vaulty-keeper apollo mark prod --appid xx SOME_KEY --plain|--secret  # flip the safe/sensitive mark without changing the value
vaulty-keeper apollo unset prod --appid xx SOME_KEY
vaulty-keeper apollo compare prod test --appid xx --appid-to yy   # added/removed/changed; masking rules below
vaulty-keeper apollo compare prod test --appid xx --appid-to yy --json
vaulty-keeper apollo reveal prod --appid xx SECRET_TOKEN          # show sensitive plaintext (TTY only)
vaulty-keeper apollo reveal prod --appid xx app.fs.oss.secret-key --key <aes> --iv <aes>   # decrypt external AES ciphertext (TTY only)
vaulty-keeper apollo edit prod --appid xx         # $EDITOR plaintext edit, re-encrypted on save (TTY only)
vaulty-keeper apollo export prod --appid xx       # decrypt everything for pasting back into Apollo (TTY only)
vaulty-keeper apollo export prod --appid xx --copy # prints first, then copies using macOS pbcopy (TTY only)
vaulty-keeper apollo rm prod --appid xx           # delete snapshot (TTY confirms; non-TTY needs --yes)
```

> Plaintext commands (`reveal`/`export`/`edit`/`list|compare --reveal`/`aes decrypt`) require **stdin to be a TTY**; `--yes` does not bypass that check. This is an accident-prevention gate, not human authentication or a check on stdout. TTY `get` can print plaintext directly. Agents must not invoke real-secret plaintext exits or fabricate a TTY.
>
> **Reversed default**: non-TTY `get`/`list`/`compare` mask values unless explicitly safe (`set --plain` or `mark --plain`). A safe value is authorized plaintext output, not merely a non-secret classification. Ordinary TTY list/compare can also show non-sensitive values.

CLI `compare --json` currently prints a text message, not JSON, when there are no changes. For bridge fingerprints use `remote list <env> --appid <id> --json`; `remote get` prints only the masked value string. Lengths use UTF-8 **bytes** despite the CLI label `chars`. Fingerprints use the same snapshot HMAC key over normalized values, truncated to 8 bytes: a high-confidence comparison signal, not proof of raw-byte identity. CLI and HTTP JSON shapes differ.

CLI import replaces an existing snapshot only after TTY confirmation or explicit non-TTY `--force`; UI import rejects duplicate names/AppIDs with 409. Import replacement and whole-snapshot edit rebuild entries, reclassify values and do not preserve all safe/secret marks. Omitted or unparseable entries can disappear; review parser warnings (not all edit paths expose them), the resulting keys and classifications after saving. CLI editing also creates a plaintext temporary file, and editors may keep backups.

Snapshots are keyed by "env + AppID", stored as `{env}__{appid}.json`; legacy AppID-less `{env}.json` files are still readable (accessed when `--appid` is omitted).

Parsing rules:

- Each line is `KEY = value`, split at the first `=`, both sides trimmed (values may contain `=`).
- Blank lines and whole lines starting with `#` (single/multi-line comments) are skipped.
- Multiple `KEY = ` entries glued onto one line starting with an uppercase letter are split automatically with a warning (e.g. `A = 1B = 2`; only all-uppercase keys are recognized).
- Keys are validated against `[A-Za-z_][A-Za-z0-9_.-]*`; invalid lines are skipped with a warning.

Two snapshot keys (normally in the OS secret store; nonempty environment overrides take precedence):

- **Snapshot key** (`VAULTY_KEEPER_APOLLO_KEY`, created by `apollo init`): encrypts all non-sensitive values.
- **Sensitive-value key** (`VAULTY_KEEPER_SENSITIVE_KEY`, created by `sensitive init`): encrypts newly written sensitive values independently of the snapshot key. Legacy sensitive ciphertext can still use a snapshot-key decryption fallback; the separation claim applies to independently encrypted new data. Files are 0600; encrypted values use AES-256-GCM with an independent random nonce per item.

**Linux**: initialization requires a working desktop Secret Service (for example gnome-keyring / kwallet). On headless hosts, a trusted operator can supply `VAULTY_KEEPER_APOLLO_KEY`, `VAULTY_KEEPER_SENSITIVE_KEY` and `VAULTY_KEEPER_DB_KEY` through controlled secret injection. Each must decode from Base64 to exactly 32 bytes. Overrides apply even when keyring is available; an invalid/wrong override does not retry keyring. Check the configured source before regenerating keys, which can make existing data unreadable. `openssl rand -base64 32` prints a new secret; do not run it in an agent/logged session for real keys. Putting keys in shell profiles creates plaintext files, even at 0600, and is not an encrypted-storage solution.

Sensitive classification (used on import/new set and persisted; reads do not rewrite it; existing `set` without a flag preserves classification):

- **Key name match**: `password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key` (case-insensitive)
- **Credential-bearing URI/DSN**: key name contains `uri|url|dsn|connection|endpoint|addr|address` and the value looks like `scheme://user[:password]@host` (e.g. `mongodb://root:pw@...`)
- **JWT**: value looks like a three-part base64url `eyJ...` (e.g. `SUPABASE_SERVICE_ROLE_KEY`, `NEXT_PUBLIC_SUPABASE_ANON_KEY`)

`MONGODB_URI` is not a direct secret-name match: the credential-bearing value makes that example sensitive. Classification selects encryption behavior; explicit **safe** authorization controls ordinary non-TTY/UI plaintext output independently. Do not use `--plain` to expose a real secret.

## vaulty-keeper aes — AES encrypt/decrypt (Java CryptoUtil compatible)

For decrypting Apollo values whose **value itself is CryptoUtil ciphertext** (OSS AK/SK and similar). Algorithm aligned with `CryptoUtil.java`: AES/GCM/NoPadding, 128-bit tag, key is UTF-8 bytes (16/24/32), iv is UTF-8 bytes used directly as the GCM IV, ciphertext is Base64.

key/iv live in a **plaintext named list** at `~/.vaulty/aes.json` (0600), format `[{name, secret-key, iv}, ...]` (legacy single-object `{key, iv}` is read as a `default` entry). The CLI references entries with `--name`; the web UI's AES tool and snapshot "view" decryption take **manually entered key/iv** (they do not read the list). Snapshot storage encryption and external CryptoUtil value encryption are separate layers. The UI's external-AES fields appear after reveal fails, not as an always-open advanced option.

For **new encryption, never reuse a key/IV pair for different messages**. Named entries retain their IV; Java compatibility does not make repeated use safe. Decryption needs the original pair. The reference below is syntax, not a repeatable real-secret workflow: literal keys/inline environment assignments can enter shell history and process inspection; stdin does not erase the upstream shell command. `aes gen-key` prints generated key/IV even without a TTY.

```sh
# list / generate / add entries (reference alternatives, not a sequence)
vaulty-keeper aes list
vaulty-keeper aes gen-key --name oss              # generates, prints secrets and saves plaintext aes.json
vaulty-keeper aes add --name oss --key <k> --iv <i>   # save an entry manually

# encrypt/decrypt with a list entry (decrypt prints plaintext, TTY only)
vaulty-keeper aes encrypt --name oss 'hello'
vaulty-keeper aes decrypt --name oss '<base64>'

# syntax only: manual flags / inline env can expose real secrets
vaulty-keeper aes encrypt --key <k> --iv <i> 'hello'
VAULTY_KEEPER_AES_KEY=<k> VAULTY_KEEPER_AES_IV=<i> vaulty-keeper aes decrypt '<base64>'

# decrypt an external AES ciphertext value (TTY only)
vaulty-keeper apollo reveal prod app.fs.oss.secret-key --appid xx --key <k> --iv <i>
```

Input can come from `--file`, an argument, or stdin. `decrypt` prints plaintext and requires stdin TTY, so piping ciphertext does not bypass its guard; use a file/argument in a human terminal when appropriate. Input files and outputs have their own plaintext lifecycle.

## Misc

Command-reference notation below uses alternatives/placeholders, not copyable shell pipelines:

```sh
vaulty-keeper ui                              # start local web UI (default 127.0.0.1:8080, auto-increments if busy)
vaulty-keeper serve --addr 0.0.0.0:8970       # masking proxy (for containers/isolated domains when the host holds keys)
vaulty-keeper remote list|get|compare ...     # read through the masking proxy (same shape as apollo subcommands)
vaulty-keeper db <init|add|list|test|connect|show|rm|shell|regen|on|off> ... # encrypted DB connections + tunnels
vaulty-keeper completion zsh | source /dev/stdin   # or bash / fish; add to your shell config
vaulty-keeper lang [en|zh]    # show or set the shared UI/CLI language
vaulty-keeper version
```

## Container isolation (against deliberately hostile AI, macOS / Windows)

Masking and TTY guards do not constrain a hostile same-user process. Isolation must put keys and ciphertext outside the agent's reach; the provided Docker configuration is one starting point (Docker Desktop uses a Linux VM). It does not enforce network egress restrictions or prevent access to authorized database contents. See the [security model](docs/security-model.md).

```
[Docker container: codex / claude / opencode / pi]
      │  vaulty-keeper remote list|get|compare (masked only)
      ▼
[Host: holds the keys]
      vaulty-keeper serve --addr 0.0.0.0:8970   ← snapshot API masks values; DB tunnels return data
      ▼
      OS secret store + ~/.vaulty/ (not mounted by the supplied compose file)
```

### Host side: start the masking proxy

Human host terminal 1: leave `serve` running. Use a trusted, firewall-restricted interface; the following binds all interfaces. Register databases before starting it if tunnels are needed.

```sh
vaulty-keeper serve --addr 0.0.0.0:8970     # prints token and writes ~/.vaulty/bridge-token
```

- Snapshot API values are always masked, **even for keys marked safe with `set --plain`**. JSON list/compare responses include length/fingerprint metadata; `remote get` prints only the mask.
- Every `/api` endpoint requires the token (written 0600 to `~/.vaulty/bridge-token`); failed checks add 50 ms per failure, capped at 2 seconds. This token also authorizes new and legacy PG/MySQL/Redis tunnel connections, so it is not a harmless metadata token.
- `0.0.0.0` lets Docker reach the host but also exposes plaintext HTTP and tunnel listeners to reachable networks. Token checks do not encrypt transport or make LAN exposure safe; restrict access with network controls. Use `127.0.0.1` for host-only access.

### Container side: agent isolation domain

Human host terminal 2, from the repository: choose a project directory containing no secret files. The token handoff below is an authorization decision; the entrypoint prints only a `<set>`/`<unset>` marker, never the token itself. The image builds Go inside Docker and includes neither optional agent CLIs nor database clients by default.

```sh
# build from source inside Docker; no host make build needed
docker build -t vaulty-keeper-agent:local .

# human host handoff: grants snapshot metadata and PG/MySQL/Redis database access
export VAULTY_KEEPER_BRIDGE_TOKEN="$(cat ~/.vaulty/bridge-token)"
export VAULTY_KEEPER_PROJECT_DIR="$PWD"   # current repository; review contents before mounting
docker compose up -d

# usable without installing an agent CLI; lists the host bridge's snapshots
docker compose exec agent vaulty-keeper remote list
docker compose exec agent vaulty-keeper remote dblist
```

To use `codex`, set `VAULTY_KEEPER_INSTALL_AGENTS='@openai/codex'` before creating the container and supply the CLI's own login/config separately, then run `docker compose exec agent codex`. Native DB commands additionally require `psql`, MySQL `mysql`, `redis-cli` or `mongosh` in the client environment. Generate tunnel commands on the host with `db connect <name> --container`, then deliver only the authorized tunnel credentials, never host encryption keys.

Isolation essentials (already built into `docker-compose.yml`):

- **No explicit mounts** of `~/.vaulty`, the OS secret store, `~/.ssh`, or the Docker socket. Do not defeat this by choosing a project directory containing those files or passing encryption keys via environment variables.
- Non-root user + `cap_drop: ALL` + `no-new-privileges`
- `VAULTY_KEEPER_BRIDGE_ADDR` / `VAULTY_KEEPER_BRIDGE_TOKEN` configure bridge access; compose does **not** make it the container's only network destination.
- Agent CLIs: `VAULTY_KEEPER_INSTALL_AGENTS='@openai/codex @anthropic-ai/claude-code opencode-ai'` (npm-installed into the user dir on container start)
- **Persistence**: the `agent-home` named volume mounts at `/home/agent`, so installed CLIs and session history survive rebuilds. Its actual name depends on the Compose project; remove only that verified volume after detaching its containers if deliberately resetting history and installed tools.
- **Linux**: compose includes `extra_hosts: host.docker.internal:host-gateway` (macOS/Windows Docker Desktop already provides it, no effect)

### What isolation does and does not do

When mounts, privileges and credentials are kept within these boundaries, the container has no direct path to host key storage or snapshot files. That does not stop it reading mounted project secrets, using its bridge token on PG/MySQL/Redis tunnels, querying business data or sending reachable data over the network. These are separate permissions, not encryption failures.

**Docker itself is not absolute isolation**: reduced capabilities and `no-new-privileges` reduce attack surface, but daemon privileges and container escape remain concerns. Stronger threats require separately evaluated account/VM/sandbox and network controls; no configuration here establishes a measured prevention rate.

### Windows users

- Same compose/image; Windows Docker Desktop is WSL2 underneath, `host.docker.internal` works the same
- Keys live in **Windows Credential Manager** (`vaulty-keeper apollo init` / `sensitive init` adapt automatically; no `security` command needed)
- Plaintext CLI guards check stdin console status (`GetConsoleMode` on Windows), not human identity; there is no interactive menu. Non-TTY local reads mask unmarked values, while bridge snapshot reads always mask values.

### Alternatives to Docker

`serve` + `remote` are not tied to Docker; the isolation domain can be any environment that **cannot touch keys or ciphertext**:

**① Run locally (no isolation, defends against "well-behaved" AI)**

In terminal 1, run `vaulty-keeper serve --addr 127.0.0.1:8970` and leave it running. In terminal 2 on the same host, run:

```sh
export VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8970
vaulty-keeper remote list   # uses the host's token file unless an env override is set
```

When the AI shares your account, defense rests on masking + TTY gating; no protection against an AI that actively reads keys.

**② Separate macOS account (real isolation, Docker alternative)**

Create a standard, non-admin `ai` account using macOS account settings and review filesystem access; do not put its real password in a shell command. With `codex` separately installed, a human host can explicitly delegate the bridge token:

```sh
sudo -u ai env VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8970 \
  VAULTY_KEEPER_BRIDGE_TOKEN="$(cat ~/.vaulty/bridge-token)" codex
```

The separate account should have no host keys and no read access to the host's 0700 `~/.vaulty/`. Verify permissions and other shared files; the delegated token still grants PG/MySQL/Redis access. You must manage account credentials, agent installation and filesystem permissions.

**③ Remote machine / WSL2**

Put the agent on a separately controlled machine/VM and restrict bridge/tunnel reachability to a trusted network. WSL2 alone is not a guarantee of separation from Windows host files. Token-gated snapshot masking does not protect plaintext transport or redact database results.

## Database tunnel proxy (AI queries with tunnel credentials)

> Full ASCII diagrams (what's in Docker / where credentials live / auth injection for three DBs / security boundary / sequence) live in **[`docs/db-proxy-architecture.md`](docs/db-proxy-architecture.md)** ([中文版](docs/db-proxy-architecture.zh-CN.md)).
> Multi-connection / native-client / container / permission examples and their fixture prerequisites live in **[`docs/db-proxy-examples.md`](docs/db-proxy-examples.md)** ([中文版](docs/db-proxy-examples.zh-CN.md)); check each example's evidence and version scope.
> MongoDB 8's fixed-endpoint command-aware tunnel, exact URL options, security limits and verification matrix: **[MongoDB tunnel guide](docs/mongodb-tunnel-guide.md)** ([中文版](docs/mongodb-tunnel-guide.zh-CN.md)). The older diagrams/examples below cover PG/MySQL/Redis.

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
- **MongoDB 8** supports common reads, acknowledged CRUD (server write acknowledgement, not human approval), reviewed read aggregation and cursors at one fixed endpoint. The backend account needs `listCollections` privilege for ordinary collection checks; no views/time-series, transactions, retryable writes, SRV/failover, compression or full admin/GUI compatibility. Common unsupported options/commands include `comment`, `collation`, `create` and `createIndexes`. Use bounded reads such as `db.getCollection('orders').find({}).limit(5)` on an allowed collection. The [guide](docs/mongodb-tunnel-guide.md) owns complete password-prompt commands, registered-backend vs client-URI options and troubleshooting; error code 13 alone cannot distinguish proxy policy from backend role denial.

**Security boundary**

This is a summary; the [security model](docs/security-model.md) owns the complete boundary, including UI token exposure and protocol-specific limits.

- Tunnel listen addresses follow `--addr`, default `127.0.0.1`; container access requires a reachable interface. Restrict plaintext listeners to trusted isolated networks. PG/MySQL validate token-as-user and Redis validates AUTH with dedicated/global token support; MongoDB validates its dedicated token as the virtual user's password, with no global fallback. Limited Mongo hello/monitoring is available before client auth, not business commands.
- Close unused listeners with `db off` and restore them with `db on`; state persists. Token rotation governs new connections, and listener shutdown does not promise to terminate established sessions.
- Registration URLs are encrypted at rest. Mongo authentication/control replies and proxy errors/logs omit backend credentials/hosts, but business documents are untouched. Trusted DBA definition changes and upstream Mongo logs are outside the proxy guarantee; direct human `db shell` is not a sanitized tunnel session.
- Dedicated tunnel tokens are 128-bit random and rotatable with `db regen`. Give them only to authorized agents/tools. Global bridge-token fallback applies to PG/MySQL/Redis, not MongoDB; possessing a tunnel token grants database access within backend roles and proxy policy.

### Verification fixtures

MongoDB fixture entry points are `bash scripts/mongotest.sh --mongosh` and `bash scripts/mongotest.sh --replica-set --mongosh`, using synthetic credentials and explicit temporary storage/test keys. The [verification matrix](docs/mongodb-tunnel-guide.md#verification-status) owns the dated MongoDB 8.0.13 standalone/fixed-replica-set and test/race/vet/build evidence from the 2026-09-07 implementation workspace; those historical results were not rerun for this documentation update and do not by themselves certify a release binary (MongoDB was shipped in v0.8.0). Actual MongoDB TLS, manual interactive `db shell` and final independent re-review remain unverified.

**`scripts/dbtest.sh` is isolated and safe to run (C02 done).** The current script tracks its own serve PID and containers by label, uses a per-run temp dir and a fake HOME with synthetic keys, and `--clean` tears down only the resources it registered — it no longer broadly pkills serve processes, deletes fixed containers (`aipg`, `aimysql8`, `aimariadb`, `airedis`), or overwrites the real `~/.vaulty/bridge-token` as the historical version did. Read its header before use; keep it out of CI.

Historical script interface, **not a quick-start recommendation**:

```sh
make build
./scripts/dbtest.sh          # start and test; environment stays up, prints connection info
./scripts/dbtest.sh --clean  # teardown: stop serve, remove containers
```

Its actual fixtures are PostgreSQL `postgres:17.6-alpine`, MySQL `dockerproxy.net/library/mysql:8.0` (the recorded local image was 8.0.46, not 8.4/MariaDB) and Redis `redis:7`. It requires Docker, Python 3 and a built binary. Backend, tunnel and bridge ports are allocated dynamically per run (overridable via `PGP`/`TUN_PG`/... environment variables), so inspect the printed connection info; the prepared data and queries are:

| Registration | Prepared data / query |
|---|---|
| `pgdb` | `appdb.t`, `SELECT id,name FROM t ORDER BY id;` |
| `mysqltest` / `mysqlnative` | `shop.customers`, `products`, `orders`; `SELECT COUNT(*) FROM shop.orders;` |
| `cache` | Authenticated Redis; `PING`, synthetic `SET`/`GET` |

The script uses a separate DB directory/key, not the host-default `db shell` context. Its commands/log output are fixture-specific historical examples, not proof that every client/configuration works. See the [DB examples guide](docs/db-proxy-examples.md) for native-client setup and positive/negative queries; review logs before sharing because upstream metadata and access tokens may be present.

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
| DB tunnels | Encrypted registered URLs and dedicated tokens; PG/MySQL use token-as-user, Redis token-as-password, MongoDB user `vaulty` + token-as-password with no global fallback. MongoDB keeps command-aware framing and sanitized control replies, not business-data redaction. `db regen` affects new connections; `db on/off` toggles persisted listener state, not guaranteed termination of established sessions. See the [Mongo guide](docs/mongodb-tunnel-guide.md) for scope and evidence. |
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

These are coverage pointers, not evidence of a run during this documentation update. Run relevant checks against the exact source revision after code changes; MongoDB's dated evidence and remaining gaps live in its guide.

- `internal/aesx`: byte-for-byte aligned with vectors from `tools/javaref/CryptoUtil.java` (Java 8 reference implementation; GCM is deterministic), plus key-length validation, wrong key/iv, invalid base64.
- `internal/apollo`: real pasted samples (incl. glued lines), comments, first `=`, URL params not split, encrypted snapshot on disk (no plaintext in file, 0600), diffs, sensitive detection.
- `internal/cli`: mixed argument order, import auto-naming, reveal (sensitive plaintext / explicit `--key`/`--iv` external ciphertext / multi-key JSON), edit (fake editor script), list/compare JSON, gen-key usability, aes `--name` list, completion.
- Regenerate Java vectors: `cd tools/javaref && javac CryptoUtil.java && java CryptoUtil encrypt <key> <iv> <plaintext>`
