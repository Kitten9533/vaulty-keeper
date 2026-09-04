# vaulty-keeper

> [中文](README.zh-CN.md) | English

Personal AI toolbox (single Go binary, no runtime dependencies). All values are stored encrypted on local disk; keys never live in a config center or a plaintext file. `vaulty-keeper ui` serves a local web UI covering all snapshot and AES features (loopback-only).

## Quick start

**Option 1: Download a prebuilt binary** (recommended, no Go required) — grab the archive for your platform (darwin/linux/windows × amd64/arm64) from [Releases](https://github.com/Kitten9533/vaulty-keeper/releases), extract, and put `vaulty-keeper` on your PATH:

```sh
vaulty-keeper apollo init      # first run: create snapshot key (macOS Keychain / Windows Credential Manager / Linux Secret Service)
vaulty-keeper sensitive init   # first run: create sensitive-value key
vaulty-keeper ui               # open the local web UI
```

**Option 2: Build from source** (requires Go 1.26+):

```sh
git clone https://github.com/Kitten9533/vaulty-keeper.git
make build          # → bin/vaulty-keeper
make install        # symlink to ~/.local/bin/vaulty-keeper
make test           # unit tests (incl. Java↔Go interop vectors)
make release        # cross-compile release packages for all platforms into release/
```

## Manual operation

Running `vaulty-keeper` with no arguments prints the full command tree and performs first-run initialization automatically: creates the data directories (`~/.vaulty/`, `~/.vaulty/apollo/`, 0700), seeds a `default` AES key/iv entry (`~/.vaulty/aes.json`, 0600), and checks whether the three encryption keys (snapshot / sensitive / database) are initialized — if any is missing it offers to initialize it on your TTY, or prints a hint when not on a TTY. For manual CRUD, the recommended entry point is `vaulty-keeper ui` (local web UI covering all snapshot and AES features), or the subcommands below.

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
- A random access token is generated at startup; the URL looks like `http://127.0.0.1:8080/?t=<token>`. Every write operation (import/CRUD/export/decrypt/plaintext edit) requires this token, so other local processes (e.g. an AI agent) cannot export plaintext via curl. **Open the full URL printed at startup**; hitting `localhost:8080` bare works for reads but writes are rejected.
- **Plaintext endpoints are disabled by default** (`reveal`/`export`/plaintext edit/AES decrypt return 403 unless enabled, even with the token); restart with `--allow-plaintext` to enable. So even a leaked token cannot yield plaintext in the default configuration.
- Startup prints a warning: do **not** send the token-bearing URL to AI/scripts, logs, or shell history.
- Covers all features: snapshot browse/search/CRUD, import, env comparison, plaintext edit, export (download or copy), AES encrypt/decrypt (manual key/iv), snapshot and sensitive key initialization, **database tunnels** (register/test connections, generate client commands, rotate tunnel tokens, view the real URL with `--allow-plaintext`).
- Plaintext output (view / plaintext edit / export / AES decrypt) requires a second confirmation and responses carry `Cache-Control: no-store`; the browser never persists plaintext.
- Snapshot contents are never persisted in the browser.
- The UI defaults to **English** and can be switched to Chinese (中文) from the selector in the top bar. The choice is remembered in `localStorage` and mirrored to the shared preference file, so the CLI follows it too (see [Language](#language-ui--cli)).

## Language (UI & CLI)

The whole tool is bilingual (English / 中文) and the UI and CLI share one language setting.

- The **web UI** defaults to English; the top-bar selector switches to 中文 and back. The choice is remembered per browser (`localStorage`) and pushed to the shared preference file `~/.vaulty/prefs.json` (0600). A first visit in a fresh browser adopts the shared setting (e.g. set by the CLI).
- The **CLI** prints the same language as the UI: command tree, `-h` output, usage paragraphs, runtime messages and prompts are all localized.
- `vaulty-keeper lang` prints the current language; `vaulty-keeper lang zh|en` writes the shared preference (works on a non-TTY too).
- `VAULTY_KEEPER_LANG=en|zh` overrides the file (highest priority).
- Resolution order: `VAULTY_KEEPER_LANG` → `~/.vaulty/prefs.json` → default `en`.

```sh
vaulty-keeper lang            # → language: en
vaulty-keeper lang zh         # switch to Chinese, shared with the web UI
vaulty-keeper lang            # → 语言：zh
vaulty-keeper help            # help tree is now in Chinese too
```

Shell-completion descriptions and low-level library errors stay English; on a Chinese terminal you see Chinese guidance with English error details (same as the Chinese UI does).

## vaulty-keeper apollo — Apollo snapshot tool

> A walkthrough of the snapshot implementation (encrypted file layout / dual-key design / sensitive detection / masking & fingerprints / explicit allowlisting) with tested examples lives in **[`docs/apollo-snapshot-guide.md`](docs/apollo-snapshot-guide.md)** (Chinese).

A fallback for when the Apollo Open API is unavailable: copy key-value pairs from the Apollo portal, import them into an encrypted snapshot, and let AI/scripts read, compare and modify them safely. Snapshots live in `~/.vaulty/apollo/<name>.json` by default (override with `--dir` or `VAULTY_KEEPER_APOLLO_DIR`).

```sh
vaulty-keeper apollo init                          # first run: create snapshot key (OS secret store)
vaulty-keeper sensitive init                       # first run: create sensitive-value key (independent)
vaulty-keeper apollo import prod.txt --appid xx    # parse pasted content; --appid required; --name defaults to file name; existing snapshot needs --force
vaulty-keeper apollo import - --name prod --appid xx   # read from stdin (legacy --app-id still accepted)
vaulty-keeper apollo list                          # list snapshots (env + AppID)
vaulty-keeper apollo list prod --appid xx          # default: all masked *** (length); --reveal shows plaintext (TTY only)
vaulty-keeper apollo list prod --appid xx --json   # JSON output (AI-friendly)
vaulty-keeper apollo get prod --appid xx SOME_KEY  # non-TTY: plaintext only for keys explicitly marked safe, everything else masked
vaulty-keeper apollo set prod --appid xx SOME_KEY value
vaulty-keeper apollo set prod --appid xx SOME_KEY value --plain    # explicitly mark as safe: AI/scripts may read plaintext
vaulty-keeper apollo set prod --appid xx SOME_KEY value --secret   # explicitly mark as sensitive: always masked
vaulty-keeper apollo mark prod --appid xx SOME_KEY --plain|--secret  # flip the safe/sensitive mark without changing the value
vaulty-keeper apollo unset prod --appid xx SOME_KEY
vaulty-keeper apollo compare prod test --appid xx --appid-to yy   # added/removed/changed, all masked by default
vaulty-keeper apollo compare prod test --json
vaulty-keeper apollo reveal prod --appid xx SECRET_TOKEN          # show sensitive plaintext (TTY only)
vaulty-keeper apollo reveal prod --appid xx app.fs.oss.secret-key --key <aes> --iv <aes>   # decrypt external AES ciphertext (TTY only)
vaulty-keeper apollo edit prod --appid xx         # $EDITOR plaintext edit, re-encrypted on save (TTY only)
vaulty-keeper apollo export prod --appid xx       # decrypt everything for pasting back into Apollo (TTY only)
vaulty-keeper apollo export prod --appid xx --copy # copy to clipboard (pbcopy) (TTY only)
vaulty-keeper apollo rm prod --appid xx           # delete snapshot (TTY confirms; non-TTY needs --yes)
```

> Plaintext commands (`reveal`/`export`/`edit`/`list|compare --reveal`/`aes decrypt`) **only work in an interactive terminal**; AI/script environments are always refused, `--yes` cannot override.
>
> **Reversed default**: `get`/`list`/`compare` mask **everything** in AI/script (non-TTY) environments by default — no guessing from key names — unless a key is explicitly marked safe (`set --plain` or `mark --plain`). Even with many unknown key names, an AI gets no plaintext; only the few keys you know are safe get allowlisted individually.

Snapshots are keyed by "env + AppID", stored as `{env}__{appid}.json`; legacy AppID-less `{env}.json` files are still readable (accessed when `--appid` is omitted).

Parsing rules:

- Each line is `KEY = value`, split at the first `=`, both sides trimmed (values may contain `=`).
- Blank lines and whole lines starting with `#` (single/multi-line comments) are skipped.
- Multiple `KEY = ` entries glued onto one line are split automatically with a warning (e.g. `A = 1B = 2`).
- Keys are validated against `[A-Za-z_][A-Za-z0-9_.-]*`; invalid lines are skipped with a warning.

Two keys (both in the OS secret store, both overridable by env, neither in a plaintext file):

- **Snapshot key** (`VAULTY_KEEPER_APOLLO_KEY`, created by `apollo init`): encrypts all non-sensitive values.
- **Sensitive-value key** (`VAULTY_KEEPER_SENSITIVE_KEY`, created by `sensitive init`): encrypts all sensitive values (password/token/secret/...). Only this key can decrypt them; `reveal`/`--reveal` relies on it, and an AI process without it cannot read sensitive plaintext. Files are 0600, values use AES-256-GCM with an independent random nonce per item.

**Linux**: on a desktop session (gnome-keyring / kwallet) `apollo init` / `sensitive init` / `db init` work out of the box (Secret Service); headless servers without a Secret Service use the environment-variable fallback — generate 32-byte base64 keys on any machine, then write them into the headless server's shell config (file 0600):

```sh
# generate keys (any machine, once; keep them out of terminal logs/clipboard)
openssl rand -base64 32    # → snapshot key
openssl rand -base64 32    # → sensitive-value key

# headless server ~/.profile (0600) — keys are red-line secrets, never put them in AI sessions or on the command line
export VAULTY_KEEPER_APOLLO_KEY='<base64>'
export VAULTY_KEEPER_SENSITIVE_KEY='<base64>'
```

Sensitive detection (masked by default, `--reveal` to show; `--reveal` is TTY-only):

- **Key name match**: `password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key` (case-insensitive)
- **Credential-bearing URI/DSN**: key name contains `uri|url|dsn|connection|endpoint|addr|address` and the value looks like `scheme://user[:password]@host` (e.g. `mongodb://root:pw@...`)
- **JWT**: value looks like a three-part base64url `eyJ...` (e.g. `SUPABASE_SERVICE_ROLE_KEY`, `NEXT_PUBLIC_SUPABASE_ANON_KEY`)

Better to over-mask than under-mask; `--reveal` on a TTY is the escape hatch.

## vaulty-keeper aes — AES encrypt/decrypt (Java CryptoUtil compatible)

For decrypting Apollo values whose **value itself is CryptoUtil ciphertext** (OSS AK/SK and similar). Algorithm aligned with `CryptoUtil.java`: AES/GCM/NoPadding, 128-bit tag, key is UTF-8 bytes (16/24/32), iv is UTF-8 bytes used directly as the GCM IV, ciphertext is Base64.

key/iv live in a **named list** at `~/.vaulty/aes.json` (0600), format `[{name, secret-key, iv}, ...]` (legacy single-object `{key, iv}` auto-migrates to a `default` entry). The CLI references entries with `--name`; the web UI's AES tool and snapshot "view" decryption take **manually entered key/iv** (they do not read the list).

```sh
# list / add / remove entries
vaulty-keeper aes list
vaulty-keeper aes gen-key --name oss              # generate and save to aes.json
vaulty-keeper aes add --name oss --key <k> --iv <i>   # save an entry manually

# encrypt/decrypt with a list entry (decrypt prints plaintext, TTY only)
vaulty-keeper aes encrypt --name oss 'hello'
vaulty-keeper aes decrypt --name oss '<base64>'

# or specify manually / via env (keeps keys out of shell history)
vaulty-keeper aes encrypt --key <k> --iv <i> 'hello'
VAULTY_KEEPER_AES_KEY=<k> VAULTY_KEEPER_AES_IV=<i> vaulty-keeper aes decrypt '<base64>'

# decrypt an external AES ciphertext value (TTY only)
vaulty-keeper apollo reveal prod app.fs.oss.secret-key --key <k> --iv <i>
```

Input can come from `--file`, an argument, or stdin. `decrypt` prints plaintext and **only works in an interactive terminal** (script/AI environments are always refused).

## Misc

```sh
vaulty-keeper ui                              # start local web UI (default 127.0.0.1:8080, auto-increments if busy)
vaulty-keeper serve --addr 0.0.0.0:8970       # masking proxy (for containers/isolated domains when the host holds keys)
vaulty-keeper remote list|get|compare ...     # read through the masking proxy (same shape as apollo subcommands)
vaulty-keeper db <init|add|list|test|connect|show|rm|shell|regen> ... # encrypted DB connections + tunnels (see "Database tunnel proxy")
vaulty-keeper completion zsh | source /dev/stdin   # or bash / fish; add to your shell config
vaulty-keeper lang [en|zh]    # show or set the shared UI/CLI language
vaulty-keeper version
```

## Container isolation (against deliberately hostile AI, macOS / Windows)

The default security model defends against "well-behaved" AI. Against an AI that **deliberately ignores docs and actively grabs keys**, the only reliable defense is putting it in an isolated domain that **cannot touch keys or ciphertext**. Docker unifies this (macOS/Windows Docker are Linux VMs):

```
[Docker container: codex / claude / opencode / pi]
      │  vaulty-keeper remote list|get|compare (masked only)
      ▼
[Host: holds the keys]
      vaulty-keeper serve --addr 0.0.0.0:8970   ← masking proxy, never returns plaintext
      ▼
      OS secret store + ~/.vaulty/ (invisible to the container)
```

### Host side: start the masking proxy

```sh
vaulty-keeper serve --addr 0.0.0.0:8970     # prints token and writes ~/.vaulty/bridge-token
```

- Output is masked only: `*** (n chars)` + length + fingerprint, **even for keys marked safe with `set --plain`**
- Every `/api` endpoint requires the token (written 0600 to `~/.vaulty/bridge-token`); failures are rate-limited (exponential backoff)
- `0.0.0.0` is so the Docker VM can reach it via `host.docker.internal`; token gating + masked-only output make LAN exposure acceptable (bind `127.0.0.1` for host-only use, but containers will not be able to connect)

### Container side: agent isolation domain

```sh
# build the image (host runs make build first; the binary is copied into the image)
docker build -t vaulty-keeper-agent:local .

# start (token read from host; masked-only)
export VAULTY_KEEPER_BRIDGE_TOKEN="$(cat ~/.vaulty/bridge-token)"
export VAULTY_KEEPER_PROJECT_DIR=/path/to/your/project   # mount only the project dir
docker compose up -d

# run an agent inside; read config with vaulty-keeper remote (same shapes as local)
docker compose exec agent codex
docker compose exec agent vaulty-keeper remote list prod --appid xx

# DB tunnels work inside the container too (see "Database tunnel proxy"): connect to the host.docker.internal tunnel port
docker compose exec agent bash -c 'psql "postgresql://$VAULTY_KEEPER_BRIDGE_TOKEN@host.docker.internal:15432/appdb" -c "SELECT 1;"'
```

Isolation essentials (already built into `docker-compose.yml`):

- **No mounts** of `~/.vaulty`, the OS secret store, `~/.ssh`, or the docker socket → the container cannot reach keys or ciphertext
- Non-root user + `cap_drop: ALL` + `no-new-privileges`
- The container only reaches the masking proxy via `VAULTY_KEEPER_BRIDGE_ADDR` / `VAULTY_KEEPER_BRIDGE_TOKEN`
- Agent CLIs: `VAULTY_KEEPER_INSTALL_AGENTS='@openai/codex @anthropic-ai/claude-code opencode-ai'` (npm-installed into the user dir on container start)
- **Persistence**: the `agent-home` named volume mounts at `/home/agent`, so installed CLIs and agent session history survive container rebuilds; full reset via `docker volume rm vaulty-keeper_agent-home`
- **Linux**: compose includes `extra_hosts: host.docker.internal:host-gateway` (macOS/Windows Docker Desktop already provides it, no effect)

### Why this stops deliberately hostile AI

Even an AI that reads no docs inside the container: it cannot reach the OS secret store (VM isolation), cannot reach snapshot files (not mounted), cannot reach keys (no env passed); its only exit is the masking proxy, which **returns masked values only**. Plaintext cannot leave the host, cryptographically.

**Note: Docker itself is not absolute isolation**: `cap_drop ALL` + `no-new-privileges` sharply narrow the container-escape surface, but the Docker daemon runs as root and container escape is a real attack surface. For very high threat levels (professional adversaries / compliance audits), move to a separate macOS account, a VM, or a cloud sandbox (next section); Docker suits "prevent the vast majority of AI from grabbing keys".

### Windows users

- Same compose/image; Windows Docker Desktop is WSL2 underneath, `host.docker.internal` works the same
- Keys live in **Windows Credential Manager** (`vaulty-keeper apollo init` / `sensitive init` adapt automatically; no `security` command needed)
- Interactive menus / plaintext commands need a real console TTY (`isTTY` checks `GetConsoleMode`); scripts / proxy environments are masked automatically

### Alternatives to Docker

`serve` + `remote` are not tied to Docker; the isolation domain can be any environment that **cannot touch keys or ciphertext**:

**① Run locally (no isolation, defends against "well-behaved" AI)**

```sh
vaulty-keeper serve --addr 127.0.0.1:8970    # terminal 1: host proxy (holds keys)
export VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8970
vaulty-keeper remote list prod --appid xx    # terminal 2: masked only
```

When the AI shares your account, defense rests on masking + TTY gating; no protection against an AI that actively reads keys.

**② Separate macOS account (real isolation, Docker alternative)**

```sh
sudo sysadminctl -addUser ai -password '<pw>' -admin no   # one-time creation
# start an agent (token is masked-only, harmless in the ai session):
sudo -u ai env VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8970 \
  VAULTY_KEEPER_BRIDGE_TOKEN="$(cat ~/.vaulty/bridge-token)" codex
```

The `ai` account has no keys in its Keychain and cannot read `~/.vaulty/` (0700); isolation is comparable to Docker. Cost: you manage the account, git credentials, and file permissions.

**③ Remote machine / WSL2**

Put the agent on another machine or Windows WSL2; the host's `vaulty-keeper serve --addr 0.0.0.0:8970` is reachable over the network (token-gated, masked-only).

## Database tunnel proxy (AI queries DBs, DSN never exposed)

> Full ASCII diagrams (what's in Docker / where credentials live / auth injection for three DBs / security boundary / sequence) live in **[`docs/db-proxy-architecture.md`](docs/db-proxy-architecture.md)** (Chinese).
> Many tested usage examples (multi-connection / client commands / container AI / permissions / scripts) live in **[`docs/db-proxy-examples.md`](docs/db-proxy-examples.md)** (Chinese).

Lets an AI in a container/isolated domain query databases with **native clients** (psql / mysql / redis-cli) and get real data, while the database connection URL (host/account/password) is **never exposed to the AI**. URLs exist only as ciphertext in vaulty-keeper on the host (independent DB key, `VAULTY_KEEPER_DB_KEY` / OS secret store); `serve` opens one TCP tunnel per connection, injects the real credentials during the handshake, then forwards raw bytes.

```
[Docker container: AI agent]
  psql "postgresql://$TOKEN@host.docker.internal:15432/appdb"   # token in the user field
  mysql -h host.docker.internal -P 15435 -u "$TOKEN" -px         # token in the username field
  redis-cli -a "$TOKEN" -p 15434                                  # token in AUTH
        ▼ TCP
[Host: vaulty-keeper serve --addr 0.0.0.0:8970]
  HTTP masking bridge (existing) + one TCP tunnel per connection (validate token → connect real DB with decrypted URL → inject real credentials → forward)
        ▼
  real databases
```

**Usage**

```sh
vaulty-keeper db init                                                          # first run: create DB key
printf 'postgres://app:pass@db.example.com:5432/orders' \
  | vaulty-keeper db add orders [--port 15432]                                 # URL via stdin, never in argv/history
vaulty-keeper db list                                                          # orders (postgres) :15432
vaulty-keeper db regen orders                                                  # rotate that connection's tunnel token; old token dies immediately
vaulty-keeper db regen --all                                                   # rotate all tunnel tokens
vaulty-keeper db off orders [--all]                                            # close the tunnel, port stops listening (serve picks it up in ~2s)
vaulty-keeper db on orders [--all]                                             # reopen the tunnel
vaulty-keeper serve --addr 0.0.0.0:8970                                        # start masking bridge + tunnels together
```

- Type is auto-detected from the URL scheme: `postgres://`/`postgresql://`, `mysql://`, `redis://`/`rediss://`
- **Multiple connections of the same type**: one name + one independent tunnel port each, unlimited (e.g. three MySQL: `mysql-orders`/`mysql-billing`/`mysql-reporting`; assign or auto-allocate ports at `db add`), fetch commands per connection with `db connect <name>`
- Inside containers/isolated domains, use `vaulty-keeper db list` (reads via the bridge when there's no local store) or `vaulty-keeper remote dblist` to find tunnel ports, then connect with a native client (`$TOKEN` is the connection-specific token printed by `vaulty-keeper db connect <name>`; legacy connections without one fall back to the global `VAULTY_KEEPER_BRIDGE_TOKEN`)
- **Hot reload**: `serve` syncs `db.json` every 2 seconds — `db add`/`db rm`/`db regen`/`db on`/`db off` opens/closes tunnels automatically, **no serve restart needed**
- **Tunnels are on by default**; `db off <name>|--all` closes one (the port stops listening), `db on` reopens it; `db list`/`remote dblist` show a `[off]` marker; the UI has an Open/Close tunnel button per row
- `vaulty-keeper db connect <name>` prints the **ready-to-run client command with the token filled in** (psql/mysql/redis-cli); `--container` switches to `host.docker.internal`, `--host` targets another host, `--cmd` prints a single one-line command; every tunnel link carries **user+password** (token in PG/MySQL's user field / Redis's AUTH password; the other field is a placeholder `x` the tunnel ignores), so GUI tools that require both fields work
- `vaulty-keeper db regen <name>|--all` rotates tunnel tokens: every connection has its own **per-connection token** (128-bit random, stored encrypted alongside the URL, generated at `db add`); rotate one connection alone when a token leaks — the global bridge token is unaffected
- **Credential injection**: PG fake server passes through (trust-style, token in the user field); MySQL swaps the real password's auth response into the handshake (supports `mysql_native_password` / `caching_sha2_password`); Redis proxy sends the real `AUTH` on the client's behalf. Clients never need the real password
- **TLS**: PG honors the URL's `sslmode` (require/verify-ca/verify-full/prefer), MySQL uses `?tls=true`, Redis uses `rediss://` to reach the real DB; client↔proxy is plaintext on localhost/LAN
- **Read-only control**: the proxy does not enforce read-only; registering a URL with a read-only account is naturally read-only
- `vaulty-keeper db shell <name>` opens a native client interactively on the host (TTY-only; credentials via env, never in argv)
- Mongo is not supported (no mature Go proxy library; use `vaulty-keeper db shell` on the host or mongosh directly)

**Security boundary**

- Tunnel listen addresses follow `--addr`: default `127.0.0.1`; containers need `0.0.0.0` (LAN-reachable), **gated by the token** — the token is validated in PG/MySQL's username field and Redis's first AUTH command (either the per-connection token or the global bridge token matches); LAN users without a token are disconnected on connect
- Close tunnels you are not using with `db off` (the port stops listening entirely) and reopen with `db on`; tunnels are on by default and the state persists in db.json
- Real URLs/credentials exist only in host memory: db.json has no plaintext, logs record nothing, no reply carries them
- Tunnel tokens are per-connection (128-bit random, stored encrypted with the URL), rotatable via `db regen`; legacy connections fall back to the global bridge token (used by the masking bridge, 128-bit random) with rate-limited failures; a leaked token is by design (the AI is supposed to use it) — this defends against "third parties without a token"

### Manual verification (one-shot Docker)

`scripts/dbtest.sh` starts postgres + MySQL(8.4, with a simulated `shop` business database) + redis containers with Docker, registers connections, starts `serve`, runs the full positive/negative test suite, and keeps the environment running:

```sh
make build
./scripts/dbtest.sh          # start and test; environment stays up, prints connection info
./scripts/dbtest.sh --clean  # teardown: stop serve, remove containers
```

Or verify step by step (once the environment is ready):

```sh
TOKEN=$(cat ~/.vaulty/bridge-token)   # serve rotates the token on each restart, grab it first

# ① Redis directly on the host (token in AUTH, never touches the real password)
redis-cli -p 15434 -a "$TOKEN" --no-auth-warning ping
redis-cli -p 15434 -a "$TOKEN" --no-auth-warning set k v && redis-cli -p 15434 -a "$TOKEN" --no-auth-warning get k

# ② Simulate an in-container AI: native clients through host.docker.internal (token in user/username)
docker run --rm postgres:17.6-alpine psql "postgresql://$TOKEN@host.docker.internal:15432/appdb" -c "SELECT 1;"
docker run --rm mysql:8.4 mysql -h host.docker.internal -P 15435 -u "$TOKEN" -pxxx --ssl-mode=DISABLED -e "SELECT COUNT(*) FROM shop.orders;"
#   (15435=caching_sha2 user, 15436=mysql_native_password user; both auth methods covered)

# ③ Negative: wrong tokens are always rejected
redis-cli -p 15434 -a WRONG --no-auth-warning ping        # → ERR authentication required

# ④ Tunnel info / masking bridge (readable without a DB key)
export VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8972 VAULTY_KEEPER_BRIDGE_TOKEN="$TOKEN"
vaulty-keeper remote dblist      # connection name/type/port

# ⑤ Interactive db shell (TTY-only; redis works on the host, PG/MySQL need psql/mysql installed)
vaulty-keeper db shell cache

# ⑥ Audit log (successful authenticated / rejected invalid bridge token; no DSNs)
cat /tmp/vaulty-keeper-dbtest-serve.log | grep dbproxy:
```

## Safe usage guide for AI / scripts

### Security model overview

In one sentence: **plaintext exits only on the user's own terminal (AI/script environments are always refused, `--yes` cannot override), and masking is the reversed default**. Against "same-user AI that actively reads keys" this tool makes no promise (see trust boundary).

| Layer | Mechanism |
|---|---|
| At-rest encryption | All snapshot values AES-256-GCM on disk (0600, no plaintext); two independent keys: snapshot key (non-sensitive) + sensitive-value key (sensitive), both in the OS secret store (macOS Keychain / Windows Credential Manager / Linux Secret Service) |
| Trust boundary | The OS secret store **does not protect against same-user processes** (tested: a same-UID process can read both keys without prompting via `security find-generic-password -w`); it protects against other users/other machines/accidental plaintext. Against a **deliberately hostile** same-user AI, use "Container isolation" to put the AI in a domain that cannot touch the keys |
| Masking proxy | `vaulty-keeper serve` (host holds keys) + `vaulty-keeper remote` (inside the container/isolated domain) — the container side only gets `*** (n chars)` + length + fingerprint, **even for keys marked safe with `set --plain`**; token-gated + rate-limited |
| AI reads | **Reversed default**: `get`/`list`/`compare` mask everything (`*** (n chars)`) in non-TTY environments, no guessing from key names; only keys explicitly marked safe via `set --plain` / `mark --plain` return plaintext. Plaintext exits (reveal, export, edit, `--reveal`, `aes decrypt`) are **always refused in non-interactive terminals, even with `--yes`** — only on the user's own TTY |
| AI writes | `set`/`unset`/`mark`/`import` are safe (write-then-encrypt), no `--yes` needed |
| DB tunnels | `vaulty-keeper db add` only encrypts the URL (independent DB key + `~/.vaulty/db.json`, 0600) and generates a **per-connection tunnel token**; `serve` opens TCP tunnels and injects real credentials at handshake; clients use the per-connection token (legacy connections fall back to the bridge token; PG/MySQL username field / Redis AUTH); `db regen` rotates tokens; **tunnels are on by default, `db on/off` toggles each connection (port stops/resumes listening, state persists)**; DSNs never leave the host, never in logs/replies |
| Web UI | 127.0.0.1 only + random token gating writes/plaintext exits, GET returns masked data only (unmarked keys are always masked); **plaintext endpoints (reveal/export/plaintext edit/AES decrypt) are disabled by default**, require `--allow-plaintext` explicitly, otherwise 403 even with the token; token failures rate-limited (exponential backoff) |
| Brute-force resistance | Fingerprints are HMAC-SHA256 (keyed by the snapshot key); without the key, weak values cannot be matched offline against masked fingerprints; tokens are 128-bit random |
| Consistency checks | Use `compare` (mask + length + fingerprint), don't `get` plaintext |

Every vaulty-keeper subcommand works fine in non-TTY (script/AI agent) environments, and `--json` output is AI-friendly. But once plaintext hits stdout it enters the conversation context and session logs (e.g. `~/.codex`, terminal scrollback), where it may be persisted or synced. Know the safe vs dangerous commands:

**Safe (masked by default; fine to give AI/scripts)**
- `apollo list <env> --appid xx [--json]` — unmarked keys show `*** (n chars)`, no guessing from key names
- `apollo compare <a> <b> --appid xx --appid-to yy [--json]` — unmarked values masked + length
- `apollo get <env> <key>` — unmarked keys output `*** (n chars)`
- `apollo set/unset/mark`, `init`, `rm --yes` — write/delete operations, safe
- `remote list|get|compare` — read via the masking proxy, **masked only, always** (even for keys marked safe)
- `db list` / `remote dblist` — connection name/type/port only, **never the URL**
- `db add` — write-only (encrypted), safe; URL from stdin, never in argv/shell history

**Keys to allowlist for the AI**: mark them safe explicitly first, then the AI can read plaintext (e.g. `APP_NAME`, `LOG_LEVEL` — values you know contain nothing sensitive):
- `apollo set <env> <key> <value> --plain` (marks while setting)
- `apollo mark <env> <key> --plain` (marks only, value unchanged)

**Mis-mark guard**: with `set --plain` / `mark --plain`, if the key name or value matches the sensitive rules (password/token/secret/JWT/credential-bearing URI), **non-TTY is always refused** and TTY requires a second confirmation — prevents accidentally marking a sensitive key safe and leaking it to the AI.

**Dangerous (prints plaintext; only in an interactive terminal (TTY), always refused in AI/script environments, `--yes` cannot override)**
- `apollo reveal <env> <key>` → decrypted plaintext
- `apollo export <env>` → everything in plaintext
- `apollo list/compare --reveal` → plaintext
- `aes decrypt` → plaintext

Plaintext commands are **unconditionally refused** in non-interactive terminals (scripts / AI agents), even with an explicit `--yes` — an AI cannot get plaintext even if induced to ask. Plaintext is only visible on the user's own terminal (TTY) and enters the terminal session log; clean up after use.

Other notes:
- **Keys never enter AI environments**: snapshot key lives in macOS Keychain (`vaulty-keeper apollo init`), sensitive key in Keychain (`vaulty-keeper sensitive init`); never `export VAULTY_KEEPER_APOLLO_KEY` / `VAULTY_KEEPER_SENSITIVE_KEY` inside an AI session — the snapshot key decrypts non-sensitive values, the sensitive key decrypts everything sensitive. Same for `VAULTY_KEEPER_AES_KEY` / `VAULTY_KEEPER_AES_IV`: never pass them as `--key`/`--iv` command-line arguments (they show up in `ps` and shell history). Note that a process with the same privileges as the AI can read `~/.vaulty/aes.json` (plaintext AES key/iv) and Keychain items (`security find-generic-password -w`); real isolation means putting keys where the AI process cannot read them (different account/sandbox).
- `import` refuses to overwrite an existing snapshot (TTY asks; scripts/AI must pass `--force` explicitly) so old snapshots are never silently lost.
- To check whether a key matches between two environments, use `compare` (masked + length is enough); don't `get` plaintext.

## Verification

- `internal/aesx`: byte-for-byte aligned with vectors from `tools/javaref/CryptoUtil.java` (Java 8 reference implementation; GCM is deterministic), plus key-length validation, wrong key/iv, invalid base64.
- `internal/apollo`: real pasted samples (incl. glued lines), comments, first `=`, URL params not split, encrypted snapshot on disk (no plaintext in file, 0600), diffs, sensitive detection.
- `internal/cli`: mixed argument order, import auto-naming, reveal (sensitive plaintext / explicit `--key`/`--iv` external ciphertext / multi-key JSON), edit (fake editor script), list/compare JSON, gen-key usability, aes `--name` list, completion.
- Regenerate Java vectors: `cd tools/javaref && javac CryptoUtil.java && java CryptoUtil encrypt <key> <iv> <plaintext>`