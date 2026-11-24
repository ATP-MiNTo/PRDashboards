package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return filepath.Abs(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[1:]), nil
}

// prepareDataDir creates dir with mode 0700 and, on POSIX systems, refuses a directory that is
// writable by its group or by others.
func prepareDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat data dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("data dir %s is not a directory", dir)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("data dir %s is writable by group or others (mode %04o); run chmod 700 on it",
			dir, info.Mode().Perm())
	}
	return nil
}
