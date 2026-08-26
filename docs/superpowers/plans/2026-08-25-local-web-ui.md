# Local Web UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `ai-tools ui`, a loopback-only, Claude-light-theme Web UI for managing encrypted Apollo snapshots without exposing sensitive plaintext during ordinary browsing or comparison.

**Architecture:** Create an `internal/ui` package that exposes a testable JSON HTTP handler and a blocking loopback server launcher. The handler adapts the existing `internal/apollo` encrypted storage APIs into safe view models; static HTML, CSS, and JavaScript are embedded in the Go binary and call only same-origin API endpoints. The CLI only parses `ui` flags and passes the configured snapshot directory to the UI package.

**Tech Stack:** Go standard library (`net/http`, `net`, `embed`, `httptest`, `os/exec`), existing `internal/apollo` package, static HTML/CSS/vanilla JavaScript.

---

## File Structure

- Modify: `internal/apollo/store.go`
  - Add snapshot-name validation and safe, decrypted-to-view projection helpers shared by HTTP handlers.
- Modify: `internal/apollo/parser.go`
  - Export the existing key-validation rule so item mutation APIs and CLI use the same accepted key format as import parsing.
- Modify: `internal/apollo/store_test.go`
  - Cover name validation and ensure safe projections never contain sensitive plaintext.
- Modify: `internal/apollo/parser_test.go`
  - Cover the exported key validator with accepted and rejected key names.
- Create: `internal/ui/ui.go`
  - Define UI configuration, server startup, loopback listener creation, safe JSON response helpers, and API routing.
- Create: `internal/ui/ui_test.go`
  - Exercise the handler through `httptest` with an injected key provider and temporary snapshot directory.
- Create: `internal/ui/browser_darwin.go`
  - Open the loopback URL with macOS `open`.
- Create: `internal/ui/browser_linux.go`
  - Open the loopback URL with Linux `xdg-open`.
- Create: `internal/ui/browser_other.go`
  - Keep other platforms usable by returning without attempting to launch a browser.
- Create: `internal/ui/static/index.html`
  - Semantic shell for the v6 visual design, dialogs, and the accessible action controls.
- Create: `internal/ui/static/app.css`
  - Claude-light visual system approved in the v6 mockup, including responsive behavior.
- Create: `internal/ui/static/app.js`
  - Same-origin API client and rendering/actions for list, import, safe browse, edit, delete, compare, confirmed export, and a reload-only recent-work list.
- Modify: `internal/cli/cli.go`
  - Register `ui`, document it, parse `--dir`, `--port`, and `--no-open`, then launch `internal/ui`.
- Modify: `internal/cli/cli_test.go`
  - Verify `ui` usage and flag validation without starting a browser process.
- Modify: `README.md`
  - Document local-only behavior, launch command, first-release scope, and sensitive-value guarantees.

## API Contract

All `/api/*` responses must include `Cache-Control: no-store`.

```text
GET    /api/snapshots
GET    /api/snapshots/{name}
POST   /api/import/preview
POST   /api/snapshots
PUT    /api/snapshots/{name}/items/{key}
DELETE /api/snapshots/{name}/items/{key}
GET    /api/compare?from={name}&to={name}
POST   /api/snapshots/{name}/export
```

Request and response requirements:

- `GET /api/snapshots` returns name, App ID, capture time, total count, and sensitive count. It never decrypts values.
- `GET /api/snapshots/{name}` returns a safe entry list. A normal entry has `value`; a sensitive entry has `value: null` and `length`, never its plaintext or ciphertext.
- `POST /api/import/preview` accepts `{ "text": "..." }` and returns parsed keys, inferred sensitivity, and parser warnings without writing a file.
- `POST /api/snapshots` accepts `{ "name": "prod", "app_id": "", "text": "..." }`. It rejects invalid names, empty parses, and existing snapshot names with a conflict response; successful writes create encrypted snapshots only.
- `PUT /api/snapshots/{name}/items/{key}` accepts `{ "value": "...", "secret": true|false|null }`. The response is the safe entry representation. The UI must never prefill the field for an existing sensitive entry.
- `DELETE /api/snapshots/{name}/items/{key}` removes one entry and returns `204 No Content`.
- `GET /api/compare` returns added, removed, and changed values through safe value objects. If either side is sensitive, both sides are represented only by presence and length.
- `POST /api/snapshots/{name}/export` accepts `{ "confirm": true }`. Requests without explicit confirmation return `400`; confirmed requests return a plaintext attachment with `Cache-Control: no-store` and are called only after the browser confirmation dialog.

