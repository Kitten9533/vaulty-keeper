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

	"ai-tools/internal/apollo"
	"ai-tools/internal/app"
	"ai-tools/internal/ui"
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
		if isTerminal() {
			return runInteractive()
		}
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
	case "completion":
		return runCompletion(args[1:])
	case "version", "--version", "-v":
		fmt.Println("ai-tools", Version)
		return 0
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "ai-tools: unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `ai-tools %s - personal AI toolbox

Usage:
  ai-tools                          interactive menu (TTY only)
  ai-tools apollo <subcommand>   Apollo snapshot tool (encrypted at rest)
  ai-tools aes <subcommand>      AES/GCM encrypt/decrypt (Java CryptoUtil compatible)
  ai-tools sensitive <subcommand>  sensitive-value key management
  ai-tools ui [--dir <dir>] [--port <port>] [--no-open]
  ai-tools completion <shell>    print zsh/bash/fish completion
  ai-tools version

Run 'ai-tools <command> -h' for subcommand help.
`, Version)
}

func fail(format string, a ...any) int {
	fmt.Fprintln(os.Stderr, paint(os.Stderr, ansiRed, "ai-tools: "+fmt.Sprintf(format, a...)))
	return 1
}

// parseFlags wraps fs.Parse to allow flags after positional args: Go's flag
// package stops at the first non-flag token, so flag tokens are reordered to
// the front while keeping "--flag value" pairs intact. All subcommands use
// ContinueOnError so failures report through fail() instead of hard-exiting;
// -h prints the command's flag usage and returns 0. Returns a non-zero exit
// code for the caller to return.
func parseFlags(fs *flag.FlagSet, args []string) int {
	fs.SetOutput(io.Discard)
	err := fs.Parse(normalizeArgs(fs, args))
	if err == nil {
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		fs.SetOutput(os.Stderr)
		fs.Usage()
		return 0
	}
	return fail("%s: %v", fs.Name(), err)
}

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
	if d := os.Getenv("AI_TOOLS_APOLLO_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ai-tools", "apollo"), nil
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
		return nil, fail("load snapshot %q: %v", name, err)
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
		fmt.Fprintf(os.Stderr, "ai-tools apollo: unknown subcommand %q\n\n", sub)
		apolloUsage(os.Stderr)
		return 2
	}
}

func apolloUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage:
  ai-tools apollo init [--force]                     create snapshot encryption key in Keychain
  ai-tools apollo import <file|-> [--name <env>] [--app-id <id>] [--dir <dir>] [--force]
  ai-tools apollo list [name] [--reveal] [--json] [--yes] [--dir <dir>]
  ai-tools apollo get <name> <key> [--yes] [--dir <dir>]
  ai-tools apollo set <name> <key> <value> [--secret|--plain] [--dir <dir>]
  ai-tools apollo unset <name> <key> [--dir <dir>]
  ai-tools apollo compare <nameA> <nameB> [--reveal] [--json] [--yes] [--dir <dir>]
  ai-tools apollo reveal <name> <key...> [--key <aes>] [--iv <aes>] [--json] [--yes] [--dir <dir>]
  ai-tools apollo edit <name> [--appid <id>] [--editor <bin>] [--yes] [--dir <dir>]
  ai-tools apollo export <name> [--appid <id>] [--copy] [--yes] [--dir <dir>]
  ai-tools apollo rm <name> --appid <id> [--yes] [--dir <dir>]

Every command accepts --appid <id> to address a snapshot stored as
{env}__{appid}.json; without it they read the legacy {env}.json.
rm requires confirmation on a TTY (or --yes when piped).
import refuses to overwrite an existing snapshot without --force
(on a TTY it asks for confirmation instead).
Plaintext-emitting commands (get on a sensitive key, list/compare --reveal,
reveal, export, edit, aes decrypt) are only available in an interactive
terminal; scripts/AI environments are always refused, even with --yes.

Snapshots live in ~/.ai-tools/apollo/ (override: --dir or AI_TOOLS_APOLLO_DIR).
Sensitive keys (password/token/secret/...) are encrypted with the sensitive
key and masked unless --reveal; reveal shows their plaintext by default and
decrypts an external CryptoUtil AES ciphertext when --key/--iv are given.
`)
}

func apolloInit(args []string) int {
	fs := flag.NewFlagSet("apollo init", flag.ContinueOnError)
	force := fs.Bool("force", false, "regenerate key even if one exists")
	if code := parseFlags(fs, args); code != 0 {
		return code
	}

	if err := app.InitKey(*force); err != nil {
		return fail("apollo init: %v", err)
	}
	fmt.Println(green("snapshot key created and stored in macOS Keychain"))
	return 0
}

