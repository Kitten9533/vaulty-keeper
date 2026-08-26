//go:build darwin

package ui

import "os/exec"

func openURL(url string) error { return exec.Command("open", url).Start() }
