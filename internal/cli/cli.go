package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"vaulty-keeper/internal/apollo"
	"vaulty-keeper/internal/app"
	"vaulty-keeper/internal/ui"
)

const Version = "0.5.0"

// bothKeys resolves both the snapshot key and the sensitive-value key.
func bothKeys() ([]byte, []byte, error) {
	snapKey, err := apollo.SnapshotKey()
	if err != nil {
		return nil, nil, err
	}
	sensitiveKey, err := apollo.SensitiveKey()
	if err != nil {
		return nil, nil, err
	}
	return snapKey, sensitiveKey, nil
}

func Run(args []string) int {
	if len(args) == 0 {
		ensureKeys()
		usage(os.Stdout)
		return 0
	}
	switch args[0] {
	case "apollo":
		return runApollo(args[1:])
	case "aes":
		return runAES(args[1:])
	case "sensitive":
		return runSensitive(args[1:])
	case "ui":
		return runUI(args[1:])
	case "serve":
		return runServe(args[1:])
	case "remote":
		return runRemote(args[1:])
	case "db":
		return runDB(args[1:])
	case "completion":
		return runCompletion(args[1:])
	case "version", "--version", "-v":
		fmt.Println("vaulty-keeper", Version)
		return 0
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "vaulty-keeper: unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "vaulty-keeper %s - personal AI toolbox\n\n", Version)
	fmt.Fprintln(w, "Usage: vaulty-keeper <command> [args] [flags]")
	fmt.Fprintln(w)
	printCommandTree(w)
}

// confirmKeyInit is overridable in tests to answer the init prompt without
// touching stdin.
var confirmKeyInit = confirmYes

// ensureKeys checks that the encryption keys are initialized and, on a TTY,
// offers to initialize any missing ones so a fresh setup is one command away.
// Non-TTY callers just get a hint. The DB key is only checked when a db store
// exists (db support is optional); env-override keys count as available.
func ensureKeys() {
	ensureDirs()
	ensureAESConfig()
	checkKey("快照", "vaulty-keeper apollo init", app.KeyAvailable, func() error { return app.InitKey(false) })
	checkKey("敏感值", "vaulty-keeper sensitive init", app.SensitiveKeyAvailable, func() error { return app.InitSensitiveKey(false) })
	if p, err := dbPath(""); err == nil {
		if _, err := os.Stat(p); err == nil {
			checkKey("数据库", "vaulty-keeper db init",
				func() bool { _, err := apollo.DBKey(); return err == nil },
				func() error { return apollo.GenerateAndStoreDBKey(false) })
		}
	}
}

// ensureDirs creates the data directories (~/.vaulty and ~/.vaulty/apollo)
// with 0700 so every later write works even on a brand-new machine.
func ensureDirs() {
	dir, err := snapDir("")
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: 创建数据目录失败："+err.Error()))
	}
}

// ensureAESConfig makes the named AES key/iv list usable out of the box: when
// it is missing or empty it writes a generated "default" entry (~/.vaulty/
// aes.json, 0600). Existing entries are never touched.
func ensureAESConfig() {
	entries, err := app.AESConfigList()
	if err != nil {
		fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: 读取 AES key/iv 列表失败："+err.Error()))
		return
	}
	if len(entries) > 0 {
		return
	}
	key, iv, err := app.GenKey(16, 16)
	if err != nil {
		fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: 生成 AES key/iv 失败："+err.Error()))
		return
	}
	if err := app.AESConfigAdd("default", key, iv); err != nil {
		fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: 初始化 AES key/iv 列表失败："+err.Error()))
		return
	}
	fmt.Println(dim("已初始化 AES key/iv 列表（~/.vaulty/aes.json，已生成 default 条目；用 aes list 查看，aes add / aes gen-key --name 添加更多）"))
}

func checkKey(label, cmd string, available func() bool, init func() error) {
	if available() {
		return
	}
	if !isTerminal() {
		fmt.Fprintf(os.Stderr, "提示：%s密钥未初始化，请运行 %s\n", label, cmd)
		return
	}
	if !confirmKeyInit(fmt.Sprintf("%s密钥未初始化，现在运行 %s 吗？", label, cmd)) {
		return
	}
	if err := init(); err != nil {
		fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: "+err.Error()))
		return
	}
	fmt.Println(green(fmt.Sprintf("%s密钥已创建（%s）", label, apollo.StoreName())))
}

// isTerminalFunc is overridable in tests to simulate a non-TTY stdin.
var isTerminalFunc = func() bool {
	return isTTY(os.Stdin.Fd())
}

func isTerminal() bool { return isTerminalFunc() }

func fail(format string, a ...any) int {
	fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: "+fmt.Sprintf(format, a...)))
	return 1
}

// parseFlags wraps fs.Parse to allow flags after positional args: Go's flag
// package stops at the first non-flag token, so flag tokens are reordered to
// the front while keeping "--flag value" pairs intact. All subcommands use
// ContinueOnError so failures report through fail() instead of hard-exiting.
// -h prints the command's full help (syntax + flags) and returns helped=true
// so the caller returns immediately without running the command. Returns the
// exit code for the caller to return.
func parseFlags(fs *flag.FlagSet, args []string) (int, bool) {
	fs.SetOutput(io.Discard)
	err := fs.Parse(normalizeArgs(fs, args))
	if err == nil {
		return 0, false
	}
	if errors.Is(err, flag.ErrHelp) {
		printCommandHelp(os.Stdout, fs)
		return 0, true
	}
	return fail("%s: %v", fs.Name(), err), false
}

