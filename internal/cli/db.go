package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vaulty-keeper/internal/apollo"
	"vaulty-keeper/internal/bridge"
	"vaulty-keeper/internal/dbproxy"
	"vaulty-keeper/internal/i18n"
)

// dbPath resolves the store file: --dir overrides, else VAULTY_KEEPER_DB_DIR, else
// the default ~/.vaulty/db.json.
func dbPath(flagDir string) (string, error) {
	if flagDir != "" {
		return filepath.Join(flagDir, dbproxy.FileName), nil
	}
	if d := os.Getenv("VAULTY_KEEPER_DB_DIR"); d != "" {
		return filepath.Join(d, dbproxy.FileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return dbproxy.DefaultPath(home), nil
}

// ---- db ----

func runDB(args []string) int {
	if len(args) == 0 {
		dbUsage(os.Stderr)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return dbInit(rest)
	case "add":
		return dbAdd(rest)
	case "list":
		return dbList(rest)
	case "rm":
		return dbRM(rest)
	case "shell":
		return dbShell(rest)
	case "connect":
		return dbConnect(rest)
	case "test":
		return dbTest(rest)
	case "regen":
		return dbRegen(rest)
	case "on":
		return dbTunnelOn(rest)
	case "off":
		return dbTunnelOff(rest)
	case "show":
		return dbShow(rest)
	case "help", "-h", "--help":
		dbUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "vaulty-keeper db: unknown subcommand %q\n\n", sub)
		dbUsage(os.Stderr)
		return 2
	}
}

func dbUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	printDomainUsage(w, "db")
	fmt.Fprint(w, i18n.T("help.usage.db"))
}

func dbInit(args []string) int {
	fs := flag.NewFlagSet("db init", flag.ContinueOnError)
	force := fs.Bool("force", false, "re-generate the key even if one exists")
	yes := fs.Bool("yes", false, "confirm --force when not on a TTY")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if *force {
		// A key already exists: regenerating silently makes every existing
		// connection in db.json undecryptable (the old key is gone).
		if _, err := apollo.DBKey(); err == nil {
			msg := i18n.T("db.init-force-warning")
			if !isTTY(os.Stdin.Fd()) {
				if !*yes {
					return fail("db init: %s", i18n.T("db.init-force-non-tty", msg))
				}
			} else if !confirmYes(msg) {
				return fail("db init: %s", i18n.T("db.init-cancelled"))
			}
		}
	}
	if err := apollo.GenerateAndStoreDBKey(*force); err != nil {
		return fail("db init: %v", err)
	}
	fmt.Println(i18n.T("db.key-created"))
	return 0
}

// confirmYes asks a yes/no question on the terminal.
func confirmYes(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}

func dbAdd(args []string) int {
	fs := flag.NewFlagSet("db add", flag.ContinueOnError)
	dir := fs.String("dir", "", "store directory")
	port := fs.Int("port", 0, "tunnel port (default: auto)")
	test := fs.Bool("test", false, "connect to the database and authenticate before saving")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 1 {
		return fail("db add: %s", i18n.T("db.add-usage"))
	}
	name := fs.Arg(0)
	raw := readDSN()
	if raw == "" {
		return fail("db add: %s", i18n.T("db.add-stdin-required", name))
	}
	typ, err := dbproxy.ConnTypeFromURL(raw)
	if err != nil {
		return fail("db add: %v", err)
	}
	if *test {
		if err := dbproxy.TestConn(dbproxy.Conn{Name: name, URL: raw, Type: typ}); err != nil {
			return fail("db add: %s", i18n.T("db.add-test-failed", err.Error()))
		}
	}
	path, err := dbPath(*dir)
	if err != nil {
		return fail("db add: %v", err)
	}
	key, err := apollo.DBKey()
	if err != nil {
		return fail("db add: %v", err)
	}
	if err := dbproxy.Add(path, key, name, raw, *port); err != nil {
		return fail("db add: %v", err)
	}
	fmt.Println(i18n.T("db.added", name, typ))
	return 0
}

