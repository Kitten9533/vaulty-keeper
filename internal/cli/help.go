package cli

import (
	"flag"
	"fmt"
	"io"

	"vaulty-keeper/internal/i18n"
)

// cmdHelp is a single entry in the command table. The syntax line is the
// single source of truth for every help surface: the top-level command tree,
// the per-domain usage, and each subcommand's -h output. There is no separate
// "short" help to maintain. The description is an i18n key (help.d.*) so the
// help output follows the CLI language (VAULTY_KEEPER_LANG / prefs file).
type cmdHelp struct {
	domain  string // grouping used by the top-level command tree
	path    string // subcommand path, matched against flag.FlagSet.Name()
	syntax  string // full one-line syntax, e.g. "vaulty-keeper apollo get <env> <key>"
	descKey string // i18n key for the one-line description (shown by subcommand -h)
}

var commandHelp = []cmdHelp{
	// apollo
	{domain: "apollo", path: "apollo init", syntax: "vaulty-keeper apollo init [--force]", descKey: "help.d.apollo.init"},
	{domain: "apollo", path: "apollo import", syntax: "vaulty-keeper apollo import <file|-> [--name <env>] --appid <id> [--force] [--dir <dir>]", descKey: "help.d.apollo.import"},
	{domain: "apollo", path: "apollo list", syntax: "vaulty-keeper apollo list [<env>] [--appid <id>] [--json] [--reveal] [--dir <dir>]", descKey: "help.d.apollo.list"},
	{domain: "apollo", path: "apollo get", syntax: "vaulty-keeper apollo get <env> <key> [--appid <id>] [--dir <dir>]", descKey: "help.d.apollo.get"},
	{domain: "apollo", path: "apollo set", syntax: "vaulty-keeper apollo set <env> <key> <value> [--plain|--secret] [--appid <id>] [--dir <dir>]", descKey: "help.d.apollo.set"},
	{domain: "apollo", path: "apollo unset", syntax: "vaulty-keeper apollo unset <env> <key> [--appid <id>] [--dir <dir>]", descKey: "help.d.apollo.unset"},
	{domain: "apollo", path: "apollo mark", syntax: "vaulty-keeper apollo mark <env> <key> --plain|--secret [--appid <id>] [--dir <dir>]", descKey: "help.d.apollo.mark"},
	{domain: "apollo", path: "apollo compare", syntax: "vaulty-keeper apollo compare <envA> <envB> [--appid <a>] [--appid-to <b>] [--json] [--reveal] [--dir <dir>]", descKey: "help.d.apollo.compare"},
	{domain: "apollo", path: "apollo reveal", syntax: "vaulty-keeper apollo reveal <env> <key...> [--appid <id>] [--json] [--key <aes>] [--iv <aes>] [--dir <dir>]", descKey: "help.d.apollo.reveal"},
	{domain: "apollo", path: "apollo edit", syntax: "vaulty-keeper apollo edit <env> [--appid <id>] [--editor <bin>] [--dir <dir>]", descKey: "help.d.apollo.edit"},
	{domain: "apollo", path: "apollo export", syntax: "vaulty-keeper apollo export <env> [--appid <id>] [--copy] [--dir <dir>]", descKey: "help.d.apollo.export"},
	{domain: "apollo", path: "apollo rm", syntax: "vaulty-keeper apollo rm <env> --appid <id> [--yes] [--dir <dir>]", descKey: "help.d.apollo.rm"},

	// aes
	{domain: "aes", path: "aes encrypt", syntax: "vaulty-keeper aes encrypt [--key <k>] [--iv <i>] [--name <entry>] [--file <p>] [<text>]", descKey: "help.d.aes.encrypt"},
	{domain: "aes", path: "aes decrypt", syntax: "vaulty-keeper aes decrypt [--key <k>] [--iv <i>] [--name <entry>] [--file <p>] [<base64>]", descKey: "help.d.aes.decrypt"},
	{domain: "aes", path: "aes gen-key", syntax: "vaulty-keeper aes gen-key [--bytes 16|24|32] [--iv-bytes 12|16] [--name <entry>]", descKey: "help.d.aes.gen-key"},
	{domain: "aes", path: "aes list", syntax: "vaulty-keeper aes list", descKey: "help.d.aes.list"},
	{domain: "aes", path: "aes add", syntax: "vaulty-keeper aes add --name <entry> --key <k> --iv <i>", descKey: "help.d.aes.add"},

	// sensitive
	{domain: "sensitive", path: "sensitive init", syntax: "vaulty-keeper sensitive init [--force]", descKey: "help.d.sensitive.init"},

	// ui / serve
	{domain: "ui", path: "ui", syntax: "vaulty-keeper ui [--dir <dir>] [--port <port>] [--no-open] [--allow-plaintext]", descKey: "help.d.ui"},
	{domain: "serve", path: "serve", syntax: "vaulty-keeper serve [--addr <host:port>] [--dir <dir>]", descKey: "help.d.serve"},

	// remote
	{domain: "remote", path: "remote list", syntax: "vaulty-keeper remote list [<env>] [--appid <id>] [--json]", descKey: "help.d.remote.list"},
	{domain: "remote", path: "remote get", syntax: "vaulty-keeper remote get <env> <key> [--appid <id>]", descKey: "help.d.remote.get"},
	{domain: "remote", path: "remote compare", syntax: "vaulty-keeper remote compare <envA> <envB> [--appid <a>] [--appid-to <b>] [--json]", descKey: "help.d.remote.compare"},
	{domain: "remote", path: "remote dblist", syntax: "vaulty-keeper remote dblist [--json]", descKey: "help.d.remote.dblist"},

	// db
	{domain: "db", path: "db init", syntax: "vaulty-keeper db init [--force]", descKey: "help.d.db.init"},
	{domain: "db", path: "db add", syntax: "vaulty-keeper db add <name> [--port <port>] [--test] [--dir <dir>]", descKey: "help.d.db.add"},
	{domain: "db", path: "db list", syntax: "vaulty-keeper db list [--json] [--dir <dir>]", descKey: "help.d.db.list"},
	{domain: "db", path: "db test", syntax: "vaulty-keeper db test <name> [--dir <dir>]", descKey: "help.d.db.test"},
	{domain: "db", path: "db regen", syntax: "vaulty-keeper db regen <name> | --all [--dir <dir>]", descKey: "help.d.db.regen"},
	{domain: "db", path: "db on", syntax: "vaulty-keeper db on <name> | --all [--dir <dir>]", descKey: "help.d.db.on"},
	{domain: "db", path: "db off", syntax: "vaulty-keeper db off <name> | --all [--dir <dir>]", descKey: "help.d.db.off"},
	{domain: "db", path: "db connect", syntax: "vaulty-keeper db connect <name> [--container] [--cmd] [--host <host>] [--dir <dir>]", descKey: "help.d.db.connect"},
	{domain: "db", path: "db show", syntax: "vaulty-keeper db show <name> [--dir <dir>]", descKey: "help.d.db.show"},
	{domain: "db", path: "db rm", syntax: "vaulty-keeper db rm <name> [--yes] [--dir <dir>]", descKey: "help.d.db.rm"},
	{domain: "db", path: "db shell", syntax: "vaulty-keeper db shell <name> [--dir <dir>]", descKey: "help.d.db.shell"},

	// global
	{domain: "global", path: "completion", syntax: "vaulty-keeper completion <zsh|bash|fish>", descKey: "help.d.completion"},
	{domain: "global", path: "version", syntax: "vaulty-keeper version", descKey: "help.d.version"},
	{domain: "global", path: "help", syntax: "vaulty-keeper help", descKey: "help.d.help"},
	{domain: "global", path: "lang", syntax: "vaulty-keeper lang [en|zh]", descKey: "lang.usage"},
}

