# Env+AppID Unique Key, Snapshot Delete, And Import Dialog Polish Design

English | [中文](2026-08-25-env-appid-and-snapshot-delete.zh-CN.md)

> **Historical source of implemented decisions, 2026-08-25; lifecycle annotated 2026-09-07. Not an executable plan or complete current API contract.** Retains identity, naming, compatibility and deletion-confirmation rationale and original test requirements; requirements are not passing evidence. Current operations: [Apollo guide](../../apollo-snapshot-guide.md), [UI guide](../../ui-guide.md). This follows the optional-AppID assumptions in the [initial UI](2026-08-25-claude-style-web-ui-design.md) and [full migration](2026-08-25-full-ui-migration-design.md).
>
> Old `--app-id` spelling, test expectations and UI layout below are retained as proposals from that date; use the guides' current `--appid` spelling. Later operations require fresh task authorization; this record grants no deletion, migration or plaintext-access permission.

## Goal

Use environment + required AppID as the snapshot's unique key. Support snapshot deletion in UI and CLI, show both environment and AppID in the list, and improve preview scrolling in the import dialog.

## Scope

- Storage: new snapshots use `{env}__{appid}.json`; retain read compatibility with old `{env}.json` files without AppID.
- CLI: support `--appid` across commands; require it for import/rm; add confirmed `rm`.
- UI: show environment + AppID in the sidebar, add confirmed list-item deletion, require AppID for import, detect conflicts by env+appid, and make the preview scrollable.
- Exclude migration scripts, batch operations by AppID and automatic AppID selection for comparison; explicitly supply from/to AppID.

## Storage & Naming

Proposed `internal/apollo` changes:

- Add `SnapshotRef{ Name, AppID string }`.
- `ListSnapshots(dir) ([]SnapshotRef, error)` scans `*.json` and reads Name/AppID from metadata, not filename parsing.
- Add `ValidateAppID(appID) error`: nonempty, matching `^[A-Za-z0-9][A-Za-z0-9_.-]*$`.
- Add `FileName(name, appID) string`: nonempty AppID yields `{name}__{appID}.json`; empty yields `{name}.json`.
- Add `SnapPath(dir, name, appID) string` using `filepath.Join(dir, FileName(...))`.
- Detect conflicts by target-file existence. The original proposal noted that checking the resulting filename also catches collisions involving `__` inside an environment name.
- Extend the CLI's existing `snapPath(dir, name)` to accept AppID; leave `ValidateSnapshotName` unchanged.

## CLI

- Add `--appid` to get/list/set/unset/compare/reveal/edit/export/import/rm. Access commands without it read `{env}.json` for legacy compatibility.
- Import: the original proposal called the flag `--app-id`, made it required and included it in filenames; omission returns an error.
- Add `apollo rm <env> --appid X`, requiring AppID; deletion in this proposal targets snapshots with AppID only.
- In TTY, ask `Delete env (appid)? [y/N]`; non-TTY requires `--yes`.
- Success prints `removed env (appid)` with exit code 0; absence returns an error and exit code 1.
- Update usage and completion.

## UI API

Pass URL-encoded `appid` in the query; preserve route paths and legacy compatibility.

- `GET /api/snapshots`: keep the existing `app_id` response field.
- `GET /api/snapshots/{env}?appid=X`: read `{env}__{X}.json`; without appid read `{env}.json`.
- `POST /api/snapshots`: `{env, app_id(required), text}`. Missing/invalid AppID returns `400 invalid_app_id`; an existing file returns `409 snapshot_exists`, with environment and AppID in the message.
- `DELETE /api/snapshots/{env}?appid=X`: delete and return 204; absent returns `404 snapshot_not_found`.
- Item mutation, export, reveal and edit endpoints accept optional `?appid=`.
- `GET /api/compare?from=&to=&from_appid=&to_appid=`: AppIDs optional, defaulting to `{env}.json`.

## UI Frontend

- Two-line sidebar items: environment + entry count first, smaller AppID from `app_id` second.
- Show a delete button on hover without selecting the item. Click opens environment/AppID confirmation, then DELETE and refresh. If deleting the active snapshot, select the first remaining snapshot or empty state.
- Change `state.active` from environment alone to `{ env, appid }`; attach `?appid=` to API calls.
- Rename the import label from Snapshot Name to Environment; make Application ID required with frontend empty-input feedback. Set `.preview` to `max-height: 220px; overflow-y: auto`.
- Display the server conflict message: environment (appid) already exists.

## Security

- AppID participates in paths, so validate with `ValidateAppID` to prevent traversal.
- Issue deletion only after UI confirmation. Plaintext-exit rules remain unchanged: no-store and confirm.

## Testing

- `internal/apollo`: valid/empty/invalid/traversal AppID cases, both filename forms, env+appid listing and `SnapPath`.
- `internal/app`: existing/absent `Remove`, AppID import naming/conflicts and legacy `{env}.json` reads.
- `internal/cli`: flag parsing/addressing, rm confirmation/`--yes`/absence.
- `internal/ui`: DELETE 204/404/missing-AppID 400, POST missing-AppID 400, views and items with AppID.
- Manual frontend checklist: two-line list, deletion confirmation/refresh, import required fields/conflicts and preview scrolling.