// normalizeArgs reorders flag tokens to the front while keeping "--flag value"
// pairs intact: Go's flag package stops at the first non-flag token.
func normalizeArgs(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 1 && a[0] == '-' && a != "-" && isFlagToken(fs, a) {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && !isBoolFlag(fs, a) && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}

// isFlagToken reports whether a "-..." token should be treated as a flag
// rather than a positional value: registered flags, the "=value" form of
// registered flags, and Go's implicit help flags. Anything else (e.g. a value
// that happens to start with "-") stays positional, so `set prod K -1` works.
func isFlagToken(fs *flag.FlagSet, a string) bool {
	name := strings.TrimLeft(a, "-")
	if name == "h" || name == "help" {
		return true
	}
	if fs.Lookup(name) != nil {
		return true
	}
	if i := strings.Index(a, "="); i > 0 && fs.Lookup(strings.TrimLeft(a[:i], "-")) != nil {
		return true
	}
	return false
}

func isBoolFlag(fs *flag.FlagSet, name string) bool {
	f := fs.Lookup(strings.TrimLeft(name, "-"))
	if f == nil {
		return false
	}
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

func readInput(path string) (string, error) {
	if path == "" || path == "-" {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

func snapDir(flagDir string) (string, error) {
	if flagDir != "" {
		return flagDir, nil
	}
	if d := os.Getenv("VAULTY_KEEPER_APOLLO_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".vaulty", "apollo"), nil
}

func snapPath(dir, name, appID string) (string, error) {
	if err := apollo.ValidateSnapshotName(name); err != nil {
		return "", err
	}
	return apollo.SnapPath(dir, name, appID), nil
}

func mustSnapshot(path, name string) (*apollo.Snapshot, int) {
	s, err := apollo.Load(path)
	if err != nil {
		return nil, fail("加载快照 %q 失败：%v", name, err)
	}
	return s, 0
}

// ---- apollo ----

func runApollo(args []string) int {
	if len(args) == 0 {
		apolloUsage(os.Stderr)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return apolloInit(rest)
	case "import":
		return apolloImport(rest)
	case "list":
		return apolloList(rest)
	case "get":
		return apolloGet(rest)
	case "set":
		return apolloSet(rest)
	case "unset":
		return apolloUnset(rest)
	case "mark":
		return apolloMark(rest)
	case "compare":
		return apolloCompare(rest)
	case "reveal":
		return apolloReveal(rest)
	case "edit":
		return apolloEdit(rest)
	case "export":
		return apolloExport(rest)
	case "rm":
		return apolloRm(rest)
	case "help", "-h", "--help":
		apolloUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "vaulty-keeper apollo: unknown subcommand %q\n\n", sub)
		apolloUsage(os.Stderr)
		return 2
	}
}

func apolloUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	printDomainUsage(w, "apollo")
	fmt.Fprintf(w, `
<env> 是环境名，与 --appid <id> 一起寻址 {env}__{appid}.json；
不带 --appid 时读写旧版 {env}.json。快照默认存 ~/.vaulty/apollo/
（--dir 或 VAULTY_KEEPER_APOLLO_DIR 覆盖）。

明文命令（list/compare --reveal、reveal、export、edit）只在交互式终端可用；
脚本/AI 环境一律拒绝，加 --yes 也无法放行。非 TTY 下 get/list/compare 默认
全部掩码（反转默认），只有 set --plain / mark --plain 显式标记安全的 key
才给明文。

敏感值（名字或内容命中 password/token/secret/JWT/带凭据 URI）用敏感值密钥
加密（sensitive init），默认掩码；reveal 显示明文，也可 --key/--iv 解密外部
CryptoUtil AES 密文。rm 需要确认（TTY 提示，非 TTY 需 --yes）；import 覆盖
已有快照需 --force。
`)
}

func apolloInit(args []string) int {
	fs := flag.NewFlagSet("apollo init", flag.ContinueOnError)
	force := fs.Bool("force", false, "regenerate key even if one exists")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}

	if err := app.InitKey(*force); err != nil {
		return fail("apollo init: %v", err)
	}
	fmt.Println(green(fmt.Sprintf("snapshot key created and stored in %s", apollo.StoreName())))
	return 0
}

func apolloImport(args []string) int {
	fs := flag.NewFlagSet("apollo import", flag.ContinueOnError)
	name := fs.String("name", "", "snapshot name (optional; defaults to the file name)")
	appID := fs.String("appid", "", "Apollo app id (required)")
	legacyAppID := fs.String("app-id", "", "deprecated alias for --appid")
	dir := fs.String("dir", "", "snapshot directory")
	force := fs.Bool("force", false, "overwrite an existing snapshot")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if *appID == "" {
		*appID = *legacyAppID
	}

	if *name == "" {
		if fs.NArg() > 0 && fs.Arg(0) != "-" {
			base := filepath.Base(fs.Arg(0))
			*name = strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	if *name == "" {
		return fail("apollo import: --name 必填（或传入文件路径）")
	}
	if err := apollo.ValidateAppID(*appID); err != nil {
		return fail("apollo import: --appid 必填：%v", err)
	}
	src := "-"
	if fs.NArg() > 0 {
		src = fs.Arg(0)
	}
	text, err := readInput(src)
	if err != nil {
		return fail("apollo import: 读取输入失败：%v", err)
	}
	kvs, warnings := apollo.ParseKV(text)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if len(kvs) == 0 {
		return fail("apollo import: 未解析到任何键值条目")
	}
	key, sensitiveKey, err := bothKeys()
	if err != nil {
		return fail("apollo import: %v", err)
	}
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo import: %v", err)
	}
	if !*force {
		if _, err := os.Stat(apollo.SnapPath(dirPath, *name, *appID)); err == nil {
			if isTerminal() {
				fmt.Printf("快照 %q (appid %s) 已存在，覆盖? [y/N] ", *name, *appID)
				var ans string
				fmt.Scanln(&ans)
				if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
					fmt.Println("已取消")
					return 0
				}
			} else {
				return fail("apollo import: 快照 %q (appid %s) 已存在（用 --force 覆盖）", *name, *appID)
			}
		}
	}
	n, err := app.Import(dirPath, *name, *appID, text, key, sensitiveKey)
	if err != nil {
		return fail("apollo import: %v", err)
	}
	fmt.Println(green(fmt.Sprintf("imported %d entries into snapshot %q (appid %s) (%s)", n, *name, *appID, apollo.SnapPath(dirPath, *name, *appID))))
	return 0
}

func apolloList(args []string) int {
	fs := flag.NewFlagSet("apollo list", flag.ContinueOnError)
	reveal := fs.Bool("reveal", false, "show plaintext values")
	yes := fs.Bool("yes", false, "deprecated: plaintext is TTY-only; --yes no longer enables it when piped")
	jsonOut := fs.Bool("json", false, "output JSON")
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id (default: legacy {env}.json)")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	_ = *yes
	if *reveal && !isTerminal() {
		return fail("apollo list: 明文输出仅在交互式终端可用；脚本/AI 环境永远拿不到明文")
	}

	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo list: %v", err)
	}
	if fs.NArg() == 0 {
		refs, err := apollo.ListSnapshots(dirPath)
		if err != nil {
			return fail("apollo list: %v", err)
		}
		for _, r := range refs {
			if r.AppID != "" {
				fmt.Printf("%s (%s)\n", r.Name, r.AppID)
			} else {
				fmt.Println(r.Name)
			}
		}
		return 0
	}
	name := fs.Arg(0)
	snapKey, sensitiveKey, err := bothKeys()
	if err != nil {
		return fail("apollo list: %v", err)
	}
	path, err := snapPath(dirPath, name, *appID)
	if err != nil {
		return fail("apollo list: %v", err)
	}
	s, code := mustSnapshot(path, name)
	if code != 0 {
		return code
	}
	keys := make([]string, 0, len(s.Items))
	for k := range s.Items {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	decrypted := map[string]string{}
	for _, k := range keys {
		v, err := s.DecryptItem(s.Items[k], snapKey, sensitiveKey)
		if err != nil {
			return fail("apollo list: %v", err)
		}
		decrypted[k] = v
	}

	if *jsonOut {
		items := map[string]string{}
		for _, k := range keys {
			if maskedFor(s.Items[k], k, decrypted[k], *reveal) {
				items[k] = apollo.MaskWithLen(len(decrypted[k]))
			} else {
				items[k] = decrypted[k]
			}
		}
		b, err := json.MarshalIndent(map[string]any{
			"name":   name,
			"app_id": s.Meta.AppID,
			"items":  items,
		}, "", "  ")
		if err != nil {
			return fail("apollo list: %v", err)
		}
		fmt.Println(string(b))
		return 0
	}

	for _, k := range keys {
		if maskedFor(s.Items[k], k, decrypted[k], *reveal) {
			fmt.Printf("%s = %s\n", k, apollo.MaskWithLen(len(decrypted[k])))
			continue
		}
		fmt.Printf("%s = %s\n", k, decrypted[k])
	}
	return 0
}

// maskedFor reports whether a value must be masked in output. In a
// non-interactive (script/AI) context the default is reversed: only values
// explicitly marked safe (set --plain) are shown in plaintext, regardless of
// how the key name looks. In an interactive terminal the legacy heuristic
// applies (sensitive names / inline credentials masked unless --reveal).
func maskedFor(it apollo.Item, k, v string, reveal bool) bool {
	if reveal {
		return false
	}
	if !isTerminal() {
		return !it.Safe
	}
	return it.Secret || apollo.IsSensitiveKeyValue(k, v)
}

func apolloGet(args []string) int {
	fs := flag.NewFlagSet("apollo get", flag.ContinueOnError)
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id (default: legacy {env}.json)")
	yes := fs.Bool("yes", false, "deprecated: plaintext is TTY-only; --yes no longer enables it when piped")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	_ = *yes
	if fs.NArg() != 2 {
		return fail("apollo get: 用法：vaulty-keeper apollo get <name> <key>")
	}
	name, k := fs.Arg(0), fs.Arg(1)
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo get: %v", err)
	}
	key, sensitiveKey, err := bothKeys()
	if err != nil {
		return fail("apollo get: %v", err)
	}
	v, safe, ok, err := app.GetValueSafe(dirPath, name, *appID, key, sensitiveKey, k)
	if err != nil {
		return fail("apollo get: %v", err)
	}
	if !ok {
		return fail("apollo get: 快照 %q 中不存在 key %q", k, name)
	}
	// Reverse default: scripts/AI only receive plaintext for keys the user
	// explicitly marked safe (set --plain); everything else is masked, no
	// matter how the key name looks.
	if !isTerminal() && !safe {
		fmt.Println(apollo.MaskWithLen(len(v)))
		return 0
	}
	fmt.Println(v)
	return 0
}

func apolloSet(args []string) int {
	fs := flag.NewFlagSet("apollo set", flag.ContinueOnError)
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id (default: legacy {env}.json)")
	asSecret := fs.Bool("secret", false, "mark as sensitive (masked by default)")
	asPlain := fs.Bool("plain", false, "mark as non-sensitive")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 3 {
		return fail("apollo set: 用法：vaulty-keeper apollo set <name> <key> <value>")
	}
	name, k, v := fs.Arg(0), fs.Arg(1), fs.Arg(2)
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo set: %v", err)
	}
	key, sensitiveKey, err := bothKeys()
	if err != nil {
		return fail("apollo set: %v", err)
	}
	var secret *bool
	switch {
	case *asSecret:
		t := true
		secret = &t
	case *asPlain:
		t := false
		secret = &t
		if err := guardPlainMark(k, v); err != nil {
			return fail("apollo set: %v", err)
		}
	}
	if _, err := app.SetValue(dirPath, name, *appID, k, v, secret, key, sensitiveKey); err != nil {
		return fail("apollo set: %v", err)
	}
	fmt.Println(green(fmt.Sprintf("set %s.%s", name, k)))
	return 0
}

