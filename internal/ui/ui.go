// Package ui serves a loopback-only web UI over encrypted Apollo snapshots.
// Every /api/ response is marked Cache-Control: no-store and sensitive values
// are never returned in plaintext.
package ui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ai-tools/internal/apollo"
	"ai-tools/internal/app"
)

//go:embed static/index.html static/app.css static/app.js
var staticFiles embed.FS

type Config struct {
	Dir         string
	SnapshotKey func() ([]byte, error)
	// Token, when non-empty, gates every state-changing /api request
	// (POST/PUT/DELETE — imports, edits, reveals, exports, deletes).
	// Start() generates one automatically so a local attacker process
	// (e.g. an AI agent) cannot dump plaintext from the API without it.
	Token string
}

func NewHandler(cfg Config) http.Handler {
	if cfg.SnapshotKey == nil {
		cfg.SnapshotKey = apollo.SnapshotKey
	}
	h := &handler{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.serveStatic)
	mux.HandleFunc("/api/snapshots", h.listSnapshots)
	mux.HandleFunc("/api/snapshots/", h.snapshotView)
	mux.HandleFunc("/api/compare", h.compare)
	mux.HandleFunc("/api/compare/multi", h.compareMulti)
	mux.HandleFunc("/api/compare/key", h.compareKey)
	mux.HandleFunc("/api/import/preview", h.importPreview)
	mux.HandleFunc("/api/init", h.initKey)
	mux.HandleFunc("/api/key", h.keyStatus)
	mux.HandleFunc("/api/aes/gen-key", h.aesGenKey)
	mux.HandleFunc("/api/aes/transform", h.aesTransform)
	mux.HandleFunc("/api/aes/config", h.aesConfig)
	return checkOrigin(checkToken(cfg.Token, mux))
}

