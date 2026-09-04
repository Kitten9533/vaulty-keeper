// Package i18n provides shared en/zh localization for the CLI and the web
// UI's terminal output. The effective language is resolved once per process
// (Init) from VAULTY_KEEPER_LANG (highest priority), then
// ~/.vaulty/prefs.json (written by both the UI language switcher and
// `vaulty-keeper lang`), and defaults to English.
package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvLang = "VAULTY_KEEPER_LANG"
	LangEn  = "en"
	LangZh  = "zh"
)

// Lang is the resolved language for this process ("en" or "zh").
var Lang = LangEn

// dict maps a key to [en, zh] strings. Placeholders use fmt verbs (%s, %d).
var dict = map[string][2]string{
	// ---- lang command ----
	"lang.usage":   {"usage: vaulty-keeper lang [en|zh]", "用法：vaulty-keeper lang [en|zh]"},
	"lang.current": {"language: %s", "语言：%s"},
	"lang.invalid": {"invalid language %q (use en or zh)", "无效的语言 %q（使用 en 或 zh）"},
	"lang.set":     {"language set to %s (%s)", "语言已设为 %s（%s）"},

	// ---- cli runtime ----
	"cli.mkdir-failed":            {"failed to create the data directory: %s", "创建数据目录失败：%s"},
	"cli.aes-list-failed":         {"failed to read the AES key/iv list: %s", "读取 AES key/iv 列表失败：%s"},
	"cli.aes-gen-failed":          {"failed to generate AES key/iv: %s", "生成 AES key/iv 失败：%s"},
	"cli.aes-init-failed":         {"failed to initialize the AES key/iv list: %s", "初始化 AES key/iv 列表失败：%s"},
	"cli.aes-initialized":         {"Initialized the AES key/iv list (~/.vaulty/aes.json, generated a \"default\" entry; view with aes list, add more with aes add / aes gen-key --name)", "已初始化 AES key/iv 列表（~/.vaulty/aes.json，已生成 default 条目；用 aes list 查看，aes add / aes gen-key --name 添加更多）"},
	"cli.key-snapshot":            {"snapshot", "快照"},
	"cli.key-sensitive":           {"sensitive-value", "敏感值"},
	"cli.key-db":                  {"database", "数据库"},
	"cli.key-missing-hint":        {"hint: the %s key is not initialized; run %s", "提示：%s密钥未初始化，请运行 %s"},
	"cli.key-missing-confirm":     {"The %s key is not initialized; run %s now?", "%s密钥未初始化，现在运行 %s 吗？"},
	"cli.key-created":             {"%s key created (%s)", "%s密钥已创建（%s）"},
	"cli.load-snapshot-failed":    {"failed to load snapshot %q: %s", "加载快照 %q 失败：%s"},
	"cli.cancelled":               {"cancelled", "已取消"},
	"cli.plaintext-tty-only":      {"plaintext output is only available in an interactive terminal; script/AI environments never get plaintext", "明文输出仅在交互式终端可用；脚本/AI 环境永远拿不到明文"},
	"cli.plaintext-tty-only-edit": {"plaintext is only available in an interactive terminal; script/AI environments never get it", "明文仅在交互式终端可用；脚本/AI 环境永远拿不到明文"},
	"cli.key-not-found":           {"key %q not found in snapshot %q", "快照 %q 中不存在 key %q"},
	"cli.plain-mark-refused":      {"refusing to mark %q as safe for scripts/AI: the key name or value looks sensitive; this must be confirmed in an interactive terminal", "拒绝将 %q 标记为对脚本/AI 安全：key 名或值看起来是敏感内容；此操作必须在交互式终端确认"},
	"cli.plain-mark-note":         {"note: %q looks like a sensitive value (name or content matches the sensitive rules). Confirm marking it safe so AI/scripts can read plaintext? [y/N] ", "注意：%q 看起来是敏感值（名字或内容命中敏感规则）。确认要标记为安全、允许 AI/脚本读明文？[y/N] "},
	"cli.plain-mark-cancelled":    {"cancelled: not marked as safe", "已取消：未标记为安全"},
	"cli.mark-exactly-one":        {"mark requires exactly one of --plain or --secret", "mark 必须且只能指定 --plain 或 --secret 之一"},
	"cli.port-range":              {"port must be an integer between 0 and 65535", "端口必须是 0 到 65535 之间的整数"},
	"cli.editor-failed":           {"editor failed: %s", "编辑器失败：%s"},
	"cli.overwrite-prompt":        {"snapshot %q (appid %s) already exists; overwrite? [y/N] ", "快照 %q (appid %s) 已存在，覆盖? [y/N] "},
	"cli.import-need-name":        {"--name is required (or pass a file path)", "--name 必填（或传入文件路径）"},
	"cli.import-need-appid":       {"--appid is required: %s", "--appid 必填：%s"},
	"cli.import-read-failed":      {"failed to read input: %s", "读取输入失败：%s"},
	"cli.import-no-entries":       {"no key/value entries parsed", "未解析到任何键值条目"},
	"cli.import-exists":           {"snapshot %q (appid %s) already exists (use --force to overwrite)", "快照 %q (appid %s) 已存在（用 --force 覆盖）"},
	"cli.rm-non-tty":              {"confirmation required on non-TTY (use --yes)", "非 TTY 下需要确认（用 --yes）"},
	"cli.rm-confirm":              {"delete snapshot %q (appid %s)? [y/N] ", "删除快照 %q (appid %s)? [y/N] "},
	"cli.rm-not-found":            {"snapshot %q (appid %s) not found", "未找到快照 %q (appid %s)"},
	"cli.aes-add-need-name":       {"--name is required", "--name 必填"},
	"cli.aes-key-len":             {"key length must be 16/24/32 bytes", "key 长度必须为 16/24/32 字节"},
	"cli.aes-iv-len":              {"iv length must be 12 or 16 bytes", "iv 长度必须为 12 或 16 字节"},
	"cli.aes-entry-missing":       {"entry %q not found in %s", "%s 中不存在条目 %q"},
	"cli.aes-key-iv-required":     {"--key/--iv (or --name, or VAULTY_KEEPER_AES_KEY/VAULTY_KEEPER_AES_IV) are required", "--key/--iv（或 --name，或 VAULTY_KEEPER_AES_KEY/VAULTY_KEEPER_AES_IV）必填"},
	"cli.aes-read-file-failed":    {"failed to read file: %s", "读取文件失败：%s"},
	"cli.aes-read-stdin-failed":   {"failed to read standard input: %s", "读取标准输入失败：%s"},
	"cli.export-copied":           {"copied to clipboard", "已复制到剪贴板"},
	"cli.export-pbcopy-failed":    {"pbcopy failed: %v", "pbcopy 失败：%v"},
	"cli.snapshot-key-created":    {"snapshot key created and stored in %s", "快照密钥已创建并保存到 %s"},
	"cli.warning":                 {"warning: %s", "警告：%s"},
	"cli.imported":                {"imported %d entries into snapshot %q (appid %s) (%s)", "已导入 %d 条键值到快照 %q（appid %s）（%s）"},
	"cli.set-done":                {"set %s.%s", "已设置 %s.%s"},
	"cli.unset-done":              {"unset %s.%s", "已删除 %s.%s"},
	"cli.mark-done":               {"mark %s.%s as %s", "已将 %s.%s 标记为 %s"},
	"cli.removed-snapshot":        {"removed snapshot %q (appid %s)", "已删除快照 %q（appid %s）"},
	"cli.updated-snapshot":        {"updated snapshot %q: %d entries", "已更新快照 %q：%d 条键值"},

	// ---- usage lines (fail "用法：") ----
	"usage.apollo.get":     {"usage: vaulty-keeper apollo get <name> <key>", "用法：vaulty-keeper apollo get <name> <key>"},
	"usage.apollo.set":     {"usage: vaulty-keeper apollo set <name> <key> <value>", "用法：vaulty-keeper apollo set <name> <key> <value>"},
	"usage.apollo.unset":   {"usage: vaulty-keeper apollo unset <name> <key>", "用法：vaulty-keeper apollo unset <name> <key>"},
	"usage.apollo.mark":    {"usage: vaulty-keeper apollo mark <name> <key> --plain|--secret", "用法：vaulty-keeper apollo mark <name> <key> --plain|--secret"},
	"usage.apollo.compare": {"usage: vaulty-keeper apollo compare <nameA> <nameB>", "用法：vaulty-keeper apollo compare <nameA> <nameB>"},
	"usage.apollo.export":  {"usage: vaulty-keeper apollo export <name>", "用法：vaulty-keeper apollo export <name>"},
	"usage.apollo.rm":      {"usage: vaulty-keeper apollo rm <name> --appid <id>", "用法：vaulty-keeper apollo rm <name> --appid <id>"},
	"usage.apollo.reveal":  {"usage: vaulty-keeper apollo reveal <name> <key...>", "用法：vaulty-keeper apollo reveal <name> <key...>"},
	"usage.apollo.edit":    {"usage: vaulty-keeper apollo edit <name>", "用法：vaulty-keeper apollo edit <name>"},

	// ---- help ----
	"help.footer":      {"Run 'vaulty-keeper <command> -h' for subcommand details.", "运行 'vaulty-keeper <command> -h' 查看子命令详情。"},
	"cli.tagline":      {"vaulty-keeper %s - personal AI toolbox", "vaulty-keeper %s - 个人 AI 工具箱"},
	"cli.usage-line":   {"Usage: vaulty-keeper <command> [args] [flags]", "用法：vaulty-keeper <command> [args] [flags]"},
	"help.t.apollo":    {"Apollo snapshots (encrypted at rest)", "Apollo 快照（加密落盘）"},
	"help.t.aes":       {"AES encrypt/decrypt (Java CryptoUtil compatible)", "AES 加解密（Java CryptoUtil 兼容）"},
	"help.t.sensitive": {"Sensitive-value key management", "敏感值密钥管理"},
	"help.t.ui":        {"Local web UI", "本地 Web UI"},
	"help.t.serve":     {"Masked bridge (host side)", "掩码代理（主机侧）"},
	"help.t.remote":    {"Isolated-domain reads (via the masked bridge)", "隔离域读取（经掩码代理）"},
	"help.t.db":        {"Database tunnels (db)", "数据库隧道（db）"},
	"help.t.global":    {"Other", "其他"},

	// ---- help command descriptions ----
	"help.d.apollo.init":    {"create the snapshot encryption key (system keychain, e.g. macOS Keychain)", "生成快照加密密钥（系统密钥库，如 macOS Keychain）"},
	"help.d.apollo.import":  {"parse Apollo-pasted KEY=value text; - reads stdin; --name defaults to the file name; overwriting needs --force", "解析 Apollo 复制的 KEY=value 文本；- 表示 stdin；--name 省略时取文件名；覆盖需 --force"},
	"help.d.apollo.list":    {"without <env> list all snapshots; with <env> list every key of that snapshot (masked by default on non-TTY)", "不带 <env> 列出所有快照；带 <env> 列出该快照全部 key（非 TTY 默认掩码）"},
	"help.d.apollo.get":     {"read a single value; non-TTY shows plaintext only for keys explicitly marked safe", "读单个值；非 TTY 下只对标记安全的 key 给明文"},
	"help.d.apollo.set":     {"add/update a value; --plain marks safe, --secret marks sensitive", "新增/更新一个值；--plain 标记安全、--secret 标记敏感"},
	"help.d.apollo.unset":   {"delete a value", "删除一个值"},
	"help.d.apollo.mark":    {"keep the value, flip the safe/sensitive marker only", "不改值，只翻转安全/敏感标记"},
	"help.d.apollo.compare": {"diff two snapshots (added/removed/changed)", "对比两个快照（added/removed/changed）"},
	"help.d.apollo.reveal":  {"show plaintext (sensitive values included); --key/--iv can decrypt external AES ciphertext (TTY only)", "显示明文（含敏感值）；也可 --key/--iv 解密外部 AES 密文（仅 TTY）"},
	"help.d.apollo.edit":    {"edit plaintext in $EDITOR; re-encrypts on save (TTY only)", "$EDITOR 打开明文编辑，保存后自动重新加密（仅 TTY）"},
	"help.d.apollo.export":  {"print every value in plaintext for pasting back into Apollo (TTY only)", "全量明文输出，供粘贴回 Apollo（仅 TTY）"},
	"help.d.apollo.rm":      {"delete a whole snapshot (needs --yes on non-TTY)", "删除整个快照（非 TTY 需 --yes）"},
	"help.d.aes.encrypt":    {"encrypt plaintext with AES/GCM, output Base64", "用 AES/GCM 加密明文，输出 Base64"},
	"help.d.aes.decrypt":    {"decrypt Base64 ciphertext to plaintext (TTY only)", "解密 Base64 密文为明文（仅 TTY）"},
	"help.d.aes.gen-key":    {"generate a random key/iv; --name also saves it to aes.json", "生成随机 key/iv；--name 同时存入 aes.json"},
	"help.d.aes.list":       {"list named entries in aes.json (lengths only on non-TTY)", "列出 aes.json 里的命名条目（非 TTY 只显示长度）"},
	"help.d.aes.add":        {"manually store a key/iv pair in aes.json", "手动把 key/iv 存入 aes.json"},
	"help.d.sensitive.init": {"create the sensitive-value key (system keychain)", "生成敏感值专用密钥（系统密钥库）"},
	"help.d.ui":             {"start the local web UI (recommended for manual work; listens on 127.0.0.1 only)", "启动本地 Web UI（推荐手动操作；仅监听 127.0.0.1）"},
	"help.d.serve":          {"run the masked-only bridge + DB tunnels on the host holding the keys; prints and stores a per-run token in ~/.vaulty/bridge-token (default addr 127.0.0.1:8970), consumed by 'vaulty-keeper remote' and as the tunnel credential", "启动掩码代理 + DB 隧道（在持有密钥的主机运行）；打印并把本次运行 token 存到 ~/.vaulty/bridge-token（默认 127.0.0.1:8970），供 'vaulty-keeper remote' 与隧道凭据使用"},
	"help.d.remote.list":    {"list snapshots/keys through the masked bridge (always masked)", "经掩码代理列出快照/key（永远只有掩码）"},
	"help.d.remote.get":     {"read one value through the bridge (masked + fingerprint)", "经掩码代理读单个值（掩码 + 指纹）"},
	"help.d.remote.compare": {"diff two snapshots through the bridge (masked)", "经掩码代理对比两个快照（掩码）"},
	"help.d.remote.dblist":  {"list database tunnels through the bridge (name/type/port)", "经掩码代理列出数据库隧道（name/type/port）"},
	"help.d.db.init":        {"create the database encryption key (system keychain)", "生成数据库加密密钥（系统密钥库）"},
	"help.d.db.add":         {"register a connection (URL read from stdin, encrypted at rest; --test verifies first)", "注册连接（URL 从 stdin 读，加密落盘；--test 先验证）"},
	"help.d.db.list":        {"list connections (name/type/port, no URLs)", "列出连接（name/type/port，不含 URL）"},
	"help.d.db.test":        {"verify a registered connection authenticates (AI-safe, prints no URL)", "验证注册的连接可用（AI 安全，不打印 URL）"},
	"help.d.db.regen":       {"regenerate the tunnel token (one connection or all); old tokens stop working immediately", "重新生成隧道 token（单个连接或全部），旧 token 立即失效"},
	"help.d.db.on":          {"turn a connection's tunnel on (on by default; serve picks it up within ~2s)", "开启连接的隧道（默认开启；serve 约 2 秒内生效）"},
	"help.d.db.off":         {"turn a connection's tunnel off; the port stops listening (serve picks it up within ~2s)", "关闭连接的隧道，端口停止监听（serve 约 2 秒内生效）"},
	"help.d.db.connect":     {"print ready-to-run client commands with the tunnel token", "打印带 token 的现成客户端命令"},
	"help.d.db.show":        {"print the decrypted real URL (TTY only)", "打印解密后的真实 URL（仅 TTY）"},
	"help.d.db.rm":          {"remove a connection (needs --yes on non-TTY)", "删除一个连接（非 TTY 需 --yes）"},
	"help.d.db.shell":       {"open an interactive native client (TTY only)", "打开交互式原生客户端（仅 TTY）"},
	"help.d.completion":     {"print a shell completion script", "打印 shell 补全脚本"},
	"help.d.version":        {"show the version", "显示版本"},
	"help.d.help":           {"show this help", "显示本帮助"},

	// ---- help usage paragraphs ----
	"help.usage.apollo":    {"<env> is the environment name; together with --appid <id> it addresses\n{env}__{appid}.json; without --appid it reads/writes the legacy {env}.json.\nSnapshots live in ~/.vaulty/apollo/ by default (override with --dir or\nVAULTY_KEEPER_APOLLO_DIR).\n\nPlaintext commands (list/compare --reveal, reveal, export, edit) are only\navailable in an interactive terminal; script/AI environments are always\nrefused, even with --yes. On non-TTY, get/list/compare mask everything by\ndefault (reversed default); only keys explicitly marked safe with\nset --plain / mark --plain are shown in plaintext.\n\nSensitive values (key name or content matching password/token/secret/JWT/\ncredential-bearing URI) are encrypted with the sensitive-value key\n(sensitive init) and masked by default; reveal shows plaintext and can also\ndecrypt external CryptoUtil AES ciphertext with --key/--iv. rm needs\nconfirmation (TTY prompt; --yes when piped); import needs --force to\noverwrite an existing snapshot.\n", "<env> 是环境名，与 --appid <id> 一起寻址 {env}__{appid}.json；\n不带 --appid 时读写旧版 {env}.json。快照默认存 ~/.vaulty/apollo/\n（--dir 或 VAULTY_KEEPER_APOLLO_DIR 覆盖）。\n\n明文命令（list/compare --reveal、reveal、export、edit）只在交互式终端可用；\n脚本/AI 环境一律拒绝，加 --yes 也无法放行。非 TTY 下 get/list/compare 默认\n全部掩码（反转默认），只有 set --plain / mark --plain 显式标记安全的 key\n才给明文。\n\n敏感值（名字或内容命中 password/token/secret/JWT/带凭据 URI）用敏感值密钥\n加密（sensitive init），默认掩码；reveal 显示明文，也可 --key/--iv 解密外部\nCryptoUtil AES 密文。rm 需要确认（TTY 提示，非 TTY 需 --yes）；import 覆盖\n已有快照需 --force。\n"},
	"help.usage.sensitive": {"The sensitive-value key encrypts the sensitive values in snapshots (independent\nof the snapshot key); masked values can only be recovered with it\n(env override: VAULTY_KEEPER_SENSITIVE_KEY).\n", "敏感值密钥加密快照中的敏感值（独立于快照密钥），掩码值只能靠它解开\n（env 覆盖：VAULTY_KEEPER_SENSITIVE_KEY）。\n"},
	"help.usage.aes":       {"key/iv entries live in ~/.vaulty/aes.json ({name, secret-key, iv} array).\nResolution order: --key/--iv → --name (lookup in aes.json) →\nVAULTY_KEEPER_AES_KEY / VAULTY_KEEPER_AES_IV.\nAlgorithm: AES/GCM/NoPadding, 128-bit tag, key 16/24/32 bytes (UTF-8), iv as\nUTF-8 bytes (Java CryptoUtil compatible). decrypt prints plaintext and is only\navailable in an interactive terminal (script/AI environments are refused).\n", "key/iv 条目存于 ~/.vaulty/aes.json（{name, secret-key, iv} 数组）。\n解析顺序：--key/--iv → --name（查 aes.json）→ VAULTY_KEEPER_AES_KEY / VAULTY_KEEPER_AES_IV。\n算法：AES/GCM/NoPadding，tag 128 bits，key 16/24/32 字节（UTF-8），iv 为\nUTF-8 字节（Java CryptoUtil 兼容）。decrypt 输出明文，仅交互式终端可用\n（脚本/AI 环境一律拒绝）。\n"},
	"help.usage.db":        {"Database URLs are encrypted at rest (dedicated DB key) and never shown to\nscripts/AI. 'vaulty-keeper serve' opens one TCP tunnel per connection; inside\nan isolated container use native clients (psql/mysql/redis-cli) against the\ntunnel port, with the token as the username/AUTH password — the token is not\nthe real database password. Tunnels are on by default; 'db on'/'db off' toggle\nthem per connection (serve picks the change up within ~2s), 'db test' verifies\na registered URL authenticates (without printing it); 'db show' prints only on\nyour own terminal.\n", "数据库 URL 加密落盘（独立 DB 密钥），永不向脚本/AI 显示。'vaulty-keeper serve'\n为每个连接起一条 TCP 隧道；隔离容器内用原生客户端（psql/mysql/redis-cli）\n连隧道端口，token 作用户名/AUTH 密码——token 不是真实数据库密码。\n隧道默认开启；'db on'/'db off' 按连接开关（serve 约 2 秒内生效），\n'db test' 验证注册的 URL 可认证（不打印它）；'db show' 只在你自己的终端打印。\n"},
	"help.usage.remote":    {"The masked bridge ('vaulty-keeper serve', run on the host holding the keys)\nonly returns masked values, never plaintext, even for keys marked safe. The\nremote client is for isolated domains (Docker containers / separate accounts /\nVMs): agents there read through the bridge via %s / %s. remote dblist lists\nthe database tunnels currently listening, so native clients can connect with\nthe token as the username (PG/MySQL) or the AUTH password (Redis).\n", "掩码代理（'vaulty-keeper serve'，在持有密钥的主机运行）只回掩码值，永不回明文，\n即使 key 被标记安全也一样。remote 客户端供隔离域（Docker 容器/独立账号/VM）\n内的 AI 使用，经 %s / %s 访问桥。remote dblist 列出正在监听的数据库隧道，\n供原生客户端用 token 作为用户名（PG/MySQL）或 AUTH 密码（Redis）连接。\n"},

	// ---- db runtime ----
	"db.init-force-warning":     {"db init --force replaces the database key in the keychain; existing connections become undecryptable and must be re-registered with db add. Continue?", "db init --force 会用新密钥覆盖 Keychain 中的数据库密钥，已有的数据库连接将无法再解密，需要重新 db add。确认？"},
	"db.init-force-non-tty":     {"db init: %s (use --yes to confirm when not on a TTY)", "db init: %s（非 TTY 下请加 --yes 确认）"},
	"db.init-cancelled":         {"db init: cancelled", "db init: 已取消"},
	"db.key-created":            {"database key created", "数据库密钥已创建"},
	"db.add-usage":              {"usage: echo '<url>' | vaulty-keeper db add <name> [--port <port>] [--test]", "用法：echo '<url>' | vaulty-keeper db add <name> [--port <port>] [--test]"},
	"db.add-stdin-required":     {"provide the database URL via stdin (printf 'postgres://u:p@host:5432/db' | vaulty-keeper db add %s)", "请通过 stdin 提供数据库 URL（printf 'postgres://u:p@host:5432/db' | vaulty-keeper db add %s）"},
	"db.add-test-failed":        {"connection test failed: %s (not saved; fix the URL and retry, or drop --test to force-save)", "连接测试失败：%s（不会保存；请修正 URL 后重试，或去掉 --test 强制保存）"},
	"db.added":                  {"connection %q (%s) saved encrypted", "连接 %q（%s）已加密保存"},
	"db.list-no-bridge":         {" (and no masked bridge configured via VAULTY_KEEPER_BRIDGE_ADDR/TOKEN)", "（且未配置掩码代理 VAULTY_KEEPER_BRIDGE_ADDR/TOKEN）"},
	"db.list-hint-base":         {"local read failed: %s", "本地读取失败：%s"},
	"db.list-hint-missing":      {"%s does not exist; register one with 'vaulty-keeper db add <name>', or point --dir / VAULTY_KEEPER_DB_DIR at an existing store", "%s 不存在，请先 'vaulty-keeper db add <name>' 注册，或 --dir / VAULTY_KEEPER_DB_DIR 指向已有环境"},
	"db.list-hint-nokey":        {"%s: run 'vaulty-keeper db init' first (or set VAULTY_KEEPER_DB_KEY)", "%s：请先运行 'vaulty-keeper db init'（或设置 VAULTY_KEEPER_DB_KEY）"},
	"db.list-hint-mismatch":     {"%s exists but cannot be decrypted (key mismatch): it was created with a different VAULTY_KEEPER_DB_KEY / Keychain key; check the environment variable or point --dir / VAULTY_KEEPER_DB_DIR at the correct store", "%s 存在但解密失败（密钥不匹配）：该文件是用不同的 VAULTY_KEEPER_DB_KEY / Keychain 密钥创建的；请检查环境变量，或换用 --dir / VAULTY_KEEPER_DB_DIR 指向正确的 store"},
	"db.off-mark":               {"off", "关"},
	"db.rm-usage":               {"usage: vaulty-keeper db rm <name> [--yes]", "用法：vaulty-keeper db rm <name> [--yes]"},
	"db.removed":                {"connection %q removed", "连接 %q 已删除"},
	"db.test-usage":             {"usage: vaulty-keeper db test <name>", "用法：vaulty-keeper db test <name>"},
	"db.test-fix":               {"fix: printf '<correct URL>' | vaulty-keeper db add %s (the port stays the same)", "修复：printf '<正确URL>' | vaulty-keeper db add %s（端口保持不变）"},
	"db.regen-no-name-with-all": {"regen: no connection name allowed with --all", "regen: --all 时不接受连接名"},
	"db.regen-usage":            {"usage: vaulty-keeper db regen <name> or vaulty-keeper db regen --all", "用法：vaulty-keeper db regen <name> 或 vaulty-keeper db regen --all"},
	"db.regen-none":             {"no connections to regenerate", "没有可重新生成 token 的连接"},
	"db.regen-done":             {"regenerated tokens for %d connections: %s", "已为 %d 个连接重新生成 token：%s"},
	"db.regen-token":            {"new tunnel token for %q: %s", "连接 %q 的新隧道 token：%s"},
	"db.regen-link-local":       {"new link (local): %s", "新链接（本机）：%s"},
	"db.regen-link-container":   {"new link (container): %s", "新链接（容器）：%s"},
	"db.tunnel-no-name":         {"no connection name allowed with --all", "--all 时不接受连接名"},
	"db.tunnel-usage":           {"usage: vaulty-keeper db %s <name> [--dir <dir>] or vaulty-keeper db %s --all", "用法：vaulty-keeper db %s <name> [--dir <dir>] 或 vaulty-keeper db %s --all"},
	"db.tunnel-none":            {"no connections whose state needs changing", "没有需要变更状态的连接"},
	"db.tunnel-done-on":         {"tunnel turned on for %d connections: %s", "已开启 %d 个连接的隧道：%s"},
	"db.tunnel-done-off":        {"tunnel turned off for %d connections: %s", "已关闭 %d 个连接的隧道：%s"},
	"db.tunnel-off-done":        {"tunnel of %q turned off ('vaulty-keeper db on %s' re-enables it)", "连接 %q 的隧道已关闭（'vaulty-keeper db on %s' 重新开启）"},
	"db.tunnel-on-done":         {"tunnel of %q turned on", "连接 %q 的隧道已开启"},
	"db.show-usage":             {"usage: vaulty-keeper db show <name>", "用法：vaulty-keeper db show <name>"},
	"db.show-tty-only":          {"db show: only available in an interactive terminal (the real connection info is only shown on your terminal)", "db show: 仅可在交互式终端使用（真实连接信息只在你的终端里显示）"},
	"db.show-warn":              {"⚠ the real connection info is only visible on your terminal; do not send this output to AI/scripts or store it in logs", "⚠ 真实连接信息仅在你的终端可见；不要把这段输出发给 AI/脚本或存入日志"},
	"db.show-name":              {"name: %s", "名称：%s"},
	"db.show-type":              {"type: %s", "类型：%s"},
	"db.show-port":              {"port: %d", "端口：%d"},
	"db.show-url":               {"url:  %s", "URL：%s"},
	"db.ok":                     {"OK: %s (%s)", "OK：%s（%s）"},
	"db.ok-user":                {" user=%s", " user=%s"},
	"db.ok-db":                  {" db=%s", " db=%s"},
	"db.connect-usage":          {"usage: vaulty-keeper db connect <name> [--container] [--cmd]", "用法：vaulty-keeper db connect <name> [--container] [--cmd]"},
	"db.connect-warn-off":       {"⚠ the tunnel of %q is off ('vaulty-keeper db on %s' turns it on); links below are currently unavailable", "⚠ 连接 %q 的隧道已关闭（'vaulty-keeper db on %s' 开启）；下面的链接当前不可用"},
	"db.connect-head":           {"# %s (%s) — the token is the bridge token, not the real database password (tunnel port %d)", "# %s (%s) — token 是 bridge token，不是真实数据库密码（隧道端口 %d）"},
	"db.connect-raw":            {"raw tunnel link (AI / other tools can convert from it):", "原始隧道链接（AI / 其他工具可据此自行转换）:"},
	"db.connect-psql":           {"psql — PostgreSQL's built-in command-line client:", "psql —— PostgreSQL 自带的命令行客户端（装 PG 就有）："},
	"db.connect-dbeaver":        {"DBeaver / DataGrip — paste this JDBC URL:", "DBeaver / DataGrip —— 通用数据库图形工具（贴 JDBC 链接）："},
	"db.connect-pgadmin":        {"pgAdmin4 — PostgreSQL's official GUI (fill the fields, no URL pasting):", "pgAdmin4 —— PostgreSQL 官方图形工具（填字段，不支持贴 URL）："},
	"db.connect-workbench":      {"MySQL Workbench — MySQL's official GUI (fill the fields, no URL pasting):", "MySQL Workbench —— MySQL 官方图形工具（填字段，不支持贴 URL）："},
	"db.connect-mysql":          {"mysql — MySQL's built-in command-line client:", "mysql —— MySQL 自带的命令行客户端（装 MySQL 就有）："},
	"db.connect-insight":        {"Redis Insight — Redis's official GUI (paste the URL):", "Redis Insight —— Redis 官方图形工具（贴 URL）："},
	"db.connect-rediscli":       {"redis-cli — Redis's built-in command-line client:", "redis-cli —— Redis 自带的命令行客户端（装 Redis 就有）："},
	"db.password-empty":         {"Leave the password empty", "密码留空"},
	"db.password-any":           {"Password=any", "密码任意"},
	"db.connect-unsupported":    {"unsupported database type %q", "不支持的数据库类型 %q"},
	"db.bridge-token-missing":   {"bridge token not found (run 'vaulty-keeper serve' first, or export %s)", "未找到 bridge token（请先运行 'vaulty-keeper serve'，或导出 %s）"},
	"db.shell-usage":            {"usage: vaulty-keeper db shell <name>", "用法：vaulty-keeper db shell <name>"},
	"db.shell-tty-only":         {"db shell: only available in an interactive terminal (the connection info is only visible on your terminal)", "db shell: 仅可在交互式终端使用（连接信息只在你的终端里可见）"},
	"db.shell-bin-missing":      {"not found: %s (install the client first)", "未找到 %s（请先安装该客户端）"},

	// ---- remote runtime ----
	"remote.db-key-skip":   {"serve: database key unavailable (%s); skipping DB tunnels (the masked bridge is unaffected)", "serve: 数据库密钥不可用（%s）；跳过 DB 隧道（掩码桥不受影响）"},
	"remote.token-missing": {"bridge token not set (export %s or run 'vaulty-keeper serve' first)", "未设置 bridge token（请导出 %s 或先运行 'vaulty-keeper serve'）"},
	"remote.list-usage":    {"usage: vaulty-keeper remote list [name] [--appid <id>]", "用法：vaulty-keeper remote list [name] [--appid <id>]"},
	"remote.get-usage":     {"usage: vaulty-keeper remote get <name> <key> [--appid <id>]", "用法：vaulty-keeper remote get <name> <key> [--appid <id>]"},
	"remote.compare-usage": {"usage: vaulty-keeper remote compare <nameA> <nameB>", "用法：vaulty-keeper remote compare <nameA> <nameB>"},

	// ---- ui runtime ----
	"ui.plaintext-hint": {"hint: plaintext endpoints (export/decrypt/plaintext edit/AES key list) are currently disabled; restart the UI with --allow-plaintext to enable", "提示：明文接口（导出/解密/明文编辑/AES 密钥列表）当前已禁用；需要时用 --allow-plaintext 重启 UI 开启"},
	"ui.port-range":     {"port %d is out of the 0..65535 range", "端口 %d 超出 0..65535 范围"},
	"ui.no-free-port":   {"no free port starting from %d", "从 %d 起没有空闲端口"},
}