Errors use this envelope:

```json
{
  "error": {
    "code": "invalid_snapshot_name",
    "message": "snapshot name must start with a letter or number and contain only letters, numbers, dot, dash, or underscore"
  }
}
```

## Task 1: Add Shared Snapshot Safety Primitives

**Files:**
- Modify: `internal/apollo/store.go`
- Modify: `internal/apollo/store_test.go`
- Modify: `internal/apollo/parser.go`
- Modify: `internal/apollo/parser_test.go`

- [ ] **Step 1: Write failing snapshot-name and safe-view tests**

Add the following tests to `internal/apollo/store_test.go`:

```go
func TestValidateSnapshotName(t *testing.T) {
	for _, name := range []string{"prod", "prod-us", "prod.v2", "a_1"} {
		if err := ValidateSnapshotName(name); err != nil {
			t.Errorf("ValidateSnapshotName(%q): %v", name, err)
		}
	}
	for _, name := range []string{"", ".", "../prod", "prod/name", "/tmp/prod", "prod space"} {
		if err := ValidateSnapshotName(name); err == nil {
			t.Errorf("ValidateSnapshotName(%q) succeeded, want error", name)
		}
	}
}

func TestSnapshotVisibleItemsMaskSensitiveValues(t *testing.T) {
	s := NewSnapshot("prod", "")
	mustSet(t, s, "APP_NAME", "merdi", nil)
	mustSet(t, s, "SECRET_TOKEN", "do-not-expose", nil)

	items, err := s.VisibleItems(testKey)
	if err != nil {
		t.Fatal(err)
	}
	if items["APP_NAME"].Value == nil || *items["APP_NAME"].Value != "merdi" {
		t.Fatalf("normal item = %#v", items["APP_NAME"])
	}
	secret := items["SECRET_TOKEN"]
	if !secret.Sensitive || secret.Value != nil || secret.Length != len("do-not-expose") {
		t.Fatalf("sensitive item = %#v", secret)
	}
}
```

Add this test to `internal/apollo/parser_test.go`:

```go
func TestValidateKey(t *testing.T) {
	for _, key := range []string{"APP_NAME", "imile.fs.oss.secret-key", "SOME-KEY"} {
		if err := ValidateKey(key); err != nil {
			t.Errorf("ValidateKey(%q): %v", key, err)
		}
	}
	for _, key := range []string{"", "123BAD", "bad key", "A/B"} {
		if err := ValidateKey(key); err == nil {
			t.Errorf("ValidateKey(%q) succeeded, want error", key)
		}
	}
}
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run:

```sh
go test ./internal/apollo -run 'TestValidateSnapshotName|TestSnapshotVisibleItemsMaskSensitiveValues' -v
```

Expected: compilation failure because `ValidateSnapshotName`, `VisibleItems`, and `ValidateKey` do not exist.

- [ ] **Step 3: Add minimal shared types and implementations**

In `internal/apollo/store.go`, add a strict name regex, a public safe item type, and methods with these signatures:

```go
var snapshotNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

type VisibleItem struct {
	Key       string  `json:"key"`
	Sensitive bool    `json:"sensitive"`
	Value     *string `json:"value"`
	Length    int     `json:"length,omitempty"`
}

func ValidateSnapshotName(name string) error {
	if !snapshotNameRe.MatchString(name) {
		return errors.New("snapshot name must start with a letter or number and contain only letters, numbers, dot, dash, or underscore")
	}
	return nil
}