// checkToken requires a valid token on every state-changing request when a
// token is configured. Read-only GET endpoints (list/view/compare, all
// masked) stay open so the frontend can load without a token; the plaintext
// and mutating endpoints are all POST/PUT/DELETE and are gated.
func checkToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Method != http.MethodGet && !validToken(token, r) {
			writeAPIError(w, http.StatusUnauthorized, "auth_required", "invalid or missing auth token (open the URL printed by 'ai-tools ui')")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validToken(token string, r *http.Request) bool {
	t := r.Header.Get("X-Auth-Token")
	if t == "" {
		t = r.URL.Query().Get("t")
	}
	return subtle.ConstantTimeCompare([]byte(t), []byte(token)) == 1
}

// checkOrigin rejects requests whose Origin header does not match the served
// host, mitigating browser-based cross-site requests (CSRF). Same-origin
// fetches and CLI/curl clients (no Origin header) are unaffected.
func checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "null" {
			writeAPIError(w, http.StatusForbidden, "origin_not_allowed", "null origin rejected")
			return
		}
		if origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				writeAPIError(w, http.StatusForbidden, "origin_not_allowed", "cross-origin request rejected")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// serveStatic serves the embedded frontend. Every response (HTML, CSS and JS
// alike) is marked no-store so browsers never persist the workspace or its
// assets. API routes are registered with more specific patterns and take
// precedence over the "/" catch-all.
func (h *handler) serveStatic(w http.ResponseWriter, r *http.Request) {
	var file, ctype, cache string
	switch r.URL.Path {
	case "/":
		file, ctype, cache = "static/index.html", "text/html; charset=utf-8", "no-store"
	case "/app.css":
		file, ctype, cache = "static/app.css", "text/css; charset=utf-8", "no-store"
	case "/app.js":
		file, ctype, cache = "static/app.js", "text/javascript; charset=utf-8", "no-store"
	default:
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	data, err := staticFiles.ReadFile(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", cache)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

type handler struct {
	cfg Config
}

type snapshotSummary struct {
	Name       string `json:"name"`
	AppID      string `json:"app_id"`
	CapturedAt string `json:"captured_at"`
	Total      int    `json:"total"`
	Sensitive  int    `json:"sensitive"`
}

func (h *handler) listSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		h.createSnapshot(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	refs, err := apollo.ListSnapshots(h.cfg.Dir)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_list_failed", err.Error())
		return
	}
	summaries := make([]snapshotSummary, 0, len(refs))
	for _, ref := range refs {
		s, err := apollo.Load(apollo.SnapPath(h.cfg.Dir, ref.Name, ref.AppID))
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "snapshot_load_failed", err.Error())
			return
		}
		sensitive := 0
		for _, it := range s.Items {
			if it.Secret {
				sensitive++
			}
		}
		summaries = append(summaries, snapshotSummary{
			Name:       s.Meta.Name,
			AppID:      s.Meta.AppID,
			CapturedAt: s.Meta.CapturedAt,
			Total:      len(s.Items),
			Sensitive:  sensitive,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": summaries})
}

func (h *handler) snapshotView(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/snapshots/")
	if rest == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", "malformed snapshot path")
		return
	}
	parts := strings.Split(rest, "/")
	switch {
	case len(parts) == 1:
		switch r.Method {
		case http.MethodGet:
			h.snapshotViewName(w, r, parts[0])
		case http.MethodDelete:
			h.deleteSnapshot(w, r, parts[0])
		default:
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	case len(parts) == 2 && parts[1] == "export":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		h.exportSnapshot(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "reveal":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		h.reveal(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "edit":
		switch r.Method {
		case http.MethodPost:
			h.editLoad(w, r, parts[0])
		case http.MethodPut:
			h.editApply(w, r, parts[0])
		default:
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	case len(parts) == 3 && parts[1] == "items":
		switch r.Method {
		case http.MethodPut:
			h.updateItem(w, r, parts[0], parts[2])
		case http.MethodDelete:
			h.deleteItem(w, r, parts[0], parts[2])
		default:
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", "malformed snapshot path")
	}
}

func (h *handler) snapshotViewName(w http.ResponseWriter, r *http.Request, name string) {
	appID := r.URL.Query().Get("appid")
	if err := apollo.ValidateSnapshotName(name); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", err.Error())
		return
	}
	key, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'ai-tools apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	s, err := apollo.Load(apollo.SnapPath(h.cfg.Dir, name, appID))
	if err != nil {
		if os.IsNotExist(err) {
			writeAPIError(w, http.StatusNotFound, "snapshot_not_found", fmt.Sprintf("snapshot %q not found", name))
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "snapshot_load_failed", err.Error())
		return
	}
	items, err := s.VisibleItems(key)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_decrypt_failed", err.Error())
		return
	}
	sorted := make([]apollo.VisibleItem, 0, len(items))
	for _, it := range items {
		sorted = append(sorted, it)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	writeJSON(w, http.StatusOK, map[string]any{"name": s.Meta.Name, "items": sorted})
}

type previewRequest struct {
	Text string `json:"text"`
}

type previewItem struct {
	Key       string `json:"key"`
	Sensitive bool   `json:"sensitive"`
}

type createSnapshotRequest struct {
	Name  string `json:"name"`
	AppID string `json:"app_id"`
	Text  string `json:"text"`
}

type updateItemRequest struct {
	Value  string `json:"value"`
	Secret *bool  `json:"secret"`
}

type exportRequest struct {
	Confirm bool `json:"confirm"`
}

func (h *handler) importPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req previewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	items, warnings := apollo.ParseKV(req.Text)
	if len(items) == 0 {
		writeAPIError(w, http.StatusBadRequest, "empty_import", "no config items found")
		return
	}
	out := make([]previewItem, 0, len(items))
	for _, it := range items {
		out = append(out, previewItem{Key: it.Key, Sensitive: apollo.IsSensitive(it.Key)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "warnings": warnings})
}

func (h *handler) createSnapshot(w http.ResponseWriter, r *http.Request) {
	var req createSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if err := apollo.ValidateSnapshotName(req.Name); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", err.Error())
		return
	}
	if err := apollo.ValidateAppID(req.AppID); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_app_id", err.Error())
		return
	}
	items, _ := apollo.ParseKV(req.Text)
	if len(items) == 0 {
		writeAPIError(w, http.StatusBadRequest, "empty_import", "no config items found")
		return
	}
	path := apollo.SnapPath(h.cfg.Dir, req.Name, req.AppID)
	if _, err := os.Stat(path); err == nil {
		writeAPIError(w, http.StatusConflict, "snapshot_exists", fmt.Sprintf("snapshot %q (appid %s) already exists", req.Name, req.AppID))
		return
	} else if !os.IsNotExist(err) {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_create_failed", err.Error())
		return
	}
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'ai-tools apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	s := apollo.NewSnapshot(req.Name, req.AppID)
	sensitive := 0
	for _, it := range items {
		if apollo.IsSensitive(it.Key) {
			sensitive++
		}
		if err := s.Set(snapKey, it.Key, it.Value, nil); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "snapshot_create_failed", err.Error())
			return
		}
	}
	if err := s.Save(path); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name, "app_id": req.AppID, "total": len(items), "sensitive": sensitive})
}

func (h *handler) loadSnapshotTarget(w http.ResponseWriter, name, appID string) (*apollo.Snapshot, []byte, bool) {
	if err := apollo.ValidateSnapshotName(name); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", err.Error())
		return nil, nil, false
	}
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'ai-tools apollo init' or set %s", err, apollo.EnvKey))
		return nil, nil, false
	}
	s, err := apollo.Load(apollo.SnapPath(h.cfg.Dir, name, appID))
	if err != nil {
		if os.IsNotExist(err) {
			writeAPIError(w, http.StatusNotFound, "snapshot_not_found", fmt.Sprintf("snapshot %q not found", name))
			return nil, nil, false
		}
		writeAPIError(w, http.StatusInternalServerError, "snapshot_load_failed", err.Error())
		return nil, nil, false
	}
	return s, snapKey, true
}

