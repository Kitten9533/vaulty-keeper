package ui

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"ai-tools/internal/apollo"
	"ai-tools/internal/dbproxy"
)

// Database-connection endpoints, mirroring the `ai-tools db` CLI in the web
// UI. Same security model as the rest of the UI: list/connect are open GETs
// (no URL, no password); init/add/test/rm are POST/DELETE (token-gated);
// show (real URL) is plaintext-gated (--allow-plaintext) on top of the token.

// dbKey resolves the DB encryption key.
func (h *handler) dbKey() ([]byte, error) {
	if h.cfg.DBKey != nil {
		return h.cfg.DBKey()
	}
	return apollo.DBKey()
}

func (h *handler) dbStore() (string, bool) {
	if h.cfg.DBStore == "" {
		return "", false
	}
	return h.cfg.DBStore, true
}

// dbKeyStatus reports whether the DB encryption key exists.
func (h *handler) dbKeyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
		return
	}
	_, err := h.dbKey()
	writeJSON(w, http.StatusOK, map[string]any{"available": err == nil})
}

// dbPubKey publishes the per-process ECDH public key the frontend uses to
// encrypt database URLs before submitting them (see urlEnc). The public key
// is not secret; the matching private key lives only in this process's
// memory and is regenerated on every start.
func (h *handler) dbPubKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
		return
	}
	if h.dbPriv == nil {
		writeAPIError(w, http.StatusInternalServerError, "db_encryption_unavailable", "数据库 URL 加密不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"alg": "ECDH-P256",
		"pub": base64.StdEncoding.EncodeToString(h.dbPriv.PublicKey().Bytes()),
	})
}

// urlEnc is a database URL encrypted in the browser with AES-256-GCM under a
// fresh key derived from the UI's published ECDH public key. Eph is the
// browser's one-shot ECDH public key (raw uncompressed point), iv and ct are
// base64.
type urlEnc struct {
	Eph string `json:"eph"`
	IV  string `json:"iv"`
	Ct  string `json:"ct"`
}

// decryptURL seals an encrypted database URL back to plaintext with the
// process-local ECDH private key. Errors never echo the ciphertext.
func (h *handler) decryptURL(e urlEnc) (string, error) {
	if h.dbPriv == nil {
		return "", errors.New("数据库 URL 加密不可用")
	}
	ephRaw, err := base64.StdEncoding.DecodeString(e.Eph)
	if err != nil {
		return "", errors.New("无效的加密参数（eph）")
	}
	ephPub, err := ecdh.P256().NewPublicKey(ephRaw)
	if err != nil {
		return "", errors.New("无效的加密参数（eph 公钥）")
	}
	secret, err := h.dbPriv.ECDH(ephPub)
	if err != nil {
		return "", errors.New("无法建立加密通道")
	}
	iv, err := base64.StdEncoding.DecodeString(e.IV)
	if err != nil {
		return "", errors.New("无效的加密参数（iv）")
	}
	ct, err := base64.StdEncoding.DecodeString(e.Ct)
	if err != nil {
		return "", errors.New("无效的加密参数（ct）")
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return "", errors.New("加密初始化失败")
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New("加密初始化失败")
	}
	pt, err := g.Open(nil, iv, ct, nil)
	if err != nil {
		return "", errors.New("URL 解密失败，请刷新页面后重试")
	}
	return string(pt), nil
}

// dbList returns connection metadata only (never URLs).
func (h *handler) dbList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
		return
	}
	store, ok := h.dbStore()
	if !ok {
		writeAPIError(w, http.StatusNotFound, "db_store_unconfigured", "未配置数据库存储")
		return
	}
	key, err := h.dbKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "db_key_unavailable", "数据库密钥不可用，请先初始化（db init）")
		return
	}
	conns, err := dbproxy.List(store, key)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "db_store_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns})
}

// dbInit creates the DB encryption key (writes to Keychain / env fallback).
func (h *handler) dbInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
		return
	}
	var req initRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "无效的 JSON 请求体")
		return
	}
	if err := apollo.GenerateAndStoreDBKey(req.Force); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "db_key_init_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

type dbAddRequest struct {
	Name string `json:"name"`
	// URL is the legacy plaintext field, kept for CLI/script callers that
	// already know the value they are registering. The browser always sends
	// URLEnc instead so the URL is never plaintext on the wire.
	URL    string  `json:"url"`
	URLEnc *urlEnc `json:"url_enc"`
	Port   int     `json:"port"`
}

