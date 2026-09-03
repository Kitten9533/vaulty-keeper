//go:build darwin

package cli

import (
	"syscall"
	"unsafe"
)

// isTTY reports whether fd is a real terminal. ModeCharDevice is not enough:
// /dev/null is a character device but not a TTY. An ioctl(TIOCGETA) succeeds
// only on an actual terminal.
func isTTY(fd uintptr) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}
