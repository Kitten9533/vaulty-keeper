package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"vaulty-keeper/internal/apollo"
	"vaulty-keeper/internal/bridge"
	"vaulty-keeper/internal/dbproxy"
	"vaulty-keeper/internal/i18n"
)

// ---- serve ----

// runServe starts the masked-only bridge on the host that holds the keys, and
// (when a database store exists) one TCP tunnel per registered database
// connection. It blocks until interrupted. A per-run token is printed and
// written to ~/.vaulty/bridge-token so wrapper scripts can hand it to an
// isolated agent; the same token gates the database tunnels.
func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dirFlag := fs.String("dir", "", "snapshot directory")
	addr := fs.String("addr", "127.0.0.1:8970", "listen address (host:port)")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	dirPath, err := snapDir(*dirFlag)
	if err != nil {
		return fail("serve: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail("serve: %v", err)
	}
	tokenDir := filepath.Join(home, ".vaulty")
	token := bridge.NewToken()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the database tunnels (same token as the HTTP bridge) when the
	// store exists and the DB key is available; otherwise serve keeps working
	// for masked reads.
	dbStore, err := dbPath("")
	if err != nil {
		return fail("serve: %v", err)
	}
	if _, serr := os.Stat(dbStore); serr == nil {
		if dbKey, kerr := apollo.DBKey(); kerr == nil {
			tun := &dbproxy.Tunnel{
				Path:  dbStore,
				Key:   dbKey,
				Host:  hostOnly(*addr),
				Token: token,
				Log:   os.Stdout,
			}
			go func() {
				if terr := tun.Start(ctx); terr != nil {
					fmt.Fprintf(os.Stderr, "serve: db tunnels: %v\n", terr)
				}
			}()
		} else {
			fmt.Fprintln(os.Stderr, i18n.T("remote.db-key-skip", kerr.Error()))
		}
	}

	if err := bridge.Start(ctx, bridge.Config{Dir: dirPath, Token: token, DBStore: dbStore}, *addr, tokenDir, os.Stdout); err != nil {
		return fail("serve: %v", err)
	}
	return 0
}

// hostOnly extracts the host part of a host:port listen address.
func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// ---- remote ----

// remoteConfig resolves the bridge base URL and token: env first, then the
// host token file (~/.vaulty/bridge-token) so local dogfooding works without
// exporting anything.
func remoteConfig() (base, token string, err error) {
	base = os.Getenv(bridge.EnvAddr)
	if base == "" {
		base = "http://127.0.0.1:8970"
	}
	token = os.Getenv(bridge.EnvToken)
	if token == "" {
		if home, herr := os.UserHomeDir(); herr == nil {
			if b, rerr := os.ReadFile(bridge.TokenPath(home)); rerr == nil {
				token = strings.TrimSpace(string(b))
			}
		}
	}
	if token == "" {
		return "", "", errors.New(i18n.T("remote.token-missing", bridge.EnvToken))
	}
	return base, token, nil
}

// bridgeGet performs an authenticated GET against the masked bridge and
// returns the response body, mapping non-200 responses to errors.
func bridgeGet(base, token, path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Auth-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		msg := e.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		return nil, fmt.Errorf("bridge %s：%s", resp.Status, msg)
	}
	return body, nil
}

func runRemote(args []string) int {
	if len(args) == 0 {
		remoteUsage(os.Stderr)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return remoteList(rest)
	case "get":
		return remoteGet(rest)
	case "compare":
		return remoteCompare(rest)
	case "dblist":
		return remoteDBListCmd(rest)
	case "help", "-h", "--help":
		remoteUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "vaulty-keeper remote: unknown subcommand %q\n\n", sub)
		remoteUsage(os.Stderr)
		return 2
	}
}

func remoteUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	printDomainUsage(w, "remote")
	fmt.Fprintf(w, "%s", i18n.T("help.usage.remote", bridge.EnvAddr, bridge.EnvToken))
}

type bridgeMasked struct {
	Present     bool   `json:"present"`
	Sensitive   bool   `json:"sensitive"`
	Value       string `json:"value,omitempty"`
	Length      int    `json:"length,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

func remoteList(args []string) int {
	fs := flag.NewFlagSet("remote list", flag.ContinueOnError)
	appID := fs.String("appid", "", "app id")
	jsonOut := fs.Bool("json", false, "output JSON")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() > 1 {
		return fail("remote list: %s", i18n.T("remote.list-usage"))
	}
	base, token, err := remoteConfig()
	if err != nil {
		return fail("remote list: %v", err)
	}
	if fs.NArg() == 0 {
		body, err := bridgeGet(base, token, "/api/snapshots")
		if err != nil {
			return fail("remote list: %v", err)
		}
		if *jsonOut {
			fmt.Println(string(body))
			return 0
		}
		var res struct {
			Snapshots []struct {
				Name      string `json:"name"`
				AppID     string `json:"app_id"`
				Total     int    `json:"total"`
				Sensitive int    `json:"sensitive"`
			} `json:"snapshots"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return fail("remote list: %v", err)
		}
		for _, s := range res.Snapshots {
			if s.AppID != "" {
				fmt.Printf("%s (%s)\n", s.Name, s.AppID)
			} else {
				fmt.Println(s.Name)
			}
		}
		return 0
	}

	name := fs.Arg(0)
	path := "/api/snapshot?name=" + url.QueryEscape(name)
	if *appID != "" {
		path += "&appid=" + url.QueryEscape(*appID)
	}
	body, err := bridgeGet(base, token, path)
	if err != nil {
		return fail("remote list: %v", err)
	}
	if *jsonOut {
		fmt.Println(string(body))
		return 0
	}
	var res struct {
		Items []struct {
			Key    string       `json:"key"`
			Masked bridgeMasked `json:"value"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fail("remote list: %v", err)
	}
	for _, it := range res.Items {
		fmt.Printf("%s = %s\n", it.Key, maskedText(it.Masked))
	}
	return 0
}

func remoteGet(args []string) int {
	fs := flag.NewFlagSet("remote get", flag.ContinueOnError)
	appID := fs.String("appid", "", "app id")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 2 {
		return fail("remote get: %s", i18n.T("remote.get-usage"))
	}
	base, token, err := remoteConfig()
	if err != nil {
		return fail("remote get: %v", err)
	}
	name, key := fs.Arg(0), fs.Arg(1)
	path := "/api/get?name=" + url.QueryEscape(name) + "&key=" + url.QueryEscape(key)
	if *appID != "" {
		path += "&appid=" + url.QueryEscape(*appID)
	}
	body, err := bridgeGet(base, token, path)
	if err != nil {
		return fail("remote get: %v", err)
	}
	var res struct {
		Value bridgeMasked `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fail("remote get: %v", err)
	}
	fmt.Println(res.Value.Value)
	return 0
}

