//go:build !darwin && !linux

package cli

import "errors"

func rawMode() (func(), error) {
	return nil, errors.New("raw terminal mode not supported on this platform")
}

func isTTY(fd uintptr) bool { return false }
