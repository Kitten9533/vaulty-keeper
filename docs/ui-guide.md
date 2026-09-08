# vaulty-keeper Web UI · features & usage guide

> [中文](ui-guide.zh-CN.md) | English
>
> The local web UI (`vaulty-keeper ui`) provides snapshot management, AES encrypt/decrypt and database-tunnel management. This guide describes the current UI flows and their limits; it does not imply parity with every CLI command.
> Companion: [command reference](../README.md), [Apollo snapshots](apollo-snapshot-guide.md), [DB architecture](db-proxy-architecture.md), [security model](security-model.md), [documentation index](README.md).

---

## 1 · Start & access

```sh
vaulty-keeper ui                    # default port; rolls forward to the next free port if taken
vaulty-keeper ui --port 8123        # starting port; also rolls forward if occupied
vaulty-keeper ui --allow-plaintext  # additionally enable plaintext endpoints (export / decrypt / plaintext edit / view real URL)
```

- On start it prints `http://127.0.0.1:<port>/?t=<token>` and opens your default browser. On macOS it first looks for an already-open vaulty-keeper UI tab (loopback URL with `?t=`) in Chrome/Arc/Edge/Brave/Chromium/Opera/Safari and navigates that tab to the new URL instead of opening a new one.
- **Listens on 127.0.0.1 only**; the token is freshly randomized on every start (128-bit). **Do not share the token-bearing URL with AI/scripts, and don't paste it into logs or shell history.**
- **Plaintext endpoints are disabled by default** (a yellow banner shows at the top). Restart with `--allow-plaintext` to enable them; otherwise export / decrypt / plaintext edit / view real URL return 403 even with a valid token.
- Keep the UI process running while using the browser; use another terminal for other commands. The UI manages DB configuration but does not itself start TCP tunnels (§5).
- The UI's static assets are embedded with `go:embed`: after editing anything under `internal/ui/static/`, run `make test` (including the Node UI check) and `make build` for changes to take effect.

### Windows

The Windows release name includes its version, for example `vaulty-keeper-0.9.0-windows-x86_64.zip`; unzip it to get `vaulty-keeper.exe`. Use the `.exe` suffix:

```powershell
# PowerShell / CMD, in the extraction directory:
.\vaulty-keeper.exe ui                   # default port; rolls forward to the next free port if taken
.\vaulty-keeper.exe ui --port 8123       # starting port; also rolls forward if occupied
.\vaulty-keeper.exe ui --allow-plaintext # additionally enable plaintext endpoints
```