func remoteCompare(args []string) int {
	fs := flag.NewFlagSet("remote compare", flag.ContinueOnError)
	appID := fs.String("appid", "", "app id for the first snapshot")
	appIDTo := fs.String("appid-to", "", "app id for the second snapshot")
	jsonOut := fs.Bool("json", false, "output JSON")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 2 {
		return fail("remote compare: %s", i18n.T("remote.compare-usage"))
	}
	base, token, err := remoteConfig()
	if err != nil {
		return fail("remote compare: %v", err)
	}
	nameA, nameB := fs.Arg(0), fs.Arg(1)
	path := "/api/compare?from=" + url.QueryEscape(nameA) + "&to=" + url.QueryEscape(nameB)
	if *appID != "" {
		path += "&from_appid=" + url.QueryEscape(*appID)
	}
	if *appIDTo != "" {
		path += "&to_appid=" + url.QueryEscape(*appIDTo)
	}
	body, err := bridgeGet(base, token, path)
	if err != nil {
		return fail("remote compare: %v", err)
	}
	var res struct {
		Changes []struct {
			Key  string       `json:"key"`
			Kind string       `json:"kind"`
			Old  bridgeMasked `json:"old"`
			New  bridgeMasked `json:"new"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fail("remote compare: %v", err)
	}
	if len(res.Changes) == 0 {
		fmt.Printf("snapshots %q and %q are identical\n", nameA, nameB)
		return 0
	}
	if *jsonOut {
		added := map[string]any{}
		removed := map[string]any{}
		changed := map[string]any{}
		for _, c := range res.Changes {
			switch c.Kind {
			case "added":
				added[c.Key] = maskedWithFP(c.New)
			case "removed":
				removed[c.Key] = maskedWithFP(c.Old)
			case "changed":
				changed[c.Key] = map[string]any{"old": maskedWithFP(c.Old), "new": maskedWithFP(c.New)}
			}
		}
		b, err := json.MarshalIndent(map[string]any{
			"from":    nameA,
			"to":      nameB,
			"added":   added,
			"removed": removed,
			"changed": changed,
		}, "", "  ")
		if err != nil {
			return fail("remote compare: %v", err)
		}
		fmt.Println(string(b))
		return 0
	}
	for _, c := range res.Changes {
		switch c.Kind {
		case "added":
			fmt.Println(green(fmt.Sprintf("+ %s = %s", c.Key, maskedText(c.New))))
		case "removed":
			fmt.Println(red(fmt.Sprintf("- %s = %s", c.Key, maskedText(c.Old))))
		case "changed":
			fmt.Println(yellow(fmt.Sprintf("~ %s: %s -> %s", c.Key, maskedText(c.Old), maskedText(c.New))))
		}
	}
	return 0
}

// maskedText renders a masked value with its fingerprint so same-length values
// can still be told apart (different fingerprint = different value).
func maskedText(m bridgeMasked) string {
	if m.Fingerprint != "" {
		return fmt.Sprintf("%s [%s]", m.Value, m.Fingerprint)
	}
	return m.Value
}

// maskedWithFP returns the masked value plus fingerprint for JSON output.
func maskedWithFP(m bridgeMasked) map[string]string {
	out := map[string]string{"value": m.Value}
	if m.Fingerprint != "" {
		out["fingerprint"] = m.Fingerprint
	}
	return out
}

// remoteDBListCmd lists the host's database tunnels through the bridge, so an
// isolated agent can point native clients at them (names/types/ports only).
func remoteDBListCmd(args []string) int {
	fs := flag.NewFlagSet("remote dblist", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output JSON")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	base, token, err := remoteConfig()
	if err != nil {
		return fail("remote dblist: %v", err)
	}
	body, err := bridgeGet(base, token, "/api/db/list")
	if err != nil {
		return fail("remote dblist: %v", err)
	}
	if *jsonOut {
		fmt.Println(string(body))
		return 0
	}
	var res struct {
		Connections []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Port     int    `json:"port"`
			Disabled bool   `json:"disabled"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fail("remote dblist: %v", err)
	}
	for _, c := range res.Connections {
		if c.Disabled {
			fmt.Printf("%s (%s) :%d [%s]\n", c.Name, c.Type, c.Port, i18n.T("db.off-mark"))
		} else {
			fmt.Printf("%s (%s) :%d\n", c.Name, c.Type, c.Port)
		}
	}
	return 0
}
