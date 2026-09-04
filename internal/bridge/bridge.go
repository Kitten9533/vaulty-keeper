// Package bridge serves a masked-only HTTP API for AI agents that run outside
// the host trust domain (Docker/container, separate account, remote VM).
//
// The bridge holds the snapshot keys on the host and never ships them to
// callers. Every value it returns is masked to "*** (n chars)" plus a
// fingerprint, regardless of how the item is classified — even items the user
// marked safe (set --plain) are masked, because the caller of this API is the
// entity we are isolating against. Plaintext never leaves the host; the
// reveal/export/edit paths are deliberately not wired here.
package bridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"vaulty-keeper/internal/apollo"
	"vaulty-keeper/internal/dbproxy"
)

const (
	// EnvAddr and EnvToken are read by the remote client to reach the bridge.
	EnvAddr  = "VAULTY_KEEPER_BRIDGE_ADDR"
	EnvToken = "VAULTY_KEEPER_BRIDGE_TOKEN"

	// TokenFile is where serve writes the per-run token (0600, host-user
	// readable) so wrapper scripts can hand it to an isolated agent.
	TokenFile = "bridge-token"
)

// Config configures the masked-only bridge. SnapshotKey and SensitiveKey
// resolve the host keys (Keychain/env); Token, when empty, is generated at
// Start time. DBStore is the path to the encrypted database-connection store
// (~/.vaulty/db.json) served by /api/db/list; when empty that endpoint
// reports no connections.
type Config struct {
	Dir          string
	SnapshotKey  func() ([]byte, error)
	SensitiveKey func() ([]byte, error)
	Token        string
	DBStore      string
}

// NewHandler builds the masked-only HTTP handler. Every /api endpoint requires
// the token (via X-Auth-Token header or ?t= query) so only the party that was
// handed the token can read; /health is open for liveness checks.
func NewHandler(cfg Config) http.Handler {
	if cfg.SnapshotKey == nil {
		cfg.SnapshotKey = apollo.SnapshotKey
	}
	if cfg.SensitiveKey == nil {
		cfg.SensitiveKey = apollo.SensitiveKey
	}
	h := &handler{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.health)
	api := http.NewServeMux()
	api.HandleFunc("/api/snapshots", h.listSnapshots)
	api.HandleFunc("/api/snapshot", h.snapshotView)
	api.HandleFunc("/api/get", h.get)
	api.HandleFunc("/api/compare", h.compare)
	api.HandleFunc("/api/db/list", h.dbList)
	mux.Handle("/api/", requireToken(cfg.Token, api))
	return mux
}

// dbList returns the registered database connections (names/types/ports only —
// never URLs) so an isolated agent can point native clients at the tunnels.
func (h *handler) dbList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if h.cfg.DBStore == "" {
		writeJSON(w, http.StatusOK, map[string]any{"connections": []any{}})
		return
	}
	key, err := apollo.DBKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "db_key_unavailable", "database key unavailable")
		return
	}
	conns, err := dbproxy.List(h.cfg.DBStore, key)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "db_store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns})
}