- It prints `http://127.0.0.1:<port>/?t=<token>` and opens your default browser automatically (via `rundll32`).
- **In PowerShell you need the `.\` prefix to run a program in the current directory** (bare `vaulty-keeper.exe ui` is not found); CMD does not require it.
- Snapshot, sensitive-value and DB keys use **Windows Credential Manager** unless a nonempty environment override takes precedence; data lives under `%USERPROFILE%\.vaulty\` (snapshots in `.vaulty\apollo\`, DB connections in `.vaulty\db.json`). The separate AES key/IV list is plaintext JSON, not a Credential Manager entry.
- API gates are shared across platforms (§8); browser launch and credential-store implementations differ. The latest release **v0.9.0** includes default-off tunnels and bundles the current `docs/` guides; earlier release binaries (e.g. 0.6.0/0.7.x/0.8.0) predate the default-off behavior.

## 2 · Interface overview

Three parts:

- **Left rail**: the **Import snapshot** button on top; the snapshot list below (grouped and collapsible by environment, each entry shows appid, item count and last-update time, with a delete button on hover); then the **Tools** navigation (AES encrypt/decrypt, Database tunnels, Settings).
- **Top bar**: breadcrumb (current snapshot) + language toggle (EN / 中文).
- **Four views** (switch via the left rail):

| View | What it does |
|---|---|
| Snapshots (default) | browse/search/edit snapshot items, import, compare, export |
| AES encrypt/decrypt | AES-GCM with a manual key/iv (Java CryptoUtil compatible) |
| Database tunnels | register PostgreSQL/MySQL/Redis/MongoDB connections, manage tunnels, generate token-filled client links |
| Settings | view initialization status and initialize the snapshot/sensitive-value keys; not a key-value viewer |

## 3 · Snapshots

### 3.1 Import a snapshot

Click **Import snapshot** in the left rail (or the CTA in the main area) and fill the dialog:

1. **Environment**: the env name (e.g. `prod`).
2. **App ID (required)**: the Apollo app id (e.g. `merdi-portal`). The environment/AppID pair must be unused. Duplicate pairs are blocked in the dialog; the API also rejects them with HTTP 409. There is no UI overwrite confirmation path.
3. Paste the `KEY = value` config text (multi-line).
4. Click **Preview** first: review parsed key names, sensitive flags and any warnings about skipped or split lines; then click **Import**. Warnings may include raw input, so treat the preview as potentially plaintext.

Snapshot values are encrypted on import (`~/.vaulty/apollo/{env}__{appid}.json`, 0600); names/metadata remain readable and input text/clipboard content remain plaintext. Classification is auto-detected and persisted, but all newly imported entries default to `safe=false`. CLI overwrite differs: it requires TTY confirmation or non-TTY `--force`, and replaces the snapshot rather than merging.

### 3.2 Browse & search

- Click a snapshot in the left rail; the table has **Key** and **Value** columns with row actions. It does not have per-item fingerprint or updated-time columns; the rail/context shows snapshot time.
- The search box filters by **key or visible value**.
- **Any entry without `safe=true` is masked**, including `secret=false` entries. Explicitly safe values appear as plaintext without `--allow-plaintext`. UI/API `sensitive` here means `!safe`, not the stored `secret` classification that selects the encryption key; snapshot summary sensitive counts use `secret`.
- Displayed `chars` lengths are UTF-8 byte counts. Fingerprints are available in comparison views, not the ordinary item table; see [fingerprint semantics](apollo-snapshot-guide.md).

### 3.3 Edit / delete an item; adding keys

Click any row in the table to open the **Edit item** dialog:

- Explicitly safe values: current plaintext is shown; edit and Save.
- Masked values (`!safe`): current plaintext is not loaded into the editor. Entering a new value replaces it; **saving an empty or whitespace-only input keeps it unchanged**.
- **Delete** removes the item (with confirmation).
- Saving sends the UI masking flag as `secret`: replacing a masked item writes `secret=true, safe=false`, even if it was previously `secret=false`; saving a visible item writes `secret=false, safe=true`. This API does not apply the CLI's `--plain` heuristic guard. Do not replace a visible safe value with a secret: it would remain readable through ordinary GET.
- The row dialog has a fixed key label, not a new-key/name field. Add a key through whole-snapshot plaintext edit (§3.5), or the CLI `apollo set`; use flagless CLI `set` for existing items when preserving classification is required.

### 3.4 Compare environments (pair / multi / single key)

- **Compare environments**: compare the current snapshot with another one, listing added / removed / changed; the diff is filterable and copyable.
- **Compare across environments**: tick 2+ snapshots and compare every key side by side; the result can be **Copied as table (Tab-separated)**, **Copied as CSV**, or turned into a **Diff report** (with stats: total keys, differences, sensitive differences).
- **Single-key comparison**: use the row's **Compare this key** button. It filters to the selected snapshot's AppID (including the empty legacy AppID), not every AppID. Missing keys are shown as absent.

Comparisons can include explicitly safe plaintext. Other entries remain masked with byte length and fingerprint. Fingerprints are truncated HMACs of normalized values under the same snapshot key, not absolute proof of original byte equality. Pair comparison shows changed values as plaintext only when both sides are safe; multi/single-key comparison projects each entry's own safe flag.

### 3.5 Export & plaintext edit (need `--allow-plaintext`)

- **Export config**: opens a warning dialog; choose **Copy to clipboard** or **Export** (browser download) to request the full plaintext `KEY = value` content. The download currently has a `.json` filename but contains text, not encrypted snapshot JSON. Clipboard history and downloaded files can retain secrets.
- **Plaintext-edit all**: opening the dialog immediately requests the full plaintext; there is no additional load confirmation. Saving **replaces the entire snapshot**, re-detects `secret`, and resets `safe` to false. Omitted/unparsed entries can disappear. A completely unparseable result is rejected, but partial parse warnings are currently discarded. Check the complete text before saving and review keys/counts/classification afterward; use single-item CLI edits to retain existing flags.

Both need the UI token and **`--allow-plaintext`**. The request field `confirm:true` is supplied by JavaScript; it is not proof of a separate human confirmation or identity. Do not delegate plaintext actions on real data to agents.

### 3.6 Reveal a single value (needs `--allow-plaintext`)

Click **Reveal**, then the dialog's **Reveal** button (English UI) in the dialog to request the value. Normally this decrypts the snapshot wrapper using its configured snapshot/sensitive key and displays the stored value. If that value is itself external AES ciphertext, a successful reveal shows that ciphertext, not the external plaintext.

The manual key/IV fields appear **only after a reveal request fails**, not as an always-expandable option. A retry with both overrides first decrypts the snapshot wrapper, then the external ciphertext; it cannot repair a missing/wrong snapshot key. To decrypt external ciphertext after a successful reveal, a human can use the AES tool (§4) with the original external key/IV. Do not deliberately break key configuration to expose the advanced fields.

## 4 · AES encrypt/decrypt

Left rail → **AES encrypt/decrypt**:

1. **AES key**: a 16/24/32-byte UTF-8 string.
2. **IV**: a nonempty UTF-8 byte string. Key/IV lengths count bytes, not Unicode characters; the UI trims surrounding whitespace from both fields.
3. Put plaintext or base64 ciphertext in the input → click **Encrypt** / **Decrypt** → the result appears below; **Copy result** is available.

Compatible with Java `CryptoUtil` (AES/GCM/NoPadding; Base64 ciphertext including the authentication tag). **Decrypt needs `--allow-plaintext`**, otherwise it returns 403. Encrypt/Decrypt directly send a token-gated request, with no second confirmation dialog; the inputs/results can contain secrets. This manual external AES key is not the Base64-encoded 32-byte snapshot/DB key.

**Never encrypt different messages with the same AES-GCM key/IV pair.** Use a fresh unique IV for each new encryption under a key; decrypt with the exact original pair. Java compatibility does not make repeated fixed-IV encryption safe. This UI does not manage the CLI's persisted AES key/IV list. CLI `aes gen-key` prints generated secrets and is not a masked read.

## 5 · Database tunnels

### 5.1 Initialize the DB key

The view checks the DB key on load. A nonempty `VAULTY_KEEPER_DB_KEY` takes precedence over the system keyring; it must be standard Base64 encoding of exactly 32 bytes. An unavailable key shows **Initialize database key**. Before generating, check whether an invalid override or unavailable keyring caused the status: an invalid override does not fall back, and generation does not migrate existing data or fix an override.

### 5.2 Register a connection

Fill in the **New connection** card:

- **Name**: connection name (e.g. `mysql-orders`).
- **Tunnel port (optional)**: leave empty for auto-assignment starting at 15432. It skips ports registered in this store, not OS-occupied ports; use an explicit available port when reproducible client configuration matters.
- **Database URL**: supported schemes include `postgres://`, `mysql://`, `redis://`, `mongodb://`. For example, `postgres://demo:demo@localhost:5432/appdb` is a **synthetic syntax example**, not a provisioned database.