// dbAdd registers a new connection (the URL is encrypted at rest).
func (h *handler) dbAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
		return
	}
	store, ok := h.dbStore()
	if !ok {
		writeAPIError(w, http.StatusNotFound, "db_store_unconfigured", "未配置数据库存储")
		return
	}
	var req dbAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "无效的 JSON 请求体")
		return
	}
	rawURL := req.URL
	if rawURL == "" && req.URLEnc != nil {
		dec, err := h.decryptURL(*req.URLEnc)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_url_enc", err.Error())
			return
		}
		rawURL = dec
	}
	if req.Name == "" || rawURL == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_db_add", "name 与 url（或 url_enc）必填")
		return
	}
	key, err := h.dbKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "db_key_unavailable", "数据库密钥不可用，请先初始化（db init）")
		return
	}
	if err := dbproxy.Add(store, key, req.Name, rawURL, req.Port); err != nil {
		writeAPIError(w, http.StatusBadRequest, "db_add_failed", err.Error())
		return
	}
	typ, _ := dbproxy.ConnTypeFromURL(rawURL)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "name": req.Name, "type": typ})
}

type dbTestRequest struct {
	Name string `json:"name"`
}

// dbTest verifies a connection can authenticate (never returns the URL).
func (h *handler) dbTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
		return
	}
	store, ok := h.dbStore()
	if !ok {
		writeAPIError(w, http.StatusNotFound, "db_store_unconfigured", "未配置数据库存储")
		return
	}
	var req dbTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "无效的 JSON 请求体")
		return
	}
	key, err := h.dbKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "db_key_unavailable", "数据库密钥不可用")
		return
	}
	conn, err := dbproxy.Resolve(store, key, req.Name)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "db_not_found", err.Error())
		return
	}
	res := map[string]any{"name": req.Name, "type": conn.Type, "port": conn.Port, "ok": true}
	if user := dbUser(conn.URL); user != "" {
		res["user"] = user
	}
	if db := dbName(conn.URL); db != "" {
		res["db"] = db
	}
	if err := dbproxy.TestConn(conn); err != nil {
		res["ok"] = false
		res["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, res)
}

type dbTestURLRequest struct {
	URLEnc *urlEnc `json:"url_enc"`
}

// dbTestURL verifies an as-yet-unregistered database URL (typed into the
// registration form) can authenticate to the real database, without saving
// anything. The URL arrives encrypted and the response never contains it.
func (h *handler) dbTestURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
		return
	}
	var req dbTestURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "无效的 JSON 请求体")
		return
	}
	if req.URLEnc == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_db_test_url", "缺少 url_enc")
		return
	}
	rawURL, err := h.decryptURL(*req.URLEnc)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_url_enc", err.Error())
		return
	}
	typ, err := dbproxy.ConnTypeFromURL(rawURL)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "unsupported_db_type", err.Error())
		return
	}
	res := map[string]any{"type": typ, "ok": true}
	if user := dbUser(rawURL); user != "" {
		res["user"] = user
	}
	if db := dbName(rawURL); db != "" {
		res["db"] = db
	}
	if err := dbproxy.TestConn(dbproxy.Conn{URL: rawURL, Type: typ}); err != nil {
		res["ok"] = false
		res["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, res)
}

func dbUser(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return ""
	}
	return u.User.Username()
}

func dbName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// dbConnectInfo builds the ready-to-run client links for a connection.
type dbConnectInfo struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Port    int            `json:"port"`
	Token   string         `json:"token,omitempty"`
	Host    string         `json:"host"`
	Raw     string         `json:"raw,omitempty"`
	Clients []dbClientLine `json:"clients"`
	Note    string         `json:"note,omitempty"`
}

type dbClientLine struct {
	Label string `json:"label"`
	Line  string `json:"line"`
}