// Init resolves the effective language into the package-level Lang. It is
// cheap and idempotent; call it at the start of each command run so tests
// with t.Setenv/HOME get a fresh resolution.
func Init() {
	Lang = Current()
}

// Current resolves the effective language without mutating package state:
// VAULTY_KEEPER_LANG env wins, then the prefs file, then English.
func Current() string {
	if v := os.Getenv(EnvLang); v != "" {
		return Normalize(v)
	}
	if p := PrefsPath(); p != "" {
		if l, err := readLangFile(p); err == nil && l != "" {
			return l
		}
	}
	return LangEn
}

// normalizeLang canonicalizes a raw language value: "zh" for the zh variants,
// "en" for the en variants, or "" when unrecognized.
func normalizeLang(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case LangEn, "english":
		return LangEn
	case LangZh, "zh-cn", "zh_cn", "chinese":
		return LangZh
	}
	return ""
}

// Normalize maps a raw language value to "en" or "zh"; anything unrecognized
// maps to "en".
func Normalize(v string) string {
	if s := normalizeLang(v); s != "" {
		return s
	}
	return LangEn
}

func isValid(lang string) bool { return normalizeLang(lang) != "" }

// IsValid reports whether a raw language value is acceptable.
func IsValid(lang string) bool { return isValid(lang) }

