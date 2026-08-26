package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ai-tools/internal/apollo"
	"ai-tools/internal/app"
)

// Interactive menu mode: run bare `ai-tools` on a TTY, choose an operation
// (arrow keys / j/k / digits / enter), answer prompts, and (for import / aes)
// paste multi-line input. ESC / Ctrl-C / q exit the whole session at any step.

var errExit = errors.New("exit requested")

// isTerminalFunc is overridable in tests to simulate a non-TTY stdin.
var isTerminalFunc = func() bool {
	return isTTY(os.Stdin.Fd())
}

func isTerminal() bool { return isTerminalFunc() }

func isCancel(s string) bool {
	return strings.EqualFold(s, "cancel") || strings.EqualFold(s, "quit")
}

type interactive struct {
	in     *bufio.Reader
	dir    string
	exit   bool
	raw    bool
	aesKey string
	aesIV  string
}

func (it *interactive) loadAesConfig() {
	c, err := app.AESConfigLoad()
	if err != nil {
		return
	}
	it.aesKey, it.aesIV = c.Key, c.IV
}

func runInteractive() int {
	dir, err := snapDir("")
	if err != nil {
		return fail("interactive: %v", err)
	}
	it := &interactive{in: bufio.NewReader(os.Stdin), dir: dir}
	it.loadAesConfig()
	if isTerminal() {
		if restore, err := rawMode(); err == nil {
			it.raw = true
			defer restore()
		}
	}
	fmt.Println(bold(cyan(fmt.Sprintf("ai-tools %s — 交互模式", Version))))
	if it.aesKey != "" {
		fmt.Println(dim("已加载自定义 key/iv，AES 加密/解密/一键解密默认使用"))
	}
	fmt.Println(dim("操作: ↑/↓ 或 k(上)/j(下) 移动 · 回车/数字选择 · h 帮助 · ESC/q/Ctrl-C 退出 · cancel 取消"))
	for {
		if it.exit {
			fmt.Println(green("bye"))
			return 0
		}
		opts := []string{
			"导入 Apollo 配置 (import)",
			"查看快照 (list)",
			"读取单个值 (get)",
			"设置值 (set)",
			"删除值 (unset)",
			"对比两个快照 (compare)",
			"一键解密 (reveal)",
			"编辑快照 (edit)",
			"导出快照 (export)",
			"AES 加密 (aes encrypt)",
			"AES 解密 (aes decrypt)",
			"生成 key/iv (aes gen-key)",
			"自定义 key/iv (设置 AES key/iv)",
			"帮助 (help)",
		}
		fmt.Println()
		switch choice := it.choose(opts, "退出"); choice {
		case 0:
			fmt.Println(green("bye"))
			return 0
		case len(opts):
			it.cmdHelp()
		case 1:
			it.cmdImport()
		case 2:
			it.cmdList()
		case 3:
			it.cmdGet()
		case 4:
			it.cmdSet()
		case 5:
			it.cmdUnset()
		case 6:
			it.cmdCompare()
		case 7:
			it.cmdReveal()
		case 8:
			it.cmdEdit()
		case 9:
			it.cmdExport()
		case 10:
			it.cmdAESEncrypt()
		case 11:
			it.cmdAESDecrypt()
		case 12:
			it.cmdGenKey()
		case 13:
			it.cmdCustomKeyIv()
		}
	}
}

func (it *interactive) cmdHelp() {
	apolloUsage(os.Stdout)
	aesUsage(os.Stdout)
}