// guardPlainMark blocks (non-TTY) or asks for confirmation (TTY) when someone
// tries to mark a sensitive-looking key/value as safe (--plain), preventing
// accidental leaks of password/token/secret/JWT values to scripts/AI. Reading
// stays name-agnostic (reverse default masks everything); this guard only
// restricts the explicit opt-in, which must be a human decision.
func guardPlainMark(key, value string) error {
	if !apollo.IsSensitiveKeyValue(key, value) {
		return nil
	}
	if !isTerminal() {
		return fmt.Errorf("拒绝将 %q 标记为对脚本/AI 安全：key 名或值看起来是敏感内容；此操作必须在交互式终端确认", key)
	}
	fmt.Printf("注意：%q 看起来是敏感值（名字或内容命中敏感规则）。确认要标记为安全、允许 AI/脚本读明文？[y/N] ", key)
	var ans string
	fmt.Scanln(&ans)
	if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
		return errors.New("已取消：未标记为安全")
	}
	return nil
}

func apolloUnset(args []string) int {
	fs := flag.NewFlagSet("apollo unset", flag.ContinueOnError)
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id (default: legacy {env}.json)")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 2 {
		return fail("apollo unset: 用法：vaulty-keeper apollo unset <name> <key>")
	}
	name, k := fs.Arg(0), fs.Arg(1)
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo unset: %v", err)
	}
	ok, err := app.DeleteValue(dirPath, name, *appID, k, nil)
	if err != nil {
		return fail("apollo unset: %v", err)
	}
	if !ok {
		return fail("apollo unset: 快照 %q 中不存在 key %q", k, name)
	}
	fmt.Println(green(fmt.Sprintf("unset %s.%s", name, k)))
	return 0
}