func (h *handler) loadItemTarget(w http.ResponseWriter, name, appID, key string) (*apollo.Snapshot, []byte, bool) {
	if err := apollo.ValidateKey(key); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_key", err.Error())
		return nil, nil, false
	}
	return h.loadSnapshotTarget(w, name, appID)
}

func (h *handler) updateItem(w http.ResponseWriter, r *http.Request, name, itemKey string) {
	appID := r.URL.Query().Get("appid")
	s, snapKey, ok := h.loadItemTarget(w, name, appID, itemKey)
	if !ok {
		return
	}
	var req updateItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if err := s.Set(snapKey, itemKey, req.Value, req.Secret); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "item_update_failed", err.Error())
		return
	}
	if err := s.Save(apollo.SnapPath(h.cfg.Dir, name, appID)); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "item_update_failed", err.Error())
		return
	}
	visible, err := s.VisibleItems(snapKey)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_decrypt_failed", err.Error())
		return
	}
	it, ok := visible[itemKey]
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "item_update_failed", "updated item not found")
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (h *handler) deleteItem(w http.ResponseWriter, r *http.Request, name, itemKey string) {
	appID := r.URL.Query().Get("appid")
	s, _, ok := h.loadItemTarget(w, name, appID, itemKey)
	if !ok {
		return
	}
	if !s.Delete(itemKey) {
		writeAPIError(w, http.StatusNotFound, "key_not_found", fmt.Sprintf("item %q not found", itemKey))
		return
	}
	if err := s.Save(apollo.SnapPath(h.cfg.Dir, name, appID)); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "item_delete_failed", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) exportSnapshot(w http.ResponseWriter, r *http.Request, name string) {
	appID := r.URL.Query().Get("appid")
	s, snapKey, ok := h.loadSnapshotTarget(w, name, appID)
	if !ok {
		return
	}
	var req exportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if !req.Confirm {
		writeAPIError(w, http.StatusBadRequest, "export_confirmation_required", "confirmation required for plaintext export")
		return
	}
	keys := make([]string, 0, len(s.Items))
	for k := range s.Items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf strings.Builder
	for _, k := range keys {
		v, err := s.Items[k].DecryptValue(snapKey)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "snapshot_decrypt_failed", err.Error())
			return
		}
		buf.WriteString(k)
		buf.WriteString(" = ")
		buf.WriteString(v)
		buf.WriteByte('\n')
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", apollo.FileName(name, appID)))
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, buf.String())
}

