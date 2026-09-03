# Claude-Style Local Web UI Design

## Goal

Add a local Web UI for the existing encrypted Apollo snapshot workflow. The UI
will make snapshot management easier for manual use without replacing the CLI,
which remains the scripting and automation interface.

## Scope

The first release covers snapshot management only:

- List available snapshots and select a current snapshot.
- Import an Apollo key/value paste as a new encrypted snapshot.
- Browse a snapshot's configuration with sensitive values masked by default.
- Search configuration keys and visible values.
- Read, add, update, and remove individual configuration entries.
- Compare two snapshots and show added, removed, and changed entries.
- Export a snapshot only after a conspicuous confirmation.

AES utilities, direct `reveal`, persistent operation history, and sending data
to an external AI are out of scope. The interface may show safe-diff language,
but it will not call an external model in this release.

## Product Shape

The UI uses a calm, Claude-inspired snapshot workspace rather than a dense
enterprise administration dashboard:

- A white side rail follows Claude's workspace structure: a compact product
  mark and import action, a session-only recent-work list with timestamps, an
  active snapshot list, and a bottom local-workspace identity block. The recent
  work list is reset on reload and is not persisted in browser or disk storage.
- A compact workspace title bar identifies the current context, for example
  `Configuration workspace / prod`.
- The main canvas opens on the current snapshot with a concise serif task title
  and a reminder that values are encrypted locally and sensitive values are
  masked.
- A current-environment context row shows the snapshot name, total entry count,
  sensitive-entry count, last update time, and local-encryption status.
- The primary pane is a searchable configuration table immediately beneath the
  current-environment row. It is the visual focus of the page and keeps
  sensitive values masked by default.
- A contextual comparison panel appears beneath the table so the user can
  understand the selected environment without switching pages.
- A right-side "next steps" pane provides direct actions for comparison,
  single-key edits, export, safe AI summary generation in a later release, and
  future AES utilities.
- The selected snapshot is the active context. Actions performed from it return
  to the same context rather than forcing the user through the global menu.

The visual language follows Claude's light theme: a white rail, warm
off-white paper canvas, deep charcoal text, coral accents reserved for the
product mark, active navigation, environment marks, and changes, and subtle
green dot-plus-text encryption status. Serif display headings, low-contrast
borders, quiet text actions, a compact top bar, generous reading space, and
precise hover states keep the interface calm and legible.

The configuration table uses a search control with an icon, monospace keys and
values, right-aligned values, extremely light row dividers, and a subtle row
hover. Sensitive values use masked dots plus a small length label rather than
a colored badge. The diff uses compact symbol-prefixed rows. It is inspired by
Claude's calm tone and typography, but remains a configuration workspace and
does not imitate a chat transcript, reuse Claude assets, or claim a Claude
integration.

## Architecture

The executable gains an `ui` subcommand. It starts an HTTP server bound only to
`127.0.0.1` on a configurable ephemeral port, opens the browser, and stops on
process exit. The server must never bind to a LAN interface by default.

The server is a thin adapter over the existing Apollo package. It does not
read, decrypt, or write snapshot files directly outside that package. Existing
CLI commands may be refactored to share application-level helpers, but the CLI
contract remains intact.

The first release embeds static HTML, CSS, and browser JavaScript into the Go
binary. This preserves the project's single-binary and zero-runtime-dependency
model. The browser talks to JSON endpoints on the same loopback origin.

## Data and Security Rules

- Snapshot values remain AES-256-GCM encrypted at rest exactly as today.
- The UI uses the existing snapshot-key resolution flow. If no key is
  available, it presents the existing initialization guidance and exposes no
  snapshot values.
- All browse, list, and comparison endpoints mask entries marked sensitive.
  The response may include key name, sensitive flag, presence, and value
  length; it must not include plaintext or decryptable ciphertext for a masked
  entry.
- A reveal action is not exposed in this first release.
- Updating a sensitive value is allowed only through a dedicated edit form
  whose value field is visually marked sensitive and is never prefilled.
- Export is a deliberate local download/copy flow requiring a confirmation
  dialog that explains it contains plaintext configuration.
- API responses set `Cache-Control: no-store`. The UI does not write snapshot
  values to browser local storage, session storage, or browser logs.
- The UI must reject snapshot identifiers that are not valid snapshot names;
  this is shared with the CLI to prevent path traversal.

## Primary Flows

### Open and browse a snapshot

1. User runs `vaulty-keeper ui`.
2. The browser opens the workspace and the rail lists snapshots.
3. Selecting a snapshot opens its context on the canvas.
4. The canvas shows metadata, a searchable key/value list, and masked sensitive
   entries.
5. Selecting a non-sensitive entry can show and edit its value. Selecting a
   sensitive entry only shows metadata and a replace action.

### Import

1. User chooses the import action from the side rail or current snapshot
   context.
2. The UI collects a snapshot name, optional Apollo App ID, and pasted text.
3. The server parses the input using the existing parser and returns warnings
   before writing.
4. User confirms the import. The server encrypts and saves the new snapshot.
5. The new snapshot becomes active and the canvas reports the entry count and
   parser warnings.

### Compare

1. User invokes compare from the active snapshot context.
2. The UI selects the other snapshot.
3. The server decrypts locally, compares values, and returns a safe result.
4. The canvas renders the result as a compact diff panel: normal values are
   visible; sensitive values report only masked metadata and changed state.

### Export

1. User invokes export from the active snapshot context.
2. The UI shows a confirmation that plaintext will be generated locally.
3. On confirmation, the server produces a text download with a no-store
   response. The UI does not display the full export inline.

## Error Handling

- Requests with malformed JSON, invalid names, unavailable snapshot keys,
  missing snapshots, malformed imported text, and decryption failures return a
  structured error code and user-facing message.
- Failed import validation never creates or overwrites a snapshot.
- Failed edits and deletes leave the existing snapshot unchanged.
- The UI keeps the current selected snapshot after an error and renders an
  inline retryable message in the main canvas.

## Testing

- Package tests cover snapshot-name validation, safe UI view projection,
  sensitive value masking, import preview warnings, compare serialization, and
  no-store response headers.
- HTTP handler tests verify loopback-only defaults, input validation, error
  responses, and that sensitive plaintext is absent from list and compare JSON.
- Existing Apollo storage, parser, AES, and CLI tests remain green.
- A focused manual check opens `vaulty-keeper ui`, imports a sample, edits a normal
  and sensitive key, compares snapshots, and verifies the export confirmation.

## Non-Goals and Follow-Up Work

The first release does not implement an MCP server, external AI calls, AES UI
tools, key rotation, operation history, multi-user access, or remote hosting.
Once the safe Web UI boundary is validated, a later release can add a
`safe-json` export flow and optional user-directed handoff to an external AI.
