//go:build !darwin && !linux

package ui

func openURL(url string) error { return nil }
