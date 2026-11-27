package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
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

// prepareDataDir creates dir with mode 0700 and refuses a directory that is writable by its group
// or by others on POSIX systems, or whose ACL names other accounts than the user, SYSTEM and
// Administrators on Windows.
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
	return checkPathACL(dir)
}

// privateFiles are the files in the data dir that hold secrets or user data.
var privateFiles = []string{"session.key", secrets.FileName, secrets.VaultFileName, "data.db"}

// checkPrivateFiles refuses to start when a secret file can be read or written by the group or by
// others on POSIX, or is shared with other accounts by its ACL on Windows. Files that do not
// exist are fine.
func checkPrivateFiles(dir string) error {
	for _, name := range privateFiles {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%s is accessible by group or others (mode %04o); run: chmod 600 %s",
				path, info.Mode().Perm(), path)
		}
		if err := checkPathACL(path); err != nil {
			return err
		}
	}
	return nil
}