func apolloMark(args []string) int {
	fs := flag.NewFlagSet("apollo mark", flag.ContinueOnError)
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id (default: legacy {env}.json)")
	asPlain := fs.Bool("plain", false, "mark as safe for scripts/AI (plaintext visible)")
	asSecret := fs.Bool("secret", false, "mark as sensitive (masked for scripts/AI)")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if *asPlain == *asSecret {
		return fail("apollo mark: 必须且只能指定 --plain 或 --secret 之一")
	}
	if fs.NArg() != 2 {
		return fail("apollo mark: 用法：vaulty-keeper apollo mark <name> <key> --plain|--secret")
	}
	name, k := fs.Arg(0), fs.Arg(1)
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo mark: %v", err)
	}
	key, sensitiveKey, err := bothKeys()
	if err != nil {
		return fail("apollo mark: %v", err)
	}
	if *asPlain {
		v, _, ok, err := app.GetValueSafe(dirPath, name, *appID, key, sensitiveKey, k)
		if err != nil {
			return fail("apollo mark: %v", err)
		}
		if ok {
			if err := guardPlainMark(k, v); err != nil {
				return fail("apollo mark: %v", err)
			}
		}
	}
	ok, err := app.MarkValue(dirPath, name, *appID, k, *asPlain, key, sensitiveKey)
	if err != nil {
		return fail("apollo mark: %v", err)
	}
	if !ok {
		return fail("apollo mark: 快照 %q 中不存在 key %q", k, name)
	}
	label := "safe (--plain)"
	if *asSecret {
		label = "sensitive (--secret)"
	}
	fmt.Println(green(fmt.Sprintf("mark %s.%s as %s", name, k, label)))
	return 0
}

