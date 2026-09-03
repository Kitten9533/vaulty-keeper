package cli

import (
	"flag"
	"fmt"
	"io"
)

// cmdHelp is a single entry in the command table. The syntax line is the
// single source of truth for every help surface: the top-level command tree,
// the per-domain usage, and each subcommand's -h output. There is no separate
// "short" help to maintain.
type cmdHelp struct {
	domain string // grouping used by the top-level command tree
	path   string // subcommand path, matched against flag.FlagSet.Name()
	syntax string // full one-line syntax, e.g. "ai-tools apollo get <env> <key>"
	desc   string // one-line Chinese description (shown by subcommand -h)
}

var commandHelp = []cmdHelp{
	// apollo
	{domain: "apollo", path: "apollo init", syntax: "ai-tools apollo init [--force]", desc: "生成快照加密密钥（系统密钥库，如 macOS Keychain）"},
	{domain: "apollo", path: "apollo import", syntax: "ai-tools apollo import <file|-> [--name <env>] --appid <id> [--force] [--dir <dir>]", desc: "解析 Apollo 复制的 KEY=value 文本；- 表示 stdin；--name 省略时取文件名；覆盖需 --force"},
	{domain: "apollo", path: "apollo list", syntax: "ai-tools apollo list [<env>] [--appid <id>] [--json] [--reveal] [--dir <dir>]", desc: "不带 <env> 列出所有快照；带 <env> 列出该快照全部 key（非 TTY 默认掩码）"},
	{domain: "apollo", path: "apollo get", syntax: "ai-tools apollo get <env> <key> [--appid <id>] [--dir <dir>]", desc: "读单个值；非 TTY 下只对标记安全的 key 给明文"},
	{domain: "apollo", path: "apollo set", syntax: "ai-tools apollo set <env> <key> <value> [--plain|--secret] [--appid <id>] [--dir <dir>]", desc: "新增/更新一个值；--plain 标记安全、--secret 标记敏感"},
	{domain: "apollo", path: "apollo unset", syntax: "ai-tools apollo unset <env> <key> [--appid <id>] [--dir <dir>]", desc: "删除一个值"},
	{domain: "apollo", path: "apollo mark", syntax: "ai-tools apollo mark <env> <key> --plain|--secret [--appid <id>] [--dir <dir>]", desc: "不改值，只翻转安全/敏感标记"},
	{domain: "apollo", path: "apollo compare", syntax: "ai-tools apollo compare <envA> <envB> [--appid <a>] [--appid-to <b>] [--json] [--reveal] [--dir <dir>]", desc: "对比两个快照（added/removed/changed）"},
	{domain: "apollo", path: "apollo reveal", syntax: "ai-tools apollo reveal <env> <key...> [--appid <id>] [--json] [--key <aes>] [--iv <aes>] [--dir <dir>]", desc: "显示明文（含敏感值）；也可 --key/--iv 解密外部 AES 密文（仅 TTY）"},
	{domain: "apollo", path: "apollo edit", syntax: "ai-tools apollo edit <env> [--appid <id>] [--editor <bin>] [--dir <dir>]", desc: "$EDITOR 打开明文编辑，保存后自动重新加密（仅 TTY）"},
	{domain: "apollo", path: "apollo export", syntax: "ai-tools apollo export <env> [--appid <id>] [--copy] [--dir <dir>]", desc: "全量明文输出，供粘贴回 Apollo（仅 TTY）"},
	{domain: "apollo", path: "apollo rm", syntax: "ai-tools apollo rm <env> --appid <id> [--yes] [--dir <dir>]", desc: "删除整个快照（非 TTY 需 --yes）"},

	// aes
	{domain: "aes", path: "aes encrypt", syntax: "ai-tools aes encrypt [--key <k>] [--iv <i>] [--name <entry>] [--file <p>] [<text>]", desc: "用 AES/GCM 加密明文，输出 Base64"},
	{domain: "aes", path: "aes decrypt", syntax: "ai-tools aes decrypt [--key <k>] [--iv <i>] [--name <entry>] [--file <p>] [<base64>]", desc: "解密 Base64 密文为明文（仅 TTY）"},
	{domain: "aes", path: "aes gen-key", syntax: "ai-tools aes gen-key [--bytes 16|24|32] [--iv-bytes 12|16] [--name <entry>]", desc: "生成随机 key/iv；--name 同时存入 aes.json"},
	{domain: "aes", path: "aes list", syntax: "ai-tools aes list", desc: "列出 aes.json 里的命名条目（非 TTY 只显示长度）"},
	{domain: "aes", path: "aes add", syntax: "ai-tools aes add --name <entry> --key <k> --iv <i>", desc: "手动把 key/iv 存入 aes.json"},

	// sensitive
	{domain: "sensitive", path: "sensitive init", syntax: "ai-tools sensitive init [--force]", desc: "生成敏感值专用密钥（系统密钥库）"},

	// ui / serve
	{domain: "ui", path: "ui", syntax: "ai-tools ui [--dir <dir>] [--port <port>] [--no-open] [--allow-plaintext]", desc: "启动本地 Web UI（推荐手动操作；仅监听 127.0.0.1）"},
	{domain: "serve", path: "serve", syntax: "ai-tools serve [--addr <host:port>] [--dir <dir>]", desc: "启动掩码代理 + DB 隧道（在持有密钥的主机运行）"},

	// remote
	{domain: "remote", path: "remote list", syntax: "ai-tools remote list [<env>] [--appid <id>] [--json]", desc: "经掩码代理列出快照/key（永远只有掩码）"},
	{domain: "remote", path: "remote get", syntax: "ai-tools remote get <env> <key> [--appid <id>]", desc: "经掩码代理读单个值（掩码 + 指纹）"},
	{domain: "remote", path: "remote compare", syntax: "ai-tools remote compare <envA> <envB> [--appid <a>] [--appid-to <b>] [--json]", desc: "经掩码代理对比两个快照（掩码）"},
	{domain: "remote", path: "remote dblist", syntax: "ai-tools remote dblist [--json]", desc: "经掩码代理列出数据库隧道（name/type/port）"},

	// db
	{domain: "db", path: "db init", syntax: "ai-tools db init [--force]", desc: "生成数据库加密密钥（系统密钥库）"},
	{domain: "db", path: "db add", syntax: "ai-tools db add <name> [--port <port>] [--test] [--dir <dir>]", desc: "注册连接（URL 从 stdin 读，加密落盘；--test 先验证）"},
	{domain: "db", path: "db list", syntax: "ai-tools db list [--json] [--dir <dir>]", desc: "列出连接（name/type/port，不含 URL）"},
	{domain: "db", path: "db test", syntax: "ai-tools db test <name> [--dir <dir>]", desc: "验证注册的连接可用（AI 安全，不打印 URL）"},
	{domain: "db", path: "db connect", syntax: "ai-tools db connect <name> [--container] [--cmd] [--host <host>] [--dir <dir>]", desc: "打印带 token 的现成客户端命令"},
	{domain: "db", path: "db show", syntax: "ai-tools db show <name> [--dir <dir>]", desc: "打印解密后的真实 URL（仅 TTY）"},
	{domain: "db", path: "db rm", syntax: "ai-tools db rm <name> [--yes] [--dir <dir>]", desc: "删除一个连接（非 TTY 需 --yes）"},
	{domain: "db", path: "db shell", syntax: "ai-tools db shell <name> [--dir <dir>]", desc: "打开交互式原生客户端（仅 TTY）"},

	// global
	{domain: "global", path: "completion", syntax: "ai-tools completion <zsh|bash|fish>", desc: "打印 shell 补全脚本"},
	{domain: "global", path: "version", syntax: "ai-tools version", desc: "显示版本"},
	{domain: "global", path: "help", syntax: "ai-tools help", desc: "显示本帮助"},
}