type SafeValue struct {
	Present     bool    `json:"present"`
	Sensitive   bool    `json:"sensitive"`
	Value       *string `json:"value"`
	Length      int     `json:"length,omitempty"`
	Fingerprint string  `json:"fingerprint,omitempty"`
}

type SafeChange struct {
	Key  string    `json:"key"`
	Kind string    `json:"kind"`
	Old  SafeValue `json:"old"`
	New  SafeValue `json:"new"`
}

func (h *handler) compare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	fromAppID := r.URL.Query().Get("from_appid")
	toAppID := r.URL.Query().Get("to_appid")
	for _, name := range []string{from, to} {
		if err := apollo.ValidateSnapshotName(name); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", err.Error())
			return
		}
	}
	a, err := h.loadSnapshotRef(from, fromAppID)
	if err != nil {
		h.loadError(w, from, err)
		return
	}
	b, err := h.loadSnapshotRef(to, toAppID)
	if err != nil {
		h.loadError(w, to, err)
		return
	}
	key, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'ai-tools apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	changes := make([]SafeChange, 0, len(a.Items)+len(b.Items))
	diffs, err := a.Diff(b, key)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_decrypt_failed", err.Error())
		return
	}
	for _, c := range diffs {
		sc := SafeChange{Key: c.Key, Kind: c.Kind}
		switch c.Kind {
		case "added":
			sc.New = safeValue(true, c.New, c.Secret)
		case "removed":
			sc.Old = safeValue(true, c.Old, c.Secret)
		default:
			sc.Old = safeValue(true, c.Old, c.Secret)
			sc.New = safeValue(true, c.New, c.Secret)
		}
		changes = append(changes, sc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "changes": changes})
}

func (h *handler) loadSnapshotRef(name, appID string) (*apollo.Snapshot, error) {
	return apollo.Load(apollo.SnapPath(h.cfg.Dir, name, appID))
}

type multiCompareRequest struct {
	Refs []apollo.SnapshotRef `json:"refs"`
}

// compareValue projects one key of a loaded snapshot into a SafeValue.
func compareValue(s *apollo.Snapshot, key []byte, k string) SafeValue {
	it, ok := s.Items[k]
	if !ok {
		return SafeValue{Present: false}
	}
	v, err := it.DecryptValue(key)
	if err != nil {
		return SafeValue{Present: true, Sensitive: it.Secret}
	}
	return safeValue(true, v, it.Secret)
}

// compareMulti returns a horizontal comparison: one row per key (sorted),
// one column per requested snapshot ref. Missing keys are present:false.
func (h *handler) compareMulti(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req multiCompareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if len(req.Refs) < 2 {
		writeAPIError(w, http.StatusBadRequest, "invalid_refs", "at least two snapshots are required")
		return
	}
	for _, ref := range req.Refs {
		if err := apollo.ValidateSnapshotName(ref.Name); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", err.Error())
			return
		}
		if ref.AppID != "" {
			if err := apollo.ValidateAppID(ref.AppID); err != nil {
				writeAPIError(w, http.StatusBadRequest, "invalid_app_id", err.Error())
				return
			}
		}
	}
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'ai-tools apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	snapshots := make([]*apollo.Snapshot, 0, len(req.Refs))
	refsOut := make([]map[string]any, 0, len(req.Refs))
	for _, ref := range req.Refs {
		s, err := h.loadSnapshotRef(ref.Name, ref.AppID)
		if err != nil {
			h.loadError(w, ref.Name, err)
			return
		}
		sensitive := 0
		for _, it := range s.Items {
			if it.Secret {
				sensitive++
			}
		}
		refsOut = append(refsOut, map[string]any{"name": ref.Name, "app_id": ref.AppID, "total": len(s.Items), "sensitive": sensitive})
		snapshots = append(snapshots, s)
	}
	keySet := map[string]bool{}
	for _, s := range snapshots {
		for k := range s.Items {
			keySet[k] = true
		}
	}
	sorted := make([]string, 0, len(keySet))
	for k := range keySet {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	rows := make([]map[string]any, 0, len(sorted))
	for _, k := range sorted {
		values := make([]SafeValue, 0, len(snapshots))
		for _, s := range snapshots {
			values = append(values, compareValue(s, snapKey, k))
		}
		rows = append(rows, map[string]any{"key": k, "values": values})
	}
	writeJSON(w, http.StatusOK, map[string]any{"refs": refsOut, "rows": rows})
}

