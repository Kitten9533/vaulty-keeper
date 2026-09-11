# vaulty-keeper Apollo snapshots · usage walkthrough & implementation notes

> [中文](apollo-snapshot-guide.zh-CN.md) | English
>
> Explains snapshot storage, key selection, classification, explicit plaintext allowlisting and masked comparison. This is an operational guide, not a guarantee that a same-user process cannot obtain plaintext.
> Companion: [command reference](../README.md), [UI guide](ui-guide.md), [security model](security-model.md), [documentation index](README.md).

---

## Figure 1 · Overview: the whole chain in one picture

```text
Apollo text / input file (plaintext)
  -> import -> snapshot JSON (encrypted values; readable metadata)
                 secret=false: snapshot key
                 secret=true: sensitive-value key (new encryption)
                 safe=true: permits ordinary non-TTY/UI plaintext reads

Host CLI, stdin TTY: get prints plaintext; list/compare use heuristics
Host CLI, non-TTY: ordinary reads mask unless safe; gated exits refused
UI GET: masks or safe plaintext; DB connect GET also returns access tokens
Isolated client via serve: snapshot values always masked
```

**Encrypted-at-rest applies to stored snapshot values, not every file or output.** Import files, editor temporary files, exports/downloads, clipboard content, terminal output and process memory can contain plaintext; the separate AES key/IV list is plaintext JSON with file permissions. TTY checks are an accident-prevention gate, not human identity or stdout protection. Agents must not invoke plaintext exits or obtain keys. `serve` masks snapshot values but does not enforce a container's only network exit; its global token also authorizes PostgreSQL/MySQL/Redis tunnels. See the [security model](security-model.md) for the shared trust boundary.

---

## 1 · Start with synthetic data

Prerequisite: install/build `vaulty-keeper` using the [README](../README.md). In a human-controlled terminal, initialize missing keys only; check environment overrides before regenerating anything (§3). In a local editor, prepare these two complete **synthetic** files. Do not paste real credentials into command arguments or shell history.

`prod.txt`:

```properties
APP_NAME = merdi
API_SECRET = demo-token-a
REDIS_URI = redis://:demo@localhost:6379/0
```

`test.txt`:

```properties
APP_NAME = merdi
API_SECRET = demo-token-b
REDIS_URI = redis://:demo@localhost:6379/0
```

```sh
# ① First run: both keys go into the system keyring (macOS Keychain / Windows Credential Manager / Linux Secret Service)
vaulty-keeper apollo init        # snapshot key (encrypts non-sensitive values)
vaulty-keeper sensitive init     # sensitive-value key (encrypts sensitive values)

# ② Import both fixtures; use unused environment/AppID pairs
#    --appid is required; --name defaults to the file name
vaulty-keeper apollo import prod.txt --name prod --appid merdi
vaulty-keeper apollo import test.txt --name test --appid merdi2

# ③ Non-TTY ordinary reads mask entries not explicitly marked safe
vaulty-keeper apollo list prod --appid merdi --json
vaulty-keeper apollo compare prod test --appid merdi --appid-to merdi2 --json

# ④ TTY get prints plaintext; non-TTY get masks unless safe (§6)
vaulty-keeper apollo get prod APP_NAME --appid merdi
```

Each file has three entries; comparison reports `API_SECRET` changed. Non-TTY masks are both `*** (12 chars)` despite different values. On a TTY, `list` shows `APP_NAME` but masks the credential-like entries. These are source-derived expectations, not a new runtime test record. CLI overwrite prompts on a TTY; non-TTY overwrite requires `--force`. It replaces the snapshot, not a merge. The UI instead rejects duplicates with HTTP 409.

Snapshots default to `~/.vaulty/apollo/`, addressed as `{env}__{appid}.json` (env name + AppID); legacy snapshots without an AppID are `{env}.json` and are read without `--appid`.

---

## 2 · What the encrypted snapshot looks like (file layout)

Illustrative layout of `~/.vaulty/apollo/prod__merdi.json` (mode 0600). Ciphertexts/nonces below illustrate the format, not reproducible output from the synthetic fixture:

