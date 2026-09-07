# Full UI Migration Design

English | [中文](2026-08-25-full-ui-migration-design.zh-CN.md)

> **Historical design source, 2026-08-25; lifecycle annotated 2026-09-07. Not an executable plan.** Retains original tradeoffs, proposed APIs and test requirements, without certifying that all shipped. Current operations/security: [UI guide](../../ui-guide.md), [Apollo guide](../../apollo-snapshot-guide.md). The [historical plan](../plans/2026-08-25-full-ui-migration.md) retains implementation source.
>
> **Superseded assumptions:** Deferring the AI plaintext boundary, retaining the TTY menu, optional AppID, `/api/aes/config` routes and universal second confirmation below are not current contracts. The TTY menu was removed; snapshot identity was followed by the [Env+AppID design](2026-08-25-env-appid-and-snapshot-delete.md). An old `confirm:true` field does not prove human confirmation. Do not replay key initialization/plaintext examples. Retain the shared `internal/app` rationale without restoring removed routes or relaxing permissions.

## Goal

Migrate all `vaulty-keeper` functionality to the local Web UI as the primary manual entry point. Preserve every CLI subcommand as the scripting/automation interface, with existing behavior and output formats unchanged.

## Scope

The migration proposed:

- **Apollo:** init, import, list/get, set, unset, compare, reveal, edit and export, including clipboard copying.
- **AES:** encrypt, decrypt, gen-key and key/IV management in `~/.vaulty/aes.json`.
- **Key management:** snapshot-key initialization guidance completed inside the UI.
- **CLI:** retain everything, refactoring only internal implementation to share the application layer, without output changes.

Excluded, explicitly deferred by the user at that time: the AI caller's plaintext safety boundary, external AI integration, an MCP server, non-AES cryptography, and multi-user/remote hosting.

## Product Shape

A single-page application retaining the Claude-light visual system, with two additional sidebar sections:

- **Snapshot workspace (existing):** snapshot list, browse, search, entry CRUD, import, compare and export.
- **AES tools:** encrypt/decrypt forms, one-click gen-key, key/IV prefilled from `aes.json` or entered manually.
- **Settings:** snapshot-key availability and initialization, and `aes.json` viewing/saving/clearing.

The proposed common plaintext-exit rule for reveal, full edit, export and AES decryption results was: show only after explicit second confirmation, provide a close button, never write to localStorage/sessionStorage/cookies, and return `Cache-Control: no-store`.

## Architecture

Introduce `internal/app` as the shared domain-logic layer used by CLI and UI:

```text
internal/app/
  snapshot.go    Apollo domain operations
  aes.go         AES operations and aes.json persistence
  key.go         Snapshot-key resolution and availability
```

- Make `internal/cli` a thin adapter: flag parsing, application call, formatted output. JSON/color/masking presentation remains in CLI; names, flags and output formats stay unchanged.
- UI handlers also call `app`, retaining only HTTP adaptation: parsing, error envelopes and no-store headers.
- Move `aes.json` persistence (`aesConfigPath`/`loadAESConfig`/`saveAESConfig`) from `internal/cli/interactive.go` into `internal/app` for both callers.

### Operations Moved Into App

- Apollo: `Init`, `Import`, `GetValue`, `SetValue`, `DeleteValue`, `Compare`, `Reveal`, `EditLoad`, `EditApply`, `Export`.
- AES: `Encrypt`, `Decrypt`, `GenKey`, `AESConfigLoad`, `AESConfigSave`, `AESConfigClear`.
- Key: `SnapshotKey`, `KeyAvailable`.

## API Contract

Historical proposal: all `/api/*` responses carry `Cache-Control: no-store`. Existing endpoints remain; add:

```text
POST /api/init                         {force:bool}                  -> 201 create/store snapshot key
POST /api/snapshots/{name}/reveal      {targets:[...], confirm:true} -> 200 {values:{key:"plaintext"}}
POST /api/snapshots/{name}/edit        {confirm:true}                -> 200 {text:"KEY = value\n..."}
PUT  /api/snapshots/{name}/edit        {text:"..."}                  -> 200 full re-encryption/save
POST /api/aes/gen-key                  {bytes, iv_bytes}             -> 200 {key, iv}
POST /api/aes/transform                {op:"encrypt"|"decrypt", key, iv, text} -> 200 {result}
GET  /api/aes/config                                                -> 200 {key, iv, path}
PUT  /api/aes/config                   {key, iv}                     -> 200 save aes.json
DELETE /api/aes/config                                              -> 204 clear aes.json
```

### Endpoint Behavior

