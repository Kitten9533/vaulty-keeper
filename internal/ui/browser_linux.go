//go:build linux

package ui

import "os/exec"

func openURL(url string) error { return exec.Command("xdg-open", url).Start() }