```json
{
  "meta": {
    "name": "prod",
    "app_id": "merdi",
    "captured_at": "2026-09-03T08:23:26Z"
  },
  "items": {
    "API_SECRET": {
      "enc": "3Xz4BLf8aeoZNCLmmYY0NxSh92J3yPYtYYU=",
      "nonce": "VnBOQYdbGGJ/Qbji",
      "secret": true
    },
    "APP_NAME": {
      "enc": "X/OMnOMPicPpuxoUX1bPVCbuSFYeL4jNTmh/5A==",
      "nonce": "yuRomYvHYPyWEBtg",
      "secret": false
    },
    "REDIS_URI": {
      "enc": "5T6vGBMiX41Jhf06RfW6WxTe3NfsVooxWrC4/nATfacIH591XtuaCK0B1AtglR2fCc1jy5WJLL2WIJzDbcQw",
      "nonce": "i0nCJQycO9R3L+oZ",
      "secret": true
    }
  }
}
```

Key points (`internal/apollo/store.go`):

- **Every stored snapshot value is ciphertext**: each value is encrypted independently with AES-256-GCM; `enc` = Base64 ciphertext, `nonce` = a per-entry random nonce. Metadata and key names remain readable; plaintext file/output exceptions are listed above.
- **For new encryption, `secret` selects the key**: `true` = sensitive-value key, `false` = snapshot key; legacy decryption differs (§3).
- **`safe` is a separate output permission**: `secret=false` alone does not allow non-TTY/UI plaintext. New auto-classified entries default to `safe=false`; explicit `--plain` sets `secret=false, safe=true`, while `--secret` sets `secret=true, safe=false`.
- **`meta.captured_at`** records the import time (UTC RFC3339).
- In the file name `prod__merdi.json`, `__` is the separator, i.e. `{env}__{appid}.json` (`FileName` in `internal/apollo/store.go:88`).

---

## 3 · Why sensitive values need their own key

Two independent keys, both in the system keyring (`internal/apollo/keyring.go`), both overridable via environment variables:

| Key | Keychain account | Env var | Encrypts |
|---|---|---|---|
| Snapshot key | `apollo-snapshot-key` | `VAULTY_KEEPER_APOLLO_KEY` | non-sensitive values (secret=false) |
| Sensitive key | `sensitive-key` | `VAULTY_KEEPER_SENSITIVE_KEY` | sensitive values (secret=true) |

For newly encrypted entries using independently generated keys, the snapshot key alone cannot decrypt sensitive-key ciphertext. Legacy sensitive entries may still use the snapshot key: `DecryptItem` in [`internal/apollo/store.go`](../internal/apollo/store.go) first selects the configured key, then tries the snapshot key after sensitive-key decryption fails. This compatibility path is not a migration of old ciphertext.

> Nonempty environment variables take precedence over the keyring, even when the keyring works. Each must be standard Base64 encoding of exactly 32 bytes. Invalid overrides fail rather than falling back; a valid but wrong key also prevents decryption. Check the active source before generating/replacing keys; replacing a key does not re-encrypt existing data. Do not export real keys into an AI session or pass them as command-line arguments.

---

## 4 · Sensitive detection: what gets auto-marked secret

Automatic detection at import time (`IsSensitiveKeyValue` in `internal/apollo/mask.go`); any match → `secret=true`:

1. **Key name match** (case-insensitive):
   `password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key`
   → `API_SECRET`, `CMS_SECRET`, `SENTRY_AUTH_TOKEN` match. `MONGODB_URI` does not match by name alone.
2. **URI/DSN with embedded credentials**: key name contains `uri|url|dsn|connection|endpoint|addr|address`, **and** the value looks like `scheme://user[:password]@host`
   → Synthetic `REDIS_URI=redis://:demo@localhost:6379/0` and credential-bearing `MONGODB_URI` values match; a plain URL without `@` credentials (e.g. `https://example.com/api`) does not.
3. **JWT**: value shaped like `eyJ...` with three base64url segments → e.g. `SUPABASE_SERVICE_ROLE_KEY`.

Import and new-item `set` persist the detected `secret` classification and default to `safe=false`. Ordinary reads do not rewrite classification. `set` without `--plain/--secret` preserves an existing item's `secret` and `safe` flags. Masking non-TTY/UI reads depends on `safe`, not on detection succeeding.