MongoDB 8 accepts one fixed endpoint with normal username/password authentication. Follow the [registered-URL whitelist](tunnel/mongodb-tunnel-guide.md#registered-url), not a generic driver's full option set: registered `retryWrites` is rejected even though generated client URIs include `retryWrites=false`. The backend account needs `listCollections` privilege to verify ordinary collections; views/time-series and full admin/GUI introspection are unsupported. A successful connection test does not verify all business collection privileges.

**Registration and entered-URL tests encrypt the URL before POST**: the browser fetches the server's ECDH public key from `/api/db/pubkey`, derives an AES-GCM key, and encrypts the URL; the private key lives in UI process memory and is regenerated at startup. This scope does not include **View URL**, which returns decrypted plaintext over loopback HTTP, or all subsequent database traffic. Do not treat it as a general TLS guarantee.

- **Test connection**: try connecting with the entered URL first (nothing is stored).
- **Register connection**: encrypts it to `~/.vaulty/db.json` (0600) and generates a dedicated tunnel token for the connection. The tunnel stays off until **Turn on tunnel**.
- Registering the same name replaces the connection, generates a new token and resets `enabled` to false. Leaving the port blank retains its previous port. Re-distribute new connection info and turn the tunnel on again if it should listen.

MySQL tunnel `?tls=true` negotiates TLS with the backend and uses the upgraded connection for authentication and forwarding (add `tlsCAFile=<path>` to trust a private/self-signed CA); the fix is unit-tested, and a one-off native TLS query passed though that evidence is not pinned by an integration test. A direct **Test connection** performs the same backend TLS upgrade but is not a full tunnel test. See the [DB examples](db-proxy-examples.md) for protocol-specific limits.

### 5.3 Start the tunnel service

After registration, a host terminal with access to the same DB store/key must run `vaulty-keeper serve --addr 127.0.0.1:8970` and stay running. A watcher starts only if the DB store exists and its key resolves **when serve starts**. If serve was started bridge-only before the first registration, restart it after registration. An active watcher picks up add/delete/on/off changes about every two seconds. UI enabled/disabled is stored configuration, not listener health or proof that a client can connect.

Install the chosen native client separately. The UI's **Connect info** uses `127.0.0.1` and has no container-address toggle. For containers, generate links using host CLI `vaulty-keeper db connect <name> --container` and deliver only approved tunnel credentials. `--container` changes the printed host to `host.docker.internal`, not the listening address; serve needs a container-reachable bind and appropriate firewall restrictions. Container-side `remote dblist` returns metadata only; local `db connect` still requires the local DB store/key. Do not mount host keys to make it work. See the [DB examples](db-proxy-examples.md).

The left sidebar **Database tunnels** card lists every registered connection. On is a green switch labeled On; off is a red switch labeled Off. Click the switch to toggle without a confirmation dialog (the table's Turn off button still confirms). Click the name to open this view. With none registered it shows **No tunnels configured**. The switch reflects saved `enabled` state, not listener health.

### 5.4 Connection table & actions

The table lists Name / Type / State / Port / Actions:

| Action | Effect |
|---|---|
| **Test** | connect to the database **directly** with the decrypted real URL (not through the tunnel) to verify the registered connection |
| **Connect info** | protocol-specific raw tunnel link and client choices: psql/libpq, DBeaver/DataGrip JDBC, pgAdmin4 fields, Redis Insight, redis-cli or mongosh; tunnel token already filled in |
| **Regenerate** | rotate this connection's dedicated token for new connections (confirmation required); re-distribute new links; does not revoke established sessions or the global token; "Regenerate all" rotates every dedicated token |
| **Turn off all tunnels** | next to "Regenerate all"; confirmation required; sets every connection to `enabled=false`. An active watcher closes listeners on the next sync; established sessions are not forcibly closed |
| **Enable / Disable tunnel** | persist the desired state in db.json; an active serve watcher closes/reopens the listener on its next sync, normally ~2 s; established sessions are not forcibly closed |
| **View URL** | directly requests and displays the decrypted backend URL, with no second confirmation dialog; only shown with `--allow-plaintext`, and requires the UI token |
| **Delete** | remove the connection (confirmation required); an active watcher removes its listener, without a promise to terminate established sessions |

> **Broken** means the stored URL cannot be decrypted (for example, a stale key or damaged ciphertext); the row only offers delete. Token decryption/Resolve failures are separate and may occur without a Broken badge. Check key source first; same-name re-registration has the token/enabled-state effects described above.

PostgreSQL/MySQL/Redis accept the current global bridge token **or** the dedicated connection token for both new and old registrations. Dedicated-token rotation does not revoke global-token access. Mongo differs as below. Tunnel links hide backend authentication credentials, but their tokens grant database access and query results may contain secrets.

Mongo **Connect info** uses user `vaulty`, dedicated token as SCRAM-SHA-256 password, `authSource=admin`, `directConnection=true` and `retryWrites=false`; no global bridge-token fallback. These proxy-only links are intended for authorized agents/tools, unlike the UI access token or **View URL** output. The generated mongosh command includes the tunnel token in argv, not the real backend URI; protect it as an access credential, or use a password prompt for human clients. Disabling a listener does not promise to kill established sessions. See the [Mongo guide](tunnel/mongodb-tunnel-guide.md) for plaintext-network restrictions, unchanged business data and the verified/pending test matrix.

**Evidence scope:** the 2026-09-07 implementation record reports test/race/vet/build and MongoDB 8.0.13 standalone/fixed-replica-set Go-driver/mongosh checks passed on the uncommitted Mongo worktree based on `7273bb21e8347777058a17fb95a16aa2a17a36dc` (macOS 12.4 Intel). This is historical evidence, not a rerun for this documentation edit or manual browser validation. Actual Mongo TLS, manual TTY `db shell`, and the unavailable independent follow-up review remain unverified; the [Mongo guide](tunnel/mongodb-tunnel-guide.md) owns the detailed matrix.

## 6 · Settings

View initialization status and initialize the two keys, not their secret values:

- **Snapshot key**: encrypts non-sensitive values.
- **Sensitive-value key**: encrypts sensitive values.

When unavailable, a **Generate** button appears per key; initialized keys show their status. These are the same keys as `apollo init` / `sensitive init`. As with the DB key, nonempty environment overrides win over keyring values and must decode from Base64 to 32 bytes. An error status is not proof the original key is absent: check the source before generating. New-format key isolation and legacy snapshot-key fallback are described in the [Apollo guide](apollo-snapshot-guide.md).

## 7 · Language switch & CLI sync

The top-bar toggle switches **English / 中文**. Browser `localStorage` wins for that browser; only without a local choice does the UI try `/api/prefs`. Switching also attempts a token-gated write to `~/.vaulty/prefs.json`, but failure is silently ignored. Existing browser choices are not continuously synchronized with CLI changes.

CLI precedence is `VAULTY_KEEPER_LANG` → shared prefs → `en`. Use `vaulty-keeper lang en` or `vaulty-keeper lang zh` (not the literal `en|zh`). The UI's prefs response resolves the server process environment and current shared file on each request; changing a server launch-time environment override requires restarting that process. Some flag descriptions, prompts and underlying errors remain English. A browser choice, server process and env-overridden CLI can therefore disagree.

## 8 · API and plaintext boundaries

- **Loopback and UI token**: listens on 127.0.0.1; non-GET API requests require the per-start token (`?t=` or `X-Auth-Token`). Failed-token delays grow linearly by 50 ms per failure, capped at 2 s, not exponentially.
- **GET is not masked-only or token-gated**: snapshot view/compare may return explicitly safe plaintext; `GET /api/db/connect?name=...` can return a usable database token and token-filled links without a UI token or `--allow-plaintext`. Backend passwords remaining hidden does not make these access credentials harmless.
- **Dedicated plaintext endpoints**: reveal/export/edit/AES decrypt/real-URL view need both the UI token and `--allow-plaintext`. Confirmation flows vary (§3-5); `confirm:true` is not human authentication. The CLI's stdin TTY check is likewise not human identity.
- **Origin and caching**: the server rejects `Origin: null` and origins whose host differs from the served host; clients with no Origin header are allowed. `Cache-Control: no-store` discourages caching, but cannot erase downloads, clipboard history, screenshots, logs or browser memory.

Do not share the UI token-bearing URL or delegate plaintext actions to agents. Only deliver database tokens to authorized consumers. Key custody, plaintext-at-rest exceptions (including AES key/IV JSON), same-user access and container limitations have one canonical home: the [security model](security-model.md).

## 9 · FAQ

| Symptom | Fix |
|---|---|
| Changes to static files don't show | run `make test` then `make build` (assets are embedded via go:embed) |
| Plaintext buttons do nothing / 403 | check the current token and restart the UI with `--allow-plaintext`; follow the action-specific flow, not an assumed universal second confirmation |
| Language out of sync with the CLI | check CLI env overrides, browser local preference, and whether the token-gated prefs write succeeded (§7) |
| Want a fresh token / lost the URL | restart `vaulty-keeper ui` — the token is randomized on every start |
| A connection shows Broken | check key source/ciphertext first; same-name re-registration replaces the token and restores enabled state, keeping the old port only when no new port is supplied |
| UI port already in use | even explicit `--port` is a starting port; read the actual printed URL. DB port allocation is separate and does not probe OS availability |
| Enabled DB connection cannot be reached | verify serve started with store/key available, its bind/firewall and client installation; a direct connection test does not prove listener health (§5.3) |

---

*Dev note: `internal/ui/static/` (index.html / app.js / app.css) is the frontend; `internal/ui/ui.go` and `internal/ui/db.go` hold the API and gates.*
