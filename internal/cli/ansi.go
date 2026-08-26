package cli

import (
	"os"
)

// Minimal ANSI styling (chalk-like). All helpers fall back to plain text
// when the target is not a TTY or NO_COLOR/TERM=dumb is set, so piped/script
// output stays clean.

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiCyan    = "\x1b[36m"
	ansiMagenta = "\x1b[35m"
	ansiGray    = "\x1b[90m"
)

func colorFor(w *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTTY(w.Fd())
}

func paint(w *os.File, code, s string) string {
	if !colorFor(w) {
		return s
	}
	return code + s + ansiReset
}

func cyan(s string) string    { return paint(os.Stdout, ansiCyan, s) }
func green(s string) string   { return paint(os.Stdout, ansiGreen, s) }
func yellow(s string) string  { return paint(os.Stdout, ansiYellow, s) }
func red(s string) string     { return paint(os.Stdout, ansiRed, s) }
func dim(s string) string     { return paint(os.Stdout, ansiDim, s) }
func bold(s string) string    { return paint(os.Stdout, ansiBold, s) }
func magenta(s string) string { return paint(os.Stdout, ansiMagenta, s) }
func gray(s string) string    { return paint(os.Stdout, ansiGray, s) }