---

## 5 · Reversed default and the TTY boundary

Output rules for non-TTY (scripts / AI) (`maskedFor` in `internal/cli/cli.go`):

- **Everything is masked by default** — no guessing from key names. `get`/`list`/`compare` print `*** (n chars)` for any key **not explicitly marked safe** (`MaskWithLen`, length preserved).
- Only keys **explicitly marked safe** with `set --plain` / `mark --plain` print plaintext.
- Plaintext exits (`reveal`/`export`/`edit`/`list|compare --reveal`/`aes decrypt`) reject non-TTY stdin even with `--yes`. The implementation checks stdin, not human identity or stdout. Agents must not fabricate a TTY or use other routes to obtain plaintext.

```sh
vaulty-keeper apollo get prod REDIS_URI --appid merdi     # non-TTY → *** (30 chars)
vaulty-keeper apollo get prod APP_NAME --appid merdi      # not allowlisted → *** (5 chars)
```

On a TTY, `list`/`compare` mask `secret` entries and heuristic matches unless `--reveal` is used. **TTY `get` directly prints the value, including sensitive values**, without `--reveal`. Local CLI list/get masks have no fingerprint; bridge list/compare and UI comparisons expose fingerprints (§7).

---

## 6 · Explicit allowlist: set --plain / mark --plain

```sh
# mark at set time
vaulty-keeper apollo set prod NEXT_PUBLIC_SAFE_FLAG true --plain --appid merdi

# or flip the flag of an existing key without changing its value
vaulty-keeper apollo mark prod APP_NAME --plain --appid merdi
vaulty-keeper apollo mark prod APP_NAME --secret --appid merdi   # revoke the allowlist
```

After `--plain` (before revoking it with `--secret`), `safe:true` is written back to the file, and non-TTY `get`/`list` print plaintext. Example item within `items`:

```json
{ "APP_NAME": { "enc": "...", "nonce": "...", "secret": false, "safe": true } }
```

**Mis-flag guard** (`guardPlainMark`): when `--plain` hits a key whose name/value looks sensitive, non-TTY is always rejected and a TTY needs a second confirmation — prevents accidentally marking `API_SECRET` as "safe" and leaking it to AI.

---

## 7 · Fingerprints: judging "are two values the same" under masks

A mask only gives the length, so different values of the same length look identical. `remote list <env>` and `remote compare` additionally show an **HMAC-SHA256 fingerprint** (first 8 bytes, encoded as 16 hex digits; `Fingerprint` in [`internal/apollo/mask.go`](../internal/apollo/mask.go)). `remote get` prints only the mask even though its API response contains a fingerprint.

- Fingerprint key = snapshot key: **without the key, weak values can't be brute-forced offline to match a fingerprint**.
- Fingerprints are comparable only under the same HMAC key and use `NormValue` (strip one matching pair of outer quotes; whitespace trimming happens separately during text parsing). A match is a high-confidence signal about normalized values, not proof of original byte equality: normalization and truncated-hash collisions matter.
- Lengths come from Go `len(string)`: **UTF-8 bytes**, despite the literal UI/CLI label `chars`. UI comparison JSON uses `length`/`fingerprint` with `value:null` for masked entries; bridge values carry a mask string and fingerprint. Local CLI JSON uses mask strings, not that API shape.

```sh
# Host terminal: keep this process running; use another terminal for remote reads
vaulty-keeper serve --addr 127.0.0.1:8970
```

```sh
# Second host terminal; uses the host's bridge-token file, not encryption keys in argv
vaulty-keeper remote list prod --appid merdi --json
vaulty-keeper remote compare prod test --appid merdi --appid-to merdi2 --json
```

`remote compare --json` returns `from`, `to`, `added`, `removed`, `changed`; changed entries contain `old`/`new` objects with `value` mask strings and `fingerprint`. Exact fingerprints depend on the key and are intentionally not invented here. Both local and remote `compare --json` currently print a plain-text identical-snapshots message when there are no differences. With the same HMAC key, different fingerprints imply different normalized values. **Use comparison instead of requesting plaintext.** For isolated clients, provision only approved proxy credentials and reachable addresses; the [DB examples](db-proxy-examples.md) distinguish host link generation from container use.