// domainTitles controls the order and headings of the top-level command tree.
var domainTitles = []struct {
	domain string
	title  string
}{
	{"apollo", "Apollo 快照（加密落盘）"},
	{"aes", "AES 加解密（Java CryptoUtil 兼容）"},
	{"sensitive", "敏感值密钥管理"},
	{"ui", "本地 Web UI"},
	{"serve", "掩码代理（主机侧）"},
	{"remote", "隔离域读取（经掩码代理）"},
	{"db", "数据库隧道（db）"},
	{"global", "其他"},
}

// printCommandTree renders the full command tree (used by bare ai-tools,
// `ai-tools help` and `ai-tools --help`).
func printCommandTree(w io.Writer) {
	for _, d := range domainTitles {
		fmt.Fprintf(w, "%s:\n", d.title)
		for _, c := range commandHelp {
			if c.domain == d.domain {
				fmt.Fprintf(w, "  %s\n", c.syntax)
			}
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Run 'ai-tools <command> -h' for subcommand details.")
}

// printDomainUsage renders every command in a domain (used by `ai-tools <cmd> -h`
// for the top-level domains, e.g. `ai-tools apollo -h`).
func printDomainUsage(w io.Writer, domain string) {
	for _, c := range commandHelp {
		if c.domain == domain {
			fmt.Fprintf(w, "  %s\n", c.syntax)
		}
	}
}

// printCommandHelp renders one subcommand's full help: its syntax line, a
// short description, and the auto-generated flag list (used by `ai-tools
// <cmd> <sub> -h`).
func printCommandHelp(w io.Writer, fs *flag.FlagSet) {
	for _, c := range commandHelp {
		if c.path == fs.Name() {
			fmt.Fprintf(w, "%s\n", c.syntax)
			if c.desc != "" {
				fmt.Fprintf(w, "  %s\n", c.desc)
			}
			break
		}
	}
	fmt.Fprintln(w)
	fs.SetOutput(w)
	fs.PrintDefaults()
}