func apolloCompare(args []string) int {
	fs := flag.NewFlagSet("apollo compare", flag.ContinueOnError)
	reveal := fs.Bool("reveal", false, "show plaintext values")
	yes := fs.Bool("yes", false, "deprecated: plaintext is TTY-only; --yes no longer enables it when piped")
	jsonOut := fs.Bool("json", false, "output JSON")
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id for the first snapshot")
	appIDTo := fs.String("appid-to", "", "app id for the second snapshot")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	_ = *yes
	if *reveal && !isTerminal() {
		return fail("apollo compare: 明文输出仅在交互式终端可用；脚本/AI 环境永远拿不到明文")
	}
	if fs.NArg() != 2 {
		return fail("apollo compare: 用法：vaulty-keeper apollo compare <nameA> <nameB>")
	}
	nameA, nameB := fs.Arg(0), fs.Arg(1)
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo compare: %v", err)
	}
	key, sensitiveKey, err := bothKeys()
	if err != nil {
		return fail("apollo compare: %v", err)
	}
	changes, err := app.Compare(dirPath, nameA, *appID, nameB, *appIDTo, key, sensitiveKey)
	if err != nil {
		return fail("apollo compare: %v", err)
	}
	if len(changes) == 0 {
		fmt.Printf("snapshots %q and %q are identical\n", nameA, nameB)
		return 0
	}
	val := func(c apollo.Change, which string) string {
		v := c.Old
		if which == "new" {
			v = c.New
		}
		if *reveal {
			return v
		}
		if !isTerminal() {
			if !c.Safe {
				return apollo.MaskWithLen(len(v))
			}
			return v
		}
		if c.Secret || apollo.IsSensitiveKeyValue(c.Key, v) {
			return apollo.MaskWithLen(len(v))
		}
		return v
	}

	if *jsonOut {
		added := map[string]string{}
		removed := map[string]string{}
		changed := map[string]any{}
		for _, c := range changes {
			switch c.Kind {
			case "added":
				added[c.Key] = val(c, "new")
			case "removed":
				removed[c.Key] = val(c, "old")
			case "changed":
				changed[c.Key] = map[string]string{"old": val(c, "old"), "new": val(c, "new")}
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
			return fail("apollo compare: %v", err)
		}
		fmt.Println(string(b))
		return 0
	}

	for _, c := range changes {
		switch c.Kind {
		case "added":
			fmt.Println(green(fmt.Sprintf("+ %s = %s", c.Key, val(c, "new"))))
		case "removed":
			fmt.Println(red(fmt.Sprintf("- %s = %s", c.Key, val(c, "old"))))
		case "changed":
			fmt.Println(yellow(fmt.Sprintf("~ %s: %s -> %s", c.Key, val(c, "old"), val(c, "new"))))
		}
	}
	return 0
}

func apolloExport(args []string) int {
	fs := flag.NewFlagSet("apollo export", flag.ContinueOnError)
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id (default: legacy {env}.json)")
	copyToClip := fs.Bool("copy", false, "copy to clipboard (macOS pbcopy)")
	yes := fs.Bool("yes", false, "deprecated: plaintext is TTY-only; --yes no longer enables it when piped")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	_ = *yes
	if !isTerminal() {
		return fail("apollo export: 明文输出仅在交互式终端可用；脚本/AI 环境永远拿不到明文")
	}
	if fs.NArg() != 1 {
		return fail("apollo export: 用法：vaulty-keeper apollo export <name>")
	}
	name := fs.Arg(0)
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo export: %v", err)
	}
	key, sensitiveKey, err := bothKeys()
	if err != nil {
		return fail("apollo export: %v", err)
	}
	text, err := app.Export(dirPath, name, *appID, key, sensitiveKey)
	if err != nil {
		return fail("apollo export: %v", err)
	}
	var out strings.Builder
	out.WriteString(text)
	fmt.Print(out.String())
	if *copyToClip {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(out.String())
		if err := cmd.Run(); err != nil {
			return fail("apollo export: pbcopy 失败：%v", err)
		}
		fmt.Fprintln(os.Stderr, "copied to clipboard")
	}
	return 0
}

// apolloRm deletes a snapshot file after confirmation.
func apolloRm(args []string) int {
	fs := flag.NewFlagSet("apollo rm", flag.ContinueOnError)
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id (required)")
	yes := fs.Bool("yes", false, "skip confirmation (required when piped)")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if fs.NArg() != 1 {
		return fail("apollo rm: 用法：vaulty-keeper apollo rm <name> --appid <id>")
	}
	name := fs.Arg(0)
	if err := apollo.ValidateAppID(*appID); err != nil {
		return fail("apollo rm: --appid is required: %v", err)
	}
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo rm: %v", err)
	}
	if !*yes && isTerminal() {
		fmt.Printf("删除快照 %q (appid %s)? [y/N] ", name, *appID)
		var ans string
		fmt.Scanln(&ans)
		if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
			fmt.Println("已取消")
			return 0
		}
	} else if !*yes {
		return fail("apollo rm: 非 TTY 下需要确认（用 --yes）")
	}
	ok, err := app.Remove(dirPath, name, *appID)
	if err != nil {
		return fail("apollo rm: %v", err)
	}
	if !ok {
		return fail("apollo rm: 未找到快照 %q (appid %s)", name, *appID)
	}
	fmt.Println(green(fmt.Sprintf("removed snapshot %q (appid %s)", name, *appID)))
	return 0
}

// apolloReveal shows plaintext for snapshot keys (sensitive values via the
// sensitive key), or decrypts an external CryptoUtil AES ciphertext when
// --key/--iv are given.
func apolloReveal(args []string) int {
	fs := flag.NewFlagSet("apollo reveal", flag.ContinueOnError)
	dir := fs.String("dir", "", "snapshot directory")
	jsonOut := fs.Bool("json", false, "output JSON")
	appID := fs.String("appid", "", "app id (default: legacy {env}.json)")
	keyFlag := fs.String("key", "", "AES secret key override (env VAULTY_KEEPER_AES_KEY)")
	ivFlag := fs.String("iv", "", "AES iv override (env VAULTY_KEEPER_AES_IV)")
	yes := fs.Bool("yes", false, "deprecated: plaintext is TTY-only; --yes no longer enables it when piped")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	_ = *yes
	if !isTerminal() {
		return fail("apollo reveal: 明文输出仅在交互式终端可用；脚本/AI 环境永远拿不到明文")
	}
	if fs.NArg() < 2 {
		return fail("apollo reveal: 用法：vaulty-keeper apollo reveal <name> <key...>")
	}
	name := fs.Arg(0)
	targets := fs.Args()[1:]

	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo reveal: %v", err)
	}
	snapKey, sensitiveKey, err := bothKeys()
	if err != nil {
		return fail("apollo reveal: %v", err)
	}
	plain, err := app.Reveal(dirPath, name, *appID, snapKey, sensitiveKey, targets, *keyFlag, *ivFlag)
	if err != nil {
		return fail("apollo reveal: %v", err)
	}

	if *jsonOut {
		b, err := json.MarshalIndent(plain, "", "  ")
		if err != nil {
			return fail("apollo reveal: %v", err)
		}
		fmt.Println(string(b))
		return 0
	}
	if len(targets) == 1 {
		fmt.Println(cyan(plain[targets[0]]))
		return 0
	}
	for _, k := range targets {
		fmt.Printf("%s = %s\n", k, plain[k])
	}
	return 0
}

