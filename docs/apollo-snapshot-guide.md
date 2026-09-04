# vaulty-keeper Apollo snapshots · usage walkthrough & implementation notes

> [中文](apollo-snapshot-guide.zh-CN.md) | English
>
> Explains how the "paste config from the Apollo portal → encrypted to disk → AI-safe reads" chain works: what a snapshot file looks like, how the two keys divide the work, how sensitive values are detected, why AI can only ever get masks, and how to explicitly allowlist / compare for consistency.
> Companion: `README.md` (full command reference), `docs/db-proxy-architecture.md` (DB tunnel diagrams).

---

## Figure 1 · Overview: the whole chain in one picture

```
┌─────────────────────────────────────────────────────────────────────┐
│ Apollo portal (config center)                                       │
│   copy KEY = value text — plaintext appears only while you paste it │
└──────────────────────┬──────────────────────────────────────────────┘
                       │  vaulty-keeper apollo import prod.txt --name prod --appid merdi
                       ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Host: ~/.vaulty/apollo/{env}__{appid}.json   (0600, no plaintext    │
│       on disk)                                                      │
│                                                                     │
│   items: KEY → { enc(AES-256-GCM ciphertext), nonce(random),        │
│           secret }                                                  │
│     secret=false → snapshot key    secret=true → sensitive key      │
│   both keys live in the system keyring (Keychain / env              │
│   VAULTY_KEEPER_*_KEY fallback)                                     │
└───────┬──────────────────────────┬────────────────────┬─────────────┘
        │ ① your own TTY          │ ② AI / scripts      │ ③ AI in a   │
        │                         │    (non-TTY)        │    container│
        ▼                         ▼                     ▼   (isolated)│
┌───────────────────┐  ┌──────────────────────┐  ┌──────────────────────┐
│ get/list/compare  │  │ get/list/compare      │  │ remote list/get/…   │
│ reveal/export/edit│  │  → mask + length +    │  │  (via serve mask    │
│  → plaintext      │  │    fingerprint        │  │   bridge, masks     │
│  (incl. sensitive)│  │  plaintext commands   │  │   only, always)     │
│                   │  │  always rejected      │  │                     │
└───────────────────┘  └──────────────────────┘  └──────────────────────┘
```

In one sentence: **config appears as plaintext only at import/export time; the rest of the time it is ciphertext. AI/scripts get only mask + length + fingerprint by default; plaintext exits are available only on your own terminal. A containerized AI can reach any config (even masked) only through `serve`.**

---

## 1 · One-minute start

```sh
# ① First run: both keys go into the system keyring (macOS Keychain / Windows Credential Manager / Linux Secret Service)
vaulty-keeper apollo init        # snapshot key (encrypts non-sensitive values)
vaulty-keeper sensitive init     # sensitive-value key (encrypts sensitive values)

# ② Copy KEY=value text from the Apollo portal and import an encrypted snapshot
#    --appid is required; --name defaults to the file name; existing snapshots need --force
vaulty-keeper apollo import prod.txt --name prod --appid merdi
#   imported 4 entries into snapshot "prod" (appid merdi) (~/.vaulty/apollo/prod__merdi.json)

# ③ List (masked by default, AI-friendly)
vaulty-keeper apollo list prod --appid merdi --json
#   { "name": "prod", "app_id": "merdi", "items": {
#     "API_SECRET": "*** (12 chars)", "APP_NAME": "*** (5 chars)", ... } }

# ④ Read a single value (non-TTY prints plaintext only for keys explicitly marked safe, see §6)
vaulty-keeper apollo get prod APP_NAME --appid merdi
```

Snapshots default to `~/.vaulty/apollo/`, addressed as `{env}__{appid}.json` (env name + AppID); legacy snapshots without an AppID are `{env}.json` and are read without `--appid`.

---

## 2 · What the encrypted snapshot looks like (file layout)

After importing the 4 values above, `~/.vaulty/apollo/prod__merdi.json` (mode 0600) actually contains:

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