// readLineBuf reads a line from the buffered reader (non-terminal path).
func (it *interactive) readLineBuf() (string, error) {
	line, err := it.in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readLineWithPrompt reads one line of input. On a TTY (raw session) it uses
// a raw-mode line editor (UTF-8 aware, backspace, left/right arrows, Enter
// to submit, ESC/Ctrl-C to exit the whole session, Ctrl-D on empty line =
// EOF). Otherwise it falls back to buffered line reading.
func (it *interactive) readLineWithPrompt(prompt string) (string, error) {
	if !it.raw {
		return it.readLineBuf()
	}

	var line []rune
	pos := 0
	redraw := func() {
		fmt.Printf("\r\x1b[K%s%s", prompt, string(line))
		if n := len(line) - pos; n > 0 {
			fmt.Printf("\x1b[%dD", displayWidth(string(line[pos:])))
		}
	}
	redraw()

	var b [1]byte
	for {
		n, err := os.Stdin.Read(b[:])
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		c := rune(b[0])
		switch {
		case c == '\x1b':
			if it.handleEscape(&line, &pos, redraw) {
				continue
			}
			it.exit = true
			return "", errExit
		case c == '\x03':
			it.exit = true
			return "", errExit
		case c == '\x04':
			if len(line) == 0 {
				return "", io.EOF
			}
		case c == '\r' || c == '\n':
			fmt.Println()
			return string(line), nil
		case c == 0x7f || c == 0x08: // backspace / delete
			if pos > 0 {
				line = append(line[:pos-1], line[pos:]...)
				pos--
				redraw()
			}
		case c == '\t':
			// ignore tabs
		case c >= 0x20:
			if c >= 0x80 {
				c = readRune(c)
			}
			line = append(line, 0)
			copy(line[pos+1:], line[pos:])
			line[pos] = c
			pos++
			redraw()
		}
	}
}

// readArrowByte consumes an ESC [ <dir> sequence byte by byte (never reading
// past the arrow bytes) and returns the direction byte ('A' up, 'B' down,
// 'C' right, 'D' left), or 0 when it was a bare ESC or not an arrow.
func readArrowByte() byte {
	if !waitReadable(os.Stdin.Fd(), 50*time.Millisecond) {
		return 0
	}
	var b1 [1]byte
	if n, _ := os.Stdin.Read(b1[:]); n <= 0 || b1[0] != '[' {
		return 0
	}
	if !waitReadable(os.Stdin.Fd(), 50*time.Millisecond) {
		return 0
	}
	var b2 [1]byte
	if n, _ := os.Stdin.Read(b2[:]); n <= 0 {
		return 0
	}
	return b2[0]
}

// handleEscape reads the bytes after ESC; returns true when an arrow key was
// consumed (cursor moved), false when it was a bare ESC (caller exits).
func (it *interactive) handleEscape(line *[]rune, pos *int, redraw func()) bool {
	switch readArrowByte() {
	case 'C': // right
		if *pos < len(*line) {
			*pos++
			redraw()
		}
		return true
	case 'D': // left
		if *pos > 0 {
			*pos--
			redraw()
		}
		return true
	case 'A', 'B': // up/down ignored in text input
		return true
	}
	return false
}

// readRune completes a multi-byte UTF-8 rune whose first byte is first.
func readRune(first rune) rune {
	var cont int
	switch {
	case first&0xE0 == 0xC0:
		cont = 1
	case first&0xF0 == 0xE0:
		cont = 2
	case first&0xF8 == 0xF0:
		cont = 3
	default:
		return first
	}
	b := []byte{byte(first)}
	var c [1]byte
	for i := 0; i < cont; i++ {
		if _, err := os.Stdin.Read(c[:]); err != nil {
			break
		}
		b = append(b, c[0])
	}
	r, _ := utf8.DecodeRune(b)
	return r
}

// displayWidth approximates the terminal display width of s (East Asian
// wide/fullwidth characters count double).
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r < 0x20:
		case r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) ||
			(r >= 0xAC00 && r <= 0xD7A3) || (r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFE30 && r <= 0xFE4F) || (r >= 0xFF00 && r <= 0xFF60) ||
			(r >= 0xFFE0 && r <= 0xFFE6) || (r >= 0x1F300 && r <= 0x1FAFF)):
			w += 2
		default:
			w++
		}
	}
	return w
}

