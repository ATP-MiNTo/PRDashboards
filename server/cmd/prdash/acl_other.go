//go:build !windows

package main

// checkPathACL does nothing off Windows, where the mode bits are checked instead.
func checkPathACL(string) error { return nil }
