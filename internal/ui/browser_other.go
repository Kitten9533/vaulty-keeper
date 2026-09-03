//go:build !darwin && !linux && !windows

package ui

func openURL(url string) error { return nil }