// choose prints a numbered menu; returns 1..len(opts), 0 for "0 <footer>",
// or -1 on cancel. On a real TTY it renders a chalk-style menu navigated
// with arrow keys / j/k / digits / enter; otherwise it falls back to
// line-based number input (scripts, tests, dumb terminals).
func (it *interactive) choose(opts []string, footer string) int {
	if !it.raw {
		return it.chooseLine(opts, footer)
	}

	sel := 0
	printed := 0
	for {
		if printed > 0 {
			fmt.Printf("\x1b[%dA", printed)
		}
		printed = it.renderMenu(opts, footer, sel)
		k, v := readKey()
		switch k {
		case keyUp:
			sel = (sel + len(opts) - 1) % len(opts)
		case keyDown:
			sel = (sel + 1) % len(opts)
		case keyEnter:
			return sel + 1
		case keyDigit:
			if v == 0 {
				it.exit = true
				return 0
			}
			if v >= 1 && v <= len(opts) {
				if v == 1 && len(opts) >= 10 {
					if k2, v2 := readDigitAfterDelay(); k2 == keyDigit {
						n := 10 + v2
						if n >= 1 && n <= len(opts) {
							drainTrailingEnter()
							return n
						}
					} else if k2 == keyEnter {
						return 1
					} else if k2 == keyEscape || k2 == keyQuit || k2 == keyCtrlC || k2 == keyCtrlD {
						it.exit = true
						return 0
					}
				}
				drainTrailingEnter()
				return v
			}
		case keyEscape, keyQuit, keyCtrlC, keyCtrlD:
			it.exit = true
			return 0
		case keyHelp:
			return len(opts)
		}
	}
}