func apolloImport(args []string) int {
	fs := flag.NewFlagSet("apollo import", flag.ContinueOnError)
	name := fs.String("name", "", "snapshot name (required)")
	appID := fs.String("app-id", "", "Apollo app id (required)")
	dir := fs.String("dir", "", "snapshot directory")
	force := fs.Bool("force", false, "overwrite an existing snapshot")
	if code := parseFlags(fs, args); code != 0 {
		return code
	}

	if *name == "" {
		if fs.NArg() > 0 && fs.Arg(0) != "-" {
			base := filepath.Base(fs.Arg(0))
			*name = strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	if *name == "" {
		return fail("apollo import: --name is required (or pass a file path)")
	}
	if err := apollo.ValidateAppID(*appID); err != nil {
		return fail("apollo import: --app-id is required: %v", err)
	}
	src := "-"
	if fs.NArg() > 0 {
		src = fs.Arg(0)
	}
	text, err := readInput(src)
	if err != nil {
		return fail("apollo import: read input: %v", err)
	}
	kvs, warnings := apollo.ParseKV(text)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if len(kvs) == 0 {
		return fail("apollo import: no key/value entries parsed")
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
				return fail("apollo import: snapshot %q (appid %s) already exists (use --force to overwrite)", *name, *appID)
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
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	_ = *yes
	if *reveal && !isTerminal() {
		return fail("apollo list: plaintext output is only available in an interactive terminal; scripts/AI environments never receive plaintext")
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
			if (s.Items[k].Secret || apollo.IsSensitiveKeyValue(k, decrypted[k])) && !*reveal {
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
		if (s.Items[k].Secret || apollo.IsSensitiveKeyValue(k, decrypted[k])) && !*reveal {
			fmt.Printf("%s = %s\n", k, apollo.MaskWithLen(len(decrypted[k])))
			continue
		}
		fmt.Printf("%s = %s\n", k, decrypted[k])
	}
	return 0
}

func apolloGet(args []string) int {
	fs := flag.NewFlagSet("apollo get", flag.ContinueOnError)
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id (default: legacy {env}.json)")
	yes := fs.Bool("yes", false, "deprecated: plaintext is TTY-only; --yes no longer enables it when piped")
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	_ = *yes
	if fs.NArg() != 2 {
		return fail("apollo get: usage: ai-tools apollo get <name> <key>")
	}
	name, k := fs.Arg(0), fs.Arg(1)
	if apollo.IsSensitive(k) && !isTerminal() {
		return fail("apollo get: key %q is sensitive; plaintext is only available in an interactive terminal, scripts/AI environments never receive it", k)
	}
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo get: %v", err)
	}
	key, sensitiveKey, err := bothKeys()
	if err != nil {
		return fail("apollo get: %v", err)
	}
	v, ok, err := app.GetValue(dirPath, name, *appID, key, sensitiveKey, k)
	if err != nil {
		return fail("apollo get: %v", err)
	}
	if !ok {
		return fail("apollo get: key %q not found in snapshot %q", k, name)
	}
	// credential URIs (e.g. mongodb://user:pw@host) are only caught by value;
	// gate them the same way as name-based sensitive keys.
	if apollo.IsSensitiveKeyValue(k, v) && !isTerminal() {
		return fail("apollo get: key %q carries inline credentials; plaintext is only available in an interactive terminal, scripts/AI environments never receive it", k)
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
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	if fs.NArg() != 3 {
		return fail("apollo set: usage: ai-tools apollo set <name> <key> <value>")
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
	}
	if _, err := app.SetValue(dirPath, name, *appID, k, v, secret, key, sensitiveKey); err != nil {
		return fail("apollo set: %v", err)
	}
	fmt.Println(green(fmt.Sprintf("set %s.%s", name, k)))
	return 0
}

func apolloUnset(args []string) int {
	fs := flag.NewFlagSet("apollo unset", flag.ContinueOnError)
	dir := fs.String("dir", "", "snapshot directory")
	appID := fs.String("appid", "", "app id (default: legacy {env}.json)")
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	if fs.NArg() != 2 {
		return fail("apollo unset: usage: ai-tools apollo unset <name> <key>")
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
		return fail("apollo unset: key %q not found in snapshot %q", k, name)
	}
	fmt.Println(green(fmt.Sprintf("unset %s.%s", name, k)))
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
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	_ = *yes
	if *reveal && !isTerminal() {
		return fail("apollo compare: plaintext output is only available in an interactive terminal; scripts/AI environments never receive plaintext")
	}
	if fs.NArg() != 2 {
		return fail("apollo compare: usage: ai-tools apollo compare <nameA> <nameB>")
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
		if (c.Secret || apollo.IsSensitiveKeyValue(c.Key, v)) && !*reveal {
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
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	_ = *yes
	if !isTerminal() {
		return fail("apollo export: plaintext output is only available in an interactive terminal; scripts/AI environments never receive plaintext")
	}
	if fs.NArg() != 1 {
		return fail("apollo export: usage: ai-tools apollo export <name>")
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
			return fail("apollo export: pbcopy failed: %v", err)
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
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	if fs.NArg() != 1 {
		return fail("apollo rm: usage: ai-tools apollo rm <name> --appid <id>")
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
		return fail("apollo rm: confirmation required on non-TTY (use --yes)")
	}
	ok, err := app.Remove(dirPath, name, *appID)
	if err != nil {
		return fail("apollo rm: %v", err)
	}
	if !ok {
		return fail("apollo rm: snapshot %q (appid %s) not found", name, *appID)
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
	keyFlag := fs.String("key", "", "AES secret key override (env AI_TOOLS_AES_KEY)")
	ivFlag := fs.String("iv", "", "AES iv override (env AI_TOOLS_AES_IV)")
	yes := fs.Bool("yes", false, "deprecated: plaintext is TTY-only; --yes no longer enables it when piped")
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	_ = *yes
	if !isTerminal() {
		return fail("apollo reveal: plaintext output is only available in an interactive terminal; scripts/AI environments never receive plaintext")
	}
	if fs.NArg() < 2 {
		return fail("apollo reveal: usage: ai-tools apollo reveal <name> <key...>")
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
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	_ = *yes
	if !isTerminal() {
		return fail("apollo edit: plaintext is only available in an interactive terminal; scripts/AI environments never receive plaintext")
	}
	if fs.NArg() != 1 {
		return fail("apollo edit: usage: ai-tools apollo edit <name>")
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

	tmp, err := os.CreateTemp("", "ai-tools-edit-*.txt")
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
		return fail("apollo edit: editor failed: %v", err)
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
		fmt.Fprintf(os.Stderr, "ai-tools sensitive: unknown subcommand %q\n\n", sub)
		sensitiveUsage(os.Stderr)
		return 2
	}
}

func sensitiveUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage:
  ai-tools sensitive init [--force]   create the sensitive-value key in Keychain

The sensitive key encrypts sensitive snapshot values, so masked values can
only be revealed with this key (env override: AI_TOOLS_SENSITIVE_KEY).
`)
}

func sensitiveInit(args []string) int {
	fs := flag.NewFlagSet("sensitive init", flag.ContinueOnError)
	force := fs.Bool("force", false, "regenerate even if a key already exists")
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	if err := app.InitSensitiveKey(*force); err != nil {
		return fail("sensitive init: %v", err)
	}
	fmt.Println(green("sensitive key created in Keychain"))
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
		fmt.Fprintf(os.Stderr, "ai-tools aes: unknown subcommand %q\n\n", sub)
		aesUsage(os.Stderr)
		return 2
	}
}

func aesUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage:
  ai-tools aes encrypt [--key <secret>] [--iv <iv>] [--name <entry>] [--file <path>] [<plaintext>]
  ai-tools aes decrypt [--key <secret>] [--iv <iv>] [--name <entry>] [--file <path>] [--yes] [<base64>]
  ai-tools aes gen-key [--bytes 16|24|32] [--iv-bytes 12|16] [--name <entry>]
  ai-tools aes list
  ai-tools aes add --name <entry> --key <secret> --iv <iv>

Key/iv entries live in aes.json (array of {name, secret-key, iv}).
Key/iv resolution: --key/--iv, then --name (looked up in aes.json), then
AI_TOOLS_AES_KEY / AI_TOOLS_AES_IV. Algorithm: AES/GCM/NoPadding, tag 128
bits, key 16/24/32 bytes (UTF-8), iv as UTF-8 bytes (Java CryptoUtil
compatible). gen-key prints fresh printable key/iv for encrypting new values;
with --name it also saves the entry to aes.json. decrypt outputs plaintext and
is only available in an interactive terminal (scripts/AI are always refused).
`)
}

func aesList(args []string) int {
	fs := flag.NewFlagSet("aes list", flag.ContinueOnError)
	if code := parseFlags(fs, args); code != 0 {
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
		fmt.Printf("%s\t%s\t%s\n", e.Name, e.SecretKey, e.IV)
	}
	return 0
}

func aesAdd(args []string) int {
	fs := flag.NewFlagSet("aes add", flag.ContinueOnError)
	name := fs.String("name", "", "entry name (required)")
	key := fs.String("key", "", "secret key (required)")
	iv := fs.String("iv", "", "iv string (required)")
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	if *name == "" {
		return fail("aes add: --name is required")
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
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	if *keyBytes != 16 && *keyBytes != 24 && *keyBytes != 32 {
		return fail("aes gen-key: key must be 16/24/32 bytes")
	}
	if *ivBytes != 12 && *ivBytes != 16 {
		return fail("aes gen-key: iv must be 12 or 16 bytes")
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
	fs := flag.NewFlagSet("aes", flag.ContinueOnError)
	key := fs.String("key", "", "secret key (UTF-8, 16/24/32 bytes); env AI_TOOLS_AES_KEY")
	iv := fs.String("iv", "", "iv string (UTF-8); env AI_TOOLS_AES_IV")
	name := fs.String("name", "", "use key/iv from the named aes.json entry")
	file := fs.String("file", "", "read input from file (supports multi-line)")
	yes := fs.Bool("yes", false, "deprecated: plaintext is TTY-only; --yes no longer enables it when piped")
	if code := parseFlags(fs, args); code != 0 {
		return code
	}
	_ = *yes
	if !encrypt && !isTerminal() {
		return fail("aes decrypt: plaintext output is only available in an interactive terminal; scripts/AI environments never receive plaintext")
	}

	k := *key
	i := *iv
	if (k == "" || i == "") && *name != "" {
		e, err := app.AESConfigGet(*name)
		if err != nil {
			return fail("aes: %v", err)
		}
		if e == nil {
			return fail("aes: entry %q not found in %s", *name, app.AESConfigPath())
		}
		if k == "" {
			k = e.SecretKey
		}
		if i == "" {
			i = e.IV
		}
	}
	if k == "" {
		k = os.Getenv("AI_TOOLS_AES_KEY")
	}
	if i == "" {
		i = os.Getenv("AI_TOOLS_AES_IV")
	}
	if k == "" || i == "" {
		return fail("aes: --key/--iv (or --name, or AI_TOOLS_AES_KEY/AI_TOOLS_AES_IV) are required")
	}

	var input string
	if *file != "" {
		b, err := os.ReadFile(*file)
		if err != nil {
			return fail("aes: read file: %v", err)
		}
		input = strings.TrimRight(string(b), "\r\n")
	} else if fs.NArg() > 0 {
		input = strings.Join(fs.Args(), " ")
	} else {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fail("aes: read stdin: %v", err)
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
		return 0, errors.New("port must be an integer between 0 and 65535")
	}
	return n, nil
}

func runUI(args []string) int {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	dirFlag := fs.String("dir", "", "snapshot directory")
	portFlag := fs.String("port", "8080", "port (default 8080; rolls forward if busy)")
	noOpen := fs.Bool("no-open", false, "do not open a browser")
	if code := parseFlags(fs, args); code != 0 {
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
	if err := ui.Start(context.Background(), ui.Config{Dir: dirPath}, port, !*noOpen, os.Stdout); err != nil {
		return fail("ui: %v", err)
	}
	return 0
}

// ---- completion ----

const completionZsh = `#compdef ai-tools
_ai-tools() {
  local -a commands
  commands=(
    'apollo:Apollo snapshot tool (encrypted at rest)'
    'aes:AES/GCM encrypt/decrypt (Java CryptoUtil compatible)'
    'sensitive:sensitive-value key management'
    'ui:local web UI'
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
  esac
}
compdef _ai-tools ai-tools
`

const completionBash = `_ai-tools() {
  local cur
  cur="${COMP_WORDS[COMP_CWORD]}"
  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "apollo aes sensitive ui completion version help" -- "${cur}") )
    return
  fi
  case "${COMP_WORDS[1]}" in
    apollo)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=( $(compgen -W "init import list get set unset compare reveal edit export help" -- "${cur}") )
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
  esac
}
complete -F _ai-tools ai-tools
`

const completionFish = `complete -c ai-tools -f -n '__fish_use_subcommand' -a apollo -d 'Apollo snapshot tool'
complete -c ai-tools -f -n '__fish_use_subcommand' -a aes -d 'AES/GCM encrypt/decrypt'
complete -c ai-tools -f -n '__fish_use_subcommand' -a sensitive -d 'sensitive-value key management'
complete -c ai-tools -f -n '__fish_use_subcommand' -a ui -d 'local web UI'
complete -c ai-tools -f -n '__fish_use_subcommand' -a completion -d 'print shell completion'
complete -c ai-tools -f -n '__fish_use_subcommand' -a version -d 'show version'
complete -c ai-tools -f -n '__fish_use_subcommand' -a help -d 'show help'
complete -c ai-tools -f -n '__fish_seen_subcommand_from apollo' -a 'init import list get set unset compare reveal edit export help'
complete -c ai-tools -f -n '__fish_seen_subcommand_from aes' -a 'encrypt decrypt gen-key list add help'
complete -c ai-tools -f -n '__fish_seen_subcommand_from sensitive' -a 'init help'
`

func runCompletion(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: ai-tools completion <zsh|bash|fish>")
		return 2
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