// apolloEdit opens a snapshot as plaintext in $EDITOR; on save it re-encrypts.
// Plaintext is only available in an interactive terminal: in script/AI
// contexts the whole command is refused, so an agent cannot dump every value
// in plaintext (e.g. via EDITOR=cat) even when asked to.
func apolloEdit(args []string) int {
	fs := flag.NewFlagSet("apollo edit", flag.ContinueOnError)
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id (default: legacy {env}.json)")
	editor := fs.String("editor", "", "editor binary (default $EDITOR or vi)")
	yes := fs.Bool("yes", false, "deprecated: plaintext is TTY-only; --yes no longer enables it when piped")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	_ = *yes
	if !isTerminal() {
		return fail("apollo edit: 明文仅在交互式终端可用；脚本/AI 环境永远拿不到明文")
	}
	if fs.NArg() != 1 {
		return fail("apollo edit: 用法：vaulty-keeper apollo edit <name>")
	}
	name := fs.Arg(0)
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo edit: %v", err)
	}
	snapKey, sensitiveKey, err := bothKeys()
	if err != nil {
		return fail("apollo edit: %v", err)
	}
	text, err := app.EditLoad(dirPath, name, *appID, snapKey, sensitiveKey)
	if err != nil {
		return fail("apollo edit: %v", err)
	}

	tmp, err := os.CreateTemp("", "vaulty-keeper-edit-*.txt")
	if err != nil {
		return fail("apollo edit: %v", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return fail("apollo edit: %v", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fail("apollo edit: %v", err)
	}
	tmp.Close()

	ed := *editor
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "vi"
	}
	cmd := exec.Command("sh", "-c", ed+" "+strconv.Quote(tmpPath))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fail("apollo edit: 编辑器失败：%v", err)
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return fail("apollo edit: %v", err)
	}
	_, warnings := apollo.ParseKV(string(content))
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	n, err := app.EditApply(dirPath, name, *appID, snapKey, sensitiveKey, string(content))
	if err != nil {
		return fail("apollo edit: %v", err)
	}
	fmt.Println(green(fmt.Sprintf("updated snapshot %q: %d entries", name, n)))
	return 0
}

// ---- sensitive ----

func runSensitive(args []string) int {
	if len(args) == 0 {
		sensitiveUsage(os.Stderr)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return sensitiveInit(rest)
	case "help", "-h", "--help":
		sensitiveUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "vaulty-keeper sensitive: unknown subcommand %q\n\n", sub)
		sensitiveUsage(os.Stderr)
		return 2
	}
}

func sensitiveUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	printDomainUsage(w, "sensitive")
	fmt.Fprintf(w, `
敏感值密钥加密快照中的敏感值（独立于快照密钥），掩码值只能靠它解开
（env 覆盖：VAULTY_KEEPER_SENSITIVE_KEY）。
`)
}

func sensitiveInit(args []string) int {
	fs := flag.NewFlagSet("sensitive init", flag.ContinueOnError)
	force := fs.Bool("force", false, "regenerate even if a key already exists")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if err := app.InitSensitiveKey(*force); err != nil {
		return fail("sensitive init: %v", err)
	}
	fmt.Println(green(fmt.Sprintf("sensitive key created in %s", apollo.StoreName())))
	return 0
}

// ---- aes ----

func runAES(args []string) int {
	if len(args) == 0 {
		aesUsage(os.Stderr)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "encrypt":
		return aesOp(rest, true)
	case "decrypt":
		return aesOp(rest, false)
	case "gen-key":
		return aesGenKey(rest)
	case "list":
		return aesList(rest)
	case "add":
		return aesAdd(rest)
	case "help", "-h", "--help":
		aesUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "vaulty-keeper aes: unknown subcommand %q\n\n", sub)
		aesUsage(os.Stderr)
		return 2
	}
}

func aesUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	printDomainUsage(w, "aes")
	fmt.Fprintf(w, `
key/iv 条目存于 ~/.vaulty/aes.json（{name, secret-key, iv} 数组）。
解析顺序：--key/--iv → --name（查 aes.json）→ VAULTY_KEEPER_AES_KEY / VAULTY_KEEPER_AES_IV。
算法：AES/GCM/NoPadding，tag 128 bits，key 16/24/32 字节（UTF-8），iv 为
UTF-8 字节（Java CryptoUtil 兼容）。decrypt 输出明文，仅交互式终端可用
（脚本/AI 环境一律拒绝）。
`)
}

