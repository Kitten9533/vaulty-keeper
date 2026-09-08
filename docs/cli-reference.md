# vaulty-keeper CLI Reference

> [中文](cli-reference.zh-CN.md) | English

Complete command reference for `vaulty-keeper`: the Apollo snapshot tool, AES encrypt/decrypt helpers, database tunnels and miscellaneous commands. Install and quick start live in the [README](../README.md); usage walkthroughs live in the [documentation index](README.md). This file is a **reference, not a script**: `<...>`, `[...]` and `a|b` denote placeholders/choices, never literal shell input. Substitute filenames, names, AppIDs and keys.

## vaulty-keeper apollo — Apollo snapshot tool

A walkthrough of the snapshot implementation (encrypted file layout / dual-key design / sensitive detection / masking & fingerprints / explicit allowlisting) with tested examples lives in **[`apollo-snapshot-guide.md`](apollo-snapshot-guide.md)** ([中文版](apollo-snapshot-guide.zh-CN.md)).

A fallback for when the Apollo Open API is unavailable: copy key-value pairs from the Apollo portal and import them into an encrypted snapshot. AI/script access follows the masking and write permissions in the [README safety guide](../README.md#safe-usage-guide-for-ai--scripts). Snapshots live under `~/.vaulty/apollo/` by default (override with `--dir` or `VAULTY_KEEPER_APOLLO_DIR`).

After human host key initialization (see the README), this complete example creates two snapshots in a new temporary directory using **synthetic values only**. Keep the directory variable in this terminal for all commands:

```sh
DEMO_SNAP_DIR=$(mktemp -d)
printf '%s\n' 'APP_NAME = demo' 'LOG_LEVEL = info' 'SECRET_TOKEN = synthetic-prod' \
  | vaulty-keeper apollo import - --dir "$DEMO_SNAP_DIR" --name prod --appid demo
printf '%s\n' 'APP_NAME = demo' 'LOG_LEVEL = debug' 'SECRET_TOKEN = synthetic-test' \
  | vaulty-keeper apollo import - --dir "$DEMO_SNAP_DIR" --name test --appid demo
vaulty-keeper apollo list prod --dir "$DEMO_SNAP_DIR" --appid demo --json </dev/null
vaulty-keeper apollo compare prod test --dir "$DEMO_SNAP_DIR" --appid demo --appid-to demo --json </dev/null
```

The comparison reports `LOG_LEVEL` and `SECRET_TOKEN` as changed with masked values. This uses host key storage, not an isolated key fixture.

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

## Database tunnels

Encrypted connection store and TCP tunnels. Exact flags: `vaulty-keeper db -h` and `vaulty-keeper db <cmd> -h`. Per-protocol setup: [PostgreSQL](tunnel/postgres-tunnel-guide.md) / [MySQL](tunnel/mysql-tunnel-guide.md) / [Redis](tunnel/redis-tunnel-guide.md) / [MongoDB](tunnel/mongodb-tunnel-guide.md). Architecture and fixtures: [db-proxy-architecture](db-proxy-architecture.md), [db-proxy-examples](db-proxy-examples.md). Security boundary: [security-model](security-model.md).

```sh
vaulty-keeper db init
vaulty-keeper db add <name> [--port <port>] [--test]   # URL on stdin; tunnel stays off until db on
vaulty-keeper db list [--json]
vaulty-keeper db test <name>
vaulty-keeper db connect <name> [--container] [--cmd] [--host <host>]
vaulty-keeper db regen <name>|--all
vaulty-keeper db on|off <name>|--all
vaulty-keeper db show <name>     # TTY only: decrypted URL
vaulty-keeper db shell <name>    # TTY only: direct backend client
vaulty-keeper db rm <name> [--yes]
```

`db show` / `db shell` are plaintext exits (stdin TTY). Agents use `list` / `test` / `connect` / `on` / `off` / `regen`.

## Misc

```sh
vaulty-keeper ui                              # start local web UI (default 127.0.0.1:8080, auto-increments if busy)
vaulty-keeper serve --addr 0.0.0.0:8970       # masking proxy (for containers/isolated domains when the host holds keys)
vaulty-keeper remote list|get|compare ...     # read through the masking proxy (same shape as apollo subcommands)
vaulty-keeper completion zsh | source /dev/stdin   # or bash / fish; add to your shell config
vaulty-keeper lang [en|zh]    # show or set the shared UI/CLI language
vaulty-keeper version
```
