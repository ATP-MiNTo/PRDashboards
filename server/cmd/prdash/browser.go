package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

func browserCommand(goos, url string) (*exec.Cmd, error) {
	switch goos {
	case "darwin":
		return exec.Command("open", url), nil //nolint:gosec // fixed binary, validated URL
	case "linux":
		return exec.Command("xdg-open", url), nil //nolint:gosec // fixed binary, validated URL
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url), nil //nolint:gosec // fixed binary, validated URL
	}
	return nil, fmt.Errorf("opening a browser is not supported on %s", goos)
}

// openBrowser opens url in the default browser without a shell.
func openBrowser(url string) error {
	cmd, err := browserCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