func aesList(args []string) int {
	fs := flag.NewFlagSet("aes list", flag.ContinueOnError)
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	entries, err := app.AESConfigList()
	if err != nil {
		return fail("aes list: %v", err)
	}
	if len(entries) == 0 {
		fmt.Println("(no entries)")
		return 0
	}
	for _, e := range entries {
		if !isTerminal() {
			// scripts/AI only learn entry names; stored AES keys/ivs are
			// masked so they never enter session logs.
			fmt.Printf("%s\t%s\t%s\n", e.Name, apollo.MaskWithLen(len(e.SecretKey)), apollo.MaskWithLen(len(e.IV)))
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", e.Name, e.SecretKey, e.IV)
	}
	return 0
}

func aesAdd(args []string) int {
	fs := flag.NewFlagSet("aes add", flag.ContinueOnError)
	name := fs.String("name", "", "entry name (required)")
	key := fs.String("key", "", "secret key (required)")
	iv := fs.String("iv", "", "iv string (required)")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if *name == "" {
		return fail("aes add: --name 必填")
	}
	if err := app.AESConfigAdd(*name, *key, *iv); err != nil {
		return fail("aes add: %v", err)
	}
	fmt.Println(green(fmt.Sprintf("saved entry %q to %s", *name, app.AESConfigPath())))
	return 0
}

func aesGenKey(args []string) int {
	fs := flag.NewFlagSet("aes gen-key", flag.ContinueOnError)
	keyBytes := fs.Int("bytes", 16, "key length in bytes (16/24/32)")
	ivBytes := fs.Int("iv-bytes", 16, "iv length in bytes (12/16)")
	name := fs.String("name", "", "save the generated key/iv to aes.json under this name")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	if *keyBytes != 16 && *keyBytes != 24 && *keyBytes != 32 {
		return fail("aes gen-key: key 长度必须为 16/24/32 字节")
	}
	if *ivBytes != 12 && *ivBytes != 16 {
		return fail("aes gen-key: iv 长度必须为 12 或 16 字节")
	}
	key, iv, err := app.GenKey(*keyBytes, *ivBytes)
	if err != nil {
		return fail("aes gen-key: %v", err)
	}
	if *name != "" {
		if err := app.AESConfigAdd(*name, key, iv); err != nil {
			return fail("aes gen-key: %v", err)
		}
		fmt.Println(green(fmt.Sprintf("saved entry %q to %s", *name, app.AESConfigPath())))
	}
	fmt.Printf("SECRET_KEY: %s\n", key)
	fmt.Printf("IV: %s\n", iv)
	return 0
}

func aesOp(args []string, encrypt bool) int {
	fsName := "aes encrypt"
	if !encrypt {
		fsName = "aes decrypt"
	}
	fs := flag.NewFlagSet(fsName, flag.ContinueOnError)
	key := fs.String("key", "", "secret key (UTF-8, 16/24/32 bytes); env VAULTY_KEEPER_AES_KEY")
	iv := fs.String("iv", "", "iv string (UTF-8); env VAULTY_KEEPER_AES_IV")
	name := fs.String("name", "", "use key/iv from the named aes.json entry")
	file := fs.String("file", "", "read input from file (supports multi-line)")
	yes := fs.Bool("yes", false, "deprecated: plaintext is TTY-only; --yes no longer enables it when piped")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}
	_ = *yes
	if !encrypt && !isTerminal() {
		return fail("aes decrypt: 明文输出仅在交互式终端可用；脚本/AI 环境永远拿不到明文")
	}

	k := *key
	i := *iv
	if (k == "" || i == "") && *name != "" {
		e, err := app.AESConfigGet(*name)
		if err != nil {
			return fail("aes: %v", err)
		}
		if e == nil {
			return fail("aes: %s 中不存在条目 %q", *name, app.AESConfigPath())
		}
		if k == "" {
			k = e.SecretKey
		}
		if i == "" {
			i = e.IV
		}
	}
	if k == "" {
		k = os.Getenv("VAULTY_KEEPER_AES_KEY")
	}
	if i == "" {
		i = os.Getenv("VAULTY_KEEPER_AES_IV")
	}
	if k == "" || i == "" {
		return fail("aes: --key/--iv（或 --name，或 VAULTY_KEEPER_AES_KEY/VAULTY_KEEPER_AES_IV）必填")
	}

	var input string
	if *file != "" {
		b, err := os.ReadFile(*file)
		if err != nil {
			return fail("aes: 读取文件失败：%v", err)
		}
		input = strings.TrimRight(string(b), "\r\n")
	} else if fs.NArg() > 0 {
		input = strings.Join(fs.Args(), " ")
	} else {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fail("aes: 读取标准输入失败：%v", err)
		}
		input = strings.TrimSpace(string(b))
	}

	var (
		out string
		err error
	)
	if encrypt {
		out, err = app.Encrypt(k, i, input)
	} else {
		out, err = app.Decrypt(k, i, input)
	}
	if err != nil {
		return fail("aes: %v", err)
	}
	fmt.Println(out)
	return 0
}

// ---- ui ----

func parseUIPort(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 65535 {
		return 0, errors.New("端口必须是 0 到 65535 之间的整数")
	}
	return n, nil
}

func runUI(args []string) int {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	dirFlag := fs.String("dir", "", "snapshot directory")
	portFlag := fs.String("port", "8080", "port (default 8080; rolls forward if busy)")
	noOpen := fs.Bool("no-open", false, "do not open a browser")
	allowPlain := fs.Bool("allow-plaintext", false, "enable plaintext endpoints (reveal/export/edit/AES decrypt)")
	if code, helped := parseFlags(fs, args); helped || code != 0 {
		return code
	}

	dirPath, err := snapDir(*dirFlag)
	if err != nil {
		return fail("ui: %v", err)
	}
	port, err := parseUIPort(*portFlag)
	if err != nil {
		return fail("ui: %v", err)
	}
	dbStore, err := dbPath("")
	if err != nil {
		return fail("ui: %v", err)
	}
	if err := ui.Start(context.Background(), ui.Config{
		Dir:            dirPath,
		AllowPlaintext: *allowPlain,
		DBStore:        dbStore,
		DBKey:          apollo.DBKey,
	}, port, !*noOpen, os.Stdout); err != nil {
		return fail("ui: %v", err)
	}
	return 0
}