// compareKey returns the value of a single key across snapshots. An optional
// "appid" query parameter filters to snapshots with the same app id (empty
// matches legacy snapshots without one); when absent, every snapshot matches.
func (h *handler) compareKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	keyName := r.URL.Query().Get("key")
	if err := apollo.ValidateKey(keyName); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_key", err.Error())
		return
	}
	filterAppID, filterSet := r.URL.Query()["appid"]
	if filterSet && filterAppID[0] != "" {
		if err := apollo.ValidateAppID(filterAppID[0]); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_app_id", err.Error())
			return
		}
	}
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'ai-tools apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	refs, err := apollo.ListSnapshots(h.cfg.Dir)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_list_failed", err.Error())
		return
	}
	rows := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		if filterSet && ref.AppID != filterAppID[0] {
			continue
		}
		s, err := apollo.Load(apollo.SnapPath(h.cfg.Dir, ref.Name, ref.AppID))
		if err != nil {
			continue
		}
		rows = append(rows, map[string]any{
			"name":   ref.Name,
			"app_id": ref.AppID,
			"value":  compareValue(s, snapKey, keyName),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": keyName, "rows": rows})
}

func (h *handler) loadError(w http.ResponseWriter, name string, err error) {
	if os.IsNotExist(err) {
		writeAPIError(w, http.StatusNotFound, "snapshot_not_found", fmt.Sprintf("snapshot %q not found", name))
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "snapshot_load_failed", err.Error())
}

func (h *handler) deleteSnapshot(w http.ResponseWriter, r *http.Request, name string) {
	appID := r.URL.Query().Get("appid")
	if err := apollo.ValidateSnapshotName(name); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", err.Error())
		return
	}
	if err := apollo.ValidateAppID(appID); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_app_id", err.Error())
		return
	}
	ok, err := app.Remove(h.cfg.Dir, name, appID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_delete_failed", err.Error())
		return
	}
	if !ok {
		writeAPIError(w, http.StatusNotFound, "snapshot_not_found", fmt.Sprintf("snapshot %q (appid %s) not found", name, appID))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func safeValue(present bool, plain string, sensitive bool) SafeValue {
	sv := SafeValue{Present: present}
	if !present {
		return sv
	}
	sv.Sensitive = sensitive
	if sensitive {
		sv.Length = len(plain)
		// A truncated SHA-256 lets the user tell whether two masked values are
		// truly identical without exposing the plaintext. Quotes are
		// normalized so `"abc"` and `abc` share a fingerprint.
		sum := sha256.Sum256([]byte(apollo.NormValue(plain)))
		sv.Fingerprint = hex.EncodeToString(sum[:8])
	} else {
		v := plain
		sv.Value = &v
	}
	return sv
}

type initRequest struct {
	Force bool `json:"force"`
}

func (h *handler) keyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	_, err := h.cfg.SnapshotKey()
	writeJSON(w, http.StatusOK, map[string]any{"available": err == nil})
}

func (h *handler) initKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req initRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if !req.Force {
		if _, err := h.cfg.SnapshotKey(); err == nil {
			writeAPIError(w, http.StatusConflict, "key_exists", "a snapshot key already exists")
			return
		}
	}
	if err := app.InitKey(req.Force); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "key_init_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

type revealRequest struct {
	Targets []string `json:"targets"`
	Confirm bool     `json:"confirm"`
}

func (h *handler) reveal(w http.ResponseWriter, r *http.Request, name string) {
	appID := r.URL.Query().Get("appid")
	var req revealRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if !req.Confirm {
		writeAPIError(w, http.StatusBadRequest, "confirm_required", "confirmation required for plaintext reveal")
		return
	}
	if len(req.Targets) == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_key", "no targets provided")
		return
	}
	if err := apollo.ValidateSnapshotName(name); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", err.Error())
		return
	}
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'ai-tools apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	plain, err := app.Reveal(h.cfg.Dir, name, appID, snapKey, req.Targets, "", "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "decrypt_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": plain})
}