func (it *interactive) chooseLine(opts []string, footer string) int {
	for {
		for i, o := range opts {
			fmt.Printf("  %2d. %s\n", i+1, o)
		}
		fmt.Printf("   0. %s\n", footer)
		fmt.Print("> ")
		line, err := it.readLineBuf()
		if err != nil {
			return 0
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isCancel(line) {
			return -1
		}
		n, err := strconv.Atoi(line)
		if err != nil {
			fmt.Println("请输入数字")
			continue
		}
		if n == 0 {
			return 0
		}
		if n >= 1 && n <= len(opts) {
			return n
		}
		fmt.Println("超出范围")
	}
}

// renderMenu prints the menu with the selected item highlighted and returns
// the number of lines printed (for cursor repositioning).
func (it *interactive) renderMenu(opts []string, footer string, sel int) int {
	lines := 0
	for i, o := range opts {
		if i == sel {
			fmt.Printf("\r\x1b[2K▸ %s\n", paint(os.Stdout, ansiBold+ansiCyan, fmt.Sprintf("%2d. %s", i+1, o)))
		} else {
			fmt.Printf("\r\x1b[2K  %s %s\n", gray(fmt.Sprintf("%2d.", i+1)), o)
		}
		lines++
	}
	fmt.Printf("\r\x1b[2K  %s\n", dim("0. "+footer))
	return lines + 1
}

type menuKey int

const (
	keyUp menuKey = iota
	keyDown
	keyEnter
	keyCtrlC
	keyCtrlD
	keyEscape
	keyQuit
	keyHelp
	keyDigit
	keyOther
)

// readDigitAfterDelay waits briefly for a second digit (for two-digit menu
// options). It returns (key, value): (keyDigit, d) for a digit, an exit key
// for ESC/Ctrl-C/Ctrl-D, (keyOther, c) for anything else, or (0, 0) when the
// window lapses without input.
func readDigitAfterDelay() (menuKey, int) {
	if !waitReadable(os.Stdin.Fd(), 300*time.Millisecond) {
		return 0, 0
	}
	b := make([]byte, 1)
	n, _ := os.Stdin.Read(b)
	if n <= 0 {
		return 0, 0
	}
	switch b[0] {
	case '\x03':
		return keyCtrlC, 0
	case '\x04':
		return keyCtrlD, 0
	case '\x1b':
		return keyEscape, 0
	case '\r', '\n':
		return keyEnter, 0
	}
	if b[0] >= '0' && b[0] <= '9' {
		return keyDigit, int(b[0] - '0')
	}
	return keyOther, int(b[0])
}

// drainTrailingEnter consumes an Enter pressed right after a digit selection
// (e.g. "13<Enter>"), so it doesn't leak into the next prompt as empty input.
func drainTrailingEnter() {
	if !waitReadable(os.Stdin.Fd(), 60*time.Millisecond) {
		return
	}
	var b [1]byte
	if n, _ := os.Stdin.Read(b[:]); n > 0 && (b[0] == '\r' || b[0] == '\n') {
		return
	}
}

// readKey reads one key from stdin in raw mode: arrows (ESC [ A/B), digits,
// enter, q/h/j/k, ESC, Ctrl-C/Ctrl-D.
func readKey() (menuKey, int) {
	buf := make([]byte, 4)
	n, _ := os.Stdin.Read(buf[:1])
	if n <= 0 {
		return keyCtrlD, 0
	}
	switch buf[0] {
	case '\x03':
		return keyCtrlC, 0
	case '\x04':
		return keyCtrlD, 0
	case '\r', '\n':
		return keyEnter, 0
	case '\x1b':
		switch readArrowByte() {
		case 'A':
			return keyUp, 0
		case 'B':
			return keyDown, 0
		}
		return keyEscape, 0
	case 'q', 'Q':
		return keyQuit, 0
	case 'h', 'H':
		return keyHelp, 0
	case 'j', 'J':
		return keyDown, 0
	case 'k', 'K':
		return keyUp, 0
	default:
		if buf[0] >= '0' && buf[0] <= '9' {
			return keyDigit, int(buf[0] - '0')
		}
		return keyOther, int(buf[0])
	}
}

// prompt asks a question; def is the value accepted on empty input
// (empty string means "optional / accept empty"). Returns (value, cancelled).
func (it *interactive) prompt(label, def string) (string, bool) {
	for {
		var ps string
		if def != "" {
			ps = fmt.Sprintf("%s %s: ", magenta(label), cyan("["+def+"]"))
		} else {
			ps = fmt.Sprintf("%s %s: ", magenta(label), dim("(回车跳过)"))
		}
		fmt.Print(ps)
		line, err := it.readLineWithPrompt(ps)
		if err != nil {
			return "", true
		}
		t := strings.TrimSpace(line)
		if isCancel(t) {
			return "", true
		}
		if t == "" {
			return def, false
		}
		return t, false
	}
}

// promptValue asks a question whose answer keeps spaces (e.g. a value).
func (it *interactive) promptValue(label string) (string, bool) {
	ps := fmt.Sprintf("%s: ", magenta(label))
	fmt.Print(ps)
	line, err := it.readLineWithPrompt(ps)
	if err != nil {
		return "", true
	}
	if isCancel(strings.TrimSpace(line)) {
		return "", true
	}
	return line, false
}

// promptMultiline collects lines until Ctrl-D (EOF) or a line "end"/".".
func (it *interactive) promptMultiline(label string) (string, bool) {
	fmt.Println(cyan(label))
	fmt.Println(dim("（多行输入：粘贴内容，完成后单独一行输入 end 或按 Ctrl-D；ESC 退出）"))
	var lines []string
	for {
		ps := "| "
		fmt.Print(ps)
		line, err := it.readLineWithPrompt(ps)
		if err != nil {
			break
		}
		t := strings.TrimSpace(line)
		if strings.EqualFold(t, "end") || t == "." {
			break
		}
		if isCancel(t) {
			return "", true
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), false
}

// filterOptions returns options containing the query (case-insensitive
// substring match); an empty query returns everything.
func filterOptions(options []string, query string) []string {
	q := strings.ToLower(query)
	if q == "" {
		return options
	}
	var out []string
	for _, o := range options {
		if strings.Contains(strings.ToLower(o), q) {
			out = append(out, o)
		}
	}
	return out
}

// splitSnapshotRef splits a picker value ("env" or "env (appid)") into its
// environment name and app id.
func splitSnapshotRef(s string) (string, string) {
	if i := strings.Index(s, " ("); i > 0 && strings.HasSuffix(s, ")") {
		return s[:i], s[i+2 : len(s)-1]
	}
	return s, ""
}

// snapshotKeys returns the sorted key names of a snapshot (key names are
// plaintext in the JSON; values stay encrypted).
func snapshotKeys(dir, sel string) ([]string, error) {
	name, appID := splitSnapshotRef(sel)
	path, err := snapPath(dir, name, appID)
	if err != nil {
		return nil, err
	}
	s, err := apollo.Load(path)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(s.Items))
	for k := range s.Items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// pickOrInput shows a searchable picker: a query box on top (type to filter),
// then "✏️ 自己输入" as the first entry, then all options. ↑/↓ or j/k to
// move, digits 0..n to jump, Enter to confirm (own input uses the query text,
// empty query + own input cancels the step), ESC/q/Ctrl-C/Ctrl-D exit the
// whole session. Falls back to a plain prompt when not on a TTY.
func (it *interactive) pickOrInput(label string, options []string) (string, bool) {
	if !it.raw {
		return it.prompt(label, "")
	}
	var query []rune
	sel := 0 // 0 = 自己输入, 1..len(filtered) = options
	lines := 0
	for {
		filtered := filterOptions(options, string(query))
		if sel > len(filtered) {
			sel = len(filtered)
		}
		if lines > 0 {
			fmt.Printf("\x1b[%dA", lines)
		}
		lines = it.renderPick(label, string(query), filtered, sel)

		var b [1]byte
		n, err := os.Stdin.Read(b[:])
		if err != nil {
			return "", true
		}
		if n == 0 {
			continue
		}
		c := rune(b[0])
		switch {
		case c == '\x1b':
			switch readArrowByte() {
			case 'A':
				if sel > 0 {
					sel--
				}
				continue
			case 'B':
				if sel < len(filtered) {
					sel++
				}
				continue
			}
			it.exit = true
			return "", true
		case c == '\x03' || c == '\x04':
			it.exit = true
			return "", true
		case c == '\r' || c == '\n':
			fmt.Println()
			if sel == 0 {
				if len(query) == 0 {
					return "", true
				}
				return string(query), false
			}
			return filtered[sel-1], false
		case c == 0x7f || c == 0x08: // backspace
			if len(query) > 0 {
				query = query[:len(query)-1]
				sel = 0
			}
		case c == 'j' || c == 'J':
			if sel < len(filtered) {
				sel++
			}
		case c == 'k' || c == 'K':
			if sel > 0 {
				sel--
			}
		case c >= '0' && c <= '9':
			v := int(c - '0')
			if v == 0 {
				sel = 0
			} else if v >= 1 && v <= len(filtered) {
				sel = v
			}
		case c >= 0x20:
			if c >= 0x80 {
				c = readRune(c)
			}
			query = append(query, c)
			sel = 0
		}
	}
}

// renderPick draws the picker; returns the number of lines printed.
func (it *interactive) renderPick(label, query string, filtered []string, sel int) int {
	lines := 0
	fmt.Printf("\r\x1b[2K%s\n", cyan(label))
	lines++
	fmt.Printf("\r\x1b[2K%s %s\n", magenta(">"), query)
	lines++
	own := "✏️ 自己输入"
	if query != "" {
		own = fmt.Sprintf("✏️ 自己输入 %q", query)
	}
	if sel == 0 {
		fmt.Printf("\r\x1b[2K▸ %s\n", paint(os.Stdout, ansiBold+ansiCyan, own))
	} else {
		fmt.Printf("\r\x1b[2K   %s\n", gray(own))
	}
	lines++
	for i, o := range filtered {
		if sel == i+1 {
			fmt.Printf("\r\x1b[2K▸ %s\n", paint(os.Stdout, ansiBold+ansiCyan, o))
		} else {
			fmt.Printf("\r\x1b[2K   %s\n", o)
		}
		lines++
	}
	fmt.Printf("\r\x1b[2K%s\n", dim(fmt.Sprintf("↑/↓ 或 j/k 选择 · 回车确认 · 数字直选 · ESC 退出 · %d 项", len(filtered))))
	lines++
	return lines
}

// chooseSnapshot lets the user pick an existing snapshot or type a new name.
// Returns (name, cancelled).
func (it *interactive) chooseSnapshot(action string) (string, bool) {
	refs, err := apollo.ListSnapshots(it.dir)
	if err != nil {
		refs = nil
	}
	opts := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.AppID != "" {
			opts = append(opts, fmt.Sprintf("%s (%s)", r.Name, r.AppID))
		} else {
			opts = append(opts, r.Name)
		}
	}
	fmt.Println()
	return it.pickOrInput(fmt.Sprintf("选择或输入快照（%s）:", action), opts)
}

// chooseKey lets the user pick an existing key of a snapshot or type a new
// one. Returns (key, cancelled).
func (it *interactive) chooseKey(snapshotName string) (string, bool) {
	keys, err := snapshotKeys(it.dir, snapshotName)
	if err != nil {
		keys = nil
	}
	fmt.Println()
	return it.pickOrInput(fmt.Sprintf("选择或输入 key（快照 %s）:", snapshotName), keys)
}

func writeTemp(prefix, content string) (string, error) {
	f, err := os.CreateTemp("", prefix)
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	return path, nil
}

func (it *interactive) cmdImport() {
	name, cancel := it.prompt("新建快照名（如 prod / test）", "prod")
	if cancel {
		return
	}
	appID, cancel := it.prompt("App ID（必填）", "")
	if cancel {
		return
	}
	if appID == "" {
		fmt.Println("App ID 不能为空，已取消")
		return
	}
	force := false
	if _, err := os.Stat(apollo.SnapPath(it.dir, name, appID)); err == nil {
		overwrite, cancel := it.prompt("快照已存在，覆盖? (y/N)", "n")
		if cancel || !strings.EqualFold(overwrite, "y") {
			fmt.Println("已取消")
			return
		}
		force = true
	}
	text, cancel := it.promptMultiline("粘贴 Apollo 键值对:")
	if cancel {
		return
	}
	if strings.TrimSpace(text) == "" {
		fmt.Println("没有内容，已取消")
		return
	}
	tmp, err := writeTemp("ai-tools-import-*.txt", text)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	defer os.Remove(tmp)
	args := []string{"apollo", "import", tmp, "--name", name, "--app-id", appID, "--dir", it.dir}
	if force {
		args = append(args, "--force")
	}
	Run(args)
}

func (it *interactive) cmdList() {
	sel, cancel := it.chooseSnapshot("list")
	if cancel {
		return
	}
	reveal, cancel := it.prompt("显示明文? (y/N)", "n")
	if cancel {
		return
	}
	name, appID := splitSnapshotRef(sel)
	args := []string{"apollo", "list", name, "--dir", it.dir}
	if appID != "" {
		args = append(args, "--appid", appID)
	}
	if strings.EqualFold(reveal, "y") || strings.EqualFold(reveal, "yes") {
		args = append(args, "--reveal")
	}
	Run(args)
}

func (it *interactive) cmdGet() {
	sel, cancel := it.chooseSnapshot("get")
	if cancel {
		return
	}
	key, cancel := it.chooseKey(sel)
	if cancel {
		return
	}
	name, appID := splitSnapshotRef(sel)
	args := []string{"apollo", "get", name, key, "--dir", it.dir}
	if appID != "" {
		args = append(args, "--appid", appID)
	}
	Run(args)
}

func (it *interactive) cmdSet() {
	sel, cancel := it.chooseSnapshot("set")
	if cancel {
		return
	}
	key, cancel := it.chooseKey(sel)
	if cancel {
		return
	}
	value, cancel := it.promptValue("value")
	if cancel {
		return
	}
	name, appID := splitSnapshotRef(sel)
	args := []string{"apollo", "set", name, key, value, "--dir", it.dir}
	if appID != "" {
		args = append(args, "--appid", appID)
	}
	Run(args)
}

func (it *interactive) cmdUnset() {
	sel, cancel := it.chooseSnapshot("unset")
	if cancel {
		return
	}
	key, cancel := it.chooseKey(sel)
	if cancel {
		return
	}
	name, appID := splitSnapshotRef(sel)
	args := []string{"apollo", "unset", name, key, "--dir", it.dir}
	if appID != "" {
		args = append(args, "--appid", appID)
	}
	Run(args)
}

func (it *interactive) cmdCompare() {
	selA, cancel := it.chooseSnapshot("compare A")
	if cancel {
		return
	}
	selB, cancel := it.chooseSnapshot("compare B")
	if cancel {
		return
	}
	reveal, cancel := it.prompt("显示明文? (y/N)", "n")
	if cancel {
		return
	}
	nameA, appIDA := splitSnapshotRef(selA)
	nameB, appIDB := splitSnapshotRef(selB)
	args := []string{"apollo", "compare", nameA, nameB, "--dir", it.dir}
	if appIDA != "" {
		args = append(args, "--appid", appIDA)
	}
	if appIDB != "" {
		args = append(args, "--appid-to", appIDB)
	}
	if strings.EqualFold(reveal, "y") || strings.EqualFold(reveal, "yes") {
		args = append(args, "--reveal")
	}
	Run(args)
}

func (it *interactive) cmdReveal() {
	sel, cancel := it.chooseSnapshot("reveal")
	if cancel {
		return
	}
	key, cancel := it.chooseKey(sel)
	if cancel {
		return
	}
	name, appID := splitSnapshotRef(sel)
	args := []string{"apollo", "reveal", name, key, "--dir", it.dir}
	if appID != "" {
		args = append(args, "--appid", appID)
	}
	if it.aesKey != "" {
		args = append(args, "--key", it.aesKey)
	}
	if it.aesIV != "" {
		args = append(args, "--iv", it.aesIV)
	}
	Run(args)
}

func (it *interactive) cmdEdit() {
	sel, cancel := it.chooseSnapshot("edit")
	if cancel {
		return
	}
	name, appID := splitSnapshotRef(sel)
	args := []string{"apollo", "edit", name, "--dir", it.dir}
	if appID != "" {
		args = append(args, "--appid", appID)
	}
	Run(args)
}

func (it *interactive) cmdExport() {
	sel, cancel := it.chooseSnapshot("export")
	if cancel {
		return
	}
	toClip, cancel := it.prompt("复制到剪贴板? (y/N)", "n")
	if cancel {
		return
	}
	name, appID := splitSnapshotRef(sel)
	args := []string{"apollo", "export", name, "--dir", it.dir}
	if appID != "" {
		args = append(args, "--appid", appID)
	}
	if strings.EqualFold(toClip, "y") || strings.EqualFold(toClip, "yes") {
		args = append(args, "--copy")
	}
	Run(args)
}

func (it *interactive) aesKeyIv() (string, string, bool) {
	key, cancel := it.prompt("AES secret key", it.aesKey)
	if cancel {
		return "", "", true
	}
	iv, cancel := it.prompt("AES iv", it.aesIV)
	if cancel {
		return "", "", true
	}
	if key == "" || iv == "" {
		fmt.Println("key 和 iv 都不能为空，已取消")
		return "", "", true
	}
	return key, iv, false
}

func (it *interactive) cmdAESEncrypt() {
	key, iv, cancel := it.aesKeyIv()
	if cancel {
		return
	}
	text, cancel := it.promptMultiline("输入要加密的明文（可多行）:")
	if cancel {
		return
	}
	if strings.TrimSpace(text) == "" {
		fmt.Println("没有内容，已取消")
		return
	}
	tmp, err := writeTemp("ai-tools-aes-in-*.txt", text)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	defer os.Remove(tmp)
	Run([]string{"aes", "encrypt", "--key", key, "--iv", iv, "--file", tmp})
}

func (it *interactive) cmdAESDecrypt() {
	key, iv, cancel := it.aesKeyIv()
	if cancel {
		return
	}
	cipher, cancel := it.promptValue("Base64 密文")
	if cancel {
		return
	}
	if strings.TrimSpace(cipher) == "" {
		fmt.Println("没有内容，已取消")
		return
	}
	tmp, err := writeTemp("ai-tools-aes-in-*.txt", cipher+"\n")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	defer os.Remove(tmp)
	Run([]string{"aes", "decrypt", "--key", key, "--iv", iv, "--file", tmp})
}

func (it *interactive) cmdGenKey() {
	Run([]string{"aes", "gen-key"})
}

// cmdCustomKeyIv lets the user set a custom AES key/iv that is saved and used
// as the default by AES encrypt/decrypt and reveal.
func (it *interactive) cmdCustomKeyIv() {
	key, cancel := it.prompt("AES secret key", it.aesKey)
	if cancel {
		return
	}
	iv, cancel := it.prompt("AES iv", it.aesIV)
	if cancel {
		return
	}
	if key == "" || iv == "" {
		fmt.Println("key 和 iv 都不能为空，已取消")
		return
	}
	it.aesKey, it.aesIV = key, iv
	if err := app.AESConfigSave(it.aesKey, it.aesIV); err != nil {
		fmt.Fprintf(os.Stderr, "warning: 保存自定义 key/iv 失败: %v\n", err)
		return
	}
	fmt.Println(green(fmt.Sprintf("已设置自定义 key/iv（%s）", app.AESConfigPath())))
}