// readDSN reads one line of input. On a TTY it prompts; otherwise it reads
// everything from stdin (pipe/script). The DSN is never taken from argv.
func readDSN() string {
	if isTTY(os.Stdin.Fd()) {
		fmt.Fprint(os.Stderr, "Paste database URL (e.g. postgres://u:p@host:5432/db): ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		return strings.TrimSpace(line)
	}
	b, _ := io.ReadAll(os.Stdin)
	return strings.TrimSpace(string(b))
}

func dbList(args []string) int {
	fs := flag.NewFlagSet("db list", flag.ContinueOnError)
	dir := fs.String("dir", "", "store directory")
	jsonOut := fs.Bool("json", false, "output JSON")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	path, err := dbPath(*dir)
	if err != nil {
		return fail("db list: %v", err)
	}
	conns, localErr := localConns(path)
	if localErr != nil {
		// No local store/key: reach the host bridge from an isolated container.
		if remote := remoteDBList(); remote != nil {
			conns = remote
		} else {
			return fail("db list: %s", dbListHint(path, localErr)+i18n.T("db.list-no-bridge"))
		}
	}
	if *jsonOut {
		b, err := json.MarshalIndent(map[string]any{"connections": conns}, "", "  ")
		if err != nil {
			return fail("db list: %v", err)
		}
		fmt.Println(string(b))
		return 0
	}
	for _, c := range conns {
		if c.Disabled {
			fmt.Printf("%s (%s) :%d [%s]\n", c.Name, c.Type, c.Port, i18n.T("db.off-mark"))
		} else {
			fmt.Printf("%s (%s) :%d\n", c.Name, c.Type, c.Port)
		}
	}
	return 0
}

// localConns lists connections from the local encrypted store.
func localConns(path string) ([]dbproxy.Conn, error) {
	key, err := apollo.DBKey()
	if err != nil {
		return nil, err
	}
	return dbproxy.List(path, key)
}

// dbListHint explains a local db list failure with an actionable message
// (missing store vs key mismatch) and the store path.
func dbListHint(path string, err error) string {
	base := i18n.T("db.list-hint-base", err.Error())
	if _, serr := os.Stat(path); os.IsNotExist(serr) {
		return i18n.T("db.list-hint-missing", path)
	}
	if strings.Contains(err.Error(), "database key not found") {
		return i18n.T("db.list-hint-nokey", base)
	}
	if strings.Contains(err.Error(), "decrypt") || strings.Contains(err.Error(), "cipher") {
		return i18n.T("db.list-hint-mismatch", path)
	}
	return base
}

// remoteDBList fetches the connection list through the masked bridge
// (VAULTY_KEEPER_BRIDGE_ADDR/TOKEN). Returns nil when the bridge is unreachable or
// not configured.
func remoteDBList() []dbproxy.Conn {
	base, token, err := remoteConfig()
	if err != nil {
		return nil
	}
	body, err := bridgeGet(base, token, "/api/db/list")
	if err != nil {
		return nil
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
		return nil
	}
	out := make([]dbproxy.Conn, 0, len(res.Connections))
	for _, c := range res.Connections {
		out = append(out, dbproxy.Conn{Name: c.Name, Type: c.Type, Port: c.Port, Disabled: c.Disabled})
	}
	return out
}

func dbRM(args []string) int {
	fs := flag.NewFlagSet("db rm", flag.ContinueOnError)
	dir := fs.String("dir", "", "store directory")
	yes := fs.Bool("yes", false, "confirm removal when not on a TTY")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 1 {
		return fail("db rm: %s", i18n.T("db.rm-usage"))
	}
	if !isTTY(os.Stdin.Fd()) && !*yes {
		return fail("db rm: %s", i18n.T("cli.rm-non-tty"))
	}
	name := fs.Arg(0)
	path, err := dbPath(*dir)
	if err != nil {
		return fail("db rm: %v", err)
	}
	key, err := apollo.DBKey()
	if err != nil {
		return fail("db rm: %v", err)
	}
	if err := dbproxy.Remove(path, key, name); err != nil {
		return fail("db rm: %v", err)
	}
	fmt.Println(i18n.T("db.removed", name))
	return 0
}

// dbTest verifies that a registered connection can reach and authenticate to
// the real database. Safe for AI/scripts: the output never contains the URL,
// password, or real host:port.
func dbTest(args []string) int {
	fs := flag.NewFlagSet("db test", flag.ContinueOnError)
	dir := fs.String("dir", "", "store directory")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 1 {
		return fail("db test: %s", i18n.T("db.test-usage"))
	}
	name := fs.Arg(0)
	path, err := dbPath(*dir)
	if err != nil {
		return fail("db test: %v", err)
	}
	key, err := apollo.DBKey()
	if err != nil {
		return fail("db test: %v", err)
	}
	conn, err := dbproxy.Resolve(path, key, name)
	if err != nil {
		return fail("db test: %v", err)
	}
	if err := dbproxy.TestConn(conn); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %s: %v\n", name, err)
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("db.test-fix", name))
		return 1
	}
	user := ""
	if u, perr := url.Parse(conn.URL); perr == nil && u.User != nil {
		user = u.User.Username()
	}
	fmt.Printf(i18n.T("db.ok"), name, conn.Type)
	if user != "" {
		fmt.Printf(i18n.T("db.ok-user"), user)
	}
	if db := strings.TrimPrefix(mustURLPath(conn.URL), "/"); db != "" {
		fmt.Printf(i18n.T("db.ok-db"), db)
	}
	fmt.Printf("\n")
	return 0
}

func mustURLPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

// dbRegen regenerates the dedicated tunnel token of one connection (or every
// connection with --all). The old token stops working immediately because the
// tunnel re-checks it on every connection; the global bridge token used by
// masked reads / container agents is unaffected. Safe for AI: it only prints
// the tunnel token, never the real database URL or credentials.
func dbRegen(args []string) int {
	fs := flag.NewFlagSet("db regen", flag.ContinueOnError)
	dir := fs.String("dir", "", "store directory")
	all := fs.Bool("all", false, "regenerate the token of every connection")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if *all && fs.NArg() != 0 {
		return fail("db regen: %s", i18n.T("db.regen-no-name-with-all"))
	}
	if !*all && fs.NArg() != 1 {
		return fail("db regen: %s", i18n.T("db.regen-usage"))
	}
	path, err := dbPath(*dir)
	if err != nil {
		return fail("db regen: %v", err)
	}
	key, err := apollo.DBKey()
	if err != nil {
		return fail("db regen: %v", err)
	}
	if *all {
		names, err := dbproxy.RegenTokenAll(path, key)
		if err != nil {
			return fail("db regen: %v", err)
		}
		if len(names) == 0 {
			fmt.Println(i18n.T("db.regen-none"))
			return 0
		}
		fmt.Println(i18n.T("db.regen-done", len(names), strings.Join(names, ", ")))
		return 0
	}
	name := fs.Arg(0)
	tok, err := dbproxy.RegenToken(path, key, name)
	if err != nil {
		return fail("db regen: %v", err)
	}
	conn, err := dbproxy.Resolve(path, key, name)
	if err != nil {
		return fail("db regen: %v", err)
	}
	u, err := url.Parse(conn.URL)
	if err != nil {
		return fail("db regen: %v", err)
	}
	db := strings.TrimPrefix(u.Path, "/")
	fmt.Println(i18n.T("db.regen-token", name, tok))
	fmt.Println(i18n.T("db.regen-link-local", dbproxy.RawTunnelURL(conn.Type, tok, "127.0.0.1", conn.Port, db)))
	fmt.Println(i18n.T("db.regen-link-container", dbproxy.RawTunnelURL(conn.Type, tok, "host.docker.internal", conn.Port, db)))
	return 0
}

// dbTunnelOn/Off turn a connection's tunnel on (`db on`) or off (`db off`),
// per connection or for every connection with --all. The running serve picks
// the change up within ~2s (it syncs db.json), so no restart is needed. Safe
// for AI/scripts: only the on/off flag changes, never any plaintext.
func dbTunnelOn(args []string) int  { return dbTunnelSet(args, false, "on") }
func dbTunnelOff(args []string) int { return dbTunnelSet(args, true, "off") }

