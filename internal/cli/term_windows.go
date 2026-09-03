//go:build windows

package cli

import "golang.org/x/sys/windows"

// isTTY reports whether fd is a console handle: GetConsoleMode succeeds only
// on real console input/output handles, not on pipes or files.
func isTTY(fd uintptr) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(fd), &mode) == nil
}
