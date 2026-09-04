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
	"vaulty-keeper/internal/i18n"
	"vaulty-keeper/internal/ui"
)

const Version = "0.6.0"

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
	i18n.Init()
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
	case "lang":
		return runLang(args[1:])
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
	fmt.Fprintln(w, i18n.T("cli.tagline", Version))
	fmt.Fprintln(w)
	fmt.Fprintln(w, i18n.T("cli.usage-line"))
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
	checkKey(i18n.T("cli.key-snapshot"), "vaulty-keeper apollo init", app.KeyAvailable, func() error { return app.InitKey(false) })
	checkKey(i18n.T("cli.key-sensitive"), "vaulty-keeper sensitive init", app.SensitiveKeyAvailable, func() error { return app.InitSensitiveKey(false) })
	if p, err := dbPath(""); err == nil {
		if _, err := os.Stat(p); err == nil {
			checkKey(i18n.T("cli.key-db"), "vaulty-keeper db init",
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
		fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: "+i18n.T("cli.mkdir-failed", err.Error())))
	}
}

// ensureAESConfig makes the named AES key/iv list usable out of the box: when
// it is missing or empty it writes a generated "default" entry (~/.vaulty/
// aes.json, 0600). Existing entries are never touched.
func ensureAESConfig() {
	entries, err := app.AESConfigList()
	if err != nil {
		fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: "+i18n.T("cli.aes-list-failed", err.Error())))
		return
	}
	if len(entries) > 0 {
		return
	}
	key, iv, err := app.GenKey(16, 16)
	if err != nil {
		fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: "+i18n.T("cli.aes-gen-failed", err.Error())))
		return
	}
	if err := app.AESConfigAdd("default", key, iv); err != nil {
		fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: "+i18n.T("cli.aes-init-failed", err.Error())))
		return
	}
	fmt.Println(dim(i18n.T("cli.aes-initialized")))
}

func checkKey(label, cmd string, available func() bool, init func() error) {
	if available() {
		return
	}
	if !isTerminal() {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("cli.key-missing-hint", label, cmd))
		return
	}
	if !confirmKeyInit(i18n.T("cli.key-missing-confirm", label, cmd)) {
		return
	}
	if err := init(); err != nil {
		fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "vaulty-keeper: "+err.Error()))
		return
	}
	fmt.Println(green(i18n.T("cli.key-created", label, apollo.StoreName())))
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
		return nil, fail("%s", i18n.T("cli.load-snapshot-failed", name, err.Error()))
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
	fmt.Fprint(w, i18n.T("help.usage.apollo"))
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
	fmt.Println(green(i18n.T("cli.snapshot-key-created", apollo.StoreName())))
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
		return fail("apollo import: %s", i18n.T("cli.import-need-name"))
	}
	if err := apollo.ValidateAppID(*appID); err != nil {
		return fail("apollo import: %s", i18n.T("cli.import-need-appid", err.Error()))
	}
	src := "-"
	if fs.NArg() > 0 {
		src = fs.Arg(0)
	}
	text, err := readInput(src)
	if err != nil {
		return fail("apollo import: %s", i18n.T("cli.import-read-failed", err.Error()))
	}
	kvs, warnings := apollo.ParseKV(text)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, i18n.T("cli.warning", w))
	}
	if len(kvs) == 0 {
		return fail("apollo import: %s", i18n.T("cli.import-no-entries"))
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
				fmt.Printf("%s", i18n.T("cli.overwrite-prompt", *name, *appID))
				var ans string
				fmt.Scanln(&ans)
				if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
					fmt.Println(i18n.T("cli.cancelled"))
					return 0
				}
			} else {
				return fail("apollo import: %s", i18n.T("cli.import-exists", *name, *appID))
			}
		}
	}
	n, err := app.Import(dirPath, *name, *appID, text, key, sensitiveKey)
	if err != nil {
		return fail("apollo import: %v", err)
	}
	fmt.Println(green(i18n.T("cli.imported", n, *name, *appID, apollo.SnapPath(dirPath, *name, *appID))))
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
		return fail("apollo list: %s", i18n.T("cli.plaintext-tty-only"))
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
		return fail("apollo get: %s", i18n.T("usage.apollo.get"))
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
		return fail("apollo get: %s", i18n.T("cli.key-not-found", k, name))
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
		return fail("apollo set: %s", i18n.T("usage.apollo.set"))
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
	fmt.Println(green(i18n.T("cli.set-done", name, k)))
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
		return fmt.Errorf("%s", i18n.T("cli.plain-mark-refused", key))
	}
	fmt.Printf("%s", i18n.T("cli.plain-mark-note", key))
	var ans string
	fmt.Scanln(&ans)
	if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
		return errors.New(i18n.T("cli.plain-mark-cancelled"))
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
		return fail("apollo unset: %s", i18n.T("usage.apollo.unset"))
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
		return fail("apollo unset: %s", i18n.T("cli.key-not-found", k, name))
	}
	fmt.Println(green(i18n.T("cli.unset-done", name, k)))
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
		return fail("apollo mark: %s", i18n.T("cli.mark-exactly-one"))
	}
	if fs.NArg() != 2 {
		return fail("apollo mark: %s", i18n.T("usage.apollo.mark"))
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
		return fail("apollo mark: %s", i18n.T("cli.key-not-found", k, name))
	}
	label := "safe (--plain)"
	if *asSecret {
		label = "sensitive (--secret)"
	}
	fmt.Println(green(i18n.T("cli.mark-done", name, k, label)))
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
		return fail("apollo compare: %s", i18n.T("cli.plaintext-tty-only"))
	}
	if fs.NArg() != 2 {
		return fail("apollo compare: %s", i18n.T("usage.apollo.compare"))
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
		return fail("apollo export: %s", i18n.T("cli.plaintext-tty-only"))
	}
	if fs.NArg() != 1 {
		return fail("apollo export: %s", i18n.T("usage.apollo.export"))
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
			return fail("apollo export: %s", i18n.T("cli.export-pbcopy-failed", err))
		}
		fmt.Fprintln(os.Stderr, i18n.T("cli.export-copied"))
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
		return fail("apollo rm: %s", i18n.T("usage.apollo.rm"))
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
		fmt.Printf("%s", i18n.T("cli.rm-confirm", name, *appID))
		var ans string
		fmt.Scanln(&ans)
		if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
			fmt.Println(i18n.T("cli.cancelled"))
			return 0
		}
	} else if !*yes {
		return fail("apollo rm: %s", i18n.T("cli.rm-non-tty"))
	}
	ok, err := app.Remove(dirPath, name, *appID)
	if err != nil {
		return fail("apollo rm: %v", err)
	}
	if !ok {
		return fail("apollo rm: %s", i18n.T("cli.rm-not-found", name, *appID))
	}
	fmt.Println(green(i18n.T("cli.removed-snapshot", name, *appID)))
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
		return fail("apollo reveal: %s", i18n.T("cli.plaintext-tty-only"))
	}
	if fs.NArg() < 2 {
		return fail("apollo reveal: %s", i18n.T("usage.apollo.reveal"))
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
		return fail("apollo edit: %s", i18n.T("cli.plaintext-tty-only-edit"))
	}
	if fs.NArg() != 1 {
		return fail("apollo edit: %s", i18n.T("usage.apollo.edit"))
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
		return fail("apollo edit: %s", i18n.T("cli.editor-failed", err.Error()))
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return fail("apollo edit: %v", err)
	}
	_, warnings := apollo.ParseKV(string(content))
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, i18n.T("cli.warning", w))
	}
	n, err := app.EditApply(dirPath, name, *appID, snapKey, sensitiveKey, string(content))
	if err != nil {
		return fail("apollo edit: %v", err)
	}
	fmt.Println(green(i18n.T("cli.updated-snapshot", name, n)))
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
	fmt.Fprint(w, i18n.T("help.usage.sensitive"))
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
	fmt.Fprint(w, i18n.T("help.usage.aes"))
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
		return fail("aes add: %s", i18n.T("cli.aes-add-need-name"))
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
		return fail("aes gen-key: %s", i18n.T("cli.aes-key-len"))
	}
	if *ivBytes != 12 && *ivBytes != 16 {
		return fail("aes gen-key: %s", i18n.T("cli.aes-iv-len"))
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
		return fail("aes decrypt: %s", i18n.T("cli.plaintext-tty-only"))
	}

	k := *key
	i := *iv
	if (k == "" || i == "") && *name != "" {
		e, err := app.AESConfigGet(*name)
		if err != nil {
			return fail("aes: %v", err)
		}
		if e == nil {
			return fail("aes: %s", i18n.T("cli.aes-entry-missing", *name, app.AESConfigPath()))
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
		return fail("aes: %s", i18n.T("cli.aes-key-iv-required"))
	}

	var input string
	if *file != "" {
		b, err := os.ReadFile(*file)
		if err != nil {
			return fail("aes: %s", i18n.T("cli.aes-read-file-failed", err.Error()))
		}
		input = strings.TrimRight(string(b), "\r\n")
	} else if fs.NArg() > 0 {
		input = strings.Join(fs.Args(), " ")
	} else {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fail("aes: %s", i18n.T("cli.aes-read-stdin-failed", err.Error()))
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
		return 0, errors.New(i18n.T("cli.port-range"))
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

// ---- lang ----

// runLang prints the current language, or writes a new one to the shared
// prefs file (~/.vaulty/prefs.json) so the web UI and every later CLI run
// pick it up.
func runLang(args []string) int {
	switch {
	case len(args) == 0:
		fmt.Println(i18n.T("lang.current", i18n.Current()))
		return 0
	case args[0] == "-h" || args[0] == "--help" || args[0] == "help":
		fmt.Fprintln(os.Stdout, i18n.T("lang.usage"))
		return 0
	}
	if len(args) > 1 {
		return fail("lang: %s", i18n.T("lang.usage"))
	}
	if !i18n.IsValid(args[0]) {
		return fail("lang: %s", i18n.T("lang.invalid", args[0]))
	}
	v := i18n.Normalize(args[0])
	if err := i18n.WriteLang(v); err != nil {
		return fail("lang: %v", err)
	}
	i18n.Init()
	fmt.Println(i18n.T("lang.set", v, i18n.PrefsPath()))
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
    'lang:show or set the UI/CLI language (en|zh)'
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
      subs=('init:create database key' 'add:register a connection' 'list:list connections' 'test:check a connection works' 'connect:print ready client command' 'show:print real URL (TTY)' 'regen:rotate the tunnel token' 'on:turn the tunnel on' 'off:turn the tunnel off' 'rm:remove a connection' 'shell:open interactive shell')
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
    COMPREPLY=( $(compgen -W "apollo aes sensitive ui serve remote db completion lang version help" -- "${cur}") )
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
        COMPREPLY=( $(compgen -W "init add list test regen connect show rm shell on off help" -- "${cur}") )
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
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a lang -d 'show or set the UI/CLI language'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a version -d 'show version'
complete -c vaulty-keeper -f -n '__fish_use_subcommand' -a help -d 'show help'
complete -c vaulty-keeper -f -n '__fish_seen_subcommand_from apollo' -a 'init import list get set unset mark compare reveal edit export help'
complete -c vaulty-keeper -f -n '__fish_seen_subcommand_from aes' -a 'encrypt decrypt gen-key list add help'
complete -c vaulty-keeper -f -n '__fish_seen_subcommand_from sensitive' -a 'init help'
complete -c vaulty-keeper -f -n '__fish_seen_subcommand_from remote' -a 'list get compare dblist help'
complete -c vaulty-keeper -f -n '__fish_seen_subcommand_from db' -a 'init add list test regen connect show rm shell on off help'
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