func dbTunnelSet(args []string, disabled bool, verb string) int {
	fs := flag.NewFlagSet("db", flag.ContinueOnError)
	dir := fs.String("dir", "", "store directory")
	all := fs.Bool("all", false, "apply to every connection")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if *all && fs.NArg() != 0 {
		return fail("db %s: %s", verb, i18n.T("db.tunnel-no-name"))
	}
	if !*all && fs.NArg() != 1 {
		return fail("db %s: %s", verb, i18n.T("db.tunnel-usage", verb, verb))
	}
	path, err := dbPath(*dir)
	if err != nil {
		return fail("db %s: %v", verb, err)
	}
	key, err := apollo.DBKey()
	if err != nil {
		return fail("db %s: %v", verb, err)
	}
	if *all {
		names, err := dbproxy.SetTunnelAll(path, key, disabled)
		if err != nil {
			return fail("db %s: %v", verb, err)
		}
		if len(names) == 0 {
			fmt.Println(i18n.T("db.tunnel-none"))
			return 0
		}
		if disabled {
			fmt.Println(i18n.T("db.tunnel-done-off", len(names), strings.Join(names, ", ")))
		} else {
			fmt.Println(i18n.T("db.tunnel-done-on", len(names), strings.Join(names, ", ")))
		}
		return 0
	}
	name := fs.Arg(0)
	if err := dbproxy.SetTunnel(path, key, name, disabled); err != nil {
		return fail("db %s: %v", verb, err)
	}
	if disabled {
		fmt.Println(i18n.T("db.tunnel-off-done", name, name))
	} else {
		fmt.Println(i18n.T("db.tunnel-on-done", name))
	}
	return 0
}

// dbShow prints the decrypted real URL for a connection. TTY-only: like the
// plaintext commands, it is never available to scripts/AI.
func dbShow(args []string) int {
	fs := flag.NewFlagSet("db show", flag.ContinueOnError)
	dir := fs.String("dir", "", "store directory")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 1 {
		return fail("db show: %s", i18n.T("db.show-usage"))
	}
	if !isTTY(os.Stdin.Fd()) {
		return fail("db show: %s", i18n.T("db.show-tty-only"))
	}
	name := fs.Arg(0)
	path, err := dbPath(*dir)
	if err != nil {
		return fail("db show: %v", err)
	}
	key, err := apollo.DBKey()
	if err != nil {
		return fail("db show: %v", err)
	}
	conn, err := dbproxy.Resolve(path, key, name)
	if err != nil {
		return fail("db show: %v", err)
	}
	fmt.Printf(i18n.T("db.show-name")+"\n", conn.Name)
	fmt.Printf(i18n.T("db.show-type")+"\n", conn.Type)
	fmt.Printf(i18n.T("db.show-port")+"\n", conn.Port)
	fmt.Printf(i18n.T("db.show-url")+"\n", conn.URL)
	fmt.Fprintln(os.Stderr, i18n.T("db.show-warn"))
	return 0
}

// dbConnect prints a ready-to-run native client command that connects through
// the tunnel using the bridge token (never the real credentials). The token is
// resolved from env or the host's bridge-token file; it is not a secret from
// the AI, so printing it is safe.
func dbConnect(args []string) int {
	fs := flag.NewFlagSet("db connect", flag.ContinueOnError)
	dir := fs.String("dir", "", "store directory")
	host := fs.String("host", "", "host in the command (default 127.0.0.1)")
	container := fs.Bool("container", false, "use host.docker.internal (run inside an isolated container)")
	cmdOnly := fs.Bool("cmd", false, "print only the executable client command (for scripts)")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 1 {
		return fail("db connect: %s", i18n.T("db.connect-usage"))
	}
	name := fs.Arg(0)
	path, err := dbPath(*dir)
	if err != nil {
		return fail("db connect: %v", err)
	}
	key, err := apollo.DBKey()
	if err != nil {
		return fail("db connect: %v", err)
	}
	conn, err := dbproxy.Resolve(path, key, name)
	if err != nil {
		return fail("db connect: %v", err)
	}
	if conn.Disabled {
		fmt.Fprintln(os.Stderr, i18n.T("db.connect-warn-off", name, name))
	}
	// Prefer the connection's dedicated token; fall back to the global bridge
	// token for legacy connections that predate per-connection tokens.
	token := conn.Token
	if token == "" {
		if token, err = bridgeToken(); err != nil {
			return fail("db connect: %v", err)
		}
	}
	h := *host
	if *container {
		h = "host.docker.internal"
	}
	if h == "" {
		h = "127.0.0.1"
	}
	u, err := url.Parse(conn.URL)
	if err != nil {
		return fail("db connect: %v", err)
	}
	db := strings.TrimPrefix(u.Path, "/")

	if *cmdOnly {
		printConnCommand(conn.Type, token, h, conn.Port, db)
		return 0
	}

	// Rich view: raw tunnel link + ready-made links for common clients.
	fmt.Println(i18n.T("db.connect-head", name, conn.Type, conn.Port))
	raw, links, err := dbproxy.TunnelLinks(conn.Type, token, h, conn.Port, db, i18n.T)
	if err != nil {
		return fail("db connect: %s", i18n.T("db.connect-unsupported", conn.Type))
	}
	fmt.Println(i18n.T("db.connect-raw"))
	fmt.Printf("  %s\n", raw)
	for _, l := range links {
		fmt.Println(i18n.T("db.connect-" + l.Kind))
		fmt.Printf("  %s\n", l.Value)
	}
	return 0
}