// requireToken gates every request with the bridge token. Failed attempts are
// throttled with an exponential backoff (defense in depth and DoS mitigation);
// the token is 128 bits of randomness so online brute force is infeasible.
func requireToken(token string, next http.Handler) http.Handler {
	var mu sync.Mutex
	failures := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && !validToken(token, r) {
			mu.Lock()
			failures++
			f := failures
			mu.Unlock()
			delay := time.Duration(f) * 50 * time.Millisecond
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
			if delay > 0 {
				time.Sleep(delay)
			}
			writeAPIError(w, http.StatusUnauthorized, "auth_required", "invalid or missing access token (see the URL printed by 'vaulty-keeper serve')")
			return
		}
		mu.Lock()
		failures = 0
		mu.Unlock()
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

type handler struct {
	cfg Config
}

// Masked is the only value shape the bridge emits: plaintext is never
// included. Value is always the masked placeholder, Length reports the real
// size, and Fingerprint lets callers compare masked values for equality.
type Masked struct {
	Present     bool   `json:"present"`
	Sensitive   bool   `json:"sensitive"`
	Value       string `json:"value,omitempty"`
	Length      int    `json:"length,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// mask builds the masked view of one decrypted value.
func mask(present bool, plain string, sensitive bool, hmacKey []byte) Masked {
	m := Masked{Present: present, Sensitive: sensitive}
	if !present {
		return m
	}
	m.Length = len(plain)
	m.Value = apollo.MaskWithLen(m.Length)
	if fp := apollo.Fingerprint(plain, hmacKey); fp != "" {
		m.Fingerprint = fp
	}
	return m
}

type itemView struct {
	Key       string `json:"key"`
	Sensitive bool   `json:"sensitive"`
	Masked    Masked `json:"value"`
}

type snapshotSummary struct {
	Name       string `json:"name"`
	AppID      string `json:"app_id"`
	CapturedAt string `json:"captured_at"`
	Total      int    `json:"total"`
	Sensitive  int    `json:"sensitive"`
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) listSnapshots(w http.ResponseWriter, r *http.Request) {
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

// snapshotView returns every item of one snapshot, each masked.
func (h *handler) snapshotView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	name := r.URL.Query().Get("name")
	appID := r.URL.Query().Get("appid")
	if err := apollo.ValidateSnapshotName(name); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", err.Error())
		return
	}
	snapKey, sensitiveKey, ok := h.keys(w)
	if !ok {
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
	keys := make([]string, 0, len(s.Items))
	for k := range s.Items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]itemView, 0, len(keys))
	for _, k := range keys {
		it := s.Items[k]
		v, err := s.DecryptItem(it, snapKey, sensitiveKey)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "snapshot_decrypt_failed", err.Error())
			return
		}
		items = append(items, itemView{
			Key:       k,
			Sensitive: it.Secret,
			Masked:    mask(true, v, it.Secret, snapKey),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": s.Meta.Name, "app_id": s.Meta.AppID, "items": items})
}

// get returns one key's masked value.
func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	name := r.URL.Query().Get("name")
	appID := r.URL.Query().Get("appid")
	key := r.URL.Query().Get("key")
	if err := apollo.ValidateSnapshotName(name); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", err.Error())
		return
	}
	if err := apollo.ValidateKey(key); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_key", err.Error())
		return
	}
	snapKey, sensitiveKey, ok := h.keys(w)
	if !ok {
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
	it, present := s.Items[key]
	if !present {
		writeAPIError(w, http.StatusNotFound, "key_not_found", fmt.Sprintf("key %q not found in snapshot %q", key, name))
		return
	}
	v, err := s.DecryptItem(it, snapKey, sensitiveKey)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_decrypt_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key":         key,
		"value":       mask(true, v, it.Secret, snapKey),
		"captured_at": s.Meta.CapturedAt,
	})
}

type safeChange struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	Old  Masked `json:"old"`
	New  Masked `json:"new"`
}

// compare returns the added/removed/changed diff between two snapshots, all
// values masked.
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
	a, err := apollo.Load(apollo.SnapPath(h.cfg.Dir, from, fromAppID))
	if err != nil {
		h.loadError(w, from, err)
		return
	}
	b, err := apollo.Load(apollo.SnapPath(h.cfg.Dir, to, toAppID))
	if err != nil {
		h.loadError(w, to, err)
		return
	}
	snapKey, sensitiveKey, ok := h.keys(w)
	if !ok {
		return
	}
	diffs, err := a.Diff(b, snapKey, sensitiveKey)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "snapshot_decrypt_failed", err.Error())
		return
	}
	changes := make([]safeChange, 0, len(diffs))
	for _, c := range diffs {
		sc := safeChange{Key: c.Key, Kind: c.Kind}
		switch c.Kind {
		case "added":
			sc.New = mask(true, c.New, c.Secret, snapKey)
		case "removed":
			sc.Old = mask(true, c.Old, c.Secret, snapKey)
		default:
			sc.Old = mask(true, c.Old, c.Secret, snapKey)
			sc.New = mask(true, c.New, c.Secret, snapKey)
		}
		changes = append(changes, sc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "changes": changes})
}

func (h *handler) loadError(w http.ResponseWriter, name string, err error) {
	if os.IsNotExist(err) {
		writeAPIError(w, http.StatusNotFound, "snapshot_not_found", fmt.Sprintf("snapshot %q not found", name))
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "snapshot_load_failed", err.Error())
}

// keys resolves both keys, writing an error response and returning ok=false
// if either is unavailable.
func (h *handler) keys(w http.ResponseWriter) (snapKey, sensitiveKey []byte, ok bool) {
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'vaulty-keeper apollo init' or set %s", err, apollo.EnvKey))
		return nil, nil, false
	}
	sensitiveKey, err = h.cfg.SensitiveKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "sensitive_key_unavailable", fmt.Sprintf("sensitive-value key unavailable (%s); run 'vaulty-keeper sensitive init' or set %s", err, apollo.EnvSensitiveKey))
		return nil, nil, false
	}
	return snapKey, sensitiveKey, true
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

// NewToken generates a fresh 128-bit bridge token (hex-encoded).
func NewToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Start runs the bridge on addr (e.g. "127.0.0.1:8970") until ctx is done. A
// per-run token is generated and written to <home>/.vaulty/bridge-token
// (0600) plus printed to out, so wrappers can pass it to an isolated agent.
// hostDir is the directory holding the token file; it need not equal cfg.Dir.
func Start(ctx context.Context, cfg Config, addr string, hostDir string, out io.Writer) error {
	if addr == "" {
		addr = "127.0.0.1:8970"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	defer listener.Close()

	if cfg.Token == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		cfg.Token = hex.EncodeToString(b)
	}
	if hostDir != "" {
		if err := writeTokenFile(hostDir, cfg.Token); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "vaulty-keeper masked bridge listening on http://%s\n", addr)
	fmt.Fprintf(out, "hand token to the isolated agent via %s=%s and %s=http://%s\n", EnvToken, cfg.Token, EnvAddr, addr)

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

// writeTokenFile writes the bridge token to <dir>/bridge-token with mode 0600.
func writeTokenFile(dir, token string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, TokenFile)
	return os.WriteFile(path, []byte(token+"\n"), 0o600)
}

// TokenPath returns the default location of the bridge token file.
func TokenPath(home string) string {
	return filepath.Join(home, ".vaulty", TokenFile)
}