// domainTitles controls the order and headings of the top-level command tree.
var domainTitles = []struct {
	domain   string
	titleKey string
}{
	{"apollo", "help.t.apollo"},
	{"aes", "help.t.aes"},
	{"sensitive", "help.t.sensitive"},
	{"ui", "help.t.ui"},
	{"serve", "help.t.serve"},
	{"remote", "help.t.remote"},
	{"db", "help.t.db"},
	{"global", "help.t.global"},
}

// printCommandTree renders the full command tree (used by bare vaulty-keeper,
// `vaulty-keeper help` and `vaulty-keeper --help`).
func printCommandTree(w io.Writer) {
	for _, d := range domainTitles {
		fmt.Fprintf(w, "%s:\n", i18n.T(d.titleKey))
		for _, c := range commandHelp {
			if c.domain == d.domain {
				fmt.Fprintf(w, "  %s\n", c.syntax)
			}
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, i18n.T("help.footer"))
}

// printDomainUsage renders every command in a domain (used by `vaulty-keeper <cmd> -h`
// for the top-level domains, e.g. `vaulty-keeper apollo -h`).
func printDomainUsage(w io.Writer, domain string) {
	for _, c := range commandHelp {
		if c.domain == domain {
			fmt.Fprintf(w, "  %s\n", c.syntax)
		}
	}
}

// printCommandHelp renders one subcommand's full help: its syntax line, a
// short description, and the auto-generated flag list (used by `vaulty-keeper
// <cmd> <sub> -h`).
func printCommandHelp(w io.Writer, fs *flag.FlagSet) {
	for _, c := range commandHelp {
		if c.path == fs.Name() {
			fmt.Fprintf(w, "%s\n", c.syntax)
			if c.descKey != "" {
				fmt.Fprintf(w, "  %s\n", i18n.T(c.descKey))
			}
			break
		}
	}
	fmt.Fprintln(w)
	fs.SetOutput(w)
	fs.PrintDefaults()
}