// printConnCommand prints a single executable client command (for --cmd).
func printConnCommand(typ, token, host string, port int, db string) {
	switch typ {
	case "postgres":
		fmt.Printf("psql \"postgresql://%s:x@%s:%d/%s\"\n", token, host, port, db)
	case "mysql":
		fmt.Printf("mysql -h %s -P %d -u %s -px --ssl-mode=DISABLED %s\n", host, port, token, db)
	case "redis":
		fmt.Printf("redis-cli -h %s -p %d -a %s --no-auth-warning\n", host, port, token)
	}
}

// bridgeToken resolves the bridge token: env first, then ~/.vaulty/bridge-token.
func bridgeToken() (string, error) {
	if t := os.Getenv(bridge.EnvToken); t != "" {
		return t, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(bridge.TokenPath(home))
	if err != nil {
		return "", errors.New(i18n.T("db.bridge-token-missing", bridge.EnvToken))
	}
	return strings.TrimSpace(string(b)), nil
}

// dbShell opens an interactive native client on the host using the decrypted
// URL. TTY-only: in a script/AI context there is no point in opening an
// interactive shell, and the credentials must not flow through a non-interactive
// process.
func dbShell(args []string) int {
	fs := flag.NewFlagSet("db shell", flag.ContinueOnError)
	dir := fs.String("dir", "", "store directory")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 1 {
		return fail("db shell: %s", i18n.T("db.shell-usage"))
	}
	if !isTTY(os.Stdin.Fd()) {
		return fail("db shell: %s", i18n.T("db.shell-tty-only"))
	}
	name := fs.Arg(0)
	path, err := dbPath(*dir)
	if err != nil {
		return fail("db shell: %v", err)
	}
	key, err := apollo.DBKey()
	if err != nil {
		return fail("db shell: %v", err)
	}
	conn, err := dbproxy.Resolve(path, key, name)
	if err != nil {
		return fail("db shell: %v", err)
	}
	if err := openShell(conn); err != nil {
		return fail("db shell: %v", err)
	}
	return 0
}

// openShell runs the native client for a connection, passing credentials via
// environment variables (not argv) so they do not appear in ps output.
func openShell(conn dbproxy.Conn) error {
	u, err := url.Parse(conn.URL)
	if err != nil {
		return err
	}
	host, port := splitHostPort(u.Host, conn.Type)
	user := ""
	pass := ""
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	dbname := strings.TrimPrefix(u.Path, "/")

	switch conn.Type {
	case "postgres":
		env := append(os.Environ(),
			"PGHOST="+host, "PGPORT="+port, "PGUSER="+user, "PGPASSWORD="+pass, "PGDATABASE="+dbname)
		return runClient("psql", nil, env)
	case "mysql":
		args := []string{"-h", host, "-P", port, "-u", user, dbname}
		env := append(os.Environ(), "MYSQL_PWD="+pass)
		return runClient("mysql", args, env)
	case "redis":
		args := []string{"-h", host, "-p", port}
		if dbname != "" && dbname != "0" {
			args = append(args, "-n", dbname)
		}
		env := append(os.Environ(), "REDISCLI_AUTH="+pass)
		return runClient("redis-cli", args, env)
	default:
		return fmt.Errorf("%s", i18n.T("db.connect-unsupported", conn.Type))
	}
}

func runClient(bin string, args []string, env []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("%s", i18n.T("db.shell-bin-missing", bin))
		}
		return err
	}
	return nil
}

func splitHostPort(hostport, typ string) (host, port string) {
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		return h, p
	}
	switch typ {
	case "postgres":
		return hostport, "5432"
	case "mysql":
		return hostport, "3306"
	default:
		return hostport, "6379"
	}
}