// PrefsPath returns the shared preference file (~/.vaulty/prefs.json).
func PrefsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".vaulty", "prefs.json")
}

// ReadLang returns the language stored in the prefs file ("" when absent).
func ReadLang() string {
	p := PrefsPath()
	if p == "" {
		return ""
	}
	l, err := readLangFile(p)
	if err != nil {
		return ""
	}
	return l
}

// WriteLang stores a language in the prefs file (0600, dir 0700). Invalid
// values are rejected.
func WriteLang(lang string) error {
	if !isValid(lang) {
		return fmt.Errorf("invalid language %q (use en or zh)", lang)
	}
	v := Normalize(lang)
	p := PrefsPath()
	if p == "" {
		return fmt.Errorf("cannot resolve home directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(map[string]string{"lang": v})
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

type prefs struct {
	Lang string `json:"lang"`
}

func readLangFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var p prefs
	if err := json.Unmarshal(b, &p); err != nil {
		return "", err
	}
	return Normalize(p.Lang), nil
}

// T resolves a key in the current language with fmt-style substitutions.
// Unknown keys fall back to the key itself.
func T(key string, args ...any) string {
	e, ok := dict[key]
	if !ok {
		return key
	}
	s := e[0]
	if Lang == LangZh {
		s = e[1]
	}
	if len(args) > 0 && strings.Contains(s, "%") {
		s = fmt.Sprintf(s, args...)
	}
	return s
}
