//go:build !darwin && !linux && !windows

package cli

func isTTY(fd uintptr) bool { return false }