- **Every value is ciphertext**: each value is encrypted independently with AES-256-GCM; `enc` = Base64 ciphertext, `nonce` = a per-entry random nonce (different per entry, so identical plaintext yields different ciphertext). No plaintext value ever touches disk.
- **The `secret` field selects which key encrypted it**: `true` = sensitive-value key, `false` = snapshot key (§3).
- **`meta.captured_at`** records the import time (UTC RFC3339).
- In the file name `prod__merdi.json`, `__` is the separator, i.e. `{env}__{appid}.json` (`FileName` in `internal/apollo/store.go:88`).

---

## 3 · Why sensitive values need their own key

Two independent keys, both in the system keyring (`internal/apollo/keyring.go`), both overridable via environment variables:

| Key | Keychain account | Env var | Encrypts |
|---|---|---|---|
| Snapshot key | `apollo-snapshot-key` | `VAULTY_KEEPER_APOLLO_KEY` | non-sensitive values (secret=false) |
| Sensitive key | `sensitive-key` | `VAULTY_KEEPER_SENSITIVE_KEY` | sensitive values (secret=true) |

Security value: **a leaked snapshot key (e.g. sent somewhere by mistake) still can't decrypt sensitive values** — sensitive values are encrypted with the sensitive key, and `apollo init` / `sensitive init` are two independent key generations. Showing sensitive plaintext via `reveal`/`--reveal` requires the sensitive key (`DecryptItem` in `internal/app/snapshot.go` picks the key by `secret`).

> Env overrides only fall back for environments without a keyring (e.g. Linux headless servers without Secret Service). They are red-line keys: don't export them into an AI session, don't pass them as command-line arguments.

---

## 4 · Sensitive detection: what gets auto-marked secret

Automatic detection at import time (`IsSensitiveKeyValue` in `internal/apollo/mask.go`); any match → `secret=true`:

1. **Key name match** (case-insensitive):
   `password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key`
   → `API_SECRET`, `CMS_SECRET`, `SENTRY_AUTH_TOKEN`, `MONGODB_URI`… all match.
2. **URI/DSN with embedded credentials**: key name contains `uri|url|dsn|connection|endpoint|addr|address`, **and** the value looks like `scheme://user[:password]@host`
   → `REDIS_URI=redis://:pw@r-abc...:6379/0` matches; a plain URL without `@` credentials (e.g. `https://example.com/api`) does not.
3. **JWT**: value shaped like `eyJ...` with three base64url segments → e.g. `SUPABASE_SERVICE_ROLE_KEY`.

Principle: **err on the side of masking**. Auto-detection is not written back to the file (`secret` is this run's verdict; `set` without `--plain/--secret` keeps the existing entry's verdict).

---

## 5 · Reversed default: why AI only gets masks

Output rules for non-TTY (scripts / AI) (`maskedFor` in `internal/cli/cli.go`):

- **Everything is masked by default** — no guessing from key names. `get`/`list`/`compare` print `*** (n chars)` for any key **not explicitly marked safe** (`MaskWithLen`, length preserved).
- Only keys **explicitly marked safe** with `set --plain` / `mark --plain` print plaintext.
- Plaintext exits (`reveal`/`export`/`edit`/`list|compare --reveal`/`aes decrypt`) are **always rejected on non-interactive terminals, even with `--yes`** — TTY only, on your own terminal.

```sh
vaulty-keeper apollo get prod REDIS_URI --appid merdi     # non-TTY → *** (43 chars)
vaulty-keeper apollo get prod APP_NAME --appid merdi      # not allowlisted → *** (5 chars)
```

On a TTY the old heuristics apply: sensitive names / inline credentials are masked, `--reveal` shows plaintext.

---

## 6 · Explicit allowlist: set --plain / mark --plain

```sh
# mark at set time
vaulty-keeper apollo set prod NEXT_PUBLIC_SAFE_FLAG true --plain --appid merdi

# or flip the flag of an existing key without changing its value
vaulty-keeper apollo mark prod APP_NAME --plain --appid merdi
vaulty-keeper apollo mark prod APP_NAME --secret --appid merdi   # revoke the allowlist
```