---

## 8 · Import parsing rules (how pasted text is understood)

`ParseKV` in `internal/apollo/parser.go`:

- Each line is `KEY = value`, split on the **first `=`**, trimmed on both sides; values may contain `=` inside.
- Blank lines and whole lines starting with `#` (single- or multi-line comments) are skipped.
- **Auto-splitting of glued entries**: when a line contains several `KEY = ` entries that start with an uppercase letter, they are split apart with a warning (e.g. `A = 1B = 2`; the splitter only recognizes all-uppercase keys, so lower/mixed-case glued lines are not split). URL query params are not mis-split (a `?` before `...?TOKEN=1` is not glue).
- Key validation `[A-Za-z_][A-Za-z0-9_.-]*`; invalid lines are skipped with a warning.
- Matching pairs of quotes around a value are stripped (`"merdi"` ≡ `merdi`).

---

## 9 · Plaintext commands: reveal / export / edit (stdin TTY required)

Human-operated examples only. Outputs can persist in terminal logs, redirections, editor backups and clipboard history. Reading stdin does not erase the shell command that supplied it.

```sh
vaulty-keeper apollo reveal prod --appid merdi API_SECRET      # single sensitive value in plaintext
vaulty-keeper apollo reveal prod API_SECRET APP_NAME --appid merdi --json # JSON for named keys
vaulty-keeper apollo export prod --appid merdi                 # full KEY = value (paste back to Apollo)
vaulty-keeper apollo export prod --appid merdi --copy          # prints first, then calls macOS pbcopy
vaulty-keeper apollo edit prod --appid merdi                   # open in $EDITOR, save → auto re-encrypt
```

- These commands require stdin TTY (`isTerminal()` in `internal/cli/cli.go`); that is not a human-identity check. Agents must not run them on real data. `export --copy` still prints plaintext before invoking `pbcopy` and may fail to copy on other platforms.
- `edit` flow = `Export` plaintext to a temp file (0600) → editor → `ParseKV` → full re-encrypt and write back (`app.EditLoad`/`EditApply`); you don't manage the two keys by hand while editing.
- **Full replacement, not a patch**: edit and overwrite-import rebuild the snapshot, re-detect `secret`, and reset `safe` to false. Omitted or unparsed entries can disappear. Edit rejects an entirely unparseable result and the CLI prints parser warnings to stderr (the app layer discards them, so warnings are only visible through the CLI, not the UI); check the complete text before saving and review keys/counts/classification afterward. Use single-item `set` without flags when preserving existing classification is required.
- External AES ciphertext stored as a value is a second layer: first decrypt the snapshot wrapper, then use its original external key/IV. See the [UI AES workflow](ui-guide.md). New AES-GCM encryptions must never reuse a key/IV pair for different messages. `aes gen-key` prints generated secrets; do not treat it as a masked read.

---

## 10 · Common scenarios at a glance

| What you want | Command |
|---|---|
| Land config copied from Apollo | `apollo import prod.txt --name prod --appid merdi` |
| List all snapshots | `apollo list --json` |
| Check missing keys (no decrypt) | `apollo list test merdi --names --json` |
| AI reads a value (masked) | `apollo get prod KEY --appid merdi` |
| AI checks two environments match | `apollo compare prod test --appid merdi --appid-to merdi2 --json` |
| Allowlist a definitely-safe key for AI | `apollo set prod KEY v --plain --appid merdi` / `apollo mark prod KEY --plain --appid merdi` |
| Revoke the allowlist (also classify as secret) | `apollo mark prod KEY --secret --appid merdi` |
| View sensitive plaintext (your TTY) | `apollo reveal prod KEY --appid merdi` |
| Full export / edit | `apollo export prod --appid merdi` / `apollo edit prod --appid merdi` |
| "Snapshot not found" error | look for the **similar snapshots** in the hint (other appids of the same env, or the env/appid swap) — usually a typo or swapped `--appid` |

Commands in this table are subcommands: prefix them with `vaulty-keeper`. `KEY`/`v` are placeholders, not fixture names/values.