func (s *Snapshot) VisibleItems(key []byte) (map[string]VisibleItem, error) {
	items := make(map[string]VisibleItem, len(s.Items))
	for name, item := range s.Items {
		value, err := item.DecryptValue(key)
		if err != nil {
			return nil, err
		}
		view := VisibleItem{Key: name, Sensitive: item.Secret}
		if item.Secret {
			view.Length = len(value)
		} else {
			view.Value = &value
		}
		items[name] = view
	}
	return items, nil
}
```

Add `regexp` to the imports. Keep decryption local to this package; do not put ciphertext into `VisibleItem`.

In `internal/apollo/parser.go`, export the existing key rule without changing
parser behavior:

```go
func ValidateKey(key string) error {
	if !keyRe.MatchString(key) {
		return fmt.Errorf("invalid key %q", key)
	}
	return nil
}
```

Update `parseOne` to call `ValidateKey(key)` instead of matching `keyRe`
directly so both paths remain identical.

- [ ] **Step 4: Run the Apollo package tests**

Run:

```sh
go test ./internal/apollo -v
```

Expected: PASS.

- [ ] **Step 5: Apply name validation to CLI path construction**

Change `snapPath` in `internal/cli/cli.go` to return `(string, error)` and call `apollo.ValidateSnapshotName(name)` before joining the directory. Update every CLI caller to return the command-specific `fail(...)` result when name validation fails.

Use this pattern at each command boundary:

```go
path, err := snapPath(dirPath, name)
if err != nil {
	return fail("apollo get: %v", err)
}
s, code := mustSnapshot(key, path, name)
```

Do the equivalent for import, list, set, unset, compare, reveal, edit, and export. This prevents the new UI and existing CLI from disagreeing on safe snapshot names.

- [ ] **Step 6: Add a CLI regression test for path traversal**

Append this to `internal/cli/cli_test.go`:

```go
func TestApolloRejectsUnsafeSnapshotName(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
	if code := Run([]string{"apollo", "list", "../outside", "--dir", t.TempDir()}); code != 1 {
		t.Fatalf("list with unsafe name returned %d, want 1", code)
	}
}
```

- [ ] **Step 7: Run the focused CLI and Apollo tests**

Run:

```sh
go test ./internal/apollo ./internal/cli -run 'TestValidateSnapshotName|TestSnapshotVisibleItemsMaskSensitiveValues|TestValidateKey|TestApolloRejectsUnsafeSnapshotName' -v
```

Expected: PASS.

## Task 2: Build the Testable Loopback UI Server and Safe Read APIs

**Files:**
- Create: `internal/ui/ui.go`
- Create: `internal/ui/ui_test.go`

- [ ] **Step 1: Write failing handler tests for loopback and safe views**

Create `internal/ui/ui_test.go` with a temporary encrypted `prod` snapshot and an injected fixed key. Include these tests:

```go
func TestSnapshotViewMasksSensitiveValue(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\nSECRET_TOKEN = do-not-expose\n")
	r := httptest.NewRequest(http.MethodGet, "/api/snapshots/prod", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if strings.Contains(w.Body.String(), "do-not-expose") {
		t.Fatalf("safe response contains plaintext: %s", w.Body.String())
	}
}

func TestCompareMasksSensitiveValues(t *testing.T) {
	h := newCompareHandler(t)
	r := httptest.NewRequest(http.MethodGet, "/api/compare?from=prod&to=test", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "prod-secret") || strings.Contains(w.Body.String(), "test-secret") {
		t.Fatalf("compare leaked secret: %s", w.Body.String())
	}
}

func TestHandlerRejectsUnsafeSnapshotName(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	r := httptest.NewRequest(http.MethodGet, "/api/snapshots/bad%20name", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
```

Define `newTestHandler` and `newCompareHandler` in the test file. They should write snapshots using `apollo.NewSnapshot` and `Snapshot.Save`, then call `NewHandler(Config{Dir: dir, SnapshotKey: func() ([]byte, error) { return key, nil }})`.

- [ ] **Step 2: Run the handler tests to verify they fail**

Run:

```sh
go test ./internal/ui -run 'TestSnapshotViewMasksSensitiveValue|TestCompareMasksSensitiveValues|TestHandlerRejectsUnsafeSnapshotName' -v
```

Expected: package/build failure because `internal/ui` does not exist.

- [ ] **Step 3: Create the UI config, handler, and JSON helpers**

Create `internal/ui/ui.go` with these public entry points:

```go
package ui

type Config struct {
	Dir         string
	SnapshotKey func() ([]byte, error)
}

func NewHandler(cfg Config) http.Handler

func Start(ctx context.Context, cfg Config, port int, openBrowser bool, out io.Writer) error
```

`NewHandler` must default `cfg.SnapshotKey` to `apollo.SnapshotKey` if nil. Register routes through a private `http.ServeMux`. Every `/api/` response must set `Cache-Control: no-store` before writing JSON.

Implement a shared JSON error writer:

```go
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
```

Implement `GET /api/snapshots` without decrypting item values. Implement `GET /api/snapshots/{name}` using `Snapshot.VisibleItems`. Return its map values as a key-sorted JSON array to keep UI order deterministic.

Implement `GET /api/compare` by decrypting only inside the handler, then projecting each change through a safe helper. Use the following data shape:

```go
type SafeValue struct {
	Present   bool    `json:"present"`
	Sensitive bool    `json:"sensitive"`
	Value     *string `json:"value"`
	Length    int     `json:"length,omitempty"`
}

type SafeChange struct {
	Key  string    `json:"key"`
	Kind string    `json:"kind"`
	Old  SafeValue `json:"old"`
	New  SafeValue `json:"new"`
}
```

For `added` and `removed`, set the missing side to `Present: false`. If `Change.Secret` is true, set both populated sides to `Sensitive: true`, omit values, and retain only length.

- [ ] **Step 4: Add loopback-only listener startup**

In `Start`, reject ports outside `0..65535`, then bind only with:

```go
listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
if err != nil {
	return err
}
defer listener.Close()

url := "http://" + listener.Addr().String()
fmt.Fprintf(out, "ai-tools UI available at %s\n", url)
```

When `openBrowser` is true, invoke a private `openURL(url)` after the listener exists. Serve with `http.Server{Handler: NewHandler(cfg)}` and shut down when `ctx.Done()` fires. Do not add a `0.0.0.0` option.

- [ ] **Step 5: Add OS-specific browser launch files**

Create `browser_darwin.go`:

```go
//go:build darwin

package ui

import "os/exec"

func openURL(url string) error { return exec.Command("open", url).Start() }
```

Create `browser_linux.go` using `xdg-open`, and `browser_other.go` with `func openURL(string) error { return nil }` behind `//go:build !darwin && !linux`.

- [ ] **Step 6: Run the UI package tests**

Run:

```sh
go test ./internal/ui -v
```

Expected: PASS.

## Task 3: Add Import, Item Mutation, and Confirmed Export APIs

**Files:**
- Modify: `internal/ui/ui.go`
- Modify: `internal/ui/ui_test.go`

- [ ] **Step 1: Write failing mutation and export tests**

Add tests for import preview, duplicate-safe creation, sensitive replacement, deletion, and export confirmation:

```go
func TestImportPreviewReturnsWarningsWithoutWriting(t *testing.T) {
	h := newEmptyHandler(t)
	body := strings.NewReader(`{"text":"A = 1B = 2\n"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/import/preview", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "warnings") {
		t.Fatalf("preview = %d %s", w.Code, w.Body.String())
	}
}

func TestCreateSnapshotRejectsDuplicateName(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	body := strings.NewReader(`{"name":"prod","text":"APP_NAME = other"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSensitiveItemDoesNotReturnPlaintext(t *testing.T) {
	h := newTestHandler(t, "SECRET_TOKEN = old-secret\n")
	body := strings.NewReader(`{"value":"new-secret","secret":true}`)
	r := httptest.NewRequest(http.MethodPut, "/api/snapshots/prod/items/SECRET_TOKEN", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "new-secret") {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
}

func TestExportRequiresConfirmation(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/export", strings.NewReader(`{"confirm":false}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
```

- [ ] **Step 2: Run mutation tests to verify they fail**

Run:

```sh
go test ./internal/ui -run 'TestImportPreviewReturnsWarningsWithoutWriting|TestCreateSnapshotRejectsDuplicateName|TestUpdateSensitiveItemDoesNotReturnPlaintext|TestExportRequiresConfirmation' -v
```

Expected: FAIL because the routes are not registered.

- [ ] **Step 3: Implement import preview and create routes**

Add JSON request structs:

```go
type previewRequest struct { Text string `json:"text"` }
type createSnapshotRequest struct {
	Name  string `json:"name"`
	AppID string `json:"app_id"`
	Text  string `json:"text"`
}
```

`POST /api/import/preview` calls `apollo.ParseKV`, returns `items` as `[{"key":"...","sensitive":true}]`, and returns `warnings`. Reject empty parsed input with `400 empty_import`.

`POST /api/snapshots` validates the name, parses the input, checks `os.Stat(filepath.Join(cfg.Dir, name+".json"))`, returns `409 snapshot_exists` if present, encrypts every parsed key/value using `Snapshot.Set`, saves through `Snapshot.Save`, and returns `201` with the summary metadata.

- [ ] **Step 4: Implement item mutation routes**

Use these request fields:

```go
type updateItemRequest struct {
	Value  string `json:"value"`
	Secret *bool  `json:"secret"`
}
```

For `PUT /api/snapshots/{name}/items/{key}`:

1. Validate snapshot name and key with the existing Apollo key rules. Export a small `apollo.ValidateKey` wrapper around the existing `keyRe` instead of duplicating the regex in `internal/ui`.
2. Load snapshot and key using the configured key provider.
3. Call `Snapshot.Set` and `Snapshot.Save`.
4. Return a `VisibleItem` for the updated key. Sensitive values must have `value: null`.

For `DELETE`, load, call `Snapshot.Delete`, return `404 key_not_found` when false, save, and write `204`.

- [ ] **Step 5: Implement confirmed export**

Decode this request shape:

```go
type exportRequest struct { Confirm bool `json:"confirm"` }
```

Reject `Confirm == false` with `400 export_confirmation_required`. On confirmation, load and decrypt every item, sort keys, write `KEY = value\n` bytes as:

```go
w.Header().Set("Cache-Control", "no-store")
w.Header().Set("Content-Type", "text/plain; charset=utf-8")
w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+".txt"))
```

Do not log request or response bodies.

- [ ] **Step 6: Run all UI handler tests**

Run:

```sh
go test ./internal/ui -v
```

Expected: PASS.

## Task 4: Embed the Approved V6 Web Interface

**Files:**
- Create: `internal/ui/static/index.html`
- Create: `internal/ui/static/app.css`
- Create: `internal/ui/static/app.js`
- Modify: `internal/ui/ui.go`

- [ ] **Step 1: Add a failing static shell test**

Add this test to `internal/ui/ui_test.go`:

```go
func TestRootServesEmbeddedWorkspace(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `id="app"`) {
		t.Fatalf("workspace shell missing: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run the static shell test to verify it fails**

Run:

```sh
go test ./internal/ui -run TestRootServesEmbeddedWorkspace -v
```

Expected: FAIL because `/` is not yet served.

- [ ] **Step 3: Build the semantic HTML shell**

Create `internal/ui/static/index.html` with:

- An `<aside>` containing the product mark, `data-action="import"` button, `#recent-work`, `#snapshot-list`, and local workspace identity block.
- A `<header>` containing `#breadcrumb` and the overflow action button.
- A `<main id="app">` with `#hero`, `#snapshot-context`, `#config-search`, `#config-table`, `#compare-panel`, and `#next-steps`.
- Accessible `<dialog>` elements for import preview/confirmation, edit-or-replace entry, delete confirmation, comparison target selection, export confirmation, and inline error display.
- `<script src="/app.js" defer></script>` and `<link rel="stylesheet" href="/app.css">`.

Do not place real snapshot values in the initial HTML. All content must be fetched from the API after page load.

- [ ] **Step 4: Implement the approved v6 CSS**

Create `internal/ui/static/app.css` using the approved palette and layout:

```css
:root {
  --bg: #fbfaf8;
  --rail: #ffffff;
  --panel: #ffffff;
  --line: #eae6df;
  --line-soft: #f3f0eb;
  --ink: #262624;
  --muted: #78736b;
  --accent: #d97757;
  --accent-soft: #fbeae4;
  --green: #3f7d5c;
}
```

Match v6's visual behavior:

- 248px white rail on desktop; hide it below 820px.
- Warm off-white canvas and compact 54px top bar.
- Serif hero title, deep charcoal text, coral active navigation and diff marks.
- Search control with icon, monospace keys/values, right-aligned values, very light row rules, and a row hover.
- Sensitive values render masked dots plus a small length label.
- Sticky desktop next-steps card, static on mobile.
- Use low-contrast borders and no decorative drop shadows.

- [ ] **Step 5: Implement the browser application**

Create `internal/ui/static/app.js` with a single state object:

```js
const state = { snapshots: [], active: null, snapshot: null, compare: null, recentWork: [] };
```

Add an `api(path, options)` helper that calls `fetch`, parses JSON error envelopes, and throws `error.message` for non-2xx responses.

Implement these browser behaviors:

1. On load, fetch `/api/snapshots`, render the rail, select the first snapshot, then fetch its safe view.
   Maintain `state.recentWork` only in memory. Add messages such as `导入 <name>`
   and `<from> 与 <to> 配置对比` after successful UI actions, render their local
   timestamps in the rail, and discard the list on reload. Do not use local
   storage, session storage, cookies, or an API endpoint for this list.
2. Render normal values as text and sensitive values as `••••••••••••` plus the returned length. Never attempt to infer or cache plaintext sensitive values.
3. Filter entries in memory by key and normal visible value as the user types in the search control.
4. A normal entry opens an edit dialog with its existing visible value. A sensitive entry opens a replace dialog with an empty password-style field and a visible warning that the current value cannot be displayed.
5. Save uses `PUT`; delete uses `DELETE` after a confirmation dialog; both refresh the active snapshot.
6. Import first calls `/api/import/preview`, renders warnings and inferred sensitive flags, then submits `POST /api/snapshots` only after confirmation.
7. Compare opens a target selector, fetches `/api/compare`, and renders compact `+`, `-`, and `~` rows. Sensitive entries render presence, changed state, and length only.
8. Export opens a warning dialog. Only its explicit confirmation calls `POST /api/snapshots/{name}/export` with `{ confirm: true }`, receives the attachment Blob, and triggers a browser download without rendering the returned plaintext in the page.
9. Render API errors inside the active canvas/dialog; do not use `console.log` for API payloads or errors that can include user-supplied data.

- [ ] **Step 6: Embed and serve static files**

Add this to `internal/ui/ui.go`:

```go
//go:embed static/index.html static/app.css static/app.js
var staticFiles embed.FS
```

Serve `/`, `/app.css`, and `/app.js` from the embedded files with correct content types. Static files must not be served with snapshot data; set `Cache-Control: no-store` on the HTML document and APIs, and `Cache-Control: private, max-age=3600` on CSS/JS.

- [ ] **Step 7: Run UI tests and manually inspect the shell**

Run:

```sh
go test ./internal/ui -v
go run . ui --dir "$(mktemp -d)" --no-open
```

Expected: tests pass; the command prints a `http://127.0.0.1:<port>` URL and serves the blank-state workspace without binding to a non-loopback address. Stop the manual server with `Ctrl-C`.

## Task 5: Register the `ui` CLI Command and Document It

**Files:**
- Modify: `internal/cli/cli.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `README.md`

- [ ] **Step 1: Write failing CLI tests for UI argument validation**

Add tests to `internal/cli/cli_test.go` for a non-starting flag-validation helper. Extract a small pure helper from `runUI` if necessary:

```go
func TestUIOptions(t *testing.T) {
	port, err := parseUIPort("0")
	if err != nil || port != 0 {
		t.Fatalf("parseUIPort(0) = %d, %v", port, err)
	}
	if _, err := parseUIPort("70000"); err == nil {
		t.Fatal("parseUIPort(70000) succeeded")
	}
}
```

- [ ] **Step 2: Run the CLI test to verify it fails**

Run:

```sh
go test ./internal/cli -run TestUIOptions -v
```

Expected: compilation failure because `parseUIPort` does not exist.

- [ ] **Step 3: Register and implement `ui`**

In `Run`, add:

```go
case "ui":
	return runUI(args[1:])
```

Add usage text:

```text
ai-tools ui [--dir <dir>] [--port <port>] [--no-open]
```

Implement `runUI` with `flag.ContinueOnError`, `--dir`, `--port` (default `0`), and `--no-open`. Resolve the directory through existing `snapDir`, validate port with `parseUIPort`, then call:

```go
return ui.Start(context.Background(), ui.Config{Dir: dirPath}, port, !*noOpen, os.Stdout)
```

`runUI` should return `0` only on clean server shutdown and call `fail("ui: %v", err)` for startup errors. Add `"ai-tools/internal/ui"`, `context`, and any required imports.

Update zsh, bash, and fish completion strings so `ui` is suggested as a top-level command.

- [ ] **Step 4: Run focused CLI tests**

Run:

```sh
go test ./internal/cli -run 'TestUIOptions|TestApolloRejectsUnsafeSnapshotName' -v
```

Expected: PASS.

- [ ] **Step 5: Document the Web UI**

Add a `## 本地 Web UI` section to `README.md` after the interactive-mode section:

```md
## 本地 Web UI

```sh
ai-tools ui
ai-tools ui --dir /path/to/snapshots --port 8080
ai-tools ui --no-open
```

- 仅监听 `127.0.0.1`，不会暴露到局域网。
- 浏览、搜索与环境对比默认遮罩敏感值；Web UI 首版不提供 reveal。
- 导出会先显示明文风险确认，确认后仅作为本地下载生成。
- 浏览器端不持久化快照内容，API 响应使用 `Cache-Control: no-store`。
```

Also update the command list and completion documentation to include `ui`.

- [ ] **Step 6: Run all automated checks**

Run:

```sh
gofmt -w internal/apollo/store.go internal/apollo/store_test.go internal/apollo/parser.go internal/apollo/parser_test.go internal/cli/cli.go internal/cli/cli_test.go internal/ui/*.go
go test ./...
go vet ./...
```

Expected: all commands exit `0`.

- [ ] **Step 7: Perform a focused manual safety check**

Run the UI with temporary snapshots whose sensitive values are distinctive strings, then verify in browser developer tools or an HTTP client that:

1. `GET /api/snapshots/prod` contains neither sensitive plaintext nor ciphertext.
2. `GET /api/compare?from=prod&to=test` contains neither old nor new sensitive plaintext.
3. The sensitive edit dialog has an empty input.
4. Export does not begin until the confirmation dialog is accepted.
5. The server URL begins with `http://127.0.0.1:`.

## Completion Notes

- Do not create commits: the repository has no initial commit and the user did not request one.
- Do not include `.superpowers/brainstorm/` preview artifacts in implementation changes. They are design-session artifacts, not product assets.
- Before declaring completion, run the verification commands above and report their actual outcomes.