- `POST /api/init`: call `app.Init(force)`. Existing key with `force:false` returns `409 key_exists`; `force:true` regenerates even when one exists; success is 201. Show initialization only when the key is unavailable, without sending force.
- `POST /api/snapshots/{name}/reveal`: require `confirm:true`, otherwise `400 confirm_required`. Validate the snapshot name and every target using `ValidateKey`. Resolve AES configuration from `app.fs.aes.secret-key` / `app.fs.aes.iv` in the snapshot, like default CLI reveal. The proposed UI had no manual key/IV override; manual cryptography used AES tools. Decrypt each target; any failure returns a structured error with no partial plaintext.
- `POST /api/snapshots/{name}/edit`: require `confirm:true`, otherwise `400 confirm_required`. Return all plaintext as sorted `KEY = value\n` text.
- `PUT /api/snapshots/{name}/edit`: parse with `ParseKV`; no entries returns `400 empty_import`. Re-encrypt the entire snapshot using `NewSnapshot` + `Set` + `Save`, matching CLI edit. Return the new snapshot's safe summary.
- `POST /api/aes/gen-key`: accept `bytes` in `{16,24,32}` and `iv_bytes` in `{12,16}`, otherwise `400 invalid_aes_params`. Return printable key/IV.
- `POST /api/aes/transform`: invalid operation returns `400 invalid_aes_params`; missing key/IV returns 400. Call `aesx.Encrypt/Decrypt`; the proposal specified `400 decrypt_failed` on decryption errors and `aes_op_failed` on encryption errors.
- `GET /api/aes/config`: return the current file contents and path; missing file returns `{key:"",iv:""}`, not an error.
- `PUT /api/aes/config`: require nonempty key/IV, write mode 0600, return 200.
- `DELETE /api/aes/config`: delete the file; return 204 even if absent.

### Error Envelope

```json
{ "error": { "code": "confirm_required", "message": "..." } }
```

New codes proposed: `confirm_required`, `decrypt_failed`, `aes_config_io`, `key_init_failed`, `invalid_aes_params`, `aes_op_failed`, `key_exists`.

## Security Rules

- Plaintext exits (reveal, edit load, export, AES decryption) return `Cache-Control: no-store`.
- Browser plaintext never goes into localStorage/sessionStorage/cookies; it exists briefly in the current view after deliberate confirmation.
- Key/IV from `GET /api/aes/config` are sensitive plaintext, used only for form prefilling with no other persistence.
- If any reveal target fails, return no plaintext.
- Failed writes leave existing snapshots unchanged; introduce no dependencies.

## Primary Flows

### Key Initialization

1. Unavailable snapshot key shows guidance in the snapshot area: run `vaulty-keeper apollo init` or click Generate.
2. Generate calls `POST /api/init`; success refreshes the snapshot list.

### Plaintext Editing

1. Choose full plaintext editing from the snapshot context; show confirmation.
2. `POST .../edit {confirm:true}` loads all plaintext into a multiline field.
3. Save calls `PUT .../edit {text}`; the server re-encrypts the entire snapshot and refreshes the view.

### Reveal

1. Choose decrypt/show on an entry; show a warning confirmation.
2. `POST .../reveal {targets, confirm:true}` displays plaintext and a close button.

### AES Tools

1. Enter key/IV manually or prefill from `aes.json` in encrypt/decrypt forms.
2. gen-key populates the form; allow saving as default.
3. Copy results using `navigator.clipboard`; loopback is a secure context.

## Error Handling

- Keep the selected snapshot on all failures; render errors inline in the active canvas/dialog.
- Follow the existing UI rule: no `console.log` of plaintext that may contain user data.

## Testing

These were test requirements, not recorded passes:

- `internal/app`: reveal round-trip, mixed sensitive/nonsensitive edit load/apply round-trip, aes.json read/write/clear, gen-key validation and init idempotence.
- `internal/ui`: missing confirmation returns 400, plaintext responses use no-store, successful reveal, AES transform/gen-key/config operations.
- `internal/cli`: keep existing tests green as refactoring regression checks.
- Leave `internal/aesx` and `internal/apollo` unchanged.
- Manual checklist: reveal only after confirmation, edit round-trip consistency, export download only after confirmation, AES round-trip, and no plaintext outside the intended responses.

## Non-Goals And Follow-Up Work

- AI caller plaintext safety was explicitly deferred then: preserve CLI behavior, without agent detection or forced masking.
- No new dependencies or frontend framework.
- Retain the TTY menu unchanged as a fallback without a browser or in remote scenarios.
- Completion/version remain CLI-only.
- Possible later work: external-AI safe-diff summaries, `safe-json` export and an MCP server.
