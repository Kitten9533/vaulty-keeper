# vaulty-keeper Web UI · features & usage guide

> [中文](ui-guide.zh-CN.md) | English
>
> The local web UI (`vaulty-keeper ui`) covers all snapshot, AES encrypt/decrypt and database-tunnel features. This guide walks through each page: what it does, how to use it, and where the security boundary is.
> Companion: `README.md` (full command reference), `docs/apollo-snapshot-guide.md` (snapshot internals), `docs/db-proxy-architecture.md` (DB tunnel diagrams).

---

## 1 · Start & access

```sh
vaulty-keeper ui                    # default port; rolls forward to the next free port if taken
vaulty-keeper ui --port 8123        # fixed port
vaulty-keeper ui --allow-plaintext  # additionally enable plaintext endpoints (export / decrypt / plaintext edit / view real URL)
```

- On start it prints `http://127.0.0.1:<port>/?t=<token>` and opens your default browser. On macOS it first looks for an already-open vaulty-keeper UI tab (loopback URL with `?t=`) in Chrome/Arc/Edge/Brave/Chromium/Opera/Safari and navigates that tab to the new URL instead of opening a new one.
- **Listens on 127.0.0.1 only**; the token is freshly randomized on every start (128-bit). **Do not share the token-bearing URL with AI/scripts, and don't paste it into logs or shell history.**
- **Plaintext endpoints are disabled by default** (a yellow banner shows at the top). Restart with `--allow-plaintext` to enable them; otherwise export / decrypt / plaintext edit / view real URL return 403 even with a valid token.
- The UI's static assets are embedded with `go:embed`: after editing anything under `internal/ui/static/` you must `make build` for changes to take effect.

### Windows

The Windows release is `vaulty-keeper-windows-x86_64.zip`; unzip it to get `vaulty-keeper.exe`, which works exactly like the other platforms — the only difference is the `.exe` suffix:

```powershell
# PowerShell / CMD, in the extraction directory:
.\vaulty-keeper.exe ui                   # default port; rolls forward to the next free port if taken
.\vaulty-keeper.exe ui --port 8123       # fixed port
.\vaulty-keeper.exe ui --allow-plaintext # additionally enable plaintext endpoints
```

- It prints `http://127.0.0.1:<port>/?t=<token>` and opens your default browser automatically (via `rundll32`).
- **In PowerShell you need the `.\` prefix to run a program in the current directory** (bare `vaulty-keeper.exe ui` is not found); CMD does not require it.
- Keys live in the **Windows Credential Manager**; data lives under `%USERPROFILE%\.vaulty\` (snapshots in `.vaulty\apollo\`, DB connections in `.vaulty\db.json`).
- Everything else — behavior and the security model — is identical to the other platforms (see §8).

## 2 · Interface overview

Three parts:

- **Left rail**: the **Import snapshot** button on top; the snapshot list below (grouped and collapsible by environment, each entry shows appid, item count and last-update time, with a delete button on hover); then the **Tools** navigation (AES encrypt/decrypt, Database tunnels, Settings).
- **Top bar**: breadcrumb (current snapshot) + language toggle (EN / 中文).
- **Four views** (switch via the left rail):

| View | What it does |
|---|---|
| Snapshots (default) | browse/search/edit snapshot items, import, compare, export |
| AES encrypt/decrypt | AES-GCM with a manual key/iv (Java CryptoUtil compatible) |
| Database tunnels | register database connections, manage tunnels, generate token-filled client links |
| Settings | view/initialize the snapshot key and the sensitive-value key |

## 3 · Snapshots

### 3.1 Import a snapshot

Click **Import snapshot** in the left rail (or the CTA in the main area) and fill the dialog:

1. **Environment**: the env name (e.g. `prod`).
2. **App ID (required)**: the Apollo app id (e.g. `merdi-portal`). Duplicate names are flagged as you type; overwriting needs a second confirmation.
3. Paste the `KEY = value` config text (multi-line).
4. Click **Preview** first: a parse preview (item count, sensitive flags) appears; once it looks right, click **Import**.

Everything is encrypted to disk on import (`~/.vaulty/apollo/{env}__{appid}.json`, 0600); sensitive values are auto-detected.

### 3.2 Browse & search

- Click a snapshot in the left rail; the main area shows all its items (key, masked value, fingerprint/updated time).
- The search box filters by **key or visible value**.
- **Sensitive values show as masks** `*** (n chars)` by default; judge consistency by length + fingerprint, never by guessing.

### 3.3 Add / edit / delete an item

Click any row in the table to open the **Edit item** dialog:

- Regular values: the current plaintext is shown; edit and Save.
- **Sensitive values: plaintext is not shown** (a warning explains why). Entering a new value replaces it; **saving an empty value keeps it unchanged**.
- **Delete** removes the item (with confirmation).

### 3.4 Compare environments (pair / multi / single key)

- **Compare environments**: compare the current snapshot with another one, listing added / removed / changed; the diff is filterable and copyable.
- **Compare across environments**: tick 2+ snapshots and compare every key side by side; the result can be **Copied as table (Tab-separated)**, **Copied as CSV**, or turned into a **Diff report** (with stats: total keys, differences, sensitive differences).
- **Single-key comparison**: click a key to see its value across all snapshots (mask + fingerprint).

No plaintext anywhere in this flow; sensitive values stay masked.

### 3.5 Export & plaintext edit (need `--allow-plaintext`)

- **Export config**: generates the full plaintext `KEY = value` content — **Copy to clipboard** or **Export** (browser download). A warning reminds you it contains sensitive values: view on this machine only, don't forward.
- **Plaintext-edit all**: edit the whole config in a plaintext editor; saving re-encrypts the whole snapshot and writes it back.

Both are plaintext exits: **disabled by default, require `--allow-plaintext`**, and each has its own confirmation step.

### 3.6 Reveal a single value (needs `--allow-plaintext`)

Click "Reveal" on an item and confirm; the plaintext is shown in the dialog. If the value was not encrypted with the system sensitive key (e.g. external AES ciphertext), expand the advanced options and fill in a manual AES key / IV, then retry.

## 4 · AES encrypt/decrypt

Left rail → **AES encrypt/decrypt**:

1. **AES key**: a 16/24/32-byte UTF-8 string.
2. **IV**: UTF-8 bytes.
3. Put plaintext or base64 ciphertext in the input → click **Encrypt** / **Decrypt** → the result appears below; **Copy result** is available.

Compatible with Java `CryptoUtil` (AES/GCM/NoPadding). **Decrypt is a plaintext exit and needs `--allow-plaintext`**, otherwise it returns 403.

## 5 · Database tunnels

### 5.1 Initialize the DB key

The view checks the DB key (`VAULTY_KEEPER_DB_KEY` / system keyring) on load. If missing, an **Initialize database key** button appears — click it to generate.

### 5.2 Register a connection

Fill in the **New connection** card:

- **Name**: connection name (e.g. `mysql-orders`).
- **Tunnel port (optional)**: leave empty for auto-assignment (starting at 15432, skipping conflicts).
- **Database URL**: `postgres://user:pass@host:5432/dbname`, `mysql://…`, `redis://:pass@…`.

