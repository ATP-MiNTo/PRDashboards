//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheckPathACLAcceptsAPrivateDirAndRefusesAnOpenOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("icacls", dir, "/inheritance:r", "/grant:r", os.Getenv("USERNAME")+":(OI)(CI)F",
		"/grant:r", "SYSTEM:(OI)(CI)F", "/grant:r", "Administrators:(OI)(CI)F").CombinedOutput(); err != nil {
		t.Fatalf("icacls: %v %s", err, out)
	}
	if err := checkPathACL(dir); err != nil {
		t.Fatalf("private dir refused: %v", err)
	}
	if out, err := exec.Command("icacls", dir, "/grant", "Everyone:(OI)(CI)R").CombinedOutput(); err != nil {
		t.Fatalf("icacls: %v %s", err, out)
	}
	if err := checkPathACL(dir); err == nil {
		t.Fatal("a dir readable by Everyone was accepted")
	}
}
