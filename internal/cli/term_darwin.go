//go:build darwin

package cli

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

// rawMode puts stdin into raw mode (no echo, no line buffering, no signal
// keys) and returns a restore function. Output processing (OPOST) is kept so
// plain \n still wraps lines normally.
func rawMode() (func(), error) {
	fd := os.Stdin.Fd()
	var old syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&old))); errno != 0 {
		return nil, errno
	}
	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSETA, uintptr(unsafe.Pointer(&raw))); errno != 0 {
		return nil, errno
	}
	return func() {
		syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSETA, uintptr(unsafe.Pointer(&old)))
	}, nil
}

// isTTY reports whether fd is a real terminal. ModeCharDevice is not enough:
// /dev/null is a character device but not a TTY. An ioctl(TIOCGETA) succeeds
// only on an actual terminal.
func isTTY(fd uintptr) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// waitReadable reports whether fd becomes readable within timeout. Used
// instead of os.File.SetReadDeadline, which does not fire reliably on tty fds
// on macOS. Select mutates the fdset to show ready fds.
func waitReadable(fd uintptr, timeout time.Duration) bool {
	var r syscall.FdSet
	r.Bits[fd/32] |= 1 << (fd % 32)
	tv := syscall.NsecToTimeval(int64(timeout))
	if err := syscall.Select(int(fd)+1, &r, nil, nil, &tv); err != nil {
		return false
	}
	return r.Bits[fd/32]&(1<<(fd%32)) != 0
}