**The URL never crosses the wire as plaintext**: the browser fetches the server's ECDH public key from `/api/db/pubkey`, derives an AES-GCM key in the browser, encrypts the URL, and only then POSTs it; the matching private key lives in the UI process memory only and is regenerated on every start.

- **Test connection**: try connecting with the entered URL first (nothing is stored).
- **Register connection**: encrypts it to `~/.vaulty/db.json` (0600) and generates a dedicated tunnel token for the connection.

### 5.3 Connection table & actions

The table lists Name / Type / State / Port / Actions:

| Action | Effect |
|---|---|
| **Test** | connect to the database **directly** with the decrypted real URL (not through the tunnel) to verify the registered connection |
| **Connect info** | dialog with every ready-to-use client link for this connection: raw tunnel link, psql/libpq, DBeaver/DataGrip JDBC, pgAdmin4 fields, Redis Insight, redis-cli — token already filled in, copy & run |
| **Regenerate** | rotate this connection's tunnel token; **old links stop working immediately** (confirmation required); "Regenerate all" at the top rotates every connection |
| **Enable / Disable tunnel** | turn the tunnel on/off per connection (port stops/resumes listening; `serve` picks it up within ~2 s), state persisted in db.json |
| **View URL** | shows the decrypted real database URL — **visible only in your browser**; the button is hidden by default and only appears with `--allow-plaintext` |
| **Delete** | remove the connection; its tunnel disappears with it (confirmation required) |

> Connections that decrypt but fail token validation etc. are marked **Broken** and only offer delete. AI never sees real URLs/credentials; tunnel usage details live in `docs/db-proxy-architecture.md` and `docs/db-proxy-examples.md`.

## 6 · Settings

View and initialize the two keys:

- **Snapshot key**: encrypts non-sensitive values.
- **Sensitive-value key**: encrypts sensitive values.

When uninitialized, a **Generate** button appears per key; initialized keys show their status. These are the same keys as `apollo init` / `sensitive init`.

## 7 · Language switch & CLI sync

The top-bar toggle switches **English / 中文**: the choice is remembered per browser in `localStorage` and also written to the shared `~/.vaulty/prefs.json` — so CLI output and the UI share a language. On first visit the UI adopts the shared CLI/UI language; from the command line use `vaulty-keeper lang en|zh`.

## 8 · Security model (why this is safe to use)

- **Listens on 127.0.0.1 only**; the token is randomized per start, and every state-changing request (import / add / edit / delete / export / decrypt / plaintext edit) requires it (URL `?t=` or `X-Auth-Token` header). Failed attempts are throttled with exponential backoff.
- **GET endpoints return masked data only**: list/view/compare are mask + length + fingerprint, never plaintext.
- **Plaintext endpoints off by default**: export, decrypt, plaintext edit and the real-URL view require `--allow-plaintext`, and each has its own confirmation step.
- **CSRF protection**: cross-origin requests are rejected; all responses are `no-store`, so the browser caches nothing.
- **DB URL transport encryption**: URLs are encrypted in the browser (ECDH + AES-GCM) before being sent.

In one sentence: **state changes without a token are rejected, plaintext exits are locked by default, and AI/scripts hitting the open GET endpoints only ever get masks.** Don't share the token-bearing URL with AI or scripts.

## 9 · FAQ

| Symptom | Fix |
|---|---|
| Changes to static files don't show | rebuild with `make build` (assets are embedded via go:embed) |
| Plaintext buttons do nothing / 403 | restart the UI with `--allow-plaintext`; each plaintext action also needs its confirmation |
| Language out of sync with the CLI | switch in the top bar, or `vaulty-keeper lang en|zh` |
| Want a fresh token / lost the URL | restart `vaulty-keeper ui` — the token is randomized on every start |
| A connection shows Broken | usually a key mismatch (db.json stores a key_id); re-register with `db add <same name>` in the CLI |
| Port already in use | it rolls forward to the next free port automatically, or pass an explicit `--port` |

---

*Dev note: `internal/ui/static/` (index.html / app.js / app.css) is the frontend; `internal/ui/ui.go` and `internal/ui/db.go` hold the API and gates.*