// ---- completion ----

const completionZsh = `#compdef vaulty-keeper
_vaulty_keeper() {
  local -a commands
  commands=(
    'apollo:Apollo snapshot tool (encrypted at rest)'
    'aes:AES/GCM encrypt/decrypt (Java CryptoUtil compatible)'
    'sensitive:sensitive-value key management'
    'ui:local web UI'
    'serve:masked-only bridge for isolated agents'
    'remote:masked reads via the bridge'
    'completion:print shell completion'
    'version:show version'
    'help:show help'
  )
  if (( CURRENT == 2 )); then
    _describe 'command' commands
    return
  fi
  case $words[2] in
    apollo)
      local -a subs
      subs=(
        'init:create snapshot key'
        'import:import pasted KV'
        'list:list snapshots or keys'
        'get:get a value'
        'set:set a value'
        'unset:unset a value'
        'mark:mark a key safe/sensitive'
        'compare:compare two snapshots'
        'reveal:show plaintext values'
        'edit:edit snapshot in $EDITOR'
        'export:export plaintext KV'
      )
      if (( CURRENT == 3 )); then _describe 'subcommand' subs; fi
      ;;
    aes)
      local -a subs
      subs=('encrypt:encrypt plaintext' 'decrypt:decrypt base64' 'gen-key:generate key/iv' 'list:list saved entries' 'add:add a saved entry')
      if (( CURRENT == 3 )); then _describe 'subcommand' subs; fi
      ;;
    sensitive)
      local -a subs
      subs=('init:create sensitive-value key')
      if (( CURRENT == 3 )); then _describe 'subcommand' subs; fi
      ;;
    remote)
      local -a subs
      subs=('list:list snapshots or keys (masked)' 'get:get a masked value' 'compare:compare two snapshots (masked)' 'dblist:list database tunnels')
      if (( CURRENT == 3 )); then _describe 'subcommand' subs; fi
      ;;
    db)
      local -a subs
      subs=('init:create database key' 'add:register a connection' 'list:list connections' 'test:check a connection works' 'connect:print ready client command' 'show:print real URL (TTY)' 'rm:remove a connection' 'shell:open interactive shell')
      if (( CURRENT == 3 )); then _describe 'subcommand' subs; fi
      ;;
  esac
}
compdef _vaulty_keeper vaulty-keeper
`

const completionBash = `_vaulty_keeper() {
  local cur
  cur="${COMP_WORDS[COMP_CWORD]}"
  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "apollo aes sensitive ui serve remote db completion version help" -- "${cur}") )
    return
  fi
  case "${COMP_WORDS[1]}" in
    apollo)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=( $(compgen -W "init import list get set unset mark compare reveal edit export help" -- "${cur}") )
      fi
      ;;
    aes)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=( $(compgen -W "encrypt decrypt gen-key list add help" -- "${cur}") )
      fi
      ;;
    sensitive)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=( $(compgen -W "init help" -- "${cur}") )
      fi
      ;;
    remote)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=( $(compgen -W "list get compare dblist help" -- "${cur}") )
      fi
      ;;
    db)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=( $(compgen -W "init add list test regen connect show rm shell help" -- "${cur}") )
      fi
      ;;
  esac
}
complete -F _vaulty_keeper vaulty-keeper
`

const completionFish = `complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a apollo -d 'Apollo snapshot tool'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a aes -d 'AES/GCM encrypt/decrypt'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a sensitive -d 'sensitive-value key management'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a ui -d 'local web UI'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a serve -d 'masked-only bridge for isolated agents'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a remote -d 'masked reads via the bridge'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a db -d 'encrypted database connections + tunnels'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a completion -d 'print shell completion'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a version -d 'show version'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a help -d 'show help'
complete -c vaulty-keeper -f -n '__fish_seen_subcommand_from apollo' -a 'init import list get set unset mark compare reveal edit export help'
complete -c vaulty-keeper -f -n '__fish_seen_subcommand_from aes' -a 'encrypt decrypt gen-key list add help'
complete -c vaulty-keeper -f -n '__fish_seen_subcommand_from sensitive' -a 'init help'
complete -c vaulty-keeper -f -n '__fish_seen_subcommand_from remote' -a 'list get compare dblist help'
complete -c vaulty-keeper -f -n '__fish_seen_subcommand_from db' -a 'init add list test regen connect show rm shell help'
`

func runCompletion(args []string) int {
	if len(args) != 1 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(os.Stderr, "usage: vaulty-keeper completion <zsh|bash|fish>")
		fmt.Fprintln(os.Stderr, "print a shell completion script; add it to your shell config, e.g.:")
		fmt.Fprintln(os.Stderr, "  vaulty-keeper completion zsh | source /dev/stdin")
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "zsh":
		fmt.Print(completionZsh)
	case "bash":
		fmt.Print(completionBash)
	case "fish":
		fmt.Print(completionFish)
	default:
		fmt.Fprintf(os.Stderr, "unsupported shell %q (use zsh, bash or fish)\n", args[0])
		return 2
	}
	return 0
}