// dbConnect returns connect info for a connection (bridge token embedded, not
// the real database password).
func (h *handler) dbConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
		return
	}
	store, ok := h.dbStore()
	if !ok {
		writeAPIError(w, http.StatusNotFound, "db_store_unconfigured", "未配置数据库存储")
		return
	}
	name := r.URL.Query().Get("name")
	key, err := h.dbKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "db_key_unavailable", "数据库密钥不可用")
		return
	}
	conn, err := dbproxy.Resolve(store, key, name)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "db_not_found", err.Error())
		return
	}
	host := "127.0.0.1"
	if r.URL.Query().Get("container") == "1" {
		host = "host.docker.internal"
	}
	token := uiBridgeToken()
	info := dbConnectInfo{Name: conn.Name, Type: conn.Type, Port: conn.Port, Token: token, Host: host}
	db := dbName(conn.URL)
	if token == "" {
		info.Note = "serve 未运行或无 bridge token，无法生成带 token 的命令"
	}
	switch conn.Type {
	case "postgres":
		if token != "" {
			info.Raw = fmt.Sprintf("postgresql://%s@%s:%d/%s", token, host, conn.Port, db)
			info.Clients = []dbClientLine{
				{"psql —— PostgreSQL 自带的命令行客户端（装 PG 就有）", info.Raw},
				{"DBeaver / DataGrip —— 通用数据库图形工具（贴 JDBC 链接）", fmt.Sprintf("jdbc:postgresql://%s:%d/%s?user=%s", host, conn.Port, db, token)},
				{"pgAdmin4 —— PostgreSQL 官方图形工具（填字段）", fmt.Sprintf("Host=%s Port=%d Database=%s Username=%s 密码留空", host, conn.Port, db, token)},
			}
		}
	case "mysql":
		if token != "" {
			info.Raw = fmt.Sprintf("mysql://%s:x@%s:%d/%s", token, host, conn.Port, db)
			info.Clients = []dbClientLine{
				{"DBeaver / DataGrip —— 通用数据库图形工具（贴 JDBC 链接）", fmt.Sprintf("jdbc:mysql://%s:%d/%s?user=%s&password=x", host, conn.Port, db, token)},
				{"MySQL Workbench —— MySQL 官方图形工具（填字段）", fmt.Sprintf("Hostname=%s Port=%d Default Schema=%s Username=%s 密码任意", host, conn.Port, db, token)},
				{"mysql —— MySQL 自带的命令行客户端（装 MySQL 就有）", fmt.Sprintf("mysql -h %s -P %d -u %s -px --ssl-mode=DISABLED %s", host, conn.Port, token, db)},
			}
		}
	case "redis":
		if token != "" {
			info.Raw = fmt.Sprintf("redis://:%s@%s:%d/%s", token, host, conn.Port, db)
			info.Clients = []dbClientLine{
				{"Redis Insight —— Redis 官方图形工具（贴 URL）", info.Raw},
				{"redis-cli —— Redis 自带的命令行客户端（装 Redis 就有）", fmt.Sprintf("redis-cli -h %s -p %d -a %s --no-auth-warning", host, conn.Port, token)},
			}
		}
	default:
		writeAPIError(w, http.StatusBadRequest, "unsupported_db_type", fmt.Sprintf("不支持的数据库类型 %q", conn.Type))
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// uiBridgeToken resolves the bridge token (env first, then the token file).
func uiBridgeToken() string {
	if t := os.Getenv("AI_TOOLS_BRIDGE_TOKEN"); t != "" {
		return t
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".ai-tools", "bridge-token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

type dbShowRequest struct {
	Name string `json:"name"`
}

// dbShow returns the decrypted real URL. Plaintext-gated like reveal/export.
func (h *handler) dbShow(w http.ResponseWriter, r *http.Request) {
	if !h.plaintextOnly(w) {
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
		return
	}
	store, ok := h.dbStore()
	if !ok {
		writeAPIError(w, http.StatusNotFound, "db_store_unconfigured", "未配置数据库存储")
		return
	}
	var req dbShowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "无效的 JSON 请求体")
		return
	}
	key, err := h.dbKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "db_key_unavailable", "数据库密钥不可用")
		return
	}
	conn, err := dbproxy.Resolve(store, key, req.Name)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "db_not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": conn.Name, "type": conn.Type, "port": conn.Port, "url": conn.URL})
}

// dbRemove deletes a connection.
func (h *handler) dbRemove(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许")
		return
	}
	store, ok := h.dbStore()
	if !ok {
		writeAPIError(w, http.StatusNotFound, "db_store_unconfigured", "未配置数据库存储")
		return
	}
	key, err := h.dbKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "db_key_unavailable", "数据库密钥不可用")
		return
	}
	if err := dbproxy.Remove(store, key, name); err != nil {
		writeAPIError(w, http.StatusBadRequest, "db_remove_failed", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