type editLoadRequest struct {
	Confirm bool `json:"confirm"`
}

func (h *handler) editLoad(w http.ResponseWriter, r *http.Request, name string) {
	appID := r.URL.Query().Get("appid")
	var req editLoadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if !req.Confirm {
		writeAPIError(w, http.StatusBadRequest, "confirm_required", "confirmation required for plaintext edit")
		return
	}
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'ai-tools apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	text, err := app.EditLoad(h.cfg.Dir, name, appID, snapKey)
	if err != nil {
		h.loadError(w, name, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": text})
}

type editApplyRequest struct {
	Text string `json:"text"`
}

func (h *handler) editApply(w http.ResponseWriter, r *http.Request, name string) {
	appID := r.URL.Query().Get("appid")
	var req editApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if req.Text == "" {
		writeAPIError(w, http.StatusBadRequest, "empty_import", "no config items found")
		return
	}
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'ai-tools apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	n, err := app.EditApply(h.cfg.Dir, name, appID, snapKey, req.Text)
	if err != nil {
		if strings.Contains(err.Error(), "no key/value entries") {
			writeAPIError(w, http.StatusBadRequest, "empty_import", err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "snapshot_edit_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "total": n})
}

type aesGenKeyRequest struct {
	Bytes   int `json:"bytes"`
	IVBytes int `json:"iv_bytes"`
}

func (h *handler) aesGenKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req aesGenKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	key, iv, err := app.GenKey(req.Bytes, req.IVBytes)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_aes_params", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "iv": iv})
}

type aesTransformRequest struct {
	Op   string `json:"op"`
	Key  string `json:"key"`
	IV   string `json:"iv"`
	Text string `json:"text"`
}

func (h *handler) aesTransform(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req aesTransformRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if req.Key == "" || req.IV == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_aes_params", "key and iv are required")
		return
	}
	var (
		out string
		err error
	)
	switch req.Op {
	case "encrypt":
		out, err = app.Encrypt(req.Key, req.IV, req.Text)
	case "decrypt":
		out, err = app.Decrypt(req.Key, req.IV, req.Text)
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_aes_params", "op must be encrypt or decrypt")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "aes_op_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": out})
}

type aesConfigRequest struct {
	Key string `json:"key"`
	IV  string `json:"iv"`
}

func (h *handler) aesConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := app.AESConfigLoad()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "aes_config_io", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"key": c.Key, "iv": c.IV, "path": app.AESConfigPath()})
	case http.MethodPut:
		var req aesConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
			return
		}
		if req.Key == "" || req.IV == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_aes_params", "key and iv are required")
			return
		}
		if err := app.AESConfigSave(req.Key, req.IV); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "aes_config_io", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case http.MethodDelete:
		if err := app.AESConfigClear(); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "aes_config_io", err.Error())
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func Start(ctx context.Context, cfg Config, port int, openBrowser bool, out io.Writer) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("port %d out of range 0..65535", port)
	}
	listener, err := listenLoopback(port)
	if err != nil {
		return err
	}
	defer listener.Close()

	// Generate a per-run token and put it in the URL. State-changing /api
	// requests (imports, edits, reveals, exports, deletes) require it, so a
	// local process like an AI agent cannot dump plaintext via the API
	// without the token the user sees in their terminal.
	if cfg.Token == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		cfg.Token = hex.EncodeToString(b)
	}

	url := "http://" + listener.Addr().String() + "/?t=" + cfg.Token
	fmt.Fprintf(out, "ai-tools UI available at %s\n", url)
	if openBrowser {
		if err := openURL(url); err != nil {
			fmt.Fprintf(out, "failed to open browser: %v\n", err)
		}
	}

	srv := &http.Server{Handler: NewHandler(cfg)}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// listenLoopback binds 127.0.0.1. A fixed port that is already in use rolls
// forward to the next free port so repeated `ai-tools ui` runs keep working;
// port 0 requests an ephemeral port.
func listenLoopback(port int) (net.Listener, error) {
	if port == 0 {
		return net.Listen("tcp", net.JoinHostPort("127.0.0.1", "0"))
	}
	for p := port; p <= 65535; p++ {
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no free port from %d", port)
}
