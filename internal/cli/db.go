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
	fmt.Fprintf(w, `
数据库 URL 加密落盘（独立 DB 密钥），永不向脚本/AI 显示。'vaulty-keeper serve'
为每个连接起一条 TCP 隧道；隔离容器内用原生客户端（psql/mysql/redis-cli）
连隧道端口，token 作用户名/AUTH 密码——token 不是真实数据库密码。
'db test' 验证注册的 URL 可认证（不打印它）；'db show' 只在你自己的终端打印。
`)
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
			msg := "db init --force 会用新密钥覆盖 Keychain 中的数据库密钥，已有的数据库连接将无法再解密，需要重新 db add。确认？"
			if !isTTY(os.Stdin.Fd()) {
				if !*yes {
					return fail("db init: %s（非 TTY 下请加 --yes 确认）", msg)
				}
			} else if !confirmYes(msg) {
				return fail("db init: 已取消")
			}
		}
	}
	if err := apollo.GenerateAndStoreDBKey(*force); err != nil {
		return fail("db init: %v", err)
	}
	fmt.Println("数据库密钥已创建")
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
		return fail("db add: 用法：echo '<url>' | vaulty-keeper db add <name> [--port <port>] [--test]")
	}
	name := fs.Arg(0)
	raw := readDSN()
	if raw == "" {
		return fail("db add: 请通过 stdin 提供数据库 URL（printf 'postgres://u:p@host:5432/db' | vaulty-keeper db add %s）", name)
	}
	typ, err := dbproxy.ConnTypeFromURL(raw)
	if err != nil {
		return fail("db add: %v", err)
	}
	if *test {
		if err := dbproxy.TestConn(dbproxy.Conn{Name: name, URL: raw, Type: typ}); err != nil {
			return fail("db add: 连接测试失败：%v（不会保存；请修正 URL 后重试，或去掉 --test 强制保存）", err)
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
	fmt.Printf("连接 %q（%s）已加密保存\n", name, typ)
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
			return fail("db list: %s（且未配置掩码代理 VAULTY_KEEPER_BRIDGE_ADDR/TOKEN）", dbListHint(path, localErr))
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
		fmt.Printf("%s (%s) :%d\n", c.Name, c.Type, c.Port)
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
	base := fmt.Sprintf("本地读取失败：%v", err)
	if _, serr := os.Stat(path); os.IsNotExist(serr) {
		return fmt.Sprintf("%s 不存在，请先 'vaulty-keeper db add <name>' 注册，或 --dir / VAULTY_KEEPER_DB_DIR 指向已有环境", path)
	}
	if strings.Contains(err.Error(), "未找到数据库密钥") {
		return base + "：请先运行 'vaulty-keeper db init'（或设置 VAULTY_KEEPER_DB_KEY）"
	}
	if strings.Contains(err.Error(), "解密") || strings.Contains(err.Error(), "cipher") {
		return fmt.Sprintf("%s 存在但解密失败（密钥不匹配）：该文件是用不同的 VAULTY_KEEPER_DB_KEY / Keychain 密钥创建的；请检查环境变量，或换用 --dir / VAULTY_KEEPER_DB_DIR 指向正确的 store", path)
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
			Name string `json:"name"`
			Type string `json:"type"`
			Port int    `json:"port"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil
	}
	out := make([]dbproxy.Conn, 0, len(res.Connections))
	for _, c := range res.Connections {
		out = append(out, dbproxy.Conn{Name: c.Name, Type: c.Type, Port: c.Port})
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
		return fail("db rm: 用法：vaulty-keeper db rm <name> [--yes]")
	}
	if !isTTY(os.Stdin.Fd()) && !*yes {
		return fail("db rm: 非 TTY 下需要确认（用 --yes）")
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
	fmt.Printf("连接 %q 已删除\n", name)
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
		return fail("db test: 用法：vaulty-keeper db test <name>")
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
		fmt.Fprintf(os.Stderr, "修复：printf '<正确URL>' | vaulty-keeper db add %s（端口保持不变）\n", name)
		return 1
	}
	user := ""
	if u, perr := url.Parse(conn.URL); perr == nil && u.User != nil {
		user = u.User.Username()
	}
	fmt.Printf("OK: %s (%s)", name, conn.Type)
	if user != "" {
		fmt.Printf(" user=%s", user)
	}
	if db := strings.TrimPrefix(mustURLPath(conn.URL), "/"); db != "" {
		fmt.Printf(" db=%s", db)
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

// dbShow prints the decrypted real URL for a connection. TTY-only: like the
// plaintext commands, it is never available to scripts/AI.
func dbShow(args []string) int {
	fs := flag.NewFlagSet("db show", flag.ContinueOnError)
	dir := fs.String("dir", "", "store directory")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 1 {
		return fail("db show: 用法：vaulty-keeper db show <name>")
	}
	if !isTTY(os.Stdin.Fd()) {
		return fail("db show: 仅可在交互式终端使用（真实连接信息只在你的终端里显示）")
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
	fmt.Printf("name: %s\n", conn.Name)
	fmt.Printf("type: %s\n", conn.Type)
	fmt.Printf("port: %d\n", conn.Port)
	fmt.Printf("url:  %s\n", conn.URL)
	fmt.Fprintln(os.Stderr, "⚠ 真实连接信息仅在你的终端可见；不要把这段输出发给 AI/脚本或存入日志")
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
		return fail("db connect: 用法：vaulty-keeper db connect <name> [--container] [--cmd]")
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
	token, err := bridgeToken()
	if err != nil {
		return fail("db connect: %v", err)
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
	fmt.Printf("# %s (%s) — token 是 bridge token，不是真实数据库密码（隧道端口 %d）\n", name, conn.Type, conn.Port)
	raw := rawTunnelURL(conn.Type, token, h, conn.Port, db)
	fmt.Println("原始隧道链接（AI / 其他工具可据此自行转换）:")
	fmt.Printf("  %s\n", raw)
	switch conn.Type {
	case "postgres":
		fmt.Println("psql —— PostgreSQL 自带的命令行客户端（装 PG 就有）：")
		fmt.Printf("  %s\n", raw)
		fmt.Println("DBeaver / DataGrip —— 通用数据库图形工具（贴 JDBC 链接）：")
		fmt.Printf("  jdbc:postgresql://%s:%d/%s?user=%s\n", h, conn.Port, db, token)
		fmt.Println("pgAdmin4 —— PostgreSQL 官方图形工具（填字段，不支持贴 URL）：")
		fmt.Printf("  Host=%s Port=%d Database=%s Username=%s 密码留空\n", h, conn.Port, db, token)
	case "mysql":
		fmt.Println("DBeaver / DataGrip —— 通用数据库图形工具（贴 JDBC 链接）：")
		fmt.Printf("  jdbc:mysql://%s:%d/%s?user=%s&password=x\n", h, conn.Port, db, token)
		fmt.Println("MySQL Workbench —— MySQL 官方图形工具（填字段，不支持贴 URL）：")
		fmt.Printf("  Hostname=%s Port=%d Default Schema=%s Username=%s 密码任意\n", h, conn.Port, db, token)
		fmt.Println("mysql —— MySQL 自带的命令行客户端（装 MySQL 就有）：")
		fmt.Printf("  mysql -h %s -P %d -u %s -px --ssl-mode=DISABLED %s\n", h, conn.Port, token, db)
	case "redis":
		fmt.Println("Redis Insight —— Redis 官方图形工具（贴 URL）：")
		fmt.Printf("  %s\n", raw)
		fmt.Println("redis-cli —— Redis 自带的命令行客户端（装 Redis 就有）：")
		fmt.Printf("  redis-cli -h %s -p %d -a %s --no-auth-warning\n", h, conn.Port, token)
	default:
		return fail("db connect: 不支持的数据库类型 %q", conn.Type)
	}
	return 0
}

// rawTunnelURL builds the canonical token-based tunnel URL.
func rawTunnelURL(typ, token, host string, port int, db string) string {
	switch typ {
	case "postgres":
		return fmt.Sprintf("postgresql://%s@%s:%d/%s", token, host, port, db)
	case "mysql":
		return fmt.Sprintf("mysql://%s:x@%s:%d/%s", token, host, port, db)
	case "redis":
		return fmt.Sprintf("redis://:%s@%s:%d/%s", token, host, port, db)
	}
	return ""
}

// printConnCommand prints a single executable client command (for --cmd).
func printConnCommand(typ, token, host string, port int, db string) {
	switch typ {
	case "postgres":
		fmt.Printf("psql \"postgresql://%s@%s:%d/%s\"\n", token, host, port, db)
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
		return "", errors.New("未找到 bridge token（请先运行 'vaulty-keeper serve'，或导出 " + bridge.EnvToken + "）")
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
		return fail("db shell: 用法：vaulty-keeper db shell <name>")
	}
	if !isTTY(os.Stdin.Fd()) {
		return fail("db shell: 仅可在交互式终端使用（连接信息只在你的终端里可见）")
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
		return fmt.Errorf("不支持的数据库类型 %q", conn.Type)
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
			return fmt.Errorf("未找到 %s（请先安装该客户端）", bin)
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