After allowlisting, `safe:true` is written back to the file, and non-TTY `get`/`list` print plaintext:

```json
"APP_NAME": { "enc": "...", "nonce": "...", "secret": false, "safe": true }
```

**Mis-flag guard** (`guardPlainMark`): when `--plain` hits a key whose name/value looks sensitive, non-TTY is always rejected and a TTY needs a second confirmation — prevents accidentally marking `API_SECRET` as "safe" and leaking it to AI.

---

## 7 · Fingerprints: judging "are two values the same" under masks

A mask only gives the length, so different values of the same length look identical. `remote compare`/`remote get` additionally give an **HMAC-SHA256 fingerprint** (8-byte hex, `Fingerprint` in `internal/apollo/mask.go`):

- Fingerprint key = snapshot key: **without the key, weak values can't be brute-forced offline to match a fingerprint**.
- Judging whether a key matches between two environments: **same length + same fingerprint → same value**, with no plaintext ever shown.

```sh
vaulty-keeper remote compare prod test --appid merdi --appid-to merdi2 --json
#   { "changed": { "REDIS_URI": { "old": {"value":"*** (43 chars)","fingerprint":"3f2a..."},
#                                   "new": {"value":"*** (43 chars)","fingerprint":"9c11..."} } } }
```

Different fingerprint → different value, even at the same length. **Judge consistency with `compare`, never `get` plaintext** (the safe posture for AI).

---

## 8 · Import parsing rules (how pasted text is understood)

`ParseKV` in `internal/apollo/parser.go`:

- Each line is `KEY = value`, split on the **first `=`**, trimmed on both sides; values may contain `=` inside.
- Blank lines and whole lines starting with `#` (single- or multi-line comments) are skipped.
- **Auto-splitting of glued entries**: multiple `KEY = ` entries glued into one line are split apart with a warning (e.g. `A = 1B = 2`), while URL query params are not mis-split (a `?` before `...?TOKEN=1` is not glue).
- Key validation `[A-Za-z_][A-Za-z0-9_.-]*`; invalid lines are skipped with a warning.
- Matching pairs of quotes around a value are stripped (`"merdi"` ≡ `merdi`).

---

## 9 · Plaintext commands: reveal / export / edit (your TTY only)

```sh
vaulty-keeper apollo reveal prod --appid merdi API_SECRET      # single sensitive value in plaintext
vaulty-keeper apollo reveal prod --appid merdi --json          # JSON for multiple keys
vaulty-keeper apollo export prod --appid merdi                 # full KEY = value (paste back to Apollo)
vaulty-keeper apollo export prod --appid merdi --copy          # straight to the clipboard
vaulty-keeper apollo edit prod --appid merdi                   # open in $EDITOR, save → auto re-encrypt
```

- All **work only on an interactive terminal**; scripts/AI are always rejected (the `isTerminal()` gate at each command entry in `internal/cli/cli.go`).
- `edit` flow = `Export` plaintext to a temp file (0600) → editor → `ParseKV` → full re-encrypt and write back (`app.EditLoad`/`EditApply`); you don't manage the two keys by hand while editing.

---

## 10 · Common scenarios at a glance

| What you want | Command |
|---|---|
| Land config copied from Apollo | `apollo import prod.txt --name prod --appid merdi` |
| List all snapshots | `apollo list` |
| AI reads a value (masked) | `apollo get prod KEY --appid merdi` |
| AI checks two environments match | `apollo compare prod test --appid merdi --appid-to merdi2 --json` |
| Allowlist a definitely-safe key for AI | `set prod KEY v --plain` / `mark prod KEY --plain` |
| Revoke the allowlist | `mark prod KEY --secret` |
| View sensitive plaintext (your TTY) | `apollo reveal prod KEY --appid merdi` |
| Full export / edit | `apollo export prod --appid merdi` / `apollo edit prod --appid merdi` |
| "Snapshot not found" error | look for the **similar snapshots** in the hint (other appids of the same env) — usually a typo in `--appid` |
